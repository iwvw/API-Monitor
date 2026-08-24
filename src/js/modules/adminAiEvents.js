export function parseAdminAiEvent(eventType, data) {
  const parsed = JSON.parse(data);
  return { type: eventType, ...parsed };
}