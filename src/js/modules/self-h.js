/**
 * 自建服务模块 (Self-H) 前端逻辑
 */
import { store } from '../store.js';
import { toast } from './toast.js';

export const selfHMethods = {
    // 加载所有 OpenList 账号
    async loadOpenListAccounts() {
        try {
            const response = await fetch('/api/openlist/manage-accounts');
            const data = await response.json();
            if (data.success) {
                this.openListAccounts = data.data;
                this.openListStats.onlineCount = this.openListAccounts.filter(a => a.status === 'online').length;

                // 如果当前没有选中的账号，但有可用账号，则自动选择第一个
                if (!this.currentOpenListAccount && this.openListAccounts.length > 0) {
                    this.selectOpenListAccount(this.openListAccounts[0]);
                }

                // 尝试获取第一个在线账号的存储统计 (用于概览展示)
                const onlineAccount = this.openListAccounts.find(a => a.status === 'online');
                if (onlineAccount) {
                    this.fetchStorageStats(onlineAccount.id);
                }
            }

            // 加载设置
            this.loadOpenListSettings();
        } catch (e) {
            console.error('Failed to load OpenList accounts:', e);
        }
    },

    // 加载设置
    async loadOpenListSettings() {
        try {
            const res = await fetch('/api/openlist/settings/preview_size');
            const data = await res.json();
            if (data.success && data.value) {
                store.openListPreviewSize = parseInt(data.value);
            }
        } catch (e) {
            console.warn('Failed to load settings:', e);
        }
    },

    // 保存预览尺寸
    async saveOpenListPreviewSize() {
        try {
            await fetch('/api/openlist/settings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ key: 'preview_size', value: store.openListPreviewSize.toString() })
            });
            toast.success('设置已保存');
        } catch (e) {
            toast.error('保存失败');
        }
    },

    // 获取存储统计
    async fetchStorageStats(accountId) {
        try {
            const response = await fetch(`/api/openlist/${accountId}/admin/storages`);
            const data = await response.json();
            if (data.code === 200 && data.data && data.data.content) {
                const storages = data.data.content;
                this.openListStorages = storages; // 保存完整列表用于路径匹配

                let total = 0;
                let free = 0;
                let hasData = false;

                storages.forEach(storage => {
                    if (storage.mount_details) {
                        if (storage.mount_details.total_space > 0) {
                            total += storage.mount_details.total_space;
                            free += storage.mount_details.free_space;
                            hasData = true;
                        }
                    }
                });

                if (hasData) {
                    this.openListStats = {
                        ...this.openListStats,
                        totalSpace: total,
                        usedSpace: total - free,
                        freeSpace: free,
                        hasStorageData: true
                    };
                }
            }
        } catch (e) {
            console.warn('Failed to fetch storage stats:', e);
        }
    },

    // 切换到账号管理标签
    goToOpenListAccounts() {
        this.openListSubTab = 'settings';
    },

    // 添加账号
    async doAddOpenListAccount() {
        if (!this.newOpenListAcc.name || !this.newOpenListAcc.api_url || !this.newOpenListAcc.api_token) {
            return toast.error('请填写完整信息');
        }
        try {
            const response = await fetch('/api/openlist/manage-accounts', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(this.newOpenListAcc)
            });
            const data = await response.json();
            if (data.success) {
                toast.success('账号已添加');
                this.newOpenListAcc = { name: '', api_url: '', api_token: '' };
                this.loadOpenListAccounts();
            }
        } catch (e) {
            toast.error('添加失败: ' + e.message);
        }
    },

    // 删除账号
    async deleteOpenListAccount(id) {
        if (!confirm('确定要删除这个 OpenList 实例配置吗？')) return;
        try {
            const response = await fetch(`/api/openlist/manage-accounts/${id}`, { method: 'DELETE' });
            const data = await response.json();
            if (data.success) {
                toast.success('账号已删除');
                if (this.currentOpenListAccount && this.currentOpenListAccount.id === id) {
                    this.currentOpenListAccount = null;
                }
                this.loadOpenListAccounts();
            }
        } catch (e) {
            toast.error('删除失败');
        }
    },

    // 测试账号连接
    async testOpenListAccount(id) {
        try {
            toast.info('正在测试连接...');
            const response = await fetch(`/api/openlist/manage-accounts/${id}/test`, {
                method: 'POST'
            });
            const data = await response.json();
            if (data.success) {
                const result = data.data;
                if (result.status === 'online') {
                    toast.success(`连接成功！用户: ${result.user?.username || '未知'}`);
                } else if (result.status === 'auth_failed') {
                    toast.warning('Token 无效，请检查配置');
                } else {
                    toast.error('连接失败: ' + (result.error || '服务不可用'));
                }
                // 刷新账号列表以更新状态
                this.loadOpenListAccounts();
            }
        } catch (e) {
            toast.error('测试连接失败: ' + e.message);
        }
    },

    // 根据 ID 选择账号
    selectOpenListAccountById(id) {
        const acc = this.openListAccounts.find(a => a.id === id);
        if (acc) this.selectOpenListAccount(acc);
    },

    // 选择账号进入文件管理
    selectOpenListAccount(account) {
        this.currentOpenListAccount = account;
        this.openListSubTab = 'files';
        this._clearOpenListSearch(); // 切换账号或回到根目录时清空搜索
        this.loadOpenListFiles('/');
    },

    // 辅助：清空搜索框内容
    _clearOpenListSearch() {
        const searchInput = document.querySelector('.integrated-search input');
        if (searchInput) searchInput.value = '';
        store.openListSearchActive = false; // 重置搜索激活状态
    },

    // 加载文件列表
    async loadOpenListFiles(path, refresh = false) {
        console.log('[OpenList] Loading path:', path);
        if (!this.currentOpenListAccount) return;

        // 导航到新路径时强制清空搜索框（除非是搜索本身触发，但搜索不走此方法）
        this._clearOpenListSearch();
        store.openListSearchActive = false; // 确保关闭搜索状态

        // 1. 乐观更新路径
        this.openListPath = path;

        // 2. 检查缓存 (如果不是强制刷新)
        if (!refresh && store.openListFileCache[path]) {
            console.log('[OpenList] Hit cache for:', path);
            let cachedContent = store.openListFileCache[path].content || [];

            // 验证缓存的文件名是否有效
            const hasInvalidNames = cachedContent.some(f => typeof f.name !== 'string');
            if (hasInvalidNames) {
                console.warn('[OpenList] Cache has invalid names, refreshing...');
                delete store.openListFileCache[path];
            } else {
                this.openListFiles = cachedContent;
                this.openListReadme = store.openListFileCache[path].readme;
                this.openListFilesLoading = false;
                return;
            }
        }

        // 3. 无缓存或强制刷新 -> 发起请求
        this.openListFilesLoading = true;
        this.openListFiles = []; // 清空当前列表显示骨架屏
        this.openListReadme = '';

        try {
            const response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/fs/list`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path, refresh })
            });

            // 检查 HTTP 响应状态
            if (!response.ok) {
                if (this.openListPath === path) {
                    toast.error(`服务器错误 (${response.status}): 请检查 OpenList 连接`);
                    // 恢复为空列表表示加载失败
                    this.openListFiles = [];
                }
                return;
            }

            const data = await response.json();

            // 校验：确保返回的数据依然对应当前路径
            if (this.openListPath === path) {
                if (data.code === 200) {
                    let content = data.data.content || [];
                    const readme = data.data.readme || '';

                    // 验证并修正文件数据
                    content = content.map(file => {
                        // 确保 name 是字符串
                        if (typeof file.name !== 'string') {
                            console.warn('[OpenList] Invalid file name type:', file);
                            file.name = String(file.name || 'unknown');
                        }
                        return file;
                    });

                    this.openListFiles = content;
                    this.openListReadme = readme;

                    // 写入缓存
                    store.openListFileCache[path] = { content, readme, timestamp: Date.now() };

                    // API 成功，更新账号状态为 online
                    if (this.currentOpenListAccount && this.currentOpenListAccount.status !== 'online') {
                        this.currentOpenListAccount.status = 'online';
                    }
                } else {
                    toast.error('加载失败: ' + (data.message || '未知错误'));
                    this.openListFiles = [];
                }
            }
        } catch (e) {
            if (this.openListPath === path) {
                console.error('[OpenList] Load error:', e);
                toast.error('请求出错: ' + e.message);
                this.openListFiles = [];
            }
        } finally {
            if (this.openListPath === path) {
                this.openListFilesLoading = false;
            }
        }
    },

    // 悬停预览逻辑
    showHoverPreview(e, src) {
        const preview = document.getElementById('file-hover-preview');
        const img = document.getElementById('file-hover-img');

        if (!preview || !img || !src) return;

        // 立即显示骨架屏，并定位
        preview.classList.add('loading');
        preview.style.display = 'block';
        preview.style.width = '200px';
        preview.style.height = '150px';
        img.src = ''; // 清除上一张图，防止闪烁

        this.updatePreviewPosition(e, 200, 150);
        preview.style.opacity = '1';

        img.onload = () => {
            const size = parseInt(store.openListPreviewSize) || 800;
            const ratio = (img.naturalWidth || img.width) / (img.naturalHeight || img.height) || 1;

            const maxW = window.innerWidth * 0.45;
            const maxH = window.innerHeight * 0.7;

            let targetWidth, targetHeight;
            if (ratio >= maxW / maxH) {
                targetWidth = Math.min(size, maxW);
                targetHeight = targetWidth / ratio;
            } else {
                targetHeight = Math.min(size, maxH);
                targetWidth = targetHeight * ratio;
            }

            preview.classList.remove('loading');
            preview.style.width = targetWidth + 'px';
            preview.style.height = targetHeight + 'px';

            // 加载完成后再次修正位置
            this.updatePreviewPosition(e, targetWidth, targetHeight);
        };

        img.onerror = () => {
            preview.classList.remove('loading');
            preview.style.display = 'none';
        };

        img.src = src;
    },

    // 跟随鼠标移动
    moveHoverPreview(e) {
        const preview = document.getElementById('file-hover-preview');
        if (!preview || preview.style.display === 'none') return;

        const width = parseFloat(preview.style.width) || 200;
        const height = parseFloat(preview.style.height) || 150;

        this.updatePreviewPosition(e, width, height);
    },

    // 核心定位算法：确保在视口内且不遮挡鼠标
    updatePreviewPosition(e, width, height) {
        const preview = document.getElementById('file-hover-preview');
        if (!preview) return;

        const mouseX = e.clientX;
        const mouseY = e.clientY;
        const margin = 15;
        const screenMargin = 5;

        // 默认放在右侧居中
        let left = mouseX + margin;
        let top = mouseY - (height / 2);

        // 如果右侧放不下，放到左侧
        if (left + width + screenMargin > window.innerWidth) {
            left = mouseX - width - margin;
        }

        // 垂直边界检测
        if (top < screenMargin) {
            top = screenMargin;
        } else if (top + height + screenMargin > window.innerHeight) {
            top = window.innerHeight - height - screenMargin;
        }

        // 极端情况：两边都放不下，强行贴左/右最宽处
        if (left < screenMargin) left = screenMargin;

        preview.style.left = left + 'px';
        preview.style.top = top + 'px';
    },

    hideHoverPreview() {
        const preview = document.getElementById('file-hover-preview');
        if (preview) {
            // 立刻隐藏
            preview.classList.remove('loading');
            preview.style.display = 'none';
            preview.style.opacity = '0';
            preview.style.left = '';
            preview.style.top = '';
            preview.style.width = '';
            preview.style.height = '';
            preview.style.transition = '';

            // 移除滚动监听
            window.removeEventListener('scroll', this.hideHoverPreview, { capture: true });
        }
    },

    // 导航操作：后退 (映射为向上)
    navigateBack() {
        console.warn('[OpenList] navigateBack triggered!');
        this.goUpOpenListDir();
    },

    // 导航操作：前进
    navigateForward() {
        // Future implementation
    },

    // 返回上一级
    goUpOpenListDir() {
        const currentPath = store.openListPath;
        const isLoading = store.openListFilesLoading;

        console.log('[OpenList] goUpOpenListDir. Path:', currentPath, 'Loading:', isLoading);

        if (isLoading) return;
        if (!currentPath || currentPath === '/') return;

        const parts = currentPath.split('/').filter(p => p);
        parts.pop();
        const newPath = '/' + parts.join('/');
        this.loadOpenListFiles(newPath);
    },

    // 处理文件/目录点击
    handleOpenFile(file) {
        if (store.openListFilesLoading) return;

        if (file.is_dir) {
            console.log('[OpenList] Opening folder:', file);

            // 确保 file.name 是字符串
            const fileName = typeof file.name === 'string' ? file.name : String(file.name || '');
            if (!fileName) {
                console.error('[OpenList] Invalid file name:', file);
                return;
            }

            const newPath = this._getFilePath(file, store.openListPath);

            // 搜索结果中的文件带有 parent 字段（完整父路径）
            if (file.parent) {
                // 搜索结果：在临时标签页中打开
                // 清空搜索框（如果存在）
                const searchInput = document.querySelector('.integrated-search input');
                if (searchInput) searchInput.value = '';

                this.openTempTab(fileName, newPath);
                return;
            }

            console.log('[OpenList] Resolved target path:', newPath);
            this.loadOpenListFiles(newPath);
        } else {
            this.showOpenFileDetail(file, file.parent || store.openListPath);
        }
    },

    // 辅助：获取文件相对于特定目录的完整路径
    _getFilePath(file, baseDir = '/') {
        // 不再信任 file.path (因为它可能是相对于挂载点的路径)
        let name = file && typeof file.name === 'string' ? file.name : String((file && file.name) || '');
        name = name.replace(/^\//, ''); // 移除开头的 /

        // 搜索结果的 parent 是完整目录路径，普通浏览 baseDir 是当前路径
        let parent = (file && file.parent !== undefined && file.parent !== null) ? file.parent : baseDir;
        if (!parent || parent === '/') return '/' + name;

        if (typeof parent === 'string') {
            // 确保 parent 以 / 开头且不以 / 结尾
            if (!parent.startsWith('/')) parent = '/' + parent;
            parent = parent.replace(/\/$/, '');
        }

        const fullPath = `${parent}/${name}`;
        console.log(`[OpenList] _getFilePath: name=${name}, parent=${parent} -> ${fullPath}`);
        return fullPath;
    },

    // 中键点击处理
    handleMiddleClickItem(file) {
        if (file.is_dir) {
            // 目录：在新临时标签页中打开
            const fileName = typeof file.name === 'string' ? file.name : String(file.name || '');
            let baseDir = store.openListPath;

            // 如果是在某个临时标签页中点击，baseDir 应该为该标签页的当前路径
            if (this.openListSubTab === 'temp' && this.currentOpenListTempTab) {
                baseDir = this.currentOpenListTempTab.path;
            }

            const newPath = this._getFilePath(file, baseDir);
            console.log('[OpenList] Middle click opening folder:', newPath);
            this.openTempTab(fileName, newPath);
        } else {
            // 文件：直接触发下载（在新标签页打开渲染或下载）
            let baseDir = store.openListPath;
            if (this.openListSubTab === 'temp' && this.currentOpenListTempTab) {
                baseDir = this.currentOpenListTempTab.path;
            }
            this.downloadOpenListFile(file, baseDir);
        }
    },

    // 打开临时标签页
    openTempTab(name, path) {
        const id = 'tab-' + Date.now() + Math.random().toString(36).substr(2, 4);
        const newTab = {
            id,
            name,
            path,
            files: [],
            loading: false
        };
        store.openListTempTabs.push(newTab);
        store.openListActiveTempTabId = id;
        this.openListSubTab = 'temp';
        this.loadTempTabFiles(path, false, id);
    },

    // 切换临时地标签
    selectTempTab(id) {
        store.openListActiveTempTabId = id;
        this.openListSubTab = 'temp';
    },

    // 关闭临时标签页
    closeOpenListTempTab(id) {
        const targetId = id || store.openListActiveTempTabId;
        const index = store.openListTempTabs.findIndex(t => t.id === targetId);
        if (index === -1) return;

        store.openListTempTabs.splice(index, 1);

        // 如果关闭的是当前选中的
        if (store.openListActiveTempTabId === targetId) {
            if (store.openListTempTabs.length > 0) {
                // 自动选中前一个或第一个
                const nextTab = store.openListTempTabs[Math.max(0, index - 1)];
                store.openListActiveTempTabId = nextTab.id;
            } else {
                store.openListActiveTempTabId = null;
                this.openListSubTab = 'files';
            }
        }
    },

    // 加载临时标签页文件
    async loadTempTabFiles(path, refresh = false, tabId = null) {
        if (!this.currentOpenListAccount) return;

        const targetId = tabId || store.openListActiveTempTabId;
        const tab = store.openListTempTabs.find(t => t.id === targetId);
        if (!tab) return;

        tab.path = path;
        tab.loading = true;

        try {
            console.log(`[OpenList] Loading temp tab files for path: ${path} (refresh: ${refresh})`);
            const response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/fs/list`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path, refresh: !!refresh })
            });

            if (!response.ok) {
                toast.error(`加载失败 (${response.status})`);
                tab.loading = false;
                return;
            }

            const data = await response.json();
            if (data.code === 200) {
                const content = data.data.content || [];
                // 确保数据修正（与主列表一致）
                tab.files = content.map(f => {
                    if (typeof f.name !== 'string') f.name = String(f.name || 'unknown');
                    return f;
                });
                console.log(`[OpenList] Loaded ${tab.files.length} files for path ${path}`);
            } else {
                toast.error('加载失败: ' + (data.message || '未知错误'));
            }
        } catch (e) {
            toast.error('请求出错: ' + e.message);
        } finally {
            tab.loading = false;
        }
    },

    // 处理临时标签页文件点击
    handleTempTabFile(file) {
        const tab = this.currentOpenListTempTab;
        if (!tab || tab.loading) return;

        if (file.is_dir) {
            const fileName = typeof file.name === 'string' ? file.name : String(file.name || '');
            const newPath = this._getFilePath(file, tab.path);
            this.loadTempTabFiles(newPath);
        } else {
            this.showOpenFileDetail(file, tab.path);
        }
    },

    // 合并到主列表
    mergeToMainTab() {
        const tab = this.currentOpenListTempTab;
        if (tab) {
            const path = tab.path;
            this.closeOpenListTempTab(tab.id);
            this.loadOpenListFiles(path);
        }
    },

    // 搜索文件
    async searchOpenListFiles(keywords) {
        if (!keywords || !this.currentOpenListAccount) {
            if (store.openListSearchActive) {
                store.openListSearchActive = false;
                this.loadOpenListFiles(this.openListPath); // 恢复正常列表
            }
            return;
        }

        store.openListSearchActive = true;
        this.openListFilesLoading = true;
        // 搜索时，路径保持不变，但列表内容替换
        try {
            const response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/fs/search`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    keywords,
                    parent: store.openListPath,
                    scope: store.openListSearchScope
                })
            });

            if (!response.ok) {
                toast.error(`搜索失败 (${response.status})`);
                return;
            }

            const data = await response.json();
            if (data.code === 200) {
                this.openListFiles = data.data.content || [];
                toast.success(`找到 ${this.openListFiles.length} 个项目`);
            } else {
                toast.error('搜索失败: ' + (data.message || '未知错误'));
            }
        } catch (e) {
            toast.error('搜索失败: ' + e.message);
        } finally {
            this.openListFilesLoading = false;
        }
    },

    // 新建文件夹
    async mkdirOpenList() {
        const name = await this.showPrompt({ title: '新建文件夹', placeholder: '请输入文件夹名称' });
        if (!name) return;

        try {
            const response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/proxy/fs/mkdir`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: store.openListPath === '/' ? `/${name}` : `${store.openListPath}/${name}` })
            });

            if (!response.ok) {
                toast.error(`创建失败 (${response.status})`);
                return;
            }

            const data = await response.json();
            if (data.code === 200) {
                toast.success('创建成功');
                this.loadOpenListFiles(store.openListPath, true);
            } else {
                toast.error('创建失败: ' + (data.message || '未知错误'));
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
        }
    },

    // 重命名
    async renameOpenListFile(file) {
        const newName = await this.showPrompt({ title: '重命名', promptValue: file.name });
        if (!newName || newName === file.name) return;

        try {
            const response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/proxy/fs/rename`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    name: newName,
                    path: file.path || (store.openListPath === '/' ? `/${file.name}` : `${store.openListPath}/${file.name}`)
                })
            });

            if (!response.ok) {
                toast.error(`重命名失败 (${response.status})`);
                return;
            }

            const data = await response.json();
            if (data.code === 200) {
                toast.success('重命名成功');
                this.loadOpenListFiles(store.openListPath, true);
            } else {
                toast.error('重命名失败: ' + (data.message || '未知错误'));
            }
        } catch (e) {
            toast.error('操作失败: ' + e.message);
        }
    },

    // 删除
    async deleteOpenListFile(file) {
        const confirmed = await this.showConfirm({
            title: '确认删除',
            message: `确定要永久删除 "${file.name}" 吗？`,
            confirmClass: 'btn-danger'
        });
        if (!confirmed) return;

        try {
            const response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/proxy/fs/remove`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    names: [file.name],
                    dir: store.openListPath
                })
            });

            if (!response.ok) {
                toast.error(`删除失败 (${response.status})`);
                return;
            }

            const data = await response.json();
            if (data.code === 200) {
                toast.success('已删除');
                this.loadOpenListFiles(store.openListPath, true);
            } else {
                toast.error('删除失败: ' + (data.message || '未知错误'));
            }
        } catch (e) {
            toast.error('删除失败: ' + e.message);
        }
    },

    // 获取文件图标
    getFileIconClass(file) {
        if (file.is_dir) return 'fas fa-folder text-warning';
        const name = file.name.toLowerCase();
        if (/\.(jpg|jpeg|png|gif|webp|svg)$/.test(name)) return 'fas fa-file-image text-success';
        if (/\.(mp4|webm|mkv|avi)$/.test(name)) return 'fas fa-file-video text-danger';
        if (/\.(mp3|wav|flac)$/.test(name)) return 'fas fa-file-audio text-info';
        if (/\.(zip|rar|7z|gz|tar)$/.test(name)) return 'fas fa-file-archive text-warning';
        if (/\.(pdf)$/.test(name)) return 'fas fa-file-pdf text-danger';
        if (/\.(txt|md|sql|js|json|html|css|py)$/.test(name)) return 'fas fa-file-alt text-secondary';
        return 'fas fa-file text-secondary';
    },

    // 获取缩略图 URL
    getFileThumbnail(file) {
        // 如果有缩略图 URL，直接返回
        if (file.thumb) return file.thumb;

        // 对于图片文件，可以通过 API 生成预览 URL
        if (!file.is_dir && this.currentOpenListAccount) {
            const name = file.name.toLowerCase();
            if (/\.(jpg|jpeg|png|gif|webp|svg|bmp)$/i.test(name)) {
                const fullPath = store.openListPath === '/' ? `/${file.name}` : `${store.openListPath}/${file.name}`;
                // 通过代理获取原始文件（作为缩略图）
                // 注意：如果 OpenList 返回了 sign，可以直接用 d 接口
                if (file.sign) {
                    const baseUrl = this.currentOpenListAccount.api_url.replace(/\/$/, '');
                    return `${baseUrl}/d${encodeURI(fullPath)}?sign=${file.sign}`;
                }
            }
        }

        return null;
    },

    // 下载文件
    async downloadOpenListFile(file, baseDir = store.openListPath) {
        const fullPath = this._getFilePath(file, baseDir);
        try {
            const response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/fs/get`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: fullPath })
            });

            if (!response.ok) {
                toast.error(`获取链接失败 (${response.status})`);
                return;
            }

            const data = await response.json();
            if (data.code === 200 && data.data.raw_url) {
                window.open(data.data.raw_url, '_blank');
            } else {
                toast.error('获取链接失败: ' + (data.message || '未知错误'));
            }
        } catch (e) {
            toast.error('下载请求失败: ' + e.message);
        }
    },

    // 显示文件详情
    async showOpenFileDetail(file, baseDir = store.openListPath) {
        const fullPath = this._getFilePath(file, baseDir);
        try {
            const response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/fs/get`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: fullPath })
            });

            if (!response.ok) {
                toast.error(`获取详情失败 (${response.status})`);
                return;
            }

            const data = await response.json();
            if (data.code === 200) {
                const info = data.data;

                const detailRows = [
                    { label: '完整路径', value: `<code>${fullPath}</code>` },
                    { label: '文件大小', value: this.formatFileSize(info.size) },
                    { label: '修改日期', value: this.formatDateTime(info.modified) }
                ];

                if (info.created) detailRows.push({ label: '创建日期', value: this.formatDateTime(info.created) });
                if (info.driver) detailRows.push({ label: '存储驱动', value: `<span class="badge bg-light text-dark border">${info.driver}</span>` });

                if (info.hash_info) {
                    if (info.hash_info.sha1) detailRows.push({ label: 'SHA1', value: `<small class="text-break">${info.hash_info.sha1}</small>` });
                    if (info.hash_info.md5) detailRows.push({ label: 'MD5', value: `<small class="text-break">${info.hash_info.md5}</small>` });
                }

                let message = `
                    <div class="text-start" style="font-size: 13px;">
                        <table class="table table-sm table-borderless mb-0" style="table-layout: fixed; width: 100%;">
                            <tbody>
                                ${detailRows.map(row => `
                                    <tr>
                                        <td style="width: 80px; color: var(--text-tertiary); padding-left: 0; vertical-align: top;">${row.label}</td>
                                        <td style="color: var(--text-primary); word-break: break-all; white-space: normal;">${row.value}</td>
                                    </tr>
                                `).join('')}
                            </tbody>
                        </table>
                    </div>
                `;

                const iconClass = this.getFileIconClass(file).split(' ').filter(c => c.startsWith('fa-')).join(' ');
                this.showAlert(message, info.name, iconClass || 'fa-file', true);
            } else {
                toast.error('获取详情失败: ' + data.message);
            }
        } catch (e) {
            console.error('[OpenList] Detail load error:', e);
            toast.error('获取详情出错');
        }
    },

    // 切换排序
    toggleOpenListSort(key) {
        if (store.openListSortKey === key) {
            if (store.openListSortOrder === 'asc') {
                store.openListSortOrder = 'desc';
            } else {
                store.openListSortKey = null;
                store.openListSortOrder = 'asc';
            }
        } else {
            store.openListSortKey = key;
            store.openListSortOrder = 'asc';
        }
    },

    // 格式化大小显示 (智能判断)
    getOpenListFileSize(file) {
        const fullPath = store.openListPath === '/' ? `/${file.name}` : `${store.openListPath}/${file.name}`;

        if (file.is_dir) {
            const storage = this.openListStorages.find(s => s.mount_path === fullPath);
            if (storage && storage.mount_details && storage.mount_details.total_space > 0) {
                const used = storage.mount_details.total_space - storage.mount_details.free_space;
                return this.formatFileSize(used);
            }
            if (file.size && file.size > 0) {
                return this.formatFileSize(file.size);
            }
            return ''; // 文件夹大小为0或未定义时留空
        }

        if (!file.size || file.size <= 0) return '';
        return this.formatFileSize(file.size);
    }
};

