//! 中继管理 HTTP 接口（原 Go relay 的 mgmt API）。
//!
//! 保留这套 HTTP 面而不直接暴露 Rust 函数，是因为 agent 侧的
//! `tcp_forwarder::relay_request` 已经按 `POST /forwards`、`DELETE
//! /forwards/{id}` 的语义写好，保持接口不变可以把改动面压到最小，
//! 也让运维能继续用 curl 排障。

use axum::{
    extract::{Path, State},
    http::{HeaderMap, StatusCode},
    response::IntoResponse,
    routing::{delete, get, post},
    Json, Router,
};
use serde::Deserialize;
use serde_json::{json, Value};
use std::sync::Arc;

use crate::relay_server::RelayServer;

#[derive(Deserialize)]
struct AddForwardBody {
    id: String,
    listen_port: u16,
    #[serde(default)]
    token: String,
    #[serde(default)]
    udp: bool,
}

pub fn router(server: Arc<RelayServer>) -> Router {
    Router::new()
        .route("/forwards", post(add_forward))
        .route("/forwards", get(list_forwards))
        .route("/forwards/{id}", delete(delete_forward))
        .route("/health", get(health))
        .with_state(server)
}

fn authorized(server: &RelayServer, headers: &HeaderMap) -> bool {
    match server.mgmt_token() {
        None => true,
        Some(expected) => headers
            .get("authorization")
            .and_then(|v| v.to_str().ok())
            .and_then(|v| v.strip_prefix("Bearer "))
            .map(|t| t == expected)
            .unwrap_or(false),
    }
}

async fn add_forward(
    State(server): State<Arc<RelayServer>>,
    headers: HeaderMap,
    Json(body): Json<AddForwardBody>,
) -> impl IntoResponse {
    if !authorized(&server, &headers) {
        return (StatusCode::UNAUTHORIZED, Json(json!({"error": "unauthorized"})));
    }
    match server
        .upsert_forward(body.id.clone(), body.listen_port, body.token, body.udp)
        .await
    {
        Ok(()) => (
            StatusCode::OK,
            Json(json!({"ok": true, "id": body.id, "port": body.listen_port})),
        ),
        // 端口绑定失败必须显式失败：否则面板会把「没监听」误判成「运行中」。
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({"ok": false, "error": e})),
        ),
    }
}

async fn delete_forward(
    State(server): State<Arc<RelayServer>>,
    headers: HeaderMap,
    Path(id): Path<String>,
) -> impl IntoResponse {
    if !authorized(&server, &headers) {
        return (StatusCode::UNAUTHORIZED, Json(json!({"error": "unauthorized"})));
    }
    let exists = server.forwards.lock().unwrap().contains_key(&id);
    if !exists {
        return (
            StatusCode::NOT_FOUND,
            Json(json!({"error": "forward not found"})),
        );
    }
    server.remove_forward(&id).await;
    (StatusCode::OK, Json(json!({"ok": true, "id": id})))
}

async fn list_forwards(
    State(server): State<Arc<RelayServer>>,
    headers: HeaderMap,
) -> impl IntoResponse {
    if !authorized(&server, &headers) {
        return (StatusCode::UNAUTHORIZED, Json(json!({"error": "unauthorized"})));
    }
    let forwards = server.forwards.lock().unwrap();
    let list: Vec<Value> = forwards
        .values()
        .map(|f| {
            json!({
                "id": f.id,
                "port": f.port,
                "has_tunnel": f.has_tunnel(),
                "token_required": f.requires_token(),
            })
        })
        .collect();
    (StatusCode::OK, Json(json!({ "forwards": list })))
}

async fn health(State(server): State<Arc<RelayServer>>) -> impl IntoResponse {
    let (total, active, uptime) = server.stats();
    Json(json!({
        "status": "running",
        "forwards": server.forward_count(),
        "stats": {
            "total_connections": total,
            "active_connections": active,
            "uptime_seconds": uptime,
        }
    }))
}
