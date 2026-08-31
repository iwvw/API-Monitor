import React, { useEffect, useMemo, useState } from 'react';
import { ClipboardText } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Switch } from '@cloudflare/kumo/components/switch';
import { toast } from '../../modules/toast.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { invalidateDashboardStats } from '../../modules/dashboardInvalidation.js';
import useStore from '../../store.js';
import { SectionCard } from '../ui/AppPrimitives.jsx';
import { PublicPageBrandIcon } from '../public/PublicPageIconPicker.jsx';
import {
  Copy,
  Edit,
  ExternalLink,
  Globe,
  RefreshCw,
  Save,
  Trash,
  X,
} from '../Icons.jsx';

const createEmptyGitHubPublicPageForm = () => ({
  id: null,
  title: '',
  slug: '',
  domain: '',
  description: '',
  public: true,
  cacheSeconds: 300,
  showRepoLinks: true,
  showDescriptions: true,
  showRepositoryStats: true,
  showOnDashboard: true,
  publicIconId: '',
  repositoryIds: [],
});

const normalizeGitHubPublicSlug = (value, fallback = 'github') => {
  const text = String(value || fallback).trim().toLowerCase();
  const slug = text.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
  return slug || fallback;
};

const normalizeGitHubPublicDomain = (value) => (
  String(value || '')
    .trim()
    .replace(/^https?:\/\//i, '')
    .split('/')[0]
    .replace(/\/+$/g, '')
);

const getGitHubPublicPageBaseOrigin = () => {
  const configured = String(useStore.getState().publicApiUrl || '').trim().replace(/\/+$/g, '');
  return configured || window.location.origin;
};

const getGitHubPublicPageUrl = (pageOrForm, mode = 'github') => {
  const slug = normalizeGitHubPublicSlug(pageOrForm?.slug || pageOrForm?.title || 'github');
  return `${getGitHubPublicPageBaseOrigin()}/${mode}/${encodeURIComponent(slug)}`;
};

const getGitHubPublicDomainUrl = (pageOrForm) => {
  const domain = normalizeGitHubPublicDomain(pageOrForm?.domain);
  return domain ? `https://${domain}` : '';
};

function GitHubPublicPagesPanel({ repositories = [] }) {
  const { isArmed, confirmPress } = useConfirmPress();
  const [pages, setPages] = useState([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState(() => createEmptyGitHubPublicPageForm());

  const repositoryMap = useMemo(
    () => new Map(repositories.map((repo) => [String(repo.id), repo])),
    [repositories],
  );

  const api = async (path, options = {}) => {
    const response = await fetch(path, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        ...(options.headers || {}),
      },
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.success === false) {
      throw new Error(result.error || `请求失败: ${response.status}`);
    }
    return result.data !== undefined ? result.data : result;
  };

  const loadPages = async () => {
    setLoading(true);
    try {
      const result = await api('/api/github/public-pages');
      setPages(Array.isArray(result) ? result : []);
    } catch (error) {
      toast.error(error.message || '加载 GitHub 公开页失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadPages();
  }, []);

  useEffect(() => {
    setForm((current) => ({
      ...current,
      repositoryIds: current.repositoryIds.filter((id) => repositoryMap.has(String(id))),
    }));
  }, [repositoryMap]);

  const resetForm = () => setForm(createEmptyGitHubPublicPageForm());

  const toggleRepository = (id, checked) => {
    setForm((current) => ({
      ...current,
      repositoryIds: checked
        ? Array.from(new Set([...current.repositoryIds, id]))
        : current.repositoryIds.filter((item) => String(item) !== String(id)),
    }));
  };

  const editPage = (page) => {
    const config = page?.config || {};
    setForm({
      id: page.id,
      title: page.title || '',
      slug: page.slug || '',
      domain: page.domain || '',
      description: page.description || '',
      public: page.public !== false,
      cacheSeconds: page.cacheSeconds || 300,
      showRepoLinks: config.showRepoLinks !== false,
      showDescriptions: config.showDescriptions !== false,
      showRepositoryStats: config.showRepositoryStats !== false,
      showOnDashboard: !!config.showOnDashboard,
      publicIconId: String(config.publicIconId || '').trim(),
      repositoryIds: Array.isArray(page.repositoryIds) ? page.repositoryIds : [],
    });
  };

  const savePage = async () => {
    const title = form.title.trim();
    const slug = normalizeGitHubPublicSlug(form.slug || title);
    if (!title) {
      toast.warning('请填写公开页名称');
      return;
    }
    if (form.repositoryIds.length === 0) {
      toast.warning('请至少绑定一个仓库');
      return;
    }

    setSaving(true);
    try {
      const payload = {
        title,
        slug,
        domain: normalizeGitHubPublicDomain(form.domain),
        description: form.description.trim(),
        public: !!form.public,
        cacheSeconds: Math.max(30, Number(form.cacheSeconds) || 300),
        repositoryIds: form.repositoryIds,
        config: {
          showRepoLinks: !!form.showRepoLinks,
          showDescriptions: !!form.showDescriptions,
          showRepositoryStats: !!form.showRepositoryStats,
          showOnDashboard: !!form.showOnDashboard,
          ...(form.publicIconId ? { publicIconId: form.publicIconId } : {}),
        },
      };
      const endpoint = form.id ? `/api/github/public-pages/${form.id}` : '/api/github/public-pages';
      await api(endpoint, {
        method: form.id ? 'PUT' : 'POST',
        body: JSON.stringify(payload),
      });
      invalidateDashboardStats('github-public-pages:save');
      toast.success(form.id ? 'GitHub 公开页已保存' : 'GitHub 公开页已创建');
      resetForm();
      await loadPages();
    } catch (error) {
      toast.error(error.message || '保存 GitHub 公开页失败');
    } finally {
      setSaving(false);
    }
  };

  const deletePage = async (page) => {
    if (!confirmPress(`github-public-page:${page.id}`, `删除公开页「${page?.title || page?.slug || String(page?.id)}」`)) return;
    try {
      await api(`/api/github/public-pages/${page.id}`, { method: 'DELETE' });
      invalidateDashboardStats('github-public-pages:delete');
      toast.success('GitHub 公开页已删除');
      if (form.id === page.id) resetForm();
      await loadPages();
    } catch (error) {
      toast.error(error.message || '删除 GitHub 公开页失败');
    }
  };

  return (
    <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(24rem,0.92fr)_minmax(0,1.08fr)]">
      <SectionCard
        title={form.id ? '编辑 GitHub 公开页' : '新建 GitHub 公开页'}
        icon={<Globe className="h-4 w-4 text-brand" />}
        action={form.id ? (
          <Button size="sm" variant="secondary" shape="square" icon={<X className="h-3.5 w-3.5" />} onClick={resetForm} aria-label="取消编辑" />
        ) : null}
        bodyPadding="lg"
        bodyClassName="space-y-4"
      >
        <div className="grid gap-3 cq-sm:grid-cols-2">
          <Input
            size="sm"
            label="名称"
            value={form.title}
            onChange={(event) => setForm((current) => ({
              ...current,
              title: event.target.value,
              slug: current.slug || normalizeGitHubPublicSlug(event.target.value),
            }))}
            placeholder="GitHub 最新动态"
          />
          <Input
            size="sm"
            label="Slug"
            value={form.slug}
            onChange={(event) => setForm((current) => ({ ...current, slug: normalizeGitHubPublicSlug(event.target.value) }))}
            placeholder="github"
          />
          <Input
            size="sm"
            label="自定义域名"
            value={form.domain}
            onChange={(event) => setForm((current) => ({ ...current, domain: normalizeGitHubPublicDomain(event.target.value) }))}
            placeholder="github.example.com"
          />
          <Input
            size="sm"
            label="缓存秒数"
            type="number"
            min="30"
            value={form.cacheSeconds}
            onChange={(event) => setForm((current) => ({ ...current, cacheSeconds: event.target.value }))}
          />
          <div className="cq-sm:col-span-2">
            <Textarea
              size="sm"
              label="说明"
              value={form.description}
              onChange={(event) => setForm((current) => ({ ...current, description: event.target.value }))}
              placeholder="可选说明"
              rows={3}
            />
          </div>
        </div>

        <div className="grid gap-2 cq-sm:grid-cols-2">
          {[
            ['public', '公开访问', '关闭后公开页不可访问。'],
            ['showRepoLinks', '仓库跳转', '允许跳转到 GitHub 仓库。'],
            ['showDescriptions', '显示描述', '显示仓库描述和 workflow 摘要。'],
            ['showRepositoryStats', '显示统计', '显示星标、复刻、议题等统计。'],
            ['showOnDashboard', '首页快捷卡片', '在仪表盘显示入口。'],
          ].map(([key, title, description]) => (
            <div key={key} className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
              <div className="min-w-0">
                <div className="text-sm font-semibold text-kumo-strong">{title}</div>
                <div className="mt-1 text-xs text-kumo-subtle">{description}</div>
              </div>
              <Switch checked={!!form[key]} onCheckedChange={(checked) => setForm((current) => ({ ...current, [key]: checked }))} />
            </div>
          ))}
        </div>

        <div className="space-y-2">
          <div className="flex items-center justify-between gap-2">
            <div className="text-sm font-semibold text-kumo-strong">绑定仓库</div>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setForm((current) => ({ ...current, repositoryIds: repositories.map((repo) => repo.id) }))}
              disabled={repositories.length === 0}
            >
              全选
            </Button>
          </div>
          <div className="max-h-72 overflow-y-auto rounded-lg border border-kumo-line bg-kumo-base p-2 scrollbar-thin">
            {repositories.length === 0 ? (
              <div className="p-4 text-center text-xs text-kumo-subtle">暂无 GitHub 仓库，请先添加。</div>
            ) : (
              <div className="grid gap-1.5">
                {repositories.map((repo) => (
                  <label key={repo.id} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-kumo-recessed">
                    <Checkbox
                      checked={form.repositoryIds.includes(repo.id)}
                      onCheckedChange={(checked) => toggleRepository(repo.id, checked)}
                      aria-label={`绑定 ${repo.full_name}`}
                    />
                    <span className="min-w-0 flex-1 truncate text-sm text-kumo-strong">{repo.full_name}</span>
                    <span className="hidden max-w-[12rem] truncate text-[10px] text-kumo-subtle cq-sm:block">
                      {repo.private ? '私有仓库' : '公开仓库'}
                    </span>
                  </label>
                ))}
              </div>
            )}
          </div>
        </div>

        <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-xs text-kumo-subtle">
          <div className="font-semibold text-kumo-strong">预览地址</div>
          <div className="mt-2 space-y-1 font-mono">
            <div className="truncate">{getGitHubPublicPageUrl(form, 'github')}</div>
            <div className="truncate">{getGitHubPublicPageUrl(form, 'gh')}</div>
            {getGitHubPublicDomainUrl(form) && <div className="truncate">{getGitHubPublicDomainUrl(form)}</div>}
          </div>
        </div>

        <div className="flex flex-wrap justify-end gap-2">
          <Button size="sm" variant="secondary" onClick={resetForm}>重置</Button>
          <Button size="sm" variant="primary" loading={saving} onClick={savePage} icon={<Save className="h-3.5 w-3.5" />}>
            {form.id ? '保存公开页' : '创建公开页'}
          </Button>
        </div>
      </SectionCard>

      <SectionCard
        title="已发布公开页"
        icon={<Globe className="h-4 w-4 text-brand" />}
        className="self-start"
        actions={(
          <Button size="sm" variant="secondary" icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={loadPages} loading={loading}>
            刷新
          </Button>
        )}
        bodyPadding="lg"
        bodyClassName="space-y-4"
      >
        {loading && pages.length === 0 ? (
          <div className="space-y-3">
            {Array.from({ length: 3 }).map((_, index) => <SkeletonLine key={index} className="h-16 w-full" />)}
          </div>
        ) : pages.length === 0 ? (
          <div className="flex min-h-56 flex-col items-center justify-center rounded-lg border border-dashed border-kumo-line text-center text-sm text-kumo-subtle">
            <Globe className="mb-3 h-8 w-8 opacity-40" />
            暂无 GitHub 公开页
          </div>
        ) : (
          <div className="grid gap-3">
            {pages.map((page) => {
              const publicUrl = getGitHubPublicPageUrl(page, 'github');
              const compactUrl = getGitHubPublicPageUrl(page, 'gh');
              const domainUrl = getGitHubPublicDomainUrl(page);
              const boundRepositories = (Array.isArray(page.repositoryIds) ? page.repositoryIds : [])
                .map((id) => repositoryMap.get(String(id))?.full_name)
                .filter(Boolean);
              return (
                <div key={page.id} className="rounded-lg border border-kumo-line bg-kumo-base p-3">
                  <div className="flex flex-col gap-3 cq-sm:flex-row cq-sm:items-start cq-sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-brand/10 text-brand">
                          <PublicPageBrandIcon pageKind="github" config={page.config} iconClassName="h-4 w-4" customIconClassName="h-4 w-4" />
                        </span>
                        <span className="truncate text-sm font-semibold text-kumo-strong">{page.title || page.slug}</span>
                        <span className={`rounded px-2 py-0.5 text-[10px] font-semibold ${page.public ? 'bg-kumo-success/10 text-kumo-success' : 'bg-kumo-line/30 text-kumo-subtle'}`}>
                          {page.public ? '公开' : '私有'}
                        </span>
                        <span className="rounded bg-kumo-recessed px-2 py-0.5 font-mono text-[10px] text-kumo-subtle">{page.cacheSeconds || 300}s</span>
                      </div>
                      <div className="mt-1 truncate font-mono text-xs text-kumo-subtle">{page.slug}</div>
                      {page.description && <div className="mt-2 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">{page.description}</div>}
                      {boundRepositories.length > 0 && (
                        <div className="mt-2 truncate text-[11px] text-kumo-subtle" title={boundRepositories.join(', ')}>
                          绑定仓库：{boundRepositories.join(', ')}
                        </div>
                      )}
                    </div>
                    <div className="flex shrink-0 flex-wrap gap-2">
                      <Button size="sm" variant="secondary" shape="square" icon={<Edit className="h-3.5 w-3.5" />} onClick={() => editPage(page)} aria-label="编辑公开页" />
                      <Button size="sm" variant="secondary" shape="square" icon={<ExternalLink className="h-3.5 w-3.5" />} onClick={() => window.open(publicUrl, '_blank', 'noopener,noreferrer')} aria-label="打开公开页" />
                      <Button size="sm" variant="secondary" shape="square" icon={<Copy className="h-3.5 w-3.5" />} onClick={() => navigator.clipboard.writeText(publicUrl).then(() => toast.success('公开地址已复制')).catch(() => toast.error('复制公开地址失败'))} aria-label="复制公开页地址" />
                      <Button size="sm" variant={isArmed(`github-public-page:${page.id}`) ? 'destructive' : 'secondary-destructive'} shape="square" icon={<Trash className="h-3.5 w-3.5" />} onClick={() => deletePage(page)} aria-label="删除公开页" />
                    </div>
                  </div>
                  <div className="mt-3 grid gap-2 text-xs">
                    <ClipboardText size="sm" text={publicUrl} className="min-w-0 w-full" tooltip={{ text: '复制公开页地址', copiedText: '地址已复制' }} />
                    <ClipboardText size="sm" text={compactUrl} className="min-w-0 w-full" tooltip={{ text: '复制 /gh 地址', copiedText: '地址已复制' }} />
                    {domainUrl && (
                      <ClipboardText size="sm" text={domainUrl} className="min-w-0 w-full" tooltip={{ text: '复制自定义域名', copiedText: '地址已复制' }} />
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </SectionCard>
    </div>
  );
}

export default GitHubPublicPagesPanel;
