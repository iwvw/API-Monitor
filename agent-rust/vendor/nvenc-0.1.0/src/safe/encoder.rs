use std::ffi::{CStr, c_void};
use std::marker::PhantomData;
use std::mem::ManuallyDrop;
use std::sync::Arc;
use std::{mem::MaybeUninit, ptr::NonNull};

#[cfg(windows)]
use windows::Win32::Graphics::Direct3D11::ID3D11Texture2D;

use crate::input_buffer::InputBuffer;
use crate::safe::bitstream::BitStream;
use crate::sys::enums::{
    NVencBufferFormat, NVencBufferUsage, NVencInputResourceType, NVencMemoryHeap, NVencPicStruct,
    NVencPicType,
};
use crate::sys::structs::{
    Guid, NV_ENC_CREATE_INPUT_BUFFER_VER, NV_ENC_LOCK_INPUT_BUFFER_VER, NVencCodecPicParams,
    NVencCreateInputBuffer, NVencLockInputBuffer, NVencPicParamsH264, NVencRegisterResource,
};
use crate::sys::{
    enums::{NVencPicFlags, NVencTuningInfo},
    function_table::NVencFunctionList,
    result::NVencError,
    structs::{
        NV_ENC_CONFIG_VER, NV_ENC_CREATE_BITSTREAM_BUFFER_VER, NV_ENC_LOCK_BITSTREAM_VER,
        NV_ENC_MAP_INPUT_RESOURCE_VER, NV_ENC_PIC_PARAMS_VER, NV_ENC_PRESET_CONFIG_VER,
        NV_ENC_RECONFIGURE_PARAMS_VER, NV_ENC_REGISTER_RESOURCE_VER, NVencCreateBitstreamBuffer,
        NVencInitializeParams, NVencLockBitStream, NVencPicParams, NVencPresetConfig,
        NVencReconfigureParams,
    },
};
use crate::sys::structs::NVencConfig;

pub struct Encoder {
    pub(crate) encoder: Arc<EncoderInternal>,
    /// Snapshot of the initialization parameters so `reconfigure_bitrate` can
    /// build a valid `NV_ENC_RECONFIGURE_PARAMS` without re-negotiating the
    /// session (resolution/profile/GOP stay pinned to the original values).
    pub(crate) init_snapshot: NVencInitializeParams,
    /// Owned copy of the encode config referenced by `init_snapshot`. The
    /// caller may drop its own `NVencConfig` right after `init_encoder`, so
    /// reconfigure must not dereference the pointer inside the snapshot.
    pub(crate) init_config: Box<NVencConfig>,
    pub(crate) marker: PhantomData<*mut ()>,
}

pub trait EncoderInput {
    #[doc(hidden)]
    fn as_ptr(&self) -> *mut c_void;

    fn pitch(&self) -> u32;
    fn height(&self) -> u32;
    fn width(&self) -> u32;
}

impl EncoderInput for RegisteredResource {
    fn as_ptr(&self) -> *mut c_void {
        self.mapped
    }

    fn pitch(&self) -> u32 {
        self.pitch
    }

    fn height(&self) -> u32 {
        self.height
    }

    fn width(&self) -> u32 {
        self.width
    }
}

impl EncoderInput for InputBuffer {
    fn as_ptr(&self) -> *mut c_void {
        self.buffer
    }

    fn pitch(&self) -> u32 {
        self.width
    }

    fn height(&self) -> u32 {
        self.height
    }

    fn width(&self) -> u32 {
        self.width
    }
}

impl Encoder {
    #[cfg(windows)]
    /// Registers a DX11 texture as an nvenc input resource
    pub fn register_resource_dx11(
        &self,
        resource: &ID3D11Texture2D,
        format: NVencBufferFormat,
        pitch: u32,
    ) -> Result<RegisteredResource, NVencError> {
        self.encoder.register_resource_dx(resource, format, pitch)
    }

