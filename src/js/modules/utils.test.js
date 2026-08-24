import { describe, expect, it, vi } from 'vitest';

import {
  escapeHtml,
  formatDateTime,
  getLocalTimestamp,
  formatLocalISO,
  formatFileSize,
  formatTokens,
  formatUptime,
  debounce,
  throttle,
  deepClone,
  maskAddress,
  formatRegion,
  formatSpeedCompact,
  parseSpeed,
  setDisplayTimeZone,
  getDisplayTimeZone,
} from './utils.js';

describe('escapeHtml', () => {
  it('escapes HTML special characters', () => {
    const div = { _text: '' };
    Object.defineProperty(div, 'textContent', {
      set(v) {
        this._text = String(v);
        this.innerHTML = this._text
          .replace(/&/g, '&amp;')
          .replace(/</g, '&lt;')
          .replace(/>/g, '&gt;')
          .replace(/"/g, '&quot;')
          .replace(/'/g, '&#39;');
      },
    });
    vi.stubGlobal('document', { createElement: () => div });
    expect(escapeHtml('<script>alert("x&y")</script>')).toBe(
      '&lt;script&gt;alert(&quot;x&amp;y&quot;)&lt;/script&gt;',
    );
    vi.unstubAllGlobals();
  });
});

describe('formatDateTime', () => {
  it('returns a dash for empty input', () => {
    expect(formatDateTime(null)).toBe('-');
    expect(formatDateTime(undefined)).toBe('-');
    expect(formatDateTime('')).toBe('-');
  });

  it('formats a Date object in the requested zone', () => {
    const d = new Date('2026-07-12T08:00:00Z');
    expect(formatDateTime(d, { timeZone: 'UTC' })).toBe('2026/7/12 08:00:00');
  });

  it('treats SQLite-style timestamps without a zone as UTC', () => {
    expect(formatDateTime('2026-07-12 08:00:00', { timeZone: 'UTC' })).toBe('2026/7/12 08:00:00');
  });

  it('appends Z to other zone-less strings', () => {
    expect(formatDateTime('2026/07/12 08:00:00', { timeZone: 'UTC' })).toBe('2026/7/12 08:00:00');
  });

  it('passes through strings that already carry a zone', () => {
    expect(formatDateTime('2026-07-12T08:00:00+02:00', { timeZone: 'UTC' })).toBe('2026/7/12 06:00:00');
  });

  it('falls back to the globally configured display zone', () => {
    setDisplayTimeZone('Asia/Shanghai');
    try {
      expect(formatDateTime('2026-07-12 08:00:00')).toBe('2026/07/12 16:00:00');
    } finally {
      setDisplayTimeZone('system');
    }
  });

  it('ignores the global zone when an explicit timeZone is passed', () => {
    setDisplayTimeZone('Asia/Shanghai');
    try {
      expect(formatDateTime('2026-07-12 08:00:00', { timeZone: 'UTC' })).toBe('2026/7/12 08:00:00');
    } finally {
      setDisplayTimeZone('system');
    }
  });
});

describe('displayTimeZone helpers', () => {
  it('returns system by default', () => {
    expect(getDisplayTimeZone()).toBe('system');
  });

  it('stores a trimmed zone', () => {
    setDisplayTimeZone('  Asia/Shanghai  ');
    expect(getDisplayTimeZone()).toBe('Asia/Shanghai');
    setDisplayTimeZone('system');
  });

  it('resets invalid input to system', () => {
    setDisplayTimeZone('');
    expect(getDisplayTimeZone()).toBe('system');
    setDisplayTimeZone(null);
    expect(getDisplayTimeZone()).toBe('system');
    setDisplayTimeZone(42);
    expect(getDisplayTimeZone()).toBe('system');
  });
});

describe('getLocalTimestamp', () => {
  it('produces a YYYY-MM-DD_HH-MM-SS shape', () => {
    const out = getLocalTimestamp();
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}$/);
  });
});

describe('formatLocalISO', () => {
  it('converts to local ISO without trailing Z', () => {
    const out = formatLocalISO('2026-07-12T08:00:00Z');
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}$/);
    expect(out.endsWith('Z')).toBe(false);
  });
});

describe('formatFileSize', () => {
  it('handles zero and edge cases', () => {
    expect(formatFileSize(0)).toBe('0 B');
  });

  it('formats binary units', () => {
    expect(formatFileSize(1024)).toBe('1 KB');
    expect(formatFileSize(1536)).toBe('1.5 KB');
    expect(formatFileSize(1024 * 1024)).toBe('1 MB');
    expect(formatFileSize(1024 * 1024 * 1024)).toBe('1 GB');
    expect(formatFileSize(5 * 1024 * 1024 * 1024 * 1024)).toBe('5 TB');
  });
});

describe('formatTokens', () => {
  it('formats token counts', () => {
    expect(formatTokens(0)).toBe('0');
    expect(formatTokens(undefined)).toBe('0');
    expect(formatTokens(999)).toBe('999');
    expect(formatTokens(1500)).toBe('1.5K');
    expect(formatTokens(1000000)).toBe('1M');
    expect(formatTokens(1234567890)).toBe('1.23B');
  });
});

