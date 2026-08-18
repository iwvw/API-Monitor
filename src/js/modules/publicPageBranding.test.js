import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

import {
  PUBLIC_PAGE_ICON_CONFIG_KEY,
  getPublicPageUploadedIconUrl,
  getPublicPageIconId,
  withPublicPageIconId,
  renderPublicPageDefaultIcon,
  getPublicPageFaviconHref,
  swapPublicPageFavicon,
  listPublicPageIcons,
  uploadPublicPageIcon,
  deletePublicPageIcon,
} from './publicPageBranding.js';

const makeDocumentStub = () => {
  const links = [];
  const head = {
    querySelector(selector) {
      return links.find((link) => link._selector === selector) || null;
    },
    appendChild(node) {
      node._selector = `link[rel="${node._rel}"]`;
      links.push(node);
    },
  };
  const createElement = (tag) => {
    if (tag !== 'link') throw new Error(`unexpected tag: ${tag}`);
    const attrs = {};
    return {
      _rel: '',
      _href: '',
      _selector: '',
      set rel(value) {
        this._rel = String(value);
      },
      get rel() {
        return this._rel;
      },
      set href(value) {
        this._href = String(value);
        attrs.href = String(value);
      },
      get href() {
        return this._href;
      },
      getAttribute(name) {
        return name in attrs ? attrs[name] : null;
      },
      removeAttribute(name) {
        delete attrs[name];
      },
    };
  };
  return { head, links, createElement, document: { head, createElement } };
};

const okResponse = (payload, ok = true) => ({ ok, json: async () => payload });

describe('constants', () => {
  it('exposes the icon config key', () => {
    expect(PUBLIC_PAGE_ICON_CONFIG_KEY).toBe('publicIconId');
  });
});

describe('withPublicPageIconId', () => {
  it('stores a trimmed icon id', () => {
    expect(withPublicPageIconId({}, 'abc')).toEqual({ publicIconId: 'abc' });
    expect(withPublicPageIconId({ other: 1 }, '  abc  ')).toEqual({ other: 1, publicIconId: 'abc' });
  });

  it('removes the key for empty ids', () => {
    expect(withPublicPageIconId({ publicIconId: 'old' }, '')).toEqual({});
    expect(withPublicPageIconId({ publicIconId: 'old' }, '   ')).toEqual({});
    expect(withPublicPageIconId({ publicIconId: 'old' }, undefined)).toEqual({});
  });

  it('tolerates non-object configs', () => {
    expect(withPublicPageIconId(null, '')).toEqual({});
    expect(withPublicPageIconId(undefined, '')).toEqual({});
    expect(withPublicPageIconId('not-an-object', '')).toEqual({});
    expect(withPublicPageIconId(null, 'abc')).toEqual({ publicIconId: 'abc' });
  });

  it('does not mutate the input config', () => {
    const config = { publicIconId: 'old' };
    withPublicPageIconId(config, 'new');
    expect(config.publicIconId).toBe('old');
  });
});

describe('getPublicPageIconId', () => {
  it('normalizes the stored icon id', () => {
    expect(getPublicPageIconId({})).toBe('');
    expect(getPublicPageIconId({ publicIconId: '  x  ' })).toBe('x');
  });
});

describe('getPublicPageUploadedIconUrl', () => {
  it('returns empty for empty ids', () => {
    expect(getPublicPageUploadedIconUrl()).toBe('');
    expect(getPublicPageUploadedIconUrl('')).toBe('');
    expect(getPublicPageUploadedIconUrl(null)).toBe('');
  });

  it('builds an encoded url for real ids', () => {
    expect(getPublicPageUploadedIconUrl('a b')).toBe('/site-brand-icons/a%20b');
  });
});

describe('renderPublicPageDefaultIcon', () => {
  it('returns a React element for known and unknown kinds', () => {
    expect(renderPublicPageDefaultIcon('uptime').type).toBeDefined();
    expect(renderPublicPageDefaultIcon('server').type).toBeDefined();
    expect(renderPublicPageDefaultIcon('github').type).toBeDefined();
  });
});

describe('getPublicPageFaviconHref', () => {
  it('prefers the uploaded icon', () => {
    expect(getPublicPageFaviconHref('uptime', { publicIconId: 'x' })).toBe('/site-brand-icons/x');
  });

  it('falls back to the uptime data url', () => {
    const href = getPublicPageFaviconHref('uptime', {});
    expect(href.startsWith('data:image/svg+xml,')).toBe(true);
    expect(href).toContain('%23f48120');
  });

  it('uses the server default for the server page', () => {
    const href = getPublicPageFaviconHref('server', {});
    expect(href.startsWith('data:image/svg+xml,')).toBe(true);
    expect(href).toContain('%23f48120');
  });

  it('uses the github default for the github page', () => {
    const href = getPublicPageFaviconHref('github', {});
    expect(href.startsWith('data:image/svg+xml,')).toBe(true);
    expect(href).toContain('0%200%201230%201200');
  });

  it('falls back to uptime for unknown kinds', () => {
    expect(getPublicPageFaviconHref('wat')).toBe(getPublicPageFaviconHref('uptime', {}));
  });
});

