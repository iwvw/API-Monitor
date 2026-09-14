import { normalizeWorkflowJobName } from '../../modules/githubWorkflowJobs.js';
import {
  ACTION_FLOW_CARD_WIDTH,
  ACTION_FLOW_STAGE_GAP,
  ACTION_FLOW_PADDING_X,
  ACTION_FLOW_PADDING_Y,
  ACTION_FLOW_ROW_GAP,
  ACTION_FLOW_VIEWPORT_HEIGHT,
  ACTION_FLOW_MIN_VIEWPORT_HEIGHT,
  ACTION_FLOW_BRANCH_INSET,
} from './constants.js';
import {
  buildWorkflowGraph,
  summarizeWorkflowGroup,
  workflowDependentsById,
  workflowDescendantCounts,
  sortWorkflowGroups,
  groupWorkflowLayer,
  actionFlowExpandedVariantsHeight,
} from './utils.js';

const actionFlowNodeHeight = (node, expanded = false) => {
  if (!node) return 44;
  const variants = Array.isArray(node.variants) ? node.variants : [];
  if (node.group) {
    const rowCount = Math.max(1, variants.length);
    const headerHeight = node.name ? 24 : 0;
    const rowHeight = 30;
    const gaps = Math.max(0, rowCount - 1) * 8 + (node.name ? 10 : 0);
    return 24 + headerHeight + rowCount * rowHeight + gaps;
  }
  if (!node.matrix && !node.step && variants.length <= 1) return 52;

  const rows = [node.matrix ? 52 : 28];
  const showActiveStep = Boolean(node.step && ['in_progress', 'running', 'queued', 'pending', 'waiting'].includes(normalizeWorkflowJobName(node.status)));
  if (showActiveStep) rows.push(24);
  rows.push(24);
  if (expanded && variants.length > 1) rows.push(actionFlowExpandedVariantsHeight(variants.length));
  rows.push(24);
  return 24 + rows.reduce((sum, height) => sum + height, 0) + Math.max(0, rows.length - 1) * 8;
};

const actionFlowPortY = (rect) => rect.y + Math.max(22, Math.min(26, rect.height - 24));

const workflowReachable = (from, to, dependents, visited = new Set()) => {
  if (from === to) return true;
  if (visited.has(from)) return false;
  visited.add(from);
  return (dependents.get(from) || []).some((child) => workflowReachable(child, to, dependents, visited));
};

const reduceWorkflowEdges = (definitions, dependents) => {
  const edges = [];
  definitions.forEach((definition) => {
    const needs = (definition.needs || []).filter((need) => dependents.has(need));
    needs.forEach((need) => {
      const expressedByIntermediate = needs.some((otherNeed) => (
        otherNeed !== need && workflowReachable(need, otherNeed, dependents)
      ));
      if (!expressedByIntermediate) edges.push({ from: need, to: definition.id });
    });
  });
  return edges;
};

const workflowTrace = (focusIds, edges, mode = 'neighbors') => {
  if (!focusIds || focusIds.size === 0) return null;
  const parents = new Map();
  const children = new Map();
  edges.forEach((edge) => {
    if (!parents.has(edge.to)) parents.set(edge.to, []);
    if (!children.has(edge.from)) children.set(edge.from, []);
    parents.get(edge.to).push(edge);
    children.get(edge.from).push(edge);
  });
  const tracedNodes = new Set(focusIds);
  const tracedEdges = new Map();
  const includeEdge = (edge, kind) => {
    tracedNodes.add(edge.from);
    tracedNodes.add(edge.to);
    tracedEdges.set(`${edge.from}->${edge.to}`, { kind });
  };
  focusIds.forEach((id) => {
    (parents.get(id) || []).forEach((edge) => includeEdge(edge, 'neighbor'));
    (children.get(id) || []).forEach((edge) => includeEdge(edge, 'neighbor'));
  });
  if (mode === 'neighbors') return { nodes: tracedNodes, edges: tracedEdges };

  const visitedParents = new Set();
  const visitParents = (id) => {
    if (visitedParents.has(id)) return;
    visitedParents.add(id);
    (parents.get(id) || []).forEach((edge) => {
      includeEdge(edge, 'upstream');
      visitParents(edge.from);
    });
  };
  focusIds.forEach(visitParents);
  return { nodes: tracedNodes, edges: tracedEdges };
};

const makeRoundedOrthogonalPath = (points, radius = 20) => {
  const compact = points.filter((point, index) => (
    index === 0 || point.x !== points[index - 1].x || point.y !== points[index - 1].y
  ));
  if (compact.length < 2) return '';
  const commands = [`M ${compact[0].x} ${compact[0].y}`];
  const lineTo = (point, previous) => {
    if (point.y === previous.y) commands.push(`H ${point.x}`);
    else commands.push(`V ${point.y}`);
  };
  let cursor = compact[0];
  for (let index = 1; index < compact.length - 1; index += 1) {
    const previous = compact[index - 1];
    const corner = compact[index];
    const next = compact[index + 1];
    const incoming = Math.abs(corner.x - previous.x) + Math.abs(corner.y - previous.y);
    const outgoing = Math.abs(next.x - corner.x) + Math.abs(next.y - corner.y);
    const cornerRadius = Math.min(radius, incoming / 2, outgoing / 2);
    const before = {
      x: corner.x + Math.sign(previous.x - corner.x) * cornerRadius,
      y: corner.y + Math.sign(previous.y - corner.y) * cornerRadius,
    };
    const after = {
      x: corner.x + Math.sign(next.x - corner.x) * cornerRadius,
      y: corner.y + Math.sign(next.y - corner.y) * cornerRadius,
    };
    lineTo(before, cursor);
    commands.push(`Q ${corner.x} ${corner.y} ${after.x} ${after.y}`);
    cursor = after;
  }
  lineTo(compact[compact.length - 1], cursor);
  return commands.join(' ');
};

