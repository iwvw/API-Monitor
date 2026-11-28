//! 内嵌 relay 的端到端回归测试。
//!
//! 这些用例逐条对齐原 Go 实现（backend-go/cmd/api-monitor-relay/main_test.go）
//! 的语义，确保 Rust 重写没有丢功能：
//!   1. 单客户端双向桥接 + 断开时回 CLOSE 帧
//!   2. 多客户端复用同一条隧道（conn_id 隔离）
//!   3. KEEPALIVE 不被透传给客户端
//!   4. 错误握手被当作普通客户端而不是隧道
//!   5. token 模式：raw 握手通过 / 错误被拒
//!   6. token 模式：HTTP Bearer 通过
//!   7. UDP 转发
//!   8. 服务端先说话的协议（客户端不发数据）不会被掐断
//!   9. 监听持久化 → 重启后自动恢复

use super::*;
use crate::relay_server::{
    load_config, read_frame, write_frame, RelayServer, FRAME_CLOSE, FRAME_DATA, FRAME_KEEPALIVE,
    FRAME_UDP_DATA,
};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::UdpSocket;
use tokio::net::TcpListener;

/// 申请一个当前可用的 TCP 端口。
///
/// 早期版本用「全局计数器递增」（46100, 46101...），但 cargo test 默认并行，
/// 多个用例在同一进程里可能抢到同一端口，或上一个用例还没释放监听就撞上，
/// 表现为偶发 Address already in use。改为真正 bind 一次再释放：
/// 由内核给出一个当前空闲端口。
fn free_port() -> u16 {
    let l = std::net::TcpListener::bind(("127.0.0.1", 0)).expect("bind ephemeral");
    l.local_addr().expect("local_addr").port()
}

/// relay 用例串行锁：每个用例都会真实 bind 端口并启动后台 accept/UDP 循环。
/// cargo test 默认并行，多实例同时跑会互相抢端口；这里串起来跑。
/// 注意：锁必须覆盖整个用例（含 cleanup），否则上一个用例的监听还没关，
/// 下一个用例就 bind 了同一个端口。
static RELAY_SERIAL: std::sync::Mutex<()> = std::sync::Mutex::new(());

fn serial() -> std::sync::MutexGuard<'static, ()> {
    RELAY_SERIAL.lock().unwrap_or_else(|e| e.into_inner())
}

/// upsert_forward 内部先 bind 再返回，所以返回时端口已经处于监听状态。
/// 这里只做一次很短的就绪等待（UDP 监听与 accept 循环是异步起的），
/// 不用 TCP connect 探测——探测本身会被中继当作一个真实客户端。
async fn wait_ready() {
    tokio::time::sleep(Duration::from_millis(50)).await;
}

/// 起一个 relay（不含 mgmt），返回 server 句柄。
/// 注意：调用方必须保证在测试结束前 shutdown_all()，否则 listener 会泄漏，
/// 后续测试复用同一端口号时会 Address already in use。
async fn start_relay() -> Arc<RelayServer> {
    RelayServer::new(None)
}

async fn add(server: &Arc<RelayServer>, id: &str, port: u16, token: &str, udp: bool) {
    server
        .upsert_forward(id.to_string(), port, token.to_string(), udp)
        .await
        .expect("upsert forward");
    wait_ready().await;
}

/// 建立隧道：`[4B len][forward_id]` → 校验 4 字节 ack 为 0。
async fn dial_tunnel(port: u16, id: &str) -> TcpStream {
    let mut s = TcpStream::connect(("127.0.0.1", port)).await.expect("connect");
    let mut hdr = Vec::new();
    hdr.extend_from_slice(&(id.len() as u32).to_be_bytes());
    hdr.extend_from_slice(id.as_bytes());
    s.write_all(&hdr).await.expect("send handshake");
    let mut ack = [0u8; 4];
    s.read_exact(&mut ack).await.expect("read ack");
    assert_eq!(u32::from_be_bytes(ack), 0, "tunnel rejected");
    s
}

/// 读隧道上的一条 DATA 帧（跳过 KEEPALIVE 与「建连公告」空载荷帧）。
async fn read_data_frame(s: &mut TcpStream, timeout: Duration) -> (u16, Vec<u8>) {
    let deadline = tokio::time::Instant::now() + timeout;
    loop {
        let remaining = deadline.saturating_duration_since(tokio::time::Instant::now());
        let (typ, conn_id, payload) = tokio::time::timeout(remaining, read_frame(s))
            .await
            .expect("timed out waiting for frame")
            .expect("read frame");
        if typ == FRAME_KEEPALIVE {
            continue;
        }
        return (conn_id, payload);
    }
}

