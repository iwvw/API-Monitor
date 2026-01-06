/**
 * API Monitor - 主应用模块
 * 整合所有功能模块，初始化 Vue 应用
 */

// 日志控制（必须最先导入，覆盖 console 方法）
import './modules/logger.js';

// 导入样式
// 核心样式 (Critical CSS) - 首屏渲染必须
import '../css/styles.css';
import '../css/dashboard.css';
import '../css/modals.css';
import '../css/login.css';
import '../css/sidebar-nav.css';
import '../css/transitions.css';
import '../css/refined-ui.css';
import '../css/nav-grouped.css';
import '../css/refined-mobile.css'; // 移动端适配
// xterm.css moved to lazy load

// 懒加载样式 (Lazy Load CSS) - 非首屏模块
async function loadLazyCSS() {
  if (window.lazyCSSLoaded) return;
  window.lazyCSSLoaded = true;
  console.log('[System] Loading lazy CSS resources...');
  const styles = [
    import('../css/projects.css'),
    import('../css/cloudflare.css'),
    import('../css/tables.css'),
    import('../css/tabs.css'),
    import('../css/settings.css'),
    import('../css/logs.css'),
    import('../css/server.css'),
    import('../css/ssh-ide.css'),
    import('../css/ssh-ide.css'),
    // xterm.css 移至关键资源导入
    import('@xterm/xterm/css/xterm.css'),
    import('@applemusic-like-lyrics/core/style.css'),
    import('../css/antigravity.css'),
    import('../css/gemini-cli.css'),
    import('../css/openai.css'),
    import('../css/self-h.css'),
    import('../css/zeabur.css'),
    import('../css/koyeb.css'),
    import('../css/fly.css'),
    import('../css/r2.css'),
    import('../css/chat.css'),
    // import('../css/template.css'), // 模板文件，从构建中排除
    import('../css/stream-player.css'),
    import('plyr/dist/plyr.css'),
    import('../css/totp.css'),
    import('../css/music.css'),
    import('../css/uptime.css'),
  ];
  await Promise.all(styles);
  console.log('[System] Lazy CSS loaded');
}

// 导入模板加载器
import './template-loader.js';

// Vue and FontAwesome imports
// 必须导入 compile 函数以确保 Vue 模板编译器被打包（否则会被 tree-shaking 掉）
import { createApp, toRefs, compile } from 'vue';
// 强制保留 compile 引用到 window，防止被 Terser 优化掉
window.__VUE_COMPILE__ = compile;
import pinia from './stores/index.js';
import { useAuthStore } from './stores/auth.js';
import { useAppStore } from './stores/app.js';
import { useServerStore } from './stores/server.js';
// 图标字体从本地 npm 包加载
import '@fortawesome/fontawesome-free/css/all.min.css';
import 'simple-icons-font/font/simple-icons.min.css';

// 导入功能模块
import { dashboardMethods } from './modules/dashboard.js';
import { authMethods } from './modules/auth.js';
import { zeaburMethods } from './modules/zeabur.js';
import { renderMarkdown } from './modules/utils.js';
import { paasMethods } from './modules/paas.js';
import { koyebMethods } from './modules/koyeb.js';
import { flyMethods } from './modules/fly.js';
import { selfHMethods, selfHComputed } from './modules/self-h.js';
import { cloudflareMethods } from './modules/cloudflare.js';
import { r2Methods } from './modules/r2.js';
import { openaiMethods } from './modules/openai.js';
import { antigravityMethods } from './modules/antigravity.js';
import { geminiCliMethods } from './modules/gemini-cli.js';
import { settingsMethods } from './modules/settings.js';
import { systemLogsMethods } from './modules/logs.js';
import { logViewerMethods } from './modules/log-viewer.js';
import { transitionsMethods } from './modules/transitions.js';
import { hostMethods } from './modules/host.js';
import { metricsMethods } from './modules/metrics.js';
import { snippetsMethods } from './modules/snippets.js';
import { sshMethods } from './modules/ssh.js';
import { sftpMethods } from './modules/sftp.js';
import { serverStatusMethods } from './modules/server-status.js';
import { commonMethods } from './modules/common.js';
import { toast } from './modules/toast.js';
import { streamPlayerMethods } from './modules/stream-player-ui.js';
import { totpMethods, totpComputed, totpData } from './modules/totp.js';
import { musicMethods } from './modules/music.js';
import { uptimeData, uptimeMethods, uptimeComputed } from './modules/uptime.js';
import { formatDateTime, formatFileSize, maskAddress, formatRegion } from './modules/utils.js';

// 导入全局状态
import { store, MODULE_CONFIG, MODULE_GROUPS } from './store.js';
import { computed } from 'vue';

// 导入 Composables
import { useResponsive } from './composables/index.js';

