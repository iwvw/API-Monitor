export const DEFAULT_TASK_FORM = {
  id: null,
  name: '',
  description: '',
  useCustom: false,
  periodType: 'day',
  hour: 3,
  minute: 0,
  dayOfMonth: 1,
  weekday: '1',
  schedule: '0 3 * * *',
  type: 'shell',
  command: '',
  enabled: 1,
  timeout_seconds: 300,
  retry_count: 0,
  retry_interval_seconds: 30,
  max_concurrency: 1,
  node_id: 'local',
  node_selector: '',
  // AI 智能任务扩展配置（保存时序列化为 config JSON）
  aiModel: '',
  aiPolicy: 'allow',
  aiChannelId: '',
};

export const DEFAULT_WORKFLOW_FORM = {
  id: null,
  name: '',
  description: '',
  schedule: '',
  enabled: 1,
  concurrency_policy: 'skip',
  failure_policy: 'stop',
  nodes: [
    { id: 'start', name: '开始', type: 'start', enabled: 1, x: 40, y: 80 },
    { id: 'task-1', name: '任务 1', type: 'task', task_id: 0, enabled: 1, x: 260, y: 80 },
  ],
  edges: [{ id: 'edge-1', from: 'start', to: 'task-1', condition: 'success' }],
};

export const PERIOD_ITEMS = [
  { value: 'minute', label: '每分钟' },
  { value: 'hour', label: '每小时' },
  { value: 'day', label: '每天' },
  { value: 'week', label: '每周' },
  { value: 'month', label: '每月' },
];

export const WEEKDAY_ITEMS = [
  { value: '0', label: '周日' },
  { value: '1', label: '周一' },
  { value: '2', label: '周二' },
  { value: '3', label: '周三' },
  { value: '4', label: '周四' },
  { value: '5', label: '周五' },
  { value: '6', label: '周六' },
];

export const TYPE_ITEMS = [
  { value: 'shell', label: 'Shell 命令' },
  { value: 'http', label: 'HTTP 请求' },
  { value: 'internal', label: '内部接口' },
  { value: 'agent', label: 'Agent 命令' },
  { value: 'ai', label: 'AI 智能任务' },
];

export const AI_POLICY_ITEMS = [
  { value: 'allow', label: '完全允许（写操作免审批）' },
  { value: 'readonly', label: '只读（禁用写操作）' },
];

export const CONDITION_ITEMS = [
  { value: 'success', label: '成功后' },
  { value: 'failed', label: '失败后' },
  { value: 'complete', label: '完成后' },
];

export const STATUS_LABELS = {
  success: '成功',
  failed: '失败',
  running: '运行中',
  queued: '排队中',
  skipped: '已跳过',
  timeout: '超时',
  cancelled: '已取消',
};

export const WORKFLOW_CANVAS_SIZES = {
  // compact 与 editor 共用同一套布局常量：卡片画布的节点尺寸/连线风格与编辑画布完全一致；
  //     高度由左栏锚定（absolute inset-0 + wrapper min-h 常量兜底，无测量反馈环）；
  // default（运行详情）用固定视口高度（GitHub ActionWorkflowCanvas 同款 320）；
  // editor 在对话框定高 flex 链中，测量稳定。
  compact: { nodeW: 208, nodeH: 96, stageGap: 72, rowGap: 18, padX: 24, padY: 24, minScale: 0.45, fixedHeight: null },
  editor: { nodeW: 208, nodeH: 96, stageGap: 72, rowGap: 18, padX: 24, padY: 24, minScale: 0.45, fixedHeight: null },
  default: { nodeW: 240, nodeH: 108, stageGap: 72, rowGap: 26, padX: 24, padY: 24, minScale: 0.6, fixedHeight: 320 },
};

// editor 视口位于对话框定高 flex 链中，测量高度与内容无关（无反馈）；此上界仅为异常兜底。
export const WORKFLOW_CANVAS_MAX_HEIGHT = 520;
