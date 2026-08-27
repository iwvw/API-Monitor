use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::tcp::{OwnedReadHalf, OwnedWriteHalf};
use tokio::net::TcpStream;
use tokio::sync::{oneshot, Mutex as AsyncMutex};

// ==================== 复用隧道协议 ====================
// 面板 task 53。中继端为 backend-go/cmd/api-monitor-relay。
// 帧：[1B type][2B conn_id BE][4B 载荷长度 BE][载荷]
//   type 0x01=数据, 0x02=关闭该 conn, 0x03=保活（双方消化，不透传）。
// conn_id 由中继（连接发起/客户端侧）分配，本机镜像使用；本机为每个 conn
// 维护到 local_host:local_port 的本地连接。

mod frame {
    pub const DATA: u8 = 0x01;
    pub const CLOSE: u8 = 0x02;
    pub const KEEPALIVE: u8 = 0x03;
    pub const MAX_PAYLOAD: usize = 1 << 20;
    pub const HDR_LEN: usize = 7;
}

#[derive(Debug, Clone, Deserialize)]
struct Request {
    operation: String,
    #[serde(default)]
    forward_id: String,
    #[serde(default)]
    relay_host: String,
    #[serde(default)]
    relay_port: u16,
    #[serde(default)]
    local_host: String,
    #[serde(default)]
    local_port: u16,
    #[serde(default)]
    relay_asset_url: String,
    #[serde(default)]
    relay_asset_sha256: String,
    #[serde(default)]
    token: String,
    #[serde(default)]
    proxy_port: u16,
}

#[derive(Debug, Serialize)]
struct Status {
    forward_id: String,
    connected: bool,
    connector_count: usize,
    uptime_seconds: u64,
}

static TUNNEL_MGR: OnceLock<TunnelManager> = OnceLock::new();

fn tunnel_manager() -> &'static TunnelManager {
    TUNNEL_MGR.get_or_init(TunnelManager::new)
}

struct TunnelEntry {
    forward_id: String,
    local_host: String,
    local_port: u16,
    relay_host: String,
    relay_port: u16,
    active: AtomicBool,
    connected: AtomicBool,
    started_at: Mutex<Option<Instant>>,
    writer: AsyncMutex<Option<OwnedWriteHalf>>,
    conns: AsyncMutex<HashMap<u16, OwnedWriteHalf>>,
    control: Mutex<Option<oneshot::Sender<()>>>,
}

struct TunnelManager {
    tunnels: Mutex<HashMap<String, Arc<TunnelEntry>>>,
}

impl TunnelManager {
    fn new() -> Self {
        TunnelManager { tunnels: Mutex::new(HashMap::new()) }
    }

    fn register(&self, forward_id: String, entry: Arc<TunnelEntry>) {
        self.tunnels.lock().unwrap().insert(forward_id, entry);
    }

    fn unregister(&self, forward_id: &str) -> Option<Arc<TunnelEntry>> {
        self.tunnels.lock().unwrap().remove(forward_id)
    }

    fn get(&self, forward_id: &str) -> Option<Arc<TunnelEntry>> {
        self.tunnels.lock().unwrap().get(forward_id).cloned()
    }
}

impl TunnelEntry {
    fn new(request: &Request) -> Self {
        TunnelEntry {
            forward_id: request.forward_id.clone(),
            local_host: request.local_host.clone(),
            local_port: request.local_port,
            relay_host: request.relay_host.clone(),
            relay_port: request.relay_port,
            active: AtomicBool::new(true),
            connected: AtomicBool::new(false),
            started_at: Mutex::new(None),
            writer: AsyncMutex::new(None),
            conns: AsyncMutex::new(HashMap::new()),
            control: Mutex::new(None),
        }
    }

    async fn write_tunnel_frame(&self, typ: u8, conn_id: u16, payload: &[u8]) -> std::io::Result<()> {
        let mut guard = self.writer.lock().await;
        match guard.as_mut() {
            Some(w) => write_frame(w, typ, conn_id, payload).await,
            None => Err(std::io::Error::new(std::io::ErrorKind::NotConnected, "tunnel writer not set")),
        }
    }

    async fn put_conn(&self, conn_id: u16, write_half: OwnedWriteHalf) {
        self.conns.lock().await.insert(conn_id, write_half);
    }

    async fn remove_conn(&self, conn_id: u16) {
        self.conns.lock().await.remove(&conn_id);
    }

    async fn write_local(&self, conn_id: u16, payload: &[u8]) {
        let mut map = self.conns.lock().await;
        if let Some(w) = map.get_mut(&conn_id) {
            let _ = w.write_all(payload).await;
        }
    }

    async fn close_all_conns(&self) {
        let mut map = self.conns.lock().await;
        map.clear();
    }
}

async fn write_frame(w: &mut OwnedWriteHalf, typ: u8, conn_id: u16, payload: &[u8]) -> std::io::Result<()> {
    let mut hdr = [0u8; frame::HDR_LEN];
    hdr[0] = typ;
    hdr[1..3].copy_from_slice(&conn_id.to_be_bytes());
    hdr[3..7].copy_from_slice(&(payload.len() as u32).to_be_bytes());
    w.write_all(&hdr).await?;
    if !payload.is_empty() {
        w.write_all(payload).await?;
    }
    Ok(())
}

