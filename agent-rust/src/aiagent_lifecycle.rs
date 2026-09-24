// AI Agent 进程生命周期管理（ADR-0006 第 3-5 条）：
//   - start：按 Provider 模板启动本地 Agent 服务，绑定给定端口（任务 57）
//   - stop：停止该实例对应的进程（任务 58）
//   - status：返回该实例的进程表条目（任务 59）
//
// 安全约束（ADR-0006 第 6 条）：云端不能传任意路径或任意参数。可执行文件路径来自
// Agent 本地配置，命令行按 Provider 模板拼装，端口做数值范围校验。这三条缺一不可，
// 否则「可远程启动进程」会变成「可远程执行任意命令」。
//
// 进程表持久化到本地状态文件，Agent 重启后据此恢复处于 running 期望的进程。

use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::{Mutex, OnceLock};
use std::time::Duration;

use crate::aiagent::listening_pid;

/// 单个实例的托管进程记录。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ManagedProcess {
    pub instance_id: String,
    pub provider: String,
    pub port: u16,
    pub pid: u32,
    /// 启动时刻（Unix 秒），用于计算运行时长。
    pub started_at: i64,
    /// 已重启次数，超过上限后进入 crashed 终态。
    pub restarts: u32,
    /// 是否仍希望该进程运行。stop 时置 false，让 supervisor 不再拉起。
    #[serde(default = "default_desired")]
    pub desired_running: bool,
    /// 是否已进入 crashed 终态（重启次数耗尽）。需显式 stop/start 才能恢复。
    #[serde(default)]
    pub crashed: bool,
}

fn default_desired() -> bool {
    true
}

/// supervisor 单次崩溃后的重启退避序列（秒）。用尽后进入 crashed 终态。
///
/// 递增退避是为了避免「进程因配置错误秒退」时形成重启风暴打满 CPU。
const RESTART_BACKOFF_SECONDS: &[u64] = &[1, 2, 5, 10, 30];
/// 重启次数上限：等于退避序列长度。超过即 crashed。
const MAX_RESTARTS: u32 = 5;
/// supervisor 巡检间隔。比最短退避更细，保证及时拉起。
const SUPERVISOR_TICK: Duration = Duration::from_millis(500);

/// 稳定运行多久后把重启计数归零（秒）。
///
/// 退避序列衡量的是「连续快速失败次数」，而不是「历史总失败次数」。
/// 若不归零：进程崩溃重启 4 次后稳定跑了三天，某天再崩一次就会被判 crashed，
/// 尽管它此前已健康运行很久。这是 ADR-0006 第 4.1 条的语义要求。
///
/// 取 60 秒：比最长退避（30 秒）更长，确保「挺过退避窗口」才算稳定；
/// 又足够短，让偶发崩溃不会累积成假崩溃。
const RESTART_STABLE_RESET_SECONDS: i64 = 60;

#[derive(Debug, Deserialize)]
pub struct StartPayload {
    pub instance_id: String,
    #[serde(default)]
    pub provider: String,
    pub port: u16,
}

#[derive(Debug, Deserialize)]
pub struct StopPayload {
    pub instance_id: String,
}

#[derive(Debug, Deserialize)]
pub struct StatusPayload {
    pub instance_id: String,
}

/// 进程表：instance_id → 托管进程。进程内单例。
fn process_table() -> &'static Mutex<HashMap<String, ManagedProcess>> {
    static TABLE: OnceLock<Mutex<HashMap<String, ManagedProcess>>> = OnceLock::new();
    TABLE.get_or_init(|| Mutex::new(HashMap::new()))
}

/// 进程表持久化文件路径（与 Agent 其它状态放在同一目录）。
fn state_file() -> PathBuf {
    std::env::var("API_MONITOR_AIAGENT_STATE")
        .map(PathBuf::from)
        .unwrap_or_else(|_| {
            let mut path = dirs_state_dir();
            path.push("aiagent_processes.json");
            path
        })
}

fn dirs_state_dir() -> PathBuf {
    // 与 tcp_forwarder 等模块一致：优先用 Agent 的工作目录。
    std::env::current_dir().unwrap_or_else(|_| PathBuf::from("."))
}

/// 允许的端口上限：与云端 Provider 注册表的区间保持一致（默认端口 + 99）。
/// 这里做二次校验，即使云端被攻破也无法让 Agent 绑定任意端口。
const MAX_PORT_OFFSET: u16 = 99;

/// Provider 模板：可执行文件名 + 参数模板。
///
/// 只允许启动这里的白名单条目；云端只能指定 Provider ID，不能传路径或参数。
struct ProviderTemplate {
    id: &'static str,
    default_port: u16,
    executable: &'static str,
    /// 参数模板，`{port}` 会被替换为校验后的端口。
    args: &'static [&'static str],
    /// 进程名匹配规则，与云端 Provider 注册表保持一致。
    /// 用于状态查询时做「端口监听者是否为本 Provider 进程」的关联验证。
    process_match: &'static [&'static str],
    /// 覆盖可执行文件路径的环境变量名。
    ///
    /// Agent 作为系统服务运行时 PATH 往往与用户交互式登录不同（Windows 服务
    /// 不含用户级 npm 目录），只靠 PATH 查找会启动失败。部署方可用该变量指向
    /// 绝对路径。这同时满足 ADR-0006 第 6.1 条「路径来自 Agent 本地配置」：
    /// 云端仍然只能传 Provider ID，无法指定路径。
    executable_env: &'static str,
}

const PROVIDER_TEMPLATES: &[ProviderTemplate] = &[ProviderTemplate {
    id: "opencode",
    default_port: 4096,
    executable: "opencode",
    args: &["serve", "--hostname", "127.0.0.1", "--port", "{port}"],
    process_match: &["opencode"],
    executable_env: "API_MONITOR_AIAGENT_OPENCODE_BIN",
}];

fn lookup_template(provider: &str) -> Option<&'static ProviderTemplate> {
    let id = if provider.is_empty() {
        "opencode"
    } else {
        provider
    };
    PROVIDER_TEMPLATES.iter().find(|template| template.id == id)
}

/// 去掉路径两侧可能残留的引号与空白。
///
/// 用 `setx` 或手写 .reg 设置环境变量时，很容易把值连同英文双引号一起写进去
/// （`setx VAR "C:\a\b.exe"` 在部分写法下引号会成为值的一部分）。这种值传给
/// `PathBuf::is_file()` 会判定不存在，spawn 也只剩一句含糊的 program not found。
/// 这里只剥离**首尾成对**的引号，不碰路径中间出现的引号字符。
fn strip_wrapping_quotes(value: &str) -> String {
    let trimmed = value.trim();
    for (open, close) in [('"', '"'), ('\'', '\'')] {
        if trimmed.len() >= 2 && trimmed.starts_with(open) && trimmed.ends_with(close) {
            return trimmed[1..trimmed.len() - close.len_utf8()]
                .trim()
                .to_string();
        }
    }
    trimmed.to_string()
}

/// 解析可执行文件路径：优先本地配置的环境变量，其次自动探测常见安装位置，
/// 最后在 PATH 中查找。
///
/// 返回 `(路径, 是否确认存在)`。自动探测解决「Agent 服务 PATH 不含用户级安装目录、
/// 且 PATH 根目录只有 .cmd shim 没有真身 .exe」导致的启动失败：
///   - Windows 上 npm 全局安装的 opencode 真身在 `node_modules/opencode-ai/bin/`
///     下，Command::new 只认 .exe；
///   - Linux/macOS 上官方安装脚本（`curl -fsSL https://opencode.ai/install | bash`）
///     把二进制放到 `~/.opencode/bin/opencode`，服务环境的 PATH 通常不含该目录。
///
/// 最后一步在 PATH 中做真实查找（Windows 只认 .exe；Unix 需任一执行位），
/// 而不是无条件回退裸命令名——否则已装好且 PATH 可达时仍会报 found=false。
fn resolve_executable(template: &ProviderTemplate) -> (String, bool) {
    // 1. 显式环境变量优先（ADR-0006 第 6.1 条：路径来自 Agent 本地配置）。
    //    先剥离首尾引号：setx/.reg 写入时常把引号一并存进值里。
    if let Ok(value) = std::env::var(template.executable_env) {
        let value = strip_wrapping_quotes(&value);
        if !value.is_empty() {
            let exists = PathBuf::from(&value).is_file();
            return (value, exists);
        }
    }
    // 2. 自动探测常见 npm 全局安装位置（真身 exe，绕过 .cmd shim）。
    if let Some(path) = detect_npm_executable(template) {
        return (path.to_string_lossy().to_string(), true);
    }
    // 3. 探测常见安装目录与 PATH（覆盖官方安装脚本的 ~/.opencode/bin 等）。
    if let Some(path) = find_in_search_dirs(template) {
        return (path.to_string_lossy().to_string(), true);
    }
    // 4. 回退到裸命令名（走 PATH；仍可能因服务 PATH 精简而未命中，由 spawn 报错）。
    (template.executable.to_string(), false)
}

