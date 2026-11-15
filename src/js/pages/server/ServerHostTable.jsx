import React from 'react';
import * as echarts from 'echarts/core';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { ContextMenu } from '@cloudflare/kumo/primitives/context-menu';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { ChartLegend, Loader, Tabs } from '@cloudflare/kumo';
import { AppCard, ChartWarmupSkeleton, ResponsiveSearchInput, ScrollableTable } from '../../components/ui/AppPrimitives.jsx';
import AnimatedCollapse, { DeferredRender } from '../../components/AnimatedCollapse.jsx';
import CountryFlag from '../../components/CountryFlag.jsx';
import SiteFontTimeseriesChart from '../../components/SiteFontTimeseriesChart.jsx';
import ServerLocationMap from '../../components/server/ServerLocationMap.jsx';
import TileGrid from '../tiles/TileGrid.jsx';
import { ChevronDown, ChevronUp, DragHandle, Globe, LayoutDashboard, Menu, Pause, Play, RefreshCw, RotateCw, Search, Upload } from '../../components/Icons.jsx';
import { CompactExpandedRow } from './CompactExpandedRow.jsx';
import { CompactMetricBar } from './CompactMetricBar.jsx';
import { DenseDetailChip } from './DenseDetailChip.jsx';
import { DenseLifecycleMeter } from './DenseLifecycleMeter.jsx';
import { DenseTrafficCell } from './DenseTrafficCell.jsx';
import { DenseUsageMeter } from './DenseUsageMeter.jsx';
import { ExpandedInfoChip } from './ExpandedInfoChip.jsx';
import { ExpandedProgressMetric } from './ExpandedProgressMetric.jsx';
import { ExpandedSection } from './ExpandedSection.jsx';
import { ExpandedStatTile } from './ExpandedStatTile.jsx';
import { ExpandedTrendChartCard } from './ExpandedTrendChartCard.jsx';
import { NetworkQualityPanel } from './NetworkQualityPanel.jsx';
import { ServerConnectionActions } from './ServerConnectionActions.jsx';
import { TrafficTotalSummary } from './TrafficTotalSummary.jsx';
import { TrendSeriesLabel } from './TrendSeriesLabel.jsx';
import { canOpenRemoteDesktop } from '../../modules/remoteDesktop.js';
import { canOpenTerminal, hasSshEndpoint, resolveTerminalProtocol } from '../../modules/serverTerminal.js';
import { formatDateTime, parseSpeed } from '../../modules/utils.js';
import { summarizeDockerContainers } from '../../modules/dockerSummary.js';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { buildNetworkQualitySeries, formatUptimeDaysOnly, getGpuModelText, getHostAddress, getPrimaryLoadValue, getServerCycleParts, getServerLifecycle, getServerMetricDisplay, getServerMonitorModeLabel, getTrafficQuota, isNetworkQualityUnsupportedError, isServerOnline } from './serverHelpers.js';
import { clampPercent, formatBytesSpeed, formatCompactBytesSpeed, formatMetricTooltipValue, getCpuTemp, getExpandedCardSpanClassName, getExpandedInfoGridClassName, getExpandedTrendGridClassName, getFlagCountry, getGpuTemp, getLatestMetricValue, getOSIconClass, getServerCardRowHeight, getServerLocationText, getServerLocationTitle, getServerTagBadgeVariant, toNumber } from './utils.js';
import { COMPACT_ACTION_BUTTON_CLASS, COMPACT_INLINE_BOX_CLASS, HOST_COMPACT_ADAPTIVE_COLUMNS, HOST_COMPACT_HEADER_BOX_CLASS, SERVER_CHART_RENDER_DEFER_MS, SERVER_FAST_CHART_UPDATE_BEHAVIOR, SERVER_SECONDARY_TABS_GROUP_CLASS } from './constants.js';

