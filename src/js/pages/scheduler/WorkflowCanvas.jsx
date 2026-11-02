import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import useDraggableScroll from '../../hooks/useDraggableScroll.js';
import { WORKFLOW_CANVAS_MAX_HEIGHT, WORKFLOW_CANVAS_SIZES } from './constants.js';
import {
  buildWorkflowCanvasLayout,
  conditionLabel,
  statusBadgeVariant,
  statusLabel,
  workflowNodeKindLabel,
  workflowNodeKindVariant,
  workflowNodeTypeLabel,
} from './utils.js';

/* ---------- 工作流画布（复用 GitHub Actions flow 画布的「纯布局 + SVG 连线」设计） ----------
   布局与连线全部由纯函数按节点/连线计算（不做 DOM 测量），缩放只作用于展示层 transform，
   天然避免 kumo Flow 在祖先 transform 下测量导致连线/节点错位的同类问题；画布外壳、双层
   动画连线、拖拽平移与非测量式 fit 缩放均与 GitHubPage 的 ActionWorkflowCanvas 同款。 */

export function WorkflowCanvas({ workflow, runs = [], tasks = [], selectedNodeId = '', onSelectNode = null, size = 'default' }) {
  const nodes = workflow.nodes || [];
  const edges = workflow.edges || [];
  const compact = size === 'compact';
  const editor = size === 'editor';
  const cfg = WORKFLOW_CANVAS_SIZES[size] || WORKFLOW_CANVAS_SIZES.default;
  const layout = useMemo(() => buildWorkflowCanvasLayout(nodes, edges, size), [nodes, edges, size]);
  const latestRun = runs.find((run) => run.workflow_id === workflow.id);
  const nodeStatus = Object.fromEntries((latestRun?.node_runs || []).map((run) => [run.node_id, run.status]));
  const incomingEdges = useMemo(() => {
    const grouped = new Map();
    layout.edges.forEach((edge) => {
      if (!grouped.has(edge.to)) grouped.set(edge.to, []);
      grouped.get(edge.to).push(edge);
    });
    return grouped;
  }, [layout.edges]);

  const viewportRef = useRef(null);
  const [viewportSize, setViewportSize] = useState({ width: 0, height: 0 });
  useEffect(() => {
    const element = viewportRef.current;
    if (!element) return undefined;
    const updateSize = () => {
      const rect = element.getBoundingClientRect();
      setViewportSize({ width: rect.width, height: rect.height });
    };
    updateSize();
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', updateSize);
      return () => window.removeEventListener('resize', updateSize);
    }
    const observer = new ResizeObserver(updateSize);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  // 画布缩放默认值：70%（用户指定），滚轮 ±，复位回 70%；不再以「适配容器」作为默认缩放
  const WORKFLOW_CANVAS_DEFAULT_SCALE = 0.7;
  const [displayedScale, setDisplayedScale] = useState(WORKFLOW_CANVAS_DEFAULT_SCALE);
  const view = useMemo(() => {
    const width = viewportSize.width || layout.width;
    const height = cfg.fixedHeight != null ? cfg.fixedHeight : Math.min(viewportSize.height || layout.height, WORKFLOW_CANVAS_MAX_HEIGHT);
    const scaledWidth = layout.width * displayedScale;
    const scaledHeight = layout.height * displayedScale;
    const overflowX = scaledWidth > width;
    const overflowY = scaledHeight > height;
    return {
      displayedScale,
      left: overflowX ? Math.max(8, cfg.padX / 2) : Math.max(0, (width - scaledWidth) / 2),
      top: overflowY ? Math.max(8, cfg.padY / 2) : Math.max(0, (height - scaledHeight) / 2),
      overflowX,
      overflowY,
      contentWidth: overflowX ? Math.ceil(scaledWidth + Math.max(8, cfg.padX / 2) * 2 + 12) : width,
      contentHeight: overflowY ? Math.ceil(scaledHeight + Math.max(8, cfg.padY / 2) * 2 + 12) : height,
    };
  }, [viewportSize.width, viewportSize.height, layout, cfg, displayedScale]);

  // 滚轮缩放：原生监听（passive:false）才能阻止默认滚动；Ctrl/⌘ 滚轮保留浏览器页面缩放
  useEffect(() => {
    const element = viewportRef.current;
    if (!element) return undefined;
    const handleWheel = (event) => {
      if (event.ctrlKey || event.metaKey) return;
      const factor = Math.exp(-event.deltaY * 0.0015);
      const next = Math.min(3, Math.max(0.25, displayedScale * factor));
      if (next === displayedScale) return;
      setDisplayedScale(next);
      event.preventDefault();
    };
    element.addEventListener('wheel', handleWheel, { passive: false });
    return () => element.removeEventListener('wheel', handleWheel);
  }, [displayedScale]);

  // 鸟瞰图：监听滚动窗口，视口指示框随平移/缩放实时更新
  const [scrollWindow, setScrollWindow] = useState({ left: 0, top: 0, width: 0, height: 0 });
  const minimapRef = useRef(null);
  const minimapDragRef = useRef(false);
  const syncScrollWindow = useCallback(() => {
    const element = viewportRef.current;
    if (!element) return;
    setScrollWindow({ left: element.scrollLeft, top: element.scrollTop, width: element.clientWidth || 0, height: element.clientHeight || 0 });
  }, []);
  useEffect(() => {
    const element = viewportRef.current;
    if (!element) return undefined;
    const onScroll = () => syncScrollWindow();
    onScroll();
    element.addEventListener('scroll', onScroll, { passive: true });
    const observer = new ResizeObserver(onScroll);
    observer.observe(element);
    return () => {
      element.removeEventListener('scroll', onScroll);
      observer.disconnect();
    };
  }, [syncScrollWindow, view.contentWidth, view.contentHeight]);

  const showMinimap = !compact && viewportSize.width >= 340;
  const navigateCanvasFromEvent = useCallback((event) => {
    const element = viewportRef.current;
    const frame = minimapRef.current;
    if (!element || !frame) return;
    const rect = frame.getBoundingClientRect();
    const mmScale = Math.min((rect.width - 16) / layout.width, (rect.height - 16) / layout.height);
    const layoutX = (event.clientX - rect.left - 8) / mmScale;
    const layoutY = (event.clientY - rect.top - 8) / mmScale;
    element.scrollLeft = layoutX * view.displayedScale + view.left - element.clientWidth / 2;
    element.scrollTop = layoutY * view.displayedScale + view.top - element.clientHeight / 2;
    syncScrollWindow();
  }, [layout.width, layout.height, view.displayedScale, view.left, view.top, syncScrollWindow]);

  const { dragHandlers, isDragging } = useDraggableScroll(viewportRef, {
    disabled: !(view.overflowX || view.overflowY),
  });

  const renderNodeCard = (node, rect) => {
    const status = nodeStatus[node.id];
    const selected = selectedNodeId === node.id;
    const linkedTask = tasks.find((t) => String(t.id) === String(node.task_id));
    const isAi = node.type === 'ai' || (!!linkedTask && linkedTask.type === 'ai');
    const dependencies = incomingEdges.get(node.id) || [];
    const dependencyText = dependencies.length > 1
      ? `${dependencies.length} 条依赖`
      : dependencies[0]
        ? conditionLabel(dependencies[0].condition)
        : '';

    return (
      <div
        key={node.id}
        role={onSelectNode ? 'button' : undefined}
        tabIndex={onSelectNode ? 0 : undefined}
        aria-label={onSelectNode ? `选择节点 ${node.name || node.id}` : undefined}
        aria-pressed={onSelectNode ? selected : undefined}
        onClick={onSelectNode ? () => onSelectNode(node.id) : undefined}
        onKeyDown={onSelectNode ? (event) => {
          if (event.key !== 'Enter' && event.key !== ' ') return;
          event.preventDefault();
          onSelectNode(node.id);
        } : undefined}
        style={{
          left: rect.x,
          top: rect.y,
          width: rect.width,
          height: rect.height,
          cursor: onSelectNode ? 'pointer' : 'default',
        }}
        className={`absolute flex flex-col overflow-hidden rounded-md border bg-kumo-base text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/45 ${compact ? 'px-2.5 py-2' : editor ? 'px-3.5 py-2.5' : 'px-4 py-3'} ${selected ? 'border-brand ring-2 ring-brand/25' : 'border-kumo-line hover:border-brand/50'} ${node.enabled === 0 ? 'opacity-60' : ''} ${status === 'running' ? 'scheduler-node-running' : ''}`}
      >
        <span className={`flex min-w-0 items-start justify-between ${compact ? 'gap-1.5' : editor ? 'gap-2.5' : 'gap-3'}`}>
          <span className="min-w-0">
            <span className={`block truncate font-semibold text-kumo-strong ${compact ? 'text-xs' : 'text-sm'}`}>{node.name || node.id}</span>
            <span className={`mt-0.5 block truncate leading-4 text-kumo-subtle ${compact ? 'text-[10px]' : 'mt-1 text-xs leading-5'}`}>{isAi ? 'AI 智能任务' : workflowNodeTypeLabel(node)}</span>
          </span>
          <Badge variant={isAi ? 'purple' : workflowNodeKindVariant(node)} className={compact ? 'text-[9px] px-1 py-0' : undefined}>{isAi ? 'AI' : workflowNodeKindLabel(node)}</Badge>
        </span>
        <span className={`mt-auto flex min-w-0 items-center justify-between gap-2 ${compact ? 'pt-2' : editor ? 'pt-3' : 'pt-5'}`}>
          <Badge variant={statusBadgeVariant(status || (node.enabled === 0 ? 'skipped' : 'queued'))} appearance="dot" className={compact ? 'text-[9px] px-1' : undefined}>
            {node.enabled === 0 ? '停用' : status ? statusLabel(status) : '待运行'}
          </Badge>
          {dependencyText && (
            <span className={`min-w-0 truncate text-kumo-subtle ${compact ? 'text-[10px]' : 'text-xs'}`}>
              {dependencyText}
            </span>
          )}
        </span>
      </div>
    );
  };

  if (nodes.length === 0) {
    return (
      <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-8 text-center text-xs text-kumo-subtle">
        暂无节点
      </div>
    );
  }

  return (
    <div
      className={`${compact ? 'absolute inset-0' : 'relative h-full border border-kumo-line'} overflow-hidden rounded-md bg-kumo-base`}
      style={cfg.fixedHeight ? { height: cfg.fixedHeight } : undefined}
    >
      <div
        ref={viewportRef}
        {...dragHandlers}
        onDoubleClick={(e) => {
          if (e.target instanceof Element && !e.target.closest('[role="button"]')) setDisplayedScale(WORKFLOW_CANVAS_DEFAULT_SCALE);
        }}
        className={`absolute inset-0 select-none overflow-auto ${view.overflowX || view.overflowY ? (isDragging ? 'cursor-grabbing' : 'cursor-grab') : ''}`}
      >
      <div className="relative" style={{ width: view.contentWidth, height: view.contentHeight }}>
        <div
          className="relative origin-top-left"
          style={{
            width: layout.width,
            height: layout.height,
            transform: `translate(${view.left}px, ${view.top}px) scale(${view.displayedScale})`,
          }}
        >
          <svg className="pointer-events-none absolute inset-0 z-0" width={layout.width} height={layout.height} aria-hidden="true">
            <g fill="none" strokeLinecap="round" strokeLinejoin="round">
              {layout.edges.map((edge) => (
                <g key={`${edge.from}-${edge.to}`}>
                  <path d={edge.path} stroke="var(--color-kumo-line)" strokeOpacity="0.9" strokeWidth="2" />
                  <path
                    d={edge.path}
                    stroke="var(--color-brand)"
                    strokeOpacity="0.3"
                    strokeWidth="2.5"
                    pathLength="1"
                    strokeDasharray="0.14 0.86"
                    strokeDashoffset="0"
                  >
                    <animate attributeName="stroke-dashoffset" from="1" to="0" dur="2.2s" repeatCount="indefinite" />
                  </path>
                </g>
              ))}
            </g>
          </svg>
          {layout.stages.flatMap((stage) => stage.map((node) => renderNodeCard(node, layout.rectById.get(node.id))))}
        </div>
      </div>
      </div>
      <Button
        type="button"
        size="xs"
        variant="ghost"
        onClick={() => setDisplayedScale(WORKFLOW_CANVAS_DEFAULT_SCALE)}
        title="滚轮缩放画布；点击或双击画布恢复 70%"
        className="absolute right-2 top-2 z-10 h-6 gap-1 rounded-full bg-kumo-base/90 px-2 text-[11px] font-medium text-kumo-subtle shadow-sm ring-1 ring-kumo-line"
      >
        {Math.round(view.displayedScale * 100)}%
      </Button>
      {showMinimap && (() => {
        const frameW = Math.min(180, Math.max(120, viewportSize.width * 0.3));
        const frameH = Math.min(120, Math.max(72, layout.height / layout.width * frameW + 8));
        const mmScale = Math.min((frameW - 16) / layout.width, (frameH - 16) / layout.height);
        const rawLeft = (scrollWindow.left - view.left) / view.displayedScale;
        const rawTop = (scrollWindow.top - view.top) / view.displayedScale;
        const rawW = scrollWindow.width / view.displayedScale;
        const rawH = scrollWindow.height / view.displayedScale;
        const visX = Math.max(0, Math.min(rawLeft, layout.width)) * mmScale;
        const visY = Math.max(0, Math.min(rawTop, layout.height)) * mmScale;
        const visW = Math.min(rawW, layout.width - visX / mmScale) * mmScale;
        const visH = Math.min(rawH, layout.height - visY / mmScale) * mmScale;
        return (
          <div
            ref={minimapRef}
            title="鸟瞰图：点击或拖动定位画布"
            className="absolute bottom-2 left-2 z-10 overflow-hidden rounded-md border border-kumo-line bg-kumo-base/95"
            style={{ width: frameW, height: frameH }}
            onPointerDown={(event) => {
              if (event.button !== 0) return;
              minimapDragRef.current = true;
              event.preventDefault();
              navigateCanvasFromEvent(event);
            }}
            onPointerMove={(event) => {
              if (minimapDragRef.current) navigateCanvasFromEvent(event);
            }}
            onPointerUp={() => { minimapDragRef.current = false; }}
            onPointerLeave={() => { minimapDragRef.current = false; }}
          >
            <svg width={frameW - 16} height={frameH - 16} className="pointer-events-none absolute left-2 top-2">
              <g transform={`scale(${mmScale})`} fill="none">
                {layout.edges.map((edge) => (
                  <path key={`${edge.from}-${edge.to}`} d={edge.path} stroke="var(--color-kumo-line)" strokeOpacity="0.85" strokeWidth="2" />
                ))}
                {layout.stages.flatMap((stage) => stage.map((node) => {
                  const rect = layout.rectById.get(node.id);
                  if (!rect) return null;
                  return (
                    <rect
                      key={node.id}
                      x={rect.x}
                      y={rect.y}
                      width={rect.width}
                      height={rect.height}
                      rx={4}
                      fill="var(--color-kumo-base)"
                      stroke="var(--color-kumo-line)"
                      strokeWidth="1.5"
                    />
                  );
                }))}
              </g>
              <rect
                x={visX}
                y={visY}
                width={Math.max(12, visW)}
                height={Math.max(8, visH)}
                fill="var(--color-brand)"
                fillOpacity="0.12"
                stroke="var(--color-brand)"
                strokeWidth="1.5"
              />
            </svg>
          </div>
        );
      })()}
    </div>
  );
}
