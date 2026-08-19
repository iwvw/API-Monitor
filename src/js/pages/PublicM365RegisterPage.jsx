import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { InputGroup } from '@cloudflare/kumo/components/input-group';
import { Select } from '@cloudflare/kumo/components/select';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Check, Copy, Download, ExternalLink, Globe, Shield, User } from '../components/Icons.jsx';
import { SectionCard } from '../components/ui/AppPrimitives.jsx';
import { useCloudflareSpotlight } from '../hooks/useCloudflareSpotlight.js';
import { toast } from '../modules/toast.js';
import useStore from '../store.js';

const MICROSOFT_LOGIN_URL = 'https://login.microsoftonline.com/';
const MICROSOFT_OFFICE_ICON_URL = 'https://www.svgrepo.com/show/473720/microsoftoffice.svg';

function getRegisterParams() {
  const params = new URLSearchParams(window.location.search);
  return {
    code: String(params.get('code') || '').trim(),
    batch: String(params.get('batch') || '').trim(),
  };
}

function formatDateTime(value) {
  if (!value) return '长期有效';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '长期有效';
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function parseResponse(response) {
  return response
    .json()
    .catch(() => ({}))
    .then(payload => {
      if (!response.ok || payload.success === false) {
        throw new Error(payload.error || '请求失败');
      }
      return payload.data ?? payload;
    });
}

async function copyText(text, message) {
  try {
    await navigator.clipboard.writeText(String(text || ''));
    toast.success(message);
  } catch {
    toast.error('复制失败');
  }
}

function downloadCredentials(success) {
  try {
    const filenameBase = String(success?.userPrincipalName || 'm365-credential').replace(
      /[^\w.-]+/g,
      '_'
    );
    const content = [
      'Microsoft 365 登录凭证',
      '',
      `登录账号: ${success?.userPrincipalName || ''}`,
      `初始密码: ${success?.initialPassword || ''}`,
      `首次登录需改密: ${success?.forceChangePasswordNextSignIn ? '是' : '否'}`,
      `微软登录入口: ${MICROSOFT_LOGIN_URL}`,
    ].join('\n');
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `${filenameBase}.txt`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
    toast.success('凭证已下载');
  } catch {
    toast.error('下载凭证失败');
  }
}

function PublicM365RegisterPage() {
  const surfaceRef = useCloudflareSpotlight();
  const successRef = useRef(null);
  const theme = useStore(state => state.theme);
  const { code, batch } = useMemo(getRegisterParams, []);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState(null);
  const [invite, setInvite] = useState(null);
  const [form, setForm] = useState({
    displayName: '',
    mailNickname: '',
    accountId: '',
    domain: '',
  });

  const loadInvite = useCallback(
    async ({ withLoading = true, reportError = true } = {}) => {
      if (!code && !batch) {
        if (withLoading) setLoading(false);
        if (reportError) setError('注册链接缺少参数');
        return null;
      }
      if (withLoading) setLoading(true);
      if (reportError) setError('');
      try {
        const query = new URLSearchParams();
        if (code) query.set('code', code);
        if (batch) query.set('batch', batch);
        const data = await fetch(`/api/m365/public/register?${query.toString()}`, {
          cache: 'no-store',
        }).then(parseResponse);
        const inviteData = data?.invite || null;
        setInvite(inviteData);
        const targets = Array.isArray(inviteData?.targets) ? inviteData.targets : [];
        const firstTarget = targets[0] || null;
        const availableTargetKeys = new Set(
          targets.map(target => `${target?.accountId || ''}::${target?.domain || ''}`)
        );
        setForm(current => {
          if (inviteData?.targetCount === 1 && firstTarget?.accountId && firstTarget?.domain) {
            return {
              ...current,
              accountId: String(firstTarget.accountId),
              domain: String(firstTarget.domain),
            };
          }
          const currentTargetKey =
            current.accountId && current.domain ? `${current.accountId}::${current.domain}` : '';
          if (currentTargetKey && availableTargetKeys.has(currentTargetKey)) {
            return current;
          }
          return {
            ...current,
            accountId: '',
            domain: '',
          };
        });
        return inviteData;
      } catch (err) {
        if (reportError) {
          setError(err.message || '公开注册信息加载失败');
        }
        return null;
      } finally {
        if (withLoading) setLoading(false);
      }
    },
    [batch, code]
  );

  useEffect(() => {
    void loadInvite();
  }, [loadInvite]);

  useEffect(() => {
    if (!success) return undefined;
    const timer = window.setTimeout(() => {
      successRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }, 40);
    return () => window.clearTimeout(timer);
  }, [success]);

  const targetItems = useMemo(() => {
    const targets = Array.isArray(invite?.targets) ? invite.targets : [];
    const domainCount = targets.reduce((acc, item) => {
      const key = String(item?.domain || '')
        .trim()
        .toLowerCase();
      if (!key) return acc;
      acc[key] = (acc[key] || 0) + 1;
      return acc;
    }, {});
    return targets.map(target => {
      const domain = String(target?.domain || '').trim();
      const duplicatedDomain = (domainCount[domain.toLowerCase()] || 0) > 1;
      return {
        value: `${target.accountId}::${target.domain}`,
        label: duplicatedDomain ? `@${domain} · ${target.accountName}` : `@${domain}`,
      };
    });
  }, [invite]);

  const selectedTargetValue =
    form.accountId && form.domain ? `${form.accountId}::${form.domain}` : null;

  const requiresTargetSelection = Number(invite?.targetCount || 0) > 1;
  const singleTargetDomain = String(
    form.domain || invite?.domain || invite?.targets?.[0]?.domain || ''
  ).trim();
  const loginSuffix = requiresTargetSelection
    ? form.domain
      ? `@${form.domain}`
      : '@请选择域名'
    : singleTargetDomain
      ? `@${singleTargetDomain}`
      : '@-';

  const refreshInviteUsage = useCallback(() => {
    setInvite(current => {
      if (!current) return current;
      const totalCount = Math.max(1, Number(current.inviteCount || 1));
      const currentUsedCount = Math.max(0, Number(current.usedCount || 0));
      const nextUsedCount = Math.min(totalCount, currentUsedCount + 1);
      if (current.mode === 'batch') {
        const currentAvailableCount = Math.max(
          0,
          Number(current.availableCount ?? totalCount - currentUsedCount)
        );
        const nextAvailableCount = Math.max(0, currentAvailableCount - 1);
        return {
          ...current,
          usedCount: nextUsedCount,
          availableCount: nextAvailableCount,
          used: nextAvailableCount === 0,
          available: nextAvailableCount > 0,
          availabilityReason: nextAvailableCount > 0 ? current.availabilityReason : 'used',
        };
      }
      return {
        ...current,
        usedCount: nextUsedCount,
        used: nextUsedCount >= totalCount,
        available: nextUsedCount < totalCount,
        availabilityReason: nextUsedCount < totalCount ? current.availabilityReason : 'used',
      };
    });
  }, []);

  const renderShell = content => (
    <div
      ref={surfaceRef}
      className="cf-ai-background-surface public-m365-register-page relative isolate min-h-screen text-kumo-default"
    >
      <div aria-hidden="true" className="cf-ai-background pointer-events-none absolute inset-0" />
      <main className="relative z-10 mx-auto flex min-h-screen w-full max-w-6xl flex-col px-4 py-6 sm:px-6 lg:px-8 xl:px-10">
        {content}
      </main>
    </div>
  );

  const submit = async () => {
    if (!form.mailNickname.trim()) {
      setError('请填写登录前缀');
      return;
    }
    if (requiresTargetSelection && (!form.accountId || !form.domain)) {
      setError('请先选择注册域名');
      return;
    }
    setSubmitting(true);
    setError('');
    setSuccess(null);
    try {
      const payload = {
        displayName: form.displayName.trim(),
        mailNickname: form.mailNickname.trim(),
      };
      if (code) payload.code = code;
      if (batch) payload.batch = batch;
      if (form.accountId) payload.accountId = Number(form.accountId);
      if (form.domain) payload.domain = form.domain;
      const result = await fetch('/api/m365/public/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }).then(parseResponse);
      setSuccess(result);
      setForm(current => ({
        ...current,
        displayName: '',
        mailNickname: '',
      }));
      refreshInviteUsage();
      void loadInvite({ withLoading: false, reportError: false });
    } catch (err) {
      setError(err.message || '注册失败');
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return renderShell(
      <div className="mx-auto mt-10 w-full max-w-5xl">
        <SectionCard
          title="Microsoft 365 注册中"
          icon={<Globe className="h-4 w-4 text-brand" />}
          bodyPadding="lg"
        >
          <div className="py-10 text-center text-sm text-kumo-subtle">正在加载注册链接信息…</div>
        </SectionCard>
      </div>
    );
  }

  return renderShell(
    <div className="flex flex-1 flex-col gap-4 py-1">
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <img
            src={MICROSOFT_OFFICE_ICON_URL}
            alt="Microsoft Office"
            className="h-9 w-9 shrink-0 object-contain"
            style={{ filter: theme === 'dark' ? 'brightness(0) invert(1)' : 'none' }}
          />
          <div className="min-w-0">
            <div className="text-base font-bold text-kumo-strong">Microsoft 365 公开注册</div>
            <div className="mt-1">
              <Badge variant="secondary">{batch || code || '-'}</Badge>
            </div>
          </div>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <Badge variant={invite?.used ? 'error' : invite?.available ? 'success' : 'warning'}>
            {invite?.mode === 'batch'
              ? `已用 ${invite?.usedCount || 0}/${invite?.inviteCount || 0}`
              : invite?.used
                ? '已使用'
                : invite?.available
                  ? '可用'
                  : '不可用'}
          </Badge>
        </div>
      </div>

      <div className="grid items-stretch gap-4 lg:grid-cols-[minmax(0,1.2fr)_24rem] xl:grid-cols-[minmax(0,1.3fr)_26rem]">
        <SectionCard
          className="h-full"
          bodyClassName="flex min-h-0 flex-1 flex-col"
          bodyPadding="lg"
          title={invite?.publicPageName || '注册链接'}
          description="填写前缀后创建账号"
          icon={<User className="h-4 w-4 text-brand" />}
        >
          <div className="flex min-h-0 flex-1 flex-col gap-4">
            <Input
              aria-label="姓名"
              value={form.displayName}
              onChange={event =>
                setForm(current => ({ ...current, displayName: event.target.value }))
              }
              placeholder="姓名，可留空"
            />
            <div
              className={
                requiresTargetSelection
                  ? 'grid gap-3 lg:grid-cols-[minmax(0,1fr)_15rem] lg:items-end'
                  : 'grid gap-2'
              }
            >
              <div className="grid gap-2">
                <div className="text-xs text-kumo-subtle">账户名</div>
                <InputGroup className="w-full">
                  <InputGroup.Input
                    aria-label="账户名"
                    value={form.mailNickname}
                    onChange={event =>
                      setForm(current => ({ ...current, mailNickname: event.target.value }))
                    }
                  />
                  <InputGroup.Suffix>{loginSuffix}</InputGroup.Suffix>
                </InputGroup>
              </div>

              {requiresTargetSelection ? (
                <div className="grid gap-2">
                  <div className="text-xs text-kumo-subtle">注册域名</div>
                  <Select
                    aria-label="注册域名"
                    value={selectedTargetValue}
                    placeholder="点击选择"
                    onValueChange={value => {
                      const [accountId, domain] = String(value || '').split('::');
                      setForm(current => ({
                        ...current,
                        accountId: accountId || '',
                        domain: domain || '',
                      }));
                    }}
                    items={targetItems}
                  />
                </div>
              ) : null}
            </div>
            <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-2 text-sm text-kumo-subtle">
              初始密码由系统自动生成，创建成功后展示。
              {invite?.forceChangePasswordNextSignIn ? ' 首次登录后会要求立即修改密码。' : ''}
            </div>
            {error ? (
              <div className="rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-sm text-kumo-danger">
                {error}
              </div>
            ) : null}

            <div className="mt-auto flex justify-end">
              <Button
                size="sm"
                variant="primary"
                onClick={submit}
                disabled={submitting || invite?.available === false}
              >
                {submitting ? '注册中...' : '创建账号'}
              </Button>
            </div>
          </div>
        </SectionCard>

        <SectionCard
          className="h-full"
          bodyClassName="flex min-h-0 flex-1 flex-col"
          bodyPadding="lg"
          title="注册链接信息"
          icon={<Shield className="h-4 w-4 text-brand" />}
        >
          <div className="grid gap-3 text-sm">
            <div className="grid gap-2 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3 text-xs sm:grid-cols-2">
              <div>
                <div className="text-kumo-subtle">状态</div>
                <div className="mt-1 font-semibold text-kumo-strong">
                  {invite?.available ? '可用' : invite?.availabilityReason || '不可用'}
                </div>
              </div>
              <div>
                <div className="text-kumo-subtle">有效期</div>
                <div className="mt-1 font-semibold text-kumo-strong">
                  {formatDateTime(invite?.expiresAt)}
                </div>
              </div>
              <div>
                <div className="text-kumo-subtle">使用次数</div>
                <div className="mt-1 font-semibold text-kumo-strong">
                  {invite?.usedCount || 0} / {invite?.inviteCount || 1}
                </div>
              </div>
              <div>
                <div className="text-kumo-subtle">注册链接</div>
                <div className="mt-1 font-semibold text-kumo-strong">
                  {invite?.targetCount > 1 ? '支持自选域名' : '固定域名'}
                </div>
              </div>
            </div>
            <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
              <div className="text-xs text-kumo-subtle">说明</div>
              <div className="mt-1 text-kumo-subtle">
                一个邀请码只能使用一次。创建成功后，请前往微软登录页登录。
              </div>
            </div>
          </div>
        </SectionCard>
      </div>

      {success ? (
        <div ref={successRef}>
          <SectionCard
            title="账号创建成功"
            description="请保存账号和初始密码"
            icon={<Check className="h-4 w-4 text-kumo-success" />}
            bodyPadding="lg"
          >
            <div className="grid gap-4">
              <div className="rounded-md border border-kumo-success/30 bg-kumo-success/10 p-4 text-sm text-kumo-success">
                请保存下方登录账号和初始密码。
              </div>
              <div className="grid gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-4 text-sm md:grid-cols-2">
                <div className="rounded-md border border-kumo-line/70 bg-kumo-base/70 p-3">
                  <div className="text-xs text-kumo-subtle">登录账号</div>
                  <div className="mt-2 flex items-center justify-between gap-3">
                    <span
                      className="min-w-0 truncate font-semibold text-kumo-strong"
                      title={success.userPrincipalName}
                    >
                      {success.userPrincipalName}
                    </span>
                    <Button
                      size="sm"
                      variant="secondary"
                      shape="square"
                      icon={<Copy className="h-4 w-4" />}
                      aria-label="复制登录账号"
                      onClick={() => copyText(success.userPrincipalName, '登录账号已复制')}
                    />
                  </div>
                </div>

                <div className="rounded-md border border-kumo-line/70 bg-kumo-base/70 p-3">
                  <div className="text-xs text-kumo-subtle">初始密码</div>
                  <div className="mt-2 flex items-center justify-between gap-3">
                    <span
                      className="min-w-0 truncate font-semibold text-kumo-strong"
                      title={success.initialPassword || '-'}
                    >
                      {success.initialPassword || '-'}
                    </span>
                    <Button
                      size="sm"
                      variant="secondary"
                      shape="square"
                      icon={<Copy className="h-4 w-4" />}
                      aria-label="复制初始密码"
                      onClick={() => copyText(success.initialPassword, '初始密码已复制')}
                    />
                  </div>
                </div>
              </div>

              <div className="rounded-md border border-kumo-warning/30 bg-kumo-warning/10 p-3 text-kumo-warning">
                初始密码只显示这一次，请及时保存。
                {success.forceChangePasswordNextSignIn ? ' 首次登录后会要求立即修改密码。' : ''}
              </div>

              {success.warning ? (
                <div className="rounded-md border border-kumo-warning/30 bg-kumo-warning/10 p-3 text-kumo-warning">
                  {success.warning}
                </div>
              ) : null}

              <div className="flex flex-wrap justify-end gap-2">
                <Button
                  size="sm"
                  variant="secondary"
                  icon={<Download className="h-4 w-4" />}
                  onClick={() => downloadCredentials(success)}
                >
                  下载凭证
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  icon={<ExternalLink className="h-4 w-4" />}
                  onClick={() => window.open(MICROSOFT_LOGIN_URL, '_blank', 'noopener,noreferrer')}
                >
                  前往微软登录
                </Button>
              </div>
            </div>
          </SectionCard>
        </div>
      ) : null}
    </div>
  );
}

export default PublicM365RegisterPage;
