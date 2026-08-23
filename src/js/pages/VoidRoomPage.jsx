import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import axios from 'axios';
import QRCode from 'qrcode';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { ClipboardText, Meter } from '@cloudflare/kumo';
import {
  AlertTriangle,
  Download,
  FileText,
  Image,
  Paperclip,
  RefreshCw,
  Send,
  Users,
  X,
} from '../components/Icons.jsx';
import { SectionCard, cx } from '../components/ui/AppPrimitives.jsx';
import { toast } from '../modules/toast.js';
import { formatDateTime, formatFileSize } from '../modules/utils.js';

const VOID_CHUNK_SIZE = 64 * 1024;
const VOID_BUFFER_LIMIT = 4 * 1024 * 1024;
const VOID_SIGNAL_POLL_MS = 350;
const VOID_READY_RETRY_MS = 2500;
const VOID_CONNECT_TIMEOUT_MS = 12000;
const VOID_ICE_SERVERS = [
  { urls: 'stun:stun.l.google.com:19302' },
  { urls: 'stun:global.stun.twilio.com:3478' },
];
const OWNER_STORAGE_PREFIX = 'void_owner_credentials:';
const GUEST_STORAGE_PREFIX = 'void_guest_credentials:';
const DEVICE_CLIENT_ID_KEY = 'void_device_client_id';

function roomIdFromPath() {
  const match = window.location.pathname.match(/^\/void\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]).replace(/[^A-Z0-9]/gi, '').toUpperCase() : '';
}

function roomURL(roomId, origin = window.location.origin) {
  return `${String(origin || window.location.origin).replace(/\/+$/g, '')}/void/${encodeURIComponent(roomId)}`;
}

function sortedParticipants(participants) {
  return [...(participants || [])].sort((a, b) => {
    if (a.role !== b.role) return a.role === 'owner' ? -1 : 1;
    if ((a.createdAt || 0) !== (b.createdAt || 0)) return (a.createdAt || 0) - (b.createdAt || 0);
    return String(a.id || '').localeCompare(String(b.id || ''));
  });
}

