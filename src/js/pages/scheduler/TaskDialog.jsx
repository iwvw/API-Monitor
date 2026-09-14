import React from 'react';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Button } from '@cloudflare/kumo/components/button';
import { Textarea } from '@cloudflare/kumo';
import FormCard from '../../components/ui/FormCard.jsx';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Clock, Save, Server, Sliders, Sparkle, X } from '../../components/Icons.jsx';
import { AI_POLICY_ITEMS, TYPE_ITEMS } from './constants.js';
import { CronEditor } from './CronEditor.jsx';

export function TaskDialog({
  taskDialogOpen,
  setTaskDialogOpen,
  taskForm,
  setTaskForm,
  nodeItems,
  taskCommandLabel,
  taskCommandPlaceholder,
  aiModelOptions,
  aiChannelOptions,
  cronPreview,
  cronPreviewError,
  saveTask,
  saving,
}) {
  return (
    <Dialog.Root open={taskDialogOpen} onOpenChange={setTaskDialogOpen}>
      <Dialog className="@container scheduler-task-dialog flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden p-5 cq-sm:p-6">
        <Dialog.Title className="mb-4 shrink-0 text-base font-semibold text-kumo-strong">{taskForm.id ? '编辑任务' : '新建任务'}</Dialog.Title>
        <div className="min-h-0 flex-1 overflow-y-auto pr-1">
          <div className="grid gap-4 cq-xs:grid-cols-2 cq-xs:items-start">
            <div className="min-w-0 space-y-4">
          <FormCard icon={<Server className="h-4 w-4" />} title="基础信息" description="任务名称、描述与执行节点">
            <div className="space-y-3 py-4">
              <div className="grid gap-3 cq-sm:grid-cols-2">
                <Input size="sm" label="名称" value={taskForm.name} onChange={(event) => setTaskForm((prev) => ({ ...prev, name: event.target.value }))} />
                <Select alignItemWithTrigger size="sm" label="执行节点" className="w-full" value={taskForm.node_id} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, node_id: value }))} items={nodeItems} />
              </div>
              <Input size="sm" label="描述" value={taskForm.description} onChange={(event) => setTaskForm((prev) => ({ ...prev, description: event.target.value }))} />
              <div className="grid gap-3 cq-sm:grid-cols-2">
                <Select alignItemWithTrigger size="sm" label="任务类型" className="w-full" value={taskForm.type} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, type: value }))} items={TYPE_ITEMS} />
                <Input size="sm" label="节点标签选择器" value={taskForm.node_selector} onChange={(event) => setTaskForm((prev) => ({ ...prev, node_selector: event.target.value }))} />
              </div>
              {taskForm.type === 'shell' || taskForm.type === 'agent' ? (
                <div>
                  <div className="mb-1 text-xs font-medium text-kumo-subtle">{taskCommandLabel}</div>
                  <CodeEditor language="shell" placeholder={taskCommandPlaceholder} value={taskForm.command} onChange={(command) => setTaskForm((prev) => ({ ...prev, command }))} minHeight="8rem" />
                </div>
              ) : taskForm.type === 'ai' ? (
                <div>
                  <div className="mb-1 text-xs font-medium text-kumo-subtle">{taskCommandLabel}</div>
                  <Textarea
                    rows={6}
                    placeholder={taskCommandPlaceholder}
                    value={taskForm.command}
                    onChange={(event) => setTaskForm((prev) => ({ ...prev, command: event.target.value }))}
                    className="w-full"
                  />
                </div>
              ) : (
                <Input size="sm" label={taskCommandLabel} placeholder={taskCommandPlaceholder} value={taskForm.command} onChange={(event) => setTaskForm((prev) => ({ ...prev, command: event.target.value }))} />
              )}
            </div>
          </FormCard>

          <FormCard icon={<Sliders className="h-4 w-4" />} title="执行参数" description="超时、重试与并发控制">
            <div className="grid gap-3 py-4 cq-sm:grid-cols-2">
              <Input size="sm" type="number" label="超时秒数" min="1" value={taskForm.timeout_seconds} onChange={(event) => setTaskForm((prev) => ({ ...prev, timeout_seconds: Number(event.target.value) }))} />
              <Input size="sm" type="number" label="重试次数" min="0" value={taskForm.retry_count} onChange={(event) => setTaskForm((prev) => ({ ...prev, retry_count: Number(event.target.value) }))} />
              <Input size="sm" type="number" label="重试间隔" min="0" value={taskForm.retry_interval_seconds} onChange={(event) => setTaskForm((prev) => ({ ...prev, retry_interval_seconds: Number(event.target.value) }))} />
              <Input size="sm" type="number" label="最大并发" min="1" value={taskForm.max_concurrency} onChange={(event) => setTaskForm((prev) => ({ ...prev, max_concurrency: Number(event.target.value) }))} />
            </div>
            <div className="flex items-center justify-between border-t border-kumo-line py-3">
              <span className="text-sm font-medium text-kumo-strong">启用任务</span>
              <Switch checked={taskForm.enabled === 1} onCheckedChange={(checked) => setTaskForm((prev) => ({ ...prev, enabled: checked ? 1 : 0 }))} />
            </div>
          </FormCard>
        </div>
        <div className="min-w-0 space-y-4">

          {taskForm.type === 'ai' && (
            <FormCard icon={<Sparkle className="h-4 w-4" />} title="AI 执行配置" description="可调用全部内部接口">
              <div className="grid gap-3 py-4 cq-sm:grid-cols-2">
                <Select alignItemWithTrigger size="sm" label="推理模型" className="w-full" value={taskForm.aiModel} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, aiModel: String(value) }))} items={aiModelOptions} />
                <Select alignItemWithTrigger size="sm" label="写操作策略" className="w-full" value={taskForm.aiPolicy} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, aiPolicy: String(value) }))} items={AI_POLICY_ITEMS} />
                <Select alignItemWithTrigger size="sm" label="结果推送" className="w-full cq-sm:col-span-2" value={taskForm.aiChannelId} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, aiChannelId: String(value) }))} items={[{ value: '', label: '不推送（仅记录到运行结果）' }, ...aiChannelOptions]} />
              </div>
              <div className="border-t border-kumo-line py-3 text-xs text-kumo-subtle">
                策略「完全允许」时写操作免审批执行。
              </div>
            </FormCard>
          )}

          <FormCard icon={<Clock className="h-4 w-4" />} title="调度规则" description="可视化 Cron 或自定义表达式">
            <div className="py-4">
              <CronEditor form={taskForm} setForm={setTaskForm} preview={cronPreview} previewError={cronPreviewError} />
            </div>
          </FormCard>
          </div>
          </div>
        </div>
        <div className="mt-3 flex shrink-0 items-center justify-end gap-2 border-t border-kumo-line pt-3">
          <Button size="sm" variant="secondary" onClick={() => setTaskDialogOpen(false)}><X className="h-3.5 w-3.5" />取消</Button>
          <Button size="sm" variant="primary" onClick={saveTask} disabled={saving || Boolean(cronPreviewError)}><Save className="h-3.5 w-3.5" />保存</Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
