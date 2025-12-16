# 🔐 Session 认证说明

本项目使用基于服务器内存的 Session 机制进行管理员认证，提供安全可靠的身份验证。

## 📖 认证机制概述

### 核心特性

- ✅ **内存存储** - 会话保存在服务器内存中，性能高效
- ✅ **HttpOnly Cookie** - 使用 `sid` cookie，防止 XSS 攻击
- ✅ **自动过期** - 会话有效期 2 天，超时自动失效
- ✅ **安全传输** - 生产环境自动启用 Secure 标志
- ✅ **兼容性** - 支持传统 `x-admin-password` 头认证

### 工作原理

```
┌─────────┐      登录请求       ┌─────────┐
│ 浏览器  │ ──────────────────> │ 服务器  │
│         │                     │         │
│         │ <────────────────── │         │
│         │   设置 HttpOnly     │ 内存中  │
│         │   Cookie (sid)      │ 存储    │
│         │                     │ Session │
│         │      后续请求        │         │
│         │ ──────────────────> │         │
│         │   携带 Cookie       │ 验证    │
│         │                     │ Session │
└─────────┘                     └─────────┘
```

### 会话生命周期

1. **创建** - 用户登录成功后创建会话
2. **存储** - 会话数据保存在服务器内存
3. **验证** - 每次请求验证 cookie 中的 session ID
4. **更新** - 访问时更新最后访问时间
5. **过期** - 2 天后自动失效或手动登出
6. **清理** - 服务器重启后所有会话清空

## 🔌 API 接口

### 认证相关接口

| 方法 | 端点 | 说明 | 请求体 | 响应 |
|------|------|------|--------|------|
| `POST` | `/api/login` | 用户登录 | `{ password: string }` | 设置 HttpOnly cookie |
| `POST` | `/api/logout` | 用户登出 | - | 清空 cookie |
| `GET` | `/api/session` | 检查会话状态 | - | `{ authenticated: boolean }` |

### 接口详情

#### 1. 登录接口

**请求：**
```http
POST /api/login
Content-Type: application/json

{
  "password": "your_password"
}
```

**成功响应：**
```json
{
  "success": true,
  "message": "登录成功"
}
```

**失败响应：**
```json
{
  "success": false,
  "message": "密码错误"
}
```

#### 2. 登出接口

**请求：**
```http
POST /api/logout
```

**响应：**
```json
{
  "success": true,
  "message": "已登出"
}
```

#### 3. 会话检查接口

**请求：**
```http
GET /api/session
```

**响应：**
```json
{
  "authenticated": true
}
```

## 🧪 测试示例

### 使用 curl（Linux/macOS）

```bash
# 1. 登录并保存 cookie
curl -c cookiejar.txt \
  -H "Content-Type: application/json" \
  -d '{"password":"your_admin_password"}' \
  http://localhost:3000/api/login

# 2. 使用 cookie 访问受保护接口
curl -b cookiejar.txt \
  http://localhost:3000/api/server-accounts

# 3. 检查会话状态
curl -b cookiejar.txt \
  http://localhost:3000/api/session

# 4. 登出
curl -b cookiejar.txt \
  -X POST \
  http://localhost:3000/api/logout
```

### 使用 PowerShell（Windows）

```powershell
# 1. 登录并保存会话
$body = @{ password = 'your_admin_password' } | ConvertTo-Json
Invoke-WebRequest -Uri http://localhost:3000/api/login `
  -Method POST `
  -Body $body `
  -ContentType 'application/json' `
  -SessionVariable session

# 2. 使用会话访问受保护接口
Invoke-WebRequest -Uri http://localhost:3000/api/server-accounts `
  -WebSession $session

# 3. 检查会话状态
Invoke-WebRequest -Uri http://localhost:3000/api/session `
  -WebSession $session

# 4. 登出
Invoke-WebRequest -Uri http://localhost:3000/api/logout `
  -Method POST `
  -WebSession $session
```

### 使用 JavaScript（浏览器）

```javascript
// 1. 登录
async function login(password) {
  const response = await fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
    credentials: 'include'  // 重要：携带 cookie
  });
  return await response.json();
}

// 2. 检查会话
async function checkSession() {
  const response = await fetch('/api/session', {
    credentials: 'include'
  });
  return await response.json();
}

// 3. 登出
async function logout() {
  const response = await fetch('/api/logout', {
    method: 'POST',
    credentials: 'include'
  });
  return await response.json();
}

// 使用示例
await login('your_password');
const session = await checkSession();
console.log('已登录:', session.authenticated);
await logout();
```

