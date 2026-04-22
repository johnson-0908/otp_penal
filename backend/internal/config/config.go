package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	ListenAddr  string   `json:"listen_addr"`
	TLSCertFile string   `json:"tls_cert_file"`
	TLSKeyFile  string   `json:"tls_key_file"`
	DataDir     string   `json:"data_dir"`
	JWTSecret   string   `json:"jwt_secret"`
	Issuer      string   `json:"issuer"`
	AllowedIPs  []string `json:"allowed_ips"`

	// EntryPath is a random URL segment that gates panel access (like BT/1panel
	// "security entrance"). Requests must first hit `/<EntryPath>/` to receive
	// the panel_entry cookie; without the cookie every path returns 404 so
	// scanners can't fingerprint the panel. Empty = disabled (dev only).
	EntryPath string `json:"entry_path"`

	// EntrySecret signs the panel_entry cookie (HMAC-SHA256). Separate from
	// JWTSecret so rotating one doesn't invalidate the other.
	EntrySecret string `json:"entry_secret"`

	// DevMode is set only at runtime (via -dev flag) and never persisted.
	// When true: admin/admin is seeded on first run, TOTP checks are
	// bypassed on /api/auth/login and /api/auth/change-password, and
	// EntryPath check is skipped.
	DevMode bool `json:"-"`
}

func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		ListenAddr: "127.0.0.1:8443",
		DataDir:    filepath.Join(home, ".ops-panel"),
		Issuer:     "ops-panel",
	}
}

func Load(path string) (*Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := c.ensureSecret(); err != nil {
				return nil, err
			}
			if err := c.Save(path); err != nil {
				return nil, err
			}
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	if err := c.ensureSecret(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) ensureSecret() error {
	if c.JWTSecret == "" {
		buf := make([]byte, 48)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		c.JWTSecret = base64.RawStdEncoding.EncodeToString(buf)
	}
	if c.EntrySecret == "" {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		c.EntrySecret = base64.RawStdEncoding.EncodeToString(buf)
	}
	return nil
}

// RandomEntryPath generates a short, URL-safe random segment (no ambiguous
// chars) used as the "security entrance" path. Matches BT-style `/abc1234/`.
func RandomEntryPath(n int) (string, error) {
	if n < 8 {
		n = 10
	}
	const alpha = "abcdefghijkmnpqrstuvwxyz23456789"
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alpha[int(buf[i])%len(alpha)]
	}
	return string(buf), nil
}

func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) EnsureDataDir() error {
	return os.MkdirAll(c.DataDir, 0o700)
}

func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "panel.db") }

func RandomPassword(n int) (string, error) {
	if n < 12 {
		n = 16
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)[:n], nil
}

// RandomUsername generates a username of the form `prefix_<N lowercase
// alphanumerics>`, suitable for seeding the first admin. Defeats
// username-enumeration guesses (no hardcoded "admin").
func RandomUsername(prefix string, n int) (string, error) {
	if n < 6 {
		n = 8
	}
	const alpha = "abcdefghijkmnpqrstuvwxyz23456789" // no 0/o/1/l/i for readability
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	for i := range buf {
		buf[i] = alpha[int(buf[i])%len(alpha)]
	}
	if prefix == "" {
		return string(buf), nil
	}
	return prefix + "_" + string(buf), nil
}
