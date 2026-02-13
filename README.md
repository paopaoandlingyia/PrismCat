# 🐱 PrismCat

**PrismCat** 是一个专为开发者设计的本地 **LLM API 透传代理与流量观测工具**。

它能帮助你在本地开发大模型应用时，清晰地观测到发送给 OpenAI、Gemini 等上游的每一个字节，支持完整记录 Streaming 响应，并提供类似 Postman 的请求重放（Replay）功能。

---

## ✨ 核心特性

- 🚀 **透明反向代理**：基于域名/Host 的路由方案（如 `openai.localhost`），无需修改 SDK 路径，仅需修改 `baseURL` 即可无感接入。
- 📊 **流量全观测**：
    - 完整记录请求/响应体（支持 JSON、文本等自动美化）。
    - **流式处理 (SSE)**：完美支持流式响应记录，实时 Tee 复制，不增加转发延迟。
    - **多值 Header 支持**：遵循标准 HTTP 规范，完整保留所有 Header 信息。
- 🔐 **安全脱敏**：
    - 自动对 `Authorization`、`api-key` 等敏感头部进行掩码处理。
    - 自定义脱敏规则，防止 API Key 泄露到日志中。
- 📦 **高性能存储系统**：
    - **异步写入**：采用独立 Worker 异步落库，确保高频请求下代理稳定性。
    - **大文件分离 (Blob Store)**：自动提取 Body 中的大型 Base64（如图片），存入本地文件系统，防止数据库无限膨胀。
    - **自动清理**：支持按天设置日志保留策略，自动回收过期数据。
- 🛠️ **开发者工具柜**：
    - **Playground**：一键重放历史请求，支持 10MB 深度响应观测（带截断提示）。
    - **统计看板**：实时监控上游成功率、各状态码比例及平均延迟。
    - **多语言适配**：提供完整的中英文交互界面。

---

## 🛠️ 快速开始

### 1. 编译运行 (推荐方案)

确保你已安装 Go 1.22+ 及 Node.js 环境。

**后端构建：**
```bash
go build -o prismcat ./cmd/prismcat
```

**前端构建：**
```bash
cd web
npm install
npm run build
```
构建出的前端资源会自动嵌入 Go 二进制文件中，实现单文件分发。

**启动服务：**
```bash
./prismcat -config config.yaml
```

### 2. Docker 部署

如果你更倾向于使用容器：
```bash
docker-compose up -d
```
默认控制台地址：`http://localhost:8080`

---

## 🏗️ 架构思路：Host 路由

PrismCat 采用 **子域名路由** 策略，无需在请求路径中拼接 URL，保持了与原厂 SDK 的最大兼容性。

**示例场景：**
假设你的配置文件中定义了上游 `openai`:
- **控制台地址**：`http://localhost:8080`
- **代理域名后缀**：`localhost`
- **上游目标**：`https://api.openai.com`

在代码中，你只需修改：
```python
# OpenAI Python SDK 示例
client = OpenAI(
    base_url="http://openai.localhost:8080/v1", # 指向 PrismCat
    api_key="your-key"
)
```

---

## ⚙️ 配置说明 (`config.yaml`)

```yaml
server:
  addr: 0.0.0.0      # 监听地址
  port: 8080         # 端口
  ui_password: ""    # 控制面板密码（可选，强烈建议服务器部署时开启）
  proxy_domains:     # 用于路由的后缀域名
    - localhost

logging:
  max_request_body: 1048576       # 日志记录的最大 Body 大小 (1MB)
  sensitive_headers:             # 需要脱敏的头部
    - Authorization
    - api-key
  detach_body_over_bytes: 102400  # 超过 100KB 的 Body 存入文件系统而非 SQL

storage:
  database: "data/prismcat.db"   # SQLite 路径
  retention_days: 7              # 日志保留时长 (天)
  blob_dir: "data/blobs"         # 附件存储位置
```

---

## 🚀 进阶与优化

- **响应截断机制**：为保护浏览器内存，Playground 重放响应超过 10MB 时会自动截断并提示，确保在调试超长文本吐出时不会导致页面卡死。
- **WAL 模式**：SQLite 存储层默认开启 WAL 模式，在大量并发写入的同时，依然能保证 UI 查询的极致响应速度。

---

## 🛡️ License

[MIT License](LICENSE)
