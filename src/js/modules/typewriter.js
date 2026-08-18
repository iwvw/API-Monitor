// TypewriterText 的核心状态推进逻辑（纯函数，便于单元测试）。
//
// SSE 增量下文本是「追加」的（next 以 prev 开头）；同一组件实例复用承载
// 新消息时文本是「整体替换」的。reset 表示应从头开始揭示，extend 表示应
// 在现有揭示进度上继续，两者都不成立时保持现状。
export function typewriterFrame(prev, next) {
  if (!next.startsWith(prev)) return { reset: true };
  if (next.length > prev.length) return { extend: true };
  return {};
}