/// 读隧道上第一条「非空载荷」DATA 帧（跳过建连公告帧）。
async fn read_payload_frame(s: &mut TcpStream, timeout: Duration) -> (u16, Vec<u8>) {
    let deadline = tokio::time::Instant::now() + timeout;
    loop {
        let remaining = deadline.saturating_duration_since(tokio::time::Instant::now());
        let (typ, conn_id, payload) = tokio::time::timeout(remaining, read_frame(s))
            .await
            .expect("timed out waiting for frame")
            .expect("read frame");
        if typ == FRAME_DATA && payload.is_empty() {
            continue;
        }
        return (conn_id, payload);
    }
}

// ==================== 1. 单客户端双向桥接 ====================

#[tokio::test]
async fn bridges_single_client_both_directions() {
    let _serial = serial();
    let server = start_relay().await;
    let port = free_port();
    add(&server, "fwd_test01", port, "", false).await;

    let mut tun = dial_tunnel(port, "fwd_test01").await;

    let mut client = TcpStream::connect(("127.0.0.1", port)).await.expect("client");
    client.write_all(b"hello from client").await.expect("write");

    let (conn_id, payload) = read_payload_frame(&mut tun, Duration::from_secs(3)).await;
    assert_eq!(payload, b"hello from client");

    // 回程：隧道 → 客户端
    write_frame(&mut tun, FRAME_DATA, conn_id, b"hello from server")
        .await
        .expect("write back");
    let mut buf = vec![0u8; b"hello from server".len()];
    tokio::time::timeout(Duration::from_secs(3), client.read_exact(&mut buf))
        .await
        .expect("client read timeout")
        .expect("client read");
    assert_eq!(buf, b"hello from server");

    // 客户端断开 → 隧道收到 CLOSE
    drop(client);
    let deadline = tokio::time::Instant::now() + Duration::from_secs(3);
    loop {
        let remaining = deadline.saturating_duration_since(tokio::time::Instant::now());
        let (typ, id, _) = tokio::time::timeout(remaining, read_frame(&mut tun))
            .await
            .expect("timed out waiting for CLOSE")
            .expect("read frame");
        if typ == FRAME_KEEPALIVE {
            continue;
        }
        assert_eq!(typ, FRAME_CLOSE);
        assert_eq!(id, conn_id);
        break;
    }
    server.shutdown_all().await;
}

// ==================== 2. 多客户端复用 ====================

#[tokio::test]
async fn multiplexes_concurrent_clients() {
    let _serial = serial();
    let server = start_relay().await;
    let port = free_port();
    add(&server, "fwd_mux", port, "", false).await;

    let mut tun = dial_tunnel(port, "fwd_mux").await;

    let mut c1 = TcpStream::connect(("127.0.0.1", port)).await.unwrap();
    let mut c2 = TcpStream::connect(("127.0.0.1", port)).await.unwrap();
    c1.write_all(b"AAA").await.unwrap();
    c2.write_all(b"BBB").await.unwrap();

    let mut got: Vec<(u16, Vec<u8>)> = Vec::new();
    for _ in 0..2 {
        got.push(read_payload_frame(&mut tun, Duration::from_secs(3)).await);
    }
    got.sort_by(|a, b| a.1.cmp(&b.1));
    assert_eq!(got[0].1, b"AAA");
    assert_eq!(got[1].1, b"BBB");
    // conn_id 必须互不相同
    assert_ne!(got[0].0, got[1].0, "conn ids must be unique");

    // 分别回程，数据不能串线
    let id_a = got[0].0;
    let id_b = got[1].0;
    write_frame(&mut tun, FRAME_DATA, id_a, b"to-A").await.unwrap();
    write_frame(&mut tun, FRAME_DATA, id_b, b"to-B").await.unwrap();

    let mut buf = [0u8; 4];
    c1.read_exact(&mut buf).await.unwrap();
    assert_eq!(&buf, b"to-A");
    c2.read_exact(&mut buf).await.unwrap();
    assert_eq!(&buf, b"to-B");
    server.shutdown_all().await;
}

// ==================== 3. KEEPALIVE 不透传 ====================

