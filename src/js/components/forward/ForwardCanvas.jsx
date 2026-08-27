import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Badge, Button, ClipboardText } from '@cloudflare/kumo';
import { ArrowUpRight } from '@phosphor-icons/react';
import { Input } from '@cloudflare/kumo/components/input';
import { useCloudflareSpotlight } from '../../hooks/useCloudflareSpotlight.js';
import {
  buildTreeLayout,
  STATUS_COLORS,
  STATUS_LABELS,
  TRANSPORT_LABELS,
  TRANSPORT_SHORT,
} from './useMeshLayout.js';

const DETAIL_W = 320;
const FIT_PAD = 24;
const DEFAULT_SCALE = 1.0;
const MIN_SCALE = 0.6;
const MAX_SCALE = 2.0;

const STATUS_BADGE_VARIANT = {
  running: 'success',
  deploying: 'info',
  failed: 'error',
  disconnected: 'warning',
  stopped: 'neutral',
  pending: 'neutral',
};

export default function ForwardCanvas({ forwards, servers, deploying, acting, onEdit, onDeploy, onStop, onStart, onDelete, deleteConfirmActive, isDeleting }) {
  const viewportRef = useRef(null);
  const spotlightSurfaceRef = useCloudflareSpotlight();
  const flashTimerRef = useRef(null);
  const prevStatusRef = useRef({});
  const [viewportSize, setViewportSize] = useState({ width: 0, height: 0 });
  const [selectedId, setSelectedId] = useState('');
  const [flashId, setFlashId] = useState('');
  const [canvasHeight, setCanvasHeight] = useState(0);
  const [view, setView] = useState({ x: 0, y: 0, scale: DEFAULT_SCALE });
  const [isPanning, setIsPanning] = useState(false);
  const [query, setQuery] = useState('');
  const [inactiveHosts, setInactiveHosts] = useState(() => new Set());
  const [hoverHostId, setHoverHostId] = useState('');

  // 树形布局：hub → 主机 → 规则卡
  const layout = useMemo(() => buildTreeLayout(forwards, servers), [forwards, servers]);

  // 搜索匹配：命中名称 / 本地地址 / 访问地址
  const normalizedQuery = query.trim().toLowerCase();
  const matchCard = useCallback(
    (fwd) => {
      if (!normalizedQuery) return true;
      return [fwd.name, `${fwd.local_host}:${fwd.local_port}`, fwd.access_url, fwd.server_name]
        .filter(Boolean)
        .some((text) => text.toLowerCase().includes(normalizedQuery));
    },
    [normalizedQuery]
  );
  const isDimmedCard = useCallback(
    (fwd, serverId) => {
      if (inactiveHosts.has(serverId)) return true;
      if (!normalizedQuery) return false;
      return !matchCard(fwd);
    },
    [inactiveHosts, normalizedQuery, matchCard]
  );

  // 画布高度：同步测量「视口顶部 → 窗口底部」剩余空间（画布 tab 下 PageStack
  // 底部 padding 已置 0，留 10px 呼吸边距）。useLayoutEffect 在绘制前执行，
  // 初始 0 高不会闪出滚动条；观察父容器以响应工具栏换行/窗口缩放
  React.useLayoutEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    const update = () => {
      const top = el.getBoundingClientRect().top;
      const h = window.innerHeight - top - 10;
      setCanvasHeight((prev) => {
        const next = h > 0 ? h : 240;
        return prev === next ? prev : next;
      });
    };
    update();
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(update) : null;
    if (ro) ro.observe(el.parentElement);
    window.addEventListener('resize', update);
    return () => {
      ro?.disconnect();
      window.removeEventListener('resize', update);
    };
  }, []);

  // 视口尺寸（用于自动适应缩放）
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

  // 整图适配：完整树居中铺满视口
  const computeFit = useCallback(() => {
    const vw = viewportSize.width || 800;
    const vh = viewportSize.height || 400;
    const s = Math.max(MIN_SCALE, Math.min(1, (vw - FIT_PAD * 2) / layout.width, (vh - FIT_PAD * 2) / layout.height));
    return { scale: s, x: (vw - layout.width * s) / 2, y: (vh - layout.height * s) / 2 };
  }, [viewportSize.width, viewportSize.height, layout.width, layout.height]);

  // 用户未手动操作前：默认 100% 缩放并居中显示（不做自动缩放适配）。
  // 点「适应画布」仍可整体铺满；canvasHeight 进依赖，首帧画布撑开后居中基准重算
  const didInteractRef = useRef(false);
  useEffect(() => {
    if (viewportSize.width > 0 && viewportSize.height > 40 && !didInteractRef.current) {
      setView({
        scale: DEFAULT_SCALE,
        x: (viewportSize.width - layout.width * DEFAULT_SCALE) / 2,
        y: (viewportSize.height - layout.height * DEFAULT_SCALE) / 2,
      });
    }
  }, [viewportSize.width, viewportSize.height, canvasHeight, layout.width, layout.height]);

  // 滚轮缩放：以光标为锚点；Ctrl/⌘ 滚轮保留页面缩放。
  // 只注册一次，函数式更新读最新 view，避免闭包旧值导致的错位
  useEffect(() => {
    const element = viewportRef.current;
    if (!element) return undefined;
    const handleWheel = (event) => {
      if (event.ctrlKey || event.metaKey) return;
      event.preventDefault();
      setView((prev) => {
        const next = Math.min(MAX_SCALE, Math.max(MIN_SCALE, prev.scale * Math.exp(-event.deltaY * 0.0015)));
        if (next === prev.scale) return prev;
        const rect = element.getBoundingClientRect();
        const cx = event.clientX - rect.left;
        const cy = event.clientY - rect.top;
        const k = next / prev.scale;
        return { scale: next, x: cx - k * (cx - prev.x), y: cy - k * (cy - prev.y) };
      });
      didInteractRef.current = true;
    };
    // 浏览器中键默认行为（自动滚动/中键粘贴）会打断拖拽，统一拦掉
    const stopMiddle = (event) => {
      if (event.button === 1) event.preventDefault();
    };
    element.addEventListener('wheel', handleWheel, { passive: false });
    element.addEventListener('mousedown', stopMiddle);
    element.addEventListener('auxclick', stopMiddle);
    return () => {
      element.removeEventListener('wheel', handleWheel);
      element.removeEventListener('mousedown', stopMiddle);
      element.removeEventListener('auxclick', stopMiddle);
    };
  }, []);

  const zoomBy = useCallback(
    (factor) =>
      setView((prev) => {
        const next = Math.min(MAX_SCALE, Math.max(MIN_SCALE, prev.scale * factor));
        const cx = (viewportSize.width || 800) / 2;
        const cy = (viewportSize.height || 400) / 2;
        const k = next / prev.scale;
        return { scale: next, x: cx - k * (cx - prev.x), y: cy - k * (cy - prev.y) };
      }),
    [viewportSize.width, viewportSize.height]
  );

  const fit = useCallback(() => setView(computeFit()), [computeFit]);

  // 中键拖拽平移：记录上一指针位置，按增量平移
  const panLastRef = useRef(null);
  const handlePanPointerDown = useCallback((event) => {
    if (event.button !== 1) return;
    event.preventDefault(); // 阻止浏览器中键自动滚动
    panLastRef.current = { x: event.clientX, y: event.clientY };
    didInteractRef.current = true;
    setIsPanning(true);
    event.currentTarget.setPointerCapture?.(event.pointerId);
  }, []);
  const handlePanPointerMove = useCallback((event) => {
    const last = panLastRef.current;
    if (!last) return;
    const dx = event.clientX - last.x;
    const dy = event.clientY - last.y;
    panLastRef.current = { x: event.clientX, y: event.clientY };
    setView((prev) => ({ ...prev, x: prev.x + dx, y: prev.y + dy }));
  }, []);
  const handlePanPointerEnd = useCallback((event) => {
    if (!panLastRef.current) return;
    panLastRef.current = null;
    setIsPanning(false);
    event.currentTarget.releasePointerCapture?.(event.pointerId);
  }, []);

  // 状态变化：节点入场闪烁提示
  useEffect(() => {
    const prev = prevStatusRef.current;
    forwards.forEach((fwd) => {
      if (prev[fwd.id] && prev[fwd.id] !== fwd.apply_status) {
        setFlashId(fwd.id);
        if (flashTimerRef.current) window.clearTimeout(flashTimerRef.current);
        flashTimerRef.current = window.setTimeout(() => setFlashId(''), 900);
      }
    });
    const next = {};
    forwards.forEach((fwd) => { next[fwd.id] = fwd.apply_status; });
    prevStatusRef.current = next;
  }, [forwards]);

  const handleCanvasClick = useCallback((event) => {
    // 点到卡片或详情框以外（含内容区空白、连线层）即关闭信息框
    const t = event.target;
    if (!t.closest('.fwd-satellite') && !t.closest('[data-detail-card]')) {
      setSelectedId('');
    }
  }, []);

  const toggleHostFilter = useCallback((serverId) => {
    didInteractRef.current = true;
    setInactiveHosts((prev) => {
      const nextSet = new Set(prev);
      if (nextSet.has(serverId)) nextSet.delete(serverId);
      else nextSet.add(serverId);
      return nextSet;
    });
  }, []);

  // 选中的卡片与详情卡位置
  let selectedCard = null;
  layout.hosts.forEach((host) => {
    const hit = host.cards.find((c) => c.fwd.id === selectedId);
    if (hit) selectedCard = { ...hit, hostName: host.name, online: host.online };
  });
  // 详情 popover：屏幕坐标锚定在卡片正下方（不随缩放变形），越界自动翻转/钳制
  const DETAIL_H = 252;
  const detailScreen = useMemo(() => {
    if (!selectedCard) return null;
    const vw = viewportSize.width || 800;
    const vh = viewportSize.height || 400;
    const left = Math.min(
      Math.max(view.x + selectedCard.x * view.scale, 8),
      Math.max(8, vw - DETAIL_W - 8)
    );
    const below = view.y + (selectedCard.y + selectedCard.h) * view.scale + 6;
    if (below + DETAIL_H <= vh - 6) return { x: Math.round(left), y: Math.round(below) };
    const above = view.y + selectedCard.y * view.scale - DETAIL_H - 6;
    if (above >= 8) return { x: Math.round(left), y: Math.round(above) };
    return { x: Math.round(left), y: Math.max(8, vh - DETAIL_H - 6) };
  }, [selectedCard, view, viewportSize]);

  const hasFilter = normalizedQuery !== '' || inactiveHosts.size > 0;

  // 卡片级淡化判断需要 fwd 对象：构建 id→卡片 索引
  const cardIndex = useMemo(() => {
    const map = new Map();
    layout.hosts.forEach((host) => host.cards.forEach((card) => map.set(card.fwd.id, card)));
    return map;
  }, [layout.hosts]);

  // 悬停主机：保留它与它自身的卡片和连线，其余淡化
  const related = useMemo(() => {
    if (!hoverHostId) return null;
    const hosts = new Set([hoverHostId]);
    const fwds = new Set();
    layout.hosts.forEach((host) => {
      if (host.serverId !== hoverHostId) return;
      host.cards.forEach((card) => fwds.add(card.fwd.id));
    });
    return { hosts, fwds };
  }, [hoverHostId, layout]);

  const hostIsDim = useCallback(
    (serverId) => inactiveHosts.has(serverId) || Boolean(related && !related.hosts.has(serverId)),
    [inactiveHosts, related]
  );
  const cardIsDim = useCallback(
    (fwd, serverId) => isDimmedCard(fwd, serverId) || Boolean(related && fwd && !related.fwds.has(fwd.id)),
    [isDimmedCard, related]
  );
  const edgeDimmed = useCallback(
    (edge) => {
      if (edge.kind === 'trunk' || edge.kind === 'spine') return inactiveHosts.has(edge.hostId);
      const card = cardIndex.get(edge.fwdId);
      if (!card) return false;
      return isDimmedCard(card.fwd, edge.hostId);
    },
    [inactiveHosts, cardIndex, isDimmedCard]
  );
  const edgeIsDim = useCallback(
    (edge) => {
      if (hasFilter && edgeDimmed(edge)) return true;
      if (!related) return false;
      if (edge.kind === 'branch') return !related.fwds.has(edge.fwdId);
      return !related.hosts.has(edge.hostId);
    },
    [hasFilter, edgeDimmed, related]
  );

  // 右下角总览图：布局等比缩略 + 当前视口框，点击/拖拽导航
  const MINIMAP_W = 150;
  const MINIMAP_H = 110;
  const minimap = useMemo(() => {
    const s = Math.min(MINIMAP_W / layout.width, MINIMAP_H / layout.height);
    return { s, w: layout.width * s, h: layout.height * s };
  }, [layout.width, layout.height]);
  const minimapViewRect = useMemo(() => {
    if (!viewportSize.width || !viewportSize.height) return null;
    const s = minimap.s;
    return {
      x: (-view.x / view.scale) * s,
      y: (-view.y / view.scale) * s,
      w: (viewportSize.width / view.scale) * s,
      h: (viewportSize.height / view.scale) * s,
    };
  }, [view, viewportSize, minimap]);
  const minimapRef = useRef(null);
  const jumpOnMinimap = useCallback(
    (clientX, clientY) => {
      const el = minimapRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      const lx = (clientX - rect.left) / minimap.s;
      const ly = (clientY - rect.top) / minimap.s;
      didInteractRef.current = true;
      setView((prev) => {
        const vw = viewportSize.width || 800;
        const vh = viewportSize.height || 400;
        return { scale: prev.scale, x: vw / 2 - lx * prev.scale, y: vh / 2 - ly * prev.scale };
      });
    },
    [minimap.s, viewportSize]
  );

  return (
    <div className="flex flex-col gap-2">
      <div className="flex shrink-0 flex-wrap items-center gap-2 text-xs text-kumo-text-secondary">
        <Button size="sm" variant="outline" shape="square" aria-label="放大" onClick={() => zoomBy(1.25)}>+</Button>
        <Button size="sm" variant="outline" shape="square" aria-label="缩小" onClick={() => zoomBy(0.8)}>−</Button>
        <Button size="sm" variant="outline" onClick={fit}>适应画布</Button>
        <span className="w-12 tabular-nums">{Math.round(view.scale * 100)}%</span>
        <Input
          size="sm"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索规则、地址…"
          className="h-7 w-44"
          aria-label="搜索规则"
        />
        {layout.hosts.map((host) => (
          <Button
            key={host.id}
            type="button"
            size="xs"
            onClick={() => toggleHostFilter(host.serverId)}
            className={`flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs transition-colors ${
              inactiveHosts.has(host.serverId)
                ? 'border-kumo-line text-kumo-subtle opacity-60'
                : 'border-kumo-brand/40 bg-kumo-brand/10 font-medium text-kumo-strong'
            }`}
            title={inactiveHosts.has(host.serverId) ? '点击显示该主机' : '点击隐藏该主机'}
            aria-label={inactiveHosts.has(host.serverId) ? `显示 ${host.name}` : `隐藏 ${host.name}`}
          >
            <span
              className="h-1.5 w-1.5 rounded-full"
              style={{ background: host.online ? 'var(--color-kumo-success)' : 'var(--color-kumo-line)' }}
            />
            {host.name}
            <span className="tabular-nums opacity-70">{host.cards.length}</span>
          </Button>
        ))}
        {(query || inactiveHosts.size > 0) && (
          <Button size="sm" variant="ghost" onClick={() => { setQuery(''); setInactiveHosts(new Set()); }}>
            清除筛选
          </Button>
        )}
        <div className="ml-auto flex flex-wrap items-center gap-3">
          {['running', 'deploying', 'disconnected', 'failed', 'stopped'].map((status) => (
            <span key={status} className="flex items-center gap-1">
              <span className="h-2 w-2 rounded-full" style={{ background: STATUS_COLORS[status] }} />
              {STATUS_LABELS[status]}
            </span>
          ))}
        </div>
      </div>

      <div
        ref={viewportRef}
        className={`relative select-none overflow-hidden rounded-lg border border-kumo-line bg-kumo-base ${isPanning ? 'cursor-grabbing' : ''}`}
        style={{ height: canvasHeight }}
        onClick={handleCanvasClick}
        onDoubleClick={(event) => {
          const t = event.target;
          if (!t.closest('.fwd-satellite') && !t.closest('[data-detail-card]')) fit();
        }}
        onPointerDown={handlePanPointerDown}
        onPointerMove={handlePanPointerMove}
        onPointerUp={handlePanPointerEnd}
        onPointerCancel={handlePanPointerEnd}
      >
        <div
          ref={spotlightSurfaceRef}
          className="cf-ai-background-surface cf-ai-background fwd-spotlight pointer-events-none absolute inset-0"
          aria-hidden="true"
        />
        {forwards.length === 0 && (
          <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-2 text-kumo-subtle">
            <span className="text-sm">暂无转发规则</span>
            <span className="text-xs">创建第一条转发规则后点亮拓扑</span>
          </div>
        )}
        <div
          className="absolute origin-top-left"
          style={{
            width: layout.width,
            height: layout.height,
            transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})`,
          }}
        >
          {/* 连线：主干 + 分支 */}
          <svg className="pointer-events-none absolute inset-0" width={layout.width} height={layout.height} aria-hidden="true">
            <g fill="none" strokeLinecap="round" strokeLinejoin="round">
              {layout.edges.map((edge) => {
                const dim = edgeIsDim(edge);
                if (edge.kind === 'trunk' || edge.kind === 'spine') {
                  return (
                    <path
                      key={edge.id}
                      d={edge.path}
                      className={`fwd-edge-base transition-opacity ${dim ? 'opacity-20' : ''}`}
                      strokeWidth={2}
                    />
                  );
                }
                const color =
                  edge.status === 'running'
                    ? 'var(--color-kumo-brand)'
                    : STATUS_COLORS[edge.status] || 'var(--color-kumo-line)';
                const selected = selectedId === edge.fwdId;
                return (
                  <g key={edge.id} className={`transition-opacity ${dim ? 'opacity-20' : ''}`}>
                  <path d={edge.path} className="fwd-edge-base" fill="none" />
                  <path
                    d={edge.flowPath || edge.path}
                    stroke={color}
                    strokeWidth={selected ? 2.6 : 2}
                    pathLength="1"
                    opacity={selected ? 0.95 : edge.status === 'running' ? 0.38 : 0.72}
                    className="fwd-edge-flow"
                    fill="none"
                  />
                </g>
                );
              })}
            </g>
          </svg>

          {/* 主机节点 */}
          {layout.hosts.map((host) => {
            const dim = hostIsDim(host.serverId);
            return (
              <Button
                key={host.id}
                type="button"
                size="sm"
                title="点击隐藏/显示该主机的规则"
                onClick={(event) => {
                  event.stopPropagation();
                  toggleHostFilter(host.serverId);
                }}
                onMouseEnter={() => setHoverHostId(host.serverId)}
                onMouseLeave={() => setHoverHostId('')}
                className={`absolute z-[2] flex items-center gap-2 rounded-lg border px-3 transition-opacity ${
                  dim
                    ? 'border-kumo-line opacity-40'
                    : hoverHostId === host.serverId
                      ? 'border-kumo-brand bg-kumo-elevated shadow-md'
                      : 'border-kumo-line bg-kumo-elevated shadow-sm'
                }`}
                style={{ left: host.x, top: host.y, width: host.w, height: host.h }}
                aria-label={`主机 ${host.name}，点击切换显示`}
              >
                <span
                  className="h-2 w-2 shrink-0 rounded-full"
                  style={{ background: host.online ? 'var(--color-kumo-success)' : 'var(--color-kumo-line)', opacity: host.online ? 1 : 0.45 }}
                />
                <span className="min-w-0 flex-1 truncate text-left text-[13px] font-semibold text-kumo-strong">{host.name}</span>
                <Badge variant="neutral" size="sm">{host.cards.length}</Badge>
              </Button>
            );
          })}

          {/* 规则卡（按主机分块纵向堆叠） */}
          {layout.hosts.flatMap((host) =>
            host.cards.map((card) => {
              const fwd = card.fwd;
              const status = fwd.apply_status || 'pending';
              const statusColor = STATUS_COLORS[status] || 'var(--color-kumo-line)';
              const selected = selectedId === fwd.id;
              const dim = cardIsDim(fwd, host.serverId);
              const isTcpRelay = fwd.transport === 'tcp_relay' && fwd.relay_server_id && fwd.relay_server_id !== fwd.server_id;
              return (
                <div
                  key={fwd.id}
                  className={`fwd-satellite absolute z-[2] flex flex-col justify-center rounded-lg border px-3 transition-opacity ${
                    selected ? 'fwd-satellite-selected' : 'border-kumo-line'
                  } ${flashId === fwd.id ? 'fwd-satellite-enter' : ''} ${dim ? 'bg-kumo-elevated opacity-30' : 'bg-kumo-elevated'}`}
                  style={{ left: card.x, top: card.y, width: card.w, height: card.h, cursor: 'pointer' }}
                  role="button"
                  tabIndex={0}
                  aria-label={`转发规则 ${fwd.name}`}
                  onClick={(event) => {
                    event.stopPropagation();
                    setSelectedId((current) => (current === fwd.id ? '' : fwd.id));
                  }}
                  onDoubleClick={(event) => {
                    event.stopPropagation();
                    onEdit?.(fwd);
                  }}
                >
                  <div className="flex min-w-0 items-center gap-1.5 truncate leading-5">
                    <span
                      className={`h-1.5 w-1.5 shrink-0 rounded-full ${status === 'deploying' || status === 'disconnected' ? 'fwd-dot-pulse' : ''}`}
                      style={{ background: statusColor }}
                    />
                    <span className="truncate text-sm font-semibold text-kumo-strong">{fwd.name}</span>
                    <span className="ml-auto shrink-0 text-xs tabular-nums text-kumo-subtle">
                      {fwd.connector_count || 0} 连
                    </span>
                  </div>
                  <div className="truncate leading-5 text-xs text-kumo-subtle">
                    <span className="truncate">{fwd.local_host}:{fwd.local_port}</span>
                    <span className="ml-1 shrink-0 rounded bg-kumo-fill px-1 text-[10px] font-medium text-kumo-text-secondary">
                      {TRANSPORT_SHORT[fwd.transport] || fwd.transport}
                    </span>
                  </div>
                  {isTcpRelay && (
                    <div className="truncate leading-5 text-xs font-medium text-kumo-brand">
                      → {fwd.relay_server_name || fwd.relay_server_id}{fwd.remote_port ? ' :' + fwd.remote_port : ''}
                    </div>
                  )}
                  {fwd.failover_current_server_id && (
                    <div className="truncate leading-5 text-xs font-medium text-kumo-text-warning">
                      已切换 → {fwd.failover_current_server_id}
                    </div>
                  )}
                </div>
              );
            })
          )}

          </div>

          {/* 选中详情卡：屏幕坐标 popover，锚定卡片正下方，越界自动翻到上方 */}
          {selectedCard && detailScreen && (
            <div
              data-detail-card
              className="absolute z-[7] rounded-lg border border-kumo-brand/50 bg-kumo-elevated p-3 shadow-md ring-1 ring-kumo-brand/10"
              style={{ left: detailScreen.x, top: detailScreen.y, width: DETAIL_W }}
              onClick={(event) => event.stopPropagation()}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="min-w-0 truncate text-xs font-semibold text-kumo-strong">{selectedCard.fwd.name}</span>
                <Badge variant={STATUS_BADGE_VARIANT[selectedCard.fwd.apply_status] || 'neutral'} size="sm">
                  {STATUS_LABELS[selectedCard.fwd.apply_status] || selectedCard.fwd.apply_status}
                </Badge>
              </div>
              <div className="mt-2 flex flex-col gap-1.5 text-xs">
                <div className="flex items-center justify-between gap-2">
                  <span className="shrink-0 text-kumo-subtle">所属主机</span>
                  <span className="truncate text-kumo-default">{selectedCard.hostName}</span>
                </div>
                <div className="flex items-center justify-between gap-2">
                  <span className="shrink-0 text-kumo-subtle">访问地址</span>
                  {selectedCard.fwd.access_url ? (
                    <span className="flex min-w-0 items-center gap-1">
                      <span className="min-w-0 truncate text-xs text-kumo-default">{selectedCard.fwd.access_url}</span>
                      <ClipboardText size="sm" text={selectedCard.fwd.access_url} tooltip={{ text: '复制', copiedText: '已复制', side: 'top' }} />
                      {/^https?:\/\//.test(selectedCard.fwd.access_url) && (
                        <Button
                          size="sm"
                          variant="outline"
                          shape="square"
                          aria-label="打开访问地址"
                          title="打开访问地址"
                          onClick={(e) => { e.stopPropagation(); window.open(selectedCard.fwd.access_url, '_blank', 'noopener'); }}
                        >
                          <ArrowUpRight className="h-3.5 w-3.5" weight="bold" />
                        </Button>
                      )}
                    </span>
                  ) : (
                    <span className="text-kumo-subtle">部署后显示</span>
                  )}
                </div>
                <div className="flex items-center justify-between gap-2">
                  <span className="shrink-0 text-kumo-subtle">本地服务</span>
                  <span className="truncate text-kumo-default">
                    {selectedCard.fwd.local_host}:{selectedCard.fwd.local_port}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2">
                  <span className="shrink-0 text-kumo-subtle">传输方式</span>
                  <span className="text-kumo-default">
                    {TRANSPORT_LABELS[selectedCard.fwd.transport] || selectedCard.fwd.transport}
                  </span>
                </div>
                {selectedCard.fwd.transport === 'tcp_relay' && selectedCard.fwd.relay_server_name && (
                  <div className="flex items-center justify-between gap-2">
                    <span className="shrink-0 text-kumo-subtle">中转节点</span>
                    <span className="text-kumo-default">{selectedCard.fwd.relay_server_name}</span>
                  </div>
                )}
                <div className="flex items-center justify-between gap-2">
                  <span className="shrink-0 text-kumo-subtle">连接数</span>
                  <span className="tabular-nums text-kumo-default">{selectedCard.fwd.connector_count || 0}</span>
                </div>
                {selectedCard.fwd.last_error && (
                  <div className="truncate text-kumo-text-danger" title={selectedCard.fwd.last_error}>
                    {selectedCard.fwd.last_error}
                  </div>
                )}
              </div>
              <div className="mt-2.5 flex flex-wrap items-center gap-1.5 border-t border-kumo-line pt-2.5">
                <Button size="sm" variant="outline" onClick={() => onEdit?.(selectedCard.fwd)}>编辑</Button>
                {selectedCard.fwd.apply_status === 'running' ? (
                  <Button size="sm" variant="outline" onClick={() => onStop?.(selectedCard.fwd.id)} disabled={acting?.has(`stop:${selectedCard.fwd.id}`)}>
                    {acting?.has(`stop:${selectedCard.fwd.id}`) ? '停止中' : '停止'}
                  </Button>
                ) : (
                  <Button size="sm" variant="outline" onClick={() => onStart?.(selectedCard.fwd.id)} disabled={acting?.has(`start:${selectedCard.fwd.id}`)}>
                    {acting?.has(`start:${selectedCard.fwd.id}`) ? '启动中' : '启动'}
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => onDeploy?.(selectedCard.fwd.id)}
                  disabled={deploying?.has(selectedCard.fwd.id)}
                >
                  {deploying?.has(selectedCard.fwd.id) ? '部署中' : '部署'}
                </Button>
                <Button
                  size="sm"
                  variant={deleteConfirmActive?.(`fwd:${selectedCard.fwd.id}`) ? 'destructive' : 'secondary-destructive'}
                  className="ml-auto"
                  disabled={isDeleting}
                  onClick={() => onDelete?.(selectedCard.fwd)}
                >
                  {deleteConfirmActive?.(`fwd:${selectedCard.fwd.id}`) ? '确认删除' : '删除'}
                </Button>
              </div>
            </div>
          )}

          {/* 右下角总览图：等比缩略 + 当前视口框，点击/拖拽导航 */}
          {forwards.length > 0 && (
            <div className="absolute bottom-3 right-3 z-[7] select-none rounded-lg border border-kumo-line bg-kumo-elevated/95 p-1.5 shadow-lg backdrop-blur">
              <div
                ref={minimapRef}
                className="relative cursor-crosshair overflow-hidden rounded"
                style={{ width: minimap.w, height: minimap.h }}
                onPointerDown={(event) => {
                  event.currentTarget.setPointerCapture?.(event.pointerId);
                  jumpOnMinimap(event.clientX, event.clientY);
                }}
                onPointerMove={(event) => {
                  if (event.buttons & 1) jumpOnMinimap(event.clientX, event.clientY);
                }}
              >
                <svg width={minimap.w} height={minimap.h}>
                  {layout.hosts.map((host) => (
                    <rect
                      key={host.id}
                      x={host.x * minimap.s}
                      y={host.y * minimap.s}
                      width={host.w * minimap.s}
                      height={host.h * minimap.s}
                      rx={4}
                      fill="var(--color-kumo-line)"
                      opacity={0.6}
                    />
                  ))}
                  {layout.hosts.flatMap((host) =>
                    host.cards.map((card) => (
                      <rect
                        key={card.fwd.id}
                        x={card.x * minimap.s}
                        y={card.y * minimap.s}
                        width={(card.w || 200) * minimap.s}
                        height={(card.h || 64) * minimap.s}
                        rx={2}
                        fill={STATUS_COLORS[card.fwd.apply_status] || 'var(--color-kumo-line)'}
                        opacity={0.75}
                      />
                    ))
                  )}
                  {minimapViewRect && (
                    <rect
                      x={Math.max(0, minimapViewRect.x)}
                      y={Math.max(0, minimapViewRect.y)}
                      width={Math.min(minimapViewRect.w, minimap.w - Math.max(0, minimapViewRect.x))}
                      height={Math.min(minimapViewRect.h, minimap.h - Math.max(0, minimapViewRect.y))}
                      fill="var(--color-kumo-brand)"
                      fillOpacity={0.1}
                      stroke="var(--color-kumo-brand)"
                      strokeWidth={1}
                      rx={2}
                    />
                  )}
                </svg>
              </div>
            </div>
          )}
      </div>
    </div>
  );
}
