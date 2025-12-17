/**
 * SSH 终端 WebSocket 服务
 * 提供真正的交互式 SSH 终端体验
 */

const { WebSocketServer } = require('ws');
const { Client } = require('ssh2');
const { createLogger } = require('../../src/utils/logger');
const { serverStorage } = require('./storage');

const logger = createLogger('SSHTerminal');

// 存储活跃的 SSH 连接
const activeConnections = new Map();

/**
 * 初始化 WebSocket 服务
 * @param {http.Server} server - HTTP 服务器实例
 */
function init(server) {
    const wss = new WebSocketServer({
        server,
        path: '/ws/ssh'
    });

    wss.on('connection', (ws, req) => {
        logger.info('新的 WebSocket 连接');

        let sshClient = null;
        let sshStream = null;
        let connectionId = null;

        ws.on('message', async (message) => {
            try {
                const data = JSON.parse(message.toString());

                switch (data.type) {
                    case 'connect':
                        // 连接到 SSH 服务器
                        const { serverId } = data;
                        const serverConfig = serverStorage.getById(serverId);

                        if (!serverConfig) {
                            ws.send(JSON.stringify({
                                type: 'error',
                                message: '主机不存在'
                            }));
                            return;
                        }

                        connectionId = `${serverId}-${Date.now()}`;
                        sshClient = new Client();

                        sshClient.on('ready', () => {
                            logger.info(`SSH 连接成功: ${serverConfig.host}`);

                            // 请求一个交互式 shell
                            sshClient.shell({
                                term: 'xterm-256color',
                                cols: data.cols || 80,
                                rows: data.rows || 24
                            }, (err, stream) => {
                                if (err) {
                                    ws.send(JSON.stringify({
                                        type: 'error',
                                        message: err.message
                                    }));
                                    return;
                                }

                                sshStream = stream;
                                activeConnections.set(connectionId, { client: sshClient, stream: sshStream });

                                ws.send(JSON.stringify({
                                    type: 'connected',
                                    message: `已连接到 ${serverConfig.host}`
                                }));

                                // 将 SSH 输出发送到 WebSocket
                                stream.on('data', (chunk) => {
                                    ws.send(JSON.stringify({
                                        type: 'output',
                                        data: chunk.toString('utf8')
                                    }));
                                });

                                stream.stderr.on('data', (chunk) => {
                                    ws.send(JSON.stringify({
                                        type: 'output',
                                        data: chunk.toString('utf8')
                                    }));
                                });

                                stream.on('close', () => {
                                    ws.send(JSON.stringify({
                                        type: 'disconnected',
                                        message: 'SSH 连接已关闭'
                                    }));
                                    cleanup();
                                });
                            });
                        });

                        sshClient.on('error', (err) => {
                            logger.error(`SSH 连接错误: ${err.message}`);
                            ws.send(JSON.stringify({
                                type: 'error',
                                message: `连接失败: ${err.message}`
                            }));
                        });

                        sshClient.on('close', () => {
                            ws.send(JSON.stringify({
                                type: 'disconnected',
                                message: 'SSH 连接已断开'
                            }));
                            cleanup();
                        });

                        // 构建 SSH 连接配置
                        const sshConfig = {
                            host: serverConfig.host,
                            port: serverConfig.port || 22,
                            username: serverConfig.username,
                            readyTimeout: 10000
                        };

                        if (serverConfig.auth_type === 'password') {
                            sshConfig.password = serverConfig.password;
                        } else if (serverConfig.auth_type === 'key') {
                            sshConfig.privateKey = serverConfig.private_key;
                            if (serverConfig.passphrase) {
                                sshConfig.passphrase = serverConfig.passphrase;
                            }
                        }

                        sshClient.connect(sshConfig);
                        break;

                    case 'input':
                        // 将用户输入发送到 SSH
                        if (sshStream) {
                            sshStream.write(data.data);
                        }
                        break;

                    case 'resize':
                        // 调整终端大小
                        if (sshStream) {
                            sshStream.setWindow(data.rows, data.cols, data.height || 480, data.width || 640);
                        }
                        break;

                    case 'disconnect':
                        cleanup();
                        break;
                }
            } catch (error) {
                logger.error(`处理消息错误: ${error.message}`);
                ws.send(JSON.stringify({
                    type: 'error',
                    message: error.message
                }));
            }
        });

        ws.on('close', () => {
            logger.info('WebSocket 连接关闭');
            cleanup();
        });

        ws.on('error', (error) => {
            logger.error(`WebSocket 错误: ${error.message}`);
            cleanup();
        });

        function cleanup() {
            if (sshStream) {
                sshStream.end();
                sshStream = null;
            }
            if (sshClient) {
                sshClient.end();
                sshClient = null;
            }
            if (connectionId) {
                activeConnections.delete(connectionId);
                connectionId = null;
            }
        }
    });

    logger.info('SSH 终端 WebSocket 服务已启动');
}

/**
 * 关闭所有活跃连接
 */
function closeAll() {
    for (const [id, conn] of activeConnections) {
        if (conn.stream) conn.stream.end();
        if (conn.client) conn.client.end();
    }
    activeConnections.clear();
}

module.exports = {
    init,
    closeAll
};
