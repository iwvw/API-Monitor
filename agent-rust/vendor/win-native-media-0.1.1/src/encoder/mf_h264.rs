//! Media Foundation H.264 encoder MFT.
//!
//! Enumerates a hardware (async, D3D11-aware) H.264 encoder by default, feeding
//! it zero-copy NV12 textures from an MFT-owned sample allocator, with a
//! software (sync) MFT fallback for systems without a usable hardware encoder.
//! Output is emitted as Annex-B `EncodedSample`s.
//!
//! Threading: MF requires MTA. `MfH264Encoder::new` calls `CoInitializeEx`
//! (MTA) + `MFStartup` on the constructing thread; keep the encoder on one
//! thread. The pipeline runs it on a dedicated encode thread.

use std::sync::OnceLock;
use std::time::Duration;

use windows::core::{Interface, GUID, PCWSTR};
use windows::Win32::Graphics::Direct3D11::ID3D11Device;
use windows::Win32::Graphics::Direct3D11::ID3D11Texture2D;
use windows::Win32::Media::MediaFoundation::*;
use windows::Win32::System::Com::{CoInitializeEx, CoUninitialize, COINIT_MULTITHREADED};

use super::{EncodedSample, ParameterSets};
use crate::convert::Bgra2Nv12;
use crate::{PipelineError, Result, VideoConfig};

/// 100ns ticks per second (MF time base).
const HNS_PER_SEC: i64 = 10_000_000;

/// Static-frame detection: when the desktop content is unchanged, the software
/// H.264 MFT is expensive to run per frame. Skip encoding identical frames and
/// emit a heartbeat keyframe every so often so the WebRTC decoder stays healthy.
const STATIC_HEARTBEAT_FRAMES: u32 = 60;

pub struct MfH264Encoder {
    transform: IMFTransform,
    input_stream_id: u32,
    output_stream_id: u32,
    device_manager: IMFDXGIDeviceManager,
    cfg: VideoConfig,
    params: ParameterSets,
    /// Whether the MFT is an async (hardware) MFT driven by events.
    is_async: bool,
    event_gen: Option<IMFMediaEventGenerator>,
    started: bool,
    /// Outstanding METransformNeedInput requests the async MFT has issued that
    /// we have not yet satisfied with a ProcessInput. Persisted across encode()
    /// calls so we never drop a request (dropping one deadlocks the MFT).
    pending_input_requests: u32,
    /// BGRA->NV12 converter, present only when the MFT requires NV12 input.
    converter: Option<Bgra2Nv12>,
    /// Reused CPU NV12 frame buffer for the software encoder path.
    cpu_nv12: Vec<u8>,
    /// Sample allocator for the hardware D3D11-aware MFT. When present, input
    /// samples come from here (MFT-owned NV12 textures) instead of textures we
    /// create — hardware MFTs reject foreign textures at ProcessInput.
    allocator: Option<IMFVideoSampleAllocatorEx>,
    /// Static-frame skip (software path only). Enables skipping encoding when
    /// the frame content is unchanged since the previous frame.
    static_skip: bool,
    /// FNV-1a hash of the last encoded CPU NV12 frame, for change detection.
    last_frame_hash: Option<u64>,
    /// Consecutive identical frames skipped since the last emitted sample.
    static_frames_skipped: u32,
    // Declared last so COM is uninitialized after every MFT/DXGI field is
    // released. Media Foundation itself is process-scoped and remains started
    // until process exit, as required by the MF lifetime contract.
    _runtime: MediaFoundationRuntime,
}

struct MediaFoundationRuntime {
    com_initialized: bool,
}

impl MediaFoundationRuntime {
    fn start() -> Result<Self> {
        unsafe {
            let com_initialized = CoInitializeEx(None, COINIT_MULTITHREADED).is_ok();
            static MF_STARTED: OnceLock<std::result::Result<(), i32>> = OnceLock::new();
            let mf_result = MF_STARTED.get_or_init(|| {
                MFStartup(MF_VERSION, MFSTARTUP_FULL).map_err(|error| error.code().0)
            });
            if let Err(code) = mf_result {
                if com_initialized {
                    CoUninitialize();
                }
                return Err(PipelineError::TypeNegotiation(
                    format!("Media Foundation startup failed: HRESULT 0x{code:08x}").into(),
                ));
            }
            Ok(Self { com_initialized })
        }
    }
}

impl Drop for MediaFoundationRuntime {
    fn drop(&mut self) {
        unsafe {
            if self.com_initialized {
                CoUninitialize();
            }
        }
    }
}

impl MfH264Encoder {
    /// Create the encoder sharing `device`.
    ///
    /// Creates the software (sync) H.264 MFT by default.
    pub fn new(device: &ID3D11Device, cfg: VideoConfig) -> Result<Self> {
        Self::new_with(device, cfg, false)
    }

    /// Create the encoder assuming the captured frames are `input_width` x
    /// `input_height` and the encoder output should be `cfg.width` x
    /// `cfg.height`. The video processor scales input to output, which lets
    /// remote desktop encode a downscaled stream (huge win for software H.264).
    /// Uses the software (sync) MFT.
    pub fn new_with_input_size(
        device: &ID3D11Device,
        cfg: VideoConfig,
        input_width: u32,
        input_height: u32,
    ) -> Result<Self> {
        Self::new_with_input_size_and_hw(device, cfg, input_width, input_height, false)
    }

