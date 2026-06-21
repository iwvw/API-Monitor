use bollard::container::{
    Config as ContainerConfig, CreateContainerOptions, ListContainersOptions, LogsOptions,
    RemoveContainerOptions, RenameContainerOptions, RestartContainerOptions, StartContainerOptions,
    Stats, StatsOptions, StopContainerOptions,
};
use bollard::image::{
    CreateImageOptions, ListImagesOptions, PruneImagesOptions, RemoveImageOptions,
};
use bollard::models::{
    EndpointSettings, HostConfig, Ipam, IpamConfig, PortBinding, PortMap, RestartPolicy,
    RestartPolicyNameEnum,
};
use bollard::network::{
    ConnectNetworkOptions, CreateNetworkOptions, DisconnectNetworkOptions, ListNetworksOptions,
    PruneNetworksOptions,
};
use bollard::volume::{
    CreateVolumeOptions, ListVolumesOptions, PruneVolumesOptions, RemoveVolumeOptions,
};
use bollard::Docker;
use futures_util::stream::{self, StreamExt};
use serde::{Deserialize, Serialize};
use std::collections::{HashMap, HashSet};
use std::process::Command;
use std::time::{Duration, Instant};

use crate::collector::{DockerContainer, DockerInfo};
use crate::protocol::{DockerCheckUpdateRequest, DockerImageUpdateStatus};

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
#[allow(dead_code)]
pub struct DockerActionRequest {
    pub action: String,
    #[serde(alias = "container_id")]
    pub container_id: String,
    pub image: Option<String>,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct DockerLogsRequest {
    #[serde(alias = "container_id")]
    pub container_id: String,
    pub tail: Option<i32>,
    pub since: Option<String>,
}

pub struct DockerBridge {
    pub docker: Option<Docker>,
    remote_digest_cache: HashMap<String, RemoteDigestCacheEntry>,
}

#[derive(Deserialize)]
struct DockerCreateContainerRequest {
    image: String,
    name: Option<String>,
    ports: Option<Vec<String>>,
    volumes: Option<Vec<String>>,
    env: Option<HashMap<String, String>>,
    network: Option<String>,
    restart: Option<String>,
    privileged: Option<bool>,
    #[serde(rename = "extraArgs", alias = "extra_args")]
    extra_args: Option<Vec<String>>,
}

#[derive(Deserialize)]
struct DockerRenameContainerRequest {
    #[serde(rename = "containerId", alias = "container_id")]
    container_id: String,
    #[serde(rename = "newName", alias = "new_name")]
    new_name: String,
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

struct RemoteDigestCacheEntry {
    digest: String,
    expires_at: Instant,
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
        std::path::Path::new("/var/run/docker.sock").exists()
            || std::path::Path::new("/usr/bin/docker").exists()
            || std::path::Path::new("/usr/local/bin/docker").exists()
    }
}

impl DockerBridge {
    pub fn new() -> Self {
        let docker = Docker::connect_with_local_defaults().ok();
        DockerBridge {
            docker,
            remote_digest_cache: HashMap::new(),
        }
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
                let name = c
                    .names
                    .as_ref()
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

                let created_str = c
                    .created
                    .map(|ts| {
                        let dt = std::time::SystemTime::UNIX_EPOCH
                            + std::time::Duration::from_secs(ts as u64);
                        let datetime: chrono::DateTime<chrono::Utc> = dt.into();
                        datetime.format("%Y-%m-%d %H:%M:%S").to_string()
                    })
                    .unwrap_or_default();

                let id_short =
                    c.id.as_ref()
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

    pub async fn handle_docker_containers(&mut self, _data: &str) -> Result<String, String> {
        let docker_client = match &self.docker {
            Some(d) => d,
            None => {
                self.docker = Docker::connect_with_local_defaults().ok();
                self.docker
                    .as_ref()
                    .ok_or_else(|| "Docker 客户端不可用".to_string())?
            }
        };

        docker_client
            .ping()
            .await
            .map_err(|e| format!("Docker daemon 不可用: {}", e))?;

        let options = ListContainersOptions::<String> {
            all: true,
            ..Default::default()
        };
        let list = docker_client
            .list_containers(Some(options))
            .await
            .map_err(|e| format!("获取容器列表失败: {}", e))?;

        #[derive(Serialize)]
        struct DockerContainerListItem {
            id: String,
            name: String,
            image: String,
            status: String,
            state: String,
            created: String,
            ports: String,
        }

        let mut containers = Vec::new();
        for c in list {
            let name = c
                .names
                .as_ref()
                .and_then(|names| names.first())
                .map(|n| n.trim_start_matches('/').to_string())
                .unwrap_or_default();
            let status = c.status.clone().unwrap_or_default();
            let state = c.state.clone().unwrap_or_default();
            let created = c
                .created
                .map(|ts| {
                    let dt = std::time::SystemTime::UNIX_EPOCH
                        + std::time::Duration::from_secs(ts as u64);
                    let datetime: chrono::DateTime<chrono::Utc> = dt.into();
                    datetime.format("%Y-%m-%d %H:%M:%S").to_string()
                })
                .unwrap_or_default();
            let id =
                c.id.as_ref()
                    .map(|id| id.chars().take(12).collect::<String>())
                    .unwrap_or_default();
            let mut ports_vec = Vec::new();
            if let Some(ports) = c.ports {
                for p in ports {
                    if let Some(pub_port) = p.public_port {
                        ports_vec.push(format!("{}:{}", pub_port, p.private_port));
                    } else {
                        ports_vec.push(format!("{}", p.private_port));
                    }
                }
            }

            containers.push(DockerContainerListItem {
                id,
                name,
                image: c.image.unwrap_or_default(),
                status,
                state,
                created,
                ports: ports_vec.join(", "),
            });
        }

        serde_json::to_string(&containers).map_err(|e| format!("序列化容器列表失败: {}", e))
    }

    pub async fn handle_docker_action(&mut self, req_data: &str) -> Result<String, String> {
        let req: DockerActionRequest =
            serde_json::from_str(req_data).map_err(|e| format!("解析请求失败: {}", e))?;

        if req.container_id.is_empty() {
            return Err("缺少容器 ID".to_string());
        }

        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker 客户端不可用".to_string())?;

        let container_id = req.container_id.as_str();
        match req.action.as_str() {
            "start" => docker_client
                .start_container(container_id, None::<StartContainerOptions<String>>)
                .await
                .map_err(|e| format!("Docker start failed: {}", e))?,
            "stop" => docker_client
                .stop_container(container_id, Some(StopContainerOptions { t: 10 }))
                .await
                .map_err(|e| format!("Docker stop failed: {}", e))?,
            "restart" => docker_client
                .restart_container(container_id, Some(RestartContainerOptions { t: 10 }))
                .await
                .map_err(|e| format!("Docker restart failed: {}", e))?,
            "pause" => docker_client
                .pause_container(container_id)
                .await
                .map_err(|e| format!("Docker pause failed: {}", e))?,
            "unpause" => docker_client
                .unpause_container(container_id)
                .await
                .map_err(|e| format!("Docker unpause failed: {}", e))?,
            "rm" | "delete" => docker_client
                .remove_container(
                    container_id,
                    Some(RemoveContainerOptions {
                        force: true,
                        v: false,
                        link: false,
                    }),
                )
                .await
                .map_err(|e| format!("Docker remove failed: {}", e))?,
            _ => return Err(format!("不支持的操作: {}", req.action)),
        }

        Ok(format!("container {} success", req.action))
    }

    pub async fn handle_docker_create_container_api(
        &mut self,
        data: &str,
    ) -> Result<String, String> {
        let req: DockerCreateContainerRequest = serde_json::from_str(data)
            .map_err(|e| format!("Parse create request failed: {}", e))?;
        let image = req.image.trim();
        if image.is_empty() {
            return Err("missing image".to_string());
        }
        if req.privileged.unwrap_or(false) {
            return Err("privileged containers are not supported from this API".to_string());
        }
        if req
            .extra_args
            .as_ref()
            .map(|args| args.iter().any(|arg| !arg.trim().is_empty()))
            .unwrap_or(false)
        {
            return Err("extraArgs is not supported; use explicit create fields".to_string());
        }

        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker client unavailable".to_string())?;

        if docker_client.inspect_image(image).await.is_err() {
            pull_docker_image(docker_client, image).await?;
        }

        let mut exposed_ports: HashMap<String, HashMap<(), ()>> = HashMap::new();
        let mut port_bindings: PortMap = HashMap::new();
        if let Some(ports) = req.ports.as_ref() {
            for port in ports {
                let (container_port, binding) = parse_container_port_binding(port)?;
                exposed_ports.insert(container_port.clone(), HashMap::new());
                port_bindings.insert(container_port, binding);
            }
        }

        let env = req.env.map(|values| {
            values
                .into_iter()
                .map(|(key, value)| format!("{key}={value}"))
                .collect::<Vec<_>>()
        });

        let restart_policy = req
            .restart
            .as_deref()
            .and_then(parse_restart_policy)
            .transpose()?;

        let host_config = HostConfig {
            binds: req.volumes.filter(|volumes| !volumes.is_empty()),
            network_mode: req.network.filter(|network| !network.trim().is_empty()),
            port_bindings: if port_bindings.is_empty() {
                None
            } else {
                Some(port_bindings)
            },
            restart_policy,
            ..Default::default()
        };

        let create_options = req
            .name
            .as_deref()
            .filter(|name| !name.trim().is_empty())
            .map(|name| CreateContainerOptions {
                name: name.trim().to_string(),
                ..Default::default()
            });
        let create_result = docker_client
            .create_container(
                create_options,
                ContainerConfig {
                    image: Some(image.to_string()),
                    env,
                    exposed_ports: if exposed_ports.is_empty() {
                        None
                    } else {
                        Some(exposed_ports)
                    },
                    host_config: Some(host_config),
                    ..Default::default()
                },
            )
            .await
            .map_err(|e| format!("Docker create container failed: {}", e))?;

        docker_client
            .start_container(&create_result.id, None::<StartContainerOptions<String>>)
            .await
            .map_err(|e| format!("Docker start container failed: {}", e))?;

        Ok(format!("container created\nID: {}", create_result.id))
    }

    pub async fn handle_docker_rename_container_api(
        &mut self,
        data: &str,
    ) -> Result<String, String> {
        let req: DockerRenameContainerRequest = serde_json::from_str(data)
            .map_err(|e| format!("Parse rename request failed: {}", e))?;
        if req.container_id.trim().is_empty() || req.new_name.trim().is_empty() {
            return Err("container id and new name are required".to_string());
        }

        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker client unavailable".to_string())?;

        docker_client
            .rename_container(
                req.container_id.trim(),
                RenameContainerOptions {
                    name: req.new_name.trim().to_string(),
                },
            )
            .await
            .map_err(|e| format!("Docker rename container failed: {}", e))?;

        Ok("container renamed".to_string())
    }

    pub async fn handle_docker_stats_api(&mut self, _data: &str) -> Result<String, String> {
        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker client unavailable".to_string())?;

        let containers = docker_client
            .list_containers(Some(ListContainersOptions::<String> {
                all: false,
                ..Default::default()
            }))
            .await
            .map_err(|e| format!("Docker stats list containers failed: {}", e))?;

        let mut result = Vec::new();
        for container in containers {
            let Some(container_id) = container.id.as_deref() else {
                continue;
            };

            let mut stream = docker_client.stats(
                container_id,
                Some(StatsOptions {
                    stream: false,
                    one_shot: false,
                }),
            );

            let Some(item) = stream.next().await else {
                continue;
            };
            let sample = item.map_err(|e| format!("Docker stats failed: {}", e))?;
            result.push(docker_container_stats_from_sample(
                &sample,
                &container.names,
            ));
        }

        serde_json::to_string(&result).map_err(|e| format!("serialize docker stats failed: {}", e))
    }
    pub async fn handle_docker_logs_api(&mut self, req_data: &str) -> Result<String, String> {
        let req: DockerLogsRequest = serde_json::from_str(req_data)
            .map_err(|e| format!("Parse logs request failed: {}", e))?;

        if req.container_id.trim().is_empty() {
            return Err("missing container id".to_string());
        }

        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker client unavailable".to_string())?;

        let mut stream = docker_client.logs(
            req.container_id.trim(),
            Some(LogsOptions::<String> {
                follow: false,
                stdout: true,
                stderr: true,
                since: req
                    .since
                    .as_deref()
                    .and_then(parse_docker_since)
                    .unwrap_or(0),
                until: 0,
                timestamps: false,
                tail: req.tail.unwrap_or(100).max(0).to_string(),
            }),
        );

        let mut logs = String::new();
        while let Some(item) = stream.next().await {
            let output = item.map_err(|e| format!("Docker logs failed: {}", e))?;
            logs.push_str(&String::from_utf8_lossy(output.as_ref()));
        }
        Ok(logs)
    }
    pub async fn handle_docker_images_api(&mut self, _data: &str) -> Result<String, String> {
        #[derive(Serialize)]
        struct DockerImageInfo {
            repository: String,
            tag: String,
            id: String,
            created: String,
            size: String,
        }

        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker client unavailable".to_string())?;

        let list = docker_client
            .list_images(Some(ListImagesOptions::<String> {
                all: true,
                ..Default::default()
            }))
            .await
            .map_err(|e| format!("Failed to list Docker images: {}", e))?;

        let mut images = Vec::new();
        for image in list {
            let id = short_docker_id(&image.id);
            let created = unix_seconds_to_display(image.created);
            let size = format_docker_bytes(image.size);
            let tags = if image.repo_tags.is_empty() {
                vec!["<none>:<none>".to_string()]
            } else {
                image.repo_tags
            };
            for full_tag in tags {
                let (repository, tag) = split_image_tag(&full_tag);
                images.push(DockerImageInfo {
                    repository,
                    tag,
                    id: id.clone(),
                    created: created.clone(),
                    size: size.clone(),
                });
            }
        }

        serde_json::to_string(&images)
            .map_err(|e| format!("Failed to serialize Docker images: {}", e))
    }

    pub async fn handle_docker_networks_api(&mut self, _data: &str) -> Result<String, String> {
        #[derive(Serialize)]
        struct DockerNetworkInfo {
            id: String,
            name: String,
            driver: String,
            scope: String,
        }

        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker client unavailable".to_string())?;

        let networks = docker_client
            .list_networks(Some(ListNetworksOptions::<String> {
                ..Default::default()
            }))
            .await
            .map_err(|e| format!("Failed to list Docker networks: {}", e))?
            .into_iter()
            .map(|network| DockerNetworkInfo {
                id: network
                    .id
                    .map(|id| short_docker_id(&id))
                    .unwrap_or_default(),
                name: network.name.unwrap_or_default(),
                driver: network.driver.unwrap_or_default(),
                scope: network.scope.unwrap_or_default(),
            })
            .collect::<Vec<_>>();

        serde_json::to_string(&networks)
            .map_err(|e| format!("Failed to serialize Docker networks: {}", e))
    }

    pub async fn handle_docker_volumes_api(&mut self, _data: &str) -> Result<String, String> {
        #[derive(Serialize)]
        struct DockerVolumeInfo {
            name: String,
            driver: String,
            scope: String,
        }

        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker client unavailable".to_string())?;

        let volumes = docker_client
            .list_volumes(Some(ListVolumesOptions::<String> {
                ..Default::default()
            }))
            .await
            .map_err(|e| format!("Failed to list Docker volumes: {}", e))?
            .volumes
            .unwrap_or_default()
            .into_iter()
            .map(|volume| DockerVolumeInfo {
                name: volume.name,
                driver: volume.driver,
                scope: volume
                    .scope
                    .map(|scope| scope.to_string())
                    .unwrap_or_default(),
            })
            .collect::<Vec<_>>();

        serde_json::to_string(&volumes)
            .map_err(|e| format!("Failed to serialize Docker volumes: {}", e))
    }

    pub async fn handle_docker_image_action_api(&mut self, data: &str) -> Result<String, String> {
        #[derive(Deserialize)]
        struct ImageActionReq {
            action: String,
            #[serde(default)]
            image: String,
        }
        let req: ImageActionReq =
            serde_json::from_str(data).map_err(|e| format!("Parse image action failed: {}", e))?;

        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker client unavailable".to_string())?;

        match req.action.as_str() {
            "pull" => {
                if req.image.trim().is_empty() {
                    return Err("missing image".to_string());
                }
                let (from_image, tag) = split_pull_image_ref(req.image.trim());
                let mut stream = docker_client.create_image(
                    Some(CreateImageOptions::<String> {
                        from_image,
                        tag,
                        ..Default::default()
                    }),
                    None,
                    None,
                );
                let mut last_status = String::new();
                while let Some(item) = stream.next().await {
                    let info = item.map_err(|e| format!("Docker image pull failed: {}", e))?;
                    if let Some(status) = info.status {
                        last_status = status;
                    }
                    if let Some(progress) = info.progress {
                        if !progress.trim().is_empty() {
                            last_status = progress;
                        }
                    }
                }
                if last_status.is_empty() {
                    last_status = "image pull success".to_string();
                }
                Ok(last_status)
            }
            "remove" => {
                if req.image.trim().is_empty() {
                    return Err("missing image".to_string());
                }
                docker_client
                    .remove_image(
                        req.image.trim(),
                        Some(RemoveImageOptions {
                            force: true,
                            noprune: false,
                        }),
                        None,
                    )
                    .await
                    .map_err(|e| format!("Docker image remove failed: {}", e))?;
                Ok("image remove success".to_string())
            }
            "prune" => {
                let result = docker_client
                    .prune_images(Some(PruneImagesOptions::<String> {
                        ..Default::default()
                    }))
                    .await
                    .map_err(|e| format!("Docker image prune failed: {}", e))?;
                Ok(format!(
                    "image prune success, reclaimed {}",
                    format_docker_bytes(result.space_reclaimed.unwrap_or_default())
                ))
            }
            _ => Err(format!("unsupported image action: {}", req.action)),
        }
    }

    pub async fn handle_docker_network_action_api(&mut self, data: &str) -> Result<String, String> {
        #[derive(Deserialize)]
        struct NetworkActionReq {
            action: String,
            #[serde(default)]
            name: String,
            driver: Option<String>,
            subnet: Option<String>,
            gateway: Option<String>,
            container: Option<String>,
        }
        let req: NetworkActionReq = serde_json::from_str(data)
            .map_err(|e| format!("Parse network action failed: {}", e))?;

        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker client unavailable".to_string())?;

        match req.action.as_str() {
            "create" => {
                if req.name.trim().is_empty() {
                    return Err("missing network name".to_string());
                }
                let mut ipam = Ipam::default();
                if req.subnet.as_deref().unwrap_or("").trim() != ""
                    || req.gateway.as_deref().unwrap_or("").trim() != ""
                {
                    ipam.config = Some(vec![IpamConfig {
                        subnet: req
                            .subnet
                            .as_deref()
                            .map(str::trim)
                            .filter(|value| !value.is_empty())
                            .map(str::to_string),
                        gateway: req
                            .gateway
                            .as_deref()
                            .map(str::trim)
                            .filter(|value| !value.is_empty())
                            .map(str::to_string),
                        ..Default::default()
                    }]);
                }
                let response = docker_client
                    .create_network(CreateNetworkOptions::<String> {
                        name: req.name.trim().to_string(),
                        check_duplicate: true,
                        driver: req
                            .driver
                            .as_deref()
                            .map(str::trim)
                            .filter(|value| !value.is_empty())
                            .unwrap_or("bridge")
                            .to_string(),
                        internal: false,
                        attachable: false,
                        ingress: false,
                        ipam,
                        enable_ipv6: false,
                        options: HashMap::new(),
                        labels: HashMap::new(),
                    })
                    .await
                    .map_err(|e| format!("Docker network create failed: {}", e))?;
                Ok(format!(
                    "network create success{}",
                    response
                        .id
                        .map(|id| format!(": {}", short_docker_id(&id)))
                        .unwrap_or_default()
                ))
            }
            "remove" => {
                if req.name.trim().is_empty() {
                    return Err("missing network name".to_string());
                }
                docker_client
                    .remove_network(req.name.trim())
                    .await
                    .map_err(|e| format!("Docker network remove failed: {}", e))?;
                Ok("network remove success".to_string())
            }
            "connect" => {
                if req.name.trim().is_empty() {
                    return Err("missing network name".to_string());
                }
                let container = req
                    .container
                    .as_deref()
                    .map(str::trim)
                    .filter(|value| !value.is_empty())
                    .ok_or_else(|| "missing container".to_string())?;
                docker_client
                    .connect_network(
                        req.name.trim(),
                        ConnectNetworkOptions {
                            container,
                            endpoint_config: EndpointSettings::default(),
                        },
                    )
                    .await
                    .map_err(|e| format!("Docker network connect failed: {}", e))?;
                Ok("network connect success".to_string())
            }
            "disconnect" => {
                if req.name.trim().is_empty() {
                    return Err("missing network name".to_string());
                }
                let container = req
                    .container
                    .as_deref()
                    .map(str::trim)
                    .filter(|value| !value.is_empty())
                    .ok_or_else(|| "missing container".to_string())?;
                docker_client
                    .disconnect_network(
                        req.name.trim(),
                        DisconnectNetworkOptions {
                            container,
                            force: true,
                        },
                    )
                    .await
                    .map_err(|e| format!("Docker network disconnect failed: {}", e))?;
                Ok("network disconnect success".to_string())
            }
            "prune" => {
                let result = docker_client
                    .prune_networks(Some(PruneNetworksOptions::<String> {
                        ..Default::default()
                    }))
                    .await
                    .map_err(|e| format!("Docker network prune failed: {}", e))?;
                Ok(format!(
                    "network prune success, deleted {}",
                    result.networks_deleted.unwrap_or_default().len()
                ))
            }
            _ => Err(format!("unsupported network action: {}", req.action)),
        }
    }

    pub async fn handle_docker_volume_action_api(&mut self, data: &str) -> Result<String, String> {
        #[derive(Deserialize)]
        struct VolumeActionReq {
            action: String,
            #[serde(default)]
            name: String,
            driver: Option<String>,
        }
        let req: VolumeActionReq =
            serde_json::from_str(data).map_err(|e| format!("Parse volume action failed: {}", e))?;

        if self.docker.is_none() {
            self.docker = Docker::connect_with_local_defaults().ok();
        }
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker client unavailable".to_string())?;

        match req.action.as_str() {
            "create" => {
                if req.name.trim().is_empty() {
                    return Err("missing volume name".to_string());
                }
                docker_client
                    .create_volume(CreateVolumeOptions::<String> {
                        name: req.name.trim().to_string(),
                        driver: req.driver.unwrap_or_else(|| "local".to_string()),
                        driver_opts: HashMap::new(),
                        labels: HashMap::new(),
                    })
                    .await
                    .map_err(|e| format!("Docker volume create failed: {}", e))?;
                Ok("volume create success".to_string())
            }
            "remove" => {
                if req.name.trim().is_empty() {
                    return Err("missing volume name".to_string());
                }
                docker_client
                    .remove_volume(req.name.trim(), Some(RemoveVolumeOptions { force: true }))
                    .await
                    .map_err(|e| format!("Docker volume remove failed: {}", e))?;
                Ok("volume remove success".to_string())
            }
            "prune" => {
                let result = docker_client
                    .prune_volumes(Some(PruneVolumesOptions::<String> {
                        ..Default::default()
                    }))
                    .await
                    .map_err(|e| format!("Docker volume prune failed: {}", e))?;
                Ok(format!(
                    "volume prune success, reclaimed {}",
                    format_docker_bytes(result.space_reclaimed.unwrap_or_default())
                ))
            }
            _ => Err(format!("unsupported volume action: {}", req.action)),
        }
    }

    pub fn handle_docker_compose_list(&self, _data: &str) -> Result<String, String> {
        let output = Command::new("docker")
            .args(["compose", "ls", "--format", "json"])
            .output()
            .or_else(|_| {
                Command::new("docker-compose")
                    .args(["ls", "--format", "json"])
                    .output()
            })
            .map_err(|e| format!("获取 Compose 项目失败: {}", e))?;

        Ok(String::from_utf8_lossy(&output.stdout).to_string())
    }

    pub fn handle_docker_compose_action(&self, data: &str) -> Result<String, String> {
        // Parse: config_file, action
        #[derive(Deserialize)]
        struct ComposeActionReq {
            #[serde(
                default,
                rename = "config_file",
                alias = "configFile",
                alias = "configFiles",
                alias = "ConfigFiles"
            )]
            config_file: String,
            #[serde(default, alias = "projectName", alias = "Name", alias = "name")]
            project: Option<String>,
            action: String,
        }

