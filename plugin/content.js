/**
 * API Monitor 2FA - Content Script
 */

const serverUrl = '';
let responseServerUrl = '';
let showFillButton = true;
let allAccounts = [];

function isContextValid() { return typeof chrome !== 'undefined' && chrome.runtime && !!chrome.runtime.id; }

function is2FAInput(input) {
  if (input.dataset.apiMonitorIgnore) return false;
  if (input.type !== 'text' && input.type !== 'tel' && input.type !== 'number' && input.type !== 'password') return false;

  // 识别分段输入框 (格子)
  const isDigitBox = (input.maxLength === 1 || (input.size === 1 && !input.maxLength));
  if (isDigitBox) {
    const group = getDigitGroup(input);
    if (group.length >= 4) return true;
  }

  const hints = ['otp', '2fa', 'totp', 'code', 'verification', 'authenticator', 'token', 'mfa', '验证码', '验证'];
  const attrs = [input.name, input.id, input.placeholder, input.autocomplete, input.getAttribute('aria-label'), input.className].filter(Boolean).map(s => s.toLowerCase());
  return hints.some(h => attrs.some(a => a.includes(h))) || (parseInt(input.maxLength) >= 4 && parseInt(input.maxLength) <= 8);
}

function formatCode(code) {
  if (!code) return '------';
  return code.length === 6 ? code.substring(0, 3) + ' ' + code.substring(3) : code;
}

function getDigitGroup(input) {
  // 向上寻找共同祖先，最多找 3 层，尝试找到包含多个小格子的容器
  let p = input.parentElement;
  let bestGroup = [input];

  for (let depth = 0; depth < 3 && p; depth++) {
    const allInputs = Array.from(p.querySelectorAll('input')).filter(i => {
      const style = window.getComputedStyle(i);
      return style.display !== 'none' && style.visibility !== 'hidden' &&
        (i.type === 'text' || i.type === 'tel' || i.type === 'number' || i.type === 'password') &&
        !i.dataset.apiMonitorIgnore;
    });

    // 筛选出特征明显的“小格子”：maxLength 为 1，或者视觉上很窄
    const smallInputs = allInputs.filter(i => {
      const rect = i.getBoundingClientRect();
      return i.maxLength === 1 || i.size === 1 || (rect.width > 0 && rect.width < 60);
    });

    if (smallInputs.length >= 4 && smallInputs.length <= 12 && smallInputs.includes(input)) {
      // 检查这些小格子是否在视觉上大致水平排列（这是格子布局的特征）
      const firstRect = smallInputs[0].getBoundingClientRect();
      const lastRect = smallInputs[smallInputs.length - 1].getBoundingClientRect();
      const isHorizontal = Math.abs(firstRect.top - lastRect.top) < 20;

      if (isHorizontal) {
        bestGroup = smallInputs;
        break;
      }
    }
    p = p.parentElement;
  }
  return bestGroup;
}

function safeSendMessage(message, callback) {
  if (!isContextValid()) return;
  try {
    chrome.runtime.sendMessage(message, (r) => {
      if (chrome.runtime.lastError && chrome.runtime.lastError.message.includes('context invalidated')) return;
      if (callback) callback(r);
    });
  } catch (e) { }
}

function createFillButton(input) {
  const btn = document.createElement('button');
  btn.className = 'api-monitor-2fa-btn'; btn.innerHTML = '🔐';
  btn.title = '一键填充 2FA 验证码'; btn.type = 'button';
  btn.addEventListener('click', (e) => { e.preventDefault(); e.stopPropagation(); showCodePicker(input); });
  return btn;
}

async function showCodePicker(input) {
  if (!isContextValid()) { alert('扩展已更新，请刷新页面'); return; }
  document.querySelectorAll('.api-monitor-2fa-picker').forEach(el => el.remove());

  const picker = document.createElement('div');
  picker.className = 'api-monitor-2fa-picker';

  picker.innerHTML = `
    <div class="api-monitor-2fa-list-container" id="api-2fa-list">
      <div class="loading">正在加载验证码...</div>
    </div>
  `;

  const rect = input.getBoundingClientRect();
  picker.style.top = `${rect.bottom + window.scrollY + 8}px`; /* Increased offset from 6px to 8px */
  picker.style.left = `${rect.left + window.scrollX}px`;
  picker.style.width = `${rect.width}px`;
  picker.style.minWidth = '240px';
  document.body.appendChild(picker);

  const listCont = picker.querySelector('#api-2fa-list');

  safeSendMessage({ type: 'GET_ACCOUNTS', domain: window.location.hostname }, (response) => {
    if (!response || !response.success) { listCont.innerHTML = `<div class="error">${response?.error || '获取失败'}</div>`; return; }
    allAccounts = response.matched?.length > 0 ? response.matched : response.data;
    if (!allAccounts || allAccounts.length === 0) { listCont.innerHTML = '<div class="empty">📭 暂无账号</div>'; return; }

    renderPickerList(listCont, allAccounts, input);
  });

  const closeHandler = (e) => {
    if (!picker.contains(e.target) && e.target !== input) {
      picker.remove();
      document.removeEventListener('mousedown', closeHandler);
    }
  };
  setTimeout(() => document.addEventListener('mousedown', closeHandler), 10);
}

