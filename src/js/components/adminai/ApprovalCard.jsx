import React, { useEffect, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Loader } from '@cloudflare/kumo';

export default function ApprovalCard({ approval, onResolve }) {
  const { id, planSummary, targetMethod, targetPath, expiresAt, status } = approval || {};
  const [countdown, setCountdown] = useState('');
  const [resolving, setResolving] = useState(false);

  useEffect(() => {
    if (!expiresAt || status !== 'pending') return;
    const update = () => {
      const remaining = new Date(expiresAt).getTime() - Date.now();
      if (remaining <= 0) {
        setCountdown('已过期');
        return;
      }
      const mins = Math.floor(remaining / 60000);
      const secs = Math.floor((remaining % 60000) / 1000);
      setCountdown(`${mins}分${secs}秒`);
    };
    update();
    const timer = setInterval(update, 1000);
    return () => clearInterval(timer);
  }, [expiresAt, status]);

  const handleResolve = async (action) => {
    setResolving(true);
    try {
      await onResolve(id, action);
    } finally {
      setResolving(false);
    }
  };

  if (status === 'approved' || status === 'rejected') {
    return (
      <div className="rounded-lg border border-kumo-line bg-kumo-control p-3 text-xs">
        <div className="mb-2 flex items-center gap-2">
          <span className={`font-semibold ${status === 'approved' ? 'text-kumo-success' : 'text-kumo-danger'}`}>
            {status === 'approved' ? '已批准' : '已拒绝'}
          </span>
        </div>
        {planSummary && <div className="text-kumo-subtle">{planSummary}</div>}
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-kumo-brand/30 bg-kumo-brand/5 p-3 text-xs">
      <div className="mb-1 font-semibold text-kumo-strong">需要批准</div>
      {planSummary && <div className="mb-2 text-kumo-default">{planSummary}</div>}
      <div className="mb-3 space-y-0.5 font-mono text-kumo-subtle">
        {targetMethod && targetPath && (
          <div>
            <span className="font-semibold">{targetMethod}</span> {targetPath}
          </div>
        )}
      </div>
      {countdown && (
        <div className="mb-3 text-kumo-warning">
          剩余 {countdown}
        </div>
      )}
      <div className="flex gap-2">
        <Button
          size="sm"
          variant="primary"
          disabled={resolving}
          onClick={() => handleResolve('approve')}
        >
          {resolving ? <Loader size={12} /> : null}
          批准
        </Button>
        <Button
          size="sm"
          variant="destructive"
          disabled={resolving}
          onClick={() => handleResolve('reject')}
        >
          拒绝
        </Button>
      </div>
    </div>
  );
}