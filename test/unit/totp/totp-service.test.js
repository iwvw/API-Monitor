import { describe, expect, it } from 'vitest';

const totpService = await import('../../../modules/totp-api/totp-service.js');

describe('totp service', () => {
  it('normalizes legacy uppercase OTP algorithm names', () => {
    expect(totpService.normalizeOtpAlgorithm('SHA1')).toBe('sha1');
    expect(totpService.normalizeOtpAlgorithm('SHA-256')).toBe('sha256');
    expect(totpService.normalizeOtpAlgorithm('sha512')).toBe('sha512');
  });

  it('generates codes for accounts stored with uppercase algorithms', () => {
    const codes = totpService.generateAllCodes([
      {
        id: 'legacy-uppercase',
        otp_type: 'totp',
        secret: 'JBSWY3DPEHPK3PXP',
        algorithm: 'SHA1',
        digits: 6,
        period: 30,
      },
    ]);

    expect(codes['legacy-uppercase'].code).toMatch(/^\d{6}$/);
    expect(codes['legacy-uppercase'].remaining).toBeGreaterThan(0);
  });
});