        let req: ComposeActionReq =
            serde_json::from_str(data).map_err(|e| format!("解析请求失败: {}", e))?;

        let mut args = vec![];
        if !req.config_file.is_empty() {
            for file in req.config_file.split(',') {
                let file = file.trim();
                if !file.is_empty() {
                    args.push("-f");
                    args.push(file);
                }
            }
        } else if let Some(project) = req.project.as_deref() {
            if !project.trim().is_empty() {
                args.push("--project-name");
                args.push(project.trim());
            }
        }

        match req.action.as_str() {
            "up" => args.extend(["up", "-d"]),
            "down" => args.push("down"),
            "stop" => args.push("stop"),
            "start" => args.push("start"),
            "restart" => args.push("restart"),
            "pull" => args.push("pull"),
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
    pub async fn handle_docker_check_update(&mut self, req_data: &str) -> Result<String, String> {
        let req: DockerCheckUpdateRequest = if req_data.is_empty() {
            DockerCheckUpdateRequest { container_id: None }
        } else {
            serde_json::from_str(req_data).map_err(|e| format!("解析请求失败: {}", e))?
        };

        let docker_client = self
            .docker
            .as_ref()
            .cloned()
            .ok_or_else(|| "Docker 客户端不可用".to_string())?;

        let mut containers = Vec::new();

        let has_specific_container = if let Some(ref cid) = req.container_id {
            !cid.trim().is_empty()
        } else {
            false
        };

        if has_specific_container {
            let cid = req.container_id.as_ref().unwrap();
            let c = docker_client
                .inspect_container(cid, None)
                .await
                .map_err(|e| format!("获取容器信息失败: {}", e))?;

            containers.push((
                c.id.clone().unwrap_or_default(),
                c.name.clone().unwrap_or_default(),
                c.config.and_then(|cfg| cfg.image).unwrap_or_default(),
            ));
        } else {
            let options = ListContainersOptions::<String> {
                all: false,
                ..Default::default()
            };
            let list = docker_client
                .list_containers(Some(options))
                .await
                .map_err(|e| format!("获取容器列表失败: {}", e))?;

            for c in list {
                let name = c
                    .names
                    .as_ref()
                    .and_then(|names| names.first())
                    .cloned()
                    .unwrap_or_default();
                containers.push((c.id.unwrap_or_default(), name, c.image.unwrap_or_default()));
            }
        }

        if containers.is_empty() {
            return Ok("[]".to_string());
        }

        let mut results = Vec::new();
        let mut missing_remote = HashSet::new();

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
                let cache_key = remote_digest_cache_key(&registry, &repo, &tag);
                if let Some(cached) = self.cached_remote_digest(&cache_key) {
                    status.latest_digest = cached;
                    status.has_update = !status.current_digest.is_empty()
                        && !status.latest_digest.is_empty()
                        && status.current_digest != status.latest_digest;
                } else {
                    missing_remote.insert((registry, repo, tag));
                }
            }

            results.push(status);
        }

