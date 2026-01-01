/**
 * Music Player Module - 网易云音乐播放器
 * 基于 api-enhanced 和 UnblockNeteaseMusic 实现
 */

import { store } from '../store.js';
import { toast } from './toast.js';

// 导入 AMLL 样式
// AMLL 样式已移至 lazy load
// import '@applemusic-like-lyrics/core/style.css';

// 音频播放器实例
let audioPlayer = null;
const audioContext = null;
const analyser = null;

// AMLL 核心组件 (从 npm 包动态导入)
let amllPlayer = null; // AMLL 歌词播放器实例
let amllUpdateFrame = null;

// Media Session 相关变量
let mediaSessionInitialized = false;

/**
 * 确保 URL 为 HTTPS，避免 Mixed Content 错误
 */
function ensureHttps(url) {
  if (!url || typeof url !== 'string') return url;
  if (url.startsWith('http://')) {
    return url.replace('http://', 'https://');
  }
  // 如果是 // 开头的协议相对路径，也补齐 https:
  if (url.startsWith('//')) {
    return 'https:' + url;
  }
  return url;
}

/**
 * 转换 NCM 歌词为 AMLL 格式
 */
function transformToAMLL(lyrics, translations = []) {
  if (!lyrics || !lyrics.length) return [];

  // 如果是逐字歌词 (来自 yrc)
  if (lyrics[0].words && lyrics[0].words.length > 0) {
    return lyrics.map((line, index) => {
      const nextTime = lyrics[index + 1]?.startTime || line.endTime + 500;

      let trans = '';
      if (translations && translations.length) {
        const match = translations.find(t => Math.abs(t.time - line.startTime) < 500);
        if (match) trans = match.text;
      }

      return {
        startTime: line.startTime,
        endTime: line.endTime,
        words: line.words.map(w => ({
          startTime: w.startTime,
          endTime: w.endTime,
          word: w.word || '',
        })),
        translatedLyric: trans || '',
        romanLyric: '',
        isBG: false,
        isDuet: false,
      };
    });
  }

  // 普通 LRC 转换为 AMLL 格式并模拟逐字动画
  return lyrics.map((line, index) => {
    const nextTime = lyrics[index + 1]?.time || line.time + 5000;

    // 让 endTime 紧贴下一行 startTime，避免 AMLL 识别出间奏
    // 增加安全检测：确保 gap 至少为 10ms
    const gap = Math.max(nextTime - line.time, 10);
    // 只有间隔超过 4.5秒 才真正结束当前行，否则紧贴下一行开始，利于平滑滚动
    const duration = Math.min(gap - 100, 8000);
    // 确保 endTime 不早于 startTime
    const endTime = gap > 4500 ? line.time + 4000 : Math.max(line.time + 10, nextTime - 10);

    let trans = '';
    if (translations && translations.length) {
      const match = translations.find(t => Math.abs(t.time - line.time) < 100);
      if (match) trans = match.text;
    }

    // 智能拆分字词以模拟逐字效果
    let words = [];
    const text = line.text || '';

    // 判断是否包含中日韩字符 (CJK)
    const isCJK = /[\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\uff66-\uff9f]/.test(text);

    if (isCJK) {
      // 如果是中文/日韩文，按字符拆分
      const chars = text.split('');
      const charDuration = duration / Math.max(chars.length, 1);
      words = chars.map((char, i) => ({
        startTime: line.time + i * charDuration,
        endTime: line.time + (i + 1) * charDuration,
        word: char,
      }));
    } else {
      // 如果是英文等，按单词拆分
      const parts = text.split(/(\s+)/); // 保留空格
      const totalParts = parts.length;
      // 确保 duration 至少为 100ms，避免除零
      const safeDuration = Math.max(duration, 100);
      const partDuration = safeDuration / Math.max(totalParts, 1);
      words = parts.map((part, i) => ({
        startTime: line.time + i * partDuration,
        endTime: line.time + (i + 1) * partDuration,
        word: part,
      }));
    }

    return {
      startTime: line.time,
      endTime: endTime,
      words: words,
      translatedLyric: trans || '',
      romanLyric: '',
      isBG: false,
      isDuet: false,
    };
  });
}

/**
 * 更新 Media Session 元数据 (用于 SMTC 和系统媒体控制)
 * 支持 Windows 系统媒体控制中心显示歌曲信息
 */
function updateMediaSession(song) {
  if (!('mediaSession' in navigator)) {
    console.warn('[Music] Media Session API not supported');
    return;
  }

  if (!song) return;

  // 准备封面图片 URL
  const artworkUrl = ensureHttps(
    song.cover || 'https://p2.music.126.net/6y-UleORITEDbvrOLV0Q8A==/5639395138885805.jpg'
  );

  // 更新媒体元数据
  navigator.mediaSession.metadata = new MediaMetadata({
    title: song.name || '未知歌曲',
    artist: song.artists || '未知艺术家',
    album: song.album || '未知专辑',
    artwork: [
      { src: artworkUrl + '?param=96y96', sizes: '96x96', type: 'image/jpeg' },
      { src: artworkUrl + '?param=128y128', sizes: '128x128', type: 'image/jpeg' },
      { src: artworkUrl + '?param=192y192', sizes: '192x192', type: 'image/jpeg' },
      { src: artworkUrl + '?param=256y256', sizes: '256x256', type: 'image/jpeg' },
      { src: artworkUrl + '?param=384y384', sizes: '384x384', type: 'image/jpeg' },
      { src: artworkUrl + '?param=512y512', sizes: '512x512', type: 'image/jpeg' },
    ],
  });

  // 更新页面标题以显示在 SMTC 中
  // Windows SMTC 会从 document.title 获取应用名称
  const songTitle = song.name || '未知歌曲';
  const artistName = song.artists || '未知艺术家';
  document.title = `${songTitle} - ${artistName} | API Monitor`;

  console.log('[Music] Media Session updated:', song.name);
}

/**
 * 恢复页面默认标题
 */
function resetDocumentTitle() {
  document.title = 'API Monitor';
}

/**
 * 更新 Media Session 播放位置信息
 */
function updateMediaSessionPosition() {
  if (!('mediaSession' in navigator) || !('setPositionState' in navigator.mediaSession)) return;
  if (!audioPlayer || !audioPlayer.duration || isNaN(audioPlayer.duration)) return;

  try {
    navigator.mediaSession.setPositionState({
      duration: audioPlayer.duration,
      playbackRate: audioPlayer.playbackRate,
      position: audioPlayer.currentTime,
    });
  } catch (e) {
    // 忽略位置状态更新错误（可能在切换歌曲时发生）
  }
}

// Media Session 位置更新节流变量
let lastMediaSessionPositionUpdate = 0;

/**
 * 节流版本的 Media Session 位置更新 (每秒最多更新一次)
 */
function updateMediaSessionPositionThrottled() {
  const now = Date.now();
  if (now - lastMediaSessionPositionUpdate < 1000) return;
  lastMediaSessionPositionUpdate = now;
  updateMediaSessionPosition();
}

/**
 * 设置 Media Session 事件处理器 (媒体快捷键响应)
 * 支持系统媒体键：播放/暂停、上一曲、下一曲、快进、快退、跳转
 */
function setupMediaSessionHandlers() {
  if (!('mediaSession' in navigator)) return;
  if (mediaSessionInitialized) return;

  mediaSessionInitialized = true;

  // 播放
  navigator.mediaSession.setActionHandler('play', () => {
    console.log('[Music] Media Session: play');
    if (audioPlayer && audioPlayer.src) {
      audioPlayer.play();
    }
  });

  // 暂停
  navigator.mediaSession.setActionHandler('pause', () => {
    console.log('[Music] Media Session: pause');
    if (audioPlayer) {
      audioPlayer.pause();
    }
  });

  // 上一曲
  navigator.mediaSession.setActionHandler('previoustrack', () => {
    console.log('[Music] Media Session: previoustrack');
    if (window.vueApp && window.vueApp.playPrevious) {
      window.vueApp.playPrevious();
    }
  });

  // 下一曲
  navigator.mediaSession.setActionHandler('nexttrack', () => {
    console.log('[Music] Media Session: nexttrack');
    if (window.vueApp && window.vueApp.playNext) {
      window.vueApp.playNext();
    } else {
      playNext();
    }
  });

  // 快退 10 秒
  navigator.mediaSession.setActionHandler('seekbackward', details => {
    console.log('[Music] Media Session: seekbackward');
    if (audioPlayer) {
      const skipTime = details.seekOffset || 10;
      audioPlayer.currentTime = Math.max(0, audioPlayer.currentTime - skipTime);
      updateMediaSessionPosition();
    }
  });

  // 快进 10 秒
  navigator.mediaSession.setActionHandler('seekforward', details => {
    console.log('[Music] Media Session: seekforward');
    if (audioPlayer) {
      const skipTime = details.seekOffset || 10;
      audioPlayer.currentTime = Math.min(
        audioPlayer.duration || 0,
        audioPlayer.currentTime + skipTime
      );
      updateMediaSessionPosition();
    }
  });

  // 跳转到指定位置
  navigator.mediaSession.setActionHandler('seekto', details => {
    console.log('[Music] Media Session: seekto', details.seekTime);
    if (audioPlayer && details.seekTime !== undefined) {
      audioPlayer.currentTime = details.seekTime;
      updateMediaSessionPosition();
    }
  });

  // 停止
  navigator.mediaSession.setActionHandler('stop', () => {
    console.log('[Music] Media Session: stop');
    if (audioPlayer) {
      audioPlayer.pause();
      audioPlayer.currentTime = 0;
    }
  });

  console.log('[Music] Media Session handlers registered');
}

/**
 * 初始化音频播放器
 */
