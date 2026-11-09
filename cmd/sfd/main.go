// sfd 是 gomoufox 的常驻浏览器 daemon：启动一次浏览器，sf 通过 unix socket 复用它。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"
	gomoufox "github.com/ehmo/gomoufox"
	"github.com/ehmo/gomoufox/camoufoxcfg"
)

type request struct {
	URL     string  `json:"url"`
	Session string  `json:"session"`
	Timeout int     `json:"timeout"`
	Wait    float64 `json:"wait"`
}

type response struct {
	HTML  string `json:"html"`
	Error string `json:"error"`
}

func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "[sfd] "+format+"\n", a...)
}

func dshAlive(pid int) bool {
	if pid <= 0 {
		return true
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func main() {
	sock := flag.String("sock", defaultSock(), "unix socket path")
	dshPID := flag.Int("dsh-pid", 0, "dsh host pid; daemon exits when it dies")
	idle := flag.Int("idle", 1800, "idle timeout seconds")
	backend := flag.String("backend", "gomoufox", "gomoufox|chromedp")
	flag.Parse()

	// 单实例锁
	lockPath := *sock + ".lock"
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		logf("打开锁失败: %v", err)
		return
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		logf("已有 daemon 在跑，退出")
		return
	}
	defer lf.Close()

	_ = os.Remove(*sock)
	ln, err := net.Listen("unix", *sock)
	if err != nil {
		logf("监听失败: %v", err)
		return
	}
	_ = os.Chmod(*sock, 0o600)
	logf("监听 %s（dsh_pid=%d, idle=%ds）", *sock, *dshPID, *idle)

	ctx := context.Background()

	if *backend == "chromedp" {
		serveChromedp(ctx, *sock, *dshPID, *idle, ln)
		return
	}

	var newOpts []gomoufox.Option
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SF_HEADLESS"))) {
	case "false":
		newOpts = append(newOpts, gomoufox.WithHeadless(camoufoxcfg.HeadlessFalse))
	case "true":
		newOpts = append(newOpts, gomoufox.WithHeadless(camoufoxcfg.HeadlessTrue))
	default:
		newOpts = append(newOpts, gomoufox.WithHeadless(camoufoxcfg.HeadlessVirtual))
	}
	browser, err := gomoufox.New(ctx, newOpts...)
	if err != nil {
		logf("浏览器启动失败: %v", err)
		return
	}
	defer browser.Close()
	logf("浏览器已常驻")

	lastUsed := time.Now()
	go func() {
		for {
			time.Sleep(10 * time.Second)
			if !dshAlive(*dshPID) {
				logf("DSH 已退出，关闭")
				os.Exit(0)
			}
			if int(time.Since(lastUsed).Seconds()) > *idle {
				logf("空闲超时，关闭")
				os.Exit(0)
			}
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		lastUsed = time.Now()
		go func(c net.Conn) {
			defer c.Close()
			line, err := bufio.NewReader(c).ReadBytes('\n')
			if err != nil {
				return
			}
			var req request
			if err := json.Unmarshal(line, &req); err != nil {
				writeJSON(c, response{Error: "bad request"})
				return
			}
			html, err := fetchPage(ctx, browser, req)
			if err != nil {
				writeJSON(c, response{Error: err.Error()})
			} else {
				writeJSON(c, response{HTML: html})
			}
			lastUsed = time.Now()
		}(conn)
	}
}

func fetchPage(ctx context.Context, b *gomoufox.Browser, req request) (string, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	opts := []gomoufox.ContextOption{
		gomoufox.WithViewport(1280, 900),
		gomoufox.WithContextLocale("zh-CN"),
		gomoufox.WithTimezoneID("Asia/Shanghai"),
	}
	if req.Session != "" {
		if data, err := os.ReadFile(req.Session); err == nil {
			var st gomoufox.StorageState
			if json.Unmarshal(data, &st) == nil {
				opts = append(opts, gomoufox.WithStorageState(&st))
			}
		}
	}
	page, err := b.NewPage(ctx, opts...)
	if err != nil {
		return "", err
	}
	defer page.Close()

	if _, err := page.Goto(ctx, req.URL,
		gomoufox.WaitUntil("domcontentloaded"),
		gomoufox.WithTimeout(time.Duration(timeout)*time.Second)); err != nil {
		return "", err
	}
	if req.Wait > 0 {
		time.Sleep(time.Duration(req.Wait * float64(time.Second)))
	}
	html, err := page.Content(ctx)
	if err != nil {
		return "", err
	}
	if req.Session != "" {
		if cookies, err := page.Cookies(ctx); err == nil && len(cookies) > 0 {
			st := gomoufox.StorageState{Cookies: cookies}
			if data, err := json.Marshal(st); err == nil {
				_ = os.WriteFile(req.Session, data, 0o600)
			}
		}
	}
	return html, nil
}

func writeJSON(c net.Conn, v response) {
	data, _ := json.Marshal(v)
	_, _ = c.Write(append(data, '\n'))
}

func defaultSock() string {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/tmp"
	}
	return filepath.Join(runtimeDir, fmt.Sprintf("sf-gomoufox-%d.sock", os.Getuid()))
}

