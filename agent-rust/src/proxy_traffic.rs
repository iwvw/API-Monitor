#[cfg(unix)]
use crate::config::Config;
#[cfg(unix)]
use prost::Message;
#[cfg(unix)]
use serde::{Deserialize, Serialize};
#[cfg(unix)]
use std::collections::{BTreeMap, BTreeSet};
#[cfg(unix)]
use std::fs;
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
#[cfg(unix)]
use std::path::{Path, PathBuf};
#[cfg(unix)]
use std::time::Duration;
#[cfg(unix)]
use tonic::codegen::http::uri::PathAndQuery;
#[cfg(unix)]
use tonic::transport::Channel;
#[cfg(unix)]

const STATE_ROOT: &str = "/var/lib/api-monitor/proxy/nodes";
#[cfg(unix)]
const TRAFFIC_STATE_PATH: &str = "/var/lib/api-monitor/proxy/traffic-state.json";
#[cfg(unix)]
const REPORT_INTERVAL: Duration = Duration::from_secs(15 * 60);

#[cfg(unix)]
#[derive(Clone, PartialEq, Message)]
struct QueryStatsRequest {
    #[prost(string, tag = "1")]
    pattern: String,
    #[prost(bool, tag = "2")]
    reset: bool,
    #[prost(string, repeated, tag = "3")]
    patterns: Vec<String>,
    #[prost(bool, tag = "4")]
    regexp: bool,
}

#[cfg(unix)]
#[derive(Clone, PartialEq, Message)]
struct Stat {
    #[prost(string, tag = "1")]
    name: String,
    #[prost(int64, tag = "2")]
    value: i64,
}

#[cfg(unix)]
#[derive(Clone, PartialEq, Message)]
struct QueryStatsResponse {
    #[prost(message, repeated, tag = "1")]
    stat: Vec<Stat>,
}

#[cfg(unix)]
#[derive(Debug, Clone, Serialize, Deserialize)]
struct UsageReport {
    node_id: String,
    credential_id: String,
    upload_bytes: i64,
    download_bytes: i64,
}

#[cfg(unix)]
#[derive(Debug, Clone, Serialize)]
struct UsageBatch<'a> {
    boot_id: &'a str,
    sequence: u64,
    reports: &'a [UsageReport],
}

#[cfg(unix)]
#[derive(Debug, Default, Serialize, Deserialize)]
struct TrafficState {
    boot_id: String,
    sequence: u64,
    #[serde(default)]
    baselines: BTreeMap<String, i64>,
    #[serde(default)]
    pending: Vec<UsageReport>,
}

#[cfg(unix)]
#[derive(Debug, Deserialize)]
struct AppliedNodeState {
    #[serde(default)]
    stats_port: u16,
}

#[cfg(unix)]
pub async fn run(config: Config) {
    let client = match reqwest::Client::builder()
        .connect_timeout(Duration::from_secs(10))
        .timeout(Duration::from_secs(30))
        .build()
    {
        Ok(client) => client,
        Err(error) => {
            eprintln!("[Agent] create proxy traffic client failed: {error}");
            return;
        }
    };
    let mut timer = tokio::time::interval(REPORT_INTERVAL);
    timer.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    timer.tick().await;
    loop {
        if let Err(error) = collect_once(&config, &client).await {
            if config.debug {
                eprintln!("[Agent] proxy subscriber traffic: {error}");
            }
        }
        timer.tick().await;
    }
}

#[cfg(unix)]
async fn collect_once(config: &Config, client: &reqwest::Client) -> Result<(), String> {
    let current_boot_id = read_boot_id();
    let mut state = load_state();
    if state.boot_id.is_empty() {
        state.boot_id = current_boot_id.clone();
    }
    if !state.pending.is_empty() {
        send_pending(config, client, &mut state).await?;
        if state.boot_id != current_boot_id {
            state.boot_id = current_boot_id.clone();
            state.sequence = 0;
            state.baselines.clear();
            persist_state(&state)?;
        }
    } else if state.boot_id != current_boot_id {
        state.boot_id = current_boot_id;
        state.sequence = 0;
        state.baselines.clear();
    }

    let nodes = discover_stats_nodes();
    let active_nodes = nodes
        .iter()
        .map(|(node_id, _)| node_id.clone())
        .collect::<BTreeSet<_>>();
    state.baselines.retain(|key, _| {
        key.split_once('\u{1f}')
            .is_some_and(|(node_id, _)| active_nodes.contains(node_id))
    });
    let mut usage: BTreeMap<(String, String), (i64, i64)> = BTreeMap::new();
    let mut failures = Vec::new();
    for (node_id, port) in nodes {
        let stats = match query_stats(port).await {
            Ok(stats) => stats,
            Err(error) => {
                failures.push(format!("{node_id}: {error}"));
                continue;
            }
        };
        for stat in stats {
            let Some((user, direction)) = parse_user_stat(&stat.name) else {
                continue;
            };
            let key = format!("{node_id}\u{1f}{user}\u{1f}{direction}");
            let current = stat.value.max(0);
            let previous = state.baselines.get(&key).copied().unwrap_or(0);
            let delta = counter_delta(current, previous);
            state.baselines.insert(key, current);
            let entry = usage.entry((node_id.clone(), user)).or_default();
            if direction == "uplink" {
                entry.0 = entry.0.saturating_add(delta);
            } else {
                entry.1 = entry.1.saturating_add(delta);
            }
        }
    }
    state.pending = usage
        .into_iter()
        .filter(|(_, (upload, download))| *upload > 0 || *download > 0)
        .map(
            |((node_id, credential_id), (upload_bytes, download_bytes))| UsageReport {
                node_id,
                credential_id,
                upload_bytes,
                download_bytes,
            },
        )
        .collect();
    persist_state(&state)?;
    if !state.pending.is_empty() {
        send_pending(config, client, &mut state).await?;
    }
    if failures.is_empty() {
        Ok(())
    } else {
        Err(format!(
            "{} stats endpoint(s) unavailable: {}",
            failures.len(),
            failures.join("; ")
        ))
    }
}

