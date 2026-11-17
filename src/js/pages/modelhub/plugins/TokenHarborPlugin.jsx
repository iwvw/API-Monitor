import { useRef, useState } from 'react';
import { Button, Dialog, Input } from '@cloudflare/kumo';
import { TokenPlugin } from './TokenPlugin.jsx';
import { TokenHarborBrand } from '../../../components/Icons.jsx';
import { getAuthHeaders } from '../utils.js';
import { toast } from '../../../modules/toast.js';

const API = '/api/tokenharbor';

// TokenHarborPlugin:TokenHarbor(tokenharbor.ai)账号转 OpenAI 兼容端点。
//
// 接入方式二选一:粘贴浏览器 Cookie(含 sb-auth-auth-token.0),或点「邮箱登录」
// 走 Supabase password grant 直接登录入库。登录态由后端自动续期。
export function TokenHarborPlugin() {
  const [loginOpen, setLoginOpen] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const reloadRef = useRef(null);

  const submitLogin = async () => {
    if (!email.trim() || !password) {
      setError('请填写邮箱与密码');
      return;
    }
    setBusy(true);
    setError('');
    try {
      const res = await fetch(`${API}/login`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ email: email.trim(), password }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '登录失败');
      toast.success(data.updated ? 'TokenHarbor 登录成功,已更新既有账号' : 'TokenHarbor 登录成功,账号已接入');
      reloadRef.current?.();
      setLoginOpen(false);
      setPassword('');
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <TokenPlugin
        config={{ id: 'tokenharbor' }}
        api={API}
        brandIcon={TokenHarborBrand}
        title="TokenHarbor"
        hasRegion={false}
        hasCheckin={false}
        addHint="浏览器登录 tokenharbor.ai → F12 → Network → 复制任意 API 请求的完整 Cookie 头(必须含 sb-auth-auth-token.0);或点上方「邮箱登录」直接登录,无需手动复制。"
        credentialFields={[
          { key: 'cookie', label: 'Cookie(必填,含 sb-auth-auth-token.0)', placeholder: 'sb-auth-auth-token.0=base64-...; th_sid=...', required: true },
        ]}
        strategyOptions={[
          { value: 'first', label: '固定首个可用' },
          { value: 'round-robin', label: '轮询' },
        ]}
        headerActions={refreshAll => {
          reloadRef.current = refreshAll;
          return (
            <Button size="sm" variant="outline" onClick={() => setLoginOpen(true)} title="用 TokenHarbor 邮箱密码登录并自动接入">
              邮箱登录
            </Button>
          );
        }}
      />
      <Dialog.Root
        open={loginOpen}
        onOpenChange={open => {
          setLoginOpen(open);
          if (!open) setError('');
        }}
      >
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(30rem,calc(100vw-2rem))] !max-w-[min(30rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">邮箱登录 TokenHarbor</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              用 TokenHarbor 账号邮箱与密码登录,凭证直接入库并由后端自动续期。
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="flex flex-col gap-3">
              <Input
                size="sm"
                label="邮箱"
                type="email"
                className="w-full"
                value={email}
                onChange={e => setEmail(e.target.value)}
                placeholder="you@example.com"
              />
              <Input
                size="sm"
                label="密码"
                type="password"
                className="w-full"
                value={password}
                onChange={e => setPassword(e.target.value)}
                placeholder="TokenHarbor 登录密码"
              />
              {error ? <span className="text-xs text-kumo-warning">{error}</span> : null}
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>关闭</Button>} />
            <Button size="sm" variant="primary" disabled={busy} onClick={submitLogin}>
              {busy ? '登录中...' : '登录'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </>
  );
}
