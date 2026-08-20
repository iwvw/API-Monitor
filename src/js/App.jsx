import React, { lazy, Suspense, useEffect, useState } from 'react';
import { Loader } from '@cloudflare/kumo';
import useStore, { applyThemeMode, applyUIFont, applyUIFontSize, getPendingAuthProvider } from './store.js';
import AuthPage from './pages/AuthPage.jsx';
import { GitHubBrand, Shield } from './components/IconsCore.jsx';

// MainLayout 携带侧边栏、图标库等大量依赖，懒加载让登录页与公开
// 状态页不必下载主应用的 JS。
const MainLayout = lazy(() => import('./components/MainLayout.jsx'));

const PublicSharePage = lazy(() => import('./pages/PublicSharePage.jsx'));
const PublicM365RegisterPage = lazy(() => import('./pages/PublicM365RegisterPage.jsx'));
const PublicStatusPage = lazy(() => import('./pages/PublicStatusPage.jsx'));
const PublicServerStatusPage = lazy(() => import('./pages/PublicServerStatusPage.jsx'));
const PublicGitHubPage = lazy(() => import('./pages/PublicGitHubPage.jsx'));
const PublicSubscriptionInfoPage = lazy(() => import('./pages/PublicSubscriptionInfoPage.jsx'));
const VoidRoomPage = lazy(() => import('./pages/VoidRoomPage.jsx'));
const RemoteDesktopPage = lazy(() => import('./pages/RemoteDesktopPage.jsx'));
const PublicPromptPage = lazy(() => import('./pages/PublicPromptPage.jsx'));

const isLocalHost = (host) => /^(localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$/i.test(host || '');

const isDockerMockPreviewRoute = () => (
  typeof window !== 'undefined'
  && import.meta.env?.DEV
  && isLocalHost(window.location.host)
  && new URLSearchParams(window.location.search).has('mockDocker')
);

const getPublicStatusRouteMode = () => {
  if (typeof window === 'undefined') return false;
  const path = window.location.pathname.replace(/\/+$/, '') || '/';
  if (/^\/sub\/[^/]+$/.test(path)) return 'subscription-info';
  if (/^\/(?:status|u)\/[^/]+$/.test(path)) return 'slug';
  if (/^\/(?:servers|s)\/[^/]+$/.test(path)) return 'server-slug';
  if (/^\/(?:github|gh)\/[^/]+$/.test(path)) return 'github-slug';
  if (path === '/' && !isLocalHost(window.location.host)) return 'domain';
  return false;
};

const getPublicFileboxRouteMode = () => {
  if (typeof window === 'undefined') return false;
  const path = window.location.pathname.replace(/\/+$/, '') || '/';
  if (/^\/share\/[^/]+$/.test(path)) return 'share';
  if (/^\/void\/[^/]+$/.test(path)) return 'void';
  return false;
};

const isPublicM365RegisterRoute = () => {
  if (typeof window === 'undefined') return false;
  const path = window.location.pathname.replace(/\/+$/, '') || '/';
  return path === '/m365/register';
};

const getCheckingAuthProvider = () => {
  if (typeof window === 'undefined') return '';
  const fromStorage = getPendingAuthProvider();
  if (fromStorage) return fromStorage;
  return String(new URLSearchParams(window.location.search).get('provider') || '').trim();
};

const normalizeLegacyDashboardPath = () => {
  if (typeof window === 'undefined') return;
  const normalized = window.location.pathname.replace(/\/+$/, '') || '/';
  if (normalized !== '/dashboard') return;
  window.history.replaceState(window.history.state, '', `/${window.location.search}${window.location.hash}`);
};

function AuthTransitionScreen() {
  const provider = getCheckingAuthProvider();
  const isGitHub = provider === 'github';

  return (
    <main className="relative flex min-h-dvh w-screen items-center justify-center overflow-hidden bg-kumo-canvas px-4 py-8 text-kumo-default">
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top,rgba(64,123,255,0.12),transparent_42%),radial-gradient(circle_at_bottom_right,rgba(15,23,42,0.08),transparent_38%)]"
      />
      <div className="relative z-10 flex w-full max-w-sm flex-col items-center gap-5 rounded-2xl border border-kumo-line bg-kumo-base/95 px-8 py-10 text-center shadow-none">
        <div className="flex size-14 items-center justify-center rounded-2xl border border-kumo-line bg-kumo-recessed text-brand">
          {isGitHub ? <GitHubBrand className="size-7" /> : <Shield className="size-7" />}
        </div>
        <div className="space-y-1">
          <div className="text-base font-semibold text-kumo-strong">
            {isGitHub ? '正在验证 GitHub' : '正在登录'}
          </div>
        </div>
        <Loader size={32} />
      </div>
    </main>
  );
}

