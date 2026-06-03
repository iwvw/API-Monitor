use std::process::Command;
use bollard::Docker;
use bollard::container::{ListContainersOptions, CreateContainerOptions};
use serde::{Serialize, Deserialize};

use crate::collector::{DockerInfo, DockerContainer};
use crate::protocol::{DockerCheckUpdateRequest, DockerImageUpdateStatus};

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
#[allow(dead_code)]
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
    pub docker: Option<Docker>,
}

fn is_docker_installed() -> bool {
    // 1. Check if "docker" executable exists in PATH
    let cmd = if cfg!(target_os = "windows") {
        Command::new("where").arg("docker").output()
    } else {
        Command::new("which").arg("docker").output()
    };
    
    if let Ok(output) = cmd {
        if output.status.success() {
            return true;
        }
    }
    
    // 2. Check common install directories or sockets
    if cfg!(target_os = "windows") {
        std::path::Path::new("C:\\Program Files\\Docker\\Docker").exists()
    } else {
        std::path::Path::new("/var/run/docker.sock").exists() ||
        std::path::Path::new("/usr/bin/docker").exists() ||
        std::path::Path::new("/usr/local/bin/docker").exists()
    }
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
                        installed: is_docker_installed(),
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
                installed: is_docker_installed(),
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

    pub fn handle_docker_image_action(&self, data: &str) -> Result<String, String> {
        #[derive(Deserialize)]
        struct ImageActionReq {
            action: String,
            image: String,
        }
        let req: ImageActionReq = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.image.is_empty() {
            return Err("缺少镜像名称".to_string());
        }

        let mut args = vec![];
        let action_desc = match req.action.as_str() {
            "pull" => {
                args.extend(["pull", &req.image]);
                "拉取镜像"
            }
            "remove" => {
                args.extend(["rmi", "-f", &req.image]);
                "删除镜像"
            }
            "prune" => {
                args.extend(["image", "prune", "-f"]);
                "清理未使用镜像"
            }
            _ => return Err(format!("不支持的镜像操作: {}", req.action)),
        };

        let output = Command::new("docker")
            .args(&args)
            .output()
            .map_err(|e| format!("执行镜像操作失败: {}", e))?;

        if output.status.success() {
            Ok(format!("{}成功", action_desc))
        } else {
            Err(format!("{}失败: {}", action_desc, String::from_utf8_lossy(&output.stderr)))
        }
    }

    pub fn handle_docker_network_action(&self, data: &str) -> Result<String, String> {
        #[derive(Deserialize)]
        struct NetworkActionReq {
            action: String,
            name: String,
            driver: Option<String>,
            subnet: Option<String>,
            gateway: Option<String>,
            container: Option<String>,
        }
        let req: NetworkActionReq = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.name.is_empty() {
            return Err("缺少网络名称".to_string());
        }

        let mut args = vec![];
        let action_desc = match req.action.as_str() {
            "create" => {
                args.extend(["network", "create"]);
                if let Some(ref d) = req.driver {
                    args.extend(["--driver", d]);
                }
                if let Some(ref s) = req.subnet {
                    args.extend(["--subnet", s]);
                }
                if let Some(ref g) = req.gateway {
                    args.extend(["--gateway", g]);
                }
                args.push(&req.name);
                "创建网络"
            }
            "remove" => {
                args.extend(["network", "rm", &req.name]);
                "删除网络"
            }
            "connect" => {
                let container = req.container.as_ref().ok_or("缺少容器 ID")?;
                args.extend(["network", "connect", &req.name, container]);
                "连接容器到网络"
            }
            "disconnect" => {
                let container = req.container.as_ref().ok_or("缺少容器 ID")?;
                args.extend(["network", "disconnect", &req.name, container]);
                "断开容器与网络"
            }
            _ => return Err(format!("不支持的网络操作: {}", req.action)),
        };

        let output = Command::new("docker")
            .args(&args)
            .output()
            .map_err(|e| format!("执行网络操作失败: {}", e))?;

        if output.status.success() {
            Ok(format!("{}成功", action_desc))
        } else {
            Err(format!("{}失败: {}", action_desc, String::from_utf8_lossy(&output.stderr)))
        }
    }

    pub fn handle_docker_volume_action(&self, data: &str) -> Result<String, String> {
        #[derive(Deserialize)]
        struct VolumeActionReq {
            action: String,
            name: String,
            driver: Option<String>,
        }
        let req: VolumeActionReq = serde_json::from_str(data)
            .map_err(|e| format!("解析请求失败: {}", e))?;

        if req.name.is_empty() {
            return Err("缺少卷名称".to_string());
        }

        let mut args = vec![];
        let action_desc = match req.action.as_str() {
            "create" => {
                args.extend(["volume", "create"]);
                if let Some(ref d) = req.driver {
                    args.extend(["--driver", d]);
                }
                args.push(&req.name);
                "创建卷"
            }
            "remove" => {
                args.extend(["volume", "rm", "-f", &req.name]);
                "删除卷"
            }
            "prune" => {
                args.extend(["volume", "prune", "-f"]);
                "清理未使用卷"
            }
            _ => return Err(format!("不支持的卷操作: {}", req.action)),
        };

        let output = Command::new("docker")
            .args(&args)
            .output()
            .map_err(|e| format!("执行卷操作失败: {}", e))?;

        if output.status.success() {
            Ok(format!("{}成功", action_desc))
        } else {
            Err(format!("{}失败: {}", action_desc, String::from_utf8_lossy(&output.stderr)))
        }
    }

    pub async fn handle_docker_check_update(&self, req_data: &str) -> Result<String, String> {
        let req: DockerCheckUpdateRequest = if req_data.is_empty() {
            DockerCheckUpdateRequest { container_id: None }
        } else {
            serde_json::from_str(req_data)
                .map_err(|e| format!("解析请求失败: {}", e))?
        };

        let docker_client = self.docker.as_ref().ok_or_else(|| "Docker 客户端不可用".to_string())?;

        let mut containers = Vec::new();

        if let Some(ref cid) = req.container_id {
            let c = docker_client.inspect_container(cid, None)
                .await
                .map_err(|e| format!("获取容器信息失败: {}", e))?;
            
            containers.push((
                c.id.clone().unwrap_or_default(),
                c.name.clone().unwrap_or_default(),
                c.config.and_then(|cfg| cfg.image).unwrap_or_default()
            ));
        } else {
            let options = ListContainersOptions::<String> {
                all: false,
                ..Default::default()
            };
            let list = docker_client.list_containers(Some(options))
                .await
                .map_err(|e| format!("获取容器列表失败: {}", e))?;
            
            for c in list {
                let name = c.names.as_ref()
                    .and_then(|names| names.first())
                    .cloned()
                    .unwrap_or_default();
                containers.push((
                    c.id.unwrap_or_default(),
                    name,
                    c.image.unwrap_or_default()
                ));
            }
        }

        if containers.is_empty() {
            return Ok("[]".to_string());
        }

        let mut results = Vec::new();

        for (id, name, image) in containers {
            let container_name = name.trim_start_matches('/').to_string();
            let mut status = DockerImageUpdateStatus {
                container_id: id,
                container_name,
                image: image.clone(),
                current_digest: String::new(),
                latest_digest: String::new(),
                has_update: false,
                error: None,
            };

            match docker_client.inspect_image(&image).await {
                Ok(img_inspect) => {
                    if let Some(repo_digests) = img_inspect.repo_digests {
                        if let Some(digest_str) = repo_digests.first() {
                            if let Some(idx) = digest_str.find('@') {
                                status.current_digest = digest_str[idx + 1..].to_string();
                            }
                        }
                    }
                }
                Err(e) => {
                    status.error = Some(format!("获取本地镜像信息失败: {}", e));
                }
            }

            if status.error.is_none() && !image.starts_with("sha256:") {
                let (registry, repo, tag) = parse_image_name(&image);
                match get_remote_digest(&registry, &repo, &tag).await {
                    Ok(remote_digest) => {
                        status.latest_digest = remote_digest.clone();
                        status.has_update = !status.current_digest.is_empty() 
                            && !remote_digest.is_empty() 
                            && status.current_digest != remote_digest;
                    }
                    Err(e) => {
                        status.error = Some(format!("获取远程镜像信息失败: {}", e));
                    }
                }
            }

            results.push(status);
        }

        serde_json::to_string(&results)
            .map_err(|e| format!("序列化结果失败: {}", e))
    }

    pub async fn update_container_compose(
        &self,
        project: &str,
        service: &str,
        working_dir: &str,
        config_files: &str,
        _container_name: &str,
        update_progress: impl Fn(i32, &str, &str),
    ) -> Result<(), String> {
        update_progress(10, "检测到 Compose 容器，准备拉取镜像...", "");

        let mut compose_args = vec!["compose".to_string()];
        if !working_dir.is_empty() {
            compose_args.push("--project-directory".to_string());
            compose_args.push(working_dir.to_string());
        }
        if !config_files.is_empty() {
            for file in config_files.split(',') {
                if !file.is_empty() {
                    compose_args.push("-f".to_string());
                    compose_args.push(file.to_string());
                }
            }
        } else {
            compose_args.push("--project-name".to_string());
            compose_args.push(project.to_string());
        }

        let mut pull_args = compose_args.clone();
        pull_args.push("pull".to_string());
        pull_args.push(service.to_string());

        update_progress(15, "正在拉取 Compose 服务镜像...", "");
        
        let output = Command::new("docker")
            .args(&pull_args)
            .output();

        let output = match output {
            Ok(out) => out,
            Err(_) => {
                let mut pull_args_legacy = vec!["pull".to_string(), service.to_string()];
                if !working_dir.is_empty() {
                    pull_args_legacy.insert(0, working_dir.to_string());
                    pull_args_legacy.insert(0, "--project-directory".to_string());
                }
                if !config_files.is_empty() {
                    for file in config_files.split(',').rev() {
                        if !file.is_empty() {
                            pull_args_legacy.insert(0, file.to_string());
                            pull_args_legacy.insert(0, "-f".to_string());
                        }
                    }
                } else {
                    pull_args_legacy.insert(0, project.to_string());
                    pull_args_legacy.insert(0, "--project-name".to_string());
                }
                
                Command::new("docker-compose")
                    .args(&pull_args_legacy)
                    .output()
                    .map_err(|e| format!("docker-compose 启动失败: {}", e))?
            }
        };

        if !output.status.success() {
            return Err(format!("Compose 拉取镜像失败:\n{}", String::from_utf8_lossy(&output.stderr)));
        }

        update_progress(60, "拉取完成，准备重建容器...", "");

        let mut up_args = compose_args;
        up_args.push("up".to_string());
        up_args.push("-d".to_string());
        up_args.push(service.to_string());

        let up_output = Command::new("docker")
            .args(&up_args)
            .output()
            .or_else(|_| Command::new("docker-compose").args(&up_args[1..]).output())
            .map_err(|e| format!("执行 Compose up 失败: {}", e))?;

        if !up_output.status.success() {
            return Err(format!("Compose 重建容器失败: {}", String::from_utf8_lossy(&up_output.stderr)));
        }

        update_progress(100, "更新完成", "");
        Ok(())
    }

    pub async fn update_container_standalone(
        &self,
        container_id: &str,
        container_name: &str,
        new_image: &str,
        update_progress: impl Fn(i32, &str, &str),
    ) -> Result<(), String> {
        let docker_client = self.docker.as_ref().ok_or_else(|| "Docker 客户端不可用".to_string())?;

        update_progress(5, "获取容器配置...", "");
        let inspect = docker_client.inspect_container(container_id, None)
            .await
            .map_err(|e| format!("获取容器配置失败: {}", e))?;

        update_progress(10, &format!("正在拉取镜像: {}", new_image), "");
        let pull_output = Command::new("docker")
            .args(["pull", new_image])
            .output()
            .map_err(|e| format!("拉取镜像失败: {}", e))?;

        if !pull_output.status.success() {
            return Err(format!("拉取镜像失败: {}", String::from_utf8_lossy(&pull_output.stderr)));
        }

        update_progress(40, "镜像拉取完成", "");

        update_progress(50, "正在停止旧容器 (等待优雅退出)...", "");
        let stop_options = bollard::container::StopContainerOptions {
            t: 30,
        };
        docker_client.stop_container(container_id, Some(stop_options))
            .await
            .map_err(|e| format!("停止容器失败: {}", e))?;

        update_progress(60, "正在备份旧容器元数据...", "");
        let timestamp = chrono::Local::now().format("%Y%m%d-%H%M%S").to_string();
        let backup_name = format!("{}-backup-{}", container_name, timestamp);
        let rename_options = bollard::container::RenameContainerOptions {
            name: backup_name.clone(),
        };
        
        if let Err(e) = docker_client.rename_container(container_id, rename_options).await {
            let _ = docker_client.start_container::<String>(container_id, None).await;
            return Err(format!("备份容器失败: {}", e));
        }

        update_progress(75, "正在克隆配置并创建新容器...", "");
        let config = inspect.config.ok_or_else(|| "旧容器缺少配置".to_string())?;
        let mut config: bollard::container::Config<String> = config.into();
        config.image = Some(new_image.to_string());
        config.host_config = inspect.host_config;

        let networks = inspect.network_settings
            .and_then(|ns| ns.networks)
            .unwrap_or_default();

        let mut endpoints_config = std::collections::HashMap::new();
        let short_old_id = &container_id[..std::cmp::min(12, container_id.len())];

        for (net_name, mut endpoint) in networks {
            if let Some(ref mut aliases) = endpoint.aliases {
                aliases.retain(|alias| alias != short_old_id);
            }
            endpoints_config.insert(net_name, endpoint);
        }
        let networking_config = bollard::container::NetworkingConfig {
            endpoints_config,
        };

        let create_options = CreateContainerOptions {
            name: container_name.to_string(),
            ..Default::default()
        };

        let create_res = docker_client.create_container(
            Some(create_options),
            bollard::container::Config {
                networking_config: Some(networking_config),
                ..config
            }
        ).await;

        let created_id = match create_res {
            Ok(res) => res.id,
            Err(e) => {
                let _ = docker_client.rename_container(&backup_name, bollard::container::RenameContainerOptions { name: container_name.to_string() }).await;
                let _ = docker_client.start_container::<String>(container_id, None).await;
                return Err(format!("创建新容器失败: {}", e));
            }
        };

        update_progress(90, "正在启动新容器...", "");
        if let Err(e) = docker_client.start_container::<String>(&created_id, None).await {
            let _ = docker_client.remove_container(&created_id, None).await;
            let _ = docker_client.rename_container(&backup_name, bollard::container::RenameContainerOptions { name: container_name.to_string() }).await;
            let _ = docker_client.start_container::<String>(container_id, None).await;
            return Err(format!("启动新容器失败: {}", e));
        }

        update_progress(98, "正在清理旧容器备份...", "");
        let remove_options = bollard::container::RemoveContainerOptions {
            force: true,
            ..Default::default()
        };
        if let Err(e) = docker_client.remove_container(&backup_name, Some(remove_options)).await {
            println!("[Docker] 删除备份容器失败: {}", e);
        }

        update_progress(100, "更新完成", "");
        Ok(())
    }
}

