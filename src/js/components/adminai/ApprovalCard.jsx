import React, { useEffect, useState } from 'react';
import { Loader } from '@cloudflare/kumo';
import { ChevronDown, ChevronRight, Check } from '../Icons.jsx';

/* 审批卡片 — Cloudflare Agent 风格：
 * 计划摘要 + 参数 code 高亮 + 「N 处更改」展开 diff + 4 操作按钮 + 请求更改输入 */
export default function ApprovalCard({ approval, onResolve }) {
  const {
    id,
    planSummary,
    method,
    path,
    bodySnapshot,
    expiresAt,
    status,
  } = approval || {};
  const [countdown, setCountdown] = useState('');
  const [resolving, setResolving] = useState(false);
  const [showDiff, setShowDiff] = useState(false);
  const [requestText, setRequestText] = useState('');
  const [requestOpen, setRequestOpen] = useState(false);

  useEffect(() => {
    if (!expiresAt || status !== 'pending') return;
    const update = () => {
      const remaining = new Date(expiresAt).getTime() - Date.now();
      if (remaining <= 0) { setCountdown('已过期'); return; }
      const mins = Math.floor(remaining / 60000);
      const secs = Math.floor((remaining % 60000) / 1000);
      setCountdown(`${mins}分${secs}秒`);
    };
    update();
    const timer = setInterval(update, 1000);
    return () => clearInterval(timer);
  }, [expiresAt, status]);

  // 从 body 快照与 method/path 组合变更明细行（近似 Cloudflare 的「N 处更改」）
  const diffRows = [];
  if (bodySnapshot) {
    try {
      const parsed = typeof bodySnapshot === 'string' ? JSON.parse(bodySnapshot) : bodySnapshot;
      if (parsed && typeof parsed === 'object') {
        for (const [k, v] of Object.entries(parsed)) {
          diffRows.push({ key: k, value: typeof v === 'string' ? v : JSON.stringify(v) });
        }
      }
    } catch {
      diffRows.push({ key: 'body', value: String(bodySnapshot).slice(0, 200) });
    }
  }
  diffRows.push({ key: 'method', value: method || 'GET' });
  if (path) diffRows.push({ key: 'path', value: path });

  const handleResolve = async (action) => {
    setResolving(true);
    try {
      await onResolve(id, action);
    } finally {
      setResolving(false);
    }
  };

  const handleRequestChanges = async () => {
    if (!requestText.trim()) return;
    setResolving(true);
    try {
      await onResolve(id, 'reject');
    } finally {
      setResolving(false);
      setRequestOpen(false);
      setRequestText('');
    }
  };

  if (status === 'approved' || status === 'rejected') {
    return (
      <div className="rounded-xl bg-kumo-base px-4 py-3 ring-1 ring-kumo-line">
        <div className="mb-1.5 flex items-center gap-2 text-xs">
          {status === 'approved' ? (
            <span className="flex items-center gap-1 font-medium text-kumo-success">
              <Check className="h-3.5 w-3.5" /> 已批准
            </span>
          ) : (
            <span className="font-medium text-kumo-danger">已拒绝</span>
          )}
        </div>
        {planSummary && <div className="text-xs text-kumo-subtle">{planSummary}</div>}
      </div>
    );
  }

  return (
    <div className="overflow-visible rounded-xl bg-kumo-base px-4 py-3 ring-1 ring-kumo-line">
      {/* 计划摘要 */}
      <div className="text-sm font-medium text-kumo-default">{planSummary}</div>
      {/* 参数详情（method/path + body） */}
      <div className="mt-1.5 text-xs leading-relaxed text-kumo-default">
        <span className="mr-1 rounded bg-kumo-fill px-1.5 py-0.5 font-mono text-[11px]">{method || 'GET'}</span>
        <code className="rounded bg-kumo-fill px-1.5 py-0.5 font-mono text-[11px]">{path || ''}</code>
      </div>

      {/* 「N 处更改」展开区 */}
      {diffRows.length > 0 && (
        <div className="mt-2">
          <button
            type="button"
            onClick={() => setShowDiff(!showDiff)}
            className="flex items-center gap-1 text-xs text-kumo-brand transition-colors hover:text-kumo-strong"
          >
            {showDiff ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
            {diffRows.length} 处更改
          </button>
          {showDiff && (
            <div className="mt-2 space-y-1 rounded-lg bg-kumo-control p-2.5 font-mono text-[11px] text-kumo-default">
              {diffRows.map((row, i) => (
                <div key={i} className="flex gap-2">
                  <span className="shrink-0 text-kumo-subtle">{row.key}</span>
                  <span className="break-all">{row.value}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* 倒计时 */}
      {countdown && (
        <div className="mt-2 text-[11px] text-kumo-warning">剩余 {countdown}</div>
      )}

      {/* 请求更改输入 */}
      {requestOpen && (
        <div className="mt-3">
          <textarea
            className="w-full resize-none rounded-lg border-0 bg-kumo-control p-2.5 text-xs text-kumo-default outline-none ring-1 ring-kumo-line focus:ring-kumo-brand/50"
            placeholder="描述需要更改的内容……"
            rows={3}
            maxLength={1000}
            value={requestText}
            onChange={(e) => setRequestText(e.target.value)}
          />
          <div className="mt-1.5 flex items-center justify-between">
            <span className="text-[10px] text-kumo-subtle">{requestText.length}/1,000</span>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => { setRequestOpen(false); setRequestText(''); }}
                className="rounded-md px-2.5 py-1 text-xs text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default"
              >
                取消
              </button>
              <button
                type="button"
                disabled={!requestText.trim() || resolving}
                onClick={handleRequestChanges}
                className="rounded-md bg-kumo-brand px-2.5 py-1 text-xs font-medium text-white transition-opacity disabled:opacity-40"
              >
                发送
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 操作按钮：允许此对话 / 仅此次 / 拒绝 / 请求更改 */}
      {!requestOpen && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <button
            type="button"
            disabled={resolving}
            onClick={() => handleResolve('approve')}
            className="inline-flex h-7 items-center gap-1.5 rounded-md bg-kumo-success px-2.5 text-xs font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-50"
          >
            {resolving ? <Loader size={12} /> : null}
            允许此对话
          </button>
          <button
            type="button"
            disabled={resolving}
            onClick={() => handleResolve('approve')}
            className="inline-flex h-7 items-center gap-1.5 rounded-md bg-kumo-fill px-2.5 text-xs font-medium text-kumo-default ring-1 ring-kumo-line transition-colors hover:bg-kumo-tint disabled:opacity-50"
          >
            仅此次
          </button>
          <button
            type="button"
            disabled={resolving}
            onClick={() => handleResolve('reject')}
            className="inline-flex h-7 items-center rounded-md px-2.5 text-xs font-medium text-kumo-danger ring-1 ring-kumo-line transition-colors hover:bg-kumo-danger/10 disabled:opacity-50"
          >
            拒绝
          </button>
          <button
            type="button"
            disabled={resolving}
            onClick={() => setRequestOpen(true)}
            className="inline-flex h-7 items-center rounded-md px-2.5 text-xs font-medium text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default disabled:opacity-50"
          >
            请求更改
          </button>
        </div>
      )}
    </div>
  );
}