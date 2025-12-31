---
description: API Monitor 项目优化实施计划
---

# 🚀 API Monitor 项目优化计划

> **项目版本**: v0.1.2  
> **计划创建**: 2025-12-31  
> **预计周期**: 4-6 周（根据可用时间调整）

---

## 📋 阶段概览

| 阶段 | 名称 | 时间估算 | 核心目标 |
|------|------|----------|----------|
| Phase 1 | 基础设施完善 | 1 周 | 工程规范化 |
| Phase 2 | 代码质量提升 | 1-2 周 | 可维护性 |
| Phase 3 | 安全性加固 | 1 周 | 安全防护 |
| Phase 4 | 性能优化 | 1 周 | 运行效率 |
| Phase 5 | 前端重构 | 2 周 | 架构优化 |

---

## 🔵 Phase 1: 基础设施完善 (Week 1)

### 1.1 代码规范配置
- [x] **ESLint 配置** ✅ (2025-12-31)
  ```bash
  npm install -D eslint @eslint/js
  ```
  - 创建 `eslint.config.js` (ESLint v9 平面配置)
  - 配置 Node.js 和浏览器环境规则
  
- [x] **Prettier 配置** ✅ (2025-12-31)
  ```bash
  npm install -D prettier eslint-config-prettier
  ```
  - 创建 `.prettierrc`
  - 创建 `.prettierignore`
  - 统一代码格式化风格

- [x] **EditorConfig** ✅ (2025-12-31)
  - 创建 `.editorconfig`
  - 统一编辑器基础设置

### 1.2 Git 规范化
- [ ] **Commitlint 配置**
  ```bash
  npm install -D @commitlint/cli @commitlint/config-conventional
  ```
  - 强制 conventional commits 格式
  
- [ ] **Husky 钩子**
  ```bash
  npm install -D husky lint-staged
  ```
  - pre-commit: lint-staged
  - commit-msg: commitlint

### 1.3 CI/CD 配置
- [x] **GitHub Actions** ✅ (2025-12-31)
  - `.github/workflows/ci.yml` - 构建检查 + Docker 测试
  - [ ] `.github/workflows/release.yml` - 自动发版 (待完成)

### 1.4 文档完善
- [x] **CHANGELOG.md** - 变更日志 ✅ (2025-12-31)
- [x] **CONTRIBUTING.md** - 贡献指南 ✅ (2025-12-31)
- [ ] **docs/ARCHITECTURE.md** - 架构说明 (待完成)

**Phase 1 产出**:
- 标准化的开发环境
- 自动代码检查流程
- CI/CD 流水线

---

## 🟢 Phase 2: 代码质量提升 (Week 2-3)

### 2.1 测试框架搭建
- [ ] **安装 Vitest**
  ```bash
  npm install -D vitest @vitest/coverage-v8
  ```
  
- [ ] **创建测试目录结构**
  ```
  test/
  ├── unit/
  │   ├── db/
  │   │   └── database.test.js
  │   ├── middleware/
  │   │   └── auth.test.js
  │   └── utils/
  │       ├── logger.test.js
  │       └── encryption.test.js
  ├── integration/
  │   ├── api/
  │   │   ├── auth.test.js
  │   │   └── music.test.js
  └── fixtures/
      └── mock-data.js
  ```

### 2.2 核心模块测试
按优先级编写测试：

| 优先级 | 模块 | 测试重点 |
|--------|------|----------|
| P0 | `src/middleware/auth.js` | 认证逻辑 |
| P0 | `src/db/database.js` | CRUD 操作 |
| P1 | `src/services/session.js` | 会话管理 |
| P1 | `modules/music-api/router.js` | API 响应 |
| P2 | `src/utils/encryption.js` | 加解密 |

### 2.3 统一错误处理
- [ ] **创建错误处理中间件**
  - `src/middleware/errorHandler.js`
  - 统一错误响应格式
  
- [ ] **创建自定义错误类**
  - `src/errors/AppError.js`
  - `src/errors/AuthError.js`
  - `src/errors/ValidationError.js`

### 2.4 代码重构
- [ ] **移动顶部 require 语句**
  - `modules/music-api/router.js` 第 311, 379, 522 行
  
- [ ] **提取重复代码**
  - Cookie 处理逻辑 → `src/utils/cookie-helper.js`
  - API 响应构建 → `src/utils/response-builder.js`

**Phase 2 产出**:
- 测试覆盖率 > 60%
- 统一的错误处理机制
- 更清晰的代码结构

---

## 🟡 Phase 3: 安全性加固 (Week 4)

### 3.1 敏感数据加密
- [ ] **Cookie 加密存储**
  - 使用 `src/utils/encryption.js` 加密 music_settings.cookie
  - 读取时自动解密
  
- [ ] **Token 加密**
  - 所有 API Token 加密存储
  - 添加解密层

### 3.2 速率限制
- [ ] **安装依赖**
  ```bash
  npm install express-rate-limit
  ```
  
- [ ] **配置限制规则**
  ```javascript
  // src/middleware/rateLimit.js
  const limiter = rateLimit({
    windowMs: 15 * 60 * 1000,
    max: 100,
    standardHeaders: true
  });
  ```
  
- [ ] **应用到敏感端点**
  - `/api/auth/*` - 登录相关
  - `/api/music/audio/proxy` - 音频代理
  - `/api/openai/*` - OpenAI 代理

### 3.3 输入验证
- [ ] **安装 Joi/Zod**
  ```bash
  npm install zod
  ```
  
