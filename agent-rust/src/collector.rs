use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};
use serde::{Serialize, Deserialize};
use sysinfo::{System, Disks, Networks, Components};


#[derive(Serialize, Deserialize, Debug, Clone, Default)]
pub struct HostInfo {
    pub platform: String,
    pub platform_version: String,
    pub cpu: Vec<String>,
    pub cores: usize, // logical cores (compatible with dashboard)
    pub logical_cores: usize,
    pub physical_cores: usize,
    pub gpu: Vec<String>,
    pub gpu_mem_total: u64,
    pub mem_total: u64,
    pub disk_total: u64,
    pub swap_total: u64,
    pub arch: String,
    pub virtualization: String,
    pub boot_time: i64,
    pub ip: String,
    pub country_code: String,
    pub agent_version: String,
}

#[derive(Serialize, Deserialize, Debug, Clone, Default)]
pub struct DockerContainer {
    pub id: String,
    pub name: String,
    pub image: String,
    pub status: String,
    pub created: String,
    pub ports: String,
}

#[derive(Serialize, Deserialize, Debug, Clone, Default)]
pub struct DockerInfo {
    pub installed: bool,
    pub running: i32,
    pub stopped: i32,
    pub containers: Vec<DockerContainer>,
}

#[derive(Serialize, Deserialize, Debug, Clone, Default)]
pub struct State {
    pub cpu: f64,
    pub cpu_temp: f64,
    pub gpu_temp: f64,
    pub mem_used: u64,
    pub swap_used: u64,
    pub disk_used: u64,
    pub net_in_transfer: u64,
    pub net_out_transfer: u64,
    pub net_in_speed: u64,
    pub net_out_speed: u64,
    pub uptime: u64,
    pub load1: f64,
    pub load5: f64,
    pub load15: f64,
    pub tcp_conn_count: i32,
    pub udp_conn_count: i32,
    pub process_count: i32,
    pub temperatures: Vec<String>,
    pub gpu: f64,
    pub gpu_mem_used: u64,
    pub gpu_mem_total: u64,
    pub gpu_power: f64,
    pub docker: DockerInfo,
}

pub struct Collector {
    sys: System,
    disks: Disks,
    networks: Networks,
    components: Components,
    
    // Cached values and time tracking
    last_net_rx: u64,
    last_net_tx: u64,
    last_net_time: Instant,
    
    cached_host_info: Arc<Mutex<Option<HostInfo>>>,
    
    last_gpu_usage: f64,
    last_gpu_mem_used: u64,
    last_gpu_power: f64,
    last_gpu_temp: f64,
    last_gpu_time: Instant,
}

impl Collector {
    pub fn new() -> Self {
        let mut sys = System::new_all();
        sys.refresh_all();
        
        let disks = Disks::new_with_refreshed_list();
        let networks = Networks::new_with_refreshed_list();
        let components = Components::new_with_refreshed_list();

        // Calculate initial network base
        let mut total_rx = 0;
        let mut total_tx = 0;
        for (_, interface) in &networks {
            total_rx += interface.received();
            total_tx += interface.transmitted();
        }

        Collector {
            sys,
            disks,
            networks,
            components,
            last_net_rx: total_rx,
            last_net_tx: total_tx,
            last_net_time: Instant::now(),
            cached_host_info: Arc::new(Mutex::new(None)),
            last_gpu_usage: 0.0,
            last_gpu_mem_used: 0,
            last_gpu_power: 0.0,
            last_gpu_temp: 0.0,
            last_gpu_time: Instant::now() - Duration::from_secs(3600), // Ensure immediate first run
        }
    }

    pub async fn collect_host_info(&mut self, version: &str) -> HostInfo {
        {
            let cache = self.cached_host_info.lock().unwrap();
            if let Some(ref info) = *cache {
                return info.clone();
            }
        }

        self.sys.refresh_all();
        
        let platform = System::name().unwrap_or_else(|| std::env::consts::OS.to_string());
        let platform_version = System::os_version().unwrap_or_default();
        let arch = System::cpu_arch().unwrap_or_else(|| std::env::consts::ARCH.to_string());
        
        let logical_cores = self.sys.cpus().len();
        let physical_cores = self.sys.physical_core_count().unwrap_or(logical_cores);
        
        let mut cpu_models = Vec::new();
        if let Some(first_cpu) = self.sys.cpus().first() {
            let model = format!("{} {} Core(s)", first_cpu.vendor_id().trim(), first_cpu.brand().trim());
            cpu_models.push(model);
        } else {
            cpu_models.push(format!("Unknown CPU {} Core(s)", physical_cores));
        }

        let mem_total = self.sys.total_memory();
        let swap_total = self.sys.total_swap();

        // Disks total
        self.disks.refresh_list();
        let mut disk_total = 0;
        for disk in &self.disks {
            disk_total += disk.total_space();
        }

        let boot_time = System::boot_time() as i64;
        let ip = get_public_ip().await;

        // GPU detection
        let (gpu_models, gpu_mem_total) = self.collect_gpu_metadata();

        let host_info = HostInfo {
            platform,
            platform_version,
            cpu: cpu_models,
            cores: logical_cores,
            logical_cores,
            physical_cores,
            gpu: gpu_models,
            gpu_mem_total,
            mem_total,
            disk_total,
            swap_total,
            arch,
            virtualization: String::new(), // Optional
            boot_time,
            ip,
            country_code: String::new(),
            agent_version: version.to_string(),
        };

        {
            let mut cache = self.cached_host_info.lock().unwrap();
            *cache = Some(host_info.clone());
        }
        
        host_info
    }

