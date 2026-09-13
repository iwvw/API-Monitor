const statusPanelClass = {
  success: 'border-kumo-success/45 bg-kumo-base text-kumo-success',
  danger: 'border-kumo-danger/45 bg-kumo-base text-kumo-danger',
  warning: 'border-kumo-warning/45 bg-kumo-base text-kumo-warning',
  neutral: 'border-kumo-interact/80 bg-kumo-base text-kumo-strong',
};

const statusTone = (status) => {
  const value = String(status || '').toLowerCase();
  if (['success', 'completed', 'active'].includes(value)) return 'success';
  if (['partial', 'partial_success', 'partial-success'].includes(value)) return 'warning';
  if (['failure', 'failed', 'error', 'timed_out', 'cancelled', 'action_required', 'startup_failure', 'critical'].includes(value)) return 'error';
  if (['in_progress', 'queued', 'pending', 'requested', 'waiting', 'running', 'warning', 'rate_limited'].includes(value)) return 'warning';
  return 'neutral';
};

const statusLabel = (status) => ({
  success: '成功',
  completed: '已完成',
  active: '已启用',
  failure: '失败',
  failed: '失败',
  error: '错误',
  timed_out: '超时',
  cancelled: '已取消',
  action_required: '需要操作',
  startup_failure: '启动失败',
  critical: '严重',
  partial: '部分成功',
  partial_success: '部分成功',
  'partial-success': '部分成功',
  in_progress: '运行中',
  running: '运行中',
  queued: '排队中',
  pending: '等待中',
  requested: '已请求',
  waiting: '等待中',
  warning: '警告',
  rate_limited: '已限流',
  skipped: '已跳过',
  stale: '已过期',
  disabled: '已停用',
  neutral: '未知',
  unknown: '未知',
  info: '信息',
}[String(status || '').toLowerCase()] || status || '未知');

const ACTION_FLOW_CARD_WIDTH = 260;
const ACTION_FLOW_STAGE_GAP = 48;
const ACTION_FLOW_PADDING_X = 28;
const ACTION_FLOW_PADDING_Y = 28;
const ACTION_FLOW_ROW_GAP = 38;
const ACTION_FLOW_VIEWPORT_HEIGHT = 320;
const ACTION_FLOW_MIN_VIEWPORT_HEIGHT = 112;
const ACTION_FLOW_MIN_SCALE = 0.72;
const ACTION_FLOW_BRANCH_INSET = 28;
const PUBLIC_ACTION_PANEL_INSET_Y = 26;

export {
  statusPanelClass,
  statusTone,
  statusLabel,
  ACTION_FLOW_CARD_WIDTH,
  ACTION_FLOW_STAGE_GAP,
  ACTION_FLOW_PADDING_X,
  ACTION_FLOW_PADDING_Y,
  ACTION_FLOW_ROW_GAP,
  ACTION_FLOW_VIEWPORT_HEIGHT,
  ACTION_FLOW_MIN_VIEWPORT_HEIGHT,
  ACTION_FLOW_MIN_SCALE,
  ACTION_FLOW_BRANCH_INSET,
  PUBLIC_ACTION_PANEL_INSET_Y,
};