#[cfg(unix)]
fn counter_delta(current: i64, previous: i64) -> i64 {
    let previous = previous.max(0);
    if current >= previous {
        current - previous
    } else {
        current
    }
}

#[cfg(unix)]
async fn send_pending(
    config: &Config,
    client: &reqwest::Client,
    state: &mut TrafficState,
) -> Result<(), String> {
    let url = format!(
        "{}/api/server/agent/proxy/{}/traffic",
        config.server_url.trim_end_matches('/'),
        config.server_id
    );
    let response = client
        .post(url)
        .bearer_auth(&config.agent_key)
        .json(&UsageBatch {
            boot_id: &state.boot_id,
            sequence: state.sequence,
            reports: &state.pending,
        })
        .send()
        .await
        .map_err(|error| format!("report subscriber traffic: {error}"))?;
    if !response.status().is_success() {
        let status = response.status();
        let body = response.text().await.unwrap_or_default();
        return Err(format!(
            "report subscriber traffic returned {status}: {body}"
        ));
    }
    state.pending.clear();
    state.sequence = state.sequence.wrapping_add(1);
    persist_state(state)
}

#[cfg(unix)]
async fn query_stats(port: u16) -> Result<Vec<Stat>, String> {
    let channel = Channel::from_shared(format!("http://127.0.0.1:{port}"))
        .map_err(|error| format!("invalid stats endpoint: {error}"))?
        .connect_timeout(Duration::from_secs(3))
        .timeout(Duration::from_secs(5))
        .connect()
        .await
        .map_err(|error| format!("connect sing-box stats on port {port}: {error}"))?;
    let mut grpc = tonic::client::Grpc::new(channel);
    grpc.ready()
        .await
        .map_err(|error| format!("sing-box stats unavailable: {error}"))?;
    let path = PathAndQuery::from_static("/v2ray.core.app.stats.command.StatsService/QueryStats");
    let request = tonic::Request::new(QueryStatsRequest {
        pattern: String::new(),
        reset: false,
        patterns: vec!["user>>>".to_string()],
        regexp: false,
    });
    let response: tonic::Response<QueryStatsResponse> = grpc
        .unary(request, path, tonic::codec::ProstCodec::default())
        .await
        .map_err(|error| format!("query sing-box stats: {error}"))?;
    Ok(response.into_inner().stat)
}

#[cfg(unix)]
fn discover_stats_nodes() -> Vec<(String, u16)> {
    let mut nodes = Vec::new();
    let Ok(entries) = fs::read_dir(STATE_ROOT) else {
        return nodes;
    };
    for entry in entries.flatten() {
        let node_id = entry.file_name().to_string_lossy().to_string();
        let path = entry.path().join("applied.json");
        let Ok(raw) = fs::read_to_string(path) else {
            continue;
        };
        let Ok(state) = serde_json::from_str::<AppliedNodeState>(&raw) else {
            continue;
        };
        if state.stats_port > 0 {
            nodes.push((node_id, state.stats_port));
        }
    }
    nodes.sort();
    nodes
}

#[cfg(unix)]
fn parse_user_stat(name: &str) -> Option<(String, &'static str)> {
    let rest = name.strip_prefix("user>>>")?;
    let (user, suffix) = rest.split_once(">>>traffic>>>")?;
    if user.is_empty() {
        return None;
    }
    match suffix {
        "uplink" => Some((user.to_string(), "uplink")),
        "downlink" => Some((user.to_string(), "downlink")),
        _ => None,
    }
}

