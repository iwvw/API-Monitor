/**
 * SFTP 服务 - 提供远程文件管理功能
 * 包括文件浏览、上传、下载、编辑、删除等操作
 */

const { Client } = require('ssh2');
const { serverStorage } = require('./storage');
const { createLogger } = require('../../src/utils/logger');
const path = require('path');
const { Readable } = require('stream');

const logger = createLogger('SFTPService');

class SFTPService {
    constructor() {
        // 连接缓存池（可选，用于会话复用）
        this.connectionPool = new Map();
    }

    /**
     * 获取 SFTP 连接
     * @param {string} serverId - 服务器 ID
     * @returns {Promise<{ sftp: Object, conn: Client }>}
     */
    async getConnection(serverId) {
        const serverConfig = serverStorage.getById(serverId);
        if (!serverConfig) {
            throw new Error('服务器配置不存在');
        }

        return new Promise((resolve, reject) => {
            const conn = new Client();
            const timeout = setTimeout(() => {
                conn.end();
                reject(new Error('SFTP 连接超时'));
            }, 20000);

            conn.on('ready', () => {
                clearTimeout(timeout);
                conn.sftp((err, sftp) => {
                    if (err) {
                        conn.end();
                        return reject(err);
                    }
                    resolve({ sftp, conn });
                });
            });

            conn.on('error', err => {
                clearTimeout(timeout);
                reject(err);
            });

            const connSettings = {
                host: serverConfig.host,
                port: serverConfig.port || 22,
                username: serverConfig.username,
                readyTimeout: 20000,
            };

            if (serverConfig.auth_type === 'key') {
                connSettings.privateKey = serverConfig.private_key;
                if (serverConfig.passphrase) connSettings.passphrase = serverConfig.passphrase;
            } else {
                connSettings.password = serverConfig.password;
            }

            conn.connect(connSettings);
        });
    }

    /**
     * 列出目录内容
     * @param {string} serverId - 服务器 ID
     * @param {string} remotePath - 远程路径
     * @returns {Promise<Array>}
     */
    async listDirectory(serverId, remotePath = '.') { // Default to current directory
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            return await new Promise((resolve, reject) => {
                // First resolve the real path (handles '.' -> '/home/user' or 'C:/Users/User')
                sftp.realpath(remotePath, (err, absPath) => {
                    if (err) return reject(err);

                    sftp.readdir(absPath, (err, list) => {
                        if (err) return reject(err);

                        // Format file list
                        const files = list.map(item => ({
                            name: item.filename,
                            path: path.posix.join(absPath, item.filename),
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

                        // Sort
                        files.sort((a, b) => {
                            if (a.isDirectory && !b.isDirectory) return -1;
                            if (!a.isDirectory && b.isDirectory) return 1;
                            return a.name.localeCompare(b.name);
                        });

                        resolve({ files, cwd: absPath });
                    });
                });
            });
        } finally {
            conn.end();
        }
    }

    /**
     * 获取文件状态信息
     * @param {string} serverId 
     * @param {string} remotePath 
     */
    async stat(serverId, remotePath) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            return await new Promise((resolve, reject) => {
                sftp.stat(remotePath, (err, stats) => {
                    if (err) return reject(err);
                    resolve({
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
                    });
                });
            });
        } finally {
            conn.end();
        }
    }

    /**
     * 读取文件内容
     * @param {string} serverId
     * @param {string} remotePath
     * @param {number} maxSize - 最大读取大小（字节），默认 1MB
     */
    async readFile(serverId, remotePath, maxSize = 1024 * 1024) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            // 先检查文件大小
            const stats = await new Promise((resolve, reject) => {
                sftp.stat(remotePath, (err, stats) => {
                    if (err) return reject(err);
                    resolve(stats);
                });
            });

            if (stats.size > maxSize) {
                throw new Error(`文件过大 (${this._formatSize(stats.size)})，最大支持 ${this._formatSize(maxSize)}`);
            }

