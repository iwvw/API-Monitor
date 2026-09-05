import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { toast } from '../modules/toast.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge, ClipboardText, Empty, Loader, Tabs, Text } from '@cloudflare/kumo';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { ResponsiveSearchInput, SectionCard, TabBarOverflowActions, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import {
  Box,
  Plus,
  Trash,
  Search,
  ExternalLink,
  RefreshCw,
  Users,
  Star,
} from '../components/Icons.jsx';

const DEFAULT_PAGE_SIZE = '100';

function DockerHubPage() {
  const getAuthHeaders = useCallback(() => ({
    'Content-Type': 'application/json',
  }), []);
  const { confirmPress } = useConfirmPress();

  const [activeTab, setActiveTab] = useState('repos');

  // --- 账号与仓库 ---
  const [accounts, setAccounts] = useState([]);
  const [loadingAccounts, setLoadingAccounts] = useState(false);
  const [accountDialogOpen, setAccountDialogOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [verifyId, setVerifyId] = useState(null);
  const [accountForm, setAccountForm] = useState({ username: '', token: '' });
  const [reposByAccount, setReposByAccount] = useState({});
  const [loadingReposId, setLoadingReposId] = useState(null);
  const [selectedAccountId, setSelectedAccountId] = useState(null);
  const [tagsRepo, setTagsRepo] = useState(null);
  const [tags, setTags] = useState([]);
  const [tagsLoading, setTagsLoading] = useState(false);

  // --- 镜像搜索 ---
  const [query, setQuery] = useState('');
  const [searching, setSearching] = useState(false);
  const [searchResults, setSearchResults] = useState([]);
  const [searchCount, setSearchCount] = useState(0);

  const api = useCallback(async (path, options = {}) => {
    const response = await fetch(path, {
      ...options,
      headers: {
        ...getAuthHeaders(),
        ...(options.headers || {}),
      },
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.success === false) {
      throw new Error(result.error || `请求失败: ${response.status}`);
    }
    return result.data !== undefined ? result.data : result;
  }, [getAuthHeaders]);

  const loadAccountRepositories = useCallback(async (accountId) => {
    setLoadingReposId(String(accountId));
    try {
      const result = await api(`/api/dockerhub/accounts/${accountId}/repositories?page_size=${DEFAULT_PAGE_SIZE}`);
      const list = Array.isArray(result) ? result : [];
      setReposByAccount((prev) => ({ ...prev, [String(accountId)]: list }));
      return list;
    } catch (error) {
      toast.error(error.message || '加载仓库失败');
      setReposByAccount((prev) => ({ ...prev, [String(accountId)]: [] }));
      return [];
    } finally {
      setLoadingReposId(null);
    }
  }, [api]);

  // 进入页面默认加载账号，选中第一个账号并加载其仓库
  const loadAccounts = useCallback(async () => {
    setLoadingAccounts(true);
    try {
      const data = await api('/api/dockerhub/accounts');
      const list = Array.isArray(data) ? data : [];
      setAccounts(list);
      setSelectedAccountId((current) => {
        if (current && list.some((account) => String(account.id) === String(current))) return current;
        return list.length > 0 ? String(list[0].id) : null;
      });
      return list;
    } catch (error) {
      toast.error(error.message || '加载账号失败');
      return [];
    } finally {
      setLoadingAccounts(false);
    }
  }, [api]);

  useEffect(() => {
    void loadAccounts();
  }, [loadAccounts]);

  // 选中账号变化时加载其仓库（未缓存才拉取）
  useEffect(() => {
    if (!selectedAccountId) return;
    if (reposByAccount[String(selectedAccountId)] !== undefined) return;
    void loadAccountRepositories(selectedAccountId);
  }, [selectedAccountId, loadAccountRepositories, reposByAccount]);

  // 添加账号后自动选中并加载其仓库
  const createAccount = async () => {
    const username = accountForm.username.trim();
    const token = accountForm.token.trim();
    if (!username || !token) {
      toast.warning('请填写用户名和访问令牌');
      return;
    }
    setSaving(true);
    try {
      await api('/api/dockerhub/accounts', {
        method: 'POST',
        body: JSON.stringify({ username, token }),
      });
      toast.success('Docker Hub 账号已保存');
      setAccountDialogOpen(false);
      setAccountForm({ username: '', token: '' });
      const fresh = await api('/api/dockerhub/accounts');
      const list = Array.isArray(fresh) ? fresh : [];
      setAccounts(list);
      const added = list.find((account) => String(account.username) === username);
      if (added) {
        setSelectedAccountId(String(added.id));
      }
    } catch (error) {
      toast.error(error.message || '保存账号失败');
    } finally {
      setSaving(false);
    }
  };

  const verifyAccount = async (id) => {
    setVerifyId(String(id));
    try {
      const result = await api(`/api/dockerhub/accounts/${id}/verify`, { method: 'POST', body: '{}' });
      if (result.valid) {
        toast.success('令牌有效');
      } else {
        toast.error(result.error || '令牌无效');
      }
    } catch (error) {
      toast.error(error.message || '验证失败');
    } finally {
      setVerifyId(null);
    }
  };

  const deleteAccount = async (id, username) => {
    const ok = await confirmPress(`dockerhub-account:${id}`, `删除 Docker Hub 账号「${username}」`);
    if (!ok) return;
    try {
      await api(`/api/dockerhub/accounts/${id}`, { method: 'DELETE' });
      toast.success('账号已删除');
      setReposByAccount((prev) => {
        const next = { ...prev };
        delete next[String(id)];
        return next;
      });
      await loadAccounts();
    } catch (error) {
      toast.error(error.message || '删除失败');
    }
  };

  const openTags = async (namespace, name, accountId) => {
    setTagsRepo({ namespace, name });
    setTags([]);
    setTagsLoading(true);
    try {
      const accountQuery = accountId ? `&accountId=${encodeURIComponent(accountId)}` : '';
      const tagList = await api(`/api/dockerhub/repositories/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/tags?page_size=50${accountQuery}`);
      setTags(Array.isArray(tagList) ? tagList : []);
    } catch (error) {
      toast.error(error.message || '加载标签失败');
    } finally {
      setTagsLoading(false);
    }
  };

  const runSearch = async () => {
    const keyword = query.trim();
    if (!keyword) {
      toast.warning('请输入搜索关键词');
      return;
    }
    setSearching(true);
    try {
      const res = await fetch(`/api/dockerhub/search?query=${encodeURIComponent(keyword)}&page_size=25`, {
        headers: getAuthHeaders(),
      });
      const result = await res.json().catch(() => ({}));
      if (!res.ok || result.success === false) {
        throw new Error(result.error || `搜索失败: ${res.status}`);
      }
      setSearchResults(Array.isArray(result.data) ? result.data : []);
      setSearchCount(result.count || 0);
    } catch (error) {
      toast.error(error.message || '搜索失败');
      setSearchResults([]);
      setSearchCount(0);
    } finally {
      setSearching(false);
    }
  };

  const selectedAccount = useMemo(
    () => accounts.find((account) => String(account.id) === String(selectedAccountId)) || accounts[0] || null,
    [accounts, selectedAccountId],
  );

  const selectedRepos = useMemo(
    () => (selectedAccount ? reposByAccount[String(selectedAccount.id)] || [] : []),
    [selectedAccount, reposByAccount],
  );

  const selectAccount = (value) => {
    setSelectedAccountId(value);
    setTagsRepo(null);
    setTags([]);
    if (value && reposByAccount[String(value)] === undefined) {
      void loadAccountRepositories(value);
    }
  };

  const accountOptions = accounts.map((account) => ({ value: String(account.id), label: account.username }));
  const repositoryCount = selectedAccount ? selectedRepos.length : 0;

  const renderReposTable = (rows, { accountId, linkBase } = {}) => (
    <div className="overflow-x-auto">
      <Table layout="fixed" className="min-w-[76rem]">
        <colgroup>
          <col className="w-[16rem]" />
          <col />
          <col className="w-[6rem]" />
          <col className="w-[7.5rem]" />
          <col className="w-[7.5rem]" />
          <col className="w-[11rem]" />
          <col className="w-[5.5rem]" />
          <col className="w-[7.5rem]" />
        </colgroup>
        <Table.Header variant="compact">
          <Table.Row>
            <Table.Head>仓库名</Table.Head>
            <Table.Head>描述</Table.Head>
            <Table.Head className="text-right">星标</Table.Head>
            <Table.Head className="text-right">拉取</Table.Head>
            <Table.Head className="text-right">大小</Table.Head>
            <Table.Head>更新时间</Table.Head>
            <Table.Head>类型</Table.Head>
            <Table.Head className="app-table-action">操作</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {rows.length === 0 ? (
            <Table.Row>
              <Table.Cell colSpan={8} className="py-8 text-center text-kumo-subtle">暂无仓库</Table.Cell>
            </Table.Row>
          ) : (
            rows.map((repo) => {
              // 「我的仓库」列表返回 namespace/name；「镜像搜索」返回 repo_name。
              // 优先用显式 namespace/name，缺失时才从 repo_name 解析，避免误判为 library。
              const namespace = repo.namespace || repo.repo_owner
                || (() => { const n = (repo.repo_name || '').split('/'); return n.length === 2 ? n[0] : 'library'; })();
              const repoName = repo.name
                || (() => { const n = (repo.repo_name || '').split('/'); return n[n.length - 1] || n[0] || ''; })();
              const description = repo.description || repo.short_description || '-';
              const updatedAt = repo.last_updated || repo.last_modified || '';
              return (
                <Table.Row key={namespace + '/' + repoName} className="hover:bg-kumo-recessed/25">
                  <Table.Cell>
                    <span className="flex min-w-0 items-center gap-2">
                      <Box className="h-4 w-4 shrink-0 text-kumo-info" />
                      <span className="truncate font-medium text-kumo-strong" title={`${namespace}/${repoName}`}>{namespace}/{repoName}</span>
                    </span>
                  </Table.Cell>
                  <Table.Cell className="truncate text-kumo-subtle" title={description}>{description}</Table.Cell>
                  <Table.Cell className="whitespace-nowrap text-right font-mono text-xs text-kumo-subtle">{formatNumber(repo.star_count)}</Table.Cell>
                  <Table.Cell className="whitespace-nowrap text-right font-mono text-xs text-kumo-subtle">{formatNumber(repo.pull_count)}</Table.Cell>
                  <Table.Cell className="whitespace-nowrap text-right font-mono text-xs text-kumo-subtle">{formatBytes(repo.storage_size)}</Table.Cell>
                  <Table.Cell className="truncate whitespace-nowrap text-kumo-subtle" title={updatedAt}>
                    <span className="truncate">{formatHubTime(updatedAt)}</span>
                  </Table.Cell>
                  <Table.Cell>
                    <span className="whitespace-nowrap">
                      {repo.is_private ? (
                        <Badge variant="warning">私有</Badge>
                      ) : repo.is_official ? (
                        <Badge variant="primary">官方</Badge>
                      ) : (
                        <Badge variant="neutral">公开</Badge>
                      )}
                    </span>
                  </Table.Cell>
                  <Table.Cell className="text-right">
                    <div className="flex justify-end gap-1 whitespace-nowrap">
                      <Button size="sm" shape="square" variant="secondary" icon={<Search className="h-3.5 w-3.5" />} aria-label="查看标签" title="查看标签" onClick={() => openTags(namespace, repoName, accountId)} />
                      <Button size="sm" shape="square" variant="secondary" icon={<ExternalLink className="h-3.5 w-3.5" />} aria-label="在 Docker Hub 打开" title="在 Docker Hub 打开" onClick={() => window.open(linkBase ? `${linkBase}${namespace}/${repoName}` : `https://hub.docker.com/r/${namespace}/${repoName}`, '_blank', 'noopener,noreferrer')} />
                    </div>
                  </Table.Cell>
                </Table.Row>
              );
            })
          )}
        </Table.Body>
      </Table>
    </div>
  );

  const tabsHeader = (
    <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
      <div className="min-w-0 w-full cq-md:w-auto">
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={[
            { value: 'repos', label: <span className="inline-flex items-center gap-1.5"><Box className="w-4 h-4 text-kumo-info" />我的仓库</span> },
            { value: 'search', label: <span className="inline-flex items-center gap-1.5"><Search className="w-4 h-4 text-kumo-secondary" />镜像搜索</span> },
          ]}
        />
      </div>
      <TabBarOverflowActions
        items={[
          {
            key: 'add-account',
            label: '添加账号',
            icon: <Plus className="w-3.5 h-3.5" />,
            onClick: () => setAccountDialogOpen(true),
          },
          ...(activeTab === 'repos' && selectedAccount
            ? [{
                key: 'refresh-repos',
                label: '刷新仓库',
                icon: <RefreshCw className="w-3.5 h-3.5" />,
                onClick: () => loadAccountRepositories(String(selectedAccount.id)),
                loading: loadingReposId === String(selectedAccount.id),
              }]
            : []),
          ...(activeTab === 'search'
            ? [{ key: 'search-refresh', label: '搜索', icon: <Search className="w-3.5 h-3.5" />, onClick: () => runSearch(), loading: searching, disabled: !query.trim() }]
            : []),
        ]}
      />
    </div>
  );

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      {tabsHeader}

      <div className="min-w-0">
        {activeTab === 'repos' && (
          loadingAccounts && accounts.length === 0 ? (
            <Empty size="base" icon={<Loader size={32} className="text-kumo-info" />} title="正在加载账号" description="读取已保存的 Docker Hub 凭据" />
          ) : accounts.length === 0 ? (
            <Empty
              size="base"
              icon={<Users className="h-8 w-8 text-kumo-secondary" />}
              title="还没有 Docker Hub 账号"
              description="先添加你的账号，即可查看该账号下全部镜像仓库"
            >
              <Button size="sm" variant="primary" icon={<Plus className="h-3.5 w-3.5" />} onClick={() => setAccountDialogOpen(true)}>添加账号</Button>
            </Empty>
          ) : (
            <SectionCard
              icon={<Box className="h-4 w-4 text-kumo-info" />}
              title="我的仓库"
              meta={selectedAccount ? <Badge variant="neutral">{selectedAccount.username} · {repositoryCount} 个仓库</Badge> : null}
              actions={(
                <>
                  <Select
                    alignItemWithTrigger
                    size="sm"
                    aria-label="选择 Docker Hub 账号"
                    className="w-44"
                    value={selectedAccount ? String(selectedAccount.id) : ''}
                    onValueChange={selectAccount}
                    items={accountOptions}
                  />
                  <Button
                    size="sm"
                    variant="secondary"
                    icon={<Search className="h-3.5 w-3.5" />}
                    loading={verifyId === String(selectedAccount?.id)}
                    onClick={() => selectedAccount && verifyAccount(selectedAccount.id)}
                  >验证令牌</Button>
                  <Button
                    size="sm"
                    variant="secondary-destructive"
                    icon={<Trash className="h-3.5 w-3.5" />}
                    onClick={() => selectedAccount && deleteAccount(selectedAccount.id, selectedAccount.username)}
                  >删除账号</Button>
                </>
              )}
              bodyPadding="none"
            >
              {loadingReposId === String(selectedAccount?.id) && selectedRepos.length === 0 ? (
                <Empty size="base" className="rounded-none border-0 bg-transparent" icon={<Loader size={32} className="text-kumo-info" />} title={`正在加载 ${selectedAccount.username} 的仓库`} description="同步镜像仓库列表" />
              ) : selectedRepos.length === 0 ? (
                <Empty size="base" className="rounded-none border-0 bg-transparent" icon={<Box className="h-8 w-8 text-kumo-secondary" />} title="该账号暂无仓库" description="账号下没有镜像仓库" />
              ) : (
                renderReposTable(selectedRepos, { accountId: selectedAccount.id, linkBase: 'https://hub.docker.com/r/' })
              )}
            </SectionCard>
          )
        )}

        {activeTab === 'search' && (
          <div className="space-y-3">
            <SectionCard
              icon={<Search className="h-4 w-4 text-kumo-info" />}
              title="搜索 Docker Hub 镜像"
              bodyPadding="md"
            >
              <div className="flex flex-wrap items-center gap-2">
                <ResponsiveSearchInput
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  onSearch={() => void runSearch()}
                  placeholder="搜索 Docker Hub 镜像，如 nginx、redis"
                  ariaLabel="搜索 Docker Hub 镜像"
                  className="min-w-0 flex-1"
                />
                <Button
                  size="sm"
                  variant="primary"
                  icon={<Search className="h-3.5 w-3.5" />}
                  loading={searching}
                  onClick={() => void runSearch()}
                  className="shrink-0"
                >搜索</Button>
              </div>
            </SectionCard>

            <SectionCard
              icon={<Search className="h-4 w-4 text-kumo-info" />}
              title="搜索结果"
              meta={searchCount > 0 ? <Badge variant="neutral">{searchCount} 个官方镜像</Badge> : null}
              bodyPadding="none"
            >
              {searching && searchResults.length === 0 ? (
                <Empty size="base" className="rounded-none border-0 bg-transparent" icon={<Loader size={32} className="text-kumo-info" />} title="正在搜索" description="检索 Docker Hub 镜像仓库" />
              ) : searchResults.length === 0 ? (
                <Empty size="base" className="rounded-none border-0 bg-transparent" icon={<Box className="h-8 w-8 text-kumo-secondary" />} title="暂无搜索结果" description={query.trim() ? '换个关键词试试' : '输入关键词开始搜索'} />
              ) : (
                renderReposTable(searchResults, { accountId: undefined })
              )}
            </SectionCard>
          </div>
        )}
      </div>

      {/* 标签查看 Dialog */}
      <Dialog.Root open={Boolean(tagsRepo)} onOpenChange={(open) => { if (!open) setTagsRepo(null); }}>
        <Dialog className="@container flex max-h-[min(calc(100dvh-2rem),18rem)] w-[min(calc(100vw-2rem),80rem)] flex-col overflow-hidden p-0">
          <div className="border-b border-kumo-line bg-kumo-recessed/20 px-5 py-3">
            <Dialog.Title className="text-base font-semibold text-kumo-strong">
              标签 {tagsRepo?.namespace}/{tagsRepo?.name}
            </Dialog.Title>
            <Dialog.Description className="mt-1 text-xs text-kumo-subtle">
              按更新时间排序，共 {tags.length} 个标签。
            </Dialog.Description>
          </div>
          <div className="min-h-0 max-h-[calc(18rem-5.5rem)] flex-1 overflow-y-auto px-5 py-4">
            {tagsLoading ? (
              <div className="flex items-center justify-center py-6"><Loader size={24} className="text-kumo-info" /></div>
            ) : tags.length === 0 ? (
              <Empty size="sm" title="暂无标签" description="该仓库没有标签" />
            ) : (
              <div className="space-y-0.5">
                {sortTagsByUpdated(tags).map((tag) => (
                  <div
                    key={tag.name}
                    className="grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-4 rounded-md px-2 py-1 hover:bg-kumo-recessed/25"
                  >
                    <span className="flex min-w-0 items-center gap-2">
                      <Box className="h-3.5 w-3.5 shrink-0 text-kumo-info" />
                      <code className="truncate text-xs font-medium text-kumo-strong" title={tag.name}>{tag.name}</code>
                      {tag.digest && <span className="hidden min-w-0 truncate font-mono text-[10px] text-kumo-subtle cq-sm:inline">{tag.digest.slice(0, 12)}</span>}
                    </span>
                    <span className="text-right font-mono text-[11px] text-kumo-subtle">{formatBytes(tag.full_size)}</span>
                    <span className="whitespace-nowrap text-[11px] text-kumo-subtle" title={tag.last_updated || tag.tag_last_pushed || ''}>{formatHubTime(tag.last_updated || tag.tag_last_pushed)}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-5 py-3">
            <Button size="sm" variant="secondary" onClick={() => setTagsRepo(null)}>关闭</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 添加账号 Dialog */}
      <Dialog.Root open={accountDialogOpen} onOpenChange={setAccountDialogOpen}>
        <Dialog className="@container flex max-h-[min(calc(100dvh-2rem),34rem)] w-[min(calc(100vw-2rem),38rem)] flex-col overflow-hidden p-0">
          <div className="border-b border-kumo-line bg-kumo-recessed/20 px-5 py-4">
            <Dialog.Title className="text-base font-semibold text-kumo-strong">添加 Docker Hub 账号</Dialog.Title>
            <Dialog.Description className="mt-1 text-xs text-kumo-subtle">
              访问令牌用于验证凭据并列出该账号下全部仓库（含私有）。
            </Dialog.Description>
          </div>
          <form
            className="min-h-0 flex-1 overflow-y-auto"
            onSubmit={(e) => { e.preventDefault(); void createAccount(); }}
          >
            <div className="space-y-4 px-5 py-4">
              <Input
                size="sm"
                label="Docker Hub 用户名"
                value={accountForm.username}
                onChange={(e) => setAccountForm((p) => ({ ...p, username: e.target.value }))}
                placeholder="如 myusername"
                autoFocus
              />
              <Input
                size="sm"
                label="访问令牌（PAT / 临时令牌）"
                type="password"
                value={accountForm.token}
                onChange={(e) => setAccountForm((p) => ({ ...p, token: e.target.value }))}
                placeholder="dckr_pat_..."
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                spellCheck={false}
              />
              <Text variant="secondary" size="xs">
                提示：令牌不会明文展示，仅加密存储在服务端。
              </Text>
            </div>
            <div className="flex justify-end gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-5 py-3">
              <Dialog.Close render={(props) => <Button type="button" size="sm" variant="secondary" {...props}>取消</Button>} />
              <Button type="submit" size="sm" variant="primary" icon={<Plus className="h-3.5 w-3.5" />} loading={saving}>保存</Button>
            </div>
          </form>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

function formatNumber(value) {
  const number = Number(value || 0);
  if (!Number.isFinite(number)) return '0';
  if (number >= 1e9) return `${(number / 1e9).toFixed(1)}B`;
  if (number >= 1e6) return `${(number / 1e6).toFixed(1)}M`;
  if (number >= 1e3) return `${(number / 1e3).toFixed(1)}K`;
  return String(number);
}

function formatBytes(value) {
  const bytes = Number(value || 0);
  if (!Number.isFinite(bytes) || bytes <= 0) return '-';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let index = 0;
  let size = bytes;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size.toFixed(size >= 100 ? 0 : 1)} ${units[index]}`;
}

function hubTimeValue(value) {
  if (!value) return null;
  const text = String(value);
  // Docker Hub 命名标签返回 MM/DD/YYYY HH:mm:ss
  const match = text.match(/^(\d{2})\/(\d{2})\/(\d{4})\s+(\d{2}):(\d{2}):(\d{2})/);
  if (match) {
    const [, month, day, year, hour, minute, second] = match;
    const date = new Date(Number(year), Number(month) - 1, Number(day), Number(hour), Number(minute), Number(second));
    return Number.isNaN(date.getTime()) ? null : date.getTime();
  }
  // 其余格式（含 ISO 8601，如 2026-09-01T03:18:25.748824Z）用标准解析
  const ts = Date.parse(text);
  return Number.isNaN(ts) ? null : ts;
}

function formatHubTime(value) {
  if (!value) return '-';
  const ts = hubTimeValue(value);
  if (ts === null) return String(value);
  return new Date(ts).toLocaleString();
}

function sortTagsByUpdated(tags) {
  return [...tags].sort((a, b) => {
    const ta = hubTimeValue(a.last_updated || a.tag_last_pushed);
    const tb = hubTimeValue(b.last_updated || b.tag_last_pushed);
    if (ta === null && tb === null) return String(a.name).localeCompare(String(b.name));
    if (ta === null) return 1;
    if (tb === null) return -1;
    return tb - ta;
  });
}

export default DockerHubPage;