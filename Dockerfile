# ===================================
# API Monitor Docker Image
# ===================================
# 多阶段构建：Deps -> Builder -> Runner

# 阶段 1: 依赖安装 (包含 devDependencies)
FROM node:20-alpine AS deps
RUN apk add --no-cache python3 make g++
WORKDIR /app
COPY package.json package-lock.json ./
# 这里必须安装所有依赖，因为 build 需要 vite
RUN npm install --legacy-peer-deps

# 阶段 2: 构建前端
FROM node:20-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./
COPY . .
# 设置环境变量为 production (某些构建脚本可能会用到)
ENV NODE_ENV=production
# 执行构建，生成 dist 目录
RUN npm run build

# 阶段 3: 运行时镜像 (仅包含生产依赖)
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

# 安装仅生产依赖 (为了减小体积，这里重新安装一次 production deps)
COPY package.json package-lock.json ./
RUN npm install --only=production --legacy-peer-deps && npm cache clean --force

# 从 Builder 阶段复制构建好的前端资源
COPY --from=builder --chown=nodejs:nodejs /app/dist ./

# 复制后端源码
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