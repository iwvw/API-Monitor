/* ==================== 管理 AI 消息状态机 ====================
 * 消息模型：assistant 消息持有按时间序排列的 parts（timeline，对齐 opencode 的
 * part 渲染）：reasoning / tool_call / tool_result / text / approval / error。
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
  ERROR: 'error', // 决议失败（过期/已处理/执行已结束），保留原因供展示
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
  'retry',
];

/* ---------- 构造器 ---------- */

export function createUserMessage(id, content) {
  return { id, role: 'user', content: content || '', status: MSG.IDLE };
}

export function createAssistantMessage(id, runId = null, status = MSG.PENDING) {
  return {
    id,
    role: 'assistant',
    parts: [],
    status,
    runId,
    active: true, // active 的消息才会接收流事件
    roundUserMsgId: '', // 本轮归属的服务端 user 消息 id：追问入队后事件按它分段
  };
}

/* ---------- 事件规范化（raw = parseAdminAiEvent 的产物） ---------- */

export function normalizeAiEvent(raw) {
  if (!raw || !raw.type) return null;
  switch (raw.type) {
    case 'reasoning':
      return { type: 'reasoning', text: raw.text || '', userMessageId: raw.userMessageId || '' };
    case 'reasoning_summary':
      return { type: 'reasoning_summary', text: raw.text || '' };
    case 'delta':
      return { type: 'delta', text: raw.text || '', userMessageId: raw.userMessageId || '' };
    case 'tool_start':
      return { type: 'tool_start', toolName: raw.toolName || '', toolCallId: raw.toolCallId || '', args: raw.args, desc: raw.desc || '', userMessageId: raw.userMessageId || '' };
    case 'tool_result':
      return {
        type: 'tool_result',
        toolName: raw.toolName || '',
        toolCallId: raw.toolCallId || '',
        status: raw.status === 'success' ? STEP.SUCCESS : STEP.FAILED,
        summary: raw.summary || '',
        error: raw.error || '',
        userMessageId: raw.userMessageId || '',
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
        userMessageId: raw.userMessageId || '',
      };
    case 'error':
      return { type: 'error', message: raw.message || '发生错误', userMessageId: raw.userMessageId || '' };
    case 'retry':
      return { type: 'retry', attempt: Number(raw.attempt || 0), total: Number(raw.total || 0), message: raw.message || '', userMessageId: raw.userMessageId || '' };
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
  next[idx] = { ...next[idx], parts: [...(next[idx].parts || [])] };
  return next;
}

function activate(msg) {
  return msg.status === MSG.PENDING ? { ...msg, status: MSG.STREAMING } : msg;
}

/* ---------- part 操作（时间序追加/合并） ---------- */

// 追加 part；若最后一个 part 与目标类型相同且满足合并条件则原地合并。
function appendPart(msg, part, merge = false) {
  const parts = msg.parts || [];
  const last = parts[parts.length - 1];
  if (merge && last && last.type === part.type) {
    const merged = { ...last };
    if (part.type === 'text') merged.text = (last.text || '') + (part.text || '');
    if (part.type === 'reasoning') merged.text = (last.text || '') + (part.text || '');
    const next = parts.slice();
    next[next.length - 1] = merged;
    return { ...msg, parts: next };
  }
  return { ...msg, parts: [...parts, part] };
}

// 更新最后一个匹配条件的 part（原地替换）。
function updateLastPart(msg, predicate, update) {
  const parts = msg.parts || [];
  let idx = -1;
  for (let i = parts.length - 1; i >= 0; i--) {
    if (predicate(parts[i])) {
      idx = i;
      break;
    }
  }
  if (idx < 0) return msg;
  const next = parts.slice();
  next[idx] = { ...next[idx], ...update };
  return { ...msg, parts: next };
}

// 更新第一个匹配条件的 part（原地替换）。
function updateFirstPart(msg, predicate, update) {
  const parts = msg.parts || [];
  let idx = -1;
  for (let i = 0; i < parts.length; i++) {
    if (predicate(parts[i])) {
      idx = i;
      break;
    }
  }
  if (idx < 0) return msg;
  const next = parts.slice();
  next[idx] = { ...next[idx], ...update };
  return { ...msg, parts: next };
}

/* ---------- 主 reducer ---------- */

// 轮次分段：后端 join 语义下，一次流里可能连续处理多轮追问（每轮事件带各自的
// userMessageId）。目标占位已完结或事件归属新一轮时，为其新建独立段消息，
// 避免多轮输出堆积在同一个气泡里（对齐 opencode 的逐轮 part 渲染）。
function locateSegment(messages, event, targetId) {
  let idx = findTarget(messages, targetId);
  if (idx >= 0) {
    const cur = messages[idx];
    const round = event.userMessageId;
    if (round && cur.roundUserMsgId && cur.roundUserMsgId !== round) {
      idx = -1; // 事件归属新一轮
    }
  }
  if (idx >= 0 || !event.userMessageId) return idx;
  const sid = `assistant_${event.userMessageId}`;
  const found = messages.findIndex((m) => m.role === 'assistant' && m.id === sid);
  if (found >= 0) return found;
  return -1;
}

export function applyAiEvent(messages, event, targetId) {
  if (!event) return messages;
  let idx = locateSegment(messages, event, targetId);
  if (idx < 0) {
    // 无匹配目标：只对带轮次归属的流事件落地（无 userMessageId 的离线事件不落）
    if (!event.userMessageId) return messages;
    idx = messages.length;
    messages = [...messages, createAssistantMessage(`assistant_${event.userMessageId}`, null, MSG.STREAMING)];
  }
  messages = cloneAt(messages, idx);
  let msg = messages[idx];
  if (event.userMessageId && !msg.roundUserMsgId) {
    msg.roundUserMsgId = event.userMessageId;
  }
  switch (event.type) {
    case 'reasoning':
      msg = appendPart(msg, { type: 'reasoning', text: event.text || '' }, true);
      break;
    case 'reasoning_summary':
      msg = updateLastPart(msg, (p) => p.type === 'reasoning', { summary: event.text });
      break;
    case 'delta':
      msg = appendPart(msg, { type: 'text', text: event.text || '' }, true);
      break;
    case 'tool_start':
      msg = appendPart(msg, {
        type: 'tool_call',
        toolName: event.toolName,
        toolCallId: event.toolCallId,
        args: event.args || '',
        desc: event.desc || '',
        status: STEP.RUNNING,
      });
      break;
    case 'tool_result':
      if (event.toolCallId) {
        msg = updateLastPart(msg, (p) => p.type === 'tool_call' && p.toolCallId === event.toolCallId, { status: event.status, error: event.error || '' });
      } else if (event.toolName) {
        msg = updateLastPart(msg, (p) => p.type === 'tool_call' && p.toolName === event.toolName && p.status === STEP.RUNNING && !p.toolCallId, { status: event.status, error: event.error || '' });
      } else {
        msg = updateFirstPart(msg, (p) => p.type === 'tool_call' && p.status === STEP.RUNNING && !p.toolCallId, { status: event.status, error: event.error || '' });
      }
      msg = appendPart(msg, {
        type: 'tool_result',
        toolName: event.toolName,
        toolCallId: event.toolCallId,
        summary: event.summary || '',
        status: event.status,
      });
      break;
    case 'approval':
      msg = appendPart(msg, {
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
    case 'retry': {
      // 上游瞬时故障重试提示：就地更新最近一条 notice，让次数在同一行滚动，避免逐次堆行
      const notice = { type: 'notice', text: `${event.message}（第 ${event.attempt}/${event.total || 10} 次）` };
      msg = (msg.parts || []).some((p) => p.type === 'notice')
        ? updateLastPart(msg, (p) => p.type === 'notice', notice)
        : appendPart(msg, notice);
      break;
    }
    case 'error':
      msg = appendPart(msg, { type: 'error', message: event.message, retryable: true, retryPrompt: event.retryPrompt || '' });
      msg = { ...msg, status: MSG.ERROR, active: false };
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
    parts: [{ type: 'error', message: message || '发送失败，请重试', retryable: true, retryPrompt: retryPrompt || '' }],
  };
  return next;
}

// 取消：无任何输出时移除占位；有输出则标记 cancelled。
// 定位不要求 active：手动中断时后端 error 事件可能已先落地（消息被置为终态
// active=false），取消语义仍应覆盖为「已停止」，避免中断被误显示为「出错了」。
export function cancelMessage(messages, targetId) {
  const idx = messages.findIndex((m) => m.role === 'assistant' && m.id === targetId);
  if (idx < 0) return messages;
  const msg = messages[idx];
  const hasOutput = (msg.parts && msg.parts.length > 0);
  if (!hasOutput) {
    return messages.filter((m) => m.id !== targetId);
  }
  const next = messages.slice();
  next[idx] = { ...msg, status: MSG.CANCELLED, active: false };
  return next;
}

// 审批回调：更新 approval part 状态。
export function resolveApprovalPart(messages, approvalId, action) {
  const status = action === 'approve' ? APPROVAL.APPROVED : APPROVAL.REJECTED;
  return messages.map((m) => {
    if (!m.parts || !m.parts.some((p) => p.type === 'approval' && p.approvalId === approvalId)) return m;
    return {
      ...m,
      parts: m.parts.map((p) => (p.type === 'approval' && p.approvalId === approvalId ? { ...p, status } : p)),
    };
  });
}

/* ---------- 历史恢复（DB 行 → timeline parts） ----------
 * 服务端按 (created_at, id) 落库：assistant 行可能携带 tool_call_meta（JSON 数组，
 * 含 desc）、reasoning_content；紧随其后的 tool 行是工具结果（tool_call_id 配对）；
 * 最后的 assistant 纯文本行是最终正文。映射为一条消息的按时间序 parts。 */
export function buildTimelineFromRows(rows) {
  const messages = [];
  let current = null; // 当前 assistant 消息（连续 assistant/tool 块合并）
  let toolIndex = 0; // 当前消息内 tool_call 计数（配对未带 id 的旧数据）
  for (const row of rows || []) {
    if (row.role === 'user') {
      const mentions = [];
      if (row.mentions) {
        try {
          const parsed = typeof row.mentions === 'string' ? JSON.parse(row.mentions) : row.mentions;
          if (Array.isArray(parsed)) mentions.push(...parsed);
        } catch {
        }
      }
      messages.push({ id: row.id, role: 'user', content: row.content || '', status: MSG.IDLE, ...(mentions.length > 0 ? { mentions } : {}) });
      current = null;
      continue;
    }
    if (row.role === 'assistant') {
      const reasoning = row.reasoning_content || '';
      let tcs = [];
      if (row.toolCallMeta) {
        try {
          const parsed = typeof row.toolCallMeta === 'string' ? JSON.parse(row.toolCallMeta) : row.toolCallMeta;
          tcs = Array.isArray(parsed) ? parsed : [parsed];
        } catch {
          tcs = [];
        }
      }
      if (!current) {
        current = { id: row.id, role: 'assistant', parts: [], status: MSG.IDLE, active: false, roundUserMsgId: '' };
        messages.push(current);
        toolIndex = 0;
      }
      if (reasoning) {
        current.parts.push({ type: 'reasoning', text: reasoning, summary: row.reasoning_summary || '' });
      }
      const desc = row.toolCallDesc || '';
      tcs.forEach((tc, idx) => {
        const id = tc.id || `tc_${toolIndex + idx}_${idx}`;
        current.parts.push({
          type: 'tool_call',
          toolName: tc.function?.name || tc.toolName || '未知工具',
          toolCallId: id,
          args: tc.function?.arguments || tc.args || '',
          desc: tc.desc || (idx === 0 ? desc : ''),
          status: STEP.SUCCESS,
        });
      });
      toolIndex += tcs.length;
      if (row.content) {
        current.parts.push({ type: 'text', text: row.content });
      }
      continue;
    }
    if (row.role === 'tool') {
      if (!current) continue; // 无前置 assistant 的孤儿 tool 行：忽略
      // 执行成败按后端落库的 tool_status 还原：失败时同步把最近的未配对
      // tool_call part 标红，避免「失败动作显示绿勾」
      const failed = row.toolStatus === 'error';
      if (failed) {
        for (let i = current.parts.length - 1; i >= 0; i--) {
          const p = current.parts[i];
          if (p.type === 'tool_call' && p.status === STEP.SUCCESS) {
            p.status = STEP.FAILED;
            break;
          }
        }
      }
      current.parts.push({
        type: 'tool_result',
        toolName: row.toolName || '',
        toolCallId: row.toolCallId || '',
        summary: row.content || '',
        status: failed ? STEP.FAILED : STEP.SUCCESS,
      });
    }
  }
  return messages;
}

/* ---------- 查询辅助 ---------- */

export function isStreaming(status) {
  return status === MSG.PENDING || status === MSG.STREAMING;
}

/* ---------- 外部 run live 标注 ----------
 * 外部来源（MCP/API/BOT/定时任务）没有 SSE 通道，消息靠轮询从 DB 重拉，
 * 加载时所有行都是终态快照（IDLE），无法体现「run 仍在执行」。
 * 后端在消息响应携带 activeRun 时，把最后一条助手消息标为 STREAMING：
 * - 末 part 恰为 tool_call 时置 RUNNING（该工具尚未落结果行）
 * - 其余 part（reasoning/text/tool_result）保持终态，动态由图标/胶囊层表达
 * 运行结束后 activeRun 消失，下一次重拉自然恢复静态。 */
export function markLiveMessage(messages, live) {
  if (!live || !live.runId) return messages;
  const next = messages.map((m) => ({ ...m, parts: (m.parts || []).map((p) => ({ ...p })) }));
  for (let i = next.length - 1; i >= 0; i--) {
    const msg = next[i];
    if (msg.role !== 'assistant') continue;
    msg.status = MSG.STREAMING;
    const parts = msg.parts || [];
    if (parts.length > 0 && parts[parts.length - 1].type === 'tool_call') {
      parts[parts.length - 1].status = STEP.RUNNING;
    }
    break;
  }
  return next;
}

// 汇总助手消息的可复制文本（text parts 全文）。
export function collectAssistantText(msg) {
  const textParts = (msg.parts || []).filter((p) => p.type === 'text').map((p) => p.text || '');
  return textParts.join('\n');
}

// 消息是否只有终态文本/工具输出之外的内容（用于 pending 骨架判断）。
export function hasVisibleParts(msg) {
  return !!(msg.parts && msg.parts.length > 0);
}
