// AI Agent 管理模块的 Agent 侧支持：
//   - probe：探测本机某个 AI Agent 进程是否运行、端口是否监听（任务 56）
//   - bridge：反连云端数据通道，把本机 127.0.0.1:<port> 的字节双向搬运（任务 55）
//
// 数据通道是「一条通道对应一次 HTTP 请求」的字节管道；云端在此之上用
// http.Transport 说话，因此天然支持 SSE 流式响应，无需在本层解析 HTTP。

use bytes::Bytes;
use futures_util::{Sink, SinkExt, Stream, StreamExt};
use serde::Deserialize;
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio_tungstenite::{
    connect_async_with_config, tungstenite::protocol::Message, tungstenite::protocol::WebSocketConfig,
};

use crate::config::Config;

const PROBE_CONNECT_TIMEOUT: Duration = Duration::from_secs(3);
const BRIDGE_CONNECT_TIMEOUT: Duration = Duration::from_secs(8);
const BRIDGE_READ_BUF: usize = 32 * 1024;
// 与云端网关请求体上限（8MB）对齐的消息/帧上限。
const BRIDGE_MAX_MESSAGE: usize = 8 * 1024 * 1024;
// 与云端 gatewayStreamWriteTimeout(2 分钟) 对齐的单次写入上限。
const BRIDGE_WRITE_TIMEOUT: Duration = Duration::from_secs(2 * 60);
// 空闲上限：两侧长时间无数据时主动断开，避免桥接任务永不回收。
const BRIDGE_IDLE_TIMEOUT: Duration = Duration::from_secs(15 * 60);

#[derive(Deserialize, Debug, Clone)]
pub struct ProbePayload {
    #[serde(default)]
    pub provider: String,
    pub port: u16,
    #[serde(default)]
    pub process_match: Vec<String>,
}

#[derive(Deserialize, Debug, Clone)]
pub struct BridgePayload {
    pub port: u16,
    pub stream_id: String,
    pub stream_token: String,
}

/// 复用的本机 HTTP 客户端。
///
/// 必须是单例：reqwest::Client 自带连接池，每次新建意味着每条转发请求都要
/// 重新与 127.0.0.1 上的 opencode 建一次 TCP 连接，且旧客户端的池被丢弃。
/// 网关侧已经为「复用连接」做了大量工作，本机这一跳不该再退回每次新建。
///
/// 关闭自动解压：响应体由本端按 identity 读取后重新组装，若客户端透明解压，
/// Content-Length 会与实际 body 不一致，网关就无法判定响应边界。
fn local_http_client() -> &'static reqwest::Client {
    static CLIENT: std::sync::OnceLock<reqwest::Client> = std::sync::OnceLock::new();
    CLIENT.get_or_init(|| {
        reqwest::Client::builder()
            .timeout(Duration::from_secs(60))
            .no_gzip()
            .no_brotli()
            .no_deflate()
            .build()
            .unwrap_or_else(|_| reqwest::Client::new())
    })
}

/// 持久化的 sysinfo 实例：CPU 使用率是「距上次刷新的增量」，
/// 每次新建 System 做单次刷新会因为没有基线而恒返回 0。
/// 这里复用一个实例，使 CPU 有可比较的上一次采样。
fn resource_sampler() -> &'static std::sync::Mutex<sysinfo::System> {
    static SAMPLER: std::sync::OnceLock<std::sync::Mutex<sysinfo::System>> =
        std::sync::OnceLock::new();
    SAMPLER.get_or_init(|| std::sync::Mutex::new(sysinfo::System::new()))
}

/// 查询指定进程的资源占用（内存字节数 + CPU 百分比）。
///
/// 只刷新目标进程，不做全表扫描——探测是高频路径。
/// 查不到进程时返回 (0, 0.0)。
///
/// 注意：CPU 百分比是「距上一次刷新的增量」。探测由用户打开面板触发，
/// 间隔通常是秒级，远大于 sysinfo 的 MINIMUM_CPU_UPDATE_INTERVAL，
/// 因此无需额外节流；首次探测因无基线可能偏低，属预期行为。
pub(crate) fn process_resources(pid: u32) -> (u64, f32) {
    if pid == 0 {
        return (0, 0.0);
    }
    let target = sysinfo::Pid::from_u32(pid);
    let Ok(mut system) = resource_sampler().lock() else {
        return (0, 0.0);
    };
    system.refresh_processes(sysinfo::ProcessesToUpdate::Some(&[target]));
    let Some(process) = system.process(target) else {
        return (0, 0.0);
    };
    (process.memory(), process.cpu_usage())
}

/// 探测 AI Agent 运行时状态。返回 JSON 字符串供云端解析；即使未命中进程也返回
/// 同构的 JSON（processRunning=false + detail），避免云端按 successful 与否遇到两种载荷形态。
///
/// 除「进程是否运行」「端口是否监听」外，还返回**监听该端口的 PID**。
/// 云端据此做关联验证：进程命中与端口监听必须指向同一个进程，否则会把
/// 「A 进程占端口 + 系统里另有同名进程」误报为在线（见 ADR-0006 第 2 条）。
pub async fn probe(raw: &str) -> Result<String, String> {
    let payload: ProbePayload =
        serde_json::from_str(raw).map_err(|err| format!("探测参数无效: {}", err))?;

    let port_listening = tcp_listening(payload.port).await;
    let match_terms: Vec<String> = payload
        .process_match
        .iter()
        .map(|term| term.trim().to_lowercase())
        .filter(|term| !term.is_empty())
        .collect();

    // 监听 PID 查询、进程枚举与资源采集都是阻塞式系统调用，一起放到 blocking 线程池，
    // 避免占用 Tokio worker。
    let terms_for_pid = match_terms.clone();
    let port = payload.port;
    let (process_running, pid, detail, listener_pid, listener_matches, memory_bytes, cpu_percent) =
        tokio::task::spawn_blocking(move || {
            let listener_pid = listening_pid(port);
            let (process_running, pid, detail) = match_process(&terms_for_pid);
            // 关联验证：监听 PID 命中进程规则才算「该实例在跑」。
            let listener_matches = match listener_pid {
                Some(lpid) => process_name_matches(lpid, &terms_for_pid),
                None => false,
            };
            // 资源占用以「实际监听端口的进程」为准；没有监听者时退回进程匹配结果。
            let resource_pid = listener_pid.unwrap_or(pid);
            let (memory_bytes, cpu_percent) = process_resources(resource_pid);
            (
                process_running,
                pid,
                detail,
                listener_pid,
                listener_matches,
                memory_bytes,
                cpu_percent,
            )
        })
        .await
        .map_err(|err| format!("进程匹配任务失败: {}", err))?;

    let response = serde_json::json!({
        "provider": payload.provider,
        "port": payload.port,
        "processRunning": process_running,
        "portListening": port_listening,
        "pid": pid,
        "listenerPid": listener_pid,
        "listenerMatchesProcess": listener_matches,
        "memoryBytes": memory_bytes,
        "cpuPercent": cpu_percent,
        "detail": detail,
    });
    Ok(response.to_string())
}

/// 判断指定 PID 的进程名是否命中匹配规则（用于监听端口与进程的关联验证）。
pub(crate) fn process_name_matches(pid: u32, terms: &[String]) -> bool {
    if terms.is_empty() {
        return false;
    }
    let mut system = sysinfo::System::new();
    system.refresh_processes(sysinfo::ProcessesToUpdate::All);
    let name = match system.process(sysinfo::Pid::from_u32(pid)) {
        Some(process) => process.name().to_string_lossy().to_lowercase(),
        // Linux 上 sysinfo 可能读不到目标进程（权限/快照时机），
        // 直接看 /proc/<pid> 兜底，避免把「查不到」误判为「不匹配」。
        None => match comm_name(pid) {
            Some(name) => name,
            None => return false,
        },
    };
    let trimmed = name.strip_suffix(".exe").unwrap_or(name.as_str());
    if terms.iter().any(|term| trimmed == term.as_str()) {
        return true;
    }
    if terms.iter().any(|term| name.contains(term.as_str())) {
        return true;
    }
    // 名称未命中时再看命令行，与 match_process 的判定保持一致。
    // sysinfo 的 Process::cmd() 在某些平台/刷新组合下可能为空，
    // Linux 上额外读 /proc/<pid>/cmdline 兜底，避免把「查得到但 cmd 未填充」
    // 误判为「不匹配」。
    let cmd_hit = match system.process(sysinfo::Pid::from_u32(pid)) {
        Some(process) => process.cmd().iter().any(|part| {
            let value = part.to_string_lossy().to_lowercase();
            let value = value.strip_suffix(".exe").unwrap_or(&value);
            terms.iter().any(|term| value.contains(term.as_str()))
        }),
        None => false,
    };
    cmd_hit || cmdline_matches(pid, terms)
}

/// 判断 `pid` 的父进程链上是否存在 `ancestors` 中的任一 PID（含 pid 自身）。
///
/// 用于「残留子进程归属判定」：父进程已死、子进程仍持有端口时，只凭 PID 无法
/// 确认归属，但父链能追溯到本 Agent 记录的 spawn PID。带环与深度保护，避免
/// 异常进程表导致死循环。查不到父进程时保守返回 false（宁可漏清也不误杀）。
///
/// Unix 上父进程先死会使子进程被 reparent 到 init，父链随之断开。因此额外比对
/// 进程组：本 Agent spawn 时把子进程设为独立进程组（PGID = 子 PID），同一组的
/// 进程即视为归属本 Agent。
pub(crate) fn process_descends_from(pid: u32, ancestors: &[u32]) -> bool {
    if ancestors.is_empty() {
        return false;
    }
    if ancestors.contains(&pid) {
        return true;
    }
    if unix_same_process_group(pid, ancestors) {
        return true;
    }
    let mut system = sysinfo::System::new();
    system.refresh_processes(sysinfo::ProcessesToUpdate::All);

    let mut current = pid;
    let mut visited: Vec<u32> = vec![pid];
    // 深度上限：进程树异常深或存在环时及时退出。
    for _ in 0..64 {
        let Some(parent) = system
            .process(sysinfo::Pid::from_u32(current))
            .and_then(|process| process.parent())
        else {
            return false;
        };
        let parent = parent.as_u32();
        if parent == 0 || visited.contains(&parent) {
            return false;
        }
        if ancestors.contains(&parent) {
            return true;
        }
        visited.push(parent);
        current = parent;
    }
    false
}