function sleep(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function newMessageId() {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 9)}`;
}

function randomClientId() {
  const random = new Uint8Array(16);
  crypto.getRandomValues(random);
  return Array.from(random, (value) => value.toString(16).padStart(2, '0')).join('');
}

function deviceClientId() {
  const stored = localStorage.getItem(DEVICE_CLIENT_ID_KEY);
  if (stored) return stored;
  const id = randomClientId();
  localStorage.setItem(DEVICE_CLIENT_ID_KEY, id);
  return id;
}

function saveBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename || 'void-transfer.bin';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

function deviceName() {
  const ua = navigator.userAgent || '';
  if (/iphone|android|mobile/i.test(ua)) return '手机';
  if (/ipad|tablet/i.test(ua)) return '平板';
  return '浏览器';
}

function readJSONStorage(key) {
  try {
    return JSON.parse(sessionStorage.getItem(key) || 'null');
  } catch {
    return null;
  }
}

function writeJSONStorage(key, value) {
  sessionStorage.setItem(key, JSON.stringify(value));
}

function readOwnerCredentials(roomId) {
  const key = OWNER_STORAGE_PREFIX + roomId;
  const sessionCred = readJSONStorage(key);
  if (sessionCred?.ownerToken) {
    localStorage.removeItem(key);
    return sessionCred;
  }
  try {
    const localCred = JSON.parse(localStorage.getItem(key) || 'null');
    if (localCred?.ownerToken) {
      writeJSONStorage(key, localCred);
      localStorage.removeItem(key);
      return localCred;
    }
  } catch {
    localStorage.removeItem(key);
  }
  return null;
}

function isOpenChannel(channel) {
  return channel && channel.readyState === 'open';
}

function createObjectURL(blob) {
  return URL.createObjectURL(blob);
}

function MessageBubble({ message, mine }) {
  const isImage = message.mime?.startsWith('image/') || message.kind === 'image';
  return (
    <div className={cx('flex', mine ? 'justify-end' : 'justify-start')}>
      <div className={cx('max-w-[82%] rounded-md border border-kumo-line p-3', mine ? 'bg-brand/10' : 'bg-kumo-recessed/35')}>
        <div className="mb-1 flex items-center gap-2 text-[11px] text-kumo-subtle">
          <span className="font-semibold text-kumo-strong">{mine ? '我' : message.name || message.from || '对方'}</span>
          <span>{message.status === 'sending' ? '发送中' : message.status === 'failed' ? '失败' : formatDateTime(message.createdAt)}</span>
        </div>
        {message.kind === 'text' ? (
          <div className="whitespace-pre-wrap break-words text-sm text-kumo-strong">{message.text}</div>
        ) : (
          <div className="grid gap-2">
            <div className="flex items-start gap-2">
              {isImage ? <Image className="mt-0.5 h-4 w-4 shrink-0 text-brand" /> : <FileText className="mt-0.5 h-4 w-4 shrink-0 text-brand" />}
              <div className="min-w-0">
                <div className="break-all text-sm font-semibold text-kumo-strong">{message.fileName || '文件'}</div>
                <div className="mt-0.5 text-[11px] text-kumo-subtle">{formatFileSize(message.size || 0)}</div>
              </div>
            </div>
            {message.status !== 'done' && <Meter label="传输进度" value={message.progress || 0} customValue={`${message.progress || 0}%`} />}
            {isImage && message.url && <img src={message.url} alt={message.fileName || '图片'} className="max-h-64 rounded-md border border-kumo-line object-contain" />}
            {message.blob && (
              <div className="flex justify-end">
                <Button size="sm" variant="secondary" onClick={() => saveBlob(message.blob, message.fileName)} icon={<Download className="h-3.5 w-3.5" />}>保存</Button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function VoidRoomPage() {
  const roomId = useMemo(roomIdFromPath, []);
  const [room, setRoom] = useState(null);
  const [role, setRole] = useState('guest');
  const [participant, setParticipant] = useState(null);
  const [status, setStatus] = useState('初始化');
  const [error, setError] = useState('');
  const [messages, setMessages] = useState([]);
  const [text, setText] = useState('');
  const [selectedFile, setSelectedFile] = useState(null);
  const [sending, setSending] = useState(false);
  const [connectedPeers, setConnectedPeers] = useState({});
  const [qrCode, setQrCode] = useState('');

  const roleRef = useRef(role);
  const participantRef = useRef(participant);
  const roomRef = useRef(room);
  const messagesRef = useRef(messages);
  const connectedPeersRef = useRef(connectedPeers);
  const peersRef = useRef(new Map());
  const pendingIceRef = useRef(new Map());
  const lastSignalIDRef = useRef(0);
  const stoppedRef = useRef(false);
  const pollTimerRef = useRef(null); // 轮询定时器句柄：卸载竞态时由 cleanup 兜底再清一次
  const fileInputRef = useRef(null);

  useEffect(() => { roleRef.current = role; }, [role]);
  useEffect(() => { participantRef.current = participant; }, [participant]);
  useEffect(() => { roomRef.current = room; }, [room]);
  useEffect(() => { messagesRef.current = messages; }, [messages]);
  useEffect(() => { connectedPeersRef.current = connectedPeers; }, [connectedPeers]);

  const activeLink = roomURL(roomId);
  const participantName = participant?.name || (role === 'owner' ? '房主' : deviceName());
  const clientId = useMemo(deviceClientId, []);

  const upsertMessage = useCallback((message) => {
    setMessages((prev) => {
      const index = prev.findIndex((item) => item.id === message.id);
      if (index === -1) {
        const next = [...prev, message];
        if (next.length > 120) {
          const removed = next.splice(0, next.length - 120);
          removed.forEach((m) => { if (m.url) URL.revokeObjectURL(m.url); });
        }
        return next;
      }
      const next = [...prev];
      next[index] = { ...next[index], ...message };
      return next;
    });
  }, []);

  const updatePeerStatus = useCallback((peerId, state) => {
    setConnectedPeers((prev) => ({ ...prev, [peerId]: state }));
  }, []);

  const closePeer = useCallback((peerId, nextState = 'connecting') => {
    const entry = peersRef.current.get(peerId);
    if (entry) {
      entry.closing = true;
      entry.channel?.close?.();
      entry.peer?.close?.();
    }
    peersRef.current.delete(peerId);
    pendingIceRef.current.delete(peerId);
    if (nextState) updatePeerStatus(peerId, nextState);
  }, [updatePeerStatus]);

  const postSignal = useCallback(async (to, type, payload) => {
    const current = participantRef.current;
    if (!current?.id || !current?.token) return;
    await axios.post(`/api/filebox/void/rooms/${encodeURIComponent(roomId)}/signals`, {
      participantId: current.id,
      participantToken: current.token,
      to,
      type,
      payload,
    });
  }, [roomId]);

  const flushPendingIce = useCallback(async (peerId, peer) => {
    const pending = pendingIceRef.current.get(peerId) || [];
    pendingIceRef.current.delete(peerId);
    for (const candidate of pending) {
      await peer.addIceCandidate(candidate).catch(() => {});
    }
  }, []);

  const sendJSON = useCallback((channel, payload) => {
    if (isOpenChannel(channel)) channel.send(JSON.stringify(payload));
  }, []);

  const broadcastJSON = useCallback((payload, excludePeerId = '') => {
    for (const [peerId, entry] of peersRef.current.entries()) {
      if (peerId === excludePeerId) continue;
      sendJSON(entry.channel, payload);
    }
  }, [sendJSON]);

  const sendBlobOnChannel = useCallback(async (channel, blob, meta, onProgress) => {
    if (!isOpenChannel(channel)) throw new Error('连接尚未建立');
    channel.send(JSON.stringify(meta));
    let sent = 0;
    for (let offset = 0; offset < blob.size; offset += VOID_CHUNK_SIZE) {
      while (channel.bufferedAmount > VOID_BUFFER_LIMIT) await sleep(25);
      if (!isOpenChannel(channel)) throw new Error('连接已关闭');
      const chunk = await blob.slice(offset, offset + VOID_CHUNK_SIZE).arrayBuffer();
      channel.send(chunk);
      sent += chunk.byteLength;
      onProgress?.(Math.min(100, Math.round((sent / Math.max(1, blob.size)) * 100)));
    }
    channel.send(JSON.stringify({ kind: 'file.done', id: meta.id }));
  }, []);

  const broadcastBlob = useCallback(async (blob, meta, excludePeerId = '') => {
    const targets = [...peersRef.current.entries()].filter(([peerId, entry]) => peerId !== excludePeerId && isOpenChannel(entry.channel));
    if (targets.length === 0) return;
    // 并发投递全部目标：单个 peer 失败不影响其余 peer 收到文件
    await Promise.all(targets.map(async ([, entry]) => {
      try {
        await sendBlobOnChannel(entry.channel, blob, meta);
      } catch {
        // 失败目标单独忽略（房间会话中 peer 掉线是常态），剩余目标继续投递。
      }
    }));
  }, [sendBlobOnChannel]);

  const handleChannelMessage = useCallback(async (peerId, event) => {
    const entry = peersRef.current.get(peerId);
    if (!entry) return;
    if (typeof event.data === 'string') {
      let payload;
      try {
        payload = JSON.parse(event.data);
      } catch {
        return;
      }
      if (payload.kind === 'room.hello') {
        updatePeerStatus(peerId, 'connected');
        return;
      }
      if (payload.kind === 'chat.text') {
        upsertMessage({ ...payload, kind: 'text', status: 'done', progress: 100 });
        if (roleRef.current === 'owner') broadcastJSON(payload, peerId);
        sendJSON(entry.channel, { kind: 'receipt', id: payload.id });
        return;
      }
      if (payload.kind === 'file.meta') {
        entry.incoming = { meta: payload, chunks: [], received: 0 };
        upsertMessage({
          id: payload.id,
          kind: payload.mime?.startsWith('image/') ? 'image' : 'file',
          from: payload.from,
          name: payload.name,
          fileName: payload.fileName,
          size: payload.size,
          mime: payload.mime,
          createdAt: payload.createdAt,
          progress: 0,
          status: 'receiving',
        });
        return;
      }
      if (payload.kind === 'file.done' && entry.incoming?.meta?.id === payload.id) {
        const meta = entry.incoming.meta;
        const blob = new Blob(entry.incoming.chunks, { type: meta.mime || 'application/octet-stream' });
        const url = meta.mime?.startsWith('image/') ? createObjectURL(blob) : '';
        upsertMessage({
          id: meta.id,
          kind: meta.mime?.startsWith('image/') ? 'image' : 'file',
          from: meta.from,
          name: meta.name,
          fileName: meta.fileName,
          size: meta.size || blob.size,
          mime: meta.mime,
          blob,
          url,
          progress: 100,
          status: 'done',
        });
        entry.incoming = null;
        sendJSON(entry.channel, { kind: 'receipt', id: meta.id });
        if (roleRef.current === 'owner') {
          await broadcastBlob(blob, meta, peerId).catch(() => {});
        }
        return;
      }
      if (payload.kind === 'file.error') {
        upsertMessage({ id: payload.id, status: 'failed', error: payload.error || '传输失败' });
      }
      return;
    }

    if (event.data instanceof ArrayBuffer && entry.incoming) {
      entry.incoming.chunks.push(event.data);
      entry.incoming.received += event.data.byteLength;
      const meta = entry.incoming.meta;
      upsertMessage({
        id: meta.id,
        progress: Math.min(100, Math.round((entry.incoming.received / Math.max(1, meta.size || entry.incoming.received)) * 100)),
      });
    }
  }, [broadcastBlob, broadcastJSON, sendJSON, updatePeerStatus, upsertMessage]);

  const bindChannel = useCallback((peerId, channel) => {
    const entry = peersRef.current.get(peerId) || {};
    entry.channel = channel;
    peersRef.current.set(peerId, entry);
    channel.binaryType = 'arraybuffer';
    channel.onopen = () => {
      if (entry.closing) return;
      updatePeerStatus(peerId, 'connected');
      setError('');
      setStatus(roleRef.current === 'owner' ? '房间已连接' : '已连接房主');
      const currentParticipant = participantRef.current;
      sendJSON(channel, {
        kind: 'room.hello',
        id: newMessageId(),
        from: currentParticipant?.id,
        name: currentParticipant?.name || (roleRef.current === 'owner' ? '房主' : deviceName()),
        createdAt: Date.now(),
      });
    };
    channel.onclose = () => {
      if (!entry.closing) {
        updatePeerStatus(peerId, 'closed');
        setStatus(roleRef.current === 'owner' ? '等待设备重连' : '等待房主重连');
      }
    };
    channel.onerror = () => {
      if (entry.closing) return;
      updatePeerStatus(peerId, 'failed');
      setError('直连失败，可能是 AP 隔离、防火墙或双方不在同一可达网络。');
    };
    channel.onmessage = (event) => handleChannelMessage(peerId, event);
  }, [handleChannelMessage, sendJSON, updatePeerStatus]);

  const ensurePeer = useCallback(async (peerId, initiator, options = {}) => {
    const existing = peersRef.current.get(peerId);
    if (options.reset && existing) {
      closePeer(peerId, 'connecting');
    } else if (existing?.peer && existing.peer.connectionState !== 'closed') {
      const staleConnecting = existing.startedAt && Date.now() - existing.startedAt > VOID_CONNECT_TIMEOUT_MS && ['new', 'connecting', 'disconnected'].includes(existing.peer.connectionState);
      if (staleConnecting) {
        closePeer(peerId, 'connecting');
      } else {
        return existing.peer;
      }
    }
    const peer = new RTCPeerConnection({ iceServers: VOID_ICE_SERVERS, iceCandidatePoolSize: 4 });
    const entry = { peer, channel: null, incoming: null, closing: false, startedAt: Date.now() };
    peersRef.current.set(peerId, entry);
    updatePeerStatus(peerId, 'connecting');
    if (!Object.values(connectedPeersRef.current).includes('connected')) {
      setStatus(roleRef.current === 'owner' ? '正在建立直连' : '正在连接房主');
    }
    peer.onicecandidate = (event) => {
      if (event.candidate) postSignal(peerId, 'webrtc.ice', event.candidate.toJSON()).catch(() => {});
    };
    peer.onconnectionstatechange = () => {
      const currentEntry = peersRef.current.get(peerId);
      if (currentEntry?.peer !== peer || currentEntry?.closing) return;
      updatePeerStatus(peerId, peer.connectionState);
      if (peer.connectionState === 'connected') {
        setError('');
      } else if (peer.connectionState === 'failed') {
        setStatus(roleRef.current === 'owner' ? '等待设备重连' : '等待房主重连');
        setError('直连中断，可能是 AP 隔离、防火墙或网络切换。');
      }
    };
    peer.ondatachannel = (event) => bindChannel(peerId, event.channel);
    if (initiator) {
      const channel = peer.createDataChannel('void-room', { ordered: true });
      bindChannel(peerId, channel);
      const offer = await peer.createOffer();
      await peer.setLocalDescription(offer);
      await postSignal(peerId, 'webrtc.offer', peer.localDescription);
    }
    return peer;
  }, [bindChannel, closePeer, postSignal, updatePeerStatus]);

  const handleSignal = useCallback(async (signal) => {
    if (!signal?.type || signal.from === participantRef.current?.id) return;
    const peerId = signal.from;
    if (roleRef.current === 'owner' && ['participant.joined', 'participant.ready'].includes(signal.type)) {
      await ensurePeer(peerId, true, { reset: true }).catch((err) => setError(err.message || '创建直连失败'));
      return;
    }
    if (signal.type === 'webrtc.offer') {
      const peer = await ensurePeer(peerId, false, { reset: true });
      await peer.setRemoteDescription(signal.payload);
      await flushPendingIce(peerId, peer);
      const answer = await peer.createAnswer();
      await peer.setLocalDescription(answer);
      await postSignal(peerId, 'webrtc.answer', peer.localDescription);
      return;
    }
    if (signal.type === 'webrtc.answer') {
      const peer = peersRef.current.get(peerId)?.peer;
      if (peer && !peer.currentRemoteDescription) {
        await peer.setRemoteDescription(signal.payload);
        await flushPendingIce(peerId, peer);
      }
      return;
    }
    if (signal.type === 'webrtc.ice') {
      const peer = peersRef.current.get(peerId)?.peer;
      if (peer?.remoteDescription) {
        await peer.addIceCandidate(signal.payload).catch(() => {});
      } else {
        const pending = pendingIceRef.current.get(peerId) || [];
        pending.push(signal.payload);
        pendingIceRef.current.set(peerId, pending);
      }
    }
  }, [ensurePeer, flushPendingIce, postSignal]);

  const pollSignals = useCallback(async () => {
    const current = participantRef.current;
    if (!current?.id || !current?.token || stoppedRef.current) return;
    try {
      const res = await axios.get(`/api/filebox/void/rooms/${encodeURIComponent(roomId)}/signals`, {
        params: {
          participantId: current.id,
          participantToken: current.token,
          since: lastSignalIDRef.current,
        },
      });
      const data = res.data?.data || {};
      const serverLastSignalId = Number(data.room?.lastSignalId) || 0;
      if (serverLastSignalId < lastSignalIDRef.current) {
        lastSignalIDRef.current = 0;
      }
      if (data.room) setRoom(data.room);
      for (const signal of data.signals || []) {
        lastSignalIDRef.current = Math.max(lastSignalIDRef.current, Number(signal.id) || 0);
        await handleSignal(signal);
      }
    } catch (err) {
      if (err.response?.status === 404) setError('房间不存在或已过期');
      else if (err.response?.status === 403) setError('房间身份已失效，请重新进入');
    }
  }, [handleSignal, roomId]);

  useEffect(() => {
    let timer;
    const startPolling = () => {
      timer = window.setInterval(pollSignals, VOID_SIGNAL_POLL_MS);
      pollSignals();
    };
    const init = async () => {
      stoppedRef.current = false;
      if (!roomId) {
        setError('房间号无效');
        return;
      }
      try {
        const roomRes = await axios.get(`/api/filebox/void/rooms/${encodeURIComponent(roomId)}`);
        const roomData = roomRes.data?.data;
        setRoom(roomData);
        lastSignalIDRef.current = Number(roomData?.lastSignalId) || 0;
        const ownerCred = readOwnerCredentials(roomId);
        if (ownerCred?.ownerToken) {
          const owner = { id: 'owner', token: ownerCred.ownerToken, name: '房主' };
          setRole('owner');
          setParticipant(owner);
          participantRef.current = owner;
          setStatus('等待设备加入');
          startPolling();
          (roomData?.participants || [])
            .filter((item) => item.id && item.id !== 'owner' && item.online)
            .forEach((item) => {
              ensurePeer(item.id, true, { reset: true }).catch((err) => setError(err.message || '创建直连失败'));
            });
          return;
        }

        const guestKey = GUEST_STORAGE_PREFIX + roomId;
        const storedGuest = readJSONStorage(guestKey);
        const knownGuest = storedGuest && roomData?.participants?.some((item) => item.id === storedGuest.participantId);
        if (knownGuest) {
          const guest = { id: storedGuest.participantId, token: storedGuest.participantToken, name: storedGuest.name || deviceName() };
          setParticipant(guest);
          participantRef.current = guest;
          setStatus('等待房主连接');
          await postSignal('owner', 'participant.ready', { name: guest.name, createdAt: Date.now() }).catch(() => {});
          startPolling();
          return;
        }
        const joinRes = await axios.post(`/api/filebox/void/rooms/${encodeURIComponent(roomId)}/participants`, { name: deviceName(), clientId });
        const joined = joinRes.data?.data;
        const guest = { id: joined.participantId, token: joined.participantToken, name: deviceName() };
        writeJSONStorage(guestKey, {
          participantId: guest.id,
          participantToken: guest.token,
          clientId,
          name: guest.name,
        });
        setParticipant(guest);
        participantRef.current = guest;
        if (joined.room) setRoom(joined.room);
        lastSignalIDRef.current = Number(joined.room?.lastSignalId) || lastSignalIDRef.current;
        setStatus('等待房主连接');
        await postSignal('owner', 'participant.ready', { name: guest.name, createdAt: Date.now() }).catch(() => {});
        startPolling();
      } catch (err) {
        setError(err.response?.data?.error || '进入房间失败');
        setStatus('进入失败');
      }
    };
    init();
    return () => {
      stoppedRef.current = true;
      if (timer) window.clearInterval(timer);
      for (const entry of peersRef.current.values()) {
        entry.closing = true;
        entry.channel?.close?.();
        entry.peer?.close?.();
      }
      peersRef.current.clear();
      for (const message of messagesRef.current) {
        if (message.url) URL.revokeObjectURL(message.url);
      }
    };
  }, [clientId, ensurePeer, pollSignals, postSignal, roomId]);

  useEffect(() => {
    if (role !== 'guest' || !participant?.id || !participant?.token || connectedPeers.owner === 'connected') return undefined;
    const sendReady = () => {
      postSignal('owner', 'participant.ready', { name: participant.name || deviceName(), createdAt: Date.now() }).catch(() => {});
    };
    const timer = window.setInterval(sendReady, VOID_READY_RETRY_MS);
    return () => window.clearInterval(timer);
  }, [connectedPeers.owner, participant?.id, participant?.name, participant?.token, postSignal, role]);

  useEffect(() => {
    let cancelled = false;
    QRCode.toDataURL(activeLink, { width: 180, margin: 1 })
      .then((value) => { if (!cancelled) setQrCode(value); })
      .catch(() => { if (!cancelled) setQrCode(''); });
    return () => {
      cancelled = true;
    };
  }, [activeLink]);

  const sendText = async () => {
    const content = text.trim();
    if (!content || !participant) return;
    const payload = {
      kind: 'chat.text',
      id: newMessageId(),
      from: participant.id,
      name: participantName,
      text: content,
      createdAt: Date.now(),
    };
    upsertMessage({ ...payload, kind: 'text', status: 'done', progress: 100 });
    setText('');
    if (role === 'owner') {
      broadcastJSON(payload);
      return;
    }
    const ownerChannel = peersRef.current.get('owner')?.channel;
    if (!isOpenChannel(ownerChannel)) {
      setError('还没有连接到房主');
      return;
    }
    sendJSON(ownerChannel, payload);
  };

  const handleTextKeyDown = (event) => {
    if (event.key !== 'Enter' || event.shiftKey || event.ctrlKey || event.metaKey || event.altKey) return;
    event.preventDefault();
    if (canSend && text.trim()) {
      sendText();
    }
  };

  const sendFile = async (file = selectedFile) => {
    if (!file || !participant) return;
    setSelectedFile(file);
    setSending(true);
    setError('');
    const meta = {
      kind: 'file.meta',
      id: newMessageId(),
      from: participant.id,
      name: participantName,
      fileName: file.name,
      size: file.size,
      mime: file.type || 'application/octet-stream',
      createdAt: Date.now(),
    };
    upsertMessage({
      id: meta.id,
      kind: meta.mime.startsWith('image/') ? 'image' : 'file',
      from: meta.from,
      name: meta.name,
      fileName: meta.fileName,
      size: meta.size,
      mime: meta.mime,
      progress: 0,
      status: 'sending',
      createdAt: meta.createdAt,
    });
    try {
      if (role === 'owner') {
        const targets = [...peersRef.current.values()].filter((entry) => isOpenChannel(entry.channel));
        if (targets.length === 0) throw new Error('暂无已连接设备');
        for (const entry of targets) {
          await sendBlobOnChannel(entry.channel, file, meta, (progress) => upsertMessage({ id: meta.id, progress }));
        }
      } else {
        const ownerChannel = peersRef.current.get('owner')?.channel;
        if (!isOpenChannel(ownerChannel)) throw new Error('还没有连接到房主');
        await sendBlobOnChannel(ownerChannel, file, meta, (progress) => upsertMessage({ id: meta.id, progress }));
      }
      upsertMessage({ id: meta.id, progress: 100, status: 'done' });
      setSelectedFile(null);
      if (fileInputRef.current) fileInputRef.current.value = '';
    } catch (err) {
      upsertMessage({ id: meta.id, status: 'failed' });
      setError(err.message || '文件发送失败');
    } finally {
      setSending(false);
    }
  };

  const handleFileChange = (event) => {
    const file = event.target.files?.[0];
    if (!file) return;
    sendFile(file);
  };

  const closeRoom = async () => {
    const ownerToken = participant?.token || readJSONStorage(OWNER_STORAGE_PREFIX + roomId)?.ownerToken;
    if (!ownerToken) return;
    try {
      await axios.delete(`/api/filebox/void/rooms/${encodeURIComponent(roomId)}`, { headers: { 'X-Void-Owner-Token': ownerToken } });
      sessionStorage.removeItem(OWNER_STORAGE_PREFIX + roomId);
      localStorage.removeItem(OWNER_STORAGE_PREFIX + roomId);
      toast.success('房间已关闭');
      window.location.href = '/filebox';
    } catch (err) {
      setError(err.response?.data?.error || '关闭房间失败');
    }
  };

  const participants = sortedParticipants(room?.participants);
  const roomMode = room?.mode || (room?.persistent ? 'persistent' : 'temporary');
  const isPersistentRoom = roomMode === 'persistent';
  const openPeerCount = Object.values(connectedPeers).filter((value) => value === 'connected').length;
  const canSend = role === 'owner' ? openPeerCount > 0 : connectedPeers.owner === 'connected';
  const connectionStatusLabel = canSend && role === 'owner' ? `已直连 ${openPeerCount} 台` : canSend ? '已直连' : status;
  const participantConnection = (item) => {
    if (role === 'guest' && item.role === 'owner') return connectedPeers.owner;
    if (role === 'owner' && item.id !== 'owner') return connectedPeers[item.id];
    return item.online ? 'self' : '';
  };
  const participantBadge = (item) => {
    const state = participantConnection(item);
    if (state === 'connected' || state === 'self') return { variant: 'success', label: state === 'connected' ? '已直连' : '在线' };
    if (['new', 'connecting', 'checking', 'disconnected'].includes(state)) return { variant: 'warning', label: '直连中' };
    if (state === 'failed' || state === 'closed') return { variant: 'warning', label: '失败' };
    return { variant: item.online ? 'secondary' : 'secondary', label: item.online ? '在线' : '离线' };
  };

  return (
    <div className="min-h-screen bg-kumo-canvas p-4 text-kumo-default sm:p-5">
      <div className="mx-auto flex w-full max-w-7xl flex-col gap-4">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-line pb-3">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-base font-bold text-kumo-strong">虚空传输</h1>
              <Badge variant={role === 'owner' ? 'success' : 'secondary'}>{role === 'owner' ? '房主' : '访客'}</Badge>
              <Badge variant={isPersistentRoom ? 'success' : 'secondary'}>{isPersistentRoom ? '持久' : '临时'}</Badge>
              <Badge variant={canSend ? 'success' : 'warning'}>{connectionStatusLabel}</Badge>
            </div>
            <div className="mt-1 font-mono text-xs text-kumo-subtle">{roomId || '-'}</div>
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="secondary" onClick={() => pollSignals()} icon={<RefreshCw className="h-4 w-4" />}>刷新</Button>
            {role === 'owner' && <Button size="sm" variant="secondary-destructive" onClick={closeRoom} icon={<X className="h-4 w-4" />}>关闭房间</Button>}
          </div>
        </div>

        <div className="grid items-stretch gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
          <SectionCard title="房间会话" icon={<Send className="h-4 w-4 text-brand" />} meta={<Badge variant="secondary">P2P</Badge>} className="h-full" bodyClassName="flex min-h-[30rem] flex-1 flex-col gap-3">
            {error && (
              <div className="flex items-start gap-2 rounded-md border border-kumo-error/30 bg-kumo-error/10 p-3 text-xs font-semibold text-kumo-error">
                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <div className="min-h-40 flex-1 overflow-auto rounded-md border border-kumo-line bg-kumo-recessed/20 p-3">
              {messages.length === 0 ? (
                <div className="flex min-h-32 items-center justify-center text-center text-xs text-kumo-subtle">
                  {canSend ? '直连已建立' : '等待直连建立'}
                </div>
              ) : (
                <div className="grid gap-3">
                  {messages.map((message) => (
                    <MessageBubble key={message.id} message={message} mine={message.from === participant?.id} />
                  ))}
                </div>
              )}
            </div>

            {selectedFile && (
              <div className="flex items-center justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-2 text-xs">
                <div className="min-w-0">
                  <div className="truncate font-semibold text-kumo-strong">{selectedFile.name}</div>
                  <div className="text-kumo-subtle">{formatFileSize(selectedFile.size)}</div>
                </div>
                <Button size="sm" variant="secondary" onClick={() => { setSelectedFile(null); if (fileInputRef.current) fileInputRef.current.value = ''; }} icon={<X className="h-3.5 w-3.5" />}>移除</Button>
              </div>
            )}

            <div className="grid gap-2 border-t border-kumo-line pt-3">
              <Textarea
                label="消息"
                value={text}
                onChange={(event) => setText(event.target.value)}
                onKeyDown={handleTextKeyDown}
                className="min-h-20 text-sm"
                placeholder={canSend ? '输入文字' : '等待连接后发送'}
              />
              <div className="flex flex-wrap justify-end gap-2">
                <Input ref={fileInputRef} type="file" aria-label="选择文件" className="hidden" onChange={handleFileChange} />
                <Button size="sm" variant="secondary" disabled={!canSend || sending} loading={sending} onClick={() => fileInputRef.current?.click()} icon={<Paperclip className="h-4 w-4" />}>文件</Button>
                <Button size="sm" variant="primary" disabled={!canSend || !text.trim()} onClick={sendText} icon={<Send className="h-4 w-4" />}>发送</Button>
              </div>
            </div>
          </SectionCard>

          <div className="flex h-full flex-col gap-4">
            {role === 'owner' && (
              <SectionCard title="房间入口" icon={<Users className="h-4 w-4 text-brand" />} bodyClassName="grid min-w-0 gap-3">
                <div className="grid min-w-0 gap-3">
                  {qrCode && (
                    <div className="flex justify-center rounded-md border border-kumo-line bg-kumo-base p-3">
                      <img src={qrCode} alt="虚空传输二维码" className="h-36 w-36 rounded p-1.5" />
                    </div>
                  )}
                  <div className="grid min-w-0 gap-2">
                    <div className="flex items-center justify-between gap-3 text-xs">
                      <span className="font-semibold text-kumo-strong">当前入口</span>
                      <Badge variant={isPersistentRoom ? 'success' : 'secondary'}>{isPersistentRoom ? '持久房间' : '临时房间'}</Badge>
                    </div>
                    <ClipboardText text={activeLink} tooltip={{ text: '复制链接', copiedText: '链接已复制' }} labels={{ copyAction: '复制链接' }} />
                  </div>
                </div>
              </SectionCard>
            )}

            <SectionCard title="连接状态" icon={<Users className="h-4 w-4 text-brand" />}>
              <div className="grid gap-3 text-xs">
                <div className="grid grid-cols-2 gap-2 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
                  <div><span className="text-kumo-subtle">有效期</span><div className="mt-1 font-semibold text-kumo-strong">{isPersistentRoom ? '持久' : room?.expiresAt ? formatDateTime(room.expiresAt) : '-'}</div></div>
                  <div><span className="text-kumo-subtle">在线</span><div className="mt-1 font-semibold text-kumo-strong">{participants.filter((item) => item.online).length}</div></div>
                </div>
                <div className="divide-y divide-kumo-line rounded-md border border-kumo-line">
                  {participants.length === 0 ? (
                    <div className="p-3 text-center text-kumo-subtle">暂无设备</div>
                  ) : participants.map((item) => {
                    const badge = participantBadge(item);
                    return (
                      <div key={item.id} className="flex items-center justify-between gap-3 px-3 py-2">
                        <div className="min-w-0">
                          <div className="truncate font-semibold text-kumo-strong">{item.role === 'owner' ? '房主' : item.name || '访客'}</div>
                          <div className="font-mono text-[10px] text-kumo-subtle">{item.id}</div>
                        </div>
                        <Badge variant={badge.variant}>{badge.label}</Badge>
                      </div>
                    );
                  })}
                </div>
              </div>
            </SectionCard>
          </div>
        </div>
      </div>
    </div>
  );
}

export default VoidRoomPage;
