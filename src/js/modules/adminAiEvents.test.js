import { describe, expect, it } from 'vitest';
import { parseAdminAiEvent } from './adminAiEvents.js';

describe('parseAdminAiEvent', () => {
  it('merges the type with parsed payload fields', () => {
    expect(parseAdminAiEvent('monitor-update', '{"serverId":1,"load":0.5}')).toEqual({
      type: 'monitor-update',
      serverId: 1,
      load: 0.5,
    });
  });

  it('lets a parsed type field take precedence', () => {
    expect(parseAdminAiEvent('outer', '{"type":"inner","n":2}')).toEqual({ type: 'inner', n: 2 });
  });

  it('handles non-object JSON payloads', () => {
    expect(parseAdminAiEvent('x', '123')).toEqual({ type: 'x' });
    expect(parseAdminAiEvent('x', 'null')).toEqual({ type: 'x' });
  });

  it('throws a SyntaxError on malformed JSON', () => {
    expect(() => parseAdminAiEvent('x', 'not-json')).toThrow(SyntaxError);
    expect(() => parseAdminAiEvent('x', '{"a":')).toThrow(SyntaxError);
    expect(() => parseAdminAiEvent('x', '')).toThrow(SyntaxError);
  });
});