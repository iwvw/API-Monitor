/**
 * 流媒体播放器 UI 模块
 * 
 * 提供 Vue 组件方法和模板数据
 * 
 * @module stream-player-ui
 */

import { store } from '../store.js';
import { toast } from './toast.js';
import { streamPlayer } from './stream-player.js';

// ==================== 状态 ====================

// 将播放器状态添加到 store
if (!store.streamPlayer) {
    Object.assign(store, {
        streamPlayer: {
            visible: false,
            loading: false,
            playing: false,
            currentTime: 0,
            duration: 0,
            buffered: 0,
            volume: 1,
            muted: false,
            playbackRate: 1,
            fullscreen: false,
            filename: '',
            url: '',

            // 音频警告
            audioWarning: false,
            audioWarningMessage: '',

            // 不支持格式对话框
            showUnsupportedDialog: false,
            unsupportedFormat: '',
            unsupportedUrl: '',
            unsupportedFilename: '',

            // 播放器 UI 交互
            showControls: true,
            controlsTimer: null,
            hideTimer: null,
            isLongPressing: false,
            lastTapTime: 0,
            animationType: null,
            animationText: '',
            bufferedTime: 0,
            isDragging: false,
            dragTime: 0,

            // 播放速度选项
            playbackRates: [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]
        }
    });
}

// ==================== 方法 ====================