function renderPickerList(container, accounts, input) {
  const groups = {};
  accounts.forEach(acc => {
    const issuer = acc.issuer || '其他';
    if (!groups[issuer]) groups[issuer] = [];
    groups[issuer].push(acc);
  });

  const sortedIssuers = Object.keys(groups).sort((a, b) => {
    const countA = groups[a].length;
    const countB = groups[b].length;
    if (countB !== countA) return countB - countA;
    return a.localeCompare(b);
  });

  let html = '';
  sortedIssuers.forEach(issuer => {
    html += groups[issuer].map(acc => {
      return `
        <div class="account-item" data-code="${acc.currentCode || ''}">
          <div class="api-monitor-2fa-info">
            <div class="api-monitor-2fa-account">${acc.account || '未命名'}</div>
            <div class="api-monitor-2fa-issuer">${acc.issuer || '其他'}</div>
          </div>
          <div class="api-monitor-2fa-code-wrapper">
            <div class="api-monitor-2fa-code">${formatCode(acc.currentCode)}</div>
            <div class="api-monitor-2fa-progress-container"><div class="api-monitor-2fa-progress-bar" id="prog-${acc.id}"></div></div>
          </div>
        </div>`;
    }).join('');
  });

  container.innerHTML = html;

  container.querySelectorAll('.account-item').forEach(item => {
    item.addEventListener('click', () => {
      if (item.dataset.code) {
        const code = item.dataset.code;
        const group = getDigitGroup(input);

        if (group.length > 1) {
          // 分段填充
          const digits = code.replace(/\s/g, '').split('');
          group.forEach((el, idx) => {
            if (digits[idx] && el) {
              el.value = digits[idx];
              el.dispatchEvent(new Event('input', { bubbles: true }));
              el.dispatchEvent(new Event('change', { bubbles: true }));
            }
          });
          if (group[0]) group[0].focus();
        } else {
          // 普通填充
          input.dataset.justFilled = 'true';
          input.value = code;
          input.dispatchEvent(new Event('input', { bubbles: true }));
          input.dispatchEvent(new Event('change', { bubbles: true }));
          input.focus();
        }
      }
      document.querySelectorAll('.api-monitor-2fa-picker').forEach(p => p.remove());
    });
  });
  updateProgress(accounts);
}

let progressTimer;
function updateProgress(accounts) {
  if (progressTimer) clearInterval(progressTimer);
  const tick = () => {
    const rem = 30 - (Math.floor(Date.now() / 1000) % 30);
    accounts.forEach(acc => {
      const bar = document.getElementById(`prog-${acc.id}`);
      if (bar) { bar.style.width = `${(rem / 30) * 100}%`; bar.classList.toggle('low', rem <= 5); }
    });
  };
  tick(); progressTimer = setInterval(tick, 1000);
}

safeSendMessage({ type: 'GET_CONFIG' }, (config) => {
  if (config) {
    if (config.serverUrl) responseServerUrl = config.serverUrl.endsWith('/') ? config.serverUrl.slice(0, -1) : config.serverUrl;
    showFillButton = config.showFillButton !== false;
    if (showFillButton) scanInputs();
  }
});

chrome.storage.onChanged.addListener((changes) => {
  if (changes.showFillButton) {
    showFillButton = changes.showFillButton.newValue !== false;
    if (showFillButton) {
      scanInputs();
    } else {
      removeButtons();
    }
  }
});

function removeButtons() {
  document.querySelectorAll('.api-monitor-2fa-btn').forEach(btn => btn.remove());
  document.querySelectorAll('.api-monitor-2fa-picker').forEach(p => p.remove());
  document.querySelectorAll('input[data-api-monitor-2fa]').forEach(input => {
    delete input.dataset.apiMonitor2fa;
    // 如果有 wrapper，可以选择保留或移除。为了简单，我们主要控制按钮的显示。
  });
}

function scanInputs() {
  if (!isContextValid() || !showFillButton) return;
  document.querySelectorAll('input').forEach(input => {
    if (input.dataset.apiMonitor2fa || input.dataset.apiMonitorIgnore) return;
    if (is2FAInput(input)) {
      const group = getDigitGroup(input);
      if (group.length > 1 && group[0] !== input) {
        // 如果是格子组中的非第一个，标记忽略
        input.dataset.apiMonitorIgnore = 'true';
        return;
      }

      input.dataset.apiMonitor2fa = 'true';
      const wrapper = document.createElement('div'); wrapper.className = 'api-monitor-2fa-wrapper';
      input.parentNode.insertBefore(wrapper, input); wrapper.appendChild(input); wrapper.appendChild(createFillButton(input));

      input.addEventListener('focus', () => {
        if (input.dataset.justFilled === 'true') {
          input.dataset.justFilled = 'false';
          return;
        }
        setTimeout(() => showCodePicker(input), 150);
      });
    }
  });
}

const observer = new MutationObserver(() => isContextValid() ? scanInputs() : observer.disconnect());
scanInputs(); if (isContextValid()) observer.observe(document.body, { childList: true, subtree: true });
