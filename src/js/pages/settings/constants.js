import {
  DEFAULT_MODULE_ORDER,
  MODULE_CONFIG,
  MODULE_GROUPS,
  getGroupModuleIds,
} from '../../store.js';

export const SECURITY_MASONRY_CARD_CLASS = '';

export const THEME_OPTIONS = [
  { value: 'auto', label: '跟随系统' },
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
];

export const GITHUB_NEW_OAUTH_APP_URL = 'https://github.com/settings/applications/new';

export const TIMEZONE_OPTIONS = [
  { value: 'system', label: '跟随服务器' },
  { value: 'UTC', label: 'UTC' },
  { value: 'Asia/Shanghai', label: '中国标准时间 (Asia/Shanghai)' },
  { value: 'Asia/Tokyo', label: '日本时间 (Asia/Tokyo)' },
  { value: 'Asia/Singapore', label: '新加坡时间 (Asia/Singapore)' },
  { value: 'Europe/London', label: '伦敦时间 (Europe/London)' },
  { value: 'Europe/Berlin', label: '柏林时间 (Europe/Berlin)' },
  { value: 'America/New_York', label: '纽约时间 (America/New_York)' },
  { value: 'America/Los_Angeles', label: '洛杉矶时间 (America/Los_Angeles)' },
];



export const moduleRows = DEFAULT_MODULE_ORDER.filter((moduleId) => moduleId !== 'prompts').map((moduleId) => {
  const group = MODULE_GROUPS.find((item) => getGroupModuleIds(item).includes(moduleId));
  return {
    id: moduleId,
    groupId: group?.id || 'other',
    groupName: group?.name || '其他模块',
    config: MODULE_CONFIG[moduleId] || { name: moduleId },
  };
});
