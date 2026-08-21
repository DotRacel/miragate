package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCreds 实现 CredentialProvider 供测试。
type fakeCreds struct {
	cred    string
	signHdr map[string]string
	refused bool
}

func (f *fakeCreds) Credential(context.Context) (string, error) { return f.cred, nil }
func (f *fakeCreds) SignHeaders(method, path string, body []byte) map[string]string {
	return f.signHdr
}
func (f *fakeCreds) TicketRefused() { f.refused = true }

// TestProxyInjectsHeaders 验证反代注入凭据、剥离客户端鉴权、附加元信息与签名头。
func TestProxyInjectsHeaders(t *testing.T) {
	var got *http.Request
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer upstream.Close()

	creds := &fakeCreds{cred: "TICKET123", signHdr: map[string]string{"x-mirasim-sig": "SIG", "x-mirasim-device": "DEV"}}
	p := New(Options{RelayBase: upstream.URL, Agent: "claude", ClientVer: "0.0.214", DeviceID: "uuid-1", Creds: creds})

	body := `{"model":"claude-sonnet-5"}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("authorization", "Bearer CLIENT_SHOULD_BE_STRIPPED")
	req.Header.Set("x-api-key", "should-be-stripped")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20, other-beta")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if got == nil {
		t.Fatal("上游未收到请求")
	}
	if gotBody != body {
		t.Fatalf("body 未透传: %q", gotBody)
	}
	if got.Header.Get("Authorization") != "Bearer TICKET123" {
		t.Fatalf("凭据注入错误: %q", got.Header.Get("Authorization"))
	}
	if got.Header.Get("x-api-key") != "" {
		t.Fatal("x-api-key 未被剥离")
	}
	if got.Header.Get("anthropic-version") != "2023-06-01" {
		t.Fatal("anthropic-version 应透传")
	}
	// anthropic-beta 应剔除 oauth-2025-04-20，保留 other-beta
	if got.Header.Get("anthropic-beta") != "other-beta" {
		t.Fatalf("anthropic-beta 过滤错误: %q", got.Header.Get("anthropic-beta"))
	}
	if got.Header.Get("x-mirasim-agent") != "claude" || got.Header.Get("x-mirasim-device") != "DEV" {
		t.Fatalf("元信息/签名头错误: agent=%q device=%q", got.Header.Get("x-mirasim-agent"), got.Header.Get("x-mirasim-device"))
	}
	if got.Header.Get("x-mirasim-sig") != "SIG" {
		t.Fatal("签名头未附加")
	}
	if got.Header.Get("x-mirasim-call") == "" {
		t.Fatal("缺 x-mirasim-call")
	}
	if rec.Body.String() != "data: ok\n\n" {
		t.Fatalf("响应体透传错误: %q", rec.Body.String())
	}
}

// TestProxy401TriggersRefused 验证 relay 返回 401 时触发重新换票。
func TestProxy401TriggersRefused(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer upstream.Close()
	creds := &fakeCreds{cred: "T"}
	p := New(Options{RelayBase: upstream.URL, Creds: creds})
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}")))
	if !creds.refused {
		t.Fatal("401 应触发 TicketRefused")
	}
}

func TestFilterCSV(t *testing.T) {
	got := filterCSV([]string{"oauth-2025-04-20, keep-me", "another"}, map[string]bool{"oauth-2025-04-20": true})
	if got != "keep-me, another" {
		t.Fatalf("filterCSV=%q", got)
	}
	if filterCSV([]string{"oauth-2025-04-20"}, map[string]bool{"oauth-2025-04-20": true}) != "" {
		t.Fatal("全部剔除应为空")
	}
}
