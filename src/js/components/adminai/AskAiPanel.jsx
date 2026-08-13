import React, { useCallback, useEffect, useRef, useState } from 'react';
import useStore from '../../store.js';
import { Sparkle, X, Send, Plus, ChevronDown, Settings as SettingsIcon, Sliders, Star, ArrowLeft, Trash, Maximize2 } from '../Icons.jsx';
import MessageList from './MessageList.jsx';
import { parseAdminAiEvent } from '../../modules/adminAiEvents.js';

const PANEL_MIN_WIDTH = 320;
const PANEL_MAX_WIDTH = 800;
const PANEL_DEFAULT_WIDTH = 450;

const SUGGESTED_PROMPTS = [
  { title: '环境变量', subtitle: '调整 Worker 的环境变量', icon: 'Env' },
  { title: '创建 API Token', subtitle: '创建 API 访问令牌', icon: 'Key' },
  { title: '域名设置', subtitle: '显示我的域名配置', icon: 'Globe' },
  { title: 'DNS 记录', subtitle: '添加一条 DNS 记录', icon: 'Routes' },
  { title: '服务器状态', subtitle: '检查服务器运行状态', icon: 'Server' },
];

const PRIVACY_TEXT = '聊天记录用于改进服务，处理遵循隐私政策。';

