const registry = new Map();

function persist(item) {
  try {
    const db = require('../db/database').getDatabase();
    db.prepare(
      `INSERT INTO settings_registry (
        domain,
        defaults_json,
        schema_json,
        mask_fields_json,
        updated_at
      ) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
      ON CONFLICT(domain) DO UPDATE SET
        defaults_json = excluded.defaults_json,
        schema_json = excluded.schema_json,
        mask_fields_json = excluded.mask_fields_json,
        updated_at = CURRENT_TIMESTAMP`
    ).run(
      item.domain,
      JSON.stringify(item.defaults || {}),
      item.schema ? JSON.stringify(item.schema) : null,
      JSON.stringify(item.maskFields || [])
    );
  } catch (error) {
    // Registry must remain usable during isolated unit tests or early boot.
  }
}

function register(domain, definition = {}) {
  if (!domain || typeof domain !== 'string') {
    throw new Error('settings domain is required');
  }

  const normalized = {
    domain,
    defaults: definition.defaults || {},
    schema: definition.schema || null,
    maskFields: definition.maskFields || [],
    normalize: definition.normalize || null,
    updatedAt: new Date().toISOString(),
  };
  registry.set(domain, normalized);
  persist(normalized);
  return normalized;
}

function get(domain) {
  return registry.get(domain) || null;
}

function list() {
  return Array.from(registry.values());
}

function normalize(domain, value = {}) {
  const item = get(domain);
  if (!item) return value;
  const merged = { ...item.defaults, ...value };
  return typeof item.normalize === 'function' ? item.normalize(merged) : merged;
}

module.exports = {
  register,
  get,
  list,
  normalize,
};