    /// Like [`Self::new_with_input_size`], but tries the hardware (async,
    /// D3D11-aware) H.264 MFT first when `prefer_hardware` is true. Kept
    /// private: the hardware MFT path leaks driver resources across repeated
    /// sessions (verified by remote_desktop_resource_smoke), so callers should
    /// not opt into it.
    fn new_with_input_size_and_hw(
        device: &ID3D11Device,
        cfg: VideoConfig,
        input_width: u32,
        input_height: u32,
        prefer_hardware: bool,
    ) -> Result<Self> {
        Self::build(device, cfg, input_width, input_height, prefer_hardware)
    }

    /// Create the encoder, explicitly choosing hardware or software.
    pub fn new_with(
        device: &ID3D11Device,
        cfg: VideoConfig,
        prefer_hardware: bool,
    ) -> Result<Self> {
        Self::build(device, cfg, cfg.width, cfg.height, prefer_hardware)
    }

    /// Shared construction: input frames are `input_width` x `input_height`,
    /// encoder output is `cfg.width` x `cfg.height`. When the input is larger
    /// than the output the video processor downscales during the BGRA->NV12
    /// conversion, so the H.264 encoder never sees the full desktop size.
    ///
    /// `prefer_hardware` tries the hardware (async, D3D11-aware) H.264 MFT
    /// first. If no hardware encoder is available or initialization fails the
    /// caller may fall back to software. The hardware path feeds MFT-owned NV12
    /// textures from an [`IMFVideoSampleAllocatorEx`] (zero-copy), so it needs
    /// the shared D3D11 device and never allocates CPU buffers.
    fn build(
        device: &ID3D11Device,
        cfg: VideoConfig,
        input_width: u32,
        input_height: u32,
        prefer_hardware: bool,
    ) -> Result<Self> {
        unsafe {
            // MF needs MTA. The guard balances both startup calls on successful
            // construction and on every early-return error path.
            let runtime = MediaFoundationRuntime::start()?;

            let device_manager = create_device_manager(device)?;
            let transform = enumerate_h264_encoder(prefer_hardware)?;
            let (input_id, output_id) = stream_ids(&transform)?;

            // Detect async (hardware) MFT via attribute.
            let is_async = mft_is_async(&transform);

            // Hand the D3D device manager only to hardware MFTs (software MFTs
            // take CPU buffers and can choke on a D3D manager).
            if is_async {
                let _ = transform.ProcessMessage(
                    MFT_MESSAGE_SET_D3D_MANAGER,
                    std::mem::transmute::<_, usize>(device_manager.clone()),
                );
            }

            if is_async {
                // Unlock async MFT so we may use event-driven processing, and
                // request low-latency mode (Intel Quick Sync MFT wants this for
                // real-time D3D input).
                if let Ok(attrs) = transform.GetAttributes() {
                    let _ = attrs.SetUINT32(&MF_TRANSFORM_ASYNC_UNLOCK, 1);
                    let _ = attrs.SetUINT32(&MF_LOW_LATENCY, 1);
                }
            }

            configure_low_latency(&transform, &cfg);

            // Output type MUST be set before input type for encoders.
            set_output_type(&transform, output_id, &cfg)?;
            // Hardware (async) MFTs take D3D textures only as NV12 — they may
            // *accept* ARGB32 during negotiation but reject a D3D ARGB texture at
            // ProcessInput. So force NV12 for the async path; software MFTs can
            // take ARGB32 directly from a CPU buffer.
            let input_format = set_input_type(&transform, input_id, &cfg, is_async)?;
            // Hardware MFTs require NV12; build the on-GPU converter for that path.
            // It scales input frames to the encoder output size via the video
            // processor, so a 4K desktop can be encoded at 1080p.
            let converter = if input_format == InputFormat::Nv12 {
                Some(Bgra2Nv12::new_with_scale(
                    device,
                    input_width,
                    input_height,
                    cfg.width,
                    cfg.height,
                )?)
            } else {
                None
            };

            // Hardware D3D11-aware MFTs demand input samples from their own
            // allocator. Build one bound to the shared device manager, producing
            // NV12 textures with RENDER_TARGET (video-processor writable) +
            // VIDEO_ENCODER bind flags.
            let allocator = if is_async && input_format == InputFormat::Nv12 {
                create_input_allocator(&transform, input_id, &device_manager, &cfg).ok()
            } else {
                None
            };

            let event_gen = if is_async {
                transform.cast::<IMFMediaEventGenerator>().ok()
            } else {
                None
            };

            Ok(Self {
                transform,
                input_stream_id: input_id,
                output_stream_id: output_id,
                device_manager,
                cfg,
                params: ParameterSets::default(),
                is_async,
                event_gen,
                started: false,
                pending_input_requests: 0,
                converter,
                cpu_nv12: Vec::new(),
                allocator,
                static_skip: false,
                last_frame_hash: None,
                static_frames_skipped: 0,
                _runtime: runtime,
            })
        }
    }

    /// SPS/PPS captured after the first output. Empty until then.
    pub fn parameter_sets(&self) -> &ParameterSets {
        &self.params
    }

