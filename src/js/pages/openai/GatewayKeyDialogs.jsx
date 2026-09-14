import { CalendarDotsIcon } from '@phosphor-icons/react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import {
  Badge,
  ClipboardText,
  Collapsible,
  DatePicker,
  Label,
  Popover,
} from '@cloudflare/kumo';
import { formatDateTime } from '../../modules/utils.js';
import { X } from '../../components/Icons.jsx';
import { MultiSelectPopover } from './MultiSelectPopover.jsx';
import { parseLocalDateTime } from './utils.js';
import { GATEWAY_EXPIRY_HOURS, GATEWAY_EXPIRY_MINUTES } from './constants.js';

export function GatewayKeyDialogs({ keysApi, endpointsApi }) {
  const {
    gatewayKeyDialogOpen, setGatewayKeyDialogOpen,
    editingGatewayKey,
    gatewayKeyForm, setGatewayKeyForm,
    gatewayKeyAdvancedOpen, setGatewayKeyAdvancedOpen,
    gatewayKeyFormError,
    gatewayKeySaving,
    newGatewayKey, setNewGatewayKey,
    applyGatewayKeyExpiryPreset,
    updateGatewayKeyExpiryDate,
    updateGatewayKeyExpiryTime,
    toggleGatewayKeyListItem,
    removeGatewayKeyListItem,
    saveGatewayKey,
  } = keysApi;
  const { allModels, endpoints } = endpointsApi;
  return (
    <>
      <Dialog.Root open={gatewayKeyDialogOpen} onOpenChange={setGatewayKeyDialogOpen}>
        <Dialog className="!w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
            {editingGatewayKey ? '编辑 API 密钥' : '新建 API 密钥'}
          </Dialog.Title>

          <div className="space-y-4">
            <Input
              size="sm"
              label="名称"
              value={gatewayKeyForm.name}
              onChange={e => setGatewayKeyForm({ ...gatewayKeyForm, name: e.target.value })}
              placeholder="如：生产环境、Open WebUI"
              className="w-full text-sm text-kumo-strong"
            />

            <div className="space-y-1.5">
              <Label>
                过期时间
                <span className="font-normal text-kumo-subtle">（可选）</span>
              </Label>
              <div className="flex flex-wrap items-center gap-1.5">
                {[
                  { label: '1 天', days: 1 },
                  { label: '14 天', days: 14 },
                  { label: '30 天', days: 30 },
                  { label: '永久', days: 0 },
                ].map(preset => (
                  <Button
                    key={preset.label}
                    size="xs"
                    variant={
                      (preset.days === 0 && !gatewayKeyForm.expiresAt) ||
                      (preset.days > 0 &&
                        gatewayKeyForm.expiresAt &&
                        Math.abs(
                          new Date(gatewayKeyForm.expiresAt).getTime() -
                            (Date.now() + preset.days * 24 * 60 * 60 * 1000)
                        ) < 60 * 1000)
                        ? 'primary'
                        : 'outline'
                    }
                    onClick={() => applyGatewayKeyExpiryPreset(preset.days)}
                  >
                    {preset.label}
                  </Button>
                ))}
              </div>
              <div className="flex items-center gap-1.5">
                <Popover>
                  <Popover.Trigger
                    render={
                      <Button
                        size="sm"
                        variant="outline"
                        icon={CalendarDotsIcon}
                        className="min-w-0 flex-1 justify-start font-normal"
                      />
                    }
                  >
                    <span className="truncate">
                      {gatewayKeyForm.expiresAt
                        ? formatDateTime(gatewayKeyForm.expiresAt)
                        : '永不过期'}
                    </span>
                  </Popover.Trigger>
                  <Popover.Content className="p-3">
                    <DatePicker
                      size="sm"
                      mode="single"
                      selected={parseLocalDateTime(gatewayKeyForm.expiresAt)}
                      onChange={updateGatewayKeyExpiryDate}
                    />
                    {gatewayKeyForm.expiresAt && (
                      <div className="mt-2 flex justify-end border-t border-kumo-line pt-2">
                        <Button
                          size="xs"
                          variant="ghost"
                          onClick={() =>
                            setGatewayKeyForm(current => ({ ...current, expiresAt: '' }))
                          }
                        >
                          清除
                        </Button>
                      </div>
                    )}
                  </Popover.Content>
                </Popover>
                <Select alignItemWithTrigger
                  size="sm"
                  aria-label="过期小时"
                  disabled={!gatewayKeyForm.expiresAt}
                  value={gatewayKeyForm.expiresAt.slice(11, 13)}
                  onValueChange={value => updateGatewayKeyExpiryTime('hour', value)}
                  items={GATEWAY_EXPIRY_HOURS}
                  className="w-[3.25rem] shrink-0"
                />
                <span className="shrink-0 text-sm text-kumo-subtle">:</span>
                <Select alignItemWithTrigger
                  size="sm"
                  aria-label="过期分钟"
                  disabled={!gatewayKeyForm.expiresAt}
                  value={gatewayKeyForm.expiresAt.slice(14, 16)}
                  onValueChange={value => updateGatewayKeyExpiryTime('minute', value)}
                  items={GATEWAY_EXPIRY_MINUTES}
                  className="w-[3.25rem] shrink-0"
                />
              </div>
            </div>

            <Collapsible.Root
              open={gatewayKeyAdvancedOpen}
              onOpenChange={setGatewayKeyAdvancedOpen}
            >
              <Collapsible.DefaultTrigger>高级过滤</Collapsible.DefaultTrigger>
              <Collapsible.DefaultPanel className="mt-2">
                <div className="space-y-4">
                  <div className="space-y-1.5">
                    <Label>
                      允许的模型（白名单）
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <MultiSelectPopover
                        triggerLabel="选择模型"
                        searchPlaceholder="搜索模型…"
                        emptyText="暂无可用模型"
                        options={allModels.map(m => ({ value: m.id, label: m.id }))}
                        selected={gatewayKeyForm.allowedModels || []}
                        onToggle={(value, checked) => toggleGatewayKeyListItem('allowedModels', value, checked)}
                        onClear={() => setGatewayKeyForm(current => ({ ...current, allowedModels: [] }))}
                      />
                      {(gatewayKeyForm.allowedModels || []).map(model => (
                        <Badge
                          key={model}
                          variant="outline"
                          className="max-w-full gap-1 font-mono !text-[11px] font-medium"
                        >
                          <span className="truncate">{model}</span>
                          <Button
                            size="xs"
                            shape="square"
                            variant="ghost"
                            aria-label={`移除 ${model}`}
                            onClick={() => removeGatewayKeyListItem('allowedModels', model)}
                            icon={<X className="h-3 w-3" />}
                          />
                        </Badge>
                      ))}
                    </div>
                  </div>
                  <div className="space-y-1.5">
                    <Label>
                      允许的端点（白名单）
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <MultiSelectPopover
                        triggerLabel="选择端点"
                        searchPlaceholder="搜索端点…"
                        emptyText="暂无可用端点"
                        options={endpoints.map(ep => ({ value: ep.id, label: ep.name || ep.id }))}
                        selected={gatewayKeyForm.allowedEndpoints || []}
                        onToggle={(value, checked) => toggleGatewayKeyListItem('allowedEndpoints', value, checked)}
                        onClear={() => setGatewayKeyForm(current => ({ ...current, allowedEndpoints: [] }))}
                      />
                      {(gatewayKeyForm.allowedEndpoints || []).map(endpointId => {
                        const endpointLabel = endpoints.find(ep => ep.id === endpointId)?.name || endpointId;
                        return (
                          <Badge
                            key={endpointId}
                            variant="outline"
                            className="max-w-full gap-1 !text-[11px] font-medium"
                          >
                            <span className="truncate">{endpointLabel}</span>
                            <Button
                              size="xs"
                              shape="square"
                              variant="ghost"
                              aria-label={`移除 ${endpointLabel}`}
                              onClick={() => removeGatewayKeyListItem('allowedEndpoints', endpointId)}
                              icon={<X className="h-3 w-3" />}
                            />
                          </Badge>
                        );
                      })}
                    </div>
                  </div>
                  <div className="space-y-1.5">
                    <Label>
                    Token 配额
                    <span className="font-normal text-kumo-subtle">（可选）</span>
                  </Label>
                    <Input
                      size="sm"
                      type="number"
                      min="0"
                      value={gatewayKeyForm.maxTokensQuota}
                      aria-label="Token 配额"
                      onChange={e =>
                        setGatewayKeyForm({ ...gatewayKeyForm, maxTokensQuota: e.target.value })
                      }
                      placeholder="0 = 不限制"
                      className="w-full font-mono text-[0.85em] text-kumo-strong"
                    />
                  </div>
                </div>
              </Collapsible.DefaultPanel>
            </Collapsible.Root>

            {gatewayKeyFormError && (
              <p className="text-sm font-semibold text-kumo-danger">{gatewayKeyFormError}</p>
            )}
            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close
                render={props => (
                  <Button size="sm" {...props} variant="secondary">
                    取消
                  </Button>
                )}
              />
              <Button
                size="sm"
                variant="primary"
                disabled={gatewayKeySaving}
                onClick={saveGatewayKey}
              >
                {gatewayKeySaving ? '保存中...' : '保存密钥'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={!!newGatewayKey} onOpenChange={open => !open && setNewGatewayKey(null)}>
        <Dialog className="!w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
            API 密钥已创建
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
            可立即复制，也可稍后从 API 密钥列表查看并复制。
          </Dialog.Description>
          <div className="space-y-4">
            <p className="text-sm font-medium text-kumo-strong">
              {newGatewayKey?.name || 'API Key'}
            </p>
            <ClipboardText
              size="sm"
              text={newGatewayKey?.apiKey || ''}
              className="min-w-0 w-full"
              tooltip={{ text: '复制 API Key', copiedText: 'API Key 已复制' }}
              labels={{ copyAction: '复制 API Key' }}
            />
            <div className="flex justify-end">
              <Dialog.Close
                render={props => (
                  <Button size="sm" variant="primary" {...props}>
                    我已保存
                  </Button>
                )}
              />
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 3. Health Check Config Dialog */}
    </>
  );
}
