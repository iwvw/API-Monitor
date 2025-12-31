/**
 * 数据库模型统一导出
 */

const { ZeaburAccount, ZeaburProject } = require('../../../modules/zeabur-api/models');
const {
  CloudflareAccount,
  CloudflareDnsTemplate,
  CloudflareZone,
  CloudflareDnsRecord,
} = require('../../../modules/cloudflare-dns/models');
const { OpenAIEndpoint, OpenAIHealthHistory } = require('../../../modules/openai-api/models');
const { SystemConfig, Session, UserSettings, OperationLog } = require('./System');
const {
  ServerAccount,
  ServerMonitorLog,
  ServerMonitorConfig,
  ServerCredential,
  ServerSnippet,
} = require('../../../modules/server-management/models');

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

  // 主机管理模块
  ServerAccount,
  ServerMonitorLog,
  ServerMonitorConfig,
  ServerCredential,
  ServerSnippet,
};
