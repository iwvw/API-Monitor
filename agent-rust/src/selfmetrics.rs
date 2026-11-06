//! Agent 自身运行时指标采集。
//!
//! 为什么单独成模块：这些数不属于「被监控主机」的状态，而是 **Agent 自己**
//! 的运行时表现（内存、CPU、队列丢弃、重连次数、采集耗时）。混进 collector
//! 会让「主机指标」和「Agent 指标」的口径互相污染；单独放着也便于加测试。
//!
//! 用途：面板上出现「数据延迟/断点」时，先看 Agent 自己是否过载或频繁重连，
//! 而不是一上来就怀疑网络或对端主机。

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

/// Linux 用户态默认 100，与 `/proc/<pid>/stat` 的 jiffies 一致。
const CLOCK_TICKS_PER_SEC: u64 = 100;

/// Agent 自身的运行时计数器。跨连接存活（重连不清零），累计值。
#[derive(Clone, Default)]
pub struct SelfMetricsSource {
    dropped_low: Arc<AtomicU64>,
    reconnect_count: Arc<AtomicU64>,
    /// 最近一次 collect_state 耗时（微秒）。
    collect_state_us: Arc<AtomicU64>,
    /// CPU 采样基准（上次的 tick 与时间）。用 Mutex 而不是原子，
    /// 因为两个值必须成对更新，否则会算出荒唐的瞬时百分比。
    sampler: Arc<Mutex<Option<(u64, Instant)>>>,
}

/// 一次自身资源快照。
pub struct SelfSnapshot {
    pub rss_bytes: u64,
    pub cpu_percent: f64,
}

impl SelfMetricsSource {
    pub fn new() -> Self {
        Self::default()
    }

    /// 记录一次低优先级消息因队列满被丢弃。
    pub fn record_dropped_low(&self) {
        self.dropped_low.fetch_add(1, Ordering::Relaxed);
    }

    /// 记录一次重连（由主循环在每次 run_client 返回后调用）。
    pub fn record_reconnect(&self) {
        self.reconnect_count.fetch_add(1, Ordering::Relaxed);
    }

    pub fn record_collect_state(&self, elapsed: Duration) {
        // 微秒足够：正常在几十~几百 µs，异常时到毫秒级也能看出量级。
        let micros = elapsed.as_micros().min(u64::MAX as u128) as u64;
        self.collect_state_us.store(micros, Ordering::Relaxed);
    }

    pub fn dropped_low(&self) -> u64 {
        self.dropped_low.load(Ordering::Relaxed)
    }

    pub fn reconnect_count(&self) -> u64 {
        self.reconnect_count.load(Ordering::Relaxed)
    }

    pub fn collect_state_us(&self) -> u64 {
        self.collect_state_us.load(Ordering::Relaxed)
    }

    /// 采一份 Agent 自身资源快照（RSS + CPU%）。
    ///
    /// CPU 用两次 `/proc/self/stat` 的 utime+stime 差值，需要保留上次基准，
    /// 所以放在有状态的采样器里；第一次调用返回 0（没有基准）。
    pub fn snapshot(&self) -> SelfSnapshot {
        let previous = self
            .sampler
            .lock()
            .ok()
            .and_then(|mut guard| guard.take());
        let now = Instant::now();
        let current = read_self_cpu_ticks();

        let (cpu_percent, next) = match (previous, current) {
            (Some((prev_ticks, prev_at)), Some(ticks)) => {
                let elapsed = now.duration_since(prev_at).as_secs_f64();
                let delta = ticks.saturating_sub(prev_ticks) as f64;
                let percent = if elapsed > 0.0 {
                    delta / CLOCK_TICKS_PER_SEC as f64 / elapsed * 100.0
                } else {
                    0.0
                };
                (percent, Some((ticks, now)))
            }
            (_, Some(ticks)) => (0.0, Some((ticks, now))),
            (_, None) => (0.0, None),
        };

        if let Ok(mut guard) = self.sampler.lock() {
            *guard = next;
        }

        SelfSnapshot {
            rss_bytes: read_self_rss_bytes(),
            cpu_percent,
        }
    }
}

// 计数器是累计值，Debug 只输出数值便于日志排查。
impl std::fmt::Debug for SelfMetricsSource {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("SelfMetricsSource")
            .field("dropped_low", &self.dropped_low())
            .field("reconnect_count", &self.reconnect_count())
            .field("collect_state_us", &self.collect_state_us())
            .finish()
    }
}

#[cfg(target_os = "linux")]
fn read_self_cpu_ticks() -> Option<u64> {
    let raw = std::fs::read_to_string("/proc/self/stat").ok()?;
    // comm 可能含空格/括号，从最后一个 ')' 之后切；其后第 11/12 个字段是 utime/stime。
    let close = raw.rfind(')')?;
    let rest: Vec<&str> = raw[close + 1..].split_whitespace().collect();
    let utime: u64 = rest.get(11)?.parse().ok()?;
    let stime: u64 = rest.get(12)?.parse().ok()?;
    Some(utime + stime)
}

#[cfg(not(target_os = "linux"))]
fn read_self_cpu_ticks() -> Option<u64> {
    None
}

#[cfg(target_os = "linux")]
fn read_self_rss_bytes() -> u64 {
    let Ok(raw) = std::fs::read_to_string("/proc/self/status") else {
        return 0;
    };
    for line in raw.lines() {
        if let Some(rest) = line.strip_prefix("VmRSS:") {
            let kb: u64 = rest
                .trim()
                .trim_end_matches(" kB")
                .trim()
                .parse()
                .unwrap_or(0);
            return kb.saturating_mul(1024);
        }
    }
    0
}

#[cfg(not(target_os = "linux"))]
fn read_self_rss_bytes() -> u64 {
    0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn counters_start_at_zero_and_accumulate() {
        let source = SelfMetricsSource::new();
        assert_eq!(source.dropped_low(), 0);
        assert_eq!(source.reconnect_count(), 0);
        assert_eq!(source.collect_state_us(), 0);

        source.record_dropped_low();
        source.record_dropped_low();
        source.record_reconnect();
        source.record_collect_state(Duration::from_micros(1234));

        assert_eq!(source.dropped_low(), 2);
        assert_eq!(source.reconnect_count(), 1);
        assert_eq!(source.collect_state_us(), 1234);
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn snapshot_reads_own_rss() {
        let source = SelfMetricsSource::new();
        let snapshot = source.snapshot();
        assert!(snapshot.rss_bytes > 0, "应能读到自身 VmRSS");
        // 首次没有基准，CPU 报 0 而不是瞎猜一个值。
        assert_eq!(snapshot.cpu_percent, 0.0);
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn second_snapshot_reports_non_negative_cpu() {
        let source = SelfMetricsSource::new();
        let _first = source.snapshot();
        std::thread::sleep(Duration::from_millis(60));
        let second = source.snapshot();
        assert!(
            second.cpu_percent >= 0.0,
            "CPU 百分比不应为负，got {}",
            second.cpu_percent
        );
    }
}
