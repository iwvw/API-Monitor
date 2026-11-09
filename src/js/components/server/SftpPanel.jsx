import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { formatDateTime, formatFileSize } from '../../modules/utils.js';
import { buildSftpDownloadUrl, chmodSftpPath, createSftpDirectory, deleteSftpPath, listSftpDirectory, readSftpFile, renameSftpPath, uploadSftpFile, writeSftpFile } from '../../modules/server-sftp.js';
import { ContextMenu } from '@cloudflare/kumo/primitives/context-menu';
import { ArrowLeft, Copy, Download, Edit, Eye, FileText, Folder, FolderOpen, Key, RefreshCw, Save, Trash, Upload, X } from '../Icons.jsx';
import CodeEditor from '../ui/CodeEditor.jsx';

const contextMenuItemClassName = 'relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay';
const contextMenuDangerItemClassName = 'relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm text-kumo-danger outline-hidden select-none focus:text-kumo-danger focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-danger/5 data-highlighted:text-kumo-danger';

function buildBreadcrumbs(remotePath) {
  const normalized = String(remotePath || '/').replace(/\\/g, '/');
  const parts = normalized.split('/').filter(Boolean);
  if (/^[A-Za-z]:$/.test(parts[0])) {
    const drive = parts.shift();
    const crumbs = [{ name: drive, path: drive }];
    let current = drive;
    parts.forEach(part => {
      current += `/${part}`;
      crumbs.push({ name: part, path: current });
    });
    return crumbs;
  }

  const crumbs = [{ name: '/', path: '/' }];
  let current = '';
  parts.forEach(part => {
    current += `/${part}`;
    crumbs.push({ name: part, path: current });
  });
  return crumbs;
}

/**
 * SFTP 目录项排序：目录恒在文件之前，同组内按名称自然序（数字按数值比较）。
 * 远端返回顺序不可靠（ls 顺序受文件系统影响），必须在前端统一。
 */
