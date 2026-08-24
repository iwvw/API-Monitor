import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Empty, LayerCard, Tabs } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Select } from '@cloudflare/kumo/components/select';
import DocumentWorkspace from '../components/editor/DocumentWorkspace.jsx';
import PromptDetailsPanel from '../components/prompts/PromptDetailsPanel.jsx';
import PromptWorkspaceSidebar from '../components/prompts/PromptWorkspaceSidebar.jsx';
import {
  PromptCollectionsView,
  PromptPublishedView,
  PromptSettingsPanel,
} from '../components/prompts/PromptManagementViews.jsx';
import useStore from '../store.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { dialog } from '../modules/dialog.js';
import { toast } from '../modules/toast.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { CheckDouble, FileText, Folder, Grid, Plus, Settings, Star } from '../components/Icons.jsx';
import { AlertTriangle } from '../components/IconsCore.jsx';
import { iconButtonIconClass } from '../components/ui/AppPrimitives.jsx';

const API = '/api/prompts';
const TABS = [
  { value: 'workspace', label: <span className="inline-flex items-center gap-1.5"><Grid className="h-3.5 w-3.5" />工作区</span> },
  { value: 'collections', label: <span className="inline-flex items-center gap-1.5"><Folder className="h-3.5 w-3.5" />集合</span> },
  { value: 'published', label: <span className="inline-flex items-center gap-1.5"><Star className="h-3.5 w-3.5" />已发布</span> },
  { value: 'settings', label: <span className="inline-flex items-center gap-1.5"><Settings className="h-3.5 w-3.5" />设置</span> },
];

async function apiFetch(path, options = {}) {
  const response = await fetch(`${API}${path}`, {
    ...options,
    headers: { ...(useStore.getState().getAuthHeaders?.() || {}), ...options.headers },
  });
  if (!response.ok && response.status !== 204) {
    const payload = await response.json().catch(() => ({}));
    const error = new Error(payload.message || payload.error || `HTTP ${response.status}`);
    error.status = response.status;
    error.payload = payload;
    throw error;
  }
  return response.json().catch(() => ({}));
}