async fn read_frame(r: &mut OwnedReadHalf) -> std::io::Result<(u8, u16, Vec<u8>)> {
    let mut hdr = [0u8; frame::HDR_LEN];
    r.read_exact(&mut hdr).await?;
    let typ = hdr[0];
    let conn_id = u16::from_be_bytes([hdr[1], hdr[2]]);
    let len = u32::from_be_bytes([hdr[3], hdr[4], hdr[5], hdr[6]]) as usize;
    if len > frame::MAX_PAYLOAD {
        return Err(std::io::Error::new(std::io::ErrorKind::InvalidData, format!("frame payload too large: {len}")));
    }
    let mut payload = vec![0u8; len];
    if len > 0 {
        r.read_exact(&mut payload).await?;
    }
    Ok((typ, conn_id, payload))
}

// connect_tunnel 建立反向隧道并完成握手（面板要求的转发协议头）。
async fn connect_tunnel(entry: &Arc<TunnelEntry>) -> Result<TcpStream, String> {
    let addr = format!("{}:{}", entry.relay_host, entry.relay_port);
    let stream = tokio::time::timeout(Duration::from_secs(10), TcpStream::connect(&addr))
        .await
        .map_err(|_| format!("connect to relay {addr} timed out"))?
        .map_err(|e| format!("connect to relay {addr}: {e}"))?;

    let id_bytes = entry.forward_id.as_bytes();
    let len = (id_bytes.len() as u32).to_be_bytes();
    let mut header = len.to_vec();
    header.extend_from_slice(id_bytes);

    let (mut read_half, mut write_half) = stream.into_split();
    write_half.write_all(&header).await.map_err(|e| format!("send forward_id header: {e}"))?;
    let mut ack = [0u8; 4];
    read_half.read_exact(&mut ack).await.map_err(|e| format!("read relay ack: {e}"))?;
    let status = u32::from_be_bytes(ack);
    if status != 0 {
        return Err(format!("relay rejected forward_id: status={status}"));
    }
    // 还原完整 TcpStream 交给会话循环
    let stream = read_half.reunite(write_half).map_err(|e| format!("reunite stream: {e}"))?;
    Ok(stream)
}

// run_read_loop 读取隧道帧并分发；隧道断开或收到控制信号即返回。
async fn run_read_loop(entry: Arc<TunnelEntry>, mut reader: OwnedReadHalf, mut control_rx: oneshot::Receiver<()>) {
    loop {
        tokio::select! {
            _ = &mut control_rx => break,
            frame = read_frame(&mut reader) => {
                match frame {
                    Ok((typ, conn_id, payload)) => {
                        match typ {
                            frame::DATA => dispatch_data(&entry, conn_id, payload).await,
                            frame::CLOSE => {
                                entry.remove_conn(conn_id).await;
                            }
                            frame::KEEPALIVE => {}
                            other => eprintln!("[tcp_forwarder] {} weird frame type {other}", entry.forward_id),
                        }
                    }
                    Err(_) => break,
                }
            }
        }
    }
    entry.connected.store(false, Ordering::Relaxed);
    entry.close_all_conns().await;
}

// dispatch_data 把中继数据投递给本地连接；无连接时按需建立到本地服务的连接。
async fn dispatch_data(entry: &Arc<TunnelEntry>, conn_id: u16, payload: Vec<u8>) {
    {
        let guard = entry.conns.lock().await;
        if guard.contains_key(&conn_id) {
            drop(guard);
            entry.write_local(conn_id, &payload).await;
            return;
        }
    }

    let target = format!("{}:{}", entry.local_host, entry.local_port);
    match tokio::time::timeout(Duration::from_secs(5), TcpStream::connect(&target)).await {
        Ok(Ok(local)) => {
            let (local_read, local_write) = local.into_split();
            entry.put_conn(conn_id, local_write).await;
            if !payload.is_empty() {
                entry.write_local(conn_id, &payload).await;
            }
            let entry_clone = entry.clone();
            tokio::spawn(async move {
                local_to_tunnel(entry_clone, conn_id, local_read).await;
            });
        }
        _ => {
            eprintln!("[tcp_forwarder] forward {}: cannot reach {target}, dropping conn {conn_id}", entry.forward_id);
            let _ = entry.write_tunnel_frame(frame::CLOSE, conn_id, &[]).await;
        }
    }
}

// local_to_tunnel 本地服务 → 中继：读本地字节流并帧化上送。
async fn local_to_tunnel(entry: Arc<TunnelEntry>, conn_id: u16, mut local_read: OwnedReadHalf) {
    let mut buf = vec![0u8; 32 * 1024];
    loop {
        match local_read.read(&mut buf).await {
            Ok(0) | Err(_) => break,
            Ok(n) => {
                if entry.write_tunnel_frame(frame::DATA, conn_id, &buf[..n]).await.is_err() {
                    break;
                }
            }
        }
    }
    entry.remove_conn(conn_id).await;
    let _ = entry.write_tunnel_frame(frame::CLOSE, conn_id, &[]).await;
}

