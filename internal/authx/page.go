package authx

import (
	"fmt"
	"html"
	"strings"
)

// pageData 驱动 OAuth 回调页面渲染。
type pageData struct {
	OK     bool
	Title  string
	Detail string
	Name   string
	Mail   string
}

// resultPage 渲染登录结果页，风格对齐 Mirasim（Den 函数）。
func resultPage(d pageData) []byte {
	tone := "var(--err)"
	icon := `<path pathLength="100" d="M7 7l10 10M17 7L7 17"/>`
	if d.OK {
		tone = "var(--ok)"
		icon = `<path pathLength="100" d="M5 12.6l4.6 4.6L19 7.4"/>`
	}
	who := ""
	if d.OK && d.Name != "" {
		initial := strings.ToUpper(string([]rune(strings.TrimSpace(d.Name))[:1]))
		mail := ""
		if d.Mail != "" && d.Mail != d.Name {
			mail = `<div class="mail">` + html.EscapeString(d.Mail) + `</div>`
		}
		who = fmt.Sprintf(`<div class="who"><div class="avatar">%s</div><div class="meta"><div class="name">%s</div>%s</div></div>`,
			html.EscapeString(initial), html.EscapeString(d.Name), mail)
	}
	sub := ""
	if d.Detail != "" {
		sub = `<p class="sub">` + html.EscapeString(d.Detail) + `</p>`
	}
	return []byte(fmt.Sprintf(`<!doctype html><html lang="zh"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>miragate</title>
<style>
:root{--bg:#f9f9f9;--card:#fff;--edge:rgba(24,24,27,.1);--edge-subtle:rgba(24,24,27,.07);--tile:#f9f9f9;--fg:#18181b;--fg-3:rgba(39,39,46,.72);--fg-5:rgba(82,82,91,.55);--ok:#16a34a;--err:#dc2626;--sans:-apple-system,BlinkMacSystemFont,'Segoe UI',system-ui,'PingFang SC','Microsoft YaHei',sans-serif;--mono:'JetBrains Mono',ui-monospace,Menlo,Consolas,monospace;--shadow:0 1px 2px rgba(24,24,27,.04),0 14px 34px rgba(24,24,27,.09)}
@media(prefers-color-scheme:dark){:root{--bg:#0b0d11;--card:#171b24;--edge:rgba(148,163,184,.16);--edge-subtle:rgba(148,163,184,.1);--tile:rgba(255,255,255,.045);--fg:rgba(248,250,252,.95);--fg-3:rgba(226,232,240,.66);--fg-5:rgba(148,163,184,.52);--ok:#34d399;--err:#f87171;--shadow:0 1px 2px rgba(0,0,0,.3),0 18px 44px rgba(0,0,0,.5)}}
*{box-sizing:border-box}html,body{margin:0;height:100%%}
body{display:flex;align-items:center;justify-content:center;min-height:100vh;background:var(--bg);color:var(--fg);font-family:var(--sans);-webkit-font-smoothing:antialiased}
.card{width:min(92vw,384px);background:var(--card);border:1px solid var(--edge);border-radius:16px;padding:36px 32px 22px;text-align:center;box-shadow:var(--shadow)}
.badge{--tone:%s;width:52px;height:52px;margin:0 auto 18px;border-radius:50%%;display:flex;align-items:center;justify-content:center;border:1.5px solid var(--tone);background:color-mix(in oklab,var(--tone) 13%%,transparent)}
.badge svg{width:26px;height:26px}.badge path{fill:none;stroke:var(--tone);stroke-width:2.4;stroke-linecap:round;stroke-linejoin:round}
h1{margin:0;font-size:18px;font-weight:600}.sub{margin:9px 0 0;font-size:13px;line-height:1.5;color:var(--fg-5)}
.who{margin:18px 0 2px;display:flex;align-items:center;gap:11px;text-align:left;padding:10px 12px;border:1px solid var(--edge-subtle);border-radius:11px;background:var(--tile)}
.avatar{width:34px;height:34px;flex:0 0 auto;border-radius:9px;border:1px solid var(--edge);background:var(--card);display:flex;align-items:center;justify-content:center;font-size:15px;font-weight:600}
.meta{min-width:0;flex:1}.name{font-size:13.5px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.mail{margin-top:1px;font-family:var(--mono);font-size:11.5px;color:var(--fg-3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.brand{margin:20px 0 0;font-size:11px;font-weight:500;color:var(--fg-5)}
</style></head><body><div class="card">
<div class="badge"><svg viewBox="0 0 24 24" aria-hidden="true">%s</svg></div>
<h1>%s</h1>%s%s<p class="brand">miragate</p></div></body></html>`,
		tone, icon, html.EscapeString(d.Title), who, sub))
}
