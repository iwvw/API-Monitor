use std::{ffi::c_void, mem::ManuallyDrop};

use crate::sys::{
    enums::{
        NVencAV1PartSize, NVencBFrameRefMode, NVencBitDepth, NVencBufferFormat, NVencBufferUsage,
        NVencCaps, NVencDeviceType, NVencDisplayPicStruct, NVencH264AdaptiveTransformMode,
        NVencH264BDirectMode, NVencH264EntropyCodingMode, NVencH264FMOMode, NVencHEVCCuSize,
        NVencInputResourceType, NVencLookAheadLevel, NVencMVPrecision, NVencMemoryHeap,
        NVencMultiPass, NVencNumRefFrames, NVencOutputStatsLevel, NVencParamsFrameFieldMode,
        NVencParamsRcMode, NVencPicStruct, NVencPicType, NVencQPMapMode, NVencStateRestoreType,
        NVencStereoPackingMode, NVencTemporalFilterLevel, NVencTuningInfo, NVencVUIColorPrimaries,
        NVencVUIMatrixCoeffs, NVencVUITransCharacteristics, NVencVUIVideoFormat,
    },
    version::struct_version,
};

#[derive(Default, Clone, PartialEq, Eq)]
#[repr(C)]
pub struct Guid {
    pub data1: u32,
    pub data2: u16,
    pub data3: u16,
    pub data4: [u8; 8],
}

impl Guid {
    pub const fn from_values(data1: u32, data2: u16, data3: u16, data4: [u8; 8]) -> Self {
        Self {
            data1,
            data2,
            data3,
            data4,
        }
    }

    pub const fn from_u128(uuid: u128) -> Self {
        Self {
            data1: (uuid >> 96) as u32,
            data2: ((uuid >> 80) & 0xffff) as u16,
            data3: ((uuid >> 64) & 0xffff) as u16,
            data4: (uuid as u64).to_be_bytes(),
        }
    }
}

#[repr(C)]
pub struct NVencCapsParam {
    version: u32,
    caps_to_query: NVencCaps,
    rsvd: [u32; 62],
}

pub const NV_ENC_CAPS_PARAM_VERSION: u32 = struct_version(1);

