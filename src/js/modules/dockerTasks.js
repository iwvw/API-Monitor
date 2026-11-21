const parseJSONMessage = (message) => {
  if (typeof message !== 'string') return message;
  const trimmed = message.trim();
  if (!trimmed.startsWith('[') && !trimmed.startsWith('{')) return null;
  try {
    return JSON.parse(trimmed);
  } catch {
    return null;
  }
};

const asResultList = (message) => {
  const parsed = parseJSONMessage(message);
  if (parsed == null) return null;
  return Array.isArray(parsed) ? parsed : [parsed];
};

const getResultError = (result = {}) => String(result?.error || '').trim();

const getResultContainerName = (result = {}) => String(
  result?.container_name || result?.containerName || result?.name || '容器'
).replace(/^\/+/, '');

const isUpdateResult = (result = {}) => Boolean(result?.has_update ?? result?.hasUpdate);

const isSuccessfulState = (state) => ['success', 'succeeded'].includes(String(state || '').toLowerCase());

export const normalizeDockerTaskResult = (task = {}) => {
  if (task.action !== 'container.checkUpdates' || !isSuccessfulState(task.state)) return task;
  const results = asResultList(task.message);
  if (!results) return task;
  const failed = results.filter(result => getResultError(result));
  if (failed.length === 0) return task;

  const first = failed[0];
  return {
    ...task,
    state: 'failed',
    error: `${getResultContainerName(first)}: ${getResultError(first)}`,
  };
};

export const summarizeDockerTaskMessage = (task = {}) => {
  const raw = String(task.message || '');
  if (!raw) return '';
  const results = asResultList(raw);
  if (!results) return raw.length > 96 ? `${raw.slice(0, 96)}...` : raw;

  const failedCount = results.filter(result => getResultError(result)).length;
  const updateCount = results.filter(result => isUpdateResult(result)).length;
  const isUpdateCheck = task.action === 'container.checkUpdates'
    || results.some(result => result?.container_name || result?.containerName);
  if (isUpdateCheck) {
    if (failedCount > 0) {
      return `${failedCount} 个容器检测失败${updateCount > 0 ? `，另有 ${updateCount} 个可更新` : ''}`;
    }
    return updateCount > 0 ? `发现 ${updateCount} 个可更新容器` : `已检查 ${results.length} 个容器`;
  }
  return `共 ${results.length} 条结果`;
};

export const formatDockerPruneResult = (action, data = {}) => {
  const raw = data?.data?.message ?? data?.message ?? '';
  const parsed = parseJSONMessage(raw);
  const result = parsed ?? raw;
  if (!result || typeof result !== 'object') return String(raw || '清理完成');

  const deleted = Number(result.deleted) || 0;
  const reclaimed = String(result.reclaimed || '').trim();
  if (action === 'image.prune') return `已清理 ${deleted} 个镜像${reclaimed ? `，释放 ${reclaimed}` : ''}`;
  if (action === 'network.prune') return `已清理 ${deleted} 个网络`;
  if (action === 'volume.prune') return `已清理 ${deleted} 个存储卷${reclaimed ? `，释放 ${reclaimed}` : ''}`;
  return '清理完成';
};

const imageRepository = (image = {}) => {
  const repoTags = Array.isArray(image.RepoTags || image.repoTags) ? (image.RepoTags || image.repoTags) : [];
  const firstRepoTag = repoTags.find(Boolean) || '';
  const fromTag = firstRepoTag && firstRepoTag !== '<none>:<none>'
    ? firstRepoTag.split(':').slice(0, -1).join(':')
    : '';
  return image.repository || image.Repository || image.repo || image.Repo || fromTag || '<none>';
};

const imageTag = (image = {}) => {
  const repoTags = Array.isArray(image.RepoTags || image.repoTags) ? (image.RepoTags || image.repoTags) : [];
  const firstRepoTag = repoTags.find(Boolean) || '';
  if (image.tag || image.Tag) return image.tag || image.Tag;
  if (firstRepoTag.includes('@')) return '-';
  const lastSlash = firstRepoTag.lastIndexOf('/');
  const lastColon = firstRepoTag.lastIndexOf(':');
  if (lastColon > lastSlash) return firstRepoTag.slice(lastColon + 1);
  return '-';
};

export const isDockerImagePruneCandidate = (image = {}) => {
  if (typeof image.dangling === 'boolean') return image.dangling;
  if (typeof image.Dangling === 'boolean') return image.Dangling;
  return imageRepository(image) === '<none>' || imageTag(image) === '<none>';
};
