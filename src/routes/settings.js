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
  updateUserSettings,
} = require('../services/userSettings');
const dbService = require('../db/database');
const { SystemConfig, OperationLog } = require('../db/models');
const { createLogger, getBuffer } = require('../utils/logger');

const logger = createLogger('Settings');

/**
 * 获取系统审计日志
 * GET /api/settings/operation-logs
 */
router.get('/operation-logs', (req, res) => {
  try {
    const logs = OperationLog.getLogs(null, 100);
    res.json({
      success: true,
      data: logs,
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 获取系统运行内存日志 (最近 200 条)
 * GET /api/settings/sys-logs
 */
router.get('/sys-logs', (req, res) => {
  try {
    const { getBuffer, LOG_FILE, getLogFileInfo } = require('../utils/logger');
    let buffer = getBuffer();

    // 如果内存 Buffer 为空，尝试从物理文件兜底读取最近 50 条
    if (!buffer || buffer.length === 0) {
      if (fs.existsSync(LOG_FILE)) {
        const content = fs.readFileSync(LOG_FILE, 'utf8');
        const lines = content.trim().split('\n').slice(-50);
        buffer = lines.map(line => {
          try {
            return JSON.parse(line);
          } catch (e) {
            return { message: line, level: 'INFO', timestamp: new Date().toISOString() };
          }
        });
      }
    }

    const formattedLogs = (buffer || []).map(entry => ({
      time: entry.timestamp
        ? new Date(entry.timestamp).toLocaleTimeString('zh-CN', { hour12: false })
        : '00:00:00',
      level: entry.level || 'INFO',
      module: entry.module || 'core',
      message: entry.message + (entry.data ? ' [DATA]' : ''),
    }));

    // 获取详细日志文件信息
    const fileInfo = getLogFileInfo();

    res.json({
      success: true,
      data: formattedLogs,
      fileSize: `${fileInfo.sizeMB} MB`,
      fileInfo: fileInfo,
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 读取原始 app.log 文件内容
 * GET /api/settings/app-log-file
 */
router.get('/app-log-file', (req, res) => {
  try {
    const logPath = path.join(process.cwd(), 'data/logs/app.log');
    if (!fs.existsSync(logPath)) {
      return res.json({ success: true, data: 'Log file not found at: ' + logPath });
    }

    // 使用 readFileSync + slice 读取末尾内容，避免 open/read 权限冲突
    const content = fs.readFileSync(logPath, 'utf8');
    const lines = content.split('\n');
    const lastLines = lines.slice(-500).join('\n'); // 读取最后 500 行

    res.json({
      success: true,
      data: lastLines,
      size: (fs.statSync(logPath).size / 1024).toFixed(2) + ' KB',
    });
  } catch (error) {
    logger.error('读取日志文件失败:', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 获取用户设置
 * GET /api/settings
 */
router.get('/', (req, res) => {
  try {
    const settings = loadUserSettings();
    res.json({
      success: true,
      data: settings,
    });
  } catch (error) {
    res.status(500).json({
      success: false,
      error: error.message,
    });
  }
});

/**
 * 保存用户设置（完整替换）
 * POST /api/settings
 */
router.post('/', (req, res) => {
  try {
    const settings = req.body;

    // 保存完整的 JSON 配置 (包含 channelModelPrefix 等)
    const result = saveUserSettings(settings);

    if (result.success) {
      res.json({
        success: true,
        message: '设置已保存',
      });
    } else {
      res.status(500).json({
        success: false,
        error: result.error,
      });
    }
  } catch (error) {
    res.status(500).json({
      success: false,
      error: error.message,
    });
  }
});

/**
 * 更新用户设置（部分更新）
 * PATCH /api/settings
 */
router.patch('/', (req, res) => {
  try {
    const partialSettings = req.body;
    const result = updateUserSettings(partialSettings);

    if (result.success) {
      res.json({
        success: true,
        message: '设置已更新',
      });
    } else {
      res.status(500).json({
        success: false,
        error: result.error,
      });
    }
  } catch (error) {
    res.status(500).json({
      success: false,
      error: error.message,
    });
  }
});

/**
 * 导出数据库文件
 * GET /api/settings/export-database
 */
router.get('/export-database', async (req, res) => {
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
    res.download(backupPath, backupFileName, err => {
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
      error: '数据库导出失败: ' + error.message,
    });
  }
});

/**
 * 导入数据库文件
 * POST /api/settings/import-database
 */
router.post('/import-database', async (req, res) => {
  try {
    logger.info('开始导入数据库');

    // 检查是否有上传的文件
    if (!req.files || !req.files.database) {
      return res.status(400).json({
        success: false,
        error: '未找到上传的数据库文件',
      });
    }

    const uploadedFile = req.files.database;

    // 验证文件类型
    if (!uploadedFile.name.endsWith('.db')) {
      return res.status(400).json({
        success: false,
        error: '无效的文件类型，请上传 .db 文件',
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
    const dbPath = path.join(__dirname, '../../data/data.db');

    // 显式清理可能残留的 WAL 临时文件，防止文件锁定冲突
    try {
      if (fs.existsSync(dbPath + '-shm')) fs.unlinkSync(dbPath + '-shm');
      if (fs.existsSync(dbPath + '-wal')) fs.unlinkSync(dbPath + '-wal');
    } catch (e) {
      logger.warn('清理临时数据库文件失败:', e.message);
    }

    await uploadedFile.mv(dbPath);

    // 重新初始化数据库连接
    dbService.initialize();

    logger.success('数据库导入成功');

    res.json({
      success: true,
      message: '数据库导入成功，原数据库已备份',
      backupPath: currentBackupPath,
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
      error: '数据库导入失败: ' + error.message,
    });
  }
});

/**
 * 获取数据库统计信息
 * GET /api/settings/database-stats
 */
router.get('/database-stats', (req, res) => {
  try {
    const stats = dbService.getStats();
    res.json({
      success: true,
      data: stats,
    });
  } catch (error) {
    logger.error('获取数据库统计失败', error.message);
    res.status(500).json({
      success: false,
      error: error.message,
    });
  }
});

/**
 * 压缩数据库 (VACUUM)
 * POST /api/settings/vacuum-database
 */
router.post('/vacuum-database', async (req, res) => {
  try {
    logger.info('收到数据库压缩请求');
    dbService.vacuum();
    res.json({
      success: true,
      message: '数据库压缩整理完成',
    });
  } catch (error) {
    logger.error('数据库压缩请求失败', error.message);
    res.status(500).json({
      success: false,
      error: '数据库压缩失败: ' + error.message,
    });
  }
});

/**
 * 清理日志
 * POST /api/settings/clear-logs
 */
router.post('/clear-logs', async (req, res) => {
  try {
    logger.info('收到清理日志请求');
    const count = dbService.clearLogs();
    res.json({
      success: true,
      message: `日志清理完成，共移除 ${count} 条记录`,
      count,
    });
  } catch (error) {
    logger.error('日志清理请求失败', error.message);
    res.status(500).json({
      success: false,
      error: '日志清理失败: ' + error.message,
    });
  }
});

/**
 * 物理清空 app.log 文件
 * POST /api/settings/clear-app-logs
 */
router.post('/clear-app-logs', (req, res) => {
  try {
    const { clearLogFile } = require('../utils/logger');
    const success = clearLogFile();
    if (success) {
      logger.success('系统日志文件已物理清空');
    }

    res.json({ success: true, message: 'Logs cleared' });
  } catch (error) {
    logger.error('清空日志文件失败:', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

module.exports = router;

/**
 * 获取日志保留设置
 * GET /api/settings/log-settings
 */
router.get('/log-settings', (req, res) => {
  try {
    const { getLogConfig, getLogFileInfo } = require('../utils/logger');

    const days = SystemConfig.getConfigValue('log_retention_days', 0);
    const count = SystemConfig.getConfigValue('log_max_count', 0);
    const dbSizeMB = SystemConfig.getConfigValue('log_max_db_size_mb', 0);
    const logFileSizeMB = SystemConfig.getConfigValue('log_file_max_size_mb', 10);

    // 同步配置到 logger 模块
    const logConfig = getLogConfig();
    const fileInfo = getLogFileInfo();

    res.json({
      success: true,
      data: {
        days: parseInt(days) || 0,
        count: parseInt(count) || 0,
        dbSizeMB: parseInt(dbSizeMB) || 0,
        logFileSizeMB: parseInt(logFileSizeMB) || 10,
      },
      logConfig: logConfig,
      fileInfo: fileInfo,
    });
  } catch (error) {
    logger.error('获取日志设置失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 保存日志保留设置
 * POST /api/settings/log-settings
 */
router.post('/log-settings', (req, res) => {
  try {
    const { days, count, dbSizeMB, logFileSizeMB } = req.body;
    const { updateLogConfig, getLogFileInfo } = require('../utils/logger');

    SystemConfig.setConfig('log_retention_days', days || 0, '日志保留天数');
    SystemConfig.setConfig('log_max_count', count || 0, '单表最大日志数');
    SystemConfig.setConfig('log_max_db_size_mb', dbSizeMB || 0, '数据库最大大小(MB)');

    // 保存并同步日志文件大小设置到运行时
    if (logFileSizeMB !== undefined) {
      const sizeMB = Math.max(1, parseInt(logFileSizeMB) || 10);
      SystemConfig.setConfig('log_file_max_size_mb', sizeMB, '日志文件最大大小(MB)');
      updateLogConfig({ maxFileSizeMB: sizeMB });
    }

    res.json({
      success: true,
      message: '日志设置已保存',
      fileInfo: getLogFileInfo(),
    });
  } catch (error) {
    logger.error('保存日志设置失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 强制执行日志清理
 * POST /api/settings/enforce-log-limits
 */
router.post('/enforce-log-limits', async (req, res) => {
  try {
    // 优先使用传入的参数，如果没有则读取已保存的配置
    let { days, count, dbSizeMB } = req.body;

    if (days === undefined || count === undefined || dbSizeMB === undefined) {
      days = parseInt(SystemConfig.getConfigValue('log_retention_days', 0)) || 0;
      count = parseInt(SystemConfig.getConfigValue('log_max_count', 0)) || 0;
      dbSizeMB = parseInt(SystemConfig.getConfigValue('log_max_db_size_mb', 0)) || 0;
    }

    const result = dbService.enforceLogLimits({
      days: parseInt(days),
      count: parseInt(count),
      dbSizeMB: parseInt(dbSizeMB),
    });

    res.json({
      success: true,
      message: `日志清理完成，共移除 ${result.deleted} 条记录`,
      data: result,
    });
  } catch (error) {
    logger.error('强制日志清理失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});
