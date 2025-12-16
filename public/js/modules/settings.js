/**
 * 设置管理模块
 * 负责系统设置、模块配置和数据导入导出
 */

export const settingsMethods = {
  // 从后端加载所有设置
  async loadUserSettings() {
    try {
      const response = await fetch('/api/settings', {
        headers: this.getAuthHeaders()
      });

      if (response.ok) {
        const result = await response.json();
        if (result.success && result.data) {
          const settings = result.data;

          // 应用自定义CSS
          if (settings.customCss) {
            this.customCss = settings.customCss;
            this.applyCustomCss();
          }

          // 应用模块设置
          if (settings.moduleVisibility) {
            this.moduleVisibility = settings.moduleVisibility;
          }
          if (settings.moduleOrder) {
            this.moduleOrder = settings.moduleOrder;
          }

          return true;
        }
      }

      // 如果后端没有设置，尝试从localStorage加载（向后兼容）
      this.loadCustomCssFromLocal();
      this.loadModuleSettingsFromLocal();
      return false;
    } catch (error) {
      console.error('加载用户设置失败:', error);
      // 降级到localStorage
      this.loadCustomCssFromLocal();
      this.loadModuleSettingsFromLocal();
      return false;
    }
  },

  // 从localStorage加载自定义CSS（向后兼容）
  loadCustomCssFromLocal() {
    const savedCss = localStorage.getItem('custom_css');
    if (savedCss) {
      this.customCss = savedCss;
      this.applyCustomCss();
    }
  },

  // 从localStorage加载模块设置（向后兼容）
  loadModuleSettingsFromLocal() {
    const savedVisibility = localStorage.getItem('module_visibility');
    const savedOrder = localStorage.getItem('module_order');

    const availableModules = ['zeabur', 'dns', 'openai'];

    if (savedVisibility) {
      const saved = JSON.parse(savedVisibility);
      availableModules.forEach(module => {
        if (!(module in saved)) {
          saved[module] = true;
        }
      });
      this.moduleVisibility = saved;
    }

    if (savedOrder) {
      const saved = JSON.parse(savedOrder);
      availableModules.forEach(module => {
        if (!saved.includes(module)) {
          saved.push(module);
        }
      });
      this.moduleOrder = saved.filter(m => availableModules.includes(m));
    }
  },

  // 保存所有设置到后端
  async saveUserSettingsToServer() {
    try {
      const settings = {
        customCss: this.customCss,
        moduleVisibility: this.moduleVisibility,
        moduleOrder: this.moduleOrder
      };

      const response = await fetch('/api/settings', {
        method: 'POST',
        headers: this.getAuthHeaders(),
        body: JSON.stringify(settings)
      });

      if (response.ok) {
        const result = await response.json();
        return result.success;
      }
      return false;
    } catch (error) {
      console.error('保存用户设置失败:', error);
      return false;
    }
  },

  // 应用自定义 CSS
  applyCustomCss() {
    const styleElement = document.getElementById('custom-css');
    if (styleElement) {
      styleElement.textContent = this.customCss;
    }
  },

  // 保存自定义 CSS
  async saveCustomCss() {
    try {
      // 先保存到localStorage（向后兼容）
      localStorage.setItem('custom_css', this.customCss);
      this.applyCustomCss();

      // 保存到后端
      const success = await this.saveUserSettingsToServer();

      if (success) {
        this.customCssSuccess = '自定义 CSS 已保存到服务器';
      } else {
        this.customCssSuccess = '自定义 CSS 已保存到本地';
      }
      this.customCssError = '';

      setTimeout(() => {
        this.customCssSuccess = '';
      }, 3000);
    } catch (error) {
      this.customCssError = '保存失败: ' + error.message;
      this.customCssSuccess = '';
    }
  },

  // 重置自定义 CSS
  async resetCustomCss() {
    this.customCss = '';
    localStorage.removeItem('custom_css');
    this.applyCustomCss();

    // 保存到后端
    await this.saveUserSettingsToServer();

    this.customCssSuccess = '自定义 CSS 已重置';
    this.customCssError = '';
    setTimeout(() => {
      this.customCssSuccess = '';
    }, 3000);
  },

  // 加载模块设置（已废弃，使用 loadUserSettings 代替）
  async loadModuleSettings() {
    // 从后端加载所有设置
    await this.loadUserSettings();

    // 定义所有可用模块
    const availableModules = ['zeabur', 'dns', 'openai'];

    // 确保所有模块都有配置
    availableModules.forEach(module => {
      if (!(module in this.moduleVisibility)) {
        this.moduleVisibility[module] = true;
      }
    });

    // 确保模块顺序包含所有模块
    availableModules.forEach(module => {
      if (!this.moduleOrder.includes(module)) {
        this.moduleOrder.push(module);
      }
    });

    // 确保至少有一个模块可见，并切换到第一个可见模块
    const hasVisibleModule = Object.values(this.moduleVisibility).some(v => v);
    if (!hasVisibleModule) {
      this.moduleVisibility[this.moduleOrder[0]] = true;
    }

    // 切换到第一个可见的模块
    const firstVisibleModule = this.moduleOrder.find(m => this.moduleVisibility[m]);
    if (firstVisibleModule) {
      this.mainActiveTab = firstVisibleModule;
    }

    // 保存更新后的设置
    await this.saveModuleSettings();
  },

  // 保存模块设置
  async saveModuleSettings() {
    // 保存到localStorage（向后兼容）
    localStorage.setItem('module_visibility', JSON.stringify(this.moduleVisibility));
    localStorage.setItem('module_order', JSON.stringify(this.moduleOrder));

    // 保存到后端
    await this.saveUserSettingsToServer();
  },

  // 切换模块可见性
  toggleModuleVisibility(module) {
    this.moduleVisibility[module] = !this.moduleVisibility[module];

    // 确保至少有一个模块可见
    const hasVisibleModule = Object.values(this.moduleVisibility).some(v => v);
    if (!hasVisibleModule) {
      this.moduleVisibility[module] = true;
      this.showGlobalToast('至少需要显示一个模块', 'warning');
      return;
    }

    // 如果隐藏的是当前模块，切换到第一个可见模块
    if (!this.moduleVisibility[module] && this.mainActiveTab === module) {
      const firstVisibleModule = this.moduleOrder.find(m => this.moduleVisibility[m]);
      if (firstVisibleModule) {
        this.mainActiveTab = firstVisibleModule;
      }
    }

    this.saveModuleSettings();
    this.showGlobalToast(`${this.getModuleName(module)} 模块已${this.moduleVisibility[module] ? '显示' : '隐藏'}`, 'success');
  },

  // 获取模块名称
  getModuleName(module) {
    const names = {
      zeabur: 'Zeabur 监控',
      dns: 'CF DNS 管理',
      openai: 'OpenAI API'
    };
    return names[module] || module;
  },

  // 获取模块图标
  getModuleIcon(module) {
    const icons = {
      zeabur: 'fa-rocket',
      dns: 'fa-cloud',
      openai: 'fa-robot'
    };
    return icons[module] || 'fa-cube';
  },

  // 拖拽开始
  handleDragStart(event, index) {
    this.draggedIndex = index;
    event.target.classList.add('dragging');
    // 防止选中文本
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/html', event.target.innerHTML);
  },

  // 拖拽结束
  handleDragEnd(event) {
    event.target.classList.remove('dragging');
    // 移除所有拖拽over样式
    document.querySelectorAll('.draggable-module-item').forEach(item => {
      item.classList.remove('drag-over');
    });
  },

  // 拖拽经过
  handleDragOver(event, index) {
    if (this.draggedIndex === null || this.draggedIndex === index) return;

    // 移除所有拖拽over样式
    document.querySelectorAll('.draggable-module-item').forEach(item => {
      item.classList.remove('drag-over');
    });

    // 添加当前项的拖拽over样式
    event.currentTarget.classList.add('drag-over');
  },

  // 拖拽放下
  handleDrop(event, dropIndex) {
    event.preventDefault();

    if (this.draggedIndex === null || this.draggedIndex === dropIndex) return;

    // 重新排列数组
    const draggedItem = this.moduleOrder[this.draggedIndex];
    const newOrder = [...this.moduleOrder];

    // 移除拖拽的项
    newOrder.splice(this.draggedIndex, 1);

    // 插入到新位置
    newOrder.splice(dropIndex, 0, draggedItem);

    this.moduleOrder = newOrder;
    this.draggedIndex = null;

    // 移除拖拽over样式
    event.currentTarget.classList.remove('drag-over');
  },

  // 保存设置
  async saveSettings() {
    await this.saveModuleSettings();
    this.showGlobalToast('设置已保存到服务器', 'success');
    this.showSettingsModal = false;
  },

  // 导出全部数据
  async exportAllData() {
    try {
      const exportData = {
        version: '1.0',
        exportTime: new Date().toISOString(),
        zeabur: {
          accounts: this.managedAccounts,
          projectCosts: this.projectCosts
        },
        dns: {
          accounts: this.dnsAccounts,
          templates: this.dnsTemplates
        },
        openai: {
          endpoints: this.openaiEndpoints
        },
        settings: {
          moduleVisibility: this.moduleVisibility,
          moduleOrder: this.moduleOrder
        }
      };

      const dataStr = JSON.stringify(exportData, null, 2);
      const dataBlob = new Blob([dataStr], { type: 'application/json' });
      const url = URL.createObjectURL(dataBlob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `api-monitor-backup-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      this.showGlobalToast('数据导出成功', 'success');
    } catch (error) {
      this.showGlobalToast('导出失败: ' + error.message, 'error');
    }
  },

  // 导入全部数据
  async importAllData() {
    const confirmed = await this.showConfirm({
      title: '确认导入',
      message: '导入数据将覆盖当前所有配置，是否继续？',
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
          if (!importedData.version) {
            this.showGlobalToast('无效的备份文件格式', 'error');
            return;
          }

          // 导入 Zeabur 数据
          if (importedData.zeabur) {
            if (importedData.zeabur.accounts) {
              this.managedAccounts = importedData.zeabur.accounts;
              await this.saveManagedAccounts();
            }
            if (importedData.zeabur.projectCosts) {
              this.projectCosts = importedData.zeabur.projectCosts;
              localStorage.setItem('zeabur_project_costs', JSON.stringify(this.projectCosts));
            }
          }

          // 导入 DNS 数据
          if (importedData.dns) {
            if (importedData.dns.accounts) {
              // 通过API导入DNS账号
              for (const account of importedData.dns.accounts) {
                await fetch('/api/cf-dns/accounts', {
                  method: 'POST',
                  headers: this.getAuthHeaders(),
                  body: JSON.stringify(account)
                });
              }
              await this.loadDnsAccounts();
            }
            if (importedData.dns.templates) {
              // 通过API导入DNS模板
              for (const template of importedData.dns.templates) {
                await fetch('/api/cf-dns/templates', {
                  method: 'POST',
                  headers: this.getAuthHeaders(),
                  body: JSON.stringify(template)
                });
              }
              await this.loadDnsTemplates();
            }
          }

          // 导入 OpenAI 数据
          if (importedData.openai && importedData.openai.endpoints) {
            await fetch('/api/openai/import', {
              method: 'POST',
              headers: this.getAuthHeaders(),
              body: JSON.stringify({ endpoints: importedData.openai.endpoints })
            });
            await this.loadOpenaiEndpoints();
          }

          // 导入设置
          if (importedData.settings) {
            if (importedData.settings.moduleVisibility) {
              this.moduleVisibility = importedData.settings.moduleVisibility;
            }
            if (importedData.settings.moduleOrder) {
              this.moduleOrder = importedData.settings.moduleOrder;
            }
            this.saveModuleSettings();
          }

          this.showGlobalToast('数据导入成功', 'success');
          await this.fetchData();
        } catch (error) {
          this.showGlobalToast('导入失败: ' + error.message, 'error');
        }
      };
      reader.readAsText(file);
    };
    input.click();
  }
};
