const listeners = new Set();
const queue = [];
let activeRequest = null;
let nextId = 1;

const DEFAULT_TEXT = {
  alert: {
    title: '提示',
    confirmText: '确定',
  },
  confirm: {
    title: '确认操作',
    confirmText: '确定',
    cancelText: '取消',
  },
  prompt: {
    title: '请输入',
    confirmText: '确定',
    cancelText: '取消',
  },
};

const normalizeOptions = (type, input = {}) => {
  const source = typeof input === 'string' ? { message: input } : { ...input };
  const defaults = DEFAULT_TEXT[type] || DEFAULT_TEXT.confirm;

  return {
    ...source,
    title: source.title || defaults.title,
    message: source.message || source.description || '',
    confirmText: source.confirmText || defaults.confirmText,
    cancelText: source.cancelText ?? defaults.cancelText,
    confirmClass: source.confirmClass || '',
    placeholder: source.placeholder || '',
    defaultValue: source.defaultValue ?? source.value ?? '',
    role: source.role,
    size: source.size,
    disablePointerDismissal: source.disablePointerDismissal,
  };
};

const emit = () => {
  listeners.forEach((listener) => listener(activeRequest));
};

const flushQueue = () => {
  if (activeRequest || queue.length === 0) return;
  activeRequest = queue.shift();
  emit();
};

const requestDialog = (type, options) => new Promise((resolve) => {
  queue.push({
    id: nextId++,
    type,
    options: normalizeOptions(type, options),
    resolve,
  });
  flushQueue();
});

export const subscribeDialog = (listener) => {
  listeners.add(listener);
  listener(activeRequest);
  return () => {
    listeners.delete(listener);
  };
};

export const resolveDialog = (value) => {
  if (!activeRequest) return;
  const request = activeRequest;
  activeRequest = null;
  request.resolve(value);
  emit();
  flushQueue();
};

export const cancelDialog = () => {
  if (!activeRequest) return;
  resolveDialog(activeRequest.type === 'confirm' ? false : null);
};

export const dialog = {
  alert: (messageOrOptions, title) => requestDialog(
    'alert',
    typeof messageOrOptions === 'string'
      ? { message: messageOrOptions, title }
      : messageOrOptions
  ).then(() => true),
  confirm: (options) => requestDialog('confirm', options),
  deleteResource: (options) => requestDialog('confirm', {
    ...options,
    deleteResource: true,
    variant: options?.variant || 'destructive',
    confirmText: options?.confirmText || '删除',
  }),
  prompt: (options) => requestDialog('prompt', options),
};

if (typeof globalThis !== 'undefined') {
  globalThis.apiMonitorDialog = dialog;
}

export default dialog;
