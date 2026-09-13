import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Label } from '@cloudflare/kumo/components/label';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Badge, LayerCard } from '@cloudflare/kumo';
import { sectionCardHeaderClass } from '../../components/ui/AppPrimitives.jsx';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Download, Save, Star, Trash, X } from '../../components/Icons.jsx';
import { nodeTypeBadgeVariant, syncNodeForm } from './utils.js';
import { NodeFlag, TemplateCodeEditor, TrafficSizeInput } from './components.jsx';

export function PlanDialog({ open, onOpenChange, editingPlanId, planForm, setPlanForm, nodes, allVisiblePlanNodesSelected, visiblePlanNodeIDs, visiblePlanNodes, planNodeTypeItems, planNodeSourceFilter, setPlanNodeSourceFilter, planNodeTypeFilter, setPlanNodeTypeFilter, saving, onSave }) {
  return (
      <Dialog.Root open={open} onOpenChange={onOpenChange}>
        <Dialog size="xl" className="@container flex max-h-[min(calc(100dvh-2rem),48rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-sm:!w-[min(72rem,calc(100vw-3rem))] cq-sm:!max-w-[min(72rem,calc(100vw-3rem))]">
          <div className="border-b border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4"><Dialog.Title>{editingPlanId ? '编辑套餐' : '新建套餐'}</Dialog.Title></div>
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-3 scrollbar-thin cq-sm:p-5">
            <div className="grid gap-3 cq-sm:grid-cols-2"><Input size="sm" label="套餐名称" value={planForm.name} onChange={(e) => setPlanForm((prev) => ({ ...prev, name: e.target.value }))} /><Input size="sm" label="备注" value={planForm.remark} onChange={(e) => setPlanForm((prev) => ({ ...prev, remark: e.target.value }))} /></div>
            <div className="grid items-end gap-3 cq-md:grid-cols-[minmax(16rem,1.2fr)_minmax(12rem,.8fr)_minmax(10rem,.7fr)]"><TrafficSizeInput label="订阅额度（仅托管节点，0 不限）" value={planForm.total_bytes} onChange={(value) => setPlanForm((prev) => ({ ...prev, total_bytes: value }))} /><Select alignItemWithTrigger size="sm" label="重置周期" value={planForm.cycle_type} onValueChange={(value) => setPlanForm((prev) => ({ ...prev, cycle_type: String(value) }))} items={[{ value: 'monthly', label: '每月重置' }, { value: 'none', label: '不重置' }]} /><Input size="sm" label="每月重置日" type="number" min="1" max="31" value={planForm.cycle_day} disabled={planForm.cycle_type !== 'monthly'} onChange={(e) => setPlanForm((prev) => ({ ...prev, cycle_day: Number(e.target.value) || 1 }))} /></div>
			{planForm.total_bytes > 0 && ((planForm.selection_mode === 'all' && planForm.include_external_nodes) || (planForm.selection_mode === 'explicit' && planForm.node_ids.some((id) => nodes.some((node) => node.id === id)))) && <div className="rounded-md border border-kumo-warning/30 bg-kumo-warning/10 px-3 py-2 text-xs text-kumo-warning">外部节点不受 Agent 管理，额度仅约束内部节点。</div>}
            <div className="grid items-end gap-3 cq-md:grid-cols-[minmax(18rem,1fr)_auto]"><Input size="sm" label="订阅请求限制（次/分钟）" type="number" min="1" value={planForm.rate_limit_per_minute} onChange={(e) => setPlanForm((prev) => ({ ...prev, rate_limit_per_minute: Number(e.target.value) || 30 }))} /><div className="flex min-h-8 items-center"><Switch size="sm" label="启用请求限制" checked={planForm.rate_limit_enabled} onCheckedChange={(checked) => setPlanForm((prev) => ({ ...prev, rate_limit_enabled: checked }))} /></div></div>
            <div className="border-t border-kumo-line pt-4">
              <div className="mb-3 grid items-end gap-3 cq-sm:grid-cols-[14rem_1fr]">
                <Select alignItemWithTrigger size="sm" label="节点范围" value={planForm.selection_mode} onValueChange={(value) => setPlanForm((prev) => ({ ...prev, selection_mode: String(value), node_ids: String(value) === 'all' ? [] : prev.node_ids }))} items={[{ value: 'explicit', label: '指定节点' }, { value: 'all', label: '全部当前及未来节点' }]} />
                {planForm.selection_mode === 'all' && <div className="flex min-h-8 flex-wrap items-center gap-x-6 gap-y-2"><Switch size="sm" label="包含内部节点" checked={planForm.include_internal_nodes} onCheckedChange={(checked) => setPlanForm((prev) => ({ ...prev, include_internal_nodes: checked }))} /><Switch size="sm" label="包含外部节点" checked={planForm.include_external_nodes} onCheckedChange={(checked) => setPlanForm((prev) => ({ ...prev, include_external_nodes: checked }))} /></div>}
              </div>
              {planForm.selection_mode === 'explicit' && <>
				<div className="mb-2 flex flex-wrap items-end justify-between gap-2"><div className="flex flex-wrap items-end gap-2"><Label className="text-xs font-semibold text-kumo-subtle">套餐节点</Label><Select alignItemWithTrigger size="sm" aria-label="节点类型筛选" value={planNodeTypeFilter} onValueChange={(value) => setPlanNodeTypeFilter(String(value))} items={planNodeTypeItems} className="w-36" /><Select alignItemWithTrigger size="sm" aria-label="节点来源筛选" value={planNodeSourceFilter} onValueChange={(value) => setPlanNodeSourceFilter(String(value))} items={[{ value: 'all', label: '全部来源' }, { value: 'internal', label: 'Agent 节点' }, { value: 'external', label: '外部节点' }]} className="w-36" /></div><div className="flex items-center gap-2"><Badge variant="neutral">已选 {planForm.node_ids.length}</Badge><Button size="sm" variant="secondary" disabled={visiblePlanNodeIDs.length === 0} onClick={() => setPlanForm((prev) => ({ ...prev, node_ids: allVisiblePlanNodesSelected ? prev.node_ids.filter((id) => !visiblePlanNodeIDs.includes(id)) : [...new Set([...prev.node_ids, ...visiblePlanNodeIDs])] }))}>{allVisiblePlanNodesSelected ? '取消当前全部' : '全选当前结果'}</Button></div></div>
				<div className="max-h-72 overflow-auto rounded-md border border-kumo-line p-2 scrollbar-thin"><div className="grid gap-1 cq-sm:grid-cols-2 cq-lg:grid-cols-3">{visiblePlanNodes.map((node) => <label key={`${node.source_group}-${node.id}`} className="flex min-w-0 items-center gap-2 rounded px-2 py-1.5 hover:bg-kumo-recessed"><Checkbox aria-label={`选择套餐节点 ${node.name}`} checked={planForm.node_ids.includes(node.id)} onCheckedChange={(checked) => setPlanForm((prev) => ({ ...prev, node_ids: checked ? [...new Set([...prev.node_ids, node.id])] : prev.node_ids.filter((id) => id !== node.id) }))} /><span className="min-w-0 flex-1 truncate text-xs font-semibold">{node.name}</span><Badge variant="neutral">{node.source_group === 'internal' ? 'Agent' : '外部'}</Badge><Badge variant={nodeTypeBadgeVariant(node.display_type)}>{node.display_type || '-'}</Badge></label>)}{visiblePlanNodes.length === 0 && <div className="p-5 text-center text-xs text-kumo-subtle cq-sm:col-span-2 cq-lg:col-span-3">没有符合类型与来源条件的节点</div>}</div></div>
              </>}
            </div>
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-3 py-3 cq-sm:px-5"><Dialog.Close render={(props) => <Button size="sm" variant="secondary" {...props}>取消</Button>} /><Button size="sm" variant="primary" loading={saving} onClick={onSave}><Save className="h-3.5 w-3.5" />保存套餐</Button></div>
        </Dialog>
      </Dialog.Root>
  );
}

