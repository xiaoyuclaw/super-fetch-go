package xfetch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"
)

// FindSystemChrome 找系统真实 Chrome（nodriver 通道用它，而不是 Playwright 的 Chromium）。
func FindSystemChrome() string {
	candidates := []string{
		os.Getenv("CHROME_PATH"),
		"/usr/bin/google-chrome",
		"/opt/google/chrome/chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	}
	for _, c := range candidates {
		if c != "" {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	return ""
}

// FetchWithNodriver 启动真实 Chrome（CDP 远程调试端口，不带自动化参数）抓一页。
func FetchWithNodriver(rawURL, sessionFile string, opts BrowserOptions) (string, string, error) {
	exe := FindSystemChrome()
	if exe == "" {
		return "", "", errors.New("找不到系统 Chrome（可用 CHROME_PATH 指定）")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30
	}
	profile, err := os.MkdirTemp("", "sf-nd-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(profile)

	args := []string{
		"--remote-debugging-port=0",
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-blink-features=AutomationControlled",
		"--disable-dev-shm-usage",
		"--no-sandbox",
		"--window-position=-2400,-2400",
		"--window-size=1280,900",
		"--lang=zh-CN",
	}
	if strings.EqualFold(os.Getenv("SF_HEADLESS"), "true") {
		args = append(args, "--headless=new")
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return "", "", err
	}
	// 退出时连同子进程一起收掉，避免残留
	defer func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		_ = cmd.Wait()
	}()

	port, err := waitDevToolsPort(profile, 20*time.Second)
	if err != nil {
		return "", "", err
	}
	ws, err := wsEndpoint(port)
	if err != nil {
		return "", "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.Timeout+30)*time.Second)
	defer cancel()
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ctx, ws)
	defer allocCancel()
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()
	tabCtx, timeoutCancel := context.WithTimeout(tabCtx, time.Duration(opts.Timeout)*time.Second)
	defer timeoutCancel()

	var html string
	actions := []chromedp.Action{
		chromedp.Navigate(rawURL),
		chromedp.Sleep(1200 * time.Millisecond),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	}
	if err := chromedp.Run(tabCtx, actions...); err != nil {
		return "", "", err
	}
	return html, "text/html", nil
}

func waitDevToolsPort(profile string, d time.Duration) (string, error) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) > 0 && lines[0] != "" {
				return lines[0], nil
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	return "", errors.New("等待 Chrome 调试端口超时")
}

func wsEndpoint(port string) (string, error) {
	resp, err := http.Get("http://127.0.0.1:" + port + "/json/version")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var v struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(bufio.NewReader(resp.Body)).Decode(&v); err != nil {
		return "", err
	}
	if v.WS == "" {
		return "", fmt.Errorf("拿不到 webSocketDebuggerUrl")
	}
	return v.WS, nil
}
