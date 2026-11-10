import React, { useEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Badge } from '@cloudflare/kumo';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { formatDateTime, formatFileSize } from '../../modules/utils.js';
import { buildSftpDownloadUrl, chmodSftpPath, createSftpDirectory, deleteSftpPath, listSftpDirectory, readSftpFile, renameSftpPath, uploadSftpFile, writeSftpFile } from '../../modules/server-sftp.js';
import { ContextMenu } from '@cloudflare/kumo/primitives/context-menu';
import { ChevronRight, Copy, Download, Edit, Eye, FileText, Folder, FolderOpen, Key, RefreshCw, Save, Trash, Upload } from '../Icons.jsx';
import CodeEditor from '../ui/CodeEditor.jsx';

const contextMenuItemClassName = 'relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay';
const contextMenuDangerItemClassName = 'relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm text-kumo-danger outline-hidden select-none focus:text-kumo-danger focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-danger/5 data-highlighted:text-kumo-danger';

function sortSftpEntries(entries) {
  return [...entries].sort((a, b) => {
    if (Boolean(a.isDirectory) !== Boolean(b.isDirectory)) return a.isDirectory ? -1 : 1;
    return String(a.name || '').localeCompare(String(b.name || ''), 'zh-Hans-CN', {
      numeric: true,
      sensitivity: 'base',
    });
  });
}

const parentOfPath = value => {
  const parts = String(value || '').replace(/\\/g, '/').split('/');
  parts.pop();
  const base = parts.join('/');
  return base || '/';
};

const sftpTreeCache = new Map();

function FileContextMenu({ file, children, onOpen, onDownload, onCopyPath, onRename, onChmod, onDelete }) {
  return (
    <ContextMenu.Root>
      <ContextMenu.Trigger render={children} />
      <ContextMenu.Portal>
        <ContextMenu.Positioner sideOffset={6}>
          <ContextMenu.Popup className="z-50 min-w-44 overflow-hidden rounded-lg border border-kumo-line bg-kumo-control p-1.5 text-kumo-default outline-none data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95">
            <ContextMenu.Item className={contextMenuItemClassName} onClick={onOpen}>
              <Eye className="h-4 w-4" />
              <span>{file.isDirectory ? '打开目录' : '打开文件'}</span>
            </ContextMenu.Item>
            <ContextMenu.Item className={contextMenuItemClassName} disabled={file.isDirectory || !onDownload} onClick={onDownload}>
              <Download className="h-4 w-4" />
              <span>下载</span>
            </ContextMenu.Item>
            <ContextMenu.Item className={contextMenuItemClassName} onClick={onCopyPath}>
              <Copy className="h-4 w-4" />
              <span>复制路径</span>
            </ContextMenu.Item>
            <ContextMenu.Separator className="-mx-1.5 my-1 h-0.5 bg-kumo-hairline" />
            <ContextMenu.Item className={contextMenuItemClassName} onClick={onRename}>
              <Edit className="h-4 w-4" />
              <span>重命名</span>
            </ContextMenu.Item>
            <ContextMenu.Item className={contextMenuItemClassName} onClick={onChmod}>
              <Key className="h-4 w-4" />
              <span>权限</span>
            </ContextMenu.Item>
            <ContextMenu.Separator className="-mx-1.5 my-1 h-0.5 bg-kumo-hairline" />
            <ContextMenu.Item className={contextMenuDangerItemClassName} onClick={onDelete}>
              <Trash className="h-4 w-4" />
              <span>删除</span>
            </ContextMenu.Item>
          </ContextMenu.Popup>
        </ContextMenu.Positioner>
      </ContextMenu.Portal>
    </ContextMenu.Root>
  );
}

