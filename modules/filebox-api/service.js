const fs = require('fs');
const path = require('path');
const bcrypt = require('bcryptjs');
const dbService = require('../../src/db/database');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('FileBox');
const DEFAULT_FILEBOX_SETTINGS = {
  max_file_size: 100 * 1024 * 1024,
  allowed_mime_types: [],
  default_expiry_hours: 24,
  public_upload_enabled: false,
};

class FileBoxService {
  constructor() {
    const rootDataDir = process.env.DATA_DIR
      ? path.resolve(process.env.DATA_DIR)
      : path.resolve(__dirname, '../../data');
    this.dataDir = path.join(rootDataDir, 'filebox');
    this.uploadsDir = path.join(this.dataDir, 'uploads');
    this.metadataFile = path.join(this.dataDir, 'metadata.json');

    this.ensureDirs();
    dbService.initialize();
    this.ensureSettings();
    this.migrateJsonMetadata();
  }

  get db() {
    return dbService.getDatabase();
  }

  ensureDirs() {
    if (!fs.existsSync(this.dataDir)) {
      fs.mkdirSync(this.dataDir, { recursive: true });
    }
    if (!fs.existsSync(this.uploadsDir)) {
      fs.mkdirSync(this.uploadsDir, { recursive: true });
    }
  }