    /// Request an IDR on the next encoded frame after a WebRTC PLI.
    pub fn force_keyframe(&self) {
        unsafe {
            if let Ok(codec) = self.transform.cast::<ICodecAPI>() {
                let value = windows::Win32::System::Variant::VARIANT::from(true);
                let _ = codec.SetValue(&CODECAPI_AVEncVideoForceKeyFrame, &value);
            }
        }
    }

    /// Enable static-frame skipping for the software path. When enabled, frames
    /// whose CPU NV12 content is identical to the previous encoded frame are
    /// skipped instead of encoded, cutting CPU and heap churn for idle desktops.
    /// A heartbeat keyframe is still emitted every [`STATIC_HEARTBEAT_FRAMES`]
    /// identical frames so the WebRTC decoder never stalls. No-op on the
    /// hardware (async) path, where encoding is cheap and content lives on GPU.
    pub fn set_static_skip(&mut self, enabled: bool) {
        self.static_skip = enabled;
        if !enabled {
            self.last_frame_hash = None;
            self.static_frames_skipped = 0;
        }
    }

    /// FNV-1a 64-bit hash of the CPU NV12 buffer. Cheap enough per frame.
    fn nv12_hash(data: &[u8]) -> u64 {
        let mut hash: u64 = 0xcbf2_9ce4_8422_2325;
        for &byte in data {
            hash ^= u64::from(byte);
            hash = hash.wrapping_mul(0x0000_0100_0000_01b3);
        }
        hash
    }

    /// Decide whether the current CPU NV12 frame should be skipped as unchanged.
    /// Returns true to skip encoding this frame entirely.
    fn should_skip_static_frame(&mut self, nv12: &[u8]) -> bool {
        if !self.static_skip || nv12.is_empty() {
            return false;
        }
        let hash = Self::nv12_hash(nv12);
        if self.last_frame_hash == Some(hash) {
            self.static_frames_skipped += 1;
            if self.static_frames_skipped >= STATIC_HEARTBEAT_FRAMES {
                self.static_frames_skipped = 0;
                self.force_keyframe();
                return false;
            }
            return true;
        }
        self.last_frame_hash = Some(hash);
        self.static_frames_skipped = 0;
        false
    }

    fn ensure_started(&mut self) -> Result<()> {
        if self.started {
            return Ok(());
        }
        unsafe {
            self.transform
                .ProcessMessage(MFT_MESSAGE_NOTIFY_BEGIN_STREAMING, 0)?;
            self.transform
                .ProcessMessage(MFT_MESSAGE_NOTIFY_START_OF_STREAM, 0)?;
        }
        self.started = true;
        Ok(())
    }

    /// Encode one captured texture at `timestamp`. Pushes any produced encoded
    /// samples to `out`. May produce zero, one, or more samples per input.
    pub fn encode(
        &mut self,
        texture: &ID3D11Texture2D,
        timestamp: Duration,
        out: &mut Vec<EncodedSample>,
    ) -> Result<()> {
        self.ensure_started()?;
        // Build the input sample. Software (sync) MFT path: convert BGRA->NV12
        // on-GPU, read back to a CPU buffer, wrap in an MF memory buffer.
        // Hardware (async) path: wrap the GPU NV12 texture directly (zero-copy).
        let sample = match (self.converter.as_mut(), self.is_async) {
            (Some(conv), false) => {
                conv.convert_to_cpu_into(texture, &mut self.cpu_nv12)?;
                let frame_nv12 = std::mem::take(&mut self.cpu_nv12);
                let skip = self.should_skip_static_frame(&frame_nv12);
                if skip {
                    self.cpu_nv12 = frame_nv12;
                    return Ok(());
                }
                let sample = self.wrap_cpu_nv12(&frame_nv12, timestamp)?;
                self.cpu_nv12 = frame_nv12;
                sample
            }
            (Some(_), true) => {
                // Hardware path: get an MFT-owned NV12 sample from the allocator,
                // blit the captured BGRA into its texture, feed that sample.
                self.build_allocated_sample(texture, timestamp)?
            }
            (None, _) => self.wrap_texture(texture, timestamp)?,
        };

        if self.is_async {
            self.encode_async(sample, out)
        } else {
            self.encode_sync(sample, out)
        }
    }

    /// Flush at end of stream; drains remaining output.
    pub fn drain(&mut self, out: &mut Vec<EncodedSample>) -> Result<()> {
        if !self.started {
            return Ok(());
        }
        unsafe {
            self.transform
                .ProcessMessage(MFT_MESSAGE_COMMAND_DRAIN, 0)?;
        }

        if self.is_async {
            // Async MFT signals completion by emitting METransformDrainComplete
            // after all outputs; pull on each HaveOutput until then.
            let gen = self
                .event_gen
                .clone()
                .expect("async MFT has event generator");
            loop {
                let event = unsafe { gen.GetEvent(MF_EVENT_FLAG_NONE)? };
                let met = unsafe { event.GetType()? } as i32;
                if met == METransformHaveOutput.0 {
                    self.pull_output(out)?;
                } else if met == METransformDrainComplete.0 {
                    break;
                } else if met == METransformNeedInput.0 {
                    // Ignore during drain.
                }
            }
        } else {
            // Sync MFT: pull until it needs more input.
            loop {
                match self.pull_output(out) {
                    Ok(true) => continue,
                    Ok(false) => break,
                    Err(e) => return Err(e),
                }
            }
        }
        Ok(())
    }

