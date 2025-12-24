/**
 * 主机管理模块
 * 负责主机 CRUD、导入导出、凭据管理、Docker 操作等
 */

/**
 * 主机管理方法集合
 * 注意：所有方法必须使用普通函数（非箭头函数）以正确绑定 Vue 的 this 上下文
 */
export const hostMethods = {
    // ==================== 主机 CRUD ====================

    async openAddServerModal() {
        this.serverModalMode = 'add';
        this.serverAddMode = 'ssh'; // 重置为 SSH 模式
        this.quickDeployName = '';  // 重置快速部署名称
        this.quickDeployResult = null; // 重置快速部署结果

        // 重置表单
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

        // 确保凭据列表已加载
        if (this.serverCredentials.length === 0) {
            await this.loadCredentials();
        }

        // 自动应用默认凭据
        const defaultCred = this.serverCredentials.find(c => c.is_default);
        if (defaultCred) {
            this.serverForm.username = defaultCred.username || '';
            this.serverForm.password = defaultCred.password || '';
            this.serverForm.authType = defaultCred.auth_type === 'key' ? 'privateKey' : 'password';
            if (defaultCred.private_key) {
                this.serverForm.privateKey = defaultCred.private_key || '';
                this.serverForm.passphrase = defaultCred.passphrase || '';
            }
            console.log('[Server] 已应用默认凭据:', defaultCred.name);
        }

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
                        authType: server.auth_type === 'key' ? 'privateKey' : (server.auth_type || 'password'),
                        password: '', // 不显示原密码
                        privateKey: '', // 不显示原私钥
                        passphrase: '',
                        tagsInput: Array.isArray(server.tags) ? server.tags.join(',') : '',
                        description: server.description || ''
                    };
                    this.showServerModal = true;
                } else {
                    this.showGlobalToast('主机不存在', 'error');
                }
            } else {
                this.showGlobalToast('加载主机信息失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('加载主机信息失败:', error);
            this.showGlobalToast('加载主机信息失败', 'error');
        }
    },

    closeServerModal() {
        this.showServerModal = false;
        this.serverModalError = '';
        this.quickDeployResult = null; // 关闭时清空结果
    },

    /**
     * 生成快速安装命令 (Agent 模式)
     */
    async generateQuickInstallCommand() {
        const name = this.quickDeployName?.trim();
        if (!name) {
            this.serverModalError = '请输入服务器名称';
            return;
        }

        this.serverModalSaving = true;
        this.serverModalError = '';

        try {
            const response = await fetch('/api/server/agent/quick-install', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name })
            });

            const data = await response.json();

            if (data.success) {
                this.quickDeployResult = data.data;
                this.showGlobalToast(
                    data.data.isNew
                        ? `已创建主机 "${name}"`
                        : `主机 "${name}" 已存在，已生成安装命令`,
                    'success'
                );

                // 如果是新创建的主机，刷新主机列表
                if (data.data.isNew) {
                    await this.loadServerList();
                }
            } else {
                this.serverModalError = data.error || '生成安装命令失败';
            }
        } catch (error) {
            console.error('[Quick Deploy] 生成失败:', error);
            this.serverModalError = '生成安装命令失败: ' + error.message;
        } finally {
            this.serverModalSaving = false;
        }
    },

    /**
     * 复制快速部署安装命令到剪贴板
     */
    async copyQuickDeployCommand() {
        const command = this.agentInstallOS === 'linux'
            ? this.quickDeployResult?.installCommand
            : this.quickDeployResult?.winInstallCommand;

        if (!command) return;

        try {
            await navigator.clipboard.writeText(command);
            this.showGlobalToast('安装命令已复制到剪贴板', 'success');
        } catch (error) {
            // 降级方案
            const textarea = document.createElement('textarea');
            textarea.value = command;
            document.body.appendChild(textarea);
            textarea.select();
            document.execCommand('copy');
            document.body.removeChild(textarea);
            this.showGlobalToast('安装命令已复制', 'success');
        }
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
            const response = await fetch('/api/server/test-connection', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    host: this.serverForm.host,
                    port: this.serverForm.port,
                    username: this.serverForm.username,
                    auth_type: this.serverForm.authType === 'privateKey' ? 'key' : this.serverForm.authType,
                    password: this.serverForm.password,
                    private_key: this.serverForm.privateKey,
                    passphrase: this.serverForm.passphrase
                })
            });

            const data = await response.json();

            if (data.success) {
                this.showGlobalToast('连接测试成功！', 'success', 3000, true);
            } else {
                this.serverModalError = '连接测试失败: ' + data.message;
                this.showGlobalToast('连接测试失败', 'error', 3000, true);
            }
        } catch (error) {
            console.error('测试连接失败:', error);
            this.serverModalError = '测试连接失败: ' + error.message;
            this.showGlobalToast('测试连接失败', 'error', 3000, true);
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
                auth_type: this.serverForm.authType === 'privateKey' ? 'key' : this.serverForm.authType,
                tags: tags,
                description: this.serverForm.description
            };

            // 只在有值时才发送密码/私钥
            if (this.serverForm.authType === 'password' && this.serverForm.password) {
                payload.password = this.serverForm.password;
            }
            if (this.serverForm.authType === 'privateKey' && this.serverForm.privateKey) {
                payload.private_key = this.serverForm.privateKey;
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
                    this.serverModalMode === 'add' ? '主机添加成功' : '主机更新成功',
                    'success'
                );
                this.closeServerModal();

                // 刷新主机列表
                await this.loadServerList();

                // 如果是添加模式，立即刷新新主机的详细信息
                if (this.serverModalMode === 'add' && data.data && data.data.id) {
                    this.refreshServerInfo(data.data.id);
                }
            } else {
                this.serverModalError = data.error || '保存失败';
                this.showGlobalToast('保存失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('保存主机失败:', error);
            this.serverModalError = '保存失败: ' + error.message;
            this.showGlobalToast('保存主机失败', 'error');
        } finally {
            this.serverModalSaving = false;
        }
    },

    async deleteServerById(serverId) {
        const confirmed = await this.showConfirm({
            title: '删除主机',
            message: '确定要删除这台主机吗？',
            icon: 'fa-trash',
            confirmText: '确定',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/server/accounts/${serverId}`, {
                method: 'DELETE'
            });

            const data = await response.json();

            if (data.success) {
                this.showGlobalToast('主机删除成功', 'success');
                await this.loadServerList();
            } else {
                this.showGlobalToast('删除失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('删除主机失败:', error);
            this.showGlobalToast('删除主机失败', 'error');
        }
    },

    async rebootServerById(serverId) {
        const confirmed = await this.showConfirm({
            title: '重启主机',
            message: '确定要重启这台主机吗？',
            icon: 'fa-redo',
            confirmText: '重启',
            confirmClass: 'btn-warning'
        });

        if (!confirmed) return;

        try {
            const response = await fetch('/api/server/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ serverId, action: 'reboot' })
            });

            const data = await response.json();

            if (data.success) {
                this.showGlobalToast('重启命令已发送', 'success');
            } else {
                this.showGlobalToast('重启失败: ' + data.message, 'error');
            }
        } catch (error) {
            console.error('重启主机失败:', error);
            this.showGlobalToast('重启主机失败', 'error');
        }
    },

    async shutdownServerById(serverId) {
        const confirmed = await this.showConfirm({
            title: '关闭主机',
            message: '确定要关闭这台主机吗？此操作不可逆！',
            icon: 'fa-power-off',
            confirmText: '确定关机',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch('/api/server/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ serverId, action: 'shutdown' })
            });

            const data = await response.json();

            if (data.success) {
                this.showGlobalToast('关机命令已发送', 'success');
            } else {
                this.showGlobalToast('关机失败: ' + data.message, 'error');
            }
        } catch (error) {
            console.error('关机失败:', error);
            this.showGlobalToast('关机失败', 'error');
        }
    },

    // ==================== 导入导出 ====================

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
                    this.importModalError = '文件格式错误：应为主机数组';
                    return;
                }

                // 验证数据格式
                const validServers = data.filter(server => {
                    return server.name && server.host && server.username;
                });

                if (validServers.length === 0) {
                    this.importModalError = '文件中没有有效的主机配置';
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
                this.showGlobalToast(`成功导入 ${data.imported || data.results?.filter(r => r.success).length || 0} 台主机`, 'success');
                this.closeImportServerModal();

                // 刷新主机列表
                this.loadServerList();
            } else {
                this.importModalError = '导入失败: ' + data.error;
                this.showGlobalToast('导入失败', 'error');
            }
        } catch (error) {
            console.error('导入主机失败:', error);
            this.importModalError = '导入失败: ' + error.message;
            this.showGlobalToast('导入主机失败', 'error');
        } finally {
            this.importModalSaving = false;
        }
    },

    async batchAddServers() {
        this.serverBatchError = '';
        this.serverBatchSuccess = '';

        if (!this.serverBatchText.trim()) {
            this.serverBatchError = '请输入主机信息';
            return;
        }

        const lines = this.serverBatchText.split('\n');
        const servers = [];
        let parseErrors = [];

        for (let i = 0; i < lines.length; i++) {
            const line = lines[i].trim();
            if (!line) continue;

            try {
                // 尝试解析 JSON
                if (line.startsWith('{')) {
                    const server = JSON.parse(line);
                    if (server.name && server.host) {
                        // 确保必要字段存在
                        server.port = server.port || 22;
                        server.auth_type = server.auth_type || 'password';
                        servers.push(server);
                    } else {
                        parseErrors.push(`第 ${i + 1} 行: 缺少必要字段(name, host)`);
                    }
                } else {
                    // 解析 CSV: name, host, port, username, password
                    // 支持逗号或竖线分隔
                    const parts = line.split(/[|,，]/).map(p => p.trim());

                    if (parts.length >= 2) {
                        const server = {
                            name: parts[0],
                            host: parts[1],
                            port: parseInt(parts[2]) || 22,
                            username: parts[3] || 'root',
                            auth_type: 'password',
                            password: parts[4] || ''
                        };

                        if (!server.name || !server.host) {
                            parseErrors.push(`第 ${i + 1} 行: 格式错误，缺少名称或IP`);
                            continue;
                        }

                        servers.push(server);
                    } else {
                        parseErrors.push(`第 ${i + 1} 行: 格式错误，请检查分隔符`);
                    }
                }
            } catch (e) {
                parseErrors.push(`第 ${i + 1} 行: 解析失败(${e.message})`);
            }
        }

        if (servers.length === 0) {
            this.serverBatchError = '没有识别到有效的主机信息。\n' + (parseErrors.length > 0 ? '错误示例:\n' + parseErrors.slice(0, 3).join('\n') : '');
            return;
        }

        this.serverAddingBatch = true;

        try {
            const response = await fetch('/api/server/accounts/import', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ servers })
            });

            const data = await response.json();

            if (data.success) {
                const successCount = data.results ? data.results.filter(r => r.success).length : 0;
                const failCount = data.results ? data.results.filter(r => !r.success).length : 0;

                let msg = `批量添加完成: 成功 ${successCount} 台`;
                if (failCount > 0) msg += `, 失败 ${failCount} 台`;

                this.serverBatchSuccess = msg;
                this.showGlobalToast(msg, failCount > 0 ? 'warning' : 'success');

                if (successCount > 0) {
                    this.serverBatchText = ''; // 清空输入
                    await this.loadServerList();

                    // 立即刷新所有新添加的主机信息
                    if (data.results) {
                        const newServerIds = data.results
                            .filter(r => r.success && r.data && r.data.id)
                            .map(r => r.data.id);

                        for (const id of newServerIds) {
                            this.refreshServerInfo(id);
                        }
                    }
                }
            } else {
                this.serverBatchError = '添加失败: ' + data.error;
            }
        } catch (error) {
            console.error('批量添加失败:', error);
            this.serverBatchError = '请求失败: ' + error.message;
        } finally {
            this.serverAddingBatch = false;
        }
    },

    async exportServers() {
        try {
            const response = await fetch('/api/server/accounts');
            const data = await response.json();

            if (data.success) {
                // 导出时去除敏感字段
                const exportData = data.data.map(server => ({
                    name: server.name,
                    host: server.host,
                    port: server.port,
                    username: server.username,
                    auth_type: server.auth_type,
                    tags: server.tags,
                    description: server.description
                    // 不导出密码和私钥
                }));

                const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' });
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = `servers_${new Date().toISOString().slice(0, 10)}.json`;
                a.click();
                URL.revokeObjectURL(url);
                this.showGlobalToast('导出成功', 'success');
            }
        } catch (error) {
            console.error('导出失败:', error);
            this.showGlobalToast('导出失败', 'error');
        }
    },

    // ==================== 主机列表展开相关 ====================

    isServerExpanded(serverId) {
        return this.expandedServers.includes(serverId);
    },

    async toggleServer(serverId) {
        const index = this.expandedServers.indexOf(serverId);
        if (index !== -1) {
            // 收起：从数组中移除
            this.expandedServers.splice(index, 1);
        } else {
            // 展开：添加到数组
            this.expandedServers.push(serverId);

            const server = this.serverList.find(s => s.id === serverId);
            if (!server) return;

            // 延迟加载历史指标图表，避免展开瞬间卡顿
            setTimeout(() => this.loadCardMetrics(serverId), 300);

            // 不再强制刷新数据 - 实时数据由 Socket.IO 推送
        }
    },

    async loadServerInfo(serverId, force = false, silent = false) {
        const server = this.serverList.find(s => s.id === serverId);
        if (!server) return;

        if (force) {
            server.info = null; // 强制刷新时清除旧数据，显示 loading
        }

        if (!silent && !server.info) {
            server.loading = true;
        }

        try {
            const response = await fetch('/api/server/info', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ serverId, force })
            });

            const data = await response.json();
            if (data.success) {
                server.info = { ...data };
                server.error = null;

                if (data.is_cached && !force) {
                    setTimeout(() => this.loadServerInfo(serverId, true, true), 300);
                }
            } else {
                server.error = data.error || '加载失败';
            }
        } catch (error) {
            console.error('加载主机信息失败:', error);
            server.error = error.message;
        } finally {
            if (!silent) {
                server.loading = false;
            }
        }
    },

    async refreshServerInfo(serverId) {
        const server = this.serverList.find(s => s.id === serverId);
        if (server) {
            if (server.loading) return;
            await this.loadServerInfo(serverId, true, false);
        }
    },

    async loadServerList() {
        this.serverLoading = true;
        try {
            const response = await fetch('/api/server/accounts');
            const data = await response.json();

            if (data.success) {
                // 将主机数据存储到 serverList, 并保留现有的 info 等状态
                const existingServersMap = new Map(this.serverList.map(s => [s.id, s]));

                this.serverList = data.data.map(server => {
                    const existing = existingServersMap.get(server.id);
                    return {
                        ...server,
                        // 优先使用 API 返回的 info（首屏瞬显关键），如果本地已有且更新则保持本地
                        info: (existing && existing.info && !server.info) ? existing.info : server.info,
                        error: (existing && existing.error) ? existing.error : null,
                        loading: (existing && existing.loading) ? existing.loading : false
                    };
                });
            } else {
                // 处理错误情况
                console.error('加载主机列表失败:', data.error);
                if (data.error && data.error.includes('未认证')) {
                    // 认证错误,不显示toast,避免干扰用户
                    this.serverList = [];
                } else {
                    this.showGlobalToast('加载主机列表失败: ' + data.error, 'error');
                    this.serverList = [];
                }
            }
        } catch (error) {
            console.error('加载主机列表失败:', error);
            this.showGlobalToast('加载主机列表失败', 'error');
            this.serverList = [];
        } finally {
            this.serverLoading = false;
            // 1. 首先尝试连接实时指标推送流 (秒级监控)
            if (this.isAuthenticated && this.mainActiveTab === 'server') {
                this.connectMetricsStream();
            }

            // 2. 仅在 Socket.IO 未连接时才启动 HTTP 轮询作为降级
            // 注意: connectMetricsStream 是异步的, 这里延迟检查连接状态
            setTimeout(() => {
                if (!this.metricsWsConnected && !this.metricsWsConnecting) {
                    console.log('[Host] Socket.IO 未连接，启动 HTTP 轮询作为降级');
                    this.startServerPolling();
                }
            }, 3000);

            // 3. 进入页面时立即 ping 所有主机获取延迟
            this.pingAllServers();
        }
    },

    async pingAllServers() {
        if (this.serverList.length === 0) return;

        try {
            const response = await fetch('/api/server/ping-all', { method: 'POST' });
            const data = await response.json();

            if (data.success && data.results) {
                // 更新 serverList 中的延迟数据
                for (const result of data.results) {
                    const server = this.serverList.find(s => s.id === result.serverId);
                    if (server && result.success) {
                        server.response_time = result.latency;
                    }
                }
            }
        } catch (error) {
            console.warn('批量 ping 失败:', error);
        }
    },

    // ==================== Docker 相关 ====================

    async checkContainerUpdate(server, container) {
        if (container.checkingUpdate) return;

        container.checkingUpdate = true;
        this.$forceUpdate();

        try {
            const response = await fetch('/api/server/docker/check-update', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    serverId: server.id,
                    imageName: container.image
                })
            });

            const data = await response.json();

            if (data.success) {
                container.updateAvailable = data.data.updateAvailable;

                if (data.data.updateAvailable) {
                    this.showGlobalToast(`容器 ${container.name} 有新版本可用`, 'success');
                } else {
                    this.showGlobalToast(`容器 ${container.name} 已是最新`, 'info');
                }
            } else {
                this.showGlobalToast('检测失败: ' + (data.error || data.message), 'error');
            }
        } catch (error) {
            console.error('检测更新失败:', error);
            this.showGlobalToast('检测请求失败', 'error');
        } finally {
            container.checkingUpdate = false;
            this.$forceUpdate();
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

    getRunningContainers(containers) {
        if (!containers || !Array.isArray(containers)) return 0;
        return containers.filter(c => c.status && c.status.includes('Up') && !c.status.includes('Paused')).length;
    },

    getPausedContainers(containers) {
        if (!containers || !Array.isArray(containers)) return 0;
        return containers.filter(c => c.status && c.status.includes('Paused')).length;
    },

    getStoppedContainers(containers) {
        if (!containers || !Array.isArray(containers)) return 0;
        return containers.filter(c => c.status && !c.status.includes('Up')).length;
    },

    toggleDockerPanel(serverId) {
        if (this.expandedDockerPanels.has(serverId)) {
            this.expandedDockerPanels.delete(serverId);
        } else {
            this.expandedDockerPanels.add(serverId);
        }
        this.expandedDockerPanels = new Set(this.expandedDockerPanels);
    },

    isDockerPanelExpanded(serverId) {
        return this.expandedDockerPanels.has(serverId);
    },

    async handleDockerAction(serverId, containerId, action) {
        const server = this.serverList.find(s => s.id === serverId);
        if (server) server.loading = true;

        try {
            const response = await fetch('/api/server/docker/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ serverId, containerId, action })
            });
            const data = await response.json();
            if (data.success) {
                this.showGlobalToast('Docker 操作已执行', 'success');
                // 延迟刷新以等待同步
                setTimeout(() => this.loadServerInfo(serverId), 1000);
            } else {
                this.showGlobalToast('操作失败: ' + data.message, 'error');
            }
        } catch (error) {
            this.showGlobalToast('Docker 操作异常', 'error');
        } finally {
            if (server) server.loading = false;
        }
    },

    loadFromServerListCache() {
        try {
            const cached = localStorage.getItem('cached_server_list');
            if (cached) {
                const servers = JSON.parse(cached);
                if (Array.isArray(servers) && servers.length > 0) {
                    console.log('[Host] Loaded servers from cache');
                    this.serverList = servers;
                }
            }
        } catch (e) {
            console.warn('Failed to load server cache:', e);
        }
    },

    // ==================== 凭据管理 ====================

    async loadCredentials() {
        try {
            const response = await fetch('/api/server/credentials');
            const data = await response.json();
            if (data.success) {
                this.serverCredentials = data.data;
            }
        } catch (error) {
            console.error('加载凭据失败:', error);
        }
    },

    async saveCredential() {
        this.credError = '';
        if (!this.credForm.name || !this.credForm.username) {
            this.credError = '请填写完整信息';
            return;
        }
        try {
            const response = await fetch('/api/server/credentials', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(this.credForm)
            });
            const data = await response.json();
            if (data.success) {
                this.showGlobalToast('凭据已保存', 'success');
                this.showAddCredentialModal = false;
                this.credForm = { name: '', username: '', password: '' };
                this.credError = '';
                await this.loadCredentials();
            } else {
                this.credError = data.error || '保存失败';
            }
        } catch (error) {
            this.credError = '保存失败: ' + error.message;
            this.showGlobalToast('保存失败', 'error');
        }
    },

    async deleteCredential(id) {
        const confirmed = await this.showConfirm({
            title: '删除凭据',
            message: '确定删除此凭据吗？',
            icon: 'fa-trash',
            confirmText: '删除',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;
        try {
            const response = await fetch(`/api/server/credentials/${id}`, {
                method: 'DELETE'
            });
            const data = await response.json();
            if (data.success) {
                this.showGlobalToast('凭据已删除', 'success');
                await this.loadCredentials();
            }
        } catch (error) {
            this.showGlobalToast('删除失败', 'error');
        }
    },

    async setDefaultCredential(id) {
        const confirmed = await this.showConfirm({
            title: '设为默认',
            message: '确定将此凭据设为默认吗？',
            icon: 'fa-star',
            confirmText: '确定',
            confirmClass: 'btn-primary'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/server/credentials/${id}/default`, {
                method: 'PUT'
            });

            if (response.status === 404) {
                this.showGlobalToast('接口未更新，请刷新页面或重启服务', 'error');
                return;
            }

            const data = await response.json();
            if (data.success) {
                this.showGlobalToast('已设置为默认凭据', 'success');
                await this.loadCredentials();
            } else {
                this.showGlobalToast('设置失败: ' + data.error, 'error');
            }
        } catch (error) {
            this.showGlobalToast('设置失败', 'error');
        }
    },

    applyCredential(event) {
        const id = event.target.value;
        if (!id) return;
        const cred = this.serverCredentials.find(c => c.id == id);
        if (cred) {
            this.serverForm.username = cred.username;
            this.serverForm.password = cred.password;
            this.serverForm.authType = 'password';
        }
    },

    // ==================== 监控配置 ====================

    async loadMonitorConfig() {
        try {
            const response = await fetch('/api/server/monitor/config');
            const data = await response.json();

            if (data.success && data.data) {
                this.monitorConfig = {
                    interval: data.data.probe_interval || 60,
                    timeout: data.data.probe_timeout || 10,
                    logRetentionDays: data.data.log_retention_days || 7
                };
                this.startServerPolling(); // 加载配置后启动轮询
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
                    probe_interval: parseInt(this.monitorConfig.interval),
                    probe_timeout: parseInt(this.monitorConfig.timeout),
                    log_retention_days: parseInt(this.monitorConfig.logRetentionDays)
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

    // ==================== 辅助函数 ====================

    getMemoryClass(usage) {
        const percent = parseFloat(usage);
        if (percent > 90) return 'danger';
        if (percent > 75) return 'warning';
        return '';
    },

    getDiskClass(usage) {
        const percent = parseFloat(usage);
        if (percent > 90) return 'danger';
        if (percent > 75) return 'warning';
        return '';
    },

    formatUptime(uptimeStr) {
        if (!uptimeStr || typeof uptimeStr !== 'string') return uptimeStr;

        // 移除 "up " 前缀
        let str = uptimeStr.replace(/^up\s+/i, '');

        // 提取各个时间部分
        const weekMatch = str.match(/(\d+)\s*weeks?/i);
        const dayMatch = str.match(/(\d+)\s*days?/i);
        const hourMatch = str.match(/(\d+)\s*hours?/i);
        const minMatch = str.match(/(\d+)\s*minutes?/i);

        let days = dayMatch ? parseInt(dayMatch[1], 10) : 0;
        const weeks = weekMatch ? parseInt(weekMatch[1], 10) : 0;
        const hours = hourMatch ? parseInt(hourMatch[1], 10) : 0;
        const minutes = minMatch ? parseInt(minMatch[1], 10) : 0;

        // 将周转换为天并累加
        days += weeks * 7;

        // 构建中文格式
        let result = '';
        if (days > 0) result += `${days}天`;
        if (hours > 0) result += `${hours}时`;
        if (minutes > 0) result += `${minutes}分`;

        // 如果都是0，显示 "0分"
        if (result === '') result = '0分';

        return result;
    },

    translateInfoKey(key) {
        const translations = {
            // 系统信息
            'OS': '操作系统',
            'Kernel': '内核版本',
            'Architecture': '架构',
            'Hostname': '主机名',
            'Uptime': '运行时间',
            // CPU 信息
            'Model': '型号',
            'Cores': '核心数',
            'Usage': '使用率',
            // 内存信息
            'Total': '总计',
            'Used': '已用',
            'Free': '可用',
            // 其他
            'Version': '版本'
        };
        return translations[key] || key;
    },

    // ==================== Agent 部署 ====================

    /**
     * 显示 Agent 安装命令弹窗
     */
    async showAgentInstallModal(serverId) {
        const server = this.serverList.find(s => s.id === serverId);
        if (!server) {
            this.showGlobalToast('服务器不存在', 'error');
            return;
        }

        this.agentInstallLoading = true;
        this.agentInstallLog = '';
        this.agentInstallResult = null;
        this.showAgentModal = true;
        this.agentModalData = {
            serverId,
            serverName: server.name,
            installCommand: '',
            apiUrl: '',
            agentKey: ''
        };

        try {
            const response = await fetch(`/api/server/agent/command/${serverId}`);
            const data = await response.json();

            if (data.success) {
                this.agentModalData = {
                    ...this.agentModalData,
                    ...data.data
                };
            } else {
                this.showGlobalToast('获取安装命令失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('获取 Agent 安装命令失败:', error);
            this.showGlobalToast('获取安装命令失败', 'error');
        } finally {
            this.agentInstallLoading = false;
        }
    },

    closeAgentModal() {
        this.showAgentModal = false;
        this.agentModalData = null;
    },

    /**
     * 复制安装命令到剪贴板
     */
    async copyAgentCommand() {
        const command = this.agentInstallOS === 'linux'
            ? this.agentModalData?.installCommand
            : this.agentModalData?.winInstallCommand;

        if (!command) return;

        try {
            await navigator.clipboard.writeText(command);
            this.showGlobalToast('安装命令已复制到剪贴板', 'success');
        } catch (error) {
            // 降级方案
            const textarea = document.createElement('textarea');
            textarea.value = command;
            document.body.appendChild(textarea);
            textarea.select();
            document.execCommand('copy');
            document.body.removeChild(textarea);
            this.showGlobalToast('安装命令已复制', 'success');
        }
    },

    /**
     * 重新生成 Agent 密钥 (全局统一密钥)
     */
    async regenerateAgentKey(serverId) {
        try {
            const response = await fetch(`/api/server/agent/regenerate-key`, {
                method: 'POST'
            });
            const data = await response.json();

            if (data.success) {
                this.showGlobalToast('全局密钥已重新生成', 'success');
                // 如果是从单个安装弹窗触发，刷新弹窗数据
                if (serverId) {
                    await this.showAgentInstallModal(serverId);
                }
            } else {
                this.showGlobalToast('重新生成失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('重新生成密钥失败:', error);
            this.showGlobalToast('重新生成失败', 'error');
        }
    },

    /**
     * 自动安装 Agent（通过 SSH）
     */
    async autoInstallAgent(serverId) {
        this.agentInstallLoading = true;
        this.agentInstallLog = '正在连接服务器并安装 Agent...\n';
        this.agentInstallResult = null;

        try {
            const response = await fetch(`/api/server/agent/auto-install/${serverId}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' }
            });
            const data = await response.json();

            if (data.success) {
                this.agentInstallLog += (data.output || '') + '\n\n✅ Agent 安装成功！';
                this.agentInstallResult = 'success';
                this.showGlobalToast('Agent 安装成功！', 'success');
            } else {
                this.agentInstallLog += (data.output || '') + '\n\n❌ 安装失败: ' + (data.error || '未知错误');
                this.agentInstallResult = 'error';
                this.showGlobalToast('安装失败: ' + (data.error || '未知错误'), 'error');
            }
        } catch (error) {
            console.error('自动安装 Agent 失败:', error);
            this.agentInstallLog += '\n❌ 网络错误: ' + error.message;
            this.agentInstallResult = 'error';
            this.showGlobalToast('安装失败: ' + error.message, 'error');
        } finally {
            this.agentInstallLoading = false;
        }
    },

    /**
     * 卸载 Agent（通过 SSH）
     */
    async uninstallAgent(serverId) {
        const confirmed = await this.showConfirm({
            title: '卸载 Agent',
            message: '确定要从目标服务器上卸载 Agent 吗？',
            icon: 'fa-trash',
            confirmText: '确定卸载',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        this.agentInstallLoading = true;

        try {
            const response = await fetch(`/api/server/agent/uninstall/${serverId}`, {
                method: 'POST'
            });
            const data = await response.json();

            if (data.success) {
                this.showGlobalToast('Agent 已卸载', 'success');
                this.closeAgentModal();
            } else {
                this.showGlobalToast('卸载失败: ' + (data.error || '未知错误'), 'error');
            }
        } catch (error) {
            console.error('卸载 Agent 失败:', error);
            this.showGlobalToast('卸载失败: ' + error.message, 'error');
        } finally {
            this.agentInstallLoading = false;
        }
    },

    /**
     * 更新服务器监控模式
     */
    async updateMonitorMode(serverId, mode) {
        try {
            const response = await fetch(`/api/server/accounts/${serverId}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ monitor_mode: mode })
            });
            const data = await response.json();

            if (data.success) {
                // 更新本地状态
                const server = this.serverList.find(s => s.id === serverId);
                if (server) {
                    server.monitor_mode = mode;
                }

                const modeNames = { ssh: 'SSH', agent: 'Agent', both: '双模式' };
                this.showGlobalToast(`监控模式已切换为 ${modeNames[mode]}`, 'success');
            } else {
                this.showGlobalToast('切换失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('更新监控模式失败:', error);
            this.showGlobalToast('切换失败', 'error');
        }
    },

    /**
     * 显示批量 Agent 安装弹窗
     */
    showBatchAgentInstallModal() {
        this.selectedBatchServers = [];
        this.batchInstallResults = [];
        this.showBatchAgentModal = true;
    },

    /**
     * 关闭批量 Agent 安装弹窗
     */
    closeBatchAgentModal() {
        if (this.agentInstallLoading) return;
        this.showBatchAgentModal = false;
    },

    /**
     * 切换批量服务器选中状态
     */
    toggleBatchServerSelection(serverId) {
        const index = this.selectedBatchServers.indexOf(serverId);
        if (index === -1) {
            this.selectedBatchServers.push(serverId);
        } else {
            this.selectedBatchServers.splice(index, 1);
        }
    },

    /**
     * 全选所有服务器进行批量安装
     */
    selectAllBatchServers() {
        this.selectedBatchServers = this.serverList.map(s => s.id);
    },

    /**
     * 执行批量 Agent 安装
     */
    async runBatchAgentInstall() {
        if (this.selectedBatchServers.length === 0) return;

        // 直接开始安装，无需确认

        this.agentInstallLoading = true;
        this.batchInstallResults = this.selectedBatchServers.map(id => {
            const server = this.serverList.find(s => s.id === id);
            return {
                serverId: id,
                serverName: server ? server.name : '未知主机',
                status: 'waiting',
                error: null
            };
        });

        try {
            // 我们采用逐个调用的方式，以便在 UI 上展示每个的进度，或者直接调用后端的批量接口
            // 考虑到 UX，逐个调用能看到实时的"处理中"状态
            for (let i = 0; i < this.batchInstallResults.length; i++) {
                const item = this.batchInstallResults[i];
                item.status = 'processing';

                try {
                    const response = await fetch(`/api/server/agent/auto-install/${item.serverId}`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' }
                    });
                    const data = await response.json();

                    if (data.success) {
                        item.status = 'success';
                    } else {
                        item.status = 'failed';
                        item.error = data.error || '安装失败';
                    }
                } catch (err) {
                    item.status = 'failed';
                    item.error = err.message;
                }
            }

            this.showGlobalToast('批量 Agent 部署任务已完成', 'info');
        } catch (error) {
            console.error('批量安装 Agent 任务异常:', error);
            this.showGlobalToast('批量任务执行异常', 'error');
        } finally {
            this.agentInstallLoading = false;
        }
    }
};
