/**
 * Uptime API 路由
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const monitorService = require('./monitor-service');

// GET /api/uptime/monitors
router.get('/monitors', (req, res) => {
    const monitors = storage.getAll();
    // 如有需要可附加最新状态，或者由前端单独获取历史记录
    // 优化：前端通常需要每个监控项的最新心跳状态。

    const result = monitors.map(m => {
        const history = storage.getHistory(m.id, 1);
        return {
            ...m,
            lastHeartbeat: history[0] || null
        };
    });

    res.json(result);
});

// GET /api/uptime/monitors/:id/history
router.get('/monitors/:id/history', (req, res) => {
    const history = storage.getHistory(req.params.id, 60); // 最近 60 个点
    res.json(history);
});

// POST /api/uptime/monitors
router.post('/monitors', (req, res) => {
    try {
        const data = req.body;
        if (!data.name) return res.status(400).json({ error: 'Name is required' });

        const newMonitor = storage.create(data);
        monitorService.startMonitor(newMonitor);

        res.json(newMonitor);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// PUT /api/uptime/monitors/:id
router.put('/monitors/:id', (req, res) => {
    try {
        const id = parseInt(req.params.id);
        const data = req.body;

        const updated = storage.update(id, data);
        if (!updated) return res.status(404).json({ error: 'Not found' });

        // 如果间隔/URL 发生变化，重启监控；或者只是执行标准重启
        monitorService.startMonitor(updated);

        res.json(updated);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// DELETE /api/uptime/monitors/:id
router.delete('/monitors/:id', (req, res) => {
    const id = parseInt(req.params.id);
    monitorService.stopMonitor(id);
    const success = storage.delete(id);
    if (success) res.json({ success: true });
    else res.status(404).json({ error: 'Not found' });
});

// POST /api/uptime/monitors/batch-delete
router.post('/monitors/batch-delete', (req, res) => {
    try {
        const { ids } = req.body;
        if (!Array.isArray(ids) || ids.length === 0) {
            return res.status(400).json({ error: 'IDs array is required' });
        }

        // Stop each monitor in the monitor service
        for (const id of ids) {
            monitorService.stopMonitor(id);
        }

        // Delete from SQLite in chunks to prevent "too many SQL variables" SQLite limit error
        const db = require('../../src/db/database').getDatabase();
        
        const tx = db.transaction(() => {
            const chunkSize = 500;
            for (let i = 0; i < ids.length; i += chunkSize) {
                const chunk = ids.slice(i, i + chunkSize);
                const placeholders = chunk.map(() => '?').join(',');
                db.prepare(`DELETE FROM uptime_heartbeats WHERE monitor_id IN (${placeholders})`).run(...chunk);
                db.prepare(`DELETE FROM uptime_incidents WHERE monitor_id IN (${placeholders})`).run(...chunk);
                db.prepare(`DELETE FROM uptime_monitors WHERE id IN (${placeholders})`).run(...chunk);
            }
        });
        tx();

        res.json({ success: true, count: ids.length });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// POST /api/uptime/monitors/:id/toggle
router.post('/monitors/:id/toggle', (req, res) => {
    const id = parseInt(req.params.id);
    const monitor = storage.getById(id);
    if (!monitor) return res.status(404).json({ error: 'Not found' });

    const newActive = !monitor.active;
    storage.update(id, { active: newActive });

    if (newActive) monitorService.startMonitor(storage.getById(id));
    else monitorService.stopMonitor(id);

    res.json({ success: true, active: newActive });
});

// GET /api/uptime/monitors/:id/uptime?days=1
router.get('/monitors/:id/uptime', (req, res) => {
    const id = parseInt(req.params.id);
    const days = parseInt(req.query.days) || 1;
    const uptime = storage.calculateUptime(id, days);
    res.json({ monitorId: id, days, uptime });
});

// GET /api/uptime/monitors/:id/incidents
router.get('/monitors/:id/incidents', (req, res) => {
    const id = parseInt(req.params.id);
    const limit = parseInt(req.query.limit) || 20;
    const incidents = storage.getIncidents(id, limit);
    res.json(incidents);
});

// GET /api/uptime/monitors/:id/state
router.get('/monitors/:id/state', (req, res) => {
    const id = parseInt(req.params.id);
    const state = monitorService.getMonitorState(id);
    res.json(state);
});

module.exports = router;
