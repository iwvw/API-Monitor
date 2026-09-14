import React, { useEffect, useMemo, useRef, useState } from 'react';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { useDraggableScroll } from '../../hooks/useDraggableScroll.js';
import {
  ACTION_FLOW_CARD_WIDTH,
  ACTION_FLOW_PADDING_X,
  ACTION_FLOW_PADDING_Y,
  ACTION_FLOW_MIN_SCALE,
  ACTION_FLOW_MIN_VIEWPORT_HEIGHT,
  ACTION_FLOW_VIEWPORT_HEIGHT,
} from './constants.js';
import { actionFlowNodeHeight, buildActionCanvasLayout } from './actionFlowCanvas.js';
import ActionFlowNode from './ActionFlowNode.jsx';

function ActionWorkflowCanvas({ workflow, jobs, now }) {
  const viewportRef = useRef(null);
  const expandedNodeRef = useRef(null);
  const [viewportSize, setViewportSize] = useState({ width: 0, height: ACTION_FLOW_VIEWPORT_HEIGHT });
  const [expandedMatrixId, setExpandedMatrixId] = useState(null);
  const [focusState, setFocusState] = useState(null);
  const focusIds = expandedMatrixId ? null : focusState?.ids;
  const focusMode = expandedMatrixId ? 'neighbors' : focusState?.mode;
  const layout = useMemo(
    () => buildActionCanvasLayout(workflow, jobs, now, focusIds, focusMode),
    [focusIds, focusMode, workflow, jobs, now],
  );
  const baseLayoutSize = useMemo(() => {
    const baseLayout = buildActionCanvasLayout(workflow, jobs, now);
    return { width: baseLayout.width, height: baseLayout.height };
  }, [workflow, jobs, now]);
  const expandedNode = useMemo(() => {
    if (!expandedMatrixId) return null;
    for (const stage of layout.stages) {
      const match = stage.nodes.find((item) => item.id === expandedMatrixId || item.definitions.some((definition) => definition.id === expandedMatrixId));
      if (match) return match;
    }
    return null;
  }, [expandedMatrixId, layout.stages]);
  const expandedCanvasHeight = useMemo(() => {
    if (!expandedNode) return layout.height;
    return Math.max(
      layout.height,
      expandedNode.rect.y + actionFlowNodeHeight(expandedNode.node, true) + ACTION_FLOW_PADDING_Y + 8,
    );
  }, [expandedNode, layout.height]);
  const hasExpandedNode = Boolean(expandedNode);

  useEffect(() => {
    if (expandedMatrixId && !expandedNode) setExpandedMatrixId(null);
  }, [expandedMatrixId, expandedNode]);

  useEffect(() => {
    if (!expandedMatrixId) return undefined;
    const handlePointerDown = (event) => {
      const target = event.target;
      if (target instanceof Node && expandedNodeRef.current?.contains(target)) return;
      setExpandedMatrixId(null);
      setFocusState(null);
    };
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') {
        setExpandedMatrixId(null);
        setFocusState(null);
      }
    };
    document.addEventListener('pointerdown', handlePointerDown, true);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [expandedMatrixId]);

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

  const canvasFit = useMemo(() => {
    const width = viewportSize.width || layout.width;
    const availableWidth = Math.max(width - ACTION_FLOW_PADDING_X * 2, ACTION_FLOW_CARD_WIDTH);
    const naturalScale = availableWidth / baseLayoutSize.width;
    const scale = Math.min(1, Math.max(ACTION_FLOW_MIN_SCALE, naturalScale));
    const scaledWidth = layout.width * scale;
    const scaledBaseHeight = baseLayoutSize.height * scale;
    const baseCanvasHeight = Math.max(ACTION_FLOW_MIN_VIEWPORT_HEIGHT, Math.ceil(scaledBaseHeight));
    const canvasHeight = Math.max(baseCanvasHeight, Math.ceil(expandedCanvasHeight * scale));
    const overflowX = scaledWidth > width;
    const left = overflowX
      ? Math.max(12, ACTION_FLOW_PADDING_X / 2)
      : Math.max(0, (width - scaledWidth) / 2);
    return {
      scale,
      left,
      top: Math.max(0, (baseCanvasHeight - scaledBaseHeight) / 2),
      height: canvasHeight,
      contentWidth: overflowX ? Math.ceil(scaledWidth + left * 2 + 12) : width,
      overflowX,
    };
  }, [baseLayoutSize.height, baseLayoutSize.width, expandedCanvasHeight, layout.width, viewportSize.width]);
  const activeConnectorPoints = useMemo(() => {
    if (hasExpandedNode) return [];
    const points = new Map();
    layout.edges.filter((edge) => edge.highlighted).forEach((edge) => {
      [
        { x: edge.sourceX, y: edge.sourceY },
        { x: edge.targetX, y: edge.targetY },
      ].forEach((point) => {
        const key = `${Math.round(point.x * 10)}-${Math.round(point.y * 10)}`;
        if (!points.has(key)) points.set(key, { ...point, key });
      });
    });
    return [...points.values()];
  }, [hasExpandedNode, layout.edges]);
  const canPanCanvas = canvasFit.overflowX;
  const { dragHandlers, isDragging } = useDraggableScroll(viewportRef, {
    disabled: hasExpandedNode || !canPanCanvas,
  });

  return (
    <div
      ref={viewportRef}
      {...dragHandlers}
      className={`relative overflow-x-auto overflow-y-hidden bg-transparent scrollbar-thin ${canPanCanvas ? (isDragging ? 'cursor-grabbing' : 'cursor-grab') : ''} ${isDragging ? 'select-none' : ''}`}
      style={{ height: canvasFit.height }}
    >
      {(layout.graph.fallback || workflow?.error) && (
        <div className="absolute left-3 top-2 z-30 max-w-[60%] truncate rounded-md border border-kumo-line bg-kumo-base/95 px-2 py-1 text-xs leading-5 text-kumo-subtle" title={workflow?.error || ''}>
          已按 Job 顺序显示{workflow?.error ? `：${workflow.error}` : ''}
        </div>
      )}
      <div
        className="relative origin-top-left min-w-full"
        style={{
          width: canvasFit.contentWidth,
          height: canvasFit.height,
        }}
      >
        <div
          className="relative origin-top-left"
          style={{
            width: layout.width,
            height: expandedCanvasHeight,
            transform: `translate(${canvasFit.left}px, ${canvasFit.top}px) scale(${canvasFit.scale})`,
          }}
        >
            <svg className="pointer-events-none absolute inset-0 z-0" width={layout.width} height={layout.height} aria-hidden="true">
              <g fill="none" strokeLinecap="round" strokeLinejoin="round">
                {layout.edges.filter((edge) => !edge.highlighted).map((edge) => (
                  <g key={`${edge.from}-${edge.to}`}>
                    <path
                      d={edge.path}
                      stroke="#b8c2cf"
                      strokeOpacity={hasExpandedNode ? '0.22' : (layout.hasFocus ? '0.38' : '0.98')}
                      strokeWidth="2"
                    />
                    <path
                      d={edge.path}
                      stroke="#6ea8ff"
                      strokeOpacity={hasExpandedNode ? '0.04' : (layout.hasFocus ? '0.08' : '0.26')}
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
            {hasExpandedNode && (
              <div
                className="absolute inset-0 z-30 bg-kumo-base/55 backdrop-blur-[1px]"
                onMouseDown={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  setExpandedMatrixId(null);
                  setFocusState(null);
                }}
                aria-hidden="true"
              />
            )}
            {layout.stages.flatMap((stage) => stage.nodes.map(({ id, definitions, node, rect, highlighted }) => (
              <ActionFlowNode
                key={`${stage.stageIndex}-${id}`}
                node={node}
                expanded={expandedMatrixId === id}
                highlighted={hasExpandedNode ? expandedMatrixId === id : highlighted}
                spotlighted={expandedMatrixId === id}
                nodeRef={expandedMatrixId === id ? expandedNodeRef : null}
                onFocus={hasExpandedNode ? undefined : () => setFocusState({ ids: new Set(definitions.map((definition) => definition.id)), mode: 'lineage' })}
                onBlur={hasExpandedNode ? undefined : () => setFocusState(null)}
                onDefinitionFocus={(definitionId) => {
                  if (hasExpandedNode) return;
                  if (!definitionId) return;
                  setFocusState({ ids: new Set([definitionId]), mode: 'lineage' });
                }}
                onDefinitionBlur={() => {
                  if (hasExpandedNode) return;
                  setFocusState({ ids: new Set(definitions.map((definition) => definition.id)), mode: 'lineage' });
                }}
                onToggle={() => {
                  const primaryId = definitions[0]?.id;
                  if (!primaryId) return;
                  setFocusState(null);
                  setExpandedMatrixId((current) => (current === primaryId ? null : primaryId));
                }}
                style={{ left: rect.x, top: rect.y, width: rect.width }}
              />
            )))}
            <svg className="pointer-events-none absolute inset-0 z-30" width={layout.width} height={layout.height} aria-hidden="true">
              <g fill="none" strokeLinecap="round" strokeLinejoin="round">
                {layout.edges.filter((edge) => !hasExpandedNode && edge.highlighted).map((edge) => (
                  <path
                    key={`${edge.from}-${edge.to}-active`}
                    className="stroke-brand transition-[stroke,stroke-opacity,stroke-width] duration-150"
                    d={edge.path}
                    pathLength="1"
                    strokeDasharray="1"
                    strokeDashoffset="0"
                    strokeOpacity="0.96"
                    strokeWidth="3"
                  >
                    <animate attributeName="stroke-dashoffset" from="1" to="0" dur="180ms" fill="freeze" />
                  </path>
                ))}
              </g>
              <g className="fill-kumo-base stroke-brand" strokeWidth="2">
                {activeConnectorPoints.map((point) => (
                  <circle key={point.key} cx={point.x} cy={point.y} r="4">
                    <animate attributeName="opacity" from="0" to="1" dur="140ms" fill="freeze" />
                  </circle>
                ))}
              </g>
            </svg>
        </div>
      </div>
    </div>
  );
}

function ActionFlowPlaceholder() {
  return (
    <div className="flex min-h-[260px] items-center justify-center rounded-md border border-kumo-line bg-kumo-base px-8">
      <div className="flex w-full max-w-4xl items-center justify-center gap-4">
        {[0, 1, 2, 3].map((item) => (
          <React.Fragment key={item}>
            <div className="w-52 rounded-md border border-kumo-line bg-kumo-base p-3">
              <div className="flex items-center justify-between gap-3">
                <SkeletonLine className="h-4 w-24" />
                <SkeletonLine className="h-5 w-14 rounded-full" />
              </div>
              <SkeletonLine className="mt-3 h-3 w-28" />
              <SkeletonLine className="mt-5 h-3 w-36" />
            </div>
            {item < 3 && <SkeletonLine className="h-1 w-12 shrink-0 rounded-full" />}
          </React.Fragment>
        ))}
      </div>
    </div>
  );
}

function ActionWorkflowLoadingState() {
  return (
    <div className="flex min-h-[112px] items-center justify-center bg-transparent px-6">
      <div className="w-52 rounded-md border border-kumo-line bg-kumo-base p-3 opacity-80">
        <div className="flex items-center justify-between gap-3">
          <SkeletonLine className="h-4 w-24" />
          <SkeletonLine className="h-4 w-12 rounded-full" />
        </div>
        <SkeletonLine className="mt-3 h-3 w-28" />
      </div>
    </div>
  );
}

export default ActionWorkflowCanvas;
export { ActionFlowPlaceholder, ActionWorkflowLoadingState };
