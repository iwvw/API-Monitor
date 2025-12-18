/**
 * {{MODULE_NAME}} 模块 - 数据存储层
 * 
 * 使用说明：
 * 1. 将 {{MODULE_NAME}} 替换为实际模块名
 * 2. 将 {{ModelName}} 替换为模型类名（如 MyFeatureItem）
 * 3. 在 src/db/models.js 中创建对应的模型类
 */

const { {{ ModelName }} } = require('../../src/db/models');
const dbService = require('../../src/db/database');

// 初始化数据库
dbService.initialize();

// ==================== 基础 CRUD 操作 ====================

/**
 * 获取所有项目
 */
function getAll() {
    try {
        const items = {{ ModelName }
    }.findAll();
    // 转换字段名以保持向后兼容（如需要）
    return items.map(item => ({
        id: item.id,
        name: item.name,
        // ... 其他字段映射
        createdAt: item.created_at,
        updatedAt: item.updated_at
    }));
} catch (e) {
    console.error('❌ 读取数据失败:', e.message);
    return [];
}
}

/**
 * 根据 ID 获取单个项目
 */
function getById(id) {
    try {
        const item = {{ ModelName }
    }.findById(id);
    if (!item) return null;

    return {
        id: item.id,
        name: item.name,
        // ... 其他字段映射
        createdAt: item.created_at,
        updatedAt: item.updated_at
    };
} catch (e) {
    console.error('❌ 获取数据失败:', e.message);
    return null;
}
}

/**
 * 创建新项目
 * @param {Object} data - { name, ...其他字段 }
 */
function create(data) {
    const id = '{{prefix}}_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
    const now = new Date().toISOString();

    const newItem = {
        id,
        name: data.name,
        // ... 其他字段
        created_at: now,
        updated_at: now
    };

    { { ModelName } }.create(newItem);

    return {
        id: newItem.id,
        name: newItem.name,
        createdAt: newItem.created_at,
        updatedAt: newItem.updated_at
    };
}

/**
 * 更新项目
 */
function update(id, updates) {
    try {
        const item = {{ ModelName }
    }.findById(id);
    if (!item) return null;

    const updateData = {
        updated_at: new Date().toISOString()
    };

    // 只更新提供的字段
    if (updates.name !== undefined) updateData.name = updates.name;
    // ... 其他可更新字段

    { { ModelName } }.update(id, updateData);

    // 返回更新后的数据
    return getById(id);
} catch (e) {
    console.error('❌ 更新数据失败:', e.message);
    return null;
}
}

/**
 * 删除项目
 */
function deleteItem(id) {
    try {
        return {{ ModelName }
    }.delete (id);
} catch (e) {
    console.error('❌ 删除数据失败:', e.message);
    return false;
}
}

/**
 * 清空所有数据
 */
function clear() {
    try {
        { { ModelName } }.truncate();
        return true;
    } catch (e) {
        console.error('❌ 清空数据失败:', e.message);
        return false;
    }
}

/**
 * 批量保存（用于导入）
 */
function saveAll(items) {
    try {
        const db = dbService.getDatabase();

        const transaction = db.transaction(() => {
            { { ModelName } }.truncate();

            items.forEach(item => {
                { { ModelName } }.create({
                    id: item.id,
                    name: item.name,
                    // ... 其他字段
                    created_at: item.createdAt || new Date().toISOString(),
                    updated_at: item.updatedAt || new Date().toISOString()
                });
            });
        });

        transaction();
        return true;
    } catch (e) {
        console.error('❌ 批量保存失败:', e.message);
        return false;
    }
}

module.exports = {
    getAll,
    getById,
    create,
    update,
    delete: deleteItem,
    clear,
    saveAll
};
