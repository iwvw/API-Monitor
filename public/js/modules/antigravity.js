/**
 * Antigravity API 模块
 */

export const antigravityMethods = {
    switchToAntigravity() {
        this.mainActiveTab = 'antigravity';
        this.antigravityCurrentTab = 'quotas';
        this.loadAntigravityAccounts();
        this.loadAntigravityStats();
    },

    async loadAntigravityAccounts() {
        this.antigravityLoading = true;
        try {
            const response = await fetch('/api/antigravity/accounts', {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            if (Array.isArray(data)) {
                this.antigravityAccounts = data;

                // 如果未选中账号，默认选中第一个在线账号
                if (!this.antigravityQuotaSelectedAccountId && data.length > 0) {
                    const firstOnline = data.find(acc => acc.status === 'online');
                    this.antigravityQuotaSelectedAccountId = firstOnline ? firstOnline.id : data[0].id;
                    // 加载选中账号的额度
                    this.loadAntigravityQuotas();
                }
            }
        } catch (error) {
            console.error('加载 Antigravity 账号失败:', error);
            this.showGlobalToast('加载账号失败', 'error');
        } finally {
            this.antigravityLoading = false;
        }
    },

    openAddAntigravityManualModal() {
        this.antigravityManualForm = {
            name: '',
            accessToken: '',
            refreshToken: '',
            projectId: '',
            expiresIn: 3599
        };
        this.antigravityManualFormError = '';
        this.showAntigravityManualModal = true;
    },

    openAddAntigravityAccountModal() {
        this.antigravityEditingAccount = null;
        this.antigravityAccountForm = {
            name: '',
            email: '',
            password: '',
            apiKey: ''
        };
        this.antigravityAccountFormError = '';
        this.showAntigravityAccountModal = true;
    },

    editAntigravityAccount(account) {
        this.antigravityEditingAccount = account;
        this.antigravityAccountForm = {
            name: account.name || '',
            email: account.email || '',
            password: account.password || '',
            apiKey: account.api_key || ''
        };
        this.antigravityAccountFormError = '';
        this.showAntigravityAccountModal = true;
    },

    async saveAntigravityAccount() {
        if (!this.antigravityAccountForm.name) {
            this.antigravityAccountFormError = '请填写账号名称';
            return;
        }

        this.antigravitySaving = true;
        try {
            const url = this.antigravityEditingAccount
                ? `/api/antigravity/accounts/${this.antigravityEditingAccount.id}`
                : '/api/antigravity/accounts';

            const response = await fetch(url, {
                method: this.antigravityEditingAccount ? 'PUT' : 'POST',
                headers: {
                    ...this.getAuthHeaders(),
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(this.antigravityAccountForm)
            });

            if (response.ok) {
                this.showGlobalToast(this.antigravityEditingAccount ? '账号已更新' : '账号已添加');
                this.showAntigravityAccountModal = false;
                this.loadAntigravityAccounts();
            } else {
                const data = await response.json();
                this.antigravityAccountFormError = data.error || '保存失败';
            }
        } catch (error) {
            this.antigravityAccountFormError = '保存失败: ' + error.message;
        } finally {
            this.antigravitySaving = false;
        }
    },

    async deleteAntigravityAccount(account) {
        const confirmed = await this.showConfirm({
            title: '确认删除',
            message: `确定要删除账号 "${account.name}" 吗？`,
            icon: 'fa-trash',
            confirmText: '删除',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/antigravity/accounts/${account.id}`, {
                method: 'DELETE',
                headers: this.getAuthHeaders()
            });

            if (response.ok) {
                this.showGlobalToast('账号已删除');
                this.loadAntigravityAccounts();
                this.loadAntigravityStats(); // Refresh stats
            } else {
                this.showGlobalToast('删除失败', 'error');
            }
        } catch (error) {
            this.showGlobalToast('删除失败: ' + error.message, 'error');
        }
    },

    // 手动添加账号
    async saveAntigravityManualAccount() {
        if (!this.antigravityManualForm.accessToken || !this.antigravityManualForm.refreshToken) {
            this.antigravityManualFormError = 'Access Token 和 Refresh Token 均为必填项';
            return;
        }

        this.antigravitySaving = true;
        try {
            const response = await fetch('/api/antigravity/accounts/manual', {
                method: 'POST',
                headers: {
                    ...this.getAuthHeaders(),
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(this.antigravityManualForm)
            });

            const data = await response.json();
            if (response.ok) {
                this.showGlobalToast('账号添加成功');
                this.showAntigravityManualModal = false;
                this.loadAntigravityAccounts();
                this.loadAntigravityStats();
                // Reset form
                this.antigravityManualForm = { name: '', accessToken: '', refreshToken: '', projectId: '', expiresIn: 3599 };
            } else {
                this.antigravityManualFormError = data.error || '添加失败';
            }
        } catch (error) {
            this.antigravityManualFormError = '添加失败: ' + error.message;
        } finally {
            this.antigravitySaving = false;
        }
    },

    // 加载统计
    async loadAntigravityStats() {
        try {
            const response = await fetch('/api/antigravity/stats', {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            this.antigravityStats = data;
        } catch (error) {
            console.error('Failed to load stats:', error);
        }
    },

    async toggleAntigravityAccount(account) {
        try {
            const response = await fetch(`/api/antigravity/accounts/${account.id}`, {
                method: 'PUT',
                headers: {
                    ...this.getAuthHeaders(),
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ enable: !account.enable })
            });

            if (response.ok) {
                this.loadAntigravityAccounts();
            }
        } catch (error) {
            console.error('切换状态失败:', error);
        }
    },

    // OAuth 逻辑
    async openGoogleAuthUrl() {
        try {
            const response = await fetch('/api/antigravity/oauth/url', {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            if (data.url) {
                window.open(data.url, '_blank');
            }
        } catch (error) {
            this.showGlobalToast('获取授权链接失败', 'error');
        }
    },

    async parseAgOauthUrl() {
        if (!this.agOauthUrl) return;
        this.antigravityLoading = true;
        try {
            const response = await fetch('/api/antigravity/oauth/parse-url', {
                method: 'POST',
                headers: {
                    ...this.getAuthHeaders(),
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    url: this.agOauthUrl,
                    customProjectId: this.agOauthCustomProjectId,
                    allowRandomProjectId: this.agOauthAllowRandom
                })
            });

            const data = await response.json();
            if (response.ok) {
                this.showGlobalToast('账号授权成功');
                this.agOauthUrl = '';
                this.antigravityCurrentTab = 'accounts';
                this.loadAntigravityAccounts();
            } else {
                this.showGlobalToast(data.error || '解析失败', 'error');
            }
        } catch (error) {
            this.showGlobalToast('解析失败: ' + error.message, 'error');
        } finally {
            this.antigravityLoading = false;
        }
    },

    // 刷新项目 ID
    async refreshAntigravityProjectId(account) {
        try {
            const response = await fetch(`/api/antigravity/accounts/${account.id}/refresh-project-id`, {
                method: 'POST',
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            if (response.ok) {
                this.showGlobalToast(`项目 ID 已更新: ${data.projectId}`);
                this.loadAntigravityAccounts();
            } else {
                this.showGlobalToast(data.error || '更新失败', 'error');
            }
        } catch (error) {
            this.showGlobalToast('更新失败: ' + error.message, 'error');
        }
    },

    // 刷新所有 Token
    async refreshAllAntigravityTokens() {
        this.antigravityLoading = true;
        try {
            const response = await fetch('/api/antigravity/accounts/refresh-all', {
                method: 'POST',
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            this.showGlobalToast(`同步完成: 成功 ${data.refreshed}, 失败 ${data.failed}`);
            this.loadAntigravityAccounts();
        } catch (error) {
            this.showGlobalToast('刷新失败', 'error');
        } finally {
            this.antigravityLoading = false;
        }
    },

    // 切换 Antigravity 子标签页
    switchAntigravityTab(tabName) {
        this.antigravityCurrentTab = tabName;

        // 根据不同标签页加载对应数据
        if (tabName === 'quotas') {
            // 如果还没加载过账号，先加载
            if (!this.antigravityAccounts || this.antigravityAccounts.length === 0) {
                this.loadAntigravityAccounts().then(() => {
                    // 如果没有选中账号，默认选中第一个启用的
                    if (!this.antigravityQuotaSelectedAccountId && this.antigravityAccounts && this.antigravityAccounts.length > 0) {
                        const enabled = this.antigravityAccounts.filter(a => a.enable);
                        this.antigravityQuotaSelectedAccountId = enabled.length > 0 ? enabled[0].id : this.antigravityAccounts[0].id;
                    }
                    this.loadAntigravityQuotas();
                });
            } else {
                // 如果已有账号但没选中
                if (!this.antigravityQuotaSelectedAccountId && this.antigravityAccounts && this.antigravityAccounts.length > 0) {
                    const enabled = this.antigravityAccounts.filter(a => a.enable);
                    this.antigravityQuotaSelectedAccountId = enabled.length > 0 ? enabled[0].id : this.antigravityAccounts[0].id;
                }
                this.loadAntigravityQuotas();
            }
        } else if (tabName === 'settings') {
            this.loadAntigravitySettings();
        } else if (tabName === 'logs') {
            this.loadAntigravityLogs();
        } else {
            // 切出额度页，停止轮询
            this.stopAntigravityQuotaPolling();
        }
    },

    // 启动/安排下一次额度刷新
    scheduleNextQuotaLoad() {
        this.stopAntigravityQuotaPolling(); // 先清除旧的

        // 只有当前在 Antigravity 模块且在 quotas 标签页时才安排下次刷新
        if (this.mainActiveTab === 'antigravity' && this.antigravityCurrentTab === 'quotas') {
            this.antigravityQuotaTimer = setTimeout(() => {
                this.loadAntigravityQuotas(true); // true 表示自动刷新
            }, 30000); // 30秒刷新一次
        }
    },

    stopAntigravityQuotaPolling() {
        if (this.antigravityQuotaTimer) {
            clearTimeout(this.antigravityQuotaTimer);
            this.antigravityQuotaTimer = null;
        }
    },

    // 额度查看
    async loadAntigravityQuotas(isAutoRefresh = false) {
        this.antigravityCurrentTab = 'quotas';
        // 如果不是自动刷新，显示 loading 状态
        if (!isAutoRefresh) {
            this.antigravityQuotaLoading = true;
        }

        try {
            let url = '/api/antigravity/quotas';
            // 如果选中了特定账号，使用特定账号的 API
            if (this.antigravityQuotaSelectedAccountId) {
                url = `/api/antigravity/accounts/${this.antigravityQuotaSelectedAccountId}/quotas`;
            }

            const res = await fetch(url, {
                headers: this.getAuthHeaders()
            });
            const data = await res.json();
            this.antigravityQuotas = data;
            this.antigravityQuotasLastUpdated = new Date().toLocaleString('zh-CN', {
                year: 'numeric',
                month: '2-digit',
                day: '2-digit',
                hour: '2-digit',
                minute: '2-digit',
                second: '2-digit'
            });

            if (!isAutoRefresh) {
                this.showGlobalToast('刷新成功');
            }
        } catch (error) {
            console.error('加载额度失败:', error);
            if (!isAutoRefresh) {
                this.showGlobalToast('加载额度失败', 'error');
            }
        } finally {
            if (!isAutoRefresh) {
                this.antigravityQuotaLoading = false;
            }
            // 无论成功失败，都安排下一次刷新
            this.scheduleNextQuotaLoad();
        }
    },

    getAgQuotaColor(percent) {
        if (percent > 40) return 'var(--ag-success)';
        if (percent > 10) return 'var(--ag-warning)';
        return 'var(--ag-error)';
    },

    getLogStatusClass(statusCode) {
        if (statusCode >= 200 && statusCode < 300) return 'ag-status-2xx';
        if (statusCode >= 300 && statusCode < 400) return 'ag-status-3xx';
        if (statusCode >= 400 && statusCode < 500) return 'ag-status-4xx';
        if (statusCode >= 500) return 'ag-status-5xx';
        return '';
    },

    // 将重置时间转换为倒计时格式
    formatResetCountdown(isoTime) {
        if (!isoTime) return '无';
        try {
            const resetDate = new Date(isoTime);
            if (isNaN(resetDate.getTime())) return '无';
            const now = new Date();
            const diffMs = resetDate - now;

            if (diffMs <= 0) return '已重置';

            const hours = Math.floor(diffMs / (1000 * 60 * 60));
            const minutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));

            if (hours > 24) {
                const days = Math.floor(hours / 24);
                const remainHours = hours % 24;
                return `${days}天${remainHours}时`;
            }

            return `${hours}时${minutes}分`;
        } catch (e) {
            return '无';
        }
    },

    // 日志管理
    async loadAntigravityLogs() {
        this.antigravityCurrentTab = 'logs';
        this.antigravityLoading = true;
        try {
            const response = await fetch('/api/antigravity/logs', {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            this.antigravityLogs = data.logs || [];
        } catch (error) {
            this.showGlobalToast('加载日志失败', 'error');
        } finally {
            this.antigravityLoading = false;
        }
    },

    async clearAntigravityLogs() {
        const confirmed = await this.showConfirm({
            title: '确认清空',
            message: '确定要清空所有调用日志吗？',
            icon: 'fa-trash',
            confirmText: '清空',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch('/api/antigravity/logs/clear', {
                method: 'POST',
                headers: this.getAuthHeaders()
            });
            if (response.ok) {
                this.showGlobalToast('日志已清空');
                this.loadAntigravityLogs();
            } else {
                this.showGlobalToast('清空失败', 'error');
            }
        } catch (error) {
            this.showGlobalToast('请求失败: ' + error.message, 'error');
        }
    },

    async viewAntigravityLogDetail(log) {
        try {
            const response = await fetch(`/api/antigravity/logs/${log.id}`, {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();

            if (data.log) {
                this.antigravityLogDetail = data.log;
                this.showAntigravityLogDetailModal = true;
            } else {
                this.showGlobalToast('日志详情获取失败', 'error');
            }
        } catch (error) {
            console.error('获取日志详情失败:', error);
            this.showGlobalToast('获取日志详情失败: ' + error.message, 'error');
        }
    },

    // 设置管理
    async loadAntigravitySettings() {
        this.antigravityCurrentTab = 'settings';
        this.antigravityLoading = true;
        try {
            const response = await fetch('/api/antigravity/settings', {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            this.antigravitySettings = data;

            // 转换为表单对象，预初始化所有可能的 Key
            const form = {
                'DEFAULT_TEMPERATURE': '',
                'DEFAULT_TOP_P': '',
                'DEFAULT_TOP_K': '',
                'DEFAULT_MAX_TOKENS': '',
                'MAX_IMAGES': '',
                'IMAGE_BASE_URL': '',
                'CREDENTIAL_MAX_USAGE_PER_HOUR': '',
                'TIMEOUT': '',
                'REQUEST_LOG_RETENTION_DAYS': '',
                'API_KEY': '',
                'PROXY': ''
            };
            if (Array.isArray(data)) {
                data.forEach(s => {
                    form[s.key] = s.value;
                });
            }
            this.agSettingsForm = form;

            // 并行加载重定向配置
            await this.loadModelRedirects();
        } catch (error) {
            this.showGlobalToast('加载设置失败', 'error');
        } finally {
            this.antigravityLoading = false;
        }
    },

    // 批量保存所有设置
    async saveAllAgSettings() {
        this.antigravitySaving = true;
        try {
            const keys = Object.keys(this.agSettingsForm);
            let saved = 0;
            for (const key of keys) {
                const value = this.agSettingsForm[key];
                if (value !== undefined && value !== '') {
                    await fetch('/api/antigravity/settings', {
                        method: 'POST',
                        headers: {
                            ...this.getAuthHeaders(),
                            'Content-Type': 'application/json'
                        },
                        body: JSON.stringify({ key, value: String(value) })
                    });
                    saved++;
                }
            }
            this.showGlobalToast(`已保存 ${saved} 项设置`);
        } catch (error) {
            this.showGlobalToast('保存失败: ' + error.message, 'error');
        } finally {
            this.antigravitySaving = false;
        }
    },

    antigravitySettingsByGroup(group) {
        if (!Array.isArray(this.antigravitySettings)) return [];
        return this.antigravitySettings.filter(s => s.category === group);
    },

    getAgSettingLabel(key) {
        const labels = {
            'API_KEY': 'API 访问密钥 (v1/*)',
            'CREDENTIAL_MAX_USAGE_PER_HOUR': '凭证每小时调用上限',
            'REQUEST_LOG_RETENTION_DAYS': '日志保留天数',
            'PORT': '服务监听端口',
            'HOST': '服务监听地址',
            'API_URL': '流式接口 URL',
            'API_MODELS_URL': '模型列表 URL',
            'API_NO_STREAM_URL': '非流式接口 URL',
            'API_HOST': 'API Host 头',
            'API_USER_AGENT': 'User-Agent',
            'PROXY': 'HTTP 代理',
            'TIMEOUT': '请求超时 (ms)',
            'USE_NATIVE_AXIOS': '使用原生 Axios'
        };
        return labels[key] || key;
    },

    getAgSettingDefault(key) {
        const defaults = {
            'CREDENTIAL_MAX_USAGE_PER_HOUR': '20',
            'RETRY_STATUS_CODES': '429,500',
            'RETRY_MAX_ATTEMPTS': '3',
            'MAX_IMAGES': '10',
            'MAX_REQUEST_SIZE': '50mb',
            'PORT': '8045'
        };
        return defaults[key] || '-';
    },

    isAgSettingSensitive(key) {
        return ['PANEL_PASSWORD', 'API_KEY', 'GOOGLE_CLIENT_SECRET'].includes(key);
    },

    async saveAgSetting(s) {
        try {
            const response = await fetch('/api/antigravity/settings', {
                method: 'POST',
                headers: {
                    ...this.getAuthHeaders(),
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ key: s.key, value: s.value })
            });
            if (response.ok) {
                this.showGlobalToast(`设置已保存: ${this.getAgSettingLabel(s.key)}`);
            }
        } catch (error) {
            this.showGlobalToast('保存失败', 'error');
        }
    },

    getAccountName(accountId) {
        const acc = this.antigravityAccounts.find(a => a.id === accountId);
        return acc ? acc.name : accountId || 'System';
    },

    // 视图切换
    toggleAntigravityQuotaView() {
        // 如果未定义（旧状态），初始化为 list
        if (!this.antigravityQuotaViewMode) {
            this.antigravityQuotaViewMode = 'grouped'; // 切换到分组
        } else {
            this.antigravityQuotaViewMode = this.antigravityQuotaViewMode === 'list' ? 'grouped' : 'list';
        }
    },

    getAllAntigravityModels() {
        if (!this.antigravityQuotas) return [];

        let allModels = [];

        // 明确的分组顺序
        const groupOrder = ['banana_pro', 'claude_gpt', 'tab_completion', 'gemini', 'others'];

        // 按照固定顺序遍历分组
        groupOrder.forEach(groupId => {
            const group = this.antigravityQuotas[groupId];
            if (group && group.models && Array.isArray(group.models)) {
                // 给模型加上分组图标，方便识别
                const modelsWithIcon = group.models.map(m => ({
                    ...m,
                    groupIcon: group.icon,
                    groupName: group.name
                }));
                allModels = allModels.concat(modelsWithIcon);
            }
        });

        return allModels;
    },

    async toggleModelStatus(model, event) {
        const enabled = event.target.checked;
        const modelId = model.id;
        let foundModel = null;

        // Optimistic update: 修改源数据以触发视图更新
        // 遍历所有分组找到该模型
        Object.values(this.antigravityQuotas).forEach(group => {
            if (group.models) {
                const target = group.models.find(m => m.id === modelId);
                if (target) {
                    foundModel = target;
                    // 如果属性不存在，Vue 2可能需要 $set，但通常加载后属性都在
                    if (target.enabled === undefined) this.$set(target, 'enabled', true);
                    target.enabled = enabled;
                }
            }
        });

        // 同时也修改传入的临时对象，以防万一
        model.enabled = enabled;

        try {
            const response = await fetch(`/api/antigravity/models/${modelId}/status`, {
                method: 'POST',
                headers: {
                    ...this.getAuthHeaders(),
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ enabled })
            });

            if (response.ok) {
                this.showGlobalToast(`模型 ${modelId} 已${enabled ? '启用' : '禁用'}`);
            } else {
                // Revert
                if (foundModel) foundModel.enabled = !enabled;
                model.enabled = !enabled;
                event.target.checked = !enabled;
                this.showGlobalToast('状态更新失败', 'error');
            }
        } catch (error) {
            // Revert
            if (foundModel) foundModel.enabled = !enabled;
            model.enabled = !enabled;
            event.target.checked = !enabled;
            this.showGlobalToast('请求失败: ' + error.message, 'error');
        }
    },

    // 模型重定向管理
    async loadModelRedirects() {
        try {
            const response = await fetch('/api/antigravity/models/redirects', {
                headers: this.getAuthHeaders()
            });
            this.antigravityModelRedirects = await response.json();
        } catch (error) {
            this.showGlobalToast('加载重定向配置失败', 'error');
        }
    },

    async addModelRedirect(sourceModel, targetModel) {
        if (!sourceModel || !targetModel) return;
        try {
            const response = await fetch('/api/antigravity/models/redirects', {
                method: 'POST',
                headers: {
                    ...this.getAuthHeaders(),
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ sourceModel, targetModel })
            });

            if (response.ok) {
                this.showGlobalToast('添加重定向成功');
                await this.loadModelRedirects();
                return true;
            } else {
                const data = await response.json();
                this.showGlobalToast('添加失败: ' + (data.error || '未知错误'), 'error');
                return false;
            }
        } catch (error) {
            this.showGlobalToast('请求失败: ' + error.message, 'error');
            return false;
        }
    },

    async removeModelRedirect(sourceModel) {
        if (!confirm(`确定要删除 ${sourceModel} 的重定向吗？`)) return;
        try {
            const response = await fetch(`/api/antigravity/models/redirects/${encodeURIComponent(sourceModel)}`, {
                method: 'DELETE',
                headers: this.getAuthHeaders()
            });

            if (response.ok) {
                this.showGlobalToast('删除成功');
                await this.loadModelRedirects();
            } else {
                this.showGlobalToast('删除失败', 'error');
            }
        } catch (error) {
            this.showGlobalToast('请求失败: ' + error.message, 'error');
        }
    }
};