// 创建并配置 Vue 应用
const app = createApp({
  setup() {
    const authStore = useAuthStore();
    const appStore = useAppStore();
    const serverStore = useServerStore();

    // 响应式布局 (使用 Composable)
    const responsive = useResponsive();

    // 自定义计算属性
    const openListPathParts = computed(() => selfHComputed.openListPathParts(store));
    const currentOpenListTempTab = computed(() => selfHComputed.currentOpenListTempTab(store));
    const openListTempPathParts = computed(() => selfHComputed.openListTempPathParts(store));
    const sortedOpenListFiles = computed(() => selfHComputed.sortedOpenListFiles(store));
    const isSelfHVideoActive = computed(() => {
      if (store.mainActiveTab !== 'self-h' || store.openListSubTab !== 'temp') return false;
      const activeTab = store.openListTempTabs.find(t => t.id === store.openListActiveTempTabId);
      return activeTab && activeTab.isVideo;
    });

    // 将 store 的所有属性转换为 refs，这样在模板中可以直接使用且保持响应式
    console.log('[App] Initializing setup, checking store.streamPlayer:', !!store.streamPlayer);

    // 如果 store.streamPlayer 没定义（罕见情况，静态定义应生效），补全它以防崩溃
    if (!store.streamPlayer) {
      console.warn('[App] streamPlayer not found in store, applying emergency fallback');
      store.streamPlayer = { visible: false, loading: false, playing: false };
    }

    const storeRefs = toRefs(store);

    return {
      store, // 导出原始 store 对象
      ...storeRefs,
      // 显式导出常用状态，确保始终存在
      streamPlayer: store.streamPlayer,
      mfpLyricsMode: computed({
        get: () => store.mfpLyricsMode,
        set: (v) => store.mfpLyricsMode = v
      }),
      mfpPlaylistMode: computed({
        get: () => store.mfpPlaylistMode,
        set: (v) => store.mfpPlaylistMode = v
      }),
      // Pinia Stores
      authStore,
      appStore,
      serverStore,
      // 响应式布局 (来自 Composable)
      isMobile: responsive.isMobile,
      isTablet: responsive.isTablet,
      isDesktop: responsive.isDesktop,
      windowWidth: responsive.windowWidth,
      // 手动挑选常用状态（兼容 index.html 现有引用，避免全量解构导致的 $ 属性冲突）
      isAuthenticated: computed(() => authStore.isAuthenticated),
      isCheckingAuth: computed(() => authStore.isCheckingAuth),
      showLoginModal: computed({
        get: () => authStore.showLoginModal,
        set: v => (authStore.showLoginModal = v),
      }),
      showSetPasswordModal: computed({
        get: () => authStore.showSetPasswordModal,
        set: v => (authStore.showSetPasswordModal = v),
      }),
      // 认证模块：登录/设置密码相关状态
      isDemoMode: computed(() => authStore.isDemoMode),
      loginPassword: computed({
        get: () => authStore.loginPassword,
        set: v => (authStore.loginPassword = v),
      }),
      loginError: computed({
        get: () => authStore.loginError,
        set: v => (authStore.loginError = v),
      }),
      setPassword: computed({
        get: () => authStore.setPassword,
        set: v => (authStore.setPassword = v),
      }),
      setPasswordConfirm: computed({
        get: () => authStore.setPasswordConfirm,
        set: v => (authStore.setPasswordConfirm = v),
      }),
      setPasswordError: computed({
        get: () => authStore.setPasswordError,
        set: v => (authStore.setPasswordError = v),
      }),
      mainActiveTab: computed({
        get: () => appStore.mainActiveTab,
        set: v => (appStore.mainActiveTab = v),
      }),
      opacity: computed({
        get: () => appStore.opacity,
        set: v => (appStore.opacity = v),
      }),

      openListPathParts,
      currentOpenListTempTab,
      openListTempPathParts,
      sortedOpenListFiles,
      isSelfHVideoActive,
      moduleGroups: appStore.moduleGroups,
    };
  },
  data() {
    return {
      // Zeabur 相关
      lastUpdate: '--:--:--',
      newAccount: {
        name: '',
        token: '',
        balance: '',
      },
      addingAccount: false,
      addAccountError: '',
      addAccountSuccess: '',
      expandedAccounts: {},
      refreshInterval: null,
      navLayout: 'bottom-normal', // top, bottom-normal, bottom
      agentDownloadUrl: '',
      publicApiUrl: '',
      zeaburRefreshInterval: 30000,
      refreshing: false,
      lastFetchAt: 0,
      minFetchInterval: 10000, // Zeabur 数据刷新最小间隔 10 秒
      // 批量添加
      batchAccounts: '',
      maskedBatchAccounts: '',
      batchAddError: '',
      batchAddSuccess: '',
      showAddZeaburAccountModal: false,

      // 刷新倒计时
      countdownInterval: null,

      // 主标签页
      previousMainTab: null,
      tabSwitchDebounce: null,

      // 响应式状态 - isMobile 现在由 useResponsive composable 在 setup 中管理
      location: window.location,

      // DNS 管理相关 - 表单状态
      showAddDnsAccountModal: false,
      dnsAccountForm: { name: '', apiToken: '', email: '' },
      dnsAccountFormError: '',
      dnsAccountFormSuccess: '',
      dnsSavingAccount: false,
      showEditDnsAccountModal: false,
      dnsEditingAccount: null,
      dnsEditAccountForm: { name: '', apiToken: '', email: '' },
      dnsEditAccountFormError: '',
      dnsEditAccountFormSuccess: '',
      showDnsRecordModal: false,
      dnsEditingRecord: null,
      dnsRecordForm: { type: 'A', name: '', content: '', ttl: 1, proxied: false, priority: 10 },
      dnsRecordFormError: '',
      dnsSavingRecord: false,
      dnsRecordTypes: ['A', 'AAAA', 'CNAME', 'TXT', 'MX', 'NS', 'SRV', 'CAA'],
      dnsQuickSwitchType: 'A',
      dnsQuickSwitchName: '',
      dnsQuickSwitchContent: '',
      dnsSwitching: false,
      dnsTemplates: [],
      showDnsTemplateModal: false,
      dnsEditingTemplate: null,
      dnsTemplateForm: {
        name: '',
        type: 'A',
        content: '',
        ttl: 1,
        proxied: false,
        description: '',
      },
      dnsTemplateFormError: '',
      dnsSavingTemplate: false,

      // Zone (域名) 管理相关
      showAddZoneModal: false,
      zoneForm: { name: '', jumpStart: false },
      zoneFormError: '',
      dnsSaving: false,

      // Workers 管理相关
      workers: [],
      workersLoading: false,
      workersSubdomain: null,
      workersCfAccountId: null, // Cloudflare 账号 ID，用于生成编辑器链接
      selectedWorker: null,
      workerEditorContent: '',
      showNewWorkerModal: false,
      newWorkerName: '',
      newWorkerScript: '',

      // Pages 管理相关
      pagesProjects: [],
      pagesLoading: false,
      agentInstallLoading: false,
      agentInstalling: false,
      agentForceSsh: false,
      batchAgentForceSsh: false,
      showPagesDeploymentsModal: false,
      selectedPagesProject: null,
      pagesDeployments: [],
      pagesDeploymentsLoading: false,

      // Worker 路由相关
      showWorkerRoutesModal: false,
      selectedWorkerForRoutes: null,
      workerRoutes: [],
      workerRoutesLoading: false,
      newRoutePattern: '',
      newRouteScript: '',

      // Pages 自定义域名相关
      showPagesDomainsModal: false,
      selectedPagesProjectForDomains: null,
      pagesDomains: [],
      pagesDomainsLoading: false,
      newPagesDomain: '',

      // Workers 自定义域名相关
      showWorkerDomainsModal: false,
      selectedWorkerForDomains: null,
      workerDomains: [],
      workerDomainsLoading: false,
      newWorkerDomain: '',

      // OpenAI API 管理相关
      openaiEditingEndpoint: null,
      openaiEndpointForm: { name: '', baseUrl: '', apiKey: '', notes: '' },
      openaiEndpointFormError: '',
      openaiSaving: false,
      openaiExpandedEndpoints: {},
      openaiBatchText: '',
      openaiBatchError: '',
      openaiBatchSuccess: '',
      openaiAdding: false,

      // Antigravity API 相关
      showAntigravityAccountModal: false,
      antigravityEditingAccount: null,
      antigravityAccountForm: {
        name: '',
        email: '',
        password: '',
        panelUser: 'admin',
        panelPassword: '',
      },
      antigravityAccountFormError: '',
      antigravityAccountFormSuccess: '',
      showAntigravityManualModal: false,
      antigravityManualForm: {
        name: '',
        accessToken: '',
        refreshToken: '',
        projectId: '',
        expiresIn: 3599,
      },
      antigravityManualFormError: '',
      // Antigravity API 相关
      agOauthUrl: '',
      agCustomProjectId: '',
      agAllowRandomProjectId: true,
      showOAuthExpand: false,
      // antigravityLogDetail: null, // Moved to store
      showAntigravityLogDetailModal: false,
      agSettingsForm: {
        API_KEY: '',
        PROXY: '',
        load_balancing_strategy: 'random',
      },
      showAgApiKey: false,
      antigravityModelRedirects: [],
      newRedirectSource: '',
      newRedirectTarget: '',

      // 图片预览
      showImagePreviewModal: false,
      previewImageUrl: '',

      // Gemini CLI API 相关
      geminiCliAccountForm: {
        name: '',
        client_id: '',
        client_secret: '',
        refresh_token: '',
        project_id: '',
      },
      geminiCliAccountFormError: '',
      geminiCliEditingAccount: null,
      // geminiCliLogDetail: null, // Moved to store
      // showGeminiCliLogDetailModal: false, // Moved to store
      geminiCliSettingsForm: {},

      // 主机管理相关
      expandedDockerPanels: new Set(),
      showServerModal: false,
      serverModalMode: 'add', // 'add' or 'edit'
      serverModalSaving: false,
      serverModalError: '',
      serverForm: {
        id: null,
        name: '',
        host: '',
        port: 22,
        username: '',
        authType: 'password', // 'password' or 'privateKey'
        password: '',
        privateKey: '',
        passphrase: '',
        tagsInput: '',
        description: '',
      },
      showImportServerModal: false,
      importModalSaving: false,
      importModalError: '',
      importPreview: null,
      showDockerModal: false,
      dockerModalServer: null,
      dockerModalData: null,
      dockerUpdateChecking: false,  // 是否正在检查更新
      dockerUpdateResults: [],       // 更新检测结果列表
      dockerOverviewServers: [],     // Docker 标签页的主机列表
      dockerOverviewLoading: false,  // Docker 概览加载状态
      expandedDockerHosts: [],       // 展开的 Docker 主机列表
      dockerSubTab: 'containers',    // Docker 子标签页
      dockerSelectedServer: '',      // 当前选中的主机 ID
      dockerResourceLoading: false,  // 资源加载状态
      dockerImages: [],              // 镜像列表
      dockerNetworks: [],            // 网络列表
      dockerVolumes: [],             // Volume 列表
      dockerStats: [],               // 容器资源统计
      showDockerLogsModal: false,    // 日志弹窗
      dockerLogsServerId: '',        // 日志目标服务器
      dockerLogsContainerId: '',     // 日志目标容器
      dockerLogsContainerName: '',   // 日志目标容器名
      dockerLogsTail: 100,           // 日志行数
      dockerLogsContent: '',         // 日志内容
      dockerLogsLoading: false,      // 日志加载状态
      containerMenuOpen: false,      // 容器菜单是否打开
      containerMenuPosition: { x: 0, y: 0 }, // 菜单位置
      containerMenuData: { serverId: '', containerId: '', containerName: '' }, // 菜单数据
      // Docker Compose
      dockerComposeProjects: [],     // Compose 项目列表
      // 容器创建
      showCreateContainerModal: false,
      createContainerForm: {
        name: '',
        image: '',
        ports: '',
        volumes: '',
        env: '',
        network: '',
        restart: 'unless-stopped',
      },
      createContainerLoading: false,
      showAddCredentialModal: false,
      credForm: {
        name: '',
        username: '',
        password: '',
      },
      credError: '',

      // 批量添加主机
      serverBatchText: '',
      serverBatchError: '',
      serverBatchSuccess: '',
      serverAddingBatch: false,
      isDraggingFile: false, // 文件拖拽状态

      // 主机筛选与自动更新
      probeStatus: '', // '', 'loading', 'success', 'error'

      // 服务器当前标签页
      serverCurrentTab: 'list',
      activeSSHSessionId: null, // 当前激活的 SSH 会话 ID
      visibleSessionIds: [], // 分屏显示的会话 ID 列表
      sshViewLayout: 'single', // 'single', 'split-h', 'split-v'
      sshSplitSide: '', // 分屏偏向 ('left', 'right')
      sshGroupState: null, // 分屏组状态快照 { ids: [], layout: '', side: '' }
      sshSyncEnabled: false, // 是否开启多屏同步操作
      draggedSessionId: null, // 正在拖拽的会话 ID
      dropTargetId: null, // 正在悬停的目标容器 ID
      dropHint: '', // 拖拽位置提示 ('left', 'right', 'top', 'bottom', 'center')
      sshIdeFullscreen: false, // SSH 屏幕全屏模式
      sshWindowFullscreen: false, // SSH 窗口全屏模式
      showSSHQuickMenu: false, // SSH 快速连接下拉菜单
      showSnippetsSidebar: false, // 代码片段侧边栏显隐

      // 代码片段相关
      sshSnippets: [],
      showSnippetModal: false,
      snippetSaving: false,
      snippetError: '',
      snippetForm: {
        id: null,
        title: '',
        content: '',
        category: 'common',
        description: '',
      },

      // SFTP 文件管理相关
      showSftpSidebar: false, // 文件管理侧边栏显隐
      sftpServerId: null, // 当前 SFTP 连接的服务器 ID
      sftpCurrentPath: '/', // 当前目录路径
      sftpFiles: [], // 当前目录文件列表
      sftpBreadcrumbs: [], // 路径导航
      sftpLoading: false, // 加载状态
      sftpError: '', // 错误信息
      sftpEditLoading: false, // 文件编辑加载状态
      sftpSaving: false, // 文件保存状态
      sftpUploading: false, // 文件上传状态
      showSftpEditorModal: false, // 文件编辑器弹窗
      sftpEditFile: null, // 当前编辑的文件 { path, name, content, originalContent }

      // 服务器状态侧栏
      showServerStatusSidebar: false, // 服务器状态侧边栏显隐
      serverStatusData: null, // 当前服务器状态数据
      serverStatusLoading: false, // 加载状态
      serverStatusError: '', // 错误信息


      // 终端相关
      showSSHTerminalModal: false,
      sshTerminalServer: null,
      sshTerminal: null,
      sshTerminalFit: null,
      sshCommandHistory: [],
      sshHistoryIndex: -1,
      sshCurrentCommand: '',
      // 多终端会话管理
      sshSessions: [], // { id, server, terminal, fit, type, buffer, history, historyIndex }
      showAddSessionSelectModal: false,
      // 主题观察器
      themeObserver: null,
      docObserver: null,
      themeUpdateTimer: null,
      monitorConfigSaving: false,
      monitorConfigError: '',
      monitorConfigSuccess: '',
      monitorLogs: [],
      monitorLogsLoading: false,
      logFilter: {
        serverId: '',
        status: '',
      },
      logPage: 1,
      logPageSize: 50,

      // 历史指标相关
      metricsHistoryList: [],
      metricsHistoryLoading: false,
      metricsHistoryTotal: 0,
      metricsHistoryFilter: {
        serverId: '',
      },
      metricsHistoryPagination: {
        page: 1,
        pageSize: 50,
        totalPages: 0,
      },
      metricsHistoryTimeRange: '1h', // '1h', '6h', '24h', '7d', 'all'
      metricsCollectorStatus: null,
      expandedMetricsServers: [], // 展开的主机 ID 列表
      metricsCollectInterval: 5, // 采集间隔（分钟）
      showMetricsCharts: false, // 负载趋势图表区域默认折叠

      // 拖拽状态 (UI only)
      draggedIndex: null,

      newPassword: '',
      confirmPassword: '',
      passwordError: '',
      passwordSuccess: '',
      customCss: '',
      customCssError: '',
      customCssSuccess: '',

      // 设置模态框
      settingsCurrentTab: 'general', // 'general', 'modules', 'database', 'logs', 'appearance', 'about'

      // 日志保留设置
      logSettings: {
        days: 0,
        count: 0,
        dbSizeMB: 0,
        logFileSizeMB: 10, // 日志文件最大大小(MB)
      },
      logSettingsSaving: false,
      logLimitsEnforcing: false,

      // Self-H (Self-Hosted) module state
      selfHCurrentTab: 'openlist',
      openListSubTab: 'overview',
      openListAccounts: [],
      openListStats: { onlineCount: 0 },
      currentOpenListAccount: null,
      newOpenListAcc: { name: '', api_url: '', api_token: '' },

      // CDN 配置状态 (构建时注入)
      cdnEnabled: typeof __USE_CDN__ !== 'undefined' ? __USE_CDN__ : false,
      cdnProvider: typeof __CDN_PROVIDER__ !== 'undefined' ? __CDN_PROVIDER__ : 'npmmirror',

      // TOTP 2FA 验证器模块
      ...totpData,

      // Uptime 监测模块
      ...uptimeData,
    };
  },

  // 计算属性
  computed: {
    // 获取当前激活的 SSH 会话对象
    currentSSHSession() {
      if (!this.activeSSHSessionId || this.sshSessions.length === 0) return null;
      return this.sshSessions.find(s => s.id === this.activeSSHSessionId) || null;
    },

    /**
     * 合并逻辑标签页：基于分屏快照或当前物理视图
     */
    sshTabList() {
      // 优先从快照中读取分屏组信息
      const groupData = this.sshGroupState;

      // 如果没有快照，且物理上也只有 1 个或没有，则显示散乱标签
      if (!groupData && this.visibleSessionIds.length <= 1) {
        return this.sshSessions;
      }

      // 确定实际要聚合的 ID 列表 (优先用快照，否则用物理)
      const splitIds = groupData ? groupData.ids : this.visibleSessionIds;
      if (!splitIds || splitIds.length <= 1) return this.sshSessions;

      const tabs = [];
      let groupInjected = false;

      // 创建聚合标签
      const groupTab = {
        id: 'ssh-group-tab',
        isGroup: true,
        name: `(${splitIds.length})分屏`,
        // 激活条件：当前 active session 属于组内成员
        active: splitIds.includes(this.activeSSHSessionId),
        sessions: this.sshSessions.filter(s => splitIds.includes(s.id)),
      };

      this.sshSessions.forEach(session => {
        if (splitIds.includes(session.id)) {
          if (!groupInjected) {
            tabs.push(groupTab);
            groupInjected = true;
          }
        } else {
          tabs.push(session);
        }
      });

      return tabs;
    },

    totalProjects() {
      let total = 0;
      for (const acc of this.accounts) {
        if (acc.projects) total += acc.projects.length;
      }
      return total;
    },

    totalServices() {
      let total = 0;
      for (const acc of this.accounts) {
        if (acc.projects) {
          for (const p of acc.projects) {
            if (p.services) total += p.services.length;
          }
        }
      }
      return total;
    },

    runningServices() {
      let total = 0;
      for (const acc of this.accounts) {
        if (acc.projects) {
          for (const p of acc.projects) {
            if (p.services) {
              for (const s of p.services) {
                if (s.status === 'RUNNING') total++;
              }
            }
          }
        }
      }
      return total;
    },

    totalCost() {
      let total = 0;
      for (const acc of this.accounts) {
        if (acc.data && acc.data.totalCost !== undefined) {
          total += acc.data.totalCost || 0;
        } else if (acc.projects) {
          for (const p of acc.projects) {
            total += p.cost || 0;
          }
        }
      }
      return total;
    },

    /**
     * 实现主机列表的实时筛选
     */
    filteredServerList() {
      let list = this.serverList;

      // 状态筛选
      if (this.serverStatusFilter !== 'all') {
        list = list.filter(item => item.status === this.serverStatusFilter);
      }

      // 搜索文本筛选 (名称、IP、标签)
      if (this.serverSearchText.trim()) {
        const search = this.serverSearchText.toLowerCase();
        list = list.filter(item => {
          const nameMatch = item.name && item.name.toLowerCase().includes(search);
          const hostMatch = item.host && item.host.toLowerCase().includes(search);
          const tagMatch = item.tags && item.tags.some(tag => tag.toLowerCase().includes(search));
          return nameMatch || hostMatch || tagMatch;
        });
      }

      return list;
    },

    /**
     * 按主机分组的历史记录
     */
    groupedMetricsHistory() {
      const grouped = {};
      for (const record of this.metricsHistoryList) {
        const serverId = record.server_id || 'unknown';
        if (!grouped[serverId]) {
          grouped[serverId] = [];
        }
        grouped[serverId].push(record);
      }
      return grouped;
    },

    /**
     * 计算当前可见的模块数量
     */
    visibleModulesCount() {
      if (!this.moduleVisibility) return 0;
      return Object.values(this.moduleVisibility).filter(v => v).length;
    },

    /**
     * 判断当前是否有任何模态框打开
     */
    isAnyModalOpen() {
      return (
        this.showSettingsModal ||
        this.logViewer.visible ||
        this.showAddZeaburAccountModal ||
        this.showAddKoyebAccountModal ||
        this.showAddFlyAccountModal ||
        this.showAddDnsAccountModal ||
        this.showEditDnsAccountModal ||
        this.showDnsRecordModal ||
        this.showDnsTemplateModal ||
        this.showOpenaiEndpointModal ||
        this.showServerModal ||
        this.showImportServerModal ||
        this.showDockerModal ||
        this.showSSHTerminalModal ||
        this.showAntigravityAccountModal ||
        this.showAddSessionSelectModal ||
        this.showAntigravityLogDetailModal ||
        this.showGeminiCliLogDetailModal ||
        this.showGeminiCliAccountModal ||
        this.showAntigravityManualModal ||
        this.showAddCredentialModal ||
        this.showNewWorkerModal ||
        this.showWorkerRoutesModal ||
        this.showWorkerDomainsModal ||
        this.showPagesDeploymentsModal ||
        this.showPagesDomainsModal ||
        this.showImagePreviewModal ||
        this.showAddZoneModal ||
        this.showTotpModal ||
        this.showTotpImportModal ||
        (this.customDialog && this.customDialog.show)
      );
    },

    // TOTP 验证器模块计算属性
    ...totpComputed,

    // 聊天界面可用的模型列表 (根据端点筛选)
    filteredChatModels() {
      // 1. 如果选中了特定端点，直接使用该端点的原始模型列表
      if (store.openaiChatEndpoint) {
        const selectedEndpoint = store.openaiEndpoints.find(ep => ep.id === store.openaiChatEndpoint);
        if (selectedEndpoint && selectedEndpoint.models) {
          // 将字符串数组转为对象数组，并过滤
          return selectedEndpoint.models
            .filter(id => !store.openaiHiddenModels.includes(id) || store.openaiPinnedModels.includes(id))
            .map(id => ({
              id: id,
              owned_by: selectedEndpoint.name || 'custom' // 统一组名，虽然界面上可能不再分组
            }));
        }
        return [];
      }

      // 2. 自动/全量模式：聚合所有端点的列表
      // 注意：store.openaiAllModels 可能不全，所以我们需要遍历所有 endpoints 来构建完整列表
      const allModelsMap = new Map();

      // 先加入 store.openaiAllModels (如果有基本信息)
      if (store.openaiAllModels && store.openaiAllModels.length) {
        store.openaiAllModels.forEach(m => allModelsMap.set(m.id, m));
      }

      // 遍历所有端点进行补充
      if (store.openaiEndpoints) {
        store.openaiEndpoints.forEach(ep => {
          if (ep.models && Array.isArray(ep.models)) {
            ep.models.forEach(m => {
              const id = typeof m === 'string' ? m : m.id;
              if (!allModelsMap.has(id)) {
                allModelsMap.set(id, {
                  id: id,
                  owned_by: ep.name || 'custom',
                  object: 'model',
                  created: Date.now()
                });
              }
            });
          }
        });
      }

      let models = Array.from(allModelsMap.values());

      // 默认过滤掉隐藏的模型 (除非已收藏)
      models = models.filter(m =>
        !store.openaiHiddenModels.includes(m.id) || store.openaiPinnedModels.includes(m.id)
      );

      return models;
    },

    // 下拉框内搜索过滤后的模型列表
    dropdownFilteredChatModels() {
      let models = this.filteredChatModels;
      if (store.dropdownModelSearch) {
        const search = store.dropdownModelSearch.toLowerCase();
        models = models.filter(m => m.id.toLowerCase().includes(search));
      }
      return models;
    },

    // 模型管理页面过滤后的模型列表
    filteredModelsForManagement() {
      let models = [];

      // 1. 确定基础模型列表
      if (store.openaiSelectedEndpointId) {
        // 如果选择了特定端点，直接以该端点的 models 为准
        const selectedEndpoint = store.openaiEndpoints.find(ep => ep.id === store.openaiSelectedEndpointId);
        if (selectedEndpoint && selectedEndpoint.models && selectedEndpoint.models.length > 0) {
          models = selectedEndpoint.models.map(m => {
            const id = typeof m === 'string' ? m : m.id;
            // 尝试从全局列表中获取详细信息
            const existing = (store.openaiAllModels || []).find(am => am.id === id);
            if (existing) return existing;
            // 降级构造基础对象
            return { id: id, owned_by: selectedEndpoint.name || 'custom', object: 'model' };
          });
        }
      } else {
        // 聚合所有端点的模型
        const allModelsMap = new Map();

        // 1. 先加入 store.openaiAllModels (如果有基本信息)
        if (store.openaiAllModels && store.openaiAllModels.length) {
          store.openaiAllModels.forEach(m => allModelsMap.set(m.id, m));
        }

        // 2. 遍历所有端点进行补充
        if (store.openaiEndpoints) {
          store.openaiEndpoints.forEach(ep => {
            if (ep.models && Array.isArray(ep.models)) {
              ep.models.forEach(m => {
                const id = typeof m === 'string' ? m : m.id;
                // 如果该模型尚未存在，或者已存在但没有详细信息（例如只是个ID占位），则尝试充实它
                if (!allModelsMap.has(id)) {
                  allModelsMap.set(id, {
                    id: id,
                    owned_by: ep.name || 'custom',
                    object: 'model',
                    created: Date.now()
                  });
                }
              });
            }
          });
        }

        models = Array.from(allModelsMap.values());
      }

      // 2. 根据搜索过滤
      if (store.openaiModelSearch) {
        const search = store.openaiModelSearch.toLowerCase();
        models = models.filter(m =>
          m.id.toLowerCase().includes(search) ||
          (m.owned_by && m.owned_by.toLowerCase().includes(search))
        );
      }

      // 3. 根据隐藏状态过滤
      if (!store.openaiShowHiddenModels) {
        models = models.filter(m => !(store.openaiHiddenModels || []).includes(m.id));
      }

      return models;
    },
  },

  async mounted() {
    // 全局点击监听，用于关闭自定义下拉菜单
    document.addEventListener('click', this.closeAllDropdowns);

    // 0. 检测单页模式 (通过 URL 路径直接访问模块)
    this.detectSinglePageMode();

    // 1. 核心数据与 UI 重置 (立即执行)
    window.vueApp = this;
    this.loadModuleSettings();
    this.updateBrowserThemeColor();

    // 2. 尝试从缓存恢复主机列表 (实现瞬间展示)
    if (this.mainActiveTab === 'server') {
      this.loadFromServerListCache();
    }

    // 注: 窗口大小监听现在由 useResponsive composable 自动管理

    // 3. 异步认证与关键数据加载
    this.authStore.checkAuth().then(() => {
      if (this.authStore.isAuthenticated) {
        // 关键业务数据
        this.loadManagedAccounts();
        this.loadProjectCosts();
        this.loadSnippets();
        this.loadCredentials();

        // 启动自动刷新
        this.startAutoRefresh();

        // 如果当前在仪表盘页，立即加载
        if (this.mainActiveTab === 'dashboard') {
          this.initDashboard();
        }

        // 如果当前在主机页，立即加载
        if (this.mainActiveTab === 'server') {
          this.loadServerList();
        }

        // 如果当前在 Self-H 页（单页模式），立即加载
        if (this.mainActiveTab === 'self-h') {
          this.loadOpenListAccounts();
        }
      }
    });

    // 4. 延迟加载非核心功能 (500ms 后执行，不影响首屏渲染)
    setTimeout(() => {
      // 初始化辅助组件 (方法需在 methods 中定义)
      this.initSshMountObserver();
      this.initGlobalImageProxy();
      this.initGlobalTooltipEngine();
      this.initMobileGestures();
      this.initGlobalKeyListeners();

      // 深色模式自动适配
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        this.updateBrowserThemeColor();
      });

      // 标签页可见性监听
      // 注释：Socket.IO 实时流会自动维持数据更新，无需切屏刷新
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible' && this.isAuthenticated) {
          // 仅重新连接已断开的实时流，不触发完整刷新
          if (this.mainActiveTab === 'server' && this.serverCurrentTab === 'list') {
            if (!this.metricsWsConnected) {
              this.connectMetricsStream();
            }
          }
        }
      });

      console.log('[System] 非核心功能加载完成');
    }, 500);
  },


  watch: {
    // 监听图表区域展开状态，展开时渲染图表
    showMetricsCharts(newVal) {
      if (newVal) {
        this.$nextTick(() => {
          this.renderMetricsCharts();
        });
      }
    },

    // 监听全局模态框状态，控制背景滚动
    isAnyModalOpen(newVal) {
      if (newVal) {
        document.body.classList.add('modal-open');
      } else {
        document.body.classList.remove('modal-open');
      }
    },

    // 监听认证检查状态，移除启动屏 (FCP 优化，防闪屏)
    isCheckingAuth(newVal) {
      if (newVal === false) {
        // 等待 Vue 完成 DOM 更新后再移除加载屏幕
        this.$nextTick(() => {
          // 再等待一帧确保浏览器完成渲染
          requestAnimationFrame(() => {
            requestAnimationFrame(() => {
              // 通过 store 状态控制类，避免 Vue 的 :class 绑定覆盖
              store.appRevealed = true;

              // 延迟启用过渡动画，确保初始数据加载完成后才启用
              // 在此期间，CSS 规则 .app-wrapper:not(.app-ready) * { transition: none } 生效
              setTimeout(() => {
                store.appReady = true;
                console.log('[System] App ready, transitions enabled');
              }, 1000);

              const loader = document.getElementById('app-loading');
              if (loader) {
                loader.style.opacity = '0';
                setTimeout(() => loader.remove(), 350);
              }
            });
          });
        });
      }
    },

    'monitorConfig.interval'(newVal) {
      console.log('主机刷新间隔变更为:', newVal, '秒，重启轮询');
      this.startServerPolling();
    },

    settingsCurrentTab(newVal) {
      if (newVal === 'logs') {
        // 进入日志标签页：加载日志数据和设置，并自动连接日志流
        this.initLogWs();
        this.fetchLogSettings();
        // 自动连接 WebSocket 日志流
        this.connectLogStream();
      } else if (newVal === 'database') {
        // 进入数据库标签页时加载日志保留设置和数据库统计
        this.fetchLogSettings();
        this.fetchDbStats();
      } else {
        // 离开日志标签页：关闭 WebSocket 连接
        this.closeLogWs();
        if (this.logsAutoRefreshTimer) {
          clearInterval(this.logsAutoRefreshTimer);
          this.logsAutoRefreshTimer = null;
        }
      }
    },

    serverCurrentTab: {
      handler(newVal) {
        // 1. 指标流连接管理 - 仅在列表页时连接
        if (newVal === 'list' && this.mainActiveTab === 'server') {
          this.connectMetricsStream();
        }

        // 2. 标签页特定数据加载（仅在数据为空时加载，避免切换时重复刷新）
        if (newVal === 'management') {
          this.loadMonitorConfig();
          this.loadMonitorLogs();
          this.loadCredentials();
          // 仅在列表为空时才加载
          if (this.serverList.length === 0) {
            this.loadServerList();
          }
        } else if (newVal === 'list') {
          // 已有 Socket.IO 实时数据时不刷新（避免跳变）
          if (this.serverList.length === 0) {
            this.loadServerList();
          } else {
            // 为已展开的卡片重新加载图表（切换标签页后 canvas 需要重新渲染）
            this.$nextTick(() => {
              if (this.expandedServers && this.expandedServers.length > 0) {
                this.expandedServers.forEach(serverId => {
                  const server = this.serverList.find(s => s.id === serverId);
                  if (server) {
                    setTimeout(() => this.loadCardMetrics(server), 300);
                  }
                });
              }
            });
          }
        } else if (newVal === 'terminal') {
          // 切换到 终端视图时，恢复 DOM 挂载并调整大小
          this.$nextTick(() => {
            this.syncTerminalDOM();
            const session = this.sshSessions.find(s => s.id === this.activeSSHSessionId);
            if (session) {
              setTimeout(() => {
                this.safeTerminalFit(session);
                if (session.terminal) session.terminal.focus();
              }, 150);
            }
          });
        }
      },
      immediate: true,
    },

    showSettingsModal(newVal) {
      if (newVal) {
        this.settingsCurrentTab = 'general'; // Reset to general tab
        this.$nextTick(() => this.focusModalOverlay('.settings-sidebar'));

        // 加载必要的设置数据供全局或模块配置使用
        this.loadAntigravitySettings();
        this.loadGeminiCliSettings();
      }
    },

    serverIpDisplayMode(newVal) {
      // 更新全局 store
      store.serverIpDisplayMode = newVal;

      // 发送全局自定义事件，让非 Vue 渲染的模块感知
      window.dispatchEvent(new CustomEvent('server-display-mode-changed', { detail: newVal }));

      // 触发 UI 重新渲染 (针对 innerHTML 渲染的部分)
      if (window.serverModule && window.serverModule.renderServerList) {
        window.serverModule.renderServerList();
      }
      // 自动保存到后端
      this.saveUserSettingsToServer();
    },

    'openaiChatMessages.length'() {
      if (this.mainActiveTab === 'openai') {
        this.checkAndScrollOnNewMessage();
      }
    },

    mainActiveTab: {
      handler(newVal, oldVal) {
        // 1. 终端保护与恢复逻辑
        // [离开保护] 如果离开主机管理模块，强制将 DOM 节点搬回仓库，防止被销毁
        if (oldVal === 'server') {
          this.saveTerminalsToWarehouse();
          // 离开时不要关闭指标流，保持后台更新
        }

        // [切回恢复] 当重新进入时，重新挂载
        if (newVal === 'server') {
          this.$nextTick(() => {
            this.syncTerminalDOM();
            this.fitAllVisibleSessions();
            setTimeout(() => {
              this.syncTerminalDOM();
              this.fitAllVisibleSessions();
            }, 300);
          });

          // 如果在主机列表页，开启实时指标流
          if (this.serverCurrentTab === 'list') {
            this.connectMetricsStream();
          }
        }

        // 2. 浏览器与 UI 适配
        this.updateBrowserThemeColor();

        // 3. 通用的数据加载逻辑（需已认证）
        if (this.isAuthenticated) {
          this.$nextTick(() => {
            switch (newVal) {
              case 'dashboard':
                this.initDashboard();
                break;
              case 'paas':
                if (this.paasCurrentPlatform === 'zeabur') {
                  if (this.accounts.length === 0) {
                    this.loadFromZeaburCache();
                  }
                  if (!this.dataRefreshPaused) {
                    this.startAutoRefresh();
                  }
                } else if (this.paasCurrentPlatform === 'koyeb') {
                  if (this.koyebAccounts.length === 0) {
                    this.loadFromKoyebCache();
                  }
                  if (!this.koyebDataRefreshPaused) {
                    this.startKoyebAutoRefresh();
                    this.loadKoyebData();
                  }
                } else if (this.paasCurrentPlatform === 'fly') {
                  if (this.flyAccounts.length === 0) {
                    this.loadFromFlyCache();
                  }
                  if (!this.flyDataRefreshPaused) {
                    this.startFlyAutoRefresh();
                    this.loadFlyData();
                  }
                }
                break;
              case 'dns':
                if (this.dnsAccounts.length === 0) {
                  this.loadFromDnsAccountsCache();
                  this.loadDnsAccounts(true);
                  this.loadDnsTemplates();
                }
                break;
              case 'compute':
                if (this.dnsAccounts.length === 0) {
                  this.loadFromDnsAccountsCache();
                  this.loadDnsAccounts(true);
                }
                if (this.computeCurrentTab === 'workers') {
                  this.loadWorkers();
                } else {
                  this.loadPages();
                }
                break;
              case 'openai':
                if (this.openaiEndpoints.length === 0) {
                  this.loadFromOpenaiCache();
                  this.loadOpenaiEndpoints(true);
                }
                break;
              case 'server':
                // 始终加载列表以确保状态同步和触发卡片监控加载
                this.loadFromServerListCache();
                this.loadServerList();

                // 如果当前选中的是管理子标签，确保加载配置和相关数据
                if (this.serverCurrentTab === 'management') {
                  this.loadMonitorConfig();
                  this.loadMonitorLogs();
                }
                break;
              case 'self-h':
                this.loadOpenListAccounts();
                break;
              case 'antigravity':
                this.loadAntigravityAutoCheckSettings(); // 加载并启动定时检测
                if (this.antigravityCurrentTab === 'quotas') {
                  if (this.loadAntigravityQuotas) this.loadAntigravityQuotas();
                }
                break;
              case 'gemini-cli':
                this.initGeminiCli();
                this.loadGeminiCliAutoCheckSettings(); // 确保加载设置
                break;
              case 'totp':
                this.loadTotpAccounts();
                this.startTotpTimer();
                break;
              case 'ai-chat':
                this.aiChatInit();
                break;
              case 'uptime':
                this.initUptimeModule();
                break;
            }
          });
        }

        // Music 模块独立初始化 (有自己的登录系统，不依赖应用认证)
        if (newVal === 'music') {
          this.$nextTick(() => {
            this.initMusicModule();
          });
        }

        // 4. 轮询管理
        if (newVal !== 'antigravity' || this.antigravityCurrentTab !== 'quotas') {
          if (this.stopAntigravityQuotaPolling) {
            this.stopAntigravityQuotaPolling();
          }
        }
      },
      immediate: true,
    },

    // 认证成功后加载当前标签页数据
    isAuthenticated(newVal) {
      if (newVal) {
        // 登录成功，从后端加载用户设置并启动指标流
        this.loadModuleSettings();
        this.connectMetricsStream();

        // ⚡ 关键：立即加载后台定时任务设置（无论当前在哪个标签页）
        // 这些定时检测需要在后台持续运行，不能等用户切换到对应模块才启动
        this.loadAntigravityAutoCheckSettings();
        this.loadGeminiCliAutoCheckSettings();
        console.log('[System] 后台定时检测设置已加载');

        // 懒加载非核心样式 (异步加载，不阻塞首屏)
        // 由于 CSS 规则 .app-wrapper:not(.app-ready) * { transition: none } 生效中
        // CSS 注入不会导致过渡动画闪烁
        if ('requestIdleCallback' in window) {
          window.requestIdleCallback(() => loadLazyCSS());
        } else {
          setTimeout(() => loadLazyCSS(), 100);
        }

        // 加载当前激活标签页的数据
        this.$nextTick(() => {
          switch (this.mainActiveTab) {
            case 'dashboard':
              this.initDashboard();
              break;
            case 'paas':
              if (this.paasCurrentPlatform === 'zeabur') {
                this.loadFromZeaburCache();
              } else if (this.paasCurrentPlatform === 'koyeb') {
                this.loadKoyebData();
              } else if (this.paasCurrentPlatform === 'fly') {
                this.loadFlyData();
              }
              break;
            case 'dns':
              // 优先加载缓存，然后后台刷新
              this.loadFromDnsAccountsCache();
              this.loadDnsAccounts(true);
              this.loadDnsTemplates();
              break;
            case 'openai':
              // 优先加载缓存，然后后台刷新
              this.loadFromOpenaiCache();
              this.loadOpenaiEndpoints(true);
              break;
            case 'self-h':
              this.loadOpenListAccounts();
              break;
            case 'server':
              // 统一入口：加载主机列表并触发已展开卡片的指标渲染
              this.loadServerList();
              break;
            case 'antigravity':
              this.loadAntigravityAutoCheckSettings();
              if (this.antigravityCurrentTab === 'quotas') {
                if (this.loadAntigravityQuotas) this.loadAntigravityQuotas();
              }
              break;
            case 'gemini-cli':
              this.initGeminiCli();
              this.loadGeminiCliAutoCheckSettings();
              break;
            case 'totp':
              this.loadTotpAccounts();
              this.startTotpTimer();
              break;
            case 'uptime':
              this.initUptimeModule();
              // Hook for auto-refresh if needed, but socket handles it
              break;
          }
        });
      }
    },

    // PaaS 平台标签页切换监听
    paasCurrentTab(newVal) {
      if (newVal === 'zeabur') {
        this.paasCurrentPlatform = 'zeabur';
        if (this.accounts.length === 0) {
          this.loadFromZeaburCache();
        }
        // 启动 Zeabur 自动刷新
        if (!this.dataRefreshPaused) {
          this.startAutoRefresh();
        }
        // 停止 Koyeb 自动刷新
        this.stopKoyebAutoRefresh();
      } else if (newVal === 'koyeb') {
        this.paasCurrentPlatform = 'koyeb';
        // 优先加载缓存
        if (this.koyebAccounts.length === 0) {
          this.loadFromKoyebCache();
        }
        if (this.koyebManagedAccounts.length === 0) {
          this.loadKoyebManagedAccounts();
        }
        // 启动 Koyeb 自动刷新
        if (!this.koyebDataRefreshPaused) {
          this.startKoyebAutoRefresh();
          this.loadKoyebData(); // 立即触发一次
        }
        // 停止 Zeabur 自动刷新
        this.stopAutoRefresh();
      } else if (newVal === 'fly') {
        this.paasCurrentPlatform = 'fly';
        // 优先加载缓存
        if (this.flyAccounts.length === 0) {
          this.loadFromFlyCache();
        }
        if (this.flyManagedAccounts.length === 0) {
          this.loadFlyManagedAccounts();
        }
        // 启动 Fly.io 自动刷新
        if (!this.flyDataRefreshPaused) {
          this.startFlyAutoRefresh();
          this.loadFlyData(); // 立即触发一次
        }
        // 停止其他自动刷新
        this.stopAutoRefresh();
        this.stopKoyebAutoRefresh();
      } else {
        // 其他标签页，停止所有自动刷新
        this.stopAutoRefresh();
        this.stopKoyebAutoRefresh();
        this.stopFlyAutoRefresh();

        if (newVal === 'accounts') {
          // 加载三个平台的账号
          if (this.managedAccounts.length === 0) {
            this.loadManagedAccounts();
          }
          if (this.koyebManagedAccounts.length === 0) {
            this.loadKoyebManagedAccounts();
          }
          if (this.flyManagedAccounts.length === 0) {
            this.loadFlyManagedAccounts();
          }
        }
      }
    },

    // 自定义确认对话框 - 需要深度监听
    'customDialog.show'(newVal) {
      if (newVal) {
        this.$nextTick(() => {
          // 如果是 prompt 类型，聚焦输入框
          if (this.customDialog.isPrompt && this.$refs.promptInput) {
            this.$refs.promptInput.focus();
          } else {
            this.focusModalOverlay('.custom-dialog-overlay');
          }
        });
      }
    },

    // 各种模态框的聚焦处理
    showAddZeaburAccountModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showAddKoyebAccountModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showAddFlyAccountModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showAddDnsAccountModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showEditDnsAccountModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showDnsRecordModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showDnsTemplateModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showOpenaiEndpointModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showServerModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showImportServerModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showDockerModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showSSHTerminalModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showAntigravityAccountModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    opacity(newVal) {
      localStorage.setItem('card_opacity', newVal);
      this.updateOpacity();
    },

    showAddSessionSelectModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    // 搜索范围变更后自动刷新结果
    openListSearchScope() {
      if (this.mainActiveTab === 'self-h') {
        if (this.openListSubTab === 'files' && store.openListSearchActive) {
          // 如果在主列表且搜索激活，刷新主列表搜索
          this.searchOpenListFiles(store.openListSearchText);
        } else if (this.openListSubTab === 'temp' && this.currentOpenListTempTab?.isSearch) {
          // 如果在临时搜索标签页，刷新该标签页
          this.loadTempTabFiles(
            this.currentOpenListTempTab.path,
            false,
            this.currentOpenListTempTab.id
          );
        }
      }
    },
  },

  beforeUnmount() {
    document.removeEventListener('click', this.closeAllDropdowns);
    // 清理定时器，防止内存泄漏
    if (this.refreshInterval) {
      clearInterval(this.refreshInterval);
    }
    if (this.countdownInterval) {
      clearInterval(this.countdownInterval);
    }
    if (this.logsScrollTimer) {
      clearInterval(this.logsScrollTimer);
    }
    if (this.logsRealTimeTimer) {
      clearInterval(this.logsRealTimeTimer);
    }
    this.stopServerPolling();
    this.stopKoyebAutoRefresh();
  },

  methods: {
    // ==================== 功能模块 ====================
    ...dashboardMethods,
    ...authMethods,
    ...zeaburMethods,
    ...paasMethods,
    ...koyebMethods,
    ...flyMethods,
    ...selfHMethods,
    ...cloudflareMethods,
    ...r2Methods,
    ...openaiMethods,
    ...antigravityMethods,
    ...geminiCliMethods,
    ...settingsMethods,
    ...systemLogsMethods,
    ...logViewerMethods,
    ...transitionsMethods,

    // ==================== 核心模块 ====================
    ...hostMethods,
    ...metricsMethods,
    ...snippetsMethods,
    ...sshMethods,
    ...sftpMethods,
    ...serverStatusMethods,
    ...commonMethods,
    ...streamPlayerMethods,
    ...totpMethods,
    ...musicMethods,
    ...uptimeMethods,

    // ==================== 工具函数 ====================
    formatDateTime,
    formatFileSize,
    formatRegion,
    renderMarkdown,

    // 带缓存的日志内容渲染（避免重复解析 Base64 图片）
    getCachedLogHtml(obj, field = 'content') {
      if (!obj) return '';
      const content = obj[field];
      if (content === undefined || content === null) return '';

      // 生成缓存 key
      const cacheKey = `_cached_${field}`;
      const contentKey = `_cachedSource_${field}`;

      // 检查缓存是否有效
      const contentHash = typeof content === 'string' ? content : JSON.stringify(content);
      if (obj[cacheKey] && obj[contentKey] === contentHash) {
        return obj[cacheKey];
      }

      // 渲染并缓存
      const html = renderMarkdown(content);
      obj[cacheKey] = html;
      obj[contentKey] = contentHash;
      return html;
    },

    /**
     * 检测单页模式 - 通过 URL 路径直接访问特定模块
     * 支持格式: /s/模块名 (使用 MODULE_CONFIG 中的 name 或 shortName)
     * 例如: /s/AntiG, /s/2FA, /s/Hosts, /s/OpenAI
     */
    detectSinglePageMode() {
      const pathname = window.location.pathname;
      console.log('[SinglePageMode] 检测路径:', pathname);

      // 优先检测 /s/:module 格式 (单页模式专用路由)
      const singlePageMatch = pathname.match(/^\/s\/([^/]+)/i);
      if (!singlePageMatch) {
        console.log('[SinglePageMode] 非单页模式路径');
        return;
      }

      const rawModuleName = singlePageMatch[1];
      console.log('[SinglePageMode] 匹配到模块名:', rawModuleName);

      // 从 MODULE_CONFIG 构建名称到模块 ID 的映射
      // 支持 name、shortName 和模块 ID 本身（不区分大小写）
      const nameToModuleId = {};
      for (const [moduleId, config] of Object.entries(MODULE_CONFIG)) {
        // 模块 ID 本身
        nameToModuleId[moduleId.toLowerCase()] = moduleId;
        // name 属性
        if (config.name) {
          nameToModuleId[config.name.toLowerCase()] = moduleId;
        }
        // shortName 属性
        if (config.shortName) {
          nameToModuleId[config.shortName.toLowerCase()] = moduleId;
        }
      }

      console.log('[SinglePageMode] 可用映射:', Object.keys(nameToModuleId));

      // 匹配模块
      const moduleName = rawModuleName.toLowerCase();
      const moduleId = nameToModuleId[moduleName];

      if (moduleId) {
        // 使用 this 访问响应式状态确保 Vue 能感知变化
        this.singlePageMode = true;
        this.mainActiveTab = moduleId;
        // 添加到 html 和 body，确保与 head 中的脚本一致
        document.documentElement.classList.add('single-page-mode');
        document.body.classList.add('single-page-mode');

        // 获取模块配置用于标题
        const config = MODULE_CONFIG[moduleId];
        const title = config ? config.name : moduleId;
        document.title = `API Monitor - ${title}`;

        console.log(`[SinglePageMode] ✅ 已激活单页模式: /s/${rawModuleName} -> ${moduleId}`);
        console.log('[SinglePageMode] mainActiveTab =', this.mainActiveTab);
        console.log('[SinglePageMode] singlePageMode =', this.singlePageMode);
      } else {
        console.warn(`[SinglePageMode] ❌ 未找到模块: ${rawModuleName}`);
      }
    },

    // ==================== 分组导航方法 ====================

    /**
     * 获取分组中可见的模块列表
     */
    getVisibleModulesInGroup(modules) {
      return modules.filter(m => this.moduleVisibility[m]);
    },

    /**
     * 判断分组是否包含当前激活的模块
     */
    isGroupActive(modules) {
      return modules.includes(this.mainActiveTab);
    },

    /**
     * 切换分组展开状态
     */
    toggleNavGroup(groupId) {
      if (this.navGroupExpanded === groupId) {
        this.navGroupExpanded = null;
      } else {
        this.navGroupExpanded = groupId;
      }
    },

    /**
     * 处理分组按钮悬停 (PC端悬浮展开)
     */
    handleGroupHover(groupId) {
      if (this.isMobile) return;
      // 清除可能存在的关闭定时器
      if (this._groupLeaveTimer) {
        clearTimeout(this._groupLeaveTimer);
        this._groupLeaveTimer = null;
      }
      this.navGroupExpanded = groupId;
    },

    /**
     * 处理鼠标离开分组区域 (延迟关闭，避免抖动)
     */
    handleGroupLeave() {
      if (this.isMobile) return;
      this._groupLeaveTimer = setTimeout(() => {
        this.navGroupExpanded = null;
      }, 150);
    },

    /**
     * 取消分组关闭（鼠标移入下拉菜单时）
     */
    cancelGroupLeave() {
      if (this._groupLeaveTimer) {
        clearTimeout(this._groupLeaveTimer);
        this._groupLeaveTimer = null;
      }
    },

    /**
     * 处理分组内模块点击
     */
    handleGroupModuleClick(module) {
      this.handleTabSwitch(module);
      // 点击后关闭下拉菜单
      this.navGroupExpanded = null;
    },

    /**
     * 自动调整文本框高度
     */
    autoResizeTextarea(event) {
      const el = event.target;
      el.style.height = 'auto';
      el.style.height = el.scrollHeight + 'px';
    },
  },
});