export function InternalNodeDialog({ open, onOpenChange, editingInternalNodeId, internalNodeForm, setInternalNodeForm, selectedInternalHosts, setSelectedInternalHosts, runtimeReadyServers, servers, preferredAddresses, saving, onSave, onCreate }) {
  return (
      <Dialog.Root open={open} onOpenChange={onOpenChange}>
        <Dialog size="xl" className="@container !w-[min(58rem,calc(100vw-1rem))] !max-w-[min(58rem,calc(100vw-1rem))] overflow-hidden p-0">
          <div className="border-b border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4"><Dialog.Title>{editingInternalNodeId ? '编辑内部节点' : '生成内部节点'}</Dialog.Title></div>
          <div className="grid gap-3 p-3 cq-sm:grid-cols-2 cq-sm:p-5">
            {!editingInternalNodeId && <div className="cq-sm:col-span-2">
				<div className="flex items-center justify-between gap-2"><Label className="text-xs font-semibold text-kumo-subtle">已安装代理程序的实例</Label><Badge variant="neutral">已选 {selectedInternalHosts.size} / {runtimeReadyServers.length}</Badge></div>
              <div className="mt-1.5 max-h-44 overflow-auto rounded-md border border-kumo-line bg-kumo-recessed/20 p-1.5 scrollbar-thin">
                <div className="grid gap-1 cq-sm:grid-cols-2">
                  {runtimeReadyServers.map((server) => {
                    const checked = selectedInternalHosts.has(server.id);
					return <label key={server.id} className="flex min-w-0 items-center gap-2 rounded px-2 py-1.5 hover:bg-kumo-base/60"><Checkbox checked={checked} disabled={server.status !== 'online'} onCheckedChange={(value) => { const next = new Set(selectedInternalHosts); if (value) next.add(server.id); else next.delete(server.id); setSelectedInternalHosts(next); const first = [...next][0] || ''; setInternalNodeForm((prev) => ({ ...prev, server_id: first, public_host: servers.find((item) => item.id === first)?.host || '' })); }} aria-label={`选择 ${server.name}`} /><span className="min-w-0 flex-1 truncate text-xs font-semibold text-kumo-strong">{server.name}</span><Badge variant={server.status === 'online' ? 'success' : 'neutral'} appearance="dot">{server.status === 'online' ? '在线' : '离线'}</Badge></label>;
                  })}
                  {runtimeReadyServers.length === 0 && <div className="p-3 text-center text-xs text-kumo-subtle cq-sm:col-span-2">暂无已安装 sing-box 的实例</div>}
                </div>
              </div>
            </div>}
            {!editingInternalNodeId && <Select alignItemWithTrigger size="sm" label="节点协议" value={internalNodeForm.protocol} onValueChange={(value) => setInternalNodeForm((prev) => ({ ...prev, protocol: String(value), access_mode: String(value) === 'socks' || String(value) === 'http' ? 'direct' : prev.access_mode }))} items={[{ value: 'vless-reality', label: 'VLESS REALITY' }, { value: 'hysteria2', label: 'Hysteria2' }, { value: 'socks', label: 'SOCKS5' }, { value: 'http', label: 'HTTP' }]} />}
            <Input size="sm" label={editingInternalNodeId ? '节点名称' : selectedInternalHosts.size > 1 ? '节点名称前缀（可选）' : '节点名称（可选）'} placeholder="留空按实例名生成" value={internalNodeForm.name} onChange={(event) => setInternalNodeForm((prev) => ({ ...prev, name: event.target.value }))} />
            {!editingInternalNodeId && internalNodeForm.protocol === 'vless-reality' && <Input size="sm" label="REALITY 握手站点" placeholder="默认 www.cloudflare.com" value={internalNodeForm.server_name} onChange={(event) => setInternalNodeForm((prev) => ({ ...prev, server_name: event.target.value }))} />}
            {!editingInternalNodeId && internalNodeForm.protocol === 'hysteria2' && <div className="flex min-h-8 items-center rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 text-xs text-kumo-subtle">TLS 信息自动生成。</div>}
            {!editingInternalNodeId && (internalNodeForm.protocol === 'socks' || internalNodeForm.protocol === 'http') && <div className="flex min-h-8 items-center rounded-md border border-kumo-info/25 bg-kumo-info/10 px-3 text-xs text-kumo-subtle">SOCKS/HTTP 仅直连，无 TLS 加密。</div>}
            {!editingInternalNodeId && <Select alignItemWithTrigger size="sm" label="接入方式" value={internalNodeForm.access_mode || 'direct'} disabled={internalNodeForm.protocol === 'socks' || internalNodeForm.protocol === 'http'} onValueChange={(value) => setInternalNodeForm((prev) => ({ ...prev, access_mode: String(value) }))} items={[{ value: 'direct', label: '直连节点' }, { value: 'cloudflare_tunnel', label: 'Cloudflare Tunnel（VLESS WS）' }]} />}
            {internalNodeForm.access_mode === 'cloudflare_tunnel' && <Select alignItemWithTrigger size="sm" label="优选地址" value={internalNodeForm.preferred_address_id || ''} onValueChange={(value) => setInternalNodeForm((prev) => ({ ...prev, preferred_address_id: String(value) }))} items={[{ value: '', label: '继承默认地址' }, ...preferredAddresses.map((item) => ({ value: item.id, label: `${item.name} · ${item.address}` }))]} />}
            <div className="flex min-h-8 items-center rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-2"><Switch size="sm" label="稳定节点" controlFirst={false} checked={!!internalNodeForm.stable} onCheckedChange={(checked) => setInternalNodeForm((prev) => ({ ...prev, stable: checked }))} /></div>
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4"><Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button><Button size="sm" variant="primary" loading={saving} onClick={editingInternalNodeId ? onSave : onCreate}>{editingInternalNodeId ? '保存' : '生成节点'}</Button></div>
        </Dialog>
      </Dialog.Root>
  );
}

