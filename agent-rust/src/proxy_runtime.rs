#[cfg(unix)]
use serde::{Deserialize, Serialize};
#[cfg(unix)]
use std::fs;
#[cfg(unix)]
use std::net::{Ipv4Addr, SocketAddrV4, TcpListener, UdpSocket};
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
#[cfg(unix)]
use std::path::{Path, PathBuf};
#[cfg(unix)]
use std::process::{Command, Stdio};

#[cfg(unix)]
const STATE_ROOT: &str = "/var/lib/api-monitor/proxy/nodes";
#[cfg(unix)]
const CONFIG_ROOT: &str = "/etc/api-monitor/proxy/nodes";
#[cfg(unix)]
const RUNTIME_ROOT: &str = "/opt/api-monitor/proxy/versions";

#[cfg(unix)]
#[derive(Debug, Deserialize)]
pub struct ReconcileRequest {
    #[serde(default)]
    pub operation: String,
    pub node_id: String,
    pub revision: u64,
    pub runtime: String,
    pub runtime_version: String,
    pub asset_url_amd64: String,
    pub asset_sha256_amd64: String,
    pub asset_url_arm64: String,
    pub asset_sha256_arm64: String,
    #[serde(default = "default_asset_format")]
    pub asset_format: String,
    pub config: String,
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub remove: bool,
    #[serde(default)]
    pub requested_port: u16,
    #[serde(default)]
    pub excluded_ports: Vec<u16>,
    #[serde(default = "default_port_min")]
    pub port_min: u16,
    #[serde(default = "default_port_max")]
    pub port_max: u16,
    #[serde(default = "default_transport")]
    pub transport: String,
}

#[cfg(unix)]
#[derive(Debug, Serialize, Deserialize)]
struct AppliedState {
    revision: u64,
    runtime: String,
    assigned_port: u16,
    #[serde(default = "default_transport")]
    transport: String,
    #[serde(default)]
    stats_port: u16,
}

#[cfg(unix)]
fn default_port_min() -> u16 {
    45654
}
#[cfg(unix)]
fn default_port_max() -> u16 {
    55654
}
#[cfg(unix)]
fn default_transport() -> String {
    "tcp".to_string()
}

#[cfg(unix)]
fn default_asset_format() -> String {
    "archive".to_string()
}

