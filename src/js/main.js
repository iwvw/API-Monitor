/**
 * API Monitor - 主应用模块
 * 整合所有功能模块，初始化 Vue 应用
 */

// 导入样式
import '../css/styles.css';
import '../css/projects.css';
import '../css/modals.css';
import '../css/dns.css';
import '../css/tables.css';
import '../css/tabs.css';
import '../css/settings.css';
import '../css/logs.css';
import '../css/transitions.css';
import '../css/server.css';
import '../css/ssh-ide.css'; // SSH IDE 终端样式
import '../css/antigravity.css';
import '../css/gemini-cli.css';
import '../css/openai.css';
import '../css/login.css';
import '../css/sidebar-nav.css';
import '../css/zeabur.css'; // Zeabur 专属样式
import '../css/koyeb.css'; // Koyeb 专属样式
import '../css/fly.css'; // Fly.io 专属样式
import '../css/r2.css'; // R2 存储专属样式
import '../css/chat.css'; // 聊天界面样式
import '../css/template.css'; // 模块模板通用样式
import '../css/refined-ui.css'; // 精选组件样式

// 导入模板加载器
import './template-loader.js';

// Vue and FontAwesome imports
import { createApp, toRefs } from 'vue';
import '@fortawesome/fontawesome-free/css/all.min.css';

// xterm.js imports
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';

// 导入功能模块
import { authMethods } from './modules/auth.js';
import { zeaburMethods } from './modules/zeabur.js';
import { renderMarkdown } from './modules/utils.js';
import { paasMethods } from './modules/paas.js';
import { koyebMethods } from './modules/koyeb.js';
import { flyMethods } from './modules/fly.js';
import { selfHMethods } from './modules/self-h.js';
import { dnsMethods } from './modules/dns.js';
import { r2Methods } from './modules/r2.js';
import { openaiMethods } from './modules/openai.js';
import { antigravityMethods } from './modules/antigravity.js';
import { geminiCliMethods } from './modules/gemini-cli.js';
import { settingsMethods } from './modules/settings.js';
import { systemLogsMethods } from './modules/logs.js';
import { logViewerMethods } from './modules/log-viewer.js';
import { transitionsMethods } from './modules/transitions.js';
import { toast } from './modules/toast.js';
import { formatDateTime, formatFileSize, maskAddress, formatRegion } from './modules/utils.js';

// 导入全局状态
import { store } from './store.js';



