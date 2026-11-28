//! 内嵌 TCP 中继（原 backend-go/cmd/api-monitor-relay）。
//!
//! 合并进 agent 二进制的理由：relay 与 agent 装在**同一台入口主机**上，
//! 各自带一份 ~8.4MB 的静态二进制纯属浪费；而且 relay 资产的发布链路
//! 长期不可靠（Release 流水线漏传、SHA 常量过期），导致 tcp_relay 在
//! 未装过 relay 的主机上永久部署失败。合并后不存在「下载 + 校验哈希」
//! 这一步，天然免疫该问题。
//!
//! 进程形态：`api-monitor-agent relay -listen 127.0.0.1:18080 -config ...`
//! 仍由独立 systemd 服务托管（`api-monitor-relay.service`），因此 relay
//! 崩溃/重启不会影响 agent 的采集与隧道，agent 升级也不必停 relay。
//!
//! 隧道协议（与 agent 侧 `tcp_forwarder.rs` 共享，改动必须两侧同步）：
//!   - 隧道握手：源主机 → `[4B BE len][len 字节 forward_id]`
//!               中继   → `[4B BE 状态码]`（0=接受，1=未知/冲突）
//!   - 每帧：    `[1B type][2B conn_id BE][4B 载荷长度 BE][载荷]`
//!     type 0x01=数据 0x02=关闭该 conn 0x03=保活（双方消费，不透传）
//!          0x04=UDP 数据 0x05=关闭该 UDP 会话
//!   conn_id 由中继分配，源主机镜像原样使用。

use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::{Arc, Mutex as StdMutex};
use std::time::{Duration, Instant};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::tcp::{OwnedReadHalf, OwnedWriteHalf};
use tokio::net::{TcpListener, TcpStream, UdpSocket};
use tokio::sync::{oneshot, watch, Mutex as AsyncMutex};

pub const FRAME_DATA: u8 = 0x01;
pub const FRAME_CLOSE: u8 = 0x02;
pub const FRAME_KEEPALIVE: u8 = 0x03;
pub const FRAME_UDP_DATA: u8 = 0x04;
pub const FRAME_UDP_CLOSE: u8 = 0x05;

/// 单帧载荷上限（与 agent 侧 `frame::MAX_PAYLOAD` 一致）。
pub const MAX_FRAME_PAYLOAD: usize = 1 << 20;
/// 帧头长度：1B type + 2B conn_id + 4B len。
pub const FRAME_HDR_LEN: usize = 7;

/// 区分「隧道握手」与「普通客户端」的读取窗口。
///
/// 隧道建立后源主机会**立刻**发 `[4B len][forward_id]`，所以只要读到 4
/// 字节长度头即可判定，不必等满窗口。普通客户端（SSH/HTTP/MySQL…）则
/// 可能长时间不发数据（MySQL 是服务端先发握手包），因此这里只用于
/// **尽早**判定隧道，绝不因为「窗口内没数据」就断开连接。
const CLASSIFY_WINDOW: Duration = Duration::from_millis(300);
/// 分类窗口内不足 4 字节时，为隧道判定额外等待的上限。
const HANDSHAKE_EXTRA_WAIT: Duration = Duration::from_secs(3);
/// token 模式下等待客户端提交凭证的上限（客户端可能晚于窗口发送）。
const TOKEN_WAIT: Duration = Duration::from_secs(5);
/// 客户端已接入但隧道尚未上线时的等待上限。
const TUNNEL_WAIT: Duration = Duration::from_secs(10);

const KEEPALIVE_INTERVAL: Duration = Duration::from_secs(30);
const UDP_SESSION_IDLE: Duration = Duration::from_secs(120);
const UDP_SWEEP_INTERVAL: Duration = Duration::from_secs(30);
const CLIENT_READ_BUF: usize = 32 * 1024;
const UDP_BUF: usize = 64 * 1024;

// ==================== 帧编解码 ====================

pub async fn write_frame<W>(w: &mut W, typ: u8, conn_id: u16, payload: &[u8]) -> std::io::Result<()>
where
    W: AsyncWriteExt + Unpin,
{
    let mut hdr = [0u8; FRAME_HDR_LEN];
    hdr[0] = typ;
    hdr[1..3].copy_from_slice(&conn_id.to_be_bytes());
    hdr[3..7].copy_from_slice(&(payload.len() as u32).to_be_bytes());
    w.write_all(&hdr).await?;
    if !payload.is_empty() {
        w.write_all(payload).await?;
    }
    Ok(())
}

