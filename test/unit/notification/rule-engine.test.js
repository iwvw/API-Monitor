import { describe, it, expect } from 'vitest';

const ruleEngine = await import('../../../modules/notification-api/services/rule-engine.js');

describe('notification rule engine', () => {
  it('evaluates all conditions by default', () => {
    const result = ruleEngine.evaluateConditions(
      [
        { field: 'severity', operator: 'equals', value: 'critical' },
        { field: 'metrics.cpu', operator: 'gte', value: 90 },
      ],
      { severity: 'critical', metrics: { cpu: 92 } }
    );

    expect(result.allowed).toBe(true);
    expect(result.results).toHaveLength(2);
  });

  it('supports any mode and nested paths', () => {
    const result = ruleEngine.evaluateConditions(
      {
        mode: 'any',
        items: [
          { field: 'monitor.status', operator: 'equals', value: 'up' },
          { field: 'monitor.error', operator: 'contains', value: 'timeout' },
        ],
      },
      { monitor: { status: 'down', error: 'connect timeout' } }
    );

    expect(result.allowed).toBe(true);
  });

  it('converts object shorthand to equality checks', () => {
    const result = ruleEngine.evaluateConditions(
      { eventType: 'down', severity: 'warning' },
      { eventType: 'down', severity: 'info' }
    );

    expect(result.allowed).toBe(false);
    expect(result.results.map(item => item.field)).toEqual(['eventType', 'severity']);
  });
});
