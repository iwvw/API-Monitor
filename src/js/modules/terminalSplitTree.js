/**
 * terminalSplitTree.js
 * 
 * 终端多窗口分屏树核心引擎（借鉴 Tabby SplitContainer 与 react-resizable-panels 机制）：
 * 1. 递归二叉分屏树模型（Leaf 与 Branch）：支持自由嵌套的水平（h）与垂直（v）切分；
 * 2. 规范化与清理（normalize）：自动消除冗余单子分支，合并同方向相邻分支，剔除不存在的会话；
 * 3. 智能排布模式生成器（buildPresetLayout）：单窗口、双分屏、主从（Master-Stack）、品字形、网格等；
 * 4. 磁吸热区判定（getDropTargetRegion）：基于边缘距离向量的高精度判定（center, left, right, top, bottom）；
 * 5. 比例调整与约束（resizeNodeRatio）：支持按像素或百分比拖动分割线。
 */

export const DROP_EDGE_THRESHOLD = 0.25;

/**
 * 判定放置命中区域
 * @param {DOMRect} rect 
 * @param {number} clientX 
 * @param {number} clientY 
 * @returns {'left' | 'right' | 'top' | 'bottom' | 'center'}
 */
export function getDropTargetRegion(rect, clientX, clientY) {
  if (!rect || rect.width <= 0 || rect.height <= 0) return 'center';

  const x = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
  const y = Math.max(0, Math.min(1, (clientY - rect.top) / rect.height));

  const distances = {
    top: y,
    bottom: 1 - y,
    left: x,
    right: 1 - x,
  };

  const closest = Object.keys(distances).reduce((minKey, key) => {
    return distances[key] < distances[minKey] ? key : minKey;
  }, 'left');

  return distances[closest] < DROP_EDGE_THRESHOLD ? closest : 'center';
}

/**
 * 创建叶子节点
 * @param {string} sessionId 
 */
export function createLeafNode(sessionId) {
  return {
    type: 'leaf',
    id: sessionId,
  };
}

/**
 * 创建分支节点
 * @param {'h' | 'v'} direction 'h' 代表左右横向分屏（split-horizontal），'v' 代表上下纵向分屏
 * @param {object} first 子节点 1
 * @param {object} second 子节点 2
 * @param {number} ratio 分割比例（first 所占比例，0 < ratio < 1），默认 0.5
 */
export function createBranchNode(direction, first, second, ratio = 0.5) {
  return {
    type: 'branch',
    direction,
    ratio: Math.max(0.1, Math.min(0.9, ratio)),
    first,
    second,
  };
}

/**
 * 获取树中包含的所有 sessionId 列表（前序遍历）
 * @param {object | null} node 
 * @returns {string[]}
 */
export function getTreeSessionIds(node) {
  if (!node) return [];
  if (node.type === 'leaf') return [node.id];
  if (node.type === 'branch') {
    return [...getTreeSessionIds(node.first), ...getTreeSessionIds(node.second)];
  }
  return [];
}

/**
 * 规范化分屏树：过滤不存在的会话、消除退化的单子分支
 * @param {object | null} node 
 * @param {Set<string> | string[]} validSessionIds 可选的有效会话白名单
 * @returns {object | null}
 */
export function normalizeTree(node, validSessionIds) {
  if (!node) return null;
  const validSet = validSessionIds ? (validSessionIds instanceof Set ? validSessionIds : new Set(validSessionIds)) : null;

  if (node.type === 'leaf') {
    if (validSet && !validSet.has(node.id)) return null;
    return node;
  }

  if (node.type === 'branch') {
    const firstNormalized = normalizeTree(node.first, validSet);
    const secondNormalized = normalizeTree(node.second, validSet);

    if (!firstNormalized && !secondNormalized) return null;
    if (!firstNormalized) return secondNormalized;
    if (!secondNormalized) return firstNormalized;

    return {
      type: 'branch',
      direction: node.direction === 'v' ? 'v' : 'h',
      ratio: Math.max(0.1, Math.min(0.9, typeof node.ratio === 'number' ? node.ratio : 0.5)),
      first: firstNormalized,
      second: secondNormalized,
    };
  }

  return null;
}

/**
 * 在目标会话节点处进行分屏插入或替换
 * @param {object | null} root 根节点
 * @param {string} targetSessionId 目标会话 ID
 * @param {string} newSessionId 待插入的新会话 ID
 * @param {'left' | 'right' | 'top' | 'bottom' | 'center'} position 放置方位
 * @returns {object} 更新后的树
 */
export function insertSplitIntoTree(root, targetSessionId, newSessionId, position) {
  if (!root) {
    return createLeafNode(newSessionId);
  }

  // 先从原有树中移除 newSessionId（若已存在），避免重复会话
  const cleanedRoot = removeSessionFromTree(root, newSessionId) || createLeafNode(targetSessionId);

  // 如果是在中央放置（center），则替换目标节点
  if (position === 'center') {
    return replaceSessionInTree(cleanedRoot, targetSessionId, newSessionId);
  }

  const direction = (position === 'left' || position === 'right') ? 'h' : 'v';
  const isFirst = (position === 'left' || position === 'top');

  function traverseAndSplit(node) {
    if (!node) return null;
    if (node.type === 'leaf' && node.id === targetSessionId) {
      const newNode = createLeafNode(newSessionId);
      const first = isFirst ? newNode : node;
      const second = isFirst ? node : newNode;
      return createBranchNode(direction, first, second, 0.5);
    }

    if (node.type === 'branch') {
      return {
        ...node,
        first: traverseAndSplit(node.first),
        second: traverseAndSplit(node.second),
      };
    }

    return node;
  }

  const result = traverseAndSplit(cleanedRoot);
  return normalizeTree(result);
}

