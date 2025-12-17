/**
 * 用户设置路由
 */

const express = require('express');
const router = express.Router();
const path = require('path');
const fs = require('fs');
const {
  loadUserSettings,
  saveUserSettings,
  updateUserSettings
} = require('../services/userSettings');
const dbService = require('../db/database');
const { createLogger } = require('../utils/logger');

const logger = createLogger('Settings');

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

/**
 * 导出数据库文件
 * GET /api/settings/export-database
 */
router.get('/settings/export-database', async (req, res) => {
  try {
    logger.info('开始导出数据库');

    // 创建临时备份目录
    const backupDir = path.join(__dirname, '../../data/temp');
    if (!fs.existsSync(backupDir)) {
      fs.mkdirSync(backupDir, { recursive: true });
    }

    // 生成备份文件名
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
    const backupFileName = `api-monitor-backup-${timestamp}.db`;
    const backupPath = path.join(backupDir, backupFileName);

    // 执行数据库备份
    await dbService.backup(backupPath);

    // 发送文件
    res.download(backupPath, backupFileName, (err) => {
      // 下载完成后删除临时文件
      if (fs.existsSync(backupPath)) {
        fs.unlinkSync(backupPath);
      }

      if (err) {
        logger.error('数据库导出失败', err.message);
      } else {
        logger.success('数据库导出成功: ' + backupFileName);
      }
    });
  } catch (error) {
    logger.error('数据库导出失败', error.message);
    res.status(500).json({
      success: false,
      error: '数据库导出失败: ' + error.message
    });
  }
});

/**
 * 导入数据库文件
 * POST /api/settings/import-database
 */
router.post('/settings/import-database', async (req, res) => {
  try {
    logger.info('开始导入数据库');

    // 检查是否有上传的文件
    if (!req.files || !req.files.database) {
      return res.status(400).json({
        success: false,
        error: '未找到上传的数据库文件'
      });
    }

    const uploadedFile = req.files.database;

    // 验证文件类型
    if (!uploadedFile.name.endsWith('.db')) {
      return res.status(400).json({
        success: false,
        error: '无效的文件类型，请上传 .db 文件'
      });
    }

    // 备份当前数据库
    const backupDir = path.join(__dirname, '../../backup');
    if (!fs.existsSync(backupDir)) {
      fs.mkdirSync(backupDir, { recursive: true });
    }

    const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
    const currentBackupPath = path.join(backupDir, `api-monitor-before-import-${timestamp}.db`);

    // 备份当前数据库
    await dbService.backup(currentBackupPath);
    logger.info('当前数据库已备份: ' + currentBackupPath);

    // 关闭当前数据库连接
    dbService.close();

    // 替换数据库文件
    const dbPath = path.join(__dirname, '../../data/api-monitor.db');
    await uploadedFile.mv(dbPath);

    // 重新初始化数据库连接
    dbService.initialize();

    logger.success('数据库导入成功');

    res.json({
      success: true,
      message: '数据库导入成功，原数据库已备份',
      backupPath: currentBackupPath
    });
  } catch (error) {
    logger.error('数据库导入失败', error.message);

    // 尝试恢复数据库连接
    try {
      dbService.initialize();
    } catch (e) {
      logger.error('数据库连接恢复失败', e.message);
    }

    res.status(500).json({
      success: false,
      error: '数据库导入失败: ' + error.message
    });
  }
});

/**
 * 获取数据库统计信息
 * GET /api/settings/database-stats
 */
router.get('/settings/database-stats', (req, res) => {
  try {
    const stats = dbService.getStats();
    res.json({
      success: true,
      data: stats
    });
  } catch (error) {
    logger.error('获取数据库统计失败', error.message);
    res.status(500).json({
      success: false,
      error: error.message
    });
  }
});

module.exports = router;
