import { store } from '../store.js';
import { toast } from './toast.js';

export const zeaburMethods = {
  // 缓存数据到本地 (保留最新4个快照)
  saveToZeaburCache(data) {
    try {
      const cacheKey = 'zeabur_data_snapshots';
      let history = [];
      const saved = localStorage.getItem(cacheKey);
      if (saved) {
        history = JSON.parse(saved);
      }

      // 添加新快照到开头
      history.unshift({
        timestamp: Date.now(),
        accounts: data
      });

      // 仅保留最近 4 个
      if (history.length > 4) {
        history = history.slice(0, 4);
      }

      localStorage.setItem(cacheKey, JSON.stringify(history));
    } catch (e) {
      // 静默失败
    }
  },

  // 从本地缓存加载最新快照
  loadFromZeaburCache() {
    try {
      const cacheKey = 'zeabur_data_snapshots';
      const saved = localStorage.getItem(cacheKey);
      if (saved) {
        const history = JSON.parse(saved);
        if (history && history.length > 0) {
          store.accounts = history[0].accounts;
          return true;
        }
      }
    } catch (e) { }
    return false;
  },

  async loadManagedAccounts() {
    try {
      // 从主机加载账号
      const response = await fetch('/api/server-accounts', {
        headers: store.getAuthHeaders()
      });
      const accounts = await response.json();
      if (accounts && accounts.length > 0) {
        store.managedAccounts = accounts;

        // 在后台异步刷新账号余额信息，不阻塞页面显示
        this.refreshManagedAccountsBalance().catch(err => { });
      }
    } catch (error) { }
  },

  async refreshManagedAccountsBalance() {
      // 并行刷新所有账号的余额信息
      const promises = (store.managedAccounts || []).map(async (account, i) => {
        try {
          const response = await fetch('/api/validate-account', {
            method: 'POST',
            headers: store.getAuthHeaders(),
            body: JSON.stringify({
              accountName: account.name,
              apiToken: account.token
            })
          });

          if (response.ok) {
            const data = await response.json();
            // 更新账号信息
            store.managedAccounts[i] = {
              ...account,
              email: data.userData.email || data.userData.username || account.email,
              username: data.userData.username || account.username,
              balance: data.userData.credit ? data.userData.credit / 100 : 0,
              status: 'active'
            };
          } else {
            // 如果验证失败，标记为无效
            store.managedAccounts[i] = {
              ...account,
              status: 'invalid'
            };
          }
        } catch (error) {
          // 保持原有状态
          store.managedAccounts[i] = {
            ...account,
            status: account.status || 'unknown'
          };
        }
      });

      // 等待所有请求完成
      await Promise.all(promises);

      // 保存更新后的账号信息
      await this.saveManagedAccounts();
    },

  async saveManagedAccounts() {
      try {
        // 保存到主机
        const response = await fetch('/api/server-accounts', {
          method: 'POST',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({ accounts: store.managedAccounts })
        });
      } catch (error) { }
    },

    loadProjectCosts() {
      const saved = localStorage.getItem('zeabur_project_costs');
      if (saved) {
        store.projectCosts = JSON.parse(saved);
      }
    },

    startAutoRefresh() {
      try {
        if (this.refreshInterval) {
          clearInterval(this.refreshInterval);
        }
        if (this.countdownInterval) {
          clearInterval(this.countdownInterval);
        }

        // 获取刷新间隔（秒），默认为30
        const intervalSeconds = (store.zeaburRefreshInterval || 30000) / 1000;

        // 重置倒计时
        store.refreshCountdown = intervalSeconds;
        store.refreshProgress = 100;

        // 自动刷新 (仅在可见时触发)
        this.refreshInterval = setInterval(() => {
          if (document.visibilityState !== 'visible') return;
          this.fetchData();
        }, store.zeaburRefreshInterval || 30000);

        // 1s倒计时更新，到0时立即重置 (仅在可见时更新)
        this.countdownInterval = setInterval(() => {
          if (document.visibilityState !== 'visible') return;

          store.refreshCountdown--;

          if (store.refreshCountdown <= 0) {
            // 到0时立即重置，无动画
            store.refreshCountdown = intervalSeconds;
            store.refreshProgress = 100;
          } else {
            // 正常递减
            store.refreshProgress = (store.refreshCountdown / intervalSeconds) * 100;
          }
        }, 1000);
      } catch (e) { }
    },

    stopAutoRefresh() {
      if (this.refreshInterval) {
        clearInterval(this.refreshInterval);
        this.refreshInterval = null;
      }
      if (this.countdownInterval) {
        clearInterval(this.countdownInterval);
        this.countdownInterval = null;
      }
    },

  async fetchData(isManual = false) {
      // 节流：如果距离上次请求太短，跳过
      const now = Date.now();
      if (now - this.lastFetchAt < this.minFetchInterval) return;
      if (store.refreshing) return; // 防止并发请求

      this.lastFetchAt = now;
      store.refreshing = true;
      store.loading = true;

      // 手动刷新时重置倒计时
      const intervalSeconds = (store.zeaburRefreshInterval || 30000) / 1000;
      store.refreshCountdown = intervalSeconds;
      store.refreshProgress = 100;

      try {
        // 如果有账号，使用账号
        if (store.managedAccounts.length > 0) {
          // 清除账号中的手动余额，让主机使用 API 真实数据
          const accountsWithoutManualBalance = store.managedAccounts.map(acc => ({
            ...acc,
            balance: null // 不发送手动余额
          }));

          const [accountsRes, projectsRes] = await Promise.all([
            fetch('/api/temp-accounts', {
              method: 'POST',
              headers: store.getAuthHeaders(),
              body: JSON.stringify({ accounts: accountsWithoutManualBalance })
            }).then(r => r.json()),
            fetch('/api/temp-projects', {
              method: 'POST',
              headers: store.getAuthHeaders(),
              body: JSON.stringify({
                accounts: accountsWithoutManualBalance,
                projectCosts: {} // 不发送手动费用，让主机尝试从 API 获取
              })
            }).then(r => r.json())
          ]);

          // 使用Vue.set或直接重新赋值确保响应式更新
          store.accounts = [];
          this.$nextTick(() => {
            const accountsData = accountsRes.map((account, index) => {
              const projectData = projectsRes[index];
              return {
                ...account,
                projects: projectData.projects || []
              };
            });
            store.accounts = accountsData;
            // 保存到缓存
            this.saveToZeaburCache(accountsData);
          });
        } else {
          // 否则使用主机配置的账号
          const [accountsRes, projectsRes] = await Promise.all([
            fetch('/api/accounts').then(r => r.json()),
            fetch('/api/projects').then(r => r.json())
          ]);

          // 使用Vue.set或直接重新赋值确保响应式更新
          store.accounts = [];
          this.$nextTick(() => {
            const accountsData = accountsRes.map((account, index) => {
              const projectData = projectsRes[index];
              return {
                ...account,
                projects: projectData.projects || []
              };
            });
            store.accounts = accountsData;
            // 保存到缓存
            this.saveToZeaburCache(accountsData);
          });
        }
        if (isManual) {
          toast.success('Zeabur 数据已刷新');
        }
      } catch (error) {
        toast.error('获取数据失败: ' + error.message);
      } finally {
        store.loading = false;
        store.refreshing = false;
        // 强制重新渲染组件
        this.$forceUpdate();
      }
    },

    getBalanceClass(credit) {
      const balance = credit / 100;
      if (balance < 10) return 'critical';
      if (balance < 50) return 'low';
      return '';
    },

  async addAccountToList() {
      this.addAccountError = '';
      this.addAccountSuccess = '';

      if (!this.newAccount.name || !this.newAccount.token) {
        this.addAccountError = '请填写账号名称和 API Token';
        return;
      }

      this.addingAccount = true;

      try {
        // 验证账号
        const response = await fetch('/api/validate-account', {
          method: 'POST',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({
            accountName: this.newAccount.name,
            apiToken: this.newAccount.token
          })
        });

        const data = await response.json();

        if (response.ok) {
          // 检查是否已存在
          const exists = store.managedAccounts.some(acc => acc.name === this.newAccount.name);
          if (exists) {
            this.addAccountError = '该账号名称已存在';
            this.addingAccount = false;
            return;
          }

          // 添加到列表，包含余额信息
          store.managedAccounts.push({
            name: this.newAccount.name,
            token: this.newAccount.token,
            email: data.userData.email || data.userData.username,
            username: data.userData.username,
            balance: data.userData.credit ? data.userData.credit / 100 : 0,
            status: 'active'
          });

          // 保存到主机
          await this.saveManagedAccounts();

          // 刷新数据
          this.fetchData();

          // 清空表单
          this.newAccount = { name: '', token: '', balance: '' };
          this.addAccountSuccess = '✅ 账号添加成功';

          // 3秒后清除提示
          setTimeout(() => {
            this.addAccountSuccess = '';
          }, 3000);
        } else {
          this.addAccountError = data.error || '验证失败，请检查 Token 是否正确';
        }
      } catch (error) {
        this.addAccountError = '添加失败: ' + error.message;
      } finally {
        this.addingAccount = false;
      }
    },

  async batchAddAccounts() {
      this.batchAddError = '';
      this.batchAddSuccess = '';

      if (!this.batchAccounts.trim()) {
        this.batchAddError = '请输入账号信息';
        return;
      }

      const lines = this.batchAccounts.trim().split('\n');
      const accounts = [];

      // 解析每一行
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i].trim();
        if (!line) continue;

        let name = '';
        let token = '';

        // 尝试匹配括号格式：名称(token) 或 名称（token）
        const bracketMatch = line.match(/^(.+?)[（(](.+?)[）)]$/);
        if (bracketMatch) {
          name = bracketMatch[1].trim();
          token = bracketMatch[2].trim();
        } else if (line.includes(':')) {
          // 冒号格式：名称:token
          const parts = line.split(':');
          name = parts[0].trim();
          token = parts.slice(1).join(':').trim();
        } else if (line.includes('：')) {
          // 中文冒号格式：名称：token
          const parts = line.split('：');
          name = parts[0].trim();
          token = parts.slice(1).join('：').trim();
        } else {
          this.batchAddError = `第 ${i + 1} 行格式错误，支持的格式：名称:Token 或 名称：Token 或 名称(Token) 或 名称（Token）`;
          return;
        }

        if (!name || !token) {
          this.batchAddError = `第 ${i + 1} 行：账号名称或 Token 不能为空`;
          return;
        }

        accounts.push({ name, token });
      }

      if (accounts.length === 0) {
        this.batchAddError = '没有有效的账号信息';
        return;
      }

      this.addingAccount = true;
      let successCount = 0;
      let failedAccounts = [];

      // 逐个验证并添加
      for (const account of accounts) {
        try {
          const response = await fetch('/api/validate-account', {
            method: 'POST',
            headers: store.getAuthHeaders(),
            body: JSON.stringify({
              accountName: account.name,
              apiToken: account.token
            })
          });

          const data = await response.json();

          if (response.ok) {
            // 检查是否已存在
            const exists = store.managedAccounts.some(acc => acc.name === account.name);
            if (!exists) {
              store.managedAccounts.push({
                name: account.name,
                token: account.token,
                email: data.userData.email || data.userData.username,
                username: data.userData.username,
                balance: data.userData.credit ? data.userData.credit / 100 : 0,
                status: 'active'
              });
              successCount++;
            } else {
              failedAccounts.push(`${account.name}（已存在）`);
            }
          } else {
            failedAccounts.push(`${account.name}（${data.error || '验证失败'}）`);
          }
        } catch (error) {
          failedAccounts.push(`${account.name}（网络错误）`);
        }
      }

      this.addingAccount = false;

      if (successCount > 0) {
        await this.saveManagedAccounts();
        this.fetchData();
      }

      // 显示结果
      if (successCount > 0 && failedAccounts.length === 0) {
        this.batchAddSuccess = `✅ 成功添加 ${successCount} 个账号`;
        this.batchAccounts = '';
        this.maskedBatchAccounts = '';
      } else if (successCount > 0) {
        this.batchAddSuccess = `✅ 成功添加 ${successCount} 个账号`;
        this.batchAddError = `❌ 失败: ${failedAccounts.join(', ')}`;
      } else {
        this.batchAddError = `❌ 全部失败: ${failedAccounts.join(', ')}`;
      }

      // 3秒后清除提示
      setTimeout(() => {
        this.batchAddSuccess = '';
        if (successCount > 0 && failedAccounts.length === 0) {
          this.batchAddError = '';
        }
      }, 3000);
    },

    updateBatchDisplay() {
      if (!this.batchAccounts) {
        this.maskedBatchAccounts = '';
        return;
      }
      const lines = this.batchAccounts.split('\n');
      this.maskedBatchAccounts = lines.map(line => {
        // 尝试匹配括号格式：名称(token) 或 名称（token）
        const bracketMatch = line.match(/^(.+?)[（(](.+?)[）)]$/);
        if (bracketMatch) {
          const name = bracketMatch[1];
          const bracket = line.includes('（') ? '（' : '(';
          const closeBracket = line.includes('）') ? '）' : ')';
          const maskedToken = bracketMatch[2].replace(/./g, '●');
          return name + bracket + maskedToken + closeBracket;
        }

        // 冒号格式
        let separatorIndex = -1;
        let separator = '';

        if (line.includes(':')) {
          separatorIndex = line.indexOf(':');
          separator = ':';
        } else if (line.includes('：')) {
          separatorIndex = line.indexOf('：');
          separator = '：';
        }

        if (separatorIndex === -1) return line;

        const name = line.substring(0, separatorIndex);
        const token = line.substring(separatorIndex + 1);
        return name + separator + token.replace(/./g, '●');
      }).join('\n');
    },

    getProjectDomains(project) {
      const domains = [];
      if (project.services) {
        project.services.forEach(service => {
          if (service.domains && service.domains.length > 0) {
            service.domains.forEach(d => {
              if (d.domain) {
                domains.push({
                  domain: d.domain,
                  isGenerated: d.isGenerated || false
                });
              }
            });
          }
        });
      }
      return domains;
    },

    startEditProjectName(project) {
      project.isEditing = true;
      project.editingName = project.name;
      this.$nextTick(() => {
        try {
          // 使用 ref 或者通过项目 ID 查找输入框
          const inputs = this.$refs.projectNameInput;
          if (inputs) {
            const input = Array.isArray(inputs) ? inputs.find(el => el) : inputs;
            if (input) {
              input.focus();
              input.select();
            }
          } else {
            // 备用方案:通过 ID 查找
            const el = document.getElementById('proj-' + project._id);
            if (el) {
              const input = el.querySelector('.project-name-input');
              if (input) {
                input.focus();
                input.select();
              }
            }
          }
        } catch (e) { console.error(e); }
      });
    },

    cancelEditProjectName(project) {
      project.isEditing = false;
      project.editingName = '';
    },

  async saveProjectName(account, project) {
      // 如果不在编辑状态，直接返回（避免 blur 事件重复触发）
      if (!project.isEditing) {
        return;
      }

      if (!project.editingName || project.editingName.trim() === '') {
        await store.showAlert('项目名称不能为空', '错误', 'fa-exclamation-circle');
        return;
      }

      if (project.editingName === project.name) {
        this.cancelEditProjectName(project);
        return;
      }

      try {
        const accountData = store.managedAccounts.find(acc => acc.name === account.name);
        if (!accountData || !accountData.token) {
          await store.showAlert('无法获取账号 token，请重新添加账号', '错误', 'fa-exclamation-circle');
          return;
        }

        const response = await fetch('/api/project/rename', {
          method: 'POST',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({
            token: accountData.token,
            projectId: project._id,
            newName: project.editingName.trim()
          })
        });

        const result = await response.json();
        if (result.success) {
          project.name = project.editingName.trim();
          this.cancelEditProjectName(project);
          await store.showAlert('项目名称已更新', '成功', 'fa-check-circle');
        } else {
          await store.showAlert('更新失败: ' + (result.error || '未知错误'), '错误', 'fa-exclamation-circle');
        }
      } catch (error) {
        await store.showAlert('操作失败: ' + error.message, '错误', 'fa-exclamation-circle');
      }
    },

  async deleteProject(account, project) {
      const confirmed = await store.showConfirm({
        title: '确认删除项目',
        message: `确定要删除项目 "${project.name}" 吗？此操作不可恢复！`,
        icon: 'fa-exclamation-triangle',
        confirmText: '删除',
        confirmClass: 'btn-danger'
      });

      if (!confirmed) return;

      try {
        const accountData = store.managedAccounts.find(acc => acc.name === account.name);
        if (!accountData || !accountData.token) {
          await store.showAlert('无法获取账号 token，请重新添加账号', '错误', 'fa-exclamation-circle');
          return;
        }

        const response = await fetch('/api/project/delete', {
          method: 'POST',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({
            token: accountData.token,
            projectId: project._id
          })
        });

        const result = await response.json();
        if (result.success) {
          toast.success('项目已删除');
          await this.fetchData();
        } else {
          await store.showAlert('删除失败: ' + (result.error || '未知错误'), '错误', 'fa-exclamation-circle');
        }
      } catch (error) {
        await store.showAlert('操作失败: ' + error.message, '错误', 'fa-exclamation-circle');
      }
    },

  async deleteService(account, project, service) {
      const confirmed = await this.showConfirm({
        title: '确认删除服务',
        message: `确定要删除服务 "${service.name}" 吗？此操作不可恢复！`,
        icon: 'fa-exclamation-triangle',
        confirmText: '删除',
        confirmClass: 'btn-danger'
      });

      if (!confirmed) return;

      try {
        const accountData = this.managedAccounts.find(acc => acc.name === account.name);
        if (!accountData || !accountData.token) {
          await this.showAlert('无法获取账号 token，请重新添加账号', '错误', 'fa-exclamation-circle');
          return;
        }

        const response = await fetch('/api/service/delete', {
          method: 'POST',
          headers: this.getAuthHeaders(),
          body: JSON.stringify({
            token: accountData.token,
            serviceId: service._id
          })
        });

        const result = await response.json();
        if (result.success) {
          this.showGlobalToast('服务已删除', 'success');
          await this.fetchData();
        } else {
          console.error('删除服务失败:', result);
          await this.showAlert('删除失败: ' + (result.error || '未知错误'), '错误', 'fa-exclamation-circle');
        }
      } catch (error) {
        console.error('删除服务异常:', error);
        await this.showAlert('操作失败: ' + error.message, '错误', 'fa-exclamation-circle');
      }
    },

    startEditServiceName(service) {
      service.isEditing = true;
      service.editingName = service.name;
      this.$nextTick(() => {
        const input = document.querySelector(`#service-${service._id} .service-name-input`);
        if (input) {
          input.focus();
          input.select();
        }
      });
    },

    cancelEditServiceName(service) {
      service.isEditing = false;
      service.editingName = service.name;
    },

  async saveServiceName(account, project, service) {
      if (!service.isEditing) {
        return;
      }

      if (!service.editingName || service.editingName.trim() === '') {
        await this.showAlert('服务名称不能为空', '错误', 'fa-exclamation-circle');
        return;
      }

      if (service.editingName === service.name) {
        this.cancelEditServiceName(service);
        return;
      }

      try {
        const accountData = this.managedAccounts.find(acc => acc.name === account.name);
        if (!accountData || !accountData.token) {
          await this.showAlert('无法获取账号 token，请重新添加账号', '错误', 'fa-exclamation-circle');
          return;
        }

        const response = await fetch('/api/service/rename', {
          method: 'POST',
          headers: this.getAuthHeaders(),
          body: JSON.stringify({
            token: accountData.token,
            serviceId: service._id,
            newName: service.editingName.trim()
          })
        });

        const result = await response.json();
        if (result.success) {
          service.name = service.editingName.trim();
          this.cancelEditServiceName(service);
          await this.showAlert('服务名称已更新', '成功', 'fa-check-circle');
        } else {
          await this.showAlert('更新失败: ' + (result.error || '未知错误'), '错误', 'fa-exclamation-circle');
        }
      } catch (error) {
        await this.showAlert('操作失败: ' + error.message, '错误', 'fa-exclamation-circle');
      }
    },

    setupAutoScroll() {
      if (this.logsScrollTimer) {
        clearInterval(this.logsScrollTimer);
      }

      if (this.logsAutoScroll && this.showLogsModal) {
        this.logsScrollTimer = setInterval(() => {
          this.scrollToBottom();
          this.updateHorizontalScrollbar();
        }, 1000);
      }
    },

    scrollToBottom() {
      if (this.$refs.logsText) {
        this.$refs.logsText.scrollTop = this.$refs.logsText.scrollHeight;
      }
    },

    updateHorizontalScrollbar() {
      this.$nextTick(() => {
        const logsText = this.$refs.logsText;
        const scrollbar = this.$refs.logsScrollbar;
        const thumb = this.$refs.logsScrollbarThumb;

        if (!logsText || !scrollbar || !thumb) return;

        const contentWidth = logsText.scrollWidth;
        const viewportWidth = logsText.clientWidth;

        if (contentWidth <= viewportWidth) {
          scrollbar.style.display = 'none';
          return;
        }

        scrollbar.style.display = 'block';

        const maxScroll = contentWidth - viewportWidth;
        const currentScroll = logsText.scrollLeft;
        const scrollRatio = maxScroll > 0 ? currentScroll / maxScroll : 0;

        const thumbWidth = Math.max((viewportWidth / contentWidth) * 100, 5);
        const maxThumbLeft = 100 - thumbWidth;
        const thumbLeft = scrollRatio * maxThumbLeft;

        thumb.style.width = thumbWidth + '%';
        thumb.style.left = thumbLeft + '%';
      });
    },

    setupHorizontalScrollbar() {
      this.$nextTick(() => {
        const logsTextContainer = this.$refs.logsTextContainer;
        const logsText = this.$refs.logsText;
        const scrollbar = this.$refs.logsScrollbar;
        const thumb = this.$refs.logsScrollbarThumb;

        if (!logsText || !scrollbar || !thumb || !logsTextContainer) return;

        let isDragging = false;
        let dragStartX = 0;
        let dragStartThumbLeft = 0;

        // 更新滚动条状态
        const updateScrollbar = () => {
          const contentWidth = logsText.scrollWidth;
          const viewportWidth = logsText.clientWidth;

          if (contentWidth <= viewportWidth) {
            scrollbar.style.display = 'none';
            return;
          }

          scrollbar.style.display = 'block';

          const maxScroll = contentWidth - viewportWidth;
          const currentScroll = logsText.scrollLeft;
          const scrollRatio = maxScroll > 0 ? currentScroll / maxScroll : 0;

          const thumbWidth = Math.max((viewportWidth / contentWidth) * 100, 5);
          const maxThumbLeft = 100 - thumbWidth;
          const thumbLeft = scrollRatio * maxThumbLeft;

          thumb.style.width = thumbWidth + '%';
          thumb.style.left = thumbLeft + '%';
        };

        // 滚动条位置转换为内容滚动位置
        const thumbPositionToScroll = (thumbLeftPercent) => {
          const contentWidth = logsText.scrollWidth;
          const viewportWidth = logsText.clientWidth;
          const maxScroll = contentWidth - viewportWidth;
          const thumbWidth = parseFloat(thumb.style.width) || 0;
          const maxThumbLeft = 100 - thumbWidth;

          if (maxThumbLeft <= 0) return 0;

          const scrollRatio = thumbLeftPercent / maxThumbLeft;
          return scrollRatio * maxScroll;
        };

        // 监听内容滚动
        logsText.addEventListener('scroll', updateScrollbar);

        // 滑块拖拽开始
        thumb.addEventListener('mousedown', (e) => {
          isDragging = true;
          dragStartX = e.clientX;
          dragStartThumbLeft = parseFloat(thumb.style.left) || 0;
          thumb.style.cursor = 'grabbing';
          e.preventDefault();
          e.stopPropagation();
        });

        // 拖拽中
        document.addEventListener('mousemove', (e) => {
          if (!isDragging) return;

          const scrollbarRect = scrollbar.getBoundingClientRect();
          const scrollbarWidth = scrollbarRect.width;
          const thumbWidth = thumb.clientWidth;
          const maxThumbLeftPx = scrollbarWidth - thumbWidth;

          if (maxThumbLeftPx <= 0) return;

          // 计算鼠标移动距离对应的滑块移动距离
          const deltaX = e.clientX - dragStartX;
          const deltaThumbPercent = (deltaX / scrollbarWidth) * 100;
          const newThumbLeft = Math.max(0, Math.min(dragStartThumbLeft + deltaThumbPercent, 100 - (thumbWidth / scrollbarWidth * 100)));

          // 设置滑块位置并滚动内容
          thumb.style.left = newThumbLeft + '%';
          const targetScroll = thumbPositionToScroll(newThumbLeft);
          logsText.scrollLeft = targetScroll;
        });

        // 拖拽结束
        document.addEventListener('mouseup', () => {
          if (isDragging) {
            isDragging = false;
            thumb.style.cursor = 'grab';
          }
        });

        // 点击滚动条区域跳转
        scrollbar.addEventListener('click', (e) => {
          if (e.target === thumb) return;

          const scrollbarRect = scrollbar.getBoundingClientRect();
          const clickX = e.clientX - scrollbarRect.left;
          const scrollbarWidth = scrollbarRect.width;
          const thumbWidth = thumb.clientWidth;

          // 计算目标滑块位置（让滑块中心对齐到点击位置）
          const targetThumbLeftPx = Math.max(0, Math.min(clickX - thumbWidth / 2, scrollbarWidth - thumbWidth));
          const targetThumbLeftPercent = (targetThumbLeftPx / scrollbarWidth) * 100;

          // 设置滑块位置并滚动内容
          thumb.style.left = targetThumbLeftPercent + '%';
          const targetScroll = thumbPositionToScroll(targetThumbLeftPercent);
          logsText.scrollLeft = targetScroll;
        });

        // 初始化滚动条
        // 监听窗口大小变化
        const resizeObserver = new ResizeObserver(() => {
          updateScrollbar();
        });
        resizeObserver.observe(logsText);

        // 初始化
        updateScrollbar();
      });
    },

    toggleRealTimeRefresh() {
      this.logsRealTime = !this.logsRealTime;
      if (this.logsRealTime) {
        this.startRealTimeRefresh();
      } else {
        this.stopRealTimeRefresh();
      }
    },

    toggleDataRefresh() {
      this.dataRefreshPaused = !this.dataRefreshPaused;
      if (this.dataRefreshPaused) {
        // 暂停自动刷新
        this.stopAutoRefresh();
      } else {
        // 恢复自动刷新
        this.startAutoRefresh();
      }
    },

    startRealTimeRefresh() {
      if (this.logsRealTimeTimer) {
        clearInterval(this.logsRealTimeTimer);
      }

      if (this.logsRealTime && this.showLogsModal && this.logsCurrentAccount && this.logsCurrentProject && this.logsCurrentService) {
        this.logsRealTimeTimer = setInterval(async () => {
          if (document.visibilityState !== 'visible') return;
          await this.refreshLogs();
        }, 5000); // 每5秒刷新一次
      }
    },

    stopRealTimeRefresh() {
      if (this.logsRealTimeTimer) {
        clearInterval(this.logsRealTimeTimer);
        this.logsRealTimeTimer = null;
      }
    },

  async refreshLogs() {
      if (!this.logsCurrentAccount || !this.logsCurrentProject || !this.logsCurrentService) return;

      try {
        const environmentId = this.logsCurrentProject.environments && this.logsCurrentProject.environments[0] ? this.logsCurrentProject.environments[0]._id : null;
        if (!environmentId) return;

        const accountData = this.managedAccounts.find(acc => acc.name === this.logsCurrentAccount.name);
        if (!accountData || !accountData.token) return;

        const response = await fetch('/api/service/logs', {
          method: 'POST',
          headers: this.getAuthHeaders(),
          body: JSON.stringify({
            token: accountData.token,
            serviceId: this.logsCurrentService._id,
            environmentId: environmentId,
            projectId: this.logsCurrentProject._id,
            limit: 200
          })
        });

        const result = await response.json();
        if (result.success && result.logs) {
          const newLogs = result.logs.map(log => '[' + new Date(log.timestamp).toLocaleString('zh-CN') + '] ' + log.message).join('\n');

          // 如果是自动滚动状态，保持在底部
          const wasAtBottom = this.$refs.logsText && (this.$refs.logsText.scrollHeight - this.$refs.logsText.scrollTop <= this.$refs.logsText.clientHeight + 10);

          this.logsContent = newLogs;
          this.logsModalInfo.count = result.count;
          this.logsModalInfo.time = new Date().toLocaleString('zh-CN');

          this.$nextTick(() => {
            if (wasAtBottom && this.logsAutoScroll) {
              this.scrollToBottom();
            }
            this.updateHorizontalScrollbar();
          });
        }
      } catch (error) {
        console.error('刷新日志失败:', error);
      }
    },

    formatCost(cost) {
      if (cost > 0 && cost < 0.01) {
        return '0.01';
      }
      return cost.toFixed(2);
    },

    updateOpacity() {
      const opacity = this.opacity / 100;
      const root = document.documentElement;
      if (!root) return; // 防止 DOM 未加载

      // 设置所有相关的CSS变量
      root.style.setProperty('--card-opacity', opacity);
      root.style.setProperty('--service-opacity', Math.min(opacity + 0.05, 1));
      root.style.setProperty('--blur-amount', `${20 * opacity}px`);
      root.style.setProperty('--blur-amount-small', `${15 * opacity}px`);
      root.style.setProperty('--blur-amount-tiny', `${10 * opacity}px`);
      root.style.setProperty('--saturate-amount', `${100 + 80 * opacity}%`);
      root.style.setProperty('--shadow-opacity', 0.1 * opacity);
      root.style.setProperty('--shadow-opacity-light', 0.05 * opacity);
      root.style.setProperty('--border-opacity', 0.3 * opacity);
      root.style.setProperty('--border-opacity-light', 0.4 * opacity);
      root.style.setProperty('--border-opacity-strong', 0.5 * opacity);
    },

    // 切换账号展开/收起
    toggleAccount(accountName) {
      // 第一次点击时，确保状态被正确初始化
      if (!(accountName in this.expandedAccounts)) {
        this.expandedAccounts[accountName] = false;
      } else {
        this.expandedAccounts[accountName] = !this.expandedAccounts[accountName];
      }
    },

    // 检查账号是否展开
    isAccountExpanded(accountName) {
      // 如果没有设置过，默认为展开状态
      if (!(accountName in this.expandedAccounts)) {
        return true;
      }
      return this.expandedAccounts[accountName];
    },

  // 清除缓存
  async clearCache() {
      const confirmed = await this.showConfirm({
        title: '清除缓存',
        message: '确定要清除所有缓存数据吗？这将删除所有本地保存的账号、余额和费用数据。',
        icon: 'fa-exclamation-triangle',
        confirmText: '确定清除',
        confirmClass: 'btn-danger'
      });

      if (confirmed) {
        // 清除所有本地数据
        this.managedAccounts = [];
        this.projectCosts = {};
        localStorage.removeItem('zeabur_accounts');
        localStorage.removeItem('zeabur_project_costs');

        this.showGlobalToast('缓存已清除！正在重新获取数据...', 'success');
        this.fetchData();
      }
    },

  // 暂停服务
  async pauseService(account, project, service) {
      const confirmed = await this.showConfirm({
        title: '暂停服务',
        message: `确定要暂停服务"${service.name}"吗？`,
        icon: 'fa-pause-circle',
        confirmText: '确定暂停',
        confirmClass: 'btn-warning'
      });

      if (!confirmed) return;

      try {
        const environmentId = project.environments && project.environments[0] ? project.environments[0]._id : null;
        if (!environmentId) {
          this.showGlobalToast('无法获取环境 ID，请刷新页面后重试', 'error');
          return;
        }

        const accountData = this.managedAccounts.find(acc => acc.name === account.name);
        if (!accountData || !accountData.token) {
          this.showGlobalToast('无法获取账号 token，请重新添加账号', 'error');
          return;
        }

        const response = await fetch('/api/service/pause', {
          method: 'POST',
          headers: this.getAuthHeaders(),
          body: JSON.stringify({
            token: accountData.token,
            serviceId: service._id,
            environmentId: environmentId
          })
        });

        const result = await response.json();
        if (result.success) {
          this.showGlobalToast('服务已暂停', 'success');
          this.fetchData();
        } else {
          this.showGlobalToast('暂停失败: ' + (result.error || JSON.stringify(result)), 'error');
        }
      } catch (error) {
        this.showGlobalToast('操作失败: ' + error.message, 'error');
      }
    },

  // 重启服务
  async restartService(account, project, service) {
      const action = service.status === 'SUSPENDED' ? '启动' : '重启';
      const confirmed = await this.showConfirm({
        title: `${action}服务`,
        message: `确定要${action}服务"${service.name}"吗？`,
        icon: 'fa-redo',
        confirmText: `确定${action}`,
        confirmClass: 'btn-primary'
      });

      if (!confirmed) return;

      try {
        const environmentId = project.environments && project.environments[0] ? project.environments[0]._id : null;
        if (!environmentId) {
          this.showGlobalToast('无法获取环境 ID，请刷新页面后重试', 'error');
          return;
        }

        const accountData = this.managedAccounts.find(acc => acc.name === account.name);
        if (!accountData || !accountData.token) {
          this.showGlobalToast('无法获取账号 token，请重新添加账号', 'error');
          return;
        }

        const response = await fetch('/api/service/restart', {
          method: 'POST',
          headers: this.getAuthHeaders(),
          body: JSON.stringify({
            token: accountData.token,
            serviceId: service._id,
            environmentId: environmentId
          })
        });

        const result = await response.json();
        if (result.success) {
          this.showGlobalToast(`服务已${action}`, 'success');
          this.fetchData();
        } else {
          this.showGlobalToast(`${action}失败: ` + (result.error || JSON.stringify(result)), 'error');
        }
      } catch (error) {
        this.showGlobalToast('操作失败: ' + error.message, 'error');
      }
    },

  // 查看服务日志
  async showServiceLogs(account, project, service) {
      const environmentId = project.environments && project.environments[0] ? project.environments[0]._id : null;
      const accountData = this.managedAccounts.find(acc => acc.name === account.name);

      if (!environmentId || !accountData || !accountData.token) {
        toast.error('无法获取环境 ID 或账号 Token');
        return;
      }

      this.openLogViewer({
        title: `服务日志: ${service.name}`,
        subtitle: `${project.name} / ${account.name}`,
        source: 'zeabur',
        fetcher: async () => {
          const response = await fetch('/api/service/logs', {
            method: 'POST',
            headers: this.getAuthHeaders(),
            body: JSON.stringify({
              token: accountData.token,
              serviceId: service._id,
              environmentId: environmentId,
              projectId: project._id,
              limit: 200
            })
          });
          const result = await response.json();
          if (result.success) {
            return result.logs.map(l => ({
              timestamp: new Date(l.timestamp).getTime(),
              message: l.message
            }));
          } else {
            throw new Error(result.error || '获取失败');
          }
        },
        streamer: (appendLog) => {
          // Zeabur 的实时日志需要轮询或 WebSocket，这里暂时使用简单的轮询模拟实时流
          // 实际生产环境应使用 WebSocket
          this._zeaburLogTimer = setInterval(async () => {
            if (document.visibilityState !== 'visible') return;
            // 这里应该调用获取增量日志的 API
            // 暂时略过，避免过度请求
          }, 5000);
        },
        cleaner: () => {
          if (this._zeaburLogTimer) {
            clearInterval(this._zeaburLogTimer);
            this._zeaburLogTimer = null;
          }
        }
      });
    },

    // 切换自动滚动
    toggleAutoScroll() {
      this.logsAutoScroll = !this.logsAutoScroll;
      if (this.logsAutoScroll) {
        this.scrollToBottom();
      }
    },

    // 切换全屏
    toggleFullscreen() {
      this.logsFullscreen = !this.logsFullscreen;
      this.$nextTick(() => {
        if (this.logsAutoScroll) {
          this.scrollToBottom();
        }
      });
    },

  // 导出所有账号
  async exportAllAccounts() {
      try {
        if (this.managedAccounts.length === 0) {
          this.showGlobalToast('没有可导出的账号', 'warning');
          return;
        }

        const now = new Date();
        const exportData = {
          version: '1.0',
          exportTime: now.toISOString(),
          exportTimeLocal: now.toLocaleString('zh-CN', { hour12: false }),
          accounts: this.managedAccounts
        };

        // 生成本地时间格式的文件名：YYYY-MM-DD_HH-MM-SS
        const year = now.getFullYear();
        const month = String(now.getMonth() + 1).padStart(2, '0');
        const day = String(now.getDate()).padStart(2, '0');
        const hours = String(now.getHours()).padStart(2, '0');
        const minutes = String(now.getMinutes()).padStart(2, '0');
        const seconds = String(now.getSeconds()).padStart(2, '0');
        const timestamp = `${year}-${month}-${day}_${hours}-${minutes}-${seconds}`;

        const dataStr = JSON.stringify(exportData, null, 2);
        const dataBlob = new Blob([dataStr], { type: 'application/json' });
        const url = URL.createObjectURL(dataBlob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `zeabur-accounts-${timestamp}.json`;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        URL.revokeObjectURL(url);

        this.showGlobalToast('账号导出成功', 'success');
      } catch (error) {
        this.showGlobalToast('导出失败: ' + error.message, 'error');
      }
    },

  // 导入所有账号
  async importAllAccounts() {
      const confirmed = await this.showConfirm({
        title: '确认导入',
        message: '导入账号将覆盖当前所有账号配置，是否继续？',
        icon: 'fa-exclamation-triangle',
        confirmText: '确定导入',
        confirmClass: 'btn-warning'
      });

      if (!confirmed) return;

      const input = document.createElement('input');
      input.type = 'file';
      input.accept = '.json';
      input.onchange = async (event) => {
        const file = event.target.files[0];
        if (!file) return;

        const reader = new FileReader();
        reader.onload = async (e) => {
          try {
            const importedData = JSON.parse(e.target.result);

            // 验证数据格式
            if (!importedData.version || !importedData.accounts) {
              this.showGlobalToast('无效的备份文件格式', 'error');
              return;
            }

            // 导入账号
            this.managedAccounts = importedData.accounts;
            await this.saveManagedAccounts();

            this.showGlobalToast(`成功导入 ${importedData.accounts.length} 个账号`, 'success');
            await this.fetchData();
          } catch (error) {
            this.showGlobalToast('导入失败: ' + error.message, 'error');
          }
        };
        reader.readAsText(file);
      };
      input.click();
    },

  // 删除账号
  async removeAccount(index) {
      const account = store.managedAccounts[index];
      if (!account) return;

      const confirmed = await store.showConfirm({
        title: '确认删除',
        message: `确定要删除账号 "${account.name}" 吗？此操作不可恢复。`,
        icon: 'fa-exclamation-triangle',
        confirmText: '确定删除',
        confirmClass: 'btn-danger'
      });

      if (!confirmed) return;

      try {
        // 从列表中删除
        store.managedAccounts.splice(index, 1);

        // 保存到主机
        await this.saveManagedAccounts();

        toast.success(`账号 "${account.name}" 已删除`);

        // 刷新数据
        await this.fetchData();
      } catch (error) {
        toast.error('删除失败: ' + error.message);
      }
    },

  // 生成免费域名
  async generateDomain(account, project, service) {
      const confirmed = await this.showConfirm({
        title: '生成免费域名',
        message: `确定要为服务 "${service.name}" 生成免费的 Zeabur 域名吗？`,
        icon: 'fa-globe',
        confirmText: '生成',
        confirmClass: 'btn-primary'
      });

      if (!confirmed) return;

      try {
        const accountData = this.managedAccounts.find(acc => acc.name === account.name);
        if (!accountData || !accountData.token) {
          await this.showAlert('无法获取账号 token，请重新添加账号', '错误', 'fa-exclamation-circle');
          return;
        }

        const response = await fetch('/api/domain/generate', {
          method: 'POST',
          headers: this.getAuthHeaders(),
          body: JSON.stringify({
            token: accountData.token,
            serviceId: service._id
          })
        });

        const result = await response.json();
        if (result.success) {
          this.showGlobalToast(`域名已生成: ${result.domain.domain}`, 'success');
          await this.fetchData();
        } else {
          console.error('生成域名失败:', result);
          await this.showAlert('生成失败: ' + (result.error || '未知错误'), '错误', 'fa-exclamation-circle');
        }
      } catch (error) {
        console.error('生成域名异常:', error);
        await this.showAlert('操作失败: ' + error.message, '错误', 'fa-exclamation-circle');
      }
    },

  // 添加自定义域名
  async addCustomDomain(account, project, service) {
      const domain = await this.showPrompt({
        title: '添加自定义域名',
        message: '请输入您的域名：',
        placeholder: '例如：www.example.com',
        icon: 'fa-globe'
      });

      if (!domain || !domain.trim()) return;

      try {
        const accountData = this.managedAccounts.find(acc => acc.name === account.name);
        if (!accountData || !accountData.token) {
          await this.showAlert('无法获取账号 token，请重新添加账号', '错误', 'fa-exclamation-circle');
          return;
        }

        const response = await fetch('/api/domain/add', {
          method: 'POST',
          headers: this.getAuthHeaders(),
          body: JSON.stringify({
            token: accountData.token,
            serviceId: service._id,
            domain: domain.trim()
          })
        });

        const result = await response.json();
        if (result.success) {
          const dnsInfo = result.domainInfo.dnsRecord;
          const message = `域名已添加！\n\n请在您的 DNS 提供商处添加以下记录：\n\n类型: ${dnsInfo.type}\n主机: ${dnsInfo.name}\n值: ${dnsInfo.value}\n\n状态: ${result.domainInfo.status}`;
          await this.showAlert(message, '配置 DNS', 'fa-info-circle');
          await this.fetchData();
        } else {
          console.error('添加域名失败:', result);
          await this.showAlert('添加失败: ' + (result.error || '未知错误'), '错误', 'fa-exclamation-circle');
        }
      } catch (error) {
        console.error('添加域名异常:', error);
        await this.showAlert('操作失败: ' + error.message, '错误', 'fa-exclamation-circle');
      }
    },

  // 删除域名
  async deleteDomain(account, project, service, domain) {
      const confirmed = await this.showConfirm({
        title: '确认删除域名',
        message: `确定要删除域名 "${domain}" 吗？`,
        icon: 'fa-exclamation-triangle',
        confirmText: '删除',
        confirmClass: 'btn-danger'
      });

      if (!confirmed) return;

      try {
        const accountData = this.managedAccounts.find(acc => acc.name === account.name);
        if (!accountData || !accountData.token) {
          await this.showAlert('无法获取账号 token，请重新添加账号', '错误', 'fa-exclamation-circle');
          return;
        }

        const response = await fetch('/api/domain/delete', {
          method: 'POST',
          headers: this.getAuthHeaders(),
          body: JSON.stringify({
            token: accountData.token,
            serviceId: service._id,
            domain: domain
          })
        });

        const result = await response.json();
        if (result.success) {
          this.showGlobalToast('域名已删除', 'success');
          await this.fetchData();
        } else {
          console.error('删除域名失败:', result);
          await this.showAlert('删除失败: ' + (result.error || '未知错误'), '错误', 'fa-exclamation-circle');
        }
      } catch (error) {
        console.error('删除域名异常:', error);
        await this.showAlert('操作失败: ' + error.message, '错误', 'fa-exclamation-circle');
      }
    }
  };