    /// # Safety
    /// `resource` must not be null
    /// `ty` and `format` should match the original buffer
    pub unsafe fn register_resource_raw(
        &self,
        resource: *mut c_void,
        format: NVencBufferFormat,
        usage: NVencBufferUsage,
        ty: NVencInputResourceType,
        resolution: [u32; 2],
        pitch: u32,
    ) -> Result<*mut c_void, NVencError> {
        unsafe {
            self.encoder
                .register_resource_raw(resource, format, usage, ty, resolution, pitch)
        }
    }

    /// Takes an encoder inout, and outputs a bit stream that can be read back
    pub fn encode_picture<I: EncoderInput>(
        &self,
        input: &I,
        output: &BitStream,
        frame_count: usize,
        timestamp: u64,
        format: NVencBufferFormat,
        pic_struct: NVencPicStruct,
        ty: NVencPicType,
        codec_params: Option<CodecPicParams>,
    ) -> Result<(), NVencError> {
        self.encoder.encode_picture(
            input,
            output,
            frame_count,
            timestamp,
            format,
            pic_struct,
            ty,
            codec_params,
        )
    }

    /// Ends the encoder session
    pub fn end_encode(&self) -> Result<(), NVencError> {
        self.encoder.end_encode()
    }

    /// Dynamically adjusts the average bitrate of a running encoder session
    /// without tearing it down. NVENC applies the new rate control target on
    /// the next encoded frames, which avoids the stall of a full re-init when
    /// the stream profile adapts to network conditions.
    pub fn reconfigure_bitrate(&self, average_bitrate: u32) -> Result<(), NVencError> {
        // Copy the owned config out of the box; the caller's original struct
        // may already be dropped and the snapshot pointer into it is not
        // dereferenced here.
        let mut config = unsafe { std::ptr::read(&*self.init_config) };
        config.rc_params.average_bit_rate = average_bitrate;
        // Keep the constrained-VBR limits proportional to the new average.
        // Peak ratio 3/2 and VBV window 2x follow the reference policies in
        // remote_desktop.rs (JetKVM peak ratio / FFmpeg nvenc VBV default).
        config.rc_params.max_bit_rate = average_bitrate + average_bitrate / 2;
        config.rc_params.vbv_buffer_size = average_bitrate * 2;
        let mut re_init = unsafe { std::ptr::read(&self.init_snapshot) };
        re_init.encode_config = &raw mut config;
        let mut params = NVencReconfigureParams {
            version: NV_ENC_RECONFIGURE_PARAMS_VER,
            rsvd: 0,
            re_init_encode_params: re_init,
            bitflags: 0,
            rsvd2: 0,
        };
        // SAFETY: `params` is fully initialized and `config` stays alive for
        // the duration of the call; the function pointer is loaded from the
        // driver's NVENC function table and the SDK copies the config during
        // the call (it does not retain the pointer afterwards).
        unsafe {
            (self.encoder.function_list.nvenc_reconfigure_encoder)(
                self.encoder.encoder.as_ptr(),
                &raw mut params,
            )
        }
        .into_error()?;
        Ok(())
    }

    /// Creates a bitstream bfufer that can be used for output storage
    pub fn create_bitstream_buffer(&self) -> Result<BitStream, NVencError> {
        self.encoder.create_bitstream_buffer()
    }

    pub fn create_input_buffer(
        &self,
        width: u32,
        height: u32,
        memory_heap: NVencMemoryHeap,
        format: NVencBufferFormat,
    ) -> Result<InputBuffer, NVencError> {
        Ok(InputBuffer {
            encoder: self.encoder.clone(),
            buffer: self
                .encoder
                .create_input_buffer(width, height, memory_heap, format)?,
            width,
            height,
        })
    }
}

pub(crate) struct EncoderInternal {
    pub(super) encoder: NonNull<c_void>,
    pub(super) function_list: NVencFunctionList,
}

/// The Nvidia encoder is technically thread-safe
unsafe impl Send for EncoderInternal {}

/// The Nvidia encoder handles syncing via `lock_bit_stream` which is all that is allowed to be Send
unsafe impl Sync for EncoderInternal {}

impl Drop for EncoderInternal {
    fn drop(&mut self) {
        println!("Dropping encoder, this should happen last");
        unsafe { (self.function_list.nvenc_destroy_encoder)(self.encoder.as_ptr()) };
    }
}