/// Unix：判断 `pid` 的进程组 ID 是否等于 `ancestors` 中某个 PID
/// （spawn 时以子 PID 作为独立进程组 ID）。非 Unix 恒返回 false。
#[cfg(unix)]
fn unix_same_process_group(pid: u32, ancestors: &[u32]) -> bool {
    let Ok(pgid) = process_group_id(pid) else {
        return false;
    };
    pgid != 0 && ancestors.contains(&pgid)
}

#[cfg(not(unix))]
fn unix_same_process_group(_pid: u32, _ancestors: &[u32]) -> bool {
    false
}

#[cfg(target_os = "linux")]
pub(crate) fn process_group_id(pid: u32) -> Result<u32, ()> {
    // /proc/<pid>/stat 第 5 个字段是 pgrp；comm 可能含空格/括号，从最后一个 ')' 之后切。
    let stat = std::fs::read_to_string(format!("/proc/{pid}/stat")).map_err(|_| ())?;
    let rest = stat.rsplit_once(')').map(|(_, rest)| rest).ok_or(())?;
    let mut fields = rest.split_whitespace();
    // rest 首字段是 state，其后依次是 ppid、pgrp。
    fields.next();
    fields.next();
    fields
        .next()
        .and_then(|value| value.parse::<u32>().ok())
        .ok_or(())
}

#[cfg(all(unix, not(target_os = "linux")))]
fn process_group_id(_pid: u32) -> Result<u32, ()> {
    Err(())
}

/// Linux：读取 /proc/<pid>/comm（进程名，注意被内核截断到 15 字符）。
#[cfg(target_os = "linux")]
fn comm_name(pid: u32) -> Option<String> {
    std::fs::read_to_string(format!("/proc/{pid}/comm"))
        .ok()
        .map(|s| s.trim().to_lowercase())
}

#[cfg(not(target_os = "linux"))]
fn comm_name(_pid: u32) -> Option<String> {
    None
}

/// Linux：读取 /proc/<pid>/cmdline（NUL 分隔的 argv），逐段匹配。
#[cfg(target_os = "linux")]
fn cmdline_matches(pid: u32, terms: &[String]) -> bool {
    let Ok(raw) = std::fs::read(format!("/proc/{pid}/cmdline")) else {
        return false;
    };
    let lower = String::from_utf8_lossy(&raw).to_lowercase();
    lower.split('\0').any(|part| {
        let part = part.strip_suffix(".exe").unwrap_or(part);
        terms.iter().any(|term| part.contains(term.as_str()))
    })
}

#[cfg(not(target_os = "linux"))]
fn cmdline_matches(_pid: u32, _terms: &[String]) -> bool {
    false
}

/// 查询监听指定回环端口的进程 PID。
///
/// 只关心 `127.0.0.1:<port>` 上的 LISTEN：AI Agent 服务按约定绑定回环。
/// 查不到返回 None（端口未监听，或无权限查询）。
#[cfg(target_os = "windows")]
pub(crate) fn listening_pid(port: u16) -> Option<u32> {
    use windows_sys::Win32::NetworkManagement::IpHelper::{
        GetExtendedTcpTable, MIB_TCP6TABLE_OWNER_PID, MIB_TCP6ROW_OWNER_PID, MIB_TCPTABLE_OWNER_PID,
        MIB_TCPROW_OWNER_PID, TCP_TABLE_OWNER_PID_LISTENER,
    };
    use windows_sys::Win32::Networking::WinSock::{AF_INET, AF_INET6};

    const ERROR_INSUFFICIENT_BUFFER: u32 = 122;
    // MIB_TCP_STATE_LISTEN
    const TCP_STATE_LISTEN: u32 = 2;

    unsafe {
        // IPv4
        let mut size: u32 = 0;
        let mut ret = GetExtendedTcpTable(
            std::ptr::null_mut(),
            &mut size,
            0,
            AF_INET as u32,
            TCP_TABLE_OWNER_PID_LISTENER,
            0,
        );
        if ret == ERROR_INSUFFICIENT_BUFFER && size > 0 {
            let mut buf = vec![0u8; size as usize];
            ret = GetExtendedTcpTable(
                buf.as_mut_ptr() as *mut _,
                &mut size,
                0,
                AF_INET as u32,
                TCP_TABLE_OWNER_PID_LISTENER,
                0,
            );
            if ret == 0 {
                let table = buf.as_ptr() as *const MIB_TCPTABLE_OWNER_PID;
                let count = (*table).dwNumEntries as usize;
                let rows = std::ptr::addr_of!((*table).table) as *const MIB_TCPROW_OWNER_PID;
                for index in 0..count {
                    let row = &*rows.add(index);
                    if row.dwState != TCP_STATE_LISTEN {
                        continue;
                    }
                    // dwLocalPort 是网络字节序，低 16 位为端口。
                    let local_port = u16::from_be((row.dwLocalPort & 0xFFFF) as u16);
                    // dwLocalAddr 同为网络字节序；0x0100007F 即 127.0.0.1。
                    if local_port == port && u32::from_be(row.dwLocalAddr) == 0x7F00_0001 {
                        return Some(row.dwOwningPid);
                    }
                }
            }
        }

        // IPv6（回环 ::1）
        let mut size6: u32 = 0;
        let mut ret6 = GetExtendedTcpTable(
            std::ptr::null_mut(),
            &mut size6,
            0,
            AF_INET6 as u32,
            TCP_TABLE_OWNER_PID_LISTENER,
            0,
        );
        if ret6 == ERROR_INSUFFICIENT_BUFFER && size6 > 0 {
            let mut buf6 = vec![0u8; size6 as usize];
            ret6 = GetExtendedTcpTable(
                buf6.as_mut_ptr() as *mut _,
                &mut size6,
                0,
                AF_INET6 as u32,
                TCP_TABLE_OWNER_PID_LISTENER,
                0,
            );
            if ret6 == 0 {
                let table6 = buf6.as_ptr() as *const MIB_TCP6TABLE_OWNER_PID;
                let count6 = (*table6).dwNumEntries as usize;
                let rows6 = std::ptr::addr_of!((*table6).table) as *const MIB_TCP6ROW_OWNER_PID;
                for index in 0..count6 {
                    let row = &*rows6.add(index);
                    if row.dwState != TCP_STATE_LISTEN {
                        continue;
                    }
                    let local_port = u16::from_be((row.dwLocalPort & 0xFFFF) as u16);
                    // ::1 的 16 字节表示为前 15 字节全 0、最后一字节为 1。
                    let mut loopback = [0u8; 16];
                    loopback[15] = 1;
                    if local_port == port && row.ucLocalAddr == loopback {
                        return Some(row.dwOwningPid);
                    }
                }
            }
        }
    }
    None
}

/// Linux：从 /proc/net/tcp{,6} 找监听该端口的 socket inode，再反查持有它的 PID。
#[cfg(target_os = "linux")]
pub(crate) fn listening_pid(port: u16) -> Option<u32> {
    use std::collections::HashSet;

    let mut inodes: HashSet<String> = HashSet::new();
    for path in ["/proc/net/tcp", "/proc/net/tcp6"] {
        let Ok(content) = std::fs::read_to_string(path) else {
            continue;
        };
        for line in content.lines().skip(1) {
            let fields: Vec<&str> = line.split_whitespace().collect();
            // 字段：sl local_address rem_address st ... inode(9)
            if fields.len() < 10 {
                continue;
            }
            // st == 0A 表示 LISTEN
            if fields[3] != "0A" {
                continue;
            }
            let Some((_, port_hex)) = fields[1].split_once(':') else {
                continue;
            };
            let Ok(local_port) = u16::from_str_radix(port_hex, 16) else {
                continue;
            };
            if local_port == port {
                inodes.insert(fields[9].to_string());
            }
        }
    }
    if inodes.is_empty() {
        return None;
    }

    // 遍历 /proc/<pid>/fd 找持有该 socket inode 的进程。
    let Ok(entries) = std::fs::read_dir("/proc") else {
        return None;
    };
    for entry in entries.flatten() {
        let Some(pid) = entry.file_name().to_str().and_then(|n| n.parse::<u32>().ok()) else {
            continue;
        };
        let Ok(fds) = std::fs::read_dir(format!("/proc/{pid}/fd")) else {
            continue;
        };
        for fd in fds.flatten() {
            let Ok(target) = std::fs::read_link(fd.path()) else {
                continue;
            };
            let target = target.to_string_lossy();
            if let Some(inode) = target.strip_prefix("socket:[") {
                if let Some(inode) = inode.strip_suffix(']') {
                    if inodes.contains(inode) {
                        return Some(pid);
                    }
                }
            }
        }
    }
    None
}

#[cfg(not(any(target_os = "windows", target_os = "linux")))]
pub(crate) fn listening_pid(_port: u16) -> Option<u32> {
    None
}

async fn tcp_listening(port: u16) -> bool {
    let target = format!("127.0.0.1:{}", port);
    // 一次性 connect 判定端口是否监听：连接后立即丢弃，不做任何读写。
    matches!(
        tokio::time::timeout(PROBE_CONNECT_TIMEOUT, TcpStream::connect(&target)).await,
        Ok(Ok(_))
    )
}

