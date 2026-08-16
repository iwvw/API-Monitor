/* ==================== 管理 AI 消息状态机 ====================
 * 把 AskAiPanel 里散落在 SSE 监听器中的原地 mutate 收敛为纯函数状态机：
 * - 消息生命周期：pending → streaming → completed / error / cancelled
 * - 事件通过 applyAiEvent(messages, event, targetId) 应用，targetId 隔离旧流
 * - 所有更新均为不可变操作（浅拷贝），便于 React 渲染与单测
 */

export const MSG = {
  IDLE: 'idle', // 历史加载/普通消息
  PENDING: 'pending', // 占位已创建，等待 run 首个事件
  STREAMING: 'streaming', // 已收到首个流事件
  COMPLETED: 'completed',
  CANCELLED: 'cancelled',
  ERROR: 'error',
};

export const STEP = {
  RUNNING: 'running',
  SUCCESS: 'success',
  FAILED: 'failed',
};

export const APPROVAL = {
  PENDING: 'pending',
  APPROVED: 'approved',
  REJECTED: 'rejected',
};

// SSE 自定义事件名（与后端 SSEEvent.Type 对齐）。
export const STREAM_EVENTS = [
  'reasoning',
  'reasoning_summary',
  'delta',
  'tool_start',
  'tool_result',
  'approval_required',
  'error',
  'done',
  'session_title',
];

/* ---------- 构造器 ---------- */

export function createUserMessage(id, content) {
  return { id, role: 'user', content: content || '', status: MSG.IDLE };
}

export function createAssistantMessage(id, runId = null, status = MSG.PENDING) {
  return {
    id,
    role: 'assistant',
    content: '',
    reasoning: '',
    reasoningSummary: '',
    thinking: [],
    blocks: [],
    status,
    runId,
    active: true, // active 的消息才会接收流事件
  };
}

/* ---------- 事件规范化（raw = parseAdminAiEvent 的产物） ---------- */

export function normalizeAiEvent(raw) {
  if (!raw || !raw.type) return null;
  switch (raw.type) {
    case 'reasoning':
      return { type: 'reasoning', text: raw.text || '' };
    case 'reasoning_summary':
      return { type: 'reasoning_summary', text: raw.text || '' };
    case 'delta':
      return { type: 'delta', text: raw.text || '' };
    case 'tool_start':
      return { type: 'tool_start', toolName: raw.toolName || '', args: raw.args, desc: raw.desc || '' };
    case 'tool_result':
      return {
        type: 'tool_result',
        toolName: raw.toolName || '',
        status: raw.status === 'success' ? STEP.SUCCESS : STEP.FAILED,
        error: raw.error || '',
      };
    case 'approval_required':
      return {
        type: 'approval',
        approvalId: raw.approvalId,
        planSummary: raw.planSummary || '',
        expiresAt: raw.expiresAt || '',
        method: raw.method || 'GET',
        path: raw.path || '',
        bodySnapshot: raw.bodySnapshot || '',
      };
    case 'error':
      return { type: 'error', message: raw.message || '发生错误', userMessageId: raw.userMessageId || '' };
    case 'done':
      return { type: 'done', userMessageId: raw.userMessageId || '' };
    case 'session_title':
      return { type: 'session_title', sessionId: raw.sessionId || '', title: raw.title || '' };
    default:
      return null;
  }
}

/* ---------- 目标定位（防串流污染：只更新 active 的目标消息） ---------- */

function findTarget(messages, targetId) {
  if (!targetId) return -1;
  return messages.findIndex((m) => m.role === 'assistant' && m.id === targetId && m.active);
}

function cloneAt(messages, idx) {
  const next = messages.slice();
  next[idx] = { ...next[idx] };
  return next;
}

function activate(msg) {
  return msg.status === MSG.PENDING ? { ...msg, status: MSG.STREAMING } : msg;
}

/* ---------- 块操作 ---------- */

function appendTextBlock(msg, text) {
  const blocks = msg.blocks || [];
  const last = blocks[blocks.length - 1];
  if (last && last.type === 'text') {
    const updated = blocks.slice();
    updated[updated.length - 1] = { ...last, text: (last.text || '') + text };
    return { ...msg, blocks: updated };
  }
  return { ...msg, blocks: [...blocks, { type: 'text', text }] };
}

function pushBlock(msg, block) {
  return { ...msg, blocks: [...(msg.blocks || []), block] };
}

function pushThinkingStep(msg, step) {
  return { ...msg, thinking: [...(msg.thinking || []), step] };
}

