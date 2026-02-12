# PrismCat 部署指南

## 目录

- [项目简介](#项目简介)
- [架构说明](#架构说明)
- [本地开发](#本地开发)
- [生产环境部署（云端）](#生产环境部署云端)
  - [前置条件](#前置条件)
  - [第一步：编译与打包](#第一步编译与打包)
  - [第二步：服务器环境准备](#第二步服务器环境准备)
  - [第三步：上传与目录结构](#第三步上传与目录结构)
  - [第四步：修改配置文件](#第四步修改配置文件)
  - [第五步：域名与 DNS 配置](#第五步域名与-dns-配置)
  - [第六步：Nginx 反向代理](#第六步nginx-反向代理)
  - [第七步：HTTPS（SSL 证书）](#第七步httpsssl-证书)
  - [第八步：使用 systemd 管理服务](#第八步使用-systemd-管理服务)
- [验证部署](#验证部署)
- [常见问题](#常见问题)

---

## 项目简介

PrismCat 🐱 是一个 **LLM API 透传代理 & 日志记录工具**。

它的核心功能是：
- **透明代理**：通过子域名路由，将请求透传到不同的上游 API（如 OpenAI, Gemini 等）
- **请求日志**：完整记录每一次 API 请求和响应，包括请求头、请求体、响应体等
- **Web 控制台**：提供美观的 Web UI，可以查看日志、统计数据、管理上游配置
- **请求重放**：可以在 Playground 中重放历史请求，方便调试

## 架构说明

```
                    ┌─────────────────────────────┐
  用户/客户端        │         Nginx (443)          │
       │           │  - SSL 终止                   │
       │           │  - 反向代理到 PrismCat         │
       ▼           └──────────┬──────────────────┘
                              │
                              ▼
                    ┌─────────────────────────────┐
                    │     PrismCat Go (8080)       │
                    │                             │
                    │  ┌─────────┐ ┌───────────┐  │
                    │  │ UI 请求  │ │ 代理请求   │  │
                    │  │ (管理面板)│ │(API透传)   │  │
                    │  └────┬────┘ └─────┬─────┘  │
                    │       │            │         │
                    │  ┌────▼────────────▼─────┐  │
                    │  │   日志记录 + SQLite     │  │
                    │  └───────────────────────┘  │
                    └─────────────────────────────┘
```

**Host 路由规则**（以域名 `prismcat.example.com` 为例）：
- `prismcat.example.com` → 进入 Web 管理面板（UI）
- `openai.prismcat.example.com` → 透传到上游 `openai`（即 `https://api.openai.com`）
- `gemini.prismcat.example.com` → 透传到上游 `gemini`

---

## 本地开发

### 前端开发模式（热更新）

```bash
# 终端 1：启动后端
go run ./cmd/prismcat/main.go

# 终端 2：启动前端 dev server（自带反向代理到 :8080）
cd web
npm install
npm run dev
```

前端 dev server 默认运行在 `http://localhost:5173`，会自动将 `/api/*` 请求代理到后端的 `http://localhost:8080`。

### 一键编译（Windows）

双击 `快速编译并运行.bat`，它会自动：
1. 构建前端 → `web/dist/`
2. 将前端产物复制到 `internal/server/ui/`（嵌入到 Go 二进制中）
3. 编译 Go 后端为 `prismcat.exe`
4. 启动程序

---

## 生产环境部署（云端）

### 前置条件

- 一台 Linux 服务器（推荐 Ubuntu 22.04 / Debian 12）
- 一个域名（如 `example.com`），并可以管理 DNS
- 服务器已安装：
  - `nginx`
  - `certbot`（用于自动获取 HTTPS 证书）

### 第一步：编译与打包

在你的开发机（Windows）上编译 **Linux 版本**：

```bash
# 1. 构建前端
cd web
npm install
npm run build
cd ..

# 2. 同步前端产物到嵌入目录
# Windows:
xcopy /s /e /y "web\dist\*" "internal\server\ui\"

# 3. 交叉编译为 Linux amd64（在 PowerShell 中）
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o prismcat ./cmd/prismcat/main.go

# 编译完成后清除环境变量，避免影响后续本地开发
Remove-Item Env:GOOS
Remove-Item Env:GOARCH
```

> 📦 编译完成后，你会得到一个名为 `prismcat` 的 Linux 可执行文件（无扩展名）。

需要上传到服务器的文件：
- `prismcat`（Linux 可执行文件）
- `config.example.yaml`（配置模板）

### 第二步：服务器环境准备

SSH 登录到你的服务器：

```bash
# 安装 nginx 和 certbot
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx
```

### 第三步：上传与目录结构

```bash
# 创建应用目录
sudo mkdir -p /opt/prismcat
cd /opt/prismcat

# 上传文件（在你的本地电脑上执行）
# scp prismcat config.example.yaml your-user@your-server:/opt/prismcat/

# 在服务器上
sudo chmod +x prismcat
sudo cp config.example.yaml config.yaml
```

最终目录结构：
```
/opt/prismcat/
├── prismcat           # 可执行文件
├── config.yaml        # 配置文件
└── data/              # 自动创建（存放 SQLite 数据库和 blob）
    ├── prismcat.db
    └── blobs/
```

### 第四步：修改配置文件

编辑 `/opt/prismcat/config.yaml`：

```yaml
server:
  port: 8080

  # ✅ 关键：添加你的域名到 ui_hosts
  # 当用户访问这些 host 时，将展示 Web 管理面板
  ui_hosts:
    - "prismcat.example.com"       # 你的管理域名
    - "localhost"                   # 保留，用于服务器本地调试
    - "127.0.0.1"

  # ✅ 关键：添加你的域名到 proxy_domains
  # PrismCat 会从请求的 Host 中提取子域名，匹配到对应的 upstream
  # 例如 openai.prismcat.example.com → upstream "openai"
  proxy_domains:
    - "prismcat.example.com"       # 你的域名

  shutdown_timeout_seconds: 10

  # 生产环境建议限制 CORS 来源（但这个项目通常不需要跨域，因为前端嵌入在后端里）
  cors_allow_origins:
    - "*"
  cors_allow_methods:
    - "GET"
    - "POST"
    - "PUT"
    - "DELETE"
    - "OPTIONS"
  cors_allow_headers:
    - "Content-Type"
    - "Authorization"

# 配置你需要代理的上游 API
upstreams:
  openai:
    target: "https://api.openai.com"
    timeout: 120
  gemini:
    target: "https://generativelanguage.googleapis.com"
    timeout: 120

logging:
  max_request_body: 10485760      # 10MB
  max_response_body: 10485760     # 10MB
  detach_body_over_bytes: 262144  # 256KB
  body_preview_bytes: 4096        # 4KB
  sensitive_headers:
    - "Authorization"
    - "x-api-key"
    - "api-key"

storage:
  database: "./data/prismcat.db"
  retention_days: 30
  blob_store: "fs"
  blob_dir: "./data/blobs"
```

> ⚠️ **重点**：把所有的 `prismcat.example.com` 替换为你自己的域名。

### 第五步：域名与 DNS 配置

在你的域名注册商（如 Cloudflare、阿里云 DNS）中添加以下记录：

| 类型 | 名称 | 值 | 说明 |
|------|------|------|------|
| A | `prismcat` | `你的服务器IP` | 管理面板域名 |
| A | `*.prismcat` | `你的服务器IP` | **泛域名**，用于匹配所有子域名（如 openai.prismcat.example.com） |

> 💡 **泛域名解析（Wildcard DNS）** 是 PrismCat 的核心依赖。没有它，新添加的 upstream 不会自动生效。
>
> 如果你的 DNS 提供商不支持泛域名，你也可以手动为每个 upstream 添加 A 记录。

### 第六步：Nginx 反向代理

创建 Nginx 配置文件：

```bash
sudo nano /etc/nginx/sites-available/prismcat
```

写入以下内容：

```nginx
# PrismCat - 主域名和泛域名
server {
    listen 80;
    server_name prismcat.example.com *.prismcat.example.com;

    # 所有请求转发到 PrismCat 后端
    location / {
        proxy_pass http://127.0.0.1:8080;

        # 传递原始 Host 头（PrismCat 靠 Host 头来做路由）
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # SSE / 流式响应支持（LLM API 常用）
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_cache off;

        # 超时设置（LLM 响应可能很慢）
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;

        # 允许大请求体
        client_max_body_size 20M;
    }
}
```

启用配置并测试：

```bash
# 创建符号链接启用配置
sudo ln -sf /etc/nginx/sites-available/prismcat /etc/nginx/sites-enabled/

# 测试配置语法
sudo nginx -t

# 重载 Nginx
sudo systemctl reload nginx
```

### 第七步：HTTPS（SSL 证书）

使用 `certbot` 自动获取免费的 Let's Encrypt 证书：

```bash
# 为主域名获取证书
sudo certbot --nginx -d prismcat.example.com

# 为泛域名获取证书（需要 DNS 验证方式）
# 注意：泛域名证书不支持 HTTP 验证，必须使用 DNS 插件
# 如果你使用的是 Cloudflare DNS，可以这样：
sudo apt install python3-certbot-dns-cloudflare
sudo certbot certonly --dns-cloudflare \
  --dns-cloudflare-credentials ~/.secrets/cloudflare.ini \
  -d "prismcat.example.com" \
  -d "*.prismcat.example.com"
```

> 💡 **泛域名证书方案**（按难度排序）：
>
> 1. **最简单：使用 Cloudflare（免费）**
>    - 将域名 DNS 托管到 Cloudflare
>    - Cloudflare 自动代理时会提供边缘 SSL，**甚至不需要自己申请证书**
>    - 只需在 Cloudflare Dashboard 中开启 Proxy（橙色云朵）
>
> 2. **自行申请：使用 certbot + DNS 插件**
>    - 支持的 DNS 提供商很多（Cloudflare、阿里云、Route53 等）
>    - 搜索 `certbot dns <你的DNS提供商>` 即可找到对应插件
>
> 3. **手动验证：certbot manual 模式**
>    ```bash
>    sudo certbot certonly --manual --preferred-challenges dns \
>      -d "prismcat.example.com" \
>      -d "*.prismcat.example.com"
>    ```
>    - certbot 会告诉你需要添加一条 TXT 记录
>    - 添加后等待几分钟再确认
>    - 缺点：每 90 天需要手动续期

获取证书后，更新 Nginx 配置以使用 HTTPS（certbot 通常会自动修改）。最终的 Nginx 配置大致如下：

```nginx
server {
    listen 80;
    server_name prismcat.example.com *.prismcat.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name prismcat.example.com *.prismcat.example.com;

    ssl_certificate /etc/letsencrypt/live/prismcat.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/prismcat.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_cache off;

        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
        client_max_body_size 20M;
    }
}
```

### 第八步：使用 systemd 管理服务

创建 systemd 服务文件，让 PrismCat 开机自动启动、崩溃自动重启：

```bash
sudo nano /etc/systemd/system/prismcat.service
```

写入：

```ini
[Unit]
Description=PrismCat - LLM API Proxy & Logger
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/prismcat
ExecStart=/opt/prismcat/prismcat -config /opt/prismcat/config.yaml
Restart=on-failure
RestartSec=5s

# 日志输出到 systemd journal
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

启用并启动服务：

```bash
# 重载 systemd 配置
sudo systemctl daemon-reload

# 启用开机自启
sudo systemctl enable prismcat

# 启动服务
sudo systemctl start prismcat

# 查看运行状态
sudo systemctl status prismcat

# 查看实时日志
sudo journalctl -u prismcat -f
```

常用命令：

```bash
sudo systemctl start prismcat     # 启动
sudo systemctl stop prismcat      # 停止
sudo systemctl restart prismcat   # 重启
sudo systemctl status prismcat    # 查看状态
sudo journalctl -u prismcat -n 50 # 查看最近 50 行日志
```

---

## 验证部署

### 1. 健康检查

```bash
# 在服务器上测试
curl http://localhost:8080/api/health
# 期望: {"status":"ok","time":"..."}
```

### 2. 测试管理面板

在浏览器访问 `https://prismcat.example.com`，你应该能看到 PrismCat 的 Web 管理面板。

### 3. 测试 API 代理

```bash
# 通过 PrismCat 代理访问 OpenAI API
curl https://openai.prismcat.example.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-xxx" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'
```

如果一切正常，你应该能在管理面板中看到这条请求的完整日志。

---

## 常见问题

### Q: 泛域名解析没有生效？

**A**: 检查 DNS 是否已生效：
```bash
dig openai.prismcat.example.com
# 应该返回你的服务器 IP
```
DNS 生效可能需要几分钟到几小时，取决于你的 DNS 提供商。

### Q: 访问子域名显示 404 或 502？

**A**: 请检查：
1. PrismCat 是否正在运行：`sudo systemctl status prismcat`
2. Nginx 配置中是否包含了 `*.prismcat.example.com`
3. `config.yaml` 中的 `proxy_domains` 是否正确配置

### Q: 流式响应（SSE）不工作？

**A**: 确认 Nginx 配置中已关闭缓冲：
```nginx
proxy_buffering off;
proxy_cache off;
```

### Q: 如何更新 PrismCat？

1. 在开发机重新编译 Linux 版本
2. 上传新的 `prismcat` 可执行文件到服务器
3. 重启服务：`sudo systemctl restart prismcat`

### Q: 如何备份数据？

PrismCat 的所有数据都在 `/opt/prismcat/data/` 目录下：
- `prismcat.db` — SQLite 数据库（日志记录）
- `blobs/` — 大 body 存储

直接备份这个目录即可。

### Q: 可以不用 Nginx 吗？

可以，如果你不需要 HTTPS，可以让 PrismCat 直接监听公网端口：
```yaml
server:
  port: 80  # 或其他公网端口
```
但强烈建议使用 Nginx + HTTPS，以保障通信安全（API Key 等敏感信息在传输中应该被加密）。

### Q: 可以使用 Docker 吗？

当前没有提供 Dockerfile，但你可以轻松创建一个：
```dockerfile
FROM debian:bookworm-slim
WORKDIR /app
COPY prismcat config.yaml ./
RUN mkdir -p data
EXPOSE 8080
CMD ["./prismcat", "-config", "config.yaml"]
```
