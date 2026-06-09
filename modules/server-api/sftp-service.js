/**
 * SFTP 服务 - 远程文件管理
 *
 * 该服务复用 SSH/SFTP session，避免每个文件操作重复握手。对外保持旧方法名，
 * 让现有路由可以继续调用，同时提供更稳定的错误分类和连接生命周期管理。
 */

const { Client } = require('ssh2');
const path = require('path');
const { Readable } = require('stream');
const { serverStorage } = require('./storage');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('SFTPService');

const SESSION_IDLE_TIMEOUT_MS = 30 * 60 * 1000;
const SESSION_SWEEP_INTERVAL_MS = 60 * 1000;
const CONNECT_TIMEOUT_MS = 20 * 1000;
const DEFAULT_READ_LIMIT = 1024 * 1024;

class SFTPError extends Error {
  constructor(message, code = 'SFTP_ERROR', details = {}) {
    super(message);
    this.name = 'SFTPError';
    this.code = code;
    this.details = details;
  }
}

function normalizeRemotePath(remotePath = '.') {
  const text = String(remotePath || '.').trim() || '.';
  if (text === '.') return '.';
  return text.replace(/\\/g, '/').replace(/\/{2,}/g, '/');
}

function classifyError(error) {
  const message = error?.message || String(error || 'SFTP 操作失败');
  const lower = message.toLowerCase();

  if (error instanceof SFTPError) return error;
  if (lower.includes('authentication') || lower.includes('auth')) {
    return new SFTPError('SFTP 认证失败，请检查用户名、密码或密钥', 'AUTH_FAILED', { raw: message });
  }
  if (lower.includes('permission') || lower.includes('access denied')) {
    return new SFTPError('权限不足，无法完成该 SFTP 操作', 'PERMISSION_DENIED', { raw: message });
  }
  if (lower.includes('no such file') || lower.includes('not found')) {
    return new SFTPError('远程路径不存在', 'PATH_NOT_FOUND', { raw: message });
  }
  if (lower.includes('timeout')) {
    return new SFTPError('SFTP 连接或操作超时', 'TIMEOUT', { raw: message });
  }
  if (lower.includes('connection') || lower.includes('econn') || lower.includes('handshake')) {
    return new SFTPError('SFTP 连接失败', 'CONNECTION_FAILED', { raw: message });
  }
  return new SFTPError(message, 'SFTP_ERROR', { raw: message });
}

function promisifySftp(fn) {
  return new Promise((resolve, reject) => {
    fn((err, result) => {
      if (err) return reject(classifyError(err));
      resolve(result);
    });
  });
}

class SFTPSession {
  constructor(serverId, serverConfig) {
    this.serverId = serverId;
    this.serverConfig = serverConfig;
    this.conn = null;
    this.sftp = null;
    this.cwd = '.';
    this.status = 'connecting';
    this.activeOperations = 0;
    this.lastActiveAt = Date.now();
    this.connectingPromise = null;
  }

  async connect() {
    if (this.sftp && this.status === 'ready') return this;
    if (this.connectingPromise) return this.connectingPromise;

    this.status = 'connecting';
    this.connectingPromise = new Promise((resolve, reject) => {
      const conn = new Client();
      let settled = false;
      const timer = setTimeout(() => {
        if (settled) return;
        settled = true;
        conn.end();
        reject(new SFTPError('SFTP 连接超时', 'TIMEOUT'));
      }, CONNECT_TIMEOUT_MS);

      const fail = (error) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        this.status = 'closed';
        try {
          conn.end();
        } catch {
          // ignore
        }
        reject(classifyError(error));
      };

      conn.on('ready', () => {
        clearTimeout(timer);
        conn.sftp((err, sftp) => {
          if (err) return fail(err);
          settled = true;
          this.conn = conn;
          this.sftp = sftp;
          this.status = 'ready';
          this.touch();
          resolve(this);
        });
      });

      conn.on('error', fail);
      conn.on('close', () => {
        this.status = 'closed';
        this.sftp = null;
        this.conn = null;
      });

      conn.connect(this.buildConnectionSettings());
    }).finally(() => {
      this.connectingPromise = null;
    });

    return this.connectingPromise;
  }

  buildConnectionSettings() {
    const settings = {
      host: this.serverConfig.host,
      port: this.serverConfig.port || 22,
      username: this.serverConfig.username,
      readyTimeout: CONNECT_TIMEOUT_MS,
      keepaliveInterval: 15000,
      keepaliveCountMax: 3,
    };

    if (this.serverConfig.auth_type === 'key') {
      settings.privateKey = this.serverConfig.private_key;
      if (this.serverConfig.passphrase) settings.passphrase = this.serverConfig.passphrase;
    } else {
      settings.password = this.serverConfig.password;
    }

    return settings;
  }

  touch() {
    this.lastActiveAt = Date.now();
  }

  async withOperation(fn) {
    await this.connect();
    this.activeOperations += 1;
    this.touch();
    try {
      return await fn(this.sftp, this);
    } catch (error) {
      throw classifyError(error);
    } finally {
      this.activeOperations = Math.max(0, this.activeOperations - 1);
      this.touch();
    }
  }

  close(reason = 'idle') {
    this.status = 'closed';
    try {
      this.conn?.end();
    } catch {
      // ignore
    }
    this.conn = null;
    this.sftp = null;
    logger.info(`SFTP session closed: ${this.serverId} (${reason})`);
  }
}

