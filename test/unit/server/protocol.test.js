import { describe, expect, it } from 'vitest';

import protocol from '../../../modules/server-api/protocol.js';

describe('server protocol GPU metrics', () => {
  it('builds GPU info with VRAM percent from byte counters', () => {
    const gpu = protocol.buildGpuInfo({
      gpu_model: 'NVIDIA RTX',
      gpu_usage: '38.0%',
      gpu_mem: '1012 MB/8 GB',
      gpu_mem_used: 1012 * 1024 * 1024,
      gpu_mem_total: 8 * 1024 * 1024 * 1024,
      gpu_power: '0W',
      gpu_temp: 0,
    });

    expect(gpu.Memory).toBe('1012 MB/8 GB');
    expect(gpu.Percent).toBeCloseTo(12.35, 2);
  });

  it('falls back to parsing GPU memory text when counters are absent', () => {
    expect(protocol.resolveGpuMemoryPercent({ gpu_mem: '1012 MB/8 GB' })).toBeCloseTo(12.35, 2);
  });

  it('accepts percent strings from existing GPU info caches', () => {
    expect(protocol.resolveGpuMemoryPercent({ gpu: { Percent: '12%' } })).toBe(12);
  });

  it('normalizes frontend metrics with GPU memory percent', () => {
    const metrics = protocol.normalizeFrontendMetrics({
      gpu_mem: '1012 MB/8 GB',
    });

    expect(metrics.gpu_mem_percent).toBeCloseTo(12.35, 2);
  });
});