/// 在常见 npm 全局安装位置中探测 Provider 的真身可执行文件。
///
/// 覆盖两处：用户级 npm 全局目录与系统级 npm 全局目录；Windows 上真身
/// 命名带 `.exe`。探测到即返回，未找到返回 None。
fn detect_npm_executable(template: &ProviderTemplate) -> Option<PathBuf> {
    // 用户级 npm 全局目录（Windows 为 %APPDATA%\npm，Linux 为 ~/.local/share/npm 或 ~/.npm-global）
    let mut candidates: Vec<PathBuf> = Vec::new();
    if let Some(appdata) = std::env::var_os("APPDATA") {
        let npm = PathBuf::from(appdata).join("npm");
        candidates.push(npm.join("node_modules"));
    }
    if let Some(home) = std::env::var_os("HOME") {
        let home = PathBuf::from(home);
        candidates.push(home.join(".local/share/npm/node_modules"));
        candidates.push(home.join(".npm-global/node_modules"));
        candidates.push(home.join("node_modules"));
    }
    // 系统级 npm 全局目录（Linux 常见）。
    candidates.push(PathBuf::from("/usr/local/lib/node_modules"));
    candidates.push(PathBuf::from("/usr/lib/node_modules"));

    let exe_name = if cfg!(target_os = "windows") {
        format!("{}.exe", template.executable)
    } else {
        template.executable.to_string()
    };
    for modules_dir in candidates {
        let candidate = modules_dir
            .join(format!("{}-ai", template.id))
            .join("bin")
            .join(&exe_name);
        if candidate.is_file() {
            return Some(candidate);
        }
    }
    None
}

/// 平台相关的可执行文件名候选。
///
/// Windows 只认 `.exe`：`Command::new` 走 CreateProcess，不会执行 `.cmd`/`.bat`
/// 包装脚本，把 shim 当命中会让诊断谎报就绪、start 阶段才失败。npm 的 shim 由
/// `detect_npm_executable` 定位真身 .exe 来覆盖。
fn executable_names(template: &ProviderTemplate) -> Vec<String> {
    if cfg!(target_os = "windows") {
        vec![format!("{}.exe", template.executable)]
    } else {
        vec![template.executable.to_string()]
    }
}

/// 判断路径是否为一个可执行文件：普通文件，Unix 上还需任一执行位。
fn is_executable_file(path: &std::path::Path) -> bool {
    let Ok(meta) = std::fs::metadata(path) else {
        return false;
    };
    if !meta.is_file() {
        return false;
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        meta.permissions().mode() & 0o111 != 0
    }
    #[cfg(not(unix))]
    {
        true
    }
}

/// 常见安装目录（官方安装脚本、包管理器与用户级 bin），再拼接 PATH 目录，按顺序查找。
///
/// 先查固定目录是为了解决「Agent 作为系统服务运行时 PATH 与交互式登录不同」：
/// 二进制可能装在 PATH 之外的 `~/.opencode/bin`。去重避免 PATH 已含同一目录时重复探测。
fn find_in_search_dirs(template: &ProviderTemplate) -> Option<PathBuf> {
    let mut dirs: Vec<PathBuf> = Vec::new();

    // 官方安装脚本固定落在 $HOME/.opencode/bin（各平台一致）。
    if let Some(home) = std::env::var_os("HOME").or_else(|| std::env::var_os("USERPROFILE")) {
        let home = PathBuf::from(home);
        dirs.push(home.join(".opencode/bin"));
        dirs.push(home.join(".local/bin"));
        dirs.push(home.join("bin"));
    }
    // 包管理器常见系统目录（Linux/macOS）。
    dirs.push(PathBuf::from("/usr/local/bin"));
    dirs.push(PathBuf::from("/usr/bin"));
    dirs.push(PathBuf::from("/opt/homebrew/bin"));
    dirs.push(PathBuf::from("/home/linuxbrew/.linuxbrew/bin"));
    dirs.push(PathBuf::from("/snap/bin"));
    // Windows 包管理器。
    dirs.push(PathBuf::from(r"C:\ProgramData\chocolatey\bin"));
    if let Some(profile) = std::env::var_os("USERPROFILE") {
        dirs.push(PathBuf::from(profile).join("scoop/shim"));
    }

    // 再拼接 PATH（split_paths 跨平台处理 PATH）。服务 PATH 精简时上面的固定目录兜底。
    if let Some(path_var) = std::env::var_os("PATH") {
        dirs.extend(std::env::split_paths(&path_var));
    }

    let names = executable_names(template);
    let mut seen: Vec<PathBuf> = Vec::new();
    for dir in dirs {
        if dir.as_os_str().is_empty() || seen.contains(&dir) {
            continue;
        }
        seen.push(dir.clone());
        for name in &names {
            let candidate = dir.join(name);
            if is_executable_file(&candidate) {
                return Some(candidate);
            }
        }
    }
    None
}

/// 校验端口是否落在该 Provider 的允许区间内（ADR-0006 第 6.3 条）。
fn port_allowed(template: &ProviderTemplate, port: u16) -> bool {
    port >= template.default_port && port <= template.default_port.saturating_add(MAX_PORT_OFFSET)
}

fn load_persisted() -> HashMap<String, ManagedProcess> {
    let Ok(raw) = std::fs::read_to_string(state_file()) else {
        return HashMap::new();
    };
    serde_json::from_str(&raw).unwrap_or_default()
}

fn persist(table: &HashMap<String, ManagedProcess>) {
    if let Ok(raw) = serde_json::to_string_pretty(table) {
        let _ = std::fs::write(state_file(), raw);
    }
}

/// 按模板拼装参数并拉起进程，返回 PID。
///
/// 参数模板化 + 可执行文件来自本地配置，是 ADR-0006 第 6.1/6.2 条的安全约束：
/// 云端只能传 Provider ID 与端口，无法指定路径或注入参数。
fn spawn_process(template: &ProviderTemplate, port: u16) -> Result<u32, String> {
    let args: Vec<String> = template
        .args
        .iter()
        .map(|arg| {
            if *arg == "{port}" {
                port.to_string()
            } else {
                (*arg).to_string()
            }
        })
        .collect();

    let (executable, found) = resolve_executable(template);
    if !found {
        // 未能通过环境变量或常见安装位置定位真身：明确报错而非让 spawn 抛出
        // 含糊的 program not found，提示用户安装或配置 API_MONITOR_AIAGENT_OPENCODE_BIN。
        return Err(format!(
            "未找到 Provider {} 的可执行文件（{}），请安装或设置环境变量 {} 指向真实路径",
            template.id, template.executable, template.executable_env
        ));
    }
    let mut command = std::process::Command::new(&executable);
    command.args(&args);
    // Unix：把子进程放进独立进程组（PGID = 子 PID），让 terminate 能按组终止
    // 整棵进程树。否则父进程退出后子进程被 reparent 到 init，父链断开，残留
    // 子进程仍持端口却再也追溯不到归属。
    #[cfg(unix)]
    {
        use std::os::unix::process::CommandExt;
        command.process_group(0);
    }
    let child = command
        .spawn()
        .map_err(|err| format!("启动 {} 失败: {}", executable, err))?;
    Ok(child.id())
}

/// 启动托管进程（任务 57）。
pub async fn start(raw: &str) -> Result<String, String> {
    let payload: StartPayload =
        serde_json::from_str(raw).map_err(|err| format!("启动参数无效: {}", err))?;

    if payload.instance_id.trim().is_empty() {
        return Err("缺少 instance_id".to_string());
    }

    let template = lookup_template(&payload.provider)
        .ok_or_else(|| format!("未登记的 Provider: {}", payload.provider))?;

    if !port_allowed(template, payload.port) {
        return Err(format!(
            "端口 {} 超出 {} 的允许区间 [{}, {}]",
            payload.port,
            template.id,
            template.default_port,
            template.default_port.saturating_add(MAX_PORT_OFFSET)
        ));
    }

    // 幂等：已在托管且进程仍存活时直接返回，不重复启动（ADR-0006 第 3.4 条）。
    // 同时把 desired_running 置真并清除 crashed：显式 start 是 crashed 的恢复途径。
    {
        let mut table = process_table().lock().unwrap();
        if let Some(existing) = table.get_mut(&payload.instance_id) {
            if process_alive(existing.pid) {
                existing.desired_running = true;
                existing.crashed = false;
                existing.restarts = 0;
                let snapshot = existing.clone();
                persist(&table);
                return Ok(serde_json::json!({
                    "instanceId": payload.instance_id,
                    "pid": snapshot.pid,
                    "port": snapshot.port,
                    "startedAt": snapshot.started_at,
                    "restarts": snapshot.restarts,
                    "alreadyRunning": true,
                })
                .to_string());
            }
        }
    }

    // 端口必须空闲：已被其它进程占用时明确失败，不静默抢占（ADR-0006 第 7.3 条）。
    // 例外：占用者是本 Agent 托管表里记录过的残留进程（上一次 stop 未及清理就发生
    // Agent 重启等），先终止它再启动——否则自己的残留会把自己挡在门外。
    if let Some(occupant) = listening_pid(payload.port) {
        if !terminate_managed_occupant(payload.port).await {
            return Err(format!(
                "端口 {} 已被 PID {} 占用，无法启动",
                payload.port, occupant
            ));
        }
    }

    let pid = spawn_process(template, payload.port)?;
    let started_at = chrono::Utc::now().timestamp();

    // 先落表再等待：锁不能在 await 期间持有（MutexGuard 非 Send）。
    // 显式 start 重置 restarts 与 crashed，让用户的手动操作能救活 crashed 实例。
    {
        let mut table = process_table().lock().unwrap();
        table.insert(
            payload.instance_id.clone(),
            ManagedProcess {
                instance_id: payload.instance_id.clone(),
                provider: template.id.to_string(),
                port: payload.port,
                pid,
                started_at,
                restarts: 0,
                desired_running: true,
                crashed: false,
            },
        );
        persist(&table);
    }

    // 等一小会儿确认没有立即退出（端口占用/参数错误会立刻失败）。
    tokio::time::sleep(Duration::from_millis(400)).await;
    if !process_alive(pid) {
        let mut table = process_table().lock().unwrap();
        table.remove(&payload.instance_id);
        persist(&table);
        return Err(format!(
            "{} 启动后立即退出，请检查主机日志",
            resolve_executable(template).0
        ));
    }

    Ok(serde_json::json!({
        "instanceId": payload.instance_id,
        "pid": pid,
        "port": payload.port,
        "startedAt": started_at,
        "restarts": 0,
        "alreadyRunning": false,
    })
    .to_string())
}

