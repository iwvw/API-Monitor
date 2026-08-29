import { useEffect, useRef, useState } from 'react';
import { Button, Switch, Select, Loader, Textarea, Input, Dialog, Table, LayerCard, Badge } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState } from '../../../components/ui/AppPrimitives.jsx';
import { Globe, Plus, RefreshCw, Trash, Upload } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { useConfirmPress } from '../../../hooks/useConfirmPress.js';
import { getAuthHeaders } from '../utils.js';

const API = '/api/proxypool';

// ProxyPoolPlugin：模型网关「插件中心」的独立代理池管理插件。
// 提供可被其他插件或网关端点复用的出口代理池：
// CRUD、批量添加、文件导入、订阅导入、批量测试探活、一键解封。
export function ProxyPoolPlugin() {
  const { isArmed, confirmPress } = useConfirmPress();
  const [pools, setPools] = useState(null);
  const [loading, setLoading] = useState(true);
  const [dialog, setDialog] = useState(null); // null | {mode:'create'|'edit', pool}
  const [form, setForm] = useState({ id: '', name: '', proxies: '' });
  const [saving, setSaving] = useState(false);
  const [selectedId, setSelectedId] = useState('');
  const [states, setStates] = useState({}); // poolId -> proxyStateItem[]
  const [probing, setProbing] = useState(false);
  const [unbanning, setUnbanning] = useState(false);
  const [batchOpen, setBatchOpen] = useState(false);
  const [batchText, setBatchText] = useState('');
  const [subscriptionOpen, setSubscriptionOpen] = useState(false);
  const [subscriptionUrl, setSubscriptionUrl] = useState('');
  const fileInputRef = useRef(null);

  const load = async () => {
    try {
      const res = await fetch(API, { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '加载失败');
      setPools(data.pools || []);
      if (!data.pools?.length) setSelectedId('');
      else if (!data.pools.some(p => p.id === selectedId)) setSelectedId(data.pools[0].id);
    } catch (e) {
      toast.error(`代理池加载失败：${e.message}`);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const loadState = async poolId => {
    try {
      const res = await fetch(`${API}/${encodeURIComponent(poolId)}/state`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) setStates(prev => ({ ...prev, [poolId]: data.proxies || [] }));
    } catch {
      /* 静默 */
    }
  };

  useEffect(() => {
    if (selectedId) loadState(selectedId);
  }, [selectedId]);

  const openCreate = () => {
    setForm({ id: '', name: '', proxies: '' });
    setDialog({ mode: 'create', pool: null });
  };

  const openEdit = pool => {
    setForm({ id: pool.id, name: pool.name, proxies: (pool.proxies || []).join('\n') });
    setDialog({ mode: 'edit', pool });
  };

  const savePool = async () => {
    setSaving(true);
    try {
      const proxies = form.proxies.split('\n').map(s => s.trim()).filter(Boolean);
      if (dialog?.mode === 'create' && !form.id.trim()) throw new Error('请填写池 ID');
      const path = dialog?.mode === 'create' ? API : `${API}/${encodeURIComponent(form.id.trim())}`;
      const res = await fetch(path, {
        method: dialog?.mode === 'create' ? 'POST' : 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ id: form.id.trim(), name: form.name.trim(), proxies }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '保存失败');
      toast.success(dialog?.mode === 'create' ? '代理池已创建' : '代理池已更新');
      setDialog(null);
      await load();
      if (form.id.trim()) await loadState(form.id.trim());
    } catch (e) {
      toast.error(`保存失败：${e.message}`);
    } finally {
      setSaving(false);
    }
  };

  // 向当前选中的代理池直接追加代理列表并保存。
  const appendProxiesToSelected = async (newProxies, successMsg) => {
    if (!selected || !newProxies?.length) return;
    const existing = selected.proxies || [];
    const combined = [...existing, ...newProxies];
    const seen = new Set();
    const clean = [];
    for (const p of combined) {
      const trimmed = (p || '').trim();
      if (!trimmed || seen.has(trimmed)) continue;
      seen.add(trimmed);
      clean.push(trimmed);
    }
    try {
      const res = await fetch(`${API}/${encodeURIComponent(selected.id)}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ id: selected.id, name: selected.name, proxies: clean }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '保存失败');
      toast.success(successMsg || `已追加 ${newProxies.length} 条代理`);
      await load();
      await loadState(selected.id);
    } catch (e) {
      toast.error(`保存失败：${e.message}`);
    }
  };

  // 向当前选中的代理池追加文本中的代理（批量添加）。
  const addBatchToSelected = async () => {
    const add = batchText.split('\n').map(s => s.trim()).filter(Boolean);
    if (!add.length) return;
    await appendProxiesToSelected(add, `已追加 ${add.length} 条代理`);
    setBatchText('');
    setBatchOpen(false);
  };

  // 从文件导入（txt/list/conf/csv，按行读取）。
  const importFile = file => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async () => {
      const text = String(reader.result || '');
      const lines = text.split(/\r?\n/).map(s => s.trim()).filter(Boolean);
      if (!lines.length) {
        toast.warning('文件中没有代理');
        return;
      }
      await appendProxiesToSelected(lines, `已导入 ${lines.length} 条代理`);
    };
    reader.readAsText(file);
  };

  // 订阅链接导入：拉取订阅内容并解析出 socks/http 代理 URL。
  const importSubscription = async () => {
    if (!subscriptionUrl.trim()) return;
    try {
      const res = await fetch(`${API}/subscription`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ url: subscriptionUrl.trim() }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '订阅导入失败');
      const lines = (data.lines || []).filter(Boolean);
      if (!lines.length) {
        toast.warning('订阅未解析到代理');
        return;
      }
      await appendProxiesToSelected(lines, `已导入 ${lines.length} 条代理`);
      setSubscriptionUrl('');
      setSubscriptionOpen(false);
    } catch (e) {
      toast.error(e.message);
    }
  };

  const removePool = async pool => {
    if (!confirmPress(`proxy-pool-delete:${pool.id}`, `删除代理池「${pool.name || pool.id}」`)) return;
    try {
      const res = await fetch(`${API}/${encodeURIComponent(pool.id)}`, { method: 'DELETE', headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '删除失败');
      toast.success('代理池已删除');
      await load();
    } catch (e) {
      toast.error(`删除失败：${e.message}`);
    }
  };

  const probePool = async poolId => {
    setProbing(true);
    try {
      const res = await fetch(`${API}/${encodeURIComponent(poolId)}/probe`, { method: 'POST', headers: getAuthHeaders(), body: '{}' });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '测试失败');
      toast.success(`测试完成：${data.reachable || 0}/${data.probed || 0} 可达`);
      await loadState(poolId);
    } catch (e) {
      toast.error(`测试失败：${e.message}`);
    } finally {
      setProbing(false);
    }
  };

  const unbanPool = async poolId => {
    setUnbanning(true);
    try {
      const res = await fetch(`${API}/${encodeURIComponent(poolId)}/unban`, { method: 'POST', headers: getAuthHeaders(), body: '{}' });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '解封失败');
      toast.success('已解封全部出口');
      await loadState(poolId);
    } catch (e) {
      toast.error(`解封失败：${e.message}`);
    } finally {
      setUnbanning(false);
    }
  };

  if (loading) {
    return (
      <div className="flex h-full min-w-0 items-center justify-center">
        <Loader size="lg" />
      </div>
    );
  }

  const selected = pools?.find(p => p.id === selectedId) || null;
  const selectedState = selected ? (states[selected.id] || []) : [];
  const blockedCount = selectedState.filter(st => st.cooldownUntil || st.rateLimitedUntil || st.sunkUntil).length;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 cq-sm:gap-4">
      <div className="flex items-center justify-end">
        <Button size="sm" variant="primary" onClick={openCreate}>
          <Plus className="h-3.5 w-3.5" /> 新建代理池
        </Button>
      </div>

      <div className="flex min-w-0 flex-col gap-4">
        {/* 池列表 */}
        <LayerCard className="min-w-0 p-0 shadow-none">
          {pools?.length ? (
            <Table layout="fixed" className="w-full text-xs">
              <Table.Header variant="compact">
                <Table.Row className="h-8">
                  <Table.Head className="!px-2.5 !py-1.5">代理池</Table.Head>
                  <Table.Head className="!w-16 !px-2 !py-1.5 text-center">出口</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {pools.map(p => {
                  const st = states[p.id] || [];
                  const blocked = st.filter(x => x.cooldownUntil || x.rateLimitedUntil || x.sunkUntil).length;
                  return (
                    <Table.Row
                      key={p.id}
                      variant={p.id === selectedId ? 'selected' : 'default'}
                      className="h-10 cursor-pointer"
                      onClick={() => setSelectedId(p.id)}
                    >
                      <Table.Cell className="!px-2.5 !py-1.5">
                        <div className="min-w-0">
                          <div className="truncate font-semibold leading-5 text-kumo-strong">{p.name || p.id}</div>
                          <div className="truncate font-mono text-[10px] leading-4 text-kumo-subtle">{p.id}</div>
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <Badge variant={blocked > 0 ? 'warning' : 'neutral'} title={`${blocked} 个禁用/冷却`}>
                          {p.proxies?.length || 0}
                        </Badge>
                      </Table.Cell>
                    </Table.Row>
                  );
                })}
              </Table.Body>
            </Table>
          ) : (
            <div className="p-4">
              <EmptyState title="暂无代理池" description="新建一个可复用的出口代理池。" />
            </div>
          )}
        </LayerCard>

        {/* 选中池详情 */}
        <div className="flex min-w-0 flex-col gap-4">
          {selected ? (
            <SectionCard
              title={selected.name || selected.id}
              bodyPadding="none"
              actions={
                <>
                  {blockedCount > 0 && (
                    <Button size="sm" variant="secondary" disabled={unbanning} onClick={() => unbanPool(selected.id)}>
                      <RefreshCw className={`h-3.5 w-3.5 ${unbanning ? 'animate-spin' : ''}`} /> 一键解封（{blockedCount}）
                    </Button>
                  )}
                  <Button size="sm" variant="secondary" disabled={probing} onClick={() => probePool(selected.id)}>
                    {probing ? '测试中...' : '批量测试'}
                  </Button>
                  <Button size="sm" variant="secondary" onClick={() => openEdit(selected)}>
                    编辑
                  </Button>
                  <Button
                    size="sm"
                    variant={isArmed(`proxy-pool-delete:${selected.id}`) ? 'destructive' : 'secondary-destructive'}
                    title={isArmed(`proxy-pool-delete:${selected.id}`) ? '再次点击确认删除' : `删除 ${selected.name || selected.id}`}
                    onClick={() => removePool(selected)}
                  >
                    <Trash className="h-3.5 w-3.5" /> {isArmed(`proxy-pool-delete:${selected.id}`) ? '再次确认' : '删除'}
                  </Button>
                </>
              }
            >
              <FieldRow title="代理列表" description={`${selected.proxies?.length || 0} 个出口，每行一个，支持 http/https/socks5`}>
                <div className="flex min-w-0 items-center gap-1.5">
                  <Button size="sm" variant="outline" onClick={() => setBatchOpen(o => !o)}>
                    批量添加
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => fileInputRef.current?.click()}>
                    <Upload className="h-3 w-3" /> 导入文件
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => setSubscriptionOpen(o => !o)}>
                    <Globe className="h-3 w-3" /> 订阅导入
                  </Button>
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept=".txt,.list,.conf,.csv,text/plain"
                    className="hidden"
                    onChange={e => {
                      importFile(e.target.files?.[0]);
                      e.target.value = '';
                    }}
                  />
                </div>
              </FieldRow>

              {batchOpen && (
                <div className="space-y-2 border-t border-kumo-line px-4 py-3">
                  <Textarea
                    size="sm"
                    rows={4}
                    className="w-full font-mono text-[0.85em]"
                    value={batchText}
                    onChange={e => setBatchText(e.target.value)}
                    placeholder={'每行一个，如：\nsocks5://user:pass@1.2.3.4:1080\nhttp://5.6.7.8:8080'}
                    spellCheck={false}
                  />
                  <div className="flex items-center justify-end gap-2">
                    <Button size="sm" variant="outline" onClick={() => { setBatchText(''); setBatchOpen(false); }}>取消</Button>
                    <Button size="sm" variant="primary" onClick={addBatchToSelected}>追加到列表</Button>
                  </div>
                </div>
              )}

              {subscriptionOpen && (
                <div className="space-y-2 border-t border-kumo-line px-4 py-3">
                  <Input
                    size="sm"
                    type="url"
                    value={subscriptionUrl}
                    aria-label="订阅 URL"
                    onChange={e => setSubscriptionUrl(e.target.value)}
                    placeholder="https://example.com/sub?token=xxx"
                    className="w-full font-mono text-[0.85em]"
                  />
                  <div className="flex items-center justify-end gap-2">
                    <Button size="sm" variant="outline" onClick={() => { setSubscriptionUrl(''); setSubscriptionOpen(false); }}>取消</Button>
                    <Button size="sm" variant="primary" onClick={importSubscription}>导入</Button>
                  </div>
                </div>
              )}

              <div className="max-h-80 overflow-y-auto border-t border-kumo-line px-4 py-2 scrollbar-thin">
                {(selected.proxies || []).map((p, i) => {
                  const st = selectedState[i];
                  const exitIP = st?.lastExitIP;
                  const ms = st?.lastProbeMs;
                  const reachable = st?.reachable;
                  const disabled = st && (st.cooldownUntil || st.rateLimitedUntil || st.sunkUntil);
                  return (
                    <div key={i} className="flex items-center justify-between gap-2 border-b border-kumo-line/50 py-1.5 last:border-b-0">
                      <span className="min-w-0 flex-1 truncate font-mono text-[0.8em] text-kumo-strong" title={p}>{p}</span>
                      <span className="flex shrink-0 items-center gap-2">
                        {disabled && <Badge variant="warning" className="!text-[0.8em]">禁用</Badge>}
                        {reachable !== undefined && (
                          <Badge variant={reachable ? 'success' : 'danger'} className="!text-[0.8em]">
                            {reachable ? '可达' : '失败'}
                          </Badge>
                        )}
                        {exitIP && (
                          <span className="truncate font-mono text-[10px] text-kumo-subtle" title={`出口 IP ${exitIP}`}>
                            {exitIP}
                          </span>
                        )}
                        {ms > 0 && <span className="font-mono text-[10px] text-kumo-subtle">{ms}ms</span>}
                      </span>
                    </div>
                  );
                })}
              </div>
            </SectionCard>
          ) : (
            <SectionCard title="代理池" description="选择左侧代理池查看详情。">
              <div className="p-4">
                <span className="text-xs text-kumo-subtle">被端点或插件引用后，出口按池轮换并共享冷却/429 禁用状态。</span>
              </div>
            </SectionCard>
          )}
        </div>
      </div>

      <Dialog.Root open={!!dialog} onOpenChange={open => !open && setDialog(null)}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(46rem,calc(100vw-2rem))] !max-w-[min(46rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
              {dialog?.mode === 'create' ? '新建代理池' : '编辑代理池'}
            </Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              每行一个代理地址，支持 socks5://、http(s)://；被引用后出口按池轮换。
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="space-y-4">
              <Input
                size="sm"
                label="池 ID"
                type="text"
                value={form.id}
                onChange={e => setForm(f => ({ ...f, id: e.target.value }))}
                disabled={dialog?.mode === 'edit'}
                placeholder="如：gemini-pool、worker-pool"
                className="w-full text-kumo-strong font-mono text-[0.9em]"
              />
              <Input
                size="sm"
                label="名称"
                type="text"
                value={form.name}
                onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
                placeholder="可读名称"
                className="w-full text-kumo-strong text-[0.9em]"
              />
              <div>
                <div className="mb-1.5 text-sm font-semibold text-kumo-strong">代理列表</div>
                <Textarea
                  rows={12}
                  className="w-full font-mono text-xs"
                  value={form.proxies}
                  onChange={e => setForm(f => ({ ...f, proxies: e.target.value }))}
                  placeholder={'每行一个，如：\nsocks5://user:pass@1.2.3.4:1080\nhttp://5.6.7.8:8080'}
                  spellCheck={false}
                />
              </div>
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
            <Button size="sm" variant="primary" disabled={saving} onClick={savePool}>
              {saving ? '保存中...' : '保存'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}
