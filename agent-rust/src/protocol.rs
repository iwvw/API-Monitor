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
            // Socket.IO message formatting.
            // Check for namespace connect: "0/agent," or "0/agent"
            if body.starts_with("0/agent") {
                return SocketIOMessage::NamespaceConnect;
            }
            // Check for event: "2/agent,["event_name", data]"
            if body.starts_with("2/agent,") {
                let json_part = &body[8..];
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
    format!("42/agent,{}", arr.to_string())
}
