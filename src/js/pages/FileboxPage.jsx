import React, { useEffect, useRef, useState } from 'react';
import axios from 'axios';
import QRCode from 'qrcode';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { ClipboardText, LayerCard, Meter, Tabs } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { formatDateTime, formatFileSize } from '../modules/utils.js';
import {
  Clock,
  FileText,
  FolderOpen,
  History,
  Lock,
  RefreshCw,
  Send,
  Settings,
  Trash,
  Upload,
} from '../components/Icons.jsx';

const DEFAULT_FILEBOX_MAX_FILE_SIZE = 100 * 1024 * 1024;
const EXPIRY_OPTIONS = [
  { value: '0', label: '永久有效' },
  { value: '1', label: '1 小时' },
  { value: '6', label: '6 小时' },
  { value: '24', label: '1 天' },
  { value: '72', label: '3 天' },
  { value: '168', label: '7 天' },
];
const SHARE_TYPE_TABS = [
  { value: 'file', label: <span className="inline-flex items-center gap-1.5"><FolderOpen className="h-3.5 w-3.5" />文件</span> },
  { value: 'text', label: <span className="inline-flex items-center gap-1.5"><FileText className="h-3.5 w-3.5" />文本</span> },
];
const PAGE_TABS = [
  { value: 'share', label: <span className="inline-flex items-center gap-1.5"><Send className="h-3.5 w-3.5" />创建分享</span> },
  { value: 'void', label: <span className="inline-flex items-center gap-1.5"><Send className="h-3.5 w-3.5" />虚空传输</span> },
  { value: 'history', label: <span className="inline-flex items-center gap-1.5"><History className="h-3.5 w-3.5" />分享记录</span> },
  { value: 'settings', label: <span className="inline-flex items-center gap-1.5"><Settings className="h-3.5 w-3.5" />策略</span> },
];

const VOID_CHUNK_SIZE = 64 * 1024;
const VOID_ICE_SERVERS = [{ urls: 'stun:stun.l.google.com:19302' }, { urls: 'stun:global.stun.twilio.com:3478' }];

function formatSpeed(bytesPerSecond) {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return '-';
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  let value = bytesPerSecond;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(value >= 10 ? 1 : 2)} ${units[index]}`;
}

function authHeaders() {
  return { 'x-admin-password': localStorage.getItem('admin_password') || '' };
}

function downloadURL(code) {
  return `${window.location.origin}/api/filebox/download/${code}`;
}

function formatExpiry(value) {
  return Number(value) === 0 ? '永久有效' : formatDateTime(value);
}

function expiryLabel(value) {
  return EXPIRY_OPTIONS.find((item) => item.value === String(value))?.label || `${value} 小时`;
}

function sleep(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function percentOf(done, total) {
  return Math.min(100, Math.round((done / Math.max(1, total || done || 1)) * 100));
}

function normalizeVoidRoom(value) {
  return value.replace(/[^A-Z0-9]/gi, '').toUpperCase().slice(0, 12);
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

function EntryName({ entry }) {
  const isFile = entry.type === 'file';
  return (
    <div className="flex min-w-0 items-center gap-2">
      <div className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md border ${isFile ? 'border-kumo-brand/20 bg-kumo-brand/10 text-kumo-brand' : 'border-kumo-success/20 bg-kumo-success/10 text-kumo-success'}`}>
        {isFile ? <FolderOpen className="h-4 w-4" /> : <FileText className="h-4 w-4" />}
      </div>
      <div className="min-w-0">
        <div className="truncate text-xs font-semibold text-kumo-strong">{isFile ? entry.originalName || entry.filename || '文件分享' : entry.preview || entry.content || '文本分享'}</div>
        <div className="mt-0.5 text-[11px] text-kumo-subtle">{isFile ? formatFileSize(entry.size || 0) : '文本内容'}</div>
      </div>
    </div>
  );
}

