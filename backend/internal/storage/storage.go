package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type User struct {
	ID                 int64
	Username           string
	PasswordHash       string
	TOTPSecret         string
	CreatedAt          time.Time
	MustChangePassword bool
}

type Session struct {
	ID        int64
	UserID    int64
	JTI       string
	CreatedAt time.Time
	ExpiresAt time.Time
	Revoked   bool
	UserAgent string
	IP        string
}

type LoginAttempt struct {
	IP        string
	Username  string
	Success   bool
	Reason    string
	CreatedAt time.Time
}

type AuditEntry struct {
	ID        int64
	UserID    sql.NullInt64
	IP        string
	Action    string
	Detail    string
	CreatedAt time.Time
	PrevHash  string
	Hash      string
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    username             TEXT NOT NULL UNIQUE,
    password_hash        TEXT NOT NULL,
    totp_secret          TEXT NOT NULL,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    must_change_password INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    jti        TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    revoked    INTEGER NOT NULL DEFAULT 0,
    user_agent TEXT,
    ip         TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_jti ON sessions(jti);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS login_attempts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ip         TEXT NOT NULL,
    username   TEXT NOT NULL,
    success    INTEGER NOT NULL,
    reason     TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_attempts_ip_time ON login_attempts(ip, created_at);
CREATE INDEX IF NOT EXISTS idx_attempts_user_time ON login_attempts(username, created_at);

CREATE TABLE IF NOT EXISTS ip_blocks (
    ip             TEXT PRIMARY KEY,
    blocked_until  DATETIME NOT NULL,
    reason         TEXT
);

CREATE TABLE IF NOT EXISTS audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER,
    ip         TEXT,
    action     TEXT NOT NULL,
    detail     TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    prev_hash  TEXT NOT NULL DEFAULT '',
    hash       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_log(created_at);
`

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(u User) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO users(username, password_hash, totp_secret, must_change_password) VALUES (?,?,?,?)`,
		u.Username, u.PasswordHash, u.TOTPSecret, boolToInt(u.MustChangePassword),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetUserByName(username string) (*User, error) {
	row := s.db.QueryRow(`SELECT id, username, password_hash, totp_secret, created_at, must_change_password FROM users WHERE username = ?`, username)
	var u User
	var mcp int
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &u.CreatedAt, &mcp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	u.MustChangePassword = mcp == 1
	return &u, nil
}

func (s *Store) GetUserByID(id int64) (*User, error) {
	row := s.db.QueryRow(`SELECT id, username, password_hash, totp_secret, created_at, must_change_password FROM users WHERE id = ?`, id)
	var u User
	var mcp int
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &u.CreatedAt, &mcp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	u.MustChangePassword = mcp == 1
	return &u, nil
}

func (s *Store) UpdatePassword(userID int64, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash = ?, must_change_password = 0 WHERE id = ?`, hash, userID)
	return err
}

func (s *Store) UpdateTOTPSecret(userID int64, secret string) error {
	_, err := s.db.Exec(`UPDATE users SET totp_secret = ? WHERE id = ?`, secret, userID)
	return err
}

func (s *Store) CreateSession(sess Session) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions(user_id, jti, expires_at, user_agent, ip) VALUES (?,?,?,?,?)`,
		sess.UserID, sess.JTI, sess.ExpiresAt, sess.UserAgent, sess.IP,
	)
	return err
}

func (s *Store) IsSessionValid(jti string) (bool, int64, error) {
	row := s.db.QueryRow(`SELECT user_id, expires_at, revoked FROM sessions WHERE jti = ?`, jti)
	var userID int64
	var expires time.Time
	var revoked int
	if err := row.Scan(&userID, &expires, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, 0, nil
		}
		return false, 0, err
	}
	if revoked == 1 || time.Now().After(expires) {
		return false, 0, nil
	}
	return true, userID, nil
}

func (s *Store) RevokeSession(jti string) error {
	_, err := s.db.Exec(`UPDATE sessions SET revoked = 1 WHERE jti = ?`, jti)
	return err
}

func (s *Store) RecordLoginAttempt(a LoginAttempt) error {
	_, err := s.db.Exec(
		`INSERT INTO login_attempts(ip, username, success, reason) VALUES (?,?,?,?)`,
		a.IP, a.Username, boolToInt(a.Success), a.Reason,
	)
	return err
}

func (s *Store) FailedAttemptsFromIP(ip string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM login_attempts WHERE ip = ? AND success = 0 AND created_at > ?`,
		ip, since,
	).Scan(&n)
	return n, err
}

func (s *Store) FailedAttemptsForUser(username string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM login_attempts WHERE username = ? AND success = 0 AND created_at > ?`,
		username, since,
	).Scan(&n)
	return n, err
}

func (s *Store) BlockIP(ip string, until time.Time, reason string) error {
	_, err := s.db.Exec(
		`INSERT INTO ip_blocks(ip, blocked_until, reason) VALUES (?,?,?)
		 ON CONFLICT(ip) DO UPDATE SET blocked_until=excluded.blocked_until, reason=excluded.reason`,
		ip, until, reason,
	)
	return err
}

func (s *Store) IsIPBlocked(ip string) (bool, time.Time, error) {
	row := s.db.QueryRow(`SELECT blocked_until FROM ip_blocks WHERE ip = ?`, ip)
	var until time.Time
	if err := row.Scan(&until); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, time.Time{}, nil
		}
		return false, time.Time{}, err
	}
	if time.Now().After(until) {
		return false, until, nil
	}
	return true, until, nil
}

func (s *Store) WriteAudit(e AuditEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var prev string
	_ = tx.QueryRow(`SELECT hash FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&prev)

	h := sha256.New()
	h.Write([]byte(prev))
	h.Write([]byte("|"))
	h.Write([]byte(e.Action))
	h.Write([]byte("|"))
	h.Write([]byte(e.IP))
	h.Write([]byte("|"))
	h.Write([]byte(e.Detail))
	h.Write([]byte("|"))
	h.Write([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	if e.UserID.Valid {
		h.Write([]byte(fmt.Sprintf("|%d", e.UserID.Int64)))
	}
	hash := hex.EncodeToString(h.Sum(nil))

	var uid any
	if e.UserID.Valid {
		uid = e.UserID.Int64
	}
	_, err = tx.Exec(
		`INSERT INTO audit_log(user_id, ip, action, detail, prev_hash, hash) VALUES (?,?,?,?,?,?)`,
		uid, e.IP, e.Action, e.Detail, prev, hash,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListAudit(limit, offset int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, user_id, ip, action, detail, created_at, prev_hash, hash
		 FROM audit_log ORDER BY id DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var uid sql.NullInt64
		if err := rows.Scan(&e.ID, &uid, &e.IP, &e.Action, &e.Detail, &e.CreatedAt, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		e.UserID = uid
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) ListRecentAttempts(limit, offset int) ([]LoginAttempt, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(
		`SELECT ip, username, success, COALESCE(reason,''), created_at
		 FROM login_attempts ORDER BY id DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoginAttempt
	for rows.Next() {
		var a LoginAttempt
		var success int
		if err := rows.Scan(&a.IP, &a.Username, &success, &a.Reason, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Success = success == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) CountLoginAttempts() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM login_attempts`).Scan(&n)
	return n, err
}

func NormalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
