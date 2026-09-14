import { Globe } from '../../components/Icons.jsx';

export function ProxyRuntimeMeta({ proxy, state }) {
  const hasExit = state && state.lastExitIP;
  const hasTTFB = state && Number(state.lastTTFB) > 0;
  if (!hasExit && !hasTTFB) return null;
  return (
    <div className="mt-0.5 flex items-center gap-2 font-mono text-[10px] leading-none text-kumo-subtle">
      {hasExit && (
        <span title={`出口 IP（经代理出网，探活记录）\n${proxy}`} className="truncate">
          <Globe className="mr-0.5 inline h-2.5 w-2.5" />{state.lastExitIP}
        </span>
      )}
      {hasTTFB && (
        <span title="最近一次请求的首字耗时">
          ~{(state.lastTTFB / 1000).toFixed(1)}s 首字
        </span>
      )}
    </div>
  );
}
