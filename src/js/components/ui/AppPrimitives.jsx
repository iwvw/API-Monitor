import React, { useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Popover } from '@cloudflare/kumo';
import { Table } from '@cloudflare/kumo/components/table';
import { LayerCard } from '@cloudflare/kumo';
import { Info } from '../IconsCore.jsx';
import { Menu, Search } from '../Icons.jsx';
import { allocateColumnWidths, resolveTableColumns } from '../../modules/tableLayout.js';
// 轻量卡片/骨架原语已拆到 CardPrimitives:公开页只需要这一小份,若不拆会连带
// 拉取本模块的 Table/Popover/Select 与全量图标库(Icons.jsx)。这里 import 供本
// 模块内部复用,并在下方 re-export,保持既有导入路径不变。
import { cx, cardPaddingClass } from './CardPrimitives.jsx';

export { cx };
export {
  actionIconClass,
  iconButtonIconClass,
  sectionCardHeaderClass,
  sectionCardTitleClass,
  SectionCard,
  ChartWarmupSkeleton,
} from './CardPrimitives.jsx';

export const pageStackClass = 'flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4 pb-4 cq-sm:pb-8';
export const viewportPageStackClass = 'flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4 pb-0';
export const pageToolbarClass =
  'flex min-w-0 flex-col items-stretch gap-3 border-b border-kumo-line pb-3 cq-sm:flex-row cq-sm:flex-wrap cq-sm:items-center cq-sm:justify-between [&>*]:min-w-0';
export const stickyTabsBaseClass =
  'sticky top-0 z-30 flex min-h-(--app-header-height) items-center bg-[var(--app-main-surface)] px-[var(--app-tab-gutter-x)] -mx-[var(--app-canvas-gutter-x)]';
// 顶栏(面包屑)自身吸顶的页面用这个:Tab 行吸在顶栏下沿,而不是压在面包屑上。
export const stickyTabsBelowHeaderClass =
  'sticky top-(--app-header-height) z-20 flex min-h-(--app-header-height) items-center bg-[var(--app-main-surface)] px-[var(--app-tab-gutter-x)] -mx-[var(--app-canvas-gutter-x)]';
const tableDensityClass = {
  comfortable: '',
  compact: '[&_td]:!px-3 [&_td]:!py-2 [&_th]:!px-3 [&_th]:!py-2',
  dense: '[&_td]:!px-2 [&_td]:!py-1.5 [&_th]:!px-2 [&_th]:!py-1.5',
};

const pillToneClass = {
  neutral: 'bg-kumo-recessed text-kumo-subtle border-kumo-line',
  brand: 'bg-brand/10 text-brand border-brand/20',
  info: 'bg-kumo-info/10 text-kumo-info border-kumo-info/20',
  success: 'bg-kumo-success/10 text-kumo-success border-kumo-success/20',
  warning: 'bg-kumo-warning/10 text-kumo-warning border-kumo-warning/20',
  danger: 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20',
};

