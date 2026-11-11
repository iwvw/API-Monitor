import { useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Popover, Badge } from '@cloudflare/kumo';
import { Plus } from '../../components/Icons.jsx';

export function MultiSelectPopover({ triggerLabel, options, selected, onToggle, onClear, searchPlaceholder, emptyText }) {
  const [search, setSearch] = useState('');
  const keyword = search.trim().toLowerCase();
  const filtered = keyword
    ? options.filter(o => String(o.value).toLowerCase().includes(keyword) || String(o.label).toLowerCase().includes(keyword))
    : options;
  return (
    <Popover>
      <Popover.Trigger
        nativeButton={false}
        render={
          <Button type="button" size="sm" variant="secondary" className="flex items-center gap-1.5">
            <Plus className="h-3 w-3" />
            {triggerLabel}
            {selected.length > 0 && <Badge variant="secondary">{selected.length}</Badge>}
          </Button>
        }
      />
      <Popover.Content side="bottom" align="start" className="w-72 p-2">
        <Input
          size="sm"
          type="text"
          value={search}
          onChange={e => setSearch(e.target.value)}
          placeholder={searchPlaceholder}
          className="w-full"
          aria-label={searchPlaceholder}
        />
        <div className="mt-1.5 max-h-60 overflow-y-auto overscroll-contain scrollbar-thin">
          {filtered.length === 0 ? (
            <p className="px-2 py-4 text-center text-xs leading-normal text-kumo-subtle">{emptyText}</p>
          ) : (
            <div className="grid gap-0.5">
              {filtered.map(option => (
                <label
                  key={option.value}
                  className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-kumo-recessed"
                >
                  <Checkbox
                    checked={selected.includes(option.value)}
                    onCheckedChange={checked => onToggle(option.value, !!checked)}
                    aria-label={`选择 ${option.label}`}
                  />
                  <span className="min-w-0 flex-1 truncate text-sm leading-normal text-kumo-strong">{option.label}</span>
                </label>
              ))}
            </div>
          )}
        </div>
        <div className="mt-1.5 flex items-center justify-between border-t border-kumo-line pt-1.5">
          <span className="text-xs leading-normal text-kumo-subtle">已选 {selected.length} 项</span>
          <Button
            type="button"
            size="xs"
            variant="ghost"
            disabled={selected.length === 0}
            onClick={onClear}
          >
            清空
          </Button>
        </div>
      </Popover.Content>
    </Popover>
  );
}
