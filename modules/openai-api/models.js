const BaseModel = require('../../src/db/models/BaseModel');

/**
 * OpenAI API 端点模型
 */
class OpenAIEndpoint extends BaseModel {
    constructor() {
        super('openai_endpoints');
    }

    /**
     * 创建端点
     */
    createEndpoint(endpointData) {
        const data = {
            id: endpointData.id || `oai_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
            name: endpointData.name,
            base_url: endpointData.baseUrl || endpointData.base_url,
            api_key: endpointData.apiKey || endpointData.api_key,
            status: endpointData.status || 'unknown',
            enabled: endpointData.enabled !== undefined ? (endpointData.enabled ? 1 : 0) : 1,
            models: endpointData.models
                ? (typeof endpointData.models === 'string' ? endpointData.models : JSON.stringify(endpointData.models))
                : null,
            created_at: endpointData.createdAt || endpointData.created_at || new Date().toISOString(),
            last_used: endpointData.lastUsed || endpointData.last_used || null,
            last_checked: endpointData.lastChecked || endpointData.last_checked || null
        };

        this.insert(data);
        return data;
    }

    /**
     * 更新端点
     */
    updateEndpoint(id, updates) {
        const data = { ...updates };

        // 处理 models 字段
        if (data.models && typeof data.models !== 'string') {
            data.models = JSON.stringify(data.models);
        }

        // 字段映射
        if (data.baseUrl) {
            data.base_url = data.baseUrl;
            delete data.baseUrl;
        }
        if (data.apiKey) {
            data.api_key = data.apiKey;
            delete data.apiKey;
        }
        if (data.enabled !== undefined) {
            data.enabled = data.enabled ? 1 : 0;
        }
        if (data.lastUsed) {
            data.last_used = data.lastUsed;
            delete data.lastUsed;
        }
        if (data.lastChecked) {
            data.last_checked = data.lastChecked;
            delete data.lastChecked;
        }

        return this.update(id, data);
    }

    /**
     * 获取端点（解析 JSON）
     */
    getEndpoint(id) {
        const endpoint = this.findById(id);
        if (endpoint && endpoint.models) {
            endpoint.models = JSON.parse(endpoint.models);
        }
        return endpoint;
    }

    /**
     * 获取所有端点（解析 JSON）
     */
    getAllEndpoints() {
        const endpoints = this.findAll();
        return endpoints.map(endpoint => ({
            ...endpoint,
            models: endpoint.models ? JSON.parse(endpoint.models) : []
        }));
    }

    /**
     * 更新端点状态
     */
    updateStatus(id, status, models = null) {
        const data = {
            status,
            last_checked: new Date().toISOString()
        };

        if (models) {
            data.models = JSON.stringify(models);
        }

        return this.update(id, data);
    }

    /**
     * 更新最后使用时间
     */
    updateLastUsed(id) {
        return this.update(id, {
            last_used: new Date().toISOString()
        });
    }

    /**
     * 获取有效端点
     */
    getValidEndpoints() {
        return this.findWhere({ status: 'valid' });
    }

    /**
     * 获取端点统计
     */
    getEndpointStats() {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT
                COUNT(*) as total_endpoints,
                SUM(CASE WHEN status = 'valid' THEN 1 ELSE 0 END) as valid_endpoints,
                SUM(CASE WHEN status = 'invalid' THEN 1 ELSE 0 END) as invalid_endpoints,
                SUM(CASE WHEN status = 'unknown' THEN 1 ELSE 0 END) as unknown_endpoints
            FROM ${this.tableName}
        `);
        return stmt.get();
    }
}

/**
 * OpenAI 健康检查历史模型
 */
class OpenAIHealthHistory extends BaseModel {
    constructor() {
        super('openai_health_history');
    }

    /**
     * 记录健康检查
     */
    recordCheck(checkData) {
        const data = {
            endpoint_id: checkData.endpoint_id,
            status: checkData.status,
            response_time: checkData.response_time || null,
            error_message: checkData.error_message || null,
            checked_at: checkData.checked_at || new Date().toISOString()
        };

        this.insert(data);
        return data;
    }

    /**
     * 获取端点的健康历史
     */
    getEndpointHistory(endpointId, limit = 100) {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE endpoint_id = ?
            ORDER BY checked_at DESC
            LIMIT ?
        `);
        return stmt.all(endpointId, limit);
    }

    /**
     * 获取端点的最近检查
     */
    getLatestCheck(endpointId) {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE endpoint_id = ?
            ORDER BY checked_at DESC
            LIMIT 1
        `);
        return stmt.get(endpointId);
    }

    /**
     * 获取端点的健康统计
     */
    getEndpointHealthStats(endpointId, days = 7) {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT
                COUNT(*) as total_checks,
                SUM(CASE WHEN status = 'valid' THEN 1 ELSE 0 END) as success_count,
                SUM(CASE WHEN status = 'invalid' THEN 1 ELSE 0 END) as failure_count,
                AVG(response_time) as avg_response_time,
                MIN(response_time) as min_response_time,
                MAX(response_time) as max_response_time
            FROM ${this.tableName}
            WHERE endpoint_id = ?
                AND checked_at >= datetime('now', '-${days} days')
        `);
        return stmt.get(endpointId);
    }

    /**
     * 清理旧的健康检查记录
     */
    cleanOldRecords(days = 30) {
        const db = this.getDb();
        const stmt = db.prepare(`
            DELETE FROM ${this.tableName}
            WHERE checked_at < datetime('now', '-${days} days')
        `);
        return stmt.run().changes;
    }
}

module.exports = {
    OpenAIEndpoint: new OpenAIEndpoint(),
    OpenAIHealthHistory: new OpenAIHealthHistory()
};
