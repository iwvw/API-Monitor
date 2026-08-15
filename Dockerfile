# ===================================
# API Monitor Docker Image
# ===================================
# 多阶段构建：Frontend Builder -> Go Backend Builder -> Agent Builder -> Deps Builder -> Runner

# 阶段 1: 构建前端 (Frontend Builder) - 始终在构建主机平台运行
FROM --platform=$BUILDPLATFORM node:20-slim AS frontend-builder
# CI 中复用 ci-frontend 构建好的 dist，跳过依赖安装与构建（本地构建保持原流程）
ARG USE_PREBUILT_FRONTEND=false
# 安装构建工具
RUN if [ "$USE_PREBUILT_FRONTEND" != "true" ]; then \
        apt-get update && apt-get install -y --no-install-recommends \
            python3 \
            make \
            g++ \
        && rm -rf /var/lib/apt/lists/*; \
    fi

WORKDIR /app

# 1. 复制依赖定义
COPY package.json package-lock.json ./

# 设置镜像源
RUN npm config set registry https://registry.npmjs.org

# 2. 直接安装所有依赖 (确保 vite 可用)
# 注意：不使用 --only=production，确保安装 devDependencies
# 添加 --ignore-scripts 以绕过部分依赖包内置的 pnpm 强制检查 (如 only-allow)
RUN if [ "$USE_PREBUILT_FRONTEND" != "true" ]; then \
        npm ci --legacy-peer-deps --ignore-scripts; \
    fi

# 3. 复制源码（含 CI 下载的 frontend-dist 目录）
COPY . .

# 4. 执行构建或复用现成 dist
# 显式设置 PATH (虽然 npm run 通常不需要，但以防万一)
# 禁用 CDN 模式，所有依赖打包到本地
# 注意：APP_VERSION 声明必须紧贴此处——若放在阶段开头，每次构建值变化
# 会导致本阶段所有层（apt-get/npm ci 等）的缓存键失效，前端链 100% miss
ARG APP_VERSION=dev-local
ENV PATH=/app/node_modules/.bin:$PATH \
    VITE_USE_CDN=false \
    VITE_APP_VERSION=${APP_VERSION}
RUN if [ "$USE_PREBUILT_FRONTEND" = "true" ]; then \
        mkdir -p /app/dist && cp -r /app/frontend-dist/. /app/dist/; \
    else \
        npm run drawio:install && npm run build; \
    fi && \
    test -f /app/dist/index.html && test -d /app/dist/assets

# 阶段 2: 构建 Go 后端 (Go Backend Builder)
FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine3.24 AS go-builder

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
FROM rust:slim AS agent-builder
ARG USE_PREBUILT_AGENT=false
RUN if [ "$USE_PREBUILT_AGENT" != "true" ]; then \
        apt-get update && apt-get install -y --no-install-recommends \
            musl-tools \
            gcc \
        && rm -rf /var/lib/apt/lists/*; \
    fi

ARG TARGETARCH
RUN if [ "$USE_PREBUILT_AGENT" != "true" ]; then \
        if [ "$TARGETARCH" = "amd64" ]; then \
            rustup target add x86_64-unknown-linux-musl; \
        elif [ "$TARGETARCH" = "arm64" ]; then \
            rustup target add aarch64-unknown-linux-musl; \
        fi; \
    fi

WORKDIR /app/agent-rust
COPY agent-rust/ ./
# CI downloads prebuilt agent artifacts before docker build. Local builds still
# fall back to compiling the target Linux agent when the artifact is absent.
RUN if [ "$TARGETARCH" = "amd64" ]; then \
        if [ ! -f "./agent-linux-amd64" ] || [ ! -s "./agent-linux-amd64" ]; then \
            if [ "$USE_PREBUILT_AGENT" = "true" ]; then \
                echo "agent-linux-amd64 artifact is required when USE_PREBUILT_AGENT=true" >&2; \
                exit 1; \
            fi; \
            cargo build --release --target x86_64-unknown-linux-musl && \
            cp target/x86_64-unknown-linux-musl/release/api-monitor-agent ./agent-linux-amd64; \
        fi && \
        if [ ! -f "./agent-linux-arm64" ]; then touch ./agent-linux-arm64; fi && \
        if [ ! -f "./agent-windows-amd64.exe" ]; then touch ./agent-windows-amd64.exe; fi; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
        if [ ! -f "./agent-linux-arm64" ] || [ ! -s "./agent-linux-arm64" ]; then \
            if [ "$USE_PREBUILT_AGENT" = "true" ]; then \
                echo "agent-linux-arm64 artifact is required when USE_PREBUILT_AGENT=true" >&2; \
                exit 1; \
            fi; \
            cargo build --release --target aarch64-unknown-linux-musl && \
            cp target/aarch64-unknown-linux-musl/release/api-monitor-agent ./agent-linux-arm64; \
        fi && \
        if [ ! -f "./agent-linux-amd64" ]; then touch ./agent-linux-amd64; fi && \
        if [ ! -f "./agent-windows-amd64.exe" ]; then touch ./agent-windows-amd64.exe; fi; \
    fi && \
    chmod +x ./agent-linux-amd64 ./agent-linux-arm64 || true

# 注意：deps-builder 阶段已移除，Go 后端不需要 Node.js 依赖

# 阶段 4: 运行时镜像 (Runner) - 纯净的运行环境
FROM alpine:3.24.1 AS runner

LABEL org.opencontainers.image.title="API Monitor"
LABEL org.opencontainers.image.description="API聚合监控面板"
LABEL org.opencontainers.image.source="https://github.com/iwvw/api-monitor"
LABEL org.opencontainers.image.licenses="MIT"
LABEL maintainer="iwvw"

# 安装运行时依赖
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    tini \
    && apk upgrade --no-cache \
    && addgroup -g 1001 appuser \
    && adduser -D -u 1001 -G appuser appuser

WORKDIR /app

# 创建数据目录
RUN mkdir -p /app/data /app/dist/agent && chown -R appuser:appuser /app

# 1. 从 frontend-builder 复制构建好的前端资源
COPY --from=frontend-builder --chown=appuser:appuser /app/dist ./dist
RUN test -f /app/dist/index.html && test -d /app/dist/assets

# 2. 从 go-builder 复制 Go 后端二进制文件
COPY --from=go-builder --chown=appuser:appuser /app/backend-go/api-monitor /app/

# 3. 将 Rust Agent 二进制文件放入 dist/agent 目录以便静态服务
COPY --from=agent-builder --chown=appuser:appuser /app/agent-rust/agent-linux-amd64 /app/dist/agent/
COPY --from=agent-builder --chown=appuser:appuser /app/agent-rust/agent-linux-arm64 /app/dist/agent/
COPY --from=agent-builder --chown=appuser:appuser /app/agent-rust/agent-windows-amd64.exe /app/dist/agent/
RUN cd /app/dist/agent && \
    sha256sum agent-linux-amd64 | awk '{print $1}' > agent-linux-amd64.sha256 && \
    sha256sum agent-linux-arm64 | awk '{print $1}' > agent-linux-arm64.sha256 && \
    sha256sum agent-windows-amd64.exe | awk '{print $1}' > agent-windows-amd64.exe.sha256

# 4. 复制 2FA 浏览器插件目录
COPY --chown=appuser:appuser plugin /app/plugin


# 环境变量配置
ENV PORT=3000 \
    DATA_DIR=/app/data \
    DB_NAME=data.db \
    LOG_LEVEL=INFO \
    TZ=UTC

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget -q -T 5 -O - http://localhost:3000/health >/dev/null || exit 1

USER appuser

ENTRYPOINT ["/sbin/tini", "--"]

CMD ["/app/api-monitor"]
