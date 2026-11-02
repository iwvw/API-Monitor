import React, { useEffect, useRef, useState } from 'react';
import axios from 'axios';
import QRCode from 'qrcode';
import { Tabs } from '@cloudflare/kumo';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { fileboxShareURL } from '../../modules/fileboxLinks.js';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import { formatFileSize } from '../../modules/utils.js';
import { stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import { DEFAULT_FILEBOX_MAX_FILE_SIZE } from './constants.js';
import { PAGE_TABS } from './tabs.jsx';
import { authHeaders, formatSpeed } from './utils.js';
import { SharePanel } from './SharePanel.jsx';
import { HistoryPanel } from './HistoryPanel.jsx';
import { SettingsPanel } from './SettingsPanel.jsx';
import { VoidPanel } from './VoidPanel.jsx';
import { TransferDialog } from './TransferDialog.jsx';

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
        <SharePanel
          shareType={shareType}
          setShareType={setShareType}
          shareText={shareText}
          setShareText={setShareText}
          selectedFile={selectedFile}
          fileInputRef={fileInputRef}
          maxFileSize={maxFileSize}
          loading={loading}
          uploadProgress={uploadProgress}
          uploadSpeed={uploadSpeed}
          selectedNodeId={selectedNodeId}
          setSelectedNodeId={setSelectedNodeId}
          storageNodes={storageNodes}
          expiry={expiry}
          setExpiry={setExpiry}
          maxDownloads={maxDownloads}
          setMaxDownloads={setMaxDownloads}
          accessPassword={accessPassword}
          setAccessPassword={setAccessPassword}
          burnAfterReading={burnAfterReading}
          setBurnAfterReading={setBurnAfterReading}
          result={result}
          setResult={setResult}
          qrCode={qrCode}
          abortControllerRef={abortControllerRef}
          selectFile={selectFile}
          resetShare={resetShare}
          createShare={createShare}
        />
      )}

      {activeTab === 'history' && (
        <HistoryPanel
          isArmed={isArmed}
          historyLoading={historyLoading}
          serverHistory={serverHistory}
          storageNodes={storageNodes}
          localHistory={localHistory}
          accessLogs={accessLogs}
          loadServerHistory={loadServerHistory}
          runCleanup={runCleanup}
          copyLink={copyLink}
          openTransferModal={openTransferModal}
          deleteEntry={deleteEntry}
        />
      )}

      {activeTab === 'settings' && (
        <SettingsPanel
          fileboxSettings={fileboxSettings}
          setFileboxSettings={setFileboxSettings}
          settingsMimeText={settingsMimeText}
          setSettingsMimeText={setSettingsMimeText}
          settingsLoading={settingsLoading}
          loadSettings={loadSettings}
          saveSettings={saveSettings}
        />
      )}

      {activeTab === 'void' && (
        <VoidPanel
          voidMode={voidMode}
          setVoidMode={setVoidMode}
          voidRooms={voidRooms}
          voidRoomsLoading={voidRoomsLoading}
          voidLaunching={voidLaunching}
          loadVoidRooms={loadVoidRooms}
          startVoidRoom={startVoidRoom}
          openVoidRoom={openVoidRoom}
          closeVoidRoom={closeVoidRoom}
        />
      )}

      <TransferDialog
        transferModal={transferModal}
        setTransferModal={setTransferModal}
        storageNodes={storageNodes}
        onSubmit={handleTransferSubmit}
      />
    </div>
  );
}

export default FileboxPage;