#[cfg(unix)]
pub fn reconcile(raw: &str) -> Result<String, String> {
    let request: ReconcileRequest =
        serde_json::from_str(raw).map_err(|err| format!("invalid proxy desired state: {err}"))?;
    validate_supported_host()?;
    validate_node_id(&request.node_id)?;
    let runtime = normalize_runtime(&request.runtime)?;
    let transport = normalize_transport(&request.transport)?;
    match request.operation.as_str() {
        "install_runtime" => {
            let binary = ensure_runtime(&request, runtime)?;
            return Ok(serde_json::json!({
                "status": "installed", "runtime": runtime,
                "version": request.runtime_version, "binary": binary
            })
            .to_string());
        }
        "status_runtime" => {
            let binary = PathBuf::from(RUNTIME_ROOT)
                .join(runtime)
                .join(&request.runtime_version)
                .join(runtime);
            let installed = binary.is_file()
                && Command::new(&binary)
                    .arg("version")
                    .output()
                    .is_ok_and(|out| out.status.success());
            return Ok(serde_json::json!({
                "status": if installed { "installed" } else { "not_installed" },
                "installed": installed,
                "runtime": runtime, "version": if installed { request.runtime_version.as_str() } else { "" },
                "binary": binary
            }).to_string());
        }
        "status_node" => {
            let unit = unit_name(&request.node_id);
            let state_dir = PathBuf::from(STATE_ROOT).join(&request.node_id);
            let config = PathBuf::from(CONFIG_ROOT)
                .join(&request.node_id)
                .join("config.json");
            let state = read_applied_state(&state_dir.join("applied.json"));
            let active = unit_is_active(&unit);
            let status = match (&state, active, config.is_file()) {
                (None, false, false) => "missing",
                (Some(_), true, true) => "running",
                (Some(_), false, true) => "stopped",
                _ => "drifted",
            };
            return Ok(serde_json::json!({
                "node_id": request.node_id, "status": status, "service_active": active,
                "revision": state.as_ref().map(|value| value.revision).unwrap_or(0),
                "assigned_port": state.as_ref().map(|value| value.assigned_port).unwrap_or(0),
                "transport": state.as_ref().map(|value| value.transport.as_str()).unwrap_or(""),
                "stats_port": state.as_ref().map(|value| value.stats_port).unwrap_or(0),
                "config_present": config.is_file()
            })
            .to_string());
        }
        "remove_runtime" => {
            if has_managed_node_units()? {
                return Err(
                    "remove managed nodes before uninstalling the proxy runtime".to_string()
                );
            }
            for root in [
                Path::new("/opt/api-monitor/proxy"),
                Path::new("/etc/api-monitor/proxy"),
                Path::new("/var/lib/api-monitor/proxy"),
            ] {
                if root.exists() {
                    fs::remove_dir_all(root).map_err(|err| {
                        format!("remove proxy runtime data {}: {err}", root.display())
                    })?;
                }
            }
            return Ok(serde_json::json!({"status": "removed", "runtime": runtime}).to_string());
        }
        "" | "reconcile_node" => {}
        _ => return Err("unsupported proxy runtime operation".to_string()),
    }
    if request.revision == 0 || request.port_min < 1024 || request.port_min > request.port_max {
        return Err("invalid managed proxy revision or port range".to_string());
    }

    let unit = unit_name(&request.node_id);
    let state_dir = PathBuf::from(STATE_ROOT).join(&request.node_id);
    let config_dir = PathBuf::from(CONFIG_ROOT).join(&request.node_id);
    let applied_path = state_dir.join("applied.json");
    if request.remove {
        if let Some(state) = read_applied_state(&applied_path) {
            remove_firewall_port(state.assigned_port, &state.transport);
        }
        let _ = systemctl(&["disable", "--now", &unit]);
        let _ = fs::remove_file(format!("/etc/systemd/system/{unit}"));
        let _ = fs::remove_dir_all(&state_dir);
        let _ = fs::remove_dir_all(&config_dir);
        systemctl(&["daemon-reload"])?;
        return Ok(
            serde_json::json!({"node_id": request.node_id, "status": "removed"}).to_string(),
        );
    }

    let binary = ensure_runtime(&request, runtime)?;
    fs::create_dir_all(&state_dir).map_err(|err| format!("create proxy state directory: {err}"))?;
    fs::create_dir_all(&config_dir)
        .map_err(|err| format!("create proxy config directory: {err}"))?;
    set_file_mode(&state_dir, 0o700)?;
    set_file_mode(&config_dir, 0o700)?;

    let previous_state = read_applied_state(&applied_path);
    if previous_state.as_ref().is_some_and(|state| {
        state.revision == request.revision
            && state.assigned_port == request.requested_port
            && state.transport == transport
            && (state.stats_port > 0 || !config_uses_v2ray_stats(&request.config))
            && unit_is_active(&unit) == request.enabled
    }) {
        let state = previous_state.unwrap();
        return Ok(serde_json::json!({
            "node_id": request.node_id, "revision": state.revision, "runtime": runtime,
            "assigned_port": state.assigned_port, "transport": state.transport,
            "stats_port": state.stats_port,
            "status": "already_applied"
        })
        .to_string());
    }
    if previous_state
        .as_ref()
        .is_some_and(|state| state.revision > request.revision)
    {
        return Err(
            "host revision is newer than panel desired state; refusing downgrade".to_string(),
        );
    }

    let assigned_port = if previous_state.as_ref().is_some_and(|state| {
        state.assigned_port == request.requested_port && state.transport == transport
    }) {
        request.requested_port
    } else {
        find_available_port(
            request.requested_port,
            request.port_min,
            request.port_max,
            transport,
            &request.excluded_ports,
        )?
    };
    let stats_port = if config_uses_v2ray_stats(&request.config) {
        let preferred = previous_state
            .as_ref()
            .map(|state| state.stats_port)
            .unwrap_or(0);
        if preferred >= 20000 && preferred <= 29999 {
            preferred
        } else {
            find_available_port(
                0,
                20000,
                29999,
                "tcp",
                &reserved_stats_ports(&request.node_id),
            )?
        }
    } else {
        0
    };
    let effective_config = rewrite_runtime_bindings(&request.config, assigned_port, stats_port)?;
    let candidate = config_dir.join(format!("candidate-{}.json", request.revision));
    atomic_write(&candidate, effective_config.as_bytes(), 0o600)?;
    validate_config(runtime, &binary, &candidate)?;

    let active = config_dir.join("config.json");
    let previous = config_dir.join("previous.json");
    if active.exists() {
        fs::copy(&active, &previous).map_err(|err| format!("backup active proxy config: {err}"))?;
        set_file_mode(&previous, 0o600)?;
    }
    fs::rename(&candidate, &active).map_err(|err| format!("activate proxy config: {err}"))?;
    install_unit(&unit, runtime, &binary, &active)?;
    let firewall_adapter = ensure_firewall_port(assigned_port, transport)?;

    let apply_result = if request.enabled {
        systemctl(&["enable", "--now", &unit])?;
        systemctl(&["restart", &unit]).and_then(|_| systemctl(&["is-active", "--quiet", &unit]))
    } else {
        systemctl(&["disable", "--now", &unit])
    };
    if let Err(error) = apply_result {
        remove_firewall_port(assigned_port, transport);
        if previous.exists() {
            let _ = fs::copy(&previous, &active);
            let _ = systemctl(&["restart", &unit]);
        }
        return Err(format!(
            "apply proxy revision {}: {error}",
            request.revision
        ));
    }

    let state = serde_json::to_vec_pretty(&AppliedState {
        revision: request.revision,
        runtime: runtime.to_string(),
        assigned_port,
        transport: transport.to_string(),
        stats_port,
    })
    .map_err(|err| format!("serialize applied proxy state: {err}"))?;
    atomic_write(&applied_path, &state, 0o600)?;
    Ok(serde_json::json!({
        "node_id": request.node_id, "revision": request.revision, "runtime": runtime,
        "assigned_port": assigned_port, "transport": transport, "config": effective_config,
        "stats_port": stats_port,
        "firewall_adapter": firewall_adapter,
        "status": if request.enabled { "running" } else { "stopped" }
    })
    .to_string())
}

