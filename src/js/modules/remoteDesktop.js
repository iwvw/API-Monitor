const WINDOWS_PATTERN = /windows|win32|win64/i;

export function remoteDesktopPlatform(server = {}) {
  return String(server.platform || server.info?.platform || server.metadata?.platform || '').trim();
}

// 后端创建会话时只校验 remote_desktop_v1（backend-go/internal/serveragent/remote_desktop.go），
// 前端入口判定必须与之对齐，否则会出现按钮可点但创建必然失败的假阳性。
export function hasRemoteDesktopCapability(server = {}) {
  return server.agent_capabilities?.remote_desktop_v1 === true
    || server.capabilities?.remote_desktop_v1 === true;
}

export function canOpenRemoteDesktop(server = {}) {
  const online = server.is_online === true || server.agent_online === true || server.status === 'online';
  return online && WINDOWS_PATTERN.test(remoteDesktopPlatform(server)) && hasRemoteDesktopCapability(server);
}

export function remoteDesktopPath(serverId) {
  return `/remote-desktop/${encodeURIComponent(String(serverId || '').trim())}`;
}
