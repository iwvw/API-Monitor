/**
 * SSH 终端管理模块
 * 负责 SSH 会话管理、终端初始化、分屏布局、主题切换等
 */

// xterm imports moved to initSessionTerminal for lazy loading
import { toast } from './toast.js';
import { sshSplitMethods } from './ssh-split.js';

/**
 * SSH 终端方法集合
 */
export const sshMethods = {
  /**
   * 打开 SSH 终端(切换到 IDE 视图)
   */
  openSSHTerminal(server) {
    if (!server) return;

    // 检查是否已经打开了该主机的终端
    const existingSession = this.sshSessions.find(s => s.server.id === server.id);
    if (existingSession) {
      this.switchToSSHTab(existingSession.id);
      return;
    }

    const sessionId = 'session_' + Date.now();

    // 智能选择连接方式: 优先遵循 monitor_mode，或者如果 Agent 在线且 SSH 不在线则选 Agent
    let type = 'ssh';
    if (server.monitor_mode === 'agent') {
      type = 'agent';
    } else if (server.status === 'online' && (!server.host || server.host === '0.0.0.0')) {
      // 如果没有真实 IP 且 Agent 在线，默认为 Agent 模式
      type = 'agent';
    }

    const session = {
      id: sessionId,
      server: server,
      terminal: null,
      fit: null,
      ws: null,
      connected: false,
      type: type, // 'ssh' | 'agent'
      // Agent 模式专有状态
      buffer: '',
      history: [],
      historyIndex: -1,
    };

    // 核心修复：在新打开会话或切换前，先将当前所有可见终端 DOM 归还给仓库，防止被 Vue 销毁
    this.saveTerminalsToWarehouse();

    this.sshSessions.push(session);

    // 核心优化：新开终端作为独立标签，挂起现有分屏组（如果存在）
    if (this.sshViewLayout !== 'single') {
      // 如果已经在分屏，切出到单屏，不破坏现有分屏组
      this._switchOutToSingle(sessionId);
    } else {
      this.activeSSHSessionId = sessionId;
    }

    this.activeSSHSessionId = sessionId;
    this.serverCurrentTab = 'terminal';

    this.$nextTick(async () => {
      await this.initSessionTerminal(sessionId);
      // 核心修复：调度智能同步，处理 DOM 挂载和多级尺寸适配补偿
      this.scheduleSync();
    });
  },

  /**
   * 切换当前激活的 SSH 会话
   */
  switchToSSHTab(sessionId) {
    // 核心修复：在物理状态切换前，确保 DOM 节点已安全归还仓库
    this.saveTerminalsToWarehouse();

    this.serverCurrentTab = 'terminal';
    this.activeSSHSessionId = sessionId;

    const groupState = this.sshGroupState;
    const isInGroup = groupState && groupState.ids.includes(sessionId);

    if (isInGroup) {
      // 如果目标在分屏组中，则恢复分屏视图
      this._restoreGroupView();
    } else {
      // 如果目标是单屏会话，则切出到单屏模式（挂起分屏组）
      this._switchOutToSingle(sessionId);
    }

    this.$nextTick(() => {
      this.syncTerminalDOM(); // 同步 DOM 节点位置
      this.fitAllVisibleSessions();
      const session = this.getSessionById(sessionId);
      if (session && session.terminal) session.terminal.focus();
    });
  },

  /**
   * 关闭SSH会话
   */
  closeSSHSession(sessionId) {
    // 核心修复：在关闭和状态变更前，先将所有终端 DOM 归还仓库，防止布局重排导致节点丢失
    this.saveTerminalsToWarehouse();

    const index = this.sshSessions.findIndex(s => s.id === sessionId);
    if (index === -1) return;

    const session = this.sshSessions[index];

    // 清除心跳定时器
    if (session.heartbeatInterval) {
      clearInterval(session.heartbeatInterval);
      session.heartbeatInterval = null;
    }

    // 关闭 WebSocket 连接
    if (session.ws) {
      if (session.ws.readyState === WebSocket.OPEN) {
        session.ws.send(JSON.stringify({ type: 'disconnect' }));
      }
      session.ws.close();
    }

    // 移除 resize 监听器
    if (session.resizeHandler) {
      window.removeEventListener('resize', session.resizeHandler);
    }

    // 清理 ResizeObserver
    if (session.resizeObserver) {
      session.resizeObserver.disconnect();
    }

    // 销毁终端实例
    if (session.terminal) {
      session.terminal.dispose();
    }

    // 核心修复：从全局仓库中彻底删除该节点的 DOM 元素
    const terminalEl = document.getElementById('ssh-terminal-' + sessionId);
    if (terminalEl) {
      terminalEl.remove();
    }

    // 从分屏视图中移除 (如果有)
    if (this.visibleSessionIds && this.visibleSessionIds.includes(sessionId)) {
      this.removeFromSplitView(sessionId);
    }

    // 从数组中移除
    this.sshSessions.splice(index, 1);

    // 如果关闭的是当前激活的会话，切换到其他会话
    if (this.activeSSHSessionId === sessionId) {
      if (this.sshSessions.length > 0) {
        // 切换到下一个可用的会话（优先选择列表中的最后一个）
        const nextSession = this.sshSessions[this.sshSessions.length - 1];
        this.switchToSSHTab(nextSession.id);
      } else {
        // 如果没有会话了，清空激活ID并返回主机列表
        this.activeSSHSessionId = null;
        this.serverCurrentTab = 'list';
      }
    }
  },

  /**
   * 重新连接SSH会话
   */
  reconnectSSHSession(sessionId) {
    const session = this.sshSessions.find(s => s.id === sessionId);
    if (!session) return;

    console.log(`[SSH ${sessionId}] 开始重新连接...`);

    // 清除心跳定时器
    if (session.heartbeatInterval) {
      clearInterval(session.heartbeatInterval);
      session.heartbeatInterval = null;
    }

    // 如果已连接，先断开
    if (session.ws) {
      if (session.ws.readyState === WebSocket.OPEN) {
        session.ws.send(JSON.stringify({ type: 'disconnect' }));
      }
      session.ws.close();
      session.ws = null;
    }

    // 清空终端并显示重连信息
    if (session.terminal) {
      session.terminal.clear();
      session.terminal.writeln(
        `\x1b[1;33m正在重新连接到 ${session.server.name} (${this.formatHost(session.server.host)})...\x1b[0m`
      );
    }

    // 建立新的 WebSocket 连接
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws/ssh`);
    session.ws = ws;

    ws.onopen = () => {
      console.log(`[SSH ${sessionId}] WebSocket 已重新连接`);
      ws.send(
        JSON.stringify({
          type: 'connect',
          serverId: session.server.id,
          protocol: session.type, // 修复：重连时必须携带协议类型 (agent/ssh)
          cols: session.terminal.cols,
          rows: session.terminal.rows,
        })
      );

      // 启动心跳保活
      session.heartbeatInterval = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'ping' }));
        }
      }, 30000);
    };

    ws.onmessage = event => {
      try {
        const msg = JSON.parse(event.data);
        switch (msg.type) {
          case 'connected':
            session.connected = true;
            session.terminal.writeln(`\x1b[1;32m${msg.message}\x1b[0m`);
            session.terminal.writeln('');
            break;
          case 'output':
            session.terminal.write(msg.data);
            break;
          case 'error':
            session.terminal.writeln(`\x1b[1;31m错误: ${msg.message}\x1b[0m`);
            break;
          case 'disconnected':
            session.connected = false;
            session.terminal.writeln('');
            session.terminal.writeln(`\x1b[1;33m${msg.message}\x1b[0m`);
            break;
        }
      } catch (e) {
        console.error('解析消息失败:', e);
      }
    };

    ws.onerror = () => {
      session.terminal.writeln('\x1b[1;31mWebSocket 连接错误\x1b[0m');
    };

    ws.onclose = () => {
      console.log(`[SSH ${sessionId}] WebSocket 已关闭`);

      // 清除心跳定时器
      if (session.heartbeatInterval) {
        clearInterval(session.heartbeatInterval);
        session.heartbeatInterval = null;
      }

      if (session.connected) {
        session.terminal.writeln('');
        session.terminal.writeln('\x1b[1;33m连接已断开。点击"重新连接"按钮恢复连接。\x1b[0m');
      }
      session.connected = false;
    };
  },

  // ==================== SSH 分屏逻辑 (已移至 ssh-split.js) ====================

  /**
   * 对所有当前可见的终端执行 Fit 序列，解决布局切换时的尺寸计算错位
   */
  /**
   * 手动计算并调整终端尺寸 (替代不稳定的 FitAddon)
   */
  /**
   * 手动调整终端大小 (兼容 FitAddon)
   */
  manualTerminalResize(session) {
    if (!session || !session.terminal) return;

    // 如果有 FitAddon，优先使用
    if (session.fit) {
      try {
        session.fit.fit();
        // 同步后端
        this.syncTerminalSize(session);
        return;
      } catch (e) {
        console.warn('FitAddon failed, falling back to manual resize', e);
      }
    }

    const terminal = session.terminal;
    const container = document.getElementById('ssh-terminal-' + session.id);
    if (!container || container.offsetWidth === 0) return;

    // 1. 获取或缓存字符尺寸 (Consolas 14px 约 8.4x17)
    // 动态测量以适应不同系统缩放
    if (!this._charSize) {
      const measure = document.createElement('div');
      measure.style.fontFamily = 'Consolas, "Courier New", monospace';
      measure.style.fontSize = '14px';
      measure.style.lineHeight = '1.2';
      measure.style.position = 'absolute';
      measure.style.visibility = 'hidden';
      measure.style.whiteSpace = 'pre';
      measure.innerText = 'W'.repeat(10); // 测量10个字符取平均值更准
      document.body.appendChild(measure);
      this._charSize = {
        width: measure.offsetWidth / 10,
        height: measure.offsetHeight,
      };
      document.body.removeChild(measure);
    }

    // 2. 计算理想行列数 (预留内边距)
    const padding = 20; // 考虑 padding (10px * 2)
    const cols = Math.floor((container.offsetWidth - padding) / this._charSize.width);
    const rows = Math.floor((container.offsetHeight - 10) / this._charSize.height);

    // 3. 执行调整
    if (cols !== terminal.cols || rows !== terminal.rows) {
      terminal.resize(Math.max(20, cols), Math.max(5, rows));

      // 4. 同步到后端
      this.syncTerminalSize(session);
    }
  },

  syncTerminalSize(session) {
    if (!session || !session.terminal) return;
    if (session.ws && session.ws.readyState === WebSocket.OPEN) {
      if (session._resizeDebounce) clearTimeout(session._resizeDebounce);
      session._resizeDebounce = setTimeout(() => {
        if (session.ws && session.ws.readyState === WebSocket.OPEN) {
          session.ws.send(
            JSON.stringify({
              type: 'resize',
              cols: session.terminal.cols,
              rows: session.terminal.rows,
            })
          );
        }
      }, 400);
    }
  },

  fitAllVisibleSessions() {
    const ids =
      this.sshViewLayout === 'single'
        ? this.activeSSHSessionId
          ? [this.activeSSHSessionId]
          : []
        : this.visibleSessionIds;

    ids.forEach(id => {
      const session = this.getSessionById(id);
      if (session) this.manualTerminalResize(session);
    });
  },

  fitCurrentSSHSession() {
    const session = this.getSessionById(this.activeSSHSessionId);
    if (session) this.manualTerminalResize(session);
  },

  /**
   * 切换 SSH 终端全屏模式 (使用浏览器原生全屏 API)
   */
  async toggleSSHTerminalFullscreen() {
    const sshLayout = document.querySelector('.ssh-ide-layout');
    if (!sshLayout) return;

    try {
      if (!document.fullscreenElement) {
        if (sshLayout.requestFullscreen) {
          await sshLayout.requestFullscreen();
        } else if (sshLayout.webkitRequestFullscreen) {
          await sshLayout.webkitRequestFullscreen();
        } else if (sshLayout.msRequestFullscreen) {
          await sshLayout.msRequestFullscreen();
        }
      } else {
        if (document.exitFullscreen) {
          await document.exitFullscreen();
        } else if (document.webkitExitFullscreen) {
          await document.webkitExitFullscreen();
        } else if (document.msExitFullscreen) {
          await document.msExitFullscreen();
        }
      }
    } catch (err) {
      console.error('全屏操作失败:', err);
      // 容错处理：即使 API 失败也尝试切换样式类
      this.sshIdeFullscreen = !this.sshIdeFullscreen;
      setTimeout(() => this.fitCurrentSSHSession(), 300);
    }

    // 统一监听全屏状态变化，不仅处理本方法触发的，也处理 Esc 键退出的情况
    if (!window._sshFullscreenListenerBound) {
      const onFullscreenChange = () => {
        this.sshIdeFullscreen = !!document.fullscreenElement;
        // 连续触发多次 Fit，应对不同浏览器动画时长差异，彻底解决错位 bug
        const fitSequence = [50, 150, 300, 600, 1000];
        fitSequence.forEach(delay => {
          setTimeout(() => this.fitCurrentSSHSession(), delay);
        });
      };
      document.addEventListener('fullscreenchange', onFullscreenChange);
      document.addEventListener('webkitfullscreenchange', onFullscreenChange);
      window._sshFullscreenListenerBound = true;
    }
  },

  /**
   * 切换 SSH 窗口全屏模式 (使用浏览器 Fullscreen API)
   */
  async toggleSSHWindowFullscreen() {
    const sshLayout = document.querySelector('.ssh-ide-layout');
    if (!sshLayout) return;

    try {
      if (!document.fullscreenElement) {
        await sshLayout.requestFullscreen();
        this.sshWindowFullscreen = true;
      } else {
        await document.exitFullscreen();
        this.sshWindowFullscreen = false;
      }
    } catch (err) {
      console.error('窗口全屏切换失败:', err);
    }

    // 监听全屏变化事件
    document.addEventListener(
      'fullscreenchange',
      () => {
        this.sshWindowFullscreen = !!document.fullscreenElement;
        setTimeout(() => this.fitCurrentSSHSession(), 100);
        setTimeout(() => this.fitCurrentSSHSession(), 300);
        setTimeout(() => this.fitCurrentSSHSession(), 500);
      },
      { once: true }
    );
  },

  /**
   * 切换 SSH 屏幕全屏模式 (使用浏览器原生全屏 API)
   */
  async toggleSSHScreenFullscreen() {
    const sshLayout = document.querySelector('.ssh-ide-layout');
    if (!sshLayout) return;

    try {
      if (!document.fullscreenElement) {
        await sshLayout.requestFullscreen();
        this.sshIdeFullscreen = true;
      } else {
        await document.exitFullscreen();
        this.sshIdeFullscreen = false;
      }
    } catch (err) {
      console.error('全屏切换失败:', err);
    }

    // 监听全屏变化事件
    document.addEventListener(
      'fullscreenchange',
      () => {
        this.sshIdeFullscreen = !!document.fullscreenElement;
        setTimeout(() => this.fitCurrentSSHSession(), 100);
        setTimeout(() => this.fitCurrentSSHSession(), 300);
        setTimeout(() => this.fitCurrentSSHSession(), 500);
      },
      { once: true }
    );
  },

  /**
   * 更新所有终端的主题并强制重新渲染
   */
  updateAllTerminalThemes() {
    // 获取当前最新的主题配置
    const theme = this.getTerminalTheme();

    this.sshSessions.forEach(session => {
      if (session.terminal) {
        try {
          // 核心修复：显式创建新对象，触发 xterm.js 的 options 监听器
          session.terminal.options.theme = { ...theme };

          // 确保渲染器重绘
          if (session.terminal.buffer && session.terminal.buffer.active) {
            session.terminal.refresh(0, session.terminal.rows - 1);
          }
        } catch (err) {
          console.error('更新终端主题失败:', err);
        }
      }
    });
  },

  /**
   * 获取终端主题配置 - 支持深色/浅色模式自动切换
   */
  getTerminalTheme() {
    // 1. 获取 Body 上的实时计算样式
    const computedStyle = getComputedStyle(document.body);
    const bg = computedStyle.getPropertyValue('--bg-primary').trim();
    const fg = computedStyle.getPropertyValue('--text-primary').trim();

    // 2. 转换颜色为规范的 RGB 格式以便计算亮度
    const parseToRGB = colorStr => {
      if (!colorStr) return [255, 255, 255];
      if (colorStr.startsWith('rgb')) {
        return colorStr.match(/\d+/g).map(Number);
      }
      if (colorStr.startsWith('#')) {
        let hex = colorStr.substring(1);
        if (hex.length === 3)
          hex = hex
            .split('')
            .map(s => s + s)
            .join('');
        return [
          parseInt(hex.substring(0, 2), 16),
          parseInt(hex.substring(2, 4), 16),
          parseInt(hex.substring(4, 6), 16),
        ];
      }
      return [255, 255, 255];
    };

    const rgb = parseToRGB(bg);
    // 精确亮度计算 (W3C 标准)
    const brightness = (rgb[0] * 299 + rgb[1] * 587 + rgb[2] * 114) / 1000;
    const isDark = brightness < 128;

    if (isDark) {
      // 深色模式 - 高对比度调优
      return {
        background: bg || '#0d1117',
        foreground: '#ffffff',
        cursor: '#ffffff',
        selection: 'rgba(56, 139, 253, 0.5)',
        selectionBackground: 'rgba(56, 139, 253, 0.5)',
        black: '#000000',
        red: '#ff6b6b',
        green: '#4ade80',
        yellow: '#fbbf24',
        blue: '#60a5fa',
        magenta: '#e879f9',
        cyan: '#22d3ee',
        white: '#ffffff',
        brightBlack: '#94a3b8',
        brightRed: '#f87171',
        brightGreen: '#4ade80',
        brightYellow: '#fbbf24',
        brightBlue: '#60a5fa',
        brightMagenta: '#e879f9',
        brightCyan: '#22d3ee',
        brightWhite: '#ffffff',
      };
    } else {
      // 浅色模式 - 极致对比度 (针对白底黑字优化)
      return {
        background: bg || '#ffffff',
        foreground: '#000000',
        cursor: '#000000',
        selection: 'rgba(99, 102, 241, 0.3)',
        selectionBackground: 'rgba(99, 102, 241, 0.3)',
        black: '#000000',
        red: '#b91c1c',
        green: '#166534',
        yellow: '#92400e',
        blue: '#1e40af',
        magenta: '#701a75',
        cyan: '#155e75',
        white: '#1f2937',
        brightBlack: '#4b5563',
        brightRed: '#dc2626',
        brightGreen: '#15803d',
        brightYellow: '#b45309',
        brightBlue: '#2563eb',
        brightMagenta: '#9333ea',
        brightCyan: '#0891b2',
        brightWhite: '#6b7280',
      };
    }
  },

  /**
   * 设置主题观察器
   */
  setupThemeObserver() {
    // 1. 监听系统主题变化 (prefers-color-scheme)
    const darkModeQuery = window.matchMedia('(prefers-color-scheme: dark)');
    const handleThemeChange = () => {
      if (this.themeUpdateTimer) clearTimeout(this.themeUpdateTimer);
      this.themeUpdateTimer = setTimeout(() => {
        this.updateAllTerminalThemes();
      }, 150);
    };

    if (darkModeQuery.addEventListener) {
      darkModeQuery.addEventListener('change', handleThemeChange);
    } else if (darkModeQuery.addListener) {
      darkModeQuery.addListener(handleThemeChange);
    }

    // 2. 核心增强：监听 body 和 html 的属性变化 (类名、style 等)
    const attrObserver = new MutationObserver(handleThemeChange);
    attrObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class', 'style', 'data-theme'],
    });
    attrObserver.observe(document.body, { attributes: true, attributeFilter: ['class', 'style'] });

    // 3. 监听自定义 CSS 样式表的变化
    const observer = new MutationObserver(handleThemeChange);
    const customCssElement = document.getElementById('custom-css');
    if (customCssElement) {
      observer.observe(customCssElement, { childList: true, characterData: true, subtree: true });
    }

    // 4. 兜底方案：周期性校准主题 (每1秒检查一次)
    // 解决某些主题切换仅修改 CSS 变量而不触发 DOM 事件的问题
    let lastBg = '';
    this.themePollingInterval = setInterval(() => {
      const currentBg = getComputedStyle(document.body).getPropertyValue('--bg-primary').trim();
      if (currentBg && currentBg !== lastBg) {
        lastBg = currentBg;
        this.updateAllTerminalThemes();
        // 额外的 500ms 延迟刷新，确保 CSS 变量完全生效
        setTimeout(() => this.updateAllTerminalThemes(), 500);
      }
    }, 1000);

    // 保存观察器
    this.themeObserver = observer;
    this.attrObserver = attrObserver;
  },

  /**
   * 初始化会话终端 (WebSocket 版本)
   */
  async initSessionTerminal(sessionId) {
    const session = this.sshSessions.find(s => s.id === sessionId);
    if (!session) return;

    // 动态加载 xterm 库 (Code Splitting) & 确保 CSS 已加载
    // 只有在用户真正打开终端时才下载这些重型库
    let Terminal, FitAddon, WebLinksAddon;
    try {
      // 显式导入 CSS 以防 main.js 中的 lazy load 还没完成 (Vite 会自动去重)
      await import('@xterm/xterm/css/xterm.css');

      [{ Terminal }, { FitAddon }, { WebLinksAddon }] = await Promise.all([
        import('@xterm/xterm'),
        import('@xterm/addon-fit'),
        import('@xterm/addon-web-links'),
      ]);
    } catch (e) {
      console.error('Failed to load xterm:', e);
      toast.error('终端组件加载失败，请检查网络连接');
      return;
    }

    // 核心修复：如果全局仓库中不存在该节点的挂载点，则手动创建一个
    let terminalContainer = document.getElementById('ssh-terminal-' + sessionId);
    if (!terminalContainer) {
      const warehouse = document.getElementById('ssh-terminal-warehouse');
      if (!warehouse) {
        console.error('全局仓库 #ssh-terminal-warehouse 不存在！');
        return;
      }
      terminalContainer = document.createElement('div');
      terminalContainer.id = 'ssh-terminal-' + sessionId;
      warehouse.appendChild(terminalContainer);
    }

    // 清空容器
    terminalContainer.innerHTML = '';

    // 获取终端主题
    const theme = this.getTerminalTheme();

    // 创建 xterm 实例 - 不指定固定的 cols/rows，后续手动计算
    const terminal = new Terminal({
      cursorBlink: true,
      cursorStyle: 'bar',
      fontSize: 14,
      fontFamily: 'Consolas, "Courier New", monospace',
      lineHeight: 1.2,
      theme: theme,
      scrollback: 5000,
      allowProposedApi: true,
    });

    // 加载基本插件
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(new WebLinksAddon());

    // 打开终端到容器
    terminal.open(terminalContainer);

    // 实现右键复制/粘贴功能
    terminal.element.addEventListener('contextmenu', async e => {
      e.preventDefault();
      try {
        if (terminal.hasSelection()) {
          // 如果有选中内容，执行复制
          const selection = terminal.getSelection();
          await navigator.clipboard.writeText(selection);
          terminal.clearSelection();
          toast.success('已复制');
        } else {
          // 如果没有选中内容，执行粘贴
          const text = await navigator.clipboard.readText();
          if (text) {
            terminal.paste(text);
          }
        }
      } catch (err) {
        console.error('Clipboard action failed:', err);
        // 如果剪贴板 API 不可用 (非 HTTPS)，尝试降级提示
        if (err.name === 'NotAllowedError' || err.name === 'SecurityError') {
          toast.error('浏览器拒绝访问剪贴板，请允许权限');
        }
      }
    });

    // 保存到会话
    session.terminal = terminal;
    session.fit = fitAddon;

    // 立即执行一次手动适配
    this.$nextTick(() => {
      this.manualTerminalResize(session);
    });

    // 使用 ResizeObserver 监听容器大小变化
    const resizeObserver = new ResizeObserver(() => {
      // 使用 rAF 依然是好的实践，确保在浏览器布局完成后执行
      window.requestAnimationFrame(() => {
        this.manualTerminalResize(session);
      });
    });
    resizeObserver.observe(terminalContainer);
    session.resizeObserver = resizeObserver;

    // 显示连接中信息
    terminal.writeln(
      `\x1b[1;33m正在连接到 ${session.server.name} (${this.formatHost(session.server.host)})...\x1b[0m`
    );

    // 建立 WebSocket 连接 (统一支持 SSH 和 Agent PTY)
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws/ssh`);
    session.ws = ws;

    ws.onopen = () => {
      console.log(`[Terminal ${sessionId}] WebSocket 已连接 (${session.type})`);
      // 发送连接请求
      ws.send(
        JSON.stringify({
          type: 'connect',
          serverId: session.server.id,
          protocol: session.type === 'agent' ? 'agent' : 'ssh',
          cols: terminal.cols,
          rows: terminal.rows,
        })
      );

      // 启动心跳保活
      session.heartbeatInterval = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'ping' }));
        }
      }, 30000); // 每30秒发送一次心跳
    };

    ws.onmessage = event => {
      try {
        const msg = JSON.parse(event.data);

        switch (msg.type) {
          case 'connected':
            session.connected = true;
            // 连接成功后清屏，提供完全干净的界面
            terminal.clear();
            // 连接成功后再次 fit 确保终端填满容器
            setTimeout(() => this.safeTerminalFit(session), 100);
            break;

          case 'output':
            terminal.write(msg.data);
            break;

          case 'error':
            terminal.writeln(`\x1b[1;31m错误: ${msg.message}\x1b[0m`);
            break;

          case 'disconnected':
            session.connected = false;
            terminal.writeln('');
            terminal.writeln(`\x1b[1;33m${msg.message}\x1b[0m`);
            break;
        }
      } catch (e) {
        console.error('解析消息失败:', e);
      }
    };


    // 监听终端输入，发送到 WebSocket (包含多屏同步逻辑)
    terminal.onData(data => {
      // 1. 发送到当前会话
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(
          JSON.stringify({
            type: 'input',
            data: data,
          })
        );
      }

      // 2. 多屏同步：如果开启了同步且当前会话在可见分屏中，则广播输入
      if (
        this.sshSyncEnabled &&
        this.sshViewLayout !== 'single' &&
        this.visibleSessionIds.includes(sessionId)
      ) {
        this.visibleSessionIds.forEach(targetId => {
          if (targetId === sessionId) return; // 避免重复发送给原始会话

          const targetSession = this.getSessionById(targetId);
          if (targetSession && targetSession.ws && targetSession.ws.readyState === WebSocket.OPEN) {
            targetSession.ws.send(
              JSON.stringify({
                type: 'input',
                data: data,
              })
            );
          }
        });
      }
    });

    // 已使用 ResizeObserver 监听容器，此处无需 window.resize
  },



  /**
   * 为指定主机添加新会话（作为子标签页）
   */
  addSessionForServer(server) {
    this.showAddSessionSelectModal = false;

    // 检查是否已存在该主机的会话
    const existingSession = this.sshSessions.find(s => s.server.id === server.id);
    if (existingSession) {
      // 如果已存在，直接切换到该标签页
      this.switchToSSHTab(existingSession.id);
      return;
    }

    const sessionId = 'session_' + Date.now();
    let type = (server.monitor_mode === 'agent') ? 'agent' : 'ssh';

    const session = {
      id: sessionId,
      server: server,
      terminal: null,
      fit: null,
      ws: null,
      connected: false,
      type: type, // 'ssh' | 'agent'
      buffer: '',
      history: [],
      historyIndex: -1,
    };

    this.sshSessions.push(session);
    this.activeSSHSessionId = sessionId;

    // 切换到新的SSH标签页
    this.serverCurrentTab = 'terminal';

    this.$nextTick(() => {
      this.initSessionTerminal(sessionId);
      // 初始化后强制同步一次 DOM，将其从仓库移动到 Slot (如果它当前被激活)
      this.syncTerminalDOM();
    });
  },

  /**
   * 显示新建会话选择框
   */
  showAddSessionModal() {
    this.loadServerList();
    this.showAddSessionSelectModal = true;
  },

  /**
   * 全部打开主机列表中的所有 SSH 会话
   */
  async openAllServersInSSH() {
    if (this.serverList.length === 0) return;

    const count = this.serverList.length;
    this.showGlobalToast(`正在批量建立 ${count} 个连接...`, 'info');

    // 切换到终端标签页
    this.serverCurrentTab = 'terminal';
    this.showSSHQuickMenu = false;

    // 准备批量会话
    const newSessionIds = [];

    for (const server of this.serverList) {
      // 检查是否已经打开
      let session = this.sshSessions.find(s => s.server.id === server.id);
      if (!session) {
        const sessionId = 'session_' + Date.now() + '_' + Math.random().toString(36).substr(2, 5);
        let type = (server.monitor_mode === 'agent') ? 'agent' : 'ssh';

        session = {
          id: sessionId,
          server: server,
          terminal: null,
          fit: null,
          ws: null,
          connected: false,
          type: type,
          buffer: '',
          history: [],
          historyIndex: -1,
        };
        this.sshSessions.push(session);
      }
      newSessionIds.push(session.id);
    }

    // 设置布局模式：如果多于 1 个，使用 grid
    if (newSessionIds.length > 1) {
      this.sshViewLayout = 'grid';
      this.visibleSessionIds = [...newSessionIds];
    } else {
      this.sshViewLayout = 'single';
      this.activeSSHSessionId = newSessionIds[0];
    }

    // 初始化所有新终端
    this.$nextTick(() => {
      newSessionIds.forEach(id => {
        const session = this.getSessionById(id);
        if (session && !session.terminal) {
          this.initSessionTerminal(id);
        }
      });

      // 统一同步 DOM 并适配
      setTimeout(() => {
        this.syncTerminalDOM();
        this.fitAllVisibleSessions();
      }, 300);
    });
  },

  /**
   * 关闭所有 SSH 会话并返回列表
   */
  async closeAllSSHSessions() {
    if (this.sshSessions.length === 0) return;

    const confirmed = await this.showConfirm({
      title: '关闭所有会话',
      message: `确定要断开并关闭所有 ${this.sshSessions.length} 个 SSH 会话吗？`,
      icon: 'fa-power-off',
      confirmText: '全部关闭',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    // 循环关闭所有，不带参数调用 closeSSHTerminal 即可
    this.closeSSHTerminal();
    this.showGlobalToast('所有 SSH 会话已关闭', 'info');
  },

  /**
   * 关闭 SSH 终端（关闭所有会话）
   */
  closeSSHTerminal() {
    // 逆序遍历并逐个关闭，以确保数组删除过程安全
    for (let i = this.sshSessions.length - 1; i >= 0; i--) {
      this.closeSSHSession(this.sshSessions[i].id);
    }
    // 最终确认状态
    this.activeSSHSessionId = null;
    this.serverCurrentTab = 'list';
  },

  /**
   * 初始化 SSH 挂载观察器
   * 监视 DOM 变化以确保终端被正确挂载
   */
  initSshMountObserver() {
    if (this.sshMountObserver) return;

    const targetNode = document.getElementById('app');
    if (!targetNode) return;

    const observer = new MutationObserver(mutations => {
      let shouldSync = false;
      for (const mutation of mutations) {
        // 检查是否有 SSH 槽位相关的 DOM 变化
        if (mutation.type === 'childList') {
          const target = mutation.target;
          if (target.id && target.id.startsWith('ssh-slot-')) {
            shouldSync = true;
            break;
          }
          if (target.classList && target.classList.contains('ssh-terminal-wrapper')) {
            shouldSync = true;
            break;
          }
          // 检查新增节点
          if (mutation.addedNodes.length > 0) {
            for (let i = 0; i < mutation.addedNodes.length; i++) {
              const node = mutation.addedNodes[i];
              if (
                node.nodeType === 1 &&
                node.classList &&
                node.classList.contains('ssh-terminal-wrapper')
              ) {
                shouldSync = true;
                break;
              }
            }
          }
        }
      }

      if (shouldSync) {
        // 防抖同步
        if (this._mountSyncTimer) clearTimeout(this._mountSyncTimer);
        this._mountSyncTimer = setTimeout(() => {
          this.syncTerminalDOM();
        }, 50);
      }
    });

    observer.observe(targetNode, {
      childList: true,
      subtree: true,
      attributes: false,
    });

    this.sshMountObserver = observer;
    console.log('[System] SSH Mount Observer initialized');
  },

  // 展开分屏管理方法（来自 ssh-split.js）
  ...sshSplitMethods,
};
