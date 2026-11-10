use crate::protocol::NetworkQualityProbeResponse;
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use sysinfo::{Components, Disks, Networks, System};

/// 磁盘用量刷新间隔。
///
/// 磁盘占用在秒级尺度上几乎不变，但 `Disks::refresh_list()` 要解析
/// `/proc/mounts`（本机 65 条 / 25KB，~0.2ms）并对每个挂载点 statvfs。
/// 按 1.5 秒上报频率算，这是纯浪费；15 秒足够让仪表盘的磁盘曲线看起来连续。
const DISK_REFRESH_INTERVAL: Duration = Duration::from_secs(15);

/// 连接数刷新间隔。
///
/// Linux 上 `get_conn_counts()` 要读 /proc/net/tcp + tcp6 + udp + udp6 并逐行计数，
/// 实测 /proc/net/tcp 单次 ~1.36ms CPU（85KB / 569 连接），是本采集路径最贵的一项，
/// 且成本随宿主机 socket 数线性增长。连接数本身是「瞬时快照类」指标，
/// 10 秒粒度对监控用途没有损失（面板本来也只用来判断"有没有连接异常"）。
const CONN_COUNT_REFRESH_INTERVAL: Duration = Duration::from_secs(10);

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
    pub country: String,
    pub region: String,
    pub city: String,
    pub location: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub latitude: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub longitude: Option<f64>,
    pub org: String,
    pub isp: String,
    pub asn: String,
    pub timezone: String,
    pub geo_source: String,
    pub agent_version: String,
}

#[derive(Debug, Clone, Default)]
struct PublicGeo {
    ip: String,
    city: String,
    region: String,
    country_code: String,
    country: String,
    latitude: Option<f64>,
    longitude: Option<f64>,
    org: String,
    asn: String,
    timezone: String,
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
    pub cpu: f64,    pub cpu_temp: f64,
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
    #[serde(skip_serializing_if = "Option::is_none")]
    pub network_quality: Option<NetworkQualityProbeResponse>,
    /// Agent 自监控指标。面板排障时先看自己再怀疑被监控对象。
    ///
    /// 与 `State` 的其它字段不同，这些数不是"被监控主机"的状态，而是
    /// **Agent 自己的运行时状态**；它们只能由 Agent 内部填，采集器留空。
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub self_metrics: Option<AgentSelfMetrics>,
}

/// Agent 自身运行时指标。
///
/// 为什么需要：本项目曾靠外部 perf/strace 才定位到采集热点在
/// `/proc/net/tcp`；若 Agent 自己上报这些数，排障时不必先怀疑"是不是面板/网络"。
#[derive(Serialize, Deserialize, Debug, Clone, Default)]
pub struct AgentSelfMetrics {
    /// 低优先级队列因拥塞丢弃的消息数（累计）。
    pub dropped_low: u64,
    /// WebSocket 重连次数（累计）。
    pub reconnect_count: u64,
    /// 最近一次 collect_state() 的耗时（微秒）。
    pub collect_state_us: u64,
    /// 当前出站队列积压（high/normal/low 三档之和的近似值）。
    pub outbound_backlog: u64,
    /// Agent 自身常驻内存（字节；Linux 取 VmRSS）。
    pub rss_bytes: u64,
    /// Agent 自身 CPU 使用率（%，单核口径，两次采样之间的增量）。
    pub cpu_percent: f64,
    /// 离线缓冲里当前待补发的条数。
    #[serde(default)]
    pub offline_buffered: u64,
    /// 离线缓冲因容量上限挤掉的条数（累计）。
    #[serde(default)]
    pub offline_dropped: u64,
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

    /// 磁盘用量缓存。用「总量-可用」覆盖所有已挂载块设备，需要先 refresh_list()
    /// 解析 /proc/mounts 并对每个挂载点做 statvfs，属于毫秒级、且**在 1.5 秒
    /// 尺度上几乎不变**的指标，没必要每次采样都重算。
    last_disk_used: u64,
    last_disk_refresh: Instant,

