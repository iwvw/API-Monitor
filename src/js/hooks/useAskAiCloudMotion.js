import { useEffect, useRef } from 'react';

/* ==================== Ask AI 云朵联动物理（Cloudflare 官方行为复刻） ====================
   在真实 dash.cloudflare.com 页面逐帧采样测定的行为模型：
   1. 拖链机制（官方手感）：鼠标只作用于主球（领队，小圆 pull + 速度前馈粘质）；
      小球不受鼠标直接作用，以「主球位移 × 层级比例」为目标柔顺尾随
      （比例 0.95/0.75/0.56/0.42，尾随系数逐层减慢 → 拖链式延时，错落灵动）；
   2. 幅度轻微优雅：60px 处主球仅 ~4.4px，hover 中心 / 110px 外均静止；
      主球过阻尼（ζ≈0.68）无回弹，鼠标停后 ~150ms 内停住；
   3. scale 与位移耦合：位移 d 时 scale ≈ 1 + 0.001·|d|，叠加慢速呼吸正弦（±0.5%，
      周期~9s，逐球相位差）；空闲时缓慢随机漂移 ±(2~3)px；
   4. 鼠标作用域为以云为中心的小圆：0.6·d·e^(-d/33)，|d|>110px 完全无感
      （hover 中心→0，60px→~5.4px）；作用时漂移弱化，球团保持安静；
   5. 滤镜恒定：saturate(1) brightness(1)，模糊层 blur 固定（2.25/3.75/7.25px），
      不随速度/悬停变化（当前官方版本 --blur-multiplier 恒为 1）；
   6. 位移写入容器 CSS 变量 --node-N-x/y/scale（N=1..4，模糊层/阴影的 clip-path
      通过变量链实时跟随球体），节点 transform 与官方逐字节同构：
      translateX(xpx) translateY(ypx) scale(s)，零值分量省略；
   7. 阴影层 transform 与主球完全一致（官方实测同值），滤镜独立。 */

const NODES = [
  { id: 1, lead: 1.0, phase: 0.0 }, // 主球（最大，左上 8%）完全跟手
  { id: 2, lead: 0.75, phase: 2.5 },
  { id: 3, lead: 0.95, phase: 0.7 }, // 右大球：几乎与主球同步
  { id: 4, lead: 0.56, phase: 1.9 },
  { id: 5, lead: 0.42, phase: 3.1 }, // 最小球：跟随最慢
];

const FOLLOW_BLUR = { 2: 2.25, 1: 3.75 }; // 模糊层 blur px（与官方 CSS 一致）
const SHADOW_BLUR = 7.25;

// 粘质拖链模型（官方手感）：鼠标只作用于主球；小球被主球位移带动，
// 按层级延时柔顺尾随（拖链式，优雅不躁）；主球过阻尼，停得快
const SPRING_K = 0.05; // 主球回位弹簧（弱）→ 鼠标停后缓缓归位
const SPRING_DAMP = 0.74; // 主球 ζ≈0.68 过阻尼：无回弹，停止后 ~150ms 内停住
const DRAG_MAIN = 0.05; // 主球速度前馈（粘质：鼠标速度小比例被主球承接，仅作用圆内）
const VEL_SMOOTH = 0.5; // 鼠标速度平滑（防抖动尖峰）
const MAX_V = 2.5; // 主球速度上限（px/帧，防飞）
const FOLLOW_EASE = [0, 0.12, 0.12, 0.11, 0.1]; // 小球尾随系数（一阶平滑）
// 拖链历史：小球跟随主球 N 帧前的位移快照 × 比例（真正的「被拖着走」时序）
const CHAIN_HIST = 60; // 历史深度（帧，1s）
const CHAIN_DELAY = [0, 6, 4, 10, 14]; // 各球延迟帧数（越远拖得越久）
// 独立微摆：每球两段不同频率正弦（各自呼吸晃动 → 灵动不死板），幅度轻微
const WOBBLE_A = [0, 0.8, 0.7, 1.0, 1.1];
const WOBBLE_F1 = (2 * Math.PI) / 1900; // ~1.9s 主摆
const WOBBLE_F2 = (2 * Math.PI) / 3100; // ~3.1s 副摆
const SCALE_K = 0.06; // scale 线性轻弹簧（缩放保持稳定，不弹）
const SCALE_DAMP = 0.9;
// 鼠标牵引：A·d·e^(-d/λ) · 硬截断。以云为中心的小圆作用域：
// 60px→~5.8px、100px→~3px、110px 之外完全无感；hover 中心→0（官方特性）
const MOUSE_A = 0.45; // 幅度轻微（60px → ~4.4px），优雅不大动
const MOUSE_LAMBDA = 33;
const MOUSE_RANGE = 110; // 作用圆半径（px），靠近这个圆才被捕获
const STRETCH = 0.001; // 位移 → 膨胀耦合（轻微）
const BREATH_AMP = 0.005; // 呼吸缩放振幅（很轻）
const BREATH_FREQ = (2 * Math.PI) / 9000; // 呼吸周期 ~9s（慢）
const WANDER_X = 3; // 空闲漂移幅度（px，官方稳态漂移很小）
const WANDER_Y = 2;
const WANDER_RETARGET = 4000; // 每 4s 往新漂移目标小幅演化（无跳变）
const WANDER_HOVER_FADE = 0.3; // 鼠标作用时漂移弱化系数（hover 时球团保持安静，官方特性）

