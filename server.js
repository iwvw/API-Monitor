require('dotenv').config();
const express = require('express');
const path = require('path');
const fs = require('fs');

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
  console.log(`✨ Zeabur Monitor 运行在 http://0.0.0.0:${PORT}`);

  // 检查密码配置
  if (process.env.ADMIN_PASSWORD) {
    console.log(`🔐 已通过环境变量 ADMIN_PASSWORD 设置管理员密码`);
  } else if (isPasswordSavedToFile()) {
    console.log(`🔐 管理员密码已保存到文件`);
  } else {
    console.log(`⚠️ 未设置管理员密码，首次访问时请设置`);
  }

  const envAccounts = getEnvAccounts();
  const serverAccounts = loadServerAccounts();
  const totalAccounts = envAccounts.length + serverAccounts.length;

  if (totalAccounts > 0) {
    console.log(`📋 已加载 ${totalAccounts} 个账号`);
    if (envAccounts.length > 0) {
      console.log(`   环境变量: ${envAccounts.length} 个`);
      envAccounts.forEach(acc => console.log(`     - ${acc.name}`));
    }
    if (serverAccounts.length > 0) {
      console.log(`   服务器存储: ${serverAccounts.length} 个`);
      serverAccounts.forEach(acc => console.log(`     - ${acc.name}`));
    }
  } else {
    console.log(`📊 准备就绪，等待添加账号...`);
  }
});
