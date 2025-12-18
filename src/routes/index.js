/**
 * 路由汇总
 */

const express = require('express');
const { requireAuth } = require('../middleware/auth');

// 导入各个路由模块
const authRouter = require('./auth');
const healthRouter = require('./health');
const settingsRouter = require('./settings');

/**
 * 注册所有路由
 */
function registerRoutes(app) {
  const fs = require('fs');
  const path = require('path');

  // 健康检查（不需要认证）
  app.use('/health', healthRouter);

  // 认证相关路由
  app.use('/api', authRouter);

  // 用户设置路由（需要认证）- 挂载到精确路径避免拦截其他 API 请求
  app.use('/api/settings', requireAuth, settingsRouter);

  // 动态加载模块路由
  const modulesDir = path.join(__dirname, '../../modules');

  // 模块路由映射配置 (保持向后兼容)
  const moduleRouteMap = {
    'zeabur-api': '/api', // 注意：Zeabur 模块内部路由可能以 /zeabur 开头，或者直接挂载在 /api 下
    'cloudflare-dns': '/api/cf-dns',
    'openai-api': '/api/openai',
    'server-management': '/api/server',
    'antigravity-api': '/api/antigravity'
  };

  if (fs.existsSync(modulesDir)) {
    const modules = fs.readdirSync(modulesDir, { withFileTypes: true })
      .filter(dirent => dirent.isDirectory())
      .map(dirent => dirent.name);

    modules.forEach(moduleName => {
      const routerPath = path.join(modulesDir, moduleName, 'router.js');

      if (fs.existsSync(routerPath)) {
        try {
          const moduleRouter = require(routerPath);
          // 使用配置的路径，如果未配置则默认使用 /api/{moduleName}
          const routePath = moduleRouteMap[moduleName] || `/api/${moduleName}`;

          if (moduleName === 'antigravity-api') {
            // Antigravity 模块需要自定义认证逻辑（API Key 支持）
            app.use(routePath, moduleRouter);

            // 创建简洁的 /v1 端点（OpenAI 兼容格式）
            // 将 /v1/* 请求映射到模块内部的 /v1/* 路由
            const openaiRouter = express.Router();
            openaiRouter.get('/models', (req, res, next) => {
              req.url = '/v1/models';
              moduleRouter(req, res, next);
            });
            openaiRouter.post('/chat/completions', (req, res, next) => {
              req.url = '/v1/chat/completions';
              moduleRouter(req, res, next);
            });
            app.use('/v1', openaiRouter);
            console.log(`✅ 模块已加载: ${moduleName} -> /v1 (OpenAI 兼容端点)`);
          } else {
            app.use(routePath, requireAuth, moduleRouter);
          }
          console.log(`✅ 模块已加载: ${moduleName} -> ${routePath}`);
        } catch (e) {
          console.error(`⚠️ 模块加载失败: ${moduleName}`, e.message);
        }
      }
    });
  }
}

module.exports = {
  registerRoutes
};
