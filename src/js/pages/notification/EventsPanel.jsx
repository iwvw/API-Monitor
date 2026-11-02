import { Badge } from '@cloudflare/kumo/components/badge';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Info } from '../../components/Icons.jsx';
import { getSourceModuleName, getEventTypeName } from './constants.js';

export function EventsPanel({ notificationEventCatalog }) {
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2 text-[10px] text-kumo-subtle">
        <Badge className="border border-brand/25 bg-brand/10 text-brand">↻ 动态消息</Badge>
        <span>同一 Telegram 消息会随告警打开、变化和恢复持续更新；每次变化仍会写入通知历史。</span>
      </div>
      <div className="columns-1 gap-4 cq-md:columns-2 cq-xl:columns-3 [&>*]:mb-4 [&>*]:break-inside-avoid [&>*:last-child]:mb-0">
        {notificationEventCatalog.map((item) => (
          <SectionCard
            key={item.module}
            title={getSourceModuleName(item.module)}
            icon={<Info className="w-4 h-4 text-brand" />}
            meta={<span className="text-[10px] font-mono text-kumo-subtle">{item.events?.length || 0}</span>}
            bodyClassName="p-4"
          >
            <div className="flex flex-wrap gap-2">
              {(item.events || []).map((eventName) => {
                const isDynamic = (item.dynamic_events || []).includes(eventName);
                return isDynamic ? (
                  <Badge
                    key={`${item.module}-${eventName}`}
                    title="支持 Telegram 动态消息"
                    className="border border-brand/25 bg-brand/10 text-[10px] font-semibold text-brand"
                  >
                    ↻ {getEventTypeName(eventName)}
                  </Badge>
                ) : (
                  <span
                    key={`${item.module}-${eventName}`}
                    className="rounded border border-kumo-line bg-kumo-recessed px-2 py-1 text-[10px] font-semibold text-kumo-subtle"
                  >
                    {getEventTypeName(eventName)}
                  </span>
                );
              })}
            </div>
          </SectionCard>
        ))}
      </div>
    </div>
  );
}
