const THEME_META_SELECTOR = 'meta[name="theme-color"]';
const LIGHT_TITLEBAR = '#f8f7f4';
const DARK_TITLEBAR = '#050505';
const UPDATE_CHANNEL = 'api-monitor-app-updates';
const RELOADED_VERSION_KEY = 'api-monitor-reloaded-version';
const UPDATE_SIGNAL_KEY = 'api-monitor-update-signal';

let refreshingForUpdate = false;
let updateChannel = null;

const isStandalone = () =>
  window.matchMedia?.('(display-mode: standalone)').matches ||
  window.navigator.standalone === true ||
  document.referrer.startsWith('android-app://');

const upsertThemeMeta = (name, media) => {
  const selector = media
    ? `${THEME_META_SELECTOR}[media="${media}"]`
    : `${THEME_META_SELECTOR}:not([media])`;
  let meta = document.head.querySelector(selector);

  if (!meta) {
    meta = document.createElement('meta');
    meta.name = name;
    if (media) meta.media = media;
    document.head.appendChild(meta);
  }

  return meta;
};

const applyTitlebarColor = () => {
  const mode = document.documentElement.dataset.mode === 'dark' ? 'dark' : 'light';
  const fallbackColor = mode === 'dark' ? DARK_TITLEBAR : LIGHT_TITLEBAR;
  const topbar = document.querySelector('.app-main-topbar');
  const color = topbar ? getComputedStyle(topbar).backgroundColor : fallbackColor;

  upsertThemeMeta('theme-color').content = color;
  upsertThemeMeta('theme-color', '(prefers-color-scheme: light)').content = color;
  upsertThemeMeta('theme-color', '(prefers-color-scheme: dark)').content = color;

  if ('windowControlsOverlay' in navigator) {
    document.documentElement.style.setProperty('--app-titlebar-area-x', 'env(titlebar-area-x, 0px)');
    document.documentElement.style.setProperty('--app-titlebar-area-y', 'env(titlebar-area-y, 0px)');
    document.documentElement.style.setProperty('--app-titlebar-area-width', 'env(titlebar-area-width, 100vw)');
    document.documentElement.style.setProperty('--app-titlebar-area-height', 'env(titlebar-area-height, 0px)');
  }

  document.documentElement.dataset.displayMode = isStandalone() ? 'standalone' : 'browser';
};

const watchTitlebarColor = () => {
  applyTitlebarColor();
  window.requestAnimationFrame(applyTitlebarColor);

  const themeObserver = new MutationObserver(applyTitlebarColor);
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-mode', 'data-theme-mode'],
  });

  if (!document.querySelector('.app-main-topbar')) {
    const topbarObserver = new MutationObserver(() => {
      if (!document.querySelector('.app-main-topbar')) return;
      applyTitlebarColor();
      topbarObserver.disconnect();
    });
    topbarObserver.observe(document.documentElement, { childList: true, subtree: true });
  }

  window.matchMedia?.('(display-mode: standalone)').addEventListener('change', applyTitlebarColor);
  window.matchMedia?.('(prefers-color-scheme: dark)').addEventListener('change', applyTitlebarColor);

  if ('windowControlsOverlay' in navigator) {
    navigator.windowControlsOverlay.addEventListener('geometrychange', applyTitlebarColor);
  }
};

const reloadForUpdate = (version = 'controller-change') => {
  if (refreshingForUpdate) return;
  if (window.sessionStorage.getItem(RELOADED_VERSION_KEY) === version) return;

  refreshingForUpdate = true;
  window.sessionStorage.setItem(RELOADED_VERSION_KEY, version);

  window.location.reload();
};

const handleVersionMessage = (message, shouldFanOut = true) => {
  if (!['APP_UPDATED', 'APP_VERSION'].includes(message?.type) || !message.version) return;

  if (shouldFanOut) {
    updateChannel?.postMessage(message);
    try {
      window.localStorage.setItem(UPDATE_SIGNAL_KEY, JSON.stringify(message));
    } catch {
      // Storage can be unavailable in private/locked-down browser contexts.
    }
  }
  reloadForUpdate(message.version);
};

const watchServiceWorkerUpdates = (registration) => {
  registration.waiting?.postMessage({ type: 'SKIP_WAITING' });

  registration.addEventListener('updatefound', () => {
    const worker = registration.installing;
    if (!worker) return;

    worker.addEventListener('statechange', () => {
      if (worker.state === 'installed' && navigator.serviceWorker.controller) {
        worker.postMessage({ type: 'SKIP_WAITING' });
      }
    });
  });

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') {
      registration.update().catch(() => {});
    }
  });
};

const registerServiceWorker = () => {
  if (!('serviceWorker' in navigator)) return;
  if (!window.isSecureContext && window.location.hostname !== 'localhost') return;

  const hadControllerAtStartup = Boolean(navigator.serviceWorker.controller);

  if ('BroadcastChannel' in window) {
    updateChannel = new window.BroadcastChannel(UPDATE_CHANNEL);
    updateChannel.addEventListener('message', (event) => handleVersionMessage(event.data, false));
  }

  window.addEventListener('storage', (event) => {
    if (event.key !== UPDATE_SIGNAL_KEY || !event.newValue) return;
    try {
      handleVersionMessage(JSON.parse(event.newValue), false);
    } catch {
      // Ignore malformed values written by an older application version.
    }
  });

  navigator.serviceWorker.addEventListener('message', (event) => handleVersionMessage(event.data));
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (!hadControllerAtStartup || refreshingForUpdate) return;

    const controller = navigator.serviceWorker.controller;
    if (controller) {
      // 询问新 controller 的应用版本：收到应答后由 handleVersionMessage
      // 按版本决定是否重载（版本一致则不重载，避免无谓刷新）；
      // 兜底：旧版 sw.js 未实现版本应答时 1s 后仍刷新一次保证更新生效。
      controller.postMessage({ type: 'GET_APP_VERSION' });
      window.setTimeout(() => reloadForUpdate(), 1000);
    }
  });

  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/sw.js', { scope: '/', updateViaCache: 'none' })
      .then((registration) => {
        watchServiceWorkerUpdates(registration);
      })
      .catch((error) => {
        console.warn('PWA service worker registration failed:', error);
      });
  });
};

export const setupPwa = () => {
  document.documentElement.dataset.pwaReady = 'true';
  watchTitlebarColor();
  registerServiceWorker();
};
