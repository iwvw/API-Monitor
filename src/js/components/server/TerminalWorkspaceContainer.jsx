import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo';
import { Terminal as TerminalIcon, X } from '../Icons.jsx';
import { getDropTargetRegion } from '../../modules/terminalSplitTree.js';

/**
 * 拖拽预览指示器样式计算（平滑吸附高亮）
 */
function getDropIndicatorStyle(region) {
  const gap = '4px';
  switch (region) {
    case 'left':
      return { left: gap, top: gap, bottom: gap, width: 'calc(50% - 6px)' };
    case 'right':
      return { right: gap, top: gap, bottom: gap, width: 'calc(50% - 6px)' };
    case 'top':
      return { left: gap, right: gap, top: gap, height: 'calc(50% - 6px)' };
    case 'bottom':
      return { left: gap, right: gap, bottom: gap, height: 'calc(50% - 6px)' };
    case 'center':
    default:
      return { left: gap, right: gap, top: gap, bottom: gap };
  }
}

/**
 * TerminalPaneView - 单个终端视窗卡片（叶子节点）
 */
function TerminalPaneView({
  node,
  index,
  session,
  isActive,
  onActivate,
  onClose,
  onDuplicate,
  draggedSessionId,
  onDragStart,
  onSplitDrop,
}) {
  const [dropRegion, setDropRegion] = useState(null);
  const paneRef = useRef(null);

  const handleDragOver = useCallback((e) => {
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    if (!draggedSessionId || draggedSessionId === node.id) {
      setDropRegion(null);
      return;
    }
    if (!paneRef.current) return;
    const rect = paneRef.current.getBoundingClientRect();
    const region = getDropTargetRegion(rect, e.clientX, e.clientY);
    setDropRegion(region);
  }, [draggedSessionId, node.id]);

  const handleDragLeave = useCallback((e) => {
    const nextTarget = e.relatedTarget;
    if (nextTarget instanceof Node && e.currentTarget.contains(nextTarget)) return;
    setDropRegion(null);
  }, []);

  const handleDrop = useCallback((e) => {
    e.preventDefault();
    const sourceId = draggedSessionId || e.dataTransfer?.getData('text/plain');
    if (sourceId && sourceId !== node.id && dropRegion) {
      onSplitDrop(node.id, sourceId, dropRegion);
    }
    setDropRegion(null);
  }, [draggedSessionId, node.id, dropRegion, onSplitDrop]);

  return (
    <div
      ref={paneRef}
      onMouseDown={() => onActivate(node.id)}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      className={`relative flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden rounded-md border bg-kumo-base transition-colors ${
        isActive ? 'border-kumo-interact ring-1 ring-kumo-interact/30' : 'border-kumo-line'
      }`}
    >
      {/* 终端头部工具条（可拖动） */}
      <div
        draggable
        onDragStart={(e) => onDragStart(e, node.id)}
        className="flex h-8 cursor-move select-none items-center justify-between border-b border-kumo-line bg-kumo-base px-2 text-[10px] text-kumo-subtle hover:bg-kumo-recessed/40"
      >
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => onActivate(node.id)}
          className="h-6 min-w-0 justify-start px-0 text-[10px] font-semibold text-kumo-strong"
        >
          <TerminalIcon className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">{session?.name || '终端'}</span>
        </Button>
        <div className="flex items-center gap-1">
          <Button
            size="sm"
            variant="ghost"
            onClick={() => onDuplicate(node.id, 'right')}
            title="复制终端分屏"
          >
            复制
          </Button>
          <Button
            shape="square"
            size="sm"
            variant="ghost"
            icon={<X className="h-3 w-3" />}
            aria-label="关闭终端"
            title="关闭终端"
            onClick={(e) => {
              e.stopPropagation();
              onClose(node.id);
            }}
            className="hover:text-kumo-danger"
          />
        </div>
      </div>

      {/* xterm DOM 挂载插槽 */}
      <div
        id={`ssh-slot-session-${node.id}`}
        data-session-id={node.id}
        className="app-terminal-surface min-h-0 flex-1 overflow-hidden"
      />

      {/* 拖拽吸附高亮预览层 */}
      {dropRegion && (
        <div
          className="pointer-events-none absolute z-30 rounded border border-brand bg-brand/15 ring-2 ring-brand/30 transition-all duration-quick"
          style={getDropIndicatorStyle(dropRegion)}
        >
          <div className="flex h-full w-full items-center justify-center">
            <span className="rounded bg-brand/90 px-2 py-0.5 text-[10px] font-medium text-kumo-inverse shadow-sm">
              {dropRegion === 'center' ? '置换 / 聚焦' : `分屏 (${dropRegion})`}
            </span>
          </div>
        </div>
      )}
    </div>
  );
}

/**
 * TerminalTreeSplitter - 分割条，支持平滑拖拽调整比例
 */
