# API Monitor 文档中心

> **版本**: v0.1.6
> **最后更新**: 2026-06-16

---

## 📚 文档导航

### 🚀 快速开始

| 文档 | 说明 |
|------|------|
| [README](../README.md) | 项目简介、快速部署与环境配置 |
| [开发指南](./开发指南.md) | 开发环境搭建、常见开发任务、调试技巧 |

### 🏗️ 架构与设计

| 文档 | 说明 |
|------|------|
| [项目架构与技术详解](./项目架构与技术详解.md) | 完整的系统架构、技术栈、模块设计 |
| [DESIGN.md](./DESIGN.md) | 架构设计文档（精简版） |

### 📖 开发指南

| 文档 | 说明 |
|------|------|
| [开发指南](./开发指南.md) | 开发流程、代码规范、常见问题 |
| [NEW_MODULE_GUIDE.md](./NEW_MODULE_GUIDE.md) | 新模块开发指南 |
| [GO_BACKEND_GUIDE.md](./GO_BACKEND_GUIDE.md) | Go 后端开发规范 |
| [FRONTEND_BEST_PRACTICES.md](./FRONTEND_BEST_PRACTICES.md) | 前端最佳实践 |
| [KUMO_MIGRATION_RULES.md](./KUMO_MIGRATION_RULES.md) | Kumo UI 组件迁移规则 |

### 🔌 API 参考

| 文档 | 说明 |
|------|------|
| [API接口文档](./API接口文档.md) | 完整的 REST API 接口文档 |

### 🤝 贡献指南

| 文档 | 说明 |
|------|------|
| [CONTRIBUTING.md](./CONTRIBUTING.md) | 贡献指南与代码提交规范 |

---

## 📂 文档结构

```
docs/
├── README.md                       # 文档中心索引（本文件）
│
├── 快速开始
│   ├── ../README.md                # 项目介绍与快速部署
│   └── 开发指南.md                  # 开发环境与开发流程
│
├── 架构与设计
│   ├── 项目架构与技术详解.md        # 完整技术文档（推荐）
│   └── DESIGN.md                   # 架构设计简版
│
├── 开发指南
│   ├── 开发指南.md                  # 综合开发指南
│   ├── NEW_MODULE_GUIDE.md         # 新模块开发
│   ├── GO_BACKEND_GUIDE.md         # Go 后端规范
│   ├── FRONTEND_BEST_PRACTICES.md  # 前端最佳实践
│   └── KUMO_MIGRATION_RULES.md     # UI 组件规范
│
├── API 参考
│   └── API接口文档.md               # REST API 文档
│
├── 贡献指南
│   └── CONTRIBUTING.md             # 贡献流程
│
├── 归档文档（历史参考）
│   └── archive/                    # 迁移报告、测试记录等
│
└── 其他
    └── reference/                  # 参考资料
```

---

## 🎯 文档使用建议

### 👨‍💻 如果你是开发者

**新手入门流程**:
1. 阅读 [项目 README](../README.md) 了解项目概况
2. 参考 [开发指南](./开发指南.md) 配置开发环境
3. 学习 [项目架构与技术详解](./项目架构与技术详解.md) 理解系统设计
4. 根据任务类型查阅对应指南:
   - 添加新功能 → [NEW_MODULE_GUIDE.md](./NEW_MODULE_GUIDE.md)
   - 前端开发 → [FRONTEND_BEST_PRACTICES.md](./FRONTEND_BEST_PRACTICES.md)
   - 后端开发 → [GO_BACKEND_GUIDE.md](./GO_BACKEND_GUIDE.md)

**常用文档快速链接**:
- 🔧 [开发环境配置](./开发指南.md#开发环境配置)
- 📝 [代码规范](./开发指南.md#代码规范)
- 🐛 [调试技巧](./开发指南.md#调试技巧)
- ❓ [常见问题](./开发指南.md#常见问题)

### 🔌 如果你是 API 使用者

直接查阅 [API接口文档](./API接口文档.md)，包含:
- 认证方式
- 所有 REST API 接口
- WebSocket 接口
- 请求/响应示例
- 错误码说明

### 🎨 如果你是 UI/UX 设计师

参考以下文档了解前端技术栈和 UI 规范:
- [前端最佳实践](./FRONTEND_BEST_PRACTICES.md)
- [Kumo UI 组件规范](./KUMO_MIGRATION_RULES.md)
- [项目架构 - 前端架构部分](./项目架构与技术详解.md#前端架构)

### 🏗️ 如果你是架构师

重点阅读:
- [项目架构与技术详解](./项目架构与技术详解.md) - 完整系统架构
- [DESIGN.md](./DESIGN.md) - 架构设计要点
- [Go 后端指南](./GO_BACKEND_GUIDE.md) - 后端技术选型

---

## 📝 文档维护

### 核心文档说明

| 文档类型 | 维护频率 | 维护者 |
|---------|---------|--------|
| **架构文档** | 重大架构变更时更新 | 核心开发者 |
| **API 文档** | 每次 API 变更时更新 | 后端开发者 |
| **开发指南** | 开发流程变更时更新 | 所有开发者 |
| **README** | 版本发布时更新 | 项目维护者 |

### 文档贡献

欢迎改进文档！请遵循以下原则:

1. **准确性**: 确保文档内容与代码实现一致
2. **完整性**: 提供足够的上下文和示例
3. **可读性**: 使用清晰的结构和格式
4. **时效性**: 及时更新过时内容

提交文档改进的步骤:
1. Fork 项目
2. 创建文档分支 (`git checkout -b docs/improve-xxx`)
3. 修改文档并提交
4. 提交 Pull Request

---

## 🗂️ 归档文档

以下文档已移至 `archive/` 目录，仅作历史参考:

- **迁移相关**:
  - `backend-go-rust-migration-prd.md` - Go/Rust 迁移方案
  - `backend-migration-complete.md` - 后端迁移完成报告
  - `wave-5b-completion-report-2026-06-15.md` - Wave 5b 完成报告

- **测试报告**:
  - `FINAL_TEST_REPORT.md` - 最终测试报告
  - `WEBSOCKET_TEST_REPORT.md` - WebSocket 测试报告

- **重构文档**:
  - `refactor-verification.md` - 重构验证文档
  - `toolbox-modules-refactor-prd.md` - 工具箱模块重构 PRD

这些文档记录了项目的演进历史，如需查阅请访问 `archive/` 目录。

---

## 🔗 外部资源

### 技术文档

- [Go 官方文档](https://go.dev/doc/)
- [React 官方文档](https://react.dev/)
- [Vite 官方文档](https://vitejs.dev/)
- [Tailwind CSS 文档](https://tailwindcss.com/docs)
- [Kumo UI 组件库](https://github.com/cloudflare/kumo)
- [SQLite 文档](https://www.sqlite.org/docs.html)

### 项目链接

- [GitHub 仓库](https://github.com/iwvw/api-monitor)
- [Docker Hub](https://hub.docker.com/r/iwvw/api-monitor)
- [问题反馈](https://github.com/iwvw/api-monitor/issues)

---

## 📮 联系方式

如有文档相关问题或建议，欢迎通过以下方式联系:

- 提交 [GitHub Issue](https://github.com/iwvw/api-monitor/issues)
- 加入讨论 [GitHub Discussions](https://github.com/iwvw/api-monitor/discussions)

---

**文档持续完善中，感谢您的关注与贡献！**

**Made with ❤️ by [iwvw](https://github.com/iwvw) & [jiujiu532](https://github.com/jiujiu532)**