class SFTPService {
  constructor() {
    this.sessions = new Map();
    this.sweepTimer = setInterval(() => this.cleanupIdleSessions(), SESSION_SWEEP_INTERVAL_MS);
    if (typeof this.sweepTimer.unref === 'function') this.sweepTimer.unref();
  }

  getSessionKey(serverId) {
    return String(serverId);
  }

  getServerConfig(serverId) {
    const serverConfig = serverStorage.getById(serverId);
    if (!serverConfig) {
      throw new SFTPError('服务器配置不存在', 'SERVER_NOT_FOUND');
    }
    if (!serverConfig.host || !serverConfig.username) {
      throw new SFTPError('SSH 主机或用户名不完整，无法使用 SFTP', 'CONFIG_INCOMPLETE');
    }
    return serverConfig;
  }

  async getSession(serverId) {
    const key = this.getSessionKey(serverId);
    let session = this.sessions.get(key);
    if (!session || session.status === 'closed') {
      session = new SFTPSession(serverId, this.getServerConfig(serverId));
      this.sessions.set(key, session);
    }
    await session.connect();
    return session;
  }

  async getConnection(serverId) {
    const session = await this.getSession(serverId);
    return { sftp: session.sftp, conn: session.conn, session };
  }

  cleanupIdleSessions() {
    const now = Date.now();
    for (const [key, session] of this.sessions.entries()) {
      if (session.activeOperations > 0) continue;
      if (now - session.lastActiveAt > SESSION_IDLE_TIMEOUT_MS) {
        session.close('idle-timeout');
        this.sessions.delete(key);
      }
    }
  }

  closeSession(serverId) {
    const key = this.getSessionKey(serverId);
    const session = this.sessions.get(key);
    if (session) {
      session.close('manual');
      this.sessions.delete(key);
    }
  }

