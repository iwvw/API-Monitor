import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';

export default function FirewallDialog({ open, onOpenChange, editingFirewall, firewallForm, setFirewallForm, submittingFirewall, onSubmit }) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>{editingFirewall ? '编辑防火墙规则' : '新建防火墙规则'}</LayerDialog.Title>
        <LayerDialog.Description>规则对满足条件的流量执行放行或拒绝，优先级数字越小越先匹配。</LayerDialog.Description>
        <LayerDialog.Body>
      <div className="flex flex-col gap-3">
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">规则名称 *</span>
          <Input size="sm" value={firewallForm.name} onChange={(event) => setFirewallForm({ ...firewallForm, name: event.target.value })} placeholder="例如 allow-http" disabled={Boolean(editingFirewall)} />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">描述</span>
          <Input size="sm" value={firewallForm.description} onChange={(event) => setFirewallForm({ ...firewallForm, description: event.target.value })} placeholder="可选" />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">方向</span>
            <Select alignItemWithTrigger size="sm" value={firewallForm.direction} onValueChange={(value) => setFirewallForm({ ...firewallForm, direction: value })} items={[
              { value: 'INGRESS', label: '入站（INGRESS）' },
              { value: 'EGRESS', label: '出站（EGRESS）' },
            ]} />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">动作</span>
            <Select alignItemWithTrigger size="sm" value={firewallForm.action} onValueChange={(value) => setFirewallForm({ ...firewallForm, action: value })} items={[
              { value: 'allow', label: '允许（ALLOW）' },
              { value: 'deny', label: '拒绝（DENY）' },
            ]} />
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">优先级（0-65535）</span>
            <Input size="sm" type="number" value={firewallForm.priority} onChange={(event) => setFirewallForm({ ...firewallForm, priority: event.target.value })} />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">网络</span>
            <Input size="sm" value={firewallForm.network} onChange={(event) => setFirewallForm({ ...firewallForm, network: event.target.value })} placeholder="留空默认网络" />
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">来源网段（CIDR，逗号分隔）</span>
            <Input size="sm" value={firewallForm.sourceRanges} onChange={(event) => setFirewallForm({ ...firewallForm, sourceRanges: event.target.value })} placeholder="例如 0.0.0.0/0" />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">目标网段（CIDR，逗号分隔）</span>
            <Input size="sm" value={firewallForm.destinationRanges} onChange={(event) => setFirewallForm({ ...firewallForm, destinationRanges: event.target.value })} placeholder="可选" />
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">协议</span>
            <Select alignItemWithTrigger size="sm" value={firewallForm.protocol} onValueChange={(value) => setFirewallForm({ ...firewallForm, protocol: value })} items={[
              { value: 'tcp', label: 'TCP' },
              { value: 'udp', label: 'UDP' },
              { value: 'icmp', label: 'ICMP' },
              { value: 'all', label: 'all' },
            ]} />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">端口（逗号分隔，可选）</span>
            <Input size="sm" value={firewallForm.ports} onChange={(event) => setFirewallForm({ ...firewallForm, ports: event.target.value })} placeholder="例如 80,443" />
          </label>
        </div>
      </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" loading={submittingFirewall} onClick={onSubmit}>
            {editingFirewall ? '保存' : '创建'}
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
