const express = require('express');
const router = express.Router();
const fileBoxService = require('./service');
const { requireAuth } = require('../../src/middleware/auth');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('FileBox');

// Public route to get info by code (no auth needed usually, or maybe restrictive?)
// For now, let's keep it open as "FileCodeBox" implies anyone with code can access.
router.get('/retrieve/:code', (req, res) => {
    try {
        const { code } = req.params;
        const entry = fileBoxService.getEntry(code);

        if (!entry) {
            return res.status(404).json({ success: false, error: '取件码无效或已过期' });
        }

        // Do NOT increment access count yet, only on actual download/view
        // Just return metadata
        const { path, filename, ...metadata } = entry;
        res.json({ success: true, data: metadata });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// Download/View file
router.get('/download/:code', (req, res) => {
    try {
        const { code } = req.params;
        const entry = fileBoxService.getEntry(code);

        if (!entry) {
            return res.status(404).send('File not found or expired');
        }

        // Mark accessed
        fileBoxService.accessEntry(code);

        if (entry.type === 'text') {
            res.setHeader('Content-Type', 'text/plain; charset=utf-8');
            res.send(entry.content);
        } else {
            res.download(entry.path, entry.originalName);
        }
    } catch (error) {
        res.status(500).send(error.message);
    }
});

// Upload/Create (Auth required strictly?)
// Usually FileCodeBox allows anonymous upload. 
// We will allow anonymous upload for now to match "FileCodeBox" features.
router.post('/share', requireAuth, async (req, res) => {
    try {
        const { type, text, expiry, burn_after_reading } = req.body;
        const expiryHours = parseFloat(expiry) || 24;
        const burn = burn_after_reading === 'true' || burn_after_reading === true;

        let entry;

        if (type === 'text') {
            if (!text) return res.status(400).json({ success: false, error: 'Text content missing' });
            entry = fileBoxService.addText(text, expiryHours, burn);
        } else {
            if (!req.files || !req.files.file) {
                return res.status(400).json({ success: false, error: 'No file uploaded' });
            }
            entry = await fileBoxService.addFile(req.files.file, expiryHours, burn);
        }

        res.json({ success: true, code: entry.code, expiry: entry.expiry });
    } catch (error) {
        logger.error('Share failed:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

// Admin/History route (Require Auth)
router.get('/history', requireAuth, (req, res) => {
    // Ideally this should be protected. 
    // Assuming backend mounts this under /api/filebox, and we might add requireAuth middleware in main route file if needed.
    // For now, let's assume the frontend handling this part will require auth cookie.
    try {
        const list = fileBoxService.getAll();
        res.json({ success: true, data: list });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// Delete
router.delete('/:code', requireAuth, (req, res) => {
    try {
        const { code } = req.params;
        fileBoxService.deleteEntry(code);
        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

module.exports = router;
