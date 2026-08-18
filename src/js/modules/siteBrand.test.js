import { describe, expect, it, vi, afterEach } from 'vitest';

import {
  getSiteBrandUrl,
  getDefaultSiteBrandPreviewUrl,
  applySiteBrandFaviconHref,
  applySiteBrandFavicon,
} from './siteBrand.js';

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

describe('getSiteBrandUrl', () => {
  it('falls back to default for empty input', () => {
    expect(getSiteBrandUrl()).toBe('/logo.svg?v=default');
    expect(getSiteBrandUrl('')).toBe('/logo.svg?v=default');
    expect(getSiteBrandUrl(null)).toBe('/logo.svg?v=default');
    expect(getSiteBrandUrl('default')).toBe('/logo.svg?v=default');
  });

  it('trims whitespace before falling back', () => {
    expect(getSiteBrandUrl('   ')).toBe('/logo.svg?v=default');
  });

  it('encodes custom ids', () => {
    expect(getSiteBrandUrl('my icon')).toBe('/logo.svg?v=my%20icon');
    expect(getSiteBrandUrl('a/b?c')).toBe('/logo.svg?v=a%2Fb%3Fc');
  });
});

describe('getDefaultSiteBrandPreviewUrl', () => {
  it('returns the fixed preview url', () => {
    expect(getDefaultSiteBrandPreviewUrl()).toBe('/logo-default.svg?v=default');
  });
});

describe('applySiteBrandFaviconHref', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('is a no-op when document is unavailable', () => {
    expect(() => applySiteBrandFaviconHref('/favicon.ico')).not.toThrow();
  });

  it('creates icon and shortcut icon links and reuses them', () => {
    const stub = makeDocumentStub();
    vi.stubGlobal('document', stub.document);

    applySiteBrandFaviconHref('/a.ico');
    expect(stub.links).toHaveLength(2);
    expect(stub.links.map((link) => link.rel)).toEqual(['icon', 'shortcut icon']);
    expect(stub.links.every((link) => link.href === '/a.ico')).toBe(true);

    applySiteBrandFaviconHref('/b.ico');
    expect(stub.links).toHaveLength(2);
    expect(stub.links.every((link) => link.href === '/b.ico')).toBe(true);
  });
});

describe('applySiteBrandFavicon', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('is a no-op when document is unavailable', () => {
    expect(() => applySiteBrandFavicon('default')).not.toThrow();
  });

  it('applies the generated url to both link rels', () => {
    const stub = makeDocumentStub();
    vi.stubGlobal('document', stub.document);

    applySiteBrandFavicon('my icon');
    expect(stub.links).toHaveLength(2);
    expect(stub.links.every((link) => link.href === '/logo.svg?v=my%20icon')).toBe(true);
  });

  it('uses the default id when nothing is given', () => {
    const stub = makeDocumentStub();
    vi.stubGlobal('document', stub.document);

    applySiteBrandFavicon();
    expect(stub.links.every((link) => link.href === '/logo.svg?v=default')).toBe(true);
  });
});