import axios from 'axios';

const FILEBOX_MAX_FILE_SIZE = 100 * 1024 * 1024; // 100MB

function formatSpeed(bytesPerSecond) {
    if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return '-';
    const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
    let value = bytesPerSecond;
    let idx = 0;
    while (value >= 1024 && idx < units.length - 1) {
        value /= 1024;
        idx += 1;
    }
    const fixed = value >= 100 ? 0 : value >= 10 ? 1 : 2;
    return `${value.toFixed(fixed)} ${units[idx]}`;
}

function formatEta(seconds) {
    if (!Number.isFinite(seconds) || seconds <= 0) return '-';
    if (seconds < 60) return `${Math.ceil(seconds)}秒`;
    const mins = Math.floor(seconds / 60);
    const secs = Math.ceil(seconds % 60);
    return `${mins}分${secs}秒`;
}

export const fileboxData = {
    fileboxRetrieveCode: '',
    fileboxShareType: 'file', // 'file' or 'text'
    fileboxCurrentTab: 'share', // 'share' | 'retrieve' | 'history'
    fileboxShareText: '',
    fileboxSelectedFile: null,
    fileboxExpiry: '24',
    fileboxBurnAfterReading: false,
    fileboxLoading: false,
    fileboxResult: null,
    fileboxQrCode: '',
    fileboxHistory: [],
    fileboxServerHistory: [],
    fileboxHistoryLoading: false,
    fileboxRetrievedEntry: null,
    isDragging: false,

    // Upload telemetry
    fileboxUploadProgress: 0,
    fileboxUploadSpeedText: '-',
    fileboxUploadEtaText: '-',
    fileboxUploadingName: '',
    fileboxAbortController: null,

    fileboxMaxFileSize: FILEBOX_MAX_FILE_SIZE,
};

