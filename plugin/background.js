/**
 * API Monitor 2FA - 后台服务
 * 处理与 API Monitor 后端的通信
 */

// 默认配置
const DEFAULT_CONFIG = {
  serverUrl: '',
  password: '',
  showFillButton: true,
  masterEnabled: true
};

// 获取配置
async function getConfig() {
  const result = await chrome.storage.sync.get(['serverUrl', 'password', 'showFillButton', 'masterEnabled']);
  return {
    serverUrl: result.serverUrl || DEFAULT_CONFIG.serverUrl,
    password: result.password || DEFAULT_CONFIG.password,
    showFillButton: result.showFillButton !== undefined ? result.showFillButton : DEFAULT_CONFIG.showFillButton,
    masterEnabled: result.masterEnabled !== undefined ? result.masterEnabled : DEFAULT_CONFIG.masterEnabled
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
  // 简单但更健壮的主域名提取 (处理 .com.cn 等常见多级后缀)
  let mainDomain = domainParts.slice(-2).join('.');
  if (domainParts.length >= 3 && ['com', 'net', 'org', 'edu', 'gov'].includes(domainParts[domainParts.length - 2])) {
      mainDomain = domainParts.slice(-3).join('.');
  }
  
  const searchTerms = [domain, mainDomain, ...domainParts.filter(p => p.length > 2 && !['com', 'net', 'org', 'www'].includes(p))];

  return accounts.filter(account => {
    const issuer = (account.issuer || '').toLowerCase().replace(/\s/g, '');
    const accountName = (account.account || '').toLowerCase();

    return searchTerms.some(term => 
        issuer.includes(term) || 
        term.includes(issuer) || 
        accountName.includes(term)
    );
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
