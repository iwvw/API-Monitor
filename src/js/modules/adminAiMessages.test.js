import { describe, it, expect } from 'vitest';
import {
  MSG, STEP, APPROVAL, STREAM_EVENTS,
  createUserMessage, createAssistantMessage,
  normalizeAiEvent, applyAiEvent, failMessage, cancelMessage,
  resolveApprovalPart, buildTimelineFromRows, collectAssistantText, isStreaming,
} from './adminAiMessages.js';

const tgt = () => createAssistantMessage('a1', 'run1');

function withTarget(...msgs) {
  return [createUserMessage('u1', 'hi'), tgt(), ...msgs];
}

describe('normalizeAiEvent', () => {
  it('reasoning/delta 规范化', () => {
    expect(normalizeAiEvent({ type: 'reasoning', text: '想' })).toEqual({ type: 'reasoning', text: '想', userMessageId: '' });
    expect(normalizeAiEvent({ type: 'delta', text: '回答' })).toEqual({ type: 'delta', text: '回答', userMessageId: '' });
    expect(normalizeAiEvent({ type: 'delta' })).toEqual({ type: 'delta', text: '', userMessageId: '' });
  });

  it('tool_start 规范化', () => {
    expect(normalizeAiEvent({ type: 'tool_start', toolName: 'call_api', toolCallId: 'c1', args: '{}', desc: '查询' })).toEqual({
      type: 'tool_start', toolName: 'call_api', toolCallId: 'c1', args: '{}', desc: '查询', userMessageId: '',
    });
  });

  it('tool_result 状态归一为 success/failed', () => {
    expect(normalizeAiEvent({ type: 'tool_result', toolName: 'get_x', status: 'success', summary: 'ok' })).toEqual({
      type: 'tool_result', toolName: 'get_x', toolCallId: '', status: STEP.SUCCESS, summary: 'ok', error: '', userMessageId: '',
    });
    expect(normalizeAiEvent({ type: 'tool_result', toolName: 'get_x', toolCallId: 'aatc_1', status: 'error', error: 'boom' })).toEqual({
      type: 'tool_result', toolName: 'get_x', toolCallId: 'aatc_1', status: STEP.FAILED, summary: '', error: 'boom', userMessageId: '',
    });
  });

  it('未知事件返回 null', () => {
    expect(normalizeAiEvent({ type: 'meta' })).toBeNull();
    expect(normalizeAiEvent(null)).toBeNull();
  });

  it('done/error 透传 userMessageId（编辑重发截断依据）', () => {
    expect(normalizeAiEvent({ type: 'done', userMessageId: 'aam_1' })).toEqual({ type: 'done', userMessageId: 'aam_1' });
    expect(normalizeAiEvent({ type: 'done' })).toEqual({ type: 'done', userMessageId: '' });
    expect(normalizeAiEvent({ type: 'error', message: 'x', userMessageId: 'aam_1' })).toEqual({
      type: 'error', message: 'x', userMessageId: 'aam_1',
    });
  });
});

