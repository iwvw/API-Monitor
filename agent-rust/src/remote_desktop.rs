#[cfg(target_os = "windows")]
mod windows_impl {
    use std::collections::HashMap;
    use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
    use std::sync::{Arc, Mutex, OnceLock};
    use std::time::Duration;

    use bytes::Bytes;
    use enigo::{Axis, Button, Coordinate, Direction, Enigo, Key, Keyboard, Mouse, Settings};
    use rand::random;
    use rtp::codecs::h264::H264Payloader;
    use rtp::header::Header;
    use rtp::packet::Packet;
    use rtp::packetizer::Payloader;
    use serde::Deserialize;
    use serde_json::{json, Value};
    use webrtc::api::interceptor_registry::register_default_interceptors;
    use webrtc::api::media_engine::MediaEngine;
    use webrtc::api::setting_engine::SettingEngine;
    use webrtc::api::APIBuilder;
    use webrtc::data_channel::data_channel_message::DataChannelMessage;
    use webrtc::data_channel::RTCDataChannel;
    use webrtc::ice_transport::ice_candidate::RTCIceCandidateInit;
    use webrtc::ice_transport::ice_candidate_type::RTCIceCandidateType;
    use webrtc::ice_transport::ice_server::RTCIceServer;
    use webrtc::ice::network_type::NetworkType;
    use webrtc::interceptor::registry::Registry;
    use webrtc::peer_connection::configuration::RTCConfiguration;
    use webrtc::peer_connection::peer_connection_state::RTCPeerConnectionState;
    use webrtc::peer_connection::policy::ice_transport_policy::RTCIceTransportPolicy;
    use webrtc::peer_connection::sdp::session_description::RTCSessionDescription;
    use webrtc::peer_connection::RTCPeerConnection;
    use webrtc::rtcp::payload_feedbacks::picture_loss_indication::PictureLossIndication;
    use webrtc::rtcp::payload_feedbacks::receiver_estimated_maximum_bitrate::ReceiverEstimatedMaximumBitrate;
    use webrtc::rtp_transceiver::rtp_codec::RTCRtpCodecCapability;
    use webrtc::track::track_local::track_local_static_rtp::TrackLocalStaticRTP;
    use webrtc::track::track_local::{TrackLocal, TrackLocalWriter};
    use win_native_media::capture::{self, CaptureConfig};
    use win_native_media::encoder::mf_h264::MfH264Encoder;
    use win_native_media::encoder::EncodedSample;
    use win_native_media::{CaptureTarget, VideoConfig};
    use windows::Win32::Graphics::Direct3D11::ID3D11Texture2D;
    use windows_sys::Win32::Foundation::{GlobalFree, POINT, RECT};
    use windows_sys::Win32::System::DataExchange::{
        CloseClipboard, EmptyClipboard, GetClipboardData, GetClipboardSequenceNumber,
        IsClipboardFormatAvailable, OpenClipboard, SetClipboardData,
    };
    use windows_sys::Win32::System::Memory::{GlobalAlloc, GlobalLock, GlobalSize, GlobalUnlock, GMEM_MOVEABLE};
    use windows_sys::Win32::System::ProcessStatus::K32EmptyWorkingSet;
    use windows_sys::Win32::System::Threading::GetCurrentProcess;
    use windows_sys::Win32::UI::Input::Pointer::{
        InitializeTouchInjection, InjectTouchInput, POINTER_FLAG_CANCELED, POINTER_FLAG_DOWN,
        POINTER_FLAG_INCONTACT, POINTER_FLAG_INRANGE, POINTER_FLAG_UP, POINTER_FLAG_UPDATE,
        POINTER_TOUCH_INFO, TOUCH_FEEDBACK_DEFAULT,
    };
    use windows_sys::Win32::UI::WindowsAndMessaging::{
        PT_TOUCH, TOUCH_MASK_CONTACTAREA, TOUCH_MASK_ORIENTATION, TOUCH_MASK_PRESSURE,
    };

    use crate::nat::should_count_interface;
    use crate::protocol::{format_event, EVENT_AGENT_REMOTE_DESKTOP_SIGNAL};
    use crate::OutboundQueues;

    const TARGET_FPS: u32 = 60;
    // One IDR per second bounds error recovery after packet loss to ~1s while
    // keeping the keyframe overhead small relative to the steady bitrate.
    const KEYFRAME_INTERVAL_SECONDS: u32 = 1;
    // Small queue so a momentarily blocked RTP writer (congested link) does not
    // cause an immediate frame drop. Depth 3 adds at most ~50 ms of latency at
    // 60 fps while absorbing scheduler/网络抖动, which previously showed up as
    // dropped frames and triggered the frontend's bitrate downshift.
    const ENCODED_QUEUE_DEPTH: usize = 3;
    // Fallback cap on the encoded long edge, used only when the dashboard does
    // not supply one at session start. The dashboard always sends an explicit
    // profile (`dashboard:rd_start.profile`), so this is a safety net for older
    // or non-standard clients rather than the product default; it is kept in
    // sync with the frontend's `balanced` preset (1920) so a profile-less start
    // does not silently stream at a different resolution than the UI reports.
    const DEFAULT_MAX_LONG_EDGE: u32 = 1920;
    const MIN_MAX_LONG_EDGE: u32 = 1280;
    const MAX_MAX_LONG_EDGE: u32 = 3840;
    /// 1080p 参考码率上限；更高像素按 `native_pixels / (1920*1080)` 线性放大。
    /// 这是本项目的产品默认值，不来自上游实现。
    const NATIVE_BITRATE_1080P: u32 = 20_000_000;
    const NATIVE_BITRATE_MIN: u32 = 6_000_000;
    const NATIVE_BITRATE_MAX: u32 = 60_000_000;
    /// 峰值/目标码率比 = 3/2。依据 JetKVM 低延迟编码器策略
    /// (`internal/native/cgo/video_bitrate.h`: `maximum = target * 3 / 2`)。
    const BITRATE_PEAK_NUM: u32 = 3;
    const BITRATE_PEAK_DEN: u32 = 2;
    /// 编码器 VBV 窗口 = 2 × 平均码率。依据 FFmpeg nvenc 默认值
    /// (`libavcodec/nvenc.c`: 未显式指定 bufsize 时 `vbvBufferSize = 2 * averageBitRate`)。
    const VBV_WINDOW_MULTIPLIER: u32 = 2;
    /// 码率下限。REMB 反馈可能远低于前端预设，需要允许降到更低值才能真正
    /// 遵从接收端估计（否则"限速"形同虚设）。
    const REMOTE_BITRATE_FLOOR: u32 = 1_000_000;
    const RTP_CLOCK_RATE: u128 = 90_000;
    const RTP_PACKET_MTU: usize = 1_200;
    const RTP_HEADER_SIZE: usize = 12;
    // `CF_UNICODETEXT` is not exported by windows-sys; 13 is the stable format id.
    const CF_UNICODETEXT: u32 = 13;
    const PEER_DISCONNECT_GRACE: Duration = Duration::from_secs(5);
    const WORKER_SHUTDOWN_TIMEOUT: Duration = Duration::from_secs(3);

    /// Windows tunnel/VPN adapter name fragments that the shared `nat` filter
    /// does not already cover (TUN-mode proxies such as Clash/Mihomo, sing-box,
    /// Wintun, OpenVPN, ...). Host candidates gathered on these adapters are
    /// unreachable from the browser and, worse, route the media stream into the
    /// proxy tunnel (high latency, consumes the proxy's scarce bandwidth).
    const DESKTOP_TUNNEL_MARKERS: &[&str] = &[
        "wintun",
        "clash",
        "mihomo",
        "sing-box",
        "singbox",
        "openvpn",
        "wireguard",
        "v2ray",
        "xray",
        "shadowsocks",
        "openconnect",
        "anyconnect",
        "forticlient",
    ];

    /// Interface filter for the remote-desktop ICE agent: keep the real
    /// physical NICs the P2P subsystem would use, and additionally drop
    /// Windows tunnel adapters.
    fn should_use_interface_for_desktop(name: &str) -> bool {
        if !should_count_interface(name) {
            return false;
        }
        let normalized = name.trim().to_ascii_lowercase();
        !DESKTOP_TUNNEL_MARKERS
            .iter()
            .any(|marker| normalized.contains(marker))
    }

    /// Runtime-tunable remote-desktop parameters. Every field is a **product
    /// default** of this project (not derived from an upstream implementation)
    /// and can be overridden with `API_MONITOR_RD_*` environment variables, so a
    /// deployment can retune without rebuilding the Agent. Per-session values
    /// pushed from the dashboard (`dashboard:rd_start` / `video-config`) take
    /// precedence where the field supports it.
    #[derive(Clone, Debug)]
    struct RemoteDesktopTuning {
        /// 编码长边上限的默认值（`video-config.maxLongEdge` 可覆盖）。
        max_long_edge: u32,
        /// 1080p 参考码率上限；更高像素按比例放大。
        native_bitrate_1080p: u32,
        keyframe_interval_seconds: u32,
        /// 编码结果队列深度。
        queue_depth: usize,
        /// 1:1 NAT / 公网 IP 直连广播（pion `SetNAT1To1IPs` 语义）。
        nat_1to1_ips: Vec<String>,
        nat_1to1_candidate_type: RTCIceCandidateType,
    }

    impl Default for RemoteDesktopTuning {
        fn default() -> Self {
            Self {
                max_long_edge: DEFAULT_MAX_LONG_EDGE,
                native_bitrate_1080p: NATIVE_BITRATE_1080P,
                keyframe_interval_seconds: KEYFRAME_INTERVAL_SECONDS,
                queue_depth: ENCODED_QUEUE_DEPTH,
                nat_1to1_ips: Vec::new(),
                nat_1to1_candidate_type: RTCIceCandidateType::Host,
            }
        }
    }

    impl RemoteDesktopTuning {
        /// Resolve from an arbitrary key/value lookup. Tests pass a closure so
        /// they never mutate the process-global environment.
        fn from_lookup(lookup: impl Fn(&str) -> Option<String>) -> Self {
            let mut tuning = Self::default();
            let number = |key: &str| lookup(key).and_then(|value| value.trim().parse::<u32>().ok());
            if let Some(value) = number("API_MONITOR_RD_MAX_LONG_EDGE") {
                tuning.max_long_edge = value.clamp(MIN_MAX_LONG_EDGE, MAX_MAX_LONG_EDGE);
            }
            if let Some(value) = number("API_MONITOR_RD_BITRATE_1080P") {
                tuning.native_bitrate_1080p = value.clamp(1_000_000, NATIVE_BITRATE_MAX);
            }
            if let Some(value) = number("API_MONITOR_RD_KEYFRAME_SECONDS") {
                tuning.keyframe_interval_seconds = value.clamp(1, 10);
            }
            if let Some(value) = number("API_MONITOR_RD_QUEUE_DEPTH") {
                tuning.queue_depth = value.clamp(1, 16) as usize;
            }
            if let Some(value) = lookup("API_MONITOR_RD_NAT_1TO1_IPS") {
                tuning.nat_1to1_ips = value
                    .split(',')
                    .map(|item| item.trim().to_owned())
                    .filter(|item| !item.is_empty())
                    .collect();
            }
            if let Some(value) = lookup("API_MONITOR_RD_NAT_1TO1_TYPE") {
                if let Some(kind) = parse_candidate_type(&value) {
                    tuning.nat_1to1_candidate_type = kind;
                }
            }
            tuning
        }

        fn from_env() -> Self {
            Self::from_lookup(|key| std::env::var(key).ok())
        }
    }

    /// Parse an ICE candidate type for 1:1 NAT advertisement. webrtc-rs only
    /// supports `host` and `srflx` here (`ExternalIpMapper::new` rejects other
    /// types with `ErrUnsupportedNat1to1IpCandidateType`, which would fail peer
    /// creation), so anything else is treated as "not configured".
    fn parse_candidate_type(value: &str) -> Option<RTCIceCandidateType> {
        match value.trim().to_ascii_lowercase().as_str() {
            "host" => Some(RTCIceCandidateType::Host),
            "srflx" => Some(RTCIceCandidateType::Srflx),
            _ => None,
        }
    }

