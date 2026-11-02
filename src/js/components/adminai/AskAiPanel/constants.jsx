import { Sliders, ShieldCheck, Globe, Cloud, Server, Terminal, MessageSquare, Clock, FlyIoBrand, KoyebBrand, Bell } from '../../Icons.jsx';

export const PANEL_MIN_WIDTH = 320;
export const PANEL_MAX_WIDTH = 800;
export const PANEL_DEFAULT_WIDTH = 450;

// 会话历史分页：首屏只取最近 MESSAGE_PAGE_LIMIT 行，用户向上滚动到顶部时再沿
// nextCursor 逐页加载更早历史（懒加载）。不再设行数硬上限，避免长会话的早期消息
// 被静默丢弃（旧实现最多 2000 行，超出的最早内容永远看不到、也滑不上去）。
export const MESSAGE_PAGE_LIMIT = 200; // 每页行数（后端上限 200）

export const ACTIVE_SESSION_STORAGE_KEY = 'adminai-active-session';

export function readStoredActiveSession() {
  try { return localStorage.getItem(ACTIVE_SESSION_STORAGE_KEY) || ''; } catch { return ''; }
}
export function storeActiveSession(id) {
  try {
    if (id) localStorage.setItem(ACTIVE_SESSION_STORAGE_KEY, id);
  } catch { }
}

// 会话时间格式化：后端返回 RFC3339 字符串（字段名 createdAt，注意勿用 created_at）
export function formatSessionDate(value) {
  if (!value) return '';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString('zh-CN');
}

export const PROMPT_ICONS = [Sliders, ShieldCheck, Globe, Cloud, Server];

export const SUGGESTED_PROMPTS = [
  { title: '环境变量', subtitle: 'Worker 环境变量' },
  { title: '创建 API Token', subtitle: '面板访问令牌' },
  { title: '域名设置', subtitle: '我的域名配置' },
  { title: 'DNS 记录', subtitle: '添加一条记录' },
  { title: '服务器状态', subtitle: '运行状态' },
];

/* ---------- 行为模式分段切换（kumo Tabs segmented） ---------- */
export const BEHAVIOR_TABS = [
  { value: 'agent', label: (<span className="inline-flex items-center gap-1"><Terminal className="h-3 w-3" />代理</span>) },
  { value: 'ask', label: (<span className="inline-flex items-center gap-1"><MessageSquare className="h-3 w-3" />询问</span>) },
];

/* ---------- @ 资源菜单（多类型：域名/主机/定时任务/CF 账号/Fly.io/Koyeb/调度节点/通知渠道） ---------- */
export const MENTION_GROUPS = [
  { type: 'zone', label: '域名', icon: Globe },
  { type: 'host', label: '主机', icon: Server },
  { type: 'task', label: '定时任务', icon: Clock },
  { type: 'account', label: 'CF 账号', icon: Cloud },
  { type: 'flyio', label: 'Fly.io', icon: FlyIoBrand, sm: true },
  { type: 'koyeb', label: 'Koyeb', icon: KoyebBrand, sm: true },
  { type: 'node', label: '调度节点', icon: Sliders },
  { type: 'channel', label: '通知渠道', icon: Bell },
];

// @ 资源加载：单桶超时（外部聚合接口可能很慢，超时即跳过，不阻塞整列）；
// 刷新窗口（面板常驻挂载，超过该时长再开 @ 会重新拉取，避免列表长期过期与实际不符）
export const RESOURCE_FETCH_TIMEOUT_MS = 8000;
export const RESOURCE_REFRESH_MS = 60000;
