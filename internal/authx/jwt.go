// Package authx 实现与 auth.mirasim.ai 的鉴权协议：邮箱验证码登录、OAuth
// 浏览器登录、令牌刷新、身份查询，以及 JWT 载荷解析。
//
// 对应 Mirasim server.cjs 中的 KC / CKe / wKe / XC / xKe / SKe / kKe 等函数。
package authx

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Identity 是从 JWT 载荷解析出的身份信息。
type Identity struct {
	Sub     string // 用户唯一 ID
	Exp     int64  // 过期时间（Unix 秒）
	Email   string
	Tenant  string // internal / external
	Plan    string
	PlanExp int64
}

// jwtPayload 对应 JWT 中间段的 JSON。
type jwtPayload struct {
	Sub     string  `json:"sub"`
	Exp     int64   `json:"exp"`
	Email   string  `json:"email"`
	Tenant  string  `json:"tenant"`
	Plan    string  `json:"plan"`
	PlanExp float64 `json:"plan_exp"`
}

// DecodeJWT 解析 JWT，不验证签名（验证由服务端完成）。
// 只有当 sub 与 exp 均存在时才返回 Identity，否则返回 nil。
func DecodeJWT(token string) *Identity {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(padStrip(parts[1]))
	if err != nil {
		// 兼容带 padding 的编码
		raw, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil
		}
	}
	var p jwtPayload
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	if p.Sub == "" || p.Exp == 0 {
		return nil
	}
	id := &Identity{Sub: p.Sub, Exp: p.Exp, Email: p.Email, Tenant: p.Tenant, Plan: p.Plan}
	if p.PlanExp > 0 {
		id.PlanExp = int64(p.PlanExp)
	}
	return id
}

// padStrip 去除可能存在的 base64 padding，以配合 RawURLEncoding。
func padStrip(s string) string {
	return strings.TrimRight(s, "=")
}
