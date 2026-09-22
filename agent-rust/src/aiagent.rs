// AI Agent 管理模块的 Agent 侧支持：
//   - probe：探测本机某个 AI Agent 进程是否运行、端口是否监听（任务 56）
//   - bridge：反连云端数据通道，把本机 127.0.0.1:<port> 的字节双向搬运（任务 55）
//
// 数据通道是「一条通道对应一次 HTTP 请求」的字节管道；云端在此之上用
// http.Transport 说话，因此天然支持 SSE 流式响应，无需在本层解析 HTTP。

use bytes::Bytes;
use futures_util::{SinkExt, StreamExt};
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
    // 本机端口与云端数据通道的目标互不依赖（前者是 127.0.0.1:<agent_port>，后者是面板
    // 网关），两次握手并发执行，避免把两段 RTT 串成首包前的固定延迟。
    // 两者各自的超时、错误文案与失败语义保持原样：任一失败即整体失败。
    let local_fut = async {
        tokio::time::timeout(BRIDGE_CONNECT_TIMEOUT, TcpStream::connect(&target))
            .await
            .map_err(|_| format!("连接 {} 超时", target))?
            .map_err(|err| format!("连接 {} 失败: {}", target, err))
    };
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
    let (mut local, mut ws_stream) = tokio::try_join!(local_fut, ws_fut)?;

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
    use super::{extract_token, listening_pid, probe, process_resources, redact_secrets};
    use tokio::net::TcpListener;

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
