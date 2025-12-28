/**
 * Music API Router - 音乐模块后端路由
 * 使用 @neteasecloudmusicapienhanced/api (npm 包)
 */

const express = require('express');
const router = express.Router();

// NCM API 库 (使用 npm 包)
let ncmApi = null;

/**
 * 加载 NCM API 库
 */
function loadNcmApi() {
    if (ncmApi) return ncmApi;

    try {
        // 使用 npm 包 @neteasecloudmusicapienhanced/api
        ncmApi = require('@neteasecloudmusicapienhanced/api');
        console.log('[Music] NCM API loaded from npm package');
        return ncmApi;
    } catch (error) {
        console.error('[Music] Failed to load NCM API:', error.message);
        console.warn('[Music] Please install: npm install @neteasecloudmusicapienhanced/api');
        return null;
    }
}

// 初始化加载
loadNcmApi();

/**
 * 解析 Cookie 字符串
 */
function parseCookies(cookieHeader) {
    const cookies = {};
    if (!cookieHeader) return cookies;

    cookieHeader.split(';').forEach(pair => {
        const [key, value] = pair.trim().split('=');
        if (key && value) {
            cookies[key] = value;
        }
    });

    return cookies;
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
            cookie: parseCookies(req.headers.cookie)
        };

        // 直接调用 api-enhanced 封装好的方法
        const result = await api[moduleName](query);

        // 转发 Set-Cookie
        if (result.cookie && Array.isArray(result.cookie)) {
            res.set('Set-Cookie', result.cookie);
        }

        res.status(result.status || 200).json(result.body);
    } catch (error) {
        console.error(`[Music] ${moduleName} error:`, error);

        if (error.status && error.body) {
            return res.status(error.status).json(error.body);
        }

        res.status(500).json({
            code: 500,
            message: error.message || 'Internal server error'
        });
    }
}

// ===== 搜索相关 =====

/**
 * 搜索歌曲
 * GET /api/music/search?keywords=xxx&type=1&limit=30
 */
router.get('/search', (req, res) => handleRequest('cloudsearch', req, res));

/**
 * 搜索建议
 * GET /api/music/search/suggest?keywords=xxx
 */
router.get('/search/suggest', (req, res) => handleRequest('search_suggest', req, res));

/**
 * 热门搜索
 * GET /api/music/search/hot
 */
router.get('/search/hot', (req, res) => handleRequest('search_hot_detail', req, res));

// ===== 歌曲相关 =====

/**
 * 获取歌曲播放地址 (自动解锁)
 * GET /api/music/song/url?id=xxx&level=exhigh&unblock=true
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
            cookie: parseCookies(req.headers.cookie)
        };

        // 调用官方接口
        const result = await api.song_url_v1(query);
        const song = result.body?.data?.[0];

        // 检查是否需要解锁
        const needUnblock = !song?.url ||
            song.freeTrialInfo !== null ||
            [1, 4].includes(song.fee);

        // 自动尝试解锁
        if (needUnblock && unblock !== 'false') {
            console.log(`[Music] Song ${id} needs unblock, trying...`);

            try {
                const match = require('@unblockneteasemusic/server');
                const sources = ['kugou', 'kuwo', 'migu', 'youtube'];
                const unblocked = await match(Number(id), sources);

                if (unblocked && unblocked.url) {
                    console.log(`[Music] Song ${id} unblocked from ${unblocked.source}`);

                    // 替换歌曲数据
                    if (song) {
                        song.url = unblocked.url;
                        song.br = unblocked.br || 320000;
                        song.size = unblocked.size || song.size;
                        song.freeTrialInfo = null;
                        song.source = unblocked.source;
                    } else if (result.body?.data) {
                        result.body.data[0] = {
                            id: Number(id),
                            url: unblocked.url,
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
                console.warn(`[Music] Unblock failed for ${id}:`, unlockErr.message);
            }
        }

        // 转发 Set-Cookie
        if (result.cookie && Array.isArray(result.cookie)) {
            res.set('Set-Cookie', result.cookie);
        }

        res.status(result.status || 200).json(result.body);
    } catch (error) {
        console.error('[Music] song/url error:', error);

        if (error.status && error.body) {
            return res.status(error.status).json(error.body);
        }

        res.status(500).json({
            code: 500,
            message: error.message || 'Internal server error'
        });
    }
});

/**
 * 获取歌曲详情
 * GET /api/music/song/detail?ids=xxx,yyy
 */
router.get('/song/detail', (req, res) => handleRequest('song_detail', req, res));

/**
 * 使用解锁服务获取歌曲 URL
 * GET /api/music/song/url/unblock?id=xxx&source=kugou,kuwo,migu
 */