// keepalive 每 30s 发一帧保活，防止 NAT/中间盒回收隧道。
async fn keepalive_loop(entry: Arc<TunnelEntry>) {
    loop {
        tokio::time::sleep(Duration::from_secs(30)).await;
        if !entry.connected.load(Ordering::Relaxed) {
            return;
        }
        if entry.write_tunnel_frame(frame::KEEPALIVE, 0, &[]).await.is_err() {
            return;
        }
    }
}

// supervisor 维护隧道生命周期：首次握手成功后立刻回报面板，随后断开时指数退避自动重连。
async fn supervisor(entry: Arc<TunnelEntry>, mut first_result: Option<oneshot::Sender<Result<(), String>>>) {
    let mut backoff = Duration::from_secs(1);
    loop {
        if !entry.active.load(Ordering::Relaxed) {
            return;
        }
        match connect_tunnel(&entry).await {
            Ok(stream) => {
                let mut ended_rx = start_session(&entry, stream).await;
                if let Some(tx) = first_result.take() {
                    let _ = tx.send(Ok(()));
                }
                // 等待会话结束（隧道断开/移除信号）；同时每 250ms 复查是否被移除。
                let mut watchdog = tokio::time::interval(Duration::from_millis(250));
                loop {
                    tokio::select! {
                        _ = &mut ended_rx => break,
                        _ = watchdog.tick() => {
                            if !entry.active.load(Ordering::Relaxed) {
                                break;
                            }
                        }
                    }
                }
                end_session(&entry).await;
                backoff = Duration::from_secs(1);
            }
            Err(e) => {
                if let Some(tx) = first_result.take() {
                    let _ = tx.send(Err(e));
                }
                tokio::time::sleep(backoff).await;
                backoff = (backoff * 2).min(Duration::from_secs(30));
            }
        }
        if !entry.active.load(Ordering::Relaxed) {
            return;
        }
    }
}

// start_session 基于已握手的 stream 启动会话（读循环+保活），返回会话结束信号。
async fn start_session(entry: &Arc<TunnelEntry>, stream: TcpStream) -> oneshot::Receiver<()> {
    let (reader, writer) = stream.into_split();
    *entry.writer.lock().await = Some(writer);
    entry.connected.store(true, Ordering::Relaxed);
    *entry.started_at.lock().unwrap() = Some(Instant::now());

    let (ctl_tx, ctl_rx) = oneshot::channel();
    *entry.control.lock().unwrap() = Some(ctl_tx);

    let (ended_tx, ended_rx) = oneshot::channel();
    let entry_clone = entry.clone();
    tokio::spawn(async move {
        run_read_loop(entry_clone, reader, ctl_rx).await;
        let _ = ended_tx.send(());
    });
    tokio::spawn(keepalive_loop(entry.clone()));
    ended_rx
}

// end_session 清理会话残留状态（幂等）。
async fn end_session(entry: &Arc<TunnelEntry>) {
    entry.connected.store(false, Ordering::Relaxed);
    *entry.writer.lock().await = None;
    *entry.control.lock().unwrap() = None;
    entry.close_all_conns().await;
}

async fn install(request: &Request) -> Result<String, String> {
    if tunnel_manager().get(&request.forward_id).is_some() {
        return Ok(serde_json::json!({"status":"connected","forward_id":request.forward_id}).to_string());
    }
    let entry = Arc::new(TunnelEntry::new(request));
    tunnel_manager().register(request.forward_id.clone(), entry.clone());

    let (ok_tx, ok_rx) = oneshot::channel();
    tokio::spawn(supervisor(entry.clone(), Some(ok_tx)));

    match tokio::time::timeout(Duration::from_secs(15), ok_rx).await {
        Ok(Ok(Ok(()))) => Ok(serde_json::json!({"status":"connected","forward_id":request.forward_id}).to_string()),
        Ok(Ok(Err(e))) => {
            tunnel_manager().unregister(&request.forward_id);
            Err(e)
        }
        Ok(Err(_)) => {
            tunnel_manager().unregister(&request.forward_id);
            Err("隧道 supervisor 意外退出".to_string())
        }
        Err(_) => {
            entry.active.store(false, Ordering::Relaxed);
            if let Some(tx) = entry.control.lock().unwrap().take() {
                let _ = tx.send(());
            }
            tunnel_manager().unregister(&request.forward_id);
            Err("建立隧道超时".to_string())
        }
    }
}

async fn remove(forward_id: &str) -> Result<String, String> {
    if let Some(entry) = tunnel_manager().unregister(forward_id) {
        entry.active.store(false, Ordering::Relaxed);
        if let Some(tx) = entry.control.lock().unwrap().take() {
            let _ = tx.send(());
        }
        entry.close_all_conns().await;
    }
    Ok(serde_json::json!({"status":"removed","forward_id":forward_id}).to_string())
}

