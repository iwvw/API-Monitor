import { Meter } from '@cloudflare/kumo';
import { clampPercent } from './utils.js';

export function TrafficTotalSummary({ txTotal, rxTotal, quota, compact = false }) {
  const remainingPercent = quota ? clampPercent(100 - quota.percent) : 0;
  const unlimited = !!quota?.unlimited;

  if (compact) {
    return (
      <div className="grid min-w-0 grid-cols-2 gap-1 rounded-md bg-kumo-recessed/20 p-1 cq-sm:flex cq-sm:h-full cq-sm:flex-col cq-sm:justify-center cq-sm:gap-1">
        <div className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1">
          <div className="text-[10px] font-semibold leading-none text-kumo-subtle">累计上行</div>
          <div className="mt-0.5 truncate text-xs font-semibold tabular-nums text-kumo-info" title={txTotal?.text || '-'}>
            {txTotal?.text || '-'}
          </div>
        </div>
        <div className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1">
          <div className="text-[10px] font-semibold leading-none text-kumo-subtle">累计下行</div>
          <div className="mt-0.5 truncate text-xs font-semibold tabular-nums text-kumo-success" title={rxTotal?.text || '-'}>
            {rxTotal?.text || '-'}
          </div>
        </div>
        {quota && (
          <div className="col-span-2 min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1">
            <Meter
              label="剩余流量"
              value={unlimited ? 100 : remainingPercent}
              min={0}
              max={100}
              customValue={unlimited ? '∞' : `${remainingPercent.toFixed(remainingPercent >= 10 ? 0 : 1)}%`}
              className="gap-0.5 text-[10px] font-semibold leading-none text-kumo-subtle"
              trackClassName="!h-1 overflow-hidden rounded-full bg-kumo-base"
              indicatorClassName={`!h-full !bg-none ${unlimited ? '!bg-kumo-info' : quota.overLimit ? '!bg-kumo-danger' : quota.nearAlert ? '!bg-kumo-warning' : '!bg-kumo-info'}`}
            />
          </div>
        )}
      </div>
    );
  }

  const itemClassName = 'px-2.5 py-2';
  const valueClassName = 'text-sm';

  return (
    <div className="grid min-w-0 grid-cols-2 gap-1.5 cq-sm:grid-cols-1">
      <div className={`min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 ${itemClassName}`}>
        <div className="text-[10px] font-semibold text-kumo-subtle">累计上行</div>
        <div className={`mt-0.5 truncate font-semibold tabular-nums text-kumo-info ${valueClassName}`} title={txTotal?.text || '-'}>
          {txTotal?.text || '-'}
        </div>
      </div>
      <div className={`min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 ${itemClassName}`}>
        <div className="text-[10px] font-semibold text-kumo-subtle">累计下行</div>
        <div className={`mt-0.5 truncate font-semibold tabular-nums text-kumo-success ${valueClassName}`} title={rxTotal?.text || '-'}>
          {rxTotal?.text || '-'}
        </div>
      </div>
      {quota && (
        <div className={`col-span-2 min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 cq-sm:col-span-1 ${itemClassName}`}>
          <Meter
            label="剩余流量"
            value={unlimited ? 100 : remainingPercent}
            min={0}
            max={100}
            customValue={unlimited ? '∞' : `${remainingPercent.toFixed(remainingPercent >= 10 ? 0 : 1)}%`}
            className="gap-1 text-[10px] font-semibold text-kumo-subtle"
            trackClassName="!h-1.5 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-base"
            indicatorClassName={`!h-full !bg-none ${unlimited ? '!bg-kumo-info' : quota.overLimit ? '!bg-kumo-danger' : quota.nearAlert ? '!bg-kumo-warning' : '!bg-kumo-info'}`}
          />
        </div>
      )}
    </div>
  );
}