// serveChromedp 用 chromedp 跑常驻浏览器。
func serveChromedp(ctx context.Context, sock string, dshPID, idle int, ln net.Listener) {
	defer ln.Close()
	exe := os.Getenv("CHROME_PATH")
	if exe == "" {
		home, _ := os.UserHomeDir()
		for _, pat := range []string{
			filepath.Join(home, ".cache/ms-playwright/chromium-*/chrome-linux/chrome"),
			filepath.Join(home, ".cache/ms-playwright/chromium-*/chrome-linux64/chrome"),
		} {
			if hits, _ := filepath.Glob(pat); len(hits) > 0 {
				exe = hits[0]
				break
			}
		}
	}
	flags := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(exe),
		chromedp.Flag("headless", os.Getenv("SF_HEADLESS") == "true"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("window-position", "-2400,-2400"),
		chromedp.Flag("window-size", "1280,900"),
		chromedp.Flag("lang", "zh-CN"),
		chromedp.UserDataDir(""),
	}
	allocCtx, allocStop := chromedp.NewExecAllocator(context.Background(), flags...)
	defer allocStop()
	browserCtx, browserStop := chromedp.NewContext(allocCtx)
	defer browserStop()
	if err := chromedp.Run(browserCtx); err != nil {
		logf("chromedp 启动失败: %v", err)
		return
	}
	logf("chromedp 浏览器已常驻")

	lastUsed := time.Now()
	go func() {
		for {
			time.Sleep(10 * time.Second)
			if !dshAlive(dshPID) {
				logf("DSH 已退出，关闭")
				os.Exit(0)
			}
			if int(time.Since(lastUsed).Seconds()) > idle {
				logf("空闲超时，关闭")
				os.Exit(0)
			}
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		lastUsed = time.Now()
		go func(c net.Conn) {
			defer c.Close()
			line, err := bufio.NewReader(c).ReadBytes('\n')
			if err != nil {
				return
			}
			var req request
			if json.Unmarshal(line, &req) != nil {
				writeJSON(c, response{Error: "bad request"})
				return
			}
			html, err := fetchChromedp(ctx, browserCtx, req)
			if err != nil {
				writeJSON(c, response{Error: err.Error()})
			} else {
				writeJSON(c, response{HTML: html})
			}
			lastUsed = time.Now()
		}(conn)
	}
}

func fetchChromedp(ctx context.Context, browserCtx context.Context, req request) (string, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	tabCtx, cancel := chromedp.NewContext(browserCtx)
	defer cancel()
	tabCtx, cancelTO := context.WithTimeout(tabCtx, time.Duration(timeout+20)*time.Second)
	defer cancelTO()
	var html string
	err := chromedp.Run(tabCtx,
		chromedp.Navigate(req.URL),
		chromedp.Sleep(time.Duration(req.Wait*float64(time.Second))),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	)
	return html, err
}
