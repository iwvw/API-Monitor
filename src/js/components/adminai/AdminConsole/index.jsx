import React, { useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Tabs } from '@cloudflare/kumo';
import { Settings, Send, MessageSquare, Brain } from '../../Icons.jsx';
import { useSettingsForm } from './useSettingsForm.js';
import SettingsCard from './SettingsCard.jsx';
import ChannelsCard from './ChannelsCard.jsx';
import TemplatesCard from './TemplatesCard.jsx';
import MemoriesCard from './MemoriesCard.jsx';

/* ==================== 管理面板（主页面与 Ask AI 侧栏共用） ==================== */

export const TAB_OPTIONS = [
  {
    value: 'settings',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Settings className="h-3.5 w-3.5" />
        <span className="hidden @[420px]:inline">设置</span>
      </span>
    ),
  },
  {
    value: 'channels',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Send className="h-3.5 w-3.5" />
        <span className="hidden @[420px]:inline">配置</span>
      </span>
    ),
  },
  {
    value: 'templates',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <MessageSquare className="h-3.5 w-3.5" />
        <span className="hidden @[420px]:inline">模板</span>
      </span>
    ),
  },
  {
    value: 'memories',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Brain className="h-3.5 w-3.5" />
        <span className="hidden @[420px]:inline">记忆</span>
      </span>
    ),
  },
];

export default function AdminConsole({ hideTabs = false, activeTab: controlledTab, onTabChange }) {
  const [internalTab, setInternalTab] = useState('settings');
  const activeTab = controlledTab ?? internalTab;
  const handleTabChange = (v) => {
    if (onTabChange) onTabChange(v);
    else setInternalTab(v);
  };
  const form = useSettingsForm();

  return (
    <div className="flex min-h-full flex-col">
      {!hideTabs && (
        <div className="sticky top-0 z-10 -mx-4 border-b border-kumo-line bg-[var(--app-main-surface)] px-4 pb-3 pt-4">
          <Tabs
            value={activeTab}
            onValueChange={handleTabChange}
            tabs={TAB_OPTIONS}
          />
        </div>
      )}

      <div className={`space-y-4 pb-4 ${hideTabs ? '' : 'mt-4'}`}>
        {activeTab === 'settings' && <SettingsCard form={form} />}
        {activeTab === 'channels' && <ChannelsCard />}
        {activeTab === 'templates' && <TemplatesCard />}
        {activeTab === 'memories' && <MemoriesCard />}
      </div>

      {/* 吸底栏：mt-auto 让内容不满一屏时仍贴住底边；滚动时 sticky 保持可见 */}
      <div className="sticky bottom-0 z-10 mt-auto -mx-4 flex h-12 shrink-0 items-center justify-end gap-3 border-t border-kumo-line bg-[var(--app-main-surface)] px-4">
        {activeTab === 'settings' && (
          <Button size="sm" variant="primary" onClick={form.save} disabled={form.saving || !form.values}>
            {form.saving ? '保存中...' : '保存'}
          </Button>
        )}
      </div>
    </div>
  );
}
