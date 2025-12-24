import { toast } from './toast.js';

/**
 * SSH 分屏管理模块 (重构版)
 * 
 * 核心设计原则:
 * 1. 简化状态: 只维护 visibleSessionIds 数组
 * 2. 布局自动计算: 根据 visibleSessionIds.length 自动决定布局
 * 3. DOM 同步可靠: 使用 Vue 响应式 + 延迟同步
 * 4. 拖拽简化: 只支持基本的分屏/替换操作
 */

export const sshSplitMethods = {
    /**
     * 获取指定 ID 的会话
     */
    getSessionById(id) {
        return (this.sshSessions || []).find(s => s.id === id);
    },

    // ==================== 核心分屏 API ====================

    /**
     * 添加会话到分屏视图
     * @param {string} sessionId - 要添加的会话 ID
     * @param {string} position - 添加位置: 'left', 'right', 'top', 'bottom'
     */
    addToSplitView(sessionId, position = 'right') {
        if (!sessionId) return;

        // 如果已经在分屏视图中，则直接激活该分屏并退出
        if (this.visibleSessionIds.includes(sessionId)) {
            this.activeSSHSessionId = sessionId;
            return;
        }

        // 最多支持 9 个窗格
        if (this.visibleSessionIds.length >= 9) {
            if (toast && toast.warning) toast.warning('最多支持 9 个分屏窗格');
            return;
        }

        // 如果当前是单屏模式，先初始化 visibleSessionIds
        if (this.sshViewLayout === 'single') {
            this.visibleSessionIds = this.activeSSHSessionId
                ? [this.activeSSHSessionId]
                : [];
        }

        // 根据位置插入
        if (position === 'left' || position === 'top') {
            this.visibleSessionIds.unshift(sessionId);
        } else {
            this.visibleSessionIds.push(sessionId);
        }

        // 自动设置布局
        this._updateLayoutMode(position);
        this.activeSSHSessionId = sessionId;

        // 同步 DOM
        this._scheduleSync();
    },

    /**
     * 从分屏视图移除会话
     * @param {string} sessionId - 要移除的会话 ID
     */
    removeFromSplitView(sessionId) {
        const index = this.visibleSessionIds.indexOf(sessionId);
        if (index === -1) return;

        this.visibleSessionIds.splice(index, 1);

        // 如果只剩一个或没有，恢复单屏
        if (this.visibleSessionIds.length <= 1) {
            this._resetToSingle();
        } else {
            this._updateLayoutMode();
            this._scheduleSync();
        }

        // 如果关闭的是当前激活的，切换到其他
        if (this.activeSSHSessionId === sessionId && this.visibleSessionIds.length > 0) {
            this.activeSSHSessionId = this.visibleSessionIds[0];
        }
    },

    /**
     * 重置为单屏模式
     */
    _resetToSingle() {
        // 保存终端到仓库
        this._saveToWarehouse();

        this.sshViewLayout = 'single';
        this.visibleSessionIds = [];

        this._scheduleSync();
    },

    /**
     * 根据窗格数量和位置自动更新布局模式
     */
    _updateLayoutMode(position = 'right', targetIndex = -1) {
        const count = this.visibleSessionIds.length;

        if (count <= 1) {
            this.sshViewLayout = 'single';
            this.sshSplitSide = '';
        } else if (count === 2) {
            this.sshViewLayout = (position === 'top' || position === 'bottom')
                ? 'split-v'
                : 'split-h';
            this.sshSplitSide = '';
        } else if (count === 3) {
            // 3 屏的核心逻辑：如果已经是 grid 模式（如从 4 屏回退），则优先维持 grid 模式
            const isVerticalAction = position === 'top' || position === 'bottom';
            const wasGrid = this.sshViewLayout === 'grid';

            if (isVerticalAction || wasGrid) {
                this.sshViewLayout = 'grid';
                // 如果没有明确的方向偏好，默认使用 'right' (左 1 右 2)
                if (targetIndex === 0) {
                    this.sshSplitSide = 'left';
                } else {
                    this.sshSplitSide = 'right';
                }
            } else {
                // 仅在明确的左右拆分且非 Grid 背景下，才使用纵向三分屏
                this.sshViewLayout = 'grid-v';
                this.sshSplitSide = '';
            }
        } else {
            // 4-9 屏：进入密集网格模式
            this.sshViewLayout = 'grid';
            this.sshSplitSide = '';
        }

        // 每次布局更新后，更新快照并整理标签
        this._updateGroupState();
        this._organizeTabs();
    },

    /**
     * 重新整理标签顺序：基于快照或当前视图
     */
    _organizeTabs() {
        if (!this.sshSessions || this.sshSessions.length <= 1) return;

        // 优先使用快照中的 ID，保证即使切出去了，标签组顺序也不变
        const groupIds = this.sshGroupState ? this.sshGroupState.ids : (this.visibleSessionIds || []);
        if (groupIds.length <= 1) return;

        const groupSessions = [];
        const otherSessions = [];

        groupIds.forEach(id => {
            const session = this.getSessionById(id);
            if (session) groupSessions.push(session);
        });

        this.sshSessions.forEach(s => {
            if (!groupIds.includes(s.id)) otherSessions.push(s);
        });

        this.sshSessions = [...groupSessions, ...otherSessions];
    },

    /**
     * 更新分屏组快照
     */
    _updateGroupState() {
        // 只有当真正处于分屏模式且有多个窗格时，才更新快照
        if (this.sshViewLayout !== 'single' && this.visibleSessionIds.length > 1) {
            this.sshGroupState = {
                ids: [...this.visibleSessionIds],
                layout: this.sshViewLayout,
                side: this.sshSplitSide
            };
        } else if (this.visibleSessionIds.length <= 1) {
            // 如果物理上只剩一个窗格，且并非是“暂时切出”状态，则销毁快照
        }
    },

    /**
     * 恢复分屏视图（从快照）
     */
    _restoreGroupView() {
        if (!this.sshGroupState) return;

        this.visibleSessionIds = [...this.sshGroupState.ids];
        this.sshViewLayout = this.sshGroupState.layout;
        this.sshSplitSide = this.sshGroupState.side;
    },

    /**
     * 切出到单屏模式（挂起分屏）
     * 仅改变 visibleSessionIds 为当前单个，且不清除 sshGroupState
     */
    _switchOutToSingle(targetSessionId) {
        this.visibleSessionIds = [targetSessionId];
        this.sshViewLayout = 'single';
        this.sshSplitSide = '';
    },

    // ==================== 拖拽处理 ====================

    /**
     * 标签拖拽开始
     */
    onTabDragStart(sessionId, event) {
        this.draggedSessionId = sessionId;
        this.dropHint = '';
        this.dropTargetId = null;

        if (event?.dataTransfer) {
            event.dataTransfer.effectAllowed = 'move';
            event.dataTransfer.setData('text/plain', sessionId);
        }
    },

    /**
     * 标签拖拽结束
     */
    onTabDragEnd() {
        this.draggedSessionId = null;
        this.dropHint = '';
        this.dropTargetId = null;
    },

    /**
     * 设置拖拽提示
     */
    onDropZoneEnter(position, targetId) {
        this.dropHint = position;
        this.dropTargetId = targetId;
    },

    /**
     * 清除拖拽提示
     */
    onDropZoneLeave() {
        this.dropHint = '';
        this.dropTargetId = null;
    },

    /**
     * 处理放置操作
     */
    onDrop(targetId, position) {
        const effectivePosition = position || this.dropHint || 'center';
        const draggedId = this.draggedSessionId;

        console.log(`[SSH Split] Drop: dragged=${draggedId}, target=${targetId}, pos=${effectivePosition}`);

        if (!draggedId) {
            this.onTabDragEnd();
            return;
        }

        const draggedSession = this.getSessionById(draggedId);
        if (!draggedSession) {
            console.warn('[SSH Split] Dragged session not found:', draggedId);
            this.onTabDragEnd();
            return;
        }

        if (!this.visibleSessionIds.includes(draggedId)) {
            const isDuplicate = this.visibleSessionIds.some(id => {
                const s = this.getSessionById(id);
                return s && s.server.id === draggedSession.server.id && id !== targetId;
            });
            if (isDuplicate && effectivePosition !== 'center') {
                if (toast && toast.info) toast.info('该服务器已在分屏显示中');
                this.onTabDragEnd();
                return;
            }
        }

        if (effectivePosition === 'center' && targetId) {
            this._replaceInSplit(targetId, draggedId);
        } else if (this.sshViewLayout === 'single') {
            this.addToSplitView(draggedId, effectivePosition);
        } else {
            this._insertInSplit(draggedId, targetId, effectivePosition);
        }

        this.onTabDragEnd();
        this._scheduleSync();
    },

    /**
     * 替换分屏中的会话
     */
    _replaceInSplit(targetId, newId) {
        const index = this.visibleSessionIds.indexOf(targetId);
        if (index !== -1) {
            const newIndex = this.visibleSessionIds.indexOf(newId);
            if (newIndex !== -1) {
                this.visibleSessionIds[newIndex] = targetId;
            }
            this.visibleSessionIds[index] = newId;
        }
        this.activeSSHSessionId = newId;
    },

    /**
     * 在分屏中插入会话
     */
    _insertInSplit(sessionId, targetId, position) {
        const existingIndex = this.visibleSessionIds.indexOf(sessionId);
        if (existingIndex !== -1) {
            this.visibleSessionIds.splice(existingIndex, 1);
        }

        const targetIndex = targetId ? this.visibleSessionIds.indexOf(targetId) : -1;
        let insertAt = this.visibleSessionIds.length;
        if (targetIndex !== -1) {
            insertAt = (position === 'right' || position === 'bottom')
                ? targetIndex + 1
                : targetIndex;
        } else {
            insertAt = (position === 'left' || position === 'top') ? 0 : this.visibleSessionIds.length;
        }

        this.visibleSessionIds.splice(insertAt, 0, sessionId);

        if (this.visibleSessionIds.length > 9) {
            this.visibleSessionIds = this.visibleSessionIds.slice(0, 9);
            if (toast && toast.warning) toast.warning('最多支持 9 个分屏');
        }

        this._updateLayoutMode(position, targetIndex);
        this.activeSSHSessionId = sessionId;
    },

    // ==================== DOM 同步 ====================

    _scheduleSync() {
        if (this._syncTimer) clearTimeout(this._syncTimer);

        this.$nextTick(() => {
            this._syncTerminals();
            this._fitAll();

            this._syncTimer = setTimeout(() => {
                this._syncTerminals();
                this._fitAll();
            }, 150);

            setTimeout(() => {
                this._fitAll();
            }, 300);
        });
    },

    _syncTerminals() {
        // 确定当前应该显示的会话列表
        let idsToShow = [];

        if (this.sshViewLayout === 'single') {
            // 单屏模式下，只显示当前激活的会话
            if (this.activeSSHSessionId) {
                idsToShow = [this.activeSSHSessionId];
            }
        } else {
            // 分屏模式下，显示所有可见会话
            idsToShow = this.visibleSessionIds;
        }

        // 将需要显示的终端移动到对应索引的静态槽位
        idsToShow.forEach((id, index) => {
            if (!id) return;
            // 关键修复：server.html 定义的槽位 ID 是 ssh-slot-idx-{index}
            // 在单屏模式下，index 恒为 0
            const slotId = 'ssh-slot-idx-' + index;
            const slot = document.getElementById(slotId);
            const terminal = document.getElementById('ssh-terminal-' + id);

            if (slot && terminal && terminal.parentElement !== slot) {
                // 先清空槽位内可能残留的其他元素（虽然理论上 Vue 会控制显隐，但为了保险）
                // 实际上不需要清空，因为我们有 warehouse 机制回收了不该显示的
                slot.appendChild(terminal);

                // 触发一次 resize 以适应新容器
                const session = this.getSessionById(id);
                if (session && this.safeTerminalFit) {
                    this.safeTerminalFit(session);
                }
            }
        });

        const warehouse = document.getElementById('ssh-terminal-warehouse');
        if (warehouse) {
            this.sshSessions.forEach(session => {
                if (!idsToShow.includes(session.id)) {
                    const terminal = document.getElementById('ssh-terminal-' + session.id);
                    if (terminal && terminal.parentElement !== warehouse) {
                        warehouse.appendChild(terminal);
                    }
                }
            });
        }
    },

    _saveToWarehouse() {
        const warehouse = document.getElementById('ssh-terminal-warehouse');
        if (!warehouse) return;

        this.sshSessions.forEach(session => {
            const terminal = document.getElementById('ssh-terminal-' + session.id);
            if (terminal && terminal.parentElement !== warehouse) {
                warehouse.appendChild(terminal);
            }
        });
    },

    _fitAll() {
        const idsToFit = this.sshViewLayout === 'single'
            ? (this.activeSSHSessionId ? [this.activeSSHSessionId] : [])
            : this.visibleSessionIds;

        idsToFit.forEach(id => {
            const session = this.getSessionById(id);
            if (session && this.safeTerminalFit) {
                this.safeTerminalFit(session);
            }
        });
    },

    // ==================== 兼容旧 API ====================

    handleTabDragStart(sessionId, event) {
        this.onTabDragStart(sessionId, event);
    },

    handleTabDragEnd() {
        this.onTabDragEnd();
    },

    setDropHint(pos, targetId = null) {
        this.onDropZoneEnter(pos, targetId);
    },

    clearDropHint() {
        this.onDropZoneLeave();
    },

    handleTerminalDragOver(e) {
        e.preventDefault();
    },

    handleTerminalDrop(targetId = null, position = 'center') {
        const effectivePosition = targetId ? position : (this.dropHint || 'center');
        this.onDrop(targetId, effectivePosition);
    },

    /**
     * 计算全局预提示层的样式
     */
    getGlobalDropHintStyle() {
        if (!this.dropHint || this.dropTargetId) return { display: 'none' };

        const styles = {
            top: '0', left: '0', right: '0', bottom: '0'
        };

        switch (this.dropHint) {
            case 'left': styles.right = '50%'; break;
            case 'right': styles.left = '50%'; break;
            case 'top': styles.bottom = '50%'; break;
            case 'bottom': styles.top = '50%'; break;
            default: return { display: 'none' };
        }

        return styles;
    },

    closeSplitView(sessionId) {
        this.removeFromSplitView(sessionId);
    },

    resetToSingleLayout() {
        this._resetToSingle();
    },

    syncTerminalDOM() {
        this._syncTerminals();
    },

    saveTerminalsToWarehouse() {
        this._saveToWarehouse();
    },

    fitAllVisibleSessions() {
        this._fitAll();
    },

    scheduleSync() {
        this._scheduleSync();
    }
};