async fn status(forward_id: &str) -> Result<String, String> {
    let connected;
    let connector_count;
    let uptime_seconds;
    if let Some(entry) = tunnel_manager().get(forward_id) {
        connected = entry.connected.load(Ordering::Relaxed);
        connector_count = entry.conns.lock().await.len();
        uptime_seconds = entry.started_at.lock().unwrap().map(|t| t.elapsed().as_secs()).unwrap_or(0);
    } else {
        connected = false;
        connector_count = 0;
        uptime_seconds = 0;
    }
    Ok(serde_json::json!(Status {
        forward_id: forward_id.to_string(),
        connected,
        connector_count,
        uptime_seconds,
    }).to_string())
}

// ==================== 入口主机 Agent：中继监听配置 ====================

fn relay_admin_endpoint() -> String {
    std::env::var("API_MONITOR_RELAY_ADDR").unwrap_or_else(|_| "127.0.0.1:18080".into())
}

fn relay_admin_token() -> String {
    std::env::var("API_MONITOR_RELAY_TOKEN").unwrap_or_default()
}

async fn relay_request(
    method: reqwest::Method,
    path: &str,
    body: Option<serde_json::Value>,
) -> Result<serde_json::Value, String> {
    let base = relay_admin_endpoint();
    let url = format!("http://{base}{path}");
    let client = reqwest::Client::new();
    let mut builder = client.request(method, &url);
    let token = relay_admin_token();
    if !token.is_empty() {
        builder = builder.bearer_auth(&token);
    }
    if let Some(b) = body {
        builder = builder.json(&b);
    }
    let resp = builder.send().await.map_err(|e| format!("relay admin {}: {e}", url))?;
    let status = resp.status();
    let text = resp.text().await.unwrap_or_default();
    if !status.is_success() {
        return Err(format!("relay admin {} -> {status}: {text}", url));
    }
    serde_json::from_str(&text).map_err(|e| format!("relay admin parse: {e}"))
}

// listen 在入口主机的中继器上注册公开监听端口并放行防火墙。
async fn listen(request: &Request) -> Result<String, String> {
    let port = request.relay_port;
    if request.forward_id.is_empty() || port == 0 {
        return Err("listen 需要 forward_id 与 relay_port".to_string());
    }
    let body = serde_json::json!({"id": request.forward_id, "listen_port": port, "token": request.token});
    relay_request(reqwest::Method::POST, "/forwards", Some(body)).await?;
    #[cfg(unix)]
    let _ = crate::proxy_runtime::ensure_firewall_port(port, "tcp");
    Ok(serde_json::json!({"status":"listening","forward_id":request.forward_id,"port":port}).to_string())
}

// unlisten 关闭入口主机的监听端口并移除防火墙规则。
async fn unlisten(request: &Request) -> Result<String, String> {
    if request.forward_id.is_empty() {
        return Err("unlisten 需要 forward_id".to_string());
    }
    // forward_id 仅含 [a-z0-9_]，可直接放入路径
    let path = format!("/forwards/{}", request.forward_id);
    let _ = relay_request(reqwest::Method::DELETE, &path, None).await;
    #[cfg(unix)]
    {
        if request.relay_port > 0 {
            crate::proxy_runtime::remove_firewall_port(request.relay_port, "tcp");
        }
    }
    Ok(serde_json::json!({"status":"unlistened","forward_id":request.forward_id}).to_string())
}

// ==================== 中继入口自举（默认安装 relay） ====================
// 让任意主机都能作为中继入口：面板在部署前先发 bootstrap_relay（幂等，已运行则跳过）。

#[cfg(unix)]
fn relay_systemd_unit(binary: &std::path::Path) -> String {
    format!(
        "[Unit]\nDescription=API Monitor TCP relay\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart={} -listen 127.0.0.1:18080 -config /etc/api-monitor-relay/config.json\nRestart=always\nRestartSec=5s\nLimitNOFILE=1048576\n\n[Install]\nWantedBy=multi-user.target\n",
        binary.display()
    )
}

#[cfg(unix)]
fn relay_cmd_run(command: &mut std::process::Command, label: &str) -> Result<(), String> {
    let output = command
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .output()
        .map_err(|err| format!("{label}: {err}"))?;
    if output.status.success() {
        Ok(())
    } else {
        Err(format!("{label}: {}", String::from_utf8_lossy(&output.stderr).trim()))
    }
}