export function TunnelDialog({ open, onOpenChange, tunnelForm, setTunnelForm, cloudflareAccounts, cloudflareZones, tunnelTargetServer, onDeploy }) {
  return (
		<Dialog.Root open={open} onOpenChange={onOpenChange}>
			<Dialog size="lg" className="@container w-[calc(100vw-1rem)] max-w-2xl p-0">
				<div className="border-b border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4"><Dialog.Title>部署 Cloudflare Named Tunnel</Dialog.Title></div>
				<div className="grid gap-3 p-3 cq-sm:grid-cols-2 cq-sm:p-5"><Select alignItemWithTrigger size="sm" label="Cloudflare 账号" value={tunnelForm.account_id} onValueChange={(value) => setTunnelForm((prev) => ({ ...prev, account_id: String(value), zone_id: '', hostname: '' }))} items={cloudflareAccounts.map((item) => ({ value: item.id, label: item.name || item.email || item.id }))} /><Select alignItemWithTrigger size="sm" label="DNS Zone" value={tunnelForm.zone_id} onValueChange={(value) => setTunnelForm((prev) => ({ ...prev, zone_id: String(value) }))} items={cloudflareZones.map((item) => ({ value: item.id, label: item.name || item.id }))} /><Input size="sm" className="cq-sm:col-span-2" label="自动生成的 Tunnel 域名" value={tunnelForm.hostname || '选择 DNS Zone 后自动生成'} readOnly /></div>
				<div className="flex justify-end gap-2 border-t border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4"><Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button><Button size="sm" variant="primary" onClick={() => onDeploy()} disabled={!tunnelTargetServer || !tunnelForm.hostname}>开始部署</Button></div>
			</Dialog>
		</Dialog.Root>
  );
}

