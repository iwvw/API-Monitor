# 🚀 部署指南

本指南将详细介绍如何将 API Monitor 部署到 Zeabur 平台。

## 目录

- [前置准备](#前置准备)
- [部署步骤](#部署步骤)
- [配置说明](#配置说明)
- [常见问题](#常见问题)
- [高级配置](#高级配置)

## 前置准备

### 1. 准备工作

- ✅ 一个 GitHub 账号
- ✅ 一个 Zeabur 账号（[注册地址](https://zeabur.com)）
- ✅ 至少一个需要管理的服务账号（Zeabur / Cloudflare 等）

### 2. Fork 项目

1. 访问项目仓库：https://github.com/your-username/api-monitor
2. 点击右上角 **Fork** 按钮
3. 将项目 Fork 到你的 GitHub 账号下

## 部署步骤

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
2. 首次访问会提示设置管理员密码
3. 设置密码后即可开始使用

## 配置说明

### 环境变量（可选）

如果你想预配置，可以在 Zeabur 中设置环境变量：

1. 在服务页面点击 **Variables**
2. 添加以下环境变量：

| 变量名 | 说明 | 示例 |
|--------|------|------|
| `PORT` | 端口号（可选） | `3000` |
| `ADMIN_PASSWORD` | 管理员密码（可选） | `your_password` |

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
- 账号数据存储在配置文件中
- Zeabur 的文件系统是临时的，重启后会丢失
- **建议**：定期备份账号信息，或使用持久化存储

### Q4: 如何更新代码？

**A**: 
1. 在 GitHub 上更新你的代码
2. 推送到仓库
3. Zeabur 会自动检测并重新部署
4. 或者在 Zeabur 控制台手动触发重新部署

### Q5: 如何绑定自定义域名？

**A**:
1. 在服务页面点击 **Domains**
2. 点击 **Add Domain**
3. 输入你的域名（如 `monitor.example.com`）
4. 在你的 DNS 服务商添加 CNAME 记录：
   ```
   monitor.example.com  →  your-service.zeabur.app
   ```
5. 等待 DNS 生效（通常 5-10 分钟）

### Q6: 如何添加持久化存储？

**A**:
1. 在项目中点击 **Add Service**
2. 选择 **Prebuilt** → **Volumes**
3. 创建一个卷（Volume）
4. 在服务设置中挂载卷到 `/app/config`
5. 数据将会持久化保存

### Q7: 费用如何计算？

**A**:
- Zeabur 提供每月 $5 的免费额度
- 本项目资源消耗很小，通常在免费额度内
- 可以在应用中实时查看 Zeabur 账号费用

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
- [项目 GitHub](https://github.com/your-username/api-monitor)

---

祝你部署顺利！🎉
