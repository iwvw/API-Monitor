/**
 * 通用工具方法模块
 * 负责全局初始化、Toast、对话框、格式化、剪贴板等通用功能
 */

import { toast } from './toast.js';
import { formatDateTime, formatFileSize, maskAddress, formatRegion } from './utils.js';
import { store, MODULE_CONFIG } from '../store.js';

/**
 * 通用工具方法集合
 */
export const commonMethods = {
  // ==================== 全局初始化 ====================

  initGlobalTooltipEngine() {
    // 避免重复初始化
    if (document.querySelector('.system-tooltip')) return;

    const tooltipEl = document.createElement('div');
    tooltipEl.className = 'system-tooltip';
    document.body.appendChild(tooltipEl);

    window.addEventListener('mouseover', e => {
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
        let left = rect.left + rect.width / 2 - tooltipRect.width / 2;

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

    window.addEventListener('mouseout', e => {
      if (e.target.closest('[data-tooltip]')) {
        tooltipEl.classList.remove('visible');
      }
    });
  },

  initGlobalImageProxy() {
    window.addEventListener(
      'click',
      e => {
        const target = e.target;
        if (
          target.tagName === 'IMG' &&
          (target.classList.contains('msg-inline-image') || target.closest('.chat-history-compact'))
        ) {
          const link = target.closest('a');
          if (link) e.preventDefault();
          this.openImagePreview(target.src);
        }
      },
      true
    );
  },

  initGlobalKeyListeners() {
    window.addEventListener('keydown', e => {
      // 1. Esc 快捷键：关闭最上层模态框
      if (e.key === 'Escape') {
        // 如果有自定义对话框 (Confirm/Prompt)，优先处理取消逻辑
        if (this.customDialog && this.customDialog.show) {
          if (this.customDialog.onCancel) {
            this.customDialog.onCancel();
          } else if (this.customDialog.onConfirm) {
            this.customDialog.onConfirm(); // Alert 只有确认
          }
          return;
        }

        // 关闭所有已知模态框
        this.closeAllModals();
      }

      // 2. Enter 快捷键：在对话框显示时确认
      if (e.key === 'Enter') {
        if (this.customDialog && this.customDialog.show) {
          // 排除 textarea，避免在输入消息时回车触发对话框确认
          if (e.target.tagName !== 'TEXTAREA') {
            this.customDialog.onConfirm();
            e.preventDefault();
          }
        } else if (this.openaiHealthCheckModal && !this.openaiModelHealthBatchLoading) {
          // 健康检测弹窗中的 Enter 逻辑
          if (e.target.tagName !== 'INPUT') {
            this.startOpenaiHealthCheck();
            e.preventDefault();
          }
        }
      }
    });
  },

  // 关闭所有已打开的模态框
  closeAllModals() {
    // 基础核心模态框
    this.showSettingsModal = false;
    this.showServerModal = false;
    this.showImportServerModal = false;
    this.showDockerModal = false;
    this.showSSHTerminalModal = false;
    this.showImagePreviewModal = false;
    this.showAddCredentialModal = false;

    // 功能组件
    if (this.logViewer) this.logViewer.visible = false;
    if (this.customDialog) this.customDialog.show = false;

    // 各模块账号/资源模态框
    this.showAddZeaburAccountModal = false;
    this.showAddKoyebAccountModal = false;
    this.showAddFlyAccountModal = false;
    this.showAddDnsAccountModal = false;
    this.showEditDnsAccountModal = false;
    this.showDnsRecordModal = false;
    this.showDnsTemplateModal = false;
    this.showOpenaiEndpointModal = false;
    this.showAntigravityAccountModal = false;
    this.showAddSessionSelectModal = false;
    this.showAntigravityLogDetailModal = false;
    this.showGeminiCliLogDetailModal = false;
    this.showGeminiCliAccountModal = false;
    this.showAntigravityManualModal = false;
    this.showNewWorkerModal = false;
    this.showWorkerRoutesModal = false;
    this.showWorkerDomainsModal = false;
    this.showPagesDeploymentsModal = false;
    this.showPagesDomainsModal = false;
    this.showAddZoneModal = false;
    this.showTotpModal = false;
    this.showTotpImportModal = false;
    this.showAntigravityAccountModal = false;
    this.showImagePreviewModal = false;
    this.openaiHealthCheckModal = false;
    this.showHChatSettingsModal = false;
  },

  initMobileGestures() {
    // 为移动端所有可交互元素添加震动反馈
    let lastVibrateTime = 0;

    // 全局交互反馈处理函数
    const handleInteraction = e => {
      // 严重防抖：防止 touchstart 和 click 同时触发导致的双倍震动或震动失效
      const now = Date.now();
      if (now - lastVibrateTime < 150) return;

      if (window.innerWidth > 900) return;

      // 检查振动可用性
      const isVibrateAvailable = window.navigator && window.navigator.vibrate;
      // 检查开关：优先从 store 获取，默认开启
      const isVibrateEnabled = store.vibrationEnabled !== false;

      if (!isVibrateAvailable || !isVibrateEnabled) return;

      // 更新最后一次触发时间
      lastVibrateTime = now;

      // 震动分级逻辑
      // 这里的 target 用于判断震动强度，不用于过滤点击
      const target = e.target;

      const isHeavy = target.closest(
        '.btn-danger, .modal-close, [data-action="delete"], [data-action="remove"]'
      );
      const isLight = target.closest(
        '.tab-btn, .nav-item, .chip-btn, .tag, .mfp-lyric-line, .paas-tab, .mini-player-item'
      );

      if (isHeavy) {
        window.navigator.vibrate(50);
      } else if (isLight) {
        window.navigator.vibrate(20);
      } else {
        window.navigator.vibrate(30);
      }
    };

    // 仅监听 click，确保点击行为的一致性，避免滑动时触发震动
    window.addEventListener('click', handleInteraction, { capture: true });

    // 点击空白处关闭底栏展开菜单
    document.addEventListener('click', (e) => {
      // 如果没有展开的菜单，直接返回
      if (!store.navGroupExpanded) return;

      // 检查点击是否在菜单区域内
      const isInsideDropdown = e.target.closest('.nav-group-dropdown');
      const isInsideGroupBtn = e.target.closest('.nav-group-btn');

      // 如果点击的不是菜单按钮也不是下拉菜单内部，则关闭
      if (!isInsideDropdown && !isInsideGroupBtn) {
        store.navGroupExpanded = null;
      }
    });

    console.log('[System] Mobile interaction feedback (click) initialized');


    /*
        let touchStartX = null;
        let touchStartY = null;
        const swipeThreshold = 80;

        window.addEventListener('touchstart', (e) => {
            if (window.innerWidth > 768 || this.isAnyModalOpen) return;

            // 排除需要横向滚动的区域，防止滑动切换标签页
            const scrollableSelectors = [
                '#monaco-editor-container',
                '.log-stream-container',
                '.table-container',
                '.table-wrapper',
                'table',
                '.overflow-x-auto',
                '.horizontal-scroll',
                '.dns-records-table',
                '.data-table',
                '.scroll-container',
                '[style*="overflow-x"]',
                '[style*="overflow: auto"]',
                '.xterm',
                '.terminal'
            ];

            if (scrollableSelectors.some(sel => e.target.closest(sel))) return;

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
                    // 震动反馈 (如果支持)
                    if (navigator.vibrate) {
                        navigator.vibrate(15);
                    }
                    this.handleTabSwitch(visibleModules[nextIndex]);
                }
            }
        }, { passive: true });
        */
  },

  // ==================== 图片预览 ====================

  openImagePreview(url) {
    if (!url) return;
    this.previewImageUrl = url;
    this.showImagePreviewModal = true;
  },

  // ==================== 设置 ====================

  openSettingsTab(tabName) {
    this.settingsCurrentTab = tabName;
    this.showSettingsModal = true;
  },

  // ==================== 终端工具 ====================

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
      if (
        !terminal.element ||
        terminal.element.offsetWidth === 0 ||
        terminal.element.offsetHeight === 0
      ) {
        return;
      }

      try {
        const oldCols = terminal.cols;
        const oldRows = terminal.rows;

        fit.fit();

        // 仅在尺寸确实发生变化或初次渲染时刷新
        if (terminal.cols !== oldCols || terminal.rows !== oldRows || !session._initialFitDone) {
          session._initialFitDone = true;
          if (terminal.buffer && terminal.buffer.active) {
            terminal.refresh(0, terminal.rows - 1);
          }
        }

        // 只有当尺寸真正发生变化且 WebSocket 开启时才通知后端
        if (
          (terminal.cols !== oldCols || terminal.rows !== oldRows) &&
          session.ws &&
          session.ws.readyState === WebSocket.OPEN
        ) {
          session.ws.send(
            JSON.stringify({
              type: 'resize',
              cols: terminal.cols,
              rows: terminal.rows,
            })
          );
        }
      } catch (e) {
        if (
          !(
            e instanceof TypeError &&
            (e.message.includes('scrollBarWidth') || e.message.includes('undefined'))
          )
        ) {
          console.warn('终端自适应调整失败:', e);
        }
      }
    });
  },

  // ==================== 登出 ====================

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
    this.showLoginModal = true;
  },

  // ==================== 剪贴板 ====================

  async copyToClipboard(text) {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      this.showGlobalToast('已成功复制到剪贴板', 'success');
    } catch (err) {
      console.error('无法复制文本: ', err);
      // 回退方案
      try {
        const textArea = document.createElement('textarea');
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

  // ==================== 格式化函数 ====================

  formatDateTime(date) {
    if (!date) return '-';
    return formatDateTime(date);
  },

  formatRemainingTime(ms) {
    if (ms <= 0) return '0s';
    const seconds = Math.floor((ms / 1000) % 60);
    const minutes = Math.floor((ms / (1000 * 60)) % 60);
    const hours = Math.floor(ms / (1000 * 60 * 60));

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
        return `${parts[0]}.${parts[1]}.*.*`;
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

  /**
   * 格式化网速为紧凑格式
   * 例如: "1.5 MB/s" -> "1.5M", "10 KB/s" -> "10K", "0 B/s" -> "0B"
   */
  formatSpeedCompact(speed) {
    if (!speed) return '0B';
    // 移除 "/s" 后缀，移除空格，保留数字和单位字母
    return speed
      .replace(/\/s$/i, '') // 移除 /s
      .replace(/\s+/g, '') // 移除空格
      .replace(/(\d+\.?\d*)([KMGT]?)B?/i, '$1$2'); // 简化单位
  },

  /**
   * 解析网速为数字和单位分离的对象
   * 例如: "1.5 MB/s" -> { num: "1.5", unit: "M" }
   */
  parseSpeed(speed) {
    if (!speed) return { num: '0', unit: 'B' };
    const cleaned = speed.replace(/\/s$/i, '').replace(/\s+/g, '');
    const match = cleaned.match(/^(\d+\.?\d*)([KMGT]?)B?$/i);
    if (match) {
      return { num: match[1], unit: match[2] ? match[2].toUpperCase() : 'B' };
    }
    return { num: '0', unit: 'B' };
  },

  getModuleName(id, short = false) {
    const config = MODULE_CONFIG[id];
    if (!config) return id;
    return short ? config.shortName : config.name;
  },

  getModuleIcon(id) {
    const config = MODULE_CONFIG[id];
    return config ? config.icon : 'fa-cube';
  },

  // ==================== Toast 系统 ====================

  showGlobalToast(message, type = 'success', duration = 3000, isManual = false) {
    // 用户主动触发的 info 提示（如"正在导出..."）应该显示
    // 只有自动化过程中的 info 提示才会被过滤
    const effectiveIsManual = type === 'info' ? true : isManual;
    toast[type](message, { duration, isManual: effectiveIsManual });
  },

  showDnsToast(message, type = 'success') {
    toast[type](message);
  },

  showOpenaiToast(message, type = 'success') {
    toast[type](message);
  },

  // ==================== 模态框工具 ====================

  focusModalOverlay(selector = '.modal-overlay') {
    const overlay = document.querySelector(selector);
    if (overlay) {
      overlay.focus();
    }
  },

  showAlert(message, title = '提示', icon = 'fa-info-circle') {
    return new Promise(resolve => {
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
        onCancel: null,
      };
    });
  },

  showConfirm(options) {
    return new Promise(resolve => {
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
        },
      };
    });
  },

  showPrompt(options) {
    return new Promise(resolve => {
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
        },
      };
    });
  },

  // ==================== 其他工具 ====================

  maskEmail(email) {
    if (!email || !email.includes('@')) return email;
    const [local, domain] = email.split('@');
    if (local.length <= 14) return email;
    const masked =
      local.substring(0, 2) + '*'.repeat(local.length - 4) + local.substring(local.length - 2);
    return masked + '@' + domain;
  },

  updateBrowserThemeColor() {
    this.$nextTick(() => {
      const style = getComputedStyle(document.documentElement);
      const bgColor = style.getPropertyValue('--bg-primary').trim();
      const currentPrimary = style.getPropertyValue('--current-primary').trim();
      const serverPrimary = style.getPropertyValue('--server-primary').trim();
      const globalPrimary = style.getPropertyValue('--primary-color').trim();

      const inDocker = this.mainActiveTab === 'server' && this.serverCurrentTab === 'docker';
      const accentColor = inDocker
        ? serverPrimary || currentPrimary || globalPrimary
        : currentPrimary || globalPrimary || serverPrimary;

      const fallbackColor = bgColor || '#f4f6f8';
      const mixedColor = this._mixThemeColors(
        fallbackColor,
        accentColor,
        inDocker ? 0.28 : 0.16
      );

      this._setMetaThemeColor(mixedColor || accentColor || fallbackColor || '#f4f6f8');
    });
  },

  _parseThemeColor(color) {
    if (!color) return null;
    const value = String(color).trim();

    const hex3 = value.match(/^#([0-9a-f]{3})$/i);
    if (hex3) {
      const [r, g, b] = hex3[1].split('');
      return {
        r: parseInt(r + r, 16),
        g: parseInt(g + g, 16),
        b: parseInt(b + b, 16),
      };
    }

    const hex6 = value.match(/^#([0-9a-f]{6})$/i);
    if (hex6) {
      return {
        r: parseInt(hex6[1].slice(0, 2), 16),
        g: parseInt(hex6[1].slice(2, 4), 16),
        b: parseInt(hex6[1].slice(4, 6), 16),
      };
    }

    const rgb = value.match(/^rgba?\(([^)]+)\)$/i);
    if (rgb) {
      const parts = rgb[1]
        .split(',')
        .map(item => Number.parseFloat(item.trim()))
        .filter(num => Number.isFinite(num));
      if (parts.length >= 3) {
        return {
          r: parts[0],
          g: parts[1],
          b: parts[2],
        };
      }
    }

    return null;
  },

  _rgbToHex(rgb) {
    if (!rgb) return '';
    const clamp = value => Math.max(0, Math.min(255, Math.round(value)));
    const toHex = value => clamp(value).toString(16).padStart(2, '0');
    return `#${toHex(rgb.r)}${toHex(rgb.g)}${toHex(rgb.b)}`;
  },

  _mixThemeColors(baseColor, accentColor, accentRatio = 0.16) {
    const base = this._parseThemeColor(baseColor);
    const accent = this._parseThemeColor(accentColor);
    if (!base && !accent) return '';
    if (!base) return this._rgbToHex(accent);
    if (!accent) return this._rgbToHex(base);

    const ratio = Math.max(0, Math.min(1, accentRatio));
    return this._rgbToHex({
      r: base.r * (1 - ratio) + accent.r * ratio,
      g: base.g * (1 - ratio) + accent.g * ratio,
      b: base.b * (1 - ratio) + accent.b * ratio,
    });
  },

  _setMetaThemeColor(color) {
    let metaThemeColor = document.querySelector('meta[name="theme-color"]');
    if (!metaThemeColor) {
      metaThemeColor = document.createElement('meta');
      metaThemeColor.setAttribute('name', 'theme-color');
      document.head.appendChild(metaThemeColor);
    }
    metaThemeColor.setAttribute('content', color);
  },

  // ==================== 日志工具 ====================

  getLogIcon(level) {
    const icons = {
      INFO: 'fa-info-circle',
      WARN: 'fa-exclamation-triangle',
      ERROR: 'fa-times-circle',
      DEBUG: 'fa-bug',
      SUCCESS: 'fa-check-circle',
    };
    return icons[level?.toUpperCase()] || 'fa-circle';
  },

  formatMessage(msg) {
    if (!msg) return '';
    // 简易 ANSI 颜色转换
    return msg
      .replace(/\x1b\[\d+m/g, '')
      .replace(/\[32m/g, '<span class="log-success">')
      .replace(/\[31m/g, '<span class="log-error">')
      .replace(/\[33m/g, '<span class="log-warning">')
      .replace(/\[0m/g, '</span>');
  },

  // 通用打码函数
  maskAddress,

  // Markdown 渲染 (re-export)
  formatRegion,
};
