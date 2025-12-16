# ===================================
# API Monitor Docker Image
# ===================================
# 多阶段构建，优化镜像大小和安全性

# 阶段 1: 依赖安装
FROM node:18-alpine AS deps

# 设置工作目录
WORKDIR /app

# 复制依赖文件
COPY package.json package-lock.json* ./

# 安装生产依赖
RUN npm ci --only=production && \
    npm cache clean --force

# 阶段 2: 运行时镜像
FROM node:18-alpine AS runner

# 添加元数据标签
LABEL org.opencontainers.image.title="API Monitor"
LABEL org.opencontainers.image.description="API聚合监控面板"
LABEL org.opencontainers.image.source="https://github.com/iwvw/api-monitor"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.vendor="iwvw"
LABEL maintainer="iwvw"

# 安装运行时依赖
RUN apk add --no-cache \
    curl \
    tini \
    && rm -rf /var/cache/apk/*

# 创建非 root 用户
RUN addgroup -g 1001 -S nodejs && \
    adduser -S nodejs -u 1001

# 设置工作目录
WORKDIR /app

# 创建必要的目录并设置权限
RUN mkdir -p /app/config /app/data && \
    chown -R nodejs:nodejs /app

# 从依赖阶段复制 node_modules
COPY --from=deps --chown=nodejs:nodejs /app/node_modules ./node_modules

# 复制应用源码
COPY --chown=nodejs:nodejs . .

# 设置环境变量
ENV NODE_ENV=production \
    PORT=3000 \
    CONFIG_DIR=/app/config

# 暴露端口
EXPOSE 3000

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3000/ || exit 1

# 切换到非 root 用户
USER nodejs

# 使用 tini 作为 init 进程
ENTRYPOINT ["/sbin/tini", "--"]

# 启动应用
CMD ["node", "server.js"]
