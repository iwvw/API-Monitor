const express = require('express');
const router = express.Router();
const { CronTask, CronLog } = require('./models');
const cronService = require('./service');

// Initialize scheduler
cronService.initialize();

// GET tasks
router.get('/tasks', (req, res) => {
    try {
        const tasks = CronTask.findAll();
        // Sort by created_at desc
        tasks.sort((a, b) => b.created_at - a.created_at);
        res.json({ success: true, data: tasks });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

// POST task
router.post('/tasks', (req, res) => {
    try {
        const task = CronTask.createTask(req.body);
        cronService.reloadTask(task.id);
        res.json({ success: true, data: task });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

// PUT task
router.put('/tasks/:id', (req, res) => {
    try {
        const task = CronTask.updateTask(req.params.id, req.body);
        cronService.reloadTask(task.id);
        res.json({ success: true, data: task });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

// DELETE task
router.delete('/tasks/:id', (req, res) => {
    try {
        const task = CronTask.findById(req.params.id);
        if (task) {
            CronTask.delete(req.params.id);
            cronService.reloadTask(req.params.id);
        }
        res.json({ success: true });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

// POST run task manually
router.post('/tasks/:id/run', async (req, res) => {
    try {
        // Run in background
        cronService.executeTask(req.params.id);
        res.json({ success: true, message: 'Task execution started' });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

// GET logs
router.get('/logs', (req, res) => {
    try {
        const taskId = req.query.task_id;
        let logs;
        if (taskId) {
            logs = CronLog.getLogsByTask(taskId);
        } else {
            logs = CronLog.getAllLogs();
        }
        res.json({ success: true, data: logs });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

// DELETE logs (cleanup)
router.delete('/logs', (req, res) => {
    try {
        if (req.query.all === 'true') {
             CronLog.clearAllLogs();
        } else {
             const days = req.query.days ? parseInt(req.query.days) : 7;
             CronLog.cleanLogs(days);
        }
        res.json({ success: true });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

module.exports = router;
