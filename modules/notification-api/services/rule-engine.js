function getPathValue(source, pathExpression) {
  const expression = String(pathExpression || '').trim();
  if (!expression) return source;
  return expression
    .replace(/^\$\./, '')
    .replace(/^\$/, '')
    .replace(/\[(\d+)\]/g, '.$1')
    .split('.')
    .map(part => part.trim())
    .filter(Boolean)
    .reduce((value, segment) => {
      if (value === null || value === undefined) return undefined;
      return value[segment];
    }, source);
}

function coerce(value) {
  if (value === null || value === undefined) return value;
  if (typeof value === 'boolean' || typeof value === 'number') return value;
  const text = String(value).trim();
  if (/^-?\d+(\.\d+)?$/.test(text)) return Number(text);
  if (text === 'true') return true;
  if (text === 'false') return false;
  if (text === 'null') return null;
  return text;
}

function compare(actual, expected, operator = 'equals') {
  const op = operator || 'equals';
  if (op === 'exists') return actual !== undefined && actual !== null;
  if (op === 'not_exists' || op === 'notExists') return actual === undefined || actual === null;

  const left = coerce(actual);
  const right = coerce(expected);
  const actualText = typeof actual === 'string' ? actual : JSON.stringify(actual);
  const expectedText = String(expected ?? '');

  switch (op) {
    case 'equals':
    case 'eq':
      return left === right;
    case 'not_equals':
    case 'notEquals':
    case 'ne':
      return left !== right;
    case 'contains':
      return String(actualText ?? '').includes(expectedText);
    case 'not_contains':
    case 'notContains':
      return !String(actualText ?? '').includes(expectedText);
    case 'gt':
    case 'greater_than':
    case 'greaterThan':
      return Number(left) > Number(right);
    case 'gte':
    case 'greater_or_equal':
    case 'greaterOrEqual':
      return Number(left) >= Number(right);
    case 'lt':
    case 'less_than':
    case 'lessThan':
      return Number(left) < Number(right);
    case 'lte':
    case 'less_or_equal':
    case 'lessOrEqual':
      return Number(left) <= Number(right);
    case 'regex':
      return new RegExp(expectedText).test(String(actualText ?? ''));
    default:
      return left === right;
  }
}

function normalizeConditions(conditions = {}) {
  if (!conditions || (typeof conditions === 'object' && Object.keys(conditions).length === 0)) {
    return { mode: 'all', items: [] };
  }

  if (Array.isArray(conditions)) return { mode: 'all', items: conditions };

  const mode = conditions.mode || conditions.match || conditions.operator || 'all';
  const items = conditions.items || conditions.rules || conditions.conditions;
  if (Array.isArray(items)) {
    return { mode: mode === 'any' || mode === 'or' ? 'any' : 'all', items };
  }

  const entries = Object.entries(conditions)
    .filter(([key]) => !['mode', 'match', 'operator'].includes(key))
    .map(([field, value]) => ({ field, operator: 'equals', value }));

  return { mode: mode === 'any' || mode === 'or' ? 'any' : 'all', items: entries };
}

function evaluateConditions(conditions = {}, eventData = {}) {
  const { mode, items } = normalizeConditions(conditions);
  if (items.length === 0) {
    return { allowed: true, mode, results: [] };
  }

  const results = items.map(item => {
    const field = item.field || item.key || item.path || item.name;
    const operator = item.operator || item.op || 'equals';
    const expected = item.value ?? item.expected ?? item.equals;
    const actual = getPathValue(eventData, field);
    const passed = compare(actual, expected, operator);
    return { field, operator, expected, actual, passed };
  });

  const allowed = mode === 'any'
    ? results.some(result => result.passed)
    : results.every(result => result.passed);

  return { allowed, mode, results };
}

function checkTimeWindow(timeWindow = {}) {
  if (!timeWindow.enabled) return true;

  const now = new Date();
  const [startHour, startMin] = String(timeWindow.start || '00:00').split(':').map(Number);
  const [endHour, endMin] = String(timeWindow.end || '23:59').split(':').map(Number);
  const currentMinutes = now.getHours() * 60 + now.getMinutes();
  const startMinutes = startHour * 60 + startMin;
  const endMinutes = endHour * 60 + endMin;

  if (!Number.isFinite(startMinutes) || !Number.isFinite(endMinutes)) return true;
  if (endMinutes < startMinutes) {
    return currentMinutes >= startMinutes || currentMinutes <= endMinutes;
  }
  return currentMinutes >= startMinutes && currentMinutes <= endMinutes;
}

module.exports = {
  getPathValue,
  compare,
  normalizeConditions,
  evaluateConditions,
  checkTimeWindow,
};