export default function ServerHostTable(props) {
  const {
    cardColumns,
    cardDetailServerId,
    chartSeriesReadyServers,
    checkDockerUpdatesForServer,
    compactExpandedChartHeight,
    compactExpandedYAxisTickCount,
    compactNetworkQualityChartHeight,
    expandedChartXAxisTickCount,
    expandedChartXAxisTickFormat,
    expandedChartYAxisTickCount,
    expandedDockerPanels,
    expandedNumberAxisTickFormat,
    expandedServers,
    expandedSpeedAxisTickFormat,
    expandedTrendChartHeight,
    fastTimeseriesEcharts,
    filteredServers,
    getDockerActionProgress,
    getDockerContainerId,
    getDockerContainerImage,
    getDockerContainerName,
    getDockerContainerSelectionKey,
    getDockerContainerState,
    getDockerContainerUpdateCheck,
    getDockerStateBadge,
    getDockerUpdateBadge,
    handleDockerContainerUpdate,
    handleServerCardLayoutChange,
    isCompactColumnVisible,
    isCompactViewport,
    isDenseViewport,
    isDockerActionPending,
    isDockerContainerUpdateChecking,
    isDockerUpdateConfirmActive,
    loadNetworkQuality,
    networkQualityByServer,
    networkQualityChartHeight,
    openCompactColumnMenu,
    openRemoteDesktop,
    openSSHTerminal,
    renderServerContextMenu,
    renderedCompactExpandedServers,
    resolveServerDisplayStatus,
    resolveServerMetricsHealth,
    serverCardDragEnabled,
    serverCardLayout,
    serverCurrentTab,
    serverIpDisplayMode,
    serverList,
    serverListViewMode,
    serverLoading,
    serverMapOpen,
    serverSearchText,
    serverStatusFilter,
    setCardDetailServerId,
    setExpandedServers,
    setServerCardDragEnabled,
    setServerListViewMode,
    setServerMapOpen,
    setServerSearchText,
    setServerStatusFilter,
    startRenameServer,
    staticTimeseriesEcharts,
    statsSummary,
    submitDockerTask,
    theme,
    toggleDockerPanel,
    toggleServerExpand,
    visibleCompactColumnDefs,
    visibleCompactColumnWidths,
  } = props;
  // 横向滚动位置感知：只在确实有内容被 sticky 列遮住时显示该侧渐变，避免未滚动时渐变遮住相邻列。
  // 首列（sticky-left）→ scrollLeft>0 才有内容滑到它下面；末列（sticky-right）→ 未滚到最右才有。
  const compactScrollRef = React.useRef(null);
  const [compactFadeSide, setCompactFadeSide] = React.useState({ left: false, right: false });
  React.useEffect(() => {
    const node = compactScrollRef.current;
    if (!node) return undefined;
    let raf = 0;
    const measure = () => {
      const max = node.scrollWidth - node.clientWidth;
      const left = max > 0 && node.scrollLeft > 0;
      const right = max > 0 && node.scrollLeft < max - 1;
      setCompactFadeSide(prev =>
        prev.left === left && prev.right === right ? prev : { left, right }
      );
    };
    const schedule = () => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(measure);
    };
    measure();
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(schedule) : null;
    if (ro) ro.observe(node);
    node.addEventListener('scroll', schedule, { passive: true });
    window.addEventListener('resize', schedule);
    return () => {
      cancelAnimationFrame(raf);
      if (ro) ro.disconnect();
      node.removeEventListener('scroll', schedule);
      window.removeEventListener('resize', schedule);
    };
  }, [filteredServers, serverListViewMode]);
  const leftFadeClass = compactFadeSide.left ? '' : 'before:opacity-0';
  const rightFadeClass = compactFadeSide.right ? '' : 'before:opacity-0';
  return (
    <>
      {serverCurrentTab === 'list' && (
        <>
          {/* 控制过滤器栏（通栏对齐主 tab） */}
          <div className="-mt-3 -mb-3 -mx-[var(--app-canvas-gutter-x)] flex min-h-[46px] flex-wrap items-center gap-2 px-[var(--app-tab-gutter-x)] py-2 cq-lg:justify-between cq-sm:-mt-4 cq-sm:-mb-4">
            <div className={SERVER_SECONDARY_TABS_GROUP_CLASS}>
              <Tabs
                {...TOOL_TABS_PROPS}
                className="w-auto cq-sm:w-fit"
                listClassName="w-auto cq-sm:w-fit"
                value={serverStatusFilter === 'offline' ? 'offline' : 'all'}
                onValueChange={setServerStatusFilter}
                tabs={[
                  { value: 'all', label: `全部 (${statsSummary.total})` },
                  { value: 'offline', label: `离线 (${statsSummary.offline})` },
                ]}
              />
              <Tabs
                {...TOOL_TABS_PROPS}
                className="w-auto cq-sm:w-fit"
                listClassName="w-auto cq-sm:w-fit"
                value={serverListViewMode}
                onValueChange={setServerListViewMode}
                tabs={[
                  {
                    value: 'compact',
                    label: (
                      <span title="表格视图" aria-label="表格视图">
                        <Menu className="h-3.5 w-3.5" />
                      </span>
                    ),
                  },
                  {
                    value: 'cards',
                    label: (
                      <span title="卡片视图" aria-label="卡片视图">
                        <LayoutDashboard className="h-3.5 w-3.5" />
                      </span>
                    ),
                  },
                ]}
              />
              <Button
                size="sm"
                shape="square"
                variant={serverMapOpen ? 'primary' : 'secondary'}
                icon={<Globe className="h-3.5 w-3.5" />}
                aria-label={serverMapOpen ? '切回主机列表' : '切换到主机地图'}
                title={serverMapOpen ? '切回主机列表' : '切换到主机地图'}
                onClick={() => setServerMapOpen(prev => !prev)}
              />
              {serverListViewMode === 'cards' && !serverMapOpen && (
                <Button
                  size="sm"
                  shape="square"
                  variant={serverCardDragEnabled ? 'primary' : 'secondary'}
                  icon={<DragHandle className="h-3.5 w-3.5" />}
                  aria-label={serverCardDragEnabled ? '完成排序' : '排序'}
                  aria-pressed={serverCardDragEnabled}
                  title={serverCardDragEnabled ? '完成排序' : '排序'}
                  onClick={() => setServerCardDragEnabled(prev => !prev)}
                />
              )}
            </div>

            <ResponsiveSearchInput
              value={serverSearchText}
              onChange={e => setServerSearchText(e.target.value)}
              placeholder="搜索主机名称、IP 或标签..."
              ariaLabel="搜索主机"
              className="cq-lg:w-72"
            />
          </div>

          <div
            className={
              serverListViewMode === 'cards' || serverMapOpen
                ? 'flex min-h-0 flex-1 flex-col gap-4'
                : 'flex flex-col gap-4'
            }
          >
            {/* 列表渲染 */}
            {serverMapOpen ? (
            serverLoading && filteredServers.length === 0 ? (
              <div className="min-h-0 w-full flex-1 overflow-hidden rounded-lg border border-kumo-line/70 bg-kumo-base">
                <SkeletonLine className="h-full w-full rounded-none" />
              </div>
            ) : (
              <div className="min-h-0 w-full flex-1">
                <ServerLocationMap
                  echarts={echarts}
                  servers={filteredServers}
                  resolveStatus={server => resolveServerDisplayStatus(server).state}
                  title="主机地图"
                  subtitle="当前筛选中的主机地理分布"
                  height="100%"
                  className="h-full min-h-0"
                />
              </div>
            )
          ) : serverLoading && serverList.length === 0 ? (
            <AppCard
              padding="none"
              className="flex flex-col items-center justify-center gap-2 p-12 text-kumo-subtle"
            >
              <Loader size={24} />
              <p className="text-xs">正在加载主机列表...</p>
            </AppCard>
          ) : filteredServers.length === 0 ? (
            <AppCard
              padding="none"
              className="flex flex-col items-center justify-center gap-1.5 p-16 text-kumo-subtle"
            >
              <span className="text-xl">🔍</span>
              <p className="text-xs">未找到符合当前条件的主机节点</p>
            </AppCard>
          ) : (
            <div
              className={
                serverListViewMode === 'cards'
                  ? 'flex min-h-0 flex-1 flex-col gap-3'
                  : 'flex flex-col gap-3'
              }
            >
              {serverListViewMode === 'compact' ? (
                <div className="w-full max-w-full overflow-hidden rounded-md border border-kumo-interact/60 bg-kumo-base [&_tr:last-child]:border-b-0">
                  <ScrollableTable
                    layout="fixed"
                    widths={visibleCompactColumnWidths}
                    wrapperRef={compactScrollRef}
                    className="text-xs [&_td]:border-kumo-interact/45 [&_th]:border-kumo-interact/50"
                    wrapperClassName="w-full max-w-full overflow-x-auto scrollbar-thin"
                  >
                    <colgroup>
                      {visibleCompactColumnDefs
                        .filter(column => column.id !== 'status')
                        .map((column, index) => (
                          <col
                            key={`${column.id}-${visibleCompactColumnWidths[index]}`}
                            style={
                              HOST_COMPACT_ADAPTIVE_COLUMNS.has(column.id)
                                ? undefined
                                : { width: visibleCompactColumnWidths[index] }
                            }
                          />
                        ))}
                    </colgroup>
                    <Table.Header sticky variant="compact">
                      <Table.Row onContextMenu={openCompactColumnMenu}>
                        {visibleCompactColumnDefs.filter(column => column.id !== 'status').map(column => (
                          <Table.Head
                            key={column.id}
                            sticky={column.id === 'actions' ? 'right' : column.id === 'name' ? 'left' : undefined}
                            className={`${column.id === 'name' ? `!pl-[10px] !z-[3] ${leftFadeClass}` : '!px-[2px]'} !py-2 text-center text-[10px] whitespace-nowrap ${column.id === 'actions' ? `!pl-[1px] !pr-[2px] !z-[3] ${rightFadeClass}` : ''}`}
                          >
                            <div
                              className={`${column.id === 'name' ? 'flex items-center justify-start' : `mx-auto flex items-center ${HOST_COMPACT_HEADER_BOX_CLASS[column.id] || 'justify-center'}`}`}
                            >
                              {column.id === 'name' ? null : column.label}
                            </div>
                          </Table.Head>
                        ))}
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {filteredServers.map(server => {
                        const country = getFlagCountry(server);
                        const locationText = getServerLocationText(server);
                        const locationTitle = getServerLocationTitle(server);
                        const isExpanded = expandedServers.includes(server.id);
                        const shouldRenderExpandedRow =
                          isExpanded || renderedCompactExpandedServers.includes(server.id);
                        const isChartSeriesReady = chartSeriesReadyServers.includes(server.id);
                        const isDarkMode = theme === 'dark';
                        const {
                          records,
                          cpuColor,
                          memColor,
                          cpuTempColor,
                          gpuColor,
                          vramColor,
                          powerColor,
                          gpuTempColor,
                          diskColor,
                          txColor,
                          rxColor,
                          cpuMemSeries,
                          gpuSeries,
                          netSeries,
                        } = getServerMetricDisplay(
                          server.id,
                          server.metricsCache,
                          isExpanded && isChartSeriesReady,
                          isDarkMode
                        );
                        const hasGpuData =
                          !!getGpuModelText(server.info?.gpu) ||
                          records.some(
                            r =>
                              (r.gpu_usage !== null &&
                                r.gpu_usage !== undefined &&
                                toNumber(r.gpu_usage, 0) > 0) ||
                              getGpuTemp(r) > 0
                          );
                        const tx = parseSpeed(server.info?.network?.tx_speed);
                        const rx = parseSpeed(server.info?.network?.rx_speed);
                        const cycleParts = getServerCycleParts(server);
                        const txTotal = cycleParts.tx;
                        const rxTotal = cycleParts.rx;
                        const trafficQuota = getTrafficQuota(server);
                        const disk = server.info?.disk?.[0] || {};
                        const diskUsage = clampPercent(toNumber(disk.usage, 0));
                        const memUsage = clampPercent(toNumber(server.info?.memory?.Usage, 0));
                        const cpuUsage = clampPercent(toNumber(server.info?.cpu?.Usage, 0));
                        const cpuTemp = toNumber(server.info?.cpu?.Temp, 0);
                        const terminalProtocol = resolveTerminalProtocol(server);
                        const effectiveTerminalProtocol =
                          terminalProtocol || (hasSshEndpoint(server) ? 'ssh' : null);
                        const terminalLabel =
                          effectiveTerminalProtocol === 'agent' ? 'Agent 隧道终端' : 'SSH 终端';
                        const chartLoading = !!server.metricsLoading && records.length === 0;
                        const physicalCores =
                          server.info?.cpu?.PhysicalCores || server.info?.cpu?.Cores;
                        const logicalCores = server.info?.cpu?.LogicalCores;
                        const coreText =
                          physicalCores && logicalCores && physicalCores !== logicalCores
                            ? `${physicalCores}核 / ${logicalCores}线程`
                            : `${physicalCores || '-'}核`;
                        const dockerSummary = summarizeDockerContainers(
                          server.info?.docker,
                          getDockerContainerState
                        );
                        const lifecycle = getServerLifecycle(server);
                        const networkQuality = networkQualityByServer[server.id] || {};
                        const networkQualitySeries = isExpanded
                          ? buildNetworkQualitySeries(networkQuality, isDarkMode)
                          : [];
                        const hasNetworkQualityData =
                          isExpanded && networkQualitySeries.some(series => series.data.length > 0);
                        const networkQualityUnsupported =
                          isExpanded &&
                          (!!networkQuality.unsupported ||
                            isNetworkQualityUnsupportedError(
                              networkQuality.error || networkQuality.unsupportedMessage
                            ));
                        const metricsHealth = resolveServerMetricsHealth(server);
                        const rowMuted = !isServerOnline(server);

                        return (
                          <React.Fragment key={server.id}>
                            <ContextMenu.Root>
                              <ContextMenu.Trigger
                                render={
                                  <Table.Row
                                    variant={isExpanded ? 'selected' : 'default'}
                                    className={`select-none border-b border-kumo-line/80 ${isExpanded ? '' : 'hover:bg-kumo-recessed/15'}`}
                                    onClick={() => toggleServerExpand(server.id)}
                                  >
                                    {isCompactColumnVisible('name') && (
                                      <Table.Cell
                                        sticky="left"
                                        className={`!pl-[10px] !py-1.5 !z-[2] whitespace-nowrap ${leftFadeClass}`}
                                      >
                                        <div
                                          className={`flex items-center gap-2 w-full justify-start ${rowMuted ? 'text-kumo-subtle' : ''}`}
                                        >
                                          {isCompactColumnVisible('status') && (
                                            <span
                                              className="inline-flex h-2 w-2 shrink-0 rounded-full"
                                              title={
                                                metricsHealth.stale
                                                  ? 'Agent 连接存在，但最近未收到有效指标上报'
                                                  : (metricsHealth.label || '')
                                              }
                                              style={{ alignItems: 'center' }}
                                            >
                                              <span className={`inline-block h-2 w-2 rounded-full ${metricsHealth.dotClassName}`} />
                                            </span>
                                          )}
                                          <i
                                            className={`${getOSIconClass(server.info?.platform)} ${rowMuted ? 'opacity-60 grayscale' : ''}`}
                                          ></i>
                                          <div className="min-w-0">
                                            <div
                                              className={`truncate font-semibold ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-strong'}`}
                                              title={server.name}
                                            >
                                              {server.name}
                                            </div>
                                            {/* {(server.tags || []).filter(t => t !== 'Agent').length > 0 && (
                                        <div className="flex min-w-0 gap-1">
                                          {(server.tags || []).filter(t => t !== 'Agent').slice(0, 2).map(tag => (
                                            <span key={tag} className="truncate rounded bg-kumo-recessed/60 px-1 text-[9px] font-semibold text-kumo-subtle" title={tag}>
                                              {tag}
                                            </span>
                                          ))}
                                        </div>
                                      )} */}
                                          </div>
                                        </div>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('country') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 text-center whitespace-nowrap">
                                        <div className="mx-auto flex items-center justify-center gap-1.5">
                                          {locationText ? (
                                            <>
                                              {country && (
                                                <CountryFlag
                                                  preferSvg
                                                  countryCode={country}
                                                  className={`h-3.5 w-5 shrink-0 !rounded-[2px] text-sm ${rowMuted ? 'opacity-60 grayscale' : ''}`}
                                                />
                                              )}
                                              <span
                                                className={`truncate max-w-full font-semibold uppercase ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-strong'}`}
                                                title={locationTitle}
                                              >
                                                {locationText}
                                              </span>
                                            </>
                                          ) : (
                                            <span className="font-semibold text-kumo-subtle">
                                              -
                                            </span>
                                          )}
                                        </div>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('uptime') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 text-center whitespace-nowrap">
                                        <span
                                          className={`inline-flex justify-center font-semibold tabular-nums ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-strong'}`}
                                        >
                                          {formatUptimeDaysOnly(
                                            server.info?.uptime || server.info?.system?.Uptime
                                          )}
                                        </span>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('load') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 text-center whitespace-nowrap">
                                        <code
                                          className={`rounded-md bg-kumo-recessed/50 px-2 py-1 font-mono text-xs font-semibold ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-strong'} ${COMPACT_INLINE_BOX_CLASS}`}
                                        >
                                          {getPrimaryLoadValue(server.info?.cpu?.Load)}
                                        </code>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('speed') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseTrafficCell
                                          left={rx.num}
                                          leftUnit={rx.unit}
                                          right={tx.num}
                                          rightUnit={tx.unit}
                                          leftTitle={server.info?.network?.rx_speed || '0 B/s'}
                                          rightTitle={server.info?.network?.tx_speed || '0 B/s'}
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('traffic') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseTrafficCell
                                          left={rxTotal.num}
                                          leftUnit={rxTotal.unit}
                                          right={txTotal.num}
                                          rightUnit={txTotal.unit}
                                          leftTitle={rxTotal.text}
                                          rightTitle={txTotal.text}
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('cpu') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseUsageMeter
                                          label="CPU"
                                          value={cpuUsage}
                                          detail={`${Math.round(cpuUsage)}%`}
                                          indicatorClassName="!bg-none !bg-kumo-success"
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('memory') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseUsageMeter
                                          label="内存"
                                          value={memUsage}
                                          detail={`${Math.round(memUsage)}%`}
                                          indicatorClassName="!bg-none !bg-kumo-info"
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('disk') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseUsageMeter
                                          label="硬盘"
                                          value={diskUsage}
                                          detail={`${Math.round(diskUsage)}%`}
                                          indicatorClassName="!bg-none !bg-kumo-warning"
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('remaining') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <div
                                          title={
                                            lifecycle.expiresAt
                                              ? `${formatDateTime(lifecycle.startsAt)} - ${formatDateTime(lifecycle.expiresAt)}，剩余 ${Math.round(lifecycle.remainingPercent)}%`
                                              : '永久'
                                          }
                                        >
                                          <DenseLifecycleMeter
                                            lifecycle={lifecycle}
                                            muted={rowMuted}
                                          />
                                        </div>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('quotaRemaining') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseUsageMeter
                                          label="余量"
                                          value={
                                            trafficQuota.unlimited
                                              ? 100
                                              : Math.max(0, 100 - trafficQuota.percent)
                                          }
                                          detail={
                                            trafficQuota.unlimited
                                              ? '无限'
                                              : trafficQuota.remainingText
                                          }
                                          indicatorClassName={
                                            trafficQuota.overLimit
                                              ? '!bg-none !bg-kumo-danger'
                                              : '!bg-none !bg-kumo-info'
                                          }
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('actions') && (
                                      <Table.Cell
                                        sticky="right"
                                        className={`!py-1.5 !pl-[1px] !pr-[2px] !z-[2] text-center whitespace-nowrap ${rightFadeClass}`}
                                      >
                                        <div
                                          className="flex items-center justify-center gap-1"
                                          onClick={event => event.stopPropagation()}
                                        >
                                          <ServerConnectionActions
                                            remoteDesktopAvailable={canOpenRemoteDesktop(server)}
                                            terminalLabel={terminalLabel}
                                            terminalDisabled={
                                              !canOpenTerminal(server) && !hasSshEndpoint(server)
                                            }
                                            onOpenRemoteDesktop={() => openRemoteDesktop(server)}
                                            onOpenTerminal={() => openSSHTerminal(server)}
                                            buttonClassName={COMPACT_ACTION_BUTTON_CLASS}
                                          />
                                        </div>
                                      </Table.Cell>
                                    )}
                                  </Table.Row>
                                }
                              />
                              {renderServerContextMenu(server)}
                            </ContextMenu.Root>

                            {shouldRenderExpandedRow && (
                              <CompactExpandedRow
                                open={isExpanded}
                                colSpan={visibleCompactColumnDefs.filter(column => column.id !== 'status').length}
                              >
                                <div
                                  className={`flex flex-col ${isDenseViewport ? 'gap-1.5 p-1.5' : 'gap-2 p-2'}`}
                                >
                                  {server.loading && !server.info ? (
                                    <div className="space-y-2 py-5">
                                      <SkeletonLine className="mx-auto h-4 w-1/3" />
                                      <SkeletonLine className="mx-auto h-4 w-1/2" />
                                    </div>
                                  ) : server.error ? (
                                    <div className="rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-xs font-semibold text-kumo-danger">
                                      {server.error}
                                    </div>
                                  ) : (
                                    <>
                                      <div className="grid grid-cols-1 gap-1.5 cq-sm:grid-cols-2 cq-lg:grid-cols-[repeat(auto-fit,minmax(154px,1fr))]">
                                        <DenseDetailChip label="核心" value={coreText} />
                                        <DenseDetailChip
                                          label="Agent 版本"
                                          value={server.info?.agentVersion || '未报告'}
                                          valueClassName="font-mono text-kumo-strong"
                                        />
                                        <DenseDetailChip
                                          label="内存"
                                          value={`${server.info?.memory?.Used || '-'} / ${server.info?.memory?.Total || '-'}`}
                                        />
                                        <DenseDetailChip
                                          label="连接"
                                          value={server.info?.network?.connections || 0}
                                        />
                                        <DenseDetailChip
                                          label="Docker"
                                          value={
                                            server.info?.docker?.installed
                                              ? `${dockerSummary.running}/${dockerSummary.total} 运行`
                                              : '未安装'
                                          }
                                        />
                                        <DenseDetailChip
                                          label="生命周期"
                                          value={
                                            lifecycle.expiresAt
                                              ? `${lifecycle.label} / ${Math.round(lifecycle.remainingPercent)}%`
                                              : '永久'
                                          }
                                          valueClassName={lifecycle.toneClass}
                                        />
                                        <DenseDetailChip
                                          label="模式"
                                          value={getServerMonitorModeLabel(server)}
                                        />
                                      </div>

                                      <div className="flex min-w-0 flex-col gap-2">
                                        <div
                                          className={getExpandedTrendGridClassName(
                                            true,
                                            isDenseViewport
                                          )}
                                        >
                                          <ExpandedTrendChartCard
                                            title="CPU / 内存趋势"
                                            tone="success"
                                            compact
                                            className={getExpandedCardSpanClassName(0, 3)}
                                            legend={
                                              <>
                                                <ChartLegend.SmallItem
                                                  name="CPU"
                                                  color={cpuColor}
                                                  value={`${Math.round(cpuUsage)}%`}
                                                  loading={chartLoading}
                                                />
                                                <ChartLegend.SmallItem
                                                  name="内存"
                                                  color={memColor}
                                                  value={`${Math.round(memUsage)}%`}
                                                  loading={chartLoading}
                                                />
                                                <ChartLegend.SmallItem
                                                  name="温度"
                                                  color={cpuTempColor}
                                                  value={getLatestMetricValue(
                                                    records,
                                                    getCpuTemp,
                                                    v => `${v.toFixed(1)}°C`
                                                  )}
                                                  loading={chartLoading}
                                                />
                                              </>
                                            }
                                          >
                                            {tooltipBoundary => (
                                              <DeferredRender
                                                open={isExpanded}
                                                delay={SERVER_CHART_RENDER_DEFER_MS}
                                                fallback={
                                                  <ChartWarmupSkeleton
                                                    height={compactExpandedChartHeight}
                                                  />
                                                }
                                              >
                                                <SiteFontTimeseriesChart
                                                  echarts={fastTimeseriesEcharts}
                                                  data={cpuMemSeries}
                                                  height={compactExpandedChartHeight}
                                                  isDarkMode={isDarkMode}
                                                  gradient
                                                  loading={chartLoading}
                                                  tooltipBoundary={tooltipBoundary ?? undefined}
                                                  xAxisTickCount={expandedChartXAxisTickCount}
                                                  yAxisTickCount={compactExpandedYAxisTickCount}
                                                  xAxisTickFormat={expandedChartXAxisTickFormat}
                                                  yAxisTickFormat={expandedNumberAxisTickFormat}
                                                  tooltipValueFormat={formatMetricTooltipValue}
                                                  optionUpdateBehavior={
                                                    SERVER_FAST_CHART_UPDATE_BEHAVIOR
                                                  }
                                                  ariaDescription={`${server.name} CPU 与内存使用趋势`}
                                                />
                                              </DeferredRender>
                                            )}
                                          </ExpandedTrendChartCard>

                                          <ExpandedTrendChartCard
                                            title={hasGpuData ? 'GPU 趋势' : '网络趋势'}
                                            tone={hasGpuData ? 'warning' : 'info'}
                                            compact
                                            className={getExpandedCardSpanClassName(
                                              1,
                                              hasGpuData ? 3 : 2
                                            )}
                                            legend={
                                              <>
                                                {hasGpuData && (
                                                  <TrendSeriesLabel name="GPU" color={gpuColor} />
                                                )}
                                                {hasGpuData && (
                                                  <TrendSeriesLabel name="显存" color={vramColor} />
                                                )}
                                                {hasGpuData && (
                                                  <TrendSeriesLabel
                                                    name="功耗"
                                                    color={powerColor}
                                                  />
                                                )}
                                                {hasGpuData && (
                                                  <TrendSeriesLabel
                                                    name="温度"
                                                    color={gpuTempColor}
                                                  />
                                                )}
                                                {!hasGpuData && (
                                                  <ChartLegend.SmallItem
                                                    name="上行"
                                                    color={txColor}
                                                    value={getLatestMetricValue(
                                                      records,
                                                      r => toNumber(r.net_tx, 0),
                                                      formatBytesSpeed
                                                    )}
                                                    loading={chartLoading}
                                                  />
                                                )}
                                                {!hasGpuData && (
                                                  <ChartLegend.SmallItem
                                                    name="下行"
                                                    color={rxColor}
                                                    value={getLatestMetricValue(
                                                      records,
                                                      r => toNumber(r.net_rx, 0),
                                                      formatBytesSpeed
                                                    )}
                                                    loading={chartLoading}
                                                  />
                                                )}
                                              </>
                                            }
                                          >
                                            {tooltipBoundary => (
                                              <div
                                                className={`grid min-w-0 gap-1.5 ${hasGpuData ? 'grid-cols-1' : 'cq-sm:grid-cols-[minmax(0,1fr)_8.5rem]'}`}
                                              >
                                                <DeferredRender
                                                  open={isExpanded}
                                                  delay={SERVER_CHART_RENDER_DEFER_MS}
                                                  fallback={
                                                    <ChartWarmupSkeleton
                                                      height={compactExpandedChartHeight}
                                                    />
                                                  }
                                                >
                                                  <SiteFontTimeseriesChart
                                                    echarts={fastTimeseriesEcharts}
                                                    data={hasGpuData ? gpuSeries : netSeries}
                                                    height={compactExpandedChartHeight}
                                                    isDarkMode={isDarkMode}
                                                    gradient
                                                    loading={chartLoading}
                                                    tooltipBoundary={tooltipBoundary ?? undefined}
                                                    xAxisTickCount={expandedChartXAxisTickCount}
                                                    yAxisTickCount={compactExpandedYAxisTickCount}
                                                    xAxisTickFormat={expandedChartXAxisTickFormat}
                                                    yAxisTickFormat={
                                                      hasGpuData
                                                        ? expandedNumberAxisTickFormat
                                                        : formatCompactBytesSpeed
                                                    }
                                                    tooltipValueFormat={
                                                      hasGpuData
                                                        ? formatMetricTooltipValue
                                                        : formatBytesSpeed
                                                    }
                                                    optionUpdateBehavior={
                                                      SERVER_FAST_CHART_UPDATE_BEHAVIOR
                                                    }
                                                    ariaDescription={`${server.name} 主机精简趋势`}
                                                  />
                                                </DeferredRender>
                                                {!hasGpuData && (
                                                  <TrafficTotalSummary
                                                    txTotal={txTotal}
                                                    rxTotal={rxTotal}
                                                    quota={trafficQuota}
                                                    compact
                                                  />
                                                )}
                                              </div>
                                            )}
                                          </ExpandedTrendChartCard>

                                          {hasGpuData && (
                                            <ExpandedTrendChartCard
                                              title="网络趋势"
                                              tone="info"
                                              compact
                                              className={getExpandedCardSpanClassName(2, 3)}
                                              legend={
                                                <>
                                                  <ChartLegend.SmallItem
                                                    name="上行"
                                                    color={txColor}
                                                    value={getLatestMetricValue(
                                                      records,
                                                      r => toNumber(r.net_tx, 0),
                                                      formatBytesSpeed
                                                    )}
                                                    loading={chartLoading}
                                                  />
                                                  <ChartLegend.SmallItem
                                                    name="下行"
                                                    color={rxColor}
                                                    value={getLatestMetricValue(
                                                      records,
                                                      r => toNumber(r.net_rx, 0),
                                                      formatBytesSpeed
                                                    )}
                                                    loading={chartLoading}
                                                  />
                                                </>
                                              }
                                            >
                                              {tooltipBoundary => (
                                                <div className="grid min-w-0 gap-1.5 cq-sm:grid-cols-[minmax(0,1fr)_8.5rem]">
                                                  <DeferredRender
                                                    open={isExpanded}
                                                    delay={SERVER_CHART_RENDER_DEFER_MS}
                                                    fallback={
                                                      <ChartWarmupSkeleton
                                                        height={compactExpandedChartHeight}
                                                      />
                                                    }
                                                  >
                                                    <SiteFontTimeseriesChart
                                                      echarts={fastTimeseriesEcharts}
                                                      data={netSeries}
                                                      height={compactExpandedChartHeight}
                                                      isDarkMode={isDarkMode}
                                                      gradient
                                                      loading={chartLoading}
                                                      tooltipBoundary={tooltipBoundary ?? undefined}
                                                      xAxisTickCount={expandedChartXAxisTickCount}
                                                      yAxisTickCount={compactExpandedYAxisTickCount}
                                                      xAxisTickFormat={expandedChartXAxisTickFormat}
                                                      yAxisTickFormat={formatCompactBytesSpeed}
                                                      tooltipValueFormat={formatBytesSpeed}
                                                      optionUpdateBehavior={
                                                        SERVER_FAST_CHART_UPDATE_BEHAVIOR
                                                      }
                                                      ariaDescription={`${server.name} 网络精简趋势`}
                                                    />
                                                  </DeferredRender>
                                                  <TrafficTotalSummary
                                                    txTotal={txTotal}
                                                    rxTotal={rxTotal}
                                                    quota={trafficQuota}
                                                    compact
                                                  />
                                                </div>
                                              )}
                                            </ExpandedTrendChartCard>
                                          )}
                                        </div>

                                        <NetworkQualityPanel
                                          serverName={server.name}
                                          quality={networkQuality}
                                          series={networkQualitySeries}
                                          hasData={hasNetworkQualityData}
                                          unsupported={networkQualityUnsupported}
                                          chartHeight={compactNetworkQualityChartHeight}
                                          isDarkMode={isDarkMode}
                                          chartEcharts={staticTimeseriesEcharts}
                                          isCompactViewport={isCompactViewport}
                                          onCollect={() =>
                                            loadNetworkQuality(server.id, { collect: true })
                                          }
                                          compact
                                          className="min-w-0"
                                        />
                                      </div>
                                    </>
                                  )}
                                </div>
                              </CompactExpandedRow>
                            )}
                          </React.Fragment>
                        );
                      })}
                    </Table.Body>
                  </ScrollableTable>
                </div>
              ) : (
                (() => {
                  const serverCardMap = new Map(
                    filteredServers.map(server => [String(server.id), server])
                  );
                  const renderServerCard = (server, { overlay = false } = {}) => {
                  const country = getFlagCountry(server);
                  const locationText = getServerLocationText(server);
                  const isExpanded = overlay;
                  const cardBorderClass = overlay
                    ? 'rounded-lg bg-kumo-base ring ring-kumo-line'
                    : `rounded-lg bg-kumo-base ${isExpanded ? 'ring ring-brand/70' : 'ring ring-kumo-line hover:ring-kumo-fill'}`;
                  const isChartSeriesReady = chartSeriesReadyServers.includes(server.id);
                  const isDarkMode = theme === 'dark';
                  const {
                    records,
                    cpuColor,
                    memColor,
                    cpuTempColor,
                    gpuColor,
                    vramColor,
                    powerColor,
                    gpuTempColor,
                    diskColor,
                    txColor,
                    rxColor,
                    cpuMemSeries,
                    gpuSeries,
                    netSeries,
                  } = getServerMetricDisplay(
                    server.id,
                    server.metricsCache,
                    isExpanded && isChartSeriesReady,
                    isDarkMode
                  );
                  const hasGpuData =
                    !!getGpuModelText(server.info?.gpu) ||
                    records.some(
                      r =>
                        (r.gpu_usage !== null &&
                          r.gpu_usage !== undefined &&
                          toNumber(r.gpu_usage, 0) > 0) ||
                        getGpuTemp(r) > 0
                    );
                  const tx = parseSpeed(server.info?.network?.tx_speed);
                  const rx = parseSpeed(server.info?.network?.rx_speed);
                  const dockerSummary = summarizeDockerContainers(
                    server.info?.docker,
                    getDockerContainerState
                  );
                  const dockerContainers = dockerSummary.containers;
                  const dockerExpanded = expandedDockerPanels.includes(server.id);
                  const runningContainers = dockerSummary.running;
                  const pausedContainers = dockerSummary.paused;
                  const stoppedContainers = dockerSummary.stopped;
                  const lifecycle = getServerLifecycle(server);
                  const cycleParts = getServerCycleParts(server);
                  const txTotal = cycleParts.tx;
                  const rxTotal = cycleParts.rx;
                  const trafficQuota = getTrafficQuota(server);
                  const chartLoading = !!server.metricsLoading && records.length === 0;
                  const terminalProtocol = resolveTerminalProtocol(server);
                  const effectiveTerminalProtocol =
                    terminalProtocol || (hasSshEndpoint(server) ? 'ssh' : null);
                  const terminalLabel =
                    effectiveTerminalProtocol === 'agent' ? 'Agent 隧道终端' : 'SSH 终端';
                  const primaryDisk = server.info?.disk?.[0];
                  const cpuUsage = clampPercent(toNumber(server.info?.cpu?.Usage, 0));
                  const memUsage = clampPercent(toNumber(server.info?.memory?.Usage, 0));
                  const diskUsage = clampPercent(toNumber(primaryDisk?.usage, 0));
                  const cpuTemp = toNumber(server.info?.cpu?.Temp, 0);
                  const cpuPower = toNumber(server.info?.cpu?.Power, 0);
                  const gpuUsage = clampPercent(toNumber(server.info?.gpu?.Usage, 0));
                  const gpuTemp = toNumber(server.info?.gpu?.Temp, 0);
                  const gpuMemPercent = clampPercent(toNumber(server.info?.gpu?.Percent, 0));
                  const physicalCores = server.info?.cpu?.PhysicalCores || server.info?.cpu?.Cores;
                  const logicalCores = server.info?.cpu?.LogicalCores;
                  const coreText =
                    physicalCores && logicalCores && physicalCores !== logicalCores
                      ? `${physicalCores}核 / ${logicalCores}线程`
                      : `${physicalCores || '-'} 核`;
                  const networkQuality = networkQualityByServer[server.id] || {};
                  const networkQualitySeries = isExpanded
                    ? buildNetworkQualitySeries(networkQuality, isDarkMode)
                    : [];
                  const hasNetworkQualityData =
                    isExpanded && networkQualitySeries.some(series => series.data.length > 0);
                  const networkQualityUnsupported =
                    isExpanded &&
                    (!!networkQuality.unsupported ||
                      isNetworkQualityUnsupportedError(
                        networkQuality.error || networkQuality.unsupportedMessage
                      ));
                  const metricsHealth = resolveServerMetricsHealth(server);
                  const rowMuted = !isServerOnline(server);
                  const detailDisabled = rowMuted || !server.info;

                  return (
                    <ContextMenu.Root key={server.id}>
                      <ContextMenu.Trigger
                        className={`@container select-none flex ${overlay ? 'min-h-0' : 'h-full'} min-h-0 flex-col overflow-hidden bg-kumo-base ${cardBorderClass}`}
                      >
                        <div
                          onClick={
                            overlay
                              ? () => {
                                  setExpandedServers(prev => prev.filter(id => id !== server.id));
                                  setCardDetailServerId(null);
                                }
                              : serverCardDragEnabled || detailDisabled
                                ? undefined
                                : () => {
                                    toggleServerExpand(server.id);
                                    setCardDetailServerId(server.id);
                                  }
                          }
                          title={!overlay && detailDisabled ? '离线或无监控数据，详情不可用' : undefined}
                          className={`tile-header grid min-h-[48px] grid-cols-[minmax(0,1fr)_2.25rem] gap-x-2 gap-y-1.5 p-0 py-1.5 ${overlay ? 'shrink-0 border-b border-kumo-line' : ''} ${!overlay && serverCardDragEnabled ? 'cursor-move' : ''} ${!overlay && detailDisabled && !serverCardDragEnabled ? 'cursor-not-allowed' : ''} cq-card-wide:flex cq-card-wide:flex-nowrap cq-card-wide:items-center cq-card-wide:justify-between cq-card-wide:gap-2.5 cq-card-wide:py-1.5 cq-card-wide:pl-3 cq-card-wide:pr-1.5`}
                        >
                          <div className="order-1 flex min-w-0 items-center gap-3 pl-3 pr-1.5 cq-card-wide:p-0">
                            <span className="relative flex h-2 w-2 rounded-full">
                              <span
                                className={`animate-ping absolute inline-flex h-full w-full rounded-full opacity-75 ${metricsHealth.dotClassName}`}
                              ></span>
                              <span
                                className={`relative inline-flex rounded-full h-2 w-2 ${metricsHealth.dotClassName}`}
                              ></span>
                            </span>

                            <div className="flex flex-col min-w-0 gap-1">
                              <div className="flex items-center gap-2 min-w-0">
                                <i className={`${getOSIconClass(server.info?.platform)} ${rowMuted ? 'opacity-60 grayscale' : ''}`}></i>
                                <span
                                  onDoubleClick={e => {
                                    e.stopPropagation();
                                    startRenameServer(server);
                                  }}
                                  className={`text-xs font-semibold truncate hover:text-brand ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-strong'}`}
                                >
                                  {country && (
                                    <CountryFlag
                                      countryCode={country}
                                      className="mr-1.5 h-3 w-4 align-[-1px] text-xs"
                                    />
                                  )}
                                  {server.name}
                                </span>
                                {server.tags &&
                                  server.tags
                                    .filter(t => t !== 'Agent')
                                    .map(t => (
                                      <Badge
                                        key={t}
                                        variant={getServerTagBadgeVariant(t)}
                                        title={t}
                                        className="text-[9px] font-semibold"
                                      >
                                        {t}
                                      </Badge>
                                    ))}
                                {metricsHealth.stale && (
                                  <Badge
                                    variant={metricsHealth.variant}
                                    appearance="dot"
                                    title="Agent 连接存在，但最近未收到有效指标上报"
                                  >
                                    {metricsHealth.label}
                                  </Badge>
                                )}
                              </div>
                            </div>
                          </div>

                          <div className="contents cq-card-wide:order-2 cq-card-wide:ml-auto cq-card-wide:flex cq-card-wide:min-w-0 cq-card-wide:flex-nowrap cq-card-wide:items-center cq-card-wide:gap-2.5">
                            <div className="order-3 col-span-2 grid w-full grid-cols-4 gap-1 px-1.5 text-[11px] font-semibold text-kumo-strong cq-card-wide:order-none cq-card-wide:col-span-1 cq-card-wide:flex cq-card-wide:w-auto cq-card-wide:items-center cq-card-wide:gap-2.5 cq-card-wide:p-0">
                              {server.info ? (
                                <>
                                <CompactMetricBar
                                  label="CPU"
                                  value={`${parseInt(server.info.cpu?.Usage || '0')}%`}
                                  valueClassName="text-kumo-success"
                                  barClassName="bg-kumo-success"
                                  color={cpuColor}
                                  width={server.info.cpu?.Usage || '0%'}
                                  muted={rowMuted}
                                />
                                <CompactMetricBar
                                  label="内存"
                                  value={`${parseInt(server.info.memory?.Usage || '0')}%`}
                                  valueClassName="text-kumo-info"
                                  barClassName="bg-kumo-info"
                                  color={memColor}
                                  width={server.info.memory?.Usage || '0%'}
                                  muted={rowMuted}
                                />
                                {server.info.disk?.[0] && (
                                  <CompactMetricBar
                                    label="硬盘"
                                    value={`${parseInt(server.info.disk[0].usage || '0')}%`}
                                    valueClassName="text-kumo-warning"
                                    barClassName="bg-kumo-warning"
                                    color={diskColor}
                                    width={server.info.disk[0].usage || '0%'}
                                    muted={rowMuted}
                                  />
                                )}
                                <div className="hidden cq-card-wide:block">
                                  <CompactMetricBar
                                    label="余期"
                                    value={
                                      lifecycle.expiresAt
                                        ? `${Math.round(lifecycle.remainingPercent)}%`
                                        : '永久'
                                    }
                                    valueClassName={lifecycle.toneClass}
                                    barClassName={
                                      lifecycle.expired
                                        ? 'bg-kumo-danger'
                                        : lifecycle.remainingPercent <= 20
                                          ? 'bg-kumo-warning'
                                          : 'bg-kumo-success'
                                    }
                                    width={`${lifecycle.remainingPercent}%`}
                                    muted={rowMuted}
                                  />
                                </div>
                                {server.info.network && (
                                  <div className="flex min-w-0 flex-col justify-center gap-0.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-1.5 py-0.5 font-mono leading-[1.2] tabular-nums cq-card-wide:hidden">
                                    <span className={`truncate ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-info'}`}>
                                      &uarr; {tx.num}
                                      {tx.unit}
                                    </span>
                                    <span className={`truncate ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-success'}`}>
                                      &darr; {rx.num}
                                      {rx.unit}
                                    </span>
                                  </div>
                                )}
                                {server.info.network && (
                                  <div className="hidden h-8 w-[126px] shrink-0 flex-col justify-center gap-px rounded-md border border-kumo-line bg-kumo-recessed/35 px-[5px] py-0 text-[10px] font-semibold leading-[1.2] tabular-nums cq-card-wide:flex">
                                    <span className={`flex items-center justify-between whitespace-nowrap font-mono ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-info'}`}>
                                      <span className="flex flex-1 items-center">
                                        <span className="w-2 text-center opacity-70">&uarr;</span>
                                        <span className="ml-1 w-[30px] text-right">{tx.num}</span>
                                        <span className="ml-0.5 w-3 text-left opacity-80">
                                          {tx.unit}
                                        </span>
                                      </span>
                                      <span
                                        className="w-[62px] truncate text-right opacity-85"
                                        title={txTotal.text}
                                      >
                                        {txTotal.num}
                                        {txTotal.unit}
                                      </span>
                                    </span>
                                    <span className={`flex items-center justify-between whitespace-nowrap font-mono ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-success'}`}>
                                      <span className="flex flex-1 items-center">
                                        <span className="w-2 text-center opacity-70">&darr;</span>
                                        <span className="ml-1 w-[30px] text-right">{rx.num}</span>
                                        <span className="ml-0.5 w-3 text-left opacity-80">
                                          {rx.unit}
                                        </span>
                                      </span>
                                      <span
                                        className="w-[62px] truncate text-right opacity-85"
                                        title={rxTotal.text}
                                      >
                                        {rxTotal.num}
                                        {rxTotal.unit}
                                      </span>
                                    </span>
                                  </div>
                                )}
                                </>
                              ) : (
                                <>
                                  <CompactMetricBar label="CPU" value="--" barClassName="bg-kumo-subtle/40" width="0%" />
                                  <CompactMetricBar label="内存" value="--" barClassName="bg-kumo-subtle/40" width="0%" />
                                  <CompactMetricBar label="硬盘" value="--" barClassName="bg-kumo-subtle/40" width="0%" />
                                  <CompactMetricBar label="余期" value="--" barClassName="bg-kumo-subtle/40" width="0%" />
                                </>
                              )}
                              </div>

                            <div
                              className="order-2 flex items-center justify-end gap-1.5 pr-1.5 cq-card-wide:order-none cq-card-wide:p-0"
                              onClick={e => e.stopPropagation()}
                            >
                              <ServerConnectionActions
                                remoteDesktopAvailable={canOpenRemoteDesktop(server)}
                                terminalLabel={terminalLabel}
                                terminalDisabled={
                                  !canOpenTerminal(server) && !hasSshEndpoint(server)
                                }
                                onOpenRemoteDesktop={() => openRemoteDesktop(server)}
                                onOpenTerminal={() => openSSHTerminal(server)}
                                buttonClassName="h-7 w-7 p-0 cq-card-wide:h-8 cq-card-wide:w-8"
                              />
                            </div>
                          </div>
                        </div>

                        {isExpanded && (

                          <div
                            className={isDenseViewport ? 'p-1.5' : 'p-1.5 cq-sm:p-2'}
                          >
                            {server.loading && !server.info ? (
                              <div className="space-y-2 py-5">
                                <SkeletonLine className="h-4 w-1/3 mx-auto" />
                                <SkeletonLine className="h-4 w-1/2 mx-auto" />
                              </div>
                            ) : server.error ? (
                              <div className="rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-xs font-semibold text-kumo-danger">
                                {server.error}
                              </div>
                            ) : (
                              <div
                                className={`flex flex-col ${isDenseViewport ? 'gap-1.5' : 'gap-2'}`}
                              >
                                <ExpandedSection title="资源状态" tone="brand">
                                  <div className="grid grid-cols-2 gap-1.5 cq-lg:grid-cols-4">
                                    <ExpandedProgressMetric
                                      label="CPU"
                                      value={cpuUsage}
                                      detail={`${Math.round(cpuUsage)}%`}
                                      caption={`${coreText}${cpuTemp > 0 ? ` · ${Math.round(cpuTemp)}°C` : ''}${cpuPower > 0 ? ` · ${cpuPower.toFixed(1)}W` : ''}`}
                                      indicatorClassName="!bg-none !bg-kumo-success"
                                      valueClassName="text-kumo-success"
                                      muted={rowMuted}
                                    />
                                    <ExpandedProgressMetric
                                      label="内存"
                                      value={memUsage}
                                      detail={`${Math.round(memUsage)}%`}
                                      caption={`${server.info?.memory?.Used || '-'} / ${server.info?.memory?.Total || '-'}`}
                                      indicatorClassName="!bg-none !bg-kumo-info"
                                      valueClassName="text-kumo-info"
                                      muted={rowMuted}
                                    />
                                    <ExpandedProgressMetric
                                      label="磁盘"
                                      value={diskUsage}
                                      detail={primaryDisk ? `${Math.round(diskUsage)}%` : '-'}
                                      caption={
                                        primaryDisk
                                          ? `${primaryDisk.used || '-'} / ${primaryDisk.total || '-'}`
                                          : '未上报'
                                      }
                                      indicatorClassName="!bg-none !bg-kumo-warning"
                                      valueClassName="text-kumo-warning"
                                      muted={rowMuted}
                                    />
                                    <ExpandedProgressMetric
                                      label="余期"
                                      value={lifecycle.remainingPercent}
                                      detail={lifecycle.label}
                                      caption={
                                        lifecycle.expiresAt
                                          ? `${formatDateTime(lifecycle.startsAt)} - ${formatDateTime(lifecycle.expiresAt)}`
                                          : '长期有效'
                                      }
                                      indicatorClassName={lifecycle.indicatorClassName}
                                      valueClassName={lifecycle.toneClass}
                                      muted={rowMuted}
                                    />
                                  </div>
                                </ExpandedSection>

                                <div className="flex min-w-0 flex-col gap-2">
                                  <div className={getExpandedInfoGridClassName(isDenseViewport)}>
                                    <ExpandedSection
                                      title="系统概览"
                                      tone="success"
                                      className={getExpandedCardSpanClassName(0, 1)}
                                    >
                                      <div className="grid grid-cols-1 gap-1.5 cq-sm:grid-cols-3 cq-xl:grid-cols-4">
                                        <ExpandedInfoChip
                                          label="系统"
                                          value={
                                            [server.info?.platform, server.info?.platformVersion]
                                              .filter(Boolean)
                                              .join(' ') ||
                                            server.info?.system?.Kernel ||
                                            '-'
                                          }
                                        />
                                        <ExpandedInfoChip
                                          label="CPU 型号"
                                          value={
                                            server.info?.cpu?.Model ||
                                            server.metadata?.cpu_model ||
                                            server.metadata?.cpu_name ||
                                            server.metadata?.processor ||
                                            '-'
                                          }
                                        />
                                        <ExpandedInfoChip label="核心" value={coreText} />
                                        <ExpandedInfoChip
                                          label="GPU 型号"
                                          value={
                                            getGpuModelText(server.info?.gpu) ||
                                            server.metadata?.gpu_model ||
                                            server.metadata?.gpu_name ||
                                            getGpuModelText(server.metadata?.gpu) ||
                                            '-'
                                          }
                                        />
                                        <ExpandedInfoChip
                                          label="负载"
                                          value={server.info?.cpu?.Load || '-'}
                                          valueClassName="font-mono text-kumo-strong"
                                        />
                                        <ExpandedInfoChip
                                          label="在线"
                                          value={formatUptimeDaysOnly(
                                            server.info?.uptime || server.info?.system?.Uptime
                                          )}
                                        />
                                        {/* <ExpandedInfoChip label="延迟" value={formatResponseTime(server.response_time)} valueClassName="text-kumo-success" /> */}
                                        <ExpandedInfoChip
                                          label="Agent 版本"
                                          value={server.info?.agentVersion || '-'}
                                        />
                                        {/* <ExpandedInfoChip label="模式" value={getServerMonitorModeLabel(server)} /> */}
                                        <ExpandedInfoChip
                                          label="地址"
                                          value={getHostAddress(server, serverIpDisplayMode)}
                                          valueClassName="font-mono text-kumo-strong"
                                        />
                                      </div>
                                    </ExpandedSection>

                                    <ExpandedSection
                                      title="网络"
                                      tone="info"
                                      className={getExpandedCardSpanClassName(1, 3)}
                                    >
                                      <div className="grid grid-cols-2 gap-1.5">
                                        <ExpandedStatTile
                                          label="上传"
                                          value={server.info?.network?.tx_speed || '0 B/s'}
                                          tone="info"
                                          inline
                                        />
                                        <ExpandedStatTile
                                          label="下载"
                                          value={server.info?.network?.rx_speed || '0 B/s'}
                                          tone="success"
                                          inline
                                        />
                                        <ExpandedInfoChip
                                          label="本期上行"
                                          value={txTotal.text}
                                          valueClassName="text-kumo-info"
                                        />
                                        <ExpandedInfoChip
                                          label="本期下行"
                                          value={rxTotal.text}
                                          valueClassName="text-kumo-success"
                                        />
                                        <ExpandedInfoChip
                                          label="连接"
                                          value={server.info?.network?.connections || 0}
                                        />
                                        {trafficQuota && (
                                          <ExpandedInfoChip
                                            label="剩余流量"
                                            value={
                                              trafficQuota.unlimited
                                                ? '无限'
                                                : trafficQuota.overLimit
                                                  ? '已超限'
                                                  : trafficQuota.remainingText ||
                                                    `${trafficQuota.percent.toFixed(trafficQuota.percent >= 10 ? 0 : 1)}%`
                                            }
                                            valueClassName={
                                              trafficQuota.unlimited
                                                ? 'text-kumo-info'
                                                : trafficQuota.overLimit
                                                  ? 'text-kumo-danger'
                                                  : trafficQuota.nearAlert
                                                    ? 'text-kumo-warning'
                                                    : 'text-kumo-info'
                                            }
                                          />
                                        )}
                                      </div>
                                    </ExpandedSection>

                                    <ExpandedTrendChartCard
                                      title="网络趋势"
                                      tone="info"
                                      className={getExpandedCardSpanClassName(1, 3)}
                                      legend={
                                        <>
                                          <ChartLegend.SmallItem
                                            name="上行"
                                            color={txColor}
                                            value={getLatestMetricValue(
                                              records,
                                              r => toNumber(r.net_tx, 0),
                                              formatBytesSpeed
                                            )}
                                            loading={chartLoading}
                                          />
                                          <ChartLegend.SmallItem
                                            name="下行"
                                            color={rxColor}
                                            value={getLatestMetricValue(
                                              records,
                                              r => toNumber(r.net_rx, 0),
                                              formatBytesSpeed
                                            )}
                                            loading={chartLoading}
                                          />
                                        </>
                                      }
                                    >
                                      {tooltipBoundary => (
                                        <DeferredRender
                                          open={isExpanded}
                                          delay={SERVER_CHART_RENDER_DEFER_MS}
                                          fallback={
                                            <ChartWarmupSkeleton
                                              height={expandedTrendChartHeight}
                                            />
                                          }
                                        >
                                          <SiteFontTimeseriesChart
                                            echarts={fastTimeseriesEcharts}
                                            data={netSeries}
                                            height={expandedTrendChartHeight}
                                            isDarkMode={isDarkMode}
                                            gradient
                                            loading={chartLoading}
                                            tooltipBoundary={tooltipBoundary ?? undefined}
                                            xAxisTickCount={expandedChartXAxisTickCount}
                                            yAxisTickCount={expandedChartYAxisTickCount}
                                            xAxisTickFormat={expandedChartXAxisTickFormat}
                                            yAxisTickFormat={expandedSpeedAxisTickFormat}
                                            tooltipValueFormat={formatBytesSpeed}
                                            optionUpdateBehavior={SERVER_FAST_CHART_UPDATE_BEHAVIOR}
                                            ariaDescription={`${server.name} 网络上传与下载速度趋势`}
                                          />
                                        </DeferredRender>
                                      )}
                                    </ExpandedTrendChartCard>
                                  </div>

                                  <div
                                    className={getExpandedTrendGridClassName(
                                      false,
                                      isDenseViewport
                                    )}
                                  >
                                    <ExpandedTrendChartCard
                                      title="CPU / 内存趋势"
                                      tone="success"
                                      className={getExpandedCardSpanClassName(
                                        0,
                                        hasGpuData ? 2 : 1
                                      )}
                                      legend={
                                        <>
                                          <ChartLegend.SmallItem
                                            name="CPU"
                                            color={cpuColor}
                                            value={`${Math.round(cpuUsage)}%`}
                                            loading={chartLoading}
                                          />
                                          <ChartLegend.SmallItem
                                            name="内存"
                                            color={memColor}
                                            value={`${Math.round(memUsage)}%`}
                                            loading={chartLoading}
                                          />
                                          <ChartLegend.SmallItem
                                            name="温度"
                                            color={cpuTempColor}
                                            value={getLatestMetricValue(
                                              records,
                                              getCpuTemp,
                                              v => `${v.toFixed(1)}°C`
                                            )}
                                            loading={chartLoading}
                                          />
                                        </>
                                      }
                                    >
                                      {tooltipBoundary => (
                                        <DeferredRender
                                          open={isExpanded}
                                          delay={SERVER_CHART_RENDER_DEFER_MS}
                                          fallback={
                                            <ChartWarmupSkeleton
                                              height={expandedTrendChartHeight}
                                            />
                                          }
                                        >
                                          <SiteFontTimeseriesChart
                                            echarts={fastTimeseriesEcharts}
                                            data={cpuMemSeries}
                                            height={expandedTrendChartHeight}
                                            isDarkMode={isDarkMode}
                                            gradient
                                            loading={chartLoading}
                                            tooltipBoundary={tooltipBoundary ?? undefined}
                                            xAxisTickCount={expandedChartXAxisTickCount}
                                            yAxisTickCount={expandedChartYAxisTickCount}
                                            xAxisTickFormat={expandedChartXAxisTickFormat}
                                            yAxisTickFormat={expandedNumberAxisTickFormat}
                                            tooltipValueFormat={formatMetricTooltipValue}
                                            optionUpdateBehavior={SERVER_FAST_CHART_UPDATE_BEHAVIOR}
                                            ariaDescription={`${server.name} CPU 与内存使用趋势`}
                                          />
                                        </DeferredRender>
                                      )}
                                    </ExpandedTrendChartCard>

                                    {hasGpuData && (
                                      <ExpandedTrendChartCard
                                        title="GPU 趋势"
                                        tone="warning"
                                        className={getExpandedCardSpanClassName(1, 2)}
                                        legend={
                                          <>
                                            <TrendSeriesLabel name="GPU" color={gpuColor} />
                                            <TrendSeriesLabel name="显存" color={vramColor} />
                                            <TrendSeriesLabel name="功耗" color={powerColor} />
                                            <TrendSeriesLabel name="温度" color={gpuTempColor} />
                                          </>
                                        }
                                      >
                                        {tooltipBoundary => (
                                          <DeferredRender
                                            open={isExpanded}
                                            delay={SERVER_CHART_RENDER_DEFER_MS}
                                            fallback={
                                              <ChartWarmupSkeleton
                                                height={expandedTrendChartHeight}
                                              />
                                            }
                                          >
                                            <SiteFontTimeseriesChart
                                              echarts={fastTimeseriesEcharts}
                                              data={gpuSeries}
                                              height={expandedTrendChartHeight}
                                              isDarkMode={isDarkMode}
                                              gradient
                                              loading={chartLoading}
                                              tooltipBoundary={tooltipBoundary ?? undefined}
                                              xAxisTickCount={expandedChartXAxisTickCount}
                                              yAxisTickCount={expandedChartYAxisTickCount}
                                              xAxisTickFormat={expandedChartXAxisTickFormat}
                                              yAxisTickFormat={expandedNumberAxisTickFormat}
                                              tooltipValueFormat={formatMetricTooltipValue}
                                              optionUpdateBehavior={
                                                SERVER_FAST_CHART_UPDATE_BEHAVIOR
                                              }
                                              ariaDescription={`${server.name} GPU 使用率、显存与功耗趋势`}
                                            />
                                          </DeferredRender>
                                        )}
                                      </ExpandedTrendChartCard>
                                    )}
                                  </div>
                                </div>

                                <NetworkQualityPanel
                                  serverName={server.name}
                                  quality={networkQuality}
                                  series={networkQualitySeries}
                                  hasData={hasNetworkQualityData}
                                  unsupported={networkQualityUnsupported}
                                  chartHeight={networkQualityChartHeight}
                                  isDarkMode={isDarkMode}
                                  chartEcharts={staticTimeseriesEcharts}
                                  isCompactViewport={isCompactViewport}
                                  onCollect={() => loadNetworkQuality(server.id, { collect: true })}
                                />

                                {server.info?.docker?.installed && (
                                  <AppCard padding="none" className="overflow-hidden">
                                    <Button
                                      type="button"
                                      variant="ghost"
                                      size="sm"
                                      className="h-8 w-full justify-between px-3 text-left"
                                      onClick={event => {
                                        event.stopPropagation();
                                        toggleDockerPanel(server.id);
                                      }}
                                    >
                                      <span className="text-xs font-semibold text-kumo-strong">
                                        Docker 容器
                                      </span>
                                      <span className="flex min-w-0 items-center gap-1.5">
                                        <Badge variant="success" appearance="dot">
                                          {runningContainers} 运行
                                        </Badge>
                                        {pausedContainers > 0 && (
                                          <Badge variant="warning" appearance="dot">
                                            {pausedContainers} 暂停
                                          </Badge>
                                        )}
                                        {stoppedContainers > 0 && (
                                          <Badge variant="error" appearance="dot">
                                            {stoppedContainers} 停止
                                          </Badge>
                                        )}
                                        {dockerExpanded ? (
                                          <ChevronUp className="w-3.5 h-3.5" />
                                        ) : (
                                          <ChevronDown className="w-3.5 h-3.5" />
                                        )}
                                      </span>
                                    </Button>

                                    <AnimatedCollapse open={dockerExpanded} keepMounted>
                                      {dockerContainers.length > 0 ? (
                                        <div className="grid grid-cols-1 divide-y divide-kumo-line cq-xl:grid-cols-2 cq-xl:divide-x cq-xl:divide-y-0">
                                          {dockerContainers.map(c => {
                                            const state = getDockerContainerState(c);
                                            const stateBadge = getDockerStateBadge(state);
                                            const containerId = getDockerContainerId(c);
                                            const containerName = getDockerContainerName(c);
                                            const containerImage = getDockerContainerImage(c);
                                            const updateCheck = getDockerContainerUpdateCheck(
                                              server.id,
                                              c
                                            );
                                            const updateBadge = getDockerUpdateBadge(updateCheck);
                                            const updateChecking = isDockerContainerUpdateChecking(
                                              server.id,
                                              c
                                            );
                                            const toggleAction =
                                              state === 'running'
                                                ? 'container.pause'
                                                : state === 'paused'
                                                  ? 'container.unpause'
                                                  : 'container.start';
                                            const togglePayload = {
                                              serverId: server.id,
                                              containerId,
                                              containerName,
                                              image: containerImage,
                                            };
                                            const togglePending = isDockerActionPending(
                                              server.id,
                                              toggleAction,
                                              togglePayload
                                            );
                                            const restartPayload = {
                                              serverId: server.id,
                                              containerId,
                                              containerName,
                                              image: containerImage,
                                            };
                                            const restartPending = isDockerActionPending(
                                              server.id,
                                              'container.restart',
                                              restartPayload
                                            );
                                            const updatePayload = {
                                              serverId: server.id,
                                              containerId,
                                              containerName,
                                              image: containerImage,
                                            };
                                            const updatePending = isDockerActionPending(
                                              server.id,
                                              'container.update',
                                              updatePayload
                                            );
                                            const updateProgress = getDockerActionProgress(
                                              server.id,
                                              'container.update',
                                              updatePayload
                                            );
                                            const updateConfirmKey = `container.update::${getDockerContainerSelectionKey(server.id, updatePayload)}`;
                                            return (
                                              <div
                                                key={containerId || `${server.id}-${containerName}`}
                                                className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 px-3 py-2 text-xs hover:bg-kumo-recessed/20"
                                              >
                                                <div className="flex min-w-0 items-center gap-2">
                                                  <Badge
                                                    variant={stateBadge.variant}
                                                    appearance="dot"
                                                    className="shrink-0"
                                                  >
                                                    {stateBadge.label}
                                                  </Badge>
                                                  <div className="min-w-0">
                                                    <div
                                                      className="truncate font-semibold text-kumo-strong"
                                                      title={containerName}
                                                    >
                                                      {containerName}
                                                    </div>
                                                    <div
                                                      className="truncate font-mono text-[10px] text-kumo-subtle"
                                                      title={containerImage}
                                                    >
                                                      {containerImage}
                                                    </div>
                                                    {(updateCheck ||
                                                      updateChecking ||
                                                      updatePending) && (
                                                      <div className="mt-1">
                                                        {updatePending ? (
                                                          <span className="inline-flex items-center gap-1 text-[10px] font-semibold text-brand">
                                                            <RefreshCw className="h-3 w-3 animate-spin" />
                                                            更新中
                                                            {updateProgress > 0
                                                              ? ` ${updateProgress}%`
                                                              : ''}
                                                          </span>
                                                        ) : updateChecking ? (
                                                          <span className="inline-flex items-center gap-1 text-[10px] font-semibold text-kumo-subtle">
                                                            <RefreshCw className="h-3 w-3 animate-spin" />
                                                            检测中
                                                          </span>
                                                        ) : (
                                                          <Badge
                                                            variant={updateBadge.variant}
                                                            appearance="dot"
                                                            title={updateBadge.title}
                                                          >
                                                            {updateBadge.label}
                                                          </Badge>
                                                        )}
                                                      </div>
                                                    )}
                                                  </div>
                                                </div>
                                                <div className="flex shrink-0 items-center gap-1">
                                                  <Button
                                                    shape="square"
                                                    size="sm"
                                                    variant="secondary"
                                                    aria-label={
                                                      state === 'running'
                                                        ? '暂停容器'
                                                        : state === 'paused'
                                                          ? '恢复容器'
                                                          : '启动容器'
                                                    }
                                                    title={
                                                      state === 'running'
                                                        ? '暂停'
                                                        : state === 'paused'
                                                          ? '恢复'
                                                          : '启动'
                                                    }
                                                    icon={
                                                      togglePending ? (
                                                        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                      ) : state === 'running' ? (
                                                        <Pause className="h-3.5 w-3.5" />
                                                      ) : (
                                                        <Play className="h-3.5 w-3.5" />
                                                      )
                                                    }
                                                    disabled={
                                                      togglePending ||
                                                      restartPending ||
                                                      updatePending
                                                    }
                                                    onClick={event => {
                                                      event.stopPropagation();
                                                      submitDockerTask(toggleAction, togglePayload);
                                                    }}
                                                  />
                                                  <Button
                                                    shape="square"
                                                    size="sm"
                                                    variant="secondary"
                                                    aria-label="重启容器"
                                                    title="重启"
                                                    icon={
                                                      restartPending ? (
                                                        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                      ) : (
                                                        <RotateCw className="h-3.5 w-3.5" />
                                                      )
                                                    }
                                                    disabled={
                                                      togglePending ||
                                                      restartPending ||
                                                      updatePending
                                                    }
                                                    onClick={event => {
                                                      event.stopPropagation();
                                                      submitDockerTask(
                                                        'container.restart',
                                                        restartPayload
                                                      );
                                                    }}
                                                  />
                                                  <Button
                                                    shape="square"
                                                    size="sm"
                                                    variant="secondary"
                                                    aria-label="检测镜像更新"
                                                    title="检测更新"
                                                    icon={
                                                      updateChecking ? (
                                                        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                      ) : (
                                                        <Search className="h-3.5 w-3.5" />
                                                      )
                                                    }
                                                    disabled={updateChecking}
                                                    onClick={event => {
                                                      event.stopPropagation();
                                                      checkDockerUpdatesForServer(server, c);
                                                    }}
                                                  />
                                                  <Button
                                                    shape="square"
                                                    size="sm"
                                                    variant={
                                                      isDockerUpdateConfirmActive(updateConfirmKey)
                                                        ? 'secondary-destructive'
                                                        : 'primary'
                                                    }
                                                    aria-label={
                                                      isDockerUpdateConfirmActive(updateConfirmKey)
                                                        ? '再次确认更新容器'
                                                        : '一键更新容器'
                                                    }
                                                    title={
                                                      isDockerUpdateConfirmActive(updateConfirmKey)
                                                        ? '再次点击确认更新'
                                                        : '一键更新'
                                                    }
                                                    icon={
                                                      updatePending ? (
                                                        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                      ) : (
                                                        <Upload className="h-3.5 w-3.5" />
                                                      )
                                                    }
                                                    disabled={
                                                      togglePending ||
                                                      restartPending ||
                                                      updatePending
                                                    }
                                                    onClick={event => {
                                                      event.stopPropagation();
                                                      handleDockerContainerUpdate(updatePayload);
                                                    }}
                                                  />
                                                </div>
                                              </div>
                                            );
                                          })}
                                        </div>
                                      ) : (
                                        <div className="px-3 py-4 text-center text-xs text-kumo-subtle">
                                          暂无容器
                                        </div>
                        )}
                                      </AnimatedCollapse>
                                  </AppCard>
                                )}
                              </div>
                            )}
                          </div>
                        )}

                      </ContextMenu.Trigger>

                      {renderServerContextMenu(server)}
                    </ContextMenu.Root>
                  );
                };

                const serverById = item => serverCardMap.get(String(item.i));
                const detailServer = cardDetailServerId
                  ? serverCardMap.get(String(cardDetailServerId))
                  : null;

                return (
                  <div className="relative flex min-h-0 flex-1 flex-col">
                    <div className="min-h-0 flex-1 overflow-y-auto p-[2px] scrollbar-thin has-[.react-draggable-dragging]:overflow-visible">
                      <TileGrid
                        layout={serverCardLayout}
                        onLayoutChange={handleServerCardLayoutChange}
                        cols={cardColumns}
                        rowHeight={getServerCardRowHeight}
                        dragEnabled={serverCardDragEnabled && !serverSearchText.trim() && serverStatusFilter === 'all'}
                        resizeEnabled={false}
                        renderTile={item => {
                          const server = serverById(item);
                          return server ? renderServerCard(server) : null;
                        }}
                      >
                      </TileGrid>
                    </div>
                    {detailServer && (
                      <div className="absolute inset-0 z-30 overflow-y-auto p-px scrollbar-thin">
                        <div className="relative flex min-h-full w-full flex-col">
                          {renderServerCard(detailServer, { overlay: true })}
                        </div>
                      </div>
                    )}
                  </div>
                );
                })()
              )}
            </div>
          )}
        </div>
        </>
      )}

    </>
  );
}
