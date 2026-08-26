import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ChatsCircle, Lock } from '@phosphor-icons/react';
import { Button } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Textarea, Input } from '@cloudflare/kumo/components/input';
import { Empty, Sidebar, Tabs, Tooltip } from '@cloudflare/kumo';
import useStore from '../../store.js';
import {
  Sparkle, X, Send, Plus, ChevronDown, Settings as SettingsIcon,
  Sliders, ShieldCheck, Globe, Cloud, Server, Check, Trash, Maximize2, ArrowLeft, Terminal, MessageSquare, Clock, Bell,
  FlyIoBrand, KoyebBrand, WechatBrand, TelegramBrand, WeComBrand,
} from '../Icons.jsx';
import MessageList from './MessageList.jsx';
import AdminConsole, { TAB_OPTIONS } from './AdminConsole.jsx';
import ApprovalCard from './ApprovalCard.jsx';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { parseAdminAiEvent } from '../../modules/adminAiEvents.js';
import {
  MSG,
  STREAM_EVENTS,
  createUserMessage,
  createAssistantMessage,
  normalizeAiEvent,
  applyAiEvent,
  failMessage,
  cancelMessage,
  resolveApprovalPart,
  buildTimelineFromRows,
  markLiveMessage,
} from '../../modules/adminAiMessages.js';
import { useCloudflareSpotlight } from '../../hooks/useCloudflareSpotlight.js';
import { useAskAiCloudMotion } from '../../hooks/useAskAiCloudMotion.js';

const PANEL_MIN_WIDTH = 320;
const PANEL_MAX_WIDTH = 800;
const PANEL_DEFAULT_WIDTH = 450;

const ACTIVE_SESSION_STORAGE_KEY = 'adminai-active-session';

function readStoredActiveSession() {
  try { return localStorage.getItem(ACTIVE_SESSION_STORAGE_KEY) || ''; } catch { return ''; }
}
function storeActiveSession(id) {
  try {
    if (id) localStorage.setItem(ACTIVE_SESSION_STORAGE_KEY, id);
  } catch { }
}

// 会话时间格式化：后端返回 RFC3339 字符串（字段名 createdAt，注意勿用 created_at）
function formatSessionDate(value) {
  if (!value) return '';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString('zh-CN');
}

const PROMPT_ICONS = [Sliders, ShieldCheck, Globe, Cloud, Server];

const SUGGESTED_PROMPTS = [
  { title: '环境变量', subtitle: 'Worker 环境变量' },
  { title: '创建 API Token', subtitle: '面板访问令牌' },
  { title: '域名设置', subtitle: '我的域名配置' },
  { title: 'DNS 记录', subtitle: '添加一条记录' },
  { title: '服务器状态', subtitle: '运行状态' },
];

/* ---------- 空状态：云朵 + 问候 + 建议提示（Cloudflare 官方云朵 CSS 移植 + 动态/视差） ---------- */
function EmptyState({ onPrompt }) {
  const cloudRef = useRef(null);
  useAskAiCloudMotion(cloudRef);
  const hour = new Date().getHours();
  const greeting = hour < 6 ? '夜深了。' : hour < 12 ? '早上好。' : hour < 18 ? '下午好。' : '晚上好。';
  return (
    <div className="flex h-full flex-1 flex-col items-center overflow-y-auto overscroll-contain">
      <div className="my-auto flex w-full flex-col items-center gap-5 pt-4">
        <div ref={cloudRef} className="askai-cloud-container relative aspect-square -my-8" style={{ width: 150, '--blur-multiplier': 1 }} aria-hidden>
          {/* 节点顺序与 Cloudflare 官方 DOM 一致（5,4,2-blur,3,2,1-shadow,1-blur,1） */}
          <div className="askai-cloud-node askai-cloud-node-5" />
          <div className="askai-cloud-node askai-cloud-node-4" />
          <div className="askai-cloud-node askai-cloud-node-2-blur" />
          <div className="askai-cloud-node askai-cloud-node-3" />
          <div className="askai-cloud-node askai-cloud-node-2" />
          <div className="askai-cloud-node askai-cloud-node-1-shadow" />
          <div className="askai-cloud-node askai-cloud-node-1-blur" />
          <div className="askai-cloud-node askai-cloud-node-1" />
        </div>
        <div className="text-center">
          <h3 className="mb-1.5 text-lg font-medium text-kumo-default">{greeting}</h3>
          <p className="text-sm text-kumo-subtle">今天想做什么？</p>
        </div>

        <div className="flex w-full max-w-[300px] flex-col gap-1.5">
          {SUGGESTED_PROMPTS.map((p, i) => {
            const PromptIcon = PROMPT_ICONS[i % PROMPT_ICONS.length];
            return (
              <Button
                key={p.title}
                size="sm"
                variant="ghost"
                type="button"
                onClick={() => onPrompt(p.subtitle || p.title)}
                className="group relative flex !h-auto w-full cursor-pointer items-center gap-3 rounded-xl border border-kumo-line/50 bg-kumo-elevated p-2 text-left transition-all duration-200 hover:border-brand/40 hover:bg-kumo-base hover:shadow-[0_0_12px_-2px_var(--color-kumo-shadow-drop)]"
              >
                <span className="absolute left-0 top-1/2 h-0 w-[2px] -translate-y-1/2 rounded-full bg-gradient-to-b from-brand/80 to-brand transition-all duration-200 group-hover:h-5" />
                <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-kumo-fill/80 transition-colors duration-200 group-hover:bg-brand/10 dark:bg-kumo-control/60 dark:group-hover:bg-brand/20">
                  <PromptIcon className="h-3.5 w-3.5 text-kumo-subtle transition-colors duration-200 group-hover:text-brand" />
                </span>
                <span className="flex min-w-0 flex-col">
                  <span className="truncate text-xs font-medium text-kumo-subtle transition-colors group-hover:text-kumo-default">{p.title}</span>
                  <span className="truncate text-xs text-kumo-subtle">{p.subtitle}</span>
                </span>
              </Button>
            );
          })}
        </div>
      </div>
    </div>
  );
}

/* ---------- 会话来源判定与分组 ---------- */
// 机器人/自动化来源的会话（定时任务 cron、Telegram 频道 channel:*）由对应流程
// 管理上下文，用户在前端只能查看（只读），不能继续对话，避免污染机器人上下文。
function isBotSession(session) {
  return !!session && !!session.source && session.source !== 'web';
}

function sessionSourceLabel(source, channelType) {
  if (source === 'cron') return '任务';
  if (channelType === 'wechat') return '微信';
  if (channelType === 'telegram') return 'TG';
  if (channelType === 'wecom') return '企微';
  return 'BOT';
}

/* 会话列表条目（全屏侧栏与下拉菜单共用）：机器人会话带来源标签 */
function SessionItem({ s, active, deleteArmed, onSelect, onDelete }) {
  const bot = isBotSession(s);
  // 不同渠道用对应品牌图标（TG/微信），其余（web/BOT 通用）沿用聊天气泡图标
  let ChannelIcon = ChatsCircle;
  if (s.channelType === 'wechat') ChannelIcon = WechatBrand;
  else if (s.channelType === 'telegram') ChannelIcon = TelegramBrand;
  else if (s.channelType === 'wecom') ChannelIcon = WeComBrand;
  return (
    <div className="group relative">
      <Sidebar.MenuButton
        active={active}
        aria-current={active ? 'page' : undefined}
        onClick={onSelect}
        icon={
          <ChannelIcon
            weight={s.channelType === 'wechat' || s.channelType === 'telegram' || s.channelType === 'wecom' ? undefined : 'duotone'}
            className={`${s.channelType === 'wechat' || s.channelType === 'telegram' || s.channelType === 'wecom' ? 'size-5' : 'size-4'} shrink-0 transition-all duration-200 ${
              active
                ? 'text-brand'
                : 'text-kumo-subtle group-hover:scale-110 group-hover:text-kumo-default'
            }`}
          />
        }
        className={`${active ? '!bg-brand/10' : ''} !px-2`}
      >
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex min-w-0 items-center gap-1.5">
            <span
              className={`truncate text-xs transition-colors ${
                active
                  ? 'font-semibold text-kumo-default'
                  : 'font-medium text-kumo-subtle group-hover:text-kumo-default'
              }`}
            >
              {s.title || '新对话'}
            </span>
            {bot && (
              <span className="shrink-0 rounded bg-kumo-warning/10 px-1 py-px text-[9px] font-semibold leading-4 text-kumo-warning">
                {sessionSourceLabel(s.source, s.channelType)}
              </span>
            )}
          </span>
          <span className="truncate text-[10px] text-kumo-subtle/70">
            {formatSessionDate(s.createdAt)}
          </span>
        </span>
      </Sidebar.MenuButton>
      <Button
        size="sm"
        shape="square"
        variant={deleteArmed ? 'destructive' : 'ghost'}
        aria-label="删除会话"
        onClick={() => onDelete(s.id)}
        className={`!absolute right-1.5 top-1/2 z-10 -translate-y-1/2 !h-6 !w-6 !rounded-md !shadow-sm opacity-0 transition-all duration-200 group-hover:opacity-100 ${
          deleteArmed
            ? '!opacity-100 !bg-kumo-danger !text-kumo-inverse'
            : '!bg-kumo-base ring-1 ring-kumo-line hover:!bg-kumo-tint hover:!text-kumo-danger'
        }`}
      >
        {deleteArmed ? <Check className="h-3 w-3" /> : <Trash className="h-3 w-3" />}
      </Button>
    </div>
  );
}

function sessionActivityTime(s) {
  const value = s.lastActivityAt || s.createdAt;
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? 0 : d.getTime();
}

// 定时任务会话（source=cron）按会话标题（即任务名）合并成组，
// 组内按最近活跃倒序，组间按组内最新活跃倒序。
function groupCronSessions(sessions) {
  const byTitle = new Map();
  for (const s of sessions) {
    const key = s.title || '未命名任务';
    if (!byTitle.has(key)) byTitle.set(key, []);
    byTitle.get(key).push(s);
  }
  const groups = [];
  for (const [title, items] of byTitle) {
    const sorted = [...items].sort((a, b) => sessionActivityTime(b) - sessionActivityTime(a));
    groups.push({ title, items: sorted, latestAt: sessionActivityTime(sorted[0]) });
  }
  groups.sort((a, b) => b.latestAt - a.latestAt);
  return groups;
}

