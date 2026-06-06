import React, { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Table } from '@cloudflare/kumo/components/table';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Select } from '@cloudflare/kumo/components/select';
import { Tabs } from '@cloudflare/kumo';
import useTableResize from '../composables/useTableResize.js';
import useStore from '../store.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { renderMarkdown, formatDateTime } from '../modules/utils.js';
import {
  FolderOpen,
  Settings,
  Clock,
  Plus,
  Trash,
  RefreshCw,
  Play,
  Pause,
  Folder,
  FileText,
  Download,
  Edit,
  X,
  Check,
  Eye,
  Grid,
  Database,
  Plug,
  Info,
  ChevronRight,
  Sliders,
  History,
  AlertTriangle
} from '../components/Icons.jsx';

function SelfHPage() {
  const [subTab, setSubTab] = useState('files'); // 'files' | 'settings' | 'cron' | 'temp'
  const [tempTabs, setTempTabs] = useState([]);
  const [activeTempTabId, setActiveTempTabId] = useState(null);

  // OpenList Accounts & Selected
  const [accounts, setAccounts] = useState([]);
  const [currentAccount, setCurrentAccount] = useState(null);
  const [loadingAccounts, setLoadingAccounts] = useState(false);

  // OpenList Files State
  const [files, setFiles] = useState([]);
  const [loadingFiles, setLoadingFiles] = useState(false);
  const [currentPath, setCurrentPath] = useState('/');
  const [readmeContent, setReadmeContent] = useState('');
  const [layoutMode, setLayoutMode] = useState('list'); // 'list' | 'grid'
  const [previewSize, setPreviewSize] = useState(800);
  const [readmeVisible, setReadmeVisible] = useState(true);

  // Modals & Forms (Accounts)
  const [newAccForm, setNewAccForm] = useState({ name: '', api_url: '', api_token: '' });
  const [testingAccId, setTestingAccId] = useState(null);

  // File Context Menu State
  const [contextMenu, setContextMenu] = useState({ visible: false, x: 0, y: 0, file: null, path: '' });

  // Image Preview Modal
  const [imagePreview, setImagePreview] = useState({ visible: false, url: '', filename: '', path: '', loading: false });

  // Detail Modal
  const [detailModal, setDetailModal] = useState({ visible: false, title: '', content: '' });

  // Cron Tasks States
  const [cronTasks, setCronTasks] = useState([]);
  const [loadingCron, setLoadingCron] = useState(false);
  const [cronLogs, setCronLogs] = useState([]);
  const [editingCronTask, setEditingCronTask] = useState(null); // Task object or null

  // Table Resize Hooks
  const [fileColWidths, startFileResize] = useTableResize([450, 150, 200]);
  const [cronColWidths, startCronResize] = useTableResize([180, 150, 120, 250, 100]);

  // Headers
  const getAuthHeaders = useCallback(() => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
    };
  }, []);

  // ==================== 1. Load Accounts & Settings ====================
  const loadAccounts = useCallback(async () => {
    setLoadingAccounts(true);
    try {
      const response = await fetch('/api/openlist/manage-accounts', { headers: getAuthHeaders() });
      const data = await response.json();
      if (data.success) {
        setAccounts(data.data || []);
        
        // Restore last selected account
        const savedAccountId = localStorage.getItem('openlist_last_account');
        const savedAccount = savedAccountId
          ? (data.data || []).find(a => String(a.id) === savedAccountId)
          : null;

        const defaultAcc = savedAccount || (data.data && data.data.length > 0 ? data.data[0] : null);
        if (defaultAcc) {
          setCurrentAccount(defaultAcc);
          // Load preference
          loadPreference(defaultAcc.id);
        }
      }
    } catch (e) {
      toast.error('加载 OpenList 账号失败');
    } finally {
      setLoadingAccounts(false);
    }
  }, [getAuthHeaders]);

  const loadPreference = async (accountId) => {
    try {
      const res = await fetch('/api/openlist/settings/preview_size', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success && data.value) {
        setPreviewSize(parseInt(data.value));
      }
      
      const savedMode = localStorage.getItem('openListLayoutMode');
      if (savedMode && ['list', 'grid'].includes(savedMode)) {
        setLayoutMode(savedMode);
      }
    } catch (e) {
      console.warn('加载设置失败:', e);
    }
  };

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  // ==================== 2. OpenList Files Management ====================
  const loadFiles = useCallback(async (path = currentPath, refresh = false) => {
    if (!currentAccount) return;
    setLoadingFiles(true);
    setFiles([]);
    setReadmeContent('');
    setCurrentPath(path);
    localStorage.setItem(`openlist_path_${currentAccount.id}`, path);

    try {
      const response = await fetch(`/api/openlist/${currentAccount.id}/fs/list`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ path, refresh }),
      });
      if (!response.ok) {
        toast.error(`服务器错误: ${response.status}`);
        return;
      }
      const data = await response.json();
      if (data.code === 200) {
        setFiles(data.data.content || []);
        setReadmeContent(data.data.readme || '');
      } else {
        toast.error('加载失败: ' + (data.message || '未知错误'));
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    } finally {
      setLoadingFiles(false);
    }
  }, [currentAccount, currentPath, getAuthHeaders]);

  useEffect(() => {
    if (currentAccount && subTab === 'files') {
      const savedPath = localStorage.getItem(`openlist_path_${currentAccount.id}`) || '/';
      loadFiles(savedPath);
    }
  }, [currentAccount, subTab]);

  const handleSelectAccount = (acc) => {
    setCurrentAccount(acc);
    localStorage.setItem('openlist_last_account', String(acc.id));
    setSubTab('files');
    const savedPath = localStorage.getItem(`openlist_path_${acc.id}`) || '/';
    loadFiles(savedPath);
  };

  const getFilePath = (file, baseDir = currentPath) => {
    let name = file && file.name ? String(file.name) : '';
    name = name.replace(/^\//, '');
    let parent = file && file.parent !== undefined && file.parent !== null ? file.parent : baseDir;
    if (!parent || parent === '/') return '/' + name;
    if (!parent.startsWith('/')) parent = '/' + parent;
    parent = parent.replace(/\/$/, '');
    return `${parent}/${name}`;
  };

  const handleOpenFile = (file) => {
    if (file.is_dir) {
      const newPath = getFilePath(file);
      loadFiles(newPath);
    } else {
      const fileName = file.name.toLowerCase();
      if (/\.(jpg|jpeg|png|gif|webp|svg|bmp|ico)$/i.test(fileName)) {
        openImagePreview(file);
      } else {
        showFileDetail(file);
      }
    }
  };

  const getFileIcon = (file) => {
    if (file.is_dir) return <Folder className="w-4 h-4 text-warning" />;
    const name = file.name.toLowerCase();
    if (/\.(jpg|jpeg|png|gif|webp|svg)$/.test(name)) return <FileText className="w-4 h-4 text-success" />;
    if (/\.(mp4|webm|mkv|avi)$/.test(name)) return <Play className="w-4 h-4 text-danger animate-pulse" />;
    if (/\.(zip|rar|7z|gz|tar)$/.test(name)) return <FileText className="w-4 h-4 text-warning" />;
    return <FileText className="w-4 h-4 text-secondary" />;
  };

  const getOpenListFileSize = (file) => {
    if (file.is_dir) return '';
    if (!file.size || file.size <= 0) return '-';
    return formatFileSize(file.size);
  };

  const formatFileSize = (bytes) => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  // Breadcrumbs
  const pathParts = useMemo(() => {
    if (!currentPath || currentPath === '/') return [];
    const parts = currentPath.split('/').filter(p => p);
    let current = '';
    return parts.map(p => {
      current += '/' + p;
      return { name: p, path: current };
    });
  }, [currentPath]);

  // Sorting
  const [sortKey, setSortKey] = useState(null); // 'name' | 'size' | 'modified'
  const [sortOrder, setSortOrder] = useState('asc'); // 'asc' | 'desc'

  const sortedFiles = useMemo(() => {
    if (!sortKey) return files;
    const items = [...files];
    return items.sort((a, b) => {
      if (a.is_dir !== b.is_dir) {
        return a.is_dir ? -1 : 1;
      }
      let valA = a[sortKey];
      let valB = b[sortKey];
      if (sortKey === 'name') {
        valA = String(valA || '').toLowerCase();
        valB = String(valB || '').toLowerCase();
      } else {
        valA = Number(valA || 0);
        valB = Number(valB || 0);
      }
      if (valA < valB) return sortOrder === 'asc' ? -1 : 1;
      if (valA > valB) return sortOrder === 'asc' ? 1 : -1;
      return 0;
    });
  }, [files, sortKey, sortOrder]);

  const toggleSort = (key) => {
    if (sortKey === key) {
      setSortOrder(prev => prev === 'asc' ? 'desc' : 'asc');
    } else {
      setSortKey(key);
      setSortOrder('asc');
    }
  };

  // Create Folder
  const mkdirOpenList = async () => {
    const name = await dialog.prompt({
      message: '新建文件夹名称:',
    });
    if (!name) return;
    try {
      const response = await fetch(`/api/openlist/${currentAccount.id}/proxy/fs/mkdir`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          path: currentPath === '/' ? `/${name}` : `${currentPath}/${name}`,
        }),
      });
      const data = await response.json();
      if (data.code === 200) {
        toast.success('新建成功');
        loadFiles(currentPath, true);
      } else {
        toast.error('新建失败: ' + (data.message || '未知错误'));
      }
    } catch (e) {
      toast.error('新建请求失败');
    }
  };

  // Image Preview
  const openImagePreview = async (file) => {
    const fullPath = getFilePath(file);
    try {
      setImagePreview({ visible: true, url: '', filename: file.name, path: fullPath, loading: true });
      const response = await fetch(`/api/openlist/${currentAccount.id}/fs/get`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ path: fullPath }),
      });
      const data = await response.json();
      if (data.code === 200 && data.data.raw_url) {
        setImagePreview(prev => ({ ...prev, url: data.data.raw_url, loading: false }));
      } else {
        toast.error('获取图片直链失败');
        setImagePreview(prev => ({ ...prev, visible: false }));
      }
    } catch (e) {
      toast.error('获取图片直链失败');
      setImagePreview(prev => ({ ...prev, visible: false }));
    }
  };

  const downloadFile = async (file) => {
    const fullPath = getFilePath(file);
    try {
      const response = await fetch(`/api/openlist/${currentAccount.id}/fs/get`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ path: fullPath }),
      });
      const data = await response.json();
      if (data.code === 200 && data.data.raw_url) {
        window.open(data.data.raw_url, '_blank');
      } else {
        toast.error('获取下载链接失败');
      }
    } catch (e) {
      toast.error('获取下载链接失败');
    }
  };

  const deleteFile = async (file) => {
    if (!(await dialog.confirm(`确认删除 "${file.name}" 吗？`))) return;
    try {
      const response = await fetch(`/api/openlist/${currentAccount.id}/proxy/fs/remove`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          names: [file.name],
          dir: currentPath,
        }),
      });
      const data = await response.json();
      if (data.code === 200) {
        toast.success('删除成功');
        loadFiles(currentPath, true);
      } else {
        toast.error('删除失败: ' + (data.message || '未知错误'));
      }
    } catch (e) {
      toast.error('删除请求异常');
    }
  };

  const renameFile = async (file) => {
    const newName = await dialog.prompt({
      message: '请输入新名称:',
      defaultValue: file.name,
    });
    if (!newName || newName === file.name) return;
    try {
      const response = await fetch(`/api/openlist/${currentAccount.id}/proxy/fs/rename`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          name: newName,
          path: getFilePath(file),
        }),
      });
      const data = await response.json();
      if (data.code === 200) {
        toast.success('重命名成功');
        loadFiles(currentPath, true);
      } else {
        toast.error('重命名失败: ' + (data.message || '未知错误'));
      }
    } catch (e) {
      toast.error('重命名请求异常');
    }
  };

  const showFileDetail = async (file) => {
    const fullPath = getFilePath(file);
    try {
      const response = await fetch(`/api/openlist/${currentAccount.id}/fs/get`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ path: fullPath }),
      });
      const data = await response.json();
      if (data.code === 200) {
        const info = data.data;
        const details = (
          <div className="text-xs space-y-2 font-medium text-kumo-default">
            <p><span className="text-kumo-subtle font-bold mr-2">名称:</span> {info.name}</p>
            <p><span className="text-kumo-subtle font-bold mr-2">路径:</span> <code className="bg-kumo-recessed px-1 rounded">{fullPath}</code></p>
            <p><span className="text-kumo-subtle font-bold mr-2">大小:</span> {formatFileSize(info.size)}</p>
            <p><span className="text-kumo-subtle font-bold mr-2">修改时间:</span> {formatDateTime(info.modified)}</p>
            {info.driver && <p><span className="text-kumo-subtle font-bold mr-2">存储驱动:</span> <span className="bg-kumo-recessed px-1.5 py-0.5 rounded text-[10px] font-bold border border-kumo-line">{info.driver}</span></p>}
            {info.hash_info?.sha1 && <p><span className="text-kumo-subtle font-bold mr-2">SHA1:</span> <code className="break-all font-mono text-[10px]">{info.hash_info.sha1}</code></p>}
            {info.hash_info?.md5 && <p><span className="text-kumo-subtle font-bold mr-2">MD5:</span> <code className="break-all font-mono text-[10px]">{info.hash_info.md5}</code></p>}
          </div>
        );
        setDetailModal({ visible: true, title: '文件详情', content: details });
      } else {
        toast.error('获取详情失败');
      }
    } catch (e) {
      toast.error('详情请求出错');
    }
  };

  // Context Menu Interactions
  const onRowContextMenu = (e, file) => {
    e.preventDefault();
    setContextMenu({
      visible: true,
      x: e.clientX,
      y: e.clientY,
      file,
      path: currentPath
    });
  };

  useEffect(() => {
    const handleGlobalClick = () => {
      if (contextMenu.visible) {
        setContextMenu(prev => ({ ...prev, visible: false }));
      }
    };
    window.addEventListener('click', handleGlobalClick);
    return () => window.removeEventListener('click', handleGlobalClick);
  }, [contextMenu.visible]);

  // ==================== 3. Settings Tab ====================
  const handleAddAccount = async () => {
    if (!newAccForm.name || !newAccForm.api_url || !newAccForm.api_token) {
      toast.warning('请填写完整实例配置');
      return;
    }
    try {
      const response = await fetch('/api/openlist/manage-accounts', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(newAccForm),
      });
      const data = await response.json();
      if (data.success) {
        toast.success('实例已添加');
        setNewAccForm({ name: '', api_url: '', api_token: '' });
        loadAccounts();
      } else {
        toast.error('添加失败: ' + (data.error || '未知错误'));
      }
    } catch (e) {
      toast.error('添加失败');
    }
  };

  const handleDeleteAccount = async (id) => {
    if (!(await dialog.confirm('确认删除此 OpenList 实例配置吗？'))) return;
    try {
      const response = await fetch(`/api/openlist/manage-accounts/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        toast.success('实例已删除');
        if (currentAccount && currentAccount.id === id) {
          setCurrentAccount(null);
        }
        loadAccounts();
      } else {
        toast.error('删除失败');
      }
    } catch (e) {
      toast.error('删除失败');
    }
  };

  const handleTestAccount = async (id) => {
    setTestingAccId(id);
    try {
      const response = await fetch(`/api/openlist/manage-accounts/${id}/test`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        const result = data.data;
        if (result.status === 'online') {
          toast.success(`连接成功！用户: ${result.user?.username || '未知'}`);
        } else if (result.status === 'auth_failed') {
          toast.warning('认证失败，请检查令牌');
        } else {
          toast.error('测试失败: ' + (result.error || '服务离线'));
        }
        loadAccounts();
      }
    } catch (e) {
      toast.error('测试请求失败');
    } finally {
      setTestingAccId(null);
    }
  };

  const handleSavePreferences = async () => {
    try {
      const response = await fetch('/api/openlist/settings', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ key: 'preview_size', value: String(previewSize) }),
      });
      const data = await response.json();
      if (data.success) {
        toast.success('偏好设置已保存');
      } else {
        toast.error('保存失败');
      }
    } catch (e) {
      toast.error('保存请求异常');
    }
  };

  // ==================== 4. Cron Scheduler Tab ====================
  const loadCronTasks = useCallback(async () => {
    setLoadingCron(true);
    try {
      const res = await fetch('/api/cron/tasks', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success) {
        setCronTasks(data.data || []);
      }
    } catch (e) {
      toast.error('加载定时任务失败');
    } finally {
      setLoadingCron(false);
    }
  }, [getAuthHeaders]);

  const loadCronLogs = useCallback(async () => {
    try {
      const res = await fetch('/api/cron/logs', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success) {
        setCronLogs(data.data || []);
      }
    } catch (e) {
      console.warn('加载运行日志失败');
    }
  }, [getAuthHeaders]);

  useEffect(() => {
    if (subTab === 'cron') {
      loadCronTasks();
      loadCronLogs();
    }
  }, [subTab, loadCronTasks, loadCronLogs]);

  const handleToggleCronTask = async (task) => {
    try {
      const nextEnabled = task.enabled ? 0 : 1;
      const res = await fetch(`/api/cron/tasks/${task.id}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled: nextEnabled }),
      });
      const data = await res.json();
      if (data.success) {
        toast.success(nextEnabled ? '任务已启用' : '任务已禁用');
        loadCronTasks();
      }
    } catch (e) {
      toast.error('更新状态异常');
    }
  };

  const handleRunCronTask = async (task) => {
    try {
      toast.info('正在触发定时任务...');
      const res = await fetch(`/api/cron/tasks/${task.id}/run`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        toast.success('任务已触发开始执行');
        setTimeout(loadCronLogs, 1500);
      }
    } catch (e) {
      toast.error('任务触发失败');
    }
  };

  const handleDeleteCronTask = async (task) => {
    if (!(await dialog.confirm(`确认删除定时任务 "${task.name}" 吗？`))) return;
    try {
      const res = await fetch(`/api/cron/tasks/${task.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        toast.success('定时任务已删除');
        loadCronTasks();
      }
    } catch (e) {
      toast.error('删除定时任务失败');
    }
  };

  const handleClearCronLogs = async () => {
    if (!(await dialog.confirm('确认清理 7 天前的所有运行日志吗？'))) return;
    try {
      const res = await fetch('/api/cron/logs?days=7', {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        toast.success('日志已清理');
        loadCronLogs();
      }
    } catch (e) {
      toast.error('清理日志异常');
    }
  };

  // Cron Form State & Builder Helper
  const [cronForm, setCronForm] = useState({
    name: '',
    useCustom: false,
    periodType: 'day',
    hour: 0,
    minute: 0,
    dayOfMonth: 1,
    weekday: '1',
    schedule: '0 0 * * *',
    type: 'shell',
    command: '',
    enabled: 1
  });

  const getCronExpressionFromSimple = (form) => {
    if (form.useCustom) return form.schedule;
    const m = form.minute ?? 0;
    const h = form.hour ?? 0;
    const d = form.dayOfMonth ?? 1;
    const w = form.weekday ?? '1';

    switch (form.periodType) {
      case 'minute': return '* * * * *';
      case 'hour': return `${m} * * * *`;
      case 'day': return `${m} ${h} * * *`;
      case 'week': return `${m} ${h} * * ${w}`;
      case 'month': return `${m} ${h} ${d} * *`;
      default: return '0 0 * * *';
    }
  };

  const openCronEditModal = (task = null) => {
    if (task) {
      // Parse schedule
      const parts = (task.schedule || '').split(' ');
      let simpleConfig = {
        useCustom: parts.length !== 5,
        periodType: 'day',
        hour: 0,
        minute: 0,
        dayOfMonth: 1,
        weekday: '1',
      };
      if (parts.length === 5) {
        const [m, h, d, month, w] = parts;
        if (m === '*' && h === '*' && d === '*' && month === '*' && w === '*') {
          simpleConfig.periodType = 'minute';
        } else if (h === '*' && d === '*' && month === '*' && w === '*' && /^\d+$/.test(m)) {
          simpleConfig.periodType = 'hour';
          simpleConfig.minute = parseInt(m);
        } else if (d === '*' && month === '*' && w === '*' && /^\d+$/.test(m) && /^\d+$/.test(h)) {
          simpleConfig.periodType = 'day';
          simpleConfig.minute = parseInt(m);
          simpleConfig.hour = parseInt(h);
        } else if (d === '*' && month === '*' && /^\d+$/.test(w) && /^\d+$/.test(m) && /^\d+$/.test(h)) {
          simpleConfig.periodType = 'week';
          simpleConfig.minute = parseInt(m);
          simpleConfig.hour = parseInt(h);
          simpleConfig.weekday = w;
        } else if (month === '*' && w === '*' && /^\d+$/.test(d) && /^\d+$/.test(m) && /^\d+$/.test(h)) {
          simpleConfig.periodType = 'month';
          simpleConfig.minute = parseInt(m);
          simpleConfig.hour = parseInt(h);
          simpleConfig.dayOfMonth = parseInt(d);
        } else {
          simpleConfig.useCustom = true;
        }
      }
      setEditingCronTask({
        id: task.id,
        ...task,
        ...simpleConfig,
      });
      setCronForm({
        name: task.name || '',
        useCustom: simpleConfig.useCustom,
        periodType: simpleConfig.periodType,
        hour: simpleConfig.hour,
        minute: simpleConfig.minute,
        dayOfMonth: simpleConfig.dayOfMonth,
        weekday: simpleConfig.weekday,
        schedule: task.schedule || '* * * * *',
        type: task.type || 'shell',
        command: task.command || '',
        enabled: task.enabled ?? 1
      });
    } else {
      setEditingCronTask({ id: null });
      setCronForm({
        name: '',
        useCustom: false,
        periodType: 'day',
        hour: 0,
        minute: 0,
        dayOfMonth: 1,
        weekday: '1',
        schedule: '0 0 * * *',
        type: 'shell',
        command: '',
        enabled: 1
      });
    }
  };

  const handleSaveCronTask = async () => {
    if (!cronForm.name || !cronForm.command) {
      toast.warning('请输入名称和命令/URL');
      return;
    }
    const finalSchedule = getCronExpressionFromSimple(cronForm);
    const body = {
      name: cronForm.name,
      schedule: finalSchedule,
      type: cronForm.type,
      command: cronForm.command,
      enabled: cronForm.enabled
    };
    try {
      const isEdit = editingCronTask && editingCronTask.id;
      const url = isEdit ? `/api/cron/tasks/${editingCronTask.id}` : '/api/cron/tasks';
      const method = isEdit ? 'PUT' : 'POST';

      const res = await fetch(url, {
        method,
        headers: getAuthHeaders(),
        body: JSON.stringify(body),
      });
      const data = await res.json();
      if (data.success) {
        toast.success('任务保存成功');
        setEditingCronTask(null);
        loadCronTasks();
      } else {
        toast.error('保存失败: ' + (data.error || '未知错误'));
      }
    } catch (e) {
      toast.error('保存请求异常');
    }
  };

  return (
    <div className="flex flex-col gap-6 w-full px-1">
      {/* Sec Tab Header */}
      <div className="flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4">
        <Tabs
          {...MODULE_TABS_PROPS}
          value={subTab}
          onValueChange={setSubTab}
          tabs={[
            { value: 'files', label: <span className="inline-flex items-center gap-1.5"><FolderOpen className="w-4 h-4" />文件列表</span> },
            { value: 'settings', label: <span className="inline-flex items-center gap-1.5"><Settings className="w-4 h-4" />配置</span> },
            { value: 'cron', label: <span className="inline-flex items-center gap-1.5"><Clock className="w-4 h-4" />定时任务</span> },
          ]}
        />

        {subTab === 'files' && currentAccount && (
          <div className="flex items-center gap-3">
            {accounts.length > 1 && (
              <div className="flex items-center gap-2">
                <span className="text-xs text-kumo-subtle font-medium">实例</span>
                <Select
                  aria-label="OpenList 实例"
                  size="sm"
                  value={String(currentAccount.id)}
                  onValueChange={(value) => handleSelectAccount(accounts.find(a => String(a.id) === value))}
                  items={accounts.map((acc) => ({ value: String(acc.id), label: acc.name }))}
                />
              </div>
            )}
            <Button
              onClick={() => loadFiles(currentPath, true)}
              disabled={loadingFiles}
              className="h-8 px-2.5 flex items-center justify-center bg-kumo-base border border-kumo-line text-kumo-strong hover:bg-kumo-recessed"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${loadingFiles ? 'animate-spin' : ''}`} />
            </Button>
          </div>
        )}
      </div>

      {/* Main SubTab Content */}
      <div className="min-h-[400px]">
        {/* ==================== 1. Files List ==================== */}
        {subTab === 'files' && (
          <div className="space-y-4">
            {!currentAccount ? (
              <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-16 text-center flex flex-col items-center justify-center gap-4">
                <Plug className="w-10 h-10 text-kumo-subtle" />
                <h4 className="text-sm font-bold text-kumo-strong">尚未连接 OpenList 实例</h4>
                <p className="text-xs text-kumo-subtle">请前往配置页面添加并连接一个 OpenList 文件存储服务。</p>
                <Button variant="primary" onClick={() => setSubTab('settings')} className="flex items-center gap-1">
                  <Plus className="w-4 h-4" />
                  <span>前往配置</span>
                </Button>
              </div>
            ) : (
              <div className="space-y-4">
                {/* Path Breadcrumbs toolbar */}
                <div className="bg-kumo-base border border-kumo-line rounded-lg p-3 flex flex-wrap justify-between items-center gap-4 shadow-sm">
                  {/* Left: Path Breadcrumbs */}
                  <div className="flex items-center gap-1 flex-wrap text-xs text-kumo-default font-semibold">
                    <Button
                      onClick={() => loadFiles('/')}
                      variant="ghost"
                      size="sm"
                      className="hover:text-kumo-brand"
                    >
                      <Folder className="w-4 h-4 mr-1 text-warning" />
                      Home
                    </Button>
                    {pathParts.map((part, idx) => (
                      <React.Fragment key={part.path}>
                        <ChevronRight className="w-3.5 h-3.5 text-kumo-subtle" />
                        <Button
                          onClick={() => loadFiles(part.path)}
                          variant="ghost"
                          size="sm"
                          className={`hover:text-kumo-brand ${idx === pathParts.length - 1 ? 'text-kumo-strong font-bold' : ''}`}
                        >
                          {part.name}
                        </Button>
                      </React.Fragment>
                    ))}
                  </div>

                  {/* Right: Actions */}
                  <div className="flex items-center gap-2">
                    <Tabs
                      {...TOOL_TABS_PROPS}
                      value={layoutMode}
                      onValueChange={(value) => {
                        setLayoutMode(value);
                        localStorage.setItem('openListLayoutMode', value);
                      }}
                      tabs={[
                        { value: 'list', label: <Sliders className="w-4 h-4" /> },
                        { value: 'grid', label: <Grid className="w-4 h-4" /> },
                      ]}
                    />
                    <Button onClick={mkdirOpenList} className="h-8 text-xs flex items-center gap-1">
                      <Plus className="w-3.5 h-3.5" />
                      <span>新建文件夹</span>
                    </Button>
                  </div>
                </div>

                {/* Main list container */}
                <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden min-h-[300px]">
                  {loadingFiles ? (
                    <Table layout="fixed">
                      <colgroup>
                        {fileColWidths.map((w, idx) => (
                          <col key={idx} style={{ width: w }} />
                        ))}
                      </colgroup>
                      <Table.Header>
                        <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                          <Table.Head className="p-4">名称</Table.Head>
                          <Table.Head className="p-4">大小</Table.Head>
                          <Table.Head className="p-4">修改时间</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {[...Array(6)].map((_, idx) => (
                          <Table.Row key={idx} className="border-b border-kumo-line">
                            <Table.Cell className="p-4 flex items-center gap-2">
                              <SkeletonLine className="w-4 h-4 rounded" />
                              <SkeletonLine className="w-64 h-4" />
                            </Table.Cell>
                            <Table.Cell className="p-4"><SkeletonLine className="w-16 h-4" /></Table.Cell>
                            <Table.Cell className="p-4"><SkeletonLine className="w-32 h-4" /></Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </Table>
                  ) : sortedFiles.length === 0 ? (
                    <div className="p-16 text-center text-kumo-subtle text-xs">当前目录为空</div>
                  ) : layoutMode === 'list' ? (
                    <Table layout="fixed">
                      <colgroup>
                        {fileColWidths.map((w, idx) => (
                          <col key={idx} style={{ width: w }} />
                        ))}
                      </colgroup>
                      <Table.Header>
                        <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                          <Table.Head className="relative group pr-6 p-4 cursor-pointer select-none" onClick={() => toggleSort('name')}>
                            名称
                            {sortKey === 'name' && (sortOrder === 'asc' ? ' ▴' : ' ▾')}
                            <Table.ResizeHandle onMouseDown={(e) => startFileResize(0, e)} />
                          </Table.Head>
                          <Table.Head className="relative group pr-6 p-4 cursor-pointer select-none" onClick={() => toggleSort('size')}>
                            大小
                            {sortKey === 'size' && (sortOrder === 'asc' ? ' ▴' : ' ▾')}
                            <Table.ResizeHandle onMouseDown={(e) => startFileResize(1, e)} />
                          </Table.Head>
                          <Table.Head className="relative group pr-6 p-4 cursor-pointer select-none" onClick={() => toggleSort('modified')}>
                            修改时间
                            {sortKey === 'modified' && (sortOrder === 'asc' ? ' ▴' : ' ▾')}
                            <Table.ResizeHandle onMouseDown={(e) => startFileResize(2, e)} />
                          </Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {currentPath !== '/' && (
                          <Table.Row
                            onClick={() => {
                              const parts = currentPath.split('/').filter(p => p);
                              parts.pop();
                              loadFiles('/' + parts.join('/'));
                            }}
                            className="border-b border-kumo-line hover:bg-kumo-recessed/10 cursor-pointer font-bold text-kumo-brand"
                          >
                            <Table.Cell colSpan={3} className="p-4 flex items-center gap-2">
                              <Folder className="w-4 h-4 text-warning" />
                              <span>返回上一级 (..)</span>
                            </Table.Cell>
                          </Table.Row>
                        )}
                        {sortedFiles.map((file) => (
                          <Table.Row
                            key={file.name}
                            onClick={() => handleOpenFile(file)}
                            onContextMenu={(e) => onRowContextMenu(e, file)}
                            className="border-b border-kumo-line last:border-0 hover:bg-kumo-recessed/10 transition-colors cursor-pointer"
                          >
                            <Table.Cell className="p-4 flex items-center gap-2 min-w-0">
                              <div className="flex-shrink-0">{getFileIcon(file)}</div>
                              <span className="font-bold text-kumo-strong truncate" title={file.name}>
                                {file.name}
                              </span>
                            </Table.Cell>
                            <Table.Cell className="p-4 font-mono text-kumo-default tabular-nums">
                              {getOpenListFileSize(file)}
                            </Table.Cell>
                            <Table.Cell className="p-4 text-kumo-subtle font-mono text-[10px]">
                              {formatDateTime(file.modified)}
                            </Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </Table>
                  ) : (
                    // Grid Layout
                    <div className="p-6 grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-4">
                      {currentPath !== '/' && (
                        <div
                          onClick={() => {
                            const parts = currentPath.split('/').filter(p => p);
                            parts.pop();
                            loadFiles('/' + parts.join('/'));
                          }}
                          className="bg-kumo-recessed/20 border border-dashed border-kumo-line hover:border-kumo-brand rounded-lg p-4 flex flex-col items-center justify-center text-center cursor-pointer gap-2 transition-all"
                        >
                          <Folder className="w-8 h-8 text-warning" />
                          <span className="text-[11px] font-bold text-kumo-brand">返回上一级 (..)</span>
                        </div>
                      )}
                      {sortedFiles.map((file) => (
                        <div
                          key={file.name}
                          onClick={() => handleOpenFile(file)}
                          onContextMenu={(e) => onRowContextMenu(e, file)}
                          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-4 flex flex-col items-center text-center cursor-pointer gap-2 transition-all group min-w-0"
                        >
                          <div className="w-12 h-12 flex items-center justify-center bg-kumo-recessed rounded-md group-hover:bg-kumo-recessed/60">
                            {file.is_dir ? <Folder className="w-8 h-8 text-warning" /> : <FileText className="w-8 h-8 text-kumo-brand" />}
                          </div>
                          <span className="text-[11px] font-bold text-kumo-strong truncate w-full" title={file.name}>
                            {file.name}
                          </span>
                          <span className="text-[9px] text-kumo-subtle font-mono">
                            {file.is_dir ? '文件夹' : getOpenListFileSize(file)}
                          </span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                {/* README Markdown footer */}
                {readmeContent && readmeVisible && (
                  <div className="bg-kumo-base border border-kumo-line rounded-lg p-5 shadow-sm space-y-3">
                    <div className="flex justify-between items-center border-b border-kumo-line pb-2">
                      <span className="text-xs font-bold text-kumo-strong flex items-center gap-1.5">
                        <FileText className="w-4 h-4 text-kumo-brand" />
                        README.md
                      </span>
                      <Button
                        onClick={() => setReadmeVisible(false)}
                        variant="ghost"
                        size="sm"
                        shape="square"
                        aria-label="隐藏 README"
                        className="text-kumo-subtle hover:text-kumo-strong"
                      >
                        <X className="w-4 h-4" />
                      </Button>
                    </div>
                    <div
                      className="text-xs text-kumo-default leading-relaxed prose prose-invert max-w-none"
                      dangerouslySetInnerHTML={{ __html: renderMarkdown(readmeContent) }}
                    />
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        {/* ==================== 2. Instance Settings ==================== */}
        {subTab === 'settings' && (
          <div className="space-y-6">
            {/* Instance connections */}
            <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <Database className="w-4 h-4 text-kumo-brand" />
                实例管理
              </h3>
              
              <div className="space-y-3">
                {accounts.map((acc) => (
                  <div key={acc.id} className="border border-kumo-line rounded-lg p-3.5 flex justify-between items-center bg-kumo-recessed/10 hover:bg-kumo-recessed/25 transition-all gap-4">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className={`w-2.5 h-2.5 rounded-full ${acc.status === 'online' ? 'bg-kumo-success' : 'bg-kumo-subtle'}`} />
                        <span className="text-xs font-bold text-kumo-strong truncate">{acc.name}</span>
                      </div>
                      <p className="text-[10px] text-kumo-subtle truncate mt-1">{acc.api_url}</p>
                    </div>
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        disabled={currentAccount?.id === acc.id}
                        onClick={() => handleSelectAccount(acc)}
                        className={`h-7 px-2 ${currentAccount?.id === acc.id ? 'bg-kumo-success/15 text-kumo-success border border-kumo-success/20' : 'bg-kumo-base border border-kumo-line'}`}
                      >
                        <Check className="w-3.5 h-3.5" />
                      </Button>
                      <Button
                        size="sm"
                        disabled={testingAccId === acc.id}
                        onClick={() => handleTestAccount(acc.id)}
                        className="h-7 px-2 bg-kumo-base border border-kumo-line"
                      >
                        <Plug className={`w-3.5 h-3.5 ${testingAccId === acc.id ? 'animate-spin' : ''}`} />
                      </Button>
                      <Button
                        size="sm"
                        onClick={() => handleDeleteAccount(acc.id)}
                        className="h-7 px-2 bg-kumo-base border border-kumo-line text-kumo-danger hover:bg-kumo-danger/10"
                      >
                        <Trash className="w-3.5 h-3.5" />
                      </Button>
                    </div>
                  </div>
                ))}
                
                {accounts.length === 0 && (
                  <div className="text-center text-xs text-kumo-subtle py-4">暂无配置实例</div>
                )}
              </div>
            </div>

            {/* Add Instance Form */}
            <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <Plus className="w-4 h-4 text-kumo-brand" />
                添加新实例
              </h3>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <Input
                  label="名称"
                  type="text"
                  size="sm"
                  value={newAccForm.name}
                  onChange={(e) => setNewAccForm({ ...newAccForm, name: e.target.value })}
                  placeholder="生产环境 AList"
                  className="w-full"
                />
                <Input
                  label="API 接口地址"
                  type="text"
                  size="sm"
                  value={newAccForm.api_url}
                  onChange={(e) => setNewAccForm({ ...newAccForm, api_url: e.target.value })}
                  placeholder="https://alist.example.com"
                  className="w-full font-mono"
                />
                <Input
                  label="认证令牌"
                  type="password"
                  size="sm"
                  value={newAccForm.api_token}
                  onChange={(e) => setNewAccForm({ ...newAccForm, api_token: e.target.value })}
                  placeholder="alist-token-..."
                  className="w-full font-mono"
                />
              </div>
              <div className="flex justify-end">
                <Button variant="primary" onClick={handleAddAccount} className="text-xs h-8">
                  保存并连接
                </Button>
              </div>
            </div>

            {/* Preferences */}
            <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <Sliders className="w-4 h-4 text-kumo-brand" />
                偏好设置
              </h3>
              <div className="flex justify-between items-center border-b border-kumo-line pb-3">
                <div>
                  <span className="text-xs font-bold text-kumo-strong block">自动刷新目录</span>
                  <span className="text-[10px] text-kumo-subtle">在进入或退出目录时强制同步云端状态</span>
                </div>
                <Switch checked={true} disabled={true} onCheckedChange={() => {}} />
              </div>
              <div className="flex flex-col md:flex-row justify-between items-start md:items-center gap-4 py-1">
                <div>
                  <span className="text-xs font-bold text-kumo-strong block">图片预览尺寸: {previewSize}px</span>
                  <span className="text-[10px] text-kumo-subtle">在图片悬停预览时的最大边长像素限制</span>
                </div>
                <div className="flex items-center gap-3 w-full md:w-auto">
                  <Input
                    type="range"
                    aria-label="图片预览尺寸"
                    min="300"
                    max="1200"
                    step="50"
                    value={previewSize}
                    onChange={(e) => setPreviewSize(parseInt(e.target.value))}
                    className="w-40 accent-kumo-brand"
                  />
                  <Button onClick={handleSavePreferences} className="h-7 text-[10px] px-2.5">
                    保存偏好
                  </Button>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* ==================== 3. Cron Scheduler ==================== */}
        {subTab === 'cron' && (
          <div className="space-y-6">
            {/* Task list card */}
            <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4">
              <div className="flex justify-between items-center border-b border-kumo-line pb-3">
                <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                  <Clock className="w-4 h-4 text-kumo-brand" />
                  定时任务列表
                </h3>
                <Button variant="primary" onClick={() => openCronEditModal()} className="h-8 text-xs flex items-center gap-1">
                  <Plus className="w-3.5 h-3.5" />
                  <span>添加任务</span>
                </Button>
              </div>

              {loadingCron ? (
                <div className="space-y-3">
                  <SkeletonLine className="w-full h-10" />
                  <SkeletonLine className="w-full h-10" />
                </div>
              ) : cronTasks.length === 0 ? (
                <div className="p-10 text-center text-xs text-kumo-subtle">暂无定时调度任务</div>
              ) : (
                <div className="overflow-x-auto">
                  <Table layout="fixed">
                    <colgroup>
                      {cronColWidths.map((w, idx) => (
                        <col key={idx} style={{ width: w }} />
                      ))}
                    </colgroup>
                    <Table.Header>
                      <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                        <Table.Head className="relative group pr-6 p-4">
                          名称
                          <Table.ResizeHandle onMouseDown={(e) => startCronResize(0, e)} />
                        </Table.Head>
                        <Table.Head className="relative group pr-6 p-4">
                          周期
                          <Table.ResizeHandle onMouseDown={(e) => startCronResize(1, e)} />
                        </Table.Head>
                        <Table.Head className="relative group pr-6 p-4">
                          类型
                          <Table.ResizeHandle onMouseDown={(e) => startCronResize(2, e)} />
                        </Table.Head>
                        <Table.Head className="relative group pr-6 p-4">
                          命令/URL
                          <Table.ResizeHandle onMouseDown={(e) => startCronResize(3, e)} />
                        </Table.Head>
                        <Table.Head className="p-4 text-center">操作</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {cronTasks.map((task) => (
                        <Table.Row
                          key={task.id}
                          className={`border-b border-kumo-line last:border-0 hover:bg-kumo-recessed/10 transition-colors ${!task.enabled ? 'opacity-50' : ''}`}
                        >
                          <Table.Cell className="p-4 font-bold text-kumo-strong flex items-center gap-2">
                            <span className={`w-2 h-2 rounded-full ${task.enabled ? 'bg-kumo-success' : 'bg-kumo-subtle'}`} />
                            <span>{task.name}</span>
                          </Table.Cell>
                          <Table.Cell className="p-4 font-mono text-kumo-default">{task.schedule}</Table.Cell>
                          <Table.Cell className="p-4 text-kumo-subtle font-bold text-[10px] uppercase">{task.type}</Table.Cell>
                          <Table.Cell className="p-4 font-mono text-[10px] truncate" title={task.command}>{task.command}</Table.Cell>
                          <Table.Cell className="p-4 text-center">
                            <div className="flex justify-center gap-1.5">
                              <Button
                                onClick={() => handleRunCronTask(task)}
                                variant="secondary"
                                size="sm"
                                shape="square"
                                aria-label="立即执行定时任务"
                                className="text-kumo-success hover:bg-kumo-success/10"
                                title="立即执行"
                              >
                                <Play className="w-3.5 h-3.5" />
                              </Button>
                              <Button
                                onClick={() => handleToggleCronTask(task)}
                                variant="secondary"
                                size="sm"
                                shape="square"
                                aria-label={task.enabled ? '暂停定时任务' : '恢复定时任务'}
                                className="text-kumo-brand hover:bg-kumo-brand/10"
                                title={task.enabled ? '暂停' : '恢复'}
                              >
                                <Pause className="w-3.5 h-3.5" />
                              </Button>
                              <Button
                                onClick={() => openCronEditModal(task)}
                                variant="secondary"
                                size="sm"
                                shape="square"
                                aria-label="编辑定时任务"
                                className="text-kumo-subtle hover:text-kumo-strong"
                                title="编辑"
                              >
                                <Edit className="w-3.5 h-3.5" />
                              </Button>
                              <Button
                                onClick={() => handleDeleteCronTask(task)}
                                variant="secondary-destructive"
                                size="sm"
                                shape="square"
                                aria-label="删除定时任务"
                                className="hover:bg-kumo-danger/10"
                                title="删除"
                              >
                                <Trash className="w-3.5 h-3.5" />
                              </Button>
                            </div>
                          </Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </Table>
                </div>
              )}
            </div>

            {/* Run logs card */}
            <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4">
              <div className="flex justify-between items-center border-b border-kumo-line pb-3">
                <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                  <History className="w-4 h-4 text-kumo-brand" />
                  任务运行日志
                </h3>
                <div className="flex gap-2">
                  <Button onClick={loadCronLogs} className="h-8 text-xs flex items-center gap-1">
                    <RefreshCw className="w-3.5 h-3.5" />
                    <span>刷新日志</span>
                  </Button>
                  <Button onClick={handleClearCronLogs} className="h-8 text-xs bg-kumo-base border border-kumo-line text-kumo-danger hover:bg-kumo-danger/10 flex items-center gap-1">
                    <Trash className="w-3.5 h-3.5" />
                    <span>清理7天前</span>
                  </Button>
                </div>
              </div>

              <div className="space-y-2.5 max-h-[350px] overflow-y-auto pr-1">
                {cronLogs.map((log) => (
                  <div
                    key={log.id}
                    className={`border border-kumo-line rounded-lg p-3 text-xs bg-kumo-recessed/5 flex flex-col gap-2`}
                  >
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="flex items-center gap-2 font-medium">
                        <span className="text-kumo-subtle">{formatDateTime(log.start_time * 1000)}</span>
                        <span className="text-kumo-strong font-bold">{log.task_name || log.task_id}</span>
                      </div>
                      <div className="flex items-center gap-2">
                        {log.duration && <span className="text-[10px] text-kumo-subtle">{log.duration}s</span>}
                        <span className={`px-2 py-0.5 rounded border text-[10px] font-bold ${log.status === 'success' ? 'bg-kumo-success/10 border-kumo-success/20 text-kumo-success' : 'bg-kumo-danger/10 border-kumo-danger/20 text-kumo-danger'}`}>
                          {log.status === 'success' ? '成功' : '失败'}
                        </span>
                      </div>
                    </div>
                    {log.output && (
                      <details className="mt-1 font-mono text-[10px] bg-kumo-recessed p-2 rounded border border-kumo-line/50 cursor-pointer">
                        <summary className="text-kumo-subtle font-bold select-none outline-none">查看输出</summary>
                        <pre className="mt-2 text-kumo-default overflow-x-auto whitespace-pre-wrap">{log.output}</pre>
                      </details>
                    )}
                  </div>
                ))}
                
                {cronLogs.length === 0 && (
                  <div className="text-center text-xs text-kumo-subtle py-8">暂无运行日志</div>
                )}
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Right-click Context Menu */}
      {contextMenu.visible && (
        <div
          style={{ left: contextMenu.x, top: contextMenu.y }}
          className="fixed bg-kumo-base border border-kumo-line shadow-xl rounded-lg py-1.5 w-40 z-50 text-xs text-kumo-default font-semibold"
          onClick={(e) => e.stopPropagation()}
        >
          <Button
            onClick={() => {
              setContextMenu(prev => ({ ...prev, visible: false }));
              handleOpenFile(contextMenu.file);
            }}
            variant="ghost"
            size="sm"
            className="w-full justify-start px-4 py-2 text-left hover:bg-kumo-recessed"
          >
            <FolderOpen className="w-3.5 h-3.5 text-warning" />
            <span>{contextMenu.file?.is_dir ? '打开' : '查看详情'}</span>
          </Button>
          {!contextMenu.file?.is_dir && (
            <Button
              onClick={() => {
                setContextMenu(prev => ({ ...prev, visible: false }));
                downloadFile(contextMenu.file);
              }}
              variant="ghost"
              size="sm"
              className="w-full justify-start px-4 py-2 text-left hover:bg-kumo-recessed"
            >
              <Download className="w-3.5 h-3.5" />
              <span>下载</span>
            </Button>
          )}
          <div className="border-t border-kumo-line my-1" />
          <Button
            onClick={() => {
              setContextMenu(prev => ({ ...prev, visible: false }));
              renameFile(contextMenu.file);
            }}
            variant="ghost"
            size="sm"
            className="w-full justify-start px-4 py-2 text-left hover:bg-kumo-recessed"
          >
            <Edit className="w-3.5 h-3.5" />
            <span>重命名</span>
          </Button>
          <Button
            onClick={() => {
              setContextMenu(prev => ({ ...prev, visible: false }));
              deleteFile(contextMenu.file);
            }}
            variant="ghost"
            size="sm"
            className="w-full justify-start px-4 py-2 text-left text-kumo-danger hover:bg-kumo-danger/10"
          >
            <Trash className="w-3.5 h-3.5" />
            <span>删除</span>
          </Button>
        </div>
      )}

      {/* Image Preview Dialog */}
      <Dialog.Root open={imagePreview.visible} onOpenChange={(v) => setImagePreview(prev => ({ ...prev, visible: v }))}>
        <Dialog className="p-6 max-w-2xl bg-kumo-base border border-kumo-line rounded-lg shadow-xl flex flex-col gap-4">
          <div className="flex justify-between items-center">
            <span className="text-xs font-bold text-kumo-strong truncate max-w-md">{imagePreview.filename}</span>
            <div className="flex gap-2">
              <Button
                onClick={async () => {
                  try {
                    const response = await fetch(imagePreview.url);
                    const blob = await response.blob();
                    const blobUrl = window.URL.createObjectURL(blob);
                    const a = document.createElement('a');
                    a.href = blobUrl;
                    a.download = imagePreview.filename;
                    document.body.appendChild(a);
                    a.click();
                    document.body.removeChild(a);
                    window.URL.revokeObjectURL(blobUrl);
                  } catch (e) {
                    window.open(imagePreview.url, '_blank');
                  }
                }}
                className="h-8 text-xs flex items-center gap-1.5"
              >
                <Download className="w-3.5 h-3.5" />
                <span>下载</span>
              </Button>
              <Dialog.Close
                render={(props) => (
                  <Button
                    {...props}
                    variant="secondary"
                    shape="square"
                    className="h-8 border border-kumo-line bg-kumo-recessed text-xs flex items-center justify-center p-2"
                  >
                    <X className="w-4 h-4" />
                  </Button>
                )}
              />
            </div>
          </div>
          <div className="relative min-h-[300px] bg-kumo-recessed rounded border border-kumo-line flex items-center justify-center overflow-hidden">
            {imagePreview.loading && <RefreshCw className="w-8 h-8 animate-spin text-kumo-brand" />}
            {imagePreview.url && (
              <img
                src={imagePreview.url}
                alt={imagePreview.filename}
                className="max-h-[500px] object-contain w-full"
                onLoad={() => setImagePreview(prev => ({ ...prev, loading: false }))}
                onError={() => {
                  toast.error('图片加载失败');
                  setImagePreview(prev => ({ ...prev, loading: false }));
                }}
              />
            )}
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Details Alert Dialog */}
      <Dialog.Root open={detailModal.visible} onOpenChange={(v) => setDetailModal(prev => ({ ...prev, visible: v }))}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-xl space-y-4">
          <Dialog.Title className="text-sm font-bold text-kumo-strong flex items-center gap-1.5 border-b border-kumo-line pb-2.5">
            <Info className="w-4 h-4 text-kumo-brand" />
            {detailModal.title}
          </Dialog.Title>
          <div>{detailModal.content}</div>
          <div className="flex justify-end pt-2">
            <Dialog.Close
              render={(props) => (
                <Button {...props} variant="secondary" className="text-xs h-8">
                  关闭
                </Button>
              )}
            />
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Add/Edit Cron Task Dialog */}
      <Dialog.Root open={editingCronTask !== null} onOpenChange={(v) => { if (!v) setEditingCronTask(null); }}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-xl space-y-4">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            {editingCronTask?.id ? '编辑定时任务' : '添加定时任务'}
          </Dialog.Title>

          <div className="space-y-4">
            <Input
              label="任务名称"
              type="text"
              size="sm"
              value={cronForm.name}
              onChange={(e) => setCronForm({ ...cronForm, name: e.target.value })}
              placeholder="例如：每日数据库备份"
              className="w-full"
            />

            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Checkbox
                  checked={cronForm.useCustom}
                  onCheckedChange={(checked) => setCronForm({ ...cronForm, useCustom: !!checked })}
                  label="自定义 Cron 表达式"
                />
              </div>

              {cronForm.useCustom ? (
                <div className="space-y-1">
                  <Input
                    aria-label="Cron 表达式"
                    type="text"
                    size="sm"
                    value={cronForm.schedule}
                    onChange={(e) => setCronForm({ ...cronForm, schedule: e.target.value })}
                    placeholder="分 时 日 月 周，例如: 0 0 * * *"
                    className="w-full font-mono"
                  />
                  <p className="text-[9px] text-kumo-subtle">标准 Crontab 表达式支持：分、时、日、月、周</p>
                </div>
              ) : (
                <div className="bg-kumo-recessed/50 border border-kumo-line rounded p-3 space-y-3">
                  <div className="flex flex-wrap items-center gap-2 text-xs">
                    <span>频率:</span>
                    <Select
                      aria-label="频率"
                      size="sm"
                      value={cronForm.periodType}
                      onValueChange={(value) => setCronForm({ ...cronForm, periodType: value })}
                      items={[
                        { value: 'minute', label: '每分钟' },
                        { value: 'hour', label: '每小时' },
                        { value: 'day', label: '每天' },
                        { value: 'week', label: '每周' },
                        { value: 'month', label: '每月' },
                      ]}
                    />

                    {cronForm.periodType === 'week' && (
                      <Select
                        aria-label="周几"
                        size="sm"
                        value={cronForm.weekday}
                        onValueChange={(value) => setCronForm({ ...cronForm, weekday: value })}
                        items={[
                          { value: '1', label: '周一' },
                          { value: '2', label: '周二' },
                          { value: '3', label: '周三' },
                          { value: '4', label: '周四' },
                          { value: '5', label: '周五' },
                          { value: '6', label: '周六' },
                          { value: '0', label: '周日' },
                        ]}
                      />
                    )}

                    {cronForm.periodType === 'month' && (
                      <Select
                        aria-label="每月日期"
                        size="sm"
                        value={cronForm.dayOfMonth}
                        onValueChange={(value) => setCronForm({ ...cronForm, dayOfMonth: parseInt(value, 10) })}
                        items={[...Array(31)].map((_, i) => ({ value: i + 1, label: `${i + 1}日` }))}
                      />
                    )}

                    {['day', 'week', 'month'].includes(cronForm.periodType) && (
                      <>
                        <Input
                          aria-label="小时"
                          type="number"
                          size="sm"
                          min="0"
                          max="23"
                          value={cronForm.hour}
                          onChange={(e) => setCronForm({ ...cronForm, hour: parseInt(e.target.value, 10) })}
                          className="w-14 text-center"
                        />
                        <span>时</span>
                      </>
                    )}

                    {['hour', 'day', 'week', 'month'].includes(cronForm.periodType) && (
                      <>
                        <Input
                          aria-label="分钟"
                          type="number"
                          size="sm"
                          min="0"
                          max="59"
                          value={cronForm.minute}
                          onChange={(e) => setCronForm({ ...cronForm, minute: parseInt(e.target.value, 10) })}
                          className="w-14 text-center"
                        />
                        <span>分</span>
                      </>
                    )}
                  </div>
                  <div className="text-[10px] text-kumo-subtle font-mono">
                    表达式: <code className="text-kumo-brand">{getCronExpressionFromSimple(cronForm)}</code>
                  </div>
                </div>
              )}
            </div>

            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle block">任务类型</label>
              <Tabs
                {...TOOL_TABS_PROPS}
                value={cronForm.type}
                onValueChange={(value) => setCronForm({ ...cronForm, type: value })}
                tabs={[
                  { value: 'shell', label: 'Shell 命令行' },
                  { value: 'http', label: 'HTTP 请求 (GET)' },
                ]}
              />
            </div>

            <Textarea
              label={cronForm.type === 'http' ? 'HTTP 请求 URL 地址' : 'Shell 执行命令'}
              size="sm"
              value={cronForm.command}
              onChange={(e) => setCronForm({ ...cronForm, command: e.target.value })}
              placeholder={cronForm.type === 'http' ? 'https://example.com/api/backup-trigger' : 'bash /data/scripts/backup.sh'}
              className="w-full font-mono min-h-[70px]"
            />

            <div className="flex items-center gap-2 py-1">
              <Switch
                checked={cronForm.enabled === 1}
                onCheckedChange={(checked) => setCronForm({ ...cronForm, enabled: checked ? 1 : 0 })}
              />
              <span className="text-xs font-bold text-kumo-strong">立即启用此任务</span>
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <Dialog.Close
                render={(props) => (
                  <Button
                    {...props}
                    variant="secondary"
                    className="border border-kumo-line bg-kumo-recessed text-xs h-8"
                  >
                    取消
                  </Button>
                )}
              />
              <Button variant="primary" onClick={handleSaveCronTask} className="text-xs h-8">
                保存任务
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default SelfHPage;