#[cfg(unix)]
fn has_managed_node_units() -> Result<bool, String> {
    let entries = fs::read_dir("/etc/systemd/system")
        .map_err(|err| format!("inspect managed proxy services: {err}"))?;
    for entry in entries {
        let name = entry
            .map_err(|err| format!("inspect managed proxy service entry: {err}"))?
            .file_name();
        let name = name.to_string_lossy();
        if name.starts_with("api-monitor-proxy@") && name.ends_with(".service") {
            return Ok(true);
        }
    }
    Ok(false)
}

#[cfg(unix)]
fn ensure_runtime(request: &ReconcileRequest, runtime: &str) -> Result<PathBuf, String> {
    if request.runtime_version.is_empty() {
        return Err("runtime_version is required".to_string());
    }
    let arch = std::env::consts::ARCH;
    let (url, sha256) = match arch {
        "x86_64" => (&request.asset_url_amd64, &request.asset_sha256_amd64),
        "aarch64" => (&request.asset_url_arm64, &request.asset_sha256_arm64),
        _ => return Err(format!("unsupported managed proxy architecture: {arch}")),
    };
    if !url.starts_with("https://")
        || sha256.len() != 64
        || !sha256.bytes().all(|b| b.is_ascii_hexdigit())
    {
        return Err("runtime asset must use HTTPS and a SHA-256 digest".to_string());
    }
    let version_dir = PathBuf::from(RUNTIME_ROOT)
        .join(runtime)
        .join(&request.runtime_version);
    let binary = version_dir.join(runtime);
    if binary.is_file()
        && Command::new(&binary)
            .arg("version")
            .output()
            .is_ok_and(|out| out.status.success())
    {
        return Ok(binary);
    }
    fs::create_dir_all(&version_dir).map_err(|err| format!("create runtime directory: {err}"))?;
    for dependency in ["curl", "sha256sum"] {
        ensure_command(dependency)?;
    }
    let _ = fs::remove_file(&binary);
    let asset = version_dir.join("runtime.download");
    let extract_dir = version_dir.join("extracting");
    let _ = fs::remove_dir_all(&extract_dir);
    fs::create_dir_all(&extract_dir)
        .map_err(|err| format!("create runtime extraction directory: {err}"))?;
    run(
        Command::new("curl")
            .args([
                "--silent",
                "--show-error",
                "--fail",
                "--location",
                "--retry",
                "3",
                "--retry-all-errors",
                "--connect-timeout",
                "15",
                "--max-time",
                "300",
                "--proto",
                "=https",
                "--tlsv1.2",
                "--output",
            ])
            .arg(&asset)
            .arg(url),
        "download proxy runtime",
    )?;
    let output = Command::new("sha256sum")
        .arg(&asset)
        .output()
        .map_err(|err| format!("run sha256sum: {err}"))?;
    let actual = String::from_utf8_lossy(&output.stdout)
        .split_whitespace()
        .next()
        .unwrap_or("")
        .to_ascii_lowercase();
    if !output.status.success() || actual != sha256.to_ascii_lowercase() {
        let _ = fs::remove_file(&asset);
        return Err("proxy runtime SHA-256 verification failed".to_string());
    }
    let candidate = version_dir.join(format!(".{runtime}.download"));
    if request.asset_format == "binary" {
        fs::rename(&asset, &candidate).map_err(|err| format!("stage {runtime} binary: {err}"))?;
    } else if request.asset_format == "archive" {
        ensure_command("tar")?;
        // Upstream archives do not guarantee a stable number of leading path
        // components. Extract into an isolated directory, then locate the sole
        // expected executable instead of assuming a path depth.
        run(
            Command::new("tar")
                .args(["--extract", "--gzip", "--file"])
                .arg(&asset)
                .args(["--directory"])
                .arg(&extract_dir)
                .args(["--no-same-owner", "--no-same-permissions"]),
            "extract sing-box archive",
        )?;
        let extracted_binary = find_runtime_binary(&extract_dir, runtime)?;
        fs::copy(&extracted_binary, &candidate)
            .map_err(|err| format!("stage {runtime} binary: {err}"))?;
    } else {
        let _ = fs::remove_file(&asset);
        return Err("unsupported proxy runtime asset format".to_string());
    }
    set_file_mode(&candidate, 0o755)?;
    fs::rename(&candidate, &binary).map_err(|err| format!("activate {runtime} binary: {err}"))?;
    run(
        Command::new(&binary).arg("version"),
        "verify sing-box executable",
    )?;
    let _ = fs::remove_file(&asset);
    let _ = fs::remove_dir_all(&extract_dir);
    Ok(binary)
}

