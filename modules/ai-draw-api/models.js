/**
 * AI Draw 模块 - 数据模型
 */

const dbService = require('../../src/db/database');
const { v4: uuidv4 } = require('uuid');

// 获取数据库实例
const getDb = () => dbService.getDatabase();

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
      INSERT INTO ai_draw_projects (id, title, engine_type, content, thumbnail, created_at, updated_at)
      VALUES (?, ?, ?, ?, ?, ?, ?)
    `).run(
            id,
            data.title || 'Untitled',
            data.engine_type || 'drawio',
            data.content || null,
            data.thumbnail || null,
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

        if (data.title !== undefined) {
            fields.push('title = ?');
            values.push(data.title);
        }
        if (data.engine_type !== undefined) {
            fields.push('engine_type = ?');
            values.push(data.engine_type);
        }
        if (data.content !== undefined) {
            fields.push('content = ?');
            values.push(data.content);
        }
        if (data.thumbnail !== undefined) {
            fields.push('thumbnail = ?');
            values.push(data.thumbnail);
        }

        if (fields.length === 0) return this.getById(id);

        fields.push('updated_at = ?');
        values.push(new Date().toISOString());
        values.push(id);

        db.prepare(`
      UPDATE ai_draw_projects SET ${fields.join(', ')} WHERE id = ?
    `).run(...values);

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
    ProjectModel,
    ChatHistoryModel,
};
