import React, { lazy, Suspense, useEffect, useState } from 'react';
import useStore, { applyThemeMode } from './store.js';
import AuthPage from './pages/AuthPage.jsx';
import MainLayout from './components/MainLayout.jsx';

const FileboxPage = lazy(() => import('./pages/FileboxPage.jsx'));
const PublicStatusPage = lazy(() => import('./pages/PublicStatusPage.jsx'));
const PublicServerStatusPage = lazy(() => import('./pages/PublicServerStatusPage.jsx'));

const isLocalHost = (host) => /^(localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$/i.test(host || '');

const getPublicStatusRouteMode = () => {
  if (typeof window === 'undefined') return false;
  const path = window.location.pathname.replace(/\/+$/, '') || '/';
  if (/^\/(?:status|u)\/[^/]+$/.test(path)) return 'slug';
  if (/^\/(?:servers|s)\/[^/]+$/.test(path)) return 'server-slug';
  if (path === '/' && !isLocalHost(window.location.host)) return 'domain';
  return false;
};

function App() {
  const { isAuthenticated, checkAuth, isCheckingAuth, themeMode } = useStore();
  const [domainStatusFallback, setDomainStatusFallback] = useState(false);
  const [domainServerFallback, setDomainServerFallback] = useState(false);
  const publicStatusRouteMode = getPublicStatusRouteMode();

  // 挂载时自动运行初始身份校验
  useEffect(() => {
    checkAuth();
  }, [checkAuth]);

  // 同步主题至 html class
  useEffect(() => {
    applyThemeMode(themeMode);
  }, [themeMode]);

  // 监听系统主题变化（仅在用户未锁定自定义主题时生效）
  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return;

    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    const handleChange = (e) => {
      const currentMode = useStore.getState().themeMode;
      if (currentMode !== 'auto') return;

      const newTheme = e.matches ? 'dark' : 'light';
      applyThemeMode('auto');
      useStore.setState({ theme: newTheme });
    };

    if (mediaQuery.addEventListener) {
      mediaQuery.addEventListener('change', handleChange);
    } else {
      mediaQuery.addListener(handleChange);
    }

    return () => {
      if (mediaQuery.removeEventListener) {
        mediaQuery.removeEventListener('change', handleChange);
      } else {
        mediaQuery.removeListener(handleChange);
      }
    };
  }, []);

  if (!isAuthenticated && window.location.pathname === '/filebox' && new URLSearchParams(window.location.search).has('void')) {
    return <div className="min-h-screen bg-kumo-canvas p-4 sm:p-8"><Suspense fallback={null}><FileboxPage publicVoidOnly /></Suspense></div>;
  }

  if (publicStatusRouteMode === 'server-slug') {
    return <Suspense fallback={null}><PublicServerStatusPage /></Suspense>;
  }

  if (publicStatusRouteMode === 'slug' || (publicStatusRouteMode === 'domain' && !domainStatusFallback)) {
    return (
      <Suspense fallback={null}>
        <PublicStatusPage
          domainOnly={publicStatusRouteMode === 'domain'}
          onDomainNotFound={() => setDomainStatusFallback(true)}
        />
      </Suspense>
    );
  }

  if (publicStatusRouteMode === 'domain' && domainStatusFallback && !domainServerFallback) {
    return (
      <Suspense fallback={null}>
        <PublicServerStatusPage
          domainOnly
          onDomainNotFound={() => setDomainServerFallback(true)}
        />
      </Suspense>
    );
  }

  if (isCheckingAuth) {
    return null;
  }

  return isAuthenticated ? <MainLayout /> : <AuthPage />;
}

export default App;
