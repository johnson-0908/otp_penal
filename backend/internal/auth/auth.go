package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16

	accessTTL  = 15 * time.Minute
	refreshTTL = 12 * time.Hour
)

// HashPassword hashes pw with argon2id. Callers are responsible for policy
// (length, character classes). No validation is performed here so that
// firstRunInit / dev mode can seed short fixed passwords.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func VerifyPassword(pw, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory uint32
	var time1 uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time1, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, time1, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func GenerateTOTP(issuer, account string) (*otp.Key, error) {
	return totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
}

func VerifyTOTP(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	valid, _ := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return valid
}

type Claims struct {
	jwt.RegisteredClaims
	Kind string `json:"kind"`
}

func RandomID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

type TokenPair struct {
	Access        string
	Refresh       string
	AccessJTI     string
	RefreshJTI    string
	AccessExpiry  time.Time
	RefreshExpiry time.Time
}

func IssueTokens(secret, issuer string, userID int64) (TokenPair, error) {
	now := time.Now()
	accessJTI := RandomID()
	refreshJTI := RandomID()

	mk := func(jti, kind string, ttl time.Duration) (string, time.Time, error) {
		exp := now.Add(ttl)
		c := Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    issuer,
				Subject:   fmt.Sprintf("%d", userID),
				ID:        jti,
				IssuedAt:  jwt.NewNumericDate(now),
				NotBefore: jwt.NewNumericDate(now),
				ExpiresAt: jwt.NewNumericDate(exp),
			},
			Kind: kind,
		}
		t := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
		s, err := t.SignedString([]byte(secret))
		return s, exp, err
	}

	access, accessExp, err := mk(accessJTI, "access", accessTTL)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, refreshExp, err := mk(refreshJTI, "refresh", refreshTTL)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		Access: access, Refresh: refresh,
		AccessJTI: accessJTI, RefreshJTI: refreshJTI,
		AccessExpiry: accessExp, RefreshExpiry: refreshExp,
	}, nil
}

func ParseToken(secret, token string) (*Claims, error) {
	c := &Claims{}
	_, err := jwt.ParseWithClaims(token, c, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func AccessTTL() time.Duration  { return accessTTL }
func RefreshTTL() time.Duration { return refreshTTL }
