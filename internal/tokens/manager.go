// Package tokens 统一管理登录凭据的生命周期：持久化、临期自动刷新，并向
// 反代提供当前有效凭据（优先设备 ticket，回退纯 JWT）。
package tokens

import (
	"context"
	"errors"
	"sync"
	"time"

	"miragate/internal/authx"
	"miragate/internal/config"
	"miragate/internal/device"
)

// refreshBuffer 对应 server.cjs 的 wbs（15 分钟）：exp 剩余不足此值即刷新。
const refreshBuffer = 15 * time.Minute

// Manager 是凭据的单一事实来源。
type Manager struct {
	cfg    *config.Config
	auth   *authx.Client
	key    *device.Key
	ticket *device.TicketManager

	mu sync.Mutex
}

// New 构造凭据管理器。key/ticket 可为 nil（无设备签名时）。
func New(cfg *config.Config, auth *authx.Client, key *device.Key, ticket *device.TicketManager) *Manager {
	return &Manager{cfg: cfg, auth: auth, key: key, ticket: ticket}
}

// LoggedIn 返回是否已存在有效登录态。
func (m *Manager) LoggedIn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Auth != nil && m.cfg.Auth.Token != ""
}

// Identity 返回当前 JWT 身份（可能为 nil）。
func (m *Manager) Identity() *authx.Identity {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.Auth == nil || m.cfg.Auth.Token == "" {
		return nil
	}
	return authx.DecodeJWT(m.cfg.Auth.Token)
}

// Snapshot 返回身份的只读快照（含缓存的显示名/邮箱）。
type Snapshot struct {
	Token        string
	Email        string
	Name         string
	Plan         string
	Tenant       string
	Exp          int64
	HasDeviceKey bool
	DeviceID     string
}

// Snapshot 汇总当前登录信息，供状态展示与用量页使用。
func (m *Manager) Snapshot() *Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.Auth == nil || m.cfg.Auth.Token == "" {
		return nil
	}
	s := &Snapshot{Token: m.cfg.Auth.Token, Name: m.cfg.Auth.Name, Email: m.cfg.Auth.Email}
	if id := authx.DecodeJWT(m.cfg.Auth.Token); id != nil {
		s.Exp = id.Exp
		s.Tenant = id.Tenant
		s.Plan = id.Plan
		if s.Email == "" {
			s.Email = id.Email
		}
	}
	if m.key != nil {
		s.HasDeviceKey = true
		s.DeviceID = m.key.DeviceID()
	}
	return s
}

// Save 将当前登录态写入 config.Auth 并持久化。
func (m *Manager) Save(res *authx.LoginResult) error {
	m.mu.Lock()
	a := &config.Auth{Token: res.Token, RefreshToken: res.RefreshToken, Name: res.DisplayName, Email: res.Email}
	if id := authx.DecodeJWT(res.Token); id != nil {
		a.UserID = id.Sub
		a.Exp = id.Exp
		if a.Email == "" {
			a.Email = id.Email
		}
	}
	m.cfg.Auth = a
	m.mu.Unlock()
	return config.Save(m.cfg)
}

// Logout 清除登录态。
func (m *Manager) Logout() error {
	m.mu.Lock()
	m.cfg.Auth = nil
	m.mu.Unlock()
	if m.ticket != nil {
		m.ticket.Refused()
	}
	return config.Save(m.cfg)
}

// rawToken 返回当前 JWT（临期则先刷新）。
func (m *Manager) rawToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	if m.cfg.Auth == nil || m.cfg.Auth.Token == "" {
		m.mu.Unlock()
		return "", errors.New("not logged in")
	}
	tok := m.cfg.Auth.Token
	refresh := m.cfg.Auth.RefreshToken
	exp := m.cfg.Auth.Exp
	m.mu.Unlock()

	now := time.Now().Unix()
	if exp == 0 || exp-now > int64(refreshBuffer.Seconds()) {
		return tok, nil // 未临期，无需刷新
	}
	if refresh == "" {
		return tok, nil // 无 refresh_token，只能沿用直至过期
	}

	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	t, err := m.auth.Refresh(rctx, refresh)
	if err != nil {
		return tok, nil // 刷新失败则沿用旧令牌
	}
	newRefresh := t.RefreshToken
	if newRefresh == "" {
		newRefresh = refresh
	}
	m.mu.Lock()
	m.cfg.Auth.Token = t.AccessToken
	m.cfg.Auth.RefreshToken = newRefresh
	if id := authx.DecodeJWT(t.AccessToken); id != nil {
		m.cfg.Auth.Exp = id.Exp
		m.cfg.Auth.UserID = id.Sub
	}
	saved := *m.cfg
	m.mu.Unlock()
	_ = config.Save(&saved)
	return t.AccessToken, nil
}

// EnsureFresh 触发令牌刷新与设备票据刷新（后台周期调用）。
func (m *Manager) EnsureFresh(ctx context.Context) {
	if _, err := m.rawToken(ctx); err != nil {
		return
	}
	if m.ticket != nil {
		m.ticket.EnsureFresh(ctx)
	}
}

// Credential 返回代理应使用的凭据：优先设备 ticket，否则当前（必要时刷新的）JWT。
func (m *Manager) Credential(ctx context.Context) (string, error) {
	tok, err := m.rawToken(ctx)
	if err != nil {
		return "", err
	}
	if m.ticket != nil {
		if c := m.ticket.Credential(); c != "" {
			return c, nil
		}
	}
	return tok, nil
}

// SignHeaders 返回一次请求的设备签名头（无设备会话时为空）。
func (m *Manager) SignHeaders(method, path string, body []byte) map[string]string {
	if m.ticket != nil {
		return m.ticket.HeadersFor(method, path, body)
	}
	return map[string]string{}
}

// TicketRefused 通知设备会话被 relay 拒绝，需要重新换票。
func (m *Manager) TicketRefused() {
	if m.ticket != nil {
		m.ticket.Refused()
	}
}
