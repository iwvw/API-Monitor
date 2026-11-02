import React, { useLayoutEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Label } from '@cloudflare/kumo/components/label';
import { Select } from '@cloudflare/kumo/components/select';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import CountryFlag from '../../components/CountryFlag.jsx';
import { Copy } from '../../components/Icons.jsx';
import { TRAFFIC_UNITS } from './constants.js';
import { latencyChipClass, nodeCountryCode, preferredTrafficUnit, templateLanguage, trafficDisplayValue, trafficUnitBytes } from './utils.js';

export function NodeFlag({ node }) {
  const code = nodeCountryCode(node);
  if (!code) return null;
  return <CountryFlag preferSvg countryCode={code} className="h-3.5 w-5 shrink-0 !rounded-[2px] text-sm" />;
}

export function NodeHostQuality({ node, serverNameById }) {
  const hostName = node.traffic_server_id ? serverNameById.get(String(node.traffic_server_id)) || node.traffic_server_id : '';
  const orderMap = { '移动': 1, '联通': 2, '电信': 3 };
  const samples = Array.isArray(node?.quality)
    ? [...node.quality].sort((a, b) => {
        const orderA = orderMap[a.name] ?? 99;
        const orderB = orderMap[b.name] ?? 99;
        return orderA - orderB;
      }).slice(0, 3)
    : [];
  return (
    <div className="flex min-w-0 flex-col items-start gap-1 text-left">
      <span
        className={`inline-flex max-w-full items-center rounded-[3px] border px-1.5 py-0.5 text-[10px] font-semibold leading-4 ${hostName ? 'border-kumo-info/25 bg-kumo-info/10 text-kumo-info' : 'border-kumo-line bg-kumo-recessed/45 text-kumo-subtle'}`}
        title={hostName || '未绑定主机'}
      >
        <span className="truncate">{hostName || '未绑定'}</span>
      </span>
      <div className="flex max-w-full flex-wrap justify-start gap-1">
        {samples.length > 0 ? samples.map((item) => {
          const latency = Math.round(Number(item.avg_latency_ms ?? item.latency_ms) || 0);
          return (
            <span
              key={`${item.name}-${item.sampled_at || latency}`}
              className={`inline-flex items-center gap-1 rounded-[3px] border px-1.5 py-0.5 text-[10px] font-semibold leading-4 tabular-nums ${latencyChipClass(latency)}`}
              title={`${item.name || '线路'} 24h 平均 ${latency > 0 ? `${latency}ms` : '暂无延迟'}`}
            >
              <span className="max-w-8 truncate">{item.name || '-'}</span>
              <span>{latency > 0 ? `${latency}ms` : '-'}</span>
            </span>
          );
        }) : (
          <span className="inline-flex rounded-[3px] border border-kumo-line bg-kumo-recessed/45 px-1.5 py-0.5 text-[10px] font-semibold leading-4 text-kumo-subtle">
            暂无延迟
          </span>
        )}
      </div>
    </div>
  );
}

export function TemplateCodeEditor({ label, value, format, onChange }) {
  return (
    <CodeEditor
      value={value}
      onChange={onChange}
      language={templateLanguage(format)}
      label={label}
      minHeight="20rem"
    />
  );
}

export function LinkCopyButton({ label, text, onCopy, variant = 'secondary' }) {
  return (
    <Button
      size="sm"
      variant={variant}
      disabled={!text}
      onClick={() => onCopy(text, `${label} 链接已复制`)}
      className="gap-1.5"
    >
      <Copy className="h-3.5 w-3.5" />
      <span>{label}</span>
    </Button>
  );
}

export function TrafficSizeInput({ label, value, onChange }) {
  const [unit, setUnit] = useState(() => preferredTrafficUnit(value));

  return (
    <div className="min-w-0 space-y-1.5">
      <Label className="text-xs font-semibold text-kumo-subtle">{label}</Label>
      <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_4.75rem] gap-2">
        <Input
          size="sm"
          aria-label={label}
          type="number"
          min="0"
          step="0.001"
          value={trafficDisplayValue(value, unit)}
          onChange={(event) => onChange(Math.round((Number(event.target.value) || 0) * trafficUnitBytes(unit)))}
          className="w-full min-w-0"
        />
        <Select alignItemWithTrigger
          size="sm"
          aria-label={`${label}单位`}
          value={unit}
          onValueChange={(nextUnit) => setUnit(String(nextUnit))}
          items={TRAFFIC_UNITS.map(({ value: itemValue, label: itemLabel }) => ({ value: itemValue, label: itemLabel }))}
          className="w-full min-w-0"
        />
      </div>
    </div>
  );
}

export function MasonryGrid({ children, className = '' }) {
  const containerRef = useRef(null);
  const childArray = React.Children.toArray(children);
  const childKeys = childArray.map((child, index) => child.key || index).join('|');
  const [rowSpans, setRowSpans] = useState([]);

  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) return undefined;

    let frameId = null;
    const items = Array.from(container.children);
    const updateSpans = () => {
      frameId = null;
      const styles = getComputedStyle(container);
      const rowHeight = Number.parseFloat(styles.gridAutoRows) || 1;
      const rowGap = Number.parseFloat(styles.rowGap) || 0;
      const nextSpans = items.map((item) => {
        const content = item.firstElementChild || item;
        const height = content.getBoundingClientRect().height || item.scrollHeight;
        return Math.max(1, Math.ceil((height + rowGap) / (rowHeight + rowGap)));
      });
      setRowSpans((previous) => previous.length === nextSpans.length && previous.every((value, index) => value === nextSpans[index]) ? previous : nextSpans);
    };
    const scheduleUpdate = () => {
      if (frameId === null) frameId = requestAnimationFrame(updateSpans);
    };

    updateSpans();
    const resizeObserver = typeof ResizeObserver === 'function' ? new ResizeObserver(scheduleUpdate) : null;
    items.forEach((item) => resizeObserver?.observe(item.firstElementChild || item));
    resizeObserver?.observe(container);

    return () => {
      if (frameId !== null) cancelAnimationFrame(frameId);
      resizeObserver?.disconnect();
    };
  }, [childKeys]);

  return (
    <div ref={containerRef} className={`grid grid-flow-row-dense grid-cols-1 items-start gap-3 cq-lg:grid-cols-2 ${className}`} style={{ gridAutoRows: '1px' }}>
      {childArray.map((child, index) => (
        <div key={child.key || index} className="min-w-0 self-start" style={rowSpans[index] ? { gridRowEnd: `span ${rowSpans[index]}` } : undefined}>
          {child}
        </div>
      ))}
    </div>
  );
}
