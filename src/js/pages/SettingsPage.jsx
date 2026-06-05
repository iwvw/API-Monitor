import React, { useState } from 'react';
import { toast } from '../modules/toast.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Tabs } from '@cloudflare/kumo/components/tabs';
import useStore from '../store.js';
import { Settings, Lock, Shield, LayoutDashboard, Globe, Sun } from '../components/Icons.jsx';

function SettingsPage() {
  const { themeMode, theme, setThemeMode } = useStore();
  const [activeTab, setActiveTab] = useState('general');
  const [passwordForm, setPasswordForm] = useState({ old: '', new: '', confirm: '' });

  const handlePasswordChange = async () => {
    if (!passwordForm.old || !passwordForm.new || !passwordForm.confirm) {
      return toast.warning('请填写所有密码字段');
    }
    if (passwordForm.new !== passwordForm.confirm) {
      return toast.error('两次输入的新密码不一致');
    }
    try {
      const response = await fetch('/api/auth/password', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'x-admin-password': localStorage.getItem('admin_password') || ''
        },
        body: JSON.stringify({ oldPassword: passwordForm.old, newPassword: passwordForm.new })
      });
      const result = await response.json();
      if (result.success) {
        toast.success('密码修改成功，请重新登录');
        setTimeout(() => {
          useStore.getState().logout();
        }, 1500);
      } else {
        toast.error(result.error || '修改密码失败');
      }
    } catch (e) {
      toast.error('请求失败: ' + e.message);
    }
  };

  return (
    <div className="flex flex-col gap-6 w-full max-w-4xl mx-auto pb-20">
      <div className="flex items-center gap-3 border-b border-kumo-line pb-4">
        <div className="w-10 h-10 rounded-full bg-kumo-recessed border border-kumo-line flex items-center justify-center text-kumo-brand">
          <Settings className="w-5 h-5" />
        </div>
        <div>
          <h1 className="text-lg font-bold text-kumo-strong">系统设置</h1>
          <p className="text-xs text-kumo-subtle">管理 API Monitor 的全局配置和安全选项</p>
        </div>
      </div>

      <div className="flex flex-col md:flex-row gap-6">
        {/* Sidebar Tabs */}
        <div className="w-full md:w-64 shrink-0">
          <Tabs
            variant="segmented"
            size="sm"
            value={activeTab}
            onValueChange={setActiveTab}
            className="w-full"
            listClassName="w-full"
            tabs={[
              { value: 'general', label: <span className="inline-flex items-center gap-2"><LayoutDashboard className="w-4 h-4" />常规设置</span> },
              { value: 'security', label: <span className="inline-flex items-center gap-2"><Shield className="w-4 h-4" />安全与认证</span> },
              { value: 'appearance', label: <span className="inline-flex items-center gap-2"><Sun className="w-4 h-4" />外观主题</span> },
              { value: 'network', label: <span className="inline-flex items-center gap-2"><Globe className="w-4 h-4" />网络与代理</span> },
            ]}
          />
        </div>

        {/* Content Area */}
        <div className="flex-1 bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-6">
          {activeTab === 'general' && (
            <div className="space-y-6">
              <h2 className="text-base font-bold text-kumo-strong border-b border-kumo-line pb-2">常规设置</h2>
              
              <div className="p-10 text-center border border-dashed border-kumo-line rounded-lg text-kumo-subtle text-xs">
                全局常规配置功能开发中...
              </div>
            </div>
          )}

          {activeTab === 'appearance' && (
            <div className="space-y-6">
              <h2 className="text-base font-bold text-kumo-strong border-b border-kumo-line pb-2">外观主题</h2>

              <div className="flex flex-col gap-4">
                <div className="flex flex-col gap-1">
                  <h3 className="text-sm font-semibold text-kumo-strong">颜色模式</h3>
                  <p className="text-xs text-kumo-subtle">
                    当前生效为{theme === 'dark' ? '深色' : '浅色'}主题，可手动锁定或跟随系统。
                  </p>
                </div>

                <div className="max-w-xs">
                  <Select
                    label="主题模式"
                    size="sm"
                    value={themeMode}
                    onValueChange={setThemeMode}
                    items={[
                      { value: 'auto', label: '跟随系统' },
                      { value: 'light', label: '浅色主题' },
                      { value: 'dark', label: '深色主题' },
                    ]}
                  />
                </div>
              </div>
            </div>
          )}

          {activeTab === 'security' && (
            <div className="space-y-6">
              <h2 className="text-base font-bold text-kumo-strong border-b border-kumo-line pb-2">安全与认证</h2>
              
              <div className="space-y-4">
                <h3 className="text-sm font-semibold text-kumo-strong flex items-center gap-2">
                  <Lock className="w-4 h-4" />
                  修改管理员密码
                </h3>
                
                <div className="grid gap-3 max-w-md">
                  <Input
                    label="当前密码"
                    type="password"
                    size="sm"
                    value={passwordForm.old}
                    onChange={(e) => setPasswordForm({ ...passwordForm, old: e.target.value })}
                    className="w-full"
                  />
                  <Input
                    label="新密码"
                    type="password"
                    size="sm"
                    value={passwordForm.new}
                    onChange={(e) => setPasswordForm({ ...passwordForm, new: e.target.value })}
                    className="w-full"
                  />
                  <Input
                    label="确认新密码"
                    type="password"
                    size="sm"
                    value={passwordForm.confirm}
                    onChange={(e) => setPasswordForm({ ...passwordForm, confirm: e.target.value })}
                    className="w-full"
                  />
                  <div className="pt-2">
                    <Button variant="primary" size="sm" onClick={handlePasswordChange}>
                      更新密码
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'network' && (
            <div className="space-y-6">
              <h2 className="text-base font-bold text-kumo-strong border-b border-kumo-line pb-2">网络与代理</h2>
              <div className="p-10 text-center border border-dashed border-kumo-line rounded-lg text-kumo-subtle text-xs">
                全局代理配置功能开发中...
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export default SettingsPage;