/**
 * 替换树中的某个会话
 */
export function replaceSessionInTree(node, oldSessionId, newSessionId) {
  if (!node) return null;
  if (node.type === 'leaf') {
    if (node.id === oldSessionId) {
      return createLeafNode(newSessionId);
    }
    return node;
  }
  if (node.type === 'branch') {
    return {
      ...node,
      first: replaceSessionInTree(node.first, oldSessionId, newSessionId),
      second: replaceSessionInTree(node.second, oldSessionId, newSessionId),
    };
  }
  return node;
}

/**
 * 从树中移除某个会话
 */
export function removeSessionFromTree(node, sessionId) {
  if (!node) return null;
  if (node.type === 'leaf') {
    return node.id === sessionId ? null : node;
  }
  if (node.type === 'branch') {
    const first = removeSessionFromTree(node.first, sessionId);
    const second = removeSessionFromTree(node.second, sessionId);
    if (!first && !second) return null;
    if (!first) return second;
    if (!second) return first;
    return {
      ...node,
      first,
      second,
    };
  }
  return node;
}

/**
 * 根据预设模式与会话列表构建智能排布树
 * @param {string[]} sessionIds 会话 ID 列表
 * @param {'single' | 'split-h' | 'split-v' | 'master-stack' | 'grid' | 'equal'} preset 
 * @param {string} [activeSessionId] 当前激活会话（主从模式下作为 master）
 * @returns {object | null}
 */
export function buildPresetLayout(sessionIds, preset = 'grid', activeSessionId = '') {
  const ids = Array.isArray(sessionIds) ? sessionIds.filter(Boolean) : [];
  if (ids.length === 0) return null;
  if (ids.length === 1 || preset === 'single') {
    const targetId = activeSessionId && ids.includes(activeSessionId) ? activeSessionId : ids[0];
    return createLeafNode(targetId);
  }

  // 主从模式：Master 占 62%，其余在侧栏堆叠。
  // 必须排在「双会话」早返回之前：两窗口时同样要 62/38，而不是退化回 50/50。
  if (preset === 'master-stack') {
    const masterId = activeSessionId && ids.includes(activeSessionId) ? activeSessionId : ids[0];
    const stackIds = ids.filter(id => id !== masterId);
    const stackBranch = buildLinearTree(stackIds, 'v');
    if (!stackBranch) return createLeafNode(masterId);
    return createBranchNode('h', createLeafNode(masterId), stackBranch, 0.62);
  }

  // 双会话
  if (ids.length === 2) {
    const direction = preset === 'split-v' ? 'v' : 'h';
    return createBranchNode(direction, createLeafNode(ids[0]), createLeafNode(ids[1]), 0.5);
  }

  // 三会话及以上
  if (preset === 'split-h') {
    // 纯水平等分树
    return buildLinearTree(ids, 'h');
  }
  if (preset === 'split-v') {
    // 纯垂直等分树
    return buildLinearTree(ids, 'v');
  }

  // 默认 grid / equal / 智能平衡模式
  return buildBalancedGridTree(ids);
}

/**
 * 递归构建平衡网格树（2 -> 1x2, 3 -> 1大+2小品字形, 4 -> 2x2, 5+ -> 平衡二分）
 */
export function buildBalancedGridTree(ids) {
  if (ids.length === 0) return null;
  if (ids.length === 1) return createLeafNode(ids[0]);
  if (ids.length === 2) {
    return createBranchNode('h', createLeafNode(ids[0]), createLeafNode(ids[1]), 0.5);
  }
  if (ids.length === 3) {
    // 黄金品字形：左 1 个，右 2 个上下对半
    return createBranchNode(
      'h',
      createLeafNode(ids[0]),
      createBranchNode('v', createLeafNode(ids[1]), createLeafNode(ids[2]), 0.5),
      0.5
    );
  }
  if (ids.length === 4) {
    // 标准四宫格 2x2
    return createBranchNode(
      'v',
      createBranchNode('h', createLeafNode(ids[0]), createLeafNode(ids[1]), 0.5),
      createBranchNode('h', createLeafNode(ids[2]), createLeafNode(ids[3]), 0.5),
      0.5
    );
  }

  // 5 个及以上：对半切分
  const mid = Math.ceil(ids.length / 2);
  const leftIds = ids.slice(0, mid);
  const rightIds = ids.slice(mid);
  return createBranchNode('h', buildBalancedGridTree(leftIds), buildBalancedGridTree(rightIds), 0.5);
}

/**
 * 线性排列辅助树（等分）
 */
function buildLinearTree(ids, direction) {
  if (ids.length <= 1) return ids.length === 1 ? createLeafNode(ids[0]) : null;
  const count = ids.length;
  const first = createLeafNode(ids[0]);
  const rest = buildLinearTree(ids.slice(1), direction);
  // first 占 1/count
  return createBranchNode(direction, first, rest, 1 / count);
}

/**
 * 重设树中指定分支的分割比率
 * @param {object} root 根节点
 * @param {string[]} path 路径数组，例如 ['first', 'second']
 * @param {number} newRatio 新比例
 */
export function updateBranchRatio(root, path, newRatio) {
  if (!root || !path || path.length === 0) return root;
  const clampedRatio = Math.max(0.1, Math.min(0.9, newRatio));

  function cloneAndUpdate(node, index) {
    if (!node || node.type !== 'branch') return node;
    const key = path[index];
    if (index === path.length - 1) {
      return {
        ...node,
        ratio: clampedRatio,
      };
    }
    return {
      ...node,
      [key]: cloneAndUpdate(node[key], index + 1),
    };
  }

  return cloneAndUpdate(root, 0);
}
