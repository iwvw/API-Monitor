/**
 * 代码片段模块
 * 负责 SSH 代码片段的 CRUD 和发送
 */

/**
 * 代码片段方法集合
 */
export const snippetsMethods = {
  async loadSnippets() {
    try {
      const response = await fetch('/api/server/snippets');
      const data = await response.json();
      if (data.success) {
        this.sshSnippets = data.data;
      }
    } catch (error) {
      console.error('加载代码片段失败:', error);
    }
  },

  async saveSnippet() {
    if (!this.snippetForm.title || !this.snippetForm.content) {
      this.snippetError = '标题和内容不能为空';
      return;
    }
    this.snippetSaving = true;
    this.snippetError = '';
    try {
      const isEdit = !!this.snippetForm.id;
      const url = isEdit ? `/api/server/snippets/${this.snippetForm.id}` : '/api/server/snippets';
      const method = isEdit ? 'PUT' : 'POST';
      const response = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(this.snippetForm),
      });
      const data = await response.json();
      if (data.success) {
        this.showGlobalToast(isEdit ? '更新成功' : '创建成功', 'success');
        this.showSnippetModal = false;
        await this.loadSnippets();
      } else {
        this.snippetError = data.error || '保存失败';
      }
    } catch (error) {
      this.snippetError = '请求失败: ' + error.message;
    } finally {
      this.snippetSaving = false;
    }
  },

  async deleteSnippet(id) {
    const confirmed = await this.showConfirm({
      title: '删除片段',
      message: '确定要删除这个代码片段吗？',
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });
    if (!confirmed) return;
    try {
      const response = await fetch(`/api/server/snippets/${id}`, { method: 'DELETE' });
      const data = await response.json();
      if (data.success) {
        this.showGlobalToast('已删除', 'success');
        await this.loadSnippets();
      }
    } catch (error) {
      this.showGlobalToast('删除失败', 'error');
    }
  },

  sendSnippet(content) {
    if (!content) return;
    const dataToSend = content.endsWith('\n') ? content.replace(/\n$/, '\r') : content + '\r';
    if (this.sshSyncEnabled && this.sshViewLayout !== 'single') {
      this.visibleSessionIds.forEach(id => {
        const session = this.getSessionById(id);
        if (session && session.ws && session.ws.readyState === WebSocket.OPEN) {
          session.ws.send(JSON.stringify({ type: 'input', data: dataToSend }));
        }
      });
      this.showGlobalToast('指令已同步广播', 'success');
    } else {
      const session = this.getSessionById(this.activeSSHSessionId);
      if (session && session.ws && session.ws.readyState === WebSocket.OPEN) {
        session.ws.send(JSON.stringify({ type: 'input', data: dataToSend }));
        this.showGlobalToast('指令已发送', 'success');
      } else {
        this.showGlobalToast('未连接 SSH 会话', 'warning');
      }
    }
  },

  openAddSnippetModal() {
    this.snippetForm = { id: null, title: '', content: '', category: 'common', description: '' };
    this.snippetError = '';
    this.showSnippetModal = true;
  },

  openEditSnippetModal(snippet) {
    this.snippetForm = { ...snippet };
    this.snippetError = '';
    this.showSnippetModal = true;
  },
};
