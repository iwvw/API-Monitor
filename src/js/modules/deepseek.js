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
        if (store.dsCurrentTab === 'models') this.loadDsModels();
        else if (store.dsCurrentTab === 'accounts') this.loadDsAccounts();
        else if (store.dsCurrentTab === 'logs') this.loadDsLogs();
        else if (store.dsCurrentTab === 'settings') this.loadDsSettings();
    },

    switchDeepSeekTab(tab) {
        store.dsCurrentTab = tab;
        if (tab === 'models') this.loadDsModels();
        else if (tab === 'accounts') this.loadDsAccounts();
        else if (tab === 'logs') this.loadDsLogs();
        else if (tab === 'settings') this.loadDsSettings();
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
                DEFAULT_MAX_TOKENS: data.DEFAULT_MAX_TOKENS || '8192',
                SYSTEM_INSTRUCTION: data.SYSTEM_INSTRUCTION || '',
            };
        } catch (e) {
            toast.error('加载设置失败: ' + e.message);
        }
        // 填充端点信息
        this.$nextTick(() => {
            let hostUrl = window.location.origin;
            if (store.publicApiUrl) {
                hostUrl = store.publicApiUrl.replace(/\/$/, '');
            }
            const baseUrl = `${hostUrl}/v1`;
            const baseUrlEl = document.getElementById('ds-base-url');
            if (baseUrlEl) baseUrlEl.textContent = baseUrl;
            const curlEl = document.getElementById('ds-curl-example');
            if (curlEl) curlEl.textContent = `curl ${baseUrl}/chat/completions \\\n  -H "Authorization: Bearer YOUR_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{\n    "model": "deepseek-chat",\n    "messages": [{"role": "user", "content": "Hello"}],\n    "stream": true\n  }'`;
        });
    },

    async saveDsSettings() {
        store.dsSaving = true;
        try {
            const resp = await fetch(`${DS_API}/settings`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(store.dsSettingsForm),
            });
            if (resp.ok) toast.success('设置已保存');
            else toast.error('保存失败');
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
};
