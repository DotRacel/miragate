package authx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"miragate/internal/httpx"
)

// Client 封装对 auth.mirasim.ai 的调用。
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient 构造鉴权客户端。baseURL 例如 https://auth.mirasim.ai。
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    httpx.Client(30 * time.Second),
	}
}

// Tokens 是登录/刷新返回的令牌对。
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// Me 是 /auth/me 返回的用户档案。
type Me struct {
	Email   string
	Name    string
	Plan    string
	PlanExp *int64
}

// AuthError 携带服务端返回的错误详情与状态码。
type AuthError struct {
	Message string
	Status  int
	Reason  string
}

func (e *AuthError) Error() string { return e.Message }

// httpErr 从失败响应构造 AuthError，尽量提取 body 中的 detail。
func httpErr(resp *http.Response, reason string) error {
	msg := ""
	var body struct {
		Detail string `json:"detail"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if json.Unmarshal(data, &body) == nil && body.Detail != "" {
		msg = body.Detail
	}
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return &AuthError{Message: msg, Status: resp.StatusCode, Reason: reason}
}

// RequestEmailCode 对应 POST /auth/code。触发向邮箱发送验证码。
// 若服务端处于 dev 模式，返回的 devCode 非空。
func (c *Client) RequestEmailCode(ctx context.Context, email string) (devCode string, err error) {
	resp, err := c.postJSON(ctx, "/auth/code", map[string]any{"email": email}, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", httpErr(resp, "http")
	}
	var out struct {
		DevCode string `json:"dev_code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.DevCode, nil
}

// VerifyEmailCode 对应 POST /auth/verify。用邮箱+验证码换取令牌。
func (c *Client) VerifyEmailCode(ctx context.Context, email, code string) (*Tokens, error) {
	resp, err := c.postJSON(ctx, "/auth/verify", map[string]any{"email": email, "code": code}, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, httpErr(resp, "http")
	}
	var t Tokens
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, &AuthError{Message: "sign-in response carried no access_token", Reason: "bad-response"}
	}
	return &t, nil
}

// Refresh 对应 POST /auth/refresh。用 refresh_token 换取新令牌。
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	resp, err := c.postJSON(ctx, "/auth/refresh", map[string]any{"refresh_token": refreshToken}, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, httpErr(resp, "http")
	}
	var t Tokens
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, &AuthError{Message: "refresh response carried no access_token", Reason: "bad-response"}
	}
	return &t, nil
}

// Me 对应 GET /auth/me。返回当前令牌对应的用户档案。
func (c *Client) Me(ctx context.Context, token string) (*Me, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/auth/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, httpErr(resp, "http")
	}
	var raw struct {
		Email   string   `json:"email"`
		Name    string   `json:"name"`
		Plan    string   `json:"plan"`
		PlanExp *float64 `json:"plan_exp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	m := &Me{Email: raw.Email, Name: strings.TrimSpace(raw.Name), Plan: raw.Plan}
	if raw.PlanExp != nil {
		v := int64(*raw.PlanExp)
		m.PlanExp = &v
	}
	return m, nil
}

// OAuthProviders 对应 GET /auth/oauth/providers。返回可用的第三方登录方式。
func (c *Client) OAuthProviders(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/auth/oauth/providers", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, httpErr(resp, "http")
	}
	var out struct {
		Providers []string `json:"providers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Providers, nil
}

// OAuthLoginURL 构造浏览器 OAuth 登录地址。
// 对应 {base}/auth/oauth/{provider}/login?redirect_uri={callback}。
func (c *Client) OAuthLoginURL(provider, redirectURI string) string {
	return fmt.Sprintf("%s/auth/oauth/%s/login?redirect_uri=%s",
		c.BaseURL, provider, url.QueryEscape(redirectURI))
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, bearer string) (*http.Response, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return c.HTTP.Do(req)
}
