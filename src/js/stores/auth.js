/**
 * 认证相关状态存储
 */

import { defineStore } from 'pinia';
import { toast } from '../modules/toast.js';

export const useAuthStore = defineStore('auth', {
  state: () => ({
    isAuthenticated: false,
    isCheckingAuth: true,
    showLoginModal: false,
    showSetPasswordModal: false,
    loginError: '',
    loginPassword: '',
    setPassword: '',
    setPasswordConfirm: '',
    setPasswordError: '',
    isDemoMode: false,
  }),

  getters: {
    authHeaders: state => ({
      'Content-Type': 'application/json',
      'x-admin-password': state.loginPassword,
    }),
  },

  actions: {
    // 验证密码（登录）
    async verifyPassword() {
      this.loginError = '';
      try {
        const response = await fetch('/api/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ password: this.loginPassword }),
          credentials: 'include',
        });

        const result = await response.json();

        // 处理 429 限流错误
        if (response.status === 429) {
          const errorMsg = result.error?.message || '登录尝试过于频繁，请稍后再试';
          this.loginError = errorMsg;
          toast.warning(errorMsg, { duration: 5000 });
          return false;
        }

        if (result.success) {
          this.isAuthenticated = true;
          this.showLoginModal = false;

          // 保存密码和时间戳
          localStorage.setItem('admin_password', this.loginPassword);
          localStorage.setItem('password_time', Date.now().toString());

          toast.success('登录成功，欢迎回来！');
          return true;
        } else {
          this.loginError = result.error || '密码错误，请重试';
          toast.error(this.loginError);
          return false;
        }
      } catch (error) {
        this.loginError = '验证失败: ' + error.message;
        toast.error(this.loginError);
        return false;
      }
    },

    // 检查认证状态
    async checkAuth() {
      this.isCheckingAuth = true;
      try {
        const res = await fetch('/api/check-password');
        const { hasPassword, isDemoMode } = await res.json();
        this.isDemoMode = isDemoMode;

        if (isDemoMode) {
          const savedTime = localStorage.getItem('password_time');
          const now = Date.now();
          const isValidSession = savedTime && now - parseInt(savedTime) < 4 * 24 * 60 * 60 * 1000;

          if (!isValidSession) {
            this.loginPassword = '';
            return await this.verifyPassword();
          } else {
            this.isAuthenticated = true;
            this.showLoginModal = false;
            return true;
          }
        }

        if (!hasPassword) {
          this.showSetPasswordModal = true;
          this.isAuthenticated = false;
          return false;
        }

        const savedPassword = localStorage.getItem('admin_password');
        const savedTime = localStorage.getItem('password_time');

        if (savedPassword && savedTime) {
          const now = Date.now();
          if (now - parseInt(savedTime) < 4 * 24 * 60 * 60 * 1000) {
            this.loginPassword = savedPassword;
            await this.verifyPassword();
            if (!this.isAuthenticated) {
              this.showLoginModal = true;
            }
            return this.isAuthenticated;
          }
        }

        this.showLoginModal = true;
        return false;
      } catch (e) {
        console.error('Auth check error:', e);
        this.showLoginModal = true;
        return false;
      } finally {
        this.isCheckingAuth = false;
      }
    },

    logout() {
      this.isAuthenticated = false;
      this.loginPassword = '';
      localStorage.removeItem('admin_password');
      localStorage.removeItem('password_time');
    },
  },
});
