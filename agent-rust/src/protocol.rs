use serde::{Deserialize, Serialize};

// Socket.IO event names
pub const EVENT_AGENT_CONNECT: &str = "agent:connect";
pub const EVENT_AGENT_HOST_INFO: &str = "agent:host_info";
pub const EVENT_AGENT_STATE: &str = "agent:state";
pub const EVENT_AGENT_TASK_RESULT: &str = "agent:task_result";
pub const EVENT_DASHBOARD_AUTH_OK: &str = "dashboard:auth_ok";
pub const EVENT_DASHBOARD_AUTH_FAIL: &str = "dashboard:auth_fail";
pub const EVENT_DASHBOARD_TASK: &str = "dashboard:task";
pub const EVENT_DASHBOARD_PTY_INPUT: &str = "dashboard:pty_input";
pub const EVENT_DASHBOARD_PTY_RESIZE: &str = "dashboard:pty_resize";
pub const EVENT_AGENT_PTY_DATA: &str = "agent:pty_data";
pub const EVENT_AGENT_TASK_PROGRESS: &str = "agent:task_progress";

#[derive(Serialize, Debug, Clone)]
pub struct AuthPayload {
    pub server_id: String,
    pub key: String,
    pub hostname: String,
    pub version: String,
}

#[derive(Deserialize, Debug, Clone)]
pub struct AuthFailPayload {
    pub reason: String,
}

#[derive(Deserialize, Debug, Clone)]
pub struct TaskPayload {
    pub id: String,
    #[serde(rename = "type")]
    pub task_type: i32,
    pub data: String,
    pub timeout: i32,
}

#[derive(Serialize, Debug, Clone)]
pub struct TaskResultPayload {
    pub id: String,
    #[serde(rename = "type")]
    pub task_type: i32,
    pub successful: bool,
    pub data: String,
    pub delay: i64, // milliseconds
}

#[derive(Deserialize, Debug, Clone)]
pub struct PtyInputPayload {
    pub id: String,
    pub data: String,
}

#[derive(Deserialize, Debug, Clone)]
pub struct PtyResizePayload {
    pub id: String,
    pub cols: u32,
    pub rows: u32,
}

#[derive(Serialize, Debug, Clone)]
pub struct PtyDataPayload {
    pub id: String,
    pub data: String,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct TaskProgress {
    #[serde(rename = "task_id")]
    pub task_id: String,
    pub name: String,
    pub percentage: i32,
    pub message: String,
    #[serde(rename = "detail_msg")]
    pub detail_msg: String,
    #[serde(rename = "is_done")]
    pub is_done: bool,
    #[serde(rename = "is_error")]
    pub is_error: bool,
}

#[derive(Deserialize, Debug, Clone)]
pub struct DockerCheckUpdateRequest {
    #[serde(rename = "container_id")]
    pub container_id: Option<String>,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct DockerImageUpdateStatus {
    #[serde(rename = "container_id")]
    pub container_id: String,
    #[serde(rename = "container_name")]
    pub container_name: String,
    pub image: String,
    #[serde(rename = "current_digest")]
    pub current_digest: String,
    #[serde(rename = "latest_digest")]
    pub latest_digest: String,
    #[serde(rename = "has_update")]
    pub has_update: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// Socket.IO Engine.IO & Socket.IO parser
#[derive(Debug, Clone)]
pub enum SocketIOMessage {
    Ping,
    Pong,
    NamespaceConnect,
    Event(String, serde_json::Value),
    Raw(String),
    Ignored,
}

pub fn parse_socketio_message(raw: &str) -> SocketIOMessage {
    if raw.is_empty() {
        return SocketIOMessage::Ignored;
    }

    // 处理原始握手包（0{...}）
    if raw.starts_with('0') {
        return SocketIOMessage::Raw(raw.to_string());
    }

    // 处理 CONNECT ACK（40{...}）
    if raw.starts_with("40") {
        return SocketIOMessage::Raw(raw.to_string());
    }

    // Engine.IO Packet Type:
    // '2' = Ping
    // '3' = Pong
    // '4' = Message
    let eio_type = raw.chars().next().unwrap();
    let body = &raw[1..];

    match eio_type {
        '2' => SocketIOMessage::Ping,
        '3' => SocketIOMessage::Pong,
        '4' => {
            // Socket.IO 事件消息: "2["event_name", data]"
            if body.starts_with('2') {
                let json_part = &body[1..];
                if let Ok(serde_json::Value::Array(arr)) = serde_json::from_str(json_part) {
                    if arr.len() >= 1 {
                        if let Some(event_name) = arr[0].as_str() {
                            let data = if arr.len() > 1 {
                                arr[1].clone()
                            } else {
                                serde_json::Value::Null
                            };
                            return SocketIOMessage::Event(event_name.to_string(), data);
                        }
                    }
                }
            }
            SocketIOMessage::Raw(body.to_string())
        }
        _ => SocketIOMessage::Ignored,
    }
}

pub fn format_event<T: Serialize>(event: &str, payload: &T) -> String {
    let arr = serde_json::json!([event, payload]);
    format!("42{}", arr.to_string())
}
