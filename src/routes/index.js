/**
 * 路由汇总
 */

const express = require('express');
const { requireAuth } = require('../middleware/auth');

// 导入各个路由模块
const authRouter = require('./auth');
const healthRouter = require('./health');
const settingsRouter = require('./settings');
const logService = require('../services/log-service');

// 导入聚合的 v1 路由
const v1Router = require('./v1');
const { createLogger } = require('../utils/logger');

const logger = createLogger('Router');

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

  // 系统日志路由
  app.use('/api/logs', logService.router);

  // 挂载聚合的 OpenAI 兼容接口
  app.use('/v1', v1Router);

  // 动态加载模块路由
  const modulesDir = path.join(__dirname, '../../modules');

  // 模块路由映射配置 (保持向后兼容)
  const moduleRouteMap = {
    'zeabur-api': '/api', // 注意：Zeabur 模块内部路由可能以 /zeabur 开头，或者直接挂载在 /api 下
    'koyeb-api': '/api', // Koyeb 模块 - 内部路由以 /koyeb 开头
    'cloudflare-dns': '/api/cf-dns',
    'openai-api': '/api/openai',
    'server-management': '/api/server',
    'antigravity-api': '/api/antigravity',
    'gemini-cli-api': '/api/gemini-cli-api'
  };

  if (fs.existsSync(modulesDir)) {
    const modules = fs.readdirSync(modulesDir, { withFileTypes: true })
      .filter(dirent => dirent.isDirectory() && !dirent.name.startsWith('_'))
      .map(dirent => dirent.name);

    modules.forEach(moduleName => {
      const routerPath = path.join(modulesDir, moduleName, 'router.js');

      if (fs.existsSync(routerPath)) {
        try {
          const moduleRouter = require(routerPath);
          // 使用配置的路径，如果未配置则默认使用 /api/${moduleName}
          const routePath = moduleRouteMap[moduleName] || `/api/${moduleName}`;

          if (moduleName === 'antigravity-api' || moduleName === 'gemini-cli-api') {
            // 这些模块需要自定义认证逻辑（API Key 支持），挂载到各自的 API 路径
            app.use(routePath, moduleRouter);
            // 注意：/v1 路径现在由 v1Router 统一接管，不再在此处单独挂载
          } else {
            app.use(routePath, requireAuth, moduleRouter);
          }
          logger.success(`模块已挂载 -> ${moduleName}`);
        } catch (e) {
          logger.error(`模块加载失败: ${moduleName}`, e);
        }
      }
    });
  }
}

module.exports = {
  registerRoutes
};
