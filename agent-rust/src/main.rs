mod config;
use std::process::Command;
mod protocol;
mod collector;
mod docker;
mod file_manager;
mod pty;

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tokio::sync::mpsc;
use tokio::time::sleep;
use futures_util::{StreamExt, SinkExt};
use tokio_tungstenite::{connect_async, tungstenite::protocol::Message};
use serde::Deserialize;

use crate::config::Config;
use crate::protocol::*;
use crate::collector::Collector;
use crate::docker::DockerBridge;
use crate::file_manager::FileManager;
use crate::pty::PtySession;

const VERSION: &str = "0.1.2";

#[tokio::main]
async fn main() {
    println!("=================================================");
    println!("  API Monitor Agent v{} (Rust)", VERSION);
    println!("=================================================");

    let config = match Config::load() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("{}", e);
            std::process::exit(1);
        }
    };

    println!("  Server:   {}", config.server_url);
    println!("  ServerID: {}", config.server_id);
    println!("  Interval: {}ms", config.report_interval);
    println!("=================================================");

    let collector = Arc::new(tokio::sync::Mutex::new(Collector::new()));
    let docker_bridge = Arc::new(tokio::sync::Mutex::new(DockerBridge::new()));
    let pty_sessions = Arc::new(Mutex::new(HashMap::<String, Arc<PtySession>>::new()));

    // Keep dialing loop
    loop {
        println!("[Agent] 正在连接服务器...");
        match run_client(config.clone(), collector.clone(), docker_bridge.clone(), pty_sessions.clone()).await {
            Ok(_) => {
                println!("[Agent] 连接断开，准备重连...");
            }
            Err(e) => {
                eprintln!("[Agent] 运行错误: {}", e);
            }
        }
        sleep(Duration::from_millis(config.reconnect_delay)).await;
    }
}

