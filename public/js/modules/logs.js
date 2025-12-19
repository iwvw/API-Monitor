/**
 * 系统日志模块
 * 处理日志 WebSocket 连接和 UI 交互
 */

export const systemLogsMethods = {
  // 初始化系统日志 WebSocket
  initLogWs() {
    if (this.logWs && this.logWs.readyState === WebSocket.OPEN) return;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/logs`;

    this.logWs = new WebSocket(wsUrl);

    this.logWs.onopen = () => {
      this.logWsConnected = true;
      console.log('✅ 系统日志 WebSocket 已连接');
    };

    this.logWs.onmessage = (event) => {
      const message = JSON.parse(event.data);
      if (message.type === 'init') {
        this.systemLogs = message.data;
        this.scrollToBottom();
      } else if (message.type === 'log') {
        this.systemLogs.push(message.data);
        if (this.systemLogs.length > 500) {
          this.systemLogs.shift();
        }
        this.scrollToBottom();
      }
    };

    this.logWs.onclose = () => {
      this.logWsConnected = false;
      console.log('❌ 系统日志 WebSocket 已断开');
      // 如果模态框还打开着，3秒后尝试重连
      if (this.showSystemLogsModal) {
        setTimeout(() => this.initLogWs(), 3000);
      }
    };

    this.logWs.onerror = (error) => {
      console.error('WebSocket Error:', error);
      this.logWsConnected = false;
    };
  },

  closeLogWs() {
    if (this.logWs) {
      this.logWs.close();
      this.logWs = null;
    }
  },

  scrollToBottom() {
    if (!this.autoScrollLogs) return;

    this.$nextTick(() => {
      const container = this.$refs.systemLogsContainer;
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