// TileGrid —— 可复用的图块网格：宽度自适应列数、动态行高保证 1×1 正方形、
// 档位吸附（对齐 Cloudflare 离散尺寸模型）、越界下沉重排、自定义缩放柄、拖拽占位符品牌色。
// 用法：<TileGrid layout={layout} onLayoutChange={persist} renderTile={(item) => <TileFrame>…</TileFrame>} />
// 实时预览：RGL v2 拖动/缩放过程中 onLayoutChange 被 activeDrag 跳过（松手才回传），
// 这里用 ResizeObserver 实时读取 item 像素尺寸换算为连续列/行数，覆盖传给 renderTile，
// 使拖拽跨越档位阈值时卡片内部布局立即重排（而非松手后才变）。
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import GridLayout, { useContainerWidth, verticalCompactor } from 'react-grid-layout';
import 'react-grid-layout/css/styles.css';
import 'react-resizable/css/styles.css';
import TileResizeHandle from './TileResizeHandle.jsx';

// 尺寸档位：h=2 为 1×1 正方形单元，h=1 为半高卡（两张可上下叠放占满一个单元），h=4 为双高富内容卡。
// 含 3 列档位（3×1 / 3×2）：部分卡（如模块入口）可吸附到三列宽布局。
const DEFAULT_TILE_SIZES = [
  { w: 1, h: 1 },
  { w: 1, h: 2 },
  { w: 2, h: 1 },
  { w: 2, h: 2 },
  { w: 2, h: 4 },
  { w: 3, h: 1 },
  { w: 3, h: 2 },
  { w: 4, h: 1 },
  { w: 4, h: 2 },
];

// 吸附到档位：默认取最近档位；拖拽方向明确时（w/h 变化超过半档）对「小于等于原始值」的
// 回退档位加惩罚，使中等拖拽能推进到下一档（RGL 缩放值是整数列，w3 时 {2,2} 与 {4,2} 等距，
// 不罚原始档位会导致任何拖拽都弹回原尺寸）。尊重卡片 minW/minH：不满足约束的档位直接排除，
// 避免模块入口（minW:2）等被吸附回 1 列。
function snapTileSize(w, h, sizes, prevW, prevH, minW = 1, minH = 1) {
  const dw = w - prevW;
  const dh = h - prevH;
  let best = null;
  let bestScore = Infinity;
  for (const s of sizes) {
    if (s.w < minW || s.h < minH) continue;
    let score = (s.w - w) ** 2 + (s.h - h) ** 2;
    if (dw > 0.05 && s.w <= prevW) score += 2;
    if (dw < -0.05 && s.w >= prevW) score += 2;
    if (dh > 0.05 && s.h <= prevH) score += 2;
    if (dh < -0.05 && s.h >= prevH) score += 2;
    if (score < bestScore) {
      bestScore = score;
      best = s;
    }
  }
  // 无满足约束的档位时回退到最小约束尺寸
  return best || { w: Math.max(minW, w), h: Math.max(minH, h) };
}

// 将布局约束到当前列数：卡片尺寸只允许手动拖拽调整，不做自动扩展/重排；
// 仅当存在越界（宽超列数/左越界）时做必要的缩窄/下沉，防止超宽溢出。
function fitLayoutToCols(layout, cols) {
  if (!Array.isArray(layout) || !layout.length) return layout;
  const needsFit = layout.some((it) => it.w > cols || it.x + it.w > cols || it.x < 0);
  return needsFit ? fitOverflow(layout, cols) : layout;
}

// 溢出修正：越界项缩窄/左移，行内冲突下沉（保持原有 y 优先，避免整体重排）
function fitOverflow(layout, cols) {
  const occupied = new Set();
  return layout.map((it) => {
    const w = Math.min(it.w, cols);
    const x = Math.max(0, Math.min(it.x, Math.max(0, cols - w)));
    let y = Math.max(0, it.y);
    for (; y < 500; y += 1) {
      let ok = true;
      for (let yy = 0; yy < it.h && ok; yy += 1) {
        for (let xx = x; xx < x + w; xx += 1) {
          if (occupied.has(`${xx},${y + yy}`)) {
            ok = false;
            break;
          }
        }
      }
      if (ok) break;
    }
    for (let yy = 0; yy < it.h; yy += 1) {
      for (let xx = x; xx < x + w; xx += 1) occupied.add(`${xx},${y + yy}`);
    }
    return { ...it, x, y, w };
  });
}

// 行尾补全已移除：卡片尺寸只允许手动拖拽调整，不随屏幕宽度自动扩展。

export { DEFAULT_TILE_SIZES };

