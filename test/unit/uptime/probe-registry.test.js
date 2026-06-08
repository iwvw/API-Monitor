import { createServer } from 'node:http';
import { describe, it, expect } from 'vitest';

const registry = await import('../../../modules/uptime-api/adapters/probe-registry.js');

describe('uptime probe registry', () => {
  it('parses exact and ranged accepted status codes', () => {
    const accept = registry.parseAcceptedStatusCodes('200-204, 301, 418');
    expect(accept(200)).toBe(true);
    expect(accept(204)).toBe(true);
    expect(accept(301)).toBe(true);
    expect(accept(418)).toBe(true);
    expect(accept(500)).toBe(false);
  });

  it('falls back to 2xx when accepted status code config is empty', () => {
    const accept = registry.parseAcceptedStatusCodes('');
    expect(accept(204)).toBe(true);
    expect(accept(301)).toBe(false);
  });

  it('reads simple JSON path expressions', () => {
    const payload = { data: { status: 'ok', items: [{ latency: 42 }] } };
    expect(registry.getJsonPathValue(payload, '$.data.status')).toBe('ok');
    expect(registry.getJsonPathValue(payload, 'data.items[0].latency')).toBe(42);
    expect(registry.getJsonPathValue(payload, 'data.missing.value')).toBeUndefined();
  });

  it('compares JSON query values with operators', () => {
    expect(registry.compareJsonValue('ok', 'ok', 'equals')).toBe(true);
    expect(registry.compareJsonValue('api is healthy', 'healthy', 'contains')).toBe(true);
    expect(registry.compareJsonValue(128, '100', 'gt')).toBe(true);
    expect(registry.compareJsonValue('v1.2.3', '^v\\d+', 'regex')).toBe(true);
    expect(registry.compareJsonValue(undefined, '', 'not_exists')).toBe(true);
  });

  it('checks JSON query probes with a single HTTP request', async () => {
    let requestCount = 0;
    const server = createServer((req, res) => {
      requestCount++;
      res.setHeader('content-type', 'application/json');
      res.end(JSON.stringify({ data: { status: 'ok' } }));
    });

    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
    try {
      const { port } = server.address();
      const result = await registry.check({
        type: 'json',
        url: `http://127.0.0.1:${port}/health`,
        timeout: 2,
        accepted_status_codes: '200',
        config: {
          jsonQueryPath: '$.data.status',
          jsonQueryOperator: 'equals',
          jsonExpectedValue: 'ok',
        },
      });

      expect(result.ok).toBe(true);
      expect(result.details.jsonQueryActual).toBe('ok');
      expect(requestCount).toBe(1);
    } finally {
      await new Promise(resolve => server.close(resolve));
    }
  });
});
