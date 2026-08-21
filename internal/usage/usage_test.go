package usage

import (
	"encoding/json"
	"testing"
)

// TestParseLimits 验证 /v1/limits 响应解析（对应 server.cjs 的 orn）。
func TestParseLimits(t *testing.T) {
	raw := `{"windows":[
		{"name":"5h","budget":100,"used":80,"reset_at":"2999-01-01T00:00:00Z"},
		{"name":"7d","budget":1000,"used":1000,"reset_at":"2999-01-02T00:00:00Z"},
		{"name":"bad","budget":0,"used":5}
	],"degraded":false}`
	var lr limitsResponse
	if err := json.Unmarshal([]byte(raw), &lr); err != nil {
		t.Fatal(err)
	}
	snap := parseLimits(&lr)
	if !snap.OK {
		t.Fatalf("应成功: %+v", snap)
	}
	if len(snap.Windows) != 2 {
		t.Fatalf("窗口数=%d want 2（budget<=0 应跳过）", len(snap.Windows))
	}
	// 5h: 80% → warning
	if snap.Windows[0].Label != "5h" || snap.Windows[0].UsedPercent != 80 || snap.Windows[0].Status != "warning" {
		t.Fatalf("5h 窗口错误: %+v", snap.Windows[0])
	}
	if snap.Windows[0].RemainingPercent != 20 {
		t.Fatalf("剩余应为 20: %+v", snap.Windows[0])
	}
	// 7d: 100% → limit_reached
	if snap.Windows[1].Status != "limit_reached" {
		t.Fatalf("7d 应为 limit_reached: %+v", snap.Windows[1])
	}
	// 整体状态取最严重
	if snap.Status != "limit_reached" {
		t.Fatalf("整体状态应为 limit_reached, got %s", snap.Status)
	}
	// reset_at 应解析出 resetAfterSeconds
	if snap.Windows[0].ResetAfterSeconds == nil {
		t.Fatal("应计算 resetAfterSeconds")
	}
}

func TestStatusFor(t *testing.T) {
	cases := []struct {
		pct  float64
		want string
	}{
		{0, "allowed"}, {79.9, "allowed"}, {80, "warning"}, {99.9, "warning"}, {100, "limit_reached"},
	}
	for _, c := range cases {
		if got := statusFor(c.pct); got != c.want {
			t.Fatalf("statusFor(%v)=%s want %s", c.pct, got, c.want)
		}
	}
}

func TestParseLimitsEmpty(t *testing.T) {
	snap := parseLimits(&limitsResponse{})
	if snap.OK {
		t.Fatal("空窗口应 OK=false")
	}
}
