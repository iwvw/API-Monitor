/**
 * 主机管理模块
 */

import { store } from '../store.js';
import { toast } from './toast.js';

// 模块状态
const state = {
    servers: [],
    expandedServers: new Set(),
    serverInfo: new Map(),
    loading: false
};

/**
 * 初始化主机管理模块
 */
export function initServerModule() {
    console.log('初始化主机管理模块');
    loadServers();
    setupEventListeners();

    // 监听显示模式变化事件
    window.addEventListener('server-display-mode-changed', () => {
        renderServerList();
    });
}

/**
 * 设置事件监听器（支持重复调用）
 */
function setupEventListeners() {
    // 使用事件委托或延迟绑定，确保按钮存在
    const bindButton = (id, handler) => {
        const btn = document.getElementById(id);
        if (btn && !btn.dataset.bound) {
            btn.addEventListener('click', handler);
            btn.dataset.bound = 'true';
        }
    };

    // 添加主机按钮
    bindButton('add-server-btn', showAddServerModal);

    // 刷新按钮
    bindButton('refresh-servers-btn', () => {
        loadServers(false);
    });

    // 手动探测按钮
    bindButton('probe-all-servers-btn', probeAllServers);

    // 导入导出按钮
    bindButton('import-servers-btn', showImportModal);
    bindButton('export-servers-btn', exportServers);
}

/**
 * 初始化后台管理按钮（供 Vue 调用）
 */
export function initManagementButtons() {
    setupEventListeners();
}

/**
 * 加载主机列表
 */
async function loadServers(silent = false) {
    state.loading = true;
    renderServerList();

    try {
        const response = await fetch('/api/server/accounts');
        const data = await response.json();

        if (data.success) {
            state.servers = data.data;
            if (!silent) {
                toast.success('主机列表已刷新');
            }
        } else {
            toast.error('加载主机列表失败: ' + data.error);
        }
    } catch (error) {
        console.error('加载主机列表失败:', error);
        toast.error('加载主机列表失败');
    } finally {
        state.loading = false;
        renderServerList();
    }
}

/**
 * 渲染主机列表
 */
function renderServerList() {
    const container = document.getElementById('server-list-container');
    const managementContainer = document.getElementById('management-server-list-container');

    // 主机列表标签页 - 显示完整卡片
    if (container) {
        if (state.loading) {
            container.innerHTML = `
                <div class="server-loading">
                    <div class="server-loading-spinner"></div>
                    <p>加载中...</p>
                </div>
            `;
        } else if (state.servers.length === 0) {
            container.innerHTML = `
                <div class="empty-state-refined">
                    <i class="fas fa-server"></i>
                    <h3>还没有主机</h3>
                    <p>请切换到"后台管理"标签页添加您的第一台主机</p>
                </div>
            `;
        } else {
            container.innerHTML = state.servers.map(server => renderServerCard(server)).join('');
        }
    }

    // 后台管理标签页 - 显示简洁表格
    if (managementContainer) {
        if (state.loading) {
            managementContainer.innerHTML = `
                <div class="server-loading">
                    <div class="server-loading-spinner"></div>
                    <p>加载中...</p>
                </div>
            `;
        } else if (state.servers.length === 0) {
            managementContainer.innerHTML = `
                <div class="empty-state-refined">
                    <i class="fas fa-server"></i>
                    <h3>还没有主机</h3>
                    <p>点击上方"添加主机"按钮开始添加您的第一台主机</p>
                </div>
            `;
        } else {
            managementContainer.innerHTML = renderServerTable(state.servers);
        }
    }

    // 重新绑定事件
    bindServerCardEvents();
}

/**
 * 渲染后台管理的主机表格
 */
function renderServerTable(servers) {
    return `
        <table class="data-table">
            <thead>
                <tr>
                    <th>状态</th>
                    <th>主机名称</th>
                    <th>主机地址</th>
                    <th>响应时间</th>
                    <th>最后检查</th>
                    <th style="width: 150px;">操作</th>
                </tr>
            </thead>
            <tbody>
                ${servers.map(server => renderServerTableRow(server)).join('')}
            </tbody>
        </table>
    `;
}

/**
 * 格式化主机地址（支持打码/隐藏）
 */
