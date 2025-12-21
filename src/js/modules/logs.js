/**
 * 系统日志模块
 * 处理日志 WebSocket 连接和 UI 交互
 */

import { store } from '../store.js';
import { toast } from './toast.js';


export const systemLogsMethods = {
  // 打开统一的系统日志查看器
  openSystemLogViewer() {
    this.openLogViewer({
      title: '系统实时日志',
      subtitle: 'System Operation Logs',
      source: 'system',
      fetcher: async () => {
        // 获取最近的日志快照
        const response = await fetch('/api/settings/sys-logs', {
          headers: this.getAuthHeaders()
        });
        const result = await response.json();
        if (result.success) {
          // 适配日志格式 - 处理对象类型的日志
          return result.data.map(l => {
            // 如果 l 是对象
            if (typeof l === 'object' && l !== null) {
              let msg = l.message;
              // 如果 message 也是对象，序列化它
              if (typeof msg === 'object' && msg !== null) {
                msg = JSON.stringify(msg);
              }
              return {
                timestamp: l.timestamp || Date.now(),
                level: l.level || 'INFO',
                module: l.module || 'core',
                message: String(msg || JSON.stringify(l))
              };
            }
            // 如果是字符串，尝试解析
            const str = String(l || '');
            const match = str.match(/^\[(.*?)\] (.*)/);
            return {
              timestamp: Date.now(),
              level: match ? match[1] : 'INFO',
              message: match ? match[2] : str
            };
          });
        }
        return [];
      },
      streamer: (appendLog) => {
        // 连接 WebSocket 并将消息转发给 viewer
        this.initLogWs((log) => {
          // 确保 message 是字符串
          let msg = log.message;
          if (typeof msg === 'object' && msg !== null) {
            msg = JSON.stringify(msg);
          }
          appendLog({
            timestamp: log.timestamp || Date.now(),
            level: log.level || 'INFO',
            module: log.module || 'core',
            message: String(msg || '')
          });
        });
      },
      cleaner: () => {
        this.closeLogWs();
      }
    });
  },

  // 修复：viewRawAppLog 方法缺失，补充该方法
  async viewRawAppLog() {
    this.openLogViewer({
      title: '系统原始日志文件',
      subtitle: 'app.log (Raw File Content)',
      source: 'system-raw',
      fetcher: async () => {
        const response = await fetch('/api/settings/app-log-file', {
          headers: this.getAuthHeaders()
        });
        const result = await response.json();
        if (result.success && result.data) {
          // 原始文件内容通常是大段文本，按行分割处理
          const lines = result.data.split('\n');
          return lines.map((line, index) => {
            if (!line.trim()) return null;
            // 尝试解析常见日志格式，如果无法解析则作为纯文本
            const match = line.match(/^\[(.*?)\] (.*)/);
            return {
              id: `raw-${index}`,
              timestamp: Date.now(), // 文件日志可能无统一时间戳格式，暂用当前或尝试解析
              level: match ? match[1] : 'INFO',
              message: line
            };
          }).filter(l => l);
        }
        return [];
      }
    });
  },

  // 初始化系统日志 WebSocket (兼容旧版直接更新 store 和新版回调)
  initLogWs(onMessage) {
    // 如果已经连接且是为了同一个目的（有无回调状态一致），则复用
    // 但为了简化，如果提供了回调，我们总是重新挂载监听器或处理逻辑

    // 如果已有连接但没有回调（旧模式），而现在需要回调（新模式），可能需要重新绑定 onmessage
    // 这里采用简单策略：始终允许新的连接请求刷新处理逻辑
    if (this.logWs) {
      // 如果连接已打开，直接复用，但更新 onmessage 处理逻辑以支持多播（如果需要）
      // 简单起见，我们假设同一时间主要关注一种查看方式（全屏或小窗）
      // 但为了稳健性，我们让它同时支持更新 store.systemLogs 和 执行回调

      const oldOnMessage = this.logWs.onmessage;
      this.logWs.onmessage = (event) => {
        const message = JSON.parse(event.data);

        // 1. 始终更新 Store 中的 systemLogs (供设置页面小窗口使用)
        const formatEntry = (entry) => ({
          time: entry.timestamp ? new Date(entry.timestamp).toLocaleTimeString('zh-CN', { hour12: false }) : '00:00:00',
          level: entry.level || 'INFO',
          module: entry.module || 'core',
          message: entry.message + (entry.data ? ` [DATA]` : '')
        });

        if (message.type === 'init') {
          this.systemLogs = (message.data || []).map(formatEntry);
          this.scrollToBottom();
        } else if (message.type === 'log') {
          this.systemLogs.push(formatEntry(message.data));
          if (this.systemLogs.length > 200) { // 小窗口保留少一点
            this.systemLogs.shift();
          }
          this.scrollToBottom();
        }

        // 2. 如果有回调，也执行回调 (供全功能查看器使用)
        if (message.type === 'log' && onMessage) {
          onMessage(message.data);
        }
      };

      if (this.logWs.readyState === WebSocket.OPEN) {
        return; // 已连接，只需更新处理逻辑
      }
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    let wsHost = window.location.host;
    if (wsHost.startsWith('0.0.0.0')) {
      wsHost = wsHost.replace('0.0.0.0', 'localhost');
    }
    const wsUrl = `${protocol}//${wsHost}/ws/logs`;

    this.logWsConnecting = true;
    this.logWs = new WebSocket(wsUrl);

    this.logWs.onopen = () => {
      this.logWsConnected = true;
      this.logWsConnecting = false;
      console.log('✅ 系统日志 WebSocket 已连接');
    };

    this.logWs.onmessage = (event) => {
      const message = JSON.parse(event.data);

      // 1. 更新 Store (设置页面小窗口)
      const formatEntry = (entry) => ({
        time: entry.timestamp ? new Date(entry.timestamp).toLocaleTimeString('zh-CN', { hour12: false }) : '00:00:00',
        level: entry.level || 'INFO',
        module: entry.module || 'core',
        message: entry.message + (entry.data ? ` [DATA]` : '')
      });

      if (message.type === 'init') {
        this.systemLogs = (message.data || []).map(formatEntry);
        this.scrollToBottom();
      } else if (message.type === 'log') {
        this.systemLogs.push(formatEntry(message.data));
        if (this.systemLogs.length > 200) {
          this.systemLogs.shift();
        }
        this.scrollToBottom();
      }

      // 2. 执行回调 (全功能查看器)
      if (message.type === 'log' && onMessage) {
        onMessage(message.data);
      }
    };

    this.logWs.onclose = () => {
      this.logWsConnected = false;
      this.logWsConnecting = false;
      console.log('❌ 系统日志 WebSocket 已断开');
    };

    this.logWs.onerror = (err) => {
      console.error('WebSocket Error:', err);
    };
  },

  // 手动连接日志流
  connectLogStream() {
    this.logWsAutoReconnect = true;
    this.initLogWs();
  },

  // 手动断开日志流
  disconnectLogStream() {
    this.logWsAutoReconnect = false;
    this.closeLogWs();
  },

  // 切换日志流连接状态
  toggleLogStream() {
    if (this.logWsConnected || this.logWsConnecting) {
      this.disconnectLogStream();
    } else {
      this.connectLogStream();
    }
  },

  closeLogWs() {
    if (this.logWs) {
      // 使用一个标志位避免 onclose 触发自动重连
      this.logWsAutoReconnect = false;
      this.logWs.close();
      this.logWs = null;
    }
  },

  scrollToBottom() {
    if (!this.autoScrollLogs) return;

    this.$nextTick(() => {
      const container = this.$refs.systemLogsContainer || this.$refs.settingsLogStream;
      if (container) {
        container.scrollTop = container.scrollHeight;
      }
    });
  },

  formatLogTime(timestamp) {
    const date = new Date(timestamp);
    return date.toLocaleTimeString('zh-CN', { hour12: false }) + '.' +
      String(date.getMilliseconds()).padStart(3, '0');
  },

  formatLogData(data) {
    if (typeof data === 'string') return data;
    return JSON.stringify(data, null, 2);
  },

  // 清空系统日志 (物理文件 + 内存视图)
  async clearAppLogs() {
    const confirmed = await store.showConfirm({
      title: '确认清空日志文件？',
      message: '这将永久删除 app.log 文件内容并清空当前视图，建议操作前先下载备份。',
      icon: 'fa-trash-alt',
      confirmText: '确定物理清空',
      confirmClass: 'btn-danger'
    });

    if (!confirmed) return;

    try {
      const response = await fetch('/api/settings/clear-app-logs', {
        method: 'POST',
        headers: this.getAuthHeaders()
      });
      const result = await response.json();
      if (result.success) {
        this.systemLogs = [];
        toast.success('系统日志已物理清空');
        // 刷新文件大小显示
        if (typeof this.fetchLogSettings === 'function') {
          this.fetchLogSettings();
        }
      } else {
        throw new Error(result.error);
      }
    } catch (error) {
      toast.error('清空失败: ' + error.message);
    }
  },

  clearDisplayLogs() {
    this.systemLogs = [];
  }
};