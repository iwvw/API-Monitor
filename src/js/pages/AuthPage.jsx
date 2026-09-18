import React, { useEffect, useMemo, useState } from 'react';
import { Banner } from '@cloudflare/kumo/components/banner';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Loader } from '@cloudflare/kumo/components/loader';
import { GlobeMap } from '@cloudflare/kumo';
import useStore from '../store.js';
import {
  clearExplicitLogoutMarker,
  clearPendingAuthProvider,
  setPendingAuthProvider,
} from '../store.js';
import { useShallow } from 'zustand/react/shallow';
import { cx } from '../components/ui/AppPrimitives.jsx';
import { useCloudflareSpotlight } from '../hooks/useCloudflareSpotlight.js';
import { browserSupportsWebAuthn, getPasskeyAssertion } from '../modules/webauthn.js';
import {
  AlertTriangle,
  ArrowRight,
  ChevronLeft,
  GitHubBrand,
  RefreshCw,
  Key,
  LogIn,
  Shield,
} from '../components/IconsCore.jsx';

function prefersReducedMotion() {
  return (
    typeof window !== 'undefined' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  );
}

// 赤道发光通过 markers 交给 kumo 内部投影：光点钉在固定地理经度上，
// 由 GlobeMap 用真实旋转逐帧重投影，天然贴合球面、与自转完全同步，
// 背面自动隐藏、边缘淡出。CSS 用 fill 属性选择只给赤道点加光晕。
const GLOBE_DEFAULT_ROTATION = [-10, -20, -30];
const GLOBE_EQUATOR_SAMPLE_DEG = 4;

const EQUATOR_GLOW_MARKERS = Array.from(
  { length: Math.ceil(360 / GLOBE_EQUATOR_SAMPLE_DEG) },
  (_, index) => ({
    longitude: -180 + index * GLOBE_EQUATOR_SAMPLE_DEG,
    latitude: 0,
    name: '',
    color: 'var(--color-brand)',
    radius: 2,
  })
);

