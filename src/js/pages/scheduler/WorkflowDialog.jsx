import React from 'react';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerCard } from '@cloudflare/kumo';
import FormCard from '../../components/ui/FormCard.jsx';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { sectionCardHeaderClass } from '../../components/ui/AppPrimitives.jsx';
import { ArrowRight, GitBranch, Layers, Plus, Save, Sliders, Trash } from '../../components/Icons.jsx';
import { CONDITION_ITEMS, TYPE_ITEMS } from './constants.js';
import { IconButton } from './shared.jsx';
import { WorkflowCanvas } from './WorkflowCanvas.jsx';

export function WorkflowDialog({
  workflowDialogOpen,
  setWorkflowDialogOpen,
  workflowForm,
  setWorkflowForm,
  tasks,
  taskItems,
  workflowNodeItems,
  selectedWorkflowNode,
  setSelectedWorkflowNodeId,
  workflowCanvasEpoch,
  getWorkflowNodeAiChannelId,
  updateWorkflowNodeAiConfig,
  updateWorkflowNode,
  updateWorkflowNodeTask,
  deleteWorkflowNode,
  addWorkflowNode,
  addWorkflowEdge,
  saveWorkflow,
  saving,
}) {
  return (
    <Dialog.Root open={workflowDialogOpen} onOpenChange={setWorkflowDialogOpen}>
      <Dialog className="@container scheduler-workflow-dialog flex h-[min(720px,calc(100dvh-2rem))] flex-col overflow-hidden p-5 cq-sm:p-6">
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          <div className="grid min-h-0 flex-1 gap-3 overflow-y-auto pr-1 cq-md:grid-cols-[minmax(0,1fr)_26rem] cq-md:overflow-hidden cq-md:pr-0">
            <div className="flex min-h-0 flex-col gap-3 overflow-y-auto pr-1">
              <FormCard icon={<GitBranch className="h-4 w-4" />} title="工作流信息" description="名称、触发规则与启用状态">
                <div className="grid gap-3 py-3 cq-lg:grid-cols-[minmax(0,1fr)_8rem]">
                  <div className="grid min-w-0 gap-3 cq-sm:grid-cols-2">
                    <Input size="sm" label="名称" value={workflowForm.name} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, name: event.target.value }))} />
                    <Input size="sm" label="Cron（留空为手动）" value={workflowForm.schedule} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, schedule: event.target.value }))} />
                  </div>
                  <div className="flex h-8 items-center justify-end gap-2 self-end">
                    <span className="text-sm font-medium text-kumo-strong">启用</span>
                    <Switch checked={workflowForm.enabled === 1} onCheckedChange={(checked) => setWorkflowForm((prev) => ({ ...prev, enabled: checked ? 1 : 0 }))} />
                  </div>
                </div>
                <div className="border-t border-kumo-line pb-3 pt-3">
                  <Input size="sm" label="描述" value={workflowForm.description} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, description: event.target.value }))} />
                </div>
              </FormCard>
              <LayerCard className="scheduler-workflow-canvas-editor-card flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0">
                <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0 shrink-0`}>
                  <div className="flex min-w-0 items-center gap-2.5 text-sm font-semibold text-kumo-strong">
                    <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-brand">
                      <GitBranch className="h-4 w-4" />
                    </span>
                    <span className="truncate">流程画布</span>
                    <span className="shrink-0 rounded bg-kumo-recessed px-1.5 py-0.5 text-[10px] font-normal text-kumo-subtle">{workflowForm.nodes.length} 节点 / {workflowForm.edges.length} 依赖</span>
                  </div>
                  <Button size="sm" variant="secondary" onClick={addWorkflowNode}><Plus className="h-3.5 w-3.5" />新增节点</Button>
                </LayerCard.Secondary>
                <LayerCard.Primary className="min-h-0 flex-1 gap-0 overflow-visible bg-kumo-elevated p-0 pl-0 pr-0 pt-0 pb-0 ring-0">
                  <div className="min-h-0 flex-1 p-3">
                    <WorkflowCanvas key={`workflow-editor-${workflowCanvasEpoch}`} workflow={workflowForm} runs={[]} tasks={tasks} selectedNodeId={selectedWorkflowNode?.id} onSelectNode={setSelectedWorkflowNodeId} size="editor" />
                  </div>
                </LayerCard.Primary>
              </LayerCard>
            </div>

            <div className="flex min-h-0 flex-col gap-3">
              <div className="min-h-0 flex-1 space-y-3 overflow-y-auto pb-2 pr-1">
                <LayerCard className="flex flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0">
                  <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0`}>
                    <div className="flex min-w-0 items-center gap-2.5 text-sm font-semibold text-kumo-strong">
                      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-brand">
                        <Layers className="h-4 w-4" />
                      </span>
                      <span className="truncate">节点设置</span>
                    </div>
                    {selectedWorkflowNode && selectedWorkflowNode.type !== 'start' && (
                      <IconButton label="删除节点" variant="secondary-destructive" onClick={() => deleteWorkflowNode(selectedWorkflowNode.id)} icon={<Trash className="h-3.5 w-3.5" />} />
                    )}
                  </LayerCard.Secondary>
                  <LayerCard.Primary className="gap-0 overflow-visible bg-kumo-elevated px-4 pb-4 pt-3 ring-0">
                  {selectedWorkflowNode ? (
                    <div className="space-y-3">
                      <div className="grid gap-3 cq-sm:grid-cols-2">
                        <Input size="sm" label="节点名称" value={selectedWorkflowNode.name || ''} onChange={(event) => updateWorkflowNode(selectedWorkflowNode.id, { name: event.target.value })} />
                        {selectedWorkflowNode.type !== 'start' && (
                          <div className="flex items-end justify-end gap-2 pb-0.5">
                            <span className="text-sm font-medium text-kumo-strong">启用节点</span>
                            <Switch checked={selectedWorkflowNode.enabled !== 0} onCheckedChange={(checked) => updateWorkflowNode(selectedWorkflowNode.id, { enabled: checked ? 1 : 0 })} />
                          </div>
                        )}
                      </div>
                      {selectedWorkflowNode.type === 'start' ? (
                        <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-2 text-xs text-kumo-subtle">开始节点只负责触发流程，不需要绑定任务。</div>
                      ) : (
                        <div className="space-y-3">
                          <Select alignItemWithTrigger
                            size="sm"
                            label="引用任务"
                            className="w-full"
                            value={String(selectedWorkflowNode.task_id || 0)}
                            onValueChange={(value) => updateWorkflowNodeTask(selectedWorkflowNode, value)}
                            items={taskItems}
                          />
                          {!selectedWorkflowNode.task_id && (
                            <div className="space-y-3">
                              <div className="grid gap-3 cq-sm:grid-cols-2">
                                <Select alignItemWithTrigger
                                  size="sm"
                                  label="节点类型"
                                  className="w-full"
                                  value={selectedWorkflowNode.type || 'shell'}
                                  onValueChange={(value) => updateWorkflowNode(selectedWorkflowNode.id, { type: value })}
                                  items={TYPE_ITEMS}
                                />
                                <div className="flex items-end justify-end gap-2 pb-0.5">
                                  <span className="text-sm font-medium text-kumo-strong">启用节点</span>
                                  <Switch checked={selectedWorkflowNode.enabled !== 0} onCheckedChange={(checked) => updateWorkflowNode(selectedWorkflowNode.id, { enabled: checked ? 1 : 0 })} />
                                </div>
                              </div>
                              <CodeEditor
                                label={selectedWorkflowNode.type === 'ai' ? 'AI 提示词' : '内联命令'}
                                language="shell"
                                value={selectedWorkflowNode.command || ''}
                                onChange={(command) => updateWorkflowNode(selectedWorkflowNode.id, { command })}
                                placeholder={selectedWorkflowNode.type === 'ai' ? undefined : 'echo workflow-inline-step'}
                                minHeight="8rem"
                              />
                              {selectedWorkflowNode.type === 'ai' && (
                                <Input
                                  size="sm"
                                  label="推送通知渠道 ID"
                                  value={getWorkflowNodeAiChannelId(selectedWorkflowNode) || ''}
                                  onChange={(event) => updateWorkflowNodeAiConfig({ channelId: event.target.value })}
                                  placeholder="可选：notif_ 开头的通知中心渠道 ID，完成后推送 AI 输出"
                                />
                              )}
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  ) : (
                    <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-4 text-center text-xs text-kumo-subtle">从画布中选择一个节点。</div>
                  )}
                  </LayerCard.Primary>
                </LayerCard>

                <LayerCard className="flex flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0">
                  <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0`}>
                    <div className="flex items-center gap-2.5 text-sm font-semibold text-kumo-strong">
                      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-brand">
                        <Sliders className="h-4 w-4" />
                      </span>
                      依赖规则
                    </div>
                    <Button size="sm" variant="secondary" onClick={addWorkflowEdge}><Plus className="h-3.5 w-3.5" />新增</Button>
                  </LayerCard.Secondary>
                  <LayerCard.Primary className="gap-0 overflow-visible bg-kumo-elevated px-4 pb-4 pt-3 ring-0">
                  <div className="space-y-2">
                    {workflowForm.edges.length === 0 && (
                      <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-4 text-center text-xs text-kumo-subtle">暂无依赖规则，节点会独立存在。点击「新增」为节点连线。</div>
                    )}
                    {workflowForm.edges.map((edge, index) => (
                      <div key={edge.id} className="grid gap-2 rounded-md border border-kumo-line p-2.5">
                        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-end gap-2">
                          <Select alignItemWithTrigger size="sm" label="来源" className="w-full" value={edge.from} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, from: value } : item) }))} items={workflowNodeItems} />
                          <ArrowRight className="mb-2 h-3.5 w-3.5 shrink-0 text-kumo-subtle" />
                          <Select alignItemWithTrigger size="sm" label="目标" className="w-full" value={edge.to} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, to: value } : item) }))} items={workflowNodeItems} />
                        </div>
                        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-end gap-2">
                          <Select alignItemWithTrigger size="sm" label="触发条件" className="w-full" value={edge.condition} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, condition: value } : item) }))} items={CONDITION_ITEMS} />
                          <IconButton label="删除依赖" variant="secondary-destructive" onClick={() => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.filter((_, i) => i !== index) }))} icon={<Trash className="h-3.5 w-3.5" />} />
                        </div>
                      </div>
                    ))}
                  </div>
                  </LayerCard.Primary>
                </LayerCard>
              </div>
            </div>
          </div>

          <div className="flex shrink-0 items-center justify-end gap-2 border-t border-kumo-line pt-3"><Button size="sm" variant="secondary" onClick={() => setWorkflowDialogOpen(false)}>取消</Button><Button size="sm" variant="primary" onClick={saveWorkflow} disabled={saving}><Save className="h-3.5 w-3.5" />保存</Button></div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
