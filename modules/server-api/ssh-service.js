/**
 * SSH 服务 - 处理 SSH 会话、端对端命令执行及 WebSocket 终端桥接
 */

const { Client } = require('ssh2');
const { WebSocketServer } = require('ws');
const { serverStorage } = require('./storage');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('SSHService');

class SSHService {
  constructor() {
    this.wss = null;
    this.activeConnections = new Map(); // id -> { ssh, ws }
  }

  /**
   * 初始化 WebSocket 服务
   * @param {http.Server} server - Node.js HTTP Server
   */
  init(server) {
    this.wss = new WebSocketServer({ noServer: true });

    this.wss.on('connection', (ws, req) => {
      logger.info('新的 SSH WebSocket 连接已建立');
      let sshClient = null;
      let shellStream = null;
      let currentServerId = null;

      ws.on('message', async message => {
        try {
          const data = JSON.parse(message);

          switch (data.type) {
            case 'connect':
              const { serverId, cols, rows, protocol } = data;
              currentServerId = serverId;

              // 获取服务器配置
              const serverConfig = serverStorage.getById(serverId);
              if (!serverConfig) {
                ws.send(JSON.stringify({ type: 'error', message: '找不到服务器配置' }));
                return;
              }

              logger.info(`建立终端连接: serverId=${serverId}, protocol=${protocol || 'ssh'}`);

              // ==================== Agent PTY 模式 ====================
              if (protocol === 'agent') {
                const agentService = require('./agent-service');
                const { TaskTypes } = require('./protocol');
                const crypto = require('crypto');

                if (!agentService.isOnline(serverId)) {
                  logger.warn(`Agent 终端连接失败: Agent 离线 (serverId=${serverId})`);
                  ws.send(JSON.stringify({ type: 'error', message: 'Agent 离线，无法连接终端' }));
                  return;
                }

                const taskId = 'pty_' + crypto.randomBytes(4).toString('hex');
                ws._taskId = taskId;
                ws._serverId = serverId;
                ws._protocol = 'agent';

                // 监听来自 Agent 的输出
                const ptyOutputHandler = ptyData => {
                  if (ws.readyState === ws.OPEN) {
                    ws.send(JSON.stringify({ type: 'output', data: ptyData }));
                  }
                };
                agentService.on(`pty:${taskId}`, ptyOutputHandler);
                ws._ptyOutputHandler = ptyOutputHandler;

                // 下发启动 PTY 任务
                agentService.sendTask(serverId, {
                  id: taskId,
                  type: TaskTypes.PTY_START,
                  data: JSON.stringify({ cols, rows }),
                });

                logger.info(`Agent PTY 会话已初始化: taskId=${taskId}, serverId=${serverId}`);
                ws.send(JSON.stringify({ type: 'connected', message: 'Agent PTY 会话已启动' }));
                return;
              }

              // ==================== 标准 SSH 模式 ====================
              logger.info(`尝试建立 SSH 连接: serverId=${serverId} (${serverConfig.name})`);
              sshClient = new Client();

              sshClient.on('ready', () => {
                ws.send(JSON.stringify({ type: 'connected', message: 'SSH 连接已就绪' }));

                // 开启交互式 Shell
                sshClient.shell(
                  { term: 'xterm-256color', cols: cols || 80, rows: rows || 24 },
                  (err, stream) => {
                    if (err) {
                      ws.send(
                        JSON.stringify({ type: 'error', message: '无法开启 Shell: ' + err.message })
                      );
                      return;
                    }

                    shellStream = stream;

                    // 桥接流数据
                    stream.on('data', chunk => {
                      ws.send(JSON.stringify({ type: 'output', data: chunk.toString() }));
                    });

                    stream.on('close', () => {
                      sshClient.end();
                    });
                  }
                );
              });

              sshClient.on('error', err => {
                logger.error(`SSH 连接错误 (${serverConfig.name}): ${err.message}`);
                ws.send(JSON.stringify({ type: 'error', message: 'SSH 错误: ' + err.message }));
              });

              sshClient.on('close', () => {
                ws.send(JSON.stringify({ type: 'disconnected', message: 'SSH 连接已关闭' }));
              });

              // 准备连接参数
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

              sshClient.connect(connSettings);
              break;

            case 'input':
              if (ws._protocol === 'agent') {
                const agentService = require('./agent-service');
                const { Events } = require('./protocol');
                agentService.sendTask(ws._serverId, {
                  type: Events.DASHBOARD_PTY_INPUT, // 注意这里我们直接复用事件名作为类型标识或使用特定指令
                  id: ws._taskId,
                  data: data.data,
                });
                // 修正：实际应该发送 socket.io 事件
                const socket = agentService.connections.get(ws._serverId);
                if (socket) {
                  socket.emit(Events.DASHBOARD_PTY_INPUT, { id: ws._taskId, data: data.data });
                }
              } else if (shellStream) {
                shellStream.write(data.data);
              }
              break;

            case 'resize':
              if (ws._protocol === 'agent') {
                const agentService = require('./agent-service');
                const { Events } = require('./protocol');
                const socket = agentService.connections.get(ws._serverId);
                if (socket) {
                  socket.emit(Events.DASHBOARD_PTY_RESIZE, {
                    id: ws._taskId,
                    cols: data.cols,
                    rows: data.rows,
                  });
                }
              } else if (shellStream) {
                shellStream.setWindow(data.rows, data.cols, 0, 0);
              }
              break;

            case 'ping':
              ws.send(JSON.stringify({ type: 'pong' }));
              break;

            case 'disconnect':
              if (sshClient) sshClient.end();
              break;
          }
        } catch (err) {
          logger.error('处理 WebSocket 消息失败:', err);
        }
      });

      ws.on('close', () => {
        if (sshClient) sshClient.end();
        if (ws._protocol === 'agent' && ws._taskId) {
          const agentService = require('./agent-service');
          if (ws._ptyOutputHandler) {
            agentService.off(`pty:${ws._taskId}`, ws._ptyOutputHandler);
          }
        }
        logger.info('SSH/Agent WebSocket 连接已关闭');
      });
    });

    return this.wss;
  }

