//! 断线期间的指标缓冲。
//!
//! 为什么存在
//! ----------
//! Agent 的采集循环跑在「已认证连接」内部（见 main.rs 的 state_timer），
//! 连接一断整条链路就停了 —— 断网 5 分钟，面板上就是 5 分钟的数据空洞，
//! 而这些数据本来是能采到的（CPU/内存/磁盘都不依赖网络）。
//!
//! 设计取舍
//! --------
//!  - **有界**：按条数与字节数双上限。缓冲是「补最近一段」，不是时序数据库；
//!    超限丢最旧的，并且丢弃次数计入自监控（`offline_dropped`），
//!    否则「加了缓冲却悄悄丢」只能靠抓包发现。
//!  - **只存已序列化的 JSON**：回放时直接发，不必再序列化一次；也避免
//!    为缓冲单独维护一套结构体的生命周期。
//!  - **不落盘**：Agent 重启后缓冲丢失是可接受的（重启本身也是断点）；
//!    落盘会带来 fsync、损坏恢复、权限等一堆问题，收益不成比例。
//!
//! 容量
//! ----
//! 默认 600 条 ≈ 1.5 秒上报间隔下的 15 分钟；单条 state 约 600~800 字节，
//! 满额约 500KB，对 7MB 常驻的 Agent 来说可以忽略。

use crate::collector::State;
use std::collections::VecDeque;
use std::sync::{Arc, Mutex};

/// 默认最多缓存多少条 state。
pub const DEFAULT_MAX_ENTRIES: usize = 600;

/// 默认最多缓存多少字节（约 8MB，远高于 600 条的实际占用，只是兜底）。
pub const DEFAULT_MAX_BYTES: usize = 8 * 1024 * 1024;

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct BufferedState {
    /// Agent 侧递增序号，回放时用于排序/去重。
    pub sequence: u64,
    /// 采集时刻（RFC3339），服务端按此时间落库。
    pub recorded_at: String,
    /// 采集到的 state（原样保留，回放时由 format_event 序列化）。
    pub state: State,
}

pub struct OfflineBufferInner {
    /// (序列化后字节数, 条目)：字节数随条目存，出队时才算得回正确占用。
    entries: VecDeque<(usize, BufferedState)>,
    bytes: usize,
    max_entries: usize,
    max_bytes: usize,
    /// 因容量上限被挤掉的条数（累计）。
    dropped: u64,
}

/// 有界环形缓冲。`Clone` 共享同一份数据。
#[derive(Clone)]
pub struct OfflineBuffer {
    inner: Arc<Mutex<OfflineBufferInner>>,
}

impl Default for OfflineBuffer {
    fn default() -> Self {
        Self::with_limits(DEFAULT_MAX_ENTRIES, DEFAULT_MAX_BYTES)
    }
}

impl OfflineBuffer {
    pub fn with_limits(max_entries: usize, max_bytes: usize) -> Self {
        Self {
            inner: Arc::new(Mutex::new(OfflineBufferInner {
                entries: VecDeque::new(),
                bytes: 0,
                max_entries: max_entries.max(1),
                max_bytes: max_bytes.max(1),
                dropped: 0,
            })),
        }
    }

    /// 追加一条。超过上限时从最旧的开始丢。
    ///
    /// 字节数按序列化后的实际大小估算：缓冲的目的是控制内存，不是控制条数，
    /// 所以必须按真实占用算，否则状态变大时（新增字段）会悄悄超限。
    pub fn push(&self, sequence: u64, recorded_at: String, state: State) {
        let payload = BufferedState {
            sequence,
            recorded_at,
            state,
        };
        let size = serde_json::to_string(&payload)
            .map(|s| s.len())
            .unwrap_or(0);

        let Ok(mut inner) = self.inner.lock() else {
            return;
        };

        // 单条就超过总上限时直接丢弃并计数：留着它会让整个缓冲失效。
        if size > inner.max_bytes {
            inner.dropped = inner.dropped.saturating_add(1);
            return;
        }

        inner.bytes += size;
        inner.entries.push_back((size, payload));

        while inner.entries.len() > inner.max_entries || inner.bytes > inner.max_bytes {
            let Some((evicted_size, _)) = inner.entries.pop_front() else {
                break;
            };
            inner.bytes = inner.bytes.saturating_sub(evicted_size);
            inner.dropped = inner.dropped.saturating_add(1);
        }
    }

