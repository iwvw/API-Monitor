import React, { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { cx } from '../../components/ui/AppPrimitives.jsx';
import { Check, Edit, ExternalLink, Trash } from '../../components/Icons.jsx';

/**
 * 公开页书签的右键菜单。
 *
 * 仅登录态挂载（未登录时父组件不渲染它，也不会阻止浏览器默认菜单）。
 * 菜单定位在鼠标处，超出视口时会自动贴边，避免被裁掉。
 *
 * 两种形态，由 state.item 是否存在决定：
 *   - state.item 有值：书签卡片上右键 → 打开 / 编辑 / 删除
 *   - state.item 为空：页面空白处右键 → 布局切换（密度）
 */
export default function BookmarkContextMenu({
  state, onClose, onEdit, onDelete, onOpen,
  density = 'cozy', densityOptions = [], onDensityChange,
}) {
  const ref = useRef(null);
  const [pos, setPos] = useState({ x: 0, y: 0 });

  const open = Boolean(state);

  // 先按鼠标位置渲染，再根据实际尺寸做视口内收正
  useLayoutEffect(() => {
    if (!open || !state) return;
    const el = ref.current;
    const { innerWidth, innerHeight } = window;
    const rect = el?.getBoundingClientRect();
    const width = rect?.width ?? 160;
    const height = rect?.height ?? 120;
    setPos({
      x: Math.min(state.x, Math.max(0, innerWidth - width - 8)),
      y: Math.min(state.y, Math.max(0, innerHeight - height - 8)),
    });
  }, [open, state]);

  useEffect(() => {
    if (!open) return undefined;
    const close = () => onClose?.();
    const onKey = (event) => { if (event.key === 'Escape') close(); };
    // 点击菜单以外的任意位置（空白处、其他书签）都关闭菜单。
    // 用捕获阶段的 pointerdown，保证早于被点元素的 onClick 生效，
    // 且能在 onContextMenu（右键）之前收到——右键换目标时会先关闭再重新打开。
    const onPointerDown = (event) => {
      if (ref.current?.contains(event.target)) return;
      close();
    };
    // 滚动/改变窗口大小时关闭，避免菜单与目标错位
    window.addEventListener('scroll', close, true);
    window.addEventListener('resize', close);
    document.addEventListener('pointerdown', onPointerDown, true);
    document.addEventListener('contextmenu', onPointerDown, true);
    document.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('scroll', close, true);
      window.removeEventListener('resize', close);
      document.removeEventListener('pointerdown', onPointerDown, true);
      document.removeEventListener('contextmenu', onPointerDown, true);
      document.removeEventListener('keydown', onKey);
    };
  }, [open, onClose]);

  if (!open || !state) return null;

  const item = state.item;

  // 圆角/内距对齐 Kumo DropdownMenu.Content 的规范：
  // 容器 rounded-lg + p-1.5，条目 rounded-md + px-2 py-1.5。
  // 原来的 rounded-xl + p-1 与 Kumo 弹层不一致，视觉上「圆过头、挤」。
  return (
    <div
      ref={ref}
      role="menu"
      aria-label={item ? `${item.title || '书签'} 操作` : '页面布局'}
      className="public-glass-blur fixed z-50 flex min-w-[10rem] flex-col gap-0.5 rounded-lg p-1.5 text-sm shadow-lg ring-1 ring-kumo-line"
      style={{ left: pos.x, top: pos.y }}
      // 菜单自身也要阻止右键，否则在菜单上再次右键会弹出浏览器菜单
      onContextMenu={(event) => event.preventDefault()}
    >
      {item ? (
        <>
          <MenuItem
            icon={<ExternalLink className="h-3.5 w-3.5" />}
            label="打开"
            onClick={() => { onOpen?.(item); onClose?.(); }}
          />
          <MenuItem
            icon={<Edit className="h-3.5 w-3.5" />}
            label="编辑"
            onClick={() => { onEdit?.(item); onClose?.(); }}
          />
          {/* 分隔线：外层已有 gap-0.5，这里只补一点上下留白，避免和条目贴死 */}
          <div className="-mx-0.5 my-1 h-px bg-kumo-line/60" role="separator" />
          <MenuItem
            icon={<Trash className="h-3.5 w-3.5" />}
            label="删除"
            danger
            onClick={() => { onDelete?.(item); onClose?.(); }}
          />
        </>
      ) : (
        <>
          {/* 空白处右键：布局切换（原来在右上角工具条上的下拉） */}
          <div className="px-2 py-1 text-[11px] font-medium text-kumo-subtle">布局</div>
          {densityOptions.map(option => (
            <MenuItem
              key={option.value}
              // 选中项用勾号占位，保证所有条目文字左对齐、不因勾号抖动
              icon={density === option.value
                ? <Check className="h-3.5 w-3.5" />
                : <span className="inline-block h-3.5 w-3.5" aria-hidden="true" />}
              label={option.label}
              active={density === option.value}
              onClick={() => { onDensityChange?.(option.value); onClose?.(); }}
            />
          ))}
        </>
      )}
    </div>
  );
}

function MenuItem({ icon, label, onClick, danger, active }) {
  return (
    <Button
      type="button"
      role="menuitem"
      size="sm"
      variant="ghost"
      onClick={onClick}
      // 条目间距由容器 gap-0.5 提供，这里不再逐个加 margin
      className={cx(
        // Kumo Button size="sm" 自带固定高度 h-6.5，会把 py-1.5 吃掉，
        // 因此由 .public-menu-item 在 CSS 里兜底放开高度（见 app.css）。
        'public-menu-item w-full justify-start gap-2 rounded-md px-2 py-1.5 font-normal',
        danger
          ? 'text-kumo-danger hover:bg-kumo-danger/10'
          : 'text-kumo-strong hover:bg-kumo-recessed',
        // 当前生效的布局项加重字色，配合前面的勾号
        active && 'font-medium'
      )}
    >
      {icon}
      <span>{label}</span>
    </Button>
  );
}
