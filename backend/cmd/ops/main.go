// ops — ops-panel 命令行工具
//
// 安装: cd backend && go install ./cmd/ops
// 用法: ops {dev|build|run|totp|help}
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pquerna/otp/totp"
)

const usage = `ops — ops-panel CLI

用法:
  ops dev              启动开发模式 (后端 127.0.0.1:8443 + Vite 5173)
  ops build            生产构建到 dist/ (默认 linux/amd64, 可 GOOS/GOARCH 覆盖)
  ops run              用 dist/ 产物本地跑一次 (验证构建能启动)
  ops totp <secret>    根据 TOTP secret 算当前 6 位验证码 (dev 用)
  ops help             显示本帮助

示例:
  ops dev
  GOOS=linux GOARCH=arm64 ops build
  ops totp ALC35PNBRZMBJXDSVDVK7E55N3NJ5WTK
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "dev":
		cmdDev()
	case "build":
		cmdBuild()
	case "run":
		cmdRun()
	case "totp":
		cmdTotp(os.Args[2:])
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

// ---- shared helpers ----

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "错误: "+format+"\n", a...)
	os.Exit(1)
}

func findRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		die("%v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "backend", "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "frontend", "package.json")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			die("找不到 ops-panel 根目录 (需要 backend/go.mod + frontend/package.json). 请 cd 到仓库里再跑 ops")
		}
		dir = parent
	}
}

func preflight(tools ...string) {
	for _, t := range tools {
		if _, err := exec.LookPath(t); err != nil {
			hint := ""
			switch t {
			case "go":
				hint = "  安装: https://go.dev/dl/"
			case "node":
				hint = "  安装: https://nodejs.org/  (>= 20)"
			case "pnpm":
				hint = "  安装: corepack enable  (或 npm i -g pnpm)"
			}
			die("缺少 %s\n%s", t, hint)
		}
	}
}

func binExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func mustRun(dir, name string, args ...string) {
	c := exec.Command(name, args...)
	if dir != "" {
		c.Dir = dir
	}
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	if err := c.Run(); err != nil {
		die("%s %s 失败: %v", name, strings.Join(args, " "), err)
	}
}

// ---- ops dev ----

func cmdDev() {
	root := findRoot()
	preflight("go", "node", "pnpm")

	if _, err := os.Stat(filepath.Join(root, "frontend", "node_modules")); errors.Is(err, fs.ErrNotExist) {
		fmt.Println("[dev] 首次运行, 安装前端依赖...")
		mustRun(filepath.Join(root, "frontend"), "pnpm", "install")
	}

	binPath := filepath.Join(root, ".dev", "panel"+binExt())
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		die("mkdir .dev: %v", err)
	}

	fmt.Println("[dev] 编译后端...")
	mustRun(filepath.Join(root, "backend"), "go", "build", "-o", binPath, "./cmd/panel")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	fmt.Print(`
╭──────────────────────────────────────────────╮
│  ops dev 已启动                              │
│                                              │
│  前端:       http://localhost:5173           │
│  后端 API:   http://127.0.0.1:8443/api       │
│                                              │
│  首次运行请看下方日志, 获取 admin 初始密码   │
│  和 TOTP 种子 (或看 ~/.ops-panel/FIRST_*.txt)│
│                                              │
│  Ctrl+C 停止                                 │
╰──────────────────────────────────────────────╯

