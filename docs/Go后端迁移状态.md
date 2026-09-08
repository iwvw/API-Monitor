# Go 后端迁移状态

> **状态**：历史记录（迁移已完成）。路由治理真源见 `backend-go/internal/manifest/manifest.go`，完整路由清单用 `node tools/backend-route-inventory.mjs` 再生；启动与开发指引见 [GO后端启动指南](./GO后端启动指南.md)。

最后更新：2026-09-08

## 当前状态

当前活跃的后端架构是 `backend-go/` 中的 Go 后端，由 `backend-go/internal/manifest/manifest.go` 治理。

当前路由清单共登记 369 条 manifest 路由，全部由 Go 后端接管。Node sidecar 时代的 Express 模块属于历史上下文，不应再作为新工作的默认实现模型。

## 这意味着什么

- 新增或修改后端路由一律在 Go 后端中进行。
- 路由清单继续作为路由归属、鉴权模式、匹配模式和响应模式的唯一事实来源。
- 使用 `node tools/backend-route-inventory.mjs` 检查当前活跃的路由面。
- 使用 `npm run governance:check` 发现前端路由漂移和已退役路由的引用。
- 使用 `npm run backend-go:test` 运行 Go 包测试。
- 仅在运行中的 Go 后端上使用 `npm run backend-go:smoke`，当后端地址不是 `http://127.0.0.1:3000` 时设置 `API_MONITOR_BASE_URL`。

## 历史备注

较早的文档可能提到 Express 路由器、`modules/*-api` 或 Node sidecar 兜底。除非当前运维指南明确要求使用遗留模式，否则这些引用描述的都是迁移历史。

不要再新增 Express 模块。未经明确的产品决策，不要重新引入 Node sidecar 对活跃路由的接管。
