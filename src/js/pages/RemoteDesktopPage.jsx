import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { ChevronUp, Cursor, DesktopDisplay, Maximize2, Menu, RefreshCw, X } from '../components/Icons.jsx';
import {
  TOUCH_LONG_PRESS_MS,
  TOUCH_PINCH_SLOP,
  TOUCH_SCROLL_SLOP,
  TOUCH_TAP_MAX_MS,
  TOUCH_TAP_SLOP,
  consumeScrollDelta,
  initialRemoteDesktopProfile,
  isDoubleTap,
  nextPinchTransform,
  nextRemoteDesktopProfile,
  normalizedTrackpadDelta,
  normalizedVideoPoint,
  pointDistance,
  remoteCursorPoint,
  trackpadButtonMessage,
  trackpadPixelDelta,
} from '../modules/remoteDesktopTouch.js';

const ICE_SERVERS = [
  { urls: ['stun:stun.cloudflare.com:3478', 'stun:stun.l.google.com:19302'] },
];
const SIGNAL_POLL_MS = 300;

function serverIdFromPath() {
  const match = window.location.pathname.match(/^\/remote-desktop\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
}

function authHeaders(json = false) {
  return {
    ...(json ? { 'Content-Type': 'application/json' } : {}),
  };
}

async function apiRequest(url, options = {}) {
  const response = await fetch(url, options);
  const payload = await response.json().catch(() => ({}));
  if (!response.ok || payload.success === false) {
    const error = new Error(payload.error || `请求失败 (${response.status})`);
    error.status = response.status;
    throw error;
  }
  return payload.data ?? payload;
}

function stateLabel(state) {
  const labels = {
    initializing: '正在初始化',
    connecting: '正在打洞',
    signaling: '正在协商',
    connected: 'P2P 已直连',
    disconnected: '连接中断',
    failed: '直连失败',
    closed: '会话已结束',
    error: '连接错误',
  };
  return labels[state] || state;
}

// Map a WebRTC candidate type to a user-meaningful label. `host` means a direct
// LAN path (bypasses proxies/TUN); `srflx` is a public-IP hole-punch; `relay`
// goes through a TURN server. Knowing which one is active helps diagnose why a
// proxied/TUN machine cannot connect.
function candidateTypeLabel(candidate) {
  const parts = String(candidate || '').split(' · ');
  const type = (parts[0] || '').trim();
  const protocol = (parts[1] || '').trim();
  const typeLabel = {
    host: '同网直连',
    srflx: '公网打洞',
    prflx: '双向打洞',
    relay: '中继转发',
  }[type] || type || '';
  const protoLabel = protocol === 'tcp' ? 'TCP' : 'UDP';
  return `${typeLabel}${protoLabel ? `(${protoLabel})` : ''}`;
}

export default function RemoteDesktopPage() {
  const serverId = useMemo(serverIdFromPath, []);
  const [serverName, setServerName] = useState(serverId);
  const [state, setState] = useState('initializing');
  const [error, setError] = useState('');
  const [videoReady, setVideoReady] = useState(false);
  const [fillMode, setFillMode] = useState('contain');
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [fullscreenToolbarOpen, setFullscreenToolbarOpen] = useState(false);
  const [virtualCursor, setVirtualCursor] = useState({ x: 0.5, y: 0.5, visible: false });
  const [viewTransform, setViewTransform] = useState({ scale: 1, x: 0, y: 0 });
  const [surfaceSize, setSurfaceSize] = useState({ width: 1, height: 1 });
  const [controlEnabled, setControlEnabled] = useState(true);
  const [touchInputMode, setTouchInputMode] = useState('trackpad');
  const [clipboardSync, setClipboardSync] = useState(true);
  const clipboardSyncRef = useRef(true);
  clipboardSyncRef.current = clipboardSync;
  const [controlAcknowledged, setControlAcknowledged] = useState(false);
  const [stats, setStats] = useState({
    rtt: 0,
    local: '',
    remote: '',
    localLabel: '',
    remoteLabel: '',
    fps: 0,
    receivedFps: 0,
    droppedFps: 0,
    loss: 0,
    bufferMs: 0,
    bitrate: 0,
  });

  const desktopAreaRef = useRef(null);
  const surfaceRef = useRef(null);
  const videoRef = useRef(null);
  const peerRef = useRef(null);
  const channelRef = useRef(null);
  const pointerChannelRef = useRef(null);
  const sessionRef = useRef('');
  const stoppedRef = useRef(false);
  const connectionGenerationRef = useRef(0);
  const autoReconnectRef = useRef(0);
  const lastSignalRef = useRef(0);
  const pendingLocalIceRef = useRef([]);
  const pendingRemoteIceRef = useRef([]);
  const previousVideoStatsRef = useRef(null);
  const coarsePointerRef = useRef(
    Boolean(window.matchMedia?.('(pointer: coarse)').matches || navigator.maxTouchPoints > 0),
  );
  const streamProfileRef = useRef(initialRemoteDesktopProfile(coarsePointerRef.current));
  const healthyIntervalsRef = useRef(0);
  const pointerFrameRef = useRef(0);
  const absolutePointerFrameRef = useRef(0);
  const pendingAbsolutePointerRef = useRef(null);
  const pendingRelativePointerRef = useRef({ x: 0, y: 0 });
  const pointerSequenceRef = useRef(0);
  const lastPointerAckRef = useRef(0);
  const cursorPositionRef = useRef({ x: 0.5, y: 0.5 });
  const touchGestureRef = useRef(null);
  const lastTapRef = useRef(null);
  const longPressTimerRef = useRef(0);
  const ignoreMouseUntilRef = useRef(0);
  const viewTransformRef = useRef(viewTransform);
  const remoteInputRef = useRef(null);
  const remoteInputValueRef = useRef('');
  const lastSentClipboardRef = useRef('');
  const lastReceivedClipboardRef = useRef('');
  const skipAutoReconnectRef = useRef(false);

  const sendControl = useCallback((payload, { reliable = false } = {}) => {
    const highFrequency = payload.type === 'pointer'
      || payload.type === 'pointer-relative'
      || payload.type === 'wheel'
      || ((payload.type === 'pointer-contact' || payload.type === 'touch-contact') && payload.action === 'move');
    const fastChannel = pointerChannelRef.current;
    const channel = !reliable && highFrequency && fastChannel?.readyState === 'open'
      ? fastChannel
      : channelRef.current;
    if (!controlEnabled || channel?.readyState !== 'open') return false;
    if (highFrequency && channel.bufferedAmount > 16 * 1024) return false;
    channel.send(JSON.stringify(payload));
    return true;
  }, [controlEnabled]);

  const requestPointerPosition = useCallback(() => {
    pointerSequenceRef.current = (pointerSequenceRef.current + 1) >>> 0;
    sendControl({
      type: 'pointer-query',
      sequence: pointerSequenceRef.current,
    }, { reliable: true });
  }, [sendControl]);

  const updateViewTransform = useCallback((next) => {
    viewTransformRef.current = next;
    setViewTransform(next);
  }, []);

  const resetViewTransform = useCallback(() => {
    updateViewTransform({ scale: 1, x: 0, y: 0 });
  }, [updateViewTransform]);

  const clearLongPress = useCallback(() => {
    if (longPressTimerRef.current) window.clearTimeout(longPressTimerRef.current);
    longPressTimerRef.current = 0;
  }, []);

  const postSignal = useCallback(async (signal, peer, generation) => {
    if (peer !== peerRef.current || generation !== connectionGenerationRef.current || stoppedRef.current) return;
    const sessionId = sessionRef.current;
    if (!sessionId) {
      pendingLocalIceRef.current.push(signal);
      return;
    }
    await apiRequest(`/api/server/remote-desktop/sessions/${encodeURIComponent(sessionId)}/signals`, {
      method: 'POST',
      headers: authHeaders(true),
      body: JSON.stringify({ signal }),
    });
  }, []);

  const bindChannel = useCallback((channel, peer, generation) => {
    channelRef.current = channel;
    channel.onopen = () => {
      if (peer !== peerRef.current || generation !== connectionGenerationRef.current) return;
      setState('connected');
      setError('');
      surfaceRef.current?.focus();
      channel.send(JSON.stringify({ type: 'video-config', ...streamProfileRef.current }));
    };
    channel.onclose = () => {
      if (peer === peerRef.current && generation === connectionGenerationRef.current) setState('closed');
    };
    channel.onerror = () => {
      if (peer !== peerRef.current || generation !== connectionGenerationRef.current) return;
      setState('failed');
      setError('P2P 数据通道异常。网络可能存在对称 NAT、CGNAT 或 UDP 防火墙。');
    };
  }, []);

  const applyRemoteSignal = useCallback(async (signal, sessionId, generation) => {
    if (sessionRef.current !== sessionId || generation !== connectionGenerationRef.current) return;
    const peer = peerRef.current;
    if (!peer || !signal) return;
    if (signal.kind === 'answer') {
      await peer.setRemoteDescription(signal.sdp);
      for (const candidate of pendingRemoteIceRef.current.splice(0)) {
        await peer.addIceCandidate(candidate).catch(() => {});
      }
    } else if (signal.kind === 'ice' && signal.candidate) {
      if (peer.remoteDescription) await peer.addIceCandidate(signal.candidate).catch(() => {});
      else pendingRemoteIceRef.current.push(signal.candidate);
    } else if (signal.kind === 'error') {
      setState('error');
      setError(signal.message || 'Windows Agent 启动远程桌面失败');
    }
  }, []);

  const pollSignals = useCallback(async (sessionId, generation, longPoll = false) => {
    if (!sessionId || stoppedRef.current || sessionRef.current !== sessionId || generation !== connectionGenerationRef.current) return;
    try {
      const data = await apiRequest(`/api/server/remote-desktop/sessions/${encodeURIComponent(sessionId)}/signals?since=${lastSignalRef.current}&wait=${longPoll ? 15000 : 0}`, {
        headers: authHeaders(),
        cache: 'no-store',
      });
      if (sessionRef.current !== sessionId || generation !== connectionGenerationRef.current) return;
      if (data.state) setState(data.state);
      for (const item of data.signals || []) {
        if (sessionRef.current !== sessionId || generation !== connectionGenerationRef.current) return;
        lastSignalRef.current = Math.max(lastSignalRef.current, Number(item.id) || 0);
        await applyRemoteSignal(item.payload, sessionId, generation);
      }
    } catch (err) {
      if (!stoppedRef.current && sessionRef.current === sessionId && generation === connectionGenerationRef.current) {
        setError(err.message || '信令同步失败');
      }
    }
  }, [applyRemoteSignal]);

  const closeSession = useCallback(async () => {
    connectionGenerationRef.current += 1;
    stoppedRef.current = true;
    autoReconnectRef.current = 0;
    const sessionId = sessionRef.current;
    sessionRef.current = '';
    channelRef.current?.close?.();
    pointerChannelRef.current?.close?.();
    peerRef.current?.close?.();
    channelRef.current = null;
    pointerChannelRef.current = null;
    peerRef.current = null;
    previousVideoStatsRef.current = null;
    pendingAbsolutePointerRef.current = null;
    pendingRelativePointerRef.current = { x: 0, y: 0 };
    pointerSequenceRef.current = 0;
    lastPointerAckRef.current = 0;
    if (videoRef.current) videoRef.current.srcObject = null;
    setVideoReady(false);
    if (sessionId) {
      await fetch(`/api/server/remote-desktop/sessions/${encodeURIComponent(sessionId)}`, {
        method: 'DELETE',
        headers: authHeaders(),
        keepalive: true,
      }).catch(() => {});
    }
  }, []);

  // Retry a failed P2P attempt after a short backoff. Under a global proxy /
  // TUN the first hole-punch often fails while later attempts (after ICE has
  // gathered more candidates) succeed. Keep retrying (with capped attempts)
  // so transient link changes do not dead-end the session.
  const scheduleAutoReconnect = useCallback(() => {
    if (stoppedRef.current) return;
    if (autoReconnectRef.current >= 8) return;
    autoReconnectRef.current += 1;
    const attempt = autoReconnectRef.current;
    window.setTimeout(() => {
      if (stoppedRef.current || attempt !== autoReconnectRef.current) return;
      connect();
    }, 500 * attempt);
  }, [connect]);

  const connect = useCallback(async () => {
    await closeSession();
    const generation = connectionGenerationRef.current;
    stoppedRef.current = false;
    skipAutoReconnectRef.current = false;
    setState('initializing');
    setError('');
    setControlAcknowledged(false);
    streamProfileRef.current = initialRemoteDesktopProfile(coarsePointerRef.current);
    healthyIntervalsRef.current = 0;
    lastSignalRef.current = 0;
    pendingLocalIceRef.current = [];
    pendingRemoteIceRef.current = [];
    autoReconnectRef.current = 0;
    try {
      const server = await apiRequest(`/api/server/s/${encodeURIComponent(serverId)}`, { headers: authHeaders(), cache: 'no-store' });
      if (generation !== connectionGenerationRef.current) return;
      setServerName(server.name || server.hostname || serverId);
      const peer = new RTCPeerConnection({ iceServers: ICE_SERVERS, iceCandidatePoolSize: 4 });
      peerRef.current = peer;
      peer.onconnectionstatechange = () => {
        if (peer !== peerRef.current || generation !== connectionGenerationRef.current) return;
        const next = peer.connectionState;
        if (next === 'connected') {
          setState('connected');
          autoReconnectRef.current = 0;
        } else if (next === 'failed') {
          setState('failed');
          scheduleAutoReconnect();
        } else if (next === 'disconnected') {
          setState('disconnected');
          // ICE may recover on its own after a brief probe; only auto-reconnect
          // on the hard `failed` state to avoid thundering reconnect loops.
        } else if (next === 'closed') {
          setState('closed');
        }
      };
      peer.onicecandidate = (event) => {
        if (event.candidate) postSignal({ kind: 'ice', candidate: event.candidate.toJSON() }, peer, generation).catch(() => {});
      };
      peer.ontrack = (event) => {
        if (peer !== peerRef.current || generation !== connectionGenerationRef.current || event.track.kind !== 'video') return;
        const receiver = event.receiver;
        try {
          if ('playoutDelayHint' in receiver) receiver.playoutDelayHint = 0;
          if ('jitterBufferTarget' in receiver) receiver.jitterBufferTarget = 0;
        } catch {
          // Some mobile browsers expose these experimental hints as read-only.
        }
        const stream = event.streams[0] || new MediaStream([event.track]);
        if (videoRef.current) {
          videoRef.current.srcObject = stream;
          videoRef.current.play().catch(() => {});
        }
      };
      peer.addTransceiver('video', { direction: 'recvonly' });
      const channel = peer.createDataChannel('remote-desktop', { ordered: true });
      bindChannel(channel, peer, generation);
      pointerChannelRef.current = peer.createDataChannel('remote-pointer', { ordered: false, maxRetransmits: 0 });
      const handleControlMessage = (event) => {
        if (peer !== peerRef.current || generation !== connectionGenerationRef.current) return;
        if (typeof event.data === 'string') {
          try {
            const meta = JSON.parse(event.data);
            if (meta.type === 'input-ack') setControlAcknowledged(true);
            if (meta.type === 'clipboard') {
              const text = String(meta.text || '');
              lastReceivedClipboardRef.current = text;
              lastSentClipboardRef.current = text;
              // 自动同步关闭时不覆盖本地剪贴板（保护本地图片/富文本内容）；
              // 「剪贴板」按钮的本地->远程方向始终可用。
              if (clipboardSyncRef.current) {
                navigator.clipboard?.writeText?.(text).catch(() => {});
              }
            }
            if (meta.type === 'pointer-position' && Number(meta.sequence || 0) >= lastPointerAckRef.current) {
              lastPointerAckRef.current = Number(meta.sequence || 0);
              const position = {
                x: Math.max(0, Math.min(1, Number(meta.x || 0))),
                y: Math.max(0, Math.min(1, Number(meta.y || 0))),
              };
              cursorPositionRef.current = position;
              setVirtualCursor({ ...position, visible: true });
            }
          } catch {
            // Ignore unknown control messages.
          }
        }
      };
      channel.onmessage = handleControlMessage;
      pointerChannelRef.current.onmessage = handleControlMessage;
      const offer = await peer.createOffer();
      await peer.setLocalDescription(offer);
      setState('connecting');
      const created = await apiRequest('/api/server/remote-desktop/sessions', {
        method: 'POST',
        headers: authHeaders(true),
        body: JSON.stringify({ serverId, offer: peer.localDescription }),
      });
      if (generation !== connectionGenerationRef.current || peer !== peerRef.current) {
        await fetch(`/api/server/remote-desktop/sessions/${encodeURIComponent(created.sessionId)}`, {
          method: 'DELETE', headers: authHeaders(), keepalive: true,
        }).catch(() => {});
        return;
      }
      sessionRef.current = created.sessionId;
      for (const signal of pendingLocalIceRef.current.splice(0)) await postSignal(signal, peer, generation);
      await pollSignals(created.sessionId, generation);
    } catch (err) {
      if (generation === connectionGenerationRef.current) {
        setState('error');
        setError(err.message || '远程桌面初始化失败');
        // 4xx 表示请求本身被拒绝（Agent 离线 / 不支持等），重试不会改变
        // 结果；网络类错误仍在可恢复范围内，保留自动重连。
        if (err.status === 400 || err.status === 409) {
          skipAutoReconnectRef.current = true;
        }
      }
    }
  }, [bindChannel, closeSession, pollSignals, postSignal, serverId]);

  useEffect(() => {
    connect();
    let signalLoopCancelled = false;
    const runSignalLoop = async () => {
      while (!signalLoopCancelled) {
        const sessionId = sessionRef.current;
        const generation = connectionGenerationRef.current;
        if (!sessionId) {
          await new Promise(resolve => window.setTimeout(resolve, SIGNAL_POLL_MS));
          continue;
        }
        await pollSignals(sessionId, generation, true);
      }
    };
    runSignalLoop();
    const statsTimer = window.setInterval(async () => {
      const peer = peerRef.current;
      if (!peer) return;
      const reports = await peer.getStats().catch(() => null);
      if (!reports) return;
      let pair;
      let localCandidate;
      let remoteCandidate;
      let video;
      reports.forEach((report) => {
        if (report.type === 'candidate-pair' && (report.selected || report.nominated) && report.state === 'succeeded') pair = report;
        if (report.type === 'inbound-rtp' && report.kind === 'video' && !report.isRemote) video = report;
      });
      if (pair) {
        localCandidate = reports.get(pair.localCandidateId);
        remoteCandidate = reports.get(pair.remoteCandidateId);
      }
      const now = performance.now();
      const previous = previousVideoStatsRef.current;
      const elapsedSeconds = previous ? Math.max(0.001, (now - previous.at) / 1000) : 0;
      const framesReceivedDelta = previous ? Math.max(0, Number(video?.framesReceived || 0) - previous.framesReceived) : 0;
      const decodedDelta = previous ? Math.max(0, Number(video?.framesDecoded || 0) - previous.framesDecoded) : 0;
      const droppedDelta = previous ? Math.max(0, Number(video?.framesDropped || 0) - previous.framesDropped) : 0;
      const bytesDelta = previous ? Math.max(0, Number(video?.bytesReceived || 0) - previous.bytesReceived) : 0;
      const receivedDelta = previous ? Math.max(0, Number(video?.packetsReceived || 0) - previous.packetsReceived) : 0;
      const lostDelta = previous ? Math.max(0, Number(video?.packetsLost || 0) - previous.packetsLost) : 0;
      const jitterCountDelta = previous ? Math.max(0, Number(video?.jitterBufferEmittedCount || 0) - previous.jitterCount) : 0;
      const jitterDelayDelta = previous ? Math.max(0, Number(video?.jitterBufferDelay || 0) - previous.jitterDelay) : 0;
      previousVideoStatsRef.current = video ? {
        at: now,
        framesReceived: Number(video.framesReceived || 0),
        framesDecoded: Number(video.framesDecoded || 0),
        framesDropped: Number(video.framesDropped || 0),
        bytesReceived: Number(video.bytesReceived || 0),
        packetsReceived: Number(video.packetsReceived || 0),
        packetsLost: Number(video.packetsLost || 0),
        jitterCount: Number(video.jitterBufferEmittedCount || 0),
        jitterDelay: Number(video.jitterBufferDelay || 0),
      } : null;
      const measuredFps = Number(video?.framesPerSecond || (elapsedSeconds ? decodedDelta / elapsedSeconds : 0));
      const measuredReceivedFps = elapsedSeconds ? framesReceivedDelta / elapsedSeconds : 0;
      const measuredDroppedFps = elapsedSeconds ? droppedDelta / elapsedSeconds : 0;
      const measuredLoss = receivedDelta + lostDelta ? (lostDelta / (receivedDelta + lostDelta)) * 100 : 0;
      const measuredBufferMs = jitterCountDelta ? (jitterDelayDelta / jitterCountDelta) * 1000 : 0;
      const measuredRtt = Math.round(Number(pair?.currentRoundTripTime || 0) * 1000);
      const videoPixels = (videoRef.current?.videoWidth || 1920) * (videoRef.current?.videoHeight || 1080);
      const nativeBitrate = videoPixels > 3_686_400 ? 28_000_000 : videoPixels > 2_073_600 ? 18_000_000 : 12_000_000;
      const adaptation = nextRemoteDesktopProfile({
        loss: measuredLoss,
        rtt: measuredRtt,
        bufferMs: measuredBufferMs,
        droppedFps: measuredDroppedFps,
        nativeBitrate,
        healthyIntervals: healthyIntervalsRef.current,
        current: streamProfileRef.current,
        coarsePointer: coarsePointerRef.current,
      });
      const nextProfile = adaptation.profile;
      healthyIntervalsRef.current = adaptation.healthyIntervals;
      const currentProfile = streamProfileRef.current;
      if (nextProfile.fps !== currentProfile.fps || nextProfile.bitrate !== currentProfile.bitrate) {
        streamProfileRef.current = nextProfile;
        const controlChannel = channelRef.current;
        if (controlChannel?.readyState === 'open') {
          controlChannel.send(JSON.stringify({ type: 'video-config', ...nextProfile }));
        }
      }
      setStats({
        rtt: measuredRtt,
        local: localCandidate ? `${localCandidate.candidateType || 'host'} · ${localCandidate.protocol || 'udp'}` : '',
        remote: remoteCandidate ? `${remoteCandidate.candidateType || 'host'} · ${remoteCandidate.protocol || 'udp'}` : '',
        localLabel: candidateTypeLabel(localCandidate ? `${localCandidate.candidateType || 'host'} · ${localCandidate.protocol || 'udp'}` : ''),
        remoteLabel: candidateTypeLabel(remoteCandidate ? `${remoteCandidate.candidateType || 'host'} · ${remoteCandidate.protocol || 'udp'}` : ''),
        fps: measuredFps,
        receivedFps: measuredReceivedFps,
        droppedFps: measuredDroppedFps,
        loss: measuredLoss,
        bufferMs: measuredBufferMs,
        bitrate: elapsedSeconds ? (bytesDelta * 8) / elapsedSeconds : 0,
      });
    }, 2000);
    return () => {
      signalLoopCancelled = true;
      window.clearInterval(statsTimer);
      closeSession();
    };
  }, [closeSession, connect, pollSignals]);

  useEffect(() => {
    const syncFullscreen = () => {
      setIsFullscreen(document.fullscreenElement === desktopAreaRef.current);
      setFullscreenToolbarOpen(false);
    };
    document.addEventListener('fullscreenchange', syncFullscreen);
    return () => document.removeEventListener('fullscreenchange', syncFullscreen);
  }, []);

  useEffect(() => {
    const surface = surfaceRef.current;
    if (!surface) return undefined;
    const updateSize = () => {
      const rect = surface.getBoundingClientRect();
      setSurfaceSize({ width: Math.max(1, rect.width), height: Math.max(1, rect.height) });
    };
    updateSize();
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(updateSize) : null;
    observer?.observe(surface);
    window.addEventListener('resize', updateSize);
    return () => {
      observer?.disconnect();
      window.removeEventListener('resize', updateSize);
    };
  }, []);

  useEffect(() => () => {
    clearLongPress();
    if (pointerFrameRef.current) window.cancelAnimationFrame(pointerFrameRef.current);
    if (absolutePointerFrameRef.current) window.cancelAnimationFrame(absolutePointerFrameRef.current);
  }, [clearLongPress]);

  useEffect(() => {
    const closeOnPageHide = () => {
      closeSession();
    };
    window.addEventListener('pagehide', closeOnPageHide);
    return () => window.removeEventListener('pagehide', closeOnPageHide);
  }, [closeSession]);

  useEffect(() => {
    const handleKey = (event, action) => {
      if (!controlEnabled || document.activeElement !== surfaceRef.current) return;
      event.preventDefault();
      sendControl({ type: 'key', key: event.key, code: event.code, action });
    };
    const keyDown = (event) => handleKey(event, 'down');
    const keyUp = (event) => handleKey(event, 'up');
    window.addEventListener('keydown', keyDown, true);
    window.addEventListener('keyup', keyUp, true);
    return () => {
      window.removeEventListener('keydown', keyDown, true);
      window.removeEventListener('keyup', keyUp, true);
    };
  }, [controlEnabled, sendControl]);

  useEffect(() => {
    if (!videoReady) return undefined;
    const syncCursor = () => requestPointerPosition();
    syncCursor();
    // The Agent no longer burns the system cursor into the captured frames
    // (single-cursor layer). Poll the real remote pointer position so the
    // virtual cursor stays visible and tracks movements made outside this tab.
    const timer = window.setInterval(syncCursor, 2000);
    return () => window.clearInterval(timer);
  }, [requestPointerPosition, videoReady]);

  const pointerPosition = (event, clampOutside = false) => {
    const rect = surfaceRef.current?.getBoundingClientRect();
    if (!rect) return null;
    const video = videoRef.current;
    return normalizedVideoPoint(
      event,
      rect,
      {
        width: video?.videoWidth || rect.width,
        height: video?.videoHeight || rect.height,
      },
      fillMode,
      viewTransformRef.current,
      clampOutside,
    );
  };

  const flushAbsolutePointer = () => {
    if (absolutePointerFrameRef.current) {
      window.cancelAnimationFrame(absolutePointerFrameRef.current);
      absolutePointerFrameRef.current = 0;
    }
    const latest = pendingAbsolutePointerRef.current;
    pendingAbsolutePointerRef.current = null;
    if (latest) sendControl({ type: 'pointer', ...latest });
  };

  const scheduleAbsolutePointer = (position) => {
    cursorPositionRef.current = position;
    setVirtualCursor({ ...position, visible: true });
    pendingAbsolutePointerRef.current = position;
    if (absolutePointerFrameRef.current) return;
    absolutePointerFrameRef.current = window.requestAnimationFrame(() => {
      absolutePointerFrameRef.current = 0;
      flushAbsolutePointer();
    });
  };

  const sendPointerContact = (position, action, button = 0) => {
    if (!position) return false;
    if (absolutePointerFrameRef.current) {
      window.cancelAnimationFrame(absolutePointerFrameRef.current);
      absolutePointerFrameRef.current = 0;
      pendingAbsolutePointerRef.current = null;
    }
    cursorPositionRef.current = position;
    setVirtualCursor({ ...position, visible: true });
    return sendControl({ type: 'pointer-contact', ...position, button, action }, { reliable: true });
  };

  const sendTrackpadButton = (action, button = 0) => (
    sendControl(trackpadButtonMessage(action, button), { reliable: true })
  );

  // M4: 任何失败终态（Agent 端显式报错、通道关闭、ICE 失败）都会通过 state
  // 变化触发自动重连；成功路径不会反复触发，手动关闭由 stoppedRef 阻断。
  // 每次 connect() 重置 autoReconnectRef，所以多数失败都能按退避序列收敛。
  // 确定性请求错误（Agent 离线 / 能力不足等 4xx）由 skipAutoReconnectRef
  // 标记后跳过重试，避免对必然失败的请求做无意义洪泛。
  useEffect(() => {
    if ((state === 'closed' || state === 'error' || state === 'failed') && !skipAutoReconnectRef.current) {
      scheduleAutoReconnect();
    }
  }, [state, scheduleAutoReconnect]);

  const sendLocalClipboard = async () => {
    if (!navigator.clipboard?.readText) {
      setError('浏览器无法读取剪贴板（需要 HTTPS 或 localhost）');
      return;
    }
    try {
      const text = await navigator.clipboard.readText();
      if (!text) {
        setError('本地剪贴板为空');
        return;
      }
      const sent = sendControl({ type: 'clipboard-set', text }, { reliable: true });
      if (!sent) {
        setError('控制通道未建立，剪贴板未发送');
        return;
      }
      lastSentClipboardRef.current = text;
      setError('');
    } catch {
      setError('读取本地剪贴板失败');
    }
  };

  // S1: 本地复制/剪切后自动把剪贴板文本推到远程（DataChannel 直连，不过服务器）。
  useEffect(() => {
    if (!navigator.clipboard?.readText || !clipboardSync) return undefined;
    const syncLocalCopy = () => {
      window.setTimeout(async () => {
        if (stoppedRef.current) return;
        try {
          const text = await navigator.clipboard.readText();
          if (!text
            || text === lastSentClipboardRef.current
            || text === lastReceivedClipboardRef.current) return;
          const sent = sendControl({ type: 'clipboard-set', text }, { reliable: true });
          if (sent) lastSentClipboardRef.current = text;
        } catch {
          // 剪贴板权限被拒时静默（按钮路径会给出明确报错）。
        }
      }, 0);
    };
    document.addEventListener('copy', syncLocalCopy);
    document.addEventListener('cut', syncLocalCopy);
    return () => {
      document.removeEventListener('copy', syncLocalCopy);
      document.removeEventListener('cut', syncLocalCopy);
    };
  }, [clipboardSync, sendControl]);

  const sendTouchContact = (position, action) => {
    if (!position) return false;
    if (absolutePointerFrameRef.current) {
      window.cancelAnimationFrame(absolutePointerFrameRef.current);
      absolutePointerFrameRef.current = 0;
      pendingAbsolutePointerRef.current = null;
    }
    cursorPositionRef.current = position;
    setVirtualCursor({ ...position, visible: true });
    return sendControl({ type: 'touch-contact', ...position, action }, { reliable: true });
  };

  const handlePointerMove = (event) => {
    if (performance.now() < ignoreMouseUntilRef.current) return;
    const position = pointerPosition(event);
    if (position) scheduleAbsolutePointer(position);
  };

  const handleMouse = (event, action) => {
    if (performance.now() < ignoreMouseUntilRef.current) return;
    event.preventDefault();
    surfaceRef.current?.focus();
    const position = pointerPosition(event);
    if (position) sendPointerContact(position, action, event.button);
  };

  const flushRelativePointer = () => {
    if (pointerFrameRef.current) {
      window.cancelAnimationFrame(pointerFrameRef.current);
      pointerFrameRef.current = 0;
    }
    const pending = pendingRelativePointerRef.current;
    pendingRelativePointerRef.current = { x: 0, y: 0 };
    const dx = Math.round(pending.x);
    const dy = Math.round(pending.y);
    if (!dx && !dy) return;
    const latest = cursorPositionRef.current;
    setVirtualCursor({ ...latest, visible: true });
    sendControl({
      type: 'pointer-relative',
      dx,
      dy,
    }, { reliable: true });
  };

  const moveRelativePointer = (deltaX, deltaY, elapsedMs = 16) => {
    const rect = surfaceRef.current?.getBoundingClientRect();
    if (!rect) return;
    const normalizedDelta = normalizedTrackpadDelta(
      deltaX,
      deltaY,
      elapsedMs,
      rect,
      {
        width: videoRef.current?.videoWidth || rect.width,
        height: videoRef.current?.videoHeight || rect.height,
      },
    );
    const pixelDelta = trackpadPixelDelta(deltaX, deltaY, elapsedMs);
    const current = cursorPositionRef.current;
    const next = {
      x: Math.max(0, Math.min(1, current.x + normalizedDelta.x)),
      y: Math.max(0, Math.min(1, current.y + normalizedDelta.y)),
    };
    cursorPositionRef.current = next;
    pendingRelativePointerRef.current.x += pixelDelta.x;
    pendingRelativePointerRef.current.y += pixelDelta.y;
    if (pointerFrameRef.current) return;
    pointerFrameRef.current = window.requestAnimationFrame(() => {
      pointerFrameRef.current = 0;
      flushRelativePointer();
    });
  };

  const touchCenter = (touches) => ({
    x: (touches[0].clientX + touches[1].clientX) / 2,
    y: (touches[0].clientY + touches[1].clientY) / 2,
  });

  const touchDistance = (touches) => Math.hypot(
    touches[0].clientX - touches[1].clientX,
    touches[0].clientY - touches[1].clientY,
  );

  const touchCenterInSurface = (touches) => {
    const center = touchCenter(touches);
    const rect = surfaceRef.current?.getBoundingClientRect();
    return {
      x: center.x - (rect?.left || 0),
      y: center.y - (rect?.top || 0),
    };
  };

  const releaseTouchDrag = (gesture) => {
    if (gesture?.buttonDown) {
      if (!gesture.direct) flushRelativePointer();
      if (gesture.direct) sendTouchContact(gesture.position, 'up');
      else sendTrackpadButton('up');
      gesture.buttonDown = false;
    }
  };

  const handleTouchStart = (event) => {
    event.preventDefault();
    event.stopPropagation();
    ignoreMouseUntilRef.current = performance.now() + 800;
    surfaceRef.current?.focus({ preventScroll: true });
    const { touches } = event;
    if (touches.length === 1) {
      clearLongPress();
      const now = performance.now();
      const point = { at: now, x: touches[0].clientX, y: touches[0].clientY };
      const direct = touchInputMode === 'direct';
      const directPosition = direct ? pointerPosition(touches[0]) : null;
      if (direct && !directPosition) return;
      if (!direct) setVirtualCursor(cursor => ({ ...cursor, visible: true }));
      const doubleTapDrag = !direct && isDoubleTap(lastTapRef.current, point);
      const gesture = {
        kind: 'pointer',
        startX: point.x,
        startY: point.y,
        lastX: point.x,
        lastY: point.y,
        lastAt: now,
        moved: false,
        buttonDown: direct || doubleTapDrag,
        direct,
        position: directPosition,
        startedAt: now,
      };
      touchGestureRef.current = gesture;
      if (direct) {
        lastTapRef.current = null;
        sendTouchContact(directPosition, 'down');
      } else if (doubleTapDrag) {
        lastTapRef.current = null;
        flushRelativePointer();
        sendTrackpadButton('down');
        navigator.vibrate?.(8);
      } else {
        longPressTimerRef.current = window.setTimeout(() => {
          if (touchGestureRef.current !== gesture || gesture.moved || gesture.buttonDown) return;
          gesture.buttonDown = true;
          flushRelativePointer();
          sendTrackpadButton('down');
          navigator.vibrate?.(12);
        }, TOUCH_LONG_PRESS_MS);
      }
    } else if (touches.length === 2) {
      clearLongPress();
      releaseTouchDrag(touchGestureRef.current);
      const center = touchCenterInSurface(touches);
      const distance = touchDistance(touches);
      touchGestureRef.current = {
        kind: 'two-finger',
        startCenter: center,
        lastCenter: center,
        startDistance: distance,
        mode: '',
        maxMovement: 0,
        scrollRemainder: { x: 0, y: 0 },
        startTransform: viewTransformRef.current,
        startedAt: performance.now(),
      };
    }
  };

  const handleTouchMove = (event) => {
    event.preventDefault();
    event.stopPropagation();
    ignoreMouseUntilRef.current = performance.now() + 800;
    const gesture = touchGestureRef.current;
    const { touches } = event;
    if (!gesture) return;
    if (gesture.kind === 'pointer' && touches.length === 1) {
      const now = performance.now();
      const deltaX = touches[0].clientX - gesture.lastX;
      const deltaY = touches[0].clientY - gesture.lastY;
      const totalMovement = Math.hypot(
        touches[0].clientX - gesture.startX,
        touches[0].clientY - gesture.startY,
      );
      gesture.moved ||= totalMovement >= TOUCH_TAP_SLOP;
      if (gesture.moved) clearLongPress();
      if (gesture.direct) {
        const position = pointerPosition(touches[0], true);
        if (position) {
          gesture.position = position;
          sendTouchContact(position, 'move');
        }
      } else {
        moveRelativePointer(deltaX, deltaY, now - gesture.lastAt);
      }
      gesture.lastX = touches[0].clientX;
      gesture.lastY = touches[0].clientY;
      gesture.lastAt = now;
      return;
    }
    if (gesture.kind === 'two-finger' && touches.length === 2) {
      const center = touchCenterInSurface(touches);
      const distance = touchDistance(touches);
      const deltaX = center.x - gesture.lastCenter.x;
      const deltaY = center.y - gesture.lastCenter.y;
      const centerMovement = pointDistance(gesture.startCenter, center);
      const pinchMovement = Math.abs(distance - gesture.startDistance);
      gesture.maxMovement = Math.max(gesture.maxMovement, centerMovement, pinchMovement);
      if (!gesture.mode && pinchMovement >= TOUCH_PINCH_SLOP && pinchMovement > centerMovement) {
        gesture.mode = 'zoom';
      }
      if (!gesture.mode && centerMovement >= TOUCH_SCROLL_SLOP) {
        gesture.mode = 'scroll';
      }
      if (gesture.mode === 'zoom') {
        const rect = surfaceRef.current?.getBoundingClientRect();
        if (rect) {
          updateViewTransform(nextPinchTransform(
            gesture.startTransform,
            gesture.startCenter,
            center,
            gesture.startDistance,
            distance,
            rect,
          ));
        }
      } else if (gesture.mode === 'scroll') {
        const scroll = consumeScrollDelta(gesture.scrollRemainder, deltaX, deltaY);
        gesture.scrollRemainder = scroll.remainder;
        if (scroll.stepsX || scroll.stepsY) {
          sendControl({
            type: 'wheel',
            deltaX: -scroll.stepsX * 100,
            deltaY: -scroll.stepsY * 100,
          });
        }
      }
      gesture.lastCenter = center;
    }
  };

  const handleTouchEnd = (event) => {
    event.preventDefault();
    event.stopPropagation();
    ignoreMouseUntilRef.current = performance.now() + 800;
    const gesture = touchGestureRef.current;
    clearLongPress();
    if (!gesture || event.touches.length > 0) return;
    const now = performance.now();
    if (gesture.kind === 'pointer') {
      if (gesture.direct) {
        const finalTouch = event.changedTouches?.[0];
        const finalPosition = finalTouch ? pointerPosition(finalTouch, true) : null;
        if (finalPosition) gesture.position = finalPosition;
        releaseTouchDrag(gesture);
      } else if (gesture.buttonDown) {
        releaseTouchDrag(gesture);
      } else if (!gesture.moved && now - gesture.startedAt <= TOUCH_TAP_MAX_MS) {
        flushRelativePointer();
        sendTrackpadButton('click');
        lastTapRef.current = { at: now, x: gesture.startX, y: gesture.startY };
      }
    } else if (
      gesture.kind === 'two-finger'
      && !gesture.mode
      && gesture.maxMovement < TOUCH_TAP_SLOP
      && now - gesture.startedAt <= TOUCH_TAP_MAX_MS
    ) {
      flushRelativePointer();
      sendTrackpadButton('click', 2);
      navigator.vibrate?.(8);
    }
    touchGestureRef.current = null;
  };

  const handleTouchCancel = (event) => {
    event.preventDefault();
    clearLongPress();
    releaseTouchDrag(touchGestureRef.current);
    touchGestureRef.current = null;
  };

  const openSystemKeyboard = () => {
    remoteInputRef.current?.focus({ preventScroll: true });
  };

  const handleRemoteTextInput = (event) => {
    const nextValue = event.target.value;
    const previousValue = remoteInputValueRef.current;
    if (nextValue.startsWith(previousValue)) {
      const inserted = nextValue.slice(previousValue.length);
      if (inserted) sendControl({ type: 'text', text: inserted });
    } else if (previousValue.startsWith(nextValue)) {
      for (let index = 0; index < previousValue.length - nextValue.length; index += 1) {
        sendControl({ type: 'key', key: 'Backspace', code: 'Backspace', action: 'click' });
      }
    }
    remoteInputValueRef.current = nextValue;
  };

  const toggleFullscreen = () => {
    if (document.fullscreenElement) document.exitFullscreen();
    else desktopAreaRef.current?.requestFullscreen?.();
  };

  const toggleFillMode = () => {
    resetViewTransform();
    setFillMode(mode => mode === 'cover' ? 'contain' : 'cover');
  };

  const cursorDisplayPoint = remoteCursorPoint(
    virtualCursor,
    surfaceSize,
    {
      width: videoRef.current?.videoWidth || surfaceSize.width,
      height: videoRef.current?.videoHeight || surfaceSize.height,
    },
    fillMode,
    viewTransform,
  );

  return (
    <div className="flex h-dvh min-h-0 flex-col bg-kumo-recessed text-kumo-default">
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b border-kumo-line bg-kumo-base px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <DesktopDisplay className="h-5 w-5 text-brand" />
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold text-kumo-strong">{serverName || 'Windows 远程桌面'}</div>
          </div>
          <Badge variant={state === 'connected' ? 'success' : state === 'error' || state === 'failed' ? 'error' : 'neutral'} appearance="dot">
            {stateLabel(state)}
          </Badge>
        </div>
        <div className="flex w-full min-w-0 flex-wrap items-center justify-end gap-2 md:w-auto">
          <span className="hidden text-[11px] text-kumo-subtle md:inline">
            {stats.rtt ? `${stats.rtt} ms · ` : ''}{stats.fps ? `解码 ${stats.fps.toFixed(0)} FPS · ` : ''}
            {stats.receivedFps ? `接收 ${stats.receivedFps.toFixed(0)} FPS · ` : ''}
            {stats.droppedFps ? `丢帧 ${stats.droppedFps.toFixed(1)}/s · ` : ''}
            {stats.bitrate ? `${(stats.bitrate / 1_000_000).toFixed(1)} Mbps · ` : ''}
            {stats.bufferMs ? `缓冲 ${stats.bufferMs.toFixed(0)} ms · ` : ''}
            {stats.loss ? `丢包 ${stats.loss.toFixed(1)}% · ` : ''}
            {stats.local && stats.remote
              ? `${stats.localLabel || stats.local} ↔ ${stats.remoteLabel || stats.remote}`
              : 'ICE 协商中'}
          </span>
          <Button size="sm" variant={controlEnabled ? 'primary' : 'secondary'} onClick={() => setControlEnabled(value => !value)}>
            {controlEnabled ? (controlAcknowledged ? '控制通道正常' : '控制已开启') : '仅观看'}
          </Button>
          <Button size="sm" variant="secondary" onClick={openSystemKeyboard}>键盘</Button>
          <Button size="sm" variant="secondary" onClick={sendLocalClipboard}>剪贴板</Button>
          <Button
            size="sm"
            variant={clipboardSync ? 'primary' : 'secondary'}
            onClick={() => setClipboardSync(value => !value)}
          >
            {clipboardSync ? '自动' : '手动'}
          </Button>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => setTouchInputMode(mode => mode === 'trackpad' ? 'direct' : 'trackpad')}
          >
            {touchInputMode === 'trackpad' ? '触控板' : '直接触摸'}
          </Button>
          <Button size="sm" variant="secondary" onClick={toggleFillMode}>
            {fillMode === 'cover' ? '填满' : '适应'}
          </Button>
          {viewTransform.scale > 1 && <Button size="sm" variant="secondary" onClick={resetViewTransform}>重置缩放</Button>}
          <Button size="sm" shape="square" variant="secondary" icon={<RefreshCw className="h-4 w-4" />} aria-label="重新连接" onClick={connect} />
          <Button size="sm" shape="square" variant="secondary" icon={<Maximize2 className="h-4 w-4" />} aria-label="全屏" onClick={toggleFullscreen} />
          <Button size="sm" shape="square" variant="secondary" icon={<X className="h-4 w-4" />} aria-label="关闭" onClick={() => window.close()} />
        </div>
      </header>

      <main ref={desktopAreaRef} className="relative flex min-h-0 flex-1 items-center justify-center overflow-hidden bg-kumo-strong">
        <div
          ref={surfaceRef}
          tabIndex={0}
          className="relative flex h-full w-full items-center justify-center overflow-hidden outline-none focus:ring-2 focus:ring-kumo-brand"
          onMouseMove={handlePointerMove}
          onMouseDown={(event) => handleMouse(event, 'down')}
          onMouseUp={(event) => handleMouse(event, 'up')}
          onContextMenu={(event) => event.preventDefault()}
          onWheel={(event) => { event.preventDefault(); sendControl({ type: 'wheel', deltaX: event.deltaX, deltaY: event.deltaY }); }}
          onTouchStart={handleTouchStart}
          onTouchMove={handleTouchMove}
          onTouchEnd={handleTouchEnd}
          onTouchCancel={handleTouchCancel}
          style={{ touchAction: 'none', overscrollBehavior: 'contain' }}
        >
          <video
            ref={videoRef}
            autoPlay
            playsInline
            muted
            aria-label={`${serverName} 远程桌面`}
            onLoadedData={() => setVideoReady(true)}
            className={`h-full w-full select-none ${fillMode === 'cover' ? 'object-cover' : 'object-contain'} ${videoReady ? 'block' : 'hidden'}`}
            style={{
              transform: `translate3d(${viewTransform.x}px, ${viewTransform.y}px, 0) scale(${viewTransform.scale})`,
              transformOrigin: '0 0',
              willChange: viewTransform.scale > 1 ? 'transform' : 'auto',
            }}
          />
          {!videoReady && (
            <div className="flex flex-col items-center gap-3 text-center text-kumo-inverse/70">
              <DesktopDisplay className="h-12 w-12" />
              <div className="text-sm">{stateLabel(state)}</div>
              <div className="max-w-lg text-xs text-kumo-inverse/45">正在交换公网候选地址并尝试 UDP 打洞。严格直连模式不会使用 fly.io 转发桌面数据。</div>
            </div>
          )}
          {virtualCursor.visible && (
            <div
              aria-hidden="true"
              className="pointer-events-none absolute z-20 drop-shadow-[0_1px_1px_rgba(0,0,0,0.75)]"
              style={{ left: `${cursorDisplayPoint.x}px`, top: `${cursorDisplayPoint.y}px` }}
            >
              <Cursor size={18} weight="fill" className="text-brand" />
            </div>
          )}
        </div>
        {isFullscreen && (
          <div className="absolute left-3 right-3 top-3 z-30 flex justify-end">
            {!fullscreenToolbarOpen ? (
              <Button
                size="sm"
                variant="secondary"
                icon={<Menu className="h-4 w-4" />}
                aria-label="展开全屏控制栏"
                aria-expanded="false"
                onClick={() => setFullscreenToolbarOpen(true)}
              >
                控制
              </Button>
            ) : (
              <div
                role="toolbar"
                aria-label="全屏远程桌面控制栏"
                className="flex max-w-full flex-wrap items-center justify-end gap-2 rounded-md border border-kumo-line bg-kumo-base/95 p-2"
              >
                <Badge variant={state === 'connected' ? 'success' : state === 'error' || state === 'failed' ? 'error' : 'neutral'} appearance="dot">
                  {stateLabel(state)}
                </Badge>
                <span className="text-[11px] text-kumo-subtle">
                  {stats.rtt ? `${stats.rtt} ms · ` : ''}{stats.fps ? `${stats.fps.toFixed(0)} FPS` : '等待视频'}
                  {stats.bufferMs ? ` · 缓冲 ${stats.bufferMs.toFixed(0)} ms` : ''}
                </span>
                <Button size="sm" variant={controlEnabled ? 'primary' : 'secondary'} onClick={() => setControlEnabled(value => !value)}>
                  {controlEnabled ? '控制开启' : '仅观看'}
                </Button>
                <Button size="sm" variant="secondary" onClick={openSystemKeyboard}>键盘</Button>
                <Button size="sm" variant="secondary" onClick={sendLocalClipboard}>剪贴板</Button>
                <Button
                  size="sm"
                  variant={clipboardSync ? 'primary' : 'secondary'}
                  onClick={() => setClipboardSync(value => !value)}
                >
                  {clipboardSync ? '自动' : '手动'}
                </Button>
                <Button size="sm" variant="secondary" onClick={() => setTouchInputMode(mode => mode === 'trackpad' ? 'direct' : 'trackpad')}>
                  {touchInputMode === 'trackpad' ? '触控板' : '直接触摸'}
                </Button>
                <Button size="sm" variant="secondary" onClick={toggleFillMode}>
                  {fillMode === 'cover' ? '填满' : '适应'}
                </Button>
                {viewTransform.scale > 1 && <Button size="sm" variant="secondary" onClick={resetViewTransform}>重置缩放</Button>}
                <Button size="sm" shape="square" variant="secondary" icon={<RefreshCw className="h-4 w-4" />} aria-label="重新连接" onClick={connect} />
                <Button size="sm" shape="square" variant="secondary" icon={<Maximize2 className="h-4 w-4" />} aria-label="退出全屏" onClick={toggleFullscreen} />
                <Button
                  size="sm"
                  shape="square"
                  variant="secondary"
                  icon={<ChevronUp className="h-4 w-4" />}
                  aria-label="收起全屏控制栏"
                  onClick={() => setFullscreenToolbarOpen(false)}
                />
              </div>
            )}
          </div>
        )}
        {error && (
          <div className="absolute bottom-4 left-1/2 max-w-2xl -translate-x-1/2 rounded-md border border-kumo-danger/40 bg-kumo-danger/90 px-4 py-2 text-sm font-semibold text-kumo-inverse">
            {error}
          </div>
        )}
      </main>
      <textarea
        data-ui-exception="remote-system-keyboard-input"
        ref={remoteInputRef}
        aria-label="远程键盘输入"
        inputMode="text"
        autoCapitalize="off"
        autoCorrect="off"
        value={remoteInputValueRef.current}
        onChange={handleRemoteTextInput}
        className="remote-system-keyboard-input fixed -left-[9999px] top-0 h-px w-px opacity-0"
      />
    </div>
  );
}
