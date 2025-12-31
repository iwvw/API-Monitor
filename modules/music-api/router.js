/**
 * Music API 模块 - 网易云音乐代理
 * 使用 @neteasecloudmusicapienhanced/api 和 @unblockneteasemusic/server
 * Cookie 存储于数据库
 */

const express = require('express');
const router = express.Router();
const path = require('path');
const { createLogger } = require('../../src/utils/logger');
const dbService = require('../../src/db/database');

const logger = createLogger('Music');

// NCM API 库 (使用 npm 包)
let ncmApi = null;
let storedCookie = '';

/**
 * 加载 NCM API 库
 */
function loadNcmApi() {
    if (ncmApi) return ncmApi;

    try {
        ncmApi = require('@neteasecloudmusicapienhanced/api');
        logger.success('NCM API loaded from npm package');
        return ncmApi;
    } catch (error) {
        logger.error('Failed to load NCM API:', error.message);
        logger.warn('Please install: npm install @neteasecloudmusicapienhanced/api');
        return null;
    }
}

/**
 * 加载存储的 Cookie (从数据库)
 */
function loadStoredCookie() {
    try {
        const db = dbService.getDatabase();
        const row = db.prepare('SELECT value FROM music_settings WHERE key = ?').get('cookie');
        if (row && row.value) {
            storedCookie = row.value;
            logger.info('Loaded stored cookie from database, length:', storedCookie.length);
        } else {
            logger.info('No cookie found in database');
        }
    } catch (error) {
        logger.error('Failed to load cookie from database:', error.message);
    }
    return storedCookie;
}

/**
 * 保存 Cookie 到数据库
 */
function saveCookie(cookieString) {
    try {
        const db = dbService.getDatabase();
        db.prepare(`
            INSERT INTO music_settings (key, value, updated_at) 
            VALUES ('cookie', ?, CURRENT_TIMESTAMP)
            ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
        `).run(cookieString);
        storedCookie = cookieString;
        logger.success('Cookie saved to database');
    } catch (error) {
        logger.error('Failed to save cookie to database:', error.message);
    }
}

/**
 * 清除存储的 Cookie
 */
function clearCookie() {
    storedCookie = '';
    try {
        const db = dbService.getDatabase();
        db.prepare('DELETE FROM music_settings WHERE key = ?').run('cookie');
        logger.info('Cookie cleared from database');
    } catch (error) {
        logger.warn('Failed to clear cookie from database:', error.message);
    }
}

/**
 * 获取当前有效的 Cookie
 */
function getEffectiveCookie(reqCookieHeader) {
    // 优先使用服务器存储的 Cookie
    if (storedCookie) {
        return storedCookie;
    }
    // 兼容浏览器 Cookie
    return reqCookieHeader || '';
}

// 初始化
loadNcmApi();
loadStoredCookie();

// 已知不支持 HTTPS 的 CDN 域名列表
const HTTP_ONLY_DOMAINS = [
    'sycdn.kuwo.cn',     // 酷我音乐 CDN
    'er.sycdn.kuwo.cn',
    'other.web.rh01.sycdn.kuwo.cn',
    'kuwo.cn'            // 酷我其他域名
];

/**
 * 检查 URL 是否属于只支持 HTTP 的域名
 */
function isHttpOnlyDomain(url) {
    if (!url || typeof url !== 'string') return false;
    try {
        const hostname = new URL(url).hostname;
        return HTTP_ONLY_DOMAINS.some(domain => hostname.includes(domain));
    } catch {
        return false;
    }
}

/**
 * 将音频 URL 转换为代理 URL (用于不支持 HTTPS 的 CDN)
 * @param {string} url - 原始 URL
 * @param {object} req - Express 请求对象 (可选，用于构建完整代理 URL)
 * @returns {string} 代理 URL 或 HTTPS URL
 */
