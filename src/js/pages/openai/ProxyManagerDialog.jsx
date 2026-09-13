import { ArrowDown } from '@phosphor-icons/react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Loader } from '@cloudflare/kumo';
import { formatDateTime } from '../../modules/utils.js';
import {
  Plus,
  Trash,
  Edit,
  X,
  RefreshCw,
  Activity,
  Check,
  Globe,
  LogList,
  ChevronDown,
} from '../../components/Icons.jsx';
import { parseProxyEntry } from './utils.js';
import { PROXY_PREVIEW_LIMIT } from './constants.js';
import { ProxyRuntimeMeta } from './ProxyRuntimeMeta.jsx';

export function ProxyManagerDialog({ endpointsApi }) {
  const {
    proxyManagerOpen, setProxyManagerOpen,
    endpointForm,
    addEndpointProxy,
    setProxyBatchOpen,
    proxyFileInputRef,
    proxyImportLoading,
    importProxyFile,
    setSubscriptionUrlOpen,
    probeAllProxies,
    probingProxies,
    disabledProxyCount,
    unbanAllProxies,
    unbanningProxies,
    proxyBatchOpen,
    proxyBatchText, setProxyBatchText,
    saveProxyBatch,
    subscriptionUrlOpen,
    subscriptionUrl, setSubscriptionUrl,
    resolveSubscriptionProxies,
    expandedBatchId, setExpandedBatchId,
    removeProxyBatch,
    disabledProxyUntil,
    removeProxyFromBatch,
    manualProxyEntries,
    manualProxyExpanded, setManualProxyExpanded,
    editingProxyIndex, setEditingProxyIndex,
    updateEndpointProxy,
    proxyRuntimeStates,
    removeEndpointProxy,
  } = endpointsApi;
  return (
      <Dialog.Root open={proxyManagerOpen} onOpenChange={setProxyManagerOpen}>
        <Dialog className="@container flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(38rem,calc(100vw-1rem))] !max-w-[min(38rem,calc(100vw-1rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 border-b border-kumo-line px-4 py-3 cq-sm:px-5 cq-sm:py-4">
            <Dialog.Title className="text-sm font-semibold text-kumo-strong">
              出口代理池（{endpointForm.proxyPool?.length || 0}）
            </Dialog.Title>
            <Dialog.Description className="mt-0.5 text-xs text-kumo-subtle">
              请求按池轮换出口 IP。适合 IP 敏感的源；留空则直连。
            </Dialog.Description>
          </div>

          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 py-3 scrollbar-thin cq-sm:px-5 cq-sm:py-4">
            <div className="flex flex-wrap items-center gap-2">
              <Button
                size="xs"
                variant="outline"
                onClick={addEndpointProxy}
                icon={<Plus className="h-3.5 w-3.5" />}
              >
                添加代理
              </Button>
              <Button
                size="xs"
                variant="outline"
                onClick={() => setProxyBatchOpen(current => !current)}
                icon={<LogList className="h-3.5 w-3.5" />}
              >
                批量添加
              </Button>
              <Button
                size="xs"
                variant="outline"
                onClick={() => proxyFileInputRef.current?.click()}
                disabled={proxyImportLoading}
                icon={proxyImportLoading ? <Loader size="sm" /> : <ArrowDown className="h-3.5 w-3.5" />}
              >
                {proxyImportLoading ? '解析中...' : '导入文件'}
              </Button>
              <input
                ref={proxyFileInputRef}
                type="file"
                accept=".txt,.list,.conf,.csv,text/plain"
                className="hidden"
                onChange={e => importProxyFile(e.target.files?.[0])}
              />
              <Button
                size="xs"
                variant="outline"
                onClick={() => setSubscriptionUrlOpen(current => !current)}
                icon={<Globe className="h-3.5 w-3.5" />}
              >
                订阅链接导入
              </Button>
              <Button
                size="xs"
                variant="outline"
                onClick={probeAllProxies}
                disabled={probingProxies}
                icon={probingProxies ? <Loader size="sm" /> : <Activity className="h-3.5 w-3.5" />}
                title="立即对全部出口做一次连通性探活并记录出口 IP"
              >
                {probingProxies ? '测试中...' : '批量测试'}
              </Button>
              {disabledProxyCount > 0 && (
                <Button
                  size="xs"
                  variant="secondary-destructive"
                  onClick={unbanAllProxies}
                  disabled={unbanningProxies}
                  icon={unbanningProxies ? <Loader size="sm" /> : <RefreshCw className="h-3.5 w-3.5" />}
                  title="清除全部冷却 / 429 冻结 / 坏代理沉淀，使被禁用的出口立即恢复可选"
                >
                  {unbanningProxies ? '解封中...' : `一键解封（${disabledProxyCount}）`}
                </Button>
              )}
            </div>

            {proxyBatchOpen && (
              <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3">
                <Textarea
                  size="sm"
                  value={proxyBatchText}
                  onChange={e => setProxyBatchText(e.target.value)}
                  placeholder={'每行一个代理地址，支持 socks5://、http(s):// 或 host:port\n如：\nsocks5://user:pass@1.2.3.4:1080\nhttp://5.6.7.8:8080'}
                  spellCheck={false}
                  rows={5}
                  className="w-full font-mono text-[0.85em]"
                />
                <div className="flex items-center justify-end gap-2">
                  <Button
                    size="xs"
                    variant="ghost"
                    onClick={() => {
                      setProxyBatchText('');
                      setProxyBatchOpen(false);
                    }}
                  >
                    取消
                  </Button>
                  <Button
                    size="xs"
                    variant="primary"
                    onClick={saveProxyBatch}
                    icon={<Check className="h-3.5 w-3.5" />}
                  >
                    确定添加
                  </Button>
                </div>
              </div>
            )}

            {subscriptionUrlOpen && (
              <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3">
                <Input
                  size="sm"
                  type="url"
                  value={subscriptionUrl}
                  aria-label="订阅 URL"
                  onChange={e => setSubscriptionUrl(e.target.value)}
                  placeholder="https://example.com/sub?token=xxx"
                  spellCheck={false}
                  autoComplete="off"
                  data-1p-ignore
                  className="w-full font-mono text-[0.85em]"
                />
                <p className="text-xs leading-snug text-kumo-subtle">
                  后端将拉取订阅并解析其中的 socks/http 节点，导入为出口代理。仅本机/服务器能访问的节点可用。
                </p>
                <div className="flex items-center justify-end gap-2">
                  <Button
                    size="xs"
                    variant="ghost"
                    onClick={() => {
                      setSubscriptionUrl('');
                      setSubscriptionUrlOpen(false);
                    }}
                  >
                    取消
                  </Button>
                  <Button
                    size="xs"
                    variant="primary"
                    onClick={resolveSubscriptionProxies}
                    disabled={proxyImportLoading}
                    icon={proxyImportLoading ? <Loader size="sm" /> : <Globe className="h-3.5 w-3.5" />}
                  >
                    {proxyImportLoading ? '解析中...' : '解析并导入'}
                  </Button>
                </div>
              </div>
            )}

            <div className="space-y-2">
              {(endpointForm.proxyBatches || []).length > 0 && (
                <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-2">
                  <div className="px-1 pt-0.5 text-xs font-semibold text-kumo-strong">
                    导入批次
                  </div>
                  {(endpointForm.proxyBatches || []).map(batch => {
                    const expanded = expandedBatchId === batch.id;
                    return (
                      <div key={batch.id} className="overflow-hidden rounded-md border border-kumo-line bg-kumo-base">
                        <div className="flex min-w-0 items-center gap-2 px-3 py-2">
                          <Button
                            shape="square"
                            size="sm"
                            variant="ghost"
                            aria-label={expanded ? '收起' : '展开'}
                            onClick={() => setExpandedBatchId(expanded ? null : batch.id)}
                            icon={<ChevronDown className={`h-3.5 w-3.5 transition-transform ${expanded ? 'rotate-180' : ''}`} />}
                          />
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-xs font-semibold text-kumo-strong">{batch.name}</div>
                            <div className="truncate font-mono text-[11px] text-kumo-subtle">
                              {batch.proxies?.length || 0} 条 · {formatDateTime(batch.createdAt)}
                            </div>
                          </div>
                          <Button
                            size="sm"
                            shape="square"
                            variant="secondary-destructive"
                            aria-label={`移除批次 ${batch.name}`}
                            onClick={() => removeProxyBatch(batch)}
                            icon={<Trash className="h-3.5 w-3.5" />}
                          />
                        </div>
                        {expanded && (
                          <div className="space-y-1.5 border-t border-kumo-line p-2">
                            {(batch.proxies || []).slice(0, PROXY_PREVIEW_LIMIT).map(proxy => {
                            const disabled = disabledProxyUntil(proxy);
                            return (
                              <div key={proxy} className="flex min-w-0 items-center gap-2">
                                <span
                                  className={`min-w-0 flex-1 truncate rounded border px-2 py-1 font-mono text-[11px] ${
                                    disabled
                                      ? 'border-kumo-danger/50 bg-kumo-danger/10 text-kumo-danger'
                                      : 'border-kumo-line bg-kumo-recessed/25 text-kumo-subtle'
                                  }`}
                                  title={disabled ? `${proxy}\n${disabled.label}` : proxy}
                                >
                                  {proxy}
                                </span>
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant="secondary-destructive"
                                  aria-label="移出此条"
                                  onClick={() => removeProxyFromBatch(batch, proxy)}
                                  title="移出此条"
                                  icon={<X className="h-3 w-3" />}
                                />
                              </div>
                            );
                          })}
                            {(batch.proxies || []).length > PROXY_PREVIEW_LIMIT && (
                              <p className="px-1 text-[11px] text-kumo-subtle">
                                仅预览前 {PROXY_PREVIEW_LIMIT} 条，共 {batch.proxies.length} 条
                              </p>
                            )}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>

            <div className="space-y-2">
              {manualProxyEntries.length > 0 && (
                <div className="flex items-center gap-2 px-1 pt-1">
                  <span className="text-xs font-semibold text-kumo-strong">
                    手动 / 未分组代理（{manualProxyEntries.length}）
                  </span>
                  <Button
                    shape="square"
                    size="xs"
                    variant="ghost"
                    aria-label={manualProxyExpanded ? '收起手动代理' : '展开手动代理'}
                    onClick={() => setManualProxyExpanded(current => !current)}
                    icon={<ChevronDown className={`h-3.5 w-3.5 transition-transform ${manualProxyExpanded ? 'rotate-180' : ''}`} />}
                  />
                </div>
              )}
              {manualProxyExpanded &&
                manualProxyEntries.slice(0, PROXY_PREVIEW_LIMIT).map(({ proxy, index }) => {
                const entry = parseProxyEntry(proxy);
                const editing = editingProxyIndex === index;
                const disabled = disabledProxyUntil(proxy);
                return (
                  <div key={`m-${index}`} className="flex min-w-0 items-center gap-2">
                    {editing ? (
                      <Input
                        size="sm"
                        type="text"
                        value={proxy}
                        aria-label="代理地址"
                        onChange={e => updateEndpointProxy(index, e.target.value)}
                        onBlur={() => setEditingProxyIndex(-1)}
                        onKeyDown={e => {
                          if (e.key === 'Enter' || e.key === 'Escape') setEditingProxyIndex(-1);
                        }}
                        spellCheck={false}
                        autoComplete="off"
                        data-1p-ignore
                        autoFocus
                        className="min-w-0 flex-1 font-mono text-[0.85em] text-kumo-strong"
                      />
                    ) : (
                      <div
                        className={`min-w-0 flex-1 cursor-pointer rounded-md border px-3 py-2 ${
                          disabled
                            ? 'border-kumo-danger/50 bg-kumo-danger/10'
                            : 'border-kumo-line bg-kumo-recessed/25'
                        }`}
                        onClick={() => setEditingProxyIndex(index)}
                        title={`${entry.full}${disabled ? `\n\n${disabled.label}` : ''}\n点击可编辑完整代理地址`}
                      >
                        {entry.label ? (
                          <div className="min-w-0">
                            <div className="flex min-w-0 items-baseline gap-1.5">
                              <span className={`truncate text-sm font-semibold ${disabled ? 'text-kumo-danger' : 'text-kumo-strong'}`}>
                                {entry.label}
                              </span>
                              {entry.host && entry.host !== entry.label && (
                                <span className={`shrink-0 font-mono text-[11px] ${disabled ? 'text-kumo-danger/80' : 'text-kumo-subtle'}`}>
                                  {entry.host}
                                </span>
                              )}
                            </div>
                            <ProxyRuntimeMeta proxy={proxy} state={proxyRuntimeStates[proxy]} />
                          </div>
                        ) : (
                          <div className={`truncate text-sm ${disabled ? 'text-kumo-danger' : 'text-kumo-subtle'}`}>空代理</div>
                        )}
                      </div>
                    )}
                    <Button
                      shape="square"
                      size="sm"
                      variant={editing ? 'primary' : 'secondary'}
                      aria-label="编辑代理"
                      onClick={() => setEditingProxyIndex(editing ? -1 : index)}
                      title={editing ? '完成编辑' : '编辑代理'}
                      icon={editing ? <Check className="h-3.5 w-3.5" /> : <Edit className="h-3.5 w-3.5" />}
                    />
                    <Button
                      shape="square"
                      size="sm"
                      variant="secondary-destructive"
                      aria-label="删除代理"
                      onClick={() => removeEndpointProxy(index)}
                      title="删除代理"
                      icon={<Trash className="h-3.5 w-3.5" />}
                    />
                  </div>
                );
              })}
              {!endpointForm.proxyPool?.length && (
                <div className="rounded-md border border-dashed border-kumo-line py-8 text-center text-xs text-kumo-subtle">
                  暂无代理。
                </div>
              )}
              {manualProxyEntries.length > PROXY_PREVIEW_LIMIT && (
                <p className="text-xs text-kumo-subtle">
                  仅预览前 {PROXY_PREVIEW_LIMIT} 条，共 {manualProxyEntries.length} 条（保存后全部生效）。
                </p>
              )}
            </div>
          </div>

          <div className="flex justify-end gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 cq-sm:px-5">
            <Dialog.Close
              render={props => (
                <Button size="sm" {...props} variant="secondary">
                  完成
                </Button>
              )}
            />
          </div>
        </Dialog>
      </Dialog.Root>

  );
}
