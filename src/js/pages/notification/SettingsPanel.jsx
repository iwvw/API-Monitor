import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Settings, Save } from '../../components/Icons.jsx';

export function SettingsPanel({
  notificationGlobalConfig,
  setNotificationGlobalConfig,
  notificationSaving,
  handleSaveGlobalConfig,
}) {
  return (
    <SectionCard
      title="全局配置选项"
      icon={<Settings className="w-4 h-4 text-brand" />}
      bodyPadding="sm"
      bodyClassName="space-y-4"
    >
      <div className="grid gap-4">
        <Input
          size="sm"
          label="看板基准 URL"
          description="设置后，通知会附带看板链接。"
          placeholder="https://monitor.domain.com"
          value={notificationGlobalConfig.base_url || ''}
          onChange={(e) => setNotificationGlobalConfig(prev => ({ ...prev, base_url: e.target.value }))}
        />

        <div className="grid gap-4 cq-sm:grid-cols-2">
          <Input
            size="sm"
            label="全局限频（条/小时）"
            description="每小时通知上限；超限后仅推 Critical。"
            type="number"
            min="0"
            value={notificationGlobalConfig.global_rate_limit_per_hour ?? 100}
            onChange={(e) => setNotificationGlobalConfig(prev => ({ ...prev, global_rate_limit_per_hour: parseInt(e.target.value, 10) || 0 }))}
          />
          <Input
            size="sm"
            label="聚合窗口（秒）"
            description="窗口内同渠道通知合并发送。"
            type="number"
            min="0"
            value={notificationGlobalConfig.batch_interval_seconds ?? 30}
            onChange={(e) => setNotificationGlobalConfig(prev => ({ ...prev, batch_interval_seconds: parseInt(e.target.value, 10) || 0 }))}
          />
        </div>

        <div className="flex items-center justify-between rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3.5">
          <div>
            <div className="text-xs font-semibold text-kumo-strong">启用通知聚合</div>
            <div className="mt-0.5 text-[11px] text-kumo-subtle">窗口内相同告警合并发送。</div>
          </div>
          <Switch
            size="sm"
            checked={!!notificationGlobalConfig.enable_batch}
            onCheckedChange={(checked) => setNotificationGlobalConfig(prev => ({ ...prev, enable_batch: checked }))}
          />
        </div>
      </div>

      <div className="flex justify-end border-t border-kumo-line pt-3 mt-2">
        <Button size="sm" variant="primary" onClick={handleSaveGlobalConfig} loading={notificationSaving} icon={<Save className="w-3.5 h-3.5" />}>
          保存全局配置
        </Button>
      </div>
    </SectionCard>
  );
}
