# 贡献指南

感谢你对 API Monitor 项目的关注！我们欢迎所有形式的贡献。

## 🚀 快速开始

### 环境要求

- Node.js >= 18.x
- npm >= 9.x
- Rust >= 1.75 (仅 Agent 开发需要)

### 本地开发

```bash
# 1. 克隆项目
git clone https://github.com/iwvw/api-monitor.git
cd api-monitor

# 2. 安装依赖
npm install

# 3. 启动开发服务器
npm run dev
```

访问 `http://localhost:5173` 查看前端，API 服务运行在 `http://localhost:3000`。

---

## 📋 代码规范

### 代码风格

本项目使用 ESLint + Prettier 进行代码检查和格式化：

```bash
# 检查代码
npm run lint

# 自动修复
npm run lint:fix

# 格式化代码
npm run format
```

### 提交规范

请使用 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/) 格式：

```
<type>(<scope>): <subject>

<body>
```

**类型 (type)**:

- `feat`: 新功能
- `fix`: 修复 bug
- `docs`: 文档更新
- `style`: 代码格式（不影响功能）
- `refactor`: 重构（既不是新功能也不是修复）
- `perf`: 性能优化
- `test`: 测试相关
- `chore`: 构建/工具变更

**示例**:

```
feat(music): 添加歌词同步功能

- 支持 LRC 格式解析
- 添加歌词滚动动画
```

---

## 🔀 Pull Request 流程

1. **Fork** 项目到你的 GitHub 账号
2. 创建特性分支: `git checkout -b feat/my-feature`
3. 提交更改: `git commit -m 'feat: 添加新功能'`
4. 推送分支: `git push origin feat/my-feature`
5. 提交 **Pull Request**

### PR 检查清单

- [ ] 代码通过 `npm run lint`
- [ ] 代码通过 `npm run build`
- [ ] 更新相关文档
- [ ] 添加必要的测试

---

## 📁 项目结构

```
api-monitor/
├── server.js          # 服务端入口
├── src/
│   ├── js/            # 前端 JavaScript
│   ├── css/           # 样式文件
│   ├── db/            # 数据库相关
│   ├── middleware/    # Express 中间件
│   ├── routes/        # API 路由
│   ├── services/      # 业务服务
│   └── utils/         # 工具函数
├── modules/           # 功能模块
│   ├── music-api/     # 音乐 API
│   ├── cloudflare-api/# DNS 管理
│   └── ...
├── agent-rust/        # Rust Agent 源码
└── test/              # 测试文件
```

---

## 🐛 报告 Bug

请通过 [GitHub Issues](https://github.com/iwvw/api-monitor/issues) 报告问题，包含以下信息：

1. **环境**: 操作系统、Node.js 版本、浏览器
2. **复现步骤**: 详细的操作步骤
3. **预期行为**: 你期望发生什么
4. **实际行为**: 实际发生了什么
5. **截图/日志**: 如有相关截图或错误日志

---

## 💡 功能建议

欢迎在 [GitHub Discussions](https://github.com/iwvw/api-monitor/discussions) 提出新功能建议。

---

## 📜 许可证

通过贡献代码，你同意你的贡献将按照项目的 [MIT 许可证](LICENSE) 进行授权。

---

感谢你的贡献！🎉
