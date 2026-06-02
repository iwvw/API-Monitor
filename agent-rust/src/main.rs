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

use crate::config::{Config, CliArgs};
use crate::protocol::*;
use crate::collector::Collector;
use crate::docker::DockerBridge;
use crate::file_manager::FileManager;
use crate::pty::PtySession;
use clap::Parser;

const VERSION: &str = "0.1.2";

#[tokio::main]
async fn main() {
    let cli = CliArgs::parse();

    if let Some(ref action) = cli.action {
        if let Err(e) = handle_action(action) {
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

    println!("  Server:   {}", config.server_url);
    println!("  ServerID: {}", config.server_id);
    println!("  Interval: {}ms", config.report_interval);
    println!("=================================================");

    let collector = Arc::new(tokio::sync::Mutex::new(Collector::new()));
    let docker_bridge = Arc::new(tokio::sync::Mutex::new(DockerBridge::new()));
    let pty_sessions = Arc::new(Mutex::new(HashMap::<String, Arc<PtySession>>::new()));
    let task_progress = Arc::new(Mutex::new(HashMap::<String, TaskProgress>::new()));

    // Keep dialing loop
    loop {
        println!("[Agent] 正在连接服务器...");
        match run_client(config.clone(), collector.clone(), docker_bridge.clone(), pty_sessions.clone(), task_progress.clone()).await {
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
    task_progress: Arc<Mutex<HashMap<String, TaskProgress>>>,
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
                            let task_progress_task = task_progress.clone();

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
                                    11 => { // DOCKER_CHECK_UPDATE
                                        match docker_bridge_task.lock().await.handle_docker_check_update(&task.data).await {
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
                                    14 => { // DOCKER_IMAGE_ACTION
                                        match docker_bridge_task.lock().await.handle_docker_image_action(&task.data) {
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
                                    16 => { // DOCKER_NETWORK_ACTION
                                        match docker_bridge_task.lock().await.handle_docker_network_action(&task.data) {
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
                                    18 => { // DOCKER_VOLUME_ACTION
                                        match docker_bridge_task.lock().await.handle_docker_volume_action(&task.data) {
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
                                    24 => { // DOCKER_UPDATE_CONTAINER
                                        let tx_clone_inner = tx_task.clone();
                                        let docker_bridge_inner = docker_bridge_task.clone();
                                        let task_id = task.id.clone();
                                        let task_data = task.data.clone();
                                        let task_progress_inner = task_progress_task.clone();
                                        tokio::spawn(async move {
                                            handle_docker_container_update(task_id, task_data, task_progress_inner, tx_clone_inner, docker_bridge_inner).await;
                                        });
                                        successful = true;
                                        res_data = "容器更新任务已启动".to_string();
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
                                    26 => { // DOCKER_TASK_PROGRESS
                                        match get_task_progress(&task.data, task_progress_task).await {
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

async fn get_task_progress(data: &str, task_progress: Arc<Mutex<HashMap<String, TaskProgress>>>) -> Result<String, String> {
    #[derive(Deserialize)]
    struct GetProgressReq {
        #[serde(rename = "task_id")]
        task_id: String,
    }
    let req: GetProgressReq = serde_json::from_str(data)
        .map_err(|e| format!("解析请求失败: {}", e))?;

    let progress_map = task_progress.lock().unwrap();
    let prog = progress_map.get(&req.task_id)
        .ok_or_else(|| format!("任务不存在: {}", req.task_id))?;

    serde_json::to_string(prog)
        .map_err(|e| format!("序列化结果失败: {}", e))
}

async fn handle_docker_container_update(
    task_id: String,
    data: String,
    task_progress: Arc<Mutex<HashMap<String, TaskProgress>>>,
    tx: mpsc::Sender<String>,
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
    update_progress_state(&task_id, progress.clone(), task_progress.clone(), tx.clone()).await;

    let bridge = docker_bridge.lock().await;
    let docker_client = match &bridge.docker {
        Some(d) => d,
        None => {
            finish_with_error(&task_id, &mut progress, "Docker 客户端不可用", task_progress, tx).await;
            return;
        }
    };

    progress.percentage = 5;
    progress.message = "获取容器配置...".to_string();
    update_progress_state(&task_id, progress.clone(), task_progress.clone(), tx.clone()).await;

    let inspect = match docker_client.inspect_container(&req.container_id, None).await {
        Ok(ins) => ins,
        Err(e) => {
            finish_with_error(&task_id, &mut progress, &format!("获取容器配置失败: {}", e), task_progress, tx).await;
            return;
        }
    };

    let labels = inspect.config.as_ref()
        .and_then(|c| c.labels.as_ref());

    let project_label = labels.and_then(|l| l.get("com.docker.compose.project")).map(|s| s.as_str()).unwrap_or("");
    let service_label = labels.and_then(|l| l.get("com.docker.compose.service")).map(|s| s.as_str()).unwrap_or("");
    let working_dir = labels.and_then(|l| l.get("com.docker.compose.project.working_dir")).map(|s| s.as_str()).unwrap_or("");
    let config_file = labels.and_then(|l| l.get("com.docker.compose.project.config_files")).map(|s| s.as_str()).unwrap_or("");

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
        bridge.update_container_compose(
            project_label,
            service_label,
            working_dir,
            config_file,
            &req.container_name,
            update_progress_fn,
        ).await
    } else {
        let new_image = req.image.clone()
            .or_else(|| inspect.config.as_ref().and_then(|c| c.image.clone()))
            .unwrap_or_default();
        if new_image.is_empty() {
            Err("缺少镜像信息".to_string())
        } else {
            bridge.update_container_standalone(
                &req.container_id,
                &req.container_name,
                &new_image,
                update_progress_fn,
            ).await
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
            let _ = tx.send(msg).await;
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
    tx: mpsc::Sender<String>,
) {
    {
        let mut map = task_progress.lock().unwrap();
        map.insert(task_id.to_string(), progress.clone());
    }
    let msg = format_event(EVENT_AGENT_TASK_PROGRESS, &progress);
    let _ = tx.send(msg).await;
}

async fn finish_with_error(
    task_id: &str,
    progress: &mut TaskProgress,
    err_msg: &str,
    task_progress: Arc<Mutex<HashMap<String, TaskProgress>>>,
    tx: mpsc::Sender<String>,
) {
    progress.message = err_msg.to_string();
    progress.is_done = true;
    progress.is_error = true;
    update_progress_state(task_id, progress.clone(), task_progress, tx.clone()).await;

    send_task_error(task_id, err_msg, tx).await;
}

async fn send_task_error(task_id: &str, err_msg: &str, tx: mpsc::Sender<String>) {
    let res_payload = TaskResultPayload {
        id: task_id.to_string(),
        task_type: 24,
        successful: false,
        data: err_msg.to_string(),
        delay: 0,
    };
    let msg = format_event(EVENT_AGENT_TASK_RESULT, &res_payload);
    let _ = tx.send(msg).await;
}

fn handle_action(action: &str) -> Result<(), String> {
    match action {
        "install" => {
            #[cfg(target_os = "windows")]
            {
                let exe_path = std::env::current_exe().map_err(|e| e.to_string())?;
                let exe_dir = exe_path.parent().ok_or_else(|| "无法获取可执行文件目录".to_string())?;
                let vbs_path = exe_dir.join("launch.vbs");
                
                let vbs_content = format!(
                    "Set WshShell = CreateObject(\"WScript.Shell\")\nWshShell.Run \"\"\"{}\"\" -b\", 0, False\n",
                    exe_path.display()
                );
                std::fs::write(&vbs_path, vbs_content).map_err(|e| format!("写入 launch.vbs 失败: {}", e))?;
                
                let cmd_str = format!("wscript.exe \"{}\"", vbs_path.display());
                let output = std::process::Command::new("reg")
                    .args(["add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run", "/v", "APIMonitorAgent", "/t", "REG_SZ", "/d", &cmd_str, "/f"])
                    .output()
                    .map_err(|e| format!("执行 reg.exe 失败: {}", e))?;
                
                if output.status.success() {
                    println!("✅ 成功设置为用户级开机自启!");
                    println!("   注册表路径: HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run");
                    println!("   自启动脚本: {}", vbs_path.display());
                    Ok(())
                } else {
                    Err(format!("注册表写入失败: {}", String::from_utf8_lossy(&output.stderr)))
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
                    .args(["delete", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run", "/v", "APIMonitorAgent", "/f"])
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
            println!("[Agent] {} 动作由外部系统服务管理器处理，在此跳过并返回成功。", action);
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
