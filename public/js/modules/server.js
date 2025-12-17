/**
 * 服务器管理模块
 */

import { showToast } from './utils.js';

// 模块状态
const state = {
    servers: [],
    expandedServers: new Set(),
    serverInfo: new Map(),
    loading: false
};

/**
 * 初始化服务器管理模块
 */
export function initServerModule() {
    console.log('初始化服务器管理模块');
    loadServers();
    setupEventListeners();
}

/**
 * 设置事件监听器
 */
function setupEventListeners() {
    // 添加服务器按钮
    document.getElementById('add-server-btn')?.addEventListener('click', showAddServerModal);

    // 刷新按钮
    document.getElementById('refresh-servers-btn')?.addEventListener('click', () => {
        loadServers();
        showToast('正在刷新服务器列表...', 'info');
    });

    // 手动探测按钮
    document.getElementById('probe-all-servers-btn')?.addEventListener('click', probeAllServers);

    // 导入导出按钮
    document.getElementById('import-servers-btn')?.addEventListener('click', showImportModal);
    document.getElementById('export-servers-btn')?.addEventListener('click', exportServers);
}

/**
 * 加载服务器列表
 */
async function loadServers() {
    state.loading = true;
    renderServerList();

    try {
        const response = await fetch('/api/server/accounts');
        const data = await response.json();

        if (data.success) {
            state.servers = data.data;
            renderServerList();
        } else {
            showToast('加载服务器列表失败: ' + data.error, 'error');
        }
    } catch (error) {
        console.error('加载服务器列表失败:', error);
        showToast('加载服务器列表失败', 'error');
    } finally {
        state.loading = false;
    }
}

/**
 * 渲染服务器列表
 */
function renderServerList() {
    const container = document.getElementById('server-list-container');
    if (!container) return;

    if (state.loading) {
        container.innerHTML = `
            <div class="server-loading">
                <div class="server-loading-spinner"></div>
                <p>加载中...</p>
            </div>
        `;
        return;
    }

    if (state.servers.length === 0) {
        container.innerHTML = `
            <div class="server-empty-state">
                <div class="server-empty-state-icon">🖥️</div>
                <h3>还没有服务器</h3>
                <p>点击"添加服务器"按钮开始添加您的第一台服务器</p>
                <button class="btn btn-primary" onclick="window.serverModule.showAddServerModal()">
                    添加服务器
                </button>
            </div>
        `;
        return;
    }

    const html = state.servers.map(server => renderServerCard(server)).join('');
    container.innerHTML = html;

    // 重新绑定事件
    bindServerCardEvents();
}

/**
 * 渲染服务器卡片
 */
function renderServerCard(server) {
    const isExpanded = state.expandedServers.has(server.id);
    const info = state.serverInfo.get(server.id);

    const statusClass = server.status || 'unknown';
    const statusText = {
        'online': '在线',
        'offline': '离线',
        'unknown': '未知'
    }[statusClass] || '未知';

    const lastCheckTime = server.last_check_time
        ? new Date(server.last_check_time).toLocaleString('zh-CN')
        : '从未检查';

    const responseTime = server.response_time ? `${server.response_time}ms` : '-';

    return `
        <div class="server-card ${isExpanded ? 'expanded' : ''}" data-server-id="${server.id}">
            <div class="server-card-header" onclick="window.serverModule.toggleServerCard('${server.id}')">
                <div class="server-card-info">
                    <div class="server-status-indicator ${statusClass}"></div>
                    <div class="server-basic-info">
                        <div class="server-name">${escapeHtml(server.name)}</div>
                        <div class="server-host">${escapeHtml(server.username)}@${escapeHtml(server.host)}:${server.port}</div>
                        ${server.tags && server.tags.length > 0 ? `
                            <div class="server-tags">
                                ${server.tags.map(tag => `<span class="server-tag">${escapeHtml(tag)}</span>`).join('')}
                            </div>
                        ` : ''}
                    </div>
                </div>
                <div class="server-quick-info">
                    <span>状态: ${statusText}</span>
                    <span>响应: ${responseTime}</span>
                    <span>最后检查: ${lastCheckTime}</span>
                </div>
                <div class="server-card-actions" onclick="event.stopPropagation()">
                    <button class="btn btn-sm btn-primary" onclick="window.serverModule.connectSSH('${server.id}')" title="SSH 连接">
                        🔌 SSH
                    </button>
                    <button class="btn btn-sm btn-secondary" onclick="window.serverModule.showEditServerModal('${server.id}')" title="编辑">
                        ✏️
                    </button>
                    <button class="btn btn-sm btn-danger" onclick="window.serverModule.deleteServer('${server.id}')" title="删除">
                        🗑️
                    </button>
                </div>
                <div class="server-expand-icon">▼</div>
            </div>
            <div class="server-card-body">
                ${isExpanded ? renderServerDetails(server, info) : ''}
            </div>
        </div>
    `;
}

