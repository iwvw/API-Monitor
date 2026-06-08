/**
 * Music API 模块 - 网易云音乐代理
 * 使用 @neteasecloudmusicapienhanced/api 和 @unblockneteasemusic/server
 * Cookie 存储于数据库
 */

const express = require('express');
const router = express.Router();
const { createLogger } = require('../../src/utils/logger');
const authService = require('./services/auth-service');
const ncmClient = require('./services/ncm-client');
const catalogService = require('./services/catalog-service');
const unblockService = require('./services/unblock-service');
const audioProxyService = require('./services/audio-proxy-service');

const logger = createLogger('Music');

/**
 * 加载 NCM API 库
 */
function loadNcmApi() {
  return ncmClient.load();
}

/**
 * 获取当前有效的 Cookie
 */
function getEffectiveCookie(reqCookieHeader) {
  return authService.getEffectiveCookie(reqCookieHeader);
}

// 初始化
loadNcmApi();
authService.loadStoredCookie();

/**
 * 确保 URL 使用 HTTPS (避免混合内容问题)
 * 对于不支持 HTTPS 的 CDN，返回代理 URL
 * @param {string} url - 原始 URL
 * @returns {string} HTTPS URL 或代理 URL
 */
function ensureHttps(url) {
  return audioProxyService.ensureHttps(url);
}

function parseUnblockSources(source) {
  return unblockService.parseSources(source);
}

function matchUnblockedSong(id, sources) {
  return unblockService.match(id, sources);
}

/**
 * 通用请求处理器
 */
async function handleRequest(moduleName, req, res) {
  try {
    const result = await catalogService.invoke(moduleName, {
      ...req.query,
      ...req.body,
    }, req.headers.cookie);
    res.status(result.status || 200).json(result.body);
  } catch (error) {
    logger.error(`${moduleName} error:`, error.message || error);

    if (error.status && error.body) {
      return res.status(error.status).json(error.body);
    }

    res.status(500).json({
      code: 500,
      message: error.message || 'Internal server error',
    });
  }
}

// ==================== 搜索 API ====================

router.get('/search', (req, res) => handleRequest('cloudsearch', req, res));
router.get('/search/suggest', (req, res) => handleRequest('search_suggest', req, res));
router.get('/search/hot', (req, res) => handleRequest('search_hot_detail', req, res));

// ==================== 歌曲 API ====================

/**
 * 获取歌曲播放地址 (自动解锁)
 */
router.get('/song/url', async (req, res) => {
  const api = loadNcmApi();
  const { id, level = 'exhigh', unblock = 'true' } = req.query;

  if (!id) {
    return res.status(400).json({ code: 400, message: 'Missing song id' });
  }

  if (!api || typeof api.song_url_v1 !== 'function') {
    return res.status(500).json({ code: 500, message: 'NCM API not available' });
  }

  try {
    const query = {
      id,
      level,
      cookie: getEffectiveCookie(req.headers.cookie),
    };

    const result = await api.song_url_v1(query);
    const song = result.body?.data?.[0];

    const needUnblock = !song?.url || song.freeTrialInfo !== null || [1, 4].includes(song.fee);

    if (needUnblock && unblock !== 'false') {
      logger.info(`Song ${id} needs unblock, trying...`);

      try {
        const unblocked = await matchUnblockedSong(id);

        if (unblocked && unblocked.url) {
          logger.success(`Song ${id} unblocked from ${unblocked.source}`);

          if (song) {
            song.url = ensureHttps(unblocked.url);
            song.br = unblocked.br || 320000;
            song.size = unblocked.size || song.size;
            song.freeTrialInfo = null;
            song.source = unblocked.source;
          } else if (result.body?.data) {
            result.body.data[0] = {
              id: Number(id),
              url: ensureHttps(unblocked.url),
              br: unblocked.br || 320000,
              size: unblocked.size || 0,
              md5: unblocked.md5 || null,
              code: 200,
              type: 'unblock',
              source: unblocked.source,
            };
          }
        }
      } catch (unlockErr) {
        logger.warn(`Unblock failed for ${id}:`, unlockErr.message);
      }
    }

    if (result.cookie && Array.isArray(result.cookie)) {
      res.set('Set-Cookie', result.cookie);
    }

    // 在返回之前确保 URL 使用 HTTPS
    if (result.body?.data?.[0]?.url) {
      result.body.data[0].url = ensureHttps(result.body.data[0].url);
    }

    res.status(result.status || 200).json(result.body);
  } catch (error) {
    logger.error('song/url error:', error.message || error);

    if (error.status && error.body) {
      return res.status(error.status).json(error.body);
    }

    res.status(500).json({
      code: 500,
      message: error.message || 'Internal server error',
    });
  }
});

