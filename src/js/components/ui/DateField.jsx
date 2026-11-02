import React, { useMemo, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { DatePicker } from '@cloudflare/kumo/components/date-picker';
import { Popover } from '@cloudflare/kumo/components/popover';
import { Select } from '@cloudflare/kumo/components/select';
import { CalendarDotsIcon } from '@phosphor-icons/react';
import { zhCN } from 'react-day-picker/locale';
import { cx } from './AppPrimitives.jsx';

// DayPicker 的年/月下拉默认渲染原生 select 元素：样式原生、且弹出的选项列表会
// 溢出弹窗。这里用 Kumo Select 覆盖 components.Dropdown，保持全站视觉一致。
// DayPicker 传入的是原生 select 契约（value + onChange(event)），转成 Kumo 的
// items + onValueChange。
function KumoDayPickerDropdown({ options, value, onChange, disabled, className, 'aria-label': ariaLabel }) {
  const items = (options || []).map(option => ({
    value: String(option.value),
    label: option.label,
    disabled: option.disabled,
  }));
  return (
    <Select
      alignItemWithTrigger
      size="sm"
      aria-label={ariaLabel}
      disabled={disabled}
      value={value === undefined || value === null ? '' : String(value)}
      onValueChange={next => {
        onChange?.({ target: { value: next } });
      }}
      items={items}
      className={cx('w-[4.5rem] shrink-0', className)}
    />
  );
}

// 下拉导航容器：DayPicker 固定「月在前、年在后」，这里重排为「年在前后」。
// 用 key 判定（月下拉 key="month"、年下拉 key="year"）。
function KumoDropdownNav({ children, ...props }) {
  const reordered = React.Children.toArray(children).sort((a, b) => {
    const rank = node => (React.isValidElement(node) && String(node.key ?? '').includes('year') ? 0 : 1);
    return rank(a) - rank(b);
  });
  return <div {...props}>{reordered}</div>;
}

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
              className={cx('app-date-picker', showDropdown && 'app-date-picker--dropdown')}
              mode="single"
              selected={selected}
              onChange={select}
              disabled={disabledDays}
              fixedWeeks
              locale={zhCN}
              navLayout="around"
              captionLayout={showDropdown ? 'dropdown' : 'label'}
              startMonth={startMonth}
              endMonth={endMonth}
              components={{
                Dropdown: KumoDayPickerDropdown,
                DropdownNav: KumoDropdownNav,
              }}
              footer={
                <span className="block w-full text-[11px] text-kumo-subtle">
                  以站点时区显示，选中后按当日零点保存
                </span>
              }
            />
            {value && (
              <div className="mt-2 flex justify-start border-t border-kumo-line pt-2">
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
