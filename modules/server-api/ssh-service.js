/**
 * SSH 服务 - 处理 SSH 会话、端对端命令执行及 WebSocket 终端桥接
 */

const { Client } = require('ssh2');
const { WebSocketServer } = require('ws');
const { serverStorage } = require('./storage');
const { getServerCapabilities, hasSshConfig } = require('./capabilities');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('SSHService');
const TERMINAL_SESSION_TTL_MS = 10 * 60 * 1000;
const TERMINAL_BUFFER_LIMIT = 5000;

const isOpen = ws => ws && ws.readyState === 1;

class TerminalSession {
  constructor(owner, options) {
    this.owner = owner;
    this.id = options.id;
    this.serverId = options.serverId;
    this.protocol = options.protocol;
    this.serverConfig = options.serverConfig;
    this.cols = options.cols || 80;
    this.rows = options.rows || 24;
    this.ws = null;
    this.sshClient = null;
    this.shellStream = null;
    this.taskId = null;
    this.ptyOutputHandler = null;
    this.connected = false;
    this.closed = false;
    this.started = false;
    this.seq = 0;
    this.buffer = [];
    this.cleanupTimer = null;
  }

  attach(ws, lastSeq = 0) {
    if (this.closed) {
      ws.send(JSON.stringify({ type: 'error', message: '终端会话已关闭' }));
      return;
    }

    if (isOpen(this.ws) && this.ws !== ws) {
      this.ws.send(JSON.stringify({ type: 'detached', message: '终端已在新的窗口恢复' }));
      this.ws.close();
    }

    this.ws = ws;
    ws._terminalSession = this;
    ws._terminalSessionId = this.id;
    ws._protocol = this.protocol;
    ws._serverId = this.serverId;
    ws._taskId = this.taskId;
    this.clearCleanupTimer();

    if (this.connected) {
      this.send({ type: 'connected', resumed: this.started, protocol: this.protocol });
      this.replayFrom(lastSeq);
    }
  }

  start() {
    if (this.started) return;
    this.started = true;
    if (this.protocol === 'agent') {
      this.startAgentPty();
      return;
    }
    this.startSsh();
  }

  startAgentPty() {
    const agentService = require('./agent-service');
    const { TaskTypes } = require('./protocol');

    if (!agentService.isOnline(this.serverId)) {
      this.fail('Agent 离线，无法连接终端');
      return;
    }

    this.taskId = this.id;
    if (this.ws) this.ws._taskId = this.taskId;

    this.ptyOutputHandler = ptyData => this.pushOutput(ptyData);
    agentService.on(`pty:${this.taskId}`, this.ptyOutputHandler);
    agentService.sendTask(this.serverId, {
      id: this.taskId,
      type: TaskTypes.PTY_START,
      data: JSON.stringify({ cols: this.cols, rows: this.rows }),
    });

    this.connected = true;
    this.send({ type: 'connected', resumed: false, protocol: 'agent', message: 'Agent PTY 会话已启动' });
  }

  startSsh() {
    this.sshClient = new Client();

    this.sshClient.on('ready', () => {
      this.sshClient.shell(
        { term: 'xterm-256color', cols: this.cols, rows: this.rows },
        (err, stream) => {
          if (err) {
            this.fail('无法开启 Shell: ' + err.message);
            return;
          }

          this.shellStream = stream;
          this.connected = true;
          this.send({ type: 'connected', resumed: false, protocol: 'ssh', message: 'SSH 连接已就绪' });

          stream.on('data', chunk => this.pushOutput(chunk.toString()));
          stream.on('close', () => this.close('SSH 连接已关闭', true));
        }
      );
    });

    this.sshClient.on('error', err => {
      logger.error(`SSH 连接错误 (${this.serverConfig.name}): ${err.message}`);
      this.fail('SSH 错误: ' + err.message);
    });

    this.sshClient.on('close', () => {
      if (!this.closed) this.close('SSH 连接已关闭', true);
    });

    const connSettings = {
      host: this.serverConfig.host,
      port: this.serverConfig.port || 22,
      username: this.serverConfig.username,
      readyTimeout: 20000,
      keepaliveInterval: 15000,
    };

    if (this.serverConfig.auth_type === 'key') {
      connSettings.privateKey = this.serverConfig.private_key;
      if (this.serverConfig.passphrase) connSettings.passphrase = this.serverConfig.passphrase;
    } else {
      connSettings.password = this.serverConfig.password;
    }

    this.sshClient.connect(connSettings);
  }

