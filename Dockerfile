# ===================================
# API Monitor Docker Image
# ===================================
# 多阶段构建：Builder -> Runner

# 阶段 1: 构建前端 (Builder)
FROM node:20-alpine AS builder
# 安装构建工具
RUN apk add --no-cache python3 make g++
WORKDIR /app
COPY package.json package-lock.json ./
# 显式安装所有依赖 (包括 devDependencies)
RUN npm install --legacy-peer-deps
COPY . .
# 设置环境变量为 production
ENV NODE_ENV=production
# 执行构建，生成 dist 目录
RUN npm run build

# 阶段 2: 运行时镜像 (Runner)
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