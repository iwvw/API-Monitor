use serde::Deserialize;
use std::path::{Path, PathBuf};

#[derive(Debug, Deserialize, Default)]
struct Request {
    operation: String,
    #[serde(default)]
    token: String,
    #[serde(default)]
    version: String,
    #[serde(default)]
    #[allow(dead_code)] // Windows 分支不读取 Linux 资产字段
    asset_url_amd64: String,
    #[serde(default)]
    #[allow(dead_code)]
    asset_sha256_amd64: String,
    #[serde(default)]
    #[allow(dead_code)]
    asset_url_arm64: String,
    #[serde(default)]
    #[allow(dead_code)]
    asset_sha256_arm64: String,
    #[serde(default)]
    #[allow(dead_code)] // Linux 分支不读取 Windows 资产字段
    asset_url_windows_amd64: String,
    #[serde(default)]
    #[allow(dead_code)]
    asset_sha256_windows_amd64: String,
}

pub fn reconcile(raw: &str) -> Result<String, String> {
    let request: Request = serde_json::from_str(raw)
        .map_err(|err| format!("invalid cloudflared desired state: {err}"))?;
    match request.operation.trim().to_ascii_lowercase().as_str() {
        "install" | "reconcile" => install(&request),
        "remove" | "uninstall" => remove(),
        "status" => status(),
        _ => Err("cloudflared operation must be install, remove, or status".to_string()),
    }
}

// ==================== Unix（systemd 服务） ====================

#[cfg(unix)]
const CONFIG_ROOT: &str = "/etc/api-monitor/cloudflared";
#[cfg(unix)]
const RUNTIME_ROOT: &str = "/opt/api-monitor/cloudflared/versions";
#[cfg(unix)]
const UNIT_PATH: &str = "/etc/systemd/system/api-monitor-cloudflared.service";

#[cfg(unix)]
fn install(request: &Request) -> Result<String, String> {
    use std::fs;
    use std::os::unix::fs::PermissionsExt;

    validate_token(request)?;
    let binary = ensure_binary_unix(request)?;
    let root = Path::new(CONFIG_ROOT);
    fs::create_dir_all(root)
        .map_err(|err| format!("create cloudflared config directory: {err}"))?;
    fs::set_permissions(root, fs::Permissions::from_mode(0o700))
        .map_err(|err| format!("secure cloudflared config directory: {err}"))?;
    atomic_write(&root.join("token"), request.token.trim().as_bytes(), 0o600)?;

    let unit = format!(
        "[Unit]\nDescription=API Monitor managed Cloudflare Tunnel\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart={} --no-autoupdate tunnel run --token-file {}/token\nRestart=always\nRestartSec=5s\nStartLimitIntervalSec=0\nNoNewPrivileges=true\nPrivateTmp=true\nProtectHome=true\nProtectSystem=strict\nReadOnlyPaths={}\nCapabilityBoundingSet=\nLockPersonality=true\nMemoryDenyWriteExecute=true\nRestrictSUIDSGID=true\n\n[Install]\nWantedBy=multi-user.target\n",
        binary.display(), CONFIG_ROOT, CONFIG_ROOT
    );
    atomic_write(Path::new(UNIT_PATH), unit.as_bytes(), 0o644)?;
    systemctl(&["daemon-reload"])?;
    systemctl(&["enable", "--now", "api-monitor-cloudflared.service"])?;
    systemctl(&["restart", "api-monitor-cloudflared.service"])?;
    systemctl(&["is-active", "--quiet", "api-monitor-cloudflared.service"])?;
    Ok(serde_json::json!({"status":"running","version":request.version}).to_string())
}

#[cfg(unix)]
fn remove() -> Result<String, String> {
    use std::fs;

    let _ = systemctl(&["disable", "--now", "api-monitor-cloudflared.service"]);
    let _ = fs::remove_file(UNIT_PATH);
    let _ = fs::remove_dir_all(CONFIG_ROOT);
    let _ = fs::remove_dir_all("/opt/api-monitor/cloudflared");
    systemctl(&["daemon-reload"])?;
    let _ = systemctl(&["reset-failed", "api-monitor-cloudflared.service"]);
    Ok(serde_json::json!({"status":"removed"}).to_string())
}

