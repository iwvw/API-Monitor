# Session 认证说明

本项目使用基于服务器内存的 Session 进行管理员认证：

- 会话保存在服务器内存（进程）中，服务器重启后会话会被清空，用户需重新登录。
- 会话有效期：2 天（常量 `SESSION_TTL_MS`）。
- 会话通过 HttpOnly cookie `sid` 管理，前端不再把管理员密码保存到 `localStorage`。
- 为兼容旧脚本，服务器仍支持通过 `x-admin-password` 头进行验证，但前端默认使用 session。

主要相关接口：

- `POST /api/login`  接收 `{ password }`，校验通过后创建 session 并设置 HttpOnly cookie（浏览器会话认证）。
- `POST /api/logout` 注销当前 session（销毁服务器内存中的 session 并清空 cookie）。
- `GET /api/session`  返回 `{ authenticated: true|false }`，前端用于检测是否已登录。

快速测试（示例）：

- 使用 curl 登录并保存 cookie（Windows PowerShell / Linux/macOS 示例如下）：

```powershell
# Windows PowerShell: 使用 Invoke-WebRequest 保存 cookie
Invoke-WebRequest -Uri http://localhost:3000/api/login -Method POST -Body (@{ password = 'your_admin_password' } | ConvertTo-Json) -ContentType 'application/json' -SessionVariable s

# Linux/macOS: 使用 curl 保存 cookie 到 cookiejar.txt
curl -c cookiejar.txt -H "Content-Type: application/json" -d '{"password":"your_admin_password"}' http://localhost:3000/api/login
```

- 使用保存的 cookie 调用受保护接口：

```powershell
# PowerShell（重用 Session 变量）
Invoke-WebRequest -Uri http://localhost:3000/api/server-accounts -WebSession $s

# curl
curl -b cookiejar.txt http://localhost:3000/api/server-accounts
```

- 注销（清理 session）：

```powershell
# PowerShell
Invoke-WebRequest -Uri http://localhost:3000/api/logout -Method POST -WebSession $s

# curl
curl -b cookiejar.txt -X POST http://localhost:3000/api/logout
```

重要说明：

- Cookie 为 HttpOnly，脚本或浏览器无法通过 JavaScript 读取。前端通过发送带 `credentials: 'include'` 的请求来携带 cookie。若需在不同浏览器间共享登录状态，用户需在每个浏览器中登录（浏览器之间无法共享 HttpOnly cookie）。若需要跨设备单点登录（SSO）或跨浏览器记住登录，请告知目标 UX，我们可以设计长期令牌或一次性登录方案。
- 若在生产环境下通过 HTTPS 部署，请将 `NODE_ENV=production`，服务器会在设置 cookie 时使用 `Secure` 标志以提高安全性。

本地运行/调试提示：

- 若启动失败，请先确保已安装依赖：

```powershell
npm install
```
- 使用开发环境（带 nodemon 自动重载）：

```powershell
npm run dev
```
- 或直接启动：

```powershell
npm start
```
