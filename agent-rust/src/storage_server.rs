use axum::{
    body::Body,
    extract::{Path, Query, State},
    http::{header, HeaderMap, HeaderValue, Method, Response, StatusCode},
    response::IntoResponse,
    routing::{delete, get, put},
    Router,
};
use hmac::{Hmac, Mac};
use sha2::Sha256;
use std::collections::HashMap;
use std::fs;
use std::io::Write;
use std::net::SocketAddr;
use std::path::{Path as StdPath, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};
use tokio_util::io::ReaderStream;
use tower_http::cors::{Any, CorsLayer};

#[cfg(target_os = "windows")]
const FILES_ROOT: &str = "C:\\ProgramData\\api-monitor\\agent\\files";

#[cfg(not(target_os = "windows"))]
const FILES_ROOT: &str = "/var/lib/api-monitor/agent/files";

type HmacSha256 = Hmac<Sha256>;

#[derive(Clone)]
pub struct StorageState {
    pub agent_key: String,
    pub rate_limiter: Arc<Mutex<SimpleRateLimiter>>,
}

#[derive(serde::Deserialize)]
pub struct StorageQueryParams {
    pub expires: i64,
    #[serde(default)]
    pub max_size: i64,
    pub signature: String,
}

pub struct SimpleRateLimiter {
    requests: HashMap<String, Vec<u64>>,
    limit_per_minute: usize,
}

impl SimpleRateLimiter {
    pub fn new(limit_per_minute: usize) -> Self {
        Self {
            requests: HashMap::new(),
            limit_per_minute,
        }
    }

    pub fn check(&mut self, key: &str, now: u64) -> bool {
        let entry = self.requests.entry(key.to_string()).or_default();
        let one_minute_ago = now.saturating_sub(60);
        entry.retain(|&ts| ts > one_minute_ago);
        if entry.len() >= self.limit_per_minute {
            false
        } else {
            entry.push(now);
            true
        }
    }
}

pub fn current_unix_timestamp() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

static TEMP_SEQ: AtomicU64 = AtomicU64::new(0);

pub fn compute_signature(
    method: &str,
    code: &str,
    filename: &str,
    expires: i64,
    max_size: i64,
    key: &str,
) -> Result<String, String> {
    let message = format!(
        "{}\n{}\n{}\n{}\n{}",
        method.to_uppercase(),
        code,
        filename,
        expires,
        max_size
    );
    let mut mac =
        HmacSha256::new_from_slice(key.as_bytes()).map_err(|e| format!("HMAC key error: {}", e))?;
    mac.update(message.as_bytes());
    let result = mac.finalize();
    Ok(hex::encode(result.into_bytes()))
}

pub fn verify_signature(
    method: &str,
    code: &str,
    filename: &str,
    expires: i64,
    max_size: i64,
    signature: &str,
    key: &str,
) -> bool {
    let now = current_unix_timestamp() as i64;
    if expires <= now {
        return false;
    }
    let expected = match compute_signature(method, code, filename, expires, max_size, key) {
        Ok(s) => s,
        Err(_) => return false,
    };
    expected.eq_ignore_ascii_case(signature)
}

fn validate_code_and_filename(
    code: &str,
    filename: &str,
) -> Result<(), (StatusCode, &'static str)> {
    if code.is_empty()
        || code.len() > 64
        || code.contains('/')
        || code.contains('\\')
        || code.contains("..")
    {
        return Err((StatusCode::BAD_REQUEST, "Invalid share code"));
    }
    if filename.is_empty()
        || filename.len() > 255
        || filename.contains('/')
        || filename.contains('\\')
        || filename.contains("..")
    {
        return Err((StatusCode::BAD_REQUEST, "Invalid filename"));
    }
    for c in code.chars() {
        if !c.is_ascii_alphanumeric() && c != '-' && c != '_' {
            return Err((StatusCode::BAD_REQUEST, "Invalid characters in code"));
        }
    }
    Ok(())
}

fn get_storage_path(code: &str, filename: &str) -> Result<PathBuf, (StatusCode, &'static str)> {
    validate_code_and_filename(code, filename)?;
    let root = StdPath::new(FILES_ROOT).join("shares").join(code);
    Ok(root.join(filename))
}