function FileboxPage({ publicVoidOnly = false } = {}) {
  const [activeTab, setActiveTab] = useState(publicVoidOnly ? 'void' : 'share');
  const [shareType, setShareType] = useState('file');
  const [shareText, setShareText] = useState('');
  const [selectedFile, setSelectedFile] = useState(null);
  const [expiry, setExpiry] = useState('24');
  const [burnAfterReading, setBurnAfterReading] = useState(false);
  const [maxDownloads, setMaxDownloads] = useState('');
  const [accessPassword, setAccessPassword] = useState('');
  const [result, setResult] = useState(null);
  const [qrCode, setQrCode] = useState('');
  const [loading, setLoading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadSpeed, setUploadSpeed] = useState('-');
  const [localHistory, setLocalHistory] = useState([]);
  const [serverHistory, setServerHistory] = useState([]);
  const [accessLogs, setAccessLogs] = useState([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [settingsLoading, setSettingsLoading] = useState(false);
  const [fileboxSettings, setFileboxSettings] = useState({ max_file_size: DEFAULT_FILEBOX_MAX_FILE_SIZE, allowed_mime_types: [], default_expiry_hours: 24, public_upload_enabled: false });
  const [settingsMimeText, setSettingsMimeText] = useState('');
  const [voidFile, setVoidFile] = useState(null);
  const [voidText, setVoidText] = useState('');
  const [voidRoom, setVoidRoom] = useState('');
  const [voidLink, setVoidLink] = useState('');
  const [voidQr, setVoidQr] = useState('');
  const [voidStatus, setVoidStatus] = useState('空闲');
  const [voidProgress, setVoidProgress] = useState(0);
  const [voidReceiveName, setVoidReceiveName] = useState('');
  const [voidReceiveSize, setVoidReceiveSize] = useState(0);
  const [voidRole, setVoidRole] = useState(publicVoidOnly ? 'receiver' : 'sender');
  const [voidExpiresAt, setVoidExpiresAt] = useState(0);
  const [voidSpeed, setVoidSpeed] = useState(0);
  const [voidTransferred, setVoidTransferred] = useState(0);
  const [voidStartedAt, setVoidStartedAt] = useState(0);
  const [voidReceivedText, setVoidReceivedText] = useState('');
  const [voidMeta, setVoidMeta] = useState(null);
  const [voidError, setVoidError] = useState('');
  const fileInputRef = useRef(null);
  const voidFileInputRef = useRef(null);
  const abortControllerRef = useRef(null);
  const voidPeerRef = useRef(null);
  const voidChannelRef = useRef(null);
  const autoReceiveRoomRef = useRef('');
  const voidSpeedSampleRef = useRef({ bytes: 0, time: 0 });
  const maxFileSize = fileboxSettings.max_file_size || DEFAULT_FILEBOX_MAX_FILE_SIZE;

  const loadLocalHistory = () => {
    try {
      setLocalHistory(JSON.parse(localStorage.getItem('filebox_history') || '[]'));
    } catch {
      setLocalHistory([]);
    }
  };

  const loadSettings = async () => {
    setSettingsLoading(true);
    try {
      const res = await axios.get('/api/filebox/settings', { headers: authHeaders() });
      if (res.data?.success && res.data.data) {
        setFileboxSettings(res.data.data);
        setSettingsMimeText((res.data.data.allowed_mime_types || []).join(', '));
        if (!expiry) setExpiry(String(res.data.data.default_expiry_hours || 24));
      }
    } finally {
      setSettingsLoading(false);
    }
  };

  const loadServerHistory = async () => {
    setHistoryLoading(true);
    try {
      const [historyRes, logsRes] = await Promise.all([
        axios.get('/api/filebox/history', { headers: authHeaders() }),
        axios.get('/api/filebox/access-logs', { headers: authHeaders() }),
      ]);
      if (historyRes.data?.success) setServerHistory(Array.isArray(historyRes.data.data) ? historyRes.data.data : []);
      if (logsRes.data?.success) setAccessLogs(Array.isArray(logsRes.data.data) ? logsRes.data.data : []);
    } catch (error) {
      toast.error(error.response?.data?.error || '加载文件柜记录失败');
    } finally {
      setHistoryLoading(false);
    }
  };

  useEffect(() => {
    if (!publicVoidOnly) {
      loadLocalHistory();
      loadSettings();
    }
    const room = new URLSearchParams(window.location.search).get('void');
    if (room) {
      setActiveTab('void');
      setVoidRoom(room.toUpperCase());
    }
  }, [publicVoidOnly]);

  useEffect(() => {
    if (activeTab === 'history') loadServerHistory();
  }, [activeTab]);

  const saveLocalHistory = (entry) => {
    const next = [entry, ...localHistory.filter((item) => item.code !== entry.code)].slice(0, 50);
    setLocalHistory(next);
    localStorage.setItem('filebox_history', JSON.stringify(next));
  };

  const selectFile = (file) => {
    if (!file) return;
    if (file.size > maxFileSize) {
      toast.error(`文件过大，最大支持 ${formatFileSize(maxFileSize)}`);
      return;
    }
    setSelectedFile(file);
    setShareType('file');
    setResult(null);
  };

  const resetShare = () => {
    setShareText('');
    setSelectedFile(null);
    setAccessPassword('');
    setMaxDownloads('');
    setBurnAfterReading(false);
    setResult(null);
    setQrCode('');
    setUploadProgress(0);
    setUploadSpeed('-');
    if (fileInputRef.current) fileInputRef.current.value = '';
  };

  const generateQrCode = async (code) => {
    try {
      setQrCode(await QRCode.toDataURL(downloadURL(code), { width: 132, margin: 1, color: { dark: '#111827', light: '#ffffff' } }));
    } catch {
      setQrCode('');
    }
  };

  const createShare = async () => {
    const isText = shareType === 'text';
    if (isText && !shareText.trim()) return toast.warning('请输入要分享的文本');
    if (!isText && !selectedFile) return toast.warning('请选择要分享的文件');

    setLoading(true);
    setUploadProgress(0);
    setUploadSpeed('-');
    abortControllerRef.current = new AbortController();
    let lastTime = Date.now();
    let lastLoaded = 0;

    try {
      const formData = new FormData();
      formData.append('type', shareType);
      formData.append('expiry', expiry);
      formData.append('burn_after_reading', burnAfterReading);
      formData.append('max_downloads', maxDownloads || '0');
      formData.append('access_password', accessPassword);
      if (isText) formData.append('text', shareText);
      else formData.append('file', selectedFile);

      const res = await axios.post('/api/filebox/share', formData, {
        headers: { ...authHeaders(), 'Content-Type': 'multipart/form-data' },
        signal: abortControllerRef.current.signal,
        onUploadProgress: (event) => {
          if (!event?.total || isText) return;
          const now = Date.now();
          const speed = ((event.loaded - lastLoaded) * 1000) / Math.max(1, now - lastTime);
          setUploadProgress(Math.round((event.loaded / event.total) * 100));
          setUploadSpeed(formatSpeed(speed));
          lastLoaded = event.loaded;
          lastTime = now;
        },
      });

      if (!res.data?.success) throw new Error(res.data?.error || '分享失败');
      const entry = {
        code: res.data.code,
        type: shareType,
        originalName: selectedFile?.name || '',
        content: shareText,
        size: selectedFile?.size || 0,
        createdAt: Date.now(),
        requiresPassword: !!accessPassword,
      };
      setResult(entry);
      saveLocalHistory(entry);
      setUploadProgress(100);
      await generateQrCode(res.data.code);
      toast.success('分享已创建');
    } catch (error) {
      if (!axios.isCancel(error)) toast.error(error.response?.data?.error || error.message || '分享失败');
    } finally {
      setLoading(false);
      abortControllerRef.current = null;
    }
  };

  const deleteEntry = async (code) => {
    if (!(await dialog.confirm(`确定删除分享 ${code}？`))) return;
    try {
      await axios.delete(`/api/filebox/${code}`, { headers: authHeaders() });
      toast.success('分享已删除');
    } catch (error) {
      toast.error(error.response?.data?.error || '删除失败');
    }
    const localNext = localHistory.filter((item) => item.code !== code);
    setLocalHistory(localNext);
    localStorage.setItem('filebox_history', JSON.stringify(localNext));
    setServerHistory((prev) => prev.filter((item) => item.code !== code));
  };

  const runCleanup = async () => {
    if (!(await dialog.confirm('清理所有过期分享？'))) return;
    setHistoryLoading(true);
    try {
      const res = await axios.post('/api/filebox/jobs/cleanup', {}, { headers: authHeaders() });
      const deleted = res.data?.data?.result?.deleted ?? res.data?.data?.deleted ?? 0;
      toast.success(`已清理 ${deleted} 条过期分享`);
      await loadServerHistory();
    } catch (error) {
      toast.error(error.response?.data?.error || '清理失败');
    } finally {
      setHistoryLoading(false);
    }
  };

  const saveSettings = async () => {
    setSettingsLoading(true);
    try {
      const allowedMimeTypes = settingsMimeText.split(/[,，\n]/).map((item) => item.trim()).filter(Boolean);
      const res = await axios.put('/api/filebox/settings', { ...fileboxSettings, allowed_mime_types: allowedMimeTypes }, { headers: authHeaders() });
      if (!res.data?.success) throw new Error(res.data?.error || '保存失败');
      setFileboxSettings(res.data.data);
      setSettingsMimeText((res.data.data.allowed_mime_types || []).join(', '));
      toast.success('策略已保存');
    } catch (error) {
      toast.error(error.response?.data?.error || error.message || '保存失败');
    } finally {
      setSettingsLoading(false);
    }
  };

  const copyLink = async (code) => {
    await navigator.clipboard.writeText(downloadURL(code));
    toast.success('下载链接已复制');
  };

  const resetVoidTransfer = (status = '空闲') => {
    closeVoidPeer();
    setVoidStatus(status);
    setVoidProgress(0);
    setVoidSpeed(0);
    setVoidTransferred(0);
    setVoidStartedAt(0);
    setVoidReceiveName('');
    setVoidReceiveSize(0);
    setVoidReceivedText('');
    setVoidMeta(null);
    setVoidError('');
    voidSpeedSampleRef.current = { bytes: 0, time: 0 };
  };

  const updateVoidProgress = (done, total, startedAt = voidStartedAt) => {
    setVoidTransferred(done);
    setVoidProgress(percentOf(done, total));
    const now = Date.now();
    const last = voidSpeedSampleRef.current;
    if (!last.time) {
      voidSpeedSampleRef.current = { bytes: done, time: now };
      return;
    }
    const elapsed = Math.max(0.2, (now - last.time) / 1000);
    if (now - last.time >= 250 || done >= total) {
      setVoidSpeed(Math.max(0, (done - last.bytes) / elapsed));
      voidSpeedSampleRef.current = { bytes: done, time: now };
    } else if (startedAt) {
      setVoidSpeed(done / Math.max(0.2, (now - startedAt) / 1000));
    }
  };

  const postVoidSignal = async (room, kind, payload) => {
    await axios.post(`/api/filebox/void/rooms/${encodeURIComponent(room)}/${kind}`, payload, { headers: authHeaders() });
  };

  const getVoidSignal = async (room, kind) => {
    const res = await axios.get(`/api/filebox/void/rooms/${encodeURIComponent(room)}/${kind}`);
    return res.data?.data;
  };

  const closeVoidPeer = () => {
    voidChannelRef.current?.close?.();
    voidPeerRef.current?.close?.();
    voidChannelRef.current = null;
    voidPeerRef.current = null;
  };

  const sendVoidPayload = async (channel, payload) => {
    channel.send(JSON.stringify(payload));
  };

  const startVoidSend = async () => {
    if (!voidFile && !voidText.trim()) return toast.warning('请选择文件或输入文本');
    resetVoidTransfer('创建房间');
    setVoidRole('sender');
    try {
      const roomRes = await axios.post('/api/filebox/void/rooms', {}, { headers: authHeaders() });
      const room = roomRes.data?.data?.id;
      const expiresAt = roomRes.data?.data?.expires_at || 0;
      if (!room) throw new Error('创建虚空房间失败');
      const link = `${window.location.origin}/filebox?void=${room}`;
      setVoidRoom(room);
      setVoidLink(link);
      setVoidExpiresAt(expiresAt);
      setVoidQr(await QRCode.toDataURL(link, { width: 132, margin: 1 }));

      const peer = new RTCPeerConnection({ iceServers: VOID_ICE_SERVERS });
      voidPeerRef.current = peer;
      const channel = peer.createDataChannel('void-filebox', { ordered: true });
      voidChannelRef.current = channel;
      const payloadBlob = voidFile || new Blob([voidText], { type: 'text/plain;charset=utf-8' });
      const meta = voidFile
        ? { kind: 'meta', type: 'file', name: voidFile.name, size: voidFile.size, mime: voidFile.type || 'application/octet-stream' }
        : { kind: 'meta', type: 'text', name: '虚空文本.txt', size: payloadBlob.size, mime: 'text/plain;charset=utf-8' };
      setVoidMeta(meta);
      setVoidReceiveName(meta.name);
      setVoidReceiveSize(meta.size);

      peer.onconnectionstatechange = () => {
        if (['failed', 'disconnected'].includes(peer.connectionState)) {
          setVoidError('直连中断，请重新创建房间或检查双方网络');
          setVoidStatus('连接中断');
        }
      };
      peer.onicecandidate = (event) => {
        if (event.candidate) postVoidSignal(room, 'sender-candidate', event.candidate.toJSON()).catch(() => {});
      };
      channel.onopen = async () => {
        const startedAt = Date.now();
        setVoidStartedAt(startedAt);
        setVoidStatus('直连已建立，开始发送');
        await sendVoidPayload(channel, meta);
        let sent = 0;
        for (let offset = 0; offset < payloadBlob.size; offset += VOID_CHUNK_SIZE) {
          while (channel.bufferedAmount > 4 * 1024 * 1024) await sleep(25);
          if (channel.readyState !== 'open') throw new Error('连接已关闭，发送中断');
          const chunk = await payloadBlob.slice(offset, offset + VOID_CHUNK_SIZE).arrayBuffer();
          channel.send(chunk);
          sent += chunk.byteLength;
          updateVoidProgress(sent, payloadBlob.size, startedAt);
        }
        await sendVoidPayload(channel, { kind: 'done' });
        setVoidSpeed(payloadBlob.size / Math.max(0.2, (Date.now() - startedAt) / 1000));
        setVoidProgress(100);
        setVoidTransferred(payloadBlob.size);
        setVoidStatus('发送完成');
      };
      channel.onerror = () => {
        setVoidError('数据通道异常，请重新创建虚空传输');
      };
      channel.onclose = () => setVoidStatus((prev) => (prev === '发送完成' ? prev : '连接已关闭'));

      const offer = await peer.createOffer();
      await peer.setLocalDescription(offer);
      await postVoidSignal(room, 'offer', offer);
      setVoidStatus('等待接收方打开链接');

      const seen = new Set();
      for (let i = 0; i < 300; i += 1) {
        if (!voidPeerRef.current) return;
        const answer = await getVoidSignal(room, 'answer');
        if (answer && !peer.currentRemoteDescription) await peer.setRemoteDescription(answer);
        const candidates = await getVoidSignal(room, 'receiver-candidates');
        if (Array.isArray(candidates)) {
          for (const candidate of candidates) {
            const key = candidate?.candidate;
            if (key && !seen.has(key)) {
              seen.add(key);
              await peer.addIceCandidate(candidate).catch(() => {});
            }
          }
        }
        if (channel.readyState === 'open') return;
        await sleep(1000);
      }
      setVoidError('等待超时，接收方未建立直连');
      setVoidStatus('等待超时');
    } catch (error) {
      setVoidError(error.response?.data?.error || error.message || '虚空传输创建失败');
      setVoidStatus('创建失败');
      toast.error(error.response?.data?.error || error.message || '虚空传输创建失败');
    }
  };

  const startVoidReceive = async () => {
    const room = normalizeVoidRoom(voidRoom);
    if (!room) return toast.warning('请输入虚空房间号');
    resetVoidTransfer('读取发送方信令');
    setVoidRole('receiver');
    setVoidRoom(room);
    try {
      const peer = new RTCPeerConnection({ iceServers: VOID_ICE_SERVERS });
      voidPeerRef.current = peer;
      const chunks = [];
      let meta = null;
      let received = 0;
      let startedAt = 0;
      let completed = false;

      peer.onconnectionstatechange = () => {
        if (peer.connectionState === 'connected') setVoidStatus('直连已建立，等待数据');
        if (['failed', 'disconnected'].includes(peer.connectionState)) {
          setVoidError('直连中断，请让发送方重新创建房间');
          setVoidStatus('连接中断');
        }
      };
      peer.onicecandidate = (event) => {
        if (event.candidate) postVoidSignal(room, 'receiver-candidate', event.candidate.toJSON()).catch(() => {});
      };
      peer.ondatachannel = (event) => {
        const channel = event.channel;
        voidChannelRef.current = channel;
        channel.binaryType = 'arraybuffer';
        channel.onopen = () => setVoidStatus('直连已建立，等待数据');
        channel.onerror = () => setVoidError('数据通道异常，请重新接收');
        channel.onmessage = async (message) => {
          if (typeof message.data === 'string') {
            const payload = JSON.parse(message.data);
            if (payload.kind === 'meta') {
              meta = payload;
              startedAt = Date.now();
              setVoidStartedAt(startedAt);
              setVoidMeta(payload);
              setVoidReceiveName(payload.name || 'void-transfer.bin');
              setVoidReceiveSize(payload.size || 0);
              setVoidStatus('接收中');
              return;
            }
            if (payload.kind === 'done') {
              const blob = new Blob(chunks, { type: meta?.mime || 'application/octet-stream' });
              if (meta?.type === 'text') {
                setVoidReceivedText(await blob.text());
                setVoidStatus('接收完成，可复制文本');
              } else {
                saveBlob(blob, meta?.name || 'void-transfer.bin');
                setVoidStatus('接收完成，已触发下载');
              }
              setVoidProgress(100);
              setVoidTransferred(meta?.size || received);
              setVoidSpeed((meta?.size || received) / Math.max(0.2, (Date.now() - startedAt) / 1000));
              completed = true;
            }
            return;
          }
          if (message.data instanceof ArrayBuffer) {
            chunks.push(message.data);
            received += message.data.byteLength;
            updateVoidProgress(received, meta?.size || received, startedAt);
          }
        };
      };

      const offer = await getVoidSignal(room, 'offer');
      if (!offer) throw new Error('房间还没有发送方，稍后再试');
      await peer.setRemoteDescription(offer);
      const answer = await peer.createAnswer();
      await peer.setLocalDescription(answer);
      await postVoidSignal(room, 'answer', answer);
      setVoidStatus('等待直连建立');

      const seen = new Set();
      for (let i = 0; i < 300; i += 1) {
        if (!voidPeerRef.current) return;
        const candidates = await getVoidSignal(room, 'sender-candidates');
        if (Array.isArray(candidates)) {
          for (const candidate of candidates) {
            const key = candidate?.candidate;
            if (key && !seen.has(key)) {
              seen.add(key);
              await peer.addIceCandidate(candidate).catch(() => {});
            }
          }
        }
        if (completed) return;
        await sleep(1000);
      }
      setVoidError('等待超时，未收到发送方数据');
      setVoidStatus('等待超时');
    } catch (error) {
      setVoidError(error.response?.data?.error || error.message || '接收失败');
      setVoidStatus('接收失败');
      toast.error(error.response?.data?.error || error.message || '接收失败');
    }
  };

  useEffect(() => {
    if (!publicVoidOnly) return;
    const room = normalizeVoidRoom(voidRoom);
    if (!room || autoReceiveRoomRef.current === room) return;
    autoReceiveRoomRef.current = room;
    startVoidReceive();
  }, [publicVoidOnly, voidRoom]);

  const voidPayloadSize = voidReceiveSize || voidFile?.size || (voidText ? new Blob([voidText]).size : 0);
  const voidElapsedSeconds = voidStartedAt ? Math.max(0, Math.round((Date.now() - voidStartedAt) / 1000)) : 0;
  const voidStatusVariant = voidProgress >= 100 ? 'success' : voidError ? 'error' : voidRole === 'receiver' ? 'secondary' : 'success';
  const voidRoomExpiryText = voidExpiresAt ? formatDateTime(voidExpiresAt) : '房间创建后 30 分钟';
  const voidCanSend = !!voidFile || !!voidText.trim();

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 sm:gap-4">
      {!publicVoidOnly && (
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-line pb-3">
          <Tabs {...MODULE_TABS_PROPS} value={activeTab} onValueChange={setActiveTab} tabs={PAGE_TABS} />
          <div className="text-xs text-kumo-subtle">文件与文本临时分享</div>
        </div>
      )}

      {activeTab === 'share' && (
        <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(22rem,1fr)]">
          <LayerCard className="p-5">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-line pb-4">
              <div>
                <h2 className="text-base font-bold text-kumo-strong">创建分享</h2>
                <p className="mt-1 text-xs text-kumo-subtle">生成可直接访问的下载链接，支持文件、文本。</p>
              </div>
              <Tabs {...TOOL_TABS_PROPS} value={shareType} onValueChange={(value) => { setShareType(value); setResult(null); }} tabs={SHARE_TYPE_TABS} />
            </div>

            <div className="mt-4 grid gap-4">
              {shareType === 'file' ? (
                <div
                  className="rounded-md border border-dashed border-kumo-line bg-kumo-recessed/35 p-6 text-center"
                  onDragOver={(event) => event.preventDefault()}
                  onDrop={(event) => { event.preventDefault(); selectFile(event.dataTransfer.files?.[0]); }}
                >
                  <input ref={fileInputRef} type="file" className="hidden" onChange={(event) => selectFile(event.target.files?.[0])} />
                  <Upload className="mx-auto h-9 w-9 text-kumo-subtle" />
                  <div className="mt-3 text-sm font-semibold text-kumo-strong">{selectedFile ? selectedFile.name : '拖入文件或点击选择'}</div>
                  <div className="mt-1 text-xs text-kumo-subtle">最大 {formatFileSize(maxFileSize)}</div>
                  <div className="mt-4 flex justify-center"><Button size="sm" onClick={() => fileInputRef.current?.click()}>选择文件</Button></div>
                </div>
              ) : (
                <Textarea label="分享文本" value={shareText} onChange={(event) => { setShareText(event.target.value); setResult(null); }} className="min-h-56 font-mono text-sm" placeholder="粘贴需要分享的文本内容" />
              )}

              {loading && shareType === 'file' && <Meter label="上传进度" value={uploadProgress} customValue={`${uploadProgress}% · ${uploadSpeed}`} />}

              <div className="grid gap-3 md:grid-cols-2">
                <Select size="sm" label="有效期" value={expiry} onValueChange={setExpiry} items={EXPIRY_OPTIONS} />
                <Input size="sm" label="最大下载次数" type="number" min="0" value={maxDownloads} onChange={(event) => setMaxDownloads(event.target.value)} placeholder="0 或留空为不限" />
                <Input size="sm" label="访问密码" type="password" value={accessPassword} onChange={(event) => setAccessPassword(event.target.value)} placeholder="可选" />
                <div className="flex items-center justify-between rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-2">
                  <div>
                    <div className="text-xs font-semibold text-kumo-strong">阅后即焚</div>
                    <div className="text-[11px] text-kumo-subtle">首次成功下载后删除</div>
                  </div>
                  <Switch checked={burnAfterReading} onCheckedChange={setBurnAfterReading} />
                </div>
              </div>

              <div className="flex flex-wrap justify-end gap-2 border-t border-kumo-line pt-4">
                {loading && <Button size="sm" variant="secondary-destructive" onClick={() => abortControllerRef.current?.abort()}>取消上传</Button>}
                <Button size="sm" variant="secondary" onClick={resetShare}>重置</Button>
                <Button size="sm" variant="primary" onClick={createShare} loading={loading} icon={<Send className="h-4 w-4" />}>创建分享</Button>
              </div>
            </div>
          </LayerCard>

          <LayerCard className="p-5">
            <h2 className="text-base font-bold text-kumo-strong">分享结果</h2>
            {!result ? (
              <div className="mt-4 space-y-3">
                <div className="rounded-md border border-dashed border-kumo-line p-8 text-center text-xs text-kumo-subtle">创建后会在这里显示链接、二维码和取用信息。</div>
                <div className="grid gap-2 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3 text-xs">
                  <div className="flex justify-between gap-3"><span className="text-kumo-subtle">类型</span><span className="font-semibold text-kumo-strong">{shareType === 'file' ? '文件' : '文本'}</span></div>
                  <div className="flex justify-between gap-3"><span className="text-kumo-subtle">有效期</span><span className="font-semibold text-kumo-strong">{expiryLabel(expiry)}</span></div>
                  <div className="flex justify-between gap-3"><span className="text-kumo-subtle">下载次数</span><span className="font-semibold text-kumo-strong">{maxDownloads || '不限'}</span></div>
                  <div className="flex justify-between gap-3"><span className="text-kumo-subtle">访问密码</span><span className="font-semibold text-kumo-strong">{accessPassword ? '已设置' : '未设置'}</span></div>
                </div>
              </div>
            ) : (
              <div className="mt-4 space-y-4">
                <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3">
                  <div className="text-xs text-kumo-subtle">下载链接</div>
                  <ClipboardText text={downloadURL(result.code)} className="mt-2" tooltip={{ text: '复制链接', copiedText: '链接已复制' }} labels={{ copyAction: '复制链接' }} />
                </div>
                <div className="grid gap-3 sm:grid-cols-[auto_minmax(0,1fr)] sm:items-center">
                  {qrCode && <img src={qrCode} alt="分享二维码" className="h-32 w-32 rounded-md border border-kumo-line bg-white p-2" />}
                  <div className="space-y-2 text-xs text-kumo-subtle">
                    <div><span className="font-semibold text-kumo-strong">分享码:</span> <span className="font-mono text-kumo-brand">{result.code}</span></div>
                    <div>链接可直接打开；需要密码时浏览器会提示输入。</div>
                    <div>有效期: {expiryLabel(expiry)}</div>
                    {accessPassword && <Badge variant="warning">已启用访问密码</Badge>}
                  </div>
                </div>
              </div>
            )}
          </LayerCard>
        </div>
      )}

      {activeTab === 'history' && (
        <div className="grid gap-4">
          <LayerCard className="p-5">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-line pb-3">
              <div>
                <h2 className="text-base font-bold text-kumo-strong">分享记录</h2>
                <p className="mt-1 text-xs text-kumo-subtle">本地最近记录和服务端有效分享。</p>
              </div>
              <div className="flex gap-2">
                <Button size="sm" variant="secondary" onClick={loadServerHistory} loading={historyLoading} icon={<RefreshCw className="h-4 w-4" />}>刷新</Button>
                <Button size="sm" variant="secondary" onClick={runCleanup} icon={<Clock className="h-4 w-4" />}>清理过期</Button>
              </div>
            </div>
            <div className="mt-4 overflow-x-auto">
              <Table layout="fixed" className="min-w-[820px]">
                <colgroup><col /><col className="w-24" /><col className="w-32" /><col className="w-36" /><col className="w-32" /></colgroup>
                <Table.Header><Table.Row><Table.Head>内容</Table.Head><Table.Head>分享码</Table.Head><Table.Head>下载次数</Table.Head><Table.Head>到期</Table.Head><Table.Head>操作</Table.Head></Table.Row></Table.Header>
                <Table.Body>
                  {historyLoading ? Array.from({ length: 3 }).map((_, index) => (
                    <Table.Row key={index}><Table.Cell colSpan={5}><SkeletonLine className="h-8 w-full" /></Table.Cell></Table.Row>
                  )) : serverHistory.length === 0 ? (
                    <Table.Row><Table.Cell colSpan={5} className="p-8 text-center text-kumo-subtle">暂无有效分享</Table.Cell></Table.Row>
                  ) : serverHistory.map((entry) => (
                    <Table.Row key={entry.code}>
                      <Table.Cell><EntryName entry={entry} /></Table.Cell>
                      <Table.Cell className="font-mono text-xs font-semibold text-kumo-brand">{entry.code}</Table.Cell>
                      <Table.Cell className="text-xs text-kumo-subtle">{entry.downloads || 0}{entry.maxDownloads ? ` / ${entry.maxDownloads}` : ' / 不限'}</Table.Cell>
                      <Table.Cell className="text-xs text-kumo-subtle">{formatExpiry(entry.expiry)}</Table.Cell>
                      <Table.Cell><div className="flex gap-1"><Button size="sm" variant="secondary" onClick={() => copyLink(entry.code)}>复制</Button><Button size="sm" variant="secondary-destructive" onClick={() => deleteEntry(entry.code)}><Trash className="h-3.5 w-3.5" /></Button></div></Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </div>
          </LayerCard>

          <div className="grid gap-4 xl:grid-cols-2">
            <LayerCard className="p-5">
              <h2 className="text-base font-bold text-kumo-strong">本地最近创建</h2>
              <div className="mt-3 divide-y divide-kumo-line">
                {localHistory.length === 0 ? <div className="py-8 text-center text-xs text-kumo-subtle">暂无本地记录</div> : localHistory.slice(0, 8).map((entry) => (
                  <div key={entry.code} className="flex items-center justify-between gap-3 py-2.5">
                    <EntryName entry={entry} />
                    <div className="flex shrink-0 items-center gap-2"><span className="font-mono text-xs text-kumo-brand">{entry.code}</span><Button size="sm" variant="secondary" onClick={() => copyLink(entry.code)}>复制</Button></div>
                  </div>
                ))}
              </div>
            </LayerCard>

            <LayerCard className="p-5">
              <h2 className="text-base font-bold text-kumo-strong">访问日志</h2>
              <div className="mt-3 max-h-72 overflow-auto divide-y divide-kumo-line">
                {accessLogs.length === 0 ? <div className="py-8 text-center text-xs text-kumo-subtle">暂无访问日志</div> : accessLogs.slice(0, 20).map((log) => (
                  <div key={log.id} className="grid grid-cols-[5rem_5rem_minmax(0,1fr)_9rem] gap-2 py-2 text-xs">
                    <span className="font-mono text-kumo-brand">{log.code}</span>
                    <span className="text-kumo-strong">{log.action}</span>
                    <span className="truncate text-kumo-subtle">{log.ipAddress || log.userAgent || '-'}</span>
                    <span className="text-right text-kumo-subtle">{formatDateTime(log.createdAt)}</span>
                  </div>
                ))}
              </div>
            </LayerCard>
          </div>
        </div>
      )}

      {activeTab === 'settings' && (
        <LayerCard className="p-5">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-line pb-4">
            <div>
              <h2 className="text-base font-bold text-kumo-strong">文件柜策略</h2>
              <p className="mt-1 text-xs text-kumo-subtle">这些限制由后端执行，影响新创建的分享。</p>
            </div>
            <Button size="sm" variant="secondary" onClick={loadSettings} loading={settingsLoading} icon={<RefreshCw className="h-4 w-4" />}>刷新</Button>
          </div>
          <div className="mt-4 grid gap-4 md:grid-cols-2">
            <Input size="sm" label="最大文件大小 MB" type="number" min="1" value={Math.round((fileboxSettings.max_file_size || DEFAULT_FILEBOX_MAX_FILE_SIZE) / 1024 / 1024)} onChange={(event) => setFileboxSettings((prev) => ({ ...prev, max_file_size: Math.max(1, Number(event.target.value) || 1) * 1024 * 1024 }))} />
            <Input size="sm" label="默认有效期小时" type="number" min="1" value={fileboxSettings.default_expiry_hours || 24} onChange={(event) => setFileboxSettings((prev) => ({ ...prev, default_expiry_hours: Math.max(1, Number(event.target.value) || 24) }))} />
            <div className="md:col-span-2">
              <Textarea label="允许 MIME 类型" value={settingsMimeText} onChange={(event) => setSettingsMimeText(event.target.value)} className="min-h-28 font-mono text-xs" placeholder="留空表示不限制。例如 image/*, application/pdf, text/plain" />
            </div>
            <div className="md:col-span-2 flex items-center justify-between rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
              <div><div className="text-xs font-semibold text-kumo-strong">允许公开上传</div><div className="mt-1 text-[11px] text-kumo-subtle">当前接口仍要求管理员认证；保留为策略开关。</div></div>
              <Switch checked={!!fileboxSettings.public_upload_enabled} onCheckedChange={(checked) => setFileboxSettings((prev) => ({ ...prev, public_upload_enabled: checked }))} />
            </div>
          </div>
          <div className="mt-4 flex justify-end border-t border-kumo-line pt-4"><Button size="sm" variant="primary" onClick={saveSettings} loading={settingsLoading} icon={<Lock className="h-4 w-4" />}>保存策略</Button></div>
        </LayerCard>
      )}

      {activeTab === 'void' && (
        <div className={`grid items-start gap-4 ${publicVoidOnly ? 'mx-auto w-full max-w-2xl' : 'xl:grid-cols-[minmax(0,1fr)_minmax(24rem,0.9fr)]'}`}>
          {!publicVoidOnly && (
          <LayerCard className="p-5">
            <div className="flex flex-wrap items-start justify-between gap-3 border-b border-kumo-line pb-4">
              <div>
                <h2 className="text-base font-bold text-kumo-strong">虚空传输</h2>
                <p className="mt-1 text-xs text-kumo-subtle">点对点直连传输，服务器只暂存握手信令。</p>
              </div>
              <Badge variant="success">P2P</Badge>
            </div>
            <div className="mt-4 grid gap-4">
              <div
                className="rounded-md border border-dashed border-kumo-line bg-kumo-recessed/35 p-5 text-center transition-colors hover:border-kumo-brand/50"
                onDragOver={(event) => event.preventDefault()}
                onDrop={(event) => {
                  event.preventDefault();
                  const file = event.dataTransfer.files?.[0];
                  if (file) {
                    setVoidFile(file);
                    setVoidText('');
                    setVoidReceivedText('');
                  }
                }}
              >
                <input ref={voidFileInputRef} type="file" className="hidden" onChange={(event) => { setVoidFile(event.target.files?.[0] || null); setVoidText(''); }} />
                <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-md border border-kumo-line bg-kumo-base text-kumo-brand"><Upload className="h-6 w-6" /></div>
                <div className="mt-3 break-all text-sm font-semibold text-kumo-strong">{voidFile ? voidFile.name : '拖入文件或点击选择'}</div>
                <div className="mt-1 text-xs text-kumo-subtle">{voidFile ? formatFileSize(voidFile.size) : '双方页面保持打开，连接建立后浏览器直传。'}</div>
                <div className="mt-4 flex justify-center"><Button size="sm" variant="secondary" onClick={() => voidFileInputRef.current?.click()}>选择文件</Button></div>
              </div>
              <Textarea label="或发送文本" value={voidText} onChange={(event) => { setVoidText(event.target.value); setVoidFile(null); }} className="min-h-32 font-mono text-sm" placeholder="输入文本后也可以通过虚空传输" />
              <div className="flex flex-wrap justify-end gap-2 border-t border-kumo-line pt-4">
                <Button size="sm" variant="secondary" onClick={() => resetVoidTransfer('已停止')}>停止</Button>
                <Button size="sm" variant="primary" disabled={!voidCanSend} onClick={startVoidSend} icon={<Send className="h-4 w-4" />}>创建虚空传输</Button>
              </div>
            </div>
          </LayerCard>
          )}

          <LayerCard className="p-5">
            <div className="flex items-start justify-between gap-3 border-b border-kumo-line pb-4">
              <div>
                <h2 className="text-base font-bold text-kumo-strong">{publicVoidOnly ? '接收虚空传输' : '连接状态'}</h2>
                <p className="mt-1 text-xs text-kumo-subtle">{publicVoidOnly ? '保持本页打开，直连建立后会自动接收。' : '接收方扫码后在这里建立连接。'}</p>
              </div>
              <Badge variant={voidStatusVariant}>{voidProgress}%</Badge>
            </div>
            <div className="mt-4 grid gap-4">
              <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
                <Input size="sm" label="房间号" value={voidRoom} onChange={(event) => setVoidRoom(normalizeVoidRoom(event.target.value))} placeholder="扫码会自动填入" />
                <div className="flex items-end gap-2">
                  <Button size="sm" variant="primary" onClick={startVoidReceive}>{publicVoidOnly ? '重新接收' : '开始接收'}</Button>
                  <Button size="sm" variant="secondary" onClick={() => resetVoidTransfer('已停止')}>停止</Button>
                </div>
              </div>
              {publicVoidOnly && (
                <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 p-4">
                  <div className="text-xs text-kumo-subtle">当前房间</div>
                  <div className="mt-1 flex flex-wrap items-center gap-2">
                    <span className="font-mono text-2xl font-bold tracking-normal text-kumo-strong">{voidRoom || '等待房间号'}</span>
                    <Badge variant="secondary">30 分钟有效</Badge>
                  </div>
                </div>
              )}
              <div className="grid gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3 text-xs sm:grid-cols-2">
                <div><span className="text-kumo-subtle">状态</span><div className="mt-1 font-semibold text-kumo-strong">{voidStatus}</div></div>
                <div><span className="text-kumo-subtle">内容</span><div className="mt-1 truncate font-semibold text-kumo-strong">{voidReceiveName || voidFile?.name || '等待连接'}</div></div>
                <div><span className="text-kumo-subtle">大小</span><div className="mt-1 font-semibold text-kumo-strong">{formatFileSize(voidPayloadSize)}</div></div>
                <div><span className="text-kumo-subtle">速度</span><div className="mt-1 font-semibold text-kumo-strong">{formatSpeed(voidSpeed)}</div></div>
                <div><span className="text-kumo-subtle">已传输</span><div className="mt-1 font-semibold text-kumo-strong">{formatFileSize(voidTransferred)}</div></div>
                <div><span className="text-kumo-subtle">有效期</span><div className="mt-1 font-semibold text-kumo-strong">{voidRoomExpiryText}</div></div>
              </div>
              <Meter label="传输进度" value={voidProgress} customValue={`${voidProgress}%`} />
              {voidStartedAt > 0 && (
                <div className="text-xs text-kumo-subtle">已用时 {voidElapsedSeconds} 秒，{voidMeta?.type === 'text' ? '文本会在完成后直接显示。' : '文件接收完成后会自动触发浏览器下载。'}</div>
              )}
              {voidError && (
                <div className="rounded-md border border-kumo-error/30 bg-kumo-error/10 p-3 text-xs font-semibold text-kumo-error">{voidError}</div>
              )}
              {voidReceivedText && (
                <div className="rounded-md border border-kumo-line bg-kumo-base p-3">
                  <div className="mb-2 flex items-center justify-between gap-3">
                    <div className="text-xs font-semibold text-kumo-strong">接收到的文本</div>
                    <Button size="sm" variant="secondary" onClick={() => navigator.clipboard.writeText(voidReceivedText).then(() => toast.success('文本已复制'))}>复制文本</Button>
                  </div>
                  <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md border border-kumo-line bg-kumo-recessed/40 p-3 text-xs text-kumo-strong">{voidReceivedText}</pre>
                </div>
              )}
              {voidLink ? (
                <div className="grid gap-3 rounded-md border border-kumo-line bg-kumo-base p-3 sm:grid-cols-[auto_minmax(0,1fr)] sm:items-center">
                  {voidQr && <img src={voidQr} alt="虚空传输二维码" className="h-32 w-32 rounded-md border border-kumo-line bg-white p-2" />}
                  <div className="min-w-0">
                    <div className="text-xs font-semibold text-kumo-strong">让接收方扫码或打开链接</div>
                    <ClipboardText text={voidLink} className="mt-2" tooltip={{ text: '复制链接', copiedText: '链接已复制' }} labels={{ copyAction: '复制链接' }} />
                  </div>
                </div>
              ) : (
                !publicVoidOnly && <div className="rounded-md border border-dashed border-kumo-line p-8 text-center text-xs text-kumo-subtle">创建虚空传输后会显示二维码和接收链接。</div>
              )}
            </div>
          </LayerCard>
        </div>
      )}
    </div>
  );
}

export default FileboxPage;
