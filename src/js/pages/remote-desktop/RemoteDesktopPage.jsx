import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { ChevronUp, DesktopDisplay, Maximize2, Menu, RefreshCw, X } from '../../components/Icons.jsx';
import {
  DESKTOP_FPS_OPTIONS,
  DESKTOP_QUALITY_PRESETS,
  DEFAULT_DESKTOP_FPS,
  DEFAULT_DESKTOP_PRESET,
  TOUCH_LONG_PRESS_MS,
  TOUCH_PINCH_SLOP,
  TOUCH_SCROLL_SLOP,
  TOUCH_TAP_MAX_MS,
  TOUCH_TAP_SLOP,
  consumeScrollDelta,
  isDoubleTap,
  nextPinchTransform,
  nextRemoteDesktopProfile,
  normalizedTrackpadDelta,
  normalizedVideoPoint,
  normalizedDesktopFps,
  pointDistance,
  remoteDesktopProfileForPreset,
  trackpadButtonMessage,
  trackpadPixelDelta,
} from '../../modules/remoteDesktopTouch.js';
import { ICE_SERVERS, SIGNAL_POLL_MS } from './constants.js';
import { readDesktopPreferences, writeDesktopPreferences } from './preferences.js';
import {
  apiRequest,
  authHeaders,
  candidateTypeLabel,
  serverIdFromPath,
  stateLabel,
} from './utils.js';