    /// 连接数缓存。Linux 上要读 /proc/net/tcp{,6} 与 /proc/net/udp{,6} 四个文件
    /// 并逐行计数；实测 /proc/net/tcp 单次约 1.36ms CPU（85KB / 569 连接），
    /// 是采集路径上最贵的一项，且随宿主机连接数线性增长。
    last_conn_counts: (i32, i32),
    last_conn_refresh: Instant,

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

        // Calculate initial network base from external-facing interfaces only.
        let (total_rx, total_tx) =
            aggregate_network_totals((&networks).into_iter().map(|(name, interface)| {
                (
                    name.as_str(),
                    interface.total_received(),
                    interface.total_transmitted(),
                )
            }));

        Collector {
            sys,
            disks,
            networks,
            components,
            last_net_rx: total_rx,
            last_net_tx: total_tx,
            last_net_time: Instant::now(),
            // 首次采样立即全量刷新，结果缓存供后续周期复用。
            last_disk_used: 0,
            last_disk_refresh: Instant::now()
                .checked_sub(DISK_REFRESH_INTERVAL)
                .unwrap_or_else(Instant::now),
            last_conn_counts: (0, 0),
            last_conn_refresh: Instant::now()
                .checked_sub(CONN_COUNT_REFRESH_INTERVAL)
                .unwrap_or_else(Instant::now),
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
            cpu_models.push(format_cpu_model(
                first_cpu.vendor_id(),
                first_cpu.brand(),
                physical_cores,
            ));
        } else {
            cpu_models.push(format!("Unknown CPU {} Core(s)", physical_cores));
        }

        let mem_total = self.sys.total_memory();
        let swap_total = self.sys.total_swap();

