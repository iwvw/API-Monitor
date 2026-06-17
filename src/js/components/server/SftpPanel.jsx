import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Table } from '@cloudflare/kumo/components/table';
import { Popover } from '@cloudflare/kumo';
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
    <Popover>
      <Popover.Trigger
        render={(
          <Button shape="square" size="sm" variant="ghost" icon={<Eye className="h-3 w-3" />} aria-label="文件操作" title="文件操作" />
        )}
      />
      <Popover.Content side="left" align="start" className="w-44 p-0">
        <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-control p-1.5">
          <button className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm text-kumo-default hover:bg-kumo-recessed/60" type="button" onClick={onOpen}>
            <Eye className="h-3.5 w-3.5" /> 打开
          </button>
          {!file.isDirectory && onDownload ? (
            <LinkButton
              href={onDownload}
              className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm text-kumo-default hover:bg-kumo-recessed/60"
            >
              <Download className="h-3.5 w-3.5" /> 下载
            </LinkButton>
          ) : null}
          <button className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm text-kumo-default hover:bg-kumo-recessed/60" type="button" onClick={onRename}>
            <Edit className="h-3.5 w-3.5" /> 重命名
          </button>
          <button className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm text-kumo-default hover:bg-kumo-recessed/60" type="button" onClick={onChmod}>
            <Key className="h-3.5 w-3.5" /> 权限
          </button>
          <div className="my-1 h-px bg-kumo-line" />
          <button className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm text-kumo-danger hover:bg-kumo-danger/10" type="button" onClick={onDelete}>
            <Trash className="h-3.5 w-3.5" /> 删除
          </button>
        </div>
      </Popover.Content>
    </Popover>
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
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

  return (
    <>
      <div className="flex h-full min-h-0 flex-col border-t border-kumo-line bg-kumo-base">
        <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-3 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-sm font-semibold text-kumo-strong">
              <FolderOpen className="h-4 w-4 text-kumo-brand" />
              <span className="truncate">FileSystem</span>
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
                  <button type="button" className="truncate font-semibold text-kumo-default hover:text-kumo-strong" onClick={() => loadDirectory(crumb.path)}>
                    {crumb.name}
                  </button>
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
                      <Table.Head className="text-right">操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {files.length === 0 ? (
                      <Table.Row>
                        <Table.Cell colSpan={5}>
                          <div className="py-10 text-center text-xs text-kumo-subtle">当前目录为空</div>
                        </Table.Cell>
                      </Table.Row>
                    ) : files.map(file => (
                      <Table.Row key={file.path} className={file.isDirectory ? '' : ''}>
                        <Table.Cell className="min-w-0">
                          <button type="button" className="flex min-w-0 items-center gap-2 text-left" onClick={() => openFile(file)} title={file.path}>
                            {file.isDirectory ? <Folder className="h-3.5 w-3.5 shrink-0 text-kumo-brand" /> : <FileText className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />}
                            <span className="truncate font-medium text-kumo-strong">{file.name}</span>
                          </button>
                        </Table.Cell>
                        <Table.Cell className="whitespace-nowrap font-mono text-[10px]">{file.isDirectory ? '-' : formatFileSize(file.size)}</Table.Cell>
                        <Table.Cell className="whitespace-nowrap text-[10px]">{file.mtime ? formatDateTime(file.mtime) : '-'}</Table.Cell>
                        <Table.Cell className="whitespace-nowrap font-mono text-[10px]">{file.permissions || '-'}</Table.Cell>
                        <Table.Cell>
                          <div className="flex justify-end gap-1">
                            <FileActionMenu
                              file={file}
                              onOpen={() => openFile(file)}
                              onDownload={!file.isDirectory ? buildSftpDownloadUrl(serverId, file.path) : null}
                              onRename={() => { setRenameFile(file); setRenameValue(file.name); }}
                              onChmod={() => { setChmodFile(file); setChmodValue(String(file.mode ? (file.mode & 0o777).toString(8) : '644')); }}
                              onDelete={() => deletePath(file)}
                            />
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              )}
            </div>
          </div>
        </div>

        <Input ref={uploadInputRef} size="sm" aria-label="上传 SFTP 文件" type="file" className="hidden" onChange={uploadFiles} multiple />
      </div>

      <Dialog.Root open={editorOpen && Boolean(editFile)} onOpenChange={(open) => (open ? setEditorOpen(true) : requestCloseEditor())}>
        <Dialog size="xl" className="flex max-h-[calc(100dvh-1rem)] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
            <Dialog.Title className="min-w-0 truncate text-sm font-bold text-kumo-strong">编辑 {editFile?.name}</Dialog.Title>
            <Dialog.Close />
          </div>
          <div className="space-y-2 overflow-y-auto p-4">
            <div className="truncate font-mono text-[10px] text-kumo-subtle">{editFile?.path}</div>
            <Textarea aria-label="SFTP 文件内容" value={editFile?.content || ''} onChange={event => setEditFile(prev => ({ ...prev, content: event.target.value }))} className="min-h-96 font-mono text-xs" />
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
            <Button size="sm" variant="secondary" onClick={requestCloseEditor}>取消</Button>
            <Button size="sm" variant="primary" icon={<Save className="h-3.5 w-3.5" />} loading={saving} onClick={saveFile}>保存</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={mkdirOpen} onOpenChange={setMkdirOpen}>
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] flex-col overflow-hidden p-0">
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
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] flex-col overflow-hidden p-0">
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
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] flex-col overflow-hidden p-0">
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
