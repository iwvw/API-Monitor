// TimeRangePicker —— CF 风格时段选择器（完整复刻：自定义范围输入 + 快捷时段 + 日历 + 时区 + Apply）。
// 仪表盘（TilesBoard）与模型网关数据看板（OpenAIPage）共用的时间粒度/范围切换控件。
import React, { useCallback, useMemo, useState } from 'react';
import { Button, DatePicker, Input, Popover } from '@cloudflare/kumo';
import { CalendarBlank } from '@phosphor-icons/react';

export const TIME_RANGE_QUICK = [
  { label: '过去 30 分钟', minutes: 30 },
  { label: '过去 1 小时', minutes: 60 },
  { label: '过去 6 小时', minutes: 360 },
  { label: '过去 12 小时', minutes: 720 },
  { label: '过去 24 小时', minutes: 1440 },
  { label: '过去 7 天', minutes: 10080 },
  { label: '过去 30 天', minutes: 43200 },
];

export function TimeRangePicker({ value, onApply, buttonClassName = '' }) {
  const [open, setOpen] = useState(false);
  const [customText, setCustomText] = useState('');
  const [range, setRange] = useState(undefined);

  const applyMinutes = useCallback((minutes, label) => {
    const days = Math.max(1, Math.ceil(minutes / 1440));
    const cfRange = minutes <= 1440 ? '24h' : minutes <= 10080 ? '7d' : '30d';
    onApply(days, cfRange, label);
    setOpen(false);
  }, [onApply]);

  const applyCustom = useCallback(() => {
    const m = String(customText || '').trim().match(/^(\d+)\s*(m|min|mins?|minutes?|h|hour|hours|d|day|days)$/i);
    if (!m) return;
    const n = Number(m[1]);
    const u = m[2].toLowerCase()[0];
    const minutes = u === 'm' ? n : u === 'h' ? n * 60 : n * 1440;
    applyMinutes(minutes, customText.trim());
  }, [customText, applyMinutes]);

  const applyDateRange = useCallback(() => {
    if (!range?.from || !range?.to) return;
    const days = Math.max(1, Math.round((range.to - range.from) / 86400000) + 1);
    const label = `${range.from.toLocaleDateString()} - ${range.to.toLocaleDateString()}`;
    const cfRange = days <= 1 ? '24h' : days <= 7 ? '7d' : '30d';
    onApply(days, cfRange, label);
    setOpen(false);
  }, [range, onApply]);

  // CF 风格实时建议：输入纯数字 → 过去 N 分钟/小时/天/周/月；带单位 → 匹配对应建议
  const suggestions = useMemo(() => {
    const t = String(customText || '').trim();
    if (!t) return [];
    if (/^\d+$/.test(t)) {
      return [`过去 ${t} 分钟`, `过去 ${t} 小时`, `过去 ${t} 天`, `过去 ${t} 周`, `过去 ${t} 月`];
    }
    const m = t.match(/^(\d+)\s*(m|min|mins|minutes?|h|hour|hours|d|day|days|w|week|weeks|mo|month|months)$/i);
    if (m) {
      const n = m[1];
      const u = m[2].toLowerCase();
      const unitMap = {
        m: '分钟', min: '分钟', mins: '分钟', minute: '分钟', minutes: '分钟',
        h: '小时', hour: '小时', hours: '小时',
        d: '天', day: '天', days: '天',
        w: '周', week: '周', weeks: '周',
        mo: '月', month: '月', months: '月',
      };
      const unit = unitMap[u];
      return unit ? [`过去 ${n} ${unit}`] : [];
    }
    return [];
  }, [customText]);

  const applySuggestion = useCallback((label) => {
    const m = label.match(/^过去 (\d+) (分钟|小时|天|周|月)$/);
    if (!m) return;
    const n = Number(m[1]);
    const unit = m[2];
    const minutes = unit === '分钟' ? n : unit === '小时' ? n * 60 : unit === '天' ? n * 1440 : unit === '周' ? n * 10080 : n * 43200;
    applyMinutes(minutes, `过去 ${n} ${unit}`);
  }, [applyMinutes]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <Button size="sm" icon={<CalendarBlank className="h-4 w-4" />} className={buttonClassName}>{value}</Button>
      </Popover.Trigger>
      <Popover.Content sideOffset={6} className="z-50 w-fit max-w-[calc(100vw-2rem)] overflow-hidden p-0">
        <div className="flex flex-col">
          {/* 顶部：自定义范围输入 */}
          <div className="border-b border-kumo-line p-1.5">
            <Input
              size="sm"
              type="text"
              value={customText}
              onChange={(e) => setCustomText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') applyCustom();
              }}
              placeholder="自定义范围：3h、3 hours、3 m..."
              aria-label="自定义时间范围"
              className="w-full"
            />
            {suggestions.length > 0 && (
              <div className="mt-1 flex flex-wrap gap-1">
                {suggestions.map((s) => (
                  <button
                    key={s}
                    type="button"
                    onClick={() => applySuggestion(s)}
                    className="rounded-md border border-kumo-line bg-kumo-base px-2 py-1 text-[11px] text-kumo-default transition-colors hover:border-brand/60 hover:bg-kumo-tint"
                  >
                    {s}
                  </button>
                ))}
              </div>
            )}
          </div>
          <div className="flex">
            {/* 左侧：快捷时段 */}
            <div className="flex w-32 shrink-0 flex-col gap-0.5 border-r border-kumo-line p-2">
              {TIME_RANGE_QUICK.map((q) => (
                <Button
                  key={q.label}
                  type="button"
                  size="xs"
                  variant="ghost"
                  onClick={() => applyMinutes(q.minutes, q.label)}
                  className="justify-start rounded-md px-2.5 py-1.5 text-left text-xs text-kumo-default transition-colors hover:bg-kumo-tint"
                >
                  {q.label}
                </Button>
              ))}
            </div>
            {/* 右侧：日历范围选择 */}
            <div className="min-w-0 flex-1 overflow-x-auto p-2">
              <DatePicker mode="range" selected={range} onChange={setRange} />
            </div>
          </div>
          {/* 底部：操作按钮 */}
          <div className="flex items-center justify-end gap-2 border-t border-kumo-line px-3 py-2">
            <Button size="sm" variant="ghost" onClick={() => setOpen(false)}>取消</Button>
            <Button size="sm" onClick={applyCustom} disabled={!/^(\d+\s*(m|min|mins?|minutes?|h|hour|hours|d|day|days))$/i.test(String(customText || '').trim())}>
              应用
            </Button>
          </div>
        </div>
      </Popover.Content>
    </Popover>
  );
}
