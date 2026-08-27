import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Badge, Button, ClipboardText, Tabs } from '@cloudflare/kumo';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { LinkSimple } from '@phosphor-icons/react';
import { Check, Globe, Lock, Plus, Server, ShieldCheck, X } from '../Icons.jsx';
import { toast } from '../../modules/toast.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import FormCard from '../ui/FormCard.jsx';

const FORWARD_API = '/api/server/forward';

const TRANSPORT_TABS = [
  { value: 'cloudflare_tunnel', label: (<span className="inline-flex items-center gap-1"><LinkSimple className="h-3 w-3" />CF Tunnel</span>) },
  { value: 'tcp_relay', label: (<span className="inline-flex items-center gap-1"><Server className="h-3 w-3" />TCP 中继</span>) },
  { value: 'p2p', label: (<span className="inline-flex items-center gap-1"><ShieldCheck className="h-3 w-3" />P2P</span>) },
];

const ACCESS_TABS = [
  { value: 'public', label: (<span className="inline-flex items-center gap-1"><Globe className="h-3 w-3" />公开</span>) },
  { value: 'token', label: (<span className="inline-flex items-center gap-1"><Lock className="h-3 w-3" />Token</span>) },
  { value: 'panel', label: (<span className="inline-flex items-center gap-1"><ShieldCheck className="h-3 w-3" />面板</span>) },
];

const TARGET_HEALTH_VARIANT = {
  healthy: 'success',
  unhealthy: 'error',
  unknown: 'neutral',
};

const TARGET_HEALTH_LABELS = {
  healthy: '健康',
  unhealthy: '故障',
  unknown: '未知',
};

const PROTOCOL_ITEMS = [
  { value: 'tcp', label: 'TCP' },
  { value: 'http', label: 'HTTP' },
  { value: 'https', label: 'HTTPS' },
];