export function TabBarOverflowActions({
  items = [],
  className = '',
  buttonClassName = '',
  menuClassName = 'min-w-56 max-w-80',
}) {
  const wideActions = (
    <div
      className={cx(
        'hidden min-w-0 shrink-0 flex-wrap items-center justify-end gap-2 cq-md:flex',
        className
      )}
    >
      {items.map(item => {
        if (item.type === 'select') {
          return (
            <Select alignItemWithTrigger
              key={item.key}
              size="sm"
              aria-label={item.label}
              value={item.value}
              onValueChange={item.onValueChange}
              disabled={item.disabled}
              items={item.options}
              className={cx('min-w-0 flex-1 max-w-56', item.selectClassName)}
            />
          );
        }
        return (
          <Button
            key={item.key}
            size="sm"
            variant={item.variant || 'secondary'}
            icon={item.icon}
            onClick={item.onClick}
            disabled={item.disabled}
            loading={item.loading}
            title={item.title || item.label}
            className={item.buttonClassName}
          >
            {item.label}
          </Button>
        );
      })}
    </div>
  );

  const renderPopupItem = item => {
    if (item.type === 'select') {
      return (
        <div key={item.key} className="flex flex-col gap-1.5">
          <span className="flex items-center gap-2 px-1 text-xs font-medium text-kumo-subtle">
            {item.icon}
            {item.label}
          </span>
          <Select alignItemWithTrigger
            size="sm"
            aria-label={item.label}
            value={item.value}
            onValueChange={item.onValueChange}
            disabled={item.disabled}
            items={item.options}
            className="w-full"
          />
        </div>
      );
    }
    return (
      <Button
        key={item.key}
        size="sm"
        variant={item.danger ? 'destructive' : item.variant || 'secondary'}
        icon={item.icon}
        onClick={item.onClick}
        disabled={item.disabled}
        loading={item.loading}
        title={item.title || item.label}
        className="w-full justify-start"
      >
        {item.label}
      </Button>
    );
  };

  if (items.length === 0) return null;

  return (
    <>
      {wideActions}
      <div className="shrink-0 cq-md:hidden">
        <Popover>
          <Popover.Trigger
            render={
              <Button
                size="sm"
                shape="square"
                variant="secondary"
                icon={<Menu className="h-4 w-4" />}
                aria-label="更多操作"
                title="更多操作"
                className={cx('h-8 w-8 shrink-0 !rounded-lg', buttonClassName)}
              />
            }
          />
          <Popover.Content side="bottom" align="end" className={`${menuClassName} p-2`}>
            <div className="flex flex-col gap-1">
              {items.map(renderPopupItem)}
            </div>
          </Popover.Content>
        </Popover>
      </div>
    </>
  );
}

export function ResponsiveSearchInput({
  value,
  onChange,
  onSearch,
  placeholder = '搜索...',
  ariaLabel = '搜索',
  className = '',
  inputClassName = '',
}) {
  const wideInput = (
    <div className={cx('relative', inputClassName)}>
      <span className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-kumo-subtle">
        <Search className="w-3.5 h-3.5" />
      </span>
      <Input
        size="sm"
        type="text"
        aria-label={ariaLabel}
        placeholder={placeholder}
        value={value}
        onChange={onChange}
        onKeyDown={onSearch ? (e) => {
          if (e.key === 'Enter') onSearch();
        } : undefined}
        className="w-full pl-8"
      />
    </div>
  );

  return (
    <>
      <div className={cx('hidden cq-md:block', className)}>{wideInput}</div>
      <div className="cq-md:hidden">
        <Popover>
          <Popover.Trigger
            render={
              <Button
                size="sm"
                shape="square"
                variant="secondary"
                icon={<Search className="h-4 w-4" />}
                aria-label={ariaLabel}
                title={ariaLabel}
                className="shrink-0"
              />
            }
          />
          <Popover.Content side="bottom" align="end" className="w-64">
            <div className="flex flex-col gap-2">
              <div className="relative">
                <span className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-kumo-subtle">
                  <Search className="w-3.5 h-3.5" />
                </span>
                <Input
                  size="sm"
                  type="text"
                  aria-label={ariaLabel}
                  placeholder={placeholder}
                  value={value}
                  onChange={onChange}
                  onKeyDown={onSearch ? (e) => {
                    if (e.key === 'Enter') onSearch();
                  } : undefined}
                  className="w-full pl-8"
                  autoFocus
                />
              </div>
              {onSearch && (
                <Button size="sm" variant="secondary" onClick={onSearch}>搜索</Button>
              )}
            </div>
          </Popover.Content>
        </Popover>
      </div>
    </>
  );
}

export function PageStack({ className = '', children, viewport = false }) {
  return <div className={cx(viewport ? viewportPageStackClass : pageStackClass, className)}>{children}</div>;
}

export function PageToolbar({ className = '', children }) {
  return <div className={cx(pageToolbarClass, className)}>{children}</div>;
}

