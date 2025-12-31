# Changelog

本项目的所有重要更改都将记录在此文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 新增
- ESLint 代码检查配置 (`eslint.config.js`)
- Prettier 代码格式化配置 (`.prettierrc`)
- EditorConfig 编辑器配置 (`.editorconfig`)
- 项目优化实施计划 (`.agent/workflows/optimization-plan.md`)
- npm scripts: `lint`, `lint:fix`, `format`, `format:check`

### 变更
- 无

### 修复
- 无

### 移除
- 无

---

## [0.1.2] - 2025-12-30

### 新增
- Music API 模块 - 网易云音乐代理
- 音频流代理功能 (解决 HTTPS 混合内容问题)
- 歌曲解锁功能 (使用 @unblockneteasemusic/server)

### 变更
- 优化 Cookie 处理逻辑，支持登录态持久化

---

## [0.1.1] - 2025-12-28

### 新增
- 主机管理模块 (Agent 监控)
- SSH 终端功能
- Socket.IO 实时连接
- Go Agent 客户端

### 变更
- 升级 Vite 至 v7
- 优化 WebSocket 处理逻辑

---

## [0.1.0] - 2025-12-25

### 新增
- 初始版本发布
- Zeabur API 监控
- Cloudflare DNS 管理
- OpenAI API 健康检查
- TOTP 验证器
- Gemini CLI 账号管理
- 系统设置面板
- 数据库备份/恢复功能

---

[Unreleased]: https://github.com/iwvw/api-monitor/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/iwvw/api-monitor/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/iwvw/api-monitor/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/iwvw/api-monitor/releases/tag/v0.1.0
