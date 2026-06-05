import { createKumoToastManager } from '@cloudflare/kumo/components/toast';

const globalToastKey = '__apiMonitorKumoToastManager';

export const kumoToastManager =
  globalThis[globalToastKey] || (globalThis[globalToastKey] = createKumoToastManager());

const normalizeToast = (type, messageOrOptions, options = {}) => {
  const source =
    typeof messageOrOptions === 'object' && messageOrOptions !== null
      ? messageOrOptions
      : { message: messageOrOptions };

  const message = source.message ?? source.description ?? '';
  const title = source.title || message || type;
  const description = source.title ? message : source.description;

  return {
    ...source,
    ...options,
    title,
    description,
    variant: source.variant || source.type || type,
    timeout: source.duration ?? options.duration,
    className: `scale-90 origin-top-right !p-3 !min-h-10 ${source.className || options.className || ''}`
  };
};

const addToast = (type, messageOrOptions, options) => {
  if (type === 'info') {
    const source =
      typeof messageOrOptions === 'object' && messageOrOptions !== null
        ? messageOrOptions
        : options;

    if (!source?.isManual) return null;
  }

  return kumoToastManager.add(normalizeToast(type, messageOrOptions, options));
};

const toastManager = {
  show: (options = {}) => addToast(options.type || 'info', options, options),
  success: (message, options = {}) => addToast('success', message, options),
  error: (message, options = {}) => addToast('error', message, { timeout: 4000, ...options }),
  warning: (message, options = {}) => addToast('warning', message, options),
  info: (message, options = {}) => addToast('info', message, options),
  remove: (id) => kumoToastManager.close(id),
  removeAll: () => kumoToastManager.close(),
};

export default toastManager;

export const toast = {
  show: options => toastManager.show(options),
  success: (message, options) => toastManager.success(message, options),
  error: (message, options) => toastManager.error(message, options),
  warning: (message, options) => toastManager.warning(message, options),
  info: (message, options) => toastManager.info(message, options),
  remove: id => toastManager.remove(id),
  removeAll: () => toastManager.removeAll(),
};

export function showToast(message, type = 'info') {
  return toastManager[type]?.(message, { isManual: true });
}