    /// Process-wide tuning, resolved once from the environment.
    fn tuning() -> &'static RemoteDesktopTuning {
        static TUNING: OnceLock<RemoteDesktopTuning> = OnceLock::new();
        TUNING.get_or_init(RemoteDesktopTuning::from_env)
    }

    /// WebRTC PLI keyframe requests for the NVENC path. The `DesktopEncoder`
    /// trait's `force_keyframe(&self)` cannot take `&mut self`, so the capture
    /// loop flips this atomic and the next `encode` call consumes it.
    static NVENC_FORCE_IDR: AtomicBool = AtomicBool::new(false);

    /// 可选的初始视频档位。面板在建会话时一并下发，使首帧就按用户选定的
    /// 分辨率/帧率/码率编码，而不是先按 Agent 兜底默认值跑一段再到
    /// `video-config` 才纠正（那会在连接初期产生与 UI 不符的分辨率）。
    ///
    /// 面板以 camelCase 下发（`maxLongEdge`），本结构体字段是 snake_case，因此必须
    /// 显式 alias —— 否则该字段会被 serde 静默丢弃（解析成功但值为 None），
    /// 表现为「初始分辨率上限被忽略」。同时保留 snake_case 以兼容既有调用方。
    #[derive(Debug, Default, Deserialize)]
    pub struct StartProfilePayload {
        pub fps: Option<u32>,
        pub bitrate: Option<u32>,
        #[serde(alias = "maxLongEdge")]
        pub max_long_edge: Option<u32>,
    }

    #[derive(Debug, Deserialize)]
    pub struct StartPayload {
        pub session_id: String,
        pub offer: RTCSessionDescription,
        #[serde(default)]
        pub ice_servers: Vec<IceServerPayload>,
        /// 可选：1:1 NAT / 公网 IP 列表，由面板下发时覆盖 Agent 环境变量配置。
        #[serde(default)]
        pub nat_1to1_ips: Vec<String>,
        /// 可选：上述地址使用的候选类型。webrtc-rs 仅支持 host / srflx。
        #[serde(default)]
        pub nat_1to1_candidate_type: Option<RTCIceCandidateType>,
        /// 可选：会话初始视频档位（面板当前选择的画质档 + 帧率档）。
        #[serde(default)]
        pub profile: Option<StartProfilePayload>,
    }

    impl StartProfilePayload {
        /// 归一化为实际使用的 StreamProfile，复用与 `video-config` 完全相同的
        /// 钳制规则，保证两条路径不会产生不一致的档位。
        fn to_stream_profile(&self) -> StreamProfile {
            let default = StreamProfile::default();
            StreamProfile {
                fps: self
                    .fps
                    .unwrap_or(default.fps)
                    .clamp(30, TARGET_FPS),
                bitrate: self
                    .bitrate
                    .unwrap_or(default.bitrate)
                    .clamp(3_000_000, 40_000_000),
                max_long_edge: self
                    .max_long_edge
                    .map(|edge| edge.clamp(MIN_MAX_LONG_EDGE, MAX_MAX_LONG_EDGE))
                    .unwrap_or(default.max_long_edge),
            }
        }
    }

    #[derive(Debug, Deserialize)]
    pub struct IceServerPayload {
        #[serde(default)]
        pub urls: Vec<String>,
        #[serde(default)]
        pub username: String,
        #[serde(default)]
        pub credential: String,
    }

    #[derive(Debug, Deserialize)]
    pub struct SignalPayload {
        pub session_id: String,
        pub signal: Value,
    }

    #[derive(Debug, Deserialize)]
    pub struct StopPayload {
        pub session_id: String,
    }

    struct Session {
        peer: Arc<RTCPeerConnection>,
        stop: Arc<AtomicBool>,
        worker: Arc<tokio::sync::Mutex<Option<tokio::task::JoinHandle<()>>>>,
        enigo: Arc<Mutex<Option<Enigo>>>,
        touch_contact: Arc<Mutex<Option<ActiveTouchContact>>>,
    }

    #[derive(Clone, Copy)]
    enum ActiveTouchContact {
        Native { x: i32, y: i32 },
        Mouse,
    }

    #[derive(Clone, Copy, Default)]
    struct DesktopGeometry {
        x: i32,
        y: i32,
        width: u32,
        height: u32,
    }

    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    struct StreamProfile {
        fps: u32,
        bitrate: u32,
        /// 编码长边上限；0 表示使用 DEFAULT_MAX_LONG_EDGE。
        max_long_edge: u32,
    }

    impl StreamProfile {
        fn effective_max_long_edge(&self) -> u32 {
            if self.max_long_edge == 0 {
                tuning().max_long_edge
            } else {
                self.max_long_edge
                    .clamp(MIN_MAX_LONG_EDGE, MAX_MAX_LONG_EDGE)
            }
        }
    }

    impl Default for StreamProfile {
        fn default() -> Self {
            Self {
                fps: 30,
                bitrate: 6_000_000,
                max_long_edge: 0,
            }
        }
    }

    struct EncodedVideoSample {
        data: Vec<u8>,
        timestamp: Duration,
    }

    fn enqueue_encoded_sample(
        tx: &tokio::sync::mpsc::Sender<EncodedVideoSample>,
        sample: EncodedVideoSample,
    ) -> bool {
        match tx.try_send(sample) {
            Ok(()) | Err(tokio::sync::mpsc::error::TrySendError::Full(_)) => true,
            Err(tokio::sync::mpsc::error::TrySendError::Closed(_)) => false,
        }
    }

    #[derive(Default)]
    pub struct RemoteDesktopManager {
        sessions: Arc<tokio::sync::Mutex<HashMap<String, Session>>>,
    }

    impl RemoteDesktopManager {
        pub fn new() -> Self {
            Self::default()
        }

        pub async fn start(
            &self,
            payload: StartPayload,
            outbound: OutboundQueues,
        ) -> Result<(), String> {
            self.stop_all().await;

            let mut media_engine = MediaEngine::default();
            media_engine
                .register_default_codecs()
                .map_err(|err| format!("register WebRTC codecs: {err}"))?;
            let registry = register_default_interceptors(Registry::new(), &mut media_engine)
                .map_err(|err| format!("register WebRTC RTP feedback: {err}"))?;

            // Network behaviour tuned for direct (non-relayed) connectivity:
            //  * interface_filter keeps virtual/tunnel adapters (tun/tap/wg/
            //    tailscale/clash/...) out of ICE. Under a TUN proxy they
            //    otherwise contribute unroutable host candidates that waste
            //    connectivity-check time and can drag media into the tunnel.
            //  * webrtc-rs 0.17 has no ICE-TCP support, so only UDP network
            //    types are gathered.
            //  * tighter ICE timeouts surface a dead path sooner so the
            //    frontend's reconnect logic can act instead of hanging.
            let mut setting_engine = SettingEngine::default();
            setting_engine.set_interface_filter(Box::new(should_use_interface_for_desktop));
            setting_engine.set_network_types(vec![NetworkType::Udp4, NetworkType::Udp6]);
            setting_engine.set_include_loopback_candidate(false);
            setting_engine.set_ice_timeouts(
                Some(Duration::from_secs(5)),
                Some(Duration::from_secs(10)),
                // 1 s keep-alives hold NAT bindings open on jittery/proxied links.
                Some(Duration::from_secs(1)),
            );
            // 1:1 NAT / public IP advertisement. This is pion's `SetNAT1To1IPs`,
            // which neko exposes as `webrtc.nat1to1`. When the host has a static
            // public address (or a 1:1 DNAT), advertising it as a candidate
            // establishes a direct path without any STUN/hole punching. Values
            // come from the panel payload when present, otherwise from the
            // Agent's API_MONITOR_RD_NAT_1TO1_* environment configuration.
            let nat_1to1_ips = if payload.nat_1to1_ips.is_empty() {
                tuning().nat_1to1_ips.clone()
            } else {
                payload.nat_1to1_ips.clone()
            };
            if !nat_1to1_ips.is_empty() {
                let candidate_type = payload
                    .nat_1to1_candidate_type
                    .unwrap_or(tuning().nat_1to1_candidate_type);
                setting_engine.set_nat_1to1_ips(nat_1to1_ips, candidate_type);
            }

            let api = APIBuilder::new()
                .with_media_engine(media_engine)
                .with_interceptor_registry(registry)
                .with_setting_engine(setting_engine)
                .build();
            let ice_servers = payload
                .ice_servers
                .into_iter()
                .map(|item| RTCIceServer {
                    urls: item.urls,
                    username: item.username,
                    credential: item.credential,
                    ..Default::default()
                })
                .collect();
            let peer = Arc::new(
                api.new_peer_connection(RTCConfiguration {
                    ice_servers,
                    // Gather every candidate type (host/srflx/relay). Relay is
                    // still preferred to fail loudly rather than silently
                    // degrade, but direct candidates are what we want.
                    ice_transport_policy: RTCIceTransportPolicy::All,
                    ..Default::default()
                })
                .await
                .map_err(|err| format!("create WebRTC peer: {err}"))?,
            );
            let stop = Arc::new(AtomicBool::new(false));
            let worker = Arc::new(tokio::sync::Mutex::new(None));
            let stream_started = Arc::new(AtomicBool::new(false));
            let force_keyframe = Arc::new(AtomicBool::new(false));
            // Latest REMB estimate from the receiver (0 = none yet). The capture
            // loop clamps the encoder target to it; JetKVM drives its encoder the
            // same way (`video_remb.go`).
            let remb_limit = Arc::new(AtomicU32::new(0));
            let geometry = Arc::new(Mutex::new(DesktopGeometry::default()));
            // 首帧即按面板下发的档位编码；面板未带 profile 时才回落到默认档。
            let stream_profile = Arc::new(Mutex::new(
                payload
                    .profile
                    .as_ref()
                    .map(StartProfilePayload::to_stream_profile)
                    .unwrap_or_default(),
            ));
            let enigo = Arc::new(Mutex::new(Enigo::new(&Settings::default()).ok()));
            let touch_contact = Arc::new(Mutex::new(None));
            let video_track = Arc::new(TrackLocalStaticRTP::new(
                RTCRtpCodecCapability {
                    mime_type: "video/H264".to_owned(),
                    clock_rate: 90_000,
                    sdp_fmtp_line:
                        "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"
                            .to_owned(),
                    ..Default::default()
                },
                "desktop".to_owned(),
                "api-monitor".to_owned(),
            ));
            let rtp_sender = peer
                .add_track(video_track.clone() as Arc<dyn TrackLocal + Send + Sync>)
                .await
                .map_err(|err| format!("add H.264 video track: {err}"))?;
            let force_keyframe_for_rtcp = force_keyframe.clone();
            let remb_limit_for_rtcp = remb_limit.clone();
            tokio::spawn(async move {
                while let Ok((packets, _)) = rtp_sender.read_rtcp().await {
                    for packet in &packets {
                        if packet
                            .as_any()
                            .downcast_ref::<PictureLossIndication>()
                            .is_some()
                        {
                            force_keyframe_for_rtcp.store(true, Ordering::Release);
                        }
                        let Some(remb) = packet
                            .as_any()
                            .downcast_ref::<ReceiverEstimatedMaximumBitrate>()
                        else {
                            continue;
                        };
                        // A session carries a single video track, so there is no
                        // need to match `remb.ssrcs` against a specific SSRC.
                        let bitrate = remb.bitrate;
                        if bitrate.is_finite() && bitrate > 0.0 {
                            remb_limit_for_rtcp.store(
                                (bitrate as u32).max(REMOTE_BITRATE_FLOOR),
                                Ordering::Release,
                            );
                        }
                    }
                }
            });

            let session_id_for_ice = payload.session_id.clone();
            let outbound_for_ice = outbound.clone();
            peer.on_ice_candidate(Box::new(move |candidate| {
                let session_id = session_id_for_ice.clone();
                let outbound = outbound_for_ice.clone();
                Box::pin(async move {
                    let Some(candidate) = candidate else { return };
                    if let Ok(candidate) = candidate.to_json() {
                        emit_signal(
                            &outbound,
                            &session_id,
                            Some(json!({
                                "kind": "ice",
                                "candidate": candidate,
                            })),
                            None,
                        )
                        .await;
                    }
                })
            }));

            let sessions_for_peer_state = self.sessions.clone();
            let session_id_for_peer_state = payload.session_id.clone();
            peer.on_peer_connection_state_change(Box::new(move |state| {
                let sessions = sessions_for_peer_state.clone();
                let session_id = session_id_for_peer_state.clone();
                Box::pin(async move {
                    let Some(delay) = peer_shutdown_delay(state) else {
                        return;
                    };
                    tokio::spawn(async move {
                        if !delay.is_zero() {
                            tokio::time::sleep(delay).await;
                        }
                        let should_shutdown = {
                            let sessions = sessions.lock().await;
                            sessions.get(&session_id).is_some_and(|session| {
                                matches!(
                                    session.peer.connection_state(),
                                    RTCPeerConnectionState::Disconnected
                                        | RTCPeerConnectionState::Failed
                                        | RTCPeerConnectionState::Closed
                                )
                            })
                        };
                        if should_shutdown {
                            remove_and_shutdown_session(&sessions, &session_id).await;
                        }
                    });
                })
            }));

            let session_id_for_channel = payload.session_id.clone();
            let outbound_for_channel = outbound.clone();
            let stop_for_channel = stop.clone();
            let stream_started_for_channel = stream_started.clone();
            let force_keyframe_for_channel = force_keyframe.clone();
            let remb_limit_for_channel = remb_limit.clone();
            let geometry_for_channel = geometry.clone();
            let profile_for_channel = stream_profile.clone();
            let enigo_for_channel = enigo.clone();
            let touch_contact_for_channel = touch_contact.clone();
            let track_for_channel = video_track.clone();
            let worker_for_channel = worker.clone();
            let sessions_for_channel = self.sessions.clone();
            peer.on_data_channel(Box::new(move |channel: Arc<RTCDataChannel>| {
                let reliable = channel.label() == "remote-desktop";
                if !reliable && channel.label() != "remote-pointer" {
                    return Box::pin(async {});
                }
                let session_id = session_id_for_channel.clone();
                let outbound = outbound_for_channel.clone();
                let stop = stop_for_channel.clone();
                let stream_started = stream_started_for_channel.clone();
                let force_keyframe = force_keyframe_for_channel.clone();
                let remb_limit = remb_limit_for_channel.clone();
                let geometry = geometry_for_channel.clone();
                let profile = profile_for_channel.clone();
                let enigo = enigo_for_channel.clone();
                let touch_contact = touch_contact_for_channel.clone();
                let video_track = track_for_channel.clone();
                let worker = worker_for_channel.clone();
                let sessions = sessions_for_channel.clone();
                Box::pin(async move {
                    let input_enigo = enigo.clone();
                    let input_geometry = geometry.clone();
                    let input_profile = profile.clone();
                    let input_touch_contact = touch_contact.clone();
                    let input_ack_channel = channel.clone();
                    let input_ack_sent = Arc::new(AtomicBool::new(false));
                    channel.on_message(Box::new(move |message: DataChannelMessage| {
                        let enigo = input_enigo.clone();
                        let geometry = input_geometry.clone();
                        let profile = input_profile.clone();
                        let touch_contact = input_touch_contact.clone();
                        let ack_channel = input_ack_channel.clone();
                        let ack_sent = input_ack_sent.clone();
                        Box::pin(async move {
                            if message.is_string {
                                if let Ok(value) = serde_json::from_slice::<Value>(&message.data) {
                                    let pointer_sequence = pointer_message_sequence(&value);
                                    if handle_channel_message(
                                        value,
                                        &enigo,
                                        &geometry,
                                        &profile,
                                        &touch_contact,
                                    ) {
                                        if let Some(sequence) = pointer_sequence {
                                            if let Some(position) = pointer_position_message(
                                                &enigo, &geometry, sequence,
                                            ) {
                                                let _ = ack_channel.send_text(position).await;
                                            }
                                        } else if !ack_sent.swap(true, Ordering::Relaxed) {
                                            let _ = ack_channel
                                                .send_text(json!({"type": "input-ack"}).to_string())
                                                .await;
                                        }
                                    }
                                }
                            }
                        })
                    }));

                    if !reliable {
                        return;
                    }

                    let frame_stop = stop.clone();
                    let frame_started = stream_started.clone();
                    let frame_geometry = geometry.clone();
                    let frame_session_id = session_id.clone();
                    let frame_outbound = outbound.clone();
                    let frame_track = video_track.clone();
                    let frame_force_keyframe = force_keyframe.clone();
                    let frame_remb_limit = remb_limit.clone();
                    let frame_worker = worker.clone();
                    let watcher_channel = channel.clone();
                    let watcher_stop = stop.clone();
                    channel.on_open(Box::new(move || {
                        let stop = frame_stop.clone();
                        let started = frame_started.clone();
                        let geometry = frame_geometry.clone();
                        let session_id = frame_session_id.clone();
                        let outbound = frame_outbound.clone();
                        let track = frame_track.clone();
                        let force_keyframe = frame_force_keyframe.clone();
                        let remb_limit = frame_remb_limit.clone();
                        let worker = frame_worker.clone();
                        spawn_clipboard_watcher(watcher_channel.clone(), watcher_stop.clone());
                        Box::pin(async move {
                            emit_signal(&outbound, &session_id, None, Some("connected")).await;
                            if !started.swap(true, Ordering::SeqCst) {
                                let handle = tokio::spawn(stream_desktop(
                                    track,
                                    stop.clone(),
                                    geometry,
                                    profile,
                                    force_keyframe,
                                    remb_limit,
                                    outbound,
                                    session_id,
                                ));
                                let mut worker_slot = worker.lock().await;
                                if stop.load(Ordering::Acquire) {
                                    drop(worker_slot);
                                    handle.abort();
                                } else {
                                    *worker_slot = Some(handle);
                                }
                            }
                        })
                    }));

                    let close_stop = stop.clone();
                    let close_session_id = session_id.clone();
                    let close_outbound = outbound.clone();
                    let close_sessions = sessions.clone();
                    channel.on_close(Box::new(move || {
                        let stop = close_stop.clone();
                        let session_id = close_session_id.clone();
                        let outbound = close_outbound.clone();
                        let sessions = close_sessions.clone();
                        Box::pin(async move {
                            stop.store(true, Ordering::Relaxed);
                            emit_signal(&outbound, &session_id, None, Some("closed")).await;
                            remove_and_shutdown_session(&sessions, &session_id).await;
                        })
                    }));
                })
            }));

            self.sessions.lock().await.insert(
                payload.session_id.clone(),
                Session {
                    peer: peer.clone(),
                    stop,
                    worker,
                    enigo,
                    touch_contact,
                },
            );

            let negotiation = async {
                peer.set_remote_description(payload.offer)
                    .await
                    .map_err(|err| format!("set WebRTC offer: {err}"))?;
                let answer = peer
                    .create_answer(None)
                    .await
                    .map_err(|err| format!("create WebRTC answer: {err}"))?;
                peer.set_local_description(answer)
                    .await
                    .map_err(|err| format!("set WebRTC answer: {err}"))?;
                peer.local_description()
                    .await
                    .ok_or_else(|| "WebRTC local answer missing".to_string())
            }
            .await;
            let answer = match negotiation {
                Ok(answer) => answer,
                Err(error) => {
                    self.stop(&payload.session_id).await;
                    return Err(error);
                }
            };
            emit_signal(
                &outbound,
                &payload.session_id,
                Some(json!({"kind": "answer", "sdp": answer})),
                Some("signaling"),
            )
            .await;
            Ok(())
        }

        pub async fn signal(&self, payload: SignalPayload) -> Result<(), String> {
            let peer = {
                let sessions = self.sessions.lock().await;
                sessions
                    .get(&payload.session_id)
                    .map(|session| session.peer.clone())
                    .ok_or_else(|| "remote desktop session not found".to_string())?
            };
            if payload.signal.get("kind").and_then(Value::as_str) == Some("ice") {
                let candidate: RTCIceCandidateInit = serde_json::from_value(
                    payload
                        .signal
                        .get("candidate")
                        .cloned()
                        .unwrap_or(Value::Null),
                )
                .map_err(|err| format!("invalid ICE candidate: {err}"))?;
                peer.add_ice_candidate(candidate)
                    .await
                    .map_err(|err| format!("add ICE candidate: {err}"))?;
            }
            Ok(())
        }

        pub async fn stop(&self, session_id: &str) {
            remove_and_shutdown_session(&self.sessions, session_id).await;
        }

        async fn stop_all(&self) {
            let sessions = {
                let mut sessions = self.sessions.lock().await;
                sessions
                    .drain()
                    .map(|(_, session)| session)
                    .collect::<Vec<_>>()
            };
            // Clear any pending PLI keyframe request left over from a previous
            // session so a fresh session does not emit a spurious IDR.
            NVENC_FORCE_IDR.store(false, Ordering::Release);
            for session in sessions {
                shutdown_session(session).await;
            }
        }
    }

    fn peer_shutdown_delay(state: RTCPeerConnectionState) -> Option<Duration> {
        match state {
            RTCPeerConnectionState::Failed | RTCPeerConnectionState::Closed => Some(Duration::ZERO),
            RTCPeerConnectionState::Disconnected => Some(PEER_DISCONNECT_GRACE),
            _ => None,
        }
    }

    async fn remove_and_shutdown_session(
        sessions: &Arc<tokio::sync::Mutex<HashMap<String, Session>>>,
        session_id: &str,
    ) {
        let session = sessions.lock().await.remove(session_id);
        if let Some(session) = session {
            shutdown_session(session).await;
        }
    }

    async fn shutdown_session(session: Session) {
        session.stop.store(true, Ordering::Release);
        release_active_touch(&session.touch_contact, &session.enigo);
        let _ = session.peer.close().await;
        if let Some(mut worker) = session.worker.lock().await.take() {
            if tokio::time::timeout(WORKER_SHUTDOWN_TIMEOUT, &mut worker)
                .await
                .is_err()
            {
                worker.abort();
                let _ = worker.await;
            }
        }
        // Media Foundation and D3D keep released frame pages resident for
        // reuse. Once the capture worker is fully gone, return those pages to
        // Windows so the Agent's active working set drops after disconnect.
        unsafe {
            let _ = K32EmptyWorkingSet(GetCurrentProcess());
        }
    }

    async fn emit_signal(
        outbound: &OutboundQueues,
        session_id: &str,
        signal: Option<Value>,
        state: Option<&str>,
    ) {
        let payload = json!({
            "session_id": session_id,
            "signal": signal,
            "state": state,
        });
        let _ = outbound
            .send_normal(format_event(EVENT_AGENT_REMOTE_DESKTOP_SIGNAL, &payload))
            .await;
    }

    async fn stream_desktop(
        track: Arc<TrackLocalStaticRTP>,
        stop: Arc<AtomicBool>,
        geometry: Arc<Mutex<DesktopGeometry>>,
        profile: Arc<Mutex<StreamProfile>>,
        force_keyframe: Arc<AtomicBool>,
        remb_limit: Arc<AtomicU32>,
        outbound: OutboundQueues,
        session_id: String,
    ) {
        let (sample_tx, mut sample_rx) = tokio::sync::mpsc::channel(tuning().queue_depth);
        let capture_stop = stop.clone();
        let capture_geometry = geometry.clone();
        let capture_profile = profile.clone();
        let encoder_task = tokio::task::spawn_blocking(move || {
            capture_and_encode(
                capture_stop,
                capture_geometry,
                capture_profile,
                force_keyframe,
                remb_limit,
                sample_tx,
            )
        });

        let timestamp_base = random::<u32>();
        let mut sequence_number = random::<u16>();
        let mut payloader = H264Payloader::default();
        while !stop.load(Ordering::Relaxed) {
            let Some(encoded) = sample_rx.recv().await else {
                break;
            };
            let timestamp = capture_timestamp_to_rtp(timestamp_base, encoded.timestamp);
            let packets = match packetize_h264_access_unit(
                &mut payloader,
                &mut sequence_number,
                timestamp,
                Bytes::from(encoded.data),
            ) {
                Ok(packets) => packets,
                Err(_) => break,
            };
            for packet in packets {
                if track.write_rtp(&packet).await.is_err() {
                    stop.store(true, Ordering::Relaxed);
                    break;
                }
            }
        }

        stop.store(true, Ordering::Relaxed);
        // Closing the receiver guarantees the capture worker can observe
        // shutdown even if it is concurrently publishing the final frame.
        drop(sample_rx);
        match encoder_task.await {
            Ok(Ok(())) => {}
            Ok(Err(message)) => {
                emit_signal(
                    &outbound,
                    &session_id,
                    Some(json!({"kind": "error", "message": message})),
                    Some("error"),
                )
                .await;
            }
            Err(err) => {
                emit_signal(
                    &outbound,
                    &session_id,
                    Some(
                        json!({"kind": "error", "message": format!("video worker stopped: {err}")}),
                    ),
                    Some("error"),
                )
                .await;
            }
        }
    }

    /// Unified H.264 encoder surface used by the capture loop. The Media
    /// Foundation encoder and the NVENC encoder both implement this so the loop
    /// does not care which hardware path is active.
    trait DesktopEncoder {
        /// Encode one captured texture. The encoder owns the resolution scaling
        /// (input size vs its configured output size) internally.
        fn encode(
            &mut self,
            texture: &ID3D11Texture2D,
            input_width: u32,
            input_height: u32,
            timestamp: Duration,
            out: &mut Vec<EncodedSample>,
        ) -> Result<(), String>;

        fn force_keyframe(&self);

        /// Switch the target bitrate without tearing the encoder session down.
        /// The default implementation refuses so the capture loop can fall back
        /// to a full re-init (cheap for the software MFT; NVENC overrides this
        /// with a dynamic reconfigure to keep the stream uninterrupted).
        fn set_bitrate(&mut self, _bitrate: u32) -> Result<(), String> {
            Err("bitrate change requires encoder re-init".to_string())
        }
    }

    /// Media Foundation H.264 encoder adapted to the [`DesktopEncoder`] trait.
    /// Wraps the vendored `MfH264Encoder` and maps its error type to `String`.
    struct MftH264EncoderAdapter(MfH264Encoder);

    impl DesktopEncoder for MftH264EncoderAdapter {
        fn encode(
            &mut self,
            texture: &ID3D11Texture2D,
            _input_width: u32,
            _input_height: u32,
            timestamp: Duration,
            out: &mut Vec<EncodedSample>,
        ) -> Result<(), String> {
            self.0
                .encode(texture, timestamp, out)
                .map_err(|err| format!("Media Foundation H.264 encode failed: {err}"))
        }

        fn force_keyframe(&self) {
            self.0.force_keyframe();
        }
    }

    /// NVIDIA NVENC encoder over `nvEncodeAPI64.dll`, loaded at runtime via
    /// `libloading`. NVENC does not resize ARGB/RGB input itself, so captured
    /// BGRA frames are scaled to the encode size by the D3D11 video processor
    /// (`Bgra2Nv12`) and fed as NV12. This keeps the encoded resolution equal
    /// to the configured target regardless of the native desktop size.
    struct NvencH264Encoder {
        encoder: nvenc::encoder::Encoder,
        converter: win_native_media::convert::Bgra2Nv12,
        input_buffer: nvenc::input_buffer::InputBuffer,
        bitstream: nvenc::bitstream::BitStream,
        /// Reused NV12 CPU buffer; avoids a heap allocation per frame.
        nv12_buffer: Vec<u8>,
        keyframe_interval: u32,
        frame_count: u64,
    }

    impl NvencH264Encoder {
        /// Create a low-latency H.264 session bound to `device`. Captured
        /// frames (`input_width` x `input_height`) are downscaled by the video
        /// processor to `cfg.width` x `cfg.height` before encoding.
        fn new(
            device: &windows::Win32::Graphics::Direct3D11::ID3D11Device,
            cfg: VideoConfig,
            input_width: u32,
            input_height: u32,
        ) -> Result<Self, String> {
            use nvenc::session::{InitParams, Session};
            use nvenc::sys::enums::{
                NVencBufferFormat, NVencMemoryHeap, NVencTuningInfo,
            };
            use nvenc::sys::guids::{NV_ENC_CODEC_H264_GUID, NV_ENC_PRESET_P3_GUID};

            let session: Session<nvenc::session::NeedsConfig> = Session::open_dx(device)
                .map_err(|err| format!("open NVENC session: {err:?}"))?;
            let codecs = session
                .get_encode_codecs()
                .map_err(|err| format!("query NVENC codecs: {err:?}"))?;
            if !codecs.contains(&NV_ENC_CODEC_H264_GUID) {
                return Err("NVENC reports no H.264 encoder".into());
            }
            let (session, mut config) = session
                .get_encode_preset_config_ex(
                    NV_ENC_CODEC_H264_GUID,
                    NV_ENC_PRESET_P3_GUID,
                    NVencTuningInfo::LowLatency,
                )
                .map_err(|err| format!("NVENC preset config: {err:?}"))?;

            // Constrained VBR: target the requested bitrate but cap the peak so
            // desktop content changes (scrolling, video, images) cannot spike far
            // above link capacity. Peak ratio and VBV window follow the reference
            // policies documented on BITRATE_PEAK_* / VBV_WINDOW_MULTIPLIER.
            config.preset_cfg.rc_params.rate_control_mode =
                nvenc::sys::enums::NVencParamsRcMode::VBR;
            config.preset_cfg.rc_params.average_bit_rate = cfg.bitrate;
            config.preset_cfg.rc_params.max_bit_rate =
                cfg.bitrate * BITRATE_PEAK_NUM / BITRATE_PEAK_DEN;
            config.preset_cfg.rc_params.vbv_buffer_size = cfg.bitrate * VBV_WINDOW_MULTIPLIER;
            config.preset_cfg.gop_len = cfg.keyframe_interval;
            config.preset_cfg.frame_interval_p = 1;

            let init_params = InitParams {
                encode_guid: NV_ENC_CODEC_H264_GUID,
                preset_guid: NV_ENC_PRESET_P3_GUID,
                aspect_ratio: [cfg.width, cfg.height],
                encode_config: &mut config.preset_cfg,
                tuning_info: NVencTuningInfo::LowLatency,
                // NVENC encodes NV12 natively; the video processor scales the
                // captured BGRA to the target size and converts to NV12.
                buffer_format: NVencBufferFormat::NV12,
                frame_rate: [cfg.fps, 1],
                resolution: [cfg.width, cfg.height],
                enable_ptd: true,
                max_encoder_resolution: [0, 0],
            };
            let encoder = session
                .init_encoder(init_params)
                .map_err(|err| format!("init NVENC session: {err:?}"))?;
            let converter = win_native_media::convert::Bgra2Nv12::new_with_scale(
                device,
                input_width,
                input_height,
                cfg.width,
                cfg.height,
            )
            .map_err(|err| format!("create BGRA->NV12 converter: {err:?}"))?;
            let input_buffer = encoder
                .create_input_buffer(
                    cfg.width,
                    cfg.height,
                    NVencMemoryHeap::SystemCached,
                    NVencBufferFormat::NV12,
                )
                .map_err(|err| format!("create NVENC input buffer: {err:?}"))?;
            let bitstream = encoder
                .create_bitstream_buffer()
                .map_err(|err| format!("create NVENC bitstream: {err:?}"))?;
            Ok(Self {
                encoder,
                converter,
                input_buffer,
                bitstream,
                nv12_buffer: Vec::new(),
                keyframe_interval: cfg.keyframe_interval,
                frame_count: 0,
            })
        }
    }

    impl DesktopEncoder for NvencH264Encoder {
        fn encode(
            &mut self,
            texture: &ID3D11Texture2D,
            _input_width: u32,
            _input_height: u32,
            timestamp: Duration,
            out: &mut Vec<EncodedSample>,
        ) -> Result<(), String> {
            use nvenc::sys::enums::{NVencPicStruct, NVencPicType};

            // Scale BGRA -> NV12 at the encode size and read back to the reused
            // CPU buffer, then copy it into the NVENC input buffer.
            self.converter
                .convert_to_cpu_into(texture, &mut self.nv12_buffer)
                .map_err(|err| format!("NVENC BGRA->NV12 convert: {err:?}"))?;

            let lock = self
                .input_buffer
                .lock()
                .map_err(|err| format!("NVENC lock input buffer: {err:?}"))?;
            let dst = unsafe { lock.data_ptr() };
            let pitch = lock.pitch() as usize;
            let (w, h) = (lock.width() as usize, lock.height() as usize);
            let y_size = w * h;
            // `convert_to_cpu_into` packs rows tightly (no pitch padding); the
            // NVENC input buffer may have a larger pitch. Copy row by row.
            unsafe {
                for row in 0..h {
                    std::ptr::copy_nonoverlapping(
                        self.nv12_buffer.as_ptr().add(row * w),
                        dst.add(row * pitch),
                        w,
                    );
                }
                let uv_src = self.nv12_buffer.as_ptr().add(y_size);
                for row in 0..(h / 2) {
                    std::ptr::copy_nonoverlapping(
                        uv_src.add(row * w),
                        dst.add(y_size + row * pitch),
                        w,
                    );
                }
            }
            drop(lock);

            self.frame_count += 1;
            let pli_idr = NVENC_FORCE_IDR.swap(false, Ordering::AcqRel);
            let periodic_idr = self.frame_count % u64::from(self.keyframe_interval.max(1)) == 1;
            let is_keyframe = pli_idr || periodic_idr;
            let pic_type = if is_keyframe {
                NVencPicType::IDR
            } else {
                NVencPicType::P
            };
            self.encoder
                .encode_picture(
                    &self.input_buffer,
                    &self.bitstream,
                    self.frame_count as usize,
                    timestamp.as_millis() as u64,
                    nvenc::sys::enums::NVencBufferFormat::NV12,
                    NVencPicStruct::Frame,
                    pic_type,
                    None,
                )
                .map_err(|err| format!("NVENC encode: {err:?}"))?;

            // Lock and drain the produced access unit. `try_lock(true)` waits.
            let bl = self
                .bitstream
                .try_lock(true)
                .map_err(|err| format!("NVENC lock bitstream: {err:?}"))?;
            let data = bl.as_slice().to_vec();
            if !data.is_empty() {
                out.push(EncodedSample {
                    data,
                    timestamp,
                    is_keyframe,
                });
            }
            Ok(())
        }

        fn force_keyframe(&self) {
            // A WebRTC PLI wants an IDR as soon as possible. The next encode
            // call checks this flag and schedules an IDR regardless of the
            // periodic interval. `force_keyframe(&self)` cannot take `&mut self`
            // (the capture loop calls it through the trait), so a process-wide
            // atomic flag is consumed by the next `encode`.
            NVENC_FORCE_IDR.store(true, Ordering::Release);
        }

        fn set_bitrate(&mut self, bitrate: u32) -> Result<(), String> {
            self.encoder
                .reconfigure_bitrate(bitrate)
                .map_err(|err| format!("NVENC reconfigure bitrate: {err:?}"))
        }
    }

    fn capture_and_encode(
        stop: Arc<AtomicBool>,
        geometry: Arc<Mutex<DesktopGeometry>>,
        profile: Arc<Mutex<StreamProfile>>,
        force_keyframe: Arc<AtomicBool>,
        remb_limit: Arc<AtomicU32>,
        sample_tx: tokio::sync::mpsc::Sender<EncodedVideoSample>,
    ) -> Result<(), String> {
        let (session, frames) = capture::start(
            CaptureConfig {
                target: CaptureTarget::Monitor(0),
                // WGC burned the real system cursor into the video frames while
                // the frontend also renders a virtual cursor, producing duplicate
                // (sometimes three) cursors. Keep cursor capture off and let the
                // frontend's pointer-position echo be the single cursor layer.
                capture_cursor: false,
            },
            1,
        )
        .map_err(|err| format!("start Windows Graphics Capture: {err}"))?;
        if let Ok((x, y, width, height)) = capture::monitor_geometry(0) {
            if let Ok(mut current) = geometry.lock() {
                *current = DesktopGeometry {
                    x,
                    y,
                    width,
                    height,
                };
            }
        }

        let mut encoder: Option<Box<dyn DesktopEncoder>> = None;
        let mut encoded_size = (0, 0);
        let mut encoded_profile = StreamProfile::default();
        let mut last_encoded_timestamp = Duration::ZERO;
        while !stop.load(Ordering::Relaxed) {
            let frame = match frames.recv_timeout(Duration::from_millis(250)) {
                Ok(frame) => frame,
                Err(std::sync::mpsc::RecvTimeoutError::Timeout) => continue,
                Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                    return Err("Windows Graphics Capture stopped unexpectedly".to_string())
                }
            };
            if frame.width == 0 || frame.height == 0 {
                continue;
            }
            if let Ok(mut current) = geometry.lock() {
                current.width = frame.width;
                current.height = frame.height;
            }

            let mut desired_profile = profile.lock().map(|item| *item).unwrap_or_default();
            // Sender-side clamp to the receiver's REMB estimate (0 = no estimate
            // yet). Applying it here means it also covers later `video-config`
            // updates from the dashboard, so the encoder never runs ahead of what
            // the receiver reports it can take.
            desired_profile = clamp_bitrate_to_remb(
                desired_profile,
                remb_limit.load(Ordering::Acquire),
            );
            if !should_encode_next_frame(
                last_encoded_timestamp,
                frame.timestamp,
                desired_profile.fps,
                sample_tx.capacity() > 0,
            ) {
                continue;
            }
            last_encoded_timestamp = frame.timestamp;

            // Encode at a capped resolution while `geometry` keeps the native
            // desktop size for pointer coordinate math. Downscaling the stream
            // keeps encode time and network bytes proportional to the visible
            // content rather than the full desktop.
            let (encode_width, encode_height) = scaled_encode_size(
                frame.width,
                frame.height,
                desired_profile.effective_max_long_edge(),
            );

            // A builder shared by the size-change path and the software-MFT
            // bitrate-change path below.
            let build_encoder = |config: VideoConfig| -> Result<Box<dyn DesktopEncoder>, String> {
                // Prefer NVENC (GPU, zero CPU encode cost, video-processor
                // scaling), then fall back to the software Media Foundation MFT.
                // NVENC loads nvEncodeAPI64.dll at runtime, so machines without
                // an NVIDIA GPU simply fall through to MFT.
                //
                // Deliberately NOT trying the MFT hardware path: repeated
                // hardware MFT sessions leak driver threads/handles (~17 threads
                // and ~50 MiB per session, crashing WGC after a few), which is
                // why the hardware MFT was originally disabled. Software MFT is
                // leak-free and still fine at the downscaled 1080p encode size.
                match NvencH264Encoder::new(session.device(), config, frame.width, frame.height) {
                    Ok(nvenc) => Ok(Box::new(nvenc)),
                    Err(_) => {
                        let mut mft = MfH264Encoder::new_with_input_size(
                            session.device(),
                            config,
                            frame.width,
                            frame.height,
                        )
                        .map_err(|err| format!("create Media Foundation H.264 encoder: {err}"))?;
                        // Idle desktops produce identical frames; skip
                        // encoding them (heartbeat keyframe every so often)
                        // to cut CPU and heap churn on the software path.
                        mft.set_static_skip(true);
                        Ok(Box::new(MftH264EncoderAdapter(mft)))
                    }
                }
            };

            if encoder.is_none() || encoded_size != (encode_width, encode_height) {
                // Rebuild only when the *resolution* changes. Profile switches
                // (fps/bitrate adaptation) must never tear the encoder down:
                // a session re-init stalls the stream for hundreds of
                // milliseconds, which is the exact jitter the low-latency path
                // exists to avoid.
                let config = video_config(
                    encode_width,
                    encode_height,
                    frame.width,
                    frame.height,
                    desired_profile,
                );
                encoder = Some(build_encoder(config)?);
                encoded_size = (encode_width, encode_height);
                encoded_profile = desired_profile;
            } else if encoded_profile != desired_profile {
                // fps is handled purely by the capture-side frame sampling
                // (`should_encode_next_frame`); only the bitrate needs the
                // encoder. NVENC applies it dynamically; the software MFT
                // has no reconfigure, so fall back to a cheap re-init.
                let applied = desired_profile.bitrate == encoded_profile.bitrate
                    || encoder
                        .as_mut()
                        .expect("encoder initialized")
                        .set_bitrate(desired_profile.bitrate)
                        .is_ok();
                if !applied {
                    let config = video_config(
                        encode_width,
                        encode_height,
                        frame.width,
                        frame.height,
                        desired_profile,
                    );
                    encoded_size = (encode_width, encode_height);
                    encoder = Some(build_encoder(config)?);
                }
                encoded_profile = desired_profile;
            }

            let mut samples = Vec::new();
            if force_keyframe.swap(false, Ordering::AcqRel) {
                encoder
                    .as_ref()
                    .expect("encoder initialized")
                    .force_keyframe();
            }
            let encode_result = encoder
                .as_mut()
                .expect("encoder initialized")
                .encode(
                    &frame.texture,
                    frame.width,
                    frame.height,
                    frame.timestamp,
                    &mut samples,
                );
            if let Err(encode_error) = encode_result {
                return Err(format!(
                    "Media Foundation H.264 encode failed: {encode_error}"
                ));
            }

            for sample in samples {
                if !enqueue_encoded_sample(
                    &sample_tx,
                    EncodedVideoSample {
                        data: sample.data,
                        timestamp: sample.timestamp,
                    },
                ) {
                    return Ok(());
                }
            }
        }
        Ok(())
    }

    /// Cap the encoder resolution to `max_long_edge` on the long side. Keeps
    /// aspect ratio and rounds to even dimensions (NV12 requires even sizes).
    /// The stream geometry for pointer math stays at the native desktop size.
    fn scaled_encode_size(width: u32, height: u32, max_long_edge: u32) -> (u32, u32) {
        let long_edge = width.max(height);
        let scale = if long_edge > max_long_edge {
            max_long_edge as f64 / long_edge as f64
        } else {
            1.0
        };
        let mut w = (width as f64 * scale).round() as u32;
        let mut h = (height as f64 * scale).round() as u32;
        w &= !1;
        h &= !1;
        (w.max(2), h.max(2))
    }

    /// Video codec configuration. Bitrate tiers are derived from the *native*
    /// desktop pixel count, not the (downscaled) encode size: a 1080p encode of
    /// a 4K desktop must still get the high tier instead of collapsing to the
    /// 12 Mbps floor, which previously made every screen soft.
    fn video_config(
        width: u32,
        height: u32,
        native_width: u32,
        native_height: u32,
        profile: StreamProfile,
    ) -> VideoConfig {
        let native_pixels = (native_width as u64).max(1) * (native_height as u64).max(1);
        // Scale the 1080p reference ceiling by pixel count. JetKVM's low-latency
        // encoder scales its target the same way
        // (`internal/native/cgo/video_bitrate.h`: target proportional to
        // width*height/(1920*1080)); the 1080p reference value is our product
        // default (overridable via API_MONITOR_RD_BITRATE_1080P).
        let reference = u64::from(tuning().native_bitrate_1080p);
        let scaled = reference * native_pixels / (1_920 * 1_080);
        let native_bitrate = scaled.clamp(NATIVE_BITRATE_MIN as u64, NATIVE_BITRATE_MAX as u64) as u32;
        let fps = profile.fps.clamp(30, TARGET_FPS);
        VideoConfig {
            width,
            height,
            fps,
            // Floor allows REMB-driven bitrates to go below the old 3 Mbps clamp.
            bitrate: profile
                .bitrate
                .clamp(REMOTE_BITRATE_FLOOR, native_bitrate.max(REMOTE_BITRATE_FLOOR)),
            // One IDR per keyframe_interval_seconds bounds error recovery after
            // packet loss while keeping keyframe overhead small.
            keyframe_interval: fps * tuning().keyframe_interval_seconds,
        }
    }

    /// Apply the receiver's REMB estimate as a ceiling on the target bitrate.
    /// A `remb` of 0 means "no estimate yet" and leaves the profile untouched.
    /// Mirrors JetKVM's sender-side behaviour (`video_remb.go`).
    fn clamp_bitrate_to_remb(mut profile: StreamProfile, remb: u32) -> StreamProfile {
        if remb > 0 && profile.bitrate > remb {
            profile.bitrate = remb;
        }
        profile
    }

    fn should_encode_frame(last: Duration, current: Duration, fps: u32) -> bool {
        let target = Duration::from_nanos(1_000_000_000 / fps.clamp(1, TARGET_FPS) as u64);
        // Capture clocks and nominal refresh rates are not exact: a 60 Hz
        // display commonly reports 16.0-16.6 ms deltas. A strict 16.666 ms
        // comparison accidentally discards alternating frames. Keep a narrow
        // tolerance while still allowing 30 FPS profiles to downsample 60 Hz.
        last.is_zero()
            || current.saturating_sub(last) >= target.saturating_sub(Duration::from_millis(2))
    }

    fn should_encode_next_frame(
        last: Duration,
        current: Duration,
        fps: u32,
        queue_has_capacity: bool,
    ) -> bool {
        queue_has_capacity && should_encode_frame(last, current, fps)
    }

    fn capture_timestamp_to_rtp(base: u32, timestamp: Duration) -> u32 {
        let ticks = timestamp.as_nanos().saturating_mul(RTP_CLOCK_RATE) / 1_000_000_000;
        base.wrapping_add(ticks as u32)
    }

    fn packetize_h264_access_unit(
        payloader: &mut H264Payloader,
        sequence_number: &mut u16,
        timestamp: u32,
        access_unit: Bytes,
    ) -> Result<Vec<Packet>, String> {
        let payloads = payloader
            .payload(RTP_PACKET_MTU - RTP_HEADER_SIZE, &access_unit)
            .map_err(|err| format!("packetize H.264 RTP payload: {err}"))?;
        let last = payloads.len().saturating_sub(1);
        Ok(payloads
            .into_iter()
            .enumerate()
            .map(|(index, payload)| {
                let packet = Packet {
                    header: Header {
                        version: 2,
                        marker: index == last,
                        sequence_number: *sequence_number,
                        timestamp,
                        // TrackLocalStaticRTP replaces both values with the
                        // codec negotiated for the active WebRTC binding.
                        payload_type: 0,
                        ssrc: 0,
                        ..Default::default()
                    },
                    payload,
                };
                *sequence_number = sequence_number.wrapping_add(1);
                packet
            })
            .collect())
    }

    fn handle_channel_message(
        value: Value,
        enigo: &Arc<Mutex<Option<Enigo>>>,
        geometry: &Arc<Mutex<DesktopGeometry>>,
        profile: &Arc<Mutex<StreamProfile>>,
        touch_contact: &Arc<Mutex<Option<ActiveTouchContact>>>,
    ) -> bool {
        if value.get("type").and_then(Value::as_str) == Some("video-config") {
            let fps = value
                .get("fps")
                .and_then(Value::as_u64)
                .unwrap_or(TARGET_FPS as u64)
                .clamp(30, TARGET_FPS as u64) as u32;
            let bitrate = value
                .get("bitrate")
                .and_then(Value::as_u64)
                .unwrap_or(12_000_000)
                .clamp(3_000_000, 40_000_000) as u32;
            // Optional per-session resolution cap. Absent means "keep whatever
            // the current profile uses", so a plain fps/bitrate update does not
            // reset the user's resolution choice.
            let requested_max_long_edge = value
                .get("maxLongEdge")
                .and_then(Value::as_u64)
                .map(|edge| edge.clamp(MIN_MAX_LONG_EDGE as u64, MAX_MAX_LONG_EDGE as u64) as u32);
            if let Ok(mut current) = profile.lock() {
                let max_long_edge = requested_max_long_edge.unwrap_or(current.max_long_edge);
                *current = StreamProfile {
                    fps,
                    bitrate,
                    max_long_edge,
                };
            }
            return true;
        }
        if value.get("type").and_then(Value::as_str) == Some("clipboard-set") {
            let pushed = value
                .get("text")
                .and_then(Value::as_str)
                .filter(|text| !text.is_empty() && text.len() <= 1_048_576)
                .map(write_clipboard_text)
                .unwrap_or(false);
            return pushed;
        }
        handle_input(value, enigo, geometry, touch_contact)
    }

    fn handle_input(
        value: Value,
        enigo: &Arc<Mutex<Option<Enigo>>>,
        geometry: &Arc<Mutex<DesktopGeometry>>,
        touch_contact: &Arc<Mutex<Option<ActiveTouchContact>>>,
    ) -> bool {
        let Ok(mut guard) = enigo.lock() else {
            return false;
        };
        let Some(enigo) = guard.as_mut() else {
            return false;
        };
        match value.get("type").and_then(Value::as_str).unwrap_or("") {
            "pointer" => {
                let geometry = geometry.lock().map(|item| *item).unwrap_or_default();
                let (px, py) = normalized_pointer_position(&value, geometry);
                enigo.move_mouse(px, py, Coordinate::Abs).is_ok()
            }
            "pointer-contact" => {
                let geometry = geometry.lock().map(|item| *item).unwrap_or_default();
                handle_pointer_contact(enigo, &value, geometry)
            }
            "touch-contact" => {
                let geometry = geometry.lock().map(|item| *item).unwrap_or_default();
                handle_touch_contact(enigo, &value, geometry, touch_contact)
            }
            "pointer-relative" => {
                let (dx, dy) = pointer_delta(&value);
                (dx != 0 || dy != 0) && enigo.move_mouse(dx, dy, Coordinate::Rel).is_ok()
            }
            "pointer-query" => true,
            "mouse" => {
                let direction = input_direction(value.get("action").and_then(Value::as_str));
                enigo.button(input_button(&value), direction).is_ok()
            }
            "wheel" => {
                let x = value.get("deltaX").and_then(Value::as_f64).unwrap_or(0.0);
                let y = value.get("deltaY").and_then(Value::as_f64).unwrap_or(0.0);
                let vertical_ok = if y.abs() >= 1.0 {
                    enigo
                        .scroll(
                            (y / 100.0).round().clamp(-12.0, 12.0) as i32,
                            Axis::Vertical,
                        )
                        .is_ok()
                } else {
                    true
                };
                let horizontal_ok = if x.abs() >= 1.0 {
                    enigo
                        .scroll(
                            (x / 100.0).round().clamp(-12.0, 12.0) as i32,
                            Axis::Horizontal,
                        )
                        .is_ok()
                } else {
                    true
                };
                vertical_ok && horizontal_ok
            }
            "key" => {
                let key = browser_key(
                    value.get("key").and_then(Value::as_str).unwrap_or(""),
                    value.get("code").and_then(Value::as_str).unwrap_or(""),
                );
                if let Some(key) = key {
                    enigo
                        .key(
                            key,
                            input_direction(value.get("action").and_then(Value::as_str)),
                        )
                        .is_ok()
                } else {
                    false
                }
            }
            "text" => value
                .get("text")
                .and_then(Value::as_str)
                .filter(|text| !text.is_empty() && text.len() <= 4_096)
                .map(|text| enigo.text(text).is_ok())
                .unwrap_or(false),
            _ => false,
        }
    }

    fn pointer_delta(value: &Value) -> (i32, i32) {
        let dx = value
            .get("dx")
            .and_then(Value::as_f64)
            .unwrap_or(0.0)
            .clamp(-4096.0, 4096.0);
        let dy = value
            .get("dy")
            .and_then(Value::as_f64)
            .unwrap_or(0.0)
            .clamp(-4096.0, 4096.0);
        (dx.round() as i32, dy.round() as i32)
    }

    fn normalized_pointer_position(value: &Value, geometry: DesktopGeometry) -> (i32, i32) {
        let x = value
            .get("x")
            .and_then(Value::as_f64)
            .unwrap_or(0.0)
            .clamp(0.0, 1.0);
        let y = value
            .get("y")
            .and_then(Value::as_f64)
            .unwrap_or(0.0)
            .clamp(0.0, 1.0);
        (
            geometry.x + (x * geometry.width.saturating_sub(1) as f64).round() as i32,
            geometry.y + (y * geometry.height.saturating_sub(1) as f64).round() as i32,
        )
    }

    fn pointer_message_sequence(value: &Value) -> Option<u32> {
        matches!(
            value.get("type").and_then(Value::as_str),
            Some("pointer-query")
        )
        .then(|| value.get("sequence").and_then(Value::as_u64).unwrap_or(0) as u32)
    }

    fn input_button(value: &Value) -> Button {
        match value.get("button").and_then(Value::as_u64).unwrap_or(0) {
            1 => Button::Middle,
            2 => Button::Right,
            _ => Button::Left,
        }
    }

    fn handle_pointer_contact(enigo: &mut Enigo, value: &Value, geometry: DesktopGeometry) -> bool {
        let (px, py) = normalized_pointer_position(value, geometry);
        if enigo.move_mouse(px, py, Coordinate::Abs).is_err() {
            return false;
        }
        match value
            .get("action")
            .and_then(Value::as_str)
            .unwrap_or("move")
        {
            "down" | "press" => enigo.button(input_button(value), Direction::Press).is_ok(),
            "up" | "release" | "cancel" => enigo
                .button(input_button(value), Direction::Release)
                .is_ok(),
            "click" => enigo.button(input_button(value), Direction::Click).is_ok(),
            _ => true,
        }
    }

    fn touch_pointer_flags(action: &str) -> u32 {
        match action {
            "down" | "press" => POINTER_FLAG_DOWN | POINTER_FLAG_INRANGE | POINTER_FLAG_INCONTACT,
            "up" | "release" => POINTER_FLAG_UP,
            "cancel" => POINTER_FLAG_UP | POINTER_FLAG_CANCELED,
            _ => POINTER_FLAG_UPDATE | POINTER_FLAG_INRANGE | POINTER_FLAG_INCONTACT,
        }
    }

    fn inject_touch_at(action: &str, x: i32, y: i32) -> bool {
        static TOUCH_INJECTION_READY: OnceLock<bool> = OnceLock::new();
        let ready = *TOUCH_INJECTION_READY
            .get_or_init(|| unsafe { InitializeTouchInjection(1, TOUCH_FEEDBACK_DEFAULT) != 0 });
        if !ready {
            return false;
        }
        let mut contact = POINTER_TOUCH_INFO::default();
        contact.pointerInfo.pointerType = PT_TOUCH;
        contact.pointerInfo.pointerId = 1;
        contact.pointerInfo.pointerFlags = touch_pointer_flags(action);
        contact.pointerInfo.ptPixelLocation = POINT { x, y };
        contact.touchMask = TOUCH_MASK_CONTACTAREA | TOUCH_MASK_ORIENTATION | TOUCH_MASK_PRESSURE;
        contact.rcContact = RECT {
            left: x.saturating_sub(2),
            top: y.saturating_sub(2),
            right: x.saturating_add(2),
            bottom: y.saturating_add(2),
        };
        contact.orientation = 90;
        contact.pressure = 32_000;
        unsafe { InjectTouchInput(1, &contact) != 0 }
    }

    fn handle_touch_contact(
        enigo: &mut Enigo,
        value: &Value,
        geometry: DesktopGeometry,
        active_contact: &Arc<Mutex<Option<ActiveTouchContact>>>,
    ) -> bool {
        let action = value
            .get("action")
            .and_then(Value::as_str)
            .unwrap_or("move");
        let (x, y) = normalized_pointer_position(value, geometry);
        let Ok(mut active) = active_contact.lock() else {
            return false;
        };
        match action {
            "down" | "press" => {
                if let Some(previous) = active.take() {
                    release_touch_mode(previous, enigo);
                }
                if inject_touch_at("down", x, y) {
                    *active = Some(ActiveTouchContact::Native { x, y });
                    true
                } else if handle_pointer_contact(enigo, value, geometry) {
                    *active = Some(ActiveTouchContact::Mouse);
                    true
                } else {
                    false
                }
            }
            "up" | "release" | "cancel" => {
                let Some(previous) = active.take() else {
                    return true;
                };
                match previous {
                    ActiveTouchContact::Native {
                        x: last_x,
                        y: last_y,
                    } => {
                        let released = inject_touch_at(action, x, y);
                        if !released {
                            let _ = inject_touch_at("cancel", last_x, last_y);
                        }
                        released
                    }
                    ActiveTouchContact::Mouse => handle_pointer_contact(enigo, value, geometry),
                }
            }
            _ => match *active {
                Some(ActiveTouchContact::Native { .. }) => {
                    if inject_touch_at("move", x, y) {
                        *active = Some(ActiveTouchContact::Native { x, y });
                        true
                    } else {
                        false
                    }
                }
                Some(ActiveTouchContact::Mouse) => handle_pointer_contact(enigo, value, geometry),
                None => false,
            },
        }
    }

    fn release_touch_mode(contact: ActiveTouchContact, enigo: &mut Enigo) {
        match contact {
            ActiveTouchContact::Native { x, y } => {
                let _ = inject_touch_at("cancel", x, y);
            }
            ActiveTouchContact::Mouse => {
                let _ = enigo.button(Button::Left, Direction::Release);
            }
        }
    }

    /// Read the current Windows clipboard as UTF-16 text. Returns `None` when
    /// the clipboard is held by another process or holds no text.
    fn read_clipboard_text() -> Option<String> {
        unsafe {
            if OpenClipboard(std::ptr::null_mut()) == 0 {
                return None;
            }
            let result = (|| {
                if IsClipboardFormatAvailable(CF_UNICODETEXT) == 0 {
                    return None;
                }
                let handle = GetClipboardData(CF_UNICODETEXT);
                if handle.is_null() {
                    return None;
                }
                let ptr = GlobalLock(handle);
                if ptr.is_null() {
                    return None;
                }
                let size = GlobalSize(handle);
                let text = if size >= 2 {
                    let units = size / 2;
                    let slice = std::slice::from_raw_parts(ptr as *const u16, units);
                    let len = slice.iter().position(|&unit| unit == 0).unwrap_or(units);
                    Some(String::from_utf16_lossy(&slice[..len]))
                } else {
                    None
                };
                GlobalUnlock(handle);
                text
            })();
            CloseClipboard();
            result
        }
    }

    /// Replace the Windows clipboard with the given text. Returns `false`
    /// when another process holds the clipboard.
    fn write_clipboard_text(text: &str) -> bool {
        unsafe {
            if OpenClipboard(std::ptr::null_mut()) == 0 {
                return false;
            }
            let result = (|| {
                if EmptyClipboard() == 0 {
                    return false;
                }
                let utf16: Vec<u16> = text.encode_utf16().chain(std::iter::once(0)).collect();
                let handle = GlobalAlloc(GMEM_MOVEABLE, utf16.len() * 2);
                if handle.is_null() {
                    return false;
                }
                let ptr = GlobalLock(handle);
                if ptr.is_null() {
                    GlobalFree(handle);
                    return false;
                }
                std::ptr::copy_nonoverlapping(utf16.as_ptr(), ptr as *mut u16, utf16.len());
                GlobalUnlock(handle);
                // `SetClipboardData` takes ownership of the handle on success.
                if SetClipboardData(CF_UNICODETEXT, handle).is_null() {
                    GlobalFree(handle);
                    return false;
                }
                true
            })();
            CloseClipboard();
            result
        }
    }

    /// Watch the clipboard sequence counter and push text changes to the peer
    /// over the control channel. Runs until `stop` is set; the loop suppresses
    /// echoes of text this side just wrote (`last_sent` dedup).
    fn spawn_clipboard_watcher(channel: Arc<RTCDataChannel>, stop: Arc<AtomicBool>) {
        tokio::spawn(async move {
            let mut interval = tokio::time::interval(Duration::from_millis(500));
            interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
            let mut last_sequence = unsafe { GetClipboardSequenceNumber() };
            let mut last_sent: Option<String> = None;
            loop {
                interval.tick().await;
                if stop.load(Ordering::Relaxed) {
                    break;
                }
                let sequence = unsafe { GetClipboardSequenceNumber() };
                if sequence == last_sequence {
                    continue;
                }
                last_sequence = sequence;
                let read = tokio::task::spawn_blocking(read_clipboard_text)
                    .await
                    .unwrap_or(None);
                let Some(text) = read else { continue };
                if text.is_empty() || last_sent.as_deref() == Some(text.as_str()) {
                    continue;
                }
                last_sent = Some(text.clone());
                // A closed data channel makes the send fail; treat that as the
                // session ending and stop watching (the peer teardown also
                // sets `stop`).
                if channel
                    .send_text(json!({"type": "clipboard", "text": text}).to_string())
                    .await
                    .is_err()
                {
                    break;
                }
            }
        });
    }

    fn release_active_touch(
        active_contact: &Arc<Mutex<Option<ActiveTouchContact>>>,
        enigo: &Arc<Mutex<Option<Enigo>>>,
    ) {
        let active = active_contact
            .lock()
            .ok()
            .and_then(|mut contact| contact.take());
        let Some(active) = active else {
            return;
        };
        if let Ok(mut guard) = enigo.lock() {
            if let Some(enigo) = guard.as_mut() {
                release_touch_mode(active, enigo);
            } else if let ActiveTouchContact::Native { x, y } = active {
                let _ = inject_touch_at("cancel", x, y);
            }
        }
    }

    fn pointer_position_message(
        enigo: &Arc<Mutex<Option<Enigo>>>,
        geometry: &Arc<Mutex<DesktopGeometry>>,
        sequence: u32,
    ) -> Option<String> {
        let (x, y) = enigo.lock().ok()?.as_ref()?.location().ok()?;
        let geometry = geometry.lock().ok().map(|item| *item)?;
        let width = geometry.width.saturating_sub(1).max(1) as f64;
        let height = geometry.height.saturating_sub(1).max(1) as f64;
        Some(
            json!({
                "type": "pointer-position",
                "sequence": sequence,
                "x": ((x - geometry.x) as f64 / width).clamp(0.0, 1.0),
                "y": ((y - geometry.y) as f64 / height).clamp(0.0, 1.0),
            })
            .to_string(),
        )
    }

    fn input_direction(action: Option<&str>) -> Direction {
        match action.unwrap_or("") {
            "down" | "press" => Direction::Press,
            "up" | "release" => Direction::Release,
            _ => Direction::Click,
        }
    }

    fn browser_key(key: &str, code: &str) -> Option<Key> {
        Some(match key {
            "Backspace" => Key::Backspace,
            "Tab" => Key::Tab,
            "Enter" => Key::Return,
            "Shift" => Key::Shift,
            "Control" => Key::Control,
            "Alt" => Key::Alt,
            "Meta" => Key::Meta,
            "Escape" => Key::Escape,
            " " => Key::Space,
            "PageUp" => Key::PageUp,
            "PageDown" => Key::PageDown,
            "End" => Key::End,
            "Home" => Key::Home,
            "ArrowLeft" => Key::LeftArrow,
            "ArrowUp" => Key::UpArrow,
            "ArrowRight" => Key::RightArrow,
            "ArrowDown" => Key::DownArrow,
            "Delete" => Key::Delete,
            "F1" => Key::F1,
            "F2" => Key::F2,
            "F3" => Key::F3,
            "F4" => Key::F4,
            "F5" => Key::F5,
            "F6" => Key::F6,
            "F7" => Key::F7,
            "F8" => Key::F8,
            "F9" => Key::F9,
            "F10" => Key::F10,
            "F11" => Key::F11,
            "F12" => Key::F12,
            _ if key.chars().count() == 1 => Key::Unicode(key.chars().next()?),
            _ if code.starts_with("Key") && code.len() == 4 => {
                Key::Unicode(code.chars().nth(3)?.to_ascii_lowercase())
            }
            _ => return None,
        })
    }

    #[cfg(test)]
    mod tests {
        use super::*;

        #[test]
        fn video_profile_is_bounded_for_low_latency_streaming() {
            // The ceiling follows the *native* pixels: a 4K desktop gets 4x the
            // 1080p reference ceiling (20 Mbps), clamped to NATIVE_BITRATE_MAX.
            let config = video_config(
                1_920,
                1_080,
                3_840,
                2_160,
                StreamProfile {
                    fps: 10,
                    bitrate: 50_000_000,
                    max_long_edge: 0,
                },
            );
            assert_eq!(config.fps, 30);
            assert_eq!(config.bitrate, 50_000_000);
            // One IDR per second at the clamped 30 fps floor.
            assert_eq!(config.keyframe_interval, 30);
        }

        #[test]
        fn video_config_caps_the_request_to_the_native_tier() {
            // A 1080p desktop must not accept more than the 1080p reference
            // ceiling even if the client asks for more.
            let config = video_config(
                1_920,
                1_080,
                1_920,
                1_080,
                StreamProfile {
                    fps: 30,
                    bitrate: 50_000_000,
                    max_long_edge: 0,
                },
            );
            assert_eq!(config.bitrate, NATIVE_BITRATE_1080P);
        }

        #[test]
        fn mobile_safe_default_profile_limits_startup_pressure() {
            assert_eq!(
                StreamProfile::default(),
                StreamProfile {
                    fps: 30,
                    bitrate: 6_000_000,
                    max_long_edge: 0,
                }
            );
        }

        #[test]
        fn scaled_encode_size_caps_large_desktops_and_keeps_even_dimensions() {
            const CAP: u32 = DEFAULT_MAX_LONG_EDGE;
            // 4K is capped to the default long edge, even, aspect-preserving.
            // DEFAULT_MAX_LONG_EDGE mirrors the dashboard's `balanced` preset so a
            // profile-less start does not stream at a size the UI does not report.
            assert_eq!(CAP, 1_920);
            let (w, h) = scaled_encode_size(3_840, 2_160, CAP);
            assert_eq!((w, h), (1_920, 1_080));
            assert_eq!(w % 2, 0);
            assert_eq!(h % 2, 0);

            // A non-16:9 4K desktop keeps its aspect ratio under the cap.
            let (w, h) = scaled_encode_size(3_440, 1_440, CAP);
            assert_eq!((w, h), (1_920, 804));

            // Below the cap the native size is preserved (still even).
            let (w, h) = scaled_encode_size(1_280, 720, CAP);
            assert_eq!((w, h), (1_280, 720));

            // An explicit higher cap (e.g. the "sharp"/"ultra" preset) is honoured.
            let (w, h) = scaled_encode_size(3_840, 2_160, 2_560);
            assert_eq!((w, h), (2_560, 1_440));
            let (w, h) = scaled_encode_size(3_840, 2_160, 3_840);
            assert_eq!((w, h), (3_840, 2_160));

            // A lower per-session cap (e.g. the "smooth" preset) is honoured.
            let (w, h) = scaled_encode_size(3_840, 2_160, 1_280);
            assert_eq!((w, h), (1_280, 720));

            // Odd desktop sizes are rounded down to even dimensions.
            let (w, h) = scaled_encode_size(1_365, 767, CAP);
            assert_eq!(w % 2, 0);
            assert_eq!(h % 2, 0);
            assert!(w <= 1_365 && h <= 767);
        }

        #[test]
        fn start_profile_payload_is_clamped_like_video_config() {
            // 面板下发的初始档位必须与 `video-config` 走同一套钳制规则，避免
            // 建会话与后续改档两条路径产生不一致的分辨率/帧率。
            let profile = StartProfilePayload {
                fps: Some(60),
                bitrate: Some(12_000_000),
                max_long_edge: Some(1_920),
            }
            .to_stream_profile();
            assert_eq!(profile.fps, 60);
            assert_eq!(profile.bitrate, 12_000_000);
            assert_eq!(profile.max_long_edge, 1_920);

            // 帧率上限是 TARGET_FPS(60)，超出的请求被钳制而不是透传。
            let profile = StartProfilePayload {
                fps: Some(144),
                bitrate: Some(999_000_000),
                max_long_edge: Some(9_999),
            }
            .to_stream_profile();
            assert_eq!(profile.fps, TARGET_FPS);
            assert_eq!(profile.bitrate, 40_000_000);
            assert_eq!(profile.max_long_edge, MAX_MAX_LONG_EDGE);

            // 低于下限的请求同样被抬高，保证编码器拿到合法参数。
            let profile = StartProfilePayload {
                fps: Some(5),
                bitrate: Some(1),
                max_long_edge: Some(1),
            }
            .to_stream_profile();
            assert_eq!(profile.fps, 30);
            assert_eq!(profile.bitrate, 3_000_000);
            assert_eq!(profile.max_long_edge, MIN_MAX_LONG_EDGE);

            // 未提供 profile 时回落到默认档（max_long_edge = 0 表示用 tuning 默认）。
            let profile = StartProfilePayload::default().to_stream_profile();
            assert_eq!(profile.fps, StreamProfile::default().fps);
            assert_eq!(profile.max_long_edge, 0);
        }

        /// 面板（Go）下发的 JSON 使用 camelCase（`maxLongEdge`），而本结构体字段是
        /// snake_case。若不做 rename，`maxLongEdge` 会被 serde 静默丢弃、初始分辨率
        /// 上限退回默认值——这类「字段名对不上但解析成功」的缺陷不会报错，只能靠
        /// 用例锁住。这里同时覆盖完整 start 载荷的解析。
        #[test]
        fn start_payload_accepts_the_dashboard_camel_case_profile() {
            let raw = r#"{
                "session_id": "s1",
                "offer": {"type": "offer", "sdp": "v=0"},
                "profile": {"fps": 60, "bitrate": 24000000, "maxLongEdge": 2560}
            }"#;
            let payload: StartPayload = serde_json::from_str(raw).expect("start payload 应可解析");
            let profile = payload
                .profile
                .expect("profile 字段应存在")
                .to_stream_profile();
            assert_eq!(profile.fps, 60);
            assert_eq!(profile.bitrate, 24_000_000);
            assert_eq!(
                profile.max_long_edge, 2_560,
                "camelCase 的 maxLongEdge 必须被解析，否则初始分辨率上限被静默忽略"
            );
        }

        /// 缺少 profile 时不得报错（旧面板 / 兼容路径）。
        #[test]
        fn start_payload_without_profile_still_parses() {
            let raw = r#"{"session_id":"s2","offer":{"type":"offer","sdp":"v=0"}}"#;
            let payload: StartPayload = serde_json::from_str(raw).expect("无 profile 也应可解析");
            assert!(payload.profile.is_none());
        }

        #[test]
        fn desktop_interface_filter_drops_tunnels_but_keeps_physical_nics() {
            assert!(should_use_interface_for_desktop("以太网"));
            assert!(should_use_interface_for_desktop("Ethernet"));
            assert!(should_use_interface_for_desktop("Wi-Fi"));
            assert!(should_use_interface_for_desktop("WLAN"));
            // Loopback / virtual / container adapters are already excluded by
            // the shared nat filter.
            assert!(!should_use_interface_for_desktop("lo"));
            assert!(!should_use_interface_for_desktop("docker0"));
            // Windows TUN-mode proxies must not contribute ICE candidates.
            assert!(!should_use_interface_for_desktop("wintun"));
            assert!(!should_use_interface_for_desktop("Clash"));
            assert!(!should_use_interface_for_desktop("Mihomo"));
            assert!(!should_use_interface_for_desktop("sing-box"));
            assert!(!should_use_interface_for_desktop("WireGuard Tunnel"));
            assert!(!should_use_interface_for_desktop("OpenVPN TAP-Windows6"));
        }

        #[test]
        fn tuning_defaults_match_the_documented_product_defaults() {
            let tuning = RemoteDesktopTuning::from_lookup(|_| None);
            assert_eq!(tuning.max_long_edge, DEFAULT_MAX_LONG_EDGE);
            assert_eq!(tuning.native_bitrate_1080p, NATIVE_BITRATE_1080P);
            assert_eq!(tuning.keyframe_interval_seconds, KEYFRAME_INTERVAL_SECONDS);
            assert_eq!(tuning.queue_depth, ENCODED_QUEUE_DEPTH);
            assert!(tuning.nat_1to1_ips.is_empty());
            assert_eq!(tuning.nat_1to1_candidate_type, RTCIceCandidateType::Host);
        }

        #[test]
        fn tuning_reads_and_clamps_environment_overrides() {
            let tuning = RemoteDesktopTuning::from_lookup(|key| {
                match key {
                    "API_MONITOR_RD_MAX_LONG_EDGE" => Some("1024".to_string()), // below MIN
                    "API_MONITOR_RD_BITRATE_1080P" => Some(" 12345678 ".to_string()),
                    "API_MONITOR_RD_KEYFRAME_SECONDS" => Some("99".to_string()), // above max
                    "API_MONITOR_RD_QUEUE_DEPTH" => Some("5".to_string()),
                    "API_MONITOR_RD_NAT_1TO1_IPS" => {
                        Some("203.0.113.7, 198.51.100.9 ,".to_string())
                    }
                    "API_MONITOR_RD_NAT_1TO1_TYPE" => Some("Srflx".to_string()),
                    _ => None,
                }
            });
            assert_eq!(tuning.max_long_edge, MIN_MAX_LONG_EDGE);
            assert_eq!(tuning.native_bitrate_1080p, 12_345_678);
            assert_eq!(tuning.keyframe_interval_seconds, 10);
            assert_eq!(tuning.queue_depth, 5);
            assert_eq!(
                tuning.nat_1to1_ips,
                vec!["203.0.113.7".to_string(), "198.51.100.9".to_string()]
            );
            assert_eq!(tuning.nat_1to1_candidate_type, RTCIceCandidateType::Srflx);
        }

        #[test]
        fn parse_candidate_type_accepts_the_supported_names() {
            assert_eq!(parse_candidate_type("HOST"), Some(RTCIceCandidateType::Host));
            assert_eq!(parse_candidate_type(" srflx "), Some(RTCIceCandidateType::Srflx));
            // webrtc-rs rejects prflx/relay for 1:1 NAT; treat as unset.
            assert_eq!(parse_candidate_type("prflx"), None);
            assert_eq!(parse_candidate_type("relay"), None);
            assert_eq!(parse_candidate_type("bogus"), None);
        }

        #[test]
        fn remb_clamps_the_target_bitrate_only_when_lower() {
            let profile = StreamProfile {
                fps: 30,
                bitrate: 20_000_000,
                max_long_edge: 0,
            };
            // No estimate yet: leave the profile alone.
            assert_eq!(clamp_bitrate_to_remb(profile, 0).bitrate, 20_000_000);
            // Estimate above the target: still no change.
            assert_eq!(clamp_bitrate_to_remb(profile, 30_000_000).bitrate, 20_000_000);
            // Estimate below the target: clamp down, keeping the other fields.
            let clamped = clamp_bitrate_to_remb(profile, 4_000_000);
            assert_eq!(clamped.bitrate, 4_000_000);
            assert_eq!(clamped.fps, profile.fps);
            assert_eq!(clamped.max_long_edge, profile.max_long_edge);
        }

        #[test]
        fn skips_encoding_when_the_latest_frame_queue_is_full() {
            assert!(!should_encode_next_frame(
                Duration::ZERO,
                Duration::from_millis(16),
                60,
                false,
            ));
        }

        #[test]
        fn failed_peers_shutdown_immediately_while_disconnects_get_a_short_grace() {
            assert_eq!(
                peer_shutdown_delay(RTCPeerConnectionState::Failed),
                Some(Duration::ZERO)
            );
            assert_eq!(
                peer_shutdown_delay(RTCPeerConnectionState::Disconnected),
                Some(PEER_DISCONNECT_GRACE)
            );
            assert_eq!(peer_shutdown_delay(RTCPeerConnectionState::Connected), None);
        }

        #[test]
        fn full_encoded_queue_does_not_block_capture_worker() {
            let (tx, rx) = tokio::sync::mpsc::channel(1);
            assert!(enqueue_encoded_sample(
                &tx,
                EncodedVideoSample {
                    data: vec![1],
                    timestamp: Duration::ZERO,
                }
            ));

            let (done_tx, done_rx) = std::sync::mpsc::channel();
            let worker = std::thread::spawn(move || {
                let keep_running = enqueue_encoded_sample(
                    &tx,
                    EncodedVideoSample {
                        data: vec![2],
                        timestamp: Duration::ZERO,
                    },
                );
                let _ = done_tx.send(keep_running);
            });

            let completed = done_rx.recv_timeout(Duration::from_millis(100));
            drop(rx);
            worker.join().expect("capture worker should exit");

            assert_eq!(
                completed,
                Ok(true),
                "a full video queue must drop instead of blocking"
            );
        }

        #[test]
        fn keeps_frames_from_a_real_world_60hz_capture_clock() {
            let last = Duration::from_secs(1);
            assert!(should_encode_frame(
                last,
                last + Duration::from_micros(16_100),
                60
            ));
            assert!(!should_encode_frame(
                last,
                last + Duration::from_micros(16_100),
                30
            ));
        }

        #[test]
        fn touchpad_delta_is_independent_of_desktop_geometry() {
            assert_eq!(pointer_delta(&json!({"dx": 25, "dy": -12})), (25, -12));
            assert_eq!(
                pointer_delta(&json!({"dx": 9999, "dy": -9999})),
                (4096, -4096)
            );
        }

        #[test]
        fn normalized_direct_touch_maps_across_offset_desktop_geometry() {
            let geometry = DesktopGeometry {
                x: -1_920,
                y: 100,
                width: 1_920,
                height: 1_080,
            };
            assert_eq!(
                normalized_pointer_position(&json!({"x": 0.0, "y": 0.0}), geometry),
                (-1_920, 100)
            );
            assert_eq!(
                normalized_pointer_position(&json!({"x": 1.0, "y": 1.0}), geometry),
                (-1, 1_179)
            );
            assert_eq!(
                normalized_pointer_position(&json!({"x": 0.5, "y": 0.5}), geometry),
                (-960, 640)
            );
        }

        #[test]
        fn only_pointer_queries_receive_position_sequences() {
            assert_eq!(
                pointer_message_sequence(&json!({"type": "pointer-query", "sequence": 7})),
                Some(7)
            );
            assert_eq!(
                pointer_message_sequence(&json!({"type": "pointer-relative", "sequence": 8})),
                None
            );
            assert_eq!(
                pointer_message_sequence(&json!({"type": "pointer", "sequence": 9})),
                None
            );
        }

        #[test]
        fn direct_touch_contact_flags_follow_windows_pointer_lifecycle() {
            assert_eq!(
                touch_pointer_flags("down"),
                POINTER_FLAG_DOWN | POINTER_FLAG_INRANGE | POINTER_FLAG_INCONTACT
            );
            assert_eq!(
                touch_pointer_flags("move"),
                POINTER_FLAG_UPDATE | POINTER_FLAG_INRANGE | POINTER_FLAG_INCONTACT
            );
            assert_eq!(touch_pointer_flags("up"), POINTER_FLAG_UP);
            assert_eq!(
                touch_pointer_flags("cancel"),
                POINTER_FLAG_UP | POINTER_FLAG_CANCELED
            );
        }

        #[test]
        fn sparse_capture_advances_the_current_rtp_timestamp_by_the_real_gap() {
            let base = 0x1234_5678;
            let first = capture_timestamp_to_rtp(base, Duration::from_secs(1));
            let after_pause = capture_timestamp_to_rtp(base, Duration::from_secs(3));
            assert_eq!(after_pause.wrapping_sub(first), 180_000);
        }

        #[test]
        fn sixty_hz_capture_clock_advances_by_1500_rtp_ticks() {
            let base = 77;
            let first = capture_timestamp_to_rtp(base, Duration::ZERO);
            let second = capture_timestamp_to_rtp(base, Duration::from_nanos(16_666_667));
            let third = capture_timestamp_to_rtp(base, Duration::from_nanos(33_333_334));
            assert_eq!(second.wrapping_sub(first), 1_500);
            assert_eq!(third.wrapping_sub(second), 1_500);
        }

        #[test]
        fn capture_clock_wraps_as_a_32_bit_rtp_timestamp() {
            let base = u32::MAX - 10;
            assert_eq!(
                capture_timestamp_to_rtp(base, Duration::from_secs(1)),
                base.wrapping_add(90_000)
            );
        }

        #[test]
        fn h264_access_unit_uses_one_timestamp_and_marks_only_the_last_packet() {
            let mut access_unit = vec![
                0, 0, 0, 1, 0x67, 0x42, 0xe0, 0x1f, 0, 0, 0, 1, 0x68, 0xce, 0x06, 0xe2, 0, 0, 0, 1,
                0x65,
            ];
            access_unit.extend(std::iter::repeat(0x55).take(2_500));
            let mut payloader = H264Payloader::default();
            let mut sequence = u16::MAX;
            let packets = packetize_h264_access_unit(
                &mut payloader,
                &mut sequence,
                123_456,
                Bytes::from(access_unit),
            )
            .expect("packetize access unit");

            assert!(packets.len() >= 3);
            assert!(packets
                .iter()
                .all(|packet| packet.header.timestamp == 123_456));
            assert!(packets[..packets.len() - 1]
                .iter()
                .all(|packet| !packet.header.marker));
            assert!(packets.last().expect("last packet").header.marker);
            assert_eq!(packets[0].header.sequence_number, u16::MAX);
            assert_eq!(packets[1].header.sequence_number, 0);
        }
    }
}

#[cfg(not(target_os = "windows"))]
mod unsupported {
    use serde::Deserialize;
    use serde_json::Value;

    use crate::OutboundQueues;

    #[derive(Debug, Deserialize)]
    pub struct StartPayload {
        pub session_id: String,
    }

    #[derive(Debug, Deserialize)]
    pub struct SignalPayload {
        pub session_id: String,
        pub signal: Value,
    }

    #[derive(Debug, Deserialize)]
    pub struct StopPayload {
        pub session_id: String,
    }

    #[derive(Default)]
    pub struct RemoteDesktopManager;

    impl RemoteDesktopManager {
        pub fn new() -> Self {
            Self
        }
        pub async fn start(
            &self,
            _payload: StartPayload,
            _outbound: OutboundQueues,
        ) -> Result<(), String> {
            Err("remote desktop is only supported on Windows".to_string())
        }
        pub async fn signal(&self, payload: SignalPayload) -> Result<(), String> {
            let _ = (payload.session_id, payload.signal);
            Ok(())
        }
        pub async fn stop(&self, _session_id: &str) {}
    }
}

#[cfg(not(target_os = "windows"))]
pub use unsupported::*;
#[cfg(target_os = "windows")]
pub use windows_impl::*;
