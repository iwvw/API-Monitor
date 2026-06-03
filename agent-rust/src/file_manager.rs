use std::fs;
use std::path::{Path, PathBuf};
use std::io::{Read, Seek, SeekFrom};
use serde::{Serialize, Deserialize};
use base64::{Engine as _, engine::general_purpose};

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

pub struct FileManager;

impl FileManager {
    pub fn handle_file_list(data: &str) -> Result<String, String> {
        let req: FileListRequest = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        let abs_path = resolve_path(&req.path)?;

        // Windows virtual root: list drives
        if cfg!(target_os = "windows") && abs_path == PathBuf::from("/") {
            let resp = FileListResponse {
                files: list_windows_drives(),
                cwd: "/".to_string(),
            };
            return serde_json::to_string(&resp).map_err(|e| e.to_string());
        }

        let entries = fs::read_dir(&abs_path)
            .map_err(|e| format!("读取目录失败: {}", e))?;

        let mut files = Vec::new();
        for entry_res in entries {
            if let Ok(entry) = entry_res {
                if let Ok(meta) = entry.metadata() {
                    let file_path = entry.path();
                    let file_name = entry.file_name().to_string_lossy().to_string();
                    let path_str = normalize_path(&file_path);

                    let mtime = meta.modified()
                        .ok()
                        .and_then(|t| t.duration_since(std::time::SystemTime::UNIX_EPOCH).ok())
                        .map(|d| d.as_millis() as i64)
                        .unwrap_or(0);

                    let atime = meta.accessed()
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
            cwd: normalize_path(&abs_path),
        };

        serde_json::to_string(&resp).map_err(|e| e.to_string())
    }

    pub fn handle_file_read(data: &str) -> Result<String, String> {
        let req: FileReadRequest = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("文件路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        let meta = fs::metadata(&abs_path)
            .map_err(|e| format!("获取文件信息失败: {}", e))?;

        let max_size = req.max_size.unwrap_or(2 * 1024 * 1024); // 2MB default

        if meta.len() as i64 > max_size {
            return Err(format!("文件过大 ({} bytes), 最大允许 {} bytes", meta.len(), max_size));
        }

        fs::read_to_string(&abs_path)
            .map_err(|e| format!("读取文件失败: {}", e))
    }

    pub fn handle_file_write(data: &str) -> Result<String, String> {
        let req: FileWriteRequest = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("文件路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        if let Some(parent) = abs_path.parent() {
            fs::create_dir_all(parent)
                .map_err(|e| format!("创建父目录失败: {}", e))?;
        }

        fs::write(&abs_path, req.content.as_bytes())
            .map_err(|e| format!("写入文件失败: {}", e))?;

        Ok("文件保存成功".to_string())
    }

    pub fn handle_file_mkdir(data: &str) -> Result<String, String> {
        let req: FileMkdirRequest = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("目录路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        fs::create_dir_all(&abs_path)
            .map_err(|e| format!("创建目录失败: {}", e))?;

        Ok("目录创建成功".to_string())
    }

    pub fn handle_file_delete(data: &str) -> Result<String, String> {
        let req: FileDeleteRequest = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        
        // Safety checks: do not allow deleting root directory
        let clean_path = abs_path.clone();
        if clean_path == PathBuf::from("/") {
            return Err("不允许删除根目录".to_string());
        }

        if cfg!(target_os = "windows") {
            let path_str = clean_path.to_string_lossy();
            if path_str.len() <= 3 && path_str.ends_with(":\\") {
                return Err("不允许删除驱动器根目录".to_string());
            }
        }

        let meta = fs::metadata(&abs_path)
            .map_err(|e| format!("文件不存在: {}", e))?;

        if meta.is_dir() {
            if req.recursive {
                fs::remove_dir_all(&abs_path)
                    .map_err(|e| format!("删除目录失败: {}", e))?;
            } else {
                fs::remove_dir(&abs_path)
                    .map_err(|e| format!("删除空目录失败 (如需递归删除请设置 recursive=true): {}", e))?;
            }
            Ok("目录已删除".to_string())
        } else {
            fs::remove_file(&abs_path)
                .map_err(|e| format!("删除文件失败: {}", e))?;
            Ok("文件已删除".to_string())
        }
    }

    pub fn handle_file_rename(data: &str) -> Result<String, String> {
        let req: FileRenameRequest = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.old_path.is_empty() || req.new_path.is_empty() {
            return Err("路径不能为空".to_string());
        }

        let old_abs = resolve_path(&req.old_path)?;
        let new_abs = resolve_path(&req.new_path)?;

        fs::rename(old_abs, new_abs)
            .map_err(|e| format!("重命名失败: {}", e))?;

        Ok("重命名成功".to_string())
    }

    pub fn handle_file_stat(data: &str) -> Result<String, String> {
        let req: FileStatRequest = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("路径不能为空".to_string());
        }

        let abs_path = resolve_path(&req.path)?;
        let meta = fs::metadata(&abs_path)
            .map_err(|e| format!("获取文件信息失败: {}", e))?;

        let mtime = meta.modified()
            .ok()
            .and_then(|t| t.duration_since(std::time::SystemTime::UNIX_EPOCH).ok())
            .map(|d| d.as_millis() as i64)
            .unwrap_or(0);

        let atime = meta.accessed()
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
            name: abs_path.file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default(),
            path: normalize_path(&abs_path),
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
        let req: FileChmodRequest = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

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
        let req: FileDownloadChunkRequest = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.path.is_empty() {
            return Err("文件路径不能为空".to_string());
        }

        let chunk_size = if req.size <= 0 || req.size > 2 * 1024 * 1024 {
            1024 * 1024 // 1MB default
        } else {
            req.size as usize
        };

        let abs_path = resolve_path(&req.path)?;
        let mut file = fs::File::open(&abs_path)
            .map_err(|e| format!("打开文件失败: {}", e))?;

        file.seek(SeekFrom::Start(req.offset as u64))
            .map_err(|e| format!("定位文件失败: {}", e))?;

        let mut buffer = vec![0; chunk_size];
        let bytes_read = file.read(&mut buffer)
            .map_err(|e| format!("读取文件失败: {}", e))?;

        let encoded = general_purpose::STANDARD.encode(&buffer[..bytes_read]);
        Ok(encoded)
    }
}

fn normalize_path(p: &Path) -> String {
    p.to_string_lossy().replace('\\', "/")
}

fn resolve_path(input_path: &str) -> Result<PathBuf, String> {
    if input_path.is_empty() || input_path == "." {
        if let Ok(home) = std::env::var("USERPROFILE").or_else(|_| std::env::var("HOME")) {
            return Ok(PathBuf::from(home));
        }
        return std::env::current_dir().map_err(|e| e.to_string());
    }

    if cfg!(target_os = "windows") && (input_path == "/" || input_path == "\\") {
        return Ok(PathBuf::from("/"));
    }

    let mut p = input_path.replace('\\', "/");
    if cfg!(target_os = "windows") && p.starts_with('/') && p.len() >= 3 && p.chars().nth(2) == Some(':') {
        p = p[1..].to_string();
    }

    if cfg!(target_os = "windows") && p.len() == 2 && p.ends_with(':') {
        p.push('/');
    }

    let path = PathBuf::from(&p);
    if path.is_absolute() {
        Ok(path)
    } else {
        let mut abs = std::env::current_dir().map_err(|e| e.to_string())?;
        abs.push(path);
        Ok(abs)
    }
}

fn list_windows_drives() -> Vec<FileEntry> {
    let mut drives = Vec::new();
    for letter in b'A'..=b'Z' {
        let drive_letter = letter as char;
        let drive_path = format!("{}:\\", drive_letter);
        if Path::new(&drive_path).exists() {
            drives.push(FileEntry {
                name: format!("{}:", drive_letter),
                path: format!("{}:/", drive_letter),
                is_directory: true,
                is_file: false,
                is_symlink: false,
                size: 0,
                mode: 0o755,
                mtime: 0,
                atime: 0,
                permissions: "drwxr-xr-x".to_string(),
            });
        }
    }
    drives
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
