/**
 * 自建服务模块 (Self-H) 前端逻辑
 */
import { store } from '../store.js';
import { toast } from './toast.js';
import { streamPlayer } from './stream-player.js';

// 模块级变量：图片预览 ESC 键处理器
let _imagePreviewEscHandler = null;

export const selfHMethods = {
    // 加载所有 OpenList 账号
    async loadOpenListAccounts() {
        try {
            const response = await fetch('/api/openlist/manage-accounts');
            const data = await response.json();
            if (data.success) {
                this.openListAccounts = data.data;
                this.openListStats.onlineCount = this.openListAccounts.filter(a => a.status === 'online').length;

                // 如果当前没有选中的账号，尝试恢复上次选择的账号
                if (!this.currentOpenListAccount && this.openListAccounts.length > 0) {
                    const savedAccountId = localStorage.getItem('openlist_last_account');
                    const savedAccount = savedAccountId ? this.openListAccounts.find(a => a.id === savedAccountId) : null;
                    this.selectOpenListAccount(savedAccount || this.openListAccounts[0]);
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

            // 恢复视图模式
            const savedMode = localStorage.getItem('openListLayoutMode');
            if (savedMode && ['list', 'grid'].includes(savedMode)) {
                store.openListLayoutMode = savedMode;
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
        this.clearOpenListSearch(); // 切换账号或回到根目录时清空搜索

        // 保存当前账号 ID
        localStorage.setItem('openlist_last_account', account.id);

        // 尝试恢复上次浏览的路径
        const savedPath = localStorage.getItem(`openlist_path_${account.id}`);
        const initialPath = savedPath || '/';

        console.log('[OpenList] Restoring path for account:', account.id, '->', initialPath);
        this.loadOpenListFiles(initialPath);
    },

    // 辅助：清空搜索框内容
    clearOpenListSearch() {
        const searchInput = document.querySelector('.integrated-search input');
        if (searchInput) searchInput.value = '';
        store.openListSearchActive = false; // 重置搜索激活状态
    },

    // 加载文件列表
    async loadOpenListFiles(path, refresh = false) {
        console.log('[OpenList] Loading path:', path);
        if (!this.currentOpenListAccount) return;

        // 导航到新路径时强制清空搜索框（除非是搜索本身触发，但搜索不走此方法）
        this.clearOpenListSearch();
        store.openListSearchActive = false; // 确保关闭搜索状态

        // 1. 乐观更新路径
        this.openListPath = path;

        // 2. 保存当前路径到 localStorage
        if (this.currentOpenListAccount) {
            localStorage.setItem(`openlist_path_${this.currentOpenListAccount.id}`, path);
        }

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
                    // 加载失败，如果不是根目录则尝试回退
                    if (path !== '/') {
                        console.warn('[OpenList] Path failed, falling back to root:', path);
                        toast.warning('路径不可用，已返回根目录');
                        localStorage.setItem(`openlist_path_${this.currentOpenListAccount.id}`, '/');
                        this.loadOpenListFiles('/');
                        return;
                    }
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

        // 获取位置坐标 (兼容鼠标和触摸)
        const clientX = e.touches ? e.touches[0].clientX : e.clientX;
        const clientY = e.touches ? e.touches[0].clientY : e.clientY;

        // 初始化预览窗
        preview.classList.add('loading');
        preview.classList.add('active'); // 触发 CSS 展开动画

        // 初始大小（骨架屏尺寸）
        const initW = 200;
        const initH = 150;
        preview.style.width = initW + 'px';
        preview.style.height = initH + 'px';

        img.src = ''; // 清除上一张图
        this.updatePreviewPos(clientX, clientY, initW, initH);

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

            // 更新到最新位置（考虑加载期间鼠标可能移动了）
            this.updatePreviewPos(this._lastMouseX || clientX, this._lastMouseY || clientY, targetWidth, targetHeight);
        };

        img.onerror = () => {
            this.hideHoverPreview();
        };

        img.src = src;
    },

    // 跟随鼠标移动
    moveHoverPreview(e) {
        const preview = document.getElementById('file-hover-preview');
        if (!preview || !preview.classList.contains('active')) return;

        const clientX = e.touches ? e.touches[0].clientX : e.clientX;
        const clientY = e.touches ? e.touches[0].clientY : e.clientY;

        // 记录最后位置，供图片加载完校准用
        this._lastMouseX = clientX;
        this._lastMouseY = clientY;

        const width = parseFloat(preview.style.width) || 200;
        const height = parseFloat(preview.style.height) || 150;

        this.updatePreviewPos(clientX, clientY, width, height);
    },

    // 内部定位核心 (x, y 为鼠标坐标)
    updatePreviewPos(x, y, width, height) {
        const preview = document.getElementById('file-hover-preview');
        if (!preview) return;

        const margin = 20; // 与鼠标的间距
        const screenMargin = 10; // 与窗口边缘的最小间距

        let left = x + margin;
        let top = y - (height / 2);

        // 水平检测：如果右边放不下，就放左边
        if (left + width + screenMargin > window.innerWidth) {
            left = x - width - margin;
        }

        // 垂直检测：防止超出顶边或底边
        if (top < screenMargin) {
            top = screenMargin;
        } else if (top + height + screenMargin > window.innerHeight) {
            top = window.innerHeight - height - screenMargin;
        }

        // 兜底：如果左边也超出了，强行贴边
        if (left < screenMargin) left = screenMargin;

        preview.style.left = left + 'px';
        preview.style.top = top + 'px';
    },

    hideHoverPreview() {
        const preview = document.getElementById('file-hover-preview');
        if (preview) {
            // 立即移除 active 类。由于 CSS 设了 transition: none，它会立即消失。
            preview.classList.remove('active');
            preview.classList.remove('loading');

            // 重置状态
            this._lastMouseX = null;
            this._lastMouseY = null;

            // 清理样式，防止下次干扰
            setTimeout(() => {
                if (!preview.classList.contains('active')) {
                    preview.style.width = '';
                    preview.style.height = '';
                }
            }, 100);
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

            const newPath = this.getFilePath(file, store.openListPath);

            // 搜索结果中的文件带有 parent 字段（完整父路径）
            if (file.parent) {
                // 搜索结果：在临时标签页中打开
                // 清空搜索框（如果存在）
                const searchInput = document.querySelector('.integrated-search input');
                if (searchInput) searchInput.value = '';

                this.openTempTab(fileName, newPath);
                return;
            }

            this.loadOpenListFiles(newPath);
        } else {
            // 检查是否为视频文件
            if (streamPlayer.isVideoFile(file.name)) {
                this.playVideoFile(file, file.parent || store.openListPath);
            } else if (this.isImageFile(file.name)) {
                // 图片文件：打开预览弹窗
                this.openImagePreview(file, file.parent || store.openListPath);
            } else {
                this.showOpenFileDetail(file, file.parent || store.openListPath);
            }
        }
    },

    // 辅助：获取文件相对于特定目录的完整路径
    getFilePath(file, baseDir = '/') {
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

            const newPath = this.getFilePath(file, baseDir);
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
        // 查重：如果已经打开了同样路径的标签页，则直接选中
        const existingTab = store.openListTempTabs.find(t => t.path === path && !t.isVideo);
        if (existingTab) {
            this.selectTempTab(existingTab.id);
            return;
        }

        const id = 'tab-' + Date.now() + Math.random().toString(36).substr(2, 4);
        const newTab = {
            id,
            name,
            icon: 'fas fa-folder',
            path,
            files: [],
            loading: false
        };
        store.openListTempTabs.push(newTab);
        store.openListActiveTempTabId = id;
        this.openListSubTab = 'temp';
        this.loadTempTabFiles(path, false, id);
    },

    // 切换临时标签
    selectTempTab(id) {
        // 不再在切换时主动销毁播放器，允许后台继续播放或保持状态
        store.openListActiveTempTabId = id;
        this.openListSubTab = 'temp';

        // 如果新标签是视频标签页，需要等待 DOM 渲染后初始化播放器
        const tab = store.openListTempTabs.find(t => t.id === id);
        if (tab && tab.isVideo) {
            this.$nextTick(() => {
                this.initVideoPlayerInTab(tab);
            });
        }
    },

    // 关闭临时标签页
    closeOpenListTempTab(id) {
        const targetId = id || store.openListActiveTempTabId;
        const index = store.openListTempTabs.findIndex(t => t.id === targetId);
        if (index === -1) return;

        const tab = store.openListTempTabs[index];
        if (tab.isVideo && tab.plyrInstance) {
            try {
                tab.plyrInstance.destroy();
            } catch (e) {
                console.warn('[SelfH] Error destroying Plyr:', e);
            }
            tab.plyrInstance = null;
        }

        store.openListTempTabs.splice(index, 1);

        // 如果关闭的是当前选中的
        if (store.openListActiveTempTabId === targetId) {
            if (store.openListTempTabs.length > 0) {
                // 自动选中前一个或第一个
                const nextTab = store.openListTempTabs[Math.max(0, index - 1)];
                this.selectTempTab(nextTab.id);
            } else {
                store.openListActiveTempTabId = null;
                this.openListSubTab = 'files';
            }
        }
    },

    handleTabTap(tabId) {
        const now = Date.now();
        const doubleTapDelay = 300; // 300ms 内的两次点击视为双击

        if (store.openListInteraction.lastTapTabId === tabId && (now - store.openListInteraction.lastTapTime) < doubleTapDelay) {
            // 双击检测到，关闭标签页
            this.closeOpenListTempTab(tabId);
            store.openListInteraction.lastTapTime = 0;
            store.openListInteraction.lastTapTabId = null;
        } else {
            // 第一次点击，选中标签页
            this.selectTempTab(tabId);
            store.openListInteraction.lastTapTime = now;
            store.openListInteraction.lastTapTabId = tabId;
        }
    },

    // 加载临时标签页文件 (支持列表和搜索刷新)
    async loadTempTabFiles(path, refresh = false, tabId = null) {
        if (!this.currentOpenListAccount) return;

        const targetId = tabId || store.openListActiveTempTabId;
        const tab = store.openListTempTabs.find(t => t.id === targetId);
        if (!tab) return;

        tab.path = path;
        tab.loading = true;

        try {
            let response;
            if (tab.isSearch) {
                // 如果是搜索标签页，执行搜索请求
                response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/fs/search`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        keywords: tab.keywords,
                        parent: tab.path,
                        scope: store.openListSearchScope || 0
                    })
                });
            } else {
                // 普通列表请求
                response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/fs/list`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ path, refresh: !!refresh })
                });
            }

            if (!response.ok) {
                toast.error(`加载失败 (${response.status})`);
                tab.loading = false;
                return;
            }

            const data = await response.json();
            if (data.code === 200) {
                const content = data.data.content || [];
                tab.files = content.map(f => {
                    if (typeof f.name !== 'string') f.name = String(f.name || 'unknown');
                    return f;
                });
            } else {
                toast.error('加载失败: ' + (data.message || '未知错误'));
            }
        } catch (e) {
            toast.error('请求出错: ' + e.message);
        } finally {
            tab.loading = false;
        }
    },
    // 切换搜索框展开/收起
    toggleOpenListSearch() {
        if (!store.openListSearchExpanded) {
            store.openListSearchExpanded = true;
            // 聚焦输入框
            this.$nextTick(() => {
                if (this.$refs.openListSearchInputRef) {
                    this.$refs.openListSearchInputRef.focus();
                }
            });
        } else {
            // 如果已经展开，再次点击则收起，且如果已输入内容则清空
            store.openListSearchExpanded = false;
        }
    },

    // 执行搜索 (回车触发)
    performOpenListSearch() {
        if (store.openListSearchInput && store.openListSearchInput.trim()) {
            this.searchOpenListFilesNewTab(store.openListSearchInput.trim());
        }
    },

    // 失去焦点时的处理
    handleSearchBlur() {
        // 可选：如果输入框为空，自动收起
        // if (!store.openListSearchInput) {
        //    store.openListSearchExpanded = false;
        // }
    },

    // 处理临时标签页文件点击
    handleTempTabFile(file) {
        const tab = this.currentOpenListTempTab;
        if (!tab || tab.loading) return;

        if (file.is_dir) {
            const fileName = typeof file.name === 'string' ? file.name : String(file.name || '');
            const newPath = this.getFilePath(file, tab.path);
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

    // 搜索并在新标签页展示结果
    searchOpenListFilesNewTab(keywords) {
        // 如果有参数直接用参数，否则使用 store 中的 input
        let kw = typeof keywords === 'string' ? keywords : store.openListSearchInput;
        // 如果仍然没有，则不做任何事情（由 UI 控制展开和输入）
        if (!kw || !this.currentOpenListAccount) return;

        // 关闭搜索框
        store.openListSearchExpanded = false;
        store.openListSearchInput = ''; // 可选：清空搜索框

        const id = 'search-' + Date.now();
        const newTab = {
            id,
            name: kw, // 移除名字里的 emoji，改用 icon 属性
            icon: 'fas fa-search',
            path: store.openListPath,
            isSearch: true,
            keywords: kw,
            files: [],
            loading: true
        };

        store.openListTempTabs.push(newTab);
        store.openListActiveTempTabId = id;
        this.openListSubTab = 'temp';

        // 复用 loadTempTabFiles 的搜索逻辑
        this.loadTempTabFiles(store.openListPath, false, id);
    },

    // 搜索文件 (原主界面内搜，暂保留以兼容)
    async searchOpenListFiles(keywords) {
        if (!keywords || !this.currentOpenListAccount) {
            if (store.openListSearchActive) {
                store.openListSearchActive = false;
                this.loadOpenListFiles(this.openListPath);
            }
            return;
        }

        store.openListSearchActive = true;
        this.openListFilesLoading = true;
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

    // 判断是否为图片文件
    isImageFile(filename) {
        if (!filename) return false;
        return /\.(jpg|jpeg|png|gif|webp|svg|bmp|ico)$/i.test(filename);
    },

    // 打开图片预览弹窗
    async openImagePreview(file, baseDir = store.openListPath) {
        if (!this.currentOpenListAccount) {
            toast.error('未选择 OpenList 账号');
            return;
        }

        const fullPath = this.getFilePath(file, baseDir);
        const fileName = typeof file.name === 'string' ? file.name : String(file.name || '');

        try {
            const response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/fs/get`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: fullPath })
            });

            if (!response.ok) {
                toast.error(`获取文件信息失败 (${response.status})`);
                return;
            }

            const data = await response.json();
            if (data.code === 200 && data.data.raw_url) {
                // 回归原始稳定的 raw_url，确保跨域预览成功
                const imageUrl = data.data.raw_url;

                // 显示弹窗和 loading 状态
                store.imagePreview = {
                    visible: true,
                    url: imageUrl,
                    filename: fileName,
                    path: fullPath,
                    loading: true
                };

                this._bindImagePreviewEscKey();
            } else {
                toast.error('获取文件链接失败: ' + (data.message || '未知错误'));
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
        }
    },

    // ESC 键关闭图片预览
    _bindImagePreviewEscKey() {
        // 移除旧的监听器（如果有）
        if (_imagePreviewEscHandler) {
            document.removeEventListener('keydown', _imagePreviewEscHandler);
        }
        // 创建新的监听器
        _imagePreviewEscHandler = (e) => {
            if (e.key === 'Escape' && store.imagePreview?.visible) {
                this.closeImagePreview();
            }
        };
        document.addEventListener('keydown', _imagePreviewEscHandler);
    },

    // 关闭图片预览弹窗
    closeImagePreview() {
        if (store.imagePreview) {
            store.imagePreview.visible = false;
            store.imagePreview.url = ''; // 清空 URL 停止浏览器可能的请求
        }
        // 移除 ESC 键监听
        if (_imagePreviewEscHandler) {
            document.removeEventListener('keydown', _imagePreviewEscHandler);
            _imagePreviewEscHandler = null;
        }
    },

    // 在图片预览中下载 (解决 UUID 文件名问题)
    async downloadImageInPreview() {
        if (!store.imagePreview || !store.imagePreview.url) return;

        const { url, filename } = store.imagePreview;
        toast.info('准备下载...');

        try {
            // 使用 fetch 获取 blob
            const response = await fetch(url);
            const blob = await response.blob();

            // 创建本地 URL
            const blobUrl = window.URL.createObjectURL(blob);

            // 创建链接并点击
            const a = document.createElement('a');
            a.href = blobUrl;
            a.download = filename; // 此时 download 属性生效，因为是同源 blob
            document.body.appendChild(a);
            a.click();

            // 清理
            setTimeout(() => {
                document.body.removeChild(a);
                window.URL.revokeObjectURL(blobUrl);
            }, 100);

            toast.success('开始下载');
        } catch (e) {
            console.error('[ImagePreview] Download failed:', e);
            // 失败了则尝试直接打开（兜底）
            window.open(url, '_blank');
            toast.warn('由于跨域限制，请在新窗口中另存为图片');
        }
    },

    // 图片加载完成
    onImagePreviewLoad() {
        if (store.imagePreview) {
            store.imagePreview.loading = false;
            console.log('[ImagePreview] Image loaded successfully');
        }
    },

    // 图片加载失败
    onImagePreviewError() {
        if (store.imagePreview) {
            store.imagePreview.loading = false;
            console.error('[ImagePreview] Image failed to load');
            toast.error('图片加载失败，请重试');
        }
    },

    // 播放视频文件 (在临时标签页中用播放器打开)
    async playVideoFile(file, baseDir = store.openListPath) {
        const fullPath = this.getFilePath(file, baseDir);
        const fileName = typeof file.name === 'string' ? file.name : String(file.name || '');

        try {
            toast.info('正在获取视频链接...');

            const response = await fetch(`/api/openlist/${this.currentOpenListAccount.id}/fs/get`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: fullPath })
            });

            if (!response.ok) {
                toast.error(`获取视频链接失败 (${response.status})`);
                return;
            }

            const data = await response.json();
            if (data.code === 200 && data.data.raw_url) {
                // 在临时标签页中打开播放器
                this.openVideoTempTab(fileName, data.data.raw_url);
            } else {
                toast.error('获取视频链接失败: ' + (data.message || '未知错误'));
            }
        } catch (e) {
            toast.error('获取视频失败: ' + e.message);
        }
    },

    // 打开视频播放临时标签页
    openVideoTempTab(filename, videoUrl) {
        // 查重：如果已经打开了同样文件名的视频（避免 URL 中的 sign 变化导致查重失效）
        const existingTab = store.openListTempTabs.find(t => t.isVideo && t.filename === filename);
        if (existingTab) {
            // 如果 URL 变了（比如之前的过期了），更新它
            if (existingTab.videoUrl !== videoUrl) {
                existingTab.videoUrl = videoUrl;
            }
            this.selectTempTab(existingTab.id);
            return;
        }

        const id = 'video-' + Date.now() + Math.random().toString(36).substr(2, 4);
        const newTab = {
            id,
            name: filename, // 移除名字里的 emoji
            icon: 'fas fa-play-circle',
            isVideo: true,
            videoUrl,
            filename,
            files: [],
            loading: false
        };
        store.openListTempTabs.push(newTab);
        this.selectTempTab(id);
    },

    // 在标签页中初始化播放器 (使用 Plyr)
    async initVideoPlayerInTab(tab) {
        const videoId = 'plyr-video-' + tab.id;
        const videoElement = document.getElementById(videoId);
        if (!videoElement) {
            console.error('[SelfH] Plyr video element not found:', videoId);
            return;
        }

        // 先暂停所有其他视频标签页的播放器
        store.openListTempTabs.forEach(t => {
            if (t.isVideo && t.id !== tab.id && t.plyrInstance) {
                try {
                    t.plyrInstance.pause();
                    console.log('[SelfH] Paused video in tab:', t.filename);
                } catch (e) {
                    console.warn('[SelfH] Error pausing video:', e);
                }
            }
        });

        // 如果这个标签页已经有播放器实例，直接使用，不重新初始化
        if (tab.plyrInstance) {
            console.log('[SelfH] Resuming existing Plyr instance for:', tab.filename);
            // 同步状态到 store
            if (store.streamPlayer) {
                const plyr = tab.plyrInstance;
                store.streamPlayer.duration = plyr.duration || 0;
                store.streamPlayer.currentTime = plyr.currentTime || 0;
                store.streamPlayer.playing = !plyr.paused;
                store.streamPlayer.loading = false;
            }
            return;
        }

        // 重置播放器状态
        if (store.streamPlayer) {
            store.streamPlayer.duration = 0;
            store.streamPlayer.currentTime = 0;
            store.streamPlayer.playing = false;
            store.streamPlayer.bufferedTime = 0;
            store.streamPlayer.loading = true;
        }

        try {
            // 直接创建 Plyr 实例，不使用 streamPlayer 单例
            const Plyr = (await import('plyr')).default;

            videoElement.src = tab.videoUrl;

            const plyr = new Plyr(videoElement, {
                controls: ['play-large', 'play', 'progress', 'current-time', 'duration', 'mute', 'volume', 'settings', 'pip', 'fullscreen'],
                settings: ['quality', 'speed'],
                speed: { selected: 1, options: [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] },
                keyboard: { focused: true, global: false },
                tooltips: { controls: true, seek: true },
                fullscreen: { enabled: true, fallback: true },
                hideControls: true,
                clickToPlay: true,
                i18n: {
                    restart: '重新播放', play: '播放', pause: '暂停',
                    fastForward: '快进 {seektime}s', rewind: '后退 {seektime}s',
                    seek: '跳转', currentTime: '当前时间', duration: '总时长',
                    volume: '音量', mute: '静音', unmute: '取消静音',
                    enterFullscreen: '全屏', exitFullscreen: '退出全屏',
                    settings: '设置', speed: '速度', normal: '正常', pip: '画中画'
                }
            });

            // 存储到标签页对象
            tab.plyrInstance = plyr;

            // 绑定事件到 store
            plyr.on('play', () => {
                if (store.streamPlayer) store.streamPlayer.playing = true;
            });
            plyr.on('pause', () => {
                if (store.streamPlayer) store.streamPlayer.playing = false;
            });
            plyr.on('timeupdate', () => {
                if (store.streamPlayer) {
                    store.streamPlayer.currentTime = plyr.currentTime;
                    store.streamPlayer.duration = plyr.duration;
                }
            });
            plyr.on('loadedmetadata', () => {
                if (store.streamPlayer) {
                    store.streamPlayer.duration = plyr.duration;
                    store.streamPlayer.loading = false;
                }
            });
            plyr.on('waiting', () => {
                if (store.streamPlayer) store.streamPlayer.loading = true;
            });
            plyr.on('canplay', () => {
                if (store.streamPlayer) store.streamPlayer.loading = false;
            });
            plyr.on('volumechange', () => {
                if (store.streamPlayer) {
                    store.streamPlayer.volume = plyr.volume;
                    store.streamPlayer.muted = plyr.muted;
                }
            });

            // 自动播放
            await plyr.play();
            console.log('[SelfH] Plyr initialized successfully for:', tab.filename);
        } catch (e) {
            console.error('[SelfH] Failed to init Plyr:', e);
        }
    },

    // 销毁视频播放器
    destroyVideoPlayerInTab(tab) {
        if (tab && tab.plyrInstance) {
            try {
                tab.plyrInstance.destroy();
            } catch (e) {
                console.warn('[SelfH] Error destroying Plyr:', e);
            }
            tab.plyrInstance = null;
        }
    },

    // 内部事件绑定（用于控制条交互等）
    bindInternalVideoEvents(video) {
        const syncMetadata = () => {
            if (!video || !store.streamPlayer) return;
            store.streamPlayer.duration = video.duration || 0;
            store.streamPlayer.currentTime = video.currentTime || 0;
        };

        // 基础元数据绑定
        video.onloadedmetadata = syncMetadata;

        // 如果当前已经有元数据（比如切换回标签页时），手动同步一次
        if (video.readyState >= 1) {
            syncMetadata();
        }

        // 关键：如果已经可以播放，立即关闭加载图标
        if (video.readyState >= 3) {
            store.streamPlayer.loading = false;
        }

        video.onplay = () => { store.streamPlayer.playing = true; };
        video.onplaying = () => { store.streamPlayer.loading = false; };
        video.oncanplay = () => { store.streamPlayer.loading = false; };
        video.onwaiting = () => { store.streamPlayer.loading = true; };
        video.onpause = () => { store.streamPlayer.playing = false; };
        video.ontimeupdate = () => {
            store.streamPlayer.currentTime = video.currentTime;
            // 兜底：如果播放了还在 loading，强制关闭
            if (video.currentTime > 0 && store.streamPlayer.loading) {
                store.streamPlayer.loading = false;
            }
        };
        video.onvolumechange = () => {
            if (!store.streamPlayer) return;
            store.streamPlayer.volume = video.volume;
            store.streamPlayer.muted = video.muted;
        };
        video.onprogress = () => {
            if (!store.streamPlayer) return;
            if (video.buffered.length > 0) {
                store.streamPlayer.bufferedTime = video.buffered.end(video.buffered.length - 1);
            }
        };
    },

    // 视频控制处理 (对应 HTML 中的 @click 等)
    handleVideoMouseMove() {
        if (!store.streamPlayer) return;
        if (store.streamPlayer.hideTimer) clearTimeout(store.streamPlayer.hideTimer);
        store.streamPlayer.showControls = true;
        // 只有正在播放时才自动隐藏
        if (store.streamPlayer.playing) {
            store.streamPlayer.hideTimer = setTimeout(() => {
                if (store.streamPlayer) store.streamPlayer.showControls = false;
            }, 3000);
        }
    },

    handleVideoClick(e) {
        if (!store.streamPlayer) return;

        // 如果点击的是控制栏或进度条，由它们自己的事件处理
        if (e.target.closest('.stream-player-controls')) return;

        // 兼容触摸和鼠标坐标获取
        const getX = () => {
            if (e.touches && e.touches.length > 0) return e.touches[0].clientX;
            if (e.changedTouches && e.changedTouches.length > 0) return e.changedTouches[0].clientX;
            return e.clientX;
        };

        const now = Date.now();
        const delay = 300;

        // 处理双击
        if (now - (store.streamPlayer.lastTapTime || 0) < delay) {
            if (store.streamPlayer.tapTimer) {
                clearTimeout(store.streamPlayer.tapTimer);
                store.streamPlayer.tapTimer = null;
            }
            store.streamPlayer.lastTapTime = 0;

            const rect = e.currentTarget.getBoundingClientRect();
            const x = getX() - rect.left;
            const width = rect.width;

            if (x < width * 0.3) {
                this.skipVideo(-10);
                this._showVideoAnimation('seek', '-10s');
            } else if (x > width * 0.7) {
                this.skipVideo(10);
                this._showVideoAnimation('seek', '+10s');
            } else {
                this.toggleVideoPlay();
                this._showVideoAnimation(store.streamPlayer.playing ? 'play' : 'pause', store.streamPlayer.playing ? '播放' : '暂停');
            }
            return;
        }

        // 处理单击
        store.streamPlayer.lastTapTime = now;
        store.streamPlayer.tapTimer = setTimeout(() => {
            if (!store.streamPlayer) return;
            store.streamPlayer.showControls = !store.streamPlayer.showControls;
            if (store.streamPlayer.showControls) {
                this.handleVideoMouseMove();
            }
            store.streamPlayer.tapTimer = null;
        }, delay);
    },

    _showVideoAnimation(type, text = '') {
        if (!store.streamPlayer) return;
        store.streamPlayer.animationType = type;
        store.streamPlayer.animationText = text;

        if (store.streamPlayer.animTimer) clearTimeout(store.streamPlayer.animTimer);
        store.streamPlayer.animTimer = setTimeout(() => {
            if (store.streamPlayer) store.streamPlayer.animationType = null;
        }, 1000);
    },

    // 获取当前视频标签页的 Plyr 实例
    getCurrentPlyr() {
        const tab = this.currentOpenListTempTab;
        return (tab && tab.isVideo && tab.plyrInstance) ? tab.plyrInstance : null;
    },

    // 获取当前视频标签页的视频元素
    getCurrentVideoElement() {
        const plyr = this.getCurrentPlyr();
        return plyr ? plyr.media : null;
    },

    handleProgressMouseDown(e) {
        const video = this.getCurrentVideoElement();
        if (!video || !store.streamPlayer.duration) return;

        const isTouch = e.type.startsWith('touch');
        const target = e.currentTarget;
        const container = target.closest('.stream-player-container');

        store.streamPlayer.isDragging = true;
        if (container) container.classList.add('dragging');

        const update = (ex) => {
            const rect = target.getBoundingClientRect();
            const clientX = (isTouch && ex.touches) ? ex.touches[0].clientX : (ex.clientX || (ex.changedTouches && ex.changedTouches[0].clientX));
            const pos = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
            store.streamPlayer.dragTime = pos * store.streamPlayer.duration;
        };

        update(e);

        if (isTouch) {
            const onTouchMove = (te) => {
                if (te.cancelable) te.preventDefault();
                update(te);
            };
            const onTouchEnd = () => {
                const plyr = this.getCurrentPlyr();
                if (plyr) plyr.currentTime = store.streamPlayer.dragTime;
                store.streamPlayer.isDragging = false;
                if (container) container.classList.remove('dragging');
                document.removeEventListener('touchmove', onTouchMove);
                document.removeEventListener('touchend', onTouchEnd);
            };
            document.addEventListener('touchmove', onTouchMove, { passive: false });
            document.addEventListener('touchend', onTouchEnd);
        } else {
            const onMouseMove = (me) => update(me);
            const onMouseUp = () => {
                const plyr = this.getCurrentPlyr();
                if (plyr) plyr.currentTime = store.streamPlayer.dragTime;
                store.streamPlayer.isDragging = false;
                if (container) container.classList.remove('dragging');
                document.removeEventListener('mousemove', onMouseMove);
                document.removeEventListener('mouseup', onMouseUp);
            };
            document.addEventListener('mousemove', onMouseMove);
            document.addEventListener('mouseup', onMouseUp);
        }
    },

    handleVolumeMouseDown(e) {
        const update = (ex) => {
            const rect = e.currentTarget.getBoundingClientRect();
            const vol = Math.max(0, Math.min(1, (ex.clientX - rect.left) / rect.width));
            const plyr = this.getCurrentPlyr();
            if (plyr) plyr.volume = vol;
        };
        update(e);
        const onMouseMove = (me) => update(me);
        const onMouseUp = () => {
            document.removeEventListener('mousemove', onMouseMove);
            document.removeEventListener('mouseup', onMouseUp);
        };
        document.addEventListener('mousemove', onMouseMove);
        document.addEventListener('mouseup', onMouseUp);
    },

    // 视频控制代理方法
    toggleVideoPlay() {
        const plyr = this.getCurrentPlyr();
        if (plyr) plyr.togglePlay();
    },

    skipVideo(seconds) {
        const plyr = this.getCurrentPlyr();
        if (plyr) plyr.forward(seconds);
    },

    toggleMute() {
        const video = this.getCurrentVideoElement();
        if (video) {
            video.muted = !video.muted;
        }
    },

    openVideoInNewTab() {
        const tab = this.currentOpenListTempTab;
        if (tab && tab.videoUrl) {
            window.open(tab.videoUrl, '_blank');
        }
    },

    openExternalPlayer() {
        const tab = this.currentOpenListTempTab;
        if (!tab || !tab.videoUrl) return;

        const ua = navigator.userAgent.toLowerCase();
        const isMobile = /iphone|ipad|ipod|android/.test(ua);

        // 如果内部正在播放，先暂停
        const plyr = this.getCurrentPlyr();
        if (plyr && !plyr.paused) {
            plyr.pause();
        }

        if (isMobile) {
            // 尝试唤起移动端播放器 (UC)
            this.openInUCBrowser(tab.videoUrl);
            toast.info('尝试唤起移动端播放器...');
        } else {
            // PC 端尝试调用 PotPlayer
            const potUrl = `potplayer://${tab.videoUrl}`;
            const a = document.createElement('a');
            a.href = potUrl;
            a.style.display = 'none';
            document.body.appendChild(a);
            a.click();
            setTimeout(() => {
                if (document.body.contains(a)) document.body.removeChild(a);
            }, 100);
            toast.info('尝试调用 PotPlayer... 如果没反应请确保已开启关联');
        }
    },

    openInUCBrowser(url) {
        // UC 浏览器在移动端的调用方式通常是 android intent 或者特殊的 schema
        const ua = navigator.userAgent.toLowerCase();

        if (/android/.test(ua)) {
            // 根据用户提供的截图，优先尝试拉起 UC 浏览器国际版 (com.UCMobile.intl) 的视频播放组件
            // Activity: com.UCMobile.main.UCMobile.alias.video
            const ucIntlIntent = `intent:${url}#Intent;action=android.intent.action.VIEW;package=com.UCMobile.intl;component=com.UCMobile.intl/com.UCMobile.main.UCMobile.alias.video;S.ext_video_url=${url};S.browser_fallback_url=${url};end`;

            // 备选方案：通用 VIEW（系统会让用户选择合适的 App，包括 UC 国内版或其他播放器）
            const genericIntentUrl = `intent:${url}#Intent;action=android.intent.action.VIEW;S.ext_video_url=${url};S.browser_fallback_url=${url};end`;

            // 优先执行精准唤起
            window.location.href = ucIntlIntent;

            // 如果精准唤起失败（1.5秒后页面还在前台），尝试弹窗选择
            setTimeout(() => {
                if (document.visibilityState === 'visible') {
                    window.location.href = genericIntentUrl;
                }
            }, 1500);
        } else if (/iphone|ipad|ipod/.test(ua)) {
            // iOS 上的 UC 浏览器
            const ucUrl = `ucbrowser://${url}`;
            window.location.href = ucUrl;
        } else {
            // 兜底直接打开链接
            window.open(url, '_blank');
        }
    },

    getVolumeIcon() {
        if (store.streamPlayer.muted || store.streamPlayer.volume === 0) return 'fa-volume-mute';
        if (store.streamPlayer.volume < 0.5) return 'fa-volume-down';
        return 'fa-volume-up';
    },

    formatVideoTime(seconds) {
        return streamPlayer.formatTime(seconds);
    },

    getBufferedPercent() {
        if (!store.streamPlayer || !store.streamPlayer.duration) return 0;
        return (store.streamPlayer.bufferedTime / store.streamPlayer.duration) * 100;
    },

    getPlayedPercent() {
        if (!store.streamPlayer || !store.streamPlayer.duration) return 0;
        const time = store.streamPlayer.isDragging ? store.streamPlayer.dragTime : store.streamPlayer.currentTime;
        return (time / store.streamPlayer.duration) * 100;
    },

    formatVideoTime(seconds) {
        return streamPlayer.formatTime(seconds);
    },

    setVideoPlaybackRate(rate) {
        const plyr = this.getCurrentPlyr();
        if (plyr) plyr.speed = rate;
    },

    toggleVideoPiP() {
        const plyr = this.getCurrentPlyr();
        if (plyr) plyr.pip = !plyr.pip;
    },

    toggleVideoFullscreen() {
        const plyr = this.getCurrentPlyr();
        if (plyr) plyr.fullscreen.toggle();
    },

    // 下载文件
    async downloadOpenListFile(file, baseDir = store.openListPath) {
        const fullPath = this.getFilePath(file, baseDir);
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
        const fullPath = this.getFilePath(file, baseDir);
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

    // 切换子标签页 (files, settings, cron, temp)
    switchOpenListTab(tabName) {
        // 如果即将离开临时标签页（即离开视频播放），暂停视频但不销毁
        if (store.openListSubTab === 'temp' && tabName !== 'temp') {
            const plyr = this.getCurrentPlyr();
            if (plyr && !plyr.paused) {
                plyr.pause();
            }
        }
        store.openListSubTab = tabName;

        // 如果切换到定时任务，自动加载
        if (tabName === 'cron') {
            this.loadCronTasks();
            this.loadCronLogs();
        }
    },

    // 切换视图模式 (列表/网格)
    toggleOpenListLayout(mode) {
        if (['list', 'grid'].includes(mode)) {
            store.openListLayoutMode = mode;
            // 可选：持久化保存
            localStorage.setItem('openListLayoutMode', mode);
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
    },

    // ==================== 右键菜单 ====================

    // 显示右键菜单
    showFileContextMenu(e, file, baseDir = store.openListPath) {
        e.preventDefault();
        e.stopPropagation();

        // 计算菜单位置
        let x = e.clientX || e.touches?.[0]?.clientX || 0;
        let y = e.clientY || e.touches?.[0]?.clientY || 0;

        // 边界检测：确保菜单不会超出视口
        const menuWidth = 160;
        const menuHeight = 180; // 估算高度

        if (x + menuWidth > window.innerWidth) {
            x = window.innerWidth - menuWidth - 10;
        }
        if (y + menuHeight > window.innerHeight) {
            y = window.innerHeight - menuHeight - 10;
        }

        store.openListContextMenu.visible = true;
        store.openListContextMenu.x = x;
        store.openListContextMenu.y = y;
        store.openListContextMenu.file = file;
        store.openListContextMenu.baseDir = baseDir;

        // 添加点击外部关闭
        if (!this.boundCloseContextMenu) {
            this.boundCloseContextMenu = this.closeContextMenuOnClick.bind(this);
        }
        setTimeout(() => {
            document.addEventListener('click', this.boundCloseContextMenu);
            document.addEventListener('contextmenu', this.boundCloseContextMenu);
        }, 10);
    },

    // 隐藏右键菜单
    hideFileContextMenu() {
        store.openListContextMenu.visible = false;
        store.openListContextMenu.file = null;
        if (this.boundCloseContextMenu) {
            document.removeEventListener('click', this.boundCloseContextMenu);
            document.removeEventListener('contextmenu', this.boundCloseContextMenu);
        }
    },

    // 点击外部关闭菜单
    closeContextMenuOnClick(e) {
        const menu = document.querySelector('.openlist-context-menu');
        if (menu && !menu.contains(e.target)) {
            this.hideFileContextMenu();
        }
    },

    // 处理菜单操作
    handleFileContextAction(action) {
        const { file, baseDir } = store.openListContextMenu;
        if (!file) return;

        this.hideFileContextMenu();

        switch (action) {
            case 'open':
                this.handleOpenFile(file);
                break;
            case 'open-new-tab':
                if (file.is_dir) {
                    const fileName = typeof file.name === 'string' ? file.name : String(file.name || '');
                    const newPath = this.getFilePath(file, baseDir);
                    this.openTempTab(fileName, newPath);
                }
                break;
            case 'download':
                this.downloadOpenListFile(file, baseDir);
                break;
            case 'rename':
                this.renameOpenListFile(file);
                break;
            case 'delete':
                this.deleteOpenListFile(file);
                break;
            case 'detail':
                this.showOpenFileDetail(file, baseDir);
                break;
        }
    },

    handleFileTouchStart(e, file, baseDir = store.openListPath) {
        store.openListInteraction.longPressTriggered = false;
        store.openListInteraction.longPressTimer = setTimeout(() => {
            store.openListInteraction.longPressTriggered = true;
            // 触发震动反馈
            if (navigator.vibrate) {
                navigator.vibrate(30);
            }
            this.showFileContextMenu(e, file, baseDir);
        }, 500); // 500ms 长按
    },

    handleFileTouchEnd(e) {
        if (store.openListInteraction.longPressTimer) {
            clearTimeout(store.openListInteraction.longPressTimer);
            store.openListInteraction.longPressTimer = null;
        }
        // 如果长按已触发，阻止默认点击行为
        if (store.openListInteraction.longPressTriggered) {
            e.preventDefault();
            store.openListInteraction.longPressTriggered = false;
        }
    },

    handleFileTouchMove() {
        // 移动则取消长按
        if (store.openListInteraction.longPressTimer) {
            clearTimeout(store.openListInteraction.longPressTimer);
            store.openListInteraction.longPressTimer = null;
        }
    },

    // 格式化文件大小 (补足，防止主程序找不到)
    formatFileSize(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    },

    // 格式化日期 (补足)
    formatDateTime(dateStr) {
        if (!dateStr) return '-';
        const date = new Date(dateStr);
        return date.toLocaleString();
    },

    // ==================== 定时任务 (Cron) ====================

    async loadCronTasks() {
        store.cronLoading = true;
        try {
            const res = await fetch('/api/cron/tasks');
            const data = await res.json();
            if (data.success) {
                store.cronTasks = data.data;
            } else {
                toast.error('加载任务失败: ' + data.error);
            }
        } catch (e) {
            toast.error('加载任务出错: ' + e.message);
        } finally {
            store.cronLoading = false;
        }
    },

    openCronEditModal(task = null) {
        if (task) {
            store.cronEditingTask = JSON.parse(JSON.stringify(task)); // Deep copy
            // 解析现有的 cron 表达式到简化设置
            this.parseCronToSimple(store.cronEditingTask);
        } else {
            store.cronEditingTask = {
                name: '',
                schedule: '0 0 * * *',
                command: '',
                type: 'shell',
                enabled: 1,
                // 简化设置默认值
                useCustom: false,
                periodType: 'day',
                weekday: '1',
                dayOfMonth: 1,
                hour: 0,
                minute: 0,
                nextRuns: []
            };
        }
        // 自动预览
        this.previewCronSchedule();
    },

    // 解析 Cron 表达式到简化设置
    parseCronToSimple(task) {
        if (!task.schedule) return;

        const parts = task.schedule.split(' ');
        if (parts.length !== 5) {
            task.useCustom = true;
            return;
        }

        const [minute, hour, dayOfMonth, month, weekday] = parts;

        // 尝试识别简单模式
        if (minute === '*' && hour === '*' && dayOfMonth === '*' && month === '*' && weekday === '*') {
            task.periodType = 'minute';
            task.useCustom = false;
        } else if (hour === '*' && dayOfMonth === '*' && month === '*' && weekday === '*' && /^\d+$/.test(minute)) {
            task.periodType = 'hour';
            task.minute = parseInt(minute);
            task.useCustom = false;
        } else if (dayOfMonth === '*' && month === '*' && weekday === '*' && /^\d+$/.test(minute) && /^\d+$/.test(hour)) {
            task.periodType = 'day';
            task.minute = parseInt(minute);
            task.hour = parseInt(hour);
            task.useCustom = false;
        } else if (dayOfMonth === '*' && month === '*' && /^\d+$/.test(weekday) && /^\d+$/.test(minute) && /^\d+$/.test(hour)) {
            task.periodType = 'week';
            task.minute = parseInt(minute);
            task.hour = parseInt(hour);
            task.weekday = weekday;
            task.useCustom = false;
        } else if (month === '*' && weekday === '*' && /^\d+$/.test(dayOfMonth) && /^\d+$/.test(minute) && /^\d+$/.test(hour)) {
            task.periodType = 'month';
            task.minute = parseInt(minute);
            task.hour = parseInt(hour);
            task.dayOfMonth = parseInt(dayOfMonth);
            task.useCustom = false;
        } else {
            // 复杂表达式，使用自定义模式
            task.useCustom = true;
        }

        // 设置默认值
        if (task.minute === undefined) task.minute = 0;
        if (task.hour === undefined) task.hour = 0;
        if (task.weekday === undefined) task.weekday = '1';
        if (task.dayOfMonth === undefined) task.dayOfMonth = 1;
    },

    // 根据简化设置更新 Cron 表达式
    updateCronFromSimple() {
        const task = store.cronEditingTask;
        if (!task) return;

        // 如果手动切换回自定义，不执行后续逻辑
        if (task.useCustom) {
            this.previewCronSchedule();
            return;
        }

        const minute = task.minute !== undefined ? task.minute : 0;
        const hour = task.hour !== undefined ? task.hour : 0;
        const weekday = task.weekday || '1';
        const dayOfMonth = task.dayOfMonth || 1;

        switch (task.periodType) {
            case 'minute':
                task.schedule = '* * * * *';
                break;
            case 'hour':
                task.schedule = `${minute} * * * *`;
                break;
            case 'day':
                task.schedule = `${minute} ${hour} * * *`;
                break;
            case 'week':
                task.schedule = `${minute} ${hour} * * ${weekday}`;
                break;
            case 'month':
                task.schedule = `${minute} ${hour} ${dayOfMonth} * *`;
                break;
            default:
                task.schedule = '0 0 * * *';
        }

        // 自动预览
        this.previewCronSchedule();
    },

    // 预览未来 5 次执行时间
    async previewCronSchedule() {
        const task = store.cronEditingTask;
        if (!task || !task.schedule) return;

        // 防止频繁触发
        if (this._previewTimer) clearTimeout(this._previewTimer);
        this._previewTimer = setTimeout(() => {
            try {
                const nextRuns = [];
                const cronParts = task.schedule.trim().split(/\s+/);
                if (cronParts.length !== 5) {
                    task.nextRuns = [];
                    return;
                }

                const now = new Date();
                let current = new Date(now);

                for (let i = 0; i < 5; i++) {
                    const next = this.getNextCronRun(task.schedule, current);
                    if (next) {
                        nextRuns.push(next.toLocaleString('zh-CN', {
                            year: 'numeric',
                            month: '2-digit',
                            day: '2-digit',
                            hour: '2-digit',
                            minute: '2-digit',
                            hour12: false
                        }));
                        current = new Date(next.getTime() + 60000); // 寻找下一个分钟
                    } else {
                        break;
                    }
                }

                task.nextRuns = nextRuns;
            } catch (e) {
                console.warn('Cron Preview Error:', e);
                task.nextRuns = [];
            }
        }, 100);
    },

    // 计算下一次 Cron 执行时间（简单实现）
    getNextCronRun(schedule, from) {
        const [minute, hour, dayOfMonth, month, dayOfWeek] = schedule.split(' ');

        let candidate = new Date(from);
        candidate.setSeconds(0);
        candidate.setMilliseconds(0);

        // 最多尝试 366 天
        for (let attempt = 0; attempt < 366 * 24 * 60; attempt++) {
            candidate = new Date(candidate.getTime() + 60000);

            if (!this.cronFieldMatch(minute, candidate.getMinutes())) continue;
            if (!this.cronFieldMatch(hour, candidate.getHours())) continue;
            if (!this.cronFieldMatch(dayOfMonth, candidate.getDate())) continue;
            if (!this.cronFieldMatch(month, candidate.getMonth() + 1)) continue;
            if (!this.cronFieldMatch(dayOfWeek, candidate.getDay())) continue;

            return candidate;
        }
        return null;
    },

    // 检查 Cron 字段是否匹配
    cronFieldMatch(field, value) {
        if (field === '*') return true;

        // 处理逗号分隔
        if (field.includes(',')) {
            return field.split(',').some(v => this.cronFieldMatch(v, value));
        }

        // 处理范围
        if (field.includes('-')) {
            const [start, end] = field.split('-').map(Number);
            return value >= start && value <= end;
        }

        // 处理步进
        if (field.includes('/')) {
            const [base, step] = field.split('/');
            const stepNum = parseInt(step);
            if (base === '*') {
                return value % stepNum === 0;
            }
            const baseNum = parseInt(base);
            return (value - baseNum) % stepNum === 0 && value >= baseNum;
        }

        return parseInt(field) === value;
    },

    closeCronEditModal() {
        store.cronEditingTask = null;
    },

    async saveCronTask() {
        const task = store.cronEditingTask;
        if (!task.name || !task.schedule || !task.command) {
            return toast.error('请填写完整信息');
        }

        try {
            const url = task.id ? `/api/cron/tasks/${task.id}` : '/api/cron/tasks';
            const method = task.id ? 'PUT' : 'POST';

            const res = await fetch(url, {
                method: method,
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(task)
            });
            const data = await res.json();

            if (data.success) {
                toast.success('保存成功');
                this.closeCronEditModal();
                this.loadCronTasks();
            } else {
                toast.error('保存失败: ' + data.error);
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
        }
    },

    async deleteCronTask(task) {
        if (!confirm(`确定要删除任务 "${task.name}" 吗？`)) return;

        try {
            const res = await fetch(`/api/cron/tasks/${task.id}`, { method: 'DELETE' });
            const data = await res.json();
            if (data.success) {
                toast.success('已删除');
                this.loadCronTasks();
            } else {
                toast.error('删除失败: ' + data.error);
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
        }
    },

    async toggleCronTask(task) {
        try {
            const newStatus = task.enabled ? 0 : 1;
            const res = await fetch(`/api/cron/tasks/${task.id}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ enabled: newStatus })
            });
            const data = await res.json();
            if (data.success) {
                task.enabled = newStatus;
                toast.success(newStatus ? '任务已启用' : '任务已禁用');
            } else {
                toast.error('操作失败: ' + data.error);
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
        }
    },

    async runCronTask(task) {
        try {
            toast.info('正在触发任务...');
            const res = await fetch(`/api/cron/tasks/${task.id}/run`, { method: 'POST' });
            const data = await res.json();
            if (data.success) {
                toast.success('任务已开始执行');
                setTimeout(() => this.loadCronLogs(), 1000);
            } else {
                toast.error('执行失败: ' + data.error);
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
        }
    },

    async loadCronLogs(taskId = null) {
        const url = taskId ? `/api/cron/logs?task_id=${taskId}` : '/api/cron/logs';
        try {
            const res = await fetch(url);
            const data = await res.json();
            if (data.success) {
                store.cronLogs = data.data;
            }
        } catch (e) {
            console.error('Failed to load logs', e);
        }
    },

    async clearCronLogs(days = 7) {
        if (!confirm(`确定要清理 ${days} 天前的日志吗？`)) return;
        try {
            const res = await fetch(`/api/cron/logs?days=${days}`, { method: 'DELETE' });
            const data = await res.json();
            if (data.success) {
                toast.success('日志已清理');
                this.loadCronLogs();
            } else {
                toast.error('清理失败: ' + data.error);
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
        }
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