export function PreferredAddressDialog({ open, onOpenChange, preferredAddresses, preferredForm, setPreferredForm, onSave, onSetDefault, onDelete }) {
  return (
		<Dialog.Root open={open} onOpenChange={onOpenChange}>
			<Dialog size="lg" className="@container !w-[min(56rem,calc(100vw-1rem))] !max-w-[min(56rem,calc(100vw-1rem))] overflow-hidden p-0">
				<div className="flex min-h-12 items-center justify-between gap-3 border-b border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-3.5">
					<Dialog.Title>优选地址</Dialog.Title>
					<div className="flex shrink-0 items-center gap-2">
						<Badge variant="neutral">{preferredAddresses.length} 个地址</Badge>
						<Dialog.Close
							aria-label="关闭"
							render={(props) => (
								<Button
									{...props}
									type="button"
									variant="secondary"
									shape="square"
									size="sm"
									icon={<X className="h-3.5 w-3.5" />}
									aria-label="关闭"
								/>
							)}
						/>
					</div>
				</div>
				<div className="grid min-h-0 min-w-0 cq-lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
					<div className="flex min-w-0 flex-col gap-3 border-b border-kumo-line p-3 cq-sm:p-4 cq-lg:border-b-0 cq-lg:border-r">
						<div className="text-xs font-semibold text-kumo-strong">新地址</div>
						<Input size="sm" label="名称" value={preferredForm.name} onChange={(event) => setPreferredForm((prev) => ({ ...prev, name: event.target.value }))} />
						<Input size="sm" label="域名或 IP" placeholder="saas.sin.fan" value={preferredForm.address} onChange={(event) => setPreferredForm((prev) => ({ ...prev, address: event.target.value }))} />
						<Input size="sm" label="端口" type="number" value={preferredForm.port} onChange={(event) => setPreferredForm((prev) => ({ ...prev, port: Number(event.target.value) || 443 }))} />
						<div className="flex min-w-0 items-center justify-between gap-3">
							<Label className="min-w-0 truncate text-xs text-kumo-subtle">保存后设为全局默认</Label>
							<Switch size="sm" aria-label="保存后设为全局默认" checked={!!preferredForm.is_default} onCheckedChange={(checked) => setPreferredForm((prev) => ({ ...prev, is_default: checked }))} />
						</div>
						<Button size="sm" variant="primary" className="self-end" onClick={onSave}><Save className="h-3.5 w-3.5" />添加地址</Button>
					</div>
					<div className="flex min-h-0 min-w-0 flex-col">
						<div className="px-3 pt-3 cq-sm:px-4"><div className="text-xs font-semibold text-kumo-strong">地址列表</div></div>
						<div className="max-h-64 min-h-0 overflow-y-auto p-2 scrollbar-thin">
							{preferredAddresses.length === 0 ? (
								<div className="p-6 text-center text-xs text-kumo-subtle">暂无优选地址</div>
							) : [...preferredAddresses].sort((a, b) => (a.sort_order || 0) - (b.sort_order || 0) || String(a.created_at || '').localeCompare(String(b.created_at || ''))).map((item) => (
<div key={item.id} className={`flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5 ${item.is_default ? 'bg-kumo-recessed/60' : 'hover:bg-kumo-recessed/40'}`}>
									<div className="min-w-0 flex-1">
										<div className="flex min-w-0 items-center gap-1.5">
											<span className={`truncate text-xs font-semibold ${item.enabled === false ? 'text-kumo-subtle' : 'text-kumo-strong'}`}>{item.name}</span>
										</div>
										<div className="truncate font-mono text-[11px] text-kumo-subtle">{item.address}:{item.port}</div>
									</div>
									<div className="flex shrink-0 items-center gap-1">
										{!item.is_default && <Button size="sm" variant="secondary" onClick={() => onSetDefault(item)} icon={<Star className="h-3.5 w-3.5" />}>默认</Button>}
										<Button size="sm" shape="square" variant="secondary-destructive" onClick={() => onDelete(item)} icon={<Trash className="h-3.5 w-3.5" />} aria-label={`删除 ${item.name}`} />
									</div>
								</div>
							))}
						</div>
					</div>
				</div>
				<div className="flex justify-end gap-2 border-t border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4">
					<Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>关闭</Button>
				</div>
			</Dialog>
		</Dialog.Root>
  );
}

