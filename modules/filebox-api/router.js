const express = require('express');
const router = express.Router();
const fileBoxService = require('./service');
const { requireAuth } = require('../../src/middleware/auth');
const { createLogger } = require('../../src/utils/logger');
const auditService = require('../../src/services/audit-service');
const eventBus = require('../../src/services/toolbox-event-bus');
const jobScheduler = require('../../src/services/job-scheduler');

const logger = createLogger('FileBox');

function requestMeta(req) {
    return {
        ip: req.ip || req.headers['x-forwarded-for']?.split(',')?.[0]?.trim() || req.socket?.remoteAddress,
        userAgent: req.get ? req.get('user-agent') : req.headers['user-agent'],
    };
}

function getAccessPassword(req) {
    return req.query.password || req.headers['x-filebox-password'] || req.body?.password || '';
}

jobScheduler.register(
    'filebox.cleanup',
    () => ({ deleted: fileBoxService.cleanupExpired() }),
    { intervalMs: 60 * 60 * 1000, jitterMs: 5 * 60 * 1000 }
);

function sendEntryMetadata(req, res) {
    try {
        const { code } = req.params;
        const entry = fileBoxService.getEntry(code);

        if (!entry) {
            return res.status(404).json({ success: false, error: '取件码无效或已过期' });
        }

        // Do NOT increment access count yet, only on actual download/view
        // Just return metadata
        const { path, filename, content, accessPasswordHash: _accessPasswordHash, ...metadata } = entry;
        fileBoxService.logAccess(entry.code, 'retrieve', requestMeta(req));
        res.json({ success: true, data: metadata });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
}

function downloadEntry(req, res) {
    try {
        const { code } = req.params;
        const entry = fileBoxService.getEntry(code);

        if (!entry) {
            return res.status(404).send('File not found or expired');
        }
        if (!fileBoxService.verifyAccessPassword(entry, getAccessPassword(req))) {
            return res.status(403).send('Password required or invalid');
        }

        if (entry.type === 'text') {
            res.setHeader('Content-Type', 'text/plain; charset=utf-8');
            fileBoxService.accessEntry(code, requestMeta(req));
            res.send(entry.content);
        } else {
            res.download(entry.path, entry.originalName, err => {
                if (!err) {
                    fileBoxService.accessEntry(code, requestMeta(req));
                } else {
                    logger.warn('File download failed:', err.message);
                }
            });
        }
    } catch (error) {
        res.status(500).send(error.message);
    }
}

async function createShare(req, res) {
    try {
        const { type, text, expiry, burn_after_reading, max_downloads, access_password } = req.body;
        const settings = fileBoxService.getSettings();
        const expiryHours = parseFloat(expiry) || settings.default_expiry_hours;
        const burn = burn_after_reading === 'true' || burn_after_reading === true;
        const maxDownloads = Math.max(0, parseInt(max_downloads, 10) || 0);
        const accessPassword = access_password || req.body.password || '';

        let entry;

        if (type === 'text') {
            if (!text) return res.status(400).json({ success: false, error: 'Text content missing' });
            entry = fileBoxService.addText(text, expiryHours, burn, maxDownloads, accessPassword);
        } else {
            if (!req.files || !req.files.file) {
                return res.status(400).json({ success: false, error: 'No file uploaded' });
            }
            entry = await fileBoxService.addFile(req.files.file, expiryHours, burn, maxDownloads, accessPassword);
        }

        auditService.record({
            req,
            module: 'filebox',
            action: 'created',
            resourceType: entry.type === 'text' ? 'filebox_text' : 'filebox_file',
            resourceId: entry.code,
            summary: `Created Filebox share ${entry.code}`,
            metadata: { type: entry.type, filename: entry.originalName || entry.filename, expiry: entry.expiry },
        });
        eventBus.publish('filebox.resource.created', { code: entry.code, type: entry.type }, { module: 'filebox' });

        res.json({ success: true, code: entry.code, data: entry, expiry: entry.expiry });
    } catch (error) {
        logger.error('Share failed:', error);
        res.status(500).json({ success: false, error: error.message });
    }
}

function listShares(req, res) {
    try {
        const list = fileBoxService.getAll();
        res.json({ success: true, data: list });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
}

function deleteShare(req, res) {
    try {
        const { code } = req.params;
        const entry = fileBoxService.getEntry(code, { includeExpired: true });
        fileBoxService.deleteEntry(code);
        auditService.record({
            req,
            module: 'filebox',
            action: 'deleted',
            resourceType: entry?.type === 'text' ? 'filebox_text' : 'filebox_file',
            resourceId: code,
            summary: `Deleted Filebox share ${code}`,
            metadata: { type: entry?.type, filename: entry?.originalName || entry?.filename },
        });
        eventBus.publish('filebox.resource.deleted', { code }, { module: 'filebox' });
        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
}

function verifyPublicShare(req, res) {
    try {
        const entry = fileBoxService.getEntry(req.params.code);
        if (!entry) return res.status(404).json({ success: false, error: '取件码无效或已过期' });
        const verified = fileBoxService.verifyAccessPassword(entry, getAccessPassword(req));
        res.status(verified ? 200 : 403).json({ success: verified, requiresPassword: entry.requiresPassword });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
}

// Public route to get info by code (no auth needed usually, or maybe restrictive?)
// For now, let's keep it open as "FileCodeBox" implies anyone with code can access.
router.get('/retrieve/:code', sendEntryMetadata);
router.get('/public/:code', sendEntryMetadata);

// Download/View file
router.get('/download/:code', downloadEntry);
router.get('/public/:code/download', downloadEntry);
router.post('/public/:code/verify', verifyPublicShare);

// Upload/Create (Auth required strictly?)
// Usually FileCodeBox allows anonymous upload.
// We will allow anonymous upload for now to match "FileCodeBox" features.
router.post('/share', requireAuth, createShare);
router.post('/shares', requireAuth, createShare);

router.get('/access-logs', requireAuth, (req, res) => {
    try {
        const logs = fileBoxService.getAccessLogs(req.query.code, req.query.limit);
        res.json({ success: true, data: logs });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

router.get('/settings', requireAuth, (req, res) => {
    res.json({ success: true, data: fileBoxService.getSettings() });
});

router.put('/settings', requireAuth, (req, res) => {
    const settings = fileBoxService.updateSettings(req.body || {});
    auditService.record({
        req,
        module: 'filebox',
        action: 'settings.updated',
        resourceType: 'filebox_settings',
        summary: 'Updated Filebox settings',
        metadata: settings,
    });
    res.json({ success: true, data: settings });
});

router.post('/jobs/cleanup', requireAuth, async (req, res) => {
    try {
        const result = await jobScheduler.run('filebox.cleanup', { req });
        auditService.record({
            req,
            module: 'filebox',
            action: 'cleanup',
            resourceType: 'filebox_job',
            summary: 'Ran Filebox cleanup job',
            metadata: result,
        });
        res.json({ success: true, data: result });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// Admin/History route (Require Auth)
router.get('/history', requireAuth, listShares);
router.get('/shares', requireAuth, listShares);
router.get('/shares/:code', requireAuth, sendEntryMetadata);

// Delete
router.delete('/shares/:code', requireAuth, deleteShare);
router.delete('/:code', requireAuth, deleteShare);

module.exports = router;
