import { store } from '../store.js';
import { toast } from './toast.js';

export const flyMethods = {
  // 缓存数据到本地
  saveToFlyCache(data) {
    try {
      const cacheKey = 'fly_data_snapshots';
      let history = [];
      const saved = localStorage.getItem(cacheKey);
      if (saved) {
        history = JSON.parse(saved);
      }

      history.unshift({
        timestamp: Date.now(),
        accounts: data,
      });

      if (history.length > 4) {
        history = history.slice(0, 4);
      }

      localStorage.setItem(cacheKey, JSON.stringify(history));
    } catch (e) {}
  },

  // 从本地缓存加载
  loadFromFlyCache() {
    try {
      const cacheKey = 'fly_data_snapshots';
      const saved = localStorage.getItem(cacheKey);
      if (saved) {
        const history = JSON.parse(saved);
        if (history && history.length > 0) {
          store.flyAccounts = history[0].accounts;
          return true;
        }
      }
    } catch (e) {}
    return false;
  },

  // 加载 Fly.io 账号列表（用于管理）
  async loadFlyManagedAccounts() {
    try {
      const response = await fetch('/api/flyio/accounts', {
        headers: store.getAuthHeaders(),
      });

      const contentType = response.headers.get('content-type');
      if (!contentType || !contentType.includes('application/json')) {
        console.error('Fly.io 账号请求返回了非 JSON 内容:', await response.text());
        return;
      }

      const result = await response.json();
      if (result.success) {
        store.flyManagedAccounts = result.data;
      }
    } catch (error) {
      console.error('加载 Fly.io 账号失败:', error);
    }
  },

  // 加载 Fly.io 监控数据（用于 Dashboard）
  async loadFlyData(isManual = false) {
    if (store.flyRefreshing) return;

    store.flyRefreshing = true;
    store.flyLoading = true;

    // 重置倒计时
    const intervalSeconds = (store.flyRefreshInterval || 30000) / 1000;
    store.flyRefreshCountdown = intervalSeconds;
    store.flyRefreshProgress = 100;

    try {
      const response = await fetch('/api/flyio/proxy/apps', {
        headers: store.getAuthHeaders(),
      });

      const contentType = response.headers.get('content-type');
      if (!contentType || !contentType.includes('application/json')) {
        throw new Error('服务器返回了无效的数据格式 (HTML)');
      }

      const result = await response.json();

      if (result.success) {
        // 转换数据结构适配 UI
        const formattedAccounts = (result.data || []).map(acc => ({
          name: acc.accountName,
          id: acc.accountId,
          projects: (acc.apps || []).map(app => ({
            id: app.id,
            name: app.name,
            status: app.status, // deployed, suspended, etc.
            region: app.machines?.nodes?.length > 0 ? app.machines.nodes[0].region : '',
            appUrl: app.appUrl,
            deployed: app.deployed,
            hostname: app.hostname,
            machines: app.machines?.nodes || [],
            ips: (app.ipAddresses?.nodes || []).map(ip => ({
              address: ip.address,
              type: ip.type,
            })),
            domains: (app.certificates?.nodes || []).map(cert => ({
              domain: cert.hostname,
              status: cert.clientStatus,
              isVerified: cert.clientStatus === 'Ready',
            })),
            services: [], // Fly apps are essentially services themselves
          })),
          error: acc.error,
        }));

        store.flyAccounts = formattedAccounts;
        this.saveToFlyCache(formattedAccounts);
        store.flyLastUpdate = new Date().toLocaleTimeString();

        if (isManual) {
          toast.success('Fly.io 数据已刷新');
        }
      }
    } catch (error) {
      console.error('Fly.io 数据刷新失败:', error);
      if (isManual) {
        toast.error('刷新失败: ' + error.message);
      }
    } finally {
      store.flyLoading = false;
      store.flyRefreshing = false;
    }
  },

  // 添加 Fly.io 账号
  async addFlyAccount() {
    store.flyAddAccountError = '';
    store.flyAddAccountSuccess = '';

    if (!store.newFlyAccount.name || !store.newFlyAccount.token) {
      store.flyAddAccountError = '请填写账号名称和 API Token';
      return;
    }

    store.flyAddingAccount = true;

    try {
      const response = await fetch('/api/flyio/accounts', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({
          name: store.newFlyAccount.name,
          api_token: store.newFlyAccount.token,
        }),
      });

      const contentType = response.headers.get('content-type');
      if (!contentType || !contentType.includes('application/json')) {
        throw new Error('服务器返回了无效的数据格式 (HTML)');
      }

      const result = await response.json();

      if (result.success) {
        store.flyAddAccountSuccess = '✅ 账号添加成功';
        store.newFlyAccount = { name: '', token: '' };
        await this.loadFlyManagedAccounts();
        this.loadFlyData(); // 刷新监控数据

        setTimeout(() => {
          store.flyAddAccountSuccess = '';
          store.showAddFlyAccountModal = false;
        }, 1500);
      } else {
        store.flyAddAccountError = result.error || '添加失败';
      }
    } catch (error) {
      store.flyAddAccountError = '请求失败: ' + error.message;
    } finally {
      store.flyAddingAccount = false;
    }
  },

  // 删除 Fly.io 账号
  async removeFlyAccount(id, name) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除 Fly.io 账号 "${name}" 吗？`,
      icon: 'fa-exclamation-triangle',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/flyio/accounts/${id}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
      });

      const result = await response.json();
      if (result.success) {
        toast.success(`账号 "${name}" 已删除`);
        await this.loadFlyManagedAccounts();
        this.loadFlyData();
      } else {
        toast.error('删除失败: ' + result.error);
      }
    } catch (error) {
      toast.error('操作失败: ' + error.message);
    }
  },

  // 自动刷新逻辑
  startFlyAutoRefresh() {
    if (this.flyRefreshTimer) clearInterval(this.flyRefreshTimer);
    if (this.flyCountdownTimer) clearInterval(this.flyCountdownTimer);

    const intervalSeconds = (store.flyRefreshInterval || 30000) / 1000;
    store.flyRefreshCountdown = intervalSeconds;
    store.flyRefreshProgress = 100;

    this.flyRefreshTimer = setInterval(() => {
      if (document.visibilityState === 'visible' && !store.flyDataRefreshPaused) {
        this.loadFlyData();
      }
    }, store.flyRefreshInterval || 30000);

    this.flyCountdownTimer = setInterval(() => {
      if (document.visibilityState === 'visible' && !store.flyDataRefreshPaused) {
        store.flyRefreshCountdown--;
        if (store.flyRefreshCountdown <= 0) {
          store.flyRefreshCountdown = intervalSeconds;
          store.flyRefreshProgress = 100;
        } else {
          store.flyRefreshProgress = (store.flyRefreshCountdown / intervalSeconds) * 100;
        }
      }
    }, 1000);
  },

  stopFlyAutoRefresh() {
    if (this.flyRefreshTimer) {
      clearInterval(this.flyRefreshTimer);
      this.flyRefreshTimer = null;
    }
    if (this.flyCountdownTimer) {
      clearInterval(this.flyCountdownTimer);
      this.flyCountdownTimer = null;
    }
  },

  toggleFlyDataRefresh() {
    store.flyDataRefreshPaused = !store.flyDataRefreshPaused;
    if (store.flyDataRefreshPaused) {
      this.stopFlyAutoRefresh();
    } else {
      this.startFlyAutoRefresh();
    }
  },

  toggleFlyAccount(accountName) {
    if (!store.flyExpandedAccounts[accountName]) {
      store.flyExpandedAccounts[accountName] = true;
    } else {
      store.flyExpandedAccounts[accountName] = !store.flyExpandedAccounts[accountName];
    }
  },

  isFlyAccountExpanded(accountName) {
    if (store.flyExpandedAccounts[accountName] === undefined) {
      return window.innerWidth > 768; // 手机端默认折叠
    }
    return store.flyExpandedAccounts[accountName];
  },

  // 重新部署应用 (触发新 Release)
  async redeployFlyApp(account, app) {
    const confirmed = await store.showConfirm({
      title: '确认重新部署',
      message: `确定要为 Fly.io 应用 "${app.name}" 触发一次新部署吗？这将创建一个新的发布版本（仅重启现有容器）。`,
      icon: 'fa-rocket',
      confirmText: '确定部署',
      confirmClass: 'btn-primary',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/redeploy`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });

      const result = await response.json();
      if (result.success) {
        toast.success('重新部署已开始');
        this.loadFlyData();
      } else {
        toast.error('部署失败: ' + result.error);
      }
    } catch (error) {
      toast.error('操作失败: ' + error.message);
    }
  },

  // 更新容器镜像 (实现类似 fly deploy 的功能)
  async updateFlyAppImage(account, app) {
    let currentImage = '';
    store.flyLoading = true;

    try {
      // 1. 无论如何先获取最新的 machines 信息以确保拿到当前配置
      const response = await fetch(
        `/api/flyio/apps/${app.name}/machines?accountId=${account.id}`,
        {
          headers: store.getAuthHeaders(),
        }
      );
      const result = await response.json();
      if (result.success && result.data.length > 0) {
        app.machines = result.data;
        currentImage = app.machines[0].config?.image || '';
      }
    } catch (e) {
      console.warn('获取当前镜像失败:', e);
    } finally {
      store.flyLoading = false;
    }

    // 智能建议：如果是 image:v123，建议 image:latest
    let suggestedImage = currentImage;
    if (currentImage && currentImage.includes(':')) {
      suggestedImage = currentImage.split(':')[0] + ':latest';
    } else if (currentImage) {
      suggestedImage = currentImage + ':latest';
    }

    const promptResult = await store.showPrompt({
      title: '更新容器镜像',
      message: currentImage 
        ? `当前正在运行：\n${currentImage}\n\n确认要更新为以下镜像吗？`
        : `未能自动检测到当前镜像。请输入完整的镜像地址：`,
      promptValue: suggestedImage || currentImage, 
      placeholder: '例如: registry.fly.io/my-app:latest',
      icon: 'fa-ship',
    });

    if (promptResult.action !== 'confirm' || !promptResult.value || promptResult.value.trim() === '') {
      return;
    }

    const image = promptResult.value.trim();
    store.flyRefreshing = true; // 显示加载状态
    console.log(`[Fly.io] 开始为应用 ${app.name} 更新镜像: ${image.trim()}`);

    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/update-image`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({
          accountId: account.id,
          image: image.trim(),
        }),
      });

      const result = await response.json();
      console.log(`[Fly.io] 更新请求响应:`, result);

      if (result.success) {
        toast.success(`镜像更新已提交：成功更新 ${result.updated} 个实例，失败 ${result.failed} 个`);
        this.loadFlyData();
      } else {
        toast.error('更新失败: ' + (result.error || '未知错误'));
        console.error(`[Fly.io] 更新镜像业务失败:`, result);
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
      console.error(`[Fly.io] 更新镜像请求异常:`, error);
    } finally {
      store.flyRefreshing = false;
    }
  },

  // 一键更新账号下所有应用的镜像
  async updateAllFlyAppsImage(account) {
    const confirmed = await store.showConfirm({
      title: '确认批量更新',
      message: `确定要为账号 "${account.name}" 下的所有应用触发镜像更新吗？\n\n这相当于对所有应用执行一次 "fly deploy"（默认拉取 :latest 标签）。`,
      icon: 'fa-layer-group',
      confirmText: '确定批量更新',
      confirmClass: 'btn-primary',
    });

    if (!confirmed) return;

    store.flyRefreshing = true;
    toast.info('正在触发批量更新，请稍候...');

    try {
      const response = await fetch(`/api/flyio/accounts/${account.id}/update-all-images`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({ image: 'latest' }) // 默认批量更新为 latest
      });

      const result = await response.json();
      if (result.success) {
        toast.success(`批量更新已完成：成功 ${result.updated} 个，失败 ${result.failed} 个`);
        this.loadFlyData();
      } else {
        toast.error('批量更新失败: ' + result.error);
      }
    } catch (error) {
      toast.error('请求异常: ' + error.message);
    } finally {
      store.flyRefreshing = false;
    }
  },

  // 开始重命名应用
  startEditFlyAppName(app) {
    app.editingName = app.name;
    app.isEditing = true;
    this.$nextTick(() => {
      const inputs = this.$refs.flyAppNameInput;
      if (inputs) {
        const input = Array.isArray(inputs) ? inputs.find(el => el) : inputs;
        if (input) {
          input.focus();
          input.select();
        }
      }
    });
  },

  // 提交重命名
  async saveFlyAppName(account, app) {
    const newName = app.editingName?.trim();
    if (!newName || newName === app.name) {
      app.isEditing = false;
      return;
    }

    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/rename`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({
          accountId: account.id,
          newName: newName,
        }),
      });

      const result = await response.json();
      if (result.success) {
        toast.success('应用已成功重命名');
        app.name = newName;
        app.isEditing = false;
        this.loadFlyData();
      } else {
        toast.error('重命名失败: ' + result.error);
      }
    } catch (error) {
      toast.error('操作失败: ' + error.message);
    }
  },

  // 取消重命名
  cancelEditFlyAppName(app) {
    app.isEditing = false;
    app.editingName = '';
  },

  // 删除应用
  async deleteFlyApp(account, app) {
    const confirmed = await store.showConfirm({
      title: '⚠️ 确认删除应用',
      message: `确定要永久删除 Fly.io 应用 "${app.name}" 吗？此操作不可恢复，且会销毁所有底层 Machine。`,
      icon: 'fa-trash-alt',
      confirmText: '永久删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/flyio/apps/${app.name}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });

      const result = await response.json();
      if (result.success) {
        toast.success('应用已成功删除');
        this.loadFlyData();
      } else {
        toast.error('删除失败: ' + result.error);
      }
    } catch (error) {
      toast.error('操作失败: ' + error.message);
    }
  },

  // 创建应用
  async createFlyApp(account) {
    const name = await store.showPrompt({
      title: '创建新应用',
      message: '请输入新应用的名称 (留空则由系统随机生成)：',
      placeholder: '例如: my-new-api',
      icon: 'fa-plus-circle',
    });

    if (name === null) return; // 取消操作

    try {
      const response = await fetch('/api/flyio/apps', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({
          accountId: account.id,
          name: name.trim() || undefined,
        }),
      });

      const result = await response.json();
      if (result.success) {
        toast.success(`应用 ${result.data.name} 已创建`);
        this.loadFlyData();
      } else {
        toast.error('创建失败: ' + result.error);
      }
    } catch (error) {
      toast.error('操作失败: ' + error.message);
    }
  },
  // 获取实例详情
  async fetchFlyMachines(account, app) {
    if (app.showMachines) {
      app.showMachines = false;
      return;
    }

    app.loadingMachines = true;
    app.showMachines = true;

    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/machines?accountId=${account.id}`, {
        headers: store.getAuthHeaders(),
      });
      const result = await response.json();

      if (result.success) {
        app.machines = result.data;
      }
    } catch (error) {
      console.error('获取实例失败:', error);
    } finally {
      app.loadingMachines = false;
    }
  },

  // 显示日志 (容器 stdout/stderr + 系统事件)
  async showFlyAppLogs(account, app) {
    this.openLogViewer({
      title: `容器日志: ${app.name}`,
      subtitle: `Fly.io / ${account.name}`,
      source: 'fly',
      fetcher: async () => {
        try {
          // 优先获取容器实时日志 (stdout/stderr)
          const response = await fetch(`/api/flyio/apps/${app.name}/logs?accountId=${account.id}`, {
            headers: store.getAuthHeaders(),
          });
          const result = await response.json();

          if (result.success && result.data && result.data.length > 0) {
            return result.data.map(l => ({
              timestamp: l.timestamp ? new Date(l.timestamp).getTime() : Date.now(),
              message: l.message,
              level: l.level,
              meta: l.instance ? `${l.region} | ${l.instance.substring(0, 8)}` : l.region
            }));
          }

          // 如果没有容器日志，自动回退到获取系统事件 (Release 记录等)
          const eventResponse = await fetch(`/api/flyio/apps/${app.name}/events?accountId=${account.id}`, {
            headers: store.getAuthHeaders(),
          });
          const eventResult = await eventResponse.json();

          if (eventResult.success && eventResult.data.length > 0) {
            return eventResult.data;
          }

          return [{ timestamp: Date.now(), message: '暂无容器日志或系统事件。' }];
        } catch (e) {
          return [{ timestamp: Date.now(), message: '获取日志失败: ' + e.message }];
        }
      },
    });
  },

  // 导出所有账号
  async exportFlyAccounts() {
    try {
      if (store.flyManagedAccounts.length === 0) {
        toast.warning('没有可导出的账号');
        return;
      }

      const now = new Date();
      const exportData = {
        version: '1.0',
        platform: 'fly',
        exportTime: now.toISOString(),
        accounts: store.flyManagedAccounts,
      };

      const dataStr = JSON.stringify(exportData, null, 2);
      const dataBlob = new Blob([dataStr], { type: 'application/json' });
      const url = URL.createObjectURL(dataBlob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `fly-accounts-${now.getTime()}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      toast.success('Fly.io 账号导出成功');
    } catch (error) {
      toast.error('导出失败: ' + error.message);
    }
  },

  // 导入所有账号
  async importFlyAccounts() {
    const confirmed = await store.showConfirm({
      title: '确认导入',
      message: '导入账号将覆盖当前 Fly.io 账号配置，是否继续？',
      icon: 'fa-exclamation-triangle',
      confirmText: '确定导入',
      confirmClass: 'btn-warning',
    });

    if (!confirmed) return;

    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = async event => {
      const file = event.target.files[0];
      if (!file) return;

      const reader = new FileReader();
      reader.onload = async e => {
        try {
          const importedData = JSON.parse(e.target.result);

          if (!importedData.accounts) {
            toast.error('无效的备份文件格式');
            return;
          }

          // 逐个添加账号以进行验证
          for (const acc of importedData.accounts) {
            await fetch('/api/flyio/accounts', {
              method: 'POST',
              headers: store.getAuthHeaders(),
              body: JSON.stringify({
                name: acc.name,
                api_token: acc.api_token || acc.token, // 兼容不同字段名
              }),
            });
          }

          toast.success('Fly.io 账号导入完成');
          await this.loadFlyManagedAccounts();
          this.loadFlyData();
        } catch (error) {
          toast.error('导入失败: ' + error.message);
        }
      };
      reader.readAsText(file);
    };
    input.click();
  },

  // 查看配置
  async viewFlyConfig(account, app) {
    this.openLogViewer({
      title: `应用配置: ${app.name}`,
      subtitle: `Fly.io / ${account.name}`,
      source: 'fly',
      fetcher: async () => {
        try {
          const response = await fetch(`/api/flyio/apps/${app.name}/config?accountId=${account.id}`, {
            headers: store.getAuthHeaders(),
          });
          const result = await response.json();

          if (result.success) {
            // 将 JSON 配置格式化为易读的字符串显示在日志查看器中
            const configStr = JSON.stringify(result.data, null, 2);
            return [
              { timestamp: Date.now(), message: '--- 当前激活配置 (JSON 格式) ---' },
              ...configStr.split('\n').map(line => ({
                timestamp: Date.now(),
                message: line,
              })),
            ];
          }
          throw new Error(result.error);
        } catch (e) {
          return [{ timestamp: Date.now(), message: '获取配置失败: ' + e.message }];
        }
      },
    });
  },

  // 状态样式辅助
  getFlyStatusClass(status) {
    status = status?.toLowerCase() || 'unknown';
    if (['deployed', 'running', 'started'].includes(status)) return 'status-running';
    if (['suspended', 'dead', 'stopped', 'destroyed'].includes(status)) return 'status-stopped';
    if (['pending', 'created'].includes(status)) return 'status-starting';
    return 'status-unknown';
  },

  getFlyStatusText(status) {
    const map = {
      deployed: '已部署',
      running: '运行中',
      suspended: '已暂停',
      dead: '已停止',
      pending: '部署中',
    };
    return map[status?.toLowerCase()] || status;
  },

  // 批量添加 Fly.io 账号
  async batchAddFlyAccounts() {
    if (!store.flyBatchAccounts || !store.flyBatchAccounts.trim()) {
      toast.warn('请输入账号信息');
      return;
    }

    const lines = store.flyBatchAccounts.trim().split('\n');
    const accounts = [];

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i].trim();
      if (!line) continue;

      const parts = line.split(/[:：]/);
      if (parts.length < 2) {
        toast.error(`第 ${i + 1} 行格式错误，应为 "名称:Token"`);
        return;
      }

      const name = parts[0].trim();
      const token = parts.slice(1).join(':').trim();

      if (!name || !token) {
        toast.error(`第 ${i + 1} 行名称或 Token 不能为空`);
        return;
      }

      accounts.push({ name, token });
    }

    store.flyAddingAccount = true;
    let successCount = 0;
    const errors = [];

    for (const acc of accounts) {
      try {
        const response = await fetch('/api/flyio/accounts', {
          method: 'POST',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({
            name: acc.name,
            api_token: acc.token,
          }),
        });

        const result = await response.json();
        if (result.success) {
          successCount++;
        } else {
          errors.push(`${acc.name}: ${result.error}`);
        }
      } catch (e) {
        errors.push(`${acc.name}: ${e.message}`);
      }
    }

    store.flyAddingAccount = false;

    if (successCount > 0) {
      toast.success(`成功添加 ${successCount} 个 Fly.io 账号`);
      store.flyBatchAccounts = '';
      await this.loadFlyManagedAccounts();
      this.loadFlyData();
    }

    if (errors.length > 0) {
      console.error('部分账号添加失败:', errors);
      toast.error(`${errors.length} 个账号添加失败，请查看控制台`);
    }
  },
};
