/**
 * SSH 连接服务
 * 管理 SSH 连接池和命令执行
 */

const { Client } = require('ssh2');
const { ServerMonitorConfig } = require('../../src/db/models');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('SSH');

class SSHService {
    constructor() {
        // SSH 连接池：{ serverId: { client, lastUsed, serverId } }
        this.connections = new Map();
        // 正在连接中的 Promise：{ serverId: Promise }
        this.connectionPromises = new Map();

        // 定时清理过期连接
        this.startCleanupTimer();
    }

    /**
     * 创建 SSH 连接
     */
    async connect(serverConfig) {
        return new Promise((resolve, reject) => {
            const client = new Client();
            const config = ServerMonitorConfig.get();
            const timeout = (config?.probe_timeout || 10) * 1000;

            const sshConfig = {
                host: serverConfig.host,
                port: serverConfig.port || 22,
                username: serverConfig.username,
                readyTimeout: timeout
            };

            if (serverConfig.auth_type === 'password') {
                sshConfig.password = serverConfig.password;
            } else if (serverConfig.auth_type === 'key') {
                sshConfig.privateKey = serverConfig.private_key;
                if (serverConfig.passphrase) {
                    sshConfig.passphrase = serverConfig.passphrase;
                }
            }

            client.on('ready', () => resolve(client));
            client.on('error', (err) => reject(err));
            client.connect(sshConfig);
        });
    }

    /**
     * 获取或创建连接 (带并发锁)
     */
    async getConnection(serverId, serverConfig) {
        // 1. 如果已有活跃连接且通过校验，直接返回
        if (this.connections.has(serverId)) {
            const conn = this.connections.get(serverId);
            try {
                // 极简校验，如果 Socket 已关闭则直接抛错进入重建
                if (!conn.client._sock || conn.client._sock.destroyed) {
                    throw new Error('Socket destroyed');
                }
                conn.lastUsed = Date.now();
                return conn.client;
            } catch (e) {
                this.closeConnection(serverId);
            }
        }

        // 2. 如果正在连接中，等待现有的 Promise
        if (this.connectionPromises.has(serverId)) {
            return this.connectionPromises.get(serverId);
        }

        // 3. 开启新的连接任务
        const connectPromise = (async () => {
            try {
                const client = await this.connect(serverConfig);

                client.on('error', (err) => {
                    logger.error(`[SSH] 连接中断 (${serverId}): ${err.message}`);
                    this.connections.delete(serverId);
                });
                client.on('end', () => this.connections.delete(serverId));
                client.on('close', () => this.connections.delete(serverId));

                this.connections.set(serverId, {
                    client,
                    lastUsed: Date.now(),
                    serverId
                });
                return client;
            } finally {
                this.connectionPromises.delete(serverId);
            }
        })();

        this.connectionPromises.set(serverId, connectPromise);
        return connectPromise;
    }

    /**
     * 执行流式命令 (长任务)
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @param {string} command - 要执行的循环脚本
     * @returns {Promise<Object>} 包含 stream 的对象
     */
    async executeStream(serverId, serverConfig, command) {
        try {
            const client = await this.getConnection(serverId, serverConfig);

            return new Promise((resolve, reject) => {
                client.exec(command, (err, stream) => {
                    if (err) return reject(err);

                    // 标记连接活动，防止被清理
                    const conn = this.connections.get(serverId);
                    if (conn) conn.lastUsed = Date.now();

                    resolve(stream);
                });
            });
        } catch (error) {
            logger.error(`无法开启流式传输 (${serverConfig.host}): ${error.message}`);
            throw error;
        }
    }

