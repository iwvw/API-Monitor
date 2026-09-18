import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { toast } from '../../modules/toast.js';
import { formatDateTime } from '../../modules/utils.js';
import { StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { errorKindLabel, formatErrorResponseForDisplay } from './utils.js';
import { LOG_DETAIL_COLLAPSE_LIMIT } from './constants.js';

export function LogDetailDialog({ analytics }) {
  const {
    logDetail, setLogDetail,
    logDetailExpanded, setLogDetailExpanded,
  } = analytics;
  return (
      <LayerDialog.Root open={!!logDetail} onOpenChange={open => !open && setLogDetail(null)}>
        <LayerDialog.Content size="lg">
          <LayerDialog.Title>报错详情</LayerDialog.Title>
          <LayerDialog.Description>
            {logDetail && (
              <>
                {formatDateTime(logDetail.timestamp)} · {logDetail.route || 'chat.completions'} ·{' '}
                {logDetail.model || '—'} · 状态 {logDetail.statusCode}
                {logDetail.endpointName ? ` · ${logDetail.endpointName}` : ''}
              </>
            )}
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="flex flex-col gap-3">
              {logDetail?.errorKind && (
                <div>
                  <StatusBadge tone="danger" title={`错误环节：${logDetail.errorKind}`}>
                    {errorKindLabel(logDetail.errorKind)}
                  </StatusBadge>
                </div>
              )}
              {logDetail?.errorMessage && (
                <div className="rounded-md border border-kumo-danger/25 bg-kumo-danger/5 px-3 py-2 text-xs font-medium text-kumo-danger">
                  {logDetail.errorMessage}
                </div>
              )}
              {logDetail?.errorResponse && (
                <div className="flex flex-wrap justify-end gap-2">
                  {logDetail.errorResponse.length > LOG_DETAIL_COLLAPSE_LIMIT ? (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setLogDetailExpanded(v => !v)}
                      title={logDetailExpanded ? '折叠为预览内容' : '显示完整报错 JSON'}
                    >
                      {logDetailExpanded ? '收起' : '展开全部'}
                    </Button>
                  ) : null}
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      navigator.clipboard
                        .writeText(String(logDetail?.errorResponse || ''))
                        .then(() => toast.success('报错 JSON 已复制'))
                        .catch(() => toast.error('复制失败'));
                    }}
                  >
                    复制报错 JSON
                  </Button>
                </div>
              )}
          <div className="min-h-0 flex-1 overflow-auto scrollbar-thin">
            {(() => {
              if (!logDetail?.errorResponse) {
                return (
                  <div className="text-xs text-kumo-subtle">
                    该请求无报错 JSON 记录（如流式响应未采集响应体，可查看调用日志行与 relay-errors 接口）。
                  </div>
                );
              }
              let parses = true;
              try {
                JSON.parse(logDetail.errorResponse);
              } catch {
                parses = false;
              }
              const truncated = logDetail.errorResponse.includes('...(truncated)');
              if (!parses || truncated) {
                return (
                  <div className="mb-3 rounded-md border border-kumo-warning/30 bg-kumo-warning/10 px-3 py-2 text-xs text-kumo-warning">
                    {truncated
                      ? '报错 JSON 超过记录上限（64KB）已截断，内容不完整'
                      : '该内容不是标准 JSON，以下为原始内容排版'}
                  </div>
                );
              }
              return null;
            })()}
            {logDetail?.errorResponse && (
              <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-kumo-strong">
                {(() => {
                  const text = formatErrorResponseForDisplay(logDetail.errorResponse);
                  if (logDetailExpanded || text.length <= LOG_DETAIL_COLLAPSE_LIMIT) return text;
                  return `${text.slice(0, LOG_DETAIL_COLLAPSE_LIMIT)}…\n\n（内容较长，仅显示前 ${LOG_DETAIL_COLLAPSE_LIMIT} 字符）`;
                })()}
              </pre>
            )}
          </div>
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="关闭">
            <LayerDialog.Actions.Primary type="button" onClick={() => setLogDetail(null)}>
              关闭
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
  );
}