  input(data) {
    if (this.closed) return;
    if (this.protocol === 'agent') {
      const agentService = require('./agent-service');
      const { Events } = require('./protocol');
      const socket = agentService.connections.get(this.serverId);
      if (socket) socket.emit(Events.DASHBOARD_PTY_INPUT, { id: this.taskId, data });
      return;
    }
    if (this.shellStream) this.shellStream.write(data);
  }

  resize(cols, rows) {
    if (this.closed) return;
    this.cols = cols || this.cols;
    this.rows = rows || this.rows;

    if (this.protocol === 'agent') {
      const agentService = require('./agent-service');
      const { Events } = require('./protocol');
      const socket = agentService.connections.get(this.serverId);
      if (socket) {
        socket.emit(Events.DASHBOARD_PTY_RESIZE, {
          id: this.taskId,
          cols: this.cols,
          rows: this.rows,
        });
      }
      return;
    }

    if (this.shellStream) {
      this.shellStream.setWindow(this.rows, this.cols, 0, 0);
    }
  }

  detach(ws) {
    if (this.ws === ws) this.ws = null;
    this.scheduleCleanup();
  }

  replayFrom(lastSeq = 0) {
    const seq = Number(lastSeq) || 0;
    const chunks = this.buffer.filter(item => item.seq > seq);
    if (chunks.length === 0) return;
    this.send({
      type: 'history',
      fromSeq: chunks[0].seq,
      toSeq: chunks[chunks.length - 1].seq,
      data: chunks.map(item => item.data).join(''),
    });
  }

  pushOutput(data) {
    if (!data) return;
    this.seq += 1;
    const chunk = { seq: this.seq, data };
    this.buffer.push(chunk);
    if (this.buffer.length > TERMINAL_BUFFER_LIMIT) this.buffer.shift();
    this.send({ type: 'output', seq: chunk.seq, data });
  }

  send(payload) {
    if (isOpen(this.ws)) {
      this.ws.send(JSON.stringify(payload));
    }
  }

  fail(message) {
    this.send({ type: 'error', message });
    this.close(message, false);
  }

  close(message = '终端会话已关闭', notify = true) {
    if (this.closed) return;
    this.closed = true;
    this.connected = false;

    if (this.protocol === 'agent' && this.ptyOutputHandler && this.taskId) {
      const agentService = require('./agent-service');
      agentService.off(`pty:${this.taskId}`, this.ptyOutputHandler);
    }

    if (this.shellStream) {
      try { this.shellStream.end(); } catch (e) {}
      this.shellStream = null;
    }
    if (this.sshClient) {
      try { this.sshClient.end(); } catch (e) {}
      this.sshClient = null;
    }

    if (notify) this.send({ type: 'disconnected', message });
    this.clearCleanupTimer();
    this.owner.activeConnections.delete(this.id);
  }

  scheduleCleanup() {
    if (this.closed || this.cleanupTimer) return;
    this.cleanupTimer = setTimeout(() => {
      this.close('终端会话空闲超时，已关闭', true);
    }, TERMINAL_SESSION_TTL_MS);
    if (typeof this.cleanupTimer.unref === 'function') this.cleanupTimer.unref();
  }

  clearCleanupTimer() {
    if (!this.cleanupTimer) return;
    clearTimeout(this.cleanupTimer);
    this.cleanupTimer = null;
  }
}

