import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Banner } from '@cloudflare/kumo/components/banner';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { LayerCard } from '@cloudflare/kumo/components/layer-card';
import useStore from '../store.js';
import {
  AlertTriangle,
  ArrowRight,
  ChevronLeft,
  Key,
  LogIn,
  Rocket,
  Shield,
} from '../components/Icons.jsx';

const AUTH_FEATURES = [
  '统一监控入口',
  '会话自动校验',
  '双因素验证支持',
];

let authParticlesEnginePromise = null;

const getCssColor = (name, fallback) => {
  if (typeof window === 'undefined') return fallback;
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
};

const createAuthParticleOptions = () => {
  const brand = getCssColor('--color-kumo-brand', '#dc7d40');
  const info = getCssColor('--color-kumo-info', '#4d8df7');
  const line = getCssColor('--color-kumo-line', '#6b7280');
  const subtle = getCssColor('--text-color-kumo-subtle', '#9ca3af');
  const isSmallViewport = typeof window !== 'undefined' && window.innerWidth < 640;
  const isDarkMode =
    typeof document !== 'undefined' && document.documentElement.dataset.mode === 'dark';
  const particleNeutral = isDarkMode ? subtle : line;
  const linkOpacity = isDarkMode ? 0.36 : 0.24;
  const particleOpacity = isDarkMode
    ? { min: 0.34, max: 0.72 }
    : { min: 0.24, max: 0.58 };
  const particleSize = isDarkMode ? { min: 1, max: 2.9 } : { min: 0.9, max: 2.55 };

  return {
    autoPlay: true,
    background: { color: { value: 'transparent' } },
    clear: true,
    detectRetina: true,
    fpsLimit: 30,
    fullScreen: { enable: false },
    pauseOnBlur: true,
    pauseOnOutsideViewport: true,
    smooth: true,
    zLayers: 3,
    interactivity: {
      detectsOn: 'canvas',
      events: {
        onHover: {
          enable: true,
          mode: ['grab', 'repulse', 'bubble', 'parallax'],
        },
        resize: {
          enable: true,
        },
      },
      modes: {
        bubble: {
          distance: 95,
          duration: 0.35,
          opacity: 0.62,
          size: 3.2,
        },
        grab: {
          distance: isSmallViewport ? 92 : 150,
          links: {
            blink: false,
            consent: false,
            opacity: isDarkMode ? 0.58 : 0.48,
          },
        },
        parallax: {
          force: isSmallViewport ? 18 : 28,
          smooth: 18,
        },
        repulse: {
          distance: isSmallViewport ? 54 : 82,
          duration: 0.35,
          factor: 42,
          speed: 0.72,
        },
      },
    },
    particles: {
      color: {
        value: [brand, info, particleNeutral],
      },
      links: {
        blink: false,
        color: {
          value: brand,
        },
        consent: false,
        distance: isSmallViewport ? 112 : 156,
        enable: true,
        frequency: 1,
        opacity: linkOpacity,
        shadow: {
          blur: isDarkMode ? 14 : 10,
          color: {
            value: brand,
          },
          enable: true,
        },
        width: isDarkMode ? 1.15 : 1,
      },
      move: {
        direction: 'none',
        enable: true,
        outModes: {
          default: 'bounce',
        },
        random: true,
        speed: {
          min: 0.12,
          max: 0.46,
        },
        straight: false,
      },
      number: {
        density: {
          enable: true,
          height: 620,
          width: 960,
        },
        limit: {
          value: isSmallViewport ? 42 : 82,
        },
        value: isSmallViewport ? 40 : 78,
      },
      opacity: {
        value: particleOpacity,
        animation: {
          enable: true,
          speed: 0.32,
          sync: false,
        },
      },
      shape: {
        type: 'circle',
      },
      size: {
        value: particleSize,
        animation: {
          enable: true,
          speed: 0.8,
          sync: false,
        },
      },
      twinkle: {
        links: {
          color: {
            value: info,
          },
          enable: true,
          frequency: isDarkMode ? 0.12 : 0.09,
          opacity: {
            min: isDarkMode ? 0.46 : 0.36,
            max: isDarkMode ? 0.82 : 0.68,
          },
        },
      },
      zIndex: {
        value: {
          min: 0,
          max: 100,
        },
        opacityRate: 0.42,
        sizeRate: 0.78,
        velocityRate: 0.55,
      },
    },
  };
};