  /**
   * 处理 WebSocket 升级
   */
  handleUpgrade(request, socket, head, callback) {
    this.wss.handleUpgrade(request, socket, head, callback);
  }

  /**
   * 执行单个命令并返回结果 (用于测试连接或快速探测)
   */
  executeCommand(id, serverConfig, command, timeout = 10000) {
    return new Promise(resolve => {
      const conn = new Client();
      let resolved = false;

      // 如果 timeout 为 0 或无效，使用默认值
      const actualTimeout = timeout && timeout > 0 ? timeout : 10000;

      const timer = setTimeout(() => {
        if (!resolved) {
          resolved = true;
          conn.end();
          resolve({ success: false, error: `连接超时 (${actualTimeout}ms)` });
        }
      }, actualTimeout);

      conn
        .on('ready', () => {
          conn.exec(command, (err, stream) => {
            if (err) {
              clearTimeout(timer);
              resolved = true;
              conn.end();
              return resolve({ success: false, error: err.message });
            }

            let stdout = '';
            let stderr = '';

            stream.on('data', data => (stdout += data.toString()));
            stream.stderr.on('data', data => (stderr += data.toString()));

            stream.on('close', code => {
              clearTimeout(timer);
              resolved = true;
              conn.end();
              resolve({
                success: code === 0,
                stdout: stdout.trim(),
                stderr: stderr.trim(),
                code,
              });
            });
          });
        })
        .on('error', err => {
          if (!resolved) {
            clearTimeout(timer);
            resolved = true;
            conn.end();
            resolve({ success: false, error: err.message });
          }
        })
        .connect({
          host: serverConfig.host,
          port: serverConfig.port || 22,
          username: serverConfig.username,
          password: serverConfig.auth_type === 'password' ? serverConfig.password : undefined,
          privateKey: serverConfig.auth_type === 'key' ? serverConfig.private_key : undefined,
          passphrase: serverConfig.passphrase,
          readyTimeout: actualTimeout,
        });
    });
  }

  /**
   * 关闭指定 ID 的活跃连接 (占位符，如果需要管理池化连接)
   */
  closeConnection(id) {
    // 对于单次执行，连接在执行完后已自动关闭
    // 对于 WebSocket，连接随 WS 关闭而关闭
  }
}

module.exports = new SSHService();
