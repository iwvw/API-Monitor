import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { Tabs } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import {
  ResponsiveSearchInput,
  SectionCard,
  TabBarOverflowActions,
  stickyTabsBaseClass,
} from '../../components/ui/AppPrimitives.jsx';
import { Plus, RefreshCw, Settings } from '../../components/Icons.jsx';
import { assetTabs } from './tabs.jsx';
import { CATEGORIES, PAGE_SIZE, PERSISTED_STATUSES } from './constants.js';
import { parseWarnDays } from './utils.js';
import {
  createAsset,
  deleteAsset,
  fetchAssets,
  fetchCandidates,
  fetchOverview,
  fetchSettings,
  linkAssets,
  refreshAllAssets,
  refreshAsset,
  resetSettings,
  updateAsset,
  updateSettings,
} from './api.js';
import OverviewPanel from './OverviewPanel.jsx';
import AssetTable from './AssetTable.jsx';
import AssetFormDialog from './AssetFormDialog.jsx';
import AssetDetailDialog from './AssetDetailDialog.jsx';
import SourcePickerPanel from './SourcePickerPanel.jsx';

const bucketToStatus = {
  expired: 'expired',
  within_7: 'expiring',
  within_30: 'expiring',
  normal: 'active',
};

function SettingsDialog({ open, settings, saving, onClose, onSave, onReset }) {
  const [baseCurrency, setBaseCurrency] = useState('');
  const [warnDays, setWarnDays] = useState('');

  useEffect(() => {
    if (!open) return;
    setBaseCurrency(settings?.base_currency || '');
    setWarnDays(Array.isArray(settings?.warn_days) ? settings.warn_days.join(', ') : '');
  }, [open, settings]);

  const save = () => {
    void onSave({ base_currency: baseCurrency.trim(), warn_days: parseWarnDays(warnDays) });
  };

  return (
    <LayerDialog.Root open={open} onOpenChange={next => { if (!next) onClose(); }}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>资产设置</LayerDialog.Title>
        <LayerDialog.Description>
          配置成本合计的基准币种与全局到期告警阈值。留空基准币种则按币种分组展示，不做合计。
        </LayerDialog.Description>
        <LayerDialog.Body>
          <div className="space-y-4">
            <Input
              size="sm"
              label="基准币种"
              value={baseCurrency}
              onChange={event => setBaseCurrency(event.target.value)}
              placeholder="如 CNY，留空表示不合计"
            />
            <Input
              size="sm"
              label="全局告警阈值（天，逗号分隔）"
              value={warnDays}
              onChange={event => setWarnDays(event.target.value)}
              placeholder="如 30, 14, 7, 1"
            />
            <Button
              size="sm"
              variant="secondary"
              onClick={() => void onReset()}
              disabled={saving}
            >恢复默认设置</Button>
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" loading={saving} onClick={save}>保存</LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
function AssetsPage() {
  const [activeTab, setActiveTab] = useState('overview');
  const [assets, setAssets] = useState([]);
  const [overview, setOverview] = useState(null);
  const [overviewLoading, setOverviewLoading] = useState(false);
  const [settings, setSettings] = useState(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [formOpen, setFormOpen] = useState(false);
  const [formMode, setFormMode] = useState('create');
  const [editing, setEditing] = useState(null);
  const [detailAsset, setDetailAsset] = useState(null);
  const [candidates, setCandidates] = useState([]);
  const [candidatesLoading, setCandidatesLoading] = useState(false);
  const [linking, setLinking] = useState('');
  const [refreshingId, setRefreshingId] = useState('');
  const [detailNonce, setDetailNonce] = useState(0);

  const [query, setQuery] = useState('');
  const [debouncedQuery, setDebouncedQuery] = useState('');
  const [typeFilter, setTypeFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [bucketFilter, setBucketFilter] = useState('');
  const [expiringFilter, setExpiringFilter] = useState('');

  const category = activeTab === 'physical' || activeTab === 'virtual' ? activeTab : '';

  // 搜索防抖：输入时更新 query，停顿 300ms 后才触发请求，
  // 避免每敲一个字都打一次后端。
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedQuery(query), 300);
    return () => window.clearTimeout(timer);
  }, [query]);

  const loadOverview = useCallback(async () => {
    setOverviewLoading(true);
    try {
      const data = await fetchOverview();
      setOverview(data);
    } catch (error) {
      toast.error(error.message || '加载资产总览失败');
    } finally {
      setOverviewLoading(false);
    }
  }, []);

  const loadAssets = useCallback(async (cat) => {
    if (!cat) return;
    setLoading(true);
    try {
      const list = await fetchAssets({
        category: cat,
        q: debouncedQuery.trim(),
        asset_type: typeFilter,
        status: statusFilter,
        bucket: bucketFilter,
        expiring_within: expiringFilter,
        limit: PAGE_SIZE,
      });
      setAssets(list);
    } catch (error) {
      toast.error(error.message || '加载资产失败');
      setAssets([]);
    } finally {
      setLoading(false);
    }
  }, [debouncedQuery, typeFilter, statusFilter, bucketFilter, expiringFilter]);

  const loadSettings = useCallback(async () => {
    try {
      const data = await fetchSettings();
      setSettings(data);
    } catch {
      setSettings(null);
    }
  }, []);

  const loadCandidates = useCallback(async () => {
    setCandidatesLoading(true);
    try {
      const groups = await fetchCandidates();
      setCandidates(groups);
    } catch (error) {
      toast.error(error.message || '加载纳管来源失败');
      setCandidates([]);
    } finally {
      setCandidatesLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadOverview();
    void loadSettings();
  }, [loadOverview, loadSettings]);

  useEffect(() => {
    if (!category) return;
    void loadAssets(category);
  }, [category, loadAssets]);

  useEffect(() => {
    if (activeTab !== 'sources') return;
    void loadCandidates();
  }, [activeTab, loadCandidates]);

  const linkSelected = async links => {
    setLinking('link');
    try {
      const results = await linkAssets(links);
      const linked = results.filter(item => item.status === 'linked').length;
      const skipped = results.filter(item => item.status === 'skipped').length;
      if (linked > 0) toast.success(`已纳管 ${linked} 个对象`);
      if (skipped > 0) toast.warning(`${skipped} 个对象已纳管，已跳过`);
      await Promise.all([loadCandidates(), loadOverview()]);
      return true;
    } catch (error) {
      toast.error(error.message || '纳管失败');
      return false;
    } finally {
      setLinking('');
    }
  };

  const refreshAll = async () => {
    setLinking('refresh');
    try {
      const result = await refreshAllAssets();
      toast.success(`已刷新 ${result.refreshed ?? 0} 个纳管资产`);
      if (result.failed) toast.warning(`${result.failed} 个刷新失败`);
      await Promise.all([loadCandidates(), loadOverview(), category ? loadAssets(category) : Promise.resolve()]);
    } catch (error) {
      toast.error(error.message || '刷新失败');
    } finally {
      setLinking('');
    }
  };

  const typeOptions = useMemo(() => {
    const found = CATEGORIES.find(item => item.value === category);
    return [{ value: '', label: '全部类型' }, ...(found?.types || [])];
  }, [category]);

  const openCreate = () => {
    setFormMode('create');
    setEditing(null);
    setFormOpen(true);
  };

  const openEdit = asset => {
    setFormMode('edit');
    setEditing(asset);
    setFormOpen(true);
  };

  const submitForm = async payload => {
    setSaving(true);
    try {
      if (formMode === 'edit' && editing) {
        await updateAsset(editing.id, payload);
        toast.success('资产已更新');
      } else {
        await createAsset(payload);
        toast.success('资产已登记');
      }
      setFormOpen(false);
      setEditing(null);
      await Promise.all([loadAssets(category), loadOverview()]);
    } catch (error) {
      toast.error(error.message || '保存资产失败');
    } finally {
      setSaving(false);
    }
  };

  const confirmDelete = async asset => {
    const ok = await dialog.deleteResource({
      title: '删除资产',
      message: `确定删除资产「${asset.name}」吗？此操作不可撤销。`,
      confirmText: '删除',
    });
    if (!ok) return;
    try {
      await deleteAsset(asset.id);
      toast.success('资产已删除');
      await Promise.all([loadAssets(category), loadOverview()]);
    } catch (error) {
      toast.error(error.message || '删除资产失败');
    }
  };

  const saveSettings = async payload => {
    setSaving(true);
    try {
      const saved = await updateSettings(payload);
      setSettings(saved);
      toast.success('设置已保存');
      setSettingsOpen(false);
      await loadOverview();
    } catch (error) {
      toast.error(error.message || '保存设置失败');
    } finally {
      setSaving(false);
    }
  };

  const doResetSettings = async () => {
    setSaving(true);
    try {
      const saved = await resetSettings();
      setSettings(saved);
      toast.success('已恢复默认设置');
      setSettingsOpen(false);
      await loadOverview();
    } catch (error) {
      toast.error(error.message || '重置设置失败');
    } finally {
      setSaving(false);
    }
  };

  const refreshOne = async asset => {
    setRefreshingId(asset.id);
    try {
      const updated = await refreshAsset(asset.id);
      if (updated.status === 'orphan') {
        toast.warning('来源对象已消失，资产已标记为来源失效');
      } else {
        toast.success('已刷新来源快照');
      }
      // 详情弹窗开着时同步最新快照，并触发事件/告警重新拉取。
      setDetailAsset(prev => (prev && prev.id === asset.id ? updated : prev));
      setDetailNonce(n => n + 1);
      await Promise.all([loadAssets(category), loadOverview()]);
    } catch (error) {
      toast.error(error.message || '刷新失败');
    } finally {
      setRefreshingId('');
    }
  };

  const selectBucket = bucket => {
    // 桶口径与总览一致：no_renew 同时含「已退役」与「来源失效」，不能用单一
    // status 表达，改传 bucket 参数让后端按同一 bucketFor 判定。
    setBucketFilter(bucket === 'no_renew' ? 'no_renew' : '');
    setStatusFilter(bucket === 'no_renew' ? '' : (bucketToStatus[bucket] || ''));
    // 「不续费」桶指向已退役/来源失效，落在虚拟视图；其余桶保持当前视图，
    // 若当前在总览则默认切到实体视图。
    let target = activeTab;
    if (bucket === 'no_renew') target = 'virtual';
    else if (activeTab === 'overview') target = 'physical';
    setActiveTab(target);
  };

  const tabsHeader = (
    <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
      <div className="min-w-0 w-full cq-md:w-auto">
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={tab => {
            // 切换 tab 时清掉桶筛选，否则 no_renew 这类桶会残留并静默过滤其他视图。
            setBucketFilter('');
            setActiveTab(tab);
          }}
          tabs={assetTabs}
        />
      </div>
      <TabBarOverflowActions
        items={[
          ...(category
            ? [
                {
                  key: 'refresh',
                  label: '刷新',
                  icon: <RefreshCw className="w-3.5 h-3.5" />,
                  onClick: () => void loadAssets(category),
                  loading,
                },
              ]
            : []),
          {
            key: 'settings',
            label: '设置',
            icon: <Settings className="w-3.5 h-3.5" />,
            onClick: () => setSettingsOpen(true),
          },
          {
            key: 'create',
            label: '登记资产',
            icon: <Plus className="w-3.5 h-3.5" />,
            variant: 'primary',
            onClick: openCreate,
          },
        ]}
      />
    </div>
  );

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      {tabsHeader}

      <div className="min-w-0">
        {activeTab === 'overview' && (
          <OverviewPanel
            overview={overview}
            loading={overviewLoading}
            onSelectBucket={selectBucket}
            onOpenAsset={setDetailAsset}
          />
        )}

        {activeTab === 'sources' && (
          <SourcePickerPanel
            groups={candidates}
            loading={candidatesLoading}
            linking={linking}
            onLink={linkSelected}
            onRefreshAll={refreshAll}
          />
        )}

        {(activeTab === 'physical' || activeTab === 'virtual') && (
          <div className="flex min-w-0 flex-col gap-3">
            <SectionCard
              icon={null}
              title={activeTab === 'physical' ? '实体资产' : '虚拟资产'}
              description="双击行查看详情"
              actions={(
                <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
                  <ResponsiveSearchInput
                    value={query}
                    onChange={event => setQuery(event.target.value)}
                    onSearch={() => void loadAssets(category)}
                    placeholder="搜索名称、提供方、负责人"
                    ariaLabel="搜索资产"
                    className="w-52"
                  />
                  <Select
                    alignItemWithTrigger
                    size="sm"
                    aria-label="类型筛选"
                    className="w-36"
                    value={typeFilter}
                    onValueChange={setTypeFilter}
                    items={typeOptions}
                  />
                  <Select
                    alignItemWithTrigger
                    size="sm"
                    aria-label="状态筛选"
                    className="w-32"
                    value={statusFilter}
                    onValueChange={value => { setBucketFilter(''); setStatusFilter(value); }}
                    items={[{ value: '', label: '全部状态' }, ...PERSISTED_STATUSES, { value: 'expiring', label: '即将到期' }, { value: 'expired', label: '已过期' }]}
                  />
                  <Select
                    alignItemWithTrigger
                    size="sm"
                    aria-label="到期筛选"
                    className="w-32"
                    value={expiringFilter}
                    onValueChange={setExpiringFilter}
                    items={[{ value: '', label: '不限到期' }, { value: '7', label: '7 天内' }, { value: '30', label: '30 天内' }, { value: '90', label: '90 天内' }]}
                  />
                </div>
              )}
              bodyPadding="none"
            >
              <AssetTable
                assets={assets}
                loading={loading}
                category={category}
                onCreate={openCreate}
                onEdit={openEdit}
                onDelete={confirmDelete}
                onOpenAsset={setDetailAsset}
                onRefresh={refreshOne}
                refreshingId={refreshingId}
              />
            </SectionCard>
          </div>
        )}
      </div>

      <AssetFormDialog
        open={formOpen}
        mode={formMode}
        asset={editing}
        saving={saving}
        defaultCategory={category || 'physical'}
        onClose={() => { setFormOpen(false); setEditing(null); }}
        onSubmit={submitForm}
      />

      <AssetDetailDialog
        open={Boolean(detailAsset)}
        asset={detailAsset}
        refreshNonce={detailNonce}
        refreshing={refreshingId === detailAsset?.id}
        onRefresh={refreshOne}
        onClose={() => setDetailAsset(null)}
      />

      <SettingsDialog
        open={settingsOpen}
        settings={settings}
        saving={saving}
        onClose={() => setSettingsOpen(false)}
        onSave={saveSettings}
        onReset={doResetSettings}
      />
    </div>
  );
}

export default AssetsPage;
