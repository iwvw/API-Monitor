/**
 * 数据库模型统一导出
 */

const {
  CloudflareAccount,
  CloudflareDnsTemplate,
  CloudflareZone,
  CloudflareDnsRecord,
} = require('../../../modules/cloudflare-api/models');
const { OpenAIEndpoint, OpenAIHealthHistory } = require('../../../modules/openai-api/models');
const { SystemConfig, Session, UserSettings, OperationLog, LoginAttempt } = require('./System');
const {
  ServerAccount,
  ServerMonitorLog,
  ServerMonitorConfig,
  ServerCredential,
  ServerSnippet,
} = require('../../../modules/server-api/models');

module.exports = {
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
  LoginAttempt,

  // 主机管理模块
  ServerAccount,
  ServerMonitorLog,
  ServerMonitorConfig,
  ServerCredential,
  ServerSnippet,
};