export const streamPlayerMethods = {

    /**
     * 打开视频播放器
     * @param {string} url - 视频直链
     * @param {string} filename - 文件名
     */
    async openVideoPlayer(url, filename) {
        console.log('[StreamPlayerUI] Opening video:', filename);

        store.streamPlayer.visible = true;
        store.streamPlayer.loading = true;
        store.streamPlayer.filename = filename;
        store.streamPlayer.url = url;
        store.streamPlayer.currentTime = 0;
        store.streamPlayer.duration = 0;

        // 等待 DOM 更新
        await this.$nextTick();

        const videoElement = document.getElementById('stream-player-video');
        if (!videoElement) {
            console.error('[StreamPlayerUI] Video element not found');
            store.streamPlayer.loading = false;
            return;
        }

        // 绑定视频事件
        this._bindVideoEvents(videoElement);

        // 尝试播放
        const result = await streamPlayer.play({
            url,
            filename,
            videoElement,
            onUnsupported: (ext, videoUrl, videoFilename) => {
                this._showUnsupportedDialog(ext, videoUrl, videoFilename);
            }
        });

        if (!result.success) {
            // 失败已经在回调中处理了对话框
            console.warn('[StreamPlayerUI] Play failed:', result.message);
        }

        store.streamPlayer.loading = false;
    },

    /**
     * 关闭视频播放器
     */
    closeVideoPlayer() {
        streamPlayer.destroyPlayer();
        store.streamPlayer.visible = false;
        store.streamPlayer.playing = false;
        store.streamPlayer.showUnsupportedDialog = false;

        if (store.streamPlayer.controlsTimer) {
            clearTimeout(store.streamPlayer.controlsTimer);
        }
    },

    /**
     * 绑定视频事件
     * @param {HTMLVideoElement} video - 视频元素
     */
    _bindVideoEvents(video) {
        if (!video) return;

        // 清除旧事件
        video.onplay = null;
        video.onpause = null;
        video.ontimeupdate = null;
        video.onprogress = null;
        video.onvolumechange = null;
        video.onloadedmetadata = null;
        video.onerror = null;

        // 交互手势支持
        const container = video.parentElement;
        if (container) {
            // 简单点击显示/隐藏控制栏
            container.onclick = (e) => {
                // 如果点击的是按钮或进度条，不处理
                if (e.target.closest('.stream-player-btn') || e.target.closest('.stream-player-progress')) {
                    return;
                }

                // 处理双击
                const now = Date.now();
                if (now - store.streamPlayer.lastTapTime < 300) {
                    // 双击：根据位置快进/快退/暂停
                    const rect = container.getBoundingClientRect();
                    const x = e.clientX - rect.left;
                    if (x < rect.width * 0.3) {
                        this.skipVideo(-10);
                        this._showAnimation('seek', '-10s');
                    } else if (x > rect.width * 0.7) {
                        this.skipVideo(10);
                        this._showAnimation('seek', '+10s');
                    } else {
                        this.toggleVideoPlay();
                    }
                    store.streamPlayer.lastTapTime = 0;
                    return;
                }
                store.streamPlayer.lastTapTime = now;

                // 单击：切换控制栏
                store.streamPlayer.showControls = !store.streamPlayer.showControls;
                if (store.streamPlayer.showControls) {
                    this._autoHideControls();
                }
            };

            // 长按 2x 加速
            let longPressTimer = null;
            container.onmousedown = (e) => {
                if (e.button !== 0) return;
                longPressTimer = setTimeout(() => {
                    this.setVideoPlaybackRate(2);
                    this._showAnimation('speed2x');
                    store.streamPlayer.isLongPressing = true;
                }, 500);
            };

            const stopLongPress = () => {
                if (longPressTimer) clearTimeout(longPressTimer);
                if (store.streamPlayer.isLongPressing) {
                    this.setVideoPlaybackRate(1);
                    this._hideAnimation('speed2x');
                    store.streamPlayer.isLongPressing = false;
                }
            };

            container.onmouseup = stopLongPress;
            container.onmouseleave = stopLongPress;
        }

        // 核心事件
        video.onplay = () => {
            store.streamPlayer.playing = true;
            this._autoHideControls();
        };

        video.onpause = () => {
            store.streamPlayer.playing = false;
            store.streamPlayer.showControls = true;
        };

        video.ontimeupdate = () => {
            store.streamPlayer.currentTime = video.currentTime;
            store.streamPlayer.duration = video.duration;
        };

        video.onprogress = () => {
            if (video.buffered.length > 0) {
                store.streamPlayer.buffered = video.buffered.end(video.buffered.length - 1);
            }
        };

        video.onvolumechange = () => {
            store.streamPlayer.volume = video.volume;
            store.streamPlayer.muted = video.muted;
        };

        video.onerror = (e) => {
            console.error('[StreamPlayerUI] Video error:', e);
            // 不再自动报错，由 stream-player.js 处理
        };

        // 绑定键盘
        const unbindKeys = streamPlayer.bindKeyboardShortcuts(container || document.body);

        // 销毁时解绑
        const originalClose = this.closeVideoPlayer;
        this.closeVideoPlayer = () => {
            unbindKeys();
            originalClose.call(this);
        };
    },

    /**
     * 自动隐藏控制栏
     */
    _autoHideControls() {
        if (store.streamPlayer.controlsTimer) {
            clearTimeout(store.streamPlayer.controlsTimer);
        }

        if (store.streamPlayer.playing) {
            store.streamPlayer.controlsTimer = setTimeout(() => {
                if (store.streamPlayer.playing && !store.streamPlayer.isLongPressing) {
                    store.streamPlayer.showControls = false;
                }
            }, 3000);
        }
    },

    _showAnimation(type, text) {
        store.streamPlayer.animationType = type;
        store.streamPlayer.animationText = text || '';
        if (type !== 'speed2x') {
            setTimeout(() => {
                if (store.streamPlayer.animationType === type) {
                    store.streamPlayer.animationType = null;
                }
            }, 1000);
        }
    },

    _hideAnimation(type) {
        if (store.streamPlayer.animationType === type) {
            store.streamPlayer.animationType = null;
        }
    },

    /**
     * 播放/暂停
     */
    toggleVideoPlay() {
        streamPlayer.togglePlay();
    },

    /**
     * 快进/快退
     */
    skipVideo(seconds) {
        streamPlayer.skip(seconds);
    },

    /**
     * 设置播放速度
     */
    setVideoPlaybackRate(rate) {
        streamPlayer.setPlaybackRate(rate);
        store.streamPlayer.playbackRate = rate;
    },

    /**
     * 切换静音
     */
    toggleMute() {
        const video = document.getElementById('stream-player-video');
        if (video) {
            video.muted = !video.muted;
        }
    },

    /**
     * 调整音量
     */
    handleVolumeChange(e) {
        const rect = e.currentTarget.getBoundingClientRect();
        const x = e.clientX - rect.left;
        const volume = Math.max(0, Math.min(1, x / rect.width));
        streamPlayer.setVolume(volume);
        const video = document.getElementById('stream-player-video');
        if (video) video.muted = false;
    },

    /**
     * 调整进度
     */
    handleProgressClick(e) {
        const rect = e.currentTarget.getBoundingClientRect();
        const x = e.clientX - rect.left;
        const percent = Math.max(0, Math.min(1, x / rect.width));
        if (store.streamPlayer.duration) {
            streamPlayer.seek(percent * store.streamPlayer.duration);
        }
    },

    /**
     * 切换全屏
     */
    async toggleVideoFullscreen() {
        const container = document.querySelector('.stream-player-container');
        await streamPlayer.toggleFullscreen(container);
        store.streamPlayer.fullscreen = !!document.fullscreenElement;
    },

    /**
     * 切换画中画
     */
    async toggleVideoPiP() {
        await streamPlayer.togglePictureInPicture();
    },

    /**
     * 格式化时间
     */
    formatVideoTime(seconds) {
        return streamPlayer.formatTime(seconds);
    },

    /**
     * 显示不支持格式提示
     */
    _showUnsupportedDialog(ext, url, filename) {
        store.streamPlayer.showUnsupportedDialog = true;
        store.streamPlayer.unsupportedFormat = ext.toUpperCase();
        store.streamPlayer.unsupportedUrl = url;
        store.streamPlayer.unsupportedFilename = filename;
        store.streamPlayer.loading = false;
        toast.error(`暂不支持直接播放 ${ext.toUpperCase()} 格式`);
    },

    /**
     * 关闭对话框
     */
    closeUnsupportedDialog() {
        store.streamPlayer.showUnsupportedDialog = false;
    },

    /**
     * 下载视频
     */
    downloadVideo() {
        const url = store.streamPlayer.unsupportedUrl;
        if (url) {
            window.open(url, '_blank');
        }
    },

    /**
     * 获取音量图标
     */
    getVolumeIcon() {
        if (store.streamPlayer.muted || store.streamPlayer.volume === 0) {
            return 'fa-volume-mute';
        } else if (store.streamPlayer.volume < 0.5) {
            return 'fa-volume-down';
        }
        return 'fa-volume-up';
    },

    /**
     * 获取进度百分比
     */
    getPlayedPercent() {
        if (!store.streamPlayer.duration) return 0;
        return (store.streamPlayer.currentTime / store.streamPlayer.duration) * 100;
    },

    /**
     * 获取缓冲百分比
     */
    getBufferedPercent() {
        if (!store.streamPlayer.duration) return 0;
        return (store.streamPlayer.buffered / store.streamPlayer.duration) * 100;
    }
};

export default streamPlayerMethods;
