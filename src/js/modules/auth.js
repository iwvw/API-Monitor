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
        body: JSON.stringify({ password: this.setPassword })
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
        credentials: 'include' // 确保 cookie 被发送和接收
      });

      const loginResult = await loginResponse.json();
      if (loginResult.success) {
        // 登录成功
        this.loginPassword = this.setPassword;
        localStorage.setItem('admin_password', this.setPassword);
        localStorage.setItem('password_time', Date.now().toString());

        this.showSetPasswordModal = false;
        this.isAuthenticated = true;

        await this.loadManagedAccounts();
        this.loadProjectCosts();

        // 根据当前标签页加载对应的数据
        this.$nextTick(() => {
          switch (this.mainActiveTab) {
            case 'zeabur':
              this.fetchData();
              break;
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
    this.loginError = '';
    try {
      const response = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: this.loginPassword }),
        credentials: 'include' // 确保 cookie 被发送和接收
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

        // 根据当前标签页加载对应的数据
        this.$nextTick(() => {
          switch (this.mainActiveTab) {
            case 'zeabur':
              this.fetchData();
              break;
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
      } else {
        this.loginError = result.error || '密码错误，请重试';
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
  },

  // 检查认证状态 (应用启动时调用)
  async checkAuth() {
    this.isCheckingAuth = true;
    try {
      // 1. 检查是否已设置密码
      const res = await fetch('/api/check-password');
      const { hasPassword } = await res.json();

      if (!hasPassword) {
        this.showSetPasswordModal = true;
        this.isAuthenticated = false;
        return false;
      }

      // 2. 检查本地凭据
      const savedPassword = localStorage.getItem('admin_password');
      const savedTime = localStorage.getItem('password_time');

      if (savedPassword && savedTime) {
        const now = Date.now();
        // 4天有效期
        if (now - parseInt(savedTime) < 4 * 24 * 60 * 60 * 1000) {
          this.loginPassword = savedPassword;
          // 复用登录逻辑
          await this.verifyPassword();

          // 如果登录失败(密码变更等)，verifyPassword 会设置错误并保持 isAuthenticated=false
          if (!this.isAuthenticated) {
            this.showLoginModal = true;
          }
          return this.isAuthenticated;
        }
      }

      // 未登录或凭据过期
      this.showLoginModal = true;
      return false;

    } catch (e) {
      console.error('Auth check error:', e);
      this.showLoginModal = true;
      return false;
    } finally {
      this.isCheckingAuth = false;
    }
  }
};