    fn wrap_texture(&self, texture: &ID3D11Texture2D, timestamp: Duration) -> Result<IMFSample> {
        unsafe {
            let buffer = MFCreateDXGISurfaceBuffer(&ID3D11Texture2D::IID, texture, 0, false)?;
            // The hardware encoder needs a correct current length. A DXGI
            // surface buffer's GetMaxLength is unreliable for NV12; the real
            // packed size comes from IMF2DBuffer::GetContiguousLength. Set that
            // as the current length or the MFT rejects the sample (E_UNEXPECTED).
            if let Ok(two_d) = buffer.cast::<IMF2DBuffer>() {
                if let Ok(len) = two_d.GetContiguousLength() {
                    let _ = buffer.SetCurrentLength(len);
                }
            } else if let Ok(len) = buffer.GetMaxLength() {
                let _ = buffer.SetCurrentLength(len);
            }
            let sample = MFCreateSample()?;
            sample.AddBuffer(&buffer)?;
            let hns = (timestamp.as_nanos() as i64) / 100;
            sample.SetSampleTime(hns)?;
            let frame_dur = HNS_PER_SEC / self.cfg.fps.max(1) as i64;
            sample.SetSampleDuration(frame_dur)?;
            Ok(sample)
        }
    }

    /// Hardware path: allocate an MFT-owned NV12 sample, blit BGRA into its
    /// texture via the video processor, and set its time/duration.
    fn build_allocated_sample(
        &mut self,
        bgra: &ID3D11Texture2D,
        timestamp: Duration,
    ) -> Result<IMFSample> {
        let allocator = self
            .allocator
            .as_ref()
            .expect("allocator present on hardware path")
            .clone();
        let conv = self.converter.as_mut().expect("converter on hardware path");
        unsafe {
            let sample = allocator.AllocateSample()?;
            // Extract the D3D11 texture backing the sample's buffer.
            let buffer = sample.GetBufferByIndex(0)?;
            let dxgi_buf = buffer.cast::<IMFDXGIBuffer>()?;
            let mut tex_ptr: *mut core::ffi::c_void = std::ptr::null_mut();
            dxgi_buf.GetResource(&ID3D11Texture2D::IID, &mut tex_ptr)?;
            let dst = ID3D11Texture2D::from_raw(tex_ptr);
            let slice = dxgi_buf.GetSubresourceIndex().unwrap_or(0);

            // Blit the captured BGRA into the MFT-owned NV12 texture's slice.
            conv.convert_into(bgra, &dst, slice)?;

            let hns = (timestamp.as_nanos() as i64) / 100;
            sample.SetSampleTime(hns)?;
            sample.SetSampleDuration(HNS_PER_SEC / self.cfg.fps.max(1) as i64)?;
            Ok(sample)
        }
    }

    /// Wrap a CPU NV12 byte buffer as an IMFSample for the software MFT.
    fn wrap_cpu_nv12(&self, nv12: &[u8], timestamp: Duration) -> Result<IMFSample> {
        unsafe {
            let buffer = MFCreateMemoryBuffer(nv12.len() as u32)?;
            let mut ptr: *mut u8 = std::ptr::null_mut();
            let mut max_len = 0u32;
            buffer.Lock(&mut ptr, Some(&mut max_len), None)?;
            std::ptr::copy_nonoverlapping(nv12.as_ptr(), ptr, nv12.len());
            buffer.Unlock()?;
            buffer.SetCurrentLength(nv12.len() as u32)?;

            let sample = MFCreateSample()?;
            sample.AddBuffer(&buffer)?;
            let hns = (timestamp.as_nanos() as i64) / 100;
            sample.SetSampleTime(hns)?;
            let frame_dur = HNS_PER_SEC / self.cfg.fps.max(1) as i64;
            sample.SetSampleDuration(frame_dur)?;
            Ok(sample)
        }
    }

    fn encode_sync(&mut self, sample: IMFSample, out: &mut Vec<EncodedSample>) -> Result<()> {
        unsafe {
            self.transform
                .ProcessInput(self.input_stream_id, &sample, 0)?;
        }
        loop {
            match self.pull_output(out) {
                Ok(true) => continue,
                Ok(false) => break,
                Err(e) => return Err(e),
            }
        }
        Ok(())
    }

    fn encode_async(&mut self, sample: IMFSample, out: &mut Vec<EncodedSample>) -> Result<()> {
        let gen = self
            .event_gen
            .clone()
            .expect("async MFT has event generator");

        // Async MFTs decouple input and output: they emit METransformNeedInput
        // (a request for a frame) and METransformHaveOutput (a produced sample)
        // independently, and may queue several NeedInput requests ahead. We must
        // never drop a NeedInput or the MFT deadlocks. So: pump events until we
        // have satisfied exactly one input request with our sample; a request
        // already in hand (pending_input_requests) is used immediately without
        // blocking, and any surplus request seen while draining is remembered.
        loop {
            if self.pending_input_requests > 0 {
                unsafe {
                    self.transform
                        .ProcessInput(self.input_stream_id, &sample, 0)?;
                }
                self.pending_input_requests -= 1;
                break;
            }
            let event = unsafe { gen.GetEvent(MF_EVENT_FLAG_NONE)? };
            let met = unsafe { event.GetType()? } as i32;
            if met == METransformNeedInput.0 {
                self.pending_input_requests += 1;
            } else if met == METransformHaveOutput.0 {
                self.pull_output(out)?;
            }
        }

        // Async output is strictly event-driven. Calling ProcessOutput before
        // METransformHaveOutput returns E_UNEXPECTED on AMD's H.264 MFT and can
        // tear down an otherwise healthy low-latency hardware session.
        Ok(())
    }

