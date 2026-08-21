// Package relay 实现指向 relay.mirasim.ai 的高性能本地反向代理。
//
// CLI（如 Claude Code）将 ANTHROPIC_BASE_URL 指向本代理，本代理负责：
//   - 剥离客户端自带的 authorization / x-api-key；
//   - 注入 Mirasim 凭据（优先设备 ticket，回退 JWT）为 Bearer；
//   - 附加 relay 元信息头（x-mirasim-agent/device/client/session/call）；
//   - 计算并附加 mrs-sig-v1 设备签名头；
//   - 透传并流式回传上游响应（对 SSE 及时 flush）。
//
// 对应 Mirasim server.cjs 中 vr/rXe 代理类的 relay 分支与 BJe 头处理。
package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// hop-by-hop 头，与 server.cjs 的 $bs / zbs 集合一致。
var reqDrop = map[string]bool{
	"host": true, "connection": true, "content-length": true, "transfer-encoding": true,
}
var respDrop = map[string]bool{
	"content-length": true, "transfer-encoding": true, "connection": true,
}

// dropAnthropicBeta 对应 server.cjs 的 btn：从 anthropic-beta 中剔除的值。
var dropAnthropicBeta = map[string]bool{"oauth-2025-04-20": true}

// CredentialProvider 由 tokens.Manager 实现，供代理取凭据与签名头。
type CredentialProvider interface {
	Credential(ctx context.Context) (string, error)
	SignHeaders(method, path string, body []byte) map[string]string
	TicketRefused()
}

// Options 配置代理行为。
type Options struct {
	RelayBase string // 例如 https://relay.mirasim.ai
	Agent     string // relay agent，默认 claude
	ClientVer string // x-mirasim-client
	DeviceID  string // x-mirasim-device 元信息（持久 UUID）
	Locale    string // x-mirasim-locale（可选）
	Creds     CredentialProvider

	// OnCall 在每次代理调用结束后回调，供用量统计（可为 nil）。
	OnCall func(CallInfo)
}

// CallInfo 描述一次代理调用的结果。
type CallInfo struct {
	Method   string
	Path     string
	Status   int
	Bytes    int64
	Duration time.Duration
	ViaRelay bool
}

// Proxy 是 http.Handler。
type Proxy struct {
	opts      Options
	base      string
	transport *http.Transport
	sessionID string
	callSeq   atomic.Uint64
}

// New 构造代理。使用带连接池的自定义 Transport 以获得高吞吐与低延迟。
func New(opts Options) *Proxy {
	if opts.Agent == "" {
		opts.Agent = "claude"
	}
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// 不设 ResponseHeaderTimeout：模型响应首字节可能较慢。
	}
	return &Proxy{
		opts:      opts,
		base:      strings.TrimRight(opts.RelayBase, "/"),
		transport: tr,
		sessionID: randHex(16),
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	callID := randHex(12)

	// 读取请求体（签名需要 body 的 sha256）。模型请求体通常不大。
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadGateway)
		return
	}
	_ = r.Body.Close()

	// 上游 URL：relayBase + 原始 path + query。claude 无 pathPrefix。
	upstreamURL := p.base + r.URL.Path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	cred, err := p.opts.Creds.Credential(r.Context())
	if err != nil {
		http.Error(w, "not authenticated: run `miragate login`", http.StatusUnauthorized)
		return
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, strings.NewReader(string(body)))
	if err != nil {
		http.Error(w, "build request failed", http.StatusBadGateway)
		return
	}

	p.buildHeaders(outReq, r, body, cred, callID)

	resp, err := p.transport.RoundTrip(outReq)
	if err != nil {
		if r.Context().Err() != nil {
			return // 客户端取消
		}
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// relay 拒绝已签名请求 → 触发重新换票。
	if resp.StatusCode == http.StatusUnauthorized {
		p.opts.Creds.TicketRefused()
	}

	// 回写响应头（剔除 hop-by-hop）。
	dst := w.Header()
	for k, vv := range resp.Header {
		if respDrop[strings.ToLower(k)] {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	n := streamBody(w, resp.Body)

	if p.opts.OnCall != nil {
		p.opts.OnCall(CallInfo{
			Method:   r.Method,
			Path:     r.URL.Path,
			Status:   resp.StatusCode,
			Bytes:    n,
			Duration: time.Since(start),
			ViaRelay: true,
		})
	}
}

// buildHeaders 组装发往 relay 的请求头。
func (p *Proxy) buildHeaders(out *http.Request, in *http.Request, body []byte, cred, callID string) {
	h := out.Header
	// 1) 透传客户端头（剔除 hop-by-hop 与将被替换的鉴权头）。
	for k, vv := range in.Header {
		lk := strings.ToLower(k)
		if reqDrop[lk] || lk == "authorization" || lk == "x-api-key" {
			continue
		}
		if lk == "anthropic-beta" {
			if filtered := filterCSV(vv, dropAnthropicBeta); filtered != "" {
				h.Set(k, filtered)
			}
			continue
		}
		for _, v := range vv {
			h.Add(k, v)
		}
	}
	// 2) 注入凭据。
	h.Set("Authorization", "Bearer "+cred)

	// 3) relay 元信息头。
	setIf(h, "x-mirasim-agent", p.opts.Agent)
	setIf(h, "x-mirasim-device", p.opts.DeviceID)
	setIf(h, "x-mirasim-client", p.opts.ClientVer)
	setIf(h, "x-mirasim-session", p.sessionID)
	setIf(h, "x-mirasim-locale", p.opts.Locale)
	h.Set("x-mirasim-call", callID)

	// 4) 设备签名头（mrs-sig-v1，签名覆盖 path+body；无设备会话时为空）。
	for k, v := range p.opts.Creds.SignHeaders(in.Method, in.URL.Path, body) {
		if v != "" {
			h.Set(k, v)
		}
	}
}

// streamBody 将上游响应边收边写并及时 flush（保证 SSE 实时性）。
func streamBody(w http.ResponseWriter, src io.Reader) int64 {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			wn, werr := w.Write(buf[:n])
			total += int64(wn)
			if flusher != nil {
				flusher.Flush()
			}
			if werr != nil {
				return total
			}
		}
		if err != nil {
			return total
		}
	}
}

// filterCSV 从逗号分隔的多值头中剔除指定值，返回重组后的字符串。
func filterCSV(values []string, drop map[string]bool) string {
	var keep []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" && !drop[part] {
				keep = append(keep, part)
			}
		}
	}
	return strings.Join(keep, ", ")
}

func setIf(h http.Header, key, val string) {
	if val != "" {
		h.Set(key, val)
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
