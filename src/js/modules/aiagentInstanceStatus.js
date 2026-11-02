// AI Agent 实例状态的展示语义。
//
// 从 AiAgentConsole 抽出来单独成模块，原因有二：
//   1. 状态判定分支多（托管/非托管 × 崩溃/启动中/已停止/端口被占…），
//      内联在 JSX 里既难读也难测；
//   2. 判定规则直接对应 ADR-0006 的语义，需要测试锁定，避免后续改 UI 时误改语义。
//
// 本模块只做「数据 → 展示语义」的纯映射，不碰 React、不发请求。

export const STATUS_TONE = {
  online: 'success',
  process_stopped: 'warning',
  // 崩溃是比「已停止」更严重的终态：需要人工介入，用 danger 让它从列表里跳出来。
  crashed: 'danger',
  starting: 'info',
  host_offline: 'neutral',
  unknown: 'neutral',
};

/**
 * 判定单个实例的展示状态。
 *
 * 判定顺序即优先级，顺序本身是语义的一部分：
 *   1. crashed 最优先——它是终态，不应被「托管」分支吞掉；
 *   2. 托管实例的 lifecycle 优先于端口探测，因为进程由 Agent 自己启动并持有 PID；
 *   3. 非托管实例回退到探测结果，并按 ADR-0006 的关联验证给出具体失败原因。
 *
 * @param {{status?: object, lifecycle?: object}} instance
 * @returns {{label: string, tone: string, detail: string, spinner?: boolean, restarts?: number}}
 */
