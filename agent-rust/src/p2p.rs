use serde::Deserialize;
use std::collections::HashMap;
use std::net::{IpAddr, Ipv4Addr, SocketAddr};
use std::sync::atomic::{AtomicU8, Ordering};
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};
use tokio::net::UdpSocket;
use tokio::sync::oneshot;
use tokio::time::timeout;

use crate::nat;

// ==================== P2P 打洞 + UDP 直连数据面 ====================
// 数据面复用 tcp_forwarder 的帧语义（UDP datagram 即一帧）：
//   [1B type][2B conn_id BE][4B len BE][payload]，len 供校验（datagram 已天然分帧）
//   type 0x01=DATA 0x02=CLOSE 0x03=KEEPALIVE 0x04=UDP_DATA 0x05=UDP_CLOSE
// 打洞握手包用独立 magic=0x50 前缀区分：
//   [1B magic=0x50][4B session_id BE][8B peer_id BE][1B hole_type][payload]
//   hole_type 0x01=ping 0x02=pong 0x03=session_confirm

const HOLE_MAGIC: u8 = 0x50;
const HOLE_PING: u8 = 0x01;
const HOLE_PONG: u8 = 0x02;
const HOLE_CONFIRM: u8 = 0x03;
const HOLE_HDR: usize = 14;

const FRAME_UDP_DATA: u8 = 0x04;
const FRAME_UDP_CLOSE: u8 = 0x05;
const FRAME_KEEPALIVE: u8 = 0x03;

// P2P 状态
const STATE_RELAY: u8 = 0; // 中继隧道保底
const STATE_NEGOTIATING: u8 = 1; // 打洞协商中
const STATE_DIRECT: u8 = 2; // 直连已建立
const STATE_FALLBACK: u8 = 3; // 打洞失败，回退中继

const PUNCH_ROUNDS: usize = 16; // 最多发包轮数
const PUNCH_INTERVAL: Duration = Duration::from_millis(500);

#[derive(Debug, Deserialize)]
struct Request {
    operation: String,
    #[serde(default)]
    forward_id: String,
    #[serde(default)]
    local_host: String,
    #[serde(default)]
    local_port: u16,
    #[serde(default)]
    session_id: u32,
    #[serde(default)]
    stun_servers: Vec<String>,
    #[serde(default)]
    egress_ip: Option<String>,
    #[serde(default)]
    stun_asset_url: String,
    #[serde(default)]
    stun_asset_sha256: String,
    #[serde(default)]
    stun_port: u16,
    #[serde(default)]
    peer_candidates: Vec<String>,
    #[serde(default)]
    local_candidates: Vec<String>,
}

fn state_name(s: u8) -> &'static str {
    match s {
        STATE_RELAY => "relay",
        STATE_NEGOTIATING => "negotiating",
        STATE_DIRECT => "direct",
        STATE_FALLBACK => "relay_fallback",
        _ => "unknown",
    }
}

struct P2pSession {
    state: Arc<AtomicU8>,
    peer: Arc<Mutex<Option<SocketAddr>>>,
    cancel: Mutex<Option<oneshot::Sender<()>>>,
    started_at: Instant,
}

struct P2pManager {
    sessions: Mutex<HashMap<String, Arc<P2pSession>>>,
}

static P2P_MGR: OnceLock<P2pManager> = OnceLock::new();

fn manager() -> &'static P2pManager {
    P2P_MGR.get_or_init(|| P2pManager { sessions: Mutex::new(HashMap::new()) })
}

fn parse_sockaddr(s: &str) -> Option<SocketAddr> {
    s.parse::<SocketAddr>()
        .ok()
        .or_else(|| format!("{s}:0").parse::<SocketAddr>().ok())
}

// ==================== 打洞握手帧编解码 ====================

fn build_hole(session_id: u32, peer_id: u64, hole_type: u8) -> [u8; HOLE_HDR] {
    let mut buf = [0u8; HOLE_HDR];
    buf[0] = HOLE_MAGIC;
    buf[1..5].copy_from_slice(&session_id.to_be_bytes());
    buf[5..13].copy_from_slice(&peer_id.to_be_bytes());
    buf[13] = hole_type;
    buf
}

