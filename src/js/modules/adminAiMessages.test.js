import { describe, it, expect } from 'vitest';
import {
  MSG,
  STEP,
  APPROVAL,
  STREAM_EVENTS,
  createUserMessage,
  createAssistantMessage,
  normalizeAiEvent,
  applyAiEvent,
  failMessage,
  cancelMessage,
  resolveApprovalBlock,
  isStreaming,
  collectAssistantText,
} from './adminAiMessages.js';

const tgt = () => createAssistantMessage('a1', 'run1');

function withTarget(...msgs) {
  return [createUserMessage('u1', 'hi'), tgt(), ...msgs];
}

describe('normalizeAiEvent', () => {
  it('reasoning/delta 规范化', () => {
    expect(normalizeAiEvent({ type: 'reasoning', text: '想' })).toEqual({ type: 'reasoning', text: '想' });
    expect(normalizeAiEvent({ type: 'delta', text: '回答' })).toEqual({ type: 'delta', text: '回答' });
    expect(normalizeAiEvent({ type: 'delta' })).toEqual({ type: 'delta', text: '' });
  });

  it('tool_result 状态归一为 success/failed', () => {
    expect(normalizeAiEvent({ type: 'tool_result', toolName: 'get_x', status: 'success' })).toEqual({
      type: 'tool_result', toolName: 'get_x', status: STEP.SUCCESS, error: '',
    });
    expect(normalizeAiEvent({ type: 'tool_result', toolName: 'get_x', status: 'error', error: 'boom' })).toEqual({
      type: 'tool_result', toolName: 'get_x', status: STEP.FAILED, error: 'boom',
    });
  });

  it('未知事件返回 null', () => {
    expect(normalizeAiEvent({ type: 'meta' })).toBeNull();
    expect(normalizeAiEvent(null)).toBeNull();
  });
});

describe('applyAiEvent — 消息生命周期', () => {
  it('pending → streaming，reasoning 更新', () => {
    const next = applyAiEvent(withTarget(), { type: 'reasoning', text: '思考' }, 'a1');
    const msg = next[1];
    expect(msg.status).toBe(MSG.STREAMING);
    expect(msg.reasoning).toBe('思考');
  });

  it('reasoning 分块事件按顺序追加而非覆盖', () => {
    let next = applyAiEvent(withTarget(), { type: 'reasoning', text: '第一步' }, 'a1');
    next = applyAiEvent(next, { type: 'reasoning', text: '·第二步' }, 'a1');
    expect(next[1].reasoning).toBe('第一步·第二步');
  });

  it('delta 追加到已有 text 块；首个 delta 新建 text 块', () => {
    let next = applyAiEvent(withTarget(), { type: 'delta', text: '你好' }, 'a1');
    expect(next[1].blocks).toEqual([{ type: 'text', text: '你好' }]);
    next = applyAiEvent(next, { type: 'delta', text: '世界' }, 'a1');
    expect(next[1].blocks).toEqual([{ type: 'text', text: '你好世界' }]);
  });

  it('done → completed + inactive', () => {
    const next = applyAiEvent(withTarget(), { type: 'done' }, 'a1');
    expect(next[1].status).toBe(MSG.COMPLETED);
    expect(next[1].active).toBe(false);
  });

  it('error → error 状态 + 错误块 + inactive', () => {
    const next = applyAiEvent(withTarget(), { type: 'error', message: '模型崩了', retryPrompt: '运行状态' }, 'a1');
    expect(next[1].status).toBe(MSG.ERROR);
    expect(next[1].active).toBe(false);
    expect(next[1].blocks[0]).toMatchObject({ type: 'error', retryable: true, retryPrompt: '运行状态' });
  });

  it('目标不存在（已切换/已 inactive）→ 数组原样返回（防串流污染）', () => {
    const base = withTarget();
    const done = applyAiEvent(base, { type: 'done' }, 'a1');
    // done 之后 active=false，旧流事件不得再污染
    expect(applyAiEvent(done, { type: 'delta', text: '污染' }, 'a1')).toBe(done);
    // 目标 id 不在数组中
    expect(applyAiEvent(base, { type: 'delta', text: 'x' }, 'nonexistent')).toBe(base);
  });

  it('已完成消息不再接收事件', () => {
    const base = withTarget();
    let next = applyAiEvent(base, { type: 'done' }, 'a1');
    next = applyAiEvent(next, { type: 'delta', text: '晚到的文本' }, 'a1');
    expect(next[1].blocks).toEqual([]);
  });
});

