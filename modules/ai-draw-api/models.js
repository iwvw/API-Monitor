/**
 * AI Draw 模块 - 数据模型
 * 
 * 包含独立的 Provider 管理，不依赖 ai-chat-api
 */

const dbService = require('../../src/db/database');
const { v4: uuidv4 } = require('uuid');

// 获取数据库实例
const getDb = () => dbService.getDatabase();

/**
 * 生成唯一 ID
 */
function generateId(prefix = 'draw') {
    return `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
}

/**
 * Provider 模型 - AI Draw 独立的 Provider 管理
 */
class DrawProviderModel {
    /**
     * 获取所有 Provider
     */
    static getAll() {
        const db = getDb();
        const rows = db.prepare(`
            SELECT * FROM ai_draw_providers 
            ORDER BY is_default DESC, sort_order ASC, created_at ASC
        `).all();
        return rows.map(row => ({
            ...row,
            enabled: Boolean(row.enabled),
            is_default: Boolean(row.is_default),
        }));
    }

    /**
     * 获取启用的 Provider
     */
    static getEnabled() {
        const db = getDb();
        const rows = db.prepare(`
            SELECT * FROM ai_draw_providers 
            WHERE enabled = 1
            ORDER BY is_default DESC, sort_order ASC
        `).all();
        return rows.map(row => ({
            ...row,
            enabled: true,
            is_default: Boolean(row.is_default),
        }));
    }

    /**
     * 获取默认 Provider
     */
    static getDefault() {
        const db = getDb();
        const row = db.prepare(`
            SELECT * FROM ai_draw_providers 
            WHERE enabled = 1 
            ORDER BY is_default DESC, sort_order ASC 
            LIMIT 1
        `).get();
        if (!row) return null;
        return {
            ...row,
            enabled: true,
            is_default: Boolean(row.is_default),
        };
    }

    /**
     * 根据 ID 获取 Provider
     */
    static getById(id) {
        const db = getDb();
        const row = db.prepare('SELECT * FROM ai_draw_providers WHERE id = ?').get(id);
        if (!row) return null;
        return {
            ...row,
            enabled: Boolean(row.enabled),
            is_default: Boolean(row.is_default),
        };
    }

    /**
     * 创建 Provider
     */
    static create(data) {
        const db = getDb();
        const id = data.id || generateId('dp');

        // 如果设为默认，先取消其他默认
        if (data.is_default) {
            db.prepare('UPDATE ai_draw_providers SET is_default = 0').run();
        }

        db.prepare(`
            INSERT INTO ai_draw_providers 
            (id, name, source_type, base_url, api_key, default_model, internal_provider_id, enabled, is_default, sort_order)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `).run(
            id,
            data.name,
            data.source_type || 'external',
            data.base_url || null,
            data.api_key || null,
            data.default_model || null,
            data.internal_provider_id || null,
            data.enabled !== false ? 1 : 0,
            data.is_default ? 1 : 0,
            data.sort_order || 0
        );

        return this.getById(id);
    }

    /**
     * 更新 Provider
     */
    static update(id, data) {
        const db = getDb();
        const fields = [];
        const values = [];

        // 如果设为默认，先取消其他默认
        if (data.is_default) {
            db.prepare('UPDATE ai_draw_providers SET is_default = 0 WHERE id != ?').run(id);
        }

        if (data.name !== undefined) { fields.push('name = ?'); values.push(data.name); }
        if (data.source_type !== undefined) { fields.push('source_type = ?'); values.push(data.source_type); }
        if (data.base_url !== undefined) { fields.push('base_url = ?'); values.push(data.base_url); }
        if (data.api_key !== undefined) { fields.push('api_key = ?'); values.push(data.api_key); }
        if (data.default_model !== undefined) { fields.push('default_model = ?'); values.push(data.default_model); }
        if (data.internal_provider_id !== undefined) { fields.push('internal_provider_id = ?'); values.push(data.internal_provider_id); }
        if (data.enabled !== undefined) { fields.push('enabled = ?'); values.push(data.enabled ? 1 : 0); }
        if (data.is_default !== undefined) { fields.push('is_default = ?'); values.push(data.is_default ? 1 : 0); }
        if (data.sort_order !== undefined) { fields.push('sort_order = ?'); values.push(data.sort_order); }

        if (fields.length === 0) return this.getById(id);

        fields.push("updated_at = datetime('now', 'localtime')");
        values.push(id);

        db.prepare(`UPDATE ai_draw_providers SET ${fields.join(', ')} WHERE id = ?`).run(...values);
        return this.getById(id);
    }

    /**
     * 删除 Provider
     */
    static delete(id) {
        const db = getDb();
        const result = db.prepare('DELETE FROM ai_draw_providers WHERE id = ?').run(id);
        return result.changes > 0;
    }

    /**
     * 设为默认
     */
    static setDefault(id) {
        const db = getDb();
        db.prepare('UPDATE ai_draw_providers SET is_default = 0').run();
        db.prepare('UPDATE ai_draw_providers SET is_default = 1 WHERE id = ?').run(id);
        return this.getById(id);
    }
}

/**
 * 项目模型
 */
class ProjectModel {
    /**
     * 获取所有项目
     */
    static getAll(limit = 50) {
        const db = getDb();
        return db.prepare(`
            SELECT * FROM ai_draw_projects 
            ORDER BY updated_at DESC 
            LIMIT ?
        `).all(limit);
    }

    /**
     * 根据 ID 获取项目
     */
    static getById(id) {
        const db = getDb();
        return db.prepare('SELECT * FROM ai_draw_projects WHERE id = ?').get(id);
    }

    /**
     * 创建项目
     */
    static create(data) {
        const db = getDb();
        const id = data.id || uuidv4();
        const now = new Date().toISOString();

        db.prepare(`
            INSERT INTO ai_draw_projects (id, title, engine_type, content, thumbnail, provider_id, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        `).run(
            id,
            data.title || 'Untitled',
            data.engine_type || 'drawio',
            data.content || null,
            data.thumbnail || null,
            data.provider_id || null,
            now,
            now
        );

        return this.getById(id);
    }

    /**
     * 更新项目
     */
    static update(id, data) {
        const db = getDb();
        const fields = [];
        const values = [];

        if (data.title !== undefined) { fields.push('title = ?'); values.push(data.title); }
        if (data.engine_type !== undefined) { fields.push('engine_type = ?'); values.push(data.engine_type); }
        if (data.content !== undefined) { fields.push('content = ?'); values.push(data.content); }
        if (data.thumbnail !== undefined) { fields.push('thumbnail = ?'); values.push(data.thumbnail); }
        if (data.provider_id !== undefined) { fields.push('provider_id = ?'); values.push(data.provider_id); }

        if (fields.length === 0) return this.getById(id);

        fields.push('updated_at = ?');
        values.push(new Date().toISOString());
        values.push(id);

        db.prepare(`UPDATE ai_draw_projects SET ${fields.join(', ')} WHERE id = ?`).run(...values);
        return this.getById(id);
    }

    /**
     * 删除项目
     */
    static delete(id) {
        const db = getDb();
        const result = db.prepare('DELETE FROM ai_draw_projects WHERE id = ?').run(id);
        return result.changes > 0;
    }
}

/**
 * 聊天历史模型
 */
class ChatHistoryModel {
    /**
     * 获取项目的聊天历史
     */
    static getByProject(projectId, limit = 100) {
        const db = getDb();
        return db.prepare(`
            SELECT * FROM ai_draw_chat_history 
            WHERE project_id = ? 
            ORDER BY created_at ASC 
            LIMIT ?
        `).all(projectId, limit);
    }

    /**
     * 添加聊天消息
     */
    static add(projectId, role, content) {
        const db = getDb();
        const result = db.prepare(`
            INSERT INTO ai_draw_chat_history (project_id, role, content)
            VALUES (?, ?, ?)
        `).run(projectId, role, content);

        return db.prepare('SELECT * FROM ai_draw_chat_history WHERE id = ?').get(result.lastInsertRowid);
    }

    /**
     * 清空项目聊天历史
     */
    static clearByProject(projectId) {
        const db = getDb();
        const result = db.prepare('DELETE FROM ai_draw_chat_history WHERE project_id = ?').run(projectId);
        return result.changes;
    }
}

module.exports = {
    DrawProviderModel,
    ProjectModel,
    ChatHistoryModel,
    generateId,
};
