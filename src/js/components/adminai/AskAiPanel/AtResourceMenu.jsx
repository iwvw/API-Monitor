import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { MENTION_GROUPS } from './constants.jsx';

/* 子序列模糊匹配：字符按顺序出现即命中，连续命中加权。
 * 返回 null 表示不匹配，否则返回 { score, ranges }（ranges 供高亮）。 */
export function fuzzyMatch(query, text) {
  if (!query) return { score: 0, ranges: [] };
  const q = query.toLowerCase();
  const t = (text || '').toLowerCase();
  let qi = 0;
  let score = 0;
  let streak = 0;
  const ranges = [];
  for (let ti = 0; ti < t.length && qi < q.length; ti++) {
    if (t[ti] === q[qi]) {
      if (ranges.length > 0 && ranges[ranges.length - 1][1] === ti) {
        ranges[ranges.length - 1][1] = ti + 1;
      } else {
        ranges.push([ti, ti + 1]);
      }
      streak += 1;
      score += 1 + streak * 2;
      qi += 1;
    } else {
      streak = 0;
    }
  }
  if (qi < q.length) return null;
  // 前缀命中额外加权，短文本优先
  if (t.startsWith(q)) score += 10;
  score -= t.length * 0.01;
  return { score, ranges };
}

function Highlight({ text, ranges }) {
  if (!ranges || ranges.length === 0) return text;
  const nodes = [];
  let cursor = 0;
  ranges.forEach(([from, to], i) => {
    if (from > cursor) nodes.push(text.slice(cursor, from));
    nodes.push(<mark key={i} className="bg-brand/25 text-inherit">{text.slice(from, to)}</mark>);
    cursor = to;
  });
  if (cursor < text.length) nodes.push(text.slice(cursor));
  return nodes;
}

function AtResourceMenu({ resources, tab, setTab, q, setQ, loading, error, onInsert, onClose }) {
  const group = MENTION_GROUPS.find((g) => g.type === tab) || MENTION_GROUPS[0];
  const Icon = group.icon;
  const all = resources[tab] || [];
  const [activeIdx, setActiveIdx] = useState(0);
  const listRef = useRef(null);

  const list = useMemo(() => {
    if (!q) return all.map((r) => ({ item: r, ranges: [] }));
    return all
      .map((r) => ({ item: r, match: fuzzyMatch(q, r.name || '') }))
      .filter((e) => e.match)
      .sort((a, b) => b.match.score - a.match.score)
      .map((e) => ({ item: e.item, ranges: e.match.ranges }));
  }, [all, q]);

  // 结果变化后高亮项回到首条，避免索引越界
  useEffect(() => { setActiveIdx(0); }, [q, tab, list.length]);

  // 高亮项滚动进视野
  useEffect(() => {
    const el = listRef.current?.querySelector('[data-active="true"]');
    el?.scrollIntoView({ block: 'nearest' });
  }, [activeIdx]);

  // 键盘导航：上下移动高亮，Enter 选中。
  // 弹层与输入框是兄弟节点，keydown 不会冒泡到本容器，因此在捕获阶段挂 document 监听，
  // 保证在 Textarea 的发送处理之前拿到 Enter（capture 先于 target 冒泡）。
  useEffect(() => {
    const onKeyDown = (e) => {
      if (list.length === 0) return;
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        e.stopPropagation();
        setActiveIdx((i) => (i + 1) % list.length);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        e.stopPropagation();
        setActiveIdx((i) => (i - 1 + list.length) % list.length);
      } else if (e.key === 'Enter') {
        if (e.nativeEvent?.isComposing) return;
        e.preventDefault();
        e.stopPropagation();
        onInsert(list[activeIdx].item);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        onClose?.();
      }
    };
    document.addEventListener('keydown', onKeyDown, true);
    return () => document.removeEventListener('keydown', onKeyDown, true);
  }, [list, activeIdx, onInsert, onClose]);

  return (
    <div
      className="absolute bottom-full left-2 z-40 mb-1 flex w-[22rem] overflow-hidden rounded-xl bg-kumo-base shadow-lg ring-1 ring-kumo-line dark:bg-kumo-base"
      data-askai-menu
    >
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
        <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto p-1">
          {loading && list.length === 0 && <p className="px-3 py-2 text-xs text-kumo-subtle">加载中…</p>}
          {!loading && !error && list.length === 0 && (
            <p className="px-3 py-2 text-xs text-kumo-subtle">暂无{q ? '匹配结果' : `可引用${group.label}`}</p>
          )}
          {!loading && error && list.length === 0 && <p className="px-3 py-2 text-xs text-kumo-subtle">加载失败</p>}
          {list.map((entry, i) => (
            <Button
              key={entry.item.id || entry.item.name}
              size="sm"
              variant="ghost"
              type="button"
              data-active={i === activeIdx}
              onMouseEnter={() => setActiveIdx(i)}
              onClick={() => onInsert(entry.item)}
              className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-xs text-kumo-default ${
                i === activeIdx ? 'bg-kumo-tint' : 'hover:bg-kumo-tint'
              }`}
            >
              <Icon className={`${group.sm ? 'h-3 w-3' : 'h-3.5 w-3.5'} shrink-0 text-kumo-subtle`} style={group.sm ? { fontSize: '0.75rem' } : undefined} />
              <span className="truncate"><Highlight text={entry.item.name} ranges={entry.ranges} /></span>
            </Button>
          ))}
        </div>
      </div>
    </div>
  );
}

export default AtResourceMenu;
