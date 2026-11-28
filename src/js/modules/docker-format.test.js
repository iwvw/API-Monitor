import { describe, expect, it } from 'vitest';
import { buildDockerContainerPortUrl, formatDockerContainerPorts, getDockerContainerPortLinks } from './docker-format.js';

describe('formatDockerContainerPorts', () => {
  it('splits, trims, and filters string port text', () => {
    expect(formatDockerContainerPorts({ ports: '8080, 9090 ,' })).toBe('8080, 9090');
    expect(formatDockerContainerPorts({ ports: '  , ' })).toBe('-');
    expect(formatDockerContainerPorts({ ports: '' })).toBe('-');
    expect(formatDockerContainerPorts({ ports: '8080' })).toBe('8080');
  });

  it('formats object items with PascalCase and camelCase fields', () => {
    expect(formatDockerContainerPorts({ ports: [{ PrivatePort: 80, PublicPort: 8080, Type: 'tcp' }] })).toBe('8080:80/tcp');
    expect(formatDockerContainerPorts({ ports: [{ privatePort: 80, hostPort: 8080, type: 'udp' }] })).toBe('8080:80/udp');
    expect(formatDockerContainerPorts({ ports: [{ containerPort: 22, hostPort: 2222 }] })).toBe('2222:22/tcp');
  });

  it('falls back to the private port when no public port exists', () => {
    expect(formatDockerContainerPorts({ ports: [{ PrivatePort: 53 }] })).toBe('53/tcp');
  });

  it('keeps zero as a valid port value', () => {
    expect(formatDockerContainerPorts({ ports: [{ PrivatePort: 0 }] })).toBe('0/tcp');
  });

  it('drops object items without usable ports', () => {
    expect(formatDockerContainerPorts({ ports: [{ HostPort: 8080 }] })).toBe('-');
    expect(formatDockerContainerPorts({ ports: [{ PrivatePort: null, PublicPort: '' }] })).toBe('-');
  });

  it('mixes string and object items in arrays', () => {
    expect(formatDockerContainerPorts({ ports: [' 8080 ', { PrivatePort: 80, PublicPort: 8090 }] })).toBe('8080, 8090:80/tcp');
  });

  it('formats inspect-style port bindings', () => {
    expect(formatDockerContainerPorts({ ports: { '80/tcp': [{ HostPort: '49153' }] } })).toBe('49153:80/tcp');
    expect(formatDockerContainerPorts({ ports: { '443/udp': [{ hostPort: '4444' }] } })).toBe('4444:443/udp');
  });

  it('renders inspect entries with empty or missing bindings as unbound ports', () => {
    expect(formatDockerContainerPorts({ ports: { '80/tcp': [] } })).toBe('80/tcp');
    expect(formatDockerContainerPorts({ ports: { '80/tcp': null } })).toBe('80/tcp');
    expect(formatDockerContainerPorts({ ports: { '22': undefined } })).toBe('22/tcp');
  });

  it('formats multiple bindings across ports', () => {
    const ports = {
      '80/tcp': [{ HostPort: '8080' }, { HostPort: '8081' }],
      '443/tcp': [{ HostPort: '443' }],
    };
    expect(formatDockerContainerPorts({ ports })).toBe('8080:80/tcp, 8081:80/tcp, 443:443/tcp');
  });

  it('deduplicates by normalized key with case and /tcp suffix folding', () => {
    expect(formatDockerContainerPorts({ ports: '80, 80' })).toBe('80');
    expect(formatDockerContainerPorts({ ports: ['80/tcp', '80'] })).toBe('80/tcp');
    expect(formatDockerContainerPorts({ ports: ['8080', '8080/TCP'] })).toBe('8080');
    expect(formatDockerContainerPorts({ ports: [{ PrivatePort: 80 }, { PrivatePort: 80 }] })).toBe('80/tcp');
    expect(formatDockerContainerPorts({ ports: [' 80 ', '80'] })).toBe('80');
  });

  it('handles missing, nullish, and legacy-shaped containers', () => {
    expect(formatDockerContainerPorts()).toBe('-');
    expect(formatDockerContainerPorts(null)).toBe('-');
    expect(formatDockerContainerPorts({})).toBe('-');
    expect(formatDockerContainerPorts({ Ports: '1234' })).toBe('1234');
    expect(formatDockerContainerPorts({ portMappings: [{ PrivatePort: 22 }] })).toBe('22/tcp');
  });
});

describe('getDockerContainerPortLinks', () => {
  it('links only published host ports with plain http', () => {
    const links = getDockerContainerPortLinks({ ports: '5432:5432, 443:443, 80' }, '203.0.113.9');
    expect(links.map(item => item.label)).toEqual(['5432:5432', '443:443', '80']);
    expect(links.map(item => item.url)).toEqual([
      'http://203.0.113.9:5432',
      'http://203.0.113.9:443',
      '',
    ]);
  });

  it('keeps link labels in sync with formatDockerContainerPorts', () => {
    const cases = [
      { ports: '8080, 9090 ,' },
      { ports: [{ PrivatePort: 80, PublicPort: 8080, Type: 'tcp' }] },
      { ports: [' 8080 ', { PrivatePort: 80, PublicPort: 8090 }] },
      { ports: { '80/tcp': [{ HostPort: '49153' }], '22': undefined } },
      { Ports: '1234' },
    ];
    for (const container of cases) {
      const expected = formatDockerContainerPorts(container);
      const actual = getDockerContainerPortLinks(container, '1.2.3.4')
        .map(item => item.label)
        .join(', ');
      expect(actual).toBe(expected);
    }
  });

  it('returns no link when the server host is unusable', () => {
    const ports = '8080:80';
    expect(buildDockerContainerPortUrl('', ports)).toBe('');
    expect(buildDockerContainerPortUrl('-', ports)).toBe('');
    expect(buildDockerContainerPortUrl('0.0.0.0', ports)).toBe('');
    expect(buildDockerContainerPortUrl('1.2.3.4:22', ports)).toBe('');
    expect(getDockerContainerPortLinks({ ports }, '').every(item => !item.url)).toBe(true);
  });

  it('brackets ipv6 hosts and ignores optional protocol suffixes', () => {
    expect(buildDockerContainerPortUrl('2603:c024:c021:2e00::5', '8080:80')).toBe(
      'http://[2603:c024:c021:2e00::5]:8080'
    );
    expect(buildDockerContainerPortUrl('[2603:c024:c021:2e00::5]', '8080:80')).toBe(
      'http://[2603:c024:c021:2e00::5]:8080'
    );
    expect(buildDockerContainerPortUrl('example.com', '8080:80/udp')).toBe(
      'http://example.com:8080'
    );
  });
});