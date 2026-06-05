import React, { useState } from 'react';
import useStore from '../store.js';
import {
  Rocket,
  Key,
  CheckDouble,
  Info,
  Lock,
  AlertTriangle,
  LogIn,
  ChevronLeft,
  RefreshCw,
  Shield,
  ArrowRight
} from '../components/Icons.jsx';

function AuthPage() {
  const {
    isDemoMode,
    loginRequire2FA,
    loginError,
    loginLoading,
    verifyPassword,
    loginPassword,
    setLoginPassword,
    loginTotpToken,
    setLoginTotpToken,
    cancelLogin2FA,
    showSetPasswordModal,
  } = useStore();

  // 首次设置密码的本地状态
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [setupError, setSetupError] = useState('');
  const [setupLoading, setSetupLoading] = useState(false);

  // 2FA 码输入（满 6 位自动触发验证）
  const handle2FAInput = (e) => {
    const value = e.target.value;
    setLoginTotpToken(value);
    if (value.length === 6) {
      // 触发静默验证
      verifyPassword(true);
    }
  };

  // 首次设置密码提交
  const handleSetupPassword = async (e) => {
    e.preventDefault();
    setSetupError('');

    if (!newPassword || newPassword.length < 6) {
      setSetupError('密码长度至少 6 位');
      return;
    }
    if (newPassword !== confirmPassword) {
      setSetupError('两次输入的密码不一致');
      return;
    }

    setSetupLoading(true);
    try {
      // 1. 发起密码设置
      const res = await fetch('/api/set-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: newPassword }),
      });
      const result = await res.json();
      if (!result.success) {
        setSetupError(result.error || '设置密码失败');
        return;
      }

      // 2. 自动以新密码登录
      setLoginPassword(newPassword);
      await verifyPassword(false);
    } catch (err) {
      setSetupError('设置失败，请检查网络链接');
    } finally {
      setSetupLoading(false);
    }
  };

  // 登录提交
  const handleLogin = (e) => {
    e.preventDefault();
    verifyPassword();
  };

  // ==================== 渲染：首次设置密码 ====================
  if (showSetPasswordModal) {
    return (
      <div className="min-h-screen w-screen flex items-center justify-center bg-kumo-canvas p-4">
        <div className="w-full max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-xl p-6 lg:p-8">
          <div className="flex flex-col items-center text-center mb-6">
            <div className="w-12 h-12 rounded-full bg-kumo-brand/10 border border-kumo-brand/20 flex items-center justify-center text-kumo-brand mb-4 text-xl">
              <Rocket className="w-5 h-5" />
            </div>
            <h1 className="text-xl font-bold text-kumo-strong">开启新旅程</h1>
            <p className="text-xs text-kumo-subtle mt-1.5">请为您的管理后台设置初始密码</p>
          </div>

          <form onSubmit={handleSetupPassword} className="space-y-4">
            <div>
              <label className="block text-xs font-semibold text-kumo-subtle mb-1.5">设置密码</label>
              <div className="relative">
                <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-kumo-subtle text-xs flex items-center">
                  <Key className="w-4 h-4" />
                </span>
                <input
                  type="password"
                  className="w-full bg-kumo-recessed text-kumo-strong text-sm pl-9 pr-4 py-2 border border-kumo-line rounded-md focus:outline-none focus:border-kumo-brand"
                  placeholder="设置新密码 (6 位以上)"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  autoComplete="new-password"
                />
              </div>
            </div>

            <div>
              <label className="block text-xs font-semibold text-kumo-subtle mb-1.5">确认密码</label>
              <div className="relative">
                <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-kumo-subtle text-xs flex items-center">
                  <CheckDouble className="w-4 h-4" />
                </span>
                <input
                  type="password"
                  className="w-full bg-kumo-recessed text-kumo-strong text-sm pl-9 pr-4 py-2 border border-kumo-line rounded-md focus:outline-none focus:border-kumo-brand"
                  placeholder="再次确认密码"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  autoComplete="new-password"
                />
              </div>
            </div>

            {setupError && (
              <div className="flex items-center gap-2 text-xs text-kumo-danger bg-kumo-danger/10 border border-kumo-danger/20 rounded px-3 py-2">
                <Info className="w-4 h-4 flex-shrink-0" />
                <span>{setupError}</span>
              </div>
            )}

            <button
              type="submit"
              disabled={setupLoading}
              className="w-full h-10 bg-kumo-brand hover:bg-kumo-brand-hover text-white text-sm font-semibold rounded-md flex items-center justify-center gap-2 cursor-pointer transition-colors"
            >
              {setupLoading ? <RefreshCw className="w-4 h-4 animate-spin" /> : <span>开始使用</span>}
              {!setupLoading && <ArrowRight className="w-4 h-4" />}
            </button>
          </form>
        </div>
      </div>
    );
  }

  // ==================== 渲染：常规登录 / 2FA ====================
  return (
    <div className="min-h-screen w-screen flex items-center justify-center bg-kumo-canvas p-4">
      <div className="w-full max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-xl p-6 lg:p-8">
        
        <div className="flex flex-col items-center text-center mb-6">
          <div className="w-12 h-12 rounded-full bg-kumo-brand/10 border border-kumo-brand/20 flex items-center justify-center text-kumo-brand mb-4 text-xl flex-shrink-0">
            <Shield className="w-5 h-5" />
          </div>
          <h1 className="text-xl font-bold text-kumo-strong">
            {isDemoMode ? '演示模式' : loginRequire2FA ? '双因素验证' : '欢迎回来'}
          </h1>
          <p className="text-xs text-kumo-subtle mt-1.5">
            {isDemoMode 
              ? '账号无需密码，点击“立即进入”以继续' 
              : loginRequire2FA 
                ? '请输入 Authenticator App 中的验证码' 
                : '身份验证以访问监控面板'
            }
          </p>
        </div>

        <form onSubmit={handleLogin} className="space-y-4">
          
          {/* 密码输入 (非演示模式且没有 2FA 阶段) */}
          {!isDemoMode && !loginRequire2FA && (
            <div>
              <label className="block text-xs font-semibold text-kumo-subtle mb-1.5">管理员密码</label>
              <div className="relative">
                <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-kumo-subtle text-xs flex items-center">
                  <Lock className="w-4 h-4" />
                </span>
                <input
                  type="password"
                  className="w-full bg-kumo-recessed text-kumo-strong text-sm pl-9 pr-4 py-2 border border-kumo-line rounded-md focus:outline-none focus:border-kumo-brand"
                  placeholder="请输入管理员密码"
                  value={loginPassword}
                  onChange={(e) => setLoginPassword(e.target.value)}
                  autoComplete="current-password"
                  autoFocus
                />
              </div>
            </div>
          )}

          {/* 2FA 验证码输入 */}
          {loginRequire2FA && (
            <div>
              <label className="block text-xs font-semibold text-kumo-subtle mb-1.5">6 位双因素验证码</label>
              <div className="relative">
                <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-kumo-subtle text-xs flex items-center">
                  <Key className="w-4 h-4" />
                </span>
                <input
                  type="text"
                  maxLength={6}
                  className="w-full bg-kumo-recessed text-kumo-strong text-sm pl-9 pr-4 py-2 border border-kumo-line rounded-md focus:outline-none focus:border-kumo-brand tracking-widest text-center"
                  placeholder="000000"
                  value={loginTotpToken}
                  onChange={handle2FAInput}
                  autoComplete="one-time-code"
                  autoFocus
                />
              </div>
            </div>
          )}

          {/* 错误提示 */}
          {loginError && (
            <div className="flex items-center gap-2 text-xs text-kumo-danger bg-kumo-danger/10 border border-kumo-danger/20 rounded px-3 py-2">
              <AlertTriangle className="w-4 h-4 flex-shrink-0" />
              <span>{loginError}</span>
            </div>
          )}

          {/* 主动作按钮 */}
          {!loginRequire2FA && (
            <button
              type="submit"
              disabled={loginLoading}
              className="w-full h-10 bg-kumo-brand hover:bg-kumo-brand-hover text-white text-sm font-semibold rounded-md flex items-center justify-center gap-2 cursor-pointer transition-colors"
            >
              {loginLoading ? <RefreshCw className="w-4 h-4 animate-spin" /> : <span>立即进入</span>}
              {!loginLoading && <LogIn className="w-4 h-4" />}
            </button>
          )}

          {/* 返回密码输入按钮 (仅在 2FA 阶段显示) */}
          {loginRequire2FA && (
            <button
              type="button"
              onClick={cancelLogin2FA}
              className="w-full h-10 bg-kumo-recessed hover:bg-kumo-recessed/60 text-kumo-strong text-sm font-semibold border border-kumo-line rounded-md flex items-center justify-center gap-2 cursor-pointer transition-colors"
            >
              <ChevronLeft className="w-4 h-4" />
              <span>返回修改</span>
            </button>
          )}
        </form>
      </div>
    </div>
  );
}

export default AuthPage;
