/**
 * Koyeb 模块 - 前端逻辑
 * 管理 Koyeb 云平台的监控和操作
 */

import { store } from '../store.js';
import { toast } from './toast.js';

export const koyebMethods = {

    // ============ 数据加载 ============

    /**
     * 启动 Koyeb 自动刷新
     */
    startKoyebAutoRefresh() {
        try {
            this.stopKoyebAutoRefresh();

            // 获取刷新间隔（秒），默认为30
            const intervalSeconds = (store.koyebRefreshInterval || 30000) / 1000;

            // 重置倒计时
            store.koyebRefreshCountdown = intervalSeconds;
            store.koyebRefreshProgress = 100;

            // 自动刷新 (仅在可见时触发)
            this.koyebTimer = setInterval(() => {
                if (document.visibilityState !== 'visible') return;
                this.loadKoyebData();
            }, store.koyebRefreshInterval || 30000);

            // 1s倒计时更新 (仅在可见时更新)
            this.koyebCountdownTimer = setInterval(() => {
                if (document.visibilityState !== 'visible') return;

                store.koyebRefreshCountdown--;

                if (store.koyebRefreshCountdown <= 0) {
                    store.koyebRefreshCountdown = intervalSeconds;
                    store.koyebRefreshProgress = 100;
                } else {
                    store.koyebRefreshProgress = (store.koyebRefreshCountdown / intervalSeconds) * 100;
                }
            }, 1000);
        } catch (e) {
            console.error('启动 Koyeb 自动刷新失败:', e);
        }
    },

    /**
     * 停止 Koyeb 自动刷新
     */
    stopKoyebAutoRefresh() {
        if (this.koyebTimer) {
            clearInterval(this.koyebTimer);
            this.koyebTimer = null;
        }
        if (this.koyebCountdownTimer) {
            clearInterval(this.koyebCountdownTimer);
            this.koyebCountdownTimer = null;
        }
    },

    /**
     * 切换 Koyeb 自动刷新状态
     */
    toggleKoyebDataRefresh() {
        store.koyebDataRefreshPaused = !store.koyebDataRefreshPaused;
        if (store.koyebDataRefreshPaused) {
            this.stopKoyebAutoRefresh();
        } else {
            this.startKoyebAutoRefresh();
        }
    },

    /**
     * 加载 Koyeb 数据
     */
    async loadKoyebData(isManual = false) {
        // 防止并发请求
        if (store.koyebRefreshing) return;

        store.koyebRefreshing = true;
        // 只有手动触发时或者第一次加载时显示 loading 状态
        if (isManual || (store.koyebAccounts && store.koyebAccounts.length === 0)) {
            store.koyebLoading = true;
        }

        // 手动刷新时重置倒计时
        const intervalSeconds = (store.koyebRefreshInterval || 30000) / 1000;
        store.koyebRefreshCountdown = intervalSeconds;
        store.koyebRefreshProgress = 100;

        try {
            const response = await fetch('/api/koyeb/data');
            const result = await response.json();

            if (result.success) {
                store.koyebAccounts = result.accounts || [];
                store.koyebLastUpdate = new Date().toLocaleTimeString();

                // 缓存到本地
                this.saveToKoyebCache(result.accounts);

                if (isManual) {
                    toast.success('Koyeb 数据已刷新');
                }
            } else {
                throw new Error(result.error || '加载失败');
            }
        } catch (error) {
            console.error('加载 Koyeb 数据失败:', error);
            if (isManual) {
                toast.error('加载 Koyeb 数据失败: ' + error.message);
            }
        } finally {
            store.koyebLoading = false;
            store.koyebRefreshing = false;
        }
    },

    /**
     * 缓存数据到本地
     */
    saveToKoyebCache(data) {
        try {
            const cacheData = {
                timestamp: Date.now(),
                accounts: data
            };
            localStorage.setItem('koyeb_cache', JSON.stringify(cacheData));
        } catch (e) {
            console.warn('Koyeb 缓存保存失败:', e);
        }
    },

    /**
     * 从本地缓存加载
     */
    loadFromKoyebCache() {
        try {
            const cached = localStorage.getItem('koyeb_cache');
            if (cached) {
                const data = JSON.parse(cached);
                if (data.accounts) {
                    store.koyebAccounts = data.accounts;
                    // 恢复上次更新时间
                    if (data.timestamp) {
                        store.koyebLastUpdate = new Date(data.timestamp).toLocaleTimeString();
                    }
                    return true;
                }
            }
        } catch (e) {
            console.warn('Koyeb 缓存加载失败:', e);
        }
        return false;
    },

    /**
     * 加载 Koyeb 账号列表（管理用）
     */
    async loadKoyebManagedAccounts() {
        try {
            const response = await fetch('/api/koyeb/accounts');
            const result = await response.json();

            if (result.success) {
                this.koyebManagedAccounts = result.accounts || [];
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error('加载 Koyeb 账号列表失败:', error);
        }
    },

    // ============ 账号管理 ============

    /**
     * 添加 Koyeb 账号
     */
    async addKoyebAccount() {
        if (this.koyebAddingAccount) return;

        const { name, token } = this.newKoyebAccount;

        if (!name.trim()) {
            this.koyebAddAccountError = '请输入账号名称';
            return;
        }
        if (!token.trim()) {
            this.koyebAddAccountError = '请输入 API Token';
            return;
        }

        this.koyebAddingAccount = true;
        this.koyebAddAccountError = '';
        this.koyebAddAccountSuccess = '';

        try {
            const response = await fetch('/api/koyeb/accounts', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: name.trim(), token: token.trim() })
            });

            const result = await response.json();

            if (result.success) {
                this.koyebAddAccountSuccess = '账号添加成功！';
                this.newKoyebAccount = { name: '', token: '' };
                this.showAddKoyebAccountModal = false;

                // 刷新数据
                await this.loadKoyebData();
                await this.loadKoyebManagedAccounts();

                toast.success('Koyeb 账号添加成功');
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error('添加 Koyeb 账号失败:', error);
            this.koyebAddAccountError = error.message || '添加失败';
        } finally {
            this.koyebAddingAccount = false;
        }
    },

    /**
     * 删除 Koyeb 账号
     */
    async removeKoyebAccount(accountId) {
        const confirmed = await store.showConfirm({
            title: '确定要删除这个 Koyeb 账号吗？',
            message: '删除后将无法恢复，请确认操作。',
            icon: 'fa-trash-alt',
            confirmText: '确认删除',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/koyeb/accounts/${accountId}`, {
                method: 'DELETE'
            });

            const result = await response.json();

            if (result.success) {
                toast.success('账号已删除');
                await this.loadKoyebData();
                await this.loadKoyebManagedAccounts();
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error('删除 Koyeb 账号失败:', error);
            toast.error('删除失败: ' + error.message);
        }
    },

    /**
     * 批量添加 Koyeb 账号
     */
    async batchAddKoyebAccounts() {
        if (this.koyebAddingAccount) return;
        if (!this.koyebBatchAccounts.trim()) {
            this.koyebBatchAddError = '请输入账号信息';
            return;
        }

        this.koyebAddingAccount = true;
        this.koyebBatchAddError = '';
        this.koyebBatchAddSuccess = '';

        const lines = this.koyebBatchAccounts.trim().split('\n');
        let successCount = 0;
        let failCount = 0;
        const errors = [];

        for (const line of lines) {
            if (!line.trim()) continue;

            // 支持多种格式：名称:Token、名称：Token、名称(Token)、名称（Token）
            let name, token;

            if (line.includes(':') || line.includes('：')) {
                const parts = line.split(/[:：]/);
                name = parts[0].trim();
                token = parts.slice(1).join(':').trim();
            } else if (line.includes('(') || line.includes('（')) {
                const match = line.match(/(.+?)[(（](.+?)[)）]/);
                if (match) {
                    name = match[1].trim();
                    token = match[2].trim();
                }
            }

            if (!name || !token) {
                errors.push(`格式错误: ${line.substring(0, 20)}...`);
                failCount++;
                continue;
            }

            // 清理 Token，移除所有不可见字符（保留 ASCII 可打印字符）
            token = token.replace(/[^\x21-\x7E]/g, '');

            try {
                const response = await fetch('/api/koyeb/accounts', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ name, token })
                });

                const result = await response.json();

                if (result.success) {
                    successCount++;
                } else {
                    errors.push(`${name}: ${result.error}`);
                    failCount++;
                }
            } catch (error) {
                errors.push(`${name}: ${error.message}`);
                failCount++;
            }
        }

        this.koyebAddingAccount = false;

        if (successCount > 0) {
            this.koyebBatchAddSuccess = `成功添加 ${successCount} 个账号`;
            this.koyebBatchAccounts = '';
            await this.loadKoyebData();
            await this.loadKoyebManagedAccounts();
        }

        if (failCount > 0) {
            this.koyebBatchAddError = `${failCount} 个账号添加失败:\n${errors.join('\n')}`;
        }
    },

    /**
     * 导出所有 Koyeb 账号
     */
    async exportAllAccounts() {
        try {
            const response = await fetch('/api/koyeb/accounts/export');
            const result = await response.json();

            if (!result.success) throw new Error(result.error);

            const accounts = result.accounts || [];
            if (accounts.length === 0) {
                toast.warn('没有可导出的账号');
                return;
            }

            // 格式：名称:Token
            const content = accounts.map(acc => `${acc.name}:${acc.token}`).join('\n');
            const blob = new Blob([content], { type: 'text/plain' });
            const url = URL.createObjectURL(blob);

            const a = document.createElement('a');
            a.href = url;
            a.download = `koyeb_accounts_${new Date().toISOString().split('T')[0]}.txt`;
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            URL.revokeObjectURL(url);

            toast.success(`已导出 ${accounts.length} 个账号`);
        } catch (error) {
            console.error('导出失败:', error);
            toast.error('导出失败: ' + error.message);
        }
    },

    /**
     * 导入 Koyeb 账号
     */
    async importAllAccounts() {
        const input = document.createElement('input');
        input.type = 'file';
        input.accept = '.txt';

        input.onchange = (e) => {
            const file = e.target.files[0];
            if (!file) return;

            const reader = new FileReader();
            reader.onload = (event) => {
                const content = event.target.result;
                if (!content.trim()) {
                    toast.warn('文件内容为空');
                    return;
                }

                // 填充到批量添加文本框
                this.koyebBatchAccounts = content;

                // 自动跳转到批量添加面板
                this.$nextTick(() => {
                    const batchPanel = document.querySelector('.panel-title .fa-layer-group')?.closest('.panel');
                    if (batchPanel) {
                        batchPanel.scrollIntoView({ behavior: 'smooth' });
                    }
                    toast.info('文件已加载到批量添加区域，请查看并点击“批量添加”');
                });
            };
            reader.readAsText(file);
        };

        input.click();
    },

    // ============ 服务操作 ============

    /**
     * 暂停 Koyeb 服务
     */
    async pauseKoyebService(account, app, service) {
        const confirmed = await store.showConfirm({
            title: '确定要暂停这个服务吗？',
            message: `服务 "${service.name}" 将被暂停，可以随时恢复。`,
            icon: 'fa-pause-circle',
            confirmText: '确认暂停',
            confirmClass: 'btn-warning'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/koyeb/services/${service._id}/pause`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ accountId: account.id })
            });

            const result = await response.json();

            if (result.success) {
                toast.success('服务已暂停');
                service.status = 'SUSPENDED';
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error('暂停服务失败:', error);
            toast.error('暂停失败: ' + error.message);
        }
    },

    /**
     * 重启 Koyeb 服务
     */
    async restartKoyebService(account, app, service) {
        const action = service.status === 'SUSPENDED' ? '启动' : '重启';

        try {
            const response = await fetch(`/api/koyeb/services/${service._id}/restart`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ accountId: account.id })
            });

            const result = await response.json();

            if (result.success) {
                toast.success(`服务${action}中...`);
                service.status = 'STARTING';

                // 几秒后刷新数据
                setTimeout(() => this.loadKoyebData(), 3000);
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error(`${action}服务失败:`, error);
            toast.error(`${action}失败: ` + error.message);
        }
    },

    /**
     * 重新部署 Koyeb 服务
     */
    async redeployKoyebService(account, app, service) {
        const confirmed = await store.showConfirm({
            title: '确定要重新部署这个服务吗？',
            message: `服务 "${service.name}" 将使用最新代码重新部署。`,
            icon: 'fa-rocket',
            confirmText: '确认部署',
            confirmClass: 'btn-primary'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/koyeb/services/${service._id}/redeploy`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ accountId: account.id })
            });

            const result = await response.json();

            if (result.success) {
                toast.success('服务重新部署中...');
                setTimeout(() => this.loadKoyebData(), 3000);
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error('重新部署失败:', error);
            toast.error('重新部署失败: ' + error.message);
        }
    },

    /**
     * 删除 Koyeb 服务
     */
    async deleteKoyebService(account, app, service) {
        const confirmed = await store.showConfirm({
            title: '⚠️ 确定要删除这个服务吗？',
            message: `服务 "${service.name}" 将被永久删除，此操作不可撤销！`,
            icon: 'fa-trash-alt',
            confirmText: '永久删除',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/koyeb/services/${service._id}`, {
                method: 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ accountId: account.id })
            });

            const result = await response.json();

            if (result.success) {
                toast.success('服务已删除');
                // 从本地数据中移除
                const appIndex = account.projects?.findIndex(p => p._id === app._id);
                if (appIndex !== -1) {
                    const serviceIndex = account.projects[appIndex].services.findIndex(s => s._id === service._id);
                    if (serviceIndex !== -1) {
                        account.projects[appIndex].services.splice(serviceIndex, 1);
                    }
                }
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error('删除服务失败:', error);
            toast.error('删除失败: ' + error.message);
        }
    },

    /**
     * 删除 Koyeb 应用
     */
    async deleteKoyebApp(account, app) {
        const confirmed = await store.showConfirm({
            title: '⚠️ 确定要删除这个应用吗？',
            message: `应用 "${app.name}" 及其所有服务将被永久删除！`,
            icon: 'fa-trash-alt',
            confirmText: '永久删除',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/koyeb/apps/${app._id}`, {
                method: 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ accountId: account.id })
            });

            const result = await response.json();

            if (result.success) {
                toast.success('应用已删除');
                await this.loadKoyebData();
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error('删除应用失败:', error);
            toast.error('删除失败: ' + error.message);
        }
    },

    /**
     * 重命名 Koyeb 应用
     */
    async renameKoyebApp(account, app) {
        // 使用 SweetAlert2 或类似机制，或者复用 Zeabur 的编辑逻辑
        // 这里假设已经进入编辑模式，input 绑定了 app.editingName
        const newName = app.editingName.trim();
        if (!newName || newName === app.name) {
            app.isEditing = false;
            return;
        }

        // 检查是否有重名
        const hasDuplicate = account.projects.some(p =>
            p._id !== app._id && p.name.toLowerCase() === newName.toLowerCase()
        );

        if (hasDuplicate) {
            toast.error(`应用名称 "${newName}" 已存在,请使用其他名称`);
            app.editingName = app.name;
            app.isEditing = false;
            return;
        }

        try {
            const response = await fetch(`/api/koyeb/apps/${app._id}/rename`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ accountId: account.id, name: newName })
            });

            const result = await response.json();

            if (result.success) {
                toast.success('应用重命名成功');
                app.name = newName;
                app.isEditing = false;
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error('重命名应用失败:', error);

            // 提供更友好的错误提示
            let errorMsg = '重命名失败';
            if (error.message.includes('Validation') || error.message.includes('validation')) {
                errorMsg = '应用名称不符合要求,请使用字母、数字和连字符,不能以连字符开头或结尾';
            } else if (error.message.includes('already exists') || error.message.includes('duplicate')) {
                errorMsg = `应用名称 "${newName}" 已被使用,请选择其他名称`;
            } else {
                errorMsg = `重命名失败: ${error.message}`;
            }

            toast.error(errorMsg);
            app.editingName = app.name;
        }
    },

    /**
     * 重命名 Koyeb 服务
     */
    async renameKoyebService(account, app, service) {
        const newName = service.editingName.trim();
        if (!newName || newName === service.name) {
            service.isEditing = false;
            return;
        }

        // 检查同一应用下是否有重名服务
        const hasDuplicate = app.services.some(s =>
            s._id !== service._id && s.name.toLowerCase() === newName.toLowerCase()
        );

        if (hasDuplicate) {
            toast.error(`服务名称 "${newName}" 在该应用下已存在,请使用其他名称`);
            service.editingName = service.name;
            service.isEditing = false;
            return;
        }

        try {
            const response = await fetch(`/api/koyeb/services/${service._id}/rename`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ accountId: account.id, name: newName })
            });

            const result = await response.json();

            if (result.success) {
                toast.success('服务重命名成功');
                service.name = newName;
                service.isEditing = false;
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error('重命名服务失败:', error);

            // 提供更友好的错误提示
            let errorMsg = '重命名失败';
            if (error.message.includes('Validation') || error.message.includes('validation')) {
                errorMsg = '服务名称不符合要求,请使用字母、数字和连字符,不能以连字符开头或结尾';
            } else if (error.message.includes('already exists') || error.message.includes('duplicate')) {
                errorMsg = `服务名称 "${newName}" 已被使用,请选择其他名称`;
            } else {
                errorMsg = `重命名失败: ${error.message}`;
            }

            toast.error(errorMsg);
            service.editingName = service.name;
        }
    },

    /**
     * 开始编辑应用名称
     */
    startEditKoyebAppName(app) {
        app.editingName = app.name;
        app.isEditing = true;
        this.$nextTick(() => {
            // 聚焦并选中所有文本
            const inputs = this.$refs.koyebAppNameInput;
            if (inputs) {
                const input = Array.isArray(inputs) ? inputs.find(el => el) : inputs;
                if (input) {
                    input.focus();
                    input.select();
                }
            }
        });
    },

    /**
     * 取消编辑应用名称
     */
    cancelEditKoyebAppName(app) {
        app.isEditing = false;
        app.editingName = '';
    },

    /**
     * 开始编辑服务名称
     */
    startEditKoyebServiceName(service) {
        service.editingName = service.name;
        service.isEditing = true;
        this.$nextTick(() => {
            // 在 Vue 3 中,由于没有用到 ref,我们需要通过 DOM 直接找到对应的输入框
            // 可以通过查找正在编辑的服务对应的输入框
            const activeInput = document.querySelector('.service-name-input:not([style*="display: none"])');
            if (activeInput) {
                activeInput.focus();
                activeInput.select();
            }
        });
    },

    /**
     * 取消编辑服务名称
     */
    cancelEditKoyebServiceName(service) {
        service.isEditing = false;
        service.editingName = '';
    },

    /**
     * 查看 Koyeb 服务日志
     */
    async showKoyebServiceLogs(account, app, service) {
        this.openLogViewer({
            title: `服务日志: ${service.name}`,
            subtitle: `${app.name} / ${account.name}`,
            source: 'koyeb',
            fetcher: async () => {
                const response = await fetch(`/api/koyeb/services/${service._id}/logs?accountId=${account.id}`);
                const result = await response.json();

                if (result.success) {
                    // Koyeb 日志 API 返回结构适配
                    // 假设现在返回的是 streams/logs/query 的结果，通常是一个对象数组
                    // 每个对象可能有 { msg, created_at, ... } 或者 { result: { msg, ... } }
                    const rawLogs = result.logs || [];
                    return rawLogs.map(l => {
                        const entry = l.result || l; // 兼容不同结构
                        return {
                            timestamp: entry.created_at ? new Date(entry.created_at).getTime() : Date.now(),
                            message: entry.msg || JSON.stringify(entry),
                            level: 'INFO'
                        };
                    });
                } else {
                    throw new Error(result.error);
                }
            }
        });
    },


    /**
     * 获取 Koyeb 服务指标
     */
    async fetchKoyebServiceMetrics(account, service) {
        try {
            // 获取 CPU 指标
            const cpuRes = await fetch(`/api/koyeb/services/${service._id}/metrics?accountId=${account.id}&name=CPU_TOTAL_PERCENT`);
            const cpuData = await cpuRes.json();

            // 获取内存指标
            const memRes = await fetch(`/api/koyeb/services/${service._id}/metrics?accountId=${account.id}&name=MEM_RSS`);
            const memData = await memRes.json();

            if (cpuData.success && memData.success) {
                // 提取最新值
                const latestCpu = cpuData.metrics?.[0]?.data?.[cpuData.metrics[0].data.length - 1]?.[1] || 0;
                const latestMem = memData.metrics?.[0]?.data?.[memData.metrics[0].data.length - 1]?.[1] || 0;

                // 更新到服务对象（Vue 3 会自动响应）
                service.metrics = {
                    cpu: parseFloat(latestCpu).toFixed(1),
                    mem: Math.round(latestMem / 1024 / 1024) // 转换为 MB
                };
            }
        } catch (error) {
            console.error('获取指标失败:', error);
        }
    },

    /**
     * 获取 Koyeb 服务实例
     */
    async fetchKoyebServiceInstances(account, service) {
        if (service.loadingInstances) return;

        service.loadingInstances = true;
        try {
            const response = await fetch(`/api/koyeb/services/${service._id}/instances?accountId=${account.id}`);
            const result = await response.json();

            if (result.success) {
                service.instances = result.instances || [];
                service.showInstances = !service.showInstances;
            } else {
                throw new Error(result.error);
            }
        } catch (error) {
            console.error('获取实例失败:', error);
            toast.error('获取实例失败: ' + error.message);
        } finally {
            service.loadingInstances = false;
        }
    },

    /**
     * 获取 Koyeb 组织用量
     */
    async fetchKoyebUsage(account) {
        try {
            const response = await fetch(`/api/koyeb/usage?accountId=${account.id}`);
            const result = await response.json();

            if (result.success) {
                account.usageDetails = result.usage;
            }
        } catch (error) {
            console.error('获取用量失败:', error);
        }
    },

    // ============ 账号展开/收起 ============

    /**
     * 切换 Koyeb 账号展开状态
     */
    toggleKoyebAccount(accountName) {
        if (this.koyebExpandedAccounts[accountName]) {
            delete this.koyebExpandedAccounts[accountName];
        } else {
            this.koyebExpandedAccounts[accountName] = true;
        }
    },

    /**
     * 检查 Koyeb 账号是否展开
     */
    isKoyebAccountExpanded(accountName) {
        return !!this.koyebExpandedAccounts[accountName];
    },

    // ============ 辅助方法 ============

    /**
     * 获取 Koyeb 状态颜色类
     */
    getKoyebStatusClass(status) {
        const statusClasses = {
            'RUNNING': 'status-running',
            'HEALTHY': 'status-running',
            'STARTING': 'status-starting',
            'SUSPENDED': 'status-suspended',
            'PAUSED': 'status-suspended',
            'STOPPED': 'status-suspended',
            'ERROR': 'status-error',
            'ERRORED': 'status-error',
            'UNHEALTHY': 'status-error'
        };
        return statusClasses[status?.toUpperCase()] || 'status-unknown';
    },

    /**
     * 获取 Koyeb 状态显示文本
     */
    getKoyebStatusText(status) {
        const statusTexts = {
            'RUNNING': '运行中',
            'HEALTHY': '运行中',
            'STARTING': '启动中',
            'SUSPENDED': '已暂停',
            'PAUSED': '已暂停',
            'STOPPED': '已停止',
            'ERROR': '错误',
            'ERRORED': '错误',
            'UNHEALTHY': '异常'
        };
        return statusTexts[status?.toUpperCase()] || status || '未知';
    },

    /**
     * 格式化 Koyeb 余额
     */
    formatKoyebBalance(balance) {
        if (balance === null || balance === undefined) return '-';
        // Koyeb credits 单位
        return '$' + (balance / 100).toFixed(2);
    },



    /**
     * 清除 Koyeb 缓存
     */
    clearKoyebCache() {
        localStorage.removeItem('koyeb_cache');
        this.koyebAccounts = [];
        toast.success('Koyeb 缓存已清除');
    }
};
