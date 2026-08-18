import React, { useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { DropdownMenu, Popover } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Table } from '@cloudflare/kumo/components/table';
import { LayerCard } from '@cloudflare/kumo';
import { Info } from '../IconsCore.jsx';
import { MoreVertical, Search } from '../Icons.jsx';
import { resolveTableColumns } from '../../modules/tableLayout.js';

export const pageStackClass = 'flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4 pb-4 cq-sm:pb-8';
export const viewportPageStackClass = 'flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4 pb-0';
export const pageToolbarClass =
  'flex min-w-0 flex-col items-stretch gap-3 border-b border-kumo-line pb-3 cq-sm:flex-row cq-sm:flex-wrap cq-sm:items-center cq-sm:justify-between [&>*]:min-w-0';
export const stickyTabsBaseClass =
  'sticky top-0 z-30 flex min-h-(--app-header-height) items-center bg-[var(--app-main-surface)] px-[var(--app-tab-gutter-x)] -mx-[var(--app-canvas-gutter-x)]';
export const sectionCardHeaderClass =
  'flex min-h-[52px] items-center justify-between gap-3 border-b border-kumo-line bg-kumo-elevated px-4 py-2.5 cq-sm:min-h-[56px] cq-sm:flex-row cq-sm:flex-wrap cq-sm:items-center cq-sm:py-3.5';
export const sectionCardTitleClass =
  'inline-flex min-w-0 max-w-full items-center gap-2 text-sm font-bold text-kumo-strong';
export const iconButtonIconClass = 'h-3.5 w-3.5';
export const actionIconClass = 'h-4 w-4';

const cardPaddingClass = {
  none: 'p-0',
  sm: 'p-3',
  md: 'p-4',
  lg: 'p-5',
  xl: 'p-6',
};

const tableDensityClass = {
  comfortable: '',
  compact: '[&_td]:!px-3 [&_td]:!py-2 [&_th]:!px-3 [&_th]:!py-2',
  dense: '[&_td]:!px-2 [&_td]:!py-1.5 [&_th]:!px-2 [&_th]:!py-1.5',
};

const pillToneClass = {
  neutral: 'bg-kumo-recessed text-kumo-subtle border-kumo-line',
  brand: 'bg-kumo-brand/10 text-kumo-brand border-kumo-brand/20',
  info: 'bg-kumo-info/10 text-kumo-info border-kumo-info/20',
  success: 'bg-kumo-success/10 text-kumo-success border-kumo-success/20',
  warning: 'bg-kumo-warning/10 text-kumo-warning border-kumo-warning/20',
  danger: 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20',
};

export function cx(...parts) {
  return parts.filter(Boolean).join(' ');
}

export function TabBarOverflowActions({
  items = [],
  className = '',
  buttonClassName = '',
  menuClassName = 'w-60',
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
            <Select
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

  const renderMenuItem = item => {
    if (item.type === 'select') {
      return (
        <DropdownMenu.Sub key={item.key}>
          <DropdownMenu.SubTrigger className="flex items-center gap-2 px-2 py-1.5 text-sm">
            {item.icon}
            <span className="min-w-0 flex-1 truncate">{item.label}</span>
            <span className="max-w-28 truncate text-xs text-kumo-subtle">
              {item.options.find(option => String(option.value) === String(item.value))?.label || ''}
            </span>
          </DropdownMenu.SubTrigger>
          <DropdownMenu.SubContent className="w-56">
            <DropdownMenu.RadioGroup
              value={item.value}
              onValueChange={item.onValueChange}
            >
              {item.options.map(option => (
                <DropdownMenu.RadioItem key={option.value} value={option.value}>
                  {option.label}
                </DropdownMenu.RadioItem>
              ))}
            </DropdownMenu.RadioGroup>
          </DropdownMenu.SubContent>
        </DropdownMenu.Sub>
      );
    }
    return (
      <DropdownMenu.Item
        key={item.key}
        icon={item.icon}
        onClick={item.onClick}
        disabled={item.disabled}
        variant={item.danger ? 'danger' : 'default'}
      >
        {item.label}
      </DropdownMenu.Item>
    );
  };

  const menuItems = items.map(renderMenuItem);

  if (items.length === 0) return null;

  return (
    <>
      {wideActions}
      <div className="shrink-0 cq-md:hidden">
        <DropdownMenu>
          <DropdownMenu.Trigger
            render={
              <Button
                size="sm"
                shape="square"
                variant="secondary"
                icon={<MoreVertical className="h-4 w-4" />}
                aria-label="更多操作"
                title="更多操作"
                className={cx('h-9 w-9 shrink-0 !rounded-lg', buttonClassName)}
              />
            }
          />
          <DropdownMenu.Content align="end" side="bottom" className={menuClassName}>
            {menuItems}
          </DropdownMenu.Content>
        </DropdownMenu>
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
        interactive && 'transition-colors hover:border-brand/60',
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
    return warnings;
  }, [hasExplicitColgroup, hasSemanticColumns, semanticLayout.warnings]);
  const warningKey = JSON.stringify(layoutWarnings);

  React.useEffect(() => {
    if (!import.meta.env.DEV || layoutWarnings.length === 0) return;
    console.warn(`[AppTable${tableId ? `:${tableId}` : ''}] layout warnings`, layoutWarnings);
  }, [layoutWarnings, tableId, warningKey]);

  const semanticStyle = hasSemanticColumns
    ? {
        minWidth: semanticLayout.minWidth,
        width: fitContent ? semanticLayout.minWidth : '100%',
      }
    : undefined;
  const legacyStyle = totalWeight > 0
    ? { minWidth: totalWeight, width: fitContent ? totalWeight : '100%' }
    : undefined;
  const shouldRenderGeneratedColgroup = !hasExplicitColgroup && (hasSemanticColumns || totalWeight > 0);

  return (
    <Table
      {...props}
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
            ? semanticLayout.columns.map((column) => (
                <col
                  key={column.id}
                  data-column-id={column.id}
                  data-column-role={column.role}
                  style={column.width === null ? undefined : { width: column.width }}
                />
              ))
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
  ...props
}) {
  return (
    <div className={wrapperClassName}>
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
