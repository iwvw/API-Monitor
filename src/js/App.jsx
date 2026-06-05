import React, { useEffect } from 'react';
import useStore, { applyThemeMode } from './store.js';
import AuthPage from './pages/AuthPage.jsx';
import MainLayout from './components/MainLayout.jsx';

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
    // 身份校验中，保留 Loading 屏（由 main.jsx 进行后续淡出，这里仅占位）
    return null;
  }

  return isAuthenticated ? <MainLayout /> : <AuthPage />;
}

export default App;