export const fileboxMethods = {
    initFileBox() {
        this.loadFileBoxHistory();
    },

    fileboxNotify(message, type = 'info') {
        if (typeof this.showToast === 'function') {
            this.showToast(message, type);
            return;
        }
        if (this.$toast && typeof this.$toast[type] === 'function') {
            this.$toast[type](message);
            return;
        }
        console.log(`[FileBox][${type}] ${message}`);
    },

    switchFileboxTab(tab) {
        this.fileboxCurrentTab = tab;
        if (tab === 'history') {
            this.loadFileBoxServerHistory();
        }
    },

    loadFileBoxHistory() {
        try {
            const saved = localStorage.getItem('filebox_history');
            if (saved) {
                this.fileboxHistory = JSON.parse(saved);
            }
        } catch (e) {
            console.error('Failed to load history', e);
        }
    },

    saveFileBoxHistory(entry) {
        this.fileboxHistory.unshift(entry);
        if (this.fileboxHistory.length > 50) this.fileboxHistory.length = 50;
        localStorage.setItem('filebox_history', JSON.stringify(this.fileboxHistory));
    },

    clearLocalFileBoxHistory() {
        this.fileboxHistory = [];
        localStorage.removeItem('filebox_history');
        this.fileboxNotify('本地历史已清空', 'success');
    },

    async loadFileBoxServerHistory() {
        this.fileboxHistoryLoading = true;
        try {
            const res = await axios.get('/api/filebox/history');
            if (res.data?.success) {
                this.fileboxServerHistory = Array.isArray(res.data.data) ? res.data.data : [];
            }
        } catch (error) {
            this.fileboxNotify(error.response?.data?.error || '加载服务端历史失败', 'error');
        } finally {
            this.fileboxHistoryLoading = false;
        }
    },

    validateFile(file) {
        if (!file) return false;
        if (file.size > this.fileboxMaxFileSize) {
            this.fileboxNotify(`文件过大，最大支持 ${this.formatFileSize(this.fileboxMaxFileSize)}`, 'error');
            return false;
        }
        return true;
    },

    setSelectedFile(file) {
        if (!this.validateFile(file)) return;
        this.fileboxSelectedFile = file;
        this.fileboxShareType = 'file';
    },

    handleFileDrop(e) {
        this.isDragging = false;
        const files = e.dataTransfer?.files;
        if (files && files.length > 0) {
            this.setSelectedFile(files[0]);
        }
    },

    handleFileSelect(e) {
        const files = e.target?.files;
        if (files && files.length > 0) {
            this.setSelectedFile(files[0]);
        }
    },

    resetUploadTelemetry() {
        this.fileboxUploadProgress = 0;
        this.fileboxUploadSpeedText = '-';
        this.fileboxUploadEtaText = '-';
        this.fileboxUploadingName = '';
        this.fileboxAbortController = null;
    },

    resetFileBoxForm() {
        this.fileboxShareText = '';
        this.fileboxSelectedFile = null;
        this.fileboxExpiry = '24';
        this.fileboxBurnAfterReading = false;
        if (this.$refs.fileInput) this.$refs.fileInput.value = '';
        this.resetUploadTelemetry();
    },

    cancelFileBoxUpload() {
        if (this.fileboxAbortController) {
            this.fileboxAbortController.abort();
            this.fileboxNotify('上传已取消', 'warning');
        }
    },

    async shareFileBoxEntry() {
        const isTextMode = this.fileboxShareType === 'text';
        if (isTextMode && !this.fileboxShareText.trim()) return;
        if (!isTextMode && !this.fileboxSelectedFile) return;
        if (!isTextMode && !this.validateFile(this.fileboxSelectedFile)) return;

        this.fileboxLoading = true;
        this.resetUploadTelemetry();

        let lastTs = Date.now();
        let lastLoaded = 0;

        try {
            const formData = new FormData();
            formData.append('type', this.fileboxShareType);
            formData.append('expiry', this.fileboxExpiry);
            formData.append('burn_after_reading', this.fileboxBurnAfterReading);

            if (isTextMode) {
                formData.append('text', this.fileboxShareText);
            } else {
                formData.append('file', this.fileboxSelectedFile);
                this.fileboxUploadingName = this.fileboxSelectedFile.name;
                this.fileboxAbortController = new AbortController();
            }

            const res = await axios.post('/api/filebox/share', formData, {
                headers: { 'Content-Type': 'multipart/form-data' },
                signal: this.fileboxAbortController?.signal,
                onUploadProgress: (evt) => {
                    if (isTextMode) return;
                    if (!evt || !evt.total) return;

                    const now = Date.now();
                    const deltaMs = Math.max(1, now - lastTs);
                    const deltaBytes = Math.max(0, evt.loaded - lastLoaded);
                    const speed = (deltaBytes * 1000) / deltaMs;
                    const remain = Math.max(0, evt.total - evt.loaded);
                    const etaSec = speed > 0 ? remain / speed : Infinity;

                    this.fileboxUploadProgress = Math.min(100, Math.round((evt.loaded / evt.total) * 100));
                    this.fileboxUploadSpeedText = formatSpeed(speed);
                    this.fileboxUploadEtaText = formatEta(etaSec);

                    lastTs = now;
                    lastLoaded = evt.loaded;
                },
            });

            if (res.data?.success) {
                this.fileboxUploadProgress = 100;
                this.fileboxResult = { code: res.data.code };
                await this.generateFileBoxQrCode(res.data.code);

                this.saveFileBoxHistory({
                    code: res.data.code,
                    type: this.fileboxShareType,
                    originalName: this.fileboxSelectedFile ? this.fileboxSelectedFile.name : null,
                    content: this.fileboxShareText,
                    size: this.fileboxSelectedFile ? this.fileboxSelectedFile.size : 0,
                    createdAt: Date.now(),
                });

                this.fileboxNotify('分享成功，取件码已生成', 'success');
                if (this.fileboxCurrentTab === 'history') {
                    this.loadFileBoxServerHistory();
                }
            } else {
                this.fileboxNotify('分享失败: ' + (res.data?.error || '未知错误'), 'error');
            }
        } catch (error) {
            if (error.name === 'CanceledError' || error.code === 'ERR_CANCELED') {
                return;
            }
            this.handleError(error);
        } finally {
            this.fileboxLoading = false;
            this.fileboxAbortController = null;
        }
    },

    async retrieveFileBoxEntry() {
        const code = (this.fileboxRetrieveCode || '').trim().toUpperCase();
        if (!code || code.length < 5) {
            this.fileboxNotify('请输入 5 位取件码', 'warning');
            return;
        }

        this.fileboxRetrieveCode = code;
        this.fileboxLoading = true;
        try {
            const res = await axios.get(`/api/filebox/retrieve/${code}`);
            if (res.data?.success) {
                this.fileboxRetrievedEntry = res.data.data;
                if (this.fileboxRetrievedEntry.type === 'text') {
                    const contentRes = await axios.get(`/api/filebox/download/${code}`, { responseType: 'text' });
                    this.fileboxRetrievedEntry.content = contentRes.data;
                }
            } else {
                this.fileboxNotify(res.data?.error || '取件失败', 'error');
            }
        } catch (error) {
            if (error.response && error.response.status === 404) {
                this.fileboxNotify('取件码无效或已过期', 'error');
            } else {
                this.handleError(error);
            }
        } finally {
            this.fileboxLoading = false;
        }
    },

    downloadFileBoxEntry(code) {
        window.open(`/api/filebox/download/${code}`, '_blank');
    },

    async deleteFileBoxEntry(code) {
        try {
            await axios.delete(`/api/filebox/${code}`);
            this.fileboxNotify('已删除', 'success');
        } catch (error) {
            console.error('后端删除失败:', error);
        }

        this.fileboxHistory = this.fileboxHistory.filter(h => h.code !== code);
        this.fileboxServerHistory = this.fileboxServerHistory.filter(h => h.code !== code);
        localStorage.setItem('filebox_history', JSON.stringify(this.fileboxHistory));
    },

    handleError(error) {
        console.error(error);
        const msg = error.response?.data?.error || error.message || '操作失败';
        this.fileboxNotify(msg, 'error');
    },

    copyToClipboard(text) {
        if (navigator.clipboard && window.isSecureContext) {
            navigator.clipboard.writeText(text).then(() => {
                this.fileboxNotify('已复制到剪贴板', 'success');
            }, () => {
                this.fileboxNotify('复制失败', 'error');
            });
        } else {
            const textArea = document.createElement('textarea');
            textArea.value = text;
            textArea.style.position = 'fixed';
            textArea.style.left = '-9999px';
            document.body.appendChild(textArea);
            textArea.focus();
            textArea.select();
            try {
                document.execCommand('copy');
                this.fileboxNotify('已复制到剪贴板', 'success');
            } catch (err) {
                this.fileboxNotify('复制失败', 'error');
            }
            document.body.removeChild(textArea);
        }
    },

    copyFileBoxLink(code) {
        const url = `${window.location.origin}/api/filebox/download/${code}`;
        this.copyToClipboard(url);
    },

    async generateFileBoxQrCode(code) {
        const url = `${window.location.origin}/api/filebox/download/${code}`;
        try {
            const QRCode = window.QRCode || (await import('qrcode')).default;
            if (QRCode.toDataURL) {
                this.fileboxQrCode = await QRCode.toDataURL(url, {
                    width: 150,
                    margin: 1,
                    color: { dark: '#000', light: '#fff' },
                });
            }
        } catch (e) {
            console.error('QRCode generation failed:', e);
            this.fileboxQrCode = '';
        }
    },
};