    /// Pull one output sample if available. Returns Ok(true) if a sample was
    /// produced, Ok(false) if the MFT needs more input, Err on real failure.
    fn pull_output(&mut self, out: &mut Vec<EncodedSample>) -> Result<bool> {
        unsafe {
            let stream_info = self.transform.GetOutputStreamInfo(self.output_stream_id)?;

            // For encoders the MFT usually allocates output samples itself
            // (MFT_OUTPUT_STREAM_PROVIDES_SAMPLES). If not, we must allocate.
            let provides_samples = (stream_info.dwFlags
                & (MFT_OUTPUT_STREAM_PROVIDES_SAMPLES.0 | MFT_OUTPUT_STREAM_CAN_PROVIDE_SAMPLES.0)
                    as u32)
                != 0;

            let mut output = MFT_OUTPUT_DATA_BUFFER::default();
            output.dwStreamID = self.output_stream_id;
            if !provides_samples {
                let sample = MFCreateSample()?;
                let buf = MFCreateMemoryBuffer(stream_info.cbSize.max(1))?;
                sample.AddBuffer(&buf)?;
                output.pSample = std::mem::ManuallyDrop::new(Some(sample));
            }

            let mut status: u32 = 0;
            let mut buffers = [output];
            let hr = self.transform.ProcessOutput(0, &mut buffers, &mut status);

            match hr {
                Ok(()) => {
                    let produced = std::mem::ManuallyDrop::take(&mut buffers[0].pSample);
                    if let Some(sample) = produced {
                        self.emit_sample(&sample, out)?;
                    }
                    Ok(true)
                }
                Err(e) if e.code() == MF_E_TRANSFORM_NEED_MORE_INPUT => {
                    // Reclaim the sample we allocated (if any) — dropped here.
                    let _ = std::mem::ManuallyDrop::take(&mut buffers[0].pSample);
                    Ok(false)
                }
                Err(e) if e.code() == MF_E_TRANSFORM_STREAM_CHANGE => {
                    // Output type changed; re-set and retry once.
                    let _ = std::mem::ManuallyDrop::take(&mut buffers[0].pSample);
                    set_output_type(&self.transform, self.output_stream_id, &self.cfg)?;
                    Ok(false)
                }
                Err(e) => {
                    let _ = std::mem::ManuallyDrop::take(&mut buffers[0].pSample);
                    Err(PipelineError::Windows(e))
                }
            }
        }
    }

    fn emit_sample(&mut self, sample: &IMFSample, out: &mut Vec<EncodedSample>) -> Result<()> {
        unsafe {
            let is_keyframe = sample
                .GetUINT32(&MFSampleExtension_CleanPoint)
                .map(|v| v != 0)
                .unwrap_or(false);
            let time_hns = sample.GetSampleTime().unwrap_or(0);
            let timestamp = Duration::from_nanos((time_hns.max(0) as u64) * 100);

            let buffer = sample.ConvertToContiguousBuffer()?;
            let mut ptr: *mut u8 = std::ptr::null_mut();
            let mut len: u32 = 0;
            buffer.Lock(&mut ptr, None, Some(&mut len))?;
            let data = std::slice::from_raw_parts(ptr, len as usize).to_vec();
            let _ = buffer.Unlock();

            // Capture SPS/PPS from the first keyframe's parameter sets.
            if self.params.sps.is_empty() && is_keyframe {
                self.params = extract_parameter_sets(&data);
            }

            out.push(EncodedSample {
                data,
                timestamp,
                is_keyframe,
            });
        }
        Ok(())
    }
}

impl Drop for MfH264Encoder {
    fn drop(&mut self) {
        unsafe {
            // Async (hardware) MFTs keep internal driver threads and pooled
            // textures alive until their event queue is fully drained and the
            // D3D device manager reference is removed. Skipping this leaks
            // ~110 handles, ~17 threads and ~50 MiB per session (verified by
            // remote_desktop_resource_smoke). Give the MFT a bounded chance to
            // flush before tearing down, but never block forever.
            if self.is_async && self.started {
                let _ = self
                    .transform
                    .ProcessMessage(MFT_MESSAGE_COMMAND_DRAIN, 0);
                if let Some(gen) = self.event_gen.clone() {
                    for _ in 0..256 {
                        let event = match gen.GetEvent(MF_EVENT_FLAG_NONE) {
                            Ok(event) => event,
                            Err(_) => break,
                        };
                        let met = match event.GetType() {
                            Ok(met) => met as i32,
                            Err(_) => break,
                        };
                        if met == METransformHaveOutput.0 {
                            let mut drain_out = Vec::new();
                            let _ = self.pull_output(&mut drain_out);
                        } else if met == METransformDrainComplete.0 {
                            break;
                        }
                    }
                }
            }

            // A session may close with samples still queued in the transform.
            // Flush first so those frame-sized buffers are released before the
            // transform and Media Foundation runtime are torn down.
            let _ = self.transform.ProcessMessage(MFT_MESSAGE_COMMAND_FLUSH, 0);
            let _ = self
                .transform
                .ProcessMessage(MFT_MESSAGE_NOTIFY_END_OF_STREAM, 0);
            let _ = self
                .transform
                .ProcessMessage(MFT_MESSAGE_NOTIFY_END_STREAMING, 0);
            // Release the D3D manager reference held by the MFT.
            let _ = self
                .transform
                .ProcessMessage(MFT_MESSAGE_SET_D3D_MANAGER, 0);
            let _ = &self.device_manager;
        }
    }
}