// 创建并配置 Vue 应用
const app = createApp({
  setup() {
    // 将 store 的所有属性转换为 refs，这样在模板中可以直接使用且保持响应式
    return {
      ...toRefs(store)
    };
  },
  data() {
    return {
      // Zeabur 相关
      lastUpdate: '--:--:--',
      newAccount: {
        name: '',
        token: '',
        balance: ''
      },
      addingAccount: false,
      addAccountError: '',
      addAccountSuccess: '',
      expandedAccounts: {},
      refreshInterval: null,
      refreshing: false,
      lastFetchAt: 0,
      minFetchInterval: 2000,
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
      dnsTemplateForm: { name: '', type: 'A', content: '', ttl: 1, proxied: false, description: '' },
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
      workersCfAccountId: null,  // Cloudflare 账号 ID，用于生成编辑器链接
      selectedWorker: null,
      workerEditorContent: '',
      showNewWorkerModal: false,
      newWorkerName: '',
      newWorkerScript: '',

      // Pages 管理相关
      pagesProjects: [],
      pagesLoading: false,
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
        panelPassword: ''
      },
      antigravityAccountFormError: '',
      antigravityAccountFormSuccess: '',
      showAntigravityManualModal: false,
      antigravityManualForm: {
        name: '',
        accessToken: '',
        refreshToken: '',
        projectId: '',
        expiresIn: 3599
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
        load_balancing_strategy: 'random'
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
        project_id: ''
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
        description: ''
      },
      showImportServerModal: false,
      importModalSaving: false,
      importModalError: '',
      importPreview: null,
      showDockerModal: false,
      dockerModalServer: null,
      dockerModalData: null,
      showAddCredentialModal: false,
      credForm: {
        name: '',
        username: '',
        password: ''
      },
      credError: '',

      // 批量添加主机
      serverBatchText: '',
      serverBatchError: '',
      serverBatchSuccess: '',
      serverAddingBatch: false,

      // 主机筛选与自动更新
      probeStatus: '', // '', 'loading', 'success', 'error'

      // 服务器当前标签页
      serverCurrentTab: 'list',
      activeSSHSessionId: null, // 当前激活的 SSH 会话 ID
      visibleSessionIds: [],    // 分屏显示的会话 ID 列表
      sshViewLayout: 'single',  // 'single', 'split-h', 'split-v'
      sshSyncEnabled: false,    // 是否开启多屏同步操作
      draggedSessionId: null,   // 正在拖拽的会话 ID
      dropTargetId: null,       // 正在悬停的目标容器 ID
      dropHint: '',             // 拖拽位置提示 ('left', 'right', 'top', 'bottom', 'center')
      sshIdeFullscreen: false, // SSH 屏幕全屏模式
      sshWindowFullscreen: false, // SSH 窗口全屏模式
      showSSHQuickMenu: false,    // SSH 快速连接下拉菜单
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
        description: ''
      },

      // SSH 终端相关
      showSSHTerminalModal: false,
      sshTerminalServer: null,
      sshTerminal: null,
      sshTerminalFit: null,
      sshCommandHistory: [],
      sshHistoryIndex: -1,
      sshCurrentCommand: '',
      // 多终端会话管理
      sshSessions: [], // { id, server, terminal, fit, history, historyIndex }
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
        status: ''
      },
      logPage: 1,
      logPageSize: 50,

      // 历史指标相关
      metricsHistoryList: [],
      metricsHistoryLoading: false,
      metricsHistoryTotal: 0,
      metricsHistoryFilter: {
        serverId: ''
      },
      metricsHistoryPagination: {
        page: 1,
        pageSize: 50,
        totalPages: 0
      },
      metricsHistoryTimeRange: '24h', // '1h', '6h', '24h', '7d', 'all'
      metricsCollectorStatus: null,
      expandedMetricsServers: [], // 展开的主机 ID 列表
      metricsCollectInterval: 5, // 采集间隔（分钟）

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
        logFileSizeMB: 10  // 日志文件最大大小(MB)
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
      cdnProvider: typeof __CDN_PROVIDER__ !== 'undefined' ? __CDN_PROVIDER__ : 'npmmirror'
    };
  },

  // 计算属性
  computed: {
    // 获取当前激活的 SSH 会话对象
    currentSSHSession() {
      if (!this.activeSSHSessionId || this.sshSessions.length === 0) return null;
      return this.sshSessions.find(s => s.id === this.activeSSHSessionId) || null;
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
      return this.showSettingsModal ||
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
        (this.customDialog && this.customDialog.show);
    }
  },

  async mounted() {
    // 1. 核心数据与 UI 重置 (立即执行)
    window.vueApp = this;
    this.loadModuleSettings();
    this.updateBrowserThemeColor();

    // 2. 尝试从缓存恢复主机列表 (实现瞬间展示)
    if (this.mainActiveTab === 'server') {
      this.loadFromServerListCache();
    }

    // 3. 异步认证与关键数据加载
    this.checkAuth().then(() => {
      if (this.isAuthenticated) {
        // 关键业务数据
        this.loadSnippets();
        this.loadCredentials();

        // 如果当前在主机页，立即加载
        if (this.mainActiveTab === 'server') {
          this.loadServerList();
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
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible' && this.isAuthenticated) {
          if (this.mainActiveTab === 'server' && this.serverCurrentTab === 'list' && this.serverPollingEnabled) {
            this.probeAllServers();
          }
        }
      });

      console.log('[System] 非核心功能加载完成');
    }, 500);
  },

  watch: {
    mainActiveTab(newVal, oldVal) {
      // 1. [离开保护] 如果离开主机管理模块，强制将 DOM 节点搬回仓库，防止被销毁
      if (oldVal === 'server') {
        this.saveTerminalsToWarehouse();
      }

      // 2. [切回恢复] 当重新进入时，重新挂载
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
      } else {
        // 离开主机管理模块，关闭实时指标流
        this.closeMetricsStream();
      }
    },
    serverCurrentTab(newVal) {
      if (newVal === 'list' && this.mainActiveTab === 'server') {
        this.connectMetricsStream();
      } else {
        this.closeMetricsStream();
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

    opacity(newVal) {
      localStorage.setItem('card_opacity', newVal);
      this.updateOpacity();
    },

    zeaburRefreshInterval(newVal) {
      if (this.mainActiveTab === 'paas' && this.paasCurrentPlatform === 'zeabur' && !this.dataRefreshPaused) {
        this.startAutoRefresh();
      }
    },

    'monitorConfig.interval'(newVal) {
      console.log('主机刷新间隔变更为:', newVal, '秒，重启轮询');
      this.startServerPolling();
    },

    settingsCurrentTab(newVal) {
      if (newVal === 'logs') {
        // 进入日志标签页：加载日志数据和设置，并自动连接日志流
        this.fetchSystemLogs();
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
        if (newVal === 'management') {
          this.loadMonitorConfig();
          this.loadServerList();
          this.loadMonitorLogs();
        } else if (newVal === 'list') {
          // 切换回列表时重新加载
          this.loadServerList();
        } else if (newVal === 'terminal') {
          // 切换到SSH终端视图时，恢复 DOM 挂载并调整大小
          this.$nextTick(() => {
            this.syncTerminalDOM();
            const session = this.getSessionById(this.activeSSHSessionId);
            if (session) {
              // 延迟确保 DOM 渲染完成
              setTimeout(() => {
                this.safeTerminalFit(session);
                if (session.terminal) session.terminal.focus();
              }, 150);
            }
          });
        }
      },
      immediate: true
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

    mainActiveTab: {
      handler(newVal) {
        // 更新浏览器标题栏颜色
        this.updateBrowserThemeColor();

        // 通用的数据加载逻辑（需已认证）
        if (this.isAuthenticated) {
          this.$nextTick(() => {
            switch (newVal) {
              case 'paas':
                if (this.paasCurrentPlatform === 'zeabur') {
                  if (this.accounts.length === 0) {
                    this.loadFromZeaburCache();
                  }
                  if (!this.dataRefreshPaused) {
                    this.startAutoRefresh();
                  }
                } else if (this.paasCurrentPlatform === 'koyeb') {
                  // 优先加载缓存
                  if (this.koyebAccounts.length === 0) {
                    this.loadFromKoyebCache();
                  }
                  // 启动刷新
                  if (!this.koyebDataRefreshPaused) {
                    this.startKoyebAutoRefresh();
                    this.loadKoyebData(); // 立即触发一次
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
                break; case 'dns':
                if (this.dnsAccounts.length === 0) {
                  // 优先加载缓存实现即时显示
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
                  // 优先加载缓存实现即时显示
                  this.loadFromOpenaiCache();
                  this.loadOpenaiEndpoints(true);
                }
                break;
              case 'server':
                if (this.serverList.length === 0) {
                  this.loadServerList();
                }
                break;
              case 'self-h':
                this.loadOpenListAccounts();
                break;
              case 'antigravity':
                if (this.antigravityCurrentTab === 'quotas') {
                  if (this.loadAntigravityQuotas) this.loadAntigravityQuotas();
                }
                break;
              case 'gemini-cli':
                this.initGeminiCli();
                break;
            }
          });
        }

        // Antigravity 模块额度轮询管理
        if (newVal === 'antigravity' && this.antigravityCurrentTab === 'quotas') {
          // logic already handled above or needs to be specific?
          // The polling start logic is distinct from initial load.
        } else {
          if (this.stopAntigravityQuotaPolling) {
            this.stopAntigravityQuotaPolling();
          }
        }
      },
      immediate: true // 初始化时也触发
    },

    // 认证成功后加载当前标签页数据
    isAuthenticated(newVal) {
      if (newVal) {
        // 登录成功，从后端加载用户设置
        this.loadUserSettings();

        // 加载当前激活标签页的数据
        this.$nextTick(() => {
          switch (this.mainActiveTab) {
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
              this.loadServerList();
              break;
            case 'antigravity':
              if (this.antigravityCurrentTab === 'quotas') {
                if (this.loadAntigravityQuotas) this.loadAntigravityQuotas();
              }
              break;
            case 'gemini-cli':
              this.initGeminiCli();
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
        if (this.flyAccounts.length === 0) {
          this.loadFromFlyCache();
        }
        if (this.flyManagedAccounts.length === 0) {
          this.loadFlyManagedAccounts();
        }
        // 启动 Fly 自动刷新
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

    serverCurrentTab(newVal) {
      if (newVal === 'management') {
        this.loadMonitorConfig();
        this.loadCredentials();
      }
      // 切换到终端标签时，重新 fit 当前激活的终端
      if (newVal === 'terminal') {
        this.$nextTick(() => {
          const session = this.sshSessions.find(s => s.id === this.activeSSHSessionId);
          if (session) {
            setTimeout(() => this.safeTerminalFit(session), 100);
            setTimeout(() => this.safeTerminalFit(session), 300);
          }
        });
      }
    }
  },

  beforeUnmount() {
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
    ...authMethods,
    ...zeaburMethods,
    ...paasMethods,
    ...koyebMethods,
    ...flyMethods,
    ...selfHMethods,
    ...dnsMethods,
    ...r2Methods,
    ...openaiMethods,
    ...antigravityMethods,
    ...geminiCliMethods,
    ...settingsMethods,
    ...systemLogsMethods,
    ...logViewerMethods,
    ...transitionsMethods,

    /**
     * 初始化全局 Tooltip 引擎
     */
    initGlobalTooltipEngine() {
      // 避免重复初始化
      if (document.querySelector('.system-tooltip')) return;

      const tooltipEl = document.createElement('div');
      tooltipEl.className = 'system-tooltip';
      document.body.appendChild(tooltipEl);

      window.addEventListener('mouseover', (e) => {
        const trigger = e.target.closest('[data-tooltip]');
        if (trigger) {
          const text = trigger.getAttribute('data-tooltip');
          if (!text) return;

          tooltipEl.textContent = text;
          tooltipEl.classList.add('visible');

          const rect = trigger.getBoundingClientRect();
          const tooltipRect = tooltipEl.getBoundingClientRect();

          // 居中对齐触发器顶部
          let top = rect.top - tooltipRect.height - 10;
          let left = rect.left + (rect.width / 2) - (tooltipRect.width / 2);

          // 边缘检测
          if (left < 10) left = 10;
          if (left + tooltipRect.width > window.innerWidth - 10) {
            left = window.innerWidth - tooltipRect.width - 10;
          }
          if (top < 10) top = rect.bottom + 10;

          tooltipEl.style.top = `${top}px`;
          tooltipEl.style.left = `${left}px`;
        }
      });

      window.addEventListener('mouseout', (e) => {
        if (e.target.closest('[data-tooltip]')) {
          tooltipEl.classList.remove('visible');
        }
      });
    },

    /**
     * 初始化图片点击预览代理
     */
    initGlobalImageProxy() {
      window.addEventListener('click', (e) => {
        const target = e.target;
        if (target.tagName === 'IMG' && (target.classList.contains('msg-inline-image') || target.closest('.chat-history-compact'))) {
          const link = target.closest('a');
          if (link) e.preventDefault();
          this.openImagePreview(target.src);
        }
      }, true);
    },

    /**
     * 初始化全局按键监听 (Esc 等)
     */
    initGlobalKeyListeners() {
      window.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
          // 优先关闭最活跃的模态层
          if (this.showImagePreviewModal) {
            this.showImagePreviewModal = false;
          } else if (this.showSettingsModal) {
            this.showSettingsModal = false;
          } else if (this.isAnyModalOpen) {
            // 这里可以添加更详细的 Esc 逻辑，目前先关闭通用模态框
            this.showServerModal = false;
            this.showCredentialModal = false;
            this.showImportServerModal = false;
            // 更多模态框...
          }
        }
      });
    },

    /**
     * 初始化移动端标签页切换手势
     */
    initMobileGestures() {
      let touchStartX = null;
      let touchStartY = null;
      const swipeThreshold = 80;

      window.addEventListener('touchstart', (e) => {
        if (window.innerWidth > 768 || this.isAnyModalOpen) return;
        // 排除干扰容器
        if (e.target.closest('#monaco-editor-container') || e.target.closest('.log-stream-container') || e.target.closest('.table-container')) return;

        touchStartX = e.changedTouches[0].screenX;
        touchStartY = e.changedTouches[0].screenY;
      }, { passive: true });

      window.addEventListener('touchend', (e) => {
        if (window.innerWidth > 768 || touchStartX === null) return;

        const touchEndX = e.changedTouches[0].screenX;
        const touchEndY = e.changedTouches[0].screenY;
        const dx = touchEndX - touchStartX;
        const dy = touchEndY - touchStartY;

        if (Math.abs(dx) > swipeThreshold && Math.abs(dx) > Math.abs(dy) * 2) {
          const visibleModules = this.moduleOrder.filter(m => this.moduleVisibility[m]);
          const currentIndex = visibleModules.indexOf(this.mainActiveTab);
          let nextIndex = -1;

          if (dx > 0 && currentIndex > 0) nextIndex = currentIndex - 1;
          else if (dx < 0 && currentIndex < visibleModules.length - 1) nextIndex = currentIndex + 1;

          if (nextIndex !== -1) {
            this.handleTabSwitch(visibleModules[nextIndex]);
          }
        }
      }, { passive: true });
    },

    // 通用打码函数
    maskAddress,

    // Markdown 渲染
    renderMarkdown,

    // 打开图片全屏预览
    openImagePreview(url) {
      if (!url) return;
      this.previewImageUrl = url;
      this.showImagePreviewModal = true;
    },

    // 移动端设置菜单 - 打开指定设置标签页（不收起菜单）
    openSettingsTab(tabName) {
      this.settingsCurrentTab = tabName;
      this.showSettingsModal = true;
      // 不收起菜单，让用户可以继续切换
    },

    /**
     * 安全地调整终端大小
     * 只有在终端可见且已初始化渲染服务时才调用 fit()
     */
    /**
     * 安全地调整终端尺寸并通知服务器
     */
    safeTerminalFit(session) {
      if (!session || !session.fit || !session.terminal) return;

      // 防止同一帧内重复执行
      if (session._fitting) return;
      session._fitting = true;

      window.requestAnimationFrame(() => {
        session._fitting = false;
        const terminal = session.terminal;
        const fit = session.fit;

        // 如果终端尚未挂载或不可见，跳过
        if (!terminal.element || terminal.element.offsetWidth === 0 || terminal.element.offsetHeight === 0) {
          return;
        }

        try {
          const oldCols = terminal.cols;
          const oldRows = terminal.rows;

          fit.fit();

          // 仅在尺寸确实发生变化或初次渲染时刷新，且只刷新可见区域
          if (terminal.cols !== oldCols || terminal.rows !== oldRows || !session._initialFitDone) {
            session._initialFitDone = true;
            if (terminal.buffer && terminal.buffer.active) {
              terminal.refresh(0, terminal.rows - 1);
            }
          }

          // 只有当尺寸真正发生变化且 WebSocket 开启时才通知后端
          if ((terminal.cols !== oldCols || terminal.rows !== oldRows) && session.ws && session.ws.readyState === WebSocket.OPEN) {
            session.ws.send(JSON.stringify({
              type: 'resize',
              cols: terminal.cols,
              rows: terminal.rows
            }));
          }
        } catch (e) {
          if (!(e instanceof TypeError && (e.message.includes('scrollBarWidth') || e.message.includes('undefined')))) {
            console.warn('终端自适应调整失败:', e);
          }
        }
      });
    },

    async logout() {
      this.isAuthenticated = false;
      this.loginPassword = '';
      localStorage.removeItem('admin_password');
      localStorage.removeItem('password_time');

      // 重置所有模块数据
      this.accounts = [];
      this.managedAccounts = [];
      this.dnsAccounts = [];
      this.dnsZones = [];
      this.serverList = [];
      this.koyebAccounts = [];
      this.koyebManagedAccounts = [];
      this.openaiEndpoints = [];
      this.antigravityAccounts = [];
      this.geminiCliAccounts = [];
      this.flyAccounts = [];
      this.flyManagedAccounts = [];

      try {
        await fetch('/api/logout', { method: 'POST' });
      } catch (e) {
        console.warn('Logout API failed', e);
      }
      this.showGlobalToast('已退出登录', 'info');
      // 退出后显示登录框
      this.showLoginModal = true;
    },

    async copyToClipboard(text) {
      if (!text) return;
      try {
        await navigator.clipboard.writeText(text);
        this.showGlobalToast('已成功复制到剪贴板', 'success');
      } catch (err) {
        console.error('无法复制文本: ', err);
        // 回退方案
        try {
          const textArea = document.createElement("textarea");
          textArea.value = text;
          document.body.appendChild(textArea);
          textArea.select();
          document.execCommand('copy');
          document.body.removeChild(textArea);
          this.showGlobalToast('已成功复制到剪贴板', 'success');
        } catch (copyErr) {
          this.showGlobalToast('复制失败，请手动选择复制', 'error');
        }
      }
    },

    // 辅助函数
    formatDateTime(date) {
      if (!date) return '-';
      return formatDateTime(date);
    },
    formatRemainingTime(ms) {
      if (ms <= 0) return '0s';
      const seconds = Math.floor((ms / 1000) % 60);
      const minutes = Math.floor((ms / (1000 * 60)) % 60);
      const hours = Math.floor((ms / (1000 * 60 * 60)));

      let res = '';
      if (hours > 0) res += hours + 'h';
      if (minutes > 0) res += minutes + 'm';
      if (seconds > 0 || res === '') res += seconds + 's';
      return res;
    },
    formatFileSize(bytes) {
      return formatFileSize(bytes);
    },
    formatHost(host) {
      if (!host) return '';
      const mode = this.serverIpDisplayMode || 'normal';

      if (mode === 'normal') return host;
      if (mode === 'hidden') return '****';

      if (mode === 'masked') {
        // 打码模式 (masked): 1.2.3.4 -> 1.2.*.*
        const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/;
        if (ipv4Regex.test(host)) {
          const parts = host.split('.');
          return `${parts[0]}.${parts[1]}.*.* `;
        }

        // 域名或其他: example.com -> ex****.com
        const parts = host.split('.');
        if (parts.length >= 2) {
          const main = parts[0];
          const tld = parts[parts.length - 1];
          if (main.length > 2) {
            return main.substring(0, 2) + '****.' + tld;
          }
        }
        return host.length > 4 ? host.substring(0, 2) + '****' : '****';
      }

      return host;
    },
    getModuleName(id) {
      const names = {
        'openai': 'OpenAI API',
        'antigravity': 'Antigravity',
        'gemini-cli': 'Gemini CLI',
        'paas': 'PaaS',
        'dns': 'DNS 管理',
        'server': '主机管理'
      };
      return names[id] || id;
    },

    // Toast 管理系统 - 使用新的独立 Toast 管理器
    showGlobalToast(message, type = 'success', duration = 3000, isManual = false) {
      // 使用新的toast系统
      toast[type](message, { duration, isManual });
    },

    // DNS Toast (使用新系统)
    showDnsToast(message, type = 'success') {
      toast[type](message);
    },

    // OpenAI Toast (使用新系统)
    showOpenaiToast(message, type = 'success') {
      toast[type](message);
    },

    /**
     * 聚焦到模态框遮罩层，使键盘快捷键（ESC、Enter）生效
     * @param {string} selector - 可选的 CSS 选择器，默认为 '.modal-overlay'
     */
    focusModalOverlay(selector = '.modal-overlay') {
      const overlay = document.querySelector(selector);
      if (overlay) {
        overlay.focus();
      }
    },

    showAlert(message, title = '提示', icon = 'fa-info-circle') {
      return new Promise((resolve) => {
        this.customDialog = {
          show: true,
          title: title,
          message: message,
          icon: icon,
          confirmText: '确定',
          cancelText: '',
          confirmClass: 'btn-primary',
          onConfirm: () => {
            this.customDialog.show = false;
            resolve(true);
          },
          onCancel: null
        };
      });
    },

    showConfirm(options) {
      return new Promise((resolve) => {
        this.customDialog = {
          show: true,
          title: options.title || '确认',
          message: options.message || '',
          icon: options.icon || 'fa-question-circle',
          confirmText: options.confirmText || '确定',
          cancelText: options.cancelText || '取消',
          confirmClass: options.confirmClass || 'btn-primary',
          onConfirm: () => {
            this.customDialog.show = false;
            resolve(true);
          },
          onCancel: () => {
            this.customDialog.show = false;
            resolve(false);
          }
        };
      });
    },

    showPrompt(options) {
      return new Promise((resolve) => {
        this.customDialog = {
          show: true,
          title: options.title || '输入',
          message: options.message || '',
          icon: options.icon || 'fa-edit',
          confirmText: options.confirmText || '确定',
          cancelText: options.cancelText || '取消',
          confirmClass: options.confirmClass || 'btn-primary',
          isPrompt: true,
          promptValue: '',
          placeholder: options.placeholder || '',
          onConfirm: () => {
            const value = this.customDialog.promptValue;
            this.customDialog.show = false;
            resolve(value);
          },
          onCancel: () => {
            this.customDialog.show = false;
            resolve(null);
          }
        };
      });
    },

    maskEmail(email) {
      if (!email || !email.includes('@')) return email;
      const [local, domain] = email.split('@');
      if (local.length <= 14) return email;
      const masked = local.substring(0, 2) + '*'.repeat(local.length - 4) + local.substring(local.length - 2);
      return masked + '@' + domain;
    },

    /**
     * 动态更新浏览器标题栏/主题颜色 (PWA/移动端适配)
     * @param {string} tab - 当前激活的标签页 ID
     */
    updateBrowserThemeColor() {
      // 延迟一小段时间以确保 DOM 更新和 CSS 变量生效
      this.$nextTick(() => {
        // 获取页面背景色 (--bg-primary)
        const bgColor = getComputedStyle(document.documentElement).getPropertyValue('--bg-primary').trim();

        // 如果能获取到颜色（HEX 或 RGB），则应用它
        if (bgColor) {
          this._setMetaThemeColor(bgColor);
        } else {
          // 回退逻辑：使用默认浅色
          this._setMetaThemeColor('#f4f6f8');
        }
      });
    },

    /**
     * 设置 meta 标签的颜色值
     * @param {string} color - 十六进制颜色值
     */
    _setMetaThemeColor(color) {
      let metaThemeColor = document.querySelector('meta[name="theme-color"]');
      if (!metaThemeColor) {
        metaThemeColor = document.createElement('meta');
        metaThemeColor.setAttribute('name', 'theme-color');
        document.head.appendChild(metaThemeColor);
      }
      metaThemeColor.setAttribute('content', color);
    },

    // ==================== 主机管理方法 ====================
    async openAddServerModal() {
      this.serverModalMode = 'add';

      // 重置表单
      this.serverForm = {
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
        description: ''
      };

      // 确保凭据列表已加载
      if (this.serverCredentials.length === 0) {
        await this.loadCredentials();
      }

      // 自动应用默认凭据
      const defaultCred = this.serverCredentials.find(c => c.is_default);
      if (defaultCred) {
        this.serverForm.username = defaultCred.username || '';
        this.serverForm.password = defaultCred.password || '';
        this.serverForm.authType = defaultCred.auth_type === 'key' ? 'privateKey' : 'password';
        if (defaultCred.private_key) {
          this.serverForm.privateKey = defaultCred.private_key || '';
          this.serverForm.passphrase = defaultCred.passphrase || '';
        }
        console.log('[Server] 已应用默认凭据:', defaultCred.name);
      }

      this.serverModalError = '';
      this.showServerModal = true;
    },

    async openEditServerModal(serverId) {
      this.serverModalMode = 'edit';
      this.serverModalError = '';

      try {
        const response = await fetch('/api/server/accounts');
        const data = await response.json();

        if (data.success) {
          const server = data.data.find(s => s.id === serverId);
          if (server) {
            this.serverForm = {
              id: server.id,
              name: server.name,
              host: server.host,
              port: server.port,
              username: server.username,
              authType: server.auth_type === 'key' ? 'privateKey' : (server.auth_type || 'password'),
              password: '', // 不显示原密码
              privateKey: '', // 不显示原私钥
              passphrase: '',
              tagsInput: Array.isArray(server.tags) ? server.tags.join(',') : '',
              description: server.description || ''
            };
            this.showServerModal = true;
          } else {
            this.showGlobalToast('主机不存在', 'error');
          }
        } else {
          this.showGlobalToast('加载主机信息失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('加载主机信息失败:', error);
        this.showGlobalToast('加载主机信息失败', 'error');
      }
    },

    closeServerModal() {
      this.showServerModal = false;
      this.serverModalError = '';
    },

    async testServerConnection() {
      this.serverModalError = '';

      // 验证必填字段
      if (!this.serverForm.name || !this.serverForm.host || !this.serverForm.username) {
        this.serverModalError = '请填写所有必填字段';
        return;
      }

      if (this.serverForm.authType === 'password' && !this.serverForm.password) {
        this.serverModalError = '请输入密码';
        return;
      }

      if (this.serverForm.authType === 'privateKey' && !this.serverForm.privateKey) {
        this.serverModalError = '请输入私钥';
        return;
      }

      this.serverModalSaving = true;
      this.showGlobalToast('正在测试连接...', 'info');

      try {
        const tags = this.serverForm.tagsInput
          ? this.serverForm.tagsInput.split(',').map(t => t.trim()).filter(t => t)
          : [];

        const response = await fetch('/api/server/test-connection', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            host: this.serverForm.host,
            port: this.serverForm.port,
            username: this.serverForm.username,
            auth_type: this.serverForm.authType === 'privateKey' ? 'key' : this.serverForm.authType,
            password: this.serverForm.password,
            private_key: this.serverForm.privateKey,
            passphrase: this.serverForm.passphrase
          })
        });

        const data = await response.json();

        if (data.success) {
          this.showGlobalToast('连接测试成功！', 'success', 3000, true);
        } else {
          this.serverModalError = '连接测试失败: ' + data.message;
          this.showGlobalToast('连接测试失败', 'error', 3000, true);
        }
      } catch (error) {
        console.error('测试连接失败:', error);
        this.serverModalError = '测试连接失败: ' + error.message;
        this.showGlobalToast('测试连接失败', 'error', 3000, true);
      } finally {
        this.serverModalSaving = false;
      }
    },

    async saveServer() {
      this.serverModalError = '';

      // 验证必填字段
      if (!this.serverForm.name || !this.serverForm.host || !this.serverForm.username) {
        this.serverModalError = '请填写所有必填字段';
        return;
      }

      if (this.serverForm.authType === 'password' && !this.serverForm.password && this.serverModalMode === 'add') {
        this.serverModalError = '请输入密码';
        return;
      }

      if (this.serverForm.authType === 'privateKey' && !this.serverForm.privateKey && this.serverModalMode === 'add') {
        this.serverModalError = '请输入私钥';
        return;
      }

      this.serverModalSaving = true;

      try {
        const tags = this.serverForm.tagsInput
          ? this.serverForm.tagsInput.split(',').map(t => t.trim()).filter(t => t)
          : [];

        const payload = {
          name: this.serverForm.name,
          host: this.serverForm.host,
          port: this.serverForm.port,
          username: this.serverForm.username,
          auth_type: this.serverForm.authType === 'privateKey' ? 'key' : this.serverForm.authType,
          tags: tags,
          description: this.serverForm.description
        };

        // 只在有值时才发送密码/私钥
        if (this.serverForm.authType === 'password' && this.serverForm.password) {
          payload.password = this.serverForm.password;
        }
        if (this.serverForm.authType === 'privateKey' && this.serverForm.privateKey) {
          payload.private_key = this.serverForm.privateKey;
          if (this.serverForm.passphrase) {
            payload.passphrase = this.serverForm.passphrase;
          }
        }

        const url = this.serverModalMode === 'add'
          ? '/api/server/accounts'
          : `/ api / server / accounts / ${this.serverForm.id}`;

        const method = this.serverModalMode === 'add' ? 'POST' : 'PUT';

        const response = await fetch(url, {
          method: method,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });

        const data = await response.json();

        if (data.success) {
          this.showGlobalToast(
            this.serverModalMode === 'add' ? '主机添加成功' : '主机更新成功',
            'success'
          );
          this.closeServerModal();

          // 刷新主机列表
          await this.loadServerList();

          // 如果是添加模式，立即刷新新主机的详细信息
          if (this.serverModalMode === 'add' && data.data && data.data.id) {
            this.refreshServerInfo(data.data.id);
          }
        } else {
          this.serverModalError = data.error || '保存失败';
          this.showGlobalToast('保存失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('保存主机失败:', error);
        this.serverModalError = '保存失败: ' + error.message;
        this.showGlobalToast('保存主机失败', 'error');
      } finally {
        this.serverModalSaving = false;
      }
    },

    openImportServerModal() {
      this.importPreview = null;
      this.importModalError = '';
      this.showImportServerModal = true;
    },

    closeImportServerModal() {
      this.showImportServerModal = false;
      this.importModalError = '';
      this.importPreview = null;
      if (this.$refs.importFileInput) {
        this.$refs.importFileInput.value = '';
      }
    },

    handleImportFileChange(event) {
      const file = event.target.files[0];
      if (!file) return;

      const reader = new FileReader();
      reader.onload = (e) => {
        try {
          const data = JSON.parse(e.target.result);

          if (!Array.isArray(data)) {
            this.importModalError = '文件格式错误：应为主机数组';
            return;
          }

          // 验证数据格式
          const validServers = data.filter(server => {
            return server.name && server.host && server.username;
          });

          if (validServers.length === 0) {
            this.importModalError = '文件中没有有效的主机配置';
            return;
          }

          this.importPreview = validServers;
          this.importModalError = '';
        } catch (error) {
          this.importModalError = '文件解析失败：' + error.message;
        }
      };
      reader.readAsText(file);
    },

    async confirmImportServers() {
      if (!this.importPreview || this.importPreview.length === 0) {
        return;
      }

      this.importModalSaving = true;
      this.importModalError = '';

      try {
        const response = await fetch('/api/server/accounts/import', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ servers: this.importPreview })
        });

        const data = await response.json();

        if (data.success) {
          this.showGlobalToast(`成功导入 ${data.imported || data.results?.filter(r => r.success).length || 0} 台主机`, 'success');
          this.closeImportServerModal();

          // 刷新主机列表
          this.loadServerList();
        } else {
          this.importModalError = '导入失败: ' + data.error;
          this.showGlobalToast('导入失败', 'error');
        }
      } catch (error) {
        console.error('导入主机失败:', error);
        this.importModalError = '导入失败: ' + error.message;
        this.showGlobalToast('导入主机失败', 'error');
      } finally {
        this.importModalSaving = false;
      }
    },

    /**
     * 批量添加主机 (文本方式)
     */
    async batchAddServers() {
      this.serverBatchError = '';
      this.serverBatchSuccess = '';

      if (!this.serverBatchText.trim()) {
        this.serverBatchError = '请输入主机信息';
        return;
      }

      const lines = this.serverBatchText.split('\n');
      const servers = [];
      let parseErrors = [];

      for (let i = 0; i < lines.length; i++) {
        const line = lines[i].trim();
        if (!line) continue;

        try {
          // 尝试解析 JSON
          if (line.startsWith('{')) {
            const server = JSON.parse(line);
            if (server.name && server.host) {
              // 确保必要字段存在
              server.port = server.port || 22;
              server.auth_type = server.auth_type || 'password';
              servers.push(server);
            } else {
              parseErrors.push(`第 ${i + 1} 行: 缺少必要字段(name, host)`);
            }
          } else {
            // 解析 CSV: name, host, port, username, password
            // 支持逗号或竖线分隔
            const parts = line.split(/[|,，]/).map(p => p.trim());

            if (parts.length >= 2) {
              const server = {
                name: parts[0],
                host: parts[1],
                port: parseInt(parts[2]) || 22,
                username: parts[3] || 'root',
                auth_type: 'password',
                password: parts[4] || ''
              };

              if (!server.name || !server.host) {
                parseErrors.push(`第 ${i + 1} 行: 格式错误，缺少名称或IP`);
                continue;
              }

              servers.push(server);
            } else {
              parseErrors.push(`第 ${i + 1} 行: 格式错误，请检查分隔符`);
            }
          }
        } catch (e) {
          parseErrors.push(`第 ${i + 1} 行: 解析失败(${e.message})`);
        }
      }

      if (servers.length === 0) {
        this.serverBatchError = '没有识别到有效的主机信息。\n' + (parseErrors.length > 0 ? '错误示例:\n' + parseErrors.slice(0, 3).join('\n') : '');
        return;
      }

      this.serverAddingBatch = true;

      try {
        const response = await fetch('/api/server/accounts/import', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ servers })
        });

        const data = await response.json();

        if (data.success) {
          const successCount = data.results ? data.results.filter(r => r.success).length : 0;
          const failCount = data.results ? data.results.filter(r => !r.success).length : 0;

          let msg = `批量添加完成: 成功 ${successCount} 台`;
          if (failCount > 0) msg += `, 失败 ${failCount} 台`;

          this.serverBatchSuccess = msg;
          this.showGlobalToast(msg, failCount > 0 ? 'warning' : 'success');

          if (successCount > 0) {
            this.serverBatchText = ''; // 清空输入
            await this.loadServerList();

            // 立即刷新所有新添加的主机信息
            if (data.results) {
              const newServerIds = data.results
                .filter(r => r.success && r.data && r.data.id)
                .map(r => r.data.id);

              for (const id of newServerIds) {
                this.refreshServerInfo(id);
              }
            }
          }
        } else {
          this.serverBatchError = '添加失败: ' + data.error;
        }
      } catch (error) {
        console.error('批量添加失败:', error);
        this.serverBatchError = '请求失败: ' + error.message;
      } finally {
        this.serverAddingBatch = false;
      }
    },

    /**
     * 检测 Docker 容器镜像更新
     */
    async checkContainerUpdate(server, container) {
      if (container.checkingUpdate) return;

      // 使用 Vue.set 确保响应式 (Vue 2 风格) 或直接赋值
      // container 对象是 dockerModalData.containers 数组的一部分，应该是响应式的
      container.checkingUpdate = true;
      // 强制更新视图，防止深层对象未响应
      this.$forceUpdate();

      try {
        const response = await fetch('/api/server/docker/check-update', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            serverId: server.id,
            imageName: container.image
          })
        });

        const data = await response.json();

        if (data.success) {
          // 更新容器状态
          container.updateAvailable = data.data.updateAvailable;

          if (data.data.updateAvailable) {
            this.showGlobalToast(`容器 ${container.name} 有新版本可用`, 'success');
          } else {
            this.showGlobalToast(`容器 ${container.name} 已是最新`, 'info');
          }
        } else {
          this.showGlobalToast('检测失败: ' + (data.error || data.message), 'error');
        }
      } catch (error) {
        console.error('检测更新失败:', error);
        this.showGlobalToast('检测请求失败', 'error');
      } finally {
        container.checkingUpdate = false;
        this.$forceUpdate();
      }
    },

    showDockerContainersModal(server, dockerData) {
      this.dockerModalServer = server;
      this.dockerModalData = dockerData;
      this.showDockerModal = true;
    },

    closeDockerModal() {
      this.showDockerModal = false;
      this.dockerModalServer = null;
      this.dockerModalData = null;
    },

    /**
     * 打开 SSH 终端(切换到 IDE 视图)
     */
    openSSHTerminal(server) {
      if (!server) return;

      // 检查是否已经打开了该主机的终端
      const existingSession = this.sshSessions.find(s => s.server.id === server.id);
      if (existingSession) {
        this.switchToSSHTab(existingSession.id);
        return;
      }

      const sessionId = 'session_' + Date.now();
      const session = {
        id: sessionId,
        server: server,
        terminal: null,
        fit: null,
        ws: null,
        connected: false
      };

      this.sshSessions.push(session);
      this.activeSSHSessionId = sessionId;
      this.serverCurrentTab = 'terminal';

      this.$nextTick(() => {
        this.initSessionTerminal(sessionId);
        // 延迟同步 DOM 确保 Vue 渲染完成
        setTimeout(() => this.syncTerminalDOM(), 50);
        setTimeout(() => this.syncTerminalDOM(), 200);
      });
    },

    /**
     * 切换当前激活的 SSH 会话
     */
    switchToSSHTab(sessionId) {
      this.serverCurrentTab = 'terminal';
      this.activeSSHSessionId = sessionId;

      // 如果目标会话不在当前分屏中，自动退出分屏返回单屏模式
      if (this.sshViewLayout !== 'single' && !this.visibleSessionIds.includes(sessionId)) {
        this.resetToSingleLayout();
      }

      this.$nextTick(() => {
        this.syncTerminalDOM(); // 同步 DOM 节点位置
        this.fitAllVisibleSessions();
        const session = this.getSessionById(sessionId);
        if (session && session.terminal) session.terminal.focus();
      });
    },

    /**
     * 关闭SSH会话
     */
    closeSSHSession(sessionId) {
      const index = this.sshSessions.findIndex(s => s.id === sessionId);
      if (index === -1) return;

      const session = this.sshSessions[index];

      // 清除心跳定时器
      if (session.heartbeatInterval) {
        clearInterval(session.heartbeatInterval);
        session.heartbeatInterval = null;
      }

      // 关闭 WebSocket 连接
      if (session.ws) {
        if (session.ws.readyState === WebSocket.OPEN) {
          session.ws.send(JSON.stringify({ type: 'disconnect' }));
        }
        session.ws.close();
      }

      // 移除 resize 监听器
      if (session.resizeHandler) {
        window.removeEventListener('resize', session.resizeHandler);
      }

      // 清理 ResizeObserver
      if (session.resizeObserver) {
        session.resizeObserver.disconnect();
      }

      // 销毁终端实例
      if (session.terminal) {
        session.terminal.dispose();
      }

      // 核心修复：从全局仓库中彻底删除该节点的 DOM 元素
      const terminalEl = document.getElementById('ssh-terminal-' + sessionId);
      if (terminalEl) {
        terminalEl.remove();
      }

      // 从数组中移除
      this.sshSessions.splice(index, 1);

      // 如果关闭的是当前激活的会话，切换到其他会话
      if (this.activeSSHSessionId === sessionId) {
        if (this.sshSessions.length > 0) {
          // 切换到下一个可用的会话（优先选择列表中的最后一个）
          const nextSession = this.sshSessions[this.sshSessions.length - 1];
          this.switchToSSHTab(nextSession.id);
        } else {
          // 如果没有会话了，清空激活ID并返回主机列表
          this.activeSSHSessionId = null;
          this.serverCurrentTab = 'list';
        }
      }
    },

    /**
     * 重新连接SSH会话
     */
    reconnectSSHSession(sessionId) {
      const session = this.sshSessions.find(s => s.id === sessionId);
      if (!session) return;

      console.log(`[SSH ${sessionId}] 开始重新连接...`);

      // 清除心跳定时器
      if (session.heartbeatInterval) {
        clearInterval(session.heartbeatInterval);
        session.heartbeatInterval = null;
      }

      // 如果已连接，先断开
      if (session.ws) {
        if (session.ws.readyState === WebSocket.OPEN) {
          session.ws.send(JSON.stringify({ type: 'disconnect' }));
        }
        session.ws.close();
        session.ws = null;
      }

      // 清空终端并显示重连信息
      if (session.terminal) {
        session.terminal.clear();
        session.terminal.writeln(`\x1b[1; 33m正在重新连接到 ${session.server.name} (${this.formatHost(session.server.host)})...\x1b[0m`);
      }

      // 建立新的 WebSocket 连接
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const ws = new WebSocket(`${protocol}//${window.location.host}/ws/ssh`);
      session.ws = ws;

      ws.onopen = () => {
        console.log(`[SSH ${sessionId}] WebSocket 已重新连接`);
        ws.send(JSON.stringify({
          type: 'connect',
          serverId: session.server.id,
          cols: session.terminal.cols,
          rows: session.terminal.rows
        }));

        // 启动心跳保活
        session.heartbeatInterval = setInterval(() => {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: 'ping' }));
          }
        }, 30000);
      };

      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data);
          switch (msg.type) {
            case 'connected':
              session.connected = true;
              session.terminal.writeln(`\x1b[1;32m${msg.message}\x1b[0m`);
              session.terminal.writeln('');
              break;
            case 'output':
              session.terminal.write(msg.data);
              break;
            case 'error':
              session.terminal.writeln(`\x1b[1;31m错误: ${msg.message}\x1b[0m`);
              break;
            case 'disconnected':
              session.connected = false;
              session.terminal.writeln('');
              session.terminal.writeln(`\x1b[1;33m${msg.message}\x1b[0m`);
              break;
          }
        } catch (e) {
          console.error('解析消息失败:', e);
        }
      };

      ws.onerror = () => {
        session.terminal.writeln(`\x1b[1;31mWebSocket 连接错误\x1b[0m`);
      };

      ws.onclose = () => {
        console.log(`[SSH ${sessionId}] WebSocket 已关闭`);

        // 清除心跳定时器
        if (session.heartbeatInterval) {
          clearInterval(session.heartbeatInterval);
          session.heartbeatInterval = null;
        }

        if (session.connected) {
          session.terminal.writeln('');
          session.terminal.writeln(`\x1b[1;33m连接已断开。点击"重新连接"按钮恢复连接。\x1b[0m`);
        }
        session.connected = false;
      };
    },

    // ==================== SSH 分屏拖拽逻辑 ====================

    handleTabDragStart(sessionId) {
      this.draggedSessionId = sessionId;
      this.dropHint = '';
      this.dropTargetId = null;

      // 增强某些浏览器的兼容性
      if (event && event.dataTransfer) {
        event.dataTransfer.effectAllowed = 'move';
        event.dataTransfer.setData('text/plain', sessionId);
      }
    },

    handleTabDragEnd() {
      this.draggedSessionId = null;
      this.dropHint = '';
      this.dropTargetId = null;
    },

    setDropHint(pos, targetId = null) {
      this.dropHint = pos;
      this.dropTargetId = targetId;
    },

    clearDropHint() {
      this.dropHint = '';
      this.dropTargetId = null;
    },

    handleTerminalDragOver(e) {
      e.preventDefault();
    },

    handleTerminalDrop(targetId = null, position = 'center') {
      const effectivePosition = position || this.dropHint || 'center';
      if (!this.draggedSessionId) {
        this.handleTabDragEnd();
        return;
      }

      const draggedId = this.draggedSessionId;
      const isAlreadyVisible = this.visibleSessionIds.includes(draggedId);

      // --- 1. 重复性检查 (仅针对从标签栏新拖入的情况) ---
      if (!isAlreadyVisible) {
        const draggedSession = this.getSessionById(draggedId);
        if (draggedSession) {
          // 检查该服务器是否已经有其他会话在显示了
          const isServerShown = this.visibleSessionIds.some(id => {
            if (id === targetId && effectivePosition === 'center') return false; // 允许替换
            const s = this.getSessionById(id);
            return s && s.server.id === draggedSession.server.id;
          });

          // 如果是单屏模式切分屏，检查 active 会话
          const activeSession = this.getSessionById(this.activeSSHSessionId);
          const isActiveSameServer = this.sshViewLayout === 'single' &&
            activeSession &&
            activeSession.server.id === draggedSession.server.id;

          if (isServerShown || (isActiveSameServer && effectivePosition !== 'center')) {
            toast.info('该服务器已在分屏显示中');
            this.handleTabDragEnd();
            return;
          }
        }
      }

      // --- 2. 布局逻辑处理 ---
      if (this.sshViewLayout === 'single') {
        if (effectivePosition === 'center') {
          this.activeSSHSessionId = draggedId;
        } else {
          // 单屏切分屏
          this.visibleSessionIds = (effectivePosition === 'left' || effectivePosition === 'top')
            ? [draggedId, this.activeSSHSessionId]
            : [this.activeSSHSessionId, draggedId];
          this.sshViewLayout = (effectivePosition === 'left' || effectivePosition === 'right') ? 'split-h' : 'split-v';
          this.activeSSHSessionId = draggedId;
        }
      } else {
        // 分屏模式下的 移动/替换/交换
        const draggedIndex = this.visibleSessionIds.indexOf(draggedId);
        const targetIndex = this.visibleSessionIds.indexOf(targetId);

        if (effectivePosition === 'center') {
          // 替换或交换
          if (targetIndex !== -1) {
            if (isAlreadyVisible && draggedIndex !== -1) {
              // 交换位置 (Swap)
              const newVisibleIds = [...this.visibleSessionIds];
              [newVisibleIds[draggedIndex], newVisibleIds[targetIndex]] = [newVisibleIds[targetIndex], newVisibleIds[draggedIndex]];
              this.visibleSessionIds = newVisibleIds;
            } else {
              // 外部替换
              this.visibleSessionIds.splice(targetIndex, 1, draggedId);
            }
          }
        } else {
          // 拆分或重新排序 (Rearrange)
          let newVisibleIds = this.visibleSessionIds.filter(id => id !== draggedId);
          let targetIdx = newVisibleIds.indexOf(targetId);

          if (targetIdx !== -1) {
            let insertAt = targetIdx;

            // 核心修复：针对 2 列 Grid 布局计算索引
            // 在 Grid 中，索引 0|1 是第一行，2|3 是第二行
            if (effectivePosition === 'right' || effectivePosition === 'bottom') {
              insertAt = targetIdx + 1;
            }

            // 特殊处理：如果当前是 2 屏左右(H) 且 向下拆分左侧窗口(0)
            // 我们希望结果是：[0, 1] 变成 [0, 1, new]，在网格中 new 就会出现在 0 的下方
            if (this.sshViewLayout === 'split-h' && effectivePosition === 'bottom' && targetIdx === 0) {
              insertAt = 2;
            }

            newVisibleIds.splice(insertAt, 0, draggedId);
          } else {
            // 边缘放置
            newVisibleIds.push(draggedId);
          }

          this.visibleSessionIds = newVisibleIds;

          // 智能布局切换
          if (this.visibleSessionIds.length === 2) {
            if (effectivePosition === 'left' || effectivePosition === 'right') {
              this.sshViewLayout = 'split-h';
            } else {
              this.sshViewLayout = 'split-v';
            }
          } else if (this.visibleSessionIds.length === 3) {
            // 核心修复：根据当前布局趋势决定 3 屏方向
            // 如果已经在左右分屏，向下拆分应保持左右结构 (Master-Stack)
            if (this.sshViewLayout === 'split-h') {
              this.sshViewLayout = 'grid';
            } else if (this.sshViewLayout === 'split-v') {
              this.sshViewLayout = 'grid-v';
            } else {
              // 兜底逻辑
              this.sshViewLayout = (effectivePosition === 'top' || effectivePosition === 'bottom') ? 'grid-v' : 'grid';
            }
          } else if (this.visibleSessionIds.length > 3) {
            this.sshViewLayout = 'grid';
          }
        }
        this.activeSSHSessionId = draggedId;
      }

      this.handleTabDragEnd();

      // --- 4. 同步与适配 ---
      this.$nextTick(() => {
        this.syncTerminalDOM();

        // 针对复杂的 3 屏/4 屏布局，二次同步确保万无一失
        setTimeout(() => this.syncTerminalDOM(), 100);

        this.fitAllVisibleSessions();
      });
    },

    closeSplitView(sessionId) {
      this.visibleSessionIds = this.visibleSessionIds.filter(id => id !== sessionId);

      // 自适应：如果只剩一个会话，或没有会话了，自动恢复到 single 模式
      if (this.visibleSessionIds.length <= 1) {
        this.resetToSingleLayout();
      } else {
        // 如果还剩多个，更新网格计数变量并重新 Fit
        this.$nextTick(() => {
          this.syncTerminalDOM(); // 同步 DOM 节点位置
          this.fitAllVisibleSessions();
        });
      }
    },

    getSessionById(id) {
      return this.sshSessions.find(s => s.id === id);
    },

    resetToSingleLayout() {
      // 1. [核心修复] 在销毁分屏 Slot 之前，抢先将所有终端节点撤回全局仓库保护
      this.saveTerminalsToWarehouse();

      this.sshViewLayout = 'single';
      this.visibleSessionIds = [];

      this.$nextTick(() => {
        this.syncTerminalDOM(); // 2. 重新挂载到单屏 Slot
        this.fitAllVisibleSessions();

        // 3. 二次补偿同步
        setTimeout(() => {
          this.syncTerminalDOM();
          this.fitAllVisibleSessions();
        }, 150);
      });
    },

    /**
     * 同步终端 DOM 节点，将其实际挂载点移动到当前布局的槽位中
     */
    syncTerminalDOM() {
      const isTerminalTab = this.serverCurrentTab === 'terminal';
      const idsToShow = (this.mainActiveTab === 'server' && isTerminalTab)
        ? (this.sshViewLayout === 'single' ? (this.activeSSHSessionId ? [this.activeSSHSessionId] : []) : this.visibleSessionIds)
        : [];

      idsToShow.forEach(id => {
        if (!id) return;
        const slot = document.getElementById('ssh-slot-' + id);
        const terminalEl = document.getElementById('ssh-terminal-' + id);
        const session = this.getSessionById(id);

        if (slot && terminalEl && session && session.terminal) {
          if (terminalEl.parentElement !== slot) {
            // 将终端节点移动到可见的槽位中
            slot.appendChild(terminalEl);

            this.$nextTick(() => {
              this.safeTerminalFit(session);
              if (id === this.activeSSHSessionId) {
                setTimeout(() => session.terminal.focus(), 50);
              }
            });
          }
        }
      });

      // 将其余终端放回仓库保活
      const warehouse = document.getElementById('ssh-terminal-warehouse');
      if (warehouse) {
        this.sshSessions.forEach(session => {
          if (!idsToShow.includes(session.id)) {
            const terminalEl = document.getElementById('ssh-terminal-' + session.id);
            if (terminalEl && terminalEl.parentElement !== warehouse) {
              warehouse.appendChild(terminalEl);
            }
          }
        });
      }
    },

    /**
     * 强制将所有终端节点撤回仓库保活
     */
    saveTerminalsToWarehouse() {
      const warehouse = document.getElementById('ssh-terminal-warehouse');
      if (!warehouse) return;
      this.sshSessions.forEach(session => {
        const el = document.getElementById('ssh-terminal-' + session.id);
        if (el && el.parentElement !== warehouse) {
          warehouse.appendChild(el);
        }
      });
    },

    /**
     * 初始化监听器，自动发现新 Slot 并挂载终端
     */
    initSshMountObserver() {
      if (this.sshMountObserver) this.sshMountObserver.disconnect();

      const observer = new MutationObserver((mutations) => {
        // 只有当有子节点变化时才尝试同步
        const hasRelevantChange = mutations.some(m => m.type === 'childList');
        if (hasRelevantChange && this.mainActiveTab === 'server' && this.serverCurrentTab === 'terminal') {
          this.syncTerminalDOM();
        }
      });

      // 缩小监听范围到具体的主机管理容器，而不是 body
      const container = document.getElementById('server-list-container');
      if (container) {
        observer.observe(container, { childList: true, subtree: true });
      } else {
        // Fallback
        observer.observe(document.body, { childList: true, subtree: true });
      }
      this.sshMountObserver = observer;
    },

    /**
     * 对所有当前可见的终端执行 Fit 序列，解决布局切换时的尺寸计算错位
     */
    fitAllVisibleSessions() {
      const ids = this.sshViewLayout === 'single'
        ? (this.activeSSHSessionId ? [this.activeSSHSessionId] : [])
        : this.visibleSessionIds;

      const runFit = () => {
        ids.forEach(id => {
          const session = this.getSessionById(id);
          if (session) this.safeTerminalFit(session);
        });
      };

      // 仅执行少量必要序列，配合 safeTerminalFit 内部的 rAF
      runFit();
      setTimeout(runFit, 150);
    },

    /**
     * 重新调整当前终端尺寸
     */
    fitCurrentSSHSession() {
      const session = this.sshSessions.find(s => s.id === this.activeSSHSessionId);
      if (session) {
        this.safeTerminalFit(session);
      }
    },

    /**
     * 切换 SSH 终端全屏模式 (使用浏览器原生全屏 API)
     */
    async toggleSSHTerminalFullscreen() {
      const sshLayout = document.querySelector('.ssh-ide-layout');
      if (!sshLayout) return;

      try {
        if (!document.fullscreenElement) {
          if (sshLayout.requestFullscreen) {
            await sshLayout.requestFullscreen();
          } else if (sshLayout.webkitRequestFullscreen) {
            await sshLayout.webkitRequestFullscreen();
          } else if (sshLayout.msRequestFullscreen) {
            await sshLayout.msRequestFullscreen();
          }
        } else {
          if (document.exitFullscreen) {
            await document.exitFullscreen();
          } else if (document.webkitExitFullscreen) {
            await document.webkitExitFullscreen();
          } else if (document.msExitFullscreen) {
            await document.msExitFullscreen();
          }
        }
      } catch (err) {
        console.error('全屏操作失败:', err);
        // 容错处理：即使 API 失败也尝试切换样式类
        this.sshIdeFullscreen = !this.sshIdeFullscreen;
        setTimeout(() => this.fitCurrentSSHSession(), 300);
      }

      // 统一监听全屏状态变化，不仅处理本方法触发的，也处理 Esc 键退出的情况
      if (!window._sshFullscreenListenerBound) {
        const onFullscreenChange = () => {
          this.sshIdeFullscreen = !!document.fullscreenElement;
          // 连续触发多次 Fit，应对不同浏览器动画时长差异，彻底解决错位 bug
          const fitSequence = [50, 150, 300, 600, 1000];
          fitSequence.forEach(delay => {
            setTimeout(() => this.fitCurrentSSHSession(), delay);
          });
        };
        document.addEventListener('fullscreenchange', onFullscreenChange);
        document.addEventListener('webkitfullscreenchange', onFullscreenChange);
        window._sshFullscreenListenerBound = true;
      }
    },

    /**
     * 切换 SSH 窗口全屏模式 (使用浏览器 Fullscreen API)
     */
    async toggleSSHWindowFullscreen() {
      const sshLayout = document.querySelector('.ssh-ide-layout');
      if (!sshLayout) return;

      try {
        if (!document.fullscreenElement) {
          await sshLayout.requestFullscreen();
          this.sshWindowFullscreen = true;
        } else {
          await document.exitFullscreen();
          this.sshWindowFullscreen = false;
        }
      } catch (err) {
        console.error('窗口全屏切换失败:', err);
      }

      // 监听全屏变化事件
      document.addEventListener('fullscreenchange', () => {
        this.sshWindowFullscreen = !!document.fullscreenElement;
        setTimeout(() => this.fitCurrentSSHSession(), 100);
        setTimeout(() => this.fitCurrentSSHSession(), 300);
        setTimeout(() => this.fitCurrentSSHSession(), 500);
      }, { once: true });
    },

    /**
     * 切换 SSH 屏幕全屏模式 (使用浏览器原生全屏 API)
     */
    async toggleSSHScreenFullscreen() {
      const sshLayout = document.querySelector('.ssh-ide-layout');
      if (!sshLayout) return;

      try {
        if (!document.fullscreenElement) {
          await sshLayout.requestFullscreen();
          this.sshIdeFullscreen = true;
        } else {
          await document.exitFullscreen();
          this.sshIdeFullscreen = false;
        }
      } catch (err) {
        console.error('全屏切换失败:', err);
      }

      // 监听全屏变化事件
      document.addEventListener('fullscreenchange', () => {
        this.sshIdeFullscreen = !!document.fullscreenElement;
        setTimeout(() => this.fitCurrentSSHSession(), 100);
        setTimeout(() => this.fitCurrentSSHSession(), 300);
        setTimeout(() => this.fitCurrentSSHSession(), 500);
      }, { once: true });
    },

    /**
     * 更新所有终端的主题并强制重新渲染
     */
    updateAllTerminalThemes() {
      // 获取当前最新的主题配置
      const theme = this.getTerminalTheme();

      this.sshSessions.forEach(session => {
        if (session.terminal) {
          try {
            // 核心修复：显式创建新对象，触发 xterm.js 的 options 监听器
            session.terminal.options.theme = { ...theme };

            // 确保渲染器重绘
            if (session.terminal.buffer && session.terminal.buffer.active) {
              session.terminal.refresh(0, session.terminal.rows - 1);
            }
          } catch (err) {
            console.error('更新终端主题失败:', err);
          }
        }
      });
    },

    /**
     * 获取终端主题配置 - 支持深色/浅色模式自动切换
     */
    getTerminalTheme() {
      // 1. 获取 Body 上的实时计算样式
      const computedStyle = getComputedStyle(document.body);
      let bg = computedStyle.getPropertyValue('--bg-primary').trim();
      let fg = computedStyle.getPropertyValue('--text-primary').trim();

      // 2. 转换颜色为规范的 RGB 格式以便计算亮度
      const parseToRGB = (colorStr) => {
        if (!colorStr) return [255, 255, 255];
        if (colorStr.startsWith('rgb')) {
          return colorStr.match(/\d+/g).map(Number);
        }
        if (colorStr.startsWith('#')) {
          let hex = colorStr.substring(1);
          if (hex.length === 3) hex = hex.split('').map(s => s + s).join('');
          return [
            parseInt(hex.substring(0, 2), 16),
            parseInt(hex.substring(2, 4), 16),
            parseInt(hex.substring(4, 6), 16)
          ];
        }
        return [255, 255, 255];
      };

      const rgb = parseToRGB(bg);
      // 精确亮度计算 (W3C 标准)
      const brightness = (rgb[0] * 299 + rgb[1] * 587 + rgb[2] * 114) / 1000;
      const isDark = brightness < 128;

      if (isDark) {
        // 深色模式 - 高对比度调优
        return {
          background: bg || '#0d1117',
          foreground: '#ffffff',
          cursor: '#ffffff',
          selection: 'rgba(56, 139, 253, 0.5)',
          selectionBackground: 'rgba(56, 139, 253, 0.5)',
          black: '#000000',
          red: '#ff6b6b',
          green: '#4ade80',
          yellow: '#fbbf24',
          blue: '#60a5fa',
          magenta: '#e879f9',
          cyan: '#22d3ee',
          white: '#ffffff',
          brightBlack: '#94a3b8',
          brightRed: '#f87171',
          brightGreen: '#4ade80',
          brightYellow: '#fbbf24',
          brightBlue: '#60a5fa',
          brightMagenta: '#e879f9',
          brightCyan: '#22d3ee',
          brightWhite: '#ffffff'
        };
      } else {
        // 浅色模式 - 极致对比度 (针对白底黑字优化)
        return {
          background: bg || '#ffffff',
          foreground: '#000000',
          cursor: '#000000',
          selection: 'rgba(99, 102, 241, 0.3)',
          selectionBackground: 'rgba(99, 102, 241, 0.3)',
          black: '#000000',
          red: '#b91c1c',
          green: '#166534',
          yellow: '#92400e',
          blue: '#1e40af',
          magenta: '#701a75',
          cyan: '#155e75',
          white: '#1f2937',
          brightBlack: '#4b5563',
          brightRed: '#dc2626',
          brightGreen: '#15803d',
          brightYellow: '#b45309',
          brightBlue: '#2563eb',
          brightMagenta: '#9333ea',
          brightCyan: '#0891b2',
          brightWhite: '#6b7280'
        };
      }
    },

    /**
     * 设置主题观察器
     */
    setupThemeObserver() {
      // 1. 监听系统主题变化 (prefers-color-scheme)
      const darkModeQuery = window.matchMedia('(prefers-color-scheme: dark)');
      const handleThemeChange = () => {
        if (this.themeUpdateTimer) clearTimeout(this.themeUpdateTimer);
        this.themeUpdateTimer = setTimeout(() => {
          this.updateAllTerminalThemes();
        }, 150);
      };

      if (darkModeQuery.addEventListener) {
        darkModeQuery.addEventListener('change', handleThemeChange);
      } else if (darkModeQuery.addListener) {
        darkModeQuery.addListener(handleThemeChange);
      }

      // 2. 核心增强：监听 body 和 html 的属性变化 (类名、style 等)
      const attrObserver = new MutationObserver(handleThemeChange);
      attrObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style', 'data-theme'] });
      attrObserver.observe(document.body, { attributes: true, attributeFilter: ['class', 'style'] });

      // 3. 监听自定义 CSS 样式表的变化
      const observer = new MutationObserver(handleThemeChange);
      const customCssElement = document.getElementById('custom-css');
      if (customCssElement) {
        observer.observe(customCssElement, { childList: true, characterData: true, subtree: true });
      }

      // 4. 兜底方案：周期性校准主题 (每1秒检查一次)
      // 解决某些主题切换仅修改 CSS 变量而不触发 DOM 事件的问题
      let lastBg = '';
      this.themePollingInterval = setInterval(() => {
        const currentBg = getComputedStyle(document.body).getPropertyValue('--bg-primary').trim();
        if (currentBg && currentBg !== lastBg) {
          lastBg = currentBg;
          this.updateAllTerminalThemes();
          // 额外的 500ms 延迟刷新，确保 CSS 变量完全生效
          setTimeout(() => this.updateAllTerminalThemes(), 500);
        }
      }, 1000);

      // 保存观察器
      this.themeObserver = observer;
      this.attrObserver = attrObserver;
    },

    /**
     * 初始化会话终端 (WebSocket 版本)
     */
    initSessionTerminal(sessionId) {
      const session = this.sshSessions.find(s => s.id === sessionId);
      if (!session) return;

      // 核心修复：如果全局仓库中不存在该节点的挂载点，则手动创建一个
      let terminalContainer = document.getElementById('ssh-terminal-' + sessionId);
      if (!terminalContainer) {
        // console.log(`[SSH] 为新会话 ${sessionId} 手动创建全局保活挂载点`);
        const warehouse = document.getElementById('ssh-terminal-warehouse');
        if (!warehouse) {
          console.error('全局仓库 #ssh-terminal-warehouse 不存在！');
          return;
        }
        terminalContainer = document.createElement('div');
        terminalContainer.id = 'ssh-terminal-' + sessionId;
        warehouse.appendChild(terminalContainer);
      }

      // 清空容器
      terminalContainer.innerHTML = '';

      // 获取终端主题
      const theme = this.getTerminalTheme();

      // 创建 fit addon（必须在 Terminal 之前创建）
      const fit = new FitAddon();

      // 创建 xterm 实例 - 不指定固定的 cols/rows，让 FitAddon 计算
      const terminal = new Terminal({
        cursorBlink: true,
        cursorStyle: 'bar',
        fontSize: 14,
        fontFamily: 'Consolas, "Courier New", monospace',
        lineHeight: 1.2,
        theme: theme,
        scrollback: 5000,
        allowProposedApi: true // 允许使用新 API
      });

      // 加载插件
      terminal.loadAddon(fit);
      terminal.loadAddon(new WebLinksAddon());

      // 打开终端到容器
      terminal.open(terminalContainer);

      // 保存到会话
      session.terminal = terminal;
      session.fit = fit;

      // 打印容器尺寸用于调试
      console.log(`[SSH] 容器尺寸: ${terminalContainer.offsetWidth}x${terminalContainer.offsetHeight}`);

      // 安全的 fit 函数
      const doFit = () => {
        try {
          fit.fit();
          console.log(`[SSH] Fit 成功: ${terminal.cols}x${terminal.rows}`);
          return true;
        } catch (e) {
          console.log('[SSH] Fit 失败:', e.message);
          return false;
        }
      };

      // 延迟执行 fit - 给渲染器足够时间初始化
      setTimeout(doFit, 100);
      setTimeout(doFit, 300);
      setTimeout(doFit, 500);
      setTimeout(doFit, 1000);

      // 使用 ResizeObserver 监听容器大小变化
      const resizeObserver = new ResizeObserver(() => {
        if (session.fitTimeout) clearTimeout(session.fitTimeout);
        session.fitTimeout = setTimeout(() => {
          this.safeTerminalFit(session);
        }, 150);
      });
      resizeObserver.observe(terminalContainer);
      session.resizeObserver = resizeObserver;

      // 显示连接中信息
      terminal.writeln(`\x1b[1;33m正在连接到 ${session.server.name} (${this.formatHost(session.server.host)})...\x1b[0m`);

      // 建立 WebSocket 连接
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const ws = new WebSocket(`${protocol}//${window.location.host}/ws/ssh`);
      session.ws = ws;

      ws.onopen = () => {
        console.log(`[SSH ${sessionId}] WebSocket 已连接`);
        // 发送连接请求
        ws.send(JSON.stringify({
          type: 'connect',
          serverId: session.server.id,
          cols: terminal.cols,
          rows: terminal.rows
        }));

        // 启动心跳保活
        session.heartbeatInterval = setInterval(() => {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: 'ping' }));
          }
        }, 30000); // 每30秒发送一次心跳
      };

      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data);

          switch (msg.type) {
            case 'connected':
              session.connected = true;
              // 连接成功后清屏，提供完全干净的界面
              terminal.clear();
              // 连接成功后再次 fit 确保终端填满容器
              setTimeout(() => this.safeTerminalFit(session), 100);
              break;

            case 'output':
              terminal.write(msg.data);
              break;

            case 'error':
              terminal.writeln(`\x1b[1;31m错误: ${msg.message}\x1b[0m`);
              break;

            case 'disconnected':
              session.connected = false;
              terminal.writeln('');
              terminal.writeln(`\x1b[1;33m${msg.message}\x1b[0m`);
              break;
          }
        } catch (e) {
          console.error('解析消息失败:', e);
        }
      };

      ws.onerror = (error) => {
        terminal.writeln(`\x1b[1;31mWebSocket 连接错误\x1b[0m`);
        console.error('WebSocket error:', error);
      };

      ws.onclose = () => {
        console.log(`[SSH ${sessionId}] WebSocket 已关闭`);

        // 清除心跳定时器
        if (session.heartbeatInterval) {
          clearInterval(session.heartbeatInterval);
          session.heartbeatInterval = null;
        }

        if (session.connected) {
          terminal.writeln('');
          terminal.writeln(`\x1b[1;33m连接已断开。点击"重新连接"按钮恢复连接。\x1b[0m`);
        }
        session.connected = false;
      };

      // 监听终端输入，发送到 WebSocket (包含多屏同步逻辑)
      terminal.onData(data => {
        // 1. 发送到当前会话
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({
            type: 'input',
            data: data
          }));
        }

        // 2. 多屏同步：如果开启了同步且当前会话在可见分屏中，则广播输入
        if (this.sshSyncEnabled && this.sshViewLayout !== 'single' && this.visibleSessionIds.includes(sessionId)) {
          this.visibleSessionIds.forEach(targetId => {
            if (targetId === sessionId) return; // 避免重复发送给原始会话

            const targetSession = this.getSessionById(targetId);
            if (targetSession && targetSession.ws && targetSession.ws.readyState === WebSocket.OPEN) {
              targetSession.ws.send(JSON.stringify({
                type: 'input',
                data: data
              }));
            }
          });
        }
      });

      // 监听窗口大小变化
      const resizeHandler = () => {
        this.safeTerminalFit(session);
      };
      window.addEventListener('resize', resizeHandler);
      session.resizeHandler = resizeHandler;
    },

    /**
     * 为指定主机添加新会话（作为子标签页）
     */
    addSessionForServer(server) {
      this.showAddSessionSelectModal = false;
      this.openSSHTerminal(server);
    },

    /**
     * 显示新建会话选择框
     */
    showAddSessionModal() {
      this.loadServerList();
      this.showAddSessionSelectModal = true;
    },

    /**
     * 全部打开主机列表中的所有 SSH 会话
     */
    async openAllServersInSSH() {
      if (this.serverList.length === 0) return;

      const count = this.serverList.length;
      this.showGlobalToast(`正在批量建立 ${count} 个连接...`, 'info');

      // 切换到终端标签页
      this.serverCurrentTab = 'terminal';
      this.showSSHQuickMenu = false;

      // 准备批量会话
      let newSessionIds = [];

      for (const server of this.serverList) {
        // 检查是否已经打开
        let session = this.sshSessions.find(s => s.server.id === server.id);
        if (!session) {
          const sessionId = 'session_' + Date.now() + '_' + Math.random().toString(36).substr(2, 5);
          session = {
            id: sessionId,
            server: server,
            terminal: null,
            fit: null,
            ws: null,
            connected: false
          };
          this.sshSessions.push(session);
        }
        newSessionIds.push(session.id);
      }

      // 设置布局模式：如果多于 1 个，使用 grid
      if (newSessionIds.length > 1) {
        this.sshViewLayout = 'grid';
        this.visibleSessionIds = [...newSessionIds];
      } else {
        this.sshViewLayout = 'single';
        this.activeSSHSessionId = newSessionIds[0];
      }

      // 初始化所有新终端
      this.$nextTick(() => {
        newSessionIds.forEach(id => {
          const session = this.getSessionById(id);
          if (session && !session.terminal) {
            this.initSessionTerminal(id);
          }
        });

        // 统一同步 DOM 并适配
        setTimeout(() => {
          this.syncTerminalDOM();
          this.fitAllVisibleSessions();
        }, 300);
      });
    },

    /**
     * 为指定主机添加新会话（作为子标签页）
     */
    addSessionForServer(server) {
      this.showAddSessionSelectModal = false;

      // 检查是否已存在该主机的会话
      const existingSession = this.sshSessions.find(s => s.server.id === server.id);
      if (existingSession) {
        // 如果已存在，直接切换到该标签页
        this.switchToSSHTab(existingSession.id);
        return;
      }

      const sessionId = 'session_' + Date.now();
      const session = {
        id: sessionId,
        server: server,
        terminal: null,
        fit: null,
        ws: null,
        connected: false
      };

      this.sshSessions.push(session);
      this.activeSSHSessionId = sessionId;

      // 切换到新的SSH标签页
      this.serverCurrentTab = 'terminal';

      this.$nextTick(() => {
        this.initSessionTerminal(sessionId);
        // 初始化后强制同步一次 DOM，将其从仓库移动到 Slot (如果它当前被激活)
        this.syncTerminalDOM();
      });
    },

    /**
     * 关闭所有 SSH 会话并返回列表
     */
    async closeAllSSHSessions() {
      if (this.sshSessions.length === 0) return;

      const confirmed = await this.showConfirm({
        title: '关闭所有会话',
        message: `确定要断开并关闭所有 ${this.sshSessions.length} 个 SSH 会话吗？`,
        icon: 'fa-power-off',
        confirmText: '全部关闭',
        confirmClass: 'btn-danger'
      });

      if (!confirmed) return;

      // 循环关闭所有，不带参数调用 closeSSHTerminal 即可
      this.closeSSHTerminal();
      this.showGlobalToast('所有 SSH 会话已关闭', 'info');
    },

    /**
     * 关闭 SSH 终端（关闭所有会话）
     */
    closeSSHTerminal() {
      // 逆序遍历并逐个关闭，以确保数组删除过程安全
      for (let i = this.sshSessions.length - 1; i >= 0; i--) {
        this.closeSSHSession(this.sshSessions[i].id);
      }
      // 最终确认状态
      this.activeSSHSessionId = null;
      this.serverCurrentTab = 'list';
    },

    // ==================== 主机列表展开相关 ====================

    /**
     * 判断主机是否已展开
     */
    isServerExpanded(serverId) {
      return this.expandedServers.has(serverId);
    },

    /**
 * 切换主机展开/收起
 */
    async toggleServer(serverId) {
      if (this.expandedServers.has(serverId)) {
        // 收起：直接移除
        this.expandedServers.delete(serverId);
        this.expandedServers = new Set(this.expandedServers);
      } else {
        // 展开：先立即展开卡片
        this.expandedServers.add(serverId);
        this.expandedServers = new Set(this.expandedServers);

        const server = this.serverList.find(s => s.id === serverId);
        if (!server) return;

        // 判断是否已经加载了完整详情（不仅仅是实时流的指标）
        const hasFullInfo = server.info && server.info.system && Object.keys(server.info.system).length > 0;

        // 如果有缓存数据且当前没有完整详情，立即使用
        if (server.cached_info && !hasFullInfo) {
          server.info = { ...server.cached_info };
          // 后台静默刷新最新数据
          this.loadServerInfo(serverId, false, true);
        } else if (!hasFullInfo) {
          // 确实没有详情数据，去拉取
          this.loadServerInfo(serverId, false, false);
        }
      }
    },

    /**
     * 加载主机详细信息
     */
    async loadServerInfo(serverId, force = false, silent = false) {
      const server = this.serverList.find(s => s.id === serverId);
      if (!server) return;

      if (!silent && !server.info) {
        server.loading = true;
      }

      try {
        const response = await fetch('/api/server/info', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ serverId, force })
        });

        const data = await response.json();
        if (data.success) {
          server.info = { ...data };
          server.error = null;

          if (data.is_cached && !force) {
            setTimeout(() => this.loadServerInfo(serverId, true, true), 300);
          }
        } else {
          server.error = data.error || '加载失败';
        }
      } catch (error) {
        console.error('加载主机信息失败:', error);
        server.error = error.message;
      } finally {
        if (!silent) {
          server.loading = false;
        }
      }
    },

    /**
     * 刷新主机信息（强制刷新）
     */
    async refreshServerInfo(serverId) {
      const server = this.serverList.find(s => s.id === serverId);
      if (server) {
        if (server.loading) return;
        await this.loadServerInfo(serverId, true, false);
      }
    },

    /**
     * 获取运行中的容器数量
     */
    getRunningContainers(containers) {
      if (!containers || !Array.isArray(containers)) return 0;
      return containers.filter(c => c.status && c.status.includes('Up') && !c.status.includes('Paused')).length;
    },

    /**
     * 获取暂停的容器数量
     */
    getPausedContainers(containers) {
      if (!containers || !Array.isArray(containers)) return 0;
      return containers.filter(c => c.status && c.status.includes('Paused')).length;
    },

    /**
     * 获取已停止的容器数量
     */
    getStoppedContainers(containers) {
      if (!containers || !Array.isArray(containers)) return 0;
      return containers.filter(c => c.status && !c.status.includes('Up')).length;
    },

    /**
     * 切换Docker面板展开/收起
     */
    toggleDockerPanel(serverId) {
      if (this.expandedDockerPanels.has(serverId)) {
        this.expandedDockerPanels.delete(serverId);
      } else {
        this.expandedDockerPanels.add(serverId);
      }
      this.expandedDockerPanels = new Set(this.expandedDockerPanels);
    },

    /**
     * 检查Docker面板是否展开
     */
    isDockerPanelExpanded(serverId) {
      return this.expandedDockerPanels.has(serverId);
    },

    /**
     * 获取内存使用率的样式类
     */
    getMemoryClass(usage) {
      const percent = parseFloat(usage);
      if (percent > 90) return 'danger';
      if (percent > 75) return 'warning';
      return '';
    },

    /**
     * 获取磁盘使用率的样式类
     */
    getDiskClass(usage) {
      const percent = parseFloat(usage);
      if (percent > 90) return 'danger';
      if (percent > 75) return 'warning';
      return '';
    },

    formatDateTime(dateStr) {
      if (!dateStr) return '从未检查';
      const date = new Date(dateStr);
      return date.toLocaleString('zh-CN', {
        hour12: false,
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit'
      });
    },

    /**
     * 格式化运行时间为中文格式
     * 将 "up 6 days, 2 hours, 32 minutes" 转换为 "6天2时32分"
     */
    formatUptime(uptimeStr) {
      if (!uptimeStr || typeof uptimeStr !== 'string') return uptimeStr;

      // 移除 "up " 前缀
      let str = uptimeStr.replace(/^up\s+/i, '');

      // 提取各个时间部分
      const weekMatch = str.match(/(\d+)\s*weeks?/i);
      const dayMatch = str.match(/(\d+)\s*days?/i);
      const hourMatch = str.match(/(\d+)\s*hours?/i);
      const minMatch = str.match(/(\d+)\s*minutes?/i);

      let days = dayMatch ? parseInt(dayMatch[1], 10) : 0;
      const weeks = weekMatch ? parseInt(weekMatch[1], 10) : 0;
      const hours = hourMatch ? parseInt(hourMatch[1], 10) : 0;
      const minutes = minMatch ? parseInt(minMatch[1], 10) : 0;

      // 将周转换为天并累加
      days += weeks * 7;

      // 构建中文格式
      let result = '';
      if (days > 0) result += `${days}天`;
      if (hours > 0) result += `${hours}时`;
      if (minutes > 0) result += `${minutes}分`;

      // 如果都是0，显示 "0分"
      if (result === '') result = '0分';

      return result;
    },

    /**
     * 翻译系统信息的字段名为中文
     */
    translateInfoKey(key) {
      const translations = {
        // 系统信息
        'OS': '操作系统',
        'Kernel': '内核版本',
        'Architecture': '架构',
        'Hostname': '主机名',
        'Uptime': '运行时间',
        // CPU 信息
        'Model': '型号',
        'Cores': '核心数',
        'Usage': '使用率',
        // 内存信息
        'Total': '总计',
        'Used': '已用',
        'Free': '可用',
        // 其他
        'Version': '版本'
      };
      return translations[key] || key;
    },

    /**
     * 删除主机
     */
    async deleteServerById(serverId) {
      const confirmed = await this.showConfirm({
        title: '删除主机',
        message: '确定要删除这台主机吗？',
        icon: 'fa-trash',
        confirmText: '确定',
        confirmClass: 'btn-danger'
      });

      if (!confirmed) return;

      try {
        const response = await fetch(`/api/server/accounts/${serverId}`, {
          method: 'DELETE'
        });

        const data = await response.json();

        if (data.success) {
          showToast('主机删除成功', 'success');
          await this.loadServerList();
        } else {
          showToast('删除失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('删除主机失败:', error);
        showToast('删除主机失败', 'error');
      }
    },

    /**
     * 重启主机
     */
    async rebootServerById(serverId) {
      const confirmed = await this.showConfirm({
        title: '重启主机',
        message: '确定要重启这台主机吗？',
        icon: 'fa-redo',
        confirmText: '重启',
        confirmClass: 'btn-warning'
      });

      if (!confirmed) return;

      try {
        const response = await fetch('/api/server/action', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ serverId, action: 'reboot' })
        });

        const data = await response.json();

        if (data.success) {
          showToast('重启命令已发送', 'success');
        } else {
          showToast('重启失败: ' + data.message, 'error');
        }
      } catch (error) {
        console.error('重启主机失败:', error);
        showToast('重启主机失败', 'error');
      }
    },

    /**
     * 关机
     */
    async shutdownServerById(serverId) {
      const confirmed = await this.showConfirm({
        title: '关闭主机',
        message: '确定要关闭这台主机吗？此操作不可逆！',
        icon: 'fa-power-off',
        confirmText: '确定关机',
        confirmClass: 'btn-danger'
      });

      if (!confirmed) return;

      try {
        const response = await fetch('/api/server/action', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ serverId, action: 'shutdown' })
        });

        const data = await response.json();

        if (data.success) {
          showToast('关机命令已发送', 'success');
        } else {
          showToast('关机失败: ' + data.message, 'error');
        }
      } catch (error) {
        console.error('关机失败:', error);
        showToast('关机失败', 'error');
      }
    },

    async loadMonitorConfig() {
      try {
        const response = await fetch('/api/server/monitor/config');
        const data = await response.json();

        if (data.success && data.data) {
          this.monitorConfig = {
            interval: data.data.probe_interval || 60,
            timeout: data.data.probe_timeout || 10,
            logRetentionDays: data.data.log_retention_days || 7
          };
          this.startServerPolling(); // 加载配置后启动轮询
        }
      } catch (error) {
        console.error('加载监控配置失败:', error);
      }
    },

    async saveMonitorConfig() {
      this.monitorConfigSaving = true;
      this.monitorConfigError = '';
      this.monitorConfigSuccess = '';

      try {
        const response = await fetch('/api/server/monitor/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            probe_interval: parseInt(this.monitorConfig.interval),
            probe_timeout: parseInt(this.monitorConfig.timeout),
            log_retention_days: parseInt(this.monitorConfig.logRetentionDays)
          })
        });

        const data = await response.json();

        if (data.success) {
          this.monitorConfigSuccess = '配置保存成功';
          this.showGlobalToast('监控配置已更新', 'success');
          setTimeout(() => {
            this.monitorConfigSuccess = '';
          }, 3000);
        } else {
          this.monitorConfigError = '保存失败: ' + data.error;
        }
      } catch (error) {
        console.error('保存监控配置失败:', error);
        this.monitorConfigError = '保存失败: ' + error.message;
      } finally {
        this.monitorConfigSaving = false;
      }
    },

    /**
     * 凭据管理
     */
    async loadCredentials() {
      try {
        const response = await fetch('/api/server/credentials');
        const data = await response.json();
        if (data.success) {
          this.serverCredentials = data.data;
        }
      } catch (error) {
        console.error('加载凭据失败:', error);
      }
    },

    async saveCredential() {
      this.credError = '';
      if (!this.credForm.name || !this.credForm.username) {
        this.credError = '请填写完整信息';
        return;
      }
      try {
        const response = await fetch('/api/server/credentials', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.credForm)
        });
        const data = await response.json();
        if (data.success) {
          this.showGlobalToast('凭据已保存', 'success');
          this.showAddCredentialModal = false;
          this.credForm = { name: '', username: '', password: '' };
          this.credError = '';
          await this.loadCredentials();
        } else {
          this.credError = data.error || '保存失败';
        }
      } catch (error) {
        this.credError = '保存失败: ' + error.message;
        this.showGlobalToast('保存失败', 'error');
      }
    },

    async deleteCredential(id) {
      const confirmed = await this.showConfirm({
        title: '删除凭据',
        message: '确定删除此凭据吗？',
        icon: 'fa-trash',
        confirmText: '删除',
        confirmClass: 'btn-danger'
      });

      if (!confirmed) return;
      try {
        const response = await fetch(`/api/server/credentials/${id}`, {
          method: 'DELETE'
        });
        const data = await response.json();
        if (data.success) {
          this.showGlobalToast('凭据已删除', 'success');
          await this.loadCredentials();
        }
      } catch (error) {
        this.showGlobalToast('删除失败', 'error');
      }
    },

    async setDefaultCredential(id) {
      const confirmed = await this.showConfirm({
        title: '设为默认',
        message: '确定将此凭据设为默认吗？',
        icon: 'fa-star',
        confirmText: '确定',
        confirmClass: 'btn-primary'
      });

      if (!confirmed) return;

      try {
        const response = await fetch(`/api/server/credentials/${id}/default`, {
          method: 'PUT'
        });

        if (response.status === 404) {
          this.showGlobalToast('接口未更新，请刷新页面或重启服务', 'error');
          return;
        }

        const data = await response.json();
        if (data.success) {
          this.showGlobalToast('已设置为默认凭据', 'success');
          await this.loadCredentials();
        } else {
          this.showGlobalToast('设置失败: ' + data.error, 'error');
        }
      } catch (error) {
        this.showGlobalToast('设置失败', 'error');
      }
    },

    applyCredential(event) {
      const id = event.target.value;
      if (!id) return;
      const cred = this.serverCredentials.find(c => c.id == id);
      if (cred) {
        this.serverForm.username = cred.username;
        this.serverForm.password = cred.password;
        this.serverForm.authType = 'password';
      }
    },

    /**
     * Docker 容器操作
     */
    async handleDockerAction(serverId, containerId, action) {
      const server = this.serverList.find(s => s.id === serverId);
      if (server) server.loading = true;

      try {
        const response = await fetch('/api/server/docker/action', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ serverId, containerId, action })
        });
        const data = await response.json();
        if (data.success) {
          this.showGlobalToast('Docker 操作已执行', 'success');
          // 延迟刷新以等待同步
          setTimeout(() => this.loadServerInfo(serverId), 1000);
        } else {
          this.showGlobalToast('操作失败: ' + data.message, 'error');
        }
      } catch (error) {
        this.showGlobalToast('Docker 操作异常', 'error');
      } finally {
        if (server) server.loading = false;
      }
    },

    async loadServerList() {
      this.serverLoading = true;
      try {
        const response = await fetch('/api/server/accounts');
        const data = await response.json();

        if (data.success) {
          // 将主机数据存储到 serverList, 并保留现有的 info 等状态
          const existingServersMap = new Map(this.serverList.map(s => [s.id, s]));

          this.serverList = data.data.map(server => {
            const existing = existingServersMap.get(server.id);
            return {
              ...server,
              // 如果已存在且有 info，保留 info；否则初始化为 null
              info: existing ? existing.info : null,
              error: existing ? existing.error : null,
              loading: existing ? existing.loading : false
            };
          });
        } else {
          // 处理错误情况
          console.error('加载主机列表失败:', data.error);
          if (data.error && data.error.includes('未认证')) {
            // 认证错误,不显示toast,避免干扰用户
            this.serverList = [];
          } else {
            this.showGlobalToast('加载主机列表失败: ' + data.error, 'error');
            this.serverList = [];
          }
        }
      } catch (error) {
        console.error('加载主机列表失败:', error);
        this.showGlobalToast('加载主机列表失败', 'error');
        this.serverList = [];
      } finally {
        this.serverLoading = false;
        // 1. 首先尝试连接实时指标推送流 (秒级监控)
        if (this.isAuthenticated && this.mainActiveTab === 'server') {
          this.connectMetricsStream();
        }

        // 2. 作为保底或背景维护，启动标准轮询
        this.startServerPolling();

        // 3. 进入页面时立即 ping 所有主机获取延迟
        this.pingAllServers();
      }
    },

    /**
     * 批量 ping 所有主机获取延迟
     */
    async pingAllServers() {
      if (this.serverList.length === 0) return;

      try {
        const response = await fetch('/api/server/ping-all', { method: 'POST' });
        const data = await response.json();

        if (data.success && data.results) {
          // 更新 serverList 中的延迟数据
          for (const result of data.results) {
            const server = this.serverList.find(s => s.id === result.serverId);
            if (server && result.success) {
              server.response_time = result.latency;
            }
          }
        }
      } catch (error) {
        console.warn('批量 ping 失败:', error);
      }
    },


    async loadMonitorLogs(page) {
      if (typeof page === 'number') {
        this.logPage = page;
      }

      this.monitorLogsLoading = true;

      try {
        const params = new URLSearchParams({
          page: this.logPage,
          pageSize: this.logPageSize
        });

        if (this.logFilter.serverId) {
          params.append('serverId', this.logFilter.serverId);
        }
        if (this.logFilter.status) {
          params.append('status', this.logFilter.status);
        }

        const response = await fetch(`/api/server/monitor/logs?${params}`);
        const data = await response.json();

        if (data.success) {
          this.monitorLogs = data.data;
        } else {
          this.showGlobalToast('加载日志失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('加载监控日志失败:', error);
        this.showGlobalToast('加载监控日志失败', 'error');
      } finally {
        this.monitorLogsLoading = false;
      }
    },


    /**
     * 启动服务器状态轮询 (带可见性检查)
     */
    startServerPolling() {
      // 关键决策：若有 WebSocket 实时流，则无需发起任何 HTTP 主动探测
      if (this.metricsWsConnected) {
        if (this.serverPollingTimer) {
          console.warn('🛡️ 实时流已接管，正在休眠后台轮询任务');
          this.stopServerPolling();
        }
        return;
      }

      // 确保只有一个轮询定时器在运行
      if (this.serverPollingTimer) return;

      const interval = Math.max(30000, (this.monitorConfig.interval || 60) * 1000);
      console.log(`📡 实时流不可用，启动后台降级轮询 (${interval / 1000}s)`);

      // 重置倒计时
      this.serverRefreshCountdown = Math.floor(interval / 1000);
      this.serverRefreshProgress = 100;

      // 启动倒计时定时器 (仅在可见时运行)
      this.serverCountdownInterval = setInterval(() => {
        if (document.visibilityState !== 'visible') return;

        if (this.serverRefreshCountdown > 0) {
          this.serverRefreshCountdown--;
          this.serverRefreshProgress = (this.serverRefreshCountdown / (interval / 1000)) * 100;
        }
      }, 1000);

      // 启动主轮询定时器
      this.serverPollingTimer = setInterval(() => {
        // 只要可见且已认证就探测，不再局限于 server 标签页
        if (document.visibilityState === 'visible' && this.isAuthenticated) {
          this.probeAllServers();
          // 重置倒计时
          this.serverRefreshCountdown = Math.floor(interval / 1000);
          this.serverRefreshProgress = 100;
        }
      }, interval);
    },

    stopServerPolling() {
      if (this.serverPollingTimer) {
        clearInterval(this.serverPollingTimer);
        this.serverPollingTimer = null;
      }
      if (this.serverCountdownInterval) {
        clearInterval(this.serverCountdownInterval);
        this.serverCountdownInterval = null;
      }
    },

    /**
     * 连接实时指标流 (WebSocket)
     */
    connectMetricsStream() {
      if (!this.isAuthenticated) {
        console.warn('⚠️ 尝试连接实时流失败: 用户未登录');
        return;
      }

      if (this.metricsWsConnected || this.metricsWsConnecting) {
        console.warn('ℹ️ 实时指标流已在连接中或已连接');
        return;
      }

      this.metricsWsConnecting = true;
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const wsUrl = `${protocol}//${window.location.host}/ws/metrics`;

      console.warn('🚀 正在发起实时指标流连接:', wsUrl);
      const ws = new WebSocket(wsUrl);

      ws.onopen = () => {
        this.metricsWsConnected = true;
        this.metricsWsConnecting = false;
        console.warn('✅ 实时指标流握手成功');
      };

      ws.onmessage = (event) => {
        try {
          const payload = JSON.parse(event.data);
          if (payload.type === 'metrics_update') {
            // console.log('📊 收到实时指标更新:', payload.data.length, '台主机');
            this.handleMetricsUpdate(payload.data);
          }
        } catch (err) {
          console.error('解析指标数据失败:', err);
        }
      };

      ws.onclose = () => {
        this.metricsWsConnected = false;
        this.metricsWsConnecting = false;
        this.metricsWs = null;
        console.warn('❌ 实时指标流连接已关闭');
      };

      ws.onerror = (err) => {
        console.error('WebSocket 连接错误:', err);
        this.metricsWsConnecting = false;
        this.metricsWsConnected = false;
      };

      this.metricsWs = ws;
    },

    /**
     * 关闭实时指标流
     */
    closeMetricsStream() {
      if (this.metricsWs) {
        this.metricsWs.close();
        this.metricsWs = null;
      }
    },

    /**
     * 处理收到的实时指标更新
     */
    handleMetricsUpdate(data) {
      if (!data || !Array.isArray(data)) return;

      // 智能更新 serverList 中的数据
      data.forEach(item => {
        const server = this.serverList.find(s => s.id === item.serverId);
        if (server) {
          // 初始化结构（如果为空），防止模板渲染 crash
          if (!server.info) {
            // 注意：在 Vue 3 中，为了确保响应性，直接给对象添加新属性可能不触发更新
            // 但如果 server 本身是 reactive 的，直接赋值 server.info = {...} 应该是没问题的
            server.info = {
              cpu: { Load: '', Usage: '0%', Cores: '-' },
              memory: { Used: '-', Total: '-', Usage: '0%' },
              disk: [{ device: '/', used: '-', total: '-', usage: '0%' }],
              system: {},
              docker: { installed: false, containers: [] }
            };
          }

          // 1. 更新 CPU 负载
          if (!server.info.cpu) server.info.cpu = {};
          server.info.cpu.Load = item.metrics.load;
          server.info.cpu.Usage = item.metrics.cpu_usage;
          server.info.cpu.Cores = item.metrics.cores || '-';

          // 2. 更新内存数据 (解析 "123/1024MB")
          if (!server.info.memory) server.info.memory = {};
          const memMatch = item.metrics.mem_usage.match(/(\d+)\/(\d+)MB/);
          if (memMatch) {
            const used = parseInt(memMatch[1]);
            const total = parseInt(memMatch[2]);
            server.info.memory.Used = used + ' MB';
            server.info.memory.Total = total + ' MB';
            server.info.memory.Usage = Math.round((used / total) * 100) + '%';
          }

          // 3. 更新磁盘数据 (解析 "10G/50G (20%)")
          if (!server.info.disk || !server.info.disk[0]) {
            server.info.disk = [{ device: '/', used: '-', total: '-', usage: '0%' }];
          }
          const diskMatch = item.metrics.disk_usage.match(/([^\/]+)\/([^\s]+)\s\(([\d%.]+)\)/);
          if (diskMatch) {
            server.info.disk[0].used = diskMatch[1];
            server.info.disk[0].total = diskMatch[2];
            server.info.disk[0].usage = diskMatch[3];
          }

          // 4. 更新 Docker 概要信息
          if (!server.info.docker) server.info.docker = { installed: false, containers: [] };
          server.info.docker.installed = item.metrics.docker.installed;
          server.info.docker.runningCount = item.metrics.docker.running;
          server.info.docker.stoppedCount = item.metrics.docker.stopped;

          server.status = 'online';
          server.error = null;
        }
      });
    },
    /**
     * 手动探测所有主机
     */
    async probeAllServers() {
      this.probeStatus = 'loading';
      try {
        const response = await fetch('/api/server/check-all', { method: 'POST' });
        const data = await response.json();
        if (data.success) {
          this.probeStatus = 'success';
          await this.loadServerList();
        } else {
          this.probeStatus = 'error';
        }
      } catch (error) {
        console.error('探测主机失败:', error);
        this.probeStatus = 'error';
      }
      setTimeout(() => { this.probeStatus = ''; }, 3000);
    },

    /**
     * 加载历史指标记录
     */
    async loadMetricsHistory(page = null) {
      if (page !== null) {
        this.metricsHistoryPagination.page = page;
      }

      this.metricsHistoryLoading = true;

      try {
        // 计算时间范围 (使用 UTC 时间，与数据库 CURRENT_TIMESTAMP 一致)
        let startTime = null;
        const now = Date.now();

        switch (this.metricsHistoryTimeRange) {
          case '1h':
            startTime = new Date(now - 60 * 60 * 1000).toISOString();
            break;
          case '6h':
            startTime = new Date(now - 6 * 60 * 60 * 1000).toISOString();
            break;
          case '24h':
            startTime = new Date(now - 24 * 60 * 60 * 1000).toISOString();
            break;
          case '7d':
            startTime = new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString();
            break;
          case 'all':
          default:
            startTime = null;
        }

        console.log('[History] 查询时间范围:', this.metricsHistoryTimeRange, '起始时间:', startTime);

        const params = new URLSearchParams({
          page: this.metricsHistoryPagination.page,
          pageSize: this.metricsHistoryPagination.pageSize
        });

        if (this.metricsHistoryFilter.serverId) {
          params.append('serverId', this.metricsHistoryFilter.serverId);
        }

        if (startTime) {
          params.append('startTime', startTime);
        }

        const response = await fetch(`/api/server/metrics/history?${params}`);
        const data = await response.json();

        if (data.success) {
          this.metricsHistoryList = data.data;
          this.metricsHistoryTotal = data.pagination.total;
          this.metricsHistoryPagination = {
            page: data.pagination.page,
            pageSize: data.pagination.pageSize,
            totalPages: data.pagination.totalPages
          };
        } else {
          this.showGlobalToast('加载历史记录失败: ' + data.error, 'error');
        }

        // 同时加载采集器状态
        this.loadCollectorStatus();

        // 渲染图表
        this.$nextTick(() => {
          this.renderMetricsCharts();
        });
      } catch (error) {
        console.error('加载历史指标失败:', error);
        this.showGlobalToast('加载历史指标失败', 'error');
      } finally {
        this.metricsHistoryLoading = false;
      }
    },

    /**
     * 设置时间范围筛选
     */
    setMetricsTimeRange(range) {
      this.metricsHistoryTimeRange = range;
      this.loadMetricsHistory(1);
    },

    /**
     * 手动触发一次历史采集
     */
    async triggerMetricsCollect() {
      try {
        const response = await fetch('/api/server/metrics/collect', { method: 'POST' });
        const data = await response.json();

        if (data.success) {
          this.showGlobalToast('已触发历史指标采集', 'success');
          // 延迟刷新数据
          setTimeout(() => this.loadMetricsHistory(), 1000);
        } else {
          this.showGlobalToast('触发采集失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('触发采集失败:', error);
        this.showGlobalToast('触发采集失败', 'error');
      }
    },

    /**
     * 渲染历史指标图表
     */
    renderMetricsCharts() {
      if (!window.Chart || !this.groupedMetricsHistory) return;

      Object.entries(this.groupedMetricsHistory).forEach(([serverId, records]) => {
        // 由于记录是倒序排列的，绘图前先克隆并正序排列
        const sortedRecords = [...records].reverse();

        // 准备数据
        const labels = sortedRecords.map(r => {
          const d = new Date(r.recorded_at);
          return d.getHours() + ':' + String(d.getMinutes()).padStart(2, '0');
        });
        const cpuData = sortedRecords.map(r => r.cpu_usage || 0);
        const memData = sortedRecords.map(r => r.mem_usage || 0);

        this.$nextTick(() => {
          const canvasId = `metrics-chart-${serverId}`;
          const canvas = document.getElementById(canvasId);
          if (!canvas) return;

          // 使用 Chart.js 官方推荐的方式获取并销毁已存在的实例
          const existingChart = Chart.getChart(canvas);
          if (existingChart) {
            existingChart.destroy();
          }

          // 创建新图表
          new Chart(canvas, {
            type: 'line',
            data: {
              labels: labels,
              datasets: [
                {
                  label: 'CPU (%)',
                  data: cpuData,
                  borderColor: '#10b981',
                  backgroundColor: 'rgba(16, 185, 129, 0.1)',
                  borderWidth: 2,
                  fill: true,
                  tension: 0.4,
                  pointRadius: 0,
                  pointHoverRadius: 4
                },
                {
                  label: '内存 (%)',
                  data: memData,
                  borderColor: '#3b82f6',
                  backgroundColor: 'rgba(59, 130, 246, 0.1)',
                  borderWidth: 2,
                  fill: true,
                  tension: 0.4,
                  pointRadius: 0,
                  pointHoverRadius: 4
                }
              ]
            },
            options: {
              responsive: true,
              maintainAspectRatio: false,
              plugins: {
                legend: { display: false },
                tooltip: {
                  mode: 'index',
                  intersect: false,
                  padding: 10,
                  backgroundColor: 'rgba(13, 17, 23, 0.9)',
                  titleColor: '#8b949e',
                  bodyColor: '#e6edf3',
                  borderColor: 'rgba(255, 255, 255, 0.1)',
                  borderWidth: 1
                }
              },
              scales: {
                x: {
                  display: true,
                  grid: { display: false },
                  ticks: {
                    maxRotation: 0,
                    autoSkip: true,
                    maxTicksLimit: 6,
                    font: { size: 10 },
                    color: '#8b949e'
                  }
                },
                y: {
                  display: true,
                  min: 0,
                  max: 100,
                  grid: { color: 'rgba(255, 255, 255, 0.05)' },
                  ticks: {
                    font: { size: 10 },
                    color: '#8b949e',
                    stepSize: 20
                  }
                }
              },
              interaction: {
                mode: 'nearest',
                axis: 'x',
                intersect: false
              }
            }
          });
        });
      });
    },

    /**
     * 加载采集器状态
     */
    async loadCollectorStatus() {
      try {
        const response = await fetch('/api/server/metrics/collector/status');
        const data = await response.json();

        if (data.success) {
          this.metricsCollectorStatus = data.data;
          // 同步采集间隔设置
          if (data.data.interval) {
            this.metricsCollectInterval = Math.floor(data.data.interval / 60000);
          }
        }
      } catch (error) {
        console.error('加载采集器状态失败:', error);
      }
    },

    /**
     * 获取 CPU 使用率对应的颜色类
     */
    getCpuClass(usage) {
      if (!usage && usage !== 0) return '';
      const val = parseFloat(usage);
      if (val >= 90) return 'critical';
      if (val >= 70) return 'warning';
      return 'normal';
    },

    /**
     * 切换历史记录主机卡片的展开状态
     */
    toggleMetricsServerExpand(serverId) {
      const index = this.expandedMetricsServers.indexOf(serverId);
      if (index === -1) {
        this.expandedMetricsServers.push(serverId);
      } else {
        this.expandedMetricsServers.splice(index, 1);
      }
    },

    /**
     * 更新历史采集间隔
     */
    async updateMetricsCollectInterval() {
      try {
        const intervalMs = this.metricsCollectInterval * 60 * 1000;
        const response = await fetch('/api/server/metrics/collector/interval', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ interval: intervalMs })
        });
        const data = await response.json();

        if (data.success) {
          this.showGlobalToast(`采集间隔已更新为 ${this.metricsCollectInterval} 分钟`, 'success');
          this.loadCollectorStatus();
        } else {
          this.showGlobalToast('更新失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('更新采集间隔失败:', error);
        this.showGlobalToast('更新采集间隔失败', 'error');
      }
    },

    /**
     * 导出主机配置
     */
    async exportServers() {
      try {
        const response = await fetch('/api/server/accounts/export');
        const data = await response.json();
        if (data.success) {
          const blob = new Blob([JSON.stringify(data.data, null, 2)], { type: 'application/json' });
          const url = URL.createObjectURL(blob);
          const a = document.createElement('a');
          a.href = url;
          a.download = `servers_${new Date().toISOString().split('T')[0]}.json`;
          a.click();
          URL.revokeObjectURL(url);
          this.showGlobalToast('导出成功', 'success');
        } else {
          this.showGlobalToast('导出失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('导出主机失败:', error);
        this.showGlobalToast('导出主机失败', 'error');
      }
    },

    /**
     * 加载代码片段列表
     */
    async loadSnippets() {
      try {
        const response = await fetch('/api/server/snippets');
        const data = await response.json();
        if (data.success) {
          this.sshSnippets = data.data;
        }
      } catch (error) {
        console.error('加载代码片段失败:', error);
      }
    },

    /**
     * 保存代码片段 (新增或更新)
     */
    async saveSnippet() {
      if (!this.snippetForm.title || !this.snippetForm.content) {
        this.snippetError = '标题和内容不能为空';
        return;
      }
      this.snippetSaving = true;
      this.snippetError = '';
      try {
        const isEdit = !!this.snippetForm.id;
        const url = isEdit ? `/api/server/snippets/${this.snippetForm.id}` : '/api/server/snippets';
        const method = isEdit ? 'PUT' : 'POST';
        const response = await fetch(url, {
          method,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.snippetForm)
        });
        const data = await response.json();
        if (data.success) {
          this.showGlobalToast(isEdit ? '更新成功' : '创建成功', 'success');
          this.showSnippetModal = false;
          await this.loadSnippets();
        } else {
          this.snippetError = data.error || '保存失败';
        }
      } catch (error) {
        this.snippetError = '请求失败: ' + error.message;
      } finally {
        this.snippetSaving = false;
      }
    },

    /**
     * 删除代码片段
     */
    async deleteSnippet(id) {
      const confirmed = await this.showConfirm({
        title: '删除片段',
        message: '确定要删除这个代码片段吗？',
        icon: 'fa-trash',
        confirmText: '删除',
        confirmClass: 'btn-danger'
      });
      if (!confirmed) return;
      try {
        const response = await fetch(`/api/server/snippets/${id}`, { method: 'DELETE' });
        const data = await response.json();
        if (data.success) {
          this.showGlobalToast('已删除', 'success');
          await this.loadSnippets();
        }
      } catch (error) {
        this.showGlobalToast('删除失败', 'error');
      }
    },

    /**
     * 发送代码片段到当前激活或所有可见终端
     */
    sendSnippet(content) {
      if (!content) return;
      const dataToSend = content.endsWith('\n') ? content.replace(/\n$/, '\r') : content + '\r';
      if (this.sshSyncEnabled && this.sshViewLayout !== 'single') {
        this.visibleSessionIds.forEach(id => {
          const session = this.getSessionById(id);
          if (session && session.ws && session.ws.readyState === WebSocket.OPEN) {
            session.ws.send(JSON.stringify({ type: 'input', data: dataToSend }));
          }
        });
        this.showGlobalToast('指令已同步广播', 'success');
      } else {
        const session = this.getSessionById(this.activeSSHSessionId);
        if (session && session.ws && session.ws.readyState === WebSocket.OPEN) {
          session.ws.send(JSON.stringify({ type: 'input', data: dataToSend }));
          this.showGlobalToast('指令已发送', 'success');
        } else {
          this.showGlobalToast('未连接 SSH 会话', 'warning');
        }
      }
    },

    openAddSnippetModal() {
      this.snippetForm = { id: null, title: '', content: '', category: 'common', description: '' };
      this.snippetError = '';
      this.showSnippetModal = true;
    },

    openEditSnippetModal(snippet) {
      this.snippetForm = { ...snippet };
      this.snippetError = '';
      this.showSnippetModal = true;
    },

    // 整合所有模块的方法
    ...authMethods,
    ...zeaburMethods,
    ...paasMethods,
    ...koyebMethods,
    ...flyMethods,
    ...selfHMethods,
    ...dnsMethods,
    ...r2Methods,
    ...openaiMethods,
    ...antigravityMethods,
    ...geminiCliMethods,
    ...settingsMethods,
    ...transitionsMethods,
    ...systemLogsMethods,
    ...logViewerMethods,
    formatDateTime,
    formatRegion,

    /**
     * 获取日志级别对应的图标
     */
    getLogIcon(level) {
      const icons = {
        'DEBUG': 'fa-bug',
        'INFO': 'fa-info-circle',
        'WARN': 'fa-exclamation-triangle',
        'ERROR': 'fa-times-circle',
        'FATAL': 'fa-skull-crossbones'
      };
      return icons[level?.toUpperCase()] || 'fa-file-alt';
    },

    /**
     * 格式化日志消息，支持简易 ANSI 颜色转换
     */
    formatMessage(msg) {
      if (!msg) return '';
      // 基础 ANSI 颜色转换 (简单实现)
      let formatted = msg
        .replace(/\x1b\[1;32m/g, '<span class="ansi-fg-32">')
        .replace(/\x1b\[1;33m/g, '<span class="ansi-fg-33">')
        .replace(/\x1b\[1;31m/g, '<span class="ansi-fg-31">')
        .replace(/\x1b\[0m/g, '</span>');
      return formatted;
    }
  }
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
    // 1. 加载所有模块模板
    if (window.TemplateLoader) {
      await window.TemplateLoader.loadAll();
    } else {
      console.warn('[App] TemplateLoader not found, proceeding with fallback');
    }

    // 2. 挂载 Vue 应用
    app.mount('#app');

    const elapsed = Date.now() - startTime;
    console.log(`[App] Initialized and mounted in ${elapsed}ms`);
  } catch (error) {
    console.error('[App] Critical failure during initialization:', error);
    // 即使模板加载失败，也尝试挂载 Vue 以显示基础界面或错误状态
    app.mount('#app');
  }
}

// 启动应用
initApp();