export function SubscriptionDialog({ open, onOpenChange, editingSubscriptionId, subscriptionForm, setSubscriptionForm, planItems, saving, onSave }) {
  return (
      <Dialog.Root open={open} onOpenChange={onOpenChange}>
        <Dialog size="lg" className="@container flex max-h-[min(calc(100dvh-2rem),42rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-sm:w-[min(calc(100vw-3rem),64rem)]">
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b border-kumo-line bg-kumo-recessed/20 px-3 py-3 cq-sm:px-5 cq-sm:py-3.5">
              <div className="min-w-0">
                <Dialog.Title className="min-w-0 truncate text-base font-semibold text-kumo-strong">
                  {editingSubscriptionId ? '编辑对外订阅' : '创建对外订阅'}
                </Dialog.Title>
              </div>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square"
                    size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    aria-label="关闭"
                    className="shrink-0"
                  />
                )}
              />
            </div>

            <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden px-3 py-3 text-xs scrollbar-thin cq-sm:px-5 cq-sm:py-4">
              <div className="space-y-4">
                <section className="space-y-3">
                  <div className="text-[11px] font-semibold uppercase text-kumo-subtle">基础信息</div>
                  <div className="grid min-w-0 gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(16rem,100%),1fr))]">
                    <div className="min-w-0">
                      <Input size="sm" label="名称" value={subscriptionForm.name} onChange={(e) => setSubscriptionForm((prev) => ({ ...prev, name: e.target.value }))} className="w-full min-w-0" />
                    </div>
                    <div className="min-w-0"><Select alignItemWithTrigger size="sm" label="套餐" value={subscriptionForm.plan_id || ''} onValueChange={(value) => setSubscriptionForm((prev) => ({ ...prev, plan_id: String(value) }))} items={planItems} className="w-full min-w-0" /></div>
                  </div>
                </section>

              </div>
            </div>

            <div className="flex shrink-0 items-center justify-between gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-3 py-3 cq-sm:px-5 cq-sm:justify-end">
              <Dialog.Close render={(props) => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
              <Button size="sm" variant="primary" loading={saving} onClick={onSave}><Save className="h-3.5 w-3.5" />保存</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
  );
}

