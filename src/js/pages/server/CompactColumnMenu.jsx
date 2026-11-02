import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { HOST_COMPACT_COLUMNS } from './constants.js';

export function CompactColumnMenu({ menu, visibleColumns, onToggle, onShowAll, onClose }) {
  if (!menu.open) return null;

  return (
    <div
      className="fixed z-50 w-52 rounded-md border border-kumo-line bg-kumo-control p-2 text-xs"
      style={{ left: menu.x, top: menu.y }}
      onClick={(event) => event.stopPropagation()}
      onContextMenu={(event) => event.preventDefault()}
    >
      <div className="mb-2 flex items-center justify-between gap-2 border-b border-kumo-line pb-2">
        <span className="font-semibold text-kumo-strong">显示列</span>
        <Button type="button" size="sm" variant="ghost" onClick={onShowAll}>
          全部
        </Button>
      </div>
      <div className="flex max-h-72 flex-col gap-1.5 overflow-y-auto pr-1">
        {HOST_COMPACT_COLUMNS.map(column => (
          <Checkbox
            key={column.id}
            label={column.label}
            checked={visibleColumns.includes(column.id)}
            disabled={column.required}
            onCheckedChange={(checked) => onToggle(column.id, Boolean(checked))}
          />
        ))}
      </div>
      <div className="mt-2 border-t border-kumo-line pt-2 text-right">
        <Button type="button" size="sm" variant="secondary" onClick={onClose}>
          关闭
        </Button>
      </div>
    </div>
  );
}
