/**
 * {{MODULE_NAME}} 模块 - 前端逻辑
 * 
 * 使用说明：
 * 1. 将 {{moduleName}} 替换为驼峰命名（如 myFeature）
 * 2. 将 {{MODULE_NAME}} 替换为模块显示名称
 * 3. 将 {{API_PREFIX}} 替换为 API 前缀（如 /api/my-feature）
 * 4. 将此文件保存到 public/js/modules/{{module}}.js
 */

import { store } from '../store.js';
import { toast } from './toast.js';

export const {{ moduleName }}Methods = {

    // ==================== 初始化 ====================

    /**
     * 切换到此模块时调用
     */
    switchTo{ { ModuleName } } () {
        store.mainActiveTab = '{{moduleName}}';
        // 首次加载数据
        if (store.{ { moduleName } } Items.length === 0) {
            this.load{ { ModuleName } } Items();
        }
    },

        /**
         * 显示 Toast 通知
         */
        show{ { ModuleName } } Toast(message, type = 'success') {
    toast[type](message);
},

  // ==================== 数据加载 ====================

  /**
   * 加载项目列表
   */
  async load{ { ModuleName } } Items() {
    store.{ { moduleName } } Loading = true;
    try {
        const response = await fetch('{{API_PREFIX}}/items', {
            headers: store.getAuthHeaders()
        });
        const data = await response.json();

        if (Array.isArray(data)) {
            store.{ { moduleName } } Items = data;
        } else if (data.error) {
            console.error('加载失败:', data.error);
            this.show{ { ModuleName } } Toast('加载失败: ' + data.error, 'error');
        }
    } catch (error) {
        console.error('加载失败:', error);
        this.show{ { ModuleName } } Toast('加载失败: ' + error.message, 'error');
    } finally {
        store.{ { moduleName } } Loading = false;
    }
},

  /**
   * 刷新数据
   */
  async refresh{ { ModuleName } } () {
    await this.load{ { ModuleName } } Items();
    this.show{ { ModuleName } } Toast('数据已刷新', 'success');
},

    // ==================== 添加项目 ====================

    /**
     * 打开添加弹窗
     */
    openAdd{ { ModuleName } } Modal() {
    this.{ { moduleName } } EditingItem = null;
    this.{ { moduleName } } Form = {
        name: '',
        // ... 其他表单字段
    };
    this.{ { moduleName } } FormError = '';
    this.show{ { ModuleName } } Modal = true;
},

  /**
   * 保存新项目
   */
  async add{ { ModuleName } } Item() {
    // 表单验证
    if (!this.{ { moduleName } } Form.name) {
        this.{ { moduleName } } FormError = '请填写名称';
        return;
    }

    this.{ { moduleName } } Saving = true;
    this.{ { moduleName } } FormError = '';

    try {
        const response = await fetch('{{API_PREFIX}}/items', {
            method: 'POST',
            headers: store.getAuthHeaders(),
            body: JSON.stringify(this.{{ moduleName }}Form)
    });

    const data = await response.json();

    if (response.ok && (data.success || data.item)) {
        this.show{ { ModuleName } } Toast('添加成功', 'success');
        this.show{ { ModuleName } } Modal = false;
        await this.load{ { ModuleName } } Items();
    } else {
        this.{ { moduleName } } FormError = data.error || '添加失败';
    }
} catch (error) {
    this.{ { moduleName } } FormError = '网络错误: ' + error.message;
} finally {
    this.{ { moduleName } } Saving = false;
}
  },

  // ==================== 编辑项目 ====================

  /**
   * 打开编辑弹窗
   */
  edit{ { ModuleName } } Item(item) {
    this.{ { moduleName } } EditingItem = item;
    this.{ { moduleName } } Form = {
        name: item.name,
        // ... 复制其他字段
    };
    this.{ { moduleName } } FormError = '';
    this.show{ { ModuleName } } Modal = true;
},

  /**
   * 保存编辑
   */
  async update{ { ModuleName } } Item() {
    if (!this.{ { moduleName } } Form.name) {
        this.{ { moduleName } } FormError = '请填写名称';
        return;
    }

    this.{ { moduleName } } Saving = true;
    this.{ { moduleName } } FormError = '';

    try {
        const response = await fetch(`{{API_PREFIX}}/items/${this.{{ moduleName }}EditingItem.id
} `, {
        method: 'PUT',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(this.{{moduleName}}Form)
      });

      const data = await response.json();
      
      if (response.ok && data.success) {
        this.show{{ModuleName}}Toast('更新成功', 'success');
        this.show{{ModuleName}}Modal = false;
        await this.load{{ModuleName}}Items();
      } else {
        this.{{moduleName}}FormError = data.error || '更新失败';
      }
    } catch (error) {
      this.{{moduleName}}FormError = '更新失败: ' + error.message;
    } finally {
      this.{{moduleName}}Saving = false;
    }
  },

  /**
   * 保存项目（通用方法，根据是否有 editingItem 决定新建或更新）
   */
  async save{{ModuleName}}Item() {
    if (this.{{moduleName}}EditingItem) {
      await this.update{{ModuleName}}Item();
    } else {
      await this.add{{ModuleName}}Item();
    }
  },

  // ==================== 删除项目 ====================

  /**
   * 删除单个项目
   */
  async delete{{ModuleName}}Item(item) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除 "${item.name}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger'
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`{ { API_PREFIX } } /items/${ item.id } `, {
        method: 'DELETE',
        headers: store.getAuthHeaders()
      });

      const data = await response.json();
      
      if (response.ok && data.success) {
        this.show{{ModuleName}}Toast('已删除', 'success');
        await this.load{{ModuleName}}Items();
      } else {
        this.show{{ModuleName}}Toast('删除失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.show{{ModuleName}}Toast('删除失败: ' + error.message, 'error');
    }
  },

  /**
   * 批量删除
   */
  async batchDelete{{ModuleName}}Items() {
    if (store.{{moduleName}}SelectedItems.length === 0) {
      this.show{{ModuleName}}Toast('请先选择要删除的项目', 'warning');
      return;
    }

    const confirmed = await store.showConfirm({
      title: '批量删除',
      message: `确定要删除选中的 ${ store.{ { moduleName } } SelectedItems.length } 个项目吗？`,
      icon: 'fa-exclamation-triangle',
      confirmText: '删除',
      confirmClass: 'btn-danger'
    });

    if (!confirmed) return;

    try {
      const response = await fetch('{{API_PREFIX}}/items/batch-delete', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({ ids: store.{{moduleName}}SelectedItems })
      });

      const data = await response.json();
      
      if (response.ok && data.success) {
        this.show{{ModuleName}}Toast(`成功删除 ${ data.deleted } 个项目`, 'success');
        store.{{moduleName}}SelectedItems = [];
        await this.load{{ModuleName}}Items();
      } else {
        this.show{{ModuleName}}Toast('批量删除失败', 'error');
      }
    } catch (error) {
      this.show{{ModuleName}}Toast('批量删除失败: ' + error.message, 'error');
    }
  },

  // ==================== 选择功能 ====================

  /**
   * 全选/取消全选
   */
  toggleSelectAll{{ModuleName}}(event) {
    if (event.target.checked) {
      store.{{moduleName}}SelectedItems = store.{{moduleName}}Items.map(item => item.id);
    } else {
      store.{{moduleName}}SelectedItems = [];
    }
  },

  // ==================== 导入导出 ====================

  /**
   * 导出数据
   */
  async export{{ModuleName}}Data() {
    try {
      const response = await fetch('{{API_PREFIX}}/export', {
        headers: store.getAuthHeaders()
      });
      const data = await response.json();

      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `{ { moduleName } } -export -${ new Date().toISOString().slice(0, 10) }.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      this.show{{ModuleName}}Toast('导出成功', 'success');
    } catch (error) {
      this.show{{ModuleName}}Toast('导出失败: ' + error.message, 'error');
    }
  },

  /**
   * 导入数据
   */
  async import{{ModuleName}}Data(event) {
    const file = event.target.files[0];
    if (!file) return;

    try {
      const text = await file.text();
      const importData = JSON.parse(text);

      const response = await fetch('{{API_PREFIX}}/import', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({ data: importData.data || importData })
      });

      const data = await response.json();
      
      if (response.ok && data.success) {
        this.show{{ModuleName}}Toast(`成功导入 ${ data.imported } 条数据`, 'success');
        await this.load{{ModuleName}}Items();
      } else {
        this.show{{ModuleName}}Toast('导入失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.show{{ModuleName}}Toast('导入失败: ' + error.message, 'error');
    }

    // 清空文件选择
    event.target.value = '';
  },

  // ==================== 工具函数 ====================

  /**
   * 格式化日期
   */
  format{{ModuleName}}Date(dateStr) {
    if (!dateStr) return '-';
    const date = new Date(dateStr);
    return date.toLocaleDateString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    });
  }
};

// ==================== Store 初始数据 ====================
// 请在 store.js 中添加以下属性：
/*
// {{MODULE_NAME}} 模块
{{moduleName}}Items: [],
{{moduleName}}Loading: false,
{{moduleName}}SelectedItems: [],
{{moduleName}}EditingItem: null,
{{moduleName}}Form: { name: '' },
{{moduleName}}FormError: '',
{{moduleName}}Saving: false,
show{{ModuleName}}Modal: false,
*/
