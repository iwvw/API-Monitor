import { store } from '../store.js';

const QWEN_API = '/api/qwen';

export const qwenMethods = {
    // ==================== 初始化与切换 ====================
    async switchToQwen() {
        store.mainActiveTab = 'qwen';
        store.qwenCurrentTab = store.qwenCurrentTab || 'models';
        this.loadQwenStats();
        this.loadQwenModels();
        this.loadQwenAccounts();
        this.loadQwenSettings();
        this.loadQwenModelRedirects();
    },

    switchQwenTab(tab) {
        store.qwenCurrentTab = tab;
        if (tab === 'logs') this.loadQwenLogs();
    },

    // ==================== 统计与矩阵 ====================
    async loadQwenStats() {
        try {
            const resp = await fetch(`${QWEN_API}/stats`);
            if (resp.ok) {
                try {
                    store.qwenStats = await resp.json();
                } catch (e) {
                    console.error('解析 Qwen 统计失败 (非 JSON)', e);
                }
            }
        } catch (e) { console.error('加载 Qwen 统计失败:', e); }
    },

    async loadQwenModels() {
        store.qwenModelsLoading = true;
        try {
            const resp = await fetch(`${QWEN_API}/matrix`);
            if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
            try {
                store.qwenMatrix = await resp.json();
            } catch (e) {
                console.error('解析 Qwen 矩阵 JSON 失败:', e);
            }
        } catch (e) { 
            console.error('加载 Qwen 矩阵失败:', e.message); 
        } finally { 
            store.qwenModelsLoading = false; 
        }
    },

    async syncQwenModels() {
        if (store.qwenModelsSyncing) return;
        store.qwenModelsSyncing = true;
        try {
            const resp = await fetch(`${QWEN_API}/sync-models`, { method: 'POST' });
            
            // 防御性：检查状态码
            if (!resp.ok) {
                let errorMsg = `服务器返回错误 (${resp.status})`;
                try {
                    const errorData = await resp.json();
                    errorMsg = errorData.error || errorMsg;
                } catch (e) { /* 非 JSON 响应，维持默认错误 */ }
                throw new Error(errorMsg);
            }

            // 防御性：安全解析 JSON
            let result;
            try {
                result = await resp.json();
            } catch (e) {
                throw new Error('服务器响应格式解析失败 (非有效 JSON)');
            }

            if (result.error) throw new Error(result.error);
            
            this.loadQwenModels();
            if (this.showToast) {
                this.showToast(`同步完成！发现 ${result.count} 个模型${result.added > 0 ? ` (新增 ${result.added} 个)` : ''}`, 'success');
            } else {
                alert('同步完成！');
            }
        } catch (e) {
            console.error('同步 Qwen 模型失败:', e);
            const displayMsg = e.message.includes('Unexpected end of JSON input') 
                ? '服务器连接异常，未能获取有效响应' 
                : e.message;
                
            if (this.showToast) this.showToast(`同步失败: ${displayMsg}`, 'danger');
            else alert('同步失败: ' + displayMsg);
        } finally {
            store.qwenModelsSyncing = false;
        }
    },

    async updateQwenMatrixItem(modelId, field, value) {
        try {
            const resp = await fetch(`${QWEN_API}/matrix/${modelId}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ [field]: value })
            });
            if (resp.ok) this.loadQwenModels();
        } catch (e) { console.error('更新矩阵失败:', e); }
    },

    // ==================== 账号管理 ====================
    async loadQwenAccounts() {
        store.qwenAccountsLoading = true;
        try {
            const resp = await fetch(`${QWEN_API}/accounts`);
            if (resp.ok) {
                try {
                    store.qwenAccounts = await resp.json();
                } catch (e) {
                    console.error('解析 Qwen 账号失败 (非 JSON)', e);
                }
            }
        } catch (e) { console.error('加载 Qwen 账号失败:', e); }
        finally { store.qwenAccountsLoading = false; }
    },

    async qwenRefreshAllAccounts() {
        store.qwenAccountRefreshing = true;
        await this.loadQwenAccounts();
        setTimeout(() => { store.qwenAccountRefreshing = false; }, 800);
    },

    async addQwenAccount() {
        if (!store.qwenAccountForm.token) return alert('请输入凭证内容');
        try {
            const resp = await fetch(`${QWEN_API}/accounts`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(store.qwenAccountForm)
            });
            if (resp.ok) {
                store.qwenShowAddAccount = false;
                store.qwenAccountForm = { name: '', token: '' };
                this.loadQwenAccounts();
            }
        } catch (e) { console.error('添加账号失败:', e); }
    },

    async toggleQwenAccount(id) {
        const acc = store.qwenAccounts.find(a => a.id === id);
        if (!acc) return;
        const newEnable = acc.enable === false ? true : false;
        try {
            await fetch(`${QWEN_API}/accounts/${id}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ enable: newEnable })
            });
            this.loadQwenAccounts();
        } catch (e) {}
    },

    async deleteQwenAccount(id) {
        if (!confirm('确定要删除该账号吗？')) return;
        try {
            const resp = await fetch(`${QWEN_API}/accounts/${id}`, { method: 'DELETE' });
            if (resp.ok) this.loadQwenAccounts();
        } catch (e) { console.error('删除账号失败:', e); }
    },

    // ==================== 配置 ====================
    async loadQwenSettings() {
        try {
            const resp = await fetch(`${QWEN_API}/settings`);
            if (resp.ok) {
                try {
                    const data = await resp.json();
                    store.qwenSettingsForm = {
                        API_KEY: data.API_KEY || '',
                        SYSTEM_INSTRUCTION: data.SYSTEM_INSTRUCTION || '',
                    };
                } catch (e) {
                    console.error('解析 Qwen 设置失败 (非 JSON)', e);
                }
            }
            store.qwenBaseUrl = `${window.location.origin}/v1`;
        } catch (e) { console.error('加载 Qwen 设置失败:', e); }
    },

    async saveQwenSettings() {
        store.qwenSaving = true;
        try {
            const resp = await fetch(`${QWEN_API}/settings`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(store.qwenSettingsForm)
            });
            if (resp.ok) {
                if (this.showToast) this.showToast('Qwen 配置保存成功', 'success');
                else alert('Qwen 配置保存成功');
            }
        } catch (e) { console.error('保存设置失败:', e); }
        finally { store.qwenSaving = false; }
    },

    copyQwenEndpoint() {
        navigator.clipboard.writeText(store.qwenBaseUrl).then(() => {
            if (this.showToast) this.showToast('Base URL 已复制', 'success');
            else alert('Base URL 已复制');
        }).catch(err => {
            console.error('复制失败:', err);
        });
    },

    // ==================== 模型重定向 (别名) ====================
    async loadQwenModelRedirects() {
        try {
            const resp = await fetch(`${QWEN_API}/models/redirects`);
            if (resp.ok) {
                try {
                    store.qwenModelRedirects = await resp.json();
                } catch (e) {
                    console.error('解析 Qwen 重定向失败 (非 JSON)', e);
                }
            }
        } catch (e) { console.error('加载 Qwen 重定向失败:', e); }
    },

    async addQwenModelRedirect() {
        const sourceModel = store.qwenNewRedirectSource.trim();
        const targetModel = store.qwenNewRedirectTarget.trim();
        if (!sourceModel || !targetModel) return;

        try {
            const resp = await fetch(`${QWEN_API}/models/redirects`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ sourceModel, targetModel })
            });

            if (resp.ok) {
                store.qwenNewRedirectSource = '';
                store.qwenNewRedirectTarget = '';
                store.qwenEditingRedirectSource = null;
                await this.loadQwenModelRedirects();
            } else {
                const data = await resp.json();
                alert('保存失败: ' + (data.error || '未知错误'));
            }
        } catch (e) {
            console.error('保存重定向失败:', e);
            alert('保存失败: ' + e.message);
        }
    },

    async removeQwenModelRedirect(sourceModel) {
        if (!confirm(`确定要删除对模型 ${sourceModel} 的重定向吗？`)) return;
        try {
            const resp = await fetch(`${QWEN_API}/models/redirects/${encodeURIComponent(sourceModel)}`, {
                method: 'DELETE'
            });
            if (resp.ok) await this.loadQwenModelRedirects();
        } catch (e) { console.error('删除重定向失败:', e); }
    },

    editQwenModelRedirect(r) {
        store.qwenEditingRedirectSource = r.source_model;
        store.qwenNewRedirectSource = r.source_model;
        store.qwenNewRedirectTarget = r.target_model;
    },

    cancelQwenRedirectEdit() {
        store.qwenEditingRedirectSource = null;
        store.qwenNewRedirectSource = '';
        store.qwenNewRedirectTarget = '';
    },

    // ==================== 日志与详情 ====================
    async loadQwenLogs() {
        store.qwenLogsLoading = true;
        try {
            const resp = await fetch(`${QWEN_API}/logs`);
            if (resp.ok) {
                try {
                    store.qwenLogs = await resp.json();
                } catch (e) {
                    console.error('解析 Qwen 日志失败 (非 JSON)', e);
                }
            }
        } catch (e) { console.error('加载调用日志失败:', e); }
        finally { store.qwenLogsLoading = false; }
    },

    getQwenFilteredLogs() {
        let logs = store.qwenLogs;
        if (store.qwenLogFilterModel) {
            logs = logs.filter(l => l.model === store.qwenLogFilterModel);
        }
        if (store.qwenLogFilterAccount) {
            logs = logs.filter(l => l.account_id === store.qwenLogFilterAccount || l.account_name === store.qwenLogFilterAccount);
        }
        return logs;
    },

    getQwenLogModels() {
        const models = new Set();
        store.qwenLogs.forEach(l => { if (l.model) models.add(l.model); });
        return [...models].sort();
    },

    getQwenLogStatusClass(code) {
        if (!code) return 'ag-status-unknown';
        if (code >= 200 && code < 300) return 'ag-status-success';
        if (code === 429) return 'ag-status-warning';
        if (code >= 400) return 'ag-status-danger';
        return 'ag-status-unknown';
    },

    async clearQwenLogs() {
        if (!confirm('确定清空所有调用日志？此操作不可恢复。')) return;
        try {
            await fetch(`${QWEN_API}/logs`, { method: 'DELETE' });
            store.qwenLogs = [];
            if (this.showToast) this.showToast('日志已清空', 'success');
            else alert('日志已清空');
        } catch (e) {
            console.error('清空失败:', e);
            alert('清空失败: ' + e.message);
        }
    },

    showQwenLogDetail(log) {
        store.qwenLogDetail = log;
        store.qwenLogDetailVisible = true;
        store.qwenLogDetailShowRaw = false;
    },

    getQwenLogMessages() {
        if (!store.qwenLogDetail || !store.qwenLogDetail.messages) return [];
        const msgs = store.qwenLogDetail.messages;
        try {
            return typeof msgs === 'string' ? JSON.parse(msgs) : msgs;
        } catch (e) {
            console.error('解析日志消息失败:', e);
            return [];
        }
    },

    copyQwenLogJson() {
        if (!store.qwenLogDetail) return;
        const text = JSON.stringify(store.qwenLogDetail, null, 2);
        navigator.clipboard.writeText(text).then(() => {
            if (this.showToast) this.showToast('JSON 已复制到剪贴板', 'success');
            else alert('已复制到剪贴板');
        }).catch(err => {
            console.error('复制失败:', err);
        });
    }
};
