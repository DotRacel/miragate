package main

import (
	"net/http"
	"strings"
	"time"
)

// newServeMux 返回用量页的 mux。
func newServeMux() *http.ServeMux { return http.NewServeMux() }

// newHTTPServer 构造监听服务。不设写超时以支持长连接 SSE 流式响应。
func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 30 * time.Second,
		// WriteTimeout / IdleTimeout 保持 0：SSE 及模型长响应需要长连接。
	}
}

// dispatcher 将 UI 相关路径交给用量页，其余（如 /v1/messages）交给反代。
func dispatcher(web http.Handler, proxy http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWebPath(r.URL.Path) {
			web.ServeHTTP(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// isWebPath 判断是否为用量页/管理端点（其余全部视为需转发的 API 流量）。
func isWebPath(p string) bool {
	switch p {
	case "/", "/healthz", "/favicon.ico":
		return true
	}
	return strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/_miragate/")
}