/**
 * 渲染服务器详情
 */
function renderServerDetails(server, info) {
    if (!info) {
        return `
            <div class="server-details">
                <div style="text-align: center; padding: 20px;">
                    <div class="server-loading-spinner" style="margin: 0 auto 10px;"></div>
                    <p>正在加载服务器信息...</p>
                </div>
            </div>
        `;
    }

    if (!info.success) {
        return `
            <div class="server-details">
                <div style="text-align: center; padding: 20px; color: var(--error-color);">
                    <p>❌ 加载失败: ${escapeHtml(info.error || '未知错误')}</p>
                </div>
            </div>
        `;
    }

    return `
        <div class="server-details">
            <div class="server-details-grid">
                <!-- 系统信息 -->
                <div class="server-detail-section">
                    <h4>💻 系统信息</h4>
                    ${renderDetailItems(info.system)}
                </div>

                <!-- CPU 信息 -->
                <div class="server-detail-section">
                    <h4>⚡ CPU 信息</h4>
                    ${renderDetailItems(info.cpu)}
                </div>

                <!-- 内存信息 -->
                <div class="server-detail-section">
                    <h4>🧠 内存信息</h4>
                    ${renderDetailItems(info.memory)}
                    ${renderProgressBar(info.memory.Usage)}
                </div>

                <!-- 磁盘信息 -->
                <div class="server-detail-section">
                    <h4>💾 磁盘信息</h4>
                    ${renderDiskInfo(info.disk)}
                </div>

                <!-- 网络接口 -->
                <div class="server-detail-section">
                    <h4>🌐 网络接口</h4>
                    ${renderNetworkInfo(info.network)}
                </div>

                <!-- Docker 信息 -->
                <div class="server-detail-section">
                    <h4>🐳 Docker 信息</h4>
                    ${renderDockerInfo(info.docker)}
                </div>
            </div>

            <!-- 操作按钮 -->
            <div class="server-actions-bar">
                <button class="btn btn-sm btn-primary" onclick="window.serverModule.refreshServerInfo('${server.id}')">
                    🔄 刷新信息
                </button>
                ${info && info.docker && info.docker.installed && info.docker.containers && info.docker.containers.length > 0 ? `
                    <button class="btn btn-sm btn-info" onclick="window.serverModule.showDockerContainers('${server.id}')">
                        🐳 查看容器 (${info.docker.containers.length})
                    </button>
                ` : ''}
                <button class="btn btn-sm btn-secondary" onclick="window.serverModule.openFileManager('${server.id}')">
                    📁 文件管理
                </button>
                <button class="btn btn-sm btn-warning" onclick="window.serverModule.rebootServer('${server.id}')">
                    🔄 重启服务器
                </button>
                <button class="btn btn-sm btn-danger" onclick="window.serverModule.shutdownServer('${server.id}')">
                    ⏻ 关机
                </button>
            </div>
        </div>
    `;
}

/**
 * 渲染详情项
 */
function renderDetailItems(data) {
    if (!data || typeof data !== 'object') return '<p>无数据</p>';

    return Object.entries(data).map(([key, value]) => `
        <div class="server-detail-item">
            <span class="server-detail-label">${escapeHtml(key)}</span>
            <span class="server-detail-value">${escapeHtml(String(value))}</span>
        </div>
    `).join('');
}

/**
 * 渲染进度条
 */
function renderProgressBar(usageStr) {
    if (!usageStr) return '';

    const usage = parseFloat(usageStr);
    if (isNaN(usage)) return '';

    let className = '';
    if (usage > 90) className = 'danger';
    else if (usage > 75) className = 'warning';

    return `
        <div class="progress-bar">
            <div class="progress-bar-fill ${className}" style="width: ${usage}%"></div>
        </div>
    `;
}