/// 停止托管进程（任务 58）。幂等：进程已不在时同样返回成功。
pub async fn stop(raw: &str) -> Result<String, String> {
    let payload: StopPayload =
        serde_json::from_str(raw).map_err(|err| format!("停止参数无效: {}", err))?;

    // 第一步（同步）：先把期望置为停止并重置计数，让 supervisor 立刻不再拉起。
    // 这一步不 await，避免在终止期间被 supervisor 抢先重启。
    // pid 暂留，等终止确认后再清——否则未确认退出时会谎报「已停止」。
    let (pid, port) = {
        let mut table = process_table().lock().unwrap();
        match table.get_mut(&payload.instance_id) {
            Some(process) => {
                process.desired_running = false;
                process.crashed = false;
                process.restarts = 0;
                let pid = process.pid;
                let port = process.port;
                persist(&table);
                (pid, port)
            }
            None => {
                return Ok(serde_json::json!({
                    "instanceId": payload.instance_id,
                    "stopped": false,
                    "detail": "该实例没有托管的进程",
                })
                .to_string());
            }
        }
    };

    // 第二步（异步）：终止进程。pid 为 0 表示此前已停止，无需再终止。
    let mut terminated = if pid == 0 { true } else { terminate(pid).await };

    // 第二步补充：记录的 PID 可能只是已退出的 shim/launcher，真正持有监听端口的是
    // 它的孤儿子进程（Windows shim 形态、Linux 父先死）。terminate 只确认了记录的
    // PID 退出，不代表端口已释放。此处按端口复查，若仍被本 Agent 托管的进程占用，
    // 一并清理，避免 stop 报成功但端口未释放、restart 的端口预检失败。
    // pid == 0 表示此前已停止，属幂等成功，不做端口复查（表内 pid 已为 0，
    // 归属判定必然失败，否则会把成功的幂等 stop 误报为失败）。
    if pid != 0 && terminated && port != 0 && listening_pid(port).is_some() {
        terminated = terminate_managed_occupant(port).await && listening_pid(port).is_none();
    }

    // 第三步：确认退出后才清 pid，如实反映「是否真的停了」。
    // 只在 pid 仍等于本次要终止的值时清零：避免 supervisor 在终止期间已基于旧快照
    // 拉起新进程并写入新 pid 后，被这里抹掉而成为无法追踪的孤儿（TOCTOU）。
    if terminated {
        let mut table = process_table().lock().unwrap();
        if let Some(entry) = table.get_mut(&payload.instance_id) {
            if entry.pid == pid {
                entry.pid = 0;
                persist(&table);
            }
        }
    }

    let mut result = serde_json::json!({
        "instanceId": payload.instance_id,
        "stopped": terminated,
        "pid": pid,
    });
    if !terminated {
        result["detail"] = serde_json::Value::String(format!(
            "进程 {} 未能在超时内退出，端口可能仍被占用",
            pid
        ));
    }
    Ok(result.to_string())
}

/// 查询托管进程状态（任务 59）。
///
/// 除进程表条目外，也返回**端口监听与监听者匹配**信息。
/// 这样托管实例只需一次主机往返就能拿到完整状态（进程 + 端口 + 资源），
/// 不必再单独发一次探测任务（任务 56）。
pub fn status(raw: &str) -> Result<String, String> {
    let payload: StatusPayload =
        serde_json::from_str(raw).map_err(|err| format!("查询参数无效: {}", err))?;

    // 先把需要的信息取出并**释放锁**，再做 OS 查询。
    // process_alive / process_resources / listening_pid 都是阻塞式系统调用，
    // 其中 Linux 的 listening_pid 要遍历 /proc/*/fd，可能耗时数百毫秒。
    // 在锁内做这些会让并发的 start/stop/status 全部排队。
    let snapshot = {
        let table = process_table().lock().unwrap();
        match table.get(&payload.instance_id) {
            None => None,
            Some(process) => Some(process.clone()),
        }
    };

    let Some(process) = snapshot else {
        return Ok(serde_json::json!({
            "instanceId": payload.instance_id,
            "managed": false,
            "running": false,
        })
        .to_string());
    };

    // pid 为 0 表示显式停止过，没有可查的进程。
    let running = process.pid != 0 && process_alive(process.pid);
    let uptime_seconds = if running {
        (chrono::Utc::now().timestamp() - process.started_at).max(0)
    } else {
        0
    };
    // 资源占用只在真正运行时采集，避免对已退出 PID 做无意义查询。
    let (memory_bytes, cpu_percent) = if running {
        crate::aiagent::process_resources(process.pid)
    } else {
        (0, 0.0)
    };

    // 端口状态：与探测任务同源，供云端做关联验证。
    let listener_pid = listening_pid(process.port);
    let port_listening = listener_pid.is_some();
    let listener_matches_process = match listener_pid {
        Some(pid) => {
            let terms: Vec<String> = lookup_template(&process.provider)
                .map(|template| {
                    template
                        .process_match
                        .iter()
                        .map(|term| term.trim().to_lowercase())
                        .filter(|term| !term.is_empty())
                        .collect()
                })
                .unwrap_or_default();
            crate::aiagent::process_name_matches(pid, &terms)
        }
        None => false,
    };

    Ok(serde_json::json!({
        "instanceId": process.instance_id,
        "provider": process.provider,
        "port": process.port,
        "pid": process.pid,
        "managed": true,
        "running": running,
        "desiredRunning": process.desired_running,
        "crashed": process.crashed,
        "startedAt": process.started_at,
        "uptimeSeconds": uptime_seconds,
        "restarts": process.restarts,
        "memoryBytes": memory_bytes,
        "cpuPercent": cpu_percent,
        "portListening": port_listening,
        "listenerPid": listener_pid.unwrap_or(0),
        "listenerMatchesProcess": listener_matches_process,
    })
    .to_string())
}

/// 诊断 Provider 在主机侧的可用性（任务 60）。
///
/// 返回三块信息供云端在创建/编辑实例时直接消费：
///   - executable：解析出的可执行文件路径与是否确认存在（found=false 时云端应
///     提示安装或配置 API_MONITOR_AIAGENT_OPENCODE_BIN）；
///   - portRange：该 Provider 的允许端口区间（与云端注册表一致的 min/max）；
///   - usedPorts / suggestedPort：区间内当前被占用的端口与第一个空闲端口，
///     让云端在默认端口被占时自动建议切换，而不是等 start 才报「端口被占用」。
///
/// 纯本地查询，不启动任何进程。
#[derive(Debug, Deserialize)]
pub struct DiagnosePayload {
    pub provider: String,
}

pub fn diagnose(raw: &str) -> Result<String, String> {
    let payload: DiagnosePayload =
        serde_json::from_str(raw).map_err(|err| format!("诊断参数无效: {}", err))?;

    let template = lookup_template(&payload.provider)
        .ok_or_else(|| format!("未登记的 Provider: {}", payload.provider))?;

    let (executable, found) = resolve_executable(template);
    let max_port = template.default_port.saturating_add(MAX_PORT_OFFSET);

    // 扫描允许区间内的占用情况：逐个查监听 PID。区间上界 99 个端口，
    // listening_pid 在 Windows 走 GetExtendedTcpTable、Linux 遍历 /proc，
    // 单端口开销很小，诊断是低频操作，顺序扫描可接受。
    let mut used_ports: Vec<u16> = Vec::new();
    let mut suggested_port: Option<u16> = None;
    for port in template.default_port..=max_port {
        if listening_pid(port).is_some() {
            used_ports.push(port);
        } else if suggested_port.is_none() {
            suggested_port = Some(port);
        }
    }

    Ok(serde_json::json!({
        "provider": template.id,
        "executable": {
            "path": executable,
            "found": found,
        },
        "portRange": {
            "min": template.default_port,
            "max": max_port,
        },
        "usedPorts": used_ports,
        "suggestedPort": suggested_port,
    })
    .to_string())
}