    pub fn collect_state(&mut self) -> State {
        // Refresh CPU, Memory, Components
        self.sys.refresh_cpu_all();
        self.sys.refresh_memory();
        self.components.refresh_list();

        let cpu = self.sys.global_cpu_usage() as f64;
        let mem_used = self.sys.used_memory();
        let swap_used = self.sys.used_swap();

        // Disk usage
        self.disks.refresh_list();
        let mut disk_used = 0;
        for disk in &self.disks {
            disk_used += disk.total_space() - disk.available_space();
        }

        // Networks speeds
        self.networks.refresh_list();
        let mut current_rx = 0;
        let mut current_tx = 0;
        for (_, interface) in &self.networks {
            current_rx += interface.received();
            current_tx += interface.transmitted();
        }

        let now = Instant::now();
        let elapsed = now.duration_since(self.last_net_time).as_secs_f64();
        
        let mut net_in_speed = 0;
        let mut net_out_speed = 0;
        
        if elapsed > 0.0 {
            if current_rx >= self.last_net_rx {
                net_in_speed = ((current_rx - self.last_net_rx) as f64 / elapsed) as u64;
            }
            if current_tx >= self.last_net_tx {
                net_out_speed = ((current_tx - self.last_net_tx) as f64 / elapsed) as u64;
            }
        }
        
        self.last_net_rx = current_rx;
        self.last_net_tx = current_tx;
        self.last_net_time = now;

        let uptime = System::uptime();

        // Load average (Windows simulated, Unix native)
        let (load1, load5, load15) = if cfg!(target_os = "windows") {
            let cpu_count = self.sys.cpus().len() as f64;
            let simulated_load = (cpu / 100.0) * cpu_count;
            (simulated_load, simulated_load, simulated_load)
        } else {
            let load_avg = System::load_average();
            (load_avg.one, load_avg.five, load_avg.fifteen)
        };

        // TCP & UDP connection counts
        let (tcp_conn_count, udp_conn_count) = get_conn_counts();
        let process_count = self.sys.processes().len() as i32;

        // Temperatures
        let cpu_temp = self.collect_cpu_temperature();

        // GPU metrics
        let (gpu, gpu_mem_used, gpu_power, gpu_temp) = self.collect_gpu_state();

        let gpu_mem_total = self.cached_host_info.lock().unwrap()
            .as_ref()
            .map(|info| info.gpu_mem_total)
            .unwrap_or(0);

        State {
            cpu,
            cpu_temp,
            gpu_temp,
            mem_used,
            swap_used,
            disk_used,
            net_in_transfer: current_rx,
            net_out_transfer: current_tx,
            net_in_speed,
            net_out_speed,
            uptime,
            load1,
            load5,
            load15,
            tcp_conn_count,
            udp_conn_count,
            process_count,
            temperatures: Vec::new(), // Not critical
            gpu,
            gpu_mem_used,
            gpu_mem_total,
            gpu_power,
            docker: DockerInfo::default(), // Will be populated in main loop asynchronously
        }
    }

    fn collect_cpu_temperature(&self) -> f64 {
        let mut max_temp = 0.0;
        for component in &self.components {
            let name = component.label().to_lowercase();
            if name.contains("cpu") || name.contains("core") || name.contains("k10temp") || name.contains("zen") {
                let t = component.temperature();
                if t as f64 > max_temp && t < 150.0 {
                    max_temp = t as f64;
                }
            }
        }
        
        if max_temp == 0.0 {
            // Fallback to absolute max temp of any sensor
            for component in &self.components {
                let t = component.temperature();
                if t as f64 > max_temp && t < 150.0 {
                    max_temp = t as f64;
                }
            }
        }
        max_temp
    }