/**
 * 辅助计算属性扩展
 */
export const selfHComputed = {
    openListPathParts(state) {
        if (!state.openListPath || state.openListPath === '/') return [];
        const parts = state.openListPath.split('/').filter(p => p);
        let current = '';
        return parts.map(p => {
            current += '/' + p;
            return { name: p, path: current };
        });
    },

    currentOpenListTempTab(state) {
        if (!state.openListActiveTempTabId) return null;
        return state.openListTempTabs.find(t => t.id === state.openListActiveTempTabId) || null;
    },

    openListTempPathParts(state) {
        const tab = state.openListTempTabs.find(t => t.id === state.openListActiveTempTabId);
        if (!tab || !tab.path || tab.path === '/') return [];
        const parts = tab.path.split('/').filter(p => p);
        let current = '';
        return parts.map(p => {
            current += '/' + p;
            return { name: p, path: current };
        });
    },

    sortedOpenListFiles(state) {
        if (!state.openListSortKey) {
            return state.openListFiles;
        }

        const files = [...state.openListFiles];
        const key = state.openListSortKey;
        const order = state.openListSortOrder === 'asc' ? 1 : -1;

        return files.sort((a, b) => {
            if (a.is_dir !== b.is_dir) {
                return a.is_dir ? -1 : 1;
            }

            let valA = a[key];
            let valB = b[key];

            if (key === 'size') {
                valA = valA || 0;
                valB = valB || 0;
            }

            if (typeof valA === 'string') valA = valA.toLowerCase();
            if (typeof valB === 'string') valB = valB.toLowerCase();

            if (valA < valB) return -1 * order;
            if (valA > valB) return 1 * order;
            return 0;
        });
    }
};
