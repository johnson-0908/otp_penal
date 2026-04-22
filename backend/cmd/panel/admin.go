package main

// admin subcommands — invoked via `ops-panel admin <cmd>`.
// Used by the `opsctl` wrapper on installed servers for password reset,
// TOTP reset, user listing, etc. Operates directly on the SQLite DB
// (same config/data-dir resolution as the server).

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/cirico/ops-panel/internal/auth"
	"github.com/cirico/ops-panel/internal/config"
	"github.com/cirico/ops-panel/internal/storage"
)

// version is injected at build time via -ldflags "-X main.version=..."
var version = "dev"

func versionString() string { return version }

const adminUsage = `ops-panel admin — server-side management commands

Usage:
  ops-panel admin <subcommand> [flags]

Subcommands:
  info                    show panel config + user summary (JSON)
  list-users              list all users with TOTP binding status
  reset-password          reset a user's password (prompts for new password)
  reset-totp              clear a user's TOTP secret (unbinds authenticator)

Global flags:
  -config string          path to config.json
  -data-dir string        data directory (overrides config)

Examples:
  ops-panel admin info
  ops-panel admin list-users
  ops-panel admin reset-password -user admin
  ops-panel admin reset-totp -user admin -yes
`

func adminMain(args []string) {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, adminUsage)
		os.Exit(2)
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "info":
		adminInfo(rest)
	case "list-users":
		adminListUsers(rest)
	case "reset-password":
		adminResetPassword(rest)
	case "reset-totp":
		adminResetTOTP(rest)
	case "help", "-h", "--help":
		fmt.Print(adminUsage)
	default:
		fmt.Fprintf(os.Stderr, "unknown admin subcommand: %s\n\n%s", sub, adminUsage)
		os.Exit(2)
	}
}

// openStore resolves config / data-dir the same way the server does,
// then opens the SQLite DB.
func openStore(fs *flag.FlagSet) (*storage.Store, *config.Config, string) {
	configPath := fs.String("config", "", "path to config.json")
	dataDir := fs.String("data-dir", "", "data directory (overrides config)")
	_ = fs.Parse(fs.Args()[0:]) // parsed by caller, no-op safeguard
	_ = configPath
	_ = dataDir
	return nil, nil, ""
}

// openStoreFromFlags is the real helper — call after flag.Parse.
func openStoreFromFlags(configPath, dataDir string) (*storage.Store, *config.Config) {
	defaultCfg := config.Default()
	cp := configPath
	if cp == "" {
		if dataDir != "" {
			cp = filepath.Join(dataDir, "config.json")
		} else {
			cp = filepath.Join(defaultCfg.DataDir, "config.json")
		}
	}
	cfg, err := config.Load(cp)
	if err != nil {
		die("load config %s: %v", cp, err)
	}
	if dataDir != "" {
		cfg.DataDir = dataDir
	}
	st, err := storage.Open(cfg.DBPath())
	if err != nil {
		die("open db %s: %v", cfg.DBPath(), err)
	}
	return st, cfg
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", a...)
	os.Exit(1)
}

// ---- ops-panel admin info ----

