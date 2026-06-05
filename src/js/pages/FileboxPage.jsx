import React, { useState, useEffect, useRef } from 'react';
import axios from 'axios';
import QRCode from 'qrcode';
import { toast } from '../modules/toast.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Tabs } from '@cloudflare/kumo/components/tabs';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { formatFileSize, formatDateTime } from '../modules/utils.js';
import {
  Send,
  Download,
  History,
  Info,
  FolderOpen,
  FileText,
  Upload,
  RefreshCw,
  Trash,
  X
} from '../components/Icons.jsx';

const FILEBOX_MAX_FILE_SIZE = 100 * 1024 * 1024; // 100MB

function formatSpeed(bytesPerSecond) {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return '-';
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  let value = bytesPerSecond;
  let idx = 0;
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024;
    idx += 1;
  }
  const fixed = value >= 100 ? 0 : value >= 10 ? 1 : 2;
  return `${value.toFixed(fixed)} ${units[idx]}`;
}

function formatEta(seconds) {
  if (!Number.isFinite(seconds) || seconds <= 0) return '-';
  if (seconds < 60) return `${Math.ceil(seconds)}秒`;
  const mins = Math.floor(seconds / 60);
  const secs = Math.ceil(seconds % 60);
  return `${mins}分${secs}秒`;
}

