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

const GITHUB_ACTIONS_TABLE_WIDTHS = [132, 220, 480, 132, 168, 124];
const GITHUB_EVENTS_TABLE_WIDTHS = [420, 120, 140, 200];

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
  GITHUB_ACTIONS_TABLE_WIDTHS,
  GITHUB_EVENTS_TABLE_WIDTHS,
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