/* ---------- 空状态：云 + 问候 + 建议提示（Cloudflare EmptyState 风格） ---------- */
function EmptyState({ onPrompt }) {
  const hour = new Date().getHours();
  const greeting = hour < 6 ? '夜深了。' : hour < 12 ? '早上好。' : hour < 18 ? '下午好。' : '晚上好。';
  return (
    <div className="flex h-full flex-1 flex-col items-center overflow-y-auto overscroll-contain">
      <div className="my-auto flex w-full flex-col items-center gap-8 pt-4">
        {/* 云装饰（简化版 Cloudflare 浮云动画） */}
        <div className="relative flex items-center justify-center" aria-hidden>
          <div className="cloud-orb cloud-orb--back" />
          <div className="cloud-orb cloud-orb--front" />
          <Sparkle className="absolute h-8 w-8 text-orange-400" />
        </div>
        <div className="text-center">
          <h3 className="mb-1.5 text-lg font-medium text-kumo-default">{greeting}</h3>
          <p className="text-sm text-kumo-subtle">今天想做什么？</p>
        </div>

        {/* 建议提示 */}
        <div className="flex w-full max-w-[300px] flex-col gap-1.5">
          {SUGGESTED_PROMPTS.map((p) => (
            <button
              key={p.title}
              type="button"
              onClick={() => onPrompt(p.subtitle || p.title)}
              className="group relative flex w-full cursor-pointer items-center gap-3 rounded-xl border border-kumo-line/50 bg-kumo-elevated p-2 text-left transition-all duration-200 hover:border-orange-200 hover:bg-kumo-base hover:shadow-[0_0_10px_rgba(251,146,60,0.15)] dark:hover:border-orange-950"
            >
              <span
                className="absolute left-0 top-1/2 h-0 w-[2px] -translate-y-1/2 rounded-full bg-gradient-to-b from-orange-300 to-orange-500 transition-all duration-200 group-hover:h-5"
              />
              <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-neutral-100/80 transition-colors duration-200 group-hover:bg-orange-50 dark:bg-neutral-800/60 dark:group-hover:bg-orange-900/30">
                <Star className="h-3.5 w-3.5 text-neutral-400 transition-colors duration-200 group-hover:text-orange-500" />
              </span>
              <span className="flex min-w-0 flex-col">
                <span className="truncate text-xs font-medium text-kumo-subtle transition-colors group-hover:text-kumo-default">{p.title}</span>
                <span className="truncate text-xs text-kumo-subtle">{p.subtitle}</span>
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

/* ---------- 点阵背景（InteractiveDotGrid 简化静态版） ---------- */
function DotGrid() {
  return (
    <div className="pointer-events-none absolute inset-0" aria-hidden>
      <div
        className="absolute inset-0"
        style={{
          backgroundImage: 'radial-gradient(circle, rgba(125,125,125,0.12) 1px, transparent 1px)',
          backgroundSize: '12px 12px',
        }}
      />
    </div>
  );
}

/* ---------- SupportBart：Need more help? + 支持链接 ---------- */
function SupportBar() {
  return (
    <div className="flex shrink-0 items-center justify-between rounded-xl bg-kumo-overlay p-2 pl-3 shadow-xs ring-1 ring-kumo-line dark:bg-kumo-base">
      <p className="text-sm font-medium text-kumo-default">需要更多帮助？</p>
      <a
        href="https://github.com/iwvw/API-Monitor/issues"
        target="_blank"
        rel="noopener noreferrer"
        className="mx-0 flex h-6.5 shrink-0 items-center gap-1 rounded-md bg-kumo-base px-2 text-xs font-medium text-kumo-default ring-1 ring-kumo-line transition-colors not-hover:bg-kumo-base hover:bg-kumo-tint"
      >
        支持
      </a>
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
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionMenuOpen, setSessionMenuOpen] = useState(false);
  const [privacyDismissed, setPrivacyDismissed] = useState(() => {
    try { return localStorage.getItem('adminai-privacy-dismissed') === '1'; } catch { return false; }
  });
  const [panelWidth, setPanelWidth] = useState(PANEL_DEFAULT_WIDTH);
  const [expanded, setExpanded] = useState(false);
  const [fullscreenSidebar, setFullscreenSidebar] = useState(true);
  const [fullscreenArtifacts, setFullscreenArtifacts] = useState(false);

  // 侧栏打开时主内容让出宽度（MainLayout 主画布读 --askai-sidebar-w）
  useEffect(() => {
    document.documentElement.style.setProperty(
      '--askai-sidebar-w',
      showAskAI && !expanded ? `${panelWidth}px` : '0px'
    );
  }, [showAskAI, expanded, panelWidth]);

  /* 侧栏滑入/滑出动画：组件保持挂载，由 animate 状态驱动 transform。
     首帧先以关闭态渲染、下一帧再切到目标态，保证 transition 可播放。 */
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
  const textareaRef = useRef(null);
  const dragState = useRef(null);
  const panelRef = useRef(null);

  const loadSessions = useCallback(async () => {
    setSessionsLoading(true);
    try {
      const res = await fetch('/api/admin-ai/sessions');
      const data = await res.json();
      const body = data.data || data; // response.OK 统一 { success, data } 包装
      const list = body.sessions || [];
      setSessions(list);
      if (list.length > 0 && !activeSessionId) setActiveSessionId(list[0].id);
    } catch {
    } finally {
      setSessionsLoading(false);
    }
  }, [activeSessionId]);

  const loadMessages = useCallback(async (sessionId) => {
    if (!sessionId) return;
    try {
      const res = await fetch(`/api/admin-ai/sessions/${sessionId}/messages`);
      const data = await res.json();
      const body = data.data || data;
      const items = body.items || body.messages || [];
      setMessages(items.map((m) => ({
        id: m.id,
        role: m.role,
        content: m.content || '',
        reasoning: '',
        thinking: [],
        blocks: m.toolCallMeta ? [{ type: 'tool_call', ...JSON.parse(m.toolCallMeta) }] : [],
      })));
    } catch {
    }
  }, []);

  useEffect(() => { loadSessions(); }, [loadSessions]);

  useEffect(() => {
    if (activeSessionId) loadMessages(activeSessionId);
  }, [activeSessionId, loadMessages]);

  const stopStream = useCallback(() => {
    if (eventSource.current) {
      eventSource.current.close();
      eventSource.current = null;
    }
    setStreaming(false);
    setRunId(null);
  }, []);

  useEffect(() => () => stopStream(), [stopStream]);

  /* Esc 关闭侧栏（全屏模式亦收回到侧栏形态） */
  useEffect(() => {
    if (!showAskAI) return undefined;
    const onKey = (e) => {
      if (e.key === 'Escape') {
        if (expanded) setExpanded(false);
        else setShowAskAI(false);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [showAskAI, expanded, setShowAskAI]);

  const startStream = (newRunId) => {
    stopStream();
    setRunId(newRunId);
    setStreaming(true);

    const es = new EventSource(`/api/admin-ai/messages/stream?runId=${newRunId}`);
    eventSource.current = es;

    es.addEventListener('reasoning', (e) => {
      try {
        const event = parseAdminAiEvent('reasoning', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          const last = updated[updated.length - 1];
          if (last && last.role === 'assistant') {
            last.reasoning = event.text || '';
          } else {
            updated.push({ id: `msg_${Date.now()}`, role: 'assistant', reasoning: event.text || '', thinking: [], blocks: [] });
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('delta', (e) => {
      try {
        const event = parseAdminAiEvent('delta', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          const last = updated[updated.length - 1];
          if (last && last.role === 'assistant') {
            const lastBlock = last.blocks ? last.blocks[last.blocks.length - 1] : null;
            if (lastBlock && lastBlock.type === 'text') {
              lastBlock.text = (lastBlock.text || '') + (event.delta || '');
            } else {
              if (!last.blocks) last.blocks = [];
              last.blocks.push({ type: 'text', text: event.delta || '' });
            }
          } else {
            updated.push({
              id: `msg_${Date.now()}`,
              role: 'assistant',
              reasoning: '',
              thinking: [],
              blocks: [{ type: 'text', text: event.delta || '' }],
            });
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('tool_start', (e) => {
      try {
        const event = parseAdminAiEvent('tool_start', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          const last = updated[updated.length - 1];
          if (last && last.role === 'assistant') {
            if (!last.thinking) last.thinking = [];
            last.thinking.push({ type: 'tool_call', toolName: event.toolName, args: event.args, status: 'running' });
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('tool_result', (e) => {
      try {
        const event = parseAdminAiEvent('tool_result', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          for (const msg of updated) {
            if (msg.role === 'assistant' && msg.thinking) {
              for (const step of msg.thinking) {
                if (step.toolName === event.toolName) {
                  step.status = event.status === 'success' ? 'success' : 'failed';
                  step.error = event.error || '';
                  return updated;
                }
              }
            }
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('approval_required', (e) => {
      try {
        const event = parseAdminAiEvent('approval_required', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          const last = updated[updated.length - 1];
          if (last && last.role === 'assistant') {
            if (!last.blocks) last.blocks = [];
            last.blocks.push({ type: 'approval', ...event });
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('error', (e) => {
      try {
        const event = parseAdminAiEvent('error', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          const last = updated[updated.length - 1];
          if (last && last.role === 'assistant') {
            if (!last.blocks) last.blocks = [];
            last.blocks.push({ type: 'error', message: event.message || '发生错误', retryable: true });
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('done', () => stopStream());
  };

  const handleSend = async () => {
    const trimmed = input.trim();
    if (!trimmed || !activeSessionId || streaming) return;

    setInput('');
    let sessionId = activeSessionId;

    if (!sessions.find((s) => s.id === sessionId)) {
      try {
        const res = await fetch('/api/admin-ai/sessions', { method: 'POST' });
        const data = await res.json();
        const body = data.data || data;
        sessionId = body.id || body.session?.id;
        setActiveSessionId(sessionId);
        setSessions((prev) => [...prev, { id: sessionId, title: new Date().toLocaleString('zh-CN') }]);
      } catch {
        return;
      }
    }

    setMessages((prev) => [...prev, {
      id: `user_${Date.now()}`,
      role: 'user',
      content: trimmed,
    }, {
      id: `assistant_${Date.now()}`,
      role: 'assistant',
      reasoning: '',
      thinking: [],
      blocks: [],
    }]);

    try {
      const res = await fetch('/api/admin-ai/messages', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId, prompt: trimmed }),
      });
      const data = await res.json();
      if (data.runId) startStream(data.runId);
    } catch {
      setMessages((prev) => [...prev, {
        id: `err_${Date.now()}`,
        role: 'assistant',
        reasoning: '',
        thinking: [],
        blocks: [{ type: 'error', message: '发送失败，请重试', retryable: true }],
      }]);
    }
  };

  const handleCancel = async () => {
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
    stopStream();
  };

  const handleNewSession = async () => {
    try {
      const res = await fetch('/api/admin-ai/sessions', { method: 'POST' });
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

  const handleResolveApproval = async (approvalId, action) => {
    try {
      await fetch(`/api/admin-ai/approvals/${approvalId}/resolve`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action }),
      });
      setMessages((prev) => {
        const updated = [...prev];
        for (const msg of updated) {
          if (msg.blocks) {
            for (const block of msg.blocks) {
              if (block.type === 'approval' && block.approvalId === approvalId) {
                block.status = action === 'approve' ? 'approved' : 'rejected';
                return updated;
              }
            }
          }
        }
        return updated;
      });
    } catch {
    }
  };

  /* textarea 自动增高（最大 256px，对齐 Cloudflare） */
  const resizeTextarea = () => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 256)}px`;
  };

  /* 拖宽手柄 */
  const startDrag = (e) => {
    dragState.current = { startX: e.clientX, startWidth: panelWidth };
    const onMove = (ev) => {
      const next = dragState.current.startWidth + (dragState.current.startX - ev.clientX);
      setPanelWidth(Math.max(PANEL_MIN_WIDTH, Math.min(PANEL_MAX_WIDTH, next)));
    };
    const onUp = () => {
      dragState.current = null;
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  };

  const sessionTitle = sessions.find((s) => s.id === activeSessionId)?.title
    || (activeSessionId ? new Date().toLocaleString('zh-CN') : '新对话');

  /* ==================== 渲染 ==================== */
  const closeSidebar = () => { setShowAskAI(false); setExpanded(false); };

  /* ---- 全屏扩展模式（Expand sidebar）---- */
  if (expanded) {
    return (
      <div className="askai-expand-in fixed inset-0 z-50 flex flex-col bg-neutral-800/90 p-2">
        <div className="flex h-full w-full flex-1 overflow-hidden rounded-2xl bg-kumo-canvas shadow-2xl">
          {fullscreenSidebar && (
            <div className="flex h-full w-64 shrink-0 flex-col border-r border-kumo-line bg-kumo-overlay">
              <div className="flex h-[58px] shrink-0 items-center justify-between border-b border-kumo-line px-4">
                <span className="truncate text-sm font-medium text-kumo-default">{sessionTitle}</span>
                <button type="button" onClick={handleNewSession} className="flex h-7 w-7 items-center justify-center rounded-md text-kumo-default transition-colors hover:bg-kumo-tint" aria-label="新建会话">
                  <Plus className="h-4 w-4" />
                </button>
              </div>
              <div className="flex-1 overflow-y-auto p-2">
                {sessions.map((s) => (
                  <button
                    key={s.id}
                    type="button"
                    onClick={() => { setActiveSessionId(s.id); setMessages([]); loadMessages(s.id); }}
                    className={`flex w-full items-center justify-between rounded-lg px-2.5 py-2 text-left text-xs transition-colors ${s.id === activeSessionId ? 'bg-kumo-fill font-medium text-kumo-default' : 'text-kumo-subtle hover:bg-kumo-tint'}`}
                  >
                    <span className="truncate">{s.title || new Date(s.created_at || s.id).toLocaleString('zh-CN')}</span>
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* 主对话区 */}
          <div className="relative flex min-w-0 flex-1 flex-col">
            <div className="flex h-[58px] shrink-0 items-center justify-between border-b border-kumo-line px-4">
              <div className="flex items-center gap-1">
                <button type="button" onClick={() => setFullscreenSidebar(!fullscreenSidebar)} className="flex h-7 items-center gap-1.5 rounded-md px-2 text-xs text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default" aria-label="Toggle Sidebar">
                  <ArrowLeft className={`h-3.5 w-3.5 transition-transform ${fullscreenSidebar ? '' : 'rotate-180'}`} />
                  侧栏
                </button>
                <button type="button" onClick={() => setFullscreenArtifacts(!fullscreenArtifacts)} className="flex h-7 items-center gap-1.5 rounded-md px-2 text-xs text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default" aria-label="Toggle Artifacts">
                  <Sliders className="h-3.5 w-3.5" />
                  工件
                </button>
              </div>
              <button type="button" onClick={() => setExpanded(false)} className="flex h-7 items-center gap-1.5 rounded-md px-2 text-xs text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default" aria-label="Collapse to sidebar">
                收回到侧栏
              </button>
            </div>

            <div className="flex min-h-0 flex-1">
              <div className="relative flex min-h-0 min-w-0 flex-1 flex-col p-6 md:p-8 xl:px-10 xl:py-9">
                <DotGrid />
                <div className="relative mx-auto flex min-h-0 w-full max-w-4xl flex-1 flex-col gap-4">
                  <div className="flex min-h-0 flex-1 flex-col overflow-y-auto overscroll-contain">
                    {messages.length === 0 ? (
                      <EmptyState onPrompt={(p) => { setInput(p); textareaRef.current?.focus(); }} />
                    ) : (
                      <MessageList messages={messages} onResolveApproval={handleResolveApproval} onRetry={handleSend} />
                    )}
                  </div>
                </div>
              </div>
              {fullscreenArtifacts && (
                <div className="w-72 shrink-0 border-l border-kumo-line bg-kumo-overlay p-4">
                  <p className="text-xs font-medium text-kumo-subtle">工件</p>
                  <p className="mt-2 text-xs text-kumo-subtle/70">暂无工件</p>
                </div>
              )}
            </div>

            {/* 全屏输入区 */}
            <div className="z-10 mx-auto w-full max-w-4xl px-6 pb-6">
              <div className="relative rounded-xl bg-kumo-control ring-1 ring-kumo-line transition-all has-[textarea:focus]:ring-[1.5px] has-[textarea:focus]:ring-kumo-brand/50">
                <textarea
                  ref={textareaRef}
                  rows={2}
                  value={input}
                  onChange={(e) => { setInput(e.target.value); resizeTextarea(); }}
                  onInput={resizeTextarea}
                  placeholder="输入消息，@ 引用资源"
                  className="h-auto w-full resize-none rounded-xl border-0 bg-transparent p-4 pb-0 text-sm text-kumo-default outline-none placeholder:text-kumo-subtle"
                  style={{ maxHeight: 256 }}
                />
                <div className="flex items-center justify-between gap-1 p-4 pt-1.5">
                  <button type="button" className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default" aria-label="Edit behavior">
                    询问
                  </button>
                  <div className="flex items-center gap-1">
                    <button type="button" className="flex h-6.5 w-6.5 items-center justify-center rounded-md text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default" aria-label="设置">
                      <SettingsIcon className="h-3.5 w-3.5" />
                    </button>
                    {streaming ? (
                      <button type="button" onClick={handleCancel} className="flex h-6.5 w-6.5 items-center justify-center rounded-md bg-kumo-fill text-kumo-default transition-colors hover:bg-kumo-tint" aria-label="停止生成">
                        <span className="block h-2.5 w-2.5 rounded-[2px] bg-current" />
                      </button>
                    ) : (
                      <button
                        type="button"
                        onClick={handleSend}
                        disabled={!input.trim() || !activeSessionId}
                        className="flex h-6.5 w-6.5 items-center justify-center rounded-md bg-kumo-fill text-kumo-default transition-colors hover:bg-kumo-tint disabled:cursor-not-allowed disabled:text-kumo-subtle disabled:opacity-60"
                        aria-label="发送"
                      >
                        <Send className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  /* ---- 侧栏模式 ---- */
  return (
    <div
      ref={panelRef}
      className="fixed right-0 top-0 z-[1150] flex h-screen flex-col overflow-hidden border-l border-kumo-line bg-kumo-overlay transition-[width,transform] duration-300 ease-in-out dark:bg-kumo-base max-md:!w-screen"
      style={{ width: `${panelWidth}px`, transform: animated ? 'translateX(0)' : 'translateX(100%)', pointerEvents: animated ? 'auto' : 'none' }}
    >
      {/* 拖宽手柄 */}
      <div className="absolute inset-y-0 -left-1 z-20 w-2 cursor-col-resize" onMouseDown={startDrag} aria-hidden />

      {/* ===== Header（58px） ===== */}
      <div className="flex h-[58px] shrink-0 items-center justify-between border-b border-kumo-line bg-kumo-overlay px-4 dark:bg-kumo-base">
        {/* 会话切换 */}
        <div className="relative flex items-center">
          <button
            type="button"
            onClick={() => setSessionMenuOpen(!sessionMenuOpen)}
            className="flex h-9 max-w-[200px] min-w-0 items-center justify-between gap-2 rounded-lg px-1 text-left text-sm text-kumo-default transition-colors hover:bg-kumo-tint"
            aria-haspopup="menu"
            aria-expanded={sessionMenuOpen}
          >
            <span className="truncate">{sessionTitle}</span>
            <ChevronDown className="h-3 w-3 shrink-0 text-kumo-subtle" />
          </button>
          {sessionMenuOpen && (
            <div className="absolute left-0 top-[calc(100%+4px)] z-40 w-64 overflow-hidden rounded-xl bg-kumo-base shadow-lg ring-1 ring-kumo-line dark:bg-kumo-base">
              <div className="max-h-72 overflow-y-auto p-1.5">
                {sessions.length === 0 && (
                  <p className="px-2.5 py-2 text-xs text-kumo-subtle">暂无会话</p>
                )}
                {sessions.map((s) => (
                  <div key={s.id} className="group flex items-center">
                    <button
                      type="button"
                      onClick={() => { setActiveSessionId(s.id); setMessages([]); loadMessages(s.id); setSessionMenuOpen(false); }}
                      className={`flex-1 truncate rounded-lg px-2.5 py-2 text-left text-xs transition-colors ${s.id === activeSessionId ? 'bg-kumo-fill font-medium text-kumo-default' : 'text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default'}`}
                    >
                      {s.title || new Date(s.created_at || s.id).toLocaleString('zh-CN')}
                    </button>
                    <button
                      type="button"
                      onClick={() => handleDeleteSession(s.id)}
                      className="mr-1 hidden h-6 w-6 shrink-0 items-center justify-center rounded-md text-kumo-subtle transition-colors hover:bg-kumo-danger/10 hover:text-kumo-danger group-hover:flex"
                      aria-label="删除会话"
                    >
                      <Trash className="h-3 w-3" />
                    </button>
                  </div>
                ))}
              </div>
              <div className="border-t border-kumo-line p-1.5">
                <button type="button" onClick={handleNewSession} className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-xs text-kumo-default transition-colors hover:bg-kumo-tint">
                  <Plus className="h-3.5 w-3.5" /> 新对话
                </button>
              </div>
            </div>
          )}
        </div>

        {/* 右侧操作 */}
        <div className="flex items-center gap-0.5">
          <button type="button" onClick={handleNewSession} className="flex h-7 w-7 items-center justify-center rounded-md text-kumo-default transition-colors hover:bg-kumo-tint" aria-label="新对话">
            <Plus className="h-3.5 w-3.5" />
          </button>
          <button type="button" onClick={() => setExpanded(true)} className="flex h-7 w-7 items-center justify-center rounded-md text-kumo-default transition-colors hover:bg-kumo-tint" aria-label="展开侧栏">
            <Maximize2 className="h-3.5 w-3.5" />
          </button>
          <button type="button" onClick={closeSidebar} className="flex h-7 w-7 items-center justify-center rounded-md text-kumo-default transition-colors hover:bg-kumo-tint" aria-label="关闭侧栏">
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {/* ===== Body ===== */}
      <div className="relative flex min-h-0 flex-1 flex-col gap-2 overflow-y-hidden p-4">
        <DotGrid />
        <div className="relative flex min-h-0 flex-1 flex-col gap-2">
          <SupportBar />
          <div className="relative min-h-0 flex-1 flex-col">
            {messages.length === 0 ? (
              <EmptyState onPrompt={(p) => { setInput(p); setTimeout(() => textareaRef.current?.focus(), 0); }} />
            ) : (
              <MessageList messages={messages} onResolveApproval={handleResolveApproval} onRetry={handleSend} />
            )}
          </div>
        </div>
      </div>

      {/* ===== Footer ===== */}
      <div className="mt-auto flex shrink-0 flex-col gap-2 p-4">
        {!privacyDismissed && (
          <div className="flex items-center gap-2 rounded-xl bg-kumo-overlay p-3 shadow-xs ring-1 ring-kumo-line dark:bg-kumo-base">
            <p className="flex-1 text-xs leading-snug text-kumo-subtle">
              {PRIVACY_TEXT}
              <a href="https://github.com/iwvw/API-Monitor" target="_blank" rel="noopener noreferrer" className="ml-1 underline transition-colors hover:text-kumo-default">隐私政策</a>
            </p>
            <button
              type="button"
              onClick={() => { setPrivacyDismissed(true); try { localStorage.setItem('adminai-privacy-dismissed', '1'); } catch { } }}
              className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default"
              aria-label="关闭隐私提示"
            >
              <X className="h-3 w-3" />
            </button>
          </div>
        )}

        <form
          onSubmit={(e) => { e.preventDefault(); handleSend(); }}
        >
          <div className="relative rounded-xl bg-kumo-control ring-1 ring-kumo-line transition-all has-[textarea:focus]:ring-[1.5px] has-[textarea:focus]:ring-kumo-brand/50">
            <textarea
              ref={textareaRef}
              rows={2}
              value={input}
              onChange={(e) => { setInput(e.target.value); resizeTextarea(); }}
              onInput={resizeTextarea}
              onKeyDown={(e) => {
                if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
                  e.preventDefault();
                  handleSend();
                }
              }}
              placeholder="输入消息，@ 引用资源"
              className="h-auto w-full resize-none rounded-xl border-0 bg-transparent p-4 pb-0 text-sm text-kumo-default outline-none placeholder:text-kumo-subtle"
              style={{ maxHeight: 256 }}
            />
            <div className="flex items-center justify-between gap-1 p-4 pt-1.5">
              <button type="button" className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default" aria-label="行为模式">
                <Sparkle className="h-3 w-3" />
                询问
              </button>
              <div className="flex items-center gap-1">
                <button type="button" className="flex h-6.5 w-6.5 items-center justify-center rounded-md text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default" aria-label="设置">
                  <SettingsIcon className="h-3.5 w-3.5" />
                </button>
                {streaming ? (
                  <button
                    type="button"
                    onClick={handleCancel}
                    className="flex h-6.5 w-6.5 items-center justify-center rounded-md bg-kumo-fill text-kumo-default transition-colors hover:bg-kumo-tint"
                    aria-label="停止生成"
                  >
                    <span className="block h-2.5 w-2.5 rounded-[2px] bg-current" />
                  </button>
                ) : (
                  <button
                    type="submit"
                    disabled={!input.trim() || !activeSessionId}
                    className="flex h-6.5 w-6.5 items-center justify-center rounded-md bg-kumo-fill text-kumo-default transition-colors hover:bg-kumo-tint disabled:cursor-not-allowed disabled:text-kumo-subtle disabled:opacity-60"
                    aria-label="发送"
                  >
                    <Send className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            </div>
          </div>
        </form>
      </div>
    </div>
  );
}