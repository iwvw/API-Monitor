/**
 * TOTP/HOTP 验证码生成服务
 * 使用 otplib 库实现标准 TOTP/HOTP 算法
 */

const { authenticator, hotp } = require('otplib');

// 配置 TOTP 默认选项
authenticator.options = {
  digits: 6,
  step: 30,
  window: 1, // 允许前后 1 个时间窗口的容差
};

// 配置 HOTP 默认选项
hotp.options = {
  digits: 6,
};

function withTemporaryOptions(generator, patch, callback) {
  const previous = { ...generator.options };
  try {
    generator.options = { ...previous, ...patch };
    return callback();
  } finally {
    generator.options = previous;
  }
}

function normalizeOtpAlgorithm(value) {
  const normalized = String(value || 'SHA1')
    .replace(/[-_]/g, '')
    .toLowerCase();

  if (normalized === 'sha256') return 'sha256';
  if (normalized === 'sha512') return 'sha512';
  return 'sha1';
}

function buildTotpOptions(options = {}) {
  return {
    digits: options.digits || 6,
    step: options.period || 30,
    algorithm: normalizeOtpAlgorithm(options.algorithm),
  };
}

function buildHotpOptions(options = {}) {
  return {
    digits: options.digits || 6,
    algorithm: normalizeOtpAlgorithm(options.algorithm),
  };
}

/**
 * 生成 TOTP 验证码
 * @param {string} secret - Base32 编码的密钥
 * @param {Object} options - 可选的 TOTP 参数
 * @returns {string} 6-8 位验证码
 */
function generateTotpCode(secret, options = {}) {
  try {
    return withTemporaryOptions(authenticator, buildTotpOptions(options), () =>
      authenticator.generate(secret)
    );
  } catch (error) {
    console.error('[TOTP Service] 生成 TOTP 验证码失败:', error.message);
    return null;
  }
}

/**
 * 生成 HOTP 验证码
 * @param {string} secret - Base32 编码的密钥
 * @param {number} counter - 计数器值
 * @param {Object} options - 可选的 HOTP 参数
 * @returns {string} 6-8 位验证码
 */
function generateHotpCode(secret, counter, options = {}) {
  try {
    return withTemporaryOptions(hotp, buildHotpOptions(options), () => hotp.generate(secret, counter));
  } catch (error) {
    console.error('[HOTP Service] 生成 HOTP 验证码失败:', error.message);
    return null;
  }
}

/**
 * 根据账号类型生成验证码
 * @param {Object} account - 账号对象
 * @returns {Object} { code, remaining?, counter? }
 */
function generateCode(account) {
  const { otp_type = 'totp', secret, digits, period, counter } = account;

  if (otp_type === 'hotp') {
    return {
      code: generateHotpCode(secret, counter || 0, { digits, algorithm: account.algorithm }),
      counter: counter || 0,
    };
  }

  // 默认 TOTP
  const now = Math.floor(Date.now() / 1000);
  const currentPeriod = period || 30;
  return {
    code: generateTotpCode(secret, { digits, period: currentPeriod, algorithm: account.algorithm }),
    remaining: currentPeriod - (now % currentPeriod),
  };
}

/**
 * 验证 TOTP 验证码
 * @param {string} secret - Base32 编码的密钥
 * @param {string} token - 用户输入的验证码
 * @param {Object} options - 可选的 TOTP 参数
 * @returns {boolean} 是否验证通过
 */
function verifyTotpCode(secret, token, options = {}) {
  try {
    return withTemporaryOptions(authenticator, buildTotpOptions(options), () =>
      authenticator.verify({ token, secret })
    );
  } catch (error) {
    console.error('[TOTP Service] 验证失败:', error.message);
    return false;
  }
}

/**
 * 验证 HOTP 验证码
 * @param {string} secret - Base32 编码的密钥
 * @param {string} token - 用户输入的验证码
 * @param {number} counter - 当前计数器值
 * @param {Object} options - 可选参数
 * @returns {Object} { valid, newCounter }
 */
