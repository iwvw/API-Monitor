mod config;
use std::fs;
use std::path::PathBuf;
use std::process::Command;
mod cloudflared;
mod collector;
mod docker;
mod file_manager;
mod protocol;
mod proxy_runtime;
mod proxy_traffic;
mod pty;
mod tcp_forwarder;
mod remote_desktop;
mod storage_server;

use futures_util::{SinkExt, StreamExt};
use serde::Deserialize;
use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use tokio::net::TcpStream;
use tokio::sync::mpsc;
use tokio::time::sleep;
use tokio_tungstenite::{
    client_async_with_config, connect_async, tungstenite::protocol::Message, MaybeTlsStream,
};

use crate::collector::{Collector, DockerInfo, State};
use crate::config::{CliArgs, Config};
use crate::docker::DockerBridge;
use crate::file_manager::FileManager;
use crate::protocol::*;
use crate::pty::PtySession;
use crate::remote_desktop::{
    RemoteDesktopManager, SignalPayload as RemoteDesktopSignalPayload,
    StartPayload as RemoteDesktopStartPayload, StopPayload as RemoteDesktopStopPayload,
};
use clap::Parser;

const VERSION: &str = env!("CARGO_PKG_VERSION");

fn agent_capabilities() -> Vec<String> {
    #[allow(unused_mut)]
    let mut capabilities = vec![
        "terminal_stream_v2".to_string(),
        "self_update_v1".to_string(),
        "cloudflared_runtime_v1".to_string(),
        "tcp_forwarder_v1".to_string(),
        "storage_node_v1".to_string(),
    ];
    #[cfg(target_os = "linux")]
    capabilities.push("proxy_runtime_v1".to_string());
    #[cfg(target_os = "linux")]
    capabilities.push("proxy_runtime_lifecycle_v2".to_string());
    #[cfg(target_os = "linux")]
    capabilities.push("proxy_user_traffic_v1".to_string());
    #[cfg(target_os = "linux")]
    capabilities.push("self_uninstall_v1".to_string());
    #[cfg(target_os = "windows")]
    let capabilities = {
        let mut capabilities = capabilities;
        capabilities.push("remote_desktop_v1".to_string());
        capabilities.push("remote_desktop_video_v2".to_string());
        capabilities
    };
    capabilities
}

#[derive(Deserialize, Debug, Clone, Default)]
struct UpgradePayload {
    download_url: Option<String>,
    download_base_url: Option<String>,
}

fn stamp_state(state: &mut State, sequence: u64, sample_interval_ms: u64) {
    state.sequence = sequence;
    state.sample_interval_ms = sample_interval_ms;
}

fn docker_summary(info: &DockerInfo) -> DockerInfo {
    DockerInfo {
        installed: info.installed,
        running: info.running,
        stopped: info.stopped,
        containers: Vec::new(),
    }
}

#[derive(Clone)]
struct OutboundQueues {
    high: mpsc::Sender<String>,
    normal: mpsc::Sender<String>,
    low: mpsc::Sender<String>,
    latest_normal: Arc<Mutex<Option<String>>>,
    dropped_low: Arc<AtomicU64>,
}

impl OutboundQueues {
    fn new() -> (
        Self,
        mpsc::Receiver<String>,
        mpsc::Receiver<String>,
        mpsc::Receiver<String>,
    ) {
        let (high_tx, high_rx) = mpsc::channel::<String>(128);
        let (normal_tx, normal_rx) = mpsc::channel::<String>(128);
        let (low_tx, low_rx) = mpsc::channel::<String>(256);
        (
            Self {
                high: high_tx,
                normal: normal_tx,
                low: low_tx,
                latest_normal: Arc::new(Mutex::new(None)),
                dropped_low: Arc::new(AtomicU64::new(0)),
            },
            high_rx,
            normal_rx,
            low_rx,
        )
    }

    async fn send_high(&self, msg: String) -> Result<(), mpsc::error::SendError<String>> {
        self.high.send(msg).await
    }

    async fn send_normal(&self, msg: String) -> Result<(), mpsc::error::SendError<String>> {
        self.normal.send(msg).await
    }

    fn send_normal_latest(&self, msg: String) {
        match self.normal.try_send(msg) {
            Ok(()) => {}
            Err(mpsc::error::TrySendError::Full(msg)) => {
                *self.latest_normal.lock().unwrap() = Some(msg);
            }
            Err(mpsc::error::TrySendError::Closed(_)) => {}
        }
    }

    fn send_low_lossy(&self, msg: String) {
        match self.low.try_send(msg) {
            Ok(()) => {}
            Err(mpsc::error::TrySendError::Full(_)) => {
                self.dropped_low.fetch_add(1, Ordering::Relaxed);
            }
            Err(mpsc::error::TrySendError::Closed(_)) => {}
        }
    }

    fn is_closed(&self) -> bool {
        self.high.is_closed() || self.normal.is_closed() || self.low.is_closed()
    }

    fn take_latest_normal(&self) -> Option<String> {
        self.latest_normal.lock().unwrap().take()
    }
}

#[tokio::main]
async fn main() {
    let cli = CliArgs::parse();

    if let Some(ref action) = cli.action {
        if let Err(e) = handle_action(action, &cli) {
            eprintln!("执行操作失败: {}", e);
            std::process::exit(1);
        }
        std::process::exit(0);
    }

    if cli.daemon {
        hide_console_window();
    }

    println!("=================================================");
    println!("  API Monitor Agent v{} (Rust)", VERSION);
    println!("=================================================");

    let config = match Config::load(&cli) {
        Ok(c) => c,
        Err(e) => {
            eprintln!("{}", e);
            std::process::exit(1);
        }
    };
    persist_runtime_config(&config);

    println!("  Server:   {}", config.server_url);
    println!("  ServerID: {}", config.server_id);
    println!("  Interval: {}ms", config.report_interval);
    println!("=================================================");

    let collector = Arc::new(tokio::sync::Mutex::new(Collector::new()));
    let docker_bridge = Arc::new(tokio::sync::Mutex::new(DockerBridge::new()));
    let pty_sessions = Arc::new(Mutex::new(HashMap::<String, Arc<PtySession>>::new()));
    let task_progress = Arc::new(Mutex::new(HashMap::<String, TaskProgress>::new()));
    let remote_desktop = Arc::new(RemoteDesktopManager::new());

    #[cfg(unix)]
    tokio::spawn(proxy_traffic::run(config.clone()));

    tokio::spawn(storage_server::run(
        config.storage_port,
        config.agent_key.clone(),
    ));

    // Keep dialing loop. A healthy long-lived connection resets the delay; only
    // repeated short failures back off to avoid hammering the control plane.
    let base_reconnect_delay = Duration::from_millis(config.reconnect_delay.max(250));
    let max_reconnect_delay = Duration::from_secs(30);
    let mut next_reconnect_delay = base_reconnect_delay;
    loop {
        let connected_since = Instant::now();
        println!("[Agent] 正在连接服务器...");
        match run_client(
            config.clone(),
            collector.clone(),
            docker_bridge.clone(),
            pty_sessions.clone(),
            task_progress.clone(),
            remote_desktop.clone(),
        )
        .await
        {
            Ok(_) => {
                println!("[Agent] 连接断开，准备重连...");
            }
            Err(e) => {
                eprintln!("[Agent] 运行错误: {}", e);
            }
        }
        if connected_since.elapsed() >= Duration::from_secs(60) {
            next_reconnect_delay = base_reconnect_delay;
        }
        println!(
            "[Agent] 将在 {}ms 后重连...",
            next_reconnect_delay.as_millis()
        );
        sleep(next_reconnect_delay).await;
        if connected_since.elapsed() < Duration::from_secs(60) {
            next_reconnect_delay =
                std::cmp::min(next_reconnect_delay.saturating_mul(2), max_reconnect_delay);
        } else {
            next_reconnect_delay = base_reconnect_delay;
        }
    }
}

async fn connect_to_host(host: &str, port: u16) -> Result<TcpStream, String> {
    use tokio::net::lookup_host;
    let addrs = lookup_host(format!("{}:{}", host, port))
        .await
        .map_err(|e| format!("DNS 解析失败: {}", e))?;

    let mut addrs: Vec<_> = addrs.collect();
    prefer_ipv4_addresses(&mut addrs);

    let mut last_err = None;
    for addr in addrs {
        match tokio::time::timeout(Duration::from_millis(2500), TcpStream::connect(addr)).await {
            Ok(Ok(stream)) => {
                let _ = stream.set_nodelay(true);
                return Ok(stream);
            }
            Ok(Err(e)) => {
                last_err = Some(format!("连接 {} 失败: {}", addr, e));
            }
            Err(_) => {
                last_err = Some(format!("连接 {} 超时", addr));
            }
        }
    }

    Err(last_err.unwrap_or_else(|| "未找到可用的解析地址".to_string()))
}

fn prefer_ipv4_addresses(addrs: &mut [SocketAddr]) {
    // IPv6 may be advertised even when the local route is a black hole.
    addrs.sort_by_key(|addr| addr.is_ipv6());
}

