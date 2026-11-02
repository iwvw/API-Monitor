# mobile-android — API Monitor 安卓客户端

Kotlin + Jetpack Compose 原生客户端，UI 遵循 Material Design 3。与 `src/`（Web 前端）、`backend-go/`、`agent-rust/` 同仓库，共用同一套 Go 后端接口。

## 当前能力

- **底栏四入口**：首页（仪表盘）/ 主机实例 / 模型网关 / AI Agent
- 登录面板：支持**账号密码登录**（获得实时推送）与 **API Token**（只读），凭据经 Android Keystore AES-GCM 加密
- 首页仪表盘：主机/端点/Agent 计数、网关请求趋势图（Vico）、面板负载、快捷入口、账号与退出登录
- 主机实例：CPU / 内存 / 磁盘 / 网络，**Socket.IO 实时推送**，断线自动降级 5 秒轮询
- 主机详情：CPU 型号与温度、内存、多磁盘、GPU、网络累计流量、Docker 状态
- 模型网关：端点列表（排序与面板一致）/ 端点详情（接入信息、模型逐个启停、健康与延迟）/ 密钥（编辑、轮换、删除）/ 调用日志（**无限滚动**）
- AI Agent：模块运行指标 + 实例列表，支持**启动 / 重启 / 停止**（停止有二次确认）
- 设置页：面板地址、账号、深色主题、版本、退出登录
- 冷启动自动恢复登录态（sid cookie 加密持久化）

## 环境要求

| 依赖 | 版本 | 本机位置 |
|---|---|---|
| JDK | 17+ | `C:\Program Files\Eclipse Adoptium\jdk-17.0.20.101-hotspot` |
| Android SDK | platform 37.2 / build-tools 37.0.0 | `E:\Android\Sdk` |
| Gradle | 9.8.2（wrapper 自带） | `gradle/wrapper/` |
| AGP | 9.4.1 | `gradle/libs.versions.toml` |
| Compose BOM | 2026.09.00（material3 1.4.0） | 同上 |

环境变量（用户级已配好）：

```
ANDROID_HOME=E:\Android\Sdk
ANDROID_SDK_ROOT=E:\Android\Sdk
PATH 追加：E:\Android\Sdk\cmdline-tools\latest\bin;E:\Android\Sdk\platform-tools;E:\Android\Sdk\emulator
```

`local.properties` 不入库，首次构建前从 `local.properties.example` 复制并改路径；Android Studio 会自动生成。

## 常用命令

```powershell
cd mobile-android

.\gradlew.bat :app:assembleDebug        # 构建 debug APK
.\gradlew.bat :app:testDebugUnitTest    # 单元测试

emulator -avd apimonitor_api36          # 启动模拟器
adb install -r app\build\outputs\apk\debug\app-debug.apk
adb shell am start -n com.iwvw.apimonitor.debug/com.iwvw.apimonitor.MainActivity
```

根目录也提供 `npm run android:assemble` 与 `npm run android:test`。

产物：`app/build/outputs/apk/debug/app-debug.apk`

## 包名与签名

- release 包名 `com.iwvw.apimonitor`，debug 为 `com.iwvw.apimonitor.debug`（可与正式包并存）
- release 签名尚未配置，正式发版前需生成 keystore

## 架构

```
app/src/main/java/com/iwvw/apimonitor/
├── ApiMonitorApp.kt            Application：全局唯一的 ApiClient + MetricsSocket
├── MainActivity.kt             唯一 Activity，Compose 入口
├── data/
│   ├── model/ServerModels.kt   服务器列表 DTO + 容错反序列化
│   ├── model/LiveMetrics.kt    实时推送 DTO + 合并进快照的逻辑
│   ├── model/GatewayModels.kt  模型网关 DTO（端点 / 密钥 / 日志）
│   ├── model/AiAgentModels.kt  AI Agent DTO（实例 / 指标 / 动作）
│   ├── model/SystemModels.kt   面板负载与 API 统计 DTO
│   ├── network/ApiClient.kt    OkHttp + 登录 + 会话 + 各模块接口
│   ├── network/SessionCookieJar.kt   sid cookie 持有与恢复
│   ├── network/MetricsSocket.kt      EIO4/Socket.IO 客户端（手写，无第三方库）
│   └── settings/               凭据与 session（Keystore AES-GCM）
└── ui/
    ├── APIMonitorRoot.kt       根页面（登录 / 主壳 / 设置 / 详情）
    ├── MainScaffold.kt         底栏四 tab 容器 + 共享 ServerListViewModel
    ├── common/                 Haptics、MotionTransitions、WindowInsets
    ├── connect/                登录页（账号密码 / API Token 双模式，支持 2FA）
    ├── home/                   首页仪表盘 + Vico 趋势图
    ├── server/                 主机列表、详情、指标组件、ViewModel
    ├── gateway/                模型网关（端点列表 + 端点详情 / 密钥 / 日志三子页）
    ├── aiagent/                AI Agent（实例列表 + 生命周期操作）
    ├── settings/               设置页
    └── theme/                  Color / Shape / Theme
```

