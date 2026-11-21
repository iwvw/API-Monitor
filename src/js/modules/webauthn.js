const toBase64Url = (input) => {
  const bytes = input instanceof Uint8Array ? input : new Uint8Array(input);
  let binary = '';
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte);
  });
  return window.btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
};

const fromBase64Url = (value) => {
  const normalized = String(value || '').replace(/-/g, '+').replace(/_/g, '/');
  const padded = normalized + '='.repeat((4 - normalized.length % 4) % 4);
  const binary = window.atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
};

const mapCredentialDescriptor = (item) => ({
  ...item,
  id: fromBase64Url(item.id),
});

const normalizeRegistrationOptions = (payload) => ({
  ...payload,
  publicKey: {
    ...payload.publicKey,
    challenge: fromBase64Url(payload.publicKey.challenge),
    user: {
      ...payload.publicKey.user,
      id: fromBase64Url(payload.publicKey.user.id),
    },
    excludeCredentials: (payload.publicKey.excludeCredentials || []).map(mapCredentialDescriptor),
  },
});

const normalizeAuthenticationOptions = (payload) => ({
  ...payload,
  publicKey: {
    ...payload.publicKey,
    challenge: fromBase64Url(payload.publicKey.challenge),
    allowCredentials: (payload.publicKey.allowCredentials || []).map(mapCredentialDescriptor),
  },
});

const serializeCredential = (credential) => {
  if (!credential) return null;
  const response = credential.response || {};
  return {
    id: credential.id,
    rawId: toBase64Url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment || undefined,
    clientExtensionResults: credential.getClientExtensionResults?.() || {},
    response: {
      clientDataJSON: response.clientDataJSON ? toBase64Url(response.clientDataJSON) : undefined,
      attestationObject: response.attestationObject ? toBase64Url(response.attestationObject) : undefined,
      authenticatorData: response.authenticatorData ? toBase64Url(response.authenticatorData) : undefined,
      signature: response.signature ? toBase64Url(response.signature) : undefined,
      userHandle: response.userHandle ? toBase64Url(response.userHandle) : undefined,
      transports: typeof response.getTransports === 'function' ? response.getTransports() : undefined,
    },
  };
};

export const browserSupportsWebAuthn = () => Boolean(
  typeof window !== 'undefined'
  && window.isSecureContext
  && typeof window.PublicKeyCredential !== 'undefined'
  && typeof navigator !== 'undefined'
  && navigator.credentials
);

export const createPasskeyCredential = async (options) => {
  if (!browserSupportsWebAuthn()) {
    throw new Error('浏览器不支持通行密钥');
  }
  const credential = await navigator.credentials.create(normalizeRegistrationOptions(options));
  return serializeCredential(credential);
};

export const getPasskeyAssertion = async (options) => {
  if (!browserSupportsWebAuthn()) {
    throw new Error('浏览器不支持通行密钥');
  }
  const credential = await navigator.credentials.get(normalizeAuthenticationOptions(options));
  return serializeCredential(credential);
};
