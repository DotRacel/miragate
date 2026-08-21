package authx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"
)

// LoginResult 是一次登录得到的令牌与显示名。
type LoginResult struct {
	Token        string
	RefreshToken string
	DisplayName  string
	Email        string
}

// EmailLogin 执行邮箱验证码登录：先请求验证码，再用回调获取用户输入的验证码校验。
// getCode 由调用方提供（例如从终端读取一行）。
func (c *Client) EmailLogin(ctx context.Context, email string, getCode func(devCode string) (string, error)) (*LoginResult, error) {
	devCode, err := c.RequestEmailCode(ctx, email)
	if err != nil {
		return nil, err
	}
	code, err := getCode(devCode)
	if err != nil {
		return nil, err
	}
	tok, err := c.VerifyEmailCode(ctx, email, code)
	if err != nil {
		return nil, err
	}
	return c.finish(ctx, tok)
}

// OAuthLogin 执行浏览器 OAuth 登录：在本地起临时 /callback 服务，打开浏览器，
// 等待 auth.mirasim.ai 携带 access_token/refresh_token 回调。
// 对应 server.cjs 的 kKe 函数。timeout<=0 时默认 5 分钟。
func (c *Client) OAuthLogin(ctx context.Context, provider string, timeout time.Duration, open func(url string)) (*LoginResult, error) {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	callback := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	type outcome struct {
		res *LoginResult
		err error
	}
	done := make(chan outcome, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		token := firstNonEmpty(q.Get("access_token"), q.Get("token"))
		refresh := q.Get("refresh_token")
		if token == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(resultPage(pageData{Title: "登录失败", Detail: "未收到 token，请回到终端重试。"}))
			done <- outcome{err: errors.New("callback missing token")}
			return
		}
		id := DecodeJWT(token)
		me, _ := c.Me(r.Context(), token)
		name, emailAddr := "", ""
		if me != nil {
			name, emailAddr = me.Name, me.Email
		}
		if emailAddr == "" && id != nil {
			emailAddr = id.Email
		}
		pd := pageData{OK: true, Title: "登录成功", Detail: "可以关闭此页面，回到终端继续。", Name: name}
		if pd.Name == "" {
			pd.Name = emailAddr
		}
		pd.Mail = emailAddr
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(resultPage(pd))
		display := name
		if display == "" {
			display = firstNonEmpty(emailAddr, "")
		}
		done <- outcome{res: &LoginResult{Token: token, RefreshToken: refresh, DisplayName: display, Email: emailAddr}}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	loginURL := c.OAuthLoginURL(provider, callback)
	if open != nil {
		open(loginURL)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, &AuthError{Message: "browser sign-in timed out", Reason: "timeout"}
	case o := <-done:
		if o.err != nil {
			return nil, o.err
		}
		return c.finish(ctx, &Tokens{AccessToken: o.res.Token, RefreshToken: o.res.RefreshToken})
	}
}

// finish 在拿到令牌后补齐显示名/邮箱（调用 /auth/me）。
func (c *Client) finish(ctx context.Context, tok *Tokens) (*LoginResult, error) {
	res := &LoginResult{Token: tok.AccessToken, RefreshToken: tok.RefreshToken}
	if id := DecodeJWT(tok.AccessToken); id != nil {
		res.Email = id.Email
	}
	mctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if me, err := c.Me(mctx, tok.AccessToken); err == nil && me != nil {
		if me.Name != "" {
			res.DisplayName = me.Name
		}
		if me.Email != "" {
			res.Email = me.Email
		}
	}
	if res.DisplayName == "" {
		res.DisplayName = res.Email
	}
	return res, nil
}

// OpenBrowser 尽力在系统默认浏览器中打开 URL。
func OpenBrowser(target string) {
	if _, err := url.Parse(target); err != nil {
		return
	}
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{target}
	case "windows":
		cmd, args = "cmd", []string{"/c", "start", "", target}
	default:
		cmd, args = "xdg-open", []string{target}
	}
	c := exec.Command(cmd, args...)
	_ = c.Start()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
