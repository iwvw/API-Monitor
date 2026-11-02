import { sectionCardHeaderClass } from '../../components/ui/AppPrimitives.jsx';

export const SERVER_LIST_VIEW_STORAGE_KEY = 'server_list_view_mode_v2';
export const SERVER_COMPACT_COLUMNS_STORAGE_KEY = 'server_compact_visible_columns';
export const SERVER_STATUS_SYNC_INTERVAL_MS = 15000;

export const HOST_COMPACT_COLUMNS = [
  { id: 'status', label: '状态' },
  { id: 'name', label: '名称', required: true },
  { id: 'country', label: '位置' },
  { id: 'uptime', label: '在线' },
  { id: 'load', label: '负载' },
  { id: 'speed', label: '网速' },
  { id: 'traffic', label: '流量' },
  { id: 'cpu', label: 'CPU' },
  { id: 'memory', label: '内存' },
  { id: 'disk', label: '硬盘' },
  { id: 'remaining', label: '余期' },
  { id: 'quotaRemaining', label: '余量' },
  { id: 'actions', label: '', required: true },
];
export const HOST_COMPACT_COLUMN_IDS = HOST_COMPACT_COLUMNS.map(column => column.id);
export const HOST_COMPACT_DEFAULT_VISIBLE_COLUMNS = Array.from(new Set([
  ...HOST_COMPACT_COLUMN_IDS,
  'quotaRemaining',
]));
export const HOST_COMPACT_COLUMN_WIDTHS = {
  status: 74,
  name: 112,
  country: 71,
  uptime: 71,
  load: 71,
  speed: 228,
  traffic: 228,
  cpu: 112,
  memory: 112,
  disk: 112,
  quotaRemaining: 112,
  remaining: 112,
  actions: 40,
};
export const HOST_COMPACT_ADAPTIVE_COLUMNS = new Set(['cpu', 'memory', 'disk', 'remaining', 'quotaRemaining']);
export const HOST_COMPACT_HEADER_BOX_CLASS = {
  status: 'w-[58px] justify-center',
  name: 'w-[96px] justify-center',
  country: 'w-[55px] justify-center',
  uptime: 'w-[55px] justify-center',
  load: 'w-[55px] justify-center',
  speed: 'w-full min-w-[208px] justify-center',
  traffic: 'w-full min-w-[208px] justify-center',
  cpu: 'w-full min-w-[96px] justify-center',
  memory: 'w-full min-w-[96px] justify-center',
  disk: 'w-full min-w-[96px] justify-center',
  quotaRemaining: 'w-full min-w-[96px] justify-center',
  remaining: 'w-full min-w-[96px] justify-center',
  actions: 'w-[34px] justify-center',
};
export const COMPACT_INLINE_BOX_CLASS = 'border border-kumo-interact/70 shadow-none';
export const COMPACT_INLINE_SUBBOX_CLASS = 'border border-kumo-interact/70 shadow-none';
export const COMPACT_STICKY_ACTION_CLASS = 'border-l border-kumo-interact/60 before:!w-1 before:!-left-1';
export const COMPACT_ACTION_BUTTON_CLASS = '!shadow-none';
export const SERVER_SECTION_HEADER_CLASS = sectionCardHeaderClass;
export const SERVER_SECONDARY_BAR_CLASS = 'flex min-h-[46px] flex-wrap items-center gap-2 rounded-md border border-kumo-line/90 bg-kumo-base px-3 py-2 cq-lg:justify-between';
export const SERVER_SECONDARY_TABS_GROUP_CLASS = 'flex min-w-0 flex-nowrap items-center gap-1.5 overflow-x-auto whitespace-nowrap p-0.5 scrollbar-thin cq-sm:gap-2';
export const MANAGEMENT_CARD_ICON_CLASS = 'h-3.5 w-3.5 shrink-0 text-brand';
export const SERVER_MODULE_TAB_ICON_CLASS = 'h-3.5 w-3.5 shrink-0';
export const COMPACT_EXPAND_EXIT_MS = 230;
export const SERVER_CHART_SERIES_DEFER_MS = 44;
export const SERVER_CHART_RENDER_DEFER_MS = 88;
export const SERVER_CHART_ANIMATION_MS = 90;
export const SERVER_CHART_UPDATE_ANIMATION_MS = 70;
export const SERVER_FAST_CHART_UPDATE_BEHAVIOR = { lazyUpdate: false };
export const SERVER_NETWORK_QUALITY_REFRESH_MS = 60 * 1000;
export const SERVER_NETWORK_QUALITY_CHART_UPDATE_BEHAVIOR = { lazyUpdate: true };

export const SERVER_FAST_CHART_ANIMATION_OPTIONS = {
  animation: true,
  animationDuration: SERVER_CHART_ANIMATION_MS,
  animationDurationUpdate: SERVER_CHART_UPDATE_ANIMATION_MS,
};
export const SERVER_STATIC_CHART_ANIMATION_OPTIONS = {
  animation: false,
  animationDuration: 0,
  animationDurationUpdate: 0,
};

export const LOCATION_COUNTRY_CODE_MAP = {
  'united states': 'us',
  usa: 'us',
  'u.s.': 'us',
  america: 'us',
  'united kingdom': 'gb',
  uk: 'gb',
  britain: 'gb',
  england: 'gb',
  germany: 'de',
  france: 'fr',
  netherlands: 'nl',
  japan: 'jp',
  singapore: 'sg',
  hongkong: 'hk',
  'hong kong': 'hk',
  china: 'cn',
  taiwan: 'tw',
  korea: 'kr',
  canada: 'ca',
  australia: 'au',
  '美国': 'us',
  '英国': 'gb',
  '德国': 'de',
  '法国': 'fr',
  '荷兰': 'nl',
  '日本': 'jp',
  '新加坡': 'sg',
  '香港': 'hk',
  '中国': 'cn',
  '台湾': 'tw',
  '韩国': 'kr',
  '加拿大': 'ca',
  '澳大利亚': 'au',
};

export const METRICS_COLLECT_INTERVAL_TABS = [1, 2, 5, 10, 15, 30, 60].map(value => ({
  value: String(value),
  label: `${value}m`,
}));
export const METRICS_RETENTION_TABS = [7, 30, 60, 90, 180].map(value => ({
  value: String(value),
  label: `${value}天`,
}));
