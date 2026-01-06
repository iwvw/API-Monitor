/**
 * 路由汇总
 */

// Aliyun routes reloaded
const express = require('express');
const path = require('path');
const fs = require('fs');
const { requireAuth } = require('../middleware/auth');

const { loadUserSettings } = require('../services/userSettings');

// 导入核心路由模块
const authRouter = require('./auth');
const healthRouter = require('./health');
const settingsRouter = require('./settings');
const logService = require('../services/log-service');
const v1Router = require('./v1');
const { createLogger } = require('../utils/logger');

const logger = createLogger('Router');

/**
 * 注册所有路由
 */
function registerRoutes(app) {
  // 1. 基础系统路由 (无需/需认证)
  app.use('/health', healthRouter);
  app.use('/api/settings', requireAuth, settingsRouter);
  app.use('/api/logs', logService.router);
  app.use('/v1', v1Router);

  // 3. 独立认证路由 (避免干扰 /api/xxxx)
  app.use('/api/auth', authRouter);

  // ==================== Chat History API ====================
  const chatRouter = express.Router();
  const db = require('../db/database');

  // 获取所有聊天会话
  chatRouter.get('/sessions', (req, res) => {
    try {
      const database = db.getDatabase();
      const sessions = database.prepare(`
        SELECT s.*, 
               (SELECT content FROM chat_messages WHERE session_id = s.id ORDER BY created_at DESC LIMIT 1) as last_message
        FROM chat_sessions s 
        ORDER BY s.updated_at DESC
      `).all();
      res.json({ success: true, data: sessions });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 创建新会话
  chatRouter.post('/sessions', (req, res) => {
    try {
      const { title, model, endpoint_id, persona_id, system_prompt } = req.body;
      const database = db.getDatabase();
      const result = database.prepare(`
        INSERT INTO chat_sessions (title, model, endpoint_id, persona_id, system_prompt) VALUES (?, ?, ?, ?, ?)
      `).run(title || '新对话', model || '', endpoint_id || '', persona_id || null, system_prompt || '');

      const session = database.prepare('SELECT * FROM chat_sessions WHERE id = ?').get(result.lastInsertRowid);
      res.json({ success: true, data: session });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 获取会话消息
  chatRouter.get('/sessions/:id/messages', (req, res) => {
    try {
      const { id } = req.params;
      const database = db.getDatabase();
      const messages = database.prepare(`
        SELECT * FROM chat_messages WHERE session_id = ? ORDER BY created_at ASC
      `).all(id);
      res.json({ success: true, data: messages });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 更新会话信息
  chatRouter.put('/sessions/:id', (req, res) => {
    try {
      const { id } = req.params;
      const { title, model, endpoint_id, persona_id, system_prompt } = req.body;
      const database = db.getDatabase();
      database.prepare(`
        UPDATE chat_sessions SET title = ?, model = ?, endpoint_id = ?, persona_id = ?, system_prompt = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
      `).run(title, model, endpoint_id || '', persona_id || null, system_prompt, id);

      const session = database.prepare('SELECT * FROM chat_sessions WHERE id = ?').get(id);
      res.json({ success: true, data: session });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 删除会话
  chatRouter.delete('/sessions/:id', (req, res) => {
    try {
      const { id } = req.params;
      const database = db.getDatabase();
      database.prepare('DELETE FROM chat_sessions WHERE id = ?').run(id);
      database.prepare('DELETE FROM chat_messages WHERE session_id = ?').run(id);
      res.json({ success: true });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 批量删除/清空会话
  chatRouter.delete('/sessions', (req, res) => {
    try {
      const { ids } = req.body;
      const database = db.getDatabase();

      if (ids && Array.isArray(ids)) {
        // 删除选定的会话
        const placeholders = ids.map(() => '?').join(',');
        database.prepare(`DELETE FROM chat_sessions WHERE id IN (${placeholders})`).run(...ids);
        database.prepare(`DELETE FROM chat_messages WHERE session_id IN (${placeholders})`).run(...ids);
      } else {
        // 清空所有会话
        database.prepare('DELETE FROM chat_sessions').run();
        database.prepare('DELETE FROM chat_messages').run();
      }

      res.json({ success: true });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 保存消息到会话
  chatRouter.post('/sessions/:id/messages', (req, res) => {
    try {
      const { id } = req.params;
      const { role, content, reasoning } = req.body;
      const database = db.getDatabase();

      const result = database.prepare(`
        INSERT INTO chat_messages (session_id, role, content, reasoning) VALUES (?, ?, ?, ?)
      `).run(id, role, content, reasoning || null);

      // 更新会话的 updated_at
      database.prepare('UPDATE chat_sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(id);

      const message = database.prepare('SELECT * FROM chat_messages WHERE id = ?').get(result.lastInsertRowid);
      res.json({ success: true, data: message });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 清空会话的所有消息
  chatRouter.delete('/sessions/:id/messages', (req, res) => {
    try {
      const { id } = req.params;
      const database = db.getDatabase();
      database.prepare('DELETE FROM chat_messages WHERE session_id = ?').run(id);
      res.json({ success: true });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 删除单条消息
  chatRouter.delete('/sessions/:sessionId/messages/:messageId', (req, res) => {
    try {
      const { sessionId, messageId } = req.params;
      const database = db.getDatabase();
      database.prepare('DELETE FROM chat_messages WHERE id = ? AND session_id = ?').run(messageId, sessionId);
      res.json({ success: true });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  app.use('/api/chat', requireAuth, chatRouter);
  logger.info('Chat History API 已挂载 -> /api/chat');

  // ==================== Persona API (人设系统) ====================
  const personaRouter = express.Router();

  // 获取所有人设
  personaRouter.get('/', (req, res) => {
    try {
      const database = db.getDatabase();
      const personas = database.prepare('SELECT * FROM chat_personas ORDER BY is_default DESC, id ASC').all();
      res.json({ success: true, data: personas });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 创建人设
  personaRouter.post('/', (req, res) => {
    try {
      const { name, system_prompt, icon } = req.body;
      if (!name || !system_prompt) {
        return res.status(400).json({ success: false, error: '名称和系统提示词不能为空' });
      }
      const database = db.getDatabase();
      const result = database.prepare(`
        INSERT INTO chat_personas (name, system_prompt, icon) VALUES (?, ?, ?)
      `).run(name, system_prompt, icon || 'fa-robot');
      const persona = database.prepare('SELECT * FROM chat_personas WHERE id = ?').get(result.lastInsertRowid);
      res.json({ success: true, data: persona });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 更新人设
  personaRouter.put('/:id', (req, res) => {
    try {
      const { id } = req.params;
      const { name, system_prompt, icon } = req.body;
      const database = db.getDatabase();
      database.prepare(`
        UPDATE chat_personas SET name = ?, system_prompt = ?, icon = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
      `).run(name, system_prompt, icon || 'fa-robot', id);
      const persona = database.prepare('SELECT * FROM chat_personas WHERE id = ?').get(id);
      res.json({ success: true, data: persona });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 删除人设
  personaRouter.delete('/:id', (req, res) => {
    try {
      const { id } = req.params;
      const database = db.getDatabase();
      // 不允许删除默认人设
      const persona = database.prepare('SELECT * FROM chat_personas WHERE id = ?').get(id);
      if (persona && persona.is_default) {
        return res.status(400).json({ success: false, error: '不能删除默认人设' });
      }
      database.prepare('DELETE FROM chat_personas WHERE id = ?').run(id);
      // 将使用该人设的会话重置为 null
      database.prepare('UPDATE chat_sessions SET persona_id = NULL WHERE persona_id = ?').run(id);
      res.json({ success: true });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  app.use('/api/personas', requireAuth, personaRouter);
  logger.info('Persona API 已挂载 -> /api/personas');

  // 3. Agent 公开接口 (不需要认证，必须在 /api/server 模块之前挂载)
  const agentPublicRouter = express.Router();
  const agentService = require('../../modules/server-api/agent-service');
  const { serverStorage } = require('../../modules/server-api/storage');

  // Agent 数据推送 (由远程 Agent 调用)
  agentPublicRouter.post('/push', (req, res) => {
    try {
      const serverId = req.headers['x-server-id'];
      const agentKey = req.headers['x-agent-key'];

      if (!serverId) {
        return res.status(400).json({ success: false, error: '缺少 Server ID' });
      }

      if (!agentService.verifyAgent(serverId, agentKey)) {
        return res.status(401).json({ success: false, error: '无效的 Agent 密钥' });
      }

      const metrics = agentService.processMetrics(serverId, req.body);
      serverStorage.updateStatus(serverId, { status: 'online' });

      logger.info(`[Agent Push] 收到来自服务器 ${serverId} 的指标数据`);
      res.json({ success: true, received: true });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 支持 GET 请求以供健康检查和调试
  agentPublicRouter.get('/push', (req, res) => {
    res.json({
      success: true,
      message: 'Agent 推送接口运行中',
      method: 'POST',
      tip: '请使用 POST 请求并携带正确的 Header (X-Server-ID, X-Agent-Key) 推送指标数据',
    });
  });

  // Agent 路由根路径说明
  agentPublicRouter.get('/', (req, res) => {
    const stats = agentService.getConnectionStats
      ? agentService.getConnectionStats()
      : { online: 0 };
    const protocol = req.protocol;
    const host = req.get('host');
    const baseUrl = `${protocol}://${host}`;

    res.json({
      success: true,
      message: 'API Monitor Agent 公开接口',
      version: '2.0.0',
      protocol: 'Socket.IO + HTTP (兼容)',
      connections: stats,
      quickInstall: {
        description: '一键安装：只需服务器名称，自动创建并生成安装命令',
        example: `curl -fsSL "${baseUrl}/api/server/agent/quick-install/我的服务器" | sudo bash`,
        api: {
          method: 'POST',
          url: `${baseUrl}/api/server/agent/quick-install`,
          body: '{ "name": "服务器名称" }',
          description: '需要认证，返回 JSON 格式的安装命令',
        },
      },
      endpoints: [
        { path: '/push', method: 'POST', description: '数据推送 (HTTP 兼容)' },
        { path: '/install/:serverId', method: 'GET', description: '安装脚本下载' },
        { path: '/quick-install/:name', method: 'GET', description: '一键安装脚本 (自动创建主机)' },
        { path: '/quick-install', method: 'POST', description: '快速安装 API (需要认证)' },
        { path: '/status', method: 'GET', description: 'Socket.IO 连接统计' },
      ],
      socketio: {
        namespace: '/agent',
        events: ['agent:connect', 'agent:state', 'agent:host_info'],
      },
    });
  });

  // Socket.IO 连接状态 (需要认证)
  agentPublicRouter.get('/status', requireAuth, (req, res) => {
    try {
      const stats = agentService.getConnectionStats ? agentService.getConnectionStats() : {};
      const onlineAgents = agentService.getOnlineAgents ? agentService.getOnlineAgents() : [];
      res.json({
        success: true,
        data: {
          ...stats,
          onlineAgents,
          protocol: 'socket.io',
          version: '2.0.0',
        },
      });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // Agent 安装脚本下载 (由远程服务器通过 curl 调用)
  agentPublicRouter.get('/install/:serverId', (req, res) => {
    try {
      const { serverId } = req.params;
      const server = serverStorage.getById(serverId);

      if (!server) {
        return res.status(404).send('# Error: Server not found');
      }

      const protocol = req.protocol;
      const host = req.get('host');
      const serverUrl = `${protocol}://${host}`;

      const script = agentService.generateInstallScript(serverId, serverUrl);

      res.setHeader('Content-Type', 'text/plain; charset=utf-8');
      res.send(script);
    } catch (error) {
      res.status(500).send(`# Error: ${error.message}`);
    }
  });

  // Windows Agent 安装脚本下载 (由远程服务器通过 PowerShell 调用)
  agentPublicRouter.get('/install/win/:serverId', (req, res) => {
    try {
      const { serverId } = req.params;
      const server = serverStorage.getById(serverId);

      if (!server) {
        return res.status(404).send('# Error: Server not found');
      }

      const settings = loadUserSettings();
      let serverUrl = settings.publicApiUrl;

      if (!serverUrl || serverUrl.trim() === '') {
        serverUrl = process.env.API_PUBLIC_URL;
      }

      if (!serverUrl || serverUrl.trim() === '') {
        const protocol = req.protocol;
        const host = req.get('host');
        serverUrl = `${protocol}://${host}`;
      }

      const script = agentService.generateWinInstallScript(serverId, serverUrl);

      // 设置为 UTF-8 编码，防止 PowerShell 处理中文乱码
      res.setHeader('Content-Type', 'text/plain; charset=utf-8');
      res.send(script);
    } catch (error) {
      res.status(500).send(`# Error: ${error.message}`);
    }
  });

  // Agent 安装脚本下载 (Linux, 带 Key 校验，匹配前端新生成规则)
  agentPublicRouter.get('/install/linux/:serverId/:agentKey', (req, res) => {
    try {
      const { serverId, agentKey } = req.params;
      const server = serverStorage.getById(serverId);

      if (!server) {
        return res.status(404).send('# Error: Server not found');
      }

      // 校验 Key
      const storedKey = agentService.getAgentKey(serverId);
      if (storedKey && storedKey !== agentKey) {
        return res.status(401).send('# Error: Invalid Agent Key');
      }

      // 修正: 确保 serverUrl 使用正确协议 (优先使用 用户设置 > API_PUBLIC_URL > 请求检测)
      const settings = loadUserSettings();
      let serverUrl = settings.publicApiUrl;

      if (!serverUrl || serverUrl.trim() === '') {
        serverUrl = process.env.API_PUBLIC_URL;
      }

      if (!serverUrl || serverUrl.trim() === '') {
        const protocol = req.protocol;
        const host = req.get('host');
        serverUrl = `${protocol}://${host}`;
      }

      const script = agentService.generateInstallScript(serverId, serverUrl);

      res.setHeader('Content-Type', 'text/plain; charset=utf-8');
      res.send(script);
    } catch (error) {
      res.status(500).send(`# Error: ${error.message}`);
    }
  });

  // Agent 安装脚本下载 (Windows, 带 Key 校验，匹配前端新生成规则)
  agentPublicRouter.get('/install/win/:serverId/:agentKey', (req, res) => {
    try {
      const { serverId, agentKey } = req.params;
      const server = serverStorage.getById(serverId);

      if (!server) {
        return res.status(404).send('# Error: Server not found');
      }

      // 校验 Key
      const storedKey = agentService.getAgentKey(serverId);
      if (storedKey && storedKey !== agentKey) {
        return res.status(401).send('# Error: Invalid Agent Key');
      }

      // 修正: 确保 serverUrl 使用正确协议 (优先使用 用户设置 > API_PUBLIC_URL > 请求检测)
      const settings = loadUserSettings();
      let serverUrl = settings.publicApiUrl;

      if (!serverUrl || serverUrl.trim() === '') {
        serverUrl = process.env.API_PUBLIC_URL;
      }

      if (!serverUrl || serverUrl.trim() === '') {
        const protocol = req.protocol;
        const host = req.get('host');
        serverUrl = `${protocol}://${host}`;
      }

      const script = agentService.generateWinInstallScript(serverId, serverUrl);

      // 设置为 UTF-8 编码
      res.setHeader('Content-Type', 'text/plain; charset=utf-8');
      res.send(script);
    } catch (error) {
      res.status(500).send(`# Error: ${error.message}`);
    }
  });

  // ==================== 快速安装 API ====================
  // 只需输入名称，自动创建主机并生成安装命令

  /**
   * POST /api/server/agent/quick-install
   * Body: { name: "服务器名称" }
   *
   * 创建一个新的主机记录并返回一键安装命令
   */
  agentPublicRouter.post('/quick-install', requireAuth, (req, res) => {
    try {
      const { name } = req.body;

      if (!name || typeof name !== 'string' || name.trim().length === 0) {
        return res.status(400).json({
          success: false,
          error: '请提供有效的服务器名称',
        });
      }

      const serverName = name.trim();

      // 检查是否已存在同名主机
      const allServers = serverStorage.getAll();
      const existing = allServers.find(s => s.name === serverName);

      let server;
      let isNew = false;

      if (existing) {
        // 使用已存在的主机
        server = existing;
      } else {
        // 创建新的主机记录 (Agent 模式不需要 SSH 凭据)
        server = serverStorage.create({
          name: serverName,
          host: '', // Agent 连接后由主机信息自动填充
          port: 22,
          username: 'agent', // 占位符，Agent 模式不需要真正的 SSH 用户
          password: '',
          auth_type: 'password', // 数据库约束只允许 password/key
          status: 'offline',
          monitor_mode: 'agent', // 通过此字段标记为 Agent 模式
          tags: ['Agent'], // 自动标记
          notes: `通过快速安装 API 创建于 ${new Date().toLocaleString('zh-CN')}`,
        });
        isNew = true;
        logger.info(`[Quick Install] 已创建新主机: ${serverName} (ID: ${server.id})`);
      }

      // 生成安装命令 (优先使用 用户设置 > API_PUBLIC_URL > 请求检测)
      const settings = loadUserSettings();
      let serverUrl = settings.publicApiUrl;

      if (!serverUrl || serverUrl.trim() === '') {
        serverUrl = process.env.API_PUBLIC_URL;
      }

      if (!serverUrl || serverUrl.trim() === '') {
        const protocol = req.protocol;
        const host = req.get('host');
        serverUrl = `${protocol}://${host}`;
      }

      const agentKey = agentService.getAgentKey(server.id);

      const linuxInstallUrl = `${serverUrl}/api/server/agent/install/linux/${server.id}/${agentKey}`;
      const winInstallUrl = `${serverUrl}/api/server/agent/install/win/${server.id}/${agentKey}`;

      res.json({
        success: true,
        data: {
          serverId: server.id,
          serverName: server.name,
          isNew,
          installCommand: `curl -fsSL "${linuxInstallUrl}" | sudo bash`,
          winInstallCommand: `powershell -c "irm ${winInstallUrl} | iex"`,
          apiUrl: serverUrl,
          // 也提供环境变量方式安装（适用于 Docker 等场景）
          envInstall: {
            command: `API_MONITOR_SERVER=${serverUrl} API_MONITOR_SERVER_ID=${server.id} API_MONITOR_KEY=${agentKey} node index.js`,
            serverUrl,
            serverId: server.id,
            agentKey,
          },
          message: isNew
            ? `已创建新主机 "${serverName}"，请在目标服务器执行安装命令`
            : `主机 "${serverName}" 已存在，请在目标服务器执行安装命令`,
        },
      });
    } catch (error) {
      logger.error('[Quick Install] 错误:', error.message);
      res.status(500).json({ success: false, error: error.message });
    }
  });

  /**
   * GET /api/server/agent/quick-install/:name
   *
   * 直接返回安装脚本 (可通过 curl | bash 直接执行)
   * 自动创建主机记录（如果不存在）
   */
  agentPublicRouter.get('/quick-install/:name', (req, res) => {
    try {
      const { name } = req.params;
      const serverName = decodeURIComponent(name).trim();

      if (!serverName) {
        return res.status(400).send('# Error: 请提供服务器名称');
      }

      // 查找或创建主机
      const allServers = serverStorage.getAll();
      let server = allServers.find(s => s.name === serverName);

      if (!server) {
        server = serverStorage.create({
          name: serverName,
          host: '',
          port: 22,
          username: 'agent',
          password: '',
          auth_type: 'password', // 数据库约束只允许 password/key
          status: 'offline',
          monitor_mode: 'agent', // 通过此字段标记为 Agent 模式
          tags: ['Agent'],
          notes: `通过一键安装创建于 ${new Date().toLocaleString('zh-CN')}`,
        });
        console.log(`[Quick Install] 自动创建主机: ${serverName} (ID: ${server.id})`);
      }

      // 生成安装脚本
      const protocol = req.protocol;
      const host = req.get('host');
      const serverUrl = `${protocol}://${host}`;
      const script = agentService.generateInstallScript(server.id, serverUrl);

      res.setHeader('Content-Type', 'text/plain; charset=utf-8');
      res.send(script);
    } catch (error) {
      res.status(500).send(`# Error: ${error.message}`);
    }
  });

  // --- 以下为需要认证的 Agent 管理接口 ---

  // 获取 Agent 安装命令 (前端弹窗使用)
  agentPublicRouter.get('/command/:serverId', requireAuth, (req, res) => {
    try {
      const { serverId } = req.params;
      const server = serverStorage.getById(serverId);
      if (!server) return res.status(404).json({ success: false, error: '主机不存在' });

      const protocol = req.protocol;
      const host = req.get('host');
      const serverUrl = `${protocol}://${host}`;
      const installUrl = `${serverUrl}/api/server/agent/install/${serverId}`;

      res.json({
        success: true,
        data: {
          serverId,
          serverName: server.name,
          installCommand: `curl -fsSL ${installUrl} | sudo bash`,
          winInstallCommand: `powershell -c "irm ${serverUrl}/api/server/agent/install/win/${serverId} | iex"`,
          apiUrl: serverUrl,
          agentKey: agentService.getAgentKey(serverId),
        },
      });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 重新生成全局 Agent 密钥
  agentPublicRouter.post('/regenerate-key', requireAuth, (req, res) => {
    try {
      const newKey = agentService.regenerateGlobalKey();
      res.json({ success: true, key: newKey });
    } catch (error) {
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 自动安装 Agent (通过 SSH)
  agentPublicRouter.post('/auto-install/:serverId', requireAuth, async (req, res) => {
    try {
      const { serverId } = req.params;
      const server = serverStorage.getById(serverId);
      const sshService = require('../../modules/server-api/ssh-service');

      if (!server) return res.status(404).json({ success: false, error: '主机不存在' });

      // 策略更新：如果 Agent 在线且未强制 SSH，优先发送升级指令
      const forceSsh = req.body.force_ssh === true;
      if (agentService.isAgentOnline(serverId) && !forceSsh) {
        logger.info(`[Auto-Install] 检测到 Agent 在线: ${server.name}，发送升级指令`);
        const sent = agentService.sendUpgradeTask(serverId);
        if (sent) {
          return res.json({
            success: true,
            message: 'Agent 升级指令已下发（后台执行）',
            output: '正在通过现有的 Agent 连接执行版本更新...'
          });
        }
        logger.warn(`[Auto-Install] Agent 升级指令发送失败，回退到 SSH 模式`);
      }

      // 如果 Agent 不在线或发送失败，尝试 SSH 安装
      logger.info(`[Auto-Install] 开始安装 Agent (SSH): ${server.name} (${serverId})`);

      const protocol = req.protocol;
      const host = req.get('host');
      const serverUrl = `${protocol}://${host}`;
      const script = agentService.generateInstallScript(serverId, serverUrl);

      // 120秒超时，下载+安装需要时间
      const result = await sshService.executeCommand(
        serverId,
        server,
        `cat << 'EOF' > /tmp/agent_install.sh\n${script}\nEOF\nsudo bash /tmp/agent_install.sh`,
        120000
      );

      logger.info(`[Auto-Install] 执行结果: success=${result.success}, code=${result.code}`);
      if (result.stdout) logger.info(`[Auto-Install] stdout: ${result.stdout.substring(0, 500)}`);

      const errorDetails = result.error || result.stderr || '未知错误';
      if (!result.success) {
        logger.warn(`[Auto-Install] 失败详情: ${errorDetails}`);
      }

      if (result.success) {
        serverStorage.updateStatus(serverId, { status: 'online' });
        res.json({ success: true, message: 'Agent 安装命令已执行', output: result.stdout });
      } else {
        res.status(500).json({
          success: false,
          error: '安装执行失败',
          details: errorDetails,
          output: result.stdout,
          code: result.code,
        });
      }
    } catch (error) {
      logger.error(`[Auto-Install] 异常: ${error.message}`);
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 卸载 Agent (通过 SSH)
  agentPublicRouter.post('/uninstall/:serverId', requireAuth, async (req, res) => {
    try {
      const { serverId } = req.params;
      const server = serverStorage.getById(serverId);
      const sshService = require('../../modules/server-api/ssh-service');

      if (!server) return res.status(404).json({ success: false, error: '主机不存在' });

      logger.info(`[Uninstall] 开始卸载 Agent (SSH): ${server.name} (${serverId})`);

      const script = agentService.generateUninstallScript();

      try {
        const result = await sshService.executeCommand(
          serverId,
          server,
          `cat << 'EOF' > /tmp/agent_uninstall.sh\n${script}\nEOF\nsudo bash /tmp/agent_uninstall.sh`
        );

        logger.info(`[Uninstall] SSH 执行结果: success=${result.success}`);

        if (result.success) {
          res.json({ success: true, message: 'Agent 卸载命令已执行' });
        } else {
          const errDetail = result.stderr || result.error || '未知错误';
          logger.warn(`[Uninstall] 执行失败详情: ${errDetail}`);
          res.status(500).json({ success: false, error: '卸载执行失败', details: errDetail });
        }
      } catch (sshErr) {
        logger.error(`[Uninstall] SSH 调用异常: ${sshErr.message}`, sshErr);
        res.status(500).json({ success: false, error: `SSH 调用异常: ${sshErr.message}` });
      }
    } catch (error) {
      logger.error(`[Uninstall] 路由异常: ${error.message}`, error);
      res.status(500).json({ success: false, error: error.message });
    }
  });

  // 获取 Agent 连接详情 (用于精确判定上线状态)
  agentPublicRouter.get('/connection-info/:serverId', requireAuth, (req, res) => {
    const info = agentService.getAgentConnectionInfo(req.params.serverId);
    if (info) {
      res.json({ success: true, status: 'online', ...info });
    } else {
      res.json({ success: true, status: 'offline' });
    }
  });

  app.use('/api/server/agent', agentPublicRouter);
  logger.info('Agent 公开接口已挂载 -> /api/server/agent');

  // 4. 动态加载功能模块路由
  const modulesDir = path.join(__dirname, '../../modules');

  // 模块路由映射配置 (精准匹配目录名)
  const moduleRouteMap = {
    'zeabur-api': '/api/zeabur',
    'koyeb-api': '/api/koyeb',
    'cloudflare-api': '/api/cloudflare',
    'flyio-api': '/api/flyio',
    'aliyun-api': '/api/aliyun',
    'openai-api': '/api/openai',
    'openlist-api': '/api/openlist',
    'server-api': '/api/server',
    'antigravity-api': '/api/antigravity',
    'gemini-cli-api': '/api/gemini-cli',
    'ai-chat-api': '/api/ai-chat',
    'totp-api': '/api/totp',
    'uptime-api': '/api/uptime',
    'cron-api': '/api/cron',
    // music-api 在下方单独挂载（无需认证）
  };

  if (fs.existsSync(modulesDir)) {
    const modules = fs
      .readdirSync(modulesDir, { withFileTypes: true })
      .filter(dirent => dirent.isDirectory() && !dirent.name.startsWith('_'))
      .map(dirent => dirent.name);

    modules.forEach(moduleName => {
      const routerPath = path.join(modulesDir, moduleName, 'router.js');

      if (fs.existsSync(routerPath)) {
        try {
          const moduleRouter = require(routerPath);
          const routePath = moduleRouteMap[moduleName] || `/api/${moduleName.replace('-api', '')}`;

          // 根据模块特性决定是否应用认证中间件
          if (moduleName === 'antigravity-api' || moduleName === 'gemini-cli-api') {
            app.use(routePath, moduleRouter);
          } else {
            // 模块路由优先挂载
            app.use(routePath, requireAuth, moduleRouter);
          }
          logger.success(`模块已挂载 -> ${moduleName} [${routePath}]`);
        } catch (e) {
          logger.error(`模块加载失败: ${moduleName}`, e.message);
        }
      }
    });
  }

  // 5. 音乐模块路由 (代理 NCM API) - 无需认证，允许公开访问
  try {
    const musicRouter = require('../../modules/music-api/router');
    app.use('/api/music', musicRouter);
    logger.success('模块已挂载 -> music-api [/api/music] (public)');
  } catch (e) {
    logger.warn('音乐模块加载失败 (可选模块):', e.message);
  }

  // 6. 核心认证路由兼容旧版 (放在最后作为兜底，防止拦截模块路由)
  app.use('/api', authRouter);
}

module.exports = {
  registerRoutes,
};