#[cfg(unix)]
fn read_boot_id() -> String {
    fs::read_to_string("/proc/sys/kernel/random/boot_id")
        .unwrap_or_else(|_| "unknown-boot".to_string())
        .trim()
        .to_string()
}

#[cfg(unix)]
fn load_state() -> TrafficState {
    fs::read_to_string(TRAFFIC_STATE_PATH)
        .ok()
        .and_then(|raw| serde_json::from_str(&raw).ok())
        .unwrap_or_default()
}

#[cfg(unix)]
fn persist_state(state: &TrafficState) -> Result<(), String> {
    let path = PathBuf::from(TRAFFIC_STATE_PATH);
    let parent = path
        .parent()
        .unwrap_or(Path::new("/var/lib/api-monitor/proxy"));
    fs::create_dir_all(parent)
        .map_err(|error| format!("create traffic state directory: {error}"))?;
    fs::set_permissions(parent, fs::Permissions::from_mode(0o700))
        .map_err(|error| format!("protect traffic state directory: {error}"))?;
    let temporary = path.with_extension("json.tmp");
    let raw = serde_json::to_vec_pretty(state)
        .map_err(|error| format!("serialize traffic state: {error}"))?;
    fs::write(&temporary, raw).map_err(|error| format!("write traffic state: {error}"))?;
    fs::set_permissions(&temporary, fs::Permissions::from_mode(0o600))
        .map_err(|error| format!("protect traffic state: {error}"))?;
    fs::rename(&temporary, &path).map_err(|error| format!("activate traffic state: {error}"))
}

#[cfg(all(test, unix))]
mod tests {
    use super::{counter_delta, parse_user_stat, query_stats};
    use std::fs;
    use std::net::TcpListener;
    use std::process::{Command, Stdio};
    use std::time::{Duration, SystemTime, UNIX_EPOCH};

    #[test]
    fn parses_only_user_traffic_counters() {
        assert_eq!(
            parse_user_stat("user>>>sub-1>>>traffic>>>uplink"),
            Some(("sub-1".to_string(), "uplink"))
        );
        assert_eq!(
            parse_user_stat("user>>>sub-1>>>traffic>>>downlink"),
            Some(("sub-1".to_string(), "downlink"))
        );
        assert_eq!(parse_user_stat("inbound>>>node>>>traffic>>>uplink"), None);
    }

    #[test]
    fn counter_delta_handles_runtime_restart_without_going_negative() {
        assert_eq!(counter_delta(150, 100), 50);
        assert_eq!(counter_delta(20, 100), 20);
        assert_eq!(counter_delta(0, 0), 0);
        assert_eq!(counter_delta(20, -1), 20);
    }

    #[tokio::test]
    #[ignore = "requires API_MONITOR_SING_BOX_BIN with the managed with_v2ray_api build"]
    async fn queries_managed_sing_box_stats_endpoint() {
        let binary = std::env::var("API_MONITOR_SING_BOX_BIN")
            .expect("API_MONITOR_SING_BOX_BIN must point to the managed sing-box binary");
        let inbound_port = TcpListener::bind("127.0.0.1:0")
            .unwrap()
            .local_addr()
            .unwrap()
            .port();
        let stats_port = TcpListener::bind("127.0.0.1:0")
            .unwrap()
            .local_addr()
            .unwrap()
            .port();
        let suffix = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let config_path = std::env::temp_dir().join(format!("api-monitor-stats-{suffix}.json"));
        let config = serde_json::json!({
            "log": {"level": "error"},
            "inbounds": [{
                "type": "vless", "tag": "managed-vless", "listen": "127.0.0.1",
                "listen_port": inbound_port,
                "users": [{"name": "sub-1", "uuid": "11111111-1111-4111-8111-111111111111"}]
            }],
            "outbounds": [{"type": "direct", "tag": "direct"}],
            "experimental": {"v2ray_api": {
                "listen": format!("127.0.0.1:{stats_port}"),
                "stats": {"enabled": true, "users": ["sub-1"]}
            }}
        });
        fs::write(&config_path, serde_json::to_vec(&config).unwrap()).unwrap();
        let mut child = Command::new(binary)
            .args(["run", "-c"])
            .arg(&config_path)
            .stdout(Stdio::null())
            .stderr(Stdio::inherit())
            .spawn()
            .unwrap();
        let mut result = None;
        for _ in 0..30 {
            match query_stats(stats_port).await {
                Ok(stats) => {
                    result = Some(stats);
                    break;
                }
                Err(_) => tokio::time::sleep(Duration::from_millis(100)).await,
            }
        }
        let _ = child.kill();
        let _ = child.wait();
        let _ = fs::remove_file(config_path);
        assert!(
            result.is_some(),
            "managed stats endpoint did not become ready"
        );
    }
}