fn parse_image_name(image: &str) -> (String, String, String) {
    let mut registry = "registry-1.docker.io".to_string();
    let mut tag = "latest".to_string();
    let mut image_clean = image.to_string();

    if let Some(idx) = image_clean.find('@') {
        image_clean = image_clean[..idx].to_string();
    }

    if let Some(idx) = image_clean.rfind(':') {
        let slash_idx = image_clean.rfind('/').unwrap_or(0);
        if idx > slash_idx {
            tag = image_clean[idx + 1..].to_string();
            image_clean = image_clean[..idx].to_string();
        }
    }

    let parts: Vec<&str> = image_clean.split('/').collect();
    let repo = if parts.len() == 1 {
        format!("library/{}", parts[0])
    } else if parts.len() == 2 {
        if parts[0].contains('.') || parts[0].contains(':') {
            registry = parts[0].to_string();
            parts[1].to_string()
        } else {
            image_clean.clone()
        }
    } else {
        registry = parts[0].to_string();
        parts[1..].join("/")
    };

    (registry, repo, tag)
}

async fn get_remote_digest(registry: &str, repo: &str, tag: &str) -> Result<String, String> {
    if registry != "registry-1.docker.io" && registry != "docker.io" {
        return try_get_digest_from_host(registry, repo, tag).await;
    }

    let accelerators = vec![
        "registry-1.docker.io",
        "docker.m.daocloud.io",
        "docker.1panel.live",
        "hub.rat.dev",
    ];

    let mut last_err = String::new();
    for host in accelerators {
        match try_get_digest_from_host(host, repo, tag).await {
            Ok(digest) => return Ok(digest),
            Err(e) => {
                last_err = format!("{}: {}", host, e);
                println!("[Docker] 尝试 {} 失败: {}, 切换下一个", host, e);
            }
        }
    }

    Err(format!("所有镜像源均失败: {}", last_err))
}

