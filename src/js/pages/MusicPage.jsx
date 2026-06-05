import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import useStore from '../store.js';
import toastManager from '../modules/toast.js';
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { SkeletonLine } from "@cloudflare/kumo/components/loader";
import { Table } from "@cloudflare/kumo/components/table";
import { Switch } from "@cloudflare/kumo/components/switch";
import { Badge } from "@cloudflare/kumo/components/badge";
import { Autocomplete } from "@cloudflare/kumo/components/autocomplete";
import {
  Play,
  Pause,
  ChevronLeft,
  SkipBack,
  SkipForward,
  Repeat,
  Repeat1,
  Shuffle,
  Volume2,
  VolumeX,
  Search,
  Music,
  Compass,
  Home,
  User,
  Heart,
  ListMusic,
  Maximize2,
  X,
  RefreshCw,
  Clock,
  ExternalLink,
  ChevronRight,
  ArrowLeft
} from '../components/Icons.jsx';

// --- Constants & Helpers ---
const ITEM_HEIGHT = 68;
const BUFFER = 8;

function ensureHttps(url) {
  if (!url || typeof url !== 'string') return url;
  if (url.startsWith('http://')) {
    return url.replace('http://', 'https://');
  }
  if (url.startsWith('//')) {
    return 'https:' + url;
  }
  return url;
}