const makeActionConnectorPath = (source, target, busX, sourceY = actionFlowPortY(source), targetY = actionFlowPortY(target)) => {
  const x1 = source.x + source.width;
  const y1 = sourceY;
  const x2 = target.x;
  const y2 = targetY;
  if (Math.abs(y2 - y1) < 4) return `M ${x1} ${y1} H ${x2}`;
  const bus = Math.max(x1 + 10, Math.min(x2 - 10, Number.isFinite(busX) ? busX : x1 + (x2 - x1) / 2));
  return makeRoundedOrthogonalPath([
    { x: x1, y: y1 },
    { x: bus, y: y1 },
    { x: bus, y: y2 },
    { x: x2, y: y2 },
  ]);
};

const makeActionStageBranchPath = (sourceX, sourceY, targetX, targetY, busX) => {
  if (Math.abs(targetY - sourceY) < 4) return `M ${sourceX} ${sourceY} H ${targetX}`;
  const dir = Math.sign(targetY - sourceY) || 1;
  const availableLeft = Math.max(8, busX - sourceX);
  const availableRight = Math.max(8, targetX - busX);
  const curve = Math.max(8, Math.min(18, availableLeft, availableRight));
  const handle = curve * 0.5522847498;
  const startCurveX = busX - curve;
  const startCurveY = sourceY + dir * curve;
  const endCurveY = targetY - dir * curve;
  return [
    `M ${sourceX} ${sourceY}`,
    `H ${startCurveX}`,
    `C ${busX - curve + handle} ${sourceY} ${busX} ${sourceY + dir * (curve - handle)} ${busX} ${startCurveY}`,
    `V ${endCurveY}`,
    `C ${busX} ${targetY - dir * (curve - handle)} ${busX + curve - handle} ${targetY} ${busX + curve} ${targetY}`,
    `H ${targetX}`,
  ].join(' ');
};

const actionFlowLateSplitBusX = (sourceX, targetX) => Math.max(sourceX + 12, targetX - ACTION_FLOW_BRANCH_INSET);

const findClearActionLane = (rects, preferredY) => {
  const clearance = 4;
  const intervals = rects
    .map((rect) => ({ start: rect.y - clearance, end: rect.y + rect.height + clearance }))
    .sort((a, b) => a.start - b.start)
    .reduce((merged, interval) => {
      const previous = merged[merged.length - 1];
      if (!previous || interval.start > previous.end) merged.push({ ...interval });
      else previous.end = Math.max(previous.end, interval.end);
      return merged;
    }, []);
  if (intervals.length === 0) return preferredY;
  const candidates = [Math.max(8, intervals[0].start - 10), intervals[intervals.length - 1].end + 10];
  for (let index = 0; index < intervals.length - 1; index += 1) {
    const start = intervals[index].end;
    const end = intervals[index + 1].start;
    if (end - start >= 8) candidates.push(start + (end - start) / 2);
  }
  return candidates.sort((a, b) => Math.abs(a - preferredY) - Math.abs(b - preferredY))[0];
};

const compactActionStageUpwards = (stage, visualParents, nodeRects) => {
  const firstNode = stage?.nodes?.[0];
  if (!firstNode?.rect) return;
  const parentPorts = [...(visualParents.get(firstNode.id) || [])]
    .map((parentId) => nodeRects.get(parentId))
    .filter(Boolean)
    .map(actionFlowPortY)
    .sort((a, b) => a - b);
  if (parentPorts.length === 0) return;
  const currentTop = firstNode.rect.y;
  const currentPort = actionFlowPortY(firstNode.rect);
  const maxShift = currentTop - ACTION_FLOW_PADDING_Y;
  if (maxShift <= 0) return;
  const desiredPort = Math.min(...parentPorts);
  const shift = Math.min(maxShift, Math.max(0, currentPort - desiredPort));
  if (shift < 2) return;
  stage.nodes.forEach((item) => {
    item.rect.y -= shift;
  });
};

const compactActionStages = (stages, visualParents, nodeRects, startStageIndex = 1) => {
  stages.slice(startStageIndex).forEach((stage) => {
    compactActionStageUpwards(stage, visualParents, nodeRects);
  });
};

