import {
  ACTION_FLOW_BRANCH_INSET,
  ACTION_FLOW_CARD_WIDTH,
  ACTION_FLOW_MIN_VIEWPORT_HEIGHT,
  ACTION_FLOW_PADDING_X,
  ACTION_FLOW_PADDING_Y,
  ACTION_FLOW_ROW_GAP,
  ACTION_FLOW_STAGE_GAP,
  ACTION_FLOW_VIEWPORT_HEIGHT,
} from './constants.js';
import {
  actionFlowNodeHeight,
  actionFlowPortY,
  alignAdjacentFanoutStages,
  buildWorkflowGraph,
  compactActionStages,
  findClearActionLane,
  groupWorkflowLayer,
  reduceWorkflowEdges,
  resolveActionConnectorBusX,
  resolveActionStageDesiredPorts,
  routeActionConnector,
  sortWorkflowGroups,
  straightenActionConnectorStages,
  summarizeWorkflowGroup,
  summarizeWorkflowJobs,
  workflowDependentsById,
  workflowDescendantCounts,
  workflowTrace,
} from './utils.js';

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

export { buildActionCanvasLayout };