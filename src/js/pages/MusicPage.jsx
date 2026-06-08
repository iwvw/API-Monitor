import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Autocomplete } from '@cloudflare/kumo/components/autocomplete';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Table } from '@cloudflare/kumo/components/table';
import { Meter, Tabs } from '@cloudflare/kumo';
import { Slider } from '@cloudflare/kumo/primitives/slider';
import {
  AppCard,
  EmptyState as AppEmptyState,
  SectionHeader as AppSectionHeader,
} from '../components/ui/AppPrimitives.jsx';
import {
  ArrowLeft,
  Compass,
  Disc3,
  Headphones,
  Heart,
  Home,
  Library,
  ListMusic,
  LogOut,
  Maximize2,
  Music2,
  Pause,
  Play,
  RefreshCw,
  Repeat,
  Repeat1,
  Search,
  Shuffle,
  SkipBack,
  SkipForward,
  User,
  Volume2,
  VolumeX,
  X,
} from 'lucide-react';
import useStore from '../store.js';
import toastManager from '../modules/toast.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';

const DEFAULT_COVER = 'https://p2.music.126.net/6y-UleORITEDbvrOLV0Q8A==/5639395138885805.jpg';
const MUSIC_TIMELINE_COMMIT_INTERVAL_MS = 350;
const MUSIC_TIMELINE_MIN_TIME_DELTA = 0.35;
const MUSIC_TIMELINE_MIN_PROGRESS_DELTA = 0.35;

let audioPlayerInstance = null;
let audioPlayerListenerCleanup = null;
let lastMusicTimelineCommitAt = 0;
let lastMusicTimelineState = { currentTime: -1, progress: -1 };

function ensureHttps(url) {
  if (!url || typeof url !== 'string') return url;
  if (url.startsWith('http://')) return url.replace('http://', 'https://');
  if (url.startsWith('//')) return `https:${url}`;
  return url;
}

function withCoverSize(url, size = 200) {
  return `${ensureHttps(url || DEFAULT_COVER)}?param=${size}y${size}`;
}