function FileboxPage() {
  const [activeTab, setActiveTab] = useState('share'); // 'share' | 'retrieve' | 'history'
  const [shareType, setShareType] = useState('file'); // 'file' | 'text'
  const [retrieveCode, setRetrieveCode] = useState('');
  const [shareText, setShareText] = useState('');
  const [selectedFile, setSelectedFile] = useState(null);
  const [expiry, setExpiry] = useState('24');
  const [burnAfterReading, setBurnAfterReading] = useState(false);
  const [loading, setLoading] = useState(false);

  // Results
  const [result, setResult] = useState(null);
  const [qrCode, setQrCode] = useState('');
  
  // History lists
  const [localHistory, setLocalHistory] = useState([]);
  const [serverHistory, setServerHistory] = useState([]);
  const [historyLoading, setHistoryLoading] = useState(false);

  // Teleport/Modal retrieved item
  const [retrievedEntry, setRetrievedEntry] = useState(null);
  
  // Drag & drop state
  const [isDragging, setIsDragging] = useState(false);

  // Upload telemetry states
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadSpeedText, setUploadSpeedText] = useState('-');
  const [uploadEtaText, setUploadEtaText] = useState('-');
  const [uploadingName, setUploadingName] = useState('');

  const abortControllerRef = useRef(null);
  const fileInputRef = useRef(null);

  // Get Auth Headers
  const getAuthHeaders = () => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'x-admin-password': password,
    };
  };

  // Load history on mount
  useEffect(() => {
    loadLocalHistory();
  }, []);

  // Fetch server history when switching to history tab
  useEffect(() => {
    if (activeTab === 'history') {
      loadServerHistory();
    }
  }, [activeTab]);

  const loadLocalHistory = () => {
    try {
      const saved = localStorage.getItem('filebox_history');
      if (saved) {
        setLocalHistory(JSON.parse(saved));
      }
    } catch (e) {
      console.error('Failed to load local history', e);
    }
  };

  const saveToLocalHistory = (entry) => {
    setLocalHistory((prev) => {
      const updated = [entry, ...prev];
      if (updated.length > 50) updated.length = 50;
      localStorage.setItem('filebox_history', JSON.stringify(updated));
      return updated;
    });
  };

  const clearLocalHistory = () => {
    setLocalHistory([]);
    localStorage.removeItem('filebox_history');
    toast.success('本地历史已清空');
  };

  const loadServerHistory = async () => {
    setHistoryLoading(true);
    try {
      const res = await axios.get('/api/filebox/history', {
        headers: getAuthHeaders(),
      });
      if (res.data?.success) {
        setServerHistory(Array.isArray(res.data.data) ? res.data.data : []);
      }
    } catch (error) {
      console.error(error);
      toast.error(error.response?.data?.error || '加载服务端历史失败');
    } finally {
      setHistoryLoading(false);
    }
  };

  const handleSelectFile = (file) => {
    if (!file) return;
    if (file.size > FILEBOX_MAX_FILE_SIZE) {
      toast.error(`文件过大，最大支持 ${formatFileSize(FILEBOX_MAX_FILE_SIZE)}`);
      return;
    }
    setSelectedFile(file);
    setShareType('file');
  };

  const resetForm = () => {
    setShareText('');
    setSelectedFile(null);
    setExpiry('24');
    setBurnAfterReading(false);
    setResult(null);
    setQrCode('');
    setUploadProgress(0);
    setUploadSpeedText('-');
    setUploadEtaText('-');
    setUploadingName('');
    abortControllerRef.current = null;
    if (fileInputRef.current) fileInputRef.current.value = '';
  };

  const cancelUpload = () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
      toast.warning('上传已取消');
    }
  };

  const generateQrCode = async (code) => {
    const url = `${window.location.origin}/api/filebox/download/${code}`;
    try {
      const qrDataUrl = await QRCode.toDataURL(url, {
        width: 150,
        margin: 1,
        color: { dark: '#000000', light: '#ffffff' },
      });
      setQrCode(qrDataUrl);
    } catch (e) {
      console.error('QRCode generation failed:', e);
      setQrCode('');
    }
  };

  const handleShare = async () => {
    const isTextMode = shareType === 'text';
    if (isTextMode && !shareText.trim()) return;
    if (!isTextMode && !selectedFile) return;

    setLoading(true);
    setUploadProgress(0);
    setUploadSpeedText('-');
    setUploadEtaText('-');
    
    if (selectedFile) {
      setUploadingName(selectedFile.name);
      abortControllerRef.current = new AbortController();
    }

    let lastTs = Date.now();
    let lastLoaded = 0;

    try {
      const formData = new FormData();
      formData.append('type', shareType);
      formData.append('expiry', expiry);
      formData.append('burn_after_reading', burnAfterReading);

      if (isTextMode) {
        formData.append('text', shareText);
      } else {
        formData.append('file', selectedFile);
      }

      const res = await axios.post('/api/filebox/share', formData, {
        headers: {
          ...getAuthHeaders(),
          'Content-Type': 'multipart/form-data',
        },
        signal: abortControllerRef.current?.signal,
        onUploadProgress: (evt) => {
          if (isTextMode || !evt || !evt.total) return;

          const now = Date.now();
          const deltaMs = Math.max(1, now - lastTs);
          const deltaBytes = Math.max(0, evt.loaded - lastLoaded);
          const speed = (deltaBytes * 1000) / deltaMs;
          const remain = Math.max(0, evt.total - evt.loaded);
          const etaSec = speed > 0 ? remain / speed : Infinity;

          setUploadProgress(Math.min(100, Math.round((evt.loaded / evt.total) * 100)));
          setUploadSpeedText(formatSpeed(speed));
          setUploadEtaText(formatEta(etaSec));

          lastTs = now;
          lastLoaded = evt.loaded;
        },
      });

      if (res.data?.success) {
        setUploadProgress(100);
        setResult({ code: res.data.code });
        await generateQrCode(res.data.code);

        saveToLocalHistory({
          code: res.data.code,
          type: shareType,
          originalName: selectedFile ? selectedFile.name : null,
          content: shareText,
          size: selectedFile ? selectedFile.size : 0,
          createdAt: Date.now(),
        });

        toast.success('分享成功，取件码已生成');
      } else {
        toast.error('分享失败: ' + (res.data?.error || '未知错误'));
      }
    } catch (error) {
      if (axios.isCancel(error)) {
        return;
      }
      console.error(error);
      toast.error(error.response?.data?.error || error.message || '分享失败');
    } finally {
      setLoading(false);
      abortControllerRef.current = null;
    }
  };

  const handleRetrieve = async () => {
    const code = retrieveCode.trim().toUpperCase();
    if (!code || code.length < 5) {
      toast.warning('请输入 5 位取件码');
      return;
    }

    setLoading(true);
    try {
      const res = await axios.get(`/api/filebox/retrieve/${code}`);
      if (res.data?.success) {
        const entry = res.data.data;
        if (entry.type === 'text') {
          const contentRes = await axios.get(`/api/filebox/download/${code}`, {
            responseType: 'text',
          });
          entry.content = contentRes.data;
        }
        setRetrievedEntry(entry);
        setRetrieveCode('');
      } else {
        toast.error(res.data?.error || '取件失败');
      }
    } catch (error) {
      if (error.response && error.response.status === 404) {
        toast.error('取件码无效或已过期');
      } else {
        console.error(error);
        toast.error(error.response?.data?.error || '取件失败');
      }
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteEntry = async (code) => {
    if (!confirm(`确定要删除取件码为 "${code}" 的分享吗？`)) {
      return;
    }

    try {
      await axios.delete(`/api/filebox/${code}`, {
        headers: getAuthHeaders(),
      });
      toast.success('已成功删除分享内容');
    } catch (error) {
      console.error('后端删除失败:', error);
      toast.error(error.response?.data?.error || '删除失败');
    }

    // Filter local memory arrays and save updated local storage
    const updatedLocal = localHistory.filter((h) => h.code !== code);
    setLocalHistory(updatedLocal);
    localStorage.setItem('filebox_history', JSON.stringify(updatedLocal));

    setServerHistory((prev) => prev.filter((h) => h.code !== code));
  };

  const copyToClipboard = async (text) => {
    try {
      await navigator.clipboard.writeText(text);
      toast.success('已复制到剪贴板');
    } catch (e) {
      const textArea = document.createElement('textarea');
      textArea.value = text;
      textArea.style.position = 'fixed';
      textArea.style.left = '-9999px';
      document.body.appendChild(textArea);
      textArea.focus();
      textArea.select();
      try {
        document.execCommand('copy');
        toast.success('已复制到剪贴板');
      } catch (err) {
        toast.error('复制失败');
      }
      document.body.removeChild(textArea);
    }
  };

  const copyDownloadLink = (code) => {
    const url = `${window.location.origin}/api/filebox/download/${code}`;
    copyToClipboard(url);
  };

  const triggerDownload = (code) => {
    window.open(`/api/filebox/download/${code}`, '_blank');
  };

  return (
    <div className="space-y-6 pb-20">
      {/* ==================== 顶部 Tab 导航 ==================== */}
      <div className="flex flex-col md:flex-row justify-between items-start md:items-center border-b border-kumo-line pb-4 gap-4">
        <Tabs
          variant="segmented"
          size="sm"
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={[
            { value: 'share', label: <span className="inline-flex items-center gap-1.5"><Send className="w-3.5 h-3.5" />快捷分享</span> },
            { value: 'retrieve', label: <span className="inline-flex items-center gap-1.5"><Download className="w-3.5 h-3.5" />极速取件</span> },
            { value: 'history', label: <span className="inline-flex items-center gap-1.5"><History className="w-3.5 h-3.5" />历史记录</span> },
          ]}
        />
      </div>

      {/* ==================== 1. 取件 Tab 页面 ==================== */}
      {activeTab === 'retrieve' && (
        <div className="quick-fade-in max-w-lg mx-auto text-center space-y-6 py-8">
          <div className="space-y-2">
            <h2 className="text-base font-bold text-kumo-strong flex items-center justify-center gap-2 select-none">
              <Download className="w-5 h-5 text-kumo-brand" />
              下一秒，文件到手
            </h2>
            <p className="text-xs text-kumo-subtle">
              输入 5 位取件码，即可快速提取共享的文件或文本内容
            </p>
          </div>
          <div className="flex gap-2">
            <Input
              type="text"
              aria-label="取件码"
              value={retrieveCode}
              onChange={(e) => setRetrieveCode(e.target.value.toUpperCase())}
              placeholder="请输入 5 位取件码"
              maxLength={5}
              onKeyDown={(e) => e.key === 'Enter' && handleRetrieve()}
              className="flex-1 bg-kumo-base text-kumo-strong text-base font-bold font-mono tracking-widest text-center px-4 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
            />
            <Button
              variant="primary"
              onClick={handleRetrieve}
              disabled={loading}
              className="px-6 font-semibold"
            >
              {loading ? '提取中...' : '提取'}
            </Button>
          </div>
          <div className="bg-kumo-base border border-kumo-line rounded-lg p-5 text-left space-y-3 shadow-sm">
            <h3 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5 select-none">
              <Info className="w-4 h-4 text-kumo-brand" />
              取件说明
            </h3>
            <div className="text-[11px] text-kumo-subtle space-y-2 leading-relaxed">
              <p>1. 取件码为分享成功后生成的 5 位随机字符（字母与数字组合）。</p>
              <p>2. 请确保取件码在有效期内，过期的内容将自动销毁且不可恢复。</p>
              <p>3. 如果分享者开启了“阅后即焚”，内容在首次成功提取后将立即被永久删除。</p>
            </div>
          </div>
        </div>
      )}

      {/* ==================== 2. 分享 Tab 页面 ==================== */}
      {activeTab === 'share' && (
        <div className="quick-fade-in max-w-2xl mx-auto bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-6 relative">
          <h3 className="text-sm font-bold text-kumo-strong border-b border-kumo-line pb-3 mb-5 select-none">
            我要分享
          </h3>

          <div className="max-w-xs mb-5">
            <Tabs
              variant="segmented"
              size="sm"
              value={shareType}
              onValueChange={setShareType}
              tabs={[
                { value: 'file', label: <span className="inline-flex items-center gap-1.5"><FolderOpen className="w-3.5 h-3.5" />分享文件</span> },
                { value: 'text', label: <span className="inline-flex items-center gap-1.5"><FileText className="w-3.5 h-3.5" />分享文本</span> },
              ]}
            />
          </div>

          {shareType === 'file' ? (
            <div
              onDragEnter={() => setIsDragging(true)}
              onDragLeave={() => setIsDragging(false)}
              onDragOver={(e) => e.preventDefault()}
              onDrop={(e) => {
                e.preventDefault();
                setIsDragging(false);
                const files = e.dataTransfer?.files;
                if (files && files.length > 0) {
                  handleSelectFile(files[0]);
                }
              }}
              onClick={() => fileInputRef.current?.click()}
              className={`w-full py-12 border-2 border-dashed rounded-lg bg-kumo-recessed/10 flex flex-col items-center justify-center text-kumo-subtle cursor-pointer focus:border-kumo-brand focus:outline-none transition-all group ${
                isDragging
                  ? 'border-kumo-brand bg-kumo-brand/5'
                  : 'border-kumo-line hover:border-kumo-brand hover:bg-kumo-recessed/20'
              }`}
            >
              <Input
                type="file"
                aria-label="选择分享文件"
                ref={fileInputRef}
                className="hidden"
                onChange={(e) => {
                  const files = e.target.files;
                  if (files && files.length > 0) {
                    handleSelectFile(files[0]);
                  }
                }}
              />
              {!selectedFile ? (
                <>
                  <Upload className="w-8 h-8 mb-3 text-kumo-subtle opacity-50 group-hover:scale-105 transition-transform" />
                  <span className="text-xs font-semibold text-kumo-strong">
                    点击选择文件 或 拖拽文件到此处
                  </span>
                  <span className="text-[10px] text-kumo-subtle mt-1.5">
                    建议大小不超过 {formatFileSize(FILEBOX_MAX_FILE_SIZE)}
                  </span>
                </>
              ) : (
                <>
                  <FolderOpen className="w-8 h-8 mb-3 text-kumo-brand group-hover:scale-105 transition-transform" />
                  <span className="text-xs font-bold text-kumo-strong px-4 truncate max-w-full text-center">
                    {selectedFile.name}
                  </span>
                  <span className="text-[10px] text-kumo-subtle mt-1.5">
                    {formatFileSize(selectedFile.size)}
                  </span>
                  <Button
                    onClick={(e) => {
                      e.stopPropagation();
                      setSelectedFile(null);
                      if (fileInputRef.current) fileInputRef.current.value = '';
                    }}
                    variant="secondary"
                    size="sm"
                    className="mt-4"
                  >
                    重新选择
                  </Button>
                </>
              )}
            </div>
          ) : (
            <Textarea
              aria-label="分享文本内容"
              value={shareText}
              onChange={(e) => setShareText(e.target.value)}
              className="w-full h-32 bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand resize-none font-mono"
              placeholder="在此粘贴或输入需要分享的文本内容..."
            />
          )}

          {/* Upload progress telemetry card (only shown for file uploads during loading) */}
          {shareType === 'file' && loading && (
            <div className="mt-4 p-4 border border-kumo-line rounded-lg bg-kumo-recessed/40 space-y-3">
              <div className="flex justify-between items-center text-xs font-semibold">
                <span className="text-kumo-strong truncate max-w-[240px]">
                  {uploadingName || '文件上传中'}
                </span>
                <span className="text-kumo-brand">{uploadProgress}%</span>
              </div>
              <div className="w-full h-1.5 bg-kumo-recessed rounded-full overflow-hidden">
                <div
                  className="h-full bg-kumo-brand rounded-full transition-all duration-300"
                  style={{ width: `${uploadProgress}%` }}
                />
              </div>
              <div className="flex justify-between items-center text-[10px] text-kumo-subtle font-mono">
                <div className="flex gap-4">
                  <span>网速: {uploadSpeedText}</span>
                  <span>预计剩余: {uploadEtaText}</span>
                </div>
                <Button
                  onClick={cancelUpload}
                  variant="secondary-destructive"
                  size="xs"
                >
                  取消上传
                </Button>
              </div>
            </div>
          )}

          {/* Expiry and Burn config row */}
          <div className="mt-6 pt-5 border-t border-kumo-line flex flex-wrap items-center justify-between gap-4">
            <div className="flex items-center gap-6">
              <div className="flex items-center gap-2 text-xs">
                <span className="font-semibold text-kumo-subtle">有效期</span>
                <Select
                  aria-label="分享有效期"
                  size="sm"
                  value={expiry}
                  onValueChange={setExpiry}
                  items={[
                    { value: '1', label: '1 小时' },
                    { value: '24', label: '24 小时' },
                    { value: '168', label: '7 天' },
                  ]}
                />
              </div>

              <div className="flex items-center gap-2.5 text-xs">
                <span className="font-semibold text-kumo-subtle">阅后即焚</span>
                <Switch
                  checked={burnAfterReading}
                  onCheckedChange={setBurnAfterReading}
                  size="sm"
                />
              </div>
            </div>

            <Button
              variant="primary"
              disabled={loading || (shareType === 'file' ? !selectedFile : !shareText.trim())}
              onClick={handleShare}
              className="px-6 h-9 font-semibold"
            >
              {loading ? '分享中...' : '立即分享'}
            </Button>
          </div>

          {/* Success Overlay result */}
          {result && (
            <div className="absolute inset-0 bg-kumo-base rounded-lg p-6 flex flex-col items-center justify-center text-center space-y-4 z-20">
              <span className="w-12 h-12 rounded-full bg-kumo-success/10 text-kumo-success flex items-center justify-center text-2xl font-bold">
                ✓
              </span>
              <div className="space-y-1">
                <h3 className="text-sm font-bold text-kumo-strong">分享成功</h3>
                <p className="text-[11px] text-kumo-subtle">
                  取件码已生成，凭此码可在取件页面提取共享内容
                </p>
              </div>

              <div className="text-2xl font-bold font-mono tracking-widest text-kumo-brand bg-kumo-recessed px-6 py-2 rounded-lg border border-kumo-line select-all">
                {result.code}
              </div>

              {/* QR Code */}
              {qrCode && (
                <div className="flex flex-col items-center justify-center p-2.5 border border-kumo-line bg-kumo-recessed rounded-lg">
                  <img src={qrCode} alt="取件二维码" className="w-[110px] h-[110px]" />
                  <span className="text-[9px] text-kumo-subtle mt-1">扫码快速提取</span>
                </div>
              )}

              <div className="flex gap-2 pt-2">
                <Button onClick={() => copyToClipboard(result.code)}>复制码</Button>
                <Button
                  onClick={() =>
                    copyToClipboard(`${window.location.origin}/api/filebox/download/${result.code}`)
                  }
                >
                  复制链接
                </Button>
                <Button variant="primary" onClick={resetForm}>
                  继续分享
                </Button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* ==================== 3. 历史记录 Tab 页面 ==================== */}
      {activeTab === 'history' && (
        <div className="quick-fade-in space-y-6">
          {/* 本地分享历史 */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-6 space-y-4">
            <div className="flex justify-between items-center border-b border-kumo-line pb-3 select-none">
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-semibold text-kumo-strong">本地分享历史</h3>
                <span className="text-[10px] text-kumo-subtle bg-kumo-recessed border border-kumo-line px-1.5 py-0.5 rounded font-mono font-semibold">
                  本地 {localHistory.length} 条
                </span>
              </div>
              <div className="flex gap-2">
                <Button
                  onClick={loadServerHistory}
                  variant="secondary"
                  size="sm"
                  shape="square"
                  aria-label="刷新服务端历史"
                  className="text-kumo-subtle hover:text-kumo-strong"
                  title="刷新服务端历史"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${historyLoading ? 'animate-spin' : ''}`} />
                </Button>
                <Button
                  onClick={clearLocalHistory}
                  variant="secondary-destructive"
                  size="sm"
                  shape="square"
                  aria-label="清空本地历史"
                  className="hover:bg-kumo-danger/10"
                  title="清空本地历史"
                >
                  <Trash className="w-3.5 h-3.5" />
                </Button>
              </div>
            </div>

            {localHistory.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-12 text-kumo-subtle">
                <FolderOpen className="w-10 h-10 opacity-30 mb-2.5" />
                <span className="text-xs">尚未分享过任何内容</span>
              </div>
            ) : (
              <div className="divide-y divide-kumo-line">
                {localHistory.map((item) => (
                  <div key={item.code} className="flex justify-between items-center py-3 first:pt-0 last:pb-0">
                    <div className="flex items-center gap-3">
                      <div
                        className={`w-8 h-8 rounded-lg flex items-center justify-center text-xs font-semibold ${
                          item.type === 'file'
                            ? 'bg-kumo-brand/10 text-kumo-brand border border-kumo-brand/20'
                            : 'bg-kumo-success/10 text-kumo-success border border-kumo-success/20'
                        }`}
                      >
                        {item.type === 'file' ? <FolderOpen className="w-4 h-4" /> : <FileText className="w-4 h-4" />}
                      </div>
                      <div className="min-w-0">
                        <p className="text-xs font-bold text-kumo-strong truncate max-w-sm">
                          {item.type === 'file'
                            ? item.originalName
                            : item.content
                            ? item.content.substring(0, 50) + (item.content.length > 50 ? '...' : '')
                            : '文本内容'}
                        </p>
                        <p className="text-[10px] text-kumo-subtle mt-0.5">
                          {formatDateTime(item.createdAt)}
                          {item.type === 'file' && ` · ${formatFileSize(item.size)}`}
                        </p>
                      </div>
                    </div>

                    <div className="flex items-center gap-3">
                      <span className="text-xs font-mono font-bold text-kumo-brand bg-kumo-recessed px-2 py-0.5 border border-kumo-line rounded select-all">
                        {item.code}
                      </span>
                      <div className="flex gap-1.5">
                        <Button
                          onClick={() => copyDownloadLink(item.code)}
                          variant="secondary"
                          size="sm"
                          shape="square"
                          aria-label="复制下载链接"
                          className="text-kumo-subtle hover:text-kumo-strong"
                          title="复制下载链接"
                        >
                          <Send className="w-3.5 h-3.5" />
                        </Button>
                        <Button
                          onClick={() => handleDeleteEntry(item.code)}
                          variant="secondary-destructive"
                          size="sm"
                          shape="square"
                          aria-label="删除分享记录"
                          className="hover:bg-kumo-danger/10"
                          title="删除"
                        >
                          <Trash className="w-3.5 h-3.5" />
                        </Button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* 服务端文件历史 */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-6 space-y-4">
            <div className="flex justify-between items-center border-b border-kumo-line pb-3 select-none">
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-semibold text-kumo-strong">服务端文件历史</h3>
                <span className="text-[10px] text-kumo-subtle bg-kumo-recessed border border-kumo-line px-1.5 py-0.5 rounded font-mono font-semibold">
                  {serverHistory.length} 条
                </span>
              </div>
            </div>

            {historyLoading ? (
              <div className="space-y-3.5">
                {[...Array(3)].map((_, i) => (
                  <div key={i} className="flex justify-between items-center py-3">
                    <div className="flex items-center gap-3 flex-1">
                      <SkeletonLine className="w-8 h-8 rounded-lg" />
                      <div className="flex-1 space-y-1.5">
                        <SkeletonLine className="w-1/3 h-3.5" />
                        <SkeletonLine className="w-1/2 h-2.5" />
                      </div>
                    </div>
                    <SkeletonLine className="w-12 h-6 rounded" />
                  </div>
                ))}
              </div>
            ) : serverHistory.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-12 text-kumo-subtle">
                <FolderOpen className="w-10 h-10 opacity-30 mb-2.5" />
                <span className="text-xs">服务端暂无可用文件记录</span>
              </div>
            ) : (
              <div className="divide-y divide-kumo-line">
                {serverHistory.map((item) => (
                  <div key={'server-' + item.code} className="flex justify-between items-center py-3 first:pt-0 last:pb-0">
                    <div className="flex items-center gap-3">
                      <div
                        className={`w-8 h-8 rounded-lg flex items-center justify-center text-xs font-semibold ${
                          item.type === 'file'
                            ? 'bg-kumo-brand/10 text-kumo-brand border border-kumo-brand/20'
                            : 'bg-kumo-success/10 text-kumo-success border border-kumo-success/20'
                        }`}
                      >
                        {item.type === 'file' ? <FolderOpen className="w-4 h-4" /> : <FileText className="w-4 h-4" />}
                      </div>
                      <div className="min-w-0">
                        <p className="text-xs font-bold text-kumo-strong truncate max-w-sm">
                          {item.type === 'file' ? item.originalName : '文本内容'}
                        </p>
                        <p className="text-[10px] text-kumo-subtle mt-0.5">
                          创建时间: {formatDateTime(item.createdAt)} · 到期时间: {formatDateTime(item.expiry)} · 下载 {item.downloads || 0} 次
                        </p>
                      </div>
                    </div>

                    <div className="flex items-center gap-3">
                      <span className="text-xs font-mono font-bold text-kumo-brand bg-kumo-recessed px-2 py-0.5 border border-kumo-line rounded select-all">
                        {item.code}
                      </span>
                      <div className="flex gap-1.5">
                        <Button
                          onClick={() => copyDownloadLink(item.code)}
                          variant="secondary"
                          size="sm"
                          shape="square"
                          aria-label="复制下载链接"
                          className="text-kumo-subtle hover:text-kumo-strong"
                          title="复制下载链接"
                        >
                          <Send className="w-3.5 h-3.5" />
                        </Button>
                        <Button
                          onClick={() => handleDeleteEntry(item.code)}
                          variant="secondary-destructive"
                          size="sm"
                          shape="square"
                          aria-label="删除分享记录"
                          className="hover:bg-kumo-danger/10"
                          title="删除"
                        >
                          <Trash className="w-3.5 h-3.5" />
                        </Button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* ==================== 模态框: 提取结果 ==================== */}
      <Dialog.Root open={retrievedEntry !== null} onOpenChange={(open) => !open && setRetrievedEntry(null)}>
        <Dialog className="p-6 sm:max-w-md">
          <Dialog.Title className="text-base font-bold text-kumo-strong mb-1 select-none flex items-center gap-2">
            <FolderOpen className="w-5 h-5 text-kumo-brand" />
            文件提取成功
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            已成功提取到分享内容，请妥善保管。
          </Dialog.Description>

          {retrievedEntry && (
            <div className="space-y-4">
              <div className="flex flex-col items-center justify-center py-4 space-y-2">
                <div
                  className={`w-14 h-14 rounded-2xl flex items-center justify-center text-xl ${
                    retrievedEntry.type === 'file'
                      ? 'bg-kumo-brand/10 text-kumo-brand border border-kumo-brand/20'
                      : 'bg-kumo-success/10 text-kumo-success border border-kumo-success/20'
                  }`}
                >
                  {retrievedEntry.type === 'file' ? <FolderOpen className="w-7 h-7" /> : <FileText className="w-7 h-7" />}
                </div>
                <h4 className="text-sm font-bold text-kumo-strong max-w-xs truncate text-center select-all">
                  {retrievedEntry.type === 'file' ? retrievedEntry.originalName : '分享文本结果'}
                </h4>
              </div>

              {retrievedEntry.type === 'file' ? (
                <div className="flex justify-center gap-4 text-[10px] text-kumo-subtle font-mono bg-kumo-recessed border border-kumo-line p-2.5 rounded-lg select-none">
                  <span>大小: {formatFileSize(retrievedEntry.size)}</span>
                  <span>有效期至: {formatDateTime(retrievedEntry.expiry)}</span>
                </div>
              ) : (
                <div className="bg-kumo-recessed border border-kumo-line p-3 rounded-lg max-h-40 overflow-y-auto font-mono text-xs text-kumo-strong select-text whitespace-pre-wrap leading-relaxed scrollbar-thin">
                  {retrievedEntry.content}
                </div>
              )}

              {retrievedEntry.burnAfterReading && (
                <div className="p-3 bg-kumo-danger/10 border border-kumo-danger/20 text-kumo-danger text-[10px] rounded-md flex items-center gap-2 select-none">
                  <span>🔥 此内容开启了"阅后即焚"，本次提取后服务端已自动销毁且无法再次获取。</span>
                </div>
              )}

              <div className="flex justify-end gap-3 pt-2">
                <Dialog.Close asChild>
                  <Button>关闭</Button>
                </Dialog.Close>
                {retrievedEntry.type === 'file' ? (
                  <Button variant="primary" onClick={() => triggerDownload(retrievedEntry.code)}>
                    下载文件
                  </Button>
                ) : (
                  <Button variant="primary" onClick={() => copyToClipboard(retrievedEntry.content)}>
                    复制文本
                  </Button>
                )}
              </div>
            </div>
          )}
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default FileboxPage;