fn parse_hole(buf: &[u8]) -> Option<(u32, u64, u8)> {
    if buf.len() < HOLE_HDR || buf[0] != HOLE_MAGIC {
        return None;
    }
    let session_id = u32::from_be_bytes([buf[1], buf[2], buf[3], buf[4]]);
    let peer_id = u64::from_be_bytes([
        buf[5], buf[6], buf[7], buf[8], buf[9], buf[10], buf[11], buf[12],
    ]);
    Some((session_id, peer_id, buf[13]))
}

// ==================== 打洞主循环 ====================

/// 打洞 + 直连数据面主循环。握手上：向对端候选端点发 ping，监听 ping/pong/confirm；
/// 直连后：收到数据帧桥接到本地 UDP 服务，本地回包封装为帧发回对端。
async fn run_punch(
    sock: Arc<UdpSocket>,
    session_id: u32,
    peer_id: u64,
    peer_candidates: Vec<SocketAddr>,
    local_host: String,
    local_port: u16,
    state: Arc<AtomicU8>,
    peer: Arc<Mutex<Option<SocketAddr>>>,
    cancel_rx: oneshot::Receiver<()>,
) {
    let ping = build_hole(session_id, peer_id, HOLE_PING);
    let mut sent = 0usize;
    let mut last_send = Instant::now();
    // 本地 UDP 会话映射 conn_id -> 本地 socket（用于回程）；任务自持会话表以支持空闲回收
    let udp_sessions: Arc<Mutex<HashMap<u16, Arc<UdpSocket>>>> = Arc::new(Mutex::new(HashMap::new()));

    let mut buf = [0u8; 65535];
    tokio::pin!(cancel_rx);
    loop {
        if sent < PUNCH_ROUNDS && last_send.elapsed() >= PUNCH_INTERVAL {
            for addr in &peer_candidates {
                let _ = sock.send_to(&ping, addr).await;
            }
            sent += 1;
            last_send = Instant::now();
        }

        let recv = tokio::select! {
            _ = &mut cancel_rx => {
                // 收到停止信号：退出打洞与数据面，socket/任务随之释放
                break;
            }
            r = timeout(Duration::from_millis(200), sock.recv_from(&mut buf)) => r,
        };
        match recv {
            Ok(Ok((n, src))) => {
                if let Some((sid, _pid, htype)) = parse_hole(&buf[..n]) {
                    // session_id 是面板仅分发给两端对端的随机共享密钥，匹配即可证明是合法对端；
                    // peer_id 仅用于标识发送方，不做自身匹配（两端 peer_id 不同）。
                    if sid == session_id {
                        match htype {
                            HOLE_PING => {
                                let pong = build_hole(session_id, peer_id, HOLE_PONG);
                                let _ = sock.send_to(&pong, src).await;
                            }
                            HOLE_PONG => {
                                *peer.lock().unwrap() = Some(src);
                                state.store(STATE_DIRECT, Ordering::Relaxed);
                                let confirm = build_hole(session_id, peer_id, HOLE_CONFIRM);
                                let _ = sock.send_to(&confirm, src).await;
                            }
                            HOLE_CONFIRM => {
                                *peer.lock().unwrap() = Some(src);
                                state.store(STATE_DIRECT, Ordering::Relaxed);
                            }
                            _ => {}
                        }
                        continue;
                    }
                }
                // 直连后走数据面：仅对已确认的对端处理
                if state.load(Ordering::Relaxed) != STATE_DIRECT {
                    continue;
                }
                let Some(p) = *peer.lock().unwrap() else { continue };
                if src != p {
                    continue;
                }
                let Some((typ, conn_id, payload)) = parse_frame(&buf[..n]) else {
                    continue;
                };
                match typ {
                    FRAME_KEEPALIVE => {}
                    FRAME_UDP_DATA => {
                        let existing = udp_sessions.lock().unwrap().get(&conn_id).cloned();
                        if let Some(local) = existing {
                            let _ = local.send(payload).await;
                        } else {
                            let Ok(local_sock) = UdpSocket::bind((Ipv4Addr::UNSPECIFIED, 0)).await else {
                                continue;
                            };
                            let local = Arc::new(local_sock);
                            let target: SocketAddr = format!("{local_host}:{local_port}")
                                .parse()
                                .unwrap_or_else(|_| SocketAddr::new(IpAddr::V4(Ipv4Addr::LOCALHOST), local_port));
                            let _ = local.send_to(payload, target).await;
                            let sess = local.clone();
                            let sock2 = sock.clone();
                            let peer2 = peer.clone();
                            let sessions2 = udp_sessions.clone();
                            let cid = conn_id;
                            tokio::spawn(async move {
                                let mut b2 = vec![0u8; 65535];
                                loop {
                                    match timeout(Duration::from_secs(60), sess.recv_from(&mut b2)).await {
                                        Ok(Ok((n, _))) => {
                                            let pp = match *peer2.lock().unwrap() {
                                                Some(p) => p,
                                                None => continue,
                                            };
                                            let mut f = Vec::with_capacity(8 + n);
                                            f.push(FRAME_UDP_DATA);
                                            f.extend_from_slice(&cid.to_be_bytes());
                                            f.extend_from_slice(&(n as u32).to_be_bytes());
                                            f.extend_from_slice(&b2[..n]);
                                            let _ = sock2.send_to(&f, pp).await;
                                        }
                                        _ => {
                                            // 60s 无回程流量：关闭空闲会话，释放 socket 与转发任务
                                            sessions2.lock().unwrap().remove(&cid);
                                            break;
                                        }
                                    }
                                }
                            });
                            udp_sessions.lock().unwrap().insert(conn_id, local);
                        }
                    }
                    FRAME_UDP_CLOSE => {
                        udp_sessions.lock().unwrap().remove(&conn_id);
                    }
                    _ => {}
                }
            }
            Ok(Err(_)) => break,
            Err(_) => {
                // 超时：P2P 握手中继续；若已协商完进入直连，超时只是无包
            }
        }

        // 打洞超时回退中继
        if sent >= PUNCH_ROUNDS && state.load(Ordering::Relaxed) != STATE_DIRECT {
            state.compare_exchange(STATE_NEGOTIATING, STATE_FALLBACK, Ordering::Relaxed, Ordering::Relaxed).ok();
        }
        if state.load(Ordering::Relaxed) == STATE_FALLBACK {
            break;
        }
    }
}