pub async fn handle_put(
    State(state): State<StorageState>,
    Path((code, filename)): Path<(String, String)>,
    Query(params): Query<StorageQueryParams>,
    headers: HeaderMap,
    body: Body,
) -> impl IntoResponse {
    let client_ip = headers
        .get("x-forwarded-for")
        .and_then(|v| v.to_str().ok())
        .and_then(|s| s.split(',').next())
        .unwrap_or("unknown")
        .trim();

    {
        let mut limiter = state.rate_limiter.lock().unwrap();
        if !limiter.check(client_ip, current_unix_timestamp()) {
            return (StatusCode::TOO_MANY_REQUESTS, "Rate limit exceeded").into_response();
        }
    }

    if params.max_size <= 0 {
        return (StatusCode::BAD_REQUEST, "max_size must be positive").into_response();
    }

    if !verify_signature(
        "PUT",
        &code,
        &filename,
        params.expires,
        params.max_size,
        &params.signature,
        &state.agent_key,
    ) {
        return (StatusCode::FORBIDDEN, "Invalid or expired signature").into_response();
    }

    if let Some(content_len) = headers
        .get(header::CONTENT_LENGTH)
        .and_then(|v| v.to_str().ok())
        .and_then(|s| s.parse::<i64>().ok())
    {
        if content_len > params.max_size {
            return (
                StatusCode::PAYLOAD_TOO_LARGE,
                "File exceeds max allowed size",
            )
                .into_response();
        }
    }

    let target_path = match get_storage_path(&code, &filename) {
        Ok(p) => p,
        Err((status, msg)) => return (status, msg).into_response(),
    };

    let parent_dir = target_path.parent().unwrap();
    if let Err(e) = fs::create_dir_all(parent_dir) {
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("Failed to create storage dir: {}", e),
        )
            .into_response();
    }

    let tmp_seq = TEMP_SEQ.fetch_add(1, Ordering::Relaxed);
    let tmp_path =
        target_path.with_extension(format!("tmp.{}.{}", current_unix_timestamp(), tmp_seq));
    let mut file = match fs::File::create(&tmp_path) {
        Ok(f) => f,
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                format!("Failed to open temp file: {}", e),
            )
                .into_response()
        }
    };

    use futures_util::StreamExt;
    let mut stream = body.into_data_stream();
    let mut written: i64 = 0;

    while let Some(chunk_res) = stream.next().await {
        let chunk = match chunk_res {
            Ok(c) => c,
            Err(e) => {
                let _ = fs::remove_file(&tmp_path);
                return (
                    StatusCode::BAD_REQUEST,
                    format!("Error reading upload stream: {}", e),
                )
                    .into_response();
            }
        };

        written += chunk.len() as i64;
        if written > params.max_size {
            let _ = fs::remove_file(&tmp_path);
            return (
                StatusCode::PAYLOAD_TOO_LARGE,
                "File exceeds max allowed size",
            )
                .into_response();
        }

        if let Err(e) = file.write_all(&chunk) {
            let _ = fs::remove_file(&tmp_path);
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                format!("Failed to write to file: {}", e),
            )
                .into_response();
        }
    }

    if let Err(e) = file.flush() {
        let _ = fs::remove_file(&tmp_path);
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("Failed to flush file: {}", e),
        )
            .into_response();
    }
    drop(file);

    if let Err(e) = fs::rename(&tmp_path, &target_path) {
        let _ = fs::remove_file(&tmp_path);
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("Failed to finalize file: {}", e),
        )
            .into_response();
    }

    (
        StatusCode::OK,
        [("Content-Type", "application/json")],
        format!("{{\"success\":true,\"size\":{}}}", written),
    )
        .into_response()
}

pub async fn handle_get(
    State(state): State<StorageState>,
    Path((code, filename)): Path<(String, String)>,
    Query(params): Query<StorageQueryParams>,
) -> impl IntoResponse {
    if !verify_signature(
        "GET",
        &code,
        &filename,
        params.expires,
        0,
        &params.signature,
        &state.agent_key,
    ) {
        return (StatusCode::FORBIDDEN, "Invalid or expired signature").into_response();
    }

    let target_path = match get_storage_path(&code, &filename) {
        Ok(p) => p,
        Err((status, msg)) => return (status, msg).into_response(),
    };

    if !target_path.exists() || !target_path.is_file() {
        return (StatusCode::NOT_FOUND, "File not found").into_response();
    }

    let file = match tokio::fs::File::open(&target_path).await {
        Ok(f) => f,
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                format!("Failed to open file: {}", e),
            )
                .into_response()
        }
    };

    let meta = match file.metadata().await {
        Ok(m) => m,
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                format!("Failed to read metadata: {}", e),
            )
                .into_response()
        }
    };

    let stream = ReaderStream::new(file);
    let body = Body::from_stream(stream);

    let mime_type = mime_from_filename(&filename);

    let disposition = format!("attachment; filename=\"{}\"", filename);

    let mut response = Response::new(body);
    *response.status_mut() = StatusCode::OK;
    response
        .headers_mut()
        .insert(header::CONTENT_TYPE, HeaderValue::from_static(mime_type));
    response
        .headers_mut()
        .insert(header::CONTENT_LENGTH, HeaderValue::from(meta.len()));
    if let Ok(disp_val) = HeaderValue::from_str(&disposition) {
        response
            .headers_mut()
            .insert(header::CONTENT_DISPOSITION, disp_val);
    }

    response.into_response()
}

fn mime_from_filename(filename: &str) -> &'static str {
    let ext = StdPath::new(filename)
        .extension()
        .and_then(|s| s.to_str())
        .unwrap_or("")
        .to_ascii_lowercase();
    match ext.as_str() {
        "txt" => "text/plain; charset=utf-8",
        "html" | "htm" => "text/html; charset=utf-8",
        "json" => "application/json",
        "md" => "text/markdown; charset=utf-8",
        "pdf" => "application/pdf",
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "gif" => "image/gif",
        "svg" => "image/svg+xml",
        "webp" => "image/webp",
        "zip" => "application/zip",
        "tar" => "application/x-tar",
        "gz" => "application/gzip",
        "mp4" => "video/mp4",
        "mp3" => "audio/mpeg",
        _ => "application/octet-stream",
    }
}

