import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';

/* 斜杠命令：输入框行首输入 / 触发。
 * 只暴露已有能力（切换行为模式、新对话、停止生成），不引入新的后端语义。 */
export const SLASH_COMMANDS = [
  { name: 'agent', label: '切换到代理模式', hint: '可调用工具执行操作' },
  { name: 'ask', label: '切换到询问模式', hint: '只回答问题，不执行工具' },
  { name: 'new', label: '新建对话', hint: '开启一个全新会话' },
  { name: 'stop', label: '停止生成', hint: '中断当前执行' },
];

function SlashCommandMenu({ query, onPick, onClose }) {
  const [activeIdx, setActiveIdx] = useState(0);
  const list = useMemo(() => {
    const q = (query || '').toLowerCase();
    if (!q) return SLASH_COMMANDS;
    return SLASH_COMMANDS.filter((c) => c.name.includes(q) || c.label.includes(query));
  }, [query]);
  const listRef = useRef(null);

  useEffect(() => { setActiveIdx(0); }, [query, list.length]);

  useEffect(() => {
    const el = listRef.current?.querySelector('[data-active="true"]');
    el?.scrollIntoView({ block: 'nearest' });
  }, [activeIdx]);

  // 与 @ 菜单同理：与输入框是兄弟节点，需在捕获阶段拦截键盘。
  useEffect(() => {
    const onKeyDown = (e) => {
      if (list.length === 0) return;
      if (e.key === 'ArrowDown') {
        e.preventDefault(); e.stopPropagation();
        setActiveIdx((i) => (i + 1) % list.length);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault(); e.stopPropagation();
        setActiveIdx((i) => (i - 1 + list.length) % list.length);
      } else if (e.key === 'Enter') {
        if (e.nativeEvent?.isComposing) return;
        e.preventDefault(); e.stopPropagation();
        onPick(list[activeIdx].name);
      } else if (e.key === 'Escape') {
        e.preventDefault(); e.stopPropagation();
        onClose?.();
      }
    };
    document.addEventListener('keydown', onKeyDown, true);
    return () => document.removeEventListener('keydown', onKeyDown, true);
  }, [list, activeIdx, onPick, onClose]);

  if (list.length === 0) {
    return (
      <div className="absolute bottom-full left-2 z-40 mb-1 w-[20rem] rounded-xl bg-kumo-base p-3 text-xs text-kumo-subtle shadow-lg ring-1 ring-kumo-line">
        无匹配命令
      </div>
    );
  }

  return (
    <div
      ref={listRef}
      className="absolute bottom-full left-2 z-40 mb-1 w-[20rem] overflow-hidden rounded-xl bg-kumo-base p-1 shadow-lg ring-1 ring-kumo-line"
      data-askai-menu
    >
      {list.map((c, i) => (
        <Button
          key={c.name}
          type="button"
          size="sm"
          variant="ghost"
          data-active={i === activeIdx}
          onMouseEnter={() => setActiveIdx(i)}
          onClick={() => onPick(c.name)}
          className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left ${i === activeIdx ? 'bg-kumo-tint' : 'hover:bg-kumo-tint'}`}
        >
          <span className="shrink-0 font-mono text-[11px] text-brand">/{c.name}</span>
          <span className="min-w-0 flex-1 truncate text-xs text-kumo-default">{c.label}</span>
          <span className="shrink-0 text-[10px] text-kumo-subtle">{c.hint}</span>
        </Button>
      ))}
    </div>
  );
}

export default SlashCommandMenu;