fn parse_frame(buf: &[u8]) -> Option<(u8, u16, &[u8])> {
    if buf.len() < 7 {
        return None;
    }
    let typ = buf[0];
    let conn_id = u16::from_be_bytes([buf[1], buf[2]]);
    let len = u32::from_be_bytes([buf[3], buf[4], buf[5], buf[6]]) as usize;
    if buf.len() < 7 + len {
        return None;
    }
    Some((typ, conn_id, &buf[7..7 + len]))
}

// ==================== 操作入口 ====================

pub async fn reconcile(raw: &str) -> Result<String, String> {
    let request: Request = serde_json::from_str(raw).map_err(|e| format!("invalid p2p request: {e}"))?;
    match request.operation.trim().to_ascii_lowercase().as_str() {
        "collect_endpoints" => collect_endpoints(&request).await,
        "hole_punch" => hole_punch(&request).await,
        "status" => status(&request.forward_id).await,
        "stop" => stop(&request.forward_id).await,
        "bootstrap_stun" => bootstrap_stun(&request).await,
        "stun_status" => stun_status().await,
        "stun_stop" => stun_stop().await,
        other => Err(format!("unknown p2p operation: {other}")),
    }
}

async fn collect_endpoints(request: &Request) -> Result<String, String> {
    let servers = if request.stun_servers.is_empty() {
        vec!["stun.cloudflare.com:3478".to_string()]
    } else {
        request.stun_servers.clone()
    };
    let endpoints = nat::collect_endpoints(&servers, request.egress_ip.as_deref(), 3000).await;
    Ok(serde_json::json!({ "endpoints": endpoints }).to_string())
}

