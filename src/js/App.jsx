import React, { lazy, Suspense, useEffect } from 'react';
import useStore, { applyThemeMode } from './store.js';
import AuthPage from './pages/AuthPage.jsx';
import MainLayout from './components/MainLayout.jsx';

const FileboxPage = lazy(() => import('./pages/FileboxPage.jsx'));

function App() {
  const { isAuthenticated, checkAuth, isCheckingAuth, themeMode } = useStore();

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

  if (isCheckingAuth) {
    return null;
  }

  if (!isAuthenticated && window.location.pathname === '/filebox' && new URLSearchParams(window.location.search).has('void')) {
    return <div className="min-h-screen bg-kumo-canvas p-4 sm:p-8"><Suspense fallback={null}><FileboxPage publicVoidOnly /></Suspense></div>;
  }

  return isAuthenticated ? <MainLayout /> : <AuthPage />;
}

export default App;