async fn run_client(
    config: Config,
    collector: Arc<tokio::sync::Mutex<Collector>>,
    docker_bridge: Arc<tokio::sync::Mutex<DockerBridge>>,
    pty_sessions: Arc<Mutex<HashMap<String, Arc<PtySession>>>>,
) -> Result<(), String> {
    // 1. Polling handshake to get sid
    let handshake_url = format!("{}/socket.io/?EIO=4&transport=polling", config.server_url);
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(5))
        .build()
        .map_err(|e| format!("HTTP客户端初始化失败: {}", e))?;

    let resp = client.get(&handshake_url)
        .send()
        .await
        .map_err(|e| format!("Handshake 请求失败: {}", e))?
        .text()
        .await
        .map_err(|e| format!("读取 Handshake 响应失败: {}", e))?;

    if resp.len() < 2 || !resp.starts_with('0') {
        return Err(format!("无效的 Handshake 响应: {}", resp));
    }

    let handshake_json = &resp[1..];
    let handshake_val: serde_json::Value = serde_json::from_str(handshake_json)
        .map_err(|e| format!("解析 Handshake 响应失败: {}", e))?;

    let sid = handshake_val["sid"].as_str()
        .ok_or_else(|| "Handshake 响应中缺少 sid".to_string())?;

    // 2. Build WebSocket URL and connect
    let mut ws_url = if config.server_url.starts_with("https://") {
        config.server_url.replace("https://", "wss://")
    } else {
        config.server_url.replace("http://", "ws://")
    };
    ws_url = format!("{}/socket.io/?EIO=4&transport=websocket&sid={}", ws_url, sid);

    if config.debug {
        println!("[Agent] 正在建立 WebSocket 连接: {}", ws_url);
    }

    let (ws_stream, _) = connect_async(&ws_url)
        .await
        .map_err(|e| format!("WebSocket 连接失败: {}", e))?;

    println!("[Agent] WebSocket 连接已建立");

    let (mut ws_writer, mut ws_reader) = ws_stream.split();

    // Channel for multiplexing outbound messages
    let (tx, mut rx) = mpsc::channel::<String>(100);

    // Task to write outgoing messages to websocket
    let mut write_task = tokio::spawn(async move {
        while let Some(msg) = rx.recv().await {
            if ws_writer.send(Message::Text(msg.into())).await.is_err() {
                break;
            }
        }
    });

    // Send probe upgrade confirmation
    tx.send("2probe".to_string()).await.map_err(|_| "发送升级探针失败")?;

    // Wait for 3probe and namespace connect
    let mut authenticated = Arc::new(tokio::sync::Mutex::new(false));

    // Handle WebSocket receiver loop
    let tx_clone = tx.clone();
    let auth_ok_tx = tx.clone();
    let collector_clone = collector.clone();
    let docker_bridge_clone = docker_bridge.clone();
    let pty_sessions_clone = pty_sessions.clone();
    let authenticated_clone = authenticated.clone();
    let config_clone = config.clone();

    let mut read_task = tokio::spawn(async move {
        let mut initial_host_sent = false;

        while let Some(Ok(Message::Text(text))) = ws_reader.next().await {
            if config_clone.debug && text.as_str() != "2" && text.as_str() != "3" {
                println!("[Agent] 收到原始消息: {}", text);
            }

            let msg = parse_socketio_message(&text);
            match msg {
                SocketIOMessage::Ping => {
                    let _ = tx_clone.send("3".to_string()).await;
                }
                SocketIOMessage::NamespaceConnect => {
                    // Namespace confirmed / connected
                }
                SocketIOMessage::Event(event, data) => {
                    if event == EVENT_DASHBOARD_AUTH_OK {
                        println!("[Agent] ✅ 认证成功");
                        *authenticated_clone.lock().await = true;

                        // Start loops for reports
                        let auth_tx = auth_ok_tx.clone();
                        let collector_loop = collector_clone.clone();
                        let docker_loop = docker_bridge_clone.clone();
                        let cfg = config_clone.clone();

                        tokio::spawn(async move {
                            // First run
                            let host_info = collector_loop.lock().await.collect_host_info(VERSION).await;
                            let _ = auth_tx.send(format_event(EVENT_AGENT_HOST_INFO, &host_info)).await;

                            let mut state = collector_loop.lock().await.collect_state();
                            state.docker = docker_loop.lock().await.collect_docker_info().await;
                            let _ = auth_tx.send(format_event(EVENT_AGENT_STATE, &state)).await;

                            let mut state_timer = tokio::time::interval(Duration::from_millis(cfg.report_interval));
                            let mut host_timer = tokio::time::interval(Duration::from_millis(cfg.host_info_interval));
                            
                            // Prevent tick stacking
                            state_timer.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
                            host_timer.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);

                            loop {
                                tokio::select! {
                                    _ = state_timer.tick() => {
                                        let mut state = collector_loop.lock().await.collect_state();
                                        state.docker = docker_loop.lock().await.collect_docker_info().await;
                                        if auth_tx.send(format_event(EVENT_AGENT_STATE, &state)).await.is_err() {
                                            break;
                                        }
                                    }
                                    _ = host_timer.tick() => {
                                        let host_info = collector_loop.lock().await.collect_host_info(VERSION).await;
                                        if auth_tx.send(format_event(EVENT_AGENT_HOST_INFO, &host_info)).await.is_err() {
                                            break;
                                        }
                                    }
                                }
                            }
                        });
                    } else if event == EVENT_DASHBOARD_AUTH_FAIL {
                        let reason: AuthFailPayload = serde_json::from_value(data).unwrap_or(AuthFailPayload { reason: "未知".to_string() });
                        eprintln!("[Agent] ❌ 认证失败: {}", reason.reason);
                        std::process::exit(1);
                    } else if event == EVENT_DASHBOARD_TASK {
                        if let Ok(task) = serde_json::from_value::<TaskPayload>(data) {
                            let tx_task = tx_clone.clone();
                            let docker_bridge_task = docker_bridge_clone.clone();
                            let pty_sessions_task = pty_sessions_clone.clone();
                            let config_task = config_clone.clone();

                            tokio::spawn(async move {
                                let start_time = Instant::now();
                                let mut successful = false;
                                let mut res_data = String::new();

                                match task.task_type {
                                    1 => { // COMMAND
                                        match execute_command(&task.data, task.timeout as u64).await {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    10 => { // DOCKER_ACTION
                                        match docker_bridge_task.lock().await.handle_docker_action(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    13 => { // DOCKER_IMAGES
                                        match docker_bridge_task.lock().await.handle_docker_images(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    15 => { // DOCKER_NETWORKS
                                        match docker_bridge_task.lock().await.handle_docker_networks(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    17 => { // DOCKER_VOLUMES
                                        match docker_bridge_task.lock().await.handle_docker_volumes(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    19 => { // DOCKER_LOGS
                                        match docker_bridge_task.lock().await.handle_docker_logs(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    20 => { // DOCKER_STATS
                                        match docker_bridge_task.lock().await.handle_docker_stats(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    21 => { // DOCKER_COMPOSE_LIST
                                        match docker_bridge_task.lock().await.handle_docker_compose_list(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    22 => { // DOCKER_COMPOSE_ACTION
                                        match docker_bridge_task.lock().await.handle_docker_compose_action(&task.data) {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    23 => { // DOCKER_CREATE_CONTAINER
                                        match handle_docker_create_container(&task.data).await {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    25 => { // DOCKER_RENAME_CONTAINER
                                        match handle_docker_rename_container(&task.data).await {
                                            Ok(out) => {
                                                successful = true;
                                                res_data = out;
                                            }
                                            Err(err) => {
                                                res_data = err;
                                            }
                                        }
                                    }
                                    30 => { // FILE_LIST
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
                                    31 => { // FILE_READ
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
                                    32 => { // FILE_WRITE
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
                                    33 => { // FILE_MKDIR
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
                                    34 => { // FILE_DELETE
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
                                    35 => { // FILE_RENAME
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
                                    36 => { // FILE_STAT
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
                                    37 => { // FILE_CHMOD
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
                                    38 => { // FILE_DOWNLOAD_CHUNK
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
                                    5 => { // UPGRADE
                                        handle_upgrade(&task.id, &config_task).await;
                                        successful = true;
                                        res_data = "正在后台执行升级...".to_string();
                                    }
                                    12 => { // PTY_START
                                        let _ = handle_pty_start(&task.id, &task.data, pty_sessions_task, tx_task.clone()).await;
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
                                let _ = tx_task.send(format_event(EVENT_AGENT_TASK_RESULT, &res_payload)).await;
                            });
                        }
                    } else if event == EVENT_DASHBOARD_PTY_INPUT {
                        if let Ok(input) = serde_json::from_value::<PtyInputPayload>(data) {
                            if let Some(session) = pty_sessions_clone.lock().unwrap().get(&input.id) {
                                let _ = session.write(input.data.as_bytes());
                            }
                        }
                    } else if event == EVENT_DASHBOARD_PTY_RESIZE {
                        if let Ok(resize) = serde_json::from_value::<PtyResizePayload>(data) {
                            if let Some(session) = pty_sessions_clone.lock().unwrap().get(&resize.id) {
                                let _ = session.resize(resize.cols, resize.rows);
                            }
                        }
                    }
                }
                SocketIOMessage::Raw(r) => {
                    // Check for probe confirmation sequence
                    if r == "3probe" {
                        // Upgrade complete, send 5
                        let _ = tx_clone.send("5".to_string()).await;
                        // Connect namespace /agent
                        let _ = tx_clone.send("40/agent,".to_string()).await;
                    } else if r == "40/agent" || r.starts_with("40/agent,") {
                        // Namespace confirmed. Perform authentication
                        let hostname = hostname::get()
                            .map(|h| h.to_string_lossy().to_string())
                            .unwrap_or_else(|_| "unknown".to_string());

                        let auth = AuthPayload {
                            server_id: config_clone.server_id.clone(),
                            key: config_clone.agent_key.clone(),
                            hostname,
                            version: VERSION.to_string(),
                        };

                        let msg = format_event(EVENT_AGENT_CONNECT, &auth);
                        let _ = tx_clone.send(msg).await;
                    }
                }
                _ => {}
            }
        }
    });

    // Run both tasks concurrently until connection breaks
    tokio::select! {
        _ = &mut write_task => {
            read_task.abort();
            Err("Write task terminated".to_string())
        }
        _ = &mut read_task => {
            write_task.abort();
            Err("Read task terminated".to_string())
        }
    }
}

// Helpers
use std::time::Instant;

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

    let child = cmd.stdout(std::process::Stdio::piped())
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

async fn handle_docker_create_container(data: &str) -> Result<String, String> {
    #[derive(Deserialize)]
    struct DockerCreateContainerRequest {
        image: String,
        name: Option<String>,
        ports: Option<Vec<String>>,
        volumes: Option<Vec<String>>,
        env: Option<HashMap<String, String>>,
        network: Option<String>,
        restart: Option<String>,
        privileged: Option<bool>,
        #[serde(rename = "extraArgs")]
        extra_args: Option<Vec<String>>,
    }

    let req: DockerCreateContainerRequest = serde_json::from_str(data)
        .map_err(|e| format!("解析请求失败: {}", e))?;

    if req.image.is_empty() {
        return Err("缺少镜像名称".to_string());
    }

    let mut args = vec!["run".to_string(), "-d".to_string()];

    if let Some(name) = req.name {
        if !name.is_empty() {
            args.push("--name".to_string());
            args.push(name);
        }
    }

    if let Some(ports) = req.ports {
        for p in ports {
            args.push("-p".to_string());
            args.push(p);
        }
    }

    if let Some(volumes) = req.volumes {
        for v in volumes {
            args.push("-v".to_string());
            args.push(v);
        }
    }

    if let Some(env) = req.env {
        for (k, v) in env {
            args.push("-e".to_string());
            args.push(format!("{}={}", k, v));
        }
    }

    if let Some(network) = req.network {
        if !network.is_empty() {
            args.push("--network".to_string());
            args.push(network);
        }
    }

    if let Some(restart) = req.restart {
        if !restart.is_empty() {
            args.push("--restart".to_string());
            args.push(restart);
        }
    }

    if let Some(true) = req.privileged {
        args.push("--privileged".to_string());
    }

    if let Some(extra) = req.extra_args {
        for arg in extra {
            args.push(arg);
        }
    }

    args.push(req.image);

    let output = Command::new("docker")
        .args(&args)
        .output()
        .map_err(|e| format!("创建容器失败: {}", e))?;

    if output.status.success() {
        let container_id = String::from_utf8_lossy(&output.stdout).trim().to_string();
        Ok(format!("容器创建成功\nID: {}", container_id))
    } else {
        Err(format!("创建容器失败: {}", String::from_utf8_lossy(&output.stderr)))
    }
}

async fn handle_docker_rename_container(data: &str) -> Result<String, String> {
    #[derive(Deserialize)]
    struct DockerRenameContainerRequest {
        #[serde(rename = "containerId")]
        container_id: String,
        #[serde(rename = "newName")]
        new_name: String,
    }

    let req: DockerRenameContainerRequest = serde_json::from_str(data)
        .map_err(|e| format!("解析请求失败: {}", e))?;

    if req.container_id.is_empty() || req.new_name.is_empty() {
        return Err("容器ID或新名称不能为空".to_string());
    }

    let output = Command::new("docker")
        .args(["rename", &req.container_id, &req.new_name])
        .output()
        .map_err(|e| format!("容器重命名失败: {}", e))?;

    if output.status.success() {
        Ok("容器重命名成功".to_string())
    } else {
        Err(format!("容器重命名失败: {}", String::from_utf8_lossy(&output.stderr)))
    }
}

async fn handle_upgrade(task_id: &str, config: &Config) {
    sleep(Duration::from_secs(1)).await;
    println!("[Upgrade] 开始执行升级流程...");

    let install_url = if cfg!(target_os = "windows") {
        format!("{}/api/server/agent/install/win/{}", config.server_url, config.server_id)
    } else {
        format!("{}/api/server/agent/install/linux/{}", config.server_url, config.server_id)
    };

    if cfg!(target_os = "windows") {
        let ps_cmd = format!("irm {} | iex", install_url);
        let _ = Command::new("powershell")
            .args(["-Command", "Start-Process", "powershell", "-ArgumentList", &format!("'-NoProfile -ExecutionPolicy Bypass -Command \"{}\"'", ps_cmd), "-WindowStyle", "Hidden"])
            .spawn();
    } else {
        let sh_cmd = format!("curl -fsSL {} | sudo bash", install_url);
        let _ = Command::new("sh")
            .args(["-c", &format!("nohup sh -c '{}' > /tmp/agent_upgrade.log 2>&1 &", sh_cmd)])
            .spawn();
    }
}

async fn handle_pty_start(
    task_id: &str,
    data: &str,
    pty_sessions: Arc<Mutex<HashMap<String, Arc<PtySession>>>>,
    tx: mpsc::Sender<String>,
) -> Result<(), String> {
    #[derive(Deserialize)]
    struct PtyResizeReq {
        cols: Option<u32>,
        rows: Option<u32>,
    }

    let req: PtyResizeReq = serde_json::from_str(data).unwrap_or(PtyResizeReq { cols: None, rows: None });
    let cols = req.cols.unwrap_or(80);
    let rows = req.rows.unwrap_or(24);

    let session = Arc::new(PtySession::new(cols, rows)?);

    // Insert session
    pty_sessions.lock().unwrap().insert(task_id.to_string(), session.clone());

    // Spawn reading thread
    let mut reader = session.try_clone_reader()?;
    let task_id_str = task_id.to_string();
    let pty_sessions_cleanup = pty_sessions.clone();

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
                    if tx.blocking_send(msg).is_err() {
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