// --- free functions -------------------------------------------------------

unsafe fn configure_low_latency(transform: &IMFTransform, cfg: &VideoConfig) {
    let Ok(codec) = transform.cast::<ICodecAPI>() else {
        return;
    };
    let settings = [
        (
            &CODECAPI_AVEncCommonLowLatency,
            windows::Win32::System::Variant::VARIANT::from(true),
        ),
        (
            &CODECAPI_AVEncCommonRealTime,
            windows::Win32::System::Variant::VARIANT::from(true),
        ),
        // Dedicated low-latency switch. Chromium's H.264 MFT encoder enables it
        // (media/gpu/windows/media_foundation_video_encode_accelerator_win.cc).
        (
            &CODECAPI_AVLowLatencyMode,
            windows::Win32::System::Variant::VARIANT::from(true),
        ),
        (
            &CODECAPI_AVEncCommonMeanBitRate,
            windows::Win32::System::Variant::VARIANT::from(cfg.bitrate),
        ),
        // Peak = 1.5x the target. JetKVM's low-latency encoder uses the same
        // ratio (`internal/native/cgo/video_bitrate.h`: maximum = target * 3/2).
        (
            &CODECAPI_AVEncCommonMaxBitRate,
            windows::Win32::System::Variant::VARIANT::from(cfg.bitrate + cfg.bitrate / 2),
        ),
        // Peak-constrained VBR is the mode Chromium selects for a VBR stream
        // carrying both mean and max bitrate. It does NOT set
        // AVEncCommonBufferSize, so neither do we (the HRD window stays at the
        // driver default).
        (
            &CODECAPI_AVEncCommonRateControlMode,
            windows::Win32::System::Variant::VARIANT::from(
                eAVEncCommonRateControlMode_PeakConstrainedVBR.0 as u32,
            ),
        ),
        // Keep one reference frame to minimise the decoding (re)order delay.
        // Same as Chromium's low-latency configuration.
        (
            &CODECAPI_AVEncVideoMaxNumRefFrame,
            windows::Win32::System::Variant::VARIANT::from(1u32),
        ),
        (
            &CODECAPI_AVEncMPVGOPSize,
            windows::Win32::System::Variant::VARIANT::from(cfg.keyframe_interval),
        ),
        (
            &CODECAPI_AVEncVideoMaxKeyframeDistance,
            windows::Win32::System::Variant::VARIANT::from(cfg.keyframe_interval),
        ),
        (
            &CODECAPI_AVEncMPVDefaultBPictureCount,
            windows::Win32::System::Variant::VARIANT::from(0u32),
        ),
    ];
    for (key, value) in settings {
        let _ = codec.SetValue(key, &value);
    }
}

/// Create and initialize a video sample allocator that produces NV12 textures
/// the hardware MFT will accept as input. Bound to the shared DXGI device
/// manager so the textures live on the encoder's device.
unsafe fn create_input_allocator(
    transform: &IMFTransform,
    input_id: u32,
    device_manager: &IMFDXGIDeviceManager,
    cfg: &VideoConfig,
) -> Result<IMFVideoSampleAllocatorEx> {
    // Only meaningful if the MFT reports D3D11 awareness on its input stream.
    if let Ok(attrs) = transform.GetInputStreamAttributes(input_id) {
        let aware = attrs.GetUINT32(&MF_SA_D3D11_AWARE).unwrap_or(0);
        if aware == 0 {
            return Err(PipelineError::Audio("MFT not D3D11-aware".into()));
        }
    }

    let mut alloc: *mut core::ffi::c_void = std::ptr::null_mut();
    MFCreateVideoSampleAllocatorEx(&IMFVideoSampleAllocatorEx::IID, &mut alloc)?;
    let allocator = IMFVideoSampleAllocatorEx::from_raw(alloc);

    allocator.SetDirectXManager(device_manager)?;

    // The NV12 media type the allocator produces.
    let nv12_type = MFCreateMediaType()?;
    nv12_type.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
    nv12_type.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_NV12)?;
    set_frame_size(&nv12_type, cfg.width, cfg.height)?;
    set_ratio(&nv12_type, &MF_MT_FRAME_RATE, cfg.fps, 1)?;
    set_ratio(&nv12_type, &MF_MT_PIXEL_ASPECT_RATIO, 1, 1)?;
    nv12_type.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32)?;

    // Attributes controlling the allocated textures' bind flags: RENDER_TARGET
    // (video processor writes) + VIDEO_ENCODER (hardware encoder input).
    let attrs = create_mf_attributes(1)?;
    use windows::Win32::Graphics::Direct3D11::{
        D3D11_BIND_RENDER_TARGET, D3D11_BIND_VIDEO_ENCODER,
    };
    attrs.SetUINT32(
        &MF_SA_D3D11_BINDFLAGS,
        (D3D11_BIND_RENDER_TARGET.0 | D3D11_BIND_VIDEO_ENCODER.0) as u32,
    )?;

    // A small pool: a few in-flight frames.
    allocator.InitializeSampleAllocatorEx(2, 6, &attrs, &nv12_type)?;
    Ok(allocator)
}