const shiftActionStage = (stage, delta) => {
  if (!stage?.nodes?.length || !Number.isFinite(delta) || Math.abs(delta) < 2) return 0;
  let applied = delta;
  if (applied < 0) {
    const maxShiftUp = Math.max(0, stage.nodes[0].rect.y - ACTION_FLOW_PADDING_Y);
    applied = -Math.min(-applied, maxShiftUp);
  }
  if (Math.abs(applied) < 2) return 0;
  stage.nodes.forEach((item) => {
    item.rect.y += applied;
  });
  const lastRect = stage.nodes[stage.nodes.length - 1]?.rect;
  if (lastRect) {
    stage.height = Math.max(stage.height, lastRect.y + lastRect.height - ACTION_FLOW_PADDING_Y);
  }
  return applied;
};

const resolveActionStageDesiredPorts = (nodes, visualParents, nodeRects, fallbackPort) => {
  const parentIdsByIndex = nodes.map((item) => [...(visualParents.get(item.id) || [])]
    .filter((parentId) => nodeRects.has(parentId))
    .sort());
  const parentPortsByIndex = parentIdsByIndex.map((parentIds) => parentIds
    .map((parentId) => nodeRects.get(parentId))
    .filter(Boolean)
    .map(actionFlowPortY)
    .sort((a, b) => a - b));

  const desiredPorts = nodes.map((item, index) => {
    const parentPorts = parentPortsByIndex[index];
    if (parentPorts.length === 0) return fallbackPort;
    if (parentPorts.length === 1) return parentPorts[0];
    return parentPorts.reduce((sum, value) => sum + value, 0) / parentPorts.length;
  });

  const alignmentGroups = new Map();
  nodes.forEach((item, index) => {
    const parentIds = parentIdsByIndex[index];
    if (parentIds.length === 0) return;
    const key = parentIds.join('|');
    if (!alignmentGroups.has(key)) alignmentGroups.set(key, { parentIds, entries: [] });
    alignmentGroups.get(key).entries.push({ item, index });
  });

  alignmentGroups.forEach(({ parentIds, entries }) => {
    if (entries.length <= 1) return;
    const contiguous = entries.every((entry, index) => index === 0 || entry.index === entries[index - 1].index + 1);
    if (!contiguous) return;
    const parentPorts = parentIds
      .map((parentId) => nodeRects.get(parentId))
      .filter(Boolean)
      .map(actionFlowPortY)
      .sort((a, b) => a - b);
    if (parentPorts.length === 0) return;

    if (parentIds.length === 1) {
      const parentPort = parentPorts[0];
      const pivotIndex = Math.floor((entries.length - 1) / 2);
      const pivotEntry = entries[pivotIndex];
      const pivotOffset = actionFlowPortY({ y: 0, height: pivotEntry.item.height });
      const pivotTop = parentPort - pivotOffset;
      desiredPorts[pivotEntry.index] = parentPort;

      let currentTop = pivotTop;
      for (let index = pivotIndex - 1; index >= 0; index -= 1) {
        const entry = entries[index];
        currentTop -= ACTION_FLOW_ROW_GAP + entry.item.height;
        const portOffset = actionFlowPortY({ y: 0, height: entry.item.height });
        desiredPorts[entry.index] = currentTop + portOffset;
      }

      let currentBottom = pivotTop + pivotEntry.item.height;
      for (let index = pivotIndex + 1; index < entries.length; index += 1) {
        const entry = entries[index];
        const top = currentBottom + ACTION_FLOW_ROW_GAP;
        const portOffset = actionFlowPortY({ y: 0, height: entry.item.height });
        desiredPorts[entry.index] = top + portOffset;
        currentBottom = top + entry.item.height;
      }
      return;
    }

    let currentBottom = Math.min(...parentPorts) - actionFlowPortY({ y: 0, height: entries[0].item.height }) + entries[0].item.height;
    entries.forEach((entry, index) => {
      const top = index === 0
        ? currentBottom - entry.item.height
        : currentBottom + ACTION_FLOW_ROW_GAP;
      const portOffset = actionFlowPortY({ y: 0, height: entry.item.height });
      desiredPorts[entry.index] = top + portOffset;
      currentBottom = top + entry.item.height;
    });
  });

  return desiredPorts;
};

const shiftActionStageUp = (stage, amount) => {
  if (!stage?.nodes?.length || !Number.isFinite(amount) || amount < 2) return 0;
  const maxShift = Math.max(0, stage.nodes[0].rect.y - ACTION_FLOW_PADDING_Y);
  const applied = Math.min(maxShift, amount);
  if (applied < 2) return 0;
  stage.nodes.forEach((item) => {
    item.rect.y -= applied;
  });
  const lastRect = stage.nodes[stage.nodes.length - 1]?.rect;
  if (lastRect) {
    stage.height = Math.max(stage.height, lastRect.y + lastRect.height - ACTION_FLOW_PADDING_Y);
  }
  return applied;
};

const shiftActionStageTail = (stage, startIndex, amount) => {
  if (!stage?.nodes?.length || !Number.isFinite(amount) || amount < 2) return 0;
  if (!Number.isInteger(startIndex) || startIndex < 0 || startIndex >= stage.nodes.length) return 0;
  stage.nodes.slice(startIndex).forEach((item) => {
    item.rect.y += amount;
  });
  const lastRect = stage.nodes[stage.nodes.length - 1]?.rect;
  if (lastRect) {
    stage.height = Math.max(stage.height, lastRect.y + lastRect.height - ACTION_FLOW_PADDING_Y);
  }
  return amount;
};