#[cfg(unix)]
async fn bootstrap_relay(request: &Request) -> Result<String, String> {
    use std::os::unix::fs::PermissionsExt;

    // 幂等：已运行直接返回
    let active = std::process::Command::new("systemctl")
        .args(["is-active", "--quiet", "api-monitor-relay.service"])
        .status()
        .is_ok_and(|s| s.success());
    if active {
        return Ok(serde_json::json!({"status":"already_running"}).to_string());
    }
    let url = request.relay_asset_url.trim();
    let sha = request.relay_asset_sha256.trim();
    if !url.starts_with("https://") || sha.len() != 64 || !sha.bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err("relay 资产必须为 HTTPS 地址并带 SHA-256".to_string());
    }
    let dir = std::path::Path::new("/opt/api-monitor-relay");
    std::fs::create_dir_all(dir).map_err(|e| format!("创建 relay 目录失败: {e}"))?;
    let binary = dir.join("api-monitor-relay");
    let candidate = dir.join(".api-monitor-relay.download");
    let _ = std::fs::remove_file(&candidate);
    relay_cmd_run(
        std::process::Command::new("curl").args(["--fail", "--location", "--retry", "3", "--proto", "=https", "--output"]).arg(&candidate).arg(url),
        "下载 api-monitor-relay",
    )?;
    let sum = std::process::Command::new("sha256sum").arg(&candidate).output()
        .map_err(|e| format!("sha256sum: {e}"))?;
    let actual = String::from_utf8_lossy(&sum.stdout).split_whitespace().next().unwrap_or("").to_ascii_lowercase();
    if !sum.status.success() || actual != sha.to_ascii_lowercase() {
        let _ = std::fs::remove_file(&candidate);
        return Err("api-monitor-relay SHA-256 校验失败".to_string());
    }
    std::fs::set_permissions(&candidate, std::fs::Permissions::from_mode(0o755))
        .map_err(|e| format!("chmod relay: {e}"))?;
    std::fs::rename(&candidate, &binary).map_err(|e| format!("激活 relay 二进制: {e}"))?;

    std::fs::create_dir_all("/etc/api-monitor-relay").map_err(|e| format!("创建 relay 配置目录: {e}"))?;
    std::fs::write("/etc/api-monitor-relay/config.json", "{\"forwards\":[]}").map_err(|e| format!("写 relay 配置: {e}"))?;
    std::fs::write("/etc/systemd/system/api-monitor-relay.service", relay_systemd_unit(&binary))
        .map_err(|e| format!("写 systemd 单元: {e}"))?;
    relay_cmd_run(std::process::Command::new("systemctl").args(["daemon-reload"]), "systemctl daemon-reload")?;
    relay_cmd_run(std::process::Command::new("systemctl").args(["enable", "--now", "api-monitor-relay.service"]), "enable relay")?;
    relay_cmd_run(std::process::Command::new("systemctl").args(["restart", "api-monitor-relay.service"]), "restart relay")?;
    relay_cmd_run(std::process::Command::new("systemctl").args(["is-active", "--quiet", "api-monitor-relay.service"]), "校验 relay 服务")?;
    Ok(serde_json::json!({"status":"running"}).to_string())
}

#[cfg(windows)]
fn relay_windows_root() -> std::path::PathBuf {
    std::env::var_os("ProgramData")
        .map(std::path::PathBuf::from)
        .unwrap_or_else(|| std::path::PathBuf::from("C:\\ProgramData"))
        .join("api-monitor-relay")
}

