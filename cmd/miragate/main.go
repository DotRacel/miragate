// Command miragate 是 Mirasim 桌面端鉴权 + 本地反代 + 用量页的 Go 复刻。
//
// 子命令：
//
//	miragate login     交互式登录（邮箱验证码或 OAuth 浏览器登录）
//	miragate serve     启动本地反代（供 CLI 设置 ANTHROPIC_BASE_URL）与用量页
//	miragate status    查看登录态与最近用量
//	miragate whoami    打印当前身份
//	miragate logout    退出登录
//	miragate env       打印供 shell/CLI 使用的环境变量
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"miragate/internal/authx"
	"miragate/internal/config"
	"miragate/internal/device"
	"miragate/internal/httpx"
	"miragate/internal/relay"
	"miragate/internal/tokens"
	"miragate/internal/usage"
	"miragate/internal/web"
)

// clientVer 作为 x-mirasim-client 头，与被复刻的桌面端版本一致。
const clientVer = "0.0.214"

func main() {
	// 让 ALL_PROXY 回退为 HTTP(S)_PROXY，统一出站代理（http/socks5）。须在任何请求前。
	httpx.NormalizeProxyEnv()

	if len(os.Args) < 2 {
		usageText()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "login":
		err = cmdLogin(args)
	case "serve":
		err = cmdServe(args)
	case "status":
		err = cmdStatus(args)
	case "whoami":
		err = cmdWhoami(args)
	case "logout":
		err = cmdLogout(args)
	case "env":
		err = cmdEnv(args)
	case "-h", "--help", "help":
		usageText()
		return
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n\n", cmd)
		usageText()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func usageText() {
	fmt.Fprint(os.Stderr, `miragate — Mirasim 鉴权/反代/用量 的 Go 复刻

用法:
  miragate login [--email <addr>] [--oauth <provider>]
  miragate serve [--listen 127.0.0.1:8788]
  miragate status
  miragate whoami
  miragate logout
  miragate env [--listen 127.0.0.1:8788]

登录后运行 serve，然后让 CLI 使用本地反代：
  export ANTHROPIC_BASE_URL=http://127.0.0.1:8788
  export ANTHROPIC_AUTH_TOKEN=mirasim   # 占位即可，真实凭据由 miragate 注入
用量页: http://127.0.0.1:8788/
`)
}

// build 组装运行所需的各组件。
func build() (*config.Config, *authx.Client, *tokens.Manager, *device.Key, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	auth := authx.NewClient(cfg.AuthBaseURL())

	var pemStr string
	if cfg.Device != nil {
		pemStr = cfg.Device.PrivateKey
	}
	key, kerr := device.LoadOrCreate(pemStr, func(newPEM string) error {
		cfg.Device = &config.Device{PrivateKey: newPEM}
		return config.Save(cfg)
	})
	if kerr != nil {
		key = nil // 设备签名不可用则回退纯令牌
	}

	var ticket *device.TicketManager
	if key != nil {
		ticket = device.NewTicketManager(key, cfg.RelayBaseURL(), clientVer, func() string {
			if cfg.Auth != nil {
				return cfg.Auth.Token
			}
			return ""
		})
	}
	tm := tokens.New(cfg, auth, key, ticket)
	return cfg, auth, tm, key, nil
}

func cmdLogin(args []string) error {
	fs := newFlags(args)
	email := fs.str("email")
	provider := fs.str("oauth")

	cfg, auth, tm, _, err := build()
	if err != nil {
		return err
	}
	ctx := context.Background()

	// 未指定方式则交互选择。
	if email == "" && provider == "" {
		providers, _ := auth.OAuthProviders(ctx)
		fmt.Println("请选择登录方式:")
		fmt.Println("  1) 邮箱验证码")
		if len(providers) > 0 {
			fmt.Printf("  2) OAuth (%s)\n", strings.Join(providers, ", "))
		}
		fmt.Print("输入序号 (默认 1): ")
		choice := readLine()
		if choice == "2" && len(providers) > 0 {
			if len(providers) == 1 {
				provider = providers[0]
			} else {
				fmt.Printf("选择 provider (%s): ", strings.Join(providers, "/"))
				provider = strings.TrimSpace(readLine())
			}
		} else {
			fmt.Print("邮箱地址: ")
			email = strings.TrimSpace(readLine())
		}
	}

	var res *authx.LoginResult
	switch {
	case provider != "":
		fmt.Printf("正在通过浏览器进行 OAuth 登录 (%s)…\n", provider)
		res, err = auth.OAuthLogin(ctx, provider, 5*time.Minute, func(u string) {
			fmt.Println("  若浏览器未自动打开，请手动访问:")
			fmt.Println("  " + u)
			authx.OpenBrowser(u)
		})
	case email != "":
		res, err = auth.EmailLogin(ctx, email, func(devCode string) (string, error) {
			if devCode != "" {
				fmt.Printf("  (dev 模式验证码: %s)\n", devCode)
			}
			fmt.Print("请输入邮箱收到的验证码: ")
			return strings.TrimSpace(readLine()), nil
		})
	default:
		return fmt.Errorf("未提供登录方式")
	}
	if err != nil {
		return err
	}
	if err := tm.Save(res); err != nil {
		return err
	}
	name := res.DisplayName
	if name == "" {
		name = res.Email
	}
	fmt.Printf("✓ 登录成功: %s\n", name)
	if id := authx.DecodeJWT(res.Token); id != nil && id.Plan != "" {
		fmt.Printf("  套餐: %s  租户: %s\n", id.Plan, id.Tenant)
	}
	_ = cfg
	return nil
}

func cmdServe(args []string) error {
	fs := newFlags(args)
	cfg, _, tm, key, err := build()
	if err != nil {
		return err
	}
	if !tm.LoggedIn() {
		return fmt.Errorf("尚未登录，请先运行 `miragate login`")
	}
	listen := fs.str("listen")
	if listen == "" {
		listen = cfg.ListenAddr()
	}

	deviceID := device.PersistentID(config.Home())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 用量轮询器。
	poller := usage.NewPoller(cfg.RelayBaseURL(), clientVer, deviceID, tm, 60*time.Second)
	go poller.Run(ctx)

	// 凭据/设备票据的后台保鲜。
	go func() {
		tm.EnsureFresh(ctx)
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tm.EnsureFresh(ctx)
			}
		}
	}()

	// 反代。
	proxy := relay.New(relay.Options{
		RelayBase: cfg.RelayBaseURL(),
		Agent:     "claude",
		ClientVer: clientVer,
		DeviceID:  deviceID,
		Creds:     tm,
	})

	// 用量页。
	webSrv := web.New(tm, poller, listen)
	webMux := newServeMux()
	webSrv.Register(webMux)

	handler := dispatcher(webMux, proxy)
	srv := newHTTPServer(listen, handler)

	fmt.Printf("miragate 已启动:\n")
	fmt.Printf("  反代 (ANTHROPIC_BASE_URL): http://%s\n", listen)
	fmt.Printf("  用量页:                    http://%s/\n", listen)
	if key != nil {
		fmt.Printf("  设备签名: 启用 (device %s)\n", key.DeviceID())
	} else {
		fmt.Printf("  设备签名: 未启用（使用纯令牌）\n")
	}
	fmt.Printf("按 Ctrl+C 退出。\n")

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	if err := srv.ListenAndServe(); err != nil && err.Error() != "http: Server closed" {
		return err
	}
	return nil
}

