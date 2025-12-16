/**
 * 路由汇总
 */

const express = require('express');
const { requireAuth } = require('../middleware/auth');

// 导入各个路由模块
const authRouter = require('./auth');
const healthRouter = require('./health');

/**
 * 注册所有路由
 */
function registerRoutes(app) {
  // 健康检查（不需要认证）
  app.use('/health', healthRouter);

  // 认证相关路由
  app.use('/api', authRouter);

  // Zeabur API 管理模块
  try {
    const zeaburRouter = require('../../modules/zeabur-api/router');
    app.use('/api', requireAuth, zeaburRouter);
    console.log('✅ Zeabur API 模块已加载');
  } catch (e) {
    console.log('⚠️ Zeabur API 模块未加载:', e.message);
  }

  // Cloudflare DNS 管理模块
  try {
    const cfDnsRouter = require('../../modules/cloudflare-dns/router');
    app.use('/api/cf-dns', requireAuth, cfDnsRouter);
    console.log('✅ Cloudflare DNS 模块已加载');
  } catch (e) {
    console.log('⚠️ Cloudflare DNS 模块未加载:', e.message);
  }

  // OpenAI API 管理模块
  try {
    const openaiRouter = require('../../modules/openai-api/router');
    app.use('/api/openai', requireAuth, openaiRouter);
    console.log('✅ OpenAI API 模块已加载');
  } catch (e) {
    console.log('⚠️ OpenAI API 模块未加载:', e.message);
  }
}

module.exports = {
  registerRoutes
};