describe('formatUptime', () => {
  it('returns a dash for nullish input', () => {
    expect(formatUptime(null)).toBe('-');
    expect(formatUptime(undefined)).toBe('-');
  });

  it('formats numeric seconds', () => {
    expect(formatUptime(0)).toBe('0分');
    expect(formatUptime(45)).toBe('0分');
    expect(formatUptime(3661)).toBe('1时1分');
    expect(formatUptime(90061)).toBe('1天1时1分');
  });

  it('parses Linux uptime style strings', () => {
    expect(formatUptime('up 1 day, 10:23')).toBe('1天10时23分');
    expect(formatUptime('up 10:23')).toBe('10时23分');
  });

  it('parses week/day/hour/minute words', () => {
    expect(formatUptime('up 1 week, 2 days, 3 hours, 4 minutes')).toBe('9天3时4分');
    expect(formatUptime('2 days 3 hours')).toBe('2天3时');
  });

  it('keeps the original value when nothing parses', () => {
    expect(formatUptime('random garbage')).toBe('random garbage');
    // "5 min" matches the minutes pattern and renders as 5分.
    expect(formatUptime('up 5 min')).toBe('5分');
    // A bare seconds mention has no parsable unit but hints at recency.
    expect(formatUptime('up 45 sec')).toBe('刚刚');
  });

  it('passes through non-string, non-number inputs', () => {
    const obj = { a: 1 };
    expect(formatUptime(obj)).toBe(obj);
  });
});

describe('debounce', () => {
  it('only calls the function once after rapid invocations', () => {
    vi.useFakeTimers();
    const fn = vi.fn();
    const debounced = debounce(fn, 100);
    debounced();
    debounced();
    debounced();
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(150);
    expect(fn).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
});

describe('throttle', () => {
  it('calls once immediately and then respects the limit', () => {
    vi.useFakeTimers();
    const fn = vi.fn();
    const throttled = throttle(fn, 100);
    throttled();
    throttled();
    throttled();
    expect(fn).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(150);
    throttled();
    expect(fn).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });
});

describe('deepClone', () => {
  it('clones nested objects, arrays, and dates', () => {
    const original = { a: 1, b: [1, 2, { c: 3 }], d: new Date('2026-01-01T00:00:00Z') };
    const clone = deepClone(original);
    expect(clone).toEqual(original);
    expect(clone).not.toBe(original);
    expect(clone.b).not.toBe(original.b);
    expect(clone.d).not.toBe(original.d);
    expect(clone.d.getTime()).toBe(original.d.getTime());
  });

  it('returns primitives unchanged', () => {
    expect(deepClone(null)).toBe(null);
    expect(deepClone(42)).toBe(42);
    expect(deepClone('str')).toBe('str');
  });
});

describe('maskAddress', () => {
  it('handles empty input and modes', () => {
    expect(maskAddress('')).toBe('');
    expect(maskAddress('1.2.3.4', 'normal')).toBe('1.2.3.4');
    expect(maskAddress('1.2.3.4', 'hidden')).toBe('****');
  });

  it('masks IPv4 addresses', () => {
    expect(maskAddress('192.168.1.100', 'masked')).toBe('192.168.*.*');
  });

  it('masks domain names', () => {
    expect(maskAddress('example.com', 'masked')).toBe('ex****.com');
    // Short first segment falls back to the generic mask branch.
    expect(maskAddress('a.com', 'masked')).toBe('a.****');
  });

  it('preserves protocol and path for URLs', () => {
    expect(maskAddress('https://sub.example.com/v1/status', 'masked')).toBe('https://su****.com/v1/status');
  });
});

describe('formatRegion', () => {
  it('handles empty input', () => {
    expect(formatRegion('')).toBe('未知');
    expect(formatRegion(null)).toBe('未知');
    expect(formatRegion({})).toBe('未知');
  });

  it('keeps Chinese text unchanged', () => {
    expect(formatRegion('香港')).toBe('香港');
  });

  it('maps known region codes', () => {
    expect(formatRegion('singapore')).toBe('新加坡');
    expect(formatRegion('cn-hangzhou')).toBe('杭州');
    expect(formatRegion('ap-singapore')).toBe('新加坡');
    expect(formatRegion('Hong Kong')).toBe('香港');
  });

  it('matches by substring and falls back to raw value', () => {
    expect(formatRegion('fra')).toBe('法兰克福');
    expect(formatRegion('us-east-1')).toBe('us-east-1');
  });

  it('accepts object shapes', () => {
    expect(formatRegion({ name: 'tokyo' })).toBe('东京');
    expect(formatRegion({ id: 'sin' })).toBe('新加坡');
  });
});

describe('formatSpeedCompact', () => {
  it('compacts speed strings', () => {
    expect(formatSpeedCompact('')).toBe('0B');
    // "0 B/s" -> regex leaves no unit letter, so the B is dropped.
    expect(formatSpeedCompact('0 B/s')).toBe('0');
    expect(formatSpeedCompact('1.5 MB/s')).toBe('1.5M');
    expect(formatSpeedCompact('10 KB/s')).toBe('10K');
  });
});

describe('parseSpeed', () => {
  it('splits number and unit', () => {
    expect(parseSpeed('')).toEqual({ num: '0', unit: 'B' });
    expect(parseSpeed('1.5 MB/s')).toEqual({ num: '1.5', unit: 'M' });
    expect(parseSpeed('10 KB/s')).toEqual({ num: '10', unit: 'K' });
    expect(parseSpeed('512 B/s')).toEqual({ num: '512', unit: 'B' });
  });

  it('returns a zero object for garbage input', () => {
    expect(parseSpeed('not-a-speed')).toEqual({ num: '0', unit: 'B' });
  });
});