function getProxyUrl(url, req) {
    if (!url || typeof url !== 'string') return url;

    // 如果是不支持 HTTPS 的域名，使用代理
    if (isHttpOnlyDomain(url)) {
        // 构建代理 URL: /api/music/audio/proxy?url=xxx
        const encodedUrl = encodeURIComponent(url);
        return `/api/music/audio/proxy?url=${encodedUrl}`;
    }

    // 否则尝试使用 HTTPS
    return url.replace(/^http:\/\//i, 'https://');
}

/**
 * 确保 URL 使用 HTTPS (避免混合内容问题)
 * 对于不支持 HTTPS 的 CDN，返回代理 URL
 * @param {string} url - 原始 URL
 * @returns {string} HTTPS URL 或代理 URL
 */
function ensureHttps(url) {
    if (!url || typeof url !== 'string') return url;

    // 对于不支持 HTTPS 的域名，使用代理
    if (isHttpOnlyDomain(url)) {
        const encodedUrl = encodeURIComponent(url);
        return `/api/music/audio/proxy?url=${encodedUrl}`;
    }

    // 将 http:// 替换为 https://
    return url.replace(/^http:\/\//i, 'https://');
}

/**
 * 通用请求处理器
 */
async function handleRequest(moduleName, req, res) {
    const api = loadNcmApi();

    if (!api || typeof api[moduleName] !== 'function') {
        return res.status(404).json({
            code: 404,
            message: `API method ${moduleName} not found`
        });
    }

    try {
        const query = {
            ...req.query,
            ...req.body,
            cookie: getEffectiveCookie(req.headers.cookie)
        };

        const result = await api[moduleName](query);

        // 如果返回新的 Cookie，合并到现有 Cookie（而非覆盖）
        if (result.cookie && Array.isArray(result.cookie)) {
            // HTTP Cookie 属性列表（需要过滤掉）
            const httpAttrs = ['max-age', 'expires', 'path', 'domain', 'secure', 'httponly', 'samesite'];

            // 提取新 Cookie 的 key=value 部分，过滤 HTTP 属性
            const newCookieParts = [];
            result.cookie.forEach(c => {
                const match = String(c).match(/^([^;]+)/);
                if (match) {
                    const part = match[1].trim();
                    const [key] = part.split('=');
                    // 只保留非 HTTP 属性的 Cookie
                    if (key && !httpAttrs.includes(key.toLowerCase())) {
                        newCookieParts.push(part);
                    }
                }
            });

            if (newCookieParts.length === 0) {
                // 没有有效的新 Cookie，跳过
            } else {
                // 解析现有 Cookie
                const existingCookies = {};
                if (storedCookie) {
                    storedCookie.split(';').forEach(part => {
                        const trimmed = part.trim();
                        if (!trimmed) return;
                        const [key, ...valueParts] = trimmed.split('=');
                        if (key && !httpAttrs.includes(key.toLowerCase())) {
                            existingCookies[key.trim()] = valueParts.join('=');
                        }
                    });
                }

                // 检查现有 Cookie 是否包含登录态
                const hasExistingLogin = !!existingCookies['MUSIC_U'] || !!existingCookies['MUSIC_R_U'];

                // 合并新 Cookie
                newCookieParts.forEach(part => {
                    const [key, ...valueParts] = part.split('=');
                    if (!key) return;

                    const keyTrimmed = key.trim();

                    // 如果已有登录态，只接受登录相关的 Cookie 更新，拒绝匿名 Cookie 覆盖
                    if (hasExistingLogin) {
                        // 登录相关的 Cookie 可以更新
                        const loginCookies = ['MUSIC_U', 'MUSIC_R_U', 'MUSIC_A', 'MUSIC_A_T', '__csrf'];
                        if (loginCookies.includes(keyTrimmed)) {
                            existingCookies[keyTrimmed] = valueParts.join('=');
                        }
                        // NMTID 等匿名 Cookie 不更新
                    } else {
                        // 没有登录态时，接受所有 Cookie
                        existingCookies[keyTrimmed] = valueParts.join('=');
                    }
                });

                // 重新组合 Cookie 字符串
                const mergedCookie = Object.entries(existingCookies)
                    .map(([k, v]) => `${k}=${v}`)
                    .join('; ');

                if (mergedCookie && mergedCookie !== storedCookie) {
                    saveCookie(mergedCookie);
                }
            }
        }

        res.status(result.status || 200).json(result.body);
    } catch (error) {
        logger.error(`${moduleName} error:`, error.message || error);

        if (error.status && error.body) {
            return res.status(error.status).json(error.body);
        }

        res.status(500).json({
            code: 500,
            message: error.message || 'Internal server error'
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
            cookie: getEffectiveCookie(req.headers.cookie)
        };

        const result = await api.song_url_v1(query);
        const song = result.body?.data?.[0];

        const needUnblock = !song?.url ||
            song.freeTrialInfo !== null ||
            [1, 4].includes(song.fee);

        if (needUnblock && unblock !== 'false') {
            logger.info(`Song ${id} needs unblock, trying...`);

            try {
                const match = require('@unblockneteasemusic/server');
                const sources = ['pyncmd', 'bodian'];
                const unblocked = await match(Number(id), sources);

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
                            source: unblocked.source
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
            message: error.message || 'Internal server error'
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
        const match = require('@unblockneteasemusic/server');
        const sources = source ? source.split(',') : ['pyncmd', 'bodian'];

        logger.info(`Unblock: trying to match song ${id} with sources:`, sources);

        const result = await match(Number(id), sources);

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
                    source: result.source || 'unknown'
                }
            });
        } else {
            res.status(404).json({
                code: 404,
                message: 'No available source'
            });
        }
    } catch (error) {
        const errMsg = error?.message || (typeof error === 'string' ? error : 'Unblock failed');
        logger.error('Unblock error:', errMsg);
        res.status(500).json({
            code: 500,
            message: errMsg
        });
    }
});

