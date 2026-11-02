// AI Agent 实例表单的端口占用预检。
//
// 创建/编辑实例时，结合主机侧诊断结果（usedPorts/sameProviderPorts/
// foreignOccupiedPorts）与当前表单值，推导「所选端口是否被占用」以及该怎么提示。
//
// 语义（ADR-0006 第 7.5 条修订：目标端口占用一律强制清理）：
//   - 同类占用（进程名命中 Provider，如别的 opencode）：start 时会自动清理，
//     不提示换端口——换端口也只是换个地方被清理。
//   - 无关占用（其它进程）：start 时同样会被强制清理，仅做提示，不拦启动。

/**
 * 判断给定端口是否被任何进程占用（同诊断报告口径）。
 *
 * @param {{usedPorts?: number[]}} diagnose 诊断结果（可为 null/undefined）
 * @param {number|string|undefined} port 表单端口（空串/未填表示用默认端口）
 * @param {number|undefined} defaultPort Provider 默认端口
 * @returns {boolean}
 */
export function isPortOccupied(diagnose, port, defaultPort) {
  if (!diagnose) return false;
  const used = Array.isArray(diagnose.usedPorts) ? diagnose.usedPorts : [];
  if (used.length === 0) return false;
  const raw = String(port ?? '').trim();
  const target = raw === '' ? Number(defaultPort) : Number(raw);
  if (!Number.isInteger(target) || target <= 0) return false;
  return used.includes(target);
}

/**
 * 解析表单端口值（空串表示未填，走默认端口）。
 */
function resolvePort(port, defaultPort) {
  const raw = String(port ?? '').trim();
  const value = raw === '' ? Number(defaultPort) : Number(raw);
  if (!Number.isInteger(value) || value <= 0) return null;
  return value;
}

/**
 * 推导端口占用情况，供表单给出针对性提示。
 *
 * @param {{
 *   usedPorts?: number[],
 *   sameProviderPorts?: number[],
 *   foreignOccupiedPorts?: number[],
 * }} diagnose
 * @param {string} port 表单端口值（空串表示未填，走默认端口）
 * @param {number|undefined} defaultPort Provider 默认端口
 * @returns {{port: number, kind: 'same-provider'|'foreign'}|null} 无占用/诊断缺失返回 null
 */
export function resolvePortOccupancy(diagnose, port, defaultPort) {
  if (!diagnose) return null;
  const target = resolvePort(port, defaultPort);
  if (target === null) return null;
  const same = Array.isArray(diagnose.sameProviderPorts) ? diagnose.sameProviderPorts : [];
  const foreign = Array.isArray(diagnose.foreignOccupiedPorts) ? diagnose.foreignOccupiedPorts : [];
  // 兼容旧版 Agent：没有分类字段时，回退按 usedPorts 一律视为无关占用（旧行为）。
  const hasBreakdown = Array.isArray(diagnose.sameProviderPorts) || Array.isArray(diagnose.foreignOccupiedPorts);
  if (!hasBreakdown) {
    return isPortOccupied(diagnose, port, defaultPort) ? { port: target, kind: 'foreign' } : null;
  }
  if (foreign.includes(target)) return { port: target, kind: 'foreign' };
  if (same.includes(target)) return { port: target, kind: 'same-provider' };
  return null;
}
