import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Input } from '@cloudflare/kumo/components/input';
import { Tabs } from '@cloudflare/kumo';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import {
  AppCard,
  PageStack,
  TabBarOverflowActions,
  stickyTabsBaseClass,
} from '../../components/ui/AppPrimitives.jsx';
import {
  Activity,
  Bot,
  Download,
  FileText,
  RefreshCw,
  Shield,
  Trash,
} from '../../components/Icons.jsx';
import { tabs } from './tabs.jsx';
import AIAccessConsole from './AIAccessConsole.jsx';
import AIAuditConsole from './AIAuditConsole.jsx';
import APIKeyConsole from './APIKeyConsole.jsx';
import RouteDetail from './RouteDetail.jsx';
import RouteTree from './RouteTree.jsx';
import { FilterSelect, StatCard } from './components.jsx';
import { AI_ACCESS_BASE, API_KEYS_BASE, apiDocsShellClass } from './constants.js';
import {
  apiRequest,
  createDefaultAPIKeyForm,
  fetchJsonEnvelope,
  getRouteKey,
  groupOrderIndex,
  normalizeDocsPayload,
  normalizeSummary,
  toLocalDateTimeInput,
} from './utils.js';

function ApiDocsPage() {
  const [docs, setDocs] = useState(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [activeView, setActiveView] = useState('routes');
  const [query, setQuery] = useState('');
  const [group, setGroup] = useState('all');
  const [auth, setAuth] = useState('all');
  const [status, setStatus] = useState('all');
  const [selectedKey, setSelectedKey] = useState('');
  const [aiAccess, setAiAccess] = useState(null);
  const [aiLoading, setAiLoading] = useState(false);
  const [aiError, setAiError] = useState('');
  const [keyVisible, setKeyVisible] = useState(false);
  const [auditRecords, setAuditRecords] = useState([]);
  const [auditTotal, setAuditTotal] = useState(0);
  const [auditPage, setAuditPage] = useState(1);
  const [auditPageSize, setAuditPageSize] = useState(20);
  const [auditDays, setAuditDays] = useState(7);
  const [auditLoading, setAuditLoading] = useState(false);
  const [auditError, setAuditError] = useState('');
  const [auditAction, setAuditAction] = useState('');
  const [auditSearch, setAuditSearch] = useState('');
  const [apiKeyOverview, setApiKeyOverview] = useState(null);
  const [apiKeysLoading, setApiKeysLoading] = useState(false);
  const [apiKeysError, setApiKeysError] = useState('');
  const [apiKeyForm, setApiKeyForm] = useState(createDefaultAPIKeyForm);
  const [apiKeyEditingId, setApiKeyEditingId] = useState('');
  const [apiKeySubmitting, setApiKeySubmitting] = useState(false);
  const [issuedAPIKey, setIssuedAPIKey] = useState('');

  const loadDocs = useCallback(async (silent = false) => {
    if (silent) {
      setRefreshing(true);
    } else {
      setLoading(true);
    }
    try {
      let nextDocs;
      try {
        nextDocs = await fetchJsonEnvelope('/api/system/api-docs');
      } catch (primaryError) {
        console.info('api docs route unavailable, falling back to migration status:', primaryError);
        const migration = await fetchJsonEnvelope('/api/migration/status');
        nextDocs = {
          version: migration.version,
          summary: {
            byOwner: migration.routeSummary,
          },
          routes: migration.routes,
        };
      }
      const normalizedDocs = normalizeDocsPayload(nextDocs);
      setDocs(normalizedDocs);
      setSelectedKey(current => {
        if (current && normalizedDocs.routes.some(route => getRouteKey(route) === current)) {
          return current;
        }
        return normalizedDocs.routes[0] ? getRouteKey(normalizedDocs.routes[0]) : '';
      });
    } catch (error) {
      console.error('load api docs failed:', error);
      toast.error(error.message || '接口文档加载失败');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    loadDocs();
  }, [loadDocs]);

  const loadAIAccess = useCallback(async (silent = false) => {
    if (!silent) setAiLoading(true);
    setAiError('');
    try {
      const payload = await fetchJsonEnvelope(AI_ACCESS_BASE);
      setAiAccess(payload);
    } catch (error) {
      console.error('load ai access failed:', error);
      setAiError(error.message || 'AI 接入数据加载失败');
    } finally {
      setAiLoading(false);
    }
  }, []);

  useEffect(() => {
    if (activeView === 'ai' && !aiAccess && !aiLoading && !aiError) {
      loadAIAccess();
    }
  }, [activeView, aiAccess, aiError, aiLoading, loadAIAccess]);

  const loadAIAudit = useCallback(async (silent = false) => {
    if (!silent) setAuditLoading(true);
    setAuditError('');
    try {
      const params = new URLSearchParams({ days: auditDays, page: auditPage, pageSize: auditPageSize });
      if (auditAction) params.set('action', auditAction);
      if (auditSearch) params.set('search', auditSearch);
      const payload = await fetchJsonEnvelope(
        `${AI_ACCESS_BASE}/audit?${params}`
      );
      setAuditRecords(payload.records || []);
      setAuditTotal(payload.total || 0);
    } catch (error) {
      console.error('load ai audit failed:', error);
      setAuditError(error.message || '调用审计加载失败');
    } finally {
      setAuditLoading(false);
    }
  }, [auditDays, auditPage, auditPageSize, auditAction, auditSearch]);

  const handleAuditActionChange = useCallback(value => {
    setAuditAction(value);
    setAuditPage(1);
  }, []);

  const handleAuditSearchChange = useCallback(value => {
    setAuditSearch(value);
    setAuditPage(1);
  }, []);

  const clearAuditFilters = useCallback(() => {
    setAuditAction('');
    setAuditSearch('');
    setAuditPage(1);
  }, []);

  useEffect(() => {
    if (activeView !== 'audit') return undefined;
    // 搜索输入防抖：避免每敲一个字符触发一次请求
    const timer = window.setTimeout(() => {
      loadAIAudit();
    }, 300);
    return () => window.clearTimeout(timer);
  }, [activeView, loadAIAudit]);

  const loadAPIKeys = useCallback(async (silent = false) => {
    if (!silent) setApiKeysLoading(true);
    setApiKeysError('');
    try {
      setApiKeyOverview(await fetchJsonEnvelope(API_KEYS_BASE));
    } catch (error) {
      console.error('load api keys failed:', error);
      setApiKeysError(error.message || '密钥数据加载失败');
    } finally {
      setApiKeysLoading(false);
    }
  }, []);

  useEffect(() => {
    if (activeView === 'keys' && !apiKeyOverview && !apiKeysLoading && !apiKeysError) {
      loadAPIKeys();
    }
  }, [activeView, apiKeyOverview, apiKeysError, apiKeysLoading, loadAPIKeys]);

  const routes = docs?.routes || [];
  const summary = normalizeSummary(docs?.summary);
  const aiRouteCount = routes.filter(
    route =>
      route.group === '模型网关' ||
      route.prefix === '/api/ai/manifest' ||
      route.prefix === '/api/ai/mcp' ||
      route.prefix.startsWith('/api/ai-access')
  ).length;

  const groupItems = useMemo(() => {
    const groups = [...new Set(routes.map(route => route.group).filter(Boolean))].sort((a, b) => {
      const order = groupOrderIndex(a) - groupOrderIndex(b);
      return order !== 0 ? order : a.localeCompare(b, 'zh-CN');
    });
    return [
      { value: 'all', label: '全部分组' },
      ...groups.map(item => ({ value: item, label: item })),
    ];
  }, [routes]);

  const filteredRoutes = useMemo(() => {
    const text = query.trim().toLowerCase();
    return routes.filter(route => {
      if (group !== 'all' && route.group !== group) return false;
      if (auth !== 'all' && route.auth !== auth) return false;
      if (status !== 'all' && route.status !== status) return false;
      if (!text) return true;
      return [
        route.prefix,
        route.module,
        route.group,
        route.description,
        route.detail,
        route.auth,
        route.responseMode,
        ...(route.notes || []),
      ].some(value =>
        String(value || '')
          .toLowerCase()
          .includes(text)
      );
    });
  }, [auth, group, query, routes, status]);

  const selectedRoute = useMemo(() => {
    const visibleSelected = filteredRoutes.find(route => getRouteKey(route) === selectedKey);
    if (visibleSelected) return visibleSelected;
    return filteredRoutes[0] || null;
  }, [filteredRoutes, selectedKey]);

  const exportOpenAPI = () => {
    if (!summary.openapiRoute) return;
    window.open(summary.openapiRoute, '_blank', 'noopener,noreferrer');
  };

  const copyText = async (text, message) => {
    try {
      await navigator.clipboard.writeText(String(text || ''));
      toast.success(message);
    } catch (error) {
      console.error('copy failed:', error);
      toast.error('复制失败');
    }
  };

  const refreshAIAccess = () => loadAIAccess(true);

  const rotateAIKey = async () => {
    try {
      const payload = await apiRequest(`${AI_ACCESS_BASE}/key/rotate`, { method: 'POST' });
      setAiAccess(payload);
      setKeyVisible(true);
      toast.success('Agent Key 已轮换');
    } catch (error) {
      toast.error(error.message || '轮换失败');
    }
  };

  const toggleAIWrite = async enabled => {
    try {
      const payload = await apiRequest(`${AI_ACCESS_BASE}/write`, {
        method: 'PUT',
        body: JSON.stringify({ writeEnabled: enabled }),
      });
      setAiAccess(payload);
      toast.success(enabled ? '已开启 AI 写入，写操作将受到审计' : '已关闭 AI 写入，Agent 仅可读');
    } catch (error) {
      toast.error(error.message || '切换失败');
    }
  };

  const setAIAccessPolicy = async policy => {
    try {
      const payload = await apiRequest(`${AI_ACCESS_BASE}/policy`, {
        method: 'PUT',
        body: JSON.stringify({ policy }),
      });
      setAiAccess(payload);
      const label = { minimal: '只读（minimal）', standard: '标准（standard）', full: '全部权限（full）' }[policy] || policy;
      toast.success(`AI 接入权限模式已切换为 ${label}`);
    } catch (error) {
      toast.error(error.message || '切换失败');
    }
  };

  const clearAIAudit = async () => {
    try {
      const confirmed = await dialog.confirm('确认清空全部调用审计记录？此操作不可恢复。');
      if (!confirmed) return;
      await apiRequest(`${AI_ACCESS_BASE}/audit/clear`, { method: 'POST' });
      setAuditRecords([]);
      setAuditTotal(0);
      setAuditPage(1);
      toast.success('审计记录已清空');
    } catch (error) {
      toast.error(error.message || '清空失败');
    }
  };

  const resetAPIKeyForm = () => {
    setApiKeyEditingId('');
    setApiKeyForm(createDefaultAPIKeyForm());
  };

  const saveAPIKey = async () => {
    if (!apiKeyForm.name.trim()) {
      toast.error('请输入密钥名称');
      return;
    }
    if (apiKeyForm.kind === 'api' && apiKeyForm.scopes.length === 0) {
      toast.error('通用 API Key 至少需要一个权限');
      return;
    }
    let expiresAt = '';
    if (apiKeyForm.expiresAt) {
      const expiry = new Date(apiKeyForm.expiresAt);
      if (Number.isNaN(expiry.getTime())) {
        toast.error('过期时间无效');
        return;
      }
      expiresAt = expiry.toISOString();
    }
    setApiKeySubmitting(true);
    try {
      const payload = await apiRequest(
        apiKeyEditingId ? `${API_KEYS_BASE}/${apiKeyEditingId}` : API_KEYS_BASE,
        {
          method: apiKeyEditingId ? 'PUT' : 'POST',
          body: JSON.stringify({ ...apiKeyForm, name: apiKeyForm.name.trim(), expiresAt }),
        }
      );
      if (payload.apiKey) setIssuedAPIKey(payload.apiKey);
      toast.success(apiKeyEditingId ? '密钥设置已更新' : 'API Key 已生成');
      resetAPIKeyForm();
      await loadAPIKeys(true);
    } catch (error) {
      toast.error(error.message || '密钥保存失败');
    } finally {
      setApiKeySubmitting(false);
    }
  };

  const editAPIKey = key => {
    setApiKeyEditingId(key.id);
    setApiKeyForm({
      name: key.name || '',
      kind: key.kind || 'api',
      scopes: key.scopes || [],
      expiresAt: toLocalDateTimeInput(key.expiresAt),
      enabled: key.enabled !== false,
    });
  };

  const updateAPIKey = async (key, changes, successMessage) => {
    try {
      await apiRequest(`${API_KEYS_BASE}/${key.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          name: key.name,
          kind: key.kind,
          scopes: key.scopes || [],
          expiresAt: key.expiresAt || '',
          enabled: key.enabled !== false,
          ...changes,
        }),
      });
      toast.success(successMessage);
      await loadAPIKeys(true);
    } catch (error) {
      toast.error(error.message || '密钥更新失败');
    }
  };

  const toggleAPIKey = key =>
    updateAPIKey(key, { enabled: !key.enabled }, key.enabled ? '密钥已停用' : '密钥已启用');

  const rotateAPIKey = async key => {
    const confirmed = await dialog.confirm({
      title: '确认轮换密钥',
      message: `轮换“${key.name}”后，旧密钥会立即失效。确定要继续吗？`,
      confirmText: '确认轮换',
      confirmClass: '!bg-kumo-danger !text-white',
    });
    if (!confirmed) return;
    try {
      const payload = await apiRequest(`${API_KEYS_BASE}/${key.id}/rotate`, { method: 'POST' });
      setIssuedAPIKey(payload.apiKey || '');
      toast.success('API Key 已轮换');
      await loadAPIKeys(true);
    } catch (error) {
      toast.error(error.message || '密钥轮换失败');
    }
  };

  const revokeAPIKey = async key => {
    const confirmed = await dialog.confirm({
      title: '确认撤销密钥',
      message: `撤销“${key.name}”后，只有轮换才能重新启用。确定要继续吗？`,
      confirmText: '确认撤销',
      confirmClass: '!bg-kumo-danger !text-white',
    });
    if (!confirmed) return;
    try {
      await apiRequest(`${API_KEYS_BASE}/${key.id}/revoke`, { method: 'POST' });
      if (apiKeyEditingId === key.id) resetAPIKeyForm();
      toast.success('API Key 已撤销');
      await loadAPIKeys(true);
    } catch (error) {
      toast.error(error.message || '密钥撤销失败');
    }
  };

  if (loading) {
    return (
      <PageStack viewport className={apiDocsShellClass}>
        <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
          <Tabs
            {...MODULE_TABS_PROPS}
            value={activeView}
            onValueChange={setActiveView}
            tabs={tabs}
          />
        </div>
        <div className="grid grid-cols-4 gap-2 cq-sm:gap-3">
          {Array.from({ length: 4 }).map((_, index) => (
            <AppCard key={index} padding="none" className="min-w-0 p-2 cq-sm:p-3">
              <SkeletonLine className="h-4 w-20" />
              <SkeletonLine className="mt-3 h-6 w-14" />
            </AppCard>
          ))}
        </div>
        <AppCard padding="lg">
          <SkeletonLine className="h-5 w-36" />
          <SkeletonLine className="mt-4 h-80 w-full" />
        </AppCard>
      </PageStack>
    );
  }

  return (
    <PageStack viewport className={apiDocsShellClass}>
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeView}
          onValueChange={setActiveView}
          tabs={tabs}
        />
        {activeView === 'routes' && (
        <TabBarOverflowActions
          items={[
            {
              key: 'refresh',
              label: '刷新',
              icon: <RefreshCw className="h-3.5 w-3.5" />,
              onClick: () => loadDocs(true),
              disabled: refreshing,
              loading: refreshing,
            },
            {
              key: 'export',
              label: 'OpenAPI',
              icon: <Download className="h-3.5 w-3.5" />,
              onClick: exportOpenAPI,
              disabled: !summary.openapiRoute,
              variant: 'primary',
            },
          ]}
        />
        )}
        {activeView === 'audit' && (
        <TabBarOverflowActions
          items={[
            {
              type: 'select',
              key: 'days',
              label: '时间范围',
              value: String(auditDays),
              onValueChange: value => setAuditDays(Number(value)),
              options: [
                { value: '7', label: '近 7 天' },
                { value: '30', label: '近 30 天' },
                { value: '90', label: '近 90 天' },
              ],
            },
            {
              key: 'refresh',
              label: '刷新',
              icon: <RefreshCw className="h-3.5 w-3.5" />,
              onClick: () => loadAIAudit(true),
              disabled: auditLoading,
              loading: auditLoading,
            },
            {
              key: 'clear',
              label: '清空',
              icon: <Trash className="h-3.5 w-3.5" />,
              onClick: clearAIAudit,
              variant: 'destructive',
            },
          ]}
        />
        )}
      </div>

      {activeView === 'routes' && (
        <div className="flex min-w-0 flex-col gap-2">
          <div className="space-y-2">
            <div className="grid grid-cols-4 gap-2 cq-sm:gap-3">
              <StatCard icon={FileText} label="接口总数" value={summary.total} />
              <StatCard
                icon={Activity}
                label="可用接口"
                value={summary.byStatus.active || 0}
                tone="success"
              />
              <StatCard
                icon={Shield}
                label="登录保护"
                value={summary.byAuth.session || 0}
                tone="warning"
              />
              <StatCard
                icon={Bot}
                label="AI 接口"
                value={aiRouteCount}
                tone="info"
              />
            </div>

            <AppCard padding="md" className="shrink-0">
              <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_repeat(3,minmax(0,0.67fr))] items-center gap-2 cq-sm:grid-cols-[minmax(240px,1.35fr)_repeat(3,minmax(0,0.82fr))]">
                <Input
                  size="sm"
                  aria-label="搜索接口"
                  value={query}
                  onChange={event => setQuery(event.target.value)}
                  placeholder="搜索路径、模块或描述"
                  className="min-w-0 w-full text-xs text-kumo-strong"
                />
                <FilterSelect
                  label="接口分组"
                  value={group}
                  onValueChange={setGroup}
                  items={groupItems}
                />
                <FilterSelect
                  label="认证方式"
                  value={auth}
                  onValueChange={setAuth}
                  items={[
                    { value: 'all', label: '全部认证' },
                    { value: 'public', label: '公开' },
                    { value: 'session', label: '登录' },
                    { value: 'api_key', label: 'API Key' },
                    { value: 'agent_key', label: 'Agent Key' },
                  ]}
                />
                <FilterSelect
                  label="接口状态"
                  value={status}
                  onValueChange={setStatus}
                  items={[
                    { value: 'all', label: '全部状态' },
                    { value: 'active', label: '可用' },
                    { value: 'retired', label: '停用' },
                    { value: 'unknown', label: '未知' },
                  ]}
                />
              </div>
            </AppCard>
          </div>

          <div className="grid min-w-0 gap-3 cq-xl:grid-cols-[minmax(340px,0.9fr)_minmax(0,1.1fr)] cq-2xl:grid-cols-[minmax(360px,0.84fr)_minmax(0,1.16fr)]">
            <RouteTree
              routes={filteredRoutes}
              selectedRoute={selectedRoute}
              onSelect={route => setSelectedKey(getRouteKey(route))}
              revealAll={query.trim().length > 0}
            />
            <div className="min-w-0 cq-xl:sticky cq-xl:top-[70px] cq-xl:max-h-[calc(100vh-82px)] cq-xl:overflow-y-auto cq-xl:overscroll-contain cq-xl:self-start">
              <RouteDetail route={selectedRoute} openapiRoute={summary.openapiRoute} />
            </div>
          </div>
        </div>
      )}

      {activeView === 'ai' && (
        <div className="min-h-0 flex-1">
          <AIAccessConsole
            aiAccess={aiAccess}
            loading={aiLoading}
            error={aiError}
            keyVisible={keyVisible}
            setKeyVisible={setKeyVisible}
            onRefresh={refreshAIAccess}
            onRotateKey={rotateAIKey}
            onToggleWrite={toggleAIWrite}
            onSetPolicy={setAIAccessPolicy}
            onCopy={copyText}
          />
        </div>
      )}

      {activeView === 'audit' && (
        <div className="min-h-0 flex-1">
          <AIAuditConsole
            records={auditRecords}
            total={auditTotal}
            page={auditPage}
            pageSize={auditPageSize}
            loading={auditLoading}
            error={auditError}
            actionFilter={auditAction}
            searchText={auditSearch}
            onActionFilterChange={handleAuditActionChange}
            onSearchTextChange={handleAuditSearchChange}
            onClearFilters={clearAuditFilters}
            onPageChange={setAuditPage}
            onPageSizeChange={setAuditPageSize}
            onRefresh={() => loadAIAudit(true)}
          />
        </div>
      )}

      {activeView === 'keys' && (
        <div className="min-h-0 flex-1">
          <APIKeyConsole
            overview={apiKeyOverview}
            loading={apiKeysLoading}
            error={apiKeysError}
            form={apiKeyForm}
            setForm={setApiKeyForm}
            editingId={apiKeyEditingId}
            submitting={apiKeySubmitting}
            issuedSecret={issuedAPIKey}
            onDismissSecret={() => setIssuedAPIKey('')}
            onSave={saveAPIKey}
            onEdit={editAPIKey}
            onCancelEdit={resetAPIKeyForm}
            onToggle={toggleAPIKey}
            onRotate={rotateAPIKey}
            onRevoke={revokeAPIKey}
            onRefresh={() => loadAPIKeys(true)}
            onCopy={copyText}
          />
        </div>
      )}

    </PageStack>
  );
}

export default ApiDocsPage;
