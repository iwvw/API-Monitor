/**
 * 通用工具方法模块
 * 负责全局初始化、Toast、对话框、格式化、剪贴板等通用功能
 */

import { toast } from './toast.js';
import { formatDateTime, formatFileSize, maskAddress, formatRegion } from './utils.js';

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

    initGlobalKeyListeners() {
        window.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                // 优先关闭最活跃的模态层
                if (this.showImagePreviewModal) {
                    this.showImagePreviewModal = false;
                } else if (this.showSettingsModal) {
                    this.showSettingsModal = false;
                } else if (this.isAnyModalOpen) {
                    this.showServerModal = false;
                    this.showCredentialModal = false;
                    this.showImportServerModal = false;
                }
            }
        });
    },

    initMobileGestures() {
        let touchStartX = null;
        let touchStartY = null;
        const swipeThreshold = 80;

        window.addEventListener('touchstart', (e) => {
            if (window.innerWidth > 768 || this.isAnyModalOpen) return;
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
                    // 震动反馈 (如果支持)
                    if (navigator.vibrate) {
                        navigator.vibrate(15);
                    }
                    this.handleTabSwitch(visibleModules[nextIndex]);
                }
            }
        }, { passive: true });
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
            if (!terminal.element || terminal.element.offsetWidth === 0 || terminal.element.offsetHeight === 0) {
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

    // ==================== 格式化函数 ====================

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

    // ==================== Toast 系统 ====================

    showGlobalToast(message, type = 'success', duration = 3000, isManual = false) {
        toast[type](message, { duration, isManual });
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

    // ==================== 其他工具 ====================

    maskEmail(email) {
        if (!email || !email.includes('@')) return email;
        const [local, domain] = email.split('@');
        if (local.length <= 14) return email;
        const masked = local.substring(0, 2) + '*'.repeat(local.length - 4) + local.substring(local.length - 2);
        return masked + '@' + domain;
    },

    updateBrowserThemeColor() {
        this.$nextTick(() => {
            const bgColor = getComputedStyle(document.documentElement).getPropertyValue('--bg-primary').trim();

            if (bgColor) {
                this._setMetaThemeColor(bgColor);
            } else {
                this._setMetaThemeColor('#f4f6f8');
            }
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
            'INFO': 'fa-info-circle',
            'WARN': 'fa-exclamation-triangle',
            'ERROR': 'fa-times-circle',
            'DEBUG': 'fa-bug',
            'SUCCESS': 'fa-check-circle'
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
    formatRegion
};