export default function TileGrid({
  layout,
  onLayoutChange,
  renderTile,
  onColsChange,
  className = '',
  tileSizes = DEFAULT_TILE_SIZES,
  maxCols = 8,
  cols: colsOverride,
}) {
  const { width, containerRef, mounted } = useContainerWidth();
  // 列数：外部 cols 优先（按断点固定，断点内宽度变化列数不变、布局稳定）；
  // 未指定时随容器宽度自适应（约 250px 一列），宽仪表盘自动展更多列
  const cols = colsOverride ?? (mounted ? Math.min(maxCols, Math.max(2, Math.round(width / 250))) : 4);
  // 行高随当前列宽动态计算，使 1×1（h=2）在任何宽度下都是精确正方形：2*rowHeight + marginY = 列宽
  // containerPadding 归零：嵌入仪表盘时与页面 gutter（12px）对齐，避免网格内容额外内缩一圈
  const colWidth = (width - (cols - 1) * 12) / cols;
  const rowHeight = Math.max(72, Math.min(150, (colWidth - 12) / 2));

  const onColsChangeRef = useRef(onColsChange);
  onColsChangeRef.current = onColsChange;
  useEffect(() => {
    onColsChangeRef.current?.(cols);
  }, [cols]);

  const fittedLayout = useMemo(() => fitLayoutToCols(layout, cols), [layout, cols]);

  const layoutRef = useRef(layout);
  layoutRef.current = layout;
  const onLayoutChangeRef = useRef(onLayoutChange);
  onLayoutChangeRef.current = onLayoutChange;
  // RGL v2 在缩放停止后会回传原始尺寸（未经吸附），若每次都重新吸附会形成 2x2↔4x2 无限循环。
  // 记录每项「最近上报的原始尺寸」与「已提交的档位」：同一原始尺寸重复上报时直接透传已提交档位。
  const rawDeliveredRef = useRef(new Map());
  const committedRef = useRef(new Map());

  // —— 拖动中的实时尺寸预览 ——
  // 像素换算公式（RGL 布局几何）：单列/单行像素增量 = 列宽/行高 + 12(margin)，item 像素 = w*colWidth + (w-1)*12，
  // 反推 w = (px + 12) / (colWidth + 12)，h 同理。换算出的连续值仅当与档位差异超过半档时覆盖渲染。
  const liveSizesRef = useRef(new Map()); // i -> { w, h } 连续换算值
  const spanRef = useRef({ colWidth, rowHeight }); // RO 回调读取实时列宽/行高（避免闭包捕获旧值）
  spanRef.current = { colWidth, rowHeight };
  const [, bumpRender] = useState(0);
  const roRef = useRef(null);
  const observedRef = useRef(new Set());
  // 实时预览只在用户拖拽缩放手柄期间生效；窗口 resize 引发的尺寸变化不覆盖渲染，
  // 避免与 RGL 重排的暂态像素打架导致内容布局错乱
  const resizingRef = useRef(false);
  // 触屏设备（移动端）防误触：长按 0.6s 震动提示后才开始拖动；拖动启动阈值也调大
  const isCoarsePointer = typeof window !== 'undefined' && window.matchMedia('(pointer: coarse)').matches;
  const hapticTimerRef = useRef(0);
  // 用户手势标记：拖拽/缩放手势结束后保留短暂窗口（RGL 松手后才回传 onLayoutChange），
  // 用于区分「用户操作的回传」与「列数变化/窗口 resize 的自适应重排回传」——
  // 后者不得写回持久布局，否则移动端↔桌面端切换会把重排结果永久保存导致布局错乱。
  const userGestureRef = useRef(false);
  let gestureTimer = 0;
  useEffect(() => {
    const onDown = (e) => {
      if (e.target && e.target.closest && e.target.closest('.react-resizable-handle, .react-grid-item')) {
        resizingRef.current = true;
        userGestureRef.current = true;
        window.clearTimeout(gestureTimer);
        if (isCoarsePointer) {
          window.clearTimeout(hapticTimerRef.current);
          hapticTimerRef.current = window.setTimeout(() => {
            try {
              navigator.vibrate?.(12);
            } catch {
              // 环境不支持震动则静默
            }
          }, 600);
        }
      }
    };
    const onUp = () => {
      resizingRef.current = false;
      window.clearTimeout(gestureTimer);
      window.clearTimeout(hapticTimerRef.current);
      gestureTimer = window.setTimeout(() => {
        userGestureRef.current = false;
      }, 300);
    };
    window.addEventListener('pointerdown', onDown, true);
    window.addEventListener('pointerup', onUp, true);
    window.addEventListener('pointercancel', onUp, true);
    return () => {
      window.removeEventListener('pointerdown', onDown, true);
      window.removeEventListener('pointerup', onUp, true);
      window.removeEventListener('pointercancel', onUp, true);
      window.clearTimeout(gestureTimer);
    };
  }, []);

  const handleGridChange = useCallback((next) => {
    // 非用户手势（窗口 resize / 列数变化触发的自适应重排）不回写持久布局，
    // 避免移动端↔桌面端切换把临时重排结果永久保存
    if (!userGestureRef.current) return;
    const prevMap = new Map(layoutRef.current.map((it) => [it.i, it]));
    let changed = false;
    const mapped = next.map((it) => {
      const old = prevMap.get(it.i);
      if (!old || (old.w === it.w && old.h === it.h)) return it;
      const lastRaw = rawDeliveredRef.current.get(it.i);
      if (lastRaw && lastRaw.w === it.w && lastRaw.h === it.h) {
        const committed = committedRef.current.get(it.i);
        if (committed && (committed.w !== it.w || committed.h !== it.h)) {
          changed = true;
          return { ...it, w: committed.w, h: committed.h };
        }
        return it;
      }
      rawDeliveredRef.current.set(it.i, { w: it.w, h: it.h });
      const snap = snapTileSize(it.w, it.h, tileSizes, old.w, old.h, it.minW || 1, it.minH || 1);
      committedRef.current.set(it.i, { w: snap.w, h: snap.h });
      if (snap.w !== it.w || snap.h !== it.h) {
        changed = true;
        return { ...it, w: snap.w, h: snap.h };
      }
      return it;
    });
    onLayoutChangeRef.current?.(changed ? mapped : next);
  }, [tileSizes]);

  // 观察所有图块容器：像素尺寸换算连续档位，与 RGL 档位差异超过半档时实时覆盖渲染。
  // 松手后 RGL 回传吸附档位，换算值回落回 item 档位，自动停止覆盖。
  const renderItem = useCallback((item) => {
    const live = liveSizesRef.current.get(item.i);
    if (live && (Math.abs(live.w - item.w) > 0.3 || Math.abs(live.h - item.h) > 0.3)) {
      // 实时预览同样尊重 minW/minH，避免带约束的卡在拖动中闪现越界尺寸
      return renderTile({
        ...item,
        w: Math.max(live.w, item.minW || 1),
        h: Math.max(live.h, item.minH || 1),
      });
    }
    return renderTile(item);
  }, [renderTile]);

  useEffect(() => {
    if (!mounted) return undefined;
    if (!roRef.current) {
      roRef.current = new ResizeObserver((entries) => {
        // 非拖拽（含窗口 resize）时清掉残留的实时覆盖，不做换算覆盖
        if (!resizingRef.current) {
          if (liveSizesRef.current.size) {
            liveSizesRef.current.clear();
            bumpRender((n) => n + 1);
          }
          return;
        }
        const { colWidth: cw, rowHeight: rh } = spanRef.current;
        const span = cw + 12;
        const spanH = rh + 12;
        let changed = false;
        for (const entry of entries) {
          const el = entry.target;
          const i = el.dataset?.tileItem;
          if (!i) continue;
          const fw = (el.offsetWidth + 12) / span;
          const fh = (el.offsetHeight + 12) / spanH;
          const item = layoutRef.current?.find((it) => it.i === i);
          if (!item) continue;
          const prev = liveSizesRef.current.get(i);
          const differs = Math.abs(fw - item.w) > 0.3 || Math.abs(fh - item.h) > 0.3;
          if (differs) {
            if (!prev || Math.abs(prev.w - fw) > 0.05 || Math.abs(prev.h - fh) > 0.05) {
              liveSizesRef.current.set(i, { w: fw, h: fh });
              changed = true;
            }
          } else if (prev) {
            liveSizesRef.current.delete(i);
            changed = true;
          }
        }
        if (changed) bumpRender((n) => n + 1);
      });
    }
    // 新挂载的项纳入观察（去重），无需重建 RO
    const els = containerRef.current?.querySelectorAll('[data-tile-item]') || [];
    els.forEach((el) => {
      if (observedRef.current.has(el)) return;
      observedRef.current.add(el);
      roRef.current.observe(el);
    });
    return undefined;
  });

  return (
    <div
      ref={containerRef}
      className={`w-full select-none [&_.react-grid-item.react-grid-placeholder]:rounded-lg! [&_.react-grid-item.react-grid-placeholder]:bg-brand/30! ${className}`}
    >
      {mounted && layout != null && (
        <GridLayout
          width={width}
          layout={fittedLayout}
          gridConfig={{ cols, rowHeight, margin: [12, 12], containerPadding: [0, 0] }}
          dragConfig={{ enabled: true, handle: '.tile-header', cancel: 'canvas, button, select, input, a, .tile-scroll', threshold: isCoarsePointer ? 15 : 3 }}
          resizeConfig={{ enabled: true, handles: ['se'], handleComponent: TileResizeHandle }}
          compactor={verticalCompactor}
          onLayoutChange={handleGridChange}
        >
          {fittedLayout.map((item) => (
            <div key={item.i} data-tile-item={item.i} className="h-full min-h-0">
              {renderItem(item)}
            </div>
          ))}
        </GridLayout>
      )}
    </div>
  );
}