    /**
     * 执行命令 (带重试机制)
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @param {string} command - 要执行的命令
     * @param {number} maxRetries - 最大重试次数
     * @returns {Promise<Object>} 命令执行结果
     */
    async executeCommand(serverId, serverConfig, command, maxRetries = 2) {
        let lastError = null;

        for (let attempt = 0; attempt <= maxRetries; attempt++) {
            const startTime = Date.now();
            try {
                if (attempt > 0) {
                    // console.log(`[SSH] 重试第 ${attempt} 次: ${serverId}`);
                    // 重试前稍微等待
                    await new Promise(resolve => setTimeout(resolve, 1000 * attempt));
                }

                const client = await this.getConnection(serverId, serverConfig);

                return await new Promise((resolve, reject) => {
                    client.exec(command, (err, stream) => {
                        if (err) {
                            reject(err);
                            return;
                        }

                        let stdout = '';
                        let stderr = '';

                        stream.on('close', (code, signal) => {
                            const responseTime = Date.now() - startTime;
                            resolve({
                                success: code === 0,
                                code,
                                signal,
                                stdout,
                                stderr,
                                responseTime
                            });
                        });

                        stream.on('data', (data) => {
                            stdout += data.toString();
                        });

                        stream.stderr.on('data', (data) => {
                            stderr += data.toString();
                        });
                    });
                });
            } catch (error) {
                lastError = error;
                const responseTime = Date.now() - startTime;

                // 如果是连接错误，清理连接以便下次重试重新建立
                if (error.message.includes('Not connected') || error.message.includes('Socket is closed')) {
                    this.closeConnection(serverId);
                }

                if (attempt === maxRetries) {
                    return {
                        success: false,
                        error: error.message,
                        responseTime,
                        attempts: attempt + 1
                    };
                }
            }
        }
    }

    /**
     * 测试连接
     * @param {Object} serverConfig - 主机配置
     * @returns {Promise<Object>} 测试结果
     */
    async testConnection(serverConfig) {
        const startTime = Date.now();

        try {
            const client = await this.connect(serverConfig);

            // 执行简单命令测试
            const result = await new Promise((resolve, reject) => {
                client.exec('echo "test"', (err, stream) => {
                    if (err) {
                        reject(err);
                        return;
                    }

                    let output = '';
                    stream.on('close', (code) => {
                        resolve({ success: code === 0, output });
                    });

                    stream.on('data', (data) => {
                        output += data.toString();
                    });
                });
            });

            // 关闭测试连接
            client.end();

            const responseTime = Date.now() - startTime;

            return {
                success: true,
                responseTime,
                message: '连接成功'
            };
        } catch (error) {
            const responseTime = Date.now() - startTime;
            return {
                success: false,
                responseTime,
                error: error.message
            };
        }
    }

    /**
     * 关闭连接
     * @param {string} serverId - 主机 ID
     */
    closeConnection(serverId) {
        if (this.connections.has(serverId)) {
            const conn = this.connections.get(serverId);
            conn.client.end();
            this.connections.delete(serverId);
            logger.info(`已断开并清理连接: ${serverId}`);
        }
    }

    /**
     * 关闭所有连接
     */
    closeAllConnections() {
        for (const [serverId, conn] of this.connections) {
            conn.client.end();
        }
        this.connections.clear();
    }

    /**
     * 清理最久未使用的连接
     */
    cleanupOldestConnection() {
        let oldestServerId = null;
        let oldestTime = Date.now();

        for (const [serverId, conn] of this.connections) {
            if (conn.lastUsed < oldestTime) {
                oldestTime = conn.lastUsed;
                oldestServerId = serverId;
            }
        }

        if (oldestServerId) {
            this.closeConnection(oldestServerId);
        }
    }

    /**
     * 启动定时清理任务
     */
    startCleanupTimer() {
        // 每分钟检查一次过期连接
        setInterval(() => {
            const config = ServerMonitorConfig.get();
            const sessionTimeout = (config?.session_timeout || 1800) * 1000;
            const now = Date.now();

            for (const [serverId, conn] of this.connections) {
                if (now - conn.lastUsed > sessionTimeout) {
                    console.log(`清理过期 SSH 连接: ${serverId}`);
                    this.closeConnection(serverId);
                }
            }
        }, 60000); // 每分钟执行一次
    }

    /**
     * 获取连接池状态
     * @returns {Object} 连接池状态
     */
    getStatus() {
        const connections = [];
        for (const [serverId, conn] of this.connections) {
            connections.push({
                serverId,
                lastUsed: conn.lastUsed,
                idleTime: Date.now() - conn.lastUsed
            });
        }

        return {
            totalConnections: this.connections.size,
            connections
        };
    }
}

// 导出单例
module.exports = new SSHService();
