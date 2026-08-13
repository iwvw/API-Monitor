import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Table } from '@cloudflare/kumo/components/table';
import { DropdownMenu } from '@cloudflare/kumo';
import { ContextMenu } from '@cloudflare/kumo/primitives/context-menu';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { formatDateTime, formatFileSize } from '../../modules/utils.js';
import {
  buildSftpDownloadUrl,
  chmodSftpPath,
  createSftpDirectory,
  deleteSftpPath,
  listSftpDirectory,
  readSftpFile,
  renameSftpPath,
  uploadSftpFile,
  writeSftpFile,
} from '../../modules/server-sftp.js';
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

function FileActionMenu({ file, onOpen, onDownload, onRename, onChmod, onDelete }) {
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button shape="square" size="sm" variant="ghost" icon={<Eye className="h-3 w-3" />} aria-label="文件操作" title="文件操作" />
        }
      />
      <DropdownMenu.Content side="left" align="start" className="w-44">
        <DropdownMenu.Item icon={<Eye className="h-3.5 w-3.5" />} onClick={onOpen}>打开</DropdownMenu.Item>
        {!file.isDirectory && onDownload ? (
          <DropdownMenu.Item icon={<Download className="h-3.5 w-3.5" />} onClick={() => window.open(onDownload, '_blank', 'noopener')}>
            下载
          </DropdownMenu.Item>
        ) : null}
        <DropdownMenu.Item icon={<Edit className="h-3.5 w-3.5" />} onClick={onRename}>重命名</DropdownMenu.Item>
        <DropdownMenu.Item icon={<Key className="h-3.5 w-3.5" />} onClick={onChmod}>权限</DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item icon={<Trash className="h-3.5 w-3.5" />} variant="danger" onClick={onDelete}>删除</DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
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
      setFiles(data.data || []);
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
        message: `文件 ${editFile?.name || ''} 还有未保存内容，确定关闭编辑器吗？`,
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
    toast.success(`上传完成：成功 ${ok} 个，失败 ${failed} 个`);
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
        <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-3 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-sm font-semibold text-kumo-strong">
              <FolderOpen className="h-4 w-4 text-kumo-brand" />
              <span className="truncate">文件系统</span>
            </div>
            <div className="truncate text-[10px] text-kumo-subtle">{serverName || serverId || '-'}</div>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <Button size="sm" variant="secondary" icon={<Upload className="h-3.5 w-3.5" />} onClick={() => uploadInputRef.current?.click()} disabled={!serverId || uploading}>上传</Button>
            <Button size="sm" variant="secondary" icon={<Folder className="h-3.5 w-3.5" />} onClick={() => setMkdirOpen(true)}>新建</Button>
            <Button size="sm" variant="secondary" icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={() => loadDirectory(currentPath)} loading={loading || uploading}>刷新</Button>
            <Button shape="square" size="sm" variant="ghost" icon={<X className="h-3 w-3" />} aria-label="关闭 SFTP" title="关闭 SFTP" onClick={onClose} />
          </div>
        </div>

        <div className="flex min-h-0 flex-1 flex-col gap-3 p-3">
          <form
            className="flex min-w-0 items-center gap-2"
            onSubmit={event => {
              event.preventDefault();
              loadDirectory(pathInput);
            }}
          >
            <Button shape="square" size="sm" variant="secondary" icon={<ArrowLeft className="h-3.5 w-3.5" />} onClick={() => loadDirectory(breadcrumbs.at(-2)?.path || breadcrumbs[0]?.path || currentPath)} />
            <Input size="sm" aria-label="SFTP 路径" value={pathInput} onChange={event => setPathInput(event.target.value)} className="min-w-0 flex-1 font-mono" />
            <Button size="sm" variant="primary" type="submit">跳转</Button>
          </form>

          <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-kumo-line">
            <div className="flex items-center gap-1.5 border-b border-kumo-line px-3 py-2 text-[10px] text-kumo-subtle">
              {breadcrumbs.map((crumb, idx) => (
                <React.Fragment key={`${crumb.path}-${idx}`}>
                  <Button type="button" size="xs" variant="ghost" className="h-auto min-w-0 truncate px-0 py-0 font-semibold text-kumo-default hover:text-kumo-strong" onClick={() => loadDirectory(crumb.path)}>
                    {crumb.name}
                  </Button>
                  {idx < breadcrumbs.length - 1 ? <span>/</span> : null}
                </React.Fragment>
              ))}
            </div>
            <div className="min-h-0 flex-1 overflow-auto">
              {loading ? (
                <div className="py-10 text-center text-xs text-kumo-subtle">加载中...</div>
              ) : error ? (
                <div className="m-3 rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-xs text-kumo-danger">{error}</div>
              ) : (
                <Table size="sm">
                  <Table.Header>
                    <Table.Row>
                      <Table.Head className="w-[45%]">路径</Table.Head>
                      <Table.Head className="w-[12%]">大小</Table.Head>
                      <Table.Head className="w-[18%]">最后修改</Table.Head>
                      <Table.Head className="w-[15%]">权限</Table.Head>
                      <Table.Head className="app-table-action">操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {files.length === 0 ? (
                      <Table.Row>
                        <Table.Cell colSpan={5}>
                          <div className="py-10 text-center text-xs text-kumo-subtle">目录为空</div>
                        </Table.Cell>
                      </Table.Row>
                    ) : files.map(file => {
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
                          <Table.Row className="hover:bg-kumo-recessed/15">
                            <Table.Cell className="min-w-0">
                              <Button type="button" size="xs" variant="ghost" className="h-auto min-w-0 justify-start gap-2 px-0 py-0 text-left" onClick={() => openFile(file)} title={file.path}>
                                {file.isDirectory ? <Folder className="h-3.5 w-3.5 shrink-0 text-kumo-brand" /> : <FileText className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />}
                                <span className="truncate font-medium text-kumo-strong">{file.name}</span>
                              </Button>
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap font-mono text-[10px]">{file.isDirectory ? '-' : formatFileSize(file.size)}</Table.Cell>
                            <Table.Cell className="whitespace-nowrap text-[10px]">{file.mtime ? formatDateTime(file.mtime) : '-'}</Table.Cell>
                            <Table.Cell className="whitespace-nowrap font-mono text-[10px]">{file.permissions || '-'}</Table.Cell>
                            <Table.Cell>
                              <div className="flex justify-end gap-1">
                                <FileActionMenu
                                  file={file}
                                  onOpen={() => openFile(file)}
                                  onDownload={downloadUrl}
                                  onRename={startRename}
                                  onChmod={startChmod}
                                  onDelete={() => deletePath(file)}
                                />
                              </div>
                            </Table.Cell>
                          </Table.Row>
                        </FileContextMenu>
                      );
                    })}
                  </Table.Body>
                </Table>
              )}
            </div>
          </div>
        </div>

        <Input ref={uploadInputRef} size="sm" aria-label="上传 SFTP 文件" type="file" className="hidden" onChange={uploadFiles} multiple />
      </div>

      <Dialog.Root open={editorOpen && Boolean(editFile)} onOpenChange={(open) => (open ? setEditorOpen(true) : requestCloseEditor())}>
        <Dialog size="xl" className="flex h-[min(72dvh,720px)] w-[min(920px,calc(100vw-2rem))] max-h-[calc(100dvh-1rem)] max-w-none flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
            <Dialog.Title className="min-w-0 truncate text-sm font-bold text-kumo-strong">编辑 {editFile?.name}</Dialog.Title>
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

      <Dialog.Root open={mkdirOpen} onOpenChange={setMkdirOpen}>
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] !w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3"><Dialog.Title className="text-sm font-bold text-kumo-strong">新建目录</Dialog.Title><Dialog.Close /></div>
          <div className="p-4">
            <Input size="sm" label="目录名" value={mkdirName} onChange={event => setMkdirName(event.target.value)} />
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
            <Button size="sm" variant="secondary" onClick={() => setMkdirOpen(false)}>取消</Button>
            <Button size="sm" variant="primary" onClick={createDirectory}>创建</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={Boolean(renameFile)} onOpenChange={open => !open && setRenameFile(null)}>
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] !w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3"><Dialog.Title className="text-sm font-bold text-kumo-strong">重命名</Dialog.Title><Dialog.Close /></div>
          <div className="p-4">
            <Input size="sm" label="新名称" value={renameValue} onChange={event => setRenameValue(event.target.value)} />
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
            <Button size="sm" variant="secondary" onClick={() => setRenameFile(null)}>取消</Button>
            <Button size="sm" variant="primary" onClick={renamePath}>保存</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={Boolean(chmodFile)} onOpenChange={open => !open && setChmodFile(null)}>
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] !w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3"><Dialog.Title className="text-sm font-bold text-kumo-strong">修改权限</Dialog.Title><Dialog.Close /></div>
          <div className="p-4">
            <Input size="sm" label="权限值" value={chmodValue} onChange={event => setChmodValue(event.target.value)} className="font-mono" />
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
            <Button size="sm" variant="secondary" onClick={() => setChmodFile(null)}>取消</Button>
            <Button size="sm" variant="primary" onClick={chmodPath}>保存</Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </>
  );
}