class SSHService {
  constructor() {
    this.wss = null;
    this.activeConnections = new Map(); // sessionId -> TerminalSession
  }

  /**
   * 初始化 WebSocket 服务
   * @param {http.Server} server - Node.js HTTP Server
   */
  init(server) {
    this.wss = new WebSocketServer({ noServer: true });

    this.wss.on('connection', ws => {
      logger.info('新的 SSH WebSocket 连接已建立');
      ws.on('message', async message => {
        try {
          const data = JSON.parse(message);

          switch (data.type) {
            case 'connect': {
              const { serverId, cols, rows, protocol, sessionId, lastSeq } = data;
              const terminalSessionId = sessionId || `${serverId}:${protocol || 'ssh'}`;

              // 获取服务器配置
              const serverConfig = serverStorage.getById(serverId);
              if (!serverConfig) {
                ws.send(JSON.stringify({ type: 'error', message: '找不到服务器配置' }));
                return;
              }

              let existingSession = this.activeConnections.get(terminalSessionId);
              if (existingSession?.closed) {
                this.activeConnections.delete(terminalSessionId);
                existingSession = null;
              }

              if (existingSession) {
                if (existingSession.serverId !== serverId) {
                  ws.send(JSON.stringify({ type: 'error', message: '终端会话归属主机不匹配' }));
                  return;
                }
                existingSession.attach(ws, lastSeq);
                existingSession.resize(cols, rows);
                return;
              }

              logger.info(`建立终端连接: sessionId=${terminalSessionId}, serverId=${serverId}, protocol=${protocol || 'ssh'}`);
              let resolvedProtocol = protocol === 'agent' ? 'agent' : 'ssh';

              if (protocol === 'agent') {
                const agentService = require('./agent-service');
                const capabilities = getServerCapabilities(serverConfig, {
                  agentOnline: agentService.isOnline(serverId),
                });

                if (!capabilities.agent_online) {
                  if (capabilities.ssh_configured) {
                    resolvedProtocol = 'ssh';
                    logger.warn(`Agent 离线，终端自动降级到 SSH (serverId=${serverId})`);
                    ws.send(JSON.stringify({
                      type: 'output',
                      data: '\r\n\x1b[1;33mAgent 离线，正在切换到 SSH...\x1b[0m\r\n',
                    }));
                  } else {
                    logger.warn(`Agent 终端连接失败: Agent 离线 (serverId=${serverId})`);
                    ws.send(JSON.stringify({ type: 'error', message: 'Agent 离线，且未配置可用 SSH 凭据' }));
                    return;
                  }
                }
              }

              if (resolvedProtocol === 'ssh' && !hasSshConfig(serverConfig)) {
                ws.send(JSON.stringify({ type: 'error', message: 'SSH 主机或凭据不完整' }));
                return;
              }

              const session = new TerminalSession(this, {
                id: terminalSessionId,
                serverId,
                protocol: resolvedProtocol,
                serverConfig,
                cols,
                rows,
              });
              this.activeConnections.set(terminalSessionId, session);
              session.attach(ws, lastSeq);
              session.start();
              break;
            }

            case 'input':
              ws._terminalSession?.input(data.data);
              break;

            case 'resize':
              ws._terminalSession?.resize(data.cols, data.rows);
              break;

            case 'ping':
              ws.send(JSON.stringify({ type: 'pong' }));
              break;

            case 'disconnect':
              ws._terminalSession?.close('客户端主动关闭终端会话', true);
              break;
          }
        } catch (err) {
          logger.error('处理 WebSocket 消息失败:', err);
        }
      });

      ws.on('close', () => {
        ws._terminalSession?.detach(ws);
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
    for (const session of this.activeConnections.values()) {
      if (session.id === id || session.serverId === id) {
        session.close('会话已被管理端关闭', true);
      }
    }
  }
}

module.exports = new SSHService();
