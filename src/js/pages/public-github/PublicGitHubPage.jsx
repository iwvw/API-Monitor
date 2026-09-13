import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import PublicPageIconPicker from '../../components/public/PublicPageIconPicker.jsx';
import { useCloudflareSpotlight } from '../../hooks/useCloudflareSpotlight.js';
import PublicOverviewStats from '../../components/public/PublicOverviewStats.jsx';
import {
  getPublicGithubDataUpdatedAt,
  getPublicGithubRefreshInterval,
  hasPublicGithubWorkflowDetail,
  mergePublicGithubRepositories,
  shouldLoadPublicGithubRepositoryDetail,
} from '../../modules/githubPublicRealtime.js';
import {
  getPublicPageFaviconHref,
  swapPublicPageFavicon,
  withPublicPageIconId,
} from '../../modules/publicPageBranding.js';
import { toast } from '../../modules/toast.js';
import { useNowTick } from '../../modules/usePageVisibility.js';
import useStore from '../../store.js';
import {
  AlertTriangle,
  Globe,
  Home,
  LogIn,
  RefreshCw,
} from '../../components/Icons.jsx';
import { statusPanelClass, statusTone } from './constants.js';
import { formatDateTime, normalizePublicPath, parsePublicGithubStreamPayload } from './utils.js';
import RepositoryCard from './RepositoryCard.jsx';
import { ActionFlowPlaceholder } from './ActionWorkflowCanvas.jsx';