/// 在系统进程列表中匹配进程名。返回（是否命中, pid, 说明）。
/// 优先精确匹配可执行文件名，其次子串匹配；避免多个候选时报告任意第一个。
fn match_process(terms: &[String]) -> (bool, u32, String) {
    if terms.is_empty() {
        return (false, 0, "未配置进程匹配规则".to_string());
    }
    let mut system = sysinfo::System::new();
    // 只刷新进程列表，避免整机采集开销。
    system.refresh_processes(sysinfo::ProcessesToUpdate::All);

    let mut exact: Option<(u32, String)> = None;
    let mut fuzzy: Option<(u32, String)> = None;
    for (pid, process) in system.processes() {
        let name = process.name().to_string_lossy().to_lowercase();
        // .exe 后缀只在这里剥离一次，避免在内层循环里反复 format。
        let trimmed = name.strip_suffix(".exe").unwrap_or(name.as_str());

        // 名称精确匹配优先级最高；命中即可结束扫描（exact.or(fuzzy) 保证精确优先）。
        if exact.is_none() && terms.iter().any(|term| trimmed == term.as_str()) {
            exact = Some((pid.as_u32(), name.clone()));
            break;
        }
        // 名称子串命中次之。
        if fuzzy.is_none() && terms.iter().any(|term| name.contains(term.as_str())) {
            fuzzy = Some((pid.as_u32(), name.clone()));
            continue;
        }
        // 名称未命中时再看命令行：逐段匹配 argv（含脚本名等中间参数），避免
        // 「python script.py」这类启动器进程漏判；逐段比较也不需拼接整串。
        if fuzzy.is_none() {
            let cmd_hit = process.cmd().iter().any(|part| {
                let value = part.to_string_lossy().to_lowercase();
                let value = value.strip_suffix(".exe").unwrap_or(&value);
                terms.iter().any(|term| value.contains(term.as_str()))
            });
            if cmd_hit {
                fuzzy = Some((pid.as_u32(), name.clone()));
            }
        }
    }
    if let Some((pid, name)) = exact.or(fuzzy) {
        return (true, pid, format!("匹配进程 {}", name));
    }
    (false, 0, "未找到匹配进程".to_string())
}

/// 从任务载荷中提取一次性 stream_token，供日志脱敏使用（解析失败时返回空串）。
pub fn extract_token(raw: &str) -> String {
    serde_json::from_str::<BridgePayload>(raw)
        .map(|payload| payload.stream_token)
        .unwrap_or_default()
}

/// 日志/回传前脱敏：优先按「已知的确切令牌值」替换，其次按 token= 键前缀兜底。
/// 直接替换确切值比按前缀推断更稳（不受 URL 编码、query 顺序、分隔符差异影响）。
pub fn redact_secrets(message: &str, secrets: &[&str]) -> String {
    let mut redacted = message.to_string();
    for secret in secrets {
        if !secret.is_empty() && redacted.contains(secret) {
            redacted = redacted.replace(secret, "<REDACTED>");
        }
    }
    // 兜底：仍带 token=/stream_token= 键前缀的值（例如来自其它错误格式）。
    loop {
        let mut replaced = false;
        for key in ["stream_token=", "token="] {
            let Some(index) = redacted.find(key) else {
                continue;
            };
            let value_start = index + key.len();
            // 扫描到下一个明确分隔符（&、#、空白或行尾）为止：令牌可能含 URL 编码
            // 产生的 % / + / 等字符，若按「非字母数字」提前截断会只替换掉一部分。
            let value_end = redacted[value_start..]
                .find(|ch: char| ch == '&' || ch == '#' || ch.is_whitespace())
                .map(|offset| value_start + offset)
                .unwrap_or(redacted.len());
            // 空值也要连同键一起替换，避免留下 token= 这种「存在凭据」的痕迹。
            redacted.replace_range(index..value_end, "<REDACTED>");
            replaced = true;
        }
        if !replaced {
            break;
        }
    }
    redacted
}

/// 反连云端数据通道并桥接本机端口。成功建立连接后进入双向字节搬运，直到任一端关闭。
pub async fn bridge(config: &Config, raw: &str) -> Result<(), String> {
    let payload: BridgePayload =
        serde_json::from_str(raw).map_err(|err| format!("数据通道参数无效: {}", err))?;

    // 端口来自云端经鉴权控制通道下发、并以一次性 token 绑定的任务；云端已把实例
    // 端口限制为 Provider 默认端口，这里不再重复维护端口白名单。
    let target = format!("127.0.0.1:{}", payload.port);

    // 先建立数据通道，再读首包判断请求类型：
    //   - 轻量消息请求：走投影循环，不占用本机 opencode 连接（投影时按需直连）。
    //   - 其余请求：连本机端口做裸字节透传。
    // 若两条路径都在建连时就并发打开本机端口，轻量隧道会白白占用一条 opencode
    // 连接长达空闲超时，故按类型延迟建立本机连接。
    let ws_fut = async {
        let url = agent_port_stream_url(config, &payload.stream_id, &payload.stream_token)?;
        // 帧/消息上限与云端网关的请求体上限（8MB）对齐，约束最坏情况下的内存分配。
        let mut ws_config = WebSocketConfig::default();
        ws_config.max_frame_size = Some(BRIDGE_MAX_MESSAGE);
        ws_config.max_message_size = Some(BRIDGE_MAX_MESSAGE);
        // 第三个参数是 disable_nagle（关闭 Nagle 以降低流式延迟），不是跳过证书校验：
        // TLS 校验沿用 tokio-tungstenite 的 rustls + webpki-roots 默认行为，与主控制连接一致。
        let disable_nagle = true;
        tokio::time::timeout(
            Duration::from_secs(12),
            connect_async_with_config(url, Some(ws_config), disable_nagle),
        )
        .await
        .map_err(|_| "数据通道连接超时".to_string())?
        .map_err(|err| format!("数据通道连接失败: {}", err))
        .map(|(ws_stream, _)| ws_stream)
    };
    let mut ws_stream = ws_fut.await?;

    // 隧道分流：网关对**非流式**请求启用了连接复用（keep-alive），因此本端不能
    // 处理一次就关隧道，而要循环处理同一隧道上的后续请求，直到空闲超时或对端
    // 关闭。每条请求由网关的 http.Transport 在同一 net.Conn 上串行发出
    // （HTTP/1.1 无流水线），故按「读请求头 → 回响应」的顺序处理即可。
    //
    // 1) 轻量消息投影请求（X-Lightweight + GET /session/{id}/message）：不走
    //    裸字节透传，而由本机直连 opencode、裁剪响应后再回传。
    // 2) 其它非流式请求：通用 keep-alive 隧道，透传转发。
    // 3) 流式请求（SSE / WebSocket）：保持原有裸字节管道，独占一条通道 ——
    //    那类响应语义是「读到关闭为止」，与复用连接冲突。
    // 复用连接上的「首包」可能不会立刻到达：网关的 http.Transport 会按需预建
    // 连接并放进池里，随后才写请求。若这里只等 BRIDGE_CONNECT_TIMEOUT（8s），
    // 那些还没被使用的池中连接会超时退出，导致网关把该连接判死、退回裸透传路径
    // —— 表现为「并发一上来消息投影就永久失效」。
    //
    // 因此首包等待用空闲超时（15 分钟）：连接建好后长时间没请求是复用池的正常
    // 状态，由空闲超时统一回收，而不是按「建连超时」判定失败。
    let first_head = match tokio::time::timeout(BRIDGE_IDLE_TIMEOUT, read_until_http_head(&mut ws_stream)).await {
        Ok(Ok(head)) => head,
        Ok(Err(err)) => return Err(err),
        Err(_) => return Err("读取请求头超时".to_string()),
    };

    if parse_lightweight_request(&first_head).is_some() {
        return serve_lightweight_tunnel(payload.port, first_head, &mut ws_stream).await;
    }

    if parse_keepalive_request(&first_head).is_some() {
        return serve_keepalive_tunnel(payload.port, first_head, &mut ws_stream).await;
    }

    // 流式：连本机端口，首包原样透传，进入原有裸字节搬运循环。
    let mut local = tokio::time::timeout(BRIDGE_CONNECT_TIMEOUT, TcpStream::connect(&target))
        .await
        .map_err(|_| format!("连接 {} 超时", target))?
        .map_err(|err| format!("连接 {} 失败: {}", target, err))?;
    if let Err(err) = local.write_all(&first_head).await {
        return Err(format!("本地端口写入失败: {}", err));
    }
    let _ = local.flush().await;

    let mut read_buf = vec![0u8; BRIDGE_READ_BUF];
    // 单循环同时持有两侧，任一侧结束即可优雅收尾：向对端发送 Close 帧、
    // 关闭本地写端，然后退出。用一个可刷新的空闲截止时间包住「取数据 + 发送」
    // 整段逻辑，因此即使发送端被对端背压卡住，超时同样生效，任务不会永不回收。
    let mut idle_deadline = tokio::time::Instant::now() + BRIDGE_IDLE_TIMEOUT;
    // close 是否已在循环内发送过（救援路径），避免收尾时重复发 Close。
    let mut close_sent = false;
    let outcome: Result<(), String> = loop {
        let step = async {
            tokio::select! {
                read = local.read(&mut read_buf) => {
                    match read {
                        Ok(0) => Ok(Step::Finished),
                        Ok(read) => {
                            // 每个分片复制一次到 Bytes 交给 tungstenite；分片上限 32KB，
                            // 单次分配成本可接受，换取读缓冲可安全复用。
                            ws_stream
                                .send(Message::Binary(Bytes::copy_from_slice(&read_buf[..read])))
                                .await
                                .map_err(|_| "数据通道发送失败".to_string())
                                .map(|_| Step::Data)
                        }
                        Err(err) => Err(format!("本地端口读取失败: {}", err)),
                    }
                }
                inbound = ws_stream.next() => {
                    match inbound {
                        Some(Ok(Message::Binary(data))) => {
                            // 单次写本地端口也要有独立上限：慢/卡死的本地消费者不应把
                            // 整个 step 拖到空闲超时（15 分钟），与云端写截止对齐。
                            match tokio::time::timeout(BRIDGE_WRITE_TIMEOUT, local.write_all(&data)).await {
                                Ok(Ok(())) => {
                                    let _ = local.flush().await;
                                    Ok(Step::Data)
                                }
                                Ok(Err(err)) => Err(format!("本地端口写入失败: {}", err)),
                                Err(_) => Err("本地端口写入超时".to_string()),
                            }
                        }
                        // 文本帧同样承载隧道字节（RFC 6455），按字节转发而不是丢弃。
                        Some(Ok(Message::Text(text))) => {
                            match tokio::time::timeout(BRIDGE_WRITE_TIMEOUT, local.write_all(text.as_bytes())).await {
                                Ok(Ok(())) => {
                                    let _ = local.flush().await;
                                    Ok(Step::Data)
                                }
                                Ok(Err(err)) => Err(format!("本地端口写入失败: {}", err)),
                                Err(_) => Err("本地端口写入超时".to_string()),
                            }
                        }
                        // tokio-tungstenite 不自动回 Ping：显式回 Pong，避免云端
                        // 的保活探测因未响应而判定连接失效。
                        Some(Ok(Message::Ping(payload))) => {
                            let _ = ws_stream.send(Message::Pong(payload)).await;
                            Ok(Step::Active)
                        }
                        Some(Ok(Message::Close(_))) | None => Ok(Step::Finished),
                        Some(Err(err)) => Err(format!("数据通道接收失败: {}", err)),
                        _ => Ok(Step::Active),
                    }
                }
            }
        };

        match tokio::time::timeout_at(idle_deadline, step).await {
            Err(_) => {
                // 空闲（或发送被背压卡住）超过上限：先关本地写端（有限等待），
                // 再限时发送非正常关闭码，使云端把这次中断识别为异常而不是干净结束。
                let _ = tokio::time::timeout(Duration::from_secs(2), local.shutdown()).await;
                let _ = tokio::time::timeout(
                    Duration::from_secs(5),
                    ws_stream.send(Message::Close(Some(tokio_tungstenite::tungstenite::protocol::CloseFrame {
                        code: tokio_tungstenite::tungstenite::protocol::frame::coding::CloseCode::Policy,
                        reason: "idle timeout".into(),
                    }))),
                )
                .await;
                close_sent = true;
                // 以 Err 返回，让上层日志把这次中断记为失败（而非正常结束）。
                break Err("数据通道空闲超时，已中断桥接".to_string());
            }
            Ok(Ok(Step::Finished)) => break Ok(()),
            Ok(Ok(Step::Data)) => {
                // 数据字节流动：刷新空闲截止时间。单步内的读/写各自有 32KB 上限，
                // 因此一次「已完成的数据步」本身就证明对端在持续推进。
                idle_deadline = tokio::time::Instant::now() + BRIDGE_IDLE_TIMEOUT;
            }
            Ok(Ok(Step::Active)) => {}
            Ok(Err(err)) => break Err(err),
        }
    };

    // 收尾同样限时：对端卡死时不应让任务无法回收。救援路径已发过 Close 则不重复发。
    // 出错结束时发非正常关闭码，使云端把中断识别为异常；正常结束才发干净 Close。
    if !close_sent {
        let close_frame = if outcome.is_ok() {
            None
        } else {
            Some(tokio_tungstenite::tungstenite::protocol::CloseFrame {
                code: tokio_tungstenite::tungstenite::protocol::frame::coding::CloseCode::Policy,
                reason: "bridge error".into(),
            })
        };
        let _ = tokio::time::timeout(Duration::from_secs(5), ws_stream.close(close_frame)).await;
    }
    let _ = local.shutdown().await;
    outcome
}