router.get('/song/detail', (req, res) => handleRequest('song_detail', req, res));

/**
 * 使用解锁服务获取歌曲 URL
 */
router.get('/song/url/unblock', async (req, res) => {
  const { id, source } = req.query;

  if (!id) {
    return res.status(400).json({ code: 400, message: 'Missing song id' });
  }

  try {
    const sources = parseUnblockSources(source);

    logger.info(`Unblock: trying to match song ${id} with sources:`, sources);

    const result = await matchUnblockedSong(id, sources);

    if (result && result.url) {
      logger.success(`Unblock: matched song ${id} from ${result.source}`);
      res.json({
        code: 200,
        data: {
          id: Number(id),
          url: ensureHttps(result.url),
          br: result.br || 320000,
          size: result.size || 0,
          md5: result.md5 || null,
          source: result.source || 'unknown',
        },
      });
    } else {
      res.status(404).json({
        code: 404,
        message: 'No available source',
      });
    }
  } catch (error) {
    const errMsg = error?.message || (typeof error === 'string' ? error : 'Unblock failed');
    logger.error('Unblock error:', errMsg);
    res.status(500).json({
      code: 500,
      message: errMsg,
    });
  }
});

// ==================== 音频代理 API ====================

/**
 * 音频流代理 - 用于转发不支持 HTTPS 的 CDN 资源
 * 解决浏览器混合内容 (Mixed Content) 阻止 HTTP 音频的问题
 */
router.get('/audio/proxy', async (req, res) => {
  return audioProxyService.proxy(req, res);
});

// ==================== 歌词 API ====================

router.get('/lyric', (req, res) => handleRequest('lyric_new', req, res));

// ==================== 歌单 API ====================

router.get('/playlist/detail', async (req, res) => {
  try {
    const result = await catalogService.playlistDetail(req.query, req.headers.cookie);
    res.status(result.status || 200).json(result.body);
  } catch (error) {
    logger.error('playlist/detail error:', error.message);
    res.status(error.status || 500).json({ code: error.status || 500, message: error.message });
  }
});
router.get('/top/playlist', (req, res) => handleRequest('top_playlist', req, res));
router.get('/top/playlist/highquality', (req, res) =>
  handleRequest('top_playlist_highquality', req, res)
);
router.get('/playlist/catlist', (req, res) => handleRequest('playlist_catlist', req, res));

// ==================== 推荐 API ====================

router.get('/recommend/songs', (req, res) => handleRequest('recommend_songs', req, res));
router.get('/personalized', (req, res) => handleRequest('personalized', req, res));
router.get('/personalized/newsong', (req, res) => handleRequest('personalized_newsong', req, res));
router.get('/personal/fm', (req, res) => handleRequest('personal_fm', req, res));

// ==================== 排行榜 API ====================

router.get('/toplist', (req, res) => handleRequest('toplist', req, res));
router.get('/toplist/detail', (req, res) => handleRequest('toplist_detail', req, res));

// ==================== 歌手 API ====================

router.get('/artist/detail', (req, res) => handleRequest('artist_detail', req, res));
router.get('/artist/top/song', (req, res) => handleRequest('artist_top_song', req, res));
router.get('/artist/songs', (req, res) => handleRequest('artist_songs', req, res));
router.get('/artist/album', (req, res) => handleRequest('artist_album', req, res));

// ==================== 专辑 API ====================

router.get('/album', (req, res) => handleRequest('album', req, res));
router.get('/album/detail', (req, res) => handleRequest('album_detail', req, res));

// ==================== MV API ====================

router.get('/mv/detail', (req, res) => handleRequest('mv_detail', req, res));
router.get('/mv/url', (req, res) => handleRequest('mv_url', req, res));

// ==================== 用户 API ====================

