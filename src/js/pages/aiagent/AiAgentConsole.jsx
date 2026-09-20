import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from '../../modules/toast.js';
import { del, get, post, put } from '../../modules/apiClient.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { ClipboardText as ClipboardTextField, Empty, Loader, Tabs } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import {
  PageStack,
  SectionCard,
  StatusBadge,
  stickyTabsBaseClass,
} from '../../components/ui/AppPrimitives.jsx';
import {
  Activity,
  ClipboardText,
  Edit,
  Eye,
  Lock,
  Pause,
  Play,
  Plug,
  Plus,
  RefreshCw,
  RotateCw,
  Trash,
  Users,
} from '../../components/Icons.jsx';
import {
  STATUS_TONE,
  buildInstanceDetailGroups,
  formatResource,
  instanceStatus,
} from '../../modules/aiagentInstanceStatus.js';
import {
  AUTO_REFRESH_INTERVAL_MS,
  readAutoRefreshPreference,
  shouldAutoRefresh,
  writeAutoRefreshPreference,
} from '../../modules/aiagentAutoRefresh.js';
import { sortInstances } from '../../modules/aiagentInstanceList.js';
import { resolvePortConflict } from '../../modules/aiagentPortConflict.js';
import { useVisiblePolling } from '../../modules/usePageVisibility.js';

