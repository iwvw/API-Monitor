/**
 * Cookie 解析工具
 */

/**
 * 解析请求中的 Cookie
 * @param {Object} req - Express 请求对象
 * @returns {Object} Cookie 键值对
 */
function parseCookies(req) {
  const header = req.headers && req.headers.cookie;
  const result = Object.create(null);
  if (!header) return result;

  header.split(';').forEach(pair => {
    const idx = pair.indexOf('=');
    if (idx < 0) return;
    const key = pair.slice(0, idx).trim();
    const val = pair.slice(idx + 1).trim();
    result[key] = decodeURIComponent(val);
  });

  return result;
}

module.exports = {
  parseCookies,
};
