import React, { useEffect, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Empty, Loader, Tabs } from '@cloudflare/kumo';
import { AppCard } from '../components/ui/AppPrimitives.jsx';
import { Settings, Bot } from '../components/Icons.jsx';

function SettingsCard() {
  const [settings, setSettings] = useState({
    defaultModel: '',
    writeEnabled: false,
    maxToolCalls: 10,
    timeout: 60,
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/admin-ai/settings');
        const data = await res.json();
        if (data.data) {
          setSettings((prev) => ({ ...prev, ...data.data }));
        }
      } catch {
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      await fetch('/api/admin-ai/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(settings),
      });
    } catch {
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return <div className="flex justify-center py-10"><Loader size={20} className="text-kumo-subtle" /></div>;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-semibold text-kumo-strong">默认模型</div>
          <div className="text-xs text-kumo-subtle">AI 助手使用的默认模型</div>
        </div>
        <Input
          className="w-48"
          placeholder="gpt-4o"
          value={settings.defaultModel}
          onChange={(e) => setSettings((prev) => ({ ...prev, defaultModel: e.target.value }))}
        />
      </div>
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-semibold text-kumo-strong">写操作开关</div>
          <div className="text-xs text-kumo-subtle">允许 AI 执行写操作</div>
        </div>
        <Switch
          checked={settings.writeEnabled}
          onChange={(checked) => setSettings((prev) => ({ ...prev, writeEnabled: checked }))}
        />
      </div>
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-semibold text-kumo-strong">工具调用上限</div>
          <div className="text-xs text-kumo-subtle">单次对话最多工具调用次数</div>
        </div>
        <Input
          className="w-24"
          type="number"
          min={1}
          max={100}
          value={settings.maxToolCalls}
          onChange={(e) => setSettings((prev) => ({ ...prev, maxToolCalls: Number(e.target.value) }))}
        />
      </div>
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-semibold text-kumo-strong">超时（秒）</div>
          <div className="text-xs text-kumo-subtle">AI 响应超时时间</div>
        </div>
        <Input
          className="w-24"
          type="number"
          min={10}
          max={300}
          value={settings.timeout}
          onChange={(e) => setSettings((prev) => ({ ...prev, timeout: Number(e.target.value) }))}
        />
      </div>
      <div className="flex justify-end">
        <Button size="sm" variant="primary" onClick={handleSave} disabled={saving}>
          {saving ? '保存中...' : '保存设置'}
        </Button>
      </div>
    </div>
  );
}

function ChannelsCard() {
  return (
    <div className="text-xs text-kumo-subtle">
      <p className="mb-2">频道配置管理（v1 仅展示入口）</p>
      <Button size="sm" variant="secondary" disabled>
        配置频道
      </Button>
    </div>
  );
}

function AuditCard() {
  const [records, setRecords] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/admin-ai/audit?limit=20');
        const data = await res.json();
        setRecords(data.records || data.data || []);
      } catch {
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  if (loading) {
    return <div className="flex justify-center py-10"><Loader size={20} className="text-kumo-subtle" /></div>;
  }

  if (records.length === 0) {
    return <Empty title="暂无执行记录" />;
  }

  return (
    <div className="space-y-2">
      {records.map((record, idx) => (
        <div key={record.id || idx} className="rounded border border-kumo-line bg-kumo-control p-3 text-xs">
          <div className="flex items-center justify-between">
            <span className="font-semibold text-kumo-strong">{record.action || record.type}</span>
            <span className="text-kumo-subtle">{record.created_at ? new Date(record.created_at).toLocaleString('zh-CN') : ''}</span>
          </div>
          {record.summary && <div className="mt-1 text-kumo-subtle">{record.summary}</div>}
          {record.status && (
            <span className={`mt-1 inline-block text-[10px] ${record.status === 'success' ? 'text-kumo-success' : 'text-kumo-danger'}`}>
              {record.status === 'success' ? '成功' : '失败'}
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

const TAB_OPTIONS = [
  {
    value: 'settings',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Settings className="h-3.5 w-3.5" />
        系统设置
      </span>
    ),
  },
  {
    value: 'channels',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Bot className="h-3.5 w-3.5" />
        频道配置
      </span>
    ),
  },
  {
    value: 'audit',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Bot className="h-3.5 w-3.5" />
        审计查询
      </span>
    ),
  },
];

export default function AdminAIPage() {
  const [activeTab, setActiveTab] = useState('settings');

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-4 sm:p-6">
      <div className="flex items-center gap-2">
        <Bot className="h-5 w-5 text-kumo-brand" />
        <h1 className="text-lg font-bold text-kumo-strong">管理 AI</h1>
      </div>

      <Tabs
        value={activeTab}
        onValueChange={setActiveTab}
        tabs={TAB_OPTIONS}
      />

      <AppCard>
        {activeTab === 'settings' && <SettingsCard />}
        {activeTab === 'channels' && <ChannelsCard />}
        {activeTab === 'audit' && <AuditCard />}
      </AppCard>
    </div>
  );
}