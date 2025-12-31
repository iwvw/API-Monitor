/**
 * 流媒体播放器模块 (Stream Player)
 *
 * 使用 Plyr 作为播放器核心
 *
 * @module stream-player
 */

import Plyr from 'plyr';
import 'plyr/dist/plyr.css';
import { store } from '../store.js';

// ==================== 配置常量 ====================

const PLAYER_CONFIG = {
  // 支持的视频格式
  VIDEO_FORMATS: [
    'mp4',
    'webm',
    'ogg',
    'mkv',
    'ts',
    'avi',
    'wmv',
    'rmvb',
    'rm',
    'asf',
    'vob',
    '3gp',
    'mov',
    'm3u8',
    'flv',
    'mpd',
  ],
};

// Plyr 默认配置
const PLYR_OPTIONS = {
  controls: [
    'play-large',
    'play',
    'progress',
    'current-time',
    'duration',
    'mute',
    'volume',
    'captions',
    'settings',
    'pip',
    'airplay',
    'fullscreen',
  ],
  settings: ['captions', 'quality', 'speed'],
  speed: { selected: 1, options: [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] },
  keyboard: { focused: true, global: true },
  tooltips: { controls: true, seek: true },
  captions: { active: true, language: 'auto', update: false },
  fullscreen: { enabled: true, fallback: true, iosNative: true },
  ratio: '16:9',
  hideControls: true,
  resetOnEnd: false,
  disableContextMenu: false,
  // 手势支持
  clickToPlay: true,
  // 国际化
  i18n: {
    restart: '重新播放',
    rewind: '后退 {seektime}s',
    play: '播放',
    pause: '暂停',
    fastForward: '快进 {seektime}s',
    seek: '跳转',
    seekLabel: '{currentTime} / {duration}',
    played: '已播放',
    buffered: '已缓冲',
    currentTime: '当前时间',
    duration: '总时长',
    volume: '音量',
    mute: '静音',
    unmute: '取消静音',
    enableCaptions: '开启字幕',
    disableCaptions: '关闭字幕',
    download: '下载',
    enterFullscreen: '全屏',
    exitFullscreen: '退出全屏',
    frameTitle: '播放器: {title}',
    captions: '字幕',
    settings: '设置',
    pip: '画中画',
    menuBack: '返回',
    speed: '速度',
    normal: '正常',
    quality: '画质',
    loop: '循环',
    start: '开始',
    end: '结束',
    all: '全部',
    reset: '重置',
    disabled: '禁用',
    enabled: '启用',
    advertisement: '广告',
    qualityBadge: {
      2160: '4K',
      1440: 'HD',
      1080: 'HD',
      720: 'HD',
      576: 'SD',
      480: 'SD',
    },
  },
};

// ==================== 状态管理 ====================

const playerState = {
  // 当前 Plyr 实例
  plyr: null,
  // 视频元素
  videoElement: null,
  // 当前播放信息
  currentFile: null,
  currentUrl: null,
  isPlaying: false,
  isFullscreen: false,

  // 用户偏好 (可持久化)
  userPreferences: {
    volume: 1,
    playbackRate: 1,
  },
};

// ==================== 工具函数 ====================

/**
 * 获取文件扩展名
 * @param {string} filename - 文件名
 * @returns {string} 小写扩展名
 */
function getFileExtension(filename) {
  if (!filename) return '';
  const parts = filename.split('.');
  return parts.length > 1 ? parts.pop().toLowerCase() : '';
}

/**
 * 格式化时间
 * @param {number} seconds - 秒数
 * @returns {string} 格式化的时间字符串
 */
function formatTime(seconds) {
  if (typeof seconds !== 'number' || isNaN(seconds) || !isFinite(seconds) || seconds < 0) {
    return '0:00';
  }
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `${m}:${s.toString().padStart(2, '0')}`;
}

// ==================== 播放器核心 ====================

/**
 * 销毁当前播放器实例
 */
function destroyPlayer() {
  if (playerState.plyr) {
    try {
      playerState.plyr.destroy();
    } catch (e) {
      console.warn('[StreamPlayer] Error destroying Plyr:', e);
    }
    playerState.plyr = null;
  }

  if (playerState.videoElement) {
    playerState.videoElement.pause();
    playerState.videoElement.src = '';
    playerState.videoElement.load();
  }

  playerState.videoElement = null;
  playerState.isPlaying = false;
  playerState.currentFile = null;
  playerState.currentUrl = null;
}

// ==================== 主播放方法 ====================

/**
 * 播放视频
 * @param {Object} options - 播放选项
 * @param {string} options.url - 视频直链
 * @param {string} options.filename - 文件名
 * @param {HTMLVideoElement} options.videoElement - 视频元素
 * @param {Function} [options.onUnsupported] - 不支持格式时的回调
 * @returns {Promise<{success: boolean, message?: string, player?: Plyr}>}
 */