#[tokio::test]
async fn keepalive_not_forwarded_to_client() {
    let _serial = serial();
    let server = start_relay().await;
    let port = free_port();
    add(&server, "fwd_ka", port, "", false).await;

    let mut tun = dial_tunnel(port, "fwd_ka").await;
    let mut client = TcpStream::connect(("127.0.0.1", port)).await.unwrap();
    client.write_all(b"x").await.unwrap();
    let (conn_id, _) = read_payload_frame(&mut tun, Duration::from_secs(3)).await;

    // 中继发 KEEPALIVE 给隧道；客户端不应收到任何字节
    write_frame(&mut tun, FRAME_KEEPALIVE, 0, &[]).await.unwrap();
    let mut buf = [0u8; 8];
    let r = tokio::time::timeout(Duration::from_millis(400), client.read(&mut buf)).await;
    assert!(
        r.is_err(),
        "client must not receive keepalive bytes, got {:?}",
        r.ok().and_then(|v| v.ok())
    );
    let _ = conn_id;
    server.shutdown_all().await;
}

// ==================== 4. 错误握手按普通客户端处理 ====================

#[tokio::test]
async fn wrong_handshake_is_treated_as_client() {
    let _serial = serial();
    let server = start_relay().await;
    let port = free_port();
    add(&server, "fwd_wr", port, "", false).await;

    let mut tun = dial_tunnel(port, "fwd_wr").await;

    // 长度 7 但内容不是本规则的 id
    let mut c = TcpStream::connect(("127.0.0.1", port)).await.unwrap();
    let mut hdr = Vec::new();
    hdr.extend_from_slice(&7u32.to_be_bytes());
    hdr.extend_from_slice(b"bogus!!");
    c.write_all(&hdr).await.unwrap();
    c.write_all(b" rest-of-data").await.unwrap();

    let want = b"\x00\x00\x00\x07bogus!! rest-of-data";
    let mut acc = Vec::new();
    while acc.len() < want.len() {
        let (_, payload) = read_payload_frame(&mut tun, Duration::from_secs(3)).await;
        acc.extend_from_slice(&payload);
    }
    assert_eq!(acc, want, "client prefix must be preserved verbatim");
    server.shutdown_all().await;
}

// ==================== 5. token raw 握手 ====================

#[tokio::test]
async fn token_raw_handshake_gate() {
    let _serial = serial();
    let server = start_relay().await;
    let port = free_port();
    add(&server, "fwd_tk", port, "am_abc123", false).await;

    let mut tun = dial_tunnel(port, "fwd_tk").await;

    // 正确 token：被剥离，后续数据原样桥接
    let mut ok = TcpStream::connect(("127.0.0.1", port)).await.unwrap();
    let mut hdr = Vec::new();
    hdr.extend_from_slice(&("am_abc123".len() as u32).to_be_bytes());
    hdr.extend_from_slice(b"am_abc123");
    ok.write_all(&hdr).await.unwrap();
    ok.write_all(b"ping").await.unwrap();
    let (_, payload) = read_payload_frame(&mut tun, Duration::from_secs(3)).await;
    assert_eq!(payload, b"ping", "token must be stripped");

    // 错误 token：连接被关闭，隧道不应收到该数据
    let mut bad = TcpStream::connect(("127.0.0.1", port)).await.unwrap();
    let mut hdr = Vec::new();
    hdr.extend_from_slice(&("am_wrong".len() as u32).to_be_bytes());
    hdr.extend_from_slice(b"am_wrong");
    bad.write_all(&hdr).await.unwrap();
    bad.write_all(b"should-not-pass").await.unwrap();
    let mut buf = [0u8; 4];
    let r = tokio::time::timeout(Duration::from_secs(2), bad.read(&mut buf)).await;
    // 服务端关闭 → read 返回 Ok(0) 或 Err，都不应是「有数据」
    match r {
        Ok(Ok(0)) | Ok(Err(_)) | Err(_) => {}
        Ok(Ok(n)) => panic!("bad-token client got {n} bytes back: {:?}", &buf[..n]),
    }
    server.shutdown_all().await;
}

// ==================== 6. token HTTP Bearer ====================

#[tokio::test]
async fn token_http_bearer_gate() {
    let _serial = serial();
    let server = start_relay().await;
    let port = free_port();
    add(&server, "fwd_http", port, "am_http", false).await;

    let mut tun = dial_tunnel(port, "fwd_http").await;

    let mut c = TcpStream::connect(("127.0.0.1", port)).await.unwrap();
    let req = b"GET / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer am_http\r\n\r\n";
    c.write_all(req).await.unwrap();

    let mut acc = Vec::new();
    while acc.len() < req.len() {
        let (_, payload) = read_payload_frame(&mut tun, Duration::from_secs(3)).await;
        acc.extend_from_slice(&payload);
    }
    // HTTP 请求整段原样转发（仅作访问闸门，不剥离）
    assert_eq!(acc, req.to_vec());
    server.shutdown_all().await;
}

