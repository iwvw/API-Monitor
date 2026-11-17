import { useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Popover } from '@cloudflare/kumo';
import { Edit } from '../../components/Icons.jsx';

export function EndpointRoutingEditor({ item, onSave }) {
  const [open, setOpen] = useState(false);
  const [priority, setPriority] = useState('0');
  const [weight, setWeight] = useState('100');
  const [saving, setSaving] = useState(false);

  const handleOpenChange = next => {
    if (next) {
      setPriority(String(item.priority ?? 0));
      setWeight(String(item.weight ?? 100));
    }
    setOpen(next);
  };

  const handleSave = async () => {
    if (saving) return;
    setSaving(true);
    try {
      const nextPriority = Math.min(999, Math.max(0, Number(priority) || 0));
      const nextWeight = Math.min(9999, Math.max(1, Number(weight) || 1));
      if (nextPriority !== (item.priority ?? 0)) {
        await onSave(item.id, 'priority', nextPriority);
      }
      if (nextWeight !== (item.weight ?? 100)) {
        await onSave(item.id, 'weight', nextWeight);
      }
      setOpen(false);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <Popover.Trigger
        nativeButton={false}
        render={
          <Button
            size="sm"
            variant="secondary"
            icon={<Edit className="h-3.5 w-3.5" />}
            className="h-7 px-2 text-[11px]"
            title="编辑路由优先级与权重"
          >
            路由
          </Button>
        }
      />
      <Popover.Content side="bottom" align="start" className="w-56 p-3">
        <Popover.Title className="text-sm font-semibold text-kumo-strong">路由设置</Popover.Title>
        <div className="mt-2 grid gap-2">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs text-kumo-subtle">优先级</span>
            <Input
              size="sm"
              type="number"
              min={0}
              max={999}
              value={priority}
              onChange={event => setPriority(event.target.value)}
              onKeyDown={event => {
                event.stopPropagation();
                if (event.key === 'Enter') handleSave();
              }}
              aria-label="路由优先级(值越大越优先)"
              className="h-7 w-20 text-center font-mono text-xs"
            />
          </div>
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs text-kumo-subtle">权重</span>
            <Input
              size="sm"
              type="number"
              min={1}
              max={9999}
              value={weight}
              onChange={event => setWeight(event.target.value)}
              onKeyDown={event => {
                event.stopPropagation();
                if (event.key === 'Enter') handleSave();
              }}
              aria-label="同优先级内的加权因子"
              className="h-7 w-20 text-center font-mono text-xs"
            />
          </div>
        </div>
        <div className="mt-3 flex items-center justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={() => setOpen(false)}>
            取消
          </Button>
          <Button size="sm" variant="primary" loading={saving} disabled={saving} onClick={handleSave}>
            保存
          </Button>
        </div>
      </Popover.Content>
    </Popover>
  );
}