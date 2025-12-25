# ===================================
# API Monitor Docker Image
# ===================================
# 多阶段构建：Builder -> Runner

# 阶段 1: 构建前端 (Builder)
FROM node:20-alpine AS builder
# 安装构建工具
RUN apk add --no-cache python3 make g++
WORKDIR /app

# 1. 复制依赖定义
COPY package.json package-lock.json ./

# 设置镜像源
RUN npm config set registry https://registry.npmmirror.com

# 2. 直接安装所有依赖 (确保 vite 可用)
# 注意：不使用 --only=production，确保安装 devDependencies
RUN npm install --legacy-peer-deps

# 3. 复制源码
COPY . .

# 4. 执行构建
# 显式设置 PATH (虽然 npm run 通常不需要，但以防万一)
# 设置 CDN 环境变量，启用 CDN 模式以减小打包体积
ENV PATH=/app/node_modules/.bin:$PATH \
    VITE_USE_CDN=true \
    VITE_CDN_PROVIDER=jsdelivr
RUN npm run build

# 阶段 2: 构建 Go Agent 二进制 (Agent Builder)
FROM golang:1.21-alpine AS agent-builder
WORKDIR /app/agent-go
# 安装构建工具
RUN apk add --no-cache upx
# 复制 Go 模块文件
COPY agent-go/go.mod agent-go/go.sum ./
RUN go mod download
# 复制源码并构建
COPY agent-go/ .
# 构建 Linux amd64 和 arm64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o agent-linux-amd64 && \
    upx --best agent-linux-amd64 || true
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o agent-linux-arm64 && \
    upx --best agent-linux-arm64 || true
# 构建 Windows amd64
RUN CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o agent-windows-amd64.exe

# 阶段 3: 运行时镜像 (Runner)
FROM node:20-alpine AS runner

LABEL org.opencontainers.image.title="API Monitor"
LABEL org.opencontainers.image.description="API聚合监控面板"
LABEL org.opencontainers.image.source="https://github.com/iwvw/api-monitor"
LABEL org.opencontainers.image.licenses="MIT"
LABEL maintainer="iwvw"

RUN apk add --no-cache curl tini && rm -rf /var/cache/apk/*

RUN addgroup -g 1001 -S nodejs && adduser -S nodejs -u 1001

WORKDIR /app

# 创建数据目录
RUN mkdir -p /app/config /app/data && chown -R nodejs:nodejs /app

# 1. 复制依赖定义
COPY package.json package-lock.json ./

# 设置镜像源
RUN npm config set registry https://registry.npmmirror.com

# 2. 仅安装生产依赖 (减小体积)
RUN npm install --only=production --legacy-peer-deps && npm cache clean --force

# 3. 从各阶段复制构建好的资源
COPY --from=builder --chown=nodejs:nodejs /app/dist ./dist
# 将 Go Agent 二进制文件放入 dist/agent 目录以便静态服务
RUN mkdir -p /app/dist/agent
COPY --from=agent-builder --chown=nodejs:nodejs /app/agent-go/agent-linux-amd64 /app/dist/agent/
COPY --from=agent-builder --chown=nodejs:nodejs /app/agent-go/agent-linux-arm64 /app/dist/agent/
COPY --from=agent-builder --chown=nodejs:nodejs /app/agent-go/agent-windows-amd64.exe /app/dist/agent/

# 4. 复制后端源码
COPY --chown=nodejs:nodejs . .

ENV NODE_ENV=production \
    PORT=3000 \
    CONFIG_DIR=/app/config

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3000/ || exit 1

USER nodejs

ENTRYPOINT ["/sbin/tini", "--"]

CMD ["node", "server.js"]
