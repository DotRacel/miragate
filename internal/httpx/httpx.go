// Package httpx 提供统一的、支持出站代理（http/https/socks5）的 HTTP 客户端。
//
// 代理通过环境变量配置（便于在 docker compose 中设置）：
//   - HTTP_PROXY  / http_proxy
//   - HTTPS_PROXY / https_proxy
//   - ALL_PROXY   / all_proxy   （作为上面两者的回退，socks5 常用此项）
//   - NO_PROXY    / no_proxy
//
// socks5 示例：ALL_PROXY=socks5://user:pass@host:1080
// http  示例：HTTPS_PROXY=http://user:pass@host:8080
package httpx

import (
	"net/http"
	"os"
	"time"
)

// NormalizeProxyEnv 让 ALL_PROXY 成为 HTTP_PROXY/HTTPS_PROXY 的回退，
// 从而"一个代理设置覆盖全部出站流量"。必须在发起任何 HTTP 请求前调用一次。
func NormalizeProxyEnv() {
	all := firstEnv("ALL_PROXY", "all_proxy")
	if all == "" {
		return
	}
	ensureEnv("HTTP_PROXY", "http_proxy", all)
	ensureEnv("HTTPS_PROXY", "https_proxy", all)
}

// Transport 返回一个遵循代理环境变量的 Transport（http/https/socks5 由标准库处理）。
func Transport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = http.ProxyFromEnvironment
	return t
}

// Client 返回带超时、遵循代理设置的 HTTP 客户端。
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: Transport()}
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func ensureEnv(upper, lower, val string) {
	if os.Getenv(upper) == "" && os.Getenv(lower) == "" {
		_ = os.Setenv(upper, val)
	}
}