router.get('/song/url/unblock', async (req, res) => {
    const { id, source } = req.query;

    if (!id) {
        return res.status(400).json({ code: 400, message: 'Missing song id' });
    }

    try {
        // 使用 npm 包 @unblockneteasemusic/server
        const match = require('@unblockneteasemusic/server');

        // 默认音源列表：酷狗、酷我、咪咕、YouTube
        const sources = source ? source.split(',') : ['kugou', 'kuwo', 'migu', 'youtube'];

        console.log(`[Music] Unblock: trying to match song ${id} with sources:`, sources);

        const result = await match(Number(id), sources);

        if (result && result.url) {
            console.log(`[Music] Unblock: matched song ${id} from ${result.source}`);
            res.json({
                code: 200,
                data: {
                    id: Number(id),
                    url: result.url,
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
        console.error('[Music] Unblock error:', error.message || error);
        res.status(500).json({
            code: 500,
            message: error.message || 'Unblock failed'
        });
    }
});

// ===== 歌词相关 =====

/**
 * 获取歌词
 * GET /api/music/lyric?id=xxx
 */
router.get('/lyric', (req, res) => handleRequest('lyric_new', req, res));

// ===== 歌单相关 =====

/**
 * 获取歌单详情
 * GET /api/music/playlist/detail?id=xxx
 */
router.get('/playlist/detail', (req, res) => handleRequest('playlist_detail', req, res));

/**
 * 获取热门歌单
 * GET /api/music/top/playlist?limit=20
 */
router.get('/top/playlist', (req, res) => handleRequest('top_playlist', req, res));

/**
 * 获取精品歌单
 * GET /api/music/top/playlist/highquality?limit=20
 */
router.get('/top/playlist/highquality', (req, res) => handleRequest('top_playlist_highquality', req, res));

/**
 * 获取歌单分类
 * GET /api/music/playlist/catlist
 */
router.get('/playlist/catlist', (req, res) => handleRequest('playlist_catlist', req, res));

// ===== 推荐相关 =====

/**
 * 每日推荐歌曲 (需要登录)
 * GET /api/music/recommend/songs
 */
router.get('/recommend/songs', (req, res) => handleRequest('recommend_songs', req, res));

/**
 * 推荐歌单 (无需登录)
 * GET /api/music/personalized?limit=10
 */
router.get('/personalized', (req, res) => handleRequest('personalized', req, res));

/**
 * 推荐新歌
 * GET /api/music/personalized/newsong
 */
router.get('/personalized/newsong', (req, res) => handleRequest('personalized_newsong', req, res));

/**
 * 私人 FM (需要登录)
 * GET /api/music/personal/fm
 */
router.get('/personal/fm', (req, res) => handleRequest('personal_fm', req, res));

// ===== 排行榜 =====

/**
 * 获取排行榜列表
 * GET /api/music/toplist
 */
router.get('/toplist', (req, res) => handleRequest('toplist', req, res));

/**
 * 获取排行榜详情
 * GET /api/music/toplist/detail
 */
router.get('/toplist/detail', (req, res) => handleRequest('toplist_detail', req, res));

// ===== 歌手相关 =====

/**
 * 获取歌手详情
 * GET /api/music/artist/detail?id=xxx
 */
router.get('/artist/detail', (req, res) => handleRequest('artist_detail', req, res));

/**
 * 获取歌手热门歌曲
 * GET /api/music/artist/top/song?id=xxx
 */
router.get('/artist/top/song', (req, res) => handleRequest('artist_top_song', req, res));

/**
 * 获取歌手专辑
 * GET /api/music/artist/album?id=xxx
 */
router.get('/artist/album', (req, res) => handleRequest('artist_album', req, res));

// ===== 专辑相关 =====

/**
 * 获取专辑详情
 * GET /api/music/album?id=xxx
 */
router.get('/album', (req, res) => handleRequest('album', req, res));

/**
 * 获取专辑内容
 * GET /api/music/album/detail?id=xxx
 */
router.get('/album/detail', (req, res) => handleRequest('album_detail', req, res));

// ===== MV 相关 =====

/**
 * 获取 MV 详情
 * GET /api/music/mv/detail?mvid=xxx
 */
router.get('/mv/detail', (req, res) => handleRequest('mv_detail', req, res));

/**
 * 获取 MV 播放地址
 * GET /api/music/mv/url?id=xxx
 */
router.get('/mv/url', (req, res) => handleRequest('mv_url', req, res));

// ===== 用户相关 =====

/**
 * 获取用户歌单
 * GET /api/music/user/playlist?uid=xxx
 */
router.get('/user/playlist', (req, res) => handleRequest('user_playlist', req, res));

/**
 * 获取登录状态
 * GET /api/music/login/status
 */
router.get('/login/status', (req, res) => handleRequest('login_status', req, res));

/**
 * 获取二维码 key
 * GET /api/music/login/qr/key
 */
router.get('/login/qr/key', (req, res) => handleRequest('login_qr_key', req, res));

/**
 * 生成二维码
 * GET /api/music/login/qr/create?key=xxx
 */
router.get('/login/qr/create', (req, res) => handleRequest('login_qr_create', req, res));

/**
 * 检查二维码状态
 * GET /api/music/login/qr/check?key=xxx
 */
router.get('/login/qr/check', (req, res) => handleRequest('login_qr_check', req, res));

// ===== 健康检查 =====

/**
 * 检查音乐模块状态
 * GET /api/music/health
 */
router.get('/health', (req, res) => {
    const api = loadNcmApi();

    res.json({
        status: 'ok',
        modulesLoaded: !!api,
        moduleCount: api ? Object.keys(api).filter(k => typeof api[k] === 'function').length : 0,
        timestamp: new Date().toISOString()
    });
});

module.exports = router;
