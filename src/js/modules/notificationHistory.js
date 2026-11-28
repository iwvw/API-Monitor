const parseHistoryData = (rawData) => {
  if (rawData && typeof rawData === 'object') return rawData;
  if (typeof rawData !== 'string' || !rawData.trim()) return {};
  try {
    const parsed = JSON.parse(rawData);
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch (_) {
    return {};
  }
};

export const parseLifecycleHistoryMeta = (rawData) => {
  const data = parseHistoryData(rawData);
  if (!data.lifecycleKind) return null;
  return {
    mutation: data.lifecycleMutation || data.lifecyclePhase || '',
    kind: data.lifecycleKind,
    resourceKey: data.lifecycleResourceKey || '',
    duration: data.downDuration || '',
    changedFields: Object.keys(data.lifecycleChanges || {}),
  };
};

const toHistoryTimestamp = (value, fallback = 0) => {
  if (!value) return fallback;
  const text = String(value);
  const normalized = /^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(\.\d+)?$/.test(text)
    ? `${text.replace(' ', 'T')}Z`
    : text;
  const parsed = Date.parse(normalized);
  return Number.isFinite(parsed) ? parsed : fallback;
};

export const collapseNotificationHistory = (history = []) => {
  const sorted = [...history].sort((a, b) => {
    const tsDiff = toHistoryTimestamp(b?.created_at) - toHistoryTimestamp(a?.created_at);
    if (tsDiff !== 0) return tsDiff;
    return Number(b?.id || 0) - Number(a?.id || 0);
  });
  const collapsed = [];
  const lifecycleGroups = new Map();

  sorted.forEach((item) => {
    const lifecycleMeta = parseLifecycleHistoryMeta(item?.data);
    if (!lifecycleMeta?.resourceKey || !lifecycleMeta.kind) {
      collapsed.push({
        ...item,
        lifecycle_meta: lifecycleMeta,
        lifecycle_update_count: lifecycleMeta ? 1 : 0,
        lifecycle_first_created_at: item?.created_at || '',
      });
      return;
    }

    const groupKey = [
      item?.rule_id || '',
      item?.channel_id || '',
      lifecycleMeta.kind,
      lifecycleMeta.resourceKey,
    ].join('::');
    const existing = lifecycleGroups.get(groupKey);

    if (!existing) {
      const next = {
        ...item,
        lifecycle_meta: lifecycleMeta,
        lifecycle_update_count: 1,
        lifecycle_first_created_at: item?.created_at || '',
      };
      lifecycleGroups.set(groupKey, next);
      collapsed.push(next);
      return;
    }

    existing.lifecycle_update_count += 1;
    existing.lifecycle_first_created_at = item?.created_at || existing.lifecycle_first_created_at;
  });

  return collapsed;
};
