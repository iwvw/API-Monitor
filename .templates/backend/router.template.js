/**
 * {{MODULE_NAME}} 模块 - API 路由
 * 
 * 使用说明：
 * 1. 将 {{MODULE_NAME}} 替换为实际模块名（如 my-feature）
 * 2. 将 {{ModuleName}} 替换为驼峰式命名（如 myFeature）
 * 3. 将 {{API_PREFIX}} 替换为 API 前缀（如 /api/my-feature）
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
// const service = require('./service'); // 如需外部 API 调用，取消注释

// ==================== 基础 CRUD 操作 ====================

/**
 * 获取所有项目列表
 * GET {{API_PREFIX}}/items
 */
router.get('/items', async (req, res) => {
    try {
        const items = storage.getAll();
        res.json(items);
    } catch (e) {
        console.error('❌ 获取列表失败:', e.message);
        res.status(500).json({ error: e.message });
    }
});

/**
 * 获取单个项目
 * GET {{API_PREFIX}}/items/:id
 */
router.get('/items/:id', async (req, res) => {
    try {
        const { id } = req.params;
        const item = storage.getById(id);

        if (!item) {
            return res.status(404).json({ error: '项目不存在' });
        }

        res.json(item);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 创建新项目
 * POST {{API_PREFIX}}/items
 */
router.post('/items', async (req, res) => {
    try {
        const { name, ...otherFields } = req.body;

        // 验证必填字段
        if (!name) {
            return res.status(400).json({ error: '名称不能为空' });
        }

        const newItem = storage.create({ name, ...otherFields });

        res.json({
            success: true,
            item: newItem
        });
    } catch (e) {
        console.error('❌ 创建失败:', e.message);
        res.status(500).json({ error: e.message });
    }
});

/**
 * 更新项目
 * PUT {{API_PREFIX}}/items/:id
 */
router.put('/items/:id', async (req, res) => {
    try {
        const { id } = req.params;
        const updates = req.body;

        const updated = storage.update(id, updates);

        if (!updated) {
            return res.status(404).json({ error: '项目不存在' });
        }

        res.json({
            success: true,
            item: updated
        });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 删除项目
 * DELETE {{API_PREFIX}}/items/:id
 */
router.delete('/items/:id', async (req, res) => {
    try {
        const { id } = req.params;
        const deleted = storage.delete(id);

        if (!deleted) {
            return res.status(404).json({ error: '项目不存在' });
        }

        res.json({ success: true });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// ==================== 批量操作 ====================

/**
 * 批量删除
 * POST {{API_PREFIX}}/items/batch-delete
 */
router.post('/items/batch-delete', async (req, res) => {
    try {
        const { ids } = req.body;

        if (!ids || !Array.isArray(ids) || ids.length === 0) {
            return res.status(400).json({ error: '请提供要删除的 ID 列表' });
        }

        let successCount = 0;
        let failedCount = 0;

        for (const id of ids) {
            const deleted = storage.delete(id);
            if (deleted) {
                successCount++;
            } else {
                failedCount++;
            }
        }

        res.json({
            success: true,
            deleted: successCount,
            failed: failedCount
        });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// ==================== 导出导入 ====================

/**
 * 导出数据
 * GET {{API_PREFIX}}/export
 */
router.get('/export', (req, res) => {
    try {
        const items = storage.getAll();
        res.json({
            version: '1.0',
            exportTime: new Date().toISOString(),
            data: items
        });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 导入数据
 * POST {{API_PREFIX}}/import
 */
router.post('/import', async (req, res) => {
    try {
        const { data, overwrite = false } = req.body;

        if (!data || !Array.isArray(data)) {
            return res.status(400).json({ error: '无效的导入数据格式' });
        }

        if (overwrite) {
            storage.clear();
        }

        let imported = 0;
        for (const item of data) {
            storage.create(item);
            imported++;
        }

        res.json({ success: true, imported });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

module.exports = router;