function TerminalTreeSplitter({ direction, onResize }) {
  const isDraggingRef = useRef(false);
  const startPosRef = useRef(0);
  const containerSizeRef = useRef(0);
  const [isHoveredOrActive, setIsHoveredOrActive] = useState(false);

  const handlePointerDown = useCallback((e) => {
    e.preventDefault();
    e.stopPropagation();
    isDraggingRef.current = true;
    setIsHoveredOrActive(true);

    const isHorizontal = direction === 'h';
    startPosRef.current = isHorizontal ? e.clientX : e.clientY;

    const parentEl = e.currentTarget.parentElement;
    if (parentEl) {
      const rect = parentEl.getBoundingClientRect();
      containerSizeRef.current = isHorizontal ? rect.width : rect.height;
    }

    const onPointerMove = (moveEvent) => {
      if (!isDraggingRef.current || containerSizeRef.current <= 0) return;
      const currentPos = isHorizontal ? moveEvent.clientX : moveEvent.clientY;
      const deltaPx = currentPos - startPosRef.current;
      const deltaRatio = deltaPx / containerSizeRef.current;
      onResize(deltaRatio, false);
    };

    const onPointerUp = () => {
      isDraggingRef.current = false;
      setIsHoveredOrActive(false);
      window.removeEventListener('pointermove', onPointerMove);
      window.removeEventListener('pointerup', onPointerUp);
      window.removeEventListener('pointercancel', onPointerUp);
      onResize(0, true); // 提交并触发 final fit
    };

    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
    window.addEventListener('pointercancel', onPointerUp);
  }, [direction, onResize]);

  return (
    <div
      onPointerDown={handlePointerDown}
      className={`group relative z-20 shrink-0 transition-colors ${
        direction === 'h'
          ? 'w-1.5 cursor-col-resize hover:bg-brand/40'
          : 'h-1.5 cursor-row-resize hover:bg-brand/40'
      } ${isHoveredOrActive ? '!bg-brand' : 'bg-transparent'}`}
    >
      {/* 辅助热区扩大线 */}
      <div
        className={`absolute -inset-1 ${
          direction === 'h' ? 'cursor-col-resize' : 'cursor-row-resize'
        }`}
      />
    </div>
  );
}

/**
 * TerminalNodeRenderer - 递归渲染二叉分屏树节点
 */
function TerminalNodeRenderer({
  node,
  path = [],
  sessionMap,
  activeSessionId,
  onActivate,
  onClose,
  onDuplicate,
  draggedSessionId,
  onDragStart,
  onSplitDrop,
  onRatioChange,
}) {
  const [liveRatio, setLiveRatio] = useState(null);

  if (!node) return null;

  if (node.type === 'leaf') {
    const session = sessionMap.get(node.id);
    return (
      <TerminalPaneView
        node={node}
        session={session}
        isActive={activeSessionId === node.id}
        onActivate={onActivate}
        onClose={onClose}
        onDuplicate={onDuplicate}
        draggedSessionId={draggedSessionId}
        onDragStart={onDragStart}
        onSplitDrop={onSplitDrop}
      />
    );
  }

  if (node.type === 'branch') {
    const isHorizontal = node.direction === 'h';
    const currentRatio = liveRatio ?? (node.ratio || 0.5);

    const handleSplitterResize = (deltaRatio, isFinal) => {
      if (isFinal) {
        if (liveRatio !== null) {
          onRatioChange(path, liveRatio);
          setLiveRatio(null);
        }
      } else {
        const next = Math.max(0.1, Math.min(0.9, (node.ratio || 0.5) + deltaRatio));
        setLiveRatio(next);
      }
    };

    return (
      <div
        className={`flex min-h-0 min-w-0 flex-1 overflow-hidden ${
          isHorizontal ? 'flex-row' : 'flex-col'
        }`}
      >
        <div
          style={{
            flex: `${currentRatio} ${currentRatio} 0%`,
          }}
          className="flex min-h-0 min-w-0 overflow-hidden"
        >
          <TerminalNodeRenderer
            node={node.first}
            path={[...path, 'first']}
            sessionMap={sessionMap}
            activeSessionId={activeSessionId}
            onActivate={onActivate}
            onClose={onClose}
            onDuplicate={onDuplicate}
            draggedSessionId={draggedSessionId}
            onDragStart={onDragStart}
            onSplitDrop={onSplitDrop}
            onRatioChange={onRatioChange}
          />
        </div>

        <TerminalTreeSplitter
          direction={node.direction}
          onResize={handleSplitterResize}
        />

        <div
          style={{
            flex: `${1 - currentRatio} ${1 - currentRatio} 0%`,
          }}
          className="flex min-h-0 min-w-0 overflow-hidden"
        >
          <TerminalNodeRenderer
            node={node.second}
            path={[...path, 'second']}
            sessionMap={sessionMap}
            activeSessionId={activeSessionId}
            onActivate={onActivate}
            onClose={onClose}
            onDuplicate={onDuplicate}
            draggedSessionId={draggedSessionId}
            onDragStart={onDragStart}
            onSplitDrop={onSplitDrop}
            onRatioChange={onRatioChange}
          />
        </div>
      </div>
    );
  }

  return null;
}

/**
 * TerminalWorkspaceContainer - 终端多窗口工作区入口容器
 */
export default function TerminalWorkspaceContainer({
  splitTree,
  sessions = [],
  activeSessionId,
  draggedSessionId,
  onActivateSession,
  onCloseSession,
  onDuplicateSession,
  onDragStartSession,
  onSplitDrop,
  onRatioChange,
}) {
  const sessionMap = new Map(sessions.map((s) => [s.id, s]));

  return (
    <div className="relative flex h-full min-h-0 w-full min-w-0 flex-1 overflow-hidden bg-kumo-recessed p-1.5">
      <TerminalNodeRenderer
        node={splitTree}
        path={[]}
        sessionMap={sessionMap}
        activeSessionId={activeSessionId}
        onActivate={onActivateSession}
        onClose={onCloseSession}
        onDuplicate={onDuplicateSession}
        draggedSessionId={draggedSessionId}
        onDragStart={onDragStartSession}
        onSplitDrop={onSplitDrop}
        onRatioChange={onRatioChange}
      />
    </div>
  );
}
