import { describe, expect, it } from 'vitest';
import {
  DEFAULT_ONEPANEL_BASE_URL,
  buildOnepanelPayload,
  describeOnepanelServer,
  getAvailableOnepanelServers,
  validateOnepanelForm,
} from './onepanelConfig.js';

describe('getAvailableOnepanelServers', () => {
  const servers = [
    { id: 'a', name: 'ARM', host: '1.1.1.1' },
    { id: 'b', name: '香港', host: '2.2.2.2' },
    { id: 'c', name: '伦敦', host: '3.3.3.3' },
  ];

  it('excludes already configured servers', () => {
    const out = getAvailableOnepanelServers(servers, [{ serverId: 'a' }, { serverId: 'c' }]);
    expect(out.map(s => s.id)).toEqual(['b']);
  });

  it('treats numeric and string ids as equal', () => {
    const out = getAvailableOnepanelServers([{ id: 42 }], [{ serverId: 42 }]);
    expect(out).toEqual([]);
  });

  it('returns everything when nothing configured', () => {
    expect(getAvailableOnepanelServers(servers, [])).toHaveLength(3);
  });

  it('tolerates missing input', () => {
    expect(getAvailableOnepanelServers()).toEqual([]);
    expect(getAvailableOnepanelServers(servers, null)).toHaveLength(3);
    expect(getAvailableOnepanelServers([{ name: 'no-id' }], [])).toEqual([]);
  });

  it('keeps only hosts where 1Panel was detected', () => {
    const out = getAvailableOnepanelServers(servers, [], new Set(['a', 'c']));
    expect(out.map(s => s.id)).toEqual(['a', 'c']);
  });

  it('returns nothing when the probe found no 1Panel host', () => {
    // 探测完成但无一命中时不能回落到「不过滤」，否则未装 1Panel 的主机会被列出。
    expect(getAvailableOnepanelServers(servers, [], new Set())).toEqual([]);
  });

  it('applies both install and configured filters', () => {
    const out = getAvailableOnepanelServers(servers, [{ serverId: 'a' }], new Set(['a', 'b']));
    expect(out.map(s => s.id)).toEqual(['b']);
  });
});

describe('validateOnepanelForm', () => {
  it('requires serverId and apiKey', () => {
    expect(validateOnepanelForm({})).toBe('请选择主机');
    expect(validateOnepanelForm({ serverId: 'a' })).toBe('请填写 1Panel API Key');
    expect(validateOnepanelForm({ serverId: '  ', apiKey: 'k' })).toBe('请选择主机');
    expect(validateOnepanelForm({ serverId: 'a', apiKey: '  ' })).toBe('请填写 1Panel API Key');
  });

  it('passes when both present', () => {
    expect(validateOnepanelForm({ serverId: 'a', apiKey: 'k' })).toBe('');
  });
});

describe('buildOnepanelPayload', () => {
  it('trims values and falls back to the default base url', () => {
    expect(buildOnepanelPayload({ serverId: ' a ', apiKey: ' k ', baseUrl: '' })).toEqual({
      serverId: 'a',
      apiKey: 'k',
      baseUrl: DEFAULT_ONEPANEL_BASE_URL,
    });
  });

  it('keeps a custom base url', () => {
    expect(buildOnepanelPayload({ serverId: 'a', apiKey: 'k', baseUrl: ' http://10.0.0.5:8888 ' }).baseUrl)
      .toBe('http://10.0.0.5:8888');
  });
});

describe('describeOnepanelServer', () => {
  it('formats name with host', () => {
    expect(describeOnepanelServer({ name: 'ARM', host: '1.1.1.1' })).toBe('ARM（1.1.1.1）');
  });

  it('falls back progressively', () => {
    expect(describeOnepanelServer({ name: 'ARM' })).toBe('ARM');
    expect(describeOnepanelServer({ host: '1.1.1.1' })).toBe('1.1.1.1');
    expect(describeOnepanelServer({ id: 'x' })).toBe('x');
    expect(describeOnepanelServer(null)).toBe('未知主机');
  });
});
