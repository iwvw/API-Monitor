import { LayerCard, Table, Tabs } from '@cloudflare/kumo';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppCard, AppTable, EmptyState } from '../../components/ui/AppPrimitives.jsx';
import { DockerManageCard, DockerResourceTableCard } from './DockerResourceCards.jsx';
import DockerAutoUpdatePanel from './DockerAutoUpdatePanel.jsx';
import { Box, Database, FileText, FolderOpen, Globe, HardDrive, Pause, Play, RefreshCw, RotateCw, Search, Settings, Square, Trash, Upload } from '../../components/Icons.jsx';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { isDockerImagePruneCandidate } from '../../modules/dockerTasks.js';
import { asArray, formatComposeStatusLabel, getComposeConfigFiles, getComposePrimaryConfigFile, getComposeProjectName, getComposeStatus, getComposeWorkingDir, getContainerAutoUpdateBadge, getDockerImageId, getDockerImageRepository, getDockerImageSize, getDockerImageTag, getDockerNetworkDriver, getDockerNetworkId, getDockerNetworkName, getDockerNetworkScope, getDockerVolumeDriver, getDockerVolumeName, getDockerVolumeScope, isComposeConfigMissing, isDockerOverviewHostVisible, matchContainerAutoUpdateLabel } from './serverHelpers.js';
import { SERVER_SECONDARY_TABS_GROUP_CLASS, SERVER_SECTION_HEADER_CLASS } from './constants.js';