const clearActionLaneInStage = (stage, laneY, clearance = 6) => {
  if (!stage?.nodes?.length || !Number.isFinite(laneY)) return 0;
  const blockers = stage.nodes
    .map((item, index) => ({ rect: item.rect, index }))
    .filter(({ rect }) => laneY >= rect.y - clearance && laneY <= rect.y + rect.height + clearance);
  if (blockers.length === 0) return 0;
  const requiredShift = Math.max(...blockers.map(({ rect }) => rect.y + rect.height + clearance - laneY));
  const shiftedUp = shiftActionStageUp(stage, requiredShift);
  if (shiftedUp > 0) return shiftedUp;
  const startIndex = Math.min(...blockers.map(({ index }) => index));
  return shiftActionStageTail(stage, startIndex, requiredShift);
};

const straightenActionConnectorStages = (candidateEdges, stages) => {
  candidateEdges
    .filter((edge) => edge.toStage - edge.fromStage > 1)
    .sort((left, right) => {
      const leftPriority = Number(Boolean(left.fanOut || left.fanIn));
      const rightPriority = Number(Boolean(right.fanOut || right.fanIn));
      if (leftPriority !== rightPriority) return rightPriority - leftPriority;
      const leftDelta = Math.abs(actionFlowPortY(left.sourceRect) - actionFlowPortY(left.targetRect));
      const rightDelta = Math.abs(actionFlowPortY(right.sourceRect) - actionFlowPortY(right.targetRect));
      if (leftDelta !== rightDelta) return leftDelta - rightDelta;
      return (right.toStage - right.fromStage) - (left.toStage - left.fromStage);
    })
    .forEach((edge) => {
      const sourceY = actionFlowPortY(edge.sourceRect);
      const targetY = actionFlowPortY(edge.targetRect);
      if (!(edge.fanOut || edge.fanIn) && Math.abs(sourceY - targetY) > 24) return;
      const preferredLaneY = sourceY;
      for (let stageIndex = edge.fromStage + 1; stageIndex < edge.toStage; stageIndex += 1) {
        clearActionLaneInStage(stages[stageIndex], preferredLaneY);
      }
    });
};

const alignAdjacentFanoutStages = (stages, visualChildren, visualStageById, nodeRects) => {
  visualChildren.forEach((targetIds, sourceId) => {
    if (targetIds.size <= 1) return;
    const sourceStageIndex = visualStageById.get(sourceId);
    if (!Number.isFinite(sourceStageIndex)) return;
    const nextStageIndex = sourceStageIndex + 1;
    const adjacentTargetIds = [...targetIds].filter((targetId) => visualStageById.get(targetId) === nextStageIndex);
    if (adjacentTargetIds.length <= 1) return;
    const stage = stages[nextStageIndex];
    const sourceRect = nodeRects.get(sourceId);
    if (!stage || !sourceRect || stage.nodes.length !== adjacentTargetIds.length) return;
    const sourceY = actionFlowPortY(sourceRect);
    const primaryNode = [...stage.nodes]
      .filter((item) => adjacentTargetIds.includes(item.id))
      .sort((left, right) => (
        Math.abs(actionFlowPortY(left.rect) - sourceY) - Math.abs(actionFlowPortY(right.rect) - sourceY)
      ))[0];
    if (!primaryNode) return;
    shiftActionStage(stage, sourceY - actionFlowPortY(primaryNode.rect));
  });
};

const resolveActionConnectorBusX = (edge, stages) => {
  const sourceX = edge.sourceRect.x + edge.sourceRect.width;
  const targetX = edge.targetRect.x;
  const isAdjacent = edge.toStage - edge.fromStage <= 1;

  if (isAdjacent) {
    if (edge.fanOut && !edge.fanIn) {
      return sourceX + Math.min(ACTION_FLOW_BRANCH_INSET, (targetX - sourceX) * 0.45);
    }
    if (edge.fanIn && !edge.fanOut) {
      return targetX - Math.min(ACTION_FLOW_BRANCH_INSET, (targetX - sourceX) * 0.45);
    }
    return sourceX + (targetX - sourceX) / 2;
  }

  const intermediateRects = stages
    .slice(edge.fromStage + 1, edge.toStage)
    .flatMap((stage) => stage.nodes.map((item) => item.rect));
  const crossesCard = (y) => intermediateRects.some((rect) => y >= rect.y - 4 && y <= rect.y + rect.height + 4);
  const canLateSplit = !crossesCard(edge.sourceY);
  if ((edge.fanOut || edge.fanIn || Math.abs(edge.sourceY - edge.targetY) <= 24) && canLateSplit) {
    return actionFlowLateSplitBusX(sourceX, targetX);
  }
  return targetX - ACTION_FLOW_STAGE_GAP / 2;
};