#[cfg(unix)]
fn status() -> Result<String, String> {
    use std::process::Command;

    let active = Command::new("systemctl")
        .args(["is-active", "--quiet", "api-monitor-cloudflared.service"])
        .status()
        .is_ok_and(|status| status.success());
    Ok(serde_json::json!({"status": if active { "running" } else { "stopped" }}).to_string())
}

#[cfg(unix)]
fn ensure_binary_unix(request: &Request) -> Result<PathBuf, String> {
    use std::fs;
    use std::os::unix::fs::PermissionsExt;
    use std::process::Command;

    if request.version.trim().is_empty() {
        return Err("cloudflared version is required".to_string());
    }
    let (url, digest) = match std::env::consts::ARCH {
        "x86_64" => (&request.asset_url_amd64, &request.asset_sha256_amd64),
        "aarch64" => (&request.asset_url_arm64, &request.asset_sha256_arm64),
        arch => return Err(format!("unsupported cloudflared architecture: {arch}")),
    };
    validate_asset(url, digest)?;
    let version_dir = PathBuf::from(RUNTIME_ROOT).join(request.version.trim());
    let binary = version_dir.join("cloudflared");
    if binary.is_file()
        && Command::new(&binary)
            .arg("--version")
            .output()
            .is_ok_and(|output| output.status.success())
    {
        return Ok(binary);
    }
    fs::create_dir_all(&version_dir)
        .map_err(|err| format!("create cloudflared runtime directory: {err}"))?;
    let candidate = version_dir.join(".cloudflared.download");
    let _ = fs::remove_file(&candidate);
    run(
        Command::new("curl")
            .args([
                "--fail",
                "--location",
                "--retry",
                "3",
                "--retry-all-errors",
                "--connect-timeout",
                "15",
                "--proto",
                "=https",
                "--tlsv1.2",
                "--output",
            ])
            .arg(&candidate)
            .arg(url),
        "download cloudflared",
    )?;
    let output = Command::new("sha256sum")
        .arg(&candidate)
        .output()
        .map_err(|err| format!("run sha256sum: {err}"))?;
    let actual = String::from_utf8_lossy(&output.stdout)
        .split_whitespace()
        .next()
        .unwrap_or("")
        .to_ascii_lowercase();
    if !output.status.success() || actual != digest.to_ascii_lowercase() {
        let _ = fs::remove_file(&candidate);
        return Err("cloudflared SHA-256 verification failed".to_string());
    }
    fs::set_permissions(&candidate, fs::Permissions::from_mode(0o755))
        .map_err(|err| format!("make cloudflared executable: {err}"))?;
    fs::rename(&candidate, &binary).map_err(|err| format!("activate cloudflared: {err}"))?;
    run(
        Command::new(&binary).arg("--version"),
        "verify cloudflared executable",
    )?;
    Ok(binary)
}

// ==================== Windows（后台进程 + 开机计划任务） ====================

#[cfg(windows)]
fn config_root_windows() -> PathBuf {
    let base = std::env::var_os("ProgramData")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("C:\\ProgramData"));
    base.join("api-monitor").join("cloudflared")
}

#[cfg(windows)]
fn binary_path_windows(request: &Request) -> Result<PathBuf, String> {
    if request.version.trim().is_empty() {
        return Err("cloudflared version is required".to_string());
    }
    let url = &request.asset_url_windows_amd64;
    let digest = &request.asset_sha256_windows_amd64;
    if url.is_empty() {
        return Err("cloudflared Windows 资产未配置（asset_url_windows_amd64）".to_string());
    }
    validate_asset(url, digest)?;
    let root = config_root_windows();
    let version_dir = root.join("versions").join(request.version.trim());
    let binary = version_dir.join("cloudflared.exe");
    if binary.is_file() {
        return Ok(binary);
    }
    std::fs::create_dir_all(&version_dir)
        .map_err(|err| format!("create cloudflared runtime directory: {err}"))?;
    let candidate = version_dir.join(".cloudflared.download.exe");
    let _ = std::fs::remove_file(&candidate);
    // 用 PowerShell Invoke-WebRequest 下载：走系统代理（Windows 常需代理访问 GitHub），
    // curl.exe 不读系统代理、且对国内网络易失败。
    let script = "param($u,$o) $ErrorActionPreference='Stop'; try { Invoke-WebRequest -Uri $u -OutFile $o -TimeoutSec 300 -UseBasicParsing -MaximumRedirection 5; exit 0 } catch { Write-Error $_.Exception.Message; exit 1 }";
    let status = std::process::Command::new("powershell")
        .args([
            "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script,
        ])
        .arg(url)
        .arg(&candidate)
        .status()
        .map_err(|err| format!("download cloudflared: {err}"))?;
    if !status.success() {
        let _ = std::fs::remove_file(&candidate);
        return Err("cloudflared 下载失败".to_string());
    }
    let actual = sha256_of(&candidate)?;
    if actual != digest.to_ascii_lowercase() {
        let _ = std::fs::remove_file(&candidate);
        return Err("cloudflared SHA-256 verification failed".to_string());
    }
    std::fs::rename(&candidate, &binary).map_err(|err| format!("activate cloudflared: {err}"))?;
    Ok(binary)
}