enum Step {
    /// 仅控制帧（Ping/Pong 等）流动：不刷新空闲截止时间。
    Active,
    /// 实际数据字节流动：刷新空闲截止时间。
    Data,
    Finished,
}

/// 轻量请求的请求头上限：正常 HTTP 请求头只有几 KB，超过即视为异常。
const HTTP_HEAD_MAX: usize = 64 * 1024;

/// 复用隧道请求体上限。opencode 的请求体是 JSON（prompt / 配置等），远小于此值；
/// 设上限是为了防止伪造的超大 Content-Length 让本端无限缓冲。
const KEEPALIVE_BODY_MAX: usize = 32 * 1024 * 1024;

/// 从数据通道读到 HTTP 请求头结束（\r\n\r\n）。返回的字节包含请求头，不含 body。
/// body 由调用方按 Content-Length 继续从同一流读取（轻量请求为 GET，无 body）。
type AgentWsStream =
    tokio_tungstenite::WebSocketStream<tokio_tungstenite::MaybeTlsStream<tokio::net::TcpStream>>;

/// 把一段字节按数据通道的单帧上限分片发送。
///
/// 网关对数据通道设置了读上限（64KB），单帧超过即被拒并使隧道断开。原透传路径
/// 天然按 32KB 读缓冲分片；轻量响应是一次性组装的（可能上百 KB），必须在此显式
/// 分片，否则大会话的轻量响应会 502。
async fn send_framed(stream: &mut AgentWsStream, payload: &[u8]) -> Result<(), String> {
    for chunk in payload.chunks(BRIDGE_READ_BUF) {
        stream
            .send(Message::Binary(Bytes::copy_from_slice(chunk)))
            .await
            .map_err(|_| "数据通道发送失败".to_string())?;
    }
    Ok(())
}

async fn read_until_http_head(stream: &mut AgentWsStream) -> Result<Vec<u8>, String> {
    let mut buf: Vec<u8> = Vec::with_capacity(1024);
    loop {
        let next = stream
            .next()
            .await
            .ok_or_else(|| "数据通道在请求头到达前关闭".to_string())?;
        match next {
            Ok(Message::Binary(data)) => {
                buf.extend_from_slice(&data);
            }
            Ok(Message::Text(text)) => {
                buf.extend_from_slice(text.as_bytes());
            }
            Ok(Message::Ping(payload)) => {
                let _ = stream.send(Message::Pong(payload)).await;
                continue;
            }
            Ok(Message::Close(_)) | Err(_) => {
                return Err("数据通道在请求头到达前关闭".to_string());
            }
            _ => continue,
        }
        if buf.windows(4).any(|w| w == b"\r\n\r\n") {
            return Ok(buf);
        }
        if buf.len() > HTTP_HEAD_MAX {
            return Err("请求头超过上限".to_string());
        }
    }
}

/// 轻量隧道服务循环：在一条复用的数据通道上，按「读请求头 → 投影 → 回响应」
/// 处理多条请求，直到空闲超时或对端关闭。首包 head 已由调用方读入。
///
/// **同一连接上会混有非轻量请求**：网关的 http.Transport 按 host 复用连接，
/// 同一实例的所有请求共用同一个池，因此一条连接既可能承载
/// `X-Lightweight` 的消息请求，也可能承载 `/permission`、`/vcs` 等普通请求。
/// 遇到非轻量请求必须就地透传，而不能回 400 并关隧道 —— 那会让网关把这条
/// 连接判死并退回裸透传路径，表现为「消息投影时好时坏，并发一上来就永久失效」。
async fn serve_lightweight_tunnel(
    port: u16,
    first_head: Vec<u8>,
    ws_stream: &mut AgentWsStream,
) -> Result<(), String> {
    let mut head = first_head;
    loop {
        match parse_lightweight_request(&head) {
            Some(request) => {
                match handle_lightweight_request(port, &request.path).await {
                    Ok(response) => {
                        if send_framed(ws_stream, &response).await.is_err() {
                            return Ok(());
                        }
                    }
                    Err(err) => {
                        // 投影失败：回 502，由客户端按错误处理（不会误当成功响应）。
                        eprintln!("[aiagent] lightweight projection failed: {}", err);
                        let body = format!("{{\"error\":\"projection failed: {}\"}}", err.replace('"', "'"));
                        let response = build_http_response(502, "application/json", body.as_bytes(), &[]);
                        let _ = send_framed(ws_stream, &response).await;
                    }
                }
            }
            // 非轻量请求（普通 GET/POST）：就地透传，保持隧道存活。
            // 不能因为「这条隧道是为轻量开的」就拒绝它 —— 网关按 host 复用连接，
            // 混流是常态。
            None => match parse_keepalive_request(&head) {
                Some(request) => {
                    let body = match read_request_body(&head, request.content_length, ws_stream).await {
                        Ok(body) => body,
                        Err(err) => {
                            eprintln!("[aiagent] tunnel body read failed: {}", err);
                            let response =
                                build_http_response(400, "application/json", b"{\"error\":\"bad request body\"}", &[]);
                            let _ = send_framed(ws_stream, &response).await;
                            return Ok(());
                        }
                    };
                    match handle_keepalive_request(port, &request, &body).await {
                        Ok(response) => {
                            if send_framed(ws_stream, &response).await.is_err() {
                                return Ok(());
                            }
                        }
                        Err(err) => {
                            eprintln!("[aiagent] tunnel forward failed: {}", err);
                            let body = format!("{{\"error\":\"forward failed: {}\"}}", err.replace('"', "'"));
                            let response = build_http_response(502, "application/json", body.as_bytes(), &[]);
                            let _ = send_framed(ws_stream, &response).await;
                        }
                    }
                }
                // 流式请求出现在复用隧道上（网关已按 Accept 分流，不应发生）：
                // 回 400 并结束，避免把长连接语义混进复用连接。
                None => {
                    let response = build_http_response(400, "application/json", b"{\"error\":\"bad request\"}", &[]);
                    let _ = send_framed(ws_stream, &response).await;
                    return Ok(());
                }
            },
        }

        // 读下一条请求头；空闲超时或对端关闭即结束整条隧道。
        match tokio::time::timeout(BRIDGE_IDLE_TIMEOUT, read_until_http_head(ws_stream)).await {
            Ok(Ok(next_head)) => head = next_head,
            Ok(Err(_)) | Err(_) => {
                let _ = ws_stream.close(None).await;
                return Ok(());
            }
        }
    }
}

