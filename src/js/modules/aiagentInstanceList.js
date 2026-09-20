// 实例列表的排序。
//
// 抽成纯函数模块：「哪些状态算异常」是业务判断，需要测试锁定，
// 不能埋进 JSX 的 sort 回调里。

import { instanceStatus } from './aiagentInstanceStatus.js';

/**
 * 判断实例是否属于「需要关注」的状态。
 *
 * 定义：崩溃、端口被占用、未监听端口、主机离线、进程未运行、状态未知、已停用。
 * 不包含「已停止」——那是用户主动操作的结果，属正常终态。
 * 也不包含「启动中」——那是过渡态，会自动收敛。
 */
export function needsAttention(instance) {
  if (!instance) return false;
  if (!instance.enabled) return true;
  const status = instanceStatus(instance);
  return ['已崩溃', '端口被占用', '未监听端口', '主机离线', '进程未运行', '状态未知'].includes(
    status.label
  );
}

/**
 * 按「异常优先，其次按名称」排序实例。
 *
 * 异常置顶是唯一有实际价值的排序：实例出问题时应该第一眼看到。
 * 名称作为次级键保证顺序稳定可预期（不依赖数组原始顺序）。
 *
 * @param {Array} instances 原始列表
 * @returns {Array} 新数组，不修改入参
 */
export function sortInstances(instances) {
  if (!Array.isArray(instances)) return [];
  // 复制后再排：不能就地修改入参数组（调用方可能在别处复用）
  return [...instances].sort((a, b) => {
    const attentionDiff = Number(needsAttention(b)) - Number(needsAttention(a));
    if (attentionDiff !== 0) return attentionDiff;
    return String(a?.label || '').localeCompare(String(b?.label || ''), 'zh-CN');
  });
}
