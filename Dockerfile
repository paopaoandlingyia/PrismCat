# --- 第一阶段：构建前端 ---
FROM node:20-slim AS frontend-builder
WORKDIR /web
COPY web/package*.json ./
# 既然是全自动构建，建议使用 npm ci 确保版本锁定
RUN npm install
COPY web/ ./
RUN npm run build

# --- 第二阶段：构建后端 ---
# 使用更高的 Go 版本镜像，以支持 go.mod 中的 1.25.x 需求
FROM golang:1.24-bookworm AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
# 如果遇到 go.mod 版本过高，自动下载对应的工具链
RUN go mod download
COPY . .
# 将前端构建产物复制到后端嵌入目录
COPY --from=frontend-builder /web/dist ./internal/server/ui
# 静态编译后端
RUN CGO_ENABLED=0 GOOS=linux go build -v -o prismcat ./cmd/prismcat

# --- 第三阶段：最终镜像 ---
FROM debian:bookworm-slim
WORKDIR /app

# 安装必要的根证书（用于 HTTPS 请求上游）
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=backend-builder /app/prismcat .
COPY config.example.yaml ./config.yaml

# 创建数据目录并声明卷
RUN mkdir -p data
VOLUME ["/app/data"]

EXPOSE 8080

CMD ["./prismcat", "-config", "config.yaml"]