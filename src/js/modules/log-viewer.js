/**
 * 统一日志查看器模块
 * 处理所有模块的日志显示、过滤、搜索和流式更新
 */

import { store } from '../store.js';
import { toast } from './toast.js';

export const logViewerMethods = {
    /**
     * 打开日志查看器
     * @param {Object} options 配置项
     * @param {string} options.title 标题
     * @param {string} options.subtitle 副标题 (可选)
     * @param {string} options.source 来源标识 ('zeabur', 'koyeb', 'system')
     * @param {Function} options.fetcher 初始数据获取函数 (可选)
     * @param {Function} options.streamer 流式连接函数 (可选)
     * @param {Function} options.cleaner 清理函数 (关闭时调用)
     */
    async openLogViewer(options = {}) {
        // 重置状态
        store.logViewer.visible = true;
        store.logViewer.title = options.title || '日志查看器';
        store.logViewer.subtitle = options.subtitle || '';
        store.logViewer.source = options.source || 'generic';
        store.logViewer.logs = [];
        store.logViewer.loading = true;
        store.logViewer.filterText = '';
        store.logViewer.levelFilter = 'ALL';
        store.logViewer.streamActive = false;

        // 保存清理函数引用
        this._logViewerCleaner = options.cleaner;

        // 加载初始数据
        if (options.fetcher) {
            try {
                const logs = await options.fetcher();
                this.appendLogs(logs);
            } catch (error) {
                this.appendLogs([{
                    timestamp: Date.now(),
                    level: 'ERROR',
                    message: '无法加载日志: ' + error.message
                }]);
            } finally {
                store.logViewer.loading = false;
            }
        } else {
            store.logViewer.loading = false;
        }

        // 启动流
        if (options.streamer) {
            store.logViewer.streamActive = true;
            options.streamer((newLog) => this.appendLogs(newLog));
        }
    },

    /**
     * 关闭日志查看器
     */
    closeLogViewer() {
        store.logViewer.visible = false;
        store.logViewer.streamActive = false;

        // 执行清理 (如断开 WebSocket)
        if (this._logViewerCleaner) {
            this._logViewerCleaner();
            this._logViewerCleaner = null;
        }
    },

    /**
     * 追加日志
     * @param {Array|Object|String} logs 日志数据
     */
    appendLogs(logs) {
        if (!logs) return;

        const newEntries = [];
        const processLog = (log) => {
            if (typeof log === 'string') {
                return this._parseLogString(log);
            } else if (typeof log === 'object') {
                // 已经是对象，确保有必要字段
                // 处理 message：如果是对象需要序列化
                let messageStr = log.message;
                if (typeof messageStr === 'object' && messageStr !== null) {
                    messageStr = JSON.stringify(messageStr);
                } else if (messageStr === undefined || messageStr === null) {
                    messageStr = JSON.stringify(log);
                }
                return {
                    id: log.id || Date.now() + Math.random().toString(36).substr(2, 9),
                    timestamp: log.timestamp || Date.now(),
                    level: log.level || this._detectLevel(String(messageStr)) || 'INFO',
                    module: log.module || '',
                    message: String(messageStr),
                    raw: log.raw || messageStr
                };
            }
            return null;
        };

        if (Array.isArray(logs)) {
            logs.forEach(l => {
                const entry = processLog(l);
                if (entry) newEntries.push(entry);
            });
        } else {
            const entry = processLog(logs);
            if (entry) newEntries.push(entry);
        }

        // 添加到 Store (使用 Object.freeze 提高性能)
        // 限制最大条数，防止浏览器崩溃
        const MAX_LOGS = 5000;
        if (store.logViewer.logs.length + newEntries.length > MAX_LOGS) {
            const removeCount = (store.logViewer.logs.length + newEntries.length) - MAX_LOGS;
            store.logViewer.logs.splice(0, removeCount);
        }

        store.logViewer.logs.push(...newEntries);

        // 自动滚动
        if (store.logViewer.autoScroll) {
            this.scrollToLogBottom();
        }
    },

    /**
     * 滚动到底部
     */
    scrollToLogBottom() {
        // 使用 setTimeout 确保 DOM 更新后滚动
        setTimeout(() => {
            const container = document.getElementById('log-viewer-container');
            if (container) {
                container.scrollTop = container.scrollHeight;
            }
        }, 10);
    },

    /**
     * 切换自动滚动
     */
    toggleLogAutoScroll() {
        store.logViewer.autoScroll = !store.logViewer.autoScroll;
        if (store.logViewer.autoScroll) {
            this.scrollToLogBottom();
        }
    },

    /**
     * 切换换行
     */
    toggleLogWrap() {
        store.logViewer.wrapText = !store.logViewer.wrapText;
    },

    /**
     * 清空日志
     */
    clearLogs() {
        store.logViewer.logs = [];
    },

    /**
     * 下载日志
     */
    downloadLogs() {
        if (store.logViewer.logs.length === 0) {
            toast.info('没有日志可下载');
            return;
        }

        const content = store.logViewer.logs.map(l => {
            const time = new Date(l.timestamp).toLocaleString();
            return `[${time}] [${l.level}] ${l.message}`;
        }).join('\n');

        const blob = new Blob([content], { type: 'text/plain' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `logs-${store.logViewer.source}-${new Date().toISOString().slice(0, 19)}.log`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    },

    // ============ 内部辅助方法 ============ 

    _parseLogString(l) {
        const str = String(l || '');
        // 简单解析，尝试提取时间和级别
        // 示例: "2023-10-27 10:00:00 [INFO] message"
        const timeMatch = str.match(/^\(?(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2})\]?/);
        // 支持带颜色的级别匹配，如 [32mINFO[39m
        const levelMatch = str.match(/\[(?:\d+m)?(INFO|WARN|ERROR|DEBUG|FATAL)(?:\d+m)?\]/i);

        return {
            id: Date.now() + Math.random().toString(36).substr(2, 9),
            timestamp: timeMatch ? new Date(timeMatch[1]).getTime() : Date.now(),
            level: levelMatch ? levelMatch[1].toUpperCase() : this._detectLevel(str),
            message: str, // 保留原始消息，由 formatMessage 处理 ANSI
            raw: str
        };
    },

    _detectLevel(str) {
        if (!str) return 'INFO';
        const s = str.toLowerCase();
        if (s.includes('error') || s.includes('fail') || s.includes('exception')) return 'ERROR';
        if (s.includes('warn')) return 'WARN';
        if (s.includes('debug')) return 'DEBUG';
        return 'INFO';
    },

    /**
     * 获取日志级别图标
     */
    getLogIcon(level) {
        const icons = {
            'INFO': 'fa-info-circle',
            'WARN': 'fa-exclamation-triangle',
            'ERROR': 'fa-times-circle',
            'DEBUG': 'fa-bug',
            'FATAL': 'fa-skull'
        };
        return icons[level] || 'fa-circle';
    },

    /**
     * 格式化日志消息 (处理 ANSI 颜色)
     */
    formatMessage(msg) {
        if (!msg) return '';

        // 预处理：有些环境可能会丢失 ESC，或者显示为字面量
        // 如果字符串包含 [32m 但不包含 ESC，尝试补充
        let processedMsg = msg;
        if (!/\x1B/.test(processedMsg) && /\[\d{1,2}m/.test(processedMsg)) {
            // 只有当看起来像 ANSI 代码时才替换
            processedMsg = processedMsg.replace(/\[(\d{1,2})m/g, '\x1B[$1m');
        }

        // 替换 ANSI 颜色代码为 HTML span
        let html = processedMsg.replace(/(?:\x1B|\\u001b|\\033)\[(\d+)m/g, (match, code) => {
            if (code === '0' || code === '39') {
                return '</span>';
            }
            // 仅支持前景颜色 30-37 (及高亮 90-97)
            if ((code >= 30 && code <= 37) || (code >= 90 && code <= 97)) {
                // 简化映射: 9x -> 3x (bright colors)
                const baseCode = code >= 90 ? code - 60 : code;
                return `<span class="ansi-fg-${baseCode}">`;
            }
            return ''; // 忽略其他代码
        });

        // 自动补全未闭合的 span
        const openCount = (html.match(/<span/g) || []).length;
        const closeCount = (html.match(/<\/span>/g) || []).length;
        if (openCount > closeCount) {
            html += '</span>'.repeat(openCount - closeCount);
        }

        return html;
    },

    // 计算属性对应的过滤列表 (需要在组件中实现或在模板中使用)
    getFilteredLogs() {
        let logs = store.logViewer.logs;

        // 级别过滤
        if (store.logViewer.levelFilter !== 'ALL') {
            logs = logs.filter(l => l.level === store.logViewer.levelFilter);
        }

        // 文本搜索
        if (store.logViewer.filterText) {
            const lowerFilter = store.logViewer.filterText.toLowerCase();
            logs = logs.filter(l => l.message.toLowerCase().includes(lowerFilter));
        }

        return logs;
    }
};
