import { describe, expect, it } from 'vitest';
import { getOSIconClass, getServerPlatformLabel } from './osPlatform.js';

describe('OS platform presentation', () => {
  it('uses distribution-specific icons', () => {
    expect(getOSIconClass('Ubuntu 24.04')).toContain('si-ubuntu');
    expect(getOSIconClass('Debian GNU/Linux')).toContain('si-debian');
    expect(getOSIconClass('Windows 11')).toContain('fa-windows');
  });

  it('combines platform and version fields from server payloads', () => {
    expect(getServerPlatformLabel({ platform: 'Linux', platform_version: 'Ubuntu 24.04' })).toBe('Linux Ubuntu 24.04');
  });
});

describe('getOSIconClass', () => {
  it('covers remaining distributions and aliases', () => {
    expect(getOSIconClass('CentOS Stream 9')).toContain('si-centos');
    expect(getOSIconClass('Alpine 3.20')).toContain('si-alpinelinux');
    expect(getOSIconClass('RedHat 8')).toContain('si-redhat');
    expect(getOSIconClass('RHEL 9.4')).toContain('si-redhat');
    expect(getOSIconClass('Fedora 40')).toContain('si-fedora');
    expect(getOSIconClass('Rocky Linux 9')).toContain('si-rockylinux');
    expect(getOSIconClass('AlmaLinux 9')).toContain('si-almalinux');
    expect(getOSIconClass('Arch Linux')).toContain('si-archlinux');
    expect(getOSIconClass('Darwin 24')).toContain('si-apple');
    expect(getOSIconClass('macOS 15')).toContain('si-apple');
  });

  it('is case-insensitive', () => {
    expect(getOSIconClass('debian')).toContain('si-debian');
    expect(getOSIconClass('UBUNTU')).toContain('si-ubuntu');
    expect(getOSIconClass('redhat')).toContain('si-redhat');
    expect(getOSIconClass('rhel')).toContain('si-redhat');
    expect(getOSIconClass('MACOS')).toContain('si-apple');
  });

  it('applies the offline state class', () => {
    expect(getOSIconClass('Ubuntu', { offline: true })).toBe(
      'si si-ubuntu si--color shrink-0 text-base leading-none text-kumo-subtle opacity-60 grayscale',
    );
    expect(getOSIconClass('Ubuntu', { offline: false })).toBe('si si-ubuntu si--color shrink-0 text-base leading-none');
    expect(getOSIconClass('', { offline: true })).toBe(
      'fas fa-server text-kumo-subtle shrink-0 text-base leading-none text-kumo-subtle opacity-60 grayscale',
    );
  });

  it('falls back to the server icon for empty platforms', () => {
    const fallback = 'fas fa-server text-kumo-subtle shrink-0 text-base leading-none';
    expect(getOSIconClass('')).toBe(fallback);
    expect(getOSIconClass(undefined)).toBe(fallback);
    expect(getOSIconClass(null)).toBe(fallback);
  });

  it('falls back to the linux icon for unknown non-empty platforms', () => {
    expect(getOSIconClass('FreeBSD 14')).toContain('si-linux');
    expect(getOSIconClass('suse')).toContain('si-linux');
    expect(getOSIconClass('Red Hat Enterprise Linux')).toContain('si-linux');
    expect(getOSIconClass(42)).toContain('si-linux');
  });
});

describe('getServerPlatformLabel', () => {
  it('filters falsy platform and version fields', () => {
    expect(getServerPlatformLabel({ platform: 'Linux', platform_version: '' })).toBe('Linux');
    expect(getServerPlatformLabel({ platform: '', platform_version: 'Ubuntu 24.04' })).toBe('Ubuntu 24.04');
    expect(getServerPlatformLabel({ platform: null, platform_version: 'v1' })).toBe('v1');
    expect(getServerPlatformLabel({ platform: 'Debian' })).toBe('Debian');
  });

  it('supports the camelCase version field', () => {
    expect(getServerPlatformLabel({ platform: 'Linux', platformVersion: '6.8' })).toBe('Linux 6.8');
  });

  it('returns an empty string when nothing is present', () => {
    expect(getServerPlatformLabel({})).toBe('');
    expect(getServerPlatformLabel(null)).toBe('');
    expect(getServerPlatformLabel(undefined)).toBe('');
  });
});
