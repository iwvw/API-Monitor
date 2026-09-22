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
export const GITHUB_ACTIONS_COLUMNS = [
  { id: 'status', role: 'status', minWidth: 132, maxWidth: 160 },
  { id: 'workflow', role: 'primary', minWidth: 180, maxWidth: 260, grow: 1 },
  { id: 'commit', role: 'content', minWidth: 280, grow: 3 },
  { id: 'branch', role: 'meta', minWidth: 120, maxWidth: 180 },
  { id: 'time', role: 'datetime', align: 'center' },
  { id: 'actions', role: 'actions-md' },
];

export const GITHUB_EVENTS_COLUMNS = [
  { id: 'event', role: 'content', minWidth: 320, grow: 3 },
  { id: 'severity', role: 'status' },
  { id: 'source', role: 'type', align: 'center' },
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