const routeActionConnector = (edge, stages, busXOverride = null) => {
  const sourceX = edge.sourceRect.x + edge.sourceRect.width;
  const targetX = edge.targetRect.x;
  const sourceBusX = sourceX + ACTION_FLOW_STAGE_GAP / 2;
  const targetBusX = targetX - ACTION_FLOW_STAGE_GAP / 2;
  if (edge.toStage - edge.fromStage <= 1) {
    if (Math.abs(edge.sourceY - edge.targetY) < 4) {
      return { path: `M ${sourceX} ${edge.sourceY} H ${targetX}`, maxY: Math.max(edge.sourceY, edge.targetY) };
    }
    const busX = Number.isFinite(busXOverride) ? busXOverride : actionFlowLateSplitBusX(sourceX, targetX);
    return {
      path: makeActionStageBranchPath(sourceX, edge.sourceY, targetX, edge.targetY, busX),
      maxY: Math.max(edge.sourceY, edge.targetY),
    };
  }

  const intermediateRects = stages
    .slice(edge.fromStage + 1, edge.toStage)
    .flatMap((stage) => stage.nodes.map((item) => item.rect));
  const crossesCard = (y) => intermediateRects.some((rect) => y >= rect.y - 4 && y <= rect.y + rect.height + 4);
  const canLateSplit = !crossesCard(edge.sourceY);
  if ((edge.fanOut || edge.fanIn || Math.abs(edge.sourceY - edge.targetY) <= 24) && canLateSplit) {
    const busX = Number.isFinite(busXOverride) ? busXOverride : actionFlowLateSplitBusX(sourceX, targetX);
    return {
      path: makeActionStageBranchPath(sourceX, edge.sourceY, targetX, edge.targetY, busX),
      maxY: Math.max(edge.sourceY, edge.targetY),
    };
  }

  if (edge.fanOut) {
    if (!crossesCard(edge.sourceY)) {
      return { path: makeActionConnectorPath(edge.sourceRect, edge.targetRect, targetBusX, edge.sourceY, edge.targetY), maxY: Math.max(edge.sourceY, edge.targetY) };
    }
    const laneY = findClearActionLane(intermediateRects, edge.sourceY);
    return {
      path: makeRoundedOrthogonalPath([
        { x: sourceX, y: edge.sourceY },
        { x: sourceBusX, y: edge.sourceY },
        { x: sourceBusX, y: laneY },
        { x: targetBusX, y: laneY },
        { x: targetBusX, y: edge.targetY },
        { x: targetX, y: edge.targetY },
      ]),
      maxY: Math.max(edge.sourceY, edge.targetY, laneY),
    };
  }
  if (!crossesCard(edge.sourceY)) {
    return { path: makeActionConnectorPath(edge.sourceRect, edge.targetRect, targetBusX, edge.sourceY, edge.targetY), maxY: Math.max(edge.sourceY, edge.targetY) };
  }
  if (!crossesCard(edge.targetY)) {
    return { path: makeActionConnectorPath(edge.sourceRect, edge.targetRect, sourceBusX, edge.sourceY, edge.targetY), maxY: Math.max(edge.sourceY, edge.targetY) };
  }

  const laneY = findClearActionLane(intermediateRects, (edge.sourceY + edge.targetY) / 2);
  return {
    path: makeRoundedOrthogonalPath([
      { x: sourceX, y: edge.sourceY },
      { x: sourceBusX, y: edge.sourceY },
      { x: sourceBusX, y: laneY },
      { x: targetBusX, y: laneY },
      { x: targetBusX, y: edge.targetY },
      { x: targetX, y: edge.targetY },
    ]),
    maxY: Math.max(edge.sourceY, edge.targetY, laneY),
  };
};

