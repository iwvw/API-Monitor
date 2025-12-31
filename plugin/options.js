/**
 * API Monitor 2FA - 设置页面逻辑
 */

const form = document.getElementById('settingsForm');
const serverUrlInput = document.getElementById('serverUrl');
const passwordInput = document.getElementById('password');
const messageEl = document.getElementById('message');

// 加载已保存的设置
chrome.storage.sync.get(['serverUrl', 'password'], (result) => {
  if (result.serverUrl) {
    serverUrlInput.value = result.serverUrl;
  }
  if (result.password) {
    passwordInput.value = result.password;
  }
});

// 显示消息
function showMessage(text, type) {
  messageEl.textContent = text;
  messageEl.className = 'message ' + type;
  messageEl.style.display = 'block';

  setTimeout(() => {
    messageEl.style.display = 'none';
  }, 3000);
}

// 保存设置
form.addEventListener('submit', async (e) => {
  e.preventDefault();

  const serverUrl = serverUrlInput.value.trim().replace(/\/$/, ''); // 移除末尾斜杠
  const password = passwordInput.value;

  if (!serverUrl) {
    showMessage('请输入服务器地址', 'error');
    return;
  }

  // 测试连接
  try {
    const response = await fetch(`${serverUrl}/api/totp/accounts`, {
      headers: {
        'Content-Type': 'application/json',
        'x-admin-password': password
      }
    });

    if (!response.ok) {
      showMessage('连接失败: ' + response.status, 'error');
      return;
    }

    const data = await response.json();
    if (!data.success) {
      showMessage('认证失败，请检查密码', 'error');
      return;
    }

    // 保存配置
    chrome.storage.sync.set({ serverUrl, password }, () => {
      showMessage('设置已保存！即将自动关闭...', 'success');
      setTimeout(() => {
        window.close();
      }, 1000);
    });

  } catch (error) {
    showMessage('网络错误: ' + error.message, 'error');
  }
});
