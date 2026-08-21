package httpx

import (
	"os"
	"testing"
)

func TestNormalizeProxyEnv(t *testing.T) {
	// 清理相关环境
	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		os.Unsetenv(k)
	}
	os.Setenv("ALL_PROXY", "socks5://127.0.0.1:1080")
	defer os.Unsetenv("ALL_PROXY")

	NormalizeProxyEnv()
	if os.Getenv("HTTP_PROXY") != "socks5://127.0.0.1:1080" {
		t.Fatalf("HTTP_PROXY 未从 ALL_PROXY 回退: %q", os.Getenv("HTTP_PROXY"))
	}
	if os.Getenv("HTTPS_PROXY") != "socks5://127.0.0.1:1080" {
		t.Fatalf("HTTPS_PROXY 未从 ALL_PROXY 回退: %q", os.Getenv("HTTPS_PROXY"))
	}
}

func TestNormalizeProxyEnvKeepsExisting(t *testing.T) {
	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		os.Unsetenv(k)
	}
	os.Setenv("HTTP_PROXY", "http://explicit:8080")
	os.Setenv("ALL_PROXY", "socks5://fallback:1080")
	defer func() { os.Unsetenv("HTTP_PROXY"); os.Unsetenv("ALL_PROXY") }()

	NormalizeProxyEnv()
	if os.Getenv("HTTP_PROXY") != "http://explicit:8080" {
		t.Fatalf("已显式设置的 HTTP_PROXY 不应被覆盖: %q", os.Getenv("HTTP_PROXY"))
	}
	if os.Getenv("HTTPS_PROXY") != "socks5://fallback:1080" {
		t.Fatalf("未设置的 HTTPS_PROXY 应回退到 ALL_PROXY: %q", os.Getenv("HTTPS_PROXY"))
	}
}
