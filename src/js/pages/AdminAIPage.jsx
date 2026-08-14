import React from 'react';
import { AppCard } from '../components/ui/AppPrimitives.jsx';
import { Bot } from '../components/Icons.jsx';
import AdminConsole from '../components/adminai/AdminConsole.jsx';

// 管理 AI 已迁移至 Ask AI 侧栏（主目录入口已移除）；本页保留供 /adminai 直接访问。
export default function AdminAIPage() {
  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-4 cq-sm:p-6">
      <div className="flex items-center gap-2">
        <Bot className="h-5 w-5 text-kumo-brand" />
        <h1 className="text-lg font-bold text-kumo-strong">管理 AI</h1>
      </div>

      <AppCard>
        <AdminConsole />
      </AppCard>
    </div>
  );
}
