/**
 * DeepSeek API 前端模块
 */

import { store } from '../store.js';
import { toast } from './toast.js';

const DS_API = '/api/deepseek';

export const deepseekMethods = {
    switchToDeepSeek() {
        store.mainActiveTab = 'deepseek';
        if (!store.dsCurrentTab) store.dsCurrentTab = 'models';
        this.switchDeepSeekTab(store.dsCurrentTab);
    },

    switchDeepSeekTab(tab) {
        store.dsCurrentTab = tab;
        if (tab === 'models') {
            this.loadDsModels();
            this.loadDsMatrix();
            this.loadDsStats(); // 把统计也放在 models 标签下展示 (类似 gcli)
        } else if (tab === 'accounts') {
            this.loadDsAccounts();
            this.loadDsCheckHistory();
        } else if (tab === 'logs') {
            this.loadDsLogs();
        } else if (tab === 'settings') {
            this.loadDsSettings();
            this.loadDsModelRedirects();
        }
    },

    // ==================== 模型矩阵 ====================
    async loadDsMatrix() {
        try {
            const resp = await fetch(`${DS_API}/matrix`);
            if (resp.ok) {
                store.dsMatrix = await resp.json();
            }
        } catch (e) {
            console.error('加载矩阵失败:', e.message);
        }
    },

    async updateDsMatrixItem(modelId, field, value) {
        try {
            const resp = await fetch(`${DS_API}/matrix/${modelId}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ [field]: value }),
            });
            if (resp.ok) {
                const data = await resp.json();
                store.dsMatrix = data.matrix;
                toast.success(`${modelId} 配置已同步`);
            }
        } catch (e) {
            toast.error('更新矩阵失败: ' + e.message);
        }
    },

    async loadDsStats() {
        try {
            const resp = await fetch(`${DS_API}/stats`);
            if (resp.ok) {
                store.dsStats = await resp.json();
            }
        } catch (e) {
            console.error('加载统计失败:', e.message);
        }
    },

    // ==================== 模型管理 ====================
    async loadDsModels() {
        store.dsModelsLoading = true;
        try {
            const resp = await fetch(`${DS_API}/models`);
            const data = await resp.json();
            store.dsModels = data.data || [];
        } catch (e) {
            toast.error('加载模型失败: ' + e.message);
        } finally {
            store.dsModelsLoading = false;
        }
        this.loadDsModelRedirects();
    },

    // ==================== 模型重定向 ====================
    async loadDsModelRedirects() {
        try {
            const resp = await fetch(`${DS_API}/models/redirects`);
            if (resp.ok) {
                store.dsModelRedirects = await resp.json();
            }
        } catch (e) {
            console.error('加载重定向失败:', e.message);
        }
    },

    async addDsModelRedirect() {
        const source = store.dsNewRedirectSource?.trim();
        const target = store.dsNewRedirectTarget?.trim();
        if (!source || !target) return;

        try {
            // 编辑模式：源名变了则先删旧的
            if (store.dsEditingRedirectSource && store.dsEditingRedirectSource !== source) {
                await fetch(
                    `${DS_API}/models/redirects/${encodeURIComponent(store.dsEditingRedirectSource)}`,
                    { method: 'DELETE' }
                );
            }

            const resp = await fetch(`${DS_API}/models/redirects`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ sourceModel: source, targetModel: target }),
            });

            if (resp.ok) {
                toast.success('重定向规则已保存');
                store.dsNewRedirectSource = '';
                store.dsNewRedirectTarget = '';
                store.dsEditingRedirectSource = null;
                await this.loadDsModelRedirects();
            } else {
                const data = await resp.json();
                toast.error('保存失败: ' + (data.error || '未知错误'));
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
        }
    },

    editDsModelRedirect(r) {
        store.dsEditingRedirectSource = r.source_model;
        store.dsNewRedirectSource = r.source_model;
        store.dsNewRedirectTarget = r.target_model;
    },

    async removeDsModelRedirect(source) {
        const confirmed = await store.showConfirm({
            title: '确认删除',
            message: `确定要删除 ${source} 的重定向规则吗？`,
            icon: 'fa-trash',
            confirmText: '删除',
            confirmClass: 'btn-danger',
        });
        if (!confirmed) return;

        try {
            const resp = await fetch(
                `${DS_API}/models/redirects/${encodeURIComponent(source)}`,
                { method: 'DELETE' }
            );
            if (resp.ok) {
                toast.success('删除成功');
                await this.loadDsModelRedirects();
            } else {
                toast.error('删除失败');
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
        }
    },

    cancelDsRedirectEdit() {
        store.dsEditingRedirectSource = null;
        store.dsNewRedirectSource = '';
        store.dsNewRedirectTarget = '';
    },

    // ==================== 账号管理 ====================
    async loadDsAccounts() {
        store.dsAccountsLoading = true;
        try {
            const resp = await fetch(`${DS_API}/accounts`);
            const data = await resp.json();
            store.dsAccounts = Array.isArray(data) ? data : [];
        } catch (e) {
            toast.error('加载账号失败: ' + e.message);
        } finally {
            store.dsAccountsLoading = false;
        }
    },

    async addDsAccount() {
        const form = store.dsAccountForm;

        if (store.dsAccountAddMode === 'token') {
            if (!form.token) {
                store.dsAccountFormError = '请填写 Token';
                return;
            }
        } else {
            if (!form.emailOrMobile) {
                store.dsAccountFormError = '请填写邮箱或手机号';
                return;
            }
            if (!form.password) {
                store.dsAccountFormError = '请填写密码';
                return;
            }
        }

        store.dsAccountAdding = true;
        store.dsAccountFormError = '';
        try {
            const body = { name: form.name || '' };

            if (store.dsAccountAddMode === 'token') {
                body.token = form.token;
                body.email = form.name || 'Token Account';
            } else {
                body.password = form.password;
                const isEmail = form.emailOrMobile.includes('@');
                if (isEmail) body.email = form.emailOrMobile;
                else body.mobile = form.emailOrMobile;
            }

            const resp = await fetch(`${DS_API}/accounts`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });
            const data = await resp.json();
            if (resp.ok) {
                toast.success('账号添加成功');
                store.dsShowAddAccount = false;
                store.dsAccountForm = { name: '', emailOrMobile: '', password: '', token: '' };
                store.dsAccountAddMode = 'password';
                this.loadDsAccounts();
            } else {
                store.dsAccountFormError = data.error || '添加失败';
            }
        } catch (e) {
            store.dsAccountFormError = e.message;
        } finally {
            store.dsAccountAdding = false;
        }
    },

    async toggleDsAccount(id) {
        try {
            const resp = await fetch(`${DS_API}/accounts/${id}/toggle`, { method: 'POST' });
            const data = await resp.json();
            toast.success(`账号已${data.enable ? '启用' : '禁用'}`);
            this.loadDsAccounts();
        } catch (e) {
            toast.error('操作失败: ' + e.message);
        }
    },

    async testDsAccount(id) {
        toast.info('正在测试登录...');
        try {
            const resp = await fetch(`${DS_API}/accounts/${id}/test`, { method: 'POST' });
            const data = await resp.json();
            if (data.success) {
                toast.success('登录测试成功');
                this.loadDsAccounts();
            } else {
                toast.error('登录测试失败: ' + (data.error || 'unknown'));
            }
        } catch (e) {
            toast.error('测试失败: ' + e.message);
        }
    },

    async deleteDsAccount(id) {
        const confirmed = await store.showConfirm({
            title: '确认删除',
            message: '确定删除此账号？此操作不可恢复。',
            icon: 'fa-trash',
            confirmText: '删除',
            confirmClass: 'btn-danger',
        });
        if (!confirmed) return;
        try {
            await fetch(`${DS_API}/accounts/${id}`, { method: 'DELETE' });
            toast.success('账号已删除');
            this.loadDsAccounts();
        } catch (e) {
            toast.error('删除失败: ' + e.message);
        }
    },

    async dsRefreshAllAccounts() {
        store.dsAccountRefreshing = true;
        try {
            const resp = await fetch(`${DS_API}/accounts/refresh`, { method: 'POST' });
            const data = await resp.json();
            toast.success(`刷新完成: ${data.refreshed} 成功, ${data.failed} 失败`);
            this.loadDsAccounts();
        } catch (e) {
            toast.error('刷新失败: ' + e.message);
        } finally {
            store.dsAccountRefreshing = false;
        }
    },

    // 导出账号
    async dsExportAccounts() {
        try {
            const resp = await fetch(`${DS_API}/accounts/export`);
            const data = await resp.json();
            const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `deepseek-accounts-${new Date().toISOString().slice(0, 10)}.json`;
            a.click();
            URL.revokeObjectURL(url);
            toast.success('导出成功');
        } catch (e) {
            toast.error('导出失败: ' + e.message);
        }
    },

    // 导入账号
    dsImportAccounts() {
        const input = document.createElement('input');
        input.type = 'file';
        input.accept = '.json';
        input.onchange = async (e) => {
            const file = e.target.files[0];
            if (!file) return;
            try {
                const text = await file.text();
                const data = JSON.parse(text);
                const accounts = data.accounts || data;
                if (!Array.isArray(accounts)) {
                    toast.error('无效的文件格式');
                    return;
                }
                const resp = await fetch(`${DS_API}/accounts/import`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ accounts }),
                });
                const result = await resp.json();
                toast.success(`导入完成: ${result.imported} 成功, ${result.skipped} 跳过`);
                this.loadDsAccounts();
            } catch (err) {
                toast.error('导入失败: ' + err.message);
            }
        };
        input.click();
    },

    // ==================== 日志管理 ====================
    async loadDsLogs() {
        store.dsLogsLoading = true;
        try {
            const resp = await fetch(`${DS_API}/logs?limit=200`);
            const data = await resp.json();
            store.dsLogs = Array.isArray(data) ? data : [];
        } catch (e) {
            toast.error('加载日志失败: ' + e.message);
        } finally {
            store.dsLogsLoading = false;
        }
    },

    getDsFilteredLogs() {
        let logs = store.dsLogs;
        if (store.dsLogFilterModel) {
            logs = logs.filter(l => l.model === store.dsLogFilterModel);
        }
        if (store.dsLogFilterAccount) {
            logs = logs.filter(l => l.accountId === store.dsLogFilterAccount || l.accountName === store.dsLogFilterAccount);
        }
        return logs;
    },

    getDsLogModels() {
        const models = new Set();
        store.dsLogs.forEach(l => { if (l.model) models.add(l.model); });
        return [...models].sort();
    },

    async showDsLogDetail(log) {
        try {
            const resp = await fetch(`${DS_API}/logs/${log.id}`);
            const data = await resp.json();
            if (data && data.detail) {
                try { data.detail = JSON.parse(data.detail); } catch (e) { }
            }
            store.dsLogDetail = data;
            store.dsLogDetailContent = JSON.stringify(data, null, 2);
            store.dsLogDetailShowRaw = false;
            store.dsLogDetailVisible = true;
        } catch (e) {
            toast.error('获取详情失败: ' + e.message);
        }
    },

    getDsLogMessages() {
        const detail = store.dsLogDetail?.detail;
        if (!detail) return [];
        return detail.messages || [];
    },

    getDsLogResponse() {
        const detail = store.dsLogDetail?.detail;
        if (!detail) return null;
        const resp = detail.response;
        if (!resp) return null;
        const choice = resp.choices?.[0];
        if (!choice) return null;
        return choice.message || {};
    },

    getDsLogStatusClass(code) {
        if (!code) return 'ag-status-unknown';
        if (code >= 200 && code < 300) return 'ag-status-success';
        if (code === 429) return 'ag-status-warning';
        if (code >= 400) return 'ag-status-danger';
        return 'ag-status-unknown';
    },

    copyDsLogJson() {
        if (!store.dsLogDetail) return;
        const json = JSON.stringify(store.dsLogDetail, null, 2);
        navigator.clipboard.writeText(json).then(() => {
            toast.success('JSON 已复制到剪贴板');
        }).catch(err => {
            toast.error('复制失败: ' + err.message);
        });
    },

    async clearDsLogs() {
        const confirmed = await store.showConfirm({
            title: '确认清空',
            message: '确定清空所有调用日志？此操作不可恢复。',
            icon: 'fa-trash',
            confirmText: '清空',
            confirmClass: 'btn-danger',
        });
        if (!confirmed) return;
        try {
            await fetch(`${DS_API}/logs`, { method: 'DELETE' });
            store.dsLogs = [];
            toast.success('日志已清空');
        } catch (e) {
            toast.error('清空失败: ' + e.message);
        }
    },

    // ==================== 设置管理 ====================
    async loadDsSettings() {
        try {
            const resp = await fetch(`${DS_API}/settings`);
            const data = await resp.json();
            store.dsSettingsForm = {
                API_KEY: data.API_KEY || '',
                DEFAULT_TEMPERATURE: data.DEFAULT_TEMPERATURE || '1',
                DEFAULT_MAX_TOKENS: data.DEFAULT_MAX_TOKENS || '131072',
                SYSTEM_INSTRUCTION: data.SYSTEM_INSTRUCTION || '',
            };

            // 填充端点信息 (放在 try 块内以访问 data)
            let hostUrl = window.location.origin;
            if (store.publicApiUrl) {
                hostUrl = store.publicApiUrl.replace(/\/$/, '');
            }
            const baseUrl = `${hostUrl}/v1`;
            store.dsBaseUrl = baseUrl;
            const apiKey = (this.agSettingsForm && this.agSettingsForm.API_KEY) || data.API_KEY || 'YOUR_API_KEY';
            const example = `curl ${baseUrl}/chat/completions \\\n  -H "Authorization: Bearer ${apiKey}" \\\n  -H "Content-Type: application/json" \\\n  -d '{\n    "model": "deepseek-chat",\n    "messages": [{"role": "user", "content": "Hello"}],\n    "stream": true\n  }'`;
            store.dsCurlExample = example;
            console.log('[DS] API Guide updated:', { baseUrl, apiKeyLength: apiKey.length });
        } catch (e) {
            toast.error('加载设置失败: ' + e.message);
        }
    },

    async saveDsSettings() {
        store.dsSaving = true;
        try {
            const resp = await fetch(`${DS_API}/settings`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(store.dsSettingsForm),
            });
            if (resp.ok) {
                toast.success('设置已保存');
                await this.loadDsSettings();
            } else {
                toast.error('保存失败');
            }
        } catch (e) {
            toast.error('保存失败: ' + e.message);
        } finally {
            store.dsSaving = false;
        }
    },

    // 复制端点地址
    copyDsEndpoint() {
        let hostUrl = window.location.origin;
        if (store.publicApiUrl) {
            hostUrl = store.publicApiUrl.replace(/\/$/, '');
        }
        const baseUrl = `${hostUrl}/v1`;
        navigator.clipboard.writeText(baseUrl).then(() => {
            toast.success('已复制 API 端点地址');
        }).catch(() => {
            toast.error('复制失败，请手动复制');
        });
    },

    // ==================== 模型检测 (参考 gcli 模式) ====================
    async loadDsCheckHistory() {
        try {
            const resp = await fetch(`${DS_API}/check/history`);
            if (resp.ok) {
                store.dsCheckHistory = await resp.json();
            }
        } catch (e) {
            console.error('加载检测历史失败:', e.message);
        }
    },

    async runDsHealthCheck() {
        if (store.dsChecking) return;
        store.dsChecking = true;
        try {
            const resp = await fetch(`${DS_API}/check/run`, { method: 'POST' });
            if (resp.ok) {
                toast.success('已启动批量健康检测');
                // 启动轮询检查进度
                this.pollDsCheckHistory();
            } else {
                toast.error('启动检测失败');
                store.dsChecking = false;
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
            store.dsChecking = false;
        }
    },

    // 轮询检测进度
    async pollDsCheckHistory() {
        const interval = setInterval(async () => {
            await this.loadDsCheckHistory();
            // 如果所有模型在最新时间点都有了结果，或者超过 2 分钟，停止轮询
            // 这里简单处理：如果正在检测的任务完成（后端逻辑），通常 matrix 会更新
            // 目前后端逻辑是后台运行，前端可以一直轮询直到用户切换页面或多次加载后无变化
            // 为了性能，我们轮询 12 次 (约 1 分钟)
        }, 5000);
        setTimeout(() => {
            clearInterval(interval);
            store.dsChecking = false;
        }, 60000);
    },

    async clearDsCheckHistory() {
        const confirmed = await store.showConfirm({
            title: '确认清空',
            message: '确定清空所有模型健康检测历史记录吗？',
            icon: 'fa-trash',
            confirmText: '清空',
            confirmClass: 'btn-danger',
        });
        if (!confirmed) return;

        try {
            const resp = await fetch(`${DS_API}/check/clear`, { method: 'POST' });
            if (resp.ok) {
                toast.success('记录已清空');
                store.dsCheckHistory = { models: [], times: [], matrix: {} };
            }
        } catch (e) {
            toast.error('操作失败: ' + e.message);
        }
    },

    formatDsCheckTime(timestamp) {
        if (!timestamp) return '';
        const date = new Date(timestamp * 1000);
        const now = new Date();
        const isToday = date.toDateString() === now.toDateString();
        
        const h = date.getHours().toString().padStart(2, '0');
        const m = date.getMinutes().toString().padStart(2, '0');
        
        if (isToday) return `${h}:${m}`;
        
        const mo = (date.getMonth() + 1).toString().padStart(2, '0');
        const d = date.getDate().toString().padStart(2, '0');
        return `${mo}-${d} ${h}:${m}`;
    },

    toggleDsCheckModel(model) {
        if (store.dsDisabledCheckModels.includes(model)) {
            store.dsDisabledCheckModels = store.dsDisabledCheckModels.filter(m => m !== model);
        } else {
            store.dsDisabledCheckModels.push(model);
        }
    },

    getDsCheckBadgeClass(matrixData, accountIndex) {
        if (!matrixData || !matrixData.passedAccounts) return 'check-badge-unknown';
        const passedArr = matrixData.passedAccounts.split(',').map(Number);
        if (passedArr.includes(accountIndex)) return 'check-badge-ok';
        if (matrixData.status === 'error' || matrixData.status === 'ok') {
            // 如果整体状态已有，但此账号不在通过列表，说明它失败了
            return 'check-badge-error';
        }
        return 'check-badge-unknown';
    },

    getDsCheckBadgeTitle(matrixData, accountIndex) {
        if (!matrixData) return '未检测';
        const passedArr = (matrixData.passedAccounts || '').split(',').map(Number);
        if (passedArr.includes(accountIndex)) return '检测通过';
        if (matrixData.error_log) return `检测失败:\n${matrixData.error_log}`;
        return '未通过';
    },

    // 保留旧的测试方法，以防某些地方仍有引用
    async testDsModel() {
        toast.info('该功能已迁移至健康检测');
    },
};