func adminInfo(args []string) {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config.json")
	dd := fs.String("data-dir", "", "data directory")
	_ = fs.Parse(args)

	st, cfg := openStoreFromFlags(*cfgPath, *dd)
	defer st.Close()

	users, err := st.ListUsersBrief()
	if err != nil {
		die("list users: %v", err)
	}

	out := map[string]any{
		"version":     version,
		"listen_addr": cfg.ListenAddr,
		"data_dir":    cfg.DataDir,
		"db_path":     cfg.DBPath(),
		"issuer":      cfg.Issuer,
		"tls":         cfg.TLSCertFile != "" && cfg.TLSKeyFile != "",
		"allowed_ips": cfg.AllowedIPs,
		"users":       users,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// ---- ops-panel admin list-users ----

func adminListUsers(args []string) {
	fs := flag.NewFlagSet("list-users", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config.json")
	dd := fs.String("data-dir", "", "data directory")
	asJSON := fs.Bool("json", false, "output as JSON")
	_ = fs.Parse(args)

	st, _ := openStoreFromFlags(*cfgPath, *dd)
	defer st.Close()

	users, err := st.ListUsersBrief()
	if err != nil {
		die("list users: %v", err)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(users)
		return
	}

	fmt.Printf("%-4s  %-20s  %-10s  %-10s  %s\n", "ID", "USERNAME", "TOTP", "MUST_CHG", "CREATED")
	fmt.Println(strings.Repeat("-", 72))
	for _, u := range users {
		totp := "no"
		if u.HasTOTP {
			totp = "yes"
		}
		mc := "no"
		if u.MustChangePassword {
			mc = "yes"
		}
		fmt.Printf("%-4d  %-20s  %-10s  %-10s  %s\n", u.ID, u.Username, totp, mc, u.CreatedAt.Format("2006-01-02 15:04"))
	}
}

// ---- ops-panel admin reset-password ----

func adminResetPassword(args []string) {
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config.json")
	dd := fs.String("data-dir", "", "data directory")
	username := fs.String("user", "admin", "username to reset")
	pwFlag := fs.String("password", "", "new password (if omitted, read from stdin)")
	mustChange := fs.Bool("must-change", false, "force password change on next login")
	_ = fs.Parse(args)

	st, _ := openStoreFromFlags(*cfgPath, *dd)
	defer st.Close()

	u, err := st.GetUserByName(storage.NormalizeUsername(*username))
	if err != nil {
		die("lookup user: %v", err)
	}
	if u == nil {
		die("user %q not found", *username)
	}

	pw := *pwFlag
	if pw == "" {
		isTTY := term.IsTerminal(int(os.Stdin.Fd()))
		pw = promptSecret("New password for " + u.Username + ": ")
		if isTTY {
			confirm := promptSecret("Confirm password: ")
			if pw != confirm {
				die("passwords do not match")
			}
		}
	}
	if len(pw) < 12 {
		die("password must be at least 12 characters")
	}

	hash, err := auth.HashPassword(pw)
	if err != nil {
		die("hash password: %v", err)
	}
	if err := st.UpdatePassword(u.ID, hash); err != nil {
		die("update password: %v", err)
	}
	if *mustChange {
		if err := st.SetMustChangePassword(u.ID, true); err != nil {
			die("set must_change: %v", err)
		}
	}
	fmt.Printf("password updated for user %q\n", u.Username)
	if *mustChange {
		fmt.Println("user will be forced to change password on next login")
	}
}

// ---- ops-panel admin reset-totp ----

func adminResetTOTP(args []string) {
	fs := flag.NewFlagSet("reset-totp", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config.json")
	dd := fs.String("data-dir", "", "data directory")
	username := fs.String("user", "admin", "username")
	yes := fs.Bool("yes", false, "skip confirmation")
	_ = fs.Parse(args)

	st, _ := openStoreFromFlags(*cfgPath, *dd)
	defer st.Close()

	u, err := st.GetUserByName(storage.NormalizeUsername(*username))
	if err != nil {
		die("lookup user: %v", err)
	}
	if u == nil {
		die("user %q not found", *username)
	}
	if u.TOTPSecret == "" {
		fmt.Printf("user %q already has no TOTP secret — nothing to do\n", u.Username)
		return
	}

	if !*yes {
		fmt.Printf("This will UNBIND the Authenticator for %q. Next login will require only a password.\n", u.Username)
		fmt.Print("Type 'yes' to continue: ")
		r := bufio.NewReader(os.Stdin)
		line, _ := r.ReadString('\n')
		if strings.TrimSpace(line) != "yes" {
			fmt.Println("aborted")
			return
		}
	}

	if err := st.UpdateTOTPSecret(u.ID, ""); err != nil {
		die("clear TOTP: %v", err)
	}
	fmt.Printf("TOTP cleared for user %q. They can re-bind from the Account page after logging in.\n", u.Username)
}

// ---- helpers ----

func promptSecret(prompt string) string {
	// If stdin is a terminal, use no-echo. Otherwise read a line
	// (supports `echo newpw | ops-panel admin reset-password`).
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			die("read password: %v", err)
		}
		return string(b)
	}
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		die("read password: %v", err)
	}
	return strings.TrimRight(line, "\r\n")
}
