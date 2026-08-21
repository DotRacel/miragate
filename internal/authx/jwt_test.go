package authx

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// makeJWT 构造一个仅供测试的未签名 JWT（header.payload.sig）。
func makeJWT(payload map[string]any) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	pj, _ := json.Marshal(payload)
	pb := base64.RawURLEncoding.EncodeToString(pj)
	return hdr + "." + pb + ".sig"
}

func TestDecodeJWT(t *testing.T) {
	tok := makeJWT(map[string]any{
		"sub":      "user-123",
		"exp":      2000000000,
		"email":    "a@b.com",
		"tenant":   "internal",
		"plan":     "pro",
		"plan_exp": 1900000000,
	})
	id := DecodeJWT(tok)
	if id == nil {
		t.Fatal("解析失败")
	}
	if id.Sub != "user-123" || id.Email != "a@b.com" || id.Tenant != "internal" || id.Plan != "pro" {
		t.Fatalf("字段错误: %+v", id)
	}
	if id.Exp != 2000000000 || id.PlanExp != 1900000000 {
		t.Fatalf("时间字段错误: %+v", id)
	}
}

func TestDecodeJWTInvalid(t *testing.T) {
	cases := []string{"", "a.b", "not-a-jwt", "a.b.c.d"}
	for _, c := range cases {
		if DecodeJWT(c) != nil {
			t.Fatalf("应返回 nil: %q", c)
		}
	}
	// 缺少 sub/exp 的载荷也应视为无效
	tok := makeJWT(map[string]any{"email": "x@y.com"})
	if DecodeJWT(tok) != nil {
		t.Fatal("缺 sub/exp 应返回 nil")
	}
}