const buildActionCanvasLayout = (workflow, jobs, now, focusedDefinitionIds = null, focusMode = 'neighbors') => {
  const graph = buildWorkflowGraph(workflow, jobs, now);
  const definitionById = new Map(graph.definitions.map((definition) => [definition.id, definition]));
  const used = new Set();
  let layers = [];
  if (graph.fallback) {
    layers = graph.definitions.map((definition) => [definition]);
    graph.definitions.forEach((definition) => used.add(definition.id));
  } else if (Array.isArray(workflow?.layers) && workflow.layers.length > 0) {
    layers = workflow.layers
      .map((layer) => layer.filter((definition) => definitionById.has(definition.id)))
      .filter((layer) => layer.length > 0);
    layers.flat().forEach((definition) => used.add(definition.id));
  }
  graph.definitions.filter((definition) => !used.has(definition.id)).forEach((definition) => {
    const needs = (definition.needs || []).filter((need) => definitionById.has(need));
    let index = 0;
    needs.forEach((need) => {
      const parentIndex = layers.findIndex((layer) => layer.some((item) => item.id === need));
      if (parentIndex >= 0) index = Math.max(index, parentIndex + 1);
    });
    while (layers.length <= index) layers.push([]);
    layers[index].push(definition);
  });
  if (layers.length === 0) {
    layers = jobs.map((job) => [{ id: String(job.id), name: job.name, needs: [] }]);
  }

  const dependents = workflowDependentsById(graph.definitions);
  const descendantCounts = workflowDescendantCounts(graph.definitions, dependents);
  const orderById = new Map(graph.definitions.map((definition, index) => [definition.id, index]));
  const groupedLayers = layers.map((layer) => sortWorkflowGroups(groupWorkflowLayer(layer, dependents), orderById, dependents, descendantCounts));
  const visualIdByDefinition = new Map();
  const visualStageById = new Map();
  groupedLayers.forEach((layer) => {
    layer.forEach((definitions) => {
      const id = definitions.length === 1 ? definitions[0].id : `group-${definitions.map((definition) => definition.id).join('-')}`;
      definitions.forEach((definition) => visualIdByDefinition.set(definition.id, id));
    });
  });
  groupedLayers.forEach((layer, stageIndex) => {
    layer.forEach((definitions) => {
      const id = definitions.length === 1 ? definitions[0].id : `group-${definitions.map((definition) => definition.id).join('-')}`;
      visualStageById.set(id, stageIndex);
    });
  });
  const reducedDefinitionEdges = reduceWorkflowEdges(graph.definitions, dependents);
  const trace = workflowTrace(focusedDefinitionIds, reducedDefinitionEdges, focusMode);
  const highlightedVisualIds = trace
    ? new Set([...trace.nodes].map((id) => visualIdByDefinition.get(id)).filter(Boolean))
    : null;

  const stageItems = groupedLayers.map((layer) => layer.map((definitions) => {
    const id = definitions.length === 1 ? definitions[0].id : `group-${definitions.map((definition) => definition.id).join('-')}`;
    const node = summarizeWorkflowGroup(definitions, graph.summaries);
    const highlighted = highlightedVisualIds ? highlightedVisualIds.has(id) : null;
    return { id, definitions, node, highlighted, height: actionFlowNodeHeight(node, false) };
  }));
  const visualParents = new Map();
  const visualChildren = new Map();
  reducedDefinitionEdges.forEach((edge) => {
    const from = visualIdByDefinition.get(edge.from);
    const to = visualIdByDefinition.get(edge.to);
    if (!from || !to || from === to) return;
    if (!visualParents.has(to)) visualParents.set(to, new Set());
    if (!visualChildren.has(from)) visualChildren.set(from, new Set());
    visualParents.get(to).add(from);
    visualChildren.get(from).add(to);
  });
  const primaryLaneY = ACTION_FLOW_PADDING_Y + Math.max(0, ...stageItems.map((nodes) => (
    nodes[0] ? actionFlowPortY({ y: 0, height: nodes[0].height }) : 0
  )));
  const nodeRects = new Map();
  const stages = stageItems.map((nodes, stageIndex) => {
    const x = ACTION_FLOW_PADDING_X + stageIndex * (ACTION_FLOW_CARD_WIDTH + ACTION_FLOW_STAGE_GAP);
    const availableHeight = ACTION_FLOW_VIEWPORT_HEIGHT - ACTION_FLOW_PADDING_Y * 2;
    const desiredPorts = resolveActionStageDesiredPorts(nodes, visualParents, nodeRects, primaryLaneY);
    let previousBottom = ACTION_FLOW_PADDING_Y - ACTION_FLOW_ROW_GAP;
    const resolvedNodes = nodes.map((item, index) => {
      const portOffset = actionFlowPortY({ y: 0, height: item.height });
      const desiredPort = desiredPorts[index] ?? primaryLaneY;
      const alignedY = desiredPort - portOffset;
      const minimumY = previousBottom + ACTION_FLOW_ROW_GAP;
      const y = Math.max(ACTION_FLOW_PADDING_Y, minimumY, alignedY);
      const rect = { x, y, width: ACTION_FLOW_CARD_WIDTH, height: item.height };
      nodeRects.set(item.id, rect);
      previousBottom = y + item.height;
      return { ...item, rect };
    });
    const contentHeight = Math.max(0, previousBottom - ACTION_FLOW_PADDING_Y);
    return {
      stageIndex,
      nodes: resolvedNodes,
      height: Math.max(contentHeight, availableHeight),
    };
  });
  compactActionStages(stages, visualParents, nodeRects);
  alignAdjacentFanoutStages(stages, visualChildren, visualStageById, nodeRects);
  visualChildren.forEach((targetIds, sourceId) => {
    if (targetIds.size <= 1) return;
    const sourceStageIndex = visualStageById.get(sourceId);
    const targetStageIndexes = [...targetIds]
      .map((targetId) => visualStageById.get(targetId))
      .filter((stageIndex) => Number.isFinite(stageIndex) && stageIndex > sourceStageIndex);
    if (!Number.isFinite(sourceStageIndex) || targetStageIndexes.length <= 1) return;
    const firstTargetStage = Math.min(...targetStageIndexes);
    if (firstTargetStage - sourceStageIndex <= 1) return;
    const intermediateRects = stages
      .slice(sourceStageIndex + 1, firstTargetStage)
      .flatMap((stage) => stage.nodes.map((item) => item.rect));
    const sourceRect = nodeRects.get(sourceId);
    const sourceStage = stages[sourceStageIndex];
    const sourceNodeIndex = sourceStage?.nodes.findIndex((item) => item.id === sourceId) ?? -1;
    if (!sourceRect || !sourceStage || sourceNodeIndex < 0 || intermediateRects.length === 0) return;
    const sourceY = actionFlowPortY(sourceRect);
    const laneY = findClearActionLane(intermediateRects, sourceY);
    const delta = laneY - sourceY;
    if (Math.abs(delta) < 2) return;
    if (delta > 0) {
      sourceStage.nodes.slice(sourceNodeIndex).forEach((item) => {
        item.rect.y += delta;
      });
    } else {
      const movable = Math.min(-delta, Math.max(0, sourceStage.nodes[0].rect.y - ACTION_FLOW_PADDING_Y));
      sourceStage.nodes.slice(0, sourceNodeIndex + 1).forEach((item) => {
        item.rect.y -= movable;
      });
    }
    const lastRect = sourceStage.nodes[sourceStage.nodes.length - 1]?.rect;
    if (lastRect) sourceStage.height = Math.max(sourceStage.height, lastRect.y + lastRect.height - ACTION_FLOW_PADDING_Y);
  });
  compactActionStages(stages, visualParents, nodeRects);

  const candidateEdges = [];
  const seenEdges = new Map();
  const visibleDefinitionEdges = reducedDefinitionEdges.map((edge) => ({ ...edge }));

  visibleDefinitionEdges.forEach((definitionEdge) => {
    const from = visualIdByDefinition.get(definitionEdge.from);
    const to = visualIdByDefinition.get(definitionEdge.to);
    if (!from || !to || from === to || !nodeRects.has(from) || !nodeRects.has(to)) return;
    const fromStage = visualStageById.get(from);
    const toStage = visualStageById.get(to);
    if (!Number.isFinite(fromStage) || !Number.isFinite(toStage) || toStage <= fromStage) return;
    const edgeTrace = trace?.edges.get(`${definitionEdge.from}->${definitionEdge.to}`);
    const focusedPath = Boolean(edgeTrace);
    const key = `${from}->${to}`;
    const existing = seenEdges.get(key);
    if (existing) {
      existing.highlighted = existing.highlighted || focusedPath;
      return;
    }
    const sourceRect = nodeRects.get(from);
    const targetRect = nodeRects.get(to);
    const sourceStage = stages[fromStage];
    const targetStage = stages[toStage];
    const sourceNode = sourceStage?.nodes.find((item) => item.id === from);
    const targetNode = targetStage?.nodes.find((item) => item.id === to);
    candidateEdges.push({
      from,
      to,
      fromStage,
      toStage,
      sourceRect,
      targetRect,
      sourceNode,
      targetNode,
      fromDefinition: definitionEdge.from,
      toDefinition: definitionEdge.to,
      highlighted: focusedPath,
    });
    seenEdges.set(key, candidateEdges[candidateEdges.length - 1]);
  });
  const outgoing = new Map();
  const incoming = new Map();
  candidateEdges.forEach((edge) => {
    if (!outgoing.has(edge.from)) outgoing.set(edge.from, []);
    if (!incoming.has(edge.to)) incoming.set(edge.to, []);
    outgoing.get(edge.from).push(edge);
    incoming.get(edge.to).push(edge);
  });
  candidateEdges.forEach((edge) => {
    edge.fanOut = (outgoing.get(edge.from)?.length || 0) > 1;
    edge.fanIn = (incoming.get(edge.to)?.length || 0) > 1;
  });
  straightenActionConnectorStages(candidateEdges, stages);
  const rightAnchors = new Map();
  const leftAnchors = new Map();
  nodeRects.forEach((rect, id) => {
    rightAnchors.set(id, actionFlowPortY(rect));
    leftAnchors.set(id, actionFlowPortY(rect));
  });
  const canPlaceAnchor = (rect, y) => y >= rect.y + 4 && y <= rect.y + rect.height - 4;
  incoming.forEach((items, id) => {
    const rect = nodeRects.get(id);
    const current = leftAnchors.get(id);
    const candidates = items
      .map((edge) => rightAnchors.get(edge.from))
      .filter((y) => Number.isFinite(y) && canPlaceAnchor(rect, y) && Math.abs(y - current) <= 28)
      .sort((a, b) => Math.abs(a - current) - Math.abs(b - current));
    if (candidates.length > 0) leftAnchors.set(id, candidates[0]);
  });
  outgoing.forEach((items, id) => {
    const rect = nodeRects.get(id);
    const current = rightAnchors.get(id);
    const candidates = items
      .map((edge) => leftAnchors.get(edge.to))
      .filter((y) => Number.isFinite(y) && canPlaceAnchor(rect, y) && Math.abs(y - current) <= 28)
      .sort((a, b) => Math.abs(a - current) - Math.abs(b - current));
    if (candidates.length > 0) rightAnchors.set(id, candidates[0]);
  });
  outgoing.forEach((items, id) => {
    if (items.length <= 1) return;
    const sourceY = rightAnchors.get(id);
    const primaryEdge = [...items].sort((left, right) => (
      (left.toStage - left.fromStage) - (right.toStage - right.fromStage)
      || Math.abs(actionFlowPortY(left.targetRect) - sourceY) - Math.abs(actionFlowPortY(right.targetRect) - sourceY)
    ))[0];
    if (primaryEdge && canPlaceAnchor(primaryEdge.targetRect, sourceY)) {
      leftAnchors.set(primaryEdge.to, sourceY);
    }
  });
  incoming.forEach((items, id) => {
    if (items.length <= 1) return;
    const targetY = leftAnchors.get(id);
    const primaryEdge = [...items].sort((left, right) => (
      (left.toStage - left.fromStage) - (right.toStage - right.fromStage)
      || Math.abs(actionFlowPortY(left.sourceRect) - targetY) - Math.abs(actionFlowPortY(right.sourceRect) - targetY)
    ))[0];
    if (primaryEdge && canPlaceAnchor(primaryEdge.sourceRect, targetY)) {
      rightAnchors.set(primaryEdge.from, targetY);
    }
  });
  candidateEdges.forEach((edge) => {
    const sourceY = rightAnchors.get(edge.from);
    const targetY = leftAnchors.get(edge.to);
    if (Math.abs(targetY - sourceY) > 28) return;
    const sourceSingle = outgoing.get(edge.from)?.length === 1;
    const targetSingle = incoming.get(edge.to)?.length === 1;
    if (sourceSingle && targetSingle) {
      const overlapTop = Math.max(edge.sourceRect.y + 4, edge.targetRect.y + 4);
      const overlapBottom = Math.min(edge.sourceRect.y + edge.sourceRect.height - 4, edge.targetRect.y + edge.targetRect.height - 4);
      if (overlapTop <= overlapBottom) {
        const lane = Math.max(overlapTop, Math.min(overlapBottom, (sourceY + targetY) / 2));
        rightAnchors.set(edge.from, lane);
        leftAnchors.set(edge.to, lane);
      }
    } else if (sourceSingle && canPlaceAnchor(edge.sourceRect, targetY)) {
      rightAnchors.set(edge.from, targetY);
    } else if (targetSingle && canPlaceAnchor(edge.targetRect, sourceY)) {
      leftAnchors.set(edge.to, sourceY);
    }
  });
  candidateEdges.forEach((edge) => {
    edge.sourceY = rightAnchors.get(edge.from);
    edge.targetY = leftAnchors.get(edge.to);
  });
  const edgeBusXMap = new Map();
  candidateEdges.forEach((edge) => {
    edgeBusXMap.set(edge, resolveActionConnectorBusX(edge, stages));
  });

  outgoing.forEach((edgesFromSource) => {
    if (edgesFromSource.length <= 1) return;
    const adjacentEdges = edgesFromSource.filter((edge) => edge.toStage - edge.fromStage <= 1);
    if (adjacentEdges.length <= 1) return;
    const sampleEdge = adjacentEdges[0];
    const sourceX = sampleEdge.sourceRect.x + sampleEdge.sourceRect.width;
    const targetX = sampleEdge.targetRect.x;
    const unifiedBusX = sourceX + Math.min(ACTION_FLOW_BRANCH_INSET, (targetX - sourceX) * 0.45);
    adjacentEdges.forEach((edge) => edgeBusXMap.set(edge, unifiedBusX));
  });

  incoming.forEach((edgesToTarget) => {
    if (edgesToTarget.length <= 1) return;
    const adjacentEdges = edgesToTarget.filter((edge) => edge.toStage - edge.fromStage <= 1);
    if (adjacentEdges.length <= 1) return;
    const sampleEdge = adjacentEdges[0];
    const sourceX = sampleEdge.sourceRect.x + sampleEdge.sourceRect.width;
    const targetX = sampleEdge.targetRect.x;
    const unifiedBusX = targetX - Math.min(ACTION_FLOW_BRANCH_INSET, (targetX - sourceX) * 0.45);
    adjacentEdges.forEach((edge) => {
      if (!edge.fanOut) {
        edgeBusXMap.set(edge, unifiedBusX);
      }
    });
  });

  const edges = candidateEdges.map((edge) => {
    const route = routeActionConnector(edge, stages, edgeBusXMap.get(edge));
    return {
      from: edge.from,
      to: edge.to,
      sourceX: edge.sourceRect.x + edge.sourceRect.width,
      targetX: edge.targetRect.x,
      sourceY: edge.sourceY,
      targetY: edge.targetY,
      highlighted: edge.highlighted,
      path: route.path,
      maxY: route.maxY,
    };
  });
  const width = ACTION_FLOW_PADDING_X * 2 + Math.max(1, groupedLayers.length) * ACTION_FLOW_CARD_WIDTH + Math.max(0, groupedLayers.length - 1) * ACTION_FLOW_STAGE_GAP;
  const height = Math.max(
    ACTION_FLOW_MIN_VIEWPORT_HEIGHT,
    ACTION_FLOW_PADDING_Y + Math.max(
      0,
      ...stages.flatMap((stage) => stage.nodes.map((item) => item.rect.y + item.rect.height)),
      ...edges.map((edge) => edge.maxY),
    ),
  );
  const resolvedStages = highlightedVisualIds
    ? stages.map((stage) => ({
      ...stage,
      nodes: stage.nodes.map((item) => ({ ...item, highlighted: highlightedVisualIds.has(item.id) })),
    }))
    : stages;
  return { graph, stages: resolvedStages, edges, width, height, hasFocus: Boolean(trace) };
};

export {
  actionFlowNodeHeight,
  actionFlowPortY,
  buildActionCanvasLayout,
};