## 🔒 安全说明

### Cookie 安全特性

| 特性 | 说明 | 作用 |
|------|------|------|
| **HttpOnly** | JavaScript 无法读取 | 防止 XSS 攻击窃取 cookie |
| **Secure** | 仅 HTTPS 传输（生产环境） | 防止中间人攻击 |
| **SameSite** | 限制跨站请求 | 防止 CSRF 攻击 |
| **有效期** | 2 天自动过期 | 限制会话时长 |

### 重要提示

#### ⚠️ HttpOnly Cookie 限制

- Cookie 为 HttpOnly，JavaScript **无法读取**
- 前端必须使用 `credentials: 'include'` 携带 cookie
- 不同浏览器间**无法共享**登录状态
- 每个浏览器需要独立登录

#### 🔐 生产环境配置

**启用 HTTPS 安全传输：**

```bash
export NODE_ENV=production
```

生产环境下，服务器会自动：
- 启用 `Secure` 标志（仅 HTTPS 传输）
- 增强 cookie 安全性
- 建议使用反向代理（Nginx）配置 SSL

#### 🌐 跨域请求配置

前端请求必须包含 `credentials: 'include'`：

```javascript
fetch('/api/login', {
  method: 'POST',
  credentials: 'include',  // 必需！
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ password })
});
```

### 兼容性说明

#### 传统密码头认证

为兼容旧脚本，服务器仍支持 `x-admin-password` 头：

```bash
curl -H "x-admin-password: your_password" \
  http://localhost:3000/api/server-accounts
```

> ⚠️ 不推荐使用，建议迁移到 Session 认证

## 🛠️ 本地开发

### 快速启动

```bash
# 1. 安装依赖
npm install

# 2. 开发模式（支持热重载）
npm run dev

# 3. 生产模式
npm start
```

### 调试技巧

**查看 Cookie：**
- Chrome DevTools → Application → Cookies
- 查看 `sid` cookie 的值和属性

**清除会话：**
```bash
# 删除 cookie
# 或调用登出接口
curl -X POST http://localhost:3000/api/logout
```

**查看会话数据：**
```bash
# 会话存储在 config/sessions.json
cat config/sessions.json
```

## 🔄 会话管理

### 会话配置

在 `src/services/session.js` 中配置：

```javascript
const SESSION_TTL_MS = 2 * 24 * 60 * 60 * 1000;  // 2 天
```

### 会话清理

**自动清理：**
- 过期会话自动失效
- 服务器重启后清空所有会话

**手动清理：**
```bash
# 删除会话文件
rm config/sessions.json

# 或重启服务器
pm2 restart api-monitor
```

## 🚨 常见问题

### Q1: 为什么重启后需要重新登录？

**A**: 会话存储在服务器内存中，重启后会清空。

**解决方案：**
- 使用持久化存储（Redis）
- 延长会话有效期
- 实现"记住我"功能

### Q2: 跨域请求无法携带 Cookie？

**A**: 需要配置 CORS 和 credentials。

**后端配置：**
```javascript
app.use(cors({
  origin: 'https://your-frontend.com',
  credentials: true
}));
```

**前端配置：**
```javascript
fetch(url, { credentials: 'include' });
```

### Q3: 如何实现多设备登录？

**A**: 当前每个设备/浏览器需要独立登录。

**未来计划：**
- 支持多设备会话管理
- 实现设备列表查看
- 支持远程登出设备

### Q4: 会话安全吗？

**A**: 是的，采用了多重安全措施：

- ✅ HttpOnly 防止 XSS
- ✅ Secure 防止中间人攻击
- ✅ SameSite 防止 CSRF
- ✅ 自动过期限制时长
- ✅ 随机 Session ID

## 📚 相关文档

- [部署指南](./DEPLOY.md) - 了解如何部署项目
- [项目结构](./STRUCTURE.md) - 了解代码架构
- [README](./README.md) - 项目总览

## 🔗 参考资源

- [MDN - HTTP Cookies](https://developer.mozilla.org/zh-CN/docs/Web/HTTP/Cookies)
- [OWASP - Session Management](https://owasp.org/www-community/controls/Session_Management_Cheat_Sheet)
- [Express Session Best Practices](https://expressjs.com/en/advanced/best-practice-security.html)
