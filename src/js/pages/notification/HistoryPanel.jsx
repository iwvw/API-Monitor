import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppCard } from '../../components/ui/AppPrimitives.jsx';
import { History, RotateCw, Trash } from '../../components/Icons.jsx';
import { parseLifecycleHistoryMeta } from '../../modules/notificationHistory.js';
import { getChannelTypeName, getSourceModuleName, getEventTypeName } from './constants.js';
import { formatHistoryDate } from './utils.js';

export function HistoryPanel({
  notificationLoading,
  notificationHistory,
  filteredHistory,
  notificationHistoryFilter,
  setNotificationHistoryFilter,
  notificationChannels,
  loadNotificationHistory,
  handleClearHistory,
}) {
  return (
    <div className="space-y-4">
      {/* 筛选控制条 */}
      <div className="flex items-center justify-between pb-2 gap-3 select-none">
        <div className="flex items-center gap-2">
          <Select alignItemWithTrigger
            aria-label="通知历史状态筛选" size="sm"
            value={notificationHistoryFilter}
            onValueChange={setNotificationHistoryFilter}
            placeholder="全部状态"
            items={[
              { value: '', label: '全部状态' },
              { value: 'sent', label: '已发送' },
              { value: 'failed', label: '失败' },
              { value: 'pending', label: '队列中' },
            ]}
          />

          <Button
            onClick={loadNotificationHistory}
            loading={notificationLoading}
          variant="secondary" size="sm"
          shape="square"
          aria-label="刷新通知历史"
          className="text-kumo-subtle hover:text-kumo-strong"
          title="刷新"
            icon={<RotateCw className="w-3.5 h-3.5" />}
          />
        </div>

        {notificationHistory.length > 0 && (
          <Button
            variant="destructive" size="sm"
            onClick={handleClearHistory}
            icon={<Trash className="w-3.5 h-3.5" />}
          >
            清空历史
          </Button>
        )}
      </div>

      {notificationLoading && notificationHistory.length === 0 ? (
        <div className="space-y-3">
          {[...Array(3)].map((_, i) => (
            <AppCard key={i} padding="none" className="space-y-3 p-4">
              <div className="flex items-center justify-between">
                <SkeletonLine className="w-1/4 h-3.5" />
                <SkeletonLine className="w-1/6 h-2.5" />
              </div>
              <SkeletonLine className="w-full h-12 rounded-md" />
            </AppCard>
          ))}
        </div>
      ) : filteredHistory.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
          <History className="w-12 h-12 opacity-30 mb-4" />
          <div className="text-sm">暂无匹配的通知历史记录</div>
        </div>
      ) : (
        <div className="flex flex-col gap-2.5">
          {filteredHistory.map((log) => {
            const lifecycleMeta = log.lifecycle_meta || parseLifecycleHistoryMeta(log.data);
            const mutationLabel = lifecycleMeta ? ({
              open: '告警打开',
              refresh: '动态更新',
              resolve: '告警恢复',
            }[lifecycleMeta.mutation] || lifecycleMeta.mutation) : null;

            // 从 data JSON 提取来源模块与事件类型（历史行无独立列）
            let logEventType = '';
            let logSourceModule = '';
            if (log.data) {
              try {
                const parsed = typeof log.data === 'string' ? JSON.parse(log.data) : log.data;
                logEventType = parsed.eventType || parsed.event_type || '';
                logSourceModule = parsed.sourceModule || parsed.source_module || '';
              } catch { /* ignore malformed data */ }
            }

            // 获取匹配的通知渠道名称
            const matchedChannel = notificationChannels.find(c => String(c.id) === String(log.channel_id));
            const channelDisplayName = log.channel_name || matchedChannel?.name || (log.channel_type ? getChannelTypeName(log.channel_type) : null);

            return (
              <AppCard
                key={log.id}
                padding="none"
                className="grid grid-cols-1 gap-3 p-3.5 hover:border-brand/40 cq-md:grid-cols-[280px_1fr] cq-md:items-start"
              >
                {/* 左栏：状态与元数据快照 (固定 280px 宽度，饱满工整) */}
                <div className="flex flex-col gap-2 min-w-0 pr-0 cq-md:pr-3.5 cq-md:border-r cq-md:border-kumo-line/50">
                  {/* 1. 状态指示 + 标题 + 投递状态 */}
                  <div className="flex items-center justify-between gap-2 min-w-0">
                    <div className="flex items-center gap-2 min-w-0">
                      <span className={`h-2 w-2 rounded-full shrink-0 ${
                        log.status === 'sent'
                          ? 'bg-kumo-success animate-pulse'
                          : log.status === 'failed'
                            ? 'bg-kumo-danger'
                            : 'bg-kumo-warning'
                      }`} />
                      <span className="text-[13px] font-semibold text-kumo-strong truncate leading-snug" title={log.title}>
                        {log.title}
                      </span>
                    </div>
                    <Badge className={`text-[10px] font-semibold px-2 py-0.5 border shrink-0 ${
                      log.status === 'sent'
                        ? 'bg-kumo-success/10 text-kumo-success border-kumo-success/20'
                        : log.status === 'failed'
                          ? 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20'
                          : 'bg-kumo-warning/10 text-kumo-warning border-kumo-warning/20'
                    }`}>
                      {log.status === 'sent' ? '发送成功' : log.status === 'failed' ? '发送失败' : '队列处理中'}
                    </Badge>
                  </div>

                  {/* 2. 投递渠道标识 (Notification Channel) */}
                  {channelDisplayName && (
                    <div className="flex items-center gap-1.5 text-[11px] text-kumo-subtle select-none">
                      <span className="font-medium">📢 渠道:</span>
                      <span className="rounded border border-kumo-line/60 bg-kumo-recessed/60 px-1.5 py-0.5 text-[10px] font-medium text-kumo-strong">
                        {channelDisplayName}
                      </span>
                    </div>
                  )}

                  {/* 2.5 来源模块与事件类型标识（cron 等链路可溯源） */}
                  {(logSourceModule || logEventType) && (
                    <div className="flex flex-wrap items-center gap-1.5 text-[10px]">
                      {logSourceModule && (
                        <Badge className="border border-brand/25 bg-brand/10 text-[10px] font-semibold text-brand py-0.5 px-2">
                          {getSourceModuleName(logSourceModule)}
                        </Badge>
                      )}
                      {logEventType && (
                        <span className="rounded border border-kumo-line/60 bg-kumo-recessed/60 px-1.5 py-0.5 font-mono text-[10px] text-kumo-subtle">
                          {getEventTypeName(logEventType.replace(/^cron\./, ''))}
                        </span>
                      )}
                    </div>
                  )}

                  {/* 3. 动态生命周期 Badges & 细节 */}
                  {lifecycleMeta && (
                    <div className="flex flex-wrap items-center gap-1.5 text-[10px]">
                      <Badge className="border border-brand/25 bg-brand/10 text-[10px] font-semibold text-brand py-0.5 px-2">
                        ↻ {mutationLabel}
                      </Badge>
                      {lifecycleMeta.kind && (
                        <span className="rounded bg-kumo-recessed px-1.5 py-0.5 font-mono text-[10px] text-kumo-subtle border border-kumo-line/40">
                          {lifecycleMeta.kind}
                        </span>
                      )}
                      {log.lifecycle_update_count > 1 && (
                        <span className="rounded border border-brand/20 bg-brand/5 px-1.5 py-0.5 text-[10px] font-medium text-kumo-subtle">
                          更新 {log.lifecycle_update_count - 1} 次
                        </span>
                      )}
                      {lifecycleMeta.duration && (
                        <span className="text-[11px] text-kumo-subtle">
                          持续 {lifecycleMeta.duration}
                        </span>
                      )}
                      {lifecycleMeta.changedFields.length > 0 && (
                        <span className="text-[11px] text-kumo-subtle" title={lifecycleMeta.changedFields.join(', ')}>
                          变化 {lifecycleMeta.changedFields.length} 项
                        </span>
                      )}
                    </div>
                  )}

                  {/* 4. 时间记录 */}
                  <div className="font-mono text-[11px] text-kumo-subtle select-none pt-0.5 flex items-center gap-1">
                    <span className="opacity-60">🕒</span>
                    {formatHistoryDate(log.created_at)}
                  </div>
                  {log.lifecycle_update_count > 1 && log.lifecycle_first_created_at && log.lifecycle_first_created_at !== log.created_at && (
                    <div className="font-mono text-[11px] text-kumo-subtle/80 select-none flex items-center gap-1">
                      <span className="opacity-50">↺</span>
                      首次告警 {formatHistoryDate(log.lifecycle_first_created_at)}
                    </div>
                  )}
                </div>

                {/* 右栏：详细消息内容与异常/重试提示 */}
                <div className="min-w-0 flex-1 space-y-1.5">
                  {log.message && (
                    <div className="rounded-md border border-kumo-line/60 bg-kumo-recessed/30 px-3.5 py-2.5 font-mono text-[11px] leading-relaxed text-kumo-subtle whitespace-pre-wrap break-all">
                      {log.message}
                    </div>
                  )}

                  {(log.error_message || log.retry_count > 0) && (
                    <div className="flex flex-wrap items-center gap-2 pt-0.5 select-none">
                      {log.error_message && (
                        <span className="rounded border border-kumo-danger/20 bg-kumo-danger/10 px-2 py-0.5 text-[11px] font-semibold text-kumo-danger">
                          报错: {log.error_message}
                        </span>
                      )}
                      {log.retry_count > 0 && (
                        <span className="rounded border border-kumo-warning/20 bg-kumo-warning/10 px-2 py-0.5 font-mono text-[11px] font-semibold text-kumo-warning">
                          重试: {log.retry_count} 次
                        </span>
                      )}
                    </div>
                  )}
                </div>
              </AppCard>
            );
          })}
        </div>
      )}
    </div>
  );
}