/* 任务会话列表：定时任务（source=cron）按任务名分组，组默认折叠，点击组头展开 */
function BotTaskList({
  taskGroups,
  collapsedTaskGroups,
  onToggleTaskGroup,
  activeSessionId,
  deleteIsArmed,
  onSelect,
  onDelete,
}) {
  return (
    <>
      {taskGroups.map((group) => {
        const collapsed = collapsedTaskGroups === null || collapsedTaskGroups.has(group.title);
        return (
          <div key={group.title}>
            <Sidebar.MenuButton
              active={!collapsed}
              onClick={() => onToggleTaskGroup(group.title)}
              icon={<Terminal className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />}
              className="!px-2"
            >
              <span className="min-w-0 flex-1 truncate text-xs font-semibold text-kumo-default">
                {group.title}
              </span>
              <span className="shrink-0 text-[10px] text-kumo-subtle">{group.items.length} 次</span>
              <ChevronDown
                className={`h-3 w-3 shrink-0 text-kumo-subtle transition-transform duration-200 ${collapsed ? '' : 'rotate-180'}`}
              />
            </Sidebar.MenuButton>
            {!collapsed && (
              <div className="ml-2.5 border-l border-kumo-line pl-1.5">
                {group.items.map((s) => (
                  <SessionItem
                    key={s.id}
                    s={s}
                    active={s.id === activeSessionId}
                    deleteArmed={deleteIsArmed(s.id)}
                    onSelect={() => onSelect(s)}
                    onDelete={onDelete}
                  />
                ))}
              </div>
            )}
          </div>
        );
      })}
    </>
  );
}

/* 会话标题：有标题直接显示；空会话按实时时间兜底，时钟只重渲染本组件
   （避免把每秒 setState 提到面板主组件导致整棵会话/消息树反复渲染） */
function SessionTitleText({ title, active }) {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    if (title || !active) return undefined;
    const t = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(t);
  }, [title, active]);
  return <>{title || (active ? new Date(now).toLocaleString('zh-CN') : '新对话')}</>;
}

/* ---------- 点阵背景（复用登录页 .cf-ai-background + surface 光斑） ---------- */
function DotGrid({ surfaceRef }) {
  return (
    <div className="pointer-events-none absolute inset-0" aria-hidden>
      <div ref={surfaceRef} className="cf-ai-background-surface cf-ai-background absolute inset-0" />
    </div>
  );
}

/* ---------- 行为模式分段切换（kumo Tabs segmented） ---------- */
const BEHAVIOR_TABS = [
  { value: 'agent', label: (<span className="inline-flex items-center gap-1"><Terminal className="h-3 w-3" />代理</span>) },
  { value: 'ask', label: (<span className="inline-flex items-center gap-1"><MessageSquare className="h-3 w-3" />询问</span>) },
];

/* ---------- @ 资源菜单（多类型：域名/主机/定时任务/CF 账号/Fly.io/Koyeb/调度节点/通知渠道） ---------- */
const MENTION_GROUPS = [
  { type: 'zone', label: '域名', icon: Globe },
  { type: 'host', label: '主机', icon: Server },
  { type: 'task', label: '定时任务', icon: Clock },
  { type: 'account', label: 'CF 账号', icon: Cloud },
  { type: 'flyio', label: 'Fly.io', icon: FlyIoBrand, sm: true },
  { type: 'koyeb', label: 'Koyeb', icon: KoyebBrand, sm: true },
  { type: 'node', label: '调度节点', icon: Sliders },
  { type: 'channel', label: '通知渠道', icon: Bell },
];

// 数组元素是否带资源标识（id/_id/appName/name）：区分真正的资源列表与
// 内部数据数组（如主机 info.disk），后者被拒绝，避免任意子值深入时被截胡
function isResourceArray(arr) {
  return arr.length > 0 && arr.every((el) => {
    if (!el || typeof el !== 'object') return false;
    return ['id', '_id', 'appName', 'name'].some((k) => el[k] !== undefined && el[k] !== null && el[k] !== '');
  });
}

// 列表响应宽容解析：精确键、信封（data/items/list）、跨元素合并（多账号 apps 嵌套）、
// 包裹穿透（koyeb accounts[].projects[].services）；内部数据数组会被过滤
function extractResourceList(data, keys) {
  if (!data) return [];
  const walk = (v, depth) => {
    if (depth > 4) return null;
    if (Array.isArray(v)) {
      if (v.length === 0 || typeof v[0] !== 'object') return null;
      // 收集所有元素的嵌套资源数组（flyio data[].apps 多账号、koyeb projects[].services 多项目）
      const gathered = [];
      let hit = false;
      for (const el of v) {
        if (!el || typeof el !== 'object') continue;
        for (const k of keys) {
          if (Array.isArray(el[k]) && el[k].length > 0 && typeof el[k][0] === 'object') {
            gathered.push(...el[k]);
            hit = true;
          }
        }
      }
      if (hit) return gathered;
      // 元素可能是包裹对象（account→projects），深入其子值找命中 keys 的数组
      for (const el of v) {
        for (const key of Object.keys(el)) {
          if (el[key] && typeof el[key] === 'object') {
            const r = walk(el[key], depth + 1);
            if (r) return r;
          }
        }
      }
      // 兜底：仅当元素带资源标识时才视为资源列表（拒绝 disk/cpu 等内部数据数组）
      return isResourceArray(v) ? v : null;
    }
    if (v && typeof v === 'object') {
      for (const k of keys) {
        if (Array.isArray(v[k])) return v[k];
      }
      for (const k of ['data', 'items', 'list', 'results']) {
        if (v[k] && typeof v[k] === 'object') {
          const hit = walk(v[k], depth + 1);
          if (hit) return hit;
        }
      }
      // 兜底：任意子值深入（如 koyeb data 形如 {accounts:[{projects:[{services}]}]}，
      // accounts 不在信封键内，必须遍历任意子值才能触达 services）
      for (const k of Object.keys(v)) {
        if (v[k] && typeof v[k] === 'object') {
          const hit = walk(v[k], depth + 1);
          if (hit) return hit;
        }
      }
    }
    return null;
  };
  return walk(data, 0) || [];
}

