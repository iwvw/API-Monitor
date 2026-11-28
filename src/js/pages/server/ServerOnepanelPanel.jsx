import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Select } from '@cloudflare/kumo/components/select';
import { SensitiveInput } from '@cloudflare/kumo/components/sensitive-input';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { EmptyState, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Check, ExternalLink, Plus, RefreshCw, Server, Trash } from '../../components/Icons.jsx';
import {
  createOnepanelConfig,
  deleteOnepanelConfig,
  getOnepanelHealth,
  listOnepanelConfigs,
  probeOnepanelInstall,
} from '../../modules/onepanel.js';
import {
  DEFAULT_ONEPANEL_BASE_URL,
  buildOnepanelPayload,
  describeOnepanelServer,
  getAvailableOnepanelServers,
  validateOnepanelForm,
} from '../../modules/onepanelConfig.js';

const DEFAULT_BASE_URL = DEFAULT_ONEPANEL_BASE_URL;

// 1Panel 连接管理（主机实例 → 1Panel 标签页）。
// 这里填写的 API Key 是能读写 nginx 站点配置的高权限凭据，后端加密入库，
// 列表接口只回 hasKey 布尔值，永不回传明文——所以编辑时输入框始终留空。
export default function ServerOnepanelPanel({ serverCurrentTab }) {
  const [servers, setServers] = useState([]);
  const [configs, setConfigs] = useState([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [testingId, setTestingId] = useState('');

  const [formServerId, setFormServerId] = useState('');
  const [formApiKey, setFormApiKey] = useState('');
  const [formBaseUrl, setFormBaseUrl] = useState(DEFAULT_BASE_URL);
  const [showAddDialog, setShowAddDialog] = useState(false);
  const [probing, setProbing] = useState(false);
  const [probedIds, setProbedIds] = useState(new Set());

  const loadServers = useCallback(async () => {
    try {
      const response = await fetch('/api/server/accounts', { cache: 'no-store' });
      const data = await response.json();
      if (data.success && Array.isArray(data.data)) {
        setServers(data.data.map(s => ({ id: String(s.id), name: s.name || s.id, host: s.host || '' })));
      }
    } catch {
      // 主机列表拉取失败不阻塞连接管理：用户仍可删除/测试已有连接。
    }
  }, []);

  const loadConfigs = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const payload = await listOnepanelConfigs();
      setConfigs(Array.isArray(payload?.data) ? payload.data : []);
    } catch (err) {
      setError(err?.message || '读取 1Panel 连接失败');
    } finally {
      setLoading(false);
    }
  }, []);

  // 仅在本标签页可见时拉取，避免离开页面后仍发请求。
  useEffect(() => {
    if (serverCurrentTab !== 'onepanel') return;
    loadServers();
    loadConfigs();
  }, [loadConfigs, loadServers, serverCurrentTab]);

  // Agent 探测哪些主机装了 1Panel：未安装的主机不进入新增下拉。
  // 探测失败的主机一律按「未安装」处理，避免把无法确认的机器列出来。
  const runProbe = useCallback(async list => {
    if (!list || list.length === 0) {
      setProbedIds(new Set());
      return;
    }
    setProbing(true);
    try {
      const payload = await probeOnepanelInstall(list.map(s => s.id));
      const items = Array.isArray(payload?.data?.servers) ? payload.data.servers : [];
      setProbedIds(new Set(items.filter(item => item?.installed).map(item => String(item.serverId))));
    } catch {
      setProbedIds(new Set());
    } finally {
      setProbing(false);
    }
  }, []);

  useEffect(() => {
    if (serverCurrentTab !== 'onepanel') return;
    runProbe(servers);
  }, [runProbe, serverCurrentTab, servers]);

  const serverNameById = useMemo(() => {
    const map = new Map();
    servers.forEach(s => map.set(s.id, s));
    return map;
  }, [servers]);

  // 只列出「已安装 1Panel 且尚未配置」的主机。
  const availableServers = useMemo(
    () => getAvailableOnepanelServers(servers, configs, probedIds),
    [configs, probedIds, servers]
  );

  useEffect(() => {
    if (availableServers.length === 0) {
      setFormServerId('');
      return;
    }
    if (!availableServers.some(s => s.id === formServerId)) {
      setFormServerId(availableServers[0].id);
    }
  }, [availableServers, formServerId]);

  const handleCreate = async () => {
    const invalid = validateOnepanelForm({ serverId: formServerId, apiKey: formApiKey });
    if (invalid) {
      toast.error(invalid);
      return;
    }
    setBusy(true);
    try {
      await createOnepanelConfig(
        buildOnepanelPayload({ serverId: formServerId, apiKey: formApiKey, baseUrl: formBaseUrl })
      );
      toast.success('已保存 1Panel 连接');
      setShowAddDialog(false);
      setFormApiKey('');
      setFormBaseUrl(DEFAULT_BASE_URL);
      await loadConfigs();
    } catch (err) {
      toast.error(err?.message || '保存失败');
    } finally {
      setBusy(false);
    }
  };

  const test = async serverId => {
    setTestingId(String(serverId));
    try {
      await getOnepanelHealth(String(serverId));
      toast.success(`${serverNameById.get(String(serverId))?.name || serverId} 连接正常`);
    } catch (err) {
      toast.error(err?.message || '连接失败');
    } finally {
      setTestingId('');
    }
  };

  const remove = async serverId => {
    const name = serverNameById.get(String(serverId))?.name || serverId;
    const confirmed = await dialog.deleteResource({
      title: '删除 1Panel 连接',
      message: `删除「${name}」的 1Panel 连接？不会影响 1Panel 本身，仅移除本面板保存的密钥。`,
      confirmText: '删除连接',
    });
    if (!confirmed) return;
    setBusy(true);
    try {
      await deleteOnepanelConfig(String(serverId));
      toast.success('已删除连接');
      await loadConfigs();
    } catch (err) {
      toast.error(err?.message || '删除失败');
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      {serverCurrentTab === 'onepanel' && (
    <div className="min-h-0 overflow-auto cq-md:h-full">
      <SectionCard
        className="flex min-h-0 cq-md:h-full"
        title="1Panel 连接"
        icon={<Server className="h-4 w-4 text-brand" />}
        actions={
          <>
            <Button
              size="xs"
              variant="secondary"
              icon={<RefreshCw className="h-3.5 w-3.5" />}
              onClick={() => {
                loadConfigs();
                loadServers();
                runProbe(servers);
              }}
              disabled={loading}
            >
              刷新
            </Button>
            <Button
              size="xs"
              variant="primary"
              icon={<Plus className="h-3.5 w-3.5" />}
              onClick={() => setShowAddDialog(true)}
              disabled={availableServers.length === 0 || probing}
              loading={probing}
            >
              {probing ? '检测中' : '新增连接'}
            </Button>
          </>
        }
        bodyClassName="flex min-h-0 flex-1 flex-col gap-3 overflow-auto"
      >
        {error ? (
          <div className="rounded-md border border-kumo-line bg-kumo-recessed/40 px-3 py-2 text-xs text-kumo-danger">
            {error}
          </div>
        ) : null}

        {/* 已配置连接 */}
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2 text-xs font-semibold text-kumo-subtle">
            <span>已配置</span>
            <span className="h-px min-w-4 flex-1 bg-kumo-line" />
            <span className="font-normal">{configs.length} 台</span>
          </div>

          {loading ? (
            <div className="flex flex-col gap-2">
              {[0, 1].map(i => (
                <SkeletonLine key={i} className="h-12 w-full rounded-lg" />
              ))}
            </div>
          ) : configs.length === 0 ? (
            <EmptyState
              icon={Server}
              title="还没有配置 1Panel"
              description={
                probing
                  ? '正在检测各主机是否安装 1Panel…'
                  : availableServers.length === 0
                    ? '未检测到安装 1Panel 的主机。点「新增连接」会重新检测。'
                    : '点击右上角「新增连接」，选择已安装 1Panel 的主机并填写 API Key。'
              }
            />
          ) : (
            <div className="flex flex-col gap-2">
              {configs.map(config => {
                const server = serverNameById.get(String(config.serverId));
                const testing = testingId === String(config.serverId);
                return (
                  <div
                    key={config.serverId}
                    className="flex flex-wrap items-center gap-2 rounded-md border border-kumo-line bg-kumo-base px-3 py-2"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-semibold text-kumo-strong">
                        {server?.name || config.serverId}
                        {server?.host ? (
                          <span className="ml-2 font-mono text-[11px] font-normal text-kumo-subtle">
                            {server.host}
                          </span>
                        ) : null}
                      </div>
                      <div className="truncate font-mono text-[11px] text-kumo-subtle">
                        {config.baseUrl || DEFAULT_BASE_URL}
                      </div>
                    </div>
                    <Badge variant={config.hasKey ? 'success' : 'warning'} className="text-[10px]">
                      {config.hasKey ? '已配置密钥' : '密钥异常'}
                    </Badge>
                    <Button
                      size="xs"
                      variant="secondary"
                      icon={<Check className="h-3.5 w-3.5" />}
                      onClick={() => test(config.serverId)}
                      disabled={testing}
                    >
                      {testing ? '测试中' : '测试连接'}
                    </Button>
                    <Button
                      size="xs"
                      variant="ghost"
                      shape="square"
                      icon={<Trash className="h-3.5 w-3.5" />}
                      aria-label={`删除 ${server?.name || config.serverId} 的连接`}
                      onClick={() => remove(config.serverId)}
                      disabled={busy}
                    />
                  </div>
                );
              })}
            </div>
          )}
        </div>

        <p className="text-[11px] text-kumo-subtle">
          需要先在 1Panel 中开启「API 接口」并生成密钥。
          <a
            className="ml-1 inline-flex items-center gap-0.5 text-brand hover:underline"
            href="/api/onepanel/spec"
            target="_blank"
            rel="noopener noreferrer"
          >
            查看可用接口
            <ExternalLink className="h-3 w-3" />
          </a>
        </p>
      </SectionCard>
    </div>
      )}

      <LayerDialog.Root open={showAddDialog} onOpenChange={setShowAddDialog}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>新增 1Panel 连接</LayerDialog.Title>
          <LayerDialog.Body>
            <div className="flex flex-col gap-3 text-xs">
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle">主机</label>
                <Select
                  value={formServerId}
                  onValueChange={setFormServerId}
                  placeholder="选择主机"
                  aria-label="选择主机"
                  items={[
                    { value: '', label: '选择主机' },
                    ...availableServers.map(s => ({ value: s.id, label: describeOnepanelServer(s) })),
                  ]}
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle">API Key</label>
                {/* 不用 type=password：浏览器密码管理器会把已存凭据自动填进来覆盖手输内容。
                    SensitiveInput 支持点击显示/隐藏，同时带上各密码管理器的忽略标记。 */}
                <SensitiveInput
                  size="sm"
                  value={formApiKey}
                  onValueChange={setFormApiKey}
                  placeholder="1Panel 面板 → 设置 → API 接口 生成"
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                  className="w-full px-3 py-2 font-mono text-kumo-strong"
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle">面板地址</label>
                <Input
                  size="sm"
                  value={formBaseUrl}
                  onChange={event => setFormBaseUrl(event.target.value)}
                  placeholder={DEFAULT_BASE_URL}
                  aria-label="1Panel 面板地址"
                  autoComplete="off"
                  data-form-type="other"
                  className="px-3 py-2 text-kumo-strong"
                />
                <p className="text-[11px] text-kumo-subtle">
                  填 1Panel 服务端可达的地址，本机安装保持默认即可。
                </p>
              </div>
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Action type="button" loading={busy} onClick={handleCreate}>
              保存
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </>
  );
}
