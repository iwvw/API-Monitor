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
const LINE_H = 20;   // 卡片内每行高度（与 text-sm/text-xs 行高对齐）
const CARD_PAD = 16; // 卡片上下内边距余量

// 卡片内容行数：名称行；地址+传输行；tcp_relay 的「→ 中继 :端口」行；故障转移行
export function cardLineCount(fwd) {
  let n = 2;
  if (fwd.transport === 'tcp_relay' && fwd.relay_server_id && fwd.relay_server_id !== fwd.server_id) n += 1;
  if (fwd.failover_current_server_id) n += 1;
  return n;
}
// 卡片高度 = 内容行数 × 行高 + 内边距，保证字体底部完整不被截断
export function cardHeightOf(fwd) {
  return cardLineCount(fwd) * LINE_H + CARD_PAD;
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

  // 中继识别：被任意规则的 relay_server_id 引用的主机视为中继节点（仅在节点卡上标「中继」徽章）
  const relayIds = new Set(
    forwards.map((f) => f.relay_server_id).filter(Boolean)
  );
  // 源主机在前、中继在后（稳定排序）
  groups.sort((a, b) => Number(relayIds.has(a.key)) - Number(relayIds.has(b.key)));

  const hostTop = PAD;
  const cardsTop = hostTop + HOST_H + 22;
  // 每列高度按各自卡片实际高度累计，画布高度取最高列
  const colHeights = groups.map((g) =>
    g.fwds.reduce((acc, f, idx) => acc + cardHeightOf(f) + (idx > 0 ? CARD_GAP_V : 0), 0)
  );
  const maxColH = colHeights.reduce((acc, h) => Math.max(acc, h), 1);
  const height = Math.max(cardsTop + maxColH + PAD, 260);
  const width =
    groups.length > 0 ? PAD + groups.length * PITCH_X - (PITCH_X - CARD_W) + PAD : 480;

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

return { width, height, hosts, edges };
}
