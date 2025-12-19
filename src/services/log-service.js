/**
 * 系统日志服务
 * 处理日志查询 API 和 WebSocket 实时推送
 */

const { WebSocketServer } = require('ws');
const express = require('express');
const { logEmitter, getBuffer } = require('../utils/logger');
const { requireAuth } = require('../middleware/auth');
const fs = require('fs');
const path = require('path');

const router = express.Router();

/**
 * API: 获取历史日志（从内存缓冲区）
 */
router.get('/recent', requireAuth, (req, res) => {
  res.json({
    success: true,
    data: getBuffer()
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
    const logs = content.trim().split('\n').map(line => {
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
    server,
    path: '/ws/logs'
  });

  wss.on('connection', (ws) => {
    // 发送当前缓冲区中的日志
    const buffer = getBuffer();
    ws.send(JSON.stringify({
      type: 'init',
      data: buffer
    }));

    // 监听新日志
    const logHandler = (logEntry) => {
      if (ws.readyState === ws.OPEN) {
        ws.send(JSON.stringify({
          type: 'log',
          data: logEntry
        }));
      }
    };

    logEmitter.on('log', logHandler);

    ws.on('close', () => {
      logEmitter.off('log', logHandler);
    });
  });

  console.log('✅ 系统日志 WebSocket 服务已启动: /ws/logs');
}

module.exports = {
  router,
  init
};