describe('applyAiEvent — parts 生命周期', () => {
  it('pending → streaming，reasoning part 追加', () => {
    const next = applyAiEvent(withTarget(), { type: 'reasoning', text: '思考' }, 'a1');
    const msg = next[1];
    expect(msg.status).toBe(MSG.STREAMING);
    expect(msg.parts).toEqual([{ type: 'reasoning', text: '思考' }]);
  });

  it('reasoning 分块事件按顺序合并到同一 part 而非覆盖', () => {
    let list = applyAiEvent(withTarget(), { type: 'reasoning', text: '先' }, 'a1');
    list = applyAiEvent(list, { type: 'reasoning', text: '后' }, 'a1');
    expect(list[1].parts).toEqual([{ type: 'reasoning', text: '先后' }]);
  });

  it('reasoning_summary 更新最后一个 reasoning part', () => {
    let list = applyAiEvent(withTarget(), { type: 'reasoning', text: '想' }, 'a1');
    list = applyAiEvent(list, { type: 'reasoning_summary', text: '探索witr项目' }, 'a1');
    expect(list[1].parts).toEqual([{ type: 'reasoning', text: '想', summary: '探索witr项目' }]);
  });

  it('delta 追加 text part，连续 delta 合并', () => {
    let list = applyAiEvent(withTarget(), { type: 'delta', text: '你好' }, 'a1');
    list = applyAiEvent(list, { type: 'delta', text: '世界' }, 'a1');
    expect(list[1].parts).toEqual([{ type: 'text', text: '你好世界' }]);
  });

  it('工具调用时间序：tool_start → tool_result 生成两个 part 并更新状态', () => {
    let list = applyAiEvent(withTarget(), { type: 'tool_start', toolName: 'call_api', toolCallId: 'c1', args: '{}', desc: '查询' }, 'a1');
    expect(list[1].parts).toEqual([{ type: 'tool_call', toolName: 'call_api', toolCallId: 'c1', args: '{}', desc: '查询', status: STEP.RUNNING }]);
    list = applyAiEvent(list, { type: 'tool_result', toolName: 'call_api', toolCallId: 'c1', status: 'success', summary: '结果' }, 'a1');
    expect(list[1].parts[0].status).toBe(STEP.SUCCESS);
    expect(list[1].parts[1]).toEqual({ type: 'tool_result', toolName: 'call_api', toolCallId: 'c1', summary: '结果', status: STEP.SUCCESS });
  });

  it('失败工具：tool_call 状态 failed，tool_result part 携带 error 状态', () => {
    let list = applyAiEvent(withTarget(), { type: 'tool_start', toolName: 'call_api', toolCallId: 'c1' }, 'a1');
    list = applyAiEvent(list, normalizeAiEvent({ type: 'tool_result', toolName: 'call_api', toolCallId: 'c1', status: 'error', error: 'boom', summary: '' }), 'a1');
    expect(list[1].parts[0].status).toBe(STEP.FAILED);
    expect(list[1].parts[1].status).toBe(STEP.FAILED);
  });

  it('done 结束消息且 roundUserMsgId 记录归属', () => {
    let list = applyAiEvent(withTarget(), { type: 'delta', text: '正文', userMessageId: 'aam_u1' }, 'a1');
    list = applyAiEvent(list, { type: 'done', userMessageId: 'aam_u1' }, 'a1');
    const msg = list[1];
    expect(msg.status).toBe(MSG.COMPLETED);
    expect(msg.active).toBe(false);
    expect(msg.roundUserMsgId).toBe('aam_u1');
    // done 事件把 userMessageId 记到其前一条用户消息（编辑重发截断依据）
    expect(list[0].dbId).toBe('aam_u1');
  });

  it('error 事件：error part + 终态', () => {
    const next = applyAiEvent(withTarget(), { type: 'error', message: '执行失败', userMessageId: 'aam_u1' }, 'a1');
    const msg = next[1];
    expect(msg.status).toBe(MSG.ERROR);
    expect(msg.parts).toEqual([{ type: 'error', message: '执行失败', retryable: true, retryPrompt: '' }]);
  });

  it('approval 事件 append approval part', () => {
    const next = applyAiEvent(withTarget(), { type: 'approval', approvalId: 'ap1', planSummary: '删除', method: 'DELETE', path: 'resource/x' }, 'a1');
    expect(next[1].parts[0]).toMatchObject({ type: 'approval', approvalId: 'ap1', method: 'DELETE', path: 'resource/x', status: APPROVAL.PENDING });
  });

  it('retry 事件 append notice part（不打断流）', () => {
    let list = applyAiEvent(withTarget(), { type: 'delta', text: '正文' }, 'a1');
    list = applyAiEvent(list, normalizeAiEvent({ type: 'retry', attempt: 2, message: '上游暂时不可用，正在重试', userMessageId: 'aam_u1' }), 'a1');
    expect(list[1].status).toBe(MSG.STREAMING);
    expect(list[1].parts[1]).toMatchObject({ type: 'notice' });
    expect(list[1].parts[1].text).toContain('第 2/10 次');
    // 重试后继续收到正文（notice 之后为新 text part），流不被中断
    list = applyAiEvent(list, { type: 'delta', text: '更多' }, 'a1');
    expect(list[1].parts[2].text).toBe('更多');
  });
});