function initAudioPlayer() {
  if (audioPlayer) return audioPlayer;

  audioPlayer = new Audio();
  audioPlayer.preload = 'auto';

  // 绑定事件
  audioPlayer.addEventListener('timeupdate', handleTimeUpdate);
  audioPlayer.addEventListener('ended', handleTrackEnd);
  audioPlayer.addEventListener('error', handlePlayError);
  audioPlayer.addEventListener('loadedmetadata', handleMetadataLoaded);
  audioPlayer.addEventListener('canplay', handleCanPlay);
  audioPlayer.addEventListener('waiting', () => (store.musicBuffering = true));
  audioPlayer.addEventListener('playing', () => (store.musicBuffering = false));

  // 确保播放状态严格同步：监听原生 play/pause 事件
  audioPlayer.addEventListener('play', () => {
    store.musicPlaying = true;
    if (typeof amllPlayer !== 'undefined' && amllPlayer) amllPlayer.resume();
    startAmllUpdateLoop(); // 确保同步循环开启
    // 更新 Media Session 播放状态
    if ('mediaSession' in navigator) {
      navigator.mediaSession.playbackState = 'playing';
    }
  });
  audioPlayer.addEventListener('pause', () => {
    store.musicPlaying = false;
    if (typeof amllPlayer !== 'undefined' && amllPlayer) amllPlayer.pause();
    // 更新 Media Session 播放状态
    if ('mediaSession' in navigator) {
      navigator.mediaSession.playbackState = 'paused';
    }
  });

  // 初始化 Media Session 处理器
  setupMediaSessionHandlers();

  return audioPlayer;
}

/**
 * 播放时间更新
 */
function handleTimeUpdate() {
  if (!audioPlayer) return;
  store.musicCurrentTime = audioPlayer.currentTime;

  // 如果正在拖动进度条，不要让自动更新覆盖拖动位置
  if (!store.musicIsDragging) {
    store.musicProgress = (audioPlayer.currentTime / audioPlayer.duration) * 100 || 0;
  }

  // 更新当前歌词行 (增加提前量以抵消感官延迟)
  updateCurrentLyricLine();

  // 如果全屏且 AMLL 存在，也在这里更新一次确保对齐 (同步增加提前量)
  if (store.musicShowFullPlayer && amllPlayer) {
    amllPlayer.setCurrentTime(audioPlayer.currentTime * 1000 + 200);
  }

  // 每 5 秒保存一次播放状态到 localStorage
  savePlayStateThrottled();

  // 更新 Media Session 位置信息 (节流，每秒更新一次)
  updateMediaSessionPositionThrottled();
}

// 节流保存播放状态
let lastSaveTime = 0;
function savePlayStateThrottled() {
  const now = Date.now();
  if (now - lastSaveTime < 5000) return; // 5 秒内不重复保存
  lastSaveTime = now;
  savePlayState();
}

/**
 * 保存播放状态到 localStorage
 */
function savePlayState() {
  if (!store.musicCurrentSong || !audioPlayer) return;

  const state = {
    song: store.musicCurrentSong,
    currentTime: audioPlayer.currentTime,
    duration: audioPlayer.duration,
    playlist: store.musicPlaylist,
    currentIndex: store.musicCurrentIndex,
    volume: store.musicVolume,
    repeatMode: store.musicRepeatMode,
    shuffleEnabled: store.musicShuffleEnabled,
    savedAt: Date.now(),
  };

  try {
    localStorage.setItem('music_play_state', JSON.stringify(state));
  } catch (e) {
    console.warn('[Music] Failed to save play state:', e);
  }
}

/**
 * 歌曲播放结束
 */
function handleTrackEnd() {
  console.log('[Music] Track ended, repeat mode:', store.musicRepeatMode);

  if (!audioPlayer) {
    console.warn('[Music] audioPlayer not available');
    return;
  }

  if (store.musicRepeatMode === 'one') {
    // 单曲循环 - 重置时间并重新播放
    console.log('[Music] Single repeat: restarting song');
    audioPlayer.currentTime = 0;
    audioPlayer
      .play()
      .then(() => {
        console.log('[Music] Single repeat: playback restarted');
        store.musicPlaying = true;
      })
      .catch(err => {
        console.error('[Music] Single repeat play failed:', err);
      });
  } else if (store.musicRepeatMode === 'all' || store.musicPlaylist.length > 1) {
    // 列表循环或有下一首
    playNext();
  } else {
    // 停止播放
    store.musicPlaying = false;
    store.musicCurrentLyricText = '';
    store.musicCurrentLyricTranslation = '';
  }
}

/**
 * 播放错误处理
 */
function handlePlayError(e) {
  // 忽略加载被中断引起的错误 (src 被重置或切换)
  if (audioPlayer && (audioPlayer.src === '' || audioPlayer.src === window.location.href)) return;

  // 如果是因为找不到源触发的 abort，忽略之
  if (e && e.name === 'AbortError') return;

  console.error('[Music] Play error:', e);
  store.musicBuffering = false;

  // 只有确实有错误时输出日志
  const mediaError = audioPlayer?.error;
  if (mediaError) {
    console.warn('[Music] Media Error Code:', mediaError.code);
  }

  // 尝试使用解锁服务重新获取
  if (store.musicCurrentSong) {
    retryWithUnblock(store.musicCurrentSong.id);
  }
}

/**
 * 元数据加载完成
 */
function handleMetadataLoaded() {
  store.musicDuration = audioPlayer.duration;
}

/**
 * 可以播放
 */
function handleCanPlay() {
  store.musicBuffering = false;
}

/**
 * 使用解锁服务重试
 */
async function retryWithUnblock(songId) {
  try {
    const response = await fetch(`/api/music/song/url/unblock?id=${songId}`);
    const data = await response.json();

    // 后端返回格式: { code: 200, data: { url, source, ... } }
    const urlData = data.data || data;

    if (urlData?.url) {
      audioPlayer.src = urlData.url;
      await audioPlayer.play();
      store.musicPlaying = true;
      store.musicBuffering = false;
    } else {
      store.musicBuffering = false;
    }
  } catch (error) {
    if (error.name === 'AbortError') return;
    console.error('[Music] Unblock retry failed:', error);
    store.musicBuffering = false;
  }
}

/**
 * 更新当前歌词行
 */
function updateCurrentLyricLine() {
  if (!store.musicLyrics.length || !audioPlayer) return;

  // 动态视觉提前量：播放前 0.5s 不提前，防止首句歌词在还没响时就亮起
  const currentTime =
    audioPlayer.currentTime > 0.5
      ? audioPlayer.currentTime * 1000 + 150
      : audioPlayer.currentTime * 1000;

  // 1. 查找当前行索引
  let activeIndex = -1;
  for (let i = 0; i < store.musicLyrics.length; i++) {
    if (store.musicLyrics[i].time <= currentTime) {
      activeIndex = i;
    } else {
      break;
    }
  }

  if (activeIndex >= 0 && store.musicLyrics.length > 0) {
    const currentLine = store.musicLyrics[activeIndex];
    const nextLine = store.musicLyrics[activeIndex + 1];
    const nextTime = nextLine ? nextLine.time : currentLine.time + 5000;

    const textLen = currentLine.text?.length || 1;
    const gap = nextTime - currentLine.time;
    const estimatedDuration = Math.min(gap * 0.85, textLen * 280, 5000);

    const elapsed = currentTime - currentLine.time;

    if (elapsed >= 0 && estimatedDuration > 0) {
      // 纯音乐直接亮起，不走百分比进度
      if (currentLine.text?.includes('纯音乐') || currentLine.text?.includes('Instrumental')) {
        store.musicCurrentLyricPercent = 100;
      } else {
        store.musicCurrentLyricPercent = Math.min(
          100,
          Math.max(0, (elapsed / estimatedDuration) * 100)
        );
      }
    } else {
      store.musicCurrentLyricPercent = 0;
    }

    if (store.musicCurrentLyricIndex !== activeIndex) {
      store.musicCurrentLyricIndex = activeIndex;
      store.musicCurrentLyricText = currentLine.text || '';

      // 获取下一句歌词
      const nextIdx = activeIndex + 1;
      if (nextIdx < store.musicLyrics.length) {
        const nextL = store.musicLyrics[nextIdx];
        store.musicNextLyricText = nextL.text || '';

        if (store.musicLyricsTranslation.length > 0) {
          const nTrans = store.musicLyricsTranslation.find(
            t => Math.abs(t.time - nextL.time) < 1000
          );
          store.musicNextLyricTranslation = nTrans ? nTrans.text : '';
        } else {
          store.musicNextLyricTranslation = '';
        }
      } else {
        store.musicNextLyricText = '';
        store.musicNextLyricTranslation = '';
      }

      if (store.musicLyricsTranslation.length > 0) {
        const trans = store.musicLyricsTranslation.find(
          t => Math.abs(t.time - currentLine.time) < 1000
        );
        store.musicCurrentLyricTranslation = trans ? trans.text : '';
      } else {
        store.musicCurrentLyricTranslation = '';
      }

      // 当打开了桌面端全屏（且没加载 AMLL）或者打开了移动端歌词模式时，触发原生滚动
      if (store.mfpLyricsMode || (store.musicShowFullPlayer && !amllPlayer)) {
        requestAnimationFrame(() => scrollToCurrentLyric());
      }
    }
  }
}

/**
 * 启动 AMLL 更新循环
 */
function startAmllUpdateLoop() {
  if (amllUpdateFrame) return;

  let lastTime = performance.now();
  function step(now) {
    if (!store.musicPlaying && !store.musicShowFullPlayer) {
      amllUpdateFrame = null;
      return;
    }

    const delta = now - lastTime;
    lastTime = now;

    updateCurrentLyricLine();

    if (store.musicShowFullPlayer && amllPlayer && audioPlayer && !audioPlayer.paused) {
      // 关键修复：如果 AMLL 元素隐藏 (offsetParent 为 null/0 宽)，跳过更新，防止 NaN 错误
      const el = amllPlayer.getElement();
      if (el && el.offsetParent !== null && el.clientWidth > 0) {
        amllPlayer.update(delta);
      }
    }

    amllUpdateFrame = requestAnimationFrame(step);
  }
  amllUpdateFrame = requestAnimationFrame(step);
}

/**
 * 滚动歌词到中心位置
 */