        if !missing_remote.is_empty() {
            let fetched = stream::iter(missing_remote.into_iter().map(
                |(registry, repo, tag)| async move {
                    let cache_key = remote_digest_cache_key(&registry, &repo, &tag);
                    let result = get_remote_digest(&registry, &repo, &tag).await;
                    (cache_key, result)
                },
            ))
            .buffer_unordered(8)
            .collect::<Vec<_>>()
            .await;

            let mut remote_results = HashMap::new();
            for (cache_key, result) in fetched {
                if let Ok(digest) = &result {
                    self.remote_digest_cache.insert(
                        cache_key.clone(),
                        RemoteDigestCacheEntry {
                            digest: digest.clone(),
                            expires_at: Instant::now() + Duration::from_secs(30),
                        },
                    );
                }
                remote_results.insert(cache_key, result);
            }

            for status in results.iter_mut() {
                if status.error.is_some() || status.image.starts_with("sha256:") {
                    continue;
                }
                if !status.latest_digest.is_empty() {
                    continue;
                }
                let (registry, repo, tag) = parse_image_name(&status.image);
                let cache_key = remote_digest_cache_key(&registry, &repo, &tag);
                match remote_results.remove(&cache_key) {
                    Some(Ok(remote_digest)) => {
                        status.latest_digest = remote_digest.clone();
                        status.has_update = !status.current_digest.is_empty()
                            && !remote_digest.is_empty()
                            && status.current_digest != remote_digest;
                    }
                    Some(Err(e)) => {
                        status.error = Some(format!("获取远程镜像信息失败: {}", e));
                    }
                    None => {}
                }
            }
        }

