package web

// indexHTML 是用量页，纯静态 + 前端轮询 /api/usage 渲染，风格对齐 Mirasim。
const indexHTML = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>miragate · 用量</title>
<style>
:root{
  --bg:#f7f7f8;--card:#fff;--edge:rgba(24,24,27,.1);--edge-2:rgba(24,24,27,.06);
  --fg:#18181b;--fg-2:rgba(39,39,46,.72);--fg-3:rgba(82,82,91,.55);
  --track:rgba(24,24,27,.08);--ok:#16a34a;--warn:#d97706;--err:#dc2626;--accent:#4f46e5;
  --sans:-apple-system,BlinkMacSystemFont,'Segoe UI',system-ui,'PingFang SC','Microsoft YaHei',sans-serif;
  --mono:'JetBrains Mono',ui-monospace,'SF Mono',Menlo,Consolas,monospace;
  --shadow:0 1px 2px rgba(24,24,27,.04),0 12px 30px rgba(24,24,27,.07);
}
@media(prefers-color-scheme:dark){:root{
  --bg:#0b0d11;--card:#151920;--edge:rgba(148,163,184,.16);--edge-2:rgba(148,163,184,.09);
  --fg:rgba(248,250,252,.95);--fg-2:rgba(226,232,240,.66);--fg-3:rgba(148,163,184,.5);
  --track:rgba(148,163,184,.16);--ok:#34d399;--warn:#fbbf24;--err:#f87171;--accent:#818cf8;
  --shadow:0 1px 2px rgba(0,0,0,.3),0 16px 40px rgba(0,0,0,.45);
}}
*{box-sizing:border-box}
html,body{margin:0}
body{background:var(--bg);color:var(--fg);font-family:var(--sans);-webkit-font-smoothing:antialiased;
  min-height:100vh;padding:32px 20px;display:flex;justify-content:center}
.wrap{width:min(96vw,720px)}
header{display:flex;align-items:center;justify-content:space-between;margin-bottom:20px}
.brand{display:flex;align-items:center;gap:10px;font-weight:650;font-size:16px;letter-spacing:-.01em}
.dot{width:9px;height:9px;border-radius:50%;background:var(--fg-3)}
.dot.on{background:var(--ok)}.dot.warn{background:var(--warn)}.dot.err{background:var(--err)}
.refresh{font:inherit;font-size:12.5px;color:var(--fg-2);background:var(--card);border:1px solid var(--edge);
  border-radius:9px;padding:6px 12px;cursor:pointer}
.refresh:hover{border-color:var(--fg-3)}
.card{background:var(--card);border:1px solid var(--edge);border-radius:16px;box-shadow:var(--shadow);
  padding:20px 22px;margin-bottom:16px}
.who{display:flex;align-items:center;gap:13px}
.avatar{width:44px;height:44px;flex:0 0 auto;border-radius:11px;border:1px solid var(--edge);
  display:flex;align-items:center;justify-content:center;font-size:19px;font-weight:650;background:var(--bg)}
.who .name{font-size:15px;font-weight:650}
.who .mail{font-family:var(--mono);font-size:12px;color:var(--fg-2);margin-top:2px}
.tags{display:flex;gap:7px;margin-top:9px;flex-wrap:wrap}
.tag{font-size:11.5px;padding:2px 9px;border-radius:999px;border:1px solid var(--edge-2);color:var(--fg-2);background:var(--bg)}
.tag.plan{color:var(--accent);border-color:color-mix(in oklab,var(--accent) 40%,transparent)}
.sec-title{font-size:12px;font-weight:600;color:var(--fg-3);text-transform:uppercase;letter-spacing:.05em;margin:0 0 14px}
.win{margin-bottom:18px}
.win:last-child{margin-bottom:0}
.win-head{display:flex;align-items:baseline;justify-content:space-between;margin-bottom:7px}
.win-label{font-size:14px;font-weight:600}
.win-pct{font-family:var(--mono);font-size:13px;color:var(--fg-2)}
.bar{height:9px;border-radius:999px;background:var(--track);overflow:hidden}
.bar>i{display:block;height:100%;border-radius:999px;background:var(--ok);transition:width .5s cubic-bezier(.21,.47,.32,.98)}
.bar>i.warn{background:var(--warn)}.bar>i.err{background:var(--err)}
.win-meta{display:flex;justify-content:space-between;margin-top:6px;font-size:11.5px;color:var(--fg-3)}
.foot{display:flex;justify-content:space-between;font-size:11.5px;color:var(--fg-3);margin-top:6px;padding:0 2px}
.empty{color:var(--fg-3);font-size:13.5px;text-align:center;padding:22px 0}
.hint{font-size:12px;color:var(--warn);margin-top:10px}
code{font-family:var(--mono);font-size:12px;background:var(--bg);border:1px solid var(--edge-2);border-radius:6px;padding:1px 6px}
</style>
</head>
<body>
<div class="wrap">
  <header>
    <div class="brand"><span id="statusDot" class="dot"></span>miragate</div>
    <button class="refresh" id="refreshBtn">刷新</button>
  </header>
  <div id="accountCard" class="card"></div>
  <div class="card">
    <p class="sec-title">额度用量 · 真实</p>
    <div id="usageBody"></div>
  </div>
  <div class="foot"><span id="asOf"></span><span id="src"></span></div>