export function NodeDialog({ open, onOpenChange, nodeForm, setNodeForm, saving, onSave }) {
  return (
      <Dialog.Root open={open} onOpenChange={onOpenChange}>
        <Dialog size="lg" className="@container flex max-h-[min(calc(100dvh-2rem),44rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-sm:w-[min(calc(100vw-3rem),72rem)]">
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b border-kumo-line bg-kumo-recessed/20 px-3 py-3 cq-sm:px-5 cq-sm:py-3.5">
              <div className="min-w-0">
                <Dialog.Title className="min-w-0 truncate text-base font-semibold text-kumo-strong">编辑节点</Dialog.Title>
              </div>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square"
                    size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    aria-label="关闭"
                    className="shrink-0"
                  />
                )}
              />
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-3 py-3 scrollbar-thin cq-sm:px-5 cq-sm:py-4">
              <div className="space-y-4">
                <section className="min-w-0 space-y-3">
                  <div className="text-[11px] font-semibold uppercase text-kumo-subtle">连接信息</div>
                  <div className="grid min-w-0 gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(14rem,100%),1fr))]">
                    <Input size="sm" label="节点名称" value={nodeForm.name} onChange={(e) => setNodeForm((prev) => syncNodeForm(prev, 'name', e.target.value))} className="w-full min-w-0" />
                    <Input size="sm" label="协议类型" value={nodeForm.type} onChange={(e) => setNodeForm((prev) => syncNodeForm(prev, 'type', e.target.value))} className="w-full min-w-0" />
                    <Input size="sm" label="服务器地址" value={nodeForm.server} onChange={(e) => setNodeForm((prev) => syncNodeForm(prev, 'server', e.target.value))} className="w-full min-w-0" />
                    <Input size="sm" label="端口" type="number" value={nodeForm.port || 0} onChange={(e) => setNodeForm((prev) => syncNodeForm(prev, 'port', Number(e.target.value) || 0))} className="w-full min-w-0" />
                    <Input size="sm" label="国家 / 地区代码" value={nodeForm.country_code || ''} onChange={(e) => setNodeForm((prev) => ({ ...prev, country_code: e.target.value }))} className="w-full min-w-0" />
                    <Input size="sm" label="位置" value={nodeForm.location || ''} onChange={(e) => setNodeForm((prev) => ({ ...prev, location: e.target.value }))} className="w-full min-w-0" />
                    <Input size="sm" label="标签" value={nodeForm.tags || ''} onChange={(e) => setNodeForm((prev) => ({ ...prev, tags: e.target.value }))} className="w-full min-w-0" />
                  </div>
                </section>

                <section className="min-w-0 space-y-3 border-t border-kumo-line pt-4">
                  <div className="text-[11px] font-semibold uppercase text-kumo-subtle">外部节点属性</div>
                  <div className="grid min-w-0 items-end gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(14rem,100%),1fr))]">
                    <Input size="sm" label="排序" type="number" value={nodeForm.sort_order || 0} onChange={(e) => setNodeForm((prev) => ({ ...prev, sort_order: Number(e.target.value) || 0 }))} className="w-full min-w-0" />
                    <div className="grid min-h-8 min-w-0 gap-3 rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-2 cq-sm:grid-cols-2">
                      <Switch size="sm" label="启用节点" controlFirst={false} checked={!!nodeForm.enabled} onCheckedChange={(checked) => setNodeForm((prev) => ({ ...prev, enabled: checked }))} />
                      <Switch size="sm" label="稳定节点" controlFirst={false} checked={!!nodeForm.stable} onCheckedChange={(checked) => setNodeForm((prev) => ({ ...prev, stable: checked }))} />
                    </div>
                  </div>
                </section>

                <section className="min-w-0 space-y-3 border-t border-kumo-line pt-4">
                  <div className="text-[11px] font-semibold uppercase text-kumo-subtle">原始配置</div>
                  <div className="grid min-w-0 gap-3">
                    <CodeEditor label="原始节点链接" language="text" minHeight="9rem" value={nodeForm.raw || ''} onChange={(raw) => setNodeForm((prev) => syncNodeForm(prev, 'raw', raw))} />
                    <CodeEditor label="节点配置 JSON" language="json" minHeight="9rem" value={nodeForm.config_json || ''} onChange={(config_json) => setNodeForm((prev) => syncNodeForm(prev, 'config_json', config_json))} />
                  </div>
                </section>
              </div>
            </div>

            <div className="flex shrink-0 items-center justify-between gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-3 py-3 cq-sm:px-5 cq-sm:justify-end">
              <Dialog.Close render={(props) => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
              <Button size="sm" variant="primary" loading={saving} onClick={onSave}><Save className="h-3.5 w-3.5" />保存节点</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
  );
}

