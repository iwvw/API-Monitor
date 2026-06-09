async function parseResponse(response) {
  const data = await response.json().catch(() => ({}));
  if (!response.ok || data.success === false) {
    const error = new Error(data.error || 'SFTP 请求失败');
    error.code = data.code;
    error.details = data.details;
    throw error;
  }
  return data;
}

export async function listSftpDirectory(serverId, path = '.') {
  const response = await fetch('/api/server/sftp/list', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ serverId, path }),
  });
  return parseResponse(response);
}

export async function readSftpFile(serverId, path, maxSize) {
  const response = await fetch('/api/server/sftp/read', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ serverId, path, maxSize }),
  });
  return parseResponse(response);
}

export async function writeSftpFile(serverId, path, content) {
  const response = await fetch('/api/server/sftp/write', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ serverId, path, content }),
  });
  return parseResponse(response);
}

export async function createSftpDirectory(serverId, path) {
  const response = await fetch('/api/server/sftp/mkdir', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ serverId, path }),
  });
  return parseResponse(response);
}

export async function renameSftpPath(serverId, oldPath, newPath) {
  const response = await fetch('/api/server/sftp/rename', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ serverId, oldPath, newPath }),
  });
  return parseResponse(response);
}

export async function deleteSftpPath(serverId, path, isDirectory = false, recursive = false) {
  const response = await fetch(isDirectory ? '/api/server/sftp/rmdir' : '/api/server/sftp/delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ serverId, path, recursive }),
  });
  return parseResponse(response);
}

export async function chmodSftpPath(serverId, path, mode) {
  const response = await fetch('/api/server/sftp/chmod', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ serverId, path, mode }),
  });
  return parseResponse(response);
}

export async function uploadSftpFile(serverId, path, file, relativePath) {
  const formData = new FormData();
  formData.append('serverId', serverId);
  formData.append('path', path);
  formData.append('file', file);
  if (relativePath) formData.append('relativePath', relativePath);

  const response = await fetch('/api/server/sftp/upload', {
    method: 'POST',
    body: formData,
  });
  return parseResponse(response);
}

export function buildSftpDownloadUrl(serverId, path) {
  return `/api/server/sftp/download/${encodeURIComponent(serverId)}?path=${encodeURIComponent(path)}`;
}