function formatMusicTime(ms) {
  if (!ms || Number.isNaN(ms)) return '0:00';
  const seconds = Math.floor(ms / 1000);
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${mins}:${secs.toString().padStart(2, '0')}`;
}

function formatMusicSeconds(seconds) {
  if (!seconds || Number.isNaN(seconds)) return '0:00';
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return `${mins}:${secs.toString().padStart(2, '0')}`;
}

function formatPlayCount(value) {
  const count = Number(value) || 0;
  if (count >= 100000000) return `${(count / 100000000).toFixed(1)} 亿`;
  if (count >= 10000) return `${Math.round(count / 10000)} 万`;
  return `${count}`;
}

function clampPercent(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return 0;
  return Math.max(0, Math.min(100, number));
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
      const minutes = Number.parseInt(match[1], 10) || 0;
      const seconds = Number.parseInt(match[2], 10) || 0;
      const ms = Number.parseInt(match[3].padEnd(3, '0'), 10) || 0;
      lyrics.push({
        time: minutes * 60 * 1000 + seconds * 1000 + ms,
        text,
      });
    }
  }

  return lyrics.sort((a, b) => a.time - b.time);
}

function getSongArtists(song) {
  return song?.artists || '未知歌手';
}

function getSongAlbum(song) {
  return song?.album || '未知专辑';
}

function KumoSlider({
  value,
  min = 0,
  max = 100,
  step = 1,
  label,
  disabled,
  onValueChange,
  onValueCommitted,
  className = '',
}) {
  const normalizedValue = Math.max(min, Math.min(max, Number(value) || 0));

  return (
    <Slider.Root
      value={normalizedValue}
      min={min}
      max={max}
      step={step}
      disabled={disabled}
      onValueChange={onValueChange}
      onValueCommitted={onValueCommitted}
      className={`w-full touch-none select-none ${className}`}
    >
      <Slider.Control className="relative flex h-6 w-full items-center">
        <Slider.Track className="relative h-1.5 w-full overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
          <Slider.Indicator className="h-full rounded-full bg-kumo-brand" />
        </Slider.Track>
        <Slider.Thumb
          className="size-3 rounded-full border border-kumo-line bg-kumo-base outline-none transition-transform focus-visible:ring-2 focus-visible:ring-kumo-brand disabled:opacity-50 data-[dragging]:scale-110"
          getAriaLabel={() => label}
        />
      </Slider.Control>
    </Slider.Root>
  );
}

function AlbumCover({ song, playlist, size = 'md', className = '' }) {
  const sizeClass = {
    xs: 'h-8 w-8 rounded',
    sm: 'h-10 w-10 rounded-md',
    md: 'h-14 w-14 rounded-md',
    lg: 'h-20 w-20 rounded-lg',
    xl: 'h-28 w-28 rounded-lg',
  }[size] || 'h-14 w-14 rounded-md';
  const src = withCoverSize(song?.cover || playlist?.cover, size === 'xl' ? 512 : 200);

  return (
    <img
      src={src}
      className={`${sizeClass} shrink-0 border border-kumo-line object-cover bg-kumo-recessed ${className}`}
      alt=""
      loading="lazy"
    />
  );
}

function MusicCard({ className = '', children, ...props }) {
  return (
    <AppCard
      {...props}
      padding="none"
      className={className}
    >
      {children}
    </AppCard>
  );
}

function EmptyState({ icon: Icon = Music2, title, description, action }) {
  return (
    <AppEmptyState icon={Icon} title={title} description={description} action={action} />
  );
}

function SectionHeader({ title, description, action }) {
  return <AppSectionHeader title={title} description={description} action={action} />;
}

function SongTable({
  songs,
  loading,
  emptyTitle = '暂无歌曲',
  emptyDescription,
  currentSong,
  onPlay,
  onLoadMore,
  hasMore,
  loadingMore,
  compact = false,
}) {
  if (loading) {
    return (
      <MusicCard className="space-y-3 p-4">
        {Array.from({ length: compact ? 5 : 8 }).map((_, index) => (
          <SkeletonLine key={index} className="h-10 w-full" />
        ))}
      </MusicCard>
    );
  }

  if (!songs?.length) {
    return <EmptyState title={emptyTitle} description={emptyDescription} />;
  }

  return (
    <MusicCard className="overflow-hidden">
      <div className="max-h-[calc(100vh-310px)] min-h-0 overflow-auto">
        <Table layout="fixed">
          <colgroup>
            <col className="w-12" />
            <col />
            <col className="hidden w-[28%] md:table-column" />
            <col className="w-20" />
            <col className="w-14" />
          </colgroup>
          <Table.Header sticky variant="compact">
            <Table.Row>
              <Table.Head className="text-center text-[10px]">#</Table.Head>
              <Table.Head className="text-[10px]">歌曲</Table.Head>
              <Table.Head className="hidden text-[10px] md:table-cell">专辑</Table.Head>
              <Table.Head className="text-right text-[10px]">时长</Table.Head>
              <Table.Head className="text-right text-[10px]" />
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {songs.map((song, index) => {
              const isCurrent = currentSong?.id === song.id;
              return (
                <Table.Row
                  key={`${song.id}-${index}`}
                  variant={isCurrent ? 'selected' : 'default'}
                  className="cursor-pointer"
                  onClick={() => onPlay(song)}
                >
                  <Table.Cell className="text-center text-xs tabular-nums text-kumo-subtle">
                    {isCurrent ? <Badge variant="success" appearance="dot">播</Badge> : index + 1}
                  </Table.Cell>
                  <Table.Cell>
                    <div className="flex min-w-0 items-center gap-3">
                      <AlbumCover song={song} size="sm" />
                      <div className="min-w-0">
                        <div className="truncate text-xs font-semibold text-kumo-strong">{song.name}</div>
                        <div className="truncate text-[11px] text-kumo-subtle">{getSongArtists(song)}</div>
                      </div>
                    </div>
                  </Table.Cell>
                  <Table.Cell className="hidden truncate text-xs text-kumo-subtle md:table-cell">
                    {getSongAlbum(song)}
                  </Table.Cell>
                  <Table.Cell className="text-right text-xs tabular-nums text-kumo-subtle">
                    {formatMusicTime(song.duration)}
                  </Table.Cell>
                  <Table.Cell className="text-right">
                    <Button
                      type="button"
                      variant={isCurrent ? 'primary' : 'ghost'} size="sm"
                      shape="square"
                      aria-label="播放"
                      icon={<Play className="h-3.5 w-3.5" />}
                      onClick={(event) => {
                        event.stopPropagation();
                        onPlay(song);
                      }}
                    />
                  </Table.Cell>
                </Table.Row>
              );
            })}
          </Table.Body>
        </Table>
      </div>

      {hasMore && (
        <div className="border-t border-kumo-line p-3 text-center">
          <Button
            type="button"
            variant="secondary" size="sm"
            loading={loadingMore}
            onClick={onLoadMore}
          >
            加载更多
          </Button>
        </div>
      )}
    </MusicCard>
  );
}

function PlaylistGrid({ playlists, loading, onOpen, emptyText = '暂无歌单' }) {
  if (loading) {
    return (
      <div className="grid gap-2 md:grid-cols-2 2xl:grid-cols-3">
        {Array.from({ length: 8 }).map((_, index) => (
          <SkeletonLine key={index} className="h-[72px] w-full" />
        ))}
      </div>
    );
  }

  if (!playlists?.length) {
    return <EmptyState icon={ListMusic} title={emptyText} />;
  }

  return (
    <div className="grid gap-2 md:grid-cols-2 2xl:grid-cols-3">
      {playlists.map((playlist) => (
        <MusicCard
          key={playlist.id}
          className="group flex h-[72px] cursor-pointer items-center gap-3 overflow-hidden p-2 transition-colors hover:border-kumo-brand"
          onClick={() => onOpen(playlist.id)}
        >
          <img
            src={withCoverSize(playlist.cover, 120)}
            className="h-14 w-14 shrink-0 rounded-md border border-kumo-line object-cover bg-kumo-recessed"
            alt=""
            loading="lazy"
          />
          <div className="min-w-0 flex-1">
            <div className="line-clamp-2 text-xs font-semibold leading-4 text-kumo-strong">
              {playlist.name}
            </div>
            <div className="mt-1 flex items-center justify-between gap-2 text-[11px] leading-4 text-kumo-subtle">
              <span className="truncate">{playlist.creator || `${playlist.trackCount || 0} 首`}</span>
              {playlist.playCount != null && <span className="shrink-0 tabular-nums">{formatPlayCount(playlist.playCount)}</span>}
            </div>
          </div>
        </MusicCard>
      ))}
    </div>
  );
}

function MusicPage() {
  const musicPlaylist = useStore((state) => state.musicPlaylist);
  const musicCurrentIndex = useStore((state) => state.musicCurrentIndex);
  const musicCurrentSong = useStore((state) => state.musicCurrentSong);
  const musicPlaying = useStore((state) => state.musicPlaying);
  const musicBuffering = useStore((state) => state.musicBuffering);
  const musicCurrentTime = useStore((state) => state.musicCurrentTime);
  const musicDuration = useStore((state) => state.musicDuration);
  const musicProgress = useStore((state) => state.musicProgress);
  const musicVolume = useStore((state) => state.musicVolume);
  const musicRepeatMode = useStore((state) => state.musicRepeatMode);
  const musicShuffleEnabled = useStore((state) => state.musicShuffleEnabled);
  const musicLyrics = useStore((state) => state.musicLyrics);
  const musicCurrentLyricIndex = useStore((state) => state.musicCurrentLyricIndex);
  const musicCurrentLyricText = useStore((state) => state.musicCurrentLyricText);
  const musicCurrentLyricTranslation = useStore((state) => state.musicCurrentLyricTranslation);
  const musicShowFullPlayer = useStore((state) => state.musicShowFullPlayer);
  const musicUser = useStore((state) => state.musicUser);
  const musicShowLoginModal = useStore((state) => state.musicShowLoginModal);
  const musicCurrentTab = useStore((state) => state.musicCurrentTab);
  const musicSearchKeyword = useStore((state) => state.musicSearchKeyword);
  const musicSearchResults = useStore((state) => state.musicSearchResults);
  const musicSearchPlaylists = useStore((state) => state.musicSearchPlaylists);
  const musicSearchArtists = useStore((state) => state.musicSearchArtists);
  const musicSearchType = useStore((state) => state.musicSearchType);
  const musicSearchOffset = useStore((state) => state.musicSearchOffset);
  const musicSearchHasMore = useStore((state) => state.musicSearchHasMore);
  const musicSearchLoading = useStore((state) => state.musicSearchLoading);
  const musicSearchLoadingMore = useStore((state) => state.musicSearchLoadingMore);
  const musicMyPlaylists = useStore((state) => state.musicMyPlaylists);
  const musicCurrentPlaylistDetail = useStore((state) => state.musicCurrentPlaylistDetail);
  const musicShowDetail = useStore((state) => state.musicShowDetail);
  const musicMuted = useStore((state) => state.musicMuted);

  const [hotPlaylists, setHotPlaylists] = useState([]);
  const [playlistsLoading, setPlaylistsLoading] = useState(false);
  const [dailyRecommend, setDailyRecommend] = useState([]);
  const [recommendLoading, setRecommendLoading] = useState(false);
  const [collectedPlaylists, setCollectedPlaylists] = useState([]);
  const [qrImg, setQrImg] = useState('');
  const [qrExpired, setQrExpired] = useState(false);
  const [qrChecking, setQrChecking] = useState(false);
  const [loginStatusText, setLoginStatusText] = useState('请使用网易云音乐 App 扫码登录');
  const [loginLoading, setLoginLoading] = useState(false);
  const [playlistDetailLoading, setPlaylistDetailLoading] = useState(false);
  const [musicSuggestions, setMusicSuggestions] = useState([]);

  const lyricsScrollRef = useRef(null);
  const qrCheckTimerRef = useRef(null);

  const likedPlaylist = useMemo(
    () => musicMyPlaylists.find((playlist) => playlist.isSpecial),
    [musicMyPlaylists]
  );
  const activeLyrics = useMemo(() => {
    if (!musicLyrics.length) return [];
    const start = Math.max(0, musicCurrentLyricIndex - 2);
    const end = Math.min(musicLyrics.length, musicCurrentLyricIndex + 5);
    return musicLyrics.slice(start, end).map((line, index) => ({
      ...line,
      originalIndex: start + index,
    }));
  }, [musicLyrics, musicCurrentLyricIndex]);

  function initAudioPlayer() {
    if (audioPlayerInstance) return audioPlayerInstance;

    audioPlayerInstance = new Audio();
    audioPlayerInstance.preload = 'auto';
    audioPlayerInstance.volume = (useStore.getState().musicMuted ? 0 : useStore.getState().musicVolume) / 100;

    const handleCanPlay = () => useStore.setState({ musicBuffering: false });
    const handleWaiting = () => useStore.setState({ musicBuffering: true });
    const handlePlaying = () => useStore.setState({ musicBuffering: false });
    const handlePlay = () => {
      useStore.setState({ musicPlaying: true });
      if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'playing';
    };
    const handlePause = () => {
      useStore.setState({ musicPlaying: false });
      if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'paused';
    };

    audioPlayerInstance.addEventListener('timeupdate', handleTimeUpdate);
    audioPlayerInstance.addEventListener('ended', handleTrackEnd);
    audioPlayerInstance.addEventListener('error', handlePlayError);
    audioPlayerInstance.addEventListener('loadedmetadata', handleMetadataLoaded);
    audioPlayerInstance.addEventListener('canplay', handleCanPlay);
    audioPlayerInstance.addEventListener('waiting', handleWaiting);
    audioPlayerInstance.addEventListener('playing', handlePlaying);
    audioPlayerInstance.addEventListener('play', handlePlay);
    audioPlayerInstance.addEventListener('pause', handlePause);

    setupMediaSessionHandlers();
    audioPlayerListenerCleanup = () => {
      if (!audioPlayerInstance) return;
      audioPlayerInstance.removeEventListener('timeupdate', handleTimeUpdate);
      audioPlayerInstance.removeEventListener('ended', handleTrackEnd);
      audioPlayerInstance.removeEventListener('error', handlePlayError);
      audioPlayerInstance.removeEventListener('loadedmetadata', handleMetadataLoaded);
      audioPlayerInstance.removeEventListener('canplay', handleCanPlay);
      audioPlayerInstance.removeEventListener('waiting', handleWaiting);
      audioPlayerInstance.removeEventListener('playing', handlePlaying);
      audioPlayerInstance.removeEventListener('play', handlePlay);
      audioPlayerInstance.removeEventListener('pause', handlePause);
      audioPlayerInstance.pause();
      audioPlayerInstance.removeAttribute('src');
      audioPlayerInstance.load();
      audioPlayerInstance = null;
      audioPlayerListenerCleanup = null;
      useStore.setState({ musicPlaying: false, musicBuffering: false });
      if ('mediaSession' in navigator) {
        ['play', 'pause', 'previoustrack', 'nexttrack', 'seekto'].forEach(action => {
          try {
            navigator.mediaSession.setActionHandler(action, null);
          } catch {
            // Some browsers do not support every media session action.
          }
        });
      }
    };
    return audioPlayerInstance;
  }

  function handleTimeUpdate() {
    if (!audioPlayerInstance) return;

    const currentTime = audioPlayerInstance.currentTime;
    const duration = audioPlayerInstance.duration || 0;
    const progress = duration ? (currentTime / duration) * 100 : 0;
    const state = useStore.getState();
    const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
    const shouldCommitTimeline =
      now - lastMusicTimelineCommitAt >= MUSIC_TIMELINE_COMMIT_INTERVAL_MS ||
      Math.abs(currentTime - lastMusicTimelineState.currentTime) >= MUSIC_TIMELINE_MIN_TIME_DELTA ||
      (!state.musicIsDragging &&
        Math.abs(progress - lastMusicTimelineState.progress) >= MUSIC_TIMELINE_MIN_PROGRESS_DELTA) ||
      (duration > 0 && duration - currentTime < 0.5);

    if (shouldCommitTimeline) {
      const nextTimeline = { musicCurrentTime: currentTime };
      if (!state.musicIsDragging) nextTimeline.musicProgress = progress;
      useStore.setState(nextTimeline);
      lastMusicTimelineCommitAt = now;
      lastMusicTimelineState = { currentTime, progress };
    }

    updateCurrentLyricLine();
    if (shouldCommitTimeline) updateMediaSessionPosition();
  }

  function handleTrackEnd() {
    const { musicRepeatMode: repeatMode, musicPlaylist: playlist } = useStore.getState();

    if (repeatMode === 'one') {
      audioPlayerInstance.currentTime = 0;
      audioPlayerInstance.play();
      return;
    }

    if (repeatMode === 'all' || playlist.length > 1) {
      playNext();
      return;
    }

    useStore.setState({ musicPlaying: false });
  }

  function handlePlayError(event) {
    if (!audioPlayerInstance || audioPlayerInstance.src === '' || audioPlayerInstance.src === window.location.href) return;
    console.error('[Music] Play error:', event);
    useStore.setState({ musicBuffering: false });

    const { musicCurrentSong: currentSong } = useStore.getState();
    if (currentSong) retryWithUnblock(currentSong.id);
  }

  function handleMetadataLoaded() {
    if (!audioPlayerInstance) return;
    useStore.setState({ musicDuration: audioPlayerInstance.duration || 0 });
  }

  async function retryWithUnblock(songId) {
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
  }

  function updateCurrentLyricLine() {
    const {
      musicLyrics: lyrics,
      musicLyricsTranslation: translations,
      musicCurrentLyricIndex: currentLyricIndex,
    } = useStore.getState();

    if (!lyrics.length || !audioPlayerInstance) return;

    const currentTime = audioPlayerInstance.currentTime * 1000 + 150;
    let activeIndex = -1;

    for (let index = 0; index < lyrics.length; index += 1) {
      if (lyrics[index].time <= currentTime) activeIndex = index;
      else break;
    }

    if (activeIndex < 0 || activeIndex === currentLyricIndex) return;

    const currentLine = lyrics[activeIndex];
    const translation = translations.find((item) => Math.abs(item.time - currentLine.time) < 1000);

    useStore.setState({
      musicCurrentLyricIndex: activeIndex,
      musicCurrentLyricText: currentLine.text || '',
      musicCurrentLyricTranslation: translation ? translation.text : currentLine.trans || '',
    });
  }

  async function musicLoadHotPlaylists() {
    setPlaylistsLoading(true);
    try {
      const response = await fetch('/api/music/top/playlist?limit=24');
      const data = await response.json();
      if (data.playlists) {
        setHotPlaylists(data.playlists.map((playlist) => ({
          id: playlist.id,
          name: playlist.name,
          cover: ensureHttps(playlist.coverImgUrl),
          playCount: playlist.playCount,
          creator: playlist.creator?.nickname,
        })));
      }
    } catch (error) {
      console.error('[Music] Hot playlists failed:', error);
      toastManager.error('热门歌单加载失败');
    } finally {
      setPlaylistsLoading(false);
    }
  }

  async function musicLoadDailyRecommend() {
    if (!useStore.getState().musicUser) return;

    setRecommendLoading(true);
    try {
      const response = await fetch('/api/music/recommend/songs');
      const data = await response.json();
      if (data.data?.dailySongs) {
        setDailyRecommend(data.data.dailySongs.map((song) => ({
          id: song.id,
          name: song.name,
          artists: song.ar?.map((artist) => artist.name).join(' / '),
          album: song.al?.name,
          cover: ensureHttps(song.al?.picUrl),
          duration: song.dt,
        })));
      }
    } catch (error) {
      console.error('[Music] Daily recommend failed:', error);
      toastManager.error('每日推荐加载失败');
    } finally {
      setRecommendLoading(false);
    }
  }

  async function musicLoadUserPlaylists() {
    const user = useStore.getState().musicUser;
    if (!user) return;

    try {
      const response = await fetch(`/api/music/user/playlist?uid=${user.userId}`);
      const data = await response.json();
      if (!data.playlist) return;

      const my = [];
      const collected = [];

      data.playlist.forEach((playlist) => {
        const item = {
          id: playlist.id,
          name: playlist.name,
          cover: ensureHttps(playlist.coverImgUrl),
          trackCount: playlist.trackCount,
          isSpecial: playlist.specialType === 5,
          creator: playlist.creator?.nickname,
        };

        if (playlist.creator?.userId === user.userId) my.push(item);
        else collected.push(item);
      });

      useStore.setState({ musicMyPlaylists: my });
      setCollectedPlaylists(collected);
    } catch (error) {
      console.error('[Music] User playlists failed:', error);
    }
  }

  async function musicLoadPlaylistDetail(id) {
    useStore.setState({ musicShowDetail: true, musicCurrentPlaylistDetail: null });
    setPlaylistDetailLoading(true);

    try {
      const response = await fetch(`/api/music/playlist/detail?id=${id}&fetch_limit=200`);
      const data = await response.json();

      if (data.playlist) {
        const playlist = data.playlist;
        useStore.setState({
          musicCurrentPlaylistDetail: {
            id: playlist.id,
            name: playlist.name,
            cover: ensureHttps(playlist.coverImgUrl),
            description: playlist.description,
            creator: playlist.creator?.nickname,
            trackCount: playlist.trackCount,
            tracks: (playlist.tracks || []).map((song) => ({
              id: song.id,
              name: song.name,
              artists: song.ar?.map((artist) => artist.name).join(' / '),
              album: song.al?.name,
              cover: ensureHttps(song.al?.picUrl),
              duration: song.dt,
            })),
          },
        });
      }
    } catch (error) {
      console.error('[Music] Playlist detail failed:', error);
      useStore.setState({ musicShowDetail: false });
      toastManager.error('歌单详情加载失败');
    } finally {
      setPlaylistDetailLoading(false);
    }
  }

  async function musicSearch(loadMore = false) {
    const {
      musicSearchKeyword: keyword,
      musicSearchType: searchType,
      musicSearchOffset: searchOffset,
      musicSearchResults: searchResults,
      musicSearchPlaylists: searchPlaylists,
      musicSearchArtists: searchArtists,
    } = useStore.getState();

    if (!keyword.trim()) return;

    if (!loadMore) {
      useStore.setState({
        musicSearchResults: [],
        musicSearchPlaylists: [],
        musicSearchArtists: [],
        musicSearchOffset: 0,
        musicSearchHasMore: true,
        musicSearchLoading: true,
      });
    } else {
      useStore.setState({ musicSearchLoadingMore: true });
    }

    const typeMap = { songs: 1, playlists: 1000, artists: 100 };
    const limit = 30;
    const offset = loadMore ? searchOffset : 0;

    try {
      const response = await fetch(`/api/music/search?keywords=${encodeURIComponent(keyword)}&type=${typeMap[searchType]}&limit=${limit}&offset=${offset}`);
      const data = await response.json();

      if (searchType === 'songs' && data.result?.songs) {
        const songs = data.result.songs.map((song) => ({
          id: song.id,
          name: song.name,
          artists: song.ar?.map((artist) => artist.name).join(' / '),
          album: song.al?.name,
          cover: ensureHttps(song.al?.picUrl),
          duration: song.dt,
        }));
        useStore.setState({
          musicSearchResults: loadMore ? [...searchResults, ...songs] : songs,
          musicSearchHasMore: songs.length >= limit,
        });
      } else if (searchType === 'playlists' && data.result?.playlists) {
        const playlists = data.result.playlists.map((playlist) => ({
          id: playlist.id,
          name: playlist.name,
          cover: ensureHttps(playlist.coverImgUrl),
          creator: playlist.creator?.nickname,
          trackCount: playlist.trackCount,
          playCount: playlist.playCount,
        }));
        useStore.setState({
          musicSearchPlaylists: loadMore ? [...searchPlaylists, ...playlists] : playlists,
          musicSearchHasMore: playlists.length >= limit,
        });
      } else if (searchType === 'artists' && data.result?.artists) {
        const artists = data.result.artists.map((artist) => ({
          id: artist.id,
          name: artist.name,
          cover: ensureHttps(artist.picUrl || artist.img1v1Url),
          alias: artist.alias?.join(' / '),
          albumCount: artist.albumSize,
        }));
        useStore.setState({
          musicSearchArtists: loadMore ? [...searchArtists, ...artists] : artists,
          musicSearchHasMore: artists.length >= limit,
        });
      }

      useStore.setState({ musicSearchOffset: offset + limit });
    } catch (error) {
      console.error('[Music] Search failed:', error);
      toastManager.error('搜索失败');
    } finally {
      useStore.setState({ musicSearchLoading: false, musicSearchLoadingMore: false });
    }
  }

  async function musicPlay(song) {
    if (!song) return;

    const player = initAudioPlayer();
    player.pause();
    player.src = '';

    useStore.setState({
      musicBuffering: true,
      musicCurrentSong: song,
      musicCurrentLyricIndex: -1,
      musicCurrentLyricText: '',
      musicCurrentLyricTranslation: '',
      musicLyrics: [],
      musicLyricsTranslation: [],
      musicProgress: 0,
      musicCurrentTime: 0,
    });
    lastMusicTimelineCommitAt = 0;
    lastMusicTimelineState = { currentTime: 0, progress: 0 };

    const currentPlaylist = useStore.getState().musicPlaylist;
    const nextPlaylist = currentPlaylist.find((item) => item.id === song.id)
      ? currentPlaylist
      : [...currentPlaylist, song];

    useStore.setState({
      musicPlaylist: nextPlaylist,
      musicCurrentIndex: nextPlaylist.findIndex((item) => item.id === song.id),
    });

    updateMediaSession(song);
    musicLoadLyrics(song.id);

    try {
      const response = await fetch(`/api/music/song/url?id=${song.id}&level=exhigh`);
      const data = await response.json();
      let audioUrl = data.data?.[0]?.url;

      if (!audioUrl) {
        const unblockResponse = await fetch(`/api/music/song/url/unblock?id=${song.id}`);
        const unblockData = await unblockResponse.json();
        audioUrl = unblockData.data?.url || unblockData.url;
      }

      if (!audioUrl) {
        toastManager.error('当前歌曲暂不可播放');
        useStore.setState({ musicBuffering: false });
        return;
      }

      player.src = audioUrl;
      player.volume = (useStore.getState().musicMuted ? 0 : useStore.getState().musicVolume) / 100;
      await player.play();
      useStore.setState({ musicPlaying: true, musicBuffering: false });
    } catch (error) {
      console.error('[Music] Play failed:', error);
      toastManager.error('播放失败');
      useStore.setState({ musicBuffering: false });
    }
  }

  function musicPlayPlaylist(tracks) {
    if (!tracks?.length) return;
    useStore.setState({ musicPlaylist: tracks, musicCurrentIndex: 0 });
    musicPlay(tracks[0]);
  }

  function musicTogglePlay() {
    const player = initAudioPlayer();
    const state = useStore.getState();

    if (!state.musicCurrentSong && state.musicPlaylist.length > 0) {
      musicPlay(state.musicPlaylist[0]);
      return;
    }

    if (state.musicPlaying) {
      player.pause();
      return;
    }

    if (!player.src && state.musicCurrentSong) {
      musicPlay(state.musicCurrentSong);
      return;
    }

    player.play().catch((error) => {
      console.error('[Music] Resume failed:', error);
      toastManager.error('恢复播放失败');
    });
  }

  function playNext() {
    const { musicPlaylist: playlist, musicCurrentIndex: currentIndex, musicShuffleEnabled: shuffleEnabled } = useStore.getState();
    if (!playlist.length) return;

    const nextIndex = shuffleEnabled
      ? Math.floor(Math.random() * playlist.length)
      : (currentIndex + 1) % playlist.length;

    musicPlay(playlist[nextIndex]);
  }

  function playPrevious() {
    const { musicPlaylist: playlist, musicCurrentIndex: currentIndex, musicShuffleEnabled: shuffleEnabled } = useStore.getState();
    if (!playlist.length) return;

    const previousIndex = shuffleEnabled
      ? Math.floor(Math.random() * playlist.length)
      : (currentIndex - 1 + playlist.length) % playlist.length;

    musicPlay(playlist[previousIndex]);
  }

  async function musicLoadLyrics(songId) {
    try {
      const response = await fetch(`/api/music/lyric?id=${songId}`);
      const data = await response.json();
      const rawLyrics = parseLyrics(data.lrc?.lyric || '');
      const rawTrans = parseLyrics(data.tlyric?.lyric || '');

      const merged = rawLyrics.map((line) => {
        const translation = rawTrans.find((item) => Math.abs(item.time - line.time) < 1000);
        return { ...line, trans: translation ? translation.text : '' };
      });

      useStore.setState({
        musicLyrics: merged,
        musicLyricsTranslation: rawTrans,
        musicCurrentLyricIndex: -1,
      });
    } catch (error) {
      console.error('[Music] Lyrics failed:', error);
    }
  }

  async function musicLogout() {
    try {
      await fetch('/api/music/logout', { method: 'POST' });
    } catch (error) {
      console.error('[Music] Logout failed:', error);
    }

    useStore.setState({ musicUser: null, musicMyPlaylists: [] });
    setCollectedPlaylists([]);
    localStorage.removeItem('music_user_info');
    toastManager.success('已退出登录');
  }

  async function musicGenerateLoginQr() {
    setLoginLoading(true);
    setQrExpired(false);
    setLoginStatusText('请使用网易云音乐 App 扫码登录');

    try {
      const keyResponse = await fetch('/api/music/login/qr/key');
      const keyData = await keyResponse.json();
      const key = keyData.data?.unikey;

      const qrResponse = await fetch(`/api/music/login/qr/create?key=${key}&qrimg=true`);
      const qrData = await qrResponse.json();
      setQrImg(qrData.data?.qrimg);
      startQrCheck(key);
    } catch (error) {
      console.error('[Music] QR generate failed:', error);
      toastManager.error('生成二维码失败');
    } finally {
      setLoginLoading(false);
    }
  }

  function startQrCheck(key) {
    if (qrCheckTimerRef.current) {
      clearInterval(qrCheckTimerRef.current);
    }

    setQrChecking(true);
    qrCheckTimerRef.current = setInterval(async () => {
      try {
        const response = await fetch(`/api/music/login/qr/check?key=${key}`);
        const data = await response.json();

        if (data.code === 800) {
          setQrExpired(true);
          setQrChecking(false);
          clearInterval(qrCheckTimerRef.current);
          qrCheckTimerRef.current = null;
        } else if (data.code === 803) {
          setQrChecking(false);
          clearInterval(qrCheckTimerRef.current);
          qrCheckTimerRef.current = null;
          useStore.setState({ musicShowLoginModal: false });
          checkLoginStatus();
        } else if (data.code === 802) {
          setLoginStatusText('扫码成功，请在手机上确认登录');
        }
      } catch (error) {
        console.error('[Music] QR check failed:', error);
      }
    }, 2000);
  }

  async function checkLoginStatus() {
    try {
      const response = await fetch('/api/music/auth/status');
      const data = await response.json();

      if (data.loggedIn && data.user) {
        const user = {
          ...data.user,
          avatarUrl: ensureHttps(data.user.avatarUrl),
        };
        useStore.setState({ musicUser: user });
        localStorage.setItem('music_user_info', JSON.stringify(user));
        musicLoadUserPlaylists();
        musicLoadDailyRecommend();
      }
    } catch (error) {
      console.error('[Music] Auth status failed:', error);
    }
  }

  function initMusicModule() {
    musicLoadHotPlaylists();

    const cached = localStorage.getItem('music_user_info');
    if (cached) {
      try {
        useStore.setState({ musicUser: JSON.parse(cached) });
        musicLoadUserPlaylists();
        musicLoadDailyRecommend();
      } catch (error) {
        console.error('[Music] Cached user parse failed:', error);
      }
    }

    checkLoginStatus();
  }

  function updateMediaSession(song) {
    if (!('mediaSession' in navigator)) return;
    navigator.mediaSession.metadata = new MediaMetadata({
      title: song.name,
      artist: song.artists,
      album: song.album,
      artwork: [
        { src: withCoverSize(song.cover, 512), sizes: '512x512', type: 'image/jpeg' },
      ],
    });
  }

  function updateMediaSessionPosition() {
    if (!('mediaSession' in navigator) || !audioPlayerInstance) return;

    const duration = audioPlayerInstance.duration || 0;
    if (!Number.isFinite(duration) || duration <= 0) return;

    navigator.mediaSession.setPositionState({
      duration,
      playbackRate: audioPlayerInstance.playbackRate,
      position: audioPlayerInstance.currentTime,
    });
  }

  function setupMediaSessionHandlers() {
    if (!('mediaSession' in navigator)) return;
    navigator.mediaSession.setActionHandler('play', () => audioPlayerInstance.play());
    navigator.mediaSession.setActionHandler('pause', () => audioPlayerInstance.pause());
    navigator.mediaSession.setActionHandler('nexttrack', () => playNext());
    navigator.mediaSession.setActionHandler('previoustrack', () => playPrevious());
    navigator.mediaSession.setActionHandler('seekto', (details) => {
      if (details.seekTime != null) audioPlayerInstance.currentTime = details.seekTime;
    });
  }

  async function fetchSuggestions(value) {
    if (!value?.trim()) {
      setMusicSuggestions([]);
      return;
    }

    try {
      const response = await fetch(`/api/music/search/suggest?keywords=${encodeURIComponent(value)}`);
      const data = await response.json();
      setMusicSuggestions(data.result?.allMatch || []);
    } catch (error) {
      console.error('[Music] Suggest failed:', error);
    }
  }

  function handleSearchSubmit() {
    if (!musicSearchKeyword.trim()) return;
    useStore.setState({ musicCurrentTab: 'search', musicShowDetail: false });
    setTimeout(() => musicSearch(false), 0);
  }

  function handleSeekChange(value) {
    useStore.setState({ musicIsDragging: true, musicProgress: clampPercent(value) });
  }

  function handleSeekCommitted(value) {
    const progress = clampPercent(value);
    const duration = useStore.getState().musicDuration || 0;

    if (audioPlayerInstance && duration > 0) {
      audioPlayerInstance.currentTime = (progress / 100) * duration;
      useStore.setState({ musicCurrentTime: audioPlayerInstance.currentTime });
      lastMusicTimelineCommitAt = 0;
      lastMusicTimelineState = { currentTime: audioPlayerInstance.currentTime, progress };
    }

    useStore.setState({ musicIsDragging: false, musicProgress: progress });
  }

  function handleVolumeChange(value) {
    const volume = clampPercent(value);
    const muted = volume === 0;

    useStore.setState({ musicVolume: volume, musicMuted: muted });
    if (audioPlayerInstance) audioPlayerInstance.volume = muted ? 0 : volume / 100;
  }

  function toggleMute() {
    const nextMuted = !musicMuted;
    useStore.setState({ musicMuted: nextMuted });
    if (audioPlayerInstance) audioPlayerInstance.volume = nextMuted ? 0 : musicVolume / 100;
  }

  function cycleRepeatMode() {
    const modes = ['none', 'all', 'one'];
    const nextMode = modes[(modes.indexOf(musicRepeatMode) + 1) % modes.length];
    useStore.setState({ musicRepeatMode: nextMode });
  }

  useEffect(() => {
    initAudioPlayer();
    initMusicModule();

    return () => {
      if (qrCheckTimerRef.current) {
        clearInterval(qrCheckTimerRef.current);
        qrCheckTimerRef.current = null;
      }
      audioPlayerListenerCleanup?.();
    };
  }, []);

  useEffect(() => {
    if (audioPlayerInstance) {
      audioPlayerInstance.volume = musicMuted ? 0 : musicVolume / 100;
    }
  }, [musicVolume, musicMuted]);

  useEffect(() => {
    if (!musicShowFullPlayer || !lyricsScrollRef.current) return;
    const active = lyricsScrollRef.current.querySelector('[data-active="true"]');
    active?.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }, [musicCurrentLyricIndex, musicShowFullPlayer]);

  const renderLibraryRail = () => (
    <MusicCard className="flex min-h-0 flex-col p-4">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-sm font-semibold text-kumo-strong">音乐库</h2>
          <p className="mt-0.5 text-xs text-kumo-subtle">
            {musicUser ? `${musicUser.nickname} 的歌单` : '登录后同步歌单'}
          </p>
        </div>
        {musicUser ? (
          <Button
            type="button"
            variant="ghost" size="sm"
            shape="square"
            aria-label="退出登录"
            icon={<LogOut className="h-3.5 w-3.5" />}
            onClick={musicLogout}
          />
        ) : (
          <Button
            type="button"
            variant="primary" size="sm"
            icon={<User className="h-3.5 w-3.5" />}
            onClick={() => useStore.setState({ musicShowLoginModal: true })}
          >
            登录
          </Button>
        )}
      </div>

      <div className="mt-4 grid gap-2">
        <Button
          type="button"
          variant="secondary" size="sm"
          className="flex h-auto w-full items-center justify-between px-3 py-2 text-left"
          onClick={() => {
            if (likedPlaylist) musicLoadPlaylistDetail(likedPlaylist.id);
            else useStore.setState({ musicShowLoginModal: true });
          }}
        >
          <span className="flex min-w-0 items-center gap-2">
            <Heart className="h-4 w-4 text-kumo-danger" />
            <span className="truncate text-xs font-semibold text-kumo-strong">我喜欢的音乐</span>
          </span>
          <span className="text-[11px] text-kumo-subtle">{likedPlaylist?.trackCount || 0}</span>
        </Button>

        <Button
          type="button"
          variant="secondary" size="sm"
          className="flex h-auto w-full items-center justify-between px-3 py-2 text-left"
          onClick={() => useStore.setState({ musicCurrentTab: 'discover', musicShowDetail: false })}
        >
          <span className="flex min-w-0 items-center gap-2">
            <Compass className="h-4 w-4 text-kumo-info" />
            <span className="truncate text-xs font-semibold text-kumo-strong">发现歌单</span>
          </span>
          <span className="text-[11px] text-kumo-subtle">{hotPlaylists.length}</span>
        </Button>
      </div>

      <div className="mt-4 border-t border-kumo-line pt-4">
        <SectionHeader title="我的歌单" description={`${musicMyPlaylists.length} 个创建歌单`} />
        <div className="max-h-[32vh] space-y-1 overflow-auto pr-1">
          {musicMyPlaylists.length === 0 ? (
            <p className="rounded-md bg-kumo-recessed/35 px-3 py-6 text-center text-xs text-kumo-subtle">
              暂无本地歌单数据
            </p>
          ) : (
            musicMyPlaylists.map((playlist) => (
              <Button
                key={playlist.id}
                type="button"
                variant="ghost" size="sm"
                className="flex h-auto w-full min-w-0 justify-start gap-2 px-2 py-2 text-left"
                onClick={() => musicLoadPlaylistDetail(playlist.id)}
              >
                <AlbumCover playlist={playlist} size="xs" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-xs font-semibold text-kumo-strong">{playlist.name}</span>
                  <span className="block truncate text-[11px] text-kumo-subtle">{playlist.trackCount || 0} 首</span>
                </span>
              </Button>
            ))
          )}
        </div>
      </div>

      <div className="mt-4 border-t border-kumo-line pt-4">
        <SectionHeader title="收藏歌单" description={`${collectedPlaylists.length} 个收藏歌单`} />
        <div className="max-h-[26vh] space-y-1 overflow-auto pr-1">
          {collectedPlaylists.length === 0 ? (
            <p className="rounded-md bg-kumo-recessed/35 px-3 py-6 text-center text-xs text-kumo-subtle">
              暂无收藏歌单
            </p>
          ) : (
            collectedPlaylists.map((playlist) => (
              <Button
                key={playlist.id}
                type="button"
                variant="ghost" size="sm"
                className="flex h-auto w-full min-w-0 justify-start gap-2 px-2 py-2 text-left"
                onClick={() => musicLoadPlaylistDetail(playlist.id)}
              >
                <AlbumCover playlist={playlist} size="xs" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-xs font-semibold text-kumo-strong">{playlist.name}</span>
                  <span className="block truncate text-[11px] text-kumo-subtle">{playlist.trackCount || 0} 首</span>
                </span>
              </Button>
            ))
          )}
        </div>
      </div>
    </MusicCard>
  );

  const renderHomeTab = () => (
    <div className="space-y-5">
      <MusicCard className="p-4">
        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_220px]">
          <div className="min-w-0">
            <div className="flex items-center gap-3">
              {musicUser ? (
                <img
                  src={withCoverSize(musicUser.avatarUrl, 120)}
                  className="h-12 w-12 rounded-md border border-kumo-line object-cover"
                  alt=""
                />
              ) : (
                <div className="flex h-12 w-12 items-center justify-center rounded-md border border-kumo-line bg-kumo-recessed">
                  <User className="h-5 w-5 text-kumo-subtle" />
                </div>
              )}
              <div className="min-w-0">
                <h1 className="truncate text-lg font-semibold text-kumo-strong">
                  {musicUser ? `${musicUser.nickname}，晚上好` : '网易云音乐'}
                </h1>
                <p className="mt-1 truncate text-xs text-kumo-subtle">
                  {musicUser ? '今日推荐、歌单和播放队列已放在同一工作台' : '登录后可以加载每日推荐和你的歌单'}
                </p>
              </div>
            </div>
          </div>

          <div className="grid grid-cols-3 gap-2 lg:grid-cols-1">
            <Badge variant={musicUser ? 'success' : 'secondary'} appearance={musicUser ? 'dot' : 'filled'}>
              {musicUser ? '已登录' : '未登录'}
            </Badge>
            <Badge variant="info">{musicPlaylist.length} 首队列</Badge>
            <Badge variant={musicPlaying ? 'success' : 'secondary'}>{musicPlaying ? '播放中' : '待播放'}</Badge>
          </div>
        </div>
      </MusicCard>

      <div>
        <SectionHeader
          title="每日推荐"
          description={musicUser ? '来自网易云的个性化推荐' : '需要登录后使用'}
          action={musicUser && (
            <Button
              type="button"
              variant="secondary" size="sm"
              loading={recommendLoading}
              icon={<RefreshCw className="h-3.5 w-3.5" />}
              onClick={musicLoadDailyRecommend}
            >
              刷新
            </Button>
          )}
        />
        {musicUser ? (
          <SongTable
            songs={dailyRecommend}
            loading={recommendLoading}
            emptyTitle="暂无每日推荐"
            emptyDescription="点击刷新重新拉取推荐歌曲"
            currentSong={musicCurrentSong}
            onPlay={musicPlay}
            compact
          />
        ) : (
          <EmptyState
            icon={User}
            title="登录后查看每日推荐"
            description="使用网易云音乐 App 扫码登录后，同步每日推荐和个人歌单。"
            action={(
              <Button
                type="button"
                variant="primary" size="sm"
                icon={<User className="h-3.5 w-3.5" />}
                onClick={() => useStore.setState({ musicShowLoginModal: true })}
              >
                扫码登录
              </Button>
            )}
          />
        )}
      </div>

      <div>
        <SectionHeader
          title="热门歌单"
          description="适合快速发现可播放内容"
          action={(
            <Button
              type="button"
              variant="secondary" size="sm"
              loading={playlistsLoading}
              icon={<RefreshCw className="h-3.5 w-3.5" />}
              onClick={musicLoadHotPlaylists}
            >
              换一批
            </Button>
          )}
        />
        <PlaylistGrid
          playlists={hotPlaylists.slice(0, 8)}
          loading={playlistsLoading}
          onOpen={musicLoadPlaylistDetail}
          emptyText="暂无热门歌单"
        />
      </div>
    </div>
  );

  const renderDiscoverTab = () => (
    <div>
      <SectionHeader
        title="发现歌单"
        description="网易云热门歌单，点击进入后可播放全部"
        action={(
          <Button
            type="button"
            variant="secondary" size="sm"
            loading={playlistsLoading}
            icon={<RefreshCw className="h-3.5 w-3.5" />}
            onClick={musicLoadHotPlaylists}
          >
            刷新
          </Button>
        )}
      />
      <PlaylistGrid playlists={hotPlaylists} loading={playlistsLoading} onOpen={musicLoadPlaylistDetail} />
    </div>
  );

  const renderLibraryTab = () => (
    <div className="space-y-5">
      {musicUser ? (
        <>
          <div>
            <SectionHeader title="我创建的歌单" description={`${musicMyPlaylists.length} 个歌单`} />
            <PlaylistGrid playlists={musicMyPlaylists} onOpen={musicLoadPlaylistDetail} emptyText="暂无创建歌单" />
          </div>
          <div>
            <SectionHeader title="收藏歌单" description={`${collectedPlaylists.length} 个歌单`} />
            <PlaylistGrid playlists={collectedPlaylists} onOpen={musicLoadPlaylistDetail} emptyText="暂无收藏歌单" />
          </div>
        </>
      ) : (
        <EmptyState
          icon={Library}
          title="登录后同步音乐库"
          description="登录网易云音乐后会显示你创建和收藏的歌单。"
          action={(
            <Button
              type="button"
              variant="primary" size="sm"
              icon={<User className="h-3.5 w-3.5" />}
              onClick={() => useStore.setState({ musicShowLoginModal: true })}
            >
              登录网易云
            </Button>
          )}
        />
      )}
    </div>
  );

  const renderSearchTab = () => (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <SectionHeader
          title={`搜索：${musicSearchKeyword || '未输入关键词'}`}
          description={`已加载 ${musicSearchOffset} 条结果`}
        />
        <Tabs
          {...TOOL_TABS_PROPS}
          value={musicSearchType}
          onValueChange={(value) => {
            useStore.setState({ musicSearchType: value });
            setTimeout(() => musicSearch(false), 0);
          }}
          className="w-full md:w-72"
          tabs={[
            { value: 'songs', label: '歌曲' },
            { value: 'playlists', label: '歌单' },
            { value: 'artists', label: '歌手' },
          ]}
        />
      </div>

      {musicSearchType === 'songs' && (
        <SongTable
          songs={musicSearchResults}
          loading={musicSearchLoading}
          emptyTitle="没有搜索到歌曲"
          emptyDescription="换个关键词再试试"
          currentSong={musicCurrentSong}
          onPlay={musicPlay}
          hasMore={musicSearchHasMore}
          loadingMore={musicSearchLoadingMore}
          onLoadMore={() => musicSearch(true)}
        />
      )}

      {musicSearchType === 'playlists' && (
        <>
          <PlaylistGrid playlists={musicSearchPlaylists} loading={musicSearchLoading} onOpen={musicLoadPlaylistDetail} emptyText="没有搜索到歌单" />
          {musicSearchHasMore && musicSearchPlaylists.length > 0 && (
            <div className="text-center">
              <Button type="button" variant="secondary" size="sm" loading={musicSearchLoadingMore} onClick={() => musicSearch(true)}>
                加载更多
              </Button>
            </div>
          )}
        </>
      )}

      {musicSearchType === 'artists' && (
        <MusicCard className="overflow-hidden">
          {musicSearchLoading ? (
            <div className="space-y-3 p-4">
              {Array.from({ length: 8 }).map((_, index) => <SkeletonLine key={index} className="h-12 w-full" />)}
            </div>
          ) : musicSearchArtists.length === 0 ? (
            <EmptyState icon={User} title="没有搜索到歌手" description="换个关键词再试试" />
          ) : (
            <Table layout="fixed">
              <colgroup>
                <col />
                <col className="w-28" />
              </colgroup>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head className="text-[10px]">歌手</Table.Head>
                  <Table.Head className="text-right text-[10px]">专辑数</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {musicSearchArtists.map((artist) => (
                  <Table.Row key={artist.id}>
                    <Table.Cell>
                      <div className="flex min-w-0 items-center gap-3">
                        <img
                          src={withCoverSize(artist.cover, 120)}
                          className="h-10 w-10 rounded-md border border-kumo-line object-cover bg-kumo-recessed"
                          alt=""
                        />
                        <div className="min-w-0">
                          <div className="truncate text-xs font-semibold text-kumo-strong">{artist.name}</div>
                          <div className="truncate text-[11px] text-kumo-subtle">{artist.alias || '歌手'}</div>
                        </div>
                      </div>
                    </Table.Cell>
                    <Table.Cell className="text-right text-xs tabular-nums text-kumo-subtle">
                      {artist.albumCount || 0}
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          )}
        </MusicCard>
      )}
    </div>
  );

  const renderPlaylistDetail = () => {
    if (playlistDetailLoading && !musicCurrentPlaylistDetail) {
      return (
        <MusicCard className="space-y-4 p-4">
          <SkeletonLine className="h-28 w-full" />
          <SkeletonLine className="h-10 w-full" />
          <SkeletonLine className="h-10 w-full" />
          <SkeletonLine className="h-10 w-full" />
        </MusicCard>
      );
    }

    if (!musicCurrentPlaylistDetail) return null;

    return (
      <div className="space-y-4">
        <MusicCard className="p-4">
          <div className="flex flex-col gap-4 md:flex-row md:items-end">
            <AlbumCover playlist={musicCurrentPlaylistDetail} size="xl" />
            <div className="min-w-0 flex-1">
              <div className="mb-2 flex flex-wrap items-center gap-2">
                <Badge variant="info">歌单</Badge>
                <Badge variant="secondary">{musicCurrentPlaylistDetail.trackCount || 0} 首</Badge>
              </div>
              <h1 className="truncate text-xl font-semibold text-kumo-strong">{musicCurrentPlaylistDetail.name}</h1>
              <p className="mt-1 truncate text-xs text-kumo-subtle">
                {musicCurrentPlaylistDetail.creator || '未知创建者'}
              </p>
              {musicCurrentPlaylistDetail.description && (
                <p className="mt-3 line-clamp-2 text-xs leading-5 text-kumo-subtle">
                  {musicCurrentPlaylistDetail.description}
                </p>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <Button
                type="button"
                variant="secondary" size="sm"
                icon={<ArrowLeft className="h-3.5 w-3.5" />}
                onClick={() => useStore.setState({ musicShowDetail: false })}
              >
                返回
              </Button>
              <Button
                type="button"
                variant="primary" size="sm"
                icon={<Play className="h-3.5 w-3.5" />}
                onClick={() => musicPlayPlaylist(musicCurrentPlaylistDetail.tracks)}
              >
                播放全部
              </Button>
            </div>
          </div>
        </MusicCard>

        <SongTable
          songs={musicCurrentPlaylistDetail.tracks}
          loading={false}
          currentSong={musicCurrentSong}
          onPlay={musicPlay}
          emptyTitle="歌单暂无歌曲"
        />
      </div>
    );
  };

  const renderMainContent = () => {
    if (musicShowDetail) return renderPlaylistDetail();
    if (musicCurrentTab === 'discover') return renderDiscoverTab();
    if (musicCurrentTab === 'library') return renderLibraryTab();
    if (musicCurrentTab === 'search') return renderSearchTab();
    return renderHomeTab();
  };

  const renderNowPlayingPanel = () => (
    <div className="grid min-h-0 gap-4">
      <MusicCard className="p-4">
        <SectionHeader
          title="正在播放"
          description={musicCurrentSong ? getSongArtists(musicCurrentSong) : '选择歌曲开始播放'}
          action={musicCurrentSong && (
            <Button
              type="button"
              variant="ghost" size="sm"
              shape="square"
              aria-label="打开播放页"
              icon={<Maximize2 className="h-3.5 w-3.5" />}
              onClick={() => useStore.setState({ musicShowFullPlayer: true })}
            />
          )}
        />

        {musicCurrentSong ? (
          <div className="space-y-4">
            <div className="flex items-center gap-3">
              <AlbumCover song={musicCurrentSong} size="lg" />
              <div className="min-w-0">
                <div className="truncate text-sm font-semibold text-kumo-strong">{musicCurrentSong.name}</div>
                <div className="mt-1 truncate text-xs text-kumo-subtle">{getSongAlbum(musicCurrentSong)}</div>
                <div className="mt-2 flex flex-wrap gap-2">
                  <Badge variant={musicPlaying ? 'success' : 'secondary'} appearance={musicPlaying ? 'dot' : 'filled'}>
                    {musicBuffering ? '缓冲中' : musicPlaying ? '播放中' : '暂停'}
                  </Badge>
                  <Badge variant="outline">{formatMusicSeconds(musicCurrentTime)} / {formatMusicSeconds(musicDuration)}</Badge>
                </div>
              </div>
            </div>

            <Meter
              label="播放进度"
              value={clampPercent(musicProgress)}
              min={0}
              max={100}
              customValue={`${Math.round(clampPercent(musicProgress))}%`}
            />

            <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3">
              <div className="text-[11px] font-semibold text-kumo-subtle">当前歌词</div>
              <div className="mt-2 min-h-10 text-sm font-semibold leading-5 text-kumo-strong">
                {musicCurrentLyricText || '暂无歌词'}
              </div>
              {musicCurrentLyricTranslation && (
                <div className="mt-1 text-xs leading-5 text-kumo-subtle">{musicCurrentLyricTranslation}</div>
              )}
            </div>
          </div>
        ) : (
          <EmptyState icon={Headphones} title="还没有播放内容" description="从搜索结果、歌单或每日推荐里选择一首歌。" />
        )}
      </MusicCard>

      <MusicCard className="min-h-0 p-4">
        <SectionHeader title="播放队列" description={`${musicPlaylist.length} 首歌曲`} />
        <div className="max-h-[42vh] min-h-0 space-y-1 overflow-auto pr-1">
          {musicPlaylist.length === 0 ? (
            <p className="rounded-md bg-kumo-recessed/35 px-3 py-6 text-center text-xs text-kumo-subtle">
              队列为空
            </p>
          ) : (
            musicPlaylist.map((song, index) => {
              const active = index === musicCurrentIndex;
              return (
                <Button
                  key={`${song.id}-${index}`}
                  type="button"
                  variant="ghost" size="sm"
                  className={`flex w-full min-w-0 items-center gap-2 px-2 py-2 text-left transition-colors ${
                    active ? 'text-kumo-strong' : ''
                  }`}
                  onClick={() => musicPlay(song)}
                >
                  <AlbumCover song={song} size="xs" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-xs font-semibold text-kumo-strong">{song.name}</span>
                    <span className="block truncate text-[11px] text-kumo-subtle">{getSongArtists(song)}</span>
                  </span>
                  {active && <Badge variant="success" appearance="dot">当前</Badge>}
                </Button>
              );
            })
          )}
        </div>
      </MusicCard>
    </div>
  );

  const renderMiniPlayer = () => (
    <MusicCard className="fixed inset-x-4 bottom-4 z-40 mx-auto max-w-[calc(100vw-2rem)] p-3 xl:left-[calc(var(--sidebar-width,0px)+1rem)]">
      <div className="grid gap-3 lg:grid-cols-[minmax(220px,320px)_minmax(280px,1fr)_220px] lg:items-center">
        <div className="flex min-w-0 items-center gap-3">
          <Button
            type="button"
            variant="ghost" size="sm"
            shape="square"
            aria-label="打开完整播放器"
            title="打开完整播放器"
            className="h-auto w-auto shrink-0 p-0"
            onClick={() => musicCurrentSong && useStore.setState({ musicShowFullPlayer: true })}
            disabled={!musicCurrentSong}
          >
            <AlbumCover song={musicCurrentSong} size="md" />
          </Button>
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold text-kumo-strong">{musicCurrentSong?.name || '未播放'}</div>
            <div className="truncate text-xs text-kumo-subtle">{musicCurrentSong ? getSongArtists(musicCurrentSong) : '选择歌曲开始播放'}</div>
          </div>
        </div>

        <div className="min-w-0">
          <div className="mb-1 flex items-center justify-center gap-2">
            <Button
              type="button"
              variant={musicShuffleEnabled ? 'primary' : 'ghost'} size="sm"
              shape="square"
              aria-label="随机播放"
              icon={<Shuffle className="h-3.5 w-3.5" />}
              onClick={() => useStore.setState({ musicShuffleEnabled: !musicShuffleEnabled })}
            />
            <Button
              type="button"
              variant="ghost" size="sm"
              shape="square"
              aria-label="上一首"
              icon={<SkipBack className="h-3.5 w-3.5" />}
              onClick={playPrevious}
            />
            <Button
              type="button"
              variant="primary" size="sm"
              shape="circle"
              aria-label={musicPlaying ? '暂停' : '播放'}
              loading={musicBuffering}
              icon={musicPlaying ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
              onClick={musicTogglePlay}
            />
            <Button
              type="button"
              variant="ghost" size="sm"
              shape="square"
              aria-label="下一首"
              icon={<SkipForward className="h-3.5 w-3.5" />}
              onClick={playNext}
            />
            <Button
              type="button"
              variant={musicRepeatMode !== 'none' ? 'primary' : 'ghost'} size="sm"
              shape="square"
              aria-label="循环模式"
              icon={musicRepeatMode === 'one' ? <Repeat1 className="h-3.5 w-3.5" /> : <Repeat className="h-3.5 w-3.5" />}
              onClick={cycleRepeatMode}
            />
          </div>

          <div className="grid grid-cols-[40px_minmax(0,1fr)_40px] items-center gap-2">
            <span className="text-right text-[11px] tabular-nums text-kumo-subtle">{formatMusicSeconds(musicCurrentTime)}</span>
            <KumoSlider
              label="播放进度"
              value={musicProgress}
              disabled={!musicCurrentSong || musicDuration <= 0}
              onValueChange={handleSeekChange}
              onValueCommitted={handleSeekCommitted}
            />
            <span className="text-[11px] tabular-nums text-kumo-subtle">{formatMusicSeconds(musicDuration)}</span>
          </div>
        </div>

        <div className="hidden min-w-0 items-center gap-2 lg:flex">
          <Button
            type="button"
            variant="ghost" size="sm"
            shape="square"
            aria-label={musicMuted ? '取消静音' : '静音'}
            icon={musicMuted || musicVolume === 0 ? <VolumeX className="h-3.5 w-3.5" /> : <Volume2 className="h-3.5 w-3.5" />}
            onClick={toggleMute}
          />
          <KumoSlider
            label="音量"
            value={musicMuted ? 0 : musicVolume}
            onValueChange={handleVolumeChange}
            onValueCommitted={handleVolumeChange}
          />
          <Button
            type="button"
            variant="ghost" size="sm"
            shape="square"
            aria-label="打开播放页"
            icon={<Maximize2 className="h-3.5 w-3.5" />}
            onClick={() => useStore.setState({ musicShowFullPlayer: true })}
          />
        </div>
      </div>
    </MusicCard>
  );

  const renderLoginDialog = () => (
    <Dialog.Root open={musicShowLoginModal} onOpenChange={(open) => useStore.setState({ musicShowLoginModal: open })}>
      <Dialog size="md" className="p-0">
        <div className="flex items-center justify-between border-b border-kumo-line px-4 py-3">
          <Dialog.Title className="text-sm font-semibold text-kumo-strong">网易云音乐登录</Dialog.Title>
          <Dialog.Close
            aria-label="关闭"
            render={(props) => (
              <Button
                {...props}
                type="button"
                variant="ghost" size="sm"
                shape="square"
                aria-label="关闭"
                icon={<X className="h-3.5 w-3.5" />}
              />
            )}
          />
        </div>

        <div className="p-5">
          {qrImg ? (
            <div className="flex flex-col items-center gap-4">
              <div className="app-card p-3">
                <img src={qrImg} className="h-52 w-52" alt="网易云音乐登录二维码" />
              </div>
              <div className="text-center">
                <div className="text-sm font-semibold text-kumo-strong">
                  {qrExpired ? '二维码已过期' : loginStatusText}
                </div>
                <div className="mt-1 text-xs text-kumo-subtle">
                  {qrChecking ? '正在等待扫码确认' : '点击刷新二维码重新登录'}
                </div>
              </div>
              <Button
                type="button"
                variant="secondary" size="sm"
                loading={loginLoading}
                icon={<RefreshCw className="h-3.5 w-3.5" />}
                onClick={musicGenerateLoginQr}
              >
                刷新二维码
              </Button>
            </div>
          ) : (
            <EmptyState
              icon={User}
              title="扫码登录网易云音乐"
              description="登录态会保存在后端数据库，用于读取每日推荐和个人歌单。"
              action={(
                <Button size="sm"
                  type="button"
                  variant="primary"
                  loading={loginLoading}
                  icon={<RefreshCw className="h-3.5 w-3.5" />}
                  onClick={musicGenerateLoginQr}
                >
                  获取二维码
                </Button>
              )}
            />
          )}
        </div>
      </Dialog>
    </Dialog.Root>
  );

  const renderFullPlayerDialog = () => (
    <Dialog.Root open={musicShowFullPlayer} onOpenChange={(open) => useStore.setState({ musicShowFullPlayer: open })}>
      <Dialog size="xl" className="max-h-[86vh] overflow-hidden p-0">
        <div className="flex items-center justify-between border-b border-kumo-line px-4 py-3">
          <div className="min-w-0">
            <Dialog.Title className="truncate text-sm font-semibold text-kumo-strong">
              {musicCurrentSong?.name || '播放详情'}
            </Dialog.Title>
            <p className="mt-0.5 truncate text-xs text-kumo-subtle">
              {musicCurrentSong ? getSongArtists(musicCurrentSong) : '暂无播放内容'}
            </p>
          </div>
          <Dialog.Close
            aria-label="关闭"
            render={(props) => (
              <Button
                {...props}
                type="button"
                variant="ghost" size="sm"
                shape="square"
                aria-label="关闭"
                icon={<X className="h-3.5 w-3.5" />}
              />
            )}
          />
        </div>

        <div className="grid max-h-[calc(86vh-56px)] gap-0 overflow-hidden lg:grid-cols-[320px_minmax(0,1fr)]">
          <div className="border-b border-kumo-line p-5 lg:border-b-0 lg:border-r">
            {musicCurrentSong ? (
              <div className="space-y-4">
                <img
                  src={withCoverSize(musicCurrentSong.cover, 800)}
                  className="aspect-square w-full rounded-lg border border-kumo-line object-cover bg-kumo-recessed"
                  alt=""
                />
                <div>
                  <h3 className="truncate text-lg font-semibold text-kumo-strong">{musicCurrentSong.name}</h3>
                  <p className="mt-1 truncate text-xs text-kumo-subtle">{getSongArtists(musicCurrentSong)}</p>
                </div>
                <div className="grid gap-2">
                  <KumoSlider
                    label="播放进度"
                    value={musicProgress}
                    disabled={!musicCurrentSong || musicDuration <= 0}
                    onValueChange={handleSeekChange}
                    onValueCommitted={handleSeekCommitted}
                  />
                  <div className="flex items-center justify-between text-[11px] tabular-nums text-kumo-subtle">
                    <span>{formatMusicSeconds(musicCurrentTime)}</span>
                    <span>{formatMusicSeconds(musicDuration)}</span>
                  </div>
                </div>
                <div className="flex items-center justify-center gap-2">
                  <Button type="button" variant="ghost" size="sm" shape="square" aria-label="上一首" icon={<SkipBack className="h-4 w-4" />} onClick={playPrevious} />
                  <Button
                    type="button"
                    variant="primary" size="sm"
                    shape="circle"
                    aria-label={musicPlaying ? '暂停' : '播放'}
                    loading={musicBuffering}
                    icon={musicPlaying ? <Pause className="h-5 w-5" /> : <Play className="h-5 w-5" />}
                    onClick={musicTogglePlay}
                  />
                  <Button type="button" variant="ghost" size="sm" shape="square" aria-label="下一首" icon={<SkipForward className="h-4 w-4" />} onClick={playNext} />
                </div>
              </div>
            ) : (
              <EmptyState icon={Disc3} title="暂无播放内容" description="选择歌曲后会在这里显示封面和控制器。" />
            )}
          </div>

          <div ref={lyricsScrollRef} className="min-h-[360px] overflow-auto p-6">
            {musicLyrics.length > 0 ? (
              <div className="space-y-5 pb-24">
                {musicLyrics.map((line, index) => {
                  const active = index === musicCurrentLyricIndex;
                  return (
                    <Button
                      key={`${line.time}-${index}`}
                      type="button"
                      data-active={active}
                      variant="ghost" size="sm"
                      aria-current={active ? 'true' : undefined}
                      className={`block h-auto w-full px-3 py-2 text-left transition-colors ${
                        active ? 'text-kumo-strong' : 'text-kumo-subtle'
                      }`}
                      onClick={() => {
                        if (!audioPlayerInstance) return;
                        audioPlayerInstance.currentTime = line.time / 1000;
                        if (!musicPlaying) audioPlayerInstance.play();
                      }}
                    >
                      <span className={`block leading-7 ${active ? 'text-lg font-semibold' : 'text-sm font-medium'}`}>
                        {line.text}
                      </span>
                      {line.trans && (
                        <span className="mt-1 block text-xs leading-5 text-kumo-subtle">{line.trans}</span>
                      )}
                    </Button>
                  );
                })}
              </div>
            ) : (
              <EmptyState icon={Music2} title="暂无歌词" description="部分歌曲可能没有同步歌词。" />
            )}
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );

  return (
    <div className="music-page-container flex h-full min-h-0 flex-col gap-4 pb-28">
      <MusicCard className="p-4">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <Music2 className="h-5 w-5 text-kumo-brand" />
                <h1 className="truncate text-lg font-semibold text-kumo-strong">音乐</h1>
              </div>
              <p className="mt-1 text-xs text-kumo-subtle">搜索、歌单、播放队列和歌词集中管理</p>
            </div>
            <Tabs
              {...MODULE_TABS_PROPS}
              value={musicCurrentTab}
              onValueChange={(value) => {
                useStore.setState({ musicCurrentTab: value, musicShowDetail: false });
                if (value === 'discover' && hotPlaylists.length === 0) musicLoadHotPlaylists();
              }}
              className="w-full sm:w-auto"
              tabs={[
                { value: 'home', label: <span className="inline-flex items-center gap-1.5"><Home className="h-3.5 w-3.5" />首页</span> },
                { value: 'discover', label: <span className="inline-flex items-center gap-1.5"><Compass className="h-3.5 w-3.5" />发现</span> },
                { value: 'library', label: <span className="inline-flex items-center gap-1.5"><Library className="h-3.5 w-3.5" />音乐库</span> },
                { value: 'search', label: <span className="inline-flex items-center gap-1.5"><Search className="h-3.5 w-3.5" />搜索</span> },
              ]}
            />
          </div>

          <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center">
            {musicShowDetail && (
              <Button
                type="button"
                variant="secondary" size="sm"
                icon={<ArrowLeft className="h-3.5 w-3.5" />}
                onClick={() => useStore.setState({ musicShowDetail: false })}
              >
                返回歌单
              </Button>
            )}

            <Autocomplete
              items={musicSuggestions}
              value={musicSearchKeyword}
              onValueChange={(value) => {
                useStore.setState({ musicSearchKeyword: value });
                fetchSuggestions(value);
              }}
              filter={null}
            >
              <div className="relative w-full sm:w-80">
                <Autocomplete.InputGroup
                  placeholder="搜索歌曲、歌单或歌手"
                  className="h-8.5 w-full pl-8 text-xs"
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') handleSearchSubmit();
                  }}
                />
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-kumo-subtle" />
              </div>
              <Autocomplete.Content className="z-50 mt-1 overflow-hidden app-card">
                <Autocomplete.List>
                  {musicSuggestions.map((item) => (
                    <Autocomplete.Item
                      key={item.keyword}
                      value={item.keyword}
                      className="cursor-pointer px-3 py-2 text-xs text-kumo-strong hover:bg-kumo-tint"
                      onClick={() => {
                        useStore.setState({
                          musicSearchKeyword: item.keyword,
                          musicCurrentTab: 'search',
                          musicShowDetail: false,
                        });
                        setTimeout(() => musicSearch(false), 0);
                      }}
                    >
                      {item.keyword}
                    </Autocomplete.Item>
                  ))}
                </Autocomplete.List>
              </Autocomplete.Content>
            </Autocomplete>

            <Button
              type="button"
              variant="primary" size="sm"
              icon={<Search className="h-3.5 w-3.5" />}
              onClick={handleSearchSubmit}
            >
              搜索
            </Button>
          </div>
        </div>
      </MusicCard>

      <div className="grid min-h-0 flex-1 gap-4 xl:grid-cols-[280px_minmax(0,1fr)_320px]">
        <aside className="hidden min-h-0 xl:block">
          {renderLibraryRail()}
        </aside>

        <main className="min-h-0 overflow-auto pr-1">
          {renderMainContent()}
        </main>

        <aside className="hidden min-h-0 xl:block">
          {renderNowPlayingPanel()}
        </aside>
      </div>

      <div className="xl:hidden">
        {renderNowPlayingPanel()}
      </div>

      {renderMiniPlayer()}
      {renderLoginDialog()}
      {renderFullPlayerDialog()}
    </div>
  );
}

export default MusicPage;
