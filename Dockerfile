# ===================================
# API Monitor Docker Image
# ===================================

FROM node:18-alpine

# 添加元数据标签
LABEL org.opencontainers.image.title="API Monitor"
LABEL org.opencontainers.image.description="统一的 API 管理面板"
LABEL org.opencontainers.image.source="https://github.com/iwvw/api-monitor"
LABEL org.opencontainers.image.licenses="MIT"

# 安装 curl 用于健康检查
RUN apk add --no-cache curl

# 创建非 root 用户
RUN addgroup -g 1001 -S nodejs && \
    adduser -S nodejs -u 1001

# 创建应用目录
WORKDIR /app

# 创建配置目录并设置权限
RUN mkdir -p /app/config && chown -R nodejs:nodejs /app

# 复制依赖文件
COPY --chown=nodejs:nodejs package.json package-lock.json* ./

# 安装生产依赖
RUN npm ci --only=production && npm cache clean --force

# 复制应用源码
COPY --chown=nodejs:nodejs . .

# 设置环境变量
ENV NODE_ENV=production
ENV PORT=3000
ENV CONFIG_DIR=/app/config

# 暴露端口
EXPOSE 3000

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3000/ || exit 1

# 切换到非 root 用户
USER nodejs

# 启动应用
CMD ["node", "server.js"]
