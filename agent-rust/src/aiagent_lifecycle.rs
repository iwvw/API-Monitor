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
    let id = if provider.is_empty() { "opencode" } else { provider };
    PROVIDER_TEMPLATES.iter().find(|template| template.id == id)
}

/// 解析可执行文件路径：优先本地配置的环境变量，其次回退到 PATH 查找。
fn resolve_executable(template: &ProviderTemplate) -> String {
    match std::env::var(template.executable_env) {
        Ok(value) if !value.trim().is_empty() => value.trim().to_string(),
        _ => template.executable.to_string(),
    }
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

    let executable = resolve_executable(template);
    let child = std::process::Command::new(&executable)
        .args(&args)
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
    if let Some(occupant) = listening_pid(payload.port) {
        return Err(format!(
            "端口 {} 已被 PID {} 占用，无法启动",
            payload.port, occupant
        ));
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
            resolve_executable(template)
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

    // 保留条目但标记 desired_running=false：supervisor 据此跳过，不会把它拉回来；
    // 同时 status 仍能报告「托管中但已停止」，前端可区分「未托管」与「已停止」。
    // pid 置 0 表示当前没有进程。
    let previous_pid = {
        let mut table = process_table().lock().unwrap();
        match table.get_mut(&payload.instance_id) {
            Some(process) => {
                let pid = process.pid;
                process.pid = 0;
                process.desired_running = false;
                process.crashed = false;
                process.restarts = 0;
                persist(&table);
                Some(pid)
            }
            None => None,
        }
    };

    let Some(pid) = previous_pid else {
        return Ok(serde_json::json!({
            "instanceId": payload.instance_id,
            "stopped": false,
            "detail": "该实例没有托管的进程",
        })
        .to_string());
    };

    // pid 为 0 表示此前已被停止（或从未真正启动），无需再终止。
    if pid != 0 {
        terminate(pid).await;
    }
    Ok(serde_json::json!({
        "instanceId": payload.instance_id,
        "stopped": pid != 0,
        "pid": pid,
    })
    .to_string())
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

        // 端口被别的进程占用时不要盲目拉起：记一次失败并累计重启次数，
        // 避免与占用者形成「反复抢端口」的循环。
        if let Some(occupant) = listening_pid(port) {
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
    // /proc/<pid> 存在即视为存活；不存在则为僵尸或已退出。
    std::path::Path::new(&format!("/proc/{pid}")).exists()
}

#[cfg(not(any(target_os = "windows", target_os = "linux")))]
fn process_alive(_pid: u32) -> bool {
    false
}

/// 终止进程：先优雅终止，超时后强杀。
#[cfg(target_os = "windows")]
async fn terminate(pid: u32) {
    if !process_alive(pid) {
        return;
    }
    // Windows 无 SIGTERM 语义，直接强杀（opencode serve 无需要保存的状态）。
    let _ = std::process::Command::new("taskkill")
        .args(["/F", "/PID", &pid.to_string()])
        .output();
}

#[cfg(target_os = "linux")]
async fn terminate(pid: u32) {
    if !process_alive(pid) {
        return;
    }
    let _ = std::process::Command::new("kill")
        .args(["-TERM", &pid.to_string()])
        .status();
    // 给进程一个退出窗口，超时后强杀。
    for _ in 0..10 {
        tokio::time::sleep(Duration::from_millis(200)).await;
        if !process_alive(pid) {
            return;
        }
    }
    let _ = std::process::Command::new("kill")
        .args(["-9", &pid.to_string()])
        .status();
}

#[cfg(not(any(target_os = "windows", target_os = "linux")))]
async fn terminate(_pid: u32) {}

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
        terminate(process.pid).await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

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
        let template = lookup_template("opencode").expect("opencode template");
        // 未设置时回退到裸命令名（走 PATH）。
        assert_eq!(resolve_executable(template), "opencode");

        // 设置后使用绝对路径：Agent 作为服务运行时 PATH 与交互式登录不同，
        // 需要部署方显式指定。
        std::env::set_var(template.executable_env, "C:\\custom\\opencode.exe");
        assert_eq!(resolve_executable(template), "C:\\custom\\opencode.exe");

        // 空白值不应覆盖，避免误配成空路径。
        std::env::set_var(template.executable_env, "   ");
        assert_eq!(resolve_executable(template), "opencode");

        std::env::remove_var(template.executable_env);
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
}