pub async fn handle_delete(
    State(state): State<StorageState>,
    Path((code, filename)): Path<(String, String)>,
    Query(params): Query<StorageQueryParams>,
) -> impl IntoResponse {
    if !verify_signature(
        "DELETE",
        &code,
        &filename,
        params.expires,
        0,
        &params.signature,
        &state.agent_key,
    ) {
        return (StatusCode::FORBIDDEN, "Invalid or expired signature").into_response();
    }

    let target_path = match get_storage_path(&code, &filename) {
        Ok(p) => p,
        Err((status, msg)) => return (status, msg).into_response(),
    };

    if target_path.exists() {
        let _ = fs::remove_file(&target_path);
    }

    if let Some(parent) = target_path.parent() {
        let _ = fs::remove_dir(parent);
    }

    (
        StatusCode::OK,
        [("Content-Type", "application/json")],
        "{\"success\":true}",
    )
        .into_response()
}

pub fn create_router(state: StorageState) -> Router {
    let cors = CorsLayer::new()
        .allow_origin(Any)
        .allow_methods([Method::GET, Method::PUT, Method::DELETE, Method::OPTIONS])
        .allow_headers(Any);

    Router::new()
        .route("/storage/:code/:filename", put(handle_put))
        .route("/storage/:code/:filename", get(handle_get))
        .route("/storage/:code/:filename", delete(handle_delete))
        .layer(cors)
        .with_state(state)
}

pub async fn run(port: u16, agent_key: String) {
    let state = StorageState {
        agent_key,
        rate_limiter: Arc::new(Mutex::new(SimpleRateLimiter::new(120))),
    };

    let app = create_router(state);
    let addr = SocketAddr::from(([0, 0, 0, 0], port));

    println!("[StorageServer] 监听存储直传端点: http://0.0.0.0:{}", port);

    let listener = match tokio::net::TcpListener::bind(addr).await {
        Ok(l) => l,
        Err(e) => {
            eprintln!("[StorageServer] 绑定端口 {} 失败: {}", port, e);
            return;
        }
    };

    if let Err(e) = axum::serve(listener, app).await {
        eprintln!("[StorageServer] 运行出错: {}", e);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_signature_verification_success() {
        let key = "secret-test-key-12345";
        let code = "ABCDEFGH";
        let filename = "sample.txt";
        let expires = (current_unix_timestamp() + 300) as i64;
        let max_size = 1024 * 1024;

        let sig = compute_signature("PUT", code, filename, expires, max_size, key).unwrap();
        assert!(verify_signature(
            "PUT", code, filename, expires, max_size, &sig, key
        ));
    }

    #[test]
    fn test_signature_expired_fails() {
        let key = "secret-test-key-12345";
        let code = "ABCDEFGH";
        let filename = "sample.txt";
        let expires = (current_unix_timestamp() - 10) as i64;
        let max_size = 1024 * 1024;

        let sig = compute_signature("PUT", code, filename, expires, max_size, key).unwrap();
        assert!(!verify_signature(
            "PUT", code, filename, expires, max_size, &sig, key
        ));
    }

    #[test]
    fn test_signature_tampered_fails() {
        let key = "secret-test-key-12345";
        let code = "ABCDEFGH";
        let filename = "sample.txt";
        let expires = (current_unix_timestamp() + 300) as i64;
        let max_size = 1024 * 1024;

        let sig = compute_signature("PUT", code, filename, expires, max_size, key).unwrap();
        // Tamper method
        assert!(!verify_signature(
            "GET", code, filename, expires, max_size, &sig, key
        ));
        // Tamper filename
        assert!(!verify_signature(
            "PUT", code, "evil.txt", expires, max_size, &sig, key
        ));
        // Tamper size
        assert!(!verify_signature(
            "PUT", code, filename, expires, 2048, &sig, key
        ));
    }

    #[test]
    fn test_path_traversal_rejection() {
        assert!(validate_code_and_filename("../evil", "file.txt").is_err());
        assert!(validate_code_and_filename("validcode", "../evil.txt").is_err());
        assert!(validate_code_and_filename("valid/code", "file.txt").is_err());
        assert!(validate_code_and_filename("validcode", "sub/file.txt").is_err());
        assert!(validate_code_and_filename("validcode", "file.txt").is_ok());
    }

    #[test]
    fn test_rate_limiter() {
        let mut limiter = SimpleRateLimiter::new(2);
        let now = 1000;
        assert!(limiter.check("ip1", now));
        assert!(limiter.check("ip1", now));
        assert!(!limiter.check("ip1", now)); // exceeded
        assert!(limiter.check("ip2", now)); // different ip ok
        assert!(limiter.check("ip1", now + 61)); // next minute ok
    }
}
