# 构建阶段 1: 前端构建 (Frontend)
FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# 构建阶段 2: 后端构建 (Backend)
FROM golang:1.24-alpine AS backend-builder
WORKDIR /app

# 启用 Go 工具链自动下载
ENV GOTOOLCHAIN=auto
ENV GOWORK=off

# 安装构建依赖
RUN apk add --no-cache git

# 复制 go mod 文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码 (不包含 internal/web/dist，这将在下一步从前端构建阶段复制)
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY engine ./engine

# 复制构建好的前端资源到 internal/web/dist 以便嵌入
# 必须在 go build 之前完成
COPY --from=frontend-builder /app/web/dist ./internal/web/dist/

# 验证前端资源已复制
RUN ls -la internal/web/dist/ && echo "Frontend assets copied successfully"

# 整理依赖并编译二进制
RUN go mod tidy
RUN VERSION=$(git describe --tags --always --dirty || echo "unknown") && \
    BUILD_TIME=$(date "+%Y-%m-%d %H:%M:%S") && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -tags "with_utls nomsgpack" -ldflags "-s -w -X 'github.com/boa-z/vohive/internal/global.Version=${VERSION}' -X 'github.com/boa-z/vohive/internal/global.BuildTime=${BUILD_TIME}'" -o /app/vo-hive ./cmd/vohive

# 运行阶段 (Runtime)
FROM alpine:latest
WORKDIR /app

# 安装运行时依赖
# - ca-certificates / tzdata: 基础 HTTPS 与时区支持
RUN apk add --no-cache ca-certificates tzdata

# 复制二进制文件
COPY --from=backend-builder /app/vo-hive .

# 创建配置和数据目录
RUN mkdir -p config data logs

# 暴露端口 (API)
EXPOSE 7575

# 默认配置路径环境变量
ENV CONFIG_PATH=/app/config/config.yaml

# 入口点
ENTRYPOINT ["./vo-hive"]
CMD ["-c", "/app/config/config.yaml"]