#[cfg(windows)]
async fn bootstrap_relay(request: &Request) -> Result<String, String> {
    use std::os::windows::process::CommandExt;

    let root = relay_windows_root();
    let pid_file = root.join("relay.pid");
    let running = std::fs::read_to_string(&pid_file)
        .ok().and_then(|t| t.trim().parse::<u32>().ok())
        .map(|pid| {
            std::process::Command::new("tasklist").args(["/FI", &format!("PID eq {pid}"), "/NH"]).output()
                .map(|o| String::from_utf8_lossy(&o.stdout).contains(&pid.to_string()))
                .unwrap_or(false)
        })
        .unwrap_or(false);
    if running {
        return Ok(serde_json::json!({"status":"already_running"}).to_string());
    }

    let url = request.relay_asset_url.trim();
    let sha = request.relay_asset_sha256.trim();
    if !url.starts_with("https://") || sha.len() != 64 || !sha.bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err("relay 资产必须为 HTTPS 地址并带 SHA-256".to_string());
    }
    std::fs::create_dir_all(&root).map_err(|e| format!("创建 relay 目录失败: {e}"))?;
    let binary = root.join("api-monitor-relay.exe");
    let candidate = root.join(".api-monitor-relay.download.exe");
    let _ = std::fs::remove_file(&candidate);
    let script = root.join("download-relay.ps1");
    std::fs::write(&script, "param([string]$Uri,[string]$OutFile)\n$ErrorActionPreference='Stop'\ntry {\n  Invoke-WebRequest -Uri $Uri -OutFile $OutFile -TimeoutSec 300 -UseBasicParsing -MaximumRedirection 5\n  exit 0\n} catch {\n  Write-Error $_.Exception.Message\n  exit 1\n}\n")
        .map_err(|e| format!("写下载脚本: {e}"))?;
    let st = std::process::Command::new("powershell")
        .args(["-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File"]).arg(&script)
        .arg("-Uri").arg(url)
        .arg("-OutFile").arg(&candidate)
        .status().map_err(|e| format!("下载 api-monitor-relay: {e}"))?;
    if !st.success() {
        let _ = std::fs::remove_file(&candidate);
        return Err("api-monitor-relay 下载失败".to_string());
    }
    // certutil 校验 SHA-256
    let sum = std::process::Command::new("certutil").args(["-hashfile"]).arg(&candidate).arg("SHA256").output()
        .map_err(|e| format!("certutil: {e}"))?;
    let sum_text = String::from_utf8_lossy(&sum.stdout).into_owned();
    let mut actual = String::new();
    for line in sum_text.lines() {
        let t = line.trim();
        if t.len() == 64 && t.bytes().all(|b| b.is_ascii_hexdigit()) {
            actual = t.to_ascii_lowercase();
            break;
        }
    }
    if !sum.status.success() || actual != sha.to_ascii_lowercase() {
        let _ = std::fs::remove_file(&candidate);
        return Err("api-monitor-relay SHA-256 校验失败".to_string());
    }
    std::fs::rename(&candidate, &binary).map_err(|e| format!("激活 relay 二进制: {e}"))?;

    let log_file = root.join("relay.log");
    let log = std::fs::OpenOptions::new().create(true).append(true).open(&log_file)
        .map_err(|e| format!("打开 relay 日志: {e}"))?;
    let child = std::process::Command::new(&binary)
        .args(["-listen", "127.0.0.1:18080", "-config"])
        .arg(root.join("config.json"))
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::from(log.try_clone().map_err(|e| format!("clone log: {e}"))?))
        .stderr(std::process::Stdio::from(log))
        .creation_flags(0x08000000)
        .spawn()
        .map_err(|e| format!("启动 relay: {e}"))?;
    std::fs::write(&pid_file, child.id().to_string()).map_err(|e| format!("写 pid: {e}"))?;
    // 开机自启（尽力而为）
    let tr = format!("\\\"{}\\\" -listen 127.0.0.1:18080 -config \\\"{}\\\"", binary.display().to_string().replace('\\', "\\\\"), root.join("config.json").display().to_string().replace('\\', "\\\\"));
    let _ = std::process::Command::new("schtasks")
        .args(["/Create", "/TN", "api-monitor-relay", "/TR", &tr, "/SC", "ONSTART", "/RU", "SYSTEM", "/RL", "HIGHEST", "/F"])
        .status();
    Ok(serde_json::json!({"status":"running"}).to_string())
}

// ==================== 鉴权代理（CF 隧道 token 转发，源主机侧） ====================
// 面板对 CF+token 转发部署时下发 auth_proxy_start：下载 api-monitor-auth-proxy 二进制
// 并托管在 127.0.0.1:<proxy_port>，cloudflared ingress 指向它；转发停止时 auth_proxy_stop 回收。

fn auth_proxy_root() -> std::path::PathBuf {
    #[cfg(unix)]
    {
        std::path::PathBuf::from("/opt/api-monitor-auth-proxy")
    }
    #[cfg(windows)]
    {
        std::env::var_os("ProgramData")
            .map(std::path::PathBuf::from)
            .unwrap_or_else(|| std::path::PathBuf::from("C:\\ProgramData"))
            .join("api-monitor-auth-proxy")
    }
}

struct AuthProxyEntry {
    port: u16,
    pid: u32,
}

static AUTH_PROXIES: OnceLock<std::sync::Mutex<HashMap<String, AuthProxyEntry>>> = OnceLock::new();

fn auth_proxies() -> &'static std::sync::Mutex<HashMap<String, AuthProxyEntry>> {
    AUTH_PROXIES.get_or_init(|| std::sync::Mutex::new(HashMap::new()))
}

