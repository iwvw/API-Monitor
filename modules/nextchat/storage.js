/**
 * NextChat 模块 - 存储层
 * 管理会话和消息的 CRUD 操作
 */

const path = require('path');
const fs = require('fs');

// 数据库服务
let db;

/**
 * 初始化数据库
 */
function initDatabase() {
    const dbService = require('../../src/db/database');
    db = dbService.getDatabase();

    // 执行 schema
    const schemaPath = path.join(__dirname, 'schema.sql');
    const schema = fs.readFileSync(schemaPath, 'utf8');
    db.exec(schema);
}

/**
 * 生成唯一 ID
 */
function generateId() {
    return `nc_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
}

// ==================== 会话管理 ====================

/**
 * 获取所有会话
 * @returns {Array} 会话列表
 */
function getAllSessions() {
    const stmt = db.prepare(`
    SELECT s.*, 
           (SELECT COUNT(*) FROM nextchat_messages WHERE session_id = s.id) as message_count,
           (SELECT content FROM nextchat_messages WHERE session_id = s.id ORDER BY created_at DESC LIMIT 1) as last_message
    FROM nextchat_sessions s
    ORDER BY s.updated_at DESC
  `);
    return stmt.all();
}

/**
 * 获取单个会话
 * @param {string} id 会话 ID
 * @returns {Object|null} 会话信息
 */
function getSession(id) {
    const stmt = db.prepare('SELECT * FROM nextchat_sessions WHERE id = ?');
    return stmt.get(id);
}

/**
 * 创建新会话
 * @param {Object} data 会话数据
 * @returns {Object} 新创建的会话
 */
function createSession(data = {}) {
    const id = generateId();
    const topic = data.topic || '新对话';
    const model = data.model || 'gemini-2.5-flash';
    const systemPrompt = data.system_prompt || null;

    const stmt = db.prepare(`
    INSERT INTO nextchat_sessions (id, topic, model, system_prompt)
    VALUES (?, ?, ?, ?)
  `);
    stmt.run(id, topic, model, systemPrompt);

    return getSession(id);
}

/**
 * 更新会话
 * @param {string} id 会话 ID
 * @param {Object} data 更新数据
 * @returns {Object|null} 更新后的会话
 */
function updateSession(id, data) {
    const updates = [];
    const values = [];

    if (data.topic !== undefined) {
        updates.push('topic = ?');
        values.push(data.topic);
    }
    if (data.model !== undefined) {
        updates.push('model = ?');
        values.push(data.model);
    }
    if (data.system_prompt !== undefined) {
        updates.push('system_prompt = ?');
        values.push(data.system_prompt);
    }

    if (updates.length === 0) return getSession(id);

    updates.push('updated_at = CURRENT_TIMESTAMP');
    values.push(id);

    const stmt = db.prepare(`
    UPDATE nextchat_sessions 
    SET ${updates.join(', ')}
    WHERE id = ?
  `);
    stmt.run(...values);

    return getSession(id);
}

/**
 * 删除会话
 * @param {string} id 会话 ID
 * @returns {boolean} 是否成功删除
 */
function deleteSession(id) {
    // 消息会通过 CASCADE 自动删除
    const stmt = db.prepare('DELETE FROM nextchat_sessions WHERE id = ?');
    const result = stmt.run(id);
    return result.changes > 0;
}

/**
 * 清空所有会话
 * @returns {number} 删除的会话数量
 */
function clearAllSessions() {
    const stmt = db.prepare('DELETE FROM nextchat_sessions');
    const result = stmt.run();
    return result.changes;
}

// ==================== 消息管理 ====================

/**
 * 获取会话的所有消息
 * @param {string} sessionId 会话 ID
 * @returns {Array} 消息列表
 */
function getMessages(sessionId) {
    const stmt = db.prepare(`
    SELECT * FROM nextchat_messages 
    WHERE session_id = ?
    ORDER BY created_at ASC
  `);
    return stmt.all(sessionId);
}

/**
 * 添加消息
 * @param {string} sessionId 会话 ID
 * @param {Object} message 消息数据
 * @returns {Object} 新创建的消息
 */
function addMessage(sessionId, message) {
    const id = generateId();
    const role = message.role || 'user';
    const content = message.content || '';
    const model = message.model || null;
    const tokenCount = message.token_count || 0;

    const stmt = db.prepare(`
    INSERT INTO nextchat_messages (id, session_id, role, content, model, token_count)
    VALUES (?, ?, ?, ?, ?, ?)
  `);
    stmt.run(id, sessionId, role, content, model, tokenCount);

    // 更新会话的 updated_at
    const updateStmt = db.prepare('UPDATE nextchat_sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?');
    updateStmt.run(sessionId);

    return { id, session_id: sessionId, role, content, model, token_count: tokenCount };
}

/**
 * 更新消息
 * @param {string} id 消息 ID
 * @param {Object} data 更新数据
 * @returns {boolean} 是否成功更新
 */
function updateMessage(id, data) {
    const updates = [];
    const values = [];

    if (data.content !== undefined) {
        updates.push('content = ?');
        values.push(data.content);
    }
    if (data.token_count !== undefined) {
        updates.push('token_count = ?');
        values.push(data.token_count);
    }

    if (updates.length === 0) return false;

    values.push(id);

    const stmt = db.prepare(`
    UPDATE nextchat_messages 
    SET ${updates.join(', ')}
    WHERE id = ?
  `);
    const result = stmt.run(...values);
    return result.changes > 0;
}

/**
 * 删除消息
 * @param {string} id 消息 ID
 * @returns {boolean} 是否成功删除
 */
function deleteMessage(id) {
    const stmt = db.prepare('DELETE FROM nextchat_messages WHERE id = ?');
    const result = stmt.run(id);
    return result.changes > 0;
}

/**
 * 清空会话的所有消息
 * @param {string} sessionId 会话 ID
 * @returns {number} 删除的消息数量
 */
function clearMessages(sessionId) {
    const stmt = db.prepare('DELETE FROM nextchat_messages WHERE session_id = ?');
    const result = stmt.run(sessionId);
    return result.changes;
}

// ==================== 统计信息 ====================

/**
 * 获取统计信息
 * @returns {Object} 统计数据
 */
function getStats() {
    const sessionCount = db.prepare('SELECT COUNT(*) as count FROM nextchat_sessions').get();
    const messageCount = db.prepare('SELECT COUNT(*) as count FROM nextchat_messages').get();
    const totalTokens = db.prepare('SELECT SUM(token_count) as total FROM nextchat_messages').get();

    return {
        sessions: sessionCount.count,
        messages: messageCount.count,
        tokens: totalTokens.total || 0
    };
}

module.exports = {
    initDatabase,
    // 会话管理
    getAllSessions,
    getSession,
    createSession,
    updateSession,
    deleteSession,
    clearAllSessions,
    // 消息管理
    getMessages,
    addMessage,
    updateMessage,
    deleteMessage,
    clearMessages,
    // 统计
    getStats
};
