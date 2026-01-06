/**
 * SFTP 文件管理模块
 * 负责远程文件浏览、上传、下载、编辑等操作
 */

import { toast } from './toast.js';

/**
 * SFTP 方法集合
 */
export const sftpMethods = {
    /**
     * 加载目录内容
     * @param {string} serverId - 服务器 ID
     * @param {string} path - 目录路径
     */
    async loadSftpDirectory(serverId, path = '/') {
        if (!serverId) {
            toast.error('请先选择服务器');
            return;
        }

        this.sftpLoading = true;
        this.sftpError = '';

        try {
            const response = await fetch('/api/server/sftp/list', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ serverId, path }),
            });
            const data = await response.json();

            if (data.success) {
                this.sftpFiles = data.data;
                this.sftpCurrentPath = data.path;
                this.sftpServerId = serverId;
                // 构建路径导航
                this._buildPathBreadcrumbs();
            } else {
                this.sftpError = data.error || '加载失败';
                toast.error(this.sftpError);
            }
        } catch (error) {
            this.sftpError = '请求失败: ' + error.message;
            toast.error(this.sftpError);
        } finally {
            this.sftpLoading = false;
        }
    },

    /**
     * 构建路径面包屑
     */
    _buildPathBreadcrumbs() {
        const parts = this.sftpCurrentPath.split('/').filter(Boolean);
        const crumbs = [{ name: '/', path: '/' }];
        let currentPath = '';

        for (const part of parts) {
            currentPath += '/' + part;
            crumbs.push({ name: part, path: currentPath });
        }

        this.sftpBreadcrumbs = crumbs;
    },

    /**
     * 导航到指定路径
     */
    navigateToPath(path) {
        this.loadSftpDirectory(this.sftpServerId, path);
    },

    /**
     * 进入目录
     */
    enterDirectory(file) {
        if (file.isDirectory) {
            this.loadSftpDirectory(this.sftpServerId, file.path);
        }
    },

    /**
     * 返回上级目录
     */
    goUpDirectory() {
        if (this.sftpCurrentPath === '/') return;
        const parentPath = this.sftpCurrentPath.split('/').slice(0, -1).join('/') || '/';
        this.loadSftpDirectory(this.sftpServerId, parentPath);
    },

    /**
     * 刷新当前目录
     */
    refreshSftpDirectory() {
        this.loadSftpDirectory(this.sftpServerId, this.sftpCurrentPath);
    },

    /**
     * 打开文件（编辑或下载）
     */
    async openFile(file) {
        if (file.isDirectory) {
            this.enterDirectory(file);
            return;
        }

        // 检查文件大小，超过 1MB 提示下载
        if (file.size > 1024 * 1024) {
            const confirmed = await this.showConfirm({
                title: '文件较大',
                message: `文件大小为 ${this.formatFileSize(file.size)}，建议下载查看。是否仍要在线编辑？`,
                confirmText: '在线编辑',
                cancelText: '下载',
            });

            if (!confirmed) {
                this.downloadFile(file);
                return;
            }
        }

        this.sftpEditLoading = true;

        try {
            const response = await fetch('/api/server/sftp/read', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    serverId: this.sftpServerId,
                    path: file.path,
                    maxSize: 2 * 1024 * 1024, // 最大 2MB
                }),
            });
            const data = await response.json();

            if (data.success) {
                this.sftpEditFile = {
                    path: file.path,
                    name: file.name,
                    content: data.data,
                    originalContent: data.data,
                };
                this.showSftpEditorModal = true;
            } else {
                toast.error(data.error || '读取文件失败');
            }
        } catch (error) {
            toast.error('请求失败: ' + error.message);
        } finally {
            this.sftpEditLoading = false;
        }
    },

    /**
     * 保存编辑的文件
     */
    async saveEditedFile() {
        if (!this.sftpEditFile) return;

        this.sftpSaving = true;

        try {
            const response = await fetch('/api/server/sftp/write', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    serverId: this.sftpServerId,
                    path: this.sftpEditFile.path,
                    content: this.sftpEditFile.content,
                }),
            });
            const data = await response.json();

            if (data.success) {
                toast.success('保存成功');
                this.sftpEditFile.originalContent = this.sftpEditFile.content;
                this.showSftpEditorModal = false;
            } else {
                toast.error(data.error || '保存失败');
            }
        } catch (error) {
            toast.error('请求失败: ' + error.message);
        } finally {
            this.sftpSaving = false;
        }
    },

    /**
     * 下载文件
     */
    downloadFile(file) {
        const url = `/api/server/sftp/download/${this.sftpServerId}?path=${encodeURIComponent(file.path)}`;
        const a = document.createElement('a');
        a.href = url;
        a.download = file.name;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        toast.success('开始下载: ' + file.name);
    },

    /**
     * 创建新目录
     */
    async createDirectory() {
        const name = await this.showPrompt({
            title: '新建文件夹',
            message: '请输入文件夹名称',
            placeholder: '新文件夹',
        });

        if (!name) return;

        const newPath = this.sftpCurrentPath === '/'
            ? '/' + name
            : this.sftpCurrentPath + '/' + name;

        try {
            const response = await fetch('/api/server/sftp/mkdir', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    serverId: this.sftpServerId,
                    path: newPath,
                }),
            });
            const data = await response.json();

            if (data.success) {
                toast.success('文件夹创建成功');
                this.refreshSftpDirectory();
            } else {
                toast.error(data.error || '创建失败');
            }
        } catch (error) {
            toast.error('请求失败: ' + error.message);
        }
    },

    /**
     * 创建新文件
     */
    async createFile() {
        const name = await this.showPrompt({
            title: '新建文件',
            message: '请输入文件名称',
            placeholder: 'new_file.txt',
        });

        if (!name) return;

        const newPath = this.sftpCurrentPath === '/'
            ? '/' + name
            : this.sftpCurrentPath + '/' + name;

        try {
            const response = await fetch('/api/server/sftp/write', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    serverId: this.sftpServerId,
                    path: newPath,
                    content: '',
                }),
            });
            const data = await response.json();

            if (data.success) {
                toast.success('文件创建成功');
                this.refreshSftpDirectory();
            } else {
                toast.error(data.error || '创建失败');
            }
        } catch (error) {
            toast.error('请求失败: ' + error.message);
        }
    },

    /**
     * 删除文件或目录
     */
    async deleteItem(file) {
        const message = file.isDirectory
            ? `确定要删除目录 "${file.name}" 及其所有内容吗？\n⚠️ 此操作不可恢复！`
            : `确定要删除文件 "${file.name}" 吗？`;

        const confirmed = await this.showConfirm({
            title: '确认删除',
            message: message,
            icon: 'fa-trash',
            confirmText: '删除',
            confirmClass: 'btn-danger',
        });

        if (!confirmed) return;

        const endpoint = file.isDirectory ? '/api/server/sftp/rmdir' : '/api/server/sftp/delete';
        const body = {
            serverId: this.sftpServerId,
            path: file.path,
        };

        // 目录使用递归删除
        if (file.isDirectory) {
            body.recursive = true;
        }

        try {
            const response = await fetch(endpoint, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });
            const data = await response.json();

            if (data.success) {
                toast.success('删除成功');
                this.refreshSftpDirectory();
            } else {
                toast.error(data.error || '删除失败');
            }
        } catch (error) {
            toast.error('请求失败: ' + error.message);
        }
    },

    /**
     * 重命名文件或目录
     */
    async renameItem(file) {
        const newName = await this.showPrompt({
            title: '重命名',
            message: '请输入新名称',
            defaultValue: file.name,
        });

        if (!newName || newName === file.name) return;

        const parentPath = file.path.substring(0, file.path.lastIndexOf('/')) || '/';
        const newPath = parentPath === '/' ? '/' + newName : parentPath + '/' + newName;

        try {
            const response = await fetch('/api/server/sftp/rename', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    serverId: this.sftpServerId,
                    oldPath: file.path,
                    newPath: newPath,
                }),
            });
            const data = await response.json();

            if (data.success) {
                toast.success('重命名成功');
                this.refreshSftpDirectory();
            } else {
                toast.error(data.error || '重命名失败');
            }
        } catch (error) {
            toast.error('请求失败: ' + error.message);
        }
    },

    /**
     * 上传文件
     */
    async uploadFiles(event) {
        const files = event.target.files;
        if (!files || files.length === 0) return;

        this.sftpUploading = true;
        let successCount = 0;
        let failCount = 0;

        for (const file of files) {
            try {
                const formData = new FormData();
                formData.append('serverId', this.sftpServerId);
                formData.append('path', this.sftpCurrentPath);
                formData.append('file', file);

                const response = await fetch('/api/server/sftp/upload', {
                    method: 'POST',
                    body: formData,
                });
                const data = await response.json();

                if (data.success) {
                    successCount++;
                } else {
                    failCount++;
                    console.error('Upload failed:', file.name, data.error);
                }
            } catch (error) {
                failCount++;
                console.error('Upload error:', file.name, error);
            }
        }

        this.sftpUploading = false;

        if (successCount > 0) {
            toast.success(`上传成功 ${successCount} 个文件`);
            this.refreshSftpDirectory();
        }
        if (failCount > 0) {
            toast.error(`上传失败 ${failCount} 个文件`);
        }

        // 清空文件输入
        event.target.value = '';
    },

    /**
     * 触发上传
     */
    triggerUpload() {
        const input = document.getElementById('sftp-upload-input');
        if (input) input.click();
    },

    /**
     * 触发文件夹上传
     */
    triggerFolderUpload() {
        const input = document.getElementById('sftp-folder-upload-input');
        if (input) input.click();
    },

    /**
     * 上传文件夹
     */
    async uploadFolder(event) {
        const files = event.target.files;
        if (!files || files.length === 0) return;

        this.sftpUploading = true;
        let successCount = 0;
        let failCount = 0;
        const totalFiles = files.length;

        toast.info(`开始上传 ${totalFiles} 个文件...`);

        for (let i = 0; i < files.length; i++) {
            const file = files[i];
            try {
                const formData = new FormData();
                formData.append('serverId', this.sftpServerId);
                formData.append('path', this.sftpCurrentPath);
                formData.append('file', file);
                // 使用 webkitRelativePath 保持目录结构
                formData.append('relativePath', file.webkitRelativePath);

                const response = await fetch('/api/server/sftp/upload', {
                    method: 'POST',
                    body: formData,
                });
                const data = await response.json();

                if (data.success) {
                    successCount++;
                } else {
                    failCount++;
                    console.error('Upload failed:', file.webkitRelativePath, data.error);
                }

                // 每上传 10 个文件更新一次进度
                if ((i + 1) % 10 === 0) {
                    toast.info(`上传进度: ${i + 1}/${totalFiles}`);
                }
            } catch (error) {
                failCount++;
                console.error('Upload error:', file.webkitRelativePath, error);
            }
        }

        this.sftpUploading = false;

        if (successCount > 0) {
            toast.success(`上传成功 ${successCount} 个文件`);
            this.refreshSftpDirectory();
        }
        if (failCount > 0) {
            toast.error(`上传失败 ${failCount} 个文件`);
        }

        // 清空文件输入
        event.target.value = '';
    },

    /**
     * 格式化文件大小
     */
    formatFileSize(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    },

    /**
     * 格式化时间戳
     */
    formatFileTime(timestamp) {
        if (!timestamp) return '-';
        return new Date(timestamp).toLocaleString();
    },

    /**
     * 获取文件图标
     */
    getFileIcon(file) {
        if (file.isDirectory) return 'fa-folder';
        if (file.isSymlink) return 'fa-link';

        const ext = file.name.split('.').pop().toLowerCase();
        const iconMap = {
            // 代码
            js: 'fa-file-code', ts: 'fa-file-code', jsx: 'fa-file-code', tsx: 'fa-file-code',
            py: 'fa-file-code', java: 'fa-file-code', go: 'fa-file-code', rs: 'fa-file-code',
            c: 'fa-file-code', cpp: 'fa-file-code', h: 'fa-file-code', cs: 'fa-file-code',
            php: 'fa-file-code', rb: 'fa-file-code', swift: 'fa-file-code', kt: 'fa-file-code',
            html: 'fa-file-code', htm: 'fa-file-code', css: 'fa-file-code', scss: 'fa-file-code',
            vue: 'fa-file-code', svelte: 'fa-file-code',
            // 配置
            json: 'fa-file-alt', yaml: 'fa-file-alt', yml: 'fa-file-alt', toml: 'fa-file-alt',
            xml: 'fa-file-alt', ini: 'fa-file-alt', conf: 'fa-file-alt', cfg: 'fa-file-alt',
            env: 'fa-file-alt',
            // 脚本
            sh: 'fa-file-code', bash: 'fa-file-code', zsh: 'fa-file-code',
            bat: 'fa-file-code', cmd: 'fa-file-code', ps1: 'fa-file-code',
            // 文档
            md: 'fa-file-alt', txt: 'fa-file-alt', log: 'fa-file-alt',
            doc: 'fa-file-word', docx: 'fa-file-word',
            pdf: 'fa-file-pdf',
            xls: 'fa-file-excel', xlsx: 'fa-file-excel',
            ppt: 'fa-file-powerpoint', pptx: 'fa-file-powerpoint',
            // 图片
            png: 'fa-file-image', jpg: 'fa-file-image', jpeg: 'fa-file-image',
            gif: 'fa-file-image', svg: 'fa-file-image', webp: 'fa-file-image',
            ico: 'fa-file-image', bmp: 'fa-file-image',
            // 音视频
            mp3: 'fa-file-audio', wav: 'fa-file-audio', flac: 'fa-file-audio', aac: 'fa-file-audio',
            mp4: 'fa-file-video', avi: 'fa-file-video', mkv: 'fa-file-video', mov: 'fa-file-video',
            // 压缩包
            zip: 'fa-file-archive', rar: 'fa-file-archive', tar: 'fa-file-archive',
            gz: 'fa-file-archive', '7z': 'fa-file-archive', bz2: 'fa-file-archive',
        };

        return iconMap[ext] || 'fa-file';
    },

    /**
     * 获取文件图标颜色
     */
    getFileIconColor(file) {
        if (file.isDirectory) return '#f59e0b'; // 黄色文件夹
        if (file.isSymlink) return '#8b5cf6'; // 紫色链接

        const ext = file.name.split('.').pop().toLowerCase();
        const colorMap = {
            // 代码 - 蓝色系
            js: '#f7df1e', ts: '#3178c6', jsx: '#61dafb', tsx: '#3178c6',
            py: '#3776ab', java: '#007396', go: '#00add8', rs: '#dea584',
            vue: '#42b883', svelte: '#ff3e00',
            html: '#e34c26', css: '#264de4', scss: '#cf649a',
            php: '#777bb4', rb: '#cc342d',
            // 配置 - 灰色
            json: '#cbcb41', yaml: '#cb171e', yml: '#cb171e',
            xml: '#f26522', env: '#ecd53f',
            // 脚本 - 绿色
            sh: '#4eaa25', bash: '#4eaa25',
            // 文档 - 中性色
            md: '#083fa1', txt: '#6b7280',
            pdf: '#ff0000',
            // 图片 - 粉色
            png: '#a855f7', jpg: '#a855f7', gif: '#a855f7',
            // 压缩包 - 棕色
            zip: '#854d0e', rar: '#854d0e', tar: '#854d0e',
        };

        return colorMap[ext] || 'var(--text-tertiary)';
    },

    /**
     * 切换文件管理侧栏
     */
    toggleSftpSidebar() {
        this.showSftpSidebar = !this.showSftpSidebar;

        // 首次打开时，如果有当前会话，加载其服务器的文件
        if (this.showSftpSidebar && !this.sftpServerId && this.currentSSHSession) {
            this.loadSftpDirectory(this.currentSSHSession.server.id, '/');
        }
    },

    /**
     * 在终端中执行 cd 命令
     */
    cdToPath(path) {
        const session = this.getSessionById(this.activeSSHSessionId);
        if (session && session.ws && session.ws.readyState === WebSocket.OPEN) {
            session.ws.send(JSON.stringify({ type: 'input', data: `cd ${path}\r` }));
            toast.success('已切换目录');
        }
    },

    /**
     * 在终端中执行 cat 命令
     */
    catFile(file) {
        if (file.isDirectory) return;
        const session = this.getSessionById(this.activeSSHSessionId);
        if (session && session.ws && session.ws.readyState === WebSocket.OPEN) {
            session.ws.send(JSON.stringify({ type: 'input', data: `cat "${file.path}"\r` }));
        }
    },
};
