import React, { useEffect, useMemo, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import {
  createCommandSnippet,
  deleteCommandSnippet,
  fetchCommandHistory,
  fetchCommandSnippets,
  previewCommand,
  recordCommandHistory,
  updateCommandSnippet,
} from '../../modules/server-commands.js';
import { Clock, Copy, Edit, Eye, Plus, Save, Send, Star, Trash, Users } from '../Icons.jsx';

const DEFAULT_LINUX_COMMANDS = [
  { title: '当前目录', content: 'pwd', category: '默认', platform: 'linux' },
  { title: '列出文件', content: 'ls -la', category: '默认', platform: 'linux' },
  { title: '磁盘占用', content: 'df -h', category: '系统', platform: 'linux' },
  { title: 'Docker 容器', content: 'docker ps', category: 'Docker', platform: 'linux' },
];

const DEFAULT_WINDOWS_COMMANDS = [
  { title: '列出文件', content: 'dir', category: '默认', platform: 'windows' },
  { title: '网络信息', content: 'ipconfig', category: '网络', platform: 'windows' },
  { title: '进程列表', content: 'Get-Process | Select-Object -First 10', category: '系统', platform: 'windows' },
  { title: '当前位置', content: 'Get-Location', category: '默认', platform: 'windows' },
];

const EMPTY_FORM = {
  title: '',
  content: '',
  category: '默认',
  platform: 'all',
  tags: [],
  favorite: false,
  description: '',
};

function getPlatform(server) {
  const text = String(server?.info?.platform || server?.platform || '').toLowerCase();
  if (text.includes('win')) return 'windows';
  if (text.includes('darwin') || text.includes('mac')) return 'darwin';
  return 'linux';
}

export default function QuickCommandBar({
  activeServer,
  activeSessionId,
  sessions = [],
  visibleSessionIds = [],
  syncEnabled = false,
  onRunCommand,
}) {
  const [snippets, setSnippets] = useState([]);
  const [loading, setLoading] = useState(false);
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('all');
  const [customCommand, setCustomCommand] = useState('');
  const [managerOpen, setManagerOpen] = useState(false);
  const [editing, setEditing] = useState(null);
  const [form, setForm] = useState(EMPTY_FORM);
  const [targetMode, setTargetMode] = useState('active');
  const [sendMode, setSendMode] = useState('execute');
  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewState, setPreviewState] = useState(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyItems, setHistoryItems] = useState([]);

  const platform = getPlatform(activeServer);
  const defaultCommands = platform === 'windows' ? DEFAULT_WINDOWS_COMMANDS : DEFAULT_LINUX_COMMANDS;

  const targetSessions = useMemo(() => {
    if (targetMode === 'visible') {
      const visible = new Set(visibleSessionIds);
      return sessions.filter(session => visible.has(session.id));
    }
    if (targetMode === 'all') return sessions;
    return sessions.filter(session => session.id === activeSessionId);
  }, [activeSessionId, sessions, targetMode, visibleSessionIds]);

  const targetSessionIds = targetSessions.map(session => session.id);

  const loadSnippets = async () => {
    setLoading(true);
    try {
      const data = await fetchCommandSnippets({ platform, q: query });
      setSnippets(data.data || []);
    } catch (error) {
      toast.error(error.message || '加载命令片段失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadSnippets();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [platform]);

  const commands = useMemo(() => {
    const backend = snippets.map(item => ({ ...item, source: 'saved' }));
    const fallback = defaultCommands.map((item, index) => ({
      id: `default-${platform}-${index}`,
      ...item,
      source: 'default',
      favorite: false,
      dangerous: false,
      dangerous_reasons: [],
    }));
    const merged = backend.length > 0 ? backend : fallback;
    return merged.filter(item => {
      const matchesCategory = category === 'all' || (item.category || '默认') === category;
      const q = query.trim().toLowerCase();
      const matchesQuery = !q || `${item.title} ${item.content} ${item.description || ''}`.toLowerCase().includes(q);
      return matchesCategory && matchesQuery;
    });
  }, [category, defaultCommands, platform, query, snippets]);

  const categories = useMemo(() => {
    const names = new Set(['all']);
    [...snippets, ...defaultCommands].forEach(item => names.add(item.category || '默认'));
    return Array.from(names);
  }, [defaultCommands, snippets]);

  const loadHistory = async () => {
    setHistoryLoading(true);
    try {
      const data = await fetchCommandHistory({ serverId: activeServer?.id, limit: 80 });
      setHistoryItems(data.data || []);
    } catch (error) {
      toast.error(error.message || '加载执行历史失败');
    } finally {
      setHistoryLoading(false);
    }
  };

  const getPreview = async (command, options = {}) => {
    const preview = await previewCommand({
      command,
      snippetId: options.snippetId,
      serverId: activeServer?.id,
    });
    return preview.data || {
      command,
      rendered: command,
      dangerous: false,
      dangerReasons: [],
    };
  };

  const openPreview = async (command, options = {}) => {
    if (!command?.trim()) return;
    try {
      setPreviewState({
        ...(await getPreview(command, options)),
        targetCount: targetSessionIds.length,
        sendMode,
      });
      setPreviewOpen(true);
    } catch (error) {
      toast.error(error.message || '生成命令预览失败');
    }
  };

  const runCommand = async (command, options = {}) => {
    if (targetSessionIds.length === 0 || !command?.trim()) return;

    let rendered = command;
    let dangerous = false;
    let dangerReasons = [];
    try {
      const preview = await getPreview(command, options);
      rendered = preview.rendered || command;
      dangerous = Boolean(preview.dangerous);
      dangerReasons = preview.dangerReasons || [];
    } catch {
      // ignore preview failure
    }

    const isBatch = targetSessionIds.length > 1;
    if (dangerous || isBatch || (syncEnabled && options.syncAware !== false)) {
      const ok = await dialog.confirm({
        title: dangerous ? '确认执行高风险命令' : '确认批量执行命令',
        message: dangerous
          ? `检测到风险项：${dangerReasons.join('、') || '请谨慎执行'}\n\n${rendered}`
          : `该命令会发送到 ${targetSessionIds.length} 个终端。\n\n${rendered}`,
        confirmText: '确认执行',
        cancelText: '取消',
        variant: dangerous ? 'danger' : 'default',
      });
      if (!ok) return;
    }

    onRunCommand?.(rendered, {
      ...options,
      targetSessionIds,
      appendNewline: sendMode === 'execute',
    });
    setCustomCommand('');
    recordCommandHistory({
      snippetId: options.snippetId,
      serverId: activeServer?.id,
      command,
      renderedCommand: rendered,
      executionMode: targetSessionIds.length > 1 ? 'batch-terminal' : (sendMode === 'insert' ? 'insert-terminal' : (syncEnabled ? 'sync-terminal' : 'terminal')),
      status: 'sent',
    }).catch(() => {});
  };

  const startCreate = () => {
    setEditing(null);
    setForm({ ...EMPTY_FORM, platform });
    setManagerOpen(true);
  };

  const startEdit = (snippet) => {
    setEditing(snippet);
    setForm({
      title: snippet.title || '',
      content: snippet.content || '',
      category: snippet.category || '默认',
      platform: snippet.platform || 'all',
      tags: snippet.tags || [],
      favorite: Boolean(snippet.favorite),
      description: snippet.description || '',
    });
    setManagerOpen(true);
  };

  const saveSnippet = async () => {
    if (!form.title.trim() || !form.content.trim()) {
      toast.error('请填写命令名称和内容');
      return;
    }
    try {
      if (editing?.id) {
        await updateCommandSnippet(editing.id, form);
        toast.success('命令片段已更新');
      } else {
        await createCommandSnippet(form);
        toast.success('命令片段已创建');
      }
      setManagerOpen(false);
      await loadSnippets();
    } catch (error) {
      toast.error(error.message || '保存命令片段失败');
    }
  };

  const removeSnippet = async (snippet) => {
    const ok = await dialog.deleteResource({
      resourceType: '命令片段',
      resourceName: snippet.title,
    });
    if (!ok) return;
    try {
      await deleteCommandSnippet(snippet.id);
      toast.success('命令片段已删除');
      await loadSnippets();
    } catch (error) {
      toast.error(error.message || '删除命令片段失败');
    }
  };

  return (
    <>
      <div className="flex h-full min-h-0 flex-col border-t border-kumo-line bg-kumo-base">
        <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-3 py-3">
          <div className="min-w-0">
            <div className="text-sm font-semibold text-kumo-strong">命令片段</div>
            <div className="text-[10px] text-kumo-subtle">发送到当前终端或分屏组</div>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <Button size="sm" variant="secondary" shape="square" icon={<Plus className="h-3.5 w-3.5" />} aria-label="新增片段" title="新增片段" onClick={startCreate} />
            <Button size="sm" variant="secondary" shape="square" icon={<Clock className="h-3.5 w-3.5" />} aria-label="执行历史" title="执行历史" onClick={() => { setHistoryOpen(true); loadHistory(); }} />
          </div>
        </div>

        <div className="flex min-h-0 flex-1 flex-col gap-3 p-3">
          <div className="grid grid-cols-[minmax(0,1fr)_5.5rem_5.5rem] gap-2">
            <Input size="sm" aria-label="自定义命令" value={customCommand} onChange={event => setCustomCommand(event.target.value)} placeholder="输入命令，支持 {host} {username} {cwd}" className="min-w-0 font-mono" />
            <Select size="sm" aria-label="命令目标" value={targetMode} onChange={event => setTargetMode(event.target.value)}>
              <option value="active">当前</option>
              <option value="visible">分屏</option>
              <option value="all">全部</option>
            </Select>
            <Select size="sm" aria-label="发送模式" value={sendMode} onChange={event => setSendMode(event.target.value)}>
              <option value="execute">执行</option>
              <option value="insert">插入</option>
            </Select>
          </div>

          <div className="flex items-center gap-2">
            <Button size="sm" variant="primary" icon={<Send className="h-3.5 w-3.5" />} disabled={targetSessionIds.length === 0 || !customCommand.trim()} onClick={() => runCommand(customCommand.trim())}>
              {sendMode === 'insert' ? '插入' : '发送'}
            </Button>
            <Button size="sm" variant="secondary" icon={<Eye className="h-3.5 w-3.5" />} disabled={!customCommand.trim()} onClick={() => openPreview(customCommand.trim())}>
              预览
            </Button>
          </div>

          <div className="grid grid-cols-3 gap-2">
            <button type="button" className={`rounded-md border px-2 py-2 text-xs ${category === 'all' ? 'border-kumo-interact bg-kumo-brand/10 text-kumo-brand' : 'border-kumo-line text-kumo-default'}`} onClick={() => setCategory('all')}>
              全部
            </button>
            {categories.filter(item => item !== 'all').slice(0, 2).map(item => (
              <button key={item} type="button" className={`rounded-md border px-2 py-2 text-xs ${category === item ? 'border-kumo-interact bg-kumo-brand/10 text-kumo-brand' : 'border-kumo-line text-kumo-default'}`} onClick={() => setCategory(item)}>
                {item}
              </button>
            ))}
          </div>

          <Input size="sm" aria-label="搜索命令片段" value={query} onChange={event => setQuery(event.target.value)} onBlur={loadSnippets} placeholder="搜索标题、命令、说明" className="w-full" />

          <div className="min-h-0 flex-1 space-y-2 overflow-y-auto pr-1">
            {loading ? (
              <div className="py-8 text-center text-xs text-kumo-subtle">加载中...</div>
            ) : commands.length === 0 ? (
              <div className="rounded-md border border-dashed border-kumo-line px-3 py-8 text-center text-xs text-kumo-subtle">暂无匹配的命令片段</div>
            ) : commands.map(command => (
              <div key={command.id} className="rounded-md border border-kumo-line bg-kumo-base p-2.5">
                <div className="flex items-start gap-2">
                  <button
                    type="button"
                    className="min-w-0 flex-1 text-left"
                    onClick={() => runCommand(command.content, { snippetId: command.source === 'saved' ? command.id : undefined })}
                    disabled={targetSessionIds.length === 0}
                  >
                    <div className="flex items-center gap-1.5">
                      <span className="truncate text-sm font-medium text-kumo-strong">{command.title || command.content}</span>
                      {command.favorite ? <Star className="h-3.5 w-3.5 shrink-0 text-kumo-warning" /> : null}
                    </div>
                    <div className="mt-1 truncate font-mono text-[11px] text-kumo-subtle" title={command.content}>{command.content}</div>
                    {command.description ? <div className="mt-1 line-clamp-2 text-[11px] text-kumo-subtle">{command.description}</div> : null}
                  </button>
                  <div className="flex shrink-0 items-center gap-1">
                    <Button shape="square" size="sm" variant="ghost" icon={<Eye className="h-3.5 w-3.5" />} aria-label="预览命令" title="预览命令" onClick={() => openPreview(command.content, { snippetId: command.source === 'saved' ? command.id : undefined })} />
                    {command.source === 'saved' ? <Button shape="square" size="sm" variant="ghost" icon={<Edit className="h-3.5 w-3.5" />} aria-label="编辑命令" title="编辑命令" onClick={() => startEdit(command)} /> : null}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <Dialog.Root open={managerOpen} onOpenChange={setManagerOpen}>
        <Dialog size="md" className="flex max-h-[calc(100dvh-1rem)] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
            <Dialog.Title className="text-sm font-bold text-kumo-strong">{editing ? '编辑命令片段' : '新增命令片段'}</Dialog.Title>
            <Dialog.Close />
          </div>
          <div className="space-y-3 overflow-y-auto p-4">
            <Input size="sm" label="名称" value={form.title} onChange={event => setForm(prev => ({ ...prev, title: event.target.value }))} />
            <Input size="sm" label="分类" value={form.category} onChange={event => setForm(prev => ({ ...prev, category: event.target.value }))} />
            <Select size="sm" label="适用平台" value={form.platform} onChange={event => setForm(prev => ({ ...prev, platform: event.target.value }))}>
              <option value="all">全部</option>
              <option value="linux">Linux</option>
              <option value="windows">Windows</option>
              <option value="darwin">macOS</option>
            </Select>
            <Input size="sm" label="命令" value={form.content} onChange={event => setForm(prev => ({ ...prev, content: event.target.value }))} className="font-mono" />
            <Input size="sm" label="说明" value={form.description} onChange={event => setForm(prev => ({ ...prev, description: event.target.value }))} />
            <Button size="sm" variant={form.favorite ? 'primary' : 'secondary'} icon={<Star className="h-3.5 w-3.5" />} onClick={() => setForm(prev => ({ ...prev, favorite: !prev.favorite }))}>
              {form.favorite ? '已收藏' : '收藏'}
            </Button>
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
            {editing?.id ? <Button size="sm" variant="danger" icon={<Trash className="h-3.5 w-3.5" />} onClick={() => removeSnippet(editing)}>删除</Button> : null}
            <Button size="sm" variant="secondary" onClick={() => setManagerOpen(false)}>取消</Button>
            <Button size="sm" variant="primary" icon={<Save className="h-3.5 w-3.5" />} onClick={saveSnippet}>保存</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={previewOpen} onOpenChange={setPreviewOpen}>
        <Dialog size="md" className="flex max-h-[calc(100dvh-1rem)] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
            <Dialog.Title className="text-sm font-bold text-kumo-strong">命令预览</Dialog.Title>
            <Dialog.Close />
          </div>
          <div className="space-y-3 overflow-y-auto p-4 text-xs">
            <div className="grid gap-2 sm:grid-cols-2">
              <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 p-2">
                <div className="text-kumo-subtle">目标终端</div>
                <div className="mt-1 flex items-center gap-1 font-semibold text-kumo-strong">
                  <Users className="h-3.5 w-3.5" />
                  {previewState?.targetCount || 0} 个
                </div>
              </div>
              <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 p-2">
                <div className="text-kumo-subtle">发送模式</div>
                <div className="mt-1 font-semibold text-kumo-strong">{sendMode === 'insert' ? '插入终端' : '立即执行'}</div>
              </div>
            </div>
            <Textarea label="原始命令" readOnly value={previewState?.command || ''} className="min-h-20 font-mono text-xs" />
            <Textarea label="变量替换后" readOnly value={previewState?.rendered || ''} className="min-h-24 font-mono text-xs" />
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
            <Button size="sm" variant="secondary" onClick={() => setPreviewOpen(false)}>关闭</Button>
            <Button size="sm" variant="primary" icon={<Send className="h-3.5 w-3.5" />} disabled={!previewState?.command || targetSessionIds.length === 0} onClick={() => {
              setPreviewOpen(false);
              runCommand(previewState.command);
            }}>
              {sendMode === 'insert' ? '插入' : '执行'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={historyOpen} onOpenChange={setHistoryOpen}>
        <Dialog size="lg" className="flex max-h-[calc(100dvh-1rem)] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
            <Dialog.Title className="text-sm font-bold text-kumo-strong">命令执行历史</Dialog.Title>
            <Dialog.Close />
          </div>
          <div className="space-y-2 overflow-y-auto p-4">
            {historyLoading ? (
              <div className="py-8 text-center text-xs text-kumo-subtle">加载历史中...</div>
            ) : historyItems.length === 0 ? (
              <div className="py-8 text-center text-xs text-kumo-subtle">暂无执行历史</div>
            ) : (
              <div className="max-h-[26rem] overflow-auto rounded-md border border-kumo-line scrollbar-thin">
                {historyItems.map(item => (
                  <div key={item.id} className="flex min-w-0 items-start justify-between gap-3 border-b border-kumo-line bg-kumo-base p-2 last:border-b-0">
                    <div className="min-w-0">
                      <div className="truncate font-mono text-xs text-kumo-strong" title={item.rendered_command}>{item.rendered_command}</div>
                      <div className="mt-1 flex flex-wrap items-center gap-2 text-[10px] text-kumo-subtle">
                        <span>{item.server_name || '未指定主机'}</span>
                        <span>{item.execution_mode || 'terminal'}</span>
                        <span>{item.created_at || '-'}</span>
                      </div>
                    </div>
                    <div className="flex shrink-0 gap-1">
                      <Button shape="square" size="sm" variant="ghost" icon={<Copy className="h-3.5 w-3.5" />} aria-label="填入历史命令" title="填入历史命令" onClick={() => {
                        setCustomCommand(item.rendered_command || item.command || '');
                        setHistoryOpen(false);
                      }} />
                      <Button shape="square" size="sm" variant="ghost" icon={<Send className="h-3.5 w-3.5" />} aria-label="再次执行" title="再次执行" disabled={targetSessionIds.length === 0} onClick={() => runCommand(item.command || item.rendered_command || '')} />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
            <Button size="sm" variant="secondary" onClick={loadHistory} loading={historyLoading}>刷新</Button>
            <Button size="sm" variant="primary" onClick={() => setHistoryOpen(false)}>关闭</Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </>
  );
}