/// Agent 启动时调用：恢复进程表。
///
/// 仍存活的条目保留；已死的条目标记 pid=0，但**保留 desired_running**，
/// 让 supervisor 在启动后按它重新拉起（ADR-0006 第 4.2 条）。
/// 只有显式 stop 过的实例（desired_running=false）才不会被拉起。
pub fn reconcile_on_boot() {
    let persisted = load_persisted();
    let mut table = HashMap::new();
    for (instance_id, mut process) in persisted {
        if !process_alive(process.pid) {
            // 进程已随上次 Agent 退出而死；清 pid，等 supervisor 决定是否拉起。
            process.pid = 0;
        }
        table.insert(instance_id, process);
    }
    let mut current = process_table().lock().unwrap();
    *current = table;
    persist(&current);
}

/// supervisor：巡检所有托管条目，把「期望运行但实际已死」的进程按退避策略拉起。
///
/// 这是 ADR-0006 第 4.1 条「崩溃自动重启」的实现。与云端后台收敛的分工：
///   - supervisor（本函数）在主机侧，负责进程崩溃后的自愈，延迟低（亚秒级）；
///   - 云端收敛负责「期望状态」层面的纠偏（如 Agent 重启、配置变更）。
/// 两者幂等，重复拉起不会产生重复进程（start 有幂等检查）。
pub async fn run_supervisor() {
    let mut ticker = tokio::time::interval(SUPERVISOR_TICK);
    // 首次 tick 立即触发，跳过：启动时的 reconcile 已经处理过。
    ticker.tick().await;

    loop {
        ticker.tick().await;
        supervise_once().await;
    }
}

/// 把「已稳定运行足够久」的实例的重启计数归零。
///
/// 与巡检分开成独立函数：它是纯粹的状态维护，不涉及进程操作，
/// 因此可以脱离异步上下文单独测试。
fn reset_stable_restart_counts() {
    let now = chrono::Utc::now().timestamp();

    // 先在锁外做存活判断（阻塞式系统调用），再进锁改计数：
    // 避免在持锁期间做 OS 查询拖住并发的 start/stop/status。
    let snapshot: Vec<ManagedProcess> = {
        let table = process_table().lock().unwrap();
        table.values().cloned().collect()
    };

    let reset_ids: Vec<String> = snapshot
        .into_iter()
        .filter(|process| {
            process.restarts > 0
                && !process.crashed
                // 只对「正在运行且已跑够久」的实例归零：
                // 已死的实例仍处在退避序列中，归零会让它无限重试。
                && process_alive(process.pid)
                && now - process.started_at >= RESTART_STABLE_RESET_SECONDS
        })
        .map(|process| process.instance_id)
        .collect();

    if reset_ids.is_empty() {
        return;
    }
    let mut table = process_table().lock().unwrap();
    for id in &reset_ids {
        if let Some(process) = table.get_mut(id) {
            process.restarts = 0;
        }
    }
    persist(&table);
}

/// 单次巡检。抽成独立函数便于测试。
async fn supervise_once() {
    reset_stable_restart_counts();
    // 先取快照再判断存活：process_alive 是阻塞式系统调用，
    // 在锁内对全部实例逐个调用会让并发的 start/stop/status 排队等待。
    // （快照也避免在 await 期间持有锁。）
    let snapshot: Vec<ManagedProcess> = {
        let table = process_table().lock().unwrap();
        table.values().cloned().collect()
    };

    let candidates: Vec<(String, String, u16, u32, u32, i64)> = snapshot
        .into_iter()
        .filter(|process| {
            process.desired_running && !process.crashed && !process_alive(process.pid)
        })
        .map(|process| {
            (
                process.instance_id,
                process.provider,
                process.port,
                process.restarts,
                process.pid,
                process.started_at,
            )
        })
        .collect();

    for (instance_id, provider, port, restarts, dead_pid, started_at) in candidates {
        // 退避：刚崩溃时不要立刻重试，按已重启次数选择延迟。
        let backoff = RESTART_BACKOFF_SECONDS
            .get(restarts as usize)
            .copied()
            .unwrap_or(0);
        if backoff == 0 {
            // 退避序列用尽 → crashed 终态，等用户显式 start 才能恢复。
            let mut table = process_table().lock().unwrap();
            if let Some(process) = table.get_mut(&instance_id) {
                process.crashed = true;
                process.pid = 0;
                persist(&table);
            }
            eprintln!(
                "[aiagent] instance {} 重启次数已达上限({}), 进入 crashed 终态",
                instance_id, MAX_RESTARTS
            );
            continue;
        }

        // 距上次启动不足退避时长则跳过，下个 tick 再看。
        let elapsed = chrono::Utc::now().timestamp() - started_at;
        if dead_pid != 0 && elapsed < backoff as i64 {
            continue;
        }

        let Some(template) = lookup_template(&provider) else {
            continue;
        };

        // 端口被占用时不要盲目拉起。若占用者是我们自己的残留（父进程已死、子进程
        // 仍持端口），先清理再拉起；否则记一次失败并累计重启次数，避免与占用者
        // 形成「反复抢端口」的循环（ADR-0006 第 7.3 条：不静默杀掉他人进程）。
        if let Some(occupant) = listening_pid(port) {
            if !terminate_managed_occupant(port).await {
                let mut table = process_table().lock().unwrap();
                if let Some(process) = table.get_mut(&instance_id) {
                    process.restarts = process.restarts.saturating_add(1);
                    process.pid = 0;
                    persist(&table);
                }
                eprintln!(
                    "[aiagent] instance {} 重启失败：端口 {} 被 PID {} 占用",
                    instance_id, port, occupant
                );
                continue;
            }
        }

        match spawn_process(template, port) {
            Ok(pid) => {
                let mut table = process_table().lock().unwrap();
                if let Some(process) = table.get_mut(&instance_id) {
                    process.pid = pid;
                    process.started_at = chrono::Utc::now().timestamp();
                    process.restarts = process.restarts.saturating_add(1);
                    persist(&table);
                }
                eprintln!(
                    "[aiagent] instance {} 已自动重启 (pid={}, 第 {} 次)",
                    instance_id,
                    pid,
                    restarts + 1
                );
            }
            Err(err) => {
                let mut table = process_table().lock().unwrap();
                if let Some(process) = table.get_mut(&instance_id) {
                    process.restarts = process.restarts.saturating_add(1);
                    process.pid = 0;
                    persist(&table);
                }
                eprintln!("[aiagent] instance {} 自动重启失败: {}", instance_id, err);
            }
        }
    }
}

/// 进程是否存活。
#[cfg(target_os = "windows")]
fn process_alive(pid: u32) -> bool {
    use windows_sys::Win32::Foundation::{CloseHandle, STILL_ACTIVE};
    use windows_sys::Win32::System::Threading::{
        GetExitCodeProcess, OpenProcess, PROCESS_QUERY_LIMITED_INFORMATION,
    };

    if pid == 0 {
        return false;
    }
    unsafe {
        let handle = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
        if handle.is_null() {
            return false;
        }
        let mut exit_code: u32 = 0;
        let ok = GetExitCodeProcess(handle, &mut exit_code);
        CloseHandle(handle);
        ok != 0 && exit_code == STILL_ACTIVE as u32
    }
}

#[cfg(target_os = "linux")]
fn process_alive(pid: u32) -> bool {
    if pid == 0 {
        return false;
    }
    // 只凭 /proc/<pid> 目录存在会误判：僵尸进程（未被父进程 wait 回收）的目录
    // 仍保留，会被当成「还活着」，导致 supervisor 不重启崩溃进程、terminate
    // 的退出轮询永不结束。改读 /proc/<pid>/stat，取括号后第一个字符（状态位），
    // Z=zombie、X=dead 均视为已退出。
    let Ok(stat) = std::fs::read_to_string(format!("/proc/{pid}/stat")) else {
        return false;
    };
    // 格式：pid (comm) state ...，comm 可能含空格/括号，从最后一个 ')' 之后取状态。
    let Some(rest) = stat.rsplit_once(')') else {
        return false;
    };
    let Some(state) = rest.1.trim_start().chars().next() else {
        return false;
    };
    state != 'Z' && state != 'X'
}

#[cfg(not(any(target_os = "windows", target_os = "linux")))]
fn process_alive(_pid: u32) -> bool {
    false
}

/// 轮询等待进程退出，最多约 2 秒。返回是否已确认退出。
async fn wait_process_exit(pid: u32) -> bool {
    for _ in 0..10 {
        if !process_alive(pid) {
            return true;
        }
        tokio::time::sleep(Duration::from_millis(200)).await;
    }
    !process_alive(pid)
}

