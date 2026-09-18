import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from '../../modules/toast.js';
import { del, get, post, put } from '../../modules/apiClient.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { ClipboardText as ClipboardTextField, Empty, LayerCard, Tabs } from '@cloudflare/kumo';
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
  Lock,
  Plug,
  Plus,
  RefreshCw,
  Trash,
  Users,
} from '../../components/Icons.jsx';

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

const STATUS_TONE = {
  online: 'success',
  process_stopped: 'warning',
  host_offline: 'neutral',
  unknown: 'neutral',
};

function instanceStatus(instance) {
  const status = instance.status || {};
  if (status.online) {
    return {
      label: '运行中',
      tone: STATUS_TONE.online,
      detail: status.pid ? `PID ${status.pid}` : '',
    };
  }
  // 顺序要紧：主机在线但进程未运行属于「警示」，不应被通用 error 分支吞掉。
  if (!status.hostOnline) {
    return { label: '主机离线', tone: STATUS_TONE.host_offline, detail: status.error || '' };
  }
  if (status.processRunning === false || status.portListening === false) {
    return {
      label: '进程未运行',
      tone: STATUS_TONE.process_stopped,
      detail: status.error || 'Agent 进程未在运行或端口未监听',
    };
  }
  return { label: '状态未知', tone: STATUS_TONE.unknown, detail: status.error || '' };
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

function instanceDialogDescription(editingInstance, instanceOwner) {
  if (editingInstance) {
    return '实例指向一台已安装 Agent 的主机上的 AI Agent 本地服务端口。';
  }
  if (instanceOwner) {
    return `为该用户（${instanceOwner.username}）登记一台已安装 Agent 的主机上的 AI Agent 服务。`;
  }
  return '实例指向一台已安装 Agent 的主机上的 AI Agent 本地服务端口。';
}

const RESULT_META = {
  ok: { label: '成功', tone: 'success' },
  denied: { label: '拒绝', tone: 'warning' },
  error: { label: '失败', tone: 'danger' },
};

function resultMeta(result) {
  return RESULT_META[result] || { label: result || '-', tone: 'neutral' };
}

// InstanceTable 是实例列表的共用表格：「我的实例」按登录身份呈现，
// 管理员的「用户实例」弹层复用同一份渲染（此时 isAdmin 传 false，归属已知）。
function InstanceTable({ instances, isAdmin, onAccess, onEdit, onDelete, isArmed }) {
  return (
    <div className="overflow-x-auto">
      <Table layout="fixed" className={isAdmin ? 'min-w-[1010px]' : 'min-w-[880px]'}>
        <colgroup>
          <col className="w-[200px]" />
          {isAdmin && <col className="w-[130px]" />}
          <col className="w-[130px]" />
          <col className="w-[170px]" />
          <col className="w-[90px]" />
          <col className="w-[140px]" />
          <col className="w-[210px]" />
        </colgroup>
        <Table.Header variant="compact">
          <Table.Row>
            <Table.Head>名称</Table.Head>
            {isAdmin && <Table.Head>归属用户</Table.Head>}
            <Table.Head>Provider</Table.Head>
            <Table.Head>主机</Table.Head>
            <Table.Head className="text-right">端口</Table.Head>
            <Table.Head>状态</Table.Head>
            <Table.Head className="app-table-action">操作</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {instances.map(instance => {
            const status = instanceStatus(instance);
            return (
              <Table.Row key={instance.id}>
                <Table.Cell>
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-kumo-strong">{instance.label}</span>
                    {!instance.enabled && <StatusBadge tone="neutral">已停用</StatusBadge>}
                  </div>
                </Table.Cell>
                {isAdmin && (
                  <Table.Cell className="text-xs text-kumo-subtle">
                    {instance.username || shortId(instance.userId)}
                  </Table.Cell>
                )}
                <Table.Cell>{instance.providerLabel || instance.provider}</Table.Cell>
                <Table.Cell>{instance.hostName || instance.serverId}</Table.Cell>
                <Table.Cell className="text-right font-mono text-xs">{instance.port}</Table.Cell>
                <Table.Cell>
                  <StatusBadge tone={status.tone} title={status.detail}>
                    {status.label}
                  </StatusBadge>
                </Table.Cell>
                <Table.Cell className="app-table-action">
                  <div className="flex items-center justify-center gap-1">
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
  const [providers, setProviders] = useState([]);
  const [servers, setServers] = useState([]);
  const [instanceDialogOpen, setInstanceDialogOpen] = useState(false);
  const [editingInstance, setEditingInstance] = useState(null);
  // instanceOwner 是新增实例时的归属用户（管理员从「用户管理」指定）；
  // 普通用户新增时为空，由后端归属到当前账号。
  const [instanceOwner, setInstanceOwner] = useState(null);
  const [instanceForm, setInstanceForm] = useState({
    serverId: '',
    provider: 'opencode',
    label: '',
    port: '',
    enabled: true,
  });
  const [instanceSaving, setInstanceSaving] = useState(false);
  const [accessInfo, setAccessInfo] = useState(null);
  // userInstancesTarget 是管理员正在查看实例的用户（用户管理里的二级弹层）。
  const [userInstancesTarget, setUserInstancesTarget] = useState(null);
  const [userInstances, setUserInstances] = useState([]);
  const [userInstancesLoading, setUserInstancesLoading] = useState(false);

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
  // 当前所选 Provider 的默认端口：实例端口第一版只允许该值。
  const selectedProviderPort = useMemo(() => {
    const provider = providers.find(item => item.id === instanceForm.provider);
    return provider?.defaultPort;
  }, [providers, instanceForm.provider]);
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

  const loadInstances = useCallback(async () => {
    setInstancesLoading(true);
    try {
      const payload = await get('/api/aiagent/instances?probe=1');
      setInstances(payload.data || []);
    } catch (error) {
      toast.error(error.message || '加载实例失败');
    } finally {
      setInstancesLoading(false);
    }
  }, []);

  // loadUserInstances 拉取指定用户的实例（管理员在用户管理里的二级弹层）。
  // 重新选择用户时用请求序号丢弃过期响应，避免旧请求覆盖新用户的数据。
  const userInstancesSeqRef = useRef(0);
  const loadUserInstances = useCallback(async user => {
    const seq = ++userInstancesSeqRef.current;
    setUserInstancesLoading(true);
    try {
      const payload = await get(
        `/api/aiagent/instances?probe=1&userId=${encodeURIComponent(user.id)}`
      );
      if (seq !== userInstancesSeqRef.current) return;
      setUserInstances(payload.data || []);
    } catch (error) {
      if (seq !== userInstancesSeqRef.current) return;
      toast.error(error.message || '加载该用户实例失败');
      setUserInstances([]);
    } finally {
      if (seq === userInstancesSeqRef.current) setUserInstancesLoading(false);
    }
  }, []);

  const openUserInstances = user => {
    setUserInstancesTarget(user);
    setUserInstances([]);
    loadUserInstances(user);
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

  const loadLogs = useCallback(async () => {
    setLogsLoading(true);
    try {
      const payload = await get('/api/aiagent/logs?limit=120');
      setLogs(payload.data || []);
    } catch (error) {
      toast.error(error.message || '加载日志失败');
      setLogs([]);
    } finally {
      setLogsLoading(false);
    }
  }, []);

  useEffect(() => {
    loadInstances();
    loadProviders();
    loadServers();
  }, [loadInstances, loadProviders, loadServers]);

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
  const logsTabLoadedRef = useRef(false);
  useEffect(() => {
    if (activeTab !== 'logs') return;
    if (logsTabLoadedRef.current) return;
    logsTabLoadedRef.current = true;
    loadLogs();
  }, [activeTab, loadLogs]);

  // openCreateInstance 新增实例。管理员从「用户管理」逐用户进入时传入 owner，
  // 归属由入口决定，不再在表单里选择；普通用户新增时 owner 为空，由后端归属自己。
  const openCreateInstance = owner => {
    setEditingInstance(null);
    setInstanceOwner(owner || null);
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
    setInstanceOwner(null);
    setInstanceForm({
      serverId: instance.serverId,
      provider: instance.provider,
      label: instance.label,
      port: instance.port ? String(instance.port) : '',
      enabled: instance.enabled,
    });
    setInstanceDialogOpen(true);
  };

  const saveInstance = async () => {
    const label = instanceForm.label.trim();
    if (!instanceForm.serverId) {
      toast.warning('请选择主机');
      return;
    }
    // 主机已被移除时不允许直接提交旧值，要求重新选择。
    // 所选主机不可用（列表已加载但无此项，或列表未加载且当前值无法解析）时要求重选。
    const hostKnown = serverOptions.some(option => option.value === instanceForm.serverId);
    if (!hostKnown && (serverOptions.length > 0 || !isAdmin)) {
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
    // 后端第一版只接受 Provider 的默认端口；这里提前拦住并给出明确提示。
    // 区分「列表未加载」与「所选 Provider 已不在列表中」两种情形。
    if (port !== undefined && selectedProviderPort === undefined) {
      if (providerOptions.length === 0) {
        toast.warning('Provider 列表未加载，无法校验端口，请先刷新后再试');
      } else {
        toast.warning('所选 Provider 已不可用，请重新选择后再填写端口');
      }
      return;
    }
    if (port !== undefined && port !== selectedProviderPort) {
      toast.warning(`该 Provider 当前仅支持默认端口 ${selectedProviderPort}`);
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
        // 归属由入口决定：管理员从用户实例弹层进入时带 owner，普通用户由后端归属自己。
        if (instanceOwner?.id) body.userId = instanceOwner.id;
        await post('/api/aiagent/instances', body);
        toast.success('实例已添加');
      }
      setInstanceDialogOpen(false);
      await Promise.all([
        loadInstances(),
        userInstancesTarget ? loadUserInstances(userInstancesTarget) : Promise.resolve(),
      ]);
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
      await Promise.all([
        loadInstances(),
        userInstancesTarget ? loadUserInstances(userInstancesTarget) : Promise.resolve(),
      ]);
    } catch (error) {
      toast.error(error.message || '删除实例失败');
    }
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
                  {isAdmin ? '全部实例' : '我的实例'}
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
          title={isAdmin ? '全部实例' : '我的实例'}
          description={
            isAdmin
              ? '各用户名下的 AI Agent 实例；新增与归属请在「用户管理」中按用户操作。'
              : '登记已分配主机上的 AI Agent 服务。'
          }
          bodyPadding="none"
          actions={
            <>
              <Button size="sm" variant="secondary" onClick={loadInstances}>
                <RefreshCw className="h-4 w-4" />
                刷新
              </Button>
              {!isAdmin && (
                <Button size="sm" variant="primary" onClick={() => openCreateInstance(null)}>
                  <Plus className="h-4 w-4" />
                  添加实例
                </Button>
              )}
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
              description={
                isAdmin
                  ? '在「用户管理」中选择用户后为其添加实例'
                  : '添加一台已安装 Agent 的主机上的 AI Agent 服务'
              }
            />,
            <InstanceTable
              instances={instances}
              isAdmin={isAdmin}
              onAccess={showAccessInfo}
              onEdit={openEditInstance}
              onDelete={removeInstance}
              isArmed={isArmed}
            />
          )}
        </SectionCard>
      )}

      {activeTab === 'users' && (
        <SectionCard
          title="用户管理"
          description="创建用户、设置密码，并按用户管理其 AI Agent 实例。"
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
                            aria-label="管理实例"
                            title="管理实例"
                            onClick={() => openUserInstances(user)}
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
            <Button size="sm" variant="secondary" onClick={loadLogs}>
              <RefreshCw className="h-4 w-4" />
              刷新
            </Button>
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

      <Dialog.Root open={instanceDialogOpen} onOpenChange={setInstanceDialogOpen}>
        <Dialog className="@container !w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">
            {editingInstance ? '编辑实例' : '添加实例'}
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">
            {instanceDialogDescription(editingInstance, instanceOwner)}
          </Dialog.Description>
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
                端口（当前仅支持该 Provider 的默认端口
                {selectedProviderPort ? ` ${selectedProviderPort}` : ''}）
              </span>
              <Input
                size="sm"
                value={instanceForm.port}
                inputMode="numeric"
                placeholder={selectedProviderPort ? String(selectedProviderPort) : '默认端口'}
                onChange={event => setInstanceForm(prev => ({ ...prev, port: event.target.value }))}
              />
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
          <div className="mt-5 flex justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => setInstanceDialogOpen(false)}>
              取消
            </Button>
            <Button size="sm" variant="primary" onClick={saveInstance} disabled={instanceSaving}>
              {instanceSaving ? '保存中…' : '保存'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root
        open={Boolean(userInstancesTarget)}
        onOpenChange={open => {
          if (!open) setUserInstancesTarget(null);
        }}
      >
        <Dialog className="@container !w-[min(52rem,calc(100vw-2rem))] !max-w-[min(52rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">
            {userInstancesTarget?.username} 的实例
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">
            登记在用户名下的 AI Agent 实例，可为其新增、编辑或删除。
          </Dialog.Description>
          <div className="mb-3 flex items-center justify-between gap-2">
            <Button size="sm" variant="secondary" onClick={() => openUserInstances(userInstancesTarget)}>
              <RefreshCw className="h-4 w-4" />
              刷新
            </Button>
            <Button
              size="sm"
              variant="primary"
              onClick={() => openCreateInstance(userInstancesTarget)}
            >
              <Plus className="h-4 w-4" />
              添加实例
            </Button>
          </div>
          {renderLoadingOrEmpty(
            userInstancesLoading,
            userInstances.length === 0,
            <Empty
              size="sm"
              className="rounded-none border-0 bg-transparent"
              title="该用户还没有实例"
              description="点击「添加实例」为其登记一台已安装 Agent 的机器"
            />,
            <LayerCard className="overflow-x-auto p-0">
              <InstanceTable
                instances={userInstances}
                isAdmin={false}
                onAccess={showAccessInfo}
                onEdit={openEditInstance}
                onDelete={removeInstance}
                isArmed={isArmed}
              />
            </LayerCard>
          )}
          <div className="mt-5 flex justify-end">
            <Button size="sm" variant="secondary" onClick={() => setUserInstancesTarget(null)}>
              关闭
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={userDialogOpen} onOpenChange={setUserDialogOpen}>
        <Dialog className="@container !w-[min(30rem,calc(100vw-2rem))] !max-w-[min(30rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">
            {editingUser ? `编辑用户 ${editingUser.username}` : '新建用户'}
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">
            用户用于客户端登录，与面板管理员账号相互独立。
          </Dialog.Description>
          <div className="flex flex-col gap-3">
            <label className="flex flex-col gap-1">
              <span className="text-sm text-kumo-subtle">用户名</span>
              <Input
                size="sm"
                value={userForm.username}
                disabled={Boolean(editingUser)}
                placeholder="3-32 位字母数字与 - _ ."
                onChange={event => setUserForm(prev => ({ ...prev, username: event.target.value }))}
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
                  onCheckedChange={checked => setUserForm(prev => ({ ...prev, disabled: checked }))}
                />
                禁用该用户（其全部令牌立即失效）
              </label>
            )}
          </div>
          <div className="mt-5 flex justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => setUserDialogOpen(false)}>
              取消
            </Button>
            <Button size="sm" variant="primary" onClick={saveUser} disabled={userSaving}>
              {userSaving ? '保存中…' : '保存'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root
        open={Boolean(accessInfo)}
        onOpenChange={open => {
          if (!open) setAccessInfo(null);
        }}
      >
        <Dialog className="@container !w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">
            接入信息
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">
            客户端（如 OpenCode UI）填写面板域名后，使用该实例的网关路径访问。
          </Dialog.Description>
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
          <div className="mt-5 flex justify-end">
            <Button size="sm" variant="secondary" onClick={() => setAccessInfo(null)}>
              关闭
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root
        open={Boolean(resetTarget)}
        onOpenChange={open => {
          if (!open) setResetTarget(null);
        }}
      >
        <Dialog className="@container !w-[min(30rem,calc(100vw-2rem))] !max-w-[min(30rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">
            重置密码 {resetTarget?.username}
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">
            重置后该用户已签发的全部令牌会立即失效，客户端需要重新登录。
          </Dialog.Description>
          <div>
            <label className="flex flex-col gap-1">
              <span className="text-sm text-kumo-subtle">新密码（至少 8 位）</span>
              <Input
                size="sm"
                type="password"
                value={resetPassword}
                onChange={event => setResetPassword(event.target.value)}
              />
            </label>
          </div>
          <div className="mt-5 flex justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => setResetTarget(null)}>
              取消
            </Button>
            <Button size="sm" variant="primary" onClick={resetPasswordFor} disabled={resetting}>
              {resetting ? '提交中…' : '重置'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </PageStack>
  );
}
