const { createLogger } = require('../../../src/utils/logger');
const ncmClient = require('./ncm-client');

const logger = createLogger('MusicCatalogService');

class MusicCatalogService {
  constructor() {
    this.cache = new Map();
  }

  _cacheKey(moduleName, params = {}) {
    const stableParams = Object.keys(params)
      .sort()
      .reduce((acc, key) => {
        if (params[key] !== undefined && params[key] !== null) acc[key] = params[key];
        return acc;
      }, {});
    return `${moduleName}:${JSON.stringify(stableParams)}`;
  }

  _getCached(key) {
    const item = this.cache.get(key);
    if (!item) return null;
    if (item.expiresAt <= Date.now()) {
      this.cache.delete(key);
      return null;
    }
    return item.value;
  }

  _setCached(key, value, ttlMs) {
    if (!ttlMs) return;
    this.cache.set(key, {
      value,
      expiresAt: Date.now() + ttlMs,
    });
  }

  async invoke(moduleName, params = {}, reqCookieHeader = '', options = {}) {
    const cacheable = options.cacheable !== false && !reqCookieHeader;
    const ttlMs = options.ttlMs ?? 30_000;
    const cacheKey = cacheable ? this._cacheKey(moduleName, params) : null;
    const cached = cacheKey ? this._getCached(cacheKey) : null;
    if (cached) return cached;

    const result = await ncmClient.invoke(moduleName, params, reqCookieHeader);
    if (cacheKey) this._setCached(cacheKey, result, ttlMs);
    return result;
  }

  async playlistDetail(params = {}, reqCookieHeader = '') {
    const api = ncmClient.load();
    if (!api || typeof api.playlist_detail !== 'function') {
      const error = new Error('API not available');
      error.status = 500;
      throw error;
    }

    const result = await this.invoke(
      'playlist_detail',
      {
        id: params.id,
        s: params.s || 20000,
      },
      reqCookieHeader,
      { ttlMs: 60_000 }
    );

    logger.info('[Playlist] trackCount:', result.body?.playlist?.trackCount);
    logger.info('[Playlist] tracks length:', result.body?.playlist?.tracks?.length);
    logger.info('[Playlist] trackIds length:', result.body?.playlist?.trackIds?.length);

    if (
      result.body?.playlist &&
      result.body.playlist.trackIds?.length > 0 &&
      (!result.body.playlist.tracks || result.body.playlist.tracks.length === 0)
    ) {
      const fetchLimit = Math.min(parseInt(params.fetch_limit, 10) || 200, 500);
      const trackIds = result.body.playlist.trackIds.slice(0, fetchLimit).map(track => track.id);

      if (trackIds.length > 0 && ncmClient.hasMethod('song_detail')) {
        const songResult = await this.invoke(
          'song_detail',
          { ids: trackIds.join(',') },
          reqCookieHeader,
          { ttlMs: 60_000 }
        );

        if (songResult.body?.songs) {
          result.body.playlist.tracks = songResult.body.songs;
          logger.info('[Playlist] Loaded', result.body.playlist.tracks.length, 'songs');
        }
      }
    }

    return result;
  }
}

module.exports = new MusicCatalogService();