unsafe fn create_mf_attributes(count: u32) -> Result<IMFAttributes> {
    let mut attrs: Option<IMFAttributes> = None;
    MFCreateAttributes(&mut attrs, count)?;
    Ok(attrs.unwrap())
}

unsafe fn create_device_manager(device: &ID3D11Device) -> Result<IMFDXGIDeviceManager> {
    let mut reset_token: u32 = 0;
    let mut manager: Option<IMFDXGIDeviceManager> = None;
    MFCreateDXGIDeviceManager(&mut reset_token, &mut manager)?;
    let manager = manager.expect("device manager created");
    manager.ResetDevice(device, reset_token)?;
    Ok(manager)
}

/// Enumerate H.264 video encoders, preferring hardware, and return the created
/// transform.
unsafe fn enumerate_h264_encoder(prefer_hardware: bool) -> Result<IMFTransform> {
    let output_info = MFT_REGISTER_TYPE_INFO {
        guidMajorType: MFMediaType_Video,
        guidSubtype: MFVideoFormat_H264,
    };

    let flags = if prefer_hardware {
        MFT_ENUM_FLAG_HARDWARE | MFT_ENUM_FLAG_TRANSCODE_ONLY | MFT_ENUM_FLAG_SORTANDFILTER
    } else {
        MFT_ENUM_FLAG_SYNCMFT | MFT_ENUM_FLAG_TRANSCODE_ONLY | MFT_ENUM_FLAG_SORTANDFILTER
    };

    let mut activates: *mut Option<IMFActivate> = std::ptr::null_mut();
    let mut count: u32 = 0;
    MFTEnumEx(
        MFT_CATEGORY_VIDEO_ENCODER,
        flags,
        None,
        Some(&output_info),
        &mut activates,
        &mut count,
    )?;

    if count == 0 || activates.is_null() {
        if !activates.is_null() {
            windows::Win32::System::Com::CoTaskMemFree(Some(activates as *const _));
        }
        return Err(PipelineError::NoEncoderFound);
    }

    let slice = std::slice::from_raw_parts_mut(activates, count as usize);
    let owned_activations = slice
        .iter_mut()
        .filter_map(Option::take)
        .collect::<Vec<_>>();
    windows::Win32::System::Com::CoTaskMemFree(Some(activates as *const _));

    // Take the first (sorted best-first by SORTANDFILTER).
    let result = (|| {
        for act in &owned_activations {
            if let Ok(transform) = act.ActivateObject::<IMFTransform>() {
                return Some(transform);
            }
        }
        None
    })();

    // Dropping the owned activation list releases every COM reference returned
    // by MFTEnumEx. The activated transform owns everything it still needs.
    result.ok_or(PipelineError::NoEncoderFound)
}

unsafe fn stream_ids(transform: &IMFTransform) -> Result<(u32, u32)> {
    // Most encoder MFTs use stream id 0 for both. GetStreamIDs may return
    // E_NOTIMPL meaning "use 0-based indices".
    let mut input_ids = [0u32; 1];
    let mut output_ids = [0u32; 1];
    let _ = transform.GetStreamIDs(&mut input_ids, &mut output_ids);
    // If GetStreamIDs is not implemented, ids stay 0 which is correct.
    Ok((input_ids[0], output_ids[0]))
}

unsafe fn mft_is_async(transform: &IMFTransform) -> bool {
    if let Ok(attrs) = transform.GetAttributes() {
        if let Ok(v) = attrs.GetUINT32(&MF_TRANSFORM_ASYNC) {
            return v != 0;
        }
    }
    false
}

unsafe fn set_output_type(
    transform: &IMFTransform,
    output_id: u32,
    cfg: &VideoConfig,
) -> Result<()> {
    let media_type = MFCreateMediaType()?;
    media_type.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
    media_type.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_H264)?;
    media_type.SetUINT32(&MF_MT_AVG_BITRATE, cfg.bitrate)?;
    set_frame_size(&media_type, cfg.width, cfg.height)?;
    set_ratio(&media_type, &MF_MT_FRAME_RATE, cfg.fps, 1)?;
    set_ratio(&media_type, &MF_MT_PIXEL_ASPECT_RATIO, 1, 1)?;
    media_type.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32)?;
    // Match the profile browsers advertise for WebRTC H.264. Main-profile
    // bitstreams labeled as constrained baseline may negotiate but then fail
    // at the decoder on Safari and some Android hardware.
    media_type.SetUINT32(
        &MF_MT_MPEG2_PROFILE,
        eAVEncH264VProfile_ConstrainedBase.0 as u32,
    )?;
    transform
        .SetOutputType(output_id, &media_type, 0)
        .map_err(|e| PipelineError::TypeNegotiation(format!("output: {e}")))?;
    Ok(())
}

