const fixedRole = (minWidth, idealWidth, maxWidth, align = 'left') => ({
  minWidth,
  idealWidth,
  maxWidth,
  grow: 0,
  align,
  verticalAlign: 'middle',
});

const flexibleRole = (minWidth, idealWidth, align = 'left', verticalAlign = 'middle') => ({
  minWidth,
  idealWidth,
  maxWidth: null,
  grow: 1,
  align,
  verticalAlign,
});

export const TABLE_COLUMN_ROLES = Object.freeze({
  check: fixedRole(40, 40, 40, 'center'),
  control: fixedRole(48, 56, 64, 'center'),
  status: fixedRole(72, 88, 112, 'center'),
  type: fixedRole(80, 96, 128, 'center'),
  count: fixedRole(72, 88, 112, 'right'),
  number: fixedRole(88, 112, 144, 'right'),
  date: fixedRole(104, 120, 144),
  datetime: fixedRole(136, 160, 184),
  meta: fixedRole(104, 136, 176),
  'actions-sm': fixedRole(56, 72, 80, 'center'),
  'actions-md': fixedRole(96, 120, 144, 'center'),
  'actions-lg': fixedRole(144, 184, 240, 'center'),
  'actions-xl': fixedRole(280, 340, 400, 'center'),
  primary: flexibleRole(160, 240),
  content: flexibleRole(200, 320, 'left', 'top'),
  identifier: flexibleRole(176, 240),
});

const VALID_ALIGNMENTS = new Set(['left', 'center', 'right']);
const VALID_VERTICAL_ALIGNMENTS = new Set(['middle', 'top']);
const VALID_HIDE_BREAKPOINTS = new Set(['sm', 'md']);

function positiveNumber(value, fallback) {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : fallback;
}

function columnIndexes(columns, predicate) {
  const indexes = columns
    .map((column, index) => (predicate(column) ? index + 1 : null))
    .filter(Boolean);
  return indexes.length > 0 ? indexes.join(' ') : undefined;
}

function normalizeColumn(input, index, warnings) {
  const source = typeof input === 'string' ? { role: input } : (input || {});
  const role = source.role || 'primary';
  const roleDefaults = TABLE_COLUMN_ROLES[role];
  if (!roleDefaults) {
    throw new Error(`Unknown table column role: ${role}`);
  }

  const id = String(source.id || `${role}-${index + 1}`);
  const minWidth = positiveNumber(source.minWidth, roleDefaults.minWidth);
  const configuredMax = source.maxWidth === null
    ? null
    : positiveNumber(source.maxWidth, roleDefaults.maxWidth);
  const maxWidth = configuredMax === null ? null : Math.max(configuredMax, minWidth);
  const grow = source.grow === undefined
    ? roleDefaults.grow
    : Math.max(Number(source.grow) || 0, 0);
  const explicitWidth = source.width === undefined || source.width === null
    ? null
    : positiveNumber(source.width, roleDefaults.idealWidth);
  const preferredWidth = explicitWidth ?? roleDefaults.idealWidth;
  const clampedWidth = Math.max(minWidth, maxWidth === null ? preferredWidth : Math.min(preferredWidth, maxWidth));
  const width = explicitWidth !== null || grow === 0 ? clampedWidth : null;

  if (explicitWidth !== null && explicitWidth !== clampedWidth) {
    warnings.push({
      code: 'width-clamped',
      columnId: id,
      requestedWidth: explicitWidth,
      resolvedWidth: clampedWidth,
    });
  }

  const align = VALID_ALIGNMENTS.has(source.align) ? source.align : roleDefaults.align;
  const verticalAlign = VALID_VERTICAL_ALIGNMENTS.has(source.verticalAlign)
    ? source.verticalAlign
    : roleDefaults.verticalAlign;
  const hideBelow = VALID_HIDE_BREAKPOINTS.has(source.hideBelow) ? source.hideBelow : null;

  if (hideBelow && !source.hasAlternate) {
    warnings.push({
      code: 'hidden-column-without-alternate',
      columnId: id,
      hideBelow,
    });
  }

  return {
    id,
    role,
    minWidth,
    idealWidth: roleDefaults.idealWidth,
    maxWidth,
    width,
    grow,
    align,
    verticalAlign,
    hideBelow,
    sticky: source.sticky || null,
    resizable: Boolean(source.resizable),
  };
}