// 生命周期控件（ADR-0006）：仅当主机 Agent 声明了能力、且实例启用时可用。
// 未声明能力时按钮置灰并提示升级，而不是点了没反应。
function renderLifecycleControls(instance, status, onLifecycle, busyId) {
  const lifecycle = instance.lifecycle || {};
  const supported = lifecycle.supported === true;
  const running = status.label === '运行中';
  const crashed = status.label === '已崩溃';
  const busy = busyId === instance.id;
  const disabled = !instance.enabled || !supported || busy;
  const unsupportedTip = supported ? '' : '主机 Agent 版本过旧，请升级后使用进程管理';
  const disabledTip = busy ? '操作进行中…' : !instance.enabled ? '实例已停用' : unsupportedTip;

  // 崩溃终态：给一个明确的「重新启动」入口。
  // Agent 侧的 start 会重置重启计数并清除 crashed，因此这就是恢复操作。
  // 不用 restart（它先 stop 再 start），因为崩溃时已无进程可停。
  if (crashed) {
    return (
      <>
        <Button
          size="sm"
          variant="primary"
          aria-label="重新启动并重置计数"
          title={disabled ? disabledTip : '重新启动并重置重启计数'}
          disabled={disabled}
          onClick={() => onLifecycle(instance, 'start')}
        >
          <RotateCw className="h-3.5 w-3.5" />
          重新启动
        </Button>
      </>
    );
  }

  return (
    <>
      <Button
        size="sm"
        variant="secondary"
        aria-label={running ? '停止进程' : '启动进程'}
        title={disabled ? disabledTip : running ? '停止进程' : '启动进程'}
        disabled={disabled}
        onClick={() => onLifecycle(instance, running ? 'stop' : 'start')}
      >
        {running ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
      </Button>
      <Button
        size="sm"
        variant="secondary"
        aria-label="重启进程"
        title={disabled ? disabledTip : '重启进程'}
        disabled={disabled}
        onClick={() => onLifecycle(instance, 'restart')}
      >
        <RotateCw className="h-3.5 w-3.5" />
      </Button>
    </>
  );
}

function defaultProviderId(providers) {
  return providers[0]?.id || 'opencode';
}

// buildProviderOptions 保证当前值始终可见：Provider 不在列表时补一个占位项，
// 避免 Select 显示空值却仍在提交旧 provider。
function buildProviderOptions(providerOptions, currentProviderId) {
  // 列表为空（未加载/加载失败）时不能判定为「已移除」，否则会把默认值误标成移除项。
  if (providerOptions.length === 0) {
    const id = currentProviderId || 'opencode';
    return [{ value: id, label: id === 'opencode' ? 'OpenCode' : id }];
  }
  const missing =
    currentProviderId && !providerOptions.some(option => option.value === currentProviderId);
  if (missing) {
    return [
      { value: currentProviderId, label: `${currentProviderId}（已移除）` },
      ...providerOptions,
    ];
  }
  return providerOptions;
}

// buildServerOptions 保证当前值始终可见：主机被移除时补一个占位选项，
// 避免 Select 显示空值却仍在提交旧 serverId。
function buildServerOptions(serverOptions, currentServerId) {
  const missing =
    currentServerId && !serverOptions.some(option => option.value === currentServerId);
  if (missing) {
    return [{ value: currentServerId, label: `${currentServerId}（已移除）` }, ...serverOptions];
  }
  if (serverOptions.length === 0) {
    return [{ value: '', label: '暂无可选主机' }];
  }
  return serverOptions;
}

// 与后端一致按字节长度校验（Go 用 len(password)），避免多字节密码在 UI 被误判。
function passwordByteLength(value) {
  return new TextEncoder().encode(value).length;
}

function validatePassword(value) {
  const bytes = passwordByteLength(value);
  if (bytes < 8) return '密码至少 8 字节';
  if (bytes > 72) return '密码最多 72 字节';
  return '';
}

function formatTime(value) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

const ACTION_LABELS = {
  login: '登录',
  logout: '退出登录',
  'token.issue': '签发令牌',
  'token.revoke': '吊销令牌',
  'user.create': '创建用户',
  'user.update': '修改用户',
  'user.delete': '删除用户',
  'instance.create': '创建实例',
  'instance.update': '修改实例',
  'instance.delete': '删除实例',
  'preferences.put': '保存偏好',
  'preferences.delete': '删除偏好',
};

// parseAccessAction 把后端动作拆成人类可读的结构。
// 网关转发形如 `gw GET /permission?x=1 (726ms, 2 bytes)` 或
// `gw POST denied (invalid or expired stream token)`；
// 其余为固定动作名（login / token.issue 等）。
function parseAccessAction(action) {
  const raw = action || '';
  const gateway = raw.match(/^gw\s+(\S+)(?:\s+(.*))?$/);
  if (!gateway) {
    return {
      kind: 'named',
      label: ACTION_LABELS[raw] || raw || '-',
      detail: ACTION_LABELS[raw] ? raw : '',
    };
  }
  const method = gateway[1].toUpperCase();
  const rest = (gateway[2] || '').trim();
  const denied = rest.match(/^denied(?:\s+\((.+)\))?$/);
  if (denied) {
    return {
      kind: 'gateway',
      label: '网关转发被拒',
      method,
      detail: denied[1] || '',
    };
  }
  const meta = rest.match(/^(.*?)\s*\(([^()]*)\)\s*$/);
  const path = (meta ? meta[1] : rest).trim();
  const stats = meta ? meta[2].trim() : '';
  return { kind: 'gateway', label: '网关转发', method, path, stats, detail: raw };
}

function shortId(value) {
  if (!value) return '-';
  return value.length > 12 ? `${value.slice(0, 12)}…` : value;
}

function instanceDialogDescription(editingInstance) {
  if (editingInstance) {
    return '实例指向一台已安装 Agent 的主机上的 AI Agent 本地服务端口。';
  }
  return '实例是平台共享资源，登记后可在「用户管理」中按需授权给用户。';
}

const RESULT_META = {
  ok: { label: '成功', tone: 'success' },
  denied: { label: '拒绝', tone: 'warning' },
  error: { label: '失败', tone: 'danger' },
};

function resultMeta(result) {
  return RESULT_META[result] || { label: result || '-', tone: 'neutral' };
}

// InstanceTable 是实例列表的共用表格。实例是平台资源，不归属用户，
// 用户可用哪些实例由「用户管理」里的授权勾选决定。
function InstanceTable({
  instances,
  onDetail,
  onAccess,
  onEdit,
  onDelete,
  onLifecycle,
  lifecycleBusyId,
  selectedInstanceIds,
  onToggleSelect,
  onSelectAll,
  isArmed,
}) {
  return (
    <div className="overflow-x-auto">
      <Table layout="fixed" className="min-w-[1140px]">
        <colgroup>
          <col className="w-[40px]" />
          <col className="w-[200px]" />
          <col className="w-[110px]" />
          <col className="w-[150px]" />
          <col className="w-[80px]" />
          <col className="w-[130px]" />
          <col className="w-[90px]" />
          <col className="w-[80px]" />
          <col className="w-[260px]" />
        </colgroup>
        <Table.Header variant="compact">
          <Table.Row>
            <Table.Head>
              {/* 表头全选：只针对当前可见（筛选后）的实例，
                  避免「筛选后全选」选中了看不见的实例造成误操作。 */}
              <Checkbox
                checked={
                  instances.length > 0 && instances.every(i => selectedInstanceIds.has(i.id))
                }
                onCheckedChange={checked => onSelectAll(checked === true)}
                aria-label="全选"
              />
            </Table.Head>
            <Table.Head>名称</Table.Head>
            <Table.Head>Provider</Table.Head>
            <Table.Head>主机</Table.Head>
            <Table.Head className="text-right">端口</Table.Head>
            <Table.Head>状态</Table.Head>
            <Table.Head className="text-right">内存</Table.Head>
            <Table.Head className="text-right">CPU</Table.Head>
            <Table.Head className="app-table-action">操作</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {instances.map(instance => {
            const status = instanceStatus(instance);
            return (
              <Table.Row key={instance.id}>
                <Table.Cell>
                  <Checkbox
                    checked={selectedInstanceIds.has(instance.id)}
                    onCheckedChange={() => onToggleSelect(instance.id)}
                    aria-label={`选择 ${instance.label}`}
                  />
                </Table.Cell>
                <Table.Cell>
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-kumo-strong">{instance.label}</span>
                    {!instance.enabled && <StatusBadge tone="neutral">已停用</StatusBadge>}
                  </div>
                </Table.Cell>
                <Table.Cell>{instance.providerLabel || instance.provider}</Table.Cell>
                <Table.Cell>{instance.hostName || instance.serverId}</Table.Cell>
                <Table.Cell className="text-right font-mono text-xs">{instance.port}</Table.Cell>
                <Table.Cell>
                  <div className="flex items-center gap-1.5">
                    <StatusBadge tone={status.tone} title={status.detail}>
                      <span className="inline-flex items-center gap-1">
                        {/* 过渡态（正在拉起/自动重启）加转圈：与静止的故障态区分开，
                            否则用户会以为卡住了。 */}
                        {status.spinner && <Loader size={10} className="animate-spin" />}
                        {status.label}
                      </span>
                    </StatusBadge>
                    {/* 自动重启是重要事件，不能只藏在 tooltip 里；运行中也要能看见。 */}
                    {status.restarts > 0 && status.label !== '已停止' && (
                      <span
                        className="shrink-0 text-[length:var(--fs-xxs)] text-kumo-subtle"
                        title={`已自动重启 ${status.restarts} 次`}
                      >
                        ×{status.restarts}
                      </span>
                    )}
                  </div>
                </Table.Cell>
                <Table.Cell className="text-right font-mono text-xs">
                  {formatResource(instance, 'memory')}
                </Table.Cell>
                <Table.Cell className="text-right font-mono text-xs">
                  {formatResource(instance, 'cpu')}
                </Table.Cell>
                <Table.Cell className="app-table-action">
                  <div className="flex items-center justify-center gap-1">
                    {renderLifecycleControls(instance, status, onLifecycle, lifecycleBusyId)}
                    <Button
                      size="sm"
                      variant="secondary"
                      aria-label="查看详情"
                      title="查看详情"
                      onClick={() => onDetail(instance)}
                    >
                      <Eye className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      size="sm"
                      variant="secondary"
                      aria-label="接入信息"
                      title="接入信息"
                      onClick={() => onAccess(instance)}
                    >
                      <ClipboardText className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      size="sm"
                      variant="secondary"
                      aria-label="编辑实例"
                      title="编辑实例"
                      onClick={() => onEdit(instance)}
                    >
                      <Edit className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      size="sm"
                      variant={
                        isArmed(`aiagent-instance:${instance.id}`)
                          ? 'destructive'
                          : 'secondary-destructive'
                      }
                      aria-label="删除实例"
                      title="删除实例"
                      onClick={() => onDelete(instance)}
                    >
                      <Trash className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </Table.Cell>
              </Table.Row>
            );
          })}
        </Table.Body>
      </Table>
    </div>
  );
}

// 三态渲染助手：避免在 JSX 中写嵌套三元（项目代码风格禁止）。
function renderLoadingOrEmpty(loading, isEmpty, emptyNode, contentNode) {
  if (loading) {
    return (
      <div className="space-y-2 p-4">
        {Array.from({ length: 5 }).map((_, index) => (
          <SkeletonLine key={index} className="h-11 w-full" />
        ))}
      </div>
    );
  }
  if (isEmpty) {
    return emptyNode;
  }
  return contentNode;
}

export default function AiAgentConsole() {
  const { confirmPress, isArmed } = useConfirmPress();
  const [activeTab, setActiveTab] = useState('instances');

  const [instances, setInstances] = useState([]);
  const [instancesLoading, setInstancesLoading] = useState(false);
  // 列表排序固定为「异常优先，其次按名称」：异常置顶是唯一有实际价值的排序，
  // 因此在渲染前统一处理，不提供切换入口。
  const [providers, setProviders] = useState([]);
  const [servers, setServers] = useState([]);
  const [instanceDialogOpen, setInstanceDialogOpen] = useState(false);
  const [editingInstance, setEditingInstance] = useState(null);
  const [instanceForm, setInstanceForm] = useState({
    serverId: '',
    provider: 'opencode',
    label: '',
    port: '',
    enabled: true,
  });
  const [instanceSaving, setInstanceSaving] = useState(false);
  // 正在执行生命周期操作的实例 id：用于禁用按钮，避免重复点击发出并发 start/stop。
  const [lifecycleBusyId, setLifecycleBusyId] = useState(null);
  // serverDiagnose 是「主机 × Provider」可用性诊断结果（exe 是否就绪 + 端口占用 +
  // 建议空闲端口）。在实例弹层中选择主机/Provider 后自动拉取，用于端口冲突预检
  // 与 Agent exe 缺失提示；diagnoseError 记录拉取失败原因（离线/旧 Agent 等）。
  const [serverDiagnose, setServerDiagnose] = useState(null);
  const [serverDiagnoseError, setServerDiagnoseError] = useState('');
  const [serverDiagnoseLoading, setServerDiagnoseLoading] = useState(false);
  const [accessInfo, setAccessInfo] = useState(null);
  // grantsTarget 是管理员正在配置「可用实例」的用户（用户管理里的授权弹层）。
  const [grantsTarget, setGrantsTarget] = useState(null);
  const [grantsLoading, setGrantsLoading] = useState(false);
  const [grantsSaving, setGrantsSaving] = useState(false);
  // grantedIds 是该用户当前被授权的实例 ID 集合。
  const [grantedIds, setGrantedIds] = useState(() => new Set());

  const [users, setUsers] = useState([]);
  const [usersLoading, setUsersLoading] = useState(false);
  // 用户列表可取 = 当前会话是面板管理员；非管理员创建实例时归属自动为自己。
  const [isAdmin, setIsAdmin] = useState(false);
  // 保存最近一次拉取到的用户列表，供「刚拉完就要读」的场景同步取值。
  const usersRef = useRef([]);
  // 身份是否已确定（避免非管理员反复请求必然失败的接口）。
  const adminResolvedRef = useRef(false);

  const applyUsers = useCallback(list => {
    usersRef.current = list;
    setUsers(list);
  }, []);
  const [userDialogOpen, setUserDialogOpen] = useState(false);
  const [editingUser, setEditingUser] = useState(null);
  const [userForm, setUserForm] = useState({
    username: '',
    password: '',
    displayName: '',
    disabled: false,
  });
  const [userSaving, setUserSaving] = useState(false);
  const [resetTarget, setResetTarget] = useState(null);
  const [resetPassword, setResetPassword] = useState('');
  const [resetting, setResetting] = useState(false);

  const [logs, setLogs] = useState([]);
  const [logsLoading, setLogsLoading] = useState(false);

  const providerOptions = useMemo(
    () =>
      providers.map(provider => ({
        value: provider.id,
        label: `${provider.label}${provider.verified ? '' : '（未验证）'}`,
      })),
    [providers]
  );
  // 当前所选 Provider 的允许端口区间（ADR-0006 第 1 条）：
  // [defaultPort, defaultPort + portRangeSize]，未声明区间时退化为仅默认端口。
  const selectedProviderRange = useMemo(() => {
    const provider = providers.find(item => item.id === instanceForm.provider);
    if (!provider || !provider.defaultPort) return undefined;
    const size = provider.portRangeSize > 0 ? provider.portRangeSize : 0;
    return { min: provider.defaultPort, max: provider.defaultPort + size };
  }, [providers, instanceForm.provider]);
  const selectedProviderPort = selectedProviderRange?.min;

  // 端口冲突预检：结合诊断结果与当前表单值推导「是否冲突 + 建议端口」。
  // 用户未填端口（将用默认端口）时看默认端口是否被占；已填则看该端口是否被占。
  // 返回 null 表示无冲突或诊断未就绪。判定逻辑抽到纯函数模块便于测试锁定。
  const portConflict = useMemo(
    () => resolvePortConflict(serverDiagnose, instanceForm.port, selectedProviderPort),
    [serverDiagnose, instanceForm.port, selectedProviderPort]
  );

  // 诊断提示的 exe 状态：Agent 找不到可执行文件时给出可操作提示。
  const diagnoseExecutableIssue = useMemo(() => {
    const diagnose = serverDiagnose;
    if (!diagnose) return null;
    if (diagnose.executableFound !== false) return null;
    return {
      path: diagnose.executablePath || '',
    };
  }, [serverDiagnose]);

  // 排序后的列表：异常优先，其次按名称。
  const visibleInstances = useMemo(() => sortInstances(instances), [instances]);

  const serverOptions = useMemo(
    () =>
      servers.map(server => ({
        value: server.id,
        label: `${server.name}${server.online ? '' : '（离线）'}`,
      })),
    [servers]
  );
  const instanceLabels = useMemo(
    () => new Map(instances.map(instance => [instance.id, instance.label])),
    [instances]
  );

  // instancesLoading 的 ref 镜像：自动刷新回调里需要读最新值，
  // 但不能把 instancesLoading 放进轮询依赖（会让每次 loading 翻转都重建定时器）。
  const instancesLoadingRef = useRef(false);

  // loadInstances 拉取实例列表（带探测）。
  // silent=true 用于自动刷新：不置 loading（否则列表反复闪骨架屏）、
  // 失败也不弹 toast（后台刷新失败不该打扰用户，下次轮询会自愈）。
  const loadInstances = useCallback(async (options = {}) => {
    const silent = options.silent === true;
    if (!silent) setInstancesLoading(true);
    instancesLoadingRef.current = true;
    try {
      const payload = await get('/api/aiagent/instances?probe=1');
      setInstances(payload.data || []);
    } catch (error) {
      if (!silent) toast.error(error.message || '加载实例失败');
    } finally {
      instancesLoadingRef.current = false;
      if (!silent) setInstancesLoading(false);
    }
  }, []);

  // loadUserGrants 拉取指定用户被授权的实例 ID（用户管理里的授权弹层）。
  // 重新选择用户时用请求序号丢弃过期响应，避免旧请求覆盖新用户的数据。
  const grantsSeqRef = useRef(0);
  const loadUserGrants = useCallback(async user => {
    const seq = ++grantsSeqRef.current;
    setGrantsLoading(true);
    try {
      const payload = await get(`/api/aiagent/users/${user.id}/instances`);
      if (seq !== grantsSeqRef.current) return;
      setGrantedIds(new Set(payload.data?.instanceIds || []));
    } catch (error) {
      if (seq !== grantsSeqRef.current) return;
      toast.error(error.message || '加载该用户可用实例失败');
      setGrantedIds(new Set());
    } finally {
      if (seq === grantsSeqRef.current) setGrantsLoading(false);
    }
  }, []);

  const openUserGrants = user => {
    setGrantsTarget(user);
    setGrantedIds(new Set());
    loadUserGrants(user);
  };

  const toggleGrant = instanceId => {
    setGrantedIds(prev => {
      const next = new Set(prev);
      if (next.has(instanceId)) {
        next.delete(instanceId);
      } else {
        next.add(instanceId);
      }
      return next;
    });
  };

  const saveUserGrants = async () => {
    if (!grantsTarget) return;
    setGrantsSaving(true);
    try {
      await put(`/api/aiagent/users/${grantsTarget.id}/instances`, {
        instanceIds: Array.from(grantedIds),
      });
      toast.success('可用实例已更新');
      setGrantsTarget(null);
    } catch (error) {
      toast.error(error.message || '保存可用实例失败');
    } finally {
      setGrantsSaving(false);
    }
  };

  const loadProviders = useCallback(async () => {
    try {
      const payload = await get('/api/aiagent/providers');
      setProviders(payload.data || []);
    } catch (error) {
      toast.error(error.message || '加载 Provider 失败');
    }
  }, []);

  const loadServers = useCallback(async () => {
    try {
      const payload = await get('/api/aiagent/servers');
      setServers(payload.data || []);
    } catch (error) {
      toast.error(error.message || '加载主机列表失败');
    }
  }, []);

  // resolveIdentity 统一「拉用户列表 + 判定管理员身份」：成功即管理员，
  // 401/403 即非管理员；其余错误返回 null（身份仍未知）。
  const resolveIdentity = useCallback(async () => {
    try {
      const payload = await get('/api/aiagent/users');
      const list = payload.data || [];
      applyUsers(list);
      setIsAdmin(true);
      adminResolvedRef.current = true;
      return { isAdmin: true, users: list };
    } catch (error) {
      if (error?.status === 401 || error?.status === 403) {
        applyUsers([]);
        setIsAdmin(false);
        adminResolvedRef.current = true;
        return { isAdmin: false, users: [] };
      }
      return { isAdmin: null, users: [], error };
    }
  }, [applyUsers]);

  const loadUsers = useCallback(async () => {
    setUsersLoading(true);
    try {
      const resolved = await resolveIdentity();
      if (resolved.isAdmin === null) {
        // 网络异常 / 5xx：清空陈旧列表并提示，避免显示过期数据。
        applyUsers([]);
        toast.error(resolved.error?.message || '加载用户失败');
      }
      return resolved.users;
    } finally {
      setUsersLoading(false);
    }
  }, [resolveIdentity, applyUsers]);

  // logInstanceFilter 是访问日志的实例维度过滤。
  // 排查单个实例的问题时，全量日志里翻找效率极低。
  const [logInstanceFilter, setLogInstanceFilter] = useState('');

  const loadLogs = useCallback(async () => {
    setLogsLoading(true);
    try {
      const query = logInstanceFilter ? `&instanceId=${encodeURIComponent(logInstanceFilter)}` : '';
      const payload = await get(`/api/aiagent/logs?limit=120${query}`);
      setLogs(payload.data || []);
    } catch (error) {
      toast.error(error.message || '加载日志失败');
      setLogs([]);
    } finally {
      setLogsLoading(false);
    }
  }, [logInstanceFilter]);

  useEffect(() => {
    loadInstances();
    loadProviders();
    loadServers();
  }, [loadInstances, loadProviders, loadServers]);

  // 实例列表自动刷新：进程会被 supervisor 自动拉起、也可能崩溃，
  // 用户盯着面板时应能看到状态变化。页面隐藏时 hook 会自动暂停。
  const [autoRefresh, setAutoRefresh] = useState(readAutoRefreshPreference);
  const autoRefreshRef = useRef(autoRefresh);
  autoRefreshRef.current = autoRefresh;

  useVisiblePolling(
    () => {
      // 三个条件：用户开启、页面可见、无请求在途。
      // 最后一条避免与手动刷新/生命周期操作叠加成探测风暴。
      if (
        !shouldAutoRefresh({
          enabled: autoRefreshRef.current,
          loading: instancesLoadingRef.current,
        })
      )
        return;
      // 静默刷新：不置 loading，否则列表会反复闪骨架屏。
      void loadInstances({ silent: true });
    },
    AUTO_REFRESH_INTERVAL_MS,
    [activeTab]
  );

  // 挂载即解析身份：实例页的标签/归属列/新增按钮与用户管理入口都依赖是否管理员。
  const identityResolvedRef = useRef(false);
  useEffect(() => {
    if (identityResolvedRef.current) return;
    identityResolvedRef.current = true;
    resolveIdentity();
  }, [resolveIdentity]);

  // 用户列表首次进入 Tab 加载一次；已确认非管理员不再请求。刷新交给头部按钮。
  const usersTabLoadedRef = useRef(false);
  useEffect(() => {
    if (activeTab !== 'users') return;
    if (usersTabLoadedRef.current) return;
    if (adminResolvedRef.current && !isAdmin) return;
    usersTabLoadedRef.current = true;
    loadUsers();
  }, [activeTab, isAdmin, loadUsers]);

  // 日志首次进入加载；之后切回沿用已加载数据，刷新交给头部按钮。
  // logsFilterRef 记录上次拉取用的筛选值，用于区分「切回 Tab」与「改筛选」。
  const logsTabLoadedRef = useRef(false);
  const logsFilterRef = useRef('');
  useEffect(() => {
    if (activeTab !== 'logs') return;
    // 筛选条件变化时重新拉取（此时不再受「首次加载」闸门限制）。
    if (logsTabLoadedRef.current && logInstanceFilter === logsFilterRef.current) return;
    logsFilterRef.current = logInstanceFilter;
    logsTabLoadedRef.current = true;
    loadLogs();
  }, [activeTab, loadLogs, logInstanceFilter]);

  // openCreateInstance 新增实例。实例是平台资源，不归属用户；
  // 用户可用哪些实例由「用户管理」里的授权勾选决定。
  const openCreateInstance = () => {
    setEditingInstance(null);
    setInstanceForm({
      serverId: '',
      provider: defaultProviderId(providers),
      label: '',
      port: '',
      enabled: true,
    });
    setInstanceDialogOpen(true);
    if (serverOptions.length === 0) {
      toast.warning('当前没有可用主机，请联系管理员先纳管一台机器');
    }
  };

  const openEditInstance = instance => {
    setEditingInstance(instance);
    setInstanceForm({
      serverId: instance.serverId,
      provider: instance.provider,
      label: instance.label,
      port: instance.port ? String(instance.port) : '',
      enabled: instance.enabled,
    });
    setInstanceDialogOpen(true);
  };

  // 实例弹层中选择主机 + Provider 后自动拉取可用性诊断：
  // exe 是否就绪、端口区间占用、建议空闲端口。用于创建/编辑时预检端口冲突，
  // 避免提交后才发现默认端口被占（ADR-0006 端口不抢占语义）。
  useEffect(() => {
    if (!instanceDialogOpen) return;
    const serverId = instanceForm.serverId;
    const provider = instanceForm.provider;
    setServerDiagnose(null);
    setServerDiagnoseError('');
    if (!serverId || !provider) {
      setServerDiagnoseLoading(false);
      return;
    }
    let cancelled = false;
    setServerDiagnoseLoading(true);
    (async () => {
      try {
        const payload = await get(
          `/api/aiagent/servers/${encodeURIComponent(serverId)}/diagnose?provider=${encodeURIComponent(provider)}`
        );
        if (cancelled) return;
        setServerDiagnose(payload.data || null);
      } catch (error) {
        if (cancelled) return;
        // 主机离线/旧 Agent/无 runtime 都会让诊断失败：
        // 记下原因展示，但不在保存路径上硬拦截（后端仍会做最终校验）。
        setServerDiagnoseError(error.message || '无法诊断主机可用性');
      } finally {
        if (!cancelled) setServerDiagnoseLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [instanceDialogOpen, instanceForm.serverId, instanceForm.provider, get]);

  const saveInstance = async () => {
    const label = instanceForm.label.trim();
    if (!instanceForm.serverId) {
      toast.warning('请选择主机');
      return;
    }
    // 主机已被移除时不允许直接提交旧值，要求重新选择。
    // 所选主机不可用（列表已加载但无此项，或列表未加载且当前值无法解析）时要求重选。
    const hostKnown = serverOptions.some(option => option.value === instanceForm.serverId);
    if (!hostKnown && serverOptions.length > 0) {
      toast.warning('所选主机已不可用，请重新选择主机');
      return;
    }
    // Provider 已不可用时拦截：仅在成功加载过 Provider 列表、且当前值确实不在其中
    // 时判定为过期（列表为空时允许提交，交由后端校验）。
    if (
      providerOptions.length > 0 &&
      !providerOptions.some(option => option.value === instanceForm.provider)
    ) {
      toast.warning('所选 Provider 已不可用，请重新选择');
      return;
    }
    if (!label) {
      toast.warning('请填写实例名称');
      return;
    }
    const rawPort = instanceForm.port.trim();
    let port;
    if (rawPort === '') {
      port = undefined;
    } else if (/^\d+$/.test(rawPort)) {
      port = Number(rawPort);
    } else {
      port = Number.NaN;
    }
    if (port !== undefined && (!Number.isInteger(port) || port < 1 || port > 65535)) {
      toast.warning('端口必须为 1-65535 之间的整数');
      return;
    }
    // 端口必须落在 Provider 的允许区间内（ADR-0006 第 1 条）。
    // 区分「列表未加载」与「所选 Provider 已不在列表中」两种情形。
    if (port !== undefined && selectedProviderRange === undefined) {
      if (providerOptions.length === 0) {
        toast.warning('Provider 列表未加载，无法校验端口，请先刷新后再试');
      } else {
        toast.warning('所选 Provider 已不可用，请重新选择后再填写端口');
      }
      return;
    }
    if (
      port !== undefined &&
      (port < selectedProviderRange.min || port > selectedProviderRange.max)
    ) {
      toast.warning(
        `端口需在 ${selectedProviderRange.min}-${selectedProviderRange.max} 之间（该 Provider 的允许区间）`
      );
      return;
    }
    if (serverOptions.length === 0) {
      toast.warning('当前没有可用主机，请先在「主机」中纳管一台机器');
      return;
    }
    setInstanceSaving(true);
    try {
      const body = {
        serverId: instanceForm.serverId,
        provider: instanceForm.provider,
        label,
        enabled: instanceForm.enabled,
      };
      if (port !== undefined) body.port = port;
      if (editingInstance) {
        await put(`/api/aiagent/instances/${editingInstance.id}`, body);
        toast.success('实例已更新');
      } else {
        await post('/api/aiagent/instances', body);
        toast.success('实例已添加');
      }
      setInstanceDialogOpen(false);
      await loadInstances();
    } catch (error) {
      toast.error(error.message || '保存实例失败');
    } finally {
      setInstanceSaving(false);
    }
  };

  const removeInstance = async instance => {
    const ok = await confirmPress(
      `aiagent-instance:${instance.id}`,
      `删除实例「${instance.label}」`
    );
    if (!ok) return;
    try {
      await del(`/api/aiagent/instances/${instance.id}`);
      toast.success('实例已删除');
      await loadInstances();
    } catch (error) {
      toast.error(error.message || '删除实例失败');
    }
  };

  // runLifecycleAction 触发一次进程生命周期操作（ADR-0006）。
  // 后端会同步落期望状态并立即执行，完成后回读实际状态。
  const runLifecycleAction = async (instance, action) => {
    const verb = { start: '启动', stop: '停止', restart: '重启' }[action] || action;
    setLifecycleBusyId(instance.id);
    try {
      await post(`/api/aiagent/instances/${instance.id}/lifecycle`, { action });
      toast.success(`已${verb}「${instance.label}」`);
      await loadInstances();
    } catch (error) {
      toast.error(error.message || `${verb}失败`);
    } finally {
      setLifecycleBusyId(null);
    }
  };

  // 批量操作：选中的实例 ID 集合。
  // 用 Set 便于增删，且判定「是否全选」时是 O(1)。
  const [selectedInstanceIds, setSelectedInstanceIds] = useState(() => new Set());
  const [batchBusy, setBatchBusy] = useState(false);

  const toggleInstanceSelection = id => {
    setSelectedInstanceIds(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  // 清理已不存在的选中项：实例被删除后不应留在选中集合里，
  // 否则批量操作会去操作一个不存在的 ID。
  useEffect(() => {
    if (selectedInstanceIds.size === 0) return;
    const existing = new Set(instances.map(instance => instance.id));
    let changed = false;
    for (const id of selectedInstanceIds) {
      if (!existing.has(id)) {
        changed = true;
        break;
      }
    }
    if (!changed) return;
    setSelectedInstanceIds(prev => {
      const next = new Set();
      for (const id of prev) if (existing.has(id)) next.add(id);
      return next;
    });
  }, [instances, selectedInstanceIds]);

  // runBatchAction 批量启动/停止/重启。
  // 后端逐个执行并返回每项结果，这里据实汇报成功/失败数量。
  const runBatchAction = async action => {
    const ids = Array.from(selectedInstanceIds);
    if (ids.length === 0) return;
    const verb = { start: '启动', stop: '停止', restart: '重启' }[action] || action;

    setBatchBusy(true);
    try {
      const payload = await post('/api/aiagent/instances/batch', {
        action,
        instanceIds: ids,
      });
      const data = payload.data || {};
      const failed = data.failed || 0;
      if (failed === 0) {
        toast.success(`已批量${verb} ${data.succeeded || ids.length} 个实例`);
      } else {
        // 部分失败时明确指出数量，让用户知道需要进一步处理。
        toast.warning(`${verb}完成：成功 ${data.succeeded || 0} 个，失败 ${failed} 个`);
      }
      setSelectedInstanceIds(new Set());
      await loadInstances();
    } catch (error) {
      toast.error(error.message || `批量${verb}失败`);
    } finally {
      setBatchBusy(false);
    }
  };

  // detailInstance 是详情弹层当前展示的实例（快照）。
  // 存快照而非 id：弹层打开期间列表会被自动刷新替换，存 id 会导致内容跳动。
  const [detailInstance, setDetailInstance] = useState(null);

  const showInstanceDetail = instance => {
    setDetailInstance(instance);
  };

  const showAccessInfo = async instance => {
    try {
      const payload = await get(`/api/aiagent/instances/${instance.id}/access-info`);
      setAccessInfo(payload.data || null);
    } catch (error) {
      toast.error(error.message || '获取接入信息失败');
    }
  };

  const openCreateUser = () => {
    setEditingUser(null);
    setUserForm({ username: '', password: '', displayName: '', disabled: false });
    setUserDialogOpen(true);
  };

  const openEditUser = user => {
    setEditingUser(user);
    setUserForm({
      username: user.username,
      password: '',
      displayName: user.displayName || '',
      disabled: user.disabled,
    });
    setUserDialogOpen(true);
  };

  const saveUser = async () => {
    if (editingUser) {
      setUserSaving(true);
      try {
        await put(`/api/aiagent/users/${editingUser.id}`, {
          displayName: userForm.displayName,
          disabled: userForm.disabled,
        });
        toast.success('用户已更新');
        setUserDialogOpen(false);
        await loadUsers();
      } catch (error) {
        toast.error(error.message || '更新用户失败');
      } finally {
        setUserSaving(false);
      }
      return;
    }
    const username = userForm.username.trim();
    if (!/^[A-Za-z0-9._-]{3,32}$/.test(username)) {
      toast.warning('用户名需为 3-32 位字母数字与 - _ .');
      return;
    }
    const passwordError = validatePassword(userForm.password);
    if (passwordError) {
      toast.warning(passwordError);
      return;
    }
    setUserSaving(true);
    try {
      await post('/api/aiagent/users', {
        username,
        password: userForm.password,
        displayName: userForm.displayName,
      });
      toast.success('用户已创建');
      setUserDialogOpen(false);
      await loadUsers();
    } catch (error) {
      toast.error(error.message || '创建用户失败');
    } finally {
      setUserSaving(false);
    }
  };

  const resetPasswordFor = async () => {
    if (!resetTarget) return;
    const resetError = validatePassword(resetPassword);
    if (resetError) {
      toast.warning(resetError);
      return;
    }
    setResetting(true);
    try {
      await post(`/api/aiagent/users/${resetTarget.id}/reset-password`, {
        password: resetPassword,
      });
      toast.success('密码已重置，该用户全部令牌已吊销');
      setResetTarget(null);
      setResetPassword('');
    } catch (error) {
      toast.error(error.message || '重置密码失败');
    } finally {
      setResetting(false);
    }
  };

  const removeUser = async user => {
    const ok = await confirmPress(
      `aiagent-user:${user.id}`,
      `删除用户「${user.username}」（其令牌与实例一并删除）`
    );
    if (!ok) return;
    try {
      await del(`/api/aiagent/users/${user.id}`);
      toast.success('用户已删除');
      await loadUsers();
    } catch (error) {
      toast.error(error.message || '删除用户失败');
    }
  };

  return (
    <PageStack viewport>
      <div
        className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}
      >
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={[
            {
              value: 'instances',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Plug className="w-3.5 h-3.5" />
                  AI Agent 实例
                </span>
              ),
            },
            {
              value: 'users',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Users className="w-3.5 h-3.5" />
                  用户管理
                </span>
              ),
            },
            {
              value: 'logs',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Activity className="w-3.5 h-3.5" />
                  访问日志
                </span>
              ),
            },
          ]}
        />
      </div>

      {activeTab === 'instances' && (
        <SectionCard
          title="AI Agent 实例"
          description="登记各主机上的 AI Agent 服务；用户在「用户管理」中按需授权可用实例。"
          bodyPadding="none"
          actions={
            <>
              <label
                className="inline-flex items-center gap-1.5 text-xs text-kumo-subtle cursor-pointer select-none"
                title={`每 ${AUTO_REFRESH_INTERVAL_MS / 1000} 秒自动刷新一次（页面隐藏时暂停）`}
              >
                <Switch
                  checked={autoRefresh}
                  onCheckedChange={checked => {
                    setAutoRefresh(checked);
                    writeAutoRefreshPreference(checked);
                  }}
                />
                自动刷新
              </label>
              <Button size="sm" variant="secondary" onClick={() => loadInstances()}>
                <RefreshCw className="h-4 w-4" />
                刷新
              </Button>
              <Button size="sm" variant="primary" onClick={openCreateInstance}>
                <Plus className="h-4 w-4" />
                添加实例
              </Button>
            </>
          }
        >
          {renderLoadingOrEmpty(
            instancesLoading,
            instances.length === 0,
            <Empty
              size="sm"
              className="rounded-none border-0 bg-transparent"
              title="还没有实例"
              description="添加一台已安装 Agent 的主机上的 AI Agent 服务"
            />,
            <>
              {/* 批量工具条：仅在有选中项时出现，避免平时占用视觉空间。 */}
              {selectedInstanceIds.size > 0 && (
                <div className="flex flex-wrap items-center gap-2 border-b border-kumo-line bg-kumo-elevated px-4 py-2">
                  <span className="text-xs text-kumo-subtle">
                    已选 {selectedInstanceIds.size} 个
                  </span>
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={batchBusy}
                    onClick={() => runBatchAction('start')}
                  >
                    <Play className="h-3.5 w-3.5" />
                    批量启动
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={batchBusy}
                    onClick={() => runBatchAction('restart')}
                  >
                    <RotateCw className="h-3.5 w-3.5" />
                    批量重启
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary-destructive"
                    disabled={batchBusy}
                    onClick={() => runBatchAction('stop')}
                  >
                    <Pause className="h-3.5 w-3.5" />
                    批量停止
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    className="ml-auto"
                    disabled={batchBusy}
                    onClick={() => setSelectedInstanceIds(new Set())}
                  >
                    取消选择
                  </Button>
                </div>
              )}
              <InstanceTable
                instances={visibleInstances}
                onDetail={showInstanceDetail}
                onAccess={showAccessInfo}
                onEdit={openEditInstance}
                onDelete={removeInstance}
                onLifecycle={runLifecycleAction}
                lifecycleBusyId={lifecycleBusyId}
                selectedInstanceIds={selectedInstanceIds}
                onToggleSelect={toggleInstanceSelection}
                onSelectAll={checked =>
                  setSelectedInstanceIds(
                    checked ? new Set(visibleInstances.map(i => i.id)) : new Set()
                  )
                }
                isArmed={isArmed}
              />
            </>
          )}
        </SectionCard>
      )}

      {activeTab === 'users' && (
        <SectionCard
          title="用户管理"
          description="创建用户、设置密码，并按用户授权其可用的 AI Agent 实例。"
          bodyPadding="none"
          actions={
            isAdmin ? (
              <>
                <Button size="sm" variant="secondary" onClick={loadUsers}>
                  <RefreshCw className="h-4 w-4" />
                  刷新
                </Button>
                <Button size="sm" variant="primary" onClick={openCreateUser}>
                  <Plus className="h-4 w-4" />
                  新建用户
                </Button>
              </>
            ) : null
          }
        >
          {renderLoadingOrEmpty(
            usersLoading,
            users.length === 0,
            <Empty
              size="sm"
              className="rounded-none border-0 bg-transparent"
              title="暂无用户或无权查看"
              description="用户管理仅对面板管理员开放"
            />,
            <div className="overflow-x-auto">
              <Table layout="fixed" className="min-w-[860px]">
                <colgroup>
                  <col className="w-[170px]" />
                  <col className="w-[160px]" />
                  <col className="w-[110px]" />
                  <col className="w-[160px]" />
                  <col className="w-[210px]" />
                </colgroup>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head>用户名</Table.Head>
                    <Table.Head>显示名</Table.Head>
                    <Table.Head>状态</Table.Head>
                    <Table.Head>最后登录</Table.Head>
                    <Table.Head className="app-table-action">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {users.map(user => (
                    <Table.Row key={user.id}>
                      <Table.Cell className="font-medium text-kumo-strong">
                        {user.username}
                      </Table.Cell>
                      <Table.Cell>{user.displayName || '-'}</Table.Cell>
                      <Table.Cell>
                        <StatusBadge tone={user.disabled ? 'neutral' : 'success'}>
                          {user.disabled ? '已禁用' : '正常'}
                        </StatusBadge>
                      </Table.Cell>
                      <Table.Cell className="text-xs text-kumo-subtle">
                        {formatTime(user.lastLoginAt)}
                      </Table.Cell>
                      <Table.Cell className="app-table-action">
                        <div className="flex items-center justify-center gap-1">
                          <Button
                            size="sm"
                            variant="secondary"
                            aria-label="可用实例"
                            title="可用实例"
                            onClick={() => openUserGrants(user)}
                          >
                            <Plug className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            size="sm"
                            variant="secondary"
                            aria-label="重置密码"
                            title="重置密码"
                            onClick={() => {
                              setResetTarget(user);
                              setResetPassword('');
                            }}
                          >
                            <Lock className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            size="sm"
                            variant="secondary"
                            aria-label="编辑用户"
                            title="编辑用户"
                            onClick={() => openEditUser(user)}
                          >
                            <Edit className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            size="sm"
                            variant={
                              isArmed(`aiagent-user:${user.id}`)
                                ? 'destructive'
                                : 'secondary-destructive'
                            }
                            aria-label="删除用户"
                            title="删除用户"
                            onClick={() => removeUser(user)}
                          >
                            <Trash className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </div>
          )}
        </SectionCard>
      )}

      {activeTab === 'logs' && (
        <SectionCard
          title="访问日志"
          description="登录、令牌、实例与网关转发的审计记录。"
          bodyPadding="none"
          actions={
            <>
              <div className="w-[180px]">
                <Select
                  size="sm"
                  value={logInstanceFilter}
                  onValueChange={setLogInstanceFilter}
                  items={[
                    { value: '', label: '全部实例' },
                    ...instances.map(instance => ({
                      value: instance.id,
                      label: instance.label,
                    })),
                  ]}
                />
              </div>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => {
                  loadLogs();
                }}
              >
                <RefreshCw className="h-4 w-4" />
                刷新
              </Button>
            </>
          }
        >
          {renderLoadingOrEmpty(
            logsLoading,
            logs.length === 0,
            <Empty
              size="sm"
              className="rounded-none border-0 bg-transparent"
              title="暂无日志"
              description="有登录或转发行为后会在这里出现记录"
            />,
            <div className="overflow-x-auto">
              <Table layout="fixed" className="min-w-[860px]">
                <colgroup>
                  <col className="w-[160px]" />
                  <col className="w-[340px]" />
                  <col className="w-[90px]" />
                  <col className="w-[150px]" />
                  <col className="w-[120px]" />
                </colgroup>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head>时间</Table.Head>
                    <Table.Head>动作</Table.Head>
                    <Table.Head className="text-center">结果</Table.Head>
                    <Table.Head>实例</Table.Head>
                    <Table.Head>IP</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {logs.map(entry => {
                    const action = parseAccessAction(entry.action);
                    const result = resultMeta(entry.result);
                    const instanceLabel = instanceLabels.get(entry.instanceId);
                    return (
                      <Table.Row key={entry.id}>
                        <Table.Cell
                          className="truncate whitespace-nowrap text-xs text-kumo-subtle"
                          title={formatTime(entry.createdAt)}
                        >
                          {formatTime(entry.createdAt)}
                        </Table.Cell>
                        <Table.Cell className="min-w-0">
                          <div className="flex min-w-0 flex-col gap-0.5" title={entry.action}>
                            <span className="flex min-w-0 items-center gap-1.5">
                              <span className="shrink-0 text-xs font-medium text-kumo-strong">
                                {action.label}
                              </span>
                              {action.kind === 'gateway' && action.method && (
                                <code className="shrink-0 rounded bg-kumo-surface-2 px-1 font-mono text-[10px] text-kumo-subtle">
                                  {action.method}
                                </code>
                              )}
                            </span>
                            {action.kind === 'gateway' && (action.path || action.detail) && (
                              <span className="truncate font-mono text-[11px] text-kumo-subtle">
                                {action.path || action.detail}
                                {action.stats ? ` · ${action.stats}` : ''}
                              </span>
                            )}
                          </div>
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <StatusBadge tone={result.tone} title={entry.error || undefined}>
                            {result.label}
                          </StatusBadge>
                        </Table.Cell>
                        <Table.Cell
                          className="truncate text-xs text-kumo-strong"
                          title={entry.instanceId || ''}
                        >
                          {instanceLabel || shortId(entry.instanceId)}
                        </Table.Cell>
                        <Table.Cell
                          className="truncate font-mono text-[11px] text-kumo-subtle"
                          title={entry.ip || ''}
                        >
                          {entry.ip || '-'}
                        </Table.Cell>
                      </Table.Row>
                    );
                  })}
                </Table.Body>
              </Table>
            </div>
          )}
        </SectionCard>
      )}

      <LayerDialog.Root open={instanceDialogOpen} onOpenChange={setInstanceDialogOpen}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>{editingInstance ? '编辑实例' : '添加实例'}</LayerDialog.Title>
          <LayerDialog.Description>
            {instanceDialogDescription(editingInstance)}
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="flex flex-col gap-3">
              <label className="flex flex-col gap-1">
                <span className="text-sm text-kumo-subtle">主机</span>
                <Select
                  alignItemWithTrigger
                  size="sm"
                  value={instanceForm.serverId}
                  onValueChange={value => setInstanceForm(prev => ({ ...prev, serverId: value }))}
                  items={buildServerOptions(serverOptions, instanceForm.serverId)}
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-sm text-kumo-subtle">Provider</span>
                <Select
                  alignItemWithTrigger
                  size="sm"
                  value={instanceForm.provider}
                  onValueChange={value =>
                    // 切换 Provider 时清空端口：各 Provider 默认端口不同，沿用旧端口会被后端拒绝。
                    setInstanceForm(prev => ({ ...prev, provider: value, port: '' }))
                  }
                  items={buildProviderOptions(providerOptions, instanceForm.provider)}
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-sm text-kumo-subtle">实例名称</span>
                <Input
                  size="sm"
                  value={instanceForm.label}
                  placeholder="例如：家里台机"
                  onChange={event =>
                    setInstanceForm(prev => ({ ...prev, label: event.target.value }))
                  }
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-sm text-kumo-subtle">
                  端口
                  {selectedProviderRange
                    ? `（允许 ${selectedProviderRange.min}-${selectedProviderRange.max}，留空用默认 ${selectedProviderRange.min}）`
                    : ''}
                </span>
                <Input
                  size="sm"
                  value={instanceForm.port}
                  inputMode="numeric"
                  placeholder={selectedProviderPort ? String(selectedProviderPort) : '默认端口'}
                  onChange={event =>
                    setInstanceForm(prev => ({ ...prev, port: event.target.value }))
                  }
                />
                {serverDiagnoseLoading && (
                  <span className="text-[11px] text-kumo-subtle">
                    <Loader size={10} className="animate-spin" />
                    正在检测端口占用…
                  </span>
                )}
                {serverDiagnoseError && (
                  <span className="text-[11px] text-kumo-subtle">
                    无法检测端口占用（{serverDiagnoseError}），保存时后端会再做校验
                  </span>
                )}
                {diagnoseExecutableIssue && (
                  <span className="text-[11px] text-kumo-warning">
                    主机未找到 {instanceForm.provider} 可执行文件
                    {diagnoseExecutableIssue.path ? `（${diagnoseExecutableIssue.path}）` : ''}
                    ，请先在主机上安装，或在主机 Agent 环境配置对应
                    API_MONITOR_AIAGENT_*_BIN 变量
                  </span>
                )}
                {portConflict && (
                  <span className="flex flex-wrap items-center gap-1.5 text-[11px] text-kumo-warning">
                    端口 {portConflict.occupied} 已被占用
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() =>
                        setInstanceForm(prev => ({
                          ...prev,
                          port: String(portConflict.suggested),
                        }))
                      }
                      className="!h-6 !px-2 !text-[11px] !text-brand hover:!text-kumo-strong"
                    >
                      使用空闲端口 {portConflict.suggested}
                    </Button>
                  </span>
                )}
              </label>
              <label className="flex items-center gap-2 text-sm text-kumo-subtle">
                <Switch
                  checked={instanceForm.enabled}
                  onCheckedChange={checked =>
                    setInstanceForm(prev => ({ ...prev, enabled: checked }))
                  }
                />
                启用该实例
              </label>
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary
              type="button"
              onClick={saveInstance}
              loading={instanceSaving}
            >
              保存
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      <LayerDialog.Root
        open={Boolean(grantsTarget)}
        onOpenChange={open => {
          if (!open) setGrantsTarget(null);
        }}
      >
        <LayerDialog.Content size="lg">
          <LayerDialog.Title>{grantsTarget?.username} 的可用实例</LayerDialog.Title>
          <LayerDialog.Description>
            勾选该用户可以使用的 AI Agent 实例；默认不授权任何实例。
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="flex flex-col gap-3">
              <div className="flex items-center justify-between gap-2">
                <Button size="sm" variant="secondary" onClick={() => loadUserGrants(grantsTarget)}>
                  <RefreshCw className="h-4 w-4" />
                  刷新
                </Button>
                <span className="text-xs text-kumo-subtle">
                  已选 {grantedIds.size} / {instances.length}
                </span>
              </div>
              {renderLoadingOrEmpty(
                grantsLoading,
                instances.length === 0,
                <Empty
                  size="sm"
                  className="rounded-none border-0 bg-transparent"
                  title="还没有实例"
                  description="请先在「AI Agent 实例」中登记实例"
                />,
                <div className="flex max-h-[50vh] flex-col gap-1 overflow-y-auto">
                  {instances.map(instance => (
                    <label
                      key={instance.id}
                      className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-kumo-fill/40"
                    >
                      <Checkbox
                        checked={grantedIds.has(instance.id)}
                        onCheckedChange={() => toggleGrant(instance.id)}
                      />
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate font-medium text-kumo-strong">
                          {instance.label}
                        </span>
                        <span className="truncate text-xs text-kumo-subtle">
                          {instance.providerLabel || instance.provider} ·{' '}
                          {instance.hostName || instance.serverId} · 端口 {instance.port}
                          {instance.enabled ? '' : ' · 已停用'}
                        </span>
                      </span>
                    </label>
                  ))}
                </div>
              )}
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary
              type="button"
              onClick={saveUserGrants}
              loading={grantsSaving}
            >
              保存
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      <LayerDialog.Root open={userDialogOpen} onOpenChange={setUserDialogOpen}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>
            {editingUser ? `编辑用户 ${editingUser.username}` : '新建用户'}
          </LayerDialog.Title>
          <LayerDialog.Description>
            用户用于客户端登录，与面板管理员账号相互独立。
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="flex flex-col gap-3">
              <label className="flex flex-col gap-1">
                <span className="text-sm text-kumo-subtle">用户名</span>
                <Input
                  size="sm"
                  value={userForm.username}
                  disabled={Boolean(editingUser)}
                  placeholder="3-32 位字母数字与 - _ ."
                  onChange={event =>
                    setUserForm(prev => ({ ...prev, username: event.target.value }))
                  }
                />
              </label>
              {!editingUser && (
                <label className="flex flex-col gap-1">
                  <span className="text-sm text-kumo-subtle">密码（至少 8 位）</span>
                  <Input
                    size="sm"
                    type="password"
                    value={userForm.password}
                    onChange={event =>
                      setUserForm(prev => ({ ...prev, password: event.target.value }))
                    }
                  />
                </label>
              )}
              <label className="flex flex-col gap-1">
                <span className="text-sm text-kumo-subtle">显示名</span>
                <Input
                  size="sm"
                  value={userForm.displayName}
                  onChange={event =>
                    setUserForm(prev => ({ ...prev, displayName: event.target.value }))
                  }
                />
              </label>
              {editingUser && (
                <label className="flex items-center gap-2 text-sm text-kumo-subtle">
                  <Switch
                    checked={userForm.disabled}
                    onCheckedChange={checked =>
                      setUserForm(prev => ({ ...prev, disabled: checked }))
                    }
                  />
                  禁用该用户（其全部令牌立即失效）
                </label>
              )}
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary type="button" onClick={saveUser} loading={userSaving}>
              保存
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      <LayerDialog.Root
        open={Boolean(accessInfo)}
        onOpenChange={open => {
          if (!open) setAccessInfo(null);
        }}
      >
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>接入信息</LayerDialog.Title>
          <LayerDialog.Description>
            客户端（如 OpenCode UI）填写面板域名后，使用该实例的网关路径访问。
          </LayerDialog.Description>
          <LayerDialog.Body>
            {accessInfo && (
              <div className="flex flex-col gap-3 text-sm">
                <div className="flex flex-col gap-1">
                  <span className="text-kumo-subtle">网关路径</span>
                  <ClipboardTextField
                    size="sm"
                    text={accessInfo.gatewayPath}
                    tooltip={{ text: '复制', copiedText: '已复制' }}
                  />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div className="flex flex-col gap-1">
                    <span className="text-kumo-subtle">Provider</span>
                    <span className="text-kumo-strong">{accessInfo.providerLabel}</span>
                  </div>
                  <div className="flex flex-col gap-1">
                    <span className="text-kumo-subtle">目标端口</span>
                    <span className="font-mono text-kumo-strong">{accessInfo.port}</span>
                  </div>
                  <div className="flex flex-col gap-1">
                    <span className="text-kumo-subtle">主机</span>
                    <span className="text-kumo-strong">{accessInfo.serverId}</span>
                  </div>
                  <div className="flex flex-col gap-1">
                    <span className="text-kumo-subtle">流式协议</span>
                    <span className="text-kumo-strong">{accessInfo.streaming || '-'}</span>
                  </div>
                </div>
              </div>
            )}
          </LayerDialog.Body>
        </LayerDialog.Content>
      </LayerDialog.Root>

      {/* 实例详情：把探测明细、托管状态与接入信息集中展示，便于对照排查。
          数据来自列表快照（打开时定格），因此弹层内容不会随自动刷新跳动。 */}
      <LayerDialog.Root
        open={Boolean(detailInstance)}
        onOpenChange={open => {
          if (!open) setDetailInstance(null);
        }}
      >
        <LayerDialog.Content size="md">
          <LayerDialog.Title>{detailInstance?.label || '实例详情'}</LayerDialog.Title>
          <LayerDialog.Description>
            进程状态由主机 Agent 上报；探测结果与托管状态不一致时，以托管状态为准。
          </LayerDialog.Description>
          <LayerDialog.Body>
            {detailInstance && (
              <div className="flex flex-col gap-4">
                {buildInstanceDetailGroups(detailInstance).map(group => (
                  <div key={group.title} className="flex flex-col gap-2">
                    <div className="text-xs font-medium text-kumo-subtle">{group.title}</div>
                    <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
                      {group.rows.map(row => (
                        <div key={row.label} className="flex min-w-0 flex-col gap-0.5">
                          <span className="text-xs text-kumo-subtle">{row.label}</span>
                          <span className="font-mono text-kumo-strong break-all">{row.value}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </LayerDialog.Body>
        </LayerDialog.Content>
      </LayerDialog.Root>

      <LayerDialog.Root
        open={Boolean(resetTarget)}
        onOpenChange={open => {
          if (!open) setResetTarget(null);
        }}
      >
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>重置密码 {resetTarget?.username}</LayerDialog.Title>
          <LayerDialog.Description>
            重置后该用户已签发的全部令牌会立即失效，客户端需要重新登录。
          </LayerDialog.Description>
          <LayerDialog.Body>
            <label className="flex flex-col gap-1">
              <span className="text-sm text-kumo-subtle">新密码（至少 8 位）</span>
              <Input
                size="sm"
                type="password"
                value={resetPassword}
                onChange={event => setResetPassword(event.target.value)}
              />
            </label>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary
              type="button"
              onClick={resetPasswordFor}
              loading={resetting}
            >
              重置
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </PageStack>
  );
}