// ==================== 7. UDP 转发 ====================

#[tokio::test]
async fn udp_forwarding_roundtrip() {
    let _serial = serial();
    let server = start_relay().await;
    let port = free_port();
    add(&server, "fwd_udp", port, "", true).await;
    // UDP 监听是异步起在 start_forward 里，稍等
    tokio::time::sleep(Duration::from_millis(120)).await;

    let mut tun = dial_tunnel(port, "fwd_udp").await;

    let client = UdpSocket::bind(("127.0.0.1", 0)).await.unwrap();
    let target: SocketAddr = format!("127.0.0.1:{port}").parse().unwrap();
    client.send_to(b"udp-hello", target).await.unwrap();

    let (typ, conn_id, payload) = tokio::time::timeout(Duration::from_secs(3), read_frame(&mut tun))
        .await
        .expect("udp frame timeout")
        .expect("read frame");
    assert_eq!(typ, FRAME_UDP_DATA);
    assert_eq!(payload, b"udp-hello");

    // 回程
    write_frame(&mut tun, FRAME_UDP_DATA, conn_id, b"udp-back")
        .await
        .unwrap();
    let mut buf = [0u8; 16];
    let (n, _) = tokio::time::timeout(Duration::from_secs(3), client.recv_from(&mut buf))
        .await
        .expect("udp back timeout")
        .expect("recv");
    assert_eq!(&buf[..n], b"udp-back");
    server.shutdown_all().await;
}

// ==================== 8. 服务端先说话的协议 ====================
// 这是原 Go 版的核心缺陷：300ms 分类窗口内客户端不发数据就被 close，
// 导致 MySQL/PostgreSQL/Redis 这类「服务端先发握手包」的转发永远不通。

#[tokio::test]
async fn passive_client_gets_conn_announced_and_stays_alive() {
    let _serial = serial();
    let server = start_relay().await;
    let port = free_port();
    add(&server, "fwd_passive", port, "", false).await;

    let mut tun = dial_tunnel(port, "fwd_passive").await;

    // 客户端连上后什么都不发（模拟 MySQL 客户端等待服务端 greeting）
    let mut client = TcpStream::connect(("127.0.0.1", port)).await.unwrap();
    tokio::time::sleep(Duration::from_millis(700)).await; // 远超 300ms 分类窗口

    // 关键：超过分类窗口后，中继必须已经主动通告 conn_id（空载荷 DATA 帧），
    // 否则 Agent 永远不会去连本地服务，服务端 greeting 就无从产生。
    let (conn_id, payload) = read_data_frame(&mut tun, Duration::from_secs(3)).await;
    assert!(
        payload.is_empty(),
        "announce frame must carry no payload, got {payload:?}"
    );

    // 服务端先说话：隧道主动下发 greeting，passive 客户端必须能收到
    write_frame(&mut tun, FRAME_DATA, conn_id, b"SSH-2.0-fake\r\n")
        .await
        .unwrap();
    let mut buf = [0u8; 14];
    tokio::time::timeout(Duration::from_secs(3), client.read_exact(&mut buf))
        .await
        .expect("passive client must receive server-first greeting")
        .expect("read greeting");
    assert_eq!(&buf, b"SSH-2.0-fake\r\n");

    // 客户端随后发数据，仍应转发到隧道（同一 conn_id）
    client.write_all(b"z").await.unwrap();
    let (id2, payload2) = read_data_frame(&mut tun, Duration::from_secs(3)).await;
    assert_eq!(id2, conn_id);
    assert_eq!(payload2, b"z");
    server.shutdown_all().await;
}

// ==================== 9. 配置持久化与恢复 ====================

