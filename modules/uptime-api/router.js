/**
 * Uptime API 路由
 */

const express = require('express');
const router = express.Router();
const publicRouter = express.Router();
const storage = require('./storage');
const monitorService = require('./monitor-service');
const probeRegistry = require('./adapters/probe-registry');
const auditService = require('../../src/services/audit-service');
const eventBus = require('../../src/services/toolbox-event-bus');

function escapeSvgText(value) {
    return String(value ?? '')
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

function monitorKey(monitor = {}) {
    return [
        monitor.name,
        monitor.type || 'http',
        monitor.url || '',
        monitor.hostname || '',
        monitor.port || 0,
    ].join('|').toLowerCase();
}

function buildUptimeExport() {
    return {
        type: 'api-monitor-uptime-export',
        version: 1,
        exportedAt: new Date().toISOString(),
        monitors: storage.getAll(),
        statusPages: storage.listStatusPages(),
        maintenanceWindows: storage.listMaintenanceWindows(),
    };
}

function previewUptimeImport(payload = {}) {
    const existingMonitors = new Map(storage.getAll().map(monitor => [monitorKey(monitor), monitor]));
    const existingPages = new Map(storage.listStatusPages().map(page => [page.slug, page]));
    const existingMaintenance = new Map(storage.listMaintenanceWindows().map(item => [item.title, item]));

    const monitors = Array.isArray(payload.monitors) ? payload.monitors : [];
    const statusPages = Array.isArray(payload.statusPages) ? payload.statusPages : [];
    const maintenanceWindows = Array.isArray(payload.maintenanceWindows) ? payload.maintenanceWindows : [];

    return {
        monitors: monitors.map(item => ({
            name: item.name,
            type: item.type,
            action: existingMonitors.has(monitorKey(item)) ? 'update' : 'create',
        })),
        statusPages: statusPages.map(item => ({
            title: item.title,
            slug: item.slug,
            action: existingPages.has(item.slug) ? 'update' : 'create',
        })),
        maintenanceWindows: maintenanceWindows.map(item => ({
            title: item.title,
            action: existingMaintenance.has(item.title) ? 'update' : 'create',
        })),
        counts: {
            monitors: monitors.length,
            statusPages: statusPages.length,
            maintenanceWindows: maintenanceWindows.length,
        },
    };
}

// POST /api/uptime/push/:token
publicRouter.post('/push/:token', (req, res) => {
    try {
        const result = monitorService.recordPush(req.params.token, req.body || {}, req);
        if (!result) {
            return res.status(404).json({ success: false, error: 'Invalid push token' });
        }
        res.json({
            success: true,
            data: {
                monitorId: result.monitor.id,
                receivedAt: result.beat.time,
            },
        });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

// GET /api/uptime/public/status-pages/:slug
publicRouter.get('/public/status-pages/:slug', (req, res) => {
    const page = storage.getPublicStatusPage(req.params.slug);
    if (!page) return res.status(404).json({ success: false, error: 'Not found' });
    res.set('Cache-Control', `public, max-age=${page.cacheSeconds || 300}`);
    res.json({ success: true, data: page });
});

// GET /api/uptime/public/badge/:monitorId
publicRouter.get('/public/badge/:monitorId', (req, res) => {
    const monitor = storage.getById(parseInt(req.params.monitorId));
    if (!monitor) return res.status(404).type('text/plain').send('Not found');

    const state = monitorService.getMonitorState(monitor.id)?.state || 'unknown';
    const colorMap = {
        up: '#16a34a',
        down: '#dc2626',
        pending_down: '#d97706',
        pending_up: '#d97706',
        maintenance: '#2563eb',
        paused: '#64748b',
        unknown: '#64748b',
    };
    const label = escapeSvgText(monitor.name || `Monitor ${monitor.id}`);
    const value = escapeSvgText(state);
    const color = colorMap[state] || colorMap.unknown;
    const labelWidth = Math.max(80, Math.min(220, label.length * 7 + 18));
    const valueWidth = Math.max(58, value.length * 7 + 18);
    const width = labelWidth + valueWidth;

    res.set('Cache-Control', 'public, max-age=60');
    res.type('image/svg+xml').send(
        `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="20" role="img" aria-label="${label}: ${value}">` +
        `<linearGradient id="s" x2="0" y2="100%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>` +
        `<rect rx="3" width="${width}" height="20" fill="#555"/>` +
        `<rect rx="3" x="${labelWidth}" width="${valueWidth}" height="20" fill="${color}"/>` +
        `<path fill="${color}" d="M${labelWidth} 0h4v20h-4z"/>` +
        `<rect rx="3" width="${width}" height="20" fill="url(#s)"/>` +
        `<g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,sans-serif" font-size="11">` +
        `<text x="${labelWidth / 2}" y="15">${label}</text>` +
        `<text x="${labelWidth + valueWidth / 2}" y="15">${value}</text>` +
        `</g></svg>`
    );
});

// GET /api/uptime/summary
router.get('/summary', (req, res) => {
    const monitors = storage.getAll();
    const stats = { total: monitors.length, up: 0, down: 0, pending: 0, paused: 0, unknown: 0 };
    for (const monitor of monitors) {
        if (!monitor.active) {
            stats.paused++;
            continue;
        }
        const state = monitorService.getMonitorState(monitor.id);
        if (state?.state === 'up') stats.up++;
        else if (state?.state === 'down') stats.down++;
        else if (state?.state?.startsWith('pending')) stats.pending++;
        else stats.unknown++;
    }
    res.json({ success: true, data: stats });
});

// GET /api/uptime/status-pages
router.get('/status-pages', (req, res) => {
    res.json({ success: true, data: storage.listStatusPages() });
});

// POST /api/uptime/status-pages
router.post('/status-pages', (req, res) => {
    try {
        const page = storage.createStatusPage(req.body || {});
        res.json({ success: true, data: page });
    } catch (e) {
        res.status(400).json({ success: false, error: e.message });
    }
});

// PUT /api/uptime/status-pages/:id
router.put('/status-pages/:id', (req, res) => {
    try {
        const page = storage.updateStatusPage(parseInt(req.params.id), req.body || {});
        if (!page) return res.status(404).json({ success: false, error: 'Not found' });
        res.json({ success: true, data: page });
    } catch (e) {
        res.status(400).json({ success: false, error: e.message });
    }
});

// DELETE /api/uptime/status-pages/:id
router.delete('/status-pages/:id', (req, res) => {
    const ok = storage.deleteStatusPage(parseInt(req.params.id));
    res.status(ok ? 200 : 404).json(ok ? { success: true } : { success: false, error: 'Not found' });
});

// GET /api/uptime/public/status-pages/:slug
router.get('/public/status-pages/:slug', (req, res) => {
    const page = storage.getPublicStatusPage(req.params.slug);
    if (!page) return res.status(404).json({ success: false, error: 'Not found' });
    res.set('Cache-Control', `public, max-age=${page.cacheSeconds || 300}`);
    res.json({ success: true, data: page });
});

// GET /api/uptime/maintenance
router.get('/maintenance', (req, res) => {
    res.json({ success: true, data: storage.listMaintenanceWindows() });
});

// POST /api/uptime/maintenance
router.post('/maintenance', (req, res) => {
    try {
        const item = storage.createMaintenanceWindow(req.body || {});
        res.json({ success: true, data: item });
    } catch (e) {
        res.status(400).json({ success: false, error: e.message });
    }
});

// PUT /api/uptime/maintenance/:id
router.put('/maintenance/:id', (req, res) => {
    try {
        const item = storage.updateMaintenanceWindow(parseInt(req.params.id), req.body || {});
        if (!item) return res.status(404).json({ success: false, error: 'Not found' });
        res.json({ success: true, data: item });
    } catch (e) {
        res.status(400).json({ success: false, error: e.message });
    }
});

// DELETE /api/uptime/maintenance/:id
router.delete('/maintenance/:id', (req, res) => {
    const ok = storage.deleteMaintenanceWindow(parseInt(req.params.id));
    res.status(ok ? 200 : 404).json(ok ? { success: true } : { success: false, error: 'Not found' });
});

// GET /api/uptime/export
router.get('/export', (req, res) => {
    try {
        const data = buildUptimeExport();
        auditService.record({
            req,
            module: 'uptime',
            action: 'exported',
            resourceType: 'uptime_export',
            summary: `Exported uptime configuration (${data.monitors.length} monitors)`,
            metadata: {
                monitors: data.monitors.length,
                statusPages: data.statusPages.length,
                maintenanceWindows: data.maintenanceWindows.length,
            },
        });
        res.json({ success: true, data });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

// POST /api/uptime/import/preview
router.post('/import/preview', (req, res) => {
    try {
        const payload = req.body?.data || req.body || {};
        if (payload.type && payload.type !== 'api-monitor-uptime-export') {
            return res.status(400).json({ success: false, error: 'Invalid uptime export payload' });
        }
        res.json({ success: true, data: previewUptimeImport(payload) });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

// POST /api/uptime/import
router.post('/import', (req, res) => {
    try {
        const payload = req.body?.data || req.body || {};
        if (payload.type && payload.type !== 'api-monitor-uptime-export') {
            return res.status(400).json({ success: false, error: 'Invalid uptime export payload' });
        }

        const existingMonitors = new Map(storage.getAll().map(monitor => [monitorKey(monitor), monitor]));
        const idMap = new Map();
        let monitorsChanged = 0;

        for (const monitor of Array.isArray(payload.monitors) ? payload.monitors : []) {
            const existing = existingMonitors.get(monitorKey(monitor));
            if (existing) {
                storage.update(existing.id, monitor);
                idMap.set(String(monitor.id), existing.id);
            } else {
                const created = storage.create(monitor);
                idMap.set(String(monitor.id), created.id);
                if (created.active) monitorService.startMonitor(created);
            }
            monitorsChanged++;
        }

        const existingPages = new Map(storage.listStatusPages().map(page => [page.slug, page]));
        let pagesChanged = 0;
        for (const page of Array.isArray(payload.statusPages) ? payload.statusPages : []) {
            const monitorIds = (page.monitorIds || []).map(id => idMap.get(String(id)) || id).filter(Boolean);
            const existing = existingPages.get(page.slug);
            if (existing) {
                storage.updateStatusPage(existing.id, { ...page, monitorIds });
            } else {
                storage.createStatusPage({ ...page, monitorIds });
            }
            pagesChanged++;
        }

        const existingMaintenance = new Map(storage.listMaintenanceWindows().map(item => [item.title, item]));
        let maintenanceChanged = 0;
        for (const item of Array.isArray(payload.maintenanceWindows) ? payload.maintenanceWindows : []) {
            const targets = (item.targets || []).map(target => {
                if (target?.type === 'monitor' && target.id !== null && target.id !== undefined) {
                    return { ...target, id: idMap.get(String(target.id)) || target.id };
                }
                return target;
            });
            const existing = existingMaintenance.get(item.title);
            if (existing) {
                storage.updateMaintenanceWindow(existing.id, { ...item, targets });
            } else {
                storage.createMaintenanceWindow({ ...item, targets });
            }
            maintenanceChanged++;
        }

        auditService.record({
            req,
            module: 'uptime',
            action: 'imported',
            resourceType: 'uptime_export',
            summary: `Imported uptime configuration`,
            metadata: { monitorsChanged, pagesChanged, maintenanceChanged },
        });
        res.json({ success: true, data: { monitorsChanged, pagesChanged, maintenanceChanged } });
    } catch (e) {
        res.status(500).json({ success: false, error: e.message });
    }
});

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
        auditService.record({
            req,
            module: 'uptime',
            action: 'created',
            resourceType: 'uptime_monitor',
            resourceId: newMonitor.id,
            summary: `Created uptime monitor ${newMonitor.name}`,
            metadata: { type: newMonitor.type, target: newMonitor.url || newMonitor.hostname },
        });
        eventBus.publish('uptime.resource.created', { monitorId: newMonitor.id }, { module: 'uptime' });

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
        auditService.record({
            req,
            module: 'uptime',
            action: 'updated',
            resourceType: 'uptime_monitor',
            resourceId: id,
            summary: `Updated uptime monitor ${updated.name}`,
            metadata: { fields: Object.keys(data) },
        });

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
    if (success) {
        auditService.record({
            req,
            module: 'uptime',
            action: 'deleted',
            resourceType: 'uptime_monitor',
            resourceId: id,
            summary: `Deleted uptime monitor ${id}`,
        });
        eventBus.publish('uptime.resource.deleted', { monitorId: id }, { module: 'uptime' });
        res.json({ success: true });
    }
    else res.status(404).json({ error: 'Not found' });
});

// POST /api/uptime/monitors/:id/clone
router.post('/monitors/:id/clone', (req, res) => {
    try {
        const id = parseInt(req.params.id);
        const monitor = storage.getById(id);
        if (!monitor) return res.status(404).json({ error: 'Not found' });
        const cloneData = {
            ...monitor,
            name: req.body?.name || `${monitor.name} Copy`,
            active: false,
        };
        delete cloneData.id;
        delete cloneData.pushToken;
        delete cloneData.push_token;
        const cloned = storage.create({
            ...cloneData,
        });
        res.json(cloned);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// POST /api/uptime/monitors/:id/test
router.post('/monitors/:id/test', async (req, res) => {
    try {
        const id = parseInt(req.params.id);
        const monitor = { ...storage.getById(id), ...(req.body || {}) };
        if (!monitor?.id && !req.body?.type) return res.status(404).json({ error: 'Not found' });
        const started = Date.now();
        const result = await probeRegistry.check(monitor);
        res.json({ success: true, data: { ...result, durationMs: Date.now() - started } });
    } catch (e) {
        res.status(400).json({ success: false, error: e.message });
    }
});

// POST /api/uptime/monitors/:id/check-now
router.post('/monitors/:id/check-now', async (req, res) => {
    try {
        const id = parseInt(req.params.id);
        const monitor = storage.getById(id);
        if (!monitor) return res.status(404).json({ error: 'Not found' });
        const beat = await monitorService.check(monitor);
        res.json({ success: true, data: beat });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
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

// POST /api/uptime/batch
router.post('/batch', (req, res) => {
    try {
        const { action, ids } = req.body || {};
        if (!Array.isArray(ids) || ids.length === 0) {
            return res.status(400).json({ error: 'IDs array is required' });
        }

        let count = 0;
        for (const id of ids.map(Number)) {
            const monitor = storage.getById(id);
            if (!monitor) continue;
            if (action === 'pause') {
                storage.update(id, { active: false });
                monitorService.stopMonitor(id);
                count++;
            } else if (action === 'resume') {
                const updated = storage.update(id, { active: true });
                monitorService.startMonitor(updated);
                count++;
            } else if (action === 'delete') {
                monitorService.stopMonitor(id);
                if (storage.delete(id)) count++;
            }
        }

        res.json({ success: true, count });
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

router.publicRouter = publicRouter;

module.exports = router;
