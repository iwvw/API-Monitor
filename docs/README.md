# API Monitor 文档

本目录只保留当前仍有维护价值的文档；历史迁移记录、一次性测试报告和包含本机路径的诊断材料统一移入 [`archive/`](./archive/)。

## 目录结构

| 目录 | 内容 |
| --- | --- |
| `guides/` | 上手与流程：开发指南、后端启动、贡献指南、待办闭环、版本管理 |
| `architecture/` | 架构与接口契约：架构详解、设计文档、API 接口文档、各模块路由清单 |
| `standards/` | 强制性规范：Kumo UI 规则、前端布局约定、开发最佳实践、模块开发指南 |
| `adr/` | 架构决策记录 |
| `prd/` | 需求文档（多数已实现，供回溯原始需求） |
| `reference/` | 上游/生成型资料快照，不参与汉化 |
| `plugins/` | 插件登记清单 `registry.json` |
| `gcp-api-contracts/` | GCP API 契约快照 |
| `archive/` | 历史与一次性文档，不视为现行标准 |

## 新维护者入口

1. 先读仓库根目录的 [`CONTEXT.md`](../CONTEXT.md)（架构、硬性规则、高风险文件、安全命令）。
2. 再读 [项目架构与技术详解](./architecture/项目架构与技术详解.md) 建立整体认识。
3. 准备改代码时看 [开发指南](./guides/开发指南.md) 与 [Kumo UI 规则](./standards/Kumo%20UI%20规则.md)。

## 上手与流程（`guides/`）

- [开发指南](./guides/开发指南.md)
- [Go 后端启动指南](./guides/GO后端启动指南.md)
- [贡献指南](./guides/贡献指南.md)
- [目录结构说明](./guides/目录结构说明.md)
- [待办任务闭环流程](./guides/待办任务闭环流程.md)
- [版本管理](./guides/版本管理.md)

## 架构与接口（`architecture/`）

- [项目架构与技术详解](./architecture/项目架构与技术详解.md)
- [设计文档](./architecture/设计文档.md)
- [API 接口文档](./architecture/API接口文档.md)
- [1Panel 快捷控制接口文档](./architecture/onepanel接口文档.md)
- [Oracle OCI 模块技术设计文档](./architecture/OracleOCI模块技术设计文档.md)
- [Oracle OCI 模块 API 路由清单](./architecture/OracleOCI模块API路由清单.md)

## 规范与标准（`standards/`）

- [Kumo UI 规则](./standards/Kumo%20UI%20规则.md)
- [前端布局约定](./standards/前端布局约定.md)
- [AI 面板布局与交互规范](./standards/AI面板布局与交互规范.md)
- [卡片库尺寸适配规范](./standards/卡片库尺寸适配规范.md)
- [新模块接入指南](./guides/新模块接入指南.md)
- [云厂商模块开发指南](./standards/云厂商模块开发指南.md)
- [模型网关插件开发指南](./standards/模型网关插件开发指南.md)
- [插件安装与卸载设计](./standards/插件安装与卸载设计.md)
- [重构验证与例外清单](./standards/重构验证与例外清单.md)
- [安全加固与扫描计划](./standards/安全加固与扫描计划.md)

进行中的设计稿（尚未全部落地）：

- [模型网关插件 WorkBuddy 方案](./standards/模型网关插件-workbuddy方案.md)

## 架构决策记录（`adr/`）

- [0001-托管代理运行时](./adr/0001-托管代理运行时.md)
- [0002-GCP 模块架构决策](./adr/0002-GCP模块架构决策.md)
- [0003-华为云模块架构决策](./adr/0003-华为云模块架构决策.md)

## 参考资料

- [PRD 目录](./prd/)，含[转发中心 PRD](./prd/转发中心PRD.md)、[Oracle OCI 主机管理模块](./prd/OracleOCI主机管理模块.md)、[GCP 云资源管理模块](./prd/GCP云资源管理模块PRD.md)、[HuaweiCloud 模块](./prd/HuaweiCloud模块PRD.md)、[提示词库模块](./prd/提示词库模块.md)、[文档编辑器重构](./prd/文档编辑器重构.md)、[Draw.io 图编辑工具模块](./prd/Drawio图编辑工具模块.md) 等
- [Kumo 参考资料](./reference/)（含组件注册表与 Dialog 说明）
- [插件登记清单](./plugins/registry.json)
- [GCP API 契约](./gcp-api-contracts/)

## 归档（`archive/`）

以下文档为历史巡检、审查、交接或迁移记录，仅作回溯参考，不视为现行标准，也不转为 GitHub Issue：

