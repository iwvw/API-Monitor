import { LayerCard, Table } from '@cloudflare/kumo';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { AppTable, EmptyState, cx } from '../../components/ui/AppPrimitives.jsx';
import { Box, RefreshCw } from '../../components/Icons.jsx';
import { SERVER_SECTION_HEADER_CLASS } from './constants.js';

const MANAGE_SHELL_CLASS = 'flex min-w-0 flex-col gap-3 cq-xl:sticky cq-xl:top-0 cq-xl:self-start';
const PANE_SHELL_CLASS = 'flex min-w-0 flex-col gap-3 cq-xl:order-2 cq-xl:min-h-0';
const CARD_CLASS =
  'flex min-h-0 min-w-0 flex-col overflow-hidden bg-kumo-base p-0 shadow-none cq-xl:h-[calc(100dvh-184px)] cq-xl:min-h-64';
const STAT_TILE_CLASS = 'rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5';
const SCROLL_BODY_CLASS = 'min-h-0 min-w-0 flex-1 overflow-auto overscroll-contain scrollbar-thin';

export function DockerManageCard({
  hosts = [],
  statItems = [],
  headerActions = null,
  selectedServerId = '',
  onSelectServer,
  getHostCount = () => 0,
  getHostBadges = () => [],
  refreshing = false,
  onRefresh,
  footer = null,
}) {
  return (
    <div className={MANAGE_SHELL_CLASS}>
      <LayerCard className="overflow-hidden bg-kumo-base p-0">
        <LayerCard.Secondary className={SERVER_SECTION_HEADER_CLASS}>
          <span className="inline-flex min-w-0 items-center gap-2 text-xs font-semibold text-kumo-strong">
            <Box className="h-4 w-4 shrink-0 text-brand" />
            管理
          </span>
          <span className="flex shrink-0 items-center gap-1.5">
            <Badge variant="neutral">{hosts.length} 主机</Badge>
            {onRefresh && (
              <Button
                shape="square"
                size="sm"
                variant="secondary"
                icon={<RefreshCw className={cx('h-3.5 w-3.5', refreshing && 'animate-spin')} />}
                disabled={refreshing}
                onClick={onRefresh}
                aria-label="刷新 Docker 数据"
                title="刷新 Docker 数据"
              />
            )}
            {headerActions}
          </span>
        </LayerCard.Secondary>
        <LayerCard.Primary className="space-y-3 p-3">
          <div className="grid grid-cols-4 gap-1.5">
            {statItems.map(item => (
              <div key={item.label} className={STAT_TILE_CLASS}>
                <div className="text-[10px] text-kumo-subtle">{item.label}</div>
                <div
                  className={cx(
                    'mt-0.5 truncate text-sm leading-normal font-semibold',
                    item.className || 'text-kumo-strong'
                  )}
                >
                  {item.value}
                </div>
              </div>
            ))}
          </div>

          <div className="flex flex-col gap-2">
            {hosts.map(server => {
              const selected = String(selectedServerId || '') === String(server.id);
              return (
                <div
                  key={server.id}
                  className={`overflow-hidden rounded-md border ${selected ? 'border-brand/55 bg-brand/5' : 'border-kumo-line/80 bg-kumo-base'}`}
                >
                  <div
                    role="button"
                    tabIndex={0}
                    onClick={() => onSelectServer?.(String(server.id))}
                    onKeyDown={event => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault();
                        onSelectServer?.(String(server.id));
                      }
                    }}
                    className="flex min-h-10 cursor-pointer items-center justify-between gap-2 px-2.5 py-2"
                  >
                    <span className="flex min-w-0 items-center gap-2">
                      <Box className="h-3.5 w-3.5 shrink-0 text-brand" />
                      <span
                        className="min-w-0 truncate text-xs leading-normal font-semibold text-kumo-strong"
                        title={server.name}
                      >
                        {server.name}
                      </span>
                    </span>
                    <span className="flex shrink-0 items-center gap-1.5">
                      <Badge variant="neutral">{getHostCount(server)}</Badge>
                      {getHostBadges(server).map(badge => (
                        <Badge
                          key={badge.label}
                          variant={badge.variant || 'neutral'}
                          appearance={badge.appearance}
                        >
                          {badge.label}
                        </Badge>
                      ))}
                    </span>
                  </div>
                </div>
              );
            })}
          </div>
        </LayerCard.Primary>
      </LayerCard>
      {footer}
    </div>
  );
}

export function DockerResourceTableCard({
  title,
  icon,
  badges = [],
  actions = null,
  headers = [],
  columns,
  layout,
  tableId,
  isEmpty = false,
  emptyTitle = '暂无数据',
  emptyDescription = '',
  emptyIcon = Box,
  children,
}) {
  return (
    <div className={PANE_SHELL_CLASS}>
      <LayerCard className={CARD_CLASS}>
        <LayerCard.Secondary className={SERVER_SECTION_HEADER_CLASS}>
          <span className="inline-flex min-w-0 items-center gap-2 text-xs font-semibold text-kumo-strong">
            {icon}
            <span className="min-w-0 truncate" title={title}>
              {title}
            </span>
            {badges.map(badge => (
              <Badge
                key={badge.label}
                variant={badge.variant || 'neutral'}
                appearance={badge.appearance}
              >
                {badge.label}
              </Badge>
            ))}
          </span>
          <span className="flex shrink-0 items-center gap-1.5">{actions}</span>
        </LayerCard.Secondary>

        <LayerCard.Primary className="flex min-h-0 min-w-0 flex-1 flex-col gap-0 overflow-hidden p-0">
          <div className={SCROLL_BODY_CLASS}>
            {isEmpty ? (
              <div className="p-4">
                <EmptyState icon={emptyIcon} title={emptyTitle} description={emptyDescription} />
              </div>
            ) : (
              <AppTable tableId={tableId} columns={columns} layout={layout} className="w-full text-xs">
                <Table.Header sticky variant="compact">
                  <Table.Row className="h-8">
                    {headers.map(header => (
                      <Table.Head
                        key={header.label}
                        className={cx(
                          '!py-1.5',
                          header.primary ? '!px-2.5' : '!px-2',
                          header.align === 'center' && 'text-center'
                        )}
                        title={header.title}
                      >
                        {header.srOnly ? (
                          <span className="sr-only">{header.label}</span>
                        ) : (
                          header.label
                        )}
                      </Table.Head>
                    ))}
                  </Table.Row>
                </Table.Header>
                <Table.Body>{children}</Table.Body>
              </AppTable>
            )}
          </div>
        </LayerCard.Primary>
      </LayerCard>
    </div>
  );
}