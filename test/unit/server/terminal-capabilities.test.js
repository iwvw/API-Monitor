import { describe, expect, it } from 'vitest';

import capabilities from '../../../modules/server-api/capabilities.js';
import {
  canOpenTerminal,
  getTerminalTransports,
  isAgentServer,
  resolveTerminalProtocol,
} from '../../../src/js/modules/serverTerminal.js';

const { getServerCapabilities, hasSshConfig } = capabilities;

describe('server terminal capabilities', () => {
  it('keeps SSH terminal available when the Agent is offline', () => {
    const server = {
      host: '203.0.113.10',
      port: 22,
      username: 'root',
      auth_type: 'password',
      password: 'secret',
    };

    const result = getServerCapabilities(server, { agentOnline: false });

    expect(result.agent_online).toBe(false);
    expect(result.ssh_configured).toBe(true);
    expect(result.terminal_transports).toEqual(['ssh']);
    expect(result.preferred_terminal_transport).toBe('ssh');
  });

  it('prefers Agent terminal while preserving SSH fallback when both are available', () => {
    const result = getServerCapabilities(
      {
        host: '203.0.113.11',
        port: 22,
        username: 'root',
        auth_type: 'key',
        private_key: '-----BEGIN OPENSSH PRIVATE KEY-----',
      },
      { agentOnline: true },
    );

    expect(result.terminal_transports).toEqual(['agent', 'ssh']);
    expect(result.preferred_terminal_transport).toBe('agent');
  });

  it('does not treat Agent placeholder hosts as SSH endpoints', () => {
    expect(hasSshConfig({
      host: '0.0.0.0',
      username: 'agent',
      auth_type: 'password',
      password: 'secret',
    })).toBe(false);
  });
});

describe('frontend terminal selection', () => {
  it('uses backend capability fields instead of host status', () => {
    const server = {
      status: 'offline',
      monitor_mode: 'agent',
      terminal_transports: ['ssh'],
      preferred_terminal_transport: 'ssh',
      ssh_configured: true,
    };

    expect(canOpenTerminal(server)).toBe(true);
    expect(resolveTerminalProtocol(server)).toBe('ssh');
  });

  it('falls back to SSH endpoint detection for older account payloads', () => {
    const server = {
      status: 'offline',
      monitor_mode: 'agent',
      host: '203.0.113.12',
      port: 22,
      username: 'root',
    };

    expect(getTerminalTransports(server)).toEqual(['ssh']);
    expect(resolveTerminalProtocol(server)).toBe('ssh');
  });

  it('only marks placeholder or tagged hosts as Agent servers on legacy payloads', () => {
    expect(isAgentServer({ host: '0.0.0.0' })).toBe(true);
    expect(isAgentServer({ host: '203.0.113.13', monitor_mode: 'agent' })).toBe(false);
  });
});
