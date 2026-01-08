/**
 * Email 通知渠道
 */

const nodemailer = require('nodemailer');
const { createLogger } = require('../../../src/utils/logger');
const { decrypt } = require('../../../src/utils/encryption');

const logger = createLogger('NotificationChannel:Email');

class EmailChannel {
    constructor() {
        this.transporters = new Map(); // host -> transporter
    }

    /**
     * 发送邮件
     * @param {Object} config - 邮件配置 (已解密)
     * @param {string} title - 邮件标题
     * @param {string} message - 邮件内容
     * @param {Object} options - 额外选项
     * @returns {Promise<boolean>}
     */
    async send(config, title, message, options = {}) {
        try {
            const transporter = this.getTransporter(config);

            const mailOptions = {
                from: config.auth.user,
                to: config.to || config.auth.user,
                subject: title,
                text: message,
                html: this.formatHTML(message),
                ...options,
            };

            const info = await transporter.sendMail(mailOptions);
            logger.info(`Email 发送成功: ${info.messageId}`);
            return true;
        } catch (error) {
            logger.error(`Email 发送失败: ${error.message}`);
            throw error;
        }
    }

    /**
     * 获取或创建 Transporter
     */
    getTransporter(config) {
        const key = `${config.host}:${config.port}:${config.auth.user}`;

        if (this.transporters.has(key)) {
            return this.transporters.get(key);
        }

        const transporter = nodemailer.createTransport({
            host: config.host,
            port: config.port,
            secure: config.secure, // true for 465, false for other ports
            auth: {
                user: config.auth.user,
                pass: config.auth.pass,
            },
        });

        this.transporters.set(key, transporter);
        return transporter;
    }

    /**
     * 格式化 HTML
     */
    formatHTML(message) {
        return `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            line-height: 1.6;
            color: #333;
            max-width: 600px;
            margin: 0 auto;
            padding: 20px;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 20px;
            border-radius: 8px 8px 0 0;
            text-align: center;
        }
        .content {
            background: #f9f9f9;
            padding: 20px;
            border-radius: 0 0 8px 8px;
            border: 1px solid #e0e0e0;
            border-top: none;
        }
        .footer {
            margin-top: 20px;
            text-align: center;
            color: #999;
            font-size: 12px;
        }
        pre {
            background: #fff;
            padding: 15px;
            border-radius: 4px;
            border: 1px solid #ddd;
            overflow-x: auto;
        }
        .timestamp {
            color: #666;
            font-size: 12px;
            margin-top: 10px;
        }
    </style>
</head>
<body>
    <div class="header">
        <h2>🔔 系统通知</h2>
    </div>
    <div class="content">
        ${this.formatMessage(message)}
        <div class="timestamp">
            发送时间: ${new Date().toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })}
        </div>
    </div>
    <div class="footer">
        <p>本邮件由 API 监控系统自动发送,请勿回复</p>
    </div>
</body>
</html>
        `;
    }

    /**
     * 格式化消息内容
     */
    formatMessage(message) {
        // 如果是 JSON,格式化显示
        try {
            const data = JSON.parse(message);
            return `<pre>${JSON.stringify(data, null, 2)}</pre>`;
        } catch (e) {
            // 普通文本,转换为段落
            return message.split('\n').map(line => `<p>${line}</p>`).join('');
        }
    }

    /**
     * 测试连接
     */
    async test(config) {
        try {
            const transporter = this.getTransporter(config);
            await transporter.verify();
            logger.info('Email 连接测试成功');
            return true;
        } catch (error) {
            logger.error(`Email 连接测试失败: ${error.message}`);
            throw error;
        }
    }

    /**
     * 关闭所有 Transporter
     */
    close() {
        for (const [key, transporter] of this.transporters) {
            transporter.close();
        }
        this.transporters.clear();
    }
}

module.exports = new EmailChannel();