function scrollToCurrentLyric() {
  const container =
    document.querySelector('.mfp-lyrics-container') ||
    document.querySelector('.full-lyrics-container');
  if (!container) return;

  const activeLine = container.querySelector('.lyric-line.active, .mfp-lyric-line.active');
  if (activeLine) {
    const containerHeight = container.offsetHeight;
    const lineOffset = activeLine.offsetTop;
    const lineHeight = activeLine.offsetHeight;

    // 计算目标位置：让当前行处于容器约 35% 处，视觉更舒适
    const targetScroll = lineOffset - containerHeight * 0.35 + lineHeight / 2;

    // 使用 behavior: 'smooth' 配合合理的 CSS transition 可实现丝滑滚动
    // 如果原生 behavior 依然不够丝滑，考虑改为手动步进动画，但目前先优化对齐位置
    container.scrollTo({
      top: targetScroll,
      behavior: 'smooth',
    });
  }
}

/**
 * 解析 LRC 歌词
 */
function parseLyrics(lrcText) {
  if (!lrcText) return [];

  const lines = lrcText.split('\n');
  const lyrics = [];
  const timeRegex = /\[(\d{2}):(\d{2})\.(\d{2,3})\]/g;

  for (const line of lines) {
    const matches = [...line.matchAll(timeRegex)];
    const text = line.replace(timeRegex, '').trim();

    if (!text) continue;

    for (const match of matches) {
      const minutes = parseInt(match[1]) || 0;
      const seconds = parseInt(match[2]) || 0;
      const ms = parseInt(match[3].padEnd(3, '0')) || 0;
      const time = minutes * 60 * 1000 + seconds * 1000 + ms;

      if (!isNaN(time)) {
        lyrics.push({ time, text });
      }
    }
  }

  return lyrics.sort((a, b) => a.time - b.time);
}

/**
 * 拆分歌词文本（用于逐字动画）
 */
function musicSplitLyricText(text) {
  if (!text) return [];
  // 如果是“纯音乐”字样，不拆分，直接返回全称以禁用逐字效果
  if (text.includes('纯音乐') || text.includes('Instrumental')) {
    return [text];
  }
  const isCJK = /[\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\uff66-\uff9f]/.test(text);
  if (isCJK) return text.split('');
  return text.split(/(\s+)/).filter(s => s.length > 0);
}

/**
 * 音乐模块方法
 */
