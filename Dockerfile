# --- 第一阶段：构建前端 ---
FROM node:20-slim AS frontend-builder
WORKDIR /web
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# --- 第二阶段：构建后端 ---
FROM golang:1.22-bookworm AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 将前端构建产物复制到后端嵌入目录
COPY --from=frontend-builder /web/dist ./internal/server/ui
# 静态编译后端
RUN CGO_ENABLED=0 GOOS=linux go build -o prismcat ./cmd/prismcat

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

# 默认启用环境变量覆盖
CMD ["./prismcat", "-config", "config.yaml"]