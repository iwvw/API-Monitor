import { Button } from '@cloudflare/kumo/components/button';
import { normalizeWorkflowJobName } from '../../modules/githubWorkflowJobs.js';
import { statusLabel } from './constants.js';
import { actionFlowStatusDotClass, actionFlowStatusMetaClass, formatDateTime } from './utils.js';

function ActionStatusDot({ status, muted = false }) {
  return (
    <span
      aria-hidden="true"
      className={`mt-[0.4rem] inline-block h-3 w-3 shrink-0 rounded-full ring-1 ring-offset-1 ring-offset-kumo-base ${actionFlowStatusDotClass(status)} ${muted ? 'opacity-72' : 'opacity-100'}`}
    />
  );
}

function ActionFlowNode({
  node,
  style,
  expanded = false,
  highlighted = null,
  spotlighted = false,
  nodeRef = null,
  onToggle,
  onFocus,
  onBlur,
  onDefinitionFocus,
  onDefinitionBlur,
}) {
  const showActiveStep = Boolean(node.step && ['in_progress', 'running', 'queued', 'pending', 'waiting'].includes(normalizeWorkflowJobName(node.status)));
  const variantPreview = node.variants || [];
  const compact = !node.group && !node.matrix && !node.step && variantPreview.length <= 1;
  const active = highlighted === true;
  const muted = highlighted === false;
  const strongTextClass = muted ? 'text-kumo-subtle' : 'text-kumo-strong';
  const subtleTextClass = muted ? 'text-kumo-subtle/80' : 'text-kumo-subtle';
  const metaTextClass = actionFlowStatusMetaClass(node.status, muted);
  const completedVariants = variantPreview.filter((variant) => ['success', 'completed'].includes(normalizeWorkflowJobName(variant.status))).length;
  const matrixSummary = node.count > 0
    ? `${completedVariants || node.count}/${node.count} 个任务${completedVariants === node.count ? '已完成' : '处理中'}`
    : '矩阵任务';
  return (
    <div
      ref={nodeRef}
      className={`absolute grid min-w-0 content-start gap-1.5 overflow-visible rounded-md border bg-kumo-base px-3 py-2.5 transition-[border-color,box-shadow,opacity,filter] ${spotlighted ? 'z-40 border-brand/60 ring-2 ring-brand/20' : 'z-20'} ${active && !spotlighted ? 'border-brand/45 ring-1 ring-brand/20' : ''} ${!active && !spotlighted ? 'border-kumo-interact/70' : ''} ${muted ? 'opacity-[0.42] saturate-75' : 'opacity-100'}`}
      onClick={(event) => event.stopPropagation()}
      onMouseDown={(event) => event.stopPropagation()}
      onMouseEnter={onFocus}
      onMouseLeave={onBlur}
      onFocus={(event) => {
        if (event.target === event.currentTarget) onFocus?.();
      }}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) onBlur?.();
      }}
      style={style}
    >
      {node.group ? (
        <>
          {node.name && (
            <div className="flex min-w-0 items-start gap-2">
              <ActionStatusDot status={node.status} muted={muted} />
              <div className={`min-w-0 truncate text-sm font-semibold leading-6 ${strongTextClass}`} title={node.name}>{node.name}</div>
            </div>
          )}
          <div className="grid gap-2">
            {variantPreview.map((variant) => (
              <div
                key={variant.id || variant.name}
                className="flex min-w-0 items-center justify-between gap-3 py-1 text-sm leading-6"
              >
                <div className="flex min-w-0 items-start gap-2">
                  <ActionStatusDot status={variant.status} muted={muted} />
                  <span className={`min-w-0 truncate font-semibold ${strongTextClass}`} title={variant.name}>{variant.name}</span>
                </div>
                <span className={`shrink-0 whitespace-nowrap text-xs leading-6 ${actionFlowStatusMetaClass(variant.status, muted)}`} title={`${statusLabel(variant.status)} · ${variant.duration}`}>
                  {variant.duration || statusLabel(variant.status)}
                </span>
              </div>
            ))}
          </div>
        </>
      ) : (
        <>
          <div className="flex min-w-0 items-start justify-between gap-2 leading-6">
            <div className="flex min-w-0 items-start gap-2">
              <ActionStatusDot status={node.status} muted={muted} />
              <div className="min-w-0">
                <div className={`truncate text-sm font-semibold leading-6 ${strongTextClass}`} title={node.name}>{node.name}</div>
                {node.matrix && <div className={`truncate text-xs leading-6 ${subtleTextClass}`}>{matrixSummary}</div>}
              </div>
            </div>
            <span className={`shrink-0 whitespace-nowrap text-xs font-medium leading-6 ${metaTextClass}`} title={`${statusLabel(node.status)} · ${node.duration}`}>
              {node.duration || statusLabel(node.status)}
            </span>
          </div>
          {!compact && (
            <>
              {showActiveStep && (
                <div className={`truncate text-xs leading-6 ${strongTextClass}`} title={node.step.name}>当前步骤：{node.step.name}</div>
              )}
              {variantPreview.length > 1 ? (
                <Button
                  type="button"
                  size="xs"
                  variant="ghost"
                  className={`h-auto min-w-0 justify-start px-0 py-0 text-xs leading-6 ${metaTextClass}`}
                  onClick={(event) => {
                    event.stopPropagation();
                    onToggle?.();
                  }}
                >
                  {expanded ? '收起 Jobs' : '查看全部 Jobs'}
                </Button>
              ) : (
                <div className={`truncate text-xs leading-6 ${subtleTextClass}`}>{node.completedAt ? '已完成' : '等待运行'}</div>
              )}
              {expanded && variantPreview.length > 1 && (
                <div className="grid gap-2 border-t border-kumo-line pt-3">
                  {variantPreview.map((variant) => (
                    <div
                      key={variant.id || variant.name}
                      className="flex min-w-0 items-center justify-between gap-2 py-1 text-xs leading-6"
                      onMouseEnter={() => onDefinitionFocus?.(variant.definitionId || variant.id)}
                      onMouseLeave={onDefinitionBlur}
                      onFocus={(event) => {
                        event.stopPropagation();
                        onDefinitionFocus?.(variant.definitionId || variant.id);
                      }}
                      onBlur={() => {
                        onDefinitionBlur?.();
                      }}
                      tabIndex={0}
                    >
                      <div className="flex min-w-0 items-start gap-2">
                        <ActionStatusDot status={variant.status} muted={muted} />
                        <span className={`min-w-0 truncate ${strongTextClass}`} title={variant.name}>{variant.name}</span>
                      </div>
                      <span className={`shrink-0 whitespace-nowrap text-[11px] leading-6 ${actionFlowStatusMetaClass(variant.status, muted)}`} title={`${statusLabel(variant.status)} · ${variant.duration}`}>
                        {variant.duration || statusLabel(variant.status)}
                      </span>
                    </div>
                  ))}
                </div>
              )}
              <div className={`truncate text-xs leading-6 ${metaTextClass}`}>{node.completedAt ? `完成于 ${formatDateTime(node.completedAt)}` : '实时更新中'}</div>
            </>
          )}
        </>
      )}
    </div>
  );
}

export default ActionFlowNode;
export { ActionStatusDot };