function formatMusicTime(ms) {
  if (!ms || isNaN(ms)) return '0:00';
  const seconds = Math.floor(ms / 1000);
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${mins}:${secs.toString().padStart(2, '0')}`;
}

function formatMusicSeconds(seconds) {
  if (!seconds || isNaN(seconds)) return '0:00';
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return `${mins}:${secs.toString().padStart(2, '0')}`;
}

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

  const sortedLyrics = lyrics.sort((a, b) => a.time - b.time);
  const finalLyrics = [];
  const INTERLUDE_THRESHOLD = 10000;

  if (sortedLyrics.length > 0 && sortedLyrics[0].time > INTERLUDE_THRESHOLD) {
    finalLyrics.push({
      time: 1000,
      isInterlude: true,
      text: '',
      duration: sortedLyrics[0].time - 2000,
      isCountdown: true,
    });
  }

  for (let i = 0; i < sortedLyrics.length; i++) {
    const current = sortedLyrics[i];
    finalLyrics.push(current);
    if (i < sortedLyrics.length - 1) {
      const next = sortedLyrics[i + 1];
      const gap = next.time - current.time;
      if (gap > INTERLUDE_THRESHOLD && !current.text.includes('纯音乐') && !next.text.includes('纯音乐')) {
        const interludeStart = current.time + 4000;
        const duration = next.time - interludeStart - 1000;
        if (duration > 0) {
          finalLyrics.push({
            time: interludeStart,
            isInterlude: true,
            text: '',
            duration: duration,
            isCountdown: true,
          });
        }
      }
    }
  }
  return finalLyrics;
}

// Spring Physics for scrolling
const SPRING_TENSION = 0.01;
const SPRING_FRICTION = 0.7;

// Audio player instance (outside to persist)
let audioPlayerInstance = null;
let amllPlayer = null;
let amllUpdateFrame = null;
let preloadedNextSong = null;
let preloadedAudioUrl = null;
let preloadedAudio = null;

const MusicPage = () => {
  const {
    musicPlaylist,
    musicCurrentIndex,
    musicCurrentSong,
    musicPlaying,
    musicBuffering,
    musicCurrentTime,
    musicDuration,
    musicProgress,
    musicVolume,
    musicRepeatMode,
    musicShuffleEnabled,
    musicLyrics,
    musicLyricsTranslation,
    musicCurrentLyricIndex,
    musicCurrentLyricText,
    musicCurrentLyricTranslation,
    musicNextLyricText,
    musicNextLyricTranslation,
    musicShowFullPlayer,
    musicIsDragging,
    musicUser,
    musicShowLoginModal,
    musicCurrentTab,
    musicSearchKeyword,
    musicSearchResults,
    musicSearchPlaylists,
    musicSearchArtists,
    musicSearchType,
    musicSearchOffset,
    musicSearchHasMore,
    musicSearchLoading,
    musicSearchLoadingMore,
    musicMyPlaylists,
    musicCurrentPlaylistDetail,
    musicVirtualScrollTop,
    musicPlaylistContainerHeight,
    musicVirtualStartIndex,
    musicPlaylistVisibleCount,
    musicShowDetail,
    mfpLyricsMode,
    musicWidgetLoading,
    musicMuted
  } = useStore();

  const [hotPlaylists, setHotPlaylists] = useState([]);
  const [playlistsLoading, setPlaylistsLoading] = useState(false);
  const [dailyRecommend, setDailyRecommend] = useState([]);
  const [recommendLoading, setRecommendLoading] = useState(false);
  const [collectedPlaylists, setCollectedPlaylists] = useState([]);
  const [qrImg, setQrImg] = useState('');
  const [qrKey, setQrKey] = useState('');
  const [qrExpired, setQrExpired] = useState(false);
  const [qrChecking, setQrChecking] = useState(false);
  const [loginStatusText, setLoginStatusText] = useState('请使用网易云音乐 App 扫码登录');
  const [loginLoading, setLoginLoading] = useState(false);
  const [playlistDetailLoading, setPlaylistDetailLoading] = useState(false);
  const [musicSuggestions, setMusicSuggestions] = useState([]);

  const songListRef = useRef(null);
  const lyricScrollState = useRef({
    currentTop: 0,
    targetTop: 0,
    velocity: 0,
    isAnimating: false,
    lastScrollTime: 0
  });

  const fetchSuggestions = useCallback(async (val) => {
    if (!val || !val.trim()) {
      setMusicSuggestions([]);
      return;
    }
    try {
      const response = await fetch(`/api/music/search/suggest?keywords=${encodeURIComponent(val)}`);
      const data = await response.json();
      // 提取建议词
      setMusicSuggestions(data.result?.allMatch || []);
    } catch (e) {
      console.error('[Music] Suggest error:', e);
    }
  }, []);

  // --- Initializers ---
  const initAudioPlayer = useCallback(() => {
    if (audioPlayerInstance) return audioPlayerInstance;
    audioPlayerInstance = new Audio();
    audioPlayerInstance.preload = 'auto';

    audioPlayerInstance.addEventListener('timeupdate', handleTimeUpdate);
    audioPlayerInstance.addEventListener('ended', handleTrackEnd);
    audioPlayerInstance.addEventListener('error', handlePlayError);
    audioPlayerInstance.addEventListener('loadedmetadata', handleMetadataLoaded);
    audioPlayerInstance.addEventListener('canplay', () => useStore.setState({ musicBuffering: false }));
    audioPlayerInstance.addEventListener('waiting', () => useStore.setState({ musicBuffering: true }));
    audioPlayerInstance.addEventListener('playing', () => useStore.setState({ musicBuffering: false }));

    audioPlayerInstance.addEventListener('play', () => {
      useStore.setState({ musicPlaying: true });
      if (amllPlayer) amllPlayer.resume();
      startAmllUpdateLoop();
      if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'playing';
    });

    audioPlayerInstance.addEventListener('pause', () => {
      useStore.setState({ musicPlaying: false });
      if (amllPlayer) amllPlayer.pause();
      if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'paused';
    });

    setupMediaSessionHandlers();
    return audioPlayerInstance;
  }, []);

  useEffect(() => {
    initAudioPlayer();
    initMusicModule();
    return () => {
      if (amllUpdateFrame) cancelAnimationFrame(amllUpdateFrame);
    };
  }, [initAudioPlayer]);

  // --- Core Methods ---

  const handleTimeUpdate = () => {
    if (!audioPlayerInstance) return;
    const currentTime = audioPlayerInstance.currentTime;
    useStore.setState({ musicCurrentTime: currentTime });
    
    if (!useStore.getState().musicIsDragging) {
      const duration = audioPlayerInstance.duration || 0;
      useStore.setState({ musicProgress: duration ? (currentTime / duration) * 100 : 0 });
    }

    updateCurrentLyricLine();
    updateMediaSessionPositionThrottled();
  };

  const handleTrackEnd = () => {
    const { musicRepeatMode, musicPlaylist } = useStore.getState();
    if (musicRepeatMode === 'one') {
      audioPlayerInstance.currentTime = 0;
      audioPlayerInstance.play();
    } else if (musicRepeatMode === 'all' || musicPlaylist.length > 1) {
      playNext();
    } else {
      useStore.setState({ musicPlaying: false });
    }
  };

  const handlePlayError = (e) => {
    if (audioPlayerInstance.src === '' || audioPlayerInstance.src === window.location.href) return;
    console.error('[Music] Play error:', e);
    useStore.setState({ musicBuffering: false });
    const { musicCurrentSong } = useStore.getState();
    if (musicCurrentSong) retryWithUnblock(musicCurrentSong.id);
  };

  const handleMetadataLoaded = () => {
    useStore.setState({ musicDuration: audioPlayerInstance.duration });
  };

  const retryWithUnblock = async (songId) => {
    try {
      const response = await fetch(`/api/music/song/url/unblock?id=${songId}`);
      const data = await response.json();
      const urlData = data.data || data;
      if (urlData?.url) {
        audioPlayerInstance.src = urlData.url;
        await audioPlayerInstance.play();
        useStore.setState({ musicPlaying: true, musicBuffering: false });
      }
    } catch (error) {
      console.error('[Music] Unblock retry failed:', error);
    }
  };

  const updateCurrentLyricLine = () => {
    const { musicLyrics, musicLyricsTranslation, musicCurrentLyricIndex, musicShowFullPlayer } = useStore.getState();
    if (!musicLyrics.length || !audioPlayerInstance) return;

    const currentTime = audioPlayerInstance.currentTime > 0.5
      ? audioPlayerInstance.currentTime * 1000 + 150
      : audioPlayerInstance.currentTime * 1000;

    let activeIndex = -1;
    for (let i = 0; i < musicLyrics.length; i++) {
      if (musicLyrics[i].time <= currentTime) {
        activeIndex = i;
      } else {
        break;
      }
    }

    if (activeIndex >= 0 && musicCurrentLyricIndex !== activeIndex) {
      const currentLine = musicLyrics[activeIndex];
      const trans = musicLyricsTranslation.find(t => Math.abs(t.time - currentLine.time) < 1000);
      
      let nextLyricText = '';
      let nextLyricTranslation = '';
      if (activeIndex + 1 < musicLyrics.length) {
        const nextLine = musicLyrics[activeIndex + 1];
        nextLyricText = nextLine.text || '';
        const nTrans = musicLyricsTranslation.find(t => Math.abs(t.time - nextLine.time) < 1000);
        nextLyricTranslation = nTrans ? nTrans.text : '';
      }

      useStore.setState({
        musicCurrentLyricIndex: activeIndex,
        musicCurrentLyricText: currentLine.text || '',
        musicCurrentLyricTranslation: trans ? trans.text : '',
        musicNextLyricText: nextLyricText,
        musicNextLyricTranslation: nextLyricTranslation
      });

      if (musicShowFullPlayer && !amllPlayer) {
        setTimeout(() => scrollToCurrentLyric(), 100);
      }
    }

    if (musicShowFullPlayer && amllPlayer) {
      amllPlayer.setCurrentTime(audioPlayerInstance.currentTime * 1000 + 200);
    }
  };

  const startAmllUpdateLoop = () => {
    if (amllUpdateFrame) return;
    let lastTime = performance.now();
    const step = (now) => {
      const { musicPlaying, musicShowFullPlayer } = useStore.getState();
      if (!musicPlaying && !musicShowFullPlayer) {
        amllUpdateFrame = null;
        return;
      }
      const delta = now - lastTime;
      lastTime = now;
      updateCurrentLyricLine();
      if (musicShowFullPlayer && amllPlayer && audioPlayerInstance && !audioPlayerInstance.paused) {
        const el = amllPlayer.getElement();
        if (el && el.offsetParent !== null && el.clientWidth > 0) {
          amllPlayer.update(delta);
        }
      }
      amllUpdateFrame = requestAnimationFrame(step);
    };
    amllUpdateFrame = requestAnimationFrame(step);
  };

  const scrollToCurrentLyric = () => {
    const container = document.querySelector('.full-lyrics-container');
    if (!container) return;

    const activeLine = container.querySelector('.lyric-line.active');
    if (activeLine) {
      const containerHeight = container.offsetHeight;
      const lineOffset = activeLine.offsetTop;
      const lineHeight = activeLine.offsetHeight;
      const targetScroll = Math.max(0, lineOffset - containerHeight * 0.35 + lineHeight / 2);
      
      lyricScrollState.current.targetTop = targetScroll;
      if (!lyricScrollState.current.isAnimating) {
        lyricScrollState.current.isAnimating = true;
        lyricScrollState.current.lastScrollTime = 0;
        requestAnimationFrame(lyricSmoothScrollLoop);
      }
    }
  };

  const lyricSmoothScrollLoop = (timestamp) => {
    const container = document.querySelector('.full-lyrics-container');
    if (!container) {
      lyricScrollState.current.isAnimating = false;
      return;
    }

    if (!lyricScrollState.current.lastScrollTime) lyricScrollState.current.lastScrollTime = timestamp;
    const dt = Math.min((timestamp - lyricScrollState.current.lastScrollTime) / 16.67, 3);
    lyricScrollState.current.lastScrollTime = timestamp;

    const distance = lyricScrollState.current.targetTop - lyricScrollState.current.currentTop;
    lyricScrollState.current.velocity += distance * SPRING_TENSION * dt;
    lyricScrollState.current.velocity *= Math.pow(SPRING_FRICTION, dt);
    lyricScrollState.current.currentTop += lyricScrollState.current.velocity * dt;

    container.scrollTop = lyricScrollState.current.currentTop;

    if (Math.abs(distance) < 0.2 && Math.abs(lyricScrollState.current.velocity) < 0.05) {
      container.scrollTop = lyricScrollState.current.targetTop;
      lyricScrollState.current.currentTop = lyricScrollState.current.targetTop;
      lyricScrollState.current.velocity = 0;
      lyricScrollState.current.isAnimating = false;
      return;
    }
    requestAnimationFrame(lyricSmoothScrollLoop);
  };

  // --- API Calls & Actions ---

  const musicLoadHotPlaylists = async () => {
    setPlaylistsLoading(true);
    try {
      const response = await fetch('/api/music/top/playlist?limit=24');
      const data = await response.json();
      if (data.playlists) {
        setHotPlaylists(data.playlists.map(pl => ({
          id: pl.id,
          name: pl.name,
          cover: ensureHttps(pl.coverImgUrl),
          playCount: pl.playCount,
          creator: pl.creator?.nickname
        })));
      }
    } catch (e) {
      console.error(e);
    } finally {
      setPlaylistsLoading(false);
    }
  };

  const musicLoadDailyRecommend = async () => {
    if (!useStore.getState().musicUser) return;
    setRecommendLoading(true);
    try {
      const response = await fetch('/api/music/recommend/songs');
      const data = await response.json();
      if (data.data?.dailySongs) {
        const songs = data.data.dailySongs.map(song => ({
          id: song.id,
          name: song.name,
          artists: song.ar?.map(a => a.name).join(' / '),
          album: song.al?.name,
          cover: ensureHttps(song.al?.picUrl),
          duration: song.dt
        }));
        setDailyRecommend(songs);
      }
    } catch (e) {
      console.error(e);
    } finally {
      setRecommendLoading(false);
    }
  };

  const musicLoadUserPlaylists = async () => {
    const user = useStore.getState().musicUser;
    if (!user) return;
    try {
      const response = await fetch(`/api/music/user/playlist?uid=${user.userId}`);
      const data = await response.json();
      if (data.playlist) {
        const my = [], collected = [];
        data.playlist.forEach(pl => {
          const item = {
            id: pl.id,
            name: pl.name,
            cover: ensureHttps(pl.coverImgUrl),
            trackCount: pl.trackCount,
            isSpecial: pl.specialType === 5
          };
          if (pl.creator?.userId === user.userId) my.push(item);
          else collected.push(item);
        });
        useStore.setState({ musicMyPlaylists: my });
        setCollectedPlaylists(collected);
      }
    } catch (e) {
      console.error(e);
    }
  };

  const musicLoadPlaylistDetail = async (id) => {
    useStore.setState({ musicShowDetail: true });
    setPlaylistDetailLoading(true);
    useStore.setState({ musicCurrentPlaylistDetail: null });
    try {
      const response = await fetch(`/api/music/playlist/detail?id=${id}`);
      const data = await response.json();
      if (data.playlist) {
        const pl = data.playlist;
        const detail = {
          id: pl.id,
          name: pl.name,
          cover: ensureHttps(pl.coverImgUrl),
          description: pl.description,
          creator: pl.creator?.nickname,
          trackCount: pl.trackCount,
          tracks: (pl.tracks || []).map(s => ({
            id: s.id,
            name: s.name,
            artists: s.ar?.map(a => a.name).join(' / '),
            album: s.al?.name,
            cover: ensureHttps(s.al?.picUrl),
            duration: s.dt
          }))
        };
        useStore.setState({
          musicCurrentPlaylistDetail: detail,
          musicVirtualScrollTop: 0,
          musicPlaylistVisibleCount: 50
        });
      }
    } catch (e) {
      console.error(e);
      useStore.setState({ musicShowDetail: false });
    } finally {
      setPlaylistDetailLoading(false);
    }
  };

  const musicSearch = async (loadMore = false) => {
    const { musicSearchKeyword, musicSearchType, musicSearchOffset, musicSearchResults, musicSearchPlaylists, musicSearchArtists } = useStore.getState();
    if (!musicSearchKeyword.trim()) return;

    if (!loadMore) {
      useStore.setState({
        musicSearchResults: [],
        musicSearchPlaylists: [],
        musicSearchArtists: [],
        musicSearchOffset: 0,
        musicSearchHasMore: true,
        musicSearchLoading: true
      });
    } else {
      useStore.setState({ musicSearchLoadingMore: true });
    }

    const typeMap = { songs: 1, playlists: 1000, artists: 100 };
    const limit = 30;
    const offset = loadMore ? musicSearchOffset : 0;

    try {
      const response = await fetch(`/api/music/search?keywords=${encodeURIComponent(musicSearchKeyword)}&type=${typeMap[musicSearchType]}&limit=${limit}&offset=${offset}`);
      const data = await response.json();

      if (musicSearchType === 'songs' && data.result?.songs) {
        const songs = data.result.songs.map(s => ({
          id: s.id,
          name: s.name,
          artists: s.ar?.map(a => a.name).join(' / '),
          album: s.al?.name,
          cover: ensureHttps(s.al?.picUrl),
          duration: s.dt
        }));
        useStore.setState({
          musicSearchResults: loadMore ? [...musicSearchResults, ...songs] : songs,
          musicSearchHasMore: songs.length >= limit
        });
      } else if (musicSearchType === 'playlists' && data.result?.playlists) {
        const pls = data.result.playlists.map(pl => ({
          id: pl.id,
          name: pl.name,
          cover: ensureHttps(pl.coverImgUrl),
          creator: pl.creator?.nickname,
          trackCount: pl.trackCount
        }));
        useStore.setState({
          musicSearchPlaylists: loadMore ? [...musicSearchPlaylists, ...pls] : pls,
          musicSearchHasMore: pls.length >= limit
        });
      } else if (musicSearchType === 'artists' && data.result?.artists) {
        const ars = data.result.artists.map(ar => ({
          id: ar.id,
          name: ar.name,
          cover: ensureHttps(ar.picUrl || ar.img1v1Url),
          alias: ar.alias?.join(' / '),
          albumCount: ar.albumSize
        }));
        useStore.setState({
          musicSearchArtists: loadMore ? [...musicSearchArtists, ...ars] : ars,
          musicSearchHasMore: ars.length >= limit
        });
      }
      useStore.setState({ musicSearchOffset: offset + limit });
    } catch (e) {
      toastManager.error('搜索失败');
    } finally {
      useStore.setState({ musicSearchLoading: false, musicSearchLoadingMore: false });
    }
  };

  const musicPlay = async (song) => {
    if (!song) return;
    initAudioPlayer();
    if (audioPlayerInstance) {
      audioPlayerInstance.pause();
      audioPlayerInstance.src = '';
    }

    useStore.setState({
      musicBuffering: true,
      musicCurrentSong: song,
      musicCurrentLyricIndex: -1,
      musicCurrentLyricText: '',
      musicCurrentLyricTranslation: '',
      musicLyrics: [],
      musicLyricsTranslation: [],
      musicProgress: 0,
      musicCurrentTime: 0
    });

    const { musicPlaylist } = useStore.getState();
    if (!musicPlaylist.find(s => s.id === song.id)) {
      useStore.setState({ musicPlaylist: [...musicPlaylist, song] });
    }
    useStore.setState({ musicCurrentIndex: useStore.getState().musicPlaylist.findIndex(s => s.id === song.id) });

    updateMediaSession(song);
    musicLoadLyrics(song.id);

    try {
      const response = await fetch(`/api/music/song/url?id=${song.id}&level=exhigh`);
      const data = await response.json();
      let audioUrl = data.data?.[0]?.url;

      if (!audioUrl) {
        const unblockRes = await fetch(`/api/music/song/url/unblock?id=${song.id}`);
        const unblockData = await unblockRes.json();
        audioUrl = unblockData.data?.url || unblockData.url;
      }

      if (audioUrl) {
        audioPlayerInstance.src = audioUrl;
        await audioPlayerInstance.play();
        useStore.setState({ musicPlaying: true, musicBuffering: false });
      }
    } catch (e) {
      console.error(e);
      useStore.setState({ musicBuffering: false });
    }
  };

  const musicTogglePlay = () => {
    if (!audioPlayerInstance) return;
    if (musicPlaying) audioPlayerInstance.pause();
    else audioPlayerInstance.play();
  };

  const playNext = () => {
    const { musicPlaylist, musicCurrentIndex, musicShuffleEnabled } = useStore.getState();
    if (musicPlaylist.length === 0) return;
    let nextIndex;
    if (musicShuffleEnabled) nextIndex = Math.floor(Math.random() * musicPlaylist.length);
    else nextIndex = (musicCurrentIndex + 1) % musicPlaylist.length;
    musicPlay(musicPlaylist[nextIndex]);
  };

  const playPrevious = () => {
    const { musicPlaylist, musicCurrentIndex, musicShuffleEnabled } = useStore.getState();
    if (musicPlaylist.length === 0) return;
    let prevIndex;
    if (musicShuffleEnabled) prevIndex = Math.floor(Math.random() * musicPlaylist.length);
    else prevIndex = (musicCurrentIndex - 1 + musicPlaylist.length) % musicPlaylist.length;
    musicPlay(musicPlaylist[prevIndex]);
  };

  const musicLoadLyrics = async (songId) => {
    try {
      const response = await fetch(`/api/music/lyric?id=${songId}`);
      const data = await response.json();
      const rawLyrics = parseLyrics(data.lrc?.lyric || '');
      const rawTrans = parseLyrics(data.tlyric?.lyric || '');
      
      const merged = rawLyrics.map(line => {
        const t = rawTrans.find(trans => Math.abs(trans.time - line.time) < 1000);
        return { ...line, trans: t ? t.text : '' };
      });

      useStore.setState({
        musicLyrics: merged,
        musicLyricsTranslation: rawTrans,
        musicCurrentLyricIndex: -1
      });
    } catch (e) {
      console.error(e);
    }
  };

  const musicLogout = async () => {
    try {
      await fetch('/api/music/logout', { method: 'POST' });
    } catch (e) {}
    useStore.setState({ musicUser: null, musicMyPlaylists: [] });
    setCollectedPlaylists([]);
    localStorage.removeItem('music_user_info');
    toastManager.success('已退出登录');
  };

  const musicGenerateLoginQr = async () => {
    setLoginLoading(true);
    setQrExpired(false);
    try {
      const keyRes = await fetch('/api/music/login/qr/key');
      const keyData = await keyRes.json();
      const key = keyData.data?.unikey;
      setQrKey(key);

      const qrRes = await fetch(`/api/music/login/qr/create?key=${key}&qrimg=true`);
      const qrData = await qrRes.json();
      setQrImg(qrData.data?.qrimg);
      startQrCheck(key);
    } catch (e) {
      toastManager.error('生成二维码失败');
    } finally {
      setLoginLoading(false);
    }
  };

  const startQrCheck = (key) => {
    if (qrChecking) return;
    setQrChecking(true);
    const interval = setInterval(async () => {
      if (!key || qrExpired) {
        clearInterval(interval);
        setQrChecking(false);
        return;
      }
      try {
        const res = await fetch(`/api/music/login/qr/check?key=${key}`);
        const data = await res.json();
        if (data.code === 800) {
          setQrExpired(true);
          clearInterval(interval);
          setQrChecking(false);
        } else if (data.code === 803) {
          clearInterval(interval);
          setQrChecking(false);
          checkLoginStatus();
          useStore.setState({ musicShowLoginModal: false });
        } else if (data.code === 802) {
          setLoginStatusText('扫码成功，请在手机上确认登录');
        }
      } catch (e) {
        console.error(e);
      }
    }, 2000);
  };

  const checkLoginStatus = async () => {
    try {
      const res = await fetch('/api/music/auth/status');
      const data = await res.json();
      if (data.loggedIn && data.user) {
        if (data.user.avatarUrl) data.user.avatarUrl = ensureHttps(data.user.avatarUrl);
        useStore.setState({ musicUser: data.user });
        localStorage.setItem('music_user_info', JSON.stringify(data.user));
        musicLoadUserPlaylists();
        musicLoadDailyRecommend();
      }
    } catch (e) {}
  };

  const initMusicModule = () => {
    musicLoadHotPlaylists();
    const cached = localStorage.getItem('music_user_info');
    if (cached) {
      try {
        useStore.setState({ musicUser: JSON.parse(cached) });
        musicLoadUserPlaylists();
        musicLoadDailyRecommend();
      } catch (e) {}
    }
    checkLoginStatus();
  };

  // --- Media Session & SMTC ---
  const updateMediaSession = (song) => {
    if (!('mediaSession' in navigator)) return;
    navigator.mediaSession.metadata = new MediaMetadata({
      title: song.name,
      artist: song.artists,
      album: song.album,
      artwork: [
        { src: song.cover + '?param=512y512', sizes: '512x512', type: 'image/jpeg' }
      ]
    });
  };

  const updateMediaSessionPositionThrottled = () => {
    if (!('mediaSession' in navigator) || !audioPlayerInstance) return;
    navigator.mediaSession.setPositionState({
      duration: audioPlayerInstance.duration || 0,
      playbackRate: audioPlayerInstance.playbackRate,
      position: audioPlayerInstance.currentTime
    });
  };

  const setupMediaSessionHandlers = () => {
    if (!('mediaSession' in navigator)) return;
    navigator.mediaSession.setActionHandler('play', () => audioPlayerInstance.play());
    navigator.mediaSession.setActionHandler('pause', () => audioPlayerInstance.pause());
    navigator.mediaSession.setActionHandler('nexttrack', () => playNext());
    navigator.mediaSession.setActionHandler('previoustrack', () => playPrevious());
    navigator.mediaSession.setActionHandler('seekto', (details) => {
      audioPlayerInstance.currentTime = details.seekTime;
    });
  };

  // --- Virtual Scrolling for Playlist ---
  const visibleTracks = useMemo(() => {
    if (!musicCurrentPlaylistDetail?.tracks) return [];
    const start = Math.max(0, Math.floor(musicVirtualScrollTop / ITEM_HEIGHT) - BUFFER);
    const end = Math.min(musicCurrentPlaylistDetail.tracks.length, Math.floor((musicVirtualScrollTop + (musicPlaylistContainerHeight || 600)) / ITEM_HEIGHT) + BUFFER);
    return musicCurrentPlaylistDetail.tracks.slice(start, end).map((track, i) => ({ ...track, originalIndex: start + i }));
  }, [musicCurrentPlaylistDetail, musicVirtualScrollTop, musicPlaylistContainerHeight]);

  const handlePlaylistScroll = (e) => {
    const el = e.target;
    useStore.setState({ musicVirtualScrollTop: el.scrollTop });
    if (el.clientHeight > 0 && Math.abs(musicPlaylistContainerHeight - el.clientHeight) > 20) {
      useStore.setState({ musicPlaylistContainerHeight: el.clientHeight });
    }
  };

  // --- UI Components ---

  const renderHomeTab = () => (
    <div className="space-y-8 animate-in fade-in duration-500">
      <div className="flex items-center gap-6">
        {musicUser ? (
          <img src={musicUser.avatarUrl} className="w-16 h-16 rounded-full border-2 border-kumo-line shadow-sm" alt="avatar" />
        ) : (
          <div className="w-16 h-16 rounded-full bg-kumo-recessed flex items-center justify-center border-2 border-kumo-line shadow-sm">
            <User className="w-8 h-8 text-kumo-subtle" />
          </div>
        )}
        <div>
          <h1 className="text-2xl font-bold text-kumo-strong">{musicUser ? `你好，${musicUser.nickname}` : '欢迎来到音乐世界'}</h1>
          <p className="text-sm text-kumo-subtle mt-1">{musicUser ? '开启你的今日音乐之旅' : '登录网易云音乐，开启个性化体验'}</p>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
<div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex items-center gap-4 cursor-pointer hover:bg-kumo-recessed transition-colors" onClick={() => musicLoadDailyRecommend()}>
          <div className="w-12 h-12 bg-kumo-brand/10 rounded-lg flex items-center justify-center text-kumo-brand">
            <span className="text-lg font-bold">{new Date().getDate()}</span>
          </div>
          <div>
            <div className="font-bold text-sm">每日推荐</div>
            <div className="text-xs text-kumo-subtle">为你量身定制</div>
          </div>
        </div>
        <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex items-center gap-4 cursor-pointer hover:bg-kumo-recessed transition-colors">
          <div className="w-12 h-12 bg-kumo-warning/10 rounded-lg flex items-center justify-center text-kumo-warning">
            <RefreshCw className="w-6 h-6" />
          </div>
          <div>
            <div className="font-bold text-sm">私人 FM</div>
            <div className="text-xs text-kumo-subtle">最懂你的频道</div>
          </div>
        </div>
        <div 
          className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex items-center gap-4 cursor-pointer hover:bg-kumo-recessed transition-colors"
          onClick={() => {
            const liked = musicMyPlaylists.find(p => p.isSpecial);
            if (liked) musicLoadPlaylistDetail(liked.id);
          }}
        >
          <div className="w-12 h-12 bg-kumo-danger/10 rounded-lg flex items-center justify-center text-kumo-danger">
            <Heart className="w-6 h-6" />
          </div>
          <div>
            <div className="font-bold text-sm">我喜欢的</div>
            <div className="text-xs text-kumo-subtle">{musicMyPlaylists.find(p => p.isSpecial)?.trackCount || 0} 首歌曲</div>
          </div>
        </div>
      </div>

      <div>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-bold text-kumo-strong">每日歌曲推荐</h2>
          {musicUser && <Button variant="ghost" size="sm" onClick={musicLoadDailyRecommend} icon={RefreshCw}>刷新</Button>}
        </div>
        {!musicUser ? (
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-12 flex flex-col items-center text-center border-dashed">
            <Music className="w-12 h-12 text-kumo-subtle mb-4" />
            <p className="text-kumo-subtle text-sm mb-6">登录后即可享受个性化推荐服务</p>
            <Button variant="primary" onClick={() => useStore.setState({ musicShowLoginModal: true })} icon={User}>立即登录</Button>
          </div>
        ) : recommendLoading ? (
          <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-4">
            {[...Array(6)].map((_, i) => <SkeletonLine key={i} className="aspect-square rounded-xl" />)}
          </div>
        ) : (
          <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-4">
            {dailyRecommend.slice(0, 12).map((song) => (
              <div key={song.id} className="group cursor-pointer" onClick={() => musicPlay(song)}>
                <div className="relative aspect-square rounded-xl overflow-hidden mb-2 shadow-sm group-hover:shadow-md transition-all">
                  <img src={song.cover + '?param=300y300'} className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500" alt={song.name} />
                  <div className="absolute inset-0 bg-black/20 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center">
                    <div className="w-10 h-10 bg-white/20 backdrop-blur-md rounded-full flex items-center justify-center text-white scale-90 group-hover:scale-100 transition-transform">
                      <Play className="w-5 h-5 fill-current" />
                    </div>
                  </div>
                </div>
                <div className="text-xs font-bold text-kumo-strong truncate">{song.name}</div>
                <div className="text-[10px] text-kumo-subtle truncate">{song.artists}</div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );

  const renderDiscoverTab = () => (
    <div className="space-y-6 animate-in fade-in duration-500">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-bold text-kumo-strong">热门歌单</h2>
        <Button variant="ghost" size="sm" onClick={musicLoadHotPlaylists} icon={RefreshCw}>换一批</Button>
      </div>
      {playlistsLoading ? (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-6">
          {[...Array(12)].map((_, i) => <SkeletonLine key={i} className="aspect-square rounded-xl" />)}
        </div>
      ) : (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-6">
          {hotPlaylists.map(pl => (
            <div key={pl.id} className="group cursor-pointer" onClick={() => musicLoadPlaylistDetail(pl.id)}>
              <div className="relative aspect-square rounded-xl overflow-hidden mb-3 shadow-sm group-hover:shadow-lg transition-all border border-kumo-line/50">
                <img src={pl.cover + '?param=400y400'} className="w-full h-full object-cover group-hover:scale-110 transition-transform duration-700" alt={pl.name} />
                <div className="absolute top-2 right-2 px-2 py-0.5 bg-black/40 backdrop-blur-md rounded-full text-[10px] text-white flex items-center gap-1">
                  <Play className="w-2.5 h-2.5" /> {(pl.playCount / 10000).toFixed(0)}万
                </div>
                <div className="absolute inset-0 bg-gradient-to-t from-black/60 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center">
                  <div className="w-12 h-12 bg-kumo-brand rounded-full flex items-center justify-center text-white shadow-lg translate-y-4 group-hover:translate-y-0 transition-transform duration-300">
                    <Play className="w-6 h-6 fill-current ml-0.5" />
                  </div>
                </div>
              </div>
              <div className="text-sm font-bold text-kumo-strong line-clamp-2 leading-snug group-hover:text-kumo-brand transition-colors">{pl.name}</div>
              <div className="text-xs text-kumo-subtle mt-1">{pl.creator}</div>
            </div>
          ))}
        </div>
      )}
    </div>
  );

  const renderLibraryTab = () => (
    <div className="space-y-8 animate-in fade-in duration-500">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-bold text-kumo-strong">我的音乐库</h2>
      </div>
      {!musicUser ? (
        <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-16 flex flex-col items-center text-center border-dashed">
          <ListMusic className="w-16 h-16 text-kumo-subtle mb-6" />
          <h3 className="text-lg font-bold mb-2">同步你的网易云音乐</h3>
          <p className="text-kumo-subtle text-sm max-w-sm mb-8">登录后可同步你的个人歌单、红心歌曲及听歌排行</p>
          <Button variant="primary" size="lg" onClick={() => useStore.setState({ musicShowLoginModal: true })} icon={User}>登录网易云</Button>
        </div>
      ) : (
        <div className="space-y-10">
          <section>
            <h3 className="text-sm font-bold text-kumo-subtle uppercase tracking-wider mb-4 flex items-center gap-2">
              <span className="w-1 h-4 bg-kumo-brand rounded-full"></span> 创建的歌单
            </h3>
            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-6">
              {musicMyPlaylists.map(pl => (
                <div key={pl.id} className="group cursor-pointer" onClick={() => musicLoadPlaylistDetail(pl.id)}>
                  <div className="relative aspect-square rounded-2xl overflow-hidden mb-3 shadow-md border border-kumo-line/50">
                    <img src={pl.cover + '?param=400y400'} className="w-full h-full object-cover group-hover:scale-105 transition-transform" alt={pl.name} />
                    {pl.isSpecial && <div className="absolute inset-0 bg-kumo-danger/10 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity"><Heart className="w-12 h-12 text-kumo-danger fill-current" /></div>}
                  </div>
                  <div className="text-sm font-bold text-kumo-strong truncate">{pl.name}</div>
                  <div className="text-xs text-kumo-subtle mt-1">{pl.trackCount} 首歌曲</div>
                </div>
              ))}
            </div>
          </section>
          
          <section>
            <h3 className="text-sm font-bold text-kumo-subtle uppercase tracking-wider mb-4 flex items-center gap-2">
              <span className="w-1 h-4 bg-kumo-warning rounded-full"></span> 收藏的歌单
            </h3>
            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-6">
              {collectedPlaylists.map(pl => (
                <div key={pl.id} className="group cursor-pointer" onClick={() => musicLoadPlaylistDetail(pl.id)}>
                  <div className="relative aspect-square rounded-2xl overflow-hidden mb-3 shadow-md border border-kumo-line/50">
                    <img src={pl.cover + '?param=400y400'} className="w-full h-full object-cover group-hover:scale-105 transition-transform" alt={pl.name} />
                  </div>
                  <div className="text-sm font-bold text-kumo-strong truncate">{pl.name}</div>
                  <div className="text-xs text-kumo-subtle mt-1">{pl.trackCount} 首歌曲</div>
                </div>
              ))}
            </div>
          </section>
        </div>
      )}
    </div>
  );

  const renderSearchTab = () => (
    <div className="space-y-6 animate-in fade-in duration-500">
      <div className="flex items-center gap-4">
        <h2 className="text-xl font-bold text-kumo-strong whitespace-nowrap">搜索: {musicSearchKeyword}</h2>
        <Tabs value={musicSearchType} onValueChange={(v) => {
          useStore.setState({ musicSearchType: v });
          setTimeout(() => musicSearch(false), 0);
        }} className="w-full max-w-xs">
          <TabsList className="bg-kumo-recessed p-1 rounded-lg h-9">
            <TabsTrigger value="songs" className="text-xs">歌曲</TabsTrigger>
            <TabsTrigger value="playlists" className="text-xs">歌单</TabsTrigger>
            <TabsTrigger value="artists" className="text-xs">歌手</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {musicSearchLoading ? (
        <div className="space-y-4">
          {[...Array(8)].map((_, i) => <SkeletonLine key={i} className="h-16 rounded-xl" />)}
        </div>
      ) : musicSearchType === 'songs' ? (
        <div className="bg-kumo-base border border-kumo-line rounded-2xl overflow-hidden shadow-sm">
          <Table>
            <Table.Header className="bg-kumo-recessed/50">
              <Table.Row>
                <Table.Head className="w-12 text-center text-[10px] font-bold">#</Table.Head>
                <Table.Head className="text-[10px] font-bold uppercase tracking-wider">标题</Table.Head>
                <Table.Head className="text-[10px] font-bold uppercase tracking-wider">专辑</Table.Head>
                <Table.Head className="w-20 text-right text-[10px] font-bold uppercase tracking-wider pr-6"><Clock className="w-3.5 h-3.5 inline" /></Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {musicSearchResults.map((song, i) => (
                <Table.Row key={song.id} className="group cursor-pointer hover:bg-kumo-recessed/70 border-kumo-line/30" onClick={() => musicPlay(song)}>
                  <Table.Cell className="text-center text-xs text-kumo-subtle font-medium">
                    <span className="group-hover:hidden">{i + 1}</span>
                    <Play className="w-3.5 h-3.5 mx-auto hidden group-hover:block text-kumo-brand fill-current" />
                  </Table.Cell>
                  <Table.Cell>
                    <div className="flex items-center gap-3">
                      <img src={song.cover + '?param=100y100'} className="w-10 h-10 rounded-lg shadow-sm" alt="" />
                      <div>
                        <div className="font-bold text-sm text-kumo-strong group-hover:text-kumo-brand transition-colors">{song.name}</div>
                        <div className="text-[11px] text-kumo-subtle mt-0.5">{song.artists}</div>
                      </div>
                    </div>
                  </Table.Cell>
                  <Table.Cell className="text-xs text-kumo-subtle truncate max-w-[200px]">{song.album}</Table.Cell>
                  <Table.Cell className="text-right text-xs text-kumo-subtle pr-6 tabular-nums">{formatMusicTime(song.duration)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
          {musicSearchHasMore && (
            <div className="p-4 text-center border-t border-kumo-line">
              <Button variant="ghost" size="sm" onClick={() => musicSearch(true)} loading={musicSearchLoadingMore}>加载更多</Button>
            </div>
          )}
        </div>
      ) : musicSearchType === 'playlists' ? (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-6">
          {musicSearchPlaylists.map(pl => (
            <div key={pl.id} className="group cursor-pointer" onClick={() => musicLoadPlaylistDetail(pl.id)}>
              <div className="relative aspect-square rounded-xl overflow-hidden mb-3 border border-kumo-line shadow-sm">
                <img src={pl.cover + '?param=400y400'} className="w-full h-full object-cover group-hover:scale-105 transition-transform" alt="" />
              </div>
              <div className="text-sm font-bold text-kumo-strong truncate">{pl.name}</div>
              <div className="text-[11px] text-kumo-subtle mt-1">{pl.creator}</div>
            </div>
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-8">
          {musicSearchArtists.map(ar => (
            <div key={ar.id} className="group cursor-pointer flex flex-col items-center text-center">
              <div className="w-32 h-32 rounded-full overflow-hidden mb-4 shadow-md border-2 border-kumo-line group-hover:border-kumo-brand transition-all">
                <img src={ar.cover + '?param=300y300'} className="w-full h-full object-cover group-hover:scale-110 transition-transform duration-500" alt="" />
              </div>
              <div className="text-sm font-bold text-kumo-strong">{ar.name}</div>
              <div className="text-[11px] text-kumo-subtle mt-1">{ar.albumCount} 张专辑</div>
            </div>
          ))}
        </div>
      )}
    </div>
  );

  const renderPlaylistDetail = () => {
    if (playlistDetailLoading && !musicCurrentPlaylistDetail) {
      return (
        <div className="space-y-8 animate-in slide-in-from-bottom-4 duration-500">
          <div className="flex flex-col md:flex-row gap-8 items-center md:items-end">
            <SkeletonLine className="w-48 h-48 rounded-2xl flex-shrink-0" />
            <div className="flex-1 space-y-4 w-full">
              <SkeletonLine className="h-8 w-1/2" />
              <SkeletonLine className="h-4 w-1/4" />
              <SkeletonLine className="h-12 w-full" />
              <div className="flex gap-4">
                <SkeletonLine className="h-10 w-32 rounded-full" />
              </div>
            </div>
          </div>
          <div className="space-y-3">
            {[...Array(10)].map((_, i) => <SkeletonLine key={i} className="h-16 rounded-xl" />)}
          </div>
        </div>
      );
    }

    if (!musicCurrentPlaylistDetail) return null;

    return (
      <div className="space-y-8 animate-in slide-in-from-bottom-4 duration-500 pb-12">
        <div className="flex flex-col md:flex-row gap-8 items-center md:items-end">
          <div className="relative w-48 h-48 group flex-shrink-0 shadow-2xl rounded-2xl overflow-hidden border border-kumo-line">
            <img src={musicCurrentPlaylistDetail.cover + '?param=512y512'} className="w-full h-full object-cover" alt="" />
            <div className="absolute inset-0 bg-black/20 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center">
              <Button variant="ghost" size="icon" className="w-14 h-14 bg-white/20 backdrop-blur-md rounded-full text-white" onClick={() => musicPlayPlaylist(musicCurrentPlaylistDetail.tracks)}>
                <Play className="w-8 h-8 fill-current" />
              </Button>
            </div>
          </div>
          <div className="flex-1 space-y-4 text-center md:text-left">
            <Badge variant="outline" className="text-[10px] bg-kumo-brand/5 border-kumo-brand/20 text-kumo-brand">歌单</Badge>
            <h1 className="text-3xl font-extrabold text-kumo-strong tracking-tight">{musicCurrentPlaylistDetail.name}</h1>
            <div className="flex items-center justify-center md:justify-start gap-2 text-sm">
              <span className="font-bold text-kumo-brand">{musicCurrentPlaylistDetail.creator}</span>
              <span className="text-kumo-subtle/50">•</span>
              <span className="text-kumo-subtle font-medium">{musicCurrentPlaylistDetail.trackCount} 首歌曲</span>
            </div>
            <p className="text-xs text-kumo-subtle line-clamp-2 max-w-2xl leading-relaxed">{musicCurrentPlaylistDetail.description}</p>
            <div className="flex items-center justify-center md:justify-start gap-4 pt-2">
              <Button variant="primary" size="lg" className="rounded-full px-8 shadow-lg shadow-kumo-brand/20" onClick={() => musicPlayPlaylist(musicCurrentPlaylistDetail.tracks)} icon={Play}>播放全部</Button>
              <Button variant="outline" size="lg" className="rounded-full px-8 border-kumo-line" icon={Heart}>收藏</Button>
            </div>
          </div>
        </div>

        <div className="bg-kumo-base border border-kumo-line rounded-2xl overflow-hidden shadow-sm">
          <div className="max-h-[600px] overflow-y-auto scrollbar-thin" onScroll={handlePlaylistScroll} ref={songListRef}>
            <div style={{ height: musicCurrentPlaylistDetail.tracks.length * ITEM_HEIGHT, position: 'relative' }}>
              <div style={{ transform: `translateY(${Math.max(0, Math.floor(musicVirtualScrollTop / ITEM_HEIGHT) - BUFFER) * ITEM_HEIGHT}px)` }}>
                <Table>
                  <Table.Header className="bg-kumo-recessed/50 sticky top-0 z-10 backdrop-blur-sm">
                    <Table.Row className="hover:bg-transparent">
                      <Table.Head className="w-14 text-center text-[10px] font-bold">#</Table.Head>
                      <Table.Head className="text-[10px] font-bold uppercase tracking-wider">标题</Table.Head>
                      <Table.Head className="text-[10px] font-bold uppercase tracking-wider hidden md:table-cell">专辑</Table.Head>
                      <Table.Head className="w-20 text-right text-[10px] font-bold uppercase tracking-wider pr-8"><Clock className="w-3.5 h-3.5 inline" /></Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {visibleTracks.map((song) => {
                      const isPlaying = musicCurrentSong?.id === song.id;
                      return (
                        <Table.Row 
                          key={`${song.id}-${song.originalIndex}`} 
                          className={`group cursor-pointer hover:bg-kumo-recessed/70 border-kumo-line/30 ${isPlaying ? 'bg-kumo-brand/5' : ''}`}
                          onClick={() => musicPlay(song)}
                        >
                          <Table.Cell className="text-center text-xs font-bold w-14">
                            {isPlaying ? (
                              <div className="flex items-center justify-center gap-0.5 h-3">
                                <div className="w-1 bg-kumo-brand animate-music-bar-1 h-3 rounded-full"></div>
                                <div className="w-1 bg-kumo-brand animate-music-bar-2 h-2 rounded-full"></div>
                                <div className="w-1 bg-kumo-brand animate-music-bar-3 h-3 rounded-full"></div>
                              </div>
                            ) : (
                              <>
                                <span className="group-hover:hidden text-kumo-subtle">{song.originalIndex + 1}</span>
                                <Play className="w-3.5 h-3.5 mx-auto hidden group-hover:block text-kumo-brand fill-current" />
                              </>
                            )}
                          </Table.Cell>
                          <Table.Cell>
                            <div className="flex items-center gap-3">
                              <img src={song.cover + '?param=100y100'} className="w-10 h-10 rounded-lg shadow-sm border border-kumo-line/50" alt="" />
                              <div className="min-w-0">
                                <div className={`font-bold text-sm truncate ${isPlaying ? 'text-kumo-brand' : 'text-kumo-strong'}`}>{song.name}</div>
                                <div className="text-[11px] text-kumo-subtle mt-0.5 truncate">{song.artists}</div>
                              </div>
                            </div>
                          </Table.Cell>
                          <Table.Cell className="text-xs text-kumo-subtle truncate max-w-[200px] hidden md:table-cell">{song.album}</Table.Cell>
                          <Table.Cell className="text-right text-xs text-kumo-subtle pr-8 tabular-nums font-medium">{formatMusicTime(song.duration)}</Table.Cell>
                        </Table.Row>
                      );
                    })}
                  </Table.Body>
                </Table>
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  };

  return (
    <div className="music-page-container h-full flex flex-col gap-6 relative pb-32">
      {/* Search Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 bg-kumo-base/50 p-4 rounded-2xl border border-kumo-line/50 backdrop-blur-md sticky top-0 z-20 shadow-sm">
        <div className="flex items-center gap-2">
          {musicShowDetail && (
            <Button variant="ghost" size="icon" onClick={() => useStore.setState({ musicShowDetail: false })} className="hover:bg-kumo-recessed rounded-full">
              <ArrowLeft className="w-5 h-5" />
            </Button>
          )}
          <Tabs value={musicCurrentTab} onValueChange={(v) => {
            useStore.setState({ musicCurrentTab: v, musicShowDetail: false });
            if (v === 'discover' && hotPlaylists.length === 0) musicLoadHotPlaylists();
          }}>
            <TabsList className="bg-transparent gap-1">
              <TabsTrigger value="home" className="data-[state=active]:bg-kumo-brand/10 data-[state=active]:text-kumo-brand border-0 rounded-full px-4 h-8 text-xs font-bold gap-1.5"><Home className="w-3.5 h-3.5" /> 首页</TabsTrigger>
              <TabsTrigger value="discover" className="data-[state=active]:bg-kumo-brand/10 data-[state=active]:text-kumo-brand border-0 rounded-full px-4 h-8 text-xs font-bold gap-1.5"><Compass className="w-3.5 h-3.5" /> 发现</TabsTrigger>
              <TabsTrigger value="library" className="data-[state=active]:bg-kumo-brand/10 data-[state=active]:text-kumo-brand border-0 rounded-full px-4 h-8 text-xs font-bold gap-1.5"><Music className="w-3.5 h-3.5" /> 库</TabsTrigger>
              {musicSearchKeyword && <TabsTrigger value="search" className="data-[state=active]:bg-kumo-brand/10 data-[state=active]:text-kumo-brand border-0 rounded-full px-4 h-8 text-xs font-bold gap-1.5"><Search className="w-3.5 h-3.5" /> 搜索</TabsTrigger>}
            </TabsList>
          </Tabs>
        </div>

        <div className="flex items-center gap-3">
          <div className="relative group flex-1 md:w-64">
            <Autocomplete 
              items={musicSuggestions}
              value={musicSearchKeyword}
              onValueChange={(v) => {
                useStore.setState({ musicSearchKeyword: v });
                fetchSuggestions(v);
              }}
              filter={null}
            >
              <div className="relative">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-kumo-subtle group-focus-within:text-kumo-brand transition-colors z-10 pointer-events-none" />
                <Autocomplete.InputGroup 
                  placeholder="搜索喜欢的音乐..." 
                  className="pl-9 pr-4 h-9 bg-kumo-recessed border-transparent focus:bg-kumo-base focus:border-kumo-brand/30 rounded-full text-xs transition-all w-full"
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      useStore.setState({ musicCurrentTab: 'search', musicShowDetail: false });
                      setTimeout(() => musicSearch(false), 0);
                    }
                  }}
                />
              </div>
              <Autocomplete.Content className="z-50 bg-kumo-base border border-kumo-line rounded-xl shadow-xl mt-2 overflow-hidden min-w-[200px]">
                <Autocomplete.List>
                  {(item) => (
                    <Autocomplete.Item 
                      key={item.keyword} 
                      value={item.keyword}
                      className="px-4 py-2.5 text-xs hover:bg-kumo-recessed cursor-pointer transition-colors flex items-center gap-2.5"
                      onClick={() => {
                        useStore.setState({ 
                          musicSearchKeyword: item.keyword, 
                          musicCurrentTab: 'search', 
                          musicShowDetail: false 
                        });
                        setTimeout(() => musicSearch(false), 0);
                      }}
                    >
                      <Search className="w-3.5 h-3.5 text-kumo-subtle" />
                      <span className="text-kumo-strong font-medium">{item.keyword}</span>
                    </Autocomplete.Item>
                  )}
                </Autocomplete.List>
              </Autocomplete.Content>
            </Autocomplete>
          </div>
          {musicUser ? (
            <div className="flex items-center gap-2 px-2 py-1 bg-kumo-recessed/50 rounded-full border border-kumo-line/30 hover:border-kumo-brand/30 cursor-pointer transition-all group" onClick={() => musicLogout()}>
              <img src={musicUser.avatarUrl} className="w-7 h-7 rounded-full shadow-sm" alt="" />
              <span className="text-xs font-bold text-kumo-strong max-w-[80px] truncate hidden md:block">{musicUser.nickname}</span>
              <X className="w-3.5 h-3.5 text-kumo-subtle opacity-0 group-hover:opacity-100 transition-opacity" />
            </div>
          ) : (
            <Button variant="outline" size="sm" className="rounded-full px-4 h-9 border-kumo-line text-xs font-bold" onClick={() => useStore.setState({ musicShowLoginModal: true })} icon={User}>登录</Button>
          )}
        </div>
      </div>

      {/* Main Content */}
      <div className="flex-1 overflow-visible">
        {musicShowDetail ? renderPlaylistDetail() : (
          <>
            {musicCurrentTab === 'home' && renderHomeTab()}
            {musicCurrentTab === 'discover' && renderDiscoverTab()}
            {musicCurrentTab === 'library' && renderLibraryTab()}
            {musicCurrentTab === 'search' && renderSearchTab()}
          </>
        )}
      </div>

      {/* Login Modal */}
      <Dialog open={musicShowLoginModal} onOpenChange={(o) => useStore.setState({ musicShowLoginModal: o })}>
        <DialogContent className="max-w-xs p-0 overflow-hidden rounded-3xl border-kumo-line bg-kumo-base shadow-2xl">
          <div className="bg-gradient-to-br from-kumo-brand/10 to-transparent p-6 text-center border-b border-kumo-line/50">
            <h2 className="text-lg font-bold text-kumo-strong">网易云音乐登录</h2>
            <p className="text-[11px] text-kumo-subtle mt-1">安全扫码，极速同步</p>
          </div>
          <div className="p-8 flex flex-col items-center">
            {loginLoading ? (
              <div className="w-48 h-48 bg-kumo-recessed rounded-2xl flex items-center justify-center animate-pulse border border-kumo-line">
                <RefreshCw className="w-8 h-8 text-kumo-subtle animate-spin" />
              </div>
            ) : qrImg ? (
              <div className="relative group">
                <div className={`p-3 bg-white rounded-2xl shadow-inner border border-kumo-line/30 ${qrExpired ? 'blur-[2px]' : ''}`}>
                  <img src={qrImg} className="w-44 h-44" alt="QR Code" />
                </div>
                {qrExpired && (
                  <div className="absolute inset-0 bg-white/60 backdrop-blur-[1px] flex flex-col items-center justify-center rounded-2xl">
                    <p className="text-xs font-bold text-kumo-strong mb-2">二维码已过期</p>
                    <Button variant="primary" size="sm" onClick={musicGenerateLoginQr} className="rounded-full shadow-lg" icon={RefreshCw}>刷新二维码</Button>
                  </div>
                )}
              </div>
            ) : (
              <Button variant="primary" size="lg" onClick={musicGenerateLoginQr} className="rounded-xl w-full h-12" icon={RefreshCw}>点击获取二维码</Button>
            )}
            <div className="mt-8 text-center">
              <div className="text-xs font-bold text-kumo-strong flex items-center gap-2 justify-center">
                <div className="w-1.5 h-1.5 rounded-full bg-kumo-brand animate-pulse"></div>
                {loginStatusText}
              </div>
              <p className="text-[10px] text-kumo-subtle mt-2 leading-relaxed">请确保手机已登录网易云音乐账号</p>
            </div>
          </div>
          <div className="p-4 bg-kumo-recessed/50 text-center border-t border-kumo-line/30">
            <Button variant="ghost" size="sm" className="text-[10px] text-kumo-subtle" onClick={() => useStore.setState({ musicShowLoginModal: false })}>取消登录</Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Playback Control Bar */}
      <div className="fixed bottom-6 left-1/2 -translate-x-1/2 w-[95%] max-w-5xl z-30">
        <div className="bg-kumo-base/80 backdrop-blur-xl border border-kumo-line/50 shadow-[0_20px_50px_rgba(0,0,0,0.1)] rounded-[2rem] p-3 md:p-4 animate-in slide-in-from-bottom-8 duration-700">
          <div className="flex items-center gap-4">
            {/* Song Info */}
            <div className="flex items-center gap-3 w-1/4 min-w-0">
              <div className="relative w-11 h-11 md:w-14 md:h-14 flex-shrink-0 group cursor-pointer" onClick={() => useStore.setState({ musicShowFullPlayer: true })}>
                <img 
                  src={(musicCurrentSong?.cover || 'https://p2.music.126.net/6y-UleORITEDbvrOLV0Q8A==/5639395138885805.jpg') + '?param=200y200'} 
                  className={`w-full h-full object-cover rounded-xl shadow-md border border-kumo-line/50 transition-all duration-500 ${musicPlaying ? 'scale-105' : 'scale-100 grayscale-[0.2]'}`} 
                  alt="" 
                />
                <div className="absolute inset-0 bg-black/40 rounded-xl opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center">
                  <Maximize2 className="w-5 h-5 text-white" />
                </div>
                {musicBuffering && (
                  <div className="absolute inset-0 bg-black/20 rounded-xl flex items-center justify-center">
                    <RefreshCw className="w-6 h-6 text-white animate-spin" />
                  </div>
                )}
              </div>
              <div className="min-w-0 overflow-hidden hidden md:block">
                <div className="font-extrabold text-sm text-kumo-strong truncate tracking-tight">{musicCurrentSong?.name || '精彩音乐'}</div>
                <div className="text-[11px] text-kumo-subtle font-medium truncate mt-0.5">{musicCurrentSong?.artists || '发现好听的声音'}</div>
              </div>
            </div>

            {/* Controls */}
            <div className="flex-1 flex flex-col gap-2">
              <div className="flex items-center justify-center gap-2 md:gap-5">
                <Button variant="ghost" size="icon" className={`w-8 h-8 rounded-full hidden sm:flex ${musicShuffleEnabled ? 'text-kumo-brand' : 'text-kumo-subtle'}`} onClick={() => useStore.setState({ musicShuffleEnabled: !musicShuffleEnabled })}>
                  <Shuffle className="w-4 h-4" />
                </Button>
                <Button variant="ghost" size="icon" className="w-9 h-9 md:w-10 md:h-10 rounded-full text-kumo-strong hover:bg-kumo-recessed active:scale-90 transition-transform" onClick={playPrevious}>
                  <SkipBack className="w-5 h-5 fill-current" />
                </Button>
                <Button 
                  variant="primary" 
                  size="icon" 
                  className="w-11 h-11 md:w-12 md:h-12 rounded-full shadow-lg shadow-kumo-brand/20 active:scale-95 transition-transform"
                  onClick={musicTogglePlay}
                >
                  {musicPlaying ? <Pause className="w-5 h-5 fill-current" /> : <Play className="w-5 h-5 fill-current ml-0.5" />}
                </Button>
                <Button variant="ghost" size="icon" className="w-9 h-9 md:w-10 md:h-10 rounded-full text-kumo-strong hover:bg-kumo-recessed active:scale-90 transition-transform" onClick={playNext}>
                  <SkipForward className="w-5 h-5 fill-current" />
                </Button>
                <Button variant="ghost" size="icon" className={`w-8 h-8 rounded-full hidden sm:flex ${musicRepeatMode !== 'none' ? 'text-kumo-brand' : 'text-kumo-subtle'}`} onClick={() => {
                  const modes = ['all', 'one', 'none'];
                  const next = modes[(modes.indexOf(musicRepeatMode) + 1) % modes.length];
                  useStore.setState({ musicRepeatMode: next });
                  toastManager.info(`循环模式: ${next}`);
                }}>
                  {musicRepeatMode === 'one' ? <Repeat1 className="w-4 h-4" /> : <Repeat className="w-4 h-4" />}
                </Button>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-[10px] font-bold text-kumo-subtle tabular-nums w-8 text-right">{formatMusicSeconds(musicCurrentTime)}</span>
                <div className="flex-1 h-1.5 relative group cursor-pointer" onMouseDown={(e) => {
                  const rect = e.currentTarget.getBoundingClientRect();
                  const p = Math.max(0, Math.min(100, ((e.clientX - rect.left) / rect.width) * 100));
                  audioPlayerInstance.currentTime = (p / 100) * musicDuration;
                }}>
                  <div className="absolute inset-0 bg-kumo-recessed rounded-full overflow-hidden">
                    <div className="h-full bg-kumo-brand transition-all duration-100 ease-linear shadow-[0_0_8px_rgba(var(--kumo-brand-rgb),0.5)]" style={{ width: `${musicProgress}%` }}></div>
                  </div>
                  <div className="absolute top-1/2 -translate-y-1/2 w-3 h-3 bg-white rounded-full shadow-md scale-0 group-hover:scale-100 transition-transform border border-kumo-line" style={{ left: `calc(${musicProgress}% - 6px)` }}></div>
                </div>
                <span className="text-[10px] font-bold text-kumo-subtle tabular-nums w-8">{formatMusicSeconds(musicDuration)}</span>
              </div>
            </div>

            {/* Extra Tools */}
            <div className="w-1/4 flex items-center justify-end gap-3 min-w-0">
              <div className="items-center gap-2 hidden lg:flex group">
                <Button variant="ghost" size="icon" className="w-8 h-8 rounded-full text-kumo-subtle group-hover:text-kumo-brand transition-colors" onClick={() => {
                  const nMuted = !musicMuted;
                  useStore.setState({ musicMuted: nMuted });
                  if (audioPlayerInstance) audioPlayerInstance.muted = nMuted;
                }}>
                  {musicMuted || musicVolume === 0 ? <VolumeX className="w-4 h-4" /> : <Volume2 className="w-4 h-4" />}
                </Button>
                <div className="w-20 h-1 bg-kumo-recessed rounded-full relative cursor-pointer" onClick={(e) => {
                  const rect = e.currentTarget.getBoundingClientRect();
                  const vol = Math.max(0, Math.min(100, ((e.clientX - rect.left) / rect.width) * 100));
                  useStore.setState({ musicVolume: vol, musicMuted: false });
                  if (audioPlayerInstance) {
                    audioPlayerInstance.volume = vol / 100;
                    audioPlayerInstance.muted = false;
                  }
                }}>
                  <div className="absolute inset-y-0 left-0 bg-kumo-subtle group-hover:bg-kumo-brand rounded-full transition-all" style={{ width: `${musicMuted ? 0 : musicVolume}%` }}></div>
                </div>
              </div>
              <Button variant="ghost" size="icon" className="w-9 h-9 rounded-full text-kumo-strong hover:bg-kumo-recessed" onClick={() => useStore.setState({ musicShowFullPlayer: true })}>
                <Maximize2 className="w-5 h-5" />
              </Button>
            </div>
          </div>
        </div>
      </div>

      {/* Full Screen Player */}
      {musicShowFullPlayer && (
        <div className="fixed inset-0 z-50 bg-black animate-in fade-in zoom-in-95 duration-500 flex flex-col overflow-hidden">
          {/* Background Blur */}
          <div className="absolute inset-0 z-0">
            <div className="absolute inset-0 bg-black/60 z-10"></div>
            <img src={musicCurrentSong?.cover + '?param=800y800'} className="w-full h-full object-cover blur-[80px] scale-125 opacity-50" alt="" />
          </div>

          <header className="relative z-10 h-20 flex items-center justify-between px-10 border-b border-white/10 backdrop-blur-md">
            <div className="flex items-center gap-4">
              <Button variant="ghost" size="icon" className="text-white hover:bg-white/10 rounded-full" onClick={() => useStore.setState({ musicShowFullPlayer: false })}>
                <X className="w-6 h-6" />
              </Button>
              <div className="hidden sm:block">
                <div className="text-white font-bold text-sm tracking-wide">{musicCurrentSong?.name}</div>
                <div className="text-white/40 text-xs font-medium mt-0.5">{musicCurrentSong?.artists}</div>
              </div>
            </div>
            <div className="flex items-center gap-3">
               <Button variant="ghost" size="sm" className="text-white/60 hover:text-white hover:bg-white/10 rounded-full text-xs font-bold gap-1.5" icon={ExternalLink}>在浏览器打开</Button>
            </div>
          </header>

          <main className="relative z-10 flex-1 flex flex-col md:flex-row items-center justify-center gap-12 md:gap-24 px-10 md:px-20 py-10 overflow-hidden">
            {/* Cover Art */}
            <div className="w-64 h-64 md:w-96 md:h-96 flex-shrink-0 relative group">
              <div className={`absolute -inset-4 bg-kumo-brand/20 blur-2xl rounded-3xl opacity-0 group-hover:opacity-100 transition-opacity duration-1000 ${musicPlaying ? 'animate-pulse' : ''}`}></div>
              <img src={musicCurrentSong?.cover + '?param=800y800'} className={`w-full h-full object-cover rounded-2xl md:rounded-3xl shadow-[0_30px_60px_-12px_rgba(0,0,0,0.5)] border border-white/10 transition-transform duration-1000 ${musicPlaying ? 'scale-105' : 'scale-100'}`} alt="" />
            </div>

            {/* Lyrics Area */}
            <div className="flex-1 flex flex-col h-full max-w-2xl overflow-hidden text-center md:text-left">
              <div className="full-lyrics-container flex-1 overflow-y-auto scrollbar-none mask-fade-y py-32 space-y-12">
                {musicLyrics.length > 0 ? musicLyrics.map((line, i) => (
                  <div 
                    key={i} 
                    className={`lyric-line transition-all duration-700 cursor-pointer hover:text-white ${i === musicCurrentLyricIndex ? 'active text-white text-3xl md:text-4xl font-extrabold scale-100 opacity-100' : 'text-white/20 text-xl md:text-2xl font-bold scale-90 opacity-40 hover:opacity-60'}`}
                    onClick={() => {
                      audioPlayerInstance.currentTime = line.time / 1000;
                      if (!musicPlaying) audioPlayerInstance.play();
                    }}
                  >
                    <div className="leading-tight">{line.text}</div>
                    {line.trans && <div className="text-sm md:text-base mt-3 opacity-60 font-medium">{line.trans}</div>}
                  </div>
                )) : (
                  <div className="h-full flex flex-col items-center justify-center gap-6">
                    <Music className="w-20 h-20 text-white/10 animate-pulse" />
                    <div className="text-white/30 font-bold text-xl">纯音乐，请欣赏</div>
                  </div>
                )}
              </div>
            </div>
          </main>

          <footer className="relative z-10 h-40 px-10 md:px-20 flex flex-col justify-center gap-6 backdrop-blur-xl border-t border-white/10">
             {/* Progress Bar */}
             <div className="w-full flex items-center gap-4">
               <span className="text-[11px] font-bold text-white/40 tabular-nums w-10 text-right">{formatMusicSeconds(musicCurrentTime)}</span>
               <div className="flex-1 h-1.5 bg-white/10 rounded-full overflow-hidden relative group cursor-pointer" onMouseDown={(e) => {
                  const rect = e.currentTarget.getBoundingClientRect();
                  const p = Math.max(0, Math.min(100, ((e.clientX - rect.left) / rect.width) * 100));
                  audioPlayerInstance.currentTime = (p / 100) * musicDuration;
               }}>
                 <div className="h-full bg-white transition-all duration-100 ease-linear shadow-[0_0_15px_rgba(255,255,255,0.5)]" style={{ width: `${musicProgress}%` }}></div>
                 <div className="absolute top-1/2 -translate-y-1/2 w-4 h-4 bg-white rounded-full shadow-lg scale-0 group-hover:scale-100 transition-transform" style={{ left: `calc(${musicProgress}% - 8px)` }}></div>
               </div>
               <span className="text-[11px] font-bold text-white/40 tabular-nums w-10">{formatMusicSeconds(musicDuration)}</span>
             </div>

             <div className="flex items-center justify-between">
                <div className="flex items-center gap-4 md:gap-8 w-1/4">
                  <Button variant="ghost" size="icon" className={`text-white/40 hover:text-white rounded-full ${musicShuffleEnabled ? 'text-kumo-brand' : ''}`} onClick={() => useStore.setState({ musicShuffleEnabled: !musicShuffleEnabled })}>
                    <Shuffle className="w-5 h-5" />
                  </Button>
                  <Button variant="ghost" size="icon" className="text-white/40 hover:text-white rounded-full" onClick={() => {
                    const modes = ['all', 'one', 'none'];
                    const next = modes[(modes.indexOf(musicRepeatMode) + 1) % modes.length];
                    useStore.setState({ musicRepeatMode: next });
                  }}>
                    {musicRepeatMode === 'one' ? <Repeat1 className="w-5 h-5" /> : <Repeat className="w-5 h-5" />}
                  </Button>
                </div>

                <div className="flex items-center gap-6 md:gap-12">
                  <Button variant="ghost" size="icon" className="w-12 h-12 text-white hover:bg-white/10 rounded-full active:scale-90 transition-transform" onClick={playPrevious}>
                    <SkipBack className="w-7 h-7 fill-current" />
                  </Button>
                  <Button variant="primary" size="icon" className="w-16 h-16 bg-white text-black hover:bg-white/90 rounded-full shadow-2xl active:scale-95 transition-transform" onClick={musicTogglePlay}>
                    {musicPlaying ? <Pause className="w-7 h-7 fill-current" /> : <Play className="w-7 h-7 fill-current ml-1" />}
                  </Button>
                  <Button variant="ghost" size="icon" className="w-12 h-12 text-white hover:bg-white/10 rounded-full active:scale-90 transition-transform" onClick={playNext}>
                    <SkipForward className="w-7 h-7 fill-current" />
                  </Button>
                </div>

                <div className="flex items-center justify-end gap-6 w-1/4">
                   <div className="hidden md:flex items-center gap-3 group">
                     <Volume2 className="w-5 h-5 text-white/40 group-hover:text-white" />
                     <div className="w-24 h-1 bg-white/10 rounded-full relative cursor-pointer" onClick={(e) => {
                       const rect = e.currentTarget.getBoundingClientRect();
                       const vol = Math.max(0, Math.min(100, ((e.clientX - rect.left) / rect.width) * 100));
                       useStore.setState({ musicVolume: vol });
                       if (audioPlayerInstance) audioPlayerInstance.volume = vol / 100;
                     }}>
                       <div className="absolute inset-y-0 left-0 bg-white/60 group-hover:bg-white rounded-full transition-all" style={{ width: `${musicVolume}%` }}></div>
                     </div>
                   </div>
                   <Button variant="ghost" size="icon" className="text-white/40 hover:text-white rounded-full" onClick={() => useStore.setState({ musicShowFullPlayer: false })}>
                    <Maximize2 className="w-5 h-5" />
                  </Button>
                </div>
             </div>
          </footer>
        </div>
      )}

      <style>{`
        .mask-fade-y {
          mask-image: linear-gradient(to bottom, transparent, black 20%, black 80%, transparent);
          -webkit-mask-image: linear-gradient(to bottom, transparent, black 20%, black 80%, transparent);
        }
        .scrollbar-none::-webkit-scrollbar {
          display: none;
        }
        .scrollbar-none {
          -ms-overflow-style: none;
          scrollbar-width: none;
        }
        @keyframes music-bar-1 {
          0%, 100% { height: 4px; }
          50% { height: 12px; }
        }
        @keyframes music-bar-2 {
          0%, 100% { height: 10px; }
          50% { height: 4px; }
        }
        @keyframes music-bar-3 {
          0%, 100% { height: 6px; }
          50% { height: 14px; }
        }
        .animate-music-bar-1 { animation: music-bar-1 0.8s ease-in-out infinite; }
        .animate-music-bar-2 { animation: music-bar-2 0.8s ease-in-out infinite 0.1s; }
        .animate-music-bar-3 { animation: music-bar-3 0.8s ease-in-out infinite 0.2s; }
      `}</style>
    </div>
  );
};

export default MusicPage;
