import React, { lazy, Suspense, useEffect, useRef, useState } from 'react';
import axios from 'axios';
import QRCode from 'qrcode';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { ClipboardText, Meter, Tabs } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { fileboxDirectURL, fileboxShareURL } from '../modules/fileboxLinks.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { formatDateTime, formatFileSize } from '../modules/utils.js';
import { Clock, ExternalLink, FileText, FolderOpen, History, Lock, RefreshCw, Send, Server, Settings, Trash, Upload, Users, X } from '../components/Icons.jsx';
import { SectionCard, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';

const MarkdownEditor = lazy(() => import('../components/ui/MarkdownEditor.jsx'));

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
  {
    value: 'file',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <FolderOpen className="h-3.5 w-3.5" />
        文件
      </span>
    ),
  },
  {
    value: 'text',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <FileText className="h-3.5 w-3.5" />
        文本
      </span>
    ),
  },
];
const PAGE_TABS = [
  {
    value: 'share',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Send className="h-3.5 w-3.5" />
        创建分享
      </span>
    ),
  },
  {
    value: 'void',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Users className="h-3.5 w-3.5" />
        虚空房间
      </span>
    ),
  },
  {
    value: 'history',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <History className="h-3.5 w-3.5" />
        分享记录
      </span>
    ),
  },
  {
    value: 'settings',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Settings className="h-3.5 w-3.5" />
        策略
      </span>
    ),
  },
];
const VOID_ROOM_TABS = [
  { value: 'temporary', label: '临时房间' },
  { value: 'persistent', label: '持久房间' },
];

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
  return {};
}

function formatExpiry(value) {
  return Number(value) === 0 ? '永久有效' : formatDateTime(value);
}

function expiryLabel(value) {
  return EXPIRY_OPTIONS.find((item) => item.value === String(value))?.label || `${value} 小时`;
}

