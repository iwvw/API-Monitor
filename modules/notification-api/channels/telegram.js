/**
 * Telegram 通知渠道
 */

const axios = require('axios');
const { createLogger } = require('../../../src/utils/logger');

const logger = createLogger('NotificationChannel:Telegram');

class TelegramChannel {
    constructor() {
        this.apiBase = 'https://api.telegram.org/bot';
    }

    /**
     * 发送消息
     * @param {Object} config - Telegram 配置 (已解密)
     * @param {string} title - 消息标题
     * @param {string} message - 消息内容
     * @param {Object} options - 额外选项
     * @returns {Promise<boolean>}
     */
    async send(config, title, message, options = {}) {
        try {
            const url = `${this.apiBase}${config.bot_token}/sendMessage`;

            const text = this.formatMessage(title, message);

            const response = await axios.post(url, {
                chat_id: config.chat_id,
                text: text,
                parse_mode: 'HTML',
                disable_web_page_preview: true,
                ...options,
            }, {
                timeout: 10000, // 10秒超时
            });

            if (response.data.ok) {
                logger.info(`Telegram 发送成功: chat_id=${config.chat_id}`);
                return true;
            } else {
                throw new Error(response.data.description || 'Unknown error');
            }
        } catch (error) {
            if (error.response) {
                logger.error(`Telegram 发送失败: ${error.response.status} - ${JSON.stringify(error.response.data)}`);
            } else {
                logger.error(`Telegram 发送失败: ${error.message}`);
            }
            throw error;
        }
    }

    /**
     * 格式化消息
     */
    formatMessage(title, message) {
        // 根据严重程度添加不同的图标
        const severityIcons = {
            critical: '🚨',
            warning: '⚠️',
            info: 'ℹ️',
        };

        // 提取 severity (从 title 中)
        let icon = '🔔';
        if (title.includes('[CRITICAL]')) icon = severityIcons.critical;
        else if (title.includes('[WARNING]')) icon = severityIcons.warning;
        else if (title.includes('[INFO]')) icon = severityIcons.info;

        let text = `${icon} <b>${this.escapeHTML(title)}</b>\n\n`;

        // 格式化消息内容
        text += this.formatContent(message);

        return text;
    }

    /**
     * 格式化内容
     */
    formatContent(message) {
        // 如果是 JSON,格式化显示
        try {
            const data = JSON.parse(message);
            const jsonStr = JSON.stringify(data, null, 2);
            return `<pre>${this.escapeHTML(jsonStr)}</pre>`;
        } catch (e) {
            // 普通文本,转义并保留换行
            return this.escapeHTML(message).replace(/\n/g, '\n');
        }
    }

    /**
     * HTML 转义
     */
    escapeHTML(str) {
        const map = {
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#039;',
        };
        return str.replace(/[&<>"']/g, m => map[m]);
    }

    /**
     * 测试连接
     */
    async test(config) {
        try {
            const url = `${this.apiBase}${config.bot_token}/getMe`;

            const response = await axios.get(url, {
                timeout: 10000,
            });

            if (response.data.ok) {
                const bot = response.data.result;
                logger.info(`Telegram 连接测试成功: ${bot.first_name} (@${bot.username})`);
                return true;
            } else {
                throw new Error(response.data.description || 'Unknown error');
            }
        } catch (error) {
            if (error.response) {
                logger.error(`Telegram 连接测试失败: ${error.response.status} - ${JSON.stringify(error.response.data)}`);
            } else {
                logger.error(`Telegram 连接测试失败: ${error.message}`);
            }
            throw error;
        }
    }

    /**
     * 获取 Bot 信息
     */
    async getBotInfo(botToken) {
        try {
            const url = `${this.apiBase}${botToken}/getMe`;
            const response = await axios.get(url, { timeout: 10000 });

            if (response.data.ok) {
                return response.data.result;
            }
            throw new Error(response.data.description || 'Unknown error');
        } catch (error) {
            logger.error(`获取 Bot 信息失败: ${error.message}`);
            throw error;
        }
    }

    /**
     * 获取更新 (用于获取 chat_id)
     */
    async getUpdates(botToken, offset = 0, limit = 10) {
        try {
            const url = `${this.apiBase}${botToken}/getUpdates`;
            const response = await axios.get(url, {
                params: { offset, limit },
                timeout: 10000,
            });

            if (response.data.ok) {
                return response.data.result;
            }
            throw new Error(response.data.description || 'Unknown error');
        } catch (error) {
            logger.error(`获取更新失败: ${error.message}`);
            throw error;
        }
    }
}

module.exports = new TelegramChannel();
