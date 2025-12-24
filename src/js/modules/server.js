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
    metrics: new Map(), // 实时监控数据
    loading: false
};

/**
 * 初始化主机管理模块
 */
export function initServerModule() {
    console.log('初始化主机管理模块');
    loadServers();
    setupEventListeners();

    // 启动 WebSocket 连接
    connectMetricsWS();

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
 * 局部更新单个主机卡片
 */
function renderSingleServerCard(serverId) {
    const card = document.querySelector(`.server-card[data-server-id="${serverId}"]`);
    if (!card) return;

    const server = state.servers.find(s => s.id === serverId);
    if (!server) return;

    console.log('[Server] Partial rendering card:', serverId);
    card.outerHTML = renderServerCard(server);
}

/**
 * 获取延迟徽标 HTML
 */
function getLatencyBadgeHtml(rt) {
    if (!rt) {
        return `
            <div style="display: inline-flex; align-items: center; padding: 2px 10px; border-radius: 10px; font-size: 11px; font-weight: 700; font-family: var(--font-mono); background: rgba(128, 128, 128, 0.08); color: #8b949e;">
                WAIT
            </div>
        `;
    }
    const num = parseInt(rt);
    const bg = num < 100 ? 'rgba(16, 185, 129, 0.1)' : (num < 300 ? 'rgba(245, 158, 11, 0.1)' : 'rgba(239, 68, 68, 0.1)');
    const color = num < 100 ? '#10b981' : (num < 300 ? '#f59e0b' : '#ef4444');

    return `
        <div style="display: inline-flex; align-items: center; gap: 4px; padding: 2px 10px; border-radius: 10px; font-size: 11px; font-weight: 700; font-family: var(--font-mono); background: ${bg}; color: ${color};">
            ${rt}ms
        </div>
    `;
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
                    <th class="text-center">延迟</th>
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
                    ${escapeHtml(formatHost(server.host))}:${server.port}
                </code>
            </td>
            <td class="text-center">
                ${getLatencyBadgeHtml(server.response_time)}
            </td>
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
                        <div class="server-host" style="margin-top: 6px; background: transparent; padding: 0;">
                            ${server.response_time ? `
                                <div style="display: inline-flex; align-items: center; gap: 4px; padding: 2px 8px; border-radius: 12px; font-size: 10px; font-weight: 700; font-family: var(--font-mono); 
                                    background: ${parseInt(server.response_time) < 100 ? 'rgba(16, 185, 129, 0.1)' : (parseInt(server.response_time) < 300 ? 'rgba(245, 158, 11, 0.1)' : 'rgba(239, 68, 68, 0.1)')}; 
                                    color: ${parseInt(server.response_time) < 100 ? '#10b981' : (parseInt(server.response_time) < 300 ? '#f59e0b' : '#ef4444')};
                                    border: 1px solid ${parseInt(server.response_time) < 100 ? 'rgba(16, 185, 129, 0.2)' : (parseInt(server.response_time) < 300 ? 'rgba(245, 158, 11, 0.2)' : 'rgba(239, 68, 68, 0.2)')}">
                                    <i class="fas fa-bolt" style="font-size: 9px;"></i>
                                    <span>${server.response_time}ms</span>
                                </div>
                            ` : '<span style="font-size: 11px; color: var(--text-tertiary); opacity: 0.5;">未探测</span>'}
                        </div>
                    </div>
                </div>
                <div class="server-quick-info">
                    ${renderQuickMetrics(state.metrics.get(server.id))}
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
    // 优先尝试从 state.metrics 获取 Agent 数据
    const agentMetrics = state.metrics.get(server.id);

    if (!info) {
        if (agentMetrics) {
            // 如果没有 SSH 信息但有 Agent 数据，渲染简版详情
            return `
                <div class="server-details">
                    <div class="server-details-grid">
                        <div class="server-detail-section">
                            <h4>⚡ 实时指标 (Agent)</h4>
                            <div class="server-detail-item">
                                <span class="server-detail-label">CPU 使用率</span>
                                <span class="server-detail-value">${agentMetrics.cpu_usage || '-'}</span>
                            </div>
                            <div class="server-detail-item">
                                <span class="server-detail-label">负载 (Load)</span>
                                <span class="server-detail-value">${agentMetrics.load || '-'}</span>
                            </div>
                            <div class="server-detail-item">
                                <span class="server-detail-label">内存使用</span>
                                <span class="server-detail-value">${agentMetrics.mem_usage || '-'}</span>
                            </div>
                            <div class="server-detail-item">
                                <span class="server-detail-label">磁盘状态</span>
                                <span class="server-detail-value">${agentMetrics.disk_usage || '-'}</span>
                            </div>
                        </div>
                        <div class="server-detail-section">
                            <h4>🌐 网络实时流量</h4>
                            <div class="server-detail-item">
                                <span class="server-detail-label">下行速度</span>
                                <span class="server-detail-value">⬇️ ${agentMetrics.network?.rx_speed || '-'}</span>
                            </div>
                            <div class="server-detail-item">
                                <span class="server-detail-label">上行速度</span>
                                <span class="server-detail-value">⬆️ ${agentMetrics.network?.tx_speed || '-'}</span>
                            </div>
                            <div class="server-detail-item">
                                <span class="server-detail-label">活动连接</span>
                                <span class="server-detail-value">${agentMetrics.network?.connections || '-'}</span>
                            </div>
                        </div>
                    </div>
                    <div class="server-actions-bar">
                        <p style="font-size: 12px; color: var(--text-tertiary); margin-bottom: 10px;">
                            提示: 该数据由服务器上的 Agent 实时推送。如需查看完整硬件详情，请点击下方刷新信息。
                        </p>
                        <button class="btn btn-sm btn-primary" onclick="window.serverModule.refreshServerInfo('${server.id}')">
                            🔄 SSH 深度探测
                        </button>
                    </div>
                </div>
            `;
        }

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

    renderSingleServerCard(serverId);
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
        renderSingleServerCard(serverId);
    } catch (error) {
        console.error('加载主机信息失败:', error);
        state.serverInfo.set(serverId, {
            success: false,
            error: error.message
        });
        renderSingleServerCard(serverId);
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

/**
 * Vue 实例混入方法 - 用于解耦 main.js
 */
export const serverMethods = {
    /**
     * 从本地缓存加载主机列表（首屏瞬显）
     */
    loadFromServerListCache() {
        try {
            const cacheKey = 'server_list_cache';
            const saved = localStorage.getItem(cacheKey);
            if (saved) {
                const cached = JSON.parse(saved);
                if (cached && Array.isArray(cached) && cached.length > 0) {
                    this.serverList = cached;
                    console.log('[Cache] 主机列表已从缓存恢复:', cached.length, '台');
                    return true;
                }
            }
        } catch (e) {
            console.warn('[Cache] 读取主机列表缓存失败:', e);
        }
        return false;
    },

    /**
     * 保存主机列表到本地缓存
     */
    saveServerListCache() {
        try {
            const cacheKey = 'server_list_cache';
            // 只保存基础数据，不保存 loading 等临时状态
            const toCache = this.serverList.map(s => ({
                id: s.id,
                name: s.name,
                host: s.host,
                port: s.port,
                username: s.username,
                status: s.status,
                response_time: s.response_time,
                tags: s.tags,
                info: s.info // 保留指标信息
            }));
            localStorage.setItem(cacheKey, JSON.stringify(toCache));
        } catch (e) {
            console.warn('[Cache] 保存主机列表缓存失败:', e);
        }
    },

    /**
     * 加载主机列表
     */
    async loadServerList() {
        this.serverLoading = true;
        try {
            const response = await fetch('/api/server/accounts', {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            if (data.success) {
                const newList = data.data;

                // 智能合并：后端可能返回带有 info 的缓存数据
                newList.forEach(newServer => {
                    const existing = this.serverList.find(s => s.id === newServer.id);
                    if (existing) {
                        // 只更新基础属性
                        existing.name = newServer.name;
                        existing.host = newServer.host;
                        existing.port = newServer.port;
                        existing.username = newServer.username;
                        existing.tags = newServer.tags;
                        existing.description = newServer.description;
                        // 仅当新数据有状态/延迟时更新
                        if (newServer.status) existing.status = newServer.status;
                        if (newServer.response_time) existing.response_time = newServer.response_time;
                        // 如果后端返回了缓存的 info 且当前没有，则使用
                        if (newServer.info && !existing.info) {
                            existing.info = newServer.info;
                        }
                    } else {
                        // 新主机直接添加（包含后端返回的 info）
                        this.serverList.push(newServer);
                    }
                });

                // 移除已删除的主机
                this.serverList = this.serverList.filter(s => newList.find(ns => ns.id === s.id));

                // 保存到本地缓存
                this.saveServerListCache();
            }
        } catch (error) {
            console.error('加载主机列表失败:', error);
        } finally {
            this.serverLoading = false;
        }
    },

    /**
     * 刷新单个主机信息
     */
    async refreshServerInfo(serverId) {
        const server = this.serverList.find(s => s.id === serverId);
        if (!server) return;

        server.loading = true;
        try {
            const response = await fetch('/api/server/info', {
                method: 'POST',
                headers: {
                    ...this.getAuthHeaders(),
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ serverId })
            });
            const data = await response.json();
            if (data.success) {
                server.info = data;
                server.status = 'online';
                server.error = null;
            } else {
                server.error = data.error || '获取失败';
                server.status = 'offline';
            }
        } catch (error) {
            server.error = error.message;
            server.status = 'offline';
        } finally {
            server.loading = false;
        }
    },

    /**
     * 探测所有主机
     */
    async probeAllServers() {
        this.serverLoading = true;
        try {
            const response = await fetch('/api/server/check-all', {
                method: 'POST',
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            if (data.success) {
                await this.loadServerList();
            }
        } catch (error) {
            console.error('探测主机失败:', error);
        } finally {
            this.serverLoading = false;
        }
    },

    /**
     * 加载历史指标记录
     */
    async loadMetricsHistory(page = null) {
        if (page !== null) {
            this.metricsHistoryPagination.page = page;
        }

        this.metricsHistoryLoading = true;

        try {
            // 计算时间范围
            let startTime = null;
            const now = Date.now();

            switch (this.metricsHistoryTimeRange) {
                case '1h': startTime = new Date(now - 60 * 60 * 1000).toISOString(); break;
                case '6h': startTime = new Date(now - 6 * 60 * 60 * 1000).toISOString(); break;
                case '24h': startTime = new Date(now - 24 * 60 * 60 * 1000).toISOString(); break;
                case '7d': startTime = new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(); break;
                case 'all': default: startTime = null;
            }

            const params = new URLSearchParams({
                page: this.metricsHistoryPagination.page,
                pageSize: this.metricsHistoryPagination.pageSize
            });

            if (this.metricsHistoryFilter.serverId) {
                params.append('serverId', this.metricsHistoryFilter.serverId);
            }

            if (startTime) {
                params.append('startTime', startTime);
            }

            const response = await fetch(`/api/server/metrics/history?${params}`, {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();

            if (data.success) {
                this.metricsHistoryList = data.data;
                this.metricsHistoryTotal = data.pagination.total;
                this.metricsHistoryPagination = {
                    page: data.pagination.page,
                    pageSize: data.pagination.pageSize,
                    totalPages: data.pagination.totalPages
                };
            }

            // 加载采集器状态
            this.loadCollectorStatus();

            // 渲染图表
            this.$nextTick(() => {
                this.renderMetricsCharts();
            });
        } catch (error) {
            console.error('加载历史指标失败:', error);
        } finally {
            this.metricsHistoryLoading = false;
        }
    },

    /**
     * 渲染历史指标图表
     */
    renderMetricsCharts() {
        if (!window.Chart || !this.groupedMetricsHistory) return;

        Object.entries(this.groupedMetricsHistory).forEach(([serverId, records]) => {
            const sortedRecords = [...records].reverse();
            const labels = sortedRecords.map(r => {
                const d = new Date(r.recorded_at);
                return d.getHours() + ':' + String(d.getMinutes()).padStart(2, '0');
            });
            const cpuData = sortedRecords.map(r => r.cpu_usage || 0);
            const memData = sortedRecords.map(r => r.mem_usage || 0);

            this.$nextTick(() => {
                const canvasId = `metrics-chart-${serverId}`;
                const canvas = document.getElementById(canvasId);
                if (!canvas) return;

                const existingChart = Chart.getChart(canvas);
                if (existingChart) existingChart.destroy();

                new Chart(canvas, {
                    type: 'line',
                    data: {
                        labels: labels,
                        datasets: [
                            {
                                label: 'CPU (%)',
                                data: cpuData,
                                borderColor: '#10b981',
                                backgroundColor: 'transparent',
                                borderWidth: 2.5,
                                fill: false,
                                tension: 0.3,
                                pointRadius: 0,
                                pointHoverRadius: 5
                            },
                            {
                                label: '内存 (%)',
                                data: memData,
                                borderColor: '#3b82f6',
                                backgroundColor: 'transparent',
                                borderWidth: 2.5,
                                fill: false,
                                tension: 0.3,
                                pointRadius: 0,
                                pointHoverRadius: 5
                            }
                        ]
                    },
                    options: {
                        responsive: true,
                        maintainAspectRatio: false,
                        plugins: {
                            legend: { display: false },
                            tooltip: {
                                mode: 'index',
                                intersect: false,
                                padding: 10,
                                backgroundColor: 'rgba(13, 17, 23, 0.9)',
                                titleColor: '#8b949e',
                                bodyColor: '#e6edf3',
                                borderColor: 'rgba(255, 255, 255, 0.1)',
                                borderWidth: 1
                            }
                        },
                        scales: {
                            x: {
                                display: true,
                                grid: {
                                    display: true,
                                    color: 'rgba(255, 255, 255, 0.06)',
                                    drawBorder: false
                                },
                                ticks: {
                                    maxRotation: 0,
                                    autoSkip: true,
                                    maxTicksLimit: 6,
                                    font: { size: 10 },
                                    color: '#6e7681'
                                }
                            },
                            y: {
                                display: true,
                                min: 0,
                                max: 100,
                                grid: {
                                    display: true,
                                    color: 'rgba(255, 255, 255, 0.06)',
                                    drawBorder: false
                                },
                                ticks: {
                                    font: { size: 10 },
                                    color: '#6e7681',
                                    stepSize: 25
                                }
                            }
                        },
                        interaction: {
                            mode: 'nearest',
                            axis: 'x',
                            intersect: false
                        }
                    }
                });
            });
        });
    },

    /**
     * 手动触发指标采集
     */
    async triggerMetricsCollect() {
        try {
            const response = await fetch('/api/server/metrics/collect', {
                method: 'POST',
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            if (data.success) {
                this.showGlobalToast('已触发历史指标采集', 'success');
                setTimeout(() => this.loadMetricsHistory(), 1000);
            }
        } catch (error) {
            console.error('触发采集失败:', error);
        }
    },

    /**
     * 设置指标时间范围
     */
    setMetricsTimeRange(range) {
        this.metricsHistoryTimeRange = range;
        this.loadMetricsHistory(1);
    },

    /**
     * 加载监控配置
     */
    async loadMonitorConfig() {
        try {
            const response = await fetch('/api/server/monitor/config', {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            if (data.success) {
                this.monitorConfig = data.data;
                // 同步更新显示用的采集间隔
                if (data.data.metrics_collect_interval) {
                    this.metricsCollectInterval = Math.floor(data.data.metrics_collect_interval / 60);
                }
            }
        } catch (error) {
            console.error('加载监控配置失败:', error);
        }
    },

    /**
     * 加载采集器状态
     */
    async loadCollectorStatus() {
        try {
            const response = await fetch('/api/server/metrics/collector/status', {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            if (data.success) {
                this.metricsCollectorStatus = data.data;
            }
        } catch (error) {
            console.error('加载采集器状态失败:', error);
        }
    },

    /**
     * 更新指标采集间隔
     */
    async updateMetricsCollectInterval() {
        try {
            // 更新 monitorConfig 中的值
            this.monitorConfig.metrics_collect_interval = parseInt(this.metricsCollectInterval) * 60;
            await this.updateMonitorConfig();
        } catch (error) {
            console.error('更新采集间隔失败:', error);
        }
    },

    /**
     * 更新监控全局配置
     */
    async updateMonitorConfig() {
        try {
            const response = await fetch('/api/server/monitor/config', {
                method: 'PUT',
                headers: {
                    ...this.getAuthHeaders(),
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(this.monitorConfig)
            });
            const data = await response.json();
            if (data.success) {
                this.showGlobalToast('配置已更新', 'success');
                this.loadCollectorStatus();
                // 重新加载配置以确保同步
                this.loadMonitorConfig();
            }
        } catch (error) {
            this.showGlobalToast('配置更新失败', 'error');
            console.error('更新配置失败:', error);
        }
    }
};

/**
 * 连接 Metrics WebSocket
 */
function connectMetricsWS() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/metrics`;

    console.log('[Metrics] Connecting to WebSocket:', wsUrl);
    const ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        console.log('[Metrics] WebSocket connected');
    };

    ws.onmessage = (event) => {
        try {
            const message = JSON.parse(event.data);
            if (message.type === 'metrics_update') {
                handleMetricsUpdate(message.data);
            }
        } catch (e) {
            console.error('[Metrics] Failed to parse message:', e);
        }
    };

    ws.onclose = () => {
        console.log('[Metrics] WebSocket closed, reconnecting in 5s...');
        setTimeout(connectMetricsWS, 5000);
    };

    ws.onerror = (error) => {
        console.error('[Metrics] WebSocket error:', error);
    };
}

/**
 * 处理 Metrics 更新
 */
function handleMetricsUpdate(metricsData) {
    if (!Array.isArray(metricsData)) return;

    let hasUpdates = false;

    metricsData.forEach(item => {
        const { serverId, metrics } = item;

        // 1. 更新内部状态
        state.metrics.set(serverId, metrics);

        // 2. 更新服务器在线状态 (如果 metrics 存在，说明在线)
        const server = state.servers.find(s => s.id === serverId);
        if (server) {
            if (server.status !== 'online') {
                server.status = 'online';
                hasUpdates = true;
            }

            // 3. 更新 UI
            updateServerCardMetrics(serverId, metrics);
        }
    });
}

/**
 * 更新单个主机卡片的 Metrics 显示
 */
function updateServerCardMetrics(serverId, metrics) {
    const card = document.querySelector(`.server-card[data-server-id="${serverId}"]`);
    if (!card) return;

    // 1. 更新 .server-quick-info 区域
    const quickInfo = card.querySelector('.server-quick-info');
    if (quickInfo) {
        quickInfo.innerHTML = renderQuickMetrics(metrics);
    }

    // 2. 更新状态指示灯和徽标
    const indicator = card.querySelector('.server-status-indicator');
    if (indicator && !indicator.classList.contains('online')) {
        indicator.className = 'server-status-indicator online';
    }

    const badge = card.querySelector('.proxied-badge');
    if (badge && !badge.classList.contains('proxied-on')) {
        badge.className = 'proxied-badge proxied-on';
        badge.textContent = '在线';
    }

    // 3. 如果卡片当前处于展开状态，且正在显示“加载中”，则刷新整个卡片内容以显示详情
    const isExpanded = card.classList.contains('expanded');
    const detailsContainer = card.querySelector('.server-card-body');
    if (isExpanded && detailsContainer && detailsContainer.innerText.includes('正在加载')) {
        const server = state.servers.find(s => s.id === serverId);
        if (server) {
            detailsContainer.innerHTML = renderServerDetails(server, state.serverInfo.get(serverId));
        }
    }

    // 4. 同步更新后台管理表格中的状态（如果存在）
    const tableRow = document.querySelector(`tr:has(button[onclick*="'${serverId}'"])`);
    if (tableRow) {
        const rowBadge = tableRow.querySelector('.proxied-badge');
        if (rowBadge && !rowBadge.classList.contains('proxied-on')) {
            rowBadge.className = 'proxied-badge proxied-on';
            rowBadge.textContent = '在线';
        }
    }
}

/**
 * 渲染快速指标 HTML
 */
function renderQuickMetrics(metrics) {
    if (!metrics) return '';

    // 解析 CPU
    const cpu = metrics.cpu_usage || '0%';
    const cpuVal = parseFloat(cpu);
    const cpuClass = cpuVal > 80 ? 'text-danger' : (cpuVal > 50 ? 'text-warning' : 'text-success');

    // 解析内存
    // metrics.mem_usage 格式可能是 "512/1024MB"
    let memPercent = 0;
    const memStr = metrics.mem_usage || '';
    if (memStr.includes('/')) {
        const [used, total] = memStr.replace('MB', '').split('/');
        if (total > 0) memPercent = (used / total) * 100;
    }
    const memClass = memPercent > 80 ? 'text-danger' : (memPercent > 50 ? 'text-warning' : 'text-success');

    // 网络
    const rx = metrics.network?.rx_speed || '0B/s';
    const tx = metrics.network?.tx_speed || '0B/s';

    return `
        <div class="metric-pill" title="CPU 使用率">
            <i class="fas fa-microchip ${cpuClass}"></i>
            <span>${cpu}</span>
        </div>
        <div class="metric-pill" title="内存使用率">
            <i class="fas fa-memory ${memClass}"></i>
            <span>${Math.round(memPercent)}%</span>
        </div>
        <div class="metric-pill" title="网络下行">
            <i class="fas fa-download"></i>
            <span>${rx}</span>
        </div>
        <div class="metric-pill" title="网络上行">
            <i class="fas fa-upload"></i>
            <span>${tx}</span>
        </div>
    `;
}

// 导出函数到全局作用域...
window.serverModule = {
    // ...
    formatHost // 导出格式化函数
};