function PublicGitHubPage({ domainOnly = false, onDomainNotFound }) {
  const isAuthenticated = useStore((state) => state.isAuthenticated);
  const slug = useMemo(() => normalizePublicPath(), []);
  const surfaceRef = useCloudflareSpotlight();
  const [page, setPage] = useState(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');
  const currentTime = useNowTick(1000);
  const [detailStatusByRepo, setDetailStatusByRepo] = useState({});
  const [repositoryFilter, setRepositoryFilter] = useState('all');
  const pageRef = useRef(null);
  const detailRequestSeqRef = useRef({});
  const loadRequestSeqRef = useRef(0);
  const selectedRunsRef = useRef({});

  useEffect(() => {
    pageRef.current = page;
  }, [page]);

  const refreshRepositoryDetail = useCallback(async (pageSlug, repositoryID, { markLoading = true, runId = '' } = {}) => {
    const repoId = String(repositoryID || '');
    if (!pageSlug || !repoId) return;

    const nextRequestSeq = (detailRequestSeqRef.current[repoId] || 0) + 1;
    detailRequestSeqRef.current = { ...detailRequestSeqRef.current, [repoId]: nextRequestSeq };

    if (markLoading) {
      setDetailStatusByRepo((current) => ({ ...current, [repoId]: 'loading' }));
    }

    try {
      const params = new URLSearchParams();
      if (runId) params.set('run_id', String(runId));
      const response = await fetch(`/api/github/public/pages/${encodeURIComponent(pageSlug)}/repositories/${encodeURIComponent(repoId)}${params.toString() ? `?${params}` : ''}`, {
        cache: 'no-store',
      });
      const result = await response.json().catch(() => ({}));
      if (detailRequestSeqRef.current[repoId] !== nextRequestSeq) return;
      if (!response.ok || result.success === false) {
        throw new Error(result.error || 'Workflow 详情加载失败');
      }

      const detail = result.data || result;
      setPage((current) => {
        if (!current || current.slug !== pageSlug) return current;
        const repositories = Array.isArray(current.repositories) ? current.repositories : [];
        let changed = false;
        const nextRepositories = repositories.map((item) => {
          if (String(item?.id || '') !== repoId) return item;
          changed = true;
          return { ...item, ...detail };
        });
        return changed ? { ...current, repositories: nextRepositories } : current;
      });
      setDetailStatusByRepo((current) => ({ ...current, [repoId]: 'loaded' }));
    } catch (detailError) {
      if (detailRequestSeqRef.current[repoId] !== nextRequestSeq) return;
      setPage((current) => {
        if (!current || current.slug !== pageSlug) return current;
        const repositories = Array.isArray(current.repositories) ? current.repositories : [];
        let changed = false;
        const nextRepositories = repositories.map((item) => {
          if (String(item?.id || '') !== repoId) return item;
          changed = true;
          return {
            ...item,
            workflow_error: detailError.message || 'Workflow 详情加载失败',
          };
        });
        return changed ? { ...current, repositories: nextRepositories } : current;
      });
      setDetailStatusByRepo((current) => ({ ...current, [repoId]: 'failed' }));
    }
  }, []);

  const loadRepositoryDetails = useCallback(async (nextPage) => {
    const repositories = Array.isArray(nextPage?.repositories) ? nextPage.repositories : [];
    if (!nextPage?.slug || repositories.length === 0) {
      detailRequestSeqRef.current = {};
      setDetailStatusByRepo({});
      return;
    }

    const nextStatuses = {};
    const targets = [];
    repositories.forEach((repo) => {
      const repoId = String(repo?.id || '');
      if (!String(repo?.latest_run?.run_id || '')) {
        nextStatuses[repoId] = 'idle';
        return;
      }
      if (hasPublicGithubWorkflowDetail(repo)) {
        nextStatuses[repoId] = 'loaded';
        return;
      }
      nextStatuses[repoId] = 'loading';
      targets.push(repoId);
    });
    setDetailStatusByRepo(nextStatuses);

    await Promise.allSettled(targets.map((repoId) => (
      refreshRepositoryDetail(nextPage.slug, repoId, { markLoading: false })
    )));
  }, [refreshRepositoryDetail]);

  const syncRepositories = useCallback(async ({
    pageSlug,
    repositories,
    markLoading = false,
    onlyMissingDetail = false,
  }) => {
    if (!pageSlug) return;
    const targets = (Array.isArray(repositories) ? repositories : [])
      .map((repo) => ({ repoId: String(repo?.id || ''), repo }))
      .filter(({ repoId, repo }) => repoId && (!onlyMissingDetail || shouldLoadPublicGithubRepositoryDetail(repo)));

    if (targets.length === 0) return;

    await Promise.allSettled(targets.map(({ repoId }) => (
      refreshRepositoryDetail(pageSlug, repoId, { markLoading, runId: selectedRunsRef.current[repoId] || '' })
    )));
  }, [refreshRepositoryDetail]);

  const loadSummary = useCallback(async ({ silent = false, showRefreshing = silent } = {}) => {
    const requestSeq = loadRequestSeqRef.current + 1;
    loadRequestSeqRef.current = requestSeq;
    if (!silent) setLoading(true);
    else if (showRefreshing) setRefreshing(true);
    setError('');
    try {
      const endpoint = slug && !domainOnly
        ? `/api/github/public/pages/${encodeURIComponent(slug)}?summary=1`
        : `/api/github/public/page-by-domain?domain=${encodeURIComponent(window.location.host)}&summary=1`;
      const response = await fetch(endpoint, { cache: 'no-store' });
      const result = await response.json().catch(() => ({}));
      if (loadRequestSeqRef.current !== requestSeq) return;
      if (!response.ok || result.success === false) {
        const nextError = new Error(result.error || 'GitHub 公开页不存在或未公开');
        nextError.status = response.status;
        throw nextError;
      }
      const previousRepositories = Array.isArray(pageRef.current?.repositories) ? pageRef.current.repositories : [];
      const rawPage = result.data || result;
      const mergedPage = {
        ...rawPage,
        repositories: mergePublicGithubRepositories(
          Array.isArray(rawPage?.repositories) ? rawPage.repositories : [],
          previousRepositories,
        ),
      };
      setPage(mergedPage);
      return mergedPage;
    } catch (err) {
      if (loadRequestSeqRef.current !== requestSeq) return;
      if (!slug && domainOnly && err.status === 404 && onDomainNotFound) {
        onDomainNotFound();
        return;
      }
      setError(err.message || 'GitHub 公开页加载失败');
      if (!silent) {
        setPage(null);
        detailRequestSeqRef.current = {};
        setDetailStatusByRepo({});
      }
    } finally {
      if (loadRequestSeqRef.current === requestSeq) {
        if (!silent) setLoading(false);
        if (showRefreshing) setRefreshing(false);
      }
    }
    return null;
  }, [domainOnly, onDomainNotFound, slug]);

  const load = useCallback(async ({ silent = false, showRefreshing = silent } = {}) => {
    const nextPage = await loadSummary({ silent, showRefreshing });
    if (!nextPage) return;
    if (silent) {
      void syncRepositories({
        pageSlug: nextPage.slug,
        repositories: nextPage.repositories,
      });
      return;
    }
    void loadRepositoryDetails(nextPage);
  }, [loadRepositoryDetails, loadSummary, syncRepositories]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!page?.title) return undefined;
    const previousTitle = document.title;
    document.title = page.title;
    return () => {
      document.title = previousTitle;
    };
  }, [page?.title]);

  useEffect(() => swapPublicPageFavicon(getPublicPageFaviconHref('github', page?.config)), [page?.config]);

  useEffect(() => {
    if (typeof window.EventSource === 'function') return undefined;
    const interval = window.setInterval(() => {
      if (document.hidden) return;
      void loadSummary({ silent: true, showRefreshing: false }).then((nextPage) => {
        const currentPage = nextPage || pageRef.current;
        if (!currentPage?.slug) return;
        void syncRepositories({
          pageSlug: currentPage.slug,
          repositories: currentPage.repositories,
        });
      });
    }, getPublicGithubRefreshInterval(page));
    return () => window.clearInterval(interval);
  }, [loadSummary, page, syncRepositories]);

  useEffect(() => {
    if (!page?.slug || typeof window.EventSource !== 'function') return undefined;
    const source = new window.EventSource(`/api/github/public/pages/${encodeURIComponent(page.slug)}/stream`);
    const refreshRepository = (event) => {
      const payload = parsePublicGithubStreamPayload(event);
      const kind = String(payload?.kind || '');
      const repoId = String(payload?.repository_id || '');
      if (!repoId || !['repository_refresh', 'repository_actions_refresh'].includes(kind)) return;
      void loadSummary({ silent: true, showRefreshing: false }).then((nextPage) => {
        if (!nextPage) return;
        return refreshRepositoryDetail(nextPage.slug, repoId, {
          markLoading: false,
          runId: selectedRunsRef.current[repoId] || '',
        });
      });
    };
    source.addEventListener('github', refreshRepository);
    return () => {
      source.removeEventListener('github', refreshRepository);
      source.close();
    };
  }, [loadSummary, page?.slug, refreshRepositoryDetail]);

  const repositories = Array.isArray(page?.repositories) ? page.repositories : [];
  const handleSelectRun = useCallback((repoId, runId) => {
    const normalizedRepoId = String(repoId || '');
    const normalizedRunId = String(runId || '');
    selectedRunsRef.current = { ...selectedRunsRef.current, [normalizedRepoId]: normalizedRunId };
    refreshRepositoryDetail(slug, repoId, { markLoading: true, runId: normalizedRunId });
  }, [refreshRepositoryDetail, slug]);
  const dataUpdatedAt = getPublicGithubDataUpdatedAt(page);
  const failureCount = repositories.filter((repo) => statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status) === 'error').length;
  const warningCount = repositories.filter((repo) => statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status) === 'warning').length;
  const successCount = repositories.filter((repo) => statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status) === 'success').length;
  const neutralCount = Math.max(0, repositories.length - failureCount - warningCount - successCount);
  const visibleRepositories = repositoryFilter === 'success'
    ? repositories.filter((repo) => statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status) === 'success')
    : repositoryFilter === 'failure'
      ? repositories.filter((repo) => statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status) === 'error')
      : repositoryFilter === 'other'
        ? repositories.filter((repo) => !['success', 'error'].includes(statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status)))
        : repositories;
  const pageTone = repositories.length === 0 ? 'neutral' : failureCount > 0 ? 'danger' : warningCount > 0 ? 'warning' : 'success';
  const summaryText = repositories.length === 0
    ? '暂无公开仓库'
    : failureCount > 0
    ? `${failureCount} 个仓库的最新工作流存在失败`
    : warningCount > 0
    ? `${warningCount} 个仓库的最新工作流仍在运行或等待`
    : '全部仓库的最新工作流状态正常';
  const config = page?.config || {};

  const updatePageIcon = async (iconId) => {
    if (!page?.id) return;
    const response = await fetch(`/api/github/public-pages/${page.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        config: withPublicPageIconId(page.config, iconId),
      }),
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.success === false) {
      throw new Error(result.error || '保存 GitHub 公开页图标失败');
    }
    setPage((current) => (current ? {
      ...current,
      config: withPublicPageIconId(current.config, iconId),
    } : current));
    toast.success(iconId ? 'GitHub 公开页图标已更新' : '已恢复 GitHub 公开页默认图标');
  };

  return (
    <div ref={surfaceRef} className="cf-ai-background-surface public-github-page relative isolate min-h-screen text-kumo-default">
      <div aria-hidden="true" className="cf-ai-background pointer-events-none absolute inset-0" />
      <main className="relative z-10 mx-auto flex min-h-screen w-full max-w-[96rem] flex-col px-4 py-5 sm:px-6 lg:px-8">
        <div className="mb-4 flex items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2.5">
            <PublicPageIconPicker
              pageKind="github"
              config={page?.config}
              isAuthenticated={isAuthenticated}
              onChange={updatePageIcon}
              triggerClassName="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-kumo-interact/80 bg-kumo-base text-brand"
              iconClassName="h-5 w-5"
            />
            <div className="min-w-0">
              <div className="truncate text-base font-semibold text-kumo-strong">{page?.title || 'GitHub 动态'}</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="secondary" onClick={() => load({ silent: true })} loading={refreshing} icon={<RefreshCw className="h-3.5 w-3.5" />}>
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

        {loading && !page && (
          <div className="flex flex-col gap-3">
            <section className="public-github-card rounded-lg border border-kumo-interact/80 bg-kumo-base px-4 py-3.5">
              <div className="grid gap-3">
                <SkeletonLine className="h-5 w-48" />
                <SkeletonLine className="h-4 w-72" />
              </div>
            </section>
            <ActionFlowPlaceholder />
          </div>
        )}

        {!loading && error && !page && (
          <div className="public-github-card flex flex-1 flex-col items-center justify-center rounded-lg border border-kumo-interact/80 bg-kumo-base p-10 text-center">
            <AlertTriangle className="mb-3 h-9 w-9 text-kumo-warning" />
            <h1 className="text-lg font-semibold text-kumo-strong">无法显示 GitHub 公开页</h1>
            <p className="mt-2 max-w-md text-sm leading-relaxed text-kumo-subtle">{error}</p>
          </div>
        )}

        {page && (
          <div className="flex flex-col gap-3">
            <section className={`public-github-card rounded-lg border px-4 py-3 ${statusPanelClass[pageTone]}`}>
              <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div className="flex items-center gap-2 text-base font-semibold">
                    <Globe className="h-4 w-4" />
                    {summaryText}
                  </div>
                  {page.description && (
                    <p className="mt-1.5 max-w-3xl text-[13px] leading-5 opacity-90">{page.description}</p>
                  )}
                </div>
                <PublicOverviewStats
                  activeKey={repositoryFilter}
                  onChange={setRepositoryFilter}
                  items={[
                    { key: 'all', label: '仓库', value: repositories.length },
                    { key: 'success', label: '正常', value: successCount },
                    { key: 'failure', label: '失败', value: failureCount },
                    { key: 'other', label: '其他', value: warningCount + neutralCount },
                  ]}
                />
              </div>
            </section>

            {repositories.length === 0 ? (
              <section className="public-github-card rounded-lg border border-kumo-interact/80 bg-kumo-base p-6 text-center text-sm text-kumo-subtle">
                这个 GitHub 公开页还没有绑定仓库。
              </section>
            ) : (
              <section className="grid gap-3">
                {visibleRepositories.map((item) => (
                  <RepositoryCard
                    key={item.id || item.full_name}
                    item={item}
                    now={currentTime}
                    config={config}
                    detailLoading={detailStatusByRepo[String(item.id || '')] === 'loading'}
                    onSelectRun={(runId) => handleSelectRun(item.id, runId)}
                  />
                ))}
                {visibleRepositories.length === 0 && (
                  <div className="rounded-lg border border-kumo-interact/70 bg-kumo-base p-6 text-center text-sm text-kumo-subtle">暂无匹配仓库。</div>
                )}
              </section>
            )}

            <footer className="flex flex-col gap-2 py-3 text-xs text-kumo-subtle sm:flex-row sm:items-center sm:justify-between">
              <span className="inline-flex items-center gap-1">
                <img src="/logo.svg" className="h-3.5 w-3.5 object-contain" alt="" />
                由 API Monitor 提供
              </span>
              <span>最后更新：{formatDateTime(dataUpdatedAt)}</span>
            </footer>
          </div>
        )}
      </main>
    </div>
  );
}

export default PublicGitHubPage;
