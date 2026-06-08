const { OperationLog } = require('../db/models/System');
const { createLogger } = require('../utils/logger');
const { maskSensitiveFields } = require('../utils/secure-storage');

const logger = createLogger('AuditService');

const DEFAULT_SENSITIVE_FIELDS = [
  'password',
  'token',
  'secret',
  'apiKey',
  'api_key',
  'cookie',
  'authorization',
];

function normalizeActor(actor) {
  if (!actor) return 'system';
  if (typeof actor === 'string') return actor;
  return actor.username || actor.id || actor.name || 'system';
}

function normalizeRequest(req) {
  if (!req) return {};
  return {
    ip_address:
      req.ip ||
      req.headers?.['x-forwarded-for']?.split(',')?.[0]?.trim() ||
      req.socket?.remoteAddress ||
      null,
    user_agent: req.get ? req.get('user-agent') : req.headers?.['user-agent'] || null,
  };
}

function record(event = {}) {
  const safeMetadata = maskSensitiveFields(
    event.metadata || event.details || {},
    event.sensitiveFields || DEFAULT_SENSITIVE_FIELDS
  );
  const requestMeta = normalizeRequest(event.req);

  const details = {
    actor: normalizeActor(event.actor),
    module: event.module || event.table_name || 'system',
    action: event.action || event.operation_type || 'unknown',
    resourceType: event.resourceType || event.table_name || event.module || null,
    resourceId: event.resourceId || event.record_id || null,
    summary: event.summary || null,
    metadata: safeMetadata,
  };

  try {
    return OperationLog.logOperation({
      operation_type: details.action,
      table_name: details.module,
      record_id: details.resourceId,
      details,
      ip_address: event.ip_address || requestMeta.ip_address,
      user_agent: event.user_agent || requestMeta.user_agent,
    });
  } catch (error) {
    logger.warn('审计记录失败:', error.message);
    return null;
  }
}

module.exports = {
  record,
  normalizeActor,
};