/**
 * 渲染磁盘信息
 */
function renderDiskInfo(disks) {
    if (!disks || !Array.isArray(disks) || disks.length === 0) {
        return '<p>无磁盘信息</p>';
    }

    return disks.map(disk => `
        <div class="server-detail-item">
            <span class="server-detail-label">${escapeHtml(disk.device)}</span>
            <span class="server-detail-value">${escapeHtml(disk.used)} / ${escapeHtml(disk.total)} (${escapeHtml(disk.usage)})</span>
        </div>
        ${renderProgressBar(disk.usage)}
    `).join('');
}

/**
 * 渲染网络接口信息
 */
function renderNetworkInfo(interfaces) {
    if (!interfaces || !Array.isArray(interfaces) || interfaces.length === 0) {
        return '<p>无网络接口信息</p>';
    }

    return `
        <div class="network-interface-list">
            ${interfaces.map(iface => `
                <div class="network-interface-item">
                    <span class="network-interface-name">${escapeHtml(iface.name)}</span>
                    <span class="network-interface-address">${escapeHtml(iface.address)}</span>
                </div>
            `).join('')}
        </div>
    `;
}

/**
 * 渲染 Docker 信息
 */
function renderDockerInfo(docker) {
    if (!docker || !docker.installed) {
        return '<p>Docker 未安装</p>';
    }

    return `
        <div class="server-detail-item">
            <span class="server-detail-label">版本</span>
            <span class="server-detail-value">${escapeHtml(docker.version)}</span>
        </div>
        <div class="server-detail-item">
            <span class="server-detail-label">容器数量</span>
            <span class="server-detail-value">${docker.containers?.length || 0}</span>
        </div>
        ${docker.containers && docker.containers.length > 0 ? `
            <div class="docker-container-list">
                ${docker.containers.map(container => `
                    <div class="docker-container-item">
                        <span class="docker-container-name">${escapeHtml(container.name)}</span>
                        <span class="docker-container-status ${container.status.includes('Up') ? 'running' : 'exited'}">
                            ${escapeHtml(container.status)}
                        </span>
                    </div>
                `).join('')}
            </div>
        ` : ''}
    `;
}

/**
 * 切换服务器卡片展开/收起
 */
async function toggleServerCard(serverId) {
    if (state.expandedServers.has(serverId)) {
        state.expandedServers.delete(serverId);
    } else {
        state.expandedServers.add(serverId);

        // 如果还没有加载服务器信息，则加载
        if (!state.serverInfo.has(serverId)) {
            loadServerInfo(serverId);
        }
    }

    renderServerList();
}

/**
 * 加载服务器详细信息
 */
async function loadServerInfo(serverId) {
    try {
        const response = await fetch('/api/server/info', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ serverId })
        });

        const data = await response.json();
        state.serverInfo.set(serverId, data);
        renderServerList();
    } catch (error) {
        console.error('加载服务器信息失败:', error);
        state.serverInfo.set(serverId, {
            success: false,
            error: error.message
        });
        renderServerList();
    }
}

/**
 * 刷新服务器信息
 */
async function refreshServerInfo(serverId) {
    state.serverInfo.delete(serverId);
    await loadServerInfo(serverId);
    showToast('正在刷新服务器信息...', 'info');
}

/**
 * 显示添加服务器对话框
 */
function showAddServerModal() {
    // 触发 Vue 实例的方法
    if (window.vueApp) {
        window.vueApp.openAddServerModal();
    }
}

/**
 * 显示编辑服务器对话框
 */
function showEditServerModal(serverId) {
    // 触发 Vue 实例的方法
    if (window.vueApp) {
        window.vueApp.openEditServerModal(serverId);
    }
}

/**
 * 删除服务器
 */
async function deleteServer(serverId) {
    if (!confirm('确定要删除这台服务器吗？')) {
        return;
    }

    try {
        const response = await fetch(`/api/server/accounts/${serverId}`, {
            method: 'DELETE'
        });

        const data = await response.json();

        if (data.success) {
            showToast('服务器删除成功', 'success');
            loadServers();
        } else {
            showToast('删除失败: ' + data.error, 'error');
        }
    } catch (error) {
        console.error('删除服务器失败:', error);
        showToast('删除服务器失败', 'error');
    }
}

