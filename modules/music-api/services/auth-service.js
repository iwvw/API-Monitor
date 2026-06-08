const dbService = require('../../../src/db/database');
const { createLogger } = require('../../../src/utils/logger');
const { secureEncrypt, secureDecrypt } = require('../../../src/utils/secure-storage');

const logger = createLogger('MusicAuthService');
const HTTP_COOKIE_ATTRIBUTES = new Set(['max-age', 'expires', 'path', 'domain', 'secure', 'httponly', 'samesite']);
const LOGIN_COOKIE_KEYS = new Set(['MUSIC_U', 'MUSIC_R_U', 'MUSIC_A', 'MUSIC_A_T', '__csrf']);

function parseCookieParts(value) {
  const source = Array.isArray(value) ? value : String(value || '').split(';');
  return source
    .map(part => String(part).match(/^([^;]+)/)?.[1]?.trim() || '')
    .filter(Boolean)
    .filter(part => {
      const key = part.split('=')[0]?.trim().toLowerCase();
      return key && !HTTP_COOKIE_ATTRIBUTES.has(key);
    });
}

function toCookieMap(cookieString) {
  return parseCookieParts(cookieString).reduce((acc, part) => {
    const [key, ...valueParts] = part.split('=');
    if (key) acc[key.trim()] = valueParts.join('=');
    return acc;
  }, {});
}

class MusicAuthService {
  constructor() {
    this.storedCookie = '';
  }

  loadStoredCookie() {
    try {
      const db = dbService.getDatabase();
      const row = db.prepare('SELECT value FROM music_settings WHERE key = ?').get('cookie');
      if (row?.value) {
        this.storedCookie = secureDecrypt(row.value);
        logger.info('Loaded stored cookie from database, length:', this.storedCookie.length);
      } else {
        this.storedCookie = '';
        logger.info('No cookie found in database');
      }
    } catch (error) {
      logger.error('Failed to load cookie from database:', error.message);
    }
    return this.storedCookie;
  }

  getStoredCookie() {
    return this.storedCookie;
  }

  saveCookie(cookieString) {
    try {
      const db = dbService.getDatabase();
      const encryptedCookie = secureEncrypt(cookieString);
      db.prepare(
        `INSERT INTO music_settings (key, value, updated_at)
         VALUES ('cookie', ?, CURRENT_TIMESTAMP)
         ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`
      ).run(encryptedCookie);
      this.storedCookie = cookieString;
      logger.success('Cookie saved to database (encrypted)');
    } catch (error) {
      logger.error('Failed to save cookie to database:', error.message);
    }
  }

  clearCookie() {
    this.storedCookie = '';
    try {
      const db = dbService.getDatabase();
      db.prepare('DELETE FROM music_settings WHERE key = ?').run('cookie');
      logger.info('Cookie cleared from database');
    } catch (error) {
      logger.warn('Failed to clear cookie from database:', error.message);
    }
  }

  getEffectiveCookie(reqCookieHeader) {
    return this.storedCookie || reqCookieHeader || '';
  }

  mergeResponseCookies(responseCookies) {
    const newCookieParts = parseCookieParts(responseCookies);
    if (newCookieParts.length === 0) return this.storedCookie;

    const existingCookies = toCookieMap(this.storedCookie);
    const hasExistingLogin = !!existingCookies.MUSIC_U || !!existingCookies.MUSIC_R_U;

    newCookieParts.forEach(part => {
      const [key, ...valueParts] = part.split('=');
      const trimmedKey = key?.trim();
      if (!trimmedKey) return;
      if (!hasExistingLogin || LOGIN_COOKIE_KEYS.has(trimmedKey)) {
        existingCookies[trimmedKey] = valueParts.join('=');
      }
    });

    const mergedCookie = Object.entries(existingCookies)
      .map(([key, value]) => `${key}=${value}`)
      .join('; ');

    if (mergedCookie && mergedCookie !== this.storedCookie) {
      this.saveCookie(mergedCookie);
    }
    return this.storedCookie;
  }

  extractCookieFromLoginResult(result = {}) {
    const fromHeaders = parseCookieParts(result.cookie);
    if (fromHeaders.length > 0) return fromHeaders.join('; ');

    if (typeof result.body?.cookie === 'string') {
      return parseCookieParts(result.body.cookie).join('; ');
    }
    return '';
  }
}

module.exports = new MusicAuthService();
