package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/cirico/ops-panel/internal/api"
	"github.com/cirico/ops-panel/internal/auth"
	"github.com/cirico/ops-panel/internal/config"
	"github.com/cirico/ops-panel/internal/middleware"
	"github.com/cirico/ops-panel/internal/storage"
)

func main() {
	var (
		configPath = flag.String("config", "", "path to config.json (default: $data_dir/config.json)")
		dataDir    = flag.String("data-dir", "", "data directory (overrides config)")
		listen     = flag.String("listen", "", "listen address (overrides config)")
		frontend   = flag.String("frontend", "", "frontend dist directory to serve (optional)")
		trustProxy = flag.Bool("trust-proxy", false, "trust X-Forwarded-For / X-Real-IP")
		devMode    = flag.Bool("dev", false, "DEV MODE: seed admin/admin, disable TOTP. LOCAL ONLY.")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	defaultCfg := config.Default()
	if *configPath == "" {
		if *dataDir != "" {
			*configPath = filepath.Join(*dataDir, "config.json")
		} else {
			*configPath = filepath.Join(defaultCfg.DataDir, "config.json")
		}
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if *listen != "" {
		cfg.ListenAddr = *listen
	}
	cfg.DevMode = *devMode
	if cfg.DevMode {
		fmt.Fprintln(os.Stderr, "====================================================")
		fmt.Fprintln(os.Stderr, "  DEV MODE  (admin/admin, TOTP disabled)")
		fmt.Fprintln(os.Stderr, "  DO NOT EXPOSE THIS INSTANCE TO THE PUBLIC INTERNET")
		fmt.Fprintln(os.Stderr, "====================================================")
	}
	if err := cfg.EnsureDataDir(); err != nil {
		logger.Error("mkdir data", "err", err)
		os.Exit(1)
	}
	if err := cfg.Save(*configPath); err != nil {
		logger.Error("save config", "err", err)
		os.Exit(1)
	}

	st, err := storage.Open(cfg.DBPath())
	if err != nil {
		logger.Error("open db", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	if err := firstRunInit(cfg, st, logger); err != nil {
		logger.Error("first-run init", "err", err)
		os.Exit(1)
	}

	loginLim := auth.NewIPLimiter(rate.Every(5*time.Second), 3)
	globalLim := auth.NewIPLimiter(rate.Limit(20), 40)

	s := &api.Server{Cfg: cfg, Store: st, LoginLim: loginLim, Logger: logger}

	r := chi.NewRouter()
	r.Use(middleware.ClientIPCtx(*trustProxy))
	r.Use(middleware.IPAllowList(cfg.AllowedIPs))
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.RateLimit(globalLim))
	r.Use(middleware.CSRFIssue)

	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.CSRFVerify)
		r.Get("/health", s.Health)
		r.Post("/auth/login", s.Login)
		r.Post("/auth/refresh", s.Refresh)

		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthRequired(cfg.JWTSecret, st))
			r.Post("/auth/logout", s.Logout)
			r.Post("/auth/change-password", s.ChangePassword)
			r.Get("/me", s.Me)
			r.Get("/system/overview", s.Overview)
			r.Get("/audit", s.Audit)
			r.Get("/security/recent-attempts", s.RecentAttempts)
		})
	})

	if *frontend != "" {
		root := http.Dir(*frontend)
		fileServer := http.FileServer(root)
		r.Get("/*", func(w http.ResponseWriter, rq *http.Request) {
			path := filepath.Clean(rq.URL.Path)
			full := filepath.Join(*frontend, path)
			if info, err := os.Stat(full); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, rq)
				return
			}
			if _, err := os.Stat(filepath.Join(*frontend, "index.html")); errors.Is(err, fs.ErrNotExist) {
				http.NotFound(w, rq)
				return
			}
			http.ServeFile(w, rq, filepath.Join(*frontend, "index.html"))
		})
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		var err error
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			logger.Info("listening (TLS)", "addr", cfg.ListenAddr)
			err = srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			logger.Warn("listening without TLS — only use behind a trusted reverse proxy or localhost tunnel", "addr", cfg.ListenAddr)
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func firstRunInit(cfg *config.Config, st *storage.Store, logger *slog.Logger) error {
	n, err := st.CountUsers()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	var password string
	var mustChange bool
	if cfg.DevMode {
		password = "admin"
		mustChange = false
	} else {
		pw, err := config.RandomPassword(20)
		if err != nil {
			return err
		}
		password = pw
		mustChange = true
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	key, err := auth.GenerateTOTP(cfg.Issuer, "admin")
	if err != nil {
		return err
	}

	id, err := st.CreateUser(storage.User{
		Username: "admin", PasswordHash: hash, TOTPSecret: key.Secret(),
		MustChangePassword: mustChange,
	})
	if err != nil {
		return err
	}
	_ = st.WriteAudit(storage.AuditEntry{
		UserID: sql.NullInt64{Int64: id, Valid: true},
		Action: "user.create", Detail: "first-run admin",
	})

	if cfg.DevMode {
		logger.Warn("FIRST RUN (dev) — seeded admin/admin, TOTP disabled", "data_dir", cfg.DataDir)
		fmt.Fprintln(os.Stderr, "================ OPS-PANEL FIRST RUN (DEV) ================")
		fmt.Fprintln(os.Stderr, "  username: admin")
		fmt.Fprintln(os.Stderr, "  password: admin")
		fmt.Fprintln(os.Stderr, "  TOTP:     disabled in dev mode")
		fmt.Fprintln(os.Stderr, "===========================================================")
		return nil
	}

	credPath := filepath.Join(cfg.DataDir, "FIRST_RUN_CREDENTIALS.txt")
	content := fmt.Sprintf(`ops-panel first-run credentials
================================

Username: admin
Password: %s

TOTP secret (enter in your authenticator app):
  %s

otpauth URL:
  %s

PROVISION QR (scan with Google Authenticator / Authy / 1Password / Aegis):
  Generate from the otpauth URL above with any QR tool.

IMPORTANT:
  1. Save the TOTP secret NOW. You cannot retrieve it later.
  2. You MUST change the password on first login.
  3. Delete this file after successful login.
`, password, key.Secret(), key.URL())
	if err := os.WriteFile(credPath, []byte(content), 0o600); err != nil {
		return err
	}

	logger.Warn("FIRST RUN — admin credentials written", "file", credPath)
	fmt.Fprintln(os.Stderr, "================ OPS-PANEL FIRST RUN ================")
	fmt.Fprintln(os.Stderr, "Admin credentials have been written to:")
	fmt.Fprintln(os.Stderr, "  "+credPath)
	fmt.Fprintln(os.Stderr, "Read it, save the TOTP secret, then delete the file.")
	fmt.Fprintln(os.Stderr, "=====================================================")
	return nil
}
