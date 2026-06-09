import { describe, expect, it } from 'vitest';

import commandUtils from '../../../modules/server-api/command-utils.js';

const {
  buildCommandVariables,
  detectDangerousCommand,
  normalizeList,
  renderCommandTemplate,
} = commandUtils;

describe('server command utils', () => {
  it('renders host variables into command templates', () => {
    const variables = buildCommandVariables(
      { host: '10.0.0.2', port: 22, username: 'root', name: 'web-1' },
      { cwd: '/var/www' }
    );

    expect(renderCommandTemplate('ssh {username}@{host}:{port} && cd {cwd}', variables))
      .toBe('ssh root@10.0.0.2:22 && cd /var/www');
  });

  it('keeps unknown variables unchanged', () => {
    expect(renderCommandTemplate('echo {unknown}', {})).toBe('echo {unknown}');
  });

  it('detects dangerous destructive commands', () => {
    const result = detectDangerousCommand('sudo rm -rf /var/log/app');

    expect(result.dangerous).toBe(true);
    expect(result.reasons.length).toBeGreaterThan(0);
  });

  it('does not mark ordinary read-only commands as dangerous', () => {
    const result = detectDangerousCommand('df -h && docker ps');

    expect(result.dangerous).toBe(false);
    expect(result.reasons).toEqual([]);
  });

  it('normalizes JSON and comma separated tag lists', () => {
    expect(normalizeList('["常用","Docker"]')).toEqual(['常用', 'Docker']);
    expect(normalizeList('常用, Docker')).toEqual(['常用', 'Docker']);
  });
});