`)

	devData := filepath.Join(root, ".dev", "data")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		runStreaming(ctx, root, "backend", binPath,
			"-listen", "127.0.0.1:8443",
			"-dev",
			"-data-dir", devData,
		)
	}()
	go func() {
		defer wg.Done()
		runStreaming(ctx, filepath.Join(root, "frontend"), "frontend", "pnpm", "dev")
	}()
	wg.Wait()
	fmt.Println("[dev] 全部已停止。")
}

var outMu sync.Mutex

func runStreaming(ctx context.Context, dir, label, name string, args ...string) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Cancel = func() error {
		if runtime.GOOS == "windows" {
			// pnpm.cmd 会起 node 子进程,得杀整棵树
			_ = exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprint(c.Process.Pid)).Run()
			return nil
		}
		return c.Process.Signal(syscall.SIGTERM)
	}
	c.WaitDelay = 5 * time.Second

	stdout, _ := c.StdoutPipe()
	stderr, _ := c.StderrPipe()
	if err := c.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] 启动失败: %v\n", label, err)
		return
	}
	go prefixLines(label, stdout, os.Stdout)
	go prefixLines(label, stderr, os.Stderr)

	err := c.Wait()
	if err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "[%s] 异常退出: %v\n", label, err)
	}
}

func prefixLines(label string, r io.Reader, w io.Writer) {
	color := map[string]string{"backend": "\x1b[36m", "frontend": "\x1b[35m"}[label]
	reset := "\x1b[0m"
	if color == "" {
		reset = ""
	}
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		outMu.Lock()
		fmt.Fprintf(w, "%s[%s]%s %s\n", color, label, reset, s.Text())
		outMu.Unlock()
	}
}

// ---- ops build ----

func cmdBuild() {
	root := findRoot()
	preflight("go", "node", "pnpm")

	goos := getenvDefault("GOOS", "linux")
	goarch := getenvDefault("GOARCH", "amd64")
	fmt.Printf("[build] target: %s/%s\n", goos, goarch)

	dist := filepath.Join(root, "dist")
	_ = os.RemoveAll(dist)
	if err := os.MkdirAll(dist, 0o755); err != nil {
		die("mkdir dist: %v", err)
	}

	fmt.Println("[build] frontend...")
	mustRun(filepath.Join(root, "frontend"), "pnpm", "install", "--frozen-lockfile")
	mustRun(filepath.Join(root, "frontend"), "pnpm", "build")
	if err := copyDir(filepath.Join(root, "frontend", "dist"), filepath.Join(dist, "frontend")); err != nil {
		die("copy frontend: %v", err)
	}

	fmt.Println("[build] backend...")
	binName := "ops-panel"
	if goos == "windows" {
		binName = "ops-panel.exe"
	}
	c := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", filepath.Join(dist, binName), "./cmd/panel")
	c.Dir = filepath.Join(root, "backend")
	c.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		die("go build: %v", err)
	}

	_ = copyDir(filepath.Join(root, "scripts"), filepath.Join(dist, "scripts"))
	_ = copyFile(filepath.Join(root, "scripts", "ops-panel.service"), filepath.Join(dist, "ops-panel.service"))

	fmt.Println("\n[build] 完成. dist/:")
	_ = filepath.WalkDir(dist, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dist, p)
		info, _ := d.Info()
		fmt.Printf("  %s  (%s)\n", rel, humanSize(info.Size()))
		return nil
	})
	fmt.Println("\n上传 dist/ 到服务器, 参考 README.md 的 '生产部署' 章节.")
}

// ---- ops run ----

func cmdRun() {
	root := findRoot()
	binName := "ops-panel" + binExt()
	bin := filepath.Join(root, "dist", binName)
	if _, err := os.Stat(bin); err != nil {
		die("找不到 %s, 先跑 `ops build` (本机目标: GOOS=%s GOARCH=%s ops build)", bin, runtime.GOOS, runtime.GOARCH)
	}
	fmt.Printf("[run] %s\n", bin)
	c := exec.Command(bin, "-frontend", filepath.Join(root, "dist", "frontend"))
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	_ = c.Run()
}

// ---- ops totp ----

func cmdTotp(args []string) {
	if len(args) < 1 {
		die("用法: ops totp <secret>")
	}
	code, err := totp.GenerateCode(args[0], time.Now())
	if err != nil {
		die("%v", err)
	}
	fmt.Println(code)
}

// ---- utils ----

func getenvDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
}

func humanSize(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	e := 0
	v := float64(n)
	for v >= u && e < 4 {
		v /= u
		e++
	}
	return fmt.Sprintf("%.1f %cB", v, "KMGT"[e-1])
}
