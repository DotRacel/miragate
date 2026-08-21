// Package usage 通过 relay 的 /v1/limits 端点采集真实用量额度，并对外提供
// 快照供用量页展示。对应 Mirasim server.cjs 的 cIs/orn（/v1/limits 探测与解析）。
package usage

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"miragate/internal/httpx"
)

// ProbeHeader 是 relay 用量探测头，对应 server.cjs 的 prn。
const ProbeHeader = "x-mirasim-probe"

// LimitsPath 对应 server.cjs 的 drn。
const LimitsPath = "/v1/limits"

// Window 是一个额度窗口（如 5h、7d）。
type Window struct {
	Label             string  `json:"label"`
	UsedPercent       float64 `json:"usedPercent"`
	RemainingPercent  float64 `json:"remainingPercent"`
	ResetAt           string  `json:"resetAt,omitempty"`
	ResetAfterSeconds *int64  `json:"resetAfterSeconds,omitempty"`
	Status            string  `json:"status"` // allowed / warning / limit_reached
}

// Snapshot 是一次用量采集结果。
type Snapshot struct {
	OK         bool     `json:"ok"`
	Agent      string   `json:"agent"`
	Source     string   `json:"source"`
	Status     string   `json:"status"` // allowed / warning / limit_reached
	Windows    []Window `json:"windows"`
	Degraded   bool     `json:"degraded,omitempty"`
	CapturedAt string   `json:"capturedAt"`
	Error      string   `json:"error,omitempty"`
}

// Provider 提供采集所需的凭据与签名头（由 tokens.Manager 实现）。
type Provider interface {
	Credential(ctx context.Context) (string, error)
	SignHeaders(method, path string, body []byte) map[string]string
}

// Poller 周期性采集用量并缓存最新快照。
type Poller struct {
	relayBase string
	clientVer string
	deviceID  string
	provider  Provider
	http      *http.Client
	interval  time.Duration

	mu   sync.RWMutex
	last *Snapshot
}

// NewPoller 构造用量轮询器。interval<=0 时默认 60s。
func NewPoller(relayBase, clientVer, deviceID string, p Provider, interval time.Duration) *Poller {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Poller{
		relayBase: strings.TrimRight(relayBase, "/"),
		clientVer: clientVer,
		deviceID:  deviceID,
		provider:  p,
		http:      httpx.Client(20 * time.Second),
		interval:  interval,
	}
}

// Latest 返回最近一次采集的快照（可能为 nil）。
func (p *Poller) Latest() *Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.last
}

// Run 阻塞运行轮询，直到 ctx 取消。启动即先采集一次。
func (p *Poller) Run(ctx context.Context) {
	p.refresh(ctx)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.refresh(ctx)
		}
	}
}

// Refresh 立即采集一次并返回结果。
func (p *Poller) Refresh(ctx context.Context) *Snapshot {
	return p.refresh(ctx)
}

func (p *Poller) refresh(ctx context.Context) *Snapshot {
	snap := p.fetch(ctx)
	p.mu.Lock()
	p.last = snap
	p.mu.Unlock()
	return snap
}

// limitsResponse 对应 relay /v1/limits 的返回。
type limitsResponse struct {
	Windows []struct {
		Name    string      `json:"name"`
		Budget  json.Number `json:"budget"`
		Used    json.Number `json:"used"`
		ResetAt any         `json:"reset_at"`
	} `json:"windows"`
	Degraded bool `json:"degraded"`
}

func (p *Poller) fetch(ctx context.Context) *Snapshot {
	fail := func(msg string) *Snapshot {
		return &Snapshot{OK: false, Agent: "claude", Source: "relay-limits", Status: "unknown",
			CapturedAt: time.Now().UTC().Format(time.RFC3339), Error: msg}
	}

	cred, err := p.provider.Credential(ctx)
	if err != nil {
		return fail("not authenticated")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, p.relayBase+LimitsPath, nil)
	if err != nil {
		return fail(err.Error())
	}
	req.Header.Set(ProbeHeader, "usage")
	req.Header.Set("Authorization", "Bearer "+cred)
	if p.deviceID != "" {
		req.Header.Set("x-mirasim-device", p.deviceID)
	}
	if p.clientVer != "" {
		req.Header.Set("x-mirasim-client", p.clientVer)
	}
	for k, v := range p.provider.SignHeaders(http.MethodGet, LimitsPath, nil) {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return fail(err.Error())
	}
	defer resp.Body.Close()

	// relay 版本较老不支持 /v1/limits。
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		return fail("relay 不支持 /v1/limits（版本较老）")
	}
	if resp.StatusCode/100 != 2 {
		return fail("HTTP " + itoa(resp.StatusCode))
	}

	var lr limitsResponse
	if json.NewDecoder(resp.Body).Decode(&lr) != nil {
		return fail("解析用量响应失败")
	}
	return parseLimits(&lr)
}

// parseLimits 将 /v1/limits 响应转为快照，对应 server.cjs 的 orn。
func parseLimits(lr *limitsResponse) *Snapshot {
	var windows []Window
	for _, w := range lr.Windows {
		budget, berr := w.Budget.Float64()
		used, uerr := w.Used.Float64()
		if w.Name == "" || berr != nil || uerr != nil || budget <= 0 || math.IsNaN(used) {
			continue
		}
		usedPct := round1(clamp(used/budget*100, 0, 100))
		remaining := round1(clamp(100-usedPct, 0, 100))
		win := Window{
			Label:            w.Name,
			UsedPercent:      usedPct,
			RemainingPercent: remaining,
			Status:           statusFor(usedPct),
		}
		if s := resetAtString(w.ResetAt); s != "" {
			win.ResetAt = s
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				secs := int64(math.Max(0, math.Round(time.Until(t).Seconds())))
				win.ResetAfterSeconds = &secs
			}
		}
		windows = append(windows, win)
	}
	if len(windows) == 0 {
		return &Snapshot{OK: false, Agent: "claude", Source: "relay-limits", Status: "unknown",
			CapturedAt: time.Now().UTC().Format(time.RFC3339), Error: "无额度窗口"}
	}
	overall := "allowed"
	for _, w := range windows {
		if w.Status == "limit_reached" {
			overall = "limit_reached"
			break
		}
		if w.Status == "warning" {
			overall = "warning"
		}
	}
	return &Snapshot{
		OK:         true,
		Agent:      "claude",
		Source:     "relay-limits",
		Status:     overall,
		Windows:    windows,
		Degraded:   lr.Degraded,
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// statusFor 阈值与 server.cjs 一致：>=100 limit_reached，>=80 warning。
func statusFor(usedPct float64) string {
	switch {
	case usedPct >= 100:
		return "limit_reached"
	case usedPct >= 80:
		return "warning"
	default:
		return "allowed"
	}
}

func resetAtString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return time.Unix(int64(t), 0).UTC().Format(time.RFC3339)
	}
	return ""
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
