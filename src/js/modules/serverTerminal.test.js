import { describe, expect, it } from 'vitest';
import {
  canOpenTerminal,
  getTerminalTransports,
  resolveTerminalProtocol,
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
