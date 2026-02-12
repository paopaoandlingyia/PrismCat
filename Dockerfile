# --- 第一阶段：构建前端 ---
FROM node:20-slim AS frontend-builder
WORKDIR /web
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# --- 第二阶段：构建后端 ---
FROM golang:latest AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
ENV GOTOOLCHAIN=auto
RUN go mod download
COPY . .
COPY --from=frontend-builder /web/dist ./internal/server/ui
# 必须使用 CGO_ENABLED=0，这样二进制文件才能在 Alpine 的 musl 环境下完美运行
RUN CGO_ENABLED=0 GOOS=linux go build -v -o prismcat ./cmd/prismcat

# --- 第三阶段：最终镜像（切换为 Alpine） ---
FROM alpine:latest
WORKDIR /app

# 安装必要的根证书（Alpine 使用 apk）
RUN apk add --no-cache ca-certificates tzdata

COPY --from=backend-builder /app/prismcat .
COPY config.example.yaml ./config.yaml

# 创建数据目录并声明卷
RUN mkdir -p data
VOLUME ["/app/data"]

EXPOSE 8080

# 增加健康检查
# 每 30 秒检查一次，如果连续 3 次失败，容器会被标记为 unhealthy
# 启动 5 秒后开始检查
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/health || exit 1

CMD ["./prismcat", "-config", "config.yaml"]