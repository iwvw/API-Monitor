import React, { useState } from 'react';
import { Button, Loader } from '@cloudflare/kumo';
import { Copy, Check, ChevronDown, X, Terminal } from '../Icons.jsx';
import { STEP } from '../../modules/adminAiMessages.js';

/* 工具名中文标识：折叠组标题与无描述回退共用 */
export function toolLabel(toolName) {
  switch (toolName) {
    case 'call_api':
      return '调用接口';
    case 'get_route':
      return '读取接口契约';
    case 'get_system_status':
      return '获取系统状态';
    case 'get_openapi':
      return '读取 OpenAPI 文档';
    case 'list_apis':
      return '接口目录';
    case 'memory_search':
      return '搜索长期记忆';
    case 'memory_add':
      return '写入长期记忆';
    case 'memory_delete':
      return '删除长期记忆';
    default:
      return toolName || '';
  }
}

/* 实际 API 路径标识：「路径」视图使用（call_api → 方法+路径；get_route → 契约+路径） */
export function toolPathLabel(toolName, args) {
  let a = null;
  if (args) {
    try {
      a = typeof args === 'string' ? JSON.parse(args) : args;
    } catch {
      a = null;
    }
  }
  const path = (a && a.path) || '';
  if (toolName === 'get_route') {
    return path ? `契约 ${path}` : toolLabel(toolName);
  }
  if (path) {
    return `${((a && a.method) || 'GET').toUpperCase()} ${path}`;
  }
  return toolLabel(toolName) || toolName;
}

/* 调用关键参数指纹：从 args 提取标识性字段（hostId/id/name/action 等），
 * 参数不同的调用不合并；行内展示时同样用它区分「当前确定使用的」调用内容。 */
const ARG_KEY_FIELDS = ['hostId', 'host_id', 'id', 'name', 'action', 'zoneId', 'zone_id', 'accountId', 'account_id', 'appName', 'app_name', 'slug', 'taskId', 'task_id', 'runId', 'run_id', 'endpointId', 'endpoint_id'];
export function callArgsKey(args) {
  let a = null;
  if (args) {
    try {
      a = typeof args === 'string' ? JSON.parse(args) : args;
    } catch {
      a = null;
    }
  }
  if (!a || typeof a !== 'object') return '';
  const grab = (o, keys) => {
    for (const k of keys) {
      const v = o[k];
      if (v !== undefined && v !== null && v !== '') {
        return typeof v === 'object' ? JSON.stringify(v) : String(v);
      }
    }
    return null;
  };
  const direct = grab(a, ARG_KEY_FIELDS);
  if (direct !== null) return direct;
  const body = a.body;
  if (body && typeof body === 'object') {
    const inBody = grab(body, ARG_KEY_FIELDS);
    if (inBody !== null) return inBody;
  }
  const query = a.query;
  if (query && typeof query === 'object') {
    const inQuery = grab(query, ARG_KEY_FIELDS);
    if (inQuery !== null) return inQuery;
  }
  return '';
}

/* 工具调用卡片 — Cloudflare Agent「→ Running …」步骤行风格：
 * inline 模式用于 thinking 折叠区；完整模式带参数展开与错误复制。
 * 状态环：running=品牌蓝 spinner+脉冲边条 / success=绿对勾 / failed=红叉 */