struct LightweightRequest {
    path: String,
}

/// 判断请求是否为「轻量消息列表」请求：GET + 带 X-Lightweight 头 +
/// 路径形如 /session/{id}/message。不匹配返回 None，走普通透传。
fn parse_lightweight_request(head: &[u8]) -> Option<LightweightRequest> {
    let text = std::str::from_utf8(head).ok()?;
    let mut lines = text.split("\r\n");
    let request_line = lines.next()?;
    let mut request_parts = request_line.split_whitespace();
    let method = request_parts.next()?;
    let target = request_parts.next()?;
    if !method.eq_ignore_ascii_case("GET") {
        return None;
    }

    let mut lightweight = false;
    for line in lines {
        if line.is_empty() {
            break;
        }
        if let Some((name, value)) = line.split_once(':') {
            if name.trim().eq_ignore_ascii_case("x-lightweight") {
                let v = value.trim();
                lightweight = v == "1" || v.eq_ignore_ascii_case("true");
            }
        }
    }
    if !lightweight {
        return None;
    }

    // 路径形如 /session/<id>/message（可带 query）。
    let path_only = target.split('?').next().unwrap_or(target);
    let segments: Vec<&str> = path_only.trim_start_matches('/').split('/').collect();
    if segments.len() != 3 || segments[0] != "session" || segments[2] != "message" {
        return None;
    }
    Some(LightweightRequest {
        path: target.to_string(),
    })
}

/// 解析出「可复用隧道」请求：非流式的普通 HTTP 请求。
///
/// 网关对非流式请求启用连接复用，因此本端不能处理一次就关隧道，而要循环处理
/// 同一隧道上的后续请求（HTTP/1.1 无流水线，串行处理即可）。
///
/// 流式请求（Accept: text/event-stream / Upgrade: websocket）返回 None，继续走
/// 原有裸字节管道 —— 那类响应是长连接，必须独占一条通道。
fn parse_keepalive_request(head: &[u8]) -> Option<KeepAliveRequest> {
    let text = std::str::from_utf8(head).ok()?;
    let mut lines = text.split("\r\n");
    let request_line = lines.next()?;
    let mut request_parts = request_line.split_whitespace();
    let method = request_parts.next()?;
    let target = request_parts.next()?;
    if method.is_empty() || target.is_empty() {
        return None;
    }

    let mut accept = String::new();
    let mut upgrade = String::new();
    let mut content_length: usize = 0;
    for line in lines {
        if line.is_empty() {
            break;
        }
        if let Some((name, value)) = line.split_once(':') {
            let name = name.trim();
            let value = value.trim();
            if name.eq_ignore_ascii_case("accept") {
                accept = value.to_ascii_lowercase();
            } else if name.eq_ignore_ascii_case("upgrade") {
                upgrade = value.to_ascii_lowercase();
            } else if name.eq_ignore_ascii_case("content-length") {
                // 请求体长度：keep-alive 隧道的 body 必须按此从同一流读出后转发，
                // 否则 POST 会以空 body 发出，且残留字节会被当成下一条请求头。
                content_length = value.parse::<usize>().unwrap_or(0);
            }
        }
    }
    if accept.contains("text/event-stream") || upgrade.contains("websocket") {
        return None;
    }

    Some(KeepAliveRequest {
        method: method.to_string(),
        path: target.to_string(),
        content_length,
    })
}

/// 可复用隧道上的单条请求：方法 + 路径（含 query）+ 待读取的请求体长度。
struct KeepAliveRequest {
    method: String,
    path: String,
    content_length: usize,
}

/// 可复用隧道服务循环：在一条复用的数据通道上，按「读请求头 → 本地请求 →
/// 回响应」处理多条请求，直到空闲超时或对端关闭。首包 head 已由调用方读入。
///
/// 与轻量隧道的区别：这里做通用透传（不投影），并保留本机 opencode 的
/// status / content-type / 其它响应头；同时强制 Connection: keep-alive 与精确
/// Content-Length，网关的 http.Transport 据此判定响应边界后继续复用连接。
async fn serve_keepalive_tunnel(
    port: u16,
    first_head: Vec<u8>,
    ws_stream: &mut AgentWsStream,
) -> Result<(), String> {
    let mut head = first_head;
    loop {
        let request = match parse_keepalive_request(&head) {
            Some(request) => request,
            // 复用隧道上出现流式请求（不应发生，网关已分流）：回 400，避免静默挂起。
            None => {
                let response = build_http_response(400, "application/json", b"{\"error\":\"bad request\"}", &[]);
                let _ = send_framed(ws_stream, &response).await;
                return Ok(());
            }
        };

        // 请求体必须按 Content-Length 从同一流读出后一并转发：漏读会让 POST 以
        // 空 body 发出，且残留字节会被下一条 read_until_http_head 当成请求头，
        // 造成整条复用隧道协议错位。
        let body = match read_request_body(&head, request.content_length, ws_stream).await {
            Ok(body) => body,
            Err(err) => {
                eprintln!("[aiagent] keepalive body read failed: {}", err);
                let response = build_http_response(400, "application/json", b"{\"error\":\"bad request body\"}", &[]);
                let _ = send_framed(ws_stream, &response).await;
                return Ok(());
            }
        };

        match handle_keepalive_request(port, &request, &body).await {
            Ok(response) => {
                if send_framed(ws_stream, &response).await.is_err() {
                    return Ok(());
                }
            }
            Err(err) => {
                // 转发失败：回 502，由客户端按错误处理（不会误当成功响应）。
                eprintln!("[aiagent] keepalive forward failed: {}", err);
                let body = format!("{{\"error\":\"forward failed: {}\"}}", err.replace('"', "'"));
                let response = build_http_response(502, "application/json", body.as_bytes(), &[]);
                let _ = send_framed(ws_stream, &response).await;
            }
        }

        // 读下一条请求头；空闲超时或对端关闭即结束整条隧道。
        match tokio::time::timeout(BRIDGE_IDLE_TIMEOUT, read_until_http_head(ws_stream)).await {
            Ok(Ok(next_head)) => head = next_head,
            Ok(Err(_)) | Err(_) => {
                let _ = ws_stream.close(None).await;
                return Ok(());
            }
        }
    }
}

/// 从首包中切出请求头与已到达的 body 前缀，并按 Content-Length 读满剩余部分。
///
/// `read_until_http_head` 返回的缓冲区以 `\r\n\r\n` 结尾判断「头已读完」，但同一
/// 帧里可能已经捎带了部分（甚至全部）body，因此不能丢弃分隔符之后的字节。
///
/// 泛型化流参数：生产用 `AgentWsStream`，测试可用内存流直接覆盖「首包带 body 前缀」
/// 与「body 跨帧补齐」这两条真实路径。
async fn read_request_body<S>(
    head: &[u8],
    content_length: usize,
    stream: &mut S,
) -> Result<Vec<u8>, String>
where
    S: Stream<Item = Result<Message, tokio_tungstenite::tungstenite::Error>> + Sink<Message> + Unpin,
    S::Error: std::fmt::Debug,
{
    if content_length == 0 {
        return Ok(Vec::new());
    }
    if content_length > KEEPALIVE_BODY_MAX {
        return Err(format!("请求体超过上限: {} 字节", content_length));
    }

    // 定位头/体分隔符，保留其后的字节作为 body 前缀。
    let mut body: Vec<u8> = Vec::with_capacity(content_length.min(64 * 1024));
    if let Some(index) = head.windows(4).position(|w| w == b"\r\n\r\n") {
        body.extend_from_slice(&head[index + 4..]);
    }
    if body.len() >= content_length {
        // 首包已含完整 body；多出的字节属于下一条请求（HTTP/1.1 无流水线，正常
        // 情况下不会发生），截断而不报错，避免把合法请求判成错误。
        body.truncate(content_length);
        return Ok(body);
    }

    while body.len() < content_length {
        let next = tokio::time::timeout(BRIDGE_IDLE_TIMEOUT, stream.next())
            .await
            .map_err(|_| "读取请求体超时".to_string())?
            .ok_or_else(|| "数据通道在请求体读完前关闭".to_string())?;
        match next {
            Ok(Message::Binary(data)) => body.extend_from_slice(&data),
            Ok(Message::Text(text)) => body.extend_from_slice(text.as_bytes()),
            Ok(Message::Ping(payload)) => {
                let _ = stream.send(Message::Pong(payload)).await;
                continue;
            }
            Ok(Message::Close(_)) | Err(_) => {
                return Err("数据通道在请求体读完前关闭".to_string());
            }
            _ => continue,
        }
    }
    body.truncate(content_length);
    Ok(body)
}

