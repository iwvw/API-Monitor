import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Switch } from '@cloudflare/kumo/components/switch';
import { ClipboardText } from '@cloudflare/kumo';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { PublicPageBrandIcon } from '../../components/public/PublicPageIconPicker.jsx';
import { Copy, Edit, ExternalLink, Globe, Plus, RotateCw, Save, Trash, X } from '../../components/Icons.jsx';
import { normalizeStatusDomain, normalizeStatusSlug } from './utils.js';

function StatusPagesPanel({
  statusPageForm,
  setStatusPageForm,
  uptimeMonitors,
  uptimeStatusPages,
  uptimeMetaLoading,
  isArmed,
  getDisplayUrl,
  getStatusPagePublicUrl,
  getStatusPageDomainUrl,
  copyStatusUrl,
  editStatusPage,
  deleteStatusPage,
  saveStatusPage,
  resetStatusPageForm,
  toggleStatusPageMonitor,
  createDefaultStatusPage,
  loadUptimeStatusPages,
}) {
  return (
    <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(24rem,0.9fr)_minmax(0,1.1fr)]">
      <SectionCard
        title={statusPageForm.id ? '编辑状态页' : '新建状态页'}
        icon={<Globe className="h-4 w-4 text-brand" />}
        action={statusPageForm.id ? (
          <Button size="sm" variant="secondary" shape="square" icon={<X className="h-3.5 w-3.5" />} onClick={resetStatusPageForm} aria-label="取消编辑" />
        ) : null}
        bodyPadding="lg"
        bodyClassName="space-y-4"
      >

        <div className="grid gap-3 cq-sm:grid-cols-2">
          <Input
            size="sm"
            label="名称"
            value={statusPageForm.title}
            onChange={(event) => setStatusPageForm(prev => ({
              ...prev,
              title: event.target.value,
              slug: prev.slug || normalizeStatusSlug(event.target.value),
            }))}
            placeholder="DSUK Hub 状态"
          />
          <Input
            size="sm"
            label="Slug"
            value={statusPageForm.slug}
            onChange={(event) => setStatusPageForm(prev => ({ ...prev, slug: normalizeStatusSlug(event.target.value) }))}
            placeholder="demo"
          />
          <Input
            size="sm"
            label="自定义域名"
            value={statusPageForm.domain}
            onChange={(event) => setStatusPageForm(prev => ({ ...prev, domain: normalizeStatusDomain(event.target.value) }))}
            placeholder="status.example.com"
          />
          <Input
            size="sm"
            label="缓存秒数"
            type="number"
            min="30"
            value={statusPageForm.cacheSeconds}
            onChange={(event) => setStatusPageForm(prev => ({ ...prev, cacheSeconds: event.target.value }))}
          />
          <div className="cq-sm:col-span-2">
            <Textarea
              size="sm"
              label="说明"
              value={statusPageForm.description}
              onChange={(event) => setStatusPageForm(prev => ({ ...prev, description: event.target.value }))}
              rows={3}
            />
          </div>
        </div>

        <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
          <div className="min-w-0">
            <div className="text-sm font-semibold text-kumo-strong">公开访问</div>
            <div className="mt-1 text-xs text-kumo-subtle">关闭后公开 API 和单页都会返回不可用。</div>
          </div>
          <Switch checked={!!statusPageForm.public} onCheckedChange={(checked) => setStatusPageForm(prev => ({ ...prev, public: checked }))} />
        </div>

        <div className="grid gap-2 cq-sm:grid-cols-2">
          <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
            <div className="min-w-0">
              <div className="text-sm font-semibold text-kumo-strong">隐藏地址</div>
              <div className="mt-1 text-xs text-kumo-subtle">公开页不直接显示监测目标 URL。</div>
            </div>
            <Switch checked={!!statusPageForm.hideTargets} onCheckedChange={(checked) => setStatusPageForm(prev => ({ ...prev, hideTargets: checked }))} />
          </div>
          <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
            <div className="min-w-0">
              <div className="text-sm font-semibold text-kumo-strong">名称跳转</div>
              <div className="mt-1 text-xs text-kumo-subtle">点击服务名称打开对应网页。</div>
            </div>
            <Switch checked={!!statusPageForm.linkMonitorNames} onCheckedChange={(checked) => setStatusPageForm(prev => ({ ...prev, linkMonitorNames: checked }))} />
          </div>
          <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
            <div className="min-w-0">
              <div className="text-sm font-semibold text-kumo-strong">首页快捷卡片</div>
              <div className="mt-1 text-xs text-kumo-subtle">在仪表盘显示跳转到此状态页的快捷入口。</div>
            </div>
            <Switch checked={!!statusPageForm.showOnDashboard} onCheckedChange={(checked) => setStatusPageForm(prev => ({ ...prev, showOnDashboard: checked }))} />
          </div>
        </div>

        <div className="space-y-2">
          <div className="flex items-center justify-between gap-2">
            <div className="text-sm font-semibold text-kumo-strong">绑定监测目标</div>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setStatusPageForm(prev => ({ ...prev, monitorIds: uptimeMonitors.map(item => item.id) }))}
              disabled={uptimeMonitors.length === 0}
            >
              全选
            </Button>
          </div>
          <div className="max-h-64 overflow-y-auto rounded-lg border border-kumo-line bg-kumo-base p-2 scrollbar-thin">
            {uptimeMonitors.length === 0 ? (
              <div className="p-4 text-center text-xs text-kumo-subtle">暂无监测目标</div>
            ) : (
              <div className="grid gap-1.5">
                {uptimeMonitors.map((monitor) => (
                  <label key={monitor.id} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-kumo-recessed">
                    <Checkbox
                      checked={statusPageForm.monitorIds.includes(monitor.id)}
                      onCheckedChange={(checked) => toggleStatusPageMonitor(monitor.id, checked)}
                      aria-label={`绑定 ${monitor.name}`}
                    />
                    <span className="min-w-0 flex-1 truncate text-sm text-kumo-strong">{monitor.name}</span>
                    <span className="hidden max-w-[12rem] truncate font-mono text-[10px] text-kumo-subtle cq-sm:block">{getDisplayUrl(monitor)}</span>
                  </label>
                ))}
              </div>
            )}
          </div>
        </div>

        <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-xs text-kumo-subtle">
          <div className="font-semibold text-kumo-strong">预览地址</div>
          <div className="mt-2 space-y-1 font-mono">
            <div className="truncate">{getStatusPagePublicUrl(statusPageForm, 'status')}</div>
            <div className="truncate">{getStatusPagePublicUrl(statusPageForm, 'u')}</div>
            {getStatusPageDomainUrl(statusPageForm) && <div className="truncate">{getStatusPageDomainUrl(statusPageForm)}</div>}
          </div>
        </div>

        <div className="flex flex-wrap justify-end gap-2">
          <Button size="sm" variant="secondary" onClick={resetStatusPageForm}>重置</Button>
          <Button size="sm" variant="primary" loading={uptimeMetaLoading} onClick={saveStatusPage} icon={<Save className="h-3.5 w-3.5" />}>
            {statusPageForm.id ? '保存状态页' : '创建状态页'}
          </Button>
        </div>
      </SectionCard>

      <SectionCard
        title="已发布状态页"
        icon={<Globe className="h-4 w-4 text-brand" />}
        className="self-start"
        actions={(
          <>
            <Button size="sm" variant="secondary" icon={<RotateCw className="h-3.5 w-3.5" />} onClick={loadUptimeStatusPages} disabled={uptimeMetaLoading}>刷新</Button>
            <Button size="sm" variant="secondary" icon={<Plus className="h-3.5 w-3.5" />} onClick={createDefaultStatusPage} disabled={uptimeMetaLoading}>默认页</Button>
          </>
        )}
        bodyPadding="lg"
        bodyClassName="space-y-4"
      >

        {uptimeMetaLoading && uptimeStatusPages.length === 0 ? (
          <div className="space-y-3">
            {Array.from({ length: 3 }).map((_, index) => <SkeletonLine key={index} className="h-16 w-full" />)}
          </div>
        ) : uptimeStatusPages.length === 0 ? (
          <div className="flex min-h-56 flex-col items-center justify-center rounded-lg border border-dashed border-kumo-line text-center text-sm text-kumo-subtle">
            <Globe className="mb-3 h-8 w-8 opacity-40" />
            暂无状态页，创建一个公开单页后即可分享。
          </div>
        ) : (
          <div className="grid gap-3">
            {uptimeStatusPages.map((page) => {
              const statusUrl = getStatusPagePublicUrl(page, 'status');
              const compactUrl = getStatusPagePublicUrl(page, 'u');
              const domainUrl = getStatusPageDomainUrl(page);
              return (
                <div key={page.id} className="rounded-lg border border-kumo-line bg-kumo-base p-3">
                  <div className="flex flex-col gap-3 cq-sm:flex-row cq-sm:items-start cq-sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-brand/10 text-brand">
                          <PublicPageBrandIcon pageKind="uptime" config={page.config} iconClassName="h-4 w-4" customIconClassName="h-4 w-4" />
                        </span>
                        <span className="truncate text-sm font-semibold text-kumo-strong">{page.title || page.slug}</span>
                        <span className={`rounded px-2 py-0.5 text-[10px] font-semibold ${page.public ? 'bg-kumo-success/10 text-kumo-success' : 'bg-kumo-line/30 text-kumo-subtle'}`}>
                          {page.public ? '公开' : '私有'}
                        </span>
                        <span className="rounded bg-kumo-recessed px-2 py-0.5 font-mono text-[10px] text-kumo-subtle">{page.cacheSeconds || 300}s</span>
                      </div>
                      <div className="mt-1 truncate font-mono text-xs text-kumo-subtle">{page.slug}</div>
                      {page.description && <div className="mt-2 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">{page.description}</div>}
                    </div>
                    <div className="flex shrink-0 flex-wrap gap-2">
                      <Button size="sm" variant="secondary" shape="square" icon={<Edit className="h-3.5 w-3.5" />} onClick={() => editStatusPage(page)} aria-label="编辑状态页" />
                      <Button size="sm" variant="secondary" shape="square" icon={<ExternalLink className="h-3.5 w-3.5" />} onClick={() => window.open(statusUrl, '_blank', 'noopener,noreferrer')} aria-label="打开状态页" />
                      <Button size="sm" variant="secondary" shape="square" icon={<Copy className="h-3.5 w-3.5" />} onClick={() => copyStatusUrl(statusUrl)} aria-label="复制状态页地址" />
                      <Button size="sm" variant={isArmed(`status-page:${page.id}`) ? 'destructive' : 'secondary-destructive'} shape="square" icon={<Trash className="h-3.5 w-3.5" />} onClick={() => deleteStatusPage(page)} aria-label="删除状态页" />
                    </div>
                  </div>
                  <div className="mt-3 grid gap-2 text-xs">
                    <ClipboardText size="sm" text={statusUrl} className="min-w-0 w-full" tooltip={{ text: '复制状态页地址', copiedText: '地址已复制' }} />
                    <ClipboardText size="sm" text={compactUrl} className="min-w-0 w-full" tooltip={{ text: '复制 /u 地址', copiedText: '地址已复制' }} />
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

export default StatusPagesPanel;