describe('applyAiEvent — 生命周期管理', () => {
  it('failMessage 把占位转为错误 part', () => {
    const next = failMessage(withTarget(), 'a1', '发送失败', '原prompt');
    expect(next[1].status).toBe(MSG.ERROR);
    expect(next[1].parts).toEqual([{ type: 'error', message: '发送失败', retryable: true, retryPrompt: '原prompt' }]);
  });

  it('cancelMessage：无输出移除占位', () => {
    const next = cancelMessage(withTarget(), 'a1');
    expect(next.length).toBe(1); // 占位被移除，只留 user 消息
  });

  it('cancelMessage：有输出标记 cancelled', () => {
    let list = applyAiEvent(withTarget(), { type: 'delta', text: '部分' }, 'a1');
    list = cancelMessage(list, 'a1');
    expect(list[1].status).toBe(MSG.CANCELLED);
    expect(list[1].active).toBe(false);
  });

  it('resolveApprovalPart 更新 approval 状态', () => {
    let list = applyAiEvent(withTarget(), { type: 'approval', approvalId: 'ap1' }, 'a1');
    list = resolveApprovalPart(list, 'ap1', 'reject');
    expect(list[1].parts[0].status).toBe(APPROVAL.REJECTED);
  });

  it('collectAssistantText 汇总 text parts', () => {
    let list = applyAiEvent(withTarget(), { type: 'delta', text: '第一段' }, 'a1');
    list = applyAiEvent(list, { type: 'tool_start', toolName: 'x', toolCallId: 'c1' }, 'a1');
    list = applyAiEvent(list, { type: 'delta', text: '第二段' }, 'a1');
    expect(collectAssistantText(list[1])).toBe('第一段\n第二段');
  });

  it('isStreaming 判断 pending/streaming', () => {
    expect(isStreaming(MSG.PENDING)).toBe(true);
    expect(isStreaming(MSG.STREAMING)).toBe(true);
    expect(isStreaming(MSG.COMPLETED)).toBe(false);
  });

  it('STREAM_EVENTS 完整性', () => {
    for (const t of ['reasoning', 'reasoning_summary', 'delta', 'tool_start', 'tool_result', 'approval_required', 'error', 'done', 'session_title']) {
      expect(STREAM_EVENTS).toContain(t);
    }
  });
});

describe('join 语义：运行中追问的轮次分段', () => {
  it('首轮事件归属占位；第二轮事件自动新建独立段消息', () => {
    let list = withTarget();
    list = applyAiEvent(list, normalizeAiEvent({ type: 'reasoning', text: '第一轮思考', userMessageId: 'aam_u1' }), 'a1');
    expect(list.length).toBe(2);
    expect(list[1].parts[0].text).toBe('第一轮思考');
    expect(list[1].roundUserMsgId).toBe('aam_u1');
    list = applyAiEvent(list, normalizeAiEvent({ type: 'done', userMessageId: 'aam_u1' }), 'a1');
    expect(list[1].status).toBe(MSG.COMPLETED);
    list = applyAiEvent(list, normalizeAiEvent({ type: 'tool_start', toolName: 'call_api', toolCallId: 'c1', userMessageId: 'aam_u2' }), 'a1');
    expect(list.length).toBe(3);
    const seg = list[2];
    expect(seg.id).toBe('assistant_aam_u2');
    expect(seg.roundUserMsgId).toBe('aam_u2');
    expect(seg.parts[0].toolName).toBe('call_api');
    list = applyAiEvent(list, normalizeAiEvent({ type: 'delta', text: '第二轮的正文', userMessageId: 'aam_u2' }), 'a1');
    expect(list[2].parts[1].text).toBe('第二轮的正文');
  });

  it('无 userMessageId 的旧式事件仍按 target 归并（兼容）', () => {
    let list = withTarget();
    list = applyAiEvent(list, { type: 'delta', text: '旧式' }, 'a1');
    expect(list.length).toBe(2);
    expect(list[1].parts[0].text).toBe('旧式');
  });

  it('两轮且第二轮在首轮完成前到达：轮次变化即分段（不依赖 done）', () => {
    let list = withTarget();
    list = applyAiEvent(list, { type: 'tool_start', toolName: 'query', toolCallId: 'c1', userMessageId: 'aam_u1' }, 'a1');
    list = applyAiEvent(list, { type: 'tool_start', toolName: 'query2', toolCallId: 'c2', userMessageId: 'aam_u2' }, 'a1');
    expect(list.length).toBe(3);
    expect(list[1].parts[0].toolName).toBe('query');
    expect(list[2].parts[0].toolName).toBe('query2');
  });
});

