import { describe, it, expect } from 'vitest';
import { streamPhaseLabel } from './phaseLabel.js';

describe('streamPhaseLabel — 流式阶段文案按实际 part 推导', () => {
  it('无 part 时显示思考中', () => {
    expect(streamPhaseLabel([])).toBe('正在思考…');
    expect(streamPhaseLabel(undefined)).toBe('正在思考…');
  });

  it('末段是工具调用或工具结果时显示执行工具', () => {
    expect(streamPhaseLabel([{ type: 'tool_call' }])).toBe('正在执行工具…');
    expect(streamPhaseLabel([{ type: 'reasoning' }, { type: 'tool_result' }])).toBe('正在执行工具…');
  });

  it('末段是推理时显示思考中', () => {
    expect(streamPhaseLabel([{ type: 'tool_call' }, { type: 'reasoning' }])).toBe('正在思考…');
  });

  it('末段是审批时显示等待审批', () => {
    expect(streamPhaseLabel([{ type: 'text' }, { type: 'approval' }])).toBe('等待审批…');
  });

  it('末段是正文时显示回复中', () => {
    expect(streamPhaseLabel([{ type: 'text' }])).toBe('正在回复…');
  });
});
