package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/cirico/ops-panel/internal/auth"
	"github.com/cirico/ops-panel/internal/config"
	"github.com/cirico/ops-panel/internal/middleware"
	"github.com/cirico/ops-panel/internal/storage"
	"github.com/cirico/ops-panel/internal/system"
)

type Server struct {
	Cfg       *config.Config
	Store     *storage.Store
	LoginLim  *auth.IPLimiter
	Logger    *slog.Logger
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

type tokenResp struct {
	AccessToken       string    `json:"access_token"`
	RefreshToken      string    `json:"refresh_token"`
	AccessExpiresAt   time.Time `json:"access_expires_at"`
	RefreshExpiresAt  time.Time `json:"refresh_expires_at"`
	MustChangePassword bool     `json:"must_change_password"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"time":     time.Now().UTC(),
		"dev_mode": s.Cfg.DevMode,
	})
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	ip := middleware.IP(r.Context())

	if blocked, until, _ := s.Store.IsIPBlocked(ip); blocked {
		retry := int(time.Until(until).Seconds())
		if retry < 1 {
			retry = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeErr(w, http.StatusTooManyRequests, "ip temporarily blocked")
		return
	}

	if !s.LoginLim.Allow(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many requests")
		return
	}

	var req loginReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	username := storage.NormalizeUsername(req.Username)

	failRecently, _ := s.Store.FailedAttemptsFromIP(ip, time.Now().Add(-15*time.Minute))
	if failRecently >= 5 {
		_ = s.Store.BlockIP(ip, time.Now().Add(15*time.Minute), "too many failed logins from ip")
		_ = s.Store.WriteAudit(storage.AuditEntry{
			IP: ip, Action: "ip.block", Detail: "auto-block after 5 failed logins",
		})
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts")
		return
	}

	failUser, _ := s.Store.FailedAttemptsForUser(username, time.Now().Add(-60*time.Minute))
	if failUser >= 10 {
		_ = s.Store.RecordLoginAttempt(storage.LoginAttempt{IP: ip, Username: username, Success: false, Reason: "account temporarily locked"})
		writeErr(w, http.StatusTooManyRequests, "account temporarily locked")
		return
	}

	u, err := s.Store.GetUserByName(username)
	if err != nil {
		s.Logger.Error("user lookup", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	reject := func(reason string) {
		_ = s.Store.RecordLoginAttempt(storage.LoginAttempt{IP: ip, Username: username, Success: false, Reason: reason})
		_ = s.Store.WriteAudit(storage.AuditEntry{IP: ip, Action: "login.fail", Detail: username + ": " + reason})
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
	}

	if u == nil {
		_ = auth.VerifyPassword(req.Password, "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		reject("no such user")
		return
	}
	if !auth.VerifyPassword(req.Password, u.PasswordHash) {
		reject("bad password")
		return
	}
	if !s.Cfg.DevMode && !auth.VerifyTOTP(u.TOTPSecret, req.Code) {
		reject("bad totp")
		return
	}

	tp, err := auth.IssueTokens(s.Cfg.JWTSecret, s.Cfg.Issuer, u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	if err := s.Store.CreateSession(storage.Session{
		UserID: u.ID, JTI: tp.AccessJTI, ExpiresAt: tp.AccessExpiry,
		UserAgent: r.UserAgent(), IP: ip,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "session create failed")
		return
	}
	if err := s.Store.CreateSession(storage.Session{
		UserID: u.ID, JTI: tp.RefreshJTI, ExpiresAt: tp.RefreshExpiry,
		UserAgent: r.UserAgent(), IP: ip,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "session create failed")
		return
	}

	_ = s.Store.RecordLoginAttempt(storage.LoginAttempt{IP: ip, Username: username, Success: true})
	_ = s.Store.WriteAudit(storage.AuditEntry{
		UserID: sql.NullInt64{Int64: u.ID, Valid: true},
		IP:     ip, Action: "login.ok", Detail: username,
	})

	writeJSON(w, http.StatusOK, tokenResp{
		AccessToken: tp.Access, RefreshToken: tp.Refresh,
		AccessExpiresAt: tp.AccessExpiry, RefreshExpiresAt: tp.RefreshExpiry,
		MustChangePassword: u.MustChangePassword,
	})
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

func (s *Server) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	claims, err := auth.ParseToken(s.Cfg.JWTSecret, req.RefreshToken)
	if err != nil || claims.Kind != "refresh" {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}
	ok, userID, err := s.Store.IsSessionValid(claims.ID)
	if err != nil || !ok {
		writeErr(w, http.StatusUnauthorized, "invalid session")
		return
	}
	_ = s.Store.RevokeSession(claims.ID)

	tp, err := auth.IssueTokens(s.Cfg.JWTSecret, s.Cfg.Issuer, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	ip := middleware.IP(r.Context())
	_ = s.Store.CreateSession(storage.Session{UserID: userID, JTI: tp.AccessJTI, ExpiresAt: tp.AccessExpiry, UserAgent: r.UserAgent(), IP: ip})
	_ = s.Store.CreateSession(storage.Session{UserID: userID, JTI: tp.RefreshJTI, ExpiresAt: tp.RefreshExpiry, UserAgent: r.UserAgent(), IP: ip})

	writeJSON(w, http.StatusOK, tokenResp{
		AccessToken: tp.Access, RefreshToken: tp.Refresh,
		AccessExpiresAt: tp.AccessExpiry, RefreshExpiresAt: tp.RefreshExpiry,
	})
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	jti := middleware.JTI(r.Context())
	userID, _ := middleware.UserID(r.Context())
	_ = s.Store.RevokeSession(jti)
	_ = s.Store.WriteAudit(storage.AuditEntry{
		UserID: sql.NullInt64{Int64: userID, Valid: true},
		IP:     middleware.IP(r.Context()), Action: "logout",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserID(r.Context())
	u, err := s.Store.GetUserByID(userID)
	if err != nil || u == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                   u.ID,
		"username":             u.Username,
		"created_at":           u.CreatedAt,
		"must_change_password": u.MustChangePassword,
	})
}

func (s *Server) Overview(w http.ResponseWriter, r *http.Request) {
	ov, err := system.GetOverview(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

func (s *Server) Audit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	entries, err := s.Store.ListAudit(limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) RecentAttempts(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	attempts, err := s.Store.ListRecentAttempts(limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, err := s.Store.CountLoginAttempts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if attempts == nil {
		attempts = []storage.LoginAttempt{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": attempts,
		"total": total,
	})
}

type changePwReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
	Code        string `json:"code"`
}

func (s *Server) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserID(r.Context())
	u, err := s.Store.GetUserByID(userID)
	if err != nil || u == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var req changePwReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if !auth.VerifyPassword(req.OldPassword, u.PasswordHash) {
		writeErr(w, http.StatusUnauthorized, "old password incorrect")
		return
	}
	if !s.Cfg.DevMode && !auth.VerifyTOTP(u.TOTPSecret, req.Code) {
		writeErr(w, http.StatusUnauthorized, "totp incorrect")
		return
	}
	if len(req.NewPassword) < 12 {
		writeErr(w, http.StatusBadRequest, "new password must be at least 12 characters")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash failed")
		return
	}
	if err := s.Store.UpdatePassword(userID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	_ = s.Store.WriteAudit(storage.AuditEntry{
		UserID: sql.NullInt64{Int64: userID, Valid: true},
		IP:     middleware.IP(r.Context()),
		Action: "password.change",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
