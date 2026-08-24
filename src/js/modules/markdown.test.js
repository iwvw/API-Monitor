import { describe, expect, it, vi } from 'vitest';
import { ALLOWED_URI_REGEXP, renderMarkdown } from './markdown.js';

vi.mock('dompurify', () => ({
  default: {
    sanitize: (html) => (typeof html === 'string' ? html.replace(/<script[\s\S]*?<\/script>/gi, '') : html),
  },
}));

describe('ALLOWED_URI_REGEXP', () => {
  it('compiles and is a RegExp', () => {
    expect(ALLOWED_URI_REGEXP).toBeInstanceOf(RegExp);
  });

  it('允许安全协议与相对路径/锚点', () => {
    const allowed = [
      'http://example.com/a?b=1#c',
      'https://example.com',
      'ftp://mirror.example.com',
      'mailto:admin@example.com',
      'tel:+8613800138000',
      '/relative/path',
      './relative',
      '#anchor',
    ];
    for (const uri of allowed) {
      expect(ALLOWED_URI_REGEXP.test(uri), uri).toBe(true);
    }
  });

  it('允许 data:image/ 位图 data URL（Base64 图片预览）', () => {
    const allowed = [
      'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
      'data:image/jpeg,AAAA',
      'data:image/gif;base64,R0lGODlh',
    ];
    for (const uri of allowed) {
      expect(ALLOWED_URI_REGEXP.test(uri), uri).toBe(true);
    }
  });

  it('拦截 javascript:/vbscript:/file: 与不可信 data URL', () => {
    const blocked = [
      'javascript:alert(1)',
      'JavaScript:alert(1)',
      'vbscript:msgbox(1)',
      'file:///etc/passwd',
      'data:text/html,<script>alert(1)</script>',
      'data:image/svg+xml,<svg onload=alert(1)>',
      'data:application/javascript,alert(1)',
    ];
    for (const uri of blocked) {
      expect(ALLOWED_URI_REGEXP.test(uri), uri).toBe(false);
    }
  });
});

describe('renderMarkdown', () => {
  it('returns empty for nullish input', () => {
    expect(renderMarkdown(null)).toBe('');
    expect(renderMarkdown(undefined)).toBe('');
  });

  it('renders plain markdown', () => {
    expect(renderMarkdown('hello **bold**')).toContain('<strong>bold</strong>');
  });

  it('joins multi-modal arrays', () => {
    const out = renderMarkdown(['a', { type: 'text', text: 'b' }]);
    expect(out).toContain('ab');
  });

  it('joins text parts with fallback content keys', () => {
    const out = renderMarkdown([{ type: 'text', content: 'x' }, ' y']);
    expect(out).toContain('x y');
  });

  it('skips falsy array parts', () => {
    expect(renderMarkdown(['a', null, undefined, '', 'b'])).toContain('ab');
  });

  it('wraps text parts with thought in a reasoning details block', () => {
    const out = renderMarkdown([{ type: 'text', text: 't', thought: 'r' }]);
    expect(out).toContain('reasoning-details');
    expect(out).toContain('思考过程');
    expect(out).toContain('<p>t</p>');
  });

  it('renders image_url parts as an inline image container', () => {
    const out = renderMarkdown([{ type: 'image_url', image_url: { url: 'https://x/y.png' } }]);
    expect(out).toContain('msg-image-container');
    expect(out).toContain('img-preview-trigger');
    expect(out).toContain('msg-inline-image');
    expect(out).toContain('https://x/y.png');
  });

  it('falls back to a JSON string for unknown object parts', () => {
    const out = renderMarkdown([{ type: 'tool_call', id: 'c1' }]);
    expect(out).toContain('tool_call');
    expect(out).toContain('c1');
  });

  it('wraps single objects in a json code fence', () => {
    const out = renderMarkdown({ a: 1 });
    expect(out).toContain('language-json');
    expect(out).toContain('&quot;a&quot;: 1');
  });

  it('escapes the literal [object Object] string', () => {
    const out = renderMarkdown('[object Object]');
    expect(out).toContain('language-json');
    expect(out).toContain('&quot;[object Object]&quot;');
  });

  it('restores block math placeholders', () => {
    const out = renderMarkdown('$$x^2$$');
    expect(out).toContain('math-block');
    expect(out).toContain('katex');
  });

  it('restores inline math placeholders', () => {
    const out = renderMarkdown('\\(a+b\\)');
    expect(out).toContain('math-inline');
    expect(out).toContain('katex');
  });

  it('converts thinking tags into reasoning details', () => {
    const out = renderMarkdown('<thinking>well</thinking>');
    expect(out).toContain('reasoning-details');
    expect(out).toContain('思考过程');
    expect(out).toContain('reasoning-content-inner');
  });

  it('strips script tags', () => {
    const out = renderMarkdown('<script>alert(1)</script>');
    expect(out).not.toContain('<script');
    expect(out).not.toContain('alert(1)');
  });
});