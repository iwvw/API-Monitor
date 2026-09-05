import React, { useEffect, useMemo, useState } from 'react';
import { Loader } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import PublicPageIconPicker from '../components/public/PublicPageIconPicker.jsx';
import { useCloudflareSpotlight } from '../hooks/useCloudflareSpotlight.js';
import {
  getPublicPageFaviconHref,
  swapPublicPageFavicon,
  withPublicPageIconId,
} from '../modules/publicPageBranding.js';
import { toast } from '../modules/toast.js';
import useStore from '../store.js';
import { AlertTriangle, Bookmark, ExternalLink, Home, LogIn, RefreshCw } from '../components/Icons.jsx';

const normalizePublicPath = () => {
  const path = window.location.pathname.replace(/\/+$/, '');
  const match = path.match(/^\/(?:b|bookmarks|bm)\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
};

const isLocalHost = (host) => /^(localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$/i.test(host || '');

const formatDateTime = (value) => {
  if (!value) return '';
  const date = new Date(String(value).includes('T') ? value : `${String(value).replace(' ', 'T')}Z`);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
};

function PublicItemIcon({ item }) {
  const { icon_type: type, icon_src: src, icon_text: text } = item || {};
  if (type === 2 && src) {
    return (
      <div className="flex h-11 w-11 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-kumo-recessed">
        <img src={src} alt="" loading="lazy" className="h-7 w-7 object-contain" />
      </div>
    );
  }
  if (type === 1 && text) {
    return (
      <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-kumo-recessed text-sm font-semibold text-kumo-strong">
        {text.slice(0, 1)}
      </div>
    );
  }
  if (type === 3 && text) {
    return (
      <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-kumo-recessed text-lg">
        {text}
      </div>
    );
  }
  return (
    <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-kumo-recessed text-kumo-subtle">
      <Bookmark className="h-5 w-5" />
    </div>
  );
}

function PublicBookmarksPage({ domainOnly = false, onDomainNotFound }) {
  const slug = useMemo(() => normalizePublicPath(), []);
  const surfaceRef = useCloudflareSpotlight();
  const isAuthenticated = useStore((state) => state.isAuthenticated);
  const [page, setPage] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const endpoint = slug && !domainOnly
        ? `/api/bookmarks/public/groups/${encodeURIComponent(slug)}`
        : `/api/bookmarks/public/page-by-domain?domain=${encodeURIComponent(window.location.host)}`;
      const response = await fetch(endpoint, { cache: 'no-store' });
      const result = await response.json().catch(() => ({}));
      if (!response.ok || result.success === false) {
        const err = new Error(result.error || '网址分组不存在或未公开');
        err.status = response.status;
        throw err;
      }
      setPage(result.data?.group || result.data || result);
    } catch (err) {
      if (!slug && domainOnly && err.status === 404 && onDomainNotFound) {
        onDomainNotFound();
        return;
      }
      setError(err.message || '公开页加载失败');
      setPage(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  useEffect(() => {
    if (!page?.title) return undefined;
    const previousTitle = document.title;
    document.title = page.title;
    return () => {
      document.title = previousTitle;
    };
  }, [page?.title]);

  useEffect(() => swapPublicPageFavicon(getPublicPageFaviconHref('bookmarks', page?.config)), [page?.config]);

  const updateGroupIcon = async (iconId) => {
    if (!page?.id) return;
    const response = await fetch(`/api/bookmarks/groups/${page.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: page.title,
        description: page.description || '',
        icon: page.icon || '',
        public: true,
        slug: page.slug || '',
        domain: page.domain || '',
        cache_seconds: page.cache_seconds || 300,
        config: withPublicPageIconId(page.config, iconId),
      }),
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.success === false) {
      throw new Error(result.error || '保存图标失败');
    }
    setPage((current) => (current ? { ...current, config: withPublicPageIconId(current.config, iconId) } : current));
    toast.success(iconId ? '分组图标已更新' : '已恢复默认图标');
  };

  const openItem = (item) => {
    const raw = item?.url || '';
    if (/^https?:\/\//i.test(raw)) {
      window.open(raw, '_blank', 'noopener,noreferrer');
    }
  };

  const items = Array.isArray(page?.items) ? page.items : [];

  return (
    <div ref={surfaceRef} className="cf-ai-background-surface public-status-page relative isolate min-h-screen text-kumo-default">
      <div aria-hidden="true" className="cf-ai-background pointer-events-none absolute inset-0" />
      <main className="relative z-10 mx-auto flex min-h-screen w-full max-w-5xl flex-col px-4 py-6 sm:px-6 lg:px-8">
        <div className="mb-6 flex items-center justify-between gap-4">
          <div className="flex min-w-0 items-center gap-3">
            <PublicPageIconPicker
              pageKind="bookmarks"
              config={page?.config}
              isAuthenticated={isAuthenticated}
              onChange={updateGroupIcon}
              triggerClassName="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-kumo-interact/80 bg-kumo-base text-brand"
              iconClassName="h-5 w-5"
            />
            <div className="min-w-0">
              <div className="truncate text-base font-semibold text-kumo-strong">{page?.title || '网址导航'}</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="secondary" onClick={load} loading={loading} icon={<RefreshCw className="h-3.5 w-3.5" />}>
              刷新
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => { window.location.href = '/'; }}
              icon={isAuthenticated ? <Home className="h-3.5 w-3.5" /> : <LogIn className="h-3.5 w-3.5" />}
              aria-label={isAuthenticated ? '主页' : '登录'}
              title={isAuthenticated ? '跳转到主页' : '跳转到登录页'}
            >
              {isAuthenticated ? '主页' : '登录'}
            </Button>
          </div>
        </div>

        {loading && (
          <div className="flex flex-1 items-center justify-center py-24">
            <Loader size={28} />
          </div>
        )}

        {!loading && error && (
          <div className="public-status-card flex flex-1 flex-col items-center justify-center rounded-lg border border-kumo-interact/80 bg-kumo-base p-10 text-center">
            <AlertTriangle className="mb-3 h-9 w-9 text-kumo-warning" />
            <h1 className="text-lg font-semibold text-kumo-strong">无法显示网址导航</h1>
            <p className="mt-2 max-w-md text-sm leading-relaxed text-kumo-subtle">{error}</p>
            {!slug && isLocalHost(window.location.host) && (
              <p className="mt-2 text-xs text-kumo-subtle">本地访问请使用 /bookmarks/slug。</p>
            )}
          </div>
        )}

        {!loading && page && (
          <div className="flex flex-col gap-4">
            <section className="public-status-card rounded-lg border border-kumo-interact/80 bg-kumo-base px-4 py-3.5">
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                  <div className="text-base font-semibold text-kumo-strong">{page.title}</div>
                  {page.description && (
                    <p className="mt-1.5 max-w-2xl text-sm leading-relaxed text-kumo-subtle">{page.description}</p>
                  )}
                </div>
                <Badge variant="secondary" className="h-7 w-fit shrink-0 justify-center gap-1 !text-[11px] font-semibold">
                  <Bookmark className="h-3 w-3" />
                  {items.length} 个网址
                </Badge>
              </div>
            </section>

            <section className="public-status-card overflow-hidden rounded-lg border border-kumo-interact/80 bg-kumo-base">
              <div className="flex items-center justify-between gap-3 border-b border-kumo-interact/70 px-4 py-3">
                <h2 className="text-sm font-semibold text-kumo-strong">快速访问</h2>
              </div>
              {items.length === 0 ? (
                <div className="p-8 text-center text-sm text-kumo-subtle">这个分组还没有公开的网址。</div>
              ) : (
                <div className="grid gap-2.5 p-3 cq-sm:grid-cols-2 cq-lg:grid-cols-3 cq-xl:grid-cols-4">
                  {items.map((item) => (
                    <div
                      key={item.id}
                      role="button"
                      tabIndex={0}
                      className="group flex cursor-pointer items-center gap-3 rounded-xl border border-kumo-interact/70 bg-kumo-base p-3 transition hover:border-kumo-interact hover:bg-kumo-recessed/50"
                      onClick={() => openItem(item)}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault();
                          openItem(item);
                        }
                      }}
                      title={item.description || item.url}
                    >
                      <PublicItemIcon item={item} />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                          <span className="truncate text-sm font-medium text-kumo-strong">{item.title}</span>
                          <ExternalLink className="h-3 w-3 shrink-0 text-kumo-subtle opacity-0 transition group-hover:opacity-100" />
                        </div>
                        {item.description && (
                          <div className="mt-0.5 truncate text-xs text-kumo-subtle">{item.description}</div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </section>

            <footer className="flex flex-col gap-2 py-4 text-xs text-kumo-subtle sm:flex-row sm:items-center sm:justify-between">
              <span className="inline-flex items-center gap-1">
                <img src="/logo.svg" className="h-3.5 w-3.5 object-contain" alt="" />
                由 API Monitor 提供
              </span>
              {page.updated_at && <span>最后更新：{formatDateTime(page.updated_at || page.created_at)}</span>}
            </footer>
          </div>
        )}
      </main>
    </div>
  );
}

export default PublicBookmarksPage;