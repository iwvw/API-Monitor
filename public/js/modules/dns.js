/**
 * DNS 管理模块
 * 负责 DNS 管理相关功能
 */

export const dnsMethods = {
  switchToDns() {
          this.mainActiveTab = 'dns';
          if (this.dnsAccounts.length === 0) {
            this.loadDnsAccounts();
          }
          this.loadDnsTemplates();
        },

  showDnsToast(message, type = 'success') {
          this.dnsToast = { show: true, message, type };
          setTimeout(() => {
            this.dnsToast.show = false;
          }, 3000);
        },

  async loadDnsAccounts() {
          try {
            const response = await fetch('/api/cf-dns/accounts', {
              headers: this.getAuthHeaders()
            });
            const data = await response.json();
            // API 直接返回账号数组
            if (Array.isArray(data)) {
              // 为每个账号添加 showToken 属性
              this.dnsAccounts = data.map(acc => ({
                ...acc,
                showToken: false,
                apiToken: null
              }));
              // 如果没有选中账号且有账号列表，自动选择第一个
              if (!this.dnsSelectedAccountId && this.dnsAccounts.length > 0) {
                this.selectDnsAccount(this.dnsAccounts[0]);
              }
            } else if (data.error) {
              console.error('加载 CF 账号失败:', data.error);
            }
          } catch (error) {
            console.error('加载 CF 账号失败:', error);
          }
        },

  async toggleDnsTokenVisibility(account) {
    try {
      if (account.showToken) {
        // 隐藏 token
        account.showToken = false;
        account.apiToken = null;
      } else {
        // 显示 token - 从服务器获取
        const response = await fetch(`/api/cf-dns/accounts/${account.id}/token`, {
          headers: this.getAuthHeaders()
        });
        const data = await response.json();
        if (data.success && data.apiToken) {
          account.apiToken = data.apiToken;
          account.showToken = true;
        } else {
          this.showDnsToast('获取 Token 失败', 'error');
        }
      }
    } catch (error) {
      this.showDnsToast('操作失败: ' + error.message, 'error');
    }
  },

  selectDnsAccount(acc) {
          this.dnsSelectedAccountId = acc.id;
          this.loadDnsZones();
        },

  openAddDnsAccountModal() {
          this.dnsAccountForm = { name: '', apiToken: '', email: '' };
          this.dnsAccountFormError = '';
          this.dnsAccountFormSuccess = '';
          this.showAddDnsAccountModal = true;
        },

  editDnsAccount(account) {
          this.dnsEditingAccount = account;
          this.dnsEditAccountForm = {
            name: account.name,
            apiToken: '', // 不显示原 Token
            email: account.email || ''
          };
          this.dnsEditAccountFormError = '';
          this.dnsEditAccountFormSuccess = '';
          this.showEditDnsAccountModal = true;
        },

  async deleteDnsAccount(account) {
          const confirmed = await this.showConfirm({
            title: '确认删除',
            message: `确定要删除账号 "${account.name}" 吗？`,
            icon: 'fa-trash',
            confirmText: '删除',
            confirmClass: 'btn-danger'
          });

          if (!confirmed) return;

          try {
            const response = await fetch(`/api/cf-dns/accounts/${account.id}`, {
              method: 'DELETE',
              headers: this.getAuthHeaders()
            });

            const data = await response.json();
            // API 返回 { success: true } 或 { error: '...' }
            if (response.ok && data.success) {
              this.showDnsToast('账号已删除', 'success');
              await this.loadDnsAccounts();
              if (this.dnsSelectedAccountId === account.id) {
                this.dnsSelectedAccountId = '';
                this.dnsZones = [];
                this.dnsRecords = [];
              }
            } else {
              this.showDnsToast('删除失败: ' + (data.error || '未知错误'), 'error');
            }
          } catch (error) {
            this.showDnsToast('删除失败: ' + error.message, 'error');
          }
        },

  async loadDnsZones() {
          if (!this.dnsSelectedAccountId) {
            this.dnsZones = [];
            return;
          }

          this.dnsLoadingZones = true;
          this.dnsZones = [];
          this.dnsRecords = [];
          this.dnsSelectedZoneId = '';
          this.dnsSelectedZoneName = '';

          try {
            const response = await fetch(`/api/cf-dns/accounts/${this.dnsSelectedAccountId}/zones`, {
              headers: this.getAuthHeaders()
            });

            const data = await response.json();
            // API 返回 { zones: [...], pagination: {...} } 或 { error: '...' }
            if (data.zones) {
              this.dnsZones = data.zones;
            } else if (data.error) {
              this.showDnsToast('加载域名失败: ' + data.error, 'error');
            }
          } catch (error) {
            this.showDnsToast('加载域名失败: ' + error.message, 'error');
          } finally {
            this.dnsLoadingZones = false;
          }
        },

  selectDnsZone(zone) {
          this.dnsSelectedZoneId = zone.id;
          this.dnsSelectedZoneName = zone.name;
          this.loadDnsRecords();
        },

  async loadDnsRecords() {
          if (!this.dnsSelectedAccountId || !this.dnsSelectedZoneId) return;

          this.dnsLoadingRecords = true;
          this.dnsRecords = [];

          try {
            const response = await fetch(
              `/api/cf-dns/accounts/${this.dnsSelectedAccountId}/zones/${this.dnsSelectedZoneId}/records`,
              { headers: this.getAuthHeaders() }
            );

            const data = await response.json();
            // API 返回 { records: [...], pagination: {...} } 或 { error: '...' }
            if (data.records) {
              this.dnsRecords = data.records;
            } else if (data.error) {
              this.showDnsToast('加载记录失败: ' + data.error, 'error');
            }
          } catch (error) {
            this.showDnsToast('加载记录失败: ' + error.message, 'error');
          } finally {
            this.dnsLoadingRecords = false;
          }
        },

  openAddDnsRecordModal() {
          this.dnsEditingRecord = null;
          this.dnsRecordForm = { type: 'A', name: '', content: '', ttl: 1, proxied: false, priority: 10 };
          this.dnsRecordFormError = '';
          this.showDnsRecordModal = true;
        },

  editDnsRecord(record) {
          this.dnsEditingRecord = record;
          this.dnsRecordForm = {
            type: record.type,
            name: this.formatDnsRecordName(record.name),
            content: record.content,
            ttl: record.ttl,
            proxied: record.proxied || false,
            priority: record.priority || 10
          };
          this.dnsRecordFormError = '';
          this.showDnsRecordModal = true;
        },

  async saveDnsRecord() {
          if (!this.dnsRecordForm.name || !this.dnsRecordForm.content) {
            this.dnsRecordFormError = '请填写名称和内容';
            return;
          }

          this.dnsSavingRecord = true;
          this.dnsRecordFormError = '';

          try {
            const url = this.dnsEditingRecord
              ? `/api/cf-dns/accounts/${this.dnsSelectedAccountId}/zones/${this.dnsSelectedZoneId}/records/${this.dnsEditingRecord.id}`
              : `/api/cf-dns/accounts/${this.dnsSelectedAccountId}/zones/${this.dnsSelectedZoneId}/records`;

            const response = await fetch(url, {
              method: this.dnsEditingRecord ? 'PUT' : 'POST',
              headers: this.getAuthHeaders(),
              body: JSON.stringify(this.dnsRecordForm)
            });

            const data = await response.json();
            // API 返回 { success: true, record: {...} } 或 { error: '...' }
            if (response.ok && (data.success || data.record)) {
              this.showDnsToast(this.dnsEditingRecord ? '记录已更新' : '记录已添加', 'success');
              this.showDnsRecordModal = false;
              await this.loadDnsRecords();
            } else {
              this.dnsRecordFormError = data.error || '保存失败';
            }
          } catch (error) {
            this.dnsRecordFormError = '保存失败: ' + error.message;
          } finally {
            this.dnsSavingRecord = false;
          }
        },

  async deleteDnsRecord(record) {
          const confirmed = await this.showConfirm({
            title: '确认删除',
            message: `确定要删除记录 "${record.name}" 吗？`,
            icon: 'fa-trash',
            confirmText: '删除',
            confirmClass: 'btn-danger'
          });

          if (!confirmed) return;

          try {
            const response = await fetch(
              `/api/cf-dns/accounts/${this.dnsSelectedAccountId}/zones/${this.dnsSelectedZoneId}/records/${record.id}`,
              {
                method: 'DELETE',
                headers: this.getAuthHeaders()
              }
            );

            const data = await response.json();
            // API 返回 { success: true } 或 { error: '...' }
            if (response.ok && data.success) {
              this.showDnsToast('记录已删除', 'success');
              await this.loadDnsRecords();
            } else {
              this.showDnsToast('删除失败: ' + (data.error || '未知错误'), 'error');
            }
          } catch (error) {
            this.showDnsToast('删除失败: ' + error.message, 'error');
          }
        },

  formatDnsRecordName(name) {
          if (!name || !this.dnsSelectedZoneName) return name;
          const suffix = '.' + this.dnsSelectedZoneName;
          if (name === this.dnsSelectedZoneName) return '@';
          if (name.endsWith(suffix)) {
            return name.slice(0, -suffix.length);
          }
          return name;
        },

  toggleSelectAllDnsRecords(event) {
          if (event.target.checked) {
            this.dnsSelectedRecords = this.dnsRecords.map(r => r.id);
          } else {
            this.dnsSelectedRecords = [];
          }
        },

  async batchDeleteDnsRecords() {
          if (this.dnsSelectedRecords.length === 0) return;

          const confirmed = await this.showConfirm({
            title: '批量删除确认',
            message: `确定要删除选中的 ${this.dnsSelectedRecords.length} 条 DNS 记录吗？此操作不可恢复！`,
            icon: 'fa-exclamation-triangle',
            confirmText: '删除',
            confirmClass: 'btn-danger'
          });

          if (!confirmed) return;

          const selectedCount = this.dnsSelectedRecords.length;
          let successCount = 0;
          let failedCount = 0;

          // 显示删除进度
          this.showDnsToast(`正在删除 ${selectedCount} 条记录...`, 'success');

          for (const recordId of this.dnsSelectedRecords) {
            try {
              const response = await fetch(
                `/api/cf-dns/accounts/${this.dnsSelectedAccountId}/zones/${this.dnsSelectedZoneId}/records/${recordId}`,
                {
                  method: 'DELETE',
                  headers: this.getAuthHeaders()
                }
              );

              const data = await response.json();
              if (response.ok && data.success) {
                successCount++;
              } else {
                failedCount++;
              }
            } catch (error) {
              failedCount++;
            }
          }

          // 清空选择
          this.dnsSelectedRecords = [];

          // 刷新记录列表
          await this.loadDnsRecords();

          // 显示结果
          if (failedCount === 0) {
            this.showDnsToast(`✅ 成功删除 ${successCount} 条记录`, 'success');
          } else {
            this.showDnsToast(`⚠️ 删除完成：成功 ${successCount} 条，失败 ${failedCount} 条`, 'error');
          }
        },

  async dnsQuickSwitch() {
          if (!this.dnsQuickSwitchName || !this.dnsQuickSwitchContent) {
            this.showDnsToast('请填写记录名称和新内容', 'error');
            return;
          }

          this.dnsSwitching = true;

          try {
            // 使用正确的 API 端点 /switch
            const response = await fetch(
              `/api/cf-dns/accounts/${this.dnsSelectedAccountId}/zones/${this.dnsSelectedZoneId}/switch`,
              {
                method: 'POST',
                headers: this.getAuthHeaders(),
                body: JSON.stringify({
                  type: this.dnsQuickSwitchType,
                  name: this.dnsQuickSwitchName,
                  newContent: this.dnsQuickSwitchContent
                })
              }
            );

            const data = await response.json();
            // API 返回 { success: true, updated: n, records: [...] } 或 { error: '...' }
            if (response.ok && data.success) {
              this.showDnsToast(`切换成功！更新了 ${data.updated || 0} 条记录`, 'success');
              this.dnsQuickSwitchName = '';
              this.dnsQuickSwitchContent = '';
              await this.loadDnsRecords();
            } else {
              this.showDnsToast('切换失败: ' + (data.error || '未知错误'), 'error');
            }
          } catch (error) {
            this.showDnsToast('切换失败: ' + error.message, 'error');
          } finally {
            this.dnsSwitching = false;
          }
        },

  async loadDnsTemplates() {
          try {
            const response = await fetch('/api/cf-dns/templates', {
              headers: this.getAuthHeaders()
            });
            const data = await response.json();
            // API 直接返回模板数组
            if (Array.isArray(data)) {
              this.dnsTemplates = data;
            } else if (data.error) {
              console.error('加载模板失败:', data.error);
            }
          } catch (error) {
            console.error('加载模板失败:', error);
          }
        },

  openAddDnsTemplateModal() {
          this.dnsEditingTemplate = null;
          this.dnsTemplateForm = { name: '', type: 'A', content: '', ttl: 1, proxied: false, description: '' };
          this.dnsTemplateFormError = '';
          this.showDnsTemplateModal = true;
        },

  editDnsTemplate(template) {
          this.dnsEditingTemplate = template;
          this.dnsTemplateForm = { ...template };
          this.dnsTemplateFormError = '';
          this.showDnsTemplateModal = true;
        },

  async saveDnsTemplate() {
          if (!this.dnsTemplateForm.name || !this.dnsTemplateForm.content) {
            this.dnsTemplateFormError = '请填写模板名称和内容';
            return;
          }

          this.dnsSavingTemplate = true;
          this.dnsTemplateFormError = '';

          try {
            const url = this.dnsEditingTemplate
              ? `/api/cf-dns/templates/${this.dnsEditingTemplate.id}`
              : '/api/cf-dns/templates';

            const response = await fetch(url, {
              method: this.dnsEditingTemplate ? 'PUT' : 'POST',
              headers: this.getAuthHeaders(),
              body: JSON.stringify(this.dnsTemplateForm)
            });

            const data = await response.json();
            // API 返回 { success: true, template: {...} } 或 { error: '...' }
            if (response.ok && (data.success || data.template)) {
              this.showDnsToast(this.dnsEditingTemplate ? '模板已更新' : '模板已添加', 'success');
              this.showDnsTemplateModal = false;
              await this.loadDnsTemplates();
            } else {
              this.dnsTemplateFormError = data.error || '保存失败';
            }
          } catch (error) {
            this.dnsTemplateFormError = '保存失败: ' + error.message;
          } finally {
            this.dnsSavingTemplate = false;
          }
        },

  async deleteDnsTemplate(template) {
          const confirmed = await this.showConfirm({
            title: '确认删除',
            message: `确定要删除模板 "${template.name}" 吗？`,
            icon: 'fa-trash',
            confirmText: '删除',
            confirmClass: 'btn-danger'
          });

          if (!confirmed) return;

          try {
            const response = await fetch(`/api/cf-dns/templates/${template.id}`, {
              method: 'DELETE',
              headers: this.getAuthHeaders()
            });

            const data = await response.json();
            // API 返回 { success: true } 或 { error: '...' }
            if (response.ok && data.success) {
              this.showDnsToast('模板已删除', 'success');
              await this.loadDnsTemplates();
            } else {
              this.showDnsToast('删除失败: ' + (data.error || '未知错误'), 'error');
            }
          } catch (error) {
            this.showDnsToast('删除失败: ' + error.message, 'error');
          }
        },

  async addDnsAccount() {
          if (!this.dnsAccountForm.name || !this.dnsAccountForm.apiToken) {
            this.dnsAccountFormError = '请填写账号名称和 API Token';
            return;
          }

          this.dnsSavingAccount = true;
          this.dnsAccountFormError = '';
          this.dnsAccountFormSuccess = '';

          try {
            const response = await fetch('/api/cf-dns/accounts', {
              method: 'POST',
              headers: this.getAuthHeaders(),
              body: JSON.stringify(this.dnsAccountForm)
            });

            const data = await response.json();
            // API 返回 { success: true, account: {...} } 或 { error: '...' }
            if (response.ok && (data.success || data.account)) {
              this.dnsAccountFormSuccess = '账号添加成功！';
              await this.loadDnsAccounts();
              setTimeout(() => {
                this.showAddDnsAccountModal = false;
              }, 1000);
            } else {
              this.dnsAccountFormError = data.error || '添加失败';
            }
          } catch (error) {
            this.dnsAccountFormError = '网络错误: ' + error.message;
          } finally {
            this.dnsSavingAccount = false;
          }
        },

  async verifyDnsAccount(account) {
          try {
            const response = await fetch(`/api/cf-dns/accounts/${account.id}/verify`, {
              method: 'POST',
              headers: this.getAuthHeaders()
            });

            const data = await response.json();
            // API 返回 { valid: true/false, ... }
            if (data.valid) {
              this.showDnsToast('Token 验证成功！', 'success');
            } else {
              this.showDnsToast('Token 验证失败: ' + (data.error || '无效的 Token'), 'error');
            }
          } catch (error) {
            this.showDnsToast('验证失败: ' + error.message, 'error');
          }
        },

  async updateDnsAccount() {
          if (!this.dnsEditAccountForm.name) {
            this.dnsEditAccountFormError = '请填写账号名称';
            return;
          }

          this.dnsSavingAccount = true;
          this.dnsEditAccountFormError = '';
          this.dnsEditAccountFormSuccess = '';

          try {
            const updateData = {
              name: this.dnsEditAccountForm.name,
              email: this.dnsEditAccountForm.email
            };

            // 如果填写了新的 Token，则包含在更新数据中
            if (this.dnsEditAccountForm.apiToken) {
              updateData.apiToken = this.dnsEditAccountForm.apiToken;
            }

            const response = await fetch(`/api/cf-dns/accounts/${this.dnsEditingAccount.id}`, {
              method: 'PUT',
              headers: this.getAuthHeaders(),
              body: JSON.stringify(updateData)
            });

            const data = await response.json();
            if (response.ok && data.success) {
              this.dnsEditAccountFormSuccess = '账号更新成功！';
              setTimeout(() => {
                this.showEditDnsAccountModal = false;
                this.loadDnsAccounts();
              }, 1000);
            } else {
              this.dnsEditAccountFormError = data.error || '更新失败';
            }
          } catch (error) {
            this.dnsEditAccountFormError = '更新失败: ' + error.message;
          } finally {
            this.dnsSavingAccount = false;
          }
        },

  formatDnsDate(dateStr) {
          if (!dateStr) return '-';
          const date = new Date(dateStr);
          return date.toLocaleDateString('zh-CN');
        },

  canDnsBeProxied(type) {
          return ['A', 'AAAA', 'CNAME'].includes(type);
        },

  startEditDnsName(record) {
          // 取消其他正在编辑的记录
          this.dnsRecords.forEach(r => {
            if (r.isEditingName) {
              r.isEditingName = false;
            }
            if (r.isEditingContent) {
              r.isEditingContent = false;
            }
          });

          record.isEditingName = true;
          record.editingName = this.formatDnsRecordName(record.name);

          // 等待 DOM 更新后聚焦输入框
          this.$nextTick(() => {
            const inputs = document.querySelectorAll('.inline-edit-input');
            if (inputs.length > 0) {
              inputs[inputs.length - 1].focus();
              inputs[inputs.length - 1].select();
            }
          });
        },

  cancelEditDnsName(record) {
          record.isEditingName = false;
          record.editingName = '';
        },

  async saveDnsName(record) {
          // 如果不在编辑状态，直接返回
          if (!record.isEditingName) {
            return;
          }

          const originalName = this.formatDnsRecordName(record.name);

          // 如果名称没有变化，取消编辑
          if (record.editingName === originalName) {
            this.cancelEditDnsName(record);
            return;
          }

          // 验证名称不为空
          if (!record.editingName || record.editingName.trim() === '') {
            this.showDnsToast('名称不能为空', 'error');
            return;
          }

          try {
            const response = await fetch(
              `/api/cf-dns/accounts/${this.dnsSelectedAccountId}/zones/${this.dnsSelectedZoneId}/records/${record.id}`,
              {
                method: 'PUT',
                headers: this.getAuthHeaders(),
                body: JSON.stringify({
                  type: record.type,
                  name: record.editingName.trim(),
                  content: record.content,
                  ttl: record.ttl,
                  proxied: record.proxied || false
                })
              }
            );

            const data = await response.json();
            if (response.ok && (data.success || data.record)) {
              // 重新加载记录以获取更新后的完整名称
              this.cancelEditDnsName(record);
              this.showDnsToast('记录名称已更新', 'success');
              await this.loadDnsRecords();
            } else {
              this.showDnsToast('保存失败: ' + (data.error || '未知错误'), 'error');
            }
          } catch (error) {
            this.showDnsToast('保存失败: ' + error.message, 'error');
          }
        },

  startEditDnsContent(record) {
          // 取消其他正在编辑的记录
          this.dnsRecords.forEach(r => {
            if (r.isEditingContent) {
              r.isEditingContent = false;
            }
          });

          record.isEditingContent = true;
          record.editingContent = record.content;

          // 等待 DOM 更新后聚焦输入框
          this.$nextTick(() => {
            const inputs = document.querySelectorAll('.inline-edit-input');
            if (inputs.length > 0) {
              inputs[inputs.length - 1].focus();
              inputs[inputs.length - 1].select();
            }
          });
        },

  cancelEditDnsContent(record) {
          record.isEditingContent = false;
          record.editingContent = '';
        },

  async saveDnsContent(record) {
          // 如果不在编辑状态，直接返回
          if (!record.isEditingContent) {
            return;
          }

          // 如果内容没有变化，取消编辑
          if (record.editingContent === record.content) {
            this.cancelEditDnsContent(record);
            return;
          }

          // 验证内容不为空
          if (!record.editingContent || record.editingContent.trim() === '') {
            this.showDnsToast('内容不能为空', 'error');
            return;
          }

          try {
            const response = await fetch(
              `/api/cf-dns/accounts/${this.dnsSelectedAccountId}/zones/${this.dnsSelectedZoneId}/records/${record.id}`,
              {
                method: 'PUT',
                headers: this.getAuthHeaders(),
                body: JSON.stringify({
                  type: record.type,
                  name: this.formatDnsRecordName(record.name),
                  content: record.editingContent.trim(),
                  ttl: record.ttl,
                  proxied: record.proxied || false
                })
              }
            );

            const data = await response.json();
            if (response.ok && (data.success || data.record)) {
              // 更新本地记录
              record.content = record.editingContent.trim();
              this.cancelEditDnsContent(record);
              this.showDnsToast('记录已更新', 'success');
            } else {
              this.showDnsToast('保存失败: ' + (data.error || '未知错误'), 'error');
            }
          } catch (error) {
            this.showDnsToast('保存失败: ' + error.message, 'error');
          }
        },

  // 导出 DNS 记录
  async exportDnsRecords() {
    try {
      if (!this.dnsSelectedZoneId) {
        this.showDnsToast('请先选择一个域名', 'error');
        return;
      }

      if (this.dnsRecords.length === 0) {
        this.showDnsToast('没有可导出的 DNS 记录', 'warning');
        return;
      }

      const exportData = {
        version: '1.0',
        exportTime: new Date().toISOString(),
        zoneName: this.dnsSelectedZoneName,
        zoneId: this.dnsSelectedZoneId,
        records: this.dnsRecords.map(record => ({
          type: record.type,
          name: this.formatDnsRecordName(record.name),
          content: record.content,
          ttl: record.ttl,
          proxied: record.proxied || false,
          priority: record.priority
        }))
      };

      const dataStr = JSON.stringify(exportData, null, 2);
      const dataBlob = new Blob([dataStr], { type: 'application/json' });
      const url = URL.createObjectURL(dataBlob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `dns-${this.dnsSelectedZoneName}-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      this.showDnsToast('DNS 记录导出成功', 'success');
    } catch (error) {
      this.showDnsToast('导出失败: ' + error.message, 'error');
    }
  },

  // 导入 DNS 记录
  async importDnsRecords() {
    if (!this.dnsSelectedZoneId) {
      this.showDnsToast('请先选择一个域名', 'error');
      return;
    }

    const confirmed = await this.showConfirm({
      title: '确认导入',
      message: '导入 DNS 记录将添加到当前域名中，是否继续？',
      icon: 'fa-exclamation-triangle',
      confirmText: '确定导入',
      confirmClass: 'btn-primary'
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
          if (!importedData.version || !importedData.records) {
            this.showDnsToast('无效的备份文件格式', 'error');
            return;
          }

          // 询问是否确认导入
          const confirm2 = await this.showConfirm({
            title: '确认导入',
            message: `将导入 ${importedData.records.length} 条 DNS 记录到域名 ${this.dnsSelectedZoneName}，确定继续吗？`,
            icon: 'fa-info-circle',
            confirmText: '开始导入',
            confirmClass: 'btn-primary'
          });

          if (!confirm2) return;

          // 导入记录
          let successCount = 0;
          let failedCount = 0;

          this.showDnsToast(`正在导入 ${importedData.records.length} 条记录...`, 'success');

          for (const record of importedData.records) {
            try {
              const response = await fetch(
                `/api/cf-dns/accounts/${this.dnsSelectedAccountId}/zones/${this.dnsSelectedZoneId}/records`,
                {
                  method: 'POST',
                  headers: this.getAuthHeaders(),
                  body: JSON.stringify(record)
                }
              );

              const data = await response.json();
              if (response.ok && (data.success || data.record)) {
                successCount++;
              } else {
                failedCount++;
              }
            } catch (error) {
              failedCount++;
            }
          }

          // 刷新记录列表
          await this.loadDnsRecords();

          // 显示结果
          if (failedCount === 0) {
            this.showDnsToast(`✅ 成功导入 ${successCount} 条记录`, 'success');
          } else {
            this.showDnsToast(`⚠️ 导入完成：成功 ${successCount} 条，失败 ${failedCount} 条`, 'error');
          }
        } catch (error) {
          this.showDnsToast('导入失败: ' + error.message, 'error');
        }
      };
      reader.readAsText(file);
    };
    input.click();
  },

  // 导出 DNS 账号
  async exportDnsAccounts() {
    try {
      if (this.dnsAccounts.length === 0) {
        this.showDnsToast('没有可导出的账号', 'warning');
        return;
      }

      // 从服务器获取包含 API Token 的完整账号数据
      const response = await fetch('/api/cf-dns/accounts/export', {
        headers: this.getAuthHeaders()
      });

      const data = await response.json();
      if (!response.ok || !data.success) {
        this.showDnsToast('导出失败: ' + (data.error || '未知错误'), 'error');
        return;
      }

      const exportData = {
        version: '1.0',
        exportTime: new Date().toISOString(),
        accounts: data.accounts
      };

      const dataStr = JSON.stringify(exportData, null, 2);
      const dataBlob = new Blob([dataStr], { type: 'application/json' });
      const url = URL.createObjectURL(dataBlob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `dns-accounts-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      this.showDnsToast('DNS 账号导出成功', 'success');
    } catch (error) {
      this.showDnsToast('导出失败: ' + error.message, 'error');
    }
  },

  // 导入 DNS 账号
  async importDnsAccounts() {
    const confirmed = await this.showConfirm({
      title: '确认导入',
      message: '导入 DNS 账号将添加到现有账号列表中，是否继续？',
      icon: 'fa-exclamation-triangle',
      confirmText: '确定导入',
      confirmClass: 'btn-primary'
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
            this.showDnsToast('无效的备份文件格式', 'error');
            return;
          }

          // 询问是否确认导入
          const confirm2 = await this.showConfirm({
            title: '确认导入',
            message: `将导入 ${importedData.accounts.length} 个 DNS 账号，确定继续吗？`,
            icon: 'fa-info-circle',
            confirmText: '开始导入',
            confirmClass: 'btn-primary'
          });

          if (!confirm2) return;

          // 导入账号
          let successCount = 0;
          let failedCount = 0;
          let skippedCount = 0;

          this.showDnsToast(`正在导入 ${importedData.accounts.length} 个账号...`, 'success');

          for (const account of importedData.accounts) {
            try {
              // 检查账号是否已存在
              const exists = this.dnsAccounts.some(acc => acc.name === account.name);
              if (exists) {
                skippedCount++;
                continue;
              }

              const response = await fetch('/api/cf-dns/accounts', {
                method: 'POST',
                headers: this.getAuthHeaders(),
                body: JSON.stringify(account)
              });

              const data = await response.json();
              if (response.ok && (data.success || data.account)) {
                successCount++;
              } else {
                failedCount++;
              }
            } catch (error) {
              failedCount++;
            }
          }

          // 刷新账号列表
          await this.loadDnsAccounts();

          // 显示结果
          let message = `✅ 成功导入 ${successCount} 个账号`;
          if (skippedCount > 0) {
            message += `，跳过 ${skippedCount} 个重复账号`;
          }
          if (failedCount > 0) {
            message = `⚠️ 导入完成：成功 ${successCount} 个，跳过 ${skippedCount} 个，失败 ${failedCount} 个`;
          }
          this.showDnsToast(message, failedCount > 0 ? 'error' : 'success');
        } catch (error) {
          this.showDnsToast('导入失败: ' + error.message, 'error');
        }
      };
      reader.readAsText(file);
    };
    input.click();
  }
};
