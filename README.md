# PrismCat

**PrismCat** 是一个专为开发者设计的本地 **LLM API 透传代理与流量观测工具**。

它能帮助你在本地开发大模型应用时，清晰地观测到发送给上游的每一个字节，支持完整记录 Streaming 响应，并提供类似 Postman 的请求重放（Replay）功能。

---

## ✨ 核心特性

- 🚀 **透明反向代理**：基于子域名/Host 的路由方案（如 `openai.localhost`），无需修改 SDK 路径，仅需修改 `baseURL` 即可无感接入。
- 📊 **流量全观测**：
    - 完整记录请求/响应体（支持 JSON、文本等自动美化）。
    - **流式处理 (SSE)**：完美支持流式响应记录，实时 Tee 复制，不增加转发延迟。
- 🔐 **安全脱敏**：自动对 `Authorization`、`api-key` 等敏感头部进行掩码处理，防止敏感信息流失。
- 📦 **高性能存储系统**：
    - **异步写入**：独立 Worker 异步落库，确保高频请求下代理稳定性。
    - **大文件分离**：自动提取 Body 中的大型 Base64 附件至本地文件系统，防止数据库膨胀。
- 🛠️ **开发者工具柜**：提供 **Playground** 重放功能、实时统计看板及完整的中英文界面。

---

## 🛠️ 快速开始

### 1. 运行二进制文件 (推荐)
前往 [Releases](https://github.com/paopaoandlingyia/PrismCat/releases) 下载对应系统的压缩包。
- **Windows**: 双击 `prismcat.exe` 启动。程序会自动隐藏至系统托盘，右键即可打开控制面板。
- **Linux/macOS**: 执行 `./prismcat`。

### 2. Docker 部署
```yaml
services:
  prismcat:
    image: ghcr.io/paopaoandlingyia/prismcat:latest
    container_name: prismcat
    ports:
      - "8080:8080"
    environment:
      # 允许通过哪些地址访问控制面板 (如果不加，公网 IP 访问可能会失效)
      - PRISMCAT_UI_HOSTS=localhost,127.0.0.1
      # 代理基础域名 (例如设为 example.com 后，访问 openai.example.com 即可触发代理)
      - PRISMCAT_PROXY_DOMAINS=localhost,example.com
      # 若允许外网访问请务必设置此项并修改
      - PRISMCAT_UI_PASSWORD=your_strong_password
      - PRISMCAT_RETENTION_DAYS=30
    volumes:
      - ./data:/app/data
    restart: always
```

---

## 🏗️ 核心概念：子域名路由

PrismCat 采用 **子域名路由** 策略，保持了与原厂 SDK 的最大兼容性。

**示例场景：**
假设你的配置文件中定义了上游名称为 `openai`, 代理后缀为 `localhost`:

在代码中，你只需修改 `base_url`:
```python
# OpenAI Python SDK 示例
client = OpenAI(
    base_url="http://openai.localhost:8080/v1", # 指向 PrismCat 路由
    api_key="sk-..."
)
```

---

## 🌐 生产部署建议 (Nginx)

在公网环境部署时，建议使用泛域名解析（如 `*.prismcat.example.com`）并配置 Nginx 反向代理：

```nginx
server {
    listen 80;
    server_name prismcat.example.com *.prismcat.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host; # 必须：透传原始 Host 用于 PrismCat 路由
        
        # SSE / 流式响应优化
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;
        
        client_max_body_size 50M;
    }
}
```

---

## ⚙️ 配置说明 (`config.yaml`)

配置文件默认位于 `data/config.yaml`（首次启动自动创建）。

```yaml
server:
  port: 8080
  ui_password: ""    # 控制面板 Basic Auth 密码
  proxy_domains:     # 匹配的后缀域名
    - localhost

logging:
  max_request_body: 1048576       # 单条记录最大上限 (1MB)
  sensitive_headers:             # 自动脱敏的 Header
    - Authorization
    - api-key
  detach_body_over_bytes: 262144  # 超过 256KB 的数据存入磁盘附件区

storage:
  retention_days: 7              # 日志保留时长 (天)
```

---

## 🛡️ License

[MIT License](LICENSE)
