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
      const response = await fetch('/api/set-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: this.setPassword })
      });

      const result = await response.json();
      if (result.success) {
        // 设置成功，自动登录
        this.loginPassword = this.setPassword;
        localStorage.setItem('admin_password', this.setPassword);
        localStorage.setItem('password_time', Date.now().toString());

        this.showSetPasswordModal = false;
        this.isAuthenticated = true;

        await this.loadManagedAccounts();
        this.loadProjectCosts();
        this.fetchData();

        // 启动自动刷新
        this.startAutoRefresh();

        // 加载透明度设置
        const savedOpacity = localStorage.getItem('card_opacity');
        if (savedOpacity) {
          this.opacity = parseInt(savedOpacity);
          this.updateOpacity();
        }
      } else {
        this.setPasswordError = result.error || '设置失败';
      }
    } catch (error) {
      this.setPasswordError = '设置失败: ' + error.message;
    }
  },

  // 验证密码
  async verifyPassword() {
    this.loginError = '';
    try {
      const response = await fetch('/api/verify-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: this.loginPassword })
      });

      const result = await response.json();
      if (result.success) {
        this.isAuthenticated = true;
        this.showLoginModal = false;

        // 保存密码和时间戳
        localStorage.setItem('admin_password', this.loginPassword);
        localStorage.setItem('password_time', Date.now().toString());

        await this.loadManagedAccounts();
        this.loadProjectCosts();
        this.fetchData();

        // 启动自动刷新
        this.startAutoRefresh();

        // 加载透明度设置
        const savedOpacity = localStorage.getItem('card_opacity');
        if (savedOpacity) {
          this.opacity = parseInt(savedOpacity);
          this.updateOpacity();
        }
      } else {
        this.loginError = '密码错误，请重试';
      }
    } catch (error) {
      this.loginError = '验证失败: ' + error.message;
    }
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
          newPassword: this.newPassword
        })
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
      'x-admin-password': this.loginPassword
    };
  }
};