async fn run_client(
    config: Config,
    collector: Arc<tokio::sync::Mutex<Collector>>,
    docker_bridge: Arc<tokio::sync::Mutex<DockerBridge>>,
    pty_sessions: Arc<Mutex<HashMap<String, Arc<PtySession>>>>,
    task_progress: Arc<Mutex<HashMap<String, TaskProgress>>>,
    remote_desktop: Arc<RemoteDesktopManager>,
) -> Result<(), String> {
    // 直接 WebSocket 连接（无需 polling handshake）
    let mut ws_url = if config.server_url.starts_with("https://") {
        config.server_url.replace("https://", "wss://")
    } else {
        config.server_url.replace("http://", "ws://")
    };
    ws_url = format!("{}/socket.io/?EIO=4&transport=websocket", ws_url);

    if config.debug {
        println!("[Agent] 正在建立 WebSocket 连接: {}", ws_url);
    }

    println!("[Agent] 连接目标: {}", config.server_url);

    let parsed_url = url::Url::parse(&ws_url).map_err(|e| format!("URL 解析失败: {}", e))?;
    let host = parsed_url
        .host_str()
        .ok_or_else(|| "URL 中缺少主机名".to_string())?;
    let port = parsed_url
        .port()
        .unwrap_or(if ws_url.starts_with("wss://") {
            443
        } else {
            80
        });

    let tcp_stream = connect_to_host(host, port).await?;

    // Build a custom TLS config that forces HTTP/1.1 ALPN.
    // Without this, rustls may negotiate HTTP/2, which does not support
    // the traditional WebSocket upgrade mechanism and causes timeouts
    // behind CDN proxies like Cloudflare.
    let maybe_tls_stream = if ws_url.starts_with("wss://") {
        let root_store =
            rustls::RootCertStore::from_iter(webpki_roots::TLS_SERVER_ROOTS.iter().cloned());
        let mut tls_config = rustls::ClientConfig::builder()
            .with_root_certificates(root_store)
            .with_no_client_auth();
        tls_config.alpn_protocols = vec![b"http/1.1".to_vec()];
        let server_name = rustls::pki_types::ServerName::try_from(host.to_owned())
            .map_err(|e| format!("TLS 主机名无效: {}", e))?;
        let tls_stream = tokio_rustls::TlsConnector::from(Arc::new(tls_config))
            .connect(server_name, tcp_stream)
            .await
            .map_err(|e| format!("TLS 握手失败: {}", e))?;
        MaybeTlsStream::Rustls(tls_stream)
    } else {
        MaybeTlsStream::Plain(tcp_stream)
    };

    let (ws_stream, _) = tokio::time::timeout(
        Duration::from_secs(30),
        client_async_with_config(&ws_url, maybe_tls_stream, None),
    )
    .await
    .map_err(|_| "WebSocket 连接超时".to_string())?
    .map_err(|e| format!("WebSocket 连接失败: {}", e))?;

    println!("[Agent] WebSocket 连接已建立");

    let (mut ws_writer, mut ws_reader) = ws_stream.split();

    // Priority queues for multiplexing outbound messages.
    let (outbound, mut high_rx, mut normal_rx, mut low_rx) = OutboundQueues::new();
    let outbound_writer = outbound.clone();

    // Task to write outgoing messages to websocket
    let mut write_task = tokio::spawn(async move {
        loop {
            let msg = if let Ok(msg) = high_rx.try_recv() {
                msg
            } else if let Ok(msg) = normal_rx.try_recv() {
                msg
            } else if let Some(msg) = outbound_writer.take_latest_normal() {
                msg
            } else {
                tokio::select! {
                    biased;
                    msg = high_rx.recv() => match msg {
                        Some(msg) => msg,
                        None => return Err("高优先级发送通道已关闭".to_string()),
                    },
                    msg = normal_rx.recv() => match msg {
                        Some(msg) => msg,
                        None => return Err("普通发送通道已关闭".to_string()),
                    },
                    msg = low_rx.recv() => match msg {
                        Some(msg) => msg,
                        None => return Err("低优先级发送通道已关闭".to_string()),
                    },
                }
            };
            if let Err(err) = ws_writer.send(Message::Text(msg.into())).await {
                return Err(format!("WebSocket 写入失败: {}", err));
            }
        }
    });

    // 等待服务器握手和认证
    let authenticated = Arc::new(tokio::sync::Mutex::new(false));
    let network_targets = Arc::new(tokio::sync::Mutex::new(Vec::<NetworkQualityTarget>::new()));
    let latest_network_quality =
        Arc::new(tokio::sync::Mutex::new(None::<NetworkQualityProbeResponse>));

    // Handle WebSocket receiver loop
    let tx_clone = outbound.clone();
    let auth_ok_tx = outbound.clone();
    let collector_clone = collector.clone();
    let docker_bridge_clone = docker_bridge.clone();
    let pty_sessions_clone = pty_sessions.clone();
    let authenticated_clone = authenticated.clone();
    let config_clone = config.clone();
    let network_targets_clone = network_targets.clone();
    let latest_network_quality_clone = latest_network_quality.clone();
    let remote_desktop_clone = remote_desktop.clone();

    let mut read_task = tokio::spawn(async move {
        while let Some(res) = ws_reader.next().await {
            let text = match res {
                Ok(Message::Text(t)) => t,
                Ok(Message::Ping(_)) => {
                    if config_clone.debug {
                        println!("[Agent] 收到 WebSocket Ping");
                    }
                    continue;
                }
                Ok(Message::Pong(_)) => {
                    if config_clone.debug {
                        println!("[Agent] 收到 WebSocket Pong");
                    }
                    continue;
                }
                Ok(Message::Close(frame)) => {
                    println!("[Agent] 收到 Close 帧");
                    return Err(format!("收到 WebSocket Close 帧: {:?}", frame));
                }
                Err(e) => {
                    eprintln!("[Agent] WebSocket 读取错误: {}", e);
                    return Err(format!("WebSocket 读取错误: {}", e));
                }
                _ => continue,
            };
            if config_clone.debug && text.as_str() != "2" && text.as_str() != "3" {
                println!("[Agent] 收到原始消息: {}", text);
            }

            let msg = parse_socketio_message(&text);
            match msg {
                SocketIOMessage::Ping => {
                    let _ = tx_clone.send_high("3".to_string()).await;
                }
                SocketIOMessage::Event(event, data) => {
                    if event == EVENT_DASHBOARD_AUTH_OK {
                        println!("[Agent] ✅ 认证成功");
                        *authenticated_clone.lock().await = true;

                        // Parse network_targets from auth payload if present
                        if let Some(targets_val) = data.get("network_targets") {
                            if let Ok(targets) = serde_json::from_value::<Vec<NetworkQualityTarget>>(
                                targets_val.clone(),
                            ) {
                                *network_targets_clone.lock().await = targets;
                            }
                        }

                        // Start loops for reports
                        let auth_tx = auth_ok_tx.clone();
                        let collector_loop = collector_clone.clone();
                        let docker_loop = docker_bridge_clone.clone();
                        let cfg = config_clone.clone();
                        let network_targets_probe = network_targets_clone.clone();
                        let latest_network_quality_probe = latest_network_quality_clone.clone();

                        tokio::spawn(async move {
                            let docker_cache =
                                Arc::new(tokio::sync::Mutex::new(DockerInfo::default()));
                            {
                                let docker_refresh = docker_loop.clone();
                                let docker_cache_refresh = docker_cache.clone();
                                let docker_tx = auth_tx.clone();
                                tokio::spawn(async move {
                                    let mut docker_timer =
                                        tokio::time::interval(Duration::from_secs(60));
                                    docker_timer.set_missed_tick_behavior(
                                        tokio::time::MissedTickBehavior::Delay,
                                    );
                                    docker_timer.tick().await;

                                    loop {
                                        if docker_tx.is_closed() {
                                            break;
                                        }

                                        let info =
                                            docker_refresh.lock().await.collect_docker_info().await;
                                        *docker_cache_refresh.lock().await = info;
                                        docker_timer.tick().await;
                                    }
                                });
                            }

                            // Spawn network quality probing loop
                            {
                                let probe_targets = network_targets_probe.clone();
                                let probe_quality = latest_network_quality_probe.clone();
                                let probe_tx = auth_tx.clone();
                                tokio::spawn(async move {
                                    let mut interval =
                                        tokio::time::interval(Duration::from_secs(60));
                                    interval.set_missed_tick_behavior(
                                        tokio::time::MissedTickBehavior::Delay,
                                    );
                                    interval.tick().await; // initial tick
                                    loop {
                                        if probe_tx.is_closed() {
                                            break;
                                        }
                                        let targets = {
                                            let t = probe_targets.lock().await;
                                            t.clone()
                                        };
                                        if !targets.is_empty() {
                                            let mut handles = Vec::new();
                                            for target in targets {
                                                handles.push(tokio::spawn(async move {
                                                    probe_network_quality_target(target, 4000).await
                                                }));
                                            }
                                            let mut results = Vec::new();
                                            for h in handles {
                                                if let Ok(res) = h.await {
                                                    results.push(res);
                                                }
                                            }
                                            let resp = NetworkQualityProbeResponse {
                                                checked_at: chrono::Utc::now().to_rfc3339(),
                                                results,
                                            };
                                            *probe_quality.lock().await = Some(resp);
                                        }
                                        interval.tick().await;
                                    }
                                });
                            }

                            let mut state_sequence = 0_u64;

                            // First run
                            let host_info =
                                collector_loop.lock().await.collect_host_info(VERSION).await;
                            let _ = auth_tx
                                .send_normal(format_event(EVENT_AGENT_HOST_INFO, &host_info))
                                .await;

                            let mut state = collector_loop.lock().await.collect_state();
                            stamp_state(&mut state, state_sequence, cfg.report_interval);
                            state_sequence = state_sequence.wrapping_add(1);
                            state.docker = {
                                let docker = docker_cache.lock().await;
                                docker_summary(&*docker)
                            };
                            state.network_quality =
                                latest_network_quality_probe.lock().await.take();
                            auth_tx.send_normal_latest(format_event(EVENT_AGENT_STATE, &state));

                            let mut state_timer =
                                tokio::time::interval(Duration::from_millis(cfg.report_interval));
                            let mut host_timer = tokio::time::interval(Duration::from_millis(
                                cfg.host_info_interval,
                            ));
                            let mut upgrade_timer = tokio::time::interval(Duration::from_secs(2));

                            // Prevent tick stacking
                            state_timer
                                .set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
                            host_timer
                                .set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
                            upgrade_timer
                                .set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
                            state_timer.tick().await;
                            host_timer.tick().await;

                            loop {
                                tokio::select! {
                                    _ = state_timer.tick() => {
                                        if auth_tx.is_closed() {
                                            break;
                                        }
                                        let mut state = collector_loop.lock().await.collect_state();
                                        stamp_state(&mut state, state_sequence, cfg.report_interval);
                                        state_sequence = state_sequence.wrapping_add(1);
                                        state.docker = {
                                            let docker = docker_cache.lock().await;
                                            docker_summary(&*docker)
                                        };
                                        state.network_quality = latest_network_quality_probe.lock().await.take();
                                        auth_tx.send_normal_latest(format_event(EVENT_AGENT_STATE, &state));
                                    }
                                    _ = host_timer.tick() => {
                                        let host_info = collector_loop.lock().await.collect_host_info(VERSION).await;
                                        if auth_tx.send_normal(format_event(EVENT_AGENT_HOST_INFO, &host_info)).await.is_err() {
                                            break;
                                        }
                                    }
                                    _ = upgrade_timer.tick() => {
                                        // 后台自更新脚本完成后会写结果文件，读取并上报给面板
                                        if let Some(status) = take_upgrade_status_file() {
                                            if auth_tx.send_normal(format_event(EVENT_AGENT_UPGRADE_STATUS, &status)).await.is_err() {
                                                break;
                                            }
                                        }
                                    }
                                }
                            }
                        });
                    } else if event == "dashboard:network_targets_update" {
                        if let Ok(targets) =
                            serde_json::from_value::<Vec<NetworkQualityTarget>>(data)
                        {
                            let len = targets.len();
                            *network_targets_clone.lock().await = targets;
                            println!("[Agent] 📶 收到服务端更新的拨测目标，共 {} 个", len);
                        }
                    } else if event == EVENT_DASHBOARD_AUTH_FAIL
                        || event == EVENT_DASHBOARD_AUTH_ERROR
                    {
                        let reason = data
                            .get("reason")
                            .or_else(|| data.get("message"))
                            .and_then(|v| v.as_str())
                            .unwrap_or("未知")
                            .to_string();
                        eprintln!("[Agent] ❌ 认证失败: {}", reason);
                        std::process::exit(1);
                    } else if event == EVENT_DASHBOARD_REMOTE_DESKTOP_START {
                        if let Ok(payload) =
                            serde_json::from_value::<RemoteDesktopStartPayload>(data)
                        {
                            let manager = remote_desktop_clone.clone();
                            let outbound = tx_clone.clone();
                            let session_id = payload.session_id.clone();
                            if let Err(error) = manager.start(payload, outbound.clone()).await {
                                let event = serde_json::json!({
                                    "session_id": session_id,
                                    "state": "error",
                                    "signal": {"kind": "error", "message": error},
                                });
                                let _ = outbound
                                    .send_normal(format_event(
                                        EVENT_AGENT_REMOTE_DESKTOP_SIGNAL,
                                        &event,
                                    ))
                                    .await;
                            }
                        }
                    } else if event == EVENT_DASHBOARD_REMOTE_DESKTOP_SIGNAL {
                        if let Ok(payload) =
                            serde_json::from_value::<RemoteDesktopSignalPayload>(data)
                        {
                            let manager = remote_desktop_clone.clone();
                            let _ = manager.signal(payload).await;
                        }
                    } else if event == EVENT_DASHBOARD_REMOTE_DESKTOP_STOP {
                        if let Ok(payload) =
                            serde_json::from_value::<RemoteDesktopStopPayload>(data)
                        {
                            let manager = remote_desktop_clone.clone();
                            manager.stop(&payload.session_id).await;
                        }
                    } else if event == EVENT_DASHBOARD_SERVER_ACTION {
                        // 服务器动作（重启/关机等），由面板 /api/server/action 下发
                        if let Some(action) = data.get("action").and_then(|v| v.as_str()) {
                            let action = action.to_string();
                            tokio::spawn(async move {
                                match handle_server_action(&action).await {
                                    Ok(msg) => println!("[Action] {}: {}", action, msg),
                                    Err(err) => eprintln!("[Action] {} 执行失败: {}", action, err),
                                }
                            });
                        }
                    } else if event == EVENT_DASHBOARD_TASK {
                        if let Ok(task) = serde_json::from_value::<TaskPayload>(data) {
                            let tx_task = tx_clone.clone();
                            let docker_bridge_task = docker_bridge_clone.clone();
                            let pty_sessions_task = pty_sessions_clone.clone();
                            let config_task = config_clone.clone();
                            let task_progress_task = task_progress.clone();

                            tokio::spawn(async move {
                                let start_time = Instant::now();
                                let mut successful = false;
                                let res_data;

                                match task.task_type {
                                    1 => {
                                        // COMMAND
                                        match execute_command(&task.data, task.timeout as u64).await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    10 => {
                                        // DOCKER_ACTION
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_action(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    11 => {
                                        // DOCKER_CHECK_UPDATE
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_check_update(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    13 => {
                                        // DOCKER_IMAGES
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_images_api(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    14 => {
                                        // DOCKER_IMAGE_ACTION
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_image_action_api(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    15 => {
                                        // DOCKER_NETWORKS
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_networks_api(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    16 => {
                                        // DOCKER_NETWORK_ACTION
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_network_action_api(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    17 => {
                                        // DOCKER_VOLUMES
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_volumes_api(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    18 => {
                                        // DOCKER_VOLUME_ACTION
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_volume_action_api(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    19 => {
                                        // DOCKER_LOGS
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_logs_api(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    20 => {
                                        // DOCKER_STATS
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_stats_api(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    21 => {
                                        // DOCKER_COMPOSE_LIST
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_compose_list(&task.data)
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    22 => {
                                        // DOCKER_COMPOSE_ACTION
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_compose_action(&task.data)
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    23 => {
                                        // DOCKER_CREATE_CONTAINER
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_create_container_api(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    24 => {
                                        // DOCKER_UPDATE_CONTAINER
                                        let tx_clone_inner = tx_task.clone();
                                        let docker_bridge_inner = docker_bridge_task.clone();
                                        let task_id = task.id.clone();
                                        let task_data = task.data.clone();
                                        let task_progress_inner = task_progress_task.clone();
                                        tokio::spawn(async move {
                                            handle_docker_container_update(
                                                task_id,
                                                task_data,
                                                task_progress_inner,
                                                tx_clone_inner,
                                                docker_bridge_inner,
                                            )
                                            .await;
                                        });
                                        successful = true;
                                        res_data = "容器更新任务已启动".to_string();
                                    }
                                    25 => {
                                        // DOCKER_RENAME_CONTAINER
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_rename_container_api(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    26 => {
                                        // DOCKER_TASK_PROGRESS
                                        match get_task_progress(&task.data, task_progress_task)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    27 => {
                                        // DOCKER_CONTAINERS
                                        match docker_bridge_task
                                            .lock()
                                            .await
                                            .handle_docker_containers(&task.data)
                                            .await
                                        {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    30 => {
                                        // FILE_LIST
                                        match FileManager::handle_file_list(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    31 => {
                                        // FILE_READ
                                        match FileManager::handle_file_read(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    32 => {
                                        // FILE_WRITE
                                        match FileManager::handle_file_write(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    33 => {
                                        // FILE_MKDIR
                                        match FileManager::handle_file_mkdir(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    34 => {
                                        // FILE_DELETE
                                        match FileManager::handle_file_delete(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    35 => {
                                        // FILE_RENAME
                                        match FileManager::handle_file_rename(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    36 => {
                                        // FILE_STAT
                                        match FileManager::handle_file_stat(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    37 => {
                                        // FILE_CHMOD
                                        match FileManager::handle_file_chmod(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    38 => {
                                        // FILE_DOWNLOAD_CHUNK
                                        match FileManager::handle_file_download_chunk(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    40 => {
                                        // NETWORK_QUALITY_PROBE
                                        match handle_network_quality_probe(&task.data).await {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    50 => match proxy_runtime::reconcile(&task.data) {
                                        Ok(out) => {
                                            successful = true;
                                            res_data = out;
                                        }
                                        Err(err) => {
                                            res_data = err;
                                        }
                                    },
                                    51 => match cloudflared::reconcile(&task.data) {
                                        Ok(out) => {
                                            successful = true;
                                            res_data = out;
                                        }
                                        Err(err) => {
                                            res_data = err;
                                        }
                                    },
                                    52 => match schedule_self_uninstall() {
                                        Ok(()) => {
                                            successful = true;
                                            res_data = "Agent 卸载任务已在后台安排".to_string();
                                        }
                                        Err(err) => {
                                            res_data = err;
                                        }
                                    },
                                    53 => match tcp_forwarder::reconcile(&task.data).await {
                                        Ok(out) => {
                                            successful = true;
                                            res_data = out;
                                        }
                                        Err(err) => {
                                            res_data = err;
                                        }
                                    },
                                    5 => {
                                        // UPGRADE
                                        match handle_upgrade(&task.id, &task.data, &config_task)
                                            .await
                                        {
                                            Ok(message) => {
                                                successful = true;
                                                res_data = message;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    12 => {
                                        // PTY_START
                                        let _ = handle_pty_start(
                                            &task.id,
                                            &task.data,
                                            pty_sessions_task,
                                            tx_task.clone(),
                                            config_task.clone(),
                                        )
                                        .await;
                                        return; // PTY runs in background long connection, does not yield instant TaskResult
                                    }
                                    _ => {
                                        res_data = format!("不支持的任务类型: {}", task.task_type);
                                    }
                                }

                                let delay = start_time.elapsed().as_millis() as i64;
                                let res_payload = TaskResultPayload {
                                    id: task.id.clone(),
                                    task_type: task.task_type,
                                    successful,
                                    data: res_data,
                                    delay,
                                };
                                let _ = tx_task
                                    .send_normal(format_event(
                                        EVENT_AGENT_TASK_RESULT,
                                        &res_payload,
                                    ))
                                    .await;
                            });
                        }
                    } else if event == EVENT_DASHBOARD_PTY_INPUT {
                        if let Ok(input) = serde_json::from_value::<PtyInputPayload>(data) {
                            if let Some(session) = pty_sessions_clone.lock().unwrap().get(&input.id)
                            {
                                let _ = session.write(input.data.as_bytes());
                            }
                        }
                    } else if event == EVENT_DASHBOARD_PTY_RESIZE {
                        if let Ok(resize) = serde_json::from_value::<PtyResizePayload>(data) {
                            if let Some(session) =
                                pty_sessions_clone.lock().unwrap().get(&resize.id)
                            {
                                let _ = session.resize(resize.cols, resize.rows);
                            }
                        }
                    } else if event == EVENT_DASHBOARD_PTY_STOP {
                        if let Ok(stop) = serde_json::from_value::<PtyStopPayload>(data) {
                            pty_sessions_clone.lock().unwrap().remove(&stop.id);
                        }
                    }
                }
                SocketIOMessage::Raw(r) => {
                    // 处理服务器握手包
                    if r.starts_with('0') {
                        if config_clone.debug {
                            println!("[Agent] 收到握手包: {}", r);
                        }
                        // 发送 Socket.IO CONNECT
                        let _ = tx_clone.send_high("40".to_string()).await;
                        if config_clone.debug {
                            println!("[Agent] 发送 CONNECT: 40");
                        }
                    } else if r == "40{}" || r.starts_with("40{") {
                        // CONNECT ACK 收到，发送认证
                        if config_clone.debug {
                            println!("[Agent] 收到 CONNECT ACK: {}", r);
                        }

                        let hostname = hostname::get()
                            .map(|h| h.to_string_lossy().to_string())
                            .unwrap_or_else(|_| "unknown".to_string());

                        let auth = AuthPayload {
                            server_id: config_clone.server_id.clone(),
                            key: config_clone.agent_key.clone(),
                            hostname,
                            version: VERSION.to_string(),
                            platform: std::env::consts::OS.to_string(),
                            arch: normalized_agent_arch(),
                            capabilities: agent_capabilities(),
                        };

                        let msg = format_event(EVENT_AGENT_CONNECT, &auth);
                        let _ = tx_clone.send_high(msg).await;

                        if config_clone.debug {
                            println!("[Agent] 已发送认证信息");
                        }
                    }
                }
                _ => {}
            }
        }
        Err("WebSocket 读取循环结束".to_string())
    });

    // Run both tasks concurrently until connection breaks
    let conn_err = tokio::select! {
        result = &mut write_task => {
            read_task.abort();
            match result {
                Ok(Err(err)) => Err(err),
                Ok(Ok(())) => Err("写入任务已结束".to_string()),
                Err(err) => Err(format!("写入任务异常退出: {}", err)),
            }
        }
        result = &mut read_task => {
            write_task.abort();
            match result {
                Ok(Err(err)) => Err(err),
                Ok(Ok(())) => Err("读取任务已结束".to_string()),
                Err(err) => Err(format!("读取任务异常退出: {}", err)),
            }
        }
    };

    // 主连接断开：残留的 PTY shell 已无任何管理方（浏览器通道随连接失效），
    // 全部终止，防止孤儿进程与泄漏（Drop 会 kill 子进程）。
    let stale: Vec<String> = pty_sessions
        .lock()
        .unwrap()
        .keys()
        .cloned()
        .collect();
    for key in stale {
        pty_sessions.lock().unwrap().remove(&key);
    }
    conn_err
}

// Helpers

// handle_server_action 处理面板下发的服务器动作（reboot/restart/shutdown）。
async fn handle_server_action(action: &str) -> Result<String, String> {
    let command: &str = match action {
        "reboot" | "restart" => {
            if cfg!(target_os = "windows") {
                "shutdown /r /t 0"
            } else {
                "shutdown -r now"
            }
        }
        "shutdown" => {
            if cfg!(target_os = "windows") {
                "shutdown /s /t 0"
            } else {
                "shutdown -h now"
            }
        }
        other => return Err(format!("不支持的动作: {}", other)),
    };
    // Linux 下 Agent 可能非 root：先直接执行，失败再尝试 sudo
    let result = execute_command(command, 15).await;
    if result.is_err() && !cfg!(target_os = "windows") {
        execute_command(&format!("sudo {}", command), 15).await
    } else {
        result
    }
}

async fn execute_command(command: &str, timeout_secs: u64) -> Result<String, String> {
    if command.is_empty() {
        return Err("命令不能为空".to_string());
    }

    let mut cmd = if cfg!(target_os = "windows") {
        let mut c = tokio::process::Command::new("cmd");
        c.args(["/C", command]);
        c
    } else {
        let mut c = tokio::process::Command::new("sh");
        c.args(["-c", command]);
        c
    };

    let child = cmd
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn();

    let mut child = match child {
        Ok(c) => c,
        Err(e) => return Err(format!("Spawn failed: {}", e)),
    };

    let timeout_duration = if timeout_secs == 0 { 60 } else { timeout_secs };
    let sleep_timer = tokio::time::sleep(Duration::from_secs(timeout_duration));

    tokio::select! {
        res = child.wait() => {
            match res {
                Ok(status) => {
                    let mut stdout_str = String::new();
                    if let Some(mut out) = child.stdout.take() {
                        let _ = tokio::io::AsyncReadExt::read_to_string(&mut out, &mut stdout_str).await;
                    }
                    let mut stderr_str = String::new();
                    if let Some(mut err) = child.stderr.take() {
                        let _ = tokio::io::AsyncReadExt::read_to_string(&mut err, &mut stderr_str).await;
                    }
                    let combined = stdout_str + &stderr_str;
                    if status.success() {
                        Ok(combined)
                    } else {
                        Err(combined)
                    }
                }
                Err(e) => Err(format!("Wait failed: {}", e)),
            }
        }
        _ = sleep_timer => {
            let _ = child.kill().await;
            Err("Command timed out".to_string())
        }
    }
}

#[derive(Deserialize, Debug)]
struct NetworkQualityProbeRequest {
    targets: Vec<NetworkQualityTarget>,
    timeout_ms: Option<u64>,
}

async fn handle_network_quality_probe(data: &str) -> Result<String, String> {
    let request: NetworkQualityProbeRequest =
        serde_json::from_str(data).map_err(|e| format!("解析网络质量探测请求失败: {}", e))?;
    if request.targets.is_empty() {
        return Err("网络质量探测目标为空".to_string());
    }

    let timeout_ms = request.timeout_ms.unwrap_or(2500).clamp(200, 10000);
    let targets: Vec<NetworkQualityTarget> = request.targets.into_iter().take(12).collect();
    let mut handles = Vec::new();

    for target in targets {
        handles.push(tokio::spawn(async move {
            probe_network_quality_target(target, timeout_ms).await
        }));
    }

    let mut results = Vec::new();
    for handle in handles {
        match handle.await {
            Ok(result) => results.push(result),
            Err(err) => results.push(NetworkQualityProbeResult {
                id: None,
                name: "unknown".to_string(),
                host: "".to_string(),
                port: 0,
                success: false,
                latency_ms: None,
                error: Some(format!("探测任务失败: {}", err)),
            }),
        }
    }

    let response = NetworkQualityProbeResponse {
        checked_at: chrono::Utc::now().to_rfc3339(),
        results,
    };
    serde_json::to_string(&response).map_err(|e| format!("序列化网络质量探测结果失败: {}", e))
}

async fn probe_network_quality_target(
    target: NetworkQualityTarget,
    timeout_ms: u64,
) -> NetworkQualityProbeResult {
    let port = target.port.unwrap_or(80);
    let started = Instant::now();
    let connect = TcpStream::connect((target.host.as_str(), port));

    match tokio::time::timeout(Duration::from_millis(timeout_ms), connect).await {
        Ok(Ok(stream)) => {
            drop(stream);
            NetworkQualityProbeResult {
                id: target.id,
                name: target.name,
                host: target.host,
                port,
                success: true,
                latency_ms: Some(started.elapsed().as_secs_f64() * 1000.0),
                error: None,
            }
        }
        Ok(Err(err)) => NetworkQualityProbeResult {
            id: target.id,
            name: target.name,
            host: target.host,
            port,
            success: false,
            latency_ms: None,
            error: Some(err.to_string()),
        },
        Err(_) => NetworkQualityProbeResult {
            id: target.id,
            name: target.name,
            host: target.host,
            port,
            success: false,
            latency_ms: None,
            error: Some("TCP connect timeout".to_string()),
        },
    }
}

async fn handle_upgrade(_task_id: &str, data: &str, config: &Config) -> Result<String, String> {
    sleep(Duration::from_secs(1)).await;
    println!("[Upgrade] 开始安排自更新流程...");

    let payload = serde_json::from_str::<UpgradePayload>(data).unwrap_or_default();
    let download_url = resolve_upgrade_download_url(config, &payload)?;
    schedule_self_update(&download_url)?;

    Ok(format!("自更新已在后台安排，下载地址: {}", download_url))
}

fn resolve_upgrade_download_url(
    config: &Config,
    payload: &UpgradePayload,
) -> Result<String, String> {
    if let Some(raw) = payload.download_url.as_deref() {
        return validate_download_url(raw);
    }

    let base = payload
        .download_base_url
        .as_deref()
        .map(str::trim)
        .filter(|v| !v.is_empty())
        .map(|v| v.trim_end_matches('/').to_string())
        .unwrap_or_else(|| format!("{}/agent", config.server_url.trim_end_matches('/')));

    validate_download_url(&format!("{}/{}", base, agent_binary_filename()?))
}

fn validate_download_url(raw: &str) -> Result<String, String> {
    let value = raw.trim();
    let parsed = url::Url::parse(value).map_err(|e| format!("升级下载地址无效: {}", e))?;
    match parsed.scheme() {
        "http" | "https" => Ok(value.to_string()),
        other => Err(format!("不支持的升级下载协议: {}", other)),
    }
}

fn agent_binary_filename() -> Result<&'static str, String> {
    if cfg!(target_os = "windows") {
        return Ok("agent-windows-amd64.exe");
    }
    match normalized_agent_arch().as_str() {
        "amd64" => Ok("agent-linux-amd64"),
        "arm64" => Ok("agent-linux-arm64"),
        other => Err(format!("不支持的 Agent 架构: {}", other)),
    }
}

fn normalized_agent_arch() -> String {
    match std::env::consts::ARCH {
        "x86_64" | "amd64" => "amd64".to_string(),
        "aarch64" | "arm64" => "arm64".to_string(),
        other => other.to_string(),
    }
}

// upgrade_result_file_path 后台自更新脚本写入结果文件的固定路径。
fn upgrade_result_file_path() -> PathBuf {
    if cfg!(target_os = "windows") {
        std::env::temp_dir().join("api-monitor-agent-upgrade-result.json")
    } else {
        PathBuf::from("/tmp/api-monitor-agent-upgrade-result.json")
    }
}

// take_upgrade_status_file 读取自更新结果文件（脚本下载/替换成功或失败后写入），
// 读后删除，返回 {state, version|error} 供主循环上报面板。
fn take_upgrade_status_file() -> Option<serde_json::Value> {
    let path = upgrade_result_file_path();
    let raw = fs::read_to_string(&path).ok()?;
    let value: serde_json::Value = serde_json::from_str(&raw).ok()?;
    let _ = fs::remove_file(&path);
    Some(value)
}

fn schedule_self_update(download_url: &str) -> Result<(), String> {
    let exe_path =
        std::env::current_exe().map_err(|e| format!("获取当前 Agent 路径失败: {}", e))?;
    let timestamp_ms = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();

    if cfg!(target_os = "windows") {
        schedule_self_update_windows(&exe_path, download_url, timestamp_ms)
    } else {
        schedule_self_update_unix(&exe_path, download_url, timestamp_ms)
    }
}

#[cfg(target_os = "linux")]
fn schedule_self_uninstall() -> Result<(), String> {
    let exe_path =
        std::env::current_exe().map_err(|e| format!("获取当前 Agent 路径失败: {}", e))?;
    let install_dir = exe_path
        .parent()
        .ok_or_else(|| "无法获取 Agent 安装目录".to_string())?;
    validate_self_uninstall_dir(install_dir)?;

    let timestamp_ms = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();
    let script_path =
        std::env::temp_dir().join(format!("api-monitor-agent-uninstall-{}.sh", timestamp_ms));
    let unit_name = format!("api-monitor-agent-uninstall-{}", timestamp_ms);
    let agent_pid = std::process::id();
    let script = render_self_uninstall_script(install_dir, &script_path, agent_pid);
    fs::write(&script_path, script).map_err(|e| format!("写入卸载脚本失败: {}", e))?;

    if let Ok(status) = Command::new("systemd-run")
        .args([
            "--unit",
            &unit_name,
            "--collect",
            "--quiet",
            "sh",
            &script_path.to_string_lossy(),
        ])
        .status()
    {
        if status.success() {
            return Ok(());
        }
    }

    Command::new("sh")
        .args([
            "-c",
            &format!(
                "nohup sh {} >/tmp/api-monitor-agent-uninstall.log 2>&1 &",
                sh_quote(&script_path.to_string_lossy())
            ),
        ])
        .spawn()
        .map_err(|e| format!("启动 Agent 卸载进程失败: {}", e))?;
    Ok(())
}

#[cfg(target_os = "linux")]
fn validate_self_uninstall_dir(install_dir: &std::path::Path) -> Result<(), String> {
    let expected = std::path::Path::new("/opt/api-monitor-agent");
    if install_dir != expected {
        return Err(format!(
            "拒绝清理非标准 Agent 安装目录: {}",
            install_dir.display()
        ));
    }
    Ok(())
}

#[cfg(target_os = "linux")]
fn render_self_uninstall_script(
    install_dir: &std::path::Path,
    script_path: &std::path::Path,
    agent_pid: u32,
) -> String {
    format!(
        r#"#!/bin/sh
set -eu
LOG="/tmp/api-monitor-agent-uninstall.log"
exec >>"$LOG" 2>&1
echo "API Monitor Agent uninstall started at $(date -Is)"
INSTALL_DIR={install_dir}
SCRIPT_PATH={script_path}
AGENT_PID={agent_pid}

if [ "$(id -u)" -eq 0 ]; then
    SUDO=""
elif command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
else
    echo "Error: Agent uninstall needs root or sudo"
    exit 1
fi

case "$INSTALL_DIR" in
    "/opt/api-monitor-agent") ;;
    *)
        echo "Error: unsafe Agent install directory"
        exit 1
        ;;
esac

# Give the Agent enough time to return the accepted task result before its
# control connection is stopped.
sleep 2
if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files api-monitor-agent.service >/dev/null 2>&1; then
    # Defensive cleanup: the control plane normally removes these resources
    # first, but self-uninstall must never leave a publishable proxy service
    # running if the orchestration sequence was interrupted.
    for UNIT_PATH in /etc/systemd/system/api-monitor-proxy@*.service; do
        [ -e "$UNIT_PATH" ] || continue
        UNIT_NAME="$(basename "$UNIT_PATH")"
        $SUDO systemctl disable --now "$UNIT_NAME" || true
        $SUDO rm -f -- "$UNIT_PATH"
    done
    $SUDO systemctl disable --now api-monitor-cloudflared.service 2>/dev/null || true
    $SUDO rm -f /etc/systemd/system/api-monitor-cloudflared.service
    $SUDO rm -rf -- /opt/api-monitor/proxy /etc/api-monitor/proxy /var/lib/api-monitor/proxy
    $SUDO rm -rf -- /opt/api-monitor/cloudflared /etc/api-monitor/cloudflared
    # Remove only the now-empty managed parent. rmdir deliberately preserves it
    # if another API Monitor component still owns files below it.
    $SUDO rmdir /opt/api-monitor 2>/dev/null || true

    # Remove the durable installation before stopping the running process. The
    # control plane treats the socket disconnect as the uninstall confirmation,
    # so the unit and install directory must already be gone at that point.
    $SUDO systemctl disable api-monitor-agent.service || true
    $SUDO rm -f /etc/systemd/system/api-monitor-agent.service /usr/lib/systemd/system/api-monitor-agent.service
    $SUDO rm -rf -- "$INSTALL_DIR"
    $SUDO systemctl stop api-monitor-agent.service || true
    $SUDO systemctl daemon-reload || true
    $SUDO systemctl reset-failed api-monitor-agent.service 2>/dev/null || true
else
    $SUDO rm -rf -- "$INSTALL_DIR"
    $SUDO kill "$AGENT_PID" 2>/dev/null || true
fi

rm -f -- "$SCRIPT_PATH"
echo "API Monitor Agent uninstall completed at $(date -Is)"
"#,
        install_dir = sh_quote(&install_dir.to_string_lossy()),
        script_path = sh_quote(&script_path.to_string_lossy()),
        agent_pid = agent_pid,
    )
}

#[cfg(not(target_os = "linux"))]
fn schedule_self_uninstall() -> Result<(), String> {
    Err("当前平台暂不支持 Agent 自卸载".to_string())
}

#[cfg(target_os = "windows")]
fn schedule_self_update_windows(
    exe_path: &std::path::Path,
    download_url: &str,
    timestamp_ms: u128,
) -> Result<(), String> {
    let install_dir = exe_path
        .parent()
        .ok_or_else(|| "无法获取 Agent 安装目录".to_string())?;
    let script_path =
        std::env::temp_dir().join(format!("api-monitor-agent-upgrade-{}.ps1", timestamp_ms));
    // 注意：下载临时文件名必须以 .exe 结尾。PowerShell 5.1 无法直接运行
    // 非 .exe 后缀的文件（如 .download），`& $TempAgentPath --version` 会报
    // "Cannot run a document in the middle of a pipeline"，导致自更新中止。
    let temp_agent = install_dir.join("api-monitor-agent.new.exe");
    let launch_vbs = install_dir.join("launch.vbs");
    let agent_pid = std::process::id();

    let script = format!(
        r#"$ErrorActionPreference = "Stop"
$LogPath = Join-Path $env:TEMP "api-monitor-agent-upgrade.log"
Start-Transcript -Path $LogPath -Append | Out-Null
Write-Host "API Monitor Agent self-update started at $(Get-Date -Format o)"
$AgentPath = {agent_path}
$TempAgentPath = {temp_agent}
$DownloadUrl = {download_url}
$LaunchVbs = {launch_vbs}
$AgentPid = {agent_pid}
$ResultPath = Join-Path $env:TEMP "api-monitor-agent-upgrade-result.json"

function Write-UpgradeResult([string]$state, [string]$detail) {{
    $payload = @{{ state = $state; error = $detail }}
    if ($state -eq "succeeded") {{
        Remove-Item $payload.error -ErrorAction SilentlyContinue
        $payload = @{{ state = $state; version = $detail }}
    }}
    try {{
        Set-Content -Path $ResultPath -Value ($payload | ConvertTo-Json -Compress) -Encoding UTF8 -ErrorAction SilentlyContinue
    }} catch {{}}
}}

try {{
    if (Test-Path $TempAgentPath) {{
        Remove-Item -Path $TempAgentPath -Force -ErrorAction SilentlyContinue
    }}
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $TempAgentPath -UseBasicParsing
    $newVersion = (& $TempAgentPath --version 2>&1 | Select-Object -Last 1)

    Start-Sleep -Seconds 2
    $running = Get-Process -Id $AgentPid -ErrorAction SilentlyContinue
    if ($running) {{
        Write-Host "Stopping current Agent PID $AgentPid"
        Stop-Process -Id $AgentPid -Force -ErrorAction SilentlyContinue
        Wait-Process -Id $AgentPid -Timeout 10 -ErrorAction SilentlyContinue
    }}

    for ($i = 0; $i -lt 20; $i++) {{
        if (!(Get-Process -Id $AgentPid -ErrorAction SilentlyContinue)) {{
            break
        }}
        Start-Sleep -Milliseconds 500
    }}

    if (Get-Process -Id $AgentPid -ErrorAction SilentlyContinue) {{
        throw "current Agent process did not stop"
    }}

    if (Test-Path $AgentPath) {{
        for ($i = 0; $i -lt 20; $i++) {{
            try {{
                Remove-Item -Path $AgentPath -Force
                break
            }} catch {{
                if ($i -eq 19) {{ throw }}
                Start-Sleep -Milliseconds 500
            }}
        }}
    }}
    Move-Item -Path $TempAgentPath -Destination $AgentPath -Force

    if (Test-Path $LaunchVbs) {{
        Start-Process -FilePath "wscript.exe" -ArgumentList "`"$LaunchVbs`"" -WindowStyle Hidden
    }} else {{
        Start-Process -FilePath $AgentPath -ArgumentList "-b" -WindowStyle Hidden
    }}
    Write-UpgradeResult "succeeded" "$newVersion"
    Write-Host "API Monitor Agent self-update completed"
}} catch {{
    Write-UpgradeResult "failed" $_.Exception.Message
    Write-Host "API Monitor Agent self-update failed: $_"
    exit 1
}} finally {{
    Stop-Transcript | Out-Null
}}
"#,
        agent_path = ps_quote(&exe_path.to_string_lossy()),
        temp_agent = ps_quote(&temp_agent.to_string_lossy()),
        download_url = ps_quote(download_url),
        launch_vbs = ps_quote(&launch_vbs.to_string_lossy()),
        agent_pid = agent_pid,
    );
    fs::write(&script_path, script).map_err(|e| format!("写入升级脚本失败: {}", e))?;
    Command::new("powershell")
        .args([
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-WindowStyle",
            "Hidden",
            "-File",
            &script_path.to_string_lossy(),
        ])
        .spawn()
        .map_err(|e| format!("启动 Windows 自更新进程失败: {}", e))?;
    Ok(())
}

#[cfg(not(target_os = "windows"))]
fn schedule_self_update_windows(
    _exe_path: &std::path::Path,
    _download_url: &str,
    _timestamp_ms: u128,
) -> Result<(), String> {
    Err("Windows self-update is not available on this platform".to_string())
}

#[cfg(not(target_os = "windows"))]
fn schedule_self_update_unix(
    exe_path: &std::path::Path,
    download_url: &str,
    timestamp_ms: u128,
) -> Result<(), String> {
    let script_path =
        std::env::temp_dir().join(format!("api-monitor-agent-upgrade-{}.sh", timestamp_ms));
    let unit_name = format!("api-monitor-agent-upgrade-{}", timestamp_ms);
    let agent_pid = std::process::id();
    let script = format!(
        r#"#!/bin/sh
set -eu
LOG="/tmp/api-monitor-agent-upgrade.log"
RESULT_FILE="/tmp/api-monitor-agent-upgrade-result.json"
exec >>"$LOG" 2>&1
echo "API Monitor Agent self-update started at $(date -Is)"
AGENT_PATH={agent_path}
DOWNLOAD_URL={download_url}
AGENT_PID={agent_pid}
TMP_AGENT="$(mktemp /tmp/api-monitor-agent.XXXXXX)"
cleanup() {{
    rm -f "$TMP_AGENT"
}}
trap cleanup EXIT

write_result_failed() {{
    msg="$1"
    printf '{{"state":"failed","error":"%s"}}' "$msg" > "$RESULT_FILE" 2>/dev/null || true
}}

if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --connect-timeout 20 -o "$TMP_AGENT" "$DOWNLOAD_URL" || {{ write_result_failed "下载 Agent 二进制失败，请检查面板下载地址与网络连通性"; exit 1; }}
elif command -v wget >/dev/null 2>&1; then
    wget -O "$TMP_AGENT" "$DOWNLOAD_URL" || {{ write_result_failed "下载 Agent 二进制失败，请检查面板下载地址与网络连通性"; exit 1; }}
else
    write_result_failed "curl 与 wget 均不可用"
    exit 1
fi

chmod +x "$TMP_AGENT"
NEW_VERSION="$("$TMP_AGENT" --version 2>/dev/null | tail -n 1 || true)"

if [ "$(id -u)" -eq 0 ]; then
    SUDO=""
elif command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
else
    write_result_failed "自更新需要 root 或 sudo 权限"
    exit 1
fi

sleep 2
HAS_SYSTEMD_SERVICE=0
if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files api-monitor-agent.service >/dev/null 2>&1; then
    HAS_SYSTEMD_SERVICE=1
    $SUDO systemctl stop api-monitor-agent.service || true
else
    kill "$AGENT_PID" 2>/dev/null || true
fi

sleep 1
$SUDO install -m 0755 "$TMP_AGENT" "$AGENT_PATH"

printf '{{"state":"succeeded","version":"%s"}}' "$NEW_VERSION" > "$RESULT_FILE" 2>/dev/null || true

if [ "$HAS_SYSTEMD_SERVICE" = "1" ]; then
    $SUDO systemctl daemon-reload || true
    $SUDO systemctl start api-monitor-agent.service
else
    nohup "$AGENT_PATH" >/tmp/api-monitor-agent.log 2>&1 &
fi

echo "API Monitor Agent self-update completed at $(date -Is)"
"#,
        agent_path = sh_quote(&exe_path.to_string_lossy()),
        download_url = sh_quote(download_url),
        agent_pid = agent_pid,
    );
    fs::write(&script_path, script).map_err(|e| format!("写入升级脚本失败: {}", e))?;

    if let Ok(status) = Command::new("systemd-run")
        .args([
            "--unit",
            &unit_name,
            "--collect",
            "--quiet",
            "sh",
            &script_path.to_string_lossy(),
        ])
        .status()
    {
        if status.success() {
            return Ok(());
        }
    }

    Command::new("sh")
        .args([
            "-c",
            &format!(
                "nohup sh {} >/tmp/api-monitor-agent-upgrade.log 2>&1 &",
                sh_quote(&script_path.to_string_lossy())
            ),
        ])
        .spawn()
        .map_err(|e| format!("启动自更新进程失败: {}", e))?;
    Ok(())
}

#[cfg(target_os = "windows")]
fn schedule_self_update_unix(
    _exe_path: &std::path::Path,
    _download_url: &str,
    _timestamp_ms: u128,
) -> Result<(), String> {
    Err("Unix self-update is not available on this platform".to_string())
}

fn persist_runtime_config(config: &Config) {
    let Some(path) = agent_config_path() else {
        return;
    };
    let body = serde_json::json!({
        "serverUrl": config.server_url,
        "serverId": config.server_id,
        "agentKey": config.agent_key,
        "reportInterval": config.report_interval,
        "debug": config.debug,
    });
    if let Ok(json) = serde_json::to_string_pretty(&body) {
        let _ = fs::write(&path, json);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let _ = fs::set_permissions(&path, fs::Permissions::from_mode(0o600));
        }
    }
}

fn agent_config_path() -> Option<PathBuf> {
    let exe_path = std::env::current_exe().ok()?;
    let exe_dir = exe_path.parent()?;
    Some(exe_dir.join("config.json"))
}

#[cfg(target_os = "windows")]
fn ps_quote(value: &str) -> String {
    format!("'{}'", value.replace('\'', "''"))
}

#[cfg(not(target_os = "windows"))]
fn sh_quote(value: &str) -> String {
    format!("'{}'", value.replace('\'', "'\"'\"'"))
}

async fn handle_pty_start(
    task_id: &str,
    data: &str,
    pty_sessions: Arc<Mutex<HashMap<String, Arc<PtySession>>>>,
    tx: OutboundQueues,
    config: Config,
) -> Result<(), String> {
    let req: PtyStartPayload = serde_json::from_str(data).unwrap_or(PtyStartPayload {
        cols: None,
        rows: None,
        command: None,
        args: None,
        terminal_stream_v2: None,
        stream_id: None,
        stream_token: None,
    });
    if req.terminal_stream_v2.unwrap_or(false) {
        if let (Some(stream_id), Some(stream_token)) =
            (req.stream_id.clone(), req.stream_token.clone())
        {
            return handle_pty_start_v2(
                task_id,
                req,
                pty_sessions,
                config,
                stream_id,
                stream_token,
            )
            .await;
        }
    }
    let cols = req.cols.unwrap_or(80);
    let rows = req.rows.unwrap_or(24);

    let session_result = if let Some(command) = req.command {
        PtySession::new_with_command(cols, rows, command, req.args.unwrap_or_default())
    } else {
        PtySession::new(cols, rows)
    };

    let session = match session_result {
        Ok(session) => Arc::new(session),
        Err(err) => {
            let _ = tx
                .send_high(format_event(
                    EVENT_AGENT_PTY_STATUS,
                    &PtyStatusPayload {
                        id: task_id.to_string(),
                        status: "error".to_string(),
                        error: Some(err.clone()),
                    },
                ))
                .await;
            return Err(err);
        }
    };

    // Insert session
    pty_sessions
        .lock()
        .unwrap()
        .insert(task_id.to_string(), session.clone());

    // Spawn reading thread
    let mut reader = match session.try_clone_reader() {
        Ok(reader) => reader,
        Err(err) => {
            pty_sessions.lock().unwrap().remove(task_id);
            let _ = tx
                .send_high(format_event(
                    EVENT_AGENT_PTY_STATUS,
                    &PtyStatusPayload {
                        id: task_id.to_string(),
                        status: "error".to_string(),
                        error: Some(err.clone()),
                    },
                ))
                .await;
            return Err(err);
        }
    };
    let task_id_str = task_id.to_string();
    let pty_sessions_cleanup = pty_sessions.clone();
    let _ = tx
        .send_high(format_event(
            EVENT_AGENT_PTY_STATUS,
            &PtyStatusPayload {
                id: task_id.to_string(),
                status: "ready".to_string(),
                error: None,
            },
        ))
        .await;

    std::thread::spawn(move || {
        let mut buf = [0u8; 8192];
        loop {
            match reader.read(&mut buf) {
                Ok(n) if n > 0 => {
                    let text = String::from_utf8_lossy(&buf[..n]).to_string();
                    let payload = PtyDataPayload {
                        id: task_id_str.clone(),
                        data: text,
                    };
                    let msg = format_event(EVENT_AGENT_PTY_DATA, &payload);
                    tx.send_low_lossy(msg);
                    if tx.is_closed() {
                        break;
                    }
                }
                _ => break,
            }
        }
        // Cleanup on exit
        pty_sessions_cleanup.lock().unwrap().remove(&task_id_str);
    });

    Ok(())
}

async fn handle_pty_start_v2(
    task_id: &str,
    req: PtyStartPayload,
    pty_sessions: Arc<Mutex<HashMap<String, Arc<PtySession>>>>,
    config: Config,
    stream_id: String,
    stream_token: String,
) -> Result<(), String> {
    let cols = req.cols.unwrap_or(80);
    let rows = req.rows.unwrap_or(24);
    let session_result = if let Some(command) = req.command {
        PtySession::new_with_command(cols, rows, command, req.args.unwrap_or_default())
    } else {
        PtySession::new(cols, rows)
    };
    let session = Arc::new(session_result?);
    pty_sessions
        .lock()
        .unwrap()
        .insert(task_id.to_string(), session.clone());

    let url = terminal_stream_url(&config, &stream_id, &stream_token)?;
    let connect_result = tokio::time::timeout(Duration::from_secs(20), connect_async(url)).await;
    let (ws_stream, _) = match connect_result {
        Ok(Ok(pair)) => pair,
        Ok(Err(err)) => {
            pty_sessions.lock().unwrap().remove(task_id);
            return Err(format!("终端独立通道连接失败: {}", err));
        }
        Err(_) => {
            pty_sessions.lock().unwrap().remove(task_id);
            return Err("终端独立通道连接超时".to_string());
        }
    };
    let (mut ws_writer, mut ws_reader) = ws_stream.split();

    send_terminal_stream_message(
        &mut ws_writer,
        TerminalStreamMessage {
            message_type: "ready".to_string(),
            data: None,
            cols: None,
            rows: None,
        },
    )
    .await?;

    let mut reader = match session.try_clone_reader() {
        Ok(reader) => reader,
        Err(err) => {
            let _ = send_terminal_stream_message(
                &mut ws_writer,
                TerminalStreamMessage {
                    message_type: "error".to_string(),
                    data: Some(err.clone()),
                    cols: None,
                    rows: None,
                },
            )
            .await;
            pty_sessions.lock().unwrap().remove(task_id);
            return Err(err);
        }
    };

    let (pty_tx, mut pty_rx) = mpsc::channel::<String>(256);
    let task_id_cleanup = task_id.to_string();
    let pty_sessions_cleanup = pty_sessions.clone();
    std::thread::spawn(move || {
        let mut buf = [0u8; 8192];
        loop {
            match reader.read(&mut buf) {
                Ok(n) if n > 0 => {
                    let text = String::from_utf8_lossy(&buf[..n]).to_string();
                    match pty_tx.try_send(text) {
                        Ok(()) => {}
                        Err(mpsc::error::TrySendError::Full(_)) => {}
                        Err(mpsc::error::TrySendError::Closed(_)) => break,
                    }
                }
                _ => break,
            }
        }
        pty_sessions_cleanup
            .lock()
            .unwrap()
            .remove(&task_id_cleanup);
    });

    loop {
        tokio::select! {
            Some(data) = pty_rx.recv() => {
                if send_terminal_stream_message(
                    &mut ws_writer,
                    TerminalStreamMessage {
                        message_type: "data".to_string(),
                        data: Some(data),
                        cols: None,
                        rows: None,
                    },
                ).await.is_err() {
                    break;
                }
            }
            inbound = ws_reader.next() => {
                match inbound {
                    Some(Ok(Message::Text(text))) => {
                        if let Ok(msg) = serde_json::from_str::<TerminalStreamMessage>(&text) {
                            match msg.message_type.as_str() {
                                "input" => {
                                    if let Some(data) = msg.data {
                                        let _ = session.write(data.as_bytes());
                                    }
                                }
                                "resize" => {
                                    if let (Some(cols), Some(rows)) = (msg.cols, msg.rows) {
                                        let _ = session.resize(cols, rows);
                                    }
                                }
                                "stop" => break,
                                _ => {}
                            }
                        }
                    }
                    Some(Ok(Message::Close(_))) | None => break,
                    Some(Err(_)) => break,
                    _ => {}
                }
            }
        }
    }

    pty_sessions.lock().unwrap().remove(task_id);
    Ok(())
}

async fn send_terminal_stream_message<W>(
    writer: &mut W,
    msg: TerminalStreamMessage,
) -> Result<(), String>
where
    W: futures_util::Sink<Message> + Unpin,
    W::Error: std::fmt::Display,
{
    let json = serde_json::to_string(&msg).map_err(|e| e.to_string())?;
    writer
        .send(Message::Text(json.into()))
        .await
        .map_err(|e| e.to_string())
}

fn terminal_stream_url(
    config: &Config,
    stream_id: &str,
    stream_token: &str,
) -> Result<String, String> {
    let mut url =
        url::Url::parse(&config.server_url).map_err(|e| format!("服务端 URL 无效: {}", e))?;
    let scheme = match url.scheme() {
        "https" => "wss",
        "http" => "ws",
        other => return Err(format!("不支持的服务端 URL 协议: {}", other)),
    };
    url.set_scheme(scheme)
        .map_err(|_| "设置终端 WebSocket 协议失败".to_string())?;
    url.set_path("/ws/agent-terminal");
    url.query_pairs_mut()
        .clear()
        .append_pair("server_id", &config.server_id)
        .append_pair("stream_id", stream_id)
        .append_pair("token", stream_token);
    Ok(url.to_string())
}

async fn get_task_progress(
    data: &str,
    task_progress: Arc<Mutex<HashMap<String, TaskProgress>>>,
) -> Result<String, String> {
    #[derive(Deserialize)]
    struct GetProgressReq {
        #[serde(rename = "task_id")]
        task_id: String,
    }
    let req: GetProgressReq =
        serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

    let progress_map = task_progress.lock().unwrap();
    let prog = progress_map
        .get(&req.task_id)
        .ok_or_else(|| format!("任务不存在: {}", req.task_id))?;

    serde_json::to_string(prog).map_err(|e| format!("序列化结果失败: {}", e))
}

async fn handle_docker_container_update(
    task_id: String,
    data: String,
    task_progress: Arc<Mutex<HashMap<String, TaskProgress>>>,
    tx: OutboundQueues,
    docker_bridge: Arc<tokio::sync::Mutex<DockerBridge>>,
) {
    #[derive(Deserialize)]
    struct ContainerUpdateReq {
        #[serde(rename = "container_id")]
        container_id: String,
        #[serde(rename = "container_name")]
        container_name: String,
        image: Option<String>,
    }

    let req: ContainerUpdateReq = match serde_json::from_str(&data) {
        Ok(r) => r,
        Err(e) => {
            send_task_error(&task_id, &format!("解析请求失败: {}", e), tx).await;
            return;
        }
    };

    let name = format!("更新容器: {}", req.container_name);
    let mut progress = TaskProgress {
        task_id: task_id.clone(),
        name,
        percentage: 0,
        message: "正在准备...".to_string(),
        detail_msg: String::new(),
        is_done: false,
        is_error: false,
    };
    update_progress_state(
        &task_id,
        progress.clone(),
        task_progress.clone(),
        tx.clone(),
    )
    .await;

    let bridge = docker_bridge.lock().await;
    let docker_client = match &bridge.docker {
        Some(d) => d,
        None => {
            finish_with_error(
                &task_id,
                &mut progress,
                "Docker 客户端不可用",
                task_progress,
                tx,
            )
            .await;
            return;
        }
    };

    progress.percentage = 5;
    progress.message = "获取容器配置...".to_string();
    update_progress_state(
        &task_id,
        progress.clone(),
        task_progress.clone(),
        tx.clone(),
    )
    .await;

    let inspect = match docker_client
        .inspect_container(&req.container_id, None)
        .await
    {
        Ok(ins) => ins,
        Err(e) => {
            finish_with_error(
                &task_id,
                &mut progress,
                &format!("获取容器配置失败: {}", e),
                task_progress,
                tx,
            )
            .await;
            return;
        }
    };

    let labels = inspect.config.as_ref().and_then(|c| c.labels.as_ref());

    let project_label = labels
        .and_then(|l| l.get("com.docker.compose.project"))
        .map(|s| s.as_str())
        .unwrap_or("");
    let service_label = labels
        .and_then(|l| l.get("com.docker.compose.service"))
        .map(|s| s.as_str())
        .unwrap_or("");
    let working_dir = labels
        .and_then(|l| l.get("com.docker.compose.project.working_dir"))
        .map(|s| s.as_str())
        .unwrap_or("");
    let config_file = labels
        .and_then(|l| l.get("com.docker.compose.project.config_files"))
        .map(|s| s.as_str())
        .unwrap_or("");

    let is_compose = !project_label.is_empty() && !service_label.is_empty();

    let tx_clone = tx.clone();
    let task_progress_clone = task_progress.clone();
    let task_id_clone = task_id.clone();

    let progress_for_closure = progress.clone();
    let update_progress_fn = move |percentage: i32, message: &str, detail_msg: &str| {
        let mut prog = progress_for_closure.clone();
        prog.percentage = percentage;
        prog.message = message.to_string();
        prog.detail_msg = detail_msg.to_string();
        if percentage >= 100 {
            prog.is_done = true;
        }

        let tx = tx_clone.clone();
        let task_progress = task_progress_clone.clone();
        let tid = task_id_clone.clone();
        tokio::spawn(async move {
            update_progress_state(&tid, prog, task_progress, tx).await;
        });
    };

    let result = if is_compose {
        bridge
            .update_container_compose(
                project_label,
                service_label,
                working_dir,
                config_file,
                &req.container_name,
                update_progress_fn,
            )
            .await
    } else {
        let new_image = req
            .image
            .clone()
            .or_else(|| inspect.config.as_ref().and_then(|c| c.image.clone()))
            .unwrap_or_default();
        if new_image.is_empty() {
            Err("缺少镜像信息".to_string())
        } else {
            bridge
                .update_container_standalone(
                    &req.container_id,
                    &req.container_name,
                    &new_image,
                    update_progress_fn,
                )
                .await
        }
    };

    match result {
        Ok(_) => {
            let res_payload = TaskResultPayload {
                id: task_id.clone(),
                task_type: 24,
                successful: true,
                data: if is_compose {
                    format!("Compose 容器 {} 更新成功", req.container_name)
                } else {
                    "容器更新成功".to_string()
                },
                delay: 0,
            };
            let msg = format_event(EVENT_AGENT_TASK_RESULT, &res_payload);
            let _ = tx.send_normal(msg).await;
        }
        Err(e) => {
            let mut prog = TaskProgress {
                task_id: task_id.clone(),
                name: format!("更新容器: {}", req.container_name),
                percentage: progress.percentage,
                message: progress.message.clone(),
                detail_msg: progress.detail_msg.clone(),
                is_done: true,
                is_error: true,
            };
            finish_with_error(&task_id, &mut prog, &e, task_progress, tx).await;
        }
    }
}

async fn update_progress_state(
    task_id: &str,
    progress: TaskProgress,
    task_progress: Arc<Mutex<HashMap<String, TaskProgress>>>,
    tx: OutboundQueues,
) {
    {
        let mut map = task_progress.lock().unwrap();
        map.insert(task_id.to_string(), progress.clone());
    }
    let msg = format_event(EVENT_AGENT_TASK_PROGRESS, &progress);
    let _ = tx.send_normal(msg).await;
}

async fn finish_with_error(
    task_id: &str,
    progress: &mut TaskProgress,
    err_msg: &str,
    task_progress: Arc<Mutex<HashMap<String, TaskProgress>>>,
    tx: OutboundQueues,
) {
    progress.message = err_msg.to_string();
    progress.is_done = true;
    progress.is_error = true;
    update_progress_state(task_id, progress.clone(), task_progress, tx.clone()).await;

    send_task_error(task_id, err_msg, tx).await;
}

async fn send_task_error(task_id: &str, err_msg: &str, tx: OutboundQueues) {
    let res_payload = TaskResultPayload {
        id: task_id.to_string(),
        task_type: 24,
        successful: false,
        data: err_msg.to_string(),
        delay: 0,
    };
    let msg = format_event(EVENT_AGENT_TASK_RESULT, &res_payload);
    let _ = tx.send_normal(msg).await;
}

fn handle_action(action: &str, cli: &CliArgs) -> Result<(), String> {
    if action == "sample" || action == "probe" {
        let mut collector = Collector::new();
        let state = collector.collect_state();
        let json = serde_json::to_string_pretty(&state)
            .map_err(|e| format!("serialize sample state failed: {}", e))?;
        println!("{}", json);
        return Ok(());
    }

    match action {
        "upgrade" | "self-update" => {
            let config = Config::load(cli)?;
            let download_url = resolve_upgrade_download_url(&config, &UpgradePayload::default())?;
            schedule_self_update(&download_url)?;
            println!("✅ Agent 自更新已在后台安排");
            println!("   下载地址: {}", download_url);
            println!(
                "   日志: {}",
                if cfg!(target_os = "windows") {
                    "%TEMP%\\api-monitor-agent-upgrade.log"
                } else {
                    "/tmp/api-monitor-agent-upgrade.log"
                }
            );
            Ok(())
        }
        "install" => {
            #[cfg(target_os = "windows")]
            {
                let exe_path = std::env::current_exe().map_err(|e| e.to_string())?;
                let exe_dir = exe_path
                    .parent()
                    .ok_or_else(|| "无法获取可执行文件目录".to_string())?;
                let vbs_path = exe_dir.join("launch.vbs");

                let vbs_content = format!(
                    "Set WshShell = CreateObject(\"WScript.Shell\")\nWshShell.Run \"\"\"{}\"\" -b\", 0, False\n",
                    exe_path.display()
                );
                std::fs::write(&vbs_path, vbs_content)
                    .map_err(|e| format!("写入 launch.vbs 失败: {}", e))?;

                let cmd_str = format!("wscript.exe \"{}\"", vbs_path.display());
                let output = std::process::Command::new("reg")
                    .args([
                        "add",
                        "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run",
                        "/v",
                        "APIMonitorAgent",
                        "/t",
                        "REG_SZ",
                        "/d",
                        &cmd_str,
                        "/f",
                    ])
                    .output()
                    .map_err(|e| format!("执行 reg.exe 失败: {}", e))?;

                if output.status.success() {
                    println!("✅ 成功设置为用户级开机自启!");
                    println!(
                        "   注册表路径: HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run"
                    );
                    println!("   自启动脚本: {}", vbs_path.display());
                    Ok(())
                } else {
                    Err(format!(
                        "注册表写入失败: {}",
                        String::from_utf8_lossy(&output.stderr)
                    ))
                }
            }
            #[cfg(not(target_os = "windows"))]
            {
                Err("install 命令仅在 Windows 上可用".to_string())
            }
        }
        "uninstall" | "remove" => {
            #[cfg(target_os = "windows")]
            {
                if let Ok(exe_path) = std::env::current_exe() {
                    if let Some(exe_dir) = exe_path.parent() {
                        let vbs_path = exe_dir.join("launch.vbs");
                        let _ = std::fs::remove_file(vbs_path);
                    }
                }

                let _ = std::process::Command::new("reg")
                    .args([
                        "delete",
                        "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run",
                        "/v",
                        "APIMonitorAgent",
                        "/f",
                    ])
                    .output();

                println!("✅ 已成功取消用户级开机自启且清理了自启脚本");
                Ok(())
            }
            #[cfg(not(target_os = "windows"))]
            {
                Err("uninstall 命令仅在 Windows 上可用".to_string())
            }
        }
        "svc-install" | "svc-uninstall" | "start" | "stop" => {
            println!(
                "[Agent] {} 动作由外部系统服务管理器处理，在此跳过并返回成功。",
                action
            );
            Ok(())
        }
        _ => Err(format!("不支持的动作: {}", action)),
    }
}

#[cfg(target_os = "windows")]
extern "system" {
    fn GetConsoleWindow() -> *mut std::ffi::c_void;
    fn ShowWindow(hWnd: *mut std::ffi::c_void, nCmdShow: i32) -> i32;
}

#[cfg(target_os = "windows")]
fn hide_console_window() {
    unsafe {
        let hwnd = GetConsoleWindow();
        if !hwnd.is_null() {
            ShowWindow(hwnd, 0); // 0 = SW_HIDE
        }
    }
}

#[cfg(not(target_os = "windows"))]
fn hide_console_window() {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn prefer_ipv4_addresses_moves_ipv4_before_ipv6() {
        let mut addrs = vec![
            "[2a09:8280:1::133:c3fa:0]:443".parse().unwrap(),
            "66.241.124.67:443".parse().unwrap(),
        ];

        prefer_ipv4_addresses(&mut addrs);

        assert!(addrs[0].is_ipv4());
        assert!(addrs[1].is_ipv6());
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn self_uninstall_removes_installation_before_stopping_agent() {
        let script = render_self_uninstall_script(
            std::path::Path::new("/opt/api-monitor-agent"),
            std::path::Path::new("/tmp/api-monitor-agent-uninstall-test.sh"),
            1234,
        );

        let disable = script
            .find("systemctl disable api-monitor-agent.service")
            .expect("service must be disabled");
        let remove_unit = script
            .find("rm -f /etc/systemd/system/api-monitor-agent.service")
            .expect("systemd unit must be removed");
        let remove_install = script
            .find("rm -rf -- \"$INSTALL_DIR\"")
            .expect("install directory must be removed");
        let safety_guard = script
            .find("case \"$INSTALL_DIR\" in")
            .expect("install directory must be guarded");
        let stop = script
            .find("systemctl stop api-monitor-agent.service")
            .expect("service must be stopped");
        let remove_proxy = script
            .find("rm -rf -- /opt/api-monitor/proxy")
            .expect("managed proxy resources must be removed");
        let remove_empty_parent = script
            .find("rmdir /opt/api-monitor")
            .expect("empty managed parent must be removed");

        assert!(safety_guard < remove_install);
        assert!(safety_guard < remove_proxy);
        assert!(remove_proxy < remove_empty_parent);
        assert!(remove_proxy < stop);
        assert!(disable < remove_unit);
        assert!(remove_unit < remove_install);
        assert!(remove_install < stop);
        assert!(script.contains("\"/opt/api-monitor-agent\""));
        assert!(script.contains("rm -f -- \"$SCRIPT_PATH\""));
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn self_uninstall_only_accepts_managed_install_directory() {
        assert!(
            validate_self_uninstall_dir(std::path::Path::new("/opt/api-monitor-agent")).is_ok()
        );
        for unsafe_dir in ["/", "/opt", "/usr/bin", "/tmp/api-monitor-agent"] {
            assert!(validate_self_uninstall_dir(std::path::Path::new(unsafe_dir)).is_err());
        }
    }
}
