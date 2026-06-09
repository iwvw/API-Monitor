# ===================================
# API Monitor Docker Image
# ===================================
# 多阶段构建：Builder -> Native Deps Builder -> Runner

# 阶段 1: 构建前端 (Builder) - 始终在构建主机平台运行
FROM --platform=$BUILDPLATFORM node:20-slim AS builder
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

# 阶段 2: 构建 Rust Agent 二进制 (Agent Builder) - 优化为基于 TARGETARCH 进行条件式本机编译，以最大化编译性能并防止复杂的跨平台交叉编译错误
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
COPY agent-rust/agent-linux-amd6[4] ./
COPY agent-rust/agent-linux-arm6[4] ./
COPY agent-rust/agent-windows-amd64.ex[e] ./

# 2. 复制真正的源码并执行本机编译
COPY agent-rust/src ./src
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

# 阶段 3: 预构建生产依赖 (Native Deps Builder)
# 为目标平台安装原生模块
FROM --platform=$TARGETPLATFORM node:20-slim AS deps-builder
# 安装构建工具 (用于编译 better-sqlite3 等原生模块，以防预编译不可用)
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    make \
    g++ \
    curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
# 复制依赖定义
COPY package.json package-lock.json ./
# 设置镜像源
RUN npm config set registry https://registry.npmmirror.com
# 尝试使用预编译二进制，如果不可用则编译
# better-sqlite3 支持 prebuild，会自动下载预编译的 .node 文件
ENV npm_config_build_from_source=false
# 使用 --ignore-scripts 绕过强制包管理器检查，然后单独 rebuild 原生模块
RUN npm install --omit=dev --legacy-peer-deps --ignore-scripts && \
    npm rebuild better-sqlite3 && \
    npm cache clean --force

# 阶段 4: 运行时镜像 (Runner) - 纯净的运行环境
FROM --platform=$TARGETPLATFORM node:20-slim AS runner

LABEL org.opencontainers.image.title="API Monitor"
LABEL org.opencontainers.image.description="API聚合监控面板"
LABEL org.opencontainers.image.source="https://github.com/iwvw/api-monitor"
LABEL org.opencontainers.image.licenses="MIT"
LABEL maintainer="iwvw"

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    ca-certificates \
    tini \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd -g 1001 nodejs && useradd -m -u 1001 -g nodejs nodejs

WORKDIR /app

# 创建数据目录
RUN mkdir -p /app/config /app/data && chown -R nodejs:nodejs /app

# 1. 从 deps-builder 复制预构建的 node_modules (避免在 runner 中编译)
COPY --from=deps-builder --chown=nodejs:nodejs /app/node_modules ./node_modules
COPY --from=deps-builder --chown=nodejs:nodejs /app/package.json ./

# 2. 从 builder 复制构建好的前端资源
COPY --from=builder --chown=nodejs:nodejs /app/dist ./dist

# 3. 将 Rust Agent 二进制文件放入 dist/agent 目录以便静态服务
RUN mkdir -p /app/dist/agent
COPY --from=agent-builder --chown=nodejs:nodejs /app/agent-rust/agent-linux-amd64 /app/dist/agent/
COPY --from=agent-builder --chown=nodejs:nodejs /app/agent-rust/agent-linux-arm64 /app/dist/agent/
COPY --from=agent-builder --chown=nodejs:nodejs /app/agent-rust/agent-windows-amd64.exe /app/dist/agent/

# 4. 复制后端源码 (不包含 node_modules)
COPY --chown=nodejs:nodejs server.js ./
COPY --chown=nodejs:nodejs src ./src
COPY --chown=nodejs:nodejs modules ./modules

ENV NODE_ENV=production \
    PORT=3000 \
    CONFIG_DIR=/app/config \
    LOW_MEMORY_MODE=1 \
    LAZY_MODULE_ROUTES=1 \
    GEOIP_LOOKUP=0 \
    JSON_BODY_LIMIT=5mb \
    UPLOAD_MAX_FILE_SIZE_MB=50 \
    NODE_OPTIONS=--max-old-space-size=128

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3000/ || exit 1

USER nodejs

ENTRYPOINT ["/usr/bin/tini", "--"]

CMD ["node", "server.js"]