/// 把单条请求转发到本机 opencode，组装成 HTTP/1.1 响应字节。
///
/// 保留状态码、Content-Type 与响应体；其余响应头不透传（Content-Length 由本端
/// 按实际 body 重算，避免上游分块编码/长度不一致导致网关无法判定响应边界）。
///
/// `body` 为已按 Content-Length 读满的请求体（GET 等无体请求为空）。同时透传
/// Content-Type 之外的体语义不需要：opencode 以 JSON 为主，统一按 JSON 发送。
async fn handle_keepalive_request(
    port: u16,
    request: &KeepAliveRequest,
    body: &[u8],
) -> Result<Vec<u8>, String> {
    let url = format!("http://127.0.0.1:{}{}", port, request.path);
    let client = local_http_client();

    let method = reqwest::Method::from_bytes(request.method.as_bytes())
        .map_err(|err| format!("非法请求方法 {}: {}", request.method, err))?;

    let mut builder = client
        .request(method, &url)
        .header(reqwest::header::ACCEPT_ENCODING, "identity");
    if !body.is_empty() {
        builder = builder
            .header(reqwest::header::CONTENT_TYPE, "application/json")
            .body(body.to_vec());
    }

    let resp = builder
        .send()
        .await
        .map_err(|err| format!("请求本机 opencode 失败: {}", err))?;

    let status = resp.status();
    let content_type = resp
        .headers()
        .get(reqwest::header::CONTENT_TYPE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or("application/json")
        .to_string();
    let body = resp
        .bytes()
        .await
        .map_err(|err| format!("读取本机响应失败: {}", err))?;

    Ok(build_http_response(status.as_u16(), &content_type, &body, &[]))
}

/// 直连本机 opencode 拉取完整消息，投影后组装成 HTTP/1.1 响应字节。
/// 请求走 identity 编码（不压缩），响应体为投影后的 JSON。
async fn handle_lightweight_request(port: u16, path: &str) -> Result<Vec<u8>, String> {
    let url = format!("http://127.0.0.1:{}{}", port, path);
    let client = local_http_client();

    let resp = client
        .get(&url)
        .header(reqwest::header::ACCEPT_ENCODING, "identity")
        .send()
        .await
        .map_err(|err| format!("请求本机 opencode 失败: {}", err))?;

    let status = resp.status();
    let body = resp
        .bytes()
        .await
        .map_err(|err| format!("读取本机响应失败: {}", err))?;

    if !status.is_success() {
        // 非 2xx：原样回传状态与 body，让客户端按原有错误路径处理。
        return Ok(build_http_response(
            status.as_u16(),
            "application/json",
            &body,
            &[],
        ));
    }

    let projected = crate::aiagent_projection::project_message_list(&body)?;
    Ok(build_http_response(
        200,
        "application/json",
        &projected,
        &[("X-Lightweight", "1")],
    ))
}

/// 组装一个最小 HTTP/1.1 响应：keep-alive + 正确 Content-Length。
///
/// 必须 keep-alive：网关对该隧道启用了连接复用，响应里若写 Connection: close，
/// 网关的 http.Transport 会在每次响应后关闭连接，复用即失效。Content-Length 精确，
/// 网关据此判定响应边界，无需关闭连接即可继续复用。
fn build_http_response(status: u16, content_type: &str, body: &[u8], extra: &[(&str, &str)]) -> Vec<u8> {
    let reason = if status == 200 { "OK" } else { "Error" };
    let mut head = format!(
        "HTTP/1.1 {} {}\r\nContent-Type: {}\r\nContent-Length: {}\r\nConnection: keep-alive\r\n",
        status,
        reason,
        content_type,
        body.len()
    );
    for (name, value) in extra {
        head.push_str(name);
        head.push_str(": ");
        head.push_str(value);
        head.push_str("\r\n");
    }
    head.push_str("\r\n");

    let mut out = Vec::with_capacity(head.len() + body.len());
    out.extend_from_slice(head.as_bytes());
    out.extend_from_slice(body);
    out
}

fn agent_port_stream_url(
    config: &Config,
    stream_id: &str,
    stream_token: &str,
) -> Result<String, String> {
    let mut url =
        url::Url::parse(&config.server_url).map_err(|err| format!("服务端 URL 无效: {}", err))?;
    let scheme = match url.scheme() {
        "https" => "wss",
        "http" => "ws",
        other => return Err(format!("不支持的服务端 URL 协议: {}", other)),
    };
    url.set_scheme(scheme)
        .map_err(|_| "设置数据通道 WebSocket 协议失败".to_string())?;
    url.set_path("/ws/agent-port");
    url.query_pairs_mut()
        .clear()
        .append_pair("server_id", &config.server_id)
        .append_pair("stream_id", stream_id)
        .append_pair("token", stream_token);
    Ok(url.to_string())
}

#[cfg(test)]
mod tests {
    use super::{
        build_http_response, extract_token, handle_keepalive_request, listening_pid, parse_keepalive_request,
        parse_lightweight_request, probe, process_resources, read_request_body, redact_secrets,
        KEEPALIVE_BODY_MAX,
    };
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;
    use std::time::Duration;

    use futures_util::{Sink, Stream};
    use tokio_tungstenite::tungstenite::protocol::Message;

    // ── keep-alive 分流：非流式走复用隧道，流式继续独占通道 ──

    #[test]
    fn keepalive_matches_plain_get_requests() {
        let head = b"GET /mcp?directory=D%3A%2Ftmp HTTP/1.1\r\nHost: 127.0.0.1\r\nAccept: */*\r\n\r\n";
        let req = parse_keepalive_request(head).expect("普通 GET 应命中复用隧道");
        assert_eq!(req.method, "GET");
        assert_eq!(req.path, "/mcp?directory=D%3A%2Ftmp");
    }

    #[test]
    fn keepalive_matches_post_requests() {
        let head = b"POST /session/ses_1/prompt HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\n\r\n";
        let req = parse_keepalive_request(head).expect("POST 应命中复用隧道");
        assert_eq!(req.method, "POST");
    }

    /// 用「网关 Go http.Transport 实际发出的报文字节」验证轻量路径能被命中。
    ///
    /// 与 matches_lightweight_message_request 的差别：这里带 Host / User-Agent /
    /// Accept-Encoding，是真实传输形态。若解析逻辑对额外头敏感，会在此暴露。
    #[test]
    fn matches_lightweight_request_with_real_gateway_headers() {
        let head = b"GET /session/ses_f025d74e3ffeAFiATAbOPjoivR/message?limit=1 HTTP/1.1\r\n\
Host: 127.0.0.1:0\r\nUser-Agent: Go-http-client/1.1\r\nX-Lightweight: 1\r\nAccept-Encoding: gzip\r\n\r\n";
        let r = parse_lightweight_request(head);
        assert!(r.is_some(), "网关真实形态的请求头应命中轻量路径");
        assert_eq!(
            r.unwrap().path,
            "/session/ses_f025d74e3ffeAFiATAbOPjoivR/message?limit=1"
        );
    }

    /// SSE 是长连接，必须继续走独占通道；若误入复用隧道，复用后的连接会把
    /// 流式响应的边界搞乱。
    #[test]
    fn keepalive_rejects_sse_requests() {
        let head = b"GET /global/event HTTP/1.1\r\nHost: x\r\nAccept: text/event-stream\r\n\r\n";
        assert!(parse_keepalive_request(head).is_none());
    }

    #[test]
    fn keepalive_rejects_accept_event_stream_with_other_types() {
        // 真实浏览器会发 `Accept: text/event-stream, application/json` 这类复合值
        let head = b"GET /global/event HTTP/1.1\r\nHost: x\r\nAccept: text/event-stream, application/json\r\n\r\n";
        assert!(parse_keepalive_request(head).is_none());
    }

    #[test]
    fn keepalive_rejects_websocket_upgrade() {
        let head = b"GET /pty/1 HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n";
        assert!(parse_keepalive_request(head).is_none());
    }

    #[test]
    fn keepalive_is_case_insensitive_on_headers() {
        let head = b"GET /mcp HTTP/1.1\r\nHost: x\r\nACCEPT: TEXT/EVENT-STREAM\r\n\r\n";
        assert!(parse_keepalive_request(head).is_none());
    }

    /// 端到端：起一个假 opencode，把「两条请求 + 一条流式请求」写进同一对
    /// 内存流，验证复用隧道循环能连续处理多条并把流式的分流回 None。
    ///
    /// 这是本次改动最需要锁住的行为：网关会复用同一条隧道连续发多个非流式
    /// 请求，Agent 若只处理第一条就退出，复用会拿到已关闭的连接 —— 表现为
    /// 「打开会话时大量请求失败/挂起」。
    #[tokio::test]
    async fn keepalive_serves_multiple_requests_on_one_tunnel() {
        // 假 opencode：按 path 回不同 body
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();

        let upstream = tokio::spawn(async move {
            let mut seen = Vec::new();
            for _ in 0..2 {
                let (mut socket, _) = listener.accept().await.unwrap();
                let mut buf = vec![0u8; 4096];
                let n = socket.read(&mut buf).await.unwrap();
                let head = String::from_utf8_lossy(&buf[..n]).to_string();
                let path = head.split_whitespace().nth(1).unwrap_or("/").to_string();
                seen.push(path.clone());

                let body = format!("{{\"path\":\"{}\"}}", path);
                let resp = format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                    body.len(),
                    body
                );
                let _ = socket.write_all(resp.as_bytes()).await;
            }
            seen
        });

        // 直接验证「连续解析」这一层：两条非流式都应命中，流式应落空
        let first = b"GET /mcp?directory=x HTTP/1.1\r\nHost: 127.0.0.1\r\nAccept: */*\r\n\r\n";
        let second = b"GET /vcs?directory=x HTTP/1.1\r\nHost: 127.0.0.1\r\nAccept: */*\r\n\r\n";
        let stream = b"GET /global/event HTTP/1.1\r\nHost: 127.0.0.1\r\nAccept: text/event-stream\r\n\r\n";

        let r1 = parse_keepalive_request(first).expect("第一条应命中复用");
        let r2 = parse_keepalive_request(second).expect("第二条应命中复用");
        assert!(parse_keepalive_request(stream).is_none(), "流式不应进复用隧道");

        // 两条请求各自转发到假 opencode 并拿到正确 body
        let resp1 = handle_keepalive_request(port, &r1, &[]).await.unwrap();
        let resp2 = handle_keepalive_request(port, &r2, &[]).await.unwrap();

        let text1 = String::from_utf8(resp1).unwrap();
        let text2 = String::from_utf8(resp2).unwrap();
        assert!(text1.starts_with("HTTP/1.1 200 OK\r\n"), "转发应保留状态码: {text1}");
        assert!(text1.contains("/mcp?directory=x"), "响应体应来自第一条请求: {text1}");
        assert!(text2.contains("/vcs?directory=x"), "响应体应来自第二条请求: {text2}");
        // 响应必须可判定边界，否则网关无法复用连接
        assert!(text1.contains("Content-Length:"), "必须带精确 Content-Length");
        assert!(text1.contains("Connection: keep-alive"), "必须声明 keep-alive");

        let seen = upstream.await.unwrap();
        assert_eq!(seen, vec!["/mcp?directory=x", "/vcs?directory=x"]);
    }

    /// 上游非 2xx 时应原样回传状态码，让客户端走原有错误路径。
    #[tokio::test]
    async fn keepalive_forwards_upstream_error_status() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();

        tokio::spawn(async move {
            let (mut socket, _) = listener.accept().await.unwrap();
            let mut buf = vec![0u8; 4096];
            let _ = socket.read(&mut buf).await.unwrap();
            let body = "{\"error\":\"nope\"}";
            let resp = format!(
                "HTTP/1.1 404 Not Found\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                body.len(),
                body
            );
            let _ = socket.write_all(resp.as_bytes()).await;
        });

        let req = parse_keepalive_request(b"GET /missing HTTP/1.1\r\nHost: x\r\n\r\n").unwrap();
        let resp = handle_keepalive_request(port, &req, &[]).await.unwrap();
        let text = String::from_utf8(resp).unwrap();
        assert!(text.starts_with("HTTP/1.1 404"), "应保留上游状态码: {text}");
        assert!(text.contains("nope"));
    }

    /// 复用的轻量隧道上会混入非轻量请求：网关按 host 复用连接，同一实例的
    /// `/permission`、`/vcs` 等普通请求会落到同一条隧道上。
    ///
    /// 回归：此前非轻量请求会命中 400 分支并**关闭整条隧道**，导致网关把连接
    /// 判死并退回裸透传，表现为「消息投影时好时坏、并发一上来就永久失效」。
    /// 这里验证分流判定：非轻量请求应被识别为「可透传」，而不是「非法请求」。
    #[test]
    fn mixed_requests_on_lightweight_tunnel_are_classified_correctly() {
        // 轻量请求：命中投影分支
        let lw = b"GET /session/ses_abc/message?limit=1 HTTP/1.1\r\nHost: x\r\nX-Lightweight: 1\r\n\r\n";
        assert!(parse_lightweight_request(lw).is_some(), "轻量请求应走投影");
        assert!(parse_keepalive_request(lw).is_some(), "轻量请求同时也可透传（分流看优先级）");

        // 同一条隧道上的普通请求：必须能被识别为可透传，而不是非法
        let plain = b"GET /permission?directory=D%3A%2Ftmp HTTP/1.1\r\nHost: x\r\nAccept: */*\r\n\r\n";
        assert!(
            parse_lightweight_request(plain).is_none(),
            "普通请求不应被当成轻量"
        );
        assert!(
            parse_keepalive_request(plain).is_some(),
            "普通请求必须可透传——否则隧道会被关掉"
        );

        // 流式请求仍应被两条复用路径共同排除（网关已按 Accept 分流）
        let sse = b"GET /global/event HTTP/1.1\r\nHost: x\r\nAccept: text/event-stream\r\n\r\n";
        assert!(parse_lightweight_request(sse).is_none());
        assert!(parse_keepalive_request(sse).is_none(), "流式不应进复用隧道");
    }

    /// 端到端：一条隧道连续处理「轻量 + 普通 + 普通」三类请求，全部必须成功。
    /// 这条用例直接锁住上面那个 400-and-close 的回归。
    #[tokio::test]
    async fn tunnel_serves_mixed_request_types_without_closing() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();

        // 假 opencode：对每个请求回其 path，用于确认转发目标正确
        tokio::spawn(async move {
            for _ in 0..2 {
                let Ok((mut socket, _)) = listener.accept().await else {
                    return;
                };
                let mut buf = vec![0u8; 4096];
                let Ok(n) = socket.read(&mut buf).await else { return };
                let head = String::from_utf8_lossy(&buf[..n]).to_string();
                let path = head.split_whitespace().nth(1).unwrap_or("/").to_string();
                let body = format!("{{\"path\":\"{}\"}}", path);
                let resp = format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                    body.len(),
                    body
                );
                let _ = socket.write_all(resp.as_bytes()).await;
            }
        });

        // 普通请求经复用路径转发应成功（这是修复前会 400 的那条路径）
        let req = parse_keepalive_request(
            b"GET /permission?directory=D%3A%2Ftmp HTTP/1.1\r\nHost: x\r\nAccept: */*\r\n\r\n",
        )
        .expect("普通请求应被识别为可透传");
        let resp = handle_keepalive_request(port, &req, &[]).await.unwrap();
        let text = String::from_utf8(resp).unwrap();
        assert!(text.starts_with("HTTP/1.1 200 OK"), "普通请求应成功透传: {text}");
        assert!(text.contains("/permission?directory=D%3A%2Ftmp"), "应转发到正确路径: {text}");
    }

    /// POST 的请求体必须被读出并转发。此前 keepalive 隧道只解析方法与路径、
    /// 完全不读 body：POST 会以空 body 发出，且残留字节会被下一条请求头解析
    /// 误读，导致整条复用隧道协议错位。
    #[tokio::test]
    async fn keepalive_forwards_post_body() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();

        let upstream = tokio::spawn(async move {
            let (mut socket, _) = listener.accept().await.unwrap();
            let mut buf = vec![0u8; 8192];
            // 先把头读完，再按 Content-Length 读满 body（模拟真实上游）
            let n = socket.read(&mut buf).await.unwrap();
            let text = String::from_utf8_lossy(&buf[..n]).to_string();
            let (head, rest) = text.split_once("\r\n\r\n").unwrap();
            let length: usize = head
                .lines()
                .find_map(|line| line.strip_prefix("Content-Length: "))
                .and_then(|value| value.trim().parse().ok())
                .unwrap_or(0);
            let mut body = rest.as_bytes().to_vec();
            while body.len() < length {
                let n = socket.read(&mut buf).await.unwrap();
                if n == 0 {
                    break;
                }
                body.extend_from_slice(&buf[..n]);
            }
            let reply = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                body.len(),
                String::from_utf8_lossy(&body)
            );
            let _ = socket.write_all(reply.as_bytes()).await;
            (text, String::from_utf8_lossy(&body).to_string())
        });

        let payload = br#"{"prompt":"hello"}"#;
        let head = format!(
            "POST /session/ses_1/prompt HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n",
            payload.len()
        );

        // 首包同时带上 body 前缀，验证「分隔符之后的字节不被丢弃」
        let mut first = head.clone().into_bytes();
        first.extend_from_slice(&payload[..5]);
        let request = parse_keepalive_request(&first).expect("POST 应命中复用隧道");
        assert_eq!(request.content_length, payload.len());
        assert_eq!(request.method, "POST");

        // 剩余 body 由后续帧补齐（模拟网关把 body 分帧发送）
        let mut stream = frame_stream(vec![payload[5..].to_vec()]);
        let body = read_request_body(&first, request.content_length, &mut stream)
            .await
            .unwrap();
        assert_eq!(
            String::from_utf8_lossy(&body),
            String::from_utf8_lossy(payload),
            "body 必须等于「首包剩余 + 后续帧」的完整内容"
        );

        let resp = handle_keepalive_request(port, &request, &body).await.unwrap();
        let text = String::from_utf8(resp).unwrap();
        assert!(text.contains(r#"{"prompt":"hello"}"#), "上游应收到完整 body: {text}");

        let (upstream_head, upstream_body) = upstream.await.unwrap();
        assert!(upstream_head.starts_with("POST /session/ses_1/prompt"));
        assert_eq!(upstream_body, r#"{"prompt":"hello"}"#);
    }

    /// 首包已含完整 body 时不应再去读流（否则会阻塞到空闲超时）。
    #[tokio::test]
    async fn keepalive_body_fully_in_first_frame() {
        let payload = br#"{"a":1}"#;
        let mut first = format!(
            "POST /p HTTP/1.1\r\nHost: x\r\nContent-Length: {}\r\n\r\n",
            payload.len()
        )
        .into_bytes();
        first.extend_from_slice(payload);

        // 空帧流：若实现错误地继续读，这里会挂起并被超时兜住
        let mut stream = frame_stream(vec![]);
        let body = tokio::time::timeout(
            Duration::from_secs(1),
            read_request_body(&first, payload.len(), &mut stream),
        )
        .await
        .expect("首包已含完整 body 时不应阻塞")
        .unwrap();
        assert_eq!(String::from_utf8_lossy(&body), String::from_utf8_lossy(payload));
    }

    /// Content-Length 为 0（或缺失）时不读 body，且不得阻塞。
    #[tokio::test]
    async fn keepalive_without_body_returns_empty() {
        let head = b"POST /session/ses_1/abort HTTP/1.1\r\nHost: x\r\nContent-Length: 0\r\n\r\n";
        let request = parse_keepalive_request(head).unwrap();
        assert_eq!(request.content_length, 0);

        let mut stream = frame_stream(vec![]);
        // 流上没有任何字节：若实现错误地去读，这里会挂起
        let body = tokio::time::timeout(
            Duration::from_secs(1),
            read_request_body(head, request.content_length, &mut stream),
        )
        .await
        .expect("无 body 的请求不应阻塞")
        .unwrap();
        assert!(body.is_empty());
    }

    /// 伪造的超大 Content-Length 必须被拒绝，避免无限缓冲。
    #[tokio::test]
    async fn keepalive_rejects_oversized_content_length() {
        let head = format!(
            "POST /x HTTP/1.1\r\nHost: x\r\nContent-Length: {}\r\n\r\n",
            KEEPALIVE_BODY_MAX + 1
        );
        let request = parse_keepalive_request(head.as_bytes()).unwrap();
        let mut stream = frame_stream(vec![]);
        let result = read_request_body(head.as_bytes(), request.content_length, &mut stream).await;
        assert!(result.is_err(), "超大 Content-Length 应被拒绝");
    }

    /// Content-Length 必须被解析出来：漏解析会让 POST 以空 body 转发。
    #[test]
    fn keepalive_parses_content_length() {
        let head = b"POST /p HTTP/1.1\r\nHost: x\r\nContent-Length: 42\r\n\r\n";
        assert_eq!(parse_keepalive_request(head).unwrap().content_length, 42);

        // 大小写不敏感
        let head = b"POST /p HTTP/1.1\r\nHost: x\r\nCONTENT-LENGTH: 7\r\n\r\n";
        assert_eq!(parse_keepalive_request(head).unwrap().content_length, 7);

        // GET 无该头时为 0
        let head = b"GET /p HTTP/1.1\r\nHost: x\r\n\r\n";
        assert_eq!(parse_keepalive_request(head).unwrap().content_length, 0);
    }

    /// 构造一个只吐出给定二进制帧的内存流，用于在不起真实 WebSocket 的前提下
    /// 覆盖请求体读取路径。发送端丢弃。
    /// 测试用帧源：按顺序吐出给定二进制帧，并接受（丢弃）Pong 等出站帧。
    ///
    /// 实现 Sink 是必需的：`read_request_body` 在读到 Ping 时会回 Pong，真实
    /// `AgentWsStream` 既是 Stream 也是 Sink。
    struct FrameStream {
        frames: std::collections::VecDeque<Result<Message, tokio_tungstenite::tungstenite::Error>>,
    }

    impl Stream for FrameStream {
        type Item = Result<Message, tokio_tungstenite::tungstenite::Error>;
        fn poll_next(
            mut self: std::pin::Pin<&mut Self>,
            _cx: &mut std::task::Context<'_>,
        ) -> std::task::Poll<Option<Self::Item>> {
            std::task::Poll::Ready(self.frames.pop_front())
        }
    }

    impl Sink<Message> for FrameStream {
        type Error = tokio_tungstenite::tungstenite::Error;
        fn poll_ready(
            self: std::pin::Pin<&mut Self>,
            _cx: &mut std::task::Context<'_>,
        ) -> std::task::Poll<Result<(), Self::Error>> {
            std::task::Poll::Ready(Ok(()))
        }
        fn start_send(
            self: std::pin::Pin<&mut Self>,
            _item: Message,
        ) -> Result<(), Self::Error> {
            Ok(())
        }
        fn poll_flush(
            self: std::pin::Pin<&mut Self>,
            _cx: &mut std::task::Context<'_>,
        ) -> std::task::Poll<Result<(), Self::Error>> {
            std::task::Poll::Ready(Ok(()))
        }
        fn poll_close(
            self: std::pin::Pin<&mut Self>,
            _cx: &mut std::task::Context<'_>,
        ) -> std::task::Poll<Result<(), Self::Error>> {
            std::task::Poll::Ready(Ok(()))
        }
    }

    fn frame_stream(frames: Vec<Vec<u8>>) -> FrameStream {
        FrameStream {
            frames: frames.into_iter().map(|data| Ok(Message::Binary(data.into()))).collect(),
        }
    }

    #[test]
    fn matches_lightweight_message_request() {
        let head = b"GET /session/ses_abc/message?limit=100&directory=D%3A%2Ftmp HTTP/1.1\r\n\
Host: 127.0.0.1\r\nX-Lightweight: 1\r\nAccept: */*\r\n\r\n";
        let req = parse_lightweight_request(head).expect("应命中");
        assert_eq!(req.path, "/session/ses_abc/message?limit=100&directory=D%3A%2Ftmp");
    }

    #[test]
    fn ignores_requests_without_lightweight_header() {
        let head = b"GET /session/ses_abc/message?limit=100 HTTP/1.1\r\nHost: x\r\n\r\n";
        assert!(parse_lightweight_request(head).is_none());
    }

    #[test]
    fn ignores_non_message_paths_and_non_get() {
        let get_other = b"GET /session/ses_abc/other HTTP/1.1\r\nX-Lightweight: 1\r\n\r\n";
        assert!(parse_lightweight_request(get_other).is_none());
        let post = b"POST /session/ses_abc/message HTTP/1.1\r\nX-Lightweight: 1\r\n\r\n";
        assert!(parse_lightweight_request(post).is_none());
    }

    #[test]
    fn builds_valid_http_response_with_content_length() {
        let body = b"[]";
        let resp = build_http_response(200, "application/json", body, &[("X-Lightweight", "1")]);
        let text = String::from_utf8(resp).unwrap();
        assert!(text.starts_with("HTTP/1.1 200 OK\r\n"));
        assert!(text.contains("Content-Length: 2\r\n"));
        assert!(text.contains("X-Lightweight: 1\r\n"));
        assert!(text.ends_with("\r\n\r\n[]"));
    }

    // 起一个真实监听回环端口的 socket，验证 listening_pid 能反查到本进程 PID。
    // 这是「进程 ↔ 端口」关联验证的基础：查不到 PID 就无法判定实例是否真的在跑。
    #[tokio::test]
    async fn listening_pid_finds_own_listener() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();
        let expected = std::process::id();

        let found = listening_pid(port);
        assert_eq!(found, Some(expected), "应能反查到监听该端口的本进程 PID");
    }

    #[tokio::test]
    async fn listening_pid_returns_none_for_unused_port() {
        // 绑一个端口再立即释放，得到「几乎确定无人监听」的端口号。
        let port = {
            let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
            listener.local_addr().unwrap().port()
        };
        assert_eq!(listening_pid(port), None);
    }

    #[tokio::test]
    async fn probe_reports_listener_pid_and_process_match() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();

        // 用当前测试进程的可执行名做匹配，验证关联判定为真。
        let exe_name = std::env::current_exe()
            .ok()
            .and_then(|p| p.file_name().map(|n| n.to_string_lossy().to_string()))
            .unwrap_or_default();
        let payload = serde_json::json!({
            "provider": "opencode",
            "port": port,
            "process_match": [exe_name],
        });

        let raw = probe(&payload.to_string()).await.unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&raw).unwrap();

        assert_eq!(parsed["portListening"], serde_json::json!(true));
        assert_eq!(parsed["listenerPid"], serde_json::json!(std::process::id()));
        assert_eq!(
            parsed["listenerMatchesProcess"],
            serde_json::json!(true),
            "监听 PID 的进程名应命中匹配规则"
        );
        // 资源字段必须存在且为数值：前端据此展示内存/CPU。
        assert!(
            parsed["memoryBytes"].as_u64().is_some(),
            "memoryBytes 应为数值: {raw}"
        );
        assert!(
            parsed["cpuPercent"].as_f64().is_some(),
            "cpuPercent 应为数值: {raw}"
        );
        // 监听者就是本测试进程，内存必然大于 0。
        assert!(
            parsed["memoryBytes"].as_u64().unwrap() > 0,
            "本进程内存占用应大于 0"
        );
    }

    #[test]
    fn process_resources_returns_zero_for_unknown_pid() {
        // 不存在的 PID 不应 panic，返回 0 让调用方自行判断。
        assert_eq!(process_resources(0), (0, 0.0));
    }

    #[test]
    fn process_resources_reads_own_memory() {
        let (memory, cpu) = process_resources(std::process::id());
        assert!(memory > 0, "本进程内存应大于 0，got {memory}");
        assert!(cpu >= 0.0, "CPU 百分比不应为负，got {cpu}");
    }

    #[tokio::test]
    async fn probe_does_not_match_listener_when_process_name_differs() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();

        // 匹配一个不存在的进程名：端口在监听，但关联判定必须为假。
        // 这正是修复前会误报 online 的场景。
        let payload = serde_json::json!({
            "provider": "opencode",
            "port": port,
            "process_match": ["definitely-not-a-real-process-xyz"],
        });

        let raw = probe(&payload.to_string()).await.unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&raw).unwrap();

        assert_eq!(parsed["portListening"], serde_json::json!(true));
        assert_eq!(parsed["listenerMatchesProcess"], serde_json::json!(false));
    }

    #[test]
    fn redacts_token_query_values() {
        let input = "数据通道连接失败: ws://127.0.0.1:3000/ws/agent-port?server_id=abc&stream_id=xyz&token=deadbeef12345678";
        let output = redact_secrets(input, &[]);
        assert!(!output.contains("deadbeef12345678"), "token must be removed: {output}");
        assert!(output.contains("server_id=abc"));
        assert!(output.contains("stream_id=xyz"));
    }

    #[test]
    fn redacts_stream_token_without_leaving_marker() {
        let input = "stream_token=abcdef&server_id=abc";
        let output = redact_secrets(input, &[]);
        assert!(!output.contains("abcdef"), "token must be removed: {output}");
        assert!(!output.contains("stream_token="), "marker must be consumed: {output}");
        assert!(output.contains("server_id=abc"));
    }

    #[test]
    fn terminates_on_marker_only_input() {
        // 曾经的死循环输入：替换后若仍保留键会无限循环；空值也不得留下键痕迹。
        let output = redact_secrets("token=abc token=def token=", &[]);
        assert!(!output.contains("abc"));
        assert!(!output.contains("def"));
        assert!(!output.contains("token="), "marker must be removed: {output}");
    }

    #[test]
    fn leaves_clean_message_untouched() {
        let input = "连接 127.0.0.1:4096 失败";
        assert_eq!(redact_secrets(input, &[]), input);
    }

    #[test]
    fn redacts_exact_known_token_even_when_encoded() {
        let secret = "deadbeefcafe1234";
        let input = format!("失败: url=%2Fws%2Fagent-port%3Ftoken%3D{secret}&x=1");
        let output = redact_secrets(&input, &[secret]);
        assert!(!output.contains(secret), "exact token must be removed: {output}");
    }

    #[test]
    fn extracts_token_from_bridge_payload() {
        let raw = r#"{"port":4096,"stream_id":"sid","stream_token":"tok123"}"#;
        assert_eq!(extract_token(raw), "tok123");
        assert_eq!(extract_token("not-json"), "");
    }
}