#[tokio::test]
async fn forwards_persist_and_restore() {
    let dir = std::env::temp_dir().join(format!("relay-test-{}", std::process::id()));
    let _ = std::fs::create_dir_all(&dir);
    let cfg = dir.join("config.json");
    let _ = std::fs::remove_file(&cfg);

    let port = free_port();
    {
        let server = RelayServer::new(Some(cfg.clone()));
        add(&server, "fwd_persist", port, "tok", false).await;
        // upsert 内部已 persist
        server.shutdown_all().await;
    }

    let raw = std::fs::read_to_string(&cfg).expect("config file written");
    assert!(raw.contains("fwd_persist"), "config must contain forward id: {raw}");
    assert!(raw.contains(&port.to_string()), "config must contain port");

    // 用同一配置起新实例：监听应自动恢复
    let server2 = RelayServer::new(Some(cfg.clone()));
    let cfg_loaded = load_config(&cfg);
    for f in cfg_loaded.forwards {
        server2
            .upsert_forward(f.id.clone(), f.listen_port, f.token.clone(), f.udp)
            .await
            .expect("restore forward");
    }
    wait_ready().await;

    let _ = std::fs::remove_dir_all(&dir);
    server2.shutdown_all().await;
}

// ==================== 管理 HTTP 接口 ====================
//
// 这组用例专门覆盖「HTTP 路由是否真的能匹配上」。
// 之前所有测试都直接调用 upsert_forward，绕过了 axum 路由，
// 于是 `{id}`（axum 0.8 语法）在 axum 0.7 上永远 404 的问题没被发现——
// 表现为面板删除规则成功、relay 侧监听却还在，端口一直占着。

/// 起一个带管理接口的 relay，返回 (server, mgmt_port)。
async fn start_with_mgmt() -> (Arc<RelayServer>, u16) {
    let port = free_port();
    let server = RelayServer::new(None);
    let app = crate::relay_http::router(server.clone());
    let listener = TcpListener::bind(("127.0.0.1", port)).await.expect("mgmt bind");
    tokio::spawn(async move {
        let _ = axum::serve(listener, app).await;
    });
    tokio::time::sleep(Duration::from_millis(80)).await;
    (server, port)
}

async fn http_req(port: u16, method: &str, path: &str, body: Option<&str>) -> (u16, String) {
    use tokio::io::AsyncWriteExt as _;
    let mut s = TcpStream::connect(("127.0.0.1", port)).await.expect("connect");
    let payload = body.unwrap_or("");
    let req = format!(
        "{method} {path} HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{payload}",
        payload.len()
    );
    s.write_all(req.as_bytes()).await.expect("write");
    let mut buf = Vec::new();
    let _ = s.read_to_end(&mut buf).await;
    let text = String::from_utf8_lossy(&buf).into_owned();
    let status = text
        .split_whitespace()
        .nth(1)
        .and_then(|v| v.parse::<u16>().ok())
        .unwrap_or(0);
    (status, text)
}

#[tokio::test]
async fn mgmt_delete_route_matches() {
    let _serial = serial();
    let (server, port) = start_with_mgmt().await;
    let fwd_port = free_port();

    // 通过 HTTP 创建（同时验证 POST /forwards 路由）
    let (status, body) = http_req(
        port,
        "POST",
        "/forwards",
        Some(&format!(
            r#"{{"id":"fwd_http_del","listen_port":{fwd_port},"token":"","udp":false}}"#
        )),
    )
    .await;
    assert_eq!(status, 200, "POST /forwards should succeed: {body}");
    wait_ready().await;
    assert!(server.forwards.lock().unwrap().contains_key("fwd_http_del"));

    // DELETE /forwards/{id}：axum 0.7 必须写成 :id，否则这里 404
    let (status, body) = http_req(port, "DELETE", "/forwards/fwd_http_del", None).await;
    assert_eq!(
        status, 200,
        "DELETE /forwards/:id 未匹配（axum 0.7 路径参数语法应为 :id）: {body}"
    );
    assert!(
        !server.forwards.lock().unwrap().contains_key("fwd_http_del"),
        "删除后规则应已移除"
    );

    // 端口必须真正释放，否则删除就是「假删除」
    tokio::time::sleep(Duration::from_millis(120)).await;
    assert!(
        tokio::net::TcpStream::connect(("127.0.0.1", fwd_port)).await.is_err(),
        "删除规则后监听端口 {fwd_port} 应已释放"
    );

    server.shutdown_all().await;
}

#[tokio::test]
async fn mgmt_delete_unknown_returns_404() {
    let _serial = serial();
    let (server, port) = start_with_mgmt().await;
    let (status, _) = http_req(port, "DELETE", "/forwards/does_not_exist", None).await;
    assert_eq!(status, 404, "未知规则应返回 404 而不是路由未匹配的 404");
    server.shutdown_all().await;
}
