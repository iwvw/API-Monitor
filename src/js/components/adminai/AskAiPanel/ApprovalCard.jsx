import React, { useEffect, useMemo, useState } from 'react';
import { Button, Loader, DropdownMenu } from '@cloudflare/kumo';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { ChevronDown, ChevronRight, Check } from '../../Icons.jsx';

/* 审批卡片 — 计划摘要 + 参数 + 「N 处更改」展开 + 主次分层的操作区。
 *
 * 主次分层：主操作「仅此次」独占一行，其余三个动作（允许此对话/拒绝/请求更改）
 * 收进溢出菜单。此前四个按钮同排等权重，用户难以判断哪个是安全默认值。
 *
 * 「允许此对话」需要二次确认，改用菜单项的独立确认步骤（切到确认态再点一次），
 * 不再用 setTimeout 5 秒后静默复位——那种写法无法用键盘可靠操作，也不可测。
 *
 * 过期判断用 expiresAt 时间戳直接算 remainingMs，不再拿倒计时文案字符串比较。 */

function useRemainingMs(expiresAt, active) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active || !expiresAt) return undefined;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [active, expiresAt]);
  if (!expiresAt) return null;
  return new Date(expiresAt).getTime() - now;
}

function formatRemaining(ms) {
  if (ms === null) return '';
  if (ms <= 0) return '已过期';
  const mins = Math.floor(ms / 60000);
  const secs = Math.floor((ms % 60000) / 1000);
  return `${mins}分${secs}秒`;
}

