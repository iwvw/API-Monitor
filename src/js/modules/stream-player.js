/**
 * 流媒体播放器模块 (Stream Player)
 * 
 * 功能:
 * - 原生视频播放 (MP4, WebM, MKV 等浏览器支持的格式)
 * - 沉浸式全屏播放体验
 * - 快捷键控制
 * 
 * @module stream-player
 */

import { store } from '../store.js';
import { toast } from './toast.js';

// ==================== 配置常量 ====================

const PLAYER_CONFIG = {
    // 支持的视频格式
    VIDEO_FORMATS: ['mp4', 'webm', 'ogg', 'mkv', 'ts', 'avi', 'wmv', 'rmvb', 'rm', 'asf', 'vob', '3gp', 'mov', 'm3u8', 'flv', 'mpd']
};

// ==================== 状态管理 ====================

const playerState = {
    // 当前播放器实例
    videoElement: null,

    // 当前播放信息
    currentFile: null,
    currentUrl: null,
    isPlaying: false,
    isFullscreen: false,

    // 用户偏好 (可持久化)
    userPreferences: {
        volume: 1,
        playbackRate: 1
    }
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
    if (playerState.videoElement) {
        playerState.videoElement.pause();
        playerState.videoElement.src = '';
        playerState.videoElement.load();
    }

    playerState.isPlaying = false;
    playerState.currentFile = null;
    playerState.currentUrl = null;
}

/**
 * 原生播放
 * @param {HTMLVideoElement} video - 视频元素
 * @param {string} url - 视频 URL
 */
async function playNative(video, url) {
    video.src = url;
    await video.play();
    playerState.isPlaying = true;
}

// ==================== 主播放方法 ====================

/**
 * 播放视频
 * @param {Object} options - 播放选项
 * @param {string} options.url - 视频直链
 * @param {string} options.filename - 文件名
 * @param {HTMLVideoElement} options.videoElement - 视频元素
 * @param {Function} [options.onUnsupported] - 不支持格式时的回调
 * @returns {Promise<{success: boolean, message?: string}>}
 */
async function play(options) {
    const { url, filename, videoElement, onUnsupported } = options;

    if (!url || !videoElement) {
        return { success: false, message: '缺少必要参数' };
    }

    // 检查是否已经是当前正在播放的资源且元素一致
    if (playerState.currentUrl === url && playerState.videoElement === videoElement && videoElement.src) {
        console.log('[StreamPlayer] Already playing this source on this element, skipping reload.');
        playerState.isPlaying = true;
        videoElement.play().catch(() => { });
        return { success: true };
    }

    // 销毁之前的播放器状态
    destroyPlayer();

    playerState.videoElement = videoElement;
    playerState.currentUrl = url;
    playerState.currentFile = filename;

    const ext = getFileExtension(filename);
    console.log(`[StreamPlayer] Playing: ${filename}, ext: ${ext}`);

    try {
        // 直接尝试原生播放
        await playNative(videoElement, url);
        return { success: true };
    } catch (error) {
        console.warn('[StreamPlayer] Playback failed:', error);
        if (onUnsupported) {
            onUnsupported(ext, url, filename);
        }
        return { success: false, message: '该视频格式可能不受支持，请下载后用本地播放器观看' };
    }
}

// ==================== 播放控制 ====================

/**
 * 暂停/继续播放
 */
function togglePlay() {
    if (!playerState.videoElement) return;

    if (playerState.videoElement.paused) {
        playerState.videoElement.play();
        playerState.isPlaying = true;
    } else {
        playerState.videoElement.pause();
        playerState.isPlaying = false;
    }
}

/**
 * 设置音量
 * @param {number} volume - 0-1
 */
function setVolume(volume) {
    if (!playerState.videoElement) return;
    playerState.videoElement.volume = Math.max(0, Math.min(1, volume));
    playerState.userPreferences.volume = playerState.videoElement.volume;
}

/**
 * 设置播放速度
 * @param {number} rate - 播放速度
 */
function setPlaybackRate(rate) {
    if (!playerState.videoElement) return;
    playerState.videoElement.playbackRate = rate;
    playerState.userPreferences.playbackRate = rate;
}