async fn hole_punch(request: &Request) -> Result<String, String> {
    if request.forward_id.is_empty() || request.local_port == 0 {
        return Err("p2p hole_punch 需要 forward_id 与 local_port".to_string());
    }
    if let Some(sess) = manager().sessions.lock().unwrap().get(&request.forward_id).cloned() {
        let st = sess.state.load(Ordering::Relaxed);
        return Ok(serde_json::json!({
            "status": "negotiating",
            "forward_id": request.forward_id,
            "p2p_state": state_name(st),
        }).to_string());
    }

    let session_id = if request.session_id != 0 {
        request.session_id
    } else {
        nat_stamp() as u32
    };
    let peer_id = nat_peer_id();

    let sock = Arc::new(
        UdpSocket::bind((Ipv4Addr::UNSPECIFIED, 0))
            .await
            .map_err(|e| format!("p2p socket bind failed: {e}"))?,
    );

    let peer_candidates: Vec<SocketAddr> = request
        .peer_candidates
        .iter()
        .filter_map(|s| parse_sockaddr(s))
        .collect();
    let local_candidates: Vec<SocketAddr> = request
        .local_candidates
        .iter()
        .filter_map(|s| parse_sockaddr(s))
        .collect();

    // 本地候选注入自身监听：让对端能打到我本机地址
    let mut candidates = local_candidates;
    if let Ok(local) = sock.local_addr() {
        candidates.push(local);
    }
    // 若未显式给对端候选但给了本地地址，互为候选（环回测试友好）
    let candidates = if peer_candidates.is_empty() {
        candidates
    } else {
        peer_candidates
    };

    let state = Arc::new(AtomicU8::new(STATE_NEGOTIATING));
    let peer = Arc::new(Mutex::new(None));
    let (cancel_tx, cancel_rx) = oneshot::channel::<()>();
    let entry = Arc::new(P2pSession {
        state: state.clone(),
        peer: peer.clone(),
        cancel: Mutex::new(Some(cancel_tx)),
        started_at: Instant::now(),
    });
    manager().sessions.lock().unwrap().insert(request.forward_id.clone(), entry.clone());

    let sock2 = sock.clone();
    let fwd = request.forward_id.clone();
    let local_host = request.local_host.clone();
    let local_port = request.local_port;
    tokio::spawn(async move {
        run_punch(sock2, session_id, peer_id, candidates, local_host, local_port, state, peer, cancel_rx).await;
        // 会话结束：保留状态供查询
    });

    Ok(serde_json::json!({
        "status": "negotiating",
        "forward_id": fwd,
        "session_id": session_id,
        "p2p_state": "negotiating",
    }).to_string())
}

async fn status(forward_id: &str) -> Result<String, String> {
    let mgr = manager().sessions.lock().unwrap();
    if let Some(sess) = mgr.get(forward_id) {
        let st = sess.state.load(Ordering::Relaxed);
        let peer_addr = sess.peer.lock().unwrap().map(|p| p.to_string());
        Ok(serde_json::json!({
            "forward_id": forward_id,
            "p2p_state": state_name(st),
            "connected": st == STATE_DIRECT,
            "peer": peer_addr,
            "uptime_seconds": sess.started_at.elapsed().as_secs(),
        }).to_string())
    } else {
        Ok(serde_json::json!({
            "forward_id": forward_id,
            "p2p_state": state_name(STATE_RELAY),
            "connected": false,
        }).to_string())
    }
}

async fn stop(forward_id: &str) -> Result<String, String> {
    if let Some(sess) = manager().sessions.lock().unwrap().remove(forward_id) {
        if let Some(tx) = sess.cancel.lock().unwrap().take() {
            let _ = tx.send(());
        }
    }
    Ok(serde_json::json!({ "status": "stopped", "forward_id": forward_id }).to_string())
}

