/**
 * 凭据管理模块路由
 */

const express = require('express');
const router = express.Router();
const { ServerCredential } = require('../../src/db/models');

// 获取所有凭据
router.get('/', (req, res) => {
    try {
        const credentials = ServerCredential.getAll();
        res.json({
            success: true,
            data: credentials
        });
    } catch (error) {
        console.error('获取凭据失败:', error);
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

// 获取默认凭据
router.get('/default', (req, res) => {
    try {
        const credential = ServerCredential.getDefault();
        res.json({
            success: true,
            data: credential
        });
    } catch (error) {
        console.error('获取默认凭据失败:', error);
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

// 添加凭据
router.post('/', (req, res) => {
    try {
        const { name, username, password } = req.body;

        if (!name || !username) {
            return res.status(400).json({
                success: false,
                error: '缺少必填字段'
            });
        }

        const credential = ServerCredential.create({ name, username, password });

        res.json({
            success: true,
            message: '凭据添加成功',
            data: credential
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

// 设置默认凭据
router.put('/:id/default', (req, res) => {
    try {
        const { id } = req.params;
        const success = ServerCredential.setDefault(id);

        if (success) {
            res.json({
                success: true,
                message: '已设置为默认凭据'
            });
        } else {
            res.status(404).json({
                success: false,
                error: '凭据不存在'
            });
        }
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

// 删除凭据
router.delete('/:id', (req, res) => {
    try {
        const { id } = req.params;
        const success = ServerCredential.delete(id);

        if (success) {
            res.json({
                success: true,
                message: '凭据删除成功'
            });
        } else {
            res.status(404).json({
                success: false,
                error: '凭据不存在'
            });
        }
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

module.exports = router;