        serde_json::to_string(&results).map_err(|e| format!("序列化结果失败: {}", e))
    }

    fn cached_remote_digest(&mut self, cache_key: &str) -> Option<String> {
        let now = Instant::now();
        self.remote_digest_cache
            .retain(|_, entry| entry.expires_at > now);
        self.remote_digest_cache
            .get(cache_key)
            .map(|entry| entry.digest.clone())
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

        let output = Command::new("docker").args(&pull_args).output();

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
            return Err(format!(
                "Compose 拉取镜像失败:\n{}",
                String::from_utf8_lossy(&output.stderr)
            ));
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
            return Err(format!(
                "Compose 重建容器失败: {}",
                String::from_utf8_lossy(&up_output.stderr)
            ));
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
        let docker_client = self
            .docker
            .as_ref()
            .ok_or_else(|| "Docker 客户端不可用".to_string())?;

        update_progress(5, "获取容器配置...", "");
        let inspect = docker_client
            .inspect_container(container_id, None)
            .await
            .map_err(|e| format!("获取容器配置失败: {}", e))?;

        update_progress(10, &format!("正在拉取镜像: {}", new_image), "");
        pull_docker_image(docker_client, new_image).await?;

        update_progress(40, "镜像拉取完成", "");

        update_progress(50, "正在停止旧容器 (等待优雅退出)...", "");
        let stop_options = bollard::container::StopContainerOptions { t: 30 };
        docker_client
            .stop_container(container_id, Some(stop_options))
            .await
            .map_err(|e| format!("停止容器失败: {}", e))?;

        update_progress(60, "正在备份旧容器元数据...", "");
        let timestamp = chrono::Local::now().format("%Y%m%d-%H%M%S").to_string();
        let backup_name = format!("{}-backup-{}", container_name, timestamp);
        let rename_options = bollard::container::RenameContainerOptions {
            name: backup_name.clone(),
        };

        if let Err(e) = docker_client
            .rename_container(container_id, rename_options)
            .await
        {
            let _ = docker_client
                .start_container::<String>(container_id, None)
                .await;
            return Err(format!("备份容器失败: {}", e));
        }

        update_progress(75, "正在克隆配置并创建新容器...", "");
        let config = inspect.config.ok_or_else(|| "旧容器缺少配置".to_string())?;
        let mut config: bollard::container::Config<String> = config.into();
        config.image = Some(new_image.to_string());
        config.host_config = inspect.host_config;

        let networks = inspect
            .network_settings
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
        let networking_config = bollard::container::NetworkingConfig { endpoints_config };

        let create_options = CreateContainerOptions {
            name: container_name.to_string(),
            ..Default::default()
        };

        let create_res = docker_client
            .create_container(
                Some(create_options),
                bollard::container::Config {
                    networking_config: Some(networking_config),
                    ..config
                },
            )
            .await;

        let created_id = match create_res {
            Ok(res) => res.id,
            Err(e) => {
                let _ = docker_client
                    .rename_container(
                        &backup_name,
                        bollard::container::RenameContainerOptions {
                            name: container_name.to_string(),
                        },
                    )
                    .await;
                let _ = docker_client
                    .start_container::<String>(container_id, None)
                    .await;
                return Err(format!("创建新容器失败: {}", e));
            }
        };

        update_progress(90, "正在启动新容器...", "");
        if let Err(e) = docker_client
            .start_container::<String>(&created_id, None)
            .await
        {
            let _ = docker_client.remove_container(&created_id, None).await;
            let _ = docker_client
                .rename_container(
                    &backup_name,
                    bollard::container::RenameContainerOptions {
                        name: container_name.to_string(),
                    },
                )
                .await;
            let _ = docker_client
                .start_container::<String>(container_id, None)
                .await;
            return Err(format!("启动新容器失败: {}", e));
        }

        update_progress(98, "正在清理旧容器备份...", "");
        let remove_options = bollard::container::RemoveContainerOptions {
            force: true,
            ..Default::default()
        };
        if let Err(e) = docker_client
            .remove_container(&backup_name, Some(remove_options))
            .await
        {
            println!("[Docker] 删除备份容器失败: {}", e);
        }

        update_progress(100, "更新完成", "");
        Ok(())
    }
}