export function resolveTableColumns(columnSpecs = []) {
  if (!Array.isArray(columnSpecs) || columnSpecs.length === 0) {
    return {
      columns: [],
      minWidth: 0,
      dataAttributes: {},
      warnings: [],
    };
  }

  const warnings = [];
  const columns = columnSpecs.map((column, index) => normalizeColumn(column, index, warnings));
  const ids = new Set();
  columns.forEach((column) => {
    if (ids.has(column.id)) {
      throw new Error(`Duplicate table column id: ${column.id}`);
    }
    ids.add(column.id);
  });

  if (!columns.some((column) => column.width === null && column.grow > 0)) {
    warnings.push({ code: 'missing-flexible-column' });
  }

  const fixedWidth = columns.reduce(
    (total, column) => total + (column.width ?? 0),
    0
  );
  const flexibleColumns = columns.filter((column) => column.width === null && column.grow > 0);
  const widestFlexibleMinimum = flexibleColumns.reduce(
    (maximum, column) => Math.max(maximum, column.minWidth),
    0
  );
  const minWidth = fixedWidth + (widestFlexibleMinimum * flexibleColumns.length);

  return {
    columns,
    minWidth,
    dataAttributes: {
      'data-app-table-align-center': columnIndexes(columns, (column) => column.align === 'center'),
      'data-app-table-align-right': columnIndexes(columns, (column) => column.align === 'right'),
      'data-app-table-valign-top': columnIndexes(columns, (column) => column.verticalAlign === 'top'),
      'data-app-table-hide-sm': columnIndexes(columns, (column) => column.hideBelow === 'sm'),
      'data-app-table-hide-md': columnIndexes(columns, (column) => column.hideBelow === 'md'),
      'data-app-table-action-columns': columnIndexes(columns, (column) => column.role.startsWith('actions-')),
    },
    warnings,
  };
}

// 弹性列按 grow 权重分配剩余空间；grow 缺省视为 1（保持既有「均分」行为）。
function effectiveGrow(column) {
  return column.grow > 0 ? column.grow : 1;
}

/**
 * allocateColumnWidths 按容器可用宽度算出每列的实际像素宽度。
 *
 * 分配模型（table-layout: fixed 的硬约束决定必须用像素而非 calc 百分比）：
 *   1. 固定列（grow=0）：width = clamp(ideal, min, max)，不随表宽伸缩；
 *   2. 弹性池 = max(0, containerWidth - Σ固定列)；
 *   3. 弹性列：按 grow 权重分弹性池，再各自 clamp(min, max)；
 *   4. 容器 < minWidth 时整体按 minWidth 铺开（外层 overflow-x-auto 横向滚动）。
 *
 * 返回与 columns 等长的像素数组；不可分配时返回 null（调用方回退到默认行为）。
 *
 * @param {{columns: Array, minWidth: number}} layout resolveTableColumns 的结果
 * @param {number} containerWidth 表格容器可用宽度（px）
 * @returns {number[]|null}
 */
export function allocateColumnWidths(layout, containerWidth) {
  const columns = layout?.columns || [];
  if (columns.length === 0) return null;
  const flexibleColumns = columns.filter((column) => column.width === null && column.grow > 0);
  // 没有弹性列时交给浏览器按 width:100% 分配，不接管（保持既有行为）。
  if (flexibleColumns.length === 0) return null;
  const available = Math.max(
    layout.minWidth || 0,
    Math.floor(Number(containerWidth) || 0)
  );
  if (available <= 0) return null;

  const fixedColumns = columns.filter((column) => column.width !== null);
  const fixedTotal = fixedColumns.reduce((total, column) => total + column.width, 0);
  const pool = Math.max(0, available - fixedTotal);

  // 弹性列按权重分池，但不得低于各自的 minWidth 或高于 maxWidth。
  // 先按权重算理想值，再夹紧；夹紧后若仍有余额，留给未触顶的列。
  const growTotal = flexibleColumns.reduce((total, column) => total + effectiveGrow(column), 0) || 1;
  const flexibleWidths = new Map();
  let remaining = pool;
  let remainingGrow = growTotal;
  const unpinned = [...flexibleColumns];
  // 最多迭代列数轮：每轮解决一批触顶/触底的列。
  for (let pass = 0; pass < flexibleColumns.length + 1 && unpinned.length > 0; pass += 1) {
    const share = remainingGrow > 0 ? remaining / remainingGrow : 0;
    let pinnedThisPass = false;
    for (let i = unpinned.length - 1; i >= 0; i -= 1) {
      const column = unpinned[i];
      const ideal = share * effectiveGrow(column);
      const lower = column.minWidth;
      const upper = column.maxWidth === null ? Infinity : column.maxWidth;
      if (ideal < lower) {
        flexibleWidths.set(column.id, lower);
        remaining -= lower;
        remainingGrow -= effectiveGrow(column);
        unpinned.splice(i, 1);
        pinnedThisPass = true;
      } else if (ideal > upper) {
        flexibleWidths.set(column.id, upper);
        remaining -= upper;
        remainingGrow -= effectiveGrow(column);
        unpinned.splice(i, 1);
        pinnedThisPass = true;
      }
    }
    if (!pinnedThisPass) break;
  }
  for (const column of unpinned) {
    const share = remainingGrow > 0 ? remaining / remainingGrow : 0;
    flexibleWidths.set(column.id, Math.max(0, Math.floor(share * effectiveGrow(column))));
  }

  const result = columns.map((column) => {
    if (column.width !== null) return column.width;
    return flexibleWidths.get(column.id) ?? column.minWidth;
  });

  // 修正整数取整造成的尾差，把差额补到最后一个弹性列，保证总宽等于 available。
  const total = result.reduce((sum, value) => sum + value, 0);
  if (flexibleColumns.length > 0 && total !== available) {
    const lastFlexible = flexibleColumns[flexibleColumns.length - 1];
    const index = columns.indexOf(lastFlexible);
    if (index >= 0) result[index] = Math.max(lastFlexible.minWidth, result[index] + (available - total));
  }
  return result;
}
