import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

import {
  listSftpDirectory,
  readSftpFile,
  writeSftpFile,
  createSftpDirectory,
  renameSftpPath,
  deleteSftpPath,
  chmodSftpPath,
  uploadSftpFile,
  buildSftpDownloadUrl,
} from './server-sftp.js';

const okResponse = (payload, ok = true) => ({ ok, json: async () => payload });

const jsonResponse = () => ({ ok: true, json: async () => ({ success: true, data: 'ok' }) });

describe('server-sftp fetches', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('listSftpDirectory posts serverId and path', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: ['a', 'b'] }));
    await listSftpDirectory('srv-1', '.');
    expect(fetchMock).toHaveBeenCalledWith('/api/server/sftp/list', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{"serverId":"srv-1","path":"."}',
    });
  });

  it('readSftpFile posts serverId, path and maxSize', async () => {
    fetchMock.mockResolvedValue(jsonResponse());
    await readSftpFile('srv-1', '/etc/a', 1024);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/server/sftp/read');
    expect(fetchMock.mock.calls[0][1].body).toBe('{"serverId":"srv-1","path":"/etc/a","maxSize":1024}');
  });

  it('writeSftpFile posts serverId, path and content', async () => {
    fetchMock.mockResolvedValue(jsonResponse());
    await writeSftpFile('srv-1', '/etc/a', 'hello');
    expect(fetchMock.mock.calls[0][0]).toBe('/api/server/sftp/write');
    expect(fetchMock.mock.calls[0][1].body).toBe('{"serverId":"srv-1","path":"/etc/a","content":"hello"}');
  });

  it('createSftpDirectory posts serverId and path', async () => {
    fetchMock.mockResolvedValue(jsonResponse());
    await createSftpDirectory('srv-1', '/tmp/new');
    expect(fetchMock.mock.calls[0][0]).toBe('/api/server/sftp/mkdir');
    expect(fetchMock.mock.calls[0][1].body).toBe('{"serverId":"srv-1","path":"/tmp/new"}');
  });

  it('renameSftpPath posts oldPath and newPath', async () => {
    fetchMock.mockResolvedValue(jsonResponse());
    await renameSftpPath('srv-1', '/old', '/new');
    expect(fetchMock.mock.calls[0][0]).toBe('/api/server/sftp/rename');
    expect(fetchMock.mock.calls[0][1].body).toBe('{"serverId":"srv-1","oldPath":"/old","newPath":"/new"}');
  });

  it('deleteSftpPath uses /delete for files with recursive flag', async () => {
    fetchMock.mockResolvedValue(jsonResponse());
    await deleteSftpPath('srv-1', '/f.txt');
    expect(fetchMock.mock.calls[0][0]).toBe('/api/server/sftp/delete');
    expect(fetchMock.mock.calls[0][1].body).toBe('{"serverId":"srv-1","path":"/f.txt","recursive":false}');
  });

  it('deleteSftpPath uses /rmdir for directories', async () => {
    fetchMock.mockResolvedValue(jsonResponse());
    await deleteSftpPath('srv-1', '/d', true, false);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/server/sftp/rmdir');
    expect(fetchMock.mock.calls[0][1].body).toBe('{"serverId":"srv-1","path":"/d","recursive":false}');
  });

  it('deleteSftpPath forwards recursive for directories', async () => {
    fetchMock.mockResolvedValue(jsonResponse());
    await deleteSftpPath('srv-1', '/d', true, true);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/server/sftp/rmdir');
    expect(fetchMock.mock.calls[0][1].body).toBe('{"serverId":"srv-1","path":"/d","recursive":true}');
  });

  it('chmodSftpPath posts serverId, path and mode', async () => {
    fetchMock.mockResolvedValue(jsonResponse());
    await chmodSftpPath('srv-1', '/f', '644');
    expect(fetchMock.mock.calls[0][0]).toBe('/api/server/sftp/chmod');
    expect(fetchMock.mock.calls[0][1].body).toBe('{"serverId":"srv-1","path":"/f","mode":"644"}');
  });

  it('uploadSftpFile builds a FormData body', async () => {
    fetchMock.mockResolvedValue(jsonResponse());
    const file = new Blob(['content'], { type: 'text/plain' });
    file.name = 'a.txt';
    await uploadSftpFile('srv-1', '/up', file, 'sub/b.txt');
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/server/sftp/upload');
    expect(options.method).toBe('POST');
    expect(options.body).toBeInstanceOf(FormData);
    expect(options.headers).toBeUndefined();
    expect(options.body.get('serverId')).toBe('srv-1');
    expect(options.body.get('path')).toBe('/up');
    expect(options.body.get('file')).toBeInstanceOf(Blob);
    expect(await options.body.get('file').text()).toBe('content');
    expect(options.body.get('relativePath')).toBe('sub/b.txt');
  });

  it('uploadSftpFile omits relativePath when falsy', async () => {
    fetchMock.mockResolvedValue(jsonResponse());
    await uploadSftpFile('srv-1', '/up', { name: 'a.txt' }, undefined);
    expect(fetchMock.mock.calls[0][1].body.has('relativePath')).toBe(false);
  });

  it('parses errors with code and details', async () => {
    fetchMock.mockResolvedValue(okResponse({
      success: false,
      error: '权限不足',
      code: 'E403',
      details: { path: '/f' },
    }));
    const err = await listSftpDirectory('srv-1', '.').catch((e) => e);
    expect(err).toBeInstanceOf(Error);
    expect(err.message).toBe('权限不足');
    expect(err.code).toBe('E403');
    expect(err.details).toEqual({ path: '/f' });
  });

  it('throws plain errors without code or details', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: false, error: '失败' }));
    const err = await listSftpDirectory('srv-1', '.').catch((e) => e);
    expect(err.message).toBe('失败');
    expect(err.code).toBeUndefined();
    expect(err.details).toBeUndefined();
  });

  it('uses the server error message for non-ok responses', async () => {
    fetchMock.mockResolvedValue(okResponse({ error: '未授权' }, false));
    const err = await listSftpDirectory('srv-1', '.').catch((e) => e);
    expect(err.message).toBe('未授权');
    expect(err.code).toBeUndefined();
  });

  it('falls back when the response is not JSON', async () => {
    fetchMock.mockResolvedValue({ ok: false, json: async () => { throw new SyntaxError('bad json'); } });
    const err = await listSftpDirectory('srv-1', '.').catch((e) => e);
    expect(err.message).toBe('SFTP 请求失败');
  });
});

describe('buildSftpDownloadUrl', () => {
  it('double-encodes serverId and path', () => {
    const serverId = 'srv 1';
    expect(buildSftpDownloadUrl(serverId, '/a b/c')).toBe(
      `/api/server/sftp/download/${encodeURIComponent(serverId)}?path=%2Fa%20b%2Fc`,
    );
  });
});