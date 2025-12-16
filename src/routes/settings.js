/**
 * 用户设置路由
 */

const express = require('express');
const router = express.Router();
const {
  loadUserSettings,
  saveUserSettings,
  updateUserSettings
} = require('../services/userSettings');

/**
 * 获取用户设置
 * GET /api/settings
 */
router.get('/settings', (req, res) => {
  try {
    const settings = loadUserSettings();
    res.json({
      success: true,
      data: settings
    });
  } catch (error) {
    res.status(500).json({
      success: false,
      error: error.message
    });
  }
});

/**
 * 保存用户设置（完整替换）
 * POST /api/settings
 */
router.post('/settings', (req, res) => {
  try {
    const settings = req.body;
    const result = saveUserSettings(settings);

    if (result.success) {
      res.json({
        success: true,
        message: '设置已保存'
      });
    } else {
      res.status(500).json({
        success: false,
        error: result.error
      });
    }
  } catch (error) {
    res.status(500).json({
      success: false,
      error: error.message
    });
  }
});

/**
 * 更新用户设置（部分更新）
 * PATCH /api/settings
 */
router.patch('/settings', (req, res) => {
  try {
    const partialSettings = req.body;
    const result = updateUserSettings(partialSettings);

    if (result.success) {
      res.json({
        success: true,
        message: '设置已更新'
      });
    } else {
      res.status(500).json({
        success: false,
        error: result.error
      });
    }
  } catch (error) {
    res.status(500).json({
      success: false,
      error: error.message
    });
  }
});

module.exports = router;