function AtResourceMenu({ resources, tab, setTab, q, setQ, loading, error, onInsert }) {
  const group = MENTION_GROUPS.find((g) => g.type === tab) || MENTION_GROUPS[0];
  const Icon = group.icon;
  const all = resources[tab] || [];
  const list = q
    ? all.filter((r) => (r.name || '').toLowerCase().includes(q.toLowerCase()))
    : all;
  return (
    <div className="absolute bottom-full left-2 z-40 mb-1 flex w-[22rem] overflow-hidden rounded-xl bg-kumo-base shadow-lg ring-1 ring-kumo-line dark:bg-kumo-base">
      {/* 左侧：资源类型导航（图标 + 名称）；minHeight 固定，不随右侧列表高度变化 */}
      <div className="flex w-max shrink-0 flex-col gap-0.5 border-r border-kumo-line bg-kumo-recessed/30 p-1" style={{ minHeight: 300 }}>
        {MENTION_GROUPS.map((g) => {
          const GI = g.icon;
          return (
            <Button
              key={g.type}
              type="button"
              size="sm"
              variant="ghost"
              title={g.label}
              onClick={() => { setTab(g.type); setQ(''); }}
              aria-label={g.label}
              className={`flex items-center gap-1.5 rounded-md px-2 py-1 text-[11px] transition-colors focus-visible:!outline-none ${
                tab === g.type
                  ? 'bg-brand/15 font-medium text-brand'
                  : 'text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default'
              }`}
            >
              <GI className={`${g.sm ? 'h-3 w-3' : 'h-3.5 w-3.5'} shrink-0`} style={g.sm ? { fontSize: '0.75rem' } : undefined} />
              <span className="whitespace-nowrap">{g.label}</span>
            </Button>
          );
        })}
      </div>
      {/* 右侧：当前类型资源列表（与左列等高，列表区占满剩余） */}
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="border-b border-kumo-line px-2 py-1.5">
          <Input
            size="sm"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={`搜索${group.label}…`}
            aria-label={`搜索${group.label}`}
            className="w-full rounded-md border border-kumo-line/60 bg-kumo-recessed/60 px-2 py-1 text-[11px] text-kumo-default outline-none placeholder:text-kumo-subtle/60 focus:border-kumo-brand/60"
          />
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-1">
          {loading && <p className="px-3 py-2 text-xs text-kumo-subtle">加载中…</p>}
          {!loading && !error && list.length === 0 && (
            <p className="px-3 py-2 text-xs text-kumo-subtle">暂无{q ? '匹配结果' : `可引用${group.label}`}</p>
          )}
          {!loading && error && <p className="px-3 py-2 text-xs text-kumo-subtle">加载失败</p>}
          {!loading && list.map((r) => (
            <Button
              key={r.id || r.name}
              size="sm"
              variant="ghost"
              type="button"
              onClick={() => onInsert(r)}
              className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-xs text-kumo-default hover:bg-kumo-tint"
            >
              <Icon className={`${group.sm ? 'h-3 w-3' : 'h-3.5 w-3.5'} shrink-0 text-kumo-subtle`} style={group.sm ? { fontSize: '0.75rem' } : undefined} />
              <span className="truncate">{r.name}</span>
            </Button>
          ))}
        </div>
      </div>
    </div>
  );
}
 export default function AskAiPanel() {
  const showAskAI = useStore((s) => s.showAskAI);
  const setShowAskAI = useStore((s) => s.setShowAskAI);

  const [sessions, setSessions] = useState([]);
  const [activeSessionId, setActiveSessionId] = useState(null);
  const [messages, setMessages] = useState([]);
  const [input, setInput] = useState('');
  const [streaming, setStreaming] = useState(false);
  const [runId, setRunId] = useState(null);
  // 外部来源 run 快照（MCP/API/BOT/定时任务，无 SSE）：{runId, phase}，
  // 来自消息接口的 activeRun；SSE 活跃期间置空（面板自己的 run 走 streaming）
  const [liveRun, setLiveRun] = useState(null);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionMenuOpen, setSessionMenuOpen] = useState(false);
  // 会话列表 tab：切换查看用户对话（web）或机器人会话（cron/channel，只读）。
  const [sessionListTab, setSessionListTab] = useState('web');
  const { isArmed, confirmPress } = useConfirmPress();
  const isAskAiSessionArmed = useCallback(
    (sessionId) => isArmed(`askai-session:${sessionId}`),
    [isArmed]
  );
  const [panelWidth, setPanelWidth] = useState(() => {
    try { const v = Number(localStorage.getItem('adminai-sidebar-w')); return (v >= PANEL_MIN_WIDTH && v <= PANEL_MAX_WIDTH) ? v : PANEL_DEFAULT_WIDTH; } catch { return PANEL_DEFAULT_WIDTH; }
  });
  const [expanded, setExpanded] = useState(false);
  const [fullscreenSidebar, setFullscreenSidebar] = useState(true);
  const [atMenuOpen, setAtMenuOpen] = useState(false);
  const [manageOpen, setManageOpen] = useState(false); // 管理视图（设置/频道/审计收进侧栏）
  const [adminTab, setAdminTab] = useState('settings'); // 管理视图 tab（与 AdminConsole 共享）
  const [pendingApprovals, setPendingApprovals] = useState([]); // 写操作审批浮层队列（approval 事件触发，多条排队不覆盖）
  const [externalRun, setExternalRun] = useState(null); // 外部来源（API/BOT）run 指示：{runId, phase}
  const [behavior, setBehavior] = useState(() => {
    try { return localStorage.getItem('adminai-behavior') === 'ask' ? 'ask' : 'agent'; } catch { return 'agent'; }
  });

  // 背景光斑（鼠标跟随橙色光斑）
  const spotlightSidebarRef = useCloudflareSpotlight();

  // @ 资源（可引用资源列表：域名/主机/定时任务/CF 账号；懒加载一次）
  const [resources, setResources] = useState({ zone: [], host: [], task: [], account: [], flyio: [], koyeb: [], node: [], channel: [] });
  const [atLoading, setAtLoading] = useState(false);
  const [atError, setAtError] = useState(false);
  const [atTab, setAtTab] = useState('zone');
  const [atQuery, setAtQuery] = useState('');
  const resourcesLoadedRef = useRef(false);
  // 当前输入框已引用（插入了 @名称）的资源：chips 展示，发送时随消息携带 {type, id, name}
  const [pendingMentions, setPendingMentions] = useState([]);

  // 侧栏打开时主内容让出宽度（MainLayout 主画布读 --askai-sidebar-w）；
  // --askai-panel-w 由拖拽/面板宽度驱动，re-render（如 SSE 流式）不会重置拖拽中的宽度
  useEffect(() => {
    document.documentElement.style.setProperty('--askai-panel-w', `${panelWidth}px`);
    document.documentElement.style.setProperty(
      '--askai-sidebar-w',
      showAskAI && !expanded ? `${panelWidth}px` : '0px'
    );
  }, [showAskAI, expanded, panelWidth]);
   const [animated, setAnimated] = useState(false);
  const mountedRef = useRef(false);
  useEffect(() => {
    if (!mountedRef.current) {
      mountedRef.current = true;
      const raf = requestAnimationFrame(() => setAnimated(showAskAI));
      return () => cancelAnimationFrame(raf);
    }
    setAnimated(showAskAI);
  }, [showAskAI]);

  const eventSource = useRef(null);
  const lastSeqRef = useRef(0); // SSE 最后收到的事件 seq（重连 fromSeq 依据）
  const retryCountRef = useRef(0); // 断线重连次数（指数退避上限）
  const retryTimerRef = useRef(null); // 重连定时器
  const healthTimerRef = useRef(null); // 连接健康确认定时器（稳定期后清零退避）
  const reconnectedRef = useRef(false); // 本轮 run 是否发生过重连（终态后拉历史兜底 full）
  const runIdRef = useRef(null); // 当前活跃 runId（跨渲染同步，取消/停止路径读取）
  const parseFailRef = useRef(0); // SSE 事件解析连续失败计数（防静默吞事件）
  const textareaRef = useRef(null);
  const dragState = useRef(null);
  const dragCleanupRef = useRef(null); // 拖拽监听器清理（unmount 兜底）
  const panelRef = useRef(null);
  const activeSessionIdRef = useRef(activeSessionId);
  const lastPromptRef = useRef('');
  const skipLoadSessionRef = useRef(null);
  const streamTargetIdRef = useRef(null);
  // 外部来源轮询：streamingRef 在面板自己的 run 流式输出时为 true（此时重拉会打断打字机）；
  // lastActivityRef 记录各会话最近一次 lastActivityAt，lastCountRef 记录 messageCount——
  // 外部 run（MCP/API/BOT 发送）没有 SSE 通道，其思考/工具/回复逐条落库时
  // messageCount 递增，轮询比对到变化即重拉消息，实现“逐步可见”而不必等 run 结束。
  const streamingRef = useRef(streaming);
  const lastActivityRef = useRef(new Map());
  const lastCountRef = useRef(new Map());

  const loadSessions = useCallback(async () => {
    setSessionsLoading(true);
    try {
      const res = await fetch('/api/admin-ai/sessions');
      const data = await res.json();
      const body = data.data || data;
      const list = body.sessions || [];
      setSessions(list);
      setSessions((prev) => [...list, ...prev.filter((p) => !list.some((s) => s.id === p.id))]);
      // 不在这里自动选中上次会话：首次打开面板时统一新建空会话（见 firstOpenRef 逻辑）
      return list;
    } catch {
      return [];
    } finally {
      setSessionsLoading(false);
    }
  }, []);

  const loadMessages = useCallback(async (sessionId) => {
    if (!sessionId) return;
    try {
      const res = await fetch(`/api/admin-ai/sessions/${sessionId}/messages`);
      const data = await res.json();
      // 会话守卫：await 期间用户可能已快速切换会话（A→B），晚到的
      // A 响应不得覆盖 B 的消息列表。此守卫也作用于轮询重拉路径。
      if (activeSessionIdRef.current !== sessionId) return;
      const body = data.data || data;
      const items = body.items || body.messages || [];
      // DB 行 → timeline parts（推理/工具调用/工具结果/正文按时间序，一轮一条消息）
      const live = body.activeRun && body.activeRun.runId
        ? { runId: body.activeRun.runId, phase: body.activeRun.phase || 'starting' }
        : null;
      setLiveRun(live);
      setMessages(markLiveMessage(buildTimelineFromRows(items), live));
    } catch {
    }
  }, []);

  useEffect(() => { loadSessions(); }, [loadSessions]);

  /* 首次打开面板：优先复用最近一个空会话（还没产生对话），没有则新建；
       不再自动接续有内容的旧会话（旧会话可在会话列表切换找回） */
  const firstOpenRef = useRef(false);
  useEffect(() => {
    if (!showAskAI || firstOpenRef.current) return;
    firstOpenRef.current = true;
    (async () => {
      if (!activeSessionIdRef.current) {
        try {
          const res = await fetch('/api/admin-ai/sessions');
          const data = await res.json();
          const body = data.data || data;
          const list = body.sessions || [];
          // 恢复上次打开的会话（localStorage 记忆）：刷新后面板重开直接回到原对话（需真实存在且非只读机器人会话）
          const storedId = readStoredActiveSession();
          if (storedId) {
            const saved = list.find((s) => s.id === storedId && !isBotSession(s));
            if (saved) {
              setSessions((prev) => (prev.some((p) => p.id === saved.id) ? prev : [saved, ...prev]));
              setActiveSessionId(saved.id);
              setMessages([]);
              return;
            }
          }
          // 列表按 lastActivityAt 倒序：取最近的空会话直接复用，避免空会话越积越多
          const emptyLast = list.find((s) => !isBotSession(s) && !(s.messageCount > 0));
          if (emptyLast) {
            setSessions((prev) => (prev.some((p) => p.id === emptyLast.id) ? prev : [emptyLast, ...prev]));
            skipLoadSessionRef.current = emptyLast.id;
            setActiveSessionId(emptyLast.id);
            setMessages([]);
            return;
          }
        } catch {
        }
      }
      try {
        const res = await fetch('/api/admin-ai/sessions', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ mode: behavior }),
        });
        const data = await res.json();
        const body = data.data || data;
        const newId = body.id || body.session?.id;
        if (!newId) return;
        skipLoadSessionRef.current = newId;
        setActiveSessionId(newId);
        setSessions((prev) => [{ id: newId, title: new Date().toLocaleString('zh-CN'), mode: behavior }, ...prev]);
        setMessages([]);
      } catch {
      }
    })();
  }, [showAskAI]);

  useEffect(() => { streamingRef.current = streaming; }, [streaming]);

  /* TG/定时任务等外部来源的对话没有推送到浏览器的通道（SSE 只覆盖面板自己发起的 run），
     面板打开期间轮询会话活动时间：活跃会话 lastActivityAt 有变化则重拉消息，会话列表同步刷新。 */
  useEffect(() => {
    if (!showAskAI) return undefined;
    let inFlight = false;
    const poll = async () => {
      // 面板自己刚发起的 run 正在流式时禁止重拉：loadMessages 会用数据库快照
      // 覆盖乐观 UI（含 assistant 占位/进行中正文），把正在打字机的回复冲掉。
      // 用 streamTargetIdRef 而非 streamingRef 判活：streamingRef 靠 useEffect
      // 异步同步，存在一帧空窗，SSE 打开后首个 2s 轮询会漏过闸门（曾实测命中）。
      // 根因：调 loadSessions 期间流已经打开，晚到的 loadMessages 仍会触发。
      if (inFlight || streamTargetIdRef.current || document.visibilityState !== 'visible') return;
      inFlight = true;
      try {
        const list = await loadSessions();
        // 二次校验：await 期间新 run 已开流（streamTargetIdRef 同步置位）→ 放弃本轮重拉
        if (streamTargetIdRef.current) return;
        const sid = activeSessionIdRef.current;
        const cur = list.find((s) => s.id === sid);
        if (cur) {
          const prevAt = lastActivityRef.current.get(sid);
          const prevCount = lastCountRef.current.get(sid);
          // 活动时间变化（run 起止）或消息数变化（外部 run 过程中逐条落库）→ 重拉消息
          if (prevAt !== undefined && (cur.lastActivityAt !== prevAt || (prevCount !== undefined && cur.messageCount !== prevCount))) {
            loadMessages(sid);
          }
          // 外部 run 进行中：更新运行指示（思考中/执行工具…），run 结束自动消失
          setExternalRun(cur.activeRun ? { runId: cur.activeRun.runId, phase: cur.activeRun.phase } : null);
          lastActivityRef.current.set(sid, cur.lastActivityAt);
          lastCountRef.current.set(sid, cur.messageCount);
        } else {
          setExternalRun(null);
        }
      } catch {
      } finally {
        inFlight = false;
      }
    };
    poll(); // 面板刚打开立即同步一次，避免等首个 3s 周期
    const timer = window.setInterval(poll, 2000);
    return () => window.clearInterval(timer);
  }, [showAskAI, loadSessions, loadMessages]);

  const stopStream = useCallback(() => {
    if (retryTimerRef.current) {
      clearTimeout(retryTimerRef.current);
      retryTimerRef.current = null;
    }
    if (eventSource.current) {
      eventSource.current.close();
      eventSource.current = null;
    }
    streamTargetIdRef.current = null;
    lastSeqRef.current = 0; // 跨 run 复位：同类 runId 的 resume 重连不再误跳过旧 seq
    reconnectedRef.current = false;
    retryCountRef.current = 0;
    runIdRef.current = null;
    setStreaming(false);
    setRunId(null);
    // 清掉外部 run 快照：SSE 收尾/取消后旧 liveRun 不得残留（切换会话时
    // 随后 loadMessages 会重设，无副作用）
    setLiveRun(null);
  }, []);

  /* 停止路径统一走后端取消：关侧栏/Esc/切会话/unmount 都只在前端关 EventSource
     会让后端 run 继续执行（含写工具），且审批事件随连接关闭永久丢失。
     本函数以「当前激活 run」为准发起 POST /cancel，之后再由调用方 stopStream 收尾。 */
  const cancelBackendRun = useCallback(async () => {
    const rid = runIdRef.current;
    if (!rid) return;
    try {
      await fetch('/api/admin-ai/cancel', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ runId: rid }),
      });
    } catch {
    }
  }, []);

  /* 切会话：先取消旧 run（防后台静默执行/审批丢失），再加载新会话消息。 */
  useEffect(() => {
    if (activeSessionId) {
      activeSessionIdRef.current = activeSessionId;
      storeActiveSession(activeSessionId);
      if (runIdRef.current) cancelBackendRun();
      stopStream();
      setPendingApprovals([]); // 清掉旧会话遗留的审批浮层（切走即失效）
      if (skipLoadSessionRef.current === activeSessionId) {
        skipLoadSessionRef.current = null;
        return undefined;
      }
      loadMessages(activeSessionId);
    }
    return undefined;
  }, [activeSessionId, loadMessages, stopStream, cancelBackendRun]);

  useEffect(() => {
    const kill = () => { if (runIdRef.current) cancelBackendRun(); stopStream(); };
    return kill;
  }, [stopStream, cancelBackendRun]);
   /* Esc 关闭侧栏（管理视图先返回对话，全屏模式先收回侧栏形态） */
  useEffect(() => {
    if (!showAskAI) return undefined;
    const onKey = (e) => {
      if (e.key === 'Escape') {
        if (manageOpen) setManageOpen(false);
        else if (expanded) setExpanded(false);
        else { if (runIdRef.current) cancelBackendRun(); stopStream(); setShowAskAI(false); }
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [showAskAI, expanded, manageOpen, setShowAskAI, cancelBackendRun, stopStream]);

  /* 菜单外部点击关闭（@ / 会话） */
  useEffect(() => {
    if (!showAskAI) return undefined;
    const onDown = (e) => {
      if (e.target && e.target.closest && e.target.closest('[data-askai-menu]')) return;
      setSessionMenuOpen(false);
      setAtMenuOpen(false);
    };
    window.addEventListener('mousedown', onDown);
    return () => window.removeEventListener('mousedown', onDown);
  }, [showAskAI]);

  /* SSE 断线自动重连：网络抖动/代理切换/服务端释放连接时，后台 run 仍在执行，
     凭 runId + fromSeq 指数退避重连恢复事件流（终态/工具状态事件由后端缓冲重放，
     delta/reasoning 增量不重放避免文本重复拼接，最终内容由重连后拉取消息历史兜底）。
     重试耗尽才回退「取消占位」旧行为，杜绝“没有回复/永远 streaming”。 */
  const STREAM_RECONNECT_DELAYS = [1000, 2000, 4000, 8000, 15000];
  const STREAM_RECONNECT_MAX = 5;

  const openStream = (runId, targetId, resume) => {
    // 先清掉带触发中的重连定时器：上一 run 挂起的重连定时器若不清理，
    // 会在新 run 打开流后 1-15s 内触发 openStream 并替换新 EventSource，
    // 导致 run2 事件整体丢失、消息永远停在 streaming。
    if (retryTimerRef.current) {
      clearTimeout(retryTimerRef.current);
      retryTimerRef.current = null;
    }
    if (eventSource.current) {
      eventSource.current.close();
      eventSource.current = null;
    }
    streamTargetIdRef.current = targetId;
    if (!resume) {
      retryCountRef.current = 0;
      reconnectedRef.current = false;
      parseFailRef.current = 0;
      setRunId(runId);
      runIdRef.current = runId;
      setStreaming(true);
    }
    const q = resume ? `&resume=1&fromSeq=${lastSeqRef.current || 0}` : '';
    const es = new EventSource(`/api/admin-ai/messages/stream?runId=${runId}${q}`);
    eventSource.current = es;

    es.onopen = () => {
      // 不立即清零退避计数：抖动场景（能连上又马上断开）下立即清零会
      // 让重连永远停在 1s；连接稳定 30s 后才视为健康并重置退避。
      if (healthTimerRef.current) clearTimeout(healthTimerRef.current);
      healthTimerRef.current = window.setTimeout(() => {
        healthTimerRef.current = null;
        retryCountRef.current = 0;
      }, 30000);
    };

    const applyEvent = (raw) => {
      const ev = normalizeAiEvent(raw);
      if (!ev) return;
      if (ev.type === 'error') ev.retryPrompt = lastPromptRef.current;
      // AI 生成会话标题：更新本地会话列表，等待中的占位标题被真实标题替换
      if (ev.type === 'session_title' && ev.sessionId) {
        if (ev.title) {
          setSessions((prev) => prev.map((s) => (s.id === ev.sessionId ? { ...s, title: ev.title } : s)));
        }
        return;
      }
      // 固化 targetId：setMessages 的 updater 异步执行时，streamTargetIdRef
      // 可能已被 stopStream（done/error 后同步调用）清空，导致 findTarget 失败、
      // 消息永远停留在 streaming（正文/完成态丢失）
      const tid = streamTargetIdRef.current;
      setMessages((prev) => applyAiEvent(prev, ev, tid));
      if (ev.type === 'approval') {
        // 入队而非覆盖：多个写操作并发审批时全部展示，逐条处理，避免静默漏批
        setPendingApprovals((prev) => (prev.some((a) => a.approvalId === ev.approvalId)
          ? prev
          : [...prev, ev]));
      }
      if (ev.type === 'done' || ev.type === 'error') {
        // 只允许「自己所属的活跃流」收尾时 stopStream：上一轮收尾瞬间新一轮已开流
        // （openStream 已替换 eventSource.current）时，旧流残留的 done/error 事件
        // 不得误杀新流，否则新一轮 WebSocket 事件全部丢失、消息像被吞掉。
        if (es === eventSource.current) stopStream();
        // 本轮发生过断线重连：增量内容可能缺失，拉取服务端完整历史替换占位消息
        if (reconnectedRef.current) loadMessages(activeSessionIdRef.current);
      }
    };

    const wireEvent = (t) => es.addEventListener(t, (e) => {
      try {
        const seq = Number(e.lastEventId || 0);
        if (seq > (lastSeqRef.current || 0)) lastSeqRef.current = seq;
        const ev = parseAdminAiEvent(t, e.data);
        parseFailRef.current = 0;
        applyEvent(ev);
      } catch (err) {
        // 不静默吞：单条坏事件记录；连续多次解析失败说明流已损坏，
        // 主动回退拉历史兜底，避免「看似无响应」但毫无提示。
        parseFailRef.current += 1;
        if (parseFailRef.current >= 3) {
          parseFailRef.current = 0;
          if (streamTargetIdRef.current) loadMessages(activeSessionIdRef.current);
        }
      }
    });
    for (const t of STREAM_EVENTS) wireEvent(t);

    es.onerror = () => {
      // 连接层失败（网络断开/服务端退出）：先关闭当前 EventSource（避免浏览器
      // 对同 URL 无限自动重连、走后端 404 循环），再手动指数退避重连。
      if (healthTimerRef.current) {
        clearTimeout(healthTimerRef.current);
        healthTimerRef.current = null;
      }
      if (!streamTargetIdRef.current) return; // stopStream 已清理（手动取消/切换会话）
      const tid = streamTargetIdRef.current;
      es.close();
      eventSource.current = null;
      scheduleReconnect(runId, tid);
    };
  };

  /* 指数退避重连（1s→2s→4s→8s→15s，5 次封顶）；耗尽后回退旧行为：取消当前流目标 */
  const scheduleReconnect = (runId, targetId) => {
    const attempt = retryCountRef.current;
    if (attempt >= STREAM_RECONNECT_MAX) {
      stopStream();
      if (targetId) setMessages((prev) => cancelMessage(prev, targetId));
      return;
    }
    retryCountRef.current = attempt + 1;
    reconnectedRef.current = true;
    const delay = STREAM_RECONNECT_DELAYS[Math.min(attempt, STREAM_RECONNECT_DELAYS.length - 1)];
    retryTimerRef.current = window.setTimeout(() => {
      retryTimerRef.current = null;
      openStream(runId, targetId, true);
    }, delay);
  };
   /* @ 资源：懒加载四类引用资源（域名用聚合接口覆盖全部账号；单类失败不阻塞其他类） */
  const loadResources = useCallback(async () => {
    if (resourcesLoadedRef.current || atLoading) return;
    setAtLoading(true);
    setAtError(false);
    const fetchBucket = async (type, path, keys) => {
      try {
        const res = await fetch(path);
        const data = await res.json();
        const body = data && data.data !== undefined ? data.data : data;
        const raw = extractResourceList(body, keys);
        return raw
          .map((r) => ({
            type,
            id: r.id ?? r._id ?? r.appName ?? r.channelId ?? r.name,
            name: r.name ?? r.appName ?? r.title ?? r.hostname ?? r.domain ?? r.botUsername ?? '',
          }))
          .filter((r) => r.id && r.name);
      } catch {
        return null;
      }
    };
    const [zones, hosts, tasks, accounts, flyioApps, koyebServices, nodes, channels] = await Promise.all([
      fetchBucket('zone', '/api/cloudflare/zones', ['zones']),
      fetchBucket('host', '/api/server/accounts', ['accounts']),
      fetchBucket('task', '/api/scheduler/tasks', ['tasks']),
      fetchBucket('account', '/api/cloudflare/accounts', ['accounts']),
      fetchBucket('flyio', '/api/flyio/proxy/apps', ['apps', 'applications']),
      fetchBucket('koyeb', '/api/koyeb/data', ['services']),
      fetchBucket('node', '/api/scheduler/nodes', ['nodes']),
      fetchBucket('channel', '/api/notification/channels', ['channels']),
    ]);
    const next = {};
    let failed = false;
    [['zone', zones], ['host', hosts], ['task', tasks], ['account', accounts], ['flyio', flyioApps], ['koyeb', koyebServices], ['node', nodes], ['channel', channels]].forEach(([k, v]) => {
      if (v === null) { failed = true; next[k] = []; } else { next[k] = v; }
    });
    if (failed) setAtError(true);
    setResources((prev) => ({ ...prev, ...next }));
    resourcesLoadedRef.current = true;
    setAtLoading(false);
  }, [atLoading]);

  /* 输入检测 @ 触发资源菜单 */
  const handleInputChange = (e) => {
    const value = e.target.value;
    setInput(value);
    const atIdx = value.lastIndexOf('@');
    if (atIdx >= 0) {
      const after = value.slice(atIdx + 1);
      if (!after.includes(' ')) {
        loadResources();
        setAtMenuOpen(true);
      } else {
        setAtMenuOpen(false);
      }
    } else {
      setAtMenuOpen(false);
    }
  };

  const insertAtResource = (res) => {
    const name = res?.name || '';
    if (!name) return;
    // 资源以 pill 形式驻留输入区（不可编辑的独立块），不写入可编辑文本：
    // 光标保持在用户文本末尾，且清掉触发菜单的 @ 及未完成输入
    const atIdx = input.lastIndexOf('@');
    if (atIdx >= 0) {
      const next = input.slice(0, atIdx);
      setInput(next);
      setTimeout(() => {
        if (textareaRef.current) {
          textareaRef.current.value = next;
          resizeTextarea();
          textareaRef.current.focus();
          textareaRef.current.setSelectionRange(next.length, next.length);
        }
      }, 0);
    } else {
      setTimeout(() => { if (textareaRef.current) { textareaRef.current.focus(); } }, 0);
    }
    // 记录结构化引用（type+id 随消息发送，服务端拉取实时快照注入上下文）
    if (res.type && res.id) {
      setPendingMentions((prev) => {
        const next = prev.filter((m) => !(m.type === res.type && m.id === res.id));
        return [...next, { type: res.type, id: res.id, name }];
      });
    }
    setAtMenuOpen(false);
    setAtQuery('');
  };

  /* 移除资源 pill：chips 为权威（文本中无对应片段，无需清理文本） */
  const removeMention = (m) => {
    setPendingMentions((prev) => prev.filter((x) => !(x.type === m.type && x.id === m.id)));
    setTimeout(() => { if (textareaRef.current) textareaRef.current.focus(); }, 0);
  };
   const chooseBehavior = (mode) => {
    setBehavior(mode);
    try { localStorage.setItem('adminai-behavior', mode); } catch { }
  };
   /* 发起一轮执行：发送 prompt 并将流式响应挂到 assistantId 消息；rewindId 为编辑重发的服务端截断点；
      join 为运行中追问（join 语义）：服务端把消息入队由活跃 run 续跑，前端不重开流，轮次分段自动建段 */
  const startRun = async (sessionId, trimmed, assistantId, rewindId, join, mentions) => {
    lastPromptRef.current = trimmed;
    const failWith = (message) => {
      setMessages((prev) => failMessage(prev, assistantId, message, trimmed));
    };
    try {
      const res = await fetch('/api/admin-ai/messages', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          sessionId,
          prompt: trimmed,
          mode: behavior,
          ...(mentions && mentions.length > 0 ? { mentions } : {}),
          ...(rewindId ? { rewindId } : {}),
        }),
      });
      if (!res.ok) {
        let msg = `发送失败（HTTP ${res.status}）`;
        try {
          const data = await res.json();
          msg = (data?.error?.message) || msg;
        } catch { }
        failWith(msg);
        return;
      }
      const data = await res.json();
      const body = data && data.data ? data.data : data;
      if (body.runId) {
        if (body.queued) {
          // 追问已入队：占位无事件来源（轮次分段按 userMessageId 自动建段），先移除
          if (join) setMessages((prev) => prev.filter((m) => m.id !== assistantId));
          // 旧流已结束（服务端 run 尚在收尾/已进入下一轮）→ 接流补收后续轮次事件
          if (!streamingRef.current) openStream(body.runId, assistantId, false);
        } else {
          openStream(body.runId, assistantId, false);
        }
      } else failWith('未能启动执行，请重试');
    } catch {
      failWith('发送失败，请重试');
    }
  };

  const handleSend = async (promptOverride) => {
    const trimmed = (promptOverride === undefined ? input : promptOverride).trim();
    if (!trimmed || botActive) return;
    // 机器人会话只读：禁止继续对话，避免污染机器人流程的上下文。
    // 注意：streaming 期间允许发送（join 语义：追问入队，由当前执行续跑消费，不再 409）
    const join = streaming;

    if (promptOverride === undefined) setInput('');
    setAtMenuOpen(false);
    let sessionId = activeSessionId;

    if (!sessions.find((s) => s.id === sessionId)) {
      try {
        const res = await fetch('/api/admin-ai/sessions', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ mode: behavior }),
        });
        const data = await res.json();
        const body = data.data || data;
        sessionId = body.id || body.session?.id;
        if (!sessionId) return;
        skipLoadSessionRef.current = sessionId;
        setActiveSessionId(sessionId);
        setSessions((prev) => [{ id: sessionId, title: new Date().toLocaleString('zh-CN'), mode: behavior }, ...prev]);
      } catch {
        // 创建会话失败：用现行 parts 模型落地错误消息（旧 blocks 模型已无人渲染，会变成空气泡）
        setMessages((prev) => [...prev, {
          ...createAssistantMessage(`err_${Date.now()}`, null, MSG.ERROR),
          active: false,
          parts: [{ type: 'error', message: '创建会话失败，请重试', retryable: true, retryPrompt: trimmed }],
        }]);
        return;
      }
    }

    const assistantMsgId = `assistant_${Date.now()}`;
    const mentions = pendingMentions;
    setPendingMentions([]);
    setMessages((prev) => [...prev,
      { ...createUserMessage(`user_${Date.now()}`, trimmed), mentions: mentions.length > 0 ? mentions.map((m) => ({ ...m })) : undefined },
      createAssistantMessage(assistantMsgId),
    ]);
    await startRun(sessionId, trimmed, assistantMsgId, undefined, join, mentions);
  };

  /* 编辑用户消息并重发：截断其后所有消息，更新文本后重新执行 */
  const handleEditResend = async (messageId, newText) => {
    const trimmed = newText.trim();
    if (!trimmed || streaming) return;
    // 机器人会话只读：禁止编辑重发。
    if (botActive) return;
    const target = messages.find((m) => m.id === messageId);
    // 服务端截断依据：流事件已把真实消息 id（aam_…）记在 dbId 上；历史加载的消息 id 本身即 DB id
    const rewindId = target?.dbId || (target?.id.startsWith('aam_') ? target.id : undefined);
    const mentions = target?.mentions || [];
    const assistantMsgId = `assistant_${Date.now()}`;
    setMessages((prev) => {
      const idx = prev.findIndex((m) => m.id === messageId);
      if (idx === -1) return [...prev, createAssistantMessage(assistantMsgId)];
      const base = prev.slice(0, idx + 1).map((m, i) => (i === idx ? { ...m, content: trimmed } : m));
      return [...base, createAssistantMessage(assistantMsgId)];
    });
    await startRun(activeSessionId, trimmed, assistantMsgId, rewindId, false, mentions);
  };

  const handleCancel = async () => {
    const tid = streamTargetIdRef.current;
    if (runId) {
      try {
        await fetch('/api/admin-ai/cancel', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ runId }),
        });
      } catch {
      }
    }
    if (tid) setMessages((prev) => cancelMessage(prev, tid));
    stopStream();
  };

  const handleNewSession = async () => {
    try {
      const res = await fetch('/api/admin-ai/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      });
      const data = await res.json();
      const body = data.data || data;
      const newSession = { id: body.id || body.session?.id, title: new Date().toLocaleString('zh-CN') };
      setSessions((prev) => [newSession, ...prev]);
      setActiveSessionId(newSession.id);
      setMessages([]);
      setSessionMenuOpen(false);
    } catch {
    }
  };

  /* 会话删除：二次确认 */
  const handleDeleteSession = async (sessionId) => {
    try {
      await fetch(`/api/admin-ai/sessions/${sessionId}`, { method: 'DELETE' });
      setSessions((prev) => prev.filter((s) => s.id !== sessionId));
      if (activeSessionId === sessionId) {
        setActiveSessionId(sessions.find((s) => s.id !== sessionId)?.id || null);
        setMessages([]);
      }
    } catch {
    }
  };

  const requestDelete = (sessionId) => {
    if (confirmPress(`askai-session:${sessionId}`, '删除会话')) {
      handleDeleteSession(sessionId);
    }
  };
   const handleResolveApproval = async (approvalId, action, applyToSession, reason) => {
    try {
      const res = await fetch(`/api/admin-ai/approvals/${approvalId}/resolve`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action, applyToSession: !!applyToSession, reason: reason || '' }),
      });
      // 400/409：读取后端原因回显（过期/已处理/执行已结束），不静默消失，并出队
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        const msg = data?.error?.message || data?.error || `审批操作失败（HTTP ${res.status}）`;
        setMessages((prev) => prev.map((m) => {
          if (!m.parts || !m.parts.some((p) => p.type === 'approval' && p.approvalId === approvalId)) return m;
          return {
            ...m,
            parts: m.parts.map((p) => (p.type === 'approval' && p.approvalId === approvalId
              ? { ...p, status: 'error', errorMessage: msg }
              : p)),
          };
        }));
        setPendingApprovals((prev) => prev.filter((p) => p.approvalId !== approvalId));
        return;
      }
      setMessages((prev) => resolveApprovalPart(prev, approvalId, action));
      setPendingApprovals((prev) => prev.filter((p) => p.approvalId !== approvalId));
      loadSessions();
    } catch {
    }
  };

  /* 撤销当前会话的「允许此对话」写授权（后端写入即清 write_enabled 与有效期） */
  const handleRevokeSessionWrite = async (sessionId) => {
    if (!sessionId) return;
    try {
      await fetch(`/api/admin-ai/sessions/${sessionId}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ writeEnabled: false }),
      });
      setSessions((prev) => prev.map((s) => (s.id === sessionId ? { ...s, writeEnabled: false } : s)));
    } catch {
    }
  };

  /* 审批事件 → ApprovalCard props（侧栏内浮层，不遮挡主画布） */
  const approvalProps = (p) => ({
    id: p.approvalId,
    planSummary: p.planSummary,
    method: p.method,
    path: p.path,
    bodySnapshot: p.bodySnapshot,
    expiresAt: p.expiresAt,
    status: 'pending',
  });

  /* 审批浮层（侧栏/全屏各一份，出现在 AI 面板内部）；多条审批排队，逐条展示队首 */
  const approvalOverlay = (positionClass) => {
    const current = pendingApprovals[0];
    if (!current) return null;
    return (
      <div className={`askai-modal-in absolute z-40 ${positionClass}`}>
        <ApprovalCard approval={approvalProps(current)} onResolve={handleResolveApproval} remaining={pendingApprovals.length} />
      </div>
    );
  };

  /* textarea 自动增高（最大 256px） */
  const resizeTextarea = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 256)}px`;
  }, []);
  // input 变化（含发送后清空）自动重算高度，避免发送后残留拉高不恢复
  useEffect(() => {
    resizeTextarea();
  }, [input, resizeTextarea]);

  /* 回车发送，Shift+Enter 换行（中文输入法组词回车不发） */
  const handleTextareaKeyDown = (e) => {
    if (e.key !== 'Enter') return;
    if (e.shiftKey || e.nativeEvent.isComposing) return;
    e.preventDefault();
    handleSend();
  };

  /* 拖宽手柄：直接操作 panelRef.style.width + CSS 变量，不触发 React 重渲染 */
  const applyPanelWidth = (w) => {
    const clamped = Math.max(PANEL_MIN_WIDTH, Math.min(PANEL_MAX_WIDTH, w));
    if (panelRef.current) panelRef.current.style.width = `${clamped}px`;
    document.documentElement.style.setProperty('--askai-panel-w', `${clamped}px`);
    document.documentElement.style.setProperty('--askai-sidebar-w', showAskAI && !expanded ? `${clamped}px` : '0px');
    return clamped;
  };

  const startDrag = (e) => {
    e.preventDefault();
    if (dragState.current) return; // 拖拽进行中防重入
    document.body.style.userSelect = 'none';
    document.body.style.cursor = 'col-resize';
    if (panelRef.current) panelRef.current.style.transition = 'none'; // 拖拽中禁用宽度过渡
    dragState.current = { startX: e.clientX, startWidth: panelWidth };
    const onMove = (ev) => {
      if (!dragState.current) return;
      const next = dragState.current.startWidth + (dragState.current.startX - ev.clientX);
      applyPanelWidth(next);
    };
    const onUp = (ev) => {
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
      if (panelRef.current) panelRef.current.style.transition = '';
      if (dragState.current) {
        const next = dragState.current.startWidth + (dragState.current.startX - ev.clientX);
        const clamped = applyPanelWidth(next);
        setPanelWidth(clamped);
        try { localStorage.setItem('adminai-sidebar-w', String(clamped)); } catch { }
      }
      dragState.current = null;
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
    // 引用挂到 ref，卸载时也能移除（避免残留监听器每帧写 style.width/锁 body 光标）
    dragCleanupRef.current = () => {
      dragState.current = null;
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
      if (panelRef.current) panelRef.current.style.transition = '';
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  };

  useEffect(() => () => { if (dragCleanupRef.current) dragCleanupRef.current(); }, []);

  const activeSessionRow = sessions.find((s) => s.id === activeSessionId);
  const placeholder = behavior === 'ask' ? '输入指令' : '输入消息，@ 引用资源';
  // 会话隔离：用户主动发起的（web）与机器人/自动化来源（cron/channel）分开管理，
  // 机器人会话只读（可查看历史，禁输入），避免用户消息污染机器人流程的上下文。
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const botActive = isBotSession(activeSession);
  // 会话列表只展示已产生对话的会话：空会话是「新对话」占位，不进入下拉列表
  const webSessions = sessions.filter((s) => !isBotSession(s) && (s.messageCount || 0) > 0);
  const botSessions = sessions.filter((s) => isBotSession(s) && (s.messageCount || 0) > 0);
  // BOT（频道 channel:*）与任务（cron）分 tab 展示；任务按任务名合并成组。
  const channelSessions = botSessions.filter((s) => s.source !== 'cron');
  // 频道会话按渠道分组（Telegram / 微信 / 其他），避免混在一起。
  const channelGroups = useMemo(() => {
    const groups = [];
    const tg = channelSessions.filter((s) => s.channelType === 'telegram');
    const wx = channelSessions.filter((s) => s.channelType === 'wechat');
    const other = channelSessions.filter((s) => s.channelType !== 'telegram' && s.channelType !== 'wechat');
    if (tg.length) groups.push({ key: 'telegram', label: 'Telegram', items: tg });
    if (wx.length) groups.push({ key: 'wechat', label: '微信', items: wx });
    if (other.length) groups.push({ key: 'bot', label: 'BOT', items: other });
    return groups;
  }, [channelSessions]);
  const taskGroups = useMemo(
    () => groupCronSessions(sessions.filter((s) => isBotSession(s) && s.source === 'cron' && (s.messageCount || 0) > 0)),
    [sessions],
  );
  // 任务分组折叠状态：null = 全部折叠（初始）；否则 Set 内的任务名收起
  const [collapsedTaskGroups, setCollapsedTaskGroups] = useState(null);
  const toggleTaskGroup = useCallback((title) => {
    setCollapsedTaskGroups((prev) => {
      if (prev === null) {
        return new Set(taskGroups.filter((g) => g.title !== title).map((g) => g.title));
      }
      const next = new Set(prev);
      if (next.has(title)) next.delete(title); else next.add(title);
      return next;
    });
  }, [taskGroups]);
   /* ==================== 渲染 ==================== */
  const closeSidebar = () => {
    // 关闭侧栏不等同停止：run 仍在后台执行（含写工具）会造成「关掉还在跑」，
    // 先发起后端取消（恢复正常路径），再收起面板。
    if (runIdRef.current) cancelBackendRun();
    stopStream();
    setShowAskAI(false);
    setExpanded(false);
    setManageOpen(false);
  };

  /* 外部来源 run 指示器：嵌入输入框工具行（模式切换右侧）。
     runId 变化重挂载重播滑入动画；阶段切换时文案以 key 变化淡入 */
  const externalRunIndicator = externalRun && (
    <div
      key={externalRun.runId}
      className="askai-external-run flex min-w-0 items-center gap-1.5 text-[11px] text-kumo-subtle"
      data-external-run-indicator
    >
      <span className="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-kumo-warning" aria-hidden />
      <span key={externalRun.phase} className="askai-external-run-phase min-w-0 truncate">
        {externalRun.phase === 'tooling' ? '正在执行工具…' : externalRun.phase === 'starting' ? '正在启动…' : '正在思考…'}
      </span>
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={async () => {
          const runId = externalRun.runId;
          setExternalRun(null);
          try {
            await fetch('/api/admin-ai/cancel', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ runId }),
            });
          } catch {
          }
        }}
        aria-label="停止外部任务"
        className="!h-5 shrink-0 !rounded !px-1 !text-kumo-subtle hover:!text-kumo-default hover:!bg-kumo-tint"
      >
        <X className="h-3 w-3" />
      </Button>
    </div>
  );

  /* 会话写授权指示 + 撤销（“允许此对话”授予后展示，撤销走 PATCH sessions/{id}） */
  const writeGrantChip = activeSessionRow?.writeEnabled && !botActive && (
    <span className="askai-write-grant inline-flex shrink-0 items-center gap-1.5 rounded-full bg-kumo-success/10 px-2.5 py-1 text-[11px] text-kumo-success">
      <ShieldCheck className="h-3 w-3" />
      <span className="hidden min-w-0 truncate @[420px]:inline">本会话已授权写操作</span>
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={() => handleRevokeSessionWrite(activeSessionRow.id)}
        aria-label="撤销本会话写权限"
        title="撤销本会话写权限"
        className="!h-5 !rounded-full !px-2 !py-0 !text-[10px] text-kumo-warning hover:!bg-kumo-warning/10"
      >
        撤销
      </Button>
    </span>
  );

  /* ---- 全屏扩展模式 ---- */
  const renderFullscreen = () => (
    <div
      className="askai-expand-in group/sidebar fixed inset-x-0 top-0 z-[1150] flex h-dvh flex-col p-2"
      data-state="expanded"
      style={{
        '--sidebar-active-bg': 'var(--color-kumo-tint)',
        '--sidebar-bg': 'var(--color-kumo-base)',
        '--sidebar-animation-duration': '250ms',
        '--sidebar-easing': 'cubic-bezier(0.77, 0, 0.175, 1)',
      }}
    >
      <div className="relative flex h-full w-full flex-1 overflow-hidden rounded-2xl border-[3px] border-brand/80 bg-kumo-canvas shadow-2xl">
        <div
          className={`flex h-full shrink-0 flex-col overflow-hidden bg-kumo-base transition-[width] duration-300 ease-in-out ${fullscreenSidebar ? 'w-64 border-r border-kumo-line' : 'w-0'}`}
          aria-hidden={!fullscreenSidebar}
        >
          <div className="flex h-full w-64 shrink-0 flex-col">
            <div className="flex h-[58px] shrink-0 items-center justify-between gap-2 border-b border-kumo-line px-3">
          <div className="flex min-w-0 items-center gap-2">
            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-brand/10 text-brand">
              <Sparkle className="h-4 w-4" />
            </span>
            <span className="truncate text-sm font-bold text-kumo-strong">会话</span>
            <Badge variant="secondary">{sessions.length}</Badge>
          </div>
          <Tooltip
            content="新建会话"
            side="bottom"
            render={
              <Button size="sm" shape="square" variant="ghost" aria-label="新建会话" onClick={handleNewSession}>
                <Plus className="h-4 w-4 transition-transform duration-300 hover:rotate-90" />
              </Button>
            }
          />
        </div>
            <div className="flex shrink-0 items-center gap-1 border-b border-kumo-line px-2 py-1.5">
              <Tabs
                size="sm"
                variant="segmented"
                value={sessionListTab}
                onValueChange={setSessionListTab}
                tabs={[
                  { value: 'web', label: `用户 (${webSessions.length})` },
                  { value: 'bot', label: `BOT (${channelSessions.length})` },
                  { value: 'cron', label: `任务 (${taskGroups.length})` },
                ]}
              />
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-2 scrollbar-thin">
              {sessions.length === 0 ? (
                <Empty title="暂无会话" description="点击右上角新建会话开始对话" />
              ) : sessionListTab === 'web' ? (
                webSessions.length === 0 ? (
                  <p className="px-2.5 py-6 text-center text-xs text-kumo-subtle">暂无用户对话</p>
                ) : (
                  <Sidebar.Menu>
                    {webSessions.map((s) => (
                      <SessionItem
                        key={s.id}
                        s={s}
                          active={s.id === activeSessionId}
                          deleteArmed={isAskAiSessionArmed(s.id)}
                          onSelect={() => { setActiveSessionId(s.id); setMessages([]); loadMessages(s.id); if (s.mode === 'ask' || s.mode === 'agent') { setBehavior(s.mode); try { localStorage.setItem('adminai-behavior', s.mode); } catch { } } }}
                          onDelete={requestDelete}
                        />
                      ))}
                    </Sidebar.Menu>
                  )
                ) : sessionListTab === 'bot' ? (
                channelSessions.length === 0 ? (
                  <p className="px-2.5 py-6 text-center text-xs text-kumo-subtle">暂无 BOT 会话</p>
                ) : (
                  <Sidebar.Menu>
                    {channelGroups.map((g) => (
                      <div key={g.key}>
                        <div className="px-2.5 pb-1 pt-2.5 text-[10px] font-semibold uppercase tracking-wide text-kumo-subtle/70">{g.label}</div>
                        {g.items.map((s) => (
                          <SessionItem
                            key={s.id}
                            s={s}
                            active={s.id === activeSessionId}
                            deleteArmed={isAskAiSessionArmed(s.id)}
                            onSelect={() => { setActiveSessionId(s.id); setMessages([]); loadMessages(s.id); if (s.mode === 'ask' || s.mode === 'agent') { setBehavior(s.mode); try { localStorage.setItem('adminai-behavior', s.mode); } catch { } } }}
                            onDelete={requestDelete}
                          />
                        ))}
                      </div>
                    ))}
                  </Sidebar.Menu>
                )
              ) : taskGroups.length === 0 ? (
                <p className="px-2.5 py-6 text-center text-xs text-kumo-subtle">暂无任务会话</p>
              ) : (
                <Sidebar.Menu>
                  <BotTaskList
                    taskGroups={taskGroups}
                    collapsedTaskGroups={collapsedTaskGroups}
                    onToggleTaskGroup={toggleTaskGroup}
                    activeSessionId={activeSessionId}
                    deleteIsArmed={isAskAiSessionArmed}
                    onSelect={(s) => { setActiveSessionId(s.id); setMessages([]); loadMessages(s.id); if (s.mode === 'ask' || s.mode === 'agent') { setBehavior(s.mode); try { localStorage.setItem('adminai-behavior', s.mode); } catch { } } }}
                    onDelete={requestDelete}
                  />
                </Sidebar.Menu>
              )}
            </div>
          </div>
        </div>

        <div className="@container relative flex min-w-0 flex-1 flex-col">
          <div className="flex h-[58px] shrink-0 items-center justify-between border-b border-kumo-line px-4">
            <div className="flex items-center gap-1">
              <Button type="button" size="sm" variant="ghost" onClick={() => setFullscreenSidebar(!fullscreenSidebar)} className="flex h-7 items-center gap-1.5 rounded-md px-2 text-xs" aria-label="Toggle Sidebar">
                <ArrowLeft className={`h-3.5 w-3.5 transition-transform ${fullscreenSidebar ? '' : 'rotate-180'}`} />
                侧栏
              </Button>
            </div>
            <div className="flex items-center gap-1">
              <Button type="button" size="sm" variant="ghost" onClick={() => { setExpanded(false); setManageOpen(true); }} className="flex h-7 items-center gap-1.5 rounded-md px-2 text-xs" aria-label="设置" title="设置">
                <SettingsIcon className="h-3.5 w-3.5" />
              </Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setExpanded(false)} className="flex h-7 items-center gap-1.5 rounded-md px-2 text-xs" aria-label="Collapse to sidebar">
                收回到侧栏
              </Button>
            </div>
          </div>
           <div className="flex min-h-0 flex-1">
            <div className="relative flex min-h-0 min-w-0 flex-1 flex-col px-6 cq-md:px-8 cq-xl:px-10">
              <DotGrid />
              <div className="relative mx-auto flex min-h-0 w-full max-w-4xl flex-1 flex-col">
                <div className="flex min-h-0 flex-1 flex-col overflow-y-auto overscroll-contain">
                  {messages.length === 0 ? (
                    botActive ? (
                      <div className="flex h-full flex-col items-center justify-center gap-2 text-center text-sm text-kumo-subtle">
                        <Lock className="h-6 w-6 text-kumo-warning" />
                        <p>该会话由 BOT/自动化流程管理（只读）</p>
                        <p className="text-xs text-kumo-subtle/70">可查看历史记录</p>
                      </div>
                    ) : (
                      <EmptyState onPrompt={(p) => { setInput(p); textareaRef.current?.focus(); }} />
                    )
                  ) : (
                    <MessageList messages={messages} mode={behavior} live={streaming ? null : liveRun} onResolveApproval={handleResolveApproval} onRetry={handleSend} onEditResend={handleEditResend} />
                  )}
                </div>
                {/* 全屏输入区（实底不透明，与消息区无缝衔接）；机器人会话只读不渲染输入框 */}
                <div className="z-10 shrink-0 pb-2">
                  {botActive ? (
                    <div className="flex items-center justify-center gap-2 rounded-xl bg-kumo-recessed/40 px-4 py-3 text-xs text-kumo-subtle ring-1 ring-kumo-line">
                      <Lock className="h-3.5 w-3.5 text-kumo-warning" />
                      该会话由{activeSession?.source === 'cron' ? '定时任务' : 'BOT'}管理，仅可查看；如需对话请新建会话
                    </div>
                  ) : (
                  <div className="relative rounded-xl bg-kumo-base ring-1 ring-kumo-line transition-all has-[textarea:focus]:ring-[1.5px] has-[textarea:focus]:ring-kumo-brand/50" data-askai-menu>
              {pendingMentions.length > 0 && (
                <div className="flex flex-wrap gap-1 px-4 pt-2.5">
                  {pendingMentions.map((m) => {
                    const MI = MENTION_GROUPS.find((g) => g.type === m.type)?.icon || Globe;
                    return (
                      <span
                        key={`${m.type}-${m.id}`}
                        title={`${m.type}: ${m.id}`}
                        className="flex max-w-[220px] select-none items-center gap-1 rounded-full border border-kumo-line/60 bg-kumo-recessed/60 py-0.5 pl-2 pr-1 text-[11px] text-kumo-default"
                      >
                        <MI className="h-3 w-3 shrink-0 text-brand" />
                        <span className="truncate">{m.name}</span>
                        <Button
                          type="button"
                          size="sm"
                          variant="ghost"
                          onClick={() => removeMention(m)}
                          aria-label={`移除引用 ${m.name}`}
                          className="ml-0.5 rounded-sm p-0.5 text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-danger"
                        >
                          <X className="h-3 w-3" />
                        </Button>
                      </span>
                    );
                  })}
                </div>
              )}
              <Textarea
                ref={textareaRef}
                rows={2}
                value={input}
                onChange={handleInputChange}
                onKeyDown={handleTextareaKeyDown}
                placeholder={placeholder}
                className="!ring-0 focus:!ring-0 h-auto w-full resize-none rounded-xl border-0 bg-transparent p-4 pb-0 text-sm text-kumo-default outline-none placeholder:text-kumo-subtle"
                style={{ maxHeight: 256 }}
              />
              {atMenuOpen && (
                <AtResourceMenu resources={resources} tab={atTab} setTab={setAtTab} q={atQuery} setQ={setAtQuery} error={atError} loading={atLoading} onInsert={insertAtResource} />
              )}
              <div className="flex items-center justify-between gap-1.5 p-4 pt-1.5">
                <div className="flex min-w-0 items-center gap-2">
                  <Tabs size="sm" variant="segmented" className="shrink-0" value={behavior} onValueChange={chooseBehavior} tabs={BEHAVIOR_TABS} />
                  {externalRunIndicator}
                  {writeGrantChip}
                </div>
                <div className="flex shrink-0 items-center gap-1">
                {streaming ? (
                  <Button type="button" size="sm" variant="secondary-destructive" shape="circle" onClick={handleCancel} aria-label="停止生成">
                    <span className="block h-2.5 w-2.5 rounded-[2px] bg-current" />
                  </Button>
) : (
                  <Button
                    type="button"
                        size="sm"
                        variant="primary"
                        shape="circle"
                        onClick={handleSend}
                        disabled={!input.trim()}
                        aria-label="发送"
                  >
                    <Send className="h-3.5 w-3.5" />
                  </Button>
                )}
                </div>
              </div>
            </div>
            )}
            </div>
          </div>
        </div>
        {approvalOverlay('bottom-[150px] left-1/2 w-full max-w-4xl -translate-x-1/2 px-6')}
      </div>
      </div>
    </div>
    </div>
  );
   /* ---- 侧栏模式 ---- */
  const renderSidebar = () => (
    <div
      ref={panelRef}
      className="@container fixed right-0 top-0 z-[1150] flex h-dvh flex-col overflow-hidden border-l border-kumo-line bg-[var(--app-main-surface)] transition-[width,transform] duration-300 ease-in-out max-lg:!w-screen"
      style={{ width: 'var(--askai-panel-w)', transform: animated ? 'translateX(0)' : 'translateX(100%)', pointerEvents: animated ? 'auto' : 'none' }}
    >
      {/* 拖宽手柄 */}
      <div className="absolute inset-y-0 -left-1 z-20 w-2 cursor-col-resize" onMouseDown={startDrag} aria-hidden />

      {/* 写操作审批浮层：侧栏内滑出（输入区上方），不遮挡主画布 */}
      {approvalOverlay('inset-x-3 bottom-[132px]')}

{/* 双视图滑动切换（对话 ⇄ 管理）；overflow-clip 避免滚动偏移 */}
      <div className="relative min-h-0 flex-1 overflow-clip">
        {/* ===== 管理视图（设置/频道/审计收进侧栏） ===== */}
        <div
          className={`absolute inset-0 flex flex-col transition-[transform,opacity,visibility] duration-[350ms] ease-[cubic-bezier(0.22,1,0.36,1)] will-change-[transform,opacity] ${
            manageOpen ? 'visible translate-x-0 opacity-100' : 'pointer-events-none invisible translate-x-8 opacity-0'
          }`}
        >
          <div className="flex h-[58px] shrink-0 items-center justify-between border-b border-kumo-line bg-[var(--app-main-surface)] pl-4 pr-3">
            <Tabs value={adminTab} onValueChange={setAdminTab} tabs={TAB_OPTIONS} />
            <Button
              type="button"
              variant="ghost"
              shape="square"
              onClick={() => setManageOpen(false)}
              aria-label="关闭设置"
              title="关闭设置"
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 pt-4">
            <AdminConsole hideTabs activeTab={adminTab} onTabChange={setAdminTab} />
          </div>
        </div>

        {/* ===== 对话视图 ===== */}
        <div
          className={`absolute inset-0 flex flex-col transition-[transform,opacity,visibility] duration-[350ms] ease-[cubic-bezier(0.22,1,0.36,1)] will-change-[transform,opacity] ${
            manageOpen ? 'pointer-events-none invisible -translate-x-8 opacity-0' : 'visible translate-x-0 opacity-100'
          }`}
        >
          <div className="flex h-[58px] shrink-0 items-center justify-between border-b border-kumo-line bg-[var(--app-main-surface)] px-4">
        <div className="relative flex items-center gap-1" data-askai-menu>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => { setAtMenuOpen(false); setSessionMenuOpen(!sessionMenuOpen); }}
            className={`flex h-8 max-w-[200px] min-w-0 items-center justify-between gap-2 rounded-lg px-2.5 text-left text-sm font-medium ${
              sessionMenuOpen
                ? 'bg-kumo-tint text-kumo-strong'
                : 'bg-kumo-fill text-kumo-default hover:bg-kumo-tint'
            }`}
            aria-haspopup="menu"
            aria-expanded={sessionMenuOpen}
          >
            <span className="truncate"><SessionTitleText title={activeSessionRow?.title} active={!!activeSessionId} /></span>
            <ChevronDown className={`h-3 w-3 shrink-0 text-kumo-subtle transition-transform duration-200 ${sessionMenuOpen ? 'rotate-180' : ''}`} />
          </Button>
          <Button type="button" size="sm" variant="ghost" shape="square" onClick={handleNewSession} aria-label="新对话" title="新对话">
            <Plus className="h-4 w-4" />
          </Button>
          {sessionMenuOpen && (
            <div
              className="absolute left-0 top-[calc(100%+4px)] z-40 w-64 overflow-hidden rounded-xl bg-kumo-base shadow-lg ring-1 ring-kumo-line"
              style={{ '--sidebar-active-bg': 'var(--color-kumo-tint)', '--sidebar-animation-duration': '250ms' }}
            >
              <div className="border-b border-kumo-line p-1.5">
                <Tabs
                  size="sm"
                  variant="segmented"
                  value={sessionListTab}
                  onValueChange={setSessionListTab}
                  tabs={[
                    { value: 'web', label: `用户 (${webSessions.length})` },
                    { value: 'bot', label: `BOT (${channelSessions.length})` },
                    { value: 'cron', label: `任务 (${taskGroups.length})` },
                  ]}
                />
              </div>
              <div className="max-h-72 overflow-y-auto p-1.5">
                {sessions.length === 0 ? (
                  <p className="px-2.5 py-2 text-xs text-kumo-subtle">暂无会话</p>
                ) : sessionListTab === 'web' ? (
                  webSessions.length === 0 ? (
                    <p className="px-2.5 py-2 text-xs text-kumo-subtle">暂无用户对话</p>
                  ) : (
                    <Sidebar.Menu>
                      {webSessions.map((s) => (
                        <SessionItem
                          key={s.id}
                          s={s}
                          active={s.id === activeSessionId}
                            deleteArmed={isAskAiSessionArmed(s.id)}
                          onSelect={() => { setActiveSessionId(s.id); setMessages([]); loadMessages(s.id); if (s.mode === 'ask' || s.mode === 'agent') { setBehavior(s.mode); try { localStorage.setItem('adminai-behavior', s.mode); } catch { } } }}
                          onDelete={requestDelete}
                        />
                      ))}
                    </Sidebar.Menu>
                  )
                ) : sessionListTab === 'bot' ? (
                  channelSessions.length === 0 ? (
                    <p className="px-2.5 py-2 text-xs text-kumo-subtle">暂无 BOT 会话</p>
                  ) : (
                    <Sidebar.Menu>
                      {channelGroups.map((g) => (
                        <div key={g.key}>
                          <div className="px-2.5 pb-1 pt-2.5 text-[10px] font-semibold uppercase tracking-wide text-kumo-subtle/70">{g.label}</div>
                          {g.items.map((s) => (
                            <SessionItem
                              key={s.id}
                              s={s}
                              active={s.id === activeSessionId}
                              deleteArmed={isAskAiSessionArmed(s.id)}
                              onSelect={() => { setActiveSessionId(s.id); setMessages([]); loadMessages(s.id); if (s.mode === 'ask' || s.mode === 'agent') { setBehavior(s.mode); try { localStorage.setItem('adminai-behavior', s.mode); } catch { } } }}
                              onDelete={requestDelete}
                            />
                          ))}
                        </div>
                      ))}
                    </Sidebar.Menu>
                  )
                ) : taskGroups.length === 0 ? (
                  <p className="px-2.5 py-2 text-xs text-kumo-subtle">暂无任务会话</p>
                ) : (
                  <Sidebar.Menu>
                    <BotTaskList
                      taskGroups={taskGroups}
                      collapsedTaskGroups={collapsedTaskGroups}
                      onToggleTaskGroup={toggleTaskGroup}
                      activeSessionId={activeSessionId}
                      deleteIsArmed={isAskAiSessionArmed}
                      onSelect={(s) => { setActiveSessionId(s.id); setMessages([]); loadMessages(s.id); if (s.mode === 'ask' || s.mode === 'agent') { setBehavior(s.mode); try { localStorage.setItem('adminai-behavior', s.mode); } catch { } } }}
                      onDelete={requestDelete}
                    />
                  </Sidebar.Menu>
                )}
              </div>
              <div className="flex items-center gap-1 border-t border-kumo-line p-1.5">
                <Button type="button" size="sm" variant="ghost" onClick={handleNewSession} className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-xs text-kumo-default hover:bg-kumo-tint">
                  <Plus className="h-3.5 w-3.5" /> 新对话
                </Button>
              </div>
            </div>
          )}
        </div>
         <div className="flex items-center gap-0.5">
          <Button type="button" variant="ghost" shape="square" onClick={() => setExpanded(true)} aria-label="展开侧栏" title="展开侧栏">
            <Maximize2 className="h-4 w-4" />
          </Button>
          <Button type="button" variant="ghost" shape="square" onClick={() => setManageOpen(true)} aria-label="设置" title="设置">
            <SettingsIcon className="h-3.5 w-3.5" />
          </Button>
          <Button type="button" variant="ghost" shape="square" onClick={closeSidebar} aria-label="关闭侧栏" title="关闭侧栏">
            <X className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* ===== Body + Footer（点阵背景；消息区与输入框上下无缝衔接） ===== */}
      <div className="relative flex min-h-0 flex-1 flex-col">
        <DotGrid surfaceRef={spotlightSidebarRef} />
        <div className="flex min-h-0 flex-1 flex-col overflow-y-hidden px-4">
          <div className="relative flex min-h-0 flex-1 flex-col">
            <div className="relative min-h-0 flex-1 flex-col">
              {messages.length === 0 ? (
                botActive ? (
                  <div className="flex h-full flex-col items-center justify-center gap-2 text-center text-sm text-kumo-subtle">
                    <Lock className="h-6 w-6 text-kumo-warning" />
                    <p>该会话由 BOT/自动化流程管理（只读）</p>
                    <p className="text-xs text-kumo-subtle/70">可查看历史记录</p>
                  </div>
                ) : (
                  <EmptyState onPrompt={(p) => { setInput(p); setTimeout(() => textareaRef.current?.focus(), 0); }} />
                )
              ) : (
                <MessageList messages={messages} mode={behavior} live={streaming ? null : liveRun} onResolveApproval={handleResolveApproval} onRetry={handleSend} onEditResend={handleEditResend} />
              )}
            </div>
          </div>
        </div>
        {/* ===== Footer（输入框：实底不透明，无上边距，与消息区相连）；机器人会话只读不渲染输入框 ===== */}
        <div className="relative shrink-0 px-4 pb-4">
        {botActive ? (
          <div className="flex items-center justify-center gap-2 rounded-xl bg-kumo-recessed/40 px-4 py-3 text-xs text-kumo-subtle ring-1 ring-kumo-line">
            <Lock className="h-3.5 w-3.5 text-kumo-warning" />
            该会话由{activeSession?.source === 'cron' ? '定时任务' : 'BOT'}管理，仅可查看；如需对话请新建会话
          </div>
        ) : (
        <form onSubmit={(e) => { e.preventDefault(); handleSend(); }}>
          <div className="relative rounded-xl bg-kumo-base ring-1 ring-kumo-line transition-all has-[textarea:focus]:ring-[1.5px] has-[textarea:focus]:ring-kumo-brand/50" data-askai-menu>
            {pendingMentions.length > 0 && (
              <div className="flex flex-wrap gap-1 px-4 pt-2.5">
                {pendingMentions.map((m) => {
                  const MI = MENTION_GROUPS.find((g) => g.type === m.type)?.icon || Globe;
                  return (
                    <span
                      key={`${m.type}-${m.id}`}
                      title={`${m.type}: ${m.id}`}
                      className="flex max-w-[220px] select-none items-center gap-1 rounded-full border border-kumo-line/60 bg-kumo-recessed/60 py-0.5 pl-2 pr-1 text-[11px] text-kumo-default"
                    >
                      <MI className="h-3 w-3 shrink-0 text-brand" />
                      <span className="truncate">{m.name}</span>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => removeMention(m)}
                        aria-label={`移除引用 ${m.name}`}
                        className="ml-0.5 rounded-sm p-0.5 text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-danger"
                      >
                        <X className="h-3 w-3" />
                      </Button>
                    </span>
                  );
                })}
              </div>
            )}
            <Textarea
              ref={textareaRef}
              rows={2}
              value={input}
              onChange={handleInputChange}
              onKeyDown={handleTextareaKeyDown}
              placeholder={placeholder}
              className="!ring-0 focus:!ring-0 h-auto w-full resize-none rounded-xl border-0 bg-transparent p-4 pb-0 text-sm text-kumo-default outline-none placeholder:text-kumo-subtle"
              style={{ maxHeight: 256 }}
            />
            {atMenuOpen && (
              <AtResourceMenu resources={resources} tab={atTab} setTab={setAtTab} q={atQuery} setQ={setAtQuery} error={atError} loading={atLoading} onInsert={insertAtResource} />
            )}
            <div className="flex items-center justify-between gap-1.5 p-4 pt-1.5">
              <div className="flex min-w-0 items-center gap-2">
                <Tabs size="sm" variant="segmented" className="shrink-0" value={behavior} onValueChange={chooseBehavior} tabs={BEHAVIOR_TABS} />
                {externalRunIndicator}
                {writeGrantChip}
              </div>
              <div className="flex shrink-0 items-center gap-1">
                {streaming ? (
                  <Button type="button" size="sm" variant="secondary-destructive" shape="circle" onClick={handleCancel} aria-label="停止生成">
                    <span className="block h-2.5 w-2.5 rounded-[2px] bg-current" />
                  </Button>
                ) : (
                  <Button
                    type="submit"
                    size="sm"
                    variant="primary"
                    shape="circle"
                    disabled={!input.trim()}
                    aria-label="发送"
                  >
                    <Send className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
            </div>
          </div>
        </form>
        )}
        </div>
      </div>
        </div>
      </div>
    </div>
  );

  /* 返回：已展开 renderFullscreen，未展开 renderSidebar（含管理视图）。
     全屏遮罩用 Portal 挂到 <body>，跳出外层 Sidebar.Provider 的 isolate 层叠上下文，
     避免被左侧导航等元素在层级上盖住。 */
  return (
    <>
      {expanded ? createPortal(renderFullscreen(), document.body) : renderSidebar()}
    </>
  );
}
