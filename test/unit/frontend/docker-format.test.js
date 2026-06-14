import { describe, expect, it } from 'vitest';

import { formatDockerContainerPorts } from '../../../src/js/modules/docker-format.js';

describe('docker format helpers', () => {
  it('deduplicates repeated string port mappings', () => {
    expect(formatDockerContainerPorts({
      ports: '3000:3000, 3000:3000, 8080:80',
    })).toBe('3000:3000, 8080:80');
  });

  it('deduplicates repeated Docker API port objects', () => {
    expect(formatDockerContainerPorts({
      Ports: [
        { IP: '0.0.0.0', PublicPort: 3000, PrivatePort: 3000, Type: 'tcp' },
        { IP: '::', PublicPort: 3000, PrivatePort: 3000, Type: 'tcp' },
      ],
    })).toBe('3000:3000/tcp');
  });

  it('formats Docker inspect port objects', () => {
    expect(formatDockerContainerPorts({
      Ports: {
        '8080/tcp': [
          { HostIp: '0.0.0.0', HostPort: '3770' },
          { HostIp: '::', HostPort: '3770' },
        ],
        '9000/tcp': null,
      },
    })).toBe('3770:8080/tcp, 9000/tcp');
  });

  it('returns a placeholder when there are no ports', () => {
    expect(formatDockerContainerPorts({ ports: [] })).toBe('-');
  });
});