async fn try_get_digest_from_host(host: &str, repo: &str, tag: &str) -> Result<String, String> {
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(15))
        .danger_accept_invalid_certs(true)
        .build()
        .map_err(|e| format!("构建 HTTP 客户端失败: {}", e))?;

    let challenge_url = format!("https://{}/v2/", host);
    let challenge_resp = client.get(&challenge_url)
        .send()
        .await
        .map_err(|e| format!("challenge 请求失败: {}", e))?;

    let headers = challenge_resp.headers();
    let www_auth = headers.get("www-authenticate")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");

    let mut token = String::new();
    if www_auth.to_lowercase().starts_with("bearer") {
        token = get_bearer_token(www_auth, repo, &client).await?;
    }

    let manifest_url = format!("https://{}/v2/{}/manifests/{}", host, repo, tag);
    let mut req = client.head(&manifest_url);
    if !token.is_empty() {
        req = req.bearer_auth(&token);
    }
    
    let resp = req
        .header("Accept", "application/vnd.docker.distribution.manifest.v2+json")
        .header("Accept", "application/vnd.docker.distribution.manifest.list.v2+json")
        .header("Accept", "application/vnd.docker.distribution.manifest.v1+json")
        .header("Accept", "application/vnd.oci.image.index.v1+json")
        .send()
        .await
        .map_err(|e| format!("manifest 请求失败: {}", e))?;

    if !resp.status().is_success() {
        return Err(format!("registry 返回状态码: {}", resp.status()));
    }

    let digest = resp.headers()
        .get("docker-content-digest")
        .and_then(|v| v.to_str().ok())
        .map(|s| s.to_string())
        .ok_or_else(|| "响应中未包含 docker-content-digest".to_string())?;

    Ok(digest)
}

