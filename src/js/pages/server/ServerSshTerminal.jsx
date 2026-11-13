import { createPortal } from 'react-dom';
import { Button } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import { AppCard } from '../../components/ui/AppPrimitives.jsx';
import TerminalWorkspaceContainer from '../../components/server/TerminalWorkspaceContainer.jsx';
import QuickCommandBar from '../../components/server/QuickCommandBar.jsx';
import SftpPanel from '../../components/server/SftpPanel.jsx';
import { ExpandedSection } from './ExpandedSection.jsx';
import { ExpandedProgressMetric } from './ExpandedProgressMetric.jsx';
import { ExpandedInfoChip } from './ExpandedInfoChip.jsx';
import { ExpandedStatTile } from './ExpandedStatTile.jsx';
import {
  Activity,
  Columns,
  Eye,
  EyeOff,
  FolderOpen,
  Grid,
  Maximize2,
  RefreshCw,
  Send,
  Terminal as TerminalIcon,
  X,
} from '../../components/Icons.jsx';
import { buildPresetLayout, updateBranchRatio } from '../../modules/terminalSplitTree.js';
import { clampPercent, toNumber, getOSIconClass } from './utils.js';
import {
  formatUptimeDaysOnly,
  getGpuModelText,
  getHostAddress,
  getServerCycleParts,
} from './serverHelpers.js';

