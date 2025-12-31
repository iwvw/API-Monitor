/**
 * 主机管理模块存储层
 * 封装数据库操作
 */

const {
  ServerAccount,
  ServerMonitorLog,
  ServerMonitorConfig,
  ServerCredential,
  ServerSnippet,
} = require('../../src/db/models');

/**
 * 主机账号存储操作
 */
const serverStorage = {
  /**
   * 获取所有主机
   */
  getAll() {
    return ServerAccount.getAll();
  },

  /**
   * 根据 ID 获取主机
   */
  getById(id) {
    return ServerAccount.getById(id);
  },

  /**
   * 创建主机
   */
  create(data) {
    return ServerAccount.create(data);
  },

  /**
   * 更新主机
   */
  update(id, data) {
    return ServerAccount.update(id, data);
  },

  /**
   * 删除主机
   */
  delete(id) {
    return ServerAccount.delete(id);
  },

  /**
   * 批量删除主机
   */
  deleteMany(ids) {
    return ServerAccount.deleteMany(ids);
  },

  /**
   * 更新主机状态
   */
  updateStatus(id, statusData) {
    return ServerAccount.updateStatus(id, statusData);
  },

  /**
   * 获取在线主机数量
   */
  getOnlineCount() {
    return ServerAccount.getOnlineCount();
  },

  /**
   * 获取离线主机数量
   */
  getOfflineCount() {
    return ServerAccount.getOfflineCount();
  },
};

/**
 * 监控日志存储操作
 */
const monitorLogStorage = {
  /**
   * 创建监控日志
   */
  create(data) {
    return ServerMonitorLog.create(data);
  },

  /**
   * 获取主机的监控日志
   */
  getByServerId(serverId, options) {
    return ServerMonitorLog.getByServerId(serverId, options);
  },

  /**
   * 获取所有监控日志
   */
  getAll(options) {
    return ServerMonitorLog.getAll(options);
  },

  /**
   * 删除过期日志
   */
  deleteOldLogs(days) {
    return ServerMonitorLog.deleteOldLogs(days);
  },

  /**
   * 获取日志总数
   */
  getCount(filters) {
    return ServerMonitorLog.getCount(filters);
  },
};

/**
 * 监控配置存储操作
 */
const monitorConfigStorage = {
  /**
   * 获取监控配置
   */
  get() {
    return ServerMonitorConfig.get();
  },

  /**
   * 更新监控配置
   */
  update(data) {
    return ServerMonitorConfig.update(data);
  },
};

/**
 * 主机凭据存储操作
 */
const credentialStorage = {
  getAll() {
    return ServerCredential.getAll();
  },
  create(data) {
    return ServerCredential.create(data);
  },
  delete(id) {
    return ServerCredential.delete(id);
  },
};

/**
 * 代码片段存储操作
 */
const snippetStorage = {
  getAll() {
    return ServerSnippet.getAll();
  },
  create(data) {
    return ServerSnippet.create(data);
  },
  update(id, data) {
    return ServerSnippet.update(id, data);
  },
  delete(id) {
    return ServerSnippet.delete(id);
  },
};

module.exports = {
  serverStorage,
  monitorLogStorage,
  monitorConfigStorage,
  credentialStorage,
  snippetStorage,
};