fn pid_alive(pid: u32) -> bool {
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

async fn download_auth_proxy(url: &str, sha: &str) -> Result<std::path::PathBuf, String> {
    if !url.starts_with("https://") || sha.len() != 64 || !sha.bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err("auth-proxy 资产必须为 HTTPS 地址并带 SHA-256".to_string());
    }
    let root = auth_proxy_root();
    std::fs::create_dir_all(&root).map_err(|e| format!("创建 auth-proxy 目录失败: {e}"))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let binary = root.join("api-monitor-auth-proxy");
        let candidate = root.join(".api-monitor-auth-proxy.download");
        let _ = std::fs::remove_file(&candidate);
        let st = std::process::Command::new("curl")
            .args(["--fail", "--location", "--retry", "3", "--proto", "=https", "--output"]).arg(&candidate).arg(url)
            .status().map_err(|e| format!("curl: {e}"))?;
        if !st.success() {
            let _ = std::fs::remove_file(&candidate);
            return Err("下载 api-monitor-auth-proxy 失败".to_string());
        }
        let sum = std::process::Command::new("sha256sum").arg(&candidate).output()
            .map_err(|e| format!("sha256sum: {e}"))?;
        let actual = String::from_utf8_lossy(&sum.stdout).split_whitespace().next().unwrap_or("").to_ascii_lowercase();
        if !sum.status.success() || actual != sha.to_ascii_lowercase() {
            let _ = std::fs::remove_file(&candidate);
            return Err("api-monitor-auth-proxy SHA-256 校验失败".to_string());
        }
        std::fs::set_permissions(&candidate, std::fs::Permissions::from_mode(0o755)).map_err(|e| format!("chmod auth-proxy: {e}"))?;
        std::fs::rename(&candidate, &binary).map_err(|e| format!("激活 auth-proxy 二进制: {e}"))?;
        Ok(binary)
    }
    #[cfg(windows)]
    {
        let binary = root.join("api-monitor-auth-proxy.exe");
        let candidate = root.join(".api-monitor-auth-proxy.download.exe");
        let _ = std::fs::remove_file(&candidate);
        let script = root.join("download-auth-proxy.ps1");
        std::fs::write(&script, "param([string]$Uri,[string]$OutFile)\n$ErrorActionPreference='Stop'\ntry {\n  Invoke-WebRequest -Uri $Uri -OutFile $OutFile -TimeoutSec 300 -UseBasicParsing -MaximumRedirection 5\n  exit 0\n} catch {\n  Write-Error $_.Exception.Message\n  exit 1\n}\n")
            .map_err(|e| format!("写下载脚本: {e}"))?;
        let st = std::process::Command::new("powershell")
            .args(["-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File"]).arg(&script)
            .arg("-Uri").arg(url)
            .arg("-OutFile").arg(&candidate)
            .status().map_err(|e| format!("下载 api-monitor-auth-proxy: {e}"))?;
        if !st.success() {
            let _ = std::fs::remove_file(&candidate);
            return Err("api-monitor-auth-proxy 下载失败".to_string());
        }
        let sum = std::process::Command::new("certutil").args(["-hashfile"]).arg(&candidate).arg("SHA256").output()
            .map_err(|e| format!("certutil: {e}"))?;
        let mut actual = String::new();
        for line in String::from_utf8_lossy(&sum.stdout).lines() {
            let t = line.trim();
            if t.len() == 64 && t.bytes().all(|b| b.is_ascii_hexdigit()) {
                actual = t.to_ascii_lowercase();
                break;
            }
        }
        if !sum.status.success() || actual != sha.to_ascii_lowercase() {
            let _ = std::fs::remove_file(&candidate);
            return Err("api-monitor-auth-proxy SHA-256 校验失败".to_string());
        }
        std::fs::rename(&candidate, &binary).map_err(|e| format!("激活 auth-proxy 二进制: {e}"))?;
        Ok(binary)
    }
}

async fn auth_proxy_start(request: &Request) -> Result<String, String> {
    let port = request.proxy_port;
    if request.forward_id.is_empty() || port == 0 || request.token.is_empty() {
        return Err("auth_proxy_start 需要 forward_id、proxy_port 与 token".to_string());
    }
    // 幂等：内存或 pid 文件中存活的实例直接复用
    {
        let map = auth_proxies().lock().unwrap();
        if let Some(e) = map.get(&request.forward_id) {
            if e.port == port && pid_alive(e.pid) {
                return Ok(serde_json::json!({"status":"running","forward_id":request.forward_id,"port":e.port}).to_string());
            }
        }
    }
    let pid_file = auth_proxy_root().join(format!("{}.pid", request.forward_id));
    if let Ok(p) = std::fs::read_to_string(&pid_file) {
        if let Ok(pid) = p.trim().parse::<u32>() {
            if pid_alive(pid) {
                auth_proxies().lock().unwrap().insert(request.forward_id.clone(), AuthProxyEntry { port, pid });
                return Ok(serde_json::json!({"status":"running","forward_id":request.forward_id,"port":port}).to_string());
            }
        }
    }
    let binary = download_auth_proxy(&request.relay_asset_url, &request.relay_asset_sha256).await?;
    let log_file = auth_proxy_root().join(format!("{}.log", request.forward_id));
    let log = std::fs::OpenOptions::new().create(true).append(true).open(&log_file)
        .map_err(|e| format!("打开 auth-proxy 日志: {e}"))?;
    let upstream = format!("http://{}:{}", request.local_host, request.local_port);
    let mut cmd = std::process::Command::new(&binary);
    cmd.args(["-listen", &format!("127.0.0.1:{port}"), "-upstream", &upstream, "-token", &request.token]);
    cmd.stdin(std::process::Stdio::null());
    cmd.stdout(std::process::Stdio::from(log.try_clone().map_err(|e| format!("clone log: {e}"))?));
    cmd.stderr(std::process::Stdio::from(log));
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x08000000);
    }
    let child = cmd.spawn().map_err(|e| format!("启动 auth-proxy: {e}"))?;
    let pid = child.id();
    auth_proxies().lock().unwrap().insert(request.forward_id.clone(), AuthProxyEntry { port, pid });
    let _ = std::fs::write(&pid_file, pid.to_string());
    Ok(serde_json::json!({"status":"running","forward_id":request.forward_id,"port":port}).to_string())
}

