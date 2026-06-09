async function parseResponse(response) {
  const data = await response.json().catch(() => ({}));
  if (!response.ok || data.success === false) {
    throw new Error(data.error || '快速命令请求失败');
  }
  return data;
}

export async function fetchCommandSnippets(filters = {}) {
  const params = new URLSearchParams();
  Object.entries(filters).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') params.set(key, value);
  });
  const response = await fetch(`/api/server/snippets?${params.toString()}`);
  return parseResponse(response);
}

export async function createCommandSnippet(payload) {
  const response = await fetch('/api/server/snippets', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  return parseResponse(response);
}

export async function updateCommandSnippet(id, payload) {
  const response = await fetch(`/api/server/snippets/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  return parseResponse(response);
}

export async function deleteCommandSnippet(id) {
  const response = await fetch(`/api/server/snippets/${id}`, {
    method: 'DELETE',
  });
  return parseResponse(response);
}

export async function previewCommand(payload) {
  const response = await fetch('/api/server/snippets/preview', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  return parseResponse(response);
}

export async function recordCommandHistory(payload) {
  const response = await fetch('/api/server/snippets/history', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  return parseResponse(response);
}

export async function fetchCommandHistory(filters = {}) {
  const params = new URLSearchParams();
  Object.entries(filters).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') params.set(key, value);
  });
  const response = await fetch(`/api/server/snippets/history?${params.toString()}`);
  return parseResponse(response);
}