async fn pull_docker_image(docker_client: &Docker, image: &str) -> Result<(), String> {
    let (from_image, tag) = split_pull_image_ref(image.trim());
    let mut stream = docker_client.create_image(
        Some(CreateImageOptions {
            from_image,
            tag,
            ..Default::default()
        }),
        None,
        None,
    );
    while let Some(item) = stream.next().await {
        item.map_err(|e| format!("Docker image pull failed: {}", e))?;
    }
    Ok(())
}

fn short_docker_id(id: &str) -> String {
    id.strip_prefix("sha256:")
        .unwrap_or(id)
        .chars()
        .take(12)
        .collect()
}

fn unix_seconds_to_display(seconds: i64) -> String {
    if seconds <= 0 {
        return String::new();
    }
    let dt = std::time::SystemTime::UNIX_EPOCH + std::time::Duration::from_secs(seconds as u64);
    let datetime: chrono::DateTime<chrono::Utc> = dt.into();
    datetime.format("%Y-%m-%d %H:%M:%S").to_string()
}

fn split_image_tag(full_tag: &str) -> (String, String) {
    if full_tag == "<none>:<none>" {
        return ("<none>".to_string(), "<none>".to_string());
    }
    let slash_idx = full_tag.rfind('/').unwrap_or(0);
    let tag_idx = full_tag[slash_idx..].rfind(':').map(|idx| slash_idx + idx);
    if let Some(idx) = tag_idx {
        (full_tag[..idx].to_string(), full_tag[idx + 1..].to_string())
    } else {
        (full_tag.to_string(), "latest".to_string())
    }
}

