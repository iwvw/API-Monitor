/**
 * 数据库模型统一导出
 */

const { ZeaburAccount, ZeaburProject } = require('./Zeabur');
const {
    CloudflareAccount,
    CloudflareDnsTemplate,
    CloudflareZone,
    CloudflareDnsRecord
} = require('./Cloudflare');
const { OpenAIEndpoint, OpenAIHealthHistory } = require('./OpenAI');
const { SystemConfig, Session, UserSettings, OperationLog } = require('./System');
const { ServerAccount, ServerMonitorLog, ServerMonitorConfig } = require('./Server');

module.exports = {
    // Zeabur 模块
    ZeaburAccount,
    ZeaburProject,

    // Cloudflare 模块
    CloudflareAccount,
    CloudflareDnsTemplate,
    CloudflareZone,
    CloudflareDnsRecord,

    // OpenAI 模块
    OpenAIEndpoint,
    OpenAIHealthHistory,

    // 系统模块
    SystemConfig,
    Session,
    UserSettings,
    OperationLog,

    // 服务器管理模块
    ServerAccount,
    ServerMonitorLog,
    ServerMonitorConfig
};