#[cfg(unix)]
fn find_runtime_binary(root: &Path, runtime: &str) -> Result<PathBuf, String> {
    let mut pending = vec![root.to_path_buf()];
    let mut matches = Vec::new();
    while let Some(dir) = pending.pop() {
        for entry in
            fs::read_dir(&dir).map_err(|err| format!("read extracted runtime directory: {err}"))?
        {
            let entry = entry.map_err(|err| format!("read extracted runtime entry: {err}"))?;
            let path = entry.path();
            let kind = entry
                .file_type()
                .map_err(|err| format!("read extracted runtime type: {err}"))?;
            if kind.is_dir() {
                pending.push(path);
            } else if kind.is_file() && entry.file_name().to_string_lossy() == runtime {
                matches.push(path);
            }
        }
    }
    match matches.len() {
        1 => Ok(matches.remove(0)),
        0 => Err(format!(
            "{runtime} archive does not contain the expected executable"
        )),
        _ => Err(format!(
            "{runtime} archive contains multiple executable candidates"
        )),
    }
}

#[cfg(unix)]
fn install_unit(unit: &str, runtime: &str, binary: &Path, config: &Path) -> Result<(), String> {
    let exec = if runtime == "xray" {
        format!("{} run -config {}", binary.display(), config.display())
    } else {
        format!("{} run -c {}", binary.display(), config.display())
    };
    let content = format!("[Unit]\nDescription=API Monitor managed proxy node\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart={exec}\nRestart=on-failure\nRestartSec=3s\nNoNewPrivileges=true\nPrivateTmp=true\nProtectHome=true\nProtectSystem=strict\nReadWritePaths=/var/lib/api-monitor/proxy /etc/api-monitor/proxy\nLimitNOFILE=1048576\n\n[Install]\nWantedBy=multi-user.target\n");
    atomic_write(
        Path::new("/etc/systemd/system").join(unit).as_path(),
        content.as_bytes(),
        0o644,
    )?;
    systemctl(&["daemon-reload"])
}