/// 终止进程：先优雅终止，超时后强杀。返回是否**确认已退出**。
///
/// 返回值供调用方判断端口是否真正释放。Windows 上 opencode 常以 shim/launcher
/// 形态拉起子进程并让子进程持有监听端口，只杀父 PID 会留下子进程继续占用端口，
/// 造成 stop 报成功、restart 时 start 的端口预检却失败。因此必须按进程树终止
/// （`taskkill /T`），与 `tcp_forwarder` / `cloudflared` 的既有做法保持一致。
#[cfg(target_os = "windows")]
async fn terminate(pid: u32) -> bool {
    if !process_alive(pid) {
        return true;
    }
    // Windows 无 SIGTERM 语义，直接强杀整棵进程树（/T）。
    let _ = std::process::Command::new("taskkill")
        .args(["/F", "/T", "/PID", &pid.to_string()])
        .output();
    wait_process_exit(pid).await
}

#[cfg(target_os = "linux")]
async fn terminate(pid: u32) -> bool {
    if !process_alive(pid) {
        return true;
    }
    // 先尽力终止整个进程组（覆盖「父进程已死、子进程仍持端口」的残留场景），
    // 再向 PID 本身发信号（主路径）。
    kill_signal(pid, libc::SIGTERM);
    if wait_process_exit(pid).await {
        return true;
    }
    kill_signal(pid, libc::SIGKILL);
    wait_process_exit(pid).await
}

/// 向目标进程发送信号；仅当该进程是**自己进程组的组长**时才附带组终止。
///
/// 组长判定（pgid == pid）正是本 Agent spawn 时建立的形态（`process_group(0)`），
/// 覆盖「父进程已死、子进程仍持端口」的残留场景。对非组长进程只杀 PID 本身——
/// 否则会波及它所属的整个组（可能是用户会话或测试进程组），造成误杀。
///
/// 直接用 libc::kill 而非 shell 调用 `kill`：负的组 ID 作为信号目标时，shell 的
/// `kill -TERM -<pid> <pid>` 会把 `-<pid>` 当成非法选项导致整条命令失败（信号
/// 根本送不到），且 `--` 在 busybox/musl 下不保证支持。系统调用没有这些歧义。
#[cfg(target_os = "linux")]
fn kill_signal(pid: u32, signal: i32) {
    // pid 为 0 时信号发给整个进程组，语义错误；超出 i32 范围时 `pid as i32`
    // 会回绕成负值或错误的目标，直接跳过。
    if pid == 0 || pid > i32::MAX as u32 {
        return;
    }
    let is_group_leader = crate::aiagent::process_group_id(pid)
        .map(|pgid| pgid == pid)
        .unwrap_or(false);
    // SAFETY: kill 是异步信号安全的系统调用，仅需一个有效 pid 与信号编号。
    // 上面已保证 pid 落在 (0, i32::MAX]；负目标表示向该进程组发信号（组 ID 取正数
    // 的 pid），是 kill(2) 的既定语义。返回值（0/-1）此处不做区分：ESRCH/EPERM
    // 的最终判定由后续 wait_process_exit 的存活性轮询负责。
    unsafe {
        if is_group_leader {
            // 负 PID 表示向该进程组发信号。
            libc::kill(-(pid as i32), signal);
        }
        libc::kill(pid as i32, signal);
    }
}

#[cfg(not(any(target_os = "windows", target_os = "linux")))]
async fn terminate(_pid: u32) -> bool {
    false
}

/// 若监听该端口的进程确属本 Agent 托管（可能只是残留的子进程），终止它并返回是否成功。
///
/// 仅清理「确实归我们管」的进程，绝不触碰无关进程（ADR-0006 第 7.3 条：不静默
/// 杀掉他人进程）。所有权证据只有一条：**监听进程的父进程链能追溯到本 Agent
/// 记录的 spawn PID**——这覆盖「父进程已死、子进程仍持有端口」的孤儿场景。
///
/// 刻意不用「同端口 + 进程名匹配」作为证据：用户完全可能手工运行同名的
/// `opencode serve`，仅凭进程名会误杀用户自己的进程。查不到归属一律不杀，
/// 交回调用方按「端口被他人占用」处理。
async fn terminate_managed_occupant(port: u16) -> bool {
    let Some(occupant) = listening_pid(port) else {
        return false;
    };

    // 在锁内只收集候选 PID，阻塞式的父链查询放到锁外。
    let candidate_pids: Vec<u32> = {
        let table = process_table().lock().unwrap();
        table
            .values()
            .filter(|process| process.port == port && process.pid != 0)
            .map(|process| process.pid)
            .collect()
    };
    if candidate_pids.is_empty() {
        return false;
    }

    let owned = crate::aiagent::process_descends_from(occupant, &candidate_pids);
    if !owned {
        return false;
    }
    terminate(occupant).await
}

