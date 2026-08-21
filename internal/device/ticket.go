package device

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"miragate/internal/httpx"
)

// Ticket 是 relay 颁发的短期设备会话凭据。
type Ticket struct {
	Value     string
	ExpiresAt time.Time
}

// TicketManager 负责用 Bearer 令牌向 relay 换取并缓存 ticket。
// relay 不支持设备签名（返回 404/501 等）时进入 unsupported，调用方回退为纯令牌。
type TicketManager struct {
	key         *Key
	relayBase   string
	clientVer   string
	http        *http.Client
	issuerToken func() string // 返回当前 JWT access_token

	mu          sync.Mutex
	state       *Ticket
	mintedFor   string
	unsupported bool
	nextAttempt time.Time
}

// NewTicketManager 构造 ticket 管理器。relayBase 例如 https://relay.mirasim.ai。
func NewTicketManager(key *Key, relayBase, clientVer string, issuerToken func() string) *TicketManager {
	return &TicketManager{
		key:         key,
		relayBase:   strings.TrimRight(relayBase, "/"),
		clientVer:   clientVer,
		http:        httpx.Client(12 * time.Second),
		issuerToken: issuerToken,
	}
}

// Credential 返回当前可用的 ticket 值；无有效 ticket 时返回空串（调用方回退纯令牌）。
func (m *TicketManager) Credential() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != nil && time.Now().Before(m.state.ExpiresAt) {
		return m.state.Value
	}
	return ""
}

// Refused 由调用方在 relay 拒绝已签名请求（401）时调用，强制重新换票。
func (m *TicketManager) Refused() {
	m.mu.Lock()
	m.state = nil
	m.mintedFor = ""
	m.mu.Unlock()
}

// EnsureFresh 在需要时（无票/临近过期/令牌变化）后台换取新 ticket。
// 出错静默失败，调用方会回退为纯令牌。
func (m *TicketManager) EnsureFresh(ctx context.Context) {
	m.mu.Lock()
	if m.unsupported || time.Now().Before(m.nextAttempt) {
		m.mu.Unlock()
		return
	}
	token := ""
	if m.issuerToken != nil {
		token = m.issuerToken()
	}
	// 已有为当前令牌铸造且未临近过期的票 → 无需换。
	if token == "" || (m.state != nil && m.mintedFor == token && time.Now().Before(m.state.ExpiresAt.Add(-2*time.Minute))) {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.mint(ctx, token)
}

func (m *TicketManager) mint(ctx context.Context, token string) {
	if token == "" {
		return
	}
	bodyObj := map[string]string{
		"publicKey": m.key.PublicKeyB64(),
		"deviceId":  m.key.DeviceID(),
	}
	body, _ := json.Marshal(bodyObj)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.relayBase+SessionPath, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range m.key.SignRequest(http.MethodPost, SessionPath, body, m.clientVer) {
		req.Header.Set(k, v)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		m.mu.Lock()
		m.nextAttempt = time.Now().Add(30 * time.Second)
		m.mu.Unlock()
		return
	}
	defer resp.Body.Close()

	// relay 不支持设备签名 → 长时间退避，走纯令牌。
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		m.mu.Lock()
		m.unsupported = true
		m.mu.Unlock()
		return
	}
	if resp.StatusCode/100 != 2 {
		m.mu.Lock()
		m.nextAttempt = time.Now().Add(30 * time.Second)
		m.mu.Unlock()
		return
	}

	var out struct {
		Ticket    string  `json:"ticket"`
		ExpiresIn float64 `json:"expiresIn"`
		ExpiresAt float64 `json:"expiresAt"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || out.Ticket == "" {
		m.mu.Lock()
		m.nextAttempt = time.Now().Add(30 * time.Second)
		m.mu.Unlock()
		return
	}

	var exp time.Time
	switch {
	case out.ExpiresIn > 0:
		exp = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	case out.ExpiresAt > 0:
		exp = time.Unix(int64(out.ExpiresAt), 0)
	default:
		exp = time.Now().Add(10 * time.Minute)
	}

	m.mu.Lock()
	m.state = &Ticket{Value: out.Ticket, ExpiresAt: exp}
	m.mintedFor = token
	m.nextAttempt = time.Time{}
	m.mu.Unlock()
}

// HeadersFor 计算一次代理请求的签名头。仅当已建立设备会话（有票）时才签名，
// 否则只带 client 版本头，与 server.cjs 的 signer.headersFor 行为一致。
func (m *TicketManager) HeadersFor(method, path string, body []byte) map[string]string {
	m.mu.Lock()
	hasTicket := m.state != nil && time.Now().Before(m.state.ExpiresAt)
	m.mu.Unlock()
	if hasTicket {
		return m.key.SignRequest(method, path, body, m.clientVer)
	}
	if m.clientVer != "" {
		return map[string]string{HeaderClient: m.clientVer}
	}
	return map[string]string{}
}
