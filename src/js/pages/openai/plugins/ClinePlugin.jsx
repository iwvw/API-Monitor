import { useEffect, useRef, useState } from 'react';
import { Button, Dialog, Loader } from '@cloudflare/kumo';
import { TokenPlugin } from './TokenPlugin.jsx';
import { ClineBrand } from '../../../components/Icons.jsx';
import { getAuthHeaders } from '../utils.js';
import { toast } from '../../../modules/toast.js';

const API = '/api/cline';

// ClinePlugin：Cline（cline.bot）账号转 OpenAI 兼容端点。
//
// 上游是 WorkOS OAuth 令牌（`workos:` 前缀），免费池与 ClinePass 订阅池是
// 同一个账号上的两个额度池。支持「粘贴 token」与「快捷登录」（设备授权
// RFC 8628：/login/start + /login/poll，浏览器里完成登录后自动取回凭证）。
export function ClinePlugin() {
  const [loginOpen, setLoginOpen] = useState(false);
  const [login, setLogin] = useState({
    phase: 'idle', // idle | loading | waiting | success | expired | error
    state: '',
    userCode: '',
    url: '',
    pollMs: 5000,
    instructions: '',
    error: '',
  });
  const reloadRef = useRef(null);

  const startLogin = async () => {
    setLogin({
      phase: 'loading',
      state: '',
      userCode: '',
      url: '',
      pollMs: 5000,
      instructions: '',
      error: '',
    });
    try {
      const res = await fetch(`${API}/login/start`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '发起登录失败');
      setLogin({
        phase: 'waiting',
        state: data.state || '',
        userCode: data.userCode || '',
        url: data.url || '',
        pollMs: data.pollMs > 0 ? data.pollMs : 5000,
        instructions: data.instructions || '',
        error: '',
      });
      if (data.url) window.open(data.url, '_blank', 'noopener,noreferrer');
    } catch (e) {
      setLogin(prev => ({ ...prev, phase: 'error', error: e.message }));
    }
  };

  useEffect(() => {
    if (!loginOpen || !login.state || login.phase !== 'waiting') return undefined;
    // 会话更替（重新发起登录/弹窗关闭）时 abort 在途 poll，避免旧会话的响应
    // 把新一轮会话错误地标成 error/expired 并杀掉新会话的轮询。
    const controller = new AbortController();
    const timer = setInterval(async () => {
      try {
        const res = await fetch(`${API}/login/poll`, {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify({ state: login.state }),
          signal: controller.signal,
        });
        const data = await res.json();
        if (data?.status === 'success') {
          setLogin(prev => ({ ...prev, phase: 'success', error: '' }));
          toast.success(data.updated ? 'Cline 登录成功，已更新既有账号' : 'Cline 登录成功，账号已接入');
          reloadRef.current?.();
          setLoginOpen(false);
        } else if (data?.status === 'expired') {
          setLogin(prev => ({ ...prev, phase: 'expired', error: data.error || '授权码已过期，请重新发起登录' }));
        } else if (!data?.success || data?.status === 'error') {
          setLogin(prev => ({ ...prev, phase: 'error', error: data?.error || '登录失败' }));
        }
      } catch (e) {
        if (e?.name === 'AbortError') return;
        setLogin(prev => ({ ...prev, phase: 'error', error: e.message }));
      }
    }, login.pollMs);
    return () => {
      clearInterval(timer);
      controller.abort();
    };
  }, [loginOpen, login.state, login.phase, login.pollMs]);

  const openLogin = () => {
    setLoginOpen(true);
    startLogin();
  };

  return (
    <>
      <TokenPlugin
        config={{ id: 'cline' }}
        api={API}
        brandIcon={ClineBrand}
        title="Cline"
        hasRegion={false}
        hasCheckin={false}
        addHint="从 Cline 桌面端 ~/.cline/data/settings/providers.json 的 providers.cline.settings.auth 复制 accessToken（含 workos: 前缀）与 refreshToken；或点上方「快捷登录」走设备授权，无需手动复制。"
        credentialFields={[
          { key: 'access_token', label: 'accessToken（必填，含 workos: 前缀）', placeholder: 'workos:eyJhbGci…', required: true },
          { key: 'refresh_token', label: 'refreshToken（推荐，用于自动续期）', placeholder: 'N5EUfPqyERlpRhiF4WUXDPunz' },
        ]}
        strategyOptions={[
          { value: 'first', label: '固定首个可用' },
          { value: 'round-robin', label: '轮询' },
        ]}
        headerActions={refreshAll => {
          reloadRef.current = refreshAll;
          return (
            <Button size="sm" variant="outline" onClick={openLogin} title="在浏览器完成登录后自动取回凭证并接入">
              快捷登录
            </Button>
          );
        }}
      />
      <Dialog.Root open={loginOpen} onOpenChange={setLoginOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(30rem,calc(100vw-2rem))] !max-w-[min(30rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">快捷登录 Cline</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              已打开授权页：输入下方授权码，用 Google/GitHub 完成登录，凭证会自动取回并接入，无需手动复制。
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="flex flex-col gap-3">
              {login.phase === 'loading' ? (
                <div className="flex h-[96px] w-full items-center justify-center rounded border border-kumo-line">
                  <Loader size="lg" />
                </div>
              ) : (
                <div className="flex min-h-[96px] w-full flex-col items-center justify-center gap-2 rounded border border-kumo-line px-4 text-center text-xs">
                  {login.phase === 'error' ? (
                    <span className="text-kumo-strong">{login.error}</span>
                  ) : login.phase === 'expired' ? (
                    <span className="text-kumo-warning">{login.error}</span>
                  ) : login.phase === 'success' ? (
                    <span className="text-kumo-strong">登录成功，账号已接入。</span>
                  ) : login.phase === 'waiting' ? (
                    <>
                      <span className="text-kumo-subtle">
                        {login.instructions || '在打开的页面输入授权码并确认'}
                      </span>
                      {login.userCode ? (
                        <span className="font-mono text-xl font-semibold tracking-widest text-kumo-strong">
                          {login.userCode}
                        </span>
                      ) : null}
                    </>
                  ) : (
                    <span className="text-kumo-subtle">暂无登录链接</span>
                  )}
                </div>
              )}
              {login.url && login.phase === 'waiting' ? (
                <a
                  className="max-w-full truncate font-mono text-[0.75em] text-brand underline"
                  href={login.url}
                  target="_blank"
                  rel="noreferrer"
                  title={login.url}
                >
                  授权页没弹出来？点此重新打开
                </a>
              ) : null}
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>关闭</Button>} />
            <Button size="sm" variant="secondary" onClick={startLogin}>重新发起登录</Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </>
  );
}
