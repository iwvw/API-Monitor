import { describe, expect, it } from 'vitest';
import {
  normalizeAgentInstallOs,
  isWindowsAgentInstallOs,
  buildAgentInstallEndpoint,
  buildAgentInstallCommand,
  getAgentInstallExecutionHint,
} from './agentInstall.js';

describe('normalizeAgentInstallOs', () => {
  it('normalizes windows aliases to win', () => {
    expect(normalizeAgentInstallOs('win')).toBe('win');
    expect(normalizeAgentInstallOs('windows')).toBe('win');
    expect(normalizeAgentInstallOs('powershell')).toBe('win');
    expect(normalizeAgentInstallOs(' Windows ')).toBe('win');
    expect(normalizeAgentInstallOs('PowerShell')).toBe('win');
    expect(normalizeAgentInstallOs('POWER-SHELL')).toBe('linux');
  });

  it('defaults everything else to linux', () => {
    expect(normalizeAgentInstallOs('linux')).toBe('linux');
    expect(normalizeAgentInstallOs('Linux')).toBe('linux');
    expect(normalizeAgentInstallOs('macos')).toBe('linux');
    expect(normalizeAgentInstallOs('')).toBe('linux');
    expect(normalizeAgentInstallOs()).toBe('linux');
    expect(normalizeAgentInstallOs(null)).toBe('linux');
  });
});

describe('isWindowsAgentInstallOs', () => {
  it('returns true for windows aliases', () => {
    expect(isWindowsAgentInstallOs('win')).toBe(true);
    expect(isWindowsAgentInstallOs('POWERSHELL')).toBe(true);
  });

  it('returns false otherwise', () => {
    expect(isWindowsAgentInstallOs('linux')).toBe(false);
    expect(isWindowsAgentInstallOs('')).toBe(false);
    expect(isWindowsAgentInstallOs()).toBe(false);
  });
});

describe('buildAgentInstallEndpoint', () => {
  it('returns an empty string when required fields are missing', () => {
    expect(buildAgentInstallEndpoint()).toBe('');
    expect(buildAgentInstallEndpoint({ baseUrl: '', serverId: 's', agentKey: 'k' })).toBe('');
    expect(buildAgentInstallEndpoint({ baseUrl: 'https://x.com', serverId: '', agentKey: 'k' })).toBe('');
    expect(buildAgentInstallEndpoint({ baseUrl: 'https://x.com', serverId: 's', agentKey: '' })).toBe('');
    expect(buildAgentInstallEndpoint({ baseUrl: '   ', serverId: 's', agentKey: 'k' })).toBe('');
  });

  it('builds the install URL with a normalized base', () => {
    const serverId = 'srv-1';
    const agentKey = 'abc123';
    expect(buildAgentInstallEndpoint({ baseUrl: 'https://example.com', serverId, agentKey })).toBe(
      `https://example.com/api/server/agent/install/linux/${serverId}/${agentKey}?protocol=https`,
    );
    expect(buildAgentInstallEndpoint({ baseUrl: 'https://example.com/', serverId, agentKey })).toBe(
      `https://example.com/api/server/agent/install/linux/${serverId}/${agentKey}?protocol=https`,
    );
    expect(buildAgentInstallEndpoint({ baseUrl: 'https://example.com///', serverId, agentKey })).toBe(
      `https://example.com/api/server/agent/install/linux/${serverId}/${agentKey}?protocol=https`,
    );
    expect(buildAgentInstallEndpoint({ baseUrl: '  https://example.com/  ', serverId, agentKey })).toBe(
      `https://example.com/api/server/agent/install/linux/${serverId}/${agentKey}?protocol=https`,
    );
  });

  it('normalizes the os type inside the path', () => {
    const serverId = 's';
    const agentKey = 'k';
    expect(buildAgentInstallEndpoint({ baseUrl: 'https://example.com', serverId, agentKey, osType: 'POWERSHELL' })).toBe(
      `https://example.com/api/server/agent/install/win/${serverId}/${agentKey}?protocol=https`,
    );
  });

  it('encodes the protocol query value', () => {
    const serverId = 's';
    const agentKey = 'k';
    expect(
      buildAgentInstallEndpoint({
        baseUrl: 'https://example.com',
        serverId,
        agentKey,
        protocol: 'https?p=1&x=2',
      }),
    ).toBe(`https://example.com/api/server/agent/install/linux/${serverId}/${agentKey}?protocol=https%3Fp%3D1%26x%3D2`);
  });

  it('applies a custom protocol when provided', () => {
    const serverId = 's';
    const agentKey = 'k';
    expect(
      buildAgentInstallEndpoint({ baseUrl: 'http://10.0.0.1:3000', serverId, agentKey, protocol: 'http' }),
    ).toBe(`http://10.0.0.1:3000/api/server/agent/install/linux/${serverId}/${agentKey}?protocol=http`);
  });
});

describe('buildAgentInstallCommand', () => {
  it('returns an empty string without a usable endpoint', () => {
    expect(buildAgentInstallCommand()).toBe('');
    expect(buildAgentInstallCommand({})).toBe('');
  });

  it('builds the PowerShell command for windows', () => {
    const options = { baseUrl: 'https://example.com', serverId: 's', agentKey: 'k', osType: 'windows' };
    expect(buildAgentInstallCommand(options)).toBe(
      `powershell -c "irm https://example.com/api/server/agent/install/win/${options.serverId}/${options.agentKey}?protocol=https | iex"`,
    );
  });

  it('builds the curl command otherwise', () => {
    const options = { baseUrl: 'https://example.com', serverId: 's', agentKey: 'k' };
    expect(buildAgentInstallCommand(options)).toBe(
      `curl -fsSL https://example.com/api/server/agent/install/linux/${options.serverId}/${options.agentKey}?protocol=https | bash`,
    );
  });
});

describe('getAgentInstallExecutionHint', () => {
  it('suggests PowerShell for windows', () => {
    expect(getAgentInstallExecutionHint('powershell')).toBe('请在 Windows PowerShell 中执行（勿用 CMD/bash/zsh）。');
  });

  it('suggests a unix terminal otherwise', () => {
    expect(getAgentInstallExecutionHint()).toBe('请在 Linux/macOS 终端中执行（勿用 PowerShell）。');
    expect(getAgentInstallExecutionHint('linux')).toBe('请在 Linux/macOS 终端中执行（勿用 PowerShell）。');
  });
});