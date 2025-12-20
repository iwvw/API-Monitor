/**
 * Cloudflare Workers 管理模块
 */
const workersModule = {
    // 当前选中的账号 ID (复用 dns.js 的 store)
    // 当前选中的 Worker
    selectedWorker: null,

    // Workers 列表
    workers: [],

    // Worker 路由列表
    workerRoutes: [],

    // 编辑器内容
    editorContent: '',

    // 加载状态
    workersLoading: false,

    // CF Account ID 缓存
    cfAccountIdCache: {},

    /**
     * 加载账号下的所有 Workers
     */
    async loadWorkers() {
        if (!store.dnsSelectedAccountId) {
            toast.warning('请先选择一个账号');
            return;
        }

        this.workersLoading = true;
        try {
            const response = await fetch(`/api/cf-dns/accounts/${store.dnsSelectedAccountId}/workers`, {
                headers: store.getAuthHeaders()
            });
            const data = await response.json();

            if (response.ok) {
                this.workers = data.workers || [];
                this.cfAccountIdCache[store.dnsSelectedAccountId] = data.cfAccountId;

                // 如果有子域名，显示
                if (data.subdomain) {
                    store.workersSubdomain = data.subdomain;
                }
            } else {
                toast.error(data.error || '加载 Workers 失败');
            }
        } catch (error) {
            toast.error('加载 Workers 失败: ' + error.message);
        } finally {
            this.workersLoading = false;
        }
    },

    /**
     * 获取 Worker 脚本内容
     */
    async loadWorkerScript(scriptName) {
        if (!store.dnsSelectedAccountId) return;

        try {
            const response = await fetch(
                `/api/cf-dns/accounts/${store.dnsSelectedAccountId}/workers/${encodeURIComponent(scriptName)}`,
                { headers: store.getAuthHeaders() }
            );
            const data = await response.json();

            if (response.ok && data.worker) {
                this.selectedWorker = {
                    name: scriptName,
                    script: data.worker.script,
                    meta: data.worker.meta
                };
                this.editorContent = data.worker.script;
            } else {
                toast.error(data.error || '加载脚本失败');
            }
        } catch (error) {
            toast.error('加载脚本失败: ' + error.message);
        }
    },

    /**
     * 保存 Worker 脚本
     */
    async saveWorkerScript(scriptName, script, isNew = false) {
        if (!store.dnsSelectedAccountId) return false;

        try {
            const response = await fetch(
                `/api/cf-dns/accounts/${store.dnsSelectedAccountId}/workers/${encodeURIComponent(scriptName)}`,
                {
                    method: 'PUT',
                    headers: {
                        ...store.getAuthHeaders(),
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({ script })
                }
            );
            const data = await response.json();

            if (response.ok) {
                toast.success(isNew ? 'Worker 已创建' : 'Worker 已保存');
                await this.loadWorkers();
                return true;
            } else {
                toast.error(data.error || '保存失败');
                return false;
            }
        } catch (error) {
            toast.error('保存失败: ' + error.message);
            return false;
        }
    },

    /**
     * 删除 Worker
     */
    async deleteWorker(scriptName) {
        const confirmed = await store.showConfirm({
            title: '确认删除',
            message: `确定要删除 Worker "${scriptName}" 吗？此操作不可恢复。`,
            icon: 'fa-trash',
            confirmText: '删除',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(
                `/api/cf-dns/accounts/${store.dnsSelectedAccountId}/workers/${encodeURIComponent(scriptName)}`,
                {
                    method: 'DELETE',
                    headers: store.getAuthHeaders()
                }
            );
            const data = await response.json();

            if (response.ok) {
                toast.success('Worker 已删除');
                if (this.selectedWorker?.name === scriptName) {
                    this.selectedWorker = null;
                    this.editorContent = '';
                }
                await this.loadWorkers();
            } else {
                toast.error(data.error || '删除失败');
            }
        } catch (error) {
            toast.error('删除失败: ' + error.message);
        }
    },

    /**
     * 加载域名的 Worker 路由
     */
    async loadWorkerRoutes(zoneId) {
        if (!store.dnsSelectedAccountId || !zoneId) return;

        try {
            const response = await fetch(
                `/api/cf-dns/accounts/${store.dnsSelectedAccountId}/zones/${zoneId}/workers/routes`,
                { headers: store.getAuthHeaders() }
            );
            const data = await response.json();

            if (response.ok) {
                this.workerRoutes = data.routes || [];
            }
        } catch (error) {
            console.error('加载 Worker 路由失败:', error);
        }
    },

    /**
     * 创建 Worker 路由
     */
    async createWorkerRoute(zoneId, pattern, scriptName) {
        if (!store.dnsSelectedAccountId) return false;

        try {
            const response = await fetch(
                `/api/cf-dns/accounts/${store.dnsSelectedAccountId}/zones/${zoneId}/workers/routes`,
                {
                    method: 'POST',
                    headers: {
                        ...store.getAuthHeaders(),
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({ pattern, script: scriptName })
                }
            );
            const data = await response.json();

            if (response.ok) {
                toast.success('路由已创建');
                await this.loadWorkerRoutes(zoneId);
                return true;
            } else {
                toast.error(data.error || '创建路由失败');
                return false;
            }
        } catch (error) {
            toast.error('创建路由失败: ' + error.message);
            return false;
        }
    },

    /**
     * 删除 Worker 路由
     */
    async deleteWorkerRoute(zoneId, routeId) {
        if (!store.dnsSelectedAccountId) return;

        try {
            const response = await fetch(
                `/api/cf-dns/accounts/${store.dnsSelectedAccountId}/zones/${zoneId}/workers/routes/${routeId}`,
                {
                    method: 'DELETE',
                    headers: store.getAuthHeaders()
                }
            );

            if (response.ok) {
                toast.success('路由已删除');
                await this.loadWorkerRoutes(zoneId);
            } else {
                const data = await response.json();
                toast.error(data.error || '删除路由失败');
            }
        } catch (error) {
            toast.error('删除路由失败: ' + error.message);
        }
    },

    /**
     * 新建 Worker 对话框
     */
    openNewWorkerModal() {
        store.newWorkerName = '';
        store.newWorkerScript = `// 新建 Worker
export default {
  async fetch(request, env, ctx) {
    return new Response('Hello World!');
  },
};`;
        store.showNewWorkerModal = true;
    },

    /**
     * 关闭新建 Worker 对话框
     */
    closeNewWorkerModal() {
        store.showNewWorkerModal = false;
    },

    /**
     * 保存新 Worker
     */
    async saveNewWorker() {
        const name = store.newWorkerName?.trim();
        const script = store.newWorkerScript;

        if (!name) {
            toast.error('请输入 Worker 名称');
            return;
        }
        if (!script) {
            toast.error('请输入脚本内容');
            return;
        }

        // 验证名称格式
        if (!/^[a-z0-9-]+$/.test(name)) {
            toast.error('Worker 名称只能包含小写字母、数字和连字符');
            return;
        }

        const success = await this.saveWorkerScript(name, script, true);
        if (success) {
            this.closeNewWorkerModal();
            // 选中新创建的 Worker
            await this.loadWorkerScript(name);
        }
    },

    /**
     * 格式化日期
     */
    formatDate(dateStr) {
        if (!dateStr) return '-';
        const date = new Date(dateStr);
        return date.toLocaleString('zh-CN', {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit'
        });
    }
};

// 扩展到全局 store (如果需要)
if (typeof window !== 'undefined') {
    window.workersModule = workersModule;
}
