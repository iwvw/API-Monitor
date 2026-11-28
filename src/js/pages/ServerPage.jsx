import React, { useState, useEffect, useLayoutEffect, useRef, useMemo, useCallback } from 'react';
import { createPortal } from 'react-dom';
import useStore from '../store.js';
import { useShallow } from 'zustand/react/shallow';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import { ContextMenu } from '@cloudflare/kumo/primitives/context-menu';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { TagInput } from '@cloudflare/kumo/components/tag-input';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Popover } from '@cloudflare/kumo/components/popover';
import {
  ChartLegend,
  ChartPalette,
  ClipboardText,
  LayerCard,
  Loader,
  Meter,
  Tabs,
  Toolbar,
} from '@cloudflare/kumo';
import SiteFontTimeseriesChart from '../components/SiteFontTimeseriesChart.jsx';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AnimatedCollapse, DeferredRender } from '../components/AnimatedCollapse.jsx';
import CountryFlag from '../components/CountryFlag.jsx';
import QuickCommandBar from '../components/server/QuickCommandBar.jsx';
import SftpPanel from '../components/server/SftpPanel.jsx';
import ForwardPanel from '../components/forward/ForwardPanel.jsx';
import ServerLocationMap from '../components/server/ServerLocationMap.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
import { DateField } from '../components/ui/DateField.jsx';
import {
  ChartBoundaryBox,
  ChartWarmupSkeleton,
  AppCard,
  AppTable,
  ResponsiveSearchInput,
  ScrollableTable,
  SectionCard,
  stickyTabsBaseClass,
  TabBarOverflowActions,
  sectionCardHeaderClass,
} from '../components/ui/AppPrimitives.jsx';
import { PublicPageBrandIcon } from '../components/public/PublicPageIconPicker.jsx';
import {
  formatUptime,
  formatFileSize,
  formatDateTime,
  maskAddress,
  parseSpeed,
} from '../modules/utils.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import {
  canOpenTerminal,
  hasSshEndpoint,
  isAgentServer,
  isTerminalSocketDead,
  resolveTerminalProtocol,
  resolveTerminalSocketTransport,
  shouldDropPtyAttach,
  terminalReconnectDelay,
  TERMINAL_HEARTBEAT_MS,
} from '../modules/serverTerminal.js';
import { canOpenRemoteDesktop, remoteDesktopPath } from '../modules/remoteDesktop.js';
import { getSftpRoots, readSftpFile, writeSftpFile } from '../modules/server-sftp.js';
import { formatDockerContainerPorts } from '../modules/docker-format.js';
import { getOnepanelReverseProxyDomains } from '../modules/onepanel.js';
import { summarizeDockerContainers } from '../modules/dockerSummary.js';
import {
  formatDockerPruneResult,
  isDockerImagePruneCandidate,
  normalizeDockerTaskResult,
  summarizeDockerTaskMessage,
} from '../modules/dockerTasks.js';
import {
  buildAgentInstallCommand,
  buildAgentInstallEndpoint,
  getAgentInstallExecutionHint,
  isWindowsAgentInstallOs,
} from '../modules/agentInstall.js';
import {
  areRealtimeValuesEqual,
  mergePolledServerAccount,
  resolveServerDisplayStatus,
  mergeRealtimeDiskInfo,
  resolveServerMetricsHealth,
  resolveRealtimeMetricsCache,
  reuseRealtimeValueIfEqual,
} from '../modules/serverRealtime.js';
import {
  SERVER_CHART_HISTORY_LIMIT,
  SERVER_CHART_HISTORY_WINDOW_MS,
  SERVER_REALTIME_SAMPLE_INTERVAL_MS,
  formatSqliteUTCDateTime,
  getGpuMemPercent,
  normalizeChartMetricRecords,
  normalizeMetricRecords,
  toTimestamp,
} from '../modules/serverChartMetrics.js';
import * as echarts from 'echarts/core';
import { LineChart, MapChart, ScatterChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';
import io from 'socket.io-client';
import {
  Server,
  LayoutDashboard,
  Terminal as TerminalIcon,
  DesktopDisplay,
  Cloud,
  Globe,
  Activity,
  RefreshCw,
  ArrowRight,
  Box,
  Send,
  Shield,
  FolderOpen,
  HardDrive,
  Database,
  TrendingUp,
  Settings,
  Plus,
  Trash,
  Play,
  Pause,
  Square,
  Key,
  Folder,
  FileText,
  Save,
  RotateCw,
  Search,
  Copy,
  ExternalLink,
  Upload,
  Download,
  Edit,
  X,
  Reboot,
  ChevronDown,
  ChevronUp,
  Menu,
  Star,
  Shuffle,
  Columns,
  Grid,
  Eye,
  EyeOff,
  Maximize2,
} from '../components/Icons.jsx';
import TerminalWorkspaceContainer from '../components/server/TerminalWorkspaceContainer.jsx';
import {
  buildPresetLayout,
  createLeafNode,
  getTreeSessionIds,
  insertSplitIntoTree,
  normalizeTree,
  removeSessionFromTree,
  updateBranchRatio,
} from '../modules/terminalSplitTree.js';
import {
  SERVER_LIST_VIEW_STORAGE_KEY,
  SERVER_COMPACT_COLUMNS_STORAGE_KEY,
  SERVER_CARD_DRAG_STORAGE_KEY,
  SERVER_STATUS_SYNC_INTERVAL_MS,
  HOST_COMPACT_COLUMNS,
  HOST_COMPACT_COLUMN_IDS,
  HOST_COMPACT_COLUMN_WIDTHS,
  HOST_COMPACT_ADAPTIVE_COLUMNS,
  HOST_COMPACT_HEADER_BOX_CLASS,
  COMPACT_INLINE_BOX_CLASS,
  COMPACT_ACTION_BUTTON_CLASS,
  SERVER_SECTION_HEADER_CLASS,
  SERVER_SECONDARY_BAR_CLASS,
  SERVER_SECONDARY_TABS_GROUP_CLASS,
  COMPACT_EXPAND_EXIT_MS,
  SERVER_CHART_SERIES_DEFER_MS,
  SERVER_CHART_RENDER_DEFER_MS,
  SERVER_FAST_CHART_UPDATE_BEHAVIOR,
  SERVER_NETWORK_QUALITY_REFRESH_MS,
  SERVER_STATIC_CHART_ANIMATION_OPTIONS,
} from './server/constants.js';
import {
  createEmptyServerStatusPageForm,
  normalizeServerStatusSlug,
  normalizeServerStatusDomain,
  createFastTimeseriesEcharts,
  getInitialServerListViewMode,
  getInitialServerCardDragEnabled,
  getInitialCompactVisibleColumns,
  getOSIconClass,
  toNumber,
  clampPercent,
  getExpandedInfoGridClassName,
  getExpandedTrendGridClassName,
  getExpandedCardSpanClassName,
  cleanCountryDisplayCode,
  firstLocationText,
  firstLocationNumber,
  getFlagCountry,
  getServerLocationText,
  getServerLocationTitle,
  isPageVisible,
  getMetricsSocketUrl,
  areServerSnapshotsEqual,
  formatMetricTooltipValue,
  getMetricSeries,
  getLatestMetricValue,
  formatChartTime,
  formatCompactChartTime,
  formatPercentAxis,
  formatCompactPercentAxis,
  formatNumberAxis,
  formatCompactNumberAxis,
  formatBytesSpeed,
  formatBytesValue,
  formatCompactBytesSpeed,
  getGpuTemp,
  getCpuTemp,
  parseSpeedToBytes,
  parseMemoryUsagePercent,
} from './server/utils.js';
import { useMediaQuery } from './server/useMediaQuery.js';
import { ServerModuleTabLabel } from './server/ServerModuleTabLabel.jsx';
import { CompactMetricBar } from './server/CompactMetricBar.jsx';
import { DenseUsageMeter } from './server/DenseUsageMeter.jsx';
import { DenseLifecycleMeter } from './server/DenseLifecycleMeter.jsx';
import { ServerConnectionActions } from './server/ServerConnectionActions.jsx';
import { DenseTrafficCell } from './server/DenseTrafficCell.jsx';
import { DenseDetailChip } from './server/DenseDetailChip.jsx';
import { ExpandedSection } from './server/ExpandedSection.jsx';
import { ExpandedProgressMetric } from './server/ExpandedProgressMetric.jsx';
import { TrafficTotalSummary } from './server/TrafficTotalSummary.jsx';
import { ExpandedInfoChip } from './server/ExpandedInfoChip.jsx';
import { ExpandedStatTile } from './server/ExpandedStatTile.jsx';
import { TrendSeriesLabel } from './server/TrendSeriesLabel.jsx';
import { ExpandedTrendChartCard } from './server/ExpandedTrendChartCard.jsx';
import { NetworkQualityPanel } from './server/NetworkQualityPanel.jsx';
import { CompactExpandedRow } from './server/CompactExpandedRow.jsx';
import TileGrid from './tiles/TileGrid.jsx';
import { CompactColumnMenu } from './server/CompactColumnMenu.jsx';
import {
  DAY_MS,
  DOCKER_LOG_TAIL_ITEMS,
  DOCKER_STATS_HISTORY_LIMIT,
  DOCKER_STATS_MOCK_POINTS,
  EMPTY_METRIC_RECORDS,
  EMPTY_SERIES,
  EMPTY_SERVER_METRIC_DISPLAY,
  SERVER_CARD_METRICS_TTL_MS,
  SERVER_CHART_COALESCE_WINDOW_MS,
  SERVER_EXPAND_INTERACTION_GUARD_MS,
  SERVER_METRIC_FLUSH_DELAY_MS,
  SERVER_METRIC_MIN_RENDER_INTERVAL_MS,
  SERVER_NETWORK_QUALITY_MAX_POINTS,
  SERVER_NETWORK_QUALITY_TTL_MS,
  TRAFFIC_CYCLE_OPTIONS,
  TRAFFIC_QUOTA_UNITS,
  areServerValuesEqual,
  asArray,
  buildDockerPairSeries,
  buildDockerStatSeries,
  buildMetricHistoryRecord,
  buildNetworkQualitySeries,
  bytesToTrafficQuotaForm,
  createDockerStatsSeedHistory,
  createMockDockerOverview,
  formatComposeStatusLabel,
  formatDateInputValue,
  formatResponseTime,
  formatServerRemainingTime,
  formatUptimeDaysOnly,
  getByteParts,
  getCachedServerMetricHistory,
  getComposeConfigFileList,
  getComposeConfigFiles,
  getComposePrimaryConfigFile,
  getComposeProjectName,
  getComposeStatus,
  getComposeWorkingDir,
  getCycleByteParts,
  getDockerImageId,
  getDockerImageRepository,
  getDockerImageSize,
  getDockerImageTag,
  getDockerNetworkDriver,
  getDockerNetworkId,
  getDockerNetworkName,
  getDockerNetworkScope,
  getDockerOverviewResourceCount,
  getDockerOverviewScope,
  getDockerStatBlockIo,
  getDockerStatCpuPercent,
  getDockerStatKey,
  getDockerStatMemPercent,
  getDockerStatMemUsage,
  getDockerStatName,
  getDockerStatNetIo,
  getDockerTaskActionLabel,
  getDockerTaskStateLabel,
  getDockerVolumeDriver,
  getDockerVolumeName,
  getDockerVolumeScope,
  getGpuModelText,
  getHostAddress,
  getKumoTerminalTheme,
  getKumoToken,
  getPrimaryLoadValue,
  getServerCycleParts,
  getServerLifecycle,
  getServerMetricDisplay,
  getServerMonitorModeLabel,
  getServerRemainingToneClass,
  getTempColorClass,
  getTrafficQuota,
  hasServerDockerInstalled,
  isComposeConfigMissing,
  isDockerMockPreviewEnabled,
  isDockerOverviewHostVisible,
  isNetworkQualityUnsupportedError,
  isServerOnline,
  mergeServerMetricHistory,
  normalizeByteText,
  normalizeDockerOverviewServer,
  normalizeDockerStatSnapshot,
  normalizeDockerStringList,
  normalizeExpiryInputValue,
  normalizeStartInputValue,
  normalizeTrafficAlertPercentInput,
  normalizeTrafficCycleDayInput,
  parseDockerByteValue,
  parseDockerIoPair,
  parseGpuMemoryValue,
  resolveCycleBytes,
  serverMetricDisplayCache,
  serverMetricsHistoryCache,
  toDateMs,
  trafficQuotaInputToBytes
} from './server/serverHelpers.js';
import ServerModals from './server/ServerModals.jsx';
import ServerAdminPanel from './server/ServerAdminPanel.jsx';
import ServerOnepanelPanel from './server/ServerOnepanelPanel.jsx';
import ServerDockerConsole from './server/ServerDockerConsole.jsx';
import ServerHostTable from './server/ServerHostTable.jsx';
import ServerSshTerminal from './server/ServerSshTerminal.jsx';

echarts.use([
  LineChart,
  MapChart,
  ScatterChart,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
  CanvasRenderer,
  AriaComponent,
]);

function ServerPage() {
  const {
    setMainActiveTab,
    theme,
    publicApiUrl,
    serverIpDisplayMode: storedServerIpDisplayMode,
    agentDownloadUrl: storedAgentDownloadUrl,
  } = useStore(
    useShallow(s => ({
      setMainActiveTab: s.setMainActiveTab,
      theme: s.theme,
      publicApiUrl: s.publicApiUrl,
      serverIpDisplayMode: s.serverIpDisplayMode,
      agentDownloadUrl: s.agentDownloadUrl,
    }))
  );
  const { isArmed, confirmPress } = useConfirmPress();
  const isCompactViewport = useMediaQuery('(max-width: 640px)');
  const isDenseViewport = useMediaQuery('(max-width: 1120px)');
  const expandedMainChartHeight = isCompactViewport ? 88 : isDenseViewport ? 100 : 112;
  const expandedSubChartHeight = isCompactViewport ? 72 : isDenseViewport ? 80 : 88;
  const expandedTrendChartHeight = isCompactViewport ? 96 : isDenseViewport ? 108 : 120;
  const compactExpandedChartHeight = isCompactViewport ? 96 : isDenseViewport ? 112 : 122;
  const compactNetworkQualityChartHeight = isCompactViewport ? 104 : isDenseViewport ? 118 : 130;
  const networkQualityChartHeight = isCompactViewport ? 108 : isDenseViewport ? 132 : 148;
  const expandedChartXAxisTickCount = isCompactViewport ? 3 : isDenseViewport ? 4 : 5;
  const expandedChartYAxisTickCount = isCompactViewport ? 3 : 4;
  const compactExpandedYAxisTickCount = 3;
  const expandedChartXAxisTickFormat = isCompactViewport ? formatCompactChartTime : formatChartTime;
  const expandedPercentAxisTickFormat = isCompactViewport
    ? formatCompactPercentAxis
    : formatPercentAxis;
  const expandedNumberAxisTickFormat = isCompactViewport
    ? formatCompactNumberAxis
    : formatNumberAxis;
  const expandedSpeedAxisTickFormat = isCompactViewport
    ? formatCompactBytesSpeed
    : formatBytesSpeed;
  const fastTimeseriesEcharts = useMemo(() => createFastTimeseriesEcharts(echarts), []);
  const staticTimeseriesEcharts = useMemo(
    () => createFastTimeseriesEcharts(echarts, SERVER_STATIC_CHART_ANIMATION_OPTIONS),
    []
  );

  const [serverCurrentTab, setServerCurrentTab] = useState('list'); // 'list', 'status-pages', 'docker', 'management', 'terminal', 'forward'
  const forwardPanelRef = useRef(null);

  // 主机列表状态
  const [serverList, setServerList] = useState([]);
  // 供 socket 回调读取「当前最新的主机列表」：socket 的 connect 回调只建立一次，
  // 闭包里的 serverList 会一直是空数组，导致订阅集合永远为空。
  const serverListRef = useRef([]);
  // 订阅集合的指纹：主机增删后要重新下发，否则新加的主机永远收不到实时指标，
  // 删掉的主机则会让服务端一直为它做过滤。用逗号拼接的 id 列表当指纹，
  // 主机自身指标变化（每次上报都变）不会影响它，避免每 1.5 秒重发一次订阅。
  const subscriptionKeyRef = useRef('');
  useEffect(() => {
    serverListRef.current = serverList;
    const key = serverList
      .map(server => String(server?.id || ''))
      .filter(Boolean)
      .sort()
      .join(',');
    if (key === subscriptionKeyRef.current) return;
    subscriptionKeyRef.current = key;
    const socket = socketRef.current;
    if (socket && socket.connected) {
      socket.emit('metrics:subscribe', { serverIds: key ? key.split(',') : [] });
    }
  }, [serverList]);
  const [serverLoading, setServerLoading] = useState(false);
  const [serverStatusPages, setServerStatusPages] = useState([]);
  const [serverStatusPagesLoading, setServerStatusPagesLoading] = useState(false);
  const [serverStatusPageForm, setServerStatusPageForm] = useState(() =>
    createEmptyServerStatusPageForm()
  );
  const [serverSearchText, setServerSearchText] = useState('');
  const [serverStatusFilter, setServerStatusFilter] = useState('all');
  const [serverListViewMode, setServerListViewMode] = useState(getInitialServerListViewMode);
  const [serverCardDragEnabled, setServerCardDragEnabled] = useState(getInitialServerCardDragEnabled);
  const [cardDetailServerId, setCardDetailServerId] = useState(null);

  useEffect(() => {
    if (!cardDetailServerId) return undefined;
    const onKeyDown = event => {
      if (event.key !== 'Escape') return;
      setExpandedServers(prev => prev.filter(id => id !== cardDetailServerId));
      setCardDetailServerId(null);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [cardDetailServerId]);
  const [serverMapOpen, setServerMapOpen] = useState(false);
  const [compactVisibleColumns, setCompactVisibleColumns] = useState(
    getInitialCompactVisibleColumns
  );
  const [compactColumnMenu, setCompactColumnMenu] = useState({ open: false, x: 0, y: 0 });
  const [expandedServers, setExpandedServers] = useState([]);
  const [renderedCompactExpandedServers, setRenderedCompactExpandedServers] = useState([]);
  const [chartSeriesReadyServers, setChartSeriesReadyServers] = useState([]);
  const [expandedDockerPanels, setExpandedDockerPanels] = useState([]);

  // 凭据状态
  const [serverCredentials, setServerCredentials] = useState([]);

  // 拨测目标状态
  const [networkTargets, setNetworkTargets] = useState([]);
  const [showNetworkTargetModal, setShowNetworkTargetModal] = useState(false);
  const [networkTargetModalMode, setNetworkTargetModalMode] = useState('add'); // 'add' | 'edit'
  const [networkTargetForm, setNetworkTargetForm] = useState({
    id: null,
    name: '',
    host: '',
    port: 80,
    type: 'tcp',
    enabled: true,
    order_index: 0,
  });

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
    tags: [],
    description: '',
    startsAt: '',
    expiresAt: '',
    trafficLimitValue: '',
    trafficLimitUnit: 'TB',
    trafficLimitMode: 'total',
    trafficAlertEnabled: false,
    trafficAlertPercent: 100,
    trafficCycleType: 'none',
    trafficCycleDay: 1,
    trafficCycleStart: '',
    trafficCycleEnd: '',
    monitorMode: 'agent',
    remoteDesktopNatIps: '',
    remoteDesktopNatType: 'srflx',
  });
  const [selectedCredentialId, setSelectedCredentialId] = useState('');
  const [serverModalSaving, setServerModalSaving] = useState(false);

  const [serverAddMode, setServerAddMode] = useState('ssh'); // 'ssh' | 'agent'
  const [quickDeployName, setQuickDeployName] = useState('');
  const [quickDeployResult, setQuickDeployResult] = useState(null);
  const [agentInstallOS, setAgentInstallOS] = useState('linux');
  const [showAgentModal, setShowAgentModal] = useState(false);
  const [agentModalData, setAgentModalData] = useState(null);
  const [agentForceSsh, setAgentForceSsh] = useState(false);
  const [agentInstalling, setAgentInstalling] = useState(false);
  const [agentInstallLoading, setAgentInstallLoading] = useState(false);
  const [agentInstallLog, setAgentInstallLog] = useState('');
  const [agentInstallResult, setAgentInstallResult] = useState(null);
  const [showBatchAgentModal, setShowBatchAgentModal] = useState(false);
  const [selectedBatchServers, setSelectedBatchServers] = useState([]);
  const [batchInstallResults, setBatchInstallResults] = useState([]);
  const [batchAgentForceSsh, setBatchAgentForceSsh] = useState(false);

  // 导入/导出
  const [showImportServerModal, setShowImportServerModal] = useState(false);
  const [importPreview, setImportPreview] = useState(null);
  const [importModalError, setImportModalError] = useState('');
  const [importModalSaving, setImportModalSaving] = useState(false);
  const [serverBatchText, setServerBatchText] = useState('');
  const [serverBatchError, setServerBatchError] = useState('');
  const [serverBatchSuccess, setServerBatchSuccess] = useState('');
  const [serverAddingBatch, setServerAddingBatch] = useState(false);
  const [serverIpDisplayMode, setServerIpDisplayMode] = useState(
    storedServerIpDisplayMode || 'normal'
  ); // 'normal', 'masked', 'hidden'
  const [serverSettingsSaving, setServerSettingsSaving] = useState(false);
  const [serverSettingsForm, setServerSettingsForm] = useState({
    agentDownloadUrl: storedAgentDownloadUrl || '',
  });

  const [networkQualityByServer, setNetworkQualityByServer] = useState({});

  // Docker 状态
  const [dockerOverviewServers, setDockerOverviewServers] = useState([]);
  const [dockerReverseProxyDomains, setDockerReverseProxyDomains] = useState({});
  const [dockerOverviewLoading, setDockerOverviewLoading] = useState(false);
  const [dockerSubTab, setDockerSubTab] = useState('containers'); // 'containers', 'compose', 'images', 'networks', 'volumes', 'auto-update'
  const [dockerAutoUpdate, setDockerAutoUpdate] = useState(null);
  const [dockerAutoUpdateBusy, setDockerAutoUpdateBusy] = useState(false);
  const [dockerViewMode, setDockerViewMode] = useState('table'); // 'table', 'grid'
  const [dockerSearchQuery, setDockerSearchQuery] = useState('');
  const [dockerContainerStateFilter, setDockerContainerStateFilter] = useState('all');
  const [dockerSelectedServer, setDockerSelectedServer] = useState('');
  const [expandedDockerOverviewServers, setExpandedDockerOverviewServers] = useState(null);
  // 未手动展开/收起过任何主机区块（null = 自动模式）时，默认展开每个列表的第一台主机。
  const isDockerHostExpanded = useCallback(
    (serverId, firstVisibleServerId) =>
      expandedDockerOverviewServers === null
        ? firstVisibleServerId === serverId
        : expandedDockerOverviewServers.includes(serverId),
    [expandedDockerOverviewServers]
  );
  const [showDockerTaskDetails, setShowDockerTaskDetails] = useState(false);
  const [showDockerLogPanel, setShowDockerLogPanel] = useState(true);
  const [dockerTasks, setDockerTasks] = useState([]);
  const [dockerTaskStreamConnected, setDockerTaskStreamConnected] = useState(false);
  const [dockerTaskStreamError, setDockerTaskStreamError] = useState('');
  const [dockerResourceLoading, setDockerResourceLoading] = useState(false);
  const [dockerImages, setDockerImages] = useState([]);
  const [dockerNetworks, setDockerNetworks] = useState([]);
  const [dockerVolumes, setDockerVolumes] = useState([]);
  const [dockerStats, setDockerStats] = useState([]);
  const [dockerStatsHistory, setDockerStatsHistory] = useState([]);
  const [dockerComposeProjects, setDockerComposeProjects] = useState([]);
  const [dockerUpdateChecks, setDockerUpdateChecks] = useState({});
  const [dockerUpdateCheckLoading, setDockerUpdateCheckLoading] = useState({});
  const [dockerActionPending, setDockerActionPending] = useState({});
  const [dockerSelectedContainerKeys, setDockerSelectedContainerKeys] = useState([]);
  const [dockerBulkUpdateChecking, setDockerBulkUpdateChecking] = useState(false);
  const [dockerBulkUpdateCheckServers, setDockerBulkUpdateCheckServers] = useState({});
  const [showDockerCreateModal, setShowDockerCreateModal] = useState(false);
  const [dockerLogsModalOpen, setDockerLogsModalOpen] = useState(false);
  const [dockerLogsContent, setDockerLogsContent] = useState('');
  const [dockerLogsLoading, setDockerLogsLoading] = useState(false);
  const [dockerLogsContainer, setDockerLogsContainer] = useState(null);
  const [dockerLogsServer, setDockerLogsServer] = useState(null);
  const [dockerLogsTail, setDockerLogsTail] = useState(200);
  const [dockerComposeEditor, setDockerComposeEditor] = useState(null);

  useEffect(() => {
    if (dockerStats.length === 0) {
      setDockerStatsHistory([]);
      return;
    }

    const timestamp = Date.now();
    const nextSnapshot = dockerStats.map((stat, index) =>
      normalizeDockerStatSnapshot(stat, index, timestamp)
    );
    setDockerStatsHistory(prev => {
      if (prev.length === 0 && isDockerMockPreviewEnabled()) {
        return createDockerStatsSeedHistory(dockerStats, timestamp);
      }
      return [...prev, nextSnapshot].slice(-DOCKER_STATS_HISTORY_LIMIT);
    });
  }, [dockerStats]);

  // Agent 升级
  const [showUpgradeModal, setShowUpgradeModal] = useState(false);
  const [upgrading, setUpgrading] = useState(false);
  const [upgradeLog, setUpgradeLog] = useState('');
  const [upgradeBatchSnapshot, setUpgradeBatchSnapshot] = useState(null);
  const [upgradeProgress, setUpgradeProgress] = useState(0);
  const [upgradeFallbackSsh, setUpgradeFallbackSsh] = useState(false);

  // SSH 终端会话
  const [sshSessions, setSshSessions] = useState([]);
  const [activeSSHSessionId, setActiveSSHSessionId] = useState('');
  const [visibleSessionIds, setVisibleSessionIds] = useState([]);
  const [sshViewLayout, setSshViewLayout] = useState('single'); // 'single', 'split-h', 'split-v', 'master-stack', 'grid'
  const [sshSplitTree, setSshSplitTree] = useState(null); // 二叉分屏树模型
  const [sshSplitSide, setSshSplitSide] = useState('');
  const [sshGroupState, setSshGroupState] = useState(null);
  const [sshSyncEnabled, setSshSyncEnabled] = useState(false);
  const [draggedSessionId, setDraggedSessionId] = useState(null);

  const [activeTerminalSidebar, setActiveTerminalSidebar] = useState(null);
  const [sshIdeFullscreen, setSshIdeFullscreen] = useState(false);

  const [terminalStatusRefreshing, setTerminalStatusRefreshing] = useState(false);
  const [terminalIpMasked, setTerminalIpMasked] = useState(false);

  // SFTP 状态
  const [sftpCurrentPath, setSftpCurrentPath] = useState('/');
  const [sftpServerId, setSftpServerId] = useState('');

  // 凭据编辑/新增
  const [showAddCredentialModal, setShowAddCredentialModal] = useState(false);
  const [credForm, setCredForm] = useState({
    name: '',
    username: 'root',
    auth_type: 'password',
    password: '',
    private_key: '',
    passphrase: '',
  });

  // 终端持久化实例仓库与 WebSocket 连接引用
  const terminalDOMElements = useRef({});
  const sshSessionRefs = useRef({});
  const warehouseRef = useRef(null);
  const serverModalPortalRef = useRef(null);
  const dockerTaskStreamRef = useRef(null);
  const dockerResourcesRequestIdRef = useRef(0);
  const dockerRefreshTimerRef = useRef(null);
  const dockerTaskMetaRef = useRef({});
  const terminalResizeTimers = useRef({});
  const upgradeLogViewportRef = useRef(null);
  const upgradeLogLineKeysRef = useRef(new Set());
  const socketRef = useRef(null);
  const pendingMetricUpdatesRef = useRef([]);
  const metricFlushTimerRef = useRef(null);
  const cardMetricsRequestRef = useRef(new Map());
  const cardMetricsLoadedAtRef = useRef(new Map());
  const networkQualityLoadedAtRef = useRef(new Map());
  const serverListSyncInFlightRef = useRef(false);
  const networkQualityRequestRef = useRef(new Set());
  const serverStatusByIdRef = useRef(new Map());
  const expandedServersRef = useRef([]);
  const expandInteractionUntilRef = useRef(new Map());
  const visibleSessionIdsRef = useRef([]);
  const activeTerminalStatusServerIdRef = useRef('');
  const sshSyncEnabledRef = useRef(false);
  const sftpPathByServerRef = useRef({});

  const showServerStatusSidebar = activeTerminalSidebar === 'status';
  const showSftpSidebar = activeTerminalSidebar === 'sftp';
  const showCommandSidebar = activeTerminalSidebar === 'commands';

  useEffect(() => {
    setServerIpDisplayMode(storedServerIpDisplayMode || 'normal');
  }, [storedServerIpDisplayMode]);

  useEffect(() => {
    setServerSettingsForm(prev => ({
      ...prev,
      agentDownloadUrl: storedAgentDownloadUrl || '',
    }));
  }, [storedAgentDownloadUrl]);

  const getSettingsAuthHeaders = useCallback(
    () => ({
      'Content-Type': 'application/json',
    }),
    []
  );

  const handleServerIpDisplayModeChange = useCallback(value => {
    setServerIpDisplayMode(String(value));
  }, []);

  const saveServerModuleSettings = useCallback(async () => {
    const patch = {
      serverIpDisplayMode,
      agentDownloadUrl: String(serverSettingsForm.agentDownloadUrl || '').trim(),
    };
    setServerSettingsSaving(true);
    try {
      const response = await fetch('/api/settings', {
        method: 'PATCH',
        headers: getSettingsAuthHeaders(),
        body: JSON.stringify(patch),
      });
      const result = await response.json();
      if (!response.ok || !result.success) {
        throw new Error(result.error || '保存主机偏好失败');
      }
      useStore.setState(patch);
      toast.success('主机偏好已保存');
    } catch (error) {
      toast.error(error.message || '保存主机偏好失败');
    } finally {
      setServerSettingsSaving(false);
    }
  }, [getSettingsAuthHeaders, serverIpDisplayMode, serverSettingsForm.agentDownloadUrl]);

  useEffect(() => {
    visibleSessionIdsRef.current = visibleSessionIds;
  }, [visibleSessionIds]);

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(SERVER_LIST_VIEW_STORAGE_KEY, serverListViewMode);
    }
  }, [serverListViewMode]);

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(SERVER_CARD_DRAG_STORAGE_KEY, serverCardDragEnabled ? '1' : '0');
    }
  }, [serverCardDragEnabled]);

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(
        SERVER_COMPACT_COLUMNS_STORAGE_KEY,
        JSON.stringify(compactVisibleColumns)
      );
    }
  }, [compactVisibleColumns]);

  useEffect(() => {
    expandedServersRef.current = expandedServers;
    setRenderedCompactExpandedServers(prev => Array.from(new Set([...prev, ...expandedServers])));

    const timeout = setTimeout(() => {
      setRenderedCompactExpandedServers(prev => prev.filter(id => expandedServers.includes(id)));
    }, COMPACT_EXPAND_EXIT_MS);

    return () => clearTimeout(timeout);
  }, [expandedServers]);

  useEffect(() => {
    if (expandInteractionUntilRef.current.size === 0) return undefined;
    const timer = window.setInterval(() => {
      const now = Date.now();
      expandInteractionUntilRef.current.forEach((until, serverId) => {
        if (until > now) return;
        expandInteractionUntilRef.current.delete(serverId);
      });
    }, 250);

    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    setChartSeriesReadyServers(prev => prev.filter(id => expandedServers.includes(id)));
    if (expandedServers.length === 0) return undefined;

    const timeout = setTimeout(() => {
      setChartSeriesReadyServers(prev =>
        Array.from(
          new Set([...prev.filter(id => expandedServers.includes(id)), ...expandedServers])
        )
      );
    }, SERVER_CHART_SERIES_DEFER_MS);

    return () => clearTimeout(timeout);
  }, [expandedServers]);

  useEffect(() => {
    if (!compactColumnMenu.open) return undefined;
    const closeMenu = () => setCompactColumnMenu(prev => ({ ...prev, open: false }));
    const handleKeyDown = event => {
      if (event.key === 'Escape') closeMenu();
    };
    window.addEventListener('click', closeMenu);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('click', closeMenu);
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [compactColumnMenu.open]);

  useLayoutEffect(() => {
    if (serverCurrentTab !== 'terminal') return undefined;

    visibleSessionIdsRef.current = visibleSessionIds;
    if (typeof window === 'undefined') return undefined;

    let frameOne = 0;
    let frameTwo = 0;
    const restore = () => restoreVisibleTerminalSurfaces({ focus: true });
    frameOne = window.requestAnimationFrame(() => {
      restore();
      frameTwo = window.requestAnimationFrame(restore);
    });

    return () => {
      if (frameOne) window.cancelAnimationFrame(frameOne);
      if (frameTwo) window.cancelAnimationFrame(frameTwo);
    };
  }, [serverCurrentTab, visibleSessionIds, sshViewLayout, sshSplitTree, activeTerminalSidebar, sshIdeFullscreen]);

  useEffect(() => {
    if (serverCurrentTab === 'terminal') return undefined;
    const timer = setTimeout(() => saveTerminalsToWarehouse(), 0);
    return () => clearTimeout(timer);
  }, [serverCurrentTab]);

  useEffect(() => {
    if (showSftpSidebar && activeSSHSessionId) {
      syncSftpToSession(activeSSHSessionId);
    }
  }, [activeSSHSessionId, showSftpSidebar]);

  useEffect(() => {
    sshSyncEnabledRef.current = sshSyncEnabled;
  }, [sshSyncEnabled]);

  useEffect(() => {
    const el = upgradeLogViewportRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [upgradeLog]);

  useEffect(() => {
    const activeSession = sshSessions.find(session => session.id === activeSSHSessionId);
    activeTerminalStatusServerIdRef.current =
      serverCurrentTab === 'terminal' && showServerStatusSidebar && activeSession?.server?.id
        ? String(activeSession.server.id)
        : '';
  }, [activeSSHSessionId, serverCurrentTab, showServerStatusSidebar, sshSessions]);

  // 终端持久化实例仓库与 WebSocket 连接引用

  useEffect(() => {
    loadServerList();
    loadCredentials();
    if (isDockerMockPreviewEnabled()) {
      setServerCurrentTab('docker');
      setDockerSubTab('containers');
    }
    const connectTimer = setTimeout(() => {
      connectMetricsStream();
    }, 0);
    const serverListSyncTimer = setInterval(() => {
      if (document.hidden) return;
      loadServerList({ silent: true });
    }, SERVER_STATUS_SYNC_INTERVAL_MS);

    // 每 1500 毫秒同步刷新一次累积的指标更新，保证所有主机行同步更新，避免刷新参差不齐
    metricFlushTimerRef.current = setInterval(() => {
      if (pendingMetricUpdatesRef.current.length > 0) {
        const queued = pendingMetricUpdatesRef.current;
        pendingMetricUpdatesRef.current = [];
        handleMetricUpdateBatch(queued);
      }
    }, 1500);

    return () => {
      clearTimeout(connectTimer);
      clearInterval(serverListSyncTimer);
      if (socketRef.current) {
        socketRef.current.removeAllListeners();
        socketRef.current.disconnect();
        socketRef.current = null;
      }
      if (dockerTaskStreamRef.current) {
        dockerTaskStreamRef.current.close();
        dockerTaskStreamRef.current = null;
      }
      if (dockerRefreshTimerRef.current) {
        clearTimeout(dockerRefreshTimerRef.current);
        dockerRefreshTimerRef.current = null;
      }
      if (metricFlushTimerRef.current) {
        clearInterval(metricFlushTimerRef.current);
        metricFlushTimerRef.current = null;
      }
      pendingMetricUpdatesRef.current = [];
      Object.values(terminalResizeTimers.current).forEach(timer => clearTimeout(timer));
      terminalResizeTimers.current = {};

      Object.keys(sshSessionRefs.current).forEach(id => {
        const session = sshSessionRefs.current[id];
        // 先标记关闭并清掉重连定时器:否则 ws.close() 触发的 onclose 会走
        // scheduleReconnect,在组件卸载后继续建立新连接(并对已 dispose 的
        // terminal 调 writeln)。删除 refs 条目也让 scheduleReconnect/onopen
        // 的实例校验直接失败。
        session.closing = true;
        if (session.reconnectTimer) {
          clearTimeout(session.reconnectTimer);
          session.reconnectTimer = null;
        }
        delete sshSessionRefs.current[id];
        if (session.heartbeatInterval) clearInterval(session.heartbeatInterval);
        if (session.inputDisposable) session.inputDisposable.dispose();
        if (session.ws) session.ws.close();
        if (session.resizeObserver) session.resizeObserver.disconnect();
        if (session.terminal) session.terminal.dispose();
      });
    };
  }, []);

  useEffect(() => {
    serverStatusByIdRef.current = new Map(
      serverList.map(server => [String(server.id), server.status])
    );
  }, [serverList]);

  const dockerHostOptions = useMemo(
    () =>
      serverList
        .filter(hasServerDockerInstalled)
        .map(server => ({ value: String(server.id), label: server.name || server.id })),
    [serverList]
  );
  const dockerHostOptionKey = useMemo(
    () => dockerHostOptions.map(item => item.value).join('|'),
    [dockerHostOptions]
  );

  useEffect(() => {
    if (!dockerSelectedServer) return;
    const selectedId = String(dockerSelectedServer);
    const stillAvailable =
      dockerHostOptions.some(item => item.value === selectedId) ||
      dockerOverviewServers.some(server => String(server.id) === selectedId);
    if (!stillAvailable) {
      setDockerSelectedServer('');
    }
  }, [dockerHostOptions, dockerOverviewServers, dockerSelectedServer]);

  const mergeTerminalCapabilities = (server, agentOnline) => {
    const hasSshTransport =
      server.ssh_configured === true ||
      server.supports_ssh === true ||
      server.terminal_transports?.includes('ssh') ||
      hasSshEndpoint(server);
    const terminalTransports = agentOnline ? ['agent'] : [];
    if (hasSshTransport) terminalTransports.push('ssh');

    return {
      agent_online: agentOnline,
      terminal_transports: terminalTransports,
      preferred_terminal_transport: terminalTransports[0] || null,
    };
  };

  // 载入主机列表
  const loadServerList = async (options = {}) => {
    const { silent = false } = options;
    if (silent && serverListSyncInFlightRef.current) return;

    if (silent) serverListSyncInFlightRef.current = true;
    if (!silent) setServerLoading(true);
    try {
      const response = await fetch('/api/server/accounts', { cache: 'no-store' });
      const data = await response.json();
      if (data.success) {
        const accounts = Array.isArray(data.data) ? data.data : [];
        setServerList(prev => {
          const prevMap = new Map(prev.map(s => [s.id, s]));
          let changed = prev.length !== accounts.length;
          const updated = accounts.map(server => {
            const existing = prevMap.get(server.id);
            const cachedMetrics = existing?.metricsCache || getCachedServerMetricHistory(server.id);
            const next = mergePolledServerAccount(existing, server, {
              silent,
              cachedMetrics: cachedMetrics || null,
            });
            if (existing && areServerSnapshotsEqual(existing, next)) {
              return existing;
            }
            changed = true;
            return next;
          });
          if (!changed) return prev;
          return updated;
        });
        const onlineServerIds = accounts
          .filter(server => isServerOnline(server) || server.is_online === true)
          .map(server => server.id)
          .filter(Boolean);
        if (onlineServerIds.length > 0) {
          const prefetch = () => {
            onlineServerIds.forEach(serverId => {
              loadCardMetrics(serverId, { silent: true }).catch(error => {
                console.error('[Server] Failed to prefetch metric history:', error);
              });
            });
          };
          if (typeof window !== 'undefined' && typeof window.requestIdleCallback === 'function') {
            window.requestIdleCallback(prefetch, { timeout: 1500 });
          } else {
            setTimeout(prefetch, 0);
          }
        }
      }
    } catch (error) {
      console.error('加载主机列表失败:', error);
      if (!silent) toast.error('加载主机列表失败');
    } finally {
      if (silent) serverListSyncInFlightRef.current = false;
      if (!silent) setServerLoading(false);
    }
  };

  const refreshServerLocationsAndList = async () => {
    setServerLoading(true);
    try {
      const response = await fetch('/api/server/accounts/refresh-locations', {
        method: 'POST',
        cache: 'no-store',
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || data.success === false) {
        throw new Error(data.error || data.message || '刷新地理位置失败');
      }
      await loadServerList({ silent: true });
      const updated = Number(data.data?.updated ?? data.updated ?? 0);
      toast.success(updated > 0 ? `已刷新 ${updated} 台主机地理位置` : '地理位置已是最新缓存');
    } catch (error) {
      console.error('刷新地理位置失败:', error);
      toast.error(error.message || '刷新地理位置失败');
    } finally {
      setServerLoading(false);
    }
  };

  const loadServerStatusPages = async () => {
    setServerStatusPagesLoading(true);
    try {
      const response = await fetch('/api/server/status-pages');
      const data = await response.json();
      if (data.success && Array.isArray(data.data)) {
        setServerStatusPages(data.data);
      }
    } catch (error) {
      toast.error('载入主机状态页失败');
    } finally {
      setServerStatusPagesLoading(false);
    }
  };

  const getServerStatusPageBaseOrigin = () => {
    const configured = String(publicApiUrl || '')
      .trim()
      .replace(/\/+$/g, '');
    return configured || window.location.origin;
  };

  const getServerStatusPageUrl = (pageOrForm, mode = 'servers') => {
    const slug = normalizeServerStatusSlug(pageOrForm?.slug || pageOrForm?.title || 'servers');
    return `${getServerStatusPageBaseOrigin()}/${mode}/${encodeURIComponent(slug)}`;
  };

  const getServerStatusDomainUrl = pageOrForm => {
    const domain = normalizeServerStatusDomain(pageOrForm?.domain);
    return domain ? `https://${domain}` : '';
  };

  const copyServerStatusUrl = async value => {
    if (!value) {
      toast.warning('没有可复制的地址');
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
      toast.success('公开地址已复制');
    } catch (error) {
      toast.error('复制失败');
    }
  };

  const resetServerStatusPageForm = () =>
    setServerStatusPageForm(createEmptyServerStatusPageForm());

  const editServerStatusPage = page => {
    const config = page.config || {};
    setServerStatusPageForm({
      id: page.id,
      title: page.title || '',
      slug: page.slug || '',
      domain: page.domain || '',
      description: page.description || '',
      public: page.public !== false,
      hideHosts: config.hideHosts !== false,
      showTraffic: config.showTraffic !== false,
      showCharts: config.showCharts !== false,
      showOnDashboard: !!config.showOnDashboard,
      publicIconId: String(config.publicIconId || '').trim(),
      cacheSeconds: page.cacheSeconds || 300,
      serverIds: Array.isArray(page.serverIds) ? page.serverIds : [],
    });
  };

  const toggleServerStatusPageServer = (serverId, checked) => {
    setServerStatusPageForm(prev => {
      const ids = new Set(prev.serverIds);
      if (checked) ids.add(serverId);
      else ids.delete(serverId);
      return { ...prev, serverIds: Array.from(ids) };
    });
  };

  const saveServerStatusPage = async () => {
    const title = serverStatusPageForm.title.trim();
    if (!title) {
      toast.warning('请填写状态页名称');
      return;
    }
    if (serverStatusPageForm.serverIds.length === 0) {
      toast.warning('请至少绑定一台主机');
      return;
    }
    setServerStatusPagesLoading(true);
    try {
      const payload = {
        title,
        slug: normalizeServerStatusSlug(serverStatusPageForm.slug || title),
        domain: normalizeServerStatusDomain(serverStatusPageForm.domain),
        description: serverStatusPageForm.description.trim(),
        public: !!serverStatusPageForm.public,
        cacheSeconds: Math.max(30, Number(serverStatusPageForm.cacheSeconds) || 300),
        serverIds: serverStatusPageForm.serverIds,
        config: {
          hideHosts: !!serverStatusPageForm.hideHosts,
          showTraffic: !!serverStatusPageForm.showTraffic,
          showCharts: !!serverStatusPageForm.showCharts,
          showOnDashboard: !!serverStatusPageForm.showOnDashboard,
          ...(serverStatusPageForm.publicIconId
            ? { publicIconId: serverStatusPageForm.publicIconId }
            : {}),
        },
      };
      const isEdit = !!serverStatusPageForm.id;
      const response = await fetch(
        isEdit ? `/api/server/status-pages/${serverStatusPageForm.id}` : '/api/server/status-pages',
        {
          method: isEdit ? 'PUT' : 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        }
      );
      const data = await response.json();
      if (!response.ok || data.success === false)
        throw new Error(data.error || '保存主机状态页失败');
      toast.success(isEdit ? '主机状态页已更新' : '主机状态页已创建');
      resetServerStatusPageForm();
      await loadServerStatusPages();
    } catch (error) {
      toast.error(error.message || '保存主机状态页失败');
    } finally {
      setServerStatusPagesLoading(false);
    }
  };

  const deleteServerStatusPage = async page => {
    if (
      !confirmPress(
        `status-page.delete::${page.id}`,
        `删除主机状态页「${page.title || page.slug}」`
      )
    )
      return;
    setServerStatusPagesLoading(true);
    try {
      const response = await fetch(`/api/server/status-pages/${page.id}`, { method: 'DELETE' });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || data.success === false)
        throw new Error(data.error || '删除主机状态页失败');
      toast.success('主机状态页已删除');
      if (serverStatusPageForm.id === page.id) resetServerStatusPageForm();
      await loadServerStatusPages();
    } catch (error) {
      toast.error(error.message || '删除主机状态页失败');
    } finally {
      setServerStatusPagesLoading(false);
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

  const loadNetworkTargets = async () => {
    try {
      const response = await fetch('/api/server/network-quality/targets');
      const data = await response.json();
      if (data.success) {
        setNetworkTargets(data.data || []);
      }
    } catch (e) {
      console.error('加载拨测目标失败:', e);
      toast.show({ message: '加载拨测目标失败', variant: 'error' });
    }
  };

  const saveNetworkTarget = async e => {
    e?.preventDefault();
    try {
      const url =
        networkTargetModalMode === 'add'
          ? '/api/server/network-quality/targets'
          : `/api/server/network-quality/targets/${networkTargetForm.id}`;
      const method = networkTargetModalMode === 'add' ? 'POST' : 'PUT';
      const response = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(networkTargetForm),
      });
      const data = await response.json();
      if (data.success) {
        toast.show({
          message: networkTargetModalMode === 'add' ? '新增成功' : '保存成功',
          variant: 'success',
        });
        setShowNetworkTargetModal(false);
        loadNetworkTargets();
      } else {
        toast.show({ message: data.message || '操作失败', variant: 'error' });
      }
    } catch (err) {
      console.error('保存拨测目标失败:', err);
      toast.show({ message: '保存拨测目标失败', variant: 'error' });
    }
  };

  const deleteNetworkTarget = async id => {
    if (!confirmPress(`network-target.delete::${id}`, '删除网络拨测目标')) {
      return;
    }
    try {
      const response = await fetch(`/api/server/network-quality/targets/${id}`, {
        method: 'DELETE',
      });
      const data = await response.json();
      if (data.success) {
        toast.show({ message: '删除成功', variant: 'success' });
        loadNetworkTargets();
      } else {
        toast.show({ message: data.message || '删除失败', variant: 'error' });
      }
    } catch (err) {
      console.error('删除拨测目标失败:', err);
      toast.show({ message: '删除拨测目标失败', variant: 'error' });
    }
  };

  const toggleNetworkTargetEnabled = async (target, nextEnabled) => {
    try {
      const updated = {
        name: target.name,
        host: target.host,
        port: target.port,
        type: target.type,
        enabled: typeof nextEnabled === 'boolean' ? nextEnabled : !target.enabled,
        order_index: target.order_index ?? 0,
      };
      const response = await fetch(`/api/server/network-quality/targets/${target.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(updated),
      });
      const data = await response.json();
      if (data.success) {
        loadNetworkTargets();
      } else {
        toast.show({ message: data.message || '操作失败', variant: 'error' });
      }
    } catch (err) {
      console.error('更新目标状态失败:', err);
      toast.show({ message: '更新目标状态失败', variant: 'error' });
    }
  };

  useEffect(() => {
    if (serverCurrentTab === 'management') {
      loadNetworkTargets();
    }
  }, [serverCurrentTab]);

  const applyServerStatusSnapshot = (items = []) => {
    if (!Array.isArray(items) || items.length === 0) return;

    const statusMap = new Map();
    items.forEach(item => {
      const id = item?.serverId ?? item?.id;
      if (id !== undefined && id !== null) {
        statusMap.set(String(id), item);
      }
    });

    if (statusMap.size === 0) return;

    setServerList(prev => {
      let changed = false;
      const updated = prev.map(server => {
        const item = statusMap.get(String(server.id));
        if (!item) return server;

        const rawStatus = item.status || (item.agent_online === true ? 'online' : 'offline');
        const status = rawStatus === 'suspect' ? 'interrupted' : rawStatus;
        const agentOnline =
          typeof item.agent_online === 'boolean' ? item.agent_online : status === 'online';
        const hasResponseTime =
          Object.prototype.hasOwnProperty.call(item, 'responseTime') ||
          Object.prototype.hasOwnProperty.call(item, 'response_time');
        const responseTime = Object.prototype.hasOwnProperty.call(item, 'responseTime')
          ? item.responseTime
          : item.response_time;

        const next = {
          ...server,
          ...mergeTerminalCapabilities(server, agentOnline),
          status,
          metrics_health: item.metrics_health || server.metrics_health,
          metrics_stale: item.metrics_stale ?? server.metrics_stale ?? false,
          metrics_last_seen:
            item.metrics_last_seen || item.metricsLastSeen || server.metrics_last_seen,
          metrics_last_seen_at:
            item.metrics_last_seen_at || item.metricsLastSeenAt || server.metrics_last_seen_at || 0,
          metrics_age_ms: item.metrics_age_ms ?? item.metricsAgeMs ?? server.metrics_age_ms ?? 0,
          response_time: hasResponseTime ? responseTime : server.response_time,
          error: status === 'offline' ? item.error || null : null,
          last_seen: item.lastSeen || item.last_seen || server.last_seen,
        };
        if (areServerSnapshotsEqual(server, next)) {
          return server;
        }
        changed = true;
        return next;
      });
      if (!changed) return prev;
      return updated;
    });
  };

  const connectMetricsStream = () => {
    try {
      const socket = io(getMetricsSocketUrl(), {
        reconnection: true,
        reconnectionDelay: 1000,
        reconnectionDelayMax: 5000,
        reconnectionAttempts: Infinity,
        transports: ['websocket', 'polling'],
      });

      // 订阅当前看板上的主机。
      //
      // 服务端以前把每台主机的指标推给所有客户端：100 台 × N 个标签页时，
      // 后端每秒要序列化 N×100 次，每个浏览器还都在收与自己无关的流量。
      // 订阅后只推关心的那几台。连接（含重连）成功后都要重新订阅 ——
      // 服务端不保存订阅状态，断线即丢。
      const sendSubscription = () => {
        // 直接读最新的 serverList（不是 effect 闭包里的旧值），
        // 否则主机列表异步加载完成后订阅集合仍是空的。
        const ids = (serverListRef.current || [])
          .map(server => String(server?.id || ''))
          .filter(Boolean);
        // 同步指纹，避免紧接着的 serverList effect 又重复发一次同样的订阅。
        subscriptionKeyRef.current = [...ids].sort().join(',');
        socket.emit('metrics:subscribe', { serverIds: ids });
      };
      socket.on('connect', sendSubscription);

      socket.on('metrics:update', data => {
        if (data && data.serverId && data.metrics) {
          enqueueMetricUpdates([data]);
        }
      });

      socket.on('metrics:batch', dataArray => {
        if (Array.isArray(dataArray)) {
          enqueueMetricUpdates(dataArray);
        }
      });

      socket.on('server:status', data => {
        if (data && data.serverId) {
          applyServerStatusSnapshot([data]);
        }
      });

      socket.on('server:list', data => {
        applyServerStatusSnapshot(data);
      });

      socketRef.current = socket;
    } catch (err) {
      console.error('[Metrics] Socket.IO 初始化失败:', err);
    }
  };

  const enqueueMetricUpdates = (updates = []) => {
    const validUpdates = updates.filter(data => data && data.serverId && data.metrics);
    if (validUpdates.length === 0) return;

    pendingMetricUpdatesRef.current.push(...validUpdates);
  };

  // 处理实时推送的主机指标
  const handleMetricUpdateBatch = (updates = []) => {
    const latestByServer = new Map();
    updates.forEach(update => {
      if (!update?.serverId || !update?.metrics) return;
      const key = String(update.serverId);
      const current = latestByServer.get(key);
      const ts = toTimestamp(update.timestamp, Date.now());
      if (!current || ts >= (current.timestamp || 0)) {
        latestByServer.set(key, { ...update, timestamp: ts });
      }
    });
    if (latestByServer.size === 0) return;

    if (!isPageVisible()) {
      latestByServer.forEach(data => {
        const serverId = data.serverId;
        const now = data.timestamp || Date.now();
        const historyRecord = buildMetricHistoryRecord(data.metrics, null, now);
        mergeServerMetricHistory(serverId, getCachedServerMetricHistory(serverId) || [], [
          historyRecord,
        ]);
      });
      return;
    }

    setServerList(prev => {
      let changed = false;
      const updated = prev.map(server => {
        const data = latestByServer.get(String(server.id));
        if (!data) return server;

        const { serverId, metrics, timestamp } = data;
        const now = timestamp || Date.now();
        // 新鲜度与节流一律锚定浏览器本地时钟，避免与服务端绝对时间戳跨时钟相减。
        const receivedAt = Date.now();
        const historyRecord = buildMetricHistoryRecord(metrics, server.info || null, now);
        mergeServerMetricHistory(serverId, getCachedServerMetricHistory(serverId) || [], [
          historyRecord,
        ]);

        const lastUpdate = server.lastMetricUpdateTime || 0;
        const isTerminalStatusVisible =
          activeTerminalStatusServerIdRef.current === String(server.id);
        const isExpanded =
          expandedServersRef.current.includes(server.id) || isTerminalStatusVisible;
        const interactionGuardUntil = expandInteractionUntilRef.current.get(String(server.id)) || 0;
        const inExpandInteractionGuard = interactionGuardUntil > receivedAt;
        if (
          lastUpdate > 0 &&
          receivedAt - lastUpdate < SERVER_METRIC_MIN_RENDER_INTERVAL_MS
        ) {
          if (!isExpanded) return server;
          if (!isTerminalStatusVisible && receivedAt - lastUpdate < SERVER_REALTIME_SAMPLE_INTERVAL_MS)
            return server;
        }

        const existingCache = server.metricsCache || getCachedServerMetricHistory(serverId) || [];
        const cache = mergeServerMetricHistory(
          serverId,
          getCachedServerMetricHistory(serverId) || [],
          existingCache,
          [historyRecord]
        );

        const previousInfo = server.info || {
          cpu: { Load: '-', Usage: '0%', Cores: '-' },
          memory: { Used: '-', Total: '-', Usage: '0%' },
          disk: [{ device: '/', used: '-', total: '-', usage: '0%' }],
          network: {
            connections: 0,
            rx_speed: '0 B/s',
            tx_speed: '0 B/s',
            rx_total: '0 B',
            tx_total: '0 B',
          },
          docker: { installed: false, containers: [] },
        };
        const info = { ...previousInfo };

        // CPU
        const logicalCores =
          parseInt(metrics.logical_cores) ||
          parseInt(metrics.cores) ||
          parseInt(previousInfo.cpu?.LogicalCores) ||
          parseInt(previousInfo.cpu?.Cores) ||
          0;
        const physicalCores =
          parseInt(metrics.physical_cores) ||
          parseInt(previousInfo.cpu?.PhysicalCores) ||
          logicalCores ||
          0;
        const metricCpu = metrics.cpu && typeof metrics.cpu === 'object' ? metrics.cpu : {};
        const existingCpu =
          previousInfo.cpu && typeof previousInfo.cpu === 'object' ? previousInfo.cpu : {};
        const resolvedCpuTemp = getCpuTemp({ ...metrics, cpu: { ...existingCpu, ...metricCpu } });
        info.cpu = reuseRealtimeValueIfEqual(previousInfo.cpu, {
          Model: metrics.cpu_model || metricCpu.Model || existingCpu.Model || '',
          Load: metrics.load || '-',
          Usage: metrics.cpu_usage || '0%',
          Cores: logicalCores || previousInfo.cpu?.Cores || '-',
          LogicalCores:
            logicalCores || previousInfo.cpu?.LogicalCores || previousInfo.cpu?.Cores || '-',
          PhysicalCores:
            physicalCores || previousInfo.cpu?.PhysicalCores || previousInfo.cpu?.Cores || '-',
          Temp: resolvedCpuTemp > 0 ? resolvedCpuTemp : previousInfo.cpu?.Temp || 0,
          Power: metrics.cpu_power || metrics.cpu_power_w || previousInfo.cpu?.Power || '',
        });

        // Memory
        if (metrics.mem_usage) {
          const memMatch = String(metrics.mem_usage).match(/(\d+)\/(\d+)MB/);
          if (memMatch) {
            const used = parseInt(memMatch[1]);
            const total = parseInt(memMatch[2]);
            info.memory = reuseRealtimeValueIfEqual(previousInfo.memory, {
              Used: used + ' MB',
              Total: total + ' MB',
              Usage: Math.round((used / total) * 100) + '%',
            });
          }
        }

        // Disk
        if (metrics.disk_usage !== undefined && metrics.disk_usage !== null) {
          info.disk = mergeRealtimeDiskInfo(previousInfo.disk, metrics);
        } else if (metrics.disk_used !== undefined || metrics.disk_total !== undefined) {
          info.disk = mergeRealtimeDiskInfo(previousInfo.disk, {
            ...metrics,
            disk_usage: metrics.disk_usage ?? previousInfo.disk?.[0]?.usage ?? '0%',
          });
        } else {
          info.disk = previousInfo.disk;
        }

        // Network
        if (metrics.network) {
          info.network = reuseRealtimeValueIfEqual(previousInfo.network, {
            ...previousInfo.network,
            ...metrics.network,
          });
        }

        // GPU
        if (
          metrics.gpu !== undefined ||
          metrics.gpu_usage !== undefined ||
          metrics.gpu_mem !== undefined ||
          metrics.gpu_mem_percent !== undefined ||
          metrics.gpu_model !== undefined ||
          metrics.gpu_power !== undefined ||
          metrics.gpu_temp !== undefined
        ) {
          const existingGpu = info.gpu || {};
          const pushedGpu =
            typeof metrics.gpu === 'object' && metrics.gpu !== null ? metrics.gpu : {};
          const pushedGpuUsage =
            typeof metrics.gpu === 'number' ? `${metrics.gpu.toFixed(1)}%` : undefined;
          const pushedGpuPercent =
            pushedGpu.Percent !== undefined
              ? pushedGpu.Percent
              : metrics.gpu_mem_percent !== undefined ||
                  metrics.gpu_mem_used !== undefined ||
                  metrics.gpu_mem_total !== undefined
                ? getGpuMemPercent(metrics)
                : existingGpu.Percent;
          const resolvedGpuUsage =
            pushedGpu.Usage ||
            (typeof metrics.gpu_usage === 'number'
              ? `${metrics.gpu_usage.toFixed(1)}%`
              : metrics.gpu_usage) ||
            pushedGpuUsage ||
            existingGpu.Usage ||
            '0%';
          info.gpu = reuseRealtimeValueIfEqual(previousInfo.gpu, {
            Model: pushedGpu.Model || metrics.gpu_model || existingGpu.Model || '',
            Usage: resolvedGpuUsage,
            Memory: pushedGpu.Memory || metrics.gpu_mem || existingGpu.Memory || '',
            Power: pushedGpu.Power || metrics.gpu_power || existingGpu.Power || '',
            Temp:
              pushedGpu.Temp !== undefined
                ? pushedGpu.Temp
                : metrics.gpu_temp !== undefined
                  ? metrics.gpu_temp
                  : existingGpu.Temp || 0,
            Percent: pushedGpuPercent !== undefined ? pushedGpuPercent : 0,
          });
        }

        // Docker
        if (metrics.docker) {
          info.docker = reuseRealtimeValueIfEqual(previousInfo.docker, {
            installed: !!metrics.docker.installed,
            runningCount: metrics.docker.running || 0,
            stoppedCount: metrics.docker.stopped || 0,
            containers: Array.isArray(metrics.docker.containers) ? metrics.docker.containers : [],
          });
        }

        info.platform = metrics.platform || previousInfo.platform;
        info.platformVersion = metrics.platformVersion || previousInfo.platformVersion;
        info.uptime = metrics.uptime || previousInfo.uptime;
        info.country_code = cleanCountryDisplayCode(
          firstLocationText(
            metrics.country_code,
            metrics.country,
            previousInfo.country_code,
            previousInfo.countryCode
          )
        );
        info.countryCode = info.country_code || cleanCountryDisplayCode(previousInfo.countryCode);
        info.location = firstLocationText(
          metrics.location,
          metrics.resolved_country,
          metrics.region,
          previousInfo.location
        );
        info.region = firstLocationText(metrics.region, previousInfo.region);
        const infoLatitude = firstLocationNumber(
          metrics.latitude,
          metrics.lat,
          previousInfo.latitude,
          previousInfo.lat
        );
        const infoLongitude = firstLocationNumber(
          metrics.longitude,
          metrics.lon,
          previousInfo.longitude,
          previousInfo.lon
        );
        if (infoLatitude !== undefined && !(infoLatitude === 0 && infoLongitude === 0)) {
          info.latitude = infoLatitude;
        }
        if (infoLongitude !== undefined && !(infoLatitude === 0 && infoLongitude === 0)) {
          info.longitude = infoLongitude;
        }

        const nextInfo = reuseRealtimeValueIfEqual(server.info, info);
        const nextMetricsCache = resolveRealtimeMetricsCache(server.metricsCache, cache, {
          isExpanded,
        });
        if (inExpandInteractionGuard) return server;
        const nextCountryCode = cleanCountryDisplayCode(
          firstLocationText(
            metrics.country_code,
            metrics.country,
            server.countryCode,
            server.country_code
          )
        );
        const nextLocation = firstLocationText(
          metrics.location,
          metrics.resolved_country,
          metrics.region,
          server.location
        );
        const nextLatitude = firstLocationNumber(metrics.latitude, metrics.lat, server.latitude);
        const nextLongitude = firstLocationNumber(metrics.longitude, metrics.lon, server.longitude);
        const nextServer = {
          ...server,
          ...mergeTerminalCapabilities(server, true),
          info: nextInfo,
          countryCode: nextCountryCode || server.countryCode,
          location: nextLocation || server.location,
          region: firstLocationText(metrics.region, server.region),
          latitude:
            nextLatitude === 0 && nextLongitude === 0
              ? server.latitude
              : (nextLatitude ?? server.latitude),
          longitude:
            nextLatitude === 0 && nextLongitude === 0
              ? server.longitude
              : (nextLongitude ?? server.longitude),
          status: 'online',
          error: null,
          metricsCache: nextMetricsCache,
          metrics_health: 'fresh',
          metrics_stale: false,
          metrics_last_seen: new Date(receivedAt).toISOString(),
          metrics_last_seen_at: receivedAt,
          metrics_age_ms: 0,
          metrics_received_at: receivedAt,
          metrics_received_age_ms: 0,
          lastMetricUpdateTime: server.lastMetricUpdateTime || 0,
        };
        if (areServerSnapshotsEqual(server, nextServer)) {
          return server;
        }

        changed = true;
        return {
          ...nextServer,
          lastMetricUpdateTime: receivedAt,
        };
      });
      if (!changed) return prev;
      return updated;
    });
  };

  const loadCardMetrics = async (serverId, options = {}) => {
    const { silent = false, force = false } = options;
    const requestKey = String(serverId);
    const cached = getCachedServerMetricHistory(serverId);
    const loadedAt = cardMetricsLoadedAtRef.current.get(requestKey) || 0;
    if (!force && silent && cached?.length && Date.now() - loadedAt < SERVER_CARD_METRICS_TTL_MS) {
      return cached;
    }
    const inFlight = cardMetricsRequestRef.current.get(requestKey);
    if (inFlight) return inFlight;

    setServerList(prev => {
      let changed = false;
      const updated = prev.map(s => {
        if (s.id !== serverId) return s;
        const next = {
          ...s,
          metricsCache: s.metricsCache || cached || null,
          metricsLoading: silent && (s.metricsCache?.length || cached?.length) ? false : true,
          error: silent ? s.error : null,
        };
        if (areServerSnapshotsEqual(s, next)) return s;
        changed = true;
        return next;
      });
      return changed ? updated : prev;
    });

    const request = (async () => {
      const now = Date.now();
      const historyStart = now - SERVER_CHART_HISTORY_WINDOW_MS;
      const params = new URLSearchParams({
        serverId,
        page: 1,
        pageSize: SERVER_CHART_HISTORY_LIMIT,
        startTime: formatSqliteUTCDateTime(historyStart),
        endTime: formatSqliteUTCDateTime(now),
        highPrecision: 'true',
      });
      const response = await fetch(`/api/server/metrics/history?${params}`);
      const data = await response.json();
      if (!data.success) {
        throw new Error(data.error || '指标历史加载失败');
      }
      const sorted = [...(data.data || [])].sort(
        (a, b) => new Date(a.recorded_at) - new Date(b.recorded_at)
      );
      const merged = mergeServerMetricHistory(
        serverId,
        { records: sorted, minTimestamp: historyStart },
        getCachedServerMetricHistory(serverId) || []
      );
      cardMetricsLoadedAtRef.current.set(requestKey, Date.now());
      setServerList(prev => {
        let changed = false;
        const updated = prev.map(s => {
          if (s.id !== serverId) return s;
          const next = { ...s, metricsCache: merged, metricsLoading: false, error: null };
          if (areServerSnapshotsEqual(s, next)) return s;
          changed = true;
          return next;
        });
        return changed ? updated : prev;
      });
      return merged;
    })();

    cardMetricsRequestRef.current.set(requestKey, request);
    try {
      return await request;
    } catch (e) {
      setServerList(prev => {
        let changed = false;
        const updated = prev.map(s => {
          if (s.id !== serverId) return s;
          const next = { ...s, metricsLoading: false, error: silent ? s.error : e.message };
          if (areServerSnapshotsEqual(s, next)) return s;
          changed = true;
          return next;
        });
        return changed ? updated : prev;
      });
      return [];
    } finally {
      cardMetricsRequestRef.current.delete(requestKey);
    }
  };

  const loadNetworkQuality = useCallback(async (serverId, options = {}) => {
    const { silent = false, collect = false } = options;
    const serverKey = String(serverId);
    const requestKey = `${serverKey}:${collect ? 'collect' : 'read'}`;
    const loadedAt = networkQualityLoadedAtRef.current.get(serverKey) || 0;
    if (!collect && silent && Date.now() - loadedAt < SERVER_NETWORK_QUALITY_TTL_MS) {
      return null;
    }
    if (networkQualityRequestRef.current.has(requestKey)) {
      return null;
    }
    networkQualityRequestRef.current.add(requestKey);

    setNetworkQualityByServer(prev => {
      const next = {
        ...(prev[serverId] || {}),
        loading: !silent || !(prev[serverId]?.sampleCount > 0),
        error: null,
      };
      if (areServerValuesEqual(prev[serverId], next)) return prev;
      return {
        ...prev,
        [serverId]: next,
      };
    });

    try {
      const params = new URLSearchParams({
        hours: '24',
        maxPointsPerTarget: String(SERVER_NETWORK_QUALITY_MAX_POINTS),
      });
      const response = await fetch(
        collect
          ? `/api/server/network-quality/${serverId}/collect`
          : `/api/server/network-quality/${serverId}?${params}`,
        collect
          ? {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                hours: 24,
                maxPointsPerTarget: SERVER_NETWORK_QUALITY_MAX_POINTS,
              }),
            }
          : undefined
      );
      const data = await response.json();
      if (!data.success) {
        throw new Error(data.error || '网络质量数据加载失败');
      }

      const quality = data.data || {};
      networkQualityLoadedAtRef.current.set(serverKey, Date.now());
      setNetworkQualityByServer(prev => {
        const next = {
          ...quality,
          loading: false,
          error: null,
        };
        if (areServerValuesEqual(prev[serverId], next)) return prev;
        return {
          ...prev,
          [serverId]: next,
        };
      });
      return quality;
    } catch (error) {
      setNetworkQualityByServer(prev => ({
        ...prev,
        [serverId]: {
          ...(prev[serverId] || {}),
          loading: false,
          error: error.message,
        },
      }));
      return null;
    } finally {
      networkQualityRequestRef.current.delete(requestKey);
    }
  }, []);

  useEffect(() => {
    if (expandedServers.length === 0) return undefined;

    const timer = setInterval(() => {
      if (!isPageVisible()) return;
      expandedServers.forEach(serverId => {
        if (serverStatusByIdRef.current.get(String(serverId)) !== 'online') return;
        loadNetworkQuality(serverId, { silent: true });
      });
    }, SERVER_NETWORK_QUALITY_REFRESH_MS);

    return () => clearInterval(timer);
  }, [expandedServers, loadNetworkQuality]);

  const loadServerInfo = async (serverId, options = {}) => {
    const { force = false, silent = false } = options;
    if (!silent) {
      setServerList(prev =>
        prev.map(s => (s.id === serverId ? { ...s, loading: true, error: null } : s))
      );
    }

    try {
      const response = await fetch('/api/server/info', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverId, force }),
      });
      const data = await response.json();
      if (!data.success) {
        throw new Error(data.error || data.message || '主机详情加载失败');
      }
      const info = data.data || data;
      setServerList(prev =>
        prev.map(s => (s.id === serverId ? { ...s, info, loading: false, error: null } : s))
      );

      if (data.is_cached && !force) {
        setTimeout(() => loadServerInfo(serverId, { force: true, silent: true }), 300);
      }
      return info;
    } catch (e) {
      setServerList(prev =>
        prev.map(s => (s.id === serverId ? { ...s, loading: false, error: e.message } : s))
      );
      return null;
    }
  };

  const refreshServerInfo = async serverId => {
    const server = serverList.find(s => s.id === serverId);
    if (!server || server.loading) return;

    const waitForMetricsFlush = async (delayMs = 650) => {
      if (typeof window === 'undefined') {
        return new Promise(resolve => setTimeout(resolve, delayMs));
      }
      return new Promise(resolve => window.setTimeout(resolve, delayMs));
    };

    const [, firstHistory] = await Promise.all([
      loadServerInfo(serverId, { force: true }),
      loadCardMetrics(serverId, { silent: true, force: true }),
      loadNetworkQuality(serverId, { silent: true, collect: true }),
    ]);

    if (!firstHistory || firstHistory.length < 3) {
      await waitForMetricsFlush();
      await loadCardMetrics(serverId, { silent: true, force: true });
    }

    toast.success('主机详情已刷新');
  };

  // 切换折叠卡片并加载详情与历史数据
  const toggleServerExpand = serverId => {
    const server = serverList.find(s => s.id === serverId);
    if (!server) return;

    if (!isServerOnline(server)) {
      toast.warning('主机未在线，无法查看详情');
      return;
    }

    if (expandedServers.includes(serverId)) {
      setExpandedServers(prev => prev.filter(id => id !== serverId));
      expandInteractionUntilRef.current.delete(String(serverId));
    } else {
      setExpandedServers(prev => [...prev, serverId]);
      expandInteractionUntilRef.current.set(
        String(serverId),
        Date.now() + SERVER_EXPAND_INTERACTION_GUARD_MS
      );
      const hydrateExpandedServer = async () => {
        await Promise.all([
          server.info ? Promise.resolve(server.info) : loadServerInfo(serverId, { force: false }),
          loadCardMetrics(serverId, { silent: !!server.info }),
        ]);
        window.setTimeout(
          () => {
            loadNetworkQuality(serverId, { silent: true }).catch(() => {});
          },
          Math.max(90, SERVER_CHART_RENDER_DEFER_MS)
        );
      };
      const runHydration = () => {
        hydrateExpandedServer().catch(error => {
          console.error('[Server] Failed to hydrate expanded row:', error);
        });
      };

      if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
        window.requestAnimationFrame(runHydration);
      } else {
        setTimeout(runHydration, 0);
      }
    }
  };

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
      tags: [],
      description: '',
      startsAt: '',
      expiresAt: '',
      trafficLimitValue: '',
      trafficLimitUnit: 'TB',
      trafficAlertEnabled: false,
      trafficAlertPercent: 100,
      trafficCycleType: 'none',
      trafficCycleDay: 1,
      trafficCycleStart: '',
      trafficCycleEnd: '',
      monitorMode: 'agent',
      remoteDesktopNatIps: '',
      remoteDesktopNatType: 'srflx',
    });
    setSelectedCredentialId('');
    setServerModalMode('add');
    setShowServerModal(true);
  };

  const openEditServerModal = server => {
    const trafficQuotaForm = bytesToTrafficQuotaForm(server.traffic_limit_bytes);
    setServerForm({
      id: server.id,
      name: server.name,
      host: server.host || '',
      port: server.port || 22,
      username: server.username || 'root',
      authType: server.auth_type === 'key' ? 'privateKey' : 'password',
      password: '',
      privateKey: '',
      passphrase: '',
      tags: Array.isArray(server.tags) ? server.tags : [],
      description: server.description || '',
      startsAt: formatDateInputValue(server.starts_at || server.created_at),
      expiresAt: formatDateInputValue(server.expires_at),
      trafficLimitValue: trafficQuotaForm.value,
      trafficLimitUnit: trafficQuotaForm.unit,
      trafficLimitMode: server.traffic_limit_mode || 'total',
      trafficAlertEnabled: Boolean(server.traffic_alert_enabled),
      trafficAlertPercent: normalizeTrafficAlertPercentInput(server.traffic_alert_percent),
      trafficCycleType: server.traffic_cycle_type || 'none',
      trafficCycleDay: normalizeTrafficCycleDayInput(server.traffic_cycle_day),
      trafficCycleStart: formatDateInputValue(server.traffic_cycle_start),
      trafficCycleEnd: formatDateInputValue(server.traffic_cycle_end),
      monitorMode: server.monitor_mode || 'agent',
      remoteDesktopNatIps: server.remote_desktop_nat_ips || '',
      remoteDesktopNatType: server.remote_desktop_nat_type || 'srflx',
    });
    setServerAddMode('ssh');
    setSelectedCredentialId('');
    setServerModalMode('edit');
    setShowServerModal(true);
  };

  const applyCredential = credId => {
    const normalizedCredId = String(credId ?? '');
    setSelectedCredentialId(normalizedCredId);

    if (!normalizedCredId) return;

    const cred = serverCredentials.find(c => String(c.id) === normalizedCredId);
    if (!cred) return;

    setServerForm(prev => ({
      ...prev,
      username: cred.username,
      authType: cred.auth_type === 'key' ? 'privateKey' : 'password',
      password: cred.password || '',
      privateKey: cred.private_key || '',
      passphrase: cred.passphrase || '',
    }));
  };

  const testServerConnection = async () => {
    if (!serverForm.host || !serverForm.username) {
      toast.warning('请填写连接地址和用户名');
      return;
    }
    setServerModalSaving(true);
    toast.info('正在测试连接...');

    try {
      const response = await fetch('/api/server/test-connection', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          serverId: serverForm.id,
          host: serverForm.host,
          port: serverForm.port,
          username: serverForm.username,
          auth_type: serverForm.authType === 'privateKey' ? 'key' : 'password',
          password: serverForm.password,
          private_key: serverForm.privateKey,
          passphrase: serverForm.passphrase,
        }),
      });
      const data = await response.json();
      if (data.success) {
        toast.success('连接测试成功');
      } else {
        toast.error('测试连接失败: ' + data.message);
      }
    } catch (e) {
      toast.error('测试连接请求异常: ' + e.message);
    } finally {
      setServerModalSaving(false);
    }
  };

  const ensureTrafficAlertRule = async () => {
    try {
      const [rulesRes, channelsRes] = await Promise.all([
        fetch('/api/notification/rules'),
        fetch('/api/notification/channels'),
      ]);
      const rulesData = await rulesRes.json().catch(() => ({}));
      const channelsData = await channelsRes.json().catch(() => ({}));
      const rules = Array.isArray(rulesData.data) ? rulesData.data : [];
      if (
        rules.some(rule => rule.source_module === 'server' && rule.event_type === 'traffic_high')
      ) {
        return true;
      }
      const channels = (Array.isArray(channelsData.data) ? channelsData.data : [])
        .filter(channel => channel.enabled !== false && channel.enabled !== 0)
        .map(channel => channel.id)
        .filter(Boolean);
      if (channels.length === 0) {
        toast.warning('请先配置通知渠道后再创建流量告警规则');
        return false;
      }
      const res = await fetch('/api/notification/rules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: '主机流量超额',
          source_module: 'server',
          event_type: 'traffic_high',
          severity: 'warning',
          enabled: true,
          channels,
          suppression: { count: 1, minutes: 30 },
          title_template: '⚠️ {{serverName}} 流量超额',
          message_template:
            '主机 {{serverName}} 流量已使用 {{traffic_percent}}%，阈值 {{threshold}}%。\n已用：{{traffic_used}}\n配额：{{traffic_limit}}',
        }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || data.success === false)
        throw new Error(data.error || '自动创建流量告警规则失败');
      toast.success('已自动创建流量告警规则');
      return true;
    } catch (error) {
      toast.warning(error.message || '流量告警规则自动创建失败');
      return false;
    }
  };

  const testTrafficAlert = async () => {
    if (!serverForm.id) {
      toast.warning('请先保存主机后再测试报警');
      return;
    }
    const trafficLimitBytes = trafficQuotaInputToBytes(
      serverForm.trafficLimitValue,
      serverForm.trafficLimitUnit
    );
    if (trafficLimitBytes <= 0) {
      toast.warning('请先设置总流量配额');
      return;
    }
    setServerModalSaving(true);
    try {
      const ruleReady = await ensureTrafficAlertRule();
      if (!ruleReady) return;
      const response = await fetch(`/api/server/accounts/${serverForm.id}/test-traffic-alert`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          traffic_alert_percent: normalizeTrafficAlertPercentInput(serverForm.trafficAlertPercent),
        }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || data.success === false)
        throw new Error(data.error || data.message || '报警测试失败');
      toast.success('流量报警测试已发送');
    } catch (error) {
      toast.error(error.message || '报警测试失败');
    } finally {
      setServerModalSaving(false);
    }
  };

  const saveServer = async () => {
    const isAgentForm = serverForm.monitorMode === 'agent' || serverAddMode === 'agent';
    const hasSshEndpoint = Boolean(serverForm.host?.trim() && serverForm.username?.trim());
    if (!serverForm.name?.trim()) {
      toast.warning('请填写主机名称');
      return;
    }
    if (!isAgentForm && !hasSshEndpoint) {
      toast.warning('请填写连接地址和用户名');
      return;
    }
    if (
      serverForm.trafficCycleType === 'custom' &&
      serverForm.trafficCycleStart &&
      serverForm.trafficCycleEnd
    ) {
      const cycleStart = new Date(`${serverForm.trafficCycleStart}T00:00:00`).getTime();
      const cycleEnd = new Date(`${serverForm.trafficCycleEnd}T23:59:59`).getTime();
      if (Number.isFinite(cycleStart) && Number.isFinite(cycleEnd) && cycleEnd < cycleStart) {
        toast.warning('流量周期结束时间不能早于开始时间');
        return;
      }
    }
    setServerModalSaving(true);

    try {
      const tags = Array.isArray(serverForm.tags)
        ? serverForm.tags.map(t => String(t).trim()).filter(Boolean)
        : [];
      const trafficLimitBytes = trafficQuotaInputToBytes(
        serverForm.trafficLimitValue,
        serverForm.trafficLimitUnit
      );
      const trafficAlertEnabled = trafficLimitBytes > 0 && Boolean(serverForm.trafficAlertEnabled);
      const payload = {
        name: serverForm.name.trim(),
        host: serverForm.host?.trim() || '',
        port: serverForm.port,
        username: serverForm.username?.trim() || 'agent',
        auth_type: serverForm.authType === 'privateKey' ? 'key' : 'password',
        tags,
        description: serverForm.description,
        starts_at: normalizeStartInputValue(serverForm.startsAt),
        expires_at: normalizeExpiryInputValue(serverForm.expiresAt),
        traffic_limit_bytes: trafficLimitBytes,
        traffic_limit_mode: serverForm.trafficLimitMode || 'total',
        traffic_alert_enabled: trafficAlertEnabled,
        traffic_alert_percent: normalizeTrafficAlertPercentInput(serverForm.trafficAlertPercent),
        traffic_cycle_type: serverForm.trafficCycleType || 'none',
        traffic_cycle_day: normalizeTrafficCycleDayInput(serverForm.trafficCycleDay),
        traffic_cycle_start:
          serverForm.trafficCycleType === 'custom'
            ? normalizeStartInputValue(serverForm.trafficCycleStart)
            : null,
        traffic_cycle_end:
          serverForm.trafficCycleType === 'custom'
            ? normalizeExpiryInputValue(serverForm.trafficCycleEnd)
            : null,
        monitor_mode: isAgentForm ? 'agent' : 'ssh',
        remote_desktop_nat_ips: (serverForm.remoteDesktopNatIps || '').trim(),
        remote_desktop_nat_type: serverForm.remoteDesktopNatType || 'srflx',
      };

      if (serverForm.authType === 'password' && serverForm.password) {
        payload.password = serverForm.password;
      }
      if (serverForm.authType === 'privateKey' && serverForm.privateKey) {
        payload.private_key = serverForm.privateKey;
        payload.passphrase = serverForm.passphrase;
      }

      const url =
        serverModalMode === 'add'
          ? '/api/server/accounts'
          : `/api/server/accounts/${serverForm.id}`;
      const method = serverModalMode === 'add' ? 'POST' : 'PUT';

      const response = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const data = await response.json();
      if (data.success) {
        toast.success(serverModalMode === 'add' ? '主机添加成功' : '主机更新成功');
        if (trafficAlertEnabled) {
          await ensureTrafficAlertRule();
        }
        setShowServerModal(false);
        loadServerList();
      } else {
        toast.error(data.error || '保存失败');
      }
    } catch (e) {
      toast.error('保存异常: ' + e.message);
    } finally {
      setServerModalSaving(false);
    }
  };

  const generateQuickInstallCommand = async () => {
    const name = quickDeployName.trim();
    if (!name) {
      toast.warning('请输入主机名称');
      return;
    }

    setServerModalSaving(true);
    setQuickDeployResult(null);

    try {
      const response = await fetch('/api/server/agent/quick-install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
      const data = await response.json();

      if (data.success) {
        setQuickDeployResult(data.data);
        toast.success(data.data?.isNew ? 'Agent 主机已创建' : 'Agent 安装命令已生成');
        loadServerList();
      } else {
        toast.error(data.error || '生成 Agent 安装命令失败');
      }
    } catch (e) {
      toast.error('Agent 快速安装请求失败：' + e.message);
    } finally {
      setServerModalSaving(false);
    }
  };

  const showAgentInstallModal = async serverId => {
    const server = serverList.find(s => s.id === serverId);
    if (!server) {
      toast.error('主机不存在');
      return;
    }

    setAgentInstallLoading(true);
    setAgentInstallLog('');
    setAgentInstallResult(null);
    setAgentInstalling(false);
    setAgentForceSsh(false);
    setAgentModalData({
      serverId,
      serverName: server.name,
      installCommand: '',
      winInstallCommand: '',
      apiUrl: '',
      agentKey: '',
    });
    setShowAgentModal(true);

    try {
      const response = await fetch(`/api/server/agent/command/${serverId}`);
      const data = await response.json();
      if (data.success) {
        setAgentModalData(prev => ({ ...prev, ...(data.data || {}) }));
      } else {
        toast.error('获取 Agent 安装命令失败: ' + (data.error || data.message || '未知错误'));
      }
    } catch (e) {
      toast.error('获取 Agent 安装命令失败');
    } finally {
      setAgentInstallLoading(false);
    }
  };

  const normalizeAgentOrigin = value => {
    const raw = String(value || '')
      .trim()
      .replace(/\/+$/, '');
    if (!raw) return '';

    try {
      const url = new URL(/^https?:\/\//i.test(raw) ? raw : `https://${raw}`);
      return url.origin;
    } catch (e) {
      return '';
    }
  };

  const getAgentPublicBaseApiUrl = () => {
    const configuredOrigin = normalizeAgentOrigin(publicApiUrl);
    if (configuredOrigin) return configuredOrigin;
    return window.location.origin;
  };

  const getAgentBaseApiUrl = () => {
    const normalizeOrigin = value => {
      const raw = String(value || '')
        .trim()
        .replace(/\/+$/, '');
      if (!raw) return '';

      try {
        const url = new URL(/^https?:\/\//i.test(raw) ? raw : `https://${raw}`);
        return url.origin;
      } catch (e) {
        return '';
      }
    };

    const configuredOrigin = normalizeOrigin(publicApiUrl);
    if (configuredOrigin) return configuredOrigin;

    const modalOrigin = normalizeOrigin(agentModalData?.apiUrl);
    if (modalOrigin) return modalOrigin;

    return window.location.origin;
  };

  const getAgentInstallProtocol = () => {
    try {
      return new URL(getAgentBaseApiUrl()).protocol === 'http:' ? 'http' : 'https';
    } catch (_) {
      return 'https';
    }
  };

  const getAgentInstallEndpoint = (osType = agentInstallOS) => {
    return buildAgentInstallEndpoint({
      baseUrl: getAgentBaseApiUrl(),
      serverId: agentModalData?.serverId,
      agentKey: agentModalData?.agentKey,
      protocol: getAgentInstallProtocol(),
      osType,
    });
  };

  const getAgentInstallCommand = (osType = agentInstallOS) => {
    if (!agentModalData) return '';
    if (!agentModalData.agentKey) {
      return isWindowsAgentInstallOs(osType)
        ? agentModalData.winInstallCommand || ''
        : agentModalData.installCommand || '';
    }

    return buildAgentInstallCommand({
      baseUrl: getAgentBaseApiUrl(),
      serverId: agentModalData.serverId,
      agentKey: agentModalData.agentKey,
      protocol: getAgentInstallProtocol(),
      osType,
    });
  };

  const regenerateAgentKey = async () => {
    const confirmed = await dialog.confirm({
      title: '重新生成全局 Agent 密钥',
      message: '该密钥为全局密钥,重新生成会影响所有主机的 Agent 认证,请确认。',
      confirmText: '重新生成',
      cancelText: '取消',
      variant: 'destructive',
    });
    if (!confirmed) return;
    try {
      const response = await fetch('/api/server/agent/regenerate-key', { method: 'POST' });
      const data = await response.json();
      if (data.success) {
        toast.success('Agent 全局密钥已重新生成');
        if (agentModalData?.serverId) {
          await showAgentInstallModal(agentModalData.serverId);
        }
      } else {
        toast.error('重新生成失败: ' + (data.error || '未知错误'));
      }
    } catch (e) {
      toast.error('重新生成 Agent 密钥失败');
    }
  };

  const waitForAgentRestart = async (serverId, initialConnectedAt, timeoutMs = 180000) => {
    const start = Date.now();
    await new Promise(resolve => setTimeout(resolve, 3000));

    while (Date.now() - start < timeoutMs) {
      try {
        const response = await fetch(`/api/server/agent/connection-info/${serverId}`);
        const data = await response.json();
        if (data.status === 'online') {
          const connectedAt = data.connectedAt || 0;
          if (initialConnectedAt === 0 || connectedAt > initialConnectedAt) {
            return true;
          }
        }
      } catch (e) {
        // transient network errors are ignored during verification
      }
      await new Promise(resolve => setTimeout(resolve, 2000));
    }
    return false;
  };

  const autoInstallAgent = async serverId => {
    setAgentInstalling(true);
    setAgentInstallResult(null);
    setAgentInstallLog('正在连接服务器并安装 Agent...\n');

    let initialConnectedAt = 0;
    try {
      const response = await fetch(`/api/server/agent/connection-info/${serverId}`);
      const data = await response.json();
      if (data.status === 'online') initialConnectedAt = data.connectedAt || 0;
    } catch (e) {
      // pre-check is best-effort
    }

    try {
      const response = await fetch(
        `/api/server/agent/auto-install/${serverId}?protocol=${encodeURIComponent(getAgentInstallProtocol())}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ force_ssh: agentForceSsh, base_url: getAgentPublicBaseApiUrl() }),
        }
      );
      const data = await response.json();
      if (data.success) {
        setAgentInstallLog(
          prev =>
            `${prev}${data.output || ''}\n\n安装/升级指令执行成功，正在验证 Agent 是否重新连接...`
        );
        const ok = await waitForAgentRestart(serverId, initialConnectedAt);
        if (ok) {
          setAgentInstallLog(prev => `${prev}\nAgent 新连接已建立，安装/升级成功。`);
          setAgentInstallResult('success');
          toast.success('Agent 已就绪');
          loadServerList();
        } else {
          setAgentInstallLog(prev => `${prev}\nAgent 未能在 180 秒内重建连接，可能仍在启动中。`);
          setAgentInstallResult('warning');
        }
      } else {
        setAgentInstallLog(
          prev =>
            `${prev}${data.output || ''}\n\n安装失败: ${data.details || data.error || '未知错误'}`
        );
        setAgentInstallResult('error');
        toast.error('Agent 安装失败');
      }
    } catch (e) {
      setAgentInstallLog(prev => `${prev}\n网络错误: ${e.message}`);
      setAgentInstallResult('error');
    } finally {
      setAgentInstalling(false);
    }
  };

  const uninstallAgent = async serverId => {
    if (
      !(await dialog.confirm({
        title: '卸载 Agent',
        message: '从目标主机上卸载 Agent？',
        confirmText: '卸载',
        cancelText: '取消',
      }))
    )
      return;

    setAgentInstallLoading(true);
    try {
      let response = await fetch(`/api/server/agent/uninstall/${serverId}`, { method: 'POST' });
      let data = await response.json();
      if (response.status === 409 && data.data?.can_force_detach) {
        const confirmed = await dialog.confirm({
          title: '仅断开 Agent 关联',
          message: `${data.error}。继续操作只会断开面板关联，无法确认目标主机上的 Agent 程序已经删除。`,
          confirmText: '仅断开关联',
          cancelText: '取消',
          variant: 'destructive',
        });
        if (!confirmed) return;
        response = await fetch(`/api/server/agent/uninstall/${serverId}?force=1`, {
          method: 'POST',
        });
        data = await response.json();
      }
      if (response.ok && data.success) {
        toast.success(data.message || 'Agent 已卸载');
        setShowAgentModal(false);
        loadServerList();
      } else {
        toast.error('卸载失败: ' + (data.error || '未知错误'));
      }
    } catch (e) {
      toast.error('卸载 Agent 请求失败');
    } finally {
      setAgentInstallLoading(false);
    }
  };

  const deleteServer = async serverId => {
    const server = serverList.find(item => item.id === serverId);
    if (
      !confirmPress(
        `server.delete::${serverId}`,
        `删除主机「${server?.name || server?.host || `#${serverId}`}」`
      )
    )
      return;
    try {
      let response = await fetch(`/api/server/accounts/${serverId}`, { method: 'DELETE' });
      let data = await response.json();
      if (response.status === 409 && data.data?.can_force_delete) {
        const dependencies = data.data.dependencies || {};
        const dependencyText = [
          dependencies.nodes ? `${dependencies.nodes} 个节点` : '',
          dependencies.runtimes ? `${dependencies.runtimes} 个代理程序` : '',
          dependencies.tunnels ? `${dependencies.tunnels} 个 Tunnel` : '',
          dependencies.status_pages ? `${dependencies.status_pages} 个状态页关联` : '',
        ]
          .filter(Boolean)
          .join('、');
        const confirmed = await dialog.confirm({
          title: '强制移除离线主机',
          message: `${data.error}${dependencyText ? `。关联资源：${dependencyText}` : ''}。继续后会停止发布节点并清理全部面板关联，但离线主机本地可能残留 Agent 或代理服务。`,
          confirmText: '强制移除',
          cancelText: '取消',
          variant: 'destructive',
        });
        if (!confirmed) return;
        response = await fetch(`/api/server/accounts/${serverId}?force=1`, { method: 'DELETE' });
        data = await response.json();
      }
      if (!response.ok || !data.success) {
        throw new Error(data.error || '删除任务提交失败');
      }
      const taskId = data.data?.task_id;
      if (!taskId) {
        toast.success(data.message || '主机删除成功');
        await loadServerList();
        return;
      }
      toast.show({
        type: 'info',
        title: '正在级联删除主机',
        description: '系统将依次停止发布节点、清理代理资源、卸载 Agent，并删除面板关联。',
        isManual: true,
        duration: 5000,
      });
      const deadline = Date.now() + 10 * 60 * 1000;
      while (Date.now() < deadline) {
        const taskResponse = await fetch(`/api/server/tasks/${taskId}`, { cache: 'no-store' });
        const taskPayload = await taskResponse.json();
        if (!taskResponse.ok) throw new Error(taskPayload.error || '无法读取删除任务状态');
        const task = taskPayload.data || taskPayload;
        if (task.status === 'completed') {
          toast.success(typeof task.data === 'string' ? task.data : '主机级联删除完成');
          await loadServerList();
          return;
        }
        if (task.status === 'failed' || task.status === 'cancelled') {
          throw new Error(task.error || '主机级联删除失败');
        }
        await new Promise(resolve => window.setTimeout(resolve, 1000));
      }
      throw new Error('删除任务等待超时，请稍后刷新查看最终状态');
    } catch (e) {
      toast.error(`删除失败: ${e.message || '未知错误'}`);
    }
  };

  const runServerPowerAction = async (serverId, action) => {
    const actionText = action === 'reboot' ? '重启' : '关机';
    const confirmed = await dialog.confirm({
      title: `${actionText}主机`,
      message:
        action === 'shutdown'
          ? '此操作不可逆，请确认当前没有关键任务。'
          : '重启过程中连接会短暂中断。',
      confirmText: actionText,
      cancelText: '取消',
      variant: action === 'shutdown' ? 'destructive' : 'default',
    });
    if (!confirmed) return;

    try {
      const response = await fetch('/api/server/action', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverId, action }),
      });
      const data = await response.json();
      if (data.success) {
        toast.success(`${actionText}命令已发送`);
      } else {
        toast.error(`${actionText}失败: ${data.message || data.error || '未知错误'}`);
      }
    } catch (e) {
      toast.error(`${actionText}请求失败`);
    }
  };

  const saveServerOrder = async orderedList => {
    const orderData = orderedList.map((server, index) => ({
      id: server.id,
      order_index: index,
    }));

    try {
      const response = await fetch('/api/server/accounts/reorder', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ orderData }),
      });
      const data = await response.json();
      if (!data.success) {
        toast.error('排序保存失败: ' + (data.error || data.message || '未知错误'));
        loadServerList();
      }
    } catch (e) {
      toast.error('排序保存请求失败');
      loadServerList();
    }
  };

  const toggleDockerPanel = serverId => {
    setExpandedDockerPanels(prev =>
      prev.includes(serverId) ? prev.filter(id => id !== serverId) : [...prev, serverId]
    );
  };

  const getDockerContainerState = container => {
    const state = String(container?.state ?? container?.State ?? '').toLowerCase();
    const status = String(container?.status ?? container?.Status ?? '').toLowerCase();
    if (state === 'paused' || status.includes('paused')) return 'paused';
    if (state === 'restarting' || status.includes('restarting')) return 'restarting';
    if (state === 'running' || (status.includes('up') && !status.includes('paused')))
      return 'running';
    if (state === 'dead' || status.includes('dead')) return 'dead';
    if (
      state === 'exited' ||
      state === 'created' ||
      state === 'stopped' ||
      status.includes('exited')
    )
      return 'stopped';
    return state || 'unknown';
  };

  const getDockerContainerId = container =>
    String(
      container?.id ||
        container?.ID ||
        container?.Id ||
        container?.container_id ||
        container?.ContainerID ||
        ''
    );

  const getDockerContainerName = container => {
    const names = container?.names || container?.Names;
    if (Array.isArray(names) && names.length > 0) return String(names[0]).replace(/^\/+/, '');
    return String(
      container?.name ||
        container?.Name ||
        container?.container_name ||
        getDockerContainerId(container).slice(0, 12) ||
        '-'
    ).replace(/^\/+/, '');
  };

  const getDockerContainerImage = container =>
    String(
      container?.image || container?.Image || container?.imageName || container?.ImageName || '-'
    );

  const getDockerContainerPorts = formatDockerContainerPorts;

  const getDockerUpdateCheckKey = (serverId, value) => {
    const normalized = String(value || '').trim();
    return normalized ? `${serverId}::${normalized}` : '';
  };

  const getDockerUpdateAliases = (serverId, result = {}) => {
    const aliases = new Set();
    const add = value => {
      const text = String(value || '')
        .trim()
        .replace(/^\/+/, '');
      if (!text || text === '-') return;
      aliases.add(text);
      if (text.length > 12) aliases.add(text.slice(0, 12));
    };

    add(result.containerId);
    add(result.container_id);
    add(result.containerName);
    add(result.container_name);
    add(result.name);
    add(result.image);

    return Array.from(aliases)
      .map(value => getDockerUpdateCheckKey(serverId, value))
      .filter(Boolean);
  };

  const normalizeDockerUpdateCheck = (serverId, result = {}, fallback = {}) => ({
    serverId,
    containerId: String(result.container_id || result.containerId || fallback.containerId || ''),
    containerName: String(
      result.container_name || result.containerName || result.name || fallback.containerName || ''
    ).replace(/^\/+/, ''),
    image: String(result.image || fallback.image || ''),
    currentDigest: String(result.current_digest || result.currentDigest || ''),
    latestDigest: String(result.latest_digest || result.latestDigest || ''),
    hasUpdate: !!(result.has_update ?? result.hasUpdate),
    skipped: !!(result.skipped ?? result.has_skipped ?? false),
    error: result.error || '',
    checkedAt: Date.now(),
  });

  const storeDockerUpdateChecks = (serverId, results, fallback = {}) => {
    const list = Array.isArray(results) ? results : results ? [results] : [];
    if (list.length === 0) return;

    setDockerUpdateChecks(prev => {
      const next = { ...prev };
      list.forEach(result => {
        const normalized = normalizeDockerUpdateCheck(serverId, result, fallback);
        getDockerUpdateAliases(serverId, normalized).forEach(key => {
          next[key] = normalized;
        });
      });
      return next;
    });
  };

  const getDockerContainerUpdateCheck = (serverId, container) => {
    const containerId = getDockerContainerId(container);
    const containerName = getDockerContainerName(container);
    const containerImage = getDockerContainerImage(container);
    const keys = getDockerUpdateAliases(serverId, {
      containerId,
      containerName,
      image: containerImage,
    });

    return keys.map(key => dockerUpdateChecks[key]).find(Boolean) || null;
  };

  const isDockerContainerUpdateChecking = (serverId, container) => {
    const containerId = getDockerContainerId(container);
    const containerName = getDockerContainerName(container);
    const containerImage = getDockerContainerImage(container);
    const keys = getDockerUpdateAliases(serverId, {
      containerId,
      containerName,
      image: containerImage,
    });

    return (
      !!dockerBulkUpdateCheckServers[serverId] || keys.some(key => dockerUpdateCheckLoading[key])
    );
  };

  const getDockerActionKey = (serverId, action, payload = {}) =>
    [
      String(serverId || ''),
      String(action || ''),
      String(
        payload.containerId ||
          payload.containerName ||
          payload.image ||
          payload.name ||
          payload.project ||
          payload.projectName ||
          '*'
      ),
    ].join('::');

  const clearDockerActionPending = (serverId, action, payload = null) => {
    if (!serverId || !action) return;
    const exactKey = payload ? getDockerActionKey(serverId, action, payload) : '';
    const prefix = `${serverId}::${action}::`;
    setDockerActionPending(prev => {
      let changed = false;
      const next = { ...prev };
      if (exactKey) {
        if (next[exactKey]) {
          delete next[exactKey];
          changed = true;
        }
      } else {
        Object.keys(next).forEach(key => {
          if (key.startsWith(prefix)) {
            delete next[key];
            changed = true;
          }
        });
      }
      return changed ? next : prev;
    });
  };

  const isDockerActionPending = (serverId, action, payload = {}) =>
    !!dockerActionPending[getDockerActionKey(serverId, action, payload)];

  const getDockerContainerSelectionKey = (serverId, container = {}) => {
    const containerId = String(
      container?.containerId || container?.container_id || getDockerContainerId(container) || ''
    );
    const containerName = String(
      container?.containerName ||
        container?.container_name ||
        container?.name ||
        container?.Name ||
        ''
    ).replace(/^\/+/, '');
    const image = String(container?.image || container?.Image || '');
    return [String(serverId || ''), containerId || containerName || image || '*'].join('::');
  };

  const getDockerTaskTargetLabel = (payload = {}) =>
    String(
      payload.containerName ||
        payload.container_name ||
        payload.name ||
        payload.project ||
        payload.projectName ||
        payload.image ||
        payload.containerId ||
        payload.container_id ||
        ''
    ).replace(/^\/+/, '');

  const getDockerTaskDisplayTitle = (task = {}) => {
    const payload = task.payload || {};
    const targetName = task.targetName || getDockerTaskTargetLabel(payload);
    const actionLabel = getDockerTaskActionLabel(task.action || task.command || task.type);

    const sId = task.serverId || payload.serverId || payload.server_id;
    const serverMatch =
      (dockerOverviewServers || []).find(s => String(s.id) === String(sId)) ||
      (serverList || []).find(s => String(s.id) === String(sId));
    const hostName = task.serverName || payload.serverName || serverMatch?.name;

    const hostPrefix = hostName ? `[${hostName}] ` : '';
    const mainTitle = targetName ? `${actionLabel} ${targetName}` : actionLabel;
    return `${hostPrefix}${mainTitle}`;
  };

  const getDockerTaskStateVariant = (state = '') => {
    const normalized = String(state || '').toLowerCase();
    if (normalized === 'success' || normalized === 'succeeded') return 'success';
    if (normalized === 'failed' || normalized === 'error' || normalized === 'timeout')
      return 'error';
    if (normalized === 'cancelled' || normalized === 'canceled') return 'neutral';
    return 'warning';
  };

  const isDockerTaskFinalState = (state = '') =>
    ['success', 'succeeded', 'failed', 'error', 'timeout', 'cancelled', 'canceled'].includes(
      String(state || '').toLowerCase()
    );

  const decorateDockerTask = (task = {}) => {
    const meta = dockerTaskMetaRef.current[task.taskId] || {};
    const payload = task.payload || meta.payload || {};
    const sId = task.serverId || meta.serverId || payload.serverId || payload.server_id;
    const serverMatch =
      (dockerOverviewServers || []).find(s => String(s.id) === String(sId)) ||
      (serverList || []).find(s => String(s.id) === String(sId));
    const serverName =
      task.serverName || meta.serverName || payload.serverName || serverMatch?.name;

    return normalizeDockerTaskResult({
      ...meta,
      ...task,
      payload,
      action: task.action || meta.action,
      serverId: sId,
      serverName,
      targetName: task.targetName || meta.targetName || getDockerTaskTargetLabel(payload),
      silent: task.silent ?? meta.silent,
    });
  };

  const rememberDockerTaskMeta = (taskId, meta = {}) => {
    if (!taskId) return;
    const payload = meta.payload || {};
    const normalized = {
      ...meta,
      payload,
      targetName: meta.targetName || getDockerTaskTargetLabel(payload),
      submittedAt: Date.now(),
    };
    dockerTaskMetaRef.current = {
      ...dockerTaskMetaRef.current,
      [taskId]: normalized,
    };
    setDockerTasks(prev => {
      let found = false;
      const next = prev.map(task => {
        if (task.taskId !== taskId) return task;
        found = true;
        return decorateDockerTask({ ...task, ...normalized, taskId });
      });
      if (found) return next;
      return [
        {
          taskId,
          state: 'queued',
          progress: 0,
          domain: 'docker',
          ...normalized,
        },
        ...prev,
      ].slice(0, 30);
    });
  };

  const appendDockerInlineTask = (task = {}) => {
    const taskId =
      task.taskId ||
      `inline-${task.action || 'docker'}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const payload = task.payload || {};
    const normalized = decorateDockerTask({
      ...task,
      taskId,
      payload,
      progress: task.progress ?? (isDockerTaskFinalState(task.state) ? 100 : 0),
      domain: task.domain || 'docker',
      targetName: task.targetName || getDockerTaskTargetLabel(payload),
    });
    setDockerTasks(prev =>
      [normalized, ...prev.filter(item => item.taskId !== taskId)].slice(0, 30)
    );
    return taskId;
  };

  const markDockerContainerUpdateComplete = (serverId, payload = {}) => {
    const normalized = {
      serverId,
      containerId: String(payload.containerId || payload.container_id || ''),
      containerName: String(payload.containerName || payload.container_name || '').replace(
        /^\/+/,
        ''
      ),
      image: String(payload.image || ''),
      hasUpdate: false,
      updatedAt: Date.now(),
      checkedAt: Date.now(),
    };
    setDockerUpdateChecks(prev => {
      const next = { ...prev };
      getDockerUpdateAliases(serverId, normalized).forEach(key => {
        next[key] = normalized;
      });
      return next;
    });
    const selectionKey = getDockerContainerSelectionKey(serverId, payload);
    setDockerSelectedContainerKeys(prev => prev.filter(key => key !== selectionKey));
  };

  const getDockerActionProgress = (serverId, action, payload = {}) => {
    const actionKey = getDockerActionKey(serverId, action, payload);
    const task = dockerTasks
      .map(decorateDockerTask)
      .find(
        item =>
          item.serverId === serverId &&
          item.action === action &&
          getDockerActionKey(item.serverId, item.action, item.payload || {}) === actionKey &&
          !isDockerTaskFinalState(item.state)
      );
    return task ? clampPercent(toNumber(task.progress, 0)) : 0;
  };

  const isDockerUpdateConfirmActive = isArmed;

  const confirmDockerUpdatePress = confirmPress;

  const handleDockerContainerUpdate = (payload = {}) => {
    const serverId = payload.serverId || dockerSelectedServer;
    const targetName = getDockerTaskTargetLabel(payload) || '当前容器';
    const confirmKey = `container.update::${getDockerContainerSelectionKey(serverId, payload)}`;
    if (!confirmDockerUpdatePress(confirmKey, `更新容器 ${targetName}`)) return;
    submitDockerTask('container.update', payload, { skipConfirm: true });
  };

  const getDockerUpdateBadge = check => {
    if (!check) return { variant: 'neutral', label: '未检测', title: '尚未检测镜像更新' };
    if (check.skipped)
      return {
        variant: 'neutral',
        label: '跳过',
        title: '本地镜像无外部镜像地址,已跳过更新检测',
      };
    if (check.error) return { variant: 'error', label: '失败', title: check.error };
    if (check.hasUpdate)
      return { variant: 'warning', label: '可更新', title: '远端镜像摘要与本地不一致' };
    if (check.updatedAt)
      return {
        variant: 'success',
        label: '已更新',
        title: '容器已提交更新，等待下次检测确认镜像摘要',
      };
    if (check.currentDigest && check.latestDigest)
      return { variant: 'success', label: '已最新', title: '本地镜像已是远端最新摘要' };
    return { variant: 'neutral', label: '已检测', title: '远端或本地摘要不完整，无法严格判断' };
  };

  const getDockerUpdateResultError = (result = {}) => String(result.error || '').trim();

  const getDockerStateBadge = state => {
    if (state === 'running') return { variant: 'success', label: '运行' };
    if (state === 'paused' || state === 'restarting')
      return { variant: 'warning', label: state === 'paused' ? '暂停' : '重启中' };
    if (state === 'stopped' || state === 'dead')
      return { variant: 'error', label: state === 'dead' ? '异常' : '停止' };
    return { variant: 'neutral', label: '未知' };
  };

  const openBatchAgentModal = () => {
    setSelectedBatchServers(serverList.filter(canSshDeployAgent).map(server => server.id));
    setBatchInstallResults([]);
    setBatchAgentForceSsh(false);
    setShowBatchAgentModal(true);
  };

  const selectAllBatchServers = () => {
    setSelectedBatchServers(serverList.filter(canSshDeployAgent).map(server => server.id));
  };

  const canSshDeployAgent = (server = {}) => {
    if (server.capabilities?.ssh_configured || server.capabilities?.supports_ssh) return true;
    const host = String(server.host || '').trim();
    if (!host || host === '0.0.0.0') return false;
    const username = String(server.username || '').trim();
    if (!username) return false;
    if (server.auth_type === 'key')
      return Boolean(server.private_key || server.capabilities?.has_private_key);
    return Boolean(server.password || server.capabilities?.has_password);
  };

  const toggleBatchServerSelection = serverId => {
    setSelectedBatchServers(prev =>
      prev.includes(serverId) ? prev.filter(id => id !== serverId) : [...prev, serverId]
    );
  };

  const updateBatchResult = (serverId, patch) => {
    setBatchInstallResults(prev =>
      prev.map(item => (item.serverId === serverId ? { ...item, ...patch } : item))
    );
  };

  const normalizeAgentBatchStatus = status => {
    if (status === 'queued') return 'waiting';
    if (status === 'running') return 'processing';
    if (status === 'succeeded') return 'success';
    return status || 'waiting';
  };

  const mapAgentBatchItems = (items = []) =>
    items.map(item => ({
      serverId: item.serverId,
      serverName: item.serverName || item.serverId,
      status: normalizeAgentBatchStatus(item.status),
      error: item.error || '',
      log: item.log || [],
    }));

  const isAgentBatchDone = batch => ['succeeded', 'failed'].includes(batch?.status);

  const pollAgentBatch = async (
    batchId,
    onSnapshot,
    intervalMs = 2000,
    timeoutMs = 15 * 60 * 1000
  ) => {
    const deadline = Date.now() + timeoutMs;
    while (batchId) {
      const response = await fetch(`/api/server/agent/batch/${batchId}`);
      const payload = await response.json();
      if (!payload.success) throw new Error(payload.error || '获取批任务状态失败');
      const batch = payload.data;
      onSnapshot(batch);
      if (isAgentBatchDone(batch)) return batch;
      if (Date.now() >= deadline) return batch;
      await new Promise(resolve => setTimeout(resolve, intervalMs));
    }
    return null;
  };

  const runBatchAgentInstall = async () => {
    if (selectedBatchServers.length === 0 || agentInstallLoading) return;

    const initialResults = selectedBatchServers.map(id => {
      const server = serverList.find(s => s.id === id);
      return {
        serverId: id,
        serverName: server?.name || '未知主机',
        status: 'waiting',
        error: '',
      };
    });
    setBatchInstallResults(initialResults);
    setAgentInstallLoading(true);

    try {
      const response = await fetch(
        `/api/server/agent/batch-install?protocol=${encodeURIComponent(getAgentInstallProtocol())}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            serverIds: selectedBatchServers,
            force_ssh: batchAgentForceSsh,
            base_url: getAgentPublicBaseApiUrl(),
            concurrency: 16,
          }),
        }
      );
      const payload = await response.json();
      if (!payload.success) throw new Error(payload.error || '创建批量部署任务失败');
      setBatchInstallResults(mapAgentBatchItems(payload.data.items));
      const finalBatch = await pollAgentBatch(payload.data.id, batch => {
        setBatchInstallResults(mapAgentBatchItems(batch.items));
      });
      if (finalBatch?.status === 'succeeded') {
        toast.success('批量 Agent 部署完成');
      } else {
        toast.warning('批量 Agent 部署完成，部分主机失败');
      }
      loadServerList();
    } catch (e) {
      toast.error(`批量 Agent 部署失败: ${e.message}`);
      setBatchInstallResults(prev =>
        prev.map(item =>
          item.status === 'success' ? item : { ...item, status: 'failed', error: e.message }
        )
      );
    } finally {
      setAgentInstallLoading(false);
    }
  };

  const openUpgradeModal = () => {
    setUpgradeLog('');
    upgradeLogLineKeysRef.current = new Set();
    setUpgradeBatchSnapshot(null);
    setUpgradeProgress(0);
    setUpgrading(false);
    setUpgradeFallbackSsh(false);
    setShowUpgradeModal(true);
  };

  const getAgentUpgradeTargets = () =>
    serverList.filter(
      server => isServerOnline(server) || (upgradeFallbackSsh && canSshDeployAgent(server))
    );

  const getUpgradeBatchStatusLabel = status => {
    switch (status) {
      case 'queued':
        return '排队中';
      case 'running':
        return '执行中';
      case 'verifying':
        return '验证中';
      case 'succeeded':
        return '已完成';
      case 'failed':
        return '部分失败';
      default:
        return status || '待执行';
    }
  };

  const getUpgradeItemStatusLabel = status => {
    switch (status) {
      case 'queued':
        return '等待';
      case 'running':
        return '执行中';
      case 'verifying':
        return '验证中';
      case 'succeeded':
        return '成功';
      case 'failed':
        return '失败';
      default:
        return status || '未知';
    }
  };

  const resetUpgradeLog = (lines = []) => {
    upgradeLogLineKeysRef.current = new Set();
    const normalized = lines.map(line => String(line || '').trimEnd()).filter(Boolean);
    normalized.forEach((line, index) =>
      upgradeLogLineKeysRef.current.add(`initial:${index}:${line}`)
    );
    setUpgradeLog(normalized.length > 0 ? `${normalized.join('\n')}\n` : '');
  };

  const appendUpgradeLogEvents = (events = []) => {
    const nextLines = [];
    events.forEach(event => {
      if (!event) return;
      const key = String(event.key || event.line || '');
      const line = String(event.line || '').trimEnd();
      if (!key || !line || upgradeLogLineKeysRef.current.has(key)) return;
      upgradeLogLineKeysRef.current.add(key);
      nextLines.push(line);
    });
    if (nextLines.length === 0) return;
    setUpgradeLog(prev => `${prev || ''}${nextLines.join('\n')}\n`);
  };

  const appendUpgradeBatchSnapshot = batch => {
    if (!batch?.id) return;
    const summary = batch.summary || {};
    const total = batch.items?.length || 0;
    const done = (summary.succeeded || 0) + (summary.failed || 0);
    const events = [
      {
        key: `${batch.id}:status:${batch.status}:${done}:${total}:${summary.succeeded || 0}:${summary.failed || 0}`,
        line: `批任务 ${String(batch.id).slice(0, 8)} ${getUpgradeBatchStatusLabel(batch.status)} · 进度 ${done}/${total} · 成功 ${summary.succeeded || 0} · 失败 ${summary.failed || 0}`,
      },
    ];

    // 逐主机日志改由「主机日志」分栏按主机滚动展示;这里只保留批任务级事件,
    // 避免同一行在两处重复、也避免多主机日志交错造成阅读混乱。
    appendUpgradeLogEvents(events);
  };

  const performOneKeyUpgrade = async () => {
    if (upgrading) return;

    setUpgrading(true);
    setUpgradeBatchSnapshot(null);
    resetUpgradeLog(['开始批量升级任务...']);
    setUpgradeProgress(0);

    const targetServers = getAgentUpgradeTargets();

    if (targetServers.length === 0) {
      appendUpgradeLogEvents([{ key: 'no-targets', line: '没有检测到在线的 Agent 主机。' }]);
      setUpgrading(false);
      return;
    }

    appendUpgradeLogEvents([
      { key: 'targets', line: `目标 Agent: ${targetServers.length} 台。` },
      {
        key: `options:${upgradeFallbackSsh}`,
        line: `服务端批任务并发限制: 16。${upgradeFallbackSsh ? 'SSH 保底已开启。' : 'SSH 保底未开启。'}`,
      },
    ]);

    try {
      const response = await fetch(
        `/api/server/agent/batch-upgrade?protocol=${encodeURIComponent(getAgentInstallProtocol())}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            serverIds: targetServers.map(server => server.id),
            force_ssh: false,
            fallback_ssh: upgradeFallbackSsh,
            base_url: getAgentPublicBaseApiUrl(),
            concurrency: 16,
          }),
        }
      );
      const payload = await response.json();
      if (!payload.success) throw new Error(payload.error || '创建批量升级任务失败');

      const finalBatch = await pollAgentBatch(payload.data.id, batch => {
        setUpgradeBatchSnapshot(batch);
        const summary = batch.summary || {};
        const total = batch.items?.length || targetServers.length;
        const done = (summary.succeeded || 0) + (summary.failed || 0);
        setUpgradeProgress(total > 0 ? Math.round((done / total) * 100) : 0);
        appendUpgradeBatchSnapshot(batch);
      });
      setUpgradeBatchSnapshot(finalBatch || null);
      setUpgradeProgress(100);
      if (finalBatch?.status === 'succeeded') {
        appendUpgradeLogEvents([
          { key: 'final:succeeded', line: '所有目标 Agent 均已完成升级并重新上线。' },
        ]);
        toast.success('Agent 批量升级完成');
      } else {
        appendUpgradeLogEvents([{ key: 'final:failed', line: '批量升级完成，部分 Agent 失败。' }]);
        toast.warning('Agent 批量升级完成，部分失败');
      }
      loadServerList();
    } catch (e) {
      appendUpgradeLogEvents([{ key: `error:${e.message}`, line: `批量升级失败: ${e.message}` }]);
      toast.error(`批量升级失败: ${e.message}`);
    } finally {
      setUpgrading(false);
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
  const startRenameServer = async server => {
    const newName = await dialog.prompt({
      message: '输入新的服务器名称',
      defaultValue: server.name,
    });
    if (newName && newName.trim() && newName !== server.name) {
      renameServer(server.id, newName.trim());
    }
  };

  const renameServer = async (serverId, name) => {
    try {
      const response = await fetch(`/api/server/accounts/${serverId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
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
      if (!data.success) return;
      const includeCredentials = await dialog.confirm({
        title: '导出主机配置',
        message:
          '默认仅导出连接与配置信息,不包含 SSH 密码、私钥与口令,可安全分享。包含明文凭据的导出文件请务必妥善保管。',
        confirmText: '包含明文凭据',
        cancelText: '仅导出配置',
        variant: 'destructive',
      });
      const clean = data.data.map(s => ({
        name: s.name,
        host: s.host,
        port: s.port,
        username: s.username,
        auth_type: s.auth_type,
        ...(includeCredentials
          ? {
              password: s.password,
              private_key: s.private_key,
              passphrase: s.passphrase,
            }
          : {}),
        tags: s.tags,
        description: s.description,
        country: s.country,
        resolved_country: s.resolved_country,
        starts_at: s.starts_at,
        expires_at: s.expires_at,
        monitor_mode: s.monitor_mode,
      }));
      const blob = new Blob([JSON.stringify(clean, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `servers_export_${new Date().toISOString().slice(0, 10)}.json`;
      a.click();
      a.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 0);
      toast.success(
        includeCredentials
          ? '主机配置导出成功(含明文凭据,请妥善保管)'
          : '主机配置导出成功(未包含凭据)'
      );
    } catch (e) {
      toast.error('导出主机失败');
    }
  };

  const openAddCredentialModal = () => {
    setCredForm({
      name: '',
      username: 'root',
      auth_type: 'password',
      password: '',
      private_key: '',
      passphrase: '',
    });
    setShowAddCredentialModal(true);
  };

  const openImportServerModal = () => {
    setImportPreview(null);
    setImportModalError('');
    setImportModalSaving(false);
    setShowImportServerModal(true);
  };

  const processImportFile = file => {
    if (!file.name.endsWith('.json')) {
      setImportModalError('仅支持导入 .json 格式文件');
      return;
    }
    const reader = new FileReader();
    reader.onload = e => {
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
        body: JSON.stringify({ servers: importPreview }),
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
            password: parts[4]?.trim() || '',
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
        body: JSON.stringify({ servers }),
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

  const addCredential = async () => {
    if (!credForm.name || !credForm.username) {
      toast.warning('凭据名称与用户名必填');
      return;
    }
    try {
      const response = await fetch('/api/server/credentials', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(credForm),
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

  const deleteCredential = async id => {
    const credential = serverCredentials.find(item => item.id === id);
    if (
      !confirmPress(
        `credential.delete::${id}`,
        `删除主机凭据「${credential?.name || credential?.username || `#${id}`}」`
      )
    )
      return;
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

  const setDefaultCredential = async id => {
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
  useEffect(() => {
    if (serverCurrentTab === 'docker') {
      ensureDockerTaskStream();
      loadDockerResources();
      loadDockerAutoUpdate();
    }
  }, [serverCurrentTab, dockerSubTab, dockerHostOptionKey]);

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
          const task = decorateDockerTask(JSON.parse(event.data));
          if (task && task.domain === 'docker') {
            setDockerTasks(prev => {
              const updated = prev.filter(t => t.taskId !== task.taskId);
              updated.unshift(task);
              if (updated.length > 30) updated.pop();
              return updated;
            });
            if (['success', 'failed', 'timeout', 'cancelled'].includes(task.state)) {
              clearDockerActionPending(task.serverId, task.action, task.payload);
            }
            // 任务成功完成时，如果正在当前标签页，触发静默刷新列表
            if (task.state === 'success') {
              if (task.action === 'container.update') {
                markDockerContainerUpdateComplete(task.serverId, task.payload);
              }
              if (!task.silent) {
                toast.success(`${getDockerTaskDisplayTitle(task)} 执行成功`);
              }
              scheduleDockerResourceRefresh(150);
            } else if (task.state === 'failed') {
              if (!task.silent) {
                toast.error(`${getDockerTaskDisplayTitle(task)} 失败: ${task.error || ''}`);
              }
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

  const getDockerTaskConfirmation = (action, payload = {}) => {
    const targetName =
      payload.containerName ||
      payload.image ||
      payload.name ||
      payload.project ||
      payload.containerId ||
      'Docker 资源';
    const confirmations = {
      'container.stop': {
        title: '停止容器',
        message: `停止容器 ${targetName}？正在运行的服务会中断。`,
        confirmText: '停止',
        variant: 'danger',
      },
      'container.restart': {
        title: '重启容器',
        message: `重启容器 ${targetName}？服务会短暂中断。`,
        confirmText: '重启',
        variant: 'danger',
      },
      'container.delete': {
        title: '\u5220\u9664\u5bb9\u5668',
        message: `\u6c38\u4e45\u5220\u9664\u5bb9\u5668 ${targetName}\uff1f\u6b64\u64cd\u4f5c\u65e0\u6cd5\u64a4\u9500\u3002`,
        confirmText: '\u5220\u9664',
        variant: 'danger',
        deleteResource: true,
      },
      'compose.down': {
        title: '停止 Compose 项目',
        message: `停止 Compose 项目 ${targetName}？相关服务会中断。`,
        confirmText: '停止项目',
        variant: 'danger',
      },
      'image.prune': {
        title: '清理未使用镜像',
        message: '清理该主机上未被容器引用的 Docker 镜像？',
        confirmText: '清理镜像',
        variant: 'danger',
      },
      'network.prune': {
        title: '清理未使用网络',
        message: '清理该主机上未被容器使用的自定义 Docker 网络？',
        confirmText: '清理网络',
        variant: 'danger',
      },
      'volume.prune': {
        title: '清理未使用存储卷',
        message: '清理该主机上未被容器使用的 Docker 数据卷？',
        confirmText: '清理存储卷',
        variant: 'danger',
      },
    };

    if (['image.remove', 'network.remove', 'volume.remove'].includes(action)) {
      return {
        deleteResource: true,
        resourceType:
          action === 'image.remove'
            ? 'Docker 镜像'
            : action === 'network.remove'
              ? 'Docker 网络'
              : 'Docker 存储卷',
        resourceName: targetName,
      };
    }

    return confirmations[action] || null;
  };

  const getDockerProxyContainerAction = action =>
    ({
      'container.start': 'start',
      'container.stop': 'stop',
      'container.restart': 'restart',
      'container.pause': 'pause',
      'container.unpause': 'unpause',
    })[action] || '';

  const getDockerProxyRequest = (serverId, action, payload = {}) => {
    const containerAction = getDockerProxyContainerAction(action);
    if (containerAction) {
      const containerId = payload.containerId;
      if (!containerId) throw new Error('Missing container ID');
      return {
        url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(containerId)}/${containerAction}`,
        options: { method: 'POST' },
      };
    }
    if (action === 'container.delete') {
      const containerId = payload.containerId;
      if (!containerId) throw new Error('Missing container ID');
      return {
        url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(containerId)}`,
        options: { method: 'DELETE' },
      };
    }
    if (action === 'image.prune') {
      return {
        url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/images/prune`,
        options: { method: 'POST' },
      };
    }
    if (action === 'image.remove') {
      const image = payload.image || payload.imageId || payload.id;
      if (!image) throw new Error('Missing image ID');
      const params = new URLSearchParams({ image });
      return {
        url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/images?${params.toString()}`,
        options: { method: 'DELETE' },
      };
    }
    if (action === 'network.prune') {
      return {
        url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/networks/prune`,
        options: { method: 'POST' },
      };
    }
    if (action === 'network.remove') {
      if (!payload.name) throw new Error('Missing network name');
      return {
        url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/networks/${encodeURIComponent(payload.name)}`,
        options: { method: 'DELETE' },
      };
    }
    if (action === 'volume.prune') {
      return {
        url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/volumes/prune`,
        options: { method: 'POST' },
      };
    }
    if (action === 'volume.remove') {
      if (!payload.name) throw new Error('Missing volume name');
      return {
        url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/volumes/${encodeURIComponent(payload.name)}`,
        options: { method: 'DELETE' },
      };
    }
    if (action.startsWith('compose.')) {
      const composeAction = action.split('.')[1];
      if (['up', 'down', 'restart', 'pull', 'update'].includes(composeAction)) {
        const project = payload.project || payload.projectName || payload.name;
        if (!project) throw new Error('Missing compose project');
        const configFile =
          payload.config_file ||
          payload.configFile ||
          payload.configFiles ||
          payload.ConfigFiles ||
          '';
        return {
          url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/stacks/${encodeURIComponent(project)}/${composeAction}`,
          options: {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ config_file: configFile }),
          },
        };
      }
    }
    return null;
  };

  const scheduleDockerResourceRefresh = (delayMs = 650) => {
    if (dockerRefreshTimerRef.current) clearTimeout(dockerRefreshTimerRef.current);
    dockerRefreshTimerRef.current = setTimeout(() => {
      dockerRefreshTimerRef.current = null;
      loadDockerResources({ silent: true });
    }, delayMs);
  };

  const submitDockerTask = async (action, payload = {}, options = {}) => {
    const serverId = payload.serverId || dockerSelectedServer;
    if (!serverId) {
      toast.warning('请先选择一台主机');
      return { ok: false };
    }
    const pendingKey = getDockerActionKey(serverId, action, payload);
    if (dockerActionPending[pendingKey]) {
      toast.info('该 Docker 操作正在执行中');
      return { ok: false };
    }
    if (!options.skipConfirm) {
      const confirmation = getDockerTaskConfirmation(action, payload);
      if (confirmation?.deleteResource) {
        if (!(await dialog.deleteResource(confirmation))) return { ok: false };
      } else if (confirmation && !(await dialog.confirm(confirmation))) {
        return { ok: false };
      }
    }

    if (isDockerMockPreviewEnabled()) {
      const taskId = `mock-${action}-${Date.now()}`;
      const mockTask = {
        taskId,
        state: 'success',
        progress: 100,
        action,
        domain: 'docker',
        serverId,
        payload,
        targetName: getDockerTaskTargetLabel(payload),
        silent: options.silent,
        message: `${payload.containerName || payload.project || payload.image || payload.name || 'Docker 资源'} 模拟执行成功`,
      };
      setDockerTasks(prev => [mockTask, ...prev].slice(0, 30));
      if (action === 'container.update') {
        markDockerContainerUpdateComplete(serverId, payload);
      }
      if (!options.silent) toast.success(`Mock 模式: ${getDockerTaskDisplayTitle(mockTask)}已完成`);
      return { ok: true, taskId };
    }

    setDockerActionPending(prev => ({ ...prev, [pendingKey]: true }));
    try {
      const resourceProxyRequest = getDockerProxyRequest(serverId, action, payload);
      if (resourceProxyRequest && !getDockerProxyContainerAction(action)) {
        const res = await fetch(resourceProxyRequest.url, resourceProxyRequest.options);
        const data = await res.json().catch(() => ({}));
        if (!res.ok || !data.success) {
          throw new Error(data.error || data.message || 'Docker operation failed');
        }
        const pruneActions = ['image.prune', 'network.prune', 'volume.prune'];
        appendDockerInlineTask({
          state: 'success',
          action,
          serverId,
          payload,
          silent: options.silent,
          message: pruneActions.includes(action)
            ? data?.data?.message || data?.message || ''
            : data?.message || `${getDockerTaskActionLabel(action)}执行成功`,
        });
        if (!options.silent) {
          toast.success(
            pruneActions.includes(action)
              ? formatDockerPruneResult(action, data)
              : `${getDockerTaskActionLabel(action)}执行成功`
          );
        }
        clearDockerActionPending(serverId, action, payload);
        scheduleDockerResourceRefresh(400);
        return { ok: true, data };
      }

      const proxyAction = getDockerProxyContainerAction(action);
      if (proxyAction) {
        const containerId = payload.containerId;
        if (!containerId) {
          throw new Error('缺少容器 ID');
        }
        const res = await fetch(
          `/api/server/v2/docker/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(containerId)}/${proxyAction}`,
          {
            method: 'POST',
          }
        );
        const data = await res.json().catch(() => ({}));
        if (!res.ok || !data.success) {
          throw new Error(data.error || data.message || 'Docker 操作失败');
        }
        appendDockerInlineTask({
          state: 'success',
          action,
          serverId,
          payload,
          silent: options.silent,
          message: data?.message || `${getDockerTaskActionLabel(action)}执行成功`,
        });
        if (!options.silent) toast.success(`${getDockerTaskActionLabel(action)}执行成功`);
        clearDockerActionPending(serverId, action, payload);
        scheduleDockerResourceRefresh(400);
        return { ok: true };
      }

      const res = await fetch('/api/server/v2/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          serverId,
          domain: 'docker',
          action,
          payload,
        }),
      });
      const data = await res.json();
      if (data.success) {
        const taskId = data.taskId || data.data?.taskId;
        rememberDockerTaskMeta(taskId, {
          serverId,
          action,
          payload,
          silent: options.silent,
        });
        if (!options.silent) {
          toast.info(`${getDockerTaskDisplayTitle({ action, payload })} 已提交，正在等待后台调度`);
        }
        return { ok: true, taskId };
      } else {
        toast.error('提交失败: ' + data.error);
        setDockerActionPending(prev => {
          const next = { ...prev };
          delete next[pendingKey];
          return next;
        });
        return { ok: false };
      }
    } catch (e) {
      appendDockerInlineTask({
        state: 'failed',
        action,
        serverId,
        payload,
        silent: options.silent,
        error: e?.message || '服务下发异常',
        message: e?.message || '服务下发异常',
      });
      toast.error(e?.message || '服务下发异常');
      setDockerActionPending(prev => {
        const next = { ...prev };
        delete next[pendingKey];
        return next;
      });
      return { ok: false };
    }
  };

  const loadDockerAutoUpdate = useCallback(async () => {
    try {
      const [policyResponse, resultsResponse] = await Promise.all([
        fetch('/api/server/v2/docker/auto-update/policy'),
        fetch('/api/server/v2/docker/auto-update/results'),
      ]);
      const policyPayload = await policyResponse.json().catch(() => ({}));
      const resultsPayload = await resultsResponse.json().catch(() => ({}));
      if (!policyResponse.ok || !policyPayload.success) return;
      setDockerAutoUpdate({
        policy: policyPayload.data?.policy || null,
        running: Boolean(policyPayload.data?.running),
        lastRunAt: policyPayload.data?.lastRunAt || null,
        lastRunStatus: policyPayload.data?.lastRunStatus || '',
        lastRunSummary: policyPayload.data?.lastRunSummary || null,
        results: resultsResponse.ok && resultsPayload.success ? resultsPayload.data || [] : [],
      });
    } catch {
      // 自动更新面板失败不阻塞容器主流程
    }
  }, []);

  const saveDockerAutoUpdatePolicy = useCallback(async policy => {
    setDockerAutoUpdateBusy(true);
    try {
      const response = await fetch('/api/server/v2/docker/auto-update/policy', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(policy),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || !payload.success) {
        throw new Error(payload.error || '保存容器自动更新策略失败');
      }
      setDockerAutoUpdate(prev => ({
        ...(prev || {}),
        policy: payload.data?.policy || policy,
        running: Boolean(payload.data?.running),
        lastRunAt: payload.data?.lastRunAt || prev?.lastRunAt || null,
        lastRunStatus: payload.data?.lastRunStatus || prev?.lastRunStatus || '',
        lastRunSummary: payload.data?.lastRunSummary || prev?.lastRunSummary || null,
        results: prev?.results || [],
      }));
      toast.success('容器自动更新策略已保存');
      return true;
    } catch (error) {
      toast.error(error.message || '保存容器自动更新策略失败');
      return false;
    } finally {
      setDockerAutoUpdateBusy(false);
    }
  }, []);

  const runDockerAutoUpdate = useCallback(async () => {
    setDockerAutoUpdateBusy(true);
    try {
      const response = await fetch('/api/server/v2/docker/auto-update/run', { method: 'POST' });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || !payload.success) {
        throw new Error(payload.error || '触发容器自动更新失败');
      }
      toast.success('已触发容器自动更新,正在后台执行');
      await loadDockerAutoUpdate();
      return true;
    } catch (error) {
      toast.error(error.message || '触发容器自动更新失败');
      return false;
    } finally {
      setDockerAutoUpdateBusy(false);
    }
  }, [loadDockerAutoUpdate]);

  const dockerAutoUpdateResultMap = useMemo(() => {
    const map = new Map();
    (dockerAutoUpdate?.results || []).forEach(result => {
      if (!result?.serverId || !result?.containerId) return;
      map.set(`${result.serverId}::${result.containerId}`, result);
    });
    return map;
  }, [dockerAutoUpdate]);

  useEffect(() => {
    if (!dockerAutoUpdate?.running) return undefined;
    const timer = setInterval(() => {
      loadDockerAutoUpdate();
    }, 4000);
    return () => clearInterval(timer);
  }, [dockerAutoUpdate?.running, loadDockerAutoUpdate]);

  const loadDockerResources = async (options = {}) => {
    if (isDockerMockPreviewEnabled()) {
      const mock = createMockDockerOverview();
      setDockerOverviewServers(mock.servers);
      setDockerImages(
        mock.servers.flatMap(s =>
          s.resources.images.map(img => ({ ...img, serverName: s.name, serverId: s.id }))
        )
      );
      setDockerNetworks(
        mock.servers.flatMap(s =>
          s.resources.networks.map(n => ({ ...n, serverName: s.name, serverId: s.id }))
        )
      );
      setDockerVolumes(
        mock.servers.flatMap(s =>
          s.resources.volumes.map(v => ({ ...v, serverName: s.name, serverId: s.id }))
        )
      );
      setDockerStats(
        mock.servers.flatMap(s =>
          s.resources.stats.map(stat => ({ ...stat, serverName: s.name, serverId: s.id }))
        )
      );
      setDockerComposeProjects(
        mock.servers.flatMap(s =>
          s.resources.composeProjects.map(p => ({ ...p, serverName: s.name, serverId: s.id }))
        )
      );
      mock.updateChecks.forEach(check => storeDockerUpdateChecks(check.serverId, [check], check));
      setDockerTasks(prev =>
        prev.length > 0
          ? prev
          : [
              {
                taskId: 'mock-check-updates',
                state: 'success',
                progress: 100,
                action: 'container.checkUpdates',
                domain: 'docker',
                message: JSON.stringify(
                  mock.updateChecks.map(check => ({
                    container_id: check.containerId,
                    container_name: check.containerName,
                    image: check.image,
                    current_digest: check.currentDigest,
                    latest_digest: check.latestDigest,
                    has_update: check.hasUpdate,
                  }))
                ),
              },
            ]
      );

      const mockUpdateServer = mock.servers[0];
      const mockUpdateContainer =
        asArray(mockUpdateServer?.resources?.containers).find(item => item.name === 'api-monitor') ||
        asArray(mockUpdateServer?.resources?.containers)[0];
      if (mockUpdateServer && mockUpdateContainer) {
        const mockUpdatePayload = {
          serverId: mockUpdateServer.id,
          containerId: getDockerContainerId(mockUpdateContainer),
          containerName: getDockerContainerName(mockUpdateContainer),
          image: getDockerContainerImage(mockUpdateContainer),
        };
        setDockerActionPending(prev => ({
          ...prev,
          [getDockerActionKey(mockUpdateServer.id, 'container.update', mockUpdatePayload)]: true,
        }));
        setDockerTasks(prev =>
          [
            {
              taskId: 'mock-updating-container',
              state: 'running',
              progress: 45,
              action: 'container.update',
              domain: 'docker',
              serverId: mockUpdateServer.id,
              serverName: mockUpdateServer.name,
              payload: mockUpdatePayload,
              targetName: mockUpdateContainer.name,
              message: 'Mock 模式:容器更新进行中',
            },
            ...prev.filter(task => task.taskId !== 'mock-updating-container'),
          ].slice(0, 30)
        );
      }

      setDockerTaskStreamConnected(true);
      setDockerResourceLoading(false);
      return;
    }
    setDockerResourceLoading(true);
    const requestId = ++dockerResourcesRequestIdRef.current;
    try {
      const params = new URLSearchParams();
      params.set('scope', getDockerOverviewScope(dockerSubTab));

      const response = await fetch(`/api/server/v2/docker/overview?${params.toString()}`);
      const data = await response.json();
      if (dockerResourcesRequestIdRef.current !== requestId) return;
      if (data.success) {
        const servers = (data.data?.servers || [])
          .map(normalizeDockerOverviewServer)
          .filter(s => s.docker?.installed);
        setDockerOverviewServers(servers);

        setDockerImages(
          servers.flatMap(s =>
            s.resources.images.map(img => ({ ...img, serverName: s.name, serverId: s.id }))
          )
        );
        setDockerNetworks(
          servers.flatMap(s =>
            s.resources.networks.map(n => ({ ...n, serverName: s.name, serverId: s.id }))
          )
        );
        setDockerVolumes(
          servers.flatMap(s =>
            s.resources.volumes.map(v => ({ ...v, serverName: s.name, serverId: s.id }))
          )
        );
        setDockerStats(
          servers.flatMap(s =>
            s.resources.stats.map(stat => ({ ...stat, serverName: s.name, serverId: s.id }))
          )
        );
        setDockerComposeProjects(
          servers.flatMap(s =>
            s.resources.composeProjects.map(p => ({ ...p, serverName: s.name, serverId: s.id }))
          )
        );
      } else {
        setDockerOverviewServers([]);
        setDockerImages([]);
        setDockerNetworks([]);
        setDockerVolumes([]);
        setDockerStats([]);
        setDockerComposeProjects([]);
      }
    } catch (e) {
      console.error(e);
      if (dockerResourcesRequestIdRef.current !== requestId) return;
      if (!options.silent) {
        setDockerOverviewServers([]);
        setDockerImages([]);
        setDockerNetworks([]);
        setDockerVolumes([]);
        setDockerStats([]);
        setDockerComposeProjects([]);
      }
    } finally {
      if (dockerResourcesRequestIdRef.current === requestId) setDockerResourceLoading(false);
    }
  };

  const dockerContainerManagementServers = useMemo(
    () =>
      dockerOverviewServers
        .map(server => ({
          ...server,
          resources: {
            ...server.resources,
            containers: asArray(server.resources?.containers),
          },
        }))
        .filter(server => asArray(server.resources?.containers).length > 0),
    [dockerOverviewServers]
  );

  const visibleDockerContainerServers = useMemo(() => {
    const query = dockerSearchQuery.trim().toLowerCase();
    const filterContainers = server => {
      const containers = asArray(server.resources?.containers).filter(container => {
        const state = getDockerContainerState(container);
        const updateCheck = getDockerContainerUpdateCheck(server.id, container);
        if (dockerContainerStateFilter === 'updatable') {
          if (!updateCheck?.hasUpdate) return false;
        } else if (dockerContainerStateFilter !== 'all' && state !== dockerContainerStateFilter) {
          return false;
        }
        if (!query) return true;
        return [
          getDockerContainerName(container),
          getDockerContainerImage(container),
          getDockerContainerId(container),
          getDockerContainerPorts(container),
          server.name,
        ].some(value =>
          String(value || '')
            .toLowerCase()
            .includes(query)
        );
      });
      return {
        ...server,
        resources: {
          ...server.resources,
          containers,
        },
      };
    };

    const servers = dockerOverviewServers.map(filterContainers);
    return servers.filter(server => asArray(server.resources?.containers).length > 0);
  }, [dockerOverviewServers, dockerSearchQuery, dockerContainerStateFilter]);

  const dockerStatsChartData = useMemo(() => {
    const isDarkMode = theme === 'dark';
    const netInColor = ChartPalette.categorical(0, isDarkMode);
    const netOutColor = ChartPalette.semantic('Success', isDarkMode);
    const readColor = ChartPalette.semantic('Warning', isDarkMode);
    const writeColor = ChartPalette.categorical(3, isDarkMode);
    return {
      cpu: buildDockerStatSeries(dockerStatsHistory, 'cpu', isDarkMode),
      memory: buildDockerStatSeries(dockerStatsHistory, 'memory', isDarkMode),
      network: buildDockerPairSeries(dockerStatsHistory, [
        { key: 'netIn', name: '接收', color: netInColor },
        { key: 'netOut', name: '发送', color: netOutColor },
      ]),
      disk: buildDockerPairSeries(dockerStatsHistory, [
        { key: 'blockRead', name: '读取', color: readColor },
        { key: 'blockWrite', name: '写入', color: writeColor },
      ]),
    };
  }, [dockerStatsHistory, theme]);

  const dockerStatsSummary = useMemo(() => {
    const latest = dockerStatsHistory[dockerStatsHistory.length - 1] || [];
    return latest.reduce(
      (summary, item) => ({
        cpu: summary.cpu + toNumber(item.cpu, 0),
        memory: summary.memory + toNumber(item.memory, 0),
        netIn: summary.netIn + toNumber(item.netIn, 0),
        netOut: summary.netOut + toNumber(item.netOut, 0),
        blockRead: summary.blockRead + toNumber(item.blockRead, 0),
        blockWrite: summary.blockWrite + toNumber(item.blockWrite, 0),
      }),
      { cpu: 0, memory: 0, netIn: 0, netOut: 0, blockRead: 0, blockWrite: 0 }
    );
  }, [dockerStatsHistory]);

  const dockerResourceSummary = useMemo(
    () => ({
      compose: dockerComposeProjects.length,
      images: dockerImages.length,
      networks: dockerNetworks.length,
      volumes: dockerVolumes.length,
      stats: dockerStats.length,
    }),
    [
      dockerComposeProjects.length,
      dockerImages.length,
      dockerNetworks.length,
      dockerVolumes.length,
      dockerStats.length,
    ]
  );

  const renderDockerSimpleLogCard = (className = '') => {
    const tasks = dockerTasks.map(decorateDockerTask);
    const visibleTasks = tasks.slice(0, 8);
    return (
      <LayerCard className={`overflow-hidden p-0 ${className}`}>
        <LayerCard.Secondary className={SERVER_SECTION_HEADER_CLASS}>
          <span className="inline-flex min-w-0 items-center gap-2 text-xs font-semibold text-kumo-strong">
            <Activity className="h-4 w-4 shrink-0 text-brand" />
            日志
          </span>
          <span className="flex shrink-0 items-center gap-1.5">
            <Badge variant={dockerTaskStreamConnected ? 'success' : 'warning'} appearance="dot">
              {dockerTaskStreamConnected ? '实时连接' : '重连中'}
            </Badge>
            <Badge variant="neutral">{tasks.length} 条</Badge>
            <Button
              size="sm"
              variant="secondary"
              icon={
                showDockerLogPanel ? (
                  <ChevronUp className="h-3.5 w-3.5" />
                ) : (
                  <ChevronDown className="h-3.5 w-3.5" />
                )
              }
              onClick={() => setShowDockerLogPanel(prev => !prev)}
              aria-label={showDockerLogPanel ? '收起日志' : '展开日志'}
              title={showDockerLogPanel ? '收起日志' : '展开日志'}
            >
              {showDockerLogPanel ? '收起' : '展开'}
            </Button>
          </span>
        </LayerCard.Secondary>
        {showDockerLogPanel && (
          <LayerCard.Primary className="p-3">
            {tasks.length === 0 ? (
              <div className="text-xs text-kumo-subtle">暂无后台任务</div>
            ) : (
              <div className="flex max-h-64 flex-col gap-1 overflow-y-auto pr-1 scrollbar-thin">
                {visibleTasks.map(task => {
                  const progress = clampPercent(toNumber(task.progress, 0));
                  const showProgress = !isDockerTaskFinalState(task.state) && progress > 0;
                  const summary = summarizeDockerTaskMessage(task);
                  return (
                    <div
                      key={task.taskId}
                      className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5"
                    >
                      <div className="flex min-w-0 items-center gap-1.5">
                        <Badge variant={getDockerTaskStateVariant(task.state)} appearance="dot">
                          {getDockerTaskStateLabel(task.state)}
                        </Badge>
                        <span
                          className="min-w-0 flex-1 truncate text-[11px] leading-4 font-semibold text-kumo-strong"
                          title={getDockerTaskDisplayTitle(task)}
                        >
                          {getDockerTaskDisplayTitle(task)}
                        </span>
                        {showProgress && (
                          <span className="shrink-0 text-[10px] font-semibold text-brand">
                            {progress}%
                          </span>
                        )}
                      </div>
                      {showProgress && (
                        <Meter
                          label={`${getDockerTaskDisplayTitle(task)}进度`}
                          value={progress}
                          showValue={false}
                          className="mt-1 gap-0"
                          trackClassName="!h-1 overflow-hidden rounded-full bg-kumo-base"
                          indicatorClassName="!h-full !bg-none !bg-brand"
                        />
                      )}
                      {summary && (
                        <div
                          className="mt-0.5 truncate text-[10px] leading-4 text-kumo-subtle"
                          title={summary}
                        >
                          {summary}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </LayerCard.Primary>
        )}
      </LayerCard>
    );
  };

  const renderDockerResourceSideRail = ({
    title,
    icon = <Settings className="h-4 w-4 shrink-0 text-brand" />,
    hosts = [],
    totalCount = 0,
    countLabel = '数量',
    summaryItems = [],
    actions = null,
    getHostCount = server => getDockerOverviewResourceCount(server, dockerSubTab),
    getHostBadges = () => [],
    renderHostAction = null,
  }) => {
    const statItems = [
      { label: countLabel, value: totalCount, className: 'text-kumo-strong' },
      ...summaryItems,
      { label: '主机', value: hosts.length, className: 'text-kumo-subtle' },
    ].slice(0, 4);
    return (
      <div className="flex min-w-0 flex-col gap-3 cq-xl:sticky cq-xl:top-0 cq-xl:self-start">
        <LayerCard className="overflow-hidden p-0">
          <LayerCard.Secondary className={SERVER_SECTION_HEADER_CLASS}>
            <span className="inline-flex min-w-0 items-center gap-2 text-xs font-semibold text-kumo-strong">
              <Settings className="h-4 w-4 shrink-0 text-brand" />
              管理
            </span>
            <span className="flex shrink-0 items-center gap-1.5">
              <Badge variant="neutral">{hosts.length} 主机</Badge>
              <Button
                shape="square"
                size="sm"
                variant="secondary"
                icon={
                  <RefreshCw
                    className={`h-3.5 w-3.5 ${dockerResourceLoading ? 'animate-spin' : ''}`}
                  />
                }
                disabled={dockerResourceLoading}
                onClick={loadDockerResources}
                aria-label="刷新 Docker 数据"
                title="刷新 Docker 数据"
              />
              {actions}
            </span>
          </LayerCard.Secondary>
          <LayerCard.Primary className="space-y-3 p-3">
            <div className="grid grid-cols-4 gap-1.5">
              {statItems.map(item => (
                <div
                  key={item.label}
                  className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5"
                >
                  <div className="text-[10px] text-kumo-subtle">{item.label}</div>
                  <div
                    className={`mt-0.5 truncate text-sm font-semibold ${item.className || 'text-kumo-strong'}`}
                  >
                    {item.value}
                  </div>
                </div>
              ))}
            </div>

            <div className="flex flex-col gap-2">
              {hosts.map(server => {
                const isOpen = isDockerHostExpanded(server.id, hosts[0]?.id);
                const hostBadges = getHostBadges(server);
                return (
                  <div
                    key={`${title}-${server.id}`}
                    className={`overflow-hidden rounded-md border ${isOpen ? 'border-brand/55 bg-brand/5' : 'border-kumo-line/80 bg-kumo-base'}`}
                  >
                    <div
                      role="button"
                      tabIndex={0}
                      onClick={() => focusDockerResourceHost(server.id, hosts[0]?.id)}
                      onKeyDown={event => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault();
                          focusDockerResourceHost(server.id, hosts[0]?.id);
                        }
                      }}
                      className="flex min-h-10 cursor-pointer items-center justify-between gap-2 px-2.5 py-2"
                    >
                      <span className="flex min-w-0 items-center gap-2">
                        {React.cloneElement(icon, { className: 'h-3.5 w-3.5 shrink-0 text-brand' })}
                        <span className="min-w-0 truncate text-xs font-semibold text-kumo-strong">
                          {server.name}
                        </span>
                      </span>
                      <span className="flex shrink-0 items-center gap-1.5">
                        <Badge variant="neutral">{getHostCount(server)}</Badge>
                        {hostBadges.slice(0, 1).map(badge => (
                          <Badge
                            key={badge.label}
                            variant={badge.variant || 'neutral'}
                            appearance={badge.appearance}
                          >
                            {badge.label}
                          </Badge>
                        ))}
                      </span>
                    </div>

                    {(hostBadges.length > 0 || renderHostAction) && (
                      <AnimatedCollapse open={isOpen} keepMounted>
                        <div className="border-t border-kumo-line/70 px-2.5 py-2">
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <div className="flex flex-wrap gap-1.5">
                              {hostBadges.map(badge => (
                                <Badge
                                  key={badge.label}
                                  variant={badge.variant || 'neutral'}
                                  appearance={badge.appearance}
                                >
                                  {badge.label}
                                </Badge>
                              ))}
                            </div>
                            {renderHostAction?.(server)}
                          </div>
                        </div>
                      </AnimatedCollapse>
                    )}
                  </div>
                );
              })}
            </div>
          </LayerCard.Primary>
        </LayerCard>
        {renderDockerSimpleLogCard()}
      </div>
    );
  };

  const getDockerContainerSummary = (containers = [], serverId = '') => {
    const list = asArray(containers);
    return list.reduce(
      (summary, container) => {
        const state = getDockerContainerState(container);
        if (state === 'running') summary.running += 1;
        else if (state === 'paused') summary.paused += 1;
        else summary.stopped += 1;
        const updateCheck = serverId ? getDockerContainerUpdateCheck(serverId, container) : null;
        if (updateCheck?.hasUpdate) summary.updatable += 1;
        return summary;
      },
      { total: list.length, running: 0, paused: 0, stopped: 0, updatable: 0 }
    );
  };

  const getUpdatableDockerContainers = (servers, serverId = '') =>
    servers.flatMap(server => {
      if (serverId && String(server.id) !== String(serverId)) return [];
      return asArray(server.resources?.containers)
        .filter(container => getDockerContainerUpdateCheck(server.id, container)?.hasUpdate)
        .map(container => ({
          server,
          container,
          payload: {
            serverId: server.id,
            containerId: getDockerContainerId(container),
            containerName: getDockerContainerName(container),
            image: getDockerContainerImage(container),
          },
        }));
    });

  const getVisibleUpdatableDockerContainers = () =>
    getUpdatableDockerContainers(visibleDockerContainerServers);

  const visibleUpdatableDockerContainers = useMemo(
    () => getVisibleUpdatableDockerContainers(),
    [visibleDockerContainerServers, dockerUpdateChecks]
  );
  const dockerContainerManagementUpdatableContainers = useMemo(
    () => getUpdatableDockerContainers(dockerContainerManagementServers),
    [dockerContainerManagementServers, dockerUpdateChecks]
  );
  const dockerSelectedContainerKeySet = useMemo(
    () => new Set(dockerSelectedContainerKeys),
    [dockerSelectedContainerKeys]
  );
  const selectedUpdatableDockerContainers = useMemo(
    () =>
      visibleUpdatableDockerContainers.filter(item =>
        dockerSelectedContainerKeySet.has(
          getDockerContainerSelectionKey(item.server.id, item.payload)
        )
      ),
    [visibleUpdatableDockerContainers, dockerSelectedContainerKeySet]
  );
  const visibleUpdatableDockerContainerKeySet = useMemo(
    () =>
      new Set(
        visibleUpdatableDockerContainers.map(item =>
          getDockerContainerSelectionKey(item.server.id, item.payload)
        )
      ),
    [visibleUpdatableDockerContainers]
  );

  useEffect(() => {
    setDockerSelectedContainerKeys(prev =>
      prev.filter(key => visibleUpdatableDockerContainerKeySet.has(key))
    );
  }, [visibleUpdatableDockerContainerKeySet]);
  const dockerActiveBatchUpdateTargets =
    selectedUpdatableDockerContainers.length > 0
      ? selectedUpdatableDockerContainers
      : visibleUpdatableDockerContainers;
  const dockerActiveBatchUpdateScopeName =
    selectedUpdatableDockerContainers.length > 0 ? '选中容器' : '当前筛选';
  const dockerActiveBatchUpdateConfirmKey = useMemo(
    () =>
      `batch.update::${dockerActiveBatchUpdateScopeName}::${dockerActiveBatchUpdateTargets.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`,
    [dockerActiveBatchUpdateScopeName, dockerActiveBatchUpdateTargets]
  );
  const dockerManagementBatchUpdateConfirmKey = useMemo(
    () =>
      `batch.update::全部主机::${dockerContainerManagementUpdatableContainers.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`,
    [dockerContainerManagementUpdatableContainers]
  );

  const hasDockerResourceData =
    dockerOverviewServers.length > 0 ||
    dockerImages.length > 0 ||
    dockerNetworks.length > 0 ||
    dockerVolumes.length > 0 ||
    dockerStats.length > 0 ||
    dockerComposeProjects.length > 0;
  const showDockerBlockingLoading = dockerResourceLoading && !hasDockerResourceData;
  const dockerContainerTotals = visibleDockerContainerServers.reduce(
    (totals, server) => {
      const summary = getDockerContainerSummary(server.resources?.containers, server.id);
      totals.total += summary.total;
      totals.running += summary.running;
      totals.paused += summary.paused;
      totals.stopped += summary.stopped;
      totals.updatable += summary.updatable;
      return totals;
    },
    { total: 0, running: 0, paused: 0, stopped: 0, updatable: 0 }
  );
  const dockerContainerManagementTotals = dockerContainerManagementServers.reduce(
    (totals, server) => {
      const summary = getDockerContainerSummary(server.resources?.containers, server.id);
      totals.total += summary.total;
      totals.running += summary.running;
      totals.paused += summary.paused;
      totals.stopped += summary.stopped;
      totals.updatable += summary.updatable;
      return totals;
    },
    { total: 0, running: 0, paused: 0, stopped: 0, updatable: 0 }
  );

  useEffect(() => {
    if (dockerSubTab !== 'containers') return;
    const hostIds = visibleDockerContainerServers.map(server => server.id);
    setExpandedDockerOverviewServers(prev =>
      prev === null ? prev : prev.filter(id => hostIds.includes(id))
    );
  }, [dockerSubTab, visibleDockerContainerServers]);

  // 反代域名只是容器页的可选增强：1Panel 未连接/接口失败时静默跳过，
  // 不影响容器列表本身。只在容器页可见时拉取，避免无谓请求。
  const dockerReverseProxyHostIds = useMemo(
    () => (dockerSubTab === 'containers' ? dockerContainerManagementServers.map(s => s.id) : []),
    [dockerSubTab, dockerContainerManagementServers]
  );

  useEffect(() => {
    const hostIds = dockerReverseProxyHostIds;
    if (hostIds.length === 0) {
      setDockerReverseProxyDomains({});
      return;
    }
    let cancelled = false;
    const load = async () => {
      const entries = await Promise.all(
        hostIds.map(async serverId => {
          try {
            const payload = await getOnepanelReverseProxyDomains(serverId);
            return [serverId, asArray(payload?.data?.targets)];
          } catch {
            return [serverId, []];
          }
        })
      );
      if (cancelled) return;
      setDockerReverseProxyDomains(Object.fromEntries(entries));
    };
    load();
    return () => {
      cancelled = true;
    };
    // hostIds 的加入顺序稳定，用 join 作为依赖避免每次渲染都重拉。
  }, [dockerReverseProxyHostIds.join('|')]);

  useEffect(() => {
    if (!['compose', 'images', 'networks', 'volumes', 'stats'].includes(dockerSubTab)) return;
    const hostIds = dockerOverviewServers
      .filter(server => isDockerOverviewHostVisible(server, dockerSubTab))
      .map(server => server.id);
    setExpandedDockerOverviewServers(prev =>
      prev === null ? prev : prev.filter(id => hostIds.includes(id))
    );
  }, [dockerSubTab, dockerOverviewServers]);

  const toggleDockerOverviewServer = (serverId, autoExpandedServerId) => {
    setExpandedDockerOverviewServers(prev => {
      if (prev === null) {
        // 自动模式：点击自动展开的第一台 = 收起（进入手动全收起）；
        // 点击其它 = 只展开点击的那台（类似手风琴，第一台随之收起）。
        return autoExpandedServerId === serverId ? [] : [serverId];
      }
      return prev.includes(serverId) ? prev.filter(id => id !== serverId) : [...prev, serverId];
    });
  };

  const toggleDockerContainerSelection = (selectionKey, checked) => {
    setDockerSelectedContainerKeys(prev => {
      if (checked) return prev.includes(selectionKey) ? prev : [...prev, selectionKey];
      return prev.filter(key => key !== selectionKey);
    });
  };

  const selectDockerContainerScope = (serverId, filter = 'all') => {
    setDockerSelectedServer(serverId || '');
    setDockerContainerStateFilter(filter);
    if (serverId) setExpandedDockerOverviewServers([serverId]);
  };

  const renderDockerFilterChip = (serverId, filter, label, variant = 'neutral', title = '') => {
    const active =
      String(dockerSelectedServer || '') === String(serverId || '') &&
      dockerContainerStateFilter === filter;
    const toneClass = active
      ? ''
      : variant === 'success'
        ? '!border-kumo-success/45 !bg-kumo-success/10 !text-kumo-success hover:!bg-kumo-success/15'
        : variant === 'warning'
          ? '!border-kumo-warning/55 !bg-kumo-warning/12 !text-kumo-warning hover:!bg-kumo-warning/18'
          : variant === 'error'
            ? '!border-kumo-danger/50 !bg-kumo-danger/10 !text-kumo-danger hover:!bg-kumo-danger/15'
            : '!border-kumo-interact/70 !bg-kumo-recessed/35 !text-kumo-strong hover:!bg-kumo-recessed/55';
    return (
      <Button
        type="button"
        size="sm"
        variant={active ? 'primary' : 'secondary'}
        onClick={event => {
          event.stopPropagation();
          selectDockerContainerScope(serverId, filter);
        }}
        title={title || label}
        className={`!h-7 !rounded-full !px-3 !text-xs !font-semibold !shadow-none ${toneClass}`}
      >
        {label}
      </Button>
    );
  };

  const batchUpdateVisibleDockerContainers = async (
    serverId = '',
    useAllServers = false,
    explicitTargets = null,
    options = {}
  ) => {
    const sourceServers =
      serverId || useAllServers ? dockerContainerManagementServers : visibleDockerContainerServers;
    const targets = Array.isArray(explicitTargets)
      ? explicitTargets
      : getUpdatableDockerContainers(sourceServers, serverId);
    const scopeName = serverId
      ? dockerContainerManagementServers.find(server => String(server.id) === String(serverId))
          ?.name ||
        visibleDockerContainerServers.find(server => String(server.id) === String(serverId))
          ?.name ||
        '当前主机'
      : options.scopeName || (useAllServers ? '全部主机' : '当前筛选');
    if (targets.length === 0) {
      toast.warning(`${scopeName}下没有已检测出的可更新容器`);
      return;
    }
    const confirmKey =
      options.confirmKey ||
      `batch.update::${scopeName}::${targets.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`;
    if (
      !options.skipDoubleConfirm &&
      !confirmDockerUpdatePress(confirmKey, `更新${scopeName} ${targets.length} 个容器`)
    )
      return;

    const queue = [...targets];
    let submitted = 0;
    let failed = 0;
    const workers = Array.from({ length: Math.min(3, queue.length) }, async () => {
      while (queue.length > 0) {
        const item = queue.shift();
        try {
          const result = await submitDockerTask('container.update', item.payload, {
            skipConfirm: true,
            silent: true,
          });
          if (result?.ok) submitted += 1;
          else failed += 1;
        } catch (error) {
          failed += 1;
        }
      }
    });
    await Promise.all(workers);

    if (failed > 0) {
      toast.warning(`已提交 ${submitted} 个更新任务，${failed} 个提交失败`);
    } else {
      toast.success(`已提交 ${submitted} 个容器更新任务`);
    }
  };

  const setDockerUpdateLoadingForAliases = (serverId, fallback, loading) => {
    const aliases = getDockerUpdateAliases(serverId, fallback);
    if (aliases.length === 0) return;

    setDockerUpdateCheckLoading(prev => {
      const next = { ...prev };
      aliases.forEach(key => {
        if (loading) {
          next[key] = true;
        } else {
          delete next[key];
        }
      });
      return next;
    });
  };

  const checkDockerUpdatesForServer = async (server, container = null, options = {}) => {
    const serverId = typeof server === 'object' ? server?.id : server;
    if (!serverId) {
      toast.warning('请先选择一台主机');
      return { ok: false, error: 'missing serverId', results: [] };
    }

    const fallback = container
      ? {
          containerId: getDockerContainerId(container),
          containerName: getDockerContainerName(container),
          image: getDockerContainerImage(container),
        }
      : {};

    setDockerUpdateLoadingForAliases(serverId, fallback, true);

    try {
      const response = await fetch('/api/server/docker/check-update', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          serverId,
          containerId: fallback.containerId || '',
        }),
      });
      const data = await response.json();
      if (!response.ok || !data.success) {
        throw new Error(data.error || '检查更新失败');
      }

      const results = Array.isArray(data.data) ? data.data : data.data ? [data.data] : [];
      if (results.length > 0) {
        storeDockerUpdateChecks(serverId, results, fallback);
      } else if (container) {
        storeDockerUpdateChecks(
          serverId,
          [
            {
              ...fallback,
              error: '未返回检测结果',
            },
          ],
          fallback
        );
      }

      const failedResults = results.filter(result => getDockerUpdateResultError(result));
      if (!options.silent) {
        const updateCount = results.filter(item => item?.has_update || item?.hasUpdate).length;
        const hostTag = server?.name ? `[${server.name}] ` : '';
        if (failedResults.length > 0) {
          const target =
            failedResults[0]?.container_name ||
            failedResults[0]?.containerName ||
            fallback.containerName ||
            '容器';
          toast.error(
            `${hostTag}${target} 检测失败：${getDockerUpdateResultError(failedResults[0])}`
          );
        } else if (updateCount > 0) {
          toast.warning(`${hostTag}检测完成，发现 ${updateCount} 个可更新镜像`);
        } else {
          toast.success(`${hostTag}检测完成，暂无可更新镜像`);
        }
      }

      return {
        ok: failedResults.length === 0,
        error: getDockerUpdateResultError(failedResults[0]),
        results,
      };
    } catch (error) {
      if (container) {
        storeDockerUpdateChecks(
          serverId,
          [
            {
              ...fallback,
              error: error.message || '检查更新失败',
            },
          ],
          fallback
        );
      }
      if (!options.silent) {
        toast.error(error.message || '检查更新失败');
      }
      return { ok: false, error: error.message || '检查更新失败', results: [] };
    } finally {
      setDockerUpdateLoadingForAliases(serverId, fallback, false);
    }
  };

  const checkDockerUpdatesForServers = async (
    servers,
    emptyMessage = '当前没有可检测的 Docker 容器'
  ) => {
    const targets = servers.filter(server => asArray(server.resources?.containers).length > 0);
    if (targets.length === 0) {
      toast.warning(emptyMessage);
      return;
    }

    setDockerBulkUpdateChecking(true);
    setDockerBulkUpdateCheckServers(Object.fromEntries(targets.map(server => [server.id, true])));

    try {
      const results = await Promise.all(
        targets.map(async server => {
          try {
            return await checkDockerUpdatesForServer(server, null, { silent: true });
          } catch (error) {
            return { ok: false, error: error?.message || '检查失败', results: [] };
          } finally {
            setDockerBulkUpdateCheckServers(prev => {
              const next = { ...prev };
              delete next[server.id];
              return next;
            });
          }
        })
      );
      const finished =
        results.length > 0
          ? results
          : targets.map(() => ({ ok: false, error: '检查失败', results: [] }));
      const updateCount = finished.reduce(
        (sum, item) =>
          sum + item.results.filter(result => result?.has_update || result?.hasUpdate).length,
        0
      );
      const failedCount = finished.filter(item => !item.ok).length;

      const targetTag = targets.length === 1 && targets[0]?.name ? `[${targets[0].name}] ` : '';
      if (failedCount > 0) {
        toast.warning(
          `${targetTag}检测完成，${updateCount} 个可更新，${failedCount} 台主机检测失败`
        );
      } else if (updateCount > 0) {
        toast.warning(`${targetTag}检测完成，发现 ${updateCount} 个可更新镜像`);
      } else {
        toast.success(`${targetTag}检测完成，暂无可更新镜像`);
      }
    } finally {
      setDockerBulkUpdateChecking(false);
      setDockerBulkUpdateCheckServers({});
    }
  };

  const checkVisibleDockerUpdates = async () =>
    checkDockerUpdatesForServers(
      visibleDockerContainerServers,
      '当前筛选下没有可检测的 Docker 容器'
    );

  const checkAllDockerUpdates = async () =>
    checkDockerUpdatesForServers(dockerContainerManagementServers, '当前没有可检测的 Docker 容器');

  const renderDockerEmptyState = message => (
    <AppCard padding="none" className="p-10 text-center text-xs text-kumo-subtle">
      {message}
    </AppCard>
  );

  const renderDockerHostResourceSection = ({
    server,
    icon,
    count = 0,
    countLabel = '项',
    badges = [],
    actions = null,
    children,
    isFirstVisible = false,
  }) => {
    const isOpen = isDockerHostExpanded(server.id, isFirstVisible ? server.id : null);
    return (
      <div
        id={`docker-resource-section-${server.id}`}
        className="scroll-mt-24 overflow-hidden rounded-lg border border-kumo-line bg-kumo-base"
      >
        <div
          role="button"
          tabIndex={0}
          onClick={() => toggleDockerOverviewServer(server.id, isFirstVisible ? server.id : null)}
          onKeyDown={event => {
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault();
              toggleDockerOverviewServer(server.id, isFirstVisible ? server.id : null);
            }
          }}
          className={`flex min-h-[52px] cursor-pointer flex-wrap items-center justify-between gap-2 px-3 py-3.5 ${
            isOpen ? 'border-b border-kumo-line/70' : ''
          }`}
        >
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            {icon}
            <span className="min-w-0 truncate text-xs font-semibold text-kumo-strong">
              {server.name}
            </span>
            <Badge variant="neutral">
              {count} {countLabel}
            </Badge>
            {badges.map(badge => (
              <Badge
                key={badge.label}
                variant={badge.variant || 'neutral'}
                appearance={badge.appearance}
              >
                {badge.label}
              </Badge>
            ))}
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            {actions}
            <Button
              size="sm"
              variant="secondary"
              icon={
                isOpen ? (
                  <ChevronUp className="h-3.5 w-3.5" />
                ) : (
                  <ChevronDown className="h-3.5 w-3.5" />
                )
              }
              onClick={event => {
                event.stopPropagation();
                toggleDockerOverviewServer(server.id, isFirstVisible ? server.id : null);
              }}
            >
              {isOpen ? '收起' : '展开'}
            </Button>
          </div>
        </div>
        <AnimatedCollapse open={isOpen} keepMounted>
          {children}
        </AnimatedCollapse>
      </div>
    );
  };

  const focusDockerResourceHost = (serverId, autoExpandedServerId) => {
    if (!serverId) return;
    setExpandedDockerOverviewServers(prev =>
      prev === null && autoExpandedServerId === serverId ? [] : [serverId]
    );
    window.setTimeout(() => {
      const target = document.getElementById(`docker-resource-section-${serverId}`);
      target?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }, 80);
  };

  const submitDockerPruneTask = async (action, payload, confirmKey, confirmLabel, options = {}) => {
    if (!confirmDockerUpdatePress(confirmKey, confirmLabel)) return { ok: false };
    return submitDockerTask(action, payload, { ...options, skipConfirm: true });
  };

  const pruneDockerImagesForHosts = async (hosts, options = {}) => {
    const targets = asArray(hosts).filter(server => server?.id);
    if (targets.length === 0) {
      toast.warning('当前没有可清理镜像的主机');
      return;
    }
    const confirmKey =
      options.confirmKey || `image.prune.all::${targets.map(server => server.id).join('|')}`;
    if (!confirmDockerUpdatePress(confirmKey, `清理 ${targets.length} 台主机未使用镜像`)) return;

    let ok = 0;
    let failed = 0;
    const queue = [...targets];
    const workers = Array.from({ length: Math.min(3, queue.length) }, async () => {
      while (queue.length > 0) {
        const server = queue.shift();
        const result = await submitDockerTask(
          'image.prune',
          { serverId: server.id },
          { skipConfirm: true, silent: true }
        );
        if (result?.ok) ok += 1;
        else failed += 1;
      }
    });
    await Promise.all(workers);

    if (failed > 0) {
      toast.warning(`已清理 ${ok} 台主机镜像，${failed} 台提交失败`);
    } else {
      toast.success(`已提交 ${ok} 台主机镜像清理`);
    }
  };

  const pruneDockerNetworksForHosts = async (hosts, options = {}) => {
    const targets = asArray(hosts).filter(server => server?.id);
    if (targets.length === 0) {
      toast.warning('当前没有可清理网络的主机');
      return;
    }
    const confirmKey =
      options.confirmKey || `network.prune.all::${targets.map(server => server.id).join('|')}`;
    if (!confirmDockerUpdatePress(confirmKey, `清理 ${targets.length} 台主机未使用网络`)) return;

    let ok = 0;
    let failed = 0;
    const queue = [...targets];
    const workers = Array.from({ length: Math.min(3, queue.length) }, async () => {
      while (queue.length > 0) {
        const server = queue.shift();
        const result = await submitDockerTask(
          'network.prune',
          { serverId: server.id },
          { skipConfirm: true, silent: true }
        );
        if (result?.ok) ok += 1;
        else failed += 1;
      }
    });
    await Promise.all(workers);

    if (failed > 0) {
      toast.warning(`已清理 ${ok} 台主机网络，${failed} 台提交失败`);
    } else {
      toast.success(`已提交 ${ok} 台主机网络清理`);
    }
  };

  const openDockerComposeConfig = async (server, project, mode = 'view') => {
    const projectName = getComposeProjectName(project);
    const configFiles = getComposeConfigFileList(project);
    const path = configFiles[0] || '';
    const baseEditor = {
      mode,
      serverId: server?.id || '',
      serverName: server?.name || '-',
      projectName,
      status: formatComposeStatusLabel(getComposeStatus(project)),
      workingDir: getComposeWorkingDir(project),
      configFiles,
      path,
      content: '',
      originalContent: '',
      loading: Boolean(path),
      saving: false,
      saved: false,
      updating: false,
      error: path ? '' : '配置文件已不存在（上次扫描时的路径已失效）',
    };
    setDockerComposeEditor(baseEditor);
    if (!path) return;

    if (isDockerMockPreviewEnabled()) {
      const mockContent = [
        'services:',
        `  ${projectName}:`,
        '    image: nginx:1.27-alpine',
        '    restart: unless-stopped',
        '    ports:',
        '      - "8080:80"',
        '',
      ].join('\n');
      setDockerComposeEditor(prev =>
        prev
          ? {
              ...prev,
              content: mockContent,
              originalContent: mockContent,
              loading: false,
            }
          : prev
      );
      return;
    }

    try {
      const data = await readSftpFile(server.id, path, 1024 * 1024);
      const content = data.data || '';
      setDockerComposeEditor(prev =>
        prev
          ? {
              ...prev,
              content,
              originalContent: content,
              loading: false,
              error: '',
            }
          : prev
      );
    } catch (error) {
      // 读不到时补一条「能访问哪些目录」的说明：Agent 文件白名单没配时
      // 原始报错是 No such file or directory，用户无法判断是路径错还是权限问题。
      const hint = await describeSftpAccessHint(server.id);
      const message = error.message || '读取 Compose 配置失败';
      setDockerComposeEditor(prev =>
        prev
          ? {
              ...prev,
              loading: false,
              error: hint ? `${message}（${hint}）` : message,
            }
          : prev
      );
      toast.error(message);
    }
  };

  // describeSftpAccessHint 查询主机可访问的文件根目录，生成一句可操作提示。
  // 失败时返回空串（不影响主流程，错误信息保持原样）。
  const describeSftpAccessHint = async serverId => {
    if (!serverId) return '';
    try {
      const roots = await getSftpRoots(serverId);
      const allowed = asArray(roots?.allowedRoots);
      if (roots?.transport === 'ssh') {
        return '当前走 SSH/SFTP 直连，可访问该用户有权限的路径';
      }
      if (allowed.length > 0) {
        return `Agent 可访问根目录: ${allowed.join(', ')}；如需访问其它目录，请配置 API_MONITOR_AGENT_FILE_ROOTS 后重装 Agent`;
      }
      return '';
    } catch {
      return '';
    }
  };

  const requestCloseDockerComposeEditor = async () => {
    if (
      dockerComposeEditor?.mode === 'edit' &&
      dockerComposeEditor.content !== dockerComposeEditor.originalContent
    ) {
      const ok = await dialog.confirm({
        title: '放弃未保存修改',
        message: `Compose 配置 ${dockerComposeEditor.path || ''} 还有未保存内容。`,
        confirmText: '放弃修改',
        cancelText: '继续编辑',
        variant: 'danger',
      });
      if (!ok) return;
    }
    setDockerComposeEditor(null);
  };

  const saveDockerComposeConfig = async () => {
    if (!dockerComposeEditor?.path || dockerComposeEditor.mode !== 'edit') return;
    setDockerComposeEditor(prev => (prev ? { ...prev, saving: true, error: '' } : prev));
    if (isDockerMockPreviewEnabled()) {
      setDockerComposeEditor(prev =>
        prev
          ? {
              ...prev,
              saving: false,
              mode: 'view',
              originalContent: prev.content,
              saved: true,
            }
          : prev
      );
      toast.success('Mock 模式: Compose 配置已保存');
      return;
    }

    try {
      await writeSftpFile(
        dockerComposeEditor.serverId,
        dockerComposeEditor.path,
        dockerComposeEditor.content
      );
      setDockerComposeEditor(prev =>
        prev
          ? {
              ...prev,
              saving: false,
              mode: 'view',
              originalContent: prev.content,
              saved: true,
            }
          : prev
      );
      toast.success('Compose 配置已保存');
      scheduleDockerResourceRefresh(500);
    } catch (error) {
      const hint = await describeSftpAccessHint(dockerComposeEditor.serverId);
      const message = error.message || '保存 Compose 配置失败';
      setDockerComposeEditor(prev =>
        prev
          ? {
              ...prev,
              saving: false,
              error: hint ? `${message}（${hint}）` : message,
            }
          : prev
      );
      toast.error(message);
    }
  };

  const openSSHTerminal = server => {
    if (!server) return;

    const existing = sshSessions.find(s => s.server.id === server.id);
    if (existing) {
      switchToSSHTab(existing.id);
      return;
    }

    let terminalServer = server;
    let type = resolveTerminalProtocol(server);
    if (!type && hasSshEndpoint(server)) {
      terminalServer = {
        ...server,
        ssh_configured: true,
        supports_ssh: true,
        preferred_terminal_transport: 'ssh',
      };
      type = 'ssh';
    }
    if (!type) {
      toast.warning('该主机当前没有可用的终端传输');
      return;
    }

    const sessionId = 'session_' + Date.now();

    const newSession = {
      id: sessionId,
      server: terminalServer,
      type,
      connected: false,
      name: terminalServer.name,
    };

    // 归还现有所有 xterm 节点到 warehouse 仓库
    saveTerminalsToWarehouse();

    setSshSessions(prev => [...prev, newSession]);

    if (sshViewLayout !== 'single') {
      // 如果处于分屏中，切回单屏（挂起当前组）
      setSshSplitTree(createLeafNode(sessionId));
      visibleSessionIdsRef.current = [sessionId];
      setVisibleSessionIds([sessionId]);
      setSshViewLayout('single');
    } else {
      setSshSplitTree(createLeafNode(sessionId));
      visibleSessionIdsRef.current = [sessionId];
      setVisibleSessionIds([sessionId]);
    }

    setActiveSSHSessionId(sessionId);
    setServerCurrentTab('terminal');

    // 延迟实例化 xterm.js
    setTimeout(() => initSessionTerminal(sessionId, newSession), 200);
  };

  // 切换标签
  const loadDockerContainerLogs = async (server, container, tail = 200) => {
    const serverId = server?.id || server;
    if (!serverId || !container) return;

    const containerId = getDockerContainerId(container);
    const containerName = getDockerContainerName(container);

    setDockerLogsLoading(true);
    setDockerLogsContent(
      '\u6b63\u5728\u8fde\u63a5\u4e3b\u673a\u5e76\u83b7\u53d6\u5bb9\u5668\u65e5\u5fd7...\n'
    );

    try {
      const params = new URLSearchParams();
      params.set('tail', String(Number(tail) || 200));
      const res = await fetch(
        `/api/server/v2/docker/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(containerId)}/logs?${params.toString()}`,
        {
          cache: 'no-store',
        }
      );
      const text = await res.text();
      if (!res.ok) {
        let message = text;
        try {
          const payload = JSON.parse(text);
          message = payload.error || payload.message || message;
        } catch (_) {}
        throw new Error(message || '\u83b7\u53d6\u65e5\u5fd7\u5931\u8d25');
      }
      setDockerLogsContent(text || '\u6ca1\u6709\u65e5\u5fd7\u8f93\u51fa');
      setDockerLogsLoading(false);
    } catch (err) {
      setDockerLogsContent(`\u9519\u8bef\u003a\u0020${err.message || '\u670d\u52a1\u5f02\u5e38'}`);
      setDockerLogsLoading(false);
    }
  };

  const openDockerContainerLogs = (server, container) => {
    if (!server || !container) return;
    setDockerLogsContainer(container);
    setDockerLogsServer(server);
    setDockerLogsTail(200);
    setDockerLogsModalOpen(true);
    loadDockerContainerLogs(server, container, 200);
  };

  const switchToSSHTab = sessionId => {
    saveTerminalsToWarehouse();

    setServerCurrentTab('terminal');
    setActiveSSHSessionId(sessionId);
    if (showSftpSidebar) syncSftpToSession(sessionId);

    if (sshGroupState && sshGroupState.ids.includes(sessionId)) {
      // 如果属于原分屏组，恢复该分屏状态
      const knownSessionIds = new Set(sshSessions.map(session => session.id));
      const groupIds = sshGroupState.ids.filter(id => knownSessionIds.has(id));
      if (groupIds.length > 1 && groupIds.includes(sessionId)) {
        const nextLayout = getTerminalLayoutForSessionIds(groupIds, sshGroupState.layout);
        const nextSide = nextLayout === 'single' ? '' : sshGroupState.side;
        const restoredTree = buildPresetLayout(groupIds, nextLayout, sessionId);
        setSshSplitTree(restoredTree);
        visibleSessionIdsRef.current = groupIds;
        setVisibleSessionIds(groupIds);
        setSshViewLayout(nextLayout);
        setSshSplitSide(nextSide);
        setSshGroupState({ ids: groupIds, layout: nextLayout, side: nextSide });
      } else {
        setSshSplitTree(createLeafNode(sessionId));
        visibleSessionIdsRef.current = [sessionId];
        setVisibleSessionIds([sessionId]);
        setSshViewLayout('single');
        setSshSplitSide('');
        setSshGroupState(null);
      }
    } else {
      setSshSplitTree(createLeafNode(sessionId));
      visibleSessionIdsRef.current = [sessionId];
      setVisibleSessionIds([sessionId]);
      setSshViewLayout('single');
      setSshSplitSide('');
    }

    setTimeout(() => {
      syncTerminalDOM();
      const instance = sshSessionRefs.current[sessionId];
      if (instance?.terminal) instance.terminal.focus();
    }, 100);
  };

  const activateSSHSession = sessionId => {
    setActiveSSHSessionId(sessionId);
    if (showSftpSidebar) syncSftpToSession(sessionId);
    setTimeout(() => {
      syncTerminalDOM();
      sshSessionRefs.current[sessionId]?.terminal?.focus();
    }, 80);
  };

  const syncSftpToSession = sessionId => {
    const session = sshSessions.find(item => item.id === sessionId);
    const serverId = session?.server?.id;
    if (!serverId) return;
    const lastPath = sftpPathByServerRef.current[serverId] || '.';
    if (sftpServerId === serverId && sftpCurrentPath === lastPath) return;
    setSftpServerId(serverId);
    setSftpCurrentPath(lastPath);
  };

  const sendTerminalCommand = (sessionId, command, options = {}) => {
    const text = String(command || '').trim();
    if (!sessionId || !text) return;

    const targetIds =
      Array.isArray(options.targetSessionIds) && options.targetSessionIds.length > 0
        ? options.targetSessionIds
        : [sessionId];
    const appendNewline = options.appendNewline !== false;
    const payload = appendNewline ? `${text}\r` : text;
    const sendToSession = targetId => {
      const target = sshSessionRefs.current[targetId];
      if (target?.ws?.readyState === WebSocket.OPEN) {
        target.ws.send(JSON.stringify({ type: 'input', data: payload }));
        target.terminal?.focus();
      }
    };

    targetIds.forEach(sendToSession);
    if (
      targetIds.length === 1 &&
      appendNewline &&
      sshSyncEnabledRef.current &&
      visibleSessionIdsRef.current.includes(sessionId)
    ) {
      visibleSessionIdsRef.current.forEach(targetId => {
        if (targetId !== sessionId) sendToSession(targetId);
      });
    }
  };

  const runQuickCommand = (command, options = {}) => {
    sendTerminalCommand(activeSSHSessionId, command, options);
  };

  const getTerminalLayoutForSessionIds = (ids, preferredLayout = sshViewLayout) => {
    if (ids.length <= 1) return 'single';
    if (ids.length > 2) return preferredLayout || 'grid';
    return preferredLayout === 'split-v' ? 'split-v' : 'split-h';
  };

  const applyTerminalPresetLayout = (preset) => {
    const ids = visibleSessionIds.length > 0 ? visibleSessionIds : (activeSSHSessionId ? [activeSSHSessionId] : []);
    if (ids.length === 0) return;

    const nextTree = buildPresetLayout(ids, preset, activeSSHSessionId);
    const nextVisibleIds = nextTree ? getTreeSessionIds(nextTree) : ids;
    setSshSplitTree(nextTree);
    visibleSessionIdsRef.current = nextVisibleIds;
    setVisibleSessionIds(nextVisibleIds);
    setSshViewLayout(preset);
    setSshSplitSide(preset === 'split-v' ? 'bottom' : 'right');
    setSshGroupState(
      nextVisibleIds.length > 1 ? { ids: nextVisibleIds, layout: preset, side: 'right' } : null
    );

    setTimeout(() => {
      syncTerminalDOM();
      if (activeSSHSessionId) {
        sshSessionRefs.current[activeSSHSessionId]?.terminal?.focus();
      }
    }, 100);
  };

  const fitTerminalSession = (sessionId, notifyBackend = true) => {
    const inst = sshSessionRefs.current[sessionId];
    const termEl = terminalDOMElements.current[sessionId];
    if (!inst?.fit || !inst?.terminal || !termEl) return;

    const rect = termEl.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0 || termEl.parentElement === warehouseRef.current)
      return;

    const prevCols = inst.terminal.cols;
    const prevRows = inst.terminal.rows;
    try {
      inst.fit.fit();
      inst.terminal?.refresh?.(0, Math.max(0, inst.terminal.rows - 1));
    } catch (e) {
      return;
    }

    const cols = inst.terminal.cols;
    const rows = inst.terminal.rows;
    const sizeChanged = prevCols !== cols || prevRows !== rows;
    const backendSizeChanged = inst.lastResizeCols !== cols || inst.lastResizeRows !== rows;
    if (
      notifyBackend &&
      (sizeChanged || backendSizeChanged) &&
      inst.ws?.readyState === WebSocket.OPEN
    ) {
      inst.lastResizeCols = cols;
      inst.lastResizeRows = rows;
      inst.ws.send(JSON.stringify({ type: 'resize', cols, rows }));
    }
  };

  const scheduleTerminalFit = (sessionId, delay = 60) => {
    if (!sessionId) return;
    if (terminalResizeTimers.current[sessionId]) {
      clearTimeout(terminalResizeTimers.current[sessionId]);
    }
    terminalResizeTimers.current[sessionId] = setTimeout(() => {
      delete terminalResizeTimers.current[sessionId];
      fitTerminalSession(sessionId);
    }, delay);
  };

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

  const getTerminalPtyId = session =>
    session?.sessionMeta?.attachToPtyId ||
    session?.sessionMeta?.ptyId ||
    session?.attachToPtyId ||
    session?.ptyId ||
    session?.id ||
    '';

  const connectSessionSocket = (sessionId, sessionMeta, terminal, attempt = 0) => {
    const inst = sshSessionRefs.current[sessionId];
    if (!inst) return null;

    if (typeof WebSocket === 'undefined') {
      terminal.writeln(
        '\n\x1b[1;33m当前环境不支持 WebSocket，无法建立终端连接。\x1b[0m'
      );
      setSshSessions(prev => prev.map(s => (s.id === sessionId ? { ...s, connected: false } : s)));
      return null;
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const params = new URLSearchParams({
      server_id: String(sessionMeta.server.id),
      session_id: sessionId,
      transport: resolveTerminalSocketTransport(
        sessionMeta.server,
        sessionMeta.server.preferred_terminal_transport || sessionMeta.type || 'auto'
      ),
      cols: String(inst.terminal?.cols || 120),
      rows: String(inst.terminal?.rows || 32),
    });
    // 重连时优先 attach 回原来的 PTY，保留 shell 上下文（工作目录、环境变量、
    // 正在跑的命令），而不是重开一个全新的 shell。
    const reusePtyId = sessionMeta.attachToPtyId || sessionMeta.ptyId || inst.ptyId;
    if (reusePtyId) {
      params.set('pty_id', reusePtyId);
      params.set('attach', '1');
    }
    if (sessionMeta.containerName) {
      params.set('container', sessionMeta.containerName);
    }

    let ws;
    try {
      ws = new WebSocket(`${protocol}//${window.location.host}/ws/ssh?${params.toString()}`);
    } catch (error) {
      terminal.writeln(`\r\n\x1b[1;31m终端连接创建失败：${error?.message || error}\x1b[0m`);
      return null;
    }

    const clearHeartbeat = () => {
      if (inst.heartbeatInterval) {
        clearInterval(inst.heartbeatInterval);
        inst.heartbeatInterval = null;
      }
    };

    const scheduleReconnect = reason => {
      if (inst.closing || sshSessionRefs.current[sessionId] !== inst) return;
      clearHeartbeat();
      const delay = terminalReconnectDelay(attempt);
      if (inst.reconnectTimer) clearTimeout(inst.reconnectTimer);
      inst.connected = false;
      inst.reconnectAttempt = attempt + 1;
      setSshSessions(prev =>
        prev.map(s =>
          s.id === sessionId
            ? { ...s, connected: false, reconnecting: true, reconnectAttempt: attempt + 1 }
            : s
        )
      );
      terminal.writeln(
        `\r\n\x1b[1;33m终端连接已断开（${reason}），${Math.round(delay / 1000)}s 后自动重连…\x1b[0m`
      );
      inst.reconnectTimer = setTimeout(() => {
        inst.reconnectTimer = null;
        if (inst.closing || sshSessionRefs.current[sessionId] !== inst) return;
        connectSessionSocket(sessionId, sessionMeta, terminal, attempt + 1);
      }, delay);
    };

    ws.onopen = () => {
      if (sshSessionRefs.current[sessionId] !== inst) {
        ws.close();
        return;
      }
      inst.ws = ws;
      inst.reconnectAttempt = 0;
      // 应用层心跳：不走 WS 控制帧，因为部分反向代理/隧道会吞掉 ping/pong，
      // 导致「连接看起来还在但数据过不来」。
      clearHeartbeat();
      inst.heartbeatInterval = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'ping' }));
        }
      }, TERMINAL_HEARTBEAT_MS);
    };

    ws.onmessage = event => {
      try {
        const message = JSON.parse(event.data);
        if (message.type === 'data') {
          if (inst) inst.hasTerminalData = true;
          // 断线重连期间可能积压了输出，恢复后先落地再继续。
          terminal.write(message.data || '');
        } else if (message.type === 'pong') {
          inst.lastPongAt = Date.now();
        } else if (
          message.type === 'status' &&
          (message.data === 'connected' ||
            message.data === 'connected_legacy' ||
            message.data === 'attached')
        ) {
          const connectedTransport =
            message.transport ||
            sessionMeta.server.preferred_terminal_transport ||
            sessionMeta.type ||
            'ssh';
          if (inst && !inst.connectionBannerCleared && !inst.hasTerminalData) {
            terminal.clear();
            inst.connectionBannerCleared = true;
            scheduleTerminalFit(sessionId, 30);
          }
          if (inst) {
            inst.connected = true;
            inst.transport = connectedTransport;
            inst.reconnectAttempt = 0;
          }
          setSshSessions(prev =>
            prev.map(s =>
              s.id === sessionId
                ? { ...s, connected: true, reconnecting: false, reconnectAttempt: 0, transport: connectedTransport }
                : s
            )
          );
        } else if (message.type === 'error') {
          terminal.writeln(`\r\n\x1b[1;31m${message.data || 'SSH connection failed'}\x1b[0m`);
        }
      } catch {
        terminal.write(String(event.data || ''));
      }
    };

    ws.onerror = () => {
      // onclose 一定会跟着触发，这里不重复处理，避免刷两条提示。
    };

    ws.onclose = () => {
      if (sshSessionRefs.current[sessionId] !== inst) return;
      if (inst.closing) return;
      // attach 回原 PTY 失败时（PTY 已随 shell 退出而回收），退化为新开 shell，
      // 否则会一直 attach 失败、无限重连。
      if (shouldDropPtyAttach(attempt, inst.ptyId)) {
        sessionMeta.attachToPtyId = '';
        sessionMeta.ptyId = '';
        inst.ptyId = '';
      }
      scheduleReconnect('连接关闭');
    };

    inst.ws = ws;
    return ws;
  };

  const createSSHSocket = (sessionId, sessionMeta, terminal) =>
    connectSessionSocket(sessionId, sessionMeta, terminal, 0);

  const initSessionTerminal = (sessionId, sessionMeta) => {
    if (sshSessionRefs.current[sessionId]) return;

    const container = document.createElement('div');
    container.id = 'ssh-terminal-' + sessionId;
    container.className = 'app-terminal-surface h-full w-full overflow-hidden';
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
      scrollback: 10000,
      allowProposedApi: true,
    });

    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(new WebLinksAddon());

    terminal.open(container);
    terminal.writeln(`\x1b[1;33m正在尝试建立与 ${sessionMeta.server.name} 的连接...\x1b[0m`);

    const resizeObserver =
      typeof ResizeObserver !== 'undefined'
        ? new ResizeObserver(() => scheduleTerminalFit(sessionId, 80))
        : null;
    if (resizeObserver) resizeObserver.observe(container);

    sshSessionRefs.current[sessionId] = {
      id: sessionId,
      terminal,
      fit: fitAddon,
      ws: null,
      connected: false,
      sessionMeta,
      inputDisposable: null,
      heartbeatInterval: null,
      resizeObserver,
      lastResizeCols: 0,
      lastResizeRows: 0,
      lastSeq: 0,
      connectionBannerCleared: false,
      hasTerminalData: false,
      // 重连相关：reconnectTimer 防止重复排程，closing 让 onclose 不再重连，
      // ptyId 记录当前 PTY，重连时 attach 回去以保留 shell 上下文。
      reconnectTimer: null,
      reconnectAttempt: 0,
      closing: false,
      ptyId: '',
      lastPongAt: 0,
    };

    const ws = createSSHSocket(sessionId, sessionMeta, terminal);
    sshSessionRefs.current[sessionId].ws = ws;

    const inputDisposable = terminal.onData(data => {
      const currentWs = sshSessionRefs.current[sessionId]?.ws;
      if (currentWs?.readyState === WebSocket.OPEN) {
        currentWs.send(JSON.stringify({ type: 'input', data }));
      }

      if (sshSyncEnabledRef.current && visibleSessionIdsRef.current.includes(sessionId)) {
        const sourcePtyId = getTerminalPtyId(sshSessionRefs.current[sessionId]);
        visibleSessionIdsRef.current.forEach(targetId => {
          if (targetId === sessionId) return;
          const targetSession = sshSessionRefs.current[targetId];
          if (sourcePtyId && getTerminalPtyId(targetSession) === sourcePtyId) return;
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
      scheduleTerminalFit(sessionId, 40);
    }, 100);
  };

  // 重新计算并挂载活动终端到 session slots 槽位上（支持分屏树与静态槽位降级）
  const syncTerminalDOM = () => {
    const slots = visibleSessionIdsRef.current;
    slots.forEach((id, index) => {
      // 优先找分屏树容器中的专属会话挂载槽 ssh-slot-session-${id}，次优找旧静态槽位 ssh-slot-idx-${index}
      const slotDiv =
        document.getElementById(`ssh-slot-session-${id}`) ||
        document.getElementById(`ssh-slot-idx-${index}`);
      const termEl = terminalDOMElements.current[id];
      if (slotDiv && termEl && termEl.parentElement !== slotDiv) {
        slotDiv.appendChild(termEl);
      }
      if (slotDiv && termEl && termEl.parentElement === slotDiv) {
        const instance = sshSessionRefs.current[id];
        try {
          instance?.terminal?.refresh?.(0, Math.max(0, (instance.terminal?.rows || 1) - 1));
        } catch (e) {
          // xterm can throw while its viewport is between detached and attached states.
        }
        scheduleTerminalFit(id, 40);
      }
    });
  };

  const restoreVisibleTerminalSurfaces = ({ focus = false } = {}) => {
    syncTerminalDOM();
    visibleSessionIdsRef.current.forEach(id => {
      const instance = sshSessionRefs.current[id];
      if (!instance?.terminal) return;
      fitTerminalSession(id, false);
      try {
        instance.terminal.refresh?.(0, Math.max(0, instance.terminal.rows - 1));
      } catch (e) {
        // Safe no-op while the terminal viewport is being reparented.
      }
    });
    if (focus && activeSSHSessionId) {
      sshSessionRefs.current[activeSSHSessionId]?.terminal?.focus();
    }
  };

  // 关闭终端会话
  const closeSSHSession = sessionId => {
    saveTerminalsToWarehouse();

    const session = sshSessionRefs.current[sessionId];
    if (session) {
      // 先标记关闭，避免 onclose 触发自动重连。
      session.closing = true;
      if (session.reconnectTimer) {
        clearTimeout(session.reconnectTimer);
        session.reconnectTimer = null;
      }
      if (terminalResizeTimers.current[sessionId]) {
        clearTimeout(terminalResizeTimers.current[sessionId]);
        delete terminalResizeTimers.current[sessionId];
      }
      if (session.heartbeatInterval) clearInterval(session.heartbeatInterval);
      if (session.inputDisposable) session.inputDisposable.dispose();
      if (session.resizeObserver) session.resizeObserver.disconnect();
      if (session.ws) {
        if (session.ws.readyState === WebSocket.OPEN) {
          session.ws.send(JSON.stringify({ type: 'disconnect' }));
        }
        session.ws.close();
      }
      if (session.terminal) session.terminal.dispose();
      delete sshSessionRefs.current[sessionId];
    }

    const termEl = terminalDOMElements.current[sessionId];
    if (termEl) {
      termEl.remove();
      delete terminalDOMElements.current[sessionId];
    }

    const remaining = sshSessions.filter(s => s.id !== sessionId);
    const remainingIds = new Set(remaining.map(s => s.id));
    const nextTree = normalizeTree(removeSessionFromTree(sshSplitTree, sessionId), remainingIds);
    const remainsVisible = nextTree ? getTreeSessionIds(nextTree) : visibleSessionIds.filter(id => id !== sessionId && remainingIds.has(id));
    const nextActiveId =
      activeSSHSessionId === sessionId
        ? remainsVisible[remainsVisible.length - 1] || remaining[remaining.length - 1]?.id || ''
        : activeSSHSessionId;
    const nextVisibleIds =
      remainsVisible.length > 0 ? remainsVisible : nextActiveId ? [nextActiveId] : [];
    const nextLayout = getTerminalLayoutForSessionIds(nextVisibleIds, sshViewLayout);
    const nextSide = nextLayout === 'single' ? '' : sshSplitSide;

    setSshSessions(remaining);
    setSshSplitTree(nextTree);
    visibleSessionIdsRef.current = nextVisibleIds;
    setVisibleSessionIds(nextVisibleIds);
    setSshViewLayout(nextLayout);
    setSshSplitSide(nextSide);
    setSshGroupState(
      nextVisibleIds.length > 1 ? { ids: nextVisibleIds, layout: nextLayout, side: nextSide } : null
    );

    if (remaining.length === 0) {
      setActiveSSHSessionId('');
      setServerCurrentTab('list');
    } else {
      setActiveSSHSessionId(nextActiveId);
      setServerCurrentTab('terminal');
      if (showSftpSidebar && nextActiveId) syncSftpToSession(nextActiveId);
      setTimeout(() => {
        syncTerminalDOM();
        sshSessionRefs.current[nextActiveId]?.terminal?.focus();
      }, 100);
    }
  };

  const removeSSHSessionFromView = sessionId => {
    if (visibleSessionIds.length <= 1) {
      closeSSHSession(sessionId);
      return;
    }

    const nextTree = normalizeTree(removeSessionFromTree(sshSplitTree, sessionId), visibleSessionIds);
    const nextVisibleIds = nextTree ? getTreeSessionIds(nextTree) : visibleSessionIds.filter(id => id !== sessionId);
    const nextLayout = getTerminalLayoutForSessionIds(nextVisibleIds, sshViewLayout);
    const nextSide = nextLayout === 'single' ? '' : sshSplitSide;
    const nextActiveId =
      activeSSHSessionId === sessionId
        ? nextVisibleIds[nextVisibleIds.length - 1] || ''
        : activeSSHSessionId;

    setSshSplitTree(nextTree);
    visibleSessionIdsRef.current = nextVisibleIds;
    setVisibleSessionIds(nextVisibleIds);
    setSshViewLayout(nextLayout);
    setSshSplitSide(nextSide);
    setSshGroupState(
      nextVisibleIds.length > 1 ? { ids: nextVisibleIds, layout: nextLayout, side: nextSide } : null
    );
    setActiveSSHSessionId(nextActiveId);
    if (showSftpSidebar && nextActiveId) syncSftpToSession(nextActiveId);

    setTimeout(() => {
      syncTerminalDOM();
      sshSessionRefs.current[nextActiveId]?.terminal?.focus();
    }, 100);
  };

  const toggleTerminalSidebar = (sidebar, options = {}) => {
    setActiveTerminalSidebar(current => {
      const next = current === sidebar ? null : sidebar;
      if (next === 'sftp' && options.serverId) {
        setSftpServerId(options.serverId);
        setSftpCurrentPath(sftpPathByServerRef.current[options.serverId] || '.');
      }
      return next;
    });
  };

  // 重新连接
  const reconnectSSHSession = sessionId => {
    const session = sshSessionRefs.current[sessionId];
    if (!session) return;

    // 用户手动重连：先清掉自动重连排程，避免两条连接同时建立。
    if (session.reconnectTimer) {
      clearTimeout(session.reconnectTimer);
      session.reconnectTimer = null;
    }
    if (session.heartbeatInterval) {
      clearInterval(session.heartbeatInterval);
      session.heartbeatInterval = null;
    }
    if (session.ws) {
      session.ws.onclose = null;
      session.ws.close();
    }

    session.terminal.writeln(`\r\n\x1b[1;33m正在恢复终端会话...\x1b[0m`);
    setSshSessions(prev =>
      prev.map(s => (s.id === sessionId ? { ...s, connected: false, reconnecting: true } : s))
    );

    session.reconnectAttempt = 0;
    const nextWs = connectSessionSocket(sessionId, session.sessionMeta, session.terminal, 0);
    session.ws = nextWs;
  };

  // 页面从后台切回时立即探活：切 WiFi / 手机热点后浏览器可能已静默断开，
  // 但 WS 的 onclose 不一定及时触发（尤其在移动端休眠后）。
  useEffect(() => {
    const probeSessions = () => {
      if (document.visibilityState !== 'visible') return;
      Object.values(sshSessionRefs.current).forEach(session => {
        if (session.closing) return;
        const ws = session.ws;
        if (isTerminalSocketDead(ws) && !session.reconnectTimer) {
          session.reconnectAttempt = 0;
          connectSessionSocket(session.id, session.sessionMeta, session.terminal, 0);
          return;
        }
        if (ws?.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'ping' }));
        }
      });
    };
    document.addEventListener('visibilitychange', probeSessions);
    window.addEventListener('online', probeSessions);
    return () => {
      document.removeEventListener('visibilitychange', probeSessions);
      window.removeEventListener('online', probeSessions);
    };
  }, []);

  // -------------------- 拖拽放置分屏逻辑 --------------------
  const handleTerminalDragStart = (event, id) => {
    if (event?.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move';
      event.dataTransfer.setData('text/plain', id);
    }
    setDraggedSessionId(id);
  };

  const getSplitSourceSessionId = targetId => {
    if (draggedSessionId && draggedSessionId !== targetId) return draggedSessionId;
    if (activeSSHSessionId && activeSSHSessionId !== targetId) return activeSSHSessionId;
    return (
      sshSessions.find(
        session => session.id !== targetId && !visibleSessionIds.includes(session.id)
      )?.id ||
      sshSessions.find(session => session.id !== targetId)?.id ||
      ''
    );
  };

  const updateDockerComposeDeployment = async () => {
    if (
      !dockerComposeEditor?.serverId ||
      !dockerComposeEditor?.projectName ||
      !dockerComposeEditor?.path
    )
      return;
    setDockerComposeEditor(prev => (prev ? { ...prev, updating: true, error: '' } : prev));
    const payload = {
      serverId: dockerComposeEditor.serverId,
      project: dockerComposeEditor.projectName,
      config_file: dockerComposeEditor.configFiles?.join(', ') || dockerComposeEditor.path,
    };
    const result = await submitDockerTask('compose.update', payload, { silent: true });
    setDockerComposeEditor(prev =>
      prev ? { ...prev, updating: false, saved: result?.ok ? false : prev.saved } : prev
    );
    if (result?.ok) {
      toast.success('编排已更新，并已强制拉取镜像');
      scheduleDockerResourceRefresh(500);
    }
  };

  const openRemoteDesktop = server => {
    if (!canOpenRemoteDesktop(server)) {
      toast.warning('远程桌面仅支持在线且已升级的 Windows Agent');
      return;
    }
    window.open(remoteDesktopPath(server.id), '_blank', 'noopener,noreferrer');
  };

  const getTerminalCopyBaseName = session => {
    const name = String(session?.name || session?.server?.name || '终端').trim();
    return name.replace(/\s+(?:共享|复制(?:\s*\d+)?)$/u, '') || '终端';
  };

  const getNextTerminalCopyName = session => {
    const baseName = getTerminalCopyBaseName(session);
    const pattern = new RegExp(
      `^${baseName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s+(?:共享|复制(?:\\s*(\\d+))?)$`,
      'u'
    );
    const copyNumbers = sshSessions
      .map(item =>
        String(item.name || '')
          .trim()
          .match(pattern)
      )
      .filter(Boolean)
      .map(match => Number(match[1] || 1));
    return `${baseName} 复制 ${Math.max(0, ...copyNumbers) + 1}`;
  };

  const createAttachedTerminalView = (targetId, position = 'right') => {
    const sourceSession = sshSessions.find(session => session.id === targetId);
    if (!sourceSession) return '';

    const sourceRef = sshSessionRefs.current[targetId];
    const attachToPtyId = getTerminalPtyId(sourceRef || sourceSession);
    if (!attachToPtyId) return '';

    const sessionId = `session_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`;
    const newSession = {
      ...sourceSession,
      id: sessionId,
      connected: false,
      attachToPtyId,
      name: getNextTerminalCopyName(sourceSession),
    };

    saveTerminalsToWarehouse();
    setSshSessions(prev => [...prev, newSession]);

    // 使用分屏树进行精确嵌套切分
    const nextTree = insertSplitIntoTree(sshSplitTree, targetId, sessionId, position);
    const updated = getTreeSessionIds(nextTree);
    const nextLayout = updated.length > 2 ? 'grid' : position === 'top' || position === 'bottom' ? 'split-v' : 'split-h';
    const nextSide = position;

    setSshSplitTree(nextTree);
    visibleSessionIdsRef.current = updated;
    setVisibleSessionIds(updated);
    setSshViewLayout(nextLayout);
    setSshSplitSide(nextSide);
    setSshGroupState(
      updated.length > 1 ? { ids: updated, layout: nextLayout, side: nextSide } : null
    );
    setActiveSSHSessionId(sessionId);
    if (showSftpSidebar) syncSftpToSession(targetId);
    setDraggedSessionId(null);

    setTimeout(() => initSessionTerminal(sessionId, newSession), 200);
    setTimeout(() => {
      syncTerminalDOM();
      sshSessionRefs.current[sessionId]?.terminal?.focus();
    }, 320);

    return sessionId;
  };

  const triggerSplitPane = (targetId, position, sourceId = getSplitSourceSessionId(targetId)) => {
    if (!sourceId || sourceId === targetId) {
      createAttachedTerminalView(targetId, position);
      return;
    }

    // 使用分屏树进行精确嵌套切分
    const nextTree = insertSplitIntoTree(sshSplitTree, targetId, sourceId, position);
    const updated = getTreeSessionIds(nextTree);
    const nextLayout = updated.length > 2 ? 'grid' : position === 'top' || position === 'bottom' ? 'split-v' : 'split-h';
    const nextSide = position;

    setSshSplitTree(nextTree);
    visibleSessionIdsRef.current = updated;
    setVisibleSessionIds(updated);
    setSshViewLayout(nextLayout);
    setSshSplitSide(nextSide);
    setSshGroupState(
      updated.length > 1 ? { ids: updated, layout: nextLayout, side: nextSide } : null
    );
    setActiveSSHSessionId(sourceId);
    if (showSftpSidebar) syncSftpToSession(sourceId);
    setDraggedSessionId(null);

    setTimeout(() => {
      syncTerminalDOM();
      sshSessionRefs.current[sourceId]?.terminal?.focus();
    }, 100);
  };

  // -------------------- 计算过滤列表 --------------------

  const filteredServers = useMemo(() => {
    let list = serverList;
    const normalizedFilter = serverStatusFilter === 'offline' ? 'offline' : 'all';
    if (normalizedFilter !== 'all') {
      list = list.filter(s => resolveServerDisplayStatus(s).state === normalizedFilter);
    }
    if (serverSearchText.trim()) {
      const query = serverSearchText.toLowerCase();
      list = list.filter(
        s =>
          s.name.toLowerCase().includes(query) ||
          (s.host && s.host.toLowerCase().includes(query)) ||
          (s.tags && s.tags.some(t => t.toLowerCase().includes(query)))
      );
    }
    return list;
  }, [serverList, serverStatusFilter, serverSearchText]);

  // 详情浮层只属于「主机」tab 的卡片视图：切到表格视图 / 打开地图 / 切走 tab / 当前主机被搜索
  // 或状态筛选排除时，浮层状态必须同步清空，否则切回卡片视图会重新弹出，或浮层静默消失但展开态残留
  useEffect(() => {
    if (!cardDetailServerId) return;
    const stillVisible =
      serverCurrentTab === 'list' &&
      serverListViewMode === 'cards' &&
      !serverMapOpen &&
      filteredServers.some(server => String(server.id) === String(cardDetailServerId));
    if (stillVisible) return;
    setExpandedServers(prev => prev.filter(id => String(id) !== String(cardDetailServerId)));
    setCardDetailServerId(null);
  }, [cardDetailServerId, filteredServers, serverCurrentTab, serverListViewMode, serverMapOpen, setExpandedServers]);

  // 卡片视图网格布局：单列堆叠，由列表顺序派生（拖拽排序后重排列表，布局随之重建）
  // 卡片视图列数：视口 ≥1280px 两列，否则单列。卡片内部布局不跟这个断点走，
  // 而是由卡片自身的容器查询（@container + cq-card-wide）判定。
  const isWideCardListView = useMediaQuery('(min-width: 1280px)');
  const cardColumns = isWideCardListView ? 2 : 1;

  const serverCardLayout = useMemo(
    () =>
      filteredServers.map((server, index) => ({
        i: String(server.id),
        x: index % cardColumns,
        y: Math.floor(index / cardColumns),
        w: 1,
        h: 1,
      })),
    [filteredServers, cardColumns]
  );

  // 拖拽结束：按网格位置 (y, x) 重排主机列表并持久化顺序
  const handleServerCardLayoutChange = nextLayout => {
    const orderedIds = [...nextLayout]
      .sort((a, b) => a.y - b.y || a.x - b.x)
      .map(item => String(item.i));
    const orderedSet = new Set(orderedIds);
    const byId = new Map(serverList.map(s => [String(s.id), s]));
    const visible = orderedIds.map(id => byId.get(id)).filter(Boolean);
    const hidden = serverList.filter(s => !orderedSet.has(String(s.id)));
    const orderedServers = visible.concat(hidden);
    const changed = orderedServers.some((s, idx) => s !== serverList[idx]);
    if (!changed) return;
    setServerList(orderedServers);
    saveServerOrder(orderedServers);
  };

  const statsSummary = useMemo(() => {
    const total = serverList.length;
    const counts = serverList.reduce((acc, server) => {
      const state = resolveServerDisplayStatus(server).state;
      acc[state] = (acc[state] || 0) + 1;
      return acc;
    }, {});
    const online = counts.online || 0;
    const interrupted = counts.interrupted || 0;
    const degraded = counts.degraded || 0;
    const offline = counts.offline || 0;
    return { total, online, interrupted, degraded, offline, warning: interrupted + degraded };
  }, [serverList]);

  const visibleCompactColumnDefs = useMemo(
    () => HOST_COMPACT_COLUMNS.filter(column => compactVisibleColumns.includes(column.id)),
    [compactVisibleColumns]
  );
  // 紧凑列表：名称列按内容宽度自适应（JS 测量最长名称，避免固定 112px 截断或浪费空白）。
  // 名称单元格 = 状态点(8px) + gap-2(8px) + OS 图标(约16px) + gap-2(8px) + 名称文字 + cell横留白(2px*2)。
  const compactNameColumnWidth = useMemo(() => {
    const names = filteredServers.map(s => s.name).filter(Boolean);
    if (!names.length || typeof document === 'undefined') return HOST_COMPACT_COLUMN_WIDTHS.name;
    const probe = document.createElement('canvas');
    const ctx = probe.getContext('2d');
    if (ctx) {
      // 与名称文字样式对齐（text-xs≈12px，font-semibold≈600），sans-serif 与 Kumo 字体栈接近
      ctx.font = '600 12px system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';
    }
    const measured = ctx ? names.reduce((max, n) => Math.max(max, ctx.measureText(n).width), 0) : 0;
    const statusDot = compactVisibleColumns.includes('status') ? 8 + 8 : 0;
    // 状态点 + 图标 + 间距 + 最长文字 + 左右留白(cell 左 10px + 右 2px)，并给截断留一点余量
    return Math.min(220, Math.ceil(measured) + statusDot + 16 + 8 + 12 + 8);
  }, [filteredServers, compactVisibleColumns]);
  // 位置/在线/负载列按内容宽度自适应：测量各列内容最大宽度，避免固定宽度浪费空白或截断。
  // 返回 { country, uptime, load }，0 表示无需自适应（回退固定宽度）。
  const compactAdaptiveColumnWidths = useMemo(() => {
    const result = { country: 0, uptime: 0, load: 0 };
    if (typeof document === 'undefined') return result;
    const probe = document.createElement('canvas');
    const ctx = probe.getContext('2d');
    if (!ctx) return result;
    const measureMax = (texts, font, extra) => {
      if (!texts.length) return 0;
      ctx.font = font;
      let max = 0;
      for (const t of texts) max = Math.max(max, ctx.measureText(t).width);
      return Math.min(160, Math.ceil(max) + extra);
    };
    const sans = '600 12px system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';
    const mono = '600 12px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace';
    const countryTexts = [];
    const uptimeTexts = [];
    const loadTexts = [];
    for (const s of filteredServers) {
      const loc = getServerLocationText(s);
      // 位置单元格 = 国旗(w-5 ≈20px) + gap-1.5(6px) + 文本 + cell 横留白(2px*2)
      if (loc) countryTexts.push({ loc });
      uptimeTexts.push(formatUptimeDaysOnly(s.info?.uptime || s.info?.system?.Uptime));
      loadTexts.push(getPrimaryLoadValue(s.info?.cpu?.Load));
    }
    result.country = measureMax(
      countryTexts.map(({ loc }) => loc),
      sans,
      20 + 6 + 4 + 8
    );
    result.uptime = measureMax(uptimeTexts, sans, 4 + 8);
    result.load = measureMax(loadTexts, mono, 16 + 2 + 4 + 8);
    return result;
  }, [filteredServers]);
  const visibleCompactColumnWidths = useMemo(
    () =>
      visibleCompactColumnDefs
        .filter(column => column.id !== 'status')
        .map(column => {
          const base = HOST_COMPACT_COLUMN_WIDTHS[column.id] || 120;
          if (column.id === 'name') {
            // 名称列完全按内容自适应：测量最长名称 + 状态点/图标/留白，避免固定 112px 浪费空白或截断。
            return compactNameColumnWidth;
          }
          if (column.id === 'country' || column.id === 'uptime' || column.id === 'load') {
            // 内容很短时(如「美国」「3天」「0.1」)实测宽度会把列压得过窄,
            // 因此以 base 作为最小宽度,只有内容更宽时才采用实测值。
            return Math.max(base, compactAdaptiveColumnWidths[column.id]);
          }
          return base;
        }),
    [visibleCompactColumnDefs, compactNameColumnWidth, compactAdaptiveColumnWidths]
  );

  const isCompactColumnVisible = columnId => compactVisibleColumns.includes(columnId);

  const toggleCompactColumn = (columnId, visible) => {
    const column = HOST_COMPACT_COLUMNS.find(item => item.id === columnId);
    if (!column || column.required) return;
    setCompactVisibleColumns(prev => {
      if (visible) return prev.includes(columnId) ? prev : [...prev, columnId];
      return prev.filter(id => id !== columnId);
    });
  };

  const showAllCompactColumns = () => setCompactVisibleColumns(HOST_COMPACT_COLUMN_IDS);

  const openCompactColumnMenu = event => {
    event.preventDefault();
    setCompactColumnMenu({
      open: true,
      x: Math.min(event.clientX, window.innerWidth - 224),
      y: Math.min(event.clientY, window.innerHeight - 360),
    });
  };

  const renderServerContextMenu = server => (
    <ContextMenu.Portal>
      <ContextMenu.Positioner sideOffset={6}>
        <ContextMenu.Popup className="z-50 min-w-40 overflow-hidden rounded-lg border border-kumo-line bg-kumo-control p-1.5 text-kumo-default outline-none data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95">
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay"
            disabled={!isServerOnline(server) || server.loading}
            onClick={event => {
              event.stopPropagation();
              refreshServerInfo(server.id);
            }}
          >
            <RefreshCw className="h-4 w-4" />
            <span>刷新详情</span>
          </ContextMenu.Item>
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay"
            onClick={event => {
              event.stopPropagation();
              showAgentInstallModal(server.id);
            }}
          >
            <Shield className="h-4 w-4" />
            <span>部署 Agent</span>
          </ContextMenu.Item>
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay"
            onClick={event => {
              event.stopPropagation();
              openEditServerModal(server);
            }}
          >
            <Edit className="h-4 w-4" />
            <span>编辑主机</span>
          </ContextMenu.Item>
          <ContextMenu.Separator className="-mx-1.5 my-1 h-0.5 bg-kumo-hairline" />
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay"
            disabled={!isServerOnline(server)}
            onClick={event => {
              event.stopPropagation();
              runServerPowerAction(server.id, 'reboot');
            }}
          >
            <Reboot className="h-4 w-4" />
            <span>重启主机</span>
          </ContextMenu.Item>
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm text-kumo-danger outline-hidden select-none focus:text-kumo-danger focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-danger/5 data-highlighted:text-kumo-danger"
            disabled={!isServerOnline(server)}
            onClick={event => {
              event.stopPropagation();
              runServerPowerAction(server.id, 'shutdown');
            }}
          >
            <X className="h-4 w-4" />
            <span>关闭主机</span>
          </ContextMenu.Item>
          <ContextMenu.Separator className="-mx-1.5 my-1 h-0.5 bg-kumo-hairline" />
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm text-kumo-danger outline-hidden select-none focus:text-kumo-danger focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-danger/5 data-highlighted:text-kumo-danger"
            onClick={event => {
              event.stopPropagation();
              deleteServer(server.id);
            }}
          >
            <Trash className="h-4 w-4" />
            <span>删除主机</span>
          </ContextMenu.Item>
        </ContextMenu.Popup>
      </ContextMenu.Positioner>
    </ContextMenu.Portal>
  );

  return (
    <div
      // 终端 tab 与卡片视图需要「确定高度」：分屏树的 h-full 依赖百分比高度，若只给
      // min-h-full + flex-1（父级 inner 是 min-h-full，高度为 auto），h-full
      // 会退化成 auto，垂直分屏时两栏按内容高度累加而不是均分剩余空间
      // （实测 2 行合计 1610px > 视口 1000px）。故此处给出视口扣减高度
      // （见 docs/standards/前端布局约定.md 的「主机实例终端 tab」例外），
      // 并**不加** overflow-hidden：sticky tab 栏用 -mx-[gutter] 通栏，裁切会让左右各少 12px。
      className={
        serverCurrentTab === 'terminal'
          ? 'flex h-[calc(100dvh-var(--app-header-height)-var(--app-canvas-gutter-bottom))] min-h-[26rem] w-full min-w-0 flex-col gap-2'
          : serverCurrentTab === 'list' && (serverListViewMode === 'cards' || serverMapOpen)
            ? 'flex h-[calc(100dvh-var(--app-header-height)-var(--app-canvas-gutter-bottom))] min-h-[26rem] w-full min-w-0 flex-col gap-3 cq-sm:gap-4'
            : 'flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4'
      }
    >
      <CompactColumnMenu
        menu={compactColumnMenu}
        visibleColumns={compactVisibleColumns}
        onToggle={toggleCompactColumn}
        onShowAll={showAllCompactColumns}
        onClose={() => setCompactColumnMenu(prev => ({ ...prev, open: false }))}
      />
      {/* 顶部标签导航 */}
      <div
        className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}
      >
        <Tabs
          key={`module-tabs-${sshSessions.length > 0 ? 'terminal' : 'plain'}`}
          {...MODULE_TABS_PROPS}
          value={serverCurrentTab}
          onValueChange={value => {
            if (serverCurrentTab === 'terminal' && value !== 'terminal') {
              saveTerminalsToWarehouse();
            }
            setServerCurrentTab(value);
            if (value === 'status-pages') loadServerStatusPages();
          }}
          tabs={[
            {
              value: 'list',
              label: (
                <ServerModuleTabLabel icon={Server} short="主机">
                  主机
                </ServerModuleTabLabel>
              ),
            },
            {
              value: 'docker',
              label: (
                <ServerModuleTabLabel icon={Box} short="容器">
                  容器
                </ServerModuleTabLabel>
              ),
            },
            {
              value: 'forward',
              label: (
                <ServerModuleTabLabel icon={Shuffle} short="转发">
                  端口转发
                </ServerModuleTabLabel>
              ),
            },
            {
              value: 'status-pages',
              label: (
                <ServerModuleTabLabel icon={Globe} short="公开">
                  公开
                </ServerModuleTabLabel>
              ),
            },
            {
              value: 'management',
              label: (
                <ServerModuleTabLabel icon={Settings} short="管理">
                  管理
                </ServerModuleTabLabel>
              ),
            },
            {
              value: 'onepanel',
              label: (
                <ServerModuleTabLabel icon={Server} short="1Panel">
                  1Panel
                </ServerModuleTabLabel>
              ),
            },
            ...(sshSessions.length > 0
              ? [
                  {
                    value: 'terminal',
                    label: (
                      <ServerModuleTabLabel
                        icon={TerminalIcon}
                        short="终端"
                        badge={sshSessions.length}
                      >
                        终端
                      </ServerModuleTabLabel>
                    ),
                  },
                ]
              : []),
          ]}
        />

        {/* 右侧快速连接 */}
        <div className="flex shrink-0 items-center gap-2">
          {serverCurrentTab === 'forward' && (
            <Button
              size="sm"
              variant="primary"
              onClick={() => forwardPanelRef.current?.openCreate()}
            >
              <Plus className="h-3.5 w-3.5" />
              创建转发规则
            </Button>
          )}
          {serverCurrentTab === 'list' && (
            <Toolbar size="sm" aria-label="导出导入主机配置" className="shrink-0">
              <Toolbar.Button
                onClick={exportServers}
                aria-label="导出主机配置"
                icon={<Upload className="h-3.5 w-3.5" />}
              >
                <span className="hidden cq-sm:inline">导出</span>
              </Toolbar.Button>
              <Toolbar.Button
                onClick={openImportServerModal}
                aria-label="导入主机配置"
                icon={<Download className="h-3.5 w-3.5" />}
              >
                <span className="hidden cq-sm:inline">导入</span>
              </Toolbar.Button>
            </Toolbar>
          )}
          <TabBarOverflowActions
            items={
              serverCurrentTab === 'list'
                ? [
                    {
                      key: 'upgrade-agent',
                      label: '升级 Agent',
                      title: '升级所有在线 Agent',
                      icon: <Upload className="w-3.5 h-3.5" />,
                      onClick: openUpgradeModal,
                    },
                    {
                      key: 'batch-deploy',
                      label: '批量部署',
                      title: '批量部署 Agent',
                      icon: <Shield className="w-3.5 h-3.5" />,
                      onClick: openBatchAgentModal,
                    },
                    {
                      key: 'refresh',
                      label: '刷新列表',
                      title: '刷新列表和地理位置',
                      icon: <RefreshCw className="w-3.5 h-3.5" />,
                      onClick: refreshServerLocationsAndList,
                      loading: serverLoading,
                    },
                    {
                      key: 'add',
                      label: '新增主机',
                      icon: <Plus className="w-3.5 h-3.5" />,
                      onClick: openAddServerModal,
                      variant: 'primary',
                    },
                  ]
                : []
            }
          />
        </div>
      </div>

      {serverCurrentTab === 'status-pages' && (
        <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(24rem,0.9fr)_minmax(0,1.1fr)]">
          <LayerCard className="overflow-hidden p-0">
            <LayerCard.Secondary className={SERVER_SECTION_HEADER_CLASS}>
              <div className="min-w-0">
                <h3 className="flex items-center gap-2 text-sm font-semibold text-kumo-strong">
                  <Globe className="h-4 w-4" />
                  {serverStatusPageForm.id ? '编辑主机状态页' : '新建主机状态页'}
                </h3>
              </div>
              {serverStatusPageForm.id && (
                <Button
                  size="sm"
                  variant="secondary"
                  shape="square"
                  icon={<X className="h-3.5 w-3.5" />}
                  onClick={resetServerStatusPageForm}
                  aria-label="取消编辑"
                />
              )}
            </LayerCard.Secondary>

            <LayerCard.Primary className="space-y-4 p-4">
              <div className="grid gap-3 cq-sm:grid-cols-2">
                <Input
                  size="sm"
                  label="名称"
                  value={serverStatusPageForm.title}
                  onChange={event =>
                    setServerStatusPageForm(prev => ({
                      ...prev,
                      title: event.target.value,
                      slug: prev.slug || normalizeServerStatusSlug(event.target.value),
                    }))
                  }
                  placeholder="基础设施状态"
                />
                <Input
                  size="sm"
                  label="Slug"
                  value={serverStatusPageForm.slug}
                  onChange={event =>
                    setServerStatusPageForm(prev => ({
                      ...prev,
                      slug: normalizeServerStatusSlug(event.target.value),
                    }))
                  }
                  placeholder="infra"
                />
                <Input
                  size="sm"
                  label="自定义域名"
                  value={serverStatusPageForm.domain}
                  onChange={event =>
                    setServerStatusPageForm(prev => ({
                      ...prev,
                      domain: normalizeServerStatusDomain(event.target.value),
                    }))
                  }
                  placeholder="status.example.com"
                />
                <Input
                  size="sm"
                  label="缓存秒数"
                  type="number"
                  min="30"
                  value={serverStatusPageForm.cacheSeconds}
                  onChange={event =>
                    setServerStatusPageForm(prev => ({ ...prev, cacheSeconds: event.target.value }))
                  }
                />
                <div className="cq-sm:col-span-2">
                  <Textarea
                    size="sm"
                    label="说明"
                    value={serverStatusPageForm.description}
                    onChange={event =>
                      setServerStatusPageForm(prev => ({
                        ...prev,
                        description: event.target.value,
                      }))
                    }
                    placeholder="可选说明"
                    rows={3}
                  />
                </div>
              </div>

              <div className="grid gap-2 cq-sm:grid-cols-2">
                {[
                  ['public', '公开访问', '关闭后公开 API 和单页都会不可用。'],
                  ['hideHosts', '隐藏地址', '公开页不显示主机 IP 或连接地址。'],
                  ['showTraffic', '显示流量', '展示已用流量和流量上限。'],
                  ['showCharts', '历史指标', '公开接口下发最近指标历史。'],
                  ['showOnDashboard', '首页快捷卡片', '在仪表盘显示跳转到此状态页的快捷入口。'],
                ].map(([key, title, desc]) => (
                  <div
                    key={key}
                    className="flex items-center justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3"
                  >
                    <div className="min-w-0">
                      <div className="text-sm font-semibold text-kumo-strong">{title}</div>
                      <div className="mt-1 text-xs text-kumo-subtle">{desc}</div>
                    </div>
                    <Switch
                      checked={!!serverStatusPageForm[key]}
                      onCheckedChange={checked =>
                        setServerStatusPageForm(prev => ({ ...prev, [key]: checked }))
                      }
                    />
                  </div>
                ))}
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <div className="text-sm font-semibold text-kumo-strong">绑定主机</div>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() =>
                      setServerStatusPageForm(prev => ({
                        ...prev,
                        serverIds: serverList.map(item => item.id),
                      }))
                    }
                    disabled={serverList.length === 0}
                  >
                    全选
                  </Button>
                </div>
                <div className="max-h-64 overflow-y-auto rounded-lg border border-kumo-line bg-kumo-base p-2 scrollbar-thin">
                  {serverList.length === 0 ? (
                    <div className="p-4 text-center text-xs text-kumo-subtle">暂无主机实例。</div>
                  ) : (
                    <div className="grid gap-1.5">
                      {serverList.map(server => (
                        <label
                          key={server.id}
                          className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-kumo-recessed"
                        >
                          <Checkbox
                            checked={serverStatusPageForm.serverIds.includes(server.id)}
                            onCheckedChange={checked =>
                              toggleServerStatusPageServer(server.id, checked)
                            }
                            aria-label={`绑定 ${server.name}`}
                          />
                          <span className="min-w-0 flex-1 truncate text-sm text-kumo-strong">
                            {server.name}
                          </span>
                          <span className="hidden max-w-[12rem] truncate font-mono text-[10px] text-kumo-subtle cq-sm:block">
                            {server.host || server.id}
                          </span>
                        </label>
                      ))}
                    </div>
                  )}
                </div>
              </div>

              <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-xs text-kumo-subtle">
                <div className="font-semibold text-kumo-strong">预览地址</div>
                <div className="mt-2 space-y-1.5">
                  {[
                    getServerStatusPageUrl(serverStatusPageForm, 'servers'),
                    getServerStatusPageUrl(serverStatusPageForm, 's'),
                    getServerStatusDomainUrl(serverStatusPageForm),
                  ]
                    .filter(Boolean)
                    .map(url => (
                      <ClipboardText
                        key={url}
                        size="sm"
                        text={url}
                        className="w-full"
                        tooltip={{ text: '复制地址', copiedText: '地址已复制', side: 'top' }}
                        labels={{ copyAction: '复制状态页地址' }}
                      />
                    ))}
                </div>
              </div>

              <div className="flex flex-wrap justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={resetServerStatusPageForm}>
                  重置
                </Button>
                <Button
                  size="sm"
                  variant="primary"
                  loading={serverStatusPagesLoading}
                  onClick={saveServerStatusPage}
                  icon={<Save className="h-3.5 w-3.5" />}
                >
                  {serverStatusPageForm.id ? '保存状态页' : '创建状态页'}
                </Button>
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard className="overflow-hidden p-0">
            <LayerCard.Secondary className={SERVER_SECTION_HEADER_CLASS}>
              <div>
                <h3 className="flex items-center gap-2 text-sm font-semibold text-kumo-strong">
                  <Globe className="h-4 w-4" />
                  已发布状态页
                </h3>
              </div>
              <Button
                size="sm"
                variant="secondary"
                icon={<RotateCw className="h-3.5 w-3.5" />}
                onClick={loadServerStatusPages}
                loading={serverStatusPagesLoading}
              >
                刷新
              </Button>
            </LayerCard.Secondary>

            <LayerCard.Primary className="p-4">
              {serverStatusPages.length === 0 ? (
                <div className="flex min-h-56 flex-col items-center justify-center rounded-lg border border-dashed border-kumo-line text-center text-sm text-kumo-subtle">
                  <Globe className="mb-3 h-8 w-8 opacity-40" />
                  暂无主机状态页。
                </div>
              ) : (
                <div className="grid gap-3">
                  {serverStatusPages.map(page => {
                    const statusUrl = getServerStatusPageUrl(page, 'servers');
                    const compactUrl = getServerStatusPageUrl(page, 's');
                    const domainUrl = getServerStatusDomainUrl(page);
                    return (
                      <div
                        key={page.id}
                        className="rounded-lg border border-kumo-line bg-kumo-base p-3"
                      >
                        <div className="flex flex-col gap-3 cq-sm:flex-row cq-sm:items-start cq-sm:justify-between">
                          <div className="min-w-0">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-brand/10 text-brand">
                                <PublicPageBrandIcon
                                  pageKind="server"
                                  config={page.config}
                                  iconClassName="h-4 w-4"
                                  customIconClassName="h-4 w-4"
                                />
                              </span>
                              <span className="truncate text-sm font-semibold text-kumo-strong">
                                {page.title || page.slug}
                              </span>
                              <span
                                className={`rounded px-2 py-0.5 text-[10px] font-semibold ${page.public ? 'bg-kumo-success/10 text-kumo-success' : 'bg-kumo-line/30 text-kumo-subtle'}`}
                              >
                                {page.public ? '公开' : '私有'}
                              </span>
                              <span className="rounded bg-kumo-recessed px-2 py-0.5 font-mono text-[10px] text-kumo-subtle">
                                {page.cacheSeconds || 300}s
                              </span>
                            </div>
                            <div className="mt-1 truncate font-mono text-xs text-kumo-subtle">
                              {page.slug}
                            </div>
                            {page.description && (
                              <div className="mt-2 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">
                                {page.description}
                              </div>
                            )}
                          </div>
                          <div className="flex shrink-0 flex-wrap gap-2">
                            <Button
                              size="sm"
                              variant="secondary"
                              shape="square"
                              icon={<Edit className="h-3.5 w-3.5" />}
                              onClick={() => editServerStatusPage(page)}
                              aria-label="编辑主机状态页"
                            />
                            <Button
                              size="sm"
                              variant="secondary"
                              shape="square"
                              icon={<ExternalLink className="h-3.5 w-3.5" />}
                              onClick={() =>
                                window.open(statusUrl, '_blank', 'noopener,noreferrer')
                              }
                              aria-label="打开主机状态页"
                            />
                            <Button
                              size="sm"
                              variant="secondary"
                              shape="square"
                              icon={<Copy className="h-3.5 w-3.5" />}
                              onClick={() => copyServerStatusUrl(statusUrl)}
                              aria-label="复制主机状态页地址"
                            />
                            <Button
                              size="sm"
                              variant={
                                isArmed(`status-page.delete::${page.id}`)
                                  ? 'destructive'
                                  : 'secondary-destructive'
                              }
                              shape="square"
                              icon={<Trash className="h-3.5 w-3.5" />}
                              onClick={() => deleteServerStatusPage(page)}
                              aria-label="删除主机状态页"
                            />
                          </div>
                        </div>
                        <div className="mt-3 grid gap-2">
                          {[statusUrl, compactUrl, domainUrl].filter(Boolean).map(url => (
                            <ClipboardText
                              key={url}
                              size="sm"
                              text={url}
                              className="w-full"
                              tooltip={{ text: '复制地址', copiedText: '地址已复制', side: 'top' }}
                              labels={{ copyAction: '复制状态页地址' }}
                            />
                          ))}
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </LayerCard.Primary>
          </LayerCard>
        </div>
      )}

      {/* ==================== 端口转发 ==================== */}
      {serverCurrentTab === 'forward' && <ForwardPanel ref={forwardPanelRef} />}

      {/* ==================== 1. 主机管理 ==================== */}
      <ServerHostTable
        cardColumns={cardColumns}
        cardDetailServerId={cardDetailServerId}
        chartSeriesReadyServers={chartSeriesReadyServers}
        checkDockerUpdatesForServer={checkDockerUpdatesForServer}
        compactExpandedChartHeight={compactExpandedChartHeight}
        compactExpandedYAxisTickCount={compactExpandedYAxisTickCount}
        compactNetworkQualityChartHeight={compactNetworkQualityChartHeight}
        expandedChartXAxisTickCount={expandedChartXAxisTickCount}
        expandedChartXAxisTickFormat={expandedChartXAxisTickFormat}
        expandedChartYAxisTickCount={expandedChartYAxisTickCount}
        expandedDockerPanels={expandedDockerPanels}
        expandedNumberAxisTickFormat={expandedNumberAxisTickFormat}
        expandedServers={expandedServers}
        expandedSpeedAxisTickFormat={expandedSpeedAxisTickFormat}
        expandedTrendChartHeight={expandedTrendChartHeight}
        fastTimeseriesEcharts={fastTimeseriesEcharts}
        filteredServers={filteredServers}
        getDockerActionProgress={getDockerActionProgress}
        getDockerContainerId={getDockerContainerId}
        getDockerContainerImage={getDockerContainerImage}
        getDockerContainerName={getDockerContainerName}
        getDockerContainerSelectionKey={getDockerContainerSelectionKey}
        getDockerContainerState={getDockerContainerState}
        getDockerContainerUpdateCheck={getDockerContainerUpdateCheck}
        getDockerStateBadge={getDockerStateBadge}
        getDockerUpdateBadge={getDockerUpdateBadge}
        handleDockerContainerUpdate={handleDockerContainerUpdate}
        handleServerCardLayoutChange={handleServerCardLayoutChange}
        isCompactColumnVisible={isCompactColumnVisible}
        isCompactViewport={isCompactViewport}
        isDenseViewport={isDenseViewport}
        isDockerActionPending={isDockerActionPending}
        isDockerContainerUpdateChecking={isDockerContainerUpdateChecking}
        isDockerUpdateConfirmActive={isDockerUpdateConfirmActive}
        loadNetworkQuality={loadNetworkQuality}
        networkQualityByServer={networkQualityByServer}
        networkQualityChartHeight={networkQualityChartHeight}
        openCompactColumnMenu={openCompactColumnMenu}
        openRemoteDesktop={openRemoteDesktop}
        openSSHTerminal={openSSHTerminal}
        renderServerContextMenu={renderServerContextMenu}
        renderedCompactExpandedServers={renderedCompactExpandedServers}
        resolveServerDisplayStatus={resolveServerDisplayStatus}
        resolveServerMetricsHealth={resolveServerMetricsHealth}
        serverCardDragEnabled={serverCardDragEnabled}
        serverCardLayout={serverCardLayout}
        serverCurrentTab={serverCurrentTab}
        serverIpDisplayMode={serverIpDisplayMode}
        serverList={serverList}
        serverListViewMode={serverListViewMode}
        serverLoading={serverLoading}
        serverMapOpen={serverMapOpen}
        serverSearchText={serverSearchText}
        serverStatusFilter={serverStatusFilter}
        setCardDetailServerId={setCardDetailServerId}
        setServerCardDragEnabled={setServerCardDragEnabled}
        setExpandedServers={setExpandedServers}
        setServerListViewMode={setServerListViewMode}
        setServerMapOpen={setServerMapOpen}
        setServerSearchText={setServerSearchText}
        setServerStatusFilter={setServerStatusFilter}
        startRenameServer={startRenameServer}
        staticTimeseriesEcharts={staticTimeseriesEcharts}
        statsSummary={statsSummary}
        submitDockerTask={submitDockerTask}
        theme={theme}
        toggleDockerPanel={toggleDockerPanel}
        toggleServerExpand={toggleServerExpand}
        visibleCompactColumnDefs={visibleCompactColumnDefs}
        visibleCompactColumnWidths={visibleCompactColumnWidths}
      />

      {/* ==================== 3. Docker 控制台 ==================== */}
      <ServerDockerConsole
        batchUpdateVisibleDockerContainers={batchUpdateVisibleDockerContainers}
        checkAllDockerUpdates={checkAllDockerUpdates}
        checkDockerUpdatesForServer={checkDockerUpdatesForServer}
        checkVisibleDockerUpdates={checkVisibleDockerUpdates}
        confirmPress={confirmPress}
        dockerActiveBatchUpdateConfirmKey={dockerActiveBatchUpdateConfirmKey}
        dockerActiveBatchUpdateScopeName={dockerActiveBatchUpdateScopeName}
        dockerActiveBatchUpdateTargets={dockerActiveBatchUpdateTargets}
        dockerAutoUpdate={dockerAutoUpdate}
        dockerAutoUpdateBusy={dockerAutoUpdateBusy}
        dockerAutoUpdateResultMap={dockerAutoUpdateResultMap}
        dockerBulkUpdateChecking={dockerBulkUpdateChecking}
        dockerComposeProjects={dockerComposeProjects}
        dockerContainerManagementServers={dockerContainerManagementServers}
        dockerContainerManagementTotals={dockerContainerManagementTotals}
        dockerContainerManagementUpdatableContainers={dockerContainerManagementUpdatableContainers}
        dockerContainerStateFilter={dockerContainerStateFilter}
        dockerImages={dockerImages}
        dockerManagementBatchUpdateConfirmKey={dockerManagementBatchUpdateConfirmKey}
        dockerNetworks={dockerNetworks}
        dockerOverviewServers={dockerOverviewServers}
        dockerReverseProxyDomains={dockerReverseProxyDomains}
        dockerResourceLoading={dockerResourceLoading}
        dockerSearchQuery={dockerSearchQuery}
        dockerSelectedContainerKeySet={dockerSelectedContainerKeySet}
        dockerSelectedServer={dockerSelectedServer}
        dockerStats={dockerStats}
        dockerStatsHistory={dockerStatsHistory}
        dockerStatsSummary={dockerStatsSummary}
        dockerSubTab={dockerSubTab}
        dockerTaskStreamConnected={dockerTaskStreamConnected}
        dockerTasks={dockerTasks}
        dockerVolumes={dockerVolumes}
        expandedChartXAxisTickFormat={expandedChartXAxisTickFormat}
        fastTimeseriesEcharts={fastTimeseriesEcharts}
        getDockerActionProgress={getDockerActionProgress}
        getDockerContainerId={getDockerContainerId}
        getDockerContainerImage={getDockerContainerImage}
        getDockerContainerName={getDockerContainerName}
        getDockerContainerPorts={getDockerContainerPorts}
        getDockerContainerSelectionKey={getDockerContainerSelectionKey}
        getDockerContainerState={getDockerContainerState}
        getDockerContainerSummary={getDockerContainerSummary}
        getDockerContainerUpdateCheck={getDockerContainerUpdateCheck}
        getDockerStateBadge={getDockerStateBadge}
        getDockerUpdateBadge={getDockerUpdateBadge}
        getUpdatableDockerContainers={getUpdatableDockerContainers}
        handleDockerContainerUpdate={handleDockerContainerUpdate}
        isArmed={isArmed}
        isCompactViewport={isCompactViewport}
        isDockerActionPending={isDockerActionPending}
        isDockerContainerUpdateChecking={isDockerContainerUpdateChecking}
        isDockerHostExpanded={isDockerHostExpanded}
        isDockerUpdateConfirmActive={isDockerUpdateConfirmActive}
        loadDockerAutoUpdate={loadDockerAutoUpdate}
        loadDockerResources={loadDockerResources}
        openDockerComposeConfig={openDockerComposeConfig}
        openDockerContainerLogs={openDockerContainerLogs}
        pruneDockerImagesForHosts={pruneDockerImagesForHosts}
        pruneDockerNetworksForHosts={pruneDockerNetworksForHosts}
        renderDockerEmptyState={renderDockerEmptyState}
        renderDockerFilterChip={renderDockerFilterChip}
        renderDockerHostResourceSection={renderDockerHostResourceSection}
        renderDockerResourceSideRail={renderDockerResourceSideRail}
        renderDockerSimpleLogCard={renderDockerSimpleLogCard}
        runDockerAutoUpdate={runDockerAutoUpdate}
        saveDockerAutoUpdatePolicy={saveDockerAutoUpdatePolicy}
        selectedUpdatableDockerContainers={selectedUpdatableDockerContainers}
        serverCurrentTab={serverCurrentTab}
        setDockerContainerStateFilter={setDockerContainerStateFilter}
        setDockerSearchQuery={setDockerSearchQuery}
        setDockerSelectedServer={setDockerSelectedServer}
        setDockerSubTab={setDockerSubTab}
        setShowDockerTaskDetails={setShowDockerTaskDetails}
        showDockerBlockingLoading={showDockerBlockingLoading}
        showDockerTaskDetails={showDockerTaskDetails}
        submitDockerPruneTask={submitDockerPruneTask}
        submitDockerTask={submitDockerTask}
        theme={theme}
        toggleDockerContainerSelection={toggleDockerContainerSelection}
        toggleDockerOverviewServer={toggleDockerOverviewServer}
        visibleDockerContainerServers={visibleDockerContainerServers}
        visibleUpdatableDockerContainers={visibleUpdatableDockerContainers}
      />

      {/* ==================== 4. 后台管理 ==================== */}
      <ServerOnepanelPanel serverCurrentTab={serverCurrentTab} />
      <ServerAdminPanel
        batchAddServers={batchAddServers}
        deleteCredential={deleteCredential}
        deleteNetworkTarget={deleteNetworkTarget}
        exportServers={exportServers}
        handleServerIpDisplayModeChange={handleServerIpDisplayModeChange}
        isArmed={isArmed}
        networkTargets={networkTargets}
        openImportServerModal={openImportServerModal}
        saveServerModuleSettings={saveServerModuleSettings}
        serverAddingBatch={serverAddingBatch}
        serverBatchError={serverBatchError}
        serverBatchSuccess={serverBatchSuccess}
        serverBatchText={serverBatchText}
        serverCredentials={serverCredentials}
        serverCurrentTab={serverCurrentTab}
        serverIpDisplayMode={serverIpDisplayMode}
        serverSettingsForm={serverSettingsForm}
        serverSettingsSaving={serverSettingsSaving}
        setDefaultCredential={setDefaultCredential}
        setNetworkTargetForm={setNetworkTargetForm}
        setNetworkTargetModalMode={setNetworkTargetModalMode}
        setServerBatchText={setServerBatchText}
        setServerSettingsForm={setServerSettingsForm}
        setShowAddCredentialModal={openAddCredentialModal}
        setShowNetworkTargetModal={setShowNetworkTargetModal}
        toggleNetworkTargetEnabled={toggleNetworkTargetEnabled}
      />
      {/* ==================== 5. SSH 终端 (多分屏支持) ==================== */}
      <ServerSshTerminal
        activateSSHSession={activateSSHSession}
        activeSSHSessionId={activeSSHSessionId}
        activeTerminalSidebar={activeTerminalSidebar}
        applyTerminalPresetLayout={applyTerminalPresetLayout}
        closeSSHSession={closeSSHSession}
        createAttachedTerminalView={createAttachedTerminalView}
        draggedSessionId={draggedSessionId}
        fitTerminalSession={fitTerminalSession}
        handleTerminalDragStart={handleTerminalDragStart}
        reconnectSSHSession={reconnectSSHSession}
        refreshServerInfo={refreshServerInfo}
        removeSSHSessionFromView={removeSSHSessionFromView}
        runQuickCommand={runQuickCommand}
        serverCurrentTab={serverCurrentTab}
        serverIpDisplayMode={serverIpDisplayMode}
        serverList={serverList}
        setActiveTerminalSidebar={setActiveTerminalSidebar}
        setDraggedSessionId={setDraggedSessionId}
        setSftpCurrentPath={setSftpCurrentPath}
        setSftpServerId={setSftpServerId}
        setSshIdeFullscreen={setSshIdeFullscreen}
        setSshSplitTree={setSshSplitTree}
        setSshSyncEnabled={setSshSyncEnabled}
        setTerminalIpMasked={setTerminalIpMasked}
        setTerminalStatusRefreshing={setTerminalStatusRefreshing}
        sftpCurrentPath={sftpCurrentPath}
        sftpPathByServerRef={sftpPathByServerRef}
        sftpServerId={sftpServerId}
        showCommandSidebar={showCommandSidebar}
        showServerStatusSidebar={showServerStatusSidebar}
        showSftpSidebar={showSftpSidebar}
        sshIdeFullscreen={sshIdeFullscreen}
        sshSessions={sshSessions}
        sshSplitTree={sshSplitTree}
        sshSyncEnabled={sshSyncEnabled}
        sshViewLayout={sshViewLayout}
        switchToSSHTab={switchToSSHTab}
        terminalIpMasked={terminalIpMasked}
        terminalStatusRefreshing={terminalStatusRefreshing}
        toggleTerminalSidebar={toggleTerminalSidebar}
        triggerSplitPane={triggerSplitPane}
        visibleSessionIds={visibleSessionIds}
        visibleSessionIdsRef={visibleSessionIdsRef}
      />
      {/* ==================== xterm.js 实例静默挂载的仓库 ==================== */}
      <div
        ref={warehouseRef}
        className="hidden absolute -top-[9999px]"
        id="ssh-terminal-warehouse"
      ></div>

      {/* ==================== 模态框: 添加与编辑服务器 ==================== */}
      <ServerModals
        addCredential={addCredential}
        agentForceSsh={agentForceSsh}
        agentInstallLoading={agentInstallLoading}
        agentInstallLog={agentInstallLog}
        agentInstallOS={agentInstallOS}
        agentInstallResult={agentInstallResult}
        agentInstalling={agentInstalling}
        agentModalData={agentModalData}
        applyCredential={applyCredential}
        autoInstallAgent={autoInstallAgent}
        batchInstallResults={batchInstallResults}
        canSshDeployAgent={canSshDeployAgent}
        confirmImportServers={confirmImportServers}
        credForm={credForm}
        dockerComposeEditor={dockerComposeEditor}
        dockerLogsContainer={dockerLogsContainer}
        dockerLogsContent={dockerLogsContent}
        dockerLogsLoading={dockerLogsLoading}
        dockerLogsModalOpen={dockerLogsModalOpen}
        dockerLogsServer={dockerLogsServer}
        dockerLogsTail={dockerLogsTail}
        generateQuickInstallCommand={generateQuickInstallCommand}
        getAgentBaseApiUrl={getAgentBaseApiUrl}
        getAgentInstallCommand={getAgentInstallCommand}
        getAgentUpgradeTargets={getAgentUpgradeTargets}
        getDockerContainerName={getDockerContainerName}
        getUpgradeBatchStatusLabel={getUpgradeBatchStatusLabel}
        getUpgradeItemStatusLabel={getUpgradeItemStatusLabel}
        importModalError={importModalError}
        importModalSaving={importModalSaving}
        importPreview={importPreview}
        loadDockerContainerLogs={loadDockerContainerLogs}
        networkTargetForm={networkTargetForm}
        networkTargetModalMode={networkTargetModalMode}
        performOneKeyUpgrade={performOneKeyUpgrade}
        processImportFile={processImportFile}
        quickDeployName={quickDeployName}
        quickDeployResult={quickDeployResult}
        regenerateAgentKey={regenerateAgentKey}
        requestCloseDockerComposeEditor={requestCloseDockerComposeEditor}
        runBatchAgentInstall={runBatchAgentInstall}
        saveDockerComposeConfig={saveDockerComposeConfig}
        saveNetworkTarget={saveNetworkTarget}
        saveServer={saveServer}
        selectAllBatchServers={selectAllBatchServers}
        selectedBatchServers={selectedBatchServers}
        selectedCredentialId={selectedCredentialId}
        serverAddMode={serverAddMode}
        serverCredentials={serverCredentials}
        serverForm={serverForm}
        serverList={serverList}
        serverModalMode={serverModalMode}
        serverModalPortalRef={serverModalPortalRef}
        serverModalSaving={serverModalSaving}
        setAgentForceSsh={setAgentForceSsh}
        setAgentInstallOS={setAgentInstallOS}
        setAgentModalData={setAgentModalData}
        setCredForm={setCredForm}
        setDockerComposeEditor={setDockerComposeEditor}
        setDockerLogsContent={setDockerLogsContent}
        setDockerLogsLoading={setDockerLogsLoading}
        setDockerLogsModalOpen={setDockerLogsModalOpen}
        setDockerLogsTail={setDockerLogsTail}
        setNetworkTargetForm={setNetworkTargetForm}
        setQuickDeployName={setQuickDeployName}
        setSelectedBatchServers={setSelectedBatchServers}
        setServerAddMode={setServerAddMode}
        setServerForm={setServerForm}
        setShowAddCredentialModal={setShowAddCredentialModal}
        setShowAgentModal={setShowAgentModal}
        setShowBatchAgentModal={setShowBatchAgentModal}
        setShowImportServerModal={setShowImportServerModal}
        setShowNetworkTargetModal={setShowNetworkTargetModal}
        setShowServerModal={setShowServerModal}
        setShowUpgradeModal={setShowUpgradeModal}
        setUpgradeFallbackSsh={setUpgradeFallbackSsh}
        showAddCredentialModal={showAddCredentialModal}
        showAgentModal={showAgentModal}
        showBatchAgentModal={showBatchAgentModal}
        showImportServerModal={showImportServerModal}
        showNetworkTargetModal={showNetworkTargetModal}
        showServerModal={showServerModal}
        showUpgradeModal={showUpgradeModal}
        testServerConnection={testServerConnection}
        testTrafficAlert={testTrafficAlert}
        uninstallAgent={uninstallAgent}
        updateDockerComposeDeployment={updateDockerComposeDeployment}
        upgradeBatchSnapshot={upgradeBatchSnapshot}
        upgradeFallbackSsh={upgradeFallbackSsh}
        upgradeLog={upgradeLog}
        upgradeLogViewportRef={upgradeLogViewportRef}
        upgradeProgress={upgradeProgress}
        upgrading={upgrading}
      />
    </div>
  );
}

export default ServerPage;
