import React from 'react';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { AnimatedCollapse } from '../../components/AnimatedCollapse.jsx';
import { StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Shield } from '../../components/Icons.jsx';
import UptimeMonitorDetails from './UptimeMonitorDetails.jsx';

function MonitorCard({
  monitor,
  beats,
  isExpanded,
  showMonitorSelectionControls,
  selectedMonitorIds,
  onToggleSelect,
  onToggleExpand,
  getUptimeTypeIcon,
  getDisplayUrl,
  getUptimeRate,
  getUptimeRateClass,
  formatUptimeRateCompact,
  uptimeHeartbeatLoading,
  isDarkMode,
  onPauseResume,
  onEdit,
  onDelete,
}) {
  const lastBeat = beats[0];

  // 状态指示
  let statusClass = 'border-kumo-interact/75';
  let statusPillClass = 'bg-kumo-line/20 text-kumo-subtle';
  let statusText = '暂停/未激活';

  if (monitor.active) {
    if (!lastBeat) {
      statusClass = 'border-kumo-interact/75';
      statusPillClass = 'bg-kumo-line/20 text-kumo-subtle';
      statusText = '等待中';
    } else if (lastBeat.status === 'up') {
      statusClass = 'border-kumo-interact/75';
      statusPillClass = 'bg-kumo-success/10 text-kumo-success border border-kumo-success/20';
      statusText = '正常';
    } else if (lastBeat.status === 'down') {
      statusClass = 'border-kumo-interact/75';
      statusPillClass = 'bg-kumo-danger/10 text-kumo-danger border border-kumo-danger/20';
      statusText = '故障';
    } else if (lastBeat.status === 'pending') {
      statusClass = 'border-kumo-interact/75';
      statusPillClass = 'bg-kumo-warning/10 text-kumo-warning border border-kumo-warning/20';
      statusText = '检测中';
    }
  }

  // 30 个心跳迷你丸
  const miniBeats = [];
  for (let i = 0; i < 30; i++) {
    const beat = beats[i];
    if (beat) {
      miniBeats.unshift(beat);
    } else {
      miniBeats.unshift({ status: 'empty' });
    }
  }

  return (
    <div
      className={`overflow-hidden rounded-lg border bg-kumo-base ${statusClass}`}
    >
      {/* 卡片头部行 */}
      <div
        onClick={() => onToggleExpand(isExpanded ? null : monitor.id)}
        className="flex flex-col cq-md:flex-row items-start cq-md:items-center justify-between p-2 gap-4 cursor-pointer hover:bg-kumo-recessed/25"
      >
        {/* 左侧选择复选框 & 图标 & 核心信息 */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          {showMonitorSelectionControls && (
            <Checkbox
              checked={selectedMonitorIds.includes(monitor.id)}
              onCheckedChange={(checked) => {
                onToggleSelect(monitor.id, checked);
              }}
              onClick={(e) => e.stopPropagation()}
              aria-label={`选择监测目标: ${monitor.name}`}
            />
          )}

          {/* 类型图标 */}
          <div className="w-8 h-8 rounded-lg border border-kumo-line/70 bg-kumo-recessed flex items-center justify-center text-kumo-strong flex-shrink-0">
            {getUptimeTypeIcon(monitor.type)}
          </div>

          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs font-semibold text-kumo-strong truncate">
                {monitor.name}
              </span>
              <span className={`text-[9px] px-1.5 py-0.5 rounded font-semibold ${statusPillClass}`}>
                {statusText}
              </span>
              {/* 标签 */}
              {monitor.tags && monitor.tags.map(t => (
                <StatusBadge key={t} tone="neutral" className="text-[9px] font-medium">
                  {t}
                </StatusBadge>
              ))}
              {/* SSL 证书到期徽章 */}
              {monitor.sslExpiry && (() => {
                const daysLeft = Math.floor((new Date(monitor.sslExpiry) - Date.now()) / 86400000);
                let sslColor = 'bg-kumo-success/10 text-kumo-success border-kumo-success/20';
                if (daysLeft <= 7) sslColor = 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20';
                else if (daysLeft <= 30) sslColor = 'bg-kumo-warning/10 text-kumo-warning border-kumo-warning/20';
                return (
                  <span className={`text-[9px] px-1.5 py-0.5 rounded font-semibold border flex items-center gap-1 ${sslColor}`}>
                    <Shield className="w-2.5 h-2.5" />
                    SSL {daysLeft}天
                  </span>
                );
              })()}
            </div>
            <div className="text-[10px] text-kumo-subtle truncate max-w-[320px] mt-1 select-all" onClick={(e) => e.stopPropagation()}>
              <span className="select-none">频率[{monitor.interval}s]</span>
              <span className="text-kumo-subtle/40 mx-1.5 select-none">•</span>
              {getDisplayUrl(monitor)}
            </div>
          </div>
        </div>

        {/* 右侧数据 & Heartbeat 迷你丸列 */}
        <div className="flex items-center gap-4 w-full cq-md:w-auto justify-between cq-md:justify-end flex-shrink-0">
          {/* 实时响应时延 & 可用率 */}
          <div className="flex items-center gap-3 text-right">
            <div className="flex flex-col">
              <span className="text-[9px] text-kumo-subtle select-none">时延</span>
              <span className="inline-block min-w-[6ch] text-right text-xs font-semibold tabular-nums text-kumo-strong">
                {lastBeat && lastBeat.status === 'up' ? `${lastBeat.ping}ms` : '--'}
              </span>
            </div>
            <div className="flex flex-col">
              <span className="text-[9px] text-kumo-subtle select-none">可用率</span>
              <span className={`inline-block min-w-[6ch] text-right text-xs font-semibold tabular-nums ${getUptimeRateClass(getUptimeRate(monitor.id, 30))}`}>
                {formatUptimeRateCompact(getUptimeRate(monitor.id, 30))}%
              </span>
            </div>
          </div>

          {/* 30 心跳丸小条 */}
          <div className="grid h-4 shrink-0 grid-cols-[repeat(30,4px)] items-center gap-[4px] select-none">
            {miniBeats.map((beat, idx) => {
              let colorClass = 'bg-kumo-line opacity-20';
              if (beat.status === 'up') colorClass = 'bg-kumo-success';
              if (beat.status === 'down') colorClass = 'bg-kumo-danger';
              if (beat.status === 'pending') colorClass = 'bg-kumo-warning';
              return (
                <div key={idx} className={`h-[14px] w-[4px] rounded-full ${colorClass}`} />
              );
            })}
          </div>
        </div>
      </div>

      {/* 卡片下半部详情抽屉 */}
      <AnimatedCollapse open={isExpanded}>
        <UptimeMonitorDetails
          monitor={monitor}
          heartbeats={beats}
          loading={!!uptimeHeartbeatLoading[monitor.id]}
          uptime24h={formatUptimeRateCompact(getUptimeRate(monitor.id, 1))}
          uptime30d={formatUptimeRateCompact(getUptimeRate(monitor.id, 30))}
          isDarkMode={isDarkMode}
          onPauseResume={onPauseResume}
          onEdit={onEdit}
          onDelete={onDelete}
          expanded={isExpanded}
        />
      </AnimatedCollapse>
    </div>
  );
}

export default MonitorCard;
