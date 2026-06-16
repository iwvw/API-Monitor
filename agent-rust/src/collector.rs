use serde::{Deserialize, Serialize};
use std::fs;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use sysinfo::{Components, Disks, Networks, System};

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
pub struct TemperatureReading {
    pub name: String,
    pub temperature: f64,
}

#[derive(Debug, Clone, Default)]
struct WindowsSensorSnapshot {
    readings: Vec<TemperatureReading>,
    cpu_power: f64,
    updated_at: Option<Instant>,
    in_flight: bool,
}

#[derive(Debug, Clone, Default)]
struct WindowsConnSnapshot {
    tcp: i32,
    udp: i32,
    updated_at: Option<Instant>,
    in_flight: bool,
}

#[derive(Serialize, Deserialize, Debug, Clone, Default)]
pub struct State {
    pub timestamp_ms: u64,
    pub sequence: u64,
    pub sample_interval_ms: u64,
    pub cpu: f64,
    pub cpu_temp: f64,
    pub cpu_power: f64,
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
    pub temperatures: Vec<TemperatureReading>,
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
    last_cpu_energy_uj: Option<u64>,
    last_cpu_power_time: Option<Instant>,
    last_cpu_power_value: f64,
    last_temperature_readings: Vec<TemperatureReading>,
    last_temperature_time: Instant,
    windows_sensor_snapshot: Arc<Mutex<WindowsSensorSnapshot>>,
    windows_conn_snapshot: Arc<Mutex<WindowsConnSnapshot>>,
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
            total_rx += interface.total_received();
            total_tx += interface.total_transmitted();
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
            last_gpu_time: Instant::now()
                .checked_sub(Duration::from_secs(3600))
                .unwrap_or_else(Instant::now),
            last_cpu_energy_uj: None,
            last_cpu_power_time: None,
            last_cpu_power_value: 0.0,
            last_temperature_readings: Vec::new(),
            last_temperature_time: Instant::now()
                .checked_sub(Duration::from_secs(3600))
                .unwrap_or_else(Instant::now),
            windows_sensor_snapshot: Arc::new(Mutex::new(WindowsSensorSnapshot::default())),
            windows_conn_snapshot: Arc::new(Mutex::new(WindowsConnSnapshot::default())),
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
            let model = format!(
                "{} {} Core(s)",
                first_cpu.vendor_id().trim(),
                first_cpu.brand().trim()
            );
            cpu_models.push(model);
        } else {
            cpu_models.push(format!("Unknown CPU {} Core(s)", physical_cores));
        }

        let mem_total = self.sys.total_memory();
        let swap_total = self.sys.total_swap();

        // Disks total
        self.disks.refresh();
        let mut disk_total = 0;
        let mut seen_devices = Vec::new();
        for disk in &self.disks {
            let fs = disk.file_system().to_string_lossy().to_lowercase();
            if fs == "tmpfs"
                || fs == "overlay"
                || fs == "devtmpfs"
                || fs == "proc"
                || fs == "sysfs"
                || fs == "cgroup"
                || fs == "devpts"
                || fs == "configfs"
                || fs == "debugfs"
                || fs == "tracefs"
                || fs == "hugetlbfs"
                || fs == "mqueue"
                || fs == "pstore"
                || fs == "securityfs"
                || fs == "fusectl"
                || fs == "nsfs"
                || fs == "autofs"
                || fs == "binfmt_misc"
                || fs == "squashfs"
                || fs == "udev"
                || fs == "iso9660"
            {
                continue;
            }
            let device_name = disk.name().to_string_lossy().to_string();
            if device_name.starts_with('/') {
                if seen_devices.contains(&device_name) {
                    continue;
                }
                seen_devices.push(device_name);
            }
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
        self.disks.refresh();
        let mut disk_used = 0;
        let mut seen_devices = Vec::new();
        for disk in &self.disks {
            let fs = disk.file_system().to_string_lossy().to_lowercase();
            if fs == "tmpfs"
                || fs == "overlay"
                || fs == "devtmpfs"
                || fs == "proc"
                || fs == "sysfs"
                || fs == "cgroup"
                || fs == "devpts"
                || fs == "configfs"
                || fs == "debugfs"
                || fs == "tracefs"
                || fs == "hugetlbfs"
                || fs == "mqueue"
                || fs == "pstore"
                || fs == "securityfs"
                || fs == "fusectl"
                || fs == "nsfs"
                || fs == "autofs"
                || fs == "binfmt_misc"
                || fs == "squashfs"
                || fs == "udev"
                || fs == "iso9660"
            {
                continue;
            }
            let device_name = disk.name().to_string_lossy().to_string();
            if device_name.starts_with('/') {
                if seen_devices.contains(&device_name) {
                    continue;
                }
                seen_devices.push(device_name);
            }
            disk_used += disk.total_space() - disk.available_space();
        }

        // Networks speeds
        self.networks.refresh();
        let mut total_rx = 0;
        let mut total_tx = 0;
        for (_, interface) in &self.networks {
            total_rx += interface.total_received();
            total_tx += interface.total_transmitted();
        }

        let now = Instant::now();
        let elapsed = now.duration_since(self.last_net_time).as_secs_f64();

        let mut net_in_speed = 0;
        let mut net_out_speed = 0;

        if elapsed > 0.0 {
            if total_rx >= self.last_net_rx {
                net_in_speed = ((total_rx - self.last_net_rx) as f64 / elapsed) as u64;
            }
            if total_tx >= self.last_net_tx {
                net_out_speed = ((total_tx - self.last_net_tx) as f64 / elapsed) as u64;
            }
        }

        self.last_net_rx = total_rx;
        self.last_net_tx = total_tx;
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
        let (tcp_conn_count, udp_conn_count) = self.collect_conn_counts();
        let process_count = self.sys.processes().len() as i32;

        // Temperatures and CPU package power
        let temperatures = self.collect_temperature_readings();
        let cpu_temp = self.resolve_cpu_temperature(&temperatures);
        let cpu_power = self.collect_cpu_power();

        // GPU metrics
        let (gpu, gpu_mem_used, gpu_power, gpu_temp) = self.collect_gpu_state();

        let gpu_mem_total = self
            .cached_host_info
            .lock()
            .unwrap()
            .as_ref()
            .map(|info| info.gpu_mem_total)
            .unwrap_or(0);

        State {
            timestamp_ms: current_epoch_ms(),
            sequence: 0,
            sample_interval_ms: 0,
            cpu,
            cpu_temp,
            cpu_power,
            gpu_temp,
            mem_used,
            swap_used,
            disk_used,
            net_in_transfer: total_rx,
            net_out_transfer: total_tx,
            net_in_speed,
            net_out_speed,
            uptime,
            load1,
            load5,
            load15,
            tcp_conn_count,
            udp_conn_count,
            process_count,
            temperatures,
            gpu,
            gpu_mem_used,
            gpu_mem_total,
            gpu_power,
            docker: DockerInfo::default(), // Will be populated in main loop asynchronously
        }
    }

    fn collect_conn_counts(&self) -> (i32, i32) {
        if cfg!(target_os = "windows") {
            self.refresh_windows_conn_counts();
            return self.cached_windows_conn_counts();
        }

        get_conn_counts()
    }

    fn cached_windows_conn_counts(&self) -> (i32, i32) {
        self.windows_conn_snapshot
            .lock()
            .map(|snapshot| (snapshot.tcp, snapshot.udp))
            .unwrap_or((0, 0))
    }

    fn refresh_windows_conn_counts(&self) {
        if !cfg!(target_os = "windows") {
            return;
        }

        const WINDOWS_CONN_REFRESH_INTERVAL: Duration = Duration::from_secs(5);

        {
            let Ok(mut snapshot) = self.windows_conn_snapshot.lock() else {
                return;
            };

            if snapshot.in_flight {
                return;
            }

            let is_fresh = snapshot
                .updated_at
                .map(|updated_at| updated_at.elapsed() < WINDOWS_CONN_REFRESH_INTERVAL)
                .unwrap_or(false);
            if is_fresh {
                return;
            }

            snapshot.in_flight = true;
        }

        let snapshot = Arc::clone(&self.windows_conn_snapshot);
        std::thread::spawn(move || {
            let (tcp, udp) = collect_windows_conn_counts();
            if let Ok(mut snapshot) = snapshot.lock() {
                snapshot.tcp = tcp;
                snapshot.udp = udp;
                snapshot.updated_at = Some(Instant::now());
                snapshot.in_flight = false;
            }
        });
    }

    fn collect_temperature_readings(&mut self) -> Vec<TemperatureReading> {
        let now = Instant::now();
        if now.duration_since(self.last_temperature_time) < Duration::from_millis(5000) {
            return self.last_temperature_readings.clone();
        }

        let mut readings = Vec::new();
        for component in &self.components {
            let temperature = component.temperature() as f64;
            if temperature > 0.0 && temperature < 150.0 {
                readings.push(TemperatureReading {
                    name: component.label().to_string(),
                    temperature,
                });
            }
        }

        if cfg!(target_os = "windows") && self.resolve_cpu_temperature(&readings) <= 0.0 {
            self.refresh_windows_sensors(false);
            readings.extend(self.cached_windows_temperature_readings());
        }

        self.last_temperature_readings = readings.clone();
        self.last_temperature_time = now;

        readings
    }

    fn cached_windows_temperature_readings(&self) -> Vec<TemperatureReading> {
        self.windows_sensor_snapshot
            .lock()
            .map(|snapshot| snapshot.readings.clone())
            .unwrap_or_default()
    }

    fn cached_windows_cpu_power(&self) -> f64 {
        self.windows_sensor_snapshot
            .lock()
            .map(|snapshot| snapshot.cpu_power)
            .unwrap_or(0.0)
    }

    fn refresh_windows_sensors(&self, force_blocking: bool) {
        if !cfg!(target_os = "windows") {
            return;
        }

        const WINDOWS_SENSOR_REFRESH_INTERVAL: Duration = Duration::from_secs(30);

        let should_refresh = {
            let Ok(mut snapshot) = self.windows_sensor_snapshot.lock() else {
                return;
            };

            if snapshot.in_flight {
                return;
            }

            let is_fresh = snapshot
                .updated_at
                .map(|updated_at| updated_at.elapsed() < WINDOWS_SENSOR_REFRESH_INTERVAL)
                .unwrap_or(false);
            if !force_blocking && is_fresh {
                return;
            }

            snapshot.in_flight = true;
            force_blocking
        };

        if should_refresh {
            let (readings, cpu_power) = Self::collect_windows_sensors();
            if let Ok(mut snapshot) = self.windows_sensor_snapshot.lock() {
                snapshot.readings = readings;
                snapshot.cpu_power = cpu_power;
                snapshot.updated_at = Some(Instant::now());
                snapshot.in_flight = false;
            }
            return;
        }

        let snapshot = Arc::clone(&self.windows_sensor_snapshot);
        std::thread::spawn(move || {
            let (readings, cpu_power) = Self::collect_windows_sensors();
            if let Ok(mut snapshot) = snapshot.lock() {
                snapshot.readings = readings;
                snapshot.cpu_power = cpu_power;
                snapshot.updated_at = Some(Instant::now());
                snapshot.in_flight = false;
            }
        });
    }

    fn collect_windows_sensors() -> (Vec<TemperatureReading>, f64) {
        let ps_cmd = r#"
$items = @()
foreach ($ns in @('root/OpenHardwareMonitor', 'root/LibreHardwareMonitor')) {
  try {
    $items += Get-CimInstance -Namespace $ns -ClassName Sensor -ErrorAction Stop |
      Where-Object { $_.SensorType -eq 'Temperature' } |
      ForEach-Object { "T`t$($_.Name)`t$($_.Value)" }
    $items += Get-CimInstance -Namespace $ns -ClassName Sensor -ErrorAction Stop |
      Where-Object { $_.SensorType -eq 'Power' } |
      ForEach-Object { "P`t$($_.Name)`t$($_.Value)" }
  } catch {}
}
try {
  $items += Get-CimInstance -Namespace root/WMI -ClassName MSAcpi_ThermalZoneTemperature -ErrorAction Stop |
    ForEach-Object { "T`t$($_.InstanceName)`t$([math]::Round(($_.CurrentTemperature / 10) - 273.15, 1))" }
} catch {}
$items | Where-Object { $_ }
"#;

        let Ok(output) = std::process::Command::new("powershell")
            .args(["-NoProfile", "-NonInteractive", "-Command", ps_cmd])
            .output()
        else {
            return (Vec::new(), 0.0);
        };

        if !output.status.success() {
            return (Vec::new(), 0.0);
        }

        let mut readings = Vec::new();
        let mut ranked_power = Vec::new();

        for line in String::from_utf8_lossy(&output.stdout).lines() {
            let mut parts = line.trim().splitn(3, '\t');
            let Some(kind) = parts.next() else {
                continue;
            };
            let Some(name) = parts.next() else {
                continue;
            };
            let Some(value) = parts.next() else {
                continue;
            };

            match kind {
                "T" => {
                    let Ok(temperature) = value.trim().parse::<f64>() else {
                        continue;
                    };
                    if temperature > 0.0 && temperature < 150.0 {
                        readings.push(TemperatureReading {
                            name: name.trim().to_string(),
                            temperature,
                        });
                    }
                }
                "P" => {
                    let Ok(power) = value.trim().parse::<f64>() else {
                        continue;
                    };
                    if power > 0.0 && power < 1000.0 {
                        ranked_power.push((Self::cpu_power_rank(name), power));
                    }
                }
                _ => {}
            }
        }

        ranked_power.retain(|(rank, _)| *rank > 0);
        ranked_power.sort_by(|a, b| {
            b.0.cmp(&a.0)
                .then_with(|| b.1.partial_cmp(&a.1).unwrap_or(std::cmp::Ordering::Equal))
        });

        (
            readings,
            ranked_power.first().map(|(_, power)| *power).unwrap_or(0.0),
        )
    }

    fn resolve_cpu_temperature(&self, readings: &[TemperatureReading]) -> f64 {
        let mut ranked: Vec<(i32, f64)> = readings
            .iter()
            .map(|reading| {
                (
                    Self::cpu_temperature_rank(&reading.name),
                    reading.temperature,
                )
            })
            .filter(|(rank, temperature)| *rank > 0 && *temperature > 0.0 && *temperature < 150.0)
            .collect();

        ranked.sort_by(|a, b| {
            b.0.cmp(&a.0)
                .then_with(|| b.1.partial_cmp(&a.1).unwrap_or(std::cmp::Ordering::Equal))
        });
        if let Some((_, temperature)) = ranked.first() {
            return *temperature;
        }

        readings
            .iter()
            .filter(|reading| reading.temperature > 0.0 && reading.temperature < 150.0)
            .map(|reading| reading.temperature)
            .fold(0.0, f64::max)
    }

    fn cpu_temperature_rank(name: &str) -> i32 {
        let name = name.to_lowercase();
        if name.contains("gpu")
            || name.contains("nvidia")
            || name.contains("radeon")
            || name.contains("nvme")
            || name.contains("ssd")
            || name.contains("hdd")
            || name.contains("disk")
            || name.contains("drive")
            || name.contains("battery")
            || name.contains("fan")
            || name.contains("ambient")
        {
            return 0;
        }
        if name.contains("package")
            || name.contains("tctl")
            || name.contains("tdie")
            || name.contains("x86_pkg")
        {
            return 5;
        }
        if name.contains("cpu") || name.contains("cpu_thermal") {
            return 4;
        }
        if name.contains("core")
            || name.contains("coretemp")
            || name.contains("k10temp")
            || name.contains("zen")
        {
            return 3;
        }
        if name.contains("thermal") {
            return 1;
        }
        0
    }

    fn collect_cpu_power(&mut self) -> f64 {
        if cfg!(target_os = "windows") {
            self.refresh_windows_sensors(false);
            return self.cached_windows_cpu_power();
        }

        let Some(energy_uj) = Self::read_total_cpu_energy_uj() else {
            self.last_cpu_energy_uj = None;
            self.last_cpu_power_time = None;
            self.last_cpu_power_value = 0.0;
            return 0.0;
        };

        let now = Instant::now();
        let power = match (self.last_cpu_energy_uj, self.last_cpu_power_time) {
            (Some(previous_energy), Some(previous_time)) if energy_uj >= previous_energy => {
                let elapsed = now.duration_since(previous_time).as_secs_f64();
                if elapsed > 0.0 {
                    (energy_uj - previous_energy) as f64 / 1_000_000.0 / elapsed
                } else {
                    0.0
                }
            }
            _ => 0.0,
        };

        self.last_cpu_energy_uj = Some(energy_uj);
        self.last_cpu_power_time = Some(now);

        let power = if power.is_finite() && power > 0.0 && power < 1000.0 {
            power
        } else {
            0.0
        };
        self.last_cpu_power_value = power;
        power
    }

    fn cpu_power_rank(name: &str) -> i32 {
        let name = name.to_lowercase();
        if name.contains("gpu")
            || name.contains("nvidia")
            || name.contains("radeon")
            || name.contains("battery")
            || name.contains("adapter")
        {
            return 0;
        }
        if name.contains("package") {
            return 5;
        }
        if name.contains("cpu") || name.contains("processor") {
            return 4;
        }
        if name.contains("core") {
            return 3;
        }
        0
    }

    fn read_total_cpu_energy_uj() -> Option<u64> {
        let base = Path::new("/sys/class/powercap");
        let entries = fs::read_dir(base).ok()?;
        let mut package_total = 0_u64;
        let mut package_count = 0_u64;
        let mut fallback_total = 0_u64;
        let mut fallback_count = 0_u64;

        for entry in entries.flatten() {
            let path = entry.path();
            if !path.is_dir() {
                continue;
            }

            let Some(energy) = Self::read_u64(path.join("energy_uj")) else {
                continue;
            };

            let name = fs::read_to_string(path.join("name"))
                .unwrap_or_default()
                .to_lowercase();
            let file_name = entry.file_name().to_string_lossy().to_string();

            if name.contains("package") {
                package_total = package_total.saturating_add(energy);
                package_count += 1;
            } else if file_name.starts_with("intel-rapl") && file_name.matches(':').count() <= 1 {
                fallback_total = fallback_total.saturating_add(energy);
                fallback_count += 1;
            }
        }

        if package_count > 0 {
            Some(package_total)
        } else if fallback_count > 0 {
            Some(fallback_total)
        } else {
            None
        }
    }

    fn read_u64(path: impl AsRef<Path>) -> Option<u64> {
        fs::read_to_string(path).ok()?.trim().parse::<u64>().ok()
    }

    fn collect_gpu_metadata(&self) -> (Vec<String>, u64) {
        // Try nvidia-smi
        if let Ok(output) = std::process::Command::new("nvidia-smi")
            .args([
                "--query-gpu=name,memory.total",
                "--format=csv,noheader,nounits",
            ])
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
                        if l.is_empty() {
                            continue;
                        }
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
                .args([
                    "--query-gpu=utilization.gpu,memory.used,power.draw,temperature.gpu",
                    "--format=csv,noheader,nounits",
                ])
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

        (
            self.last_gpu_usage,
            self.last_gpu_mem_used,
            self.last_gpu_power,
            self.last_gpu_temp,
        )
    }
}

