import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

import {
  fetchCommandSnippets,
  createCommandSnippet,
  updateCommandSnippet,
  deleteCommandSnippet,
  previewCommand,
  recordCommandHistory,
  fetchCommandHistory,
} from './server-commands.js';

const okResponse = (payload, ok = true) => ({ ok, json: async () => payload });

describe('fetchCommandSnippets', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('omits undefined, null and empty filters', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [] }));
    await fetchCommandSnippets({ os: undefined, arch: null, group: '', kind: 'qr' });
    expect(fetchMock).toHaveBeenCalledWith('/api/server/snippets?kind=qr');
  });

  it('keeps falsy-but-valid filter values', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [] }));
    await fetchCommandSnippets({ kind: 'qr', os: 'linux' });
    expect(fetchMock).toHaveBeenCalledWith('/api/server/snippets?kind=qr&os=linux');
  });

  it('resolves with the response payload', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [{ id: 1 }] }));
    expect(await fetchCommandSnippets({})).toEqual({ success: true, data: [{ id: 1 }] });
  });
});

describe('createCommandSnippet', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('posts the payload as JSON', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: { id: 1 } }));
    await createCommandSnippet({ title: 't', command: 'ls' });
    expect(fetchMock).toHaveBeenCalledWith('/api/server/snippets', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{"title":"t","command":"ls"}',
    });
  });
});

describe('updateCommandSnippet', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('puts the payload to the snippet url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await updateCommandSnippet('snippet-1', { title: 't' });
    expect(fetchMock).toHaveBeenCalledWith('/api/server/snippets/snippet-1', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: '{"title":"t"}',
    });
  });
});

describe('deleteCommandSnippet', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('deletes without a body', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await deleteCommandSnippet('snippet-1');
    expect(fetchMock).toHaveBeenCalledWith('/api/server/snippets/snippet-1', { method: 'DELETE' });
  });
});

describe('previewCommand', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('posts to the preview endpoint', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: { preview: 'ls -la' } }));
    await previewCommand({ command: 'ls' });
    expect(fetchMock).toHaveBeenCalledWith('/api/server/snippets/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{"command":"ls"}',
    });
  });
});

describe('recordCommandHistory', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('posts to the history endpoint', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await recordCommandHistory({ line: 'ls', serverId: 'srv' });
    expect(fetchMock).toHaveBeenCalledWith('/api/server/snippets/history', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{"line":"ls","serverId":"srv"}',
    });
  });
});

describe('fetchCommandHistory', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('filters out empty history filters', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [] }));
    await fetchCommandHistory({ os: undefined, limit: null, group: '', serverId: 'srv' });
    expect(fetchMock).toHaveBeenCalledWith('/api/server/snippets/history?serverId=srv');
  });
});

describe('parseResponse', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('throws the server error on non-ok responses', async () => {
    fetchMock.mockResolvedValue(okResponse({ error: '权限不足' }, false));
    await expect(fetchCommandSnippets({})).rejects.toThrow('权限不足');
  });

  it('throws the default message when success is false', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: false }));
    await expect(fetchCommandSnippets({})).rejects.toThrow('快速命令请求失败');
  });

  it('keeps errors free of code/details fields', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: false }));
    const err = await fetchCommandSnippets({}).catch((e) => e);
    expect(err.code).toBeUndefined();
    expect(err.details).toBeUndefined();
  });

  it('tolerates JSON parse failures', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => { throw new SyntaxError('bad json'); } });
    expect(await fetchCommandSnippets({})).toEqual({});
  });
});