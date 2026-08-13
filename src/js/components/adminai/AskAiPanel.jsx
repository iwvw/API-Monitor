import React, { useCallback, useEffect, useRef, useState } from 'react';
import useStore from '../../store.js';
import { Sidebar } from '@cloudflare/kumo/components/sidebar';
import { Button } from '@cloudflare/kumo/components/button';
import { Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { DeleteResource } from '@cloudflare/kumo';
import { Bot, X, Send, Plus, Trash } from '../Icons.jsx';
import MessageList from './MessageList.jsx';
import { parseAdminAiEvent } from '../../modules/adminAiEvents.js';

export default function AskAiPanel() {
  const setShowAskAI = useStore((s) => s.setShowAskAI);

  const [sessions, setSessions] = useState([]);
  const [activeSessionId, setActiveSessionId] = useState(null);
  const [messages, setMessages] = useState([]);
  const [input, setInput] = useState('');
  const [streaming, setStreaming] = useState(false);
  const [runId, setRunId] = useState(null);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const eventSource = useRef(null);

  const loadSessions = useCallback(async () => {
    setSessionsLoading(true);
    try {
      const res = await fetch('/api/admin-ai/sessions');
      const data = await res.json();
      const list = data.sessions || data.data || [];
      setSessions(list);
      if (list.length > 0 && !activeSessionId) {
        setActiveSessionId(list[0].id);
      }
    } catch {
    } finally {
      setSessionsLoading(false);
    }
  }, [activeSessionId]);

  const loadMessages = useCallback(async (sessionId) => {
    if (!sessionId) return;
    try {
      const res = await fetch(`/api/admin-ai/sessions/${sessionId}/messages`);
      const data = await res.json();
      setMessages(data.messages || data.data || []);
    } catch {
    }
  }, []);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  useEffect(() => {
    if (activeSessionId) {
      loadMessages(activeSessionId);
    }
  }, [activeSessionId, loadMessages]);

  const stopStream = useCallback(() => {
    if (eventSource.current) {
      eventSource.current.close();
      eventSource.current = null;
    }
    setStreaming(false);
    setRunId(null);
  }, []);

  useEffect(() => {
    return () => {
      stopStream();
    };
  }, [stopStream]);

  const startStream = (newRunId) => {
    stopStream();
    setRunId(newRunId);
    setStreaming(true);

    const es = new EventSource(`/api/admin-ai/messages/stream?runId=${newRunId}`);
    eventSource.current = es;

    es.addEventListener('delta', (e) => {
      try {
        const event = parseAdminAiEvent('delta', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          const last = updated[updated.length - 1];
          if (last && last.role === 'assistant') {
            const lastBlock = last.blocks ? last.blocks[last.blocks.length - 1] : null;
            if (lastBlock && lastBlock.type === 'text') {
              lastBlock.text = (lastBlock.text || '') + (event.delta || '');
            } else {
              if (!last.blocks) last.blocks = [];
              last.blocks.push({ type: 'text', text: event.delta || '' });
            }
          } else {
            updated.push({
              id: `msg_${Date.now()}`,
              role: 'assistant',
              blocks: [{ type: 'text', text: event.delta || '' }],
            });
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('tool_start', (e) => {
      try {
        const event = parseAdminAiEvent('tool_start', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          const last = updated[updated.length - 1];
          if (last && last.role === 'assistant') {
            if (!last.blocks) last.blocks = [];
            last.blocks.push({
              type: 'tool_call',
              toolName: event.toolName,
              args: event.args,
              status: 'running',
            });
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('tool_result', (e) => {
      try {
        const event = parseAdminAiEvent('tool_result', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          for (const msg of updated) {
            if (msg.role === 'assistant' && msg.blocks) {
              for (const block of msg.blocks) {
                if (block.type === 'tool_call' && block.toolName === event.toolName) {
                  block.status = event.status === 'success' ? 'success' : 'failed';
                  if (event.error) block.error = event.error;
                  return updated;
                }
              }
            }
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('approval_required', (e) => {
      try {
        const event = parseAdminAiEvent('approval_required', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          const last = updated[updated.length - 1];
          if (last && last.role === 'assistant') {
            if (!last.blocks) last.blocks = [];
            last.blocks.push({ type: 'approval', ...event });
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('error', (e) => {
      try {
        const event = parseAdminAiEvent('error', e.data);
        setMessages((prev) => {
          const updated = [...prev];
          const last = updated[updated.length - 1];
          if (last && last.role === 'assistant') {
            if (!last.blocks) last.blocks = [];
            last.blocks.push({ type: 'error', message: event.message || '发生错误', retryable: true });
          }
          return updated;
        });
      } catch {
      }
    });

    es.addEventListener('done', () => {
      stopStream();
    });
  };

  const handleSend = async () => {
    const trimmed = input.trim();
    if (!trimmed || !activeSessionId || streaming) return;

    setInput('');
    let sessionId = activeSessionId;

    if (!sessions.find((s) => s.id === sessionId)) {
      try {
        const res = await fetch('/api/admin-ai/sessions', { method: 'POST' });
        const data = await res.json();
        sessionId = data.id || data.session?.id;
        setActiveSessionId(sessionId);
        setSessions((prev) => [...prev, { id: sessionId, title: new Date().toLocaleString('zh-CN') }]);
      } catch {
        return;
      }
    }

    setMessages((prev) => [...prev, { id: `user_${Date.now()}`, role: 'user', content: trimmed }]);

    try {
      const res = await fetch('/api/admin-ai/messages', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId, prompt: trimmed }),
      });
      const data = await res.json();
      if (data.runId) {
        startStream(data.runId);
      }
    } catch {
      setMessages((prev) => [...prev, {
        id: `err_${Date.now()}`,
        role: 'assistant',
        blocks: [{ type: 'error', message: '发送失败，请重试', retryable: true }],
      }]);
    }
  };

  const handleCancel = async () => {
    if (runId) {
      try {
        await fetch('/api/admin-ai/cancel', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ runId }),
        });
      } catch {
      }
    }
    stopStream();
  };

  const handleNewSession = async () => {
    try {
      const res = await fetch('/api/admin-ai/sessions', { method: 'POST' });
      const data = await res.json();
      const newSession = { id: data.id || data.session?.id, title: new Date().toLocaleString('zh-CN') };
      setSessions((prev) => [newSession, ...prev]);
      setActiveSessionId(newSession.id);
      setMessages([]);
    } catch {
    }
  };

  const handleDeleteSession = async (sessionId) => {
    try {
      await fetch(`/api/admin-ai/sessions/${sessionId}`, { method: 'DELETE' });
      setSessions((prev) => prev.filter((s) => s.id !== sessionId));
      if (activeSessionId === sessionId) {
        setActiveSessionId(sessions.find((s) => s.id !== sessionId)?.id || null);
        setMessages([]);
      }
      setDeleteDialogOpen(false);
    } catch {
    }
  };

  const handleResolveApproval = async (approvalId, action) => {
    try {
      await fetch(`/api/admin-ai/approvals/${approvalId}/resolve`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action }),
      });
      setMessages((prev) => {
        const updated = [...prev];
        for (const msg of updated) {
          if (msg.blocks) {
            for (const block of msg.blocks) {
              if (block.type === 'approval' && block.id === approvalId) {
                block.status = action === 'approve' ? 'approved' : 'rejected';
                return updated;
              }
            }
          }
        }
        return updated;
      });
    } catch {
    }
  };

  const handleKeyDown = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const sessionOptions = sessions.map((s) => ({
    value: s.id,
    label: s.title || new Date(s.created_at || s.id).toLocaleString('zh-CN'),
  }));
  const activeSessionTitle = sessionOptions.find((o) => o.value === activeSessionId)?.label || '当前会话';

  return (
    <Sidebar>
      <Sidebar.Header className="flex h-[58px]! shrink-0 items-center justify-between border-b border-kumo-line px-3">
        <div className="flex items-center gap-2">
          <Bot className="h-5 w-5 text-kumo-brand" />
          <span className="text-sm font-bold text-kumo-strong">管理 AI</span>
        </div>
        <div className="flex items-center gap-1">
          <Select
            size="sm"
            className="min-w-[110px]"
            aria-label="切换会话"
            value={activeSessionId || ''}
            items={sessionOptions}
            disabled={sessionsLoading}
            onValueChange={(val) => {
              setActiveSessionId(val);
              setMessages([]);
              if (val) loadMessages(val);
            }}
          />
          <Button
            size="sm"
            variant="ghost"
            shape="square"
            icon={<Plus className="h-4 w-4" />}
            onClick={handleNewSession}
            aria-label="新建会话"
          />
          <Button
            size="sm"
            variant="ghost"
            shape="square"
            icon={<Trash className="h-4 w-4" />}
            onClick={() => setDeleteDialogOpen(true)}
            aria-label="删除会话"
          />
          <DeleteResource
            open={deleteDialogOpen}
            onOpenChange={setDeleteDialogOpen}
            resourceType="会话"
            resourceName={activeSessionTitle}
            onDelete={() => handleDeleteSession(activeSessionId)}
          />
          <Button
            size="sm"
            variant="ghost"
            shape="square"
            icon={<X className="h-4 w-4" />}
            onClick={() => setShowAskAI(false)}
            aria-label="关闭"
          />
        </div>
      </Sidebar.Header>

      <Sidebar.Content>
        <MessageList
          messages={messages}
          onResolveApproval={handleResolveApproval}
          onRetry={handleSend}
        />
      </Sidebar.Content>

      <Sidebar.Footer className="border-t border-kumo-line px-3 py-3">
        <div className="flex items-end gap-2">
          <Textarea
            className="flex-1"
            placeholder="输入问题..."
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            rows={1}
          />
          {streaming ? (
            <Button
              size="sm"
              variant="secondary"
              onClick={handleCancel}
            >
              取消
            </Button>
          ) : (
            <Button
              size="sm"
              variant="primary"
              shape="square"
              icon={<Send className="h-4 w-4" />}
              onClick={handleSend}
              disabled={!input.trim() || !activeSessionId}
              aria-label="发送"
            />
          )}
        </div>
      </Sidebar.Footer>
    </Sidebar>
  );
}