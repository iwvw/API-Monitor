/* eslint-env serviceworker */
/* global self, caches */
/**
 * API Monitor - Service Worker
 * 提供 PWA 离线缓存支持
 */

const CACHE_NAME = 'api-monitor-v1';
const STATIC_CACHE = 'api-monitor-static-v1';

// 需要缓存的静态资源
const STATIC_ASSETS = [
  '/',
  '/logo.svg',
  '/pwa/icons/default-512.png',
  '/pwa/icons/music-512.png',
  '/pwa/icons/server-512.png',
  '/pwa/icons/totp-512.png',
];

// 安装事件 - 预缓存静态资源
self.addEventListener('install', event => {
  console.log('[SW] Installing...');
  event.waitUntil(
    caches.open(STATIC_CACHE).then(cache => {
      console.log('[SW] Pre-caching static assets');
      return cache.addAll(STATIC_ASSETS);
    })
  );
  // 立即激活新版本
  self.skipWaiting();
});

// 激活事件 - 清理旧缓存
self.addEventListener('activate', event => {
  console.log('[SW] Activating...');
  event.waitUntil(
    caches.keys().then(cacheNames => {
      return Promise.all(
        cacheNames.map(cacheName => {
          if (cacheName !== CACHE_NAME && cacheName !== STATIC_CACHE) {
            console.log('[SW] Deleting old cache:', cacheName);
            return caches.delete(cacheName);
          }
        })
      );
    })
  );
  // 接管所有页面
  self.clients.claim();
});

// 请求拦截 - Network First 策略（API 请求优先网络）
self.addEventListener('fetch', event => {
  const { request } = event;
  const url = new URL(request.url);

  // 跳过非同源请求
  if (url.origin !== location.origin) {
    return;
  }

  // API 请求：Network First
  if (url.pathname.startsWith('/api/')) {
    event.respondWith(
      fetch(request)
        .then(response => {
          // 仅缓存 GET 请求 (排除 206 Partial Content)
          if (request.method === 'GET' && response.status === 200) {
            const responseClone = response.clone();
            caches.open(CACHE_NAME).then(cache => {
              cache.put(request, responseClone);
            });
          }
          return response;
        })
        .catch(() => {
          // 网络失败时返回缓存
          return caches.match(request);
        })
    );
    return;
  }

  // 静态资源：Network First (为了更好地配合开发环境和 PWA 更新)
  event.respondWith(
    fetch(request)
      .then(response => {
        // 缓存新资源
        const responseClone = response.clone();
        caches.open(STATIC_CACHE).then(cache => {
          cache.put(request, responseClone);
        });
        return response;
      })
      .catch(() => {
        // 网络失败时使用缓存
        return caches.match(request);
      })
  );
});

// 推送通知支持（可选）
self.addEventListener('push', event => {
  if (event.data) {
    const data = event.data.json();
    self.registration.showNotification(data.title, {
      body: data.body,
      icon: '/pwa/icons/default-512.png',
      badge: '/logo.svg',
    });
  }
});