export default function ApprovalCard({ approval, onResolve, remaining = 0 }) {
  const {
    id,
    planSummary,
    method,
    path,
    bodySnapshot,
    expiresAt,
    status,
    errorMessage,
  } = approval || {};
  const [resolving, setResolving] = useState(false);
  const [showDiff, setShowDiff] = useState(false);
  const [requestText, setRequestText] = useState('');
  const [requestOpen, setRequestOpen] = useState(false);
  const [confirmAllow, setConfirmAllow] = useState(false);

  const remainingMs = useRemainingMs(expiresAt, status === 'pending');
  const expired = remainingMs !== null && remainingMs <= 0;
  const countdown = formatRemaining(remainingMs);

  // 确认态允许取消：展开菜单时重置，避免上次的待确认状态残留。
  const closeMenu = () => setConfirmAllow(false);

  // 从 body 快照与 method/path 组合变更明细行
  const diffRows = useMemo(() => {
    const rows = [];
    if (bodySnapshot) {
      try {
        const parsed = typeof bodySnapshot === 'string' ? JSON.parse(bodySnapshot) : bodySnapshot;
        if (parsed && typeof parsed === 'object') {
          for (const [k, v] of Object.entries(parsed)) {
            rows.push({ key: k, value: typeof v === 'string' ? v : JSON.stringify(v) });
          }
        }
      } catch {
        rows.push({ key: 'body', value: String(bodySnapshot).slice(0, 200) });
      }
    }
    rows.push({ key: 'method', value: method || 'GET' });
    if (path) rows.push({ key: 'path', value: path });
    return rows;
  }, [bodySnapshot, method, path]);

  const changeCount = diffRows.length;

  const handleResolve = async (action, applyToSession, message) => {
    setResolving(true);
    try {
      await onResolve(id, action, applyToSession, message);
    } finally {
      setResolving(false);
    }
  };

  const handleRequestChanges = async () => {
    if (!requestText.trim()) return;
    setResolving(true);
    try {
      await onResolve(id, 'reject', false, requestText.trim());
    } finally {
      setResolving(false);
      setRequestOpen(false);
      setRequestText('');
    }
  };

  if (status === 'approved' || status === 'rejected' || status === 'error') {
    return (
      <div className="rounded-xl bg-kumo-base px-4 py-3 ring-1 ring-kumo-line">
        <div className="mb-1.5 flex items-center gap-2 text-xs">
          {status === 'approved' ? (
            <span className="flex items-center gap-1 font-medium text-kumo-success">
              <Check className="h-3.5 w-3.5" /> 已批准
            </span>
          ) : status === 'error' ? (
            <span className="font-medium text-kumo-warning">处理失败</span>
          ) : (
            <span className="font-medium text-kumo-danger">已拒绝</span>
          )}
        </div>
        {errorMessage ? (
          <div className="text-xs text-kumo-warning">{errorMessage}</div>
        ) : planSummary ? (
          <div className="text-xs text-kumo-subtle">{planSummary}</div>
        ) : null}
      </div>
    );
  }

  return (
    <div className="overflow-visible rounded-xl bg-kumo-base px-4 py-3 ring-1 ring-kumo-line">
      {/* 计划摘要 */}
      <div className="text-sm font-medium text-kumo-default">{planSummary}</div>
      {/* 参数详情（method/path） */}
      <div className="mt-1.5 text-xs leading-relaxed text-kumo-default">
        <span className="mr-1 rounded bg-kumo-fill px-1.5 py-0.5 font-mono text-[11px]">{method || 'GET'}</span>
        <code className="rounded bg-kumo-fill px-1.5 py-0.5 font-mono text-[11px]">{path || ''}</code>
      </div>

      {/* 「N 处更改」展开区 */}
      {diffRows.length > 0 && (
        <div className="mt-2">
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setShowDiff(!showDiff)}
            className="flex items-center gap-1 text-xs !text-brand hover:!text-kumo-strong"
            aria-expanded={showDiff}
          >
            {showDiff ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
            {changeCount} 处更改
          </Button>
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
        <div className={`mt-2 text-[11px] ${expired ? 'text-kumo-danger' : 'text-kumo-warning'}`}>
          剩余 {countdown}
        </div>
      )}

      {/* 请求更改输入 */}
      {requestOpen && (
        <div className="mt-3">
          <Textarea
            className="w-full"
            placeholder="描述需要更改的内容……"
            rows={3}
            maxLength={1000}
            value={requestText}
            onChange={(e) => setRequestText(e.target.value)}
          />
          <div className="mt-1.5 flex items-center justify-between">
            <span className="text-[10px] text-kumo-subtle">{requestText.length}/1,000</span>
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="ghost"
                onClick={() => { setRequestOpen(false); setRequestText(''); }}
              >
                取消
              </Button>
              <Button
                size="sm"
                variant="primary"
                disabled={!requestText.trim() || resolving}
                onClick={handleRequestChanges}
              >
                发送
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* 操作区：主操作独占一行，次级动作收进溢出菜单 */}
      {!requestOpen && (
        <div className="mt-3 flex items-center gap-2">
          <Button
            size="sm"
            variant="primary"
            disabled={resolving || expired}
            onClick={() => handleResolve('approve', false)}
          >
            {resolving ? <Loader size={12} /> : null}
            仅此次
          </Button>
          <DropdownMenu>
            <DropdownMenu.Trigger
              render={(
                <Button size="sm" variant="secondary" disabled={resolving || expired} aria-label="更多操作">
                  更多
                  <ChevronDown className="h-3 w-3" />
                </Button>
              )}
            />
            <DropdownMenu.Content onClose={closeMenu}>
              <DropdownMenu.Item
                onClick={() => {
                  if (!confirmAllow) {
                    setConfirmAllow(true);
                    return;
                  }
                  handleResolve('approve', true);
                  setConfirmAllow(false);
                }}
              >
                {confirmAllow ? '确认允许本会话全部写操作' : '允许此对话'}
              </DropdownMenu.Item>
              <DropdownMenu.Item onClick={() => setRequestOpen(true)}>
                请求更改
              </DropdownMenu.Item>
              <DropdownMenu.Separator />
              <DropdownMenu.Item variant="danger" onClick={() => handleResolve('reject')}>
                拒绝
              </DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu>
          {confirmAllow && (
            <span className="text-[10px] text-kumo-warning">再次点击确认本会话全部写操作</span>
          )}
        </div>
      )}
      {remaining > 1 && (
        <div className="mt-2 text-[10px] text-kumo-subtle">还有 {remaining - 1} 条待审批</div>
      )}
    </div>
  );
}
