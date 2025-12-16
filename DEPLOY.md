# 🚀 部署指南

本指南详细介绍如何将 API Monitor 部署到各种平台。

## 📋 目录

- [前置准备](#前置准备)
- [Zeabur 部署（推荐）](#zeabur-部署推荐)
- [Docker 部署](#docker-部署)
- [其他平台部署](#其他平台部署)
- [配置说明](#配置说明)
- [常见问题](#常见问题)
- [高级配置](#高级配置)
- [故障排查](#故障排查)

## 前置准备

### 必需条件

- ✅ Node.js 18+ （本地开发）
- ✅ Git（代码管理）
- ✅ 至少一个需要管理的服务账号（Zeabur / Cloudflare / OpenAI 等）

### 可选条件

- GitHub 账号（用于 Zeabur 等平台部署）
- Docker（用于容器化部署）
- 云平台账号（Zeabur / Vercel / Railway 等）

### Fork 项目（可选）

如果需要自定义或贡献代码：

1. 访问项目仓库：https://github.com/iwvw/api-monitor
2. 点击右上角 **Fork** 按钮
3. 将项目 Fork 到你的 GitHub 账号下

---

## Zeabur 部署（推荐）

### 步骤 1：创建 Zeabur 项目

1. 登录 [Zeabur 控制台](https://dash.zeabur.com)
2. 点击 **Create Project** 创建新项目
3. 选择一个区域（推荐选择离你近的）
   - 🇺🇸 Silicon Valley, United States（美国硅谷）
   - 🇮🇩 Jakarta, Indonesia（印度尼西亚雅加达）
   - 🇯🇵 Tokyo, Japan（日本东京）
   - 🇭🇰 Hong Kong（香港）

### 步骤 2：添加服务

1. 在项目页面点击 **Add Service**
2. 选择 **GitHub**
3. 如果是第一次使用，需要授权 Zeabur 访问你的 GitHub
4. 在仓库列表中找到并选择 `api-monitor`
5. 点击 **Deploy**

### 步骤 3：等待部署

- Zeabur 会自动检测项目类型（Node.js）
- 自动安装依赖（`npm install`）
- 自动启动服务（`npm start`）
- 整个过程大约需要 1-3 分钟

### 步骤 4：生成访问域名

1. 部署完成后，点击服务卡片
2. 找到 **Domains** 选项
3. 点击 **Generate Domain** 生成 Zeabur 子域名
   - 格式：`your-service.zeabur.app`
4. 或者点击 **Add Domain** 绑定自定义域名

### 步骤 5：访问应用

1. 点击生成的域名
2. 首次访问会提示设置管理员密码（至少 6 位）
3. 设置密码后即可开始使用

### 优势

- ✅ **零配置部署** - 自动识别 Node.js 项目
- ✅ **自动 HTTPS** - 免费 SSL 证书
- ✅ **自动重启** - 代码更新后自动部署
- ✅ **免费额度** - 每月 $5 免费额度
- ✅ **全球 CDN** - 多地域节点

---

## Docker 部署

### 使用 Docker Compose（推荐）

1. **创建 `docker-compose.yml`**

```yaml
version: '3.8'

services:
  api-monitor:
    image: ghcr.io/iwvw/api-monitor:latest
    container_name: api-monitor
    ports:
      - "3000:3000"
    environment:
      - NODE_ENV=production
      - PORT=3000
      - ADMIN_PASSWORD=your_secure_password_here
    volumes:
      - ./data:/app/config
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/"]
      interval: 30s
      timeout: 10s
      retries: 3
```

2. **启动服务**

```bash
docker-compose up -d
```

3. **查看日志**

```bash
docker-compose logs -f
```

4. **停止服务**

```bash
docker-compose down
```

### 使用 Docker 命令

```bash
# 拉取镜像
docker pull ghcr.io/iwvw/api-monitor:latest

# 运行容器
docker run -d \
  --name api-monitor \
  -p 3000:3000 \
  -e NODE_ENV=production \
  -e ADMIN_PASSWORD="your_secure_password" \
  -v $(pwd)/data:/app/config \
  --restart unless-stopped \
  ghcr.io/iwvw/api-monitor:latest

# 查看日志
docker logs -f api-monitor

# 停止容器
docker stop api-monitor

# 删除容器
docker rm api-monitor
```

### 构建自己的镜像

```bash
# 克隆项目
git clone https://github.com/iwvw/api-monitor.git
cd api-monitor

# 构建镜像
docker build -t api-monitor:latest .

# 运行
docker run -d -p 3000:3000 api-monitor:latest
```

---

## 其他平台部署

### Vercel 部署

1. 导入项目到 Vercel
2. 设置环境变量：
   - `NODE_ENV=production`
   - `ADMIN_PASSWORD=your_password`
3. 部署

> ⚠️ 注意：Vercel 是无服务器环境，会话数据会在函数冷启动时丢失

### Railway 部署

1. 连接 GitHub 仓库
2. 选择项目
3. 设置环境变量
4. 部署

### Render 部署

1. 创建新的 Web Service
2. 连接 GitHub 仓库
3. 设置构建命令：`npm install`
4. 设置启动命令：`npm start`
5. 添加环境变量
6. 部署

### 传统服务器部署

```bash
# 1. 安装 Node.js 18+
curl -fsSL https://deb.nodesource.com/setup_18.x | sudo -E bash -
sudo apt-get install -y nodejs

# 2. 克隆项目
git clone https://github.com/iwvw/api-monitor.git
cd api-monitor

# 3. 安装依赖
npm install

# 4. 设置环境变量
export NODE_ENV=production
export ADMIN_PASSWORD=your_password

# 5. 使用 PM2 管理进程
npm install -g pm2
pm2 start server.js --name api-monitor
pm2 save
pm2 startup

# 6. 配置 Nginx 反向代理（可选）
sudo nano /etc/nginx/sites-available/api-monitor
```

Nginx 配置示例：

```nginx
server {
    listen 80;
    server_name your-domain.com;

    location / {
        proxy_pass http://localhost:3000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;
    }
}
```

---

## 配置说明

### 环境变量

| 变量名 | 说明 | 默认值 | 必需 |
|--------|------|--------|------|
| `PORT` | 服务端口 | `3000` | 否 |
| `NODE_ENV` | 运行环境 | `development` | 否 |
| `ADMIN_PASSWORD` | 管理员密码 | - | 否* |
| `CONFIG_DIR` | 配置文件目录 | `./config` | 否 |

> *首次访问时可通过 Web 界面设置密码

**在 Zeabur 中设置环境变量：**

1. 在服务页面点击 **Variables**
2. 添加需要的环境变量
3. 保存后服务会自动重启

### 配置文件

运行时会在 `config/` 目录下生成以下文件：

| 文件名 | 说明 | 格式 |
|--------|------|------|
| `password.json` | 管理员密码 | JSON |
| `sessions.json` | 会话数据 | JSON |
| `zb-accounts.json` | Zeabur 账号 | JSON |
| `cf-accounts.json` | Cloudflare 账号 | JSON |
| `openai-endpoints.json` | OpenAI 端点 | JSON |

> ⚠️ 这些文件包含敏感信息，请勿提交到 Git

### zbpack.json 配置

项目已包含 `zbpack.json` 配置文件，Zeabur 会自动识别：

```json
{
  "build_command": "npm install",
  "start_command": "node server.js",
  "install_command": "npm install"
}
```

## 常见问题

### Q1: 部署失败怎么办？

**A**: 检查以下几点：
1. 确认 `package.json` 中的依赖是否正确
2. 查看 Zeabur 的构建日志，找到错误信息
3. 确认 Node.js 版本是否兼容（推荐 18+）

### Q2: 无法访问应用？

**A**: 可能的原因：
1. 服务还在启动中，等待 1-2 分钟
2. 域名还未生效，刷新页面重试
3. 检查服务状态是否为 "Running"

### Q3: 数据会丢失吗？

**A**:
- 账号数据存储在 `config/` 目录的配置文件中
- **Zeabur/Docker**：默认文件系统是临时的，重启后会丢失
- **解决方案**：
  - 使用持久化存储（Volume）
  - 定期备份配置文件
  - 使用数据库存储（高级）

### Q4: 如何更新代码？

**A**:

**Zeabur 部署：**
1. 在 GitHub 上更新代码并推送
2. Zeabur 会自动检测并重新部署
3. 或在 Zeabur 控制台手动触发重新部署

**Docker 部署：**
```bash
# 拉取最新镜像
docker pull ghcr.io/iwvw/api-monitor:latest

# 重启容器
docker-compose down
docker-compose up -d
```

**传统服务器：**
```bash
cd api-monitor
git pull
npm install
pm2 restart api-monitor
```

### Q5: 如何绑定自定义域名？

**A**:

**Zeabur：**
1. 在服务页面点击 **Domains**
2. 点击 **Add Domain**
3. 输入域名（如 `monitor.example.com`）
4. 在 DNS 服务商添加 CNAME 记录：
   ```
   monitor.example.com  →  your-service.zeabur.app
   ```
5. 等待 DNS 生效（5-10 分钟）

**传统服务器：**
配置 Nginx 反向代理（见上文）

### Q6: 如何添加持久化存储？

**A**:

**Zeabur：**
1. 在项目中点击 **Add Service**
2. 选择 **Prebuilt** → **Volumes**
3. 创建卷并挂载到 `/app/config`

**Docker：**
```yaml
volumes:
  - ./data:/app/config  # 挂载本地目录
```

### Q7: 费用如何计算？

**A**:
- **Zeabur**：每月 $5 免费额度，本项目通常在免费额度内
- **Docker**：服务器成本（自行承担）
- **Vercel/Railway**：有免费套餐
- 可在应用中实时查看 Zeabur 账号费用

### Q8: 会话过期怎么办？

**A**:
- 会话有效期为 2 天
- 过期后需要重新登录
- 服务器重启后会话会清空
- 详见 [SESSION_AUTH.md](./SESSION_AUTH.md)

### Q9: 如何备份配置？

**A**:
```bash
# 备份配置文件
cp -r config/ config_backup_$(date +%Y%m%d)/

# 或使用 tar 打包
tar -czf config_backup_$(date +%Y%m%d).tar.gz config/
```

### Q10: 支持多用户吗？

**A**:
- 当前版本只支持单个管理员账号
- 多用户功能在规划中
- 可通过多实例部署实现隔离

## 高级配置

### 使用 PostgreSQL 存储数据

如果你想使用数据库存储账号数据：

1. 在 Zeabur 项目中添加 PostgreSQL 服务
2. 获取数据库连接信息
3. 修改 `server.js`，使用数据库替代文件存储
4. 安装 `pg` 依赖：`npm install pg`

### 使用 Redis 缓存

如果你想提高性能：

1. 在 Zeabur 项目中添加 Redis 服务
2. 安装 `redis` 依赖：`npm install redis`
3. 修改代码，添加缓存逻辑

### 配置 HTTPS

Zeabur 自动为所有域名提供免费的 HTTPS 证书，无需额外配置。

### 配置 CDN

如果你的用户分布在全球：

1. 使用 Cloudflare 等 CDN 服务
2. 将域名指向 CDN
3. CDN 回源到 Zeabur 域名

### 监控和日志

1. **查看日志**：
   - 在 Zeabur 控制台点击服务
   - 选择 **Logs** 标签
   - 查看实时日志

2. **监控指标**：
   - CPU 使用率
   - 内存使用率
   - 网络流量

3. **告警设置**：
   - 在 Zeabur 中配置告警规则
   - 当服务异常时接收通知

## 部署检查清单

部署前请确认：

- [ ] 已 Fork 项目到自己的 GitHub
- [ ] 已创建 Zeabur 账号
- [ ] 已准备好需要管理的服务账号
- [ ] 已选择合适的部署区域
- [ ] 已了解免费额度限制

部署后请验证：

- [ ] 服务状态为 "Running"
- [ ] 可以正常访问域名
- [ ] 可以设置管理员密码
- [ ] 可以添加账号
- [ ] 数据显示正常
- [ ] 所有功能正常工作

## 安全建议

1. **定期更换密码**：
   - 定期更换管理员密码
   - 定期更换 API Token

2. **限制访问**：
   - 使用强密码
   - 考虑添加 IP 白名单

3. **备份数据**：
   - 定期导出账号数据
   - 保存在安全的地方

4. **更新依赖**：
   - 定期更新 npm 依赖
   - 修复安全漏洞

## 故障排查

### 服务无法启动

1. 查看构建日志
2. 检查 `package.json` 配置
3. 确认 Node.js 版本
4. 检查端口配置

### 数据丢失

1. 检查文件系统是否持久化
2. 恢复备份数据
3. 考虑使用数据库

### 性能问题

1. 检查 CPU/内存使用率
2. 优化代码逻辑
3. 增加缓存
4. 升级服务规格

## 获取帮助

如果遇到问题：

1. 查看 [Zeabur 官方文档](https://zeabur.com/docs)
2. 在 GitHub 提交 Issue
3. 加入 Zeabur Discord 社区
4. 查看项目 README.md

## 相关链接

- [Zeabur 官网](https://zeabur.com)
- [Zeabur 文档](https://zeabur.com/docs)
- [项目 GitHub](https://github.com/iwvw/api-monitor)

---

祝你部署顺利！🎉
