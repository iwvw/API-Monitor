import { toNumber, formatLatencyValue, getNetworkQualityTone, getNetworkQualityToneClass } from './utils.js';

export function NetworkQualitySummaryStrip({ summary = [] }) {
  if (!summary.length) return null;

  return (
    <div
      className="grid min-w-0 gap-1"
      style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(min(12rem, 100%), 1fr))' }}
    >
      {summary.map(item => {
        const tone = getNetworkQualityTone(item);
        const avgLatency = toNumber(item.avgLatency, 0);
        const latestValue = avgLatency > 0
          ? formatLatencyValue(avgLatency)
          : '失败';
        const caption = `抖动 ${formatLatencyValue(item.jitterMs)} · 丢包 ${toNumber(item.lossRate, 0).toFixed(1)}%`;

        return (
          <div
            key={item.name}
            className="grid min-h-6 min-w-0 grid-cols-[auto_auto_minmax(0,1fr)] items-baseline gap-x-1.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/15 px-2 py-1 text-[10px] leading-none"
          >
            <span className="shrink-0 font-semibold text-kumo-subtle">{item.name}</span>
            <span className={`shrink-0 text-xs font-semibold tabular-nums ${getNetworkQualityToneClass(tone)}`} title={`24h 平均 ${latestValue}`}>
              {latestValue}
            </span>
            <span className="min-w-0 truncate font-medium text-kumo-subtle" title={caption}>
              {caption}
            </span>
          </div>
        );
      })}
    </div>
  );
}
