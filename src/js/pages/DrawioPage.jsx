import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Badge, Button, Empty, LayerCard, Popover, Tabs, Toolbar } from '@cloudflare/kumo';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import DrawioFrame from '../components/drawio/DrawioFrame.jsx';
import DrawioLibraryView from '../components/drawio/DrawioLibraryView.jsx';
import DrawioSettingsView from '../components/drawio/DrawioSettingsView.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
import useStore from '../store.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { dialog } from '../modules/dialog.js';
import { toast } from '../modules/toast.js';
import {
  ChevronDown,
  ChevronUp,
  Compass,
  Copy,
  Download,
  Grid,
  Image,
  Plus,
  Save,
  Settings,
  Upload,
} from '../components/Icons.jsx';
import { AlertTriangle } from '../components/IconsCore.jsx';
import {
  PageStack,
  ResponsiveSearchInput,
  iconButtonIconClass,
  stickyTabsBaseClass,
} from '../components/ui/AppPrimitives.jsx';

const API = '/api/drawio';
const TABS = [
  { value: 'editor', label: <span className="inline-flex items-center gap-1.5"><Compass className="h-3.5 w-3.5" />主界面</span> },
  { value: 'library', label: <span className="inline-flex items-center gap-1.5"><Grid className="h-3.5 w-3.5" />图库</span> },
  { value: 'settings', label: <span className="inline-flex items-center gap-1.5"><Settings className="h-3.5 w-3.5" />设置</span> },
];

async function apiFetch(path, options = {}) {
  const headers = { ...(useStore.getState().getAuthHeaders?.() || {}), ...options.headers };
  const response = await fetch(`${API}${path}`, { ...options, headers });
  if (!response.ok) {
    const payload = await response.json().catch(() => ({}));
    const error = new Error(payload.message || payload.error || `HTTP ${response.status}`);
    error.status = response.status;
    error.payload = payload;
    throw error;
  }
  return response.json().catch(() => ({}));
}

async function exportDataToBlob(data, mimeType) {
  const value = String(data || '');
  if (!value) throw new Error('画布没有返回导出内容');
  if (value.startsWith('data:')) return fetch(value).then(response => response.blob());
  return new Blob([value], { type: mimeType });
}

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}

function blobToDataUrl(blob) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ''));
    reader.onerror = () => reject(reader.error || new Error('读取缩略图失败'));
    reader.readAsDataURL(blob);
  });
}

