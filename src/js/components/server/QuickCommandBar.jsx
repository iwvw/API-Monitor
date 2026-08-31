import React, { useEffect, useMemo, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Tabs } from '@cloudflare/kumo';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
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
import { Clock, Copy, Edit, Plus, Save, Send, Star, Trash } from '../Icons.jsx';
import CodeEditor from '../ui/CodeEditor.jsx';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';

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

function normalizeCategory(value) {
  const text = String(value || '').trim();
  return text || '默认';
}

export default function QuickCommandBar({
  activeServer,
  activeSessionId,
  sessions = [],
  visibleSessionIds = [],
  syncEnabled = false,
  onRunCommand,
}) {
  const { isArmed, confirmPress } = useConfirmPress();
  const [snippets, setSnippets] = useState([]);
  const [loading, setLoading] = useState(false);
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('all');
  const [customCommand, setCustomCommand] = useState('');
  const [managerOpen, setManagerOpen] = useState(false);
  const [editing, setEditing] = useState(null);
  const [form, setForm] = useState(EMPTY_FORM);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyItems, setHistoryItems] = useState([]);

  const platform = getPlatform(activeServer);
  const defaultCommands = platform === 'windows' ? DEFAULT_WINDOWS_COMMANDS : DEFAULT_LINUX_COMMANDS;

  const targetSessions = useMemo(() => {
    return sessions.filter(session => session.id === activeSessionId);
  }, [activeSessionId, sessions]);

  const targetSessionIds = targetSessions.map(session => session.id);
  const effectiveTargetCount = syncEnabled
    ? Math.max(visibleSessionIds.length, targetSessionIds.length)
    : targetSessionIds.length;

  const loadSnippets = async () => {
    setLoading(true);
    try {
      const data = await fetchCommandSnippets({ q: query });
      setSnippets(data.data || []);
    } catch (error) {
      toast.error(error.message || '加载命令片段失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadSnippets();
     
  }, [platform]);

  const commands = useMemo(() => {
    const backend = snippets.map((item, index) => ({ ...item, source: 'saved', order: index }));
    const fallback = defaultCommands.map((item, index) => ({
      id: `default-${platform}-${index}`,
      ...item,
      source: 'default',
      order: index,
      favorite: false,
      dangerous: false,
      dangerous_reasons: [],
    }));
    const merged = backend.length > 0 ? backend : fallback;
    const q = query.trim().toLowerCase();
    return merged
      .filter(item => {
        const matchesCategory = category === 'all' || normalizeCategory(item.category) === category;
        const matchesQuery = !q || `${item.title} ${item.content} ${item.description || ''}`.toLowerCase().includes(q);
        return matchesCategory && matchesQuery;
      })
      .sort((a, b) => Number(Boolean(b.favorite)) - Number(Boolean(a.favorite)) || a.order - b.order);
  }, [category, defaultCommands, platform, query, snippets]);

  const categories = useMemo(() => {
    const names = new Set(['all']);
    [...snippets, ...defaultCommands].forEach(item => names.add(normalizeCategory(item.category)));
    return Array.from(names);
  }, [defaultCommands, snippets]);

  const categoryOptions = useMemo(() => categories.filter(item => item !== 'all'), [categories]);

  const categoryTabs = useMemo(() => [
    { value: 'all', label: '全部' },
    ...categoryOptions.map(item => ({ value: item, label: item })),
  ], [categoryOptions]);

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

    if (dangerous) {
      const ok = await dialog.confirm({
        title: '确认执行高风险命令',
        message: `检测到风险项：${dangerReasons.join('、') || '请谨慎执行'}\n\n${rendered}`,
        confirmText: '确认执行',
        cancelText: '取消',
        variant: 'danger',
      });
      if (!ok) return;
    }

    onRunCommand?.(rendered, {
      ...options,
      targetSessionIds,
      appendNewline: true,
    });
    setCustomCommand('');
    recordCommandHistory({
      snippetId: options.snippetId,
      serverId: activeServer?.id,
      command,
      renderedCommand: rendered,
      executionMode: syncEnabled ? 'sync-terminal' : 'terminal',
      status: 'sent',
    }).catch(() => {});
  };

  const startCreate = () => {
    setEditing(null);
    setForm({ ...EMPTY_FORM, platform: 'all' });
    setManagerOpen(true);
  };

  const startEdit = (snippet) => {
    setEditing(snippet);
    setForm({
      title: snippet.title || '',
      content: snippet.content || '',
      category: normalizeCategory(snippet.category),
      platform: 'all',
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
      const payload = { ...form, category: normalizeCategory(form.category), platform: 'all' };
      if (editing?.id) {
        await updateCommandSnippet(editing.id, payload);
        toast.success('命令片段已更新');
      } else {
        await createCommandSnippet(payload);
        toast.success('命令片段已创建');
      }
      setManagerOpen(false);
      await loadSnippets();
    } catch (error) {
      toast.error(error.message || '保存命令片段失败');
    }
  };

  const removeSnippet = async (snippet) => {
    if (!confirmPress('snippet-remove', '删除命令片段')) return;
    try {
      await deleteCommandSnippet(snippet.id);
      toast.success('命令片段已删除');
      await loadSnippets();
    } catch (error) {
      toast.error(error.message || '删除命令片段失败');
    }
  };

  const toggleFavorite = async (snippet) => {
    if (snippet.source !== 'saved' || !snippet.id) return;
    const nextFavorite = !snippet.favorite;
    try {
      await updateCommandSnippet(snippet.id, {
        title: snippet.title || '',
        content: snippet.content || '',
        category: normalizeCategory(snippet.category),
        platform: 'all',
        tags: snippet.tags || [],
        favorite: nextFavorite,
        description: snippet.description || '',
      });
      setSnippets(prev => prev.map(item => (
        item.id === snippet.id ? { ...item, favorite: nextFavorite } : item
      )));
      toast.success(nextFavorite ? '已收藏命令片段' : '已取消收藏');
    } catch (error) {
      toast.error(error.message || '更新收藏状态失败');
    }
  };

  const sendCustomCommand = () => {
    const command = customCommand.trim();
    if (targetSessionIds.length === 0 || !command) return;
    runCommand(command);
  };

  const handleCustomCommandKeyDown = (event) => {
    if (event.key !== 'Enter' || event.isComposing || event.nativeEvent?.isComposing) return;
    event.preventDefault();
    sendCustomCommand();
  };

  return (
    <>
      <div className="flex h-full min-h-0 w-full flex-col overflow-hidden border-t border-kumo-line bg-kumo-base">
        <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-3 py-3">
          <div className="min-w-0">
            <div className="text-sm font-semibold text-kumo-strong">命令片段</div>
            <div className="text-[10px] text-kumo-subtle">发送到终端</div>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <Button size="sm" variant="secondary" shape="square" icon={<Plus className="h-3.5 w-3.5" />} aria-label="新增片段" title="新增片段" onClick={startCreate} />
            <Button size="sm" variant="secondary" shape="square" icon={<Clock className="h-3.5 w-3.5" />} aria-label="执行历史" title="执行历史" onClick={() => { setHistoryOpen(true); loadHistory(); }} />
          </div>
        </div>

        <div className="flex min-h-0 flex-1 flex-col gap-3 p-3">
          <div className="flex min-w-0 items-center gap-2">
            <Input
              size="sm"
              aria-label="自定义命令"
              aria-keyshortcuts="Enter"
              value={customCommand}
              onChange={event => setCustomCommand(event.target.value)}
              onKeyDown={handleCustomCommandKeyDown}
              placeholder="输入命令，可用 {host} {username} {cwd}"
              className="min-w-0 flex-1 font-mono"
            />
            <Button size="sm" variant="primary" className="shrink-0" icon={<Send className="h-3.5 w-3.5" />} disabled={targetSessionIds.length === 0 || !customCommand.trim()} onClick={sendCustomCommand}>
              发送
            </Button>
          </div>

          <Tabs
            {...TOOL_TABS_PROPS}
            value={category}
            onValueChange={setCategory}
            tabs={categoryTabs}
            className="min-w-0 overflow-hidden"
            listClassName="w-full min-w-0 overflow-x-auto scrollbar-thin"
          />

          <Input size="sm" aria-label="搜索命令片段" value={query} onChange={event => setQuery(event.target.value)} onBlur={loadSnippets} placeholder="搜索标题、命令、说明" className="w-full" />

          <div className="min-h-0 min-w-0 flex-1 space-y-2 overflow-y-auto overflow-x-hidden pr-1">
            {loading ? (
              <div className="py-8 text-center text-xs text-kumo-subtle">加载中...</div>
            ) : commands.length === 0 ? (
              <div className="rounded-md border border-dashed border-kumo-line px-3 py-8 text-center text-xs text-kumo-subtle">暂无匹配的命令片段</div>
            ) : commands.map(command => (
              <div key={command.id} className="min-w-0 overflow-hidden rounded-md border border-kumo-line bg-kumo-base p-2.5">
                <div className="flex min-w-0 items-center gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    className="h-auto min-w-0 flex-1 overflow-hidden px-0 py-0 text-left"
                    onClick={() => runCommand(command.content, { snippetId: command.source === 'saved' ? command.id : undefined })}
                    disabled={targetSessionIds.length === 0}
                  >
                    <div className="flex w-full min-w-0 flex-col items-start overflow-hidden">
                      <div className="flex w-full min-w-0 items-center gap-1.5">
                        <span className="min-w-0 flex-1 truncate text-sm font-medium text-kumo-strong">{command.title || command.content}</span>
                      </div>
                      <div className="mt-1 w-full min-w-0 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-kumo-subtle" title={command.content}>{command.content}</div>
                      {command.description ? <div className="mt-1 w-full min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-kumo-subtle">{command.description}</div> : null}
                    </div>
                  </Button>
                  <div className="flex w-16 shrink-0 items-center justify-end gap-1 self-center">
                    {command.source === 'saved' ? (
                      <Button
                        shape="square"
                        size="sm"
                        variant="ghost"
                        icon={<Star className={`h-3.5 w-3.5 ${command.favorite ? 'fill-current text-kumo-warning' : 'text-kumo-subtle'}`} />}
                        aria-label={command.favorite ? '取消收藏命令' : '收藏命令'}
                        title={command.favorite ? '取消收藏' : '收藏'}
                        onClick={() => toggleFavorite(command)}
                      />
                    ) : (
                      <Button
                        shape="square"
                        size="sm"
                        variant="ghost"
                        className="opacity-0"
                        icon={<Star className="h-3.5 w-3.5" />}
                        aria-label="默认命令不可收藏"
                        tabIndex={-1}
                        disabled
                      />
                    )}
                    {command.source === 'saved' ? (
                      <Button
                        shape="square"
                        size="sm"
                        variant="ghost"
                        icon={<Edit className="h-3.5 w-3.5" />}
                        aria-label="编辑命令"
                        title="编辑命令"
                        onClick={() => startEdit(command)}
                      />
                    ) : (
                      <Button
                        shape="square"
                        size="sm"
                        variant="ghost"
                        className="opacity-0"
                        icon={<Edit className="h-3.5 w-3.5" />}
                        aria-label="默认命令不可编辑"
                        tabIndex={-1}
                        disabled
                      />
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <Dialog.Root open={managerOpen} onOpenChange={setManagerOpen}>
        <Dialog size="lg" className="flex max-h-[calc(100dvh-1rem)] !w-[min(48rem,calc(100vw-2rem))] !max-w-[min(48rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
            <Dialog.Title className="text-sm font-semibold text-kumo-strong">{editing ? '编辑命令片段' : '新增命令片段'}</Dialog.Title>
            <Dialog.Close />
          </div>
          <div className="space-y-3 overflow-y-auto p-4">
            <Input size="sm" label="名称" value={form.title} onChange={event => setForm(prev => ({ ...prev, title: event.target.value }))} />
            <div className="space-y-1.5">
              <Input size="sm" label="分类" value={form.category} onChange={event => setForm(prev => ({ ...prev, category: event.target.value }))} />
              {categoryOptions.length > 0 && (
                <div className="flex max-h-24 flex-wrap gap-1 overflow-y-auto px-1 py-1 pr-2">
                  {categoryOptions.map(option => (
                    <Button
                      key={option}
                      type="button"
                      size="sm"
                      variant="secondary"
                      className={`h-7 max-w-full px-2 text-[11px] ${
                        normalizeCategory(form.category) === option
                          ? 'border-kumo-interact bg-kumo-recessed text-kumo-strong'
                          : 'text-kumo-default'
                      }`}
                      onClick={() => setForm(prev => ({ ...prev, category: option }))}
                    >
                      <span className="truncate">{option}</span>
                    </Button>
                  ))}
                </div>
              )}
            </div>
            <CodeEditor label="命令" language="shell" value={form.content} onChange={content => setForm(prev => ({ ...prev, content }))} minHeight="8rem" />
            <Input size="sm" label="说明" value={form.description} onChange={event => setForm(prev => ({ ...prev, description: event.target.value }))} />
            <Button size="sm" variant={form.favorite ? 'primary' : 'secondary'} icon={<Star className="h-3.5 w-3.5" />} onClick={() => setForm(prev => ({ ...prev, favorite: !prev.favorite }))}>
              {form.favorite ? '已收藏' : '收藏'}
            </Button>
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
            {editing?.id ? <Button size="sm" variant={isArmed('snippet-remove') ? 'destructive' : 'secondary-destructive'} icon={<Trash className="h-3.5 w-3.5" />} onClick={() => removeSnippet(editing)}>删除</Button> : null}
            <Button size="sm" variant="secondary" onClick={() => setManagerOpen(false)}>取消</Button>
            <Button size="sm" variant="primary" icon={<Save className="h-3.5 w-3.5" />} onClick={saveSnippet}>保存</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={historyOpen} onOpenChange={setHistoryOpen}>
        <Dialog size="lg" className="flex max-h-[calc(100dvh-1rem)] !w-[min(48rem,calc(100vw-2rem))] !max-w-[min(48rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
            <Dialog.Title className="text-sm font-semibold text-kumo-strong">命令执行历史</Dialog.Title>
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
                  <div key={item.id} className="flex min-w-0 items-center gap-3 border-b border-kumo-line bg-kumo-base p-2 last:border-b-0">
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-mono text-[12px] font-semibold leading-5 text-kumo-strong" title={item.rendered_command}>{item.rendered_command}</div>
                      <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] font-medium leading-4 text-kumo-default">
                        <span>{item.server_name || '未指定主机'}</span>
                        <span>{item.execution_mode || 'terminal'}</span>
                        <span>{item.created_at || '-'}</span>
                      </div>
                    </div>
                    <div className="flex w-16 shrink-0 items-center justify-end gap-1 self-center">
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
