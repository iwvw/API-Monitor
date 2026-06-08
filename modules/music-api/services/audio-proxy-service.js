const { Readable } = require('stream');
const { pipeline } = require('stream/promises');
const { createLogger } = require('../../../src/utils/logger');

const logger = createLogger('MusicAudioProxyService');

const HTTP_ONLY_DOMAINS = [
  'sycdn.kuwo.cn',
  'er.sycdn.kuwo.cn',
  'other.web.rh01.sycdn.kuwo.cn',
  'kuwo.cn',
];

const AUDIO_PROXY_ALLOWED_DOMAINS = ['kuwo.cn', 'kugou.com', 'qq.com', 'music.163.com', 'netease.com'];

function hostMatchesDomain(hostname, domain) {
  return hostname === domain || hostname.endsWith(`.${domain}`);
}

class AudioProxyService {
  isHttpOnlyDomain(url) {
    if (!url || typeof url !== 'string') return false;
    try {
      const hostname = new URL(url).hostname;
      return HTTP_ONLY_DOMAINS.some(domain => hostMatchesDomain(hostname, domain));
    } catch {
      return false;
    }
  }

  ensureHttps(url) {
    if (!url || typeof url !== 'string') return url;
    if (this.isHttpOnlyDomain(url)) {
      return `/api/music/audio/proxy?url=${encodeURIComponent(url)}`;
    }
    return url.replace(/^http:\/\//i, 'https://');
  }

  validateTargetUrl(rawUrl) {
    const targetUrl = decodeURIComponent(rawUrl || '');
    const parsed = new URL(targetUrl);
    if (!['http:', 'https:'].includes(parsed.protocol)) {
      throw new Error('Invalid protocol');
    }
    const allowed = AUDIO_PROXY_ALLOWED_DOMAINS.some(domain => hostMatchesDomain(parsed.hostname, domain));
    if (!allowed) {
      const error = new Error('Domain not allowed');
      error.status = 403;
      throw error;
    }
    return { targetUrl, parsed };
  }

  async proxy(req, res) {
    if (!req.query.url) {
      return res.status(400).json({ code: 400, message: 'Missing url parameter' });
    }

    let targetUrl;
    let parsed;
    try {
      ({ targetUrl, parsed } = this.validateTargetUrl(req.query.url));
    } catch (error) {
      if (error.status === 403) {
        logger.warn(`[Proxy] Blocked request to unauthorized domain: ${error.hostname || 'unknown'}`);
        return res.status(403).json({ code: 403, message: 'Domain not allowed' });
      }
      return res.status(400).json({ code: 400, message: 'Invalid url parameter' });
    }

    try {
      const headers = {
        'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
        Accept: '*/*',
        'Accept-Encoding': 'identity',
      };
      if (req.headers.range) {
        headers.Range = req.headers.range;
      }

      const response = await fetch(targetUrl, {
        method: 'GET',
        headers,
        redirect: 'follow',
      });

      if (!response.ok && response.status !== 206) {
        logger.error(`[Proxy] Upstream error: ${response.status}`);
        return res.status(response.status).json({
          code: response.status,
          message: `Upstream error: ${response.statusText}`,
        });
      }

      const contentType = response.headers.get('content-type') || 'audio/mpeg';
      const contentLength = response.headers.get('content-length');
      const contentRange = response.headers.get('content-range');
      const acceptRanges = response.headers.get('accept-ranges');

      res.status(response.status);
      res.set('Content-Type', contentType);
      res.set('Accept-Ranges', acceptRanges || 'bytes');
      res.set('Access-Control-Allow-Origin', '*');
      res.set('Cache-Control', 'public, max-age=3600');
      if (contentLength) res.set('Content-Length', contentLength);
      if (contentRange) res.set('Content-Range', contentRange);

      await pipeline(Readable.fromWeb(response.body), res);
      logger.info(`[Proxy] Streaming audio from: ${parsed.hostname}`);
    } catch (error) {
      if (res.headersSent) {
        if (error.message !== 'Premature close') {
          logger.warn('[Proxy] Stream error (response already sent):', error.message);
        }
        return;
      }

      logger.error('[Proxy] Error:', error.message);
      res.status(500).json({
        code: 500,
        message: `Proxy error: ${error.message}`,
      });
    }
  }
}

module.exports = new AudioProxyService();
