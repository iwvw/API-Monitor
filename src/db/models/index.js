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
    OperationLog
};