router.get('/user/playlist', (req, res) => handleRequest('user_playlist', req, res));
router.get('/user/record', (req, res) => handleRequest('user_record', req, res));
router.get('/likelist', (req, res) => handleRequest('likelist', req, res));
router.get('/login/status', (req, res) => handleRequest('login_status', req, res));
router.get('/login/qr/key', (req, res) => handleRequest('login_qr_key', req, res));
router.get('/login/qr/create', (req, res) => handleRequest('login_qr_create', req, res));
router.get('/login/qr/check', async (req, res) => {
  const api = loadNcmApi();

  if (!api || typeof api.login_qr_check !== 'function') {
    return res.status(404).json({ code: 404, message: 'API method not found' });
  }

  try {
    const query = {
      ...req.query,
      cookie: getEffectiveCookie(req.headers.cookie),
    };

    const result = await api.login_qr_check(query);

    // 调试：打印完整结果结构
    logger.debug('[QR Check] result.body.code:', result.body?.code);
    logger.debug('[QR Check] result.cookie:', result.cookie);
    logger.debug('[QR Check] result.body.cookie:', result.body?.cookie);

    // 登录成功 (code 803) 时保存 Cookie
    if (result.body?.code === 803) {
      logger.info('[Music] 扫码登录成功，正在提取并持久化 Cookie...');

      const cookieStr = authService.extractCookieFromLoginResult(result);

      // 调试：打印 Cookie 内容
      logger.debug('[QR Check] Cookie preview:', cookieStr.substring(0, 150));

      // 验证是否包含 MUSIC_U (登录态关键字段)
      if (cookieStr) {
        const hasMusicU = cookieStr.includes('MUSIC_U=');
        if (hasMusicU) {
          authService.saveCookie(cookieStr);
          logger.success('[Music] 登录态已持久化到服务器数据库 (包含 MUSIC_U)');
        } else {
          logger.warn('[Music] Cookie 不包含 MUSIC_U，可能不是有效的登录 Cookie');
          logger.warn('[Music] Cookie 内容:', cookieStr);
          // 仍然保存，但打印警告
          authService.saveCookie(cookieStr);
        }
      } else {
        logger.error('[Music] 登录成功但未提取到有效 Cookie');
        logger.error('[Music] result.cookie:', JSON.stringify(result.cookie));
        logger.error('[Music] result.body.cookie:', result.body?.cookie);
      }
    }

    res.status(result.status || 200).json(result.body);
  } catch (error) {
    logger.error('login_qr_check error:', error.message || error);
    res.status(500).json({ code: 500, message: error.message || 'Internal server error' });
  }
});

/**
 * 退出登录 - 清除服务器存储的 Cookie
 */
router.post('/logout', (req, res) => {
  authService.clearCookie();
  res.json({ code: 200, message: 'Logged out successfully' });
});

/**
 * 获取登录状态（包含存储的 Cookie 状态）
 */
router.get('/auth/status', async (req, res) => {
  const api = loadNcmApi();

  // 每次检查时从数据库刷新 Cookie（确保使用最新的）
  const storedCookie = authService.loadStoredCookie();

  logger.debug('Auth status check, storedCookie length:', storedCookie ? storedCookie.length : 0);

  if (!storedCookie) {
    return res.json({
      code: 200,
      loggedIn: false,
      hasStoredCookie: false,
    });
  }

  try {
    const result = await api.login_status({ cookie: storedCookie });

    // 调试：打印完整返回结构
    logger.info('login_status raw result:', JSON.stringify(result.body, null, 2));

    // 网易云 API 可能在不同位置返回 profile
    const profile = result.body?.data?.profile || result.body?.profile;

    if (profile) {
      res.json({
        code: 200,
        loggedIn: true,
        hasStoredCookie: true,
        user: {
          userId: profile.userId,
          nickname: profile.nickname,
          avatarUrl: profile.avatarUrl,
          vipType: profile.vipType || 0,
        },
      });
    } else {
      logger.warn('Cookie exists but no profile returned');
      res.json({
        code: 200,
        loggedIn: false,
        hasStoredCookie: true,
        message: 'Cookie expired',
      });
    }
  } catch (error) {
    logger.error('Auth status error:', error.message);
    res.json({
      code: 200,
      loggedIn: false,
      hasStoredCookie: true,
      error: error.message,
    });
  }
});

// ==================== 健康检查 ====================

router.get('/health', (req, res) => {
  const health = ncmClient.getHealth();

  res.json({
    status: 'ok',
    modulesLoaded: health.modulesLoaded,
    moduleCount: health.moduleCount,
    hasStoredCookie: !!authService.getStoredCookie(),
    timestamp: new Date().toISOString(),
  });
});

module.exports = router;
