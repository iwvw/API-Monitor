/* 流式阶段文案：按消息当前实际所处的 part 推导，而不是固定显示「正在回复…」。
 * 外部 run（live）优先用后端给的 phase；SSE 流式按最后一个 part 的类型判断。
 * 数据模型没有 part 时间戳，因此不显示耗时。 */
export function streamPhaseLabel(parts) {
  if (!parts || parts.length === 0) return '正在思考…';
  const last = parts[parts.length - 1];
  if (last.type === 'tool_call' || last.type === 'tool_result') return '正在执行工具…';
  if (last.type === 'reasoning') return '正在思考…';
  if (last.type === 'approval') return '等待审批…';
  return '正在回复…';
}
