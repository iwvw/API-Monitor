use base64::{engine::general_purpose, Engine as _};
use serde::{Deserialize, Serialize};
use std::fs;
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::sync::OnceLock;

#[cfg(target_os = "windows")]
const FILES_ROOT: &str = "C:\\ProgramData\\api-monitor\\agent\\files";

#[cfg(not(target_os = "windows"))]
const FILES_ROOT: &str = "/var/lib/api-monitor/agent/files";

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileListRequest {
    pub path: String,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct FileEntry {
    pub name: String,
    pub path: String,
    pub is_directory: bool,
    pub is_file: bool,
    pub is_symlink: bool,
    pub size: i64,
    pub mode: u32,
    pub mtime: i64,
    pub atime: i64,
    pub permissions: String,
}

#[derive(Serialize)]
pub struct FileListResponse {
    pub files: Vec<FileEntry>,
    pub cwd: String,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileReadRequest {
    pub path: String,
    pub max_size: Option<i64>,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileWriteRequest {
    pub path: String,
    pub content: String,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileMkdirRequest {
    pub path: String,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileDeleteRequest {
    pub path: String,
    pub recursive: bool,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileRenameRequest {
    pub old_path: String,
    pub new_path: String,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileStatRequest {
    pub path: String,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
#[allow(dead_code)]
pub struct FileChmodRequest {
    pub path: String,
    pub mode: u32,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileDownloadChunkRequest {
    pub path: String,
    pub offset: i64,
    pub size: i32,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileUploadToUrlRequest {
    pub path: String,
    pub url: String,
    #[serde(default)]
    pub method: Option<String>,
    #[serde(default)]
    pub content_type: Option<String>,
}

#[derive(Serialize)]
pub struct FileUploadToUrlResponse {
    pub success: bool,
    pub status: u16,
    pub size: i64,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileFetchUrlRequest {
    pub url: String,
    pub path: String,
    #[serde(default)]
    pub headers: Option<std::collections::HashMap<String, String>>,
}

#[derive(Serialize)]
pub struct FileFetchUrlResponse {
    pub success: bool,
    pub status: u16,
    pub size: i64,
    pub path: String,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct FileWriteBase64Request {
    pub path: String,
    /// base64 编码的字节分块
    pub data: String,
    /// 写入起始偏移；0 表示从文件头开始（并截断）
    #[serde(default)]
    pub offset: i64,
    /// 是否在写入前截断文件（仅首块需要）
    #[serde(default)]
    pub truncate: bool,
}

pub struct FileManager;

impl FileManager {
    pub fn handle_file_list(data: &str) -> Result<String, String> {
        let req: FileListRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        let abs_path = resolve_path(&req.path)?;

        let entries = fs::read_dir(&abs_path).map_err(|e| format!("读取目录失败: {}", e))?;

        let mut files = Vec::new();
        for entry_res in entries {
            if let Ok(entry) = entry_res {
                if let Ok(meta) = entry.metadata() {
                    let file_path = entry.path();
                    let file_name = entry.file_name().to_string_lossy().to_string();
                    let path_str = virtual_path(&file_path);

                    let mtime = meta
                        .modified()
                        .ok()
                        .and_then(|t| t.duration_since(std::time::SystemTime::UNIX_EPOCH).ok())
                        .map(|d| d.as_millis() as i64)
                        .unwrap_or(0);

                    let atime = meta
                        .accessed()
                        .ok()
                        .and_then(|t| t.duration_since(std::time::SystemTime::UNIX_EPOCH).ok())
                        .map(|d| d.as_millis() as i64)
                        .unwrap_or(mtime);

                    #[cfg(unix)]
                    let mode = {
                        use std::os::unix::fs::MetadataExt;
                        meta.mode() & 0o777
                    };
                    #[cfg(not(unix))]
                    let mode = 0o755;

                    files.push(FileEntry {
                        name: file_name,
                        path: path_str,
                        is_directory: meta.is_dir(),
                        is_file: meta.is_file(),
                        is_symlink: meta.file_type().is_symlink(),
                        size: meta.len() as i64,
                        mode,
                        mtime,
                        atime,
                        permissions: format_permissions(&meta),
                    });
                }
            }
        }

        let resp = FileListResponse {
            files,
            cwd: virtual_path(&abs_path),
        };

        serde_json::to_string(&resp).map_err(|e| e.to_string())
    }

    pub fn handle_file_read(data: &str) -> Result<String, String> {
        let req: FileReadRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("文件路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        let meta = fs::metadata(&abs_path).map_err(|e| format!("获取文件信息失败: {}", e))?;

        let max_size = req.max_size.unwrap_or(2 * 1024 * 1024); // 2MB default

        if meta.len() as i64 > max_size {
            return Err(format!(
                "文件过大 ({} bytes), 最大允许 {} bytes",
                meta.len(),
                max_size
            ));
        }

        fs::read_to_string(&abs_path).map_err(|e| format!("读取文件失败: {}", e))
    }

    pub fn handle_file_write(data: &str) -> Result<String, String> {
        let req: FileWriteRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("文件路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        if let Some(parent) = abs_path.parent() {
            fs::create_dir_all(parent).map_err(|e| format!("创建父目录失败: {}", e))?;
        }

        fs::write(&abs_path, req.content.as_bytes()).map_err(|e| format!("写入文件失败: {}", e))?;

        Ok("文件保存成功".to_string())
    }

    pub fn handle_file_mkdir(data: &str) -> Result<String, String> {
        let req: FileMkdirRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("目录路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        fs::create_dir_all(&abs_path).map_err(|e| format!("创建目录失败: {}", e))?;

        Ok("目录创建成功".to_string())
    }

    pub fn handle_file_delete(data: &str) -> Result<String, String> {
        let req: FileDeleteRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;

        // Safety checks: 不允许删除任何被放行的根目录本身
        // （默认沙箱根与 API_MONITOR_FILE_ROOTS 额外根都算，
        // 否则一次递归删除就能把整棵被授权的目录清空）。
        if allowed_roots()?.contains(&abs_path) {
            return Err("不允许删除文件根目录".to_string());
        }

        let meta = fs::metadata(&abs_path).map_err(|e| format!("文件不存在: {}", e))?;

        if meta.is_dir() {
            if req.recursive {
                fs::remove_dir_all(&abs_path).map_err(|e| format!("删除目录失败: {}", e))?;
            } else {
                fs::remove_dir(&abs_path).map_err(|e| {
                    format!("删除空目录失败 (如需递归删除请设置 recursive=true): {}", e)
                })?;
            }
            Ok("目录已删除".to_string())
        } else {
            fs::remove_file(&abs_path).map_err(|e| format!("删除文件失败: {}", e))?;
            Ok("文件已删除".to_string())
        }
    }

    pub fn handle_file_rename(data: &str) -> Result<String, String> {
        let req: FileRenameRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.old_path.is_empty() || req.new_path.is_empty() {
            return Err("路径不能为空".to_string());
        }

        let old_abs = resolve_path(&req.old_path)?;
        let new_abs = resolve_path(&req.new_path)?;

        fs::rename(old_abs, new_abs).map_err(|e| format!("重命名失败: {}", e))?;

        Ok("重命名成功".to_string())
    }

    pub fn handle_file_stat(data: &str) -> Result<String, String> {
        let req: FileStatRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        let meta = fs::metadata(&abs_path).map_err(|e| format!("获取文件信息失败: {}", e))?;

        let mtime = meta
            .modified()
            .ok()
            .and_then(|t| t.duration_since(std::time::SystemTime::UNIX_EPOCH).ok())
            .map(|d| d.as_millis() as i64)
            .unwrap_or(0);

        let atime = meta
            .accessed()
            .ok()
            .and_then(|t| t.duration_since(std::time::SystemTime::UNIX_EPOCH).ok())
            .map(|d| d.as_millis() as i64)
            .unwrap_or(mtime);

        #[cfg(unix)]
        let mode = {
            use std::os::unix::fs::MetadataExt;
            meta.mode() & 0o777
        };
        #[cfg(not(unix))]
        let mode = 0o755;

        let entry = FileEntry {
            name: abs_path
                .file_name()
                .map(|n| n.to_string_lossy().to_string())
                .unwrap_or_default(),
            path: virtual_path(&abs_path),
            is_directory: meta.is_dir(),
            is_file: meta.is_file(),
            is_symlink: meta.file_type().is_symlink(),
            size: meta.len() as i64,
            mode,
            mtime,
            atime,
            permissions: format_permissions(&meta),
        };

        serde_json::to_string(&entry).map_err(|e| e.to_string())
    }

    pub fn handle_file_chmod(data: &str) -> Result<String, String> {
        let req: FileChmodRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("路径不能为空".to_string());
        }

        if cfg!(target_os = "windows") {
            return Err("Windows 系统不支持 chmod 操作".to_string());
        }

        #[cfg(unix)]
        {
            let abs_path = resolve_path(&req.path)?;
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(&abs_path, fs::Permissions::from_mode(req.mode))
                .map_err(|e| format!("修改权限失败: {}", e))?;
            Ok("权限修改成功".to_string())
        }
        #[cfg(not(unix))]
        {
            Err("不支持的操作".to_string())
        }
    }

    pub fn handle_file_download_chunk(data: &str) -> Result<String, String> {
        let req: FileDownloadChunkRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("文件路径不能为空".to_string());
        }

        let chunk_size = if req.size <= 0 || req.size > 2 * 1024 * 1024 {
            1024 * 1024 // 1MB default
        } else {
            req.size as usize
        };

        let abs_path = resolve_path(&req.path)?;
        let mut file = fs::File::open(&abs_path).map_err(|e| format!("打开文件失败: {}", e))?;

        file.seek(SeekFrom::Start(req.offset as u64))
            .map_err(|e| format!("定位文件失败: {}", e))?;

        let mut buffer = vec![0; chunk_size];
        let bytes_read = file
            .read(&mut buffer)
            .map_err(|e| format!("读取文件失败: {}", e))?;

        let encoded = general_purpose::STANDARD.encode(&buffer[..bytes_read]);
        Ok(encoded)
    }

    /// 把主机上的文件直接 PUT 到面板签发的签名 URL（存储节点直传）。
    /// 字节由 Agent 直接发往目标节点，不经面板中转，节省面板流量。
    pub async fn handle_file_upload_to_url(data: &str) -> Result<String, String> {
        let req: FileUploadToUrlRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("文件路径不能为空".to_string());
        }
        if req.url.is_empty() {
            return Err("上传地址不能为空".to_string());
        }
        let method = req
            .method
            .as_deref()
            .unwrap_or("PUT")
            .trim()
            .to_uppercase();
        if method != "PUT" && method != "POST" {
            return Err("仅支持 PUT/POST 上传".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        let meta = fs::metadata(&abs_path).map_err(|e| format!("获取文件信息失败: {}", e))?;
        if !meta.is_file() {
            return Err("目标不是文件".to_string());
        }
        let size = meta.len() as i64;

        let file = tokio::fs::File::open(&abs_path)
            .await
            .map_err(|e| format!("打开文件失败: {}", e))?;
        let stream = tokio_util::io::ReaderStream::new(file);
        let body = reqwest::Body::wrap_stream(stream);

        let client = reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(3600))
            .build()
            .map_err(|e| format!("创建 HTTP 客户端失败: {}", e))?;

        let mut builder = client
            .request(
                reqwest::Method::from_bytes(method.as_bytes())
                    .map_err(|e| format!("非法方法: {}", e))?,
                &req.url,
            )
            .header(
                reqwest::header::CONTENT_TYPE,
                req.content_type
                    .clone()
                    .unwrap_or_else(|| "application/octet-stream".to_string()),
            )
            .header(reqwest::header::CONTENT_LENGTH, size)
            .body(body);

        if method == "PUT" {
            builder = builder.header("X-Agent-Upload", "filebox");
        }

        let resp = builder
            .send()
            .await
            .map_err(|e| format!("上传失败: {}", e))?;
        let status = resp.status().as_u16();
        if !(200..300).contains(&status) {
            let text = resp.text().await.unwrap_or_default();
            return Err(format!("存储节点拒绝上传 (HTTP {}): {}", status, text));
        }

        let payload = FileUploadToUrlResponse {
            success: true,
            status,
            size,
        };
        serde_json::to_string(&payload).map_err(|e| e.to_string())
    }

    /// 从 URL 流式下载并写入主机本地路径。字节不经面板中转，适合把已有的
    /// 可访问地址（文件柜直链、GitHub release 等）直接落到主机上。
    pub async fn handle_file_fetch_url(data: &str) -> Result<String, String> {
        let req: FileFetchUrlRequest =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.url.is_empty() {
            return Err("下载地址不能为空".to_string());
        }
        if req.path.is_empty() {
            return Err("目标路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        if let Some(parent) = abs_path.parent() {
            fs::create_dir_all(parent).map_err(|e| format!("创建父目录失败: {}", e))?;
        }

        let client = reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(3600))
            .build()
            .map_err(|e| format!("创建 HTTP 客户端失败: {}", e))?;

        let mut builder = client.get(&req.url);
        if let Some(headers) = &req.headers {
            for (key, value) in headers {
                builder = builder.header(key.as_str(), value.as_str());
            }
        }

        let resp = builder
            .send()
            .await
            .map_err(|e| format!("下载失败: {}", e))?;
        let status = resp.status().as_u16();
        if !(200..300).contains(&status) {
            return Err(format!("下载源返回 HTTP {}", status));
        }

        let tmp_name = format!(
            "{}.download.tmp.{}",
            abs_path
                .file_name()
                .and_then(|n| n.to_str())
                .unwrap_or("download"),
            std::process::id()
        );
        let tmp_path = abs_path.with_file_name(tmp_name);
        let mut file = tokio::fs::File::create(&tmp_path)
            .await
            .map_err(|e| format!("创建临时文件失败: {}", e))?;

        use futures_util::StreamExt;
        use tokio::io::AsyncWriteExt;
        let mut stream = resp.bytes_stream();
        let mut written: i64 = 0;
        while let Some(chunk) = stream.next().await {
            let chunk = match chunk {
                Ok(c) => c,
                Err(e) => {
                    let _ = tokio::fs::remove_file(&tmp_path).await;
                    return Err(format!("读取下载流失败: {}", e));
                }
            };
            if let Err(e) = file.write_all(&chunk).await {
                let _ = tokio::fs::remove_file(&tmp_path).await;
                return Err(format!("写入文件失败: {}", e));
            }
            written += chunk.len() as i64;
        }
        if let Err(e) = file.flush().await {
            let _ = tokio::fs::remove_file(&tmp_path).await;
            return Err(format!("刷新文件失败: {}", e));
        }
        drop(file);

        if let Err(e) = tokio::fs::rename(&tmp_path, &abs_path).await {
            let _ = tokio::fs::remove_file(&tmp_path).await;
            return Err(format!("落盘失败: {}", e));
        }

        let payload = FileFetchUrlResponse {
            success: true,
            status,
            size: written,
            path: virtual_path(&abs_path),
        };
        serde_json::to_string(&payload).map_err(|e| e.to_string())
    }

    /// 按 base64 分块写入文件字节，支持 offset 续写与首块截断。
    /// 用于把二进制文件分块传到主机（绕开单帧大小与文本编码限制）。
    pub fn handle_file_write_base64(data: &str) -> Result<String, String> {
        let req: FileWriteBase64Request =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("文件路径不能为空".to_string());
        }

        let bytes = general_purpose::STANDARD
            .decode(req.data.as_bytes())
            .map_err(|e| format!("base64 解码失败: {}", e))?;

        let abs_path = resolve_path(&req.path)?;
        if let Some(parent) = abs_path.parent() {
            fs::create_dir_all(parent).map_err(|e| format!("创建父目录失败: {}", e))?;
        }

        let mut file = if req.truncate {
            fs::OpenOptions::new()
                .create(true)
                .write(true)
                .truncate(true)
                .open(&abs_path)
        } else {
            fs::OpenOptions::new()
                .create(true)
                .write(true)
                .open(&abs_path)
        }
        .map_err(|e| format!("打开文件失败: {}", e))?;

        if req.offset > 0 {
            file.seek(SeekFrom::Start(req.offset as u64))
                .map_err(|e| format!("定位写入偏移失败: {}", e))?;
        }
        file.write_all(&bytes)
            .map_err(|e| format!("写入文件失败: {}", e))?;
        file.flush().map_err(|e| format!("刷新文件失败: {}", e))?;

        Ok(format!("已写入 {} 字节", bytes.len()))
    }
}

static FILES_ROOT_CACHE: OnceLock<Result<PathBuf, String>> = OnceLock::new();

/// 文件沙箱根目录：确保目录存在并返回规范化绝对路径。
/// 仅在目录缺失时才执行创建，避免在只读操作中反复产生写副作用。
/// 结果缓存于 OnceLock，后续调用直接返回已缓存的规范路径，避免重复 syscall。
fn canonical_files_root() -> Result<PathBuf, String> {
    FILES_ROOT_CACHE
        .get_or_init(|| {
            let root = PathBuf::from(FILES_ROOT);
            if !root.exists() {
                fs::create_dir_all(&root).map_err(|e| format!("创建文件根目录失败: {}", e))?;
            }
            fs::canonicalize(&root).map_err(|e| format!("读取文件根目录失败: {}", e))
        })
        .clone()
}

/// 额外允许的文件根目录，来自环境变量 `API_MONITOR_FILE_ROOTS`
/// （多路径使用平台分隔符：Windows 为 `;`，类 Unix 为 `:`）。
/// 未设置时返回空列表，此时文件操作行为与历史版本完全一致，仅限默认沙箱。
/// 用于显式放行脚本目录等受控路径，避免把整个文件系统暴露给面板。
fn extra_roots() -> Vec<PathBuf> {
    static EXTRA_ROOTS_CACHE: OnceLock<Vec<PathBuf>> = OnceLock::new();
    EXTRA_ROOTS_CACHE
        .get_or_init(|| {
            let Ok(raw) = std::env::var("API_MONITOR_FILE_ROOTS") else {
                return Vec::new();
            };
            let mut roots = Vec::new();
            for p in std::env::split_paths(&raw) {
                if p.as_os_str().is_empty() {
                    continue;
                }
                if let Ok(canonical) = fs::canonicalize(&p) {
                    if !roots.contains(&canonical) {
                        roots.push(canonical);
                    }
                }
            }
            roots
        })
        .clone()
}

/// 返回全部允许的根目录（默认沙箱在前，额外根在后）。
fn allowed_roots() -> Result<Vec<PathBuf>, String> {
    let mut roots = vec![canonical_files_root()?];
    roots.extend(extra_roots());
    Ok(roots)
}

/// 判断路径是否落在某个允许根下，返回命中的根。
fn matched_root(path: &Path, roots: &[PathBuf]) -> Option<PathBuf> {
    roots
        .iter()
        .find(|root| path.starts_with(root.as_path()))
        .cloned()
}

/// 把真实路径转换为对外暴露的虚拟路径。
/// 默认沙箱根显示为 `/`，子路径为 `/a/b`，保证前端面包屑始终锚定在 `/`。
/// 额外白名单根下的路径不参与虚拟化，直接返回其规范化绝对路径。
fn virtual_path(abs_path: &Path) -> String {
    let fallback = abs_path.to_string_lossy().replace('\\', "/");
    let Ok(default_root) = canonical_files_root() else {
        return fallback;
    };
    // 非默认根（来自额外白名单）直接用绝对路径展示，避免前端面包屑误锚定。
    if abs_path != default_root.as_path() && !abs_path.starts_with(&default_root) {
        return fallback;
    }
    if abs_path == default_root.as_path() {
        return "/".to_string();
    }
    let rel = abs_path.strip_prefix(&default_root).unwrap_or(abs_path);
    let normalized = rel.to_string_lossy().replace('\\', "/");
    format!("/{}", normalized.trim_start_matches('/'))
}

/// 解析输入路径为绝对路径，并返回其所属的允许根。
/// 优先按「额外白名单根」做绝对路径匹配；未命中再退回历史语义——
/// 把绝对路径锚定到默认沙箱根内部解析。
fn resolve_path_root(input_path: &str) -> Result<(PathBuf, PathBuf), String> {
    let roots = allowed_roots()?;
    let default_root = canonical_files_root()?;

    let normalized = input_path.replace('\\', "/");
    let trimmed = normalized.trim_matches(|c| c == '/' || c == ' ');

    // 额外白名单根：允许用真实绝对路径直接访问（不做沙箱内锚定）。
    let extra = extra_roots();
    if !extra.is_empty() && !trimmed.is_empty() && trimmed != "." {
        let candidate = PathBuf::from(trimmed);
        if candidate.is_absolute() {
            if let Ok(canonical) = fs::canonicalize(&candidate) {
                if let Some(root) = matched_root(&canonical, &extra) {
                    return Ok((canonical, root));
                }
            } else if let Some(parent) = candidate.parent() {
                if let Ok(parent_canonical) = fs::canonicalize(parent) {
                    if let Some(file_name) = candidate.file_name() {
                        let joined = parent_canonical.join(file_name);
                        if let Some(root) = matched_root(&joined, &extra) {
                            return Ok((joined, root));
                        }
                    }
                }
            }
        }
    }

    if trimmed.is_empty() || trimmed == "." {
        return Ok((default_root.clone(), default_root));
    }

    // 默认沙箱语义：绝对路径（含 Windows 盘符形式）锚定到沙箱根内解析。
    let rel = strip_drive_prefix(trimmed.trim_start_matches('/'));
    let abs = validate_within_allowed_root(&default_root.join(rel), &roots)?;
    Ok((abs, default_root))
}

/// 解析输入路径为绝对路径（历史签名，供仅需路径的调用点使用）。
fn resolve_path(input_path: &str) -> Result<PathBuf, String> {
    resolve_path_root(input_path).map(|(path, _)| path)
}

/// 剥离 Windows 盘符前缀（如 `C:`），返回去掉前缀后的路径片段。
/// 输入首两个字节必须是 ASCII 盘符（字母 + 冒号），否则原样返回。
fn strip_drive_prefix(p: &str) -> &str {
    let bytes = p.as_bytes();
    if bytes.len() >= 2 && bytes[0].is_ascii_alphabetic() && bytes[1] == b':' {
        p[2..].trim_start_matches('/')
    } else {
        p
    }
}

fn validate_within_allowed_root(path: &Path, roots: &[PathBuf]) -> Result<PathBuf, String> {
    let canonical = if path.exists() {
        fs::canonicalize(path).map_err(|e| format!("无法访问路径: {}", e))?
    } else {
        let parent = path.parent().ok_or_else(|| "无效的路径".to_string())?;
        let parent_canonical =
            fs::canonicalize(parent).map_err(|e| format!("无法访问父目录: {}", e))?;
        let file_name = path.file_name().ok_or_else(|| "无效的文件名".to_string())?;
        parent_canonical.join(file_name)
    };

    if matched_root(&canonical, roots).is_none() {
        return Err(format!(
            "路径不在允许的文件目录范围内。允许的根目录: {}",
            roots
                .iter()
                .map(|r| r.display().to_string())
                .collect::<Vec<_>>()
                .join(", ")
        ));
    }

    Ok(canonical)
}

fn format_permissions(meta: &fs::Metadata) -> String {
    let mut s = String::new();
    if meta.is_dir() {
        s.push('d');
    } else if meta.file_type().is_symlink() {
        s.push('l');
    } else {
        s.push('-');
    }

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mode = meta.permissions().mode();
        let perms = ["---", "--x", "-w-", "-wx", "r--", "r-x", "rw-", "rwx"];
        s.push_str(perms[((mode >> 6) & 7) as usize]);
        s.push_str(perms[((mode >> 3) & 7) as usize]);
        s.push_str(perms[(mode & 7) as usize]);
    }
    #[cfg(not(unix))]
    {
        if meta.permissions().readonly() {
            s.push_str("r--r--r--");
        } else {
            s.push_str("rwxrwxrwx");
        }
    }
    s
}
