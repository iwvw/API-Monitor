# Docker 模块重构计划与状态

## 目标
解决 Docker 管理体验糟糕、性能低下、更新逻辑不安全的问题。

## 已完成工作 (Completed)
1.  **引入 Docker Go SDK**
    *   添加了 `github.com/docker/docker/client` 等依赖。
    *   创建 `agent-go/docker_service.go` 用于管理 Docker 客户端单例。
2.  **优化数据采集 (Collector)**
    *   重构 `collector.go` 中的 `collectDockerInfo`。
    *   移除 `exec.Command("docker", "ps", ...)`，改用 SDK `ContainerList`。
    *   大幅降低了周期性采集的 CPU 开销和延迟。
3.  **重构容器操作 (Actions)**
    *   重构 `main.go` 中的 `handleDockerAction`。
    *   Start/Stop/Restart/Pause/Unpause/Pull 现全部通过 SDK 直接调用 Docker API。
4.  **重构容器更新逻辑 (Critical)**
    *   重构 `handleDockerUpdate` (同步) 和 `handleDockerContainerUpdate` (异步)。
    *   **旧逻辑**: 手动解析 inspect 文本 -> 拼接 docker run (丢失大量配置)。
    *   **新逻辑**: Inspect (SDK) -> Pull -> Rename Old -> Create New (Clone Config & HostConfig) -> Start -> Remove Old。
    *   实现了真正的无损更新。
    *   移除了不安全的 `buildDockerRunArgs` 辅助函数。
5.  **重构镜像检查与管理 (Optimization)**
    *   重构 `handleDockerCheckUpdate`: 
        *   使用并发 goroutine + SDK `ContainerList` 替代串行 `docker ps` + `docker inspect`。
        *   解决了检查更新时 Agent 响应极慢的问题。
    *   重构 `handleDockerImages` & `handleDockerImageAction`:
        *   使用 SDK `ImageList`, `ImagePull`, `ImageRemove`, `ImagesPrune`。
        *   移除了易碎的 `awk` 文本解析。
    *   重构 `handleDockerRenameContainer`: 使用 SDK `ContainerRename`。
6.  **重构网络与卷管理 (Consistency)**
    *   重构 `handleDockerNetworks` & `handleDockerNetworkAction`: 使用 SDK List/Create/Remove/Connect/Disconnect。
    *   重构 `handleDockerVolumes` & `handleDockerVolumeAction`: 使用 SDK List/Create/Remove/Prune。

## 保持现状 (Status Quo)
以下功能保留 CLI 调用 (`exec.Command`)，原因如下：
*   **Docker Stats**: `handleDockerStats`。CLI 的 `--no-stream` 模式简单有效，转 SDK 需要手动计算 CPU 百分比（需维护状态），复杂度收益比不高。
*   **Docker Logs**: `handleDockerLogs`。CLI 处理 ANSI 颜色码和 stderr/stdout 合并非常方便。
*   **Create Container**: `handleDockerCreateContainer`。保留对 `ExtraArgs` 的支持，允许用户传递任意 CLI 参数，SDK 难以完全覆盖所有边缘参数。
*   **Docker Compose**: Compose 逻辑复杂，建议继续调用 `docker compose` 命令。

## 验证
已通过 `go build .` 编译测试，无语法错误。
确保 Agent 运行环境已安装 Docker 且有权限访问 `/var/run/docker.sock` (或 Windows Named Pipe)。