fn split_pull_image_ref(image: &str) -> (String, String) {
    if let Some(idx) = image.rfind('@') {
        return (image[..idx].to_string(), image[idx + 1..].to_string());
    }
    split_image_tag(image)
}

fn parse_docker_since(value: &str) -> Option<i64> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return None;
    }
    if let Ok(seconds) = trimmed.parse::<i64>() {
        return Some(seconds.max(0));
    }
    if let Ok(dt) = chrono::DateTime::parse_from_rfc3339(trimmed) {
        return Some(dt.timestamp().max(0));
    }
    None
}

fn parse_container_port_binding(value: &str) -> Result<(String, Option<Vec<PortBinding>>), String> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Err("empty port binding".to_string());
    }

    let parts: Vec<&str> = trimmed.split(':').collect();
    let (host_ip, host_port, container_port) = match parts.as_slice() {
        [container] => (None, Some(""), *container),
        [host, container] => (None, Some(*host), *container),
        [ip, host, container] => (Some(*ip), Some(*host), *container),
        _ => return Err(format!("invalid port binding: {trimmed}")),
    };

    let container_port = normalize_container_port(container_port)?;
    let binding = PortBinding {
        host_ip: host_ip
            .map(str::trim)
            .filter(|ip| !ip.is_empty())
            .map(str::to_string),
        host_port: host_port
            .map(str::trim)
            .filter(|port| !port.is_empty())
            .map(str::to_string),
    };

    Ok((container_port, Some(vec![binding])))
}

fn normalize_container_port(value: &str) -> Result<String, String> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Err("empty container port".to_string());
    }
    if trimmed.contains('/') {
        Ok(trimmed.to_string())
    } else {
        Ok(format!("{trimmed}/tcp"))
    }
}

fn parse_restart_policy(value: &str) -> Option<Result<RestartPolicy, String>> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return None;
    }

    let mut parts = trimmed.splitn(2, ':');
    let name = parts.next().unwrap_or("");
    let maximum_retry_count = parts
        .next()
        .map(|raw| {
            raw.parse::<i64>()
                .map_err(|_| format!("invalid restart retry count: {raw}"))
        })
        .transpose();

    let name = match name {
        "no" => RestartPolicyNameEnum::NO,
        "always" => RestartPolicyNameEnum::ALWAYS,
        "unless-stopped" => RestartPolicyNameEnum::UNLESS_STOPPED,
        "on-failure" => RestartPolicyNameEnum::ON_FAILURE,
        other => return Some(Err(format!("unsupported restart policy: {other}"))),
    };

    Some(
        maximum_retry_count.map(|maximum_retry_count| RestartPolicy {
            name: Some(name),
            maximum_retry_count,
        }),
    )
}

fn docker_container_stats_from_sample(
    sample: &Stats,
    listed_names: &Option<Vec<String>>,
) -> DockerContainerStats {
    let id = if sample.id.is_empty() {
        String::new()
    } else {
        short_docker_id(&sample.id)
    };
    let name = if !sample.name.is_empty() {
        sample.name.trim_start_matches('/').to_string()
    } else {
        listed_names
            .as_ref()
            .and_then(|names| names.first())
            .map(|name| name.trim_start_matches('/').to_string())
            .unwrap_or_default()
    };

    let online_cpus = sample
        .cpu_stats
        .online_cpus
        .or_else(|| {
            sample
                .cpu_stats
                .cpu_usage
                .percpu_usage
                .as_ref()
                .map(|usage| usage.len() as u64)
        })
        .unwrap_or(0);
    let cpu_percent = calculate_docker_cpu_percent(
        sample.cpu_stats.cpu_usage.total_usage,
        sample.precpu_stats.cpu_usage.total_usage,
        sample.cpu_stats.system_cpu_usage.unwrap_or(0),
        sample.precpu_stats.system_cpu_usage.unwrap_or(0),
        online_cpus,
    );

    let memory_usage = docker_memory_usage(sample);
    let memory_limit = sample.memory_stats.limit.unwrap_or(0);
    let memory_percent = if memory_limit > 0 {
        (memory_usage as f64 / memory_limit as f64) * 100.0
    } else {
        0.0
    };
    let (net_rx, net_tx) = docker_network_io(sample);
    let (block_read, block_write) = docker_block_io(sample);

    DockerContainerStats {
        container_id: id,
        name,
        cpu_percent: format_percent(cpu_percent),
        mem_usage: format!(
            "{} / {}",
            format_docker_bytes(memory_usage as i64),
            format_docker_bytes(memory_limit as i64)
        ),
        mem_percent: format_percent(memory_percent),
        net_io: format!(
            "{} / {}",
            format_docker_bytes(net_rx as i64),
            format_docker_bytes(net_tx as i64)
        ),
        block_io: format!(
            "{} / {}",
            format_docker_bytes(block_read as i64),
            format_docker_bytes(block_write as i64)
        ),
    }
}