/**
 * 跳转到指定时间
 * @param {number} time - 秒数
 */
function seek(time) {
    if (!playerState.videoElement) return;
    playerState.videoElement.currentTime = Math.max(0, Math.min(time, playerState.videoElement.duration || 0));
}

/**
 * 快进/快退
 * @param {number} seconds - 秒数 (正数快进，负数快退)
 */
function skip(seconds) {
    if (!playerState.videoElement) return;
    seek(playerState.videoElement.currentTime + seconds);
}

/**
 * 切换全屏
 */
async function toggleFullscreen(container) {
    const elem = container || playerState.videoElement?.parentElement;
    const video = playerState.videoElement;
    if (!elem) return;

    if (!document.fullscreenElement) {
        try {
            await (elem.requestFullscreen?.() || elem.webkitRequestFullscreen?.() || elem.msRequestFullscreen?.());
            playerState.isFullscreen = true;

            // 移动端自动横竖屏切换
            if (video && screen.orientation && screen.orientation.lock) {
                const isLandscape = video.videoWidth > video.videoHeight;
                try {
                    await screen.orientation.lock(isLandscape ? 'landscape' : 'portrait');
                } catch (e) {
                    console.log('[StreamPlayer] Orientation lock not supported or failed:', e);
                }
            }
        } catch (e) {
            console.error('[StreamPlayer] Fullscreen request failed:', e);
        }
    } else {
        try {
            if (screen.orientation && screen.orientation.unlock) {
                screen.orientation.unlock();
            }
            await (document.exitFullscreen?.() || document.webkitExitFullscreen?.() || document.msExitFullscreen?.());
            playerState.isFullscreen = false;
        } catch (e) {
            console.error('[StreamPlayer] Exit fullscreen failed:', e);
        }
    }
}

/**
 * 画中画
 */
async function togglePictureInPicture() {
    if (!playerState.videoElement) return;

    if (document.pictureInPictureElement) {
        await document.exitPictureInPicture();
    } else if (playerState.videoElement.requestPictureInPicture) {
        await playerState.videoElement.requestPictureInPicture();
    }
}

// ==================== 快捷键处理 ====================

/**
 * 绑定快捷键
 * @param {HTMLElement} container - 播放器容器
 */
function bindKeyboardShortcuts(container) {
    const handler = (e) => {
        // 忽略输入框中的按键
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;

        switch (e.key.toLowerCase()) {
            case ' ':
            case 'k':
                e.preventDefault();
                togglePlay();
                break;
            case 'f':
                e.preventDefault();
                toggleFullscreen(container);
                break;
            case 'p':
                if (e.shiftKey) {
                    e.preventDefault();
                    togglePictureInPicture();
                }
                break;
            case 'm':
                e.preventDefault();
                if (playerState.videoElement) {
                    playerState.videoElement.muted = !playerState.videoElement.muted;
                }
                break;
            case 'arrowleft':
                e.preventDefault();
                skip(e.shiftKey ? -30 : -5);
                break;
            case 'arrowright':
                e.preventDefault();
                skip(e.shiftKey ? 30 : 5);
                break;
            case 'arrowup':
                e.preventDefault();
                setVolume((playerState.videoElement?.volume || 0) + 0.1);
                break;
            case 'arrowdown':
                e.preventDefault();
                setVolume((playerState.videoElement?.volume || 0) - 0.1);
                break;
            case 'escape':
                if (playerState.isFullscreen) {
                    toggleFullscreen(container);
                }
                break;
            case ',':
                e.preventDefault();
                setPlaybackRate(Math.max(0.25, (playerState.videoElement?.playbackRate || 1) - 0.25));
                break;
            case '.':
                e.preventDefault();
                setPlaybackRate(Math.min(3, (playerState.videoElement?.playbackRate || 1) + 0.25));
                break;
        }
    };

    container.addEventListener('keydown', handler);

    // 返回解绑函数
    return () => container.removeEventListener('keydown', handler);
}

// ==================== 格式检查工具 ====================

function isVideoFile(filename) {
    const ext = getFileExtension(filename);
    return PLAYER_CONFIG.VIDEO_FORMATS.includes(ext);
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
    bindKeyboardShortcuts
};

export default streamPlayer;