pub async fn read_frame<R>(r: &mut R) -> std::io::Result<(u8, u16, Vec<u8>)>
where
    R: AsyncReadExt + Unpin,
{
    let mut hdr = [0u8; FRAME_HDR_LEN];
    r.read_exact(&mut hdr).await?;
    let typ = hdr[0];
    let conn_id = u16::from_be_bytes([hdr[1], hdr[2]]);
    let len = u32::from_be_bytes([hdr[3], hdr[4], hdr[5], hdr[6]]) as usize;
    if len > MAX_FRAME_PAYLOAD {
        return Err(std::io::Error::new(
            std::io::ErrorKind::InvalidData,
            format!("frame payload too large: {len}"),
        ));
    }
    let mut payload = vec![0u8; len];
    if len > 0 {
        r.read_exact(&mut payload).await?;
    }
    Ok((typ, conn_id, payload))
}

// ==================== 配置 ====================

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct RelayConfig {
    #[serde(default)]
    pub forwards: Vec<ForwardConfig>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ForwardConfig {
    pub id: String,
    pub listen_port: u16,
    #[serde(default)]
    pub token: String,
    #[serde(default)]
    pub udp: bool,
}

// ==================== 数据结构 ====================

/// 隧道写端。所有写隧道的路径都要先拿这把 async 锁（帧不能交错）。
struct Tunnel {
    writer: AsyncMutex<OwnedWriteHalf>,
}

struct UdpSession {
    client: SocketAddr,
    last_seen: Instant,
}

struct ForwardInner {
    tunnel: Option<Arc<Tunnel>>,
    conns: HashMap<u16, Arc<AsyncMutex<OwnedWriteHalf>>>,
    next_id: u16,
    udp_sessions: HashMap<u16, UdpSession>,
    udp_next_id: u16,
}

pub struct Forward {
    pub id: String,
    pub port: u16,
    token: String,
    udp: bool,
    inner: StdMutex<ForwardInner>,
    /// UDP 监听 socket（回程数据报也用它发，与 Go 版 `f.udpConn` 语义一致）。
    udp_sock: StdMutex<Option<Arc<UdpSocket>>>,
    /// 关闭信号：accept/keepalive/UDP 循环都 select 它。
    /// 没有它时 remove_forward 只能清状态，监听会永久占着端口。
    stop_tx: StdMutex<Option<watch::Sender<bool>>>,
    stop_rx: watch::Receiver<bool>,
}

impl Forward {
    fn new(id: String, port: u16, token: String, udp: bool) -> Arc<Self> {
        // 停止信号只创建一次，sender 与 Forward 同生命周期。
        // 若在 start_forward 里重建通道，Forward 里缓存的 receiver 会指向
        // 已丢弃的旧 sender，changed() 立刻返回 Err，accept 循环瞬间退出。
        let (stop_tx, stop_rx) = watch::channel(false);
        Arc::new(Self {
            id,
            port,
            token,
            udp,
            inner: StdMutex::new(ForwardInner {
                tunnel: None,
                conns: HashMap::new(),
                next_id: 0,
                udp_sessions: HashMap::new(),
                udp_next_id: 0,
            }),
            udp_sock: StdMutex::new(None),
            stop_tx: StdMutex::new(Some(stop_tx)),
            stop_rx,
        })
    }

    /// 订阅停止信号（可多次订阅，watch 支持 clone）。
    fn stop_signal(&self) -> watch::Receiver<bool> {
        self.stop_rx.clone()
    }

    fn get_tunnel(&self) -> Option<Arc<Tunnel>> {
        self.inner.lock().unwrap().tunnel.clone()
    }

    /// 是否已建立隧道（管理接口用）。
    pub fn has_tunnel(&self) -> bool {
        self.inner.lock().unwrap().tunnel.is_some()
    }

    /// 是否需要 token 鉴权（管理接口用）。
    pub fn requires_token(&self) -> bool {
        !self.token.is_empty()
    }

    fn alloc_conn_id(&self) -> u16 {
        let mut inner = self.inner.lock().unwrap();
        loop {
            inner.next_id = inner.next_id.wrapping_add(1);
            if inner.next_id == 0 {
                continue;
            }
            if !inner.conns.contains_key(&inner.next_id) {
                return inner.next_id;
            }
        }
    }

    fn udp_session_id(&self, addr: SocketAddr) -> u16 {
        let mut inner = self.inner.lock().unwrap();
        for (id, ses) in inner.udp_sessions.iter_mut() {
            if ses.client.ip() == addr.ip() && ses.client.port() == addr.port() {
                ses.last_seen = Instant::now();
                return *id;
            }
        }
        loop {
            inner.udp_next_id = inner.udp_next_id.wrapping_add(1);
            if inner.udp_next_id == 0 {
                continue;
            }
            if !inner.udp_sessions.contains_key(&inner.udp_next_id) {
                let id = inner.udp_next_id;
                inner.udp_sessions.insert(
                    id,
                    UdpSession {
                        client: addr,
                        last_seen: Instant::now(),
                    },
                );
                return id;
            }
        }
    }
}

    /// 中继实例。多个 forward 共享一个实例，管理接口也挂在上面。
pub struct RelayServer {
    pub forwards: StdMutex<HashMap<String, Arc<Forward>>>,
    total_connections: AtomicI64,
    active_connections: AtomicI64,
    started_at: Instant,
    shutdown: StdMutex<Option<oneshot::Sender<()>>>,
    config_path: StdMutex<Option<PathBuf>>,
    mgmt_token: StdMutex<Option<String>>,
}

impl RelayServer {
    pub fn new(config_path: Option<PathBuf>) -> Arc<Self> {
        Arc::new(Self {
            forwards: StdMutex::new(HashMap::new()),
            total_connections: AtomicI64::new(0),
            active_connections: AtomicI64::new(0),
            started_at: Instant::now(),
            shutdown: StdMutex::new(None),
            config_path: StdMutex::new(config_path),
            mgmt_token: StdMutex::new(None),
        })
    }

    /// 设置管理接口的 Bearer token（None = 不校验）。
    pub fn set_mgmt_token(&self, token: Option<String>) {
        *self.mgmt_token.lock().unwrap() = token.filter(|t| !t.is_empty());
    }

    pub fn mgmt_token(&self) -> Option<String> {
        self.mgmt_token.lock().unwrap().clone()
    }

    pub fn stats(&self) -> (i64, i64, f64) {
        (
            self.total_connections.load(Ordering::Relaxed),
            self.active_connections.load(Ordering::Relaxed),
            self.started_at.elapsed().as_secs_f64(),
        )
    }

    pub fn forward_count(&self) -> usize {
        self.forwards.lock().unwrap().len()
    }

    /// 停止并移除全部 forward。进程退出与测试清理共用。
    #[cfg_attr(not(test), allow(dead_code))]
    pub async fn shutdown_all(&self) {
        let all: Vec<Arc<Forward>> = {
            let mut guard = self.forwards.lock().unwrap();
            guard.drain().map(|(_, v)| v).collect()
        };
        for f in all {
            self.stop_forward(&f).await;
        }
    }

    /// 注册（或替换）一条转发规则并启动监听。
    ///
    /// 幂等：端口 / token / udp 三项完全一致时直接返回成功，避免面板重试
    /// 时反复 shutdown → rebind 造成入口闪断。
    pub async fn upsert_forward(
        self: &Arc<Self>,
        id: String,
        port: u16,
        token: String,
        udp: bool,
    ) -> Result<(), String> {
        if id.is_empty() || port == 0 {
            return Err("id and listen_port(1-65535) required".to_string());
        }
        let existing = self.forwards.lock().unwrap().get(&id).cloned();
        if let Some(old) = existing {
            if old.port == port && old.token == token && old.udp == udp {
                return Ok(());
            }
            self.forwards.lock().unwrap().remove(&id);
            self.stop_forward(&old).await;
        }
        let fwd = Forward::new(id.clone(), port, token, udp);
        self.forwards
            .lock()
            .unwrap()
            .insert(id.clone(), fwd.clone());
        // 绑定失败要让调用方知道：面板据此把规则标成 failed，而不是「运行中但端口没监听」。
        if let Err(e) = self.start_forward(fwd.clone()).await {
            self.forwards.lock().unwrap().remove(&id);
            return Err(e);
        }
        self.persist().ok();
        Ok(())
    }

    pub async fn remove_forward(&self, id: &str) {
        let fwd = self.forwards.lock().unwrap().remove(id);
        if let Some(f) = fwd {
            self.stop_forward(&f).await;
            self.persist().ok();
        }
    }

    /// 启动所有已注册 forward 的监听（启动时从 config 恢复，或 upsert 时单个启动）。
    async fn start_forward(self: &Arc<Self>, f: Arc<Forward>) -> Result<(), String> {
        let listener = TcpListener::bind(("0.0.0.0", f.port))
            .await
            .map_err(|e| format!("listen port {} (forward {}): {}", f.port, f.id, e))?;
        let me = self.clone();
        let ff = f.clone();
        let mut stop_rx_accept = f.stop_signal();
        tokio::spawn(async move {
            tokio::select! {
                _ = me.accept_loop(ff, listener) => {}
                _ = stop_rx_accept.changed() => {}
            }
        });

        let keepalive_fwd = f.clone();
        let mut stop_rx_ka = f.stop_signal();
        tokio::spawn(async move {
            let mut ticker = tokio::time::interval(KEEPALIVE_INTERVAL);
            loop {
                tokio::select! {
                    _ = stop_rx_ka.changed() => return,
                    _ = ticker.tick() => {
                        let Some(tunnel) = keepalive_fwd.get_tunnel() else {
                            return;
                        };
                        if send_frame(&tunnel, FRAME_KEEPALIVE, 0, &[]).await.is_err() {
                            return;
                        }
                    }
                }
            }
        });

        if f.udp {
            match UdpSocket::bind(("0.0.0.0", f.port)).await {
                Ok(sock) => {
                    let sock = Arc::new(sock);
                    *f.udp_sock.lock().unwrap() = Some(sock.clone());
                    let me2 = self.clone();
                    let f2 = f.clone();
                    let s2 = sock.clone();
                    let mut stop_rx = f.stop_signal();
                    tokio::spawn(async move {
                        tokio::select! {
                            _ = me2.udp_read_loop(f2, s2) => {}
                            _ = stop_rx.changed() => {}
                        }
                    });
                    let me3 = self.clone();
                    let f3 = f.clone();
                    let mut stop_rx2 = f.stop_signal();
                    tokio::spawn(async move {
                        tokio::select! {
                            _ = me3.udp_sweep_loop(f3, sock) => {}
                            _ = stop_rx2.changed() => {}
                        }
                    });
                }
                // UDP 绑定失败不阻断 TCP：与 Go 版行为一致（仅记日志）。
                // 但要把错误暴露出来，否则用户以为 UDP 也在工作。
                Err(e) => eprintln!("[relay] udp listen port {} (forward {}): {e}", f.port, f.id),
            }
        }
        Ok(())
    }

    async fn stop_forward(&self, f: &Arc<Forward>) {
        // 先触发停止信号，让 accept 循环退出并 drop listener（释放端口）。
        if let Some(tx) = f.stop_tx.lock().unwrap().take() {
            let _ = tx.send(true);
        }
        let (tunnel, conns) = {
            let mut inner = f.inner.lock().unwrap();
            let t = inner.tunnel.take();
            let c: Vec<_> = inner.conns.drain().map(|(_, v)| v).collect();
            inner.udp_sessions.clear();
            (t, c)
        };
        *f.udp_sock.lock().unwrap() = None;
        drop(tunnel);
        drop(conns);
        // 给 accept 循环一点时间真正 drop listener，避免紧接着的 rebind 撞端口。
        tokio::time::sleep(Duration::from_millis(50)).await;
    }

    fn persist(&self) -> Result<(), String> {
        let path = self.config_path.lock().unwrap().clone();
        let Some(path) = path else { return Ok(()) };
        let forwards = self.forwards.lock().unwrap();
        let mut list: Vec<ForwardConfig> = forwards
            .values()
            .map(|f| ForwardConfig {
                id: f.id.clone(),
                listen_port: f.port,
                token: f.token.clone(),
                udp: f.udp,
            })
            .collect();
        list.sort_by(|a, b| a.id.cmp(&b.id));
        let cfg = RelayConfig { forwards: list };
        let body = serde_json::to_string_pretty(&cfg).map_err(|e| e.to_string())?;
        if let Some(dir) = path.parent() {
            std::fs::create_dir_all(dir).map_err(|e| e.to_string())?;
        }
        let tmp = path.with_extension("json.tmp");
        std::fs::write(&tmp, body).map_err(|e| e.to_string())?;
        std::fs::rename(&tmp, &path).map_err(|e| e.to_string())?;
        Ok(())
    }

    // ==================== TCP 接入 ====================

    async fn accept_loop(self: Arc<Self>, f: Arc<Forward>, listener: TcpListener) {
        loop {
            match listener.accept().await {
                Ok((conn, _peer)) => {
                    self.total_connections.fetch_add(1, Ordering::Relaxed);
                    self.active_connections.fetch_add(1, Ordering::Relaxed);
                    let me = self.clone();
                    let ff = f.clone();
                    tokio::spawn(async move {
                        me.clone().handle_incoming(ff, conn).await;
                        me.active_connections.fetch_sub(1, Ordering::Relaxed);
                    });
                }
                Err(_) => {
                    // accept 短暂失败（EMFILE 等）不应退出监听循环。
                    tokio::time::sleep(Duration::from_millis(50)).await;
                }
            }
        }
    }

    /// 分类进入连接：隧道握手 / token 校验 / 普通客户端桥接。
    async fn handle_incoming(self: Arc<Self>, f: Arc<Forward>, mut conn: TcpStream) {
        let mut buf: Vec<u8> = Vec::with_capacity(4096);
        let mut tmp = [0u8; 1024];
        let deadline = tokio::time::Instant::now() + CLASSIFY_WINDOW;

        // 只读到「够判定」为止：隧道判定只需 4 字节长度头。
        loop {
            if buf.len() >= 4 {
                break;
            }
            let now = tokio::time::Instant::now();
            if now >= deadline {
                break;
            }
            match tokio::time::timeout(deadline - now, conn.read(&mut tmp)).await {
                Ok(Ok(0)) => break,
                Ok(Ok(n)) => buf.extend_from_slice(&tmp[..n]),
                Ok(Err(_)) => return,
                Err(_) => break,
            }
        }

        // 1) 隧道握手：`[4B len][forward_id]`
        if buf.len() >= 4 {
            let declared = u32::from_be_bytes([buf[0], buf[1], buf[2], buf[3]]) as usize;
            if declared > 0 && declared <= 256 && declared == f.id.len() {
                match self.read_tunnel_id(&mut conn, &mut buf, declared).await {
                    Some(id) if id == f.id.as_bytes() => {
                        if conn.write_all(&[0u8, 0, 0, 0]).await.is_err() {
                            return;
                        }
                        self.accept_tunnel(f, conn, buf).await;
                        return;
                    }
                    Some(_) => {
                        // 长度对上了但内容不是本规则的 id：当作普通客户端，
                        // 已消费的字节要原样桥接（不能丢）。
                    }
                    None => return,
                }
            }
        }

        // 2) token 模式：raw 握手或 HTTP Bearer
        if !f.token.is_empty() {
            // 客户端可能晚于分类窗口才发凭证，给一个宽松的等待窗口。
            if buf.len() < 4 {
                let mut extra = [0u8; 1024];
                let extra_deadline = tokio::time::Instant::now() + TOKEN_WAIT;
                while buf.len() < 4 {
                    let now = tokio::time::Instant::now();
                    if now >= extra_deadline {
                        break;
                    }
                    match tokio::time::timeout(extra_deadline - now, conn.read(&mut extra)).await {
                        Ok(Ok(0)) => break,
                        Ok(Ok(n)) => buf.extend_from_slice(&extra[..n]),
                        Ok(Err(_)) => return,
                        Err(_) => break,
                    }
                }
            }
            match try_auth(&f, &mut conn, &mut buf).await {
                Some(consumed) => {
                    let prefix = buf[consumed..].to_vec();
                    self.handle_client(f, conn, prefix).await;
                }
                None => {
                    // 认证失败：关闭（与 Go 版一致，不泄漏任何信息）。
                }
            }
            return;
        }

        // 3) public：整段已读字节作为前缀原样桥接。
        //    注意 buf 可能为空——MySQL/PostgreSQL 这类「服务端先发握手」的
        //    协议，客户端连上后不会立刻发数据。绝不能因此关闭连接。
        self.handle_client(f, conn, buf).await;
    }

    /// 读隧道 id 内容（分类窗口可能只读到长度头）。
    async fn read_tunnel_id(
        &self,
        conn: &mut TcpStream,
        buf: &mut Vec<u8>,
        declared: usize,
    ) -> Option<Vec<u8>> {
        let mut id = Vec::with_capacity(declared);
        if buf.len() >= 4 + declared {
            id.extend_from_slice(&buf[4..4 + declared]);
            buf.drain(..4 + declared);
            return Some(id);
        }
        if buf.len() > 4 {
            id.extend_from_slice(&buf[4..]);
        }
        buf.clear();
        let need = declared - id.len();
        let mut rest = vec![0u8; need];
        match tokio::time::timeout(HANDSHAKE_EXTRA_WAIT, conn.read_exact(&mut rest)).await {
            Ok(Ok(_)) => {
                id.extend_from_slice(&rest);
                Some(id)
            }
            _ => None,
        }
    }

    async fn accept_tunnel(self: Arc<Self>, f: Arc<Forward>, conn: TcpStream, prefix: Vec<u8>) {
        let (reader, writer) = conn.into_split();
        let tunnel = Arc::new(Tunnel {
            writer: AsyncMutex::new(writer),
        });

        // 替换旧隧道：旧隧道及其承载的客户端全部断开（同一规则只允许一条活跃隧道）。
        let stale: Vec<Arc<AsyncMutex<OwnedWriteHalf>>> = {
            let mut inner = f.inner.lock().unwrap();
            inner.tunnel = Some(tunnel.clone());
            inner.conns.drain().map(|(_, v)| v).collect()
        };
        drop(stale);

        // 隧道握手可能带多余字节（极少见），按 DATA 帧补发。
        if !prefix.is_empty() {
            let _ = send_frame(&tunnel, FRAME_DATA, 0, &prefix).await;
        }

        tokio::spawn(async move {
            tunnel_read_loop(f, tunnel, reader).await;
        });
    }

    async fn handle_client(self: Arc<Self>, f: Arc<Forward>, conn: TcpStream, prefix: Vec<u8>) {
        // 隧道可能还没重连（relay 刚重启 / 源主机网络抖动）。
        // 等一小会儿而不是立刻拒绝，避免恢复期的连接风暴。
        let tunnel = match wait_for_tunnel(&f).await {
            Some(t) => t,
            None => return,
        };

        let conn_id = f.alloc_conn_id();
        let (mut reader, writer) = conn.into_split();
        let writer = Arc::new(AsyncMutex::new(writer));
        {
            let mut inner = f.inner.lock().unwrap();
            inner.conns.insert(conn_id, writer.clone());
        }

        // 必须在此主动通告 conn_id（即使 prefix 为空）。
        //
        // 原因：源主机 Agent 只有在收到带某个 conn_id 的帧之后才会去连本地
        // 服务。而 MySQL / PostgreSQL / Redis / SMTP 这类协议是**服务端先发
        // 握手包**，客户端连上后长时间不发数据；若不主动通告，Agent 永远
        // 不会建立本地连接，服务端的 greeting 也就无从产生 —— 表现为「端口
        // 能连上但永远没数据」。原 Go 实现只在 prefix 非空时才发帧，正是
        // 这个缺陷的根源。
        //
        // 空载荷 DATA 帧在 Agent 侧等价于「建连但不写数据」，随后
        // local_to_tunnel 会把服务端首个响应回灌上来。
        if send_frame(&tunnel, FRAME_DATA, conn_id, &prefix).await.is_err() {
            remove_conn(&f, conn_id);
            return;
        }

        let mut buf = vec![0u8; CLIENT_READ_BUF];
        loop {
            match reader.read(&mut buf).await {
                Ok(0) | Err(_) => break,
                Ok(n) => {
                    if send_frame(&tunnel, FRAME_DATA, conn_id, &buf[..n]).await.is_err() {
                        break;
                    }
                }
            }
        }
        remove_conn(&f, conn_id);
        let _ = send_frame(&tunnel, FRAME_CLOSE, conn_id, &[]).await;
    }

    // ==================== UDP ====================

    async fn udp_read_loop(self: Arc<Self>, f: Arc<Forward>, sock: Arc<UdpSocket>) {
        let mut buf = vec![0u8; UDP_BUF];
        loop {
            let (n, addr) = match sock.recv_from(&mut buf).await {
                Ok(v) => v,
                Err(_) => return,
            };
            let Some(tunnel) = f.get_tunnel() else { continue };
            let id = f.udp_session_id(addr);
            if send_frame(&tunnel, FRAME_UDP_DATA, id, &buf[..n])
                .await
                .is_err()
            {
                return;
            }
        }
    }

    async fn udp_sweep_loop(self: Arc<Self>, f: Arc<Forward>, sock: Arc<UdpSocket>) {
        let mut ticker = tokio::time::interval(UDP_SWEEP_INTERVAL);
        loop {
            ticker.tick().await;
            let expired: Vec<u16> = {
                let mut inner = f.inner.lock().unwrap();
                let now = Instant::now();
                let ids: Vec<u16> = inner
                    .udp_sessions
                    .iter()
                    .filter(|(_, s)| now.duration_since(s.last_seen) > UDP_SESSION_IDLE)
                    .map(|(id, _)| *id)
                    .collect();
                for id in &ids {
                    inner.udp_sessions.remove(id);
                }
                ids
            };
            if expired.is_empty() {
                continue;
            }
            if let Some(tunnel) = f.get_tunnel() {
                for id in expired {
                    let _ = send_frame(&tunnel, FRAME_UDP_CLOSE, id, &[]).await;
                }
            }
            let _ = sock; // 保留句柄避免被提前 drop
        }
    }
}

/// 等隧道上线，最多 `TUNNEL_WAIT`。
async fn wait_for_tunnel(f: &Arc<Forward>) -> Option<Arc<Tunnel>> {
    let deadline = tokio::time::Instant::now() + TUNNEL_WAIT;
    loop {
        if let Some(t) = f.get_tunnel() {
            return Some(t);
        }
        if tokio::time::Instant::now() >= deadline {
            return None;
        }
        tokio::time::sleep(Duration::from_millis(50)).await;
    }
}

fn remove_conn(f: &Arc<Forward>, conn_id: u16) {
    f.inner.lock().unwrap().conns.remove(&conn_id);
}

async fn send_frame(tunnel: &Arc<Tunnel>, typ: u8, conn_id: u16, payload: &[u8]) -> Result<(), ()> {
    let mut w = tunnel.writer.lock().await;
    write_frame(&mut *w, typ, conn_id, payload)
        .await
        .map_err(|_| ())
}

/// 隧道 → 客户端分发。退出即代表隧道断开：清空状态等源主机重连。
async fn tunnel_read_loop(f: Arc<Forward>, tunnel: Arc<Tunnel>, mut reader: OwnedReadHalf) {
    loop {
        let (typ, conn_id, payload) = match read_frame(&mut reader).await {
            Ok(v) => v,
            Err(_) => break,
        };
        match typ {
            FRAME_DATA => {
                let writer = { f.inner.lock().unwrap().conns.get(&conn_id).cloned() };
                let Some(writer) = writer else {
                    let _ = send_frame(&tunnel, FRAME_CLOSE, conn_id, &[]).await;
                    continue;
                };
                let mut w = writer.lock().await;
                if w.write_all(&payload).await.is_err() {
                    drop(w);
                    remove_conn(&f, conn_id);
                    let _ = send_frame(&tunnel, FRAME_CLOSE, conn_id, &[]).await;
                }
            }
            FRAME_CLOSE => {
                remove_conn(&f, conn_id);
            }
            FRAME_UDP_DATA => {
                // 回程 UDP：发到该会话的客户端地址（用监听 socket，与 Go 版一致，
                // 保证源端口就是公开端口，某些 NAT 会校验源端口）。
                let target = {
                    let mut inner = f.inner.lock().unwrap();
                    inner.udp_sessions.get_mut(&conn_id).map(|s| {
                        s.last_seen = Instant::now();
                        s.client
                    })
                };
                if let Some(addr) = target {
                    let sock = f.udp_sock.lock().unwrap().clone();
                    if let Some(sock) = sock {
                        let _ = sock.send_to(&payload, addr).await;
                    }
                }
            }
            FRAME_UDP_CLOSE => {
                f.inner.lock().unwrap().udp_sessions.remove(&conn_id);
            }
            FRAME_KEEPALIVE => {}
            _ => {}
        }
    }

    // 隧道断开：清空 tunnel 与所有客户端连接（客户端会读到 EOF 自行断开）。
    let conns = {
        let mut inner = f.inner.lock().unwrap();
        inner.tunnel = None;
        inner.conns.drain().map(|(_, v)| v).collect::<Vec<_>>()
    };
    drop(conns);
    let _ = tunnel;
}

// ==================== token 鉴权 ====================

/// 尝试认证。返回 `Some(consumed)` 表示通过（consumed = 需要剥离的前缀字节数）；
/// `None` 表示失败。
async fn try_auth(f: &Arc<Forward>, conn: &mut TcpStream, buf: &mut Vec<u8>) -> Option<usize> {
    // raw 握手：`[4B len][token]`
    if buf.len() >= 4 {
        let declared = u32::from_be_bytes([buf[0], buf[1], buf[2], buf[3]]) as usize;
        if declared > 0 && declared <= 512 && declared == f.token.len() {
            let mut tok = Vec::with_capacity(declared);
            if buf.len() >= 4 + declared {
                tok.extend_from_slice(&buf[4..4 + declared]);
            } else {
                if buf.len() > 4 {
                    tok.extend_from_slice(&buf[4..]);
                }
                let need = declared - tok.len();
                let mut rest = vec![0u8; need];
                match tokio::time::timeout(HANDSHAKE_EXTRA_WAIT, conn.read_exact(&mut rest)).await {
                    Ok(Ok(_)) => tok.extend_from_slice(&rest),
                    _ => return None,
                }
            }
            if tok == f.token.as_bytes() {
                return Some(4 + declared);
            }
        }
    }

    // HTTP：`Authorization: Bearer <token>`（整请求原样转发，仅作访问闸门）
    if is_http(buf) {
        let head = String::from_utf8_lossy(buf);
        for line in head.split("\r\n") {
            let t = line.trim();
            if t.len() >= 14 && t[..14].eq_ignore_ascii_case("authorization:") {
                let val = t[14..].trim();
                if let Some(rest) = val.strip_prefix("Bearer ") {
                    if rest.trim() == f.token {
                        return Some(0);
                    }
                }
            }
        }
    }
    None
}

fn is_http(buf: &[u8]) -> bool {
    if buf.len() < 3 {
        return false;
    }
    let space = match buf.iter().position(|&b| b == b' ') {
        Some(p) if p > 0 => p,
        _ => return false,
    };
    let method = String::from_utf8_lossy(&buf[..space]).to_uppercase();
    matches!(
        method.as_str(),
        "GET" | "POST" | "PUT" | "PATCH" | "DELETE" | "HEAD" | "OPTIONS"
    )
}

// ==================== 启动入口 ====================

/// 读取配置（不存在则返回空配置）。
pub fn load_config(path: &Path) -> RelayConfig {
    match std::fs::read_to_string(path) {
        Ok(body) => serde_json::from_str(&body).unwrap_or_else(|e| {
            eprintln!("[relay] invalid config {path:?}: {e}");
            RelayConfig::default()
        }),
        Err(_) => RelayConfig::default(),
    }
}

/// 运行中继（阻塞直到收到退出信号）。
pub async fn run(listen: &str, config_path: &Path, mgmt_token: Option<String>) -> Result<(), String> {
    let cfg = load_config(config_path);
    let server = RelayServer::new(Some(config_path.to_path_buf()));
    server.set_mgmt_token(mgmt_token);

    for fwd in &cfg.forwards {
        if fwd.id.is_empty() || fwd.listen_port == 0 {
            continue;
        }
        if let Err(e) = server
            .upsert_forward(
                fwd.id.clone(),
                fwd.listen_port,
                fwd.token.clone(),
                fwd.udp,
            )
            .await
        {
            eprintln!("[relay] restore forward {} failed: {e}", fwd.id);
        }
    }

    if !listen.is_empty() {
        let app = crate::relay_http::router(server.clone());
        let listener = TcpListener::bind(listen)
            .await
            .map_err(|e| format!("management listen {listen}: {e}"))?;
        tokio::spawn(async move {
            if let Err(e) = axum::serve(listener, app).await {
                eprintln!("[relay] management server stopped: {e}");
            }
        });
    }

    println!(
        "[relay] started, mgmt={}, forwards={}",
        listen,
        server.forward_count()
    );

    let (tx, rx) = oneshot::channel::<()>();
    *server.shutdown.lock().unwrap() = Some(tx);
    tokio::select! {
        _ = rx => {}
        _ = shutdown_signal() => {}
    }
    println!("[relay] shutting down");
    Ok(())
}

async fn shutdown_signal() {
    #[cfg(unix)]
    {
        use tokio::signal::unix::{signal, SignalKind};
        let mut term = signal(SignalKind::terminate()).expect("install SIGTERM handler");
        tokio::select! {
            _ = tokio::signal::ctrl_c() => {}
            _ = term.recv() => {}
        }
    }
    #[cfg(not(unix))]
    {
        let _ = tokio::signal::ctrl_c().await;
    }
}
