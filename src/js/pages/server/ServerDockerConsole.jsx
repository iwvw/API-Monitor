import { ChartPalette, LayerCard, Meter, Tabs } from '@cloudflare/kumo';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppCard, ChartBoundaryBox } from '../../components/ui/AppPrimitives.jsx';
import AnimatedCollapse from '../../components/AnimatedCollapse.jsx';
import SiteFontTimeseriesChart from '../../components/SiteFontTimeseriesChart.jsx';
import { Activity, Box, ChevronDown, ChevronUp, Database, Edit, FileText, FolderOpen, Globe, HardDrive, Pause, Play, RefreshCw, RotateCw, Search, Settings, Square, Trash, Upload } from '../../components/Icons.jsx';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { isDockerImagePruneCandidate, summarizeDockerTaskMessage } from '../../modules/dockerTasks.js';
import { asArray, buildDockerPairSeries, buildDockerStatSeries, formatComposeStatusLabel, getComposeConfigFiles, getComposePrimaryConfigFile, getComposeProjectName, getComposeStatus, getComposeWorkingDir, getDockerImageId, getDockerImageRepository, getDockerImageSize, getDockerImageTag, getDockerNetworkDriver, getDockerNetworkId, getDockerNetworkName, getDockerNetworkScope, getDockerTaskActionLabel, getDockerTaskStateLabel, getDockerVolumeDriver, getDockerVolumeName, getDockerVolumeScope, isComposeConfigMissing, isDockerOverviewHostVisible } from './serverHelpers.js';
import { clampPercent, formatBytesValue, formatPercentAxis, toNumber } from './utils.js';
import { SERVER_FAST_CHART_UPDATE_BEHAVIOR, SERVER_SECONDARY_TABS_GROUP_CLASS, SERVER_SECTION_HEADER_CLASS } from './constants.js';

