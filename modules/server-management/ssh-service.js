/**
 * SSH 连接服务
 * 管理 SSH 连接池和命令执行
 */

const { Client } = require('ssh2');
const { ServerMonitorConfig } = require('../../src/db/models');

class SSHService {
    constructor() {
        // SSH 连接池：{ serverId: { client, lastUsed, serverId } }
        this.connections = new Map();

        // 定时清理过期连接
        this.startCleanupTimer();
    }

    /**
     * 创建 SSH 连接
     * @param {Object} serverConfig - 服务器配置
     * @returns {Promise<Client>} SSH 客户端
     */
    async connect(serverConfig) {
        return new Promise((resolve, reject) => {
            const client = new Client();
            const config = ServerMonitorConfig.get();
            const timeout = (config?.probe_timeout || 10) * 1000;

            // 连接配置
            const sshConfig = {
                host: serverConfig.host,
                port: serverConfig.port || 22,
                username: serverConfig.username,
                readyTimeout: timeout
            };

            // 根据认证方式添加配置
            if (serverConfig.auth_type === 'password') {
                sshConfig.password = serverConfig.password;
            } else if (serverConfig.auth_type === 'key') {
                sshConfig.privateKey = serverConfig.private_key;
                if (serverConfig.passphrase) {
                    sshConfig.passphrase = serverConfig.passphrase;
                }
            }

            // 连接事件处理
            client.on('ready', () => {
                resolve(client);
            });

            client.on('error', (err) => {
                reject(err);
            });

            // 发起连接
            client.connect(sshConfig);
        });
    }

    /**
     * 获取或创建连接
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
     * @returns {Promise<Client>} SSH 客户端
     */
    async getConnection(serverId, serverConfig) {
        // 检查连接池中是否有可用连接
        if (this.connections.has(serverId)) {
            const conn = this.connections.get(serverId);

            // 更新最后使用时间
            conn.lastUsed = Date.now();

            return conn.client;
        }

        // 检查连接数是否超过限制
        const config = ServerMonitorConfig.get();
        const maxConnections = config?.max_connections || 10;

        if (this.connections.size >= maxConnections) {
            // 清理最久未使用的连接
            this.cleanupOldestConnection();
        }

        // 创建新连接
        const client = await this.connect(serverConfig);

        // 添加到连接池
        this.connections.set(serverId, {
            client,
            lastUsed: Date.now(),
            serverId
        });

        return client;
    }

    /**
     * 执行命令
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
     * @param {string} command - 要执行的命令
     * @returns {Promise<Object>} 命令执行结果
     */
    async executeCommand(serverId, serverConfig, command) {
        const startTime = Date.now();

        try {
            const client = await this.getConnection(serverId, serverConfig);

            return new Promise((resolve, reject) => {
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
            const responseTime = Date.now() - startTime;
            return {
                success: false,
                error: error.message,
                responseTime
            };
        }
    }

    /**
     * 测试连接
     * @param {Object} serverConfig - 服务器配置
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
     * @param {string} serverId - 服务器 ID
     */
    closeConnection(serverId) {
        if (this.connections.has(serverId)) {
            const conn = this.connections.get(serverId);
            conn.client.end();
            this.connections.delete(serverId);
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