    /// 取出全部缓存（按采集顺序），并清空。
    ///
    /// 取走而不是复制：回放是「一次性补偿」，留在缓冲里只会重复发送。
    pub fn drain(&self) -> Vec<BufferedState> {
        let Ok(mut inner) = self.inner.lock() else {
            return Vec::new();
        };
        inner.bytes = 0;
        inner.entries.drain(..).map(|(_, entry)| entry).collect()
    }

    pub fn len(&self) -> usize {
        self.inner.lock().map(|i| i.entries.len()).unwrap_or(0)
    }

    pub fn dropped(&self) -> u64 {
        self.inner.lock().map(|i| i.dropped).unwrap_or(0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_state(cpu: f64) -> State {
        State {
            timestamp_ms: 1,
            cpu,
            ..State::default()
        }
    }

    #[test]
    fn push_and_drain_preserves_order() {
        let buffer = OfflineBuffer::default();
        buffer.push(1, "2026-11-06T00:00:00Z".to_string(), sample_state(1.0));
        buffer.push(2, "2026-11-06T00:00:01Z".to_string(), sample_state(2.0));
        buffer.push(3, "2026-11-06T00:00:02Z".to_string(), sample_state(3.0));

        assert_eq!(buffer.len(), 3);
        let drained = buffer.drain();
        assert_eq!(drained.len(), 3);
        assert_eq!(
            drained.iter().map(|s| s.sequence).collect::<Vec<_>>(),
            vec![1, 2, 3]
        );
        // 回放依赖 recorded_at 还原原始时间，必须原样保留
        assert_eq!(drained[1].recorded_at, "2026-11-06T00:00:01Z");
        assert_eq!(drained[1].state.cpu, 2.0);
        // 取走后就空了
        assert_eq!(buffer.len(), 0);
        assert_eq!(buffer.dropped(), 0);
    }

    #[test]
    fn exceeding_entry_limit_drops_oldest_and_counts() {
        // 字节上限放宽，专测「按条数淘汰」这条路径
        let buffer = OfflineBuffer::with_limits(3, 1024 * 1024);
        for i in 0..5 {
            buffer.push(i, format!("2026-11-06T00:00:0{i}Z"), sample_state(i as f64));
        }
        assert_eq!(buffer.len(), 3, "只保留最新 3 条");
        assert_eq!(buffer.dropped(), 2, "挤掉 2 条要计数");

        let drained = buffer.drain();
        assert_eq!(
            drained.iter().map(|s| s.sequence).collect::<Vec<_>>(),
            vec![2, 3, 4],
            "保留的应当是最新的 3 条"
        );
    }

    #[test]
    fn exceeding_byte_limit_drops_oldest() {
        // 一条 state 序列化后约 600-800 字节；上限 2000 只放得下 2 条左右，
        // 用来验证「按字节而不是按条数淘汰」这条路径。
        let buffer = OfflineBuffer::with_limits(100, 2000);
        for i in 0..5 {
            buffer.push(i, format!("2026-11-06T00:00:0{i}Z"), sample_state(i as f64));
        }
        assert!(buffer.len() < 5, "字节上限必须真的淘汰掉一些条目");
        assert!(buffer.dropped() >= 1);
    }

    #[test]
    fn single_entry_larger_than_limit_is_rejected() {
        let buffer = OfflineBuffer::with_limits(10, 64);
        buffer.push(1, "2026-11-06T00:00:00Z".to_string(), sample_state(1.0));
        assert_eq!(buffer.len(), 0);
        assert_eq!(buffer.dropped(), 1);
    }

    #[test]
    fn drain_on_empty_buffer_is_noop() {
        let buffer = OfflineBuffer::default();
        assert!(buffer.drain().is_empty());
        assert_eq!(buffer.dropped(), 0);
    }

    #[test]
    fn clone_shares_state() {
        let buffer = OfflineBuffer::default();
        let other = buffer.clone();
        buffer.push(1, "2026-11-06T00:00:00Z".to_string(), sample_state(1.0));
        assert_eq!(other.len(), 1);
    }
}