const loadAuthParticlesEngine = async () => {
  if (!authParticlesEnginePromise) {
    authParticlesEnginePromise = Promise.all([
      import('@tsparticles/engine'),
      import('@tsparticles/slim'),
    ]).then(async ([engineModule, slimModule]) => {
      await slimModule.loadSlim(engineModule.tsParticles);
      return engineModule.tsParticles;
    });
  }

  return authParticlesEnginePromise;
};

function usePrefersReducedMotion() {
  const [prefersReducedMotion, setPrefersReducedMotion] = useState(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return false;
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  });

  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return undefined;

    const mediaQuery = window.matchMedia('(prefers-reduced-motion: reduce)');
    const handleChange = () => setPrefersReducedMotion(mediaQuery.matches);
    handleChange();
    mediaQuery.addEventListener('change', handleChange);

    return () => mediaQuery.removeEventListener('change', handleChange);
  }, []);

  return prefersReducedMotion;
}

function AuthAmbientBackground() {
  const layerRef = useRef(null);
  const containerRef = useRef(null);
  const prefersReducedMotion = usePrefersReducedMotion();
  const particlesId = useMemo(
    () => `auth-particles-${Math.random().toString(36).slice(2, 10)}`,
    []
  );

  useEffect(() => {
    const layer = layerRef.current;
    if (prefersReducedMotion || !layer || typeof window === 'undefined') return undefined;

    let animationFrame = 0;
    const setCursorLight = (clientX, clientY) => {
      if (animationFrame) window.cancelAnimationFrame(animationFrame);

      animationFrame = window.requestAnimationFrame(() => {
        layer.style.setProperty('--auth-cursor-x', `${clientX}px`);
        layer.style.setProperty('--auth-cursor-y', `${clientY}px`);
        layer.style.setProperty('--auth-cursor-opacity', '1');
        animationFrame = 0;
      });
    };
    const hideCursorLight = () => {
      layer.style.setProperty('--auth-cursor-opacity', '0');
    };
    const handlePointerMove = (event) => setCursorLight(event.clientX, event.clientY);

    window.addEventListener('pointermove', handlePointerMove, { passive: true });
    window.addEventListener('blur', hideCursorLight);
    document.documentElement.addEventListener('pointerleave', hideCursorLight);

    return () => {
      if (animationFrame) window.cancelAnimationFrame(animationFrame);
      window.removeEventListener('pointermove', handlePointerMove);
      window.removeEventListener('blur', hideCursorLight);
      document.documentElement.removeEventListener('pointerleave', hideCursorLight);
    };
  }, [prefersReducedMotion]);

  useEffect(() => {
    if (prefersReducedMotion || !containerRef.current) return undefined;

    let cancelled = false;
    let particlesContainer = null;

    loadAuthParticlesEngine()
      .then(async (tsParticles) => {
        if (cancelled) return;

        particlesContainer = await tsParticles.load({
          id: particlesId,
          options: createAuthParticleOptions(),
        });

        if (cancelled) {
          particlesContainer?.destroy();
        }
      })
      .catch((error) => {
        console.error('Failed to load auth particles:', error);
      });

    return () => {
      cancelled = true;
      particlesContainer?.destroy();
    };
  }, [particlesId, prefersReducedMotion]);

  return (
    <div ref={layerRef} className="auth-ambient-layer" aria-hidden="true">
      <div className="auth-ambient-grid" />
      <div ref={containerRef} id={particlesId} className="auth-particles-canvas" />
      <div className="auth-cursor-light" />
    </div>
  );
}