async fn get_public_ip() -> String {
    let endpoints = vec![
        "https://ip.sb",
        "https://api.ipify.org",
        "https://icanhazip.com",
    ];
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(3))
        .build()
        .unwrap_or_default();

    for endpoint in endpoints {
        if let Ok(resp) = client.get(endpoint).send().await {
            if resp.status().is_success() {
                if let Ok(text) = resp.text().await {
                    let ip = text.trim();
                    if ip.parse::<std::net::IpAddr>().is_ok() {
                        return ip.to_string();
                    }
                }
            }
        }
    }
    String::new()
}

fn current_epoch_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_millis() as u64)
        .unwrap_or(0)
}

fn collect_windows_conn_counts() -> (i32, i32) {
    if let Ok(output) = std::process::Command::new("netstat").arg("-an").output() {
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
    (0, 0)
}

fn get_conn_counts() -> (i32, i32) {
    let tcp_cnt = std::fs::read_to_string("/proc/net/tcp")
        .map(|s| s.lines().count() as i32 - 1)
        .unwrap_or(0)
        + std::fs::read_to_string("/proc/net/tcp6")
            .map(|s| s.lines().count() as i32 - 1)
            .unwrap_or(0);
    let udp_cnt = std::fs::read_to_string("/proc/net/udp")
        .map(|s| s.lines().count() as i32 - 1)
        .unwrap_or(0)
        + std::fs::read_to_string("/proc/net/udp6")
            .map(|s| s.lines().count() as i32 - 1)
            .unwrap_or(0);
    (tcp_cnt, udp_cnt)
}