#[cfg(windows)]
fn sha256_of(path: &Path) -> Result<String, String> {
    // certutil: 输出行形如 "SHA256 的 = <hash>"
    let output = std::process::Command::new("certutil")
        .args(["-hashfile"])
        .arg(path)
        .arg("SHA256")
        .output()
        .map_err(|err| format!("run certutil: {err}"))?;
    if !output.status.success() {
        return Err("certutil 计算哈希失败".to_string());
    }
    let text = String::from_utf8_lossy(&output.stdout);
    for line in text.lines() {
        let trimmed = line.trim();
        if trimmed.len() == 64 && trimmed.bytes().all(|b| b.is_ascii_hexdigit()) {
            return Ok(trimmed.to_ascii_lowercase());
        }
    }
    Err("无法从 certutil 输出解析 SHA-256".to_string())
}

#[cfg(windows)]
fn spawn_cloudflared_windows(binary: &Path, token_file: &Path) -> Result<u32, String> {
    use std::os::windows::process::CommandExt;

    let log_file = config_root_windows().join("cloudflared.log");
    let log = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&log_file)
        .map_err(|err| format!("open cloudflared log: {err}"))?;
    let child = std::process::Command::new(binary)
        .args([
            "--no-autoupdate", "tunnel", "run", "--token-file",
        ])
        .arg(token_file)
        .stdout(std::process::Stdio::from(log.try_clone().map_err(|err| format!("clone log: {err}"))?))
        .stderr(std::process::Stdio::from(log))
        .creation_flags(0x08000000) // CREATE_NO_WINDOW
        .spawn()
        .map_err(|err| format!("spawn cloudflared: {err}"))?;
    let pid = child.id();
    std::fs::write(config_root_windows().join("cloudflared.pid"), pid.to_string())
        .map_err(|err| format!("write cloudflared pid: {err}"))?;
    Ok(pid)
}

#[cfg(windows)]
fn pid_alive_windows(pid: u32) -> bool {
    if pid == 0 {
        return false;
    }
    let output = std::process::Command::new("tasklist")
        .args(["/FI", &format!("PID eq {pid}"), "/NH"])
        .output();
    match output {
        Ok(out) => {
            let text = String::from_utf8_lossy(&out.stdout);
            text.contains(&pid.to_string())
        }
        Err(_) => false,
    }
}

#[cfg(windows)]
fn kill_pid_windows(pid: u32) {
    if pid == 0 {
        return;
    }
    let _ = std::process::Command::new("taskkill")
        .args(["/PID", &pid.to_string(), "/F", "/T"])
        .status();
}

#[cfg(windows)]
fn install(request: &Request) -> Result<String, String> {
    use std::fs;

    validate_token(request)?;
    let binary = binary_path_windows(request)?;
    let root = config_root_windows();
    fs::create_dir_all(&root).map_err(|err| format!("create cloudflared config directory: {err}"))?;
    let token_file = root.join("token");
    fs::write(&token_file, request.token.trim()).map_err(|err| format!("write cloudflared token: {err}"))?;

    // 重启当前实例（幂等：停旧进程再拉起新实例，保证使用最新 token/二进制）
    stop_cloudflared_windows();
    let pid = spawn_cloudflared_windows(&binary, &token_file)?;
    std::thread::sleep(std::time::Duration::from_millis(1200));
    if !pid_alive_windows(pid) {
        return Err("cloudflared 启动后进程即退出，请查看 %ProgramData%\\api-monitor\\cloudflared\\cloudflared.log".to_string());
    }
    // 开机自启：SYSTEM 启动任务（尽力而为；agent 存活期间进程也始终跟随）
    let _ = ensure_boot_task_windows(&binary, &token_file);
    Ok(serde_json::json!({"status":"running","version":request.version,"pid":pid}).to_string())
}