async fn auth_proxy_stop(request: &Request) -> Result<String, String> {
    if let Some(e) = auth_proxies().lock().unwrap().remove(&request.forward_id) {
        #[cfg(unix)]
        let _ = std::process::Command::new("kill").args(["-9", &e.pid.to_string()]).status();
        #[cfg(windows)]
        let _ = std::process::Command::new("taskkill").args(["/F", "/PID", &e.pid.to_string()]).status();
    } else if let Ok(p) = std::fs::read_to_string(auth_proxy_root().join(format!("{}.pid", request.forward_id))) {
        if let Ok(pid) = p.trim().parse::<u32>() {
            #[cfg(unix)]
            let _ = std::process::Command::new("kill").args(["-9", &pid.to_string()]).status();
            #[cfg(windows)]
            let _ = std::process::Command::new("taskkill").args(["/F", "/PID", &pid.to_string()]).status();
        }
    }
    let _ = std::fs::remove_file(auth_proxy_root().join(format!("{}.pid", request.forward_id)));
    Ok(serde_json::json!({"status":"stopped","forward_id":request.forward_id}).to_string())
}

pub async fn reconcile(raw: &str) -> Result<String, String> {
    let request: Request = serde_json::from_str(raw).map_err(|e| format!("invalid tcp_forwarder request: {e}"))?;
    match request.operation.trim().to_ascii_lowercase().as_str() {
        "install" => install(&request).await,
        "remove" => remove(&request.forward_id).await,
        "status" => status(&request.forward_id).await,
        "listen" => listen(&request).await,
        "unlisten" => unlisten(&request).await,
        "bootstrap_relay" => bootstrap_relay(&request).await,
        "auth_proxy_start" => auth_proxy_start(&request).await,
        "auth_proxy_stop" => auth_proxy_stop(&request).await,
        other => Err(format!("unknown tcp_forwarder operation: {other}")),
    }
}


#[cfg(test)]
mod tests {
    use super::*;

    // 环回集成测试：假中继（与 Go api-monitor-relay 同协议）→ 源主机转发器 → 本地回显服务。
    #[tokio::test]
    async fn forwarder_bridges_to_local_service() {
        let relay_listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let relay_addr = relay_listener.local_addr().unwrap();
        let echo_listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let echo_addr = echo_listener.local_addr().unwrap();

        // 本地回显服务
        tokio::spawn(async move {
            loop {
                let Ok((mut sock, _)) = echo_listener.accept().await else { return };
                tokio::spawn(async move {
                    let mut buf = vec![0u8; 4096];
                    loop {
                        match sock.read(&mut buf).await {
                            Ok(0) | Err(_) => break,
                            Ok(n) => {
                                if sock.write_all(&buf[..n]).await.is_err() {
                                    break;
                                }
                            }
                        }
                    }
                });
            }
        });

        let fwd_id = "fwd_test_agent01".to_string();
        let request = Request {
            operation: "install".into(),
            forward_id: fwd_id.clone(),
            relay_host: "127.0.0.1".into(),
            relay_port: relay_addr.port(),
            local_host: "127.0.0.1".into(),
            local_port: echo_addr.port(),
            relay_asset_url: String::new(),
            relay_asset_sha256: String::new(),
            token: String::new(),
            proxy_port: 0,
        };
        let install_req = request.clone();
        let install_handle = tokio::spawn(async move { install(&install_req).await });

        // 假中继接受隧道：完成握手，然后模拟客户端投递 → 期待回显帧
        let (mut tun_read, mut tun_write) = {
            let (sock, _) = relay_listener.accept().await.unwrap();
            sock.into_split()
        };
        let mut len_bytes = [0u8; 4];
        tun_read.read_exact(&mut len_bytes).await.unwrap();
        let len = u32::from_be_bytes(len_bytes) as usize;
        let mut id_bytes = vec![0u8; len];
        tun_read.read_exact(&mut id_bytes).await.unwrap();
        assert_eq!(String::from_utf8_lossy(&id_bytes), fwd_id);
        tun_write.write_all(&[0, 0, 0, 0]).await.unwrap();

        let out = install_handle.await.unwrap().expect("install failed");
        assert!(out.contains("connected"), "install out: {out}");

        write_frame(&mut tun_write, frame::DATA, 7, b"hello from client").await.unwrap();
        let (typ, cid, payload) = read_frame(&mut tun_read).await.expect("no reply frame");
        assert_eq!(typ, frame::DATA, "expected DATA echo, got {typ} cid={cid}");
        assert_eq!(cid, 7);
        assert_eq!(payload, b"hello from client");

        // 客户端断开（CLOSE）应回传
        write_frame(&mut tun_write, frame::CLOSE, 7, &[]).await.unwrap();
        let (typ, cid, _) = read_frame(&mut tun_read).await.expect("no reset frame");
        assert!(
            typ == frame::CLOSE || typ == frame::DATA,
            "expected CLOSE or stray DATA, got {typ} cid={cid}"
        );

        remove(&fwd_id).await.unwrap();
        let st = status(&fwd_id).await.unwrap();
        assert!(!st.contains("\"connected\":true"), "tunnel should be gone after remove: {st}");
    }
}