#[cfg(unix)]
pub(crate) fn ensure_firewall_port(port: u16, transport: &str) -> Result<String, String> {
    let rule = format!("{port}/{transport}");
    if Command::new("firewall-cmd")
        .arg("--state")
        .output()
        .is_ok_and(|out| out.status.success())
    {
        run(
            Command::new("firewall-cmd").args(["--permanent", "--add-port", &rule]),
            "open firewalld managed proxy port",
        )?;
        run(
            Command::new("firewall-cmd").arg("--reload"),
            "reload firewalld",
        )?;
        return Ok("firewalld".to_string());
    }
    if Command::new("ufw")
        .arg("status")
        .output()
        .is_ok_and(|out| out.status.success())
    {
        run(
            Command::new("ufw").args(["allow", &rule, "comment", "API Monitor managed proxy"]),
            "open ufw managed proxy port",
        )?;
        return Ok("ufw".to_string());
    }
    // No active supported firewall means there is no rule to mutate. Report it
    // explicitly so the control plane can distinguish this from a managed rule.
    Ok("none".to_string())
}

#[cfg(unix)]
pub(crate) fn remove_firewall_port(port: u16, transport: &str) {
    let rule = format!("{port}/{transport}");
    if Command::new("firewall-cmd")
        .arg("--state")
        .output()
        .is_ok_and(|out| out.status.success())
    {
        let _ = Command::new("firewall-cmd")
            .args(["--permanent", "--remove-port", &rule])
            .status();
        let _ = Command::new("firewall-cmd").arg("--reload").status();
    } else if Command::new("ufw")
        .arg("status")
        .output()
        .is_ok_and(|out| out.status.success())
    {
        let _ = Command::new("ufw")
            .args(["--force", "delete", "allow", &rule])
            .status();
    }
}

#[cfg(unix)]
fn validate_node_id(value: &str) -> Result<(), String> {
    if value.len() < 3
        || value.len() > 80
        || !value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
    {
        Err("invalid managed proxy node_id".to_string())
    } else {
        Ok(())
    }
}
#[cfg(unix)]
fn normalize_runtime(value: &str) -> Result<&'static str, String> {
    match value.trim().to_ascii_lowercase().as_str() {
        "xray" | "sing-box" | "singbox" => Ok("sing-box"),
        _ => Err("managed proxy runtime must be sing-box".to_string()),
    }
}
#[cfg(unix)]
fn normalize_transport(value: &str) -> Result<&'static str, String> {
    match value.trim().to_ascii_lowercase().as_str() {
        "" | "tcp" => Ok("tcp"),
        "udp" => Ok("udp"),
        _ => Err("managed proxy transport must be tcp or udp".to_string()),
    }
}
#[cfg(unix)]
fn find_available_port(
    requested: u16,
    min: u16,
    max: u16,
    transport: &str,
    excluded_ports: &[u16],
) -> Result<u16, String> {
    let start = if requested >= min && requested <= max {
        requested
    } else {
        min
    };
    let span = u32::from(max) - u32::from(min) + 1;
    for offset in 0..span {
        let port = min + (((u32::from(start) - u32::from(min) + offset) % span) as u16);
        if excluded_ports.contains(&port) {
            continue;
        }
        let addr = SocketAddrV4::new(Ipv4Addr::UNSPECIFIED, port);
        let available = if transport == "udp" {
            UdpSocket::bind(addr).is_ok()
        } else {
            TcpListener::bind(addr).is_ok()
        };
        if available {
            return Ok(port);
        }
    }
    Err(format!(
        "no available {transport} port in managed range {min}-{max}"
    ))
}

#[cfg(unix)]
fn validate_supported_host() -> Result<(), String> {
    if !Path::new("/run/systemd/system").exists() {
        return Err("managed proxy deployment requires systemd".to_string());
    }
    Ok(())
}

