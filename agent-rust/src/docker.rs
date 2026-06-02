use std::process::Command;
use bollard::Docker;
use bollard::container::ListContainersOptions;
use serde::{Serialize, Deserialize};

use crate::collector::{DockerInfo, DockerContainer};

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct DockerActionRequest {
    pub action: String,
    pub container_id: String,
    pub image: Option<String>,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct DockerLogsRequest {
    pub container_id: String,
    pub tail: Option<i32>,
    pub since: Option<String>,
}

pub struct DockerBridge {
    docker: Option<Docker>,
}

impl DockerBridge {
    pub fn new() -> Self {
        let docker = Docker::connect_with_local_defaults().ok();
        DockerBridge { docker }
    }

    pub async fn collect_docker_info(&mut self) -> DockerInfo {
        let docker_client = match &self.docker {
            Some(d) => d,
            None => {
                // Try to reconnect
                if let Ok(d) = Docker::connect_with_local_defaults() {
                    self.docker = Some(d);
                    self.docker.as_ref().unwrap()
                } else {
                    return DockerInfo {
                        installed: false,
                        running: 0,
                        stopped: 0,
                        containers: Vec::new(),
                    };
                }
            }
        };

        // Ping test
        if docker_client.ping().await.is_err() {
            return DockerInfo {
                installed: true, // Docker installed but not running
                running: 0,
                stopped: 0,
                containers: Vec::new(),
            };
        }

        let options = ListContainersOptions::<String> {
            all: true,
            ..Default::default()
        };

        let mut running = 0;
        let mut stopped = 0;
        let mut containers = Vec::new();

        if let Ok(list) = docker_client.list_containers(Some(options)).await {
            for c in list {
                let name = c.names.as_ref()
                    .and_then(|names| names.first())
                    .map(|n| n.trim_start_matches('/').to_string())
                    .unwrap_or_default();

                let status = c.status.clone().unwrap_or_default();
                let state = c.state.clone().unwrap_or_default();
                if state == "running" {
                    running += 1;
                } else {
                    stopped += 1;
                }

                // Port format
                let mut ports_vec = Vec::new();
                if let Some(ports) = c.ports {
                    for p in ports {
                        if let Some(pub_port) = p.public_port {
                            ports_vec.push(format!("{}:{}", pub_port, p.private_port));
                        }
                    }
                }

                let created_str = c.created.map(|ts| {
                    let dt = std::time::SystemTime::UNIX_EPOCH + std::time::Duration::from_secs(ts as u64);
                    let datetime: chrono::DateTime<chrono::Utc> = dt.into();
                    datetime.format("%Y-%m-%d %H:%M:%S").to_string()
                }).unwrap_or_default();

                let id_short = c.id.as_ref()
                    .map(|id| id.chars().take(12).collect::<String>())
                    .unwrap_or_default();

                containers.push(DockerContainer {
                    id: id_short,
                    name,
                    image: c.image.clone().unwrap_or_default(),
                    status,
                    created: created_str,
                    ports: ports_vec.join(", "),
                });
            }
        }

        DockerInfo {
            installed: true,
            running,
            stopped,
            containers,
        }
    }

    pub fn handle_docker_action(&self, req_data: &str) -> Result<String, String> {
        let req: DockerActionRequest = serde_json::from_str(req_data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.container_id.is_empty() {
            return Err("缺少容器 ID".to_string());
        }

        let mut args = vec![];
        let action_desc = match req.action.as_str() {
            "start" => {
                args.push("start");
                "启动"
            }
            "stop" => {
                args.push("stop");
                "停止"
            }
            "restart" => {
                args.push("restart");
                "重启"
            }
            "pause" => {
                args.push("pause");
                "暂停"
            }
            "unpause" => {
                args.push("unpause");
                "恢复"
            }
            "rm" | "delete" => {
                args.push("rm");
                "删除"
            }
            _ => return Err(format!("不支持的操作: {}", req.action)),
        };

        args.push(&req.container_id);

        let output = Command::new("docker")
            .args(&args)
            .output()
            .map_err(|e| format!("执行命令失败: {}", e))?;

        if output.status.success() {
            Ok(format!("容器{}成功", action_desc))
        } else {
            Err(format!("容器{}失败: {}", action_desc, String::from_utf8_lossy(&output.stderr)))
        }
    }

    pub fn handle_docker_logs(&self, req_data: &str) -> Result<String, String> {
        let req: DockerLogsRequest = serde_json::from_str(req_data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.container_id.is_empty() {
            return Err("缺少容器 ID".to_string());
        }

        let mut args = vec!["logs".to_string()];
        
        let tail_str = req.tail.unwrap_or(100).to_string();
        args.push("--tail".to_string());
        args.push(tail_str);

        if let Some(since) = req.since {
            if !since.is_empty() {
                args.push("--since".to_string());
                args.push(since);
            }
        }

        args.push(req.container_id);

        let output = Command::new("docker")
            .args(&args)
            .output()
            .map_err(|e| format!("获取日志失败: {}", e))?;

        Ok(String::from_utf8_lossy(&output.stdout).to_string() + &String::from_utf8_lossy(&output.stderr))
    }

    pub fn handle_docker_stats(&self, _data: &str) -> Result<String, String> {
        let output = Command::new("docker")
            .args(["stats", "--no-stream", "--format", "{{.ID}}|{{.Name}}|{{.CPUPerc}}|{{.MemUsage}}|{{.MemPerc}}|{{.NetIO}}|{{.BlockIO}}"])
            .output()
            .map_err(|e| format!("获取资源统计失败: {}", e))?;

        if !output.status.success() {
            return Err(String::from_utf8_lossy(&output.stderr).to_string());
        }

        #[derive(Serialize)]
        struct DockerContainerStats {
            container_id: String,
            name: String,
            cpu_percent: String,
            mem_usage: String,
            mem_percent: String,
            net_io: String,
            block_io: String,
        }

        let text = String::from_utf8_lossy(&output.stdout);
        let mut stats = Vec::new();
        for line in text.lines() {
            let parts: Vec<&str> = line.split('|').collect();
            if parts.len() >= 7 {
                stats.push(DockerContainerStats {
                    container_id: parts[0].to_string(),
                    name: parts[1].to_string(),
                    cpu_percent: parts[2].to_string(),
                    mem_usage: parts[3].to_string(),
                    mem_percent: parts[4].to_string(),
                    net_io: parts[5].to_string(),
                    block_io: parts[6].to_string(),
                });
            }
        }

        serde_json::to_string(&stats).map_err(|e| format!("序列化结果失败: {}", e))
    }

    pub fn handle_docker_images(&self, _data: &str) -> Result<String, String> {
        let output = Command::new("docker")
            .args(["images", "--format", "{{.Repository}}|{{.Tag}}|{{.ID}}|{{.CreatedAt}}|{{.Size}}"])
            .output()
            .map_err(|e| format!("获取镜像列表失败: {}", e))?;

        #[derive(Serialize)]
        struct DockerImageInfo {
            repository: String,
            tag: String,
            id: String,
            created: String,
            size: String,
        }

        let text = String::from_utf8_lossy(&output.stdout);
        let mut images = Vec::new();
        for line in text.lines() {
            let parts: Vec<&str> = line.split('|').collect();
            if parts.len() >= 5 {
                images.push(DockerImageInfo {
                    repository: parts[0].to_string(),
                    tag: parts[1].to_string(),
                    id: parts[2].to_string(),
                    created: parts[3].to_string(),
                    size: parts[4].to_string(),
                });
            }
        }

        serde_json::to_string(&images).map_err(|e| format!("序列化结果失败: {}", e))
    }

    pub fn handle_docker_networks(&self, _data: &str) -> Result<String, String> {
        let output = Command::new("docker")
            .args(["network", "ls", "--format", "{{.ID}}|{{.Name}}|{{.Driver}}|{{.Scope}}"])
            .output()
            .map_err(|e| format!("获取网络列表失败: {}", e))?;

        #[derive(Serialize)]
        struct DockerNetworkInfo {
            id: String,
            name: String,
            driver: String,
            scope: String,
        }

        let text = String::from_utf8_lossy(&output.stdout);
        let mut networks = Vec::new();
        for line in text.lines() {
            let parts: Vec<&str> = line.split('|').collect();
            if parts.len() >= 4 {
                networks.push(DockerNetworkInfo {
                    id: parts[0].to_string(),
                    name: parts[1].to_string(),
                    driver: parts[2].to_string(),
                    scope: parts[3].to_string(),
                });
            }
        }

        serde_json::to_string(&networks).map_err(|e| format!("序列化结果失败: {}", e))
    }

    pub fn handle_docker_volumes(&self, _data: &str) -> Result<String, String> {
        let output = Command::new("docker")
            .args(["volume", "ls", "--format", "{{.Name}}|{{.Driver}}|{{.Scope}}"])
            .output()
            .map_err(|e| format!("获取卷列表失败: {}", e))?;

        #[derive(Serialize)]
        struct DockerVolumeInfo {
            name: String,
            driver: String,
            scope: String,
        }

        let text = String::from_utf8_lossy(&output.stdout);
        let mut volumes = Vec::new();
        for line in text.lines() {
            let parts: Vec<&str> = line.split('|').collect();
            if parts.len() >= 3 {
                volumes.push(DockerVolumeInfo {
                    name: parts[0].to_string(),
                    driver: parts[1].to_string(),
                    scope: parts[2].to_string(),
                });
            }
        }

        serde_json::to_string(&volumes).map_err(|e| format!("序列化结果失败: {}", e))
    }

    pub fn handle_docker_compose_list(&self, _data: &str) -> Result<String, String> {
        let output = Command::new("docker")
            .args(["compose", "ls", "--format", "json"])
            .output()
            .or_else(|_| Command::new("docker-compose").args(["ls", "--format", "json"]).output())
            .map_err(|e| format!("获取 Compose 项目失败: {}", e))?;

        Ok(String::from_utf8_lossy(&output.stdout).to_string())
    }

    pub fn handle_docker_compose_action(&self, data: &str) -> Result<String, String> {
        // Parse: config_file, action
        #[derive(Deserialize)]
        struct ComposeActionReq {
            #[serde(rename = "config_file")]
            config_file: String,
            action: String,
        }

        let req: ComposeActionReq = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        let mut args = vec![];
        if !req.config_file.is_empty() {
            args.push("-f");
            args.push(&req.config_file);
        }

        match req.action.as_str() {
            "up" => args.extend(["up", "-d"]),
            "down" => args.push("down"),
            "stop" => args.push("stop"),
            "start" => args.push("start"),
            "restart" => args.push("restart"),
            _ => return Err(format!("不支持的 Compose 操作: {}", req.action)),
        }

        let output = Command::new("docker")
            .arg("compose")
            .args(&args)
            .output()
            .or_else(|_| Command::new("docker-compose").args(&args).output())
            .map_err(|e| format!("执行 Compose 动作失败: {}", e))?;

        if output.status.success() {
            Ok(format!("Compose 操作 {} 成功", req.action))
        } else {
            Err(String::from_utf8_lossy(&output.stderr).to_string())
        }
    }
}