## 实时刷新机制

面板的实时指标**没有 SSE 接口**，走 Socket.IO：

| 项 | 值 |
|---|---|
| 端点 | `/socket.io/?transport=websocket&EIO=4` |
| 命名空间 | `/metrics` |
| 事件 | `metrics:update`（单台）、`server:status`（状态变更） |
| 载荷 | `{ serverId, metrics: {...}, timestamp }` |
| 推送频率 | 每台 1.5 秒采样，服务端按需广播 |

实现要点：

- `MetricsSocket` 手写 EIO4 协议，不引入 socket.io-client（Android 端没有官方维护的等价物）。
- **`40/metrics,` 的解析必须先剥命名空间再判类型**。格式是 `<engine.io type=4><socket.io type><namespace>,<payload>`，直接拿 `startsWith("/metrics")` 判断会永远失效并陷入重发死循环。
- App 级单例 socket + 幂等 `connect`，避免多页面各自建连被服务端踢掉。
- 会话 cookie（`sid`）存内存 CookieJar，登录后加密落盘，冷启动 `restoreSession` 恢复。
- 推送断开时自动降级为 5 秒轮询兜底，`realtimeConnected` 驱动标题栏的「实时 / 轮询」指示。
- 列表与详情共用同一个 `ServerListViewModel`，推送只解析一次、两处同步刷新。

> ⚠️ 后端的 `/socket.io/` 握手**当前不做任何鉴权**，匿名连接 `/metrics` 也能收到全站广播。客户端已按需要登录的思路实现，但这层应由服务端补上（校验 `sid` 后再 `Register`）。

## 为什么需要账号密码

纯 API Token 能读数据，但拿不到面板 session，因此**接不了实时推送**。要实时刷新必须用管理员账号密码登录换取 `sid`。

密码只在 Keystore 加密后落盘，不写死、不进日志。

## Material Design 3 约定

UI 一律按 MD3 官方规范实现，禁止自造视觉规则。

- **颜色**：只用 `MaterialTheme.colorScheme` 的语义角色（`primary` / `secondaryContainer` / `surfaceContainerHigh` / `outlineVariant` 等），不写硬编码色值。调色板在 `ui/theme/Color.kt`，按 MD3 tonal palette 组织为 primary / secondary / tertiary / error 四组 + 5 级 surface container + inverse。
- **动态取色**：Android 12+ 默认跟随系统壁纸取色（`dynamicLightColorScheme` / `dynamicDarkColorScheme`），低版本回落到自带调色板。
- **形态**：统一走 `MaterialTheme.shapes`（`ui/theme/Shape.kt`），不手写圆角数值。
- **字体**：统一走 `MaterialTheme.typography`，不手写字号。
- **组件**：一律用 MD3 原生组件，不自绘——`Card`（**所有卡片统一用 Filled Card**，不用 `ElevatedCard` / `OutlinedCard`；需要点击用 `Card(onClick = ...)` 重载）、`NavigationBar` + `NavigationBarItem`（底栏）、`SegmentedButton`（网关子页切换）、`ListItem`（设置/快捷入口）、`Badge`（状态点与徽标，见 `ui/common/StatusIndicators.kt`）、`LinearProgressIndicator` / `CircularProgressIndicator`（加载与进度）、`OutlinedTextField`（含 `isError` + `supportingText`）、`Button` / `FilledTonalButton` / `OutlinedButton` / `TextButton` / `IconButton`、`AlertDialog`（危险操作确认）、`SnackbarHost`（操作反馈）、`TopAppBar` + `pinnedScrollBehavior`、`PullToRefreshBox`、`Switch`。
- **可点击容器**：用组件自带的 `onClick` 重载（`Card(onClick=)`、`Surface(onClick=)`）。`ListItem` 没有 `onClick` 时用 `SurfaceClickable` 包裹，**不要**用 `Modifier.clickable`（会绕过 MD3 ripple / 状态层 / 无障碍语义）。
- **导航分层**：
  - 根 `NavHost`（`APIMonitorRoot.kt`）只管登录态：`connect` / `main`。
  - 主 `NavHost`（`MainScaffold.kt`）管四个 tab + 所有下钻页（设置、主机详情、端点详情、插件详情）。
  - 底栏只在四个一级 tab 上显示；下钻页全屏、隐藏底栏，靠返回箭头 + 系统返回手势离开。
