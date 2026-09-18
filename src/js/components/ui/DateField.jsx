import React, { useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { DatePicker } from '@cloudflare/kumo/components/date-picker';
import { Popover } from '@cloudflare/kumo/components/popover';
import { CalendarDotsIcon } from '@phosphor-icons/react';

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

// DateField —— 日期选择字段（Popover + Kumo DatePicker），替代原生 input type="date"。
// 值为本地日期字符串 YYYY-MM-DD，空串表示未选择。
export function DateField({ value, onChange, placeholder = '选择日期', disabled = false, className = '' }) {
  const [open, setOpen] = useState(false);

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
      <Popover.Content className="p-3">
        <DatePicker
          mode="single"
          selected={parseDateFieldValue(value)}
          onChange={date => {
            onChange(formatDateFieldValue(date));
            setOpen(false);
          }}
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
  );
}
