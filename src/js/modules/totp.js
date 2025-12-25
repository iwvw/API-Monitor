/**
 * TOTP/HOTP 验证器模块
 * 负责 2FA 账号管理、分组管理和验证码显示
 */

import { store } from '../store.js';

/**
 * TOTP 模块方法集合
 */
export const totpMethods = {
    // ==================== 数据加载 ====================

    async loadTotpAccounts() {
        this.totpLoading = true;
        try {
            const [accountsRes, groupsRes] = await Promise.all([
                fetch('/api/totp/accounts'),
                fetch('/api/totp/groups')
            ]);

            const accountsData = await accountsRes.json();
            const groupsData = await groupsRes.json();

            if (accountsData.success) {
                this.totpAccounts = accountsData.data;
                await this.refreshTotpCodes();
                // 加载完成后启动定时器
                this.startTotpTimer();
            }

            if (groupsData.success) {
                this.totpGroups = groupsData.data;
            }
        } catch (error) {
            console.error('[TOTP] 加载失败:', error);
            this.showGlobalToast('加载 2FA 数据失败', 'error');
        } finally {
            this.totpLoading = false;
        }
    },

    async refreshTotpCodes() {
        this.totpRefreshing = true;
        try {
            const response = await fetch('/api/totp/codes');
            const data = await response.json();
            if (data.success) {
                this.totpCodes = data.data;
            }
        } catch (error) {
            console.error('[TOTP] 刷新验证码失败:', error);
        } finally {
            this.totpRefreshing = false;
        }
    },

    // ==================== 定时器 ====================

    startTotpTimer() {
        if (this.totpTimer) clearInterval(this.totpTimer);

        console.log('[TOTP] 启动定时器，当前验证码数:', Object.keys(this.totpCodes).length);
        console.log('[TOTP] 验证码数据:', JSON.stringify(this.totpCodes));

        this.totpTimer = setInterval(() => {
            const updatedCodes = {};
            let needRefresh = false;

            for (const id in this.totpCodes) {
                const code = this.totpCodes[id];
                if (code.remaining !== undefined && code.remaining > 0) {
                    updatedCodes[id] = { ...code, remaining: code.remaining - 1 };
                    if (updatedCodes[id].remaining <= 0) needRefresh = true;
                } else {
                    updatedCodes[id] = code;
                }
            }

            // 强制触发 Vue 响应式更新
            this.totpCodes = Object.assign({}, updatedCodes);

            if (needRefresh) {
                console.log('[TOTP] 倒计时归零，刷新验证码');
                this.refreshTotpCodes();
            }
        }, 1000);
    },

    stopTotpTimer() {
        if (this.totpTimer) {
            clearInterval(this.totpTimer);
            this.totpTimer = null;
        }
    },

    // ==================== 账号 CRUD ====================

    openAddTotpModal() {
        this.totpModalMode = 'add';
        this.totpForm = {
            otp_type: 'totp',
            issuer: '',
            account: '',
            secret: '',
            algorithm: 'SHA1',
            digits: 6,
            period: 30,
            counter: 0,
            group_id: null,
            color: ''
        };
        this.totpModalError = '';
        this.totpShowSecret = false;
        this.showTotpModal = true;
    },

    editTotpAccount(account) {
        this.totpModalMode = 'edit';
        this.totpEditingId = account.id;
        this.totpForm = {
            otp_type: account.otp_type || 'totp',
            issuer: account.issuer || '',
            account: account.account || '',
            secret: '••••••••••••••••',
            algorithm: account.algorithm || 'SHA1',
            digits: account.digits || 6,
            period: account.period || 30,
            counter: account.counter || 0,
            group_id: account.group_id || null,
            color: account.color || ''
        };
        this.totpModalError = '';
        this.totpShowSecret = false;
        this.showTotpModal = true;
    },

    closeTotpModal() {
        this.showTotpModal = false;
        this.totpModalError = '';
        this.totpEditingId = null;
    },

    async saveTotpAccount() {
        this.totpModalError = '';

        if (!this.totpForm.issuer.trim()) {
            this.totpModalError = '请输入发行商名称';
            return;
        }

        if (this.totpModalMode === 'add' && !this.totpForm.secret.trim()) {
            this.totpModalError = '请输入密钥';
            return;
        }

        this.totpModalSaving = true;

        try {
            const payload = {
                otp_type: this.totpForm.otp_type,
                issuer: this.totpForm.issuer.trim(),
                account: this.totpForm.account.trim(),
                algorithm: this.totpForm.algorithm,
                digits: this.totpForm.digits,
                period: this.totpForm.period,
                counter: this.totpForm.counter,
                group_id: this.totpForm.group_id,
                color: this.totpForm.color || null
            };

            if (this.totpModalMode === 'add') {
                payload.secret = this.totpForm.secret.replace(/\s/g, '');
            }

            const url = this.totpModalMode === 'add'
                ? '/api/totp/accounts'
                : `/api/totp/accounts/${this.totpEditingId}`;

            const response = await fetch(url, {
                method: this.totpModalMode === 'add' ? 'POST' : 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });

            const data = await response.json();

            if (data.success) {
                this.showGlobalToast(
                    this.totpModalMode === 'add' ? '账号添加成功' : '账号更新成功',
                    'success'
                );
                this.closeTotpModal();
                await this.loadTotpAccounts();
            } else {
                this.totpModalError = data.error || '保存失败';
            }
        } catch (error) {
            console.error('[TOTP] 保存失败:', error);
            this.totpModalError = '保存失败: ' + error.message;
        } finally {
            this.totpModalSaving = false;
        }
    },

    async deleteTotpAccount(account) {
        const confirmed = await this.showConfirm({
            title: '删除 2FA 账号',
            message: `确定要删除 "${account.issuer}" 的账号吗？`,
            icon: 'fa-trash',
            confirmText: '确定删除',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/totp/accounts/${account.id}`, { method: 'DELETE' });
            const data = await response.json();

            if (data.success) {
                this.showGlobalToast('账号已删除', 'success');
                await this.loadTotpAccounts();
            } else {
                this.showGlobalToast('删除失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('[TOTP] 删除失败:', error);
            this.showGlobalToast('删除失败', 'error');
        }
    },

    // ==================== HOTP 递增 ====================

    async incrementHotp(account) {
        try {
            const response = await fetch(`/api/totp/accounts/${account.id}/increment`, {
                method: 'POST'
            });
            const data = await response.json();

            if (data.success) {
                this.totpCodes[account.id] = {
                    ...this.totpCodes[account.id],
                    code: data.data.code,
                    counter: data.data.counter
                };
                this.showGlobalToast('HOTP 计数器已递增', 'success', 2000);
            }
        } catch (error) {
            console.error('[HOTP] 递增失败:', error);
            this.showGlobalToast('递增失败', 'error');
        }
    },

    // ==================== 分组管理 ====================

    openAddGroupModal() {
        this.totpGroupModalMode = 'add';
        this.totpGroupForm = { name: '', color: '#8b5cf6' };
        this.totpGroupEditingId = null;
        this.showTotpGroupModal = true;
    },

    editGroup(group) {
        this.totpGroupModalMode = 'edit';
        this.totpGroupEditingId = group.id;
        this.totpGroupForm = { name: group.name, color: group.color || '#8b5cf6' };
        this.showTotpGroupModal = true;
    },

    async saveGroup() {
        if (!this.totpGroupForm.name.trim()) {
            this.showGlobalToast('请输入分组名称', 'warning');
            return;
        }

        try {
            const url = this.totpGroupModalMode === 'add'
                ? '/api/totp/groups'
                : `/api/totp/groups/${this.totpGroupEditingId}`;

            const response = await fetch(url, {
                method: this.totpGroupModalMode === 'add' ? 'POST' : 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(this.totpGroupForm)
            });

            const data = await response.json();

            if (data.success) {
                this.showGlobalToast(
                    this.totpGroupModalMode === 'add' ? '分组创建成功' : '分组更新成功',
                    'success'
                );
                this.showTotpGroupModal = false;
                await this.loadTotpAccounts();
            } else {
                this.showGlobalToast(data.error || '保存失败', 'error');
            }
        } catch (error) {
            console.error('[TOTP] 保存分组失败:', error);
            this.showGlobalToast('保存失败', 'error');
        }
    },

    async deleteGroup(group) {
        const confirmed = await this.showConfirm({
            title: '删除分组',
            message: `确定要删除分组 "${group.name}" 吗？分组内的账号不会被删除。`,
            icon: 'fa-folder-minus',
            confirmText: '确定删除',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/totp/groups/${group.id}`, { method: 'DELETE' });
            const data = await response.json();

            if (data.success) {
                this.showGlobalToast('分组已删除', 'success');
                await this.loadTotpAccounts();
            } else {
                this.showGlobalToast('删除失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('[TOTP] 删除分组失败:', error);
            this.showGlobalToast('删除失败', 'error');
        }
    },

    getGroupAccountCount(groupId) {
        return this.totpAccounts.filter(a => a.group_id === groupId).length;
    },

    getPlatformAccountCount(issuer) {
        return this.totpAccounts.filter(a => (a.issuer || '') === (issuer || '')).length;
    },

    // ==================== 导入 ====================

    async importTotpAccounts() {
        if (!this.totpImportUris.trim()) return;

        const uris = this.totpImportUris
            .split('\n')
            .map(line => line.trim())
            .filter(line => line.startsWith('otpauth://'));

        if (uris.length === 0) {
            this.showGlobalToast('没有找到有效的 URI', 'warning');
            return;
        }

        try {
            const response = await fetch('/api/totp/import', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ uris })
            });

            const data = await response.json();

            if (data.success) {
                this.showGlobalToast(`导入完成: 成功 ${data.data.success} 个`, 'success');
                this.showTotpImportModal = false;
                this.totpImportUris = '';
                await this.loadTotpAccounts();
            } else {
                this.showGlobalToast('导入失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('[TOTP] 导入失败:', error);
            this.showGlobalToast('导入失败', 'error');
        }
    },

    // ==================== 导出 ====================

    async exportTotpAccounts() {
        if (this.totpAccounts.length === 0) {
            this.showGlobalToast('没有可导出的账号', 'warning');
            return;
        }

        try {
            const response = await fetch('/api/totp/export');
            const data = await response.json();

            if (data.success) {
                this.totpExportUris = data.data.join('\n');
                this.showTotpExportModal = true;
                this.showGlobalToast('已生成导出数据', 'success');
            } else {
                this.showGlobalToast('导出失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('[TOTP] 导出失败:', error);
            this.showGlobalToast('导出失败', 'error');
        }
    },

    async copyExportedUris() {
        if (!this.totpExportUris) return;

        try {
            await navigator.clipboard.writeText(this.totpExportUris);
            this.showGlobalToast('导出数据已复制到剪贴板', 'success');
        } catch (error) {
            console.error('[TOTP] 复制失败:', error);
            this.showGlobalToast('复制失败', 'error');
        }
    },

    // ==================== 二维码导入 ====================

    /**
     * 处理二维码粘贴事件
     */
    async handleQrPaste(event) {
        const items = event.clipboardData?.items;
        if (!items) return;

        for (const item of items) {
            if (item.type.startsWith('image/')) {
                event.preventDefault();
                const blob = item.getAsFile();
                await this.parseQrImage(blob);
                return;
            }
        }
    },

    /**
     * 处理二维码图片上传
     */
    async handleQrUpload(event) {
        const file = event.target.files?.[0];
        if (!file) return;
        await this.parseQrImage(file);
        event.target.value = ''; // 重置 input
    },

    /**
     * 解析二维码图片
     */
    async parseQrImage(blob) {
        try {
            this.qrParsing = true;
            this.qrError = '';

            const img = new Image();
            const canvas = document.createElement('canvas');
            const ctx = canvas.getContext('2d');

            await new Promise((resolve, reject) => {
                img.onload = resolve;
                img.onerror = reject;
                img.src = URL.createObjectURL(blob);
            });

            canvas.width = img.width;
            canvas.height = img.height;
            ctx.drawImage(img, 0, 0);

            const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height);

            // 使用 jsQR 解析
            const code = window.jsQR(imageData.data, imageData.width, imageData.height);

            if (code) {
                const uri = code.data;
                if (uri.startsWith('otpauth://')) {
                    // 如果开启自动保存，直接导入
                    if (this.totpSettings.autoSave) {
                        this.totpImportUris = uri;
                        await this.importTotpAccounts();
                        this.showGlobalToast('账号已自动导入', 'success');
                    } else {
                        // 添加到导入文本框
                        if (this.totpImportUris) {
                            this.totpImportUris += '\n' + uri;
                        } else {
                            this.totpImportUris = uri;
                        }
                        this.showGlobalToast('二维码解析成功', 'success');
                    }
                } else {
                    this.qrError = '二维码内容不是有效的 OTP URI';
                }
            } else {
                this.qrError = '无法识别二维码，请确保图片清晰';
            }

            URL.revokeObjectURL(img.src);
        } catch (error) {
            console.error('[TOTP] 二维码解析失败:', error);
            this.qrError = '解析失败: ' + error.message;
        } finally {
            this.qrParsing = false;
        }
    },

    // ==================== 工具方法 ====================

    async copyTotpCode(account) {
        const code = this.totpCodes[account.id]?.code;
        if (!code) return;

        try {
            await navigator.clipboard.writeText(code);
            this.showGlobalToast(`验证码已复制: ${code}`, 'success', 2000);
        } catch (error) {
            const textarea = document.createElement('textarea');
            textarea.value = code;
            document.body.appendChild(textarea);
            textarea.select();
            document.execCommand('copy');
            document.body.removeChild(textarea);
            this.showGlobalToast(`验证码已复制: ${code}`, 'success', 2000);
        }
    },


    /**
     * 格式化验证码显示
     * @param {string} code 原始验证码
     * @param {boolean} forceShow 是否强制显示（临时显示）
     */
    formatTotpCode(code, forceShow = false) {
        if (!code) return '------';

        // 如果启用隐藏验证码且未强制显示
        if (this.totpSettings.hideCode && !forceShow) {
            return '*** ***';
        }

        if (code.length === 6) return code.slice(0, 3) + ' ' + code.slice(3);
        if (code.length === 8) return code.slice(0, 4) + ' ' + code.slice(4);
        return code;
    },

    maskEmail(email) {
        if (!email) return '';
        if (!email.includes('@')) return email;
        const [local, domain] = email.split('@');
        if (local.length <= 3) return local[0] + '***@' + domain;
        return local.slice(0, 2) + '***' + local.slice(-1) + '@' + domain;
    },

    getIssuerIcon(issuer) {
        const key = issuer?.toLowerCase() || '';

        // Simple Icons 优先 (品牌官方图标)
        const simpleIcons = {
            'github': 'si si-github',
            'gitlab': 'si si-gitlab',
            'bitbucket': 'si si-bitbucket',
            'discord': 'si si-discord',
            'twitter': 'si si-twitter',
            'x.com': 'si si-x',
            'facebook': 'si si-facebook',
            'instagram': 'si si-instagram',
            'linkedin': 'si si-linkedin',
            'reddit': 'si si-reddit',
            'telegram': 'si si-telegram',
            'whatsapp': 'si si-whatsapp',
            'slack': 'si si-slack',
            'twitch': 'si si-twitch',
            'microsoft': 'fab fa-microsoft',
            'google': 'si si-google',
            'amazon': 'si si-amazon',
            'apple': 'si si-apple',
            'meta': 'si si-meta',
            'cloudflare': 'si si-cloudflare',
            'aws': 'si si-amazonaws',
            'digitalocean': 'si si-digitalocean',
            'vultr': 'si si-vultr',
            'linode': 'si si-linode',
            'heroku': 'si si-heroku',
            'vercel': 'si si-vercel',
            'netlify': 'si si-netlify',
            'railway': 'si si-railway',
            'render': 'si si-render',
            'dropbox': 'si si-dropbox',
            'drive': 'si si-googledrive',
            'onedrive': 'si si-microsoftonedrive',
            'backblaze': 'si si-backblaze',
            'steam': 'si si-steam',
            'epic': 'si si-epicgames',
            'playstation': 'si si-playstation',
            'xbox': 'si si-xbox',
            'nintendo': 'si si-nintendo',
            'blizzard': 'si si-blizzard',
            'ubisoft': 'si si-ubisoft',
            'paypal': 'si si-paypal',
            'stripe': 'si si-stripe',
            'coinbase': 'si si-coinbase',
            'binance': 'si si-binance',
            'npm': 'si si-npm',
            'docker': 'si si-docker',
            'wordpress': 'si si-wordpress',
            'jira': 'si si-jira',
            'trello': 'si si-trello',
            'figma': 'si si-figma',
            'notion': 'si si-notion',
            'tencent': 'fab fa-qq',
            'huawei': 'si si-huawei',
            'aliyun': 'si si-alibabacloud',
            'alibaba': 'si si-alibaba',
            'baidu': 'si si-baidu',
            'weixin': 'si si-wechat',
            'wechat': 'si si-wechat',
            'weibo': 'si si-sinaweibo',
            'qq': 'fab fa-qq',
            'bytedance': 'si si-bytedance',
            'douyin': 'si si-tiktok',
            'bilibili': 'si si-bilibili',
            'spaceship': 'si si-spaceship',
            'godaddy': 'si si-godaddy',
            'namecheap': 'si si-namecheap',
            'porkbun': 'si si-porkbun',
            'bitwarden': 'si si-bitwarden',
            '1password': 'si si-1password',
            'lastpass': 'si si-lastpass',
            'proton': 'si si-proton',
            'shopify': 'si si-shopify',
            'spotify': 'si si-spotify',
            'adobe': 'si si-adobe',
            'zoom': 'si si-zoom',
            'okta': 'si si-okta',
            'auth0': 'si si-auth0',
            'hetzner': 'si si-hetzner',
            'ovh': 'si si-ovh',
            'oracle': 'si si-oracle',
            'sentry': 'si si-sentry',
            'zeabur': 'si si-zeabur',
            'cloudways': 'si si-cloudways'
        };

        // Font Awesome 备选 (核心回退逻辑：当 Simple Icons 字体无法加载时，由 FA 顶替)
        const fontAwesome = {
            'microsoft': 'fab fa-microsoft',
            'github': 'fab fa-github',
            'google': 'fab fa-google',
            'amazon': 'fab fa-amazon',
            'apple': 'fab fa-apple',
            'dropbox': 'fab fa-dropbox',
            'steam': 'fab fa-steam',
            'playstation': 'fab fa-playstation',
            'xbox': 'fab fa-xbox',
            'paypal': 'fab fa-paypal',
            'stripe': 'fab fa-stripe',
            'docker': 'fab fa-docker',
            'npm': 'fab fa-npm',
            'discord': 'fab fa-discord',
            'twitter': 'fab fa-twitter',
            'facebook': 'fab fa-facebook',
            'instagram': 'fab fa-instagram',
            'linkedin': 'fab fa-linkedin',
            'reddit': 'fab fa-reddit',
            'telegram': 'fab fa-telegram',
            'whatsapp': 'fab fa-whatsapp',
            'slack': 'fab fa-slack',
            'twitch': 'fab fa-twitch',
            'spotify': 'fab fa-spotify',
            'adobe': 'fab fa-adobe',
            'cloudflare': 'fas fa-cloud',
            'vultr': 'fas fa-server',
            'digitalocean': 'fab fa-digital-ocean',
            'linux': 'fab fa-linux',
            'ubuntu': 'fab fa-ubuntu',
            'windows': 'fab fa-windows',
            'namesco': 'fas fa-globe',
            'namesilo': 'fas fa-globe',
            'email': 'fas fa-envelope',
            'mail': 'fas fa-envelope',
            'bank': 'fas fa-university',
            'crypto': 'fas fa-coins',
            'vpn': 'fas fa-shield-alt',
            'server': 'fas fa-server',
            'hosting': 'fas fa-server',
            'domain': 'fas fa-globe',
            'ssh': 'fas fa-terminal',
            'database': 'fas fa-database',
            'storage': 'fas fa-hdd',
            'cloud': 'fas fa-cloud',
            'game': 'fas fa-gamepad',
            'shop': 'fas fa-shopping-cart',
            'store': 'fas fa-store',
            'finance': 'fas fa-chart-line',
            'trading': 'fas fa-chart-bar',
            'exchange': 'fas fa-exchange-alt',
            'wallet': 'fas fa-wallet',
            'social': 'fas fa-users',
            'chat': 'fas fa-comments',
            'video': 'fas fa-video',
            'music': 'fas fa-music',
            'photo': 'fas fa-camera',
            'code': 'fas fa-code',
            'dev': 'fas fa-laptop-code'
        };

        // 先查 Simple Icons
        for (const [name, icon] of Object.entries(simpleIcons)) {
            if (key.includes(name)) return icon;
        }

        // 再查 Font Awesome 备选
        for (const [name, icon] of Object.entries(fontAwesome)) {
            if (key.includes(name)) return icon;
        }

        // 默认使用盾牌图标
        return 'fas fa-shield-alt';
    },

    getIssuerIconClass(issuer) {
        const key = issuer?.toLowerCase() || '';
        if (key.includes('github')) return 'issuer-github';
        if (key.includes('microsoft')) return 'issuer-microsoft';
        if (key.includes('google')) return 'issuer-google';
        if (key.includes('cloudflare')) return 'issuer-cloudflare';
        if (key.includes('discord')) return 'issuer-discord';
        if (key.includes('amazon')) return 'issuer-amazon';
        if (key.includes('steam')) return 'issuer-steam';
        return 'issuer-default';
    },

    getIssuerColor(issuer) {
        // 品牌官方颜色
        const colorMap = {
            // 代码托管
            'github': '#6e40c9',  // 使用 GitHub 紫色
            'gitlab': '#fc6d26',
            'bitbucket': '#0052cc',

            // 社交
            'discord': '#5865f2',
            'twitter': '#1da1f2',
            'facebook': '#1877f2',
            'instagram': '#e4405f',
            'linkedin': '#0a66c2',
            'reddit': '#ff4500',
            'telegram': '#26a5e4',
            'whatsapp': '#25d366',
            'slack': '#4a154b',
            'twitch': '#9146ff',

            // 科技公司
            'microsoft': '#00a4ef',
            'google': '#4285f4',
            'amazon': '#ff9900',
            'apple': '#000000',
            'meta': '#1877f2',

            // 云服务
            'cloudflare': '#f38020',
            'vultr': '#007bfc',
            'digitalocean': '#0080ff',
            'linode': '#00a95c',
            'heroku': '#430098',
            'vercel': '#000000',
            'netlify': '#00c7b7',
            'railway': '#0b0d0e',
            'render': '#46e3b7',
            'hetzner': '#d50c2d',
            'ovh': '#123f6d',

            // 存储
            'dropbox': '#0061ff',
            'backblaze': '#e21e29',

            // 游戏
            'steam': '#1b2838',
            'epic': '#2f2d2e',
            'playstation': '#003791',
            'xbox': '#107c10',
            'nintendo': '#e60012',
            'blizzard': '#00ceff',

            // 支付
            'paypal': '#003087',
            'stripe': '#635bff',
            'coinbase': '#0052ff',
            'binance': '#f0b90b',

            // 中国服务商
            'tencent': '#1296db',
            'huawei': '#cf0a2c',
            'aliyun': '#ff6a00',
            'alibaba': '#ff6a00',
            'baidu': '#2319dc',
            'wechat': '#07c160',
            'weixin': '#07c160',
            'weibo': '#e6162d',
            'qq': '#12b7f5',
            'bilibili': '#00a1d6',

            // 域名/托管
            'spaceship': '#394eff',
            'godaddy': '#1bdbdb',
            'namecheap': '#de3723',
            'porkbun': '#f28c9d',

            // 密码管理
            'bitwarden': '#175ddc',
            '1password': '#0094f5',
            'lastpass': '#d32d27',
            'proton': '#6d4aff',

            // 其他
            'shopify': '#7ab55c',
            'spotify': '#1db954',
            'adobe': '#ff0000',
            'zoom': '#2d8cff',
            'notion': '#000000',
            'figma': '#f24e1e',
            'docker': '#2496ed',
            'npm': '#cb3837'
        };
        const key = issuer?.toLowerCase() || '';
        for (const [name, color] of Object.entries(colorMap)) {
            if (key.includes(name)) return color;
        }
        return '#8b5cf6';
    }
};

/**
 * TOTP 模块计算属性
 */
export const totpComputed = {
    filteredTotpAccounts() {
        let accounts = [...this.totpAccounts];

        // 根据设置决定是否按平台分组
        if (this.totpSettings.groupByPlatform) {
            // 统计每个平台的账号数量
            const issuerCount = {};
            accounts.forEach(acc => {
                const issuer = acc.issuer || '';
                issuerCount[issuer] = (issuerCount[issuer] || 0) + 1;
            });

            // 按平台账号数量降序排序，数量相同则按平台名称排序
            accounts.sort((a, b) => {
                const countDiff = (issuerCount[b.issuer || ''] || 0) - (issuerCount[a.issuer || ''] || 0);
                if (countDiff !== 0) return countDiff;
                return (a.issuer || '').localeCompare(b.issuer || '');
            });
        }

        // 分组筛选
        if (this.totpFilterGroup) {
            accounts = accounts.filter(a => a.group_id === this.totpFilterGroup);
        }

        // 搜索筛选
        if (this.totpSearchQuery) {
            const query = this.totpSearchQuery.toLowerCase();
            accounts = accounts.filter(acc =>
                (acc.issuer?.toLowerCase().includes(query)) ||
                (acc.account?.toLowerCase().includes(query))
            );
        }

        return accounts;
    }
};

/**
 * TOTP 模块数据属性
 */
export const totpData = {
    totpAccounts: [],
    totpGroups: [],
    totpCodes: {},
    totpLoading: false,
    totpRefreshing: false,
    totpSearchQuery: '',
    totpFilterGroup: '',
    totpTimer: null,
    totpCurrentTab: 'accounts',
    totpGroupByIssuer: true,  // 是否按平台分组显示

    // 账号模态框
    showTotpModal: false,
    showTotpImportModal: false,
    showTotpExportModal: false,
    totpModalMode: 'add',
    totpModalSaving: false,
    totpModalError: '',
    totpEditingId: null,
    totpShowSecret: false,
    totpImportUris: '',
    totpExportUris: '',
    totpForm: {
        otp_type: 'totp',
        issuer: '',
        account: '',
        secret: '',
        algorithm: 'SHA1',
        digits: 6,
        period: 30,
        counter: 0,
        group_id: null,
        color: ''
    },

    // 分组模态框
    showTotpGroupModal: false,
    totpGroupModalMode: 'add',
    totpGroupEditingId: null,
    totpGroupForm: {
        name: '',
        color: '#8b5cf6'
    },

    // 二维码导入
    qrParsing: false,
    qrError: '',

    // TOTP 设置 (从 store 获取响应式状态)
    get totpSettings() { return store.totpSettings; }
};
