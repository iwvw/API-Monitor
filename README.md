# 🚀 Zeabur 多账号监控面板

一个美观、强大的 Zeabur 多账号监控工具，实时显示免费额度使用情况、项目费用和服务状态。

![](https://img.shields.io/badge/Node.js-18+-green.svg)
![](https://img.shields.io/badge/License-MIT-blue.svg)
![](https://img.shields.io/badge/Zeabur-Ready-blueviolet.svg)

## ✨ 功能特性

- 🎨 **现代化 UI** - 粉色主题 + 玻璃拟态效果 + 动漫背景
- 💰 **实时余额监控** - 显示每月免费额度剩余（$X.XX / $5.00）
- ***项目费用追踪** - 每个项目的实时费用统计
- ✏️ **项目快速改名** - 点击铅笔图标即可重命名项目
- 🌐 **域名显示** - 显示项目的所有域名，点击直接访问
- 🐳 **服务状态监控** - 显示所有服务的运行状态和资源配置
-  ***多账号支持** - 同时管理多个 Zeabur 账号
-  ***自动刷新** - 每 30 秒自动更新数据
- 🎚️ **透明度调节** - 可调节卡片透明度（0-100%）
- 📱 **响应式设计** - 完美适配各种屏幕尺寸
- ***密码保护** - 管理员密码验证，保护账号安全
- 💾 **服务器存储** - 账号数据存储在服务器，多设备自动同步
- ⏸️ **服务控制** - 暂停、启动、重启服务
- 📋 **查看日志** - 实时查看服务运行日志

## 📦 快速开始

### 前置要求

- Node.js 18+
- Zeabur 账号和 API Token

### 获取 Zeabur API Token

1. 登录 [Zeabur 控制台](https://zeabur.com)
2. 点击右上角头像 → **Settings**
3. 找到 **Developer** 或 **API Keys** 选项
4. 点击 **Create Token**
5. 复制生成的 Token（格式：`sk-xxxxxxxxxxxxxxxx`）

### 本地部署

```bash
# 1. 克隆项目
git clone https://github.com/jiujiu532/zeabur-monitor.git
cd zeabur-monitor

# 2. 安装依赖
npm install

# 3. 启动服务
npm start

# 4. 访问应用
# 打开浏览器访问：http://localhost:3000
```

### Zeabur 部署（推荐）

详细部署步骤请查看 [DEPLOY.md](./DEPLOY.md)

## 📖 使用说明

### 首次使用

1. 访问应用后，首次使用需要设置管理员密码（至少 6 位）
2. 设置完成后，使用密码登录
3. 点击 **"⚙️ 管理账号"** 添加 Zeabur 账号

### 添加账号

#### 单个添加
1. 点击 **"⚙️ 管理账号"**
2. 输入账号名称和 API Token
3. 点击 **"➕ 添加到列表"**

#### 批量添加
支持以下格式（每行一个账号）：
- `账号名称:API_Token`
- `账号名称：API_Token`
- `账号名称(API_Token)`
- `账号名称（API_Token）`

### 项目改名

1. 找到项目卡片
2. 点击项目名称右侧的 **✏️** 铅笔图标
3. 输入新名称，按 `Enter` 保存或 `Esc` 取消

### 服务控制

- **暂停服务**：点击 **⏸️ 暂停** 按钮
- **启动服务**：点击 **▶️ 启动** 按钮
- **重启服务**：点击 **🔄 重启** 按钮
- **查看日志**：点击 **📋 日志** 按钮

## 🔧 技术栈

- **后端**：Node.js + Express
- **前端**：Vue.js 3 (CDN)
- **API**：Zeabur GraphQL API
- **样式**：原生 CSS（玻璃拟态效果）

## 📁 项目结构

```
zeabur-monitor/
├── public/
│   ├── index.html      # 前端页面
│   ├── bg.png          # 背景图片
│   └── favicon.png     # 网站图标
├── server.js           # 后端服务
├── package.json        # 项目配置
├── .env.example        # 环境变量示例
├── .gitignore          # Git 忽略规则
├── zbpack.json         # Zeabur 配置
├── README.md           # 项目说明
└── DEPLOY.md           # 部署指南
```

## 🔒 安全说明

### 密码保护
- 首次使用需要设置管理员密码（至少 6 位）
- 密码存储在服务器的 `password.json` 文件中
- 登录后 4 天内自动保持登录状态

### API Token 安全
- Token 存储在服务器的 `accounts.json` 文件中
- 输入时自动打码显示（`●●●●●●`）
- 不会暴露在前端代码或浏览器中

### 重要提示
⚠️ **请勿将以下文件提交到 Git：**
- `.env` - 环境变量
- `accounts.json` - 账号数据
- `password.json` - 管理员密码

这些文件已在 `.gitignore` 中配置。

## 🎨 自定义

### 更换背景图片
替换 `public/bg.png` 为你喜欢的图片

### 调整透明度
使用页面上的透明度滑块调节

### 修改主题色
在 `public/index.html` 中搜索 `#f696c6` 并替换为你喜欢的颜色

## 🔄 多设备同步

账号信息存储在服务器上，所有设备自动同步！

- 在电脑上添加账号 → 手机、平板立即可见
- 在手机上删除账号 → 所有设备同步删除
- 无需任何配置，开箱即用

## 🛠️ 开发

### 环境变量（可选）

创建 `.env` 文件：
```env
PORT=3000
ACCOUNTS=账号1:token1,账号2:token2
```

### API 端点

- `GET /` - 前端页面
- `POST /api/check-password` - 检查是否已设置密码
- `POST /api/set-password` - 设置管理员密码
- `POST /api/verify-password` - 验证密码
- `POST /api/temp-accounts` - 获取账号信息
- `POST /api/temp-projects` - 获取项目信息
- `POST /api/validate-account` - 验证账号
- `GET /api/server-accounts` - 获取服务器存储的账号
- `POST /api/server-accounts` - 保存账号到服务器
- `DELETE /api/server-accounts/:index` - 删除账号
- `POST /api/project/rename` - 重命名项目
- `POST /api/service/pause` - 暂停服务
- `POST /api/service/restart` - 重启服务
- `POST /api/service/logs` - 获取服务日志

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 许可证

MIT License - 自由使用和修改

## ⭐ Star History

如果这个项目对你有帮助，请给个 Star ⭐

## 🙏 致谢

- [Zeabur](https://zeabur.com) - 提供优秀的云服务平台
- [Vue.js](https://vuejs.org) - 渐进式 JavaScript 框架
- [Express](https://expressjs.com) - 快速、开放、极简的 Web 框架

---

Made with ❤️ by [jiujiu532](https://github.com/jiujiu532)