export default function RemoteDesktopPage() {
  const serverId = useMemo(serverIdFromPath, []);
  const [serverName, setServerName] = useState(serverId);
  const [state, setState] = useState('initializing');
  const [error, setError] = useState('');
  const [videoReady, setVideoReady] = useState(false);
  const [fillMode, setFillMode] = useState('contain');
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [fullscreenToolbarOpen, setFullscreenToolbarOpen] = useState(false);
  const [viewTransform, setViewTransform] = useState({ scale: 1, x: 0, y: 0 });
  const [controlEnabled, setControlEnabled] = useState(true);
  const [touchInputMode, setTouchInputMode] = useState('trackpad');
  // 画质档与帧率从本地偏好恢复：刷新/重连后保持用户选择，而不是重置回默认。
  const desktopPreferences = useMemo(readDesktopPreferences, []);
  const [qualityPreset, setQualityPreset] = useState(desktopPreferences.preset);
  const [fpsLimit, setFpsLimit] = useState(desktopPreferences.fps);
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
  const baseProfileRef = useRef(remoteDesktopProfileForPreset(
    desktopPreferences.preset,
    coarsePointerRef.current,
    desktopPreferences.fps,
  ));
  const streamProfileRef = useRef(baseProfileRef.current);
  const qualityPresetRef = useRef(desktopPreferences.preset);
  const fpsLimitRef = useRef(desktopPreferences.fps);
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
  const remoteComposingRef = useRef(false);
  const lastSentClipboardRef = useRef('');
  const lastReceivedClipboardRef = useRef('');
  const skipAutoReconnectRef = useRef(false);
  // 本会话是否已被同一主机的新连接接管。接管是正常结果而非错误：停止轮询并
  // 提供「重新接管」入口，不自动重连（自动重连会与新页签互相抢占）。
  const supersededRef = useRef(false);
  const connectRef = useRef(null);

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
    if (!sessionId || stoppedRef.current || sessionRef.current !== sessionId || generation !== connectionGenerationRef.current) return true;
    try {
      const data = await apiRequest(`/api/server/remote-desktop/sessions/${encodeURIComponent(sessionId)}/signals?since=${lastSignalRef.current}&wait=${longPoll ? 15000 : 0}`, {
        headers: authHeaders(),
        cache: 'no-store',
      });
      if (sessionRef.current !== sessionId || generation !== connectionGenerationRef.current) return true;
      if (data.state) setState(data.state);
      for (const item of data.signals || []) {
        if (sessionRef.current !== sessionId || generation !== connectionGenerationRef.current) return true;
        lastSignalRef.current = Math.max(lastSignalRef.current, Number(item.id) || 0);
        await applyRemoteSignal(item.payload, sessionId, generation);
      }
      return true;
    } catch (err) {
      // 会话被同一主机的新连接接管：后端返回 409 + reason=superseded。这不是
      // 错误状态，而是「另一处已接管」的正常结果——必须停止轮询并提示，同时
      // 绝不能落入自动重连（否则两个页签会互相抢占形成重连风暴）。
      if (err.status === 409 && err.reason === 'superseded') {
        supersededRef.current = true;
        skipAutoReconnectRef.current = true;
        setState('superseded');
        setError('');
        return false;
      }
      if (!stoppedRef.current && sessionRef.current === sessionId && generation === connectionGenerationRef.current) {
        setError(err.message || '信令同步失败');
      }
      // 会话已不存在（404/409）：让调用方停止轮询该会话，避免对已失效的会话
      // 无延迟死循环重试（例如同一主机在另一标签页被重新创建时旧会话会被回收）。
      return err.status !== 404 && err.status !== 409;
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
      connectRef.current?.();
    }, 500 * attempt);
  }, []);

  const connect = useCallback(async () => {
    await closeSession();
    const generation = connectionGenerationRef.current;
    stoppedRef.current = false;
    skipAutoReconnectRef.current = false;
    supersededRef.current = false;
    setState('initializing');
    setError('');
    setControlAcknowledged(false);
    baseProfileRef.current = remoteDesktopProfileForPreset(
      qualityPresetRef.current,
      coarsePointerRef.current,
      fpsLimitRef.current,
    );
    streamProfileRef.current = baseProfileRef.current;
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
          // 不锁定抖动缓冲。原实现把 playoutDelayHint / jitterBufferTarget 强制为 0，
          // 等于不给解码器任何余量，网络一抖就卡顿并被判定为劣化而降码率。
          // playoutDelayHint 是可空属性，重置为 undefined 交还 UA 决定（LiveKit 客户端
          // 默认就是这么做的）；jitterBufferTarget 不可为空，因此完全不设置、保留 UA 默认。
          if ('playoutDelayHint' in receiver) receiver.playoutDelayHint = undefined;
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
              cursorPositionRef.current = {
                x: Math.max(0, Math.min(1, Number(meta.x || 0))),
                y: Math.max(0, Math.min(1, Number(meta.y || 0))),
              };
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
        // 连同初始档位一起下发，使首帧就按用户选定的画质档 + 帧率档编码，
        // 而不是先按 Agent 默认档跑一段再由 video-config 纠正。
        body: JSON.stringify({
          serverId,
          offer: peer.localDescription,
          profile: baseProfileRef.current,
        }),
      });
      if (generation !== connectionGenerationRef.current || peer !== peerRef.current) {
        await fetch(`/api/server/remote-desktop/sessions/${encodeURIComponent(created.sessionId)}`, {
          method: 'DELETE', headers: authHeaders(), keepalive: true,
        }).catch(() => {});
        return;
      }
      sessionRef.current = created.sessionId;
      for (const signal of pendingLocalIceRef.current.splice(0)) await postSignal(signal, peer, generation);
      // 首次拉取：若会话在建立瞬间就被同主机的新连接接管（另一个页签同时接管），
      // 进入「已被接管」终态。这不是错误，也不自动重连——否则两个页签会互相
      // 抢占形成重连风暴；页面提供一键「重新接管」入口。
      const alive = await pollSignals(created.sessionId, generation);
      if (!alive && generation === connectionGenerationRef.current) {
        sessionRef.current = '';
        if (supersededRef.current) {
          setState('superseded');
          setError('');
        } else {
          skipAutoReconnectRef.current = true;
          setState('closed');
          setError('远程桌面会话已被回收，请刷新页面重试。');
        }
      }
    } catch (err) {
      if (generation === connectionGenerationRef.current) {
        // superseded 已在 pollSignals 内处理为专门状态，这里不再覆盖为通用错误。
        if (!supersededRef.current) {
          setState('error');
          setError(err.message || '远程桌面初始化失败');
        }
        // 4xx 表示请求本身被拒绝（Agent 离线 / 不支持等），重试不会改变
        // 结果；网络类错误仍在可恢复范围内，保留自动重连。
        if (err.status === 400 || err.status === 409) {
          skipAutoReconnectRef.current = true;
        }
      }
    }
  }, [bindChannel, closeSession, pollSignals, postSignal, serverId]);
  connectRef.current = connect;

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
        const alive = await pollSignals(sessionId, generation, true);
        if (!alive) {
          // 仅当「正在轮询的会话仍是当前会话」时才判定为终态并退出。重连/刷新
          // 会让 closeSession 递增 generation 并清空 sessionRef，此时在途的旧会话
          // 长轮询会收到 404——那不是会话被回收，而是已被新会话取代，必须继续
          // 轮询，否则新会话将无人拉取 answer/ICE，导致重连后永远没有画面。
          const stillCurrent = !signalLoopCancelled
            && sessionRef.current === sessionId
            && generation === connectionGenerationRef.current;
          if (stillCurrent) {
            sessionRef.current = '';
            skipAutoReconnectRef.current = true;
            if (supersededRef.current) {
              // 被同主机的新连接接管：正常结果，提示但不要求刷新；页面上的
              // 「重新接管」按钮可一键夺回。
              setState('superseded');
              setError('');
            } else {
              setState('closed');
              setError('远程桌面会话已被回收，请刷新页面重试。');
            }
            break;
          }
          // 会话已被新连接取代：短暂等待后由下一轮循环接管新会话。
          await new Promise(resolve => window.setTimeout(resolve, SIGNAL_POLL_MS));
        }
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
      // 仅作为劣化下限的参考上限；真实编码档位由 Agent 按原生桌面像素决定。
      const nativeBitrate = videoPixels >= 8_294_400 ? 40_000_000
        : videoPixels >= 3_686_400 ? 28_000_000
          : videoPixels >= 2_073_600 ? 20_000_000
            : 12_000_000;
      const adaptation = nextRemoteDesktopProfile({
        loss: measuredLoss,
        rtt: measuredRtt,
        bufferMs: measuredBufferMs,
        droppedFps: measuredDroppedFps,
        nativeBitrate,
        healthyIntervals: healthyIntervalsRef.current,
        current: streamProfileRef.current,
        base: baseProfileRef.current,
      });
      const nextProfile = adaptation.profile;
      healthyIntervalsRef.current = adaptation.healthyIntervals;
      const currentProfile = streamProfileRef.current;
      if (nextProfile.fps !== currentProfile.fps
        || nextProfile.bitrate !== currentProfile.bitrate
        || nextProfile.maxLongEdge !== currentProfile.maxLongEdge) {
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

  useEffect(() => () => {
    clearLongPress();
    if (pointerFrameRef.current) window.cancelAnimationFrame(pointerFrameRef.current);
    if (absolutePointerFrameRef.current) window.cancelAnimationFrame(absolutePointerFrameRef.current);
  }, [clearLongPress]);

  useEffect(() => {
    const closeOnPageHide = (event) => {
      // 进入 bfcache（persisted）意味着页面可能被原样恢复，此时销毁会话会在
      // 用户返回标签页时留下一个已失效的连接；只在真正卸载时关闭。
      if (event.persisted) return;
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
    // 500 ms keeps it responsive; the query travels over the P2P data channel,
    // so it costs no server bandwidth.
    const timer = window.setInterval(syncCursor, 500);
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

  const commitRemoteText = (nextValue) => {
    const previousValue = remoteInputValueRef.current;
    if (nextValue === previousValue) return;
    if (nextValue.startsWith(previousValue)) {
      const inserted = nextValue.slice(previousValue.length);
      if (inserted) sendControl({ type: 'text', text: inserted });
    } else if (previousValue.startsWith(nextValue)) {
      for (let index = 0; index < previousValue.length - nextValue.length; index += 1) {
        sendControl({ type: 'key', key: 'Backspace', code: 'Backspace', action: 'click' });
      }
    } else {
      // 光标在中间编辑/替换：先退格回到公共前缀，再插入新内容。
      let common = 0;
      while (common < previousValue.length
        && common < nextValue.length
        && previousValue[common] === nextValue[common]) common += 1;
      for (let index = 0; index < previousValue.length - common; index += 1) {
        sendControl({ type: 'key', key: 'Backspace', code: 'Backspace', action: 'click' });
      }
      const inserted = nextValue.slice(common);
      if (inserted) sendControl({ type: 'text', text: inserted });
    }
    remoteInputValueRef.current = nextValue;
  };

  const handleRemoteTextInput = (event) => {
    // 输入法组合期间不发送：此时输入框里是候选/拼音临时文本，提前发出会污染远程输入。
    if (remoteComposingRef.current) return;
    commitRemoteText(event.target.value);
  };

  const handleRemoteCompositionStart = () => {
    remoteComposingRef.current = true;
  };

  const handleRemoteCompositionEnd = (event) => {
    remoteComposingRef.current = false;
    commitRemoteText(event.target.value);
  };

  const toggleFullscreen = () => {
    if (document.fullscreenElement) document.exitFullscreen();
    else desktopAreaRef.current?.requestFullscreen?.();
  };

  const toggleFillMode = () => {
    resetViewTransform();
    setFillMode(mode => mode === 'cover' ? 'contain' : 'cover');
  };

  // 选择画质档：更新基准档位并立即通过控制通道下发（分辨率 + 码率）。帧率由
  // 独立的 fps 档位控制，这里只刷新当前组合出的完整 profile。
  const applyQualityPreset = useCallback((presetId) => {
    qualityPresetRef.current = presetId;
    setQualityPreset(presetId);
    writeDesktopPreferences({ preset: presetId, fps: fpsLimitRef.current });
    const profile = remoteDesktopProfileForPreset(presetId, coarsePointerRef.current, fpsLimitRef.current);
    baseProfileRef.current = profile;
    streamProfileRef.current = profile;
    healthyIntervalsRef.current = 0;
    const controlChannel = channelRef.current;
    if (controlChannel?.readyState === 'open') {
      controlChannel.send(JSON.stringify({ type: 'video-config', ...profile }));
    }
  }, []);

  // 帧率档位（30/60）。Agent 侧 video_config 会把 fps 钳制到 [30, 60]，60 是
  // 硬上限；NVENC 走 GPU 编码，1080p60 的额外开销主要在上行带宽而非主机 CPU。
  const applyFpsLimit = useCallback((nextFps) => {
    const normalized = normalizedDesktopFps(nextFps);
    fpsLimitRef.current = normalized;
    setFpsLimit(normalized);
    writeDesktopPreferences({ preset: qualityPresetRef.current, fps: normalized });
    const profile = remoteDesktopProfileForPreset(qualityPresetRef.current, coarsePointerRef.current, normalized);
    baseProfileRef.current = profile;
    streamProfileRef.current = profile;
    healthyIntervalsRef.current = 0;
    const controlChannel = channelRef.current;
    if (controlChannel?.readyState === 'open') {
      controlChannel.send(JSON.stringify({ type: 'video-config', ...profile }));
    }
  }, []);

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
          <Select
            alignItemWithTrigger
            size="sm"
            aria-label="画质档位"
            value={qualityPreset}
            onValueChange={applyQualityPreset}
            className="w-auto min-w-0 px-3 py-1.5"
            items={DESKTOP_QUALITY_PRESETS.map(preset => ({
              value: preset.id,
              label: `画质·${preset.label}`,
            }))}
          />
          <Select
            alignItemWithTrigger
            size="sm"
            aria-label="帧率档位"
            value={String(fpsLimit)}
            onValueChange={value => applyFpsLimit(Number(value))}
            className="w-auto min-w-0 px-3 py-1.5"
            items={DESKTOP_FPS_OPTIONS.map(option => ({
              value: String(option.id),
              label: option.label,
            }))}
          />
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
          {!videoReady && state === 'superseded' && (
            <div className="flex flex-col items-center gap-3 text-center text-kumo-inverse/70">
              <DesktopDisplay className="h-12 w-12" />
              <div className="text-sm">已被其他页面接管</div>
              <div className="max-w-lg text-xs text-kumo-inverse/45">
                同一主机同时只允许一个远程桌面会话。另一处（其他页签或设备）已接管该会话，本页已停止拉流。
                点击下方按钮可夺回控制权，原页面会随即让位。
              </div>
              <Button size="sm" variant="primary" onClick={connect}>重新接管</Button>
            </div>
          )}
          {!videoReady && state !== 'superseded' && (
            <div className="flex flex-col items-center gap-3 text-center text-kumo-inverse/70">
              <DesktopDisplay className="h-12 w-12" />
              <div className="text-sm">{stateLabel(state)}</div>
              <div className="max-w-lg text-xs text-kumo-inverse/45">正在交换公网候选地址并尝试 UDP 打洞。严格直连模式不会使用 fly.io 转发桌面数据。</div>
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
                <Select
                  alignItemWithTrigger
                  size="sm"
                  aria-label="画质档位"
                  value={qualityPreset}
                  onValueChange={applyQualityPreset}
                  className="w-auto min-w-0 px-3 py-1.5"
                  items={DESKTOP_QUALITY_PRESETS.map(preset => ({
                    value: preset.id,
                    label: `画质·${preset.label}`,
                  }))}
                />
                <Select
                  alignItemWithTrigger
                  size="sm"
                  aria-label="帧率档位"
                  value={String(fpsLimit)}
                  onValueChange={value => applyFpsLimit(Number(value))}
                  className="w-auto min-w-0 px-3 py-1.5"
                  items={DESKTOP_FPS_OPTIONS.map(option => ({
                    value: String(option.id),
                    label: option.label,
                  }))}
                />
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
        onCompositionStart={handleRemoteCompositionStart}
        onCompositionEnd={handleRemoteCompositionEnd}
        className="remote-system-keyboard-input fixed -left-[9999px] top-0 h-px w-px opacity-0"
      />
    </div>
  );
}
