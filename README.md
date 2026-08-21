# miragate

轻量的 Mirasim 本地网关：登录后启动一个本地反向代理，供 Claude Code 等使用 Anthropic 接口的
CLI 直接接入；并附带一个用量页查看真实额度。单静态二进制 / 单容器，部署简单。

---

## 一、Docker 部署（推荐）

### 1. 准备

镜像由 GitHub Actions 构建并推送到 GHCR：`ghcr.io/dotracel/miragate`
（若 fork 到自己的仓库，把 `docker-compose.yml` 里的镜像地址换成你的即可）。

私有镜像需先登录 GHCR：

```bash
echo <你的GitHubToken> | docker login ghcr.io -u <你的用户名> --password-stdin
docker compose pull
```

> 也可不用镜像、本地构建：把 compose 里的 `image:` 注释掉，启用 `build: .`。

### 2. 登录（一次性）

容器内推荐用**邮箱验证码**登录，凭据会写入数据卷持久保存：

```bash
docker compose run --rm miragate login --email you@example.com
# 按提示输入邮箱收到的验证码
```

### 3. 启动服务

```bash
docker compose up -d
```

- 反代地址：`http://<主机IP>:8788`
- 用量页：浏览器打开 `http://<主机IP>:8788/`

### 4. 让 CLI 接入

在使用 Claude Code 的机器上：

```bash
export ANTHROPIC_BASE_URL=http://<主机IP>:8788
export ANTHROPIC_AUTH_TOKEN=placeholder   # 占位即可
claude
```

### 5. 常用运维

```bash
docker compose logs -f          # 查看日志
docker compose exec miragate miragate status   # 查看登录态与用量
docker compose restart          # 重启
docker compose down             # 停止（数据卷保留登录态）
```

---

## 二、出站代理（http / socks5）

若服务器需经代理访问外网，在 `docker-compose.yml` 的 `environment` 中设置即可
（三选一，`ALL_PROXY` 会自动作为 HTTP/HTTPS 代理的回退）：

```yaml
environment:
  - HTTP_PROXY=http://proxy-host:8080
  - HTTPS_PROXY=http://proxy-host:8080
  # 或使用 socks5：
  - ALL_PROXY=socks5://proxy-host:1080
  # 带账号密码：
  # - ALL_PROXY=socks5://user:pass@proxy-host:1080
  # 例外直连：
  # - NO_PROXY=localhost,127.0.0.1
```

改完执行 `docker compose up -d` 生效。

---

## 三、不用 Docker（可选）

需要 Go 1.21+：

```bash
go build -ldflags="-s -w" -o miragate ./cmd/miragate
./miragate login          # 邮箱或 OAuth（本机可用浏览器时支持 OAuth）
./miragate serve --listen 127.0.0.1:8788
```

代理同样通过环境变量设置：`HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY`。

---

## 命令一览

| 命令 | 说明 |
|------|------|
| `miragate login [--email <addr>] [--oauth github\|google]` | 登录 |
| `miragate serve [--listen 0.0.0.0:8788]` | 启动反代 + 用量页 |
| `miragate status` | 查看登录态与实时用量 |
| `miragate logout` | 退出登录 |
| `miragate env` | 输出供 CLI 使用的环境变量 |

## 说明

- 需要有效账户方能登录；本工具不绕过任何鉴权。
- 数据卷 `miragate-data`（容器内 `/data`）保存登录凭据，请勿泄露。