function formatHost(host, explicitMode) {
    if (!host) return '';
    const mode = explicitMode || store.serverIpDisplayMode || 'normal';
    if (mode === 'normal') return host;
    if (mode === 'hidden') return '****';

    // 打码模式 (masked): 1.2.3.4 -> 1.2.*.*
    // 严谨检测 IPv4
    const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/;
    if (ipv4Regex.test(host)) {
        const parts = host.split('.');
        return `${parts[0]}.${parts[1]}.*.*`;
    }

    // 域名或其他: example.com -> ex****.com
    const parts = host.split('.');
    if (parts.length >= 2) {
        const main = parts[0];
        const tld = parts[parts.length - 1];
        if (main.length > 2) {
            return main.substring(0, 2) + '****.' + tld;
        }
    }

    return host.length > 4 ? host.substring(0, 2) + '****' : '****';
}

/**
 * 渲染后台管理表格的行
 */
function renderServerTableRow(server) {
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

    const statusBadgeClass = statusClass === 'online' ? 'proxied-on' : (statusClass === 'offline' ? 'proxied-off' : '');

    return `
        <tr>
            <td>
                <span class="proxied-badge ${statusBadgeClass}">
                    ${statusText}
                </span>
            </td>
            <td>
                <strong>${escapeHtml(server.name)}</strong>
                ${server.tags && server.tags.length > 0 ?
            '<br><div style="margin-top: 4px;">' + server.tags.map(tag => `<span class="server-tag">${escapeHtml(tag)}</span>`).join(' ') + '</div>'
            : ''}
            </td>
            <td>
                <code style="background: var(--section-bg); padding: 2px 6px; border-radius: 3px; font-size: 12px;">
                    ${escapeHtml(server.username)}@${escapeHtml(formatHost(server.host))}:${server.port}
                </code>
            </td>
            <td>${responseTime}</td>
            <td>${lastCheckTime}</td>
            <td class="actions">
                <button class="btn btn-secondary" style="padding: 4px 8px; font-size: 12px;"
                    onclick="window.serverModule.connectSSH('${server.id}')" title="SSH 连接">
                    <i class="fas fa-terminal"></i>
                </button>
                <button class="btn btn-primary" style="padding: 4px 8px; font-size: 12px;"
                    onclick="window.serverModule.showEditServerModal('${server.id}')" title="编辑">
                    <i class="fas fa-edit"></i>
                </button>
                <button class="btn btn-danger" style="padding: 4px 8px; font-size: 12px;"
                    onclick="window.serverModule.deleteServer('${server.id}')" title="删除">
                    <i class="fas fa-trash"></i>
                </button>
            </td>
        </tr>
    `;
}

/**
 * 渲染主机卡片
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

    const statusBadgeClass = statusClass === 'online' ? 'proxied-on' : (statusClass === 'offline' ? 'proxied-off' : '');

    const lastCheckTime = server.last_check_time
        ? new Date(server.last_check_time).toLocaleString('zh-CN')
        : '从未检查';

    const responseTime = server.response_time ? `${server.response_time}ms` : '-';

    return `
        <div class="server-card ${isExpanded ? 'expanded' : ''}" data-server-id="${server.id}">
            <div class="server-card-header" onclick="window.serverModule.toggleServerCard('${server.id}')">
                <div class="server-card-info">
                    <span class="server-toggle-icon">
                        <i class="fas fa-chevron-right"></i>
                    </span>
                    <div class="server-status-indicator ${statusClass}"></div>
                    <div class="server-basic-info">
                        <div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 4px;">
                            <span class="server-name">${escapeHtml(server.name)}</span>
                            <span class="proxied-badge ${statusBadgeClass}">${statusText}</span>
                            ${server.tags && server.tags.length > 0 ?
            server.tags.map(tag => `<span class="server-tag">${escapeHtml(tag)}</span>`).join('')
            : ''}
                        </div>
                        <div class="server-host">${escapeHtml(server.username)}@${escapeHtml(formatHost(server.host))}:${server.port}</div>
                    </div>
                </div>
                <div class="server-quick-info">
                    <span>响应: ${responseTime}</span>
                    <span>检查: ${lastCheckTime}</span>
                </div>
                <div class="server-card-actions" onclick="event.stopPropagation()">
                    <button class="btn btn-sm btn-primary" onclick="window.serverModule.connectSSH('${server.id}')" title="SSH 连接">
                        <i class="fas fa-terminal"></i> SSH
                    </button>
                    <button class="btn btn-sm btn-secondary" onclick="window.serverModule.showEditServerModal('${server.id}')" title="编辑">
                        <i class="fas fa-edit"></i>
                    </button>
                    <button class="btn btn-sm btn-danger" onclick="window.serverModule.deleteServer('${server.id}')" title="删除">
                        <i class="fas fa-trash"></i>
                    </button>
                </div>
            </div>
            <div class="server-card-body">
                ${isExpanded ? renderServerDetails(server, info) : ''}
            </div>
        </div>
    `;
}

/**
 * 渲染主机详情
 */
