// AI Agent 实例表单的端口冲突预检。
//
// 创建/编辑实例时，结合主机侧诊断结果（usedPorts/suggestedPort）与当前表单值
// 推导「所选端口是否被占用 + 建议切换端口」。抽成纯函数便于测试锁定：
// 「默认端口被占 → 提示切换」是用户最常踩的坑，不能靠手工验证。

/**
 * 判断给定端口是否落在诊断报告的已占用列表中。
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
 * 推导端口冲突：占用中的端口 + 建议空闲端口。
 *
 * @param {{usedPorts?: number[], suggestedPort?: number}} diagnose
 * @param {string} port 表单端口值（空串表示未填，走默认端口）
 * @param {number|undefined} defaultPort Provider 默认端口
 * @returns {{occupied: number, suggested: number}|null} 无冲突/诊断缺失返回 null
 */
export function resolvePortConflict(diagnose, port, defaultPort) {
  if (!diagnose) return null;
  const suggested = Number(diagnose.suggestedPort);
  if (!Number.isInteger(suggested) || suggested <= 0) return null;
  if (!isPortOccupied(diagnose, port, defaultPort)) return null;
  const raw = String(port ?? '').trim();
  const occupied = raw === '' ? Number(defaultPort) : Number(raw);
  return { occupied, suggested };
}