export function FieldRow({ title, description, children }) {
  return (
    <div className="flex min-w-0 items-center gap-3 border-b border-kumo-line px-4 py-3 last:border-b-0 cq-tight:grid cq-tight:grid-cols-[minmax(0,1fr)_max-content] cq-tight:items-center">
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-semibold text-kumo-strong">{title}</div>
        {description && <div className="mt-1 hidden truncate text-xs leading-relaxed text-kumo-subtle cq-tight:block">{description}</div>}
      </div>
      <div className="flex min-w-0 max-w-[20rem] shrink-0 items-center justify-end gap-2 [&>*]:min-w-0">{children}</div>
    </div>
  );
}

export function AppCard({
  className = '',
  padding = 'md',
  interactive = false,
  children,
  ...props
}) {
  return (
    <LayerCard
      {...props}
      className={cx(
        'rounded-lg border border-kumo-line bg-kumo-base shadow-none ring-0',
        cardPaddingClass[padding] || cardPaddingClass.md,
        interactive && 'hover:border-brand/60',
        className
      )}
    >
      {children}
    </LayerCard>
  );
}

const insetToneClass = {
  recessed: 'border-kumo-line/80 bg-kumo-recessed/20',
  surface: 'border-kumo-line/70 bg-kumo-surface',
  dashed: 'border-dashed border-kumo-line/70 bg-transparent',
};

const keyValueGridColumnsClass = {
  1: 'grid-cols-1',
  2: 'cq-md:grid-cols-2',
  3: 'cq-md:grid-cols-3',
};

export function InsetPanel({
  tone = 'recessed',
  className = '',
  bodyClassName = '',
  padding = 'md',
  children,
  ...props
}) {
  return (
    <LayerCard
      {...props}
      className={cx(
        'overflow-hidden rounded-lg border shadow-none',
        insetToneClass[tone] || insetToneClass.recessed,
        className
      )}
    >
      <LayerCard.Primary
        className={cx(cardPaddingClass[padding] || cardPaddingClass.md, bodyClassName)}
      >
        {children}
      </LayerCard.Primary>
    </LayerCard>
  );
}

export function DataTableFrame({
  className = '',
  density = 'compact',
  variant = 'card',
  children,
  ...props
}) {
  const frameClassName = cx(
    'overflow-x-auto overscroll-x-contain touch-pan-x scrollbar-thin',
    tableDensityClass[density],
    className
  );

  if (variant === 'embedded') {
    return (
      <div {...props} className={frameClassName}>
        {children}
      </div>
    );
  }

  return (
    <AppCard {...props} padding="none" className={frameClassName}>
      {children}
    </AppCard>
  );
}