export function ImportDialog({ open, onOpenChange, importSourceURL, setImportSourceURL, importText, setImportText, importPreview, onPreview, onCommit }) {
  return (
      <Dialog.Root open={open} onOpenChange={onOpenChange}>
        <Dialog size="xl" className="@container flex !h-auto max-h-[min(calc(100dvh-1rem),42rem)] !w-[min(72rem,calc(100vw-1rem))] !max-w-[min(72rem,calc(100vw-1rem))] flex-col overflow-hidden p-0">
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b border-kumo-line bg-kumo-recessed/20 px-3 py-3 cq-sm:px-5 cq-sm:py-3.5">
              <div className="min-w-0">
                <Dialog.Title className="min-w-0 truncate text-base font-semibold text-kumo-strong">导入节点</Dialog.Title>
              </div>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square"
                    size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    aria-label="关闭"
                    className="shrink-0"
                  />
                )}
              />
            </div>

            <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden px-3 py-3 scrollbar-thin cq-sm:px-5 cq-sm:py-4">
              <div className="grid min-w-0 items-start gap-4 cq-lg:grid-cols-[minmax(0,1.35fr)_minmax(18rem,0.65fr)]">
                <div className="min-w-0 space-y-3">
                  <Input size="sm" label="订阅 URL" placeholder="https://example.com/sub.yaml" value={importSourceURL} onChange={(e) => setImportSourceURL(e.target.value)} />
                  <CodeEditor className="h-[18rem] min-w-0" label="节点链接 / YAML / Base64 内容" language="yaml" minHeight="18rem" placeholder="可粘贴节点链接、Base64 订阅，或 Clash/Mihomo YAML 的 proxies。" value={importText} onChange={setImportText} />
                </div>
                <LayerCard className="flex h-[18rem] min-h-0 min-w-0 flex-col overflow-hidden border border-kumo-line bg-kumo-elevated p-0 shadow-none cq-lg:mt-[3.5rem]">
                  <LayerCard.Secondary className={sectionCardHeaderClass}>
                    <div className="min-w-0 truncate text-sm font-semibold text-kumo-strong">解析预览</div>
					<Badge variant="neutral">{importPreview.length} 个节点</Badge>
                  </LayerCard.Secondary>
                  <LayerCard.Primary className="min-h-0 flex-1 overflow-y-auto p-0 scrollbar-thin">
                    <div className="flex min-h-full flex-col divide-y divide-kumo-line">
                      {importPreview.map((node, index) => (
                        <div key={`${node.name}-${index}`} className="grid min-h-14 min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-4 py-2.5 text-xs">
                          <div className="min-w-0">
                            <div className="flex min-w-0 items-center gap-2">
                              <NodeFlag node={node} />
                              <span className="truncate font-semibold text-kumo-strong">{node.name || '未命名节点'}</span>
                            </div>
                            <div className="mt-1 truncate font-mono text-[11px] text-kumo-subtle">{node.server || '-'}:{node.port || '-'}</div>
                          </div>
                          <Badge variant={nodeTypeBadgeVariant(node.type)} className="shrink-0 uppercase">{node.type || '-'}</Badge>
                        </div>
                      ))}
                      {importPreview.length === 0 && (
                        <div className="flex flex-1 items-center justify-center px-6 py-10 text-center text-xs text-kumo-subtle">预览后显示解析出的节点。</div>
                      )}
                    </div>
                  </LayerCard.Primary>
                </LayerCard>
              </div>
            </div>

            <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-3 py-3 cq-sm:px-5 cq-sm:justify-end">
              <Button size="sm" variant="secondary" onClick={onPreview}>预览</Button>
              <div className="flex items-center gap-2">
                <Button size="sm" variant="secondary" onClick={() => onCommit(true)}>
                  <Download className="h-3.5 w-3.5" />
                  覆盖导入
                </Button>
                <Button size="sm" variant="primary" onClick={() => onCommit(false)}>
                  <Download className="h-3.5 w-3.5" />
                  追加导入
                </Button>
              </div>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
  );
}