/**
 * 手动探测所有服务器
 */
async function probeAllServers() {
    showToast('正在探测所有服务器...', 'info');

    try {
        const response = await fetch('/api/server/check-all', {
            method: 'POST'
        });

        const data = await response.json();

        if (data.success) {
            showToast(data.message, 'success');
            loadServers();
        } else {
            showToast('探测失败: ' + data.error, 'error');
        }
    } catch (error) {
        console.error('探测服务器失败:', error);
        showToast('探测服务器失败', 'error');
    }
}

/**
 * SSH 连接
 */
function connectSSH(serverId) {
    const server = state.servers.find(s => s.id === serverId);
    if (!server) {
        showToast('服务器不存在', 'error');
        return;
    }

    // 触发 Vue 实例的方法打开 SSH 终端
    if (window.vueApp) {
        window.vueApp.openSSHTerminal(server);
    }
}

/**
 * 显示 Docker 容器详情
 */
function showDockerContainers(serverId) {
    const info = state.serverInfo.get(serverId);
    if (!info || !info.docker || !info.docker.containers) {
        showToast('无法获取容器信息', 'error');
        return;
    }

    const server = state.servers.find(s => s.id === serverId);
    if (!server) return;

    // 触发 Vue 实例的方法
    if (window.vueApp) {
        window.vueApp.showDockerContainersModal(server, info.docker);
    }
}

/**
 * 打开文件管理器
 */
function openFileManager(serverId) {
    const server = state.servers.find(s => s.id === serverId);
    if (!server) {
        showToast('服务器不存在', 'error');
        return;
    }

    // 触发 Vue 实例的方法
    if (window.vueApp) {
        window.vueApp.openFileManager(server);
    }
}

/**
 * 重启服务器
 */
async function rebootServer(serverId) {
    if (!confirm('确定要重启这台服务器吗？')) {
        return;
    }

    try {
        const response = await fetch('/api/server/action', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ serverId, action: 'reboot' })
        });

        const data = await response.json();

        if (data.success) {
            showToast('重启命令已发送', 'success');
        } else {
            showToast('重启失败: ' + data.message, 'error');
        }
    } catch (error) {
        console.error('重启服务器失败:', error);
        showToast('重启服务器失败', 'error');
    }
}

/**
 * 关机
 */
async function shutdownServer(serverId) {
    if (!confirm('确定要关闭这台服务器吗？此操作不可逆！')) {
        return;
    }

    try {
        const response = await fetch('/api/server/action', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ serverId, action: 'shutdown' })
        });

        const data = await response.json();

        if (data.success) {
            showToast('关机命令已发送', 'success');
        } else {
            showToast('关机失败: ' + data.message, 'error');
        }
    } catch (error) {
        console.error('关机失败:', error);
        showToast('关机失败', 'error');
    }
}

/**
 * 导入服务器
 */
function showImportModal() {
    // 触发 Vue 实例的方法
    if (window.vueApp) {
        window.vueApp.openImportServerModal();
    }
}

/**
 * 导出服务器
 */
async function exportServers() {
    try {
        const response = await fetch('/api/server/accounts/export');
        const data = await response.json();

        if (data.success) {
            const blob = new Blob([JSON.stringify(data.data, null, 2)], { type: 'application/json' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `servers_${new Date().toISOString().split('T')[0]}.json`;
            a.click();
            URL.revokeObjectURL(url);

            showToast('导出成功', 'success');
        } else {
            showToast('导出失败: ' + data.error, 'error');
        }
    } catch (error) {
        console.error('导出服务器失败:', error);
        showToast('导出服务器失败', 'error');
    }
}

/**
 * 绑定服务器卡片事件
 */
function bindServerCardEvents() {
    // 事件已通过 onclick 属性绑定
}

/**
 * HTML 转义
 */
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// 导出函数到全局作用域，供 HTML 中的 onclick 使用
window.serverModule = {
    toggleServerCard,
    showAddServerModal,
    showEditServerModal,
    deleteServer,
    connectSSH,
    showDockerContainers,
    openFileManager,
    rebootServer,
    shutdownServer,
    refreshServerInfo,
    loadServers // 导出以便 Vue 可以调用
};