#[cfg(windows)]
fn ensure_boot_task_windows(binary: &Path, token_file: &Path) -> Result<(), String> {
    let tr = format!(
        "\\\"{}\\\" --no-autoupdate tunnel run --token-file \\\"{}\\\"",
        binary.display().to_string().replace('\\', "\\\\"),
        token_file.display().to_string().replace('\\', "\\\\")
    );
    let status = std::process::Command::new("schtasks")
        .args(["/Create", "/TN", "api-monitor-cloudflared", "/TR", &tr, "/SC", "ONSTART", "/RU", "SYSTEM", "/RL", "HIGHEST", "/F"])
        .status();
    match status {
        Ok(st) if st.success() => Ok(()),
        Ok(_) => Err("schtasks 创建任务失败".to_string()),
        Err(err) => Err(format!("schtasks: {err}")),
    }
}

#[cfg(windows)]
fn stop_cloudflared_windows() {
    let pid_file = config_root_windows().join("cloudflared.pid");
    if let Ok(text) = std::fs::read_to_string(&pid_file) {
        if let Ok(pid) = text.trim().parse::<u32>() {
            kill_pid_windows(pid);
        }
    }
    // 兜底：清理可能残留的同名工作进程（OLDER agent 可能未写 pid 文件）
    let _ = std::process::Command::new("taskkill")
        .args(["/IM", "cloudflared.exe", "/F", "/T"])
        .status();
}

#[cfg(windows)]
fn remove() -> Result<String, String> {
    stop_cloudflared_windows();
    let _ = std::process::Command::new("schtasks").args(["/Delete", "/TN", "api-monitor-cloudflared", "/F"]).status();
    let root = config_root_windows();
    if std::env::var_os("ProgramData").is_some() {
        let _ = std::fs::remove_dir_all(&root);
    }
    Ok(serde_json::json!({"status":"removed"}).to_string())
}

#[cfg(windows)]
fn status() -> Result<String, String> {
    let pid_file = config_root_windows().join("cloudflared.pid");
    let running = std::fs::read_to_string(&pid_file)
        .ok()
        .and_then(|text| text.trim().parse::<u32>().ok())
        .map(pid_alive_windows)
        .unwrap_or(false);
    Ok(serde_json::json!({"status": if running { "running" } else { "stopped" }}).to_string())
}

// ==================== 通用 ====================

fn validate_token(request: &Request) -> Result<(), String> {
    if request.token.trim().len() < 32 || request.token.chars().any(char::is_whitespace) {
        return Err("invalid scoped Cloudflare Tunnel token".to_string());
    }
    Ok(())
}

fn validate_asset(url: &str, digest: &str) -> Result<(), String> {
    if !url.starts_with("https://")
        || digest.len() != 64
        || !digest.bytes().all(|value| value.is_ascii_hexdigit())
    {
        return Err("cloudflared asset must use HTTPS and a SHA-256 digest".to_string());
    }
    Ok(())
}

#[cfg(unix)]
fn atomic_write(path: &Path, bytes: &[u8], mode: u32) -> Result<(), String> {
    use std::os::unix::fs::PermissionsExt;

    let temporary = path.with_extension("tmp");
    std::fs::write(&temporary, bytes).map_err(|err| format!("write {}: {err}", temporary.display()))?;
    std::fs::set_permissions(&temporary, std::fs::Permissions::from_mode(mode))
        .map_err(|err| format!("secure {}: {err}", temporary.display()))?;
    std::fs::rename(&temporary, path).map_err(|err| format!("commit {}: {err}", path.display()))
}

#[cfg(unix)]
fn systemctl(args: &[&str]) -> Result<(), String> {
    run(std::process::Command::new("systemctl").args(args), &format!("systemctl {}", args.join(" ")))
}

#[cfg(unix)]
fn run(command: &mut std::process::Command, label: &str) -> Result<(), String> {
    let output = command
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .output()
        .map_err(|err| format!("{label}: {err}"))?;
    if output.status.success() {
        Ok(())
    } else {
        Err(format!(
            "{label}: {}",
            String::from_utf8_lossy(&output.stderr).trim()
        ))
    }
}