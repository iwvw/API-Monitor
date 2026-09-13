import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { toast } from '../../modules/toast.js';
import { AppCard, EmptyState, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Copy, FileText } from '../../components/Icons.jsx';
import { InfoRow, ParamTable, RouteMethodPills, SnippetBox } from './components.jsx';
import { AUTH_LABEL, AUTH_TONE, RESPONSE_LABEL, STATUS_LABEL, STATUS_TONE } from './constants.js';
import { buildCurlExample, formatJSON } from './utils.js';

export default function RouteDetail({ route, openapiRoute }) {
  const copyText = async (text, message) => {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(message);
    } catch (error) {
      console.error('copy failed:', error);
      toast.error('复制失败');
    }
  };

  if (!route) {
    return (
      <div>
        <AppCard padding="none" className="flex min-h-0 items-center justify-center">
          <EmptyState
            icon={FileText}
            title="选择一个接口"
            description="从左侧选择接口"
            card={false}
            className="min-h-0"
          />
        </AppCard>
      </div>
    );
  }

  const curl = buildCurlExample(route);

  return (
    <SectionCard
      title={<span className="break-all font-mono text-base">{route.prefix}</span>}
      icon={<FileText className="h-4 w-4 text-brand" />}
      meta={
        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge tone={STATUS_TONE[route.status]}>
            {STATUS_LABEL[route.status] || route.status}
          </StatusBadge>
          <StatusBadge tone={AUTH_TONE[route.auth]}>
            {AUTH_LABEL[route.auth] || route.auth}
          </StatusBadge>
          <StatusBadge tone="neutral">
            {RESPONSE_LABEL[route.responseMode] || route.responseMode}
          </StatusBadge>
        </div>
      }
      actions={
        <div className="flex shrink-0 gap-2">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => copyText(route.prefix, '接口路径已复制')}
            className="gap-1.5"
          >
            <Copy className="h-3.5 w-3.5" />
            <span>路径</span>
          </Button>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => copyText(curl, 'cURL 已复制')}
            className="gap-1.5"
          >
            <Copy className="h-3.5 w-3.5" />
            <span>cURL</span>
          </Button>
        </div>
      }
      className="min-h-0"
      bodyPadding="lg"
      bodyClassName="flex min-w-0 flex-col"
    >
      <div className="grid gap-3 py-4 cq-sm:grid-cols-2">
        <InfoRow label="模块" value={route.module} />
        <InfoRow label="分组" value={route.group} />
        <InfoRow label="归属" value={route.owner} />
        <InfoRow label="匹配模式" value={route.matchMode} />
        <InfoRow label="认证方式" value={AUTH_LABEL[route.auth] || route.auth} />
        <InfoRow
          label="响应类型"
          value={RESPONSE_LABEL[route.responseMode] || route.responseMode}
        />
      </div>

      <div className="flex-1 space-y-3 border-t border-kumo-line pt-4">
        <div>
          <div className="mb-2 text-xs font-semibold text-kumo-subtle">接口说明</div>
          <div className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2 text-xs leading-relaxed text-kumo-subtle">
            {route.detail || route.description}
          </div>
        </div>
        <div>
          <div className="mb-2 text-xs font-semibold text-kumo-subtle">请求方法</div>
          <RouteMethodPills methods={route.methods} />
        </div>
        <ParamTable title="路径参数" items={route.pathParams} />
        <ParamTable title="查询参数" items={route.queryParams} />
        <ParamTable title="认证与请求头" items={route.headers} />
        {route.notes?.length ? (
          <div>
            <div className="mb-2 text-xs font-semibold text-kumo-subtle">调用提示</div>
            <div className="space-y-2">
              {route.notes.map(note => (
                <div
                  key={note}
                  className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2 text-xs leading-relaxed text-kumo-subtle"
                >
                  {note}
                </div>
              ))}
            </div>
          </div>
        ) : null}
        <SnippetBox label="cURL 示例" value={curl} onCopy={copyText} />
        {route.requestExample ? (
          <SnippetBox label="请求示例" value={formatJSON(route.requestExample)} onCopy={copyText} />
        ) : null}
        {route.responseExample ? (
          <SnippetBox
            label="响应示例"
            value={formatJSON(route.responseExample)}
            onCopy={copyText}
          />
        ) : null}
        {openapiRoute && (
          <div>
            <div className="mb-2 text-xs font-semibold text-kumo-subtle">OpenAPI 文档</div>
            <div className="flex min-w-0 flex-wrap items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed/40 px-3 py-2">
              <span className="min-w-0 flex-1 truncate font-mono text-xs text-kumo-strong">
                {openapiRoute}
              </span>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => copyText(openapiRoute, 'OpenAPI 地址已复制')}
                className="gap-1.5"
              >
                <Copy className="h-3.5 w-3.5" />
                <span>复制</span>
              </Button>
            </div>
          </div>
        )}
      </div>
    </SectionCard>
  );
}