function AuthShell({ mode, title, description, children }) {
  const modeLabel = mode === 'setup' ? '初始化' : mode === '2fa' ? '二次验证' : '安全登录';

  return (
    <main className="auth-ambient-root relative isolate flex min-h-dvh w-screen overflow-hidden bg-kumo-canvas text-kumo-default">
      <AuthAmbientBackground />

      <section className="relative z-10 hidden w-[380px] shrink-0 flex-col justify-between border-r border-kumo-line bg-kumo-base/95 px-8 py-7 backdrop-blur-sm lg:flex">
        <div className="flex items-center gap-3">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-kumo-line bg-kumo-recessed">
            <img src="/logo.svg" alt="" className="size-5 object-contain" />
          </span>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-kumo-strong">API Monitor</div>
            <div className="text-[11px] text-kumo-subtle">监控控制台</div>
          </div>
        </div>

        <div className="space-y-4">
          <div className="inline-flex h-6.5 items-center rounded-md border border-kumo-line bg-kumo-recessed px-2 text-xs font-medium text-kumo-subtle">
            {modeLabel}
          </div>
          <div className="space-y-2">
            <h1 className="text-2xl font-semibold leading-snug text-kumo-strong">进入监控面板</h1>
            <p className="max-w-[280px] text-sm leading-relaxed text-kumo-subtle">
              管理 API、主机、DNS、PaaS 与告警状态。登录后即可进入统一工作台。
            </p>
          </div>
        </div>

        <div className="space-y-2 border-t border-kumo-line pt-4">
          {AUTH_FEATURES.map((item) => (
            <div key={item} className="flex items-center gap-2 text-xs text-kumo-subtle">
              <span className="size-1.5 rounded-full bg-kumo-brand" />
              <span>{item}</span>
            </div>
          ))}
        </div>
      </section>

      <section className="relative z-10 flex min-w-0 flex-1 items-center justify-center px-4 py-8 sm:px-6">
        <div className="w-full max-w-[400px]">
          <div className="mb-5 flex items-center justify-start gap-3 lg:hidden">
            <span className="flex size-10 shrink-0 items-center justify-center app-card">
              <img src="/logo.svg" alt="" className="size-6 object-contain" />
            </span>
            <div className="min-w-0">
              <div className="text-base font-semibold leading-tight text-kumo-strong">API Monitor</div>
              <div className="text-xs leading-tight text-kumo-subtle">{modeLabel}</div>
            </div>
          </div>

          <LayerCard className="w-full app-card/95 p-5 backdrop-blur-sm sm:p-6">
            <div className="mb-5 flex items-start justify-between gap-4 border-b border-kumo-line pb-4">
              <div className="min-w-0">
                <div className="mb-1 text-[11px] font-medium text-kumo-subtle">{modeLabel}</div>
                <h2 className="text-lg font-semibold text-kumo-strong">{title}</h2>
                <p className="mt-1 text-xs leading-relaxed text-kumo-subtle">{description}</p>
              </div>
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md border border-kumo-line bg-kumo-recessed text-kumo-brand">
                {mode === 'setup' ? <Rocket className="size-4" /> : <Shield className="size-4" />}
              </span>
            </div>

            {children}
          </LayerCard>
        </div>
      </section>
    </main>
  );
}

