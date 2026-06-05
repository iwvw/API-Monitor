import React, { useState, useEffect, useRef, useMemo } from 'react';
import useStore from '../store.js';
import { toast } from '../modules/toast.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Tabs } from '@cloudflare/kumo/components/tabs';
import { formatUptime, formatFileSize, formatDateTime, maskAddress } from '../modules/utils.js';
import Chart from 'chart.js/auto';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { io } from 'socket.io-client';
import {
  Server,
  Terminal as TerminalIcon,
  Cloud,
  Globe,
  Activity,
  History,
  RefreshCw,
  ArrowRight,
  Box,
  Send,
  Shield,
  FolderOpen,
  HardDrive,
  TrendingUp,
  Settings,
  Plus,
  Trash,
  Play,
  Pause,
  Key,
  Folder,
  FileText,
  Save,
  RotateCw,
  Search,
  Upload,
  Download,
  Edit,
  X,
  Reboot,
  ChevronDown,
  ChevronUp,
  Copy
} from '../components/Icons.jsx';

// ==================== 自定义 SVG 小图标 ====================
const TrashIcon = (props) => (
  <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" {...props}>
    <path d="M3 6h18M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2M10 11v6M14 11v6"/>
  </svg>
);

const EditIcon = (props) => (
  <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" {...props}>
    <path d="M12 20h9M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/>
  </svg>
);

const PlayIcon = (props) => (
  <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" {...props}>
    <polygon points="6 3 20 12 6 21 6 3"/>
  </svg>
);

const PauseIcon = (props) => (
  <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" {...props}>
    <rect width="4" height="16" x="6" y="4" rx="1"/><rect width="4" height="16" x="14" y="4" rx="1"/>
  </svg>
);

const RestartIcon = (props) => (
  <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" {...props}>
    <path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/>
    <path d="M3 3v5h5M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16"/>
    <path d="M16 16h5v5"/>
  </svg>
);

const StarIcon = (props) => (
  <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" {...props}>
    <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>
  </svg>
);

// OS 平台图标及颜色计算
const getOSIconClass = (platform) => {
  if (!platform) return 'fas fa-server text-kumo-subtle';
  const p = platform.toLowerCase();
  if (p.includes('ubuntu')) return 'fab fa-ubuntu text-kumo-warning';
  if (p.includes('debian')) return 'fab fa-linux text-kumo-danger';
  if (p.includes('centos')) return 'fab fa-centos text-kumo-brand';
  if (p.includes('alpine')) return 'fas fa-mountain text-kumo-info';
  if (p.includes('windows')) return 'fab fa-windows text-kumo-info';
  if (p.includes('darwin') || p.includes('mac')) return 'fab fa-apple text-kumo-strong';
  if (p.includes('redhat') || p.includes('rhel')) return 'fab fa-redhat text-kumo-danger';
  return 'fab fa-linux text-kumo-subtle';
};

const getFlagCountry = (server) => {
  if (server.country && server.country !== 'auto') {
    return server.country;
  }
  return server.resolved_country || server.info?.resolved_country || '';
};

const getKumoToken = (tokenName, fallback) => {
  if (typeof window === 'undefined') return fallback;
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(tokenName).trim();
  return value || fallback;
};

const getKumoChartColors = () => ({
  brand: getKumoToken('--color-kumo-brand', 'CanvasText'),
  info: getKumoToken('--color-kumo-info', 'CanvasText'),
  success: getKumoToken('--color-kumo-success', 'CanvasText'),
  warning: getKumoToken('--color-kumo-warning', 'CanvasText'),
});

const getKumoTerminalTheme = () => ({
  background: getKumoToken('--color-kumo-recessed', 'Canvas'),
  foreground: getKumoToken('--text-color-kumo-strong', 'CanvasText'),
  cursor: getKumoToken('--color-kumo-brand', 'Highlight'),
});