</div>
<script>
const $ = (id) => document.getElementById(id);
function esc(s){return (s||'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));}
function pct(n){return (Math.round(n*10)/10)+'%';}
function fmtReset(sec){
  if(sec==null) return '';
  if(sec<=0) return '即将重置';
  const h=Math.floor(sec/3600), m=Math.floor((sec%3600)/60);
  if(h>=24){const d=Math.floor(h/24);return d+' 天 '+(h%24)+' 小时后重置';}
  if(h>0) return h+' 小时 '+m+' 分后重置';
  return m+' 分后重置';
}
function toneClass(status){return status==='limit_reached'?'err':status==='warning'?'warn':'';}

function renderAccount(a){
  if(!a||!a.loggedIn){
    $('accountCard').innerHTML='<div class="empty">尚未登录。请在终端运行 <code>miragate login</code>。</div>';
    $('statusDot').className='dot err';
    return;
  }
  const name=a.name||a.email||'Mirasim 用户';
  const initial=esc((name.trim()[0]||'M').toUpperCase());
  let tags='';
  if(a.plan) tags+='<span class="tag plan">'+esc(a.plan)+'</span>';
  if(a.tenant) tags+='<span class="tag">'+esc(a.tenant)+'</span>';
  if(a.hasDeviceKey) tags+='<span class="tag">设备签名 '+esc((a.deviceId||'').slice(0,10))+'</span>';
  $('accountCard').innerHTML=
    '<div class="who"><div class="avatar">'+initial+'</div>'+
    '<div><div class="name">'+esc(name)+'</div>'+
    (a.email?'<div class="mail">'+esc(a.email)+'</div>':'')+
    '<div class="tags">'+tags+'</div></div></div>';
}

function renderUsage(u){
  const body=$('usageBody');
  if(!u||!u.ok){
    body.innerHTML='<div class="empty">'+(u&&u.error?esc(u.error):'暂无用量数据')+'</div>';
    $('statusDot').className='dot';
    return;
  }
  $('statusDot').className='dot '+(u.status==='limit_reached'?'err':u.status==='warning'?'warn':'on');
  let html='';
  for(const w of (u.windows||[])){
    const tone=toneClass(w.status);
    html+='<div class="win"><div class="win-head">'+
      '<span class="win-label">'+esc(w.label)+'</span>'+
      '<span class="win-pct">'+pct(w.usedPercent)+' 已用</span></div>'+
      '<div class="bar"><i class="'+tone+'" style="width:'+Math.min(100,w.usedPercent)+'%"></i></div>'+
      '<div class="win-meta"><span>剩余 '+pct(w.remainingPercent)+'</span>'+
      '<span>'+esc(fmtReset(w.resetAfterSeconds))+'</span></div></div>';
  }
  if(u.degraded) html+='<div class="hint">⚠ 数据为降级估算（relay 返回 degraded）</div>';
  body.innerHTML=html||'<div class="empty">无额度窗口</div>';
}

async function load(refresh){
  try{
    const r=await fetch(refresh?'/api/refresh':'/api/usage',{cache:'no-store'});
    const d=await r.json();
    renderAccount(d.account);
    renderUsage(d.usage);
    if(d.usage&&d.usage.capturedAt){
      $('asOf').textContent='更新于 '+new Date(d.usage.capturedAt).toLocaleTimeString();
    }else{$('asOf').textContent='';}
    $('src').textContent=d.usage&&d.usage.source?d.usage.source:'';
  }catch(e){ $('statusDot').className='dot err'; }
}
$('refreshBtn').addEventListener('click',()=>load(true));
load(false);
setInterval(()=>load(false),15000);
</script>
</body>
</html>`