// ==================== 应用初始化 ====================

/**
 * 异步初始化应用
 * 在挂载 Vue 之前加载动态 HTML 模板，确保模块存在于 DOM 中
 */
async function initApp() {
  console.log('[App] Starting initialization...');
  const startTime = Date.now();

  try {
    // 1. 加载所有模块模板 (必须在 Vue 挂载前加载，否则 Vue 无法编译其中的指令)
    if (window.TemplateLoader) {
      await window.TemplateLoader.loadAll();
    } else {
      console.warn('[App] TemplateLoader not found, proceeding with fallback');
    }

    // 2. 挂载 Vue 应用
    app.use(pinia);
    window.vueApp = app.mount('#app');

    // 3. 启动全局时间更新定时器 (每秒触发一次，用于倒计时)
    const appStore = useAppStore();
    setInterval(() => {
      store.currentTime = Date.now();
      appStore.updateCurrentTime();
    }, 1000);

    const elapsed = Date.now() - startTime;
    console.log(`[App] Initialized and mounted in ${elapsed}ms`);

    // 4. 延迟加载非核心资源 (样式) - 移至认证成功后加载
    // requestAnimationFrame(() => {
    //   // 懒加载其余样式
    //   loadLazyCSS();
    // });
  } catch (error) {
    console.error('[App] Critical failure during initialization:', error);
    // 即使模板加载失败，也尝试挂载 Vue 以显示基础界面或错误状态
    app.mount('#app');
  }
}

// 启动应用
initApp();