fn nat_stamp() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0) as u64
}

fn nat_peer_id() -> u64 {
    // 稳定但可变的 peer 标识：主机名哈希 + 时间戳高位，避免跨会话冲突
    let host = hostname::get().map(|h| h.to_string_lossy().into_owned()).unwrap_or_default();
    let mut h: u64 = 0xcbf29ce484222325;
    for b in host.bytes() {
        h ^= b as u64;
        h = h.wrapping_mul(0x100000001b3);
    }
    h ^ (nat_stamp() << 32)
}

// ==================== 自建 STUN 服务器托管 ====================
// 复用 api-monitor-relay 的二进制生命周期模式：由本 Agent 下载 api-monitor-stun
// 并托管为常驻进程，供 P2P 打洞做自建 STUN（减少对外部公共 STUN 的依赖）。
// 幂等：已运行即跳过。bootstrap_stun / stun_status / stun_stop。

struct StunEntry {
    port: u16,
    pid: u32,
    started_at: Instant,
}

static STUN_ENTRY: OnceLock<std::sync::Mutex<Option<StunEntry>>> = OnceLock::new();

fn stun_entry() -> &'static std::sync::Mutex<Option<StunEntry>> {
    STUN_ENTRY.get_or_init(|| std::sync::Mutex::new(None))
}

fn stun_root() -> std::path::PathBuf {
    #[cfg(unix)]
    {
        std::path::PathBuf::from("/opt/api-monitor-stun")
    }
    #[cfg(windows)]
    {
        std::env::var_os("ProgramData")
            .map(std::path::PathBuf::from)
            .unwrap_or_else(|| std::path::PathBuf::from("C:\\ProgramData"))
            .join("api-monitor-stun")
    }
}

fn stun_binary_path() -> std::path::PathBuf {
    let root = stun_root();
    #[cfg(unix)]
    let name = "api-monitor-stun";
    #[cfg(windows)]
    let name = "api-monitor-stun.exe";
    root.join(name)
}

fn process_alive(pid: u32) -> bool {
    #[cfg(unix)]
    {
        std::path::Path::new(&format!("/proc/{pid}")).exists()
    }
    #[cfg(windows)]
    {
        std::process::Command::new("tasklist")
            .args(["/FI", &format!("PID eq {pid}"), "/NH"])
            .output()
            .map(|o| String::from_utf8_lossy(&o.stdout).contains(&pid.to_string()))
            .unwrap_or(false)
    }
}

fn sha256_hex(data: &[u8]) -> String {
    use sha2::{Digest, Sha256};
    let mut h = Sha256::new();
    h.update(data);
    hex::encode(h.finalize())
}

