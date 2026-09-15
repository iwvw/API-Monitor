import { post } from './apiClient.js';

const FALLBACK = 'SFTP 请求失败';

export async function listSftpDirectory(serverId, path = '.') {
  return post('/api/server/sftp/list', { serverId, path }, { fallbackMessage: FALLBACK });
}

export async function readSftpFile(serverId, path, maxSize) {
  return post('/api/server/sftp/read', { serverId, path, maxSize }, { fallbackMessage: FALLBACK });
}

export async function writeSftpFile(serverId, path, content) {
  return post('/api/server/sftp/write', { serverId, path, content }, { fallbackMessage: FALLBACK });
}

export async function createSftpDirectory(serverId, path) {
  return post('/api/server/sftp/mkdir', { serverId, path }, { fallbackMessage: FALLBACK });
}

export async function renameSftpPath(serverId, oldPath, newPath) {
  return post('/api/server/sftp/rename', { serverId, oldPath, newPath }, { fallbackMessage: FALLBACK });
}

export async function deleteSftpPath(serverId, path, isDirectory = false, recursive = false) {
  const endpoint = isDirectory ? '/api/server/sftp/rmdir' : '/api/server/sftp/delete';
  return post(endpoint, { serverId, path, recursive }, { fallbackMessage: FALLBACK });
}

export async function chmodSftpPath(serverId, path, mode) {
  return post('/api/server/sftp/chmod', { serverId, path, mode }, { fallbackMessage: FALLBACK });
}

export async function uploadSftpFile(serverId, path, file, relativePath) {
  const formData = new FormData();
  formData.append('serverId', serverId);
  formData.append('path', path);
  formData.append('file', file);
  if (relativePath) formData.append('relativePath', relativePath);

  return post('/api/server/sftp/upload', formData, { fallbackMessage: FALLBACK });
}

export function buildSftpDownloadUrl(serverId, path) {
  return `/api/server/sftp/download/${encodeURIComponent(serverId)}?path=${encodeURIComponent(path)}`;
}
