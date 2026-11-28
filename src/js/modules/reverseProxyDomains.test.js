import { describe, expect, it } from 'vitest';
import { getDockerContainerHostPorts, matchContainerReverseProxyDomains } from './reverseProxyDomains.js';

const targets = [
  {
    upstreamPort: 2260,
    domains: [{ domain: 'resin.dsukhub.com', url: 'https://resin.dsukhub.com', siteId: 16, status: 'Running', domains: ['resin.dsukhub.com'] }],
  },
  {
    upstreamPort: 5244,
    domains: [
      { domain: 'ol.dsuk.top', url: 'https://ol.dsuk.top', siteId: 15, status: 'Running', domains: ['ol.dsuk.top', 'files.example.com'] },
      { domain: 'salen.eu.org', url: 'https://salen.eu.org', siteId: 9, status: 'Running', domains: ['salen.eu.org'] },
    ],
  },
];

describe('getDockerContainerHostPorts', () => {
  it('extracts published host ports from agent port strings', () => {
    expect(getDockerContainerHostPorts({ ports: '2260:2260, 5246:5246, 5245' })).toEqual([2260, 5246]);
  });

  it('ignores container-only ports', () => {
    expect(getDockerContainerHostPorts({ ports: '80, 443' })).toEqual([]);
  });

  it('handles object-array and inspect-map shapes', () => {
    expect(getDockerContainerHostPorts({ ports: [{ PrivatePort: 3000, PublicPort: 3010 }] })).toEqual([3010]);
    expect(getDockerContainerHostPorts({ ports: { '8080/tcp': [{ HostPort: '18088' }] } })).toEqual([18088]);
  });

  it('returns empty for missing data', () => {
    expect(getDockerContainerHostPorts()).toEqual([]);
    expect(getDockerContainerHostPorts({})).toEqual([]);
    expect(getDockerContainerHostPorts(null)).toEqual([]);
  });
});

describe('matchContainerReverseProxyDomains', () => {
  it('matches by host port only', () => {
    const matched = matchContainerReverseProxyDomains({ ports: '2260:2260' }, targets);
    expect(matched).toHaveLength(1);
    expect(matched[0].url).toBe('https://resin.dsukhub.com');
    expect(matched[0].upstreamPort).toBe(2260);
  });

  it('keeps every site sharing one upstream port, sorted by domain', () => {
    const matched = matchContainerReverseProxyDomains({ ports: '5244:5244, 5246:5246' }, targets);
    expect(matched.map(item => item.domain)).toEqual(['ol.dsuk.top', 'salen.eu.org']);
    expect(matched[0].aliases).toEqual(['ol.dsuk.top', 'files.example.com']);
  });

  it('does not guess when the container publishes no host ports', () => {
    expect(matchContainerReverseProxyDomains({ ports: '80' }, targets)).toEqual([]);
    expect(matchContainerReverseProxyDomains({ ports: '9999:9999' }, targets)).toEqual([]);
  });

  it('dedupes identical urls and tolerates bad targets', () => {
    const dup = [
      { upstreamPort: 8080, domains: [{ domain: 'a.test', url: 'https://a.test' }] },
      { upstreamPort: 8080, domains: [{ domain: 'a.test', url: 'https://a.test' }] },
    ];
    expect(matchContainerReverseProxyDomains({ ports: '8080:8080' }, dup)).toHaveLength(1);
    expect(matchContainerReverseProxyDomains({ ports: '8080:8080' }, null)).toEqual([]);
    expect(matchContainerReverseProxyDomains({ ports: '8080:8080' }, [null, {}])).toEqual([]);
  });
});
