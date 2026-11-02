import { describe, expect, it } from 'vitest';
import {
  canOpenTerminal,
  getTerminalTransports,
  isTerminalSocketDead,
  resolveTerminalProtocol,
  resolveTerminalSocketTransport,
  shouldDropPtyAttach,
  terminalReconnectDelay,
  TERMINAL_HEARTBEAT_MS,
} from './serverTerminal.js';

describe('server terminal transport resolution', () => {
  it('prefers the agent tunnel when the agent is online and ssh is also configured', () => {
    const server = {
      id: 'server-1',
      host: '203.0.113.10',
      port: 22,
      username: 'root',
      ssh_configured: true,
      agent_online: true,
    };

    expect(getTerminalTransports(server)).toEqual(['agent', 'ssh']);
    expect(resolveTerminalProtocol(server)).toBe('agent');
    expect(canOpenTerminal(server)).toBe(true);
  });

  it('falls back to ssh when the agent is offline', () => {
    const server = {
      id: 'server-1',
      host: '203.0.113.10',
      port: 22,
      username: 'root',
      ssh_configured: true,
      agent_online: false,
    };

    expect(getTerminalTransports(server)).toEqual(['ssh']);
    expect(resolveTerminalProtocol(server)).toBe('ssh');
		expect(resolveTerminalSocketTransport(server, 'agent')).toBe('ssh');
  });

	it('lets the backend use live state when a stale page snapshot offers agent and ssh', () => {
		const server = {
			id: 'server-1',
			host: '203.0.113.10',
			port: 22,
			username: 'root',
			ssh_configured: true,
			agent_online: true,
		};

		expect(resolveTerminalSocketTransport(server, 'agent')).toBe('auto');
	});

  it('opens an agent-only host through the agent tunnel', () => {
    const server = {
      id: 'server-1',
      host: '0.0.0.0',
      port: 22,
      username: 'agent',
      agent_online: true,
    };

    expect(getTerminalTransports(server)).toEqual(['agent']);
    expect(resolveTerminalProtocol(server)).toBe('agent');
    expect(canOpenTerminal(server)).toBe(true);
  });
});

describe('terminal keepalive and reconnect', () => {
  it('uses exponential backoff capped at 30s', () => {
    expect(terminalReconnectDelay(0)).toBe(1000);
    expect(terminalReconnectDelay(1)).toBe(2000);
    expect(terminalReconnectDelay(2)).toBe(4000);
    expect(terminalReconnectDelay(3)).toBe(8000);
    expect(terminalReconnectDelay(4)).toBe(16000);
    expect(terminalReconnectDelay(5)).toBe(30000);
    // 超过上限后不再增长
    expect(terminalReconnectDelay(9)).toBe(30000);
    expect(terminalReconnectDelay(-1)).toBe(1000);
    expect(terminalReconnectDelay(undefined)).toBe(1000);
  });

  it('keeps heartbeat well below backend pong wait and proxy idle timeout', () => {
    // 后端 terminalPongWait=120s，常见代理空闲超时 60s；
    // 心跳必须留出 2 个以上周期余量。
    expect(TERMINAL_HEARTBEAT_MS).toBeLessThan(60000 / 2);
    expect(TERMINAL_HEARTBEAT_MS * 2).toBeLessThan(120000);
  });

  it('treats closed and closing sockets as dead but not connecting ones', () => {
    expect(isTerminalSocketDead(null)).toBe(true);
    expect(isTerminalSocketDead({ readyState: 3 })).toBe(true);
    expect(isTerminalSocketDead({ readyState: 2 })).toBe(true);
    expect(isTerminalSocketDead({ readyState: 1 })).toBe(false);
    expect(isTerminalSocketDead({ readyState: 0 })).toBe(false);
  });

  it('drops pty attach only after the first failed reconnect', () => {
    expect(shouldDropPtyAttach(0, 'pty-1')).toBe(false);
    expect(shouldDropPtyAttach(1, 'pty-1')).toBe(true);
    // 没有 pty 时无所谓
    expect(shouldDropPtyAttach(2, '')).toBe(false);
  });
});
