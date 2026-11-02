import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Lock, RefreshCw, Settings } from '../../components/Icons.jsx';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { DEFAULT_FILEBOX_MAX_FILE_SIZE } from './constants.js';

export function SettingsPanel({
  fileboxSettings,
  setFileboxSettings,
  settingsMimeText,
  setSettingsMimeText,
  settingsLoading,
  loadSettings,
  saveSettings,
}) {
  return (
    <SectionCard
      title="文件柜策略"
      icon={<Settings className="h-4 w-4 text-brand" />}
      action={
        <Button size="sm" variant="secondary" onClick={loadSettings} loading={settingsLoading} icon={<RefreshCw className="h-4 w-4" />}>
          刷新
        </Button>
      }
    >
      <div className="grid gap-4 cq-md:grid-cols-2">
        <Input
          size="sm"
          label="最大文件大小 MB"
          type="number"
          min="1"
          value={Math.round((fileboxSettings.max_file_size || DEFAULT_FILEBOX_MAX_FILE_SIZE) / 1024 / 1024)}
          onChange={(event) =>
            setFileboxSettings((prev) => ({
              ...prev,
              max_file_size: Math.max(1, Number(event.target.value) || 1) * 1024 * 1024,
            }))
          }
        />
        <Input
          size="sm"
          label="默认有效期小时"
          type="number"
          min="1"
          value={fileboxSettings.default_expiry_hours || 24}
          onChange={(event) =>
            setFileboxSettings((prev) => ({
              ...prev,
              default_expiry_hours: Math.max(1, Number(event.target.value) || 24),
            }))
          }
        />
        <div className="cq-md:col-span-2">
          <Textarea label="允许 MIME 类型" value={settingsMimeText} onChange={(event) => setSettingsMimeText(event.target.value)} className="min-h-28 font-mono text-xs" placeholder="留空不限。如 image/*, application/pdf, text/plain" />
        </div>
        <div className="cq-md:col-span-2 flex items-center justify-between rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
          <div>
            <div className="text-xs font-semibold text-kumo-strong">允许公开上传</div>
            <div className="mt-1 text-[11px] text-kumo-subtle">当前接口仍要求管理员认证；保留为策略开关。</div>
          </div>
          <Switch checked={!!fileboxSettings.public_upload_enabled} onCheckedChange={(checked) => setFileboxSettings((prev) => ({ ...prev, public_upload_enabled: checked }))} />
        </div>
      </div>
      <div className="mt-4 flex justify-end border-t border-kumo-line pt-4">
        <Button size="sm" variant="primary" onClick={saveSettings} loading={settingsLoading} icon={<Lock className="h-4 w-4" />}>
          保存策略
        </Button>
      </div>
    </SectionCard>
  );
}