export default function DrawioPage() {
  const theme = useStore(state => state.theme);
  const [activeTab, setActiveTab] = useState('editor');
  const [documents, setDocuments] = useState([]);
  const [selectedId, setSelectedId] = useState(null);
  const [currentDoc, setCurrentDoc] = useState(null);
  const [draft, setDraft] = useState(null);
  const [draftRev, setDraftRev] = useState(0);
  const [versions, setVersions] = useState([]);
  const [settings, setSettings] = useState(null);
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(true);
  const [dirty, setDirty] = useState(false);
  const [saveState, setSaveState] = useState('idle');
  const [conflictOpen, setConflictOpen] = useState(false);
  const [xmlOpen, setXmlOpen] = useState(false);
  const [settingsSaving, setSettingsSaving] = useState(false);
  const [libraryCopyJob, setLibraryCopyJob] = useState(null);
  const [thumbnailRenderTask, setThumbnailRenderTask] = useState(null);
  const frameRef = useRef(null);
  const libraryCopyFrameRef = useRef(null);
  const thumbnailRenderFrameRef = useRef(null);
  const thumbnailRenderPromiseRef = useRef(null);
  const thumbnailRefreshQueueRef = useRef(null);
  const thumbnailRefreshRunningRef = useRef(false);
  const fileRef = useRef(null);
  const xmlSyncTimerRef = useRef(null);

  const loadDocuments = useCallback(async () => {
    try {
      const data = await apiFetch(`/documents?q=${encodeURIComponent(search)}`);
      setDocuments(data.documents || []);
      setSelectedId(current => current || data.documents?.[0]?.id || null);
    } catch (error) {
      toast.error(`图库加载失败：${error.message}`);
    } finally {
      setLoading(false);
    }
  }, [search]);

  const loadDocument = useCallback(async id => {
    if (!id) return;
    try {
      const [docData, draftData, versionData] = await Promise.all([
        apiFetch(`/documents/${id}`),
        apiFetch(`/documents/${id}/draft`),
        apiFetch(`/documents/${id}/versions`),
      ]);
      setCurrentDoc(docData.document);
      setDraft(draftData.draft);
      setDraftRev(docData.document.draft_rev);
      setVersions(versionData.versions || []);
      setDirty(false);
      setSaveState('idle');
    } catch (error) {
      toast.error(`图表加载失败：${error.message}`);
    }
  }, []);

  const loadSettings = useCallback(async () => {
    try {
      const data = await apiFetch('/settings');
      setSettings(data.settings);
    } catch (error) {
      toast.error(`设置加载失败：${error.message}`);
    }
  }, []);

  useEffect(() => {
    loadDocuments();
  }, [loadDocuments]);
  useEffect(() => {
    loadDocument(selectedId);
  }, [loadDocument, selectedId]);
  useEffect(() => {
    if (!settings) loadSettings();
  }, [loadSettings, settings]);
  useEffect(() => () => window.clearTimeout(xmlSyncTimerRef.current), [selectedId]);
  useEffect(
    () => () => {
      if (thumbnailRenderPromiseRef.current) {
        thumbnailRenderPromiseRef.current.reject(new Error('预览生成已取消'));
        thumbnailRenderPromiseRef.current = null;
      }
    },
    []
  );

  const updateXmlFromSource = useCallback(xml => {
    setDraft(current => ({ ...current, xml_content: xml }));
    setDirty(true);
    window.clearTimeout(xmlSyncTimerRef.current);
    xmlSyncTimerRef.current = window.setTimeout(() => frameRef.current?.load(xml), 250);
  }, []);

  const renderThumbnailDataUrl = useCallback(
    xml =>
      new Promise((resolve, reject) => {
        const nextXML = String(xml || '');
        if (!nextXML) {
          reject(new Error('图表没有可用于生成预览的 XML'));
          return;
        }
        if (thumbnailRenderPromiseRef.current) {
          reject(new Error('已有预览生成任务正在进行中'));
          return;
        }
        thumbnailRenderPromiseRef.current = { resolve, reject };
        setThumbnailRenderTask({
          key: `thumbnail-${Date.now()}`,
          xml: nextXML,
        });
      }),
    []
  );

  const applyThumbnailState = useCallback((documentId, patch) => {
    setDocuments(current =>
      current.map(document => (document.id === documentId ? { ...document, ...patch } : document))
    );
    setCurrentDoc(current =>
      current?.id === documentId ? { ...current, ...patch } : current
    );
  }, []);

  const finishThumbnailRender = useCallback(async () => {
    const pending = thumbnailRenderPromiseRef.current;
    if (!pending) return;
    try {
      await new Promise(resolve => window.setTimeout(resolve, 0));
      const data = await thumbnailRenderFrameRef.current?.exportSVG();
      const thumbnailPath = await blobToDataUrl(await exportDataToBlob(data, 'image/svg+xml'));
      pending.resolve(thumbnailPath);
    } catch (error) {
      pending.reject(error);
    } finally {
      thumbnailRenderPromiseRef.current = null;
      setThumbnailRenderTask(null);
    }
  }, []);

  const saveThumbnail = useCallback(async (documentId, thumbnailPath) => {
    await apiFetch(`/documents/${documentId}/thumbnail`, {
      method: 'PUT',
      body: JSON.stringify({ thumbnail_path: thumbnailPath }),
    });
  }, []);

  const generateThumbnail = useCallback(
    async (documentId, xmlContent = null) => {
      const xml =
        xmlContent != null
          ? String(xmlContent || '')
          : String((await apiFetch(`/documents/${documentId}/draft`)).draft?.xml_content || '');
      const thumbnailPath = await renderThumbnailDataUrl(xml);
      await saveThumbnail(documentId, thumbnailPath);
      return thumbnailPath;
    },
    [renderThumbnailDataUrl, saveThumbnail]
  );

  const drainThumbnailRefreshQueue = useCallback(async () => {
    if (thumbnailRefreshRunningRef.current) return;
    const next = thumbnailRefreshQueueRef.current;
    if (!next) return;

    thumbnailRefreshRunningRef.current = true;
    thumbnailRefreshQueueRef.current = null;
    try {
      const thumbnailPath = await generateThumbnail(next.documentId, next.xml);
      applyThumbnailState(next.documentId, {
        thumbnail_path: thumbnailPath,
        thumbnail_status: 'ready',
      });
    } catch (error) {
      applyThumbnailState(next.documentId, {
        thumbnail_status: 'failed',
      });
      if (!next.silent) {
        toast.error(`生成预览失败：${error.message}`);
      }
    } finally {
      thumbnailRefreshRunningRef.current = false;
      if (thumbnailRefreshQueueRef.current) {
        void drainThumbnailRefreshQueue();
      }
    }
  }, [applyThumbnailState, generateThumbnail]);

  const queueThumbnailRefresh = useCallback(
    (documentId, xml, options = {}) => {
      thumbnailRefreshQueueRef.current = {
        documentId,
        xml,
        silent: options.silent !== false,
      };
      applyThumbnailState(documentId, {
        thumbnail_path: '',
        thumbnail_status: 'pending',
      });
      void drainThumbnailRefreshQueue();
    },
    [applyThumbnailState, drainThumbnailRefreshQueue]
  );

  const saveDraft = useCallback(
    async (xml = draft?.xml_content) => {
      if (!selectedId || !xml || saveState === 'saving') return;
      setSaveState('saving');
      try {
        const data = await apiFetch(`/documents/${selectedId}/draft`, {
          method: 'PUT',
          body: JSON.stringify({
            xml_content: xml,
            expected_draft_rev: draftRev,
            editor_state_json: '{}',
          }),
        });
        setDraft(data.draft);
        setDraftRev(data.draftRev);
        setDirty(false);
        setSaveState('saved');
        window.setTimeout(() => setSaveState('idle'), 1500);
        loadDocuments();
        queueThumbnailRefresh(selectedId, xml, { silent: true });
        return data.draftRev;
      } catch (error) {
        setSaveState('error');
        if (error.status === 409) setConflictOpen(true);
        else toast.error(`保存失败：${error.message}`);
      }
    },
    [draft?.xml_content, draftRev, loadDocuments, queueThumbnailRefresh, saveState, selectedId]
  );

  useEffect(() => {
    if (!dirty || !settings?.autosave_enabled) return;
    const timer = window.setTimeout(() => saveDraft(), settings.autosave_debounce_ms || 2000);
    return () => window.clearTimeout(timer);
  }, [dirty, saveDraft, settings?.autosave_debounce_ms, settings?.autosave_enabled]);

  const createDocument = async () => {
    const title = await dialog.prompt({
      title: '新建图表',
      message: '输入图表名称，留空将按月日时分自动命名',
      placeholder: '例如：系统架构图',
      defaultValue: '',
    });
    if (title === null) return;
    const data = await apiFetch('/documents', {
      method: 'POST',
      body: JSON.stringify({ title: title.trim(), tags_json: '[]' }),
    });
    await loadDocuments();
    setSelectedId(data.document.id);
    setActiveTab('editor');
  };

  const importDocument = async file => {
    if (!file) return;
    const form = new FormData();
    form.append('file', file);
    try {
      const data = await apiFetch('/import', { method: 'POST', body: form, headers: {} });
      await loadDocuments();
      setSelectedId(data.document.id);
      setActiveTab('editor');
      toast.success('图表已导入');
    } catch (error) {
      toast.error(`导入失败：${error.message}`);
    }
  };

  const exportDocument = async (format, versionId = null) => {
    if (!selectedId) return;
    try {
      if (format === 'svg') {
        const data = await frameRef.current?.exportSVG();
        const value = String(data || '');
        const blob = value.startsWith('data:')
          ? await fetch(value).then(response => response.blob())
          : new Blob([value], { type: 'image/svg+xml' });
        downloadBlob(blob, `${currentDoc?.title || 'diagram'}.svg`);
        return;
      }
      const params = new URLSearchParams({ format });
      if (versionId) {
        params.set('source', 'version');
        params.set('versionId', String(versionId));
      }
      const response = await fetch(`${API}/documents/${selectedId}/export?${params}`, {
        headers: useStore.getState().getAuthHeaders?.() || {},
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      downloadBlob(await response.blob(), `${currentDoc?.title || 'diagram'}.${format}`);
    } catch (error) {
      toast.error(`导出失败：${error.message}`);
    }
  };

  const copyCanvas = async format => {
    if (!selectedId || !frameRef.current) return;
    try {
      if (format === 'png') {
        const data = await frameRef.current.exportPNG(3);
        const blob = await exportDataToBlob(data, 'image/png');
        if (!window.ClipboardItem || !navigator.clipboard?.write) {
          throw new Error('当前浏览器不支持复制图片');
        }
        await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })]);
        toast.success('高清 PNG 已复制');
        return;
      }

      const data = await frameRef.current.exportSVG();
      const blob = await exportDataToBlob(data, 'image/svg+xml');
      const svg = await blob.text();
      await navigator.clipboard.writeText(svg);
      toast.success('SVG 原图已复制');
    } catch (error) {
      toast.error(`复制失败：${error.message}`);
    }
  };

  const saveVersion = async () => {
    const versionRevision = dirty ? await saveDraft() : draftRev;
    const summary = await dialog.prompt({
      title: '保存版本',
      message: '填写本次版本备注（可留空）',
      defaultValue: '',
    });
    if (summary === null) return;
    try {
      await apiFetch(`/documents/${selectedId}/versions`, {
        method: 'POST',
        body: JSON.stringify({ summary, expected_draft_rev: versionRevision || draftRev }),
      });
      await loadDocument(selectedId);
      toast.success('版本已保存');
    } catch (error) {
      toast.error(`版本保存失败：${error.message}`);
    }
  };

  const deleteDocument = async document => {
    try {
      await apiFetch(`/documents/${document.id}`, { method: 'DELETE' });
      if (selectedId === document.id) {
        setSelectedId(null);
        setCurrentDoc(null);
        setDraft(null);
      }
      await loadDocuments();
      toast.success('图表已删除');
    } catch (error) {
      toast.error(`删除失败：${error.message}`);
    }
  };

  const externalAssets = useMemo(() => {
    try {
      return JSON.parse(draft?.external_assets_json || '[]');
    } catch {
      return [];
    }
  }, [draft?.external_assets_json]);

  const editorView = (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <LayerCard className="grid min-h-0 flex-1 grid-cols-1 cq-lg:grid-cols-[minmax(0,1fr)_20rem]">
        {selectedId && draft ? (
          <DrawioFrame
            ref={frameRef}
            key={selectedId}
            xml={draft.xml_content}
            theme={theme}
            onChange={xml => {
              setDraft(current => ({ ...current, xml_content: xml }));
              setDirty(true);
            }}
          />
        ) : (
          <Empty
            className="min-h-[32rem]"
            icon={<Image className="h-10 w-10 text-kumo-inactive" />}
            title="尚未选择图表"
            description="新建或从图库中选择一个图表开始编辑。"
            contents={
              <Button
                size="sm"
                variant="primary"
                icon={<Plus className={iconButtonIconClass} />}
                onClick={createDocument}
              >
                新建图表
              </Button>
            }
          />
        )}
        <div className="hidden min-h-0 flex-col border-l border-kumo-line cq-lg:flex">
          <Button
            type="button"
            variant="ghost"
            className="flex h-[30.5px] shrink-0 items-center justify-between gap-2 rounded-none border-b border-kumo-line px-3 text-left hover:bg-kumo-recessed/20"
            onClick={() => setXmlOpen(value => !value)}
            aria-label={xmlOpen ? '收起 XML 编辑区' : '展开 XML 编辑区'}
          >
            <span
              className="min-w-0 truncate text-xs font-semibold leading-none text-kumo-strong"
              title={currentDoc?.title || '图表信息'}
            >
              {currentDoc?.title || '图表信息'}
            </span>
            <span className="inline-flex shrink-0 items-center gap-1 text-xs text-kumo-subtle">
              XML
              {xmlOpen ? (
                <ChevronUp className="h-3.5 w-3.5 shrink-0" />
              ) : (
                <ChevronDown className="h-3.5 w-3.5 shrink-0" />
              )}
            </span>
          </Button>
          {xmlOpen && (
            <div className="h-48 min-h-0 shrink-0 overflow-hidden border-b border-kumo-line bg-kumo-base">
              <CodeEditor
                className="min-h-0"
                minHeight="0"
                height="100%"
                value={draft?.xml_content || ''}
                onChange={updateXmlFromSource}
                language="xml"
                label="图表 XML"
                variant="embedded"
                showHeader={false}
                showLanguage={false}
                lineWrapping
              />
            </div>
          )}
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex h-[37.5px] shrink-0 items-center justify-between border-b border-kumo-line px-3">
              <span className="text-xs font-semibold text-kumo-strong">版本记录</span>
              <span className="text-xs text-kumo-subtle">{versions.length}</span>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto px-3 pb-3 pt-3 scrollbar-thin">
              {!selectedId && <Empty size="sm" title="尚未选择图表" />}
              {selectedId && versions.length === 0 && (
                <Empty size="sm" title="暂无版本" />
              )}
              {versions.map(version => (
                <div
                  key={version.id}
                  className="mb-2 rounded-lg border border-kumo-line bg-kumo-recessed/20 px-3 py-2 last:mb-0"
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="min-w-0">
                      <div className="text-xs font-semibold text-kumo-strong">
                        v{version.version_no}
                        {version.summary ? `（${version.summary}）` : ''}
                      </div>
                      <div className="text-[10px] text-kumo-subtle">
                        {version.created_at?.slice(0, 16)}
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      <Button
                        size="sm"
                        variant="secondary"
                        shape="square"
                        icon={<Upload className={iconButtonIconClass} />}
                        aria-label={`导出版本 v${version.version_no}`}
                        title={`导出版本 v${version.version_no}`}
                        onClick={() => exportDocument('drawio', version.id)}
                      />
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={async () => {
                          await apiFetch(
                            `/documents/${selectedId}/versions/${version.id}/restore`,
                            { method: 'POST' }
                          );
                          await loadDocument(selectedId);
                          toast.success('已恢复到草稿');
                        }}
                      >
                        恢复
                      </Button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </LayerCard>
    </div>
  );

  const openDocument = id => {
    setSelectedId(id);
    setActiveTab('editor');
  };

  const copyLibraryPNG = async document => {
    if (libraryCopyJob) return;
    if (!window.ClipboardItem || !navigator.clipboard?.write) {
      toast.error('当前浏览器不支持复制图片');
      return;
    }
    try {
      const data = await apiFetch(`/documents/${document.id}/draft`);
      setLibraryCopyJob({
        documentId: document.id,
        title: document.title,
        xml: data.draft?.xml_content || '',
      });
    } catch (error) {
      toast.error(`复制失败：${error.message}`);
    }
  };

  const finishLibraryPNGCopy = async () => {
    const job = libraryCopyJob;
    if (!job) return;
    try {
      await new Promise(resolve => window.setTimeout(resolve, 0));
      const data = await libraryCopyFrameRef.current?.exportPNG(3);
      const blob = await exportDataToBlob(data, 'image/png');
      await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })]);
      toast.success(`${job.title} 的高清 PNG 已复制`);
    } catch (error) {
      toast.error(`复制失败：${error.message}`);
    } finally {
      setLibraryCopyJob(null);
    }
  };

  const rebuildThumbnail = async id => {
    try {
      await generateThumbnail(id);
      await loadDocuments();
      if (selectedId === id) await loadDocument(id);
      toast.success('预览已生成');
    } catch (error) {
      toast.error(`生成预览失败：${error.message}`);
    }
  };

  const rebuildAllThumbnails = async () => {
    if (!documents.length) {
      toast.warning('没有可重建的图表');
      return;
    }
    let successCount = 0;
    let failedCount = 0;
    for (const document of documents) {
      try {
        await generateThumbnail(document.id);
        successCount += 1;
      } catch (error) {
        failedCount += 1;
      }
    }
    await loadDocuments();
    if (selectedId) await loadDocument(selectedId);
    if (failedCount > 0) {
      toast.warning(`已重建 ${successCount} 个预览，${failedCount} 个失败`);
      return;
    }
    toast.success(`已重建 ${successCount} 个预览`);
  };

  const libraryView = (
    <DrawioLibraryView
      documents={documents}
      loading={loading}
      search={search}
      onCreate={createDocument}
      onImport={() => fileRef.current?.click()}
      onOpen={openDocument}
      onCopyPNG={copyLibraryPNG}
      onDelete={deleteDocument}
      onRebuildThumbnail={rebuildThumbnail}
      copyingDocumentId={libraryCopyJob?.documentId}
    />
  );

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
  const settingsView = (
    <DrawioSettingsView
      settings={settings}
      onChange={patch => setSettings(current => ({ ...current, ...patch }))}
      onSave={saveSettings}
      onRebuildAll={rebuildAllThumbnails}
      saving={settingsSaving}
    />
  );

  const editorToolbar = (
    <>
      {saveState === 'saving' && <Badge variant="warning">保存中</Badge>}
      {saveState === 'saved' && <Badge variant="success">已保存</Badge>}
      {dirty && saveState !== 'saving' && <Badge variant="warning">未保存</Badge>}
      <Button
        size="sm"
        variant="primary"
        icon={<Plus className={iconButtonIconClass} />}
        onClick={createDocument}
      >
        新建
      </Button>
      <div className="flex items-center gap-1">
        <Toolbar size="sm" aria-label="导入导出图表" className="shrink-0">
          <Toolbar.Button
            aria-label="导入图表"
            title="导入图表"
            onClick={() => fileRef.current?.click()}
            icon={<Download className="h-3.5 w-3.5" />}
          >
            <span className="hidden cq-sm:inline">导入</span>
          </Toolbar.Button>
          <Toolbar.Button
            aria-label="导出图表"
            title="导出图表"
            onClick={() => exportDocument(settings?.default_export_format || 'drawio')}
            icon={<Upload className="h-3.5 w-3.5" />}
          >
            <span className="hidden cq-sm:inline">导出</span>
          </Toolbar.Button>
        </Toolbar>
      </div>
      <Popover>
        <Popover.Trigger
          render={
            <Button
              size="sm"
              variant="secondary"
              icon={<Copy className={iconButtonIconClass} />}
              aria-label="复制图表"
            >
              复制
            </Button>
          }
        />
        <Popover.Content
          side="bottom"
          align="end"
          className="w-[min(22rem,calc(100vw-2rem))] p-3"
        >
          <div className="mb-2 flex items-center justify-between gap-2">
            <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
              复制图表
            </Popover.Title>
          </div>
          <div className="grid gap-2">
            <button
              type="button"
              className="flex min-w-0 items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2 text-left hover:border-brand/35 hover:bg-kumo-recessed/40"
              onClick={() => copyCanvas('png')}
            >
              <div className="min-w-0 flex-1">
                <div className="truncate text-xs font-medium text-kumo-strong">高清 PNG</div>
                <div className="mt-0.5 text-[11px] text-kumo-subtle">复制清晰位图到剪贴板</div>
              </div>
              <Copy className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />
            </button>
            <button
              type="button"
              className="flex min-w-0 items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2 text-left hover:border-brand/35 hover:bg-kumo-recessed/40"
              onClick={() => copyCanvas('svg')}
            >
              <div className="min-w-0 flex-1">
                <div className="truncate text-xs font-medium text-kumo-strong">SVG 原图</div>
                <div className="mt-0.5 text-[11px] text-kumo-subtle">复制矢量源码到剪贴板</div>
              </div>
              <Copy className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />
            </button>
          </div>
        </Popover.Content>
      </Popover>
      <Button
        size="sm"
        variant={dirty ? 'primary' : 'secondary'}
        onClick={() => saveDraft()}
        disabled={!dirty}
      >
        保存
      </Button>
      <Button
        size="sm"
        variant="secondary"
        icon={<Save className={iconButtonIconClass} />}
        onClick={saveVersion}
        disabled={!selectedId}
      >
        保存版本
      </Button>
    </>
  );

  const libraryToolbar = (
    <>
      <ResponsiveSearchInput
        value={search}
        onChange={event => setSearch(event.target.value)}
        placeholder="搜索图表"
        ariaLabel="搜索图表"
        className="w-56 max-w-full"
      />
      <Button
        size="sm"
        variant="secondary"
        shape="square"
        icon={<Download className={iconButtonIconClass} />}
        aria-label="导入图表"
        title="导入图表"
        onClick={() => fileRef.current?.click()}
      />
      <Button
        size="sm"
variant="primary"
        icon={<Plus className={iconButtonIconClass} />}
        onClick={createDocument}
      >
        新建图表
      </Button>
    </>
  );

  const renderToolbar = () =>
    activeTab === 'editor' ? editorToolbar : activeTab === 'library' ? libraryToolbar : null;

  return (
    <PageStack viewport className="h-full min-h-0 min-w-0 flex-col">
      <Input
        ref={fileRef}
        type="file"
        accept=".drawio,.xml"
        className="hidden"
        aria-label="选择要导入的图表文件"
        onChange={event => {
          importDocument(event.target.files?.[0]);
          event.target.value = '';
        }}
      />
      <div
        id="drawioTabRow"
        className="flex min-h-(--app-header-height) shrink-0 items-center justify-between gap-2 border-b border-kumo-line bg-[var(--app-main-surface)] px-[var(--app-tab-gutter-x)] -mx-[var(--app-canvas-gutter-x)] -mt-[var(--app-canvas-gutter-top)] [&>*]:min-w-0"
      >
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={TABS}
        />
        {activeTab !== 'settings' && (
          <div className="ml-auto flex shrink-0 flex-wrap items-center justify-end gap-2">
            {activeTab === 'editor' ? editorToolbar : libraryToolbar}
          </div>
        )}
      </div>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {activeTab === 'editor' && editorView}
        {activeTab === 'library' && libraryView}
        {activeTab === 'settings' && settingsView}
      </div>
      {libraryCopyJob && (
        <div
          className="pointer-events-none fixed left-[-10000px] top-0 h-[800px] w-[1200px] overflow-hidden"
          aria-hidden="true"
        >
          <DrawioFrame
            ref={libraryCopyFrameRef}
            key={`library-copy-${libraryCopyJob.documentId}`}
            xml={libraryCopyJob.xml}
            theme={theme}
            readOnly
            onReady={finishLibraryPNGCopy}
          />
        </div>
      )}
      {thumbnailRenderTask && (
        <div
          className="pointer-events-none fixed left-[-10000px] top-0 h-[800px] w-[1200px] overflow-hidden"
          aria-hidden="true"
        >
          <DrawioFrame
            ref={thumbnailRenderFrameRef}
            key={thumbnailRenderTask.key}
            xml={thumbnailRenderTask.xml}
            theme={theme}
            readOnly
            onReady={finishThumbnailRender}
          />
        </div>
      )}
      <Dialog.Root open={conflictOpen} onOpenChange={setConflictOpen} role="alertdialog">
        <Dialog className="p-6">
          <div className="flex items-center gap-3">
            <AlertTriangle className="h-5 w-5 text-kumo-warning" />
            <Dialog.Title>草稿冲突</Dialog.Title>
          </div>
          <Dialog.Description className="mt-3 text-kumo-subtle">
            另一会话已保存更新版本。
          </Dialog.Description>
          <div className="mt-6 flex justify-end gap-2">
            <Button
              size="sm"
              variant="secondary"
              onClick={() => navigator.clipboard.writeText(draft?.xml_content || '')}
            >
              复制本地 XML
            </Button>
            <Button
              size="sm"
              variant="primary"
              onClick={() => {
                setConflictOpen(false);
                loadDocument(selectedId);
              }}
            >
              加载最新草稿
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </PageStack>
  );
}