function App() {
  const { isAuthenticated, checkAuth, isCheckingAuth, themeMode } = useStore();
  const [domainStatusRoute, setDomainStatusRoute] = useState(null);
  const publicStatusRouteMode = getPublicStatusRouteMode();
  const publicFileboxRouteMode = getPublicFileboxRouteMode();
  const publicM365RegisterRoute = isPublicM365RegisterRoute();
  const dockerMockPreview = isDockerMockPreviewRoute();
  const remoteDesktopRoute = /^\/remote-desktop\/[^/]+$/.test(window.location.pathname);
	const publicPromptRoute = /^\/p\/[^/]+$/.test(window.location.pathname);

  // 挂载时自动运行初始身份校验
  useEffect(() => {
    if (dockerMockPreview) {
      useStore.setState({
        isAuthenticated: true,
        isCheckingAuth: false,
        showLoginModal: false,
        showSetPasswordModal: false,
        userSettingsLoaded: true,
      });
      return;
    }
    checkAuth();
  }, [checkAuth, dockerMockPreview]);

  useEffect(() => {
    normalizeLegacyDashboardPath();
  }, []);

  // 同步主题至 html class
  useEffect(() => {
    applyThemeMode(themeMode);
  }, [themeMode]);

  // 未认证页面（登录/公开页）同样应用本地字体设置，保证全站字体一致；
  // 登录后 applyUserSettings 会再次应用后端保存的字体（来源一致）。
  useEffect(() => {
    let stored = null;
    try {
      stored = localStorage.getItem('app_ui_font');
    } catch (e) {
      /* ignore */
    }
    if (stored) applyUIFont(stored);
  }, []);

  // 未认证页面同样应用本地字号缩放；登录后 applyUserSettings 不会覆盖该偏好。
  useEffect(() => {
    let stored = null;
    try {
      stored = localStorage.getItem('app_ui_font_size');
    } catch (e) {
      /* ignore */
    }
    if (stored) applyUIFontSize(stored);
  }, []);

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

  if (window.location.pathname === '/filebox' && new URLSearchParams(window.location.search).has('void')) {
    const room = new URLSearchParams(window.location.search).get('void');
    window.location.replace(`/void/${encodeURIComponent(room || '')}`);
    return null;
  }

  if (publicFileboxRouteMode === 'share') {
    return <div className="@container"><Suspense fallback={null}><PublicSharePage /></Suspense></div>;
  }

  if (publicFileboxRouteMode === 'void') {
    return <div className="@container"><Suspense fallback={null}><VoidRoomPage /></Suspense></div>;
  }

  if (publicM365RegisterRoute) {
    return <div className="@container"><Suspense fallback={null}><PublicM365RegisterPage /></Suspense></div>;
  }

  if (publicStatusRouteMode === 'server-slug') {
    return <div className="@container"><Suspense fallback={null}><PublicServerStatusPage /></Suspense></div>;
  }

  if (publicStatusRouteMode === 'github-slug') {
    return <div className="@container"><Suspense fallback={null}><PublicGitHubPage /></Suspense></div>;
  }

  if (publicStatusRouteMode === 'subscription-info') {
    return <div className="@container"><Suspense fallback={null}><PublicSubscriptionInfoPage /></Suspense></div>;
  }

  if (publicStatusRouteMode === 'slug') {
    return (
      <div className="@container">
        <Suspense fallback={null}>
          <PublicStatusPage />
        </Suspense>
      </div>
    );
  }

	if (publicPromptRoute) {
		return <div className="@container"><Suspense fallback={null}><PublicPromptPage /></Suspense></div>;
	}

  if (isCheckingAuth) {
    return getCheckingAuthProvider() ? <AuthTransitionScreen /> : null;
  }

  if (isAuthenticated || dockerMockPreview) {
    if (remoteDesktopRoute) {
      return <div className="@container"><Suspense fallback={null}><RemoteDesktopPage /></Suspense></div>;
    }
    return <Suspense fallback={null}><MainLayout /></Suspense>;
  }

  if (publicStatusRouteMode === 'domain') {
    return (
      <DomainPublicStatusResolver
        route={domainStatusRoute}
        onRouteChange={setDomainStatusRoute}
      />
    );
  }

  return <div className="@container"><AuthPage /></div>;
}

function DomainPublicStatusResolver({ route, onRouteChange }) {
  useEffect(() => {
    let cancelled = false;

    const resolve = async () => {
      onRouteChange(null);
      const domain = window.location.host;
      const uptimeUrl = `/api/uptime/public/status-page-by-domain?domain=${encodeURIComponent(domain)}`;
      const serverUrl = `/api/server/public/status-page-by-domain?domain=${encodeURIComponent(domain)}`;
      const githubUrl = `/api/github/public/page-by-domain?domain=${encodeURIComponent(domain)}`;

      try {
        const uptimeResponse = await fetch(uptimeUrl, { cache: 'no-store' });
        const uptimeBody = uptimeResponse.ok ? await uptimeResponse.json().catch(() => null) : null;
        if (!cancelled && uptimeBody?.data?.found) {
          onRouteChange('uptime');
          return;
        }
      } catch {
        // Fall through to server status page probing.
      }

      try {
        const serverResponse = await fetch(serverUrl, { cache: 'no-store' });
        const serverBody = serverResponse.ok ? await serverResponse.json().catch(() => null) : null;
        if (!cancelled && serverBody?.data?.found) {
          onRouteChange('server');
          return;
        }
      } catch {
        // Fall through to GitHub public page probing.
      }

      try {
        const githubResponse = await fetch(githubUrl, { cache: 'no-store' });
        const githubBody = githubResponse.ok ? await githubResponse.json().catch(() => null) : null;
        if (!cancelled && githubBody?.data?.found) {
          onRouteChange('github');
          return;
        }
      } catch {
        // Fall through to login.
      }

      if (!cancelled) onRouteChange('none');
    };

    resolve();
    return () => {
      cancelled = true;
    };
  }, [onRouteChange]);

  if (route === 'uptime') {
    return (
      <div className="@container">
        <Suspense fallback={null}>
          <PublicStatusPage domainOnly />
        </Suspense>
      </div>
    );
  }

  if (route === 'server') {
    return (
      <div className="@container">
        <Suspense fallback={null}>
          <PublicServerStatusPage domainOnly />
        </Suspense>
      </div>
    );
  }

  if (route === 'github') {
    return (
      <div className="@container">
        <Suspense fallback={null}>
          <PublicGitHubPage domainOnly />
        </Suspense>
      </div>
    );
  }

  if (route === null) {
    return null;
  }

  return <div className="@container"><AuthPage /></div>;
}

export default App;
