import { Popover } from '@cloudflare/kumo';
import { maskIp } from './utils.js';

export function IpCell({ value, viaProxy, placeholder, v6EdgeOnly }) {
  if (!value) return <>{placeholder || '—'}</>;
  return (
    <Popover>
      <Popover.Trigger
        nativeButton={false}
        render={
          <span
            className={`cursor-pointer truncate ${viaProxy ? 'text-kumo-info' : ''}`}
          >
            {maskIp(value, v6EdgeOnly)}
          </span>
        }
      />
      <Popover.Content className="p-3 max-w-xs">
        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
          {viaProxy ? '出口 IP' : '客户端 IP'}
        </Popover.Title>
        <div className="mt-2">
          <code className="rounded bg-kumo-surface-2 px-2 py-1 text-xs font-mono text-kumo-strong select-all">
            {value}
          </code>
        </div>
      </Popover.Content>
    </Popover>
  );
}
