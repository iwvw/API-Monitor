import React, { useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { DatePicker } from '@cloudflare/kumo/components/date-picker';
import { Popover } from '@cloudflare/kumo/components/popover';
import { Select } from '@cloudflare/kumo/components/select';
import { CalendarDotsIcon } from '@phosphor-icons/react';
import { formatDateFieldValue, parseDateFieldValue } from './DateField.jsx';

const HOURS = Array.from({ length: 24 }, (_, i) => ({ value: String(i).padStart(2, '0'), label: String(i).padStart(2, '0') }));
const MINUTES = Array.from({ length: 12 }, (_, i) => {
  const minute = String(i * 5).padStart(2, '0');
  return { value: minute, label: minute };
});

// 本地日期时间字符串（YYYY-MM-DDTHH:mm）与 Date 互转。
export const parseDateTimeFieldValue = value => {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
};

export const formatDateTimeFieldValue = date => {
  if (!date) return '';
  const pad = v => String(v).padStart(2, '0');
  return `${formatDateFieldValue(date)}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
};

// DateTimeField —— 日期 + 时/分选择（Popover + Kumo DatePicker + 时/分 Select），
// 替代原生 input type="datetime-local"。值为本地时间字符串 YYYY-MM-DDTHH:mm。
export function DateTimeField({ value, onChange, placeholder = '选择时间', disabled = false }) {
  const [open, setOpen] = useState(false);
  const datePart = String(value || '').slice(0, 10);
  const hourPart = String(value || '').slice(11, 13) || '00';
  const minutePart = String(value || '').slice(14, 16) || '00';
  // 已有值可能是任意分钟（如 :37），补进选项避免 Select 匹配不到。
  const minuteOptions = MINUTES.some(item => item.value === minutePart)
    ? MINUTES
    : [...MINUTES, { value: minutePart, label: minutePart }].sort((a, b) => a.value.localeCompare(b.value));

  const updateDate = date => {
    const next = parseDateFieldValue(date ? formatDateFieldValue(date) : '');
    if (!next) return;
    const base = parseDateTimeFieldValue(value) || new Date();
    next.setHours(base.getHours(), base.getMinutes(), 0, 0);
    onChange(formatDateTimeFieldValue(next));
    setOpen(false);
  };

  const updateTime = (part, next) => {
    const base = parseDateTimeFieldValue(value) || new Date();
    if (part === 'hour') base.setHours(Number(next));
    if (part === 'minute') base.setMinutes(Number(next));
    onChange(formatDateTimeFieldValue(base));
  };

  return (
    <div className="flex min-w-0 items-center gap-2">
      <Popover open={open} onOpenChange={setOpen}>
        <Popover.Trigger
          render={
            <Button
              size="sm"
              variant="outline"
              disabled={disabled}
              icon={CalendarDotsIcon}
              className="min-w-0 flex-1 justify-start font-normal"
            />
          }
        >
          <span className={`truncate ${datePart ? '' : 'text-kumo-subtle'}`}>{datePart || placeholder}</span>
        </Popover.Trigger>
        <Popover.Content className="p-3">
          <DatePicker
            mode="single"
            selected={parseDateFieldValue(datePart)}
            onChange={updateDate}
          />
          {value && (
            <div className="mt-2 flex justify-end border-t border-kumo-line pt-2">
              <Button
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
        </Popover.Content>
      </Popover>
      <Select
        alignItemWithTrigger
        size="sm"
        aria-label="小时"
        disabled={disabled}
        value={hourPart}
        onValueChange={next => updateTime('hour', next)}
        items={HOURS}
        className="w-[3.5rem] shrink-0"
      />
      <span className="shrink-0 text-sm text-kumo-subtle">:</span>
      <Select
        alignItemWithTrigger
        size="sm"
        aria-label="分钟"
        disabled={disabled}
        value={minutePart}
        onValueChange={next => updateTime('minute', next)}
        items={minuteOptions}
        className="w-[3.5rem] shrink-0"
      />
    </div>
  );
}
