import React, { useMemo, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { DatePicker } from '@cloudflare/kumo/components/date-picker';
import { Popover } from '@cloudflare/kumo/components/popover';
import { CalendarDotsIcon } from '@phosphor-icons/react';
import { cx } from './AppPrimitives.jsx';

// 本地日期字符串（YYYY-MM-DD）与 Date 互转。原生 date 输入用的是本地日历日，
// 这里保持一致，避免 toISOString 的 UTC 偏移把日期挪一天。
export const parseDateFieldValue = value => {
  if (!value) return undefined;
  const [year, month, day] = String(value).slice(0, 10).split('-').map(Number);
  if (!year || !month || !day) return undefined;
  const date = new Date(year, month - 1, day);
  return Number.isNaN(date.getTime()) ? undefined : date;
};

export const formatDateFieldValue = date => {
  if (!date) return '';
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
};

const startOfToday = () => {
  const now = new Date();
  return new Date(now.getFullYear(), now.getMonth(), now.getDate());
};

const addDays = (base, days) => {
  const next = new Date(base);
  next.setDate(next.getDate() + days);
  return next;
};

const addMonths = (base, months) => {
  const next = new Date(base);
  next.setMonth(next.getMonth() + months);
  return next;
};

const addYears = (base, years) => {
  const next = new Date(base);
  next.setFullYear(next.getFullYear() + years);
  return next;
};

// 默认快捷预设：围绕「资产到期/购置」场景，以站点当前日期为基准。
const buildDefaults = (today, presets) => {
  if (presets) return presets;
  return [
    { label: '今天', date: today },
    { label: '7 天后', date: addDays(today, 7) },
    { label: '30 天后', date: addDays(today, 30) },
    { label: '3 个月后', date: addMonths(today, 3) },
    { label: '1 年后', date: addYears(today, 1) },
    { label: '3 年后', date: addYears(today, 3) },
  ];
};

// DateField —— 日期选择字段（Popover + Kumo DatePicker），替代原生 input type="date"。
// 值为本地日期字符串 YYYY-MM-DD，空串表示未选择。
//
// 可选项：
//   presets     自定义快捷预设 [{ label, date }]；传 null 关闭预设栏
//   minDate/maxDate  可选日期区间（含端点）
//   disablePast 禁用今天之前（购置日期等「不能回选过去」的场景）
//   yearRange   年月下拉的年份跨度（默认前后 50 年）
//   showDropdown 是否启用年月下拉（默认启用）
export function DateField({
  value,
  onChange,
  placeholder = '选择日期',
  disabled = false,
  className = '',
  presets,
  minDate,
  maxDate,
  disablePast = false,
  yearRange = 50,
  showDropdown = true,
}) {
  const [open, setOpen] = useState(false);
  const selected = parseDateFieldValue(value);

  const today = useMemo(() => startOfToday(), []);
  const quickPresets = useMemo(() => buildDefaults(today, presets), [today, presets]);

  const { startMonth, endMonth } = useMemo(() => {
    const base = minDate ? new Date(minDate) : today;
    return {
      startMonth: new Date(base.getFullYear() - yearRange, 0),
      endMonth: new Date((maxDate ? new Date(maxDate).getFullYear() : today.getFullYear()) + yearRange, 11),
    };
  }, [minDate, maxDate, today, yearRange]);

  const disabledDays = useMemo(() => {
    const matchers = [];
    if (disablePast) matchers.push({ before: today });
    if (minDate) matchers.push({ before: new Date(minDate) });
    if (maxDate) matchers.push({ after: new Date(maxDate) });
    return matchers.length > 0 ? matchers : undefined;
  }, [disablePast, minDate, maxDate, today]);

  const select = date => {
    if (!date) return;
    onChange(formatDateFieldValue(date));
    setOpen(false);
  };

  const matchedPreset = date => selected && formatDateFieldValue(date) === value;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        render={
          <Button
            size="sm"
            variant="outline"
            disabled={disabled}
            icon={CalendarDotsIcon}
            className={`min-w-0 justify-start font-normal ${className}`}
          />
        }
      >
        <span className={`truncate ${value ? '' : 'text-kumo-subtle'}`}>{value || placeholder}</span>
      </Popover.Trigger>
      <Popover.Content className="p-0">
        <div className="flex">
          {quickPresets && quickPresets.length > 0 && (
            <div className="flex flex-col gap-0.5 border-r border-kumo-line p-2">
              {quickPresets.map(preset => (
                <Button
                  key={preset.label}
                  type="button"
                  size="xs"
                  variant="ghost"
                  className={cx(
                    'justify-start whitespace-nowrap',
                    matchedPreset(preset.date) && 'bg-kumo-tint text-kumo-strong'
                  )}
                  onClick={() => select(preset.date)}
                >
                  {preset.label}
                </Button>
              ))}
            </div>
          )}
          <div className="p-3">
            <DatePicker
              mode="single"
              selected={selected}
              onChange={select}
              disabled={disabledDays}
              fixedWeeks
              captionLayout={showDropdown ? 'dropdown' : 'label'}
              startMonth={startMonth}
              endMonth={endMonth}
              footer={
                <span className="block w-full pt-2 text-[11px] text-kumo-subtle">
                  以站点时区显示，选中后按当日零点保存
                </span>
              }
            />
            {value && (
              <div className="mt-2 flex justify-end border-t border-kumo-line pt-2">
                <Button
                  type="button"
                  size="xs"
                  variant="ghost"
                  onClick={() => {
                    onChange('');
                    setOpen(false);
                  }}
                >
                  清除
                </Button>
              </div>
            )}
          </div>
        </div>
      </Popover.Content>
    </Popover>
  );
}
