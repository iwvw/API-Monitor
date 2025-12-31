/**
 * useWebSocket Composable
 * 管理 WebSocket 连接的可复用逻辑
 */
import { ref, onUnmounted } from 'vue';

/**
 * 创建 WebSocket 连接管理
 * @param {string} url - WebSocket URL
 * @param {Object} options - 配置选项
 * @param {boolean} options.autoConnect - 是否自动连接
 * @param {boolean} options.autoReconnect - 是否自动重连
 * @param {number} options.reconnectInterval - 重连间隔 (ms)
 * @param {number} options.maxReconnectAttempts - 最大重连次数
 * @param {Function} options.onMessage - 消息处理函数
 * @param {Function} options.onOpen - 连接打开回调
 * @param {Function} options.onClose - 连接关闭回调
 * @param {Function} options.onError - 错误回调
 * @returns {Object} WebSocket 状态和方法
 */
export function useWebSocket(url, options = {}) {
  const {
    autoConnect = false,
    autoReconnect = true,
    reconnectInterval = 3000,
    maxReconnectAttempts = 5,
    onMessage,
    onOpen,
    onClose,
    onError,
  } = options;

  const ws = ref(null);
  const isConnected = ref(false);
  const isConnecting = ref(false);
  const error = ref(null);
  const reconnectAttempts = ref(0);
  const lastMessage = ref(null);

  let reconnectTimer = null;

  /**
   * 建立 WebSocket 连接
   */
  function connect() {
    if (
      ws.value &&
      (ws.value.readyState === WebSocket.OPEN || ws.value.readyState === WebSocket.CONNECTING)
    ) {
      return;
    }

    isConnecting.value = true;
    error.value = null;

    try {
      ws.value = new WebSocket(url);

      ws.value.onopen = event => {
        isConnected.value = true;
        isConnecting.value = false;
        reconnectAttempts.value = 0;
        if (onOpen) onOpen(event);
      };

      ws.value.onmessage = event => {
        lastMessage.value = event.data;
        if (onMessage) {
          try {
            const data = JSON.parse(event.data);
            onMessage(data, event);
          } catch {
            onMessage(event.data, event);
          }
        }
      };

      ws.value.onclose = event => {
        isConnected.value = false;
        isConnecting.value = false;
        if (onClose) onClose(event);

        // 自动重连
        if (autoReconnect && !event.wasClean && reconnectAttempts.value < maxReconnectAttempts) {
          scheduleReconnect();
        }
      };

      ws.value.onerror = event => {
        error.value = event;
        isConnecting.value = false;
        if (onError) onError(event);
      };
    } catch (err) {
      error.value = err;
      isConnecting.value = false;
      if (autoReconnect && reconnectAttempts.value < maxReconnectAttempts) {
        scheduleReconnect();
      }
    }
  }

  /**
   * 关闭 WebSocket 连接
   */
  function disconnect() {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }

    if (ws.value) {
      ws.value.close();
      ws.value = null;
    }

    isConnected.value = false;
    isConnecting.value = false;
    reconnectAttempts.value = 0;
  }

  /**
   * 发送消息
   * @param {*} data - 要发送的数据
   */
  function send(data) {
    if (!ws.value || ws.value.readyState !== WebSocket.OPEN) {
      console.warn('[WebSocket] 连接未就绪，无法发送消息');
      return false;
    }

    const message = typeof data === 'string' ? data : JSON.stringify(data);
    ws.value.send(message);
    return true;
  }

  /**
   * 计划重连
   */
  function scheduleReconnect() {
    if (reconnectTimer) return;

    reconnectAttempts.value++;
    console.log(
      `[WebSocket] 将在 ${reconnectInterval}ms 后重连 (${reconnectAttempts.value}/${maxReconnectAttempts})`
    );

    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      connect();
    }, reconnectInterval);
  }

  // 自动连接
  if (autoConnect) {
    connect();
  }

  // 组件卸载时断开连接
  onUnmounted(() => {
    disconnect();
  });

  return {
    ws,
    isConnected,
    isConnecting,
    error,
    reconnectAttempts,
    lastMessage,

    connect,
    disconnect,
    send,
  };
}

export default useWebSocket;
