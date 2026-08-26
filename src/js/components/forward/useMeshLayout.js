/**
 * 转发中心画布 — 横向树形布局（纯函数，零 DOM 测量）
 *
 * 信息架构：主机节点行 → 各自主机的转发规则卡。无中心 logo：
 * 面板本机作为一台虚拟主机参与分组。横向分布，主机从左到右排开，
 * 规则卡在各自主机正下方纵向堆叠。
 *
 * 排序规则：源主机在前、中继主机在后——中转机是服务角色而非来源角色，
 * 放行尾并用 isRelay 标记，由渲染层加徽标区分。
 */

export const CARD_W = 200;
export const CARD_H = 64;
export const HOST_W = 200;
export const HOST_H = 48;

export const STATUS_COLORS = {
  running: 'var(--color-kumo-success)',
  deploying: 'var(--color-kumo-info)',
  failed: 'var(--color-kumo-danger)',
  disconnected: 'var(--color-kumo-warning)',
  stopped: 'var(--color-kumo-line)',
  pending: 'var(--color-kumo-line)',
};

export const STATUS_LABELS = {
  pending: '待部署',
  deploying: '部署中',
  running: '运行中',
  stopped: '已停止',
  failed: '失败',
  disconnected: '已断开',
};

export const TRANSPORT_LABELS = {
  cloudflare_tunnel: 'CF Tunnel',
  tcp_relay: 'TCP 中继',
  p2p: 'P2P',
};

export const TRANSPORT_SHORT = {
  cloudflare_tunnel: 'CF',
  tcp_relay: 'TCP',
  p2p: 'P2P',
};

export function isHostOnline(server) {
  if (!server) return false;
  return server.status === 'online' || server.agent_online === true || server.agent_connected === true;
}

// 几何常量
const PAD = 28;
const GUTTER_W = 34; // 主机左缘与卡片之间的分支竖槽走廊
const PITCH_X = CARD_W + GUTTER_W + 28; // 主机列间距
const CARD_GAP_V = 12;

// 卡片高度自适应：带故障转移行的卡片更高，避免三行内容被截断
export function cardHeightOf(fwd) {
  return fwd && fwd.failover_current_server_id ? 82 : CARD_H;
}