// AuthGlobeMarkers 拉取公开节点坐标，渲染登录页左侧的地球。
// 无坐标节点后端已过滤，这里只做字段映射；请求失败静默降级为无标记的地球。
function AuthGlobeMarkers({ isDarkMode }) {
  const [markers, setMarkers] = useState([]);

  useEffect(() => {
    let cancelled = false;
    fetch('/api/server/public/globe-nodes', { cache: 'no-store' })
      .then(response => response.json())
      .then(result => {
        if (cancelled) return;
        const nodes = result?.data?.nodes || [];
        setMarkers(
          nodes
            .filter(node => Number.isFinite(node.latitude) && Number.isFinite(node.longitude))
            .map(node => ({
              longitude: node.longitude,
              latitude: node.latitude,
              name: node.name,
              description: node.online ? '在线' : '离线',
              color: node.online ? 'var(--color-kumo-success)' : 'var(--text-color-kumo-inactive)',
            }))
        );
      })
      .catch(() => {
        if (!cancelled) setMarkers([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div className="auth-globe-map">
      <GlobeMap
        markers={[...EQUATOR_GLOW_MARKERS, ...markers]}
        defaultRotation={GLOBE_DEFAULT_ROTATION}
        autoRotate={!prefersReducedMotion()}
        autoRotateSpeed={5}
        landColor={
          isDarkMode ? 'var(--text-color-kumo-subtle)' : 'var(--text-color-kumo-inactive)'
        }
        landHatchSpacing={4}
        markerColor="var(--color-kumo-brand)"
        markerRadius={6}
        showGraticule
        showTooltip={false}
        draggable={false}
        isDarkMode={isDarkMode}
        className="auth-globe-canvas"
        aria-label="监控节点分布"
      />
    </div>
  );
}

function AuthBrandCanvas() {
  const theme = useStore(s => s.theme);

  return (
    <div className="auth-brand-pane-inner">
      <div className="auth-brand-lockup">
        <div className="auth-brand-hero-mark">
          <img src="/logo.svg" alt="" className="auth-brand-logo" />
        </div>
        <div className="auth-brand-copy">
          <div className="auth-brand-title">
            <span className="auth-brand-title-api">API</span>
            <span className="auth-brand-title-monitor">Monitor</span>
          </div>
        </div>
      </div>

      <div className="auth-monitor-visual">
        <AuthGlobeMarkers isDarkMode={theme === 'dark'} />
      </div>
    </div>
  );
}

function AuthShell({ title, onBack, notice, children }) {
  const surfaceRef = useCloudflareSpotlight();

  return (
    <main
      ref={surfaceRef}
      className="auth-shell cf-ai-background-surface min-h-dvh w-full text-kumo-default"
    >
      <div className="auth-shell-spotlight cf-ai-background pointer-events-none absolute inset-0" />
      <section className="auth-brand-pane">
        <AuthBrandCanvas />
      </section>

      <section className="auth-auth-pane">
        <div className="auth-mobile-brand" aria-hidden="true">
          <img src="/logo.svg" alt="" />
          <span className="auth-brand-title">
            <span className="auth-brand-title-api">API</span>
            <span className="auth-brand-title-monitor">Monitor</span>
          </span>
        </div>

        <div className="relative w-full max-w-sm">
          <div className="auth-login-panel">
            <div className="auth-login-head flex items-center gap-2">
              {onBack && (
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  shape="square"
                  onClick={onBack}
                  icon={<ChevronLeft className="size-4" />}
                  aria-label="返回"
                  title="返回修改密码"
                  className="shrink-0 -ml-1"
                />
              )}
              <h1 className="auth-login-title" aria-live="polite">
                <span key={title} className="auth-login-title-text">{title}</span>
              </h1>
            </div>
            {children}
          </div>
          {notice && (
            <div className="absolute top-full left-0 right-0 mt-3 z-10">
              {notice}
            </div>
          )}
        </div>
      </section>
    </main>
  );
}

function AuthErrorBanner({ message }) {
  if (!message) return null;

  const normalized = String(message || '').trim();
  const isWarning =
    normalized.includes('还剩') ||
    normalized.includes('尝试过多') ||
    normalized.includes('稍后再试');
  const title = normalized.includes('GitHub')
    ? 'GitHub 登录失败'
    : normalized.includes('通行密钥')
      ? '通行密钥登录失败'
      : normalized.includes('双因素') || normalized.includes('验证码')
        ? '验证码错误'
        : normalized.includes('密码')
          ? '密码错误'
          : isWarning
            ? '请稍后再试'
            : '登录失败';

  return (
    <div
      className={`w-full rounded-lg border p-3 text-left transition-all ${
        isWarning
          ? 'border-kumo-warning/35 bg-kumo-warning/10 text-kumo-warning'
          : 'border-kumo-danger/35 bg-kumo-danger/10 text-kumo-danger'
      }`}
    >
      <div className="flex items-center gap-2 mb-1.5">
        <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-current/15">
          <AlertTriangle className="size-3.5" />
        </span>
        <div className="text-xs font-semibold leading-none">{title}</div>
      </div>
      <div className="text-[11.5px] leading-relaxed opacity-90 break-all pl-7 font-mono">
        {normalized}
      </div>
    </div>
  );
}

function AuthStatusNotice({ statusKey, message }) {
  const statusMap = {
    setup: '正在初始化',
    github: '正在验证 GitHub',
    passkey: '通行密钥验证中',
    'github-2fa': '正在验证 GitHub',
  };

  const status = statusMap[statusKey];

  return (
    <div className="w-full text-left" aria-live="polite">
      {message ? (
        <AuthErrorBanner message={message} />
      ) : status ? (
        <div className="flex w-full items-center gap-2.5 rounded-lg border border-brand/35 bg-brand/10 p-3 text-brand">
          <Loader size={16} className="shrink-0 text-brand" />
          <div className="text-xs font-semibold leading-tight">{status}</div>
        </div>
      ) : null}
    </div>
  );
}

function AuthPage() {
  const {
    isDemoMode,
    loginRequire2FA,
    loginError,
    loginLoading,
    verifyPassword,
    loginPassword,
    setLoginPassword,
    loginTotpToken,
    setLoginTotpToken,
    cancelLogin2FA,
    showSetPasswordModal,
  } = useStore(
    useShallow(s => ({
      isDemoMode: s.isDemoMode,
      loginRequire2FA: s.loginRequire2FA,
      loginError: s.loginError,
      loginLoading: s.loginLoading,
      verifyPassword: s.verifyPassword,
      loginPassword: s.loginPassword,
      setLoginPassword: s.setLoginPassword,
      loginTotpToken: s.loginTotpToken,
      setLoginTotpToken: s.setLoginTotpToken,
      cancelLogin2FA: s.cancelLogin2FA,
      showSetPasswordModal: s.showSetPasswordModal,
    }))
  );

  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [setupError, setSetupError] = useState('');
  const [setupLoading, setSetupLoading] = useState(false);
  const [loginOptions, setLoginOptions] = useState({
    githubEnabled: false,
    webauthnEnabled: false,
  });
  const [activeAction, setActiveAction] = useState('');
  const [githubFlowId, setGitHubFlowId] = useState('');
  const [pendingButton, setPendingButton] = useState('');
  const supportsPasskey = browserSupportsWebAuthn();

  const beginButtonMotion = async (buttonId, delayMs = 0) => {
    setPendingButton(buttonId);
    if (delayMs > 0) {
      await new Promise(resolve => window.setTimeout(resolve, delayMs));
    }
  };

  useEffect(() => {
    const query = new URLSearchParams(window.location.search);
    const authError = query.get('authError');
    const flowId = query.get('githubFlow');
    if (flowId) {
      setGitHubFlowId(flowId);
      clearPendingAuthProvider();
    } else {
      setGitHubFlowId('');
    }
    if (authError) {
      clearPendingAuthProvider();
      useStore.setState({ loginError: authError });
      query.delete('authError');
      const next = query.toString();
      window.history.replaceState(
        {},
        document.title,
        `${window.location.pathname}${next ? `?${next}` : ''}`
      );
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    fetch('/api/auth/login-options', { cache: 'no-store' })
      .then(response => response.json())
      .then(result => {
        if (cancelled) return;
        const payload = result.data || result;
        setLoginOptions({
          githubEnabled: !!payload.github?.enabled,
          webauthnEnabled: !!payload.webauthn?.enabled,
        });
      })
      .catch(() => {
        if (!cancelled) {
          setLoginOptions({ githubEnabled: false, webauthnEnabled: false });
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!loginLoading && !activeAction && !setupLoading) {
      setPendingButton('');
    }
  }, [activeAction, loginLoading, setupLoading]);

  const requiresSecondStep = useMemo(
    () => loginRequire2FA || Boolean(githubFlowId),
    [githubFlowId, loginRequire2FA]
  );
  const busyState = useMemo(() => {
    if (setupLoading) return 'setup';
    if (activeAction) return activeAction;
    if (loginLoading)
      return requiresSecondStep ? (githubFlowId ? 'github-2fa' : 'password-2fa') : 'password';
    return '';
  }, [activeAction, githubFlowId, loginLoading, requiresSecondStep, setupLoading]);
  const buttonsLocked = loginLoading || Boolean(activeAction) || setupLoading;
  const getButtonClassName = (buttonId, loading = false, className = '') =>
    cx(
      'auth-login-button',
      (pendingButton === buttonId || loading) && 'auth-login-button--pending',
      className
    );
  const clearLoginError = () => {
    if (loginError) {
      useStore.setState({ loginError: '' });
    }
  };

  const handle2FAInput = event => {
    const value = event.target.value.replace(/\D/g, '').slice(0, 6);
    clearLoginError();
    setLoginTotpToken(value);
    if (value.length === 6) {
      if (githubFlowId) {
        void completeGitHub2FA(value, true);
      } else {
        verifyPassword(true);
      }
    }
  };

  const handleSetupPassword = async event => {
    event.preventDefault();
    setSetupError('');

    if (!newPassword || newPassword.length < 6) {
      setSetupError('密码长度至少 6 位');
      return;
    }

    if (newPassword !== confirmPassword) {
      setSetupError('两次输入的密码不一致');
      return;
    }

    setSetupLoading(true);
    try {
      const response = await fetch('/api/auth/set-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: newPassword }),
      });
      const result = await response.json();

      if (!result.success) {
        setSetupError(result.error || '设置密码失败');
        return;
      }

      setLoginPassword(newPassword);
      await verifyPassword(false);
    } catch (error) {
      setSetupError('设置失败，请检查网络');
    } finally {
      setSetupLoading(false);
    }
  };

  const handleLogin = async event => {
    event.preventDefault();
    if (buttonsLocked) return;
    if (githubFlowId) {
      await beginButtonMotion('2fa');
      completeGitHub2FA();
      return;
    }
    await beginButtonMotion(requiresSecondStep ? '2fa' : 'password');
    verifyPassword();
  };

  const handleGitHubLogin = async () => {
    if (buttonsLocked) return;
    setActiveAction('github');
    useStore.setState({ loginError: '' });
    clearExplicitLogoutMarker();
    setPendingAuthProvider('github');
    await beginButtonMotion('github', 120);
    window.location.href = '/api/auth/github/start';
  };

  const handlePasskeyLogin = async () => {
    if (buttonsLocked) return;
    await beginButtonMotion('passkey', 90);
    setActiveAction('passkey');
    useStore.setState({ loginError: '' });
    try {
      const beginResponse = await fetch('/api/auth/webauthn/login/begin', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      });
      const beginResult = await beginResponse.json();
      if (!beginResponse.ok || beginResult.success === false) {
        throw new Error(beginResult.error || '创建通行密钥登录挑战失败');
      }
      const credential = await getPasskeyAssertion(beginResult.options);
      const finishResponse = await fetch('/api/auth/webauthn/login/finish', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          flowId: beginResult.flowId,
          credential,
        }),
      });
      const finishResult = await finishResponse.json();
      if (!finishResponse.ok || finishResult.success === false) {
        throw new Error(finishResult.error || '通行密钥登录失败');
      }
      clearExplicitLogoutMarker();
      clearPendingAuthProvider();
      useStore.setState({
        isAuthenticated: true,
        showLoginModal: false,
        loginPassword: '',
        loginRequire2FA: false,
        loginTotpToken: '',
        loginError: '',
      });
    } catch (error) {
      const message =
        error?.name === 'NotAllowedError'
          ? '通行密钥操作已取消或未通过系统验证'
          : error.message || '通行密钥登录失败';
      useStore.setState({ loginError: message });
    } finally {
      setActiveAction('');
    }
  };

  const completeGitHub2FA = async (overrideToken, silent = false) => {
    setActiveAction('github-2fa');
    try {
      const response = await fetch('/api/auth/github/2fa', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          flowId: githubFlowId,
          totpToken: overrideToken || loginTotpToken,
        }),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) {
        throw new Error(result.error || 'GitHub 登录二次验证失败');
      }
      clearExplicitLogoutMarker();
      clearPendingAuthProvider();
      const query = new URLSearchParams(window.location.search);
      query.delete('githubFlow');
      query.delete('provider');
      window.history.replaceState(
        {},
        document.title,
        `${window.location.pathname}${query.toString() ? `?${query.toString()}` : ''}`
      );
      setGitHubFlowId('');
      useStore.setState({
        isAuthenticated: true,
        showLoginModal: false,
        loginPassword: '',
        loginRequire2FA: false,
        loginTotpToken: '',
        loginError: '',
      });
    } catch (error) {
      if (!silent) {
        useStore.setState({ loginError: error.message || 'GitHub 登录二次验证失败' });
      }
    } finally {
      setActiveAction('');
    }
  };

  const cancelGitHubFlow = () => {
    clearPendingAuthProvider();
    const query = new URLSearchParams(window.location.search);
    query.delete('githubFlow');
    query.delete('provider');
    window.history.replaceState(
      {},
      document.title,
      `${window.location.pathname}${query.toString() ? `?${query.toString()}` : ''}`
    );
    setGitHubFlowId('');
    setLoginTotpToken('');
    useStore.setState({ loginError: '' });
  };

  const handleReturnToPassword = event => {
    event.preventDefault();
    event.stopPropagation();
    setPendingButton('');
    setActiveAction('');
    if (githubFlowId) {
      cancelGitHubFlow();
      return;
    }
    cancelLogin2FA();
  };

  if (showSetPasswordModal) {
    return (
      <AuthShell title="设置管理员密码">
        <form onSubmit={handleSetupPassword} className="auth-login-form space-y-4">
          <Input
            size="base"
            type="password"
            label="新密码"
            value={newPassword}
            onChange={event => {
              setSetupError('');
              setNewPassword(event.target.value);
            }}
            autoComplete="new-password"
            spellCheck={false}
            className="auth-login-input w-full"
            autoFocus
          />

          <Input
            size="base"
            type="password"
            label="确认密码"
            placeholder="再次输入密码"
            value={confirmPassword}
            onChange={event => {
              setSetupError('');
              setConfirmPassword(event.target.value);
            }}
            autoComplete="new-password"
            spellCheck={false}
            className="auth-login-input w-full"
          />

          <Button
            type="submit"
            variant="primary"
            size="base"
            loading={setupLoading}
            icon={!setupLoading ? <ArrowRight className="size-3.5" /> : undefined}
            className={getButtonClassName(
              'setup',
              setupLoading,
              'auth-login-button--primary w-full justify-center'
            )}
          >
            {setupLoading ? '处理中...' : '保存并进入'}
          </Button>
        </form>
      </AuthShell>
    );
  }

  const title = isDemoMode ? '演示模式' : requiresSecondStep ? '双因素验证' : '登录';

  return (
    <AuthShell
      title={title}
      notice={<AuthStatusNotice statusKey={busyState} message={loginError} />}
    >
      <form onSubmit={handleLogin} className="auth-login-form space-y-3">
        {isDemoMode && (
          <Banner
            variant="secondary"
            icon={<Shield className="size-4" />}
            title="演示环境"
            className="rounded-md px-3 py-2 text-xs"
          />
        )}

        {/* 保持同一个输入控件实例，让密码框在原位切换成验证码框并保留焦点。 */}
        <Input
          size="base"
          type={requiresSecondStep ? 'text' : 'password'}
          inputMode={requiresSecondStep ? 'numeric' : undefined}
          aria-label={requiresSecondStep ? '双因素验证码' : '管理员密码'}
          placeholder={requiresSecondStep ? '000000' : '密码'}
          maxLength={requiresSecondStep ? 6 : undefined}
          value={requiresSecondStep ? loginTotpToken : loginPassword}
          onChange={event => {
            if (requiresSecondStep) {
              handle2FAInput(event);
            } else {
              clearLoginError();
              setLoginPassword(event.target.value);
            }
          }}
          autoComplete={requiresSecondStep ? 'one-time-code' : 'current-password'}
          spellCheck={false}
          className={cx(
            'auth-login-input w-full text-center transition-all duration-200',
            requiresSecondStep && 'font-mono text-sm',
            loginError && 'auth-login-input--error'
          )}
          autoFocus
        />

        {/* 保持同一个按钮实例，让登录按钮在原位切换成返回按钮。 */}
        <Button
          type={requiresSecondStep ? 'button' : 'submit'}
          variant={requiresSecondStep ? 'secondary' : 'primary'}
          size="sm"
          loading={!requiresSecondStep && loginLoading}
          disabled={!requiresSecondStep && Boolean(activeAction)}
          onClick={requiresSecondStep ? handleReturnToPassword : undefined}
          icon={
            requiresSecondStep
              ? <ChevronLeft className="size-3.5" />
              : !loginLoading
                ? <LogIn className="size-3.5" />
                : undefined
          }
          className={getButtonClassName(
            requiresSecondStep ? '2fa-back' : 'password',
            !requiresSecondStep && loginLoading,
            cx(
              requiresSecondStep
                ? 'auth-login-button--secondary'
                : 'auth-login-button--primary',
              'w-full justify-center transition-all duration-200'
            )
          )}
          aria-label={requiresSecondStep ? '返回密码' : undefined}
        >
          {requiresSecondStep
            ? '返回密码'
            : loginLoading
              ? '处理中...'
              : isDemoMode
                ? '进入演示环境'
                : '登录'}
        </Button>

        {!isDemoMode &&
          (loginOptions.githubEnabled || (loginOptions.webauthnEnabled && supportsPasskey)) && (
            <div className="auth-login-alternatives pt-0.5">
              <div className="auth-login-divider mb-2.5">
                <span>其他方式</span>
              </div>
              <div
                className={cx(
                  'grid gap-2',
                  loginOptions.githubEnabled &&
                    loginOptions.webauthnEnabled &&
                    supportsPasskey &&
                    'sm:grid-cols-2'
                )}
              >
                {loginOptions.githubEnabled && (
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    onClick={handleGitHubLogin}
                    loading={activeAction === 'github'}
                    disabled={loginLoading || Boolean(activeAction && activeAction !== 'github')}
                    icon={<GitHubBrand className="size-3.5" />}
                    className={getButtonClassName(
                      'github',
                      activeAction === 'github',
                      'auth-login-button--secondary w-full justify-center'
                    )}
                  >
                    {activeAction === 'github' ? '处理中...' : 'GitHub'}
                  </Button>
                )}
                {loginOptions.webauthnEnabled && supportsPasskey && (
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    onClick={handlePasskeyLogin}
                    loading={activeAction === 'passkey'}
                    disabled={loginLoading || Boolean(activeAction && activeAction !== 'passkey')}
                    icon={<Key className="size-3.5" />}
                    className={getButtonClassName(
                      'passkey',
                      activeAction === 'passkey',
                      'auth-login-button--secondary w-full justify-center'
                    )}
                  >
                    {activeAction === 'passkey' ? '处理中...' : '通行密钥'}
                  </Button>
                )}
              </div>
            </div>
          )}
      </form>
    </AuthShell>
  );
}

export default AuthPage;
