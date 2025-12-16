require('dotenv').config();
const express = require('express');
const path = require('path');
const fs = require('fs');

// 导入日志工具
const { createLogger } = require('./src/utils/logger');
const logger = createLogger('Server');

// 导入中间件
const corsMiddleware = require('./src/middleware/cors');

// 导入服务
const { loadSessions } = require('./src/services/session');
const {
  loadAdminPassword,
  isPasswordSavedToFile,
  loadServerAccounts,
  getEnvAccounts
} = require('./src/services/config');

// 导入路由
const { registerRoutes } = require('./src/routes');

const app = express();
const PORT = process.env.PORT || 3000;

// 应用中间件
app.use(corsMiddleware);
app.use(express.json());
app.use(express.static('public'));

// 注册所有路由
registerRoutes(app);

// Favicon 处理
app.get('/favicon.ico', (req, res) => {
  const faviconPath = path.join(__dirname, 'public', 'logo.png');
  if (fs.existsSync(faviconPath)) {
    return res.sendFile(faviconPath);
  }
  return res.sendStatus(204);
});

// 加载持久化 session
loadSessions();

// 启动服务器
app.listen(PORT, '0.0.0.0', () => {
  logger.success(`服务器启动成功 - http://0.0.0.0:${PORT}`);

  // 检查密码配置
  if (process.env.ADMIN_PASSWORD) {
    logger.info('管理员密码: 环境变量');
  } else if (isPasswordSavedToFile()) {
    logger.info('管理员密码: 文件存储');
  } else {
    logger.warn('未设置管理员密码，首次访问时需设置');
  }

  const envAccounts = getEnvAccounts();
  const serverAccounts = loadServerAccounts();
  const totalAccounts = envAccounts.length + serverAccounts.length;

  if (totalAccounts > 0) {
    logger.group(`已加载 ${totalAccounts} 个Zeabur账号`);
    if (envAccounts.length > 0) {
      envAccounts.forEach(acc => logger.groupItem(`${acc.name} (环境变量)`));
    }
    if (serverAccounts.length > 0) {
      serverAccounts.forEach(acc => logger.groupItem(`${acc.name} (数据库)`));
    }
  } else {
    logger.info('准备就绪，等待添加账号');
  }
});