impl EncoderInternal {
    pub(crate) fn get_encode_preset_config_ex(
        &self,
        codec: Guid,
        preset: Guid,
        tuning_info: NVencTuningInfo,
    ) -> Result<NVencPresetConfig, NVencError> {
        let mut config: std::mem::MaybeUninit<NVencPresetConfig> = std::mem::MaybeUninit::zeroed();
        unsafe { &mut *config.as_mut_ptr() }.version = NV_ENC_PRESET_CONFIG_VER;
        unsafe { &mut *config.as_mut_ptr() }.preset_cfg.version = NV_ENC_CONFIG_VER;
        unsafe {
            (self.function_list.nvenc_get_encode_preset_config_ex)(
                self.encoder.as_ptr(),
                codec,
                preset,
                tuning_info,
                &raw mut config,
            )
            .into_error()?
        };

        Ok(unsafe { config.assume_init() })
    }

    pub(crate) fn init_encoder(
        &self,
        mut init_params: NVencInitializeParams,
    ) -> Result<(), NVencError> {
        unsafe {
            (self.function_list.nvenc_initialize_encoder)(
                self.encoder.as_ptr(),
                &raw mut init_params,
            )
        }
        .into_error()?;
        Ok(())
    }

    #[cfg(windows)]
    pub fn register_resource_dx(
        self: &Arc<EncoderInternal>,
        resource: &ID3D11Texture2D,
        format: NVencBufferFormat,
        pitch: u32,
    ) -> Result<RegisteredResource, NVencError> {
        use windows::Win32::Graphics::Direct3D11::D3D11_TEXTURE2D_DESC;
        use windows::core::Interface;

        let mut desc = D3D11_TEXTURE2D_DESC::default();
        unsafe { resource.GetDesc(&raw mut desc) };

        let registered = unsafe {
            self.register_resource_raw(
                resource.as_raw(),
                NVencBufferFormat::Undefined,
                NVencBufferUsage::Image,
                NVencInputResourceType::DirectX,
                [desc.Width, desc.Height],
                pitch,
            )?
        };

        let mapped = unsafe { self.map_input_resource_raw(registered, format)? };

        Ok(RegisteredResource {
            encoder: self.clone(),
            pitch,
            width: desc.Width,
            height: desc.Height,
            registered,
            mapped,
        })
    }

    pub unsafe fn map_input_resource_raw(
        &self,
        resource: *mut c_void,
        format: NVencBufferFormat,
    ) -> Result<*mut c_void, NVencError> {
        let mut map: crate::sys::structs::NVencMapInputResource = unsafe { std::mem::zeroed() };
        map.version = NV_ENC_MAP_INPUT_RESOURCE_VER;
        map.registered_resource = resource;
        map.mapped_buffer_format = format;

        unsafe {
            (self.function_list.nvenc_map_input_resource)(self.encoder.as_ptr(), &raw mut map)
        }
        .into_error()?;

        Ok(map.mapped_resource)
    }

    pub unsafe fn register_resource_raw(
        &self,
        resource: *mut c_void,
        format: NVencBufferFormat,
        usage: NVencBufferUsage,
        ty: NVencInputResourceType,
        resolution: [u32; 2],
        pitch: u32,
    ) -> Result<*mut c_void, NVencError> {
        let mut register_resource: NVencRegisterResource = unsafe { std::mem::zeroed() };
        register_resource.buffer_format = format;
        register_resource.buffer_usage = usage;
        register_resource.resource_type = ty;
        register_resource.version = NV_ENC_REGISTER_RESOURCE_VER;
        register_resource.width = resolution[0];
        register_resource.height = resolution[1];
        register_resource.pitch = pitch;
        register_resource.sub_resource_index = 0;
        register_resource.resource_to_register = resource;
        unsafe {
            (self.function_list.nvenc_register_resource)(
                self.encoder.as_ptr(),
                &raw mut register_resource,
            )
            .into_error()
        }?;
        Ok(register_resource.registered_resource)
    }

