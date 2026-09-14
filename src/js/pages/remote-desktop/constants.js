export const ICE_SERVERS = [
  { urls: ['stun:stun.cloudflare.com:3478', 'stun:stun.l.google.com:19302'] },
];
export const SIGNAL_POLL_MS = 300;

export const STATE_LABELS = {
  initializing: '正在初始化',
  connecting: '正在打洞',
  signaling: '正在协商',
  connected: 'P2P 已直连',
  disconnected: '连接中断',
  failed: '直连失败',
  closed: '会话已结束',
  error: '连接错误',
};

export const CANDIDATE_TYPE_LABELS = {
  host: '同网直连',
  srflx: '公网打洞',
  prflx: '双向打洞',
  relay: '中继转发',
};