const DOCKER_CONTAINER_TABLE_COLUMNS = [
  { id: 'select', role: 'check' },
  { id: 'container', role: 'primary', grow: 2 },
  { id: 'state', role: 'status' },
  { id: 'ports', role: 'meta', align: 'center', grow: 1, minWidth: 120, maxWidth: 260 },
  { id: 'update', role: 'status', minWidth: 96, width: 100, maxWidth: 116 },
  { id: 'actions', role: 'actions-lg' },
];



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
    dockerAutoUpdate,
    dockerAutoUpdateBusy,
    dockerAutoUpdateResultMap,
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
    dockerSelectedServer,
    dockerSubTab,
    dockerVolumes,
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
    isDockerActionPending,
    isDockerContainerUpdateChecking,
    isDockerHostExpanded,
    isDockerUpdateConfirmActive,
    loadDockerAutoUpdate,
    loadDockerResources,
    openDockerComposeConfig,
    openDockerContainerLogs,
    pruneDockerImagesForHosts,
    pruneDockerNetworksForHosts,
    renderDockerEmptyState,
    renderDockerFilterChip,
    renderDockerSimpleLogCard,
    runDockerAutoUpdate,
    saveDockerAutoUpdatePolicy,
    selectedUpdatableDockerContainers,
    serverCurrentTab,
    setDockerContainerStateFilter,
    setDockerSearchQuery,
    setDockerSelectedServer,
    setDockerSubTab,
    showDockerBlockingLoading,
    submitDockerPruneTask,
    submitDockerTask,
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
                  {
                    value: 'auto-update',
                    label: (
                      <span className="inline-flex items-center gap-1.5">
                        <RefreshCw className="w-3.5 h-3.5" />
                        自动更新
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
              {dockerSubTab === 'containers' && (() => {
                const activeServer =
                  visibleDockerContainerServers.find(
                    server => String(server.id) === String(dockerSelectedServer || '')
                  ) ||
                  visibleDockerContainerServers[0] ||
                  null;
                const activeContainers = asArray(activeServer?.resources?.containers);
                const activeSummary = activeServer
                  ? getDockerContainerSummary(activeContainers, activeServer.id)
                  : { total: 0, running: 0, paused: 0, stopped: 0, updatable: 0 };
                const activeUpdateTargets = activeServer
                  ? getUpdatableDockerContainers([activeServer], activeServer.id)
                  : [];
                const activeUpdateConfirmKey = activeServer
                  ? `host.update::${activeServer.id}::${activeUpdateTargets.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`
                  : '';

                if (dockerOverviewServers.length === 0) {
                  return renderDockerEmptyState('未检测到可用的 Docker 主机');
                }
                if (visibleDockerContainerServers.length === 0) {
                  return renderDockerEmptyState('当前筛选下没有容器');
                }

                return (
                  <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                    <DockerManageCard
                      hosts={visibleDockerContainerServers}
                      statItems={[
                        { label: '容器', value: dockerContainerManagementTotals.total },
                        {
                          label: '运行',
                          value: dockerContainerManagementTotals.running,
                          className: 'text-kumo-success',
                        },
                        {
                          label: '停止',
                          value: dockerContainerManagementTotals.stopped,
                          className: 'text-kumo-subtle',
                        },
                        {
                          label: '更新',
                          value: dockerContainerManagementTotals.updatable,
                          className: 'text-kumo-warning',
                        },
                      ]}
                      selectedServerId={activeServer?.id}
                      onSelectServer={setDockerSelectedServer}
                      getHostCount={server =>
                        getDockerContainerSummary(server.resources?.containers, server.id).total
                      }
                      getHostBadges={server => {
                        const summary = getDockerContainerSummary(
                          server.resources?.containers,
                          server.id
                        );
                        return [
                          ...(summary.running > 0
                            ? [{ label: summary.running, variant: 'success', appearance: 'dot' }]
                            : []),
                          ...(summary.updatable > 0
                            ? [{ label: summary.updatable, variant: 'warning', appearance: 'dot' }]
                            : []),
                        ];
                      }}
                      headerActions={
                        <>
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
                        </>
                      }
                      footer={renderDockerSimpleLogCard('order-3')}
                    />

                    <div className="flex min-w-0 flex-col gap-3 cq-xl:order-2 cq-xl:min-h-0">
                      <LayerCard className="flex min-h-0 min-w-0 flex-col overflow-hidden bg-kumo-base p-0 shadow-none cq-xl:h-[calc(100dvh-184px)] cq-xl:min-h-64">
                        <LayerCard.Secondary className={SERVER_SECTION_HEADER_CLASS}>
                          <span className="inline-flex min-w-0 items-center gap-2 text-xs font-semibold text-kumo-strong">
                            <Box className="h-4 w-4 shrink-0 text-brand" />
                            <span className="min-w-0 truncate" title={activeServer.name}>
                              {activeServer.name}
                            </span>
                          </span>
                          <span className="flex shrink-0 items-center gap-1.5">
                            {activeSummary.updatable > 0 && (
                              <Button
                                size="sm"
                                variant={
                                  isDockerUpdateConfirmActive(activeUpdateConfirmKey)
                                    ? 'secondary-destructive'
                                    : 'primary'
                                }
                                icon={<Upload className="h-3.5 w-3.5" />}
                                onClick={() =>
                                  batchUpdateVisibleDockerContainers(
                                    activeServer.id,
                                    false,
                                    activeUpdateTargets,
                                    { confirmKey: activeUpdateConfirmKey }
                                  )
                                }
                              >
                                {isDockerUpdateConfirmActive(activeUpdateConfirmKey)
                                  ? '再次确认'
                                  : '一键更新'}
                              </Button>
                            )}
                          </span>
                        </LayerCard.Secondary>

                        <LayerCard.Primary className="flex min-h-0 min-w-0 flex-1 flex-col gap-0 overflow-hidden p-0">
                          <div className="min-h-0 min-w-0 flex-1 overflow-auto overscroll-contain scrollbar-thin">
                          {activeContainers.length === 0 ? (
                            <div className="p-4">
                              <EmptyState
                                icon={Box}
                                title="没有容器"
                                description="该主机在当前筛选下没有容器。"
                              />
                            </div>
                          ) : (
                            <AppTable tableId="docker-containers" columns={DOCKER_CONTAINER_TABLE_COLUMNS} className="w-full min-w-[720px] text-xs">
                              <Table.Header sticky variant="compact">
                                <Table.Row className="h-8">
                                  <Table.Head className="!px-2 !py-1.5 text-center">
                                    <span className="sr-only">选择</span>
                                  </Table.Head>
                                  <Table.Head className="!px-2.5 !py-1.5">容器</Table.Head>
                                  <Table.Head className="!px-2 !py-1.5 text-center">状态</Table.Head>
                                  <Table.Head className="!px-2 !py-1.5 text-center">端口</Table.Head>
                                  <Table.Head className="!px-2 !py-1.5 text-center">更新</Table.Head>
                                  <Table.Head className="!px-2 !py-1.5 text-center">操作</Table.Head>
                                </Table.Row>
                              </Table.Header>
                              <Table.Body>
                                {activeContainers.map(c => {
                                  const state = getDockerContainerState(c);
                                  const stateBadge = getDockerStateBadge(state);
                                  const containerId = getDockerContainerId(c);
                                  const containerName = getDockerContainerName(c);
                                  const containerImage = getDockerContainerImage(c);
                                  const containerPorts = getDockerContainerPorts(c);
                                  const updateCheck = getDockerContainerUpdateCheck(
                                    activeServer.id,
                                    c
                                  );
                                  const updateBadge = getDockerUpdateBadge(updateCheck);
                                  const updateChecking = isDockerContainerUpdateChecking(
                                    activeServer.id,
                                    c
                                  );
                                  const toggleAction =
                                    state === 'running' ? 'container.stop' : 'container.start';
                                  const togglePayload = {
                                    serverId: activeServer.id,
                                    containerId,
                                    containerName,
                                    image: containerImage,
                                  };
                                  const togglePending = isDockerActionPending(
                                    activeServer.id,
                                    toggleAction,
                                    togglePayload
                                  );
                                  const restartPayload = {
                                    serverId: activeServer.id,
                                    containerId,
                                    containerName,
                                    image: containerImage,
                                  };
                                  const restartPending = isDockerActionPending(
                                    activeServer.id,
                                    'container.restart',
                                    restartPayload
                                  );
                                  const updatePayload = {
                                    serverId: activeServer.id,
                                    containerId,
                                    containerName,
                                    image: containerImage,
                                  };
                                  const updatePending = isDockerActionPending(
                                    activeServer.id,
                                    'container.update',
                                    updatePayload
                                  );
                                  const updateProgress = getDockerActionProgress(
                                    activeServer.id,
                                    'container.update',
                                    updatePayload
                                  );
                                  const updateConfirmKey = `container.update::${getDockerContainerSelectionKey(activeServer.id, updatePayload)}`;
                                  const selectionKey = getDockerContainerSelectionKey(
                                    activeServer.id,
                                    updatePayload
                                  );
                                  const canSelectForUpdate =
                                    !!updateCheck?.hasUpdate && !updatePending;
                                  const autoUpdateResult =
                                    dockerAutoUpdateResultMap?.get?.(
                                      `${activeServer.id}::${containerId}`
                                    ) || null;
                                  const autoUpdateTarget =
                                    Boolean(autoUpdateResult) ||
                                    matchContainerAutoUpdateLabel(
                                      c.labels,
                                      dockerAutoUpdate?.policy?.containerLabel
                                    );
                                  const autoUpdateBadge = autoUpdateTarget
                                    ? getContainerAutoUpdateBadge(autoUpdateResult)
                                    : null;
                                  return (
                                    <Table.Row
                                      className="[&>td]:align-middle"
                                      key={containerId || `${activeServer.id}-${containerName}`}
                                    >
                                      <Table.Cell className="!px-2 !py-1.5 text-center">
                                        <Checkbox
                                          checked={dockerSelectedContainerKeySet.has(selectionKey)}
                                          disabled={!canSelectForUpdate}
                                          onCheckedChange={checked =>
                                            toggleDockerContainerSelection(
                                              selectionKey,
                                              Boolean(checked)
                                            )
                                          }
                                          aria-label={`选择更新 ${containerName}`}
                                        />
                                      </Table.Cell>
                                      <Table.Cell className="!px-2.5 !py-1.5">
                                        <div className="min-w-0">
                                          <div
                                            className="truncate text-sm leading-normal font-medium text-kumo-strong"
                                            title={containerName}
                                          >
                                            {containerName}
                                          </div>
                                          <div
                                            className="truncate font-mono leading-5 text-kumo-subtle"
                                            title={containerImage}
                                          >
                                            {containerImage}
                                          </div>
                                          {autoUpdateBadge && (
                                            <Badge
                                              variant={autoUpdateBadge.variant}
                                              className="mt-1 text-[10px]"
                                              title={autoUpdateBadge.title}
                                            >
                                              {autoUpdateBadge.label}
                                            </Badge>
                                          )}
                                        </div>
                                      </Table.Cell>
                                      <Table.Cell className="!px-2 !py-1.5 text-center">
                                        <Badge variant={stateBadge.variant} className="text-xs">
                                          {stateBadge.label}
                                        </Badge>
                                      </Table.Cell>
                                      <Table.Cell className="!px-2 !py-1.5 text-center">
                                        <span
                                          className="block max-w-full truncate font-mono text-xs leading-5 text-kumo-subtle"
                                          title={containerPorts}
                                        >
                                          {containerPorts || '-'}
                                        </span>
                                      </Table.Cell>
                                      <Table.Cell className="!px-2 !py-1.5 text-center">
                                        {updatePending ? (
                                          <span
                                            className="inline-flex items-center gap-1 text-[10px] font-semibold text-brand"
                                            title={`${containerName} 更新进度`}
                                          >
                                            <RefreshCw className="h-3 w-3 animate-spin" />
                                            更新中
                                            {updateProgress > 0 ? ` ${updateProgress}%` : ''}
                                          </span>
                                        ) : updateChecking ? (
                                          <span className="inline-flex items-center gap-1 text-xs font-semibold text-kumo-subtle">
                                            <RefreshCw className="h-3 w-3 animate-spin" />
                                            检测中
                                          </span>
                                        ) : (
                                          <Badge
                                            variant={updateBadge.variant}
                                            className="text-xs"
                                            title={updateBadge.title}
                                          >
                                            {updateBadge.label}
                                          </Badge>
                                        )}
                                      </Table.Cell>
                                      <Table.Cell className="!px-2 !py-1.5 text-center">
                                        <div className="flex flex-wrap items-center justify-center gap-1">
                                          <Button
                                            shape="square"
                                            size="sm"
                                            variant={
                                              state === 'running'
                                                ? 'secondary-destructive'
                                                : 'outline'
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
                                            variant="outline"
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
                                              submitDockerTask('container.restart', restartPayload)
                                            }
                                            title="重启"
                                          />
                                          <Button
                                            shape="square"
                                            size="sm"
                                            variant="outline"
                                            icon={
                                              updateChecking ? (
                                                <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                              ) : (
                                                <Search className="h-3.5 w-3.5" />
                                              )
                                            }
                                            aria-label="检测镜像更新"
                                            disabled={updateChecking}
                                            onClick={() => checkDockerUpdatesForServer(activeServer, c)}
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
                                          <Button
                                            shape="square"
                                            size="sm"
                                            variant="outline"
                                            icon={<FileText className="h-3.5 w-3.5" />}
                                            aria-label="查看容器日志"
                                            onClick={() =>
                                              openDockerContainerLogs(activeServer, c)
                                            }
                                            title="日志"
                                          />
                                        </div>
                                      </Table.Cell>
                                    </Table.Row>
                                  );
                                })}
                              </Table.Body>
                            </AppTable>
                          )}
                          </div>
                        </LayerCard.Primary>
                      </LayerCard>
                    </div>
                  </div>
                );
              })()}

              {/* 2. Compose */}
              {dockerSubTab === 'compose' &&
                (() => {
                  const hosts = dockerOverviewServers.filter(server =>
                    isDockerOverviewHostVisible(server, 'compose')
                  );
                  const runningProjects = dockerComposeProjects.filter(item =>
                    getComposeStatus(item).includes('running')
                  ).length;
                  const activeServer =
                    hosts.find(server => String(server.id) === String(dockerSelectedServer || '')) ||
                    hosts[0] ||
                    null;
                  if (dockerOverviewServers.length === 0) {
                    return renderDockerEmptyState('未检测到可用的 Docker 主机');
                  }
                  if (!activeServer) {
                    return renderDockerEmptyState('当前筛选下没有可用的 Docker 主机');
                  }
                  const projects = asArray(activeServer.resources?.composeProjects);
                  const running = projects.filter(project =>
                    getComposeStatus(project).toLowerCase().includes('running')
                  ).length;

                  return (
                    <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                      <DockerManageCard
                        hosts={hosts}
                        statItems={[
                          { label: '项目', value: dockerComposeProjects.length },
                          {
                            label: '运行',
                            value: runningProjects,
                            className: 'text-kumo-success',
                          },
                          {
                            label: '停止',
                            value: Math.max(0, dockerComposeProjects.length - runningProjects),
                            className: 'text-kumo-subtle',
                          },
                          { label: '主机', value: hosts.length, className: 'text-kumo-subtle' },
                        ]}
                        selectedServerId={activeServer.id}
                        onSelectServer={setDockerSelectedServer}
                        getHostCount={server => asArray(server.resources?.composeProjects).length}
                        getHostBadges={server => {
                          const items = asArray(server.resources?.composeProjects);
                          const runningCount = items.filter(project =>
                            getComposeStatus(project).toLowerCase().includes('running')
                          ).length;
                          return runningCount > 0
                            ? [{ label: `${runningCount} 运行`, variant: 'success' }]
                            : [];
                        }}
                        onRefresh={loadDockerResources}
                        refreshing={dockerResourceLoading}
                        footer={renderDockerSimpleLogCard()}
                      />

                      <DockerResourceTableCard
                        title={activeServer.name}
                        icon={<FolderOpen className="h-4 w-4 shrink-0 text-brand" />}
                        badges={[
                          { label: `${projects.length} 项目` },
                          ...(running > 0 ? [{ label: `${running} 运行`, variant: 'success' }] : []),
                        ]}
                        headers={[
                          { label: '项目', primary: true },
                          { label: '状态', align: 'center' },
                          { label: '配置文件' },
                          { label: '目录' },
                          { label: '操作', align: 'center' },
                        ]}
                        layout="auto"
                        tableId="docker-compose-projects"
                        isEmpty={projects.length === 0}
                        emptyTitle="暂无 Compose 项目"
                        emptyDescription="该主机在当前筛选下没有 Compose 项目。"
                        emptyIcon={FolderOpen}
                      >
                        {projects.map(proj => {
                          const projectName = getComposeProjectName(proj);
                          const configFiles = getComposeConfigFiles(proj);
                          const primaryConfigFile = getComposePrimaryConfigFile(proj);
                          const workingDir = getComposeWorkingDir(proj);
                          const status = getComposeStatus(proj);
                          const statusLower = status.toLowerCase();
                          const composeRunning = statusLower.includes('running');
                          const statusLabel = formatComposeStatusLabel(status);
                          const composePayload = {
                            serverId: activeServer.id,
                            project: projectName,
                            config_file: configFiles,
                          };
                          const composeUpPending = isDockerActionPending(
                            activeServer.id,
                            'compose.up',
                            composePayload
                          );
                          const composeDownPending = isDockerActionPending(
                            activeServer.id,
                            'compose.down',
                            composePayload
                          );
                          const composeRestartPending = isDockerActionPending(
                            activeServer.id,
                            'compose.restart',
                            composePayload
                          );
                          const composeUpdatePending = isDockerActionPending(
                            activeServer.id,
                            'compose.update',
                            composePayload
                          );
                          const composeBusy =
                            composeUpPending ||
                            composeDownPending ||
                            composeRestartPending ||
                            composeUpdatePending;
                          const composeTogglePending = composeRunning
                            ? composeDownPending
                            : composeUpPending;
                          const composeToggleAction = composeRunning
                            ? 'compose.down'
                            : 'compose.up';
                          let composeToggleIcon = <Play className="h-3.5 w-3.5" />;
                          if (composeRunning) {
                            composeToggleIcon = <Square className="h-3.5 w-3.5" />;
                          }
                          if (composeTogglePending) {
                            composeToggleIcon = (
                              <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                            );
                          }
                          return (
                            <Table.Row
                              key={`${activeServer.id}-${projectName}-${configFiles}`}
                              className="[&>td]:align-middle"
                            >
                              <Table.Cell className="!px-2.5 !py-1.5">
                                <div
                                  className="text-sm leading-normal font-medium whitespace-nowrap text-kumo-strong"
                                  title={projectName}
                                >
                                  {projectName}
                                </div>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <div className="flex flex-col items-center gap-1">
                                  <Badge
                                    variant={composeRunning ? 'success' : 'error'}
                                    className="text-xs"
                                  >
                                    {statusLabel}
                                  </Badge>
                                  {isComposeConfigMissing(proj) && (
                                    <Badge
                                      variant="warning"
                                      className="text-xs"
                                      title="配置文件路径已失效,查看/修改配置会失败;可执行 down 或忽略该项目"
                                    >
                                      配置缺失
                                    </Badge>
                                  )}
                                </div>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5">
                                <span
                                  className="font-mono text-xs leading-5 whitespace-nowrap text-kumo-subtle"
                                  title={configFiles}
                                >
                                  {configFiles || '-'}
                                </span>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5">
                                <span
                                  className="font-mono text-xs leading-5 whitespace-nowrap text-kumo-subtle"
                                  title={workingDir}
                                >
                                  {workingDir || '-'}
                                </span>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <div className="flex items-center justify-center gap-1">
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant="outline"
                                    icon={<FileText className="h-3.5 w-3.5" />}
                                    onClick={() => openDockerComposeConfig(activeServer, proj, 'view')}
                                    disabled={!primaryConfigFile}
                                    aria-label={`查看或修改 ${projectName} 配置`}
                                    title={primaryConfigFile ? '查看/修改配置' : '未找到配置文件'}
                                  />
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant="outline"
                                    icon={
                                      composeUpdatePending ? (
                                        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                      ) : (
                                        <Upload className="h-3.5 w-3.5" />
                                      )
                                    }
                                    disabled={composeBusy}
                                    onClick={() => submitDockerTask('compose.update', composePayload)}
                                    aria-label={`升级 ${projectName}`}
                                    title="强制拉取最新镜像并重建容器"
                                  />
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant="outline"
                                    icon={
                                      composeRestartPending ? (
                                        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                      ) : (
                                        <RotateCw className="h-3.5 w-3.5" />
                                      )
                                    }
                                    disabled={composeBusy}
                                    onClick={() =>
                                      submitDockerTask('compose.restart', composePayload)
                                    }
                                    aria-label={`重启 ${projectName}`}
                                    title="重启项目"
                                  />
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant={composeRunning ? 'secondary-destructive' : 'primary'}
                                    icon={composeToggleIcon}
                                    disabled={composeBusy}
                                    onClick={() => submitDockerTask(composeToggleAction, composePayload)}
                                    aria-label={`${composeRunning ? '停止' : '启动'} ${projectName}`}
                                    title={composeRunning ? '停止项目' : '启动项目'}
                                  />
                                </div>
                              </Table.Cell>
                            </Table.Row>
                          );
                        })}
                      </DockerResourceTableCard>
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
                  const activeServer =
                    hosts.find(server => String(server.id) === String(dockerSelectedServer || '')) ||
                    hosts[0] ||
                    null;
                  if (dockerOverviewServers.length === 0) {
                    return renderDockerEmptyState('未检测到可用的 Docker 主机');
                  }
                  if (!activeServer) {
                    return renderDockerEmptyState('当前筛选下没有可用的 Docker 主机');
                  }
                  const images = asArray(activeServer.resources?.images);
                  const activePruneCandidates = images.filter(isDockerImagePruneCandidate).length;
                  const prunePayload = { serverId: activeServer.id };
                  const prunePending = isDockerActionPending(
                    activeServer.id,
                    'image.prune',
                    prunePayload
                  );
                  const pruneConfirmKey = `image.prune.section::${activeServer.id}`;

                  return (
                    <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                      <DockerManageCard
                        hosts={hosts}
                        statItems={[
                          { label: '镜像', value: dockerImages.length },
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
                          { label: '主机', value: hosts.length, className: 'text-kumo-subtle' },
                        ]}
                        headerActions={
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
                        }
                        selectedServerId={activeServer.id}
                        onSelectServer={setDockerSelectedServer}
                        getHostCount={server => asArray(server.resources?.images).length}
                        getHostBadges={server => {
                          const items = asArray(server.resources?.images);
                          const candidates = items.filter(isDockerImagePruneCandidate).length;
                          return items.length > 0
                            ? [
                                {
                                  label: `${new Set(items.map(img => getDockerImageRepository(img))).size} 仓库`,
                                },
                                ...(candidates > 0
                                  ? [{ label: `${candidates} 可清理`, variant: 'warning' }]
                                  : []),
                              ]
                            : [];
                        }}
                        onRefresh={loadDockerResources}
                        refreshing={dockerResourceLoading}
                        footer={renderDockerSimpleLogCard()}
                      />

                      <DockerResourceTableCard
                        title={activeServer.name}
                        icon={<HardDrive className="h-4 w-4 shrink-0 text-brand" />}
                        badges={[
                          { label: `${images.length} 镜像` },
                          ...(activePruneCandidates > 0
                            ? [{ label: `${activePruneCandidates} 可清理`, variant: 'warning' }]
                            : []),
                        ]}
                        actions={
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
                            onClick={() =>
                              submitDockerPruneTask(
                                'image.prune',
                                prunePayload,
                                pruneConfirmKey,
                                `清理 ${activeServer.name} 未使用镜像`
                              )
                            }
                          >
                            {isDockerUpdateConfirmActive(pruneConfirmKey) ? '再次确认' : '清理镜像'}
                          </Button>
                        }
                        headers={[
                          { label: '镜像', primary: true },
                          { label: '标签', align: 'center' },
                          { label: '大小', align: 'center' },
                          { label: '操作', align: 'center' },
                        ]}
                        columns={[
                          { id: 'repository', role: 'primary', grow: 8, minWidth: 200 },
                          { id: 'tag', role: 'primary', grow: 1, minWidth: 176, maxWidth: 240, align: 'center' },
                          { id: 'size', role: 'primary', grow: 1, minWidth: 88, maxWidth: 140, align: 'center' },
                          { id: 'actions', role: 'primary', grow: 1, minWidth: 96, maxWidth: 120, align: 'center' },
                        ]}
                        tableId="docker-images"
                        isEmpty={images.length === 0}
                        emptyTitle="暂无镜像"
                        emptyDescription="该主机在当前筛选下没有镜像。"
                        emptyIcon={HardDrive}
                      >
                        {images.map((img, i) => {
                          const repository = getDockerImageRepository(img);
                          const tag = getDockerImageTag(img);
                          const imageId = getDockerImageId(img);
                          const imageRef =
                            imageId ||
                            (tag && tag !== '-' ? `${repository}:${tag}` : repository);
                          const removePayload = {
                            serverId: activeServer.id,
                            image: imageRef,
                          };
                          const removePending = isDockerActionPending(
                            activeServer.id,
                            'image.remove',
                            removePayload
                          );
                          return (
                            <Table.Row
                              key={`${activeServer.id}-${imageId || repository}-${i}`}
                              className="[&>td]:align-middle"
                            >
                              <Table.Cell className="!px-2.5 !py-1.5">
                                <div className="min-w-0">
                                  <div
                                    className="truncate text-sm leading-normal font-medium text-kumo-strong"
                                    title={repository}
                                  >
                                    {repository}
                                  </div>
                                  <div
                                    className="truncate font-mono leading-5 text-kumo-subtle"
                                    title={imageId}
                                  >
                                    {imageId || '-'}
                                  </div>
                                </div>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <Badge
                                  variant="secondary"
                                  className="max-w-full font-mono text-xs"
                                >
                                  <span className="min-w-0 truncate" title={tag}>
                                    {tag}
                                  </span>
                                </Badge>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <span className="font-mono text-xs text-kumo-strong">
                                  {getDockerImageSize(img)}
                                </span>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <div className="flex items-center justify-center gap-1">
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant={
                                      isArmed(
                                        `docker.image.remove::${activeServer.id}::${imageRef}`
                                      )
                                        ? 'destructive'
                                        : 'secondary-destructive'
                                    }
                                    aria-label={`删除 ${repository || '镜像'}`}
                                    disabled={removePending}
                                    onClick={() => {
                                      if (
                                        !confirmPress(
                                          `docker.image.remove::${activeServer.id}::${imageRef}`,
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
                              </Table.Cell>
                            </Table.Row>
                          );
                        })}
                      </DockerResourceTableCard>
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
                  const activeServer =
                    hosts.find(server => String(server.id) === String(dockerSelectedServer || '')) ||
                    hosts[0] ||
                    null;
                  if (dockerOverviewServers.length === 0) {
                    return renderDockerEmptyState('未检测到可用的 Docker 主机');
                  }
                  if (!activeServer) {
                    return renderDockerEmptyState('当前筛选下没有可用的 Docker 主机');
                  }
                  const networks = asArray(activeServer.resources?.networks);
                  const prunePayload = { serverId: activeServer.id };
                  const prunePending = isDockerActionPending(
                    activeServer.id,
                    'network.prune',
                    prunePayload
                  );
                  const pruneConfirmKey = `network.prune.section::${activeServer.id}`;

                  return (
                    <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                      <DockerManageCard
                        hosts={hosts}
                        statItems={[
                          { label: '网络', value: dockerNetworks.length },
                          {
                            label: '驱动',
                            value: new Set(
                              dockerNetworks.map(item => getDockerNetworkDriver(item))
                            ).size,
                          },
                          { label: '主机', value: hosts.length, className: 'text-kumo-subtle' },
                        ]}
                        headerActions={
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
                        }
                        selectedServerId={activeServer.id}
                        onSelectServer={setDockerSelectedServer}
                        getHostCount={server => asArray(server.resources?.networks).length}
                        getHostBadges={server => {
                          const items = asArray(server.resources?.networks);
                          return items.length > 0
                            ? [
                                {
                                  label: `${new Set(items.map(network => getDockerNetworkDriver(network))).size} 驱动`,
                                },
                              ]
                            : [];
                        }}
                        onRefresh={loadDockerResources}
                        refreshing={dockerResourceLoading}
                        footer={renderDockerSimpleLogCard()}
                      />

                      <DockerResourceTableCard
                        title={activeServer.name}
                        icon={<Globe className="h-4 w-4 shrink-0 text-brand" />}
                        badges={[
                          { label: `${networks.length} 网络` },
                          ...(networks.length > 0
                            ? [
                                {
                                  label: `${new Set(networks.map(network => getDockerNetworkDriver(network))).size} 驱动`,
                                },
                              ]
                            : []),
                        ]}
                        actions={
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
                            onClick={() =>
                              submitDockerPruneTask(
                                'network.prune',
                                prunePayload,
                                pruneConfirmKey,
                                `清理 ${activeServer.name} 未使用网络`
                              )
                            }
                          >
                            {isDockerUpdateConfirmActive(pruneConfirmKey) ? '再次确认' : '清理网络'}
                          </Button>
                        }
                        headers={[
                          { label: '网络', primary: true },
                          { label: '驱动', align: 'center' },
                          { label: '范围', align: 'center' },
                          { label: '操作', align: 'center' },
                        ]}
                        columns={[
                          { id: 'network', role: 'primary', grow: 2 },
                          { id: 'driver', role: 'type' },
                          { id: 'scope', role: 'type' },
                          { id: 'actions', role: 'actions-md' },
                        ]}
                        tableId="docker-networks"
                        isEmpty={networks.length === 0}
                        emptyTitle="暂无 Docker 网络"
                        emptyDescription="该主机在当前筛选下没有网络。"
                        emptyIcon={Globe}
                      >
                        {networks.map((network, i) => {
                          const networkName = getDockerNetworkName(network);
                          const networkId = getDockerNetworkId(network);
                          const isBuiltinNetwork = ['bridge', 'host', 'none'].includes(networkName);
                          const removePayload = {
                            serverId: activeServer.id,
                            name: networkName,
                          };
                          const removePending = isDockerActionPending(
                            activeServer.id,
                            'network.remove',
                            removePayload
                          );
                          return (
                            <Table.Row
                              key={`${activeServer.id}-${networkId || networkName}-${i}`}
                              className="[&>td]:align-middle"
                            >
                              <Table.Cell className="!px-2.5 !py-1.5">
                                <div className="min-w-0">
                                  <div
                                    className="truncate text-sm leading-normal font-medium text-kumo-strong"
                                    title={networkName}
                                  >
                                    {networkName}
                                  </div>
                                  <div
                                    className="truncate font-mono leading-5 text-kumo-subtle"
                                    title={networkId}
                                  >
                                    {networkId || '-'}
                                  </div>
                                </div>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <Badge variant="teal" className="font-mono text-xs">
                                  {getDockerNetworkDriver(network)}
                                </Badge>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <Badge variant="neutral" className="font-mono text-xs">
                                  {getDockerNetworkScope(network)}
                                </Badge>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <div className="flex items-center justify-center gap-1">
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant={
                                      isArmed(
                                        `docker.network.remove::${activeServer.id}::${networkName}`
                                      )
                                        ? 'destructive'
                                        : 'secondary-destructive'
                                    }
                                    aria-label={`删除 ${networkName || '网络'}`}
                                    disabled={isBuiltinNetwork || removePending}
                                    onClick={() => {
                                      if (
                                        !confirmPress(
                                          `docker.network.remove::${activeServer.id}::${networkName}`,
                                          `删除网络「${networkName || '网络'}」`
                                        )
                                      )
                                        return;
                                      submitDockerTask('network.remove', removePayload, {
                                        skipConfirm: true,
                                      });
                                    }}
                                    className="text-kumo-danger disabled:opacity-40"
                                    title={isBuiltinNetwork ? '内置网络不可删除' : '删除网络'}
                                  >
                                    {removePending ? (
                                      <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                                    ) : (
                                      <Trash className="h-3.5 w-3.5" />
                                    )}
                                  </Button>
                                </div>
                              </Table.Cell>
                            </Table.Row>
                          );
                        })}
                      </DockerResourceTableCard>
                    </div>
                  );
                })()}

              {/* 5. 存储卷管理 */}
              {dockerSubTab === 'volumes' &&
                (() => {
                  const hosts = dockerOverviewServers.filter(server =>
                    isDockerOverviewHostVisible(server, 'volumes')
                  );
                  const activeServer =
                    hosts.find(server => String(server.id) === String(dockerSelectedServer || '')) ||
                    hosts[0] ||
                    null;
                  if (dockerOverviewServers.length === 0) {
                    return renderDockerEmptyState('未检测到可用的 Docker 主机');
                  }
                  if (!activeServer) {
                    return renderDockerEmptyState('当前筛选下没有可用的 Docker 主机');
                  }
                  const volumes = asArray(activeServer.resources?.volumes);
                  const prunePayload = { serverId: activeServer.id };
                  const prunePending = isDockerActionPending(
                    activeServer.id,
                    'volume.prune',
                    prunePayload
                  );
                  const pruneConfirmKey = `volume.prune.section::${activeServer.id}`;

                  return (
                    <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                      <DockerManageCard
                        hosts={hosts}
                        statItems={[
                          { label: '存储卷', value: dockerVolumes.length },
                          {
                            label: '驱动',
                            value: new Set(dockerVolumes.map(item => getDockerVolumeDriver(item)))
                              .size,
                          },
                          { label: '主机', value: hosts.length, className: 'text-kumo-subtle' },
                        ]}
                        selectedServerId={activeServer.id}
                        onSelectServer={setDockerSelectedServer}
                        getHostCount={server => asArray(server.resources?.volumes).length}
                        getHostBadges={server => {
                          const items = asArray(server.resources?.volumes);
                          return items.length > 0
                            ? [
                                {
                                  label: `${new Set(items.map(volume => getDockerVolumeDriver(volume))).size} 驱动`,
                                },
                              ]
                            : [];
                        }}
                        onRefresh={loadDockerResources}
                        refreshing={dockerResourceLoading}
                        footer={renderDockerSimpleLogCard()}
                      />

                      <DockerResourceTableCard
                        title={activeServer.name}
                        icon={<Database className="h-4 w-4 shrink-0 text-brand" />}
                        badges={[
                          { label: `${volumes.length} 存储卷` },
                          ...(volumes.length > 0
                            ? [
                                {
                                  label: `${new Set(volumes.map(volume => getDockerVolumeDriver(volume))).size} 驱动`,
                                },
                              ]
                            : []),
                        ]}
                        actions={
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
                            onClick={() =>
                              submitDockerPruneTask(
                                'volume.prune',
                                prunePayload,
                                pruneConfirmKey,
                                `清理 ${activeServer.name} 未使用存储卷`
                              )
                            }
                          >
                            {isDockerUpdateConfirmActive(pruneConfirmKey)
                              ? '再次确认'
                              : '清理存储卷'}
                          </Button>
                        }
                        headers={[
                          { label: '存储卷', primary: true },
                          { label: '驱动', align: 'center' },
                          { label: '范围', align: 'center' },
                          { label: '操作', align: 'center' },
                        ]}
                        columns={[
                          { id: 'volume', role: 'primary', grow: 2 },
                          { id: 'driver', role: 'type' },
                          { id: 'scope', role: 'type' },
                          { id: 'actions', role: 'actions-md' },
                        ]}
                        tableId="docker-volumes"
                        isEmpty={volumes.length === 0}
                        emptyTitle="暂无 Docker 存储卷"
                        emptyDescription="该主机在当前筛选下没有存储卷。"
                        emptyIcon={Database}
                      >
                        {volumes.map((volume, i) => {
                          const volumeName = getDockerVolumeName(volume);
                          const removePayload = {
                            serverId: activeServer.id,
                            name: volumeName,
                          };
                          const removePending = isDockerActionPending(
                            activeServer.id,
                            'volume.remove',
                            removePayload
                          );
                          return (
                            <Table.Row
                              key={`${activeServer.id}-${volumeName}-${i}`}
                              className="[&>td]:align-middle"
                            >
                              <Table.Cell className="!px-2.5 !py-1.5">
                                <div
                                  className="truncate text-sm leading-normal font-medium text-kumo-strong"
                                  title={volumeName}
                                >
                                  {volumeName}
                                </div>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <Badge variant="teal" className="font-mono text-xs">
                                  {getDockerVolumeDriver(volume)}
                                </Badge>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <Badge variant="neutral" className="font-mono text-xs">
                                  {getDockerVolumeScope(volume)}
                                </Badge>
                              </Table.Cell>
                              <Table.Cell className="!px-2 !py-1.5 text-center">
                                <div className="flex items-center justify-center gap-1">
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant={
                                      isArmed(
                                        `docker.volume.remove::${activeServer.id}::${volumeName}`
                                      )
                                        ? 'destructive'
                                        : 'secondary-destructive'
                                    }
                                    aria-label={`删除 ${volumeName || '存储卷'}`}
                                    disabled={removePending}
                                    onClick={() => {
                                      if (
                                        !confirmPress(
                                          `docker.volume.remove::${activeServer.id}::${volumeName}`,
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
                              </Table.Cell>
                            </Table.Row>
                          );
                        })}
                      </DockerResourceTableCard>
                    </div>
                  );
                })()}

              {dockerSubTab === 'auto-update' && (
                <DockerAutoUpdatePanel
                  state={dockerAutoUpdate}
                  busy={dockerAutoUpdateBusy}
                  onSave={saveDockerAutoUpdatePolicy}
                  onRun={runDockerAutoUpdate}
                  onRefresh={loadDockerAutoUpdate}
                />
              )}

            </div>
          )}
        </>
      )}

    </>
  );
}