// ==================== 音频代理 API ====================

/**
 * 音频流代理 - 用于转发不支持 HTTPS 的 CDN 资源
 * 解决浏览器混合内容 (Mixed Content) 阻止 HTTP 音频的问题
 */
router.get('/audio/proxy', async (req, res) => {
    const { url } = req.query;

    if (!url) {
        return res.status(400).json({ code: 400, message: 'Missing url parameter' });
    }

    let targetUrl;
    try {
        targetUrl = decodeURIComponent(url);
        // 验证是否为有效 URL
        new URL(targetUrl);
    } catch (e) {
        return res.status(400).json({ code: 400, message: 'Invalid url parameter' });
    }

    // 安全检查：只允许代理音频相关域名
    const allowedDomains = [
        'kuwo.cn',
        'kugou.com',
        'qq.com',
        'music.163.com',
        'netease.com'
    ];

    try {
        const urlObj = new URL(targetUrl);
        const isAllowed = allowedDomains.some(domain => urlObj.hostname.includes(domain));
        if (!isAllowed) {
            logger.warn(`[Proxy] Blocked request to unauthorized domain: ${urlObj.hostname}`);
            return res.status(403).json({ code: 403, message: 'Domain not allowed' });
        }
    } catch {
        return res.status(400).json({ code: 400, message: 'Invalid url' });
    }

    try {
        // 透传 Range 请求头以支持进度拖动
        const headers = {
            'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
            'Accept': '*/*',
            'Accept-Encoding': 'identity', // 避免压缩, 保持原始流
        };

        if (req.headers.range) {
            headers['Range'] = req.headers.range;
        }

        // 使用 fetch 转发请求
        const response = await fetch(targetUrl, {
            method: 'GET',
            headers,
            redirect: 'follow'
        });

        if (!response.ok && response.status !== 206) {
            logger.error(`[Proxy] Upstream error: ${response.status}`);
            return res.status(response.status).json({
                code: response.status,
                message: `Upstream error: ${response.statusText}`
            });
        }

        // 转发响应头
        const contentType = response.headers.get('content-type') || 'audio/mpeg';
        const contentLength = response.headers.get('content-length');
        const contentRange = response.headers.get('content-range');
        const acceptRanges = response.headers.get('accept-ranges');

        res.status(response.status);
        res.set('Content-Type', contentType);
        res.set('Accept-Ranges', acceptRanges || 'bytes');
        res.set('Access-Control-Allow-Origin', '*');
        res.set('Cache-Control', 'public, max-age=3600');

        if (contentLength) {
            res.set('Content-Length', contentLength);
        }
        if (contentRange) {
            res.set('Content-Range', contentRange);
        }

        // 流式转发响应体
        const reader = response.body.getReader();

        const stream = new ReadableStream({
            async start(controller) {
                try {
                    while (true) {
                        const { done, value } = await reader.read();
                        if (done) break;
                        controller.enqueue(value);
                    }
                    controller.close();
                } catch (err) {
                    controller.error(err);
                }
            }
        });

        // 将 ReadableStream 转换为 Node.js 可读流并 pipe 到响应
        const { Readable } = require('stream');
        const nodeStream = Readable.fromWeb(stream);
        nodeStream.pipe(res);

        // 记录代理请求
        logger.info(`[Proxy] Streaming audio from: ${new URL(targetUrl).hostname}`);

    } catch (error) {
        logger.error('[Proxy] Error:', error.message);
        res.status(500).json({
            code: 500,
            message: `Proxy error: ${error.message}`
        });
    }
});

