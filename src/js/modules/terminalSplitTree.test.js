import { describe, expect, it } from 'vitest';
import {
  buildBalancedGridTree,
  buildPresetLayout,
  createBranchNode,
  createLeafNode,
  getDropTargetRegion,
  getTreeSessionIds,
  insertSplitIntoTree,
  normalizeTree,
  removeSessionFromTree,
  replaceSessionInTree,
  updateBranchRatio,
} from './terminalSplitTree.js';

describe('terminalSplitTree', () => {
  describe('getDropTargetRegion', () => {
    const rect = {
      left: 100,
      top: 100,
      width: 400,
      height: 200,
    };

    it('identifies center region when pointer is inside inner area', () => {
      // clientX = 300 (50%), clientY = 200 (50%) -> center
      expect(getDropTargetRegion(rect, 300, 200)).toBe('center');
    });

    it('identifies top edge region', () => {
      // y = (110 - 100) / 200 = 0.05 < 0.25 -> top
      expect(getDropTargetRegion(rect, 300, 110)).toBe('top');
    });

    it('identifies bottom edge region', () => {
      // y = (290 - 100) / 200 = 0.95 (1 - y = 0.05) < 0.25 -> bottom
      expect(getDropTargetRegion(rect, 300, 290)).toBe('bottom');
    });

    it('identifies left edge region', () => {
      // x = (110 - 100) / 400 = 0.025 < 0.25 -> left
      expect(getDropTargetRegion(rect, 110, 200)).toBe('left');
    });

    it('identifies right edge region', () => {
      // x = (480 - 100) / 400 = 0.95 -> right
      expect(getDropTargetRegion(rect, 480, 200)).toBe('right');
    });
  });

  describe('tree manipulation', () => {
    it('creates leaf and extracts sessionIds', () => {
      const leaf = createLeafNode('s1');
      expect(getTreeSessionIds(leaf)).toEqual(['s1']);
    });

    it('inserts split to right of leaf', () => {
      const root = createLeafNode('s1');
      const updated = insertSplitIntoTree(root, 's1', 's2', 'right');
      expect(updated.type).toBe('branch');
      expect(updated.direction).toBe('h');
      expect(updated.first.id).toBe('s1');
      expect(updated.second.id).toBe('s2');
      expect(getTreeSessionIds(updated)).toEqual(['s1', 's2']);
    });

    it('inserts split to top of leaf', () => {
      const root = createLeafNode('s1');
      const updated = insertSplitIntoTree(root, 's1', 's2', 'top');
      expect(updated.type).toBe('branch');
      expect(updated.direction).toBe('v');
      expect(updated.first.id).toBe('s2');
      expect(updated.second.id).toBe('s1');
      expect(getTreeSessionIds(updated)).toEqual(['s2', 's1']);
    });

    it('replaces leaf when dropped in center', () => {
      const root = createLeafNode('s1');
      const updated = insertSplitIntoTree(root, 's1', 's2', 'center');
      expect(updated.type).toBe('leaf');
      expect(updated.id).toBe('s2');
    });

    it('removes session from tree and simplifies redundant branch', () => {
      const branch = createBranchNode('h', createLeafNode('s1'), createLeafNode('s2'), 0.5);
      const afterRemoval = removeSessionFromTree(branch, 's1');
      expect(afterRemoval.type).toBe('leaf');
      expect(afterRemoval.id).toBe('s2');
    });

    it('normalizes tree with invalid sessions list', () => {
      const branch = createBranchNode('h', createLeafNode('s1'), createLeafNode('ghost'), 0.5);
      const normalized = normalizeTree(branch, ['s1']);
      expect(normalized.type).toBe('leaf');
      expect(normalized.id).toBe('s1');
    });

    it('updates branch ratio within bounds', () => {
      const branch = createBranchNode('h', createLeafNode('s1'), createLeafNode('s2'), 0.5);
      const updated = updateBranchRatio(branch, [''], 0.7);
      expect(updated.ratio).toBe(0.7);
      // boundary test
      const clamped = updateBranchRatio(branch, [''], 0.98);
      expect(clamped.ratio).toBe(0.9);
    });
  });

  describe('buildPresetLayout', () => {
    it('creates single leaf for 1 id or single preset', () => {
      expect(buildPresetLayout(['s1', 's2'], 'single', 's2')).toEqual({
        type: 'leaf',
        id: 's2',
      });
    });

    it('creates 50/50 horizontal split for 2 ids', () => {
      const tree = buildPresetLayout(['s1', 's2'], 'grid');
      expect(tree.direction).toBe('h');
      expect(tree.ratio).toBe(0.5);
      expect(getTreeSessionIds(tree)).toEqual(['s1', 's2']);
    });

    it('creates master-stack layout for 3 ids', () => {
      const tree = buildPresetLayout(['s1', 's2', 's3'], 'master-stack', 's1');
      expect(tree.direction).toBe('h');
      expect(tree.ratio).toBe(0.62);
      expect(tree.first.id).toBe('s1');
      expect(tree.second.direction).toBe('v');
      expect(getTreeSessionIds(tree.second)).toEqual(['s2', 's3']);
    });

    it('creates master-stack layout for 2 ids (must not fall back to 50/50)', () => {
      const tree = buildPresetLayout(['s1', 's2'], 'master-stack', 's1');
      expect(tree.type).toBe('branch');
      expect(tree.direction).toBe('h');
      expect(tree.ratio).toBe(0.62);
      expect(tree.first.id).toBe('s1');
      expect(tree.second.id).toBe('s2');
      expect(getTreeSessionIds(tree)).toEqual(['s1', 's2']);
    });

    it('creates balanced 3-window grid (品字形)', () => {
      const tree = buildBalancedGridTree(['s1', 's2', 's3']);
      expect(tree.direction).toBe('h');
      expect(tree.first.id).toBe('s1');
      expect(tree.second.direction).toBe('v');
      expect(tree.second.first.id).toBe('s2');
      expect(tree.second.second.id).toBe('s3');
    });
  });
});