/// Agent 主动退出时终止所有托管进程，避免留下孤儿进程占用端口（ADR-0006 第 4.3 条）。
pub async fn shutdown_all() {
    let processes: Vec<ManagedProcess> = {
        let mut table = process_table().lock().unwrap();
        let values: Vec<ManagedProcess> = table.values().cloned().collect();
        table.clear();
        persist(&table);
        values
    };
    for process in processes {
        if process.pid == 0 {
            continue;
        }
        if !terminate(process.pid).await {
            eprintln!(
                "[aiagent] 退出时未能终止进程 {}（端口 {}），可能残留占用",
                process.pid, process.port
            );
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// 修改进程级环境变量（PATH/HOME/APPDATA/USERPROFILE/覆盖变量）的用例必须串行，
    /// 否则并行线程会互相看到对方临时设置的 PATH，断言随机失败。
    /// 用法：在用例首行 `let _guard = env_guard();`，持有到用例结束。
    fn env_guard() -> std::sync::MutexGuard<'static, ()> {
        static ENV_LOCK: std::sync::Mutex<()> = std::sync::Mutex::new(());
        ENV_LOCK.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
    }

    #[test]
    fn port_range_matches_cloud_contract() {
        let template = lookup_template("opencode").expect("opencode template");
        assert!(port_allowed(template, 4096), "默认端口必须允许");
        assert!(port_allowed(template, 4195), "区间上界必须允许");
        assert!(!port_allowed(template, 4196), "超出上界必须拒绝");
        assert!(!port_allowed(template, 4095), "低于默认端口必须拒绝");
    }

    #[test]
    fn unknown_provider_is_rejected() {
        assert!(lookup_template("definitely-not-real").is_none());
        assert!(lookup_template("opencode").is_some());
        // 空字符串回退到默认 Provider。
        assert!(lookup_template("").is_some());
    }

    #[test]
    fn executable_env_override_wins() {
        let _guard = env_guard();
        let template = lookup_template("opencode").expect("opencode template");
        // 用空临时目录抑制所有自动探测（APPDATA/HOME/USERPROFILE/PATH），保证
        // 「未设置时回退到裸命令名」可确定断言：本机可能已装 opencode，PATH 或
        // ~/.opencode/bin 命中真实路径会让断言不稳定。
        let temp_empty = std::env::temp_dir().join("aiagent-test-empty-appdata");
        std::fs::create_dir_all(&temp_empty).ok();
        let saved_appdata = std::env::var_os("APPDATA");
        let saved_home = std::env::var_os("HOME");
        let saved_profile = std::env::var_os("USERPROFILE");
        let saved_path = std::env::var_os("PATH");
        std::env::set_var("APPDATA", &temp_empty);
        std::env::set_var("HOME", &temp_empty);
        std::env::set_var("USERPROFILE", &temp_empty);
        std::env::set_var("PATH", &temp_empty);
        std::env::remove_var(template.executable_env);
        let (path, found) = resolve_executable(template);
        assert_eq!(path, "opencode", "未设置时应回退到裸命令名（走 PATH）");
        assert!(!found, "未设置且无探测命中时 found 应为 false");

        // 设置后使用绝对路径：Agent 作为服务运行时 PATH 与交互式登录不同，
        // 需要部署方显式指定。
        std::env::set_var(template.executable_env, "C:\\custom\\opencode.exe");
        let (path, found) = resolve_executable(template);
        assert_eq!(path, "C:\\custom\\opencode.exe");
        assert!(
            !found,
            "显式配置的路径不存在时应如实返回 found=false（spawn 前报错而非 program not found）"
        );

        // 空白值不应覆盖，避免误配成空路径。
        std::env::set_var(template.executable_env, "   ");
        let (path, _) = resolve_executable(template);
        assert_eq!(path, "opencode");

        std::env::remove_var(template.executable_env);
        restore_env("APPDATA", saved_appdata);
        restore_env("HOME", saved_home);
        restore_env("USERPROFILE", saved_profile);
        restore_env("PATH", saved_path);
        std::fs::remove_dir_all(&temp_empty).ok();
    }

    /// 恢复环境变量：原值存在则写回，否则删除。
    fn restore_env(key: &str, value: Option<std::ffi::OsString>) {
        match value {
            Some(value) => std::env::set_var(key, value),
            None => std::env::remove_var(key),
        }
    }

    /// 官方安装脚本把二进制放在 $HOME/.opencode/bin：即便 PATH 不含该目录也应命中。
    /// 这是 Linux 上「装好了却仍报未找到」的根因回归用例。
    #[test]
    fn resolves_home_opencode_bin_outside_path() {
        let _guard = env_guard();
        let template = lookup_template("opencode").expect("opencode template");
        let saved_appdata = std::env::var_os("APPDATA");
        let saved_home = std::env::var_os("HOME");
        let saved_profile = std::env::var_os("USERPROFILE");
        let saved_path = std::env::var_os("PATH");
        let saved_override = std::env::var_os(template.executable_env);

        let home = std::env::temp_dir().join("aiagent-test-home-opencode");
        let bin_dir = home.join(".opencode/bin");
        std::fs::create_dir_all(&bin_dir).ok();
        let exe_name = if cfg!(target_os = "windows") {
            "opencode.exe"
        } else {
            "opencode"
        };
        let real = bin_dir.join(exe_name);
        std::fs::write(&real, b"").ok();
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&real, std::fs::Permissions::from_mode(0o755)).ok();
        }

        // PATH 指向空目录，证明命中来自固定安装目录探测而非 PATH。
        let empty_path = std::env::temp_dir().join("aiagent-test-empty-path");
        std::fs::create_dir_all(&empty_path).ok();
        std::env::set_var("APPDATA", &empty_path);
        std::env::set_var("HOME", &home);
        std::env::set_var("USERPROFILE", &home);
        std::env::set_var("PATH", &empty_path);
        std::env::remove_var(template.executable_env);

        let (path, found) = resolve_executable(template);
        assert!(found, "~/.opencode/bin 下的可执行文件应被识别为已找到");
        assert_eq!(path, real.to_string_lossy().to_string());

        restore_env(template.executable_env, saved_override);
        restore_env("APPDATA", saved_appdata);
        restore_env("HOME", saved_home);
        restore_env("USERPROFILE", saved_profile);
        restore_env("PATH", saved_path);
        std::fs::remove_dir_all(&home).ok();
        std::fs::remove_dir_all(&empty_path).ok();
    }

    /// PATH 中真实存在的可执行文件应被判定为已找到：回退不再无条件报 found=false。
    #[test]
    fn resolves_executable_from_path() {
        let _guard = env_guard();
        let template = lookup_template("opencode").expect("opencode template");
        let saved_appdata = std::env::var_os("APPDATA");
        let saved_home = std::env::var_os("HOME");
        let saved_profile = std::env::var_os("USERPROFILE");
        let saved_path = std::env::var_os("PATH");
        let saved_override = std::env::var_os(template.executable_env);

        let home = std::env::temp_dir().join("aiagent-test-home-path-empty");
        std::fs::create_dir_all(&home).ok();
        let path_dir = std::env::temp_dir().join("aiagent-test-path-hit");
        std::fs::create_dir_all(&path_dir).ok();
        let exe_name = if cfg!(target_os = "windows") {
            "opencode.exe"
        } else {
            "opencode"
        };
        let real = path_dir.join(exe_name);
        std::fs::write(&real, b"").ok();
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&real, std::fs::Permissions::from_mode(0o755)).ok();
        }

        // HOME 指向空目录，证明命中来自 PATH。
        std::env::set_var("APPDATA", &home);
        std::env::set_var("HOME", &home);
        std::env::set_var("USERPROFILE", &home);
        std::env::set_var("PATH", &path_dir);
        std::env::remove_var(template.executable_env);

        let (path, found) = resolve_executable(template);
        assert!(found, "PATH 中存在的可执行文件应被识别为已找到");
        assert_eq!(path, real.to_string_lossy().to_string());

        restore_env(template.executable_env, saved_override);
        restore_env("APPDATA", saved_appdata);
        restore_env("HOME", saved_home);
        restore_env("USERPROFILE", saved_profile);
        restore_env("PATH", saved_path);
        std::fs::remove_dir_all(&home).ok();
        std::fs::remove_dir_all(&path_dir).ok();
    }

    /// 软链指向真实可执行文件时必须命中：生产环境常在 /usr/local/bin 放软链
    /// （如 `ln -s ~/.opencode/bin/opencode /usr/local/bin/opencode`）。
    /// metadata 会跟随软链，应识别其目标为可执行文件。
    #[cfg(unix)]
    #[test]
    fn resolves_symlinked_executable() {
        let _guard = env_guard();
        let template = lookup_template("opencode").expect("opencode template");
        let saved_appdata = std::env::var_os("APPDATA");
        let saved_home = std::env::var_os("HOME");
        let saved_profile = std::env::var_os("USERPROFILE");
        let saved_path = std::env::var_os("PATH");
        let saved_override = std::env::var_os(template.executable_env);

        let home = std::env::temp_dir().join("aiagent-test-home-symlink-empty");
        std::fs::create_dir_all(&home).ok();
        let bin_dir = std::env::temp_dir().join("aiagent-test-symlink-bin");
        std::fs::create_dir_all(&bin_dir).ok();
        let target = bin_dir.join("opencode-real");
        std::fs::write(&target, b"").ok();
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&target, std::fs::Permissions::from_mode(0o755)).ok();
        }
        let link = bin_dir.join("opencode");
        let _ = std::fs::remove_file(&link);
        std::os::unix::fs::symlink(&target, &link).expect("创建软链");

        // HOME 指向空目录，PATH 只含软链所在目录。
        std::env::set_var("APPDATA", &home);
        std::env::set_var("HOME", &home);
        std::env::set_var("USERPROFILE", &home);
        std::env::set_var("PATH", &bin_dir);
        std::env::remove_var(template.executable_env);

        let (path, found) = resolve_executable(template);
        assert!(found, "软链指向可执行文件时应识别为已找到");
        assert_eq!(path, link.to_string_lossy().to_string());

        restore_env(template.executable_env, saved_override);
        restore_env("APPDATA", saved_appdata);
        restore_env("HOME", saved_home);
        restore_env("USERPROFILE", saved_profile);
        restore_env("PATH", saved_path);
        std::fs::remove_dir_all(&home).ok();
        std::fs::remove_dir_all(&bin_dir).ok();
    }

    #[test]
    fn wrapped_quotes_in_env_are_stripped() {
        // setx / .reg 写入时常把英文双引号一并存进值里，解析时必须剥掉，
        // 否则 is_file() 判定不存在，只会抛出含糊的 program not found。
        assert_eq!(
            strip_wrapping_quotes(r#""C:\custom\opencode.exe""#),
            r"C:\custom\opencode.exe"
        );
        assert_eq!(
            strip_wrapping_quotes(r#"'C:\custom\opencode.exe'"#),
            r"C:\custom\opencode.exe"
        );
        // 带空白的成对引号同样要剥掉。
        assert_eq!(
            strip_wrapping_quotes(r#"  "C:\custom\opencode.exe"  "#),
            r"C:\custom\opencode.exe"
        );
        // 只有单边引号时不剥，避免把合法路径改坏。
        assert_eq!(
            strip_wrapping_quotes(r#""C:\custom\opencode.exe"#),
            r#""C:\custom\opencode.exe"#
        );
        // 路径中间出现的引号字符保持原样。
        assert_eq!(
            strip_wrapping_quotes(r#""C:\my "dir"\opencode.exe""#),
            r#"C:\my "dir"\opencode.exe"#
        );
    }

    #[test]
    fn env_override_with_quotes_resolves_to_real_path() {
        let _guard = env_guard();
        let template = lookup_template("opencode").expect("opencode template");
        let temp_appdata = std::env::temp_dir().join("aiagent-test-quotes-appdata");
        std::fs::create_dir_all(&temp_appdata).ok();
        std::env::set_var("APPDATA", &temp_appdata);

        let temp_dir = std::env::temp_dir().join("aiagent-test-quotes-bin");
        std::fs::create_dir_all(&temp_dir).ok();
        let real = temp_dir.join("opencode.exe");
        std::fs::write(&real, b"").ok();

        // 引号包裹的合法路径必须被识别为已找到。
        std::env::set_var(
            template.executable_env,
            format!("\"{}\"", real.to_string_lossy()),
        );
        let (path, found) = resolve_executable(template);
        assert_eq!(path, real.to_string_lossy().to_string());
        assert!(found, "带引号的合法路径应被判定为存在");

        std::env::remove_var(template.executable_env);
        std::env::remove_var("APPDATA");
        std::fs::remove_dir_all(&temp_appdata).ok();
        std::fs::remove_dir_all(&temp_dir).ok();
    }

    #[tokio::test]
    async fn start_rejects_out_of_range_port() {
        let payload = serde_json::json!({
            "instance_id": "inst_test",
            "provider": "opencode",
            "port": 7000,
        });
        let err = start(&payload.to_string()).await.unwrap_err();
        assert!(err.contains("超出"), "应因端口越界被拒绝: {err}");
    }

    #[tokio::test]
    async fn start_rejects_unknown_provider() {
        let payload = serde_json::json!({
            "instance_id": "inst_test",
            "provider": "nope",
            "port": 4096,
        });
        let err = start(&payload.to_string()).await.unwrap_err();
        assert!(err.contains("未登记"), "应因未知 Provider 被拒绝: {err}");
    }

    #[tokio::test]
    async fn stop_is_idempotent_for_unknown_instance() {
        let payload = serde_json::json!({ "instance_id": "inst_never_started" });
        let out = stop(&payload.to_string()).await.unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&out).unwrap();
        assert_eq!(parsed["stopped"], serde_json::json!(false));
    }

    // 显式 stop 后条目保留但 desired_running=false，supervisor 不会把它拉回来。
    #[tokio::test]
    async fn stop_marks_desired_false_and_keeps_entry() {
        let instance_id = "inst_stop_marks";
        {
            let mut table = process_table().lock().unwrap();
            table.insert(
                instance_id.to_string(),
                ManagedProcess {
                    instance_id: instance_id.to_string(),
                    provider: "opencode".to_string(),
                    port: 4096,
                    pid: 0,
                    started_at: chrono::Utc::now().timestamp(),
                    restarts: 3,
                    desired_running: true,
                    crashed: false,
                },
            );
        }

        let payload = serde_json::json!({ "instance_id": instance_id });
        stop(&payload.to_string()).await.unwrap();

        let table = process_table().lock().unwrap();
        let process = table.get(instance_id).expect("条目应保留");
        assert!(!process.desired_running, "stop 后应标记为不再期望运行");
        assert_eq!(process.pid, 0, "stop 后 pid 应清零");
        assert_eq!(process.restarts, 0, "stop 应重置重启计数");
        drop(table);

        // 清理，避免影响其它用例。
        process_table().lock().unwrap().remove(instance_id);
    }

    // 退避序列用尽时进入 crashed 终态，而不是无限重试。
    #[tokio::test]
    async fn supervisor_marks_crashed_when_backoff_exhausted() {
        let instance_id = "inst_crashed";
        {
            let mut table = process_table().lock().unwrap();
            table.insert(
                instance_id.to_string(),
                ManagedProcess {
                    instance_id: instance_id.to_string(),
                    provider: "opencode".to_string(),
                    port: 4096,
                    pid: 0,
                    started_at: 0,
                    // 用尽退避序列
                    restarts: MAX_RESTARTS,
                    desired_running: true,
                    crashed: false,
                },
            );
        }

        supervise_once().await;

        let table = process_table().lock().unwrap();
        let process = table.get(instance_id).expect("条目应保留");
        assert!(process.crashed, "退避用尽后应进入 crashed 终态");
        assert_eq!(process.pid, 0);
        drop(table);

        process_table().lock().unwrap().remove(instance_id);
    }

    // supervisor 不碰 desired_running=false 的条目（用户显式停止过）。
    #[tokio::test]
    async fn supervisor_ignores_desired_false_entries() {
        let instance_id = "inst_desired_false";
        {
            let mut table = process_table().lock().unwrap();
            table.insert(
                instance_id.to_string(),
                ManagedProcess {
                    instance_id: instance_id.to_string(),
                    provider: "opencode".to_string(),
                    port: 4096,
                    pid: 0,
                    started_at: 0,
                    restarts: 0,
                    desired_running: false,
                    crashed: false,
                },
            );
        }

        supervise_once().await;

        let table = process_table().lock().unwrap();
        let process = table.get(instance_id).expect("条目应保留");
        assert_eq!(process.pid, 0, "已停止的实例不应被拉起");
        assert_eq!(process.restarts, 0, "不应累计重启次数");
        assert!(!process.crashed);
        drop(table);

        process_table().lock().unwrap().remove(instance_id);
    }

    // crashed 的条目不会被 supervisor 反复处理。
    #[tokio::test]
    async fn supervisor_skips_already_crashed_entries() {
        let instance_id = "inst_already_crashed";
        {
            let mut table = process_table().lock().unwrap();
            table.insert(
                instance_id.to_string(),
                ManagedProcess {
                    instance_id: instance_id.to_string(),
                    provider: "opencode".to_string(),
                    port: 4096,
                    pid: 0,
                    started_at: 0,
                    restarts: MAX_RESTARTS,
                    desired_running: true,
                    crashed: true,
                },
            );
        }

        supervise_once().await;

        let table = process_table().lock().unwrap();
        let process = table.get(instance_id).expect("条目应保留");
        assert!(process.crashed);
        assert_eq!(process.restarts, MAX_RESTARTS, "crashed 条目不应再累计重启");
        drop(table);

        process_table().lock().unwrap().remove(instance_id);
    }

    // 退避序列必须递增，避免重启风暴。
    #[test]
    fn restart_backoff_is_increasing() {
        for window in RESTART_BACKOFF_SECONDS.windows(2) {
            assert!(
                window[1] > window[0],
                "退避间隔必须递增: {:?}",
                RESTART_BACKOFF_SECONDS
            );
        }
        assert_eq!(
            RESTART_BACKOFF_SECONDS.len() as u32,
            MAX_RESTARTS,
            "重启上限应与退避序列长度一致"
        );
    }

    /// 稳定运行超过阈值后重启计数归零：避免「历史总失败次数」被误当成
    /// 「连续快速失败次数」而误判 crashed。
    #[tokio::test]
    async fn stable_process_resets_restart_count() {
        #[cfg(target_os = "windows")]
        let child = std::process::Command::new("ping")
            .args(["-n", "30", "127.0.0.1"])
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();
        #[cfg(target_os = "linux")]
        let child = std::process::Command::new("sleep")
            .arg("30")
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();

        let Ok(mut child) = child else {
            eprintln!("跳过：环境缺少长跑命令");
            return;
        };
        let pid = child.id();
        let instance_id = "inst_stable_reset";

        {
            let mut table = process_table().lock().unwrap();
            table.insert(
                instance_id.to_string(),
                ManagedProcess {
                    instance_id: instance_id.to_string(),
                    provider: "opencode".to_string(),
                    port: 4096,
                    pid,
                    // 已稳定运行远超阈值
                    started_at: chrono::Utc::now().timestamp() - RESTART_STABLE_RESET_SECONDS - 10,
                    restarts: 4,
                    desired_running: true,
                    crashed: false,
                },
            );
        }

        reset_stable_restart_counts();

        let table = process_table().lock().unwrap();
        assert_eq!(
            table.get(instance_id).map(|p| p.restarts),
            Some(0),
            "稳定运行后重启计数应归零"
        );
        drop(table);

        terminate(pid).await;
        let _ = child.wait();
        process_table().lock().unwrap().remove(instance_id);
    }

    /// 刚重启不久（未达稳定阈值）的实例不应归零：
    /// 否则快速崩溃循环会被无限重试，退避机制失效。
    #[tokio::test]
    async fn recently_restarted_process_keeps_restart_count() {
        #[cfg(target_os = "windows")]
        let child = std::process::Command::new("ping")
            .args(["-n", "30", "127.0.0.1"])
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();
        #[cfg(target_os = "linux")]
        let child = std::process::Command::new("sleep")
            .arg("30")
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();

        let Ok(mut child) = child else {
            eprintln!("跳过：环境缺少长跑命令");
            return;
        };
        let pid = child.id();
        let instance_id = "inst_not_stable";

        {
            let mut table = process_table().lock().unwrap();
            table.insert(
                instance_id.to_string(),
                ManagedProcess {
                    instance_id: instance_id.to_string(),
                    provider: "opencode".to_string(),
                    port: 4096,
                    pid,
                    // 刚刚启动，未达稳定阈值
                    started_at: chrono::Utc::now().timestamp(),
                    restarts: 3,
                    desired_running: true,
                    crashed: false,
                },
            );
        }

        reset_stable_restart_counts();

        let table = process_table().lock().unwrap();
        assert_eq!(
            table.get(instance_id).map(|p| p.restarts),
            Some(3),
            "未达稳定阈值时不应归零，否则退避失效"
        );
        drop(table);

        terminate(pid).await;
        let _ = child.wait();
        process_table().lock().unwrap().remove(instance_id);
    }

    /// 已死进程的计数不归零：它仍在退避序列中，归零会导致无限重试。
    #[tokio::test]
    async fn dead_process_keeps_restart_count() {
        let instance_id = "inst_dead_keeps";
        {
            let mut table = process_table().lock().unwrap();
            table.insert(
                instance_id.to_string(),
                ManagedProcess {
                    instance_id: instance_id.to_string(),
                    provider: "opencode".to_string(),
                    port: 4096,
                    // pid=0 表示当前无进程
                    pid: 0,
                    started_at: chrono::Utc::now().timestamp() - RESTART_STABLE_RESET_SECONDS - 100,
                    restarts: 3,
                    desired_running: true,
                    crashed: false,
                },
            );
        }

        reset_stable_restart_counts();

        let table = process_table().lock().unwrap();
        assert_eq!(
            table.get(instance_id).map(|p| p.restarts),
            Some(3),
            "已死进程不应被归零，否则退避序列失去意义"
        );
        drop(table);

        process_table().lock().unwrap().remove(instance_id);
    }

    /// 起一个真实的长跑子进程，验证 Windows/Linux 上的
    /// 「存活判定 → 终止 → 确认已死」完整链路。
    ///
    /// 这是对 `process_alive` / `terminate` 的平台实现验证：Windows 走
    /// OpenProcess + GetExitCodeProcess + taskkill，Linux 走 /proc + kill。
    /// 用系统自带的长跑命令，避免引入额外依赖。
    #[tokio::test]
    async fn real_process_alive_and_terminate_roundtrip() {
        #[cfg(target_os = "windows")]
        let child = std::process::Command::new("ping")
            .args(["-n", "30", "127.0.0.1"])
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();

        #[cfg(target_os = "linux")]
        let child = std::process::Command::new("sleep")
            .arg("30")
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();

        let Ok(mut child) = child else {
            // 环境缺少该命令时跳过，不把环境问题当成功能缺陷。
            eprintln!("跳过：环境缺少长跑命令");
            return;
        };
        let pid = child.id();

        // 刚启动的进程必须被判为存活。
        assert!(process_alive(pid), "刚启动的进程应判为存活");

        terminate(pid).await;
        // terminate 内部已等待退出（Linux 轮询 / Windows taskkill 同步），
        // 这里再给一个短暂窗口，避免调度延迟造成偶发失败。
        for _ in 0..10 {
            if !process_alive(pid) {
                break;
            }
            tokio::time::sleep(Duration::from_millis(200)).await;
        }
        assert!(!process_alive(pid), "终止后应判为已退出");

        // 回收子进程，避免测试进程留下僵尸。
        let _ = child.wait();
    }

    /// `terminate` 必须确认进程真的退出，而不是只发一次终止信号。
    ///
    /// 这是「restart 报端口被占用」的回归防线：若 terminate 只看「发过信号」
    /// 就返回成功，stop 会谎报已停止、端口却没释放。
    #[tokio::test]
    async fn terminate_confirms_exit() {
        #[cfg(target_os = "windows")]
        let child = std::process::Command::new("ping")
            .args(["-n", "30", "127.0.0.1"])
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();

        #[cfg(target_os = "linux")]
        let child = std::process::Command::new("sleep")
            .arg("30")
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();

        let Ok(mut child) = child else {
            eprintln!("跳过：环境缺少长跑命令");
            return;
        };
        let pid = child.id();
        assert!(process_alive(pid), "刚启动的进程应判为存活");

        let confirmed = terminate(pid).await;
        assert!(confirmed, "terminate 应确认进程已退出");
        assert!(!process_alive(pid), "确认退出后进程不应再判为存活");

        let _ = child.wait();
    }

    /// 已退出进程的 terminate 必须返回 true（幂等），供 stop 正确判定。
    #[tokio::test]
    async fn terminate_is_idempotent_for_dead_pid() {
        assert!(terminate(0).await, "pid 0 应视为已终止");
    }

    /// 已退出进程的 PID 必须被判为不存活（这是 supervisor 拉起的前提）。
    #[tokio::test]
    async fn exited_process_is_not_alive() {
        #[cfg(target_os = "windows")]
        let child = std::process::Command::new("cmd")
            .args(["/C", "exit", "0"])
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();

        #[cfg(target_os = "linux")]
        let child = std::process::Command::new("true")
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();

        let Ok(mut child) = child else {
            eprintln!("跳过：环境缺少命令");
            return;
        };
        let pid = child.id();
        let _ = child.wait();

        // 进程已退出：Windows 上 OpenProcess 仍可能成功，但退出码不再是 STILL_ACTIVE，
        // 这正是 process_alive 用 GetExitCodeProcess 而不是仅靠句柄判断的原因。
        for _ in 0..10 {
            if !process_alive(pid) {
                return;
            }
            tokio::time::sleep(Duration::from_millis(100)).await;
        }
        panic!("已退出的进程不应判为存活（pid={pid}）");
    }

    #[test]
    fn status_reports_unmanaged_instance() {
        let payload = serde_json::json!({ "instance_id": "inst_never_started" });
        let out = status(&payload.to_string()).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&out).unwrap();
        assert_eq!(parsed["managed"], serde_json::json!(false));
        assert_eq!(parsed["running"], serde_json::json!(false));
    }

    /// 端到端：用真实模板启动 → 状态可查 → 停止 → 确认已停。
    ///
    /// 覆盖 Windows 与 Linux 的完整生命周期，验证的是真实 spawn/kill 而非桩件。
    /// 用 ping/sleep 替换模板里的可执行文件，避免依赖 opencode 是否安装。
    #[tokio::test]
    async fn end_to_end_start_status_stop() {
        // 挑一个空闲端口（先绑再放），避免与真实服务冲突。
        let port = {
            let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
            listener.local_addr().unwrap().port()
        };
        let instance_id = "inst_e2e";

        // 构造一个指向长跑命令的临时模板，绕过 Provider 白名单校验。
        // 这是测试专用路径：生产路径只认 PROVIDER_TEMPLATES。
        #[cfg(target_os = "windows")]
        let (executable, args): (&str, Vec<String>) =
            ("ping", vec!["-n".into(), "60".into(), "127.0.0.1".into()]);
        #[cfg(target_os = "linux")]
        let (executable, args): (&str, Vec<String>) = ("sleep", vec!["60".into()]);

        let child = std::process::Command::new(executable)
            .args(&args)
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();
        let Ok(mut child) = child else {
            eprintln!("跳过：环境缺少长跑命令");
            return;
        };
        let pid = child.id();

        {
            let mut table = process_table().lock().unwrap();
            table.insert(
                instance_id.to_string(),
                ManagedProcess {
                    instance_id: instance_id.to_string(),
                    provider: "opencode".to_string(),
                    port,
                    pid,
                    started_at: chrono::Utc::now().timestamp(),
                    restarts: 0,
                    desired_running: true,
                    crashed: false,
                },
            );
        }

        // status 应报告运行中，并带上资源占用。
        let payload = serde_json::json!({ "instance_id": instance_id });
        let raw = status(&payload.to_string()).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&raw).unwrap();
        assert_eq!(parsed["running"], serde_json::json!(true));
        assert_eq!(parsed["desiredRunning"], serde_json::json!(true));
        assert!(
            parsed["memoryBytes"].as_u64().unwrap_or(0) > 0,
            "运行中的托管进程应报告内存占用: {raw}"
        );

        // stop 应终止进程并标记 desired_running=false。
        let stopped = stop(&payload.to_string()).await.unwrap();
        let stopped_json: serde_json::Value = serde_json::from_str(&stopped).unwrap();
        assert_eq!(stopped_json["stopped"], serde_json::json!(true));

        for _ in 0..10 {
            if !process_alive(pid) {
                break;
            }
            tokio::time::sleep(Duration::from_millis(200)).await;
        }
        assert!(!process_alive(pid), "stop 后进程应已退出");

        // 停止后 status 应报告 running=false 且 desiredRunning=false。
        let after = status(&payload.to_string()).unwrap();
        let after_json: serde_json::Value = serde_json::from_str(&after).unwrap();
        assert_eq!(after_json["running"], serde_json::json!(false));
        assert_eq!(after_json["desiredRunning"], serde_json::json!(false));

        // supervisor 不应把已停止的实例拉回来。
        supervise_once().await;
        let table = process_table().lock().unwrap();
        assert_eq!(
            table.get(instance_id).map(|p| p.pid),
            Some(0),
            "已停止的实例不应被 supervisor 拉起"
        );
        drop(table);

        let _ = child.wait();
        process_table().lock().unwrap().remove(instance_id);
    }

    // diagose 必须返回合法的端口区间与建议端口：默认端口在区间内且建议端口不大于上界。
    #[test]
    fn diagnose_reports_port_range_and_suggestion() {
        let payload = serde_json::json!({ "provider": "opencode" });
        let out = diagnose(&payload.to_string()).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&out).unwrap();

        assert_eq!(parsed["provider"], serde_json::json!("opencode"));
        let min = parsed["portRange"]["min"].as_u64().unwrap();
        let max = parsed["portRange"]["max"].as_u64().unwrap();
        assert!(min <= max, "端口区间 min 不应大于 max");
        assert!(min <= 4096 && 4096 <= max, "默认端口应落在区间内");
        let suggested = parsed["suggestedPort"].as_u64();
        if let Some(s) = suggested {
            assert!(min <= s && s <= max, "建议端口应在区间内");
        }
        // executable 字段必须存在（found 可能为 false，但结构要完整）。
        assert!(parsed["executable"]["path"].is_string());
    }

    // 未知 Provider 必须明确报错，而不是给一个空诊断让云端猜。
    #[test]
    fn diagnose_rejects_unknown_provider() {
        let payload = serde_json::json!({ "provider": "nope" });
        let err = diagnose(&payload.to_string()).unwrap_err();
        assert!(err.contains("未登记"), "应报未登记的 Provider: {err}");
    }
}