export function buildTreeLayout(forwards = [], servers = []) {
  const groups = [];
  const index = new Map();
  forwards.forEach((fwd) => {
    const key = fwd.server_id || 'unknown';
    if (!index.has(key)) {
      index.set(key, { key, server: servers.find((s) => s.id === key), fwds: [] });
      groups.push(index.get(key));
    }
    index.get(key).fwds.push(fwd);
  });

  // 中继识别：被任意规则的 relay_server_id 引用的主机视为中继节点
  const relayIds = new Set(
    forwards.map((f) => f.relay_server_id).filter(Boolean)
  );
  // 依赖关系：中继机 ← 上游源主机集合（自中继除外）
  const deps = [];
  const seenPairs = new Set();
  forwards.forEach((f) => {
    if (!f.relay_server_id || f.relay_server_id === f.server_id) return;
    if (!index.has(f.relay_server_id)) return; // 中继主机自身无规则时不出列
    const pairKey = `${f.server_id}->${f.relay_server_id}`;
    if (seenPairs.has(pairKey)) return;
    seenPairs.add(pairKey);
    deps.push({ from: f.server_id, to: f.relay_server_id });
  });
  // 源主机在前、中继在后（稳定排序）
  groups.sort((a, b) => Number(relayIds.has(a.key)) - Number(relayIds.has(b.key)));

  // 有依赖弧线时在主机行上方预留走廊
  const DEP_BAND = deps.length > 0 ? 52 : 0;
  const hostTop = PAD + DEP_BAND;
  const cardsTop = hostTop + HOST_H + 22;
  // 每列高度按各自卡片实际高度累计，画布高度取最高列
  const colHeights = groups.map((g) =>
    g.fwds.reduce((acc, f, idx) => acc + cardHeightOf(f) + (idx > 0 ? CARD_GAP_V : 0), 0)
  );
  const maxColH = colHeights.reduce((acc, h) => Math.max(acc, h), 1);
  const height = Math.max(cardsTop + maxColH + PAD, 260);
  const width =
    groups.length > 0 ? PAD + groups.length * PITCH_X - (PITCH_X - CARD_W) + PAD : 480;

  const hostCXByKey = new Map();
  groups.forEach((g, i) => {
    hostCXByKey.set(g.key, PAD + i * PITCH_X + HOST_W / 2);
  });

  const hosts = [];
  const edges = [];
  groups.forEach((g, i) => {
    const x = PAD + i * PITCH_X;
    const cx = x + HOST_W / 2;
    const hostCY = hostTop + HOST_H / 2;
    let yCursor = cardsTop;
    const cardX = x + GUTTER_W;
    const cards = g.fwds.map((fwd) => {
      const h = cardHeightOf(fwd);
      const card = {
        fwd,
        x: cardX,
        y: yCursor,
        w: CARD_W,
        h,
        cx: cardX + CARD_W / 2,
        cy: yCursor + h / 2,
      };
      yCursor += h + CARD_GAP_V;
      return card;
    });
    const host = {
      id: `host-${g.key}`,
      serverId: g.key,
      name: g.server?.name || g.key,
      online: isHostOnline(g.server),
      isRelay: relayIds.has(g.key),
      x,
      y: hostTop,
      cx,
      cy: hostCY,
      w: HOST_W,
      h: HOST_H,
      cards,
    };
    // 一主干多分支：单条竖直主干从主机底边引出（走卡片左侧走廊），
    // 延伸到最深一张卡的高度；每张卡从主干侧面水平分支接入
    if (cards.length > 0) {
      const TRUNK_X = x + 14;
      edges.push({
        id: `spine-${g.key}`,
        kind: 'spine',
        hostId: g.key,
        path: `M ${TRUNK_X} ${hostTop + HOST_H} V ${cards[cards.length - 1].cy}`,
      });
      cards.forEach((card) => {
        edges.push({
          id: `branch-${card.fwd.id}`,
          kind: 'branch',
          hostId: g.key,
          fwdId: card.fwd.id,
          status: card.fwd.apply_status,
          // 底层只画短水平接入段；流光层走完整路径（主机底 → 主干 → 卡片）
          path: `M ${TRUNK_X} ${card.cy} H ${card.x}`,
          flowPath: `M ${TRUNK_X} ${hostTop + HOST_H} V ${card.cy} H ${card.x}`,
        });
      });
    }
    hosts.push(host);
  });

// 依赖折线：每条独占一个水平轨道（不同 y，互不重叠）；同一主机顶边的全部
    // 连接点（发出 + 落入）统一编号、沿顶边均匀分槽——任何两段线不共点不共段
    const topTotal = new Map();
    deps.forEach((d) => {
      topTotal.set(d.from, (topTotal.get(d.from) || 0) + 1);
      topTotal.set(d.to, (topTotal.get(d.to) || 0) + 1);
    });
    const topUsed = new Map();
    const slotX = (hostKey) => {
      const base = hostCXByKey.get(hostKey);
      const total = topTotal.get(hostKey) || 1;
      const idx = topUsed.get(hostKey) || 0;
      topUsed.set(hostKey, idx + 1);
      if (total <= 1) return base;
      return base - (HOST_W / 2 - 20) + (HOST_W - 40) * (idx / (total - 1));
    };
    const laneStepDep = Math.min(12, Math.floor((DEP_BAND - 10) / Math.max(deps.length, 1)));
    deps.forEach((d, laneIdx) => {
      const ux = slotX(d.from);
      const rx = slotX(d.to);
      if (ux == null || rx == null || ux === rx) return;
      const laneY = PAD + 8 + laneIdx * laneStepDep;
      edges.push({
        id: `dep-${d.from}-${d.to}`,
        kind: 'dep',
        hostId: d.to,
        fromHostId: d.from,
        path: `M ${ux} ${hostTop} V ${laneY} H ${rx} V ${hostTop}`,
      });
    });

  return { width, height, hosts, edges };
}