        // Disks total
        self.disks.refresh_list();
        let mut disk_total = 0;
        let mut seen_devices = Vec::new();
        for disk in &self.disks {
            let fs = disk.file_system().to_string_lossy().to_lowercase();
            let mount_point = disk.mount_point();
            let is_root = mount_point == Path::new("/") || mount_point.to_string_lossy() == "/";
            if !is_root
                && (fs == "tmpfs"
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
                    || fs == "iso9660")
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
        let public_geo = get_public_geo().await;

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
            ip: public_geo.ip.clone(),
            country_code: public_geo.country_code.clone(),
            country: public_geo.country.clone(),
            region: public_geo.region.clone(),
            city: public_geo.city.clone(),
            location: public_geo.location(),
            latitude: public_geo.latitude,
            longitude: public_geo.longitude,
            org: public_geo.org.clone(),
            isp: public_geo.org.clone(),
            asn: public_geo.asn.clone(),
            timezone: public_geo.timezone.clone(),
            geo_source: public_geo.source(),
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
        // components.refresh_list() 会重新枚举 /sys/class/hwmon 并逐个建路径，
        // 但温度本身已有 5 秒缓存（见 collect_temperature_readings）。
        // 这里跟着温度缓存一起走：不到期就不重新枚举。
        if self.last_temperature_time.elapsed() >= Duration::from_millis(5000) {
            self.components.refresh_list();
        }

        let cpu = self.sys.global_cpu_usage() as f64;
        let mem_used = self.sys.used_memory();
        let swap_used = self.sys.used_swap();

        let disk_used = self.collect_disk_used();

        // Networks speeds
        self.networks.refresh();
        let (total_rx, total_tx) =
            aggregate_network_totals((&self.networks).into_iter().map(|(name, interface)| {
                (
                    name.as_str(),
                    interface.total_received(),
                    interface.total_transmitted(),
                )
            }));

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
        let (tcp_conn_count, udp_conn_count) = self.collect_conn_counts_cached();
        let process_count = self.collect_process_count();

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
            network_quality: None,
            // 自监控指标由上报循环填充（需要队列/重连计数，采集器看不到）。
            self_metrics: None,
        }
    }

    fn collect_conn_counts(&self) -> (i32, i32) {
        if cfg!(target_os = "windows") {
            self.refresh_windows_conn_counts();
            return self.cached_windows_conn_counts();
        }

        get_conn_counts()
    }

    /// 带缓存的磁盘用量。
    ///
    /// 口径与旧实现一致：跳过 tmpfs/overlay/cgroup 等虚拟文件系统，
    /// 同一设备被多次挂载时只算一次（避免 btrfs 子卷或 bind mount 重复计入）。
    fn collect_disk_used(&mut self) -> u64 {
        if self.last_disk_refresh.elapsed() < DISK_REFRESH_INTERVAL {
            return self.last_disk_used;
        }

        self.disks.refresh_list();
        let mut disk_used = 0;
        let mut seen_devices = Vec::new();
        for disk in &self.disks {
            let fs = disk.file_system().to_string_lossy().to_lowercase();
            let mount_point = disk.mount_point();
            let is_root = mount_point == Path::new("/") || mount_point.to_string_lossy() == "/";
            if !is_root
                && (fs == "tmpfs"
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
                    || fs == "iso9660")
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

        self.last_disk_used = disk_used;
        self.last_disk_refresh = Instant::now();
        disk_used
    }

    /// 带缓存的连接数。
    fn collect_conn_counts_cached(&mut self) -> (i32, i32) {
        if !cfg!(target_os = "windows") && self.last_conn_refresh.elapsed() < CONN_COUNT_REFRESH_INTERVAL
        {
            return self.last_conn_counts;
        }

        let counts = self.collect_conn_counts();
        self.last_conn_counts = counts;
        self.last_conn_refresh = Instant::now();
        counts
    }

    /// 进程数（不含线程）。
    ///
    /// 不能直接 `self.sys.processes().len()`：sysinfo 0.31 的 `refresh_procs`
    /// 会递归 `/proc/<pid>/task/*`，把**线程**也塞进同一个 `process_list`
    /// （见 sysinfo-0.31.4 `src/unix/linux/process.rs` 的 `refresh_procs`），
    /// 于是返回值是「进程 + 线程」总数。实测某台 332 个 PID 的机器报 1292，
    /// 偏大近 4 倍，面板上的「进程数」和基于它的告警全部失真。
    ///
    /// 修正方式：用 sysinfo 自带的口径过滤 —— `thread_kind()` 对 `/proc/<pid>/task`
    /// 下的条目返回 `Some(ThreadKind::Userland)`，对真正的进程返回 `None`（Linux 专有）。
    /// 注意**不能**把 `Some(ThreadKind::Kernel)` 也滤掉：内核线程（PF_KTHREAD）
    /// 在 `/proc` 里是正常 PID，`ls /proc` 数得到，过滤它会反而少数几十个。
    /// 不用「数 /proc 目录」是因为那要多一次 readdir；这里顺带复用已有的
    /// 刷新结果，零额外系统调用。
    fn collect_process_count(&self) -> i32 {
        #[cfg(target_os = "linux")]
        {
            self.sys
                .processes()
                .values()
                .filter(|process| !matches!(process.thread_kind(), Some(sysinfo::ThreadKind::Userland)))
                .count() as i32
        }
        #[cfg(not(target_os = "linux"))]
        {
            // 其它平台 sysinfo 不会把线程混进来（thread_kind 恒为 None），
            // 直接用列表长度即可。
            self.sys.processes().len() as i32
        }
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

async fn get_public_geo() -> PublicGeo {
    let endpoints = vec![
        "https://64.ipcheck.ing/geo",
        "https://4.ipcheck.ing/geo",
        "https://6.ipcheck.ing/geo",
    ];
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(5))
        .build()
        .unwrap_or_default();

    for endpoint in endpoints {
        if let Ok(resp) = client.get(endpoint).send().await {
            if resp.status().is_success() {
                if let Ok(text) = resp.text().await {
                    let geo = parse_ipcheck_geo(&text);
                    if geo.has_valid_ip() {
                        return geo;
                    }
                }
            }
        }
    }

    PublicGeo {
        ip: get_public_ip().await,
        ..PublicGeo::default()
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

fn parse_ipcheck_geo(text: &str) -> PublicGeo {
    let mut geo = PublicGeo::default();

    for line in text.lines() {
        let Some((raw_key, raw_value)) = line.split_once(':') else {
            continue;
        };
        let key = raw_key.trim().to_ascii_lowercase();
        let value = normalize_geo_value(raw_value);
        if value.is_empty() {
            continue;
        }

        match key.as_str() {
            "ip" => geo.ip = value,
            "city" => geo.city = value,
            "region" => geo.region = value,
            "country" => {
                geo.country_code = value.to_ascii_lowercase();
                geo.country = value;
            }
            "latitude" => geo.latitude = value.parse::<f64>().ok(),
            "longitude" => geo.longitude = value.parse::<f64>().ok(),
            "org" => geo.org = value,
            "timezone" => geo.timezone = value,
            "asn" => geo.asn = value,
            _ => {}
        }
    }

    geo
}

fn normalize_geo_value(value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() || trimmed.eq_ignore_ascii_case("n/a") {
        String::new()
    } else {
        trimmed.to_string()
    }
}

impl PublicGeo {
    fn has_valid_ip(&self) -> bool {
        self.ip.parse::<std::net::IpAddr>().is_ok()
    }

    fn has_coordinates(&self) -> bool {
        self.latitude.is_some() && self.longitude.is_some()
    }

    fn location(&self) -> String {
        let mut parts = Vec::new();
        for value in [&self.city, &self.region, &self.country] {
            if !value.is_empty() && !parts.iter().any(|part: &&String| *part == value) {
                parts.push(value);
            }
        }
        parts
            .into_iter()
            .map(String::as_str)
            .collect::<Vec<_>>()
            .join(", ")
    }

    fn source(&self) -> String {
        if self.has_coordinates() {
            "ipcheck".to_string()
        } else {
            String::new()
        }
    }
}

fn current_epoch_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_millis() as u64)
        .unwrap_or(0)
}

fn format_cpu_model(vendor: &str, brand: &str, physical_cores: usize) -> String {
    let brand = brand.trim();
    if !brand.is_empty() {
        return brand.to_string();
    }
    let vendor = vendor.trim();
    if !vendor.is_empty() {
        return format!("{} {} Core(s)", vendor, physical_cores);
    }
    format!("Unknown CPU {} Core(s)", physical_cores)
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

fn aggregate_network_totals<I, S>(interfaces: I) -> (u64, u64)
where
    I: IntoIterator<Item = (S, u64, u64)>,
    S: AsRef<str>,
{
    interfaces
        .into_iter()
        .filter(|(name, _, _)| should_count_network_interface(name.as_ref()))
        .fold((0, 0), |(rx_total, tx_total), (_, rx, tx)| {
            (rx_total.saturating_add(rx), tx_total.saturating_add(tx))
        })
}

fn should_count_network_interface(name: &str) -> bool {
    let normalized = name.trim().to_ascii_lowercase();
    if normalized.is_empty() {
        return false;
    }

    let base_name = normalized.split('@').next().unwrap_or(&normalized);
    if base_name == "lo" || base_name.starts_with("lo:") || base_name.contains("loopback") {
        return false;
    }

    const VIRTUAL_PREFIXES: &[&str] = &[
        "docker",
        "br-",
        "veth",
        "virbr",
        "vmnet",
        "vboxnet",
        "zt",
        "tailscale",
        "tun",
        "tap",
        "wg",
        "flannel",
        "cni",
        "kube",
        "calico",
        "cali",
        "podman",
        "nerdctl",
        "containerd",
        "ifb",
        "ip6tnl",
        "sit",
        "gre",
        "gretap",
        "erspan",
        "dummy",
    ];
    if VIRTUAL_PREFIXES
        .iter()
        .any(|prefix| base_name.starts_with(prefix))
    {
        return false;
    }

    const VIRTUAL_MARKERS: &[&str] = &[
        "vethernet",
        "hyper-v",
        "virtualbox",
        "vmware",
        "wsl",
        "docker",
        "tailscale",
        "zerotier",
        "npcap",
        "tap-windows",
        "wireguard",
    ];
    !VIRTUAL_MARKERS
        .iter()
        .any(|marker| normalized.contains(marker))
}

#[cfg(test)]
mod tests {
    use super::{
        aggregate_network_totals, format_cpu_model, parse_ipcheck_geo,
        should_count_network_interface, Collector, DISK_REFRESH_INTERVAL,
    };
    // CONN_COUNT_REFRESH_INTERVAL 只被下方的 Linux 专用用例使用；
    // 这里单独 if-import，否则 Windows 构建（CI 带 -D warnings）会报 unused import。
    #[cfg(target_os = "linux")]
    use super::CONN_COUNT_REFRESH_INTERVAL;
    use std::time::Instant;

    #[test]
    fn format_cpu_model_prefers_brand_only() {
        let model = format_cpu_model("GenuineIntel", "13th Gen Intel(R) Core(TM) i7-13700HX", 16);

        assert_eq!(model, "13th Gen Intel(R) Core(TM) i7-13700HX");
    }

    #[test]
    fn format_cpu_model_handles_missing_vendor_or_brand() {
        assert_eq!(
            format_cpu_model("", "13th Gen Intel(R) Core(TM) i7-13700HX", 16),
            "13th Gen Intel(R) Core(TM) i7-13700HX"
        );
        assert_eq!(
            format_cpu_model("GenuineIntel", "", 16),
            "GenuineIntel 16 Core(s)"
        );
        assert_eq!(format_cpu_model("", "", 16), "Unknown CPU 16 Core(s)");
    }

    // ---- 采集缓存（issue #72）----

    /// 连接数在缓存窗口内必须返回同一份结果，不能每次都去读 /proc/net/tcp。
    #[cfg(target_os = "linux")]
    #[test]
    fn conn_counts_are_cached_within_refresh_interval() {
        let mut collector = Collector::new();
        // 首次调用填充缓存（构造时把 last_conn_refresh 回退了一个间隔）。
        let first = collector.collect_conn_counts_cached();
        let second = collector.collect_conn_counts_cached();
        assert_eq!(first, second, "缓存窗口内两次调用结果应当一致");

        // 直接改内部时间戳模拟"缓存已过期"，验证确实会重新采样。
        collector.last_conn_refresh = Instant::now()
            .checked_sub(CONN_COUNT_REFRESH_INTERVAL)
            .expect("subtract");
        let refreshed = collector.collect_conn_counts_cached();
        assert!(
            refreshed.0 > 0,
            "刷新后 TCP 连接数应大于 0（本机至少有自己的 socket），got {refreshed:?}"
        );
    }

    /// 磁盘用量同样走缓存：窗口内返回值不变。
    #[test]
    fn disk_usage_is_cached_within_refresh_interval() {
        let mut collector = Collector::new();
        let first = collector.collect_disk_used();
        let second = collector.collect_disk_used();
        assert_eq!(first, second);
        assert!(first > 0, "根分区已用空间不应为 0");
    }

    /// 缓存过期后必须重新计算，且结果仍是一个合理值。
    #[test]
    fn disk_usage_refreshes_after_interval() {
        let mut collector = Collector::new();
        let first = collector.collect_disk_used();
        collector.last_disk_refresh = Instant::now()
            .checked_sub(DISK_REFRESH_INTERVAL)
            .expect("subtract");
        let refreshed = collector.collect_disk_used();
        assert!(refreshed > 0);
        // 两次之间最多相差一个刚写入的文件，量级应保持一致。
        assert_eq!(first, refreshed);
    }

    #[test]
    fn aggregate_network_totals_skips_container_and_loopback_interfaces() {
        let interfaces = [
            ("lo", 91_071_099_488, 91_071_099_488),
            ("enp0s6", 4_907_967_079_965, 4_873_139_838_424),
            ("docker0", 0, 0),
            ("br-266073283c16", 4_817_677_145_747, 4_835_632_200_041),
            ("veth2ecfbcc@if2", 415_346_558, 125_317_817),
        ];

        assert_eq!(
            aggregate_network_totals(interfaces),
            (4_907_967_079_965, 4_873_139_838_424)
        );
    }

    #[test]
    fn network_interface_filter_keeps_common_external_interfaces() {
        for name in ["eth0", "enp0s6", "ens18", "wlan0", "Wi-Fi", "Ethernet"] {
            assert!(should_count_network_interface(name), "{name}");
        }
    }

    #[test]
    fn network_interface_filter_rejects_common_virtual_interfaces() {
        for name in [
            "lo",
            "docker0",
            "br-a6a0746c2495",
            "vethd0508ff@if2",
            "virbr0",
            "tailscale0",
            "wg0",
            "tun0",
            "tap0",
            "cni0",
            "flannel.1",
            "vEthernet (WSL)",
            "VMware Network Adapter VMnet8",
            "VirtualBox Host-Only Network",
            "Npcap Loopback Adapter",
        ] {
            assert!(!should_count_network_interface(name), "{name}");
        }
    }

    #[test]
    fn parse_ipcheck_geo_extracts_stable_coordinates() {
        let geo = parse_ipcheck_geo(
            r#"IP: 64.181.246.5
City: San Jose
Region: California
Country: US
Flag: 🇺🇸
Latitude: 37.33939
Longitude: -121.89496
Org: Oracle Corporation
Timezone: America/Los_Angeles
ASN: AS31898
RiskScore: 0
TrustedBot: false"#,
        );

        assert_eq!(geo.ip, "64.181.246.5");
        assert_eq!(geo.city, "San Jose");
        assert_eq!(geo.region, "California");
        assert_eq!(geo.country_code, "us");
        assert_eq!(geo.latitude, Some(37.33939));
        assert_eq!(geo.longitude, Some(-121.89496));
        assert_eq!(geo.org, "Oracle Corporation");
        assert_eq!(geo.asn, "AS31898");
        assert_eq!(geo.timezone, "America/Los_Angeles");
        assert_eq!(geo.location(), "San Jose, California, US");
        assert_eq!(geo.source(), "ipcheck");
    }

    // ---- process_count 口径（回归 issue #73）----

    /// 进程数必须与 /proc 里的 PID 数一致，不能把线程算进去。
    ///
    /// 修复前 sysinfo 的 process_list 同时含 /proc/<pid> 与 /proc/<pid>/task/*，
    /// 实测在 332 个 PID 的机器上报 1292（偏大约 4 倍）。
    #[cfg(target_os = "linux")]
    #[test]
    fn process_count_matches_proc_pid_count() {
        let collector = Collector::new();
        let reported = collector.collect_process_count();

        // 直接数 /proc 下的纯数字目录：这是"真实进程数"的权威口径。
        let expected = std::fs::read_dir("/proc")
            .expect("read /proc")
            .filter_map(Result::ok)
            .filter(|entry| {
                entry
                    .file_name()
                    .to_str()
                    .map(|name| name.bytes().all(|byte| byte.is_ascii_digit()))
                    .unwrap_or(false)
            })
            .count() as i32;

        // 允许少量偏差：两次采样之间可能有进程创建/退出。
        let delta = (reported - expected).abs();
        assert!(
            delta <= 5,
            "process_count={reported} 与 /proc 实际 PID 数 {expected} 相差 {delta}，\
             疑似又把线程算进来了"
        );
    }

    /// 结果必须是"进程数"量级，不能是"进程+线程"总量。
    ///
    /// 不做绝对数值断言（机器差异大），只守住一个宽松上界：线程总数通常
    /// 是进程数的数倍，若实现退回旧口径会明显超出。
    #[cfg(target_os = "linux")]
    #[test]
    fn process_count_is_far_below_total_task_count() {
        let collector = Collector::new();
        let reported = collector.collect_process_count();
        let task_count = collector.sys.processes().len() as i32;

        assert!(reported > 0, "进程数不应为 0");
        assert!(
            reported <= task_count,
            "process_count={reported} 不应超过 process_list 长度 {task_count}"
        );
        // 只有极简环境（几乎没有多线程进程）才会让两者相等；
        // 正常系统上线程数远多于进程数。这里不强制 < task_count，
        // 因为容器里 PID 1 独占时可能确实接近，避免误报。
    }
}