async fn bootstrap_stun(request: &Request) -> Result<String, String> {
    if request.stun_asset_url.is_empty() {
        return Err("bootstrap_stun 需要 stun_asset_url".to_string());
    }
    // 已运行则直接返回（幂等）
    if let Some(entry) = stun_entry().lock().unwrap().as_ref() {
        if process_alive(entry.pid) {
            return Ok(serde_json::json!({"status":"running","port":entry.port,"pid":entry.pid}).to_string());
        }
    }
    let root = stun_root();
    std::fs::create_dir_all(&root).map_err(|e| format!("创建 stun 目录: {e}"))?;
    let binary = stun_binary_path();
    if !binary.exists() {
        let tmp = root.join("api-monitor-stun.tmp");
        let resp = reqwest::get(&request.stun_asset_url)
            .await
            .map_err(|e| format!("下载 stun 二进制失败: {e}"))?;
        let bytes = resp.bytes().await.map_err(|e| format!("读取 stun 二进制失败: {e}"))?;
        if !request.stun_asset_sha256.is_empty()
            && sha256_hex(&bytes) != request.stun_asset_sha256.to_ascii_lowercase()
        {
            return Err("api-monitor-stun SHA-256 校验失败".to_string());
        }
        std::fs::write(&tmp, &bytes).map_err(|e| format!("写 stun 二进制: {e}"))?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let _ = std::fs::set_permissions(&tmp, std::fs::Permissions::from_mode(0o755));
        }
        std::fs::rename(&tmp, &binary).map_err(|e| format!("激活 stun 二进制: {e}"))?;
    }

    let port = if request.stun_port == 0 { 3478 } else { request.stun_port };
    let log_file = root.join("stun.log");
    let log = std::fs::OpenOptions::new().create(true).append(true).open(&log_file)
        .map_err(|e| format!("打开 stun 日志: {e}"))?;
    let mut cmd = std::process::Command::new(&binary);
    cmd.args(["-listen", &format!("0.0.0.0:{port}")])
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::from(log.try_clone().map_err(|e| format!("clone log: {e}"))?))
        .stderr(std::process::Stdio::from(log));
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x08000000);
    }
    let child = cmd.spawn().map_err(|e| format!("启动 stun 服务: {e}"))?;
    let pid = child.id();
    std::fs::write(root.join("stun.pid"), pid.to_string()).map_err(|e| format!("写 pid: {e}"))?;
    *stun_entry().lock().unwrap() = Some(StunEntry { port, pid, started_at: Instant::now() });
    Ok(serde_json::json!({"status":"running","port":port,"pid":pid}).to_string())
}

async fn stun_status() -> Result<String, String> {
    match stun_entry().lock().unwrap().as_ref() {
        Some(entry) if process_alive(entry.pid) => Ok(serde_json::json!({
            "status":"running","port":entry.port,"pid":entry.pid,
            "uptime_seconds": entry.started_at.elapsed().as_secs(),
        }).to_string()),
        _ => Ok(serde_json::json!({"status":"stopped"}).to_string()),
    }
}

async fn stun_stop() -> Result<String, String> {
    if let Some(entry) = stun_entry().lock().unwrap().take() {
        kill_process(entry.pid);
    }
    Ok(serde_json::json!({"status":"stopped"}).to_string())
}

