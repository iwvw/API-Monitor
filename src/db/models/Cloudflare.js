const BaseModel = require('./BaseModel');

/**
 * Cloudflare 账号模型
 */
class CloudflareAccount extends BaseModel {
    constructor() {
        super('cf_accounts');
    }

    /**
     * 创建 CF 账号
     */
    createAccount(accountData) {
        const data = {
            id: accountData.id || `cf_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
            name: accountData.name,
            api_token: accountData.apiToken || accountData.api_token,
            email: accountData.email || null,
            created_at: accountData.createdAt || accountData.created_at || new Date().toISOString(),
            last_used: accountData.lastUsed || accountData.last_used || null,
            is_active: accountData.is_active !== undefined ? accountData.is_active : 1
        };

        this.insert(data);
        return data;
    }

    /**
     * 更新账号
     */
    updateAccount(id, updates) {
        const allowedFields = ['name', 'api_token', 'email', 'last_used', 'is_active'];
        const data = {};

        Object.keys(updates).forEach(key => {
            if (allowedFields.includes(key)) {
                data[key] = updates[key];
            }
        });

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
     * 获取活跃账号
     */
    getActiveAccounts() {
        return this.findWhere({ is_active: 1 });
    }
}

/**
 * Cloudflare DNS 模板模型
 */
class CloudflareDnsTemplate extends BaseModel {
    constructor() {
        super('cf_dns_templates');
    }

    /**
     * 创建 DNS 模板
     */
    createTemplate(templateData) {
        const data = {
            id: templateData.id || `tpl_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
            name: templateData.name,
            description: templateData.description || null,
            records: typeof templateData.records === 'string'
                ? templateData.records
                : JSON.stringify(templateData.records),
            created_at: templateData.created_at || new Date().toISOString(),
            updated_at: new Date().toISOString()
        };

        this.insert(data);
        return data;
    }

    /**
     * 获取模板（解析 JSON）
     */
    getTemplate(id) {
        const template = this.findById(id);
        if (template && template.records) {
            template.records = JSON.parse(template.records);
        }
        return template;
    }

    /**
     * 获取所有模板（解析 JSON）
     */
    getAllTemplates() {
        const templates = this.findAll();
        return templates.map(template => ({
            ...template,
            records: JSON.parse(template.records)
        }));
    }

    /**
     * 更新模板
     */
    updateTemplate(id, updates) {
        const data = { ...updates };
        if (data.records && typeof data.records !== 'string') {
            data.records = JSON.stringify(data.records);
        }
        return this.update(id, data);
    }
}

/**
 * Cloudflare 域名模型
 */
class CloudflareZone extends BaseModel {
    constructor() {
        super('cf_zones');
    }

    /**
     * 创建域名
     */
    createZone(zoneData) {
        const data = {
            id: zoneData.id,
            account_id: zoneData.account_id,
            name: zoneData.name,
            status: zoneData.status || null,
            created_at: zoneData.created_at || new Date().toISOString()
        };

        this.insert(data);
        return data;
    }

    /**
     * 获取账号的所有域名
     */
    getZonesByAccount(accountId) {
        return this.findWhere({ account_id: accountId });
    }

    /**
     * 同步账号的域名列表
     */
    syncAccountZones(accountId, zones) {
        const db = this.getDb();

        const transaction = db.transaction(() => {
            // 删除该账号的所有旧域名
            this.deleteWhere({ account_id: accountId });

            // 插入新域名
            if (zones && zones.length > 0) {
                zones.forEach(zone => {
                    this.createZone({
                        ...zone,
                        account_id: accountId
                    });
                });
            }
        });

        transaction();
        return zones.length;
    }
}

/**
 * Cloudflare DNS 记录模型
 */
class CloudflareDnsRecord extends BaseModel {
    constructor() {
        super('cf_dns_records');
    }

    /**
     * 创建 DNS 记录
     */
    createRecord(recordData) {
        const data = {
            id: recordData.id,
            zone_id: recordData.zone_id,
            type: recordData.type,
            name: recordData.name,
            content: recordData.content,
            ttl: recordData.ttl || 1,
            proxied: recordData.proxied ? 1 : 0,
            priority: recordData.priority || null,
            created_at: recordData.created_at || new Date().toISOString(),
            updated_at: new Date().toISOString()
        };

        this.insert(data);
        return data;
    }

    /**
     * 获取域名的所有 DNS 记录
     */
    getRecordsByZone(zoneId) {
        return this.findWhere({ zone_id: zoneId });
    }

    /**
     * 同步域名的 DNS 记录
     */
    syncZoneRecords(zoneId, records) {
        const db = this.getDb();

        const transaction = db.transaction(() => {
            // 删除该域名的所有旧记录
            this.deleteWhere({ zone_id: zoneId });

            // 插入新记录
            if (records && records.length > 0) {
                records.forEach(record => {
                    this.createRecord({
                        ...record,
                        zone_id: zoneId
                    });
                });
            }
        });

        transaction();
        return records.length;
    }

    /**
     * 按类型查询 DNS 记录
     */
    getRecordsByType(zoneId, type) {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE zone_id = ? AND type = ?
            ORDER BY name
        `);
        return stmt.all(zoneId, type);
    }
}

module.exports = {
    CloudflareAccount: new CloudflareAccount(),
    CloudflareDnsTemplate: new CloudflareDnsTemplate(),
    CloudflareZone: new CloudflareZone(),
    CloudflareDnsRecord: new CloudflareDnsRecord()
};
