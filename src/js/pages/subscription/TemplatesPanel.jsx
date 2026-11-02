import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Label } from '@cloudflare/kumo/components/label';
import { Select } from '@cloudflare/kumo/components/select';
import { Badge, ClipboardText, LayerCard } from '@cloudflare/kumo';
import { SectionCard, sectionCardHeaderClass } from '../../components/ui/AppPrimitives.jsx';
import { Copy, Edit, Plus, Save, Trash } from '../../components/Icons.jsx';
import { subscriptionURL } from './utils.js';
import { MasonryGrid } from './components.jsx';

export default function TemplatesPanel({
  templates, publicBase, saving, selectedTemplateSubscription, templateSubscriptionId, setTemplateSubscriptionId,
  subscriptionItems, templateBindingId, setTemplateBindingId, templateItems, isArmed,
  onOpenCreateTemplate, onSaveTemplateBinding, onSetDefaultTemplate, onOpenCloneTemplate, onOpenEditTemplate, onDeleteTemplate,
}) {
  return (
    <MasonryGrid>
      <SectionCard
        title="模板转换"
        actions={(
          <div className="flex flex-wrap justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={onOpenCreateTemplate}><Plus className="h-3.5 w-3.5" />新建模板</Button>
            <Button size="sm" variant="primary" onClick={onSaveTemplateBinding} loading={saving} disabled={!selectedTemplateSubscription}><Save className="h-3.5 w-3.5" />保存转换</Button>
          </div>
        )}
      >
        <div className="grid grid-cols-1 gap-4 cq-sm:grid-cols-2">
          <Select alignItemWithTrigger size="sm" label="对外订阅" value={templateSubscriptionId} onValueChange={(value) => setTemplateSubscriptionId(String(value))} items={subscriptionItems} className="w-full" />
          <Select alignItemWithTrigger size="sm" label="输出模板" value={templateBindingId} onValueChange={(value) => setTemplateBindingId(String(value))} items={templateItems} disabled={!selectedTemplateSubscription} className="w-full" />
        </div>
        {selectedTemplateSubscription && (
          <div className="mt-4 grid gap-2 border-t border-kumo-line pt-4 cq-sm:grid-cols-2">
            <div className="flex min-w-0 flex-col gap-1 cq-sm:flex-row cq-sm:items-center cq-sm:gap-2">
              <Label className="text-xs font-semibold text-kumo-subtle cq-sm:w-20 cq-sm:shrink-0">自适应</Label>
              <ClipboardText size="sm" text={subscriptionURL(publicBase, selectedTemplateSubscription)} className="min-w-0 flex-1" tooltip={{ text: '复制自适应订阅链接（按客户端自动识别）', copiedText: '自适应订阅链接已复制' }} labels={{ copyAction: '复制自适应订阅' }} />
            </div>
            <div className="flex min-w-0 flex-col gap-1 cq-sm:flex-row cq-sm:items-center cq-sm:gap-2">
              <Label className="text-xs font-semibold text-kumo-subtle cq-sm:w-20 cq-sm:shrink-0">Clash (YAML)</Label>
              <ClipboardText size="sm" text={subscriptionURL(publicBase, selectedTemplateSubscription, 'clash')} className="min-w-0 flex-1" tooltip={{ text: '复制 Mihomo / Clash 链接', copiedText: 'Mihomo / Clash 链接已复制' }} labels={{ copyAction: '复制 Clash（YAML）' }} />
            </div>
            <div className="flex min-w-0 flex-col gap-1 cq-sm:flex-row cq-sm:items-center cq-sm:gap-2">
              <Label className="text-xs font-semibold text-kumo-subtle cq-sm:w-20 cq-sm:shrink-0">Base64</Label>
              <ClipboardText size="sm" text={subscriptionURL(publicBase, selectedTemplateSubscription, 'base64')} className="min-w-0 flex-1" tooltip={{ text: '复制 Base64 链接（sing-box 官方 / v2rayN）', copiedText: 'Base64 链接已复制' }} labels={{ copyAction: '复制 Base64' }} />
            </div>
            <div className="flex min-w-0 flex-col gap-1 cq-sm:flex-row cq-sm:items-center cq-sm:gap-2">
              <Label className="text-xs font-semibold text-kumo-subtle cq-sm:w-20 cq-sm:shrink-0">Raw</Label>
              <ClipboardText size="sm" text={subscriptionURL(publicBase, selectedTemplateSubscription, 'raw')} className="min-w-0 flex-1" tooltip={{ text: '复制 Raw 链接', copiedText: 'Raw 链接已复制' }} labels={{ copyAction: '复制 Raw' }} />
            </div>
            <div className="flex min-w-0 flex-col gap-1 cq-sm:flex-row cq-sm:items-center cq-sm:gap-2">
              <Label className="text-xs font-semibold text-kumo-subtle cq-sm:w-20 cq-sm:shrink-0">信息页</Label>
              <ClipboardText size="sm" text={subscriptionURL(publicBase, selectedTemplateSubscription, 'info')} className="min-w-0 flex-1" tooltip={{ text: '复制订阅信息页链接（浏览器打开）', copiedText: '订阅信息页链接已复制' }} labels={{ copyAction: '复制信息页' }} />
            </div>
          </div>
        )}
      </SectionCard>

      {templates.map((tpl) => (
          <LayerCard key={tpl.id} className="overflow-hidden">
            <LayerCard.Secondary className={sectionCardHeaderClass}>
              <div className="min-w-0">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-sm font-semibold text-kumo-strong">{tpl.name}</span>
                  {tpl.is_default && <Badge variant="success">默认</Badge>}
					{tpl.builtin && <Badge variant="neutral">内置</Badge>}
					{tpl.valid === false && <Badge variant="error">配置错误</Badge>}
                </div>
              </div>
              <div className="flex shrink-0 gap-1">
                <Button size="sm" variant="secondary" onClick={() => onSetDefaultTemplate(tpl)} disabled={tpl.valid === false}>默认</Button>
                <Button size="sm" shape="square" variant="secondary" onClick={() => onOpenCloneTemplate(tpl)} aria-label="复制模板" title="复制模板" icon={<Copy className="h-3.5 w-3.5" />} />
                <Button size="sm" shape="square" variant="secondary" onClick={() => onOpenEditTemplate(tpl)} aria-label="编辑模板" title="编辑模板" icon={<Edit className="h-3.5 w-3.5" />} />
                <Button size="sm" variant={isArmed(`template-delete:${tpl.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onDeleteTemplate(tpl)} disabled={tpl.builtin}><Trash className="h-3.5 w-3.5" /></Button>
              </div>
            </LayerCard.Secondary>
            <LayerCard.Primary>
              <div className={`mb-3 text-xs ${tpl.valid === false ? 'text-kumo-danger' : 'text-kumo-subtle'}`}>{tpl.validation_error || tpl.description || tpl.format}</div>
              <div className="max-h-44 overflow-auto">
                <pre className="m-0 w-auto rounded-none border-none bg-transparent p-0 font-mono text-sm leading-[20px] text-kumo-subtle">{tpl.content}</pre>
              </div>
            </LayerCard.Primary>
          </LayerCard>
      ))}
    </MasonryGrid>
  );
}