    pub unsafe fn unmap_input_resource(
        &self,
        mapped_resource: *mut c_void,
    ) -> Result<(), NVencError> {
        unsafe {
            (self.function_list.nvenc_unmap_input_resource)(self.encoder.as_ptr(), mapped_resource)
        }
        .into_error()
    }

    pub unsafe fn unregister_resource(
        &self,
        registered_resource: *mut c_void,
    ) -> Result<(), NVencError> {
        unsafe {
            (self.function_list.nvenc_unregister_resource)(
                self.encoder.as_ptr(),
                registered_resource,
            )
        }
        .into_error()
    }

    pub fn create_input_buffer(
        &self,
        width: u32,
        height: u32,
        memory_heap: NVencMemoryHeap,
        format: NVencBufferFormat,
    ) -> Result<*mut c_void, NVencError> {
        let mut input = NVencCreateInputBuffer {
            version: NV_ENC_CREATE_INPUT_BUFFER_VER,
            width: width,
            height: height,
            memory_heap,
            buffer_fmt: format,
            rsvd: 0,
            input_buffer: std::ptr::null_mut(),
            p_sys_mem_buffer: std::ptr::null_mut(),
            rsvd1: unsafe { std::mem::zeroed() },
            rsvd2: unsafe { std::mem::zeroed() },
        };
        unsafe {
            (self.function_list.nvenc_create_input_buffer)(self.encoder.as_ptr(), &raw mut input)
                .into_error()?
        };

        Ok(input.input_buffer)
    }

    pub fn destroy_input_buffer(&self, buffer: *mut c_void) -> Result<(), NVencError> {
        unsafe { (self.function_list.nvenc_destroy_input_buffer)(self.encoder.as_ptr(), buffer) }
            .into_error()
    }

    pub fn lock_input_buffer(&self, buffer: *mut c_void) -> Result<(*mut c_void, u32), NVencError> {
        let mut lock = NVencLockInputBuffer {
            version: NV_ENC_LOCK_INPUT_BUFFER_VER,
            bitflags: 0,
            input_buffer: buffer,
            buffer_data_ptr: std::ptr::null_mut(),
            pitch: 0,
            rsvd1: unsafe { std::mem::zeroed() },
            rsvd2: unsafe { std::mem::zeroed() },
        };

        unsafe {
            (self.function_list.nvenc_lock_input_buffer)(self.encoder.as_ptr(), &raw mut lock)
                .into_error()?;
        }

        Ok((lock.buffer_data_ptr, lock.pitch))
    }

    pub fn unlock_input_buffer(&self, locked_buffer: *mut c_void) -> Result<(), NVencError> {
        unsafe {
            (self.function_list.nvenc_unlock_input_buffer)(self.encoder.as_ptr(), locked_buffer)
                .into_error()
        }
    }

    pub fn encode_picture<I: EncoderInput>(
        &self,
        input: &I,
        output: &BitStream,
        frame_count: usize,
        timestamp: u64,
        format: NVencBufferFormat,
        pic_struct: NVencPicStruct,
        ty: NVencPicType,
        codec_params: Option<CodecPicParams>,
    ) -> Result<(), NVencError> {
        let mut params: MaybeUninit<NVencPicParams> = MaybeUninit::zeroed();
        let sys_codec = match codec_params {
            None => NVencCodecPicParams { rsvd: [0; 256] },
            Some(CodecPicParams::H264(codec)) => NVencCodecPicParams {
                h264_pic_params: ManuallyDrop::new(codec),
            },
        };
        unsafe { &mut *params.as_mut_ptr() }.version = NV_ENC_PIC_PARAMS_VER;
        unsafe { &mut *params.as_mut_ptr() }.input_width = input.width();
        unsafe { &mut *params.as_mut_ptr() }.input_height = input.height();
        unsafe { &mut *params.as_mut_ptr() }.input_pitch = input.pitch();
        unsafe { &mut *params.as_mut_ptr() }.output_bitstream = output.buffer;
        unsafe { &mut *params.as_mut_ptr() }.input_buffer = input.as_ptr();
        unsafe { &mut *params.as_mut_ptr() }.buffer_format = format;
        unsafe { &mut *params.as_mut_ptr() }.picture_struct = pic_struct;
        unsafe { &mut *params.as_mut_ptr() }.picture_type = ty;
        unsafe { &mut *params.as_mut_ptr() }.frame_idx = frame_count as u32;
        unsafe { &mut *params.as_mut_ptr() }.input_time_stamp = timestamp;
        unsafe { &mut *params.as_mut_ptr() }.codec_pic_params = sys_codec;
        match unsafe {
            (self.function_list.nvenc_encode_picture)(self.encoder.as_ptr(), &raw mut params)
                .into_error()
        } {
            Err(err) => {
                let ptr =
                    unsafe { (self.function_list.nvenc_get_last_error)(self.encoder.as_ptr()) };
                println!("{:?}", unsafe { CStr::from_ptr(ptr) });
                Err(err)
            }
            Ok(()) => Ok(()),
        }
    }

