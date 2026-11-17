import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { formatDateTime } from '../../modules/utils.js';
import { StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import {
  costDetailsFor,
  formatCostAmount,
  logOutputSpeedText,
  statusCodeTone,
} from './utils.js';

function DetailRow({ label, children }) {
  return (
    <div className="flex min-w-0 items-center justify-between gap-3">
      <span className="shrink-0 text-kumo-subtle">{label}</span>
      <span className="min-w-0 truncate text-right text-kumo-strong">{children}</span>
    </div>
  );
}

export function MobileLogDetailDialog({ log, endpoints, onClose, onShowError }) {
  const detail = log ? costDetailsFor(log, endpoints) : null;
  const actionable = !!log && log.statusCode >= 400 && log.errorResponse;
  const cachePercent =
    log && log.promptTokens > 0
      ? ((Number(log.cachedTokens) / Number(log.promptTokens)) * 100).toFixed(1)
      : '0.0';
  return (
    <LayerDialog.Root open={!!log} onOpenChange={open => !open && onClose()}>
      <LayerDialog.Content size="lg">
        <LayerDialog.Title>日志详情</LayerDialog.Title>
        <LayerDialog.Description>
          {log ? `${formatDateTime(log.timestamp)} · ${log.model || '—'}` : ''}
        </LayerDialog.Description>
        <LayerDialog.Body>
          {log && (
            <div className="flex flex-col gap-3 text-xs">
              <div className="flex flex-wrap items-center gap-1.5">
                <StatusBadge tone={statusCodeTone(log.statusCode)}>{log.statusCode}</StatusBadge>
                {log.statusCode === 503 && <StatusBadge tone="warning">网关无可用渠道</StatusBadge>}
                <StatusBadge tone={log.stream ? 'info' : 'warning'}>
                  {log.stream ? '流式' : '非流式'}
                </StatusBadge>
                {typeof log.keyIndex === 'number' && log.keyIndex >= 0 && (
                  <StatusBadge tone="info" title="使用的 API Key 序号(0=主 key)">
                    K{log.keyIndex + 1}
                  </StatusBadge>
                )}
              </div>
              <div className="grid gap-1.5">
                <DetailRow label="端点">{log.endpointName || '—'}</DetailRow>
                <DetailRow label="模型">{log.model || '—'}</DetailRow>
                {log.realModel && log.realModel !== log.model && (
                  <DetailRow label="实际模型">
                    <span className="font-mono text-kumo-info">{log.realModel}</span>
                  </DetailRow>
                )}
                <DetailRow label="输入 / 输出">
                  {log.promptTokens} / {log.completionTokens}
                </DetailRow>
                <DetailRow label="缓存">
                  {log.cachedTokens}({cachePercent}%)
                </DetailRow>
                <DetailRow label="总词元">{log.totalTokens}</DetailRow>
                <DetailRow label="费用">
                  {detail ? formatCostAmount(detail.cost, detail.currency) : '—'}
                </DetailRow>
                <DetailRow label="耗时">{(log.latencyMs / 1000).toFixed(1)}s</DetailRow>
                <DetailRow label="首字">
                  {log.ttfbMs > 0 ? `${(log.ttfbMs / 1000).toFixed(1)}s` : '—'}
                </DetailRow>
                <DetailRow label="输出速度">{logOutputSpeedText(log) || '—'}</DetailRow>
                <DetailRow label="出口 IP">
                  <span className="font-mono">{log.upstreamIp || '—'}</span>
                </DetailRow>
                <DetailRow label="客户端 IP">
                  <span className="font-mono">{log.clientIp || '—'}</span>
                </DetailRow>
              </div>
              {actionable && (
                <div className="flex justify-end">
                  <Button size="sm" variant="secondary" onClick={() => onShowError(log)}>
                    查看报错详情
                  </Button>
                </div>
              )}
            </div>
          )}
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="关闭">
          <LayerDialog.Action type="button" onClick={onClose}>
            关闭
          </LayerDialog.Action>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}