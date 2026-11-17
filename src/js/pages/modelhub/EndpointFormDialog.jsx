import { useLayoutEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Badge, Dialog, Label } from '@cloudflare/kumo';
import { Plus, Trash, Sliders } from '../../components/Icons.jsx';
import {
  ENDPOINT_PROTOCOL_OPTIONS,
  ENDPOINT_UPSTREAM_OPTIONS,
  GEMINI_DEFAULT_BASE_URL,
  VERTEX_DEFAULT_BASE_URL,
  KEY_STRATEGY_OPTIONS,
} from './constants.js';

export function EndpointFormDialog({ endpointsApi, proxypoolPools }) {
  const {
    endpointFormOpen, setEndpointFormOpen,
    editingEndpoint,
    endpointForm, setEndpointForm,
    appendEndpointKey,
    keyDeleteConfirmActive,
    removeEndpointKey,
    addEndpointHeader,
    updateEndpointHeader,
    removeEndpointHeader,
    setProxyManagerOpen,
    manualProxyEntries,
    endpointFormError,
    endpointSaving,
    saveEndpoint,
  } = endpointsApi;
  const [endpointKeyBatchOpen, setEndpointKeyBatchOpen] = useState(false);
  const [endpointKeyBatchText, setEndpointKeyBatchText] = useState('');

  const openEndpointKeyBatch = () => {
    const keys = [
      endpointForm.apiKey,
      ...(endpointForm.apiKeys || []),
    ];
    setEndpointKeyBatchText(keys.filter(k => k && k.trim()).join('\n'));
    setEndpointKeyBatchOpen(true);
  };

  const applyEndpointKeyBatch = () => {
    const keys = endpointKeyBatchText
      .split('\n')
      .map(k => k.trim())
      .filter(Boolean);
    setEndpointForm(current => ({
      ...current,
      apiKey: keys[0] || '',
      apiKeys: keys.slice(1),
    }));
    setEndpointKeyBatchOpen(false);
    setEndpointKeyBatchText('');
  };

  const keyListRef = useRef(null);
  useLayoutEffect(() => {
    const el = keyListRef.current;
    if (!el) return;
    const update = () => {
      el.style.setProperty('--scroll-fade-top', `${el.scrollTop}px`);
      el.style.setProperty(
        '--scroll-fade-bottom',
        `${Math.max(0, el.scrollHeight - el.scrollTop - el.clientHeight)}px`
      );
    };
    el.addEventListener('scroll', update);
    update();
    return () => el.removeEventListener('scroll', update);
  });

  return (
    <>
      <LayerDialog.Root
        open={endpointFormOpen}
        onOpenChange={open => {
          setEndpointFormOpen(open);
        }}
      >
        <LayerDialog.Content size="xl">
          <LayerDialog.Title>
            {editingEndpoint ? '编辑端点' : '添加 API 端点'}
          </LayerDialog.Title>
          <LayerDialog.Description>
            配置 OpenAI 兼容 API 端点，用于中转或对话。
          </LayerDialog.Description>

          <LayerDialog.Body>
            <div className="grid grid-cols-1 gap-x-5 gap-y-4 sm:grid-cols-2">
              {/* ====== 左列：基本信息 ====== */}
              <div className="space-y-4">
                <div className="grid grid-cols-1 items-stretch gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
                  <Input
                    size="sm"
                    label="名称"
                    type="text"
                    value={endpointForm.name}
                    onChange={e => setEndpointForm({ ...endpointForm, name: e.target.value })}
                    placeholder="如：DeepSeek 官方"
                    className="w-full text-kumo-strong text-sm font-sans"
                  />
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
                    className="w-full sm:w-40"
                  />
                </div>

                <Input
                  size="sm"
                  label="Base URL"
                  type="text"
                  value={endpointForm.baseUrl}
                  onChange={e => setEndpointForm({ ...endpointForm, baseUrl: e.target.value })}
                  placeholder="https://api.openai.com/v1"
                  className="w-full text-kumo-strong text-[0.9em] font-mono"
                />

                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <Label>API Key 列表</Label>
                    <div className="flex items-center gap-1.5">
                      <Button
                        size="xs"
                        variant="outline"
                        onClick={openEndpointKeyBatch}
                        title="一次粘贴/编辑所有 Key（每行一个）"
                      >
                        批量编辑
                      </Button>
                      <Button
                        size="xs"
                        variant="outline"
                        onClick={appendEndpointKey}
                        icon={<Plus className="h-3.5 w-3.5" />}
                      >
                        Key
                      </Button>
                    </div>
                  </div>

                  <div ref={keyListRef} className="key-list-fade max-h-64 space-y-1.5 overflow-y-auto py-1.5">
                    {[endpointForm.apiKey, ...(endpointForm.apiKeys || [])].map((key, rowIndex) => (
                      <div
                        key={rowIndex}
                        className="grid grid-cols-[2rem_minmax(0,1fr)_1.75rem] items-center gap-1.5"
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
                      </div>
                    ))}
                  </div>
                </div>

                <div className="space-y-1.5">
                  <Label>Key 策略</Label>
                  <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/20 p-3">
                    <div className="flex items-start gap-3">
                    <div className="flex min-w-0 flex-1 flex-col gap-1">
                      <span className="text-[11px] leading-none text-kumo-subtle">命中策略</span>
                      <Select
                        size="sm"
                        aria-label="命中策略"
                        value={endpointForm.keyStrategy || 'round_robin'}
                        onValueChange={value => setEndpointForm(current => ({ ...current, keyStrategy: value }))}
                        items={KEY_STRATEGY_OPTIONS}
                        className="w-full"
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <span className="text-[11px] leading-none text-kumo-subtle">重试(次)</span>
                      <Input
                        size="sm"
                        className="w-20"
                        type="number"
                        min={1}
                        max={5}
                        aria-label="每 key 单请求重试次数"
                        value={endpointForm.keyRetryRounds ?? 2}
                        onChange={e => {
                          const value = parseInt(e.target.value, 10);
                          setEndpointForm(current => ({
                            ...current,
                            keyRetryRounds: Number.isNaN(value) || value < 1 ? 1 : value,
                          }));
                        }}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <span className="text-[11px] leading-none text-kumo-subtle" title="0=关闭冷却；某 key 近期 429/401 后在冷却期内降低其命中，规避废 key 反复拉长对话">冷却(秒)</span>
                      <Input
                        size="sm"
                        className="w-20"
                        type="number"
                        min={0}
                        aria-label="回避冷却秒数"
                        value={endpointForm.keyCooldownSeconds ?? 60}
                        onChange={e => {
                          const value = parseInt(e.target.value, 10);
                          setEndpointForm(current => ({
                            ...current,
                            keyCooldownSeconds: Number.isNaN(value) || value < 0 ? 0 : value,
                          }));
                        }}
                      />
                    </div>
                    </div>
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
                        className="flex flex-wrap items-center gap-2 sm:grid sm:grid-cols-[minmax(0,1.2fr)_minmax(0,1.8fr)_2rem]"
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
                          className="w-full min-w-0 text-kumo-strong font-mono text-[0.85em] sm:w-auto"
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
                          className="min-w-0 flex-1 text-kumo-strong font-mono text-[0.85em] sm:w-auto sm:flex-none"
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
          </LayerDialog.Body>

          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Action
              type="button"
              disabled={endpointSaving}
              onClick={saveEndpoint}
              loading={endpointSaving}
            >
              保存端点
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      <Dialog.Root open={endpointKeyBatchOpen} onOpenChange={setEndpointKeyBatchOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),36rem)] !w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
              批量编辑 API Key
            </Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              每行一个 API Key，保存后将替换当前全部 Key。
            </Dialog.Description>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3">
            <Textarea
              size="sm"
              value={endpointKeyBatchText}
              onChange={e => setEndpointKeyBatchText(e.target.value)}
              placeholder={'每行一个 API Key\n如：\nsk-xxxxx-1\nsk-xxxxx-2'}
              spellCheck={false}
              autoComplete="off"
              data-1p-ignore
              data-lpignore="true"
              data-bwignore="true"
              rows={10}
              className="w-full font-mono text-[0.85em]"
            />
          </div>

          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close
              render={props => (
                <Button size="sm" {...props} variant="ghost">
                  取消
                </Button>
              )}
            />
            <Button size="sm" variant="primary" onClick={applyEndpointKeyBatch}>
              应用
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </>
  );
}
