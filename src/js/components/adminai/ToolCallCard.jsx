import React, { useState } from 'react';
import { Button, Loader } from '@cloudflare/kumo';
import { Copy, Check, ChevronDown, X } from '../Icons.jsx';

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

/* 工具调用卡片 — Cloudflare Agent「→ Running …」步骤行风格：
 * inline 模式用于 thinking 折叠区；完整模式带参数展开与错误复制。
 * 状态环：running=品牌蓝 spinner+脉冲边条 / success=绿对勾 / failed=红叉 */
export default function ToolCallCard({ toolCall, inline, showPath }) {
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

  /* 内联行：语义视图=中文动作描述（无 desc 时回退工具中文名，绝不显示路径）；
     路径视图=方法+路径（showPath） */
  if (inline) {
    const label = showPath
      ? toolPathLabel(toolName, args)
      : (desc || toolLabel(toolName) || '未知工具');
    return (
      <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-kumo-default">
        <span className="truncate">{label}</span>
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
        <span className="truncate font-medium text-kumo-default">{action || '未知工具'}</span>
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