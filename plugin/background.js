/**
 * API Monitor 2FA - 后台服务
 * 处理与 API Monitor 后端的通信
 */

// 默认配置
const DEFAULT_CONFIG = {
  serverUrl: '',
  password: ''
};

// 获取配置
async function getConfig() {
  const result = await chrome.storage.sync.get(['serverUrl', 'password']);
  return {
    serverUrl: result.serverUrl || DEFAULT_CONFIG.serverUrl,
    password: result.password || DEFAULT_CONFIG.password
  };
}

// 获取 TOTP 账号列表（带实时验证码）
async function fetchTotpAccounts() {
  const config = await getConfig();
  if (!config.serverUrl) {
    return { success: false, error: '请先配置服务器地址' };
  }

  try {
    const response = await fetch(`${config.serverUrl}/api/totp/accounts?withCodes=true`, {
      headers: {
        'Content-Type': 'application/json',
        'x-admin-password': config.password
      }
    });

    if (!response.ok) {
      return { success: false, error: '请求失败: ' + response.status };
    }

    const data = await response.json();
    return data;
  } catch (error) {
    return { success: false, error: '网络错误: ' + error.message };
  }
}

// 根据域名匹配账号
function matchAccountsByDomain(accounts, domain) {
  if (!accounts || !domain) return [];

  const domainParts = domain.toLowerCase().split('.');
  const mainDomain = domainParts.slice(-2).join('.'); // 获取主域名

  return accounts.filter(account => {
    const issuer = (account.issuer || '').toLowerCase();
    const accountName = (account.account || '').toLowerCase();

    // 匹配 issuer 或 account 中包含域名
    return issuer.includes(mainDomain) ||
            accountName.includes(mainDomain) ||
            mainDomain.includes(issuer.replace(/\s/g, ''));
  });
}

// 监听来自 content script 和 popup 的消息
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === 'GET_ACCOUNTS') {
    fetchTotpAccounts().then(result => {
      if (result.success && message.domain) {
        result.matched = matchAccountsByDomain(result.data, message.domain);
      }
      sendResponse(result);
    });
    return true; // 保持消息通道开放
  }

  if (message.type === 'GET_CONFIG') {
    getConfig().then(sendResponse);
    return true;
  }
});