fn calculate_docker_cpu_percent(
    cpu_total: u64,
    precpu_total: u64,
    system_total: u64,
    presystem_total: u64,
    online_cpus: u64,
) -> f64 {
    let cpu_delta = cpu_total.saturating_sub(precpu_total);
    let system_delta = system_total.saturating_sub(presystem_total);
    if cpu_delta == 0 || system_delta == 0 || online_cpus == 0 {
        return 0.0;
    }
    (cpu_delta as f64 / system_delta as f64) * online_cpus as f64 * 100.0
}

fn docker_memory_usage(sample: &Stats) -> u64 {
    let usage = sample
        .memory_stats
        .usage
        .or(sample.memory_stats.privateworkingset)
        .unwrap_or(0);
    let cache = match sample.memory_stats.stats {
        Some(bollard::container::MemoryStatsStats::V1(stats)) => stats.total_inactive_file,
        Some(bollard::container::MemoryStatsStats::V2(stats)) => stats.inactive_file,
        None => 0,
    };
    docker_memory_usage_no_cache(usage, cache)
}

fn docker_memory_usage_no_cache(usage: u64, cache: u64) -> u64 {
    if usage > cache {
        usage - cache
    } else {
        usage
    }
}

fn docker_network_io(sample: &Stats) -> (u64, u64) {
    if let Some(networks) = sample.networks.as_ref() {
        return networks.values().fold((0, 0), |(rx, tx), network| {
            (rx + network.rx_bytes, tx + network.tx_bytes)
        });
    }
    sample
        .network
        .as_ref()
        .map(|network| (network.rx_bytes, network.tx_bytes))
        .unwrap_or((0, 0))
}

fn docker_block_io(sample: &Stats) -> (u64, u64) {
    let mut read = 0;
    let mut write = 0;
    if let Some(entries) = sample.blkio_stats.io_service_bytes_recursive.as_ref() {
        for entry in entries {
            match entry.op.to_ascii_lowercase().as_str() {
                "read" => read += entry.value,
                "write" => write += entry.value,
                _ => {}
            }
        }
    }
    (read, write)
}

fn format_percent(value: f64) -> String {
    if !value.is_finite() || value < 0.0 {
        return "0.00%".to_string();
    }
    format!("{:.2}%", value)
}

fn format_docker_bytes(bytes: i64) -> String {
    if bytes < 0 {
        return "0 B".to_string();
    }
    let units = ["B", "KB", "MB", "GB", "TB"];
    let mut value = bytes as f64;
    let mut unit = 0;
    while value >= 1000.0 && unit < units.len() - 1 {
        value /= 1000.0;
        unit += 1;
    }
    if unit == 0 {
        format!("{} {}", bytes, units[unit])
    } else {
        format!("{:.1} {}", value, units[unit])
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
    let mut last_err = String::new();
    for host in registry_hosts(registry) {
        match try_get_digest_from_host(&host, repo, tag).await {
            Ok(digest) => return Ok(digest),
            Err(e) => {
                last_err = format!("{}: {}", host, e);
                println!("[Docker] digest lookup failed on {}: {}", host, e);
            }
        }
    }

    Err(format!("all registry digest lookups failed: {}", last_err))
}

async fn try_get_digest_from_host(host: &str, repo: &str, tag: &str) -> Result<String, String> {
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(15))
        .build()
        .map_err(|e| format!("构建 HTTP 客户端失败: {}", e))?;

    let challenge_url = format!("https://{}/v2/", host);
    let challenge_resp = client
        .get(&challenge_url)
        .send()
        .await
        .map_err(|e| format!("challenge 请求失败: {}", e))?;

    let headers = challenge_resp.headers();
    let www_auth = headers
        .get("www-authenticate")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");

    let mut token = String::new();
    if www_auth.to_lowercase().starts_with("bearer") {
        token = get_bearer_token(www_auth, repo, &client).await?;
    }

    let manifest_url = format!("https://{}/v2/{}/manifests/{}", host, repo, tag);
    match fetch_manifest_digest(&client, reqwest::Method::HEAD, &manifest_url, &token).await {
        Ok(digest) => Ok(digest),
        Err(head_err) => {
            fetch_manifest_digest(&client, reqwest::Method::GET, &manifest_url, &token)
                .await
                .map_err(|get_err| format!("HEAD failed: {}; GET failed: {}", head_err, get_err))
        }
    }
}

async fn fetch_manifest_digest(
    client: &reqwest::Client,
    method: reqwest::Method,
    manifest_url: &str,
    token: &str,
) -> Result<String, String> {
    let mut req = client.request(method, manifest_url);
    if !token.is_empty() {
        req = req.bearer_auth(token);
    }

    let resp = req
        .header("Accept", manifest_accept_header())
        .send()
        .await
        .map_err(|e| format!("manifest 请求失败: {}", e))?;

    if !resp.status().is_success() {
        return Err(format!("registry 返回状态码: {}", resp.status()));
    }

    resp.headers()
        .get("docker-content-digest")
        .and_then(|v| v.to_str().ok())
        .map(|s| s.to_string())
        .ok_or_else(|| "响应中未包含 docker-content-digest".to_string())
}

fn manifest_accept_header() -> &'static str {
    "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.v1+json"
}

