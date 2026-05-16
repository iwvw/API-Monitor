/**
 * 认证模块
 * 负责用户认证和密码管理
 */

export const authMethods = {
  // 设置密码（首次）
  async setAdminPassword() {
    this.setPasswordError = '';

    if (!this.setPassword || this.setPassword.length < 6) {
      this.setPasswordError = '密码长度至少6位';
      return;
    }

    if (this.setPassword !== this.setPasswordConfirm) {
      this.setPasswordError = '两次输入的密码不一致';
      return;
    }

    try {
      // 1. 设置密码
      const setResponse = await fetch('/api/set-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: this.setPassword }),
      });

      const setResult = await setResponse.json();
      if (!setResult.success) {
        this.setPasswordError = setResult.error || '设置失败';
        return;
      }

      // 2. 设置成功后，调用登录接口创建 session
      const loginResponse = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: this.setPassword }),
        credentials: 'include', // 确保 cookie 被发送和接收
      });

      const loginResult = await loginResponse.json();
      if (loginResult.success) {
        // 登录成功 - 通过 authStore 更新状态
        this.authStore.loginPassword = this.setPassword;
        this.authStore.isAuthenticated = true;
        this.authStore.showSetPasswordModal = false;

        localStorage.setItem('admin_password', this.setPassword);
        localStorage.setItem('password_time', Date.now().toString());

        await this.loadManagedAccounts();
        this.loadProjectCosts();

        // 根据当前标签页加载对应的数据
        this.$nextTick(() => {
          switch (this.mainActiveTab) {
            case 'dns':
              this.loadDnsAccounts();
              this.loadDnsTemplates();
              break;
            case 'openai':
              this.loadOpenaiEndpoints();
              break;
          }
        });

        // 启动自动刷新
        this.startAutoRefresh();

        // 加载透明度设置
        const savedOpacity = localStorage.getItem('card_opacity');
        if (savedOpacity) {
          this.opacity = parseInt(savedOpacity);
          this.updateOpacity();
        }
      } else {
        this.setPasswordError = loginResult.error || '登录失败';
      }
    } catch (error) {
      this.setPasswordError = '设置失败: ' + error.message;
    }
  },

  // 验证密码（登录）
  async verifyPassword() {
    const success = await this.authStore.verifyPassword();
    if (success) {
      await this.loadManagedAccounts();
      this.loadProjectCosts();

      // 根据当前标签页加载对应的数据
      this.$nextTick(() => {
        switch (this.mainActiveTab) {
          case 'dns':
            this.loadDnsAccounts();
            this.loadDnsTemplates();
            break;
          case 'openai':
            this.loadOpenaiEndpoints();
            break;
          case 'server':
            if (this.serverCurrentTab === 'list') {
              this.connectMetricsStream();
            }
            break;
        }
      });

      // 启动自动刷新
      this.startAutoRefresh();

      // 加载透明度设置
      const savedOpacity = localStorage.getItem('card_opacity');
      if (savedOpacity) {
        this.opacity = parseInt(savedOpacity);
        this.updateOpacity();
      }
    }
  },

  // 取消 2FA 验证返回密码输入
  cancelLogin2FA() {
    this.authStore.cancelLogin2FA();
  },

  // 修改密码
  async changePassword() {
    this.passwordError = '';
    this.passwordSuccess = '';

    if (!this.newPassword || this.newPassword.length < 6) {
      this.passwordError = '密码长度至少6位';
      return;
    }

    if (this.newPassword !== this.confirmPassword) {
      this.passwordError = '两次输入的密码不一致';
      return;
    }

    try {
      const response = await fetch('/api/change-password', {
        method: 'POST',
        headers: this.getAuthHeaders(),
        body: JSON.stringify({
          oldPassword: this.loginPassword,
          newPassword: this.newPassword,
        }),
      });

      const result = await response.json();
      if (result.success) {
        this.passwordSuccess = '密码修改成功！';
        this.loginPassword = this.newPassword;
        localStorage.setItem('admin_password', this.newPassword);
        localStorage.setItem('password_time', Date.now().toString());

        this.newPassword = '';
        this.confirmPassword = '';

        setTimeout(() => {
          this.passwordSuccess = '';
        }, 3000);
      } else {
        this.passwordError = result.error || '修改失败';
      }
    } catch (error) {
      this.passwordError = '修改失败: ' + error.message;
    }
  },

  // 获取认证请求头
  getAuthHeaders() {
    return {
      'Content-Type': 'application/json',
      'x-admin-password': this.loginPassword,
    };
  },

  // 检查认证状态 (应用启动时调用)
  async checkAuth() {
    return await this.authStore.checkAuth();
  },
};