export const musicMethods = {
  musicSplitLyricText,
  /**
   * 获取可见的歌单曲目
   */
  getVisiblePlaylistTracks() {
    const detail = store.musicCurrentPlaylistDetail;
    if (!detail || !detail.tracks) return [];

    const ITEM_HEIGHT = 68;
    const BUFFER = 8; // Buffer count
    const scrollTop = store.musicVirtualScrollTop;
    const containerHeight = store.musicPlaylistContainerHeight || 600;
    const totalTracks = detail.tracks.length;

    let start = Math.floor(scrollTop / ITEM_HEIGHT) - BUFFER;
    start = Math.max(0, start);
    let end = Math.floor((scrollTop + containerHeight) / ITEM_HEIGHT) + BUFFER;
    end = Math.min(totalTracks, end);

    // 存储当前的startIndex以便计算索引显示
    store.musicVirtualStartIndex = start;

    return detail.tracks.slice(start, end);
  },

  getPlaylistTotalHeight() {
    const detail = store.musicCurrentPlaylistDetail;
    if (!detail || !detail.tracks) return 0;
    return detail.tracks.length * 68;
  },

  getPlaylistTranslateY() {
    const ITEM_HEIGHT = 68;
    const BUFFER = 8;
    const scrollTop = store.musicVirtualScrollTop;
    let start = Math.floor(scrollTop / ITEM_HEIGHT) - BUFFER;
    start = Math.max(0, start);
    return start * ITEM_HEIGHT;
  },

  /**
   * 检查是否还有更多曲目
   */
  getHasMorePlaylistTracks() {
    const detail = store.musicCurrentPlaylistDetail;
    if (!detail || !detail.tracks) return false;
    return (store.musicPlaylistVisibleCount || 50) < detail.tracks.length;
  },

  /**
   * 滚动加载更多
   */
  handlePlaylistScroll(event) {
    const el = event.target;
    store.musicVirtualScrollTop = el.scrollTop;
    // Update container height if changed significantly (throttle)
    if (
      el.clientHeight > 0 &&
      Math.abs(store.musicPlaylistContainerHeight - el.clientHeight) > 10
    ) {
      store.musicPlaylistContainerHeight = el.clientHeight;
    }
  },

  /**
   * 加载更多歌曲
   */
  loadMorePlaylistTracks() {
    const detail = store.musicCurrentPlaylistDetail;
    if (!detail || !detail.tracks) return;

    const current = store.musicPlaylistVisibleCount || 50;
    const total = detail.tracks.length;

    if (current < total) {
      store.musicPlaylistVisibleCount = Math.min(current + 50, total);
    }
  },

  /**
   * 重置懒加载状态（在打开新歌单时调用）
   */
  resetPlaylistLazyLoad() {
    store.musicVirtualScrollTop = 0;
    store.musicPlaylistVisibleCount = 50; // Keep for compatibility if needed elsewhere
  },

  getVirtualStartIndex() {
    return store.musicVirtualStartIndex || 0;
  },

  /**
   * 滚动到当前播放的歌曲 (移动端全屏播放器嵌入列表)
   */
  mfpScrollToCurrentSong() {
    this.$nextTick(() => {
      // 延迟执行确保DOM完全渲染
      setTimeout(() => {
        const container = document.querySelector('.mfp-playlist-container');
        const activeItem = container?.querySelector('.mfp-playlist-item.active');
        if (container && activeItem) {
          // 使用 scrollTop 而非 scrollIntoView，避免影响父容器
          const containerRect = container.getBoundingClientRect();
          const itemRect = activeItem.getBoundingClientRect();
          const targetScroll =
            container.scrollTop +
            (itemRect.top - containerRect.top) -
            containerRect.height / 2 +
            itemRect.height / 2;
          container.scrollTo({
            top: Math.max(0, targetScroll),
            behavior: 'smooth',
          });
        }
      }, 150);
    });
  },

  /**
   * 滚动到当前播放的歌曲 (通用抽屉)
   */
  scrollToCurrentSong() {
    this.$nextTick(() => {
      const currentSongRef = this.$refs.currentPlayingSong;
      if (currentSongRef) {
        // Vue 3 中 v-for 的 ref 是数组
        const el = Array.isArray(currentSongRef) ? currentSongRef[0] : currentSongRef;
        if (el) {
          el.scrollIntoView({ behavior: 'smooth', block: 'center' });
        }
      }
    });
  },

  /**
   * 搜索音乐 (歌曲/歌单/歌手)
   * @param {boolean} loadMore - 是否加载更多
   */
  async musicSearch(loadMore = false) {
    if (!store.musicSearchKeyword.trim()) return;

    // 如果不是加载更多，重置状态
    if (!loadMore) {
      store.musicSearchResults = [];
      store.musicSearchPlaylists = [];
      store.musicSearchArtists = [];
      store.musicSearchOffset = 0;
      store.musicSearchHasMore = true;
      store.musicSearchLoading = true;
      store.musicShowSearchTab = true;
    } else {
      store.musicSearchLoadingMore = true;
    }

    // 搜索类型映射: 1=歌曲, 10=专辑, 100=歌手, 1000=歌单
    const typeMap = {
      songs: 1,
      playlists: 1000,
      artists: 100,
    };
    const searchType = typeMap[store.musicSearchType] || 1;
    const limit = 30;

    try {
      const response = await fetch(
        `/api/music/search?keywords=${encodeURIComponent(store.musicSearchKeyword)}&type=${searchType}&limit=${limit}&offset=${store.musicSearchOffset}`
      );
      const data = await response.json();

      if (store.musicSearchType === 'songs' && data.result?.songs) {
        const songs = data.result.songs.map(song => ({
          id: song.id,
          name: song.name,
          artists: song.ar?.map(a => a.name).join(' / ') || '未知艺术家',
          album: song.al?.name || '未知专辑',
          cover: ensureHttps(song.al?.picUrl || ''),
          duration: song.dt || 0,
        }));
        store.musicSearchResults = loadMore ? [...store.musicSearchResults, ...songs] : songs;
        store.musicSearchHasMore = songs.length >= limit;
      } else if (store.musicSearchType === 'playlists' && data.result?.playlists) {
        const playlists = data.result.playlists.map(pl => ({
          id: pl.id,
          name: pl.name,
          cover: ensureHttps(pl.coverImgUrl || ''),
          creator: pl.creator?.nickname || '未知',
          trackCount: pl.trackCount || 0,
          playCount: pl.playCount || 0,
        }));
        store.musicSearchPlaylists = loadMore
          ? [...store.musicSearchPlaylists, ...playlists]
          : playlists;
        store.musicSearchHasMore = playlists.length >= limit;
      } else if (store.musicSearchType === 'artists' && data.result?.artists) {
        const artists = data.result.artists.map(ar => ({
          id: ar.id,
          name: ar.name,
          cover: ensureHttps(ar.picUrl || ar.img1v1Url || ''),
          alias: ar.alias?.join(' / ') || '',
          albumCount: ar.albumSize || 0,
        }));
        store.musicSearchArtists = loadMore ? [...store.musicSearchArtists, ...artists] : artists;
        store.musicSearchHasMore = artists.length >= limit;
      } else {
        if (!loadMore) {
          store.musicSearchResults = [];
          store.musicSearchPlaylists = [];
          store.musicSearchArtists = [];
        }
        store.musicSearchHasMore = false;
      }

      store.musicSearchOffset += limit;
    } catch (error) {
      console.error('[Music] Search error:', error);
      toast.error('搜索失败');
    } finally {
      store.musicSearchLoading = false;
      store.musicSearchLoadingMore = false;
    }
  },

  /**
   * 关闭搜索标签
   */
  musicCloseSearchTab() {
    store.musicShowSearchTab = false;
    store.musicSearchKeyword = '';
    store.musicSearchResults = [];
    store.musicSearchPlaylists = [];
    store.musicSearchArtists = [];
    store.musicSearchOffset = 0;
    // 返回首页
    if (store.musicCurrentTab === 'search') {
      store.musicCurrentTab = 'home';
    }
  },

  /**
   * 切换搜索类型
   */
  musicSwitchSearchType(type) {
    if (store.musicSearchType !== type) {
      store.musicSearchType = type;
      this.musicSearch(false);
    }
  },

  /**
   * 切换全屏播放器展开/收起
   */
  musicToggleFullPlayer() {
    if (store.musicShowFullPlayer) {
      store.musicShowFullPlayer = false;
    } else {
      this.musicOpenFullPlayer();
    }
  },

  /**
   * 打开全屏播放器
   */
  async musicOpenFullPlayer() {
    store.musicShowFullPlayer = true;

    // 检查是否需要补载歌词
    if (store.musicCurrentSong && (!store.musicLyrics || store.musicLyrics.length === 0)) {
      this.musicLoadLyrics(store.musicCurrentSong.id);
    }

    // 递归查找容器的助手函数（支持桌面端和移动端）
    const findContainer = () => {
      // 移动端优先查找 mfp-lyrics-container
      const mfpContainer = document.querySelector('.mfp-lyrics-container');
      if (mfpContainer) return mfpContainer;
      // 桌面端使用 full-lyrics-container
      return document.querySelector('.full-lyrics-container');
    };

    // 延迟初始化 AMLL，确保容器已渲染
    setTimeout(async () => {
      // 仅在 PC 端或模拟器大屏模式下初始化 AMLL
      if (!amllPlayer && window.innerWidth >= 768) {
        try {
          const amllCore = await import('@applemusic-like-lyrics/core');
          const { LyricPlayer } = amllCore;
          const PlayerClass = LyricPlayer || amllCore.DomSlimLyricPlayer;
          if (!PlayerClass) throw new Error('AMLL Player class not found');

          amllPlayer = new PlayerClass();
          const el = amllPlayer.getElement();
          el.style.width = '100%';
          el.style.height = '100%';
          el.classList.add('amll-lyric-player');

          const container = findContainer();
          if (container && container.clientWidth > 0 && container.clientHeight > 0) {
            container.innerHTML = '';
            container.appendChild(el);
          } else {
            console.warn('[Music] Container hidden or too small, AMLL init skipped');
            amllPlayer = null; // Reset if we couldn't mount
            return;
          }

          await new Promise(r => setTimeout(r, 50));
          amllPlayer.setEnableScale(true);
          amllPlayer.setEnableBlur(true);
          amllPlayer.setEnableSpring(true);
          amllPlayer.setAlignPosition(0.35);
          amllPlayer.setAlignAnchor('center');
          amllPlayer.setWordFadeWidth(0.9);

          // Monkey-patch: 拦截 ResizeObserver，防止 display: none 导致的 NaN 错误
          // AMLL 内部会监听 el 的 resize，如果 width/height 为 0，可能导致 maskPosition 计算为 NaN
          try {
            const originalRO = window.ResizeObserver;
            // 找到 AMLL 设置在 el 上的 observer (虽然无法直接获取，但我们可以覆盖 el 的 dimensions getter 或者 hook)
            // 简单方案：在 update 循环中检测
          } catch (e) { }

          el.addEventListener('click', e => {
            let target = e.target;
            let lineFound = false;
            while (target && target !== el) {
              const lineObj = amllPlayer.lyricLineElementMap?.get(target);
              if (lineObj) {
                lineFound = true;
                const time = lineObj.getLine()?.startTime;
                if (time !== undefined) this.musicSeekToLyric(time);
                break;
              }
              target = target.parentElement;
            }
            // 仅当点击到歌词行时阻止冒泡，空白处允许冒泡以关闭播放器
            if (lineFound) e.stopPropagation();
          });
        } catch (err) {
          console.error('[Music] AMLL init failed:', err);
        }
      }

      // 无论是否刚初始化，都要启动更新循环
      startAmllUpdateLoop();

      // 每次打开都确保 AMLL 元素在容器中（仅限 PC 端）
      const container = findContainer();
      if (amllPlayer && container && window.innerWidth >= 768) {
        const el = amllPlayer.getElement();
        if (!container.contains(el)) {
          container.innerHTML = '';
          container.appendChild(el);
        }
      }

      // 设置歌词
      if (amllPlayer) {
        if (store.musicLyrics && store.musicLyrics.length > 0) {
          amllPlayer.setLyricLines(
            transformToAMLL(store.musicLyrics, store.musicLyricsTranslation)
          );
        }
        const offsetTime = audioPlayer ? audioPlayer.currentTime * 1000 + 300 : 0;
        amllPlayer.setCurrentTime(offsetTime);

        if (store.musicPlaying) amllPlayer.resume();
        else amllPlayer.pause();

        setTimeout(() => {
          if (amllPlayer) {
            amllPlayer.setAlignPosition(0.35);
            amllPlayer.setAlignAnchor('center');
          }
        }, 300);
      }
    }, 300);
  },

  /**
   * 移动端歌词模式准备 (不再使用 AMLL)
   */
  async mfpMountLyrics() {
    // 检查是否需要下载歌词
    if (store.musicCurrentSong && (!store.musicLyrics || store.musicLyrics.length === 0)) {
      this.musicLoadLyrics(store.musicCurrentSong.id);
    }

    // 开启同步循环即可，原生 Vue 会处理渲染
    startAmllUpdateLoop();

    // 立即执行一次滚动对齐
    setTimeout(() => scrollToCurrentLyric(), 100);
  },

  /**
   * 一键开启音乐之旅 (用于首页快捷访问)
   * 逻辑：如果未登录，引导登录；如果已登录，优先播放每日推荐，否则播放“我喜欢的音乐”
   */
  async musicQuickStart() {
    if (!store.musicUser) {
      // 尝试检查一次状态
      const loggedIn = await this.musicCheckLoginStatus();
      if (!loggedIn) {
        toast.info('请先登录网易云音乐');
        store.mainActiveTab = 'music';
        store.musicShowLoginModal = true;
        return;
      }
    }

    toast.info('正在为您准备音乐...');

    try {
      // 1. 优先尝试每日推荐
      const dailySongs = await this.musicLoadDailyRecommend();
      if (dailySongs && dailySongs.length > 0) {
        this.musicPlayDailyFromIndex(0);
        toast.success('为您播放：每日推荐歌曲');
        return;
      }

      // 2. 如果每日推荐没拿到，加载用户歌单并播放“我喜欢的音乐”
      if (store.musicMyPlaylists.length === 0) {
        await this.musicLoadUserPlaylists();
      }

      const likedPlaylist = store.musicMyPlaylists.find(
        p => p.isSpecial || p.name.includes('我喜欢的音乐')
      );
      if (likedPlaylist) {
        await this.musicLoadPlaylistDetail(likedPlaylist.id);
        // 等待一下详情加载（虽然 musicLoadPlaylistDetail 是 async 的）
        if (
          store.musicCurrentPlaylistDetail &&
          store.musicCurrentPlaylistDetail.tracks.length > 0
        ) {
          this.musicPlayPlaylist(store.musicCurrentPlaylistDetail.tracks);
          toast.success('为您播放：我喜欢的音乐');
          return;
        }
      }

      // 3. 兜底逻辑：跳转到音乐模块
      store.mainActiveTab = 'music';
      toast.info('请选择您想听的歌单');
    } catch (error) {
      console.error('[Music] Quick start error:', error);
      store.mainActiveTab = 'music';
    }
  },

  /**
   * 播放每日推荐 (随机开始)
   */
  async musicPlayDailyRecommend() {
    if (!store.musicUser) {
      const loggedIn = await this.musicCheckLoginStatus();
      if (!loggedIn) {
        toast.info('请先登录网易云音乐');
        store.mainActiveTab = 'music';
        store.musicShowLoginModal = true;
        return;
      }
    }

    try {
      toast.info('正在获取每日推荐...');
      const dailySongs = await this.musicLoadDailyRecommend();
      if (dailySongs && dailySongs.length > 0) {
        // 简单随机打乱
        const shuffled = [...dailySongs].sort(() => Math.random() - 0.5);
        // 替换当前播放列表
        store.musicPlaylist = shuffled;
        store.musicCurrentPlaylistId = 'daily-recommend';
        // 播放第一首
        this.musicPlay(shuffled[Math.floor(Math.random() * shuffled.length)]);
        toast.success('开始播放：每日推荐');
      } else {
        toast.warning('获取每日推荐失败');
      }
    } catch (error) {
      console.error('Play daily recommend failed:', error);
      toast.error('播放失败');
    }
  },

  /**
   * 播放我喜欢的音乐 (随机)
   */
  async musicPlayMyFavorites() {
    if (!store.musicUser) {
      const loggedIn = await this.musicCheckLoginStatus();
      if (!loggedIn) {
        toast.info('请先登录网易云音乐');
        store.mainActiveTab = 'music';
        store.musicShowLoginModal = true;
        return;
      }
    }

    try {
      toast.info('正在获取收藏列表...');
      if (store.musicMyPlaylists.length === 0) {
        await this.musicLoadUserPlaylists();
      }

      const likedPlaylist = store.musicMyPlaylists.find(
        p => p.isSpecial || p.name.includes('我喜欢的音乐')
      );
      if (likedPlaylist) {
        await this.musicLoadPlaylistDetail(likedPlaylist.id);
        if (
          store.musicCurrentPlaylistDetail &&
          store.musicCurrentPlaylistDetail.tracks.length > 0
        ) {
          const tracks = [...store.musicCurrentPlaylistDetail.tracks].sort(
            () => Math.random() - 0.5
          );
          this.musicPlayPlaylist(tracks);
          toast.success('开始随机播放：我喜欢的音乐');
        } else {
          toast.warning('歌单为空');
        }
      } else {
        toast.warning('未找到"我喜欢的音乐"歌单');
      }
    } catch (error) {
      console.error('Play favorites failed:', error);
      toast.error('播放失败');
    }
  },

  /**
   * 自动加载"我喜欢的音乐" (随机) 但不播放
   * 优化：先从播放状态/缓存恢复上次歌曲，实现瞬间显示
   */
  async musicAutoLoadFavorites() {
    // 如果已经有歌在列表里或者正在播放，就不再自动随机加载了
    if (store.musicCurrentSong) return;

    // 1. 优先从完整播放状态恢复（包含播放进度、音量等）
    const playState = localStorage.getItem('music_play_state');
    if (playState) {
      try {
        const state = JSON.parse(playState);
        // 检查保存时间，24 小时有效
        if (state.song && Date.now() - state.savedAt < 24 * 60 * 60 * 1000) {
          const song = { ...state.song };
          if (song.cover) song.cover = ensureHttps(song.cover);

          store.musicCurrentSong = song;
          store.musicPlaylist = (state.playlist || [song]).map(s => ({
            ...s,
            cover: ensureHttps(s.cover),
          }));
          store.musicCurrentIndex = state.currentIndex || 0;
          store.musicCurrentTime = state.currentTime || 0;
          store.musicDuration = state.duration || 0;
          store.musicProgress = state.duration ? (state.currentTime / state.duration) * 100 : 0;
          store.musicPlaying = false; // 不自动播放

          // 恢复设置
          if (state.volume !== undefined) store.musicVolume = state.volume;
          if (state.repeatMode) store.musicRepeatMode = state.repeatMode;
          if (state.shuffleEnabled !== undefined) store.musicShuffleEnabled = state.shuffleEnabled;

          console.log('[Music] Restored from play state:', song.name);

          // 延迟加载歌词
          setTimeout(() => this.musicLoadLyrics(song.id), 500);
          return;
        }
      } catch (e) {
        console.warn('[Music] Failed to restore play state:', e);
      }
    }

    // 2. 次选：从简单缓存恢复（瞬间显示）
    const cached = this._loadMusicCache();
    if (cached && cached.song) {
      const song = { ...cached.song };
      if (song.cover) song.cover = ensureHttps(song.cover);

      store.musicCurrentSong = song;
      store.musicPlaylist = (cached.playlist || [song]).map(s => ({
        ...s,
        cover: ensureHttps(s.cover),
      }));

      // 设置当前索引，确保下一首/上一首按钮正常工作
      store.musicCurrentIndex = store.musicPlaylist.findIndex(s => s.id === song.id);
      if (store.musicCurrentIndex === -1) store.musicCurrentIndex = 0;
      store.musicPlaying = false;
      console.log('[Music] Restored from cache, index:', store.musicCurrentIndex);

      // 恢复后延迟加载歌词
      setTimeout(() => this.musicLoadLyrics(song.id), 500);
      return;
    }

    // 3. 无缓存时加载"我喜欢的音乐"
    store.musicWidgetLoading = true;

    if (!store.musicUser) {
      await this.musicCheckLoginStatus();
    }

    if (!store.musicUser) {
      store.musicWidgetLoading = false;
      return;
    }

    try {
      if (store.musicMyPlaylists.length === 0) {
        await this.musicLoadUserPlaylists();
      }

      const likedPlaylist = store.musicMyPlaylists.find(
        p => p.isSpecial || p.name.includes('我喜欢的音乐')
      );
      if (likedPlaylist) {
        await this.musicLoadPlaylistDetail(likedPlaylist.id, 50);
        if (
          store.musicCurrentPlaylistDetail &&
          store.musicCurrentPlaylistDetail.tracks.length > 0
        ) {
          const tracks = [...store.musicCurrentPlaylistDetail.tracks].sort(
            () => Math.random() - 0.5
          );

          // 仅设置列表和第一首歌，不调用 play
          store.musicPlaylist = tracks;
          store.musicCurrentPlaylistId = likedPlaylist.id;
          store.musicCurrentSong = tracks[0];
          store.musicPlaying = false;

          // 保存到缓存供下次快速恢复
          this._saveMusicCache(tracks[0], tracks.slice(0, 20));

          // 关键修正：重置详情页显示状态，确保点击音乐模块时显示首页
          store.musicShowDetail = false;

          console.log('[Music] Auto loaded favorites (random) without playing');
        }
      }
    } catch (error) {
      console.warn('[Music] Auto load favorites failed:', error);
    } finally {
      store.musicWidgetLoading = false;
    }
  },

  /**
   * 保存音乐缓存
   */
  _saveMusicCache(song, playlist) {
    try {
      localStorage.setItem(
        'music_widget_cache',
        JSON.stringify({
          song,
          playlist,
          timestamp: Date.now(),
        })
      );
    } catch (e) {
      console.warn('[Music] Cache save failed:', e);
    }
  },

  /**
   * 加载音乐缓存
   */
  _loadMusicCache() {
    try {
      const cached = localStorage.getItem('music_widget_cache');
      if (cached) {
        const data = JSON.parse(cached);
        // 缓存 24 小时有效
        if (Date.now() - data.timestamp < 24 * 60 * 60 * 1000) {
          return data;
        }
      }
    } catch (e) {
      console.warn('[Music] Cache load failed:', e);
    }
    return null;
  },
  async musicPlay(song) {
    if (!song) return;

    initAudioPlayer();
    if (audioPlayer) {
      audioPlayer.pause();
      audioPlayer.src = ''; // 彻底切断旧音源，重置时间
    }

    store.musicBuffering = true;
    store.musicCurrentSong = song;
    store.musicCurrentLyricIndex = -1; // 使用 -1 强制触发下一次索引改变的动画
    store.musicCurrentLyricText = '';
    store.musicCurrentLyricTranslation = '';
    store.musicCurrentLyricPercent = 0;
    store.musicLyrics = [];
    store.musicLyricsTranslation = [];

    // 切歌时立即重置滚动条位置，防止歌词从奇怪的位置滑上来
    const container = document.querySelector('.mfp-lyrics-container');
    if (container) container.scrollTop = 0;

    // 添加到播放列表（如果不存在）
    if (!store.musicPlaylist.find(s => s.id === song.id)) {
      store.musicPlaylist.push(song);
    }
    store.musicCurrentIndex = store.musicPlaylist.findIndex(s => s.id === song.id);

    // 立即开启更新循环，确保切换瞬间就开始同步
    startAmllUpdateLoop();

    // 更新 Media Session (SMTC) 元数据
    updateMediaSession(song);

    // 并行加载歌词，提高响应速度
    this.musicLoadLyrics(song.id);

    try {
      // 获取播放地址
      console.log('[Music] Fetching URL for song:', song.id);
      const response = await fetch(`/api/music/song/url?id=${song.id}&level=exhigh`);
      const data = await response.json();

      const songData = data.data?.[0];
      console.log(
        '[Music] URL response:',
        songData?.url ? 'Got URL' : 'No URL',
        'source:',
        songData?.source || 'official'
      );

      if (songData?.url) {
        audioPlayer.src = songData.url;
        await audioPlayer.play();
        store.musicPlaying = true;
        store.musicBuffering = false;
        if (amllPlayer) amllPlayer.resume();

        // 更新封面（如果没有）
        if (!song.cover) {
          this.musicLoadSongDetail(song.id);
        }
      } else {
        // 尝试解锁
        console.log('[Music] No URL from official API, trying unblock...');
        await retryWithUnblock(song.id);
      }
    } catch (error) {
      if (error.name === 'AbortError') return;
      console.error('[Music] Play error:', error);
      store.musicBuffering = false;
    }
  },

  /**
   * 暂停/继续播放
   */
  async musicTogglePlay() {
    // 如果有恢复的歌曲状态但音频未加载，则从保存的进度开始播放
    if (store.musicCurrentSong && (!audioPlayer || !audioPlayer.src)) {
      await this.resumeFromSavedState();
      return;
    }

    if (!audioPlayer) return;

    if (store.musicPlaying) {
      audioPlayer.pause();
      store.musicPlaying = false;
      if (typeof amllPlayer !== 'undefined' && amllPlayer) amllPlayer.pause();
      if (typeof amllBgRender !== 'undefined' && amllBgRender) amllBgRender.pause();
    } else {
      // 如果开始播放时没有歌词，尝试加载一次
      if (store.musicCurrentSong && (!store.musicLyrics || store.musicLyrics.length === 0)) {
        this.musicLoadLyrics(store.musicCurrentSong.id);
      }
      audioPlayer.play();
      store.musicPlaying = true;
      if (typeof amllPlayer !== 'undefined' && amllPlayer) amllPlayer.resume();
      if (typeof amllBgRender !== 'undefined' && amllBgRender) amllBgRender.resume();
    }
  },

  /**
   * 从保存的状态恢复播放（从上次的进度继续）
   */
  async resumeFromSavedState() {
    const song = store.musicCurrentSong;
    if (!song) return;

    initAudioPlayer();
    store.musicBuffering = true;

    try {
      console.log('[Music] Resuming from saved state:', song.name);
      const response = await fetch(`/api/music/song/url?id=${song.id}&level=exhigh`);
      const data = await response.json();

      const songData = data.data?.[0];
      if (songData?.url) {
        audioPlayer.src = songData.url;

        // 等待音频加载后跳转到保存的位置
        audioPlayer.onloadedmetadata = () => {
          const savedTime = store.musicCurrentTime || 0;
          if (savedTime > 0 && savedTime < audioPlayer.duration) {
            audioPlayer.currentTime = savedTime;
            console.log('[Music] Seeking to saved position:', savedTime, 's');
          }
        };

        await audioPlayer.play();
        store.musicPlaying = true;
        store.musicBuffering = false;
        if (amllPlayer) amllPlayer.resume();
      } else {
        await retryWithUnblock(song.id);
      }
    } catch (error) {
      console.error('[Music] Resume error:', error);
      store.musicBuffering = false;
    }
  },

  /**
   * 播放上一首
   */
  playPrevious() {
    if (store.musicPlaylist.length === 0) return;

    let newIndex;
    if (store.musicShuffleEnabled) {
      newIndex = Math.floor(Math.random() * store.musicPlaylist.length);
    } else {
      newIndex = store.musicCurrentIndex - 1;
      if (newIndex < 0) newIndex = store.musicPlaylist.length - 1;
    }

    this.musicPlay(store.musicPlaylist[newIndex]);
  },

  /**
   * 播放下一首
   */
  playNext() {
    if (store.musicPlaylist.length === 0) return;

    let newIndex;
    if (store.musicShuffleEnabled) {
      newIndex = Math.floor(Math.random() * store.musicPlaylist.length);
    } else {
      newIndex = store.musicCurrentIndex + 1;
      if (newIndex >= store.musicPlaylist.length) newIndex = 0;
    }

    this.musicPlay(store.musicPlaylist[newIndex]);
  },

  /**
   * 跳转到指定时间
   */
  musicSeek(percent) {
    if (!audioPlayer || !store.musicDuration) return;
    audioPlayer.currentTime = (percent / 100) * store.musicDuration;
  },

  /**
   * 点击进度条跳转 (原始坐标)
   */
  musicSeekByClick(event) {
    if (!audioPlayer || !store.musicDuration) return;
    const bar = event.currentTarget;
    const rect = bar.getBoundingClientRect();
    const clientX = event.clientX || (event.touches ? event.touches[0].clientX : 0);
    const x = clientX - rect.left;
    const percent = Math.min(100, Math.max(0, (x / rect.width) * 100));
    this.musicSeek(percent);
  },

  /**
   * 进度条拖动开始
   */
  musicStartDrag(event) {
    if (!audioPlayer || !store.musicDuration) return;

    // 使用 currentTarget 获取触发的进度条元素，支持多个不同的进度条
    const bar = event.currentTarget || document.querySelector('.bar-progress');
    if (!bar) return;

    // 缓存 Rect 避免拖动中触发 Layout Reflow
    this._dragBarRect = bar.getBoundingClientRect();
    store.musicIsDragging = true;
    this.musicDoDrag(event);

    const onMouseMove = e => {
      if (this._dragFrame) cancelAnimationFrame(this._dragFrame);
      this._dragFrame = requestAnimationFrame(() => this.musicDoDrag(e));
    };

    const onMouseUp = () => {
      if (store.musicIsDragging) {
        if (this._dragFrame) cancelAnimationFrame(this._dragFrame);
        this.musicSeek(store.musicProgress);
        store.musicIsDragging = false;
        this._dragBarRect = null;
      }
      window.removeEventListener('mousemove', onMouseMove);
      window.removeEventListener('mouseup', onMouseUp);
      window.removeEventListener('touchmove', onMouseMove);
      window.removeEventListener('touchend', onMouseUp);
    };

    window.addEventListener('mousemove', onMouseMove, { passive: true });
    window.addEventListener('mouseup', onMouseUp);
    window.addEventListener('touchmove', onMouseMove, { passive: false });
    window.addEventListener('touchend', onMouseUp);
  },

  /**
   * 进度条拖动中
   */
  musicDoDrag(event) {
    if (!store.musicIsDragging || !this._dragBarRect) return;

    const clientX =
      event.clientX !== undefined ? event.clientX : event.touches ? event.touches[0].clientX : 0;
    const x = clientX - this._dragBarRect.left;
    const percent = Math.min(100, Math.max(0, (x / this._dragBarRect.width) * 100));

    store.musicProgress = percent;

    if (event.cancelable) event.preventDefault();
  },

  /**
   * 点击歌词跳转
   */
  musicSeekToLyric(timeMs) {
    if (!audioPlayer) return;
    audioPlayer.currentTime = timeMs / 1000;
    if (!store.musicPlaying) {
      this.musicTogglePlay();
    }
  },

  /**
   * 切换全屏模式
   */
  toggleFullScreen() {
    const elem = document.querySelector('.music-full-player');
    if (!elem) return;

    if (!document.fullscreenElement) {
      elem.requestFullscreen().catch(err => {
        console.error(`[Music] Fullscreen error: ${err.message}`);
      });
    } else {
      document.exitFullscreen();
    }
  },

  /**
   * 设置音量
   */
  musicSetVolume(volume) {
    store.musicVolume = volume;
    if (audioPlayer) {
      audioPlayer.volume = volume / 100;
    }
  },

  /**
   * 切换静音
   */
  musicToggleMute() {
    store.musicMuted = !store.musicMuted;
    if (audioPlayer) {
      audioPlayer.muted = store.musicMuted;
    }
  },

  /**
   * 切换顺序播放/随机播放 (左侧按钮)
   */
  musicToggleShuffleAndOrder() {
    store.musicShuffleEnabled = !store.musicShuffleEnabled;
    if (store.musicShuffleEnabled) {
      toast.info('随机播放已开启');
    } else {
      toast.info('顺序播放');
    }
    savePlayState(); // 强制保存状态
  },

  /**
   * 切换列表循环/单曲循环 (右侧按钮)
   */
  musicToggleRepeatModes() {
    // 强制在 all 和 one 之间切换
    store.musicRepeatMode = store.musicRepeatMode === 'one' ? 'all' : 'one';
    const names = { all: '列表循环', one: '单曲循环' };
    toast.info(names[store.musicRepeatMode]);
    savePlayState(); // 强制保存状态
  },

  /**
   * 加载歌词
   */
  async musicLoadLyrics(songId) {
    try {
      // 移除不稳定的新接口，统一使用标准接口
      const response = await fetch(`/api/music/lyric?id=${songId}`);
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

      const data = await response.json();

      const lrcText = data.lrc?.lyric || '';
      const tlyricText = data.tlyric?.lyric || ''; // 翻译歌词

      const rawLyrics = parseLyrics(lrcText);
      const rawTrans = parseLyrics(tlyricText);

      // 合并原文与翻译，供手机端原生渲染
      store.musicLyrics = rawLyrics.map(line => {
        const trans = rawTrans.find(t => Math.abs(t.time - line.time) < 1000);
        return {
          ...line,
          trans: trans ? trans.text : '',
        };
      });

      store.musicLyricsTranslation = rawTrans;
      store.musicCurrentLyricIndex = -1;

      // 加载完立即同步一次文字，确保瞬时显示
      updateCurrentLyricLine();

      // PC 端继续同步到 AMLL 播放器
      if (amllPlayer && window.innerWidth >= 768) {
        amllPlayer.setLyricLines(transformToAMLL(store.musicLyrics, store.musicLyricsTranslation));
      }
    } catch (error) {
      console.error('[Music] Load lyrics error:', error);
      store.musicLyrics = [];
    }
  },

  /**
   * 加载歌曲详情（封面等）
   */
  async musicLoadSongDetail(songId) {
    try {
      const response = await fetch(`/api/music/song/detail?ids=${songId}`);
      const data = await response.json();

      if (data.songs?.[0]) {
        const detail = data.songs[0];
        if (store.musicCurrentSong?.id === songId) {
          store.musicCurrentSong.cover = ensureHttps(detail.al?.picUrl || '');
          store.musicCurrentSong.album = detail.al?.name || '';
        }
      }
    } catch (error) {
      console.error('[Music] Load song detail error:', error);
    }
  },

  /**
   * 获取每日推荐
   */
  async musicLoadDailyRecommend() {
    if (store.musicDailyRecommend.length > 0) return store.musicDailyRecommend;
    store.musicRecommendLoading = true;
    try {
      const response = await fetch('/api/music/recommend/songs', {
        credentials: 'include',
      });
      const data = await response.json();

      if (data.data?.dailySongs && data.data.dailySongs.length > 0) {
        const songs = data.data.dailySongs.map(song => ({
          id: song.id,
          name: song.name,
          artists: song.ar?.map(a => a.name).join(' / ') || '未知艺术家',
          album: song.al?.name || '未知专辑',
          cover: ensureHttps(song.al?.picUrl || ''),
          duration: song.dt || 0,
        }));

        store.musicDailyRecommend = songs;
        console.log('[Music] Daily recommend loaded:', songs.length, 'songs');
        return songs;
      } else {
        console.warn('[Music] Daily recommend is empty or not logged in');
        return null;
      }
    } catch (error) {
      console.error('[Music] Daily recommend error:', error);
      return null;
    } finally {
      store.musicRecommendLoading = false;
    }
  },

  /**
   * 加载私人 FM
   */
  async musicLoadPrivateFM() {
    try {
      const response = await fetch('/api/music/personal/fm', {
        credentials: 'include',
      });
      const data = await response.json();

      if (data.data && data.data.length > 0) {
        const songs = data.data.map(song => ({
          id: song.id,
          name: song.name,
          artists: song.artists?.map(a => a.name).join(' / ') || '未知艺术家',
          album: song.album?.name || '未知专辑',
          cover: ensureHttps(song.album?.picUrl || ''),
          duration: song.duration || 0,
        }));

        // 直接播放 FM 歌曲
        store.musicPlaylist = songs;
        store.musicCurrentIndex = 0;
        this.musicPlay(songs[0]);
        toast.success('私人 FM 已加载');
      } else {
        toast.warning('需要登录才能使用私人 FM');
      }
    } catch (error) {
      console.error('[Music] Private FM error:', error);
      toast.error('加载私人 FM 失败');
    }
  },

  /**
   * 为首页加载每日推荐（不跳转详情页）
   */
  async musicLoadDailyRecommendForHome() {
    if (!store.musicUser) {
      toast.warning('需要登录才能获取每日推荐');
      return;
    }

    store.musicRecommendLoading = true;
    try {
      const response = await fetch('/api/music/recommend/songs', {
        credentials: 'include',
      });
      const data = await response.json();

      if (data.data?.dailySongs && data.data.dailySongs.length > 0) {
        const songs = data.data.dailySongs.map(song => ({
          id: song.id,
          name: song.name,
          artists: song.ar?.map(a => a.name).join(' / ') || '未知艺术家',
          album: song.al?.name || '未知专辑',
          cover: ensureHttps(song.al?.picUrl || ''),
          duration: song.dt || 0,
        }));

        store.musicDailyRecommend = songs;
        console.log('[Music] Daily recommend for home loaded:', songs.length, 'songs');
      } else {
        toast.warning('获取每日推荐失败，请稍后重试');
      }
    } catch (error) {
      console.error('[Music] Daily recommend for home error:', error);
      toast.error('加载每日推荐失败');
    } finally {
      store.musicRecommendLoading = false;
    }
  },

  /**
   * 获取热门歌单
   */
  async musicLoadHotPlaylists() {
    if (store.musicHotPlaylists.length > 0) return; // 已有数据不再重复加载
    store.musicPlaylistsLoading = true;
    try {
      const response = await fetch('/api/music/top/playlist?limit=20');
      const data = await response.json();

      if (data.playlists) {
        store.musicHotPlaylists = data.playlists.map(pl => ({
          id: pl.id,
          name: pl.name,
          cover: ensureHttps(pl.coverImgUrl || ''),
          coverImgUrl: ensureHttps(pl.coverImgUrl || ''), // 兼容旧字段
          playCount: pl.playCount || 0,
          creator: pl.creator?.nickname || '未知',
        }));
      }
    } catch (error) {
      console.error('[Music] Load hot playlists error:', error);
    } finally {
      store.musicPlaylistsLoading = false;
    }
  },

  /**
   * 加载并播放歌单
   */
  async musicLoadPlaylist(id, autoPlay = true) {
    await this.musicLoadPlaylistDetail(id);

    if (
      store.musicCurrentPlaylistDetail &&
      store.musicCurrentPlaylistDetail.tracks &&
      store.musicCurrentPlaylistDetail.tracks.length > 0
    ) {
      // 替换播放列表
      store.musicPlaylist = [...store.musicCurrentPlaylistDetail.tracks];

      if (autoPlay) {
        store.musicCurrentIndex = 0;
        this.musicPlay(store.musicPlaylist[0]);
      }
    } else {
      toast.warning('歌单为空或加载失败');
    }
  },

  /**
   * 换一批（随机播放推荐/热门歌单中的歌曲）
   */
  async musicSwapBatch() {
    // 1. 尝试初始化登录状态（如果尚未获取）
    if (!store.musicUser) {
      await this.musicCheckLoginStatus();
    }

    // 2. 优先使用“我喜欢的音乐”进行随机播放
    if (store.musicUser) {
      // 确保歌单列表已加载
      if (!store.musicMyPlaylists || store.musicMyPlaylists.length === 0) {
        await this.musicLoadUserPlaylists();
      }

      if (store.musicMyPlaylists && store.musicMyPlaylists.length > 0) {
        // 通常列表第一个就是“我喜欢的音乐” (specialType === 5)
        const likedPlaylist =
          store.musicMyPlaylists.find(p => p.isSpecial) || store.musicMyPlaylists[0];

        if (likedPlaylist) {
          toast.info('正在随机播放我喜欢的音乐...');
          // 确保已加载详情 (注意：这里直接调用 fetch 避免 autoPlay 干扰)
          await this.musicLoadPlaylistDetail(likedPlaylist.id);

          // 设置并打乱播放
          if (store.musicCurrentPlaylistDetail?.tracks) {
            const tracks = [...store.musicCurrentPlaylistDetail.tracks];
            // Fisher-Yates Shuffle
            for (let i = tracks.length - 1; i > 0; i--) {
              const j = Math.floor(Math.random() * (i + 1));
              [tracks[i], tracks[j]] = [tracks[j], tracks[i]];
            }
            store.musicPlaylist = tracks;
            store.musicCurrentIndex = 0;
            this.musicPlay(tracks[0]);
            toast.success('已切换至我喜欢的音乐 (随机)');
            return;
          }
        }
      }
    }

    // 3. 未登录或无红心歌单时，降级为随机推荐歌单
    if (!store.musicHotPlaylists || store.musicHotPlaylists.length === 0) {
      toast.info('正在获取推荐列表...');
      await this.musicLoadHotPlaylists();
    }

    if (store.musicHotPlaylists && store.musicHotPlaylists.length > 0) {
      const randomPlaylist =
        store.musicHotPlaylists[Math.floor(Math.random() * store.musicHotPlaylists.length)];
      toast.success(`切换至推荐歌单：${randomPlaylist.name}`);
      this.musicLoadPlaylist(randomPlaylist.id, true);
    } else {
      toast.warning('暂无推荐内容');
    }
  },

  /**
   * 获取歌手热门歌曲
   */
  async musicLoadArtistSongs(artistId, artistName) {
    store.musicPlaylistDetailLoading = true;
    store.musicCurrentPlaylistDetail = null;
    store.musicShowDetail = true;

    try {
      const response = await fetch(`/api/music/artist/songs?id=${artistId}`);
      const data = await response.json();

      if (data.songs && data.songs.length > 0) {
        const songs = data.songs.map(song => ({
          id: song.id,
          name: song.name,
          artists: song.ar?.map(a => a.name).join(' / ') || '未知艺术家',
          album: song.al?.name || '未知专辑',
          cover: ensureHttps(song.al?.picUrl || ''),
          duration: song.dt || 0,
        }));

        store.musicCurrentPlaylistDetail = {
          id: `artist-${artistId}`,
          name: artistName,
          cover:
            songs[0]?.cover ||
            'https://p2.music.126.net/6y-UleORITEDbvrOLV0Q8A==/5639395138885805.jpg',
          description: `${artistName} 的热门歌曲`,
          creator: '歌手',
          trackCount: songs.length,
          playCount: 0,
          tracks: songs,
        };
        store.musicPlaylistVisibleCount = 50;
      } else {
        store.musicShowDetail = false;
        toast.warning('未找到该歌手的歌曲');
      }
    } catch (error) {
      console.error('[Music] Artist songs error:', error);
      store.musicShowDetail = false;
      toast.error('加载歌手歌曲失败');
    } finally {
      store.musicPlaylistDetailLoading = false;
    }
  },

  /**
   * 获取歌单详情
   */
  async musicLoadPlaylistDetail(id, limit) {
    // 立即显示详情页和骨架屏，提供即时反馈
    store.musicShowDetail = true;
    store.musicPlaylistDetailLoading = true;
    // 清空当前详情，显示骨架屏
    store.musicCurrentPlaylistDetail = null;

    try {
      const url = limit
        ? `/api/music/playlist/detail?id=${id}&fetch_limit=${limit}`
        : `/api/music/playlist/detail?id=${id}`;
      const response = await fetch(url);
      const data = await response.json();

      if (data.playlist) {
        const pl = data.playlist;
        store.musicCurrentPlaylistDetail = {
          id: pl.id,
          name: pl.name,
          cover: ensureHttps(pl.coverImgUrl) || '',
          description: pl.description || '',
          creator: pl.creator?.nickname || '未知',
          trackCount: pl.trackCount || 0,
          playCount: pl.playCount || 0,
          tracks: (pl.tracks || []).map(song => ({
            id: song.id,
            name: song.name,
            artists: song.ar?.map(a => a.name).join(' / ') || '未知艺术家',
            album: song.al?.name || '未知专辑',
            cover: ensureHttps(song.al?.picUrl || ''),
            duration: song.dt || 0,
          })),
        };
        // 重置虚拟列表状态
        store.musicVirtualScrollTop = 0;
        store.musicPlaylistVisibleCount = 50;

        // 确保 Vue DOM 更新后初始化 ResizeObserver，使虚拟列表高度自适应
        this.$nextTick(() => {
          this.initPlaylistResizeObserver();
        });
      }
    } catch (error) {
      console.error('[Music] Playlist detail error:', error);
      // 加载失败时关闭详情页
      store.musicShowDetail = false;
    } finally {
      store.musicPlaylistDetailLoading = false;
    }
  },

  /**
   * 初始化歌单列表的 ResizeObserver
   */
  initPlaylistResizeObserver() {
    const el = this.$refs.musicSongList;
    if (!el) return;

    // 立即更新一次高度
    if (el.clientHeight > 0) {
      store.musicPlaylistContainerHeight = el.clientHeight;
    }

    // 防止重复创建
    if (this._playlistResizeObserver) {
      this._playlistResizeObserver.disconnect();
    }

    // 创建观察器，当容器大小变化（如窗口调整、flex布局自适应）时更新高度
    this._playlistResizeObserver = new ResizeObserver(entries => {
      for (const entry of entries) {
        if (entry.contentRect.height > 0) {
          store.musicPlaylistContainerHeight = entry.contentRect.height;
        }
      }
    });

    this._playlistResizeObserver.observe(el);
  },

  /**
   * 播放整个歌单
   */
  musicPlayPlaylist(tracks) {
    if (!tracks?.length) return;
    store.musicPlaylist = [...tracks];
    store.musicCurrentIndex = 0;
    this.musicPlay(tracks[0]);
  },

  /**
   * 播放每日推荐（从指定位置开始）
   */
  musicPlayDailyFromIndex(index) {
    if (!store.musicDailyRecommend?.length) return;
    // 将整个每日推荐列表加入播放队列
    store.musicPlaylist = [...store.musicDailyRecommend];
    store.musicCurrentIndex = index;
    this.musicPlay(store.musicDailyRecommend[index]);
  },

  /**
   * 从播放列表移除
   */
  musicRemoveFromPlaylist(index) {
    if (index < 0 || index >= store.musicPlaylist.length) return;

    // 如果移除的是当前播放的歌曲
    if (index === store.musicCurrentIndex) {
      if (store.musicPlaylist.length > 1) {
        this.playNext();
      } else {
        audioPlayer?.pause();
        store.musicPlaying = false;
        store.musicCurrentSong = null;
      }
    }

    store.musicPlaylist.splice(index, 1);

    // 更新当前索引
    if (index < store.musicCurrentIndex) {
      store.musicCurrentIndex--;
    }
  },

  /**
   * 清空播放列表
   */
  musicClearPlaylist() {
    audioPlayer?.pause();
    store.musicPlaying = false;
    store.musicCurrentSong = null;
    store.musicPlaylist = [];
    store.musicCurrentIndex = -1;
  },

  /**
   * 格式化时间
   */
  formatMusicTime(ms) {
    if (!ms || isNaN(ms)) return '0:00';
    const seconds = Math.floor(ms / 1000);
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}:${secs.toString().padStart(2, '0')}`;
  },

  /**
   * 格式化时间（秒）
   */
  formatMusicSeconds(seconds) {
    if (!seconds || isNaN(seconds)) return '0:00';
    const mins = Math.floor(seconds / 60);
    const secs = Math.floor(seconds % 60);
    return `${mins}:${secs.toString().padStart(2, '0')}`;
  },

  // ==================== 登录相关 ====================

  /**
   * 检查登录状态 (使用服务器存储的 Cookie)
   */
  async musicCheckLoginStatus() {
    try {
      const response = await fetch('/api/music/auth/status', { credentials: 'include' });
      const data = await response.json();

      if (data.loggedIn && data.user) {
        if (data.user.avatarUrl) data.user.avatarUrl = ensureHttps(data.user.avatarUrl);
        store.musicUser = data.user;
        localStorage.setItem('music_user_info', JSON.stringify(data.user));
        console.log('[Music] Logged in as:', data.user.nickname);

        // 登录成功后加载用户歌单
        this.musicLoadUserPlaylists();
        // 同时加载每日推荐显示在首页
        this.musicLoadDailyRecommendForHome();
        return true;
      } else {
        store.musicUser = null;
        localStorage.removeItem('music_user_info');
        return false;
      }
    } catch (error) {
      console.error('[Music] Check login status error:', error);
      return false;
    }
  },

  /**
   * 加载用户歌单列表
   */
  async musicLoadUserPlaylists() {
    if (!store.musicUser) return;

    try {
      const response = await fetch(`/api/music/user/playlist?uid=${store.musicUser.userId}`, {
        credentials: 'include',
      });
      const data = await response.json();

      if (data.playlist) {
        // 分类歌单：创建的 vs 收藏的
        const myPlaylists = [];
        const collectedPlaylists = [];

        data.playlist.forEach(pl => {
          const item = {
            id: pl.id,
            name: pl.name,
            cover: ensureHttps(pl.coverImgUrl || ''),
            trackCount: pl.trackCount || 0,
            playCount: pl.playCount || 0,
            creator: pl.creator?.nickname || '',
            isSpecial: pl.specialType === 5, // 我喜欢的音乐
          };

          if (pl.creator?.userId === store.musicUser.userId) {
            myPlaylists.push(item);
          } else {
            collectedPlaylists.push(item);
          }
        });

        store.musicMyPlaylists = myPlaylists;
        store.musicCollectedPlaylists = collectedPlaylists;
        console.log(
          '[Music] Loaded user playlists:',
          myPlaylists.length,
          'created,',
          collectedPlaylists.length,
          'collected'
        );
      }
    } catch (error) {
      console.error('[Music] Load user playlists error:', error);
    }
  },

  /**
   * 生成登录二维码
   */
  async musicGenerateLoginQr() {
    store.musicLoginLoading = true;
    store.musicQrExpired = false;

    try {
      // 1. 获取二维码 key
      const keyRes = await fetch('/api/music/login/qr/key');
      const keyData = await keyRes.json();

      if (!keyData.data?.unikey) {
        throw new Error('获取二维码 key 失败');
      }

      store.musicQrKey = keyData.data.unikey;

      // 2. 生成二维码图片
      const qrRes = await fetch(`/api/music/login/qr/create?key=${store.musicQrKey}&qrimg=true`);
      const qrData = await qrRes.json();

      if (qrData.data?.qrimg) {
        store.musicQrImg = qrData.data.qrimg;

        // 开始轮询检查扫码状态
        this.musicStartQrCheck();
      } else {
        throw new Error('生成二维码失败');
      }
    } catch (error) {
      console.error('[Music] Generate QR error:', error);
      toast.error('生成登录二维码失败');
    } finally {
      store.musicLoginLoading = false;
    }
  },

  /**
   * 开始检查二维码扫描状态
   */
  musicStartQrCheck() {
    if (store.musicQrChecking) return;
    store.musicQrChecking = true;

    const self = this;

    const checkInterval = setInterval(async () => {
      if (!store.musicQrKey || store.musicQrExpired) {
        clearInterval(checkInterval);
        store.musicQrChecking = false;
        return;
      }

      try {
        const response = await fetch(`/api/music/login/qr/check?key=${store.musicQrKey}`, {
          credentials: 'include',
        });
        const data = await response.json();

        // 状态码：800-二维码过期，801-等待扫码，802-待确认，803-授权成功
        switch (data.code) {
          case 800:
            // 二维码过期
            store.musicQrExpired = true;
            clearInterval(checkInterval);
            store.musicQrChecking = false;
            break;
          case 803:
            // 登录成功
            clearInterval(checkInterval);
            store.musicQrChecking = false;
            store.musicQrKey = '';
            store.musicQrImg = '';

            // 获取用户信息 (使用 self 保持 this 上下文)
            try {
              const statusRes = await fetch('/api/music/auth/status', { credentials: 'include' });
              const statusData = await statusRes.json();

              if (statusData.loggedIn && statusData.user) {
                if (statusData.user.avatarUrl) {
                  statusData.user.avatarUrl = ensureHttps(statusData.user.avatarUrl);
                }
                store.musicUser = statusData.user;
                localStorage.setItem('music_user_info', JSON.stringify(statusData.user));
                console.log('[Music] Logged in as:', statusData.user.nickname);

                // 加载用户歌单
                self.musicLoadUserPlaylists();

                // 关闭登录弹窗
                store.musicShowLoginModal = false;
              }
            } catch (e) {
              console.error('[Music] Get user info error:', e);
            }

            store.musicLoginStatusText = '登录成功！';
            toast.success('登录成功');
            break;
          case 802:
            // 待确认，继续等待
            store.musicLoginStatusText = '扫码成功，请在手机上确认登录';
            console.log('[Music] QR scanned, waiting for confirm...');
            break;
          case 801:
            // 等待扫码
            store.musicLoginStatusText = '请使用网易云音乐 App 扫码登录';
            break;
        }
      } catch (error) {
        console.error('[Music] QR check error:', error);
      }
    }, 2000);

    // 3分钟后自动停止检查
    setTimeout(() => {
      if (store.musicQrChecking) {
        store.musicQrExpired = true;
        clearInterval(checkInterval);
        store.musicQrChecking = false;
      }
    }, 180000);
  },

  /**
   * 退出登录 (清除服务器存储的 Cookie)
   */
  async musicLogout() {
    try {
      await fetch('/api/music/logout', { method: 'POST', credentials: 'include' });
    } catch (error) {
      console.warn('[Music] Logout API error:', error);
    }

    // 清除前端状态
    store.musicUser = null;
    localStorage.removeItem('music_user_info');
    store.musicQrKey = '';
    store.musicQrImg = '';
    store.musicQrExpired = false;
    store.musicMyPlaylists = [];
    store.musicCollectedPlaylists = [];

    toast.success('已退出登录');
  },

  /**
   * 初始化音乐模块
   */
  initMusicModule() {
    if (store.musicReady) return;
    store.musicReady = true;

    console.log('[Music] Module initialized');
    // 恢复上次的音量设置
    if (audioPlayer) {
      audioPlayer.volume = store.musicVolume / 100;
    }

    // 默认进入首页
    store.musicCurrentTab = 'home';

    // 快速恢复 localStorage 中的用户信息缓存
    const cachedUser = localStorage.getItem('music_user_info');
    if (cachedUser) {
      try {
        const user = JSON.parse(cachedUser);
        if (user.avatarUrl) user.avatarUrl = ensureHttps(user.avatarUrl);
        store.musicUser = user;
      } catch (e) {
        console.warn('[Music] Failed to parse cached user info');
      }
    }

    // 恢复上次的播放状态（歌曲、进度、播放列表），但不自动播放
    this.restorePlayState();

    // 自动加载发现页/首页数据 (热门推荐)
    this.musicLoadHotPlaylists();

    // 检查后端真实登录状态并同步数据
    this.musicCheckLoginStatus().then(() => {
      if (store.musicUser) {
        // 如果已登录，加载用户歌单和每日推荐
        this.musicLoadUserPlaylists();
        this.musicLoadDailyRecommend();
      }
    });
  },

  /**
   * 恢复上次的播放状态
   */
  restorePlayState() {
    try {
      const saved = localStorage.getItem('music_play_state');
      if (!saved) return;

      const state = JSON.parse(saved);

      // 检查保存时间，超过 24 小时则不恢复
      if (Date.now() - state.savedAt > 24 * 60 * 60 * 1000) {
        localStorage.removeItem('music_play_state');
        return;
      }

      // 恢复播放列表
      if (state.playlist && state.playlist.length > 0) {
        store.musicPlaylist = state.playlist.map(s => ({
          ...s,
          cover: ensureHttps(s.cover),
        }));
        store.musicCurrentIndex = state.currentIndex || 0;
      }

      // 恢复当前歌曲信息（但不播放）
      if (state.song) {
        const song = { ...state.song };
        if (song.cover) song.cover = ensureHttps(song.cover);

        store.musicCurrentSong = song;
        store.musicCurrentTime = state.currentTime || 0;
        store.musicDuration = state.duration || 0;
        store.musicProgress = state.duration ? (state.currentTime / state.duration) * 100 : 0;

        // 加载歌词
        this.musicLoadLyrics(song.id);

        // 初始化音频但不播放
        initAudioPlayer();

        // 更新 Media Session 元数据（显示上次播放的歌曲）
        updateMediaSession(song);

        console.log(
          '[Music] Restored play state:',
          song.name,
          'at',
          Math.floor(state.currentTime),
          's'
        );
      }

      // 恢复设置
      if (state.volume !== undefined) store.musicVolume = state.volume;
      if (state.repeatMode) store.musicRepeatMode = state.repeatMode;
      if (state.shuffleEnabled !== undefined) store.musicShuffleEnabled = state.shuffleEnabled;
    } catch (e) {
      console.warn('[Music] Failed to restore play state:', e);
    }
  },

  /**
   * 切换随机播放
   */
  musicToggleShuffle() {
    store.musicShuffleEnabled = !store.musicShuffleEnabled;
    toast.info(store.musicShuffleEnabled ? '随机播放' : '顺序播放');
  },

  /**
   * 切换循环模式 (off -> all -> one)
   */
  musicToggleRepeat() {
    const modes = ['off', 'all', 'one'];
    const current = store.musicRepeatMode || 'off';
    const nextIndex = (modes.indexOf(current) + 1) % modes.length;
    store.musicRepeatMode = modes[nextIndex];

    const messages = {
      off: '不循环',
      all: '列表循环',
      one: '单曲循环',
    };
    toast.info(messages[store.musicRepeatMode]);
  },
};

// 导出便捷函数
export function playNext() {
  if (store.musicPlaylist.length === 0) return;

  let newIndex;
  if (store.musicShuffleEnabled) {
    newIndex = Math.floor(Math.random() * store.musicPlaylist.length);
  } else {
    newIndex = store.musicCurrentIndex + 1;
    if (newIndex >= store.musicPlaylist.length) newIndex = 0;
  }

  const song = store.musicPlaylist[newIndex];
  if (song && window.vueApp) {
    window.vueApp.musicPlay(song);
  }
}