func cmdStatus(args []string) error {
	cfg, _, tm, _, err := build()
	if err != nil {
		return err
	}
	snap := tm.Snapshot()
	if snap == nil {
		fmt.Println("未登录。运行 `miragate login`。")
		return nil
	}
	fmt.Println("登录态:")
	fmt.Printf("  用户: %s\n", firstNonEmpty(snap.Name, snap.Email, snap.Token[:12]+"…"))
	if snap.Email != "" {
		fmt.Printf("  邮箱: %s\n", snap.Email)
	}
	if snap.Plan != "" {
		fmt.Printf("  套餐: %s\n", snap.Plan)
	}
	if snap.Tenant != "" {
		fmt.Printf("  租户: %s\n", snap.Tenant)
	}
	if snap.Exp > 0 {
		fmt.Printf("  令牌到期: %s\n", time.Unix(snap.Exp, 0).Local().Format("2006-01-02 15:04:05"))
	}
	fmt.Printf("  设备签名: %v (device %s)\n", snap.HasDeviceKey, snap.DeviceID)
	fmt.Printf("  relay: %s\n", cfg.RelayBaseURL())

	// 拉一次实时用量。
	deviceID := device.PersistentID(config.Home())
	poller := usage.NewPoller(cfg.RelayBaseURL(), clientVer, deviceID, tm, time.Minute)
	u := poller.Refresh(context.Background())
	fmt.Println("用量:")
	if u == nil || !u.OK {
		msg := "不可用"
		if u != nil && u.Error != "" {
			msg = u.Error
		}
		fmt.Printf("  %s\n", msg)
		return nil
	}
	for _, w := range u.Windows {
		reset := ""
		if w.ResetAfterSeconds != nil {
			reset = fmt.Sprintf("  (%s 后重置)", humanDur(*w.ResetAfterSeconds))
		}
		fmt.Printf("  %-6s 已用 %.1f%%  剩余 %.1f%%  [%s]%s\n", w.Label, w.UsedPercent, w.RemainingPercent, w.Status, reset)
	}
	return nil
}

func cmdWhoami(args []string) error {
	_, _, tm, _, err := build()
	if err != nil {
		return err
	}
	snap := tm.Snapshot()
	if snap == nil {
		fmt.Println("未登录")
		return nil
	}
	fmt.Println(firstNonEmpty(snap.Email, snap.Name, snap.Token))
	return nil
}

func cmdLogout(args []string) error {
	_, _, tm, _, err := build()
	if err != nil {
		return err
	}
	if err := tm.Logout(); err != nil {
		return err
	}
	fmt.Println("已退出登录。")
	return nil
}

func cmdEnv(args []string) error {
	fs := newFlags(args)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	listen := fs.str("listen")
	if listen == "" {
		listen = cfg.ListenAddr()
	}
	fmt.Printf("export ANTHROPIC_BASE_URL=http://%s\n", listen)
	fmt.Printf("export ANTHROPIC_AUTH_TOKEN=mirasim\n")
	return nil
}

// --- helpers ---

func readLine() string {
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func humanDur(sec int64) string {
	if sec <= 0 {
		return "0s"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	if h >= 24 {
		return fmt.Sprintf("%dd%dh", h/24, h%24)
	}
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// flags 是极简的 --key value 解析器。
type flags struct{ m map[string]string }

func newFlags(args []string) *flags {
	f := &flags{m: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			key := strings.TrimPrefix(a, "--")
			if eq := strings.IndexByte(key, '='); eq >= 0 {
				f.m[key[:eq]] = key[eq+1:]
				continue
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				f.m[key] = args[i+1]
				i++
			} else {
				f.m[key] = "true"
			}
		}
	}
	return f
}

func (f *flags) str(key string) string { return f.m[key] }