function mousePull(d) {
  // 符号保留的距离衰减牵引；超范围截断。d=像素（相对云中心）
  if (Math.abs(d) > MOUSE_RANGE) return 0;
  return Math.sign(d) * MOUSE_A * Math.abs(d) * Math.exp(-Math.abs(d) / MOUSE_LAMBDA);
}

function nodeIndexByClass(cls) {
  if (cls.includes('shadow')) return 0; // 阴影跟随主球
  const blur = cls.match(/node-(\d)-blur/);
  if (blur) return Number(blur[1]) - 1;
  return Number(cls.match(/node-(\d)/)[1]) - 1;
}

function formatTransform(x, y, s) {
  // 官方格式：translateX/translateY 零值分量省略，scale 恒写
  let t = '';
  if (Math.abs(x) >= 0.0005) t += `translateX(${x.toFixed(6)}px) `;
  if (Math.abs(y) >= 0.0005) t += `translateY(${y.toFixed(6)}px) `;
  return `${t}scale(${s.toFixed(6)})`;
}

export function useAskAiCloudMotion(containerRef) {
  // 物理状态放 ref，与 React 渲染解耦
  const stateRef = useRef(null);
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return undefined;

    const visibleNodes = [
      { cls: 'askai-cloud-node-5', blur: null },
      { cls: 'askai-cloud-node-4', blur: null },
      { cls: 'askai-cloud-node-2-blur', blur: FOLLOW_BLUR[2] },
      { cls: 'askai-cloud-node-3', blur: null },
      { cls: 'askai-cloud-node-2', blur: null },
      { cls: 'askai-cloud-node-1-shadow', blur: SHADOW_BLUR },
      { cls: 'askai-cloud-node-1-blur', blur: FOLLOW_BLUR[1] },
      { cls: 'askai-cloud-node-1', blur: null },
    ]
      .map((v) => ({ ...v, el: container.querySelector(`.${v.cls}`) }))
      .filter((v) => v.el);
    if (!visibleNodes.some((v) => v.cls === 'askai-cloud-node-1')) return undefined;

    const init = stateRef.current || NODES.map(() => ({ x: 0, y: 0, vx: 0, vy: 0, s: 1, vs: 0 }));
    const leadHist = []; // 主球位移历史（拖链延迟源）
    const wanderT = NODES.map(() => ({ x: 0, y: 0 }));
    const mouse = { x: 0, y: 0, lastX: 0, lastY: 0, sx: 0, sy: 0, active: false };
    let lastPick = 0;

    const onMove = (e) => {
      const cr = container.getBoundingClientRect();
      mouse.x = e.clientX - (cr.left + cr.width / 2);
      mouse.y = e.clientY - (cr.top + cr.height / 2);
      mouse.active = true; // 静止悬停保持活跃（仅离开窗口才失效）
    };
    const onLeave = () => {
      mouse.active = false;
    };
    // 官方在云朵 60px 外仍有强牵引 → 监听 window 级 move
    window.addEventListener('pointermove', onMove, { passive: true });
    window.addEventListener('pointerleave', onLeave, { passive: true });

    let raf = 0;
    const start = performance.now();
    const tick = (now) => {
      raf = requestAnimationFrame(tick);
      const t = now - start;

      // 空闲漂移目标：低频随机游走，小幅增量演化（无跳变，契合官方 slow drift）
      if (now - lastPick > WANDER_RETARGET) {
        lastPick = now;
        for (let i = 0; i < NODES.length; i++) {
          wanderT[i].x += (Math.random() * 2 - 1) * WANDER_X;
          wanderT[i].y += (Math.random() * 2 - 1) * WANDER_Y;
          wanderT[i].x = Math.max(-WANDER_X, Math.min(WANDER_X, wanderT[i].x));
          wanderT[i].y = Math.max(-WANDER_Y, Math.min(WANDER_Y, wanderT[i].y));
        }
      }

      const px = new Array(NODES.length);
      const py = new Array(NODES.length);
      const ps = new Array(NODES.length);

      // 鼠标速度（帧间位移，平滑）——粘质拖拽的前馈源
      // 仅当鼠标位于作用圆内时前馈才有效（圆外完全不影响云朵）
      // 鼠标活跃状态：悬停保持，离开 window 才失效
      const mActive = mouse.active;
      const mouseInRange = Math.hypot(mouse.x, mouse.y) <= MOUSE_RANGE && mActive;
      if (mActive) {
        mouse.sx = mouse.sx * VEL_SMOOTH + (mouse.x - mouse.lastX) * (1 - VEL_SMOOTH);
        mouse.sy = mouse.sy * VEL_SMOOTH + (mouse.y - mouse.lastY) * (1 - VEL_SMOOTH);
      } else {
        mouse.sx *= 0.8;
        mouse.sy *= 0.8;
      }
      mouse.lastX = mouse.x;
      mouse.lastY = mouse.y;

      for (let i = 0; i < NODES.length; i++) {
        const st = init[i];
        const lead = NODES[i].lead;
        if (i === 0) {
          // 主球：直接受鼠标作用（小圆 pull + 范围内速度前馈粘质），过阻尼回位 ——
          let tx = wanderT[0].x;
          let ty = wanderT[0].y;
          if (mActive) {
            tx = tx * WANDER_HOVER_FADE;
            ty = ty * WANDER_HOVER_FADE;
            tx += mousePull(mouse.x);
            ty += mousePull(mouse.y) * 0.55;
          }
          const feed = mouseInRange ? DRAG_MAIN : 0; // 作用圆外：无拖拽前馈
          st.vx = (st.vx + (tx - st.x) * SPRING_K + mouse.sx * feed) * SPRING_DAMP;
          st.vy = (st.vy + (ty - st.y) * SPRING_K + mouse.sy * feed) * SPRING_DAMP;
          st.vx = Math.max(-MAX_V, Math.min(MAX_V, st.vx));
          st.vy = Math.max(-MAX_V, Math.min(MAX_V, st.vy));
          st.x += st.vx;
          st.y += st.vy;
        } else {
          // —— 小球：跟随主球 N 帧前的位移快照 × 比例（拖链时序）+ 独立微摆（灵动） ——
          const fade = mActive ? WANDER_HOVER_FADE : 1;
          const prev = leadHist[Math.max(0, leadHist.length - 1 - CHAIN_DELAY[i])];
          const px0 = prev ? prev.x : 0;
          const py0 = prev ? prev.y : 0;
          const wobX = Math.sin(t * WOBBLE_F1 * (0.77 * i + 0.53)) * WOBBLE_A[i]
            + Math.sin(t * WOBBLE_F2 * (1.31 * i + 0.2)) * WOBBLE_A[i] * 0.5;
          const wobY = Math.cos(t * WOBBLE_F1 * (0.91 * i + 0.71)) * WOBBLE_A[i] * 0.7
            + Math.sin(t * WOBBLE_F2 * (0.63 * i + 1.03)) * WOBBLE_A[i] * 0.35;
          const tx = px0 * lead + wanderT[i].x * fade + wobX;
          const ty = py0 * lead + wanderT[i].y * fade + wobY;
          st.x += (tx - st.x) * FOLLOW_EASE[i];
          st.y += (ty - st.y) * FOLLOW_EASE[i];
        }
        const dist = Math.hypot(st.x, st.y);
        const ts = 1 + STRETCH * dist + Math.sin(t * BREATH_FREQ + NODES[i].phase) * BREATH_AMP;
        st.vs = (st.vs + (ts - st.s) * SCALE_K) * SCALE_DAMP;
        st.s += st.vs;
        px[i] = st.x;
        py[i] = st.y;
        ps[i] = st.s;
      }
      stateRef.current = init;
      // 记录主球位移历史（下一帧小球拖链用）
      leadHist.push({ x: px[0], y: py[0] });
      if (leadHist.length > CHAIN_HIST) leadHist.shift();

      // —— 官方同构输出：CSS 变量（驱动 blur/shadow 的 clip-path 链）+ inline transform ——
      for (let i = 0; i < NODES.length; i++) {
        const id = NODES[i].id;
        if (id <= 4) {
          container.style.setProperty(`--node-${id}-x`, px[i].toFixed(6));
          container.style.setProperty(`--node-${id}-y`, py[i].toFixed(6));
          container.style.setProperty(`--node-${id}-scale`, ps[i].toFixed(6));
        }
      }
      for (const v of visibleNodes) {
        const idx = nodeIndexByClass(v.cls); // 层级索引：5→4, 4→3, … 1-blur/shadow→0
        const tf = formatTransform(px[idx], py[idx], ps[idx]);
        v.el.style.transform = tf;
        v.el.style.opacity = '1';
        v.el.style.filter = v.blur
          ? `blur(calc(${v.blur}px)) saturate(1) brightness(1)`
          : 'saturate(1) brightness(1)';
      }
      container.style.setProperty('--blur-multiplier', '1');
    };
    raf = requestAnimationFrame(tick);

    return () => {
      cancelAnimationFrame(raf);
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerleave', onLeave);
    };
  }, [containerRef]);
}