  ensureSettings() {
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS filebox_settings (
        id INTEGER PRIMARY KEY CHECK (id = 1),
        max_file_size INTEGER NOT NULL DEFAULT 104857600,
        allowed_mime_types TEXT NOT NULL DEFAULT '[]',
        default_expiry_hours INTEGER NOT NULL DEFAULT 24,
        public_upload_enabled INTEGER NOT NULL DEFAULT 0,
        updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
      )
    `);

    this.db
      .prepare(
        `INSERT OR IGNORE INTO filebox_settings (
          id,
          max_file_size,
          allowed_mime_types,
          default_expiry_hours,
          public_upload_enabled
        ) VALUES (1, ?, ?, ?, ?)`
      )
      .run(
        DEFAULT_FILEBOX_SETTINGS.max_file_size,
        JSON.stringify(DEFAULT_FILEBOX_SETTINGS.allowed_mime_types),
        DEFAULT_FILEBOX_SETTINGS.default_expiry_hours,
        DEFAULT_FILEBOX_SETTINGS.public_upload_enabled ? 1 : 0
      );
  }

  getSettings() {
    this.ensureSettings();
    const row = this.db.prepare('SELECT * FROM filebox_settings WHERE id = 1').get();
    return {
      max_file_size: Number(row?.max_file_size || DEFAULT_FILEBOX_SETTINGS.max_file_size),
      allowed_mime_types: this._parseMetadata(row?.allowed_mime_types) || [],
      default_expiry_hours: Number(row?.default_expiry_hours || DEFAULT_FILEBOX_SETTINGS.default_expiry_hours),
      public_upload_enabled: row?.public_upload_enabled === 1,
      updated_at: row?.updated_at || null,
    };
  }

  updateSettings(input = {}) {
    const current = this.getSettings();
    const next = {
      max_file_size: Math.max(1, parseInt(input.max_file_size, 10) || current.max_file_size),
      allowed_mime_types: Array.isArray(input.allowed_mime_types)
        ? input.allowed_mime_types.map(item => String(item).trim()).filter(Boolean)
        : current.allowed_mime_types,
      default_expiry_hours: Math.max(1, parseInt(input.default_expiry_hours, 10) || current.default_expiry_hours),
      public_upload_enabled: input.public_upload_enabled === undefined
        ? current.public_upload_enabled
        : input.public_upload_enabled === true,
    };

    this.db
      .prepare(
        `UPDATE filebox_settings
         SET max_file_size = ?,
             allowed_mime_types = ?,
             default_expiry_hours = ?,
             public_upload_enabled = ?,
             updated_at = CURRENT_TIMESTAMP
         WHERE id = 1`
      )
      .run(
        next.max_file_size,
        JSON.stringify(next.allowed_mime_types),
        next.default_expiry_hours,
        next.public_upload_enabled ? 1 : 0
      );

    return this.getSettings();
  }

  migrateJsonMetadata() {
    if (!fs.existsSync(this.metadataFile)) return;

    try {
      const raw = JSON.parse(fs.readFileSync(this.metadataFile, 'utf8'));
      const entries = Object.values(raw || {});
      if (entries.length === 0) return;

      const stmt = this.db.prepare(`
        INSERT OR IGNORE INTO filebox_entries (
          code,
          type,
          content,
          original_name,
          filename,
          path,
          mimetype,
          size,
          created_at,
          expiry,
          burn_after_reading,
          downloads,
          metadata_json
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      `);

      const tx = this.db.transaction(() => {
        for (const entry of entries) {
          const code = String(entry.code || '').toUpperCase();
          if (!code) continue;
          stmt.run(
            code,
            entry.type || 'file',
            entry.type === 'text' ? entry.content || '' : null,
            entry.originalName || null,
            entry.filename || `file_${code}`,
            entry.path || null,
            entry.mimetype || null,
            Number(entry.size || 0),
            Number(entry.createdAt || Date.now()),
            Number(entry.expiry || Date.now()),
            entry.burnAfterReading ? 1 : 0,
            Number(entry.downloads || 0),
            JSON.stringify(entry)
          );
        }
      });
      tx();
      logger.success(`已兼容迁移 Filebox JSON metadata: ${entries.length} 条`);
    } catch (error) {
      logger.error('Filebox JSON metadata 迁移失败:', error.message);
    }
  }

  generateCode(length = 5) {
    const chars = '23456789ABCDEFGHJKLMNPQRSTUVWXYZ';
    let code = '';
    do {
      code = '';
      for (let i = 0; i < length; i++) {
        code += chars.charAt(Math.floor(Math.random() * chars.length));
      }
    } while (this.exists(code));
    return code;
  }

  exists(code) {
    return !!this.db
      .prepare('SELECT code FROM filebox_entries WHERE code = ? AND deleted_at IS NULL')
      .get(String(code || '').toUpperCase());
  }

  _hashAccessPassword(accessPassword) {
    if (!accessPassword) return null;
    return bcrypt.hashSync(String(accessPassword), 10);
  }

  verifyAccessPassword(entry, accessPassword) {
    if (!entry?.accessPasswordHash) return true;
    if (!accessPassword) return false;
    return bcrypt.compareSync(String(accessPassword), entry.accessPasswordHash);
  }

  addText(content, expiryHours = 24, burnAfterReading = false, maxDownloads = 0, accessPassword = '') {
    const code = this.generateCode();
    const now = Date.now();
    const expiry = now + expiryHours * 60 * 60 * 1000;
    const passwordHash = this._hashAccessPassword(accessPassword);

    this.db
      .prepare(
        `INSERT INTO filebox_entries (
          code,
          type,
          content,
          filename,
          created_at,
          expiry,
          burn_after_reading,
          max_downloads,
          access_password_hash
        ) VALUES (?, 'text', ?, ?, ?, ?, ?, ?, ?)`
      )
      .run(code, content, `text_${code}.txt`, now, expiry, burnAfterReading ? 1 : 0, maxDownloads, passwordHash);

    return this.getEntry(code, { includeExpired: true });
  }

  async addFile(fileObj, expiryHours = 24, burnAfterReading = false, maxDownloads = 0, accessPassword = '') {
    const settings = this.getSettings();
    if (Number(fileObj.size || 0) > settings.max_file_size) {
      throw new Error(`文件过大，最大支持 ${Math.round(settings.max_file_size / 1024 / 1024)}MB`);
    }
    if (!this._isMimeAllowed(fileObj.mimetype, settings.allowed_mime_types)) {
      throw new Error(`文件类型不允许: ${fileObj.mimetype || 'unknown'}`);
    }

    const code = this.generateCode();
    const now = Date.now();
    const expiry = now + expiryHours * 60 * 60 * 1000;
    const uniqueSuffix = `${Date.now()}-${Math.round(Math.random() * 1e9)}`;
    const safeName = this._sanitizeFilename(fileObj.name || 'upload.bin');
    const saveFilename = `${uniqueSuffix}-${safeName}`;
    const savePath = path.join(this.uploadsDir, saveFilename);
    const passwordHash = this._hashAccessPassword(accessPassword);

    await fileObj.mv(savePath);

    this.db
      .prepare(
        `INSERT INTO filebox_entries (
          code,
          type,
          original_name,
          filename,
          path,
          mimetype,
          size,
          created_at,
          expiry,
          burn_after_reading,
          max_downloads,
          access_password_hash
        ) VALUES (?, 'file', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
      )
      .run(
        code,
        fileObj.name,
        saveFilename,
        savePath,
        fileObj.mimetype,
        Number(fileObj.size || 0),
        now,
        expiry,
        burnAfterReading ? 1 : 0,
        maxDownloads,
        passwordHash
      );

    return this.getEntry(code, { includeExpired: true });
  }

  getEntry(code, options = {}) {
    if (!code) return null;
    const row = this.db
      .prepare('SELECT * FROM filebox_entries WHERE code = ? AND deleted_at IS NULL')
      .get(String(code).toUpperCase());
    if (!row) return null;

    const entry = this._fromRow(row);
    if (!options.includeExpired && Date.now() > entry.expiry) {
      this.deleteEntry(entry.code);
      return null;
    }
    if (!options.includeExpired && entry.maxDownloads > 0 && entry.downloads >= entry.maxDownloads) {
      this.deleteEntry(entry.code);
      return null;
    }

    return entry;
  }

  accessEntry(code, requestMeta = {}) {
    if (!code) return null;
    const entry = this.getEntry(code);
    if (!entry) return null;

    const nextDownloads = (entry.downloads || 0) + 1;
    if (entry.burnAfterReading || (entry.maxDownloads > 0 && nextDownloads >= entry.maxDownloads)) {
      this.deleteEntry(entry.code);
    } else {
      this.db
        .prepare('UPDATE filebox_entries SET downloads = downloads + 1 WHERE code = ?')
        .run(entry.code);
    }

    this.logAccess(entry.code, 'download', requestMeta);
    return entry;
  }

  deleteEntry(code) {
    const entry = this.getEntry(code, { includeExpired: true });
    if (!entry) return false;

    if (entry.type === 'file' && entry.path) {
      try {
        const resolvedPath = path.resolve(entry.path);
        const relativePath = path.relative(this.uploadsDir, resolvedPath);
        const isInsideUploads = relativePath && !relativePath.startsWith('..') && !path.isAbsolute(relativePath);
        if (isInsideUploads && fs.existsSync(resolvedPath)) {
          fs.unlinkSync(resolvedPath);
        }
      } catch (error) {
        logger.error(`Failed to delete file: ${entry.path}`, error.message);
      }
    }

    this.db.prepare('DELETE FROM filebox_entries WHERE code = ?').run(entry.code);
    this.logAccess(entry.code, 'delete');
    return true;
  }

  getAll() {
    this.cleanupExpired();
    return this.db
      .prepare(
        `SELECT * FROM filebox_entries
         WHERE deleted_at IS NULL
         ORDER BY created_at DESC`
      )
      .all()
      .map(row => this._toPublicEntry(this._fromRow(row)));
  }

  cleanupExpired() {
    const expired = this.db
      .prepare('SELECT code FROM filebox_entries WHERE expiry < ? AND deleted_at IS NULL')
      .all(Date.now());
    for (const { code } of expired) {
      this.deleteEntry(code);
    }
    return expired.length;
  }

  getAccessLogs(code, limit = 100) {
    const cappedLimit = Math.min(Math.max(parseInt(limit, 10) || 100, 1), 500);
    const params = [];
    let where = '';
    if (code) {
      where = 'WHERE code = ?';
      params.push(String(code).toUpperCase());
    }
    params.push(cappedLimit);
    return this.db
      .prepare(
        `SELECT id, code, action, ip_address as ipAddress, user_agent as userAgent, created_at as createdAt
         FROM filebox_access_logs
         ${where}
         ORDER BY created_at DESC
         LIMIT ?`
      )
      .all(...params);
  }

  logAccess(code, action, requestMeta = {}) {
    try {
      this.db
        .prepare(
          `INSERT INTO filebox_access_logs (code, action, ip_address, user_agent)
           VALUES (?, ?, ?, ?)`
        )
        .run(code, action, requestMeta.ip || null, requestMeta.userAgent || null);
    } catch (error) {
      logger.warn('Filebox access log failed:', error.message);
    }
  }

  _sanitizeFilename(name) {
    return path
      .basename(String(name))
      .replace(/[<>:"/\\|?*\x00-\x1F]/g, '_')
      .slice(0, 180);
  }

  _fromRow(row) {
    return {
      code: row.code,
      type: row.type,
      content: row.content,
      originalName: row.original_name,
      filename: row.filename,
      path: row.path,
      mimetype: row.mimetype,
      size: row.size,
      createdAt: row.created_at,
      expiry: row.expiry,
      burnAfterReading: !!row.burn_after_reading,
      downloads: row.downloads || 0,
      maxDownloads: row.max_downloads || 0,
      accessPasswordHash: row.access_password_hash || null,
      requiresPassword: !!row.access_password_hash,
      metadata: this._parseMetadata(row.metadata_json),
    };
  }

  _parseMetadata(value) {
    if (!value) return null;
    try {
      return JSON.parse(value);
    } catch (error) {
      return null;
    }
  }

  _isMimeAllowed(mimeType, allowedMimeTypes = []) {
    if (!Array.isArray(allowedMimeTypes) || allowedMimeTypes.length === 0) return true;
    const mime = String(mimeType || '').toLowerCase();
    return allowedMimeTypes.some(pattern => {
      const rule = String(pattern || '').trim().toLowerCase();
      if (!rule) return false;
      if (rule.endsWith('/*')) return mime.startsWith(rule.slice(0, -1));
      return mime === rule;
    });
  }

  _toPublicEntry(entry) {
    if (!entry) return entry;
    const {
      path: _filePath,
      content,
      metadata: _metadata,
      accessPasswordHash: _accessPasswordHash,
      ...rest
    } = entry;
    if (entry.type === 'text') {
      return {
        ...rest,
        preview: typeof content === 'string' ? content.slice(0, 80) : '',
      };
    }
    return rest;
  }
}

module.exports = new FileBoxService();