#[cfg(unix)]
fn unit_is_active(unit: &str) -> bool {
    Command::new("systemctl")
        .args(["is-active", "--quiet", unit])
        .status()
        .is_ok_and(|status| status.success())
}

#[cfg(unix)]
fn ensure_command(name: &str) -> Result<(), String> {
    Command::new("sh")
        .args(["-c", &format!("command -v -- {name} >/dev/null 2>&1")])
        .status()
        .map_err(|err| format!("check required command {name}: {err}"))?
        .success()
        .then_some(())
        .ok_or_else(|| format!("required command is unavailable: {name}"))
}
#[cfg(unix)]
fn rewrite_runtime_bindings(config: &str, port: u16, stats_port: u16) -> Result<String, String> {
    let mut root: serde_json::Value =
        serde_json::from_str(config).map_err(|err| format!("invalid proxy config: {err}"))?;
    let inbounds = root
        .get_mut("inbounds")
        .and_then(|v| v.as_array_mut())
        .ok_or_else(|| "managed proxy config must contain an inbounds array".to_string())?;
    if inbounds.len() != 1 {
        return Err("managed proxy config must contain exactly one inbound".to_string());
    }
    let inbound = inbounds[0]
        .as_object_mut()
        .ok_or_else(|| "managed proxy inbound must be an object".to_string())?;
    let key = if inbound.contains_key("listen_port") || inbound.get("type").is_some() {
        "listen_port"
    } else {
        "port"
    };
    inbound.remove(if key == "port" { "listen_port" } else { "port" });
    inbound.insert(key.to_string(), serde_json::Value::from(port));
    if stats_port > 0 {
        let v2ray = root
            .get_mut("experimental")
            .and_then(|value| value.as_object_mut())
            .and_then(|experimental| experimental.get_mut("v2ray_api"))
            .and_then(|value| value.as_object_mut())
            .ok_or_else(|| "managed proxy statistics configuration is missing".to_string())?;
        v2ray.insert(
            "listen".to_string(),
            serde_json::Value::from(format!("127.0.0.1:{stats_port}")),
        );
    }
    serde_json::to_string(&root).map_err(|err| format!("serialize proxy config: {err}"))
}

#[cfg(unix)]
fn config_uses_v2ray_stats(config: &str) -> bool {
    serde_json::from_str::<serde_json::Value>(config)
        .ok()
        .and_then(|root| root.get("experimental").cloned())
        .and_then(|experimental| experimental.get("v2ray_api").cloned())
        .and_then(|v2ray| v2ray.get("stats").cloned())
        .and_then(|stats| stats.get("enabled").and_then(|value| value.as_bool()))
        .unwrap_or(false)
}

#[cfg(unix)]
fn reserved_stats_ports(current_node_id: &str) -> Vec<u16> {
    let mut ports = Vec::new();
    let Ok(entries) = fs::read_dir(STATE_ROOT) else {
        return ports;
    };
    for entry in entries.flatten() {
        if entry.file_name().to_string_lossy() == current_node_id {
            continue;
        }
        if let Some(state) = read_applied_state(&entry.path().join("applied.json")) {
            if state.stats_port > 0 {
                ports.push(state.stats_port);
            }
        }
    }
    ports
}
#[cfg(unix)]
fn validate_config(runtime: &str, binary: &Path, path: &Path) -> Result<(), String> {
    let mut command = Command::new(binary);
    let _ = runtime;
    command.args(["check", "-c"]);
    let output = command
        .arg(path)
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .output()
        .map_err(|err| format!("start {runtime} validation: {err}"))?;
    if output.status.success() {
        Ok(())
    } else {
        Err(String::from_utf8_lossy(&output.stderr).trim().to_string())
    }
}
#[cfg(unix)]
fn unit_name(node_id: &str) -> String {
    format!("api-monitor-proxy@{node_id}.service")
}
#[cfg(unix)]
fn systemctl(args: &[&str]) -> Result<(), String> {
    run(
        Command::new("systemctl").args(args),
        &format!("systemctl {}", args.join(" ")),
    )
}
#[cfg(unix)]
fn run(command: &mut Command, label: &str) -> Result<(), String> {
    let output = command
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .output()
        .map_err(|err| format!("{label}: {err}"))?;
    if output.status.success() {
        Ok(())
    } else {
        Err(format!(
            "{label}: {}",
            compact_command_error(&output.stderr)
        ))
    }
}

