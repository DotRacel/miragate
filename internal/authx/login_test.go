package authx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testJWT(email string) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	pj, _ := json.Marshal(map[string]any{"sub": "u1", "exp": 4000000000, "email": email})
	return hdr + "." + base64.RawURLEncoding.EncodeToString(pj) + ".sig"
}

// TestOAuthLoginFlow 用 mock auth 服务 + 模拟浏览器完整跑通 OAuth 登录：
// 打开 loginURL → 从 state 里取回 redirect_uri → 携带 token 回调本地服务。
func TestOAuthLoginFlow(t *testing.T) {
	token := testJWT("oauth@example.com")

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "oauth@example.com", "name": "OAuth User", "plan": "pro"})
	})
	auth := httptest.NewServer(mux)
	defer auth.Close()

	c := NewClient(auth.URL)

	// 模拟浏览器：解析 loginURL 的 redirect_uri，直接带 token 命中回调。
	open := func(loginURL string) {
		u := loginURL
		idx := strings.Index(u, "redirect_uri=")
		if idx < 0 {
			t.Error("loginURL 缺少 redirect_uri")
			return
		}
		redirect := u[idx+len("redirect_uri="):]
		redirect = urlDecode(redirect)
		go func() {
			// 稍等回调服务就绪
			time.Sleep(50 * time.Millisecond)
			_, _ = http.Get(redirect + "?access_token=" + token + "&refresh_token=REFRESH1")
		}()
	}

	res, err := c.OAuthLogin(context.Background(), "github", 5*time.Second, open)
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != token || res.RefreshToken != "REFRESH1" {
		t.Fatalf("令牌错误: %+v", res)
	}
	if res.DisplayName != "OAuth User" || res.Email != "oauth@example.com" {
		t.Fatalf("身份错误: %+v", res)
	}
}

// TestEmailLoginFlow 用 mock auth 服务跑通邮箱验证码登录。
func TestEmailLoginFlow(t *testing.T) {
	token := testJWT("mail@example.com")
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/code", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"dev_code": "123456"})
	})
	mux.HandleFunc("/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["email"] != "mail@example.com" || in["code"] != "123456" {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"detail": "bad code"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "refresh_token": "R2"})
	})
	mux.HandleFunc("/auth/me", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "mail@example.com", "name": "Mail User"})
	})
	auth := httptest.NewServer(mux)
	defer auth.Close()

	c := NewClient(auth.URL)
	res, err := c.EmailLogin(context.Background(), "mail@example.com", func(dev string) (string, error) {
		if dev != "123456" {
			t.Errorf("devCode=%q", dev)
		}
		return "123456", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != token || res.RefreshToken != "R2" || res.DisplayName != "Mail User" {
		t.Fatalf("结果错误: %+v", res)
	}
}

// urlDecode 简单还原 %XX（测试用）。
func urlDecode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			var v int
			_, err := fscanHex(s[i+1:i+3], &v)
			if err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func fscanHex(s string, v *int) (int, error) {
	n := 0
	for _, c := range s {
		n <<= 4
		switch {
		case c >= '0' && c <= '9':
			n |= int(c - '0')
		case c >= 'a' && c <= 'f':
			n |= int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			n |= int(c-'A') + 10
		default:
			return 0, errBadHex
		}
	}
	*v = n
	return 1, nil
}

var errBadHex = &hexErr{}

type hexErr struct{}

func (*hexErr) Error() string { return "bad hex" }
