/**
 * API Monitor - 主应用模块
 * 整合所有功能模块，初始化 Vue 应用
 */

// 导入功能模块
import { authMethods } from './modules/auth.js';
import { zeaburMethods } from './modules/zeabur.js';
import { dnsMethods } from './modules/dns.js';
import { openaiMethods } from './modules/openai.js';
import { antigravityMethods } from './modules/antigravity.js';
import { geminiCliMethods } from './modules/gemini-cli.js';
import { settingsMethods } from './modules/settings.js';
import { systemLogsMethods } from './modules/logs.js';
import { transitionsMethods } from './modules/transitions.js';
import { toast } from './modules/toast.js';
import { formatDateTime, formatFileSize } from './modules/utils.js';

// 导入全局状态
import { store } from './store.js';

// 获取 Vue
const { createApp, toRefs } = Vue;

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
      dnsSelectedZoneName: '',
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
      agOauthUrl: '',
      agCustomProjectId: '',
      agAllowRandomProjectId: true,
      showOAuthExpand: false,
      antigravityQuotaViewMode: 'grouped',
      antigravityLogDetail: null,
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
      geminiCliLogDetail: null,
      showGeminiCliLogDetailModal: false,
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
      activeSessionId: null,
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

      // 拖拽状态 (UI only)
      draggedIndex: null,

      // 设置模态框 - 密码与样式表单
      newPassword: '',
      confirmPassword: '',
      passwordError: '',
      passwordSuccess: '',
      customCss: '',
      customCssError: '',
      customCssSuccess: '',
      settingsCurrentTab: 'general', // 'general', 'modules', 'database', 'appearance'

      // 日志保留设置
      logSettings: {
        days: 0,
        count: 0,
        dbSizeMB: 0
      },
      logSettingsSaving: false,
      logLimitsEnforcing: false,

      // 系统日志
      systemLogs: [],
      systemLogsLoading: false,
      systemLogMessages: [],
      logStreamEnabled: false,
      logStreamInterval: null
    };
  },

  computed: {
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
     * 判断当前是否有任何模态框打开
     */
    isAnyModalOpen() {
      return this.showSettingsModal ||
        this.showLogsModal ||
        this.showSystemLogsModal ||
        this.showAddZeaburAccountModal ||
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
        (this.customDialog && this.customDialog.show);
    }
  },

  async mounted() {
    // 保存 Vue 实例到全局，供其他模块使用
    window.vueApp = this;

    // 加载模块可见性和顺序设置
    this.loadModuleSettings();

    // SSH 终端使用固定深色主题,不需要监听主题变化
    // this.setupThemeObserver();

    // 监听标签页可见性变化，当回到页面时立即触发一次活跃模块的刷新
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible' && this.isAuthenticated) {
        console.log('👀 标签页已获得关注，触发活跃模块刷新');

        // 服务器模块
        if (this.mainActiveTab === 'server' && this.serverCurrentTab === 'list' && this.serverPollingEnabled) {
          this.probeAllServers();
        }

        // Zeabur 模块
        if (this.mainActiveTab === 'zeabur') {
          this.fetchData();
        }

        // Antigravity 模块
        if (this.mainActiveTab === 'antigravity' && this.antigravityCurrentTab === 'quotas') {
          this.loadAntigravityQuotas();
        }

        // Gemini CLI 模块
        if (this.mainActiveTab === 'gemini-cli' && this.geminiCliCurrentTab === 'models') {
          this.loadGeminiCliModels();
        }
      }
    });

    try {
      // 检查主机是否已设置密码
      const hasPasswordResponse = await fetch('/api/check-password');
      const { hasPassword } = await hasPasswordResponse.json();

      if (!hasPassword) {
        // 首次使用，显示设置密码界面
        this.showSetPasswordModal = true;
        this.isCheckingAuth = false;
        return;
      }

      // 检查本地是否有保存的密码和时间戳
      const savedPassword = localStorage.getItem('admin_password');
      const savedTime = localStorage.getItem('password_time');

      if (savedPassword && savedTime) {
        const now = Date.now();
        const elapsed = now - parseInt(savedTime);
        const fourDays = 4 * 24 * 60 * 60 * 1000;

        if (elapsed < fourDays) {
          // 4天内，自动登录
          this.loginPassword = savedPassword;
          await this.verifyPassword();
          this.isCheckingAuth = false;
          return;
        }
      }

      // 需要输入密码
      this.showLoginModal = true;
      this.isCheckingAuth = false;
    } catch (error) {
      console.error('认证检查失败:', error);
      this.showLoginModal = true;
      this.isCheckingAuth = false;
    }
  },

  watch: {
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
      if (this.mainActiveTab === 'zeabur' && !this.dataRefreshPaused) {
        this.startAutoRefresh();
      }
    },

    serverCurrentTab(newVal) {
      if (newVal === 'management') {
        this.loadMonitorConfig();
        this.loadServerList();
        this.loadMonitorLogs();
      } else if (newVal === 'list') {
        // 切换回列表时重新加载
        this.loadServerList();
      } else if (newVal && newVal.startsWith('ssh_')) {
        // 切换到SSH标签页时，调整终端大小并聚焦
        const sessionId = newVal.replace('ssh_', '');
        this.$nextTick(() => {
          const session = this.sshSessions.find(s => s.id === sessionId);
          if (session && session.fit && session.terminal) {
            // 延迟一点确保DOM完全渲染
            setTimeout(() => {
              session.fit.fit();
              session.terminal.focus();
            }, 50);
          }
        });
      }
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

    mainActiveTab: {
      handler(newVal) {
        // 通用的数据加载逻辑（需已认证）
        if (this.isAuthenticated) {
          this.$nextTick(() => {
            switch (newVal) {
              case 'zeabur':
                if (this.accounts.length === 0) {
                  this.fetchData();
                }
                break;
              case 'dns':
                if (this.dnsAccounts.length === 0) {
                  this.loadDnsAccounts();
                  this.loadDnsTemplates();
                }
                break;
              case 'openai':
                if (this.openaiEndpoints.length === 0) {
                  this.loadOpenaiEndpoints();
                }
                break;
              case 'server':
                if (this.serverList.length === 0) {
                  this.loadServerList();
                }
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
            case 'zeabur':
              this.fetchData();
              break;
            case 'dns':
              this.loadDnsAccounts();
              this.loadDnsTemplates();
              break;
            case 'openai':
              this.loadOpenaiEndpoints();
              break;
            case 'server':
              this.loadServerList();
              break;
            case 'antigravity':
              if (this.loadAntigravityQuotas) this.loadAntigravityQuotas();
              break;
            case 'gemini-cli':
              this.initGeminiCli();
              break;
          }
        });
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

    serverIpDisplayMode(newVal) {
      // 当 IP 显示模式改变时，重新渲染主机列表（无需重新加载数据）
      if (window.serverModule && window.serverModule.renderServerList) {
        window.serverModule.renderServerList();
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
  },

  methods: {
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

    formatDateTime,
    formatFileSize,

    /**
     * 格式化主机地址（支持打码/隐藏）
     */
    formatHost(host) {
      const mode = this.serverIpDisplayMode || 'normal';
      if (mode === 'normal') return host;
      if (mode === 'hidden') return '****';

      // 打码模式 (masked): 1.2.3.4 -> 1.2.*.*
      const parts = host.split('.');
      if (parts.length >= 2) {
        if (parts.length === 4 && parts.every(p => !isNaN(p))) {
          // IPv4
          return `${parts[0]}.${parts[1]}.*.*`;
        }
        // 域名或其他
        if (parts.length > 2) {
          return `${parts[0]}.****.${parts[parts.length - 1]}`;
        }
      }
      return host.length > 4 ? host.substring(0, 2) + '****' : '****';
    },

    // Toast 管理系统 - 使用新的独立 Toast 管理器
    showGlobalToast(message, type = 'success', duration = 3000) {
      // 使用新的toast系统
      toast[type](message, { duration });
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

      // 自动应用默认凭据
      const defaultCred = this.serverCredentials.find(c => c.is_default);
      if (defaultCred) {
        this.serverForm.username = defaultCred.username;
        this.serverForm.password = defaultCred.password || '';
        this.serverForm.authType = 'password';
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
          this.showGlobalToast('连接测试成功！', 'success');
        } else {
          this.serverModalError = '连接测试失败: ' + data.message;
          this.showGlobalToast('连接测试失败', 'error');
        }
      } catch (error) {
        console.error('测试连接失败:', error);
        this.serverModalError = '测试连接失败: ' + error.message;
        this.showGlobalToast('测试连接失败', 'error');
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
          : `/api/server/accounts/${this.serverForm.id}`;

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
          this.loadServerList();
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
              parseErrors.push(`第 ${i + 1} 行: 缺少必要字段 (name, host)`);
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
          parseErrors.push(`第 ${i + 1} 行: 解析失败 (${e.message})`);
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
            this.loadServerList();
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
     * 打开 SSH 终端(作为动态子标签页)
     */
    openSSHTerminal(server) {
      // 加载主机列表用于新建会话
      this.loadServerList();

      // 检查是否已存在该主机的会话
      const existingSession = this.sshSessions.find(s => s.server.id === server.id);
      if (existingSession) {
        // 如果已存在，直接切换到该标签页
        this.switchToSSHTab(existingSession.id);
        return;
      }

      // 创建新会话
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

      // 切换到新的SSH标签页
      this.serverCurrentTab = 'ssh_' + sessionId;

      // 等待 DOM 更新后初始化终端
      this.$nextTick(() => {
        this.initSessionTerminal(sessionId);
      });
    },

    /**
     * 切换到SSH标签页
     */
    switchToSSHTab(sessionId) {
      this.serverCurrentTab = 'ssh_' + sessionId;
      this.activeSessionId = sessionId;
      this.$nextTick(() => {
        const session = this.sshSessions.find(s => s.id === sessionId);
        if (session && session.fit) {
          session.fit.fit();
          session.terminal.focus();
        }
      });
    },

    /**
     * 关闭SSH会话（从子标签页）
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

      // 销毁终端实例
      if (session.terminal) {
        session.terminal.dispose();
      }

      // 如果当前正在显示此会话，切换到其他标签页
      if (this.serverCurrentTab === 'ssh_' + sessionId) {
        if (this.sshSessions.length > 1) {
          // 切换到其他SSH会话
          const nextSession = this.sshSessions.find(s => s.id !== sessionId);
          if (nextSession) {
            this.serverCurrentTab = 'ssh_' + nextSession.id;
            this.activeSessionId = nextSession.id;
          }
        } else {
          // 没有其他SSH会话，切回主机列表
          this.serverCurrentTab = 'list';
        }
      }

      // 从列表中移除
      this.sshSessions.splice(index, 1);

      // 更新 activeSessionId
      if (this.activeSessionId === sessionId) {
        this.activeSessionId = this.sshSessions.length > 0 ? this.sshSessions[0].id : null;
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
        session.terminal.writeln(`\x1b[1;33m正在重新连接到 ${session.server.name} (${this.formatHost(session.server.host)})...\x1b[0m`);
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

    /**
     * 获取终端主题配置 - 固定深色主题
     */
    getTerminalTheme() {
      // SSH 终端始终使用深色主题
      return {
        background: '#1e1e1e',
        foreground: '#e0e0e0',
        cursor: '#ffffff',
        black: '#000000',
        red: '#cd3131',
        green: '#0dbc79',
        yellow: '#e5e510',
        blue: '#2472c8',
        magenta: '#bc3fbc',
        cyan: '#11a8cd',
        white: '#e5e5e5',
        brightBlack: '#666666',
        brightRed: '#f14c4c',
        brightGreen: '#23d18b',
        brightYellow: '#f5f543',
        brightBlue: '#3b8eea',
        brightMagenta: '#d670d6',
        brightCyan: '#29b8db',
        brightWhite: '#e5e5e5'
      };
    },

    /**
     * 更新所有终端的主题
     */
    updateAllTerminalThemes() {
      const theme = this.getTerminalTheme();

      this.sshSessions.forEach(session => {
        if (session.terminal) {
          try {
            session.terminal.options.theme = theme;
          } catch (err) {
            console.error('更新终端主题失败:', err);
          }
        }
      });
    },

    /**
     * 设置主题观察器
     */
    setupThemeObserver() {
      // 使用 MutationObserver 监听 style 元素的变化
      const observer = new MutationObserver((mutations) => {
        mutations.forEach(mutation => {
          if (mutation.type === 'childList' || mutation.type === 'characterData') {
            // 延迟更新,避免过于频繁
            if (this.themeUpdateTimer) {
              clearTimeout(this.themeUpdateTimer);
            }
            this.themeUpdateTimer = setTimeout(() => {
              this.updateAllTerminalThemes();
            }, 100);
          }
        });
      });

      // 监听 custom-css style 元素的变化
      const customCssElement = document.getElementById('custom-css');
      if (customCssElement) {
        observer.observe(customCssElement, {
          childList: true,
          characterData: true,
          subtree: true
        });
      }

      // 同时监听 document.documentElement 的 style 属性变化
      const docObserver = new MutationObserver(() => {
        if (this.themeUpdateTimer) {
          clearTimeout(this.themeUpdateTimer);
        }
        this.themeUpdateTimer = setTimeout(() => {
          this.updateAllTerminalThemes();
        }, 100);
      });

      docObserver.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ['style']
      });

      // 保存观察器以便后续清理
      this.themeObserver = observer;
      this.docObserver = docObserver;
    },

    /**
     * 初始化会话终端 (WebSocket 版本)
     */
    initSessionTerminal(sessionId) {
      const session = this.sshSessions.find(s => s.id === sessionId);
      if (!session) return;

      const terminalContainer = document.getElementById('ssh-terminal-' + sessionId);
      if (!terminalContainer) {
        console.error('终端容器不存在');
        return;
      }

      // 清空容器
      terminalContainer.innerHTML = '';

      // 获取终端主题
      const theme = this.getTerminalTheme();

      // 创建 xterm 实例
      const terminal = new Terminal({
        cursorBlink: true,
        fontSize: 14,
        fontFamily: 'Consolas, "Courier New", monospace',
        theme: theme,
        cols: 80,
        rows: 24
      });

      // 创建 fit addon
      const fit = new FitAddon.FitAddon();
      terminal.loadAddon(fit);

      // 创建 web links addon
      const webLinksAddon = new WebLinksAddon.WebLinksAddon();
      terminal.loadAddon(webLinksAddon);

      // 打开终端
      terminal.open(terminalContainer);
      fit.fit();

      // 保存到会话
      session.terminal = terminal;
      session.fit = fit;

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
              terminal.writeln(`\x1b[1;32m${msg.message}\x1b[0m`);
              terminal.writeln('');
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

      // 监听终端输入，发送到 WebSocket
      terminal.onData(data => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({
            type: 'input',
            data: data
          }));
        }
      });

      // 监听窗口大小变化
      const resizeHandler = () => {
        if (session.fit) {
          session.fit.fit();
          // 通知服务器终端大小变化
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({
              type: 'resize',
              cols: terminal.cols,
              rows: terminal.rows
            }));
          }
        }
      };
      window.addEventListener('resize', resizeHandler);
      session.resizeHandler = resizeHandler;
    },

    /**
     * 切换会话
     */
    switchSession(sessionId) {
      this.activeSessionId = sessionId;
      this.$nextTick(() => {
        const session = this.sshSessions.find(s => s.id === sessionId);
        if (session && session.fit) {
          session.fit.fit();
          session.terminal.focus();
        }
      });
    },

    /**
     * 关闭单个会话
     */
    closeSession(sessionId) {
      const index = this.sshSessions.findIndex(s => s.id === sessionId);
      if (index === -1) return;

      const session = this.sshSessions[index];

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

      // 销毁终端实例
      if (session.terminal) {
        session.terminal.dispose();
      }

      // 从列表中移除
      this.sshSessions.splice(index, 1);

      // 如果关闭的是当前会话，切换到其他会话
      if (this.activeSessionId === sessionId) {
        if (this.sshSessions.length > 0) {
          this.activeSessionId = this.sshSessions[0].id;
        } else {
          this.activeSessionId = null;
          this.showSSHTerminalModal = false;
        }
      }
    },

    /**
     * 显示新建会话选择框
     */
    showAddSessionModal() {
      this.loadServerList();
      this.showAddSessionSelectModal = true;
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
      this.activeSessionId = sessionId;

      // 切换到新的SSH标签页
      this.serverCurrentTab = 'ssh_' + sessionId;

      this.$nextTick(() => {
        this.initSessionTerminal(sessionId);
      });
    },

    /**
     * 关闭 SSH 终端（关闭所有会话）
     */
    closeSSHTerminal() {
      // 关闭所有会话
      for (const session of this.sshSessions) {
        // 断开 SSH 连接
        fetch('/api/server/ssh/disconnect', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ serverId: session.server.id })
        }).catch(err => console.error('断开连接失败:', err));

        // 移除 resize 监听器
        if (session.resizeHandler) {
          window.removeEventListener('resize', session.resizeHandler);
        }

        // 销毁终端实例
        if (session.terminal) {
          session.terminal.dispose();
        }
      }

      // 重置状态
      this.sshSessions = [];
      this.activeSessionId = null;
      this.showSSHTerminalModal = false;
      this.sshTerminalServer = null;
      this.sshTerminal = null;
      this.sshTerminalFit = null;
      this.sshCommandHistory = [];
      this.sshHistoryIndex = -1;
      this.sshCurrentCommand = '';
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

        // 如果有缓存数据，立即使用（零等待）
        if (server.cached_info && !server.info) {
          server.info = {
            system: server.cached_info.system,
            cpu: server.cached_info.cpu,
            memory: server.cached_info.memory,
            disk: server.cached_info.disk,
            docker: server.cached_info.docker
          };
          // 后台静默刷新最新数据（不显示 loading）
          this.loadServerInfo(serverId);
        } else if (!server.info && !server.error) {
          // 无缓存，显示 loading 并加载
          server.loading = true;
          await this.loadServerInfo(serverId);
          server.loading = false;
        }
      }
    },

    /**
     * 加载主机详细信息
     */
    async loadServerInfo(serverId) {
      const server = this.serverList.find(s => s.id === serverId);
      if (!server) return;

      try {
        const response = await fetch('/api/server/info', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ serverId })
        });

        const data = await response.json();

        if (data.success) {
          server.info = {
            system: data.system,
            cpu: data.cpu,
            memory: data.memory,
            disk: data.disk,
            docker: data.docker
          };
          server.error = null;
        } else {
          server.error = data.error || '加载失败';
          server.info = null;
        }
      } catch (error) {
        console.error('加载主机信息失败:', error);
        server.error = error.message;
        server.info = null;
      }
    },

    /**
     * 刷新主机信息
     */
    async refreshServerInfo(serverId) {
      const server = this.serverList.find(s => s.id === serverId);
      if (server) {
        server.info = null;
        server.error = null;
        await this.loadServerInfo(serverId);
        showToast('正在刷新主机信息...', 'info');
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
        // 成功加载后启动或刷新轮询
        this.startServerPolling();
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
      this.stopServerPolling();
      if (!this.serverPollingEnabled) return;

      const interval = Math.max(10000, (this.monitorConfig.interval || 60) * 1000);
      console.log('启动主机状态轮询，间隔:', interval / 1000, '秒');

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
        // 只有在可见、已认证且在对应标签页时才执行
        if (document.visibilityState === 'visible' && this.isAuthenticated && this.mainActiveTab === 'server' && this.serverCurrentTab === 'list') {
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
     * 手动探测所有主机
     */
    async probeAllServers() {
      this.probeStatus = 'loading';

      try {
        const response = await fetch('/api/server/check-all', {
          method: 'POST'
        });

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

      // 3秒后重置状态
      setTimeout(() => {
        this.probeStatus = '';
      }, 3000);
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

    // 整合所有模块的方法
    ...authMethods,
    ...zeaburMethods,
    ...dnsMethods,
    ...openaiMethods,
    ...antigravityMethods,
    ...geminiCliMethods,
    ...settingsMethods,
    ...transitionsMethods,
    ...systemLogsMethods,
    formatDateTime,
  }
}).mount('#app');
