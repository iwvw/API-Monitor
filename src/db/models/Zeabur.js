const BaseModel = require('./BaseModel');

/**
 * Zeabur 账号模型
 */
class Zeabur extends BaseModel {
    constructor() {
        super('zeabur_accounts');
    }

    /**
     * 创建账号
     */
    createAccount(accountData) {
        const data = {
            id: accountData.id || `zb_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
            name: accountData.name,
            token: accountData.token,
            status: accountData.status || 'active',
            email: accountData.email || null,
            username: accountData.username || null,
            balance: accountData.balance || 0,
            cost: accountData.cost || 0,
            created_at: accountData.created_at || new Date().toISOString(),
            updated_at: new Date().toISOString(),
            last_synced_at: accountData.last_synced_at || null
        };

        this.insert(data);
        return data;
    }

    /**
     * 更新账号信息
     */
    updateAccount(id, updates) {
        const allowedFields = ['name', 'token', 'status', 'email', 'username', 'balance', 'cost', 'last_synced_at'];
        const data = {};

        Object.keys(updates).forEach(key => {
            if (allowedFields.includes(key)) {
                data[key] = updates[key];
            }
        });

        return this.update(id, data);
    }

    /**
     * 获取活跃账号
     */
    getActiveAccounts() {
        return this.findWhere({ status: 'active' });
    }

    /**
     * 更新账号余额和费用
     */
    updateBalance(id, balance, cost) {
        return this.update(id, {
            balance,
            cost,
            last_synced_at: new Date().toISOString()
        });
    }

    /**
     * 获取账号统计信息
     */
    getAccountStats() {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT
                COUNT(*) as total_accounts,
                SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) as active_accounts,
                SUM(balance) as total_balance,
                SUM(cost) as total_cost
            FROM ${this.tableName}
        `);
        return stmt.get();
    }
}

/**
 * Zeabur 项目模型
 */
class ZeaburProject extends BaseModel {
    constructor() {
        super('zeabur_projects');
    }

    /**
     * 创建项目
     */
    createProject(projectData) {
        const data = {
            id: projectData.id,
            account_id: projectData.account_id,
            name: projectData.name,
            region: projectData.region || null,
            status: projectData.status || null,
            created_at: projectData.created_at || new Date().toISOString(),
            updated_at: new Date().toISOString()
        };

        this.insert(data);
        return data;
    }

    /**
     * 获取账号的所有项目
     */
    getProjectsByAccount(accountId) {
        return this.findWhere({ account_id: accountId });
    }

    /**
     * 批量更新账号的项目
     */
    syncAccountProjects(accountId, projects) {
        const db = this.getDb();

        const transaction = db.transaction(() => {
            // 删除该账号的所有旧项目
            this.deleteWhere({ account_id: accountId });

            // 插入新项目
            if (projects && projects.length > 0) {
                projects.forEach(project => {
                    this.createProject({
                        ...project,
                        account_id: accountId
                    });
                });
            }
        });

        transaction();
        return projects.length;
    }

    /**
     * 获取项目统计
     */
    getProjectStats(accountId = null) {
        const db = this.getDb();

        if (accountId) {
            const stmt = db.prepare(`
                SELECT COUNT(*) as count
                FROM ${this.tableName}
                WHERE account_id = ?
            `);
            return stmt.get(accountId);
        }

        const stmt = db.prepare(`
            SELECT
                account_id,
                COUNT(*) as project_count
            FROM ${this.tableName}
            GROUP BY account_id
        `);
        return stmt.all();
    }
}

module.exports = {
    ZeaburAccount: new Zeabur(),
    ZeaburProject: new ZeaburProject()
};