export function AppTable({
  columns,
  widths,
  columnWidths,
  fitContent = false,
  tableId,
  layout,
  className = '',
  style,
  children,
  ...props
}) {
  const columnWeights = Array.isArray(widths)
    ? widths.map((width) => Math.max(Number(width) || 0, 0))
    : [];
  const totalWeight = columnWeights.reduce((total, width) => total + width, 0);
  // columnWidths：外部托管的逐列像素覆盖（如 useTableResize 的拖拽调宽）。
  // 与语义 columns 配合：有覆盖时按覆盖值渲染，未提供的位置回退到角色分配。
  const overrideWidths = Array.isArray(columnWidths) ? columnWidths : null;
  const semanticLayout = React.useMemo(
    () => resolveTableColumns(Array.isArray(columns) ? columns : []),
    [columns]
  );
  const hasSemanticColumns = semanticLayout.columns.length > 0;
  const hasExplicitColgroup = React.Children.toArray(children).some(
    (child) => React.isValidElement(child) && child.type === 'colgroup'
  );
  const layoutWarnings = React.useMemo(() => {
    const warnings = [...semanticLayout.warnings];
    if (hasSemanticColumns && hasExplicitColgroup) {
      warnings.push({ code: 'semantic-columns-with-explicit-colgroup' });
    }
    // 覆盖宽度必须与语义列数一致：短于列数时多出的列既无覆盖宽度、又因
    // overrideWidths 存在而禁用容器分配，会静默退化为浏览器均分。
    if (overrideWidths && hasSemanticColumns && overrideWidths.length !== semanticLayout.columns.length) {
      warnings.push({
        code: 'column-widths-length-mismatch',
        expected: semanticLayout.columns.length,
        actual: overrideWidths.length,
      });
    }
    return warnings;
  }, [hasExplicitColgroup, hasSemanticColumns, semanticLayout.warnings, semanticLayout.columns.length, overrideWidths]);
  const warningKey = JSON.stringify(layoutWarnings);

  React.useEffect(() => {
    if (!import.meta.env.DEV || layoutWarnings.length === 0) return;
    console.warn(`[AppTable${tableId ? `:${tableId}` : ''}] layout warnings`, layoutWarnings);
  }, [layoutWarnings, tableId, warningKey]);

  // 动态列宽：table-layout: fixed 下 <col> 不支持「(100% - 固定列) × 权重」这类
  // 相对计算（浏览器会忽略），因此弹性列必须由 JS 按容器实测宽度算出像素。
  // 固定列保持角色像素不随容器伸缩；弹性列按 grow 权重分剩余空间。
  const tableRef = React.useRef(null);
  const [allocatedWidths, setAllocatedWidths] = React.useState(null);
  // 有外部覆盖宽度（拖拽调宽）时不再用容器分配，避免与用户手动宽度冲突。
  const canAllocate = hasSemanticColumns && !hasExplicitColgroup && !overrideWidths;

  React.useEffect(() => {
    if (!canAllocate) return undefined;
    const node = tableRef.current;
    if (!node || typeof ResizeObserver !== 'function') return undefined;

    let frame = 0;
    const measure = () => {
      // 用父容器宽度（表格自身 width:100%，直接量它会形成反馈循环）。
      const parent = node.parentElement;
      const available = parent ? parent.clientWidth : node.clientWidth;
      setAllocatedWidths(current => {
        const next = allocateColumnWidths(semanticLayout, available);
        if (!next) return null;
        if (current && current.length === next.length && current.every((v, i) => v === next[i])) {
          return current;
        }
        return next;
      });
    };
    const schedule = () => {
      window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(measure);
    };

    schedule();
    const observer = new ResizeObserver(schedule);
    if (node.parentElement) observer.observe(node.parentElement);
    observer.observe(node);
    return () => {
      window.cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, [canAllocate, semanticLayout]);

  // 有拖拽宽度覆盖时，表格的 min-width 必须等于覆盖列宽之和：否则会沿用角色
  // 推导出的更大 minWidth，把列拉伸到超出拖拽设定，内容横向溢出侵入相邻列。
  const overrideTotal = overrideWidths
    ? overrideWidths.reduce((total, width) => total + Math.max(Number(width) || 0, 0), 0)
    : 0;
  const semanticMinWidth = overrideWidths ? overrideTotal : semanticLayout.minWidth;
  const semanticStyle = hasSemanticColumns
    ? {
        minWidth: semanticMinWidth,
        width: fitContent ? semanticMinWidth : '100%',
      }
    : undefined;
  const legacyStyle = totalWeight > 0
    ? { minWidth: totalWeight, width: fitContent ? totalWeight : '100%' }
    : undefined;
  const shouldRenderGeneratedColgroup = !hasExplicitColgroup && (hasSemanticColumns || totalWeight > 0);

  return (
    <Table
      {...props}
      ref={tableRef}
      layout={layout || (hasSemanticColumns || totalWeight > 0 ? 'fixed' : undefined)}
      {...semanticLayout.dataAttributes}
      data-app-table-id={tableId}
      className={cx(
        hasSemanticColumns && 'app-semantic-table',
        className
      )}
      style={{
        ...(semanticStyle || legacyStyle),
        ...style,
      }}
    >
      {shouldRenderGeneratedColgroup && (
        <colgroup>
          {hasSemanticColumns
            ? semanticLayout.columns.map((column, index) => {
                // 有分配结果时用像素；否则回退到「固定列像素 + 弹性列不设宽（浏览器均分）」。
                const allocated = overrideWidths
                  ? overrideWidths[index]
                  : (allocatedWidths ? allocatedWidths[index] : null);
                const width = allocated ?? column.width;
                return (
                  <col
                    key={column.id}
                    data-column-id={column.id}
                    data-column-role={column.role}
                    style={width === null ? undefined : { width }}
                  />
                );
              })
            : columnWeights.map((width, index) => (
                <col
                  key={index}
                  style={{ width }}
                />
              ))}
        </colgroup>
      )}
      {children}
    </Table>
  );
}

export function ScrollableTable({
  widths,
  wrapperClassName = 'min-w-0 max-w-full overflow-x-auto scrollbar-thin',
  wrapperRef,
  ...props
}) {
  return (
    <div className={wrapperClassName} ref={wrapperRef}>
      <AppTable widths={widths} {...props} />
    </div>
  );
}

export function StatusBadge({ tone = 'neutral', children, className = '', ...props }) {
  const variant =
    {
      neutral: 'secondary',
      brand: 'info',
      info: 'info',
      success: 'success',
      warning: 'warning',
      danger: 'error',
      error: 'error',
    }[tone] || 'secondary';

  return (
    <Badge {...props} variant={variant} className={cx('shrink-0', className)}>
      {children}
    </Badge>
  );
}

export function getStatusPillClass(tone = 'neutral', { border = true } = {}) {
  return cx(border && 'border', pillToneClass[tone] || pillToneClass.neutral);
}

export function getHttpStatusPillClass(code, options) {
  const statusCode = Number(code);
  if (!Number.isFinite(statusCode)) return getStatusPillClass('neutral', options);
  if (statusCode >= 200 && statusCode < 300) return getStatusPillClass('success', options);
  if (statusCode === 429 || statusCode >= 400) return getStatusPillClass('danger', options);
  return getStatusPillClass('neutral', options);
}

export function EmptyState({
  icon: Icon = Info,
  title,
  description,
  action,
  className = '',
  card = true,
}) {
  const content = (
    <div
      className={cx(
        'flex min-h-44 flex-col items-center justify-center p-6 text-center',
        className
      )}
    >
      <Icon className="mb-3 h-8 w-8 text-kumo-subtle" />
      <div className="text-sm font-semibold text-kumo-strong">{title}</div>
      {description && (
        <div className="mt-1 max-w-sm text-xs leading-relaxed text-kumo-subtle">{description}</div>
      )}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );

  if (!card) return content;
  return <AppCard padding="none">{content}</AppCard>;
}

export function KeyValueGrid({
  items,
  columns = 2,
  className = '',
  itemClassName = '',
  labelClassName = '',
  valueClassName = '',
}) {
  return (
    <div
      className={cx(
        'grid gap-3 text-sm',
        keyValueGridColumnsClass[columns] || keyValueGridColumnsClass[2],
        className
      )}
    >
      {items.map(item => (
        <div key={item.key || item.label} className={cx('min-w-0', itemClassName, item.className)}>
          <div className={cx('text-xs text-kumo-subtle', labelClassName, item.labelClassName)}>
            {item.label}
          </div>
          <div className={cx('mt-1 min-w-0 text-kumo-strong', valueClassName, item.valueClassName)}>
            {item.value}
          </div>
        </div>
      ))}
    </div>
  );
}

export function ChartBoundaryBox({ className = '', children }) {
  const [boundary, setBoundary] = useState(null);
  return (
    <div ref={setBoundary} className={className}>
      {typeof children === 'function' ? children(boundary) : children}
    </div>
  );
}

export function ChartCard({ className = '', children }) {
  const [boundary, setBoundary] = useState(null);
  return (
    <LayerCard
      className={cx(
        'min-w-0 overflow-hidden rounded-lg border border-kumo-line/90 bg-kumo-base p-3 shadow-none',
        className
      )}
    >
      <div ref={setBoundary} className="flex h-full min-w-0 flex-col">
        {typeof children === 'function' ? children(boundary) : children}
      </div>
    </LayerCard>
  );
}
