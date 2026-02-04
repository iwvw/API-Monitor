const fs = require('fs');
const path = require('path');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('FileBox');

class FileBoxService {
    constructor() {
        // Determine data directory relative to this service file
        // this file is in modules/filebox-api/service.js
        // so ../../data goes to root/data
        this.dataDir = path.resolve(__dirname, '../../data/filebox');
        this.uploadsDir = path.join(this.dataDir, 'uploads');
        this.metadataFile = path.join(this.dataDir, 'metadata.json');

        this.ensureDirs();
        this.fileStore = this.loadMetadata();
    }

    ensureDirs() {
        if (!fs.existsSync(this.dataDir)) {
            fs.mkdirSync(this.dataDir, { recursive: true });
        }
        if (!fs.existsSync(this.uploadsDir)) {
            fs.mkdirSync(this.uploadsDir, { recursive: true });
        }
    }

    loadMetadata() {
        try {
            if (fs.existsSync(this.metadataFile)) {
                return JSON.parse(fs.readFileSync(this.metadataFile, 'utf8'));
            }
        } catch (e) {
            logger.error('Failed to load metadata:', e);
        }
        return {};
    }

    saveMetadata() {
        try {
            fs.writeFileSync(this.metadataFile, JSON.stringify(this.fileStore, null, 2));
        } catch (e) {
            logger.error('Failed to save metadata:', e);
        }
    }

    generateCode(length = 5) {
        const chars = '23456789ABCDEFGHJKLMNPQRSTUVWXYZ';
        let code = '';
        do {
            code = '';
            for (let i = 0; i < length; i++) {
                code += chars.charAt(Math.floor(Math.random() * chars.length));
            }
        } while (this.fileStore[code]);
        return code;
    }

    /**
     * Add a text entry
     */
    addText(content, expiryHours = 24, burnAfterReading = false) {
        const code = this.generateCode();
        const now = Date.now();
        const expiry = now + (expiryHours * 60 * 60 * 1000);

        this.fileStore[code] = {
            code,
            type: 'text',
            content,
            filename: `text_${code}.txt`,
            createdAt: now,
            expiry,
            burnAfterReading: !!burnAfterReading,
            downloads: 0
        };
        this.saveMetadata();
        return this.fileStore[code];
    }

    /**
     * Add a file entry (express-fileupload object)
     */
    async addFile(fileObj, expiryHours = 24, burnAfterReading = false) {
        const code = this.generateCode();
        const now = Date.now();
        const expiry = now + (expiryHours * 60 * 60 * 1000);

        // Unique filename
        const uniqueSuffix = Date.now() + '-' + Math.round(Math.random() * 1E9);
        const saveFilename = `${uniqueSuffix}-${fileObj.name}`;
        const savePath = path.join(this.uploadsDir, saveFilename);

        // Save file
        await fileObj.mv(savePath);

        this.fileStore[code] = {
            code,
            type: 'file',
            originalName: fileObj.name,
            filename: saveFilename,
            path: savePath,
            mimetype: fileObj.mimetype,
            size: fileObj.size,
            createdAt: now,
            expiry,
            burnAfterReading: !!burnAfterReading,
            downloads: 0
        };
        this.saveMetadata();
        return this.fileStore[code];
    }

    /**
     * Retrieve entry by code
     */
    getEntry(code) {
        if (!code) return null;
        const entry = this.fileStore[code.toUpperCase()];
        if (!entry) return null;

        // Check expiry
        if (Date.now() > entry.expiry) {
            this.deleteEntry(code.toUpperCase());
            return null;
        }

        return entry;
    }

    /**
     * Mark as downloaded/accessed and handle burn-after-reading
     */
    accessEntry(code) {
        if (!code) return;
        const entry = this.fileStore[code.toUpperCase()];
        if (!entry) return;

        entry.downloads = (entry.downloads || 0) + 1;

        if (entry.burnAfterReading) {
            // 阅后即焚：直接删除，无需先保存 downloads（deleteEntry 内部会调用 saveMetadata）
            this.deleteEntry(code.toUpperCase());
        } else {
            // 普通文件：仅保存下载计数
            this.saveMetadata();
        }
    }

    /**
     * Delete entry and file
     */
    deleteEntry(code) {
        const entry = this.fileStore[code];
        if (entry) {
            if (entry.type === 'file' && entry.path) {
                try {
                    if (fs.existsSync(entry.path)) {
                        fs.unlinkSync(entry.path);
                    }
                } catch (e) {
                    logger.error(`Failed to delete file: ${entry.path}`, e);
                }
            }
            delete this.fileStore[code];
            this.saveMetadata();
        }
    }

    getAll() {
        this.cleanupExpired();
        return Object.values(this.fileStore).sort((a, b) => b.createdAt - a.createdAt);
    }

    cleanupExpired() {
        const now = Date.now();
        Object.keys(this.fileStore).forEach(code => {
            if (now > this.fileStore[code].expiry) {
                this.deleteEntry(code);
            }
        });
    }
}

module.exports = new FileBoxService();