    fn collect_gpu_metadata(&self) -> (Vec<String>, u64) {
        // Try nvidia-smi
        if let Ok(output) = std::process::Command::new("nvidia-smi")
            .args(["--query-gpu=name,memory.total", "--format=csv,noheader,nounits"])
            .output() 
        {
            if output.status.success() {
                let text = String::from_utf8_lossy(&output.stdout);
                let mut models = Vec::new();
                let mut total_mem = 0;
                for line in text.lines() {
                    let parts: Vec<&str> = line.split(',').collect();
                    if parts.len() >= 2 {
                        models.push(parts[0].trim().to_string());
                        if let Ok(mem_mib) = parts[1].trim().parse::<u64>() {
                            total_mem += mem_mib * 1024 * 1024; // MiB to bytes
                        }
                    }
                }
                if !models.is_empty() {
                    return (models, total_mem);
                }
            }
        }

        // Windows fallback via PowerShell
        if cfg!(target_os = "windows") {
            let ps_cmd = "Get-CimInstance Win32_VideoController | Select-Object Name, AdapterRAM | ForEach-Object { $_.Name + ',' + $_.AdapterRAM }";
            if let Ok(output) = std::process::Command::new("powershell")
                .args(["-NoProfile", "-Command", ps_cmd])
                .output()
            {
                if output.status.success() {
                    let text = String::from_utf8_lossy(&output.stdout);
                    let mut models = Vec::new();
                    let mut total_mem = 0;
                    for line in text.lines() {
                        let l = line.trim();
                        if l.is_empty() { continue; }
                        let parts: Vec<&str> = l.split(',').collect();
                        if !parts.is_empty() {
                            models.push(parts[0].trim().to_string());
                            if parts.len() >= 2 {
                                if let Ok(bytes) = parts[1].trim().parse::<u64>() {
                                    total_mem += bytes;
                                }
                            }
                        }
                    }
                    if !models.is_empty() {
                        return (models, total_mem);
                    }
                }
            }
        }

        (Vec::new(), 0)
    }

    fn collect_gpu_state(&mut self) -> (f64, u64, f64, f64) {
        let now = Instant::now();
        // Check Nvidia GPU stats via nvidia-smi with a brief caching mechanism (e.g. 3 seconds)
        if now.duration_since(self.last_gpu_time) > Duration::from_millis(3000) {
            if let Ok(output) = std::process::Command::new("nvidia-smi")
                .args(["--query-gpu=utilization.gpu,memory.used,power.draw,temperature.gpu", "--format=csv,noheader,nounits"])
                .output()
            {
                if output.status.success() {
                    let text = String::from_utf8_lossy(&output.stdout);
                    if let Some(line) = text.lines().next() {
                        let parts: Vec<&str> = line.split(',').collect();
                        if parts.len() >= 4 {
                            let usage = parts[0].trim().parse::<f64>().unwrap_or(0.0);
                            let mem_used_mib = parts[1].trim().parse::<u64>().unwrap_or(0);
                            let power = parts[2].trim().parse::<f64>().unwrap_or(0.0);
                            let temp = parts[3].trim().parse::<f64>().unwrap_or(0.0);

                            self.last_gpu_usage = usage;
                            self.last_gpu_mem_used = mem_used_mib * 1024 * 1024;
                            self.last_gpu_power = power;
                            self.last_gpu_temp = temp;
                            self.last_gpu_time = now;
                        }
                    }
                }
            }
        }

        (self.last_gpu_usage, self.last_gpu_mem_used, self.last_gpu_power, self.last_gpu_temp)
    }
}

async fn get_public_ip() -> String {
    let endpoints = vec![
        "http://ip.sb",
        "https://api.ipify.org",
        "https://icanhazip.com",
    ];
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(3))
        .build()
        .unwrap_or_default();

    for endpoint in endpoints {
        if let Ok(resp) = client.get(endpoint).send().await {
            if let Ok(text) = resp.text().await {
                let ip = text.trim();
                if !ip.is_empty() {
                    return ip.to_string();
                }
            }
        }
    }
    String::new()
}

fn get_conn_counts() -> (i32, i32) {
    if cfg!(target_os = "windows") {
        if let Ok(output) = std::process::Command::new("netstat")
            .arg("-an")
            .output()
        {
            let text = String::from_utf8_lossy(&output.stdout);
            let mut tcp = 0;
            let mut udp = 0;
            for line in text.lines() {
                let l = line.trim();
                if l.starts_with("TCP") {
                    tcp += 1;
                } else if l.starts_with("UDP") {
                    udp += 1;
                }
            }
            return (tcp, udp);
        }
    } else {
        // Read /proc/net/tcp & /proc/net/udp
        let tcp_cnt = std::fs::read_to_string("/proc/net/tcp").map(|s| s.lines().count() as i32 - 1).unwrap_or(0)
            + std::fs::read_to_string("/proc/net/tcp6").map(|s| s.lines().count() as i32 - 1).unwrap_or(0);
        let udp_cnt = std::fs::read_to_string("/proc/net/udp").map(|s| s.lines().count() as i32 - 1).unwrap_or(0)
            + std::fs::read_to_string("/proc/net/udp6").map(|s| s.lines().count() as i32 - 1).unwrap_or(0);
        return (tcp_cnt, udp_cnt);
    }
    (0, 0)
}