fn kill_process(pid: u32) {
    #[cfg(unix)]
    {
        let _ = std::process::Command::new("kill").args([&pid.to_string()]).status();
    }
    #[cfg(windows)]
    {
        let _ = std::process::Command::new("taskkill").args(["/F", "/PID", &pid.to_string()]).status();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // 打洞握手编解码往返
    #[test]
    fn hole_roundtrip() {
        let frame = build_hole(0xDEAD_BEEF, 0x1122_3344_5566_7788, HOLE_PING);
        let (sid, pid, t) = parse_hole(&frame).unwrap();
        assert_eq!(sid, 0xDEAD_BEEF);
        assert_eq!(pid, 0x1122_3344_5566_7788);
        assert_eq!(t, HOLE_PING);
    }

    #[test]
    fn data_frame_parse() {
        let mut f = vec![FRAME_UDP_DATA];
        f.extend_from_slice(&9u16.to_be_bytes());
        f.extend_from_slice(&(3u32).to_be_bytes());
        f.extend_from_slice(b"abc");
        let (typ, cid, payload) = parse_frame(&f).unwrap();
        assert_eq!(typ, FRAME_UDP_DATA);
        assert_eq!(cid, 9);
        assert_eq!(payload, b"abc");
    }

    // 环回打洞：两个 UDP socket 互为候选，应建立直连并桥接 UDP echo
    #[tokio::test]
    async fn two_agents_punch_and_bridge() {
        let a_sock = Arc::new(UdpSocket::bind((Ipv4Addr::LOCALHOST, 0)).await.unwrap());
        let b_sock = Arc::new(UdpSocket::bind((Ipv4Addr::LOCALHOST, 0)).await.unwrap());
        let a_addr = a_sock.local_addr().unwrap();
        let b_addr = b_sock.local_addr().unwrap();

        // 本地 UDP echo 服务（模拟源主机 local_service）
        let echo = UdpSocket::bind((Ipv4Addr::LOCALHOST, 0)).await.unwrap();
        let echo_addr = echo.local_addr().unwrap();
        let echo = Arc::new(echo);
        {
            let echo = echo.clone();
            tokio::spawn(async move {
                let mut b = vec![0u8; 65535];
                loop {
                    match echo.recv_from(&mut b).await {
                        Ok((n, src)) => { let _ = echo.send_to(&b[..n], src).await; }
                        Err(_) => break,
                    }
                }
            });
        }

        let sid = 42u32;
        let pid_a = 100u64;
        let pid_b = 200u64;
        let state_a = Arc::new(AtomicU8::new(STATE_NEGOTIATING));
        let state_b = Arc::new(AtomicU8::new(STATE_NEGOTIATING));
        let peer_a = Arc::new(Mutex::new(None));
        let peer_b = Arc::new(Mutex::new(None));

        // A、B 互为候选
        let (cancel_tx_a, cancel_rx_a) = oneshot::channel::<()>();
        let run_a = run_punch(
            a_sock.clone(), sid, pid_a, vec![b_addr],
            "127.0.0.1".into(), echo_addr.port(), state_a.clone(), peer_a.clone(), cancel_rx_a,
        );
        let (cancel_tx_b, cancel_rx_b) = oneshot::channel::<()>();
        let run_b = run_punch(
            b_sock.clone(), sid, pid_b, vec![a_addr],
            "127.0.0.1".into(), echo_addr.port(), state_b.clone(), peer_b.clone(), cancel_rx_b,
        );
        // Sender 存活期间会话不会被误取消；结束前显式停掉会话
        let _cancel_tx_a = cancel_tx_a;
        let _cancel_tx_b = cancel_tx_b;
        let h_a = tokio::spawn(run_a);
        let h_b = tokio::spawn(run_b);

        // 等待双方进入 DIRECT
        let deadline = Instant::now() + Duration::from_secs(5);
        while Instant::now() < deadline {
            if state_a.load(Ordering::Relaxed) == STATE_DIRECT
                && state_b.load(Ordering::Relaxed) == STATE_DIRECT
            {
                break;
            }
            tokio::time::sleep(Duration::from_millis(100)).await;
        }
        assert_eq!(state_a.load(Ordering::Relaxed), STATE_DIRECT, "A should be DIRECT");
        assert_eq!(state_b.load(Ordering::Relaxed), STATE_DIRECT, "B should be DIRECT");

        // A 向 B 的 peer 地址发一个 UDP_DATA 帧 → B 桥接到 echo → 回包帧回 A
        let conn_id = 7u16;
        let mut frame = vec![FRAME_UDP_DATA];
        frame.extend_from_slice(&conn_id.to_be_bytes());
        frame.extend_from_slice(&(3u32).to_be_bytes());
        frame.extend_from_slice(b"p2p");

        // 用 loopback 直连模拟：A 直接发到 B 的 peer 地址（B 已记为 A）
        let b_peer = peer_b.lock().unwrap().unwrap();
        let _ = a_sock.send_to(&frame, b_peer).await;

        // 等待 A 侧收到 echo 回包
        let mut buf = [0u8; 65535];
        let deadline2 = Instant::now() + Duration::from_secs(3);
        let mut got = false;
        while Instant::now() < deadline2 {
            match timeout(Duration::from_millis(300), a_sock.recv_from(&mut buf)).await {
                Ok(Ok((n, _))) => {
                    let Some((typ, cid, payload)) = parse_frame(&buf[..n]) else { continue };
                    if typ == FRAME_UDP_DATA && cid == conn_id {
                        assert_eq!(payload, b"p2p");
                        got = true;
                        break;
                    }
                }
                _ => {}
            }
        }
        assert!(got, "A should receive echoed UDP_DATA frame");

        // 会话因直连建立后不会 fallback 退出；直接取消释放
        h_a.abort();
        h_b.abort();
    }
}
