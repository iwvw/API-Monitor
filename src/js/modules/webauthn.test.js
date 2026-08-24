import { describe, expect, it, vi, afterEach } from 'vitest';

import {
  browserSupportsWebAuthn,
  createPasskeyCredential,
  getPasskeyAssertion,
} from './webauthn.js';

const b64url = (bytes) => Buffer.from(bytes).toString('base64url');

const stubWebAuthn = (overrides = {}) => {
  const createMock = vi.fn();
  const getMock = vi.fn();
  vi.stubGlobal('window', {
    btoa: globalThis.btoa,
    atob: globalThis.atob,
    PublicKeyCredential: class PublicKeyCredential {},
    ...overrides.window,
  });
  vi.stubGlobal('navigator', {
    credentials: { create: createMock, get: getMock, ...(overrides.credentials || {}) },
  });
  return { createMock, getMock };
};

describe('browserSupportsWebAuthn', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('returns false without a window', () => {
    expect(browserSupportsWebAuthn()).toBe(false);
  });

  it('returns false without PublicKeyCredential', () => {
    vi.stubGlobal('window', {});
    expect(browserSupportsWebAuthn()).toBe(false);
  });

  it('returns falsy without navigator.credentials', () => {
    vi.stubGlobal('window', { PublicKeyCredential: class {} });
    vi.stubGlobal('navigator', {});
    expect(browserSupportsWebAuthn()).toBeFalsy();
  });

  it('returns truthy when everything is present', () => {
    vi.stubGlobal('window', { PublicKeyCredential: class {} });
    vi.stubGlobal('navigator', { credentials: {} });
    expect(browserSupportsWebAuthn()).toBeTruthy();
    expect(browserSupportsWebAuthn()).toBe(navigator.credentials);
  });
});

describe('createPasskeyCredential', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('throws when the browser is unsupported', async () => {
    await expect(createPasskeyCredential({})).rejects.toThrow('浏览器不支持通行密钥');
  });

  it('normalizes options and serializes the credential', async () => {
    const { createMock } = stubWebAuthn();
    createMock.mockResolvedValue({
      id: 'cred-id',
      type: 'public-key',
      rawId: new Uint8Array([0, 62, 255, 10]),
      authenticatorAttachment: 'platform',
      getClientExtensionResults: () => ({ credProps: { rk: true } }),
      response: {
        clientDataJSON: new Uint8Array([1, 2, 3]),
        attestationObject: new Uint8Array([4, 5]),
        getTransports: () => ['internal'],
      },
    });

    const result = await createPasskeyCredential({
      publicKey: {
        challenge: 'AQID',
        rp: { name: 'demo' },
        user: { id: 'BAU', name: 'alice' },
        excludeCredentials: [{ id: 'AQI', type: 'public-key' }],
      },
    });

    expect(createMock).toHaveBeenCalledTimes(1);
    const normalized = createMock.mock.calls[0][0];
    expect(normalized.publicKey.challenge).toBeInstanceOf(ArrayBuffer);
    expect(Array.from(new Uint8Array(normalized.publicKey.challenge))).toEqual([1, 2, 3]);
    expect(normalized.publicKey.user.id).toBeInstanceOf(ArrayBuffer);
    expect(Array.from(new Uint8Array(normalized.publicKey.user.id))).toEqual([4, 5]);
    expect(normalized.publicKey.rp).toEqual({ name: 'demo' });
    expect(normalized.publicKey.user.name).toBe('alice');
    expect(normalized.publicKey.excludeCredentials[0].id).toBeInstanceOf(ArrayBuffer);
    expect(Array.from(new Uint8Array(normalized.publicKey.excludeCredentials[0].id))).toEqual([1, 2]);
    expect(normalized.publicKey.excludeCredentials[0].type).toBe('public-key');

    expect(result).toEqual({
      id: 'cred-id',
      rawId: b64url([0, 62, 255, 10]),
      type: 'public-key',
      authenticatorAttachment: 'platform',
      clientExtensionResults: { credProps: { rk: true } },
      response: {
        clientDataJSON: b64url([1, 2, 3]),
        attestationObject: b64url([4, 5]),
        authenticatorData: undefined,
        signature: undefined,
        userHandle: undefined,
        transports: ['internal'],
      },
    });
    expect(result.rawId).not.toContain('=');
    expect(result.rawId).not.toContain('+');
    expect(result.rawId).not.toContain('/');
  });

  it('returns null when the browser returns nothing', async () => {
    const { createMock } = stubWebAuthn();
    createMock.mockResolvedValue(null);
    expect(await createPasskeyCredential({
      publicKey: { challenge: 'AQID', user: { id: 'BAU' } },
    })).toBeNull();
  });
});

describe('getPasskeyAssertion', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('throws when the browser is unsupported', async () => {
    await expect(getPasskeyAssertion({})).rejects.toThrow('浏览器不支持通行密钥');
  });

  it('normalizes allowCredentials and serializes the assertion', async () => {
    const { getMock } = stubWebAuthn();
    getMock.mockResolvedValue({
      id: 'assert-id',
      type: 'public-key',
      rawId: new Uint8Array([9]),
      getClientExtensionResults: () => ({}),
      response: {
        clientDataJSON: new Uint8Array([1, 2]),
        authenticatorData: new Uint8Array([8]),
        signature: new Uint8Array([9]),
        userHandle: new Uint8Array([7]),
        getTransports: () => ['usb'],
      },
    });

    const result = await getPasskeyAssertion({
      publicKey: {
        challenge: 'AQID',
        allowCredentials: [{ id: 'AQI', type: 'public-key' }],
      },
    });

    expect(getMock).toHaveBeenCalledTimes(1);
    const normalized = getMock.mock.calls[0][0];
    expect(normalized.publicKey.challenge).toBeInstanceOf(ArrayBuffer);
    expect(Array.from(new Uint8Array(normalized.publicKey.challenge))).toEqual([1, 2, 3]);
    expect(normalized.publicKey.allowCredentials).toHaveLength(1);
    expect(normalized.publicKey.allowCredentials[0].id).toBeInstanceOf(ArrayBuffer);
    expect(Array.from(new Uint8Array(normalized.publicKey.allowCredentials[0].id))).toEqual([1, 2]);

    expect(result).toEqual({
      id: 'assert-id',
      rawId: b64url([9]),
      type: 'public-key',
      authenticatorAttachment: undefined,
      clientExtensionResults: {},
      response: {
        clientDataJSON: b64url([1, 2]),
        authenticatorData: b64url([8]),
        signature: b64url([9]),
        userHandle: b64url([7]),
        transports: ['usb'],
      },
    });
  });
});