/**
 * 加密工具模块
 * 使用 AES-256-GCM 加密算法
 */

const crypto = require('crypto');

// 从环境变量获取加密密钥，如果没有则使用默认密钥（生产环境必须设置）
const ENCRYPTION_KEY =
  process.env.ENCRYPTION_KEY || 'default-encryption-key-change-in-production-32bytes';

// 确保密钥长度为 32 字节（256 位）
const KEY = crypto.createHash('sha256').update(ENCRYPTION_KEY).digest();

/**
 * 加密文本
 * @param {string} text - 要加密的文本
 * @returns {string} 加密后的文本（格式：iv:authTag:encryptedData）
 */
function encrypt(text) {
  if (!text) return '';

  try {
    // 生成随机初始化向量
    const iv = crypto.randomBytes(16);

    // 创建加密器
    const cipher = crypto.createCipheriv('aes-256-gcm', KEY, iv);

    // 加密数据
    let encrypted = cipher.update(text, 'utf8', 'hex');
    encrypted += cipher.final('hex');

    // 获取认证标签
    const authTag = cipher.getAuthTag();

    // 返回格式：iv:authTag:encryptedData
    return `${iv.toString('hex')}:${authTag.toString('hex')}:${encrypted}`;
  } catch (error) {
    console.error('加密失败:', error);
    throw new Error('加密失败');
  }
}

/**
 * 解密文本
 * @param {string} encryptedText - 加密的文本（格式：iv:authTag:encryptedData）
 * @returns {string} 解密后的文本
 */
function decrypt(encryptedText) {
  if (!encryptedText) return '';

  try {
    // 分割加密数据
    const parts = encryptedText.split(':');
    if (parts.length !== 3) {
      throw new Error('加密数据格式错误');
    }

    const iv = Buffer.from(parts[0], 'hex');
    const authTag = Buffer.from(parts[1], 'hex');
    const encrypted = parts[2];

    // 创建解密器
    const decipher = crypto.createDecipheriv('aes-256-gcm', KEY, iv);
    decipher.setAuthTag(authTag);

    // 解密数据
    let decrypted = decipher.update(encrypted, 'hex', 'utf8');
    decrypted += decipher.final('utf8');

    return decrypted;
  } catch (error) {
    console.error('解密失败:', error);
    throw new Error('解密失败');
  }
}

/**
 * 测试加密解密功能
 */
function test() {
  const testText = 'Hello, World! 这是一个测试文本。';
  console.log('原始文本:', testText);

  const encrypted = encrypt(testText);
  console.log('加密后:', encrypted);

  const decrypted = decrypt(encrypted);
  console.log('解密后:', decrypted);

  console.log('测试结果:', testText === decrypted ? '✅ 通过' : '❌ 失败');
}

module.exports = {
  encrypt,
  decrypt,
  test,
};

// 如果直接运行此文件，则执行测试
if (require.main === module) {
  test();
}
