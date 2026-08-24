import { describe, expect, it, vi } from 'vitest';
import { fileboxDownloadEndpoint, fileboxShareURL, fileboxDirectURL } from './fileboxLinks.js';

describe('fileboxDownloadEndpoint', () => {
  it('builds the download endpoint with an encoded code', () => {
    expect(fileboxDownloadEndpoint('abc123')).toBe('/api/filebox/public/abc123/download');
    expect(fileboxDownloadEndpoint('a b/c?d')).toBe('/api/filebox/public/a%20b%2Fc%3Fd/download');
    expect(fileboxDownloadEndpoint('')).toBe('/api/filebox/public//download');
  });
});

describe('fileboxShareURL and fileboxDirectURL', () => {
  it('build URLs against window.location.origin', () => {
    vi.stubGlobal('window', { location: { origin: 'https://example.com' } });
    expect(fileboxShareURL('abc123')).toBe('https://example.com/share/abc123');
    expect(fileboxDirectURL('abc123')).toBe('https://example.com/api/filebox/d/abc123');
    expect(fileboxShareURL('a b')).toBe('https://example.com/share/a%20b');
    expect(fileboxDirectURL('a/b?x=1')).toBe('https://example.com/api/filebox/d/a%2Fb%3Fx%3D1');
    vi.unstubAllGlobals();
  });

  it('reflects a different origin', () => {
    vi.stubGlobal('window', { location: { origin: 'http://127.0.0.1:3000' } });
    expect(fileboxShareURL('x')).toBe('http://127.0.0.1:3000/share/x');
    expect(fileboxDirectURL('x')).toBe('http://127.0.0.1:3000/api/filebox/d/x');
    vi.unstubAllGlobals();
  });
});