fn registry_hosts(registry: &str) -> Vec<String> {
    let mut hosts = Vec::new();
    let canonical = if registry == "docker.io" || registry == "registry-1.docker.io" {
        "registry-1.docker.io"
    } else {
        registry
    };
    hosts.push(canonical.to_string());

    if registry == "docker.io" || registry == "registry-1.docker.io" {
        if let Ok(raw) = std::env::var("API_MONITOR_DOCKER_REGISTRY_MIRRORS") {
            for host in raw.split(',') {
                let trimmed = host
                    .trim()
                    .trim_start_matches("https://")
                    .trim_start_matches("http://");
                if !trimmed.is_empty() && !hosts.iter().any(|h| h == trimmed) {
                    hosts.push(trimmed.to_string());
                }
            }
        }
    }

    hosts
}

fn remote_digest_cache_key(registry: &str, repo: &str, tag: &str) -> String {
    format!(
        "{}/{repo}:{tag}",
        if registry == "docker.io" {
            "registry-1.docker.io"
        } else {
            registry
        }
    )
}

async fn get_bearer_token(
    www_auth: &str,
    repo: &str,
    client: &reqwest::Client,
) -> Result<String, String> {
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

    let realm = params
        .get("realm")
        .ok_or_else(|| format!("无法解析 realm from: {}", www_auth))?;
    let service = params.get("service").map(|s| s.as_str()).unwrap_or("");

    let resp = client
        .get(realm)
        .query(&[
            ("service", service.to_string()),
            ("scope", format!("repository:{}:pull", repo)),
        ])
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

    let token_resp: TokenResp = resp
        .json()
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_docker_hub_images() {
        assert_eq!(
            parse_image_name("nginx"),
            (
                "registry-1.docker.io".to_string(),
                "library/nginx".to_string(),
                "latest".to_string()
            )
        );
        assert_eq!(
            parse_image_name("library/redis:7"),
            (
                "registry-1.docker.io".to_string(),
                "library/redis".to_string(),
                "7".to_string()
            )
        );
    }

    #[test]
    fn parses_custom_registry_images() {
        assert_eq!(
            parse_image_name("registry.example.com/team/app:v1"),
            (
                "registry.example.com".to_string(),
                "team/app".to_string(),
                "v1".to_string()
            )
        );
        assert_eq!(
            parse_image_name("localhost:5000/app:dev"),
            (
                "localhost:5000".to_string(),
                "app".to_string(),
                "dev".to_string()
            )
        );
    }

    #[test]
    fn manifest_accept_header_keeps_all_supported_media_types() {
        let header = manifest_accept_header();
        assert!(header.contains("application/vnd.oci.image.index.v1+json"));
        assert!(header.contains("application/vnd.docker.distribution.manifest.list.v2+json"));
        assert!(header.contains("application/vnd.docker.distribution.manifest.v2+json"));
        assert!(header.contains(","));
    }

    #[test]
    fn docker_hub_registry_uses_canonical_host() {
        assert_eq!(
            registry_hosts("docker.io").first().map(String::as_str),
            Some("registry-1.docker.io")
        );
        assert_eq!(
            registry_hosts("registry-1.docker.io")
                .first()
                .map(String::as_str),
            Some("registry-1.docker.io")
        );
        assert_eq!(
            remote_digest_cache_key("docker.io", "library/nginx", "latest"),
            "registry-1.docker.io/library/nginx:latest"
        );
    }

    #[test]
    fn docker_api_display_helpers_match_cli_shapes() {
        assert_eq!(
            split_image_tag("nginx:1.25"),
            ("nginx".to_string(), "1.25".to_string())
        );
        assert_eq!(
            split_image_tag("localhost:5000/app:dev"),
            ("localhost:5000/app".to_string(), "dev".to_string())
        );
        assert_eq!(
            split_image_tag("registry.example.com/team/app"),
            (
                "registry.example.com/team/app".to_string(),
                "latest".to_string()
            )
        );
        assert_eq!(
            split_pull_image_ref("nginx@sha256:abcdef"),
            ("nginx".to_string(), "sha256:abcdef".to_string())
        );
        assert_eq!(parse_docker_since("1700000000"), Some(1_700_000_000));
        assert_eq!(
            parse_docker_since("2024-01-01T00:00:00Z"),
            Some(1_704_067_200)
        );
        assert_eq!(parse_docker_since("10m"), None);
        assert_eq!(short_docker_id("sha256:1234567890abcdef"), "1234567890ab");
        assert_eq!(format_docker_bytes(999), "999 B");
        assert_eq!(format_docker_bytes(1_500_000), "1.5 MB");
        assert_eq!(
            calculate_docker_cpu_percent(1_500, 1_000, 20_000, 10_000, 2),
            10.0
        );
        assert_eq!(calculate_docker_cpu_percent(1_500, 1_000, 0, 0, 2), 0.0);
        assert_eq!(docker_memory_usage_no_cache(1_000, 250), 750);
        assert_eq!(docker_memory_usage_no_cache(100, 250), 100);
        assert_eq!(format_percent(3.456), "3.46%");
        assert_eq!(format_percent(f64::NAN), "0.00%");

        let (port, binding) = parse_container_port_binding("127.0.0.1:8080:80").unwrap();
        assert_eq!(port, "80/tcp");
        let binding = binding.unwrap().remove(0);
        assert_eq!(binding.host_ip.as_deref(), Some("127.0.0.1"));
        assert_eq!(binding.host_port.as_deref(), Some("8080"));

        let (port, binding) = parse_container_port_binding("53/udp").unwrap();
        assert_eq!(port, "53/udp");
        assert_eq!(binding.unwrap()[0].host_port, None);

        let restart = parse_restart_policy("on-failure:3").unwrap().unwrap();
        assert_eq!(restart.name, Some(RestartPolicyNameEnum::ON_FAILURE));
        assert_eq!(restart.maximum_retry_count, Some(3));
        assert!(parse_restart_policy("host-crash").unwrap().is_err());
    }

    #[test]
    fn docker_task_requests_accept_frontend_and_backend_field_names() {
        let action: DockerActionRequest =
            serde_json::from_str(r#"{"action":"start","container_id":"abc123"}"#).unwrap();
        assert_eq!(action.container_id, "abc123");

        let logs: DockerLogsRequest =
            serde_json::from_str(r#"{"containerId":"abc123","tail":50}"#).unwrap();
        assert_eq!(logs.container_id, "abc123");

        let rename: DockerRenameContainerRequest =
            serde_json::from_str(r#"{"container_id":"abc123","new_name":"web"}"#).unwrap();
        assert_eq!(rename.container_id, "abc123");
        assert_eq!(rename.new_name, "web");

        let check: DockerCheckUpdateRequest =
            serde_json::from_str(r#"{"containerId":"abc123"}"#).unwrap();
        assert_eq!(check.container_id.as_deref(), Some("abc123"));
    }
}
