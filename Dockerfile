# ===================================
# API Monitor Docker Image
# ===================================
# 多阶段构建：Frontend Builder -> Go Backend Builder -> Agent Builder -> Deps Builder -> Runner

# 阶段 1: 构建前端 (Frontend Builder) - 始终在构建主机平台运行
FROM --platform=$BUILDPLATFORM node:20-slim AS frontend-builder
# CI 中复用 ci-frontend 构建好的 dist，跳过依赖安装与构建（本地构建保持原流程）
ARG USE_PREBUILT_FRONTEND=false
# 安装构建工具
# python3-fonttools / python3-brotli：字体子集化（tools/generate-simple-icons-css.mjs）
# 需要 fontTools；缺了它脚本会拒绝回退到 800KB 全量字体并直接失败（见该脚本说明）。
RUN if [ "$USE_PREBUILT_FRONTEND" != "true" ]; then \
        apt-get update && apt-get install -y --no-install-recommends \
            python3 \
            python3-fonttools \
            python3-brotli \
            make \
            g++ \
        && rm -rf /var/lib/apt/lists/*; \
    fi

WORKDIR /app

# CI 可复用独立 job 编译好的 musl 二进制(与这里同版本 Go + musl 工具链),
# 跳过 go mod download 与 go build;本地构建/发布默认仍在本阶段编译。
ARG USE_PREBUILT_BACKEND=false

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
        npm run build; \
    fi && \
    test -f /app/dist/index.html && test -d /app/dist/assets

# 5. 构建期预压缩：为文本资源生成同名 .gz / .br，运行期直接发送（见
#    backend-go/internal/server/static_precompressed.go）。放在这里而不是
#    npm run build 里面，是为了让 CI 复用 prebuilt dist 的路径也覆盖到。
#    跳过 dist/agent/（Agent 二进制下载产物）。
RUN node tools/precompress-dist.mjs /app/dist

# 阶段 2: 构建 Go 后端 (Go Backend Builder)
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine3.24 AS go-builder

# 安装必要的构建工具
RUN apk add --no-cache gcc musl-dev

# CI 可复用独立 job 编译好的 musl 二进制(同版本 Go + musl 工具链),
# 跳过 go mod download 与 go build;本地构建/正式发布默认仍在本阶段编译。
ARG USE_PREBUILT_BACKEND=false

WORKDIR /app/backend-go

# 1. 复制依赖定义并下载依赖(缓存优化)
COPY backend-go/go.mod backend-go/go.sum ./
RUN if [ "$USE_PREBUILT_BACKEND" != "true" ]; then go mod download; fi

# 2. 复制源码(USE_PREBUILT_BACKEND=true 时同时带进 CI 产物 backend-go/api-monitor)
COPY backend-go/ ./

# 3. 构建 Go 后端
# CGO_ENABLED=1 用于 SQLite3 支持
ARG TARGETOS
ARG TARGETARCH
RUN if [ "$USE_PREBUILT_BACKEND" = "true" ]; then \
        test -s ./api-monitor || { echo "USE_PREBUILT_BACKEND=true 但缺少 backend-go/api-monitor 产物" >&2; exit 1; }; \
        echo "reuse prebuilt backend binary"; \
    else \
        CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
        go build -v -trimpath \
        -ldflags="-s -w" \
        -o api-monitor ./cmd/api-monitor; \
    fi


# 阶段 3: Agent 二进制(可选,默认不打包)
# 默认镜像不再分发 Agent:三平台二进制托管在本仓库(public)的 release 里,
# 面板构建期通过 AGENT_DOWNLOAD_URL 注入直链,主机安装与自升级都从那里下载
# (每个文件都有 .sha256 sidecar 校验)。
# INCLUDE_AGENTS=true 用于离线/内网部署:需要预先把三平台产物放到 agent-rust/
# (该目录下的 agent-* 已在 .gitignore 中),否则构建直接失败而不是静默缺文件。
FROM alpine:3.24.1 AS agents
ARG INCLUDE_AGENTS=false
COPY agent-rust/ /src-bundle/
RUN set -eu; mkdir -p /out; \
    if [ "$INCLUDE_AGENTS" = "true" ]; then \
        for f in agent-linux-amd64 agent-linux-arm64 agent-windows-amd64.exe; do \
            if [ ! -s "/src-bundle/$f" ]; then \
                echo "INCLUDE_AGENTS=true 但缺少 agent-rust/$f —— 请先把三平台产物放好" >&2; \
                exit 1; \
            fi; \
            cp "/src-bundle/$f" "/out/$f"; \
            (cd /out && sha256sum "$f" | awk '{print $1}' > "$f.sha256"); \
        done; \
    fi; \
    touch /out/.empty


# 阶段 4: 运行时镜像 (Runner) - 纯净的运行环境
FROM alpine:3.24.1 AS runner
ARG INCLUDE_AGENTS=false
ARG AGENT_DOWNLOAD_URL=""
# 默认 Agent 下载源(公开 release 直链);用户设置 agent_download_url 可覆盖。
ENV AGENT_DOWNLOAD_URL=${AGENT_DOWNLOAD_URL}

LABEL org.opencontainers.image.title="API Monitor"
LABEL org.opencontainers.image.description="API聚合监控面板"
LABEL org.opencontainers.image.source="https://github.com/iwvw/api-monitor"
LABEL org.opencontainers.image.licenses="MIT"
LABEL maintainer="iwvw"

# 安装运行时依赖
# shell 任务常用工具：curl（HTTP 探测）、bind-tools（dig）、openssl、bash、jq、zip、openssh（ssh/scp）
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    tini \
    curl \
    bind-tools \
    openssl \
    bash \
    jq \
    zip \
    openssh-client \
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
# 兜底可执行位:CI 复用 artifact 时权限位可能丢失(0644),ENTRYPOINT 会失败。
RUN chmod +x /app/api-monitor

# Agent 二进制:INCLUDE_AGENTS=true 时来自 agents 阶段,否则是空目录(走远端直链)。
COPY --from=agents --chown=appuser:appuser /out/ /app/dist/agent/
RUN rm -f /app/dist/agent/.empty && ls -la /app/dist/agent

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
