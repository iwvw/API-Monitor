import { describe, expect, it } from 'vitest';
import { FLOW_UNIT_BADGE_CLASS, getFlowUnitClassName } from './flowUnits.js';

describe('getFlowUnitClassName', () => {
  it('maps each unit to its badge class', () => {
    expect(getFlowUnitClassName('K')).toBe('border-kumo-info/65 bg-kumo-info/25 text-kumo-info');
    expect(getFlowUnitClassName('M')).toBe('border-kumo-success/65 bg-kumo-success/25 text-kumo-success');
    expect(getFlowUnitClassName('G')).toBe('border-kumo-warning/65 bg-kumo-warning/25 text-kumo-warning');
    expect(getFlowUnitClassName('T')).toBe('border-kumo-danger/65 bg-kumo-danger/20 text-kumo-danger');
    expect(getFlowUnitClassName('P')).toBe('border-kumo-danger/75 bg-kumo-danger/25 text-kumo-danger');
  });

  it('is case-insensitive', () => {
    expect(getFlowUnitClassName('k')).toBe(getFlowUnitClassName('K'));
    expect(getFlowUnitClassName('m')).toBe(getFlowUnitClassName('M'));
    expect(getFlowUnitClassName('g')).toBe(getFlowUnitClassName('G'));
    expect(getFlowUnitClassName('t')).toBe(getFlowUnitClassName('T'));
    expect(getFlowUnitClassName('p')).toBe(getFlowUnitClassName('P'));
  });

  it('falls back to the default badge class', () => {
    const fallback = 'border-kumo-interact/70 bg-kumo-recessed/70 text-kumo-default';
    expect(getFlowUnitClassName('B')).toBe(fallback);
    expect(getFlowUnitClassName('KB')).toBe(fallback);
    expect(getFlowUnitClassName('X')).toBe(fallback);
    expect(getFlowUnitClassName('')).toBe(fallback);
    expect(getFlowUnitClassName()).toBe(fallback);
    expect(getFlowUnitClassName(null)).toBe(fallback);
  });

  it('exposes the shared badge class constant', () => {
    expect(FLOW_UNIT_BADGE_CLASS).toContain('inline-flex');
  });
});