- [接口巡检报告-本地](./archive/接口巡检报告-本地.md) / [接口巡检报告-生产](./archive/接口巡检报告-生产.md)（一次性全量接口巡检）
- [生产环境流程审查](./archive/生产环境流程审查.md)（一次性审查）
- [修复审查清单](./archive/修复审查清单.md) / [Bug审计与修复交接文档](./archive/Bug审计与修复交接文档.md)（一次性交接）
- [kumo-design-违规审计清单](./archive/kumo-design-违规审计清单.md) / [frontend组件一致性分析报告](./archive/frontend组件一致性分析报告.md)（一次性审计）
- [Go后端迁移状态](./archive/Go后端迁移状态.md)（迁移历史，迁移已完成）
- [handoff-zcode-ask-ai-sidebar](./archive/handoff-zcode-ask-ai-sidebar.md)（交接）
- [多Agent协作登记](./archive/多Agent协作登记.md)（已废弃，协作改由 GitHub Issues 跟踪，见 `CONTEXT.md`）
- [Cloudflare看板设计分析](./archive/Cloudflare看板设计分析.md)（一次性分析，建议已随卡片库/看板落地）

`prd/` 下已实现上线的历史需求文档（按需回溯原始需求）：

- [管理AI核心引擎与数据层](./prd/管理AI核心引擎与数据层.md) / [管理AIWeb侧栏前端](./prd/管理AIWeb侧栏前端.md) / [管理AI频道接入与Telegram](./prd/管理AI频道接入与Telegram.md) / [管理AI安全审批与审计](./prd/管理AI安全审批与审计.md) / [管理AI共通实现约定](./prd/管理AI共通实现约定.md)
- [定时任务与工作流调度](./prd/定时任务与工作流调度.md) / [提示词库模块](./prd/提示词库模块.md) / [文档编辑器重构](./prd/文档编辑器重构.md) / [Drawio图编辑工具模块](./prd/Drawio图编辑工具模块.md)
- [DevOpsGitHub仓库观察模块](./prd/DevOpsGitHub仓库观察模块.md) / [开发者运维看板功能扩展](./prd/开发者运维看板功能扩展.md) / [语义化表格布局与移动端适配](./prd/语义化表格布局与移动端适配.md)

活跃开发工作改由 GitHub Issues + `backlog`/`in-progress`/`done` 标签跟踪，见 [待办任务闭环流程](./guides/待办任务闭环流程.md) 与 `CONTEXT.md` 协作约定。

## 文档命名与内容规范

### 文件名

- 文档使用简体中文命名，文件名取自 H1 标题并移除空格，例如 `Oracle OCI 模块 API 路由清单` 对应 `OracleOCI模块API路由清单.md`。
- 专有名词缩写保留原文大小写，如 `OCI`、`API`、`PRD`、`Kumo`、`Go`。
- `reference/` 下的 Kumo 快照由上游自动生成并随 Kumo 升级刷新，保留上游英文名，不参与汉化。
- 文档链接使用相对路径；文件名含空格时使用 `%20` 编码，例如 `[Kumo UI 规则](./standards/Kumo%20UI%20规则.md)`。

### 内容

- 正文使用简体中文；代码、命令、API 路径、组件名等技术标识符保留英文原样。
- 首行 H1 标题与文件名保持一致。
- 维护型文档在标题下方标注 `最后更新：YYYY-MM-DD`。
- PRD 统一使用英文小节结构：`Problem Statement` / `Solution` / `User Stories` / `Functional Requirements` / `API Contract` / `Acceptance Criteria` / `Out of Scope`。
- 已废弃、历史或一次性内容必须显式标注，避免被误认为现行标准。
- 遵循下方文档安全约定。

### 新增文档放哪里

- 描述“怎么做某事”的流程类文档放 `guides/`。
- 描述系统“是什么、由什么组成”的结构类文档放 `architecture/`。
- 带强制约束、违反即视为回归的规则放 `standards/`。
- 需求与设计稿放 `prd/`；已落地的需求文档在文首标注状态。
- 一次性报告、审计、交接记录一律放 `archive/`，不要留在根目录。

### 链接校验

移动或重命名文档后，在仓库根目录运行：

```bash
node tools/docs-link-check.mjs
```

该脚本扫描 `docs/` 下所有 Markdown 内部链接，报告断链并以非零退出码结束。

## 文档安全约定

- 不写入真实密码、Token、Cookie、私钥、会话 ID 或云厂商凭证。
- 示例密钥统一使用 `<PLACEHOLDER>` 形式。
- 示例 IP 优先使用文档保留地址，例如 `203.0.113.10`。
- 本机绝对路径、临时目录、个人用户名和内部域名不要写入文档。