function EntryName({ entry }) {
  const isFile = entry.type === 'file';
  return (
    <div className="flex min-w-0 items-center gap-2">
      <div className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md border ${isFile ? 'border-brand/20 bg-brand/10 text-brand' : 'border-kumo-success/20 bg-kumo-success/10 text-kumo-success'}`}>{isFile ? <FolderOpen className="h-4 w-4" /> : <FileText className="h-4 w-4" />}</div>
      <div className="min-w-0">
        <div className="truncate text-xs font-semibold text-kumo-strong">{isFile ? entry.originalName || entry.filename || '文件分享' : entry.preview || entry.content || '文本分享'}</div>
        <div className="mt-0.5 text-[11px] text-kumo-subtle">{isFile ? formatFileSize(entry.size || 0) : entry.textFormat === 'markdown' ? 'Markdown 内容' : '文本内容'}</div>
      </div>
    </div>
  );
}

function FileboxPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const [activeTab, setActiveTab] = useState('share');
  const [shareType, setShareType] = useState('file');
  const [shareText, setShareText] = useState('');
  const [selectedFile, setSelectedFile] = useState(null);
  const [expiry, setExpiry] = useState('0');
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
  const [fileboxSettings, setFileboxSettings] = useState({
    max_file_size: DEFAULT_FILEBOX_MAX_FILE_SIZE,
    allowed_mime_types: [],
    default_expiry_hours: 24,
    public_upload_enabled: false,
  });
  const [settingsMimeText, setSettingsMimeText] = useState('');
  const [voidMode, setVoidMode] = useState('temporary');
  const [voidRooms, setVoidRooms] = useState([]);
  const [voidRoomsLoading, setVoidRoomsLoading] = useState(false);
  const [voidLaunching, setVoidLaunching] = useState(false);
  const [storageNodes, setStorageNodes] = useState([]);
  const [selectedNodeId, setSelectedNodeId] = useState('local');
  const [transferModal, setTransferModal] = useState({ open: false, entry: null, targetNodeId: 'local', transferring: false });
  const fileInputRef = useRef(null);
  const abortControllerRef = useRef(null);
  const maxFileSize = fileboxSettings.max_file_size || DEFAULT_FILEBOX_MAX_FILE_SIZE;

  const loadLocalHistory = () => {
    try {
      setLocalHistory(JSON.parse(localStorage.getItem('filebox_history') || '[]'));
    } catch {
      setLocalHistory([]);
    }
  };

  const loadStorageNodes = async () => {
    try {
      const res = await axios.get('/api/filebox/storage-nodes', { headers: authHeaders() });
      if (res.data?.success && Array.isArray(res.data.data)) {
        setStorageNodes(res.data.data);
      }
    } catch {
      // 忽略节点获取失败，优雅退化为仅主站本地
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
      const [historyRes, logsRes] = await Promise.all([axios.get('/api/filebox/history', { headers: authHeaders() }), axios.get('/api/filebox/access-logs', { headers: authHeaders() })]);
      if (historyRes.data?.success) setServerHistory(Array.isArray(historyRes.data.data) ? historyRes.data.data : []);
      if (logsRes.data?.success) setAccessLogs(Array.isArray(logsRes.data.data) ? logsRes.data.data : []);
    } catch (error) {
      toast.error(error.response?.data?.error || '加载文件柜记录失败');
    } finally {
      setHistoryLoading(false);
    }
  };

  useEffect(() => {
    loadLocalHistory();
    loadSettings();
    loadStorageNodes();
  }, []);

  useEffect(() => {
    if (activeTab === 'history') loadServerHistory();
    if (activeTab === 'void') loadVoidRooms();
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
      setQrCode(
        await QRCode.toDataURL(fileboxShareURL(code), {
          width: 132,
          margin: 1,
          color: { dark: '#111827', light: '#ffffff' },
        })
      );
    } catch {
      setQrCode('');
    }
  };

  const createShare = async () => {
    const isText = shareType === 'text';
    if (isText && !shareText.trim()) return toast.warning('请输入要分享的内容');
    if (!isText && !selectedFile) return toast.warning('请选择要分享的文件');

    setLoading(true);
    setUploadProgress(0);
    setUploadSpeed('-');
    abortControllerRef.current = new AbortController();
    let lastTime = Date.now();
    let lastLoaded = 0;

    try {
      let res;
      let serverEntry = {};
      let createdCode = '';

      if (!isText && selectedNodeId !== 'local') {
        // 1. 请求主站签名签发上传端点与凭证
        const initRes = await axios.post(
          '/api/filebox/shares/init-upload',
          { serverId: selectedNodeId, filename: selectedFile.name, size: selectedFile.size },
          { headers: authHeaders(), signal: abortControllerRef.current.signal }
        );
        if (!initRes.data?.success) throw new Error(initRes.data?.error || '初始化远程上传失败');
        const { uploadUrl, code } = initRes.data.data;
        createdCode = code;

        // 2. 浏览器直传 PUT 字节流至边缘节点
        await axios.put(uploadUrl, selectedFile, {
          headers: { 'Content-Type': 'application/octet-stream' },
          signal: abortControllerRef.current.signal,
          onUploadProgress: (event) => {
            if (!event?.total) return;
            const now = Date.now();
            const speed = ((event.loaded - lastLoaded) * 1000) / Math.max(1, now - lastTime);
            setUploadProgress(Math.round((event.loaded / event.total) * 100));
            setUploadSpeed(formatSpeed(speed));
            lastLoaded = event.loaded;
            lastTime = now;
          },
        });

        // 3. 通知主站完成上传并落库元数据
        const completeRes = await axios.post(
          '/api/filebox/shares/complete-upload',
          {
            code: createdCode,
            filename: selectedFile.name,
            size: selectedFile.size,
            serverId: selectedNodeId,
            mimeType: selectedFile.type || 'application/octet-stream',
            expiry,
            burn_after_reading: burnAfterReading,
            max_downloads: maxDownloads || '0',
            access_password: accessPassword,
          },
          { headers: authHeaders(), signal: abortControllerRef.current.signal }
        );
        if (!completeRes.data?.success) throw new Error(completeRes.data?.error || '登记远程上传元数据失败');
        res = completeRes;
        serverEntry = completeRes.data?.data || {};
      } else {
        const formData = new FormData();
        formData.append('type', isText ? 'text' : 'file');
        formData.append('expiry', expiry);
        formData.append('burn_after_reading', burnAfterReading);
        formData.append('max_downloads', maxDownloads || '0');
        formData.append('access_password', accessPassword);
        if (isText) formData.append('text', shareText);
        else formData.append('file', selectedFile);

        res = await axios.post('/api/filebox/share', formData, {
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
        serverEntry = res.data?.data || {};
        createdCode = res.data.code;
      }

      const entry = {
        ...serverEntry,
        code: createdCode,
        type: isText ? 'text' : 'file',
        textFormat: serverEntry.textFormat || (isText ? 'markdown' : ''),
        originalName: serverEntry.originalName || selectedFile?.name || '',
        content: shareText,
        size:
          Number(serverEntry.size) > 0
            ? Number(serverEntry.size)
            : isText
              ? new Blob([shareText]).size
              : selectedFile?.size || 0,
        createdAt: serverEntry.createdAt || Date.now(),
        requiresPassword: serverEntry.requiresPassword ?? !!accessPassword,
      };
      setResult(entry);
      saveLocalHistory(entry);
      setUploadProgress(100);
      await generateQrCode(createdCode);
      toast.success('分享已创建');
    } catch (error) {
      if (!axios.isCancel(error)) toast.error(error.response?.data?.error || error.message || '分享失败');
    } finally {
      setLoading(false);
      abortControllerRef.current = null;
    }
  };

  const deleteEntry = async (code) => {
    if (!confirmPress(`share:${code}`, `删除分享「${code}」`)) return;
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
    if (!confirmPress('clear-expired-shares', '清理所有过期分享')) return;
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
      const allowedMimeTypes = settingsMimeText
        .split(/[,，\n]/)
        .map((item) => item.trim())
        .filter(Boolean);
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
    await navigator.clipboard.writeText(fileboxShareURL(code));
    toast.success('分享链接已复制');
  };

  const openTransferModal = (entry) => {
    const currentId = entry.storageType === 'remote' && entry.serverId ? entry.serverId : 'local';
    const firstOther = currentId === 'local'
      ? (storageNodes[0]?.id || 'local')
      : 'local';
    setTransferModal({
      open: true,
      entry,
      targetNodeId: firstOther,
      transferring: false,
    });
  };

  const handleTransferSubmit = async () => {
    if (!transferModal.entry) return;
    const { entry, targetNodeId } = transferModal;
    const isLocal = targetNodeId === 'local';
    setTransferModal((prev) => ({ ...prev, transferring: true }));
    try {
      const res = await axios.post(
        `/api/filebox/shares/${encodeURIComponent(entry.code)}/transfer`,
        {
          targetStorageType: isLocal ? 'local' : 'remote',
          targetServerId: isLocal ? '' : targetNodeId,
        },
        { headers: authHeaders() }
      );
      if (!res.data?.success) throw new Error(res.data?.error || '转移存储失败');
      toast.success('存储位置转移成功');
      setTransferModal({ open: false, entry: null, targetNodeId: 'local', transferring: false });
      await loadServerHistory();
    } catch (error) {
      toast.error(error.response?.data?.error || error.message || '转移存储失败');
      setTransferModal((prev) => ({ ...prev, transferring: false }));
    }
  };

  const loadVoidRooms = async () => {
    setVoidRoomsLoading(true);
    try {
      const res = await axios.get('/api/filebox/void/rooms', { headers: authHeaders() });
      if (res.data?.success) setVoidRooms(Array.isArray(res.data.data) ? res.data.data : []);
    } catch (error) {
      toast.error(error.response?.data?.error || '加载虚空房间失败');
    } finally {
      setVoidRoomsLoading(false);
    }
  };

  const storeVoidOwnerCredentials = (room) => {
    const roomId = room.roomId || room.id;
    if (!roomId || !room.ownerToken) return;
    const credentialKey = `void_owner_credentials:${roomId}`;
    const credentials = {
      ownerToken: room.ownerToken,
      ownerParticipantId: room.ownerParticipantId || 'owner',
      expiresAt: room.expiresAt,
      mode: room.mode || (room.persistent ? 'persistent' : 'temporary'),
    };
    sessionStorage.setItem(credentialKey, JSON.stringify(credentials));
    localStorage.setItem(credentialKey, JSON.stringify(credentials));
  };

  const openVoidRoom = (room) => {
    const roomId = room.roomId || room.id;
    if (!roomId) return;
    storeVoidOwnerCredentials(room);
    const target = `/void/${encodeURIComponent(roomId)}`;
    const roomWindow = window.open(target, '_blank');
    if (roomWindow) {
      roomWindow.opener = null;
      return;
    }
    toast.warning('浏览器拦截了新标签页，已在当前页打开');
    window.location.href = target;
  };

  const closeVoidRoom = async (room) => {
    const roomId = room.roomId || room.id;
    if (!roomId) return;
    if (!(await dialog.confirm(`关闭虚空房间 ${roomId}？`))) return;
    try {
      await axios.delete(`/api/filebox/void/rooms/${encodeURIComponent(roomId)}`, {
        headers: room.ownerToken ? { 'X-Void-Owner-Token': room.ownerToken } : authHeaders(),
      });
      sessionStorage.removeItem(`void_owner_credentials:${roomId}`);
      localStorage.removeItem(`void_owner_credentials:${roomId}`);
      toast.success('房间已关闭');
      await loadVoidRooms();
    } catch (error) {
      toast.error(error.response?.data?.error || '关闭房间失败');
    }
  };

  const startVoidRoom = async () => {
    const roomWindow = window.open('', '_blank');
    if (roomWindow) roomWindow.opener = null;
    setVoidLaunching(true);
    try {
      const roomRes = await axios.post('/api/filebox/void/rooms', { mode: voidMode }, { headers: authHeaders() });
      const data = roomRes.data?.data || {};
      const roomId = data.roomId || data.id;
      if (!roomId || !data.ownerToken) throw new Error('创建虚空房间失败');
      storeVoidOwnerCredentials(data);
      const target = `/void/${encodeURIComponent(roomId)}`;
      if (roomWindow) {
        roomWindow.location.href = target;
        toast.success('虚空房间已在新标签页打开');
        setVoidLaunching(false);
        loadVoidRooms();
      } else {
        toast.warning('浏览器拦截了新标签页，已在当前页打开');
        window.location.href = target;
      }
    } catch (error) {
      if (roomWindow && !roomWindow.closed) roomWindow.close();
      toast.error(error.response?.data?.error || error.message || '虚空房间创建失败');
      setVoidLaunching(false);
    }
  };

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs {...MODULE_TABS_PROPS} value={activeTab} onValueChange={setActiveTab} tabs={PAGE_TABS} />
      </div>

      {activeTab === 'share' && (
        <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(0,1fr)_minmax(22rem,1fr)]">
          <SectionCard
            title="创建分享"
            icon={<Send className="h-4 w-4 text-brand" />}
            action={
              <Tabs
                {...TOOL_TABS_PROPS}
                value={shareType}
                onValueChange={(value) => {
                  setShareType(value);
                  setResult(null);
                }}
                tabs={SHARE_TYPE_TABS}
              />
            }
            bodyClassName="grid gap-4"
          >
            {shareType === 'file' ? (
              <div
                className="rounded-md border border-dashed border-kumo-line bg-kumo-recessed/35 p-6 text-center"
                onDragOver={(event) => event.preventDefault()}
                onDrop={(event) => {
                  event.preventDefault();
                  selectFile(event.dataTransfer.files?.[0]);
                }}
              >
                <Input ref={fileInputRef} type="file" aria-label="选择分享文件" className="hidden" onChange={(event) => selectFile(event.target.files?.[0])} />
                <Upload className="mx-auto h-9 w-9 text-kumo-subtle" />
                <div className="mt-3 text-sm font-semibold text-kumo-strong">{selectedFile ? selectedFile.name : '拖入文件或点击选择'}</div>
                <div className="mt-1 text-xs text-kumo-subtle">最大 {formatFileSize(maxFileSize)}</div>
                <div className="mt-4 flex justify-center">
                  <Button size="sm" onClick={() => fileInputRef.current?.click()}>
                    选择文件
                  </Button>
                </div>
              </div>
            ) : (
              <Suspense fallback={<div className="flex min-h-72 items-center justify-center rounded-md border border-kumo-line text-xs text-kumo-subtle">正在加载文本编辑器</div>}>
                <MarkdownEditor
                  label="分享文本"
                  value={shareText}
                  onChange={(value) => {
                    setShareText(value);
                    setResult(null);
                  }}
                  minHeight="18rem"
                  placeholder="输入或粘贴文本内容"
                />
              </Suspense>
            )}

            {loading && shareType === 'file' && <Meter label="上传进度" value={uploadProgress} customValue={`${uploadProgress}% · ${uploadSpeed}`} />}

            <div className="grid gap-3 cq-md:grid-cols-2">
              {shareType === 'file' && (
                <div className="cq-md:col-span-2">
                  <Select alignItemWithTrigger
                    size="sm"
                    label="存储位置"
                    value={selectedNodeId}
                    onValueChange={setSelectedNodeId}
                    items={[
                      { value: 'local', label: '主站本地存储' },
                      ...storageNodes.map((n) => ({
                        value: n.id,
                        label: `${n.name || n.id} (${n.host}:${n.storagePort || 61208})`,
                      })),
                    ]}
                  />
                  <div className="mt-1 text-[11px] text-kumo-subtle">
                    选择边缘节点时，文件字节流将由浏览器直传至该节点，主站不中转流量。
                  </div>
                </div>
              )}
              <Select alignItemWithTrigger size="sm" label="有效期" value={expiry} onValueChange={setExpiry} items={EXPIRY_OPTIONS} />
              <Input size="sm" label="最大下载次数" type="number" min="0" value={maxDownloads} onChange={(event) => setMaxDownloads(event.target.value)} placeholder="0 或留空为不限" />
              <Input size="sm" label="访问密码" type="text" value={accessPassword} onChange={(event) => setAccessPassword(event.target.value)} placeholder="可选" autoComplete="off" data-1p-ignore data-lpignore="true" data-bwignore="true" data-form-type="other" spellCheck={false} />
              <div className="flex items-center justify-between rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-2">
                <div>
                  <div className="text-xs font-semibold text-kumo-strong">阅后即焚</div>
                  <div className="text-[11px] text-kumo-subtle">首次成功下载后删除</div>
                </div>
                <Switch checked={burnAfterReading} onCheckedChange={setBurnAfterReading} />
              </div>
            </div>

            <div className="flex flex-wrap justify-end gap-2 border-t border-kumo-line pt-4">
              {loading && (
                <Button size="sm" variant="secondary-destructive" onClick={() => abortControllerRef.current?.abort()}>
                  取消上传
                </Button>
              )}
              <Button size="sm" variant="secondary" onClick={resetShare}>
                重置
              </Button>
              <Button size="sm" variant="primary" onClick={createShare} loading={loading} icon={<Send className="h-4 w-4" />}>
                创建分享
              </Button>
            </div>
          </SectionCard>

          <SectionCard title="分享结果" icon={<FileText className="h-4 w-4 text-brand" />}>
            {!result ? (
              <div className="space-y-3">
                <div className="rounded-md border border-dashed border-kumo-line p-8 text-center text-xs text-kumo-subtle">显示链接、二维码和取用信息。</div>
                <div className="grid gap-2 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3 text-xs">
                  <div className="flex justify-between gap-3">
                    <span className="text-kumo-subtle">类型</span>
                    <span className="font-semibold text-kumo-strong">{shareType === 'file' ? '文件' : '文本'}</span>
                  </div>
                  <div className="flex justify-between gap-3">
                    <span className="text-kumo-subtle">有效期</span>
                    <span className="font-semibold text-kumo-strong">{expiryLabel(expiry)}</span>
                  </div>
                  <div className="flex justify-between gap-3">
                    <span className="text-kumo-subtle">下载次数</span>
                    <span className="font-semibold text-kumo-strong">{maxDownloads || '不限'}</span>
                  </div>
                  <div className="flex justify-between gap-3">
                    <span className="text-kumo-subtle">访问密码</span>
                    <span className="font-semibold text-kumo-strong">{accessPassword ? '已设置' : '未设置'}</span>
                  </div>
                </div>
              </div>
            ) : (
              <div className="mt-4 space-y-4">
				<div className="grid gap-3 rounded-md border border-kumo-line bg-kumo-recessed/35 p-3 cq-sm:grid-cols-2">
				  <div className="min-w-0">
					<div className="text-xs text-kumo-subtle">分享链接</div>
					<ClipboardText text={fileboxShareURL(result.code)} className="mt-2" tooltip={{ text: '复制分享链接', copiedText: '分享链接已复制' }} labels={{ copyAction: '复制分享链接' }} />
				  </div>
				  <div className="min-w-0">
					<div className="text-xs text-kumo-subtle">直链（源码）</div>
					<ClipboardText text={fileboxDirectURL(result.code)} className="mt-2" tooltip={{ text: '复制直链', copiedText: '直链已复制' }} labels={{ copyAction: '复制直链' }} />
				  </div>
                </div>
                <div className="grid gap-3 cq-sm:grid-cols-[auto_minmax(0,1fr)] cq-sm:items-center">
                  {qrCode && <img src={qrCode} alt="分享二维码" className="h-32 w-32 rounded-md border border-kumo-line bg-white p-2" />}
                  <div className="space-y-2 text-xs text-kumo-subtle">
                    <div>
                      <span className="font-semibold text-kumo-strong">分享码:</span> <span className="font-mono text-brand">{result.code}</span>
                    </div>
                    <div>链接可直接打开；需要密码时浏览器会提示输入。</div>
                    <div>有效期: {expiryLabel(expiry)}</div>
                    {accessPassword && <Badge variant="warning">已启用访问密码</Badge>}
                  </div>
                </div>
              </div>
            )}
          </SectionCard>
        </div>
      )}

      {activeTab === 'history' && (
        <div className="grid gap-4">
          <SectionCard
            title="分享记录"
            icon={<History className="h-4 w-4 text-brand" />}
            actions={
              <>
                <Button size="sm" variant="secondary" onClick={loadServerHistory} loading={historyLoading} icon={<RefreshCw className="h-4 w-4" />}>
                  刷新
                </Button>
                <Button size="sm" variant={isArmed('clear-expired-shares') ? 'destructive' : 'secondary-destructive'} onClick={runCleanup} icon={<Clock className="h-4 w-4" />}>
                  清理过期
                </Button>
              </>
            }
            bodyPadding="none"
            bodyClassName="overflow-x-auto"
          >
            <Table layout="fixed" className="min-w-[880px]">
              <colgroup>
                <col />
                <col className="w-24" />
                <col className="w-36" />
                <col className="w-28" />
                <col className="w-32" />
                <col className="w-36" />
              </colgroup>
              <Table.Header>
                <Table.Row>
                  <Table.Head>内容</Table.Head>
                  <Table.Head>分享码</Table.Head>
                  <Table.Head>存储位置</Table.Head>
                  <Table.Head>下载次数</Table.Head>
                  <Table.Head>到期</Table.Head>
                  <Table.Head className="app-table-action">操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {historyLoading ? (
                  Array.from({ length: 3 }).map((_, index) => (
                    <Table.Row key={index}>
                      <Table.Cell colSpan={6}>
                        <SkeletonLine className="h-8 w-full" />
                      </Table.Cell>
                    </Table.Row>
                  ))
                ) : serverHistory.length === 0 ? (
                  <Table.Row>
                    <Table.Cell colSpan={6} className="p-8 text-center text-kumo-subtle">
                      暂无有效分享
                    </Table.Cell>
                  </Table.Row>
                ) : (
                  serverHistory.map((entry) => {
                    const isRemote = entry.storageType === 'remote';
                    const nodeInfo = isRemote ? storageNodes.find((n) => n.id === entry.serverId) : null;
                    const nodeLabel = isRemote
                      ? (nodeInfo ? `${nodeInfo.name || nodeInfo.id} (${nodeInfo.host})` : (entry.serverId || '远程节点'))
                      : '主站本地';

                    return (
                      <Table.Row key={entry.code}>
                        <Table.Cell>
                          <EntryName entry={entry} />
                        </Table.Cell>
                        <Table.Cell className="font-mono text-xs font-semibold text-brand">{entry.code}</Table.Cell>
                        <Table.Cell>
                          <Badge variant={isRemote ? 'brand' : 'secondary'} size="sm" className="inline-flex items-center gap-1 font-mono text-[11px]">
                            <Server className="h-3 w-3 shrink-0" />
                            <span className="truncate max-w-[120px]">{nodeLabel}</span>
                          </Badge>
                        </Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">
                          {entry.downloads || 0}
                          {entry.maxDownloads ? ` / ${entry.maxDownloads}` : ' / 不限'}
                        </Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">{formatExpiry(entry.expiry)}</Table.Cell>
                        <Table.Cell>
                          <div className="flex gap-1">
                            <Button size="sm" variant="secondary" onClick={() => copyLink(entry.code)}>
                              复制
                            </Button>
                            {entry.type === 'file' && (
                              <Button size="sm" variant="secondary" onClick={() => openTransferModal(entry)} title="转移存储位置">
                                转移
                              </Button>
                            )}
                            <Button size="sm" variant={isArmed(`share:${entry.code}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteEntry(entry.code)}>
                              <Trash className="h-3.5 w-3.5" />
                            </Button>
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    );
                  })
                )}
              </Table.Body>
            </Table>
          </SectionCard>

          <div className="grid items-start gap-4 cq-xl:grid-cols-2">
            <SectionCard title="本地最近创建" icon={<History className="h-4 w-4 text-brand" />} bodyPadding="none">
              <div className="divide-y divide-kumo-line">
                {localHistory.length === 0 ? (
                  <div className="py-8 text-center text-xs text-kumo-subtle">暂无本地记录</div>
                ) : (
                  localHistory.slice(0, 8).map((entry) => (
                    <div key={entry.code} className="flex items-center justify-between gap-3 px-4 py-2.5">
                      <EntryName entry={entry} />
                      <div className="flex shrink-0 items-center gap-2">
                        <span className="font-mono text-xs text-brand">{entry.code}</span>
                        <Button size="sm" variant="secondary" onClick={() => copyLink(entry.code)}>
                          复制
                        </Button>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </SectionCard>

            <SectionCard title="访问日志" icon={<Clock className="h-4 w-4 text-brand" />} bodyPadding="none">
              <div className="max-h-72 overflow-auto divide-y divide-kumo-line">
                {accessLogs.length === 0 ? (
                  <div className="py-8 text-center text-xs text-kumo-subtle">暂无访问日志</div>
                ) : (
                  accessLogs.slice(0, 20).map((log) => (
                    <div key={log.id} className="grid grid-cols-[5rem_5rem_minmax(0,1fr)_9rem] gap-2 px-4 py-2 text-xs">
                      <span className="font-mono text-brand">{log.code}</span>
                      <span className="text-kumo-strong">{log.action}</span>
                      <span className="truncate text-kumo-subtle">{log.ipAddress || log.userAgent || '-'}</span>
                      <span className="text-right text-kumo-subtle">{formatDateTime(log.createdAt)}</span>
                    </div>
                  ))
                )}
              </div>
            </SectionCard>
          </div>
        </div>
      )}

      {activeTab === 'settings' && (
        <SectionCard
          title="文件柜策略"
          icon={<Settings className="h-4 w-4 text-brand" />}
          action={
            <Button size="sm" variant="secondary" onClick={loadSettings} loading={settingsLoading} icon={<RefreshCw className="h-4 w-4" />}>
              刷新
            </Button>
          }
        >
          <div className="grid gap-4 cq-md:grid-cols-2">
            <Input
              size="sm"
              label="最大文件大小 MB"
              type="number"
              min="1"
              value={Math.round((fileboxSettings.max_file_size || DEFAULT_FILEBOX_MAX_FILE_SIZE) / 1024 / 1024)}
              onChange={(event) =>
                setFileboxSettings((prev) => ({
                  ...prev,
                  max_file_size: Math.max(1, Number(event.target.value) || 1) * 1024 * 1024,
                }))
              }
            />
            <Input
              size="sm"
              label="默认有效期小时"
              type="number"
              min="1"
              value={fileboxSettings.default_expiry_hours || 24}
              onChange={(event) =>
                setFileboxSettings((prev) => ({
                  ...prev,
                  default_expiry_hours: Math.max(1, Number(event.target.value) || 24),
                }))
              }
            />
            <div className="cq-md:col-span-2">
              <Textarea label="允许 MIME 类型" value={settingsMimeText} onChange={(event) => setSettingsMimeText(event.target.value)} className="min-h-28 font-mono text-xs" placeholder="留空不限。如 image/*, application/pdf, text/plain" />
            </div>
            <div className="cq-md:col-span-2 flex items-center justify-between rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
              <div>
                <div className="text-xs font-semibold text-kumo-strong">允许公开上传</div>
                <div className="mt-1 text-[11px] text-kumo-subtle">当前接口仍要求管理员认证；保留为策略开关。</div>
              </div>
              <Switch checked={!!fileboxSettings.public_upload_enabled} onCheckedChange={(checked) => setFileboxSettings((prev) => ({ ...prev, public_upload_enabled: checked }))} />
            </div>
          </div>
          <div className="mt-4 flex justify-end border-t border-kumo-line pt-4">
            <Button size="sm" variant="primary" onClick={saveSettings} loading={settingsLoading} icon={<Lock className="h-4 w-4" />}>
              保存策略
            </Button>
          </div>
        </SectionCard>
      )}

      {activeTab === 'void' && (
        <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(0,1fr)_minmax(22rem,0.8fr)]">
          <SectionCard
            title="房间管理"
            icon={<Send className="h-4 w-4 text-brand" />}
            action={
              <Button size="sm" variant="secondary" onClick={loadVoidRooms} loading={voidRoomsLoading} icon={<RefreshCw className="h-4 w-4" />}>
                刷新
              </Button>
            }
            bodyClassName="grid gap-4"
          >
            <div className="grid gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-4 cq-lg:grid-cols-[minmax(0,1fr)_auto] cq-lg:items-center">
              <div className="flex flex-wrap items-center gap-3 min-w-0">
                <div className="flex shrink-0 items-center gap-2">
                  <div className="text-sm font-semibold text-kumo-strong">新建房间</div>
                  <Badge variant={voidMode === 'persistent' ? 'success' : 'secondary'}>{voidMode === 'persistent' ? '持久' : '临时'}</Badge>
                </div>
                <div className="w-fit min-w-0 max-w-full">
                  <Tabs {...TOOL_TABS_PROPS} value={voidMode} onValueChange={setVoidMode} tabs={VOID_ROOM_TABS} />
                </div>
              </div>
              <Button size="sm" variant="primary" loading={voidLaunching} onClick={startVoidRoom} icon={<ExternalLink className="h-4 w-4" />}>
                创建并打开
              </Button>
            </div>

            <div className="grid gap-3 cq-md:grid-cols-3">
              <div className="rounded-md border border-kumo-line bg-kumo-base p-3">
                <div className="text-[11px] text-kumo-subtle">临时房间</div>
                <div className="mt-1 text-xs font-semibold text-kumo-strong">30 分钟</div>
              </div>
              <div className="rounded-md border border-kumo-line bg-kumo-base p-3">
                <div className="text-[11px] text-kumo-subtle">持久房间</div>
                <div className="mt-1 text-xs font-semibold text-kumo-strong">数据库</div>
              </div>
              <div className="rounded-md border border-kumo-line bg-kumo-base p-3">
                <div className="text-[11px] text-kumo-subtle">在线房间</div>
                <div className="mt-1 text-xs font-semibold text-kumo-strong">{voidRooms.length}</div>
              </div>
            </div>

            <div className="divide-y divide-kumo-line overflow-hidden rounded-md border border-kumo-line">
              {voidRoomsLoading ? (
                Array.from({ length: 2 }).map((_, index) => (
                  <div key={index} className="p-3">
                    <SkeletonLine className="h-8 w-full" />
                  </div>
                ))
              ) : voidRooms.length === 0 ? (
                <div className="p-6 text-center text-xs text-kumo-subtle">暂无房间</div>
              ) : (
                voidRooms.map((room) => {
                  const roomId = room.roomId || room.id;
                  const mode = room.mode || (room.persistent ? 'persistent' : 'temporary');
                  return (
                    <div key={roomId} className="grid gap-3 px-4 py-3 cq-lg:grid-cols-[minmax(0,1fr)_auto] cq-lg:items-center">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-mono text-sm font-semibold text-kumo-strong">{roomId}</span>
                          <Badge variant={mode === 'persistent' ? 'success' : 'secondary'}>{mode === 'persistent' ? '持久' : '临时'}</Badge>
                          <Badge variant="secondary">{(room.participants || []).filter((item) => item.online).length} 在线</Badge>
                        </div>
                        <div className="mt-1 text-[11px] text-kumo-subtle">{mode === 'persistent' ? '长期房间' : `到期 ${room.expiresAt ? formatDateTime(room.expiresAt) : '-'}`}</div>
                      </div>
                      <div className="flex shrink-0 flex-wrap justify-end gap-2">
                        <Button size="sm" variant="secondary" onClick={() => navigator.clipboard.writeText(`${window.location.origin}/void/${encodeURIComponent(roomId)}`).then(() => toast.success('房间链接已复制'))}>
                          复制
                        </Button>
                        <Button size="sm" variant="primary" onClick={() => openVoidRoom(room)} icon={<ExternalLink className="h-4 w-4" />}>
                          打开
                        </Button>
                        <Button size="sm" variant="secondary-destructive" onClick={() => closeVoidRoom(room)} icon={<X className="h-4 w-4" />}>
                          关闭
                        </Button>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </SectionCard>

          <SectionCard title="传输边界" icon={<Lock className="h-4 w-4 text-brand" />} bodyClassName="grid gap-3 text-xs">
            <div className="flex justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
              <span className="text-kumo-subtle">传输内容</span>
              <span className="font-semibold text-kumo-strong">浏览器直连</span>
            </div>
            <div className="flex justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
              <span className="text-kumo-subtle">房间元数据</span>
              <span className="font-semibold text-kumo-strong">按类型保存</span>
            </div>
            <div className="flex justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
              <span className="text-kumo-subtle">服务器流量</span>
              <span className="font-semibold text-kumo-strong">仅信令</span>
            </div>
            <div className="flex justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
              <span className="text-kumo-subtle">中继</span>
              <span className="font-semibold text-kumo-strong">禁用 TURN</span>
            </div>
          </SectionCard>
        </div>
      )}

      <Dialog.Root
        open={transferModal.open}
        onOpenChange={(open) => !transferModal.transferring && setTransferModal((prev) => ({ ...prev, open }))}
      >
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] !w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
            <Dialog.Title className="text-sm font-semibold text-kumo-strong">
              转移文件存储位置
            </Dialog.Title>
            <Dialog.Close disabled={transferModal.transferring} />
          </div>

          <div className="space-y-4 p-4 text-xs">
            {transferModal.entry && (
              <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
                <div className="flex justify-between gap-2">
                  <span className="text-kumo-subtle">文件名</span>
                  <span className="font-semibold text-kumo-strong truncate max-w-[200px]">
                    {transferModal.entry.originalName || transferModal.entry.filename}
                  </span>
                </div>
                <div className="flex justify-between gap-2">
                  <span className="text-kumo-subtle">大小</span>
                  <span className="font-mono text-kumo-strong">
                    {formatFileSize(transferModal.entry.size || 0)}
                  </span>
                </div>
                <div className="flex justify-between gap-2">
                  <span className="text-kumo-subtle">当前位置</span>
                  <Badge variant={transferModal.entry.storageType === 'remote' ? 'brand' : 'secondary'} size="sm">
                    {transferModal.entry.storageType === 'remote'
                      ? (storageNodes.find((n) => n.id === transferModal.entry.serverId)?.name || transferModal.entry.serverId || '远程节点')
                      : '主站本地'}
                  </Badge>
                </div>
              </div>
            )}

            <div className="space-y-1.5">
              <Select alignItemWithTrigger
                size="sm"
                label="目标存储位置"
                value={transferModal.targetNodeId}
                onValueChange={(val) => setTransferModal((prev) => ({ ...prev, targetNodeId: val }))}
                items={[
                  { value: 'local', label: '主站本地存储' },
                  ...storageNodes.map((n) => ({
                    value: n.id,
                    label: `${n.name || n.id} (${n.host}:${n.storagePort || 61208})`,
                  })),
                ]}
              />
              <p className="text-[11px] text-kumo-subtle">
                主站将自动拉取数据并安全迁移到目标节点，完成完整性校验后清理旧存储。
              </p>
            </div>
          </div>

          <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
            <Button
              size="sm"
              variant="secondary"
              disabled={transferModal.transferring}
              onClick={() => setTransferModal({ open: false, entry: null, targetNodeId: 'local', transferring: false })}
            >
              取消
            </Button>
            <Button
              size="sm"
              variant="primary"
              loading={transferModal.transferring}
              onClick={handleTransferSubmit}
            >
              开始转移
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default FileboxPage;
