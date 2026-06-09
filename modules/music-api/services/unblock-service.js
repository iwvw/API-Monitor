const { createLogger } = require('../../../src/utils/logger');

const logger = createLogger('MusicUnblockService');
const UNBLOCK_TIMEOUT_MS = 8000;
const DEFAULT_UNBLOCK_SOURCES = ['pyncmd', 'bodian'];

let unblockmatch = null;
let unblockLoadAttempted = false;

function withTimeout(promise, ms, message) {
  let timer = null;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(message)), ms);
  });

  return Promise.race([promise, timeout]).finally(() => {
    if (timer) clearTimeout(timer);
  });
}

class MusicUnblockService {
  parseSources(source) {
    if (!source) return DEFAULT_UNBLOCK_SOURCES;
    const sources = String(source)
      .split(',')
      .map(item => item.trim())
      .filter(Boolean);
    return sources.length ? sources : DEFAULT_UNBLOCK_SOURCES;
  }

  getMatcher() {
    if (!unblockLoadAttempted) {
      unblockLoadAttempted = true;
      try {
        unblockmatch = require('@unblockneteasemusic/server');
      } catch (error) {
        logger.warn('UnblockNeteaseMusic is not available:', error.message);
      }
    }

    if (typeof unblockmatch !== 'function') {
      throw new Error('UnblockNeteaseMusic is not available');
    }
    return unblockmatch;
  }

  async match(id, sources = DEFAULT_UNBLOCK_SOURCES) {
    const match = this.getMatcher();
    return withTimeout(
      match(Number(id), sources),
      UNBLOCK_TIMEOUT_MS,
      `Unblock timed out after ${UNBLOCK_TIMEOUT_MS}ms`
    );
  }
}

module.exports = new MusicUnblockService();