export function instanceStatus(instance) {
  const status = instance?.status || {};
  const lifecycle = instance?.lifecycle || {};

  // 1. 崩溃终态：重启次数用尽，需人工介入。
  if (lifecycle.crashed) {
    return {
      label: '已崩溃',
      tone: STATUS_TONE.crashed,
      detail: `重启 ${lifecycle.restarts || 0} 次后仍失败，请手动启动或检查主机日志`,
      restarts: lifecycle.restarts || 0,
    };
  }

  // 2. 托管实例：以 Agent 持有的进程状态为准。
  if (lifecycle.managed) {
    if (lifecycle.running) {
      return {
        label: '运行中',
        tone: STATUS_TONE.online,
        detail: lifecycle.pid ? `PID ${lifecycle.pid} · 托管` : '托管中',
        restarts: lifecycle.restarts || 0,
      };
    }
    // 期望运行但当前没跑 → 正在被拉起（supervisor 或云端收敛）。
    // spinner 让「过渡态」在列表里可辨，而不是看起来像静止的故障。
    if (lifecycle.desiredRunning) {
      return {
        label: '启动中',
        tone: STATUS_TONE.starting,
        detail: lifecycle.restarts ? `正在自动重启（已 ${lifecycle.restarts} 次）` : '正在拉起进程',
        spinner: true,
        restarts: lifecycle.restarts || 0,
      };
    }
    // 期望停止且已停止：这是用户主动操作的结果，属正常终态。
    return {
      label: '已停止',
      tone: STATUS_TONE.process_stopped,
      detail: '托管进程已停止',
      restarts: lifecycle.restarts || 0,
    };
  }

  // 3. 非托管实例：回退到端口/进程探测结果。
  if (status.online) {
    return {
      label: '运行中',
      tone: STATUS_TONE.online,
      detail: status.pid ? `PID ${status.pid}` : '',
    };
  }
  // 主机状态缺失（数据未加载/字段缺省）不等于离线：伪造「主机离线」会误导排查方向。
  if (status.hostOnline === undefined) {
    return { label: '状态未知', tone: STATUS_TONE.unknown, detail: status.error || '' };
  }
  // 顺序要紧：主机在线但进程未运行属于「警示」，不应被通用 error 分支吞掉。
  if (!status.hostOnline) {
    return { label: '主机离线', tone: STATUS_TONE.host_offline, detail: status.error || '' };
  }
  // 关联验证的两种明确失败（ADR-0006 第 2 条）：端口被别的进程占了，
  // 或进程在跑但没监听预期端口。两者都不该笼统报「进程未运行」。
  if (status.portListening && status.listenerMatchesProcess === false) {
    return {
      label: '端口被占用',
      tone: STATUS_TONE.process_stopped,
      detail: status.listenerPid
        ? `端口被 PID ${status.listenerPid} 的其它进程占用`
        : '端口被其它进程占用',
    };
  }
  if (status.processRunning && status.portListening === false) {
    return {
      label: '未监听端口',
      tone: STATUS_TONE.process_stopped,
      detail: '进程在运行，但未监听预期端口',
    };
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

/** 字节数格式化为人类可读（MB/GB）。0 或缺失返回 '—'，避免显示 "0 B" 造成误读。 */
export function formatBytes(value) {
  if (!value || value <= 0) return '—';
  const mb = value / (1024 * 1024);
  if (mb < 1024) return `${mb.toFixed(mb < 10 ? 1 : 0)} MB`;
  return `${(mb / 1024).toFixed(2)} GB`;
}

/** CPU 百分比格式化。0 也可能合法（空闲），因此只在缺失时显示 '—'。 */
export function formatPercent(value) {
  if (value === undefined || value === null) return '—';
  return `${Number(value).toFixed(1)}%`;
}

/**
 * 资源占用取数：托管实例以 lifecycle 为准（Agent 自己持有 PID，数值最准），
 * 非托管实例回退到探测结果。两处字段含义一致（字节 / 百分比）。
 */
export function formatResource(instance, kind) {
  const lifecycle = instance?.lifecycle || {};
  const status = instance?.status || {};
  const useLifecycle = lifecycle.managed && lifecycle.running;
  const source = useLifecycle ? lifecycle : status;
  if (kind === 'memory') return formatBytes(source.memoryBytes);
  return formatPercent(source.cpuPercent);
}

/** 运行时长格式化为「3天2小时」这类可读文本。0 或缺失返回占位符。 */
export function formatUptime(seconds) {
  if (!seconds || seconds <= 0) return '—';
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days > 0) return `${days} 天 ${hours} 小时`;
  if (hours > 0) return `${hours} 小时 ${minutes} 分`;
  if (minutes > 0) return `${minutes} 分 ${seconds % 60} 秒`;
  return `${seconds} 秒`;
}

/** 布尔值渲染为中文标签；undefined 表示未知，不伪造成「否」。 */
function booleanLabel(value) {
  if (value === undefined || value === null) return '未知';
  return value ? '是' : '否';
}

/**
 * 构建实例详情弹层的数据分组。
 *
 * 抽成纯函数是为了可测：详情字段的取舍（哪些该展示、哪些要标注来源）
 * 是业务判断，不适合埋在 JSX 里。
 *
 * 返回分组数组，每组 { title, rows: [{ label, value, tone? }] }。
 */
export function buildInstanceDetailGroups(instance) {
  if (!instance) return [];
  const status = instance.status || {};
  const lifecycle = instance.lifecycle || {};
  const provider = instance.providerLabel || instance.provider || '—';

  const identity = {
    title: '基本信息',
    rows: [
      { label: '名称', value: instance.label || '—' },
      { label: 'Provider', value: provider },
      { label: '主机', value: instance.hostName || instance.serverId || '—' },
      { label: '端口', value: String(instance.port ?? '—') },
      { label: '状态', value: instance.enabled ? '已启用' : '已停用' },
    ],
  };

  // 托管实例：展示 Agent 侧持有的权威状态。
  const lifecycleGroup =
    lifecycle.managed || lifecycle.supported
      ? {
          title: '托管进程',
          rows: [
            { label: '托管中', value: booleanLabel(lifecycle.managed) },
            { label: '期望状态', value: desiredStateLabel(instance.desiredState) },
            { label: '实际运行', value: booleanLabel(lifecycle.running) },
            { label: '进程 PID', value: lifecycle.pid ? String(lifecycle.pid) : '—' },
            { label: '运行时长', value: formatUptime(lifecycle.uptimeSeconds) },
            { label: '自动重启', value: lifecycle.restarts ? `${lifecycle.restarts} 次` : '无' },
            { label: '已崩溃', value: lifecycle.crashed ? '是（需手动启动）' : '否' },
            { label: '内存', value: formatResource(instance, 'memory') },
            { label: 'CPU', value: formatResource(instance, 'cpu') },
          ],
        }
      : null;

  // 探测结果：无论是否托管都展示，便于对照排查。
  const probeGroup = {
    title: '探测结果',
    rows: [
      { label: '主机在线', value: booleanLabel(status.hostOnline) },
      { label: '进程运行', value: booleanLabel(status.processRunning) },
      { label: '端口监听', value: booleanLabel(status.portListening) },
      { label: '匹配到的 PID', value: status.pid ? String(status.pid) : '—' },
      { label: '监听端口的 PID', value: status.listenerPid ? String(status.listenerPid) : '—' },
      {
        label: '监听者命中规则',
        value: booleanLabel(status.listenerMatchesProcess),
      },
      { label: '探测时间', value: status.probedAt || '—' },
      { label: '错误', value: status.error || '无' },
    ],
  };

  const accessGroup = {
    title: '接入信息',
    rows: [
      { label: '网关路径', value: instance.accessPath || '—' },
      { label: '网关地址', value: instance.gatewayUrl || '—' },
    ],
  };

  return [identity, lifecycleGroup, probeGroup, accessGroup].filter(Boolean);
}

/** 期望状态的中文标签。空串表示不托管，这是存量实例的默认值。 */
function desiredStateLabel(value) {
  if (value === 'running') return '运行中';
  if (value === 'stopped') return '已停止';
  return '不托管（仅探测）';
}