export default function PromptLibraryPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const [activeTab, setActiveTab] = useState('workspace');
  const [collections, setCollections] = useState([]);
  const [entries, setEntries] = useState([]);
  const [catalogEntries, setCatalogEntries] = useState([]);
  const [publishedEntries, setPublishedEntries] = useState([]);
  const [selectedCollectionId, setSelectedCollectionId] = useState(null);
  const [selectedEntryId, setSelectedEntryId] = useState(null);
  const [entry, setEntry] = useState(null);
  const [draft, setDraft] = useState(null);
  const [draftRev, setDraftRev] = useState(0);
  const [versions, setVersions] = useState([]);
  const [settings, setSettings] = useState(null);
  const [search, setSearch] = useState('');
  const [starredOnly, setStarredOnly] = useState(false);
  const [conflictOpen, setConflictOpen] = useState(false);
  const [settingsSaving, setSettingsSaving] = useState(false);

  const loadCollections = useCallback(async () => {
    const data = await apiFetch('/collections');
    setCollections(data.collections || []);
  }, []);

  const loadEntries = useCallback(async () => {
    const params = new URLSearchParams();
    if (selectedCollectionId) params.set('collection_id', String(selectedCollectionId));
    if (search.trim()) params.set('q', search.trim());
    if (starredOnly) params.set('starred', 'true');
    try {
      const data = await apiFetch(`/entries?${params}`);
      setEntries(data.entries || []);
    } catch (error) {
      toast.error(`提示词加载失败：${error.message}`);
    }
  }, [search, selectedCollectionId, starredOnly]);

  const loadCatalog = useCallback(async () => {
    const data = await apiFetch('/entries');
    setCatalogEntries(data.entries || []);
  }, []);

  const loadPublished = useCallback(async () => {
    const data = await apiFetch('/entries?published=true');
    setPublishedEntries(data.entries || []);
  }, []);

  const loadEntry = useCallback(async id => {
    if (!id) return;
    try {
      const [entryData, draftData, versionData] = await Promise.all([
        apiFetch(`/entries/${id}`),
        apiFetch(`/entries/${id}/draft`),
        apiFetch(`/entries/${id}/versions`),
      ]);
      setEntry(entryData.entry);
      setDraft(draftData.draft);
      setDraftRev(entryData.entry.current_draft_rev);
      setVersions(versionData.versions || []);
    } catch (error) {
      toast.error(`提示词加载失败：${error.message}`);
    }
  }, []);

  const refreshLists = useCallback(async () => {
    await Promise.all([loadEntries(), loadCatalog(), loadPublished()]);
  }, [loadCatalog, loadEntries, loadPublished]);

  useEffect(() => {
    loadCollections().catch(error => toast.error(`集合加载失败：${error.message}`));
    loadCatalog().catch(error => toast.error(`提示词加载失败：${error.message}`));
    loadPublished().catch(error => toast.error(`发布列表加载失败：${error.message}`));
    apiFetch('/settings')
      .then(data => setSettings(data.settings))
      .catch(error => toast.error(`设置加载失败：${error.message}`));
  }, [loadCatalog, loadCollections, loadPublished]);
  useEffect(() => {
    loadEntries();
  }, [loadEntries]);
  useEffect(() => {
    loadEntry(selectedEntryId);
  }, [loadEntry, selectedEntryId]);

  useEffect(() => {
    if (!entry?.id) return;
    const timer = window.setTimeout(() => {
      apiFetch(`/entries/${entry.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          title: entry.title,
          collection_id: entry.collection_id,
          tags_json: entry.tags_json || '[]',
          starred: Boolean(entry.starred),
          archived: Boolean(entry.archived),
          visibility: entry.visibility || 'unlisted',
        }),
      })
        .then(refreshLists)
        .catch(error => toast.error(`标题保存失败：${error.message}`));
    }, 700);
    return () => window.clearTimeout(timer);
  }, [entry?.id, entry?.title]);  

  const createCollection = async () => {
    const name = await dialog.prompt({
      title: '新建集合',
      message: '输入集合名称',
      placeholder: '例如：运维自动化',
    });
    if (!name?.trim()) return;
    await apiFetch('/collections', {
      method: 'POST',
      body: JSON.stringify({ name: name.trim(), parent_id: null }),
    });
    await loadCollections();
    toast.success('集合已创建');
  };

  const renameCollection = async collection => {
    const name = await dialog.prompt({
      title: '重命名集合',
      message: '输入新的集合名称',
      defaultValue: collection.name,
    });
    if (!name?.trim() || name.trim() === collection.name) return;
    await apiFetch(`/collections/${collection.id}`, {
      method: 'PUT',
      body: JSON.stringify({ ...collection, name: name.trim() }),
    });
    await loadCollections();
    toast.success('集合已更新');
  };

  const deleteCollection = async collection => {
    if (!confirmPress(`collection-${collection.id}`, `删除集合「${collection.name}」`)) return;
    try {
      await apiFetch(`/collections/${collection.id}`, { method: 'DELETE' });
      if (selectedCollectionId === collection.id) setSelectedCollectionId(null);
      await Promise.all([loadCollections(), refreshLists()]);
      toast.success('集合已删除');
    } catch (error) {
      toast.error(`删除失败：${error.message}`);
    }
  };

  const createEntry = async () => {
    const title = await dialog.prompt({
      title: '新建提示词',
      message: '输入提示词标题',
      defaultValue: '未命名提示词',
    });
    if (!title?.trim()) return;
    const data = await apiFetch('/entries', {
      method: 'POST',
      body: JSON.stringify({
        title: title.trim(),
        collection_id: selectedCollectionId,
        tags_json: '[]',
        visibility: settings?.default_visibility || 'unlisted',
      }),
    });
    await refreshLists();
    setSelectedEntryId(data.entry.id);
    setActiveTab('workspace');
  };

  const updateEntry = async patch => {
    if (!entry) return;
    const next = { ...entry, ...patch };
    await apiFetch(`/entries/${entry.id}`, {
      method: 'PUT',
      body: JSON.stringify({
        title: next.title,
        collection_id: next.collection_id,
        tags_json: next.tags_json || '[]',
        starred: Boolean(next.starred),
        archived: Boolean(next.archived),
        visibility: next.visibility || 'unlisted',
      }),
    });
    setEntry(next);
    await refreshLists();
  };

  const toggleEntryStar = async item => {
    if (entry?.id === item.id) {
      await updateEntry({ starred: !item.starred });
      return;
    }
    const data = await apiFetch(`/entries/${item.id}`);
    const target = data.entry;
    await apiFetch(`/entries/${item.id}`, {
      method: 'PUT',
      body: JSON.stringify({ ...target, starred: !target.starred }),
    });
    await refreshLists();
  };

  const saveDraft = async markdown => {
    try {
      const data = await apiFetch(`/entries/${entry.id}/draft`, {
        method: 'PUT',
        body: JSON.stringify({ content_md: markdown, expected_draft_rev: draftRev }),
      });
      setDraft(data.draft);
      setDraftRev(data.draftRev);
      await Promise.all([loadEntries(), loadCatalog()]);
    } catch (error) {
      if (error.status === 409) setConflictOpen(true);
      throw error;
    }
  };

  const publish = async () => {
    if (!entry) return;
    try {
      await apiFetch(`/entries/${entry.id}/publish`, {
        method: 'POST',
        body: JSON.stringify({ expected_draft_rev: draftRev }),
      });
      await Promise.all([loadEntry(entry.id), refreshLists()]);
      toast.success('已发布新版本');
    } catch (error) {
      toast.error(`发布失败：${error.message}`);
    }
  };

  const restoreVersion = async version => {
    await apiFetch(`/entries/${entry.id}/versions/${version.id}/restore`, { method: 'POST' });
    await loadEntry(entry.id);
    toast.success(`v${version.version_no} 已恢复到草稿`);
  };

  const deleteEntry = async target => {
    if (!confirmPress(`entry-${target.id}`, `删除提示词「${target.title}」`)) return;
    try {
      await apiFetch(`/entries/${target.id}`, { method: 'DELETE' });
      if (selectedEntryId === target.id) {
        setSelectedEntryId(null);
        setEntry(null);
        setDraft(null);
      }
      await refreshLists();
      toast.success('提示词已删除');
    } catch (error) {
      toast.error(`删除失败：${error.message}`);
    }
  };

  const saveSettings = async () => {
    setSettingsSaving(true);
    try {
      await apiFetch('/settings', { method: 'PUT', body: JSON.stringify(settings) });
      toast.success('设置已保存');
    } catch (error) {
      toast.error(`设置保存失败：${error.message}`);
    } finally {
      setSettingsSaving(false);
    }
  };

  const workspaceSidebar = useMemo(
    () => (
      <PromptWorkspaceSidebar
        collections={collections}
        entries={entries}
        search={search}
        onSearchChange={setSearch}
        selectedCollectionId={selectedCollectionId}
        onSelectCollection={setSelectedCollectionId}
        selectedEntryId={selectedEntryId}
        onSelectEntry={setSelectedEntryId}
        starredOnly={starredOnly}
        onToggleStarredOnly={() => setStarredOnly(value => !value)}
        onCreateEntry={createEntry}
        onToggleEntryStar={toggleEntryStar}
      />
    ),
    [collections, entries, search, selectedCollectionId, selectedEntryId, starredOnly]
  );  

  const detailsPanel = entry ? (
    <PromptDetailsPanel
      entry={entry}
      versions={versions}
      onUpdate={updateEntry}
      onRestoreVersion={restoreVersion}
    />
  ) : null;

  const workspaceView = (
    <LayerCard className="flex min-h-0 flex-1 overflow-hidden p-0">
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <div className="flex shrink-0 items-center gap-2 border-b border-kumo-line p-2 cq-md:hidden">
          <div className="min-w-0 flex-1">
            <Select
              size="sm"
              className="w-full"
              value={String(selectedEntryId || '')}
              onValueChange={value => setSelectedEntryId(Number(value) || null)}
              aria-label="选择提示词"
              placeholder="选择提示词"
              renderValue={value =>
                entries.find(item => item.id === Number(value))?.title || '选择提示词'
              }
            >
              {entries.map(item => (
                <Select.Option key={item.id} value={String(item.id)}>
                  {item.title}
                </Select.Option>
              ))}
            </Select>
          </div>
          <Button
            size="sm"
            variant="primary"
            shape="square"
            aria-label="新建提示词"
            icon={<Plus className={iconButtonIconClass} />}
            onClick={createEntry}
          />
        </div>
        {entry && draft ? (
          <DocumentWorkspace
            key={entry.id}
            initialMarkdown={draft.content_md}
            title={entry.title}
            onTitleChange={title => setEntry(current => ({ ...current, title }))}
            onSave={saveDraft}
            autosaveDelay={1800}
            showOutline
            leftPanel={{ content: workspaceSidebar, className: 'hidden !w-64 cq-md:flex cq-xl:!w-72' }}
            rightPanel={{ title: '发布与属性', content: detailsPanel }}
            extraToolbarActions={
              <>
                <Button
                  size="sm"
                  variant={entry.starred ? 'primary' : 'secondary'}
                  shape="square"
                  aria-label={entry.starred ? '取消收藏' : '收藏'}
                  icon={<Star className={iconButtonIconClass} />}
                  onClick={() => updateEntry({ starred: !entry.starred })}
                />
                <Button
                  size="sm"
                  variant="primary"
                  icon={<CheckDouble className={iconButtonIconClass} />}
                  onClick={publish}
                >
                  发布
                </Button>
              </>
            }
          />
        ) : (
          <div className="flex min-h-0 flex-1">
            <div className="hidden w-64 shrink-0 border-r border-kumo-line cq-md:block cq-xl:w-72">
              {workspaceSidebar}
            </div>
            <Empty
              className="min-h-80 flex-1"
              icon={<FileText className="h-10 w-10 text-kumo-inactive" />}
              title="选择一条提示词开始编辑"
              description="左侧用于整理和选择条目，正文会自动保存为草稿。"
              contents={
                <Button
                  size="sm"
                  variant="primary"
                  icon={<Plus className={iconButtonIconClass} />}
                  onClick={createEntry}
                >
                  新建提示词
                </Button>
              }
            />
          </div>
        )}
      </div>
    </LayerCard>
  );

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col gap-3">
      <Tabs
        className="shrink-0 self-start"
        {...MODULE_TABS_PROPS}
        value={activeTab}
        onValueChange={setActiveTab}
        tabs={TABS}
      />
      <div className="flex min-h-0 flex-1">
        {activeTab === 'workspace' && workspaceView}
        {activeTab === 'collections' && (
          <PromptCollectionsView
            collections={collections}
            entries={catalogEntries}
            onCreate={createCollection}
            onRename={renameCollection}
            onDelete={deleteCollection}
            deleteIsArmed={id => isArmed(`collection-${id}`)}
            onOpenCollection={collectionId => {
              setSelectedCollectionId(collectionId);
              setActiveTab('workspace');
            }}
          />
        )}
        {activeTab === 'published' && (
          <PromptPublishedView
            entries={publishedEntries}
            onOpen={id => {
              setSelectedEntryId(id);
              setActiveTab('workspace');
            }}
            onDelete={deleteEntry}
            deleteIsArmed={id => isArmed(`entry-${id}`)}
          />
        )}
        {activeTab === 'settings' && (
          <PromptSettingsPanel
            settings={settings}
            onChange={patch => setSettings(current => ({ ...current, ...patch }))}
            onSave={saveSettings}
            saving={settingsSaving}
          />
        )}
      </div>

      <Dialog.Root open={conflictOpen} onOpenChange={setConflictOpen} role="alertdialog">
        <Dialog className="p-6">
          <div className="flex items-center gap-3">
            <AlertTriangle className="h-5 w-5 text-kumo-warning" />
            <Dialog.Title>草稿冲突</Dialog.Title>
          </div>
          <Dialog.Description className="mt-3 text-kumo-subtle">
            另一会话已更新此草稿。
          </Dialog.Description>
          <div className="mt-6 flex justify-end">
            <Button
              size="sm"
              variant="primary"
              onClick={() => {
                setConflictOpen(false);
                loadEntry(entry.id);
              }}
            >
              加载最新草稿
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}