- **预测性返回**：由 `NavHost` 内置支持（navigation-compose 2.8+），**不要手写 `PredictiveBackHandler`**。配置方式：
  - `popExitTransition = { predictiveBackExit() }`（当前页缩小到 0.9 + 淡出）
  - `popEnterTransition = { predictiveBackEnter() }`（下层页保持静止，`EnterTransition.None`）
  - `AndroidManifest.xml` 的 `<application>` 必须有 `android:enableOnBackInvokedCallback="true"`，否则系统不下发逐帧手势回调。
- **转场动画**：`ui/common/MotionTransitions.kt`，按层级区分动效：
  - Tab 之间（同层）：`tabEnter` / `tabExit`，X 轴 1/12 屏宽位移 + 淡入淡出
  - 下钻（进下一层）：`pushEnter` / `pushExit`，X 轴 1/3 屏宽推入 + 下层轻微左移
  - 返回：`predictiveBackExit` / `predictiveBackEnter`，Z 轴缩放 0.9 + 淡出，跟随系统手势进度
  - 列表项增删与重排：`motionItem()`（`animateItem`）
  - 展开/收起：`ExpandableContent`（`expandVertically` / `shrinkVertically` + 淡入淡出）
  - 时长与缓动按 MD3 MotionTokens：spatial 400ms / effects 300ms，进入用 emphasized decelerate、退出用 emphasized accelerate
- **震动反馈**：`ui/common/Haptics.kt`，按语义分级——`segment()` 切 Tab、`tap()` 普通点击、`confirm()` 操作成功、`reject()` 失败、`danger()` 危险操作前。
- **触控目标**：交互元素不小于 48dp；`IconButton` 已满足，自定义可点击区域需显式设置。
- **状态承载**：加载 / 空 / 错误三态必须显式呈现，错误文案用 `colorScheme.error`。
- **WindowInsets**：底栏页用 `tabContentWindowInsets()`（只留顶部），全屏页用 `fullScreenWindowInsets()`（上下都留）。**不要**在底栏页用默认 inset，会把导航栏高度算两次，表现为底栏上方多一条空白。

## 接口约定

- 鉴权两种：`Authorization: Bearer <token>`（API Key）或 `sid` cookie（账号密码登录）。
- Socket.IO 复用同一份 cookie，`/metrics` 命名空间不额外校验。
- 接口路径只从 `backend-go/internal/manifest/manifest.go` 取，不在客户端硬造路径。
- 当前用到的接口：
  - `POST /api/auth/login` — 账号密码登录，下发 `sid` cookie；触发 2FA 时会提示补验证码
  - `POST /api/auth/logout` — 退出登录
  - `GET /health` — 连通性探测（公开）
  - `GET /api/settings` — 鉴权有效性校验
  - `GET /api/server/accounts` — 服务器列表（含 `info` 静态指标）

后端响应里 `tags` 是数组、数字字段可能以字符串返回，`ServerModels.kt` 里的 `Lenient*Serializer` 已做兼容，`ServerModelsTest` 覆盖这些情况。

百分比统一**向下取整**（`76.79% → 76%`），与面板 Web 端一致；`LiveMetricsTest` 覆盖。

## 模拟器

```powershell
.\tools\configure-avd.ps1     # 设中文 + 120Hz（写回 config.ini，重启不丢）
emulator -avd apimonitor_api36 -gpu host -vsync-rate 120
```

注意仅改 `config.ini` 不够，必须同时传 `-vsync-rate`，否则被 `androidboot.qemu.vsync=60` 覆盖。

## AGP 9 注意事项

AGP 9.0 起 Kotlin 支持内置，**不能再应用 `org.jetbrains.kotlin.android` 插件**，否则构建直接失败。Compose Compiler 插件仍需显式声明。

`compileSdk = 37` 是硬性要求：当前 androidx / compose 依赖的 AAR metadata 要求 API 37 以上，降到 36 会在 `checkDebugAarMetadata` 失败。

若需注解处理（Room / Hilt），用 KSP，不要用 kapt。