export function TemplateDialog({ open, onOpenChange, editingTemplateId, templateForm, setTemplateForm, saving, onSave }) {
  return (
      <Dialog.Root open={open} onOpenChange={onOpenChange}>
        <Dialog size="lg" className="@container flex max-h-[min(calc(100dvh-2rem),42rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-sm:w-[min(calc(100vw-3rem),64rem)]">
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b border-kumo-line bg-kumo-recessed/20 px-3 py-3 cq-sm:px-5 cq-sm:py-3.5">
              <div className="min-w-0">
                <Dialog.Title className="min-w-0 truncate text-base font-semibold text-kumo-strong">
                  {editingTemplateId ? '编辑模板' : '创建模板'}
                </Dialog.Title>
              </div>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square"
                    size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    aria-label="关闭"
                    className="shrink-0"
                  />
                )}
              />
            </div>

            <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden px-3 py-3 scrollbar-thin cq-sm:px-5 cq-sm:py-4">
              <div className="grid gap-4">
                <div className="grid gap-3 cq-sm:grid-cols-2">
                  <Input size="sm" label="名称" value={templateForm.name} onChange={(e) => setTemplateForm((prev) => ({ ...prev, name: e.target.value }))} />
                  <Select alignItemWithTrigger size="sm" label="格式" value={templateForm.format} onValueChange={(value) => setTemplateForm((prev) => ({ ...prev, format: String(value) }))} items={[{ value: 'clash', label: 'Mihomo/Clash YAML' }, { value: 'raw', label: 'Raw URI List' }, { value: 'base64', label: 'Base64 URI List' }]} />
                </div>
                <TemplateCodeEditor label="模板内容" value={templateForm.content} format={templateForm.format} onChange={(content) => setTemplateForm((prev) => ({ ...prev, content }))} />
                <Input size="sm" label="描述" value={templateForm.description} onChange={(e) => setTemplateForm((prev) => ({ ...prev, description: e.target.value }))} />
              </div>
            </div>

            <div className="flex shrink-0 items-center justify-between gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-3 py-3 cq-sm:px-5 cq-sm:justify-end">
              <Dialog.Close render={(props) => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
              <Button size="sm" variant="primary" loading={saving} onClick={onSave}><Save className="h-3.5 w-3.5" />保存模板</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
  );
}