  async listDirectory(serverId, remotePath = '.') {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => {
      const requestedPath = normalizeRemotePath(remotePath);
      const absPath = await promisifySftp(cb => sftp.realpath(requestedPath, cb));
      const cwd = normalizeRemotePath(absPath);
      const list = await promisifySftp(cb => sftp.readdir(cwd, cb));
      session.cwd = cwd;

      const files = list.map(item => ({
        name: item.filename,
        path: this._joinRemotePath(cwd, item.filename),
        isDirectory: item.attrs.isDirectory(),
        isFile: item.attrs.isFile(),
        isSymlink: item.attrs.isSymbolicLink(),
        size: item.attrs.size,
        mode: item.attrs.mode,
        mtime: item.attrs.mtime * 1000,
        atime: item.attrs.atime * 1000,
        uid: item.attrs.uid,
        gid: item.attrs.gid,
        permissions: this._formatPermissions(item.attrs.mode),
      }));

      files.sort((a, b) => {
        if (a.isDirectory && !b.isDirectory) return -1;
        if (!a.isDirectory && b.isDirectory) return 1;
        return a.name.localeCompare(b.name, 'zh-CN', { numeric: true });
      });

      return { files, cwd };
    });
  }

  async stat(serverId, remotePath) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => {
      const stats = await promisifySftp(cb => sftp.stat(normalizeRemotePath(remotePath), cb));
      return {
        isDirectory: stats.isDirectory(),
        isFile: stats.isFile(),
        isSymlink: stats.isSymbolicLink(),
        size: stats.size,
        mode: stats.mode,
        mtime: stats.mtime * 1000,
        atime: stats.atime * 1000,
        uid: stats.uid,
        gid: stats.gid,
        permissions: this._formatPermissions(stats.mode),
      };
    });
  }

  async readFile(serverId, remotePath, maxSize = DEFAULT_READ_LIMIT) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => {
      const normalizedPath = normalizeRemotePath(remotePath);
      const stats = await promisifySftp(cb => sftp.stat(normalizedPath, cb));
      const limit = Number(maxSize || DEFAULT_READ_LIMIT);
      if (stats.size > limit) {
        throw new SFTPError(
          `文件过大 (${this._formatSize(stats.size)})，最大支持 ${this._formatSize(limit)}`,
          'FILE_TOO_LARGE',
          { size: stats.size, limit }
        );
      }

      return new Promise((resolve, reject) => {
        const chunks = [];
        const stream = sftp.createReadStream(normalizedPath);
        stream.on('data', chunk => chunks.push(chunk));
        stream.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
        stream.on('error', err => reject(classifyError(err)));
      });
    });
  }

  async writeFile(serverId, remotePath, content) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => this._writeStream(sftp, normalizeRemotePath(remotePath), content));
  }

  async mkdir(serverId, remotePath) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => {
      await promisifySftp(cb => sftp.mkdir(normalizeRemotePath(remotePath), cb));
      return { success: true };
    });
  }

  async mkdirRecursive(serverId, remotePath) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => {
      await this._mkdirRecursiveInternal(sftp, remotePath);
      return { success: true };
    });
  }

  async deleteFile(serverId, remotePath) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => {
      await promisifySftp(cb => sftp.unlink(normalizeRemotePath(remotePath), cb));
      return { success: true };
    });
  }

  async rmdir(serverId, remotePath) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => {
      await promisifySftp(cb => sftp.rmdir(normalizeRemotePath(remotePath), cb));
      return { success: true };
    });
  }

  async rmdirRecursive(serverId, remotePath) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => {
      await this._rmdirRecursiveInternal(sftp, normalizeRemotePath(remotePath));
      return { success: true };
    });
  }

  async rename(serverId, oldPath, newPath) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => {
      await promisifySftp(cb => sftp.rename(normalizeRemotePath(oldPath), normalizeRemotePath(newPath), cb));
      return { success: true };
    });
  }

  async chmod(serverId, remotePath, mode) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => {
      await promisifySftp(cb => sftp.chmod(normalizeRemotePath(remotePath), mode, cb));
      return { success: true };
    });
  }

  async downloadStream(serverId, remotePath) {
    const session = await this.getSession(serverId);
    await session.connect();
    session.activeOperations += 1;
    session.touch();

    const normalizedPath = normalizeRemotePath(remotePath);
    const stats = await promisifySftp(cb => session.sftp.stat(normalizedPath, cb));
    const stream = session.sftp.createReadStream(normalizedPath);
    const release = () => {
      session.activeOperations = Math.max(0, session.activeOperations - 1);
      session.touch();
    };

    stream.on('close', release);
    stream.on('error', release);

    return {
      stream,
      size: stats.size,
      filename: this._extractFilename(normalizedPath),
      conn: {
        end: release,
      },
    };
  }

  async uploadFile(serverId, remotePath, data) {
    const session = await this.getSession(serverId);
    return session.withOperation(async (sftp) => this._writeStream(sftp, normalizeRemotePath(remotePath), data));
  }

  async _writeStream(sftp, remotePath, data) {
    return new Promise((resolve, reject) => {
      const writeStream = sftp.createWriteStream(remotePath);
      writeStream.on('close', () => resolve({ success: true }));
      writeStream.on('error', err => reject(classifyError(err)));

      if (Buffer.isBuffer(data)) {
        writeStream.end(data);
      } else if (data instanceof Readable) {
        data.pipe(writeStream);
      } else {
        writeStream.end(String(data || ''), 'utf8');
      }
    });
  }

  async _mkdirRecursiveInternal(sftp, remotePath) {
    const normalizedPath = normalizeRemotePath(remotePath);
    const parts = normalizedPath.split('/').filter(Boolean);
    if (parts.length === 0) return;

    let currentPath = normalizedPath.startsWith('/') ? '' : '';
    const isWinDrive = /^[A-Za-z]:$/.test(parts[0]);
    if (isWinDrive) currentPath = parts.shift();

    for (const part of parts) {
      currentPath = currentPath ? `${currentPath}/${part}` : `/${part}`;
      const exists = await new Promise(resolve => {
        sftp.stat(currentPath, (err, stats) => resolve(!err && stats.isDirectory()));
      });
      if (!exists) {
        await promisifySftp(cb => sftp.mkdir(currentPath, cb));
      }
    }
  }

  async _rmdirRecursiveInternal(sftp, remotePath) {
    const list = await promisifySftp(cb => sftp.readdir(remotePath, cb));
    for (const item of list) {
      const itemPath = this._joinRemotePath(remotePath, item.filename);
      if (item.attrs.isDirectory()) {
        await this._rmdirRecursiveInternal(sftp, itemPath);
      } else {
        await promisifySftp(cb => sftp.unlink(itemPath, cb));
      }
    }
    await promisifySftp(cb => sftp.rmdir(remotePath, cb));
  }

  _joinRemotePath(basePath, fileName) {
    const normalized = normalizeRemotePath(basePath);
    if (normalized === '/') return `/${fileName}`;
    return `${normalized.replace(/\/$/, '')}/${fileName}`;
  }

  _extractFilename(remotePath) {
    const parts = normalizeRemotePath(remotePath).split('/');
    return parts[parts.length - 1] || 'download';
  }

  _formatPermissions(mode) {
    const perms = ['---', '--x', '-w-', '-wx', 'r--', 'r-x', 'rw-', 'rwx'];
    const owner = perms[(mode >> 6) & 7];
    const group = perms[(mode >> 3) & 7];
    const other = perms[mode & 7];

    let type = '-';
    if ((mode & 0o170000) === 0o040000) type = 'd';
    if ((mode & 0o170000) === 0o120000) type = 'l';

    return type + owner + group + other;
  }

  _formatSize(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
  }
}

module.exports = new SFTPService();
module.exports.SFTPError = SFTPError;
module.exports.classifyError = classifyError;
module.exports.normalizeRemotePath = normalizeRemotePath;