const SERVICE_PRESETS = [
  { value: '', label: '常用服务预设…' },
  { value: 'ssh', label: 'SSH (22)', port: 22, protocol: 'tcp', name: 'SSH' },
  { value: 'rdp', label: 'RDP 远程桌面 (3389)', port: 3389, protocol: 'tcp', name: 'RDP 远程桌面' },
  { value: 'http', label: 'HTTP (80)', port: 80, protocol: 'http', name: 'HTTP' },
  { value: 'https', label: 'HTTPS (443)', port: 443, protocol: 'https', name: 'HTTPS' },
  { value: 'mysql', label: 'MySQL (3306)', port: 3306, protocol: 'tcp', name: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL (5432)', port: 5432, protocol: 'tcp', name: 'PostgreSQL' },
  { value: 'redis', label: 'Redis (6379)', port: 6379, protocol: 'tcp', name: 'Redis' },
  { value: 'mongo', label: 'MongoDB (27017)', port: 27017, protocol: 'tcp', name: 'MongoDB' },
  { value: 'docker', label: 'Docker API (2375)', port: 2375, protocol: 'tcp', name: 'Docker API' },
];

const TRANSPORT_NAMES = {
  cloudflare_tunnel: 'Cloudflare Tunnel',
  tcp_relay: 'TCP 中继',
  p2p: 'P2P 直连',
};

export default function ForwardDialog({ open, onOpenChange, onSubmit, servers, editing }) {
  const [name, setName] = useState('');
  const [serverId, setServerId] = useState('');
  const [localHost, setLocalHost] = useState('127.0.0.1');
  const [localPort, setLocalPort] = useState('');
  const [protocol, setProtocol] = useState('tcp');
  const [preset, setPreset] = useState('');
  const [transport, setTransport] = useState('cloudflare_tunnel');
  const [wholeHost, setWholeHost] = useState(false);
  const [relayServerId, setRelayServerId] = useState('');
  const [accessMode, setAccessMode] = useState('public');
  const [submitting, setSubmitting] = useState(false);
  const [targets, setTargets] = useState([]);
  const [targetServerId, setTargetServerId] = useState('');
  const { isArmed, confirmPress } = useConfirmPress();
  const [targetPriority, setTargetPriority] = useState('1');
  const [createdToken, setCreatedToken] = useState('');
  const [relayPorts, setRelayPorts] = useState(null);
  // 高级：健康检查 / 故障转移（仅编辑态可调）
  const [healthCheckEnabled, setHealthCheckEnabled] = useState(false);
  const [failoverEnabled, setFailoverEnabled] = useState(false);
  // 一键部署 CF Tunnel：源主机未部署隧道时的内联部署表单
  const [tunnelDeployOpen, setTunnelDeployOpen] = useState(false);
  const [tunnelAccounts, setTunnelAccounts] = useState([]);
  const [tunnelZones, setTunnelZones] = useState([]);
  const [tunnelZonesLoading, setTunnelZonesLoading] = useState(false);
  const [tunnelAccountId, setTunnelAccountId] = useState('');
  const [tunnelZoneId, setTunnelZoneId] = useState('');
  const [tunnelHostname, setTunnelHostname] = useState('');
  const [tunnelDeploying, setTunnelDeploying] = useState(false);
  const [tunnelDeployError, setTunnelDeployError] = useState('');

  // 预检
  const [preflight, setPreflight] = useState(null);
  const [preflightLoading, setPreflightLoading] = useState(false);
  const preflightTimerRef = useRef(null);

  const loadTargets = useCallback(async (forwardId) => {
    if (!forwardId) return;
    try {
      const res = await fetch(`${FORWARD_API}/${forwardId}/targets`);
      const json = await res.json();
      if (json.success) setTargets(json.data || []);
    } catch (e) {}
  }, []);

  useEffect(() => {
    if (open) {
      if (editing) {
        setName(editing.name);
        setServerId(editing.server_id);
        setLocalHost(editing.local_host);
        setLocalPort(String(editing.local_port));
        setProtocol(editing.protocol);
        setTransport(editing.transport);
        setWholeHost(!!editing.whole_host);
        setRelayServerId(editing.relay_server_id || '');
        setAccessMode(editing.access_mode);
        setHealthCheckEnabled(!!editing.health_check_enabled);
        setFailoverEnabled(!!editing.failover_enabled);
        loadTargets(editing.id);
      } else {
        setName('');
        setServerId(servers[0]?.id || '');
        setLocalHost('127.0.0.1');
        setLocalPort('');
        setProtocol('tcp');
        setTransport('cloudflare_tunnel');
        setWholeHost(false);
        setRelayServerId('');
        setAccessMode('public');
        setHealthCheckEnabled(false);
        setFailoverEnabled(false);
        setTargets([]);
      }
      setTargetServerId('');
      setTargetPriority('1');
      setPreset('');
      setPreflight(null);
      setCreatedToken('');
      setRelayPorts(null);
    }
  }, [open, editing, servers, loadTargets]);

  // 预检：表单关键字段变化后防抖 500ms 自动触发
  const runPreflight = useCallback(async () => {
    const port = parseInt(localPort, 10);
    if (!serverId || !port || port < 1 || port > 65535) return;
    setPreflightLoading(true);
    try {
      const res = await fetch(`${FORWARD_API}/preflight`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          forward_id: editing?.id || '',
          server_id: serverId,
          local_host: localHost,
          transport,
          local_port: port,
          relay_server_id: transport === 'tcp_relay' ? relayServerId : '',
        }),
      });
      const json = await res.json();
      if (json.success) setPreflight(json.data);
      else setPreflight(null);
    } catch (e) {
      setPreflight(null);
    } finally {
      setPreflightLoading(false);
    }
  }, [serverId, transport, localPort, localHost, relayServerId]);

  useEffect(() => {
    if (!open) return undefined;
    if (preflightTimerRef.current) window.clearTimeout(preflightTimerRef.current);
    preflightTimerRef.current = window.setTimeout(() => {
      if (serverId && parseInt(localPort, 10) > 0) runPreflight();
    }, 500);
    return () => {
      if (preflightTimerRef.current) window.clearTimeout(preflightTimerRef.current);
    };
  }, [open, serverId, transport, localPort, localHost, relayServerId, runPreflight]);

  // 中继入口选定后查询可用端口数
  useEffect(() => {
    if (!open || transport !== 'tcp_relay' || !relayServerId) {
      setRelayPorts(null);
      return;
    }
    let cancelled = false;
    fetch(`${FORWARD_API}/available-ports?server_id=${encodeURIComponent(relayServerId)}`)
      .then((res) => res.json())
      .then((json) => { if (!cancelled && json.success) setRelayPorts(json.data); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [open, transport, relayServerId]);

  const applyPreset = (value) => {
    setPreset(value);
    const p = SERVICE_PRESETS.find((s) => s.value === value);
    if (!p) return;
    setProtocol(p.protocol);
    setLocalPort(String(p.port));
    if (!name.trim()) setName(p.name);
  };

  const handleSubmit = async () => {
    if (!name.trim() || !serverId || !localPort) {
      toast.error('请填写必要字段');
      return;
    }
    const port = parseInt(localPort, 10);
    if (isNaN(port) || port < 1 || port > 65535) {
      toast.error('端口范围 1-65535');
      return;
    }
    setSubmitting(true);
    try {
      const result = await onSubmit({
        name: name.trim(),
        server_id: serverId,
        local_host: localHost,
        local_port: port,
        protocol,
        transport,
        whole_host: transport === 'cloudflare_tunnel' && wholeHost,
        relay_server_id: transport === 'tcp_relay' ? relayServerId : '',
        access_mode: accessMode,
        ...(editing ? { health_check_enabled: healthCheckEnabled, failover_enabled: failoverEnabled } : {}),
      });
      // token 模式：创建响应中的明文令牌仅此一次，留在弹窗内展示
      if (result && result.access_token) {
        setCreatedToken(result.access_token);
      }
    } finally {
      setSubmitting(false);
    }
  };

  const handleAddTarget = async () => {
    if (!editing || !targetServerId) return;
    try {
      const res = await fetch(`${FORWARD_API}/${editing.id}/targets`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ server_id: targetServerId, priority: parseInt(targetPriority, 10) || 1, role: 'standby' }),
      });
      const json = await res.json();
      if (json.success) {
        toast.success('备用主机已添加');
        loadTargets(editing.id);
        setTargetServerId('');
      } else {
        toast.error(json.error || '添加失败');
      }
    } catch (e) { toast.error('添加请求失败'); }
  };

  const handleRemoveTarget = async (targetId) => {
    if (!confirmPress(`fwd-target:${targetId}`, '移除备用主机')) return;
    try {
      const res = await fetch(`${FORWARD_API}/${editing.id}/targets/${targetId}`, { method: 'DELETE' });
      const json = await res.json();
      if (json.success) {
        toast.success('备用主机已移除');
        loadTargets(editing.id);
      }
    } catch (e) { toast.error('移除请求失败'); }
  };

  const port = parseInt(localPort, 10);
  const preflightReady = !!serverId && !!port && port >= 1 && port <= 65535;

  // ===== 一键部署 CF Tunnel =====
  const cfTunnelCheckFailed = () => {
    if (!preflight?.checks) return false;
    const check = preflight.checks.find((c) => c.name === 'CF Tunnel 已就绪');
    return Boolean(check && !check.passed);
  };
  const loadTunnelZones = useCallback(async (accountId) => {
    setTunnelZonesLoading(true);
    try {
      const res = await fetch(`/api/cloudflare/accounts/${encodeURIComponent(accountId)}/zones`);
      const json = await res.json();
      const zones = Array.isArray(json?.zones) ? json.zones : [];
      setTunnelZones(zones);
      const first = zones[0];
      setTunnelZoneId(first?.id || '');
      setTunnelHostname(`fwd-${Math.random().toString(36).slice(2, 8)}.${first?.name || ''}`);
    } catch (e) {
      setTunnelZones([]);
      setTunnelDeployError('获取 Cloudflare Zone 失败');
    } finally {
      setTunnelZonesLoading(false);
    }
  }, []);
  const loadTunnelAccounts = useCallback(async () => {
    setTunnelZonesLoading(true);
    try {
      const res = await fetch('/api/cloudflare/accounts');
      const json = await res.json();
      const accounts = Array.isArray(json) ? json : [];
      setTunnelAccounts(accounts);
      if (accounts.length > 0) {
        setTunnelAccountId(accounts[0].id);
        loadTunnelZones(accounts[0].id);
      }
    } catch (e) {
      setTunnelDeployError('获取 Cloudflare 账号失败');
      setTunnelZonesLoading(false);
    }
  }, [loadTunnelZones]);
  const openTunnelDeploy = () => {
    setTunnelDeployOpen(true);
    setTunnelDeployError('');
    if (tunnelAccounts.length === 0) loadTunnelAccounts();
  };
  const tunnelAccountItems = tunnelAccounts.map((a) => ({ value: a.id, label: a.name || a.id }));
  const tunnelZoneItems = tunnelZones.map((z) => ({ value: z.id, label: z.name }));
  const onTunnelAccountChange = (v) => {
    setTunnelAccountId(v);
    setTunnelZones([]);
    setTunnelZoneId('');
    setTunnelHostname('');
    loadTunnelZones(v);
  };
  const onTunnelZoneChange = (v) => {
    setTunnelZoneId(v);
    const zone = tunnelZones.find((z) => z.id === v);
    if (zone) setTunnelHostname(`fwd-${Math.random().toString(36).slice(2, 8)}.${zone.name}`);
  };
  const deploySourceTunnel = async () => {
    if (!serverId) return;
    if (!tunnelAccountId || !tunnelZoneId || !tunnelHostname.trim()) {
      setTunnelDeployError('请选择 Cloudflare 账号、Zone 并填写域名');
      return;
    }
    setTunnelDeploying(true);
    setTunnelDeployError('');
    try {
      const res = await fetch(`/api/server/agent/proxy/tunnels/${encodeURIComponent(serverId)}/deploy`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ account_id: tunnelAccountId, zone_id: tunnelZoneId, hostname: tunnelHostname.trim() }),
      });
      const json = await res.json();
      if (!res.ok || json.success === false) throw new Error(json.error || json.message || 'Tunnel 部署失败');
      toast.success('Tunnel 部署已提交');
      // 轮询托管隧道列表直到 running（部署是异步任务，避免读过期闭包）
      let attempts = 0;
      const poll = setInterval(async () => {
        attempts += 1;
        try {
          const listRes = await fetch(`/api/server/agent/proxy/tunnels?server_id=${encodeURIComponent(serverId)}`);
          const listJson = await listRes.json();
          const tunnels = Array.isArray(listJson?.data) ? listJson.data : [];
          const ready = tunnels.some((t) => t.apply_status === 'running');
          if (ready) {
            clearInterval(poll);
            setTunnelDeploying(false);
            setTunnelDeployOpen(false);
            runPreflight();
            toast.success('Cloudflare Tunnel 已就绪');
            return;
          }
        } catch (e) { /* 继续轮询 */ }
        if (attempts >= 40) {
          clearInterval(poll);
          setTunnelDeploying(false);
          setTunnelDeployError('部署仍在后台进行，稍后可点击「重新预检」查看状态');
        }
      }, 3000);
    } catch (e) {
      setTunnelDeployError(e?.message || 'Tunnel 部署失败');
      setTunnelDeploying(false);
    }
  };

  const serverItems = servers.map((s) => ({ value: s.id, label: s.name || s.id }));
  const relayItems = servers.filter((s) => s.id !== serverId).map((s) => ({ value: s.id, label: s.name || s.id }));
  const standbyItems = relayItems;

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog
        className="flex flex-col overflow-hidden !p-0"
        style={{ width: 'calc(100vw - 2rem)', maxWidth: '60rem', maxHeight: 'min(100dvh - 2rem, 48rem)' }}
      >
        <div className="shrink-0 px-6 pt-5">
          <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
            {editing ? '编辑转发规则' : '创建转发规则'}
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
            将内网服务通过 {TRANSPORT_NAMES[transport] || 'TCP 中继'} 暴露到公网。
          </Dialog.Description>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
          <div className="grid grid-cols-2 gap-x-5 gap-y-4">
            {/* ====== 左列：基本信息 + 访问控制 ====== */}
            <div className="space-y-4">
              <FormCard icon={<Server className="h-4 w-4" />} title="基本信息" description="服务所在主机与本地端口">
                <div className="flex flex-col gap-3 py-3">
                  <div className="flex flex-col gap-1">
                    <label className="text-xs text-kumo-text-secondary">规则名称</label>
                    <Input size="sm" value={name} onChange={(e) => setName(e.target.value)} placeholder="如 调试 API" />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-xs text-kumo-text-secondary">常用服务预设</label>
                    <Select size="sm" value={preset} onValueChange={applyPreset} items={SERVICE_PRESETS} placeholder="选择后自动填充端口与协议" aria-label="常用服务预设" />
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div className="flex flex-col gap-1">
                      <label className="text-xs text-kumo-text-secondary">源主机</label>
                      <Select size="sm" value={serverId} onValueChange={setServerId} items={serverItems} placeholder="选择主机" aria-label="源主机" disabled={!!editing} />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs text-kumo-text-secondary">本地地址</label>
                      <Input size="sm" value={localHost} onChange={(e) => setLocalHost(e.target.value)} placeholder="127.0.0.1" />
                    </div>
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div className="flex flex-col gap-1">
                      <label className="text-xs text-kumo-text-secondary">本地端口</label>
                      <Input size="sm" type="number" value={localPort} onChange={(e) => setLocalPort(e.target.value)} placeholder="5000" min={1} max={65535} />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs text-kumo-text-secondary">协议</label>
                      <Select size="sm" value={protocol} onValueChange={setProtocol} items={PROTOCOL_ITEMS} aria-label="协议" />
                    </div>
                  </div>
                </div>
              </FormCard>

              <FormCard icon={<Lock className="h-4 w-4" />} title="访问控制" description="客户端连接所需凭证">
                <div className="flex flex-col gap-3 py-3">
                  <Tabs size="sm" variant="segmented" className="w-full" value={accessMode} onValueChange={setAccessMode} tabs={ACCESS_TABS} />
                  {accessMode === 'token' && !createdToken && (
                    <p className="rounded-lg bg-kumo-fill px-3 py-2 text-xs text-kumo-text-secondary">
                      {transport === 'tcp_relay'
                        ? '创建后自动生成 32 位访问令牌，仅展示一次。TCP 中继部署时入口强制校验：TCP 客户端先发 [4 字节长度+token] 握手，HTTP 客户端用 Authorization: Bearer 头。'
                        : '创建后自动生成 32 位访问令牌，仅展示一次。CF 隧道经源主机鉴权代理校验（Authorization: Bearer / ?token= / cookie），仅支持 http/https（tcp 请改用 TCP 中继 + token）。'}
                    </p>
                  )}
                  {accessMode === 'panel' && <p className="rounded-lg bg-kumo-fill px-3 py-2 text-xs text-kumo-text-secondary">需登录面板后访问（预留）。当前版本不可部署，请先用「公开」模式。</p>}
                  {createdToken && (
                    <div className="rounded-lg border border-kumo-brand/30 bg-kumo-fill px-3 py-2">
                      <p className="mb-1 text-xs text-kumo-text-secondary">访问令牌（仅展示一次，请立即保存）</p>
                      <ClipboardText size="sm" text={createdToken} tooltip={{ text: '复制', copiedText: '已复制', side: 'top' }} />
                    </div>
                  )}
                </div>
              </FormCard>
            </div>

            {/* ====== 右列：传输方式 + 预检 + 备用 ====== */}
            <div className="space-y-4">
              <FormCard icon={<LinkSimple className="h-4 w-4" />} title="传输方式" description="创建后不可变更">
                <div className="flex flex-col gap-3 py-3">
                  <Tabs size="sm" variant="segmented" className="w-full" value={transport} onValueChange={(v) => { if (!editing) setTransport(v); }} tabs={TRANSPORT_TABS} />
                  {transport === 'cloudflare_tunnel' && (
                    <div className="flex flex-col gap-2">
                      <Switch
                        size="sm"
                        label="整域部署（根路径直达本地服务）"
                        controlFirst={false}
                        checked={wholeHost}
                        onCheckedChange={setWholeHost}
                      />
                      {wholeHost ? (
                        <div className="rounded-lg bg-kumo-fill px-3 py-2 text-xs text-kumo-text-secondary">
                          <p>整个域名路由到本地服务，资源路径不会被子路径拦截，适合完整网站 / SPA。</p>
                          <p className="mt-1 font-mono text-[11px] text-kumo-brand">
                            {editing?.tunnel_hostname ? `${protocol === 'http' ? 'http' : 'https'}://${editing.tunnel_hostname}` : 'https://域名根路径'}
                          </p>
                          <p className="mt-1 text-[11px] text-kumo-text-warning">整域部署会独占该域名，不再与子路径转发共享。</p>
                        </div>
                      ) : (
                        <div className="rounded-lg bg-kumo-fill px-3 py-2 text-xs text-kumo-text-secondary">
                          <p>走源主机已有的 Cloudflare Tunnel，自动追加 ingress 路径：</p>
                          <p className="mt-1 font-mono text-[11px] text-kumo-text-secondary">{editing?.tunnel_path || '/fwd/自动生成'}</p>
                        </div>
                      )}
                    </div>
                  )}
                  {transport === 'tcp_relay' && (
                    <div className="flex flex-col gap-1">
                      <label className="text-xs text-kumo-text-secondary">中继入口主机</label>
                      <Select size="sm" value={relayServerId} onValueChange={setRelayServerId} items={relayItems} placeholder="选择中继入口" aria-label="中继入口主机" />
                      <p className="text-[11px] text-kumo-text-secondary">
                        入口主机需有公网 IP，端口自动从 55655-60655 分配。
                        {relayPorts?.available?.length > 0 && ` 当前可用端口 ${relayPorts.available.length} 个（${relayPorts.available[0]}–${relayPorts.available[relayPorts.available.length - 1]}）`}
                      </p>
                    </div>
                  )}
                  {transport === 'p2p' && <p className="rounded-lg bg-kumo-fill px-3 py-2 text-xs text-kumo-text-secondary">P2P 直连为预留能力，Phase 3 开放。</p>}
                </div>
              </FormCard>

              <FormCard icon={<ShieldCheck className="h-4 w-4" />} title="预检结果" description="部署前自动检查">
                <div className="flex flex-col gap-2 py-3">
                  {!preflightReady ? (
                    <p className="text-xs text-kumo-text-secondary">填写源主机与本地端口后自动预检。</p>
                  ) : preflightLoading ? (
                    <div className="flex items-center gap-2 text-xs text-kumo-text-secondary">
                      <span className="inline-block h-3 w-3 animate-spin rounded-full border border-kumo-line border-t-kumo-brand" />正在检查…
                    </div>
                  ) : preflight ? (
                    <div className="flex flex-col gap-1.5">
                      {(preflight.checks || []).map((check) => (
                        <div key={check.name} className="flex items-center gap-2 text-xs">
                          <span className={check.passed ? 'text-kumo-success' : 'text-kumo-danger'}>{check.passed ? <Check className="h-3.5 w-3.5" weight="bold" /> : <X className="h-3.5 w-3.5" weight="bold" />}</span>
                          <span className="text-kumo-default">{check.name}</span>
                          {!check.passed && <span className="text-kumo-text-secondary">未通过</span>}
                        </div>
                      ))}
                    </div>
                  ) : <p className="text-xs text-kumo-text-secondary">预检失败，可点击下方按钮重试。</p>}
                  <div className="mt-1">
                    <Button size="sm" variant="outline" onClick={runPreflight} disabled={!preflightReady || preflightLoading}>重新预检</Button>
                  </div>
                  {transport === 'cloudflare_tunnel' && cfTunnelCheckFailed() && (
                    <div className="rounded-lg border border-kumo-warning/40 bg-kumo-fill px-3 py-2">
                      <p className="text-xs leading-5 text-kumo-text-secondary">
                        源主机尚未部署 Cloudflare Tunnel，部署转发规则前需先建立隧道。
                      </p>
                      {!tunnelDeployOpen ? (
                        <Button size="sm" variant="secondary" className="mt-1.5" onClick={openTunnelDeploy} disabled={tunnelDeploying}>
                          一键部署隧道
                        </Button>
                      ) : (
                        <div className="mt-1.5 flex flex-col gap-2">
                          <Select size="sm" value={tunnelAccountId} onValueChange={onTunnelAccountChange} items={tunnelAccountItems} placeholder="Cloudflare 账号" aria-label="Cloudflare 账号" disabled={tunnelZonesLoading} />
                          <Select size="sm" value={tunnelZoneId} onValueChange={onTunnelZoneChange} items={tunnelZoneItems} placeholder="Zone（域名）" aria-label="Cloudflare Zone" disabled={tunnelZonesLoading} />
                          <Input size="sm" value={tunnelHostname} onChange={(e) => setTunnelHostname(e.target.value)} placeholder="tunnel.example.com" aria-label="Tunnel 域名" />
                          {tunnelDeployError && <p className="text-xs text-kumo-danger">{tunnelDeployError}</p>}
                          <Button size="sm" variant="primary" onClick={deploySourceTunnel} disabled={tunnelDeploying || !tunnelAccountId || !tunnelZoneId || !tunnelHostname.trim()}>
                            {tunnelDeploying ? '部署中…' : '开始部署'}
                          </Button>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              </FormCard>

              {editing && (
                <FormCard icon={<ShieldCheck className="h-4 w-4" />} title="健康与故障转移" description="探测失败自动切换到备用主机">
                  <div className="flex flex-col gap-3 py-3">
                    <div className="grid grid-cols-2 gap-3">
                      <Switch size="sm" label="健康检查" controlFirst={false} checked={healthCheckEnabled} onCheckedChange={setHealthCheckEnabled} />
                      <Switch size="sm" label="故障转移" controlFirst={false} checked={failoverEnabled} onCheckedChange={setFailoverEnabled} />
                    </div>
                    {targets.length === 0 ? (
                      <p className="text-xs text-kumo-text-secondary">暂无备用主机</p>
                    ) : (
                      <div className="flex flex-col gap-1">
                        {targets.map((t) => (
                          <div key={t.id} className="flex items-center justify-between gap-2 rounded-lg bg-kumo-fill px-3 py-2 text-xs">
                            <span className="min-w-0 truncate text-kumo-default">{t.server_name || t.server_id}</span>
                            <span className="shrink-0 text-kumo-text-secondary">优先级 {t.priority}</span>
                            <Badge variant={TARGET_HEALTH_VARIANT[t.health_status] || 'neutral'} size="sm">{TARGET_HEALTH_LABELS[t.health_status] || t.health_status}</Badge>
                            <Button size="sm" variant={isArmed(`fwd-target:${t.id}`) ? 'destructive' : 'secondary-destructive'} shape="square" aria-label="移除" onClick={() => handleRemoveTarget(t.id)}>{isArmed(`fwd-target:${t.id}`) ? '确认' : '×'}</Button>
                          </div>
                        ))}
                      </div>
                    )}
                    <div className="flex items-center gap-2">
                      <Select size="sm" value={targetServerId} onValueChange={setTargetServerId} items={standbyItems} placeholder="选择主机" aria-label="备用主机" className="min-w-0 flex-1" />
                      <Input size="sm" type="number" value={targetPriority} onChange={(e) => setTargetPriority(e.target.value)} min="1" max="99" className="w-16" placeholder="优先级" aria-label="优先级" />
                      <Button size="sm" onClick={handleAddTarget} disabled={!targetServerId}><Plus className="h-3.5 w-3.5" weight="bold" /></Button>
                    </div>
                  </div>
                </FormCard>
              )}
            </div>
          </div>
        </div>

        <div className="flex shrink-0 items-center justify-end gap-2 border-t border-kumo-line px-6 py-4">
          <Dialog.Close render={(props) => <Button size="sm" variant="outline" {...props}>取消</Button>} />
          {createdToken ? (
            <Button size="sm" onClick={() => onOpenChange(false)}>完成</Button>
          ) : (
            <Button size="sm" onClick={handleSubmit} disabled={submitting}>
              {submitting ? '提交中...' : editing ? '保存' : '创建并部署'}
            </Button>
          )}
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
