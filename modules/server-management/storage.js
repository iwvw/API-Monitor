/**
 * 服务器管理模块存储层
 * 封装数据库操作
 */

const { ServerAccount, ServerMonitorLog, ServerMonitorConfig } = require('../../src/db/models');

/**
 * 服务器账号存储操作
 */
const serverStorage = {
    /**
     * 获取所有服务器
     */
    getAll() {
        return ServerAccount.getAll();
    },

    /**
     * 根据 ID 获取服务器
     */
    getById(id) {
        return ServerAccount.getById(id);
    },

    /**
     * 创建服务器
     */
    create(data) {
        return ServerAccount.create(data);
    },

    /**
     * 更新服务器
     */
    update(id, data) {
        return ServerAccount.update(id, data);
    },

    /**
     * 删除服务器
     */
    delete(id) {
        return ServerAccount.delete(id);
    },

    /**
     * 批量删除服务器
     */
    deleteMany(ids) {
        return ServerAccount.deleteMany(ids);
    },

    /**
     * 更新服务器状态
     */
    updateStatus(id, statusData) {
        return ServerAccount.updateStatus(id, statusData);
    },

    /**
     * 获取在线服务器数量
     */
    getOnlineCount() {
        return ServerAccount.getOnlineCount();
    },

    /**
     * 获取离线服务器数量
     */
    getOfflineCount() {
        return ServerAccount.getOfflineCount();
    }
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
     * 获取服务器的监控日志
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
    }
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
    }
};

module.exports = {
    serverStorage,
    monitorLogStorage,
    monitorConfigStorage
};
