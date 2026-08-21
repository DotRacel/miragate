# miragate

轻量的 Mirasim 本地网关：登录后启动一个本地反向代理，供 Claude Code 等使用 Anthropic 接口的
CLI 直接接入；并附带一个用量页查看真实额度。单静态二进制 / 单容器，部署简单。

---

## 一、Docker 部署（推荐）

### 1. 准备

直接 `git clone` 本仓库，有现成可用的 `docker-compose.yaml`
镜像由 GitHub Actions 构建并推送到 GHCR：`ghcr.io/dotracel/miragate`

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

或者你也可以使用 CLIProxyAPI / Sub2API / NewAPI 进行接入。

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