export default function ServerSshTerminal(props) {
  const {
    activateSSHSession,
    activeSSHSessionId,
    activeTerminalSidebar,
    applyTerminalPresetLayout,
    closeSSHSession,
    createAttachedTerminalView,
    draggedSessionId,
    fitTerminalSession,
    handleTerminalDragStart,
    reconnectSSHSession,
    refreshServerInfo,
    removeSSHSessionFromView,
    runQuickCommand,
    serverCurrentTab,
    serverIpDisplayMode,
    serverList,
    setActiveTerminalSidebar,
    setDraggedSessionId,
    setSftpCurrentPath,
    setSftpServerId,
    setSshIdeFullscreen,
    setSshSplitTree,
    setSshSyncEnabled,
    setTerminalIpMasked,
    setTerminalStatusRefreshing,
    sftpCurrentPath,
    sftpPathByServerRef,
    sftpServerId,
    showCommandSidebar,
    showServerStatusSidebar,
    showSftpSidebar,
    sshIdeFullscreen,
    sshSessions,
    sshSplitTree,
    sshSyncEnabled,
    sshViewLayout,
    switchToSSHTab,
    terminalIpMasked,
    terminalStatusRefreshing,
    toggleTerminalSidebar,
    triggerSplitPane,
    visibleSessionIds,
    visibleSessionIdsRef,
  } = props;
  return (
    <>
      {serverCurrentTab === 'terminal' &&
        (() => {
          const activeSession = sshSessions.find(s => s.id === activeSSHSessionId);
          const activeServer =
            serverList.find(server => String(server.id) === String(activeSession?.server?.id)) ||
            activeSession?.server;
          const activeInfo = activeServer?.info || {};
          const activePrimaryDisk = activeInfo.disk?.[0];
          const terminalCpuUsage = clampPercent(toNumber(activeInfo.cpu?.Usage, 0));
          const terminalMemUsage = clampPercent(toNumber(activeInfo.memory?.Usage, 0));
          const terminalDiskUsage = clampPercent(toNumber(activePrimaryDisk?.usage, 0));
          const terminalGpuUsage = clampPercent(toNumber(activeInfo.gpu?.Usage, 0));
          const terminalCpuTemp = toNumber(activeInfo.cpu?.Temp, 0);
          const terminalCpuPower = toNumber(activeInfo.cpu?.Power, 0);
          const terminalGpuTemp = toNumber(activeInfo.gpu?.Temp, 0);
          const terminalPhysicalCores = activeInfo.cpu?.PhysicalCores || activeInfo.cpu?.Cores;
          const terminalLogicalCores = activeInfo.cpu?.LogicalCores;
          const terminalCoreText =
            terminalPhysicalCores &&
            terminalLogicalCores &&
            terminalPhysicalCores !== terminalLogicalCores
              ? `${terminalPhysicalCores}核 / ${terminalLogicalCores}线程`
              : `${terminalPhysicalCores || '-'} 核`;
          const terminalGpuModel = getGpuModelText(activeInfo.gpu);
          const terminalHasGpu = !!terminalGpuModel || terminalGpuUsage > 0 || terminalGpuTemp > 0;
          const terminalCycleParts = getServerCycleParts(activeServer);
          const terminalTxTotal = terminalCycleParts.tx;
          const terminalRxTotal = terminalCycleParts.rx;
          const terminalCard = (
            <AppCard
              padding="none"
              className={
                sshIdeFullscreen
                  ? 'flex h-full w-full min-h-0 flex-col overflow-hidden rounded-none border-0'
                  : 'flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden'
              }
            >
              <div className="flex min-h-11 items-center justify-between gap-3 rounded-t-[inherit] border-b border-kumo-line bg-kumo-base px-3 py-2 text-xs">
                <div className="flex min-w-0 flex-1 items-center gap-1.5 overflow-x-auto scrollbar-thin">
                  {visibleSessionIds.length > 1 && (
                    // 分屏组：把参与分屏的会话合并成一个整体标签（不再逐个列出）
                    <div
                      className="flex h-7 shrink-0 items-center overflow-hidden rounded-md border border-brand/60 bg-kumo-recessed text-kumo-strong"
                      title={visibleSessionIds
                        .map(id => sshSessions.find(item => item.id === id)?.name || '终端')
                        .join(' + ')}
                    >
                      <div className="flex items-center gap-1.5 px-2 text-[11px] font-semibold">
                        <span className="flex items-center gap-1">
                          {visibleSessionIds.map(id => {
                            const s = sshSessions.find(item => item.id === id);
                            return (
                              <span
                                key={id}
                                className={`inline-block h-1.5 w-1.5 rounded-full ${
                                  s?.connected ? 'bg-kumo-success' : 'bg-kumo-warning'
                                }`}
                                title={`${s?.name || '终端'}: ${s?.connected ? '已连接' : '未连接'}`}
                              />
                            );
                          })}
                        </span>
                        <span className="max-w-40 truncate">
                          {visibleSessionIds
                            .map(id => sshSessions.find(item => item.id === id)?.name || '终端')
                            .join(' + ')}
                        </span>
                        <span className="rounded bg-brand/15 px-1 py-0.2 text-[10px] font-bold text-brand">
                          {visibleSessionIds.length}分屏
                        </span>
                      </div>
                    </div>
                  )}
                  {sshSessions
                    .filter(sess => visibleSessionIds.length > 1 && visibleSessionIds.includes(sess.id) ? false : true)
                    .map(sess => (
                      <div
                        key={sess.id}
                        draggable
                        onDragStart={event => handleTerminalDragStart(event, sess.id)}
                        onDragEnd={() => {
                          setDraggedSessionId(null);
                        }}
                        className={`flex h-7 shrink-0 items-center rounded-md border ${
                          visibleSessionIds.length <= 1 && activeSSHSessionId === sess.id
                            ? 'border-brand/60 bg-kumo-recessed text-kumo-strong'
                            : 'border-kumo-line bg-kumo-base text-kumo-subtle'
                        }`}
                      >
                        <Button
                          type="button"
                          size="sm"
                          variant="ghost"
                          onClick={() => {
                            if (visibleSessionIds.length > 1) {
                              // 分屏中点击其它主机：把它加入当前分屏，而不是清空分屏
                              triggerSplitPane(activeSSHSessionId || visibleSessionIds[0], 'right', sess.id);
                            } else if (visibleSessionIds.includes(sess.id)) {
                              activateSSHSession(sess.id);
                            } else {
                              switchToSSHTab(sess.id);
                            }
                          }}
                          className="h-full min-w-0 justify-start px-2 text-[11px] font-semibold text-inherit"
                          title={
                            visibleSessionIds.length > 1
                              ? `加入分屏：${sess.name}`
                              : sess.name
                          }
                        >
                          <span
                            className={`h-1.5 w-1.5 rounded-full ${
                              sess.connected
                                ? 'bg-kumo-success'
                                : sess.reconnecting
                                  ? 'animate-pulse bg-kumo-warning'
                                  : 'bg-kumo-danger'
                            }`}
                            title={
                              sess.connected
                                ? '已连接'
                                : sess.reconnecting
                                  ? `连接断开，正在第 ${sess.reconnectAttempt || 1} 次自动重连`
                                  : '连接已断开'
                            }
                          ></span>
                          <span className="max-w-28 truncate">{sess.name}</span>
                        </Button>
                        <Button
                          shape="square"
                          size="sm"
                          variant="ghost"
                          icon={<X className="h-3 w-3" />}
                          aria-label="关闭终端会话"
                          title="关闭终端会话"
                          onClick={e => {
                            e.stopPropagation();
                            closeSSHSession(sess.id);
                          }}
                          className="hover:text-kumo-danger"
                        />
                      </div>
                    ))}
                </div>

                <div className="flex shrink-0 items-center gap-1.5">
                  {visibleSessionIds.length > 1 && (
                    <div className="flex items-center gap-1 border-r border-kumo-line pr-1.5">
                      <Button
                        size="sm"
                        variant={sshViewLayout === 'single' ? 'primary' : 'secondary'}
                        onClick={() => applyTerminalPresetLayout('single')}
                        title="单窗口模式"
                      >
                        单窗
                      </Button>
                      <Button
                        size="sm"
                        variant={sshViewLayout === 'split-h' ? 'primary' : 'secondary'}
                        icon={<Columns className="h-3.5 w-3.5" />}
                        onClick={() => applyTerminalPresetLayout('split-h')}
                        title="水平排列"
                      />
                      <Button
                        size="sm"
                        variant={sshViewLayout === 'master-stack' ? 'primary' : 'secondary'}
                        onClick={() => applyTerminalPresetLayout('master-stack')}
                        title="主从模式（主操作台 + 侧栏堆叠）"
                      >
                        主从
                      </Button>
                      <Button
                        size="sm"
                        variant={sshViewLayout === 'grid' ? 'primary' : 'secondary'}
                        icon={<Grid className="h-3.5 w-3.5" />}
                        onClick={() => applyTerminalPresetLayout('grid')}
                        title="智能平衡网格"
                      />
                    </div>
                  )}
                  <Button
                    size="sm"
                    aria-pressed={sshSyncEnabled}
                    variant={sshSyncEnabled ? 'primary' : 'secondary'}
                    icon={<Send className="h-3.5 w-3.5" />}
                    onClick={() => setSshSyncEnabled(prev => !prev)}
                    title={sshSyncEnabled ? '同步输入已开启（点击关闭）' : '同步输入已关闭（点击开启）'}
                  >
                    同步
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    icon={<RefreshCw className="h-3.5 w-3.5" />}
                    onClick={() => reconnectSSHSession(activeSSHSessionId)}
                    disabled={!activeSSHSessionId}
                    title="恢复连接"
                  >
                    重连
                  </Button>
                  <Button
                    shape="square"
                    size="sm"
                    variant={sshIdeFullscreen ? 'primary' : 'secondary'}
                    icon={<Maximize2 className="h-3.5 w-3.5" />}
                    onClick={() => setSshIdeFullscreen(v => !v)}
                    title={sshIdeFullscreen ? '退出页面全屏' : '页面全屏（扩大分屏）'}
                    aria-label={sshIdeFullscreen ? '退出页面全屏' : '页面全屏'}
                  />
                </div>
              </div>

              <div className="flex min-h-0 flex-1 overflow-hidden rounded-b-[inherit]">
                <div className="flex min-w-0 flex-1 flex-col">
                  <div className="relative flex min-h-0 flex-1 overflow-hidden">
                    <div
                      className={`relative min-h-0 min-w-0 flex-1 overflow-hidden transition-[margin] duration-base ${
                        activeTerminalSidebar ? 'mr-[clamp(18rem,24vw,26rem)]' : ''
                      }`}
                    >
                      <TerminalWorkspaceContainer
                        splitTree={
                          sshSplitTree ||
                          (visibleSessionIds.length > 0
                            ? buildPresetLayout(visibleSessionIds, sshViewLayout, activeSSHSessionId)
                            : null)
                        }
                        sessions={sshSessions}
                        activeSessionId={activeSSHSessionId}
                        draggedSessionId={draggedSessionId}
                        onActivateSession={activateSSHSession}
                        onCloseSession={removeSSHSessionFromView}
                        onDuplicateSession={createAttachedTerminalView}
                        onDragStartSession={handleTerminalDragStart}
                        onSplitDrop={(targetId, sourceId, position) => {
                          triggerSplitPane(targetId, position, sourceId);
                        }}
                        onRatioChange={(path, ratio) => {
                          setSshSplitTree(prev => updateBranchRatio(prev, path, ratio));
                          setTimeout(() => {
                            visibleSessionIdsRef.current.forEach(id => fitTerminalSession(id, true));
                          }, 50);
                        }}
                      />
                    </div>

                    {activeTerminalSidebar && (
                      <div
                        className={`absolute bottom-0 right-0 top-0 z-10 flex min-h-0 flex-col overflow-hidden border-l border-kumo-line bg-kumo-base w-[clamp(18rem,24vw,26rem)]`}
                      >
                        {showServerStatusSidebar && (
                          <div className="flex h-full min-h-0 flex-col text-xs">
                            <div className="flex shrink-0 items-center justify-between gap-2 border-b border-kumo-line px-2.5 py-2">
                              <div className="flex min-w-0 items-center gap-2">
                                <Activity className="h-4 w-4 shrink-0 text-brand" />
                                <span className="truncate text-xs font-semibold text-kumo-strong">资源监控</span>
                                <Badge variant="neutral" className="max-w-32 shrink-0 truncate text-[10px] leading-4">
                                  {activeSession?.name || activeServer?.name || '-'}
                                </Badge>
                              </div>
                              <div className="flex shrink-0 items-center gap-1">
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant="secondary"
                                  icon={terminalIpMasked ? <Eye className="h-3.5 w-3.5" /> : <EyeOff className="h-3.5 w-3.5" />}
                                  aria-label={terminalIpMasked ? '显示 IP' : '打码 IP'}
                                  title={terminalIpMasked ? '显示 IP' : '打码 IP'}
                                  onClick={() => setTerminalIpMasked(v => !v)}
                                />
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant="secondary"
                                  icon={<RefreshCw className="h-3.5 w-3.5" />}
                                  aria-label="刷新资源指标"
                                  title="刷新资源指标"
                                  loading={terminalStatusRefreshing}
                                  onClick={async () => {
                                    const serverId = activeServer?.id;
                                    if (!serverId || terminalStatusRefreshing) return;
                                    setTerminalStatusRefreshing(true);
                                    try {
                                      await refreshServerInfo(serverId);
                                    } finally {
                                      setTerminalStatusRefreshing(false);
                                    }
                                  }}
                                />
                              </div>
                            </div>
                            <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-2.5">
                              <div className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 p-2">
                                <div className="flex min-w-0 items-center gap-2">
                                  <i className={getOSIconClass(activeInfo.platform)}></i>
                                  <div className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden">
                                    <span className="truncate text-xs font-semibold text-kumo-strong">
                                      {activeSession?.name || activeServer?.name || '-'}
                                    </span>
                                    <span
                                      className="truncate font-mono text-[10px] text-kumo-subtle"
                                      title={
                                        terminalIpMasked
                                          ? '****'
                                          : getHostAddress(activeServer, serverIpDisplayMode) ||
                                            activeServer?.host ||
                                            'Agent'
                                      }
                                    >
                                      {terminalIpMasked
                                        ? '****'
                                        : getHostAddress(activeServer, serverIpDisplayMode) ||
                                          activeServer?.host ||
                                          'Agent'}
                                    </span>
                                  </div>
                                </div>
                              </div>

                              <ExpandedSection title="资源状态" tone="success">
                                <div className="grid grid-cols-2 gap-1.5">
                                  <ExpandedProgressMetric
                                    label="CPU"
                                    value={terminalCpuUsage}
                                    detail={`${Math.round(terminalCpuUsage)}%`}
                                    caption={`${terminalCoreText}${terminalCpuTemp > 0 ? ` · ${Math.round(terminalCpuTemp)}°C` : ''}${terminalCpuPower > 0 ? ` · ${terminalCpuPower.toFixed(1)}W` : ''}`}
                                    indicatorClassName="!bg-none !bg-kumo-success"
                                    valueClassName="text-kumo-success"
                                  />
                                  <ExpandedProgressMetric
                                    label="内存"
                                    value={terminalMemUsage}
                                    detail={`${Math.round(terminalMemUsage)}%`}
                                    caption={`${activeInfo.memory?.Used || '-'} / ${activeInfo.memory?.Total || '-'}`}
                                    indicatorClassName="!bg-none !bg-kumo-info"
                                    valueClassName="text-kumo-info"
                                  />
                                  {activePrimaryDisk && (
                                    <ExpandedProgressMetric
                                      label="磁盘"
                                      value={terminalDiskUsage}
                                      detail={`${Math.round(terminalDiskUsage)}%`}
                                      caption={`${activePrimaryDisk.used || '-'} / ${activePrimaryDisk.total || '-'}`}
                                      indicatorClassName="!bg-none !bg-kumo-warning"
                                      valueClassName="text-kumo-warning"
                                    />
                                  )}
                                  {terminalHasGpu && (
                                    <ExpandedProgressMetric
                                      label="GPU"
                                      value={terminalGpuUsage}
                                      detail={`${Math.round(terminalGpuUsage)}%`}
                                      caption={`${terminalGpuModel || 'GPU'}${terminalGpuTemp > 0 ? ` · ${Math.round(terminalGpuTemp)}°C` : ''}`}
                                      indicatorClassName="!bg-none !bg-kumo-warning"
                                      valueClassName="text-kumo-warning"
                                    />
                                  )}
                                </div>
                              </ExpandedSection>

                              <ExpandedSection title="系统概览" tone="brand">
                                <div className="grid grid-cols-1 gap-1.5">
                                  <ExpandedInfoChip
                                    label="系统"
                                    value={
                                      [activeInfo.platform, activeInfo.platformVersion]
                                        .filter(Boolean)
                                        .join(' ') ||
                                      activeInfo.system?.Kernel ||
                                      '-'
                                    }
                                  />
                                  <ExpandedInfoChip
                                    label="CPU 型号"
                                    value={
                                      activeInfo.cpu?.Model ||
                                      activeServer?.metadata?.cpu_model ||
                                      activeServer?.metadata?.cpu_name ||
                                      '-'
                                    }
                                  />
                                  <ExpandedInfoChip
                                    label="负载"
                                    value={activeInfo.cpu?.Load || '-'}
                                    valueClassName="font-mono text-kumo-strong"
                                  />
                                  <ExpandedInfoChip
                                    label="在线"
                                    value={formatUptimeDaysOnly(
                                      activeInfo.uptime || activeInfo.system?.Uptime
                                    )}
                                  />
                                  <ExpandedInfoChip
                                    label="Agent 版本"
                                    value={activeInfo.agentVersion || '-'}
                                  />
                                </div>
                              </ExpandedSection>

                              {activeInfo.network && (
                                <ExpandedSection title="网络" tone="info">
                                  <div className="grid grid-cols-2 gap-1.5">
                                    <ExpandedStatTile
                                      label="上传"
                                      value={activeInfo.network.tx_speed || '0 B/s'}
                                      tone="info"
                                    />
                                    <ExpandedStatTile
                                      label="下载"
                                      value={activeInfo.network.rx_speed || '0 B/s'}
                                      tone="success"
                                    />
                                    <ExpandedInfoChip
                                      label="本期上行"
                                      value={terminalTxTotal.text}
                                      valueClassName="text-kumo-info"
                                      className="col-span-2"
                                    />
                                    <ExpandedInfoChip
                                      label="本期下行"
                                      value={terminalRxTotal.text}
                                      valueClassName="text-kumo-success"
                                      className="col-span-2"
                                    />
                                    <ExpandedInfoChip
                                      label="连接"
                                      value={activeInfo.network.connections || 0}
                                      className="col-span-2"
                                    />
                                  </div>
                                </ExpandedSection>
                              )}
                            </div>
                          </div>
                        )}
                        {showSftpSidebar && (
                          <div className="flex h-full min-h-0 overflow-hidden">
                            <SftpPanel
                              serverId={activeServer?.id || sftpServerId}
                              serverName={activeServer?.name}
                              initialPath={
                                sftpPathByServerRef.current[activeServer?.id] ||
                                sftpCurrentPath ||
                                '.'
                              }
                              onClose={() => setActiveTerminalSidebar(null)}
                              onPathChange={(serverId, path) => {
                                sftpPathByServerRef.current[serverId] = path;
                                setSftpServerId(serverId);
                                setSftpCurrentPath(path);
                              }}
                            />
                          </div>
                        )}
                        {showCommandSidebar && (
                          <div className="flex h-full min-h-0 overflow-hidden">
                            <QuickCommandBar
                              activeServer={activeServer}
                              activeSessionId={activeSSHSessionId}
                              sessions={sshSessions}
                              visibleSessionIds={visibleSessionIds}
                              syncEnabled={sshSyncEnabled}
                              onRunCommand={(command, options) => runQuickCommand(command, options)}
                            />
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                </div>
                <div className="flex w-11 shrink-0 flex-col items-center gap-3 border-l border-kumo-line bg-kumo-base py-3 text-kumo-subtle">
                  <Button
                    shape="square"
                    size="sm"
                    variant={showServerStatusSidebar ? 'secondary' : 'ghost'}
                    icon={<Activity className="h-4 w-4" />}
                    aria-label="资源监控"
                    title="资源监控"
                    onClick={() => toggleTerminalSidebar('status')}
                  />
                  <Button
                    shape="square"
                    size="sm"
                    variant={showSftpSidebar ? 'secondary' : 'ghost'}
                    icon={<FolderOpen className="h-4 w-4" />}
                    aria-label="SFTP 文件浏览"
                    title="SFTP 文件浏览"
                    onClick={() => {
                      const serverId = sshSessions.find(s => s.id === activeSSHSessionId)?.server
                        .id;
                      toggleTerminalSidebar('sftp', { serverId });
                    }}
                  />
                  <Button
                    shape="square"
                    size="sm"
                    variant={showCommandSidebar ? 'secondary' : 'ghost'}
                    icon={<TerminalIcon className="h-4 w-4" />}
                    aria-label="命令片段"
                    title="命令片段"
                    onClick={() => toggleTerminalSidebar('commands')}
                  />
                </div>
              </div>
            </AppCard>
          );

          return sshIdeFullscreen
            ? createPortal(
                <div className="fixed inset-0 z-[9999] bg-kumo-recessed animate-in fade-in-0 zoom-in-95 duration-quick">
                  {terminalCard}
                </div>,
                document.body
              )
            : terminalCard;
        })()}


    </>
  );
}