export default function ToolCallCard({ toolCall, inline }) {
  const [copied, setCopied] = useState(false);
  const [openArgs, setOpenArgs] = useState(false);

  const { toolName, args, status, error, desc } = toolCall || {};
  const argSummary = args
    ? (typeof args === 'string' ? args : JSON.stringify(args)).slice(0, 200)
    : '';

  /* 工具动作描述：优先接口清单的中文描述（desc），回退到参数推导（call_api → 方法+路径） */
  const describeAction = () => {
    if (desc) return desc;
    if (!args) return '';
    try {
      const a = typeof args === 'string' ? JSON.parse(args) : args;
      switch (toolName) {
        case 'call_api':
          return `${(a.method || 'GET').toUpperCase()} ${a.path || ''}`.trim();
        case 'get_route':
          return a.path ? `契约 ${a.path}` : '';
        case 'get_system_status':
          return '本机运行状态';
        case 'get_openapi':
          return 'OpenAPI 文档';
        case 'list_apis':
          return '接口目录';
        default:
          return toolName ? toolLabel(toolName) : '';
      }
    } catch {
      return '';
    }
  };
  const action = describeAction();

  const handleCopy = async (text) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
    }
  };

  /* 状态徽章：只显示图标（running=spinner / success=绿勾 / failed=红叉） */
  const statusBadge = () => {
    switch (status) {
      case 'running':
        return (
          <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-kumo-brand/10 text-kumo-brand">
            <Loader size={10} className="animate-spin" />
          </span>
        );
      case 'success':
        return (
          <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-kumo-success/10 text-kumo-success">
            <Check className="h-2.5 w-2.5" />
          </span>
        );
      case 'failed':
        return (
          <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-kumo-danger/10 text-kumo-danger">
            <X className="h-2.5 w-2.5" />
          </span>
        );
      default:
        return null;
    }
  };

  /* 内联行：语义视图=中文动作描述（无 desc 时回退工具中文名）+ 灰色小字路径；
     语义与路径分开显示，不再二选一切换 */
  if (inline) {
    const label = desc || toolLabel(toolName) || '未知工具';
    const path = toolPathLabel(toolName, args);
    return (
      <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-kumo-default">
        <span className="min-w-0 line-clamp-1 break-all">{label}</span>
        {path !== label && (
          <span className="min-w-0 line-clamp-1 break-all font-mono text-[10px] text-kumo-subtle/70">{path}</span>
        )}
        {statusBadge()}
      </span>
    );
  }

  return (
    <div className={`relative rounded-xl bg-kumo-control p-2.5 text-xs ${status === 'running' ? 'askai-tool-live' : ''}`}>
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={() => argSummary && setOpenArgs(!openArgs)}
        className="flex w-full items-center gap-2 text-left"
      >
        <span className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-lg ${status === 'running' ? 'bg-kumo-brand/10 text-kumo-brand' : status === 'success' ? 'bg-kumo-success/10 text-kumo-success' : status === 'failed' ? 'bg-kumo-danger/10 text-kumo-danger' : 'bg-kumo-tint text-kumo-subtle'}`}>
          <span className="text-[11px] font-semibold">{status === 'running' ? <Loader size={12} className="animate-spin" /> : '→'}</span>
        </span>
        <span className="min-w-0 line-clamp-1 break-all font-medium text-kumo-default">{action || '未知工具'}</span>
        <span className="ml-auto flex shrink-0 items-center gap-1.5">
          {statusBadge()}
          {argSummary && (
            <ChevronDown className={`h-3 w-3 text-kumo-subtle transition-transform ${openArgs ? 'rotate-180' : ''}`} />
          )}
        </span>
      </Button>
      {openArgs && argSummary && (
        <div className="mt-2 rounded-lg bg-kumo-base p-2 font-mono text-[11px] break-all text-kumo-subtle ring-1 ring-kumo-line">
          {argSummary}
        </div>
      )}
      {status === 'failed' && error && (
        <div className="mt-2 flex items-start gap-2 rounded-lg bg-kumo-danger/10 p-2 text-kumo-danger">
          <span className="flex-1 break-all font-mono text-[11px]">{error}</span>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            shape="square"
            onClick={() => handleCopy(error)}
            className="shrink-0 rounded p-0.5 hover:bg-kumo-danger/10"
            aria-label="复制错误消息"
          >
            {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          </Button>
        </div>
      )}
    </div>
  );
}

/* ---------- 工具步骤组：连续 tool_call/tool_result 合并展示 ----------
 * 默认折叠；仅当「同工具+同路径+关键参数相同」的调用才连续合并计数 ×N——
 * 参数不同的调用各自独立成行，行内显示该次调用真实使用的关键参数
 * （hostId/action 等），running 行高亮 spinner，杜绝「复制上面内容」的观感。 */
export function ToolSteps({ items, streaming }) {
  const [open, setOpen] = useState(false);
  const [copiedKey, setCopiedKey] = useState(null);

  const isRunning = (c) => c.call?.status === STEP.RUNNING || c.result?.status === STEP.RUNNING;
  const isFailed = (c) => c.call?.status === STEP.FAILED || c.result?.status === STEP.FAILED;

  // tool_call ↔ tool_result 按 toolCallId 配对（结果挂到最后一个未配对同 id 调用）
  const calls = [];
  for (const p of items) {
    if (p.type === 'tool_call') {
      calls.push({ call: p, result: null });
    } else if (p.type === 'tool_result') {
      const last = calls[calls.length - 1];
      if (last && last.call && !last.result && (last.call.toolCallId === p.toolCallId || !p.toolCallId)) {
        last.result = p;
      } else {
        calls.push({ call: null, result: p });
      }
    }
  }

  // 相同调用（同工具+同路径+关键参数相同）连续合并 → 计数
  const merged = [];
  for (const c of calls) {
    const sig = c.call
      ? `${c.call.toolName}\u0000${toolPathLabel(c.call.toolName, c.call.args)}\u0000${callArgsKey(c.call.args)}`
      : `result\u0000${(c.result?.summary || '').slice(0, 120)}`;
    const prev = merged[merged.length - 1];
    if (prev && prev.sig === sig) {
      prev.count += 1;
      if (!prev.result && c.result) prev.result = c.result;
      if (isRunning(c)) prev.hasRunning = true;
      if (isFailed(c)) {
        prev.hasFailed = true;
        prev.failedCall = c.call;
      }
    } else {
      merged.push({
        sig,
        call: c.call,
        result: c.result,
        count: 1,
        hasRunning: isRunning(c),
        hasFailed: isFailed(c),
        failedCall: isFailed(c) ? c.call : null,
      });
    }
  }

  const total = merged.reduce((s, m) => s + m.count, 0);
  const isOpen = open || (streaming && merged.some((m) => m.hasRunning));
  const single = merged.length === 1 ? merged[0] : null;

  const handleCopy = async (key, text) => {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      setCopiedKey(key);
      setTimeout(() => setCopiedKey(null), 2000);
    } catch {
    }
  };

  const groupBadge = (m) => {
    if (m.hasRunning) {
      return (
        <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-kumo-brand/10 text-kumo-brand">
          <Loader size={10} className="animate-spin" />
        </span>
      );
    }
    if (m.hasFailed) {
      return (
        <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-kumo-danger/10 text-kumo-danger">
          <X className="h-2.5 w-2.5" />
        </span>
      );
    }
    return (
      <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-kumo-success/10 text-kumo-success">
        <Check className="h-2.5 w-2.5" />
      </span>
    );
  };

  return (
    <div className="flex min-w-0 flex-col gap-1">
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={() => setOpen(!open)}
        className="flex w-max max-w-full cursor-pointer items-center gap-1.5 rounded-lg border border-kumo-line/60 bg-kumo-recessed/60 py-1 pl-1.5 pr-2 text-[11px] text-kumo-default hover:bg-kumo-recessed hover:text-kumo-strong"
        aria-expanded={isOpen}
      >
        <span className="flex h-4 w-4 shrink-0 items-center justify-center rounded bg-kumo-brand/10 text-kumo-brand">
          <Terminal className="h-2.5 w-2.5" />
        </span>
        {single ? (
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="max-w-[240px] truncate leading-4">
              {single.call ? (single.call.desc || toolLabel(single.call.toolName) || '未知工具') : '工具结果'}
            </span>
            {single.count > 1 && (
              <span className="shrink-0 rounded-full bg-kumo-base px-1.5 py-px text-[10px] font-semibold leading-4 text-kumo-subtle">×{single.count}</span>
            )}
          </span>
        ) : (
          <span className="shrink-0 leading-4">工具步骤 · {total} 次</span>
        )}
        <ChevronDown className={`h-3 w-3 shrink-0 text-kumo-subtle transition-transform duration-200 ${isOpen ? 'rotate-180' : ''}`} />
      </Button>
      <div className="askai-collapse" data-open={isOpen}>
        <div className="askai-tool-stagger flex min-w-0 flex-col gap-1.5 rounded-lg border border-kumo-line/50 bg-kumo-control/40 p-2">
          {merged.map((m, i) => {
            const label = m.call ? (m.call.desc || toolLabel(m.call.toolName) || '未知工具') : '工具结果';
            const path = m.call ? toolPathLabel(m.call.toolName, m.call.args) : '';
            const argsKey = m.call ? callArgsKey(m.call.args) : '';
            const copyText = m.result?.summary || m.call?.error || '';
            // 该行自己的状态（逐行真实状态，不依赖组级汇总）
            const rowRunning = m.call?.status === STEP.RUNNING;
            const rowFailed = isFailed(m);
            return (
              <div key={`${m.sig}-${i}`} className={`flex min-w-0 flex-col gap-0.5 rounded-md px-1 ${rowRunning ? 'bg-kumo-brand/5' : ''}`}>
                <div className="flex min-w-0 items-center gap-1.5 text-xs">
                  {rowRunning ? (
                    <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-kumo-brand/10 text-kumo-brand">
                      <Loader size={10} className="animate-spin" />
                    </span>
                  ) : groupBadge(m)}
                  <span className="min-w-0 line-clamp-1 break-all font-medium text-kumo-default" title={label}>{label}</span>
                  {path && path !== label && (
                    <span className="min-w-0 line-clamp-1 break-all font-mono text-[10px] text-kumo-subtle/70" title={path}>{path}</span>
                  )}
                  {argsKey && (
                    <span className="min-w-0 line-clamp-1 break-all rounded bg-kumo-base/70 px-1 py-px font-mono text-[10px] text-kumo-brand/80" title={argsKey}>
                      {argsKey}
                    </span>
                  )}
                  {m.count > 1 && (
                    <span className="shrink-0 rounded-full bg-kumo-base px-1.5 py-px text-[10px] font-semibold leading-4 text-kumo-subtle">×{m.count}</span>
                  )}
                  {copyText && (
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      shape="square"
                      onClick={() => handleCopy(`${i}`, copyText)}
                      className="ml-auto !h-5 !w-5 shrink-0 !rounded !p-0 text-kumo-subtle hover:!bg-kumo-tint"
                      aria-label="复制工具结果"
                      title={m.result?.summary ? '复制工具结果' : '复制错误信息'}
                    >
                      {copiedKey === `${i}` ? <Check className="h-3 w-3 text-kumo-brand" /> : <Copy className="h-3 w-3" />}
                    </Button>
                  )}
                </div>
                {m.result?.summary && (
                  <p className="min-w-0 line-clamp-1 break-all pl-[22px] text-[11px] leading-5 text-kumo-subtle/70" title={m.result.summary}>
                    {m.result.summary}
                  </p>
                )}
                {rowFailed && m.failedCall?.error && (
                  <p className="min-w-0 break-all pl-[22px] font-mono text-[11px] leading-5 text-kumo-danger">{m.failedCall.error}</p>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}