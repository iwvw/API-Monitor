import { Popover } from '@cloudflare/kumo';
import { StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { statusCodeTone } from './utils.js';

export function FailoverPathBadge({ path, endpointName }) {
  let steps = [];
  if (path) {
    try {
      const parsed = JSON.parse(path);
      if (Array.isArray(parsed)) steps = parsed;
    } catch (e) {}
  }
  if (steps.length < 2) return <span className="break-all">{endpointName}</span>;
  return (
    <Popover>
      <Popover.Trigger
        nativeButton={false}
        render={
          <span className="cursor-pointer break-all font-medium text-kumo-warning">
            {endpointName}
          </span>
        }
      />
      <Popover.Content className="p-3 max-w-xs">
        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
          渠道迁移路径
        </Popover.Title>
        <div className="mt-2 flex flex-col gap-1">
          {steps.map((s, i) => (
            <div key={`${s.endpoint}-${i}`} className="flex items-center gap-1.5 text-xs">
              <span className="font-mono truncate max-w-[140px]" title={s.endpoint}>
                {s.endpoint || 'unknown'}
              </span>
              <StatusBadge tone={statusCodeTone(s.status)}>{s.status || '-'}</StatusBadge>
              {typeof s.keyIndex === 'number' && s.keyIndex >= 0 && (
                <StatusBadge tone="info" title="该步使用的 API Key 序号（K1=主 key）">
                  K{s.keyIndex + 1}
                </StatusBadge>
              )}
              {i < steps.length - 1 && <span className="text-kumo-subtle">→</span>}
            </div>
          ))}
        </div>
      </Popover.Content>
    </Popover>
  );
}
