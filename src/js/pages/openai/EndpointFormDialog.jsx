import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Badge, Label } from '@cloudflare/kumo';
import { cx } from '../../components/ui/AppPrimitives.jsx';
import { Plus, Trash, RotateCw, Sliders } from '../../components/Icons.jsx';
import { KeyStatusBadge } from './KeyStatusBadge.jsx';
import {
  ENDPOINT_PROTOCOL_OPTIONS,
  ENDPOINT_UPSTREAM_OPTIONS,
  GEMINI_DEFAULT_BASE_URL,
  VERTEX_DEFAULT_BASE_URL,
} from './constants.js';

export function EndpointFormDialog({ endpointsApi, proxypoolPools }) {
  const {
    endpointFormOpen, setEndpointFormOpen,
    setEndpointKeyChecks,
    editingEndpoint,
    endpointForm, setEndpointForm,
    appendEndpointKey,
    endpointKeyChecking,
    checkEndpointKeys,
    keyDeleteConfirmActive,
    removeEndpointKey,
    endpointKeyChecks,
    addEndpointHeader,
    updateEndpointHeader,
    removeEndpointHeader,
    setProxyManagerOpen,
    manualProxyEntries,
    endpointFormError,
    endpointSaving,
    saveEndpoint,
  } = endpointsApi;
  return (
      <Dialog.Root
        open={endpointFormOpen}
        onOpenChange={open => {
          setEndpointFormOpen(open);
          if (!open) setEndpointKeyChecks([]);
        }}
      >
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),46rem)] !w-[min(58rem,calc(100vw-2rem))] !max-w-[min(58rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
              {editingEndpoint ? '编辑端点' : '添加 API 端点'}
            </Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              配置 OpenAI 兼容 API 端点，用于中转或对话。
            </Dialog.Description>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="grid grid-cols-2 gap-x-5 gap-y-4">
              {/* ====== 左列：基本信息 ====== */}
              <div className="space-y-4">
                <Input
                  size="sm"
                  label="名称"
                  type="text"
                  value={endpointForm.name}
                  onChange={e => setEndpointForm({ ...endpointForm, name: e.target.value })}
                  placeholder="如：DeepSeek 官方"
                  className="w-full text-kumo-strong text-sm font-sans"
                />

                <div className="grid grid-cols-[auto_minmax(0,1fr)] items-end gap-2">
                  <Select alignItemWithTrigger
                    size="sm"
                    label="上游协议"
                    value={endpointForm.upstreamType || 'openai'}
                    onValueChange={value =>
                      setEndpointForm(current => {
                        const defaultUrls = {
                          gemini: GEMINI_DEFAULT_BASE_URL,
                          vertex: VERTEX_DEFAULT_BASE_URL,
                        };
                        return {
                          ...current,
                          upstreamType: value,
                          ...(defaultUrls[value] && !current.baseUrl
                            ? { baseUrl: defaultUrls[value] }
                            : {}),
                        };
                      })
                    }
                    items={ENDPOINT_UPSTREAM_OPTIONS}
                    className="w-40"
                  />
                  <Input
                    size="sm"
                    label="Base URL"
                    type="text"
                    value={endpointForm.baseUrl}
                    onChange={e => setEndpointForm({ ...endpointForm, baseUrl: e.target.value })}
                    placeholder="https://api.openai.com/v1"
                    className="w-full text-kumo-strong text-[0.9em] font-mono"
                  />
                </div>

                <Input
                  size="sm"
                  label="模型列表 API（可选）"
                  type="text"
                  value={endpointForm.modelsUrl || ''}
                  onChange={e => setEndpointForm({ ...endpointForm, modelsUrl: e.target.value })}
                  placeholder="模型列表不在标准路径时在此填完整地址"
                  className="w-full text-kumo-strong text-[0.9em] font-mono"
                />

                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <Label>API Key 列表</Label>
                    <div className="flex items-center gap-1.5">
                      <Button
                        size="xs"
                        variant="outline"
                        onClick={appendEndpointKey}
                        icon={<Plus className="h-3.5 w-3.5" />}
                      >
                        Key
                      </Button>
                      <Button
                        size="xs"
                        variant="secondary"
                        type="button"
                        disabled={endpointKeyChecking || !editingEndpoint}
                        onClick={() =>
                          checkEndpointKeys(
                            [endpointForm.apiKey, ...(endpointForm.apiKeys || [])],
                            editingEndpoint?.id
                          )
                        }
                      >
                        <RotateCw className={cx(endpointKeyChecking && 'animate-spin')} size={14} />
                        {endpointKeyChecking ? '检测中' : '检测'}
                      </Button>
                    </div>
                  </div>

                  <div className="space-y-1.5">
                    {[endpointForm.apiKey, ...(endpointForm.apiKeys || [])].map((key, rowIndex) => (
                      <div
                        key={rowIndex}
                        className="grid grid-cols-[2rem_minmax(0,1fr)_1.75rem_auto] items-center gap-1.5"
                      >
                        <Badge
                          variant="outline"
                          className="w-full justify-center text-center font-mono !text-[11px] leading-none"
                        >
                          K{rowIndex + 1}
                        </Badge>
                        <Input
                          size="sm"
                          type="text"
                          value={key}
                          aria-label={`API Key K${rowIndex + 1}`}
                          onChange={e => {
                            const value = e.target.value;
                            setEndpointForm(current => {
                              if (rowIndex === 0) {
                                return { ...current, apiKey: value };
                              }
                              return {
                                ...current,
                                apiKeys: (current.apiKeys || []).map((k, j) =>
                                  j === rowIndex - 1 ? value : k
                                ),
                              };
                            });
                            setEndpointKeyChecks(prev => {
                              const next = [...prev];
                              next[rowIndex] = null;
                              return next;
                            });
                          }}
                          placeholder="sk-xxxxxxxxxxxxxxxxxxxxxxxx"
                          autoComplete="off"
                          data-1p-ignore
                          data-lpignore="true"
                          data-bwignore="true"
                          data-form-type="other"
                          spellCheck={false}
                          className="w-full text-kumo-strong text-[0.9em] font-mono"
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant={keyDeleteConfirmActive(rowIndex) ? 'destructive' : 'secondary-destructive'}
                          aria-label={
                            keyDeleteConfirmActive(rowIndex)
                              ? `再次点击确认删除 Key K${rowIndex + 1}`
                              : `删除 Key K${rowIndex + 1}`
                          }
                          onClick={() => removeEndpointKey(rowIndex)}
                          title={
                            keyDeleteConfirmActive(rowIndex)
                              ? '再次点击确认删除'
                              : '删除此 Key'
                          }
                          icon={<Trash className="h-3.5 w-3.5" />}
                        />
                        <KeyStatusBadge check={endpointKeyChecks?.[rowIndex]} />
                      </div>
                    ))}
                  </div>
                </div>

                <div className="flex min-h-8 flex-wrap items-center gap-2">
                  <span className="text-xs text-kumo-strong">多 Key 单请求重试次数</span>
                  <div className="flex min-w-0 flex-1 items-center justify-end gap-1">
                    <Input
                      size="xs"
                      className="w-20"
                      type="number"
                      min={1}
                      max={5}
                      aria-label="多 Key 单请求重试次数"
                      value={endpointForm.keyRetryRounds ?? 2}
                      onChange={e => {
                        const value = parseInt(e.target.value, 10);
                        setEndpointForm(current => ({
                          ...current,
                          keyRetryRounds: Number.isNaN(value) || value < 1 ? 1 : value,
                        }));
                      }}
                    />
                    <span className="text-xs text-kumo-subtle">次</span>
                  </div>
                </div>

                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <Label>
                      自定义请求头
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={addEndpointHeader}
                      icon={<Plus className="h-3.5 w-3.5" />}
                    >
                      添加请求头
                    </Button>
                  </div>
                  <div className="space-y-2">
                    {(endpointForm.headers || []).map((header, index) => (
                      <div
                        key={index}
                        className="grid grid-cols-[minmax(0,1.2fr)_minmax(0,1.8fr)_2rem] items-center gap-2"
                      >
                        <Input
                          size="sm"
                          type="text"
                          value={header.name}
                          aria-label="Header 名称"
                          onChange={e => updateEndpointHeader(index, 'name', e.target.value)}
                          placeholder="Header 名称"
                          spellCheck={false}
                          autoComplete="off"
                          data-1p-ignore
                          className="w-full text-kumo-strong font-mono text-[0.85em]"
                        />
                        <Input
                          size="sm"
                          type="text"
                          value={header.value}
                          aria-label="Header 值"
                          onChange={e => updateEndpointHeader(index, 'value', e.target.value)}
                          placeholder="Header 值"
                          spellCheck={false}
                          autoComplete="off"
                          data-1p-ignore
                          className="w-full text-kumo-strong font-mono text-[0.85em]"
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary-destructive"
                          aria-label="删除请求头"
                          onClick={() => removeEndpointHeader(index)}
                          title="删除请求头"
                          icon={<Trash className="h-3.5 w-3.5" />}
                        />
                      </div>
                    ))}
                  </div>
                </div>

                <Select alignItemWithTrigger
                  size="sm"
                  label="连接协议"
                  value={endpointForm.protocol || 'auto'}
                  onValueChange={value => setEndpointForm(current => ({ ...current, protocol: value }))}
                  items={ENDPOINT_PROTOCOL_OPTIONS}
                  className="w-full"
                />
              </div>

              {/* ====== 右列：连接与代理 ====== */}
              <div className="space-y-4">
                <div className="space-y-1.5">
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <Label>
                      使用独立代理池
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                  </div>
                  <Select alignItemWithTrigger
                    size="sm"
                    className="w-full"
                    value={endpointForm.proxyPoolId || ''}
                    onValueChange={value => setEndpointForm(f => ({ ...f, proxyPoolId: value || '' }))}
                    placeholder="不使用（用下方内联代理池或直连）"
                  >
                    <Select.Option value="">不使用（用下方内联代理池或直连）</Select.Option>
                    {proxypoolPools.map(p => (
                      <Select.Option key={p.id} value={p.id}>
                        {p.name || p.id}（{p.proxies?.length || 0} 个出口）
                      </Select.Option>
                    ))}
                  </Select>
                </div>

                <div className="space-y-1.5">
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <Label>
                      出口代理池
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={() => setProxyManagerOpen(true)}
                      icon={<Sliders className="h-3.5 w-3.5" />}
                    >
                      管理代理池（{endpointForm.proxyPool?.length || 0}）
                    </Button>
                  </div>
                  <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-2">
                    {endpointForm.proxyPool?.length > 0 ? (
                      <div className="flex flex-wrap items-center gap-1.5">
                        {(endpointForm.proxyBatches || []).map(batch => (
                          <Badge
                            key={batch.id}
                            variant="outline"
                            className="w-fit gap-1 !text-[11px] font-medium"
                            title={`${batch.name}\n${batch.proxies?.length || 0} 条`}
                          >
                            <span className="max-w-40 truncate">{batch.name}</span>
                            <span className="shrink-0 font-mono text-[10px] text-kumo-subtle">
                              {batch.proxies?.length || 0} 条
                            </span>
                          </Badge>
                        ))}
                        {manualProxyEntries.length > 0 && (
                          <Badge
                            variant="outline"
                            className="w-fit gap-1 !text-[11px] font-medium"
                            title="手动添加/粘贴/订阅导入的代理"
                          >
                            <span>手动</span>
                            <span className="shrink-0 font-mono text-[10px] text-kumo-subtle">
                              {manualProxyEntries.length} 条
                            </span>
                          </Badge>
                        )}
                        <span className="text-[11px] text-kumo-subtle">
                          共 {endpointForm.proxyPool.length} 条
                        </span>
                      </div>
                    ) : (
                      <span className="text-xs text-kumo-subtle">
                        未配置代理，请求将直连上游。
                      </span>
                    )}
                  </div>
                </div>

                <div className="flex min-h-8 items-center gap-2">
                  <Switch
                    size="sm"
                    aria-label="代理开关"
                    checked={!!endpointForm.proxyEnabled}
                    onCheckedChange={checked =>
                      setEndpointForm(current => ({ ...current, proxyEnabled: checked }))
                    }
                  />
                  <span className="text-xs text-kumo-strong">代理开关</span>
                </div>
                <div className="flex min-h-8 items-center gap-2">
                  <Switch
                    size="sm"
                    aria-label="限流自动切换代理"
                    checked={!!endpointForm.autoSwitch}
                    disabled={!endpointForm.proxyEnabled}
                    onCheckedChange={checked =>
                      setEndpointForm(current => ({ ...current, autoSwitch: checked }))
                    }
                  />
                  <span className="text-xs text-kumo-subtle">限流或连接失败自动切换代理</span>
                </div>
                <div className="flex min-h-8 items-center gap-2">
                  <Switch
                    size="sm"
                    aria-label="允许直连兜底"
                    checked={!!endpointForm.allowDirectFallback}
                    disabled={!endpointForm.proxyEnabled}
                    onCheckedChange={checked =>
                      setEndpointForm(current => ({ ...current, allowDirectFallback: checked }))
                    }
                  />
                  <span className="text-xs text-kumo-subtle">代理池异常时允许直连兜底</span>
                </div>
                <div className="flex min-h-8 items-center gap-2">
                  <Switch
                    size="sm"
                    aria-label="429等待重试"
                    checked={!!endpointForm.rateLimitRetryEnabled}
                    onCheckedChange={checked =>
                      setEndpointForm(current => ({ ...current, rateLimitRetryEnabled: checked }))
                    }
                  />
                  <span className="text-xs text-kumo-strong">429 等待重试</span>
                  {endpointForm.rateLimitRetryEnabled && (
                    <div className="flex min-w-0 flex-1 items-center justify-end gap-1">
                      <Input
                        size="sm"
                        className="w-20"
                        type="number"
                        min={1}
                        max={60}
                        aria-label="429 重试等待秒数"
                        value={endpointForm.rateLimitRetryWaitSeconds ?? 10}
                        onChange={e => {
                          const value = parseInt(e.target.value, 10);
                          setEndpointForm(current => ({
                            ...current,
                            rateLimitRetryWaitSeconds: Number.isNaN(value) ? 0 : value,
                          }));
                        }}
                      />
                      <span className="text-xs text-kumo-subtle">秒</span>
                    </div>
                  )}
                </div>
              </div>
            </div>

            {endpointFormError && (
                <p className="mt-4 text-sm text-kumo-danger font-semibold">{endpointFormError}</p>
              )}
            </div>

            <div className="flex shrink-0 flex-wrap items-center justify-end gap-3 border-t border-kumo-line bg-kumo-base px-6 py-4">
              <Dialog.Close
                render={props => (
                  <Button size="sm" {...props} variant="secondary">
                    取消
                  </Button>
                )}
              />
              <Button size="sm" variant="primary" disabled={endpointSaving} onClick={saveEndpoint}>
                {endpointSaving ? '保存中...' : '保存端点'}
              </Button>
            </div>
        </Dialog>
      </Dialog.Root>
  );
}