#[repr(C)]
pub struct NVencRestoreEncoderStateParams {
    version: u32,
    buffer: u32,
    state: NVencStateRestoreType,
    rsvd: u32,
    output_bit_stream: *mut c_void,
    completion_event: *mut c_void,
    rsvd1: [u32; 64],
    rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_RESTORE_STATE_PARAMS_VER: u32 = struct_version(2);

#[repr(C)]
pub struct NVencOutputStatsBlock {
    version: u32,
    qp: u8,
    rsvd: [u8; 3],
    bit_count: u32,
    satd_cost: u32,
    rsvd1: [u32; 12],
}

pub const NV_ENC_OUTPUT_STATS_BLOCK_VER: u32 = struct_version(1);

#[repr(C)]
pub struct NVencOutputStatsRow {
    version: u32,
    qp: u8,
    rsvd: [u8; 3],
    bit_count: u32,
    satd_cost: u32,
    rsvd1: [u32; 12],
}

pub const NV_ENC_OUTPUT_STATS_ROW_VER: u32 = struct_version(1);

#[repr(C)]
pub struct NVencEncodeOutParams {
    version: u32,
    bitstream_size_in_bytes: u32,
    rsvd: [u32; 62],
}

pub const NV_ENC_ENCODE_OUT_PARAMS_VER: u32 = struct_version(1);

#[repr(C)]
pub struct NVencLookAheadPicParams {
    version: u32,
    rsvd: u32,
    input_buffer: *mut c_void,
    picture_type: NVencPicType,
    rsvd1: [u32; 63],
    rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_LOOKAHEAD_PIC_PARAMS_VER: u32 = struct_version(2);

#[repr(C)]
pub struct NVencCreateInputBuffer {
    pub version: u32,
    pub width: u32,
    pub height: u32,
    pub memory_heap: NVencMemoryHeap,
    pub buffer_fmt: NVencBufferFormat,
    pub rsvd: u32,
    pub input_buffer: *mut c_void,
    pub p_sys_mem_buffer: *mut c_void,
    pub rsvd1: [u32; 58],
    pub rsvd2: [*mut c_void; 63],
}

pub const NV_ENC_CREATE_INPUT_BUFFER_VER: u32 = struct_version(2);

#[repr(C)]
pub struct NVencCreateBitstreamBuffer {
    pub version: u32,
    #[deprecated]
    size: u32,
    #[deprecated]
    memory_heap: NVencMemoryHeap,
    rsvd: u32,
    pub bitstream_buffer: *mut c_void,
    bitstream_buffer_ptr: *mut c_void,
    rsvd1: [u32; 58],
    rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_CREATE_BITSTREAM_BUFFER_VER: u32 = struct_version(1);

#[repr(C)]
pub struct NVencMVector {
    mvx: i16,
    mvy: i16,
}

#[repr(C)]
pub struct NVencH264MVData {
    mv: [NVencMVector; 4],
    mb_type: u8,
    partition_type: u8,
    rsvd: u16,
    mb_cost: u32,
}

#[repr(C)]
pub struct NVencHEVCMVData {
    mv: [NVencMVector; 4],
    cu_type: u8,
    cu_size: u8,
    partition_mode: u8,
    last_cu_int_ctb: u8,
}

#[repr(C)]
pub struct NVencCreateMVBuffer {
    version: u32,
    rsvd: u32,
    mv_buffer: *mut c_void,
    rsvd1: [u32; 254],
    rsvd2: [*mut c_void; 63],
}

pub const NV_ENC_CREATE_MV_BUFFER_VER: u32 = struct_version(2);

#[repr(C)]
pub struct NVencQP {
    qp_inter_p: u32,
    qp_inter_b: u32,
    qp_intra: u32,
}

pub const MAX_NUM_VIEWS_MINUS_1: usize = 7;

#[repr(C)]
pub struct NVencRcParams {
    version: u32,
    pub rate_control_mode: NVencParamsRcMode,
    const_qp: NVencQP,
    pub average_bit_rate: u32,
    pub max_bit_rate: u32,
    pub vbv_buffer_size: u32,
    vbv_initial_delay: u32,
    // TODO: bit fields
    bit_fields: NVencRcParamsBitFlags,
    min_qp: NVencQP,
    max_qp: NVencQP,
    initial_rcqp: NVencQP,
    temporal_layer_idx_mask: u32,
    temporal_layer_qp: [u8; 8],
    target_quality: u8,
    target_quality_lsb: u8,
    pub look_ahead_depth: u16,
    low_delay_key_frame_scale: u8,
    y_dc_qp_index_offset: i8,
    u_dc_qp_index_offset: i8,
    v_dc_qp_index_offset: i8,
    qp_map_mode: NVencQPMapMode,
    multi_pass: NVencMultiPass,
    alpha_layer_bitrate_ratio: u32,
    cb_qp_index_offset: i8,
    cr_qp_index_offset: i8,
    rsvd2: u16,
    lookahead_level: NVencLookAheadLevel,
    view_bitrate_ratios: [u8; MAX_NUM_VIEWS_MINUS_1],
    rsvd3: u8,
    rsvd1: u32,
}

#[repr(transparent)]
pub struct NVencRcParamsBitFlags(u32);

impl NVencRcParamsBitFlags {}

pub const NV_ENC_RC_PARAMS_VER: u32 = struct_version(1);

pub const MAX_NUM_CLOCK_TS: usize = 3;

#[repr(C)]
pub struct NVencClockTimeStampSet {
    pub bitflags: u32,
    pub time_offset: u32,
}

impl NVencClockTimeStampSet {}

#[repr(C)]
pub struct NVencTimeCode {
    pub display_pic_struct: NVencDisplayPicStruct,
    pub clock_timestamp: [NVencClockTimeStampSet; MAX_NUM_CLOCK_TS],
    pub skip_clock_timestamp_insertion: u32,
}

pub const MULTIVIEW_MAX_NUM_REF_DISPLAY: usize = 32;

#[repr(C)]
pub struct HEVC3DReferenceDisplayInfo {
    // TODO
    pub bitflags: u32,
    pub prec_ref_display_width: i32,
    pub prec_ref_viewing_dist: i32,
    pub num_ref_displays_minus1: i32,
    pub left_view_id: [i32; MULTIVIEW_MAX_NUM_REF_DISPLAY],
    pub right_view_id: [i32; MULTIVIEW_MAX_NUM_REF_DISPLAY],
    pub exponent_ref_display_width: [i32; MULTIVIEW_MAX_NUM_REF_DISPLAY],
    pub mantissa_ref_display_width: [i32; MULTIVIEW_MAX_NUM_REF_DISPLAY],
    pub exponent_ref_viewing_distance: [i32; MULTIVIEW_MAX_NUM_REF_DISPLAY],
    pub mantissa_ref_viewing_distance: [i32; MULTIVIEW_MAX_NUM_REF_DISPLAY],
    pub num_sample_shift_plus512: [i32; MULTIVIEW_MAX_NUM_REF_DISPLAY],
    pub additional_shift_present_flag: [u8; MULTIVIEW_MAX_NUM_REF_DISPLAY],
    pub rsvd2: [u32; 4],
}

#[repr(C)]
pub struct ChromaPoints {
    x: u16,
    y: u16,
}

#[repr(C)]
pub struct MasteringDisplayInfo {
    pub g: ChromaPoints,
    pub b: ChromaPoints,
    pub r: ChromaPoints,
    pub white_point: ChromaPoints,
    pub max_luma: u32,
    pub min_luma: u32,
}

#[repr(C)]
pub struct ContentLightLevel {
    pub max_content_light_level: u16,
    pub max_pic_average_light_level: u16,
}

#[repr(C)]
pub struct NVencConfigH264VUIParameters {
    overscan_info_present_flag: u32,
    overscan_info: u32,
    video_signal_type_present_flag: u32,
    video_format: NVencVUIVideoFormat,
    video_full_range_flag: u32,
    color_description_present_flag: u32,
    color_primaries: NVencVUIColorPrimaries,
    transfer_characteristics: NVencVUITransCharacteristics,
    color_matrix: NVencVUIMatrixCoeffs,
    chroma_sample_location_flag: u32,
    chroma_sample_location_top: u32,
    chroma_sample_location_bot: u32,
    bitstream_restriction_flag: u32,
    timing_info_present_flag: u32,
    num_unit_in_ticks: u32,
    time_scale: u32,
    rsvd: [u32; 12],
}

pub type NVencConfigHEVCVUIParamaters = NVencConfigH264VUIParameters;

#[repr(C)]
#[derive(Default)]
pub struct NVencExternalMeHintCountsPerBlockType {
    pub bitflags: u32,
    pub rsvd1: [u32; 3],
}

#[repr(transparent)]
pub struct NVencExternalMeHint(i32);

#[repr(C)]
pub struct NVencExternalMeSbHint {
    flags1: i16,
    flags2: i16,
    flags3: i16,
}

#[repr(C)]
pub struct NVencConfigH264 {
    bitflags: u32,
    level: u32,
    idr_period: u32,
    seperate_color_plane_flag: u32,
    disable_deblocking_filter_idc: u32,
    num_termpoal_layers: u32,
    sps_id: u32,
    pps_id: u32,
    adaptive_transform_mode: NVencH264AdaptiveTransformMode,
    fmo_mode: NVencH264FMOMode,
    bdirect_mode: NVencH264BDirectMode,
    entropy_coding_mode: NVencH264EntropyCodingMode,
    stereo_mode: NVencStereoPackingMode,
    intra_refresh_period: u32,
    intra_refresh_count: u32,
    max_num_ref_frames: u32,
    slice_mode: u32,
    slice_mode_data: u32,
    h264_vui_parameters: NVencConfigH264VUIParameters,
    ltr_num_frames: u32,
    ltr_trust_mode: u32,
    chroma_format_idc: u32,
    max_termpoal_layers: u32,
    use_b_frames_as_ref: NVencBFrameRefMode,
    num_ref_l0: NVencNumRefFrames,
    num_ref_l1: NVencNumRefFrames,
    output_bit_depth: NVencBitDepth,
    input_bit_depth: NVencBitDepth,
    tf_level: NVencTemporalFilterLevel,
    rsvd1: [u32; 264],
    rsvd: [*mut c_void; 64],
}

#[repr(C)]
pub struct NVencConfigHEVC {
    level: u32,
    tier: u32,
    min_cu_size: NVencHEVCCuSize,
    max_cu_size: NVencHEVCCuSize,
    bitflags: u32,
    idr_period: u32,
    intra_refresh_cnt: u32,
    max_num_ref_frames_in_dpb: u32,
    ltr_num_frames: u32,
    vps_id: u32,
    sps_id: u32,
    pps_id: u32,
    slice_mode: u32,
    slice_mode_data: u32,
    max_temporal_layers_minus_1: u32,
    hevc_vui_paramters: NVencConfigHEVCVUIParamaters,
    ltr_trust_mode: u32,
    use_b_frames_as_ref: NVencBFrameRefMode,
    tf_level: NVencTemporalFilterLevel,
    disable_deblocking_filter_idc: u32,
    output_bit_depth: NVencBitDepth,
    input_bit_depth: NVencBitDepth,
    num_temporal_layers: u32,
    num_views: u32,
    rsvd1: [u32; 208],
    rsvd2: [*mut c_void; 64],
}

pub const NV_MAX_TILE_COLS_AV1: usize = 64;
pub const NV_MAX_TILE_ROWS_AV1: usize = 64;

#[repr(C)]
pub struct NVencFilmGrainParamsAV1 {
    bitfields: u32,
    point_y_value: [u8; 14],
    point_y_scaling: [u8; 14],
    point_cb_value: [u8; 10],
    point_cb_scaling: [u8; 10],
    point_cr_value: [u8; 10],
    point_cr_scaling: [u8; 10],
    ar_coeffs_y_plus128: [u8; 24],
    ar_coeffs_cb_plus128: [u8; 25],
    ar_coeffs_cr_plus128: [u8; 25],
    rsvd2: [u8; 2],
    cb_mult: u8,
    cb_luma_mult: u8,
    cb_offset: u16,
    cr_mult: u8,
    cr_luma_mult: u8,
    cr_offset: u16,
}

#[repr(C)]
pub struct NVencConfigAV1 {
    level: u32,
    tier: u32,
    min_part_size: NVencAV1PartSize,
    max_part_size: NVencAV1PartSize,
    bitflags: u32,
    idr_period: u32,
    intra_refresh_period: u32,
    intra_refresh_count: u32,
    max_num_ref_frames_in_dpb: u32,
    num_tiles_columns: u32,
    num_tile_rows: u32,
    rsvd2: u32,
    tile_widths: *mut u32,
    tile_heights: *mut u32,
    max_temporal_layers_minus_1: u32,
    color_primaries: NVencVUIColorPrimaries,
    transfer_characteristics: NVencVUITransCharacteristics,
    matrix_coefficients: NVencVUIMatrixCoeffs,
    color_range: u32,
    chroma_sample_position: u32,
    use_b_frames_as_ref: NVencBFrameRefMode,
    film_grain_params: NVencFilmGrainParamsAV1,
    num_fwd_refs: NVencNumRefFrames,
    num_bwd_refs: NVencNumRefFrames,
    input_bit_depth: NVencBitDepth,
    ltr_num_frames: u32,
    num_temporal_layers: u32,
    tf_level: NVencTemporalFilterLevel,
    rsvd1: [u32; 230],
    rsvd3: [*mut c_void; 62],
}

#[repr(C)]
pub struct NVencConfigH264MeOnly {
    bitflags: u32,
    rsvd1: [u32; 255],
    rsvd2: [*mut c_void; 64],
}

#[repr(C)]
pub struct NVencConfigHEVCMeOnly {
    rsvd: [u32; 256],
    rsvd1: [*mut c_void; 64],
}

#[repr(C)]
pub union NVencCodecConfig {
    h264: ManuallyDrop<NVencConfigH264>,
    hevc: ManuallyDrop<NVencConfigHEVC>,
    av1: ManuallyDrop<NVencConfigAV1>,
    h264_meonly: ManuallyDrop<NVencConfigH264MeOnly>,
    hevc_meonly: ManuallyDrop<NVencConfigHEVCMeOnly>,
    rsvd: [u32; 320],
}

#[repr(C)]
pub struct NVencConfig {
    pub version: u32,
    pub profile_guid: Guid,
    pub gop_len: u32,
    pub frame_interval_p: i32,
    mono_chrome_encoding: u32,
    frame_field_mode: NVencParamsFrameFieldMode,
    mv_precision: NVencMVPrecision,
    pub rc_params: NVencRcParams,
    encode_codec_config: NVencCodecConfig,
    rsvd: [u32; 278],
    rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_CONFIG_VER: u32 = struct_version(9) | (1 << 31);

#[bitfields::bitfield(u32)]
pub struct NVencInitializeParamsBitfields {
    #[bits(1)]
    pub report_slice_offsets: bool,
    #[bits(1)]
    pub enable_sub_frame_write: bool,
    #[bits(1)]
    pub enable_external_me_hints: bool,
    #[bits(1)]
    pub enable_me_only_mode: bool,
    #[bits(1)]
    pub enable_weighted_prediction: bool,
    #[bits(4)]
    pub split_encode_mode: u8,
    #[bits(1)]
    pub enable_output_in_vid_mem: bool,
    #[bits(1)]
    pub enable_recon_frame_output: bool,
    #[bits(1)]
    pub enable_output_stats: bool,
    #[bits(1)]
    pub enable_uni_directional_b: bool,
    #[bits(19)]
    pub rsvd: u32,
}

#[repr(C)]
pub struct NVencInitializeParams {
    pub version: u32,
    pub encode_guid: Guid,
    pub preset_guid: Guid,
    pub encode_width: u32,
    pub encode_height: u32,
    pub dar_width: u32,
    pub dar_height: u32,
    pub frame_rate_num: u32,
    pub frame_rate_den: u32,
    pub enable_encode_async: u32,
    pub enable_ptd: u32,
    pub bit_flags: NVencInitializeParamsBitfields,
    pub(crate) priv_data_size: u32,
    pub(crate) rsvd: u32,
    pub(crate) priv_data: *mut c_void,
    pub encode_config: *mut NVencConfig,
    pub max_encode_width: u32,
    pub max_encode_height: u32,
    pub max_me_hint_counts_per_block: [NVencExternalMeHintCountsPerBlockType; 2],
    pub tuning_info: NVencTuningInfo,
    pub buffer_format: NVencBufferFormat,
    pub num_state_buffers: u32,
    pub output_stats_level: NVencOutputStatsLevel,
    pub(crate) rsvd1: [u32; 284],
    pub(crate) rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_INITIALIZE_PARAMS_VER: u32 = struct_version(7) | (1 << 31);

#[repr(C)]
pub struct NVencReconfigureParams {
    pub version: u32,
    pub rsvd: u32,
    pub re_init_encode_params: NVencInitializeParams,
    pub bitflags: u32,
    pub rsvd2: u32,
}

pub const NV_ENC_RECONFIGURE_PARAMS_VER: u32 = struct_version(2) | (1 << 31);

#[repr(C)]
pub struct NVencPresetConfig {
    pub version: u32,
    rsvd: u32,
    pub preset_cfg: NVencConfig,
    rsvd1: [u32; 256],
    rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_PRESET_CONFIG_VER: u32 = struct_version(5) | (1 << 31);

#[repr(C)]
pub struct NVencPicParamsMVC {
    version: u32,
    view_id: u32,
    temporal_id: u32,
    priority_id: u32,
    rsvd1: [u32; 12],
    rsvd2: [*mut c_void; 8],
}

pub const NV_ENC_PIC_PARAMS_MVC_VER: u32 = struct_version(1);

#[repr(C)]
pub union NVencPicParamsH264EXT {
    pub mvc_pic_params: ManuallyDrop<NVencPicParamsMVC>,
    pub rsvd1: [u32; 32],
}

#[repr(C)]
pub struct NVencSeiPayload {
    pub payload_size: u32,
    pub payload_type: u32,
    pub payload: *mut u8,
}

pub type NVencH264SeiPayload = NVencSeiPayload;

#[repr(C)]
pub struct NVencPicParamsH264 {
    pub display_poc_syntax: u32,
    pub rsvd3: u32,
    pub ref_pic_flag: u32,
    pub color_plane_id: u32,
    pub force_intra_refresh_with_frame_count: u32,
    pub bitflags: u32,
    pub slice_type_data: *mut u8,
    pub slice_type_array_count: u32,
    pub sei_payload_array_count: u32,
    pub sei_payload_array: *mut NVencSeiPayload,
    pub slice_mode: u32,
    pub slice_mode_data: u32,
    pub ltr_mark_framce_idx: u32,
    pub ltr_use_frame_bitmap: u32,
    pub ltr_usage_mode: u32,
    pub force_intra_slice_count: u32,
    pub force_intra_slice_idx: *mut u32,
    pub h264_ext_pic_params: NVencPicParamsH264EXT,
    pub time_code: NVencTimeCode,
    pub rsvd: [u32; 202],
    pub rsvd2: [*mut c_void; 61],
}

#[repr(C)]
pub struct NVencPicParamsHEVC {
    pub display_poc_syntax: u32,
    pub ref_pic_flag: u32,
    pub temporal_id: u32,
    pub force_intra_refresh_with_frame_count: u32,
    pub bitflags: u32,
    pub rsvd1: u32,
    pub slice_type_data: *mut u8,
    pub slice_type_array_count: u32,
    pub slice_mode: u32,
    pub slice_mode_data: u32,
    pub ltr_mark_frame_idx: u32,
    pub ltr_usage_frame_bitmap: u32,
    pub ltr_usage_mode: u32,
    pub sei_payload_array_count: u32,
    pub rsvd: u32,
    pub sei_payload_array: *mut NVencSeiPayload,
    pub time_code: NVencTimeCode,
    pub num_temporal_layers: u32,
    pub view_id: u32,
    pub _3d_reference_display_info: *mut HEVC3DReferenceDisplayInfo,
    pub max_cll: *mut ContentLightLevel,
    pub mastering_display: *mut MasteringDisplayInfo,
    pub rsvd2: [u32; 234],
    pub rsvd3: [*mut c_void; 58],
}

pub type NVencAV1OBUPayload = NVencSeiPayload;

#[repr(C)]
pub struct NVencPicParamsAV1 {
    display_poc_syntax: u32,
    ref_pic_flag: u32,
    temporal_id: u32,
    force_intra_refresh_with_frame_count: u32,
    bitflags: u32,
    num_tile_columns: u32,
    num_tile_rows: u32,
    rsvd: u32,
    tile_widths: *mut u32,
    tile_heights: *mut u32,
    obu_payload_array_count: u32,
    rsvd1: u32,
    obu_payload_array: *mut NVencAV1OBUPayload,
    file_grain_params: *mut NVencFilmGrainParamsAV1,
    ltr_mark_frame_idx: u32,
    ltr_use_frame_bitmap: u32,
    num_temporal_layers: u32,
    rsvd4: u32,
    max_cll: *mut ContentLightLevel,
    mastering_display: *mut MasteringDisplayInfo,
    rsvd2: [u32; 242],
    rsvd3: [*mut c_void; 59],
}

#[repr(C)]
pub union NVencCodecPicParams {
    pub h264_pic_params: ManuallyDrop<NVencPicParamsH264>,
    pub hevc_pic_params: ManuallyDrop<NVencPicParamsHEVC>,
    av1_pic_params: ManuallyDrop<NVencPicParamsAV1>,
    pub rsvd: [u32; 256],
}

#[repr(C)]
pub struct NVencPicParams {
    pub version: u32,
    pub input_width: u32,
    pub input_height: u32,
    pub input_pitch: u32,
    pub encode_pic_flags: u32,
    pub frame_idx: u32,
    pub input_time_stamp: u64,
    input_duration: u64,
    pub input_buffer: *mut c_void,
    pub output_bitstream: *mut c_void,
    // TODO: Windows only, needed for async
    completion_event: *mut c_void,
    pub buffer_format: NVencBufferFormat,
    pub picture_struct: NVencPicStruct,
    pub picture_type: NVencPicType,
    pub codec_pic_params: NVencCodecPicParams,
    me_hint_counts_per_block: [NVencExternalMeHintCountsPerBlockType; 2],
    me_external_hints: *mut NVencExternalMeHint,
    rsvd2: [u32; 7],
    rsvd5: [*mut c_void; 2],
    qp_delta_map: *mut i8,
    qp_deltra_map_size: u32,
    rsvd_bit_fields: u32,
    me_hint_ref_pic_dist: [u16; 2],
    rsvd4: u32,
    alpha_buffer: *mut c_void,
    me_external_sb_hints: *mut NVencExternalMeSbHint,
    me_sb_hints_count: u32,
    state_buffer_idx: u32,
    output_recon_buffer: *mut c_void,
    rsvd3: [u32; 284],
    rsvd6: [*mut c_void; 57],
}

pub const NV_ENC_PIC_PARAMS_VER: u32 = struct_version(7) | (1 << 31);

#[repr(C)]
pub struct NVencMeOnlyParams {
    version: u32,
    input_width: u32,
    input_height: u32,
    rsvd: u32,
    input_buffer: *mut c_void,
    reference_frame: *mut c_void,
    mv_buffer: *mut c_void,
    rsvd2: u32,
    buffer_formtat: NVencBufferFormat,
    view_id: u32,
    me_hint_counts_per_block: [NVencExternalMeHintCountsPerBlockType; 2],
    me_external_hints: NVencExternalMeHint,
    rsvd1: [u32; 241],
    rsvd3: [*mut c_void; 59],
}

pub const NV_ENC_MEONLY_PARAMS_VER: u32 = struct_version(4);

#[bitfields::bitfield(u32)]
pub struct NVencLockBitStreamBitFields {
    #[bits(1)]
    pub do_not_wait: bool,
    #[bits(1)]
    pub ltr_frame: bool,
    #[bits(1)]
    pub get_rc_stats: bool,
    #[bits(29)]
    rsvd: u32,
}

#[repr(C)]
pub struct NVencLockBitStream {
    pub version: u32,
    pub bit_fields: NVencLockBitStreamBitFields,
    pub output_bit_stream: *mut c_void,
    slice_offsets: *mut u32,
    frame_idx: u32,
    hw_encode_status: u32,
    num_slices: u32,
    pub bitstream_size_in_bytes: u32,
    output_time_stamp: u64,
    pub output_duration: u64,
    pub bitstream_buffer: *mut c_void,
    picture_type: NVencPicType,
    picture_struct: NVencPicStruct,
    framge_avg_qp: u32,
    frame_satd: u32,
    ltr_frame_idx: u32,
    ltr_frame_bitmap: u32,
    temporal_id: u32,
    inter_mb_count: u32,
    average_mvx: i32,
    average_mvy: i32,
    alpha_layer_size_in_bytes: u32,
    output_stats_ptr_size: u32,
    rsvd: u32,
    output_stats_ptr: *mut c_void,
    frame_idx_display: u32,
    rsvd1: [u32; 219],
    rsvd2: [*mut c_void; 63],
    rsvd_interanl: [u32; 8],
}

pub const NV_ENC_LOCK_BITSTREAM_VER: u32 = struct_version(2) | (1 << 31);

#[repr(C)]
pub struct NVencLockInputBuffer {
    pub version: u32,
    pub bitflags: u32,
    pub input_buffer: *mut c_void,
    pub buffer_data_ptr: *mut c_void,
    pub pitch: u32,
    pub rsvd1: [u32; 251],
    pub rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_LOCK_INPUT_BUFFER_VER: u32 = struct_version(1);

#[repr(C)]
pub struct NVencMapInputResource {
    pub version: u32,
    sub_resource_idx: u32,
    input_resource: *mut c_void,
    pub registered_resource: *mut c_void,
    pub mapped_resource: *mut c_void,
    pub mapped_buffer_format: NVencBufferFormat,
    rsvd1: [u32; 251],
    rsvd: [*mut c_void; 63],
}

pub const NV_ENC_MAP_INPUT_RESOURCE_VER: u32 = struct_version(4);

#[repr(C)]
pub struct NVencInputResourceOpenGLTex {
    texture: u32,
    target: u32,
}

#[repr(C)]
pub struct NVencFencePointD3D12 {
    version: u32,
    rsvd: u32,
    p_fence: *mut c_void,
    wait_value: u64,
    signal_value: u64,
    bit_flags: u32,
    rsvd1: [u32; 7],
}

pub const NV_ENC_FENCE_POINT_D3D12_VER: u32 = struct_version(1);

#[repr(C)]
pub struct NVencInputResourceD3D12 {
    version: u32,
    rsvd: u32,
    input_buffer: *mut c_void,
    input_fence_point: NVencFencePointD3D12,
    rsvd1: [u32; 16],
    rsvd2: [*mut c_void; 16],
}

pub const NV_ENC_INPUT_RESOURCE_D3D12_VER: u32 = struct_version(1);

#[repr(C)]
pub struct NVencOutputResourceD3D12 {
    version: u32,
    rsvd: u32,
    output_buffer: *mut c_void,
    output_fence_point: NVencFencePointD3D12,
    rsvd1: [u32; 16],
    rsvd2: [*mut c_void; 16],
}

pub const NV_ENC_OUTPUT_RESOURCE_D3D12_VER: u32 = struct_version(1);

#[repr(C)]
pub struct NVencRegisterResource {
    pub version: u32,
    pub resource_type: NVencInputResourceType,
    pub width: u32,
    pub height: u32,
    pub pitch: u32,
    pub sub_resource_index: u32,
    pub resource_to_register: *mut c_void,
    pub registered_resource: *mut c_void,
    pub buffer_format: NVencBufferFormat,
    pub buffer_usage: NVencBufferUsage,
    pub input_fence_point: *mut NVencFencePointD3D12,
    pub chroma_offset: [u32; 2],
    pub chroma_offset_in: [u32; 2],
    pub(crate) rsvd1: [u32; 244],
    pub(crate) rsvd2: [*mut c_void; 61],
}

pub const NV_ENC_REGISTER_RESOURCE_VER: u32 = struct_version(5);

#[repr(C)]
pub struct NVencStat {
    version: u32,
    rsvd: u32,
    output_bit_stream: *mut c_void,
    bit_stream_size: u32,
    pic_type: u32,
    last_valid_byte_offset: u32,
    slice_offsets: [u32; 16],
    pic_idx: u32,
    frame_avg_qp: u32,
    bit_flags: u32,
    ltr_frame_idx: u32,
    intra_mb_count: u32,
    inter_mb_count: u32,
    average_mv_x: i32,
    average_mv_y: i32,
    rsvd1: [u32; 227],
    rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_STAT_VER: u32 = struct_version(2);

#[repr(C)]
pub struct NVencSequenceParamPayload {
    version: u32,
    in_buffer_size: u32,
    sps_id: u32,
    pps_id: u32,
    spspps_buffer: *mut c_void,
    output_spspps_payload_size: *mut u32,
    rsvd: [u32; 250],
    rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_SEQUENCE_PARAM_PAYLOAD_VER: u32 = struct_version(1);

#[repr(C)]
pub struct NVencEventParams {
    version: u32,
    rsvd: u32,
    completion_event: *mut c_void,
    rsvd1: [u32; 254],
    rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_EVENT_PARAMS_VER: u32 = struct_version(2);

#[repr(C)]
pub struct NVencOpenEncodeSessionExParams {
    pub version: u32,
    pub device_type: NVencDeviceType,
    pub device: *mut c_void,
    rsvd: *mut c_void,
    pub api_version: u32,
    rsvd1: [u32; 253],
    rsvd2: [*mut c_void; 64],
}

pub const NV_ENC_OPEN_ENCODE_SESSION_EX_PARAMS_VER: u32 = struct_version(1);
