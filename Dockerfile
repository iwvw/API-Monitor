# ===================================
# API Monitor Docker Image
# ===================================
# 多阶段构建：Frontend Builder -> Go Backend Builder -> Agent Builder -> Deps Builder -> Runner

# 阶段 1: 构建前端 (Frontend Builder) - 始终在构建主机平台运行
FROM --platform=$BUILDPLATFORM node:20-slim AS frontend-builder
# 安装构建工具
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    make \
    g++ \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# 1. 复制依赖定义
COPY package.json package-lock.json ./

# 设置镜像源
RUN npm config set registry https://registry.npmmirror.com

# 2. 直接安装所有依赖 (确保 vite 可用)
# 注意：不使用 --only=production，确保安装 devDependencies
# 添加 --ignore-scripts 以绕过部分依赖包内置的 pnpm 强制检查 (如 only-allow)
RUN npm install --legacy-peer-deps --ignore-scripts

# 3. 复制源码
COPY . .

# 4. 执行构建
# 显式设置 PATH (虽然 npm run 通常不需要，但以防万一)
# 禁用 CDN 模式，所有依赖打包到本地
ENV PATH=/app/node_modules/.bin:$PATH \
    VITE_USE_CDN=false
RUN npm run build

# 阶段 2: 构建 Go 后端 (Go Backend Builder)
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS go-builder

# 安装必要的构建工具
RUN apk add --no-cache gcc musl-dev

WORKDIR /app/backend-go

# 1. 复制依赖定义并下载依赖（缓存优化）
COPY backend-go/go.mod backend-go/go.sum ./
RUN go mod download

# 2. 复制源码
COPY backend-go/ ./

# 3. 构建 Go 后端
# CGO_ENABLED=1 用于 SQLite3 支持
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -v -trimpath \
    -ldflags="-s -w" \
    -o api-monitor ./cmd/api-monitor

# 阶段 3: 构建 Rust Agent 二进制 (Agent Builder) - 优化为基于 TARGETARCH 进行条件式本机编译，以最大化编译性能并防止复杂的跨平台交叉编译错误
FROM --platform=$TARGETPLATFORM rust:slim AS agent-builder
RUN apt-get update && apt-get install -y --no-install-recommends \
    musl-tools \
    gcc \
    && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH
RUN if [ "$TARGETARCH" = "amd64" ]; then \
        rustup target add x86_64-unknown-linux-musl; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
        rustup target add aarch64-unknown-linux-musl; \
    fi

WORKDIR /app/agent-rust
COPY agent-rust/Cargo.toml agent-rust/Cargo.lock ./
# 预拉取和预编译依赖项（层缓存优化）
RUN mkdir src && echo "fn main() {}" > src/main.rs
RUN if [ "$TARGETARCH" = "amd64" ]; then \
        cargo build --release --target x86_64-unknown-linux-musl; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
        cargo build --release --target aarch64-unknown-linux-musl; \
    fi

# 1. 尝试从构建上下文（宿主机）复制已编好的二进制文件（如果存在）
RUN rm -rf src
COPY agent-rust/ ./

# 2. 复制真正的源码并执行本机编译
RUN if [ "$TARGETARCH" = "amd64" ]; then \
        if [ ! -f "./agent-linux-amd64" ] || [ ! -s "./agent-linux-amd64" ]; then \
            cargo build --release --target x86_64-unknown-linux-musl && \
            cp target/x86_64-unknown-linux-musl/release/api-monitor-agent ./agent-linux-amd64; \
        fi && \
        if [ ! -f "./agent-linux-arm64" ]; then touch ./agent-linux-arm64; fi && \
        if [ ! -f "./agent-windows-amd64.exe" ]; then touch ./agent-windows-amd64.exe; fi; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
        if [ ! -f "./agent-linux-arm64" ] || [ ! -s "./agent-linux-arm64" ]; then \
            cargo build --release --target aarch64-unknown-linux-musl && \
            cp target/aarch64-unknown-linux-musl/release/api-monitor-agent ./agent-linux-arm64; \
        fi && \
        if [ ! -f "./agent-linux-amd64" ]; then touch ./agent-linux-amd64; fi && \
        if [ ! -f "./agent-windows-amd64.exe" ]; then touch ./agent-windows-amd64.exe; fi; \
    fi

# 注意：deps-builder 阶段已移除，Go 后端不需要 Node.js 依赖

# 阶段 4: 运行时镜像 (Runner) - 纯净的运行环境
FROM --platform=$TARGETPLATFORM alpine:3.20 AS runner

LABEL org.opencontainers.image.title="API Monitor"
LABEL org.opencontainers.image.description="API聚合监控面板"
LABEL org.opencontainers.image.source="https://github.com/iwvw/api-monitor"
LABEL org.opencontainers.image.licenses="MIT"
LABEL maintainer="iwvw"

# 安装运行时依赖
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    tini \
    && addgroup -g 1001 appuser \
    && adduser -D -u 1001 -G appuser appuser

WORKDIR /app

# 创建数据目录
RUN mkdir -p /app/data /app/dist/agent && chown -R appuser:appuser /app

# 1. 从 frontend-builder 复制构建好的前端资源
COPY --from=frontend-builder --chown=appuser:appuser /app/dist ./dist

# 2. 从 go-builder 复制 Go 后端二进制文件
COPY --from=go-builder --chown=appuser:appuser /app/backend-go/api-monitor /app/

# 3. 将 Rust Agent 二进制文件放入 dist/agent 目录以便静态服务
COPY --from=agent-builder --chown=appuser:appuser /app/agent-rust/agent-linux-amd64 /app/dist/agent/
COPY --from=agent-builder --chown=appuser:appuser /app/agent-rust/agent-linux-arm64 /app/dist/agent/
COPY --from=agent-builder --chown=appuser:appuser /app/agent-rust/agent-windows-amd64.exe /app/dist/agent/

# 环境变量配置
ENV PORT=3000 \
    DATA_DIR=/app/data \
    DB_NAME=data.db \
    LOG_LEVEL=INFO \
    TZ=UTC

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3000/health || exit 1

USER appuser

ENTRYPOINT ["/sbin/tini", "--"]

CMD ["/app/api-monitor"]