async function play(options) {
  const { url, filename, videoElement, onUnsupported } = options;

  if (!url || !videoElement) {
    return { success: false, message: '缺少必要参数' };
  }

  // 如果已经在播放同一个视频，直接返回
  if (
    playerState.currentUrl === url &&
    playerState.videoElement === videoElement &&
    playerState.plyr
  ) {
    console.log('[StreamPlayer] Already playing this source, resuming.');
    playerState.plyr.play();
    return { success: true, player: playerState.plyr };
  }

  // 如果当前正在播放，先暂停
  if (playerState.plyr && playerState.isPlaying) {
    try {
      playerState.plyr.pause();
    } catch (e) {
      console.warn('[StreamPlayer] Error pausing before switch:', e);
    }
  }

  // 销毁之前的播放器
  destroyPlayer();

  playerState.currentUrl = url;
  playerState.currentFile = filename;
  playerState.videoElement = videoElement;

  const ext = getFileExtension(filename);
  console.log(`[StreamPlayer] Playing with Plyr: ${filename}, ext: ${ext}`);

  try {
    // 设置视频源
    videoElement.src = url;

    // 创建 Plyr 实例
    const plyr = new Plyr(videoElement, {
      ...PLYR_OPTIONS,
      title: filename,
    });

    playerState.plyr = plyr;

    // 绑定事件到 store
    plyr.on('play', () => {
      playerState.isPlaying = true;
      if (store.streamPlayer) {
        store.streamPlayer.playing = true;
      }
    });

    plyr.on('pause', () => {
      playerState.isPlaying = false;
      if (store.streamPlayer) {
        store.streamPlayer.playing = false;
      }
    });

    plyr.on('timeupdate', () => {
      if (store.streamPlayer) {
        store.streamPlayer.currentTime = plyr.currentTime;
        store.streamPlayer.duration = plyr.duration;
      }
    });

    plyr.on('volumechange', () => {
      if (store.streamPlayer) {
        store.streamPlayer.volume = plyr.volume;
        store.streamPlayer.muted = plyr.muted;
      }
    });

    plyr.on('progress', () => {
      if (store.streamPlayer && plyr.buffered > 0) {
        store.streamPlayer.buffered = plyr.buffered;
      }
    });

    plyr.on('loadedmetadata', () => {
      if (store.streamPlayer) {
        store.streamPlayer.duration = plyr.duration;
        store.streamPlayer.loading = false;
      }
    });

    plyr.on('waiting', () => {
      if (store.streamPlayer) {
        store.streamPlayer.loading = true;
      }
    });

    plyr.on('canplay', () => {
      if (store.streamPlayer) {
        store.streamPlayer.loading = false;
      }
    });

    plyr.on('enterfullscreen', () => {
      playerState.isFullscreen = true;
      if (store.streamPlayer) {
        store.streamPlayer.fullscreen = true;
      }
    });

    plyr.on('exitfullscreen', () => {
      playerState.isFullscreen = false;
      if (store.streamPlayer) {
        store.streamPlayer.fullscreen = false;
      }
    });

    plyr.on('ratechange', () => {
      if (store.streamPlayer) {
        store.streamPlayer.playbackRate = plyr.speed;
      }
    });

    plyr.on('error', e => {
      console.error('[StreamPlayer] Plyr error:', e);
      if (onUnsupported) {
        onUnsupported(ext, url, filename);
      }
    });

    // 自动播放
    await plyr.play();

    return { success: true, player: plyr };
  } catch (error) {
    console.error('[StreamPlayer] Failed to create Plyr:', error);
    if (onUnsupported) {
      onUnsupported(ext, url, filename);
    }
    return { success: false, message: '播放器初始化失败' };
  }
}

// ==================== 播放控制 ====================

/**
 * 暂停/继续播放
 */
function togglePlay() {
  if (!playerState.plyr) return;
  playerState.plyr.togglePlay();
}

/**
 * 设置音量
 * @param {number} volume - 0-1
 */
function setVolume(volume) {
  if (!playerState.plyr) return;
  playerState.plyr.volume = Math.max(0, Math.min(1, volume));
  playerState.userPreferences.volume = playerState.plyr.volume;
}

/**
 * 设置播放速度
 * @param {number} rate - 播放速度
 */
function setPlaybackRate(rate) {
  if (!playerState.plyr) return;
  playerState.plyr.speed = rate;
  playerState.userPreferences.playbackRate = rate;
}

/**
 * 跳转到指定时间
 * @param {number} time - 秒数
 */
function seek(time) {
  if (!playerState.plyr) return;
  playerState.plyr.currentTime = Math.max(0, Math.min(time, playerState.plyr.duration || 0));
}

/**
 * 快进/快退
 * @param {number} seconds - 秒数 (正数快进，负数快退)
 */
function skip(seconds) {
  if (!playerState.plyr) return;
  playerState.plyr.forward(seconds);
}

/**
 * 切换全屏
 */
async function toggleFullscreen(container) {
  if (!playerState.plyr) return;
  playerState.plyr.fullscreen.toggle();
}

/**
 * 画中画
 */
async function togglePictureInPicture() {
  if (!playerState.plyr) return;
  playerState.plyr.pip = !playerState.plyr.pip;
}

// ==================== 快捷键处理 ====================

/**
 * 绑定快捷键 (Plyr 已有内置快捷键)
 * @param {HTMLElement} container - 播放器容器
 */
function bindKeyboardShortcuts(container) {
  // Plyr 已内置快捷键支持
  return () => {};
}

// ==================== 格式检查工具 ====================

function isVideoFile(filename) {
  const ext = getFileExtension(filename);
  return PLAYER_CONFIG.VIDEO_FORMATS.includes(ext);
}

/**
 * 获取 Plyr 实例
 */
function getPlayer() {
  return playerState.plyr;
}

/**
 * 获取视频元素
 */
function getVideoElement() {
  return playerState.videoElement;
}

// ==================== 导出 ====================

export const streamPlayer = {
  // 配置
  config: PLAYER_CONFIG,

  // 状态 (只读)
  get state() {
    return { ...playerState };
  },

  // 核心方法
  play,
  destroyPlayer,
  getPlayer,
  getVideoElement,

  // 播放控制
  togglePlay,
  setVolume,
  setPlaybackRate,
  seek,
  skip,
  toggleFullscreen,
  togglePictureInPicture,

  // 工具方法
  isVideoFile,
  getFileExtension,
  formatTime,
  bindKeyboardShortcuts,
};

export default streamPlayer;