function verifyHotpCode(secret, token, counter, options = {}) {
  try {
    // 检查当前计数器及之后几个值
    const window = options.window || 10;
    return withTemporaryOptions(hotp, buildHotpOptions(options), () => {
      for (let i = 0; i <= window; i++) {
        if (hotp.verify({ token, secret, counter: counter + i })) {
          return { valid: true, newCounter: counter + i + 1 };
        }
      }
      return { valid: false, newCounter: counter };
    });
  } catch (error) {
    console.error('[HOTP Service] 验证失败:', error.message);
    return { valid: false, newCounter: counter };
  }
}

/**
 * 获取当前周期剩余秒数
 * @param {number} period - 刷新周期(秒)，默认 30
 * @returns {number} 剩余秒数
 */
function getRemainingSeconds(period = 30) {
  return period - (Math.floor(Date.now() / 1000) % period);
}

/**
 * 批量生成验证码
 * @param {Array} accounts - 账号数组
 * @returns {Object} { id: { code, remaining/counter, nextCode? } }
 */
function generateAllCodes(accounts) {
  const result = {};
  const now = Math.floor(Date.now() / 1000);

  for (const acc of accounts) {
    try {
      if (acc.otp_type === 'hotp') {
        result[acc.id] = {
          code: generateHotpCode(acc.secret, acc.counter || 0, {
            digits: acc.digits,
            algorithm: acc.algorithm,
          }),
          counter: acc.counter || 0,
          type: 'hotp',
        };
      } else {
        const period = acc.period || 30;
        const digits = acc.digits || 6;

        result[acc.id] = {
          code: generateTotpCode(acc.secret, {
            digits,
            period,
            algorithm: acc.algorithm,
          }),
          remaining: period - (now % period),
          type: 'totp',
        };
      }
    } catch (error) {
      result[acc.id] = { code: null, error: error.message };
    }
  }

  return result;
}

/**
 * 生成随机密钥
 * @returns {string} Base32 编码的随机密钥
 */
function generateSecret() {
  return authenticator.generateSecret();
}

/**
 * 生成 OTP URI (用于二维码)
 * @param {Object} params - 参数对象
 * @returns {string} otpauth:// URI
 */
function generateUri(params) {
  const { type = 'totp', issuer, account, secret, algorithm, digits, period, counter } = params;

  if (type === 'hotp') {
    const searchParams = new URLSearchParams({
      secret,
      issuer,
      algorithm: algorithm || 'SHA1',
      digits: digits || 6,
      counter: counter || 0,
    });
    return `otpauth://hotp/${encodeURIComponent(issuer)}:${encodeURIComponent(account)}?${searchParams}`;
  }

  // TOTP
  return authenticator.keyuri(account, issuer, secret);
}

/**
 * 解析 otpauth:// URI
 * @param {string} uri - OTP URI
 * @returns {Object|null} 解析结果
 */
function parseUri(uri) {
  try {
    const url = new URL(uri);

    if (url.protocol !== 'otpauth:') {
      throw new Error('无效的 OTP URI');
    }

    const type = url.hostname; // totp 或 hotp
    const label = decodeURIComponent(url.pathname.slice(1));
    const params = Object.fromEntries(url.searchParams);

    // 解析 label
    let issuer = params.issuer || '';
    let account = label;

    if (label.includes(':')) {
      const parts = label.split(':');
      issuer = parts[0];
      account = parts.slice(1).join(':');
    }

    const result = {
      otp_type: type,
      issuer,
      account,
      secret: params.secret,
      algorithm: params.algorithm || 'SHA1',
      digits: parseInt(params.digits) || 6,
    };

    if (type === 'hotp') {
      result.counter = parseInt(params.counter) || 0;
    } else {
      result.period = parseInt(params.period) || 30;
    }

    return result;
  } catch (error) {
    console.error('[TOTP Service] 解析 URI 失败:', error.message);
    return null;
  }
}

/**
 * 递增 HOTP 计数器
 * @param {number} counter - 当前计数器
 * @returns {number} 新计数器值
 */
function incrementCounter(counter) {
  return (counter || 0) + 1;
}

module.exports = {
  generateCode,
  generateTotpCode,
  generateHotpCode,
  normalizeOtpAlgorithm,
  verifyTotpCode,
  verifyHotpCode,
  getRemainingSeconds,
  generateAllCodes,
  generateSecret,
  generateUri,
  parseUri,
  incrementCounter,
};
