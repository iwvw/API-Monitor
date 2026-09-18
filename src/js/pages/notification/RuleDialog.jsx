import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { DateTimeField } from '../../components/ui/DateTimeField.jsx';
import { AppCard } from '../../components/ui/AppPrimitives.jsx';
import { Save, CheckDouble } from '../../components/Icons.jsx';
import { parseNotificationPreviewLine } from './utils.js';

export function RuleDialog({
  showRuleModal,
  setShowRuleModal,
  ruleForm,
  setRuleForm,
  catalogModuleItems,
  catalogEventItems,
  handleSourceModuleChange,
  handlePreviewTemplate,
  templatePreview,
  notificationChannels,
  handleSaveRule,
  notificationSaving,
}) {
  return (
    <LayerDialog.Root open={showRuleModal} onOpenChange={setShowRuleModal}>
      <LayerDialog.Content size="lg">
        <LayerDialog.Title>
          {ruleForm.id ? '编辑告警规则' : '添加告警规则'}
        </LayerDialog.Title>
        <LayerDialog.Description>配置触发条件和投递渠道</LayerDialog.Description>
        <LayerDialog.Body>
        <div className="space-y-4">
          {/* Rule Name */}
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-kumo-subtle">规则名称 *</label>
            <Input size="sm"
              aria-label="规则名称"
              type="text"
              placeholder="如：数据库故障告警"
              value={ruleForm.name}
              onChange={(e) => setRuleForm(prev => ({ ...prev, name: e.target.value }))}
              className="w-full"
            />
          </div>

          {/* Source & Event Type */}
          <div className="grid grid-cols-3 gap-3">
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">来源模块</label>
              <Select alignItemWithTrigger size="sm"
                aria-label="来源监控模块"
                value={ruleForm.source_module}
                onValueChange={(value) => handleSourceModuleChange(String(value))}
                className="w-full"
                items={catalogModuleItems}
              />
            </div>

            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">事件类型</label>
              <Select alignItemWithTrigger size="sm"
                aria-label="触发事件类型"
                value={ruleForm.event_type}
                onValueChange={(value) => setRuleForm(prev => ({ ...prev, event_type: String(value) }))}
                className="w-full"
                items={catalogEventItems}
              />
            </div>

            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">告警级别</label>
              <Select alignItemWithTrigger size="sm"
                aria-label="告警紧急级别"
                value={ruleForm.severity}
                onValueChange={(value) => setRuleForm(prev => ({ ...prev, severity: String(value) }))}
                className="w-full"
                items={[
                  { value: 'info', label: '常规（Info）' },
                  { value: 'warning', label: '警告（Warning）' },
                  { value: 'critical', label: '紧急（Critical）' },
                ]}
              />
            </div>
          </div>

          {/* Target Delivery Channels Checkboxes */}
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-kumo-subtle">通知渠道 *</label>
            <AppCard padding="none" className="flex flex-wrap gap-2.5 bg-kumo-recessed/50 p-3.5">
              {notificationChannels.filter(c => c.enabled).map((channel) => (
                <Checkbox
                  key={channel.id}
                  checked={ruleForm.channels.includes(String(channel.id))}
                  onCheckedChange={(checked) => {
                    const id = String(channel.id);
                    setRuleForm(prev => ({
                      ...prev,
                      channels: checked
                        ? [...prev.channels, id]
                        : prev.channels.filter(x => x !== id)
                    }));
                  }}
                  label={channel.name}
                />
              ))}
            </AppCard>
          </div>

          {/* Repeats & Cooldown Suppression */}
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">累计触发后告警</label>
              <Input size="sm"
                aria-label="累计触发次数再告警"
                type="number"
                min="1"
                value={ruleForm.suppression.repeat_count}
                onChange={(e) => setRuleForm(prev => ({
                  ...prev,
                  suppression: { ...prev.suppression, repeat_count: parseInt(e.target.value, 10) || 1 }
                }))}
                className="w-full font-mono"
              />
            </div>

            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">静默期（分钟）</label>
              <Input size="sm"
                aria-label="冷却静默期"
                type="number"
                min="0"
                value={ruleForm.suppression.silence_minutes}
                onChange={(e) => setRuleForm(prev => ({
                  ...prev,
                  suppression: { ...prev.suppression, silence_minutes: parseInt(e.target.value, 10) || 0 }
                }))}
                className="w-full font-mono"
              />
            </div>
          </div>

          {/* Backup Notification Channels Checkboxes */}
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-kumo-subtle">失败时备用渠道</label>
            <AppCard padding="none" className="flex flex-wrap gap-2.5 bg-kumo-recessed/50 p-3.5">
              {notificationChannels.filter(c => c.enabled).map((channel) => (
                <Checkbox
                  key={`backup_${channel.id}`}
                  checked={ruleForm.backup_channels.includes(String(channel.id))}
                  onCheckedChange={(checked) => {
                    const id = String(channel.id);
                    setRuleForm(prev => ({
                      ...prev,
                      backup_channels: checked
                        ? [...prev.backup_channels, id]
                        : prev.backup_channels.filter(x => x !== id)
                    }));
                  }}
                  label={channel.name}
                />
              ))}
            </AppCard>
          </div>

          {/* Custom Template Titles */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between gap-2">
              <label className="text-xs font-semibold text-kumo-subtle">标题模板（可选，支持 {'{{变量}}'}）</label>
              <Button
                size="sm"
                variant="secondary"
                onClick={handlePreviewTemplate}
                loading={notificationSaving}
                icon={<CheckDouble className="w-3.5 h-3.5" />}
              >
                预览
              </Button>
            </div>
            <Input size="sm"
              aria-label="自定义标题模板"
              type="text"
              placeholder="如：[{{severity}}] {{serverName}} 离线"
              value={ruleForm.title_template}
              onChange={(e) => setRuleForm(prev => ({ ...prev, title_template: e.target.value }))}
              className="w-full"
            />
          </div>

          {/* Custom Template Content */}
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-kumo-subtle">内容模板（可选）</label>
            <Textarea
              aria-label="自定义内容模板"
              placeholder={'状态: 故障\n监控项: {{monitorName}}\n地址: {{url}}\n原因: {{error}}\n时间: {{time}}'}
              value={ruleForm.message_template}
              onChange={(e) => setRuleForm(prev => ({ ...prev, message_template: e.target.value }))}
              className="w-full min-h-16"
            />
          </div>

          {templatePreview && (
            <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
              <div className="h-1 bg-brand" />
              <div className="p-3.5">
                <div className="text-[9px] font-semibold uppercase text-brand">API Monitor</div>
                <div className="mt-1 text-xs font-semibold text-kumo-strong">{templatePreview.title}</div>
                <div className="mt-3 max-h-36 space-y-1 overflow-y-auto border-l-2 border-brand bg-kumo-recessed/60 px-3 py-2">
                  {(templatePreview.message || '').split('\n').map((line, index) => {
                    const item = parseNotificationPreviewLine(line);
                    if (item.empty) return <div key={`empty-${index}`} className="h-1.5" />;
                    if (!item.label) return <div key={`line-${index}`} className="text-[11px] leading-relaxed text-kumo-subtle">{item.value}</div>;
                    return (
                      <div key={`${item.label}-${index}`} className="grid grid-cols-[88px_minmax(0,1fr)] gap-2 text-[11px] leading-relaxed">
                        <span className="text-kumo-subtle">{item.label}</span>
                        <span className={`min-w-0 break-words font-semibold text-kumo-strong ${item.code ? 'font-mono text-[10px]' : ''}`}>{item.value}</span>
                      </div>
                    );
                  })}
                </div>
              </div>
              <div className="flex flex-wrap gap-1.5 px-3.5 pb-3.5">
                {(templatePreview.variables || []).map((variable) => (
                  <span key={variable} className="rounded border border-kumo-line bg-kumo-base px-1.5 py-0.5 font-mono text-[9px] text-kumo-subtle">
                    {`{{${variable}}}`}
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Quiet until */}
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-kumo-subtle">静默至（此前不发送）</label>
            <DateTimeField
              value={ruleForm.quiet_until}
              onChange={quiet_until => setRuleForm(prev => ({ ...prev, quiet_until }))}
              placeholder="未设置"
            />
          </div>

          <div className="flex items-center justify-between border-t border-kumo-line pt-4 select-none">
            <span className="text-xs font-semibold text-kumo-strong">启用规则</span>
            <Switch
              checked={!!ruleForm.enabled}
              onCheckedChange={(checked) => setRuleForm(prev => ({ ...prev, enabled: checked }))}
              size="sm"
              aria-label="启用规则"
            />
          </div>
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={handleSaveRule} loading={notificationSaving}>
            <Save className="w-3.5 h-3.5" />
            保存规则
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
