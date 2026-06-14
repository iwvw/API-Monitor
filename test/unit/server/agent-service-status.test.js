import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import agentService from '../../../modules/server-api/agent-service.js';
import protocol from '../../../modules/server-api/protocol.js';

const { Events } = protocol;

const clearAgentRuntimeState = () => {
  agentService.connections.clear();
  agentService.hostInfoCache.clear();
  agentService.stateCache.clear();
  agentService.legacyMetrics.clear();
  agentService.legacyStatus.clear();
  agentService.heartbeatTimers.forEach(timer => clearTimeout(timer));
  agentService.heartbeatTimers.clear();
};

describe('agent service status synchronization', () => {
  beforeEach(() => {
    clearAgentRuntimeState();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    clearAgentRuntimeState();
  });

  it('sends a full online/offline status snapshot to frontend clients', () => {
    const servers = [
      { id: 'online-host' },
      { id: 'offline-host' },
      { id: 'ssh-host', status: 'online' },
    ];

    agentService.connections.set('online-host', { _connectedAt: 12345 });
    agentService.legacyStatus.set('offline-host', { connected: false, lastSeen: 67890 });

    const snapshot = agentService.buildServerStatusSnapshot(servers);

    expect(snapshot).toEqual(expect.arrayContaining([
      expect.objectContaining({
        serverId: 'online-host',
        status: 'online',
        agent_online: true,
        connectedAt: 12345,
      }),
      expect.objectContaining({
        serverId: 'offline-host',
        status: 'offline',
        agent_online: false,
        lastSeen: 67890,
      }),
      expect.objectContaining({
        serverId: 'ssh-host',
        status: 'online',
        agent_online: false,
      }),
    ]));

    vi.spyOn(agentService, 'getServerStatusSnapshot').mockReturnValue(snapshot);

    const socket = {
      id: 'frontend-1',
      join: vi.fn(),
      emit: vi.fn(),
      on: vi.fn(),
    };

    agentService.handleFrontendConnection(socket);

    expect(socket.emit).toHaveBeenCalledWith(
      Events.SERVER_LIST,
      snapshot,
    );
  });

  it('clears realtime metric caches when an agent is marked offline', () => {
    agentService.stateCache.set('server-1', {
      state: { cpu: 12 },
      timestamp: 1000,
      receivedAt: 2000,
    });
    agentService.legacyMetrics.set('server-1', { cpu_usage: '12%' });
    agentService.legacyStatus.set('server-1', {
      connected: true,
      lastSeen: 3000,
      version: 'socket.io',
    });

    agentService.markServerOffline('server-1');

    expect(agentService.stateCache.has('server-1')).toBe(false);
    expect(agentService.legacyMetrics.has('server-1')).toBe(false);
    expect(agentService.legacyStatus.get('server-1')).toMatchObject({
      connected: false,
      lastSeen: 3000,
      version: 'socket.io',
    });
  });
});