#[derive(PartialEq, Eq, Clone, Copy)]
enum InputFormat {
    Argb32,
    Nv12,
}

unsafe fn set_input_type(
    transform: &IMFTransform,
    input_id: u32,
    cfg: &VideoConfig,
    force_nv12: bool,
) -> Result<InputFormat> {
    // For hardware/async MFTs fed D3D textures, NV12 is the only reliable input.
    // For software MFTs, ARGB32 (BGRA) from a CPU buffer works and skips the
    // color-convert step.
    let candidates: &[(windows::core::GUID, InputFormat)] = if force_nv12 {
        &[(MFVideoFormat_NV12, InputFormat::Nv12)]
    } else {
        &[
            (MFVideoFormat_ARGB32, InputFormat::Argb32),
            (MFVideoFormat_NV12, InputFormat::Nv12),
        ]
    };
    let mut last_err = None;
    for &(subtype, format) in candidates {
        let media_type = MFCreateMediaType()?;
        media_type.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
        media_type.SetGUID(&MF_MT_SUBTYPE, &subtype)?;
        set_frame_size(&media_type, cfg.width, cfg.height)?;
        set_ratio(&media_type, &MF_MT_FRAME_RATE, cfg.fps, 1)?;
        set_ratio(&media_type, &MF_MT_PIXEL_ASPECT_RATIO, 1, 1)?;
        let _ = media_type.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32);
        match transform.SetInputType(input_id, &media_type, 0) {
            Ok(()) => return Ok(format),
            Err(e) => last_err = Some(e),
        }
    }
    Err(PipelineError::TypeNegotiation(format!(
        "input (tried ARGB32/NV12): {:?}",
        last_err
    )))
}

unsafe fn set_frame_size(mt: &IMFMediaType, w: u32, h: u32) -> Result<()> {
    let packed = ((w as u64) << 32) | (h as u64);
    mt.SetUINT64(&MF_MT_FRAME_SIZE, packed)?;
    Ok(())
}

unsafe fn set_ratio(mt: &IMFMediaType, key: *const GUID, num: u32, den: u32) -> Result<()> {
    let packed = ((num as u64) << 32) | (den as u64);
    mt.SetUINT64(&*key, packed)?;
    Ok(())
}

/// Split an Annex-B access unit into SPS (type 7) and PPS (type 8) NAL bodies
/// (without start codes).
fn extract_parameter_sets(annex_b: &[u8]) -> ParameterSets {
    let mut params = ParameterSets::default();
    for nal in iter_annex_b_nals(annex_b) {
        if nal.is_empty() {
            continue;
        }
        let nal_type = nal[0] & 0x1f;
        match nal_type {
            7 => params.sps = nal.to_vec(),
            8 => params.pps = nal.to_vec(),
            _ => {}
        }
    }
    params
}

/// Iterate NAL unit bodies (between 3- or 4-byte start codes), excluding the
/// start code itself.
pub fn iter_annex_b_nals(data: &[u8]) -> impl Iterator<Item = &[u8]> {
    let starts = find_nal_starts(data);
    let mut ranges = Vec::with_capacity(starts.len());
    for i in 0..starts.len() {
        let (body_start, _sc_len) = starts[i];
        let end = if i + 1 < starts.len() {
            starts[i + 1].0 - starts[i + 1].1
        } else {
            data.len()
        };
        ranges.push((body_start, end));
    }
    ranges.into_iter().map(move |(s, e)| &data[s..e])
}

/// Returns (body_start_index, start_code_len) for each NAL. body_start is the
/// index just after the start code.
fn find_nal_starts(data: &[u8]) -> Vec<(usize, usize)> {
    let mut out = Vec::new();
    let mut i = 0;
    while i + 3 <= data.len() {
        if data[i] == 0 && data[i + 1] == 0 && data[i + 2] == 1 {
            out.push((i + 3, 3));
            i += 3;
        } else if i + 4 <= data.len()
            && data[i] == 0
            && data[i + 1] == 0
            && data[i + 2] == 0
            && data[i + 3] == 1
        {
            out.push((i + 4, 4));
            i += 4;
        } else {
            i += 1;
        }
    }
    out
}

// Suppress unused import warning for PCWSTR if not used in some builds.
const _: Option<PCWSTR> = None;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_sps_pps_from_annex_b() {
        // start(4) SPS(type7) start(3) PPS(type8) start(4) IDR(type5)
        let stream = [
            0, 0, 0, 1, 0x67, 0xAA, 0xBB, // SPS
            0, 0, 1, 0x68, 0xCC, // PPS
            0, 0, 0, 1, 0x65, 0x11, 0x22, // IDR slice
        ];
        let p = extract_parameter_sets(&stream);
        assert_eq!(p.sps, vec![0x67, 0xAA, 0xBB]);
        assert_eq!(p.pps, vec![0x68, 0xCC]);
    }

    #[test]
    fn iterates_all_nals() {
        let stream = [0, 0, 0, 1, 0x67, 1, 2, 0, 0, 1, 0x68, 3];
        let nals: Vec<&[u8]> = iter_annex_b_nals(&stream).collect();
        assert_eq!(nals.len(), 2);
        assert_eq!(nals[0], &[0x67, 1, 2]);
        assert_eq!(nals[1], &[0x68, 3]);
    }
}
