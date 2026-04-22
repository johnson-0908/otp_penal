package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/cirico/ops-panel/internal/auth"
	"github.com/cirico/ops-panel/internal/storage"
)

type ctxKey int

const (
	ctxUserID ctxKey = iota
	ctxJTI
	ctxIP
)

func UserID(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(ctxUserID).(int64)
	return v, ok
}

func JTI(ctx context.Context) string {
	v, _ := ctx.Value(ctxJTI).(string)
	return v
}

func IP(ctx context.Context) string {
	v, _ := ctx.Value(ctxIP).(string)
	return v
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"font-src 'self' data:; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func ClientIPCtx(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var ip string
			if trustProxy {
				ip = auth.ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-IP"))
			} else {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					host = r.RemoteAddr
				}
				ip = host
			}
			ctx := context.WithValue(r.Context(), ctxIP, ip)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func IPAllowList(allowed []string) func(http.Handler) http.Handler {
	if len(allowed) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	var cidrs []*net.IPNet
	var exact []net.IP
	for _, a := range allowed {
		if strings.Contains(a, "/") {
			if _, n, err := net.ParseCIDR(a); err == nil {
				cidrs = append(cidrs, n)
			}
		} else {
			if ip := net.ParseIP(a); ip != nil {
				exact = append(exact, ip)
			}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ipStr := IP(r.Context())
			ip := net.ParseIP(ipStr)
			if ip == nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			for _, n := range cidrs {
				if n.Contains(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}
			for _, e := range exact {
				if e.Equal(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}

func RateLimit(lim *auth.IPLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := IP(r.Context())
			if !lim.Allow(ip) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

const csrfCookieName = "panel_csrf"
const csrfHeaderName = "X-CSRF-Token"

func CSRFIssue(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(csrfCookieName); err != nil || c.Value == "" {
			buf := make([]byte, 32)
			_, _ = rand.Read(buf)
			token := base64.RawURLEncoding.EncodeToString(buf)
			http.SetCookie(w, &http.Cookie{
				Name:     csrfCookieName,
				Value:    token,
				Path:     "/",
				HttpOnly: false,
				Secure:   r.TLS != nil,
				SameSite: http.SameSiteStrictMode,
				MaxAge:   60 * 60 * 12,
			})
		}
		next.ServeHTTP(w, r)
	})
}

func CSRFVerify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "csrf token missing", http.StatusForbidden)
			return
		}
		header := r.Header.Get(csrfHeaderName)
		if header == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
			http.Error(w, "csrf token mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func AuthRequired(secret string, st *storage.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			tok := strings.TrimPrefix(h, "Bearer ")
			claims, err := auth.ParseToken(secret, tok)
			if err != nil || claims.Kind != "access" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ok, userID, err := st.IsSessionValid(claims.ID)
			if err != nil || !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_ = claims.ExpiresAt
			_ = time.Now()
			ctx := context.WithValue(r.Context(), ctxUserID, userID)
			ctx = context.WithValue(ctx, ctxJTI, claims.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