function renderServerDetails(server, info) {
    if (!info) {
        return `
            <div class="server-details">
                <div style="text-align: center; padding: 8px 3px;">
                    <div class="server-loading-spinner" style="margin: 0 auto 10px;"></div>
                    <p>正在加载主机信息...</p>
                </div>
            </div>
        `;
    }

    if (!info.success) {
        return `
            <div class="server-details">
                <div style="text-align: center; padding: 8px 3px; color: var(--error-color);">
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
                <button class="btn btn-sm btn-warning" onclick="window.serverModule.rebootServer('${server.id}')">
                    🔄 重启主机
                </button>
                <button class="btn btn-sm btn-danger" onclick="window.serverModule.shutdownServer('${server.id}')">
                    ⏻ 关机
                </button>
            </div>
        </div>
    `;
}

/**
 * 格式化运行时间为中文格式
 * 将 "up 6 days, 2 hours, 32 minutes" 转换为 "6天2时32分"
 */
function formatUptime(uptimeStr) {
    if (!uptimeStr || typeof uptimeStr !== 'string') return uptimeStr;

    // 移除 "up " 前缀
    let str = uptimeStr.replace(/^up\s+/i, '');

    // 提取各个时间部分
    const weekMatch = str.match(/(\d+)\s*weeks?/i);
    const dayMatch = str.match(/(\d+)\s*days?/i);
    const hourMatch = str.match(/(\d+)\s*hours?/i);
    const minMatch = str.match(/(\d+)\s*minutes?/i);

    let days = dayMatch ? parseInt(dayMatch[1], 10) : 0;
    const weeks = weekMatch ? parseInt(weekMatch[1], 10) : 0;
    const hours = hourMatch ? parseInt(hourMatch[1], 10) : 0;
    const minutes = minMatch ? parseInt(minMatch[1], 10) : 0;

    // 将周转换为天并累加
    days += weeks * 7;

    // 构建中文格式
    let result = '';
    if (days > 0) result += `${days}天`;
    if (hours > 0) result += `${hours}时`;
    if (minutes > 0) result += `${minutes}分`;

    // 如果都是0，显示 "0分"
    if (result === '') result = '0分';

    return result;
}

/**
 * 渲染详情项
 */
function renderDetailItems(data) {
    if (!data || typeof data !== 'object') return '<p>无数据</p>';

    return Object.entries(data).map(([key, value]) => {
        // 对 Uptime 进行特殊格式化
        let displayValue = String(value);
        if (key === 'Uptime') {
            displayValue = formatUptime(value);
        }
        return `
        <div class="server-detail-item">
            <span class="server-detail-label">${escapeHtml(key)}</span>
            <span class="server-detail-value">${escapeHtml(displayValue)}</span>
        </div>
    `;
    }).join('');
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
 * 渲染 Docker 信息
 */
function renderDockerInfo(docker) {
    if (!docker || !docker.installed) {
        return '<p>Docker 未安装</p>';
    }

    const totalContainers = docker.containers?.length || 0;
    const runningContainers = docker.containers?.filter(c => c.status.includes('Up')).length || 0;
    const stoppedContainers = totalContainers - runningContainers;

    return `
        <div class="server-detail-item">
            <span class="server-detail-label">容器总数</span>
            <span class="server-detail-value">${totalContainers}</span>
        </div>
        ${totalContainers > 0 ? `
            <div class="server-detail-item">
                <span class="server-detail-label">运行中</span>
                <span class="server-detail-value" style="color: #10b981;">${runningContainers}</span>
            </div>
            <div class="server-detail-item">
                <span class="server-detail-label">已停止</span>
                <span class="server-detail-value" style="color: #ef4444;">${stoppedContainers}</span>
            </div>
        ` : ''}
    `;
}

/**
 * 切换主机卡片展开/收起
 */
async function toggleServerCard(serverId) {
    if (state.expandedServers.has(serverId)) {
        state.expandedServers.delete(serverId);
    } else {
        state.expandedServers.add(serverId);

        // 如果还没有加载主机信息，则加载
        if (!state.serverInfo.has(serverId)) {
            loadServerInfo(serverId);
        }
    }

    renderServerList();
}

/**
 * 加载主机详细信息
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
        console.error('加载主机信息失败:', error);
        state.serverInfo.set(serverId, {
            success: false,
            error: error.message
        });
        renderServerList();
    }
}

/**
 * 刷新主机信息（静默刷新，不清空现有数据）
 */
async function refreshServerInfo(serverId) {
    // 不删除现有数据，直接在后台加载新数据覆盖
    // 这样 UI 不会闪烁
    toast.info('正在刷新主机信息...');
    await loadServerInfo(serverId, true); // 传入 force=true 强制刷新
}

/**
 * 显示添加主机对话框
 */
function showAddServerModal() {
    // 触发 Vue 实例的方法
    if (window.vueApp) {
        window.vueApp.openAddServerModal();
    }
}

/**
 * 显示编辑主机对话框
 */
function showEditServerModal(serverId) {
    // 触发 Vue 实例的方法
    if (window.vueApp) {
        window.vueApp.openEditServerModal(serverId);
    }
}

/**
 * 删除主机
 */
async function deleteServer(serverId) {
    const confirmed = await store.showConfirm({
        title: '删除主机',
        message: '确定要删除这台主机吗？',
        icon: 'fa-trash',
        confirmText: '删除',
        confirmClass: 'btn-danger'
    });

    if (!confirmed) {
        return;
    }

    try {
        const response = await fetch(`/api/server/accounts/${serverId}`, {
            method: 'DELETE'
        });

        const data = await response.json();

        if (data.success) {
            toast.success('主机删除成功');
            loadServers();
        } else {
            toast.error('删除失败: ' + data.error);
        }
    } catch (error) {
        console.error('删除主机失败:', error);
        toast.error('删除主机失败');
    }
}

/**
 * 手动探测所有主机
 */
async function probeAllServers() {
    toast.info('正在探测所有主机...');

    try {
        const response = await fetch('/api/server/check-all', {
            method: 'POST'
        });

        const data = await response.json();

        if (data.success) {
            toast.success(data.message);
            loadServers();
        } else {
            toast.error('探测失败: ' + data.error);
        }
    } catch (error) {
        console.error('探测主机失败:', error);
        toast.error('探测主机失败');
    }
}

/**
 * SSH 连接
 */
function connectSSH(serverId) {
    const server = state.servers.find(s => s.id === serverId);
    if (!server) {
        toast.error('主机不存在');
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
        toast.error('无法获取容器信息');
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
 * 重启主机
 */
async function rebootServer(serverId) {
    const confirmed = await store.showConfirm({
        title: '重启主机',
        message: '确定要重启这台主机吗？',
        icon: 'fa-redo',
        confirmText: '重启',
        confirmClass: 'btn-warning'
    });

    if (!confirmed) {
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
            toast.success('重启命令已发送');
        } else {
            toast.error('重启失败: ' + data.message);
        }
    } catch (error) {
        console.error('重启主机失败:', error);
        toast.error('重启主机失败');
    }
}

/**
 * 关机
 */
async function shutdownServer(serverId) {
    const confirmed = await store.showConfirm({
        title: '关闭主机',
        message: '确定要关闭这台主机吗？此操作不可逆！',
        icon: 'fa-power-off',
        confirmText: '确定关机',
        confirmClass: 'btn-danger'
    });

    if (!confirmed) {
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
            toast.success('关机命令已发送');
        } else {
            toast.error('关机失败: ' + data.message);
        }
    } catch (error) {
        console.error('关机失败:', error);
        toast.error('关机失败');
    }
}

/**
 * 导入主机
 */
function showImportModal() {
    // 触发 Vue 实例的方法
    if (window.vueApp) {
        window.vueApp.openImportServerModal();
    }
}

/**
 * 导出主机
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

            toast.success('导出成功');
        } else {
            toast.error('导出失败: ' + data.error);
        }
    } catch (error) {
        console.error('导出主机失败:', error);
        toast.error('导出主机失败');
    }
}

/**
 * 绑定主机卡片事件
 */
function bindServerCardEvents() {
    // 事件已通过 onclick 属性绑定
}

/**
 * HTML 转义
 */
function escapeHtml(text) {
    const div = document.createElement('div');
    if (text === null || text === undefined) return '';
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
    rebootServer,
    shutdownServer,
    refreshServerInfo,
    loadServers,
    renderServerList, // 导出渲染函数，支持直接重绘
    initManagementButtons, // 初始化后台管理按钮
    formatHost // 导出格式化函数
};