describe('swapPublicPageFavicon', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('returns a no-op without a document', () => {
    const restore = swapPublicPageFavicon('/x.ico');
    expect(typeof restore).toBe('function');
    expect(() => restore()).not.toThrow();
  });

  it('returns a no-op for falsy hrefs', () => {
    const stub = makeDocumentStub();
    vi.stubGlobal('document', stub.document);
    expect(swapPublicPageFavicon('')).toEqual(expect.any(Function));
    expect(swapPublicPageFavicon(null)).toEqual(expect.any(Function));
    expect(stub.links).toHaveLength(0);
  });

  it('swaps both links and restores previous hrefs', () => {
    const stub = makeDocumentStub();
    vi.stubGlobal('document', stub.document);

    const restoreA = swapPublicPageFavicon('/a.ico');
    expect(stub.links).toHaveLength(2);
    expect(stub.links.every((link) => link.href === '/a.ico')).toBe(true);
    expect(typeof restoreA).toBe('function');

    stub.links.forEach((link) => {
      link.href = '/manual.ico';
    });

    const restoreB = swapPublicPageFavicon('/b.ico');
    expect(stub.links.every((link) => link.href === '/b.ico')).toBe(true);

    restoreB();
    expect(stub.links.every((link) => link.href === '/manual.ico')).toBe(true);
  });

  it('restore removes the href attribute when there was none before', () => {
    const stub = makeDocumentStub();
    vi.stubGlobal('document', stub.document);

    const restore = swapPublicPageFavicon('/a.ico');
    restore();
    expect(stub.links.every((link) => link.getAttribute('href') === null
      || link.href !== '/a.ico')).toBe(true);
    expect(stub.links.every((link) => link.getAttribute('href') === null)).toBe(true);
  });
});

describe('listPublicPageIcons', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('fetches with no-store and injects publicUrl', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [{ id: 'i1', name: 'n' }] }));
    const result = await listPublicPageIcons();
    expect(fetchMock).toHaveBeenCalledWith('/api/settings/site-brand/icons', { cache: 'no-store' });
    expect(result).toEqual([{ id: 'i1', name: 'n', publicUrl: '/site-brand-icons/i1' }]);
  });

  it('returns an empty array when data is not an array', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: null }));
    expect(await listPublicPageIcons()).toEqual([]);
  });

  it('throws the server error on failure responses', async () => {
    fetchMock.mockResolvedValue(okResponse({ error: '加载失败' }, false));
    await expect(listPublicPageIcons()).rejects.toThrow('加载失败');
  });

  it('throws when success is false', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: false, error: '业务错误' }));
    await expect(listPublicPageIcons()).rejects.toThrow('业务错误');
  });

  it('falls back to a default message', async () => {
    fetchMock.mockResolvedValue(okResponse({}, false));
    await expect(listPublicPageIcons()).rejects.toThrow('加载图标失败');
  });
});

describe('uploadPublicPageIcon', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('posts a FormData with file and name and injects publicUrl', async () => {
    const file = new Blob(['png'], { type: 'image/png' });
    file.name = 'logo.png';
    fetchMock.mockResolvedValue(okResponse({ success: true, data: { id: 'u1' } }));
    const result = await uploadPublicPageIcon(file);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/settings/site-brand/icons');
    expect(options.method).toBe('POST');
    expect(options.body).toBeInstanceOf(FormData);
    expect(options.body.get('file')).toBeInstanceOf(Blob);
    expect(await options.body.get('file').text()).toBe('png');
    expect(options.body.get('name')).toBe('logo.png');
    expect(result).toEqual({ id: 'u1', publicUrl: '/site-brand-icons/u1' });
  });

  it('uses the raw result when data is absent', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, id: 'u2' }));
    expect(await uploadPublicPageIcon({ name: 'a.png' })).toEqual({ success: true, id: 'u2', publicUrl: '/site-brand-icons/u2' });
  });

  it('throws on failure responses', async () => {
    fetchMock.mockResolvedValue(okResponse({ error: '上传失败' }, false));
    await expect(uploadPublicPageIcon({ name: 'a.png' })).rejects.toThrow('上传失败');
  });

  it('rejects when success is false without message', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: false }));
    await expect(uploadPublicPageIcon({ name: 'a.png' })).rejects.toThrow('上传图标失败');
  });
});

describe('deletePublicPageIcon', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('skips empty ids without fetching', async () => {
    expect(await deletePublicPageIcon('')).toBeUndefined();
    expect(await deletePublicPageIcon(null)).toBeUndefined();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('deletes with an encoded id', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await deletePublicPageIcon('a b');
    expect(fetchMock).toHaveBeenCalledWith('/api/settings/site-brand/icons/a%20b', { method: 'DELETE' });
  });

  it('throws the server error on failure', async () => {
    fetchMock.mockResolvedValue(okResponse({ error: '删除失败' }, false));
    await expect(deletePublicPageIcon('x')).rejects.toThrow('删除失败');
  });

  it('falls back to a default message', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: false }));
    await expect(deletePublicPageIcon('x')).rejects.toThrow('删除图标失败');
  });
});