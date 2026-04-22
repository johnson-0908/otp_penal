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
	// Subcommands. `ops-panel admin <sub>` routes to the admin CLI
	// (used by the `opsctl` wrapper for on-server management).
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "admin":
			adminMain(os.Args[2:])
			return
		case "version", "-v", "--version":
			fmt.Println("ops-panel", versionString())
			return
		}
	}

	var (
		configPath = flag.String("config", "", "path to config.json (default: $data_dir/config.json)")
		dataDir    = flag.String("data-dir", "", "data directory (overrides config)")
		listen     = flag.String("listen", "", "listen address (overrides config)")
		frontend   = flag.String("frontend", "", "frontend dist directory to serve (optional)")
		trustProxy = flag.Bool("trust-proxy", false, "trust X-Forwarded-For / X-Real-IP")
		devMode    = flag.Bool("dev", false, "DEV MODE: seed admin/admin, disable TOTP, disable entry gate. LOCAL ONLY.")
		autoTLS    = flag.Bool("auto-tls", true, "auto-generate a self-signed TLS cert under data_dir/tls/ if TLS files not set")
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

	// Auto-generate self-signed TLS cert on first run (unless admin set TLS
	// paths or opted out). Keeps the default deployment HTTPS-only even when
	// exposed to the internet (browser will warn once about the self-signed cert;
	// replace with a real one from a reverse proxy / Let's Encrypt later).
	if !cfg.DevMode && *autoTLS && cfg.TLSCertFile == "" && cfg.TLSKeyFile == "" {
		cert, key, err := config.EnsureSelfSignedCert(cfg.DataDir)
		if err != nil {
			logger.Error("auto-tls generate", "err", err)
			os.Exit(1)
		}
		cfg.TLSCertFile = cert
		cfg.TLSKeyFile = key
		logger.Info("auto-tls: using self-signed certificate", "cert", cert)
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
	// EntryGate must precede CSRF issuance: unauthenticated scanners should
	// see 404 (no Set-Cookie, no fingerprint), not a CSRF cookie.
	r.Use(middleware.EntryGate(cfg.EntryPath, cfg.EntrySecret, cfg.DevMode))
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
			r.Post("/account/totp/setup", s.TotpSetup)
			r.Post("/account/totp/confirm", s.TotpConfirm)
			r.Post("/account/totp/disable", s.TotpDisable)
			r.Get("/me", s.Me)
			r.Get("/system/overview", s.Overview)
			r.Get("/system/processes", s.Processes)
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

	// Generate entry path on first run if not already set. Kept separate from
	// the user-create check so rotating the path doesn't require DB reset.
	if cfg.EntryPath == "" && !cfg.DevMode {
		ep, err := config.RandomEntryPath(10)
		if err != nil {
			return err
		}
		cfg.EntryPath = ep
		// Persist the new entry path immediately. The config file path is
		// always data_dir/config.json for installed deployments.
		_ = cfg.Save(filepath.Join(cfg.DataDir, "config.json"))
	}

	var username, password string
	var mustChange bool
	if cfg.DevMode {
		username = "admin"
		password = "admin"
		mustChange = false
	} else {
		u, err := config.RandomUsername("ops", 8)
		if err != nil {
			return err
		}
		pw, err := config.RandomPassword(20)
		if err != nil {
			return err
		}
		username = u
		password = pw
		mustChange = true
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	id, err := st.CreateUser(storage.User{
		Username: username, PasswordHash: hash, TOTPSecret: "",
		MustChangePassword: mustChange,
	})
	if err != nil {
		return err
	}
	_ = st.WriteAudit(storage.AuditEntry{
		UserID: sql.NullInt64{Int64: id, Valid: true},
		Action: "user.create", Detail: "first-run admin: " + username,
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

Access URL:  https://<YOUR_SERVER_IP>:<PORT>/%s/
Username:    %s
Password:    %s

The "entry path" above is a BT/1panel-style security entrance: any URL that
doesn't first hit /%s/ returns 404, so scanners cannot fingerprint this
panel. Bookmark the full URL (including the trailing path) — you'll need it
every time your entry cookie expires (24h).

IMPORTANT:
  1. Open the Access URL in a browser. First visit accepts the self-signed
     cert warning and establishes the entry cookie.
  2. Log in with the credentials above.
  3. You will be forced to change the password on first login.
  4. Go to "Account" in the sidebar and bind an Authenticator app (Google
     Authenticator / Authy / 1Password / Aegis). TOTP is OPTIONAL but
     STRONGLY recommended for any internet-exposed deployment.
  5. Delete this file after successful login.

Recovery:
  - Forgot password:  opsctl passwd <user>
  - Lost Authenticator:  opsctl reset-2fa <user>
  - Lost entry path:  grep entry_path /var/lib/ops-panel/config.json
`, cfg.EntryPath, username, password, cfg.EntryPath)
	if err := os.WriteFile(credPath, []byte(content), 0o600); err != nil {
		return err
	}

	logger.Warn("FIRST RUN — admin credentials written", "file", credPath, "username", username, "entry_path", cfg.EntryPath)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "===========================================================")
	fmt.Fprintln(os.Stderr, "  ops-panel FIRST RUN")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Entry path: /"+cfg.EntryPath+"/")
	fmt.Fprintln(os.Stderr, "  Username:   "+username)
	fmt.Fprintln(os.Stderr, "  Password:   "+password)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Access URL:  https://<SERVER_IP>:<PORT>/"+cfg.EntryPath+"/")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  ⚠  First visit: accept self-signed cert warning.")
	fmt.Fprintln(os.Stderr, "     Any URL without the entry path returns 404.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  (also saved to: "+credPath+")")
	fmt.Fprintln(os.Stderr, "===========================================================")
	fmt.Fprintln(os.Stderr, "")
	return nil
}
