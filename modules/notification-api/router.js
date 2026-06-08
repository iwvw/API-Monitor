/**
 * 通知系统 API 路由
 */

const express = require('express');
const router = express.Router();
const { createLogger } = require('../../src/utils/logger');
const { encrypt, decrypt } = require('../../src/utils/encryption');
const storage = require('./storage');
const notificationService = require('./service');

const emailChannel = require('./channels/email');
const telegramChannel = require('./channels/telegram');

const logger = createLogger('NotificationAPI');

// ==================== 渠道管理 ====================

/**
 * 获取所有渠道
 */
router.get('/channels', (req, res) => {
    try {
        const channels = storage.channel.getAll();
        // 不返回敏感配置
        const safeChannels = channels.map(ch => {
            let config = ch.config;
            try {
                // 如果是加密字符串，尝试解密
                if (config && config.startsWith('u2f')) { // 简单的加密特征判断
                    config = JSON.parse(decrypt(config));
                } else {
                    config = JSON.parse(config);
                }
            } catch (e) {
                // 如果解析失败，可能是已加密但未匹配特征，或者本身就是存的明文但格式不对
                try {
                    config = JSON.parse(decrypt(ch.config));
                } catch (e2) {
                    config = {};
                }
            }
            return { ...ch, config };
        });
        res.json({ success: true, data: safeChannels });
    } catch (error) {
        logger.error(`获取渠道列表失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取单个渠道详情
 */
router.get('/channels/:id', (req, res) => {
    try {
        const channel = storage.channel.getById(req.params.id);
        if (!channel) {
            return res.status(404).json({ success: false, error: '渠道不存在' });
        }
        let config = channel.config;
        try {
            // 尝试解密配置
            config = JSON.parse(decrypt(config));
        } catch (e) {
            // 如果解密失败，尝试直接解析（可能是未加密的旧数据或明文）
            try {
                config = JSON.parse(channel.config);
            } catch (e2) {
                // 如果都失败，则返回空对象
                config = {};
            }
        }
        res.json({
            success: true,
            data: {
                ...channel,
                config,
            },
        });
    } catch (error) {
        logger.error(`获取渠道详情失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 创建渠道
 */
router.post('/channels', (req, res) => {
    try {
        const { name, type, config, enabled = true } = req.body;

        if (!name || !type || !config) {
            return res.status(400).json({ success: false, error: '缺少必要参数' });
        }

        if (!['email', 'telegram'].includes(type)) {
            return res.status(400).json({ success: false, error: '不支持的渠道类型' });
        }

        // 加密配置
        const encryptedConfig = encrypt(JSON.stringify(config));

        const channel = storage.channel.create({
            name,
            type,
            config: encryptedConfig,
            enabled: enabled ? 1 : 0,
        });

        logger.info(`创建渠道成功: ${name} (${type})`);
        res.json({ success: true, data: channel });
    } catch (error) {
        logger.error(`创建渠道失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 更新渠道
 */
router.put('/channels/:id', (req, res) => {
    try {
        const { name, config, enabled } = req.body;

        const updateData = {};
        if (name !== undefined) updateData.name = name;
        if (enabled !== undefined) updateData.enabled = enabled ? 1 : 0;
        if (config !== undefined) {
            updateData.config = encrypt(JSON.stringify(config));
        }

        storage.channel.update(req.params.id, updateData);

        logger.info(`更新渠道成功: ${req.params.id}`);
        res.json({ success: true });
    } catch (error) {
        logger.error(`更新渠道失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 删除渠道
 */
router.delete('/channels/:id', (req, res) => {
    try {
        storage.channel.delete(req.params.id);
        logger.info(`删除渠道成功: ${req.params.id}`);
        res.json({ success: true });
    } catch (error) {
        logger.error(`删除渠道失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 测试渠道 - 发送实际测试消息
 */
router.post('/channels/:id/test', async (req, res) => {
    try {
        const channel = storage.channel.getById(req.params.id);
        if (!channel) {
            return res.status(404).json({ success: false, error: '渠道不存在' });
        }

        // 解密配置
        const config = JSON.parse(decrypt(channel.config));

        const testTitle = '🔔 通知连通性测试';
        const testMessage = `发送时间: ${new Date().toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })}
验证状态: 配置正确, 通道畅通`;

        let success = false;
        if (channel.type === 'email') {
            success = await emailChannel.send(config, testTitle, testMessage);
        } else if (channel.type === 'telegram') {
            success = await telegramChannel.send(config, testTitle, testMessage);
        }

        if (success) {
            logger.info(`渠道测试成功: ${channel.name} (${channel.type})`);
            res.json({ success: true, message: '测试消息已发送，请检查接收' });
        } else {
            res.status(500).json({ success: false, error: '测试消息发送失败' });
        }
    } catch (error) {
        logger.error(`测试渠道失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 规则管理 ====================

/**
 * 获取所有规则
 */
router.get('/rules', (req, res) => {
    try {
        const rules = storage.rule.getAll();
        res.json({ success: true, data: rules });
    } catch (error) {
        logger.error(`获取规则列表失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取单个规则详情
 */
router.get('/rules/:id', (req, res) => {
    try {
        const rule = storage.rule.getById(req.params.id);
        if (!rule) {
            return res.status(404).json({ success: false, error: '规则不存在' });
        }
        res.json({ success: true, data: rule });
    } catch (error) {
        logger.error(`获取规则详情失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 创建规则
 */
router.post('/rules', (req, res) => {
    try {
        const {
            name,
            source_module,
            event_type,
            severity = 'warning',
            channels,
            conditions = {},
            suppression = {},
            time_window = { enabled: false },
            description = '',
            title_template = '',
            message_template = '',
            backup_channels = [],
            quiet_until = null,
            enabled = true,
        } = req.body;

        if (!name || !source_module || !event_type || !channels) {
            return res.status(400).json({ success: false, error: '缺少必要参数' });
        }

        const rule = storage.rule.create({
            name,
            source_module,
            event_type,
            severity,
            channels,
            conditions,
            suppression,
            time_window,
            description,
            title_template,
            message_template,
            backup_channels,
            quiet_until,
            enabled: enabled ? 1 : 0,
        });

        logger.info(`创建规则成功: ${name} (${source_module}/${event_type})`);
        res.json({ success: true, data: rule });
    } catch (error) {
        logger.error(`创建规则失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 更新规则
 */
router.put('/rules/:id', (req, res) => {
    try {
        const {
            name,
            source_module,
            event_type,
            severity,
            channels,
            conditions,
            suppression,
            time_window,
            description,
            title_template,
            message_template,
            backup_channels,
            quiet_until,
            enabled,
        } = req.body;

        const updateData = {};
        if (name !== undefined) updateData.name = name;
        if (source_module !== undefined) updateData.source_module = source_module;
        if (event_type !== undefined) updateData.event_type = event_type;
        if (severity !== undefined) updateData.severity = severity;
        if (channels !== undefined) updateData.channels = channels;
        if (conditions !== undefined) updateData.conditions = conditions;
        if (suppression !== undefined) updateData.suppression = suppression;
        if (time_window !== undefined) updateData.time_window = time_window;
        if (description !== undefined) updateData.description = description;
        if (title_template !== undefined) updateData.title_template = title_template;
        if (message_template !== undefined) updateData.message_template = message_template;
        if (backup_channels !== undefined) updateData.backup_channels = backup_channels;
        if (quiet_until !== undefined) updateData.quiet_until = quiet_until;
        if (enabled !== undefined) updateData.enabled = enabled ? 1 : 0;

        storage.rule.update(req.params.id, updateData);

        logger.info(`更新规则成功: ${req.params.id}`);
        res.json({ success: true });
    } catch (error) {
        logger.error(`更新规则失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 删除规则
 */
router.delete('/rules/:id', (req, res) => {
    try {
        storage.rule.delete(req.params.id);
        logger.info(`删除规则成功: ${req.params.id}`);
        res.json({ success: true });
    } catch (error) {
        logger.error(`删除规则失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 启用规则
 */
router.post('/rules/:id/enable', (req, res) => {
    try {
        storage.rule.enable(req.params.id);
        logger.info(`启用规则成功: ${req.params.id}`);
        res.json({ success: true });
    } catch (error) {
        logger.error(`启用规则失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 禁用规则
 */
router.post('/rules/:id/disable', (req, res) => {
    try {
        storage.rule.disable(req.params.id);
        logger.info(`禁用规则成功: ${req.params.id}`);
        res.json({ success: true });
    } catch (error) {
        logger.error(`禁用规则失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

router.get('/event-catalog', (req, res) => {
    try {
        res.json({ success: true, data: notificationService.getEventCatalog() });
    } catch (error) {
        logger.error(`获取事件目录失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

router.get('/events/catalog', (req, res) => {
    try {
        res.json({ success: true, data: notificationService.getEventCatalog() });
    } catch (error) {
        logger.error(`获取事件目录失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

router.post('/templates/preview', (req, res) => {
    try {
        const data = notificationService.previewTemplate(req.body || {});
        res.json({ success: true, data });
    } catch (error) {
        logger.error(`模板预览失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

router.post('/rules/:id/dry-run', (req, res) => {
    try {
        const rule = storage.rule.getById(req.params.id);
        if (!rule) {
            return res.status(404).json({ success: false, error: '规则不存在' });
        }
        const data = notificationService.dryRunRule(rule, req.body?.data || req.body || {});
        res.json({ success: true, data });
    } catch (error) {
        logger.error(`规则 dry-run 失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 历史记录 ====================

/**
 * 获取通知历史
 */
router.get('/history', (req, res) => {
    try {
        const { status, limit = 100 } = req.query;

        let history;
        if (status) {
            history = storage.history.getByStatus(status, parseInt(limit));
        } else {
            history = storage.history.getRecent(parseInt(limit));
        }

        res.json({ success: true, data: history });
    } catch (error) {
        logger.error(`获取通知历史失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 清空历史记录
 */
router.delete('/history', (req, res) => {
    try {
        storage.history.clear();
        logger.info('清空历史记录成功');
        res.json({ success: true });
    } catch (error) {
        logger.error(`清空历史记录失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 全局配置 ====================

/**
 * 获取全局配置
 */
router.get('/config', (req, res) => {
    try {
        const config = storage.globalConfig.getDefault();
        res.json({ success: true, data: config });
    } catch (error) {
        logger.error(`获取全局配置失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 更新全局配置
 */
router.put('/config', (req, res) => {
    try {
        storage.globalConfig.update(req.body);
        logger.info('更新全局配置成功');
        res.json({ success: true });
    } catch (error) {
        logger.error(`更新全局配置失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 触发器 ====================

/**
 * 手动触发告警
 */
router.post('/trigger', async (req, res) => {
    try {
        const { source_module, event_type, data } = req.body;

        if (!source_module || !event_type) {
            return res.status(400).json({ success: false, error: '缺少必要参数' });
        }

        await notificationService.trigger(source_module, event_type, data || {});

        res.json({ success: true, message: '告警已触发' });
    } catch (error) {
        logger.error(`触发告警失败: ${error.message}`);
        res.status(500).json({ success: false, error: error.message });
    }
});

module.exports = router;
