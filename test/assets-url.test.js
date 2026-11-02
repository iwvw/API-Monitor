import { describe, expect, it } from 'vitest';
import { assetLinkUrl, emptyForm, formToPayload, normalizeAssetUrl, validateForm } from '../src/js/pages/assets/utils.js';

describe('normalizeAssetUrl', () => {
  it('无协议时补 https://', () => {
    expect(normalizeAssetUrl('example.com')).toBe('https://example.com');
    expect(normalizeAssetUrl('www.example.com/path')).toBe('https://www.example.com/path');
  });

  it('保留已有的 http/https 协议', () => {
    expect(normalizeAssetUrl('http://example.com')).toBe('http://example.com');
    expect(normalizeAssetUrl('https://example.com/a?b=1')).toBe('https://example.com/a?b=1');
  });

  it('空白输入视为无链接（返回空串），不报错', () => {
    expect(normalizeAssetUrl('')).toBe('');
    expect(normalizeAssetUrl('   ')).toBe('');
    expect(normalizeAssetUrl(null)).toBe('');
    expect(normalizeAssetUrl(undefined)).toBe('');
  });

  it('非 http/https 协议判为非法（返回 null），避免 javascript: 之类的注入', () => {
    expect(normalizeAssetUrl('javascript:alert(1)')).toBeNull();
    expect(normalizeAssetUrl('JavaScript:alert(1)')).toBeNull();
    expect(normalizeAssetUrl('ftp://example.com')).toBeNull();
    expect(normalizeAssetUrl('mailto:a@b.com')).toBeNull();
    expect(normalizeAssetUrl('data:text/html,x')).toBeNull();
    expect(normalizeAssetUrl('file:///etc/passwd')).toBeNull();
  });

  it('放行「主机名:端口」：scheme 语法与 host:port 同形，不能一棍子判非法', () => {
    // 冒号后是纯数字时按端口解释（nas.local:8080 在 RFC 3986 下也匹配 scheme 语法）
    expect(normalizeAssetUrl('nas.local:8080')).toBe('https://nas.local:8080');
    expect(normalizeAssetUrl('server:3000/admin')).toBe('https://server:3000/admin');
    expect(normalizeAssetUrl('192.168.1.1:8080')).toBe('https://192.168.1.1:8080');
  });

  it('无法解析成主机的输入判为非法', () => {
    expect(normalizeAssetUrl('http://')).toBeNull();
    expect(normalizeAssetUrl('https://')).toBeNull();
  });
});

describe('assetLinkUrl', () => {
  it('从 metadata.url 读取并去空格', () => {
    expect(assetLinkUrl({ metadata: { url: '  https://a.com  ' } })).toBe('https://a.com');
  });

  it('metadata 缺失或 url 非字符串时返回空串', () => {
    expect(assetLinkUrl({})).toBe('');
    expect(assetLinkUrl(null)).toBe('');
    expect(assetLinkUrl({ metadata: { url: 123 } })).toBe('');
  });
});

describe('formToPayload 的 metadata 合并', () => {
  const baseForm = { ...emptyForm('virtual'), name: '测试资产', asset_type: 'domain', url: 'example.com' };

  it('写入规范化后的 url', () => {
    const payload = formToPayload(baseForm, null);
    expect(payload.metadata.url).toBe('https://example.com');
  });

  it('保留既有 metadata 中的其他键（不能只回传 url 就覆盖掉）', () => {
    const existing = { metadata: { owner_team: 'infra', url: 'https://old.example.com' } };
    const payload = formToPayload(baseForm, existing);
    expect(payload.metadata.owner_team).toBe('infra');
    expect(payload.metadata.url).toBe('https://example.com');
  });

  it('清空链接时删除 url 键，但保留其他 metadata', () => {
    const existing = { metadata: { owner_team: 'infra', url: 'https://old.example.com' } };
    const payload = formToPayload({ ...baseForm, url: '' }, existing);
    expect('url' in payload.metadata).toBe(false);
    expect(payload.metadata.owner_team).toBe('infra');
  });

  it('没有既有 metadata 且无链接时给出空对象', () => {
    const payload = formToPayload({ ...baseForm, url: '' }, null);
    expect(payload.metadata).toEqual({});
  });

  it('链接非法时保留原值，不把已有链接静默抹掉', () => {
    const existing = { metadata: { url: 'https://old.example.com' } };
    const payload = formToPayload({ ...baseForm, url: 'javascript:alert(1)' }, existing);
    expect(payload.metadata.url).toBe('https://old.example.com');
  });
});

describe('validateForm 的链接校验', () => {
  const valid = { ...emptyForm('virtual'), name: 'x', asset_type: 'domain' };

  it('空链接通过', () => {
    expect(validateForm({ ...valid, url: '' })).toBe('');
  });

  it('合法链接通过', () => {
    expect(validateForm({ ...valid, url: 'https://example.com' })).toBe('');
    expect(validateForm({ ...valid, url: 'example.com' })).toBe('');
  });

  it('非法链接被拦截', () => {
    expect(validateForm({ ...valid, url: 'javascript:alert(1)' })).not.toBe('');
  });
});
