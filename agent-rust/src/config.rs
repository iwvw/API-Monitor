use clap::Parser;
use serde::Deserialize;
use std::fs::File;
use std::io::Read;
use std::path::PathBuf;

#[derive(Parser, Debug)]
#[command(author, version, about, long_about = None)]
pub struct CliArgs {
    #[arg(help = "Action to perform (install, uninstall, svc-install, svc-uninstall, sample)")]
    pub action: Option<String>,

    #[arg(short = 's', long = "server", help = "Dashboard server URL")]
    pub server_url: Option<String>,

    #[arg(long = "id", help = "Server ID")]
    pub server_id: Option<String>,

    #[arg(short = 'k', long = "key", help = "Agent key")]
    pub agent_key: Option<String>,

    #[arg(
        short = 'i',
        long = "interval",
        help = "Report interval in milliseconds"
    )]
    pub report_interval: Option<u64>,

    #[arg(short = 'd', long = "debug", help = "Enable debug logging")]
    pub debug: bool,

    #[arg(
        short = 'b',
        long = "daemon",
        help = "Run in background daemon mode (Windows)"
    )]
    pub daemon: bool,
}

#[derive(Deserialize, Debug, Clone, Default)]
#[serde(rename_all = "camelCase")]
pub struct JsonConfig {
    pub server_url: Option<String>,
    pub server_id: Option<String>,
    pub agent_key: Option<String>,
    pub report_interval: Option<u64>,
    pub debug: Option<bool>,
}

#[derive(Debug, Clone)]
pub struct Config {
    pub server_url: String,
    pub server_id: String,
    pub agent_key: String,
    pub report_interval: u64,    // ms
    pub host_info_interval: u64, // ms
    pub reconnect_delay: u64,    // ms
    pub debug: bool,
}

impl Config {
    pub fn load(cli: &CliArgs) -> Result<Self, String> {
        // 2. Try loading config.json
        let json_config = Self::load_config_json().unwrap_or_default();

        // Resolve fields based on precedence: CLI > Env > JsonConfig > Defaults
        let server_url = cli
            .server_url
            .clone()
            .or_else(|| std::env::var("API_MONITOR_SERVER").ok())
            .or(json_config.server_url)
            .unwrap_or_else(|| "http://localhost:3000".to_string());

        let server_id = cli
            .server_id
            .clone()
            .or_else(|| std::env::var("API_MONITOR_SERVER_ID").ok())
            .or(json_config.server_id);

        let agent_key = cli
            .agent_key
            .clone()
            .or_else(|| std::env::var("API_MONITOR_KEY").ok())
            .or(json_config.agent_key);

        let report_interval = cli
            .report_interval
            .or(json_config.report_interval)
            .unwrap_or(1500);

        let debug = cli.debug || json_config.debug.unwrap_or(false);

        // Required checks
        let server_id = server_id.ok_or_else(|| {
            "Error: Server ID is required. Provide it via --id, API_MONITOR_SERVER_ID, or config.json".to_string()
        })?;

        let agent_key = agent_key.ok_or_else(|| {
            "Error: Agent Key is required. Provide it via -k, API_MONITOR_KEY, or config.json"
                .to_string()
        })?;

        Ok(Config {
            server_url,
            server_id,
            agent_key,
            report_interval,
            host_info_interval: 600_000, // 10 minutes
            reconnect_delay: 5000,       // 5 seconds
            debug,
        })
    }

    fn load_config_json() -> Option<JsonConfig> {
        let mut paths = vec![
            PathBuf::from("config.json"),
            PathBuf::from("../config.json"),
        ];
        if let Ok(exe_path) = std::env::current_exe() {
            if let Some(exe_dir) = exe_path.parent() {
                paths.insert(0, exe_dir.join("config.json"));
            }
        }
        for path in paths {
            if path.exists() {
                if let Ok(mut file) = File::open(&path) {
                    let mut contents = String::new();
                    if file.read_to_string(&mut contents).is_ok() {
                        if let Ok(config) = serde_json::from_str::<JsonConfig>(&contents) {
                            return Some(config);
                        }
                    }
                }
            }
        }
        None
    }
}
