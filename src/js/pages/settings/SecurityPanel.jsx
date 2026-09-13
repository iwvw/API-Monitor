import React from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { ClipboardText } from '@cloudflare/kumo';
import { toast } from '../../modules/toast.js';
import { AppCard, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { browserSupportsWebAuthn } from '../../modules/webauthn.js';
import { ExternalLink, GitHubBrand, Globe, Lock, RefreshCw, Shield } from '../../components/Icons.jsx';
import { GITHUB_NEW_OAUTH_APP_URL, SECURITY_MASONRY_CARD_CLASS } from './constants.js';
import { describeUserAgent, formatSessionTime } from './utils.js';

export function SecurityPanel({
  changePassword,
  confirm2FASetup,
  currentOrigin,
  disable2FA,
  fetchLoginSessions,
  forceAllSessionsOffline,
  forceSessionOffline,
  githubAuth,
  githubAuthLoading,
  githubAuthSaving,
  githubOAuthCallback,
  isArmed,
  isDemoMode,
  loginSessions,
  passkeyBusy,
  passkeyForm,
  passkeys,
  passkeysLoading,
  passwordForm,
  passwordSaving,
  registerPasskey,
  removePasskey,
  saveGitHubLoginConfig,
  sessionsLoading,
  setGitHubAuth,
  setPasskeyForm,
  setPasswordForm,
  setTwoFA,
  settings,
  start2FASetup,
  twoFA,
}) {
  return (
        <div className="grid min-w-0 items-start gap-4 cq-xl:grid-cols-[minmax(22rem,0.9fr)_minmax(0,1.1fr)]">
          <div className="top-[calc(var(--app-header-height)+0.5rem)] z-20 flex min-w-0 flex-col gap-4 cq-xl:sticky">
          <SectionCard
            className={SECURITY_MASONRY_CARD_CLASS}
            title="管理员密码"
            icon={<Lock className="h-4 w-4 text-brand" />}
            bodyPadding="none"
          >
            <div className="flex w-full flex-col gap-4 p-5">
              <div>
                <Input size="sm"
                  label="当前密码"
                  type="text"
                  value={passwordForm.oldPassword}
                  onChange={(e) => setPasswordForm((prev) => ({ ...prev, oldPassword: e.target.value }))}
                  disabled={isDemoMode}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                  className="w-full"
                />
              </div>
              <div className="grid grid-cols-1 gap-4 cq-sm:grid-cols-2">
                <Input size="sm"
                  label="新密码"
                  type="text"
                  value={passwordForm.newPassword}
                  onChange={(e) => setPasswordForm((prev) => ({ ...prev, newPassword: e.target.value }))}
                  disabled={isDemoMode}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                  className="w-full"
                />
                <Input size="sm"
                  label="确认新密码"
                  type="text"
                  value={passwordForm.confirmPassword}
                  onChange={(e) => setPasswordForm((prev) => ({ ...prev, confirmPassword: e.target.value }))}
                  disabled={isDemoMode}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                  className="w-full"
                />
              </div>
              <div>
                <Button size="sm" variant="primary" onClick={changePassword} loading={passwordSaving} disabled={isDemoMode}>
                  更新密码
                </Button>
              </div>
            </div>
          </SectionCard>

          <SectionCard
            className={SECURITY_MASONRY_CARD_CLASS}
            title="GitHub 一键登录"
            icon={<GitHubBrand className="h-4 w-4 text-brand" />}
            meta={(
              <Badge variant={githubAuth.enabled ? 'success' : 'secondary'}>
                {githubAuth.enabled ? '已启用' : '未启用'}
              </Badge>
            )}
            bodyPadding="lg"
          >
            <div className="grid gap-4">
              <div className="grid gap-4 border-b border-kumo-line/70 pb-4 cq-xl:grid-cols-2 cq-xl:gap-0">
                  <div className="grid gap-2 cq-xl:pr-5">
                    <div className="inline-flex items-center gap-2 text-sm font-semibold text-kumo-strong">
                      <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-brand/10 text-xs font-semibold text-brand">1</span>
                      <span>创建 OAuth App</span>
                    </div>
                    <div className="text-xs leading-relaxed text-kumo-subtle">
                      <code className="app-inline-code">Homepage URL</code> 填当前站点地址即可。
                    </div>
                    <ClipboardText
                      size="sm"
                      text={settings.publicApiUrl || currentOrigin}
                      className="min-w-0 w-full font-mono text-[11px]"
                      tooltip={{ text: '复制主页地址', copiedText: '主页地址已复制' }}
                      labels={{ copyAction: '复制主页地址' }}
                    />
                    <div className="flex flex-wrap gap-2 pt-1">
                      <a href={GITHUB_NEW_OAUTH_APP_URL} target="_blank" rel="noreferrer">
                        <Button size="sm" variant="secondary" icon={<ExternalLink className="h-4 w-4" />}>
                          新建 OAuth App
                        </Button>
                      </a>
                    </div>
                  </div>

                  <div className="grid gap-2 border-t border-kumo-line/70 pt-4 cq-xl:border-l cq-xl:border-t-0 cq-xl:pl-5 cq-xl:pt-0">
                    <div className="inline-flex items-center gap-2 text-sm font-semibold text-kumo-strong">
                      <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-brand/10 text-xs font-semibold text-brand">2</span>
                      <span>填回调并保存到下方</span>
                    </div>
                    <div className="text-xs leading-relaxed text-kumo-subtle">
                      <code className="app-inline-code">Authorization callback URL</code> 用下方地址；创建后把 <code className="app-inline-code">Client ID / Secret</code> 填到下面。
                    </div>
                    <ClipboardText
                      size="sm"
                      text={githubOAuthCallback}
                      className="min-w-0 w-full font-mono text-[11px]"
                      tooltip={{ text: '复制回调地址', copiedText: 'GitHub 回调地址已复制' }}
                      labels={{ copyAction: '复制回调地址' }}
                    />
                  </div>
              </div>

              <div className="grid gap-3 cq-sm:grid-cols-2">
                <Input
                  size="sm"
                  label="Client ID"
                  value={githubAuth.clientId}
                  onChange={(event) => setGitHubAuth((prev) => ({ ...prev, clientId: event.target.value }))}
                />
                <Input
                  size="sm"
                  label={githubAuth.hasClientSecret ? 'Client Secret（留空表示保持不变）' : 'Client Secret'}
                  type="password"
                  value={githubAuth.clientSecret}
                  onChange={(event) => setGitHubAuth((prev) => ({ ...prev, clientSecret: event.target.value }))}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                />
              </div>

              <div className="grid gap-3 cq-xl:grid-cols-2">
                <label className="grid gap-1.5 text-xs text-kumo-subtle">
                  <span className="font-semibold text-kumo-strong">允许登录的 GitHub 用户名</span>
                  <Textarea
                    value={githubAuth.allowedLoginsText}
                    onChange={(event) => setGitHubAuth((prev) => ({ ...prev, allowedLoginsText: event.target.value }))}
                    placeholder={'一行一个或逗号分隔\n如：iwvw'}
                    className="min-h-24"
                  />
                </label>
                <label className="grid gap-1.5 text-xs text-kumo-subtle">
                  <span className="font-semibold text-kumo-strong">允许登录的邮箱</span>
                  <Textarea
                    value={githubAuth.allowedEmailsText}
                    onChange={(event) => setGitHubAuth((prev) => ({ ...prev, allowedEmailsText: event.target.value }))}
                    placeholder={'可选；支持私人邮箱校验\n如：admin@example.com'}
                    className="min-h-24"
                  />
                </label>
              </div>

              <div className="flex flex-wrap items-center gap-3">
                <Switch
                  checked={githubAuth.enabled}
                  onCheckedChange={(checked) => setGitHubAuth((prev) => ({ ...prev, enabled: checked }))}
                  aria-label="启用 GitHub 登录"
                />
                <span className="text-sm text-kumo-strong">启用 GitHub 登录入口</span>
                <span className="text-xs text-kumo-subtle">保存后显示 GitHub 按钮。</span>
              </div>

              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant="primary"
                  onClick={saveGitHubLoginConfig}
                  loading={githubAuthSaving || githubAuthLoading}
                  disabled={isDemoMode}
                >
                  保存 GitHub 配置
                </Button>
              </div>
            </div>
          </SectionCard>
          </div>
          <div className="flex min-w-0 flex-col gap-4">
          <SectionCard
            className={SECURITY_MASONRY_CARD_CLASS}
            title="双因子认证与通行密钥"
            icon={<Shield className="h-4 w-4 text-brand" />}
            meta={(
              <div className="flex items-center gap-2">
                <Badge variant={twoFA.enabled ? 'success' : 'warning'}>
                  {twoFA.enabled ? 'TOTP 已启用' : 'TOTP 未启用'}
                </Badge>
                <Badge variant={passkeys.length > 0 ? 'success' : 'secondary'}>
                  {passkeys.length > 0 ? `${passkeys.length} 个通行密钥` : '无通行密钥'}
                </Badge>
              </div>
            )}
            bodyPadding="lg"
          >
            <div className="grid items-start gap-4 cq-xl:grid-cols-2">
              <AppCard padding="md" className="flex h-auto flex-col gap-4 self-start border border-kumo-line/80">
                <div className="space-y-1">
                  <div className="text-sm font-semibold text-kumo-strong">验证器</div>
                  <div className="text-xs leading-relaxed text-kumo-subtle">为密码和 GitHub 登录增加 6 位验证码</div>
                </div>

                {twoFA.error && (
                  <div className="rounded-md border border-kumo-danger/20 bg-kumo-danger/10 px-3 py-2 text-xs text-kumo-danger">
                    {twoFA.error}
                  </div>
                )}

                {!twoFA.enabled && !twoFA.setupMode && (
                  <Button size="sm" variant="primary" onClick={start2FASetup} loading={twoFA.loading} disabled={isDemoMode}>
                    启用 2FA
                  </Button>
                )}

                {twoFA.setupMode && (
                  <div className="grid gap-4">
                    {twoFA.qrCode && (
                      <AppCard padding="none" className="flex justify-center p-4">
                        <img src={twoFA.qrCode} alt="2FA QR Code" className="h-44 w-44" />
                      </AppCard>
                    )}
                    <Input size="sm" label="手动密钥" value={twoFA.secret} readOnly className="font-mono" />
                    <Input size="sm"
                      label="6 位验证码"
                      value={twoFA.token}
                      onChange={(e) => setTwoFA((prev) => ({ ...prev, token: e.target.value.replace(/\D/g, '').slice(0, 6) }))}
                      placeholder="000000"
                      className="font-mono"
                    />
                    <div className="flex gap-2">
                      <Button size="sm" onClick={() => setTwoFA((prev) => ({ ...prev, setupMode: false, token: '', error: '' }))}>取消</Button>
                      <Button size="sm" variant="primary" onClick={confirm2FASetup} loading={twoFA.loading}>确认启用</Button>
                    </div>
                  </div>
                )}

                {twoFA.enabled && !twoFA.disableMode && (
                  <Button size="sm" variant="secondary-destructive" onClick={() => setTwoFA((prev) => ({ ...prev, disableMode: true, error: '' }))} disabled={isDemoMode}>
                    禁用 2FA
                  </Button>
                )}

                {twoFA.disableMode && (
                  <div className="grid gap-4">
                    <Input size="sm"
                      label="当前密码"
                      type="password"
                      value={twoFA.disablePassword}
                      onChange={(e) => setTwoFA((prev) => ({ ...prev, disablePassword: e.target.value }))}
                      autoComplete="off"
                      data-1p-ignore
                      data-lpignore="true"
                      data-bwignore="true"
                      data-form-type="other"
                      spellCheck={false}
                    />
                    <div className="flex gap-2">
                      <Button size="sm" onClick={() => setTwoFA((prev) => ({ ...prev, disableMode: false, disablePassword: '', error: '' }))}>取消</Button>
                      <Button size="sm" variant="destructive" onClick={disable2FA} loading={twoFA.loading}>确认禁用</Button>
                    </div>
                  </div>
                )}
              </AppCard>

              <AppCard padding="md" className="flex h-auto flex-col gap-4 self-start border border-kumo-line/80">
                <div className="space-y-1">
                  <div className="text-sm font-semibold text-kumo-strong">通行密钥</div>
                  <div className="text-xs leading-relaxed text-kumo-subtle">支持 Windows Hello、Touch ID、安全密钥等。</div>
                </div>

                <div className="grid gap-3">
                  <Input
                    size="sm"
                    label="通行密钥名称"
                    value={passkeyForm.label}
                    onChange={(event) => setPasskeyForm((prev) => ({ ...prev, label: event.target.value }))}
                    placeholder="如：Windows Hello"
                  />
                </div>

                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="primary"
                    onClick={registerPasskey}
                    loading={passkeyBusy}
                    disabled={isDemoMode || !browserSupportsWebAuthn()}
                  >
                    添加通行密钥
                  </Button>
                  {!browserSupportsWebAuthn() && (
                    <span className="text-xs text-kumo-warning">当前环境不支持 WebAuthn</span>
                  )}
                </div>

                <div className="divide-y divide-kumo-line rounded-md border border-kumo-line/80">
                  {passkeysLoading && (
                    <div className="px-4 py-6 text-sm text-kumo-subtle">加载中...</div>
                  )}
                  {!passkeysLoading && passkeys.map((passkey) => (
                    <div key={passkey.id} className="grid gap-3 px-4 py-3 cq-md:grid-cols-[minmax(0,1fr)_auto] cq-md:items-center">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-sm font-semibold text-kumo-strong">{passkey.label || '通行密钥'}</span>
                          {passkey.attachment && <Badge variant="secondary">{passkey.attachment}</Badge>}
                          {passkey.backedUp ? <Badge variant="success">可同步</Badge> : null}
                        </div>
                        <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs text-kumo-subtle">
                          <span>添加时间: <span className="text-kumo-strong">{formatSessionTime(passkey.createdAt)}</span></span>
                          <span>最近使用: <span className="text-kumo-strong">{formatSessionTime(passkey.lastUsedAt)}</span></span>
                        </div>
                        <div className="mt-1 truncate font-mono text-[10px] text-kumo-subtle">{passkey.id}</div>
                      </div>
                      <Button
                        size="sm"
                        variant={isArmed(`passkey:${passkey.id}`) ? 'destructive' : 'secondary-destructive'}
                        onClick={() => removePasskey(passkey)}
                        loading={passkeyBusy}
                        disabled={isDemoMode}
                      >
                        删除
                      </Button>
                    </div>
                  ))}
                  {!passkeysLoading && passkeys.length === 0 && (
                    <div className="px-4 py-8 text-center text-sm text-kumo-subtle">暂无通行密钥</div>
                  )}
                </div>
              </AppCard>
            </div>
          </SectionCard>

          <SectionCard
            className={SECURITY_MASONRY_CARD_CLASS}
            title="登录设备"
            icon={<Globe className="h-4 w-4 text-brand" />}
            actions={(
              <div className="flex items-center gap-2">
                <Button
                  size="sm"
                  shape="square"
                  variant="secondary"
                  onClick={() => fetchLoginSessions().catch((error) => toast.error(error.message || '加载登录设备失败'))}
                  loading={sessionsLoading}
                  icon={<RefreshCw className="h-3.5 w-3.5" />}
                  aria-label="刷新登录设备"
                  title="刷新登录设备"
                />
                <Button size="sm" variant="secondary-destructive" onClick={forceAllSessionsOffline}>
                  全部下线
                </Button>
              </div>
            )}
            bodyPadding="none"
          >
            <div className="divide-y divide-kumo-line">
              {loginSessions.map((session) => (
                <div key={session.id} className="grid gap-3 px-4 py-3 cq-md:grid-cols-[minmax(0,1fr)_auto] cq-md:items-center">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-sm font-semibold text-kumo-strong">{describeUserAgent(session.userAgent)}</span>
                      {session.current && <Badge variant="success">当前设备</Badge>}
                      <span className="font-mono text-[10px] text-kumo-subtle">{session.id}</span>
                    </div>
                    <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs text-kumo-subtle">
                      <span>IP: <span className="font-mono text-kumo-strong">{session.ipAddress || '-'}</span></span>
                      <span>最后活动: <span className="text-kumo-strong">{formatSessionTime(session.lastAccessedAt)}</span></span>
                      <span>会话到期: <span className="text-kumo-strong">{formatSessionTime(session.expiresAt)}</span></span>
                    </div>
                    {session.userAgent && <div className="mt-1 truncate text-[10px] text-kumo-subtle" title={session.userAgent}>{session.userAgent}</div>}
                  </div>
                  <Button
                    size="sm"
                    variant={isArmed(`session-offline:${session.id}`) ? 'destructive' : 'secondary-destructive'}
                    onClick={() => forceSessionOffline(session)}
                  >
                    {isArmed(`session-offline:${session.id}`) ? '确认下线' : '强制下线'}
                  </Button>
                </div>
              ))}
              {!sessionsLoading && loginSessions.length === 0 && (
                <div className="px-4 py-8 text-center text-sm text-kumo-subtle">暂无有效登录设备</div>
              )}
            </div>
          </SectionCard>
          </div>
        </div>
  );
}
