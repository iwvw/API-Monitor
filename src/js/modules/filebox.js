import axios from 'axios';
import { store } from '../store.js';

export const fileboxData = {
    fileboxRetrieveCode: '',
    fileboxShareType: 'file', // 'file' or 'text'
    fileboxCurrentTab: 'share', // 'share' or 'history'
    fileboxShareText: '',
    fileboxSelectedFile: null,
    fileboxExpiry: '24',
    fileboxBurnAfterReading: false,
    fileboxLoading: false,
    fileboxResult: null, // { code: '...' }
    fileboxQrCode: '', // 二维码 Data URL
    fileboxHistory: [], // Local history of uploads
    fileboxRetrievedEntry: null, // Populated after retrieve
    isDragging: false,
};

export const fileboxMethods = {
    // Methods
    initFileBox() {
        this.loadFileBoxHistory();
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
        // Add to history
        this.fileboxHistory.unshift(entry);
        // Limit to 20
        if (this.fileboxHistory.length > 20) this.fileboxHistory.length = 20;
        localStorage.setItem('filebox_history', JSON.stringify(this.fileboxHistory));
    },

    handleFileDrop(e) {
        this.isDragging = false;
        const files = e.dataTransfer.files;
        if (files.length > 0) {
            this.fileboxSelectedFile = files[0];
            this.fileboxShareType = 'file';
        }
    },

    handleFileSelect(e) {
        const files = e.target.files;
        if (files.length > 0) {
            this.fileboxSelectedFile = files[0];
        }
    },

    resetFileBoxForm() {
        this.fileboxShareText = '';
        this.fileboxSelectedFile = null;
        this.fileboxExpiry = '24';
        this.fileboxBurnAfterReading = false;
        // Clear file input
        if (this.$refs.fileInput) this.$refs.fileInput.value = '';
    },

    async shareFileBoxEntry() {
        if (this.fileboxShareType === 'text' && !this.fileboxShareText) return;
        if (this.fileboxShareType === 'file' && !this.fileboxSelectedFile) return;

        this.fileboxLoading = true;
        try {
            const formData = new FormData();
            formData.append('type', this.fileboxShareType);
            formData.append('expiry', this.fileboxExpiry);
            formData.append('burn_after_reading', this.fileboxBurnAfterReading);

            if (this.fileboxShareType === 'text') {
                formData.append('text', this.fileboxShareText);
            } else {
                formData.append('file', this.fileboxSelectedFile);
            }

            const res = await axios.post('/api/filebox/share', formData, {
                headers: { 'Content-Type': 'multipart/form-data' }
            });

            if (res.data.success) {
                this.fileboxResult = { code: res.data.code };

                // 生成二维码
                this.generateFileBoxQrCode(res.data.code);

                // Save minimal info to history
                this.saveFileBoxHistory({
                    code: res.data.code,
                    type: this.fileboxShareType,
                    originalName: this.fileboxSelectedFile ? this.fileboxSelectedFile.name : null,
                    content: this.fileboxShareText,
                    size: this.fileboxSelectedFile ? this.fileboxSelectedFile.size : 0,
                    createdAt: Date.now()
                });

                this.showToast('分享成功！取件码已生成', 'success');
            } else {
                this.showToast('分享失败: ' + res.data.error, 'error');
            }
        } catch (error) {
            this.handleError(error);
        } finally {
            this.fileboxLoading = false;
        }
    },

    async retrieveFileBoxEntry() {
        if (!this.fileboxRetrieveCode || this.fileboxRetrieveCode.length < 5) {
            this.showToast('请输入 5 位取件码', 'warning');
            return;
        }

        this.fileboxLoading = true;
        try {
            // First get metadata
            const res = await axios.get(`/api/filebox/retrieve/${this.fileboxRetrieveCode}`);
            if (res.data.success) {
                this.fileboxRetrievedEntry = res.data.data;
                if (this.fileboxRetrievedEntry.type === 'text') {
                    const contentRes = await axios.get(`/api/filebox/download/${this.fileboxRetrieveCode}`, { responseType: 'text' });
                    this.fileboxRetrievedEntry.content = contentRes.data;
                }
            } else {
                this.showToast(res.data.error || '取件失败', 'error');
            }
        } catch (error) {
            // 404 handled here
            if (error.response && error.response.status === 404) {
                this.showToast('取件码无效或已过期', 'error');
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
            // 调用后端删除 API
            await axios.delete(`/api/filebox/${code}`);
            this.showToast('已删除', 'success');
        } catch (error) {
            // 后端删除失败（可能已过期或不存在），仍继续清理本地记录
            console.error('后端删除失败:', error);
        }
        // 同时清理本地历史记录
        this.fileboxHistory = this.fileboxHistory.filter(h => h.code !== code);
        localStorage.setItem('filebox_history', JSON.stringify(this.fileboxHistory));
    },

    handleError(error) {
        console.error(error);
        const msg = error.response?.data?.error || error.message;
        this.$toast.error(msg);
    },

    copyToClipboard(text) {
        if (navigator.clipboard && window.isSecureContext) {
            navigator.clipboard.writeText(text).then(() => {
                this.showToast('已复制到剪贴板', 'success');
            }, () => {
                this.showToast('复制失败', 'error');
            });
        } else {
            // Fallback
            const textArea = document.createElement("textarea");
            textArea.value = text;
            textArea.style.position = "fixed";
            textArea.style.left = "-9999px";
            document.body.appendChild(textArea);
            textArea.focus();
            textArea.select();
            try {
                document.execCommand('copy');
                this.showToast('已复制到剪贴板', 'success');
            } catch (err) {
                this.showToast('复制失败', 'error');
            }
            document.body.removeChild(textArea);
        }
    },

    // 复制分享链接（直接下载链接）
    copyFileBoxLink(code) {
        const url = `${window.location.origin}/api/filebox/download/${code}`;
        this.copyToClipboard(url);
    },

    // 生成二维码
    async generateFileBoxQrCode(code) {
        const url = `${window.location.origin}/api/filebox/download/${code}`;
        try {
            // 使用 QRCode CDN 库或 canvas 生成
            const QRCode = window.QRCode || (await import('qrcode')).default;
            if (QRCode.toDataURL) {
                this.fileboxQrCode = await QRCode.toDataURL(url, {
                    width: 150,
                    margin: 1,
                    color: { dark: '#000', light: '#fff' }
                });
            }
        } catch (e) {
            console.error('QRCode generation failed:', e);
            this.fileboxQrCode = '';
        }
    }
};