- [ ] **添加请求验证**
  - 为主要 API 添加 schema 验证
  - 防止 SQL 注入（虽然用 prepared statements 但仍需验证）

### 3.4 安全头
- [ ] **安装 Helmet**
  ```bash
  npm install helmet
  ```
  
- [ ] **配置安全头**
  - CSP (Content Security Policy)
  - HSTS
  - X-Frame-Options

**Phase 3 产出**:
- 敏感数据加密存储
- API 速率限制
- 增强的输入验证

---

## 🟠 Phase 4: 性能优化 (Week 5)

### 4.1 数据库优化
- [ ] **Prepared Statement 缓存**
  ```javascript
  // src/db/statements.js
  const stmtCache = new Map();
  function getStatement(sql) {
    if (!stmtCache.has(sql)) {
      stmtCache.set(sql, db.prepare(sql));
    }
    return stmtCache.get(sql);
  }
  ```

- [ ] **添加索引分析**
  - 检查慢查询
  - 添加必要索引

### 4.2 缓存策略
- [ ] **内存缓存**
  ```bash
  npm install lru-cache
  ```
  
- [ ] **应用缓存**
  - 用户信息缓存
  - 配置信息缓存
  - API 响应缓存（适当场景）

### 4.3 流处理优化
- [ ] **音频代理优化**
  - 使用 `pipeline` 替代手动流转换
  - 添加超时控制

### 4.4 构建优化
- [ ] **分析打包体积**
  ```bash
  npm install -D rollup-plugin-visualizer
  ```
  
- [ ] **代码分割**
  - 按路由懒加载
  - 提取公共依赖

**Phase 4 产出**:
- 数据库查询性能提升
- 降低内存占用
- 更小的打包体积

---

## 🔴 Phase 5: 前端重构 (Week 6-7)

### 5.1 状态管理重构
- [ ] **迁移到 Pinia**
  ```bash
  npm install pinia
  ```
  
- [ ] **拆分 store**
  ```
  src/js/stores/
  ├── auth.js      # 认证状态
  ├── server.js    # 主机管理
  ├── music.js     # 音乐模块
  ├── settings.js  # 设置
  └── index.js     # 汇总
  ```

### 5.2 组件化重构
- [ ] **拆分 main.js**
  - 提取 Vue 组件到 `src/components/`
  - 每个模块一个入口组件
  
- [ ] **组件目录结构**
  ```
  src/components/
  ├── common/
  │   ├── Modal.vue
  │   ├── Toast.vue
  │   └── Button.vue
  ├── server/
  │   ├── ServerList.vue
  │   └── ServerDetail.vue
  ├── music/
  │   ├── Player.vue
  │   └── Playlist.vue
  └── ...
  ```

### 5.3 路由改进
- [ ] **引入 Vue Router**
  ```bash
  npm install vue-router
  ```
  
- [ ] **配置路由**
  - 替换当前的 tab 切换逻辑
  - 支持浏览器历史记录

### 5.4 类型安全（可选）
- [ ] **JSDoc 注释**
  - 为关键函数添加类型注释
  - 配合 VSCode 提供智能提示

**Phase 5 产出**:
- 模块化的前端代码
- 更好的状态管理
- 改进的路由体验

---

## 📊 进度跟踪

### 里程碑

| 里程碑 | 目标日期 | 状态 |
|--------|----------|------|
| M1: 基础设施完成 | Week 1 | 🔄 进行中 (80%) |
| M2: 测试覆盖 60% | Week 3 | ⬜ 未开始 |
| M3: 安全加固完成 | Week 4 | ⬜ 未开始 |
| M4: v0.2.0 发布 | Week 5 | ⬜ 未开始 |
| M5: 前端重构完成 | Week 7 | ⬜ 未开始 |

### 状态说明
- ⬜ 未开始
- 🔄 进行中
- ✅ 已完成
- ⏸️ 暂停
- ❌ 取消

---

## 🎯 快速开始命令

```bash
# 开始 Phase 1 - 安装开发依赖
npm install -D eslint prettier eslint-config-prettier husky lint-staged @commitlint/cli @commitlint/config-conventional

# 开始 Phase 2 - 安装测试框架
npm install -D vitest @vitest/coverage-v8

# 开始 Phase 3 - 安装安全依赖
npm install express-rate-limit helmet zod

# 开始 Phase 4 - 安装性能依赖
npm install lru-cache

# 开始 Phase 5 - 安装前端依赖
npm install pinia vue-router
```

---

## 📝 备注

1. **优先级调整**: 可根据实际需求调整各阶段顺序
2. **并行执行**: Phase 1-4 中的部分任务可以并行
3. **增量发布**: 每个阶段完成后可发布小版本
4. **测试先行**: 重构前先补充测试，确保不引入回归

---

*最后更新: 2025-12-31 10:55*

---

## ✅ 已完成项目清单

| 日期 | 任务 | 文件 |
|------|------|------|
| 2025-12-31 | ESLint 配置 | `eslint.config.js` |
| 2025-12-31 | Prettier 配置 | `.prettierrc`, `.prettierignore` |
| 2025-12-31 | EditorConfig | `.editorconfig` |
| 2025-12-31 | CI 工作流 | `.github/workflows/ci.yml` |
| 2025-12-31 | CHANGELOG | `CHANGELOG.md` |
| 2025-12-31 | 贡献指南 | `CONTRIBUTING.md` |
| 2025-12-31 | npm scripts | `package.json` (lint/format) |
