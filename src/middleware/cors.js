/**
 * CORS 中间件配置
 */

const cors = require('cors');

/**
 * CORS 配置
 */
const corsOptions = {
  origin: function(origin, callback) {
    // 开发环境：允许所有本地源
    if (!origin || origin.includes('localhost') || origin.includes('127.0.0.1') || origin.includes('0.0.0.0')) {
      return callback(null, true);
    }
    // 生产环境：可在此限制
    callback(null, true);
  },
  credentials: true,
  methods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS'],
  allowedHeaders: ['Content-Type', 'Authorization', 'x-admin-password']
};

module.exports = cors(corsOptions);