// ==================== 主 React 组件 ====================
function ServerPage() {
  const { setMainActiveTab, theme } = useStore();
  
  // 核心标签页状态
  const [serverCurrentTab, setServerCurrentTab] = useState('list'); // 'list', 'history', 'docker', 'management', 'terminal'
  
  // 主机列表状态
  const [serverList, setServerList] = useState([]);
  const [serverLoading, setServerLoading] = useState(false);
  const [serverSearchText, setServerSearchText] = useState('');
  const [serverStatusFilter, setServerStatusFilter] = useState('all');
  const [expandedServers, setExpandedServers] = useState([]);
  
  // 凭据状态
  const [serverCredentials, setServerCredentials] = useState([]);
  
  // 各种模态框控制
  const [showServerModal, setShowServerModal] = useState(false);
  const [serverModalMode, setServerModalMode] = useState('add'); // 'add' | 'edit'
  const [serverForm, setServerForm] = useState({
    id: null,
    name: '',
    host: '',
    port: 22,
    username: '',
    authType: 'password',
    password: '',
    privateKey: '',
    passphrase: '',
    tagsInput: '',
    description: '',
    country: 'auto'
  });
  const [selectedCredentialId, setSelectedCredentialId] = useState('');
  const [serverModalSaving, setServerModalSaving] = useState(false);
  const [serverModalError, setServerModalError] = useState('');
  
  // 快速部署 (Agent) 模式
  const [serverAddMode, setServerAddMode] = useState('ssh'); // 'ssh' | 'agent'
  const [quickDeployName, setQuickDeployName] = useState('');
  const [quickDeployResult, setQuickDeployResult] = useState(null);
  const [agentInstallOS, setAgentInstallOS] = useState('linux');
  
  // 导入/导出
  const [showImportServerModal, setShowImportServerModal] = useState(false);
  const [importPreview, setImportPreview] = useState(null);
  const [importModalError, setImportModalError] = useState('');
  const [importModalSaving, setImportModalSaving] = useState(false);
  const [serverBatchText, setServerBatchText] = useState('');
  const [serverBatchError, setServerBatchError] = useState('');
  const [serverBatchSuccess, setServerBatchSuccess] = useState('');
  const [serverAddingBatch, setServerAddingBatch] = useState(false);
  const [serverIpDisplayMode, setServerIpDisplayMode] = useState('normal'); // 'normal', 'masked', 'hidden'
  
  // 历史趋势
  const [metricsHistoryTimeRange, setMetricsHistoryTimeRange] = useState('1h'); // '1h', '6h', '24h', '7d', 'all'
  const [metricsHistoryList, setMetricsHistoryList] = useState([]);
  const [metricsHistoryTotal, setMetricsHistoryTotal] = useState(0);
  const [metricsHistoryFilter, setMetricsHistoryFilter] = useState({ serverId: '' });
  const [metricsHistoryLoading, setMetricsHistoryLoading] = useState(false);
  const [metricsHistoryPagination, setMetricsHistoryPagination] = useState({ page: 1, pageSize: 30, totalPages: 1 });
  const [showMetricsCharts, setShowMetricsCharts] = useState(true);
  const [expandedMetricsServers, setExpandedMetricsServers] = useState([]);
  const [metricsCollectorStatus, setMetricsCollectorStatus] = useState(null);
  
  // 全局采集参数
  const [metricsCollectInterval, setMetricsCollectInterval] = useState(5);
  const [monitorConfig, setMonitorConfig] = useState({ metrics_retention_days: 30 });
  
  // Docker 状态
  const [dockerOverviewServers, setDockerOverviewServers] = useState([]);
  const [dockerOverviewLoading, setDockerOverviewLoading] = useState(false);
  const [dockerSubTab, setDockerSubTab] = useState('containers'); // 'containers', 'compose', 'images', 'networks', 'volumes', 'stats'
  const [dockerViewMode, setDockerViewMode] = useState('table'); // 'table', 'grid'
  const [dockerSearchQuery, setDockerSearchQuery] = useState('');
  const [dockerContainerStateFilter, setDockerContainerStateFilter] = useState('all');
  const [dockerSelectedServer, setDockerSelectedServer] = useState('');
  const [dockerTasks, setDockerTasks] = useState([]);
  const [dockerTaskStreamConnected, setDockerTaskStreamConnected] = useState(false);
  const [dockerTaskStreamError, setDockerTaskStreamError] = useState('');
  const [dockerResourceLoading, setDockerResourceLoading] = useState(false);
  const [dockerImages, setDockerImages] = useState([]);
  const [dockerNetworks, setDockerNetworks] = useState([]);
  const [dockerVolumes, setDockerVolumes] = useState([]);
  const [dockerStats, setDockerStats] = useState([]);
  const [dockerComposeProjects, setDockerComposeProjects] = useState([]);
  const [showDockerCreateModal, setShowDockerCreateModal] = useState(false);
  
  // Agent 升级
  const [showUpgradeModal, setShowUpgradeModal] = useState(false);
  const [upgrading, setUpgrading] = useState(false);
  const [upgradeLog, setUpgradeLog] = useState('');
  const [upgradeProgress, setUpgradeProgress] = useState(0);
  const [forceUpgrade, setForceUpgrade] = useState(false);
  const [upgradeFallbackSsh, setUpgradeFallbackSsh] = useState(true);
  
  // SSH 终端会话
  const [sshSessions, setSshSessions] = useState([]);
  const [activeSSHSessionId, setActiveSSHSessionId] = useState('');
  const [visibleSessionIds, setVisibleSessionIds] = useState([]);
  const [sshViewLayout, setSshViewLayout] = useState('single'); // 'single', 'split-h', 'split-v', 'grid-v', 'grid'
  const [sshSplitSide, setSshSplitSide] = useState('');
  const [sshGroupState, setSshGroupState] = useState(null);
  const [sshSyncEnabled, setSshSyncEnabled] = useState(false);
  
  // 终端侧边栏控制
  const [showSftpSidebar, setShowSftpSidebar] = useState(false);
  const [showSnippetsSidebar, setShowSnippetsSidebar] = useState(false);
  const [showServerStatusSidebar, setShowServerStatusSidebar] = useState(false);
  const [sshIdeFullscreen, setSshIdeFullscreen] = useState(false);
  
  // SFTP 状态
  const [sftpFiles, setSftpFiles] = useState([]);
  const [sftpCurrentPath, setSftpCurrentPath] = useState('/');
  const [sftpBreadcrumbs, setSftpBreadcrumbs] = useState([]);
  const [sftpLoading, setSftpLoading] = useState(false);
  const [sftpError, setSftpError] = useState('');
  const [sftpServerId, setSftpServerId] = useState('');
  const [sftpUploading, setSftpUploading] = useState(false);
  const [showSftpEditorModal, setShowSftpEditorModal] = useState(false);
  const [sftpEditFile, setSftpEditFile] = useState(null);
  const [sftpSaving, setSftpSaving] = useState(false);
  
  // 凭据编辑/新增
  const [showAddCredentialModal, setShowAddCredentialModal] = useState(false);
  const [credForm, setCredForm] = useState({
    name: '',
    username: 'root',
    auth_type: 'password',
    password: '',
    private_key: '',
    passphrase: ''
  });
  
  // 终端持久化实例仓库与 WebSocket 连接引用
  const terminalDOMElements = useRef({});
  const sshSessionRefs = useRef({});
  const warehouseRef = useRef(null);
  const dockerTaskStreamRef = useRef(null);
  const socketRef = useRef(null);
  const visibleSessionIdsRef = useRef([]);
  const sshSyncEnabledRef = useRef(false);

  useEffect(() => {
    visibleSessionIdsRef.current = visibleSessionIds;
  }, [visibleSessionIds]);

  useEffect(() => {
    sshSyncEnabledRef.current = sshSyncEnabled;
  }, [sshSyncEnabled]);
  
  // -------------------- 核心周期初始化 --------------------
  
  useEffect(() => {
    loadServerList();
    loadCredentials();
    connectMetricsStream();
    
    return () => {
      // 清理 WebSocket 与 SSE
      if (socketRef.current) {
        socketRef.current.disconnect();
      }
      if (dockerTaskStreamRef.current) {
        dockerTaskStreamRef.current.close();
      }
      Object.values(Chart.instances || {}).forEach(chart => {
        if (chart.canvas?.id?.includes('chart')) {
          chart.destroy();
        }
      });
      // 销毁所有终端实例
      Object.keys(sshSessionRefs.current).forEach(id => {
        const session = sshSessionRefs.current[id];
        if (session.heartbeatInterval) clearInterval(session.heartbeatInterval);
        if (session.inputDisposable) session.inputDisposable.dispose();
        if (session.ws) session.ws.close();
        if (session.resizeObserver) session.resizeObserver.disconnect();
        if (session.terminal) session.terminal.dispose();
      });
    };
  }, []);
  
  // 同步主 store 中 serverList 状态，便于 dashboard 使用
  useEffect(() => {
    useStore.setState({ serverList });
  }, [serverList]);

  const syncStoreServerList = () => {};
  
  // 载入主机列表
  const loadServerList = async () => {
    setServerLoading(true);
    try {
      const response = await fetch('/api/server/accounts');
      const data = await response.json();
      if (data.success) {
        setServerList(prev => {
          const prevMap = new Map(prev.map(s => [s.id, s]));
          const updated = data.data.map(server => {
            const existing = prevMap.get(server.id);
            return {
              ...server,
              info: existing?.info || server.info || null,
              metricsCache: existing?.metricsCache || null,
              gpuChartVisible: existing?.gpuChartVisible || false,
              gpuLoading: existing?.gpuLoading || false,
              netChartVisible: existing?.netChartVisible || false,
              netLoading: existing?.netLoading || false,
              error: existing?.error || null,
              loading: existing?.loading || false
            };
          });
          syncStoreServerList(updated);
          return updated;
        });
      }
    } catch (error) {
      console.error('加载主机列表失败:', error);
      toast.error('加载主机列表失败');
    } finally {
      setServerLoading(false);
    }
  };
  
  // 载入预设凭据
  const loadCredentials = async () => {
    try {
      const response = await fetch('/api/server/credentials');
      const data = await response.json();
      if (data.success) {
        setServerCredentials(data.data || []);
      }
    } catch (e) {
      console.error('加载凭据失败:', e);
    }
  };
  
  // Socket.IO 推送实时指标
  const connectMetricsStream = () => {
    try {
      const socket = io('/metrics', {
        reconnection: true,
        reconnectionDelay: 1000,
        reconnectionDelayMax: 5000,
        reconnectionAttempts: Infinity,
        transports: ['websocket', 'polling']
      });
      
      socket.on('connect', () => {
        console.log('✅ Socket.IO 实时流已连接');
      });
      
      socket.on('metrics:update', data => {
        if (data && data.serverId && data.metrics) {
          handleSingleMetricUpdate(data);
        }
      });
      
      socket.on('metrics:batch', dataArray => {
        if (Array.isArray(dataArray)) {
          dataArray.forEach(data => handleSingleMetricUpdate(data));
        }
      });
      
      socket.on('server:status', data => {
        if (data && data.serverId) {
          setServerList(prev => {
            const updated = prev.map(s => {
              if (s.id === data.serverId) {
                return {
                  ...s,
                  status: data.status,
                  response_time: data.responseTime || s.response_time,
                  error: data.status === 'offline' ? (data.error || 'Agent 离线') : null
                };
              }
              return s;
            });
            syncStoreServerList(updated);
            return updated;
          });
        }
      });
      
      socketRef.current = socket;
    } catch (err) {
      console.error('[Metrics] Socket.IO 初始化失败:', err);
    }
  };
  
  // 处理实时推送的主机指标
  const handleSingleMetricUpdate = (data) => {
    const { serverId, metrics, timestamp } = data;
    setServerList(prev => {
      const updated = prev.map(server => {
        if (server.id !== serverId) return server;
        
        // 防抖限制刷新间隔 >500ms
        const now = timestamp || Date.now();
        const lastUpdate = server.lastMetricUpdateTime || 0;
        if (lastUpdate > 0 && (now - lastUpdate) < 500) {
          return server;
        }
        
        const info = server.info ? { ...server.info } : {
          cpu: { Load: '-', Usage: '0%', Cores: '-' },
          memory: { Used: '-', Total: '-', Usage: '0%' },
          disk: [{ device: '/', used: '-', total: '-', usage: '0%' }],
          network: { connections: 0, rx_speed: '0 B/s', tx_speed: '0 B/s', rx_total: '-', tx_total: '-' },
          docker: { installed: false, containers: [] }
        };
        
        // CPU
        info.cpu = {
          Load: metrics.load || '-',
          Usage: metrics.cpu_usage || '0%',
          Cores: parseInt(metrics.cores) || info.cpu.Cores || '-',
          Temp: metrics.cpu_temp !== undefined ? metrics.cpu_temp : (info.cpu?.Temp || 0)
        };
        
        // Memory
        if (metrics.mem_usage) {
          const memMatch = metrics.mem_usage.match(/(\d+)\/(\d+)MB/);
          if (memMatch) {
            const used = parseInt(memMatch[1]);
            const total = parseInt(memMatch[2]);
            info.memory = {
              Used: used + ' MB',
              Total: total + ' MB',
              Usage: Math.round((used / total) * 100) + '%'
            };
          }
        }
        
        // Disk
        if (metrics.disk_usage) {
          const diskMatch = metrics.disk_usage.match(/(.+?)\/(.+?)\s*\((\d+%?)\)/);
          if (diskMatch) {
            if (!info.disk || !Array.isArray(info.disk)) info.disk = [{}];
            info.disk[0] = {
              device: '/',
              used: diskMatch[1].trim(),
              total: diskMatch[2].trim(),
              usage: diskMatch[3]
            };
          }
        }
        
        // Network
        if (metrics.network) {
          info.network = { ...info.network, ...metrics.network };
        }
        
        // GPU
        if (metrics.gpu !== undefined) {
          const existingGpu = info.gpu || {};
          if (typeof metrics.gpu === 'object' && metrics.gpu !== null) {
            info.gpu = {
              Model: metrics.gpu.Model || metrics.gpu_model || existingGpu.Model || '',
              Usage: metrics.gpu.Usage || metrics.gpu_usage || '0%',
              Memory: metrics.gpu.Memory || metrics.gpu_mem || '',
              Power: metrics.gpu.Power || metrics.gpu_power || '',
              Temp: metrics.gpu.Temp !== undefined ? metrics.gpu.Temp : (metrics.gpu_temp !== undefined ? metrics.gpu_temp : (existingGpu.Temp || 0)),
              Percent: metrics.gpu.Percent !== undefined ? metrics.gpu.Percent : (metrics.gpu_mem_percent !== undefined ? metrics.gpu_mem_percent : (existingGpu.Percent || 0))
            };
          }
        }
        
        // Docker
        if (metrics.docker) {
          info.docker = {
            installed: !!metrics.docker.installed,
            runningCount: metrics.docker.running || 0,
            stoppedCount: metrics.docker.stopped || 0,
            containers: Array.isArray(metrics.docker.containers) ? metrics.docker.containers : []
          };
        }
        
        info.platform = metrics.platform || info.platform;
        info.platformVersion = metrics.platformVersion || info.platformVersion;
        info.uptime = metrics.uptime || info.uptime;
        
        // 增量追加指标趋势缓存
        let cache = server.metricsCache ? [...server.metricsCache] : [];
        const parseSpeedToBytes = (speedStr) => {
          if (!speedStr) return 0;
          const match = speedStr.trim().match(/^([0-9.]+)\s*([A-Za-z/]+)$/);
          if (!match) return 0;
          const val = parseFloat(match[1]);
          const unit = match[2].toLowerCase();
          if (unit.startsWith('g')) return val * 1024 * 1024 * 1024;
          if (unit.startsWith('m')) return val * 1024 * 1024;
          if (unit.startsWith('k')) return val * 1024;
          return val;
        };
        
        const newRecord = {
          recorded_at: now,
          cpu_usage: parseFloat(metrics.cpu_usage || '0'),
          mem_usage: info.memory ? parseFloat(info.memory.Usage) : 0,
          gpu_usage: info.gpu ? parseFloat(info.gpu.Usage) : null,
          gpu_mem_used: info.gpu ? parseFloat(info.gpu.Percent) : 0,
          gpu_mem_total: 100,
          gpu_power: info.gpu ? parseFloat(info.gpu.Power) : 0,
          net_rx: parseSpeedToBytes(metrics.network?.rx_speed),
          net_tx: parseSpeedToBytes(metrics.network?.tx_speed)
        };
        
        cache.push(newRecord);
        if (cache.length > 60) cache.shift();
        
        // 触发本地 Canvas 图表刷新
        setTimeout(() => updateActiveCharts(serverId, cache, info.gpu?.Model, info.network !== undefined), 50);
        
        return {
          ...server,
          info,
          status: 'online',
          error: null,
          metricsCache: cache,
          lastMetricUpdateTime: now
        };
      });
      syncStoreServerList(updated);
      return updated;
    });
  };
  
  // 更新具体的 Chart.js 实例
  const updateActiveCharts = (serverId, cache, hasGpu, hasNet) => {
    const mainCanvas = document.getElementById(`metrics-chart-card-${serverId}`);
    if (mainCanvas) {
      renderSingleChartInstance(serverId, cache, mainCanvas);
    }
    const gpuCanvas = document.getElementById(`gpu-chart-${serverId}`);
    if (gpuCanvas && hasGpu) {
      renderGpuChartInstance(serverId, cache, gpuCanvas);
    }
    const netCanvas = document.getElementById(`net-chart-${serverId}`);
    if (netCanvas && hasNet) {
      renderNetChartInstance(serverId, cache, netCanvas);
    }
  };
  
  // 渲染单个主图表 (CPU & Mem)
  const renderSingleChartInstance = (serverId, cache, canvas) => {
    const cpuData = cache.map(r => r.cpu_usage || 0);
    const memData = cache.map(r => r.mem_usage || 0);
    const labels = cache.map((_, i) => `-${(cache.length - 1 - i) * 2}s`);
    const chartColors = getKumoChartColors();
    
    let chart = Chart.getChart(canvas);
    if (chart) {
      chart.data.labels = labels;
      chart.data.datasets[0].data = cpuData;
      chart.data.datasets[1].data = memData;
      chart.update('none');
    } else {
      new Chart(canvas, {
        type: 'line',
        data: {
          labels,
          datasets: [
            {
              label: 'CPU (%)',
              data: cpuData,
              borderColor: chartColors.success,
              backgroundColor: 'transparent',
              borderWidth: 1.5,
              tension: 0.3,
              pointRadius: 0
            },
            {
              label: 'Memory (%)',
              data: memData,
              borderColor: chartColors.brand,
              backgroundColor: 'transparent',
              borderWidth: 1.5,
              tension: 0.3,
              pointRadius: 0
            }
          ]
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          plugins: { legend: { display: false } },
          scales: {
            x: { grid: { display: false } },
            y: { min: 0, max: 100, ticks: { stepSize: 25 } }
          }
        }
      });
    }
  };
  
  // 渲染 GPU 趋势图
  const renderGpuChartInstance = (serverId, cache, canvas) => {
    const gpuData = cache.map(r => r.gpu_usage || 0);
    const vramData = cache.map(r => r.gpu_mem_used || 0);
    const labels = cache.map((_, i) => `-${(cache.length - 1 - i) * 2}s`);
    const chartColors = getKumoChartColors();
    
    let chart = Chart.getChart(canvas);
    if (chart) {
      chart.data.labels = labels;
      chart.data.datasets[0].data = gpuData;
      chart.data.datasets[1].data = vramData;
      chart.update('none');
    } else {
      new Chart(canvas, {
        type: 'line',
        data: {
          labels,
          datasets: [
            {
              label: 'GPU Usage (%)',
              data: gpuData,
              borderColor: chartColors.warning,
              backgroundColor: 'transparent',
              borderWidth: 1.5,
              tension: 0.3,
              pointRadius: 0
            },
            {
              label: 'VRAM (%)',
              data: vramData,
              borderColor: chartColors.success,
              backgroundColor: 'transparent',
              borderWidth: 1.5,
              tension: 0.3,
              pointRadius: 0
            }
          ]
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          plugins: { legend: { display: false } },
          scales: {
            x: { grid: { display: false } },
            y: { min: 0, max: 100 }
          }
        }
      });
    }
  };
  
  // 渲染网络趋势图
  const renderNetChartInstance = (serverId, cache, canvas) => {
    const rxData = cache.map(r => r.net_rx || 0);
    const txData = cache.map(r => r.net_tx || 0);
    const labels = cache.map((_, i) => `-${(cache.length - 1 - i) * 2}s`);
    const chartColors = getKumoChartColors();
    
    const formatBytesSpeed = (bytes) => {
      if (bytes >= 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB/s';
      if (bytes >= 1024) return (bytes / 1024).toFixed(1) + ' KB/s';
      return bytes + ' B/s';
    };
    
    let chart = Chart.getChart(canvas);
    if (chart) {
      chart.data.labels = labels;
      chart.data.datasets[0].data = txData;
      chart.data.datasets[1].data = rxData;
      chart.update('none');
    } else {
      new Chart(canvas, {
        type: 'line',
        data: {
          labels,
          datasets: [
            {
              label: 'Upload',
              data: txData,
              borderColor: chartColors.brand,
              backgroundColor: 'transparent',
              borderWidth: 1.5,
              tension: 0.3,
              pointRadius: 0
            },
            {
              label: 'Download',
              data: rxData,
              borderColor: chartColors.success,
              backgroundColor: 'transparent',
              borderWidth: 1.5,
              tension: 0.3,
              pointRadius: 0
            }
          ]
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          plugins: { legend: { display: false } },
          scales: {
            x: { grid: { display: false } },
            y: {
              ticks: {
                callback: (val) => formatBytesSpeed(val)
              }
            }
          }
        }
      });
    }
  };
  
  // 切换折叠卡片并加载历史数据
  const toggleServerExpand = async (serverId) => {
    const server = serverList.find(s => s.id === serverId);
    if (!server) return;
    
    if (server.status !== 'online') {
      toast.warning('主机未在线，无法查看监控指标');
      return;
    }
    
    if (expandedServers.includes(serverId)) {
      setExpandedServers(prev => prev.filter(id => id !== serverId));
    } else {
      setExpandedServers(prev => [...prev, serverId]);
      
      // 如果没有指标趋势缓存，立即载入历史
      if (!server.metricsCache) {
        setServerList(prev => prev.map(s => s.id === serverId ? { ...s, loading: true } : s));
        try {
          const params = new URLSearchParams({
            serverId,
            page: 1,
            pageSize: 60,
            highPrecision: 'true'
          });
          const response = await fetch(`/api/server/metrics/history?${params}`);
          const data = await response.json();
          if (data.success && data.data) {
            setServerList(prev => prev.map(s => {
              if (s.id === serverId) {
                const sorted = [...data.data].sort((a, b) => new Date(a.recorded_at) - new Date(b.recorded_at));
                return { ...s, metricsCache: sorted, loading: false };
              }
              return s;
            }));
          }
        } catch (e) {
          console.error(e);
          setServerList(prev => prev.map(s => s.id === serverId ? { ...s, loading: false } : s));
        }
      }
    }
  };
  
  // -------------------- 主机增改删操作 --------------------
  
  const openAddServerModal = () => {
    setServerAddMode('ssh');
    setQuickDeployName('');
    setQuickDeployResult(null);
    setAgentInstallOS('linux');
    setServerForm({
      id: null,
      name: '',
      host: '',
      port: 22,
      username: 'root',
      authType: 'password',
      password: '',
      privateKey: '',
      passphrase: '',
      tagsInput: '',
      description: '',
      country: 'auto'
    });
    setServerModalMode('add');
    setServerModalError('');
    setShowServerModal(true);
  };
  
  const openEditServerModal = (server) => {
    setServerForm({
      id: server.id,
      name: server.name,
      host: server.host || '',
      port: server.port || 22,
      username: server.username || 'root',
      authType: server.auth_type === 'key' ? 'privateKey' : 'password',
      password: '', // 安全考虑，密码置空
      privateKey: '',
      passphrase: '',
      tagsInput: Array.isArray(server.tags) ? server.tags.join(',') : '',
      description: server.description || '',
      country: server.country || 'auto'
    });
    setServerModalMode('edit');
    setServerModalError('');
    setShowServerModal(true);
  };
  
  const applyCredential = (credId) => {
    const cred = serverCredentials.find(c => c.id === credId);
    if (cred) {
      setServerForm(prev => ({
        ...prev,
        username: cred.username,
        authType: cred.auth_type === 'key' ? 'privateKey' : 'password',
        password: cred.password || '',
        privateKey: cred.private_key || '',
        passphrase: cred.passphrase || ''
      }));
      setSelectedCredentialId(credId);
    }
  };
  
  const testServerConnection = async () => {
    if (!serverForm.host || !serverForm.username) {
      setServerModalError('请填写连接地址和用户名');
      return;
    }
    setServerModalSaving(true);
    setServerModalError('');
    toast.info('正在测试连接中...');
    
    try {
      const response = await fetch('/api/server/test-connection', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          host: serverForm.host,
          port: serverForm.port,
          username: serverForm.username,
          auth_type: serverForm.authType === 'privateKey' ? 'key' : 'password',
          password: serverForm.password,
          private_key: serverForm.privateKey,
          passphrase: serverForm.passphrase
        })
      });
      const data = await response.json();
      if (data.success) {
        toast.success('🎉 连接测试成功！');
      } else {
        setServerModalError('测试连接失败: ' + data.message);
        toast.error('测试连接失败');
      }
    } catch (e) {
      setServerModalError('测试连接请求异常: ' + e.message);
    } finally {
      setServerModalSaving(false);
    }
  };
  
  const saveServer = async () => {
    if (!serverForm.name || !serverForm.host || !serverForm.username) {
      setServerModalError('请填写完整必填参数');
      return;
    }
    setServerModalSaving(true);
    setServerModalError('');
    
    try {
      const tags = serverForm.tagsInput ? serverForm.tagsInput.split(',').map(t => t.trim()).filter(Boolean) : [];
      const payload = {
        name: serverForm.name,
        host: serverForm.host,
        port: serverForm.port,
        username: serverForm.username,
        auth_type: serverForm.authType === 'privateKey' ? 'key' : 'password',
        tags,
        description: serverForm.description,
        country: serverForm.country
      };
      
      if (serverForm.authType === 'password' && serverForm.password) {
        payload.password = serverForm.password;
      }
      if (serverForm.authType === 'privateKey' && serverForm.privateKey) {
        payload.private_key = serverForm.privateKey;
        payload.passphrase = serverForm.passphrase;
      }
      
      const url = serverModalMode === 'add' ? '/api/server/accounts' : `/api/server/accounts/${serverForm.id}`;
      const method = serverModalMode === 'add' ? 'POST' : 'PUT';
      
      const response = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      const data = await response.json();
      if (data.success) {
        toast.success(serverModalMode === 'add' ? '主机添加成功' : '主机更新成功');
        setShowServerModal(false);
        loadServerList();
      } else {
        setServerModalError(data.error || '保存失败');
      }
    } catch (e) {
      setServerModalError('保存异常: ' + e.message);
    } finally {
      setServerModalSaving(false);
    }
  };

  const generateQuickInstallCommand = async () => {
    const name = quickDeployName.trim();
    if (!name) {
      setServerModalError('Server name is required');
      return;
    }

    setServerModalSaving(true);
    setServerModalError('');
    setQuickDeployResult(null);

    try {
      const response = await fetch('/api/server/agent/quick-install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name })
      });
      const data = await response.json();

      if (data.success) {
        setQuickDeployResult(data.data);
        toast.success(data.data?.isNew ? 'Agent host created' : 'Agent install command generated');
        loadServerList();
      } else {
        setServerModalError(data.error || 'Failed to generate Agent install command');
      }
    } catch (e) {
      setServerModalError('Agent quick install request failed: ' + e.message);
    } finally {
      setServerModalSaving(false);
    }
  };

  const copyQuickDeployCommand = async () => {
    const command = agentInstallOS === 'linux'
      ? quickDeployResult?.installCommand
      : quickDeployResult?.winInstallCommand;

    if (!command) return;

    try {
      await navigator.clipboard.writeText(command);
      toast.success('Install command copied');
    } catch (e) {
      setServerModalError('Copy failed: ' + e.message);
    }
  };
  
  const deleteServer = async (serverId) => {
    if (!confirm('确定要删除这台主机吗？此操作不可逆！')) return;
    try {
      const response = await fetch(`/api/server/accounts/${serverId}`, { method: 'DELETE' });
      const data = await response.json();
      if (data.success) {
        toast.success('主机删除成功');
        loadServerList();
      } else {
        toast.error('删除失败: ' + data.error);
      }
    } catch (e) {
      toast.error('删除请求失败');
    }
  };
  
  // 触发所有主机探测
  const probeAllServers = async () => {
    toast.info('正在向所有在线 Agent 下发网络探测请求...');
    try {
      const response = await fetch('/api/server/check-all', { method: 'POST' });
      const data = await response.json();
      if (data.success) {
        toast.success(data.message || '探测任务下发成功');
        loadServerList();
      }
    } catch (e) {
      toast.error('探测下发失败');
    }
  };
  
  // 双击就地重命名
  const startRenameServer = (server) => {
    const newName = prompt('输入新的服务器名称', server.name);
    if (newName && newName.trim() && newName !== server.name) {
      renameServer(server.id, newName.trim());
    }
  };
  
  const renameServer = async (serverId, name) => {
    try {
      const response = await fetch(`/api/server/accounts/${serverId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name })
      });
      const data = await response.json();
      if (data.success) {
        toast.success('重命名成功');
        loadServerList();
      }
    } catch (e) {
      toast.error('重命名请求异常');
    }
  };
  
  // -------------------- 批量导入导出 --------------------
  
  const exportServers = async () => {
    try {
      const response = await fetch('/api/server/accounts');
      const data = await response.json();
      if (data.success) {
        const clean = data.data.map(s => ({
          name: s.name,
          host: s.host,
          port: s.port,
          username: s.username,
          auth_type: s.auth_type,
          password: s.password,
          private_key: s.private_key,
          passphrase: s.passphrase,
          tags: s.tags,
          description: s.description,
          country: s.country
        }));
        const blob = new Blob([JSON.stringify(clean, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `servers_export_${new Date().toISOString().slice(0,10)}.json`;
        a.click();
        URL.revokeObjectURL(url);
        toast.success('主机配置导出成功');
      }
    } catch (e) {
      toast.error('导出主机失败');
    }
  };
  
  const processImportFile = (file) => {
    if (!file.name.endsWith('.json')) {
      setImportModalError('仅支持导入 .json 格式文件');
      return;
    }
    const reader = new FileReader();
    reader.onload = (e) => {
      try {
        const parsed = JSON.parse(e.target.result);
        if (!Array.isArray(parsed)) {
          setImportModalError('文件解析失败：结构必须是主机数组');
          return;
        }
        setImportPreview(parsed);
        setImportModalError('');
      } catch (err) {
        setImportModalError('JSON 文件解析失败: ' + err.message);
      }
    };
    reader.readAsText(file);
  };
  
  const confirmImportServers = async () => {
    if (!importPreview || importPreview.length === 0) return;
    setImportModalSaving(true);
    try {
      const response = await fetch('/api/server/accounts/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ servers: importPreview })
      });
      const data = await response.json();
      if (data.success) {
        toast.success(`成功导入 ${data.imported || 0} 台主机`);
        setShowImportServerModal(false);
        setImportPreview(null);
        loadServerList();
      } else {
        setImportModalError(data.error || '导入失败');
      }
    } catch (e) {
      setImportModalError('导入服务器发生网络异常');
    } finally {
      setImportModalSaving(false);
    }
  };
  
  const batchAddServers = async () => {
    if (!serverBatchText.trim()) return;
    setServerAddingBatch(true);
    setServerBatchError('');
    setServerBatchSuccess('');
    
    try {
      const lines = serverBatchText.split('\n');
      const servers = [];
      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) continue;
        const parts = trimmed.split(',');
        if (parts.length >= 2) {
          servers.push({
            name: parts[0].trim(),
            host: parts[1].trim(),
            port: parseInt(parts[2]) || 22,
            username: parts[3]?.trim() || 'root',
            auth_type: 'password',
            password: parts[4]?.trim() || ''
          });
        }
      }
      if (servers.length === 0) {
        setServerBatchError('未识别出任何有效主机行。格式: 名称,IP,端口,用户名,密码');
        setServerAddingBatch(false);
        return;
      }
      
      const response = await fetch('/api/server/accounts/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ servers })
      });
      const data = await response.json();
      if (data.success) {
        setServerBatchSuccess(`批量成功导入 ${servers.length} 台主机`);
        setServerBatchText('');
        loadServerList();
      } else {
        setServerBatchError(data.error || '批量导入失败');
      }
    } catch (e) {
      setServerBatchError('批量添加异常: ' + e.message);
    } finally {
      setServerAddingBatch(false);
    }
  };
  
  // -------------------- 凭据库管理 --------------------
  
  const addCredential = async () => {
    if (!credForm.name || !credForm.username) {
      toast.warning('凭据名称与用户名必填');
      return;
    }
    try {
      const response = await fetch('/api/server/credentials', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(credForm)
      });
      const data = await response.json();
      if (data.success) {
        toast.success('新建凭据成功');
        setShowAddCredentialModal(false);
        loadCredentials();
      }
    } catch (e) {
      toast.error('保存凭据异常');
    }
  };
  
  const deleteCredential = async (id) => {
    if (!confirm('确定要删除此凭据吗？')) return;
    try {
      const response = await fetch(`/api/server/credentials/${id}`, { method: 'DELETE' });
      const data = await response.json();
      if (data.success) {
        toast.success('删除成功');
        loadCredentials();
      }
    } catch (e) {
      toast.error('删除凭据请求异常');
    }
  };
  
  const setDefaultCredential = async (id) => {
    try {
      const response = await fetch(`/api/server/credentials/${id}/default`, { method: 'POST' });
      const data = await response.json();
      if (data.success) {
        toast.success('默认凭据设置成功');
        loadCredentials();
      }
    } catch (e) {
      toast.error('默认凭据设置失败');
    }
  };
  
  // -------------------- 采集与定时设置 --------------------
  
  const updateMetricsCollectInterval = async (val) => {
    setMetricsCollectInterval(val);
    try {
      await fetch('/api/server/monitor/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ interval: val })
      });
      toast.success('指标采集时间间隔更新成功');
    } catch (e) {
      console.error(e);
    }
  };
  
  const handleRetentionSliderChange = async (val) => {
    setMonitorConfig({ metrics_retention_days: val });
    try {
      await fetch('/api/server/monitor/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ metrics_retention_days: val })
      });
    } catch (e) {
      console.error(e);
    }
  };
  
  // -------------------- 历史数据记录子标签 --------------------
  
  useEffect(() => {
    if (serverCurrentTab === 'history') {
      loadMetricsHistory(1);
    }
  }, [serverCurrentTab, metricsHistoryTimeRange, metricsHistoryFilter.serverId]);
  
  const loadMetricsHistory = async (page = 1) => {
    setMetricsHistoryLoading(true);
    try {
      let startTime = null;
      const now = Date.now();
      if (metricsHistoryTimeRange === '1h') startTime = new Date(now - 60*60*1000).toISOString();
      else if (metricsHistoryTimeRange === '6h') startTime = new Date(now - 6*60*60*1000).toISOString();
      else if (metricsHistoryTimeRange === '24h') startTime = new Date(now - 24*60*60*1000).toISOString();
      else if (metricsHistoryTimeRange === '7d') startTime = new Date(now - 7*24*60*60*1000).toISOString();
      
      const params = new URLSearchParams({
        page,
        pageSize: 15,
        serverId: metricsHistoryFilter.serverId
      });
      if (startTime) params.append('startTime', startTime);
      
      const response = await fetch(`/api/server/metrics/history?${params}`);
      const data = await response.json();
      if (data.success) {
        setMetricsHistoryList(data.data || []);
        setMetricsHistoryTotal(data.pagination?.total || 0);
        setMetricsHistoryPagination({
          page: data.pagination?.page || 1,
          pageSize: data.pagination?.pageSize || 15,
          totalPages: data.pagination?.totalPages || 1
        });
      }
      
      // 读取采集器状态
      const statusRes = await fetch('/api/server/monitor/status');
      const statusData = await statusRes.json();
      if (statusData.success) {
        setMetricsCollectorStatus(statusData.status);
        if (statusData.status?.interval) {
          setMetricsCollectInterval(Math.floor(statusData.status.interval / 60000));
        }
      }
    } catch (e) {
      console.error(e);
    } finally {
      setMetricsHistoryLoading(false);
    }
  };
  
  const triggerManualCollect = async () => {
    toast.info('正在发送指令手动采集历史指标点...');
    try {
      const res = await fetch('/api/server/monitor/collect', { method: 'POST' });
      const data = await res.json();
      if (data.success) {
        toast.success('就绪：新指标点采集完成！');
        loadMetricsHistory(1);
      }
    } catch (e) {
      toast.error('采集失败');
    }
  };
  
  const clearMetricsHistory = async () => {
    if (!confirm('确定要清除数据库中存储的所有的历史监控记录吗？')) return;
    try {
      const res = await fetch('/api/server/metrics/history', { method: 'DELETE' });
      const data = await res.json();
      if (data.success) {
        toast.success('监控历史记录清空成功');
        loadMetricsHistory(1);
      }
    } catch (e) {
      toast.error('清空失败');
    }
  };
  
  // 按主机对历史数据进行分类展示
  const groupedMetricsHistory = useMemo(() => {
    const groups = {};
    metricsHistoryList.forEach(rec => {
      const serverId = rec.server_id || 'unknown';
      if (!groups[serverId]) groups[serverId] = [];
      groups[serverId].push(rec);
    });
    return groups;
  }, [metricsHistoryList]);
  
  // -------------------- Docker 监控与管理 --------------------
  
  useEffect(() => {
    if (serverCurrentTab === 'docker') {
      ensureDockerTaskStream();
      loadDockerResources();
    }
  }, [serverCurrentTab, dockerSubTab, dockerSelectedServer]);
  
  const ensureDockerTaskStream = () => {
    if (dockerTaskStreamRef.current) return;
    
    try {
      const stream = new EventSource('/api/server/v2/tasks/stream');
      dockerTaskStreamRef.current = stream;
      
      stream.addEventListener('ready', () => {
        setDockerTaskStreamConnected(true);
        setDockerTaskStreamError('');
      });
      
      stream.addEventListener('task.update', event => {
        try {
          const task = JSON.parse(event.data);
          if (task && task.domain === 'docker') {
            setDockerTasks(prev => {
              const updated = prev.filter(t => t.taskId !== task.taskId);
              updated.unshift(task);
              if (updated.length > 30) updated.pop();
              return updated;
            });
            // 任务成功完成时，如果正在当前标签页，触发静默刷新列表
            if (task.state === 'success') {
              toast.success(`任务 [${task.action}] 执行成功`);
              setTimeout(() => loadDockerResources(), 800);
            } else if (task.state === 'failed') {
              toast.error(`任务 [${task.action}] 失败: ${task.error || ''}`);
            }
          }
        } catch (e) {
          console.error(e);
        }
      });
      
      stream.onerror = () => {
        setDockerTaskStreamConnected(false);
        setDockerTaskStreamError('任务流重连中...');
      };
    } catch (e) {
      console.error(e);
    }
  };
  
  const submitDockerTask = async (action, payload = {}) => {
    const serverId = payload.serverId || dockerSelectedServer;
    if (!serverId) {
      toast.warning('请先选择一台主机');
      return;
    }
    try {
      const res = await fetch('/api/server/v2/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          serverId,
          domain: 'docker',
          action,
          payload
        })
      });
      const data = await res.json();
      if (data.success) {
        toast.info('Docker 管理任务提交成功，正在等待后台调度');
      } else {
        toast.error('提交失败: ' + data.error);
      }
    } catch (e) {
      toast.error('服务下发异常');
    }
  };
  
  const loadDockerResources = async () => {
    setDockerResourceLoading(true);
    try {
      // 获取概览数据
      const response = await fetch('/api/server/v2/docker/overview' + (dockerSelectedServer ? `?serverId=${dockerSelectedServer}` : ''));
      const data = await response.json();
      if (data.success) {
        const servers = (data.data?.servers || []).filter(s => s.docker?.installed);
        setDockerOverviewServers(servers);
        
        // 分发子模块资源
        if (dockerSubTab === 'images') {
          setDockerImages(servers.flatMap(s => (s.resources?.images || []).map(img => ({ ...img, serverName: s.name, serverId: s.id }))));
        } else if (dockerSubTab === 'networks') {
          setDockerNetworks(servers.flatMap(s => (s.resources?.networks || []).map(n => ({ ...n, serverName: s.name, serverId: s.id }))));
        } else if (dockerSubTab === 'volumes') {
          setDockerVolumes(servers.flatMap(s => (s.resources?.volumes || []).map(v => ({ ...v, serverName: s.name, serverId: s.id }))));
        } else if (dockerSubTab === 'stats') {
          setDockerStats(servers.flatMap(s => (s.resources?.stats || []).map(stat => ({ ...stat, serverName: s.name, serverId: s.id }))));
        } else if (dockerSubTab === 'compose') {
          setDockerComposeProjects(servers.flatMap(s => (s.resources?.compose?.projects || []).map(p => ({ ...p, serverName: s.name, serverId: s.id }))));
        }
      }
    } catch (e) {
      console.error(e);
    } finally {
      setDockerResourceLoading(false);
    }
  };
  
  // -------------------- SSH / Agent 嵌入式终端与多分屏 --------------------
  
  // 新连接 SSH 终端会话方法
  const openSSHTerminal = (server) => {
    if (!server) return;
    
    // 检查是否已存在该服务器的 SSH 会话
    const existing = sshSessions.find(s => s.server.id === server.id);
    if (existing) {
      switchToSSHTab(existing.id);
      return;
    }
    
    const sessionId = 'session_' + Date.now();
    let type = 'ssh';
    if (server.monitor_mode === 'agent' || (server.status === 'online' && !server.host)) {
      type = 'agent';
    }
    
    const newSession = {
      id: sessionId,
      server,
      type,
      connected: false,
      name: server.name
    };
    
    // 归还现有所有 xterm 节点到 warehouse 仓库
    saveTerminalsToWarehouse();
    
    setSshSessions(prev => [...prev, newSession]);
    
    if (sshViewLayout !== 'single') {
      // 如果处于分屏中，切回单屏（挂起当前组）
      setVisibleSessionIds([sessionId]);
      setSshViewLayout('single');
    } else {
      setVisibleSessionIds([sessionId]);
    }
    
    setActiveSSHSessionId(sessionId);
    setServerCurrentTab('terminal');
    
    // 延迟实例化 xterm.js
    setTimeout(() => initSessionTerminal(sessionId, newSession), 200);
  };
  
  // 切换标签
  const switchToSSHTab = (sessionId) => {
    saveTerminalsToWarehouse();
    
    setServerCurrentTab('terminal');
    setActiveSSHSessionId(sessionId);
    
    if (sshGroupState && sshGroupState.ids.includes(sessionId)) {
      // 如果属于原分屏组，恢复该分屏状态
      setVisibleSessionIds([...sshGroupState.ids]);
      setSshViewLayout(sshGroupState.layout);
      setSshSplitSide(sshGroupState.side);
    } else {
      setVisibleSessionIds([sessionId]);
      setSshViewLayout('single');
    }
    
    setTimeout(() => {
      syncTerminalDOM();
      const instance = sshSessionRefs.current[sessionId];
      if (instance?.terminal) instance.terminal.focus();
    }, 100);
  };
  
  // 归还 DOM 元素到仓库
  const saveTerminalsToWarehouse = () => {
    const warehouse = warehouseRef.current;
    if (!warehouse) return;
    
    sshSessions.forEach(session => {
      const el = terminalDOMElements.current[session.id];
      if (el && el.parentElement !== warehouse) {
        warehouse.appendChild(el);
      }
    });
  };

  const createSSHSocket = (sessionId, sessionMeta, terminal) => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws/ssh`);

    ws.onopen = () => {
      ws.send(JSON.stringify({
        type: 'connect',
        serverId: sessionMeta.server.id,
        protocol: sessionMeta.type,
        cols: terminal.cols,
        rows: terminal.rows
      }));

      const hb = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'ping' }));
        }
      }, 20000);

      const session = sshSessionRefs.current[sessionId];
      if (session) session.heartbeatInterval = hb;
    };

    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data);
        if (msg.type === 'connected') {
          setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: true } : s));
          terminal.clear();
          terminal.writeln(`\x1b[1;32m已成功连接到主服务器\x1b[0m`);
          setTimeout(() => sshSessionRefs.current[sessionId]?.fit?.fit(), 100);
        } else if (msg.type === 'output') {
          terminal.write(msg.data);
        } else if (msg.type === 'error') {
          terminal.writeln(`\n\x1b[1;31m错误: ${msg.message}\x1b[0m`);
        } else if (msg.type === 'disconnected') {
          setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));
          terminal.writeln(`\n\x1b[1;31m连接已从远端断开: ${msg.message || ''}\x1b[0m`);
        }
      } catch (err) {
        console.error(err);
      }
    };

    ws.onclose = () => {
      const session = sshSessionRefs.current[sessionId];
      if (session?.heartbeatInterval) {
        clearInterval(session.heartbeatInterval);
        session.heartbeatInterval = null;
      }
      setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));
    };

    ws.onerror = () => {
      terminal.writeln(`\n\x1b[1;31m终端连接发生网络错误\x1b[0m`);
    };

    return ws;
  };

  // 实例化 xterm.js 核心方法
  const initSessionTerminal = (sessionId, sessionMeta) => {
    if (sshSessionRefs.current[sessionId]) return;
    
    // 创建全局唯一的终端包装 div
    const container = document.createElement('div');
    container.id = 'ssh-terminal-' + sessionId;
    container.className = 'w-full h-full p-2 bg-kumo-recessed overflow-hidden';
    terminalDOMElements.current[sessionId] = container;
    if (!warehouseRef.current) return;
    warehouseRef.current.appendChild(container);
    const terminalTheme = getKumoTerminalTheme();
    
    const terminal = new Terminal({
      cursorBlink: true,
      cursorStyle: 'bar',
      fontSize: 14,
      fontFamily: 'Consolas, "Courier New", monospace',
      theme: terminalTheme,
      scrollback: 5000,
      allowProposedApi: true
    });
    
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(new WebLinksAddon());
    
    terminal.open(container);
    terminal.writeln(`\x1b[1;33m正在尝试建立与 ${sessionMeta.server.name} 的连接...\x1b[0m`);
    
    sshSessionRefs.current[sessionId] = {
      id: sessionId,
      terminal,
      fit: fitAddon,
      ws: null,
      connected: false,
      sessionMeta,
      inputDisposable: null,
      heartbeatInterval: null
    };

    const ws = createSSHSocket(sessionId, sessionMeta, terminal);
    sshSessionRefs.current[sessionId].ws = ws;

    const inputDisposable = terminal.onData(data => {
      const currentWs = sshSessionRefs.current[sessionId]?.ws;
      if (currentWs?.readyState === WebSocket.OPEN) {
        currentWs.send(JSON.stringify({ type: 'input', data }));
      }

      if (sshSyncEnabledRef.current && visibleSessionIdsRef.current.includes(sessionId)) {
        visibleSessionIdsRef.current.forEach(targetId => {
          if (targetId === sessionId) return;
          const targetSession = sshSessionRefs.current[targetId];
          if (targetSession?.ws && targetSession.ws.readyState === WebSocket.OPEN) {
            targetSession.ws.send(JSON.stringify({ type: 'input', data }));
          }
        });
      }
    });
    sshSessionRefs.current[sessionId].inputDisposable = inputDisposable;
    
    // 初始化时同步 DOM
    setTimeout(() => {
      syncTerminalDOM();
      fitAddon.fit();
    }, 100);
  };
  
  // 重新计算并挂载活动终端到 static slots 静态槽位上
  const syncTerminalDOM = () => {
    const slots = visibleSessionIds;
    slots.forEach((id, index) => {
      const slotDiv = document.getElementById(`ssh-slot-idx-${index}`);
      const termEl = terminalDOMElements.current[id];
      if (slotDiv && termEl && termEl.parentElement !== slotDiv) {
        slotDiv.appendChild(termEl);
        const inst = sshSessionRefs.current[id];
        if (inst && inst.fit) {
          setTimeout(() => {
            try { inst.fit.fit(); } catch(e){}
          }, 150);
        }
      }
    });
  };
  
  // 关闭终端会话
  const closeSSHSession = (sessionId) => {
    saveTerminalsToWarehouse();
    
    const session = sshSessionRefs.current[sessionId];
    if (session) {
      if (session.heartbeatInterval) clearInterval(session.heartbeatInterval);
      if (session.inputDisposable) session.inputDisposable.dispose();
      if (session.ws) session.ws.close();
      if (session.terminal) session.terminal.dispose();
      delete sshSessionRefs.current[sessionId];
    }
    
    const termEl = terminalDOMElements.current[sessionId];
    if (termEl) {
      termEl.remove();
      delete terminalDOMElements.current[sessionId];
    }
    
    const remaining = sshSessions.filter(s => s.id !== sessionId);
    setSshSessions(remaining);
    
    const remainsVisible = visibleSessionIds.filter(id => id !== sessionId);
    setVisibleSessionIds(remainsVisible);
    
    if (remaining.length === 0) {
      setActiveSSHSessionId('');
      setServerCurrentTab('list');
    } else if (activeSSHSessionId === sessionId) {
      const next = remaining[remaining.length - 1];
      switchToSSHTab(next.id);
    }
  };
  
  // 重新连接
  const reconnectSSHSession = (sessionId) => {
    const session = sshSessionRefs.current[sessionId];
    if (!session) return;

    if (session.heartbeatInterval) {
      clearInterval(session.heartbeatInterval);
      session.heartbeatInterval = null;
    }
    if (session.ws) {
      session.ws.onclose = null;
      session.ws.close();
    }

    session.terminal.clear();
    session.terminal.writeln(`\x1b[1;33m正在重新连接终端...\x1b[0m`);
    setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));

    const nextWs = createSSHSocket(sessionId, session.sessionMeta, session.terminal);
    session.ws = nextWs;
  };
  
  // -------------------- SFTP 文件管理系统 --------------------
  
  const loadSftpDirectory = async (serverId, path = '.') => {
    setSftpLoading(true);
    setSftpError('');
    try {
      const response = await fetch('/api/server/sftp/list', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverId, path })
      });
      const data = await response.json();
      if (data.success) {
        setSftpFiles(data.data || []);
        setSftpCurrentPath(data.path);
        setSftpServerId(serverId);
        
        // 构建路径导航
        const parts = data.path.split('/').filter(Boolean);
        const crumbs = [{ name: '/', path: '/' }];
        let cur = '';
        parts.forEach(p => {
          cur += '/' + p;
          crumbs.push({ name: p, path: cur });
        });
        setSftpBreadcrumbs(crumbs);
      } else {
        setSftpError(data.error || '加载 SFTP 目录失败');
      }
    } catch (e) {
      setSftpError('请求失败: ' + e.message);
    } finally {
      setSftpLoading(false);
    }
  };
  
  const handleSftpFileClick = (file) => {
    if (file.isDirectory) {
      loadSftpDirectory(sftpServerId, file.path);
    } else {
      openSftpFile(file);
    }
  };
  
  const openSftpFile = async (file) => {
    try {
      const res = await fetch('/api/server/sftp/read', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverId: sftpServerId, path: file.path })
      });
      const data = await res.json();
      if (data.success) {
        setSftpEditFile({
          path: file.path,
          name: file.name,
          content: data.data,
          originalContent: data.data
        });
        setShowSftpEditorModal(true);
      }
    } catch (e) {
      toast.error('读取文件失败');
    }
  };
  
  const saveSftpEditedFile = async () => {
    if (!sftpEditFile) return;
    setSftpSaving(true);
    try {
      const res = await fetch('/api/server/sftp/write', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          serverId: sftpServerId,
          path: sftpEditFile.path,
          content: sftpEditFile.content
        })
      });
      const data = await res.json();
      if (data.success) {
        toast.success('文件保存成功');
        setShowSftpEditorModal(false);
        loadSftpDirectory(sftpServerId, sftpCurrentPath);
      }
    } catch (e) {
      toast.error('保存文件异常');
    } finally {
      setSftpSaving(false);
    }
  };
  
  const handleSftpUpload = async (e) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    setSftpUploading(true);
    
    let ok = 0;
    for (let file of files) {
      const fd = new FormData();
      fd.append('serverId', sftpServerId);
      fd.append('path', sftpCurrentPath);
      fd.append('file', file);
      try {
        const res = await fetch('/api/server/sftp/upload', { method: 'POST', body: fd });
        const d = await res.json();
        if (d.success) ok++;
      } catch (err) {
        console.error(err);
      }
    }
    setSftpUploading(false);
    toast.success(`成功上传 ${ok} 个文件`);
    loadSftpDirectory(sftpServerId, sftpCurrentPath);
  };
  
  // -------------------- 拖拽放置分屏逻辑 --------------------
  const [draggedSessionId, setDraggedSessionId] = useState(null);
  const [dropHint, setDropHint] = useState('');
  const [dropTargetId, setDropTargetId] = useState(null);
  
  const handleTerminalDragStart = (id) => {
    setDraggedSessionId(id);
  };
  
  const handleTerminalDragOver = (e, targetId) => {
    e.preventDefault();
    setDropTargetId(targetId);
  };
  
  const triggerSplitPane = (targetId, position) => {
    if (!draggedSessionId || draggedSessionId === targetId) return;
    
    let updated = visibleSessionIds.filter(id => id !== draggedSessionId);
    const idx = updated.indexOf(targetId);
    if (position === 'center') {
      // 替换
      updated[idx] = draggedSessionId;
      setSshViewLayout('single');
    } else {
      // 分割
      if (position === 'left' || position === 'top') {
        updated.splice(idx, 0, draggedSessionId);
      } else {
        updated.splice(idx + 1, 0, draggedSessionId);
      }
      setSshViewLayout(updated.length > 2 ? 'grid' : (position === 'top' || position === 'bottom' ? 'split-v' : 'split-h'));
    }
    
    setVisibleSessionIds(updated);
    setDraggedSessionId(null);
    setDropTargetId(null);
    setDropHint('');
    
    // 延迟同步 DOM 尺寸
    setTimeout(() => syncTerminalDOM(), 100);
  };
  
  // -------------------- 计算过滤列表 --------------------
  
  const filteredServers = useMemo(() => {
    let list = serverList;
    if (serverStatusFilter !== 'all') {
      list = list.filter(s => s.status === serverStatusFilter);
    }
    if (serverSearchText.trim()) {
      const query = serverSearchText.toLowerCase();
      list = list.filter(s =>
        s.name.toLowerCase().includes(query) ||
        (s.host && s.host.toLowerCase().includes(query)) ||
        (s.tags && s.tags.some(t => t.toLowerCase().includes(query)))
      );
    }
    return list;
  }, [serverList, serverStatusFilter, serverSearchText]);
  
  // 统计数值
  const statsSummary = useMemo(() => {
    const total = serverList.length;
    const online = serverList.filter(s => s.status === 'online').length;
    const offline = total - online;
    return { total, online, offline };
  }, [serverList]);
  
  return (
    <div className="flex flex-col gap-6 w-full max-w-7xl mx-auto px-1">
      {/* 顶部标签导航 */}
      <div className="flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4">
        <div className="flex items-center gap-1.5 overflow-x-auto scrollbar-thin">
          <button
            onClick={() => setServerCurrentTab('list')}
            className={`flex items-center gap-2 px-3.5 py-1.5 rounded-lg text-xs font-semibold cursor-pointer transition-colors ${serverCurrentTab === 'list' ? 'bg-kumo-recessed text-kumo-strong' : 'text-kumo-subtle hover:text-kumo-strong hover:bg-kumo-recessed/60'}`}
          >
            <Server className="w-4 h-4" />
            主机实例管理
          </button>
          <button
            onClick={() => setServerCurrentTab('history')}
            className={`flex items-center gap-2 px-3.5 py-1.5 rounded-lg text-xs font-semibold cursor-pointer transition-colors ${serverCurrentTab === 'history' ? 'bg-kumo-recessed text-kumo-strong' : 'text-kumo-subtle hover:text-kumo-strong hover:bg-kumo-recessed/60'}`}
          >
            <History className="w-4 h-4" />
            历史趋势
          </button>
          <button
            onClick={() => setServerCurrentTab('docker')}
            className={`flex items-center gap-2 px-3.5 py-1.5 rounded-lg text-xs font-semibold cursor-pointer transition-colors ${serverCurrentTab === 'docker' ? 'bg-kumo-recessed text-kumo-strong' : 'text-kumo-subtle hover:text-kumo-strong hover:bg-kumo-recessed/60'}`}
          >
            <Box className="w-4 h-4" />
            Docker
          </button>
          <button
            onClick={() => setServerCurrentTab('management')}
            className={`flex items-center gap-2 px-3.5 py-1.5 rounded-lg text-xs font-semibold cursor-pointer transition-colors ${serverCurrentTab === 'management' ? 'bg-kumo-recessed text-kumo-strong' : 'text-kumo-subtle hover:text-kumo-strong hover:bg-kumo-recessed/60'}`}
          >
            <Settings className="w-4 h-4" />
            后台管理
          </button>
          {sshSessions.length > 0 && (
            <button
              onClick={() => setServerCurrentTab('terminal')}
              className={`flex items-center gap-2 px-3.5 py-1.5 rounded-lg text-xs font-semibold cursor-pointer transition-colors ${serverCurrentTab === 'terminal' ? 'bg-kumo-recessed text-kumo-strong' : 'text-kumo-subtle hover:text-kumo-strong hover:bg-kumo-recessed/60'}`}
            >
              <TerminalIcon className="w-4 h-4" />
              SSH 终端
              <span className="px-1.5 py-0.5 rounded bg-kumo-brand/10 text-kumo-brand text-[10px] font-bold">
                {sshSessions.length}
              </span>
            </button>
          )}
        </div>
        
        {/* 右侧快速连接 */}
        <div className="flex items-center gap-2">
          {serverCurrentTab === 'list' && (
            <>
              <button
                onClick={probeAllServers}
                className="flex items-center gap-1.5 px-3 py-1.5 text-xs border border-kumo-line rounded-lg bg-kumo-base text-kumo-default hover:bg-kumo-recessed/50 font-medium cursor-pointer"
              >
                <RotateCw className="w-3.5 h-3.5" />
                就地探测
              </button>
              <button
                onClick={openAddServerModal}
                className="flex items-center gap-1.5 px-3.5 py-1.5 text-xs bg-kumo-brand text-kumo-inverse hover:bg-kumo-brand-hover rounded-lg font-semibold cursor-pointer"
              >
                <Plus className="w-3.5 h-3.5" />
                新增主机实例
              </button>
            </>
          )}
        </div>
      </div>
      
      {/* ==================== 1. 主机实例管理 ==================== */}
      {serverCurrentTab === 'list' && (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <h2 className="text-base font-bold text-kumo-strong">主机实例管理</h2>
            <p className="text-xs text-kumo-subtle">
              管理主机实例的状态探测、SSH 连接、编辑与删除操作。
            </p>
          </div>

          {/* 控制过滤器栏 */}
          <div className="flex flex-col sm:flex-row gap-3 items-center justify-between bg-kumo-base border border-kumo-line p-3.5 rounded-md shadow-sm">
            <div className="flex items-center gap-1.5 bg-kumo-recessed/55 p-1 rounded-lg">
              <button
                onClick={() => setServerStatusFilter('all')}
                className={`px-3.5 py-1 rounded-md text-[11px] font-bold cursor-pointer transition-colors ${serverStatusFilter === 'all' ? 'bg-kumo-base text-kumo-strong shadow-xs' : 'text-kumo-subtle hover:text-kumo-strong'}`}
              >
                全部 ({statsSummary.total})
              </button>
              <button
                onClick={() => setServerStatusFilter('online')}
                className={`px-3 py-1 rounded-md text-[11px] font-bold cursor-pointer transition-colors ${serverStatusFilter === 'online' ? 'bg-kumo-base text-kumo-success shadow-xs' : 'text-kumo-subtle hover:text-kumo-success'}`}
              >
                在线 ({statsSummary.online})
              </button>
              <button
                onClick={() => setServerStatusFilter('offline')}
                className={`px-3 py-1 rounded-md text-[11px] font-bold cursor-pointer transition-colors ${serverStatusFilter === 'offline' ? 'bg-kumo-base text-kumo-danger shadow-xs' : 'text-kumo-subtle hover:text-kumo-danger'}`}
              >
                离线 ({statsSummary.offline})
              </button>
            </div>
            
            <div className="relative w-full sm:w-64">
              <Search className="absolute left-3 top-2.5 w-3.5 h-3.5 text-kumo-subtle" />
              <input
                type="text"
                placeholder="搜索主机名称、IP 或标签..."
                value={serverSearchText}
                onChange={e => setServerSearchText(e.target.value)}
                className="w-full pl-9 pr-4 py-2 border border-kumo-line rounded-lg bg-kumo-control text-xs text-kumo-strong focus:outline-none focus:border-kumo-brand"
              />
            </div>
          </div>
          
          {/* 列表渲染 */}
          {serverLoading && serverList.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-12 bg-kumo-base border border-kumo-line rounded-md text-kumo-subtle gap-2">
              <div className="w-6 h-6 border-2 border-kumo-brand border-t-transparent rounded-full animate-spin"></div>
              <p className="text-xs">正在连接并加载主机结构中...</p>
            </div>
          ) : filteredServers.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-16 bg-kumo-base border border-kumo-line rounded-md text-kumo-subtle gap-1.5">
              <span className="text-xl">🔍</span>
              <p className="text-xs">未找到符合当前条件的主机节点</p>
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              {filteredServers.map(server => {
                const country = getFlagCountry(server);
                const isExpanded = expandedServers.includes(server.id);
                
                return (
                  <div
                    key={server.id}
                    className={`bg-kumo-base border rounded-lg transition-all duration-200 ${isExpanded ? 'border-kumo-brand/60 shadow-md' : 'border-kumo-line hover:border-kumo-interact'}`}
                  >
                    {/* 卡片头部 */}
                    <div
                      onClick={() => toggleServerExpand(server.id)}
                      className="flex flex-wrap items-center justify-between p-3.5 cursor-pointer gap-4"
                    >
                      <div className="flex items-center gap-3.5 min-w-0">
                        {/* 呼吸状态灯 */}
                        <span className={`relative flex h-2 w-2 rounded-full`}>
                          <span className={`animate-ping absolute inline-flex h-full w-full rounded-full opacity-75 ${server.status === 'online' ? 'bg-kumo-success' : 'bg-kumo-danger'}`}></span>
                          <span className={`relative inline-flex rounded-full h-2 w-2 ${server.status === 'online' ? 'bg-kumo-success' : 'bg-kumo-danger'}`}></span>
                        </span>
                        
                        <div className="flex items-center gap-2">
                          <i className={getOSIconClass(server.info?.platform)}></i>
                          <span
                            onDoubleClick={e => {
                              e.stopPropagation();
                              startRenameServer(server);
                            }}
                            className="text-xs font-bold text-kumo-strong truncate hover:text-kumo-brand"
                          >
                            {country && (
                              <img
                                src={`https://flagcdn.com/w20/${country.toLowerCase()}.png`}
                                className="inline-block mr-1.5 w-4 h-3 rounded-xs object-cover"
                                alt={country}
                              />
                            )}
                            {server.name}
                          </span>
                          
                          {/* 标签 */}
                          {server.tags && server.tags.map(t => (
                            <span key={t} className="px-1.5 py-0.5 rounded text-[9px] font-bold bg-kumo-recessed/60 text-kumo-subtle">
                              {t}
                            </span>
                          ))}
                        </div>
                      </div>
                      
                      {/* 右侧指标与速率概要 */}
                      <div className="flex flex-wrap items-center gap-5 ml-auto">
                        {server.status === 'online' && server.info && (
                          <div className="flex items-center gap-4 text-[10px] font-semibold text-kumo-subtle">
                            {/* CPU 指示 */}
                            <div className="flex flex-col gap-1 w-14">
                              <div className="flex justify-between">
                                <span>CPU</span>
                                <span className="text-kumo-success font-bold">{parseInt(server.info.cpu?.Usage || '0')}%</span>
                              </div>
                              <div className="h-1 bg-kumo-recessed rounded-full overflow-hidden">
                                <div className="h-full bg-kumo-success" style={{ width: server.info.cpu?.Usage || '0%' }}></div>
                              </div>
                            </div>
                            {/* Memory */}
                            <div className="flex flex-col gap-1 w-14">
                              <div className="flex justify-between">
                                <span>Mem</span>
                                <span className="text-kumo-info font-bold">{parseInt(server.info.memory?.Usage || '0')}%</span>
                              </div>
                              <div className="h-1 bg-kumo-recessed rounded-full overflow-hidden">
                                <div className="h-full bg-kumo-info" style={{ width: server.info.memory?.Usage || '0%' }}></div>
                              </div>
                            </div>
                            {/* Net speed */}
                            {server.info.network && (
                              <div className="flex items-center gap-1.5 bg-kumo-recessed/35 px-2 py-1 border border-kumo-line rounded-md text-[9px] font-bold">
                                <span className="text-kumo-info">↑ {server.info.network.tx_speed || '0 B/s'}</span>
                                <span className="text-kumo-success">↓ {server.info.network.rx_speed || '0 B/s'}</span>
                              </div>
                            )}
                          </div>
                        )}
                        
                        <div className="flex items-center gap-2">
                          {server.status === 'online' && (
                            <button
                              onClick={e => {
                                e.stopPropagation();
                                openSSHTerminal(server);
                              }}
                              className="p-1.5 text-kumo-subtle hover:text-kumo-brand bg-kumo-recessed/50 hover:bg-kumo-brand/10 border border-kumo-line rounded cursor-pointer"
                              title="远程 SSH 连接"
                            >
                              <TerminalIcon className="w-3.5 h-3.5" />
                            </button>
                          )}
                          <button
                            onClick={e => {
                              e.stopPropagation();
                              openEditServerModal(server);
                            }}
                            className="p-1.5 text-kumo-subtle hover:text-kumo-strong bg-kumo-recessed/50 border border-kumo-line rounded cursor-pointer"
                          >
                            <EditIcon />
                          </button>
                        </div>
                      </div>
                    </div>
                    
                    {/* 卡片折叠面板 (详情走势图) */}
                    {isExpanded && (
                      <div className="border-t border-kumo-line p-4 bg-kumo-canvas/45 rounded-b-lg">
                        {server.loading ? (
                          <div className="py-8 text-center text-xs text-kumo-subtle">
                            正在读取最新的系统与指标快照...
                          </div>
                        ) : (
                          <div className="flex flex-col gap-4">
                            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                              {/* 硬件负载与翻转卡 */}
                              <div className="bg-kumo-base border border-kumo-line p-4 rounded-lg flex flex-col gap-2 shadow-xs">
                                <h4 className="text-xs font-bold text-kumo-strong border-b border-kumo-line pb-1.5 mb-1 flex items-center gap-1.5">
                                  <span>💻</span> 系统与载荷
                                </h4>
                                <div className="text-xs flex flex-col gap-2">
                                  <div className="flex justify-between">
                                    <span className="text-kumo-subtle font-medium">Uptime 运行时间</span>
                                    <span className="text-kumo-strong font-semibold">{formatUptime(server.info?.uptime)}</span>
                                  </div>
                                  <div className="flex justify-between">
                                    <span className="text-kumo-subtle font-medium">处理器核心</span>
                                    <span className="text-kumo-strong font-semibold">{server.info?.cpu?.Cores || '-'} 核</span>
                                  </div>
                                  <div className="flex justify-between">
                                    <span className="text-kumo-subtle font-medium">内核版本</span>
                                    <span className="text-kumo-strong font-semibold truncate max-w-[150px]">{server.info?.platformVersion || '-'}</span>
                                  </div>
                                  <div className="flex justify-between">
                                    <span className="text-kumo-subtle font-medium">延迟</span>
                                    <span className="text-kumo-strong font-semibold text-kumo-success">{server.response_time || '-'}ms</span>
                                  </div>
                                </div>
                              </div>
                              
                              {/* CPU/Mem 折线图 */}
                              <div className="md:col-span-2 bg-kumo-base border border-kumo-line p-4 rounded-lg flex flex-col gap-2 shadow-xs">
                                <h4 className="text-xs font-bold text-kumo-strong border-b border-kumo-line pb-1.5 mb-1 flex items-center justify-between">
                                  <span>📊 实时监控走势 (CPU & 内存)</span>
                                  <span className="text-[10px] text-kumo-subtle">更新自实时指标流</span>
                                </h4>
                                <div className="h-28 relative">
                                  <canvas id={`metrics-chart-card-${server.id}`}></canvas>
                                </div>
                              </div>
                            </div>
                            
                            {/* GPU 与 网络速率大图 */}
                            {(server.info?.gpu?.Model || server.info?.network) && (
                              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                {server.info?.gpu?.Model && (
                                  <div className="bg-kumo-base border border-kumo-line p-4 rounded-lg flex flex-col gap-2 shadow-xs">
                                    <h4 className="text-xs font-bold text-kumo-strong border-b border-kumo-line pb-1.5 flex items-center gap-1.5">
                                      <span>🎮</span> GPU 走势: {server.info.gpu.Model}
                                    </h4>
                                    <div className="h-24 relative">
                                      <canvas id={`gpu-chart-${server.id}`}></canvas>
                                    </div>
                                  </div>
                                )}
                                {server.info?.network && (
                                  <div className={`bg-kumo-base border border-kumo-line p-4 rounded-lg flex flex-col gap-2 shadow-xs ${!server.info?.gpu?.Model ? 'md:col-span-2' : ''}`}>
                                    <h4 className="text-xs font-bold text-kumo-strong border-b border-kumo-line pb-1.5 flex items-center gap-1.5">
                                      <span>🌐</span> 带宽速率趋势 (Upload / Download)
                                    </h4>
                                    <div className="h-24 relative">
                                      <canvas id={`net-chart-${server.id}`}></canvas>
                                    </div>
                                  </div>
                                )}
                              </div>
                            )}

                            {/* Docker 容器极简看板 */}
                            {server.info?.docker?.installed && (
                              <div className="bg-kumo-base border border-kumo-line p-4 rounded-lg flex flex-col gap-2.5 shadow-xs">
                                <h4 className="text-xs font-bold text-kumo-strong border-b border-kumo-line pb-1.5 flex items-center justify-between">
                                  <div className="flex items-center gap-1.5">
                                    <span className="text-kumo-brand font-bold">🐳</span>
                                    <span>Docker 容器极简看板</span>
                                  </div>
                                  <div className="flex items-center gap-2 text-[10px] font-medium text-kumo-subtle">
                                    <span className="flex items-center gap-1">
                                      <span className="w-1.5 h-1.5 rounded-full bg-kumo-success"></span>
                                      {server.info.docker.runningCount || 0} 运行
                                    </span>
                                    {server.info.docker.stoppedCount > 0 && (
                                      <span className="flex items-center gap-1">
                                        <span className="w-1.5 h-1.5 rounded-full bg-kumo-danger"></span>
                                        {server.info.docker.stoppedCount} 停止
                                      </span>
                                    )}
                                  </div>
                                </h4>

                                {server.info.docker.containers && server.info.docker.containers.length > 0 ? (
                                  <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-2 pt-1">
                                    {server.info.docker.containers.map(c => {
                                      const isUp = c.state === 'running' || (c.status && c.status.includes('Up') && !c.status.includes('Paused'));
                                      const isPaused = c.state === 'paused' || (c.status && c.status.includes('Paused'));
                                      return (
                                        <div key={c.id} className="flex items-center justify-between p-2 rounded border text-[11px] bg-kumo-canvas/45 border-kumo-line hover:border-kumo-interact">
                                          <div className="flex items-center gap-1.5 min-w-0 mr-2">
                                            <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${isUp ? 'bg-kumo-success animate-pulse' : isPaused ? 'bg-kumo-warning' : 'bg-kumo-fill'}`}></span>
                                            <span className="truncate font-semibold text-kumo-strong" title={c.name}>{c.name}</span>
                                          </div>
                                          <div className="flex items-center gap-1 flex-shrink-0">
                                            {isUp ? (
                                              <button
                                                onClick={(e) => { e.stopPropagation(); submitDockerTask('container.stop', { serverId: server.id, containerId: c.id, containerName: c.name }); }}
                                                className="p-1 text-kumo-subtle hover:text-kumo-warning hover:bg-kumo-warning/10 rounded cursor-pointer"
                                                title="停止"
                                              >
                                                <Pause className="w-3 h-3" />
                                              </button>
                                            ) : (
                                              <button
                                                onClick={(e) => { e.stopPropagation(); submitDockerTask('container.start', { serverId: server.id, containerId: c.id, containerName: c.name }); }}
                                                className="p-1 text-kumo-subtle hover:text-kumo-success hover:bg-kumo-success/10 rounded cursor-pointer"
                                                title="启动"
                                              >
                                                <Play className="w-3 h-3" />
                                              </button>
                                            )}
                                            <button
                                              onClick={(e) => { e.stopPropagation(); submitDockerTask('container.restart', { serverId: server.id, containerId: c.id, containerName: c.name }); }}
                                              className="p-1 text-kumo-subtle hover:text-kumo-brand hover:bg-kumo-brand/10 rounded cursor-pointer"
                                              title="重启"
                                            >
                                              <RotateCw className="w-3 h-3" />
                                            </button>
                                          </div>
                                        </div>
                                      );
                                    })}
                                  </div>
                                ) : (
                                  <div className="text-center py-4 text-xs text-kumo-subtle">
                                    暂无运行中/已停止的容器
                                  </div>
                                )}
                              </div>
                            )}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}
      
      {/* ==================== 2. 历史趋势 ==================== */}
      {serverCurrentTab === 'history' && (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col md:flex-row gap-4 justify-between bg-kumo-base border border-kumo-line p-4 rounded-lg">
            <div className="flex items-center gap-2">
              <div className="flex items-center gap-1 bg-kumo-recessed/50 p-1 rounded-lg">
                {['1h', '6h', '24h', '7d'].map(range => (
                  <button
                    key={range}
                    onClick={() => setMetricsHistoryTimeRange(range)}
                    className={`px-3 py-1 rounded-md text-[10px] font-bold cursor-pointer transition-colors ${metricsHistoryTimeRange === range ? 'bg-kumo-base text-kumo-strong shadow-xs' : 'text-kumo-subtle hover:text-kumo-strong'}`}
                  >
                    {range}
                  </button>
                ))}
              </div>
              
              <button
                onClick={triggerManualCollect}
                className="flex items-center gap-1 px-3 py-1 border border-kumo-line rounded-lg text-xs bg-kumo-base hover:bg-kumo-recessed/30 cursor-pointer font-semibold"
              >
                立即采集
              </button>
              
              <button
                onClick={clearMetricsHistory}
                className="flex items-center gap-1 px-3 py-1 border border-kumo-danger/30 text-kumo-danger rounded-lg text-xs bg-kumo-base hover:bg-kumo-danger/10 cursor-pointer font-semibold"
              >
                清空数据
              </button>
            </div>
            
            <div className="flex items-center gap-2">
              <span className="text-xs text-kumo-subtle font-medium">过滤主机</span>
              <select
                value={metricsHistoryFilter.serverId}
                onChange={e => setMetricsHistoryFilter({ serverId: e.target.value })}
                className="border border-kumo-line rounded-lg px-2.5 py-1 bg-kumo-base text-xs focus:outline-none"
              >
                <option value="">全部主机</option>
                {serverList.map(s => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
            </div>
          </div>
          
          {/* 采集器参数微型概览 */}
          {metricsCollectorStatus && (
            <div className="flex flex-wrap gap-4 text-xs font-semibold text-kumo-subtle bg-kumo-base border border-kumo-line p-3 rounded-lg">
              <div className="flex items-center gap-1.5">
                <span className={`w-2 h-2 rounded-full ${metricsCollectorStatus.isRunning ? 'bg-kumo-success' : 'bg-kumo-danger'}`}></span>
                <span>采集状态: {metricsCollectorStatus.isRunning ? '运行中' : '停止'}</span>
              </div>
              <div className="border-l border-kumo-line pl-3">
                <span>采集间隔: {metricsCollectInterval} 分钟</span>
              </div>
              <div className="border-l border-kumo-line pl-3">
                <span>最大保留天数: {monitorConfig.metrics_retention_days} 天</span>
              </div>
            </div>
          )}
          
          {/* 数据大列表表格 */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden shadow-xs">
            {metricsHistoryLoading ? (
              <div className="p-16 text-center text-xs text-kumo-subtle">
                正在检索监控历史记录...
              </div>
            ) : metricsHistoryList.length === 0 ? (
              <div className="p-16 text-center text-xs text-kumo-subtle">
                暂无历史记录指标
              </div>
            ) : (
              <table className="w-full text-left border-collapse text-xs">
                <thead>
                  <tr className="bg-kumo-recessed/45 border-b border-kumo-line font-bold text-kumo-strong">
                    <th className="p-3">记录时间</th>
                    <th className="p-3">主机</th>
                    <th className="p-3 text-center">CPU 使用率</th>
                    <th className="p-3 text-center">内存使用率</th>
                    <th className="p-3 text-center">磁盘使用率</th>
                    <th className="p-3">系统负载</th>
                  </tr>
                </thead>
                <tbody>
                  {metricsHistoryList.map(rec => (
                    <tr key={rec.id} className="border-b border-kumo-line hover:bg-kumo-recessed/15">
                      <td className="p-3 font-semibold text-kumo-strong">{formatDateTime(rec.recorded_at)}</td>
                      <td className="p-3">{rec.server_name}</td>
                      <td className="p-3 text-center font-bold text-kumo-success">{rec.cpu_usage?.toFixed(1)}%</td>
                      <td className="p-3 text-center font-bold text-kumo-info">{rec.mem_usage?.toFixed(1)}%</td>
                      <td className="p-3 text-center">{rec.disk_usage?.toFixed(1)}%</td>
                      <td className="p-3"><code className="bg-kumo-recessed px-1.5 py-0.5 rounded font-mono text-[10px]">{rec.cpu_load || '-'}</code></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      )}
      
      {/* ==================== 3. Docker 控制台 ==================== */}
      {serverCurrentTab === 'docker' && (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col md:flex-row gap-4 justify-between border-b border-kumo-line pb-2.5">
            <div className="flex items-center gap-1.5 overflow-x-auto scrollbar-thin">
              {[
                { id: 'containers', name: '容器', icon: Box },
                { id: 'compose', name: 'Compose', icon: FolderOpen },
                { id: 'images', name: '镜像', icon: HardDrive },
                { id: 'networks', name: '网络', icon: Globe },
                { id: 'volumes', name: '存储卷', icon: HardDrive },
                { id: 'stats', name: '实时统计', icon: Activity }
              ].map(sub => (
                <button
                  key={sub.id}
                  onClick={() => setDockerSubTab(sub.id)}
                  className={`px-3 py-1.5 rounded-lg text-xs font-semibold cursor-pointer transition-colors ${dockerSubTab === sub.id ? 'bg-kumo-recessed text-kumo-strong' : 'text-kumo-subtle hover:text-kumo-strong hover:bg-kumo-recessed/40'}`}
                >
                  {sub.name}
                </button>
              ))}
            </div>
            
            <div className="flex items-center gap-2">
              <span className="text-xs text-kumo-subtle font-medium">选择主机</span>
              <select
                value={dockerSelectedServer}
                onChange={e => setDockerSelectedServer(e.target.value)}
                className="border border-kumo-line rounded-lg px-2.5 py-1 bg-kumo-base text-xs focus:outline-none"
              >
                <option value="">全部 Docker 主机</option>
                {serverList.filter(s => s.status === 'online').map(s => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
            </div>
          </div>
          
          {/* Docker 任务中心 */}
          {dockerTasks.length > 0 && (
            <div className="bg-kumo-recessed border border-kumo-line p-3 rounded-lg text-xs font-mono text-kumo-default flex flex-col gap-1.5 shadow-xs">
              <div className="flex justify-between border-b border-kumo-line pb-1.5 mb-1">
                <span className="font-bold text-kumo-brand">🔄 后台 Docker 任务流水</span>
                <span className="text-[10px] text-kumo-subtle">SSE 实时长连接</span>
              </div>
              <div className="max-h-24 overflow-y-auto flex flex-col gap-1">
                {dockerTasks.map(t => (
                  <div key={t.taskId} className="flex justify-between gap-4">
                    <span className={t.state === 'success' ? 'text-kumo-success' : t.state === 'failed' ? 'text-kumo-danger' : 'text-kumo-warning'}>
                      [{t.state?.toUpperCase()}] {t.action}
                    </span>
                    <span className="text-kumo-subtle">{t.message}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
          
          {/* 内容区域 */}
          {dockerResourceLoading ? (
            <div className="p-16 text-center text-xs text-kumo-subtle flex flex-col items-center gap-2">
              <div className="w-5 h-5 border-2 border-kumo-brand border-t-transparent rounded-full animate-spin"></div>
              <span>正在拉取最新容器资源表...</span>
            </div>
          ) : (
            <div className="flex flex-col gap-4">
              {/* 1. 容器管理 */}
              {dockerSubTab === 'containers' && (
                <div className="flex flex-col gap-3">
                  {dockerOverviewServers.map(server => (
                    <div key={server.id} className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden shadow-xs">
                      <div className="bg-kumo-recessed/35 p-3 border-b border-kumo-line flex items-center justify-between">
                        <span className="text-xs font-bold text-kumo-strong flex items-center gap-2">
                          <span>🐳</span> {server.name}
                        </span>
                        <span className="px-1.5 py-0.5 rounded text-[10px] bg-kumo-brand/10 text-kumo-brand font-bold">
                          {server.resources?.containers?.length || 0} 个容器
                        </span>
                      </div>
                      
                      <div className="p-2">
                        {(!server.resources?.containers || server.resources.containers.length === 0) ? (
                          <div className="p-8 text-center text-xs text-kumo-subtle">
                            暂无运行中容器
                          </div>
                        ) : (
                          <table className="w-full text-left border-collapse text-xs">
                            <thead>
                              <tr className="border-b border-kumo-line text-kumo-subtle font-bold">
                                <th className="p-2">名称</th>
                                <th className="p-2">镜像</th>
                                <th className="p-2">状态</th>
                                <th className="p-2">端口映射</th>
                                <th className="p-2 text-right">操作</th>
                              </tr>
                            </thead>
                            <tbody>
                              {server.resources.containers.map(c => (
                                <tr key={c.id} className="border-b border-kumo-line hover:bg-kumo-recessed/10">
                                  <td className="p-2 font-bold text-kumo-strong">{c.name}</td>
                                  <td className="p-2 truncate max-w-[200px]" title={c.image}>{c.image}</td>
                                  <td className="p-2">
                                    <span className={`px-2 py-0.5 rounded text-[10px] font-bold ${c.state === 'running' ? 'bg-kumo-success/15 text-kumo-success' : 'bg-kumo-danger/15 text-kumo-danger'}`}>
                                      {c.state}
                                    </span>
                                  </td>
                                  <td className="p-2 font-mono text-[11px] text-kumo-subtle">{c.ports || '-'}</td>
                                  <td className="p-2 text-right flex items-center justify-end gap-1.5">
                                    <button
                                      onClick={() => submitDockerTask(c.state === 'running' ? 'container.stop' : 'container.start', { serverId: server.id, containerId: c.id, containerName: c.name })}
                                      className={`p-1.5 rounded cursor-pointer ${c.state === 'running' ? 'hover:bg-kumo-danger/10 text-kumo-danger' : 'hover:bg-kumo-success/10 text-kumo-success'}`}
                                      title={c.state === 'running' ? '停止' : '启动'}
                                    >
                                      {c.state === 'running' ? <PauseIcon /> : <PlayIcon />}
                                    </button>
                                    <button
                                      onClick={() => submitDockerTask('container.restart', { serverId: server.id, containerId: c.id, containerName: c.name })}
                                      className="p-1.5 rounded hover:bg-kumo-recessed text-kumo-subtle cursor-pointer"
                                      title="重启"
                                    >
                                      <RestartIcon />
                                    </button>
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
              
              {/* 2. Compose */}
              {dockerSubTab === 'compose' && (
                <div className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden p-3.5 shadow-xs">
                  {dockerComposeProjects.length === 0 ? (
                    <div className="p-12 text-center text-xs text-kumo-subtle">
                      当前主机中未检索到 Compose 项目
                    </div>
                  ) : (
                    <div className="flex flex-col gap-3">
                      {dockerComposeProjects.map(proj => (
                        <div key={proj.Name} className="flex justify-between items-center p-3 border border-kumo-line rounded-lg bg-kumo-canvas/15 hover:border-kumo-brand/50">
                          <div className="flex flex-col gap-0.5">
                            <span className="text-xs font-bold text-kumo-strong">{proj.Name}</span>
                            <span className="text-[10px] text-kumo-subtle truncate max-w-[400px]">{proj.ConfigFiles}</span>
                          </div>
                          
                          <div className="flex items-center gap-3">
                            <span className={`px-2 py-0.5 rounded text-[10px] font-bold ${proj.Status?.includes('running') ? 'bg-kumo-success/15 text-kumo-success' : 'bg-kumo-danger/15 text-kumo-danger'}`}>
                              {proj.Status}
                            </span>
                            <div className="flex gap-1">
                              <button
                                onClick={() => submitDockerTask('compose.up', { serverId: proj.serverId, projectName: proj.Name })}
                                className="p-1 px-2.5 rounded bg-kumo-brand text-kumo-inverse text-[10px] font-semibold cursor-pointer"
                              >
                                Up 启动
                              </button>
                              <button
                                onClick={() => submitDockerTask('compose.down', { serverId: proj.serverId, projectName: proj.Name })}
                                className="p-1 px-2.5 rounded border border-kumo-line text-kumo-subtle text-[10px] hover:bg-kumo-recessed/45 cursor-pointer"
                              >
                                Down 停止
                              </button>
                            </div>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
              
              {/* 3. 镜像管理 */}
              {dockerSubTab === 'images' && (
                <div className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden p-2 shadow-xs">
                  <table className="w-full text-left border-collapse text-xs">
                    <thead>
                      <tr className="bg-kumo-recessed/25 border-b border-kumo-line font-bold">
                        <th className="p-2.5">镜像仓库</th>
                        <th className="p-2.5">标签</th>
                        <th className="p-2.5">大小</th>
                        <th className="p-2.5">所在主机</th>
                        <th className="p-2.5 text-right">操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {dockerImages.map((img, i) => (
                        <tr key={img.id + i} className="border-b border-kumo-line hover:bg-kumo-recessed/10">
                          <td className="p-2.5 font-bold text-kumo-strong">{img.repository}</td>
                          <td className="p-2.5"><span className="px-1.5 py-0.5 rounded bg-kumo-recessed font-mono text-[10px]">{img.tag}</span></td>
                          <td className="p-2.5 text-kumo-subtle">{img.size}</td>
                          <td className="p-2.5">{img.serverName}</td>
                          <td className="p-2.5 text-right">
                            <button
                              onClick={() => submitDockerTask('image.remove', { serverId: img.serverId, imageId: img.id })}
                              className="p-1 hover:bg-kumo-danger/10 text-kumo-danger rounded cursor-pointer"
                            >
                              <TrashIcon />
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </div>
      )}
      
      {/* ==================== 4. 后台管理 ==================== */}
      {serverCurrentTab === 'management' && (
        <div className="flex flex-col gap-6">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            
            {/* 列表大配置 */}
            <div className="bg-kumo-base border border-kumo-line p-5 rounded-lg flex flex-col gap-4 shadow-xs">
              <h3 className="text-sm font-bold text-kumo-strong border-b border-kumo-line pb-2 mb-1">
                ⚙️ 主机自动监控配置
              </h3>
              
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">
                  自动采集间隔 (周期)
                </label>
                <div className="flex flex-wrap gap-2">
                  {[1, 2, 5, 10, 15, 30, 60].map(m => (
                    <button
                      key={m}
                      onClick={() => updateMetricsCollectInterval(m)}
                      className={`px-3 py-1.5 border rounded-lg text-xs font-semibold cursor-pointer transition-colors ${metricsCollectInterval === m ? 'bg-kumo-brand text-kumo-inverse border-kumo-brand' : 'bg-kumo-base border-kumo-line text-kumo-subtle hover:text-kumo-strong'}`}
                    >
                      {m}分钟
                    </button>
                  ))}
                </div>
              </div>
              
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold text-kumo-subtle flex justify-between">
                  <span>历史数据保留期限</span>
                  <span className="text-kumo-brand">{monitorConfig.metrics_retention_days} 天</span>
                </label>
                <input
                  type="range"
                  min="1"
                  max="180"
                  value={monitorConfig.metrics_retention_days}
                  onChange={e => handleRetentionSliderChange(parseInt(e.target.value))}
                  className="w-full accent-kumo-brand cursor-pointer"
                />
              </div>
            </div>
            
            {/* 批量添加 */}
            <div className="bg-kumo-base border border-kumo-line p-5 rounded-lg flex flex-col gap-3.5 shadow-xs">
              <h3 className="text-sm font-bold text-kumo-strong border-b border-kumo-line pb-2">
                📂 批量快速添加主机
              </h3>
              <textarea
                value={serverBatchText}
                onChange={e => setServerBatchText(e.target.value)}
                placeholder="例如格式:&#10;前端服务器,192.168.1.10,22,root,密码123&#10;数据库节点,192.168.1.11,22,root,安全密码456"
                className="w-full h-24 p-2.5 border border-kumo-line rounded-lg text-xs font-mono bg-kumo-control focus:outline-none focus:border-kumo-brand"
              ></textarea>
              {serverBatchError && <div className="text-xs text-kumo-danger font-bold">{serverBatchError}</div>}
              {serverBatchSuccess && <div className="text-xs text-kumo-success font-bold">{serverBatchSuccess}</div>}
              <button
                onClick={batchAddServers}
                disabled={serverAddingBatch}
                className="w-full py-2 bg-kumo-brand text-kumo-inverse hover:bg-kumo-brand-hover rounded-lg text-xs font-semibold cursor-pointer disabled:opacity-50"
              >
                {serverAddingBatch ? '正在同步提交...' : '确认批量录入'}
              </button>
            </div>
          </div>
          
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            
            {/* 凭据库 */}
            <div className="md:col-span-1 bg-kumo-base border border-kumo-line p-5 rounded-lg flex flex-col gap-4 shadow-xs">
              <div className="flex items-center justify-between border-b border-kumo-line pb-2.5 mb-1">
                <h3 className="text-sm font-bold text-kumo-strong">
                  🔑 预设 SSH 凭据库
                </h3>
                <button
                  onClick={() => setShowAddCredentialModal(true)}
                  className="px-2 py-1 bg-kumo-brand text-kumo-inverse rounded text-[10px] font-bold cursor-pointer"
                >
                  添加凭据
                </button>
              </div>
              
              <div className="max-h-56 overflow-y-auto flex flex-col gap-2">
                {serverCredentials.length === 0 ? (
                  <div className="py-8 text-center text-xs text-kumo-subtle">
                    暂无预设访问凭据
                  </div>
                ) : (
                  serverCredentials.map(cred => (
                    <div key={cred.id} className={`flex items-center justify-between p-2.5 border rounded-lg bg-kumo-canvas/20 ${cred.is_default ? 'border-kumo-brand/60' : 'border-kumo-line'}`}>
                      <div className="flex flex-col">
                        <span className="text-xs font-bold text-kumo-strong flex items-center gap-1">
                          {cred.is_default && <span className="text-kumo-warning">★</span>}
                          {cred.name}
                        </span>
                        <span className="text-[10px] text-kumo-subtle font-mono">{cred.username}</span>
                      </div>
                      <div className="flex gap-1.5">
                        {!cred.is_default && (
                          <button
                            onClick={() => setDefaultCredential(cred.id)}
                            className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle cursor-pointer"
                            title="设为默认"
                          >
                            <StarIcon />
                          </button>
                        )}
                        <button
                          onClick={() => deleteCredential(cred.id)}
                          className="p-1 hover:bg-kumo-danger/10 text-kumo-danger rounded cursor-pointer"
                          title="删除"
                        >
                          <TrashIcon />
                        </button>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </div>
            
            {/* 备份与导入导出 */}
            <div className="md:col-span-2 bg-kumo-base border border-kumo-line p-5 rounded-lg flex flex-col gap-4 shadow-xs">
              <h3 className="text-sm font-bold text-kumo-strong border-b border-kumo-line pb-2 mb-1">
                📥 备份 / 数据导入导出
              </h3>
              
              <div className="flex flex-col gap-3 text-xs text-kumo-subtle">
                <p>您可以通过导出功能将本地所有注册的主机信息（包含凭据、地址等）打包备份为 JSON 配置文件。在此之后，您可以在其他节点通过导入快速恢复完整的拓扑结构。</p>
                
                <div className="flex gap-3 mt-2">
                  <button
                    onClick={exportServers}
                    className="flex items-center gap-1 px-4 py-2 border border-kumo-line rounded-lg bg-kumo-base hover:bg-kumo-recessed/45 font-semibold text-kumo-strong cursor-pointer"
                  >
                    <Upload className="w-3.5 h-3.5" />
                    导出主机备份 (JSON)
                  </button>
                  
                  <button
                    onClick={() => {
                      setImportPreview(null);
                      setImportModalError('');
                      setShowImportServerModal(true);
                    }}
                    className="flex items-center gap-1 px-4 py-2 border border-kumo-line rounded-lg bg-kumo-base hover:bg-kumo-recessed/45 font-semibold text-kumo-strong cursor-pointer"
                  >
                    <Download className="w-3.5 h-3.5" />
                    导入主机配置文件
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
      
      {/* ==================== 5. SSH 终端 (多分屏支持) ==================== */}
      {serverCurrentTab === 'terminal' && (
        <div className="flex flex-col bg-kumo-canvas border border-kumo-line rounded-lg overflow-hidden w-full h-[70vh] shadow-lg">
          {/* 终端顶部操作与标签栏 */}
          <div className="flex items-center justify-between bg-kumo-base border-b border-kumo-line px-3 py-2 text-xs">
            <div className="flex items-center gap-1.5 overflow-x-auto scrollbar-thin max-w-lg">
              {sshSessions.map(sess => (
                <div
                  key={sess.id}
                  onClick={() => switchToSSHTab(sess.id)}
                  className={`flex items-center gap-2 px-3 py-1.5 rounded cursor-pointer font-semibold ${activeSSHSessionId === sess.id ? 'bg-kumo-recessed text-kumo-strong' : 'text-kumo-subtle hover:bg-kumo-recessed hover:text-kumo-strong'}`}
                >
                  <span className="w-1.5 h-1.5 rounded-full bg-kumo-success"></span>
                  <span className="truncate max-w-[80px]">{sess.name}</span>
                  <button
                    onClick={e => {
                      e.stopPropagation();
                      closeSSHSession(sess.id);
                    }}
                    className="hover:text-kumo-danger font-bold text-[10px]"
                  >
                    ×
                  </button>
                </div>
              ))}
            </div>
            
            <div className="flex items-center gap-2 text-kumo-strong">
              <button
                onClick={() => setSshSyncEnabled(prev => !prev)}
                className={`px-2 py-1 rounded text-[10px] font-bold cursor-pointer transition-colors ${sshSyncEnabled ? 'bg-kumo-brand text-kumo-inverse' : 'border border-kumo-line text-kumo-subtle hover:bg-kumo-recessed hover:text-kumo-strong'}`}
                title="开启多终端广播同步输入模式"
              >
                📣 广播输入: {sshSyncEnabled ? '开' : '关'}
              </button>
              
              <button
                onClick={() => reconnectSSHSession(activeSSHSessionId)}
                className="px-2 py-1 border border-kumo-line hover:bg-kumo-recessed rounded text-[10px] font-bold cursor-pointer"
                title="重新连接终端"
              >
                重连
              </button>
            </div>
          </div>
          
          {/* 分屏网格主展示区 */}
          <div className="flex-1 relative flex">
            {/* 网格网格系统 */}
            <div
              className={`flex-1 grid gap-1.5 p-1.5 bg-kumo-recessed ${
                sshViewLayout === 'split-h' ? 'grid-cols-2' :
                sshViewLayout === 'split-v' ? 'grid-rows-2' :
                sshViewLayout === 'grid' ? 'grid-cols-2 grid-rows-2' : 'grid-cols-1'
              }`}
            >
              {visibleSessionIds.map((id, index) => (
                <div
                  key={id}
                  id={`ssh-slot-idx-${index}`}
                  onDragOver={e => handleTerminalDragOver(e, id)}
                  onDrop={e => triggerSplitPane(id, 'right')}
                  className={`w-full h-full border ${activeSSHSessionId === id ? 'border-kumo-brand/60' : 'border-kumo-line'} rounded-sm overflow-hidden bg-kumo-base relative`}
                >
                  {/* 分割与拖拽锚点标题 */}
                  <div
                    draggable
                    onDragStart={() => handleTerminalDragStart(id)}
                    className="bg-kumo-recessed px-2 py-1.5 text-[10px] text-kumo-subtle font-mono select-none flex items-center justify-between cursor-move"
                  >
                    <span>🖥️ {sshSessions.find(s => s.id === id)?.name || 'SSH'}</span>
                    <div className="flex gap-2">
                      <button
                        onClick={() => triggerSplitPane(id, 'right')}
                        className="hover:text-kumo-strong"
                        title="左右分割"
                      >
                        [|]
                      </button>
                      <button
                        onClick={() => closeSSHSession(id)}
                        className="hover:text-kumo-danger"
                      >
                        ×
                      </button>
                    </div>
                  </div>
                  
                  {/* 此处会在 syncTerminalDOM 中动态 append xterm.js 容器 */}
                </div>
              ))}
            </div>
            
            {/* SFTP 文件管理系统抽屉面板 */}
            {showSftpSidebar && (
              <div className="w-80 border-l border-kumo-line bg-kumo-base p-3 flex flex-col gap-3 text-xs">
                <div className="flex items-center justify-between border-b border-kumo-line pb-2">
                  <span className="font-bold text-kumo-strong flex items-center gap-1.5">
                    📁 SFTP 文件管理柜
                  </span>
                  <button
                    onClick={() => setShowSftpSidebar(false)}
                    className="text-kumo-subtle hover:text-kumo-strong"
                  >
                    ×
                  </button>
                </div>
                
                <div className="flex items-center gap-1 bg-kumo-recessed p-1.5 rounded text-[10px] text-kumo-subtle font-mono truncate">
                  <span>路径: {sftpCurrentPath}</span>
                </div>
                
                {/* 目录面包屑导航 */}
                <div className="flex items-center gap-1.5 text-[10px] overflow-x-auto whitespace-nowrap scrollbar-thin">
                  {sftpBreadcrumbs.map((crumb, idx) => (
                    <React.Fragment key={crumb.path}>
                      <span
                        onClick={() => loadSftpDirectory(sftpServerId, crumb.path)}
                        className="cursor-pointer hover:text-kumo-brand"
                      >
                        {crumb.name}
                      </span>
                      {idx < sftpBreadcrumbs.length - 1 && <span className="opacity-40">/</span>}
                    </React.Fragment>
                  ))}
                </div>
                
                {/* 文件与目录树列表 */}
                <div className="flex-1 overflow-y-auto flex flex-col gap-1.5 scrollbar-thin pr-1.5">
                  {sftpLoading ? (
                    <div className="py-8 text-center text-[10px] text-kumo-subtle">
                      读取远程目录中...
                    </div>
                  ) : sftpFiles.length === 0 ? (
                    <div className="py-8 text-center text-[10px] text-kumo-subtle">
                      当前目录为空
                    </div>
                  ) : (
                    sftpFiles.map(file => (
                      <div
                        key={file.path}
                        onClick={() => handleSftpFileClick(file)}
                        className="flex items-center justify-between p-1.5 border border-kumo-line rounded bg-kumo-recessed hover:border-kumo-brand/60 cursor-pointer"
                      >
                        <div className="flex items-center gap-2 truncate">
                          <span>{file.isDirectory ? '📁' : '📄'}</span>
                          <span className="text-kumo-strong truncate font-semibold" title={file.name}>{file.name}</span>
                        </div>
                        <span className="text-[9px] text-kumo-subtle">{file.isDirectory ? '目录' : formatFileSize(file.size)}</span>
                      </div>
                    ))
                  )}
                </div>
                
                {/* 底部 SFTP 动作 */}
                <div className="flex gap-2 border-t border-kumo-line pt-2">
                  <label className="flex-1 py-1.5 bg-kumo-recessed hover:bg-kumo-fill border border-kumo-line rounded text-center text-[10px] font-bold cursor-pointer">
                    <input type="file" className="hidden" onChange={handleSftpUpload} multiple />
                    上传文件
                  </label>
                  <button
                    onClick={() => loadSftpDirectory(sftpServerId, sftpCurrentPath)}
                    className="flex-1 py-1.5 border border-kumo-line text-kumo-subtle hover:text-kumo-strong hover:bg-kumo-recessed rounded text-[10px] font-bold cursor-pointer"
                  >
                    刷新
                  </button>
                </div>
              </div>
            )}
            
            {/* 右侧终端小功能栏 */}
            <div className="w-11 border-l border-kumo-line bg-kumo-base flex flex-col items-center py-3.5 gap-4 text-kumo-subtle">
              <button
                onClick={() => {
                  setShowSftpSidebar(prev => !prev);
                  if (!showSftpSidebar && activeSSHSessionId) {
                    const serverId = sshSessions.find(s => s.id === activeSSHSessionId)?.server.id;
                    if (serverId) loadSftpDirectory(serverId, '.');
                  }
                }}
                className={`p-1.5 rounded hover:text-kumo-strong cursor-pointer ${showSftpSidebar ? 'text-kumo-strong bg-kumo-recessed' : ''}`}
                title="目录文件管理 SFTP"
              >
                <FolderOpen className="w-4 h-4" />
              </button>
            </div>
          </div>
        </div>
      )}
      
      {/* ==================== xterm.js 实例静默挂载的仓库 ==================== */}
      <div ref={warehouseRef} className="hidden absolute -top-[9999px]" id="ssh-terminal-warehouse"></div>
      
      {/* ==================== 模态框: 添加与编辑服务器 ==================== */}
      {showServerModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-kumo-contrast/60 backdrop-blur-xs">
          <div className="bg-kumo-base border border-kumo-line rounded-lg w-full max-w-lg shadow-xl overflow-hidden flex flex-col">
            <div className="flex items-center justify-between bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
              <h3 className="text-sm font-bold text-kumo-strong">
                {serverModalMode === 'add' ? '新增主机实例' : '编辑主机实例'}
              </h3>
              <button onClick={() => setShowServerModal(false)} className="text-kumo-subtle hover:text-kumo-strong cursor-pointer font-bold">
                ×
              </button>
            </div>
            
            <div className="p-4 flex-1 overflow-y-auto max-h-[70vh] flex flex-col gap-4 text-xs">
              {serverModalMode === 'add' && (
                <Tabs
                  size="sm"
                  value={serverAddMode}
                  onValueChange={(value) => {
                    setServerAddMode(value);
                    setServerModalError('');
                  }}
                  tabs={[
                    { value: 'ssh', label: 'SSH' },
                    { value: 'agent', label: 'Agent' },
                  ]}
                />
              )}

              {serverModalMode === 'edit' || serverAddMode === 'ssh' ? (
                <>
              <div className="grid grid-cols-2 gap-4">
                <div className="flex flex-col gap-1.5">
                  <label className="font-semibold text-kumo-subtle">主机名称 (别名)</label>
                  <input
                    type="text"
                    value={serverForm.name}
                    onChange={e => setServerForm(prev => ({ ...prev, name: e.target.value }))}
                    placeholder="生产数据库-01"
                    className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="font-semibold text-kumo-subtle">地区 / 归属国家 (Flags)</label>
                  <select
                    value={serverForm.country}
                    onChange={e => setServerForm(prev => ({ ...prev, country: e.target.value }))}
                    className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control focus:outline-none focus:border-kumo-brand"
                  >
                    <option value="auto">自动探测</option>
                    <option value="CN">中国 (CN)</option>
                    <option value="US">美国 (US)</option>
                    <option value="HK">香港 (HK)</option>
                    <option value="JP">日本 (JP)</option>
                    <option value="SG">新加坡 (SG)</option>
                  </select>
                </div>
              </div>
              
              <div className="grid grid-cols-3 gap-4">
                <div className="col-span-2 flex flex-col gap-1.5">
                  <label className="font-semibold text-kumo-subtle">连接地址 (IP / Host)</label>
                  <input
                    type="text"
                    value={serverForm.host}
                    onChange={e => setServerForm(prev => ({ ...prev, host: e.target.value }))}
                    placeholder="12.34.56.78"
                    className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="font-semibold text-kumo-subtle">端口</label>
                  <input
                    type="number"
                    value={serverForm.port}
                    onChange={e => setServerForm(prev => ({ ...prev, port: parseInt(e.target.value) || 22 }))}
                    placeholder="22"
                    className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
                  />
                </div>
              </div>
              
              <div className="flex flex-col gap-2">
                <label className="font-semibold text-kumo-subtle">选择凭据预设进行快速填充</label>
                <select
                  value={selectedCredentialId}
                  onChange={e => applyCredential(e.target.value)}
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control focus:outline-none"
                >
                  <option value="">-- 手动录入 --</option>
                  {serverCredentials.map(c => (
                    <option key={c.id} value={c.id}>{c.name} ({c.username})</option>
                  ))}
                </select>
              </div>
              
              <div className="border-t border-kumo-line pt-3 flex flex-col gap-3">
                <div className="grid grid-cols-2 gap-4">
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">登录用户名</label>
                    <input
                      type="text"
                      value={serverForm.username}
                      onChange={e => setServerForm(prev => ({ ...prev, username: e.target.value }))}
                      placeholder="root"
                      className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">身份验证方案</label>
                    <div className="flex gap-2 py-1">
                      <label className="flex items-center gap-1.5 cursor-pointer">
                        <input
                          type="radio"
                          name="authType"
                          checked={serverForm.authType === 'password'}
                          onChange={() => setServerForm(prev => ({ ...prev, authType: 'password' }))}
                          className="accent-kumo-brand"
                        />
                        密码验证
                      </label>
                      <label className="flex items-center gap-1.5 cursor-pointer">
                        <input
                          type="radio"
                          name="authType"
                          checked={serverForm.authType === 'privateKey'}
                          onChange={() => setServerForm(prev => ({ ...prev, authType: 'privateKey' }))}
                          className="accent-kumo-brand"
                        />
                        秘钥证书
                      </label>
                    </div>
                  </div>
                </div>
                
                {serverForm.authType === 'password' ? (
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">连接密码</label>
                    <input
                      type="password"
                      value={serverForm.password}
                      onChange={e => setServerForm(prev => ({ ...prev, password: e.target.value }))}
                      placeholder={serverModalMode === 'edit' ? '****** (留空不修改)' : '登录密码'}
                      className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
                    />
                  </div>
                ) : (
                  <div className="flex flex-col gap-3">
                    <div className="flex flex-col gap-1.5">
                      <label className="font-semibold text-kumo-subtle">证书密钥 (Private Key)</label>
                      <textarea
                        value={serverForm.privateKey}
                        onChange={e => setServerForm(prev => ({ ...prev, privateKey: e.target.value }))}
                        placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                        className="w-full h-20 p-2 border border-kumo-line rounded-lg text-xs font-mono bg-kumo-control focus:outline-none focus:border-kumo-brand"
                      ></textarea>
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="font-semibold text-kumo-subtle">密钥口令 (密码保护短语，若有)</label>
                      <input
                        type="password"
                        value={serverForm.passphrase}
                        onChange={e => setServerForm(prev => ({ ...prev, passphrase: e.target.value }))}
                        placeholder="Key Passphrase"
                        className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none"
                      />
                    </div>
                  </div>
                )}
              </div>
              
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle">自定义主机标签 (逗号分隔)</label>
                <input
                  type="text"
                  value={serverForm.tagsInput}
                  onChange={e => setServerForm(prev => ({ ...prev, tagsInput: e.target.value }))}
                  placeholder="Production,Database,US"
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
                />
              </div>
              
                </>
              ) : (
                <div className="flex flex-col gap-4">
                  <Input
                    label="Host name"
                    size="sm"
                    value={quickDeployName}
                    onChange={(e) => setQuickDeployName(e.target.value)}
                    placeholder="prod-agent-01"
                  />

                  <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-[11px] leading-relaxed text-kumo-subtle">
                    Agent mode creates or reuses a host record, then returns install commands for the target machine.
                  </div>

                  {quickDeployResult && (
                    <div className="flex flex-col gap-3">
                      <Select
                        label="Install target"
                        size="sm"
                        value={agentInstallOS}
                        onValueChange={setAgentInstallOS}
                        items={[
                          { value: 'linux', label: 'Linux / macOS' },
                          { value: 'windows', label: 'Windows PowerShell' },
                        ]}
                      />
                      <Textarea
                        label="Install command"
                        value={agentInstallOS === 'linux' ? quickDeployResult.installCommand || '' : quickDeployResult.winInstallCommand || ''}
                        readOnly
                        className="min-h-24 font-mono text-[11px]"
                      />
                      <div className="grid grid-cols-2 gap-2 text-[11px] text-kumo-subtle">
                        <div className="rounded-md border border-kumo-line bg-kumo-base p-2">
                          <div className="font-semibold text-kumo-strong">Server ID</div>
                          <div className="mt-1 font-mono">{quickDeployResult.serverId}</div>
                        </div>
                        <div className="rounded-md border border-kumo-line bg-kumo-base p-2">
                          <div className="font-semibold text-kumo-strong">API URL</div>
                          <div className="mt-1 truncate font-mono" title={quickDeployResult.apiUrl}>{quickDeployResult.apiUrl}</div>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {serverModalError && (
                <div className="text-xs text-kumo-danger font-bold bg-kumo-danger/10 border border-kumo-danger/20 p-2.5 rounded">
                  {serverModalError}
                </div>
              )}
            </div>
            
            <div className="bg-kumo-recessed/25 px-4 py-3 border-t border-kumo-line flex justify-end gap-2.5">
              {serverModalMode === 'add' && serverAddMode === 'agent' ? (
                <>
                  {quickDeployResult && (
                    <Button
                      type="button"
                      size="sm"
                      variant="secondary"
                      icon={<Copy className="w-3.5 h-3.5" />}
                      onClick={copyQuickDeployCommand}
                    >
                      Copy command
                    </Button>
                  )}
                  <Button
                    type="button"
                    size="sm"
                    variant="primary"
                    loading={serverModalSaving}
                    onClick={generateQuickInstallCommand}
                  >
                    Generate Agent command
                  </Button>
                </>
              ) : null}
              <button
                onClick={testServerConnection}
                disabled={serverModalSaving}
                className={`px-3.5 py-1.5 border border-kumo-line rounded-lg text-xs font-semibold hover:bg-kumo-recessed cursor-pointer ${serverModalMode === 'add' && serverAddMode === 'agent' ? 'hidden' : ''}`}
              >
                连接测试
              </button>
              <button
                onClick={saveServer}
                disabled={serverModalSaving}
                className={`px-4 py-1.5 bg-kumo-brand text-kumo-inverse hover:bg-kumo-brand-hover rounded-lg text-xs font-bold cursor-pointer ${serverModalMode === 'add' && serverAddMode === 'agent' ? 'hidden' : ''}`}
              >
                {serverModalSaving ? '保存中...' : '确认保存'}
              </button>
            </div>
          </div>
        </div>
      )}
      
      {/* ==================== 模态框: 凭据预设新增 ==================== */}
      {showAddCredentialModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-kumo-contrast/60 backdrop-blur-xs">
          <div className="bg-kumo-base border border-kumo-line rounded-lg w-full max-w-md shadow-xl overflow-hidden flex flex-col">
            <div className="flex items-center justify-between bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
              <h3 className="text-sm font-bold text-kumo-strong">新增 SSH 验证凭据</h3>
              <button onClick={() => setShowAddCredentialModal(false)} className="text-kumo-subtle font-bold cursor-pointer">×</button>
            </div>
            
            <div className="p-4 flex flex-col gap-4 text-xs">
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">凭据别名</label>
                <input
                  type="text"
                  value={credForm.name}
                  onChange={e => setCredForm(prev => ({ ...prev, name: e.target.value }))}
                  placeholder="美国节点通用 root 秘钥"
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none"
                />
              </div>
              
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle">用户登录名</label>
                <input
                  type="text"
                  value={credForm.username}
                  onChange={e => setCredForm(prev => ({ ...prev, username: e.target.value }))}
                  placeholder="root"
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none"
                />
              </div>
              
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">登录凭据模式</label>
                <select
                  value={credForm.auth_type}
                  onChange={e => setCredForm(prev => ({ ...prev, auth_type: e.target.value }))}
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control"
                >
                  <option value="password">明文密码</option>
                  <option value="key">私钥证书 (RSA / OpenSSH)</option>
                </select>
              </div>
              
              {credForm.auth_type === 'password' ? (
                <div className="flex flex-col gap-1.5">
                  <label className="font-semibold text-kumo-subtle">默认登录密码</label>
                  <input
                    type="password"
                    value={credForm.password}
                    onChange={e => setCredForm(prev => ({ ...prev, password: e.target.value }))}
                    placeholder="输入密码"
                    className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none"
                  />
                </div>
              ) : (
                <div className="flex flex-col gap-3">
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">PEM 私钥证书内容</label>
                    <textarea
                      value={credForm.private_key}
                      onChange={e => setCredForm(prev => ({ ...prev, private_key: e.target.value }))}
                      placeholder="-----BEGIN RSA PRIVATE KEY-----"
                      className="w-full h-24 p-2 border border-kumo-line rounded-lg text-xs font-mono bg-kumo-control focus:outline-none"
                    ></textarea>
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">证书保护密码短语 (口令)</label>
                    <input
                      type="password"
                      value={credForm.passphrase}
                      onChange={e => setCredForm(prev => ({ ...prev, passphrase: e.target.value }))}
                      placeholder="Passphrase"
                      className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none"
                    />
                  </div>
                </div>
              )}
            </div>
            
            <div className="bg-kumo-recessed/25 px-4 py-3 border-t border-kumo-line flex justify-end gap-2 text-xs">
              <button onClick={() => setShowAddCredentialModal(false)} className="px-3.5 py-1.5 border border-kumo-line rounded-lg cursor-pointer">取消</button>
              <button onClick={addCredential} className="px-4 py-1.5 bg-kumo-brand text-kumo-inverse rounded-lg font-bold cursor-pointer">确认保存</button>
            </div>
          </div>
        </div>
      )}
      
      {/* ==================== 模态框: 导入主机备份 ==================== */}
      {showImportServerModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-kumo-contrast/60 backdrop-blur-xs">
          <div className="bg-kumo-base border border-kumo-line rounded-lg w-full max-w-md shadow-xl overflow-hidden flex flex-col">
            <div className="flex items-center justify-between bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
              <h3 className="text-sm font-bold text-kumo-strong">导入主机备份配置</h3>
              <button onClick={() => setShowImportServerModal(false)} className="text-kumo-subtle font-bold cursor-pointer">×</button>
            </div>
            
            <div className="p-4 flex flex-col gap-4 text-xs">
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">选择备份 JSON 文件</label>
                <input
                  type="file"
                  onChange={e => {
                    const f = e.target.files[0];
                    if (f) processImportFile(f);
                  }}
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control"
                />
              </div>
              
              {importPreview && (
                <div className="bg-kumo-success/10 border border-kumo-success/20 p-2.5 rounded text-xs text-kumo-success font-bold">
                  ✓ 识别就绪：检测到 {importPreview.length} 个有效的服务器拓扑信息，确认开始恢复？
                </div>
              )}
              
              {importModalError && (
                <div className="text-xs text-kumo-danger font-bold bg-kumo-danger/10 border border-kumo-danger/20 p-2.5 rounded">
                  {importModalError}
                </div>
              )}
            </div>
            
            <div className="bg-kumo-recessed/25 px-4 py-3 border-t border-kumo-line flex justify-end gap-2 text-xs">
              <button onClick={() => setShowImportServerModal(false)} className="px-3.5 py-1.5 border border-kumo-line rounded-lg cursor-pointer">取消</button>
              <button
                onClick={confirmImportServers}
                disabled={importModalSaving || !importPreview}
                className="px-4 py-1.5 bg-kumo-brand text-kumo-inverse rounded-lg font-bold cursor-pointer disabled:opacity-50"
              >
                {importModalSaving ? '恢复中...' : '确认恢复导入'}
              </button>
            </div>
          </div>
        </div>
      )}
      
      {/* ==================== 模态框: SFTP 文件编辑器 ==================== */}
      {showSftpEditorModal && sftpEditFile && (
        <div className="fixed inset-0 z-[60] flex items-center justify-center p-4 bg-kumo-contrast/60 backdrop-blur-xs">
          <div className="bg-kumo-base border border-kumo-line rounded-lg w-full max-w-2xl h-[70vh] shadow-xl overflow-hidden flex flex-col text-xs text-kumo-default">
            <div className="flex items-center justify-between bg-kumo-recessed px-4 py-3 border-b border-kumo-line">
              <h3 className="font-bold">📄 在线编辑: {sftpEditFile.name}</h3>
              <button onClick={() => setShowSftpEditorModal(false)} className="text-kumo-subtle font-bold cursor-pointer">×</button>
            </div>
            
            <div className="p-4 flex-1 flex flex-col gap-2 bg-kumo-canvas">
              <div className="text-[10px] text-kumo-subtle font-mono truncate">{sftpEditFile.path}</div>
              <textarea
                value={sftpEditFile.content}
                onChange={e => setSftpEditFile(prev => ({ ...prev, content: e.target.value }))}
                className="flex-1 w-full p-2.5 bg-kumo-control border border-kumo-line rounded font-mono text-xs focus:outline-none focus:border-kumo-brand text-kumo-strong resize-none"
                spellCheck={false}
              ></textarea>
            </div>
            
            <div className="bg-kumo-recessed px-4 py-3 border-t border-kumo-line flex justify-end gap-2">
              <button onClick={() => setShowSftpEditorModal(false)} className="px-3.5 py-1.5 border border-kumo-line rounded cursor-pointer hover:bg-kumo-fill">取消</button>
              <button
                onClick={saveSftpEditedFile}
                disabled={sftpSaving}
                className="px-4 py-1.5 bg-kumo-brand text-kumo-inverse rounded font-bold cursor-pointer disabled:opacity-50"
              >
                {sftpSaving ? '正在写入保存...' : '保存文件'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default ServerPage;