describe('buildTimelineFromRows — 历史恢复为时间序 parts', () => {
  it('工具轮 + 最终正文合并为一条消息（推理/工具/结果/正文按时间序）', () => {
    const rows = [
      { id: 'aam_u1', role: 'user', content: '装好没' },
      {
        id: 'aam_a1', role: 'assistant', content: '', reasoning_content: '先看主机',
        reasoning_summary: '查看主机', toolCallMeta: JSON.stringify([
          { id: 'tc1', function: { name: 'call_api', arguments: '{}' }, desc: '列出主机' },
          { id: 'tc2', function: { name: 'call_api', arguments: '{}' }, desc: '执行安装' },
        ]),
      },
      { id: 'aam_t1', role: 'tool', content: '["host-a"]' },
      { id: 'aam_t2', role: 'tool', content: 'ok' },
      { id: 'aam_a2', role: 'assistant', content: '已装完', reasoning_content: '' },
    ];
    const msgs = buildTimelineFromRows(rows);
    expect(msgs).toHaveLength(2);
    expect(msgs[0]).toMatchObject({ role: 'user', content: '装好没' });
    const m = msgs[1];
    expect(m.role).toBe('assistant');
    expect(m.parts.map((p) => p.type)).toEqual(['reasoning', 'tool_call', 'tool_call', 'tool_result', 'tool_result', 'text']);
    expect(m.parts[0]).toMatchObject({ type: 'reasoning', text: '先看主机', summary: '查看主机' });
    expect(m.parts[1]).toMatchObject({ type: 'tool_call', toolName: 'call_api', desc: '列出主机', toolCallId: 'tc1' });
    expect(m.parts[3]).toMatchObject({ type: 'tool_result', summary: '["host-a"]' });
    expect(m.parts[5]).toMatchObject({ type: 'text', text: '已装完' });
  });

  it('多轮（user 间隔）各自独立为消息', () => {
    const rows = [
      { id: 'u1', role: 'user', content: '第一问' },
      { id: 'a1', role: 'assistant', content: '第一答' },
      { id: 'u2', role: 'user', content: '第二问' },
      { id: 'a2', role: 'assistant', content: '第二答' },
    ];
    const msgs = buildTimelineFromRows(rows);
    expect(msgs).toHaveLength(4);
    expect(msgs[1].parts[0].text).toBe('第一答');
    expect(msgs[3].parts[0].text).toBe('第二答');
  });

  it('纯推理轮（无正文）也保留 reasoning part', () => {
    const rows = [
      { id: 'u1', role: 'user', content: '查一下' },
      { id: 'a1', role: 'assistant', content: '', reasoning_content: '在想', toolCallMeta: '[]' },
      { id: 'a2', role: 'assistant', content: '', reasoning_content: '' },
    ];
    const msgs = buildTimelineFromRows(rows);
    expect(msgs[1].parts.map((p) => p.type)).toEqual(['reasoning']);
    expect(msgs[1].parts[0].text).toBe('在想');
  });

  it('toolCallMeta 为对象（旧数据）时兼容解析', () => {
    const rows = [
      { id: 'u1', role: 'user', content: '问' },
      { id: 'a1', role: 'assistant', content: '', toolCallMeta: JSON.stringify({ id: 'tc1', function: { name: 'get_x' } }) },
      { id: 't1', role: 'tool', content: 'r' },
    ];
    const msgs = buildTimelineFromRows(rows);
    expect(msgs[1].parts).toHaveLength(2);
    expect(msgs[1].parts[0]).toMatchObject({ type: 'tool_call', toolName: 'get_x' });
    expect(msgs[1].parts[1]).toMatchObject({ type: 'tool_result', summary: 'r' });
  });

  it('孤儿 tool 行忽略（无前置 assistant）', () => {
    const rows = [
      { id: 't1', role: 'tool', content: 'x', tool_call_id: 'tc1' },
    ];
    const msgs = buildTimelineFromRows(rows);
    expect(msgs).toHaveLength(0);
  });
});
