/* global self, caches */

const CACHE_VERSION = 'api-monitor-pwa-v6';
const APP_SHELL_CACHE = `${CACHE_VERSION}-shell`;
const RUNTIME_CACHE = `${CACHE_VERSION}-runtime`;

const APP_SHELL_URLS = [
  '/',
  '/index.html',
  '/manifest.webmanifest',
  '/logo.svg',
  '/pwa/apple-touch-icon.png',
  '/pwa/icon-192.png',
  '/pwa/icon-512.png',
  '/pwa/icon.svg',
  '/pwa/maskable-icon-192.png',
  '/pwa/maskable-icon-512.png',
  '/pwa/maskable-icon.svg',
  '/pwa/offline.html',
];

const isSameOrigin = (url) => url.origin === self.location.origin;
const isRuntimeAsset = (url) =>
  isSameOrigin(url) &&
  (url.pathname.startsWith('/assets/') ||
    url.pathname.startsWith('/brand-icons/') ||
    url.pathname.startsWith('/pwa/'));

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(APP_SHELL_CACHE).then(async (cache) => {
      await Promise.all(
        APP_SHELL_URLS.map(async (url) => {
          const response = await fetch(new self.Request(url, { cache: 'reload' }));
          if (!response.ok) throw new Error(`Unable to cache ${url}: ${response.status}`);
          await cache.put(url, response);
        })
      );
      await self.skipWaiting();
    })
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then(async (keys) => {
      const obsoleteCaches = keys.filter(
        (key) => key.startsWith('api-monitor-pwa-') && ![APP_SHELL_CACHE, RUNTIME_CACHE].includes(key)
      );
      const isUpgrade = obsoleteCaches.length > 0;

      await Promise.all(obsoleteCaches.map((key) => caches.delete(key)));

      await self.clients.claim();

      // The new worker must control each page before it asks that page to reload;
      // otherwise the previous worker could still serve an obsolete lazy chunk.
      if (isUpgrade) {
        const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
        windows.forEach((client) => client.postMessage({ type: 'APP_UPDATED', version: CACHE_VERSION }));
      }
    })
  );
});

self.addEventListener('message', (event) => {
  if (event.data?.type === 'SKIP_WAITING') {
    self.skipWaiting();
    return;
  }

  if (event.data?.type === 'GET_APP_VERSION') {
    event.source?.postMessage({ type: 'APP_VERSION', version: CACHE_VERSION });
  }
});

self.addEventListener('fetch', (event) => {
  const { request } = event;

  if (request.method !== 'GET') return;

  const url = new URL(request.url);

  if (!isSameOrigin(url)) return;

  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/v1') || url.pathname.startsWith('/sub/')) {
    return;
  }

  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request)
        .then((response) => {
          const copy = response.clone();
          caches.open(APP_SHELL_CACHE).then((cache) => cache.put('/index.html', copy));
          return response;
        })
        .catch(async () => {
          const cachedShell = await caches.match('/index.html');
          return cachedShell || caches.match('/pwa/offline.html');
        })
    );
    return;
  }

  if (isRuntimeAsset(url) || request.destination === 'style' || request.destination === 'script' || request.destination === 'image') {
    event.respondWith(
      fetch(request)
        .then((response) => {
          if (response && response.ok) {
            const copy = response.clone();
            event.waitUntil(caches.open(RUNTIME_CACHE).then((cache) => cache.put(request, copy)));
          }
          return response;
        })
        .catch(() => caches.match(request))
    );
  }
});
