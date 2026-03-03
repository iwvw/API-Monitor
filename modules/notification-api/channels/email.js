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
                // 支持自定义发送者名称: "Name" <email>
                from: config.sender_name ? `"${config.sender_name}" <${config.auth.user}>` : config.auth.user,
                to: config.to || config.auth.user,
                subject: title,
                text: message,
                html: this.formatHTML(message, options.notification, config),
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
    formatHTML(message, notification, config) {
        const severity = notification?.data?.severity || 'info';
        const colors = {
            critical: '#F43F5E', // Rose 500
            warning: '#F59E0B',  // Amber 500
            info: '#3B82F6',     // Blue 500
            batch: '#64748B'     // Slate 500
        };
        const statusColor = notification?.is_batch ? colors.batch : (colors[severity] || colors.info);
        const dashboardUrl = config.base_url ? `${config.base_url.replace(/\/$/, '')}/#/` : null;

        return `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #F8FAFC; margin: 0; padding: 0; -webkit-font-smoothing: antialiased; }
        .wrapper { padding: 48px 20px; text-align: center; }
        .card { max-width: 500px; margin: 0 auto; background-color: #ffffff; border-radius: 16px; overflow: hidden; box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05), 0 10px 15px -3px rgba(0, 0, 0, 0.1); text-align: left; border: 1px solid #E2E8F0; }
        .status-bar { height: 6px; background-color: ${statusColor}; }
        .content { padding: 32px; }
        .header { margin-bottom: 24px; }
        .app-name { font-size: 12px; font-weight: 700; color: #94A3B8; text-transform: uppercase; letter-spacing: 0.1em; margin-bottom: 8px; }
        .title { font-size: 20px; font-weight: 700; color: #1E293B; margin: 0; letter-spacing: -0.025em; }
        
        .data-list { margin-bottom: 32px; border-top: 1px solid #F1F5F9; }
        .data-item { padding: 12px 0; border-bottom: 1px solid #F1F5F9; display: block; }
        .data-label { font-size: 11px; font-weight: 600; color: #64748B; margin-bottom: 4px; text-transform: uppercase; letter-spacing: 0.025em; }
        .data-value { font-size: 14px; font-weight: 500; color: #334155; word-break: break-all; line-height: 1.5; }
        
        .action { text-align: center; margin-top: 8px; }
        .btn { display: inline-block; background-color: #1E293B; color: #ffffff !important; padding: 12px 32px; border-radius: 10px; text-decoration: none; font-size: 14px; font-weight: 600; transition: background-color 0.2s; }
        
        pre { background-color: #F8FAFC; padding: 16px; border-radius: 12px; border: 1px solid #E2E8F0; font-family: 'ui-monospace', 'SFMono-Regular', Menlo, Monaco, Consolas, monospace; font-size: 13px; color: #334155; margin: 0; overflow-x: auto; }
    </style>
</head>
<body>
    <div class="wrapper">
        <div class="card">
            <div class="status-bar"></div>
            <div class="content">
                
                <div class="data-list">
                    ${this.formatMessage(message)}
                </div>

                ${dashboardUrl ? `
                <div class="action">
                    <a href="${dashboardUrl}" class="btn">进入仪表盘看板</a>
                </div>` : ''}
            </div>
        </div>
    </div>
</body>
</html>
        `;
    }

    /**
     * 格式化消息内容
     */
    formatMessage(message) {
        try {
            const data = JSON.parse(message);
            return `<pre>${this.escapeHTML(JSON.stringify(data, null, 2))}</pre>`;
        } catch (e) {
            return message.split('\n')
                .filter(line => line.trim())
                .map(line => {
                    const colonIndex = line.indexOf(':');
                    if (colonIndex > 0) {
                        const label = line.substring(0, colonIndex).replace(/(📋|📧|⏰|📊|🖥️|💳|🔗|🌐|❌|⏱️|💰|🎯)/gu, '').trim();
                        const value = line.substring(colonIndex + 1).trim();
                        return `
                            <div class="data-item">
                                <div class="data-label">${this.escapeHTML(label)}</div>
                                <div class="data-value">${this.escapeHTML(value)}</div>
                            </div>
                        `;
                    }
                    return `<div class="data-item"><div class="data-value">${this.escapeHTML(line)}</div></div>`;
                })
                .join('');
        }
    }

    /**
     * HTML 转义
     */
    escapeHTML(str) {
        if (!str) return '';
        const map = {
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#039;',
        };
        return String(str).replace(/[&<>"']/g, m => map[m]);
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
