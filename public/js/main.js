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

      // 模块可见性控制
      moduleVisibility: {
        zeabur: true,
        dns: true,
        openai: true
      },
      moduleOrder: ['zeabur', 'dns', 'openai'],
      draggedIndex: null,

      // 设置模态框
      showSettingsModal: false,
      newPassword: '',
      confirmPassword: '',
      passwordError: '',
      passwordSuccess: '',

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
    }
  },

  async mounted() {
    // 加载模块可见性和顺序设置
    this.loadModuleSettings();

    // 检查服务器是否已设置密码
    const hasPasswordResponse = await fetch('/api/check-password');
    const { hasPassword } = await hasPasswordResponse.json();

    if (!hasPassword) {
      // 首次使用，显示设置密码界面
      this.showSetPasswordModal = true;
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
        return;
      }
    }

    // 需要输入密码
    this.showLoginModal = true;
  },

  watch: {
    opacity(newVal) {
      localStorage.setItem('card_opacity', newVal);
      this.updateOpacity();
    },

    showSettingsModal(newVal) {
      if (newVal) {
        document.body.classList.add('modal-open');
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

    maskEmail(email) {
      if (!email || !email.includes('@')) return email;
      const [local, domain] = email.split('@');
      if (local.length <= 14) return email;
      const masked = local.substring(0, 2) + '*'.repeat(local.length - 4) + local.substring(local.length - 2);
      return masked + '@' + domain;
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
