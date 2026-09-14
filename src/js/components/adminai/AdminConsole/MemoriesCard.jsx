import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Select } from '@cloudflare/kumo/components/select';
import { Empty, Loader } from '@cloudflare/kumo';
import { SectionCard } from '../../ui/AppPrimitives.jsx';
import { toast } from '../../../modules/toast.js';
import { useConfirmPress } from '../../../hooks/useConfirmPress.js';
import { Brain, Search, Plus, Edit, Trash, X } from '../../Icons.jsx';
import ErrorBanner from './ErrorBanner.jsx';

/* ==================== 长期记忆管理 ==================== */

const IMPORTANCE_OPTIONS = Array.from({ length: 10 }, (_, i) => ({ value: String(i + 1), label: String(i + 1) }));

export function MemoriesCard() {
  const [items, setItems] = useState(null);
  const [loading, setLoading] = useState(true);
  const [q, setQ] = useState('');
  const [adding, setAdding] = useState(false);
  const [newContent, setNewContent] = useState('');
  const [newImportance, setNewImportance] = useState('5');
  const [newTriggers, setNewTriggers] = useState('');
  const [editingId, setEditingId] = useState('');
  const [editContent, setEditContent] = useState('');
  const [editImportance, setEditImportance] = useState('5');
  const [editTriggers, setEditTriggers] = useState('');
  const { isArmed: memIsArmed, confirmPress: memConfirmPress } = useConfirmPress();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const memHoverRef = useRef(false);

  const load = useCallback(async (keyword = '') => {
    setLoading(true);
    try {
      const res = await fetch(`/api/admin-ai/memories?q=${encodeURIComponent(keyword)}`);
      const data = await res.json();
      const body = data.data || data;
      setItems(Array.isArray(body.items) ? body.items : []);
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load('');
  }, [load]);

  // 搜索防抖：停止输入 250ms 后触发检索，避免每击键一次请求
  useEffect(() => {
    const t = window.setTimeout(() => load(q.trim()), 250);
    return () => window.clearTimeout(t);
  }, [q, load]);

  const handleAdd = async () => {
    const content = newContent.trim();
    if (!content) return;
    setSaving(true);
    setError('');
    try {
      const res = await fetch('/api/admin-ai/memories', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content, importance: Number(newImportance), triggers: newTriggers.trim() }),
      });
      const data = await res.json();
      if (!res.ok || (data.data || data).error) {
        setError((data.data || data).error || '保存失败');
        return;
      }
      setNewContent('');
      setNewTriggers('');
      setNewImportance('5');
      setAdding(false);
      await load(q);
    } catch {
      setError('保存失败');
    } finally {
      setSaving(false);
    }
  };

  const startEdit = (item) => {
    setEditingId(item.id);
    setEditContent(item.content);
    setEditImportance(String(item.importance || 5));
    setEditTriggers(item.triggers || '');
  };

  const saveEdit = async (id) => {
    const content = editContent.trim();
    if (!content) return;
    setSaving(true);
    setError('');
    try {
      const res = await fetch(`/api/admin-ai/memories/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content, importance: Number(editImportance), triggers: editTriggers.trim() }),
      });
      const data = await res.json();
      if (!res.ok || (data.data || data).error) {
        setError((data.data || data).error || '保存失败');
        return;
      }
      setEditingId('');
      await load(q);
    } catch {
      setError('保存失败');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (item) => {
    if (!memConfirmPress(`adminai-memory:${item.id}`, '删除记忆条目')) return;
    try {
      await fetch(`/api/admin-ai/memories/${item.id}`, { method: 'DELETE' });
      await load(q);
    } catch {
    }
  };

  const MEM_COLLAPSED_H = 48;

  const handleMemEnter = (e) => {
    memHoverRef.current = true;
    const wrap = e.currentTarget.querySelector('[data-mem-wrap]');
    const inner = e.currentTarget.querySelector('[data-mem-content]');
    if (!wrap || !inner) return;
    inner.classList.remove('line-clamp-2');
    const full = wrap.scrollHeight;
    if (full <= MEM_COLLAPSED_H) {
      inner.classList.add('line-clamp-2');
      return;
    }
    wrap.style.maxHeight = `${full}px`;
  };

  const handleMemLeave = (e) => {
    memHoverRef.current = false;
    const wrap = e.currentTarget.querySelector('[data-mem-wrap]');
    const inner = e.currentTarget.querySelector('[data-mem-content]');
    if (!wrap || !inner) return;
    wrap.style.maxHeight = `${MEM_COLLAPSED_H}px`;
    const onEnd = (ev) => {
      if (ev.propertyName !== 'max-height') return;
      wrap.removeEventListener('transitionend', onEnd);
      if (memHoverRef.current) return;
      inner.classList.add('line-clamp-2');
    };
    wrap.addEventListener('transitionend', onEnd);
  };

  return (
    <div className="space-y-4 pb-4">
      <SectionCard
        icon={<Brain className="h-4 w-4 text-brand" />}
        title="长期记忆"
        description="在对话中说「记住…」让 AI 写入"
        bodyPadding="none"
      >
        <div className="flex items-center gap-2 border-b border-kumo-line px-4 py-3">
          <div className="relative flex-1">
            <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-kumo-subtle" />
            <Input
              size="sm"
              className="w-full pl-8"
              placeholder="搜索记忆"
              aria-label="搜索记忆"
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </div>
          <Button size="sm" onClick={() => setAdding((v) => !v)}>
            <Plus className="mr-1 h-3.5 w-3.5" />
            新增
          </Button>
        </div>

        {error && (
          <div className="border-b border-kumo-line px-4 py-3">
            <ErrorBanner message={error} />
          </div>
        )}

        {adding && (
          <div className="border-b border-kumo-line px-4 py-3">
            <div className="space-y-2.5 rounded-lg border border-kumo-line bg-kumo-recessed/40 p-3.5">
              <Textarea
                rows={2}
                value={newContent}
                onChange={(e) => setNewContent(e.target.value)}
                placeholder="记忆内容，一句话表述，具体到名称/ID/取值"
                className="w-full"
              />
              <div className="flex flex-wrap items-center gap-2">
                <Select alignItemWithTrigger
                  size="sm"
                  className="w-24"
                  value={newImportance}
                  onValueChange={setNewImportance}
                  items={IMPORTANCE_OPTIONS}
                />
                <Input
                  size="sm"
                  className="min-w-40 flex-1"
                  value={newTriggers}
                  aria-label="触发词"
                  onChange={(e) => setNewTriggers(e.target.value)}
                  placeholder="触发词（逗号分隔，选填）"
                />
                <div className="ml-auto flex items-center gap-2">
                  <Button size="sm" variant="primary" onClick={handleAdd} disabled={saving || !newContent.trim()}>
                    {saving ? '保存中...' : '保存'}
                  </Button>
                  <Button size="sm" onClick={() => setAdding(false)}>
                    <X className="mr-1 h-3.5 w-3.5" />
                    取消
                  </Button>
                </div>
              </div>
            </div>
          </div>
        )}

        {loading && items === null ? (
          <div className="flex justify-center py-10">
            <Loader size={20} className="text-kumo-subtle" />
          </div>
        ) : items.length === 0 ? (
          <Empty
            className="py-10"
            icon={<Brain className="h-5 w-5" />}
            title={q ? '没有匹配的记忆' : '还没有长期记忆'}
            description={q ? '换个关键词试试' : '在对话中说「记住…」，或点「新增」记录一条'}
          />
        ) : (
          <div>
            {items.map((item) => (
              <div
                key={item.id}
                className="group border-b border-kumo-line px-4 py-3 last:border-b-0"
                onMouseEnter={editingId === item.id ? undefined : handleMemEnter}
                onMouseLeave={editingId === item.id ? undefined : handleMemLeave}
              >
                {editingId === item.id ? (
                  <div className="space-y-2.5">
                    <Textarea
                      rows={2}
                      value={editContent}
                      onChange={(e) => setEditContent(e.target.value)}
                      className="w-full"
                    />
                    <div className="flex flex-wrap items-center gap-2">
                      <Select alignItemWithTrigger
                        size="sm"
                        className="w-24"
                        value={editImportance}
                        onValueChange={setEditImportance}
                        items={IMPORTANCE_OPTIONS}
                      />
                      <Input
                        size="sm"
                        className="min-w-40 flex-1"
                        value={editTriggers}
                        aria-label="触发词"
                        onChange={(e) => setEditTriggers(e.target.value)}
                        placeholder="触发词（逗号分隔，选填）"
                      />
                      <div className="ml-auto flex items-center gap-2">
                        <Button size="sm" variant="primary" onClick={() => saveEdit(item.id)} disabled={saving || !editContent.trim()}>
                          {saving ? '保存中...' : '保存'}
                        </Button>
                        <Button size="sm" onClick={() => setEditingId('')}>
                          <X className="mr-1 h-3.5 w-3.5" />
                          取消
                        </Button>
                      </div>
                    </div>
                  </div>
                ) : (
                  <div className="flex items-center gap-2">
                    <div className="min-w-0 flex-1">
                      <div className="mb-1 flex flex-wrap items-center gap-2 text-xs">
                        <Badge variant={item.importance >= 8 ? 'red' : 'neutral'}>{item.importance}</Badge>
                        <Badge variant="blue">{new Date(item.createdAt).toLocaleDateString()}</Badge>
                        <Badge variant={item.source === 'auto' ? 'orange' : 'teal'}>
                          {item.source === 'auto' ? '自动提炼' : 'AI 记录'}
                        </Badge>
                      </div>
                      <div
                        data-mem-wrap
                        className="max-h-12 overflow-hidden transition-[max-height] duration-300 ease-out"
                      >
                        <div data-mem-content className="line-clamp-2 text-sm leading-relaxed text-kumo-strong">
                          {item.content}
                        </div>
                      </div>
                      {item.triggers && (
                        <div className="mt-1 flex flex-wrap items-center gap-2 text-xs">
                          <Badge variant="outline" className="gap-1.5">
                            {item.triggers
                              .split(',')
                              .map((t) => t.trim())
                              .filter(Boolean)
                              .map((t, i) => (
                                <React.Fragment key={i}>
                                  {i > 0 && <span className="h-3 w-px bg-kumo-line" aria-hidden />}
                                  <span>{t}</span>
                                </React.Fragment>
                              ))}
                          </Badge>
                        </div>
                      )}
                    </div>
                    <div className="flex shrink-0 flex-col items-center justify-center gap-1">
                      <Button size="sm" variant="secondary" className="!px-2" onClick={() => startEdit(item)} title="编辑">
                        <Edit className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        size="sm"
                        variant={memIsArmed(`adminai-memory:${item.id}`) ? 'destructive' : 'secondary'}
                        className="!px-2"
                        onClick={() => handleDelete(item)}
                      >
                        <Trash className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </SectionCard>
    </div>
  );
}

export default MemoriesCard;

