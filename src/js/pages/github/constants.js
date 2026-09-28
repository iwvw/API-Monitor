const tokenTypeOptions = [
  { value: 'fine_grained', label: 'Fine-grained PAT' },
  { value: 'classic', label: 'Classic PAT' },
  { value: 'app', label: 'GitHub App' },
];

const fineGrainedTokenURL = (resourceOwner = '') => {
  const params = new URLSearchParams({
    name: 'API-Monitor',
    description: 'API-Monitor GitHub observability',
    expires_in: 'none',
    actions: 'write',
    administration: 'read',
    contents: 'read',
    issues: 'read',
    pull_requests: 'read',
    webhooks: 'write',
    workflows: 'write',
  });
  if (resourceOwner) params.set('target_name', resourceOwner);
  return `https://github.com/settings/personal-access-tokens/new?${params.toString()}`;
};

const rangeOptions = [
  { value: '7', label: '7 天' },
  { value: '30', label: '30 天' },
  { value: '90', label: '90 天' },
  { value: '365', label: '365 天' },
];

// 语义列定义（替代旧 widths 数组）：状态/时间/操作居中、提交说明与事件为弹性内容列。
// status 列要容下「标签 · 时长」整串（如「启动失败 · 1时23分45秒」）：Badge 文本是
// text-xs(12px)，实测该串约 129px，加 24px 内边距需 ~153px；旧值 maxWidth 160 在
// 理想列宽 132 时只有 108px 可用，且 Badge 无 truncate，会挤歪相邻的「工作流」列。
// 这里把 min/max 提到 168/176，保证 ≥1 小时的运行时长也完整显示。
export const GITHUB_ACTIONS_COLUMNS = [
  { id: 'status', role: 'status', minWidth: 168, maxWidth: 176 },
  { id: 'workflow', role: 'primary', minWidth: 180, maxWidth: 260, grow: 1 },
  { id: 'commit', role: 'content', minWidth: 280, grow: 3 },
  { id: 'branch', role: 'meta', minWidth: 120, maxWidth: 180 },
  { id: 'time', role: 'datetime', align: 'center' },
  // 操作列有 4 个方形按钮（打开 + 重跑 + 重跑失败项 + 取消）：
  // 需要 26×4 + 4×3 = 116px 内容宽 + 24px 内边距 = 140px，
  // 而 actions-md 的 120 只有 96px 可用，会挤掉最后一个按钮。改用 actions-lg。
  { id: 'actions', role: 'actions-lg' },
];

// source 列渲染后端事件来源标识，最长如 manual_actions_refresh（实测 134px，text-xs），
// 而 type 角色 max 128 → 仅 72px 可用；该单元格既无 truncate 也无 title，
// 会直接溢出压到相邻的「时间」列。改用 minWidth 160 容纳完整标识。
export const GITHUB_EVENTS_COLUMNS = [
  { id: 'event', role: 'content', minWidth: 320, grow: 3 },
  { id: 'severity', role: 'status' },
  { id: 'source', role: 'meta', minWidth: 160, align: 'center' },
  { id: 'time', role: 'datetime', align: 'center' },
];

const SCOPE_BADGE_VARIANTS = {
  'admin:org': 'red',
  'admin:org_hook': 'red',
  'admin:packages': 'red',
  'admin:repo_hook': 'red',
  'admin:gpg_key': 'purple',
  'admin:public_key': 'purple',
  'audit_log': 'red',
  'delete_repo': 'red',
  gist: 'teal',
  notifications: 'teal',
  project: 'orange',
  'read:gpg_key': 'purple',
  'read:org': 'orange',
  'read:packages': 'orange',
  'read:project': 'orange',
  'read:public_key': 'purple',
  'read:repo_hook': 'orange',
  'read:user': 'green',
  repo: 'blue',
  'repo:status': 'blue',
  repo_deployment: 'blue',
  security_events: 'purple',
  'user:email': 'green',
  'user:follow': 'green',
  workflow: 'purple',
  'write:org': 'red',
  'write:packages': 'orange',
  'write:repo_hook': 'orange',
};

const scopeBadgeVariant = (scope) => SCOPE_BADGE_VARIANTS[String(scope || '')] || 'neutral';

const ACTION_FLOW_CARD_WIDTH = 260;
const ACTION_FLOW_STAGE_GAP = 48;
const ACTION_FLOW_PADDING_X = 28;
const ACTION_FLOW_PADDING_Y = 28;
const ACTION_FLOW_ROW_GAP = 38;
const ACTION_FLOW_VIEWPORT_HEIGHT = 320;
const ACTION_FLOW_MIN_VIEWPORT_HEIGHT = 112;
const ACTION_FLOW_MIN_SCALE = 0.72;
const ACTION_FLOW_BRANCH_INSET = 28;

export {
  tokenTypeOptions,
  fineGrainedTokenURL,
  rangeOptions,
  SCOPE_BADGE_VARIANTS,
  scopeBadgeVariant,
  ACTION_FLOW_CARD_WIDTH,
  ACTION_FLOW_STAGE_GAP,
  ACTION_FLOW_PADDING_X,
  ACTION_FLOW_PADDING_Y,
  ACTION_FLOW_ROW_GAP,
  ACTION_FLOW_VIEWPORT_HEIGHT,
  ACTION_FLOW_MIN_VIEWPORT_HEIGHT,
  ACTION_FLOW_MIN_SCALE,
  ACTION_FLOW_BRANCH_INSET,
};