function resolveToolStep(msg, event) {
  const steps = (msg.thinking || []).slice();
  const idx = steps.findIndex((s) => s.type === 'tool_call' && s.toolName === event.toolName && s.status === STEP.RUNNING);
  if (idx < 0) return msg; // 无匹配运行步骤（幂等）
  steps[idx] = { ...steps[idx], status: event.status, error: event.error || '' };
  return { ...msg, thinking: steps };
}

/* ---------- 主 reducer ---------- */

export function applyAiEvent(messages, event, targetId) {
  if (!event) return messages;
  const idx = findTarget(messages, targetId);
  if (idx < 0) return messages;

  let msg = cloneAt(messages, idx)[idx];
  switch (event.type) {
    case 'reasoning':
      msg = { ...msg, reasoning: (msg.reasoning || '') + event.text };
      break;
    case 'reasoning_summary':
      msg = { ...msg, reasoningSummary: event.text };
      break;
    case 'delta':
      msg = appendTextBlock(msg, event.text);
      break;
    case 'tool_start':
      msg = pushThinkingStep(msg, {
        type: 'tool_call',
        toolName: event.toolName,
        args: event.args,
        desc: event.desc || '',
        status: STEP.RUNNING,
      });
      break;
    case 'tool_result':
      msg = resolveToolStep(msg, event);
      break;
    case 'approval':
      msg = pushBlock(msg, {
        type: 'approval',
        approvalId: event.approvalId,
        planSummary: event.planSummary,
        expiresAt: event.expiresAt,
        method: event.method,
        path: event.path,
        bodySnapshot: event.bodySnapshot,
        status: APPROVAL.PENDING,
      });
      break;
    case 'error':
      msg = {
        ...pushBlock(msg, { type: 'error', message: event.message, retryable: true, retryPrompt: event.retryPrompt || '' }),
        status: MSG.ERROR,
        active: false,
      };
      break;
    case 'done':
      msg = { ...msg, status: MSG.COMPLETED, active: false };
      break;
    default:
      return messages;
  }
  msg = event.type === 'done' || event.type === 'error' ? msg : activate(msg);

  const next = messages.slice();
  next[idx] = msg;
  // done/error 事件携带本轮用户消息的服务端 id（aam_…），记到其前一条用户消息上，
  // 编辑重发时用它做服务端截断（删旧消息及其后所有消息）
  if (event.userMessageId) {
    for (let i = idx - 1; i >= 0; i--) {
      if (messages[i].role === 'user') {
        next[i] = { ...messages[i], dbId: event.userMessageId };
        break;
      }
    }
  }
  return next;
}

/* ---------- 会话级/目标级操作 ---------- */

// POST 失败：把目标占位转为错误块（含重试 prompt），不留下空气泡。
export function failMessage(messages, targetId, message, retryPrompt) {
  const idx = findTarget(messages, targetId);
  if (idx < 0) return messages;
  const next = messages.slice();
  next[idx] = {
    ...next[idx],
    status: MSG.ERROR,
    active: false,
    blocks: [{ type: 'error', message: message || '发送失败，请重试', retryable: true, retryPrompt: retryPrompt || '' }],
  };
  return next;
}

// 取消：无任何输出时移除占位；有输出则标记 cancelled。
export function cancelMessage(messages, targetId) {
  const idx = findTarget(messages, targetId);
  if (idx < 0) return messages;
  const msg = messages[idx];
  const hasOutput = (msg.reasoning && msg.reasoning.length > 0)
    || (msg.thinking && msg.thinking.length > 0)
    || (msg.blocks && msg.blocks.length > 0);
  if (!hasOutput) {
    return messages.filter((m) => m.id !== targetId);
  }
  const next = messages.slice();
  next[idx] = { ...msg, status: MSG.CANCELLED, active: false };
  return next;
}

// 审批回调：更新 approval 块状态。
export function resolveApprovalBlock(messages, approvalId, action) {
  const status = action === 'approve' ? APPROVAL.APPROVED : APPROVAL.REJECTED;
  return messages.map((m) => {
    if (!m.blocks || !m.blocks.some((b) => b.type === 'approval' && b.approvalId === approvalId)) return m;
    return {
      ...m,
      blocks: m.blocks.map((b) => (b.type === 'approval' && b.approvalId === approvalId ? { ...b, status } : b)),
    };
  });
}

/* ---------- 查询辅助 ---------- */

export function isStreaming(status) {
  return status === MSG.PENDING || status === MSG.STREAMING;
}

// 汇总助手消息的可复制文本（text 块 + 历史 content）。
export function collectAssistantText(msg) {
  const textParts = (msg.blocks || []).filter((b) => b.type === 'text').map((b) => b.text || '');
  if (msg.content) textParts.unshift(msg.content);
  return textParts.join('\n');
}
