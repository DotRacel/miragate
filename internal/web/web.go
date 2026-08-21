// Package web 托管本地用量页（风格对齐 Mirasim），展示账户信息与真实额度用量。
package web

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"miragate/internal/tokens"
	"miragate/internal/usage"
)

// Server 提供用量页与 JSON API。
type Server struct {
	tm     *tokens.Manager
	poller *usage.Poller
	listen string
}

// New 构造用量页服务。
func New(tm *tokens.Manager, poller *usage.Poller, listen string) *Server {
	return &Server{tm: tm, poller: poller, listen: listen}
}

// Register 将用量页与 API 挂载到 mux。
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/usage", s.handleUsage)
	mux.HandleFunc("/api/refresh", s.handleRefresh)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

// payload 是 /api/usage 的返回结构。
type payload struct {
	Account *account        `json:"account"`
	Usage   *usage.Snapshot `json:"usage"`
	Now     string          `json:"now"`
}

type account struct {
	Name         string `json:"name,omitempty"`
	Email        string `json:"email,omitempty"`
	Plan         string `json:"plan,omitempty"`
	Tenant       string `json:"tenant,omitempty"`
	Exp          int64  `json:"exp,omitempty"`
	LoggedIn     bool   `json:"loggedIn"`
	DeviceID     string `json:"deviceId,omitempty"`
	HasDeviceKey bool   `json:"hasDeviceKey"`
}

func (s *Server) buildPayload() payload {
	p := payload{Now: time.Now().UTC().Format(time.RFC3339)}
	if snap := s.tm.Snapshot(); snap != nil {
		p.Account = &account{
			Name: snap.Name, Email: snap.Email, Plan: snap.Plan, Tenant: snap.Tenant,
			Exp: snap.Exp, LoggedIn: true, DeviceID: snap.DeviceID, HasDeviceKey: snap.HasDeviceKey,
		}
	} else {
		p.Account = &account{LoggedIn: false}
	}
	p.Usage = s.poller.Latest()
	return p
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.buildPayload())
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	s.poller.Refresh(ctx)
	writeJSON(w, s.buildPayload())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
