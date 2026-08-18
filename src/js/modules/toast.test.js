import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

const { mockAdd, mockClose, mockCreate } = vi.hoisted(() => ({
  mockAdd: vi.fn(() => ({ id: 'toast-1' })),
  mockClose: vi.fn(),
  mockCreate: vi.fn(),
}));

vi.mock('@cloudflare/kumo/components/toast', () => ({
  createKumoToastManager: (...args) => {
    mockCreate(...args);
    return { add: mockAdd, close: mockClose };
  },
}));

import toast, { showToast, kumoToastManager } from './toast.js';

describe('toast/kumoToastManager', () => {
  beforeEach(() => {
    mockAdd.mockClear();
    mockClose.mockClear();
  });

  it('creates the Kumo toast manager once', () => {
    expect(mockCreate).toHaveBeenCalledTimes(1);
    expect(kumoToastManager.add).toBe(mockAdd);
    expect(kumoToastManager.close).toBe(mockClose);
  });
});

describe('showToast', () => {
  beforeEach(() => {
    mockAdd.mockClear();
    mockClose.mockClear();
  });

  it('normalizes a plain string message for success', () => {
    const result = showToast('保存成功', 'success');
    expect(result).toEqual({ id: 'toast-1' });
    expect(mockAdd).toHaveBeenCalledTimes(1);
    expect(mockAdd.mock.calls[0][0]).toMatchObject({
      title: '保存成功',
      description: undefined,
      variant: 'success',
      isManual: true,
    });
    expect(mockAdd.mock.calls[0][0].className).toContain('scale-90 origin-top-right !p-3 !min-h-10');
  });

  it('defaults the type to info and keeps isManual', () => {
    showToast('你好');
    expect(mockAdd.mock.calls[0][0]).toMatchObject({ title: '你好', variant: 'info', timeout: undefined });
  });

  it('normalizes an options object input', () => {
    showToast({ title: '标题', description: '描述', duration: 2500 }, 'error');
    expect(mockAdd.mock.calls[0][0]).toMatchObject({
      title: '标题',
      description: '描述',
      variant: 'error',
      timeout: 2500,
      isManual: true,
    });
  });

  it('merges className from the payload', () => {
    showToast({ title: 't', className: 'extra-class' }, 'warning');
    expect(mockAdd.mock.calls[0][0].className).toContain('extra-class');
  });

  it('respects source.duration over the default', () => {
    showToast({ title: 't', duration: 500 }, 'error');
    expect(mockAdd.mock.calls[0][0].timeout).toBe(500);
  });
});

describe('toast manager', () => {
  beforeEach(() => {
    mockAdd.mockClear();
    mockClose.mockClear();
  });

  it('success uses the success variant', () => {
    toast.success('ok');
    expect(mockAdd.mock.calls[0][0]).toMatchObject({ title: 'ok', variant: 'success', timeout: undefined });
  });

  it('error keeps the raw timeout key out of the normalized payload', () => {
    toast.error('boom');
    expect(mockAdd.mock.calls[0][0]).toMatchObject({ title: 'boom', variant: 'error' });
    expect(mockAdd.mock.calls[0][0].timeout).toBeUndefined();
  });

  it('error applies an explicit duration', () => {
    toast.error('boom', { duration: 100 });
    expect(mockAdd.mock.calls[0][0].timeout).toBe(100);
  });

  it('warning uses the warning variant', () => {
    toast.warning('注意');
    expect(mockAdd.mock.calls[0][0]).toMatchObject({ title: '注意', variant: 'warning' });
  });

  it('info without isManual is suppressed', () => {
    expect(toast.info('静默')).toBeNull();
    expect(mockAdd).not.toHaveBeenCalled();
  });

  it('show with no type defaults to info and is suppressed', () => {
    expect(toast.show()).toBeNull();
    expect(mockAdd).not.toHaveBeenCalled();
  });

  it('show passes type through from options', () => {
    toast.show({ type: 'warning', title: 'w', duration: 700 });
    expect(mockAdd.mock.calls[0][0]).toMatchObject({
      title: 'w',
      variant: 'warning',
      timeout: 700,
    });
  });

  it('show with an explicit info type and isManual is allowed', () => {
    toast.show({ type: 'info', message: 'm', isManual: true });
    expect(mockAdd).toHaveBeenCalledTimes(1);
    expect(mockAdd.mock.calls[0][0]).toMatchObject({ variant: 'info', title: 'm' });
  });

  it('remove forwards the id to close', () => {
    toast.remove('toast-42');
    expect(mockClose).toHaveBeenCalledWith('toast-42');
  });

  it('removeAll calls close without arguments', () => {
    toast.removeAll();
    expect(mockClose).toHaveBeenCalledWith();
  });
});