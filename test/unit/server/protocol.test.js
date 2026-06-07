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

  it('keeps all GPU model names from host info', () => {
    const metrics = protocol.stateToFrontendFormat(
      {
        cpu: 1,
        mem_used: 1024,
        disk_used: 1024,
        gpu: 38,
        gpu_mem_used: 1012 * 1024 * 1024,
      },
      {
        mem_total: 2048,
        disk_total: 4096,
        gpu_mem_total: 8 * 1024 * 1024 * 1024,
        gpu: ['NVIDIA RTX 4090 D', 'NVIDIA RTX 4080 SUPER'],
      },
    );

    expect(metrics.gpu_model).toBe('NVIDIA RTX 4090 D / NVIDIA RTX 4080 SUPER');
  });
});

describe('server protocol temperature metrics', () => {
  it('derives CPU temperature from package sensors', () => {
    const metrics = protocol.stateToFrontendFormat(
      {
        cpu: 12,
        mem_used: 1024,
        disk_used: 1024,
        temperatures: [
          { name: 'NVMe Composite', temperature: 41 },
          { name: 'Package id 0', temperature: 64.5 },
        ],
      },
      {
        mem_total: 2048,
        disk_total: 4096,
      },
    );

    expect(metrics.cpu_temp).toBe(64.5);
  });

  it('prefers valid CPU temperature fallback when cpu_temp is zero', () => {
    expect(protocol.resolveCpuTemperature({
      cpu_temp: 0,
      cpuTemp: 58.2,
    })).toBe(58.2);
  });

  it('normalizes frontend metrics with CPU temperature sensors', () => {
    const metrics = protocol.normalizeFrontendMetrics({
      temperatures: [
        {
          name: 'coretemp',
          entries: [
            { label: 'Core 0', current: 51 },
            { label: 'Package id 0', current: 61 },
          ],
        },
      ],
    });

    expect(metrics.cpu_temp).toBe(61);
  });

  it('derives CPU temperature from psutil-style sensor maps', () => {
    const metrics = protocol.normalizeFrontendMetrics({
      temperatures: {
        nvme: [{ label: 'Composite', current: 42 }],
        coretemp: [
          { label: 'Core 0', current: 53 },
          { label: 'Package id 0', current: 67.4 },
        ],
      },
    });

    expect(metrics.cpu_temp).toBe(67.4);
  });

  it('derives CPU temperature from nested CPU sensor objects', () => {
    const metrics = protocol.normalizeFrontendMetrics({
      sensors: {
        cpu: {
          package: { current: '59.5 C' },
        },
      },
    });

    expect(metrics.cpu_temp).toBe(59.5);
  });

  it('derives CPU temperature from nested cpu.sensors payloads', () => {
    const metrics = protocol.normalizeFrontendMetrics({
      cpu: {
        sensors: [
          { name: 'CPU Package', temperature: 62.3 },
        ],
      },
    });

    expect(metrics.cpu_temp).toBe(62.3);
  });

  it('preserves CPU package power from agent state', () => {
    const metrics = protocol.stateToFrontendFormat(
      {
        cpu: 12,
        cpu_power: 34.56,
        mem_used: 1024,
        disk_used: 1024,
      },
      {
        mem_total: 2048,
        disk_total: 4096,
      },
    );

    expect(metrics.cpu_power).toBe('34.6W');
    expect(metrics.cpu_power_w).toBe(34.56);
  });
});
