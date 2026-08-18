import { describe, expect, it, vi } from 'vitest';
import { subscribeDialog, resolveDialog, cancelDialog, dialog } from './dialog.js';

const subscribe = () => {
  const listener = vi.fn();
  const unsubscribe = subscribeDialog(listener);
  listener.mockClear();
  return { listener, unsubscribe };
};

describe('subscribeDialog', () => {
  it('immediately notifies the listener with null when idle', () => {
    const listener = vi.fn();
    const unsubscribe = subscribeDialog(listener);
    expect(listener).toHaveBeenCalledWith(null);
    unsubscribe();
  });

  it('stops notifying after unsubscribe', async () => {
    const { listener, unsubscribe } = subscribe();
    unsubscribe();
    const promise = dialog.alert('x');
    expect(listener).not.toHaveBeenCalled();
    resolveDialog();
    await promise;
  });
});

describe('resolveDialog', () => {
  it('resolves queued dialogs in FIFO order', async () => {
    const { listener, unsubscribe } = subscribe();
    const first = dialog.alert('first');
    const second = dialog.confirm({ message: 'second' });
    expect(listener.mock.calls.at(-1)[0]).toMatchObject({
      type: 'alert',
      options: expect.objectContaining({ message: 'first' }),
    });
    resolveDialog('ack');
    await expect(first).resolves.toBe(true);
    expect(listener.mock.calls.at(-1)[0]).toMatchObject({
      type: 'confirm',
      options: expect.objectContaining({ message: 'second' }),
    });
    resolveDialog(false);
    await expect(second).resolves.toBe(false);
    expect(listener.mock.calls.at(-1)[0]).toBeNull();
    unsubscribe();
  });

  it('is a no-op when nothing is active', () => {
    const { listener, unsubscribe } = subscribe();
    expect(() => resolveDialog('x')).not.toThrow();
    expect(() => cancelDialog()).not.toThrow();
    expect(listener).not.toHaveBeenCalled();
    unsubscribe();
  });
});

describe('cancelDialog', () => {
  it('resolves confirm dialogs with false', async () => {
    const promise = dialog.confirm({ message: 'continue?' });
    cancelDialog();
    await expect(promise).resolves.toBe(false);
  });

  it('resolves alert dialogs to true regardless of cancellation', async () => {
    const promise = dialog.alert('info');
    cancelDialog();
    await expect(promise).resolves.toBe(true);
  });

  it('resolves prompt dialogs with null', async () => {
    const promise = dialog.prompt({ message: 'input' });
    cancelDialog();
    await expect(promise).resolves.toBeNull();
  });
});

describe('dialog facade', () => {
  it('alert accepts a string message with an optional title', async () => {
    const { listener, unsubscribe } = subscribe();
    const promise = dialog.alert('hello', '自定义标题');
    expect(listener.mock.calls.at(-1)[0]).toMatchObject({
      type: 'alert',
      options: { message: 'hello', title: '自定义标题', confirmText: '确定' },
    });
    resolveDialog();
    await expect(promise).resolves.toBe(true);
    unsubscribe();
  });

  it('alert accepts an options object', async () => {
    const { listener, unsubscribe } = subscribe();
    const promise = dialog.alert({ message: 'obj', title: 't', description: 'ignored' });
    expect(listener.mock.calls.at(-1)[0].options).toMatchObject({ message: 'obj', title: 't' });
    resolveDialog();
    await promise;
    unsubscribe();
  });

  it('alert falls back to description when message is absent', async () => {
    const { listener, unsubscribe } = subscribe();
    const promise = dialog.alert({ description: 'desc only' });
    expect(listener.mock.calls.at(-1)[0].options).toMatchObject({ message: 'desc only', title: '提示' });
    resolveDialog();
    await promise;
    unsubscribe();
  });

  it('confirm applies default texts', async () => {
    const { listener, unsubscribe } = subscribe();
    const promise = dialog.confirm({});
    expect(listener.mock.calls.at(-1)[0].options).toMatchObject({
      title: '确认',
      confirmText: '确定',
      cancelText: '取消',
    });
    resolveDialog(true);
    await expect(promise).resolves.toBe(true);
    unsubscribe();
  });

  it('prompt normalizes value into defaultValue and keeps placeholder', async () => {
    const { listener, unsubscribe } = subscribe();
    const promise = dialog.prompt({ value: 'vw', defaultValue: 'dv', placeholder: 'ph' });
    const options = listener.mock.calls.at(-1)[0].options;
    expect(options.defaultValue).toBe('dv');
    expect(options.placeholder).toBe('ph');
    expect(options.title).toBe('输入');
    resolveDialog('res');
    await expect(promise).resolves.toBe('res');
    unsubscribe();
  });

  it('deleteResource injects destructive confirmation settings', async () => {
    const { listener, unsubscribe } = subscribe();
    const promise = dialog.deleteResource({ message: 'delete it' });
    expect(listener.mock.calls.at(-1)[0]).toMatchObject({
      type: 'confirm',
      options: { deleteResource: true, variant: 'destructive', confirmText: '删除', message: 'delete it' },
    });
    resolveDialog(true);
    await expect(promise).resolves.toBe(true);
    unsubscribe();
  });

  it('deleteResource keeps custom variant and confirmText', async () => {
    const { listener, unsubscribe } = subscribe();
    const promise = dialog.deleteResource({ variant: 'warning', confirmText: '移除' });
    expect(listener.mock.calls.at(-1)[0].options).toMatchObject({ variant: 'warning', confirmText: '移除' });
    resolveDialog(true);
    await promise;
    unsubscribe();
  });

  it('deleteResource works without options', async () => {
    const { listener, unsubscribe } = subscribe();
    const promise = dialog.deleteResource();
    expect(listener.mock.calls.at(-1)[0].options).toMatchObject({
      deleteResource: true,
      variant: 'destructive',
      confirmText: '删除',
    });
    resolveDialog(false);
    await expect(promise).resolves.toBe(false);
    unsubscribe();
  });

  it('is exposed as globalThis.apiMonitorDialog', () => {
    expect(globalThis.apiMonitorDialog).toBe(dialog);
  });
});