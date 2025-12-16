/**
 * 设置管理模块
 * 负责系统设置、模块配置和数据导入导出
 */

export const settingsMethods = {
  // 加载模块设置
  loadModuleSettings() {
    const savedVisibility = localStorage.getItem('module_visibility');
    const savedOrder = localStorage.getItem('module_order');

    // 定义所有可用模块
    const availableModules = ['zeabur', 'dns', 'openai'];

    if (savedVisibility) {
      const saved = JSON.parse(savedVisibility);
      // 合并已保存的设置和新模块
      availableModules.forEach(module => {
        if (!(module in saved)) {
          // 新模块默认显示
          saved[module] = true;
        }
      });
      this.moduleVisibility = saved;
    } else {
      // 首次加载，所有模块默认显示
      availableModules.forEach(module => {
        this.moduleVisibility[module] = true;
      });
    }

    if (savedOrder) {
      const saved = JSON.parse(savedOrder);
      // 添加新模块到顺序列表末尾
      availableModules.forEach(module => {
        if (!saved.includes(module)) {
          saved.push(module);
        }
      });
      // 移除不存在的模块
      this.moduleOrder = saved.filter(m => availableModules.includes(m));
    } else {
      // 首次加载，使用默认顺序
      this.moduleOrder = [...availableModules];
    }

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
    this.saveModuleSettings();
  },

  // 保存模块设置
  saveModuleSettings() {
    localStorage.setItem('module_visibility', JSON.stringify(this.moduleVisibility));
    localStorage.setItem('module_order', JSON.stringify(this.moduleOrder));
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
  saveSettings() {
    this.saveModuleSettings();
    this.showGlobalToast('设置已保存', 'success');
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
