/**
 * API Monitor - 主应用模块
 * 整合所有功能模块，初始化 Vue 应用
 */

// 导入功能模块
import { authMethods } from './modules/auth.js';
import { zeaburMethods } from './modules/zeabur.js';
import { dnsMethods } from './modules/dns.js';
import { openaiMethods } from './modules/openai.js';
import { settingsMethods } from './modules/settings.js';
import { transitionsMethods } from './modules/transitions.js';
import { initServerModule } from './modules/server.js';

// 获取 Vue
const { createApp } = Vue;

// 创建并配置 Vue 应用
createApp({
  data() {
    return {
      // Zeabur 相关
      accounts: [],
      loading: false,
      lastUpdate: '--:--:--',
      managedAccounts: [],
      projectCosts: {},
      newAccount: {
        name: '',
        token: '',
        balance: ''
      },
      addingAccount: false,
      addAccountError: '',
      addAccountSuccess: '',
      opacity: 39,
      expandedAccounts: {},
      refreshInterval: null,
      refreshing: false,
      lastFetchAt: 0,
      minFetchInterval: 2000,

      // 密码验证
      isAuthenticated: false,
      isCheckingAuth: true, // 添加认证检查状态
      showLoginModal: false,
      showSetPasswordModal: false,
      loginPassword: '',
      loginError: '',
      setPassword: '',
      setPasswordConfirm: '',
      setPasswordError: '',

      // 批量添加
      batchAccounts: '',
      maskedBatchAccounts: '',
      batchAddError: '',
      batchAddSuccess: '',
      showAddZeaburAccountModal: false,

      // 日志模态框
      showLogsModal: false,
      logsModalTitle: '',
      logsModalInfo: {},
      logsContent: '',
      logsLoading: false,
      logsAutoScroll: true,
      logsFullscreen: false,
      logsScrollTimer: null,
      logsRealTime: true,
      logsRealTimeTimer: null,
      logsCurrentAccount: null,
      logsCurrentProject: null,
      logsCurrentService: null,

      // 刷新倒计时
      refreshCountdown: 30,
      refreshProgress: 100,
      countdownInterval: null,
      dataRefreshPaused: false,

      // 主标签页
      mainActiveTab: 'zeabur',
      zeaburCurrentTab: 'monitor',

      // DNS 管理相关
      dnsToast: { show: false, message: '', type: 'success' },
      dnsCurrentTab: 'dns',
      dnsAccounts: [],
      dnsSelectedAccountId: '',
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
      dnsZones: [],
      dnsSelectedZoneId: '',
      dnsSelectedZoneName: '',
      dnsLoadingZones: false,
      dnsRecords: [],
      dnsLoadingRecords: false,
      showDnsRecordModal: false,
      dnsEditingRecord: null,
      dnsRecordForm: { type: 'A', name: '', content: '', ttl: 1, proxied: false, priority: 10 },
      dnsRecordFormError: '',
      dnsSavingRecord: false,
      dnsRecordTypes: ['A', 'AAAA', 'CNAME', 'TXT', 'MX', 'NS', 'SRV', 'CAA'],
      dnsSelectedRecords: [],
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
      openaiEndpoints: [],
      openaiLoading: false,
      openaiRefreshing: false,
      openaiCurrentTab: 'endpoints',
      openaiToast: { show: false, message: '', type: 'success' },
      showOpenaiEndpointModal: false,
      openaiEditingEndpoint: null,
      openaiEndpointForm: { name: '', baseUrl: '', apiKey: '', notes: '' },
      openaiEndpointFormError: '',
      openaiSaving: false,
      openaiExpandedEndpoints: {},
      openaiBatchText: '',
      openaiBatchError: '',
      openaiBatchSuccess: '',
      openaiAdding: false,

      // 服务器管理相关
      serverCurrentTab: 'list',
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
      monitorConfig: {
        interval: 60,
        timeout: 10,
        logRetentionDays: 7
      },
      monitorConfigSaving: false,
      monitorConfigError: '',
      monitorConfigSuccess: '',
      serverList: [],
      monitorLogs: [],
      monitorLogsLoading: false,
      logFilter: {
        serverId: '',
        status: ''
      },
      logPage: 1,
      logPageSize: 50,

      // 文件管理器相关
      showFileManagerModal: false,
      fileManagerServer: null,
      fileManagerPath: '/',
      fileManagerFiles: [],
      fileManagerLoading: false,
      fileManagerError: '',

      // 模块可见性控制
      moduleVisibility: {
        zeabur: true,
        dns: true,
        openai: true,
        server: true
      },
      moduleOrder: ['zeabur', 'dns', 'openai', 'server'],
      draggedIndex: null,

      // 设置模态框
      showSettingsModal: false,
      newPassword: '',
      confirmPassword: '',
      passwordError: '',
      passwordSuccess: '',
      customCss: '',
      customCssError: '',
      customCssSuccess: '',

      // 全局Toast
      globalToast: { show: false, message: '', type: 'success' },

      // 自定义对话框
      customDialog: {
        show: false,
        title: '',
        message: '',
        icon: '',
        confirmText: '',
        cancelText: '',
        confirmClass: '',
        onConfirm: null,
        onCancel: null
      }
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

    // 获取模块图标
    getModuleIcon() {
      return (module) => {
        const icons = {
          zeabur: 'fa-cloud',
          dns: 'fa-globe',
          openai: 'fa-robot',
          server: 'fa-server'
        };
        return icons[module] || 'fa-cube';
      };
    },

    // 获取模块名称
    getModuleName() {
      return (module) => {
        const names = {
          zeabur: 'Zeabur 监控',
          dns: 'DNS 管理',
          openai: 'OpenAPI',
          server: '服务器管理'
        };
        return names[module] || module;
      };
    },

    // 文件管理器面包屑
    fileManagerBreadcrumbs() {
      const segments = [];
      const parts = this.fileManagerPath.split('/').filter(p => p);
      let currentPath = '';

      segments.push({ name: '/', path: '/' });

      for (const part of parts) {
        currentPath += '/' + part;
        segments.push({ name: part, path: currentPath });
      }

      return segments;
    }
  },

  async mounted() {
    // 保存 Vue 实例到全局，供其他模块使用
    window.vueApp = this;

    // 加载模块可见性和顺序设置
    this.loadModuleSettings();

    try {
      // 检查服务器是否已设置密码
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
    opacity(newVal) {
      localStorage.setItem('card_opacity', newVal);
      this.updateOpacity();
    },

    serverCurrentTab(newVal) {
      if (newVal === 'management') {
        this.loadMonitorConfig();
        this.loadServerList();
        this.loadMonitorLogs();
      }
    },

    showSettingsModal(newVal) {
      if (newVal) {
        document.body.classList.add('modal-open');
        this.$nextTick(() => this.focusModalOverlay('.settings-sidebar'));
      } else {
        document.body.classList.remove('modal-open');
      }
    },

    showLogsModal(newVal) {
      if (newVal) {
        this.$nextTick(() => {
          this.setupAutoScroll();
          this.setupHorizontalScrollbar();
          if (this.logsRealTime) {
            this.startRealTimeRefresh();
          }
          const modalOverlay = document.querySelector('.modal-overlay.logs-fullscreen-overlay, .modal-overlay');
          if (modalOverlay) {
            modalOverlay.focus();
          }
          document.body.style.overflow = 'hidden';
        });
      } else {
        if (this.logsScrollTimer) {
          clearInterval(this.logsScrollTimer);
          this.logsScrollTimer = null;
        }
        this.stopRealTimeRefresh();
        this.logsRealTime = false;
        document.body.style.overflow = '';
      }
    },

    logsAutoScroll(newVal) {
      this.setupAutoScroll();
    },

    logsRealTime(newVal) {
      if (newVal) {
        this.startRealTimeRefresh();
      } else {
        this.stopRealTimeRefresh();
      }
    },

    mainActiveTab(newVal) {
      // 当切换到服务器管理模块时，初始化模块
      if (newVal === 'server') {
        this.$nextTick(() => {
          initServerModule();
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

    showSSHTerminalModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showAddSessionSelectModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
      }
    },

    showFileManagerModal(newVal) {
      if (newVal) {
        this.$nextTick(() => this.focusModalOverlay());
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
  },

  methods: {
    // 通用工具函数
    showGlobalToast(message, type = 'success') {
      this.globalToast = { show: true, message, type };
      setTimeout(() => {
        this.globalToast.show = false;
      }, 3000);
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

    // ==================== 服务器管理方法 ====================
    openAddServerModal() {
      this.serverModalMode = 'add';
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
              authType: server.auth_type || 'password',
              password: '', // 不显示原密码
              privateKey: '', // 不显示原私钥
              passphrase: '',
              tagsInput: Array.isArray(server.tags) ? server.tags.join(',') : '',
              description: server.description || ''
            };
            this.showServerModal = true;
          } else {
            this.showGlobalToast('服务器不存在', 'error');
          }
        } else {
          this.showGlobalToast('加载服务器信息失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('加载服务器信息失败:', error);
        this.showGlobalToast('加载服务器信息失败', 'error');
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
            authType: this.serverForm.authType,
            password: this.serverForm.password,
            privateKey: this.serverForm.privateKey,
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
          authType: this.serverForm.authType,
          tags: tags,
          description: this.serverForm.description
        };

        // 只在有值时才发送密码/私钥
        if (this.serverForm.authType === 'password' && this.serverForm.password) {
          payload.password = this.serverForm.password;
        }
        if (this.serverForm.authType === 'privateKey' && this.serverForm.privateKey) {
          payload.privateKey = this.serverForm.privateKey;
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
            this.serverModalMode === 'add' ? '服务器添加成功' : '服务器更新成功',
            'success'
          );
          this.closeServerModal();

          // 刷新服务器列表
          if (window.serverModule && window.serverModule.loadServers) {
            window.serverModule.loadServers();
          }
        } else {
          this.serverModalError = data.error || '保存失败';
          this.showGlobalToast('保存失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('保存服务器失败:', error);
        this.serverModalError = '保存失败: ' + error.message;
        this.showGlobalToast('保存服务器失败', 'error');
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
            this.importModalError = '文件格式错误：应为服务器数组';
            return;
          }

          // 验证数据格式
          const validServers = data.filter(server => {
            return server.name && server.host && server.username;
          });

          if (validServers.length === 0) {
            this.importModalError = '文件中没有有效的服务器配置';
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
          this.showGlobalToast(`成功导入 ${data.imported} 台服务器`, 'success');
          this.closeImportServerModal();

          // 刷新服务器列表
          if (window.serverModule && window.serverModule.loadServers) {
            window.serverModule.loadServers();
          }
        } else {
          this.importModalError = '导入失败: ' + data.error;
          this.showGlobalToast('导入失败', 'error');
        }
      } catch (error) {
        console.error('导入服务器失败:', error);
        this.importModalError = '导入失败: ' + error.message;
        this.showGlobalToast('导入服务器失败', 'error');
      } finally {
        this.importModalSaving = false;
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
     * 打开 SSH 终端
     */
    openSSHTerminal(server) {
      // 加载服务器列表用于新建会话
      this.loadServerList();

      // 创建新会话
      const sessionId = 'session_' + Date.now();
      const session = {
        id: sessionId,
        server: server,
        terminal: null,
        fit: null,
        history: [],
        historyIndex: -1
      };

      this.sshSessions.push(session);
      this.activeSessionId = sessionId;
      this.showSSHTerminalModal = true;

      // 等待 DOM 更新后初始化终端
      this.$nextTick(() => {
        this.initSessionTerminal(sessionId);
      });
    },

    /**
     * 初始化会话终端
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

      // 创建 xterm 实例
      const terminal = new Terminal({
        cursorBlink: true,
        fontSize: 14,
        fontFamily: 'Consolas, "Courier New", monospace',
        theme: {
          background: '#1e1e1e',
          foreground: '#d4d4d4',
          cursor: '#d4d4d4',
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
        },
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

      // 显示欢迎信息
      terminal.writeln(`\x1b[1;32m连接到服务器: ${session.server.name}\x1b[0m`);
      terminal.writeln(`\x1b[1;36m地址: ${session.server.host}:${session.server.port}\x1b[0m`);
      terminal.writeln('');
      terminal.write('$ ');

      // 监听输入
      let currentLine = '';
      terminal.onData(data => {
        // 处理特殊按键
        if (data === '\r') { // Enter
          terminal.write('\r\n');
          if (currentLine.trim()) {
            this.executeSessionCommand(sessionId, currentLine.trim());
            session.history.push(currentLine.trim());
            session.historyIndex = session.history.length;
          }
          currentLine = '';
          terminal.write('$ ');
        } else if (data === '\u007F') { // Backspace
          if (currentLine.length > 0) {
            currentLine = currentLine.slice(0, -1);
            terminal.write('\b \b');
          }
        } else if (data === '\u001b[A') { // Up arrow
          if (session.historyIndex > 0) {
            // 清除当前行
            for (let i = 0; i < currentLine.length; i++) {
              terminal.write('\b \b');
            }
            // 显示历史命令
            session.historyIndex--;
            currentLine = session.history[session.historyIndex];
            terminal.write(currentLine);
          }
        } else if (data === '\u001b[B') { // Down arrow
          if (session.historyIndex < session.history.length - 1) {
            // 清除当前行
            for (let i = 0; i < currentLine.length; i++) {
              terminal.write('\b \b');
            }
            // 显示历史命令
            session.historyIndex++;
            currentLine = session.history[session.historyIndex];
            terminal.write(currentLine);
          } else if (session.historyIndex === session.history.length - 1) {
            // 清除当前行
            for (let i = 0; i < currentLine.length; i++) {
              terminal.write('\b \b');
            }
            session.historyIndex = session.history.length;
            currentLine = '';
          }
        } else if (data === '\u0003') { // Ctrl+C
          terminal.write('^C\r\n$ ');
          currentLine = '';
        } else {
          // 普通字符
          currentLine += data;
          terminal.write(data);
        }
      });

      // 监听窗口大小变化
      const resizeHandler = () => {
        if (session.fit) {
          session.fit.fit();
        }
      };
      window.addEventListener('resize', resizeHandler);
      session.resizeHandler = resizeHandler;
    },

    /**
     * 执行会话 SSH 命令
     */
    async executeSessionCommand(sessionId, command) {
      const session = this.sshSessions.find(s => s.id === sessionId);
      if (!session) return;

      try {
        const response = await fetch('/api/server/ssh/exec', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            serverId: session.server.id,
            command: command
          })
        });

        const data = await response.json();

        if (data.success) {
          // 显示命令输出
          if (data.data.stdout) {
            session.terminal.write(data.data.stdout);
          }
          if (data.data.stderr) {
            session.terminal.write(`\x1b[1;31m${data.data.stderr}\x1b[0m`);
          }
        } else {
          session.terminal.write(`\x1b[1;31m错误: ${data.error}\x1b[0m\r\n`);
        }
      } catch (error) {
        console.error('执行命令失败:', error);
        session.terminal.write(`\x1b[1;31m执行命令失败: ${error.message}\x1b[0m\r\n`);
      }
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
     * 为指定服务器添加新会话
     */
    addSessionForServer(server) {
      this.showAddSessionSelectModal = false;

      const sessionId = 'session_' + Date.now();
      const session = {
        id: sessionId,
        server: server,
        terminal: null,
        fit: null,
        history: [],
        historyIndex: -1
      };

      this.sshSessions.push(session);
      this.activeSessionId = sessionId;

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

    // ==================== 文件管理器方法 ====================

    /**
     * 打开文件管理器
     */
    openFileManager(server) {
      this.fileManagerServer = server;
      this.fileManagerPath = '/';
      this.fileManagerFiles = [];
      this.fileManagerError = '';
      this.showFileManagerModal = true;
      this.loadFileList('/');
    },

    /**
     * 关闭文件管理器
     */
    closeFileManager() {
      this.showFileManagerModal = false;
      this.fileManagerServer = null;
      this.fileManagerPath = '/';
      this.fileManagerFiles = [];
      this.fileManagerError = '';
    },

    /**
     * 加载文件列表
     */
    async loadFileList(path) {
      if (!this.fileManagerServer) return;

      this.fileManagerLoading = true;
      this.fileManagerError = '';

      try {
        const response = await fetch('/api/server/sftp/list', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            serverId: this.fileManagerServer.id,
            path: path
          })
        });

        const data = await response.json();

        if (data.success) {
          this.fileManagerPath = data.path || path;
          // 排序：目录在前，文件在后，然后按名称排序
          this.fileManagerFiles = (data.files || []).sort((a, b) => {
            if (a.type === 'directory' && b.type !== 'directory') return -1;
            if (a.type !== 'directory' && b.type === 'directory') return 1;
            return a.name.localeCompare(b.name);
          });
        } else {
          this.fileManagerError = data.error || '加载文件列表失败';
        }
      } catch (error) {
        console.error('加载文件列表失败:', error);
        this.fileManagerError = error.message;
      } finally {
        this.fileManagerLoading = false;
      }
    },

    /**
     * 刷新文件列表
     */
    refreshFileList() {
      this.loadFileList(this.fileManagerPath);
    },

    /**
     * 导航到父目录
     */
    navigateToParent() {
      if (this.fileManagerPath === '/') return;
      const parts = this.fileManagerPath.split('/').filter(p => p);
      parts.pop();
      const parentPath = '/' + parts.join('/');
      this.loadFileList(parentPath || '/');
    },

    /**
     * 导航到主目录
     */
    navigateToHome() {
      this.loadFileList('/');
    },

    /**
     * 导航到指定路径
     */
    navigateToPath(path) {
      this.loadFileList(path);
    },

    /**
     * 处理文件双击
     */
    handleFileDoubleClick(file) {
      if (file.type === 'directory') {
        const newPath = this.fileManagerPath === '/'
          ? '/' + file.name
          : this.fileManagerPath + '/' + file.name;
        this.loadFileList(newPath);
      }
    },

    /**
     * 获取文件图标
     */
    getFileIcon(file) {
      if (file.type === 'directory') return 'fa-folder';

      const ext = file.name.split('.').pop().toLowerCase();
      const codeExts = ['js', 'ts', 'py', 'java', 'cpp', 'c', 'h', 'css', 'html', 'vue', 'jsx', 'tsx', 'json', 'xml', 'yml', 'yaml', 'sh', 'bash', 'php', 'rb', 'go', 'rs', 'sql'];
      const imageExts = ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'svg', 'ico', 'webp'];
      const archiveExts = ['zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz'];

      if (codeExts.includes(ext)) return 'fa-file-code';
      if (imageExts.includes(ext)) return 'fa-file-image';
      if (archiveExts.includes(ext)) return 'fa-file-archive';

      return 'fa-file';
    },

    /**
     * 格式化文件大小
     */
    formatFileSize(bytes) {
      if (!bytes || bytes === 0) return '0 B';
      const units = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.floor(Math.log(bytes) / Math.log(1024));
      return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
    },

    /**
     * 格式化文件时间
     */
    formatFileTime(timestamp) {
      if (!timestamp) return '-';
      const date = new Date(timestamp);
      return date.toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit'
      });
    },

    /**
     * 格式化权限
     */
    formatPermissions(rights) {
      if (!rights) return '-';
      const u = rights.user || '';
      const g = rights.group || '';
      const o = rights.other || '';
      return u + g + o;
    },

    /**
     * 显示创建文件夹对话框
     */
    async showCreateFolderDialog() {
      const folderName = await this.showPrompt({
        title: '新建文件夹',
        message: '请输入文件夹名称',
        placeholder: '文件夹名称'
      });

      if (folderName) {
        this.createFolder(folderName);
      }
    },

    /**
     * 创建文件夹
     */
    async createFolder(folderName) {
      if (!this.fileManagerServer || !folderName) return;

      try {
        const newPath = this.fileManagerPath === '/'
          ? '/' + folderName
          : this.fileManagerPath + '/' + folderName;

        const response = await fetch('/api/server/sftp/mkdir', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            serverId: this.fileManagerServer.id,
            path: newPath
          })
        });

        const data = await response.json();

        if (data.success) {
          this.showGlobalToast('文件夹创建成功', 'success');
          this.refreshFileList();
        } else {
          this.showGlobalToast('创建失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('创建文件夹失败:', error);
        this.showGlobalToast('创建文件夹失败', 'error');
      }
    },

    /**
     * 显示重命名对话框
     */
    async showRenameDialog(file) {
      const newName = await this.showPrompt({
        title: '重命名',
        message: '请输入新名称',
        placeholder: file.name
      });

      if (newName && newName !== file.name) {
        this.renameFile(file, newName);
      }
    },

    /**
     * 重命名文件
     */
    async renameFile(file, newName) {
      if (!this.fileManagerServer || !newName) return;

      try {
        const oldPath = this.fileManagerPath === '/'
          ? '/' + file.name
          : this.fileManagerPath + '/' + file.name;
        const newPath = this.fileManagerPath === '/'
          ? '/' + newName
          : this.fileManagerPath + '/' + newName;

        const response = await fetch('/api/server/sftp/rename', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            serverId: this.fileManagerServer.id,
            oldPath,
            newPath
          })
        });

        const data = await response.json();

        if (data.success) {
          this.showGlobalToast('重命名成功', 'success');
          this.refreshFileList();
        } else {
          this.showGlobalToast('重命名失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('重命名失败:', error);
        this.showGlobalToast('重命名失败', 'error');
      }
    },

    /**
     * 删除文件或目录
     */
    async deleteFileItem(file) {
      const confirmed = await this.showConfirm({
        title: '确认删除',
        message: `确定要删除 "${file.name}" 吗？${file.type === 'directory' ? '（包括所有子文件）' : ''}`,
        icon: 'fa-trash',
        confirmText: '删除',
        confirmClass: 'btn-danger'
      });

      if (!confirmed) return;

      try {
        const filePath = this.fileManagerPath === '/'
          ? '/' + file.name
          : this.fileManagerPath + '/' + file.name;

        const response = await fetch('/api/server/sftp/delete', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            serverId: this.fileManagerServer.id,
            path: filePath,
            isDirectory: file.type === 'directory'
          })
        });

        const data = await response.json();

        if (data.success) {
          this.showGlobalToast('删除成功', 'success');
          this.refreshFileList();
        } else {
          this.showGlobalToast('删除失败: ' + data.error, 'error');
        }
      } catch (error) {
        console.error('删除失败:', error);
        this.showGlobalToast('删除失败', 'error');
      }
    },

    /**
     * 下载文件
     */
    async downloadFile(file) {
      this.showGlobalToast('下载功能暂不支持浏览器直接下载，请使用 SSH 终端', 'info');
    },

    /**
     * 处理文件上传
     */
    async handleFileUpload(event) {
      const files = event.target.files;
      if (!files || files.length === 0) return;

      this.showGlobalToast('文件上传功能开发中...', 'info');

      // 清空文件输入
      if (this.$refs.uploadFileInput) {
        this.$refs.uploadFileInput.value = '';
      }
    },

    async loadMonitorConfig() {
      try {
        const response = await fetch('/api/server/monitor/config');
        const data = await response.json();

        if (data.success) {
          this.monitorConfig = {
            interval: data.data.interval_seconds || 60,
            timeout: data.data.timeout_seconds || 10,
            logRetentionDays: data.data.log_retention_days || 7
          };
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
            intervalSeconds: this.monitorConfig.interval,
            timeoutSeconds: this.monitorConfig.timeout,
            logRetentionDays: this.monitorConfig.logRetentionDays
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

    async loadServerList() {
      try {
        const response = await fetch('/api/server/accounts');
        const data = await response.json();

        if (data.success) {
          this.serverList = data.data;
        }
      } catch (error) {
        console.error('加载服务器列表失败:', error);
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

    formatDateTime(dateString) {
      if (!dateString) return '-';
      const date = new Date(dateString);
      return date.toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
      });
    },

    // 整合所有模块的方法
    ...authMethods,
    ...zeaburMethods,
    ...dnsMethods,
    ...openaiMethods,
    ...settingsMethods,
    ...transitionsMethods
  }
}).mount('#app');
