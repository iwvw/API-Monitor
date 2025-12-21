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

# 2. 直接安装所有依赖 (确保 vite 可用)
# 注意：不使用 --only=production，确保安装 devDependencies
RUN npm install --legacy-peer-deps

# 3. 复制源码
COPY . .

# 4. 执行构建
# 显式设置 PATH (虽然 npm run 通常不需要，但以防万一)
ENV PATH /app/node_modules/.bin:$PATH
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

# 1. 复制依赖定义
COPY package.json package-lock.json ./

# 2. 仅安装生产依赖 (减小体积)
RUN npm install --only=production --legacy-peer-deps && npm cache clean --force

# 3. 从 Builder 阶段复制构建好的前端资源
COPY --from=builder --chown=nodejs:nodejs /app/dist ./dist

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
