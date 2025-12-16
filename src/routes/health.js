/**
 * 健康检查路由
 */

const express = require('express');
const router = express.Router();

/**
 * 健康检查（不需要认证）
 */
router.get('/', (req, res) => {
  res.json({
    ok: true,
    timestamp: new Date().toISOString(),
    origin: req.headers.origin
  });
});

module.exports = router;