export default function ServerDockerConsole(props) {
  const {
    batchUpdateVisibleDockerContainers,
    checkAllDockerUpdates,
    checkDockerUpdatesForServer,
    checkVisibleDockerUpdates,
    confirmPress,
    dockerActiveBatchUpdateConfirmKey,
    dockerActiveBatchUpdateScopeName,
    dockerActiveBatchUpdateTargets,
    dockerBulkUpdateChecking,
    dockerComposeProjects,
    dockerContainerManagementServers,
    dockerContainerManagementTotals,
    dockerContainerManagementUpdatableContainers,
    dockerContainerStateFilter,
    dockerImages,
    dockerManagementBatchUpdateConfirmKey,
    dockerNetworks,
    dockerOverviewServers,
    dockerResourceLoading,
    dockerSearchQuery,
    dockerSelectedContainerKeySet,
    dockerStats,
    dockerStatsHistory,
    dockerStatsSummary,
    dockerSubTab,
    dockerTaskStreamConnected,
    dockerTasks,
    dockerVolumes,
    expandedChartXAxisTickFormat,
    fastTimeseriesEcharts,
    getDockerActionProgress,
    getDockerContainerId,
    getDockerContainerImage,
    getDockerContainerName,
    getDockerContainerPorts,
    getDockerContainerSelectionKey,
    getDockerContainerState,
    getDockerContainerSummary,
    getDockerContainerUpdateCheck,
    getDockerStateBadge,
    getDockerUpdateBadge,
    getUpdatableDockerContainers,
    handleDockerContainerUpdate,
    isArmed,
    isCompactViewport,
    isDockerActionPending,
    isDockerContainerUpdateChecking,
    isDockerHostExpanded,
    isDockerUpdateConfirmActive,
    loadDockerResources,
    openDockerComposeConfig,
    pruneDockerImagesForHosts,
    pruneDockerNetworksForHosts,
    renderDockerEmptyState,
    renderDockerFilterChip,
    renderDockerHostResourceSection,
    renderDockerResourceSideRail,
    renderDockerSimpleLogCard,
    selectedUpdatableDockerContainers,
    serverCurrentTab,
    setDockerContainerStateFilter,
    setDockerSearchQuery,
    setDockerSubTab,
    setShowDockerTaskDetails,
    showDockerBlockingLoading,
    showDockerTaskDetails,
    submitDockerPruneTask,
    submitDockerTask,
    theme,
    toggleDockerContainerSelection,
    toggleDockerOverviewServer,
    visibleDockerContainerServers,
    visibleUpdatableDockerContainers,
  } = props;
  return (
    <>
      {serverCurrentTab === 'docker' && (
        <>
          <div className="-mt-3 -mb-3 -mx-[var(--app-canvas-gutter-x)] flex min-h-[46px] flex-wrap items-center gap-2 px-[var(--app-tab-gutter-x)] py-2 cq-lg:justify-between cq-sm:-mt-4 cq-sm:-mb-4">
            <div className={SERVER_SECONDARY_TABS_GROUP_CLASS}>
              <Tabs
                {...TOOL_TABS_PROPS}
                value={dockerSubTab}
                onValueChange={setDockerSubTab}
                tabs={[
                  {
                    value: 'containers',
                    label: (
                      <span className="inline-flex items-center gap-1.5">
                        <Box className="w-3.5 h-3.5" />
                        容器
                      </span>
                    ),
                  },
                  {
                    value: 'compose',
                    label: (
                      <span className="inline-flex items-center gap-1.5">
                        <FolderOpen className="w-3.5 h-3.5" />
                        Compose
                      </span>
                    ),
                  },
                  {
                    value: 'images',
                    label: (
                      <span className="inline-flex items-center gap-1.5">
                        <HardDrive className="w-3.5 h-3.5" />
                        镜像
                      </span>
                    ),
                  },
                  {
                    value: 'networks',
                    label: (
                      <span className="inline-flex items-center gap-1.5">
                        <Globe className="w-3.5 h-3.5" />
                        网络
                      </span>
                    ),
                  },
                  {
                    value: 'volumes',
                    label: (
                      <span className="inline-flex items-center gap-1.5">
                        <HardDrive className="w-3.5 h-3.5" />
                        存储卷
                      </span>
                    ),
                  },
                ]}
              />
            </div>

            <div className="flex min-w-0 flex-wrap items-center justify-start gap-2 cq-lg:justify-end">
              {dockerSubTab === 'containers' && (
                <>
                  <Input
                    size="sm"
                    aria-label="搜索 Docker 容器"
                    value={dockerSearchQuery}
                    onChange={event => setDockerSearchQuery(event.target.value)}
                    placeholder="搜索容器 / 镜像 / 端口"
                    className="h-6.5 w-full min-w-[12rem] cq-sm:w-52"
                  />
                  <Select
                    alignItemWithTrigger
                    aria-label="筛选容器状态"
                    size="sm"
                    value={dockerContainerStateFilter}
                    onValueChange={value => setDockerContainerStateFilter(String(value))}
                    className="h-6.5 w-28"
                    items={[
                      { value: 'all', label: '全部状态' },
                      { value: 'running', label: '运行' },
                      { value: 'paused', label: '暂停' },
                      { value: 'stopped', label: '停止' },
                      { value: 'updatable', label: '可更新' },
                    ]}
                  />
                </>
              )}
              <Button
                shape="square"
                size="sm"
                variant="secondary"
                icon={
                  <RefreshCw
                    className={`h-3.5 w-3.5 ${dockerResourceLoading ? 'animate-spin' : ''}`}
                  />
                }
                disabled={dockerResourceLoading}
                aria-label="刷新 Docker 数据"
                title="刷新 Docker 数据"
                onClick={loadDockerResources}
              />
              {dockerSubTab === 'containers' && (
                <Button
                  size="sm"
                  variant="secondary"
                  icon={
                    <RefreshCw
                      className={`h-3.5 w-3.5 ${dockerBulkUpdateChecking ? 'animate-spin' : ''}`}
                    />
                  }
                  disabled={dockerBulkUpdateChecking || visibleDockerContainerServers.length === 0}
                  onClick={checkVisibleDockerUpdates}
                >
                  检测可更新
                </Button>
              )}
              {dockerSubTab === 'containers' && (
                <Button
                  size="sm"
                  variant={
                    isDockerUpdateConfirmActive(dockerActiveBatchUpdateConfirmKey)
                      ? 'secondary-destructive'
                      : 'primary'
                  }
                  icon={<Upload className="h-3.5 w-3.5" />}
                  disabled={dockerActiveBatchUpdateTargets.length === 0}
                  onClick={() =>
                    batchUpdateVisibleDockerContainers('', false, dockerActiveBatchUpdateTargets, {
                      scopeName: dockerActiveBatchUpdateScopeName,
                      confirmKey: dockerActiveBatchUpdateConfirmKey,
                    })
                  }
                  title={
                    dockerActiveBatchUpdateTargets.length > 0
                      ? `${dockerActiveBatchUpdateScopeName}下可更新的容器`
                      : '请先检测出可更新容器'
                  }
                >
                  {isDockerUpdateConfirmActive(dockerActiveBatchUpdateConfirmKey)
                    ? '再次确认'
                    : selectedUpdatableDockerContainers.length > 0
                      ? `更新选中 ${selectedUpdatableDockerContainers.length}`
                      : `一键更新 ${visibleUpdatableDockerContainers.length > 0 ? visibleUpdatableDockerContainers.length : ''}`}
                </Button>
              )}
            </div>
          </div>

          {/* Docker 任务中心 */}
          {dockerSubTab !== 'containers' &&
            dockerSubTab === 'task-center' &&
            dockerTasks.length > 0 && (
              <AppCard padding="none" className="bg-kumo-recessed p-2.5 text-xs text-kumo-default">
                {(() => {
                  const latestTask = dockerTasks[0];
                  const progress = clampPercent(toNumber(latestTask.progress, 0));
                  const showProgress =
                    !['success', 'failed', 'timeout', 'cancelled'].includes(latestTask.state) &&
                    progress > 0;
                  const stateVariant =
                    latestTask.state === 'success'
                      ? 'success'
                      : latestTask.state === 'failed'
                        ? 'error'
                        : 'warning';
                  return (
                    <>
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="flex min-w-0 items-center gap-2">
                          <Activity className="h-3.5 w-3.5 shrink-0 text-brand" />
                          <span className="shrink-0 font-semibold text-brand">Docker 任务</span>
                          <Badge variant={stateVariant} appearance="dot">
                            {getDockerTaskStateLabel(latestTask.state)}
                          </Badge>
                          <span className="min-w-0 truncate text-kumo-subtle">
                            {getDockerTaskActionLabel(latestTask.action)}
                          </span>
                          <span className="min-w-0 truncate text-kumo-subtle">
                            {summarizeDockerTaskMessage(latestTask)}
                          </span>
                        </div>
                        <div className="flex items-center gap-2">
                          <Badge
                            variant={dockerTaskStreamConnected ? 'success' : 'warning'}
                            appearance="dot"
                          >
                            {dockerTaskStreamConnected ? '实时连接' : '重连中'}
                          </Badge>
                          <Button
                            size="sm"
                            variant="secondary"
                            icon={
                              showDockerTaskDetails ? (
                                <ChevronUp className="h-3.5 w-3.5" />
                              ) : (
                                <ChevronDown className="h-3.5 w-3.5" />
                              )
                            }
                            onClick={() => setShowDockerTaskDetails(v => !v)}
                          >
                            {showDockerTaskDetails ? '收起' : `详情 ${dockerTasks.length}`}
                          </Button>
                        </div>
                      </div>
                      {showProgress && (
                        <Meter
                          label="Docker 任务进度"
                          value={progress}
                          showValue={false}
                          className="mt-2 gap-0"
                          trackClassName="!h-1 overflow-hidden rounded-full bg-kumo-base"
                          indicatorClassName="!h-full !bg-none !bg-brand"
                        />
                      )}
                    </>
                  );
                })()}
                <AnimatedCollapse open={showDockerTaskDetails} keepMounted>
                  <div className="mt-2 max-h-36 overflow-y-auto border-t border-kumo-line pt-2">
                    <div className="flex flex-col gap-1.5">
                      {dockerTasks.slice(0, 12).map(t => {
                        const progress = clampPercent(toNumber(t.progress, 0));
                        const showProgress =
                          !['success', 'failed', 'timeout', 'cancelled'].includes(t.state) &&
                          progress > 0;
                        const stateVariant =
                          t.state === 'success'
                            ? 'success'
                            : t.state === 'failed'
                              ? 'error'
                              : 'warning';
                        return (
                          <div
                            key={t.taskId}
                            className="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)_minmax(0,1.25fr)] items-center gap-2 text-[11px]"
                          >
                            <Badge variant={stateVariant} appearance="dot">
                              {getDockerTaskStateLabel(t.state)}
                            </Badge>
                            <span
                              className="truncate font-semibold text-kumo-strong"
                              title={getDockerTaskActionLabel(t.action)}
                            >
                              {getDockerTaskActionLabel(t.action)}
                            </span>
                            <span
                              className="truncate text-kumo-subtle"
                              title={String(t.message || '')}
                            >
                              {showProgress ? `${progress}% · ` : ''}
                              {summarizeDockerTaskMessage(t)}
                            </span>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                </AnimatedCollapse>
              </AppCard>
            )}

          {/* 内容区域 */}
          {showDockerBlockingLoading ? (
            <AppCard padding="none" className="overflow-hidden p-4">
              <div className="space-y-4">
                {[...Array(3)].map((_, i) => (
                  <div key={i} className="space-y-2">
                    <SkeletonLine className="w-1/4 h-5" />
                    <SkeletonLine className="w-full h-12 rounded-lg" />
                  </div>
                ))}
              </div>
            </AppCard>
          ) : (
            <div className="flex flex-col gap-4">
              {/* 1. 容器管理 */}
              {dockerSubTab === 'containers' && (
                <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                  <div className="flex min-w-0 flex-col gap-3 cq-xl:order-2">
                    {dockerOverviewServers.length === 0
                      ? renderDockerEmptyState('未检测到可用的 Docker 主机')
                      : visibleDockerContainerServers.length === 0
                        ? renderDockerEmptyState('当前筛选下没有容器')
                        : visibleDockerContainerServers.map(server => {
                            const summary = getDockerContainerSummary(
                              server.resources?.containers,
                              server.id
                            );
                            const isOpen = isDockerHostExpanded(
                              server.id,
                              visibleDockerContainerServers[0]?.id
                            );
                            const hostUpdateTargets = getUpdatableDockerContainers(
                              [server],
                              server.id
                            );
                            const hostUpdateConfirmKey = `host.update::${server.id}::${hostUpdateTargets.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`;
                            return (
                              <div
                                key={server.id}
                                className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base"
                              >
                                <div
                                  role="button"
                                  tabIndex={0}
                                  onClick={() =>
                                    toggleDockerOverviewServer(
                                      server.id,
                                      visibleDockerContainerServers[0]?.id
                                    )
                                  }
                                  onKeyDown={event => {
                                    if (event.key === 'Enter' || event.key === ' ') {
                                      event.preventDefault();
                                      toggleDockerOverviewServer(
                                        server.id,
                                        visibleDockerContainerServers[0]?.id
                                      );
                                    }
                                  }}
                                  className="flex min-h-[52px] cursor-pointer flex-wrap items-center justify-between gap-2 border-b border-kumo-line/70 px-3 py-3.5"
                                >
                                  <div className="flex min-w-0 items-center gap-2">
                                    <Box className="h-4 w-4 shrink-0 text-brand" />
                                    <span className="truncate text-xs font-semibold text-kumo-strong">
                                      {server.name}
                                    </span>
                                    {renderDockerFilterChip(
                                      server.id,
                                      'all',
                                      `${summary.total} 容器`,
                                      'neutral',
                                      `${server.name} 全部容器`
                                    )}
                                    {renderDockerFilterChip(
                                      server.id,
                                      'running',
                                      `${summary.running} 运行`,
                                      'success',
                                      `${server.name} 运行容器`
                                    )}
                                    {summary.paused > 0 &&
                                      renderDockerFilterChip(
                                        server.id,
                                        'paused',
                                        `${summary.paused} 暂停`,
                                        'warning',
                                        `${server.name} 暂停容器`
                                      )}
                                    {summary.stopped > 0 &&
                                      renderDockerFilterChip(
                                        server.id,
                                        'stopped',
                                        `${summary.stopped} 停止`,
                                        'error',
                                        `${server.name} 停止容器`
                                      )}
                                    {summary.updatable > 0 &&
                                      renderDockerFilterChip(
                                        server.id,
                                        'updatable',
                                        `${summary.updatable} 可更新`,
                                        'warning',
                                        `${server.name} 可更新容器`
                                      )}
                                  </div>
                                  <div className="flex items-center gap-1.5">
                                    {summary.updatable > 0 && (
                                      <Button
                                        size="sm"
                                        variant={
                                          isDockerUpdateConfirmActive(hostUpdateConfirmKey)
                                            ? 'secondary-destructive'
                                            : 'primary'
                                        }
                                        icon={<Upload className="h-3.5 w-3.5" />}
                                        onClick={event => {
                                          event.stopPropagation();
                                          batchUpdateVisibleDockerContainers(
                                            server.id,
                                            false,
                                            hostUpdateTargets,
                                            {
                                              confirmKey: hostUpdateConfirmKey,
                                            }
                                          );
                                        }}
                                      >
                                        {isDockerUpdateConfirmActive(hostUpdateConfirmKey)
                                          ? '再次确认'
                                          : '一键更新'}
                                      </Button>
                                    )}
                                    <Button
                                      size="sm"
                                      variant="secondary"
                                      icon={
                                        isOpen ? (
                                          <ChevronUp className="h-3.5 w-3.5" />
                                        ) : (
                                          <ChevronDown className="h-3.5 w-3.5" />
                                        )
                                      }
                                      onClick={event => {
                                        event.stopPropagation();
                                        toggleDockerOverviewServer(
                                          server.id,
                                          visibleDockerContainerServers[0]?.id
                                        );
                                      }}
                                    >
                                      {isOpen ? '收起' : '展开'}
                                    </Button>
                                  </div>
                                </div>

                                <AnimatedCollapse open={isOpen} keepMounted>
                                  {server.resources.containers.length === 0 ? (
                                    <div className="p-8 text-center text-xs text-kumo-subtle">
                                      暂无容器
                                    </div>
                                  ) : (
                                    <div className="divide-y divide-kumo-line">
                                      {server.resources.containers.map(c => {
                                        const state = getDockerContainerState(c);
                                        const stateBadge = getDockerStateBadge(state);
                                        const containerId = getDockerContainerId(c);
                                        const containerName = getDockerContainerName(c);
                                        const containerImage = getDockerContainerImage(c);
                                        const containerPorts = getDockerContainerPorts(c);
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
                                            ? 'container.stop'
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
                                        const selectionKey = getDockerContainerSelectionKey(
                                          server.id,
                                          updatePayload
                                        );
                                        const canSelectForUpdate =
                                          !!updateCheck?.hasUpdate && !updatePending;
                                        return (
                                          <div
                                            key={containerId || `${server.id}-${containerName}`}
                                            className="flex min-w-0 items-center gap-3 px-3 py-2.5 cq-sm:px-4"
                                          >
                                            <Checkbox
                                              checked={dockerSelectedContainerKeySet.has(
                                                selectionKey
                                              )}
                                              disabled={!canSelectForUpdate}
                                              onCheckedChange={checked =>
                                                toggleDockerContainerSelection(
                                                  selectionKey,
                                                  Boolean(checked)
                                                )
                                              }
                                              aria-label={`选择更新 ${containerName}`}
                                            />
                                            <div className="min-w-0 flex-1">
                                              <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                                                <span
                                                  className="min-w-0 truncate text-xs font-semibold leading-5 text-kumo-strong"
                                                  title={containerName}
                                                >
                                                  {containerName}
                                                </span>
                                                <span
                                                  className="min-w-0 truncate font-mono text-[10px] text-kumo-subtle"
                                                  title={containerImage}
                                                >
                                                  {containerImage}
                                                </span>
                                              </div>
                                              <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-kumo-subtle">
                                                <Badge
                                                  variant={stateBadge.variant}
                                                  appearance="dot"
                                                >
                                                  {stateBadge.label}
                                                </Badge>
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
                                                {containerPorts && (
                                                  <span
                                                    className="truncate font-mono"
                                                    title={containerPorts}
                                                  >
                                                    {containerPorts}
                                                  </span>
                                                )}
                                              </div>
                                              {updatePending && updateProgress > 0 && (
                                                <Meter
                                                  label={`${containerName} 更新进度`}
                                                  value={updateProgress}
                                                  showValue={false}
                                                  className="mt-1.5 w-28 gap-0"
                                                  trackClassName="!h-1 overflow-hidden rounded-full bg-kumo-recessed"
                                                  indicatorClassName="!h-full !bg-none !bg-brand"
                                                />
                                              )}
                                            </div>
                                            <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
                                              <Button
                                                shape="square"
                                                size="sm"
                                                variant={
                                                  state === 'running'
                                                    ? 'secondary-destructive'
                                                    : 'secondary'
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
                                                aria-label={
                                                  state === 'running' ? '停止容器' : '启动容器'
                                                }
                                                disabled={
                                                  togglePending || restartPending || updatePending
                                                }
                                                onClick={() =>
                                                  submitDockerTask(toggleAction, togglePayload)
                                                }
                                                title={state === 'running' ? '停止' : '启动'}
                                              />
                                              <Button
                                                shape="square"
                                                size="sm"
                                                variant="secondary"
                                                icon={
                                                  restartPending ? (
                                                    <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                  ) : (
                                                    <RotateCw className="h-3.5 w-3.5" />
                                                  )
                                                }
                                                aria-label="重启容器"
                                                disabled={
                                                  togglePending || restartPending || updatePending
                                                }
                                                onClick={() =>
                                                  submitDockerTask(
                                                    'container.restart',
                                                    restartPayload
                                                  )
                                                }
                                                title="重启"
                                              />
                                              <Button
                                                shape="square"
                                                size="sm"
                                                variant="secondary"
                                                icon={
                                                  updateChecking ? (
                                                    <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                  ) : (
                                                    <Search className="h-3.5 w-3.5" />
                                                  )
                                                }
                                                aria-label="检测镜像更新"
                                                disabled={updateChecking}
                                                onClick={() =>
                                                  checkDockerUpdatesForServer(server, c)
                                                }
                                                title="检测更新"
                                              />
                                              <Button
                                                shape="square"
                                                size="sm"
                                                variant={
                                                  isDockerUpdateConfirmActive(updateConfirmKey)
                                                    ? 'secondary-destructive'
                                                    : 'primary'
                                                }
                                                icon={
                                                  updatePending ? (
                                                    <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                  ) : (
                                                    <Upload className="h-3.5 w-3.5" />
                                                  )
                                                }
                                                aria-label={
                                                  isDockerUpdateConfirmActive(updateConfirmKey)
                                                    ? '再次确认更新容器'
                                                    : '一键更新容器'
                                                }
                                                disabled={
                                                  togglePending || restartPending || updatePending
                                                }
                                                onClick={() =>
                                                  handleDockerContainerUpdate(updatePayload)
                                                }
                                                title={
                                                  isDockerUpdateConfirmActive(updateConfirmKey)
                                                    ? '再次点击确认更新'
                                                    : '一键更新'
                                                }
                                              />
                                            </div>
                                          </div>
                                        );
                                      })}
                                    </div>
                                  )}
                                </AnimatedCollapse>
                              </div>
                            );
                          })}
                  </div>

                  <div className="flex min-w-0 flex-col gap-3 cq-xl:order-1 cq-xl:sticky cq-xl:top-0 cq-xl:self-start">
                    {renderDockerSimpleLogCard('order-3')}

                    <LayerCard className="order-1 overflow-hidden p-0">
                      <LayerCard.Secondary className={SERVER_SECTION_HEADER_CLASS}>
                        <span className="inline-flex min-w-0 items-center gap-2 text-xs font-semibold text-kumo-strong">
                          <Settings className="h-4 w-4 shrink-0 text-brand" />
                          管理
                        </span>
                        <span className="flex shrink-0 items-center gap-1.5">
                          <Badge variant="neutral">
                            {dockerContainerManagementServers.length} 主机
                          </Badge>
                          <Button
                            shape="square"
                            size="sm"
                            variant="secondary"
                            icon={
                              <RefreshCw
                                className={`h-3.5 w-3.5 ${dockerBulkUpdateChecking ? 'animate-spin' : ''}`}
                              />
                            }
                            disabled={
                              dockerBulkUpdateChecking ||
                              dockerContainerManagementServers.length === 0
                            }
                            onClick={checkAllDockerUpdates}
                            aria-label="检测可更新"
                            title="检测全部主机可更新"
                          />
                          <Button
                            shape="square"
                            size="sm"
                            variant={
                              isDockerUpdateConfirmActive(dockerManagementBatchUpdateConfirmKey)
                                ? 'secondary-destructive'
                                : 'primary'
                            }
                            icon={<Upload className="h-3.5 w-3.5" />}
                            disabled={dockerContainerManagementUpdatableContainers.length === 0}
                            onClick={() =>
                              batchUpdateVisibleDockerContainers('', true, null, {
                                confirmKey: dockerManagementBatchUpdateConfirmKey,
                              })
                            }
                            aria-label={
                              isDockerUpdateConfirmActive(dockerManagementBatchUpdateConfirmKey)
                                ? '再次确认一键更新'
                                : '一键更新'
                            }
                            title={
                              dockerContainerManagementUpdatableContainers.length > 0
                                ? isDockerUpdateConfirmActive(dockerManagementBatchUpdateConfirmKey)
                                  ? '再次点击确认更新全部主机'
                                  : `一键更新全部主机 ${dockerContainerManagementUpdatableContainers.length} 个容器`
                                : '请先检测出可更新容器'
                            }
                          />
                        </span>
                      </LayerCard.Secondary>
                      <LayerCard.Primary className="space-y-3 p-3">
                        <div className="grid grid-cols-4 gap-1.5">
                          <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                            <div className="text-[10px] text-kumo-subtle">容器</div>
                            <div className="mt-0.5 text-sm font-semibold text-kumo-strong">
                              {dockerContainerManagementTotals.total}
                            </div>
                          </div>
                          <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                            <div className="text-[10px] text-kumo-subtle">运行</div>
                            <div className="mt-0.5 text-sm font-semibold text-kumo-success">
                              {dockerContainerManagementTotals.running}
                            </div>
                          </div>
                          <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                            <div className="text-[10px] text-kumo-subtle">停止</div>
                            <div className="mt-0.5 text-sm font-semibold text-kumo-subtle">
                              {dockerContainerManagementTotals.stopped}
                            </div>
                          </div>
                          <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                            <div className="text-[10px] text-kumo-subtle">更新</div>
                            <div className="mt-0.5 text-sm font-semibold text-kumo-warning">
                              {dockerContainerManagementTotals.updatable}
                            </div>
                          </div>
                        </div>

                        <div className="flex flex-col gap-2">
                          {dockerContainerManagementServers.map(server => {
                            const summary = getDockerContainerSummary(
                              server.resources?.containers,
                              server.id
                            );
                            const isOpen = isDockerHostExpanded(
                              server.id,
                              dockerContainerManagementServers[0]?.id
                            );
                            const managementHostUpdateTargets = getUpdatableDockerContainers(
                              [server],
                              server.id
                            );
                            const managementHostUpdateConfirmKey = `management.host.update::${server.id}::${managementHostUpdateTargets.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`;
                            return (
                              <div
                                key={`management-${server.id}`}
                                className={`overflow-hidden rounded-md border ${isOpen ? 'border-brand/55 bg-brand/5' : 'border-kumo-line/80 bg-kumo-base'}`}
                              >
                                <div
                                  role="button"
                                  tabIndex={0}
                                  onClick={() =>
                                    toggleDockerOverviewServer(
                                      server.id,
                                      dockerContainerManagementServers[0]?.id
                                    )
                                  }
                                  onKeyDown={event => {
                                    if (event.key === 'Enter' || event.key === ' ') {
                                      event.preventDefault();
                                      toggleDockerOverviewServer(
                                        server.id,
                                        dockerContainerManagementServers[0]?.id
                                      );
                                    }
                                  }}
                                  className="flex min-h-10 cursor-pointer items-center justify-between gap-2 px-2.5 py-2"
                                >
                                  <span className="flex min-w-0 items-center gap-2">
                                    <Box className="h-3.5 w-3.5 shrink-0 text-brand" />
                                    <span className="min-w-0 truncate text-xs font-semibold text-kumo-strong">
                                      {server.name}
                                    </span>
                                  </span>
                                  <span className="flex shrink-0 items-center gap-1.5">
                                    {renderDockerFilterChip(
                                      server.id,
                                      'all',
                                      String(summary.total),
                                      'neutral',
                                      `${server.name} 全部容器`
                                    )}
                                    {summary.updatable > 0 &&
                                      renderDockerFilterChip(
                                        server.id,
                                        'updatable',
                                        String(summary.updatable),
                                        'warning',
                                        `${server.name} 可更新容器`
                                      )}
                                  </span>
                                </div>

                                <AnimatedCollapse open={isOpen} keepMounted>
                                  <div className="border-t border-kumo-line/70 px-2.5 py-2">
                                    <div className="flex flex-wrap items-center justify-between gap-2">
                                      <div className="flex flex-wrap gap-1.5">
                                        {renderDockerFilterChip(
                                          server.id,
                                          'running',
                                          `${summary.running} 运行`,
                                          'success',
                                          `${server.name} 运行容器`
                                        )}
                                        {summary.paused > 0 &&
                                          renderDockerFilterChip(
                                            server.id,
                                            'paused',
                                            `${summary.paused} 暂停`,
                                            'warning',
                                            `${server.name} 暂停容器`
                                          )}
                                        {summary.stopped > 0 &&
                                          renderDockerFilterChip(
                                            server.id,
                                            'stopped',
                                            `${summary.stopped} 停止`,
                                            'error',
                                            `${server.name} 停止容器`
                                          )}
                                        {summary.updatable > 0 &&
                                          renderDockerFilterChip(
                                            server.id,
                                            'updatable',
                                            `${summary.updatable} 可更新`,
                                            'warning',
                                            `${server.name} 可更新容器`
                                          )}
                                      </div>
                                      <Button
                                        shape="square"
                                        size="sm"
                                        variant={
                                          isDockerUpdateConfirmActive(
                                            managementHostUpdateConfirmKey
                                          )
                                            ? 'secondary-destructive'
                                            : 'primary'
                                        }
                                        icon={<Upload className="h-3.5 w-3.5" />}
                                        disabled={summary.updatable === 0}
                                        onClick={event => {
                                          event.stopPropagation();
                                          batchUpdateVisibleDockerContainers(
                                            server.id,
                                            false,
                                            managementHostUpdateTargets,
                                            {
                                              confirmKey: managementHostUpdateConfirmKey,
                                            }
                                          );
                                        }}
                                        aria-label={
                                          isDockerUpdateConfirmActive(
                                            managementHostUpdateConfirmKey
                                          )
                                            ? `再次确认更新 ${server.name}`
                                            : `更新 ${server.name} 可更新容器`
                                        }
                                        title={
                                          summary.updatable > 0
                                            ? isDockerUpdateConfirmActive(
                                                managementHostUpdateConfirmKey
                                              )
                                              ? `再次点击确认更新 ${server.name}`
                                              : `更新 ${server.name} 的 ${summary.updatable} 个可更新容器`
                                            : `${server.name} 暂无可更新容器`
                                        }
                                      />
                                    </div>
                                  </div>
                                </AnimatedCollapse>
                              </div>
                            );
                          })}
                        </div>
                      </LayerCard.Primary>
                    </LayerCard>
                  </div>
                </div>
              )}

              {/* 2. Compose */}
              {dockerSubTab === 'compose' &&
                (() => {
                  const hosts = dockerOverviewServers.filter(server =>
                    isDockerOverviewHostVisible(server, 'compose')
                  );
                  const runningProjects = dockerComposeProjects.filter(item =>
                    getComposeStatus(item).includes('running')
                  ).length;
                  return (
                    <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                      {renderDockerResourceSideRail({
                        title: 'Compose 项目',
                        icon: <FolderOpen className="h-4 w-4 shrink-0 text-brand" />,
                        hosts,
                        totalCount: dockerComposeProjects.length,
                        countLabel: '项目',
                        summaryItems: [
                          { label: '运行', value: runningProjects, className: 'text-kumo-success' },
                          {
                            label: '停止',
                            value: Math.max(0, dockerComposeProjects.length - runningProjects),
                            className: 'text-kumo-subtle',
                          },
                        ],
                        getHostCount: server => asArray(server.resources?.composeProjects).length,
                        getHostBadges: server => {
                          const projects = asArray(server.resources?.composeProjects);
                          const running = projects.filter(project =>
                            getComposeStatus(project).toLowerCase().includes('running')
                          ).length;
                          return running > 0
                            ? [{ label: `${running} 运行`, variant: 'success', appearance: 'dot' }]
                            : [];
                        },
                      })}
                      <div className="flex min-w-0 flex-col gap-3">
                        {hosts.length === 0
                          ? renderDockerEmptyState('未检测到可用的 Docker 主机')
                          : hosts.map(server => {
                              const projects = asArray(server.resources?.composeProjects);
                              const running = projects.filter(project =>
                                getComposeStatus(project).toLowerCase().includes('running')
                              ).length;
                              return renderDockerHostResourceSection({
                                server,
                                isFirstVisible: server.id === hosts[0]?.id,
                                icon: <FolderOpen className="h-4 w-4 shrink-0 text-brand" />,
                                count: projects.length,
                                countLabel: '项目',
                                badges:
                                  running > 0
                                    ? [
                                        {
                                          label: `${running} 运行`,
                                          variant: 'success',
                                          appearance: 'dot',
                                        },
                                      ]
                                    : [],
                                children:
                                  projects.length === 0 ? (
                                    <div className="p-8 text-center text-xs text-kumo-subtle">
                                      暂无 Compose 项目
                                    </div>
                                  ) : (
                                    <div className="divide-y divide-kumo-line">
                                      {projects.map(proj => {
                                        const projectName = getComposeProjectName(proj);
                                        const configFiles = getComposeConfigFiles(proj);
                                        const primaryConfigFile = getComposePrimaryConfigFile(proj);
                                        const workingDir = getComposeWorkingDir(proj);
                                        const status = getComposeStatus(proj);
                                        const statusLower = status.toLowerCase();
                                        const statusLabel = formatComposeStatusLabel(status);
                                        const composePayload = {
                                          serverId: server.id,
                                          project: projectName,
                                          config_file: configFiles,
                                        };
                                        const composeUpPending = isDockerActionPending(
                                          server.id,
                                          'compose.up',
                                          composePayload
                                        );
                                        const composeDownPending = isDockerActionPending(
                                          server.id,
                                          'compose.down',
                                          composePayload
                                        );
                                        const composeRestartPending = isDockerActionPending(
                                          server.id,
                                          'compose.restart',
                                          composePayload
                                        );
                                        const composePullPending = isDockerActionPending(
                                          server.id,
                                          'compose.pull',
                                          composePayload
                                        );
                                        const composeBusy =
                                          composeUpPending ||
                                          composeDownPending ||
                                          composeRestartPending ||
                                          composePullPending;
                                        return (
                                          <div
                                            key={`${server.id}-${projectName}-${configFiles}`}
                                            className="flex min-w-0 items-center gap-3 px-3 py-2.5 cq-sm:px-4"
                                          >
                                            <div className="min-w-0 flex-1">
                                              <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                                                <span className="truncate text-xs font-semibold leading-5 text-kumo-strong">
                                                  {projectName}
                                                </span>
                                                <Badge
                                                  variant={
                                                    statusLower.includes('running')
                                                      ? 'success'
                                                      : 'error'
                                                  }
                                                  appearance="dot"
                                                >
                                                  {statusLabel}
                                                </Badge>
                                                {isComposeConfigMissing(proj) && (
                                                  <Badge
                                                    variant="warning"
                                                    appearance="dot"
                                                    title="配置文件路径已失效，查看/修改配置会失败；可执行 down 或忽略该项目"
                                                  >
                                                    配置缺失
                                                  </Badge>
                                                )}
                                              </div>
                                              <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-kumo-subtle">
                                                <span
                                                  className="min-w-0 truncate font-mono"
                                                  title={configFiles}
                                                >
                                                  配置: {configFiles || '-'}
                                                </span>
                                                <span
                                                  className="min-w-0 truncate font-mono"
                                                  title={workingDir}
                                                >
                                                  目录: {workingDir || '-'}
                                                </span>
                                              </div>
                                            </div>
                                            <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
                                              <Button
                                                shape="square"
                                                size="sm"
                                                variant="secondary"
                                                icon={<FileText className="h-3.5 w-3.5" />}
                                                onClick={() =>
                                                  openDockerComposeConfig(server, proj, 'view')
                                                }
                                                disabled={!primaryConfigFile}
                                                aria-label={`查看 ${projectName} 配置`}
                                                title={
                                                  primaryConfigFile ? '查看配置' : '未找到配置文件'
                                                }
                                              />
                                              <Button
                                                shape="square"
                                                size="sm"
                                                variant="secondary"
                                                icon={<Edit className="h-3.5 w-3.5" />}
                                                onClick={() =>
                                                  openDockerComposeConfig(server, proj, 'edit')
                                                }
                                                disabled={!primaryConfigFile}
                                                aria-label={`修改 ${projectName} 配置`}
                                                title={
                                                  primaryConfigFile ? '修改配置' : '未找到配置文件'
                                                }
                                              />
                                              <Button
                                                shape="square"
                                                size="sm"
                                                variant="secondary"
                                                icon={
                                                  composePullPending ? (
                                                    <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                  ) : (
                                                    <Upload className="h-3.5 w-3.5" />
                                                  )
                                                }
                                                disabled={composeBusy}
                                                onClick={() =>
                                                  submitDockerTask('compose.pull', composePayload)
                                                }
                                                aria-label={`升级 ${projectName}`}
                                                title="升级镜像"
                                              />
                                              <Button
                                                shape="square"
                                                size="sm"
                                                variant="secondary"
                                                icon={
                                                  composeRestartPending ? (
                                                    <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                  ) : (
                                                    <RotateCw className="h-3.5 w-3.5" />
                                                  )
                                                }
                                                disabled={composeBusy}
                                                onClick={() =>
                                                  submitDockerTask(
                                                    'compose.restart',
                                                    composePayload
                                                  )
                                                }
                                                aria-label={`重启 ${projectName}`}
                                                title="重启项目"
                                              />
                                              <Button
                                                shape="square"
                                                size="sm"
                                                variant="primary"
                                                icon={
                                                  composeUpPending ? (
                                                    <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                  ) : (
                                                    <Play className="h-3.5 w-3.5" />
                                                  )
                                                }
                                                disabled={composeBusy}
                                                onClick={() =>
                                                  submitDockerTask('compose.up', composePayload)
                                                }
                                                aria-label={`启动 ${projectName}`}
                                                title="启动项目"
                                              />
                                              <Button
                                                shape="square"
                                                size="sm"
                                                variant="secondary"
                                                icon={
                                                  composeDownPending ? (
                                                    <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                                  ) : (
                                                    <Square className="h-3.5 w-3.5" />
                                                  )
                                                }
                                                disabled={composeBusy}
                                                onClick={() =>
                                                  submitDockerTask('compose.down', composePayload)
                                                }
                                                aria-label={`停止 ${projectName}`}
                                                title="停止项目"
                                              />
                                            </div>
                                          </div>
                                        );
                                      })}
                                    </div>
                                  ),
                              });
                            })}
                      </div>
                    </div>
                  );
                })()}

              {/* 3. 镜像管理 */}
              {dockerSubTab === 'images' &&
                (() => {
                  const hosts = dockerOverviewServers.filter(server =>
                    isDockerOverviewHostVisible(server, 'images')
                  );
                  const allImagePruneConfirmKey = `image.prune.all::${hosts.map(server => server.id).join('|')}`;
                  const pruneCandidateCount = dockerImages.filter(
                    isDockerImagePruneCandidate
                  ).length;
                  return (
                    <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                      {renderDockerResourceSideRail({
                        title: '镜像',
                        icon: <HardDrive className="h-4 w-4 shrink-0 text-brand" />,
                        hosts,
                        totalCount: dockerImages.length,
                        countLabel: '镜像',
                        summaryItems: [
                          {
                            label: '仓库',
                            value: new Set(dockerImages.map(item => getDockerImageRepository(item)))
                              .size,
                          },
                          {
                            label: '可清理',
                            value: pruneCandidateCount,
                            className:
                              pruneCandidateCount > 0 ? 'text-kumo-warning' : 'text-kumo-success',
                          },
                        ],
                        actions: (
                          <Button
                            size="sm"
                            variant={
                              isDockerUpdateConfirmActive(allImagePruneConfirmKey)
                                ? 'secondary-destructive'
                                : 'primary'
                            }
                            icon={<Trash className="h-3.5 w-3.5" />}
                            disabled={hosts.length === 0}
                            onClick={() =>
                              pruneDockerImagesForHosts(hosts, {
                                confirmKey: allImagePruneConfirmKey,
                              })
                            }
                            aria-label={
                              isDockerUpdateConfirmActive(allImagePruneConfirmKey)
                                ? '再次确认一键清理镜像'
                                : '一键清理镜像'
                            }
                            title={
                              isDockerUpdateConfirmActive(allImagePruneConfirmKey)
                                ? '再次点击确认清理全部主机镜像'
                                : '一键清理全部主机未使用镜像'
                            }
                          >
                            {isDockerUpdateConfirmActive(allImagePruneConfirmKey)
                              ? '再次确认'
                              : '一键清理'}
                          </Button>
                        ),
                        getHostCount: server => asArray(server.resources?.images).length,
                        getHostBadges: server => {
                          const images = asArray(server.resources?.images);
                          const pruneCandidates = images.filter(isDockerImagePruneCandidate).length;
                          return images.length > 0
                            ? [
                                {
                                  label: `${new Set(images.map(img => getDockerImageRepository(img))).size} 仓库`,
                                },
                                ...(pruneCandidates > 0
                                  ? [{ label: `${pruneCandidates} 可清理`, variant: 'warning' }]
                                  : []),
                              ]
                            : [];
                        },
                        renderHostAction: server => {
                          const prunePayload = { serverId: server.id };
                          const prunePending = isDockerActionPending(
                            server.id,
                            'image.prune',
                            prunePayload
                          );
                          const pruneConfirmKey = `image.prune::${server.id}`;
                          return (
                            <Button
                              shape="square"
                              size="sm"
                              variant={
                                isDockerUpdateConfirmActive(pruneConfirmKey)
                                  ? 'secondary-destructive'
                                  : 'secondary'
                              }
                              icon={
                                prunePending ? (
                                  <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                ) : (
                                  <Trash className="h-3.5 w-3.5" />
                                )
                              }
                              disabled={prunePending}
                              onClick={() =>
                                submitDockerPruneTask(
                                  'image.prune',
                                  prunePayload,
                                  pruneConfirmKey,
                                  `清理 ${server.name} 未使用镜像`
                                )
                              }
                              aria-label={
                                isDockerUpdateConfirmActive(pruneConfirmKey)
                                  ? `再次确认清理 ${server.name} 镜像`
                                  : `清理 ${server.name} 镜像`
                              }
                              title={
                                isDockerUpdateConfirmActive(pruneConfirmKey)
                                  ? '再次点击确认清理'
                                  : '清理未使用镜像'
                              }
                            />
                          );
                        },
                      })}
                      <div className="flex min-w-0 flex-col gap-3">
                        {hosts.length === 0
                          ? renderDockerEmptyState('未检测到可用的 Docker 主机')
                          : hosts.map(server => {
                              const images = asArray(server.resources?.images);
                              const prunePayload = { serverId: server.id };
                              const prunePending = isDockerActionPending(
                                server.id,
                                'image.prune',
                                prunePayload
                              );
                              const pruneConfirmKey = `image.prune.section::${server.id}`;
                              const pruneCandidates = images.filter(
                                isDockerImagePruneCandidate
                              ).length;
                              return renderDockerHostResourceSection({
                                server,
                                isFirstVisible: server.id === hosts[0]?.id,
                                icon: <HardDrive className="h-4 w-4 shrink-0 text-brand" />,
                                count: images.length,
                                countLabel: '镜像',
                                badges:
                                  images.length > 0
                                    ? [
                                        {
                                          label: `${new Set(images.map(img => getDockerImageRepository(img))).size} 仓库`,
                                        },
                                        ...(pruneCandidates > 0
                                          ? [
                                              {
                                                label: `${pruneCandidates} 可清理`,
                                                variant: 'warning',
                                              },
                                            ]
                                          : []),
                                      ]
                                    : [],
                                actions: (
                                  <Button
                                    size="sm"
                                    variant={
                                      isDockerUpdateConfirmActive(pruneConfirmKey)
                                        ? 'secondary-destructive'
                                        : 'secondary'
                                    }
                                    icon={
                                      prunePending ? (
                                        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                      ) : (
                                        <Trash className="h-3.5 w-3.5" />
                                      )
                                    }
                                    disabled={prunePending}
                                    onClick={event => {
                                      event.stopPropagation();
                                      submitDockerPruneTask(
                                        'image.prune',
                                        prunePayload,
                                        pruneConfirmKey,
                                        `清理 ${server.name} 未使用镜像`
                                      );
                                    }}
                                  >
                                    {isDockerUpdateConfirmActive(pruneConfirmKey)
                                      ? '再次确认'
                                      : '清理镜像'}
                                  </Button>
                                ),
                                children:
                                  images.length === 0 ? (
                                    <div className="p-8 text-center text-xs text-kumo-subtle">
                                      暂无镜像
                                    </div>
                                  ) : (
                                    <div className="divide-y divide-kumo-line">
                                      {images.map((img, i) => {
                                        const repository = getDockerImageRepository(img);
                                        const tag = getDockerImageTag(img);
                                        const imageId = getDockerImageId(img);
                                        const imageRef =
                                          imageId ||
                                          (tag && tag !== '-'
                                            ? `${repository}:${tag}`
                                            : repository);
                                        const removePayload = {
                                          serverId: server.id,
                                          image: imageRef,
                                        };
                                        const removePending = isDockerActionPending(
                                          server.id,
                                          'image.remove',
                                          removePayload
                                        );
                                        return (
                                          <div
                                            key={`${server.id}-${imageId || repository}-${i}`}
                                            className="flex min-w-0 items-center gap-3 px-3 py-2.5 cq-sm:px-4"
                                          >
                                            <div className="min-w-0 flex-1">
                                              <div
                                                className="truncate text-xs font-semibold leading-5 text-kumo-strong"
                                                title={repository}
                                              >
                                                {repository}
                                              </div>
                                              <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-kumo-subtle">
                                                <Badge
                                                  variant="secondary"
                                                  className="font-mono text-[10px]"
                                                >
                                                  {tag}
                                                </Badge>
                                                <span className="shrink-0">
                                                  {getDockerImageSize(img)}
                                                </span>
                                              </div>
                                            </div>
                                            <Button
                                              shape="square"
                                              size="sm"
                                              variant={
                                                isArmed(
                                                  `docker.image.remove::${server.id}::${imageRef}`
                                                )
                                                  ? 'destructive'
                                                  : 'secondary-destructive'
                                              }
                                              aria-label={`删除 ${repository || '镜像'}`}
                                              disabled={removePending}
                                              onClick={() => {
                                                if (
                                                  !confirmPress(
                                                    `docker.image.remove::${server.id}::${imageRef}`,
                                                    `删除镜像「${repository || '镜像'}」`
                                                  )
                                                )
                                                  return;
                                                submitDockerTask('image.remove', removePayload, {
                                                  skipConfirm: true,
                                                });
                                              }}
                                              className="text-kumo-danger"
                                              title="删除镜像"
                                            >
                                              {removePending ? (
                                                <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                              ) : (
                                                <Trash className="h-3.5 w-3.5" />
                                              )}
                                            </Button>
                                          </div>
                                        );
                                      })}
                                    </div>
                                  ),
                              });
                            })}
                      </div>
                    </div>
                  );
                })()}

              {/* 4. 网络管理 */}
              {dockerSubTab === 'networks' &&
                (() => {
                  const hosts = dockerOverviewServers.filter(server =>
                    isDockerOverviewHostVisible(server, 'networks')
                  );
                  const allNetworkPruneConfirmKey = `network.prune.all::${hosts.map(server => server.id).join('|')}`;
                  return (
                    <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                      {renderDockerResourceSideRail({
                        title: '网络',
                        icon: <Globe className="h-4 w-4 shrink-0 text-brand" />,
                        hosts,
                        totalCount: dockerNetworks.length,
                        countLabel: '网络',
                        summaryItems: [
                          {
                            label: '驱动',
                            value: new Set(dockerNetworks.map(item => getDockerNetworkDriver(item)))
                              .size,
                          },
                        ],
                        actions: (
                          <Button
                            size="sm"
                            variant={
                              isDockerUpdateConfirmActive(allNetworkPruneConfirmKey)
                                ? 'secondary-destructive'
                                : 'primary'
                            }
                            icon={<Trash className="h-3.5 w-3.5" />}
                            disabled={hosts.length === 0}
                            onClick={() =>
                              pruneDockerNetworksForHosts(hosts, {
                                confirmKey: allNetworkPruneConfirmKey,
                              })
                            }
                            aria-label={
                              isDockerUpdateConfirmActive(allNetworkPruneConfirmKey)
                                ? '再次确认一键清理网络'
                                : '一键清理网络'
                            }
                            title={
                              isDockerUpdateConfirmActive(allNetworkPruneConfirmKey)
                                ? '再次点击确认清理全部主机网络'
                                : '一键清理全部主机未使用网络'
                            }
                          >
                            {isDockerUpdateConfirmActive(allNetworkPruneConfirmKey)
                              ? '再次确认'
                              : '一键清理'}
                          </Button>
                        ),
                        getHostCount: server => asArray(server.resources?.networks).length,
                        getHostBadges: server => {
                          const networks = asArray(server.resources?.networks);
                          return networks.length > 0
                            ? [
                                {
                                  label: `${new Set(networks.map(network => getDockerNetworkDriver(network))).size} 驱动`,
                                },
                              ]
                            : [];
                        },
                        renderHostAction: server => {
                          const prunePayload = { serverId: server.id };
                          const prunePending = isDockerActionPending(
                            server.id,
                            'network.prune',
                            prunePayload
                          );
                          const pruneConfirmKey = `network.prune::${server.id}`;
                          return (
                            <Button
                              shape="square"
                              size="sm"
                              variant={
                                isDockerUpdateConfirmActive(pruneConfirmKey)
                                  ? 'secondary-destructive'
                                  : 'secondary'
                              }
                              icon={
                                prunePending ? (
                                  <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                ) : (
                                  <Trash className="h-3.5 w-3.5" />
                                )
                              }
                              disabled={prunePending}
                              onClick={() =>
                                submitDockerPruneTask(
                                  'network.prune',
                                  prunePayload,
                                  pruneConfirmKey,
                                  `清理 ${server.name} 未使用网络`
                                )
                              }
                              aria-label={
                                isDockerUpdateConfirmActive(pruneConfirmKey)
                                  ? `再次确认清理 ${server.name} 网络`
                                  : `清理 ${server.name} 网络`
                              }
                              title={
                                isDockerUpdateConfirmActive(pruneConfirmKey)
                                  ? '再次点击确认清理'
                                  : '清理未使用网络'
                              }
                            />
                          );
                        },
                      })}
                      <div className="flex min-w-0 flex-col gap-3">
                        {hosts.length === 0
                          ? renderDockerEmptyState('未检测到可用的 Docker 主机')
                          : hosts.map(server => {
                              const networks = asArray(server.resources?.networks);
                              const prunePayload = { serverId: server.id };
                              const prunePending = isDockerActionPending(
                                server.id,
                                'network.prune',
                                prunePayload
                              );
                              const pruneConfirmKey = `network.prune.section::${server.id}`;
                              return renderDockerHostResourceSection({
                                server,
                                isFirstVisible: server.id === hosts[0]?.id,
                                icon: <Globe className="h-4 w-4 shrink-0 text-brand" />,
                                count: networks.length,
                                countLabel: '网络',
                                badges:
                                  networks.length > 0
                                    ? [
                                        {
                                          label: `${new Set(networks.map(network => getDockerNetworkDriver(network))).size} 驱动`,
                                        },
                                      ]
                                    : [],
                                actions: (
                                  <Button
                                    size="sm"
                                    variant={
                                      isDockerUpdateConfirmActive(pruneConfirmKey)
                                        ? 'secondary-destructive'
                                        : 'secondary'
                                    }
                                    icon={
                                      prunePending ? (
                                        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                      ) : (
                                        <Trash className="h-3.5 w-3.5" />
                                      )
                                    }
                                    disabled={prunePending}
                                    onClick={event => {
                                      event.stopPropagation();
                                      submitDockerPruneTask(
                                        'network.prune',
                                        prunePayload,
                                        pruneConfirmKey,
                                        `清理 ${server.name} 未使用网络`
                                      );
                                    }}
                                  >
                                    {isDockerUpdateConfirmActive(pruneConfirmKey)
                                      ? '再次确认'
                                      : '清理网络'}
                                  </Button>
                                ),
                                children:
                                  networks.length === 0 ? (
                                    <div className="p-8 text-center text-xs text-kumo-subtle">
                                      暂无 Docker 网络
                                    </div>
                                  ) : (
                                    <div className="divide-y divide-kumo-line">
                                      {networks.map((network, i) => {
                                        const networkName = getDockerNetworkName(network);
                                        const networkId = getDockerNetworkId(network);
                                        const isBuiltinNetwork = [
                                          'bridge',
                                          'host',
                                          'none',
                                        ].includes(networkName);
                                        const removePayload = {
                                          serverId: server.id,
                                          name: networkName,
                                        };
                                        const removePending = isDockerActionPending(
                                          server.id,
                                          'network.remove',
                                          removePayload
                                        );
                                        return (
                                          <div
                                            key={`${server.id}-${networkId || networkName}-${i}`}
                                            className="flex min-w-0 items-center gap-3 px-3 py-2.5 cq-sm:px-4"
                                          >
                                            <div className="min-w-0 flex-1">
                                              <div
                                                className="truncate text-xs font-semibold leading-5 text-kumo-strong"
                                                title={networkName}
                                              >
                                                {networkName}
                                              </div>
                                              <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-kumo-subtle">
                                                <span className="truncate font-mono">
                                                  {networkId || '-'}
                                                </span>
                                                <Badge
                                                  variant="teal"
                                                  className="font-mono text-[10px]"
                                                >
                                                  {getDockerNetworkDriver(network)}
                                                </Badge>
                                                <Badge
                                                  variant="neutral"
                                                  className="font-mono text-[10px]"
                                                >
                                                  {getDockerNetworkScope(network)}
                                                </Badge>
                                              </div>
                                            </div>
                                            <Button
                                              shape="square"
                                              size="sm"
                                              variant={
                                                isArmed(
                                                  `docker.network.remove::${server.id}::${networkName}`
                                                )
                                                  ? 'destructive'
                                                  : 'secondary-destructive'
                                              }
                                              aria-label={`删除 ${networkName || '网络'}`}
                                              disabled={isBuiltinNetwork || removePending}
                                              onClick={() => {
                                                if (
                                                  !confirmPress(
                                                    `docker.network.remove::${server.id}::${networkName}`,
                                                    `删除网络「${networkName || '网络'}」`
                                                  )
                                                )
                                                  return;
                                                submitDockerTask('network.remove', removePayload, {
                                                  skipConfirm: true,
                                                });
                                              }}
                                              className="text-kumo-danger disabled:opacity-40"
                                              title={
                                                isBuiltinNetwork ? '内置网络不可删除' : '删除网络'
                                              }
                                            >
                                              {removePending ? (
                                                <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                              ) : (
                                                <Trash className="h-3.5 w-3.5" />
                                              )}
                                            </Button>
                                          </div>
                                        );
                                      })}
                                    </div>
                                  ),
                              });
                            })}
                      </div>
                    </div>
                  );
                })()}

              {/* 5. 存储卷管理 */}
              {dockerSubTab === 'volumes' &&
                (() => {
                  const hosts = dockerOverviewServers.filter(server =>
                    isDockerOverviewHostVisible(server, 'volumes')
                  );
                  return (
                    <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                      {renderDockerResourceSideRail({
                        title: '存储卷',
                        icon: <Database className="h-4 w-4 shrink-0 text-brand" />,
                        hosts,
                        totalCount: dockerVolumes.length,
                        countLabel: '存储卷',
                        summaryItems: [
                          {
                            label: '驱动',
                            value: new Set(dockerVolumes.map(item => getDockerVolumeDriver(item)))
                              .size,
                          },
                        ],
                        getHostCount: server => asArray(server.resources?.volumes).length,
                        getHostBadges: server => {
                          const volumes = asArray(server.resources?.volumes);
                          return volumes.length > 0
                            ? [
                                {
                                  label: `${new Set(volumes.map(volume => getDockerVolumeDriver(volume))).size} 驱动`,
                                },
                              ]
                            : [];
                        },
                        renderHostAction: server => {
                          const prunePayload = { serverId: server.id };
                          const prunePending = isDockerActionPending(
                            server.id,
                            'volume.prune',
                            prunePayload
                          );
                          const pruneConfirmKey = `volume.prune::${server.id}`;
                          return (
                            <Button
                              shape="square"
                              size="sm"
                              variant={
                                isDockerUpdateConfirmActive(pruneConfirmKey)
                                  ? 'secondary-destructive'
                                  : 'secondary'
                              }
                              icon={
                                prunePending ? (
                                  <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                ) : (
                                  <Trash className="h-3.5 w-3.5" />
                                )
                              }
                              disabled={prunePending}
                              onClick={() =>
                                submitDockerPruneTask(
                                  'volume.prune',
                                  prunePayload,
                                  pruneConfirmKey,
                                  `清理 ${server.name} 未使用存储卷`
                                )
                              }
                              aria-label={
                                isDockerUpdateConfirmActive(pruneConfirmKey)
                                  ? `再次确认清理 ${server.name} 存储卷`
                                  : `清理 ${server.name} 存储卷`
                              }
                              title={
                                isDockerUpdateConfirmActive(pruneConfirmKey)
                                  ? '再次点击确认清理'
                                  : '清理未使用存储卷'
                              }
                            />
                          );
                        },
                      })}
                      <div className="flex min-w-0 flex-col gap-3">
                        {hosts.length === 0
                          ? renderDockerEmptyState('未检测到可用的 Docker 主机')
                          : hosts.map(server => {
                              const volumes = asArray(server.resources?.volumes);
                              const prunePayload = { serverId: server.id };
                              const prunePending = isDockerActionPending(
                                server.id,
                                'volume.prune',
                                prunePayload
                              );
                              const pruneConfirmKey = `volume.prune.section::${server.id}`;
                              return renderDockerHostResourceSection({
                                server,
                                isFirstVisible: server.id === hosts[0]?.id,
                                icon: <Database className="h-4 w-4 shrink-0 text-brand" />,
                                count: volumes.length,
                                countLabel: '存储卷',
                                badges:
                                  volumes.length > 0
                                    ? [
                                        {
                                          label: `${new Set(volumes.map(volume => getDockerVolumeDriver(volume))).size} 驱动`,
                                        },
                                      ]
                                    : [],
                                actions: (
                                  <Button
                                    size="sm"
                                    variant={
                                      isDockerUpdateConfirmActive(pruneConfirmKey)
                                        ? 'secondary-destructive'
                                        : 'secondary'
                                    }
                                    icon={
                                      prunePending ? (
                                        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                      ) : (
                                        <Trash className="h-3.5 w-3.5" />
                                      )
                                    }
                                    disabled={prunePending}
                                    onClick={event => {
                                      event.stopPropagation();
                                      submitDockerPruneTask(
                                        'volume.prune',
                                        prunePayload,
                                        pruneConfirmKey,
                                        `清理 ${server.name} 未使用存储卷`
                                      );
                                    }}
                                  >
                                    {isDockerUpdateConfirmActive(pruneConfirmKey)
                                      ? '再次确认'
                                      : '清理存储卷'}
                                  </Button>
                                ),
                                children:
                                  volumes.length === 0 ? (
                                    <div className="p-8 text-center text-xs text-kumo-subtle">
                                      暂无 Docker 存储卷
                                    </div>
                                  ) : (
                                    <div className="divide-y divide-kumo-line">
                                      {volumes.map((volume, i) => {
                                        const volumeName = getDockerVolumeName(volume);
                                        const removePayload = {
                                          serverId: server.id,
                                          name: volumeName,
                                        };
                                        const removePending = isDockerActionPending(
                                          server.id,
                                          'volume.remove',
                                          removePayload
                                        );
                                        return (
                                          <div
                                            key={`${server.id}-${volumeName}-${i}`}
                                            className="flex min-w-0 items-center gap-3 px-3 py-2.5 cq-sm:px-4"
                                          >
                                            <div className="min-w-0 flex-1">
                                              <div
                                                className="truncate text-xs font-semibold leading-5 text-kumo-strong"
                                                title={volumeName}
                                              >
                                                {volumeName}
                                              </div>
                                              <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-kumo-subtle">
                                                <Badge
                                                  variant="teal"
                                                  className="font-mono text-[10px]"
                                                >
                                                  {getDockerVolumeDriver(volume)}
                                                </Badge>
                                                <Badge
                                                  variant="neutral"
                                                  className="font-mono text-[10px]"
                                                >
                                                  {getDockerVolumeScope(volume)}
                                                </Badge>
                                              </div>
                                            </div>
                                            <Button
                                              shape="square"
                                              size="sm"
                                              variant={
                                                isArmed(
                                                  `docker.volume.remove::${server.id}::${volumeName}`
                                                )
                                                  ? 'destructive'
                                                  : 'secondary-destructive'
                                              }
                                              aria-label={`删除 ${volumeName || '存储卷'}`}
                                              disabled={removePending}
                                              onClick={() => {
                                                if (
                                                  !confirmPress(
                                                    `docker.volume.remove::${server.id}::${volumeName}`,
                                                    `删除存储卷「${volumeName || '存储卷'}」`
                                                  )
                                                )
                                                  return;
                                                submitDockerTask('volume.remove', removePayload, {
                                                  skipConfirm: true,
                                                });
                                              }}
                                              className="text-kumo-danger"
                                              title="删除存储卷"
                                            >
                                              {removePending ? (
                                                <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                              ) : (
                                                <Trash className="h-3.5 w-3.5" />
                                              )}
                                            </Button>
                                          </div>
                                        );
                                      })}
                                    </div>
                                  ),
                              });
                            })}
                      </div>
                    </div>
                  );
                })()}

              {/* 6. 实时统计 */}
              {dockerSubTab === 'stats' &&
                (() => {
                  const hosts = dockerOverviewServers.filter(server =>
                    isDockerOverviewHostVisible(server, 'stats')
                  );
                  return (
                    <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                      {renderDockerResourceSideRail({
                        title: '实时统计',
                        icon: <Activity className="h-4 w-4 shrink-0 text-brand" />,
                        hosts,
                        totalCount: dockerStats.length,
                        countLabel: '容器',
                        summaryItems: [
                          {
                            label: 'CPU',
                            value: `${dockerStatsSummary.cpu.toFixed(1)}%`,
                            className: 'text-kumo-success',
                          },
                          {
                            label: '内存',
                            value: `${dockerStatsSummary.memory.toFixed(1)}%`,
                            className: 'text-kumo-info',
                          },
                          {
                            label: '网络',
                            value: `${formatBytesValue(dockerStatsSummary.netIn)} / ${formatBytesValue(dockerStatsSummary.netOut)}`,
                          },
                          {
                            label: '磁盘',
                            value: `${formatBytesValue(dockerStatsSummary.blockRead)} / ${formatBytesValue(dockerStatsSummary.blockWrite)}`,
                          },
                        ],
                        getHostCount: server => asArray(server.resources?.stats).length,
                        getHostBadges: server => {
                          const serverHistory = dockerStatsHistory
                            .map(snapshot => snapshot.filter(item => item.serverId === server.id))
                            .filter(snapshot => snapshot.length > 0);
                          const latest = serverHistory[serverHistory.length - 1] || [];
                          const summary = latest.reduce(
                            (acc, item) => ({
                              cpu: acc.cpu + toNumber(item.cpu, 0),
                              memory: acc.memory + toNumber(item.memory, 0),
                            }),
                            { cpu: 0, memory: 0 }
                          );
                          return latest.length > 0
                            ? [
                                {
                                  label: `CPU ${summary.cpu.toFixed(1)}%`,
                                  variant: 'success',
                                  appearance: 'dot',
                                },
                                {
                                  label: `内存 ${summary.memory.toFixed(1)}%`,
                                  variant: 'info',
                                  appearance: 'dot',
                                },
                              ]
                            : [];
                        },
                      })}
                      <div className="flex min-w-0 flex-col gap-3">
                        {hosts.length === 0
                          ? renderDockerEmptyState('未检测到可用的 Docker 主机')
                          : hosts.map(server => {
                              const currentStats = asArray(server.resources?.stats);
                              const serverHistory = dockerStatsHistory
                                .map(snapshot =>
                                  snapshot.filter(item => item.serverId === server.id)
                                )
                                .filter(snapshot => snapshot.length > 0);
                              const latest = serverHistory[serverHistory.length - 1] || [];
                              const summary = latest.reduce(
                                (acc, item) => ({
                                  cpu: acc.cpu + toNumber(item.cpu, 0),
                                  memory: acc.memory + toNumber(item.memory, 0),
                                  netIn: acc.netIn + toNumber(item.netIn, 0),
                                  netOut: acc.netOut + toNumber(item.netOut, 0),
                                  blockRead: acc.blockRead + toNumber(item.blockRead, 0),
                                  blockWrite: acc.blockWrite + toNumber(item.blockWrite, 0),
                                }),
                                {
                                  cpu: 0,
                                  memory: 0,
                                  netIn: 0,
                                  netOut: 0,
                                  blockRead: 0,
                                  blockWrite: 0,
                                }
                              );
                              const isDarkMode = theme === 'dark';
                              const netInColor = ChartPalette.categorical(0, isDarkMode);
                              const netOutColor = ChartPalette.semantic('Success', isDarkMode);
                              const readColor = ChartPalette.semantic('Warning', isDarkMode);
                              const writeColor = ChartPalette.categorical(3, isDarkMode);
                              const charts = [
                                {
                                  key: 'cpu',
                                  title: 'CPU 使用率',
                                  description: '容器 CPU 趋势',
                                  data: buildDockerStatSeries(serverHistory, 'cpu', isDarkMode),
                                  value: `${summary.cpu.toFixed(1)}%`,
                                  yAxisTickFormat: formatPercentAxis,
                                  tooltipValueFormat: value => `${toNumber(value, 0).toFixed(2)}%`,
                                },
                                {
                                  key: 'memory',
                                  title: '内存使用率',
                                  description: '容器内存趋势',
                                  data: buildDockerStatSeries(serverHistory, 'memory', isDarkMode),
                                  value: `${summary.memory.toFixed(1)}%`,
                                  yAxisTickFormat: formatPercentAxis,
                                  tooltipValueFormat: value => `${toNumber(value, 0).toFixed(2)}%`,
                                },
                                {
                                  key: 'network',
                                  title: '网络 I/O',
                                  description: '容器网络流量',
                                  data: buildDockerPairSeries(serverHistory, [
                                    { key: 'netIn', name: '接收', color: netInColor },
                                    { key: 'netOut', name: '发送', color: netOutColor },
                                  ]),
                                  value: `${formatBytesValue(summary.netIn)} / ${formatBytesValue(summary.netOut)}`,
                                  yAxisTickFormat: formatBytesValue,
                                  tooltipValueFormat: formatBytesValue,
                                },
                                {
                                  key: 'disk',
                                  title: '磁盘 I/O',
                                  description: '容器磁盘流量',
                                  data: buildDockerPairSeries(serverHistory, [
                                    { key: 'blockRead', name: '读取', color: readColor },
                                    { key: 'blockWrite', name: '写入', color: writeColor },
                                  ]),
                                  value: `${formatBytesValue(summary.blockRead)} / ${formatBytesValue(summary.blockWrite)}`,
                                  yAxisTickFormat: formatBytesValue,
                                  tooltipValueFormat: formatBytesValue,
                                },
                              ];
                              return renderDockerHostResourceSection({
                                server,
                                isFirstVisible: server.id === hosts[0]?.id,
                                icon: <Activity className="h-4 w-4 shrink-0 text-brand" />,
                                count: currentStats.length,
                                countLabel: '容器',
                                badges:
                                  currentStats.length > 0
                                    ? [
                                        {
                                          label: `CPU ${summary.cpu.toFixed(1)}%`,
                                          variant: 'success',
                                          appearance: 'dot',
                                        },
                                        {
                                          label: `内存 ${summary.memory.toFixed(1)}%`,
                                          variant: 'info',
                                          appearance: 'dot',
                                        },
                                      ]
                                    : [],
                                children:
                                  currentStats.length === 0 ? (
                                    <div className="p-8 text-center text-xs text-kumo-subtle">
                                      暂无 Docker 资源统计
                                    </div>
                                  ) : (
                                    <div className="grid gap-3 cq-xl:grid-cols-2">
                                      {charts.map(chart => (
                                        <ChartBoundaryBox
                                          key={chart.key}
                                          className="min-w-0 rounded-md border border-kumo-line bg-kumo-base p-3"
                                        >
                                          {tooltipBoundary => (
                                            <div className="min-w-0">
                                              <div className="mb-3 flex min-w-0 items-start justify-between gap-3">
                                                <div className="min-w-0">
                                                  <div className="truncate text-sm font-semibold text-kumo-strong">
                                                    {chart.title}
                                                  </div>
                                                  <div className="mt-0.5 truncate text-xs text-kumo-subtle">
                                                    {chart.description}
                                                  </div>
                                                </div>
                                                <Badge variant="neutral">{chart.value}</Badge>
                                              </div>
                                              {chart.data.length === 0 ? (
                                                <div className="flex h-[220px] items-center justify-center rounded-md border border-dashed border-kumo-line text-center text-xs text-kumo-subtle">
                                                  暂无可绘制数据
                                                </div>
                                              ) : (
                                                <SiteFontTimeseriesChart
                                                  echarts={fastTimeseriesEcharts}
                                                  data={chart.data}
                                                  height={220}
                                                  isDarkMode={isDarkMode}
                                                  gradient
                                                  loading={dockerResourceLoading}
                                                  tooltipBoundary={tooltipBoundary ?? undefined}
                                                  tooltipFollowCursor="x"
                                                  xAxisTickCount={isCompactViewport ? 3 : 5}
                                                  yAxisTickCount={4}
                                                  xAxisTickFormat={expandedChartXAxisTickFormat}
                                                  yAxisTickFormat={chart.yAxisTickFormat}
                                                  tooltipValueFormat={chart.tooltipValueFormat}
                                                  optionUpdateBehavior={
                                                    SERVER_FAST_CHART_UPDATE_BEHAVIOR
                                                  }
                                                  ariaDescription={`${server.name} ${chart.title}`}
                                                />
                                              )}
                                            </div>
                                          )}
                                        </ChartBoundaryBox>
                                      ))}
                                    </div>
                                  ),
                              });
                            })}
                      </div>
                    </div>
                  );
                })()}
            </div>
          )}
        </>
      )}

    </>
  );
}
