import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppCard } from '../../components/ui/AppPrimitives.jsx';
import { Bell, Mail, Send, Edit, Trash } from '../../components/Icons.jsx';
import { getChannelTypeName } from './constants.js';

export function ChannelsPanel({
  isArmed,
  notificationLoading,
  notificationChannels,
  handleOpenAddChannel,
  handleTestChannel,
  handleOpenEditChannel,
  handleDeleteChannel,
}) {
  return (
    <div className="space-y-4">
      {notificationLoading && notificationChannels.length === 0 ? (
        <div className="grid grid-cols-1 cq-md:grid-cols-2 cq-lg:grid-cols-3 cq-xl:grid-cols-4 gap-4">
          {[...Array(4)].map((_, i) => (
            <AppCard key={i} padding="none" className="space-y-4 p-4">
              <div className="flex min-w-0 items-start justify-between gap-3">
                <SkeletonLine className="w-8 h-8 rounded-lg" />
                <div className="flex-1 space-y-1.5">
                  <SkeletonLine className="w-1/2 h-3.5" />
                  <SkeletonLine className="w-1/3 h-2.5" />
                </div>
              </div>
              <SkeletonLine className="w-full h-1" />
            </AppCard>
          ))}
        </div>
      ) : notificationChannels.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
          <Bell className="w-12 h-12 opacity-30 mb-4" />
          <div className="text-sm">暂无通知渠道，配置通知以便故障时接收提醒</div>
          <Button size="sm" variant="primary" className="mt-4" onClick={handleOpenAddChannel}>
            创建第一个渠道
          </Button>
        </div>
      ) : (
        <div className="grid grid-cols-1 cq-md:grid-cols-2 cq-lg:grid-cols-3 cq-xl:grid-cols-4 gap-4">
          {notificationChannels.map((channel) => (
            <AppCard
              key={channel.id}
              padding="none"
              interactive
              className="flex min-h-[128px] flex-col justify-between p-4 hover:border-brand/50"
            >
              <div className="flex min-w-0 items-start justify-between gap-3">
                {/* Icon */}
                <div className={`w-8 h-8 rounded-lg flex items-center justify-center text-kumo-inverse text-base flex-shrink-0 shadow-xs ${
                  channel.type === 'email' ? 'bg-kumo-info' : 'bg-brand'
                }`}>
                  {channel.type === 'email' ? <Mail className="w-4 h-4" /> : <Send className="w-4 h-4" />}
                </div>

                {/* Information */}
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5 min-w-0">
                    <h4 className="text-xs font-semibold text-kumo-strong truncate leading-tight" title={channel.name}>
                      {channel.name}
                    </h4>
                    <span className={`h-1.5 w-1.5 rounded-full shrink-0 ${channel.enabled ? 'bg-kumo-success' : 'bg-kumo-subtle/50'}`} />
                  </div>
                  <p className="text-[10px] text-kumo-subtle mt-1 select-none font-medium">
                    {getChannelTypeName(channel.type)}
                  </p>
                </div>

                {/* Actions */}
                <div className="flex items-center gap-1.5 flex-shrink-0">
                  <Button
                    onClick={() => handleTestChannel(channel.id)}
                    variant="secondary" size="sm"
                    shape="square"
                    aria-label="测试投递"
                    title="测试投递"
                    icon={<Send className="w-3 h-3" />}
                  />
                  <Button
                    onClick={() => handleOpenEditChannel(channel)}
                    variant="secondary" size="sm"
                    shape="square"
                    aria-label="编辑通知渠道"
                    title="编辑"
                    icon={<Edit className="w-3.5 h-3.5" />}
                  />
                  <Button
                    onClick={() => handleDeleteChannel(channel.id)}
                    variant={isArmed(`channel:${channel.id}`) ? 'destructive' : 'secondary-destructive'} size="sm"
                    shape="square"
                    aria-label="删除通知渠道"
                    title="删除"
                    icon={<Trash className="w-3.5 h-3.5" />}
                  />
                </div>
              </div>

              <div className="flex items-center justify-between border-t border-kumo-line/60 pt-2.5 mt-3 select-none">
                <span className={`text-[10px] font-semibold flex items-center gap-1.5 ${
                  channel.enabled ? 'text-kumo-success' : 'text-kumo-subtle'
                }`}>
                  <span className={`w-1.5 h-1.5 rounded-full ${channel.enabled ? 'bg-kumo-success animate-pulse' : 'bg-kumo-subtle'}`} />
                  {channel.enabled ? '已启用投递' : '已暂停投递'}
                </span>
              </div>
            </AppCard>
          ))}
        </div>
      )}
    </div>
  );
}