    pub fn end_encode(&self) -> Result<(), NVencError> {
        let mut params: MaybeUninit<NVencPicParams> = MaybeUninit::zeroed();
        unsafe { &mut *params.as_mut_ptr() }.version = NV_ENC_PIC_PARAMS_VER;
        unsafe { &mut *params.as_mut_ptr() }.encode_pic_flags = NVencPicFlags::Eos as u32;
        unsafe {
            (self.function_list.nvenc_encode_picture)(self.encoder.as_ptr(), &raw mut params)
                .into_error()
        }?;
        Ok(())
    }

    pub(crate) fn lock_bit_stream_buffer(
        &self,
        buffer: *mut c_void,
        wait: bool,
    ) -> Result<NVencLockBitStream, NVencError> {
        let mut lock: MaybeUninit<NVencLockBitStream> = MaybeUninit::zeroed();
        unsafe { &mut *lock.as_mut_ptr() }.version = NV_ENC_LOCK_BITSTREAM_VER;
        unsafe { &mut *lock.as_mut_ptr() }.output_bit_stream = buffer;
        unsafe { &mut *lock.as_mut_ptr() }
            .bit_fields
            .set_do_not_wait(!wait);
        unsafe { (self.function_list.nvenc_lock_bit_stream)(self.encoder.as_ptr(), &raw mut lock) }
            .into_error()?;
        Ok(unsafe { lock.assume_init() })
    }

    pub(crate) fn unlock_bit_stream_buffer(&self, buffer: *mut c_void) -> Result<(), NVencError> {
        unsafe { (self.function_list.nvenc_unlock_bit_stream)(self.encoder.as_ptr(), buffer) }
            .into_error()
    }

    pub fn create_bitstream_buffer(self: &Arc<Self>) -> Result<BitStream, NVencError> {
        let mut params: NVencCreateBitstreamBuffer = unsafe { std::mem::zeroed() };
        params.version = NV_ENC_CREATE_BITSTREAM_BUFFER_VER;
        unsafe {
            (self.function_list.nvenc_create_bit_stream_buffer)(
                self.encoder.as_ptr(),
                &raw mut params,
            )
        }
        .into_error()?;
        Ok(BitStream {
            buffer: params.bitstream_buffer,
            encoder: self.clone(),
        })
    }

    pub(crate) fn destroy_bitstream_buffer(&self, buffer: *mut c_void) -> Result<(), NVencError> {
        unsafe {
            (self.function_list.nvenc_destory_bit_stream_buffer)(self.encoder.as_ptr(), buffer)
        }
        .into_error()
    }
}

pub enum CodecPicParams {
    H264(NVencPicParamsH264),
}

pub struct RegisteredResource {
    registered: *mut c_void,
    mapped: *mut c_void,
    pitch: u32,
    width: u32,
    height: u32,
    encoder: Arc<EncoderInternal>,
}

impl Drop for RegisteredResource {
    fn drop(&mut self) {
        unsafe { self.encoder.unmap_input_resource(self.mapped).unwrap() };
        unsafe { self.encoder.unregister_resource(self.registered).unwrap() };
    }
}