#[cfg(unix)]
fn compact_command_error(stderr: &[u8]) -> String {
    let cleaned: String = String::from_utf8_lossy(stderr)
        .chars()
        .filter(|character| !character.is_control() || *character == '\n' || *character == '\t')
        .collect();
    let message = cleaned
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty())
        .collect::<Vec<_>>()
        .join(" ");
    const MAX_CHARS: usize = 800;
    let count = message.chars().count();
    if count <= MAX_CHARS {
        return message;
    }
    let tail: String = message.chars().skip(count - MAX_CHARS).collect();
    format!("...{tail}")
}
#[cfg(unix)]
fn read_applied_state(path: &Path) -> Option<AppliedState> {
    fs::read(path)
        .ok()
        .and_then(|b| serde_json::from_slice(&b).ok())
}
#[cfg(unix)]
fn atomic_write(path: &Path, bytes: &[u8], mode: u32) -> Result<(), String> {
    let tmp = path.with_extension("tmp");
    fs::write(&tmp, bytes).map_err(|err| format!("write {}: {err}", tmp.display()))?;
    set_file_mode(&tmp, mode)?;
    fs::rename(&tmp, path).map_err(|err| format!("commit {}: {err}", path.display()))
}
#[cfg(unix)]
fn set_file_mode(path: &Path, mode: u32) -> Result<(), String> {
    fs::set_permissions(path, fs::Permissions::from_mode(mode))
        .map_err(|err| format!("set permissions on {}: {err}", path.display()))
}

#[cfg(not(unix))]
pub fn reconcile(_raw: &str) -> Result<String, String> {
    Err("proxy runtime management is supported on Linux only".to_string())
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    #[test]
    fn rewrites_xray_and_sing_box_ports() {
        let x = rewrite_runtime_bindings(
            r#"{"inbounds":[{"port":443,"protocol":"vless"}]}"#,
            45654,
            0,
        )
        .unwrap();
        let s = rewrite_runtime_bindings(
            r#"{"inbounds":[{"type":"hysteria2","listen_port":443}]}"#,
            45655,
            0,
        )
        .unwrap();
        assert_eq!(
            serde_json::from_str::<serde_json::Value>(&x).unwrap()["inbounds"][0]["port"],
            45654
        );
        assert_eq!(
            serde_json::from_str::<serde_json::Value>(&s).unwrap()["inbounds"][0]["listen_port"],
            45655
        );
    }

    #[test]
    fn rewrites_loopback_stats_port_without_exposing_it() {
        let config = rewrite_runtime_bindings(
            r#"{"inbounds":[{"type":"vless","listen_port":443}],"experimental":{"v2ray_api":{"listen":"127.0.0.1:0","stats":{"enabled":true,"users":["sub"]}}}}"#,
            45654,
            23456,
        )
        .unwrap();
        let root = serde_json::from_str::<serde_json::Value>(&config).unwrap();
        assert_eq!(root["inbounds"][0]["listen_port"], 45654);
        assert_eq!(
            root["experimental"]["v2ray_api"]["listen"],
            "127.0.0.1:23456"
        );
    }
    #[test]
    fn rejects_unsafe_node_ids() {
        assert!(validate_node_id("../../bad").is_err());
    }

    #[test]
    fn finds_binary_with_variable_archive_root_depth() {
        let root = std::env::temp_dir().join(format!(
            "api-monitor-proxy-runtime-test-{}",
            std::process::id()
        ));
        let nested = root.join("sing-box-1.13.14-linux-amd64");
        fs::create_dir_all(&nested).unwrap();
        fs::write(nested.join("sing-box"), b"test binary").unwrap();
        assert_eq!(
            find_runtime_binary(&root, "sing-box").unwrap(),
            nested.join("sing-box")
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn compacts_noisy_command_errors() {
        let noisy = format!(
            "{}curl: (22) The requested URL returned error: 404",
            "download progress\r\n".repeat(100)
        );
        let compact = compact_command_error(noisy.as_bytes());
        assert!(compact.starts_with("..."));
        assert!(compact.contains("curl: (22)"));
        assert!(compact.chars().count() <= 803);
    }
}
