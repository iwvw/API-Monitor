import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { LayerCard } from '@cloudflare/kumo';

// 轻量卡片原语:只依赖 LayerCard / Button / SkeletonLine。
//
// 公开页(公开书签、分享、提示词、订阅信息、状态页等)实际只需要其中的
// SectionCard / ChartWarmupSkeleton / cx / iconButtonIconClass,但历史上都从
// AppPrimitives 导入,于是被迫连带拉取 AppPrimitives —— 后者又引入
// Table/Popover/Select 与全量图标库(Icons.jsx),使每个公开页首屏多下载
// 约 460KB。把这些原语拆到本模块后,公开页按需只加载这一小份;
// AppPrimitives 继续 re-export 同样的名字,既有导入路径不受影响。

export const sectionCardHeaderClass =
  'flex min-h-[52px] items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed px-4 py-2.5 cq-sm:min-h-[56px] cq-sm:flex-row cq-sm:flex-wrap cq-sm:items-center cq-sm:py-3.5';
export const sectionCardTitleClass =
  'inline-flex min-w-0 max-w-full items-center gap-2 text-sm font-semibold text-kumo-strong';
export const iconButtonIconClass = 'h-3.5 w-3.5';
export const actionIconClass = 'h-4 w-4';

export const cardPaddingClass = {
  none: 'p-0',
  sm: 'p-3',
  md: 'p-4',
  lg: 'p-5',
  xl: 'p-6',
};

export function cx(...parts) {
  return parts.filter(Boolean).join(' ');
}

function withCompactCardActions(node) {
  if (Array.isArray(node)) return node.map(withCompactCardActions);
  if (!React.isValidElement(node)) return node;

  if (node.type === React.Fragment) {
    return (
      <React.Fragment key={node.key}>
        {React.Children.map(node.props.children, withCompactCardActions)}
      </React.Fragment>
    );
  }

  if (node.type === Button) {
    const hasIconAndLabel = Boolean(node.props.icon && node.props.children);
    const textLabel = typeof node.props.children === 'string' || typeof node.props.children === 'number'
      ? String(node.props.children)
      : undefined;

    if (hasIconAndLabel) {
      const isSmall = (node.props.size || 'sm') === 'sm';
      const compactClass = isSmall ? 'max-sm:!px-0 max-sm:!w-6.5 max-sm:!justify-center' : 'max-sm:!px-0 max-sm:!w-9 max-sm:!justify-center';
      return React.cloneElement(node, {
        size: node.props.size || 'sm',
        className: cx(node.props.className, compactClass),
        children: <span className="hidden cq-sm:inline">{node.props.children}</span>,
        'aria-label': node.props['aria-label'] || textLabel || node.props.title,
      });
    }

    return node;
  }

  if (!node.props?.children) return node;
  return React.cloneElement(node, {
    children: React.Children.map(node.props.children, withCompactCardActions),
  });
}

export function SectionCard({
  title,
  description,
  icon,
  meta,
  action,
  actions,
  actionsClassName = '',
  children,
  className = '',
  headerClassName = '',
  bodyClassName = '',
  bodyPadding = 'md',
  titleClassName = '',
  descriptionClassName = '',
  ...props
}) {
  const trailing = [
    meta && <React.Fragment key="meta">{withCompactCardActions(meta)}</React.Fragment>,
    action && <React.Fragment key="action">{withCompactCardActions(action)}</React.Fragment>,
    actions && <React.Fragment key="actions">{withCompactCardActions(actions)}</React.Fragment>,
  ].filter(Boolean);
  return (
    <LayerCard
      {...props}
      className={cx(
        'flex flex-col overflow-hidden rounded-lg border border-kumo-line bg-kumo-base p-0 shadow-none ring-0',
        className
      )}
    >
      <LayerCard.Secondary className={cx(sectionCardHeaderClass, headerClassName)}>
        <div className="flex min-w-0 flex-1 items-center gap-x-3 gap-y-1 cq-sm:flex-row cq-sm:flex-wrap cq-sm:items-center">
          <div className={cx(sectionCardTitleClass, titleClassName)}>
            {icon}
            {typeof title === 'string' || typeof title === 'number' ? (
              <span className="min-w-0 truncate">{title}</span>
            ) : (
              title
            )}
          </div>
          {description && (
            <div
              className={cx(
                'hidden min-w-0 flex-1 text-xs font-normal leading-5 text-kumo-subtle cq-sm:block cq-sm:basis-40 cq-sm:truncate',
                descriptionClassName
              )}
            >
              {description}
            </div>
          )}
        </div>
        {trailing.length > 0 && (
          <div
            className={cx(
              'ml-3 flex shrink-0 items-center justify-end gap-2 whitespace-nowrap cq-sm:ml-auto cq-sm:flex-wrap cq-sm:whitespace-normal [&>*]:shrink-0',
              actionsClassName
            )}
          >
            {trailing}
          </div>
        )}
      </LayerCard.Secondary>
      <LayerCard.Primary
        className={cx('gap-0', cardPaddingClass[bodyPadding] || cardPaddingClass.md, bodyClassName)}
      >
        {children}
      </LayerCard.Primary>
    </LayerCard>
  );
}

export function ChartWarmupSkeleton({ height = 120, bars = 5 }) {
  return (
    <div
      aria-hidden="true"
      className="flex flex-col justify-end gap-2 overflow-hidden rounded-md border border-kumo-line/70 bg-kumo-recessed/35 p-3"
      style={{ height }}
    >
      <SkeletonLine className="h-3 w-1/3" />
      <SkeletonLine className="h-14 w-full rounded" />
      <div
        className="grid gap-2"
        style={{ gridTemplateColumns: `repeat(${bars}, minmax(0, 1fr))` }}
      >
        {Array.from({ length: bars }).map((_, index) => (
          <SkeletonLine key={index} className="h-2 w-full" />
        ))}
      </div>
    </div>
  );
}
