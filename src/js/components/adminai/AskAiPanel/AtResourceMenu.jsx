import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { MENTION_GROUPS } from './constants.jsx';

function AtResourceMenu({ resources, tab, setTab, q, setQ, loading, error, onInsert }) {
  const group = MENTION_GROUPS.find((g) => g.type === tab) || MENTION_GROUPS[0];
  const Icon = group.icon;
  const all = resources[tab] || [];
  const list = q
    ? all.filter((r) => (r.name || '').toLowerCase().includes(q.toLowerCase()))
    : all;
  return (
    <div className="absolute bottom-full left-2 z-40 mb-1 flex w-[22rem] overflow-hidden rounded-xl bg-kumo-base shadow-lg ring-1 ring-kumo-line dark:bg-kumo-base">
      {/* 左侧：资源类型导航（图标 + 名称）；minHeight 固定，不随右侧列表高度变化 */}
      <div className="flex w-max shrink-0 flex-col gap-0.5 border-r border-kumo-line bg-kumo-recessed/30 p-1" style={{ minHeight: 300 }}>
        {MENTION_GROUPS.map((g) => {
          const GI = g.icon;
          return (
            <Button
              key={g.type}
              type="button"
              size="sm"
              variant="ghost"
              title={g.label}
              onClick={() => { setTab(g.type); setQ(''); }}
              aria-label={g.label}
              className={`flex items-center gap-1.5 rounded-md px-2 py-1 text-[11px] focus-visible:!outline-none ${
                tab === g.type
                  ? 'bg-brand/15 font-medium text-brand'
                  : 'text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default'
              }`}
            >
              <GI className={`${g.sm ? 'h-3 w-3' : 'h-3.5 w-3.5'} shrink-0`} style={g.sm ? { fontSize: '0.75rem' } : undefined} />
              <span className="whitespace-nowrap">{g.label}</span>
            </Button>
          );
        })}
      </div>
      {/* 右侧：当前类型资源列表（与左列等高，列表区占满剩余） */}
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="border-b border-kumo-line px-2 py-1.5">
          <Input
            size="sm"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={`搜索${group.label}…`}
            aria-label={`搜索${group.label}`}
            className="w-full rounded-md border border-kumo-line/60 bg-kumo-recessed/60 px-2 py-1 text-[11px] text-kumo-default outline-none placeholder:text-kumo-subtle/60 focus:border-kumo-brand/60"
          />
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-1">
          {loading && list.length === 0 && <p className="px-3 py-2 text-xs text-kumo-subtle">加载中…</p>}
          {!loading && !error && list.length === 0 && (
            <p className="px-3 py-2 text-xs text-kumo-subtle">暂无{q ? '匹配结果' : `可引用${group.label}`}</p>
          )}
          {!loading && error && list.length === 0 && <p className="px-3 py-2 text-xs text-kumo-subtle">加载失败</p>}
          {list.map((r) => (
            <Button
              key={r.id || r.name}
              size="sm"
              variant="ghost"
              type="button"
              onClick={() => onInsert(r)}
              className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-xs text-kumo-default hover:bg-kumo-tint"
            >
              <Icon className={`${group.sm ? 'h-3 w-3' : 'h-3.5 w-3.5'} shrink-0 text-kumo-subtle`} style={group.sm ? { fontSize: '0.75rem' } : undefined} />
              <span className="truncate">{r.name}</span>
            </Button>
          ))}
        </div>
      </div>
    </div>
  );
}

export default AtResourceMenu;
