/**
 * 系统日志服务
 * 处理日志查询 API 和 WebSocket 实时推送
 */

const { WebSocketServer } = require('ws');
const express = require('express');
const { logEmitter, getBuffer, createLogger } = require('../utils/logger');
const { requireAuth } = require('../middleware/auth');
const fs = require('fs');
const path = require('path');

const logger = createLogger('Log');
const router = express.Router();

/**
 * API: 获取历史日志（从内存缓冲区）
 */
router.get('/recent', requireAuth, (req, res) => {
  res.json({
    success: true,
    data: getBuffer(),
  });
});

/**
 * API: 获取完整日志文件（可选）
 */
router.get('/full', requireAuth, (req, res) => {
  const logFile = path.join(process.cwd(), 'data', 'logs', 'app.log');
  if (!fs.existsSync(logFile)) {
    return res.json({ success: true, data: [] });
  }

  try {
    const content = fs.readFileSync(logFile, 'utf8');
    const logs = content
      .trim()
      .split('\n')
      .map(line => {
        try {
          return JSON.parse(line);
        } catch (e) {
          return { message: line, level: 'INFO', timestamp: new Date().toISOString() };
        }
      });
    res.json({ success: true, data: logs });
  } catch (error) {
    res.status(500).json({ success: false, message: error.message });
  }
});

/**
 * 初始化 WebSocket 服务
 * @param {http.Server} server - HTTP 服务器实例
 */
function init(server) {
  const wss = new WebSocketServer({
    noServer: true,
    perMessageDeflate: false,
  });

  handleConnection(wss);

  // 返回 wss 实例以便在 server.js 中处理升级
  return wss;
}

/**
 * 启动连接处理逻辑（由外部调用）
 */
function handleConnection(wss) {
  wss.on('connection', (ws, req) => {
    logger.info(`日志 WebSocket 客户端已连接 (来自 ${req.socket.remoteAddress})`);

    // 发送当前缓冲区中的日志
    try {
      const buffer = getBuffer();
      ws.send(
        JSON.stringify({
          type: 'init',
          data: buffer,
        })
      );
    } catch (err) {
      logger.error('发送初始日志失败:', err.message);
    }

    // 监听新日志
    const logHandler = logEntry => {
      if (ws.readyState === ws.OPEN) {
        try {
          ws.send(
            JSON.stringify({
              type: 'log',
              data: logEntry,
            })
          );
        } catch (err) {
          // 发送失败，忽略
        }
      }
    };

    logEmitter.on('log', logHandler);

    ws.on('close', (code, reason) => {
      logger.info(`日志 WebSocket 客户端已断开 (code: ${code})`);
      logEmitter.off('log', logHandler);
    });

    ws.on('error', error => {
      logger.error('WebSocket 错误:', error.message);
      logEmitter.off('log', logHandler);
    });
  });

  wss.on('error', error => {
    logger.error('WebSocket 服务器错误:', error.message);
  });

  logger.success('日志 WebSocket 服务已就绪: /ws/logs');
}

module.exports = {
  router,
  init,
  handleConnection,
};
