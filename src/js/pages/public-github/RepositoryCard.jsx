import { useEffect, useMemo, useRef, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Loader } from '@cloudflare/kumo/components/loader';
import { formatGitHubRepositoryDescription } from '../../modules/githubEmoji.js';
import { ExternalLink, GitBranch, GitHubBrand } from '../../components/Icons.jsx';
import { PUBLIC_ACTION_PANEL_INSET_Y, statusLabel, statusTone } from './constants.js';
import { formatActionDuration, formatDateTime, formatNumber } from './utils.js';
import ActionWorkflowCanvas, { ActionWorkflowLoadingState } from './ActionWorkflowCanvas.jsx';

function RepositoryStat({ label, value }) {
  return (
    <div className="min-w-0 rounded-md border border-kumo-interact/70 bg-kumo-recessed/35 px-2.5 py-1.5 text-center">
      <div className="truncate text-[10px] font-medium uppercase text-kumo-subtle">{label}</div>
      <div className="mt-0.5 truncate text-xs font-semibold text-kumo-strong">{value}</div>
    </div>
  );
}

function RepositoryCard({ item, now, config, detailLoading = false, onSelectRun }) {
  const projectPanelRef = useRef(null);
  const actionContentRef = useRef(null);
  const [panelHeights, setPanelHeights] = useState({ project: 0, action: 0 });

  // 1. 获取同一次提交 (Latest Commit SHA) 的所有 Workflow Runs 列表
  const recentRuns = Array.isArray(item?.recent_runs) && item.recent_runs.length > 0
    ? item.recent_runs
    : (item?.latest_run ? [item.latest_run] : []);

  const latestCommitSha = item?.latest_run?.commit_sha || recentRuns[0]?.commit_sha;
  const sameCommitRuns = useMemo(() => {
    if (!latestCommitSha) return recentRuns;
    const matching = recentRuns.filter(r => r.commit_sha === latestCommitSha);
    return matching.length > 0 ? matching : recentRuns;
  }, [recentRuns, latestCommitSha]);

  const [activeRunId, setActiveRunId] = useState(() => item?.latest_run?.run_id || sameCommitRuns[0]?.run_id || null);
  const [pendingRunId, setPendingRunId] = useState(null);

  useEffect(() => {
    if (sameCommitRuns.length > 0 && !sameCommitRuns.some(r => String(r.run_id) === String(activeRunId))) {
      const fallbackRunId = sameCommitRuns[0]?.run_id;
      setActiveRunId(fallbackRunId);
      setPendingRunId(fallbackRunId);
      onSelectRun?.(fallbackRunId);
    }
  }, [sameCommitRuns, activeRunId, onSelectRun]);

  useEffect(() => {
    if (!detailLoading) {
      setPendingRunId(null);
    }
  }, [detailLoading]);

  const activeRun = useMemo(() => {
    return sameCommitRuns.find(r => String(r.run_id) === String(activeRunId)) || sameCommitRuns[0] || item?.latest_run || null;
  }, [sameCommitRuns, activeRunId, item?.latest_run]);

  const activeRunIndex = Math.max(0, sameCommitRuns.findIndex(r => String(r.run_id) === String(activeRunId)));

  const workflowName = activeRun?.workflow_name || activeRun?.display_title || '最新 Workflow';
  const actionStatus = activeRun?.conclusion || activeRun?.status || item?.latest_action_conclusion || item?.latest_action_status || 'unknown';
  const actionTone = statusTone(actionStatus);
  const jobs = Array.isArray(item?.jobs) ? item.jobs : [];
  const canLinkRepo = config?.showRepoLinks !== false && item?.html_url;
  const canLinkRun = activeRun?.html_url || item?.latest_run?.html_url;
  const hasStats = config?.showRepositoryStats !== false;
  const showDescriptions = config?.showDescriptions !== false;
  const runStartedAt = activeRun?.run_started_at || activeRun?.created_at || item?.latest_action_started_at || item?.latest_action_created_at;
  const runUpdatedAt = activeRun?.updated_at || item?.latest_action_updated_at;
  const runDuration = runStartedAt ? formatActionDuration(runStartedAt, runUpdatedAt, now) : '-';
  const hasDetailPayload = Array.isArray(item?.jobs) || Boolean(item?.workflow) || Boolean(item?.workflow_error);
  const showDetailLoading = detailLoading && !hasDetailPayload;
  const actionOverflows = panelHeights.project > 0
    && panelHeights.action + PUBLIC_ACTION_PANEL_INSET_Y > panelHeights.project + 1;
  const actionExpandedHeight = actionOverflows
    ? panelHeights.action + PUBLIC_ACTION_PANEL_INSET_Y
    : panelHeights.project;
  const isSwitchingRun = detailLoading && Boolean(pendingRunId);
  const pendingRunName = sameCommitRuns.find((run) => String(run.run_id) === String(pendingRunId))?.workflow_name
    || sameCommitRuns.find((run) => String(run.run_id) === String(pendingRunId))?.display_title
    || '工作流';

  useEffect(() => {
    const projectPanel = projectPanelRef.current;
    const actionContent = actionContentRef.current;
    if (!projectPanel || !actionContent) return undefined;

    const updateHeights = () => {
      const next = {
        project: Math.ceil(projectPanel.getBoundingClientRect().height),
        action: Math.ceil(actionContent.getBoundingClientRect().height),
      };
      setPanelHeights((current) => (
        current.project === next.project && current.action === next.action ? current : next
      ));
    };

    updateHeights();
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', updateHeights);
      return () => window.removeEventListener('resize', updateHeights);
    }
    const observer = new ResizeObserver(updateHeights);
    observer.observe(projectPanel);
    observer.observe(actionContent);
    return () => observer.disconnect();
  }, [showDetailLoading, jobs.length, item.workflow_error]);

  return (
    <article className="grid gap-3 lg:grid-cols-[minmax(360px,0.82fr)_minmax(0,1.68fr)] lg:items-start">
      <div ref={projectPanelRef} className="min-w-0 overflow-hidden rounded-lg border border-kumo-interact/75 bg-kumo-base">
        <div className="border-b border-kumo-interact/65 px-4 py-2.5">
          <div className="grid gap-1.5">
            <div className="flex min-w-0 items-center justify-between gap-2">
              <div className="flex min-w-0 items-start gap-2">
              <GitHubBrand className="mt-0.5 h-4 w-4 shrink-0 text-brand" />
              {canLinkRepo ? (
                <a
                  href={item.html_url}
                  target="_blank"
                  rel="noreferrer"
                  className="block min-w-0 flex-1 truncate pb-px text-sm font-semibold leading-5 text-kumo-strong decoration-current [text-underline-offset:3px] hover:text-brand hover:underline"
                >
                  {item.full_name}
                </a>
              ) : (
                <div className="min-w-0 flex-1 truncate pb-px text-sm font-semibold leading-5 text-kumo-strong">{item.full_name}</div>
              )}
              </div>

              <div className="flex shrink-0 items-center gap-1">
                <Badge variant={item.private ? 'warning' : 'success'}>
                  {item.private ? '私有' : '公开'}
                </Badge>
                <Badge
                  variant={
                    actionTone === 'success'
                      ? 'success'
                      : actionTone === 'error'
                        ? 'error'
                        : actionTone === 'warning'
                          ? 'warning'
                          : 'secondary'
                  }
                >
                  {statusLabel(actionStatus)}
                </Badge>
                <Badge variant="secondary" className="font-medium">
                  {runDuration}
                </Badge>
                {canLinkRun && (
                  <Button
                    size="sm"
                    variant="secondary"
                    shape="square"
                    icon={<ExternalLink className="h-3.5 w-3.5" />}
                    onClick={() => window.open(activeRun?.html_url || item?.latest_run?.html_url, '_blank', 'noopener,noreferrer')}
                    aria-label="打开运行详情"
                  />
                )}
              </div>
            </div>

            {showDescriptions && item.description && (
              <div className="truncate text-[12px] leading-5 text-kumo-subtle" title={formatGitHubRepositoryDescription(item.description)}>
                {formatGitHubRepositoryDescription(item.description)}
              </div>
            )}
            <div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 text-[11px] text-kumo-subtle">
              <span className="inline-flex items-center gap-1">
                <GitBranch className="h-3.5 w-3.5" />
                {activeRun?.branch || item.default_branch || '默认分支'}
              </span>
              {activeRun?.actor && <span>{activeRun.actor}</span>}
              {activeRun?.commit_sha && <span className="font-mono">{String(activeRun.commit_sha).slice(0, 8)}</span>}
            </div>
          </div>
        </div>

        <div className={`grid min-w-0 gap-2 px-4 py-2.5 ${hasStats ? 'sm:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]' : ''}`}>
          {hasStats && (
            <div className="grid grid-cols-2 gap-1.5">
              <RepositoryStat label="星标" value={formatNumber(item.stars)} />
              <RepositoryStat label="复刻" value={formatNumber(item.forks)} />
              <RepositoryStat label="议题" value={formatNumber(item.open_issues)} />
              <RepositoryStat label="拉取请求" value={formatNumber(item.open_pull_requests)} />
            </div>
          )}

          <div className="grid content-start gap-1.5 rounded-lg border border-kumo-interact/70 bg-kumo-recessed/25 px-3 py-2 text-[11px]">
            <div className="flex items-center justify-between gap-2 border-b border-kumo-interact/40 pb-1">
              <span className="font-semibold text-kumo-strong text-[11px] flex items-center gap-1">
                <span>⚡ Actions</span>
                {sameCommitRuns.length > 1 && (
                  <span className="rounded bg-brand/10 text-brand px-1 py-0.2 text-[9px] font-mono font-semibold">
                    {activeRunIndex + 1}/{sameCommitRuns.length}
                  </span>
                )}
              </span>
              <span className="shrink-0 text-[10px] text-kumo-subtle font-mono">
                {runStartedAt ? formatDateTime(runStartedAt) : '暂无记录'}
              </span>
            </div>

            {sameCommitRuns.length > 1 ? (
              <div className="max-h-[64px] overflow-y-auto scrollbar-thin space-y-1 pr-0.5">
                {sameCommitRuns.map((run) => {
                  const isSelected = String(run.run_id) === String(activeRun?.run_id);
                  const isPending = String(run.run_id) === String(pendingRunId);
                  const tone = statusTone(run.conclusion || run.status);
                  return (
                    <button
                      key={run.run_id}
                      type="button"
                      onClick={() => {
                        if (String(run.run_id) === String(activeRun?.run_id) && !detailLoading) return;
                        setPendingRunId(run.run_id);
                        setActiveRunId(run.run_id);
                        onSelectRun?.(run.run_id);
                      }}
                      className={`w-full flex items-center justify-between gap-1.5 rounded border px-2 py-0.5 text-left text-[10.5px] transition-[transform] duration-200 ${
                        isPending
                          ? 'border-brand/45 bg-brand/12 text-brand ring-1 ring-brand/20'
                          : ''
                      } ${
                        isSelected
                          ? 'bg-brand/15 text-brand font-semibold border-brand/30'
                          : 'bg-kumo-base/60 text-kumo-subtle hover:bg-kumo-base hover:text-kumo-strong border-transparent'
                      }`}
                      disabled={isPending}
                    >
                      <div className="flex items-center gap-1.5 min-w-0">
                        <span className={`h-1.5 w-1.5 rounded-full shrink-0 ${
                          tone === 'success' ? 'bg-kumo-success' : tone === 'error' ? 'bg-kumo-danger' : 'bg-kumo-warning'
                        }`} />
                        <span className="truncate">{run.workflow_name || run.display_title}</span>
                      </div>
                      <span className="shrink-0 text-[9px] font-mono opacity-80">
                        {isPending ? (
                          <span className="inline-flex items-center gap-1 text-brand">
                            <Loader size={12} />
                            切换中
                          </span>
                        ) : (
                          statusLabel(run.conclusion || run.status)
                        )}
                      </span>
                    </button>
                  );
                })}
              </div>
            ) : (
              <div className="min-w-0 space-y-1">
                <div className="min-w-0 truncate font-semibold text-kumo-strong" title={workflowName}>{workflowName}</div>
                <div className="min-w-0 truncate text-kumo-subtle" title={activeRun?.commit_message || activeRun?.display_title || ''}>
                  {activeRun?.commit_message || activeRun?.display_title || '这个仓库还没有可展示的 workflow 运行记录。'}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      <div
        className={`group relative min-w-0 overflow-visible rounded-lg border border-kumo-interact/45 bg-kumo-base/35 p-3 transition-[height] duration-300 ease-[cubic-bezier(0.22,1,0.36,1)] will-change-[height] hover:bg-kumo-base/50 lg:h-[var(--public-action-panel-height)] ${actionOverflows ? 'lg:overflow-hidden lg:hover:h-[var(--public-action-expanded-height)]' : ''}`}
        style={panelHeights.project > 0 ? {
          '--public-action-panel-height': `${panelHeights.project}px`,
          '--public-action-expanded-height': `${actionExpandedHeight}px`,
        } : undefined}
      >
        {isSwitchingRun && (
          <div className="pointer-events-none absolute inset-x-3 top-3 z-20 flex items-center justify-between rounded-md border border-brand/30 bg-kumo-base/88 px-3 py-2 text-[11px] text-brand backdrop-blur-sm">
            <span className="inline-flex min-w-0 items-center gap-1.5">
              <Loader size={14} />
              <span className="truncate">正在切换到 {pendingRunName}</span>
            </span>
            <span className="font-mono text-[10px] text-kumo-subtle">加载最新 Job</span>
          </div>
        )}
        <div className={`w-full min-w-0 lg:flex lg:h-full ${actionOverflows ? 'lg:items-start' : 'lg:items-center'}`}>
          <div ref={actionContentRef} className={`w-full min-w-0 transition-opacity duration-200 ${isSwitchingRun ? 'opacity-45' : 'opacity-100'}`}>
          {showDetailLoading ? (
            <ActionWorkflowLoadingState />
          ) : item.workflow_error && jobs.length === 0 ? (
            <div className="rounded-md border border-kumo-warning/25 bg-kumo-warning/8 px-3 py-2.5 text-sm text-kumo-subtle">
              {item.workflow_error}
            </div>
          ) : jobs.length === 0 ? (
            <div className="rounded-md border border-kumo-interact/70 bg-kumo-recessed/20 px-3 py-2.5 text-sm text-kumo-subtle">
              暂无 Job 进度数据
            </div>
          ) : (
            <ActionWorkflowCanvas workflow={item.workflow} jobs={jobs} now={now} />
          )}
          </div>
        </div>
        {actionOverflows && (
          <div className="pointer-events-none absolute inset-x-px bottom-px hidden h-12 rounded-b-lg bg-gradient-to-t from-kumo-base/95 via-kumo-base/55 to-transparent opacity-100 transition-opacity duration-300 ease-out group-hover:opacity-0 lg:block" aria-hidden="true" />
        )}
      </div>
    </article>
  );
}

export default RepositoryCard;
export { RepositoryStat };