function sortSftpEntries(entries) {
  return [...entries].sort((a, b) => {
    if (Boolean(a.isDirectory) !== Boolean(b.isDirectory)) return a.isDirectory ? -1 : 1;
    return String(a.name || '').localeCompare(String(b.name || ''), 'zh-Hans-CN', {
      numeric: true,
      sensitivity: 'base',
    });
  });
}

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
            <ContextMenu.Separator className="mx-1 my-1 h-px bg-kumo-line" />
            <ContextMenu.Item className={contextMenuItemClassName} onClick={onRename}>
              <Edit className="h-4 w-4" />
              <span>重命名</span>
            </ContextMenu.Item>
            <ContextMenu.Item className={contextMenuItemClassName} onClick={onChmod}>
              <Key className="h-4 w-4" />
              <span>权限</span>
            </ContextMenu.Item>
            <ContextMenu.Separator className="mx-1 my-1 h-px bg-kumo-line" />
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
  const [files, setFiles] = useState([]);
  const [currentPath, setCurrentPath] = useState(initialPath || '.');
  const [pathInput, setPathInput] = useState(initialPath || '.');
  const [loading, setLoading] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');
  const [editorOpen, setEditorOpen] = useState(false);
  const [editFile, setEditFile] = useState(null);
  const [saving, setSaving] = useState(false);
  const [renameFile, setRenameFile] = useState(null);
  const [renameValue, setRenameValue] = useState('');
  const [mkdirOpen, setMkdirOpen] = useState(false);
  const [mkdirName, setMkdirName] = useState('');
  const [chmodFile, setChmodFile] = useState(null);
  const [chmodValue, setChmodValue] = useState('644');
  const breadcrumbs = useMemo(() => buildBreadcrumbs(currentPath), [currentPath]);
  const hasUnsavedEdit = Boolean(editFile && editFile.content !== editFile.originalContent);

  const loadDirectory = async (path = currentPath) => {
    if (!serverId) return;
    setLoading(true);
    setError('');
    try {
      const data = await listSftpDirectory(serverId, path || '.');
      const nextPath = data.path || path || '.';
      setFiles(sortSftpEntries(data.data || []));
      setCurrentPath(nextPath);
      setPathInput(nextPath);
      onPathChange?.(serverId, nextPath);
    } catch (err) {
      setError(err.message || '加载 SFTP 目录失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    setCurrentPath(initialPath || '.');
    setPathInput(initialPath || '.');
    if (serverId) loadDirectory(initialPath || '.');
     
  }, [serverId]);

  const openFile = async (file) => {
    if (file.isDirectory) {
      loadDirectory(file.path);
      return;
    }
    try {
      const data = await readSftpFile(serverId, file.path);
      setEditFile({ path: file.path, name: file.name, content: data.data || '', originalContent: data.data || '' });
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
      loadDirectory(currentPath);
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

  const uploadFiles = async (event) => {
    const selected = Array.from(event.target.files || []);
    if (selected.length === 0) return;
    setUploading(true);
    let ok = 0;
    let failed = 0;
    for (const file of selected) {
      try {
        await uploadSftpFile(serverId, currentPath, file, file.webkitRelativePath || '');
        ok += 1;
      } catch {
        failed += 1;
      }
    }
    event.target.value = '';
    setUploading(false);
    toast.success(`上传完成：成功 ${ok}，失败 ${failed}`);
    loadDirectory(currentPath);
  };

  const createDirectory = async () => {
    const name = mkdirName.trim();
    if (!name) return;
    const targetPath = `${currentPath.replace(/\/$/, '')}/${name}`;
    try {
      await createSftpDirectory(serverId, targetPath);
      toast.success('目录已创建');
      setMkdirOpen(false);
      setMkdirName('');
      loadDirectory(currentPath);
    } catch (err) {
      toast.error(err.message || '创建目录失败');
    }
  };

  const renamePath = async () => {
    if (!renameFile || !renameValue.trim()) return;
    const base = renameFile.path.split('/').slice(0, -1).join('/') || '/';
    const targetPath = `${base.replace(/\/$/, '')}/${renameValue.trim()}`;
    try {
      await renameSftpPath(serverId, renameFile.path, targetPath);
      toast.success('重命名成功');
      setRenameFile(null);
      loadDirectory(currentPath);
    } catch (err) {
      toast.error(err.message || '重命名失败');
    }
  };

  const deletePath = async (file) => {
    const ok = await dialog.deleteResource({
      resourceType: file.isDirectory ? '远程目录' : '远程文件',
      resourceName: file.name,
    });
    if (!ok) return;
    try {
      await deleteSftpPath(serverId, file.path, file.isDirectory, file.isDirectory);
      toast.success(file.isDirectory ? '目录已删除' : '文件已删除');
      loadDirectory(currentPath);
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
      loadDirectory(currentPath);
    } catch (err) {
      toast.error(err.message || '权限修改失败');
    }
  };

  const copyPath = async (file) => {
    try {
      await navigator.clipboard.writeText(file.path);
      toast.success('路径已复制');
    } catch {
      toast.error('复制路径失败');
    }
  };

  return (
    <>
      <div className="flex h-full min-h-0 w-full flex-col overflow-hidden border-t border-kumo-line bg-kumo-base">
        <div className="flex items-center justify-between gap-2 border-b border-kumo-line px-2.5 py-2">
          <div className="min-w-0">
            <div className="flex items-center gap-1.5 text-xs font-semibold text-kumo-strong">
              <FolderOpen className="h-3.5 w-3.5 shrink-0 text-brand" />
              <span className="truncate">文件系统</span>
            </div>
            <div className="truncate text-[10px] text-kumo-subtle">{serverName || serverId || '-'}</div>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <Button shape="square" size="sm" variant="secondary" icon={<Upload className="h-3.5 w-3.5" />} aria-label="上传" title="上传" onClick={() => uploadInputRef.current?.click()} disabled={!serverId || uploading} />
            <Button shape="square" size="sm" variant="secondary" icon={<Folder className="h-3.5 w-3.5" />} aria-label="新建目录" title="新建目录" onClick={() => setMkdirOpen(true)} />
            <Button shape="square" size="sm" variant="secondary" icon={<RefreshCw className="h-3.5 w-3.5" />} aria-label="刷新" title="刷新" onClick={() => loadDirectory(currentPath)} loading={loading || uploading} />
            <Button shape="square" size="sm" variant="ghost" icon={<X className="h-3 w-3" />} aria-label="关闭 SFTP" title="关闭 SFTP" onClick={onClose} />
          </div>
        </div>

        <div className="flex min-h-0 flex-1 flex-col gap-2 p-2.5">
          <form
            className="flex min-w-0 items-center gap-1.5"
            onSubmit={event => {
              event.preventDefault();
              loadDirectory(pathInput);
            }}
          >
            <Button shape="square" size="sm" variant="secondary" icon={<ArrowLeft className="h-3.5 w-3.5" />} aria-label="返回上级目录" onClick={() => loadDirectory(breadcrumbs.at(-2)?.path || breadcrumbs[0]?.path || currentPath)} />
            <Input size="sm" aria-label="SFTP 路径" value={pathInput} onChange={event => setPathInput(event.target.value)} className="min-w-0 flex-1 font-mono text-xs" />
            <Button size="sm" variant="primary" type="submit">跳转</Button>
          </form>

          <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-kumo-line">
            <div className="flex items-center gap-1 overflow-x-auto border-b border-kumo-line px-2.5 py-1.5 text-[10px] text-kumo-subtle scrollbar-none">
              {breadcrumbs.map((crumb, idx) => (
                <React.Fragment key={`${crumb.path}-${idx}`}>
                  <Button type="button" size="xs" variant="ghost" className="h-auto min-w-0 truncate px-0 py-0 font-semibold text-kumo-default hover:text-kumo-strong" onClick={() => loadDirectory(crumb.path)}>
                    {crumb.name}
                  </Button>
                  {idx < breadcrumbs.length - 1 ? <span>/</span> : null}
                </React.Fragment>
              ))}
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden scrollbar-thin">
              {loading ? (
                <div className="py-10 text-center text-xs text-kumo-subtle">加载中...</div>
              ) : error ? (
                <div className="m-3 rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-xs text-kumo-danger">{error}</div>
              ) : files.length === 0 ? (
                <div className="py-10 text-center text-xs text-kumo-subtle">目录为空</div>
              ) : (
                <div className="flex flex-col py-0.5">
                  {files.map(file => {
                    const downloadUrl = !file.isDirectory ? buildSftpDownloadUrl(serverId, file.path) : null;
                    const startRename = () => { setRenameFile(file); setRenameValue(file.name); };
                    const startChmod = () => { setChmodFile(file); setChmodValue(String(file.mode ? (file.mode & 0o777).toString(8) : '644')); };
                    return (
                      <FileContextMenu
                        key={file.path}
                        file={file}
                        onOpen={() => openFile(file)}
                        onDownload={downloadUrl ? () => window.open(downloadUrl, '_blank', 'noopener') : null}
                        onCopyPath={() => copyPath(file)}
                        onRename={startRename}
                        onChmod={startChmod}
                        onDelete={() => deletePath(file)}
                      >
                        <Button
                          type="button"
                          variant="ghost"
                          onClick={() => openFile(file)}
                          title={`${file.path}\n修改时间: ${file.mtime ? formatDateTime(file.mtime) : '-'}\n权限: ${file.permissions || '-'}`}
                          className="flex !h-auto !min-h-0 w-full min-w-0 items-center justify-start gap-1.5 rounded-none px-2.5 py-[3px] text-left font-normal hover:bg-kumo-fill focus-visible:bg-kumo-fill"
                        >
                          {file.isDirectory ? (
                            <Folder className="h-3.5 w-3.5 shrink-0 text-brand" />
                          ) : (
                            <FileText className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />
                          )}
                          <span className="min-w-0 truncate text-xs font-medium text-kumo-strong">{file.name}</span>
                          {!file.isDirectory && (
                            <span className="ml-auto shrink-0 pl-2 font-mono text-[10px] tabular-nums text-kumo-subtle">
                              {formatFileSize(file.size)}
                            </span>
                          )}
                        </Button>
                      </FileContextMenu>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
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