// ==================== 歌词 API ====================

router.get('/lyric', (req, res) => handleRequest('lyric_new', req, res));

// ==================== 歌单 API ====================

router.get('/playlist/detail', async (req, res) => {
    const api = loadNcmApi();
    if (!api || typeof api.playlist_detail !== 'function') {
        return res.status(500).json({ code: 500, message: 'API not available' });
    }

    try {
        // 添加 s 参数获取歌曲数量（默认获取全部，最多 20000）
        const query = {
            id: req.query.id,
            s: req.query.s || 20000, // 获取歌曲详情的数量
            cookie: getEffectiveCookie(req.headers.cookie)
        };

        const result = await api.playlist_detail(query);

        // 调试日志
        logger.info('[Playlist] trackCount:', result.body?.playlist?.trackCount);
        logger.info('[Playlist] tracks length:', result.body?.playlist?.tracks?.length);
        logger.info('[Playlist] trackIds length:', result.body?.playlist?.trackIds?.length);

        // 如果歌单有歌曲但 tracks 为空，可能需要额外获取歌曲详情
        if (result.body?.playlist && result.body.playlist.trackIds?.length > 0 &&
            (!result.body.playlist.tracks || result.body.playlist.tracks.length === 0)) {

            logger.info('[Playlist] tracks empty, fetching song details for', result.body.playlist.trackIds.length, 'songs');

            // 获取前 500 首歌的详情 (支持通过 fetch_limit 参数控制，默认 500)
            const fetchLimit = parseInt(req.query.fetch_limit) || 500;
            const trackIds = result.body.playlist.trackIds.slice(0, fetchLimit).map(t => t.id);

            if (trackIds.length > 0 && api.song_detail) {
                const songResult = await api.song_detail({
                    ids: trackIds.join(','),
                    cookie: getEffectiveCookie(req.headers.cookie)
                });

                if (songResult.body?.songs) {
                    result.body.playlist.tracks = songResult.body.songs;
                    logger.info('[Playlist] Loaded', result.body.playlist.tracks.length, 'songs');
                }
            }
        }

        res.status(result.status || 200).json(result.body);
    } catch (error) {
        logger.error('playlist/detail error:', error.message);
        res.status(500).json({ code: 500, message: error.message });
    }
});
router.get('/top/playlist', (req, res) => handleRequest('top_playlist', req, res));
router.get('/top/playlist/highquality', (req, res) => handleRequest('top_playlist_highquality', req, res));
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
            cookie: getEffectiveCookie(req.headers.cookie)
        };

        const result = await api.login_qr_check(query);

        // 调试：打印完整结果结构
        logger.debug('[QR Check] result.body.code:', result.body?.code);
        logger.debug('[QR Check] result.cookie:', result.cookie);
        logger.debug('[QR Check] result.body.cookie:', result.body?.cookie);

        // 登录成功 (code 803) 时保存 Cookie
        if (result.body?.code === 803) {
            logger.info('[Music] 扫码登录成功，正在提取并持久化 Cookie...');

            let cookieStr = '';

            // 从 result.cookie 数组提取 (更可靠)
            if (result.cookie && Array.isArray(result.cookie)) {
                const rawCookies = result.cookie;
                // 提取每个 Set-Cookie 字符串中的 key=value 部分
                const cookieParts = rawCookies.map(c => {
                    // Set-Cookie 格式: "KEY=VALUE; Path=/; Max-Age=..."
                    // 只取第一个分号前的 KEY=VALUE 部分
                    const match = String(c).match(/^([^;]+)/);
                    return match ? match[1].trim() : '';
                }).filter(Boolean);
                cookieStr = cookieParts.join('; ');
                logger.info('[QR Check] Extracted from result.cookie array, length:', cookieStr.length);
            }

            // 备选：从 body.cookie 获取并解析 (需要清理格式)
            if (!cookieStr && result.body?.cookie && typeof result.body.cookie === 'string') {
                // body.cookie 可能是 "KEY1=VAL1; Max-Age=...; KEY2=VAL2; ..."
                // 需要按分号拆分，只保留 key=value 格式的部分
                const parts = result.body.cookie.split(';');
                const cleanParts = parts
                    .map(p => p.trim())
                    .filter(p => {
                        // 只保留 key=value 格式，排除 Max-Age, Expires, Path 等属性
                        if (!p.includes('=')) return false;
                        const key = p.split('=')[0].toLowerCase();
                        return !['max-age', 'expires', 'path', 'domain', 'secure', 'httponly', 'samesite'].includes(key);
                    });
                cookieStr = cleanParts.join('; ');
                logger.info('[QR Check] Extracted from result.body.cookie, length:', cookieStr.length);
            }

            // 调试：打印 Cookie 内容
            logger.debug('[QR Check] Cookie preview:', cookieStr.substring(0, 150));

            // 验证是否包含 MUSIC_U (登录态关键字段)
            if (cookieStr) {
                const hasMusicU = cookieStr.includes('MUSIC_U=');
                if (hasMusicU) {
                    saveCookie(cookieStr);
                    logger.success('[Music] 登录态已持久化到服务器数据库 (包含 MUSIC_U)');
                } else {
                    logger.warn('[Music] Cookie 不包含 MUSIC_U，可能不是有效的登录 Cookie');
                    logger.warn('[Music] Cookie 内容:', cookieStr);
                    // 仍然保存，但打印警告
                    saveCookie(cookieStr);
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
    clearCookie();
    res.json({ code: 200, message: 'Logged out successfully' });
});

/**
 * 获取登录状态（包含存储的 Cookie 状态）
 */
router.get('/auth/status', async (req, res) => {
    const api = loadNcmApi();

    // 每次检查时从数据库刷新 Cookie（确保使用最新的）
    loadStoredCookie();

    logger.debug('Auth status check, storedCookie length:', storedCookie ? storedCookie.length : 0);

    if (!storedCookie) {
        return res.json({
            code: 200,
            loggedIn: false,
            hasStoredCookie: false
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
                    vipType: profile.vipType || 0
                }
            });
        } else {
            logger.warn('Cookie exists but no profile returned');
            res.json({
                code: 200,
                loggedIn: false,
                hasStoredCookie: true,
                message: 'Cookie expired'
            });
        }
    } catch (error) {
        logger.error('Auth status error:', error.message);
        res.json({
            code: 200,
            loggedIn: false,
            hasStoredCookie: true,
            error: error.message
        });
    }
});

// ==================== 健康检查 ====================

router.get('/health', (req, res) => {
    const api = loadNcmApi();

    res.json({
        status: 'ok',
        modulesLoaded: !!api,
        moduleCount: api ? Object.keys(api).filter(k => typeof api[k] === 'function').length : 0,
        hasStoredCookie: !!storedCookie,
        timestamp: new Date().toISOString()
    });
});

module.exports = router;

