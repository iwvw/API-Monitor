import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Table } from '@cloudflare/kumo/components/table';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { formatFileSize, formatDateTime } from '../../modules/utils.js';
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
import { Download, Edit, FileText, Folder, FolderOpen, Key, RefreshCw, Save, Trash, Upload, X } from '../Icons.jsx';

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

export default function SftpPanel({
  serverId,
  serverName,
  initialPath = '.',
  onClose,
  onPathChange,
}) {
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
      setEditFile({
        path: file.path,
        name: file.name,
        content: data.data || '',
        originalContent: data.data || '',
      });
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
      <div className="h-72 shrink-0 border-t border-kumo-line bg-kumo-base p-3 text-xs">
        <div className="mb-2 flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex min-w-0 items-center gap-2">
            <FolderOpen className="h-4 w-4 text-kumo-subtle" />
            <span className="font-bold text-kumo-strong">SFTP</span>
            <span className="truncate text-[10px] text-kumo-subtle">{serverName || serverId || '-'}</span>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <Button size="sm" variant="secondary" onClick={() => setMkdirOpen(true)}>新建目录</Button>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              icon={<Upload className="h-3.5 w-3.5" />}
              onClick={() => uploadInputRef.current?.click()}
              disabled={!serverId || uploading}
            >
              上传
            </Button>
            <Input
              ref={uploadInputRef}
              size="sm"
              aria-label="上传 SFTP 文件"
              type="file"
              className="hidden"
              onChange={uploadFiles}
              multiple
            />
            <Button
              size="sm"
              variant="secondary"
              icon={<RefreshCw className="h-3.5 w-3.5" />}
              onClick={() => loadDirectory(currentPath)}
              loading={loading || uploading}
            >
              刷新
            </Button>
            <Button
              shape="square"
              size="sm"
              variant="ghost"
              icon={<X className="h-3 w-3" />}
              aria-label="关闭 SFTP"
              title="关闭 SFTP"
              onClick={onClose}
            />
          </div>
        </div>

        <form
          className="mb-2 flex min-w-0 items-center gap-1.5"
          onSubmit={event => {
            event.preventDefault();
            loadDirectory(pathInput);
          }}
        >
          <Input
            size="sm"
            aria-label="SFTP 路径"
            value={pathInput}
            onChange={event => setPathInput(event.target.value)}
            className="min-w-0 flex-1 font-mono"
          />
          <Button size="sm" variant="primary" type="submit">跳转</Button>
        </form>

        <div className="mb-2 flex items-center gap-1.5 overflow-x-auto whitespace-nowrap text-[10px] scrollbar-thin">
          {breadcrumbs.map((crumb, idx) => (
            <React.Fragment key={`${crumb.path}-${idx}`}>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => loadDirectory(crumb.path)}
                className="h-5 px-1 py-0 text-[10px] font-semibold text-kumo-subtle hover:text-kumo-strong"
              >
                {crumb.name}
              </Button>
              {idx < breadcrumbs.length - 1 && <span className="opacity-40">/</span>}
            </React.Fragment>
          ))}
        </div>

        <div className="max-h-40 overflow-auto rounded-md border border-kumo-line scrollbar-thin">
          {loading ? (
            <div className="py-8 text-center text-[10px] text-kumo-subtle">读取远程目录中...</div>
          ) : error ? (
            <div className="m-2 rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-2 text-[10px] text-kumo-danger">{error}</div>
          ) : files.length === 0 ? (
            <div className="py-8 text-center text-[10px] text-kumo-subtle">当前目录为空</div>
          ) : (
            <Table.Root size="sm">
              <Table.Header>
                <Table.Row>
                  <Table.ColumnHeader>名称</Table.ColumnHeader>
                  <Table.ColumnHeader>大小</Table.ColumnHeader>
                  <Table.ColumnHeader>权限</Table.ColumnHeader>
                  <Table.ColumnHeader>修改时间</Table.ColumnHeader>
                  <Table.ColumnHeader className="text-right">操作</Table.ColumnHeader>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {files.map(file => (
                  <Table.Row key={file.path}>
                    <Table.Cell className="min-w-48">
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => openFile(file)}
                        className="max-w-64 justify-start px-1"
                        title={file.path}
                      >
                        {file.isDirectory ? <Folder className="h-3.5 w-3.5 shrink-0" /> : <FileText className="h-3.5 w-3.5 shrink-0" />}
                        <span className="truncate">{file.name}</span>
                      </Button>
                    </Table.Cell>
                    <Table.Cell className="whitespace-nowrap font-mono text-[10px]">{file.isDirectory ? '目录' : formatFileSize(file.size)}</Table.Cell>
                    <Table.Cell className="whitespace-nowrap font-mono text-[10px]">{file.permissions || '-'}</Table.Cell>
                    <Table.Cell className="whitespace-nowrap text-[10px]">{file.mtime ? formatDateTime(file.mtime) : '-'}</Table.Cell>
                    <Table.Cell>
                      <div className="flex justify-end gap-1">
                        {!file.isDirectory && (
                          <LinkButton
                            size="sm"
                            variant="ghost"
                            href={buildSftpDownloadUrl(serverId, file.path)}
                            icon={<Download className="h-3 w-3" />}
                            title="下载"
                          />
                        )}
                        {!file.isDirectory && (
                          <Button shape="square" size="sm" variant="ghost" icon={<Edit className="h-3 w-3" />} aria-label="编辑" title="编辑" onClick={() => openFile(file)} />
                        )}
                        <Button shape="square" size="sm" variant="ghost" icon={<Edit className="h-3 w-3" />} aria-label="重命名" title="重命名" onClick={() => { setRenameFile(file); setRenameValue(file.name); }} />
                        <Button shape="square" size="sm" variant="ghost" icon={<Key className="h-3 w-3" />} aria-label="权限" title="权限" onClick={() => { setChmodFile(file); setChmodValue(String(file.mode ? (file.mode & 0o777).toString(8) : '644')); }} />
                        <Button shape="square" size="sm" variant="ghost" icon={<Trash className="h-3 w-3" />} aria-label="删除" title="删除" onClick={() => deletePath(file)} className="hover:text-kumo-danger" />
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          )}
        </div>
      </div>

      <Dialog.Root
        open={editorOpen && Boolean(editFile)}
        onOpenChange={(open) => {
          if (open) {
            setEditorOpen(true);
          } else {
            requestCloseEditor();
          }
        }}
      >
        <Dialog.Content size="xl">
          <Dialog.Header>
            <Dialog.Title className="min-w-0 truncate">在线编辑：{editFile?.name}</Dialog.Title>
            <Dialog.Close />
          </Dialog.Header>
          <Dialog.Body className="space-y-2">
            <div className="truncate font-mono text-[10px] text-kumo-subtle">{editFile?.path}</div>
            <Textarea
              aria-label="SFTP 文件内容"
              value={editFile?.content || ''}
              onChange={event => setEditFile(prev => ({ ...prev, content: event.target.value }))}
              className="min-h-96 font-mono text-xs"
            />
          </Dialog.Body>
          <Dialog.Footer>
            <Button size="sm" variant="secondary" onClick={requestCloseEditor}>取消</Button>
            <Button size="sm" variant="primary" icon={<Save className="h-3.5 w-3.5" />} loading={saving} onClick={saveFile}>保存文件</Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog.Root>

      <Dialog.Root open={mkdirOpen} onOpenChange={setMkdirOpen}>
        <Dialog.Content size="sm">
          <Dialog.Header><Dialog.Title>新建目录</Dialog.Title><Dialog.Close /></Dialog.Header>
          <Dialog.Body>
            <Input size="sm" label="目录名" value={mkdirName} onChange={event => setMkdirName(event.target.value)} />
          </Dialog.Body>
          <Dialog.Footer>
            <Button size="sm" variant="secondary" onClick={() => setMkdirOpen(false)}>取消</Button>
            <Button size="sm" variant="primary" onClick={createDirectory}>创建</Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog.Root>

      <Dialog.Root open={Boolean(renameFile)} onOpenChange={open => !open && setRenameFile(null)}>
        <Dialog.Content size="sm">
          <Dialog.Header><Dialog.Title>重命名</Dialog.Title><Dialog.Close /></Dialog.Header>
          <Dialog.Body>
            <Input size="sm" label="新名称" value={renameValue} onChange={event => setRenameValue(event.target.value)} />
          </Dialog.Body>
          <Dialog.Footer>
            <Button size="sm" variant="secondary" onClick={() => setRenameFile(null)}>取消</Button>
            <Button size="sm" variant="primary" onClick={renamePath}>保存</Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog.Root>

      <Dialog.Root open={Boolean(chmodFile)} onOpenChange={open => !open && setChmodFile(null)}>
        <Dialog.Content size="sm">
          <Dialog.Header><Dialog.Title>修改权限</Dialog.Title><Dialog.Close /></Dialog.Header>
          <Dialog.Body>
            <Input size="sm" label="权限值" value={chmodValue} onChange={event => setChmodValue(event.target.value)} className="font-mono" />
          </Dialog.Body>
          <Dialog.Footer>
            <Button size="sm" variant="secondary" onClick={() => setChmodFile(null)}>取消</Button>
            <Button size="sm" variant="primary" onClick={chmodPath}>保存</Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog.Root>
    </>
  );
}