async fn get_bearer_token(www_auth: &str, repo: &str, client: &reqwest::Client) -> Result<String, String> {
    let raw = if www_auth.to_lowercase().starts_with("bearer ") {
        &www_auth[7..]
    } else {
        www_auth
    };

    let mut params = std::collections::HashMap::new();
    for part in raw.split(',') {
        let part = part.trim();
        if let Some(idx) = part.find('=') {
            let key = part[..idx].trim().to_lowercase();
            let val = part[idx + 1..].trim().trim_matches('"').to_string();
            params.insert(key, val);
        }
    }

    let realm = params.get("realm").ok_or_else(|| format!("无法解析 realm from: {}", www_auth))?;
    let service = params.get("service").map(|s| s.as_str()).unwrap_or("");

    let token_url = format!("{}?service={}&scope=repository:{}:pull", realm, service, repo);
    
    let resp = client.get(&token_url)
        .send()
        .await
        .map_err(|e| format!("token 请求失败: {}", e))?;

    if !resp.status().is_success() {
        return Err(format!("token 请求返回 {}", resp.status()));
    }

    #[derive(Deserialize)]
    struct TokenResp {
        token: Option<String>,
        access_token: Option<String>,
    }

    let token_resp: TokenResp = resp.json()
        .await
        .map_err(|e| format!("解析 token 响应失败: {}", e))?;

    if let Some(token) = token_resp.token {
        Ok(token)
    } else if let Some(token) = token_resp.access_token {
        Ok(token)
    } else {
        Err("响应中未包含 token 或 access_token".to_string())
    }
}