            // 读取文件
            return await new Promise((resolve, reject) => {
                const chunks = [];
                const stream = sftp.createReadStream(remotePath);

                stream.on('data', chunk => chunks.push(chunk));
                stream.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
                stream.on('error', reject);
            });
        } finally {
            conn.end();
        }
    }

    /**
     * 写入文件内容
     * @param {string} serverId
     * @param {string} remotePath
     * @param {string|Buffer} content
     */
    async writeFile(serverId, remotePath, content) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            return await new Promise((resolve, reject) => {
                const stream = sftp.createWriteStream(remotePath);

                stream.on('close', () => resolve({ success: true }));
                stream.on('error', reject);

                // 写入内容
                if (Buffer.isBuffer(content)) {
                    stream.end(content);
                } else {
                    stream.end(content, 'utf8');
                }
            });
        } finally {
            conn.end();
        }
    }

    /**
     * 创建目录
     * @param {string} serverId
     * @param {string} remotePath
     */
    async mkdir(serverId, remotePath) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            return await new Promise((resolve, reject) => {
                sftp.mkdir(remotePath, err => {
                    if (err) return reject(err);
                    resolve({ success: true });
                });
            });
        } finally {
            conn.end();
        }
    }

    /**
     * 递归创建目录（如果父目录不存在则自动创建）
     * @param {string} serverId
     * @param {string} remotePath
     */
    async mkdirRecursive(serverId, remotePath) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            await this._mkdirRecursiveInternal(sftp, remotePath);
            return { success: true };
        } finally {
            conn.end();
        }
    }

    /**
     * 内部递归创建目录方法
     */
    async _mkdirRecursiveInternal(sftp, remotePath) {
        const parts = remotePath.split('/').filter(Boolean);
        let currentPath = '';

        for (const part of parts) {
            currentPath += '/' + part;

            // 检查目录是否存在
            const exists = await new Promise(resolve => {
                sftp.stat(currentPath, (err, stats) => {
                    if (err) return resolve(false);
                    resolve(stats.isDirectory());
                });
            });

            if (!exists) {
                await new Promise((resolve, reject) => {
                    sftp.mkdir(currentPath, err => {
                        if (err && err.code !== 4) return reject(err); // code 4 = 已存在
                        resolve();
                    });
                });
            }
        }
    }

    /**
     * 删除文件
     * @param {string} serverId
     * @param {string} remotePath
     */
    async deleteFile(serverId, remotePath) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            return await new Promise((resolve, reject) => {
                sftp.unlink(remotePath, err => {
                    if (err) return reject(err);
                    resolve({ success: true });
                });
            });
        } finally {
            conn.end();
        }
    }

    /**
     * 删除目录（必须为空）
     * @param {string} serverId
     * @param {string} remotePath
     */
    async rmdir(serverId, remotePath) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            return await new Promise((resolve, reject) => {
                sftp.rmdir(remotePath, err => {
                    if (err) return reject(err);
                    resolve({ success: true });
                });
            });
        } finally {
            conn.end();
        }
    }

    /**
     * 递归删除目录（包括非空目录）
     * @param {string} serverId
     * @param {string} remotePath
     */
    async rmdirRecursive(serverId, remotePath) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            await this._rmdirRecursiveInternal(sftp, remotePath);
            return { success: true };
        } finally {
            conn.end();
        }
    }

    /**
     * 内部递归删除方法（使用已有的 sftp 连接）
     */
    async _rmdirRecursiveInternal(sftp, remotePath) {
        // 列出目录内容
        const list = await new Promise((resolve, reject) => {
            sftp.readdir(remotePath, (err, list) => {
                if (err) return reject(err);
                resolve(list);
            });
        });

        // 递归删除每个子项
        for (const item of list) {
            const itemPath = path.posix.join(remotePath, item.filename);

            if (item.attrs.isDirectory()) {
                // 递归删除子目录
                await this._rmdirRecursiveInternal(sftp, itemPath);
            } else {
                // 删除文件
                await new Promise((resolve, reject) => {
                    sftp.unlink(itemPath, err => {
                        if (err) return reject(err);
                        resolve();
                    });
                });
            }
        }

        // 最后删除空目录
        await new Promise((resolve, reject) => {
            sftp.rmdir(remotePath, err => {
                if (err) return reject(err);
                resolve();
            });
        });
    }

    /**
     * 重命名/移动文件或目录
     * @param {string} serverId
     * @param {string} oldPath
     * @param {string} newPath
     */
    async rename(serverId, oldPath, newPath) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            return await new Promise((resolve, reject) => {
                sftp.rename(oldPath, newPath, err => {
                    if (err) return reject(err);
                    resolve({ success: true });
                });
            });
        } finally {
            conn.end();
        }
    }

    /**
     * 修改文件权限
     * @param {string} serverId
     * @param {string} remotePath
     * @param {number} mode - 八进制权限值，如 0o755
     */
    async chmod(serverId, remotePath, mode) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            return await new Promise((resolve, reject) => {
                sftp.chmod(remotePath, mode, err => {
                    if (err) return reject(err);
                    resolve({ success: true });
                });
            });
        } finally {
            conn.end();
        }
    }

    /**
     * 获取文件下载流
     * @param {string} serverId
     * @param {string} remotePath
     * @returns {Promise<{ stream: Readable, size: number, filename: string }>}
     */
    async downloadStream(serverId, remotePath) {
        const { sftp, conn } = await this.getConnection(serverId);

        // 获取文件信息
        const stats = await new Promise((resolve, reject) => {
            sftp.stat(remotePath, (err, stats) => {
                if (err) return reject(err);
                resolve(stats);
            });
        });

        const stream = sftp.createReadStream(remotePath);

        // 保存连接引用以便后续关闭
        stream.on('close', () => conn.end());
        stream.on('error', () => conn.end());

        return {
            stream,
            size: stats.size,
            filename: path.posix.basename(remotePath),
            conn, // 返回连接以便手动管理
        };
    }

    /**
     * 上传文件
     * @param {string} serverId
     * @param {string} remotePath
     * @param {Buffer|Readable} data
     */
    async uploadFile(serverId, remotePath, data) {
        const { sftp, conn } = await this.getConnection(serverId);

        try {
            return await new Promise((resolve, reject) => {
                const writeStream = sftp.createWriteStream(remotePath);

                writeStream.on('close', () => resolve({ success: true }));
                writeStream.on('error', reject);

                if (Buffer.isBuffer(data)) {
                    writeStream.end(data);
                } else if (data instanceof Readable) {
                    data.pipe(writeStream);
                } else {
                    writeStream.end(data);
                }
            });
        } finally {
            conn.end();
        }
    }

    // ==================== 工具方法 ====================

    /**
     * 格式化权限位为 rwx 形式
     */
    _formatPermissions(mode) {
        const perms = ['---', '--x', '-w-', '-wx', 'r--', 'r-x', 'rw-', 'rwx'];
        const owner = perms[(mode >> 6) & 7];
        const group = perms[(mode >> 3) & 7];
        const other = perms[mode & 7];

        let type = '-';
        if ((mode & 0o170000) === 0o040000) type = 'd'; // 目录
        if ((mode & 0o170000) === 0o120000) type = 'l'; // 符号链接

        return type + owner + group + other;
    }

    /**
     * 格式化文件大小
     */
    _formatSize(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }
}

module.exports = new SFTPService();
