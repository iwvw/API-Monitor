/**
 * 系统日志模块
 * 处理日志 WebSocket 连接和 UI 交互
 */

export const systemLogsMethods = {
  // 初始化系统日志 WebSocket
  initLogWs() {
    if (this.logWs && this.logWs.readyState === WebSocket.OPEN) return;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    // 修复：如果访问地址是 0.0.0.0，替换为 localhost（0.0.0.0 是服务器监听地址，不能用作客户端连接）
    let wsHost = window.location.host;
    if (wsHost.startsWith('0.0.0.0')) {
      wsHost = wsHost.replace('0.0.0.0', 'localhost');
    }
    const wsUrl = `${protocol}//${wsHost}/ws/logs`;

    this.logWsConnecting = true;
    console.log('📡 正在连接日志流:', wsUrl);
    this.logWs = new WebSocket(wsUrl);

    this.logWs.onopen = () => {
      this.logWsConnected = true;
      this.logWsConnecting = false;
      console.log('✅ 系统日志 WebSocket 已连接');
    };

    this.logWs.onmessage = (event) => {
      const message = JSON.parse(event.data);
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
        if (this.systemLogs.length > 500) {
          this.systemLogs.shift();
        }
        this.scrollToBottom();
      }
    };

    this.logWs.onclose = (event) => {
      const wasConnected = this.logWsConnected;  // 保存之前的连接状态
      this.logWsConnected = false;
      this.logWsConnecting = false;
      console.log('❌ 系统日志 WebSocket 已断开', event.code, event.reason);

      // 只有在之前已成功连接过，且启用了自动重连，且设置页面仍然打开时才重连
      // 这样可以避免在初次连接失败时无限重试
      if (wasConnected && this.logWsAutoReconnect && this.showSettingsModal && this.settingsCurrentTab === 'logs') {
        console.log('🔄 将在 3 秒后尝试重新连接...');
        setTimeout(() => this.initLogWs(), 3000);
      }
    };

    this.logWs.onerror = (error) => {
      console.error('WebSocket Error:', error);
      // 注意：onerror 之后通常会紧跟 onclose，所以这里不需要设置状态
      // onclose 已经会处理状态更新
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

  clearDisplayLogs() {
    this.systemLogs = [];
  }
};