function AuthErrorBanner({ message }) {
  if (!message) return null;

  return (
    <Banner
      variant="error"
      icon={<AlertTriangle className="size-4" />}
      title="验证失败"
      description={message}
      className="rounded-md px-3 py-2 text-xs"
    />
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
  } = useStore();

  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [setupError, setSetupError] = useState('');
  const [setupLoading, setSetupLoading] = useState(false);

  const handle2FAInput = (event) => {
    const value = event.target.value.replace(/\D/g, '').slice(0, 6);
    setLoginTotpToken(value);
    if (value.length === 6) {
      verifyPassword(true);
    }
  };

  const handleSetupPassword = async (event) => {
    event.preventDefault();
    setSetupError('');

    if (!newPassword || newPassword.length < 6) {
      setSetupError('密码长度至少 6 位。');
      return;
    }

    if (newPassword !== confirmPassword) {
      setSetupError('两次输入的密码不一致。');
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
        setSetupError(result.error || '设置密码失败。');
        return;
      }

      setLoginPassword(newPassword);
      await verifyPassword(false);
    } catch (error) {
      setSetupError('设置失败，请检查网络连接。');
    } finally {
      setSetupLoading(false);
    }
  };

  const handleLogin = (event) => {
    event.preventDefault();
    verifyPassword();
  };

  if (showSetPasswordModal) {
    return (
      <AuthShell
        mode="setup"
        title="设置管理员密码"
        description="首次使用前，请为控制台创建一个管理员密码。"
      >
        <form onSubmit={handleSetupPassword} className="space-y-4">
          <Input
            size="base"
            type="password"
            label="新密码"
            description="至少 6 位，建议使用更长的短语。"
            placeholder="设置管理员密码"
            value={newPassword}
            onChange={(event) => setNewPassword(event.target.value)}
            autoComplete="new-password"
            className="w-full"
            autoFocus
          />

          <Input
            size="base"
            type="password"
            label="确认密码"
            placeholder="再次输入密码"
            value={confirmPassword}
            onChange={(event) => setConfirmPassword(event.target.value)}
            autoComplete="new-password"
            className="w-full"
          />

          <AuthErrorBanner message={setupError} />

          <Button
            type="submit"
            variant="primary"
            size="base"
            loading={setupLoading}
            icon={!setupLoading ? <ArrowRight className="size-3.5" /> : undefined}
            className="w-full justify-center"
          >
            开始使用
          </Button>
        </form>
      </AuthShell>
    );
  }

  const title = isDemoMode ? '演示模式' : loginRequire2FA ? '输入二次验证码' : '欢迎回来';
  const description = isDemoMode
    ? '当前环境无需密码，确认后可直接进入控制台。'
    : loginRequire2FA
      ? '请输入 Authenticator App 中显示的 6 位动态验证码。'
      : '输入管理员密码以访问监控面板。';

  return (
    <AuthShell
      mode={loginRequire2FA ? '2fa' : 'login'}
      title={title}
      description={description}
    >
      <form onSubmit={handleLogin} className="space-y-4">
        {isDemoMode && (
          <Banner
            variant="secondary"
            icon={<Shield className="size-4" />}
            title="演示环境"
            description="不会保存真实凭据，适合快速预览功能。"
            className="rounded-md px-3 py-2 text-xs"
          />
        )}

        {!isDemoMode && !loginRequire2FA && (
          <Input
            size="base"
            type="password"
            label="管理员密码"
            placeholder="请输入管理员密码"
            value={loginPassword}
            onChange={(event) => setLoginPassword(event.target.value)}
            autoComplete="current-password"
            className="w-full"
            autoFocus
          />
        )}

        {loginRequire2FA && (
          <Input
            size="base"
            type="text"
            inputMode="numeric"
            label="双因素验证码"
            description="填满 6 位后会自动验证，也可以按 Enter 提交。"
            maxLength={6}
            placeholder="000000"
            value={loginTotpToken}
            onChange={handle2FAInput}
            autoComplete="one-time-code"
            className="w-full text-center font-mono tracking-widest"
            autoFocus
          />
        )}

        <AuthErrorBanner message={loginError} />

        {!loginRequire2FA ? (
          <Button
            type="submit"
            variant="primary"
            size="base"
            loading={loginLoading}
            icon={!loginLoading ? <LogIn className="size-3.5" /> : undefined}
            className="w-full justify-center"
          >
            {isDemoMode ? '进入演示模式' : '立即进入'}
          </Button>
        ) : (
          <div className="grid grid-cols-[auto_minmax(0,1fr)] gap-2">
            <Button
              type="button"
              onClick={cancelLogin2FA}
              variant="secondary"
              size="base"
              shape="square"
              icon={<ChevronLeft className="size-3.5" />}
              aria-label="返回修改密码"
              title="返回修改密码"
            />
            <Button
              type="submit"
              variant="primary"
              size="base"
              loading={loginLoading}
              icon={!loginLoading ? <Key className="size-3.5" /> : undefined}
              className="justify-center"
            >
              验证并进入
            </Button>
          </div>
        )}

        <div className="flex items-center justify-between border-t border-kumo-line pt-3 text-[11px] text-kumo-subtle">
          <span>会话状态</span>
          <span className="font-medium text-kumo-success">受保护</span>
        </div>
      </form>
    </AuthShell>
  );
}

export default AuthPage;