describe('applyAiEvent — 工具调用', () => {
  it('tool_start 追加步骤并激活；tool_result 更新', () => {
    let next = applyAiEvent(withTarget(), { type: 'tool_start', toolName: 'get_a', args: '{}' }, 'a1');
    expect(next[1].status).toBe(MSG.STREAMING);
    expect(next[1].thinking).toHaveLength(1);
    expect(next[1].thinking[0].status).toBe(STEP.RUNNING);

    next = applyAiEvent(next, { type: 'tool_result', toolName: 'get_a', status: 'success' }, 'a1');
    expect(next[1].thinking[0].status).toBe(STEP.SUCCESS);
  });

  it('同一工具多次调用按顺序配对（不误配第一个）', () => {
    let next = withTarget();
    next = applyAiEvent(next, { type: 'tool_start', toolName: 'get_a' }, 'a1');
    next = applyAiEvent(next, { type: 'tool_start', toolName: 'get_a' }, 'a1');
    // 第一个 result → 第一个 running 步骤
    next = applyAiEvent(next, { type: 'tool_result', toolName: 'get_a', status: STEP.SUCCESS }, 'a1');
    expect(next[1].thinking.map((s) => s.status)).toEqual([STEP.SUCCESS, STEP.RUNNING]);
    // 第二个 result 应落到第二个 running 步骤，而不是覆盖第一个
    next = applyAiEvent(next, { type: 'tool_result', toolName: 'get_a', status: STEP.FAILED, error: '第二次失败' }, 'a1');
    expect(next[1].thinking.map((s) => s.status)).toEqual([STEP.SUCCESS, STEP.FAILED]);
    expect(next[1].thinking[1].error).toBe('第二次失败');
  });

  it('tool_result 无匹配 running 步骤时幂等（内容不变）', () => {
    const base = withTarget();
    const next = applyAiEvent(base, { type: 'tool_result', toolName: 'ghost', status: STEP.SUCCESS }, 'a1');
    expect(next[1].thinking).toEqual([]);
    expect(next[1].blocks).toEqual([]);
  });
});

describe('applyAiEvent — 审批', () => {
  it('approval_required 追加审批块并激活', () => {
    const ev = { type: 'approval', approvalId: 'app1', planSummary: '执行 POST /x', method: 'POST', path: '/x' };
    const next = applyAiEvent(withTarget(), ev, 'a1');
    const block = next[1].blocks[0];
    expect(block).toMatchObject({ type: 'approval', approvalId: 'app1', status: APPROVAL.PENDING });
  });

  it('resolveApprovalBlock 更新审批状态', () => {
    let next = applyAiEvent(withTarget(), { type: 'approval', approvalId: 'app1', planSummary: 's' }, 'a1');
    next = resolveApprovalBlock(next, 'app1', 'approve');
    expect(next[1].blocks[0].status).toBe(APPROVAL.APPROVED);
  });
});

describe('failMessage / cancelMessage', () => {
  it('failMessage 把占位转为错误块', () => {
    const next = failMessage(withTarget(), 'a1', '409 冲突', '运行状态');
    expect(next[1].status).toBe(MSG.ERROR);
    expect(next[1].blocks[0]).toMatchObject({ type: 'error', message: '409 冲突', retryPrompt: '运行状态' });
  });

  it('cancelMessage 无输出时移除占位', () => {
    const next = cancelMessage(withTarget(), 'a1');
    expect(next.some((m) => m.id === 'a1')).toBe(false);
    expect(next[0].role).toBe('user');
  });

  it('cancelMessage 有输出时标记 cancelled', () => {
    let base = applyAiEvent(withTarget(), { type: 'reasoning', text: '想' }, 'a1');
    base = cancelMessage(base, 'a1');
    expect(base[1].status).toBe(MSG.CANCELLED);
    expect(base[1].active).toBe(false);
  });
});

describe('查询辅助', () => {
  it('isStreaming 判定', () => {
    expect(isStreaming(MSG.PENDING)).toBe(true);
    expect(isStreaming(MSG.STREAMING)).toBe(true);
    expect(isStreaming(MSG.COMPLETED)).toBe(false);
    expect(isStreaming(MSG.ERROR)).toBe(false);
  });

  it('collectAssistantText 汇总 text 块与历史 content', () => {
    const msg = createAssistantMessage('a', null, MSG.COMPLETED);
    msg.content = '历史正文';
    msg.blocks = [{ type: 'text', text: '流式正文' }, { type: 'tool_call' }];
    expect(collectAssistantText(msg)).toBe('历史正文\n流式正文');
  });
});

describe('session_title 事件', () => {
  it('normalizeAiEvent 提取 sessionId/title', () => {
    expect(normalizeAiEvent({ type: 'session_title', sessionId: 's1', title: '我的域名配置' })).toEqual({
      type: 'session_title', sessionId: 's1', title: '我的域名配置',
    });
    expect(normalizeAiEvent({ type: 'session_title' })).toEqual({ type: 'session_title', sessionId: '', title: '' });
  });

  it('STREAM_EVENTS 包含 session_title', () => {
    expect(STREAM_EVENTS).toContain('session_title');
  });
});
