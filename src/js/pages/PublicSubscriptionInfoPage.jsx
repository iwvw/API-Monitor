import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { ClipboardText, Meter } from '@cloudflare/kumo';
import { Copy, Home, LogIn, Plug } from '../components/Icons.jsx';
import useStore from '../store.js';
import { SectionCard } from '../components/ui/AppPrimitives.jsx';
import { formatDateTime, formatFileSize } from '../modules/utils.js';
import { toast } from '../modules/toast.js';

const statusMeta = (status) => {
  switch (String(status || '').toLowerCase()) {
    case 'exhausted':
      return { label: '流量已用尽', variant: 'error', tone: 'danger' };
    case 'expired':
      return { label: '已到期', variant: 'warning', tone: 'warning' };
    default:
      return { label: '可用', variant: 'success', tone: 'success' };
  }
};

function subscriptionTokenFromPath() {
  const match = window.location.pathname.match(/^\/sub\/([^/]+)/);
  return match ? decodeURIComponent(match[1]) : '';
}

function copySubLink(base, token, format = '') {
  const suffix = format ? `?format=${format}` : '';
  const text = `${base}/sub/${token}${suffix}`;
  navigator.clipboard
    ?.writeText(text)
    .then(() => toast.success('订阅链接已复制'))
    .catch(() => toast.error('复制失败'));
  return text;
}

function PublicSubscriptionInfoPage() {
  const isAuthenticated = useStore((state) => state.isAuthenticated);
  const token = useMemo(subscriptionTokenFromPath, []);
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const base = useMemo(() => {
    const { protocol, host } = window.location;
    return `${protocol}//${host}`;
  }, []);

  const load = useCallback(async () => {
    if (!token) {
      setError('订阅地址无效');
      setLoading(false);
      return;
    }
    setLoading(true);
    setError('');
    try {
      const res = await fetch(`/api/subscription/public/${encodeURIComponent(token)}`, { cache: 'no-store' });
      if (!res.ok) {
        const payload = await res.json().catch(() => ({}));
        throw new Error(payload.error || (res.status === 404 ? '订阅不存在' : '读取失败'));
      }
      const payload = await res.json();
      if (!payload.success) throw new Error(payload.error || '读取失败');
      setData(payload.data || null);
    } catch (err) {
      setError(err.message || '读取失败');
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    load();
  }, [load]);

  const used = (data?.upload || 0) + (data?.download || 0);
  const total = data?.total || 0;
  const percent = total > 0 ? Math.min(100, Math.max(0, data?.percent || 0)) : 0;
  const meta = statusMeta(data?.status);

  return (
    <div className="min-h-dvh bg-kumo-canvas p-4 text-kumo-default sm:p-6">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-4">
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="text-base font-bold text-kumo-strong">订阅信息</div>
            <div className="mt-1 truncate font-mono text-xs text-kumo-subtle">{token || '-'}</div>
          </div>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => { window.location.href = '/'; }}
            icon={isAuthenticated ? <Home className="h-3.5 w-3.5" /> : <LogIn className="h-3.5 w-3.5" />}
          >
            {isAuthenticated ? '主页' : '登录'}
          </Button>
        </div>

        <SectionCard
          title={loading ? '正在读取订阅' : data?.name || '订阅'}
          icon={<Plug className="h-4 w-4 text-kumo-brand" />}
          bodyClassName="flex flex-col gap-4"
        >
          {loading ? (
            <div className="py-10 text-center text-sm text-kumo-subtle">读取中</div>
          ) : error ? (
            <div className="rounded-md border border-kumo-error/30 bg-kumo-error/10 p-4 text-sm font-semibold text-kumo-error">
              {error}
            </div>
          ) : data ? (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={meta.variant}>{meta.label}</Badge>
                <Badge variant="secondary">{data.node_count || 0} 个节点</Badge>
              </div>

              <div className="flex min-w-0 flex-col gap-3">
                <Meter
                  label="已用流量"
                  value={percent}
                  customValue={`${formatFileSize(used)} / ${total > 0 ? formatFileSize(total) : '无限制'}`}
                />
                <div className="grid gap-2 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3 text-xs sm:grid-cols-2">
                  <div>
                    <span className="text-kumo-subtle">已用流量</span>
                    <div className="mt-1 font-semibold text-kumo-strong">{formatFileSize(used)}</div>
                  </div>
                  <div>
                    <span className="text-kumo-subtle">总量</span>
                    <div className="mt-1 font-semibold text-kumo-strong">
                      {total > 0 ? formatFileSize(total) : '无限制'}
                    </div>
                  </div>
                  <div>
                    <span className="text-kumo-subtle">到期时间</span>
                    <div className="mt-1 font-semibold text-kumo-strong">
                      {Number(data?.expire) > 0 ? formatDateTime(data.expire) : '无到期'}
                    </div>
                  </div>
                  <div>
                    <span className="text-kumo-subtle">下次重置</span>
                    <div className="mt-1 font-semibold text-kumo-strong">
                      {data?.cycle_end ? formatDateTime(data.cycle_end) : '不自动重置'}
                    </div>
                  </div>
                </div>
              </div>

              <div className="flex flex-col gap-2 border-t border-kumo-line pt-4">
                <div className="text-xs font-semibold text-kumo-subtle">复制订阅链接</div>
                <div className="grid gap-2 sm:grid-cols-2">
                  <ClipboardText
                    size="sm"
                    text={copySubLink(base, token)}
                    tooltip={{ text: '复制自适应订阅链接（按客户端自动识别）', copiedText: '自适应订阅链接已复制' }}
                    labels={{ copyAction: '复制自适应订阅' }}
                  />
                  <ClipboardText
                    size="sm"
                    text={copySubLink(base, token, 'clash')}
                    tooltip={{ text: '复制 Mihomo / Clash 链接', copiedText: 'Mihomo / Clash 链接已复制' }}
                    labels={{ copyAction: '复制 Clash（YAML）' }}
                  />
                  <ClipboardText
                    size="sm"
                    text={copySubLink(base, token, 'base64')}
                    tooltip={{ text: '复制 Base64 链接（sing-box 官方 / v2rayN）', copiedText: 'Base64 链接已复制' }}
                    labels={{ copyAction: '复制 Base64' }}
                  />
                  <ClipboardText
                    size="sm"
                    text={copySubLink(base, token, 'raw')}
                    tooltip={{ text: '复制 Raw 链接', copiedText: 'Raw 链接已复制' }}
                    labels={{ copyAction: '复制 Raw' }}
                  />
                </div>
                <Button
                  size="sm"
                  variant="secondary"
                  className="self-start"
                  icon={<Copy className="h-3.5 w-3.5" />}
                  onClick={() => copySubLink(base, token, 'info')}
                >
                  复制信息页链接
                </Button>
              </div>
            </>
          ) : null}
        </SectionCard>
      </div>
    </div>
  );
}

export default PublicSubscriptionInfoPage;
