package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

// EntryGate implements the "security entrance" pattern (like BT Panel /
// 1panel): the panel only responds meaningfully to clients that have first
// visited a random entry path.
//
//   - Request to `/<entryPath>` or `/<entryPath>/`: server issues a signed
//     `panel_entry` cookie and 302-redirects to `/`. Subsequent requests from
//     the same browser carry the cookie and are allowed through.
//   - Request with a valid cookie: pass through (all paths).
//   - Everything else: return 404 with no body, matching what an empty TLS
//     endpoint would serve. Keeps scanners from fingerprinting the panel.
//
// DevMode or empty entryPath disables the gate entirely.
//
// The cookie is HMAC-signed with entrySecret and carries an issue timestamp;
// cookies older than `cookieTTL` are rejected (forces periodic re-entry).
func EntryGate(entryPath, entrySecret string, devMode bool) func(http.Handler) http.Handler {
	const (
		cookieName = "panel_entry"
		cookieTTL  = 24 * time.Hour
	)

	disabled := devMode || entryPath == "" || entrySecret == ""

	sign := func(payload string) string {
		mac := hmac.New(sha256.New, []byte(entrySecret))
		mac.Write([]byte(payload))
		return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	}

	verify := func(raw string) bool {
		if raw == "" {
			return false
		}
		parts := strings.SplitN(raw, ".", 2)
		if len(parts) != 2 {
			return false
		}
		tsStr, sig := parts[0], parts[1]
		expected := sign(tsStr)
		if !hmac.Equal([]byte(sig), []byte(expected)) {
			return false
		}
		tsBytes, err := base64.RawURLEncoding.DecodeString(tsStr)
		if err != nil || len(tsBytes) != 8 {
			return false
		}
		var ts int64
		for _, b := range tsBytes {
			ts = ts<<8 | int64(b)
		}
		issued := time.Unix(ts, 0)
		if time.Since(issued) > cookieTTL {
			return false
		}
		return true
	}

	mint := func() string {
		ts := time.Now().Unix()
		b := make([]byte, 8)
		for i := 7; i >= 0; i-- {
			b[i] = byte(ts & 0xff)
			ts >>= 8
		}
		tsStr := base64.RawURLEncoding.EncodeToString(b)
		return tsStr + "." + sign(tsStr)
	}

	// Normalize the configured entry path to `/<entry>`; matches both
	// `/<entry>` and `/<entry>/` (with or without trailing slash).
	normalizedEntry := "/" + strings.Trim(entryPath, "/")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if disabled {
				next.ServeHTTP(w, r)
				return
			}

			// 1. Is this the entry-minting request?
			p := r.URL.Path
			if p == normalizedEntry || p == normalizedEntry+"/" {
				http.SetCookie(w, &http.Cookie{
					Name:     cookieName,
					Value:    mint(),
					Path:     "/",
					HttpOnly: true,
					Secure:   r.TLS != nil,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   int(cookieTTL.Seconds()),
				})
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}

			// 2. Valid cookie → pass through.
			if c, err := r.Cookie(cookieName); err == nil && verify(c.Value) {
				next.ServeHTTP(w, r)
				return
			}

			// 3. Anything else → 404, no body. Indistinguishable from an
			// "empty" TLS endpoint to a passing scanner.
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		})
	}
}