export default function SftpPanel({ serverId, serverName, initialPath = '.', onClose, onPathChange }) {
  const uploadInputRef = useRef(null);
  const [rootPath, setRootPath] = useState(initialPath || '.');
  const [tree, setTree] = useState({});
  const [expanded, setExpanded] = useState(() => new Set());
  const [selectedPath, setSelectedPath] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [uploading, setUploading] = useState(false);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editFile, setEditFile] = useState(null);
  const [saving, setSaving] = useState(false);
  const [renameFile, setRenameFile] = useState(null);
  const [renameValue, setRenameValue] = useState('');
  const [mkdirOpen, setMkdirOpen] = useState(false);
  const [mkdirName, setMkdirName] = useState('');
  const [chmodFile, setChmodFile] = useState(null);
  const [chmodValue, setChmodValue] = useState('644');
  const hasUnsavedEdit = Boolean(editFile && editFile.content !== editFile.originalContent);

  // serverIdRef 镜像当前主机：异步目录请求返回时校验发起主机是否仍是当前
  // 主机，防止切换服务器后旧主机的目录结果写入新主机的树并被持久化进缓存。
  const serverIdRef = useRef(serverId);
  useEffect(() => {
    serverIdRef.current = serverId;
  }, [serverId]);

  const fetchDirectory = async dirPath => {
    const data = await listSftpDirectory(serverId, dirPath || '.');
    return { path: data.path || dirPath || '.', items: sortSftpEntries(data.data || []) };
  };

  useEffect(() => {
    setError('');
    if (!serverId) {
      setLoading(false);
      setTree({});
      setExpanded(new Set());
      setSelectedPath('');
      return undefined;
    }
    const cached = sftpTreeCache.get(serverId);
    if (cached) {
      setRootPath(cached.rootPath);
      setTree(cached.tree);
      setExpanded(new Set(cached.expanded));
      setSelectedPath(cached.selectedPath || '');
      setLoading(false);
      onPathChange?.(serverId, cached.rootPath);
      return undefined;
    }
    let cancelled = false;
    setTree({});
    setExpanded(new Set());
    setSelectedPath('');
    setLoading(true);
    (async () => {
      try {
        const { path, items } = await fetchDirectory(initialPath || '.');
        if (cancelled) return;
        setRootPath(path);
        setTree({ [path]: { status: 'loaded', items } });
        setExpanded(new Set([path]));
        onPathChange?.(serverId, path);
      } catch (err) {
        if (!cancelled) setError(err.message || '加载 SFTP 目录失败');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [serverId]);

  useEffect(() => {
    if (!serverId || Object.keys(tree).length === 0) return;
    sftpTreeCache.set(serverId, { rootPath, tree, expanded: [...expanded], selectedPath });
  }, [serverId, rootPath, tree, expanded, selectedPath]);

  const findTreeEntry = targetPath => {
    for (const dir of Object.values(tree)) {
      const hit = (dir?.items || []).find(item => item.path === targetPath);
      if (hit) return hit;
    }
    return null;
  };

  const resolveTargetDir = () => {
    if (!selectedPath) return rootPath;
    const entry = findTreeEntry(selectedPath);
    if (entry?.isDirectory) return entry.path;
    return parentOfPath(selectedPath);
  };

  const patchTreeDir = (dirPath, patch) => {
    setTree(prev => ({ ...prev, [dirPath]: { ...(prev[dirPath] || { items: [] }), ...patch } }));
  };

  const loadTreeDir = async (dirPath, { silent = false } = {}) => {
    if (!serverId) return null;
    const requestedServerId = serverId;
    patchTreeDir(dirPath, { status: 'loading', error: '' });
    try {
      const { path, items } = await fetchDirectory(dirPath);
      // 请求在途期间切换了服务器：丢弃结果，避免跨主机目录数据污染树与缓存。
      if (serverIdRef.current !== requestedServerId) return null;
      setTree(prev => {
        const next = { ...prev };
        delete next[dirPath];
        next[path] = { status: 'loaded', items };
        return next;
      });
      return path;
    } catch (err) {
      if (serverIdRef.current !== requestedServerId) return null;
      patchTreeDir(dirPath, { status: 'error', error: err.message || '加载失败' });
      if (!silent) toast.error(err.message || '加载 SFTP 目录失败');
      return null;
    }
  };

  const toggleTreeDir = async entry => {
    setSelectedPath(entry.path);
    const wasExpanded = expanded.has(entry.path);
    setExpanded(prev => {
      const next = new Set(prev);
      if (wasExpanded) next.delete(entry.path);
      else next.add(entry.path);
      return next;
    });
    if (!wasExpanded && !tree[entry.path]) {
      await loadTreeDir(entry.path);
    }
  };

  const refreshTree = async () => {
    const dirs = [rootPath, ...[...expanded].filter(p => p !== rootPath)];
    await Promise.allSettled(dirs.map(p => loadTreeDir(p, { silent: true })));
  };

  const pruneTreeFrom = dirPath => {
    const prefix = String(dirPath).replace(/\/$/, '') + '/';
    setTree(prev =>
      Object.fromEntries(Object.entries(prev).filter(([p]) => p !== dirPath && !p.startsWith(prefix)))
    );
    setExpanded(prev => new Set([...prev].filter(p => p !== dirPath && !p.startsWith(prefix))));
  };

  const openFile = async entry => {
    if (entry.isDirectory) {
      toggleTreeDir(entry);
      return;
    }
    setSelectedPath(entry.path);
    try {
      const data = await readSftpFile(serverId, entry.path);
      setEditFile({ path: entry.path, name: entry.name, content: data.data || '', originalContent: data.data || '' });
      setEditorOpen(true);
    } catch (err) {
      toast.error(err.message || '读取文件失败');
    }
  };

  const saveFile = async () => {
    if (!editFile) return;
    setSaving(true);
    try {
      await writeSftpFile(serverId, editFile.path, editFile.content);
      toast.success('文件已保存');
      setEditorOpen(false);
      setEditFile(null);
      loadTreeDir(parentOfPath(editFile.path), { silent: true });
    } catch (err) {
      toast.error(err.message || '保存文件失败');
    } finally {
      setSaving(false);
    }
  };

  const requestCloseEditor = async () => {
    if (hasUnsavedEdit) {
      const ok = await dialog.confirm({
        title: '放弃未保存修改',
        message: `文件 ${editFile?.name || ''} 有未保存内容，确定关闭？`,
        confirmText: '放弃修改',
        cancelText: '继续编辑',
        variant: 'danger',
      });
      if (!ok) return;
    }
    setEditorOpen(false);
    setEditFile(null);
  };

  const uploadFiles = async event => {
    const selected = Array.from(event.target.files || []);
    if (selected.length === 0) return;
    const targetDir = resolveTargetDir();
    setUploading(true);
    let ok = 0;
    let failed = 0;
    for (const file of selected) {
      try {
        await uploadSftpFile(serverId, targetDir, file, file.webkitRelativePath || '');
        ok += 1;
      } catch {
        failed += 1;
      }
    }
    event.target.value = '';
    setUploading(false);
    toast.success(`上传完成：成功 ${ok}，失败 ${failed}`);
    setExpanded(prev => new Set(prev).add(targetDir));
    loadTreeDir(targetDir, { silent: true });
  };

  const createDirectory = async () => {
    const name = mkdirName.trim();
    if (!name) return;
    const targetDir = resolveTargetDir();
    const targetPath = `${targetDir.replace(/\/$/, '')}/${name}`;
    try {
      await createSftpDirectory(serverId, targetPath);
      toast.success('目录已创建');
      setMkdirOpen(false);
      setMkdirName('');
      setExpanded(prev => new Set(prev).add(targetDir));
      loadTreeDir(targetDir, { silent: true });
    } catch (err) {
      toast.error(err.message || '创建目录失败');
    }
  };

  const renamePath = async () => {
    if (!renameFile || !renameValue.trim()) return;
    const parentDir = parentOfPath(renameFile.path);
    const targetPath = `${parentDir.replace(/\/$/, '')}/${renameValue.trim()}`;
    try {
      await renameSftpPath(serverId, renameFile.path, targetPath);
      toast.success('重命名成功');
      setRenameFile(null);
      if (renameFile.isDirectory) pruneTreeFrom(renameFile.path);
      loadTreeDir(parentDir, { silent: true });
    } catch (err) {
      toast.error(err.message || '重命名失败');
    }
  };

  const deletePath = async file => {
    const ok = await dialog.deleteResource({
      resourceType: file.isDirectory ? '远程目录' : '远程文件',
      resourceName: file.name,
    });
    if (!ok) return;
    const parentDir = parentOfPath(file.path);
    try {
      await deleteSftpPath(serverId, file.path, file.isDirectory, file.isDirectory);
      toast.success(file.isDirectory ? '目录已删除' : '文件已删除');
      if (file.isDirectory) pruneTreeFrom(file.path);
      if (selectedPath === file.path) setSelectedPath('');
      loadTreeDir(parentDir, { silent: true });
    } catch (err) {
      toast.error(err.message || '删除失败');
    }
  };

  const chmodPath = async () => {
    if (!chmodFile || !chmodValue.trim()) return;
    try {
      await chmodSftpPath(serverId, chmodFile.path, chmodValue.trim());
      toast.success('权限已更新');
      setChmodFile(null);
      loadTreeDir(parentOfPath(chmodFile.path), { silent: true });
    } catch (err) {
      toast.error(err.message || '权限修改失败');
    }
  };

  const copyPath = async entry => {
    try {
      await navigator.clipboard.writeText(entry.path);
      toast.success('路径已复制');
    } catch {
      toast.error('复制路径失败');
    }
  };

  const renderRows = (dirPath, depth) => {
    const dir = tree[dirPath];
    if (!dir) return null;
    if (dir.status === 'loading') {
      return (
        <div style={{ paddingLeft: 12 + depth * 16 }} className="py-1 text-[10px] text-kumo-subtle">
          加载中…
        </div>
      );
    }
    if (dir.status === 'error') {
      return (
        <div style={{ paddingLeft: 12 + depth * 16 }} className="py-1 text-[10px] text-kumo-danger">
          {dir.error}
        </div>
      );
    }
    if (dir.items.length === 0) {
      return (
        <div style={{ paddingLeft: 12 + depth * 16 }} className="py-1 text-[10px] text-kumo-subtle">
          （空目录）
        </div>
      );
    }
    return (
      <React.Fragment>
        {dir.items.map(entry => {
          const isDir = Boolean(entry.isDirectory);
          const isExpanded = isDir && expanded.has(entry.path);
          const isSelected = selectedPath === entry.path;
          const downloadUrl = !isDir ? buildSftpDownloadUrl(serverId, entry.path) : null;
          const startRename = () => {
            setRenameFile(entry);
            setRenameValue(entry.name);
          };
          const startChmod = () => {
            setChmodFile(entry);
            setChmodValue(String(entry.mode ? (entry.mode & 0o777).toString(8) : '644'));
          };
          return (
            <React.Fragment key={entry.path}>
              <FileContextMenu
                file={entry}
                onOpen={() => openFile(entry)}
                onDownload={downloadUrl ? () => window.open(downloadUrl, '_blank', 'noopener') : null}
                onCopyPath={() => copyPath(entry)}
                onRename={startRename}
                onChmod={startChmod}
                onDelete={() => deletePath(entry)}
              >
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => openFile(entry)}
                  onContextMenu={() => setSelectedPath(entry.path)}
                  title={`${entry.path}\n修改时间: ${entry.mtime ? formatDateTime(entry.mtime) : '-'}\n权限: ${entry.permissions || '-'}`}
                  className={`mb-px flex !h-auto !min-h-0 w-full min-w-0 items-center justify-start gap-1.5 rounded-md px-0 py-[3px] pr-2 text-left font-normal hover:bg-kumo-fill focus-visible:bg-kumo-fill ${isSelected ? 'bg-kumo-fill' : 'bg-transparent'}`}
                  style={{ paddingLeft: 8 + depth * 16 }}
                >
                  {isDir ? (
                    <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-kumo-subtle transition-transform duration-quick ${isExpanded ? 'rotate-90' : ''}`} />
                  ) : (
                    <span className="h-3.5 w-3.5 shrink-0" />
                  )}
                  {isDir ? (
                    <Folder className="h-3.5 w-3.5 shrink-0 text-brand" />
                  ) : (
                    <FileText className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />
                  )}
                  <span className="min-w-0 truncate text-xs font-medium text-kumo-strong">{entry.name}</span>
                  {!isDir && (
                    <span className="ml-auto shrink-0 pl-2 font-mono text-[10px] tabular-nums text-kumo-subtle">
                      {formatFileSize(entry.size)}
                    </span>
                  )}
                </Button>
              </FileContextMenu>
              {isDir && isExpanded && renderRows(entry.path, depth + 1)}
            </React.Fragment>
          );
        })}
      </React.Fragment>
    );
  };

  return (
    <>
      <div className="flex h-full min-h-0 w-full flex-col overflow-hidden bg-kumo-base">
        <div className="flex items-center justify-between gap-2 border-b border-kumo-line px-2.5 py-2">
          <div className="flex min-w-0 items-center gap-2">
            <FolderOpen className="h-4 w-4 shrink-0 text-brand" />
            <span className="truncate text-xs font-semibold text-kumo-strong">文件系统</span>
            <Badge variant="neutral" className="max-w-32 shrink-0 truncate text-[10px] leading-4">
              {serverName || serverId || '-'}
            </Badge>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <Button shape="square" size="sm" variant="secondary" icon={<Upload className="h-3.5 w-3.5" />} aria-label="上传" title="上传" onClick={() => uploadInputRef.current?.click()} disabled={!serverId || uploading} />
            <Button shape="square" size="sm" variant="secondary" icon={<Folder className="h-3.5 w-3.5" />} aria-label="新建目录" title="新建目录" onClick={() => setMkdirOpen(true)} />
            <Button shape="square" size="sm" variant="secondary" icon={<RefreshCw className="h-3.5 w-3.5" />} aria-label="刷新" title="刷新" onClick={() => refreshTree()} loading={loading || uploading} />
          </div>
        </div>

        <div className="min-h-0 flex-1 p-1">
          {loading ? (
            <div className="py-10 text-center text-xs text-kumo-subtle">加载中...</div>
          ) : error ? (
            <div className="m-3 rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-xs text-kumo-danger">{error}</div>
          ) : tree[rootPath] ? (
            <div className="min-h-0 max-h-full overflow-y-auto overflow-x-hidden scrollbar-thin">
              {renderRows(rootPath, 0)}
            </div>
          ) : null}
        </div>

        <Input ref={uploadInputRef} size="sm" aria-label="上传 SFTP 文件" type="file" className="hidden" onChange={uploadFiles} multiple />
      </div>

      <Dialog.Root open={editorOpen && Boolean(editFile)} onOpenChange={(open) => (open ? setEditorOpen(true) : requestCloseEditor())}>
        <Dialog size="xl" className="flex h-[min(72dvh,720px)] w-[min(920px,calc(100vw-2rem))] max-h-[calc(100dvh-1rem)] max-w-none flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
            <Dialog.Title className="min-w-0 truncate text-sm font-semibold text-kumo-strong">编辑 {editFile?.name}</Dialog.Title>
            <Dialog.Close />
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-hidden p-4">
            <div className="shrink-0 truncate font-mono text-[10px] text-kumo-subtle">{editFile?.path}</div>
            <CodeEditor
              label="SFTP 文件内容"
              fileName={editFile?.name || editFile?.path || ''}
              value={editFile?.content || ''}
              onChange={content => setEditFile(prev => ({ ...prev, content }))}
              className="min-h-0 flex-1"
              minHeight="0"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
            <Button size="sm" variant="secondary" onClick={requestCloseEditor}>取消</Button>
            <Button size="sm" variant="primary" icon={<Save className="h-3.5 w-3.5" />} loading={saving} onClick={saveFile}>保存</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <LayerDialog.Root open={mkdirOpen} onOpenChange={setMkdirOpen}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>新建目录</LayerDialog.Title>
          <LayerDialog.Body>
            <Input size="sm" label="目录名" value={mkdirName} onChange={event => setMkdirName(event.target.value)} />
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Action type="button" onClick={createDirectory}>创建</LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      <LayerDialog.Root open={Boolean(renameFile)} onOpenChange={open => !open && setRenameFile(null)}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>重命名</LayerDialog.Title>
          <LayerDialog.Body>
            <Input size="sm" label="新名称" value={renameValue} onChange={event => setRenameValue(event.target.value)} />
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Action type="button" onClick={renamePath}>保存</LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      <LayerDialog.Root open={Boolean(chmodFile)} onOpenChange={open => !open && setChmodFile(null)}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>修改权限</LayerDialog.Title>
          <LayerDialog.Body>
            <Input size="sm" label="权限值" value={chmodValue} onChange={event => setChmodValue(event.target.value)} className="font-mono" />
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Action type="button" onClick={chmodPath}>保存</LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </>
  );
}
