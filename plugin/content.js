/**
 * API Monitor 2FA - Content Script
 */

const serverUrl = '';
let responseServerUrl = '';
let showFillButton = false; // 默认关闭，等待配置加载
let masterEnabled = true;   // 默认开启
let allAccounts = [];

function isContextValid() { return typeof chrome !== 'undefined' && chrome.runtime && !!chrome.runtime.id; }

function is2FAInput(target) {
  if (target.dataset.apiMonitorIgnore) return false;
  
  // 识别基础：支持 INPUT 和特定自定义组件 (如华为云的 div)
  const isInput = target.tagName === 'INPUT' && (target.type === 'text' || target.type === 'tel' || target.type === 'number' || target.type === 'password');
  const isCustomBox = target.tagName === 'DIV' && (target.className.includes('hwid-input-area') || target.className.includes('sixInputArea'));

  if (!isInput && !isCustomBox) return false;

  const attrs = [target.name, target.id, target.placeholder, target.autocomplete, target.getAttribute('aria-label'), target.className].filter(Boolean).map(s => s.toLowerCase());
  
  // 排除规则
  if (target.autocomplete === 'current-password' || target.autocomplete === 'new-password') return false;
  const ignoreHints = ['search', 'user', 'mail', 'phone', 'captcha', 'username', 'login', 'signin'];
  if (ignoreHints.some(h => attrs.some(a => a.includes(h)))) return false;

  // 容器特征扫描 (向上溯源)
  let p = target.parentElement;
  for(let i=0; i<4 && p; i++) {
    const pc = p.className.toLowerCase();
    const pi = p.id.toLowerCase();
    if (pc.includes('mfa') || pc.includes('2fa') || pc.includes('sixinputarea') || pc.includes('otp-container') || pi.includes('mfa')) {
      return true; 
    }
    p = p.parentElement;
  }

  if (target.autocomplete === 'one-time-code') return true;

  const isDigitBox = (target.maxLength === 1 || (target.size === 1 && !target.maxLength) || isCustomBox);
  if (isDigitBox) {
    const group = getDigitGroup(target);
    if (group.length >= 4) return true;
  }

  const strongHints = ['otp', '2fa', 'totp', 'mfa', 'authenticator', 'token', '验证码', '两步验证', '动态码', '动态口令', '安全令牌'];
  if (strongHints.some(h => attrs.some(a => a.includes(h)))) return true;

  const weakHints = ['code', 'verification', 'verify', 'pin', 'password', '验证', '校验', '口令'];
  const hasWeakHint = weakHints.some(h => attrs.some(a => a.includes(h)));
  if (hasWeakHint) {
    const maxLen = parseInt(target.maxLength);
    if (maxLen >= 4 && maxLen <= 10) return true;
    if (target.inputMode === 'numeric' || target.type === 'number') return true;
    if (attrs.some(a => a.includes('verification') || a.includes('verify'))) return true;
  }

  return false;
}

function formatCode(code) {
  if (!code) return '------';
  return code.length === 6 ? code.substring(0, 3) + ' ' + code.substring(3) : code;
}

function getDigitGroup(input) {
  if (!input || !input.parentElement) return [];
  let bestGroup = [input];
  let p = input.parentElement;
  let depth = 0;

  while (p && depth < 5) {
    const smallInputs = Array.from(p.querySelectorAll('input, div.hwid-input-area, div.sixInputArea')).filter(el => {
      const isShort = el.offsetWidth > 0 && el.offsetWidth < 66;
      const isSingleChar = el.maxLength === 1 || (el.size === 1 && !el.maxLength) || el.className.includes('hwid-input-area');
      return (isShort || isSingleChar);
    });

    if (smallInputs.length >= 4 && smallInputs.length <= 12 && smallInputs.includes(input)) {
      const firstRect = smallInputs[0].getBoundingClientRect();
      const lastRect = smallInputs[smallInputs.length - 1].getBoundingClientRect();
      if (Math.abs(firstRect.top - lastRect.top) < 40) {
        bestGroup = smallInputs;
        break;
      }
    }
    p = p.parentElement;
    depth++;
  }
  return bestGroup;
}

function safeSendMessage(message, callback) {
  if (!isContextValid()) return;
  try {
    chrome.runtime.sendMessage(message, (r) => {
      if (chrome.runtime.lastError) return;
      if (callback) callback(r);
    });
  } catch (e) { }
}

function createFillButton(input) {
  if (input.dataset.btnAdded === 'true') return;
  const btn = document.createElement('button');
  btn.className = 'api-monitor-2fa-btn';
  btn.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg>`;
  btn.title = '一键填充 2FA 验证码';
  btn.type = 'button';
  
  const align = () => {
    if (!document.body.contains(input)) { btn.remove(); return; }
    const rect = input.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0 || window.getComputedStyle(input).display === 'none') {
      btn.style.display = 'none'; return;
    }
    btn.style.display = 'flex';
    btn.style.top = `${rect.top + window.scrollY + (rect.height - 24) / 2}px`;
    btn.style.left = `${rect.left + window.scrollX + rect.width - 32}px`;
    btn.classList.add('visible');
  };

  btn.addEventListener('click', (e) => { e.preventDefault(); e.stopPropagation(); showCodePicker(input); });
  document.body.appendChild(btn);
  input.dataset.btnAdded = 'true';
  align();

  window.addEventListener('resize', align, { passive: true });
  document.addEventListener('scroll', align, { capture: true, passive: true });
  
  const timer = setInterval(() => {
    if (!document.body.contains(input)) { btn.remove(); clearInterval(timer); } 
    else { align(); }
  }, 1000);
}

async function showCodePicker(input) {
  if (!isContextValid() || !masterEnabled) return; // 如果总开关关闭，拒绝显示面板
  document.querySelectorAll('.api-monitor-2fa-picker').forEach(el => el.remove());

  const picker = document.createElement('div');
  picker.className = 'api-monitor-2fa-picker';
  picker.innerHTML = `<div class="api-monitor-2fa-list-container" id="api-2fa-list"><div class="loading">加载中...</div></div>`;
  document.body.appendChild(picker);
  
  const rect = input.getBoundingClientRect();
  const pickerHeight = 280;
  const spaceBelow = window.innerHeight - rect.bottom;
  
  if (spaceBelow < pickerHeight && rect.top > spaceBelow) {
    picker.style.bottom = `${window.innerHeight - rect.top - window.scrollY + 8}px`;
    picker.style.top = 'auto';
    picker.classList.add('pop-up');
  } else {
    picker.style.top = `${rect.bottom + window.scrollY + 8}px`;
  }
  picker.style.left = `${Math.max(16, Math.min(rect.left + window.scrollX, window.innerWidth - 260))}px`;
  picker.style.width = `${Math.max(rect.width, 240)}px`;
  
  const listCont = picker.querySelector('#api-2fa-list');
  safeSendMessage({ type: 'GET_ACCOUNTS', domain: window.location.hostname }, (response) => {
    if (!response || !response.success) { 
      listCont.innerHTML = `<div class="error"><p>${response?.error || '获取失败'}</p></div>`; return; 
    }
    allAccounts = response.matched?.length > 0 ? response.matched : response.data;
    if (!allAccounts || allAccounts.length === 0) { 
      listCont.innerHTML = '<div class="empty">📭 暂无账号</div>'; return; 
    }
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
  container.innerHTML = accounts.map(acc => `
    <div class="account-item" data-code="${acc.currentCode || ''}">
      <div class="api-monitor-2fa-info">
        <div class="api-monitor-2fa-account">${acc.account || '未命名'}</div>
        <div class="api-monitor-2fa-issuer">${acc.issuer || '其他'}</div>
      </div>
      <div class="api-monitor-2fa-code-wrapper">
        <div class="api-monitor-2fa-code">${formatCode(acc.currentCode)}</div>
        <div class="api-monitor-2fa-progress-container"><div class="api-monitor-2fa-progress-bar" id="prog-${acc.id}"></div></div>
      </div>
    </div>`).join('');

  container.querySelectorAll('.account-item').forEach(item => {
    item.addEventListener('click', () => {
      const code = item.dataset.code;
      const group = getDigitGroup(input);
      if (group.length > 1) {
        const digits = code.replace(/\s/g, '').split('');
        group.forEach((el, idx) => {
          if (digits[idx]) {
            if (el.tagName === 'INPUT') el.value = digits[idx];
            else el.textContent = digits[idx];
            el.dispatchEvent(new Event('input', { bubbles: true }));
          }
        });
      } else {
        input.dataset.justFilled = 'true';
        if (input.tagName === 'INPUT') input.value = code;
        else input.textContent = code;
        input.dispatchEvent(new Event('input', { bubbles: true }));
      }
      container.closest('.api-monitor-2fa-picker').remove();
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
      if (bar) {
        bar.style.width = `${(rem / 30) * 100}%`;
        bar.classList.toggle('low', rem <= 5);
      }
    });
    if (rem === 30) {
       // 周期到，通知 background 刷新一下（虽然 content 目前没法直接刷 data，但至少能保住视觉同步）
    }
  };
  tick();
  progressTimer = setInterval(tick, 1000);
}

function scanInputs() {
  if (!isContextValid()) return;
  document.querySelectorAll('input, div.hwid-input-area, div.sixInputArea').forEach(input => {
    if (input.dataset.scanAdded === 'true') return;
    if (is2FAInput(input)) {
      input.dataset.scanAdded = 'true';
      const group = getDigitGroup(input);
      if (group.length > 1 && group[0] !== input) { input.dataset.apiMonitorIgnore = 'true'; return; }
      
      input.dataset.apiMonitor2fa = 'true';
      if (showFillButton) createFillButton(input);

      input.addEventListener('focus', () => {
        if (input.dataset.justFilled === 'true') { input.dataset.justFilled = 'false'; return; }
        setTimeout(() => showCodePicker(input), 150);
      });
      if (input.tagName === 'DIV') {
        input.style.cursor = 'pointer';
        input.addEventListener('click', () => showCodePicker(input));
      }
    }
  });
}

function removeButtons() {
  document.querySelectorAll('.api-monitor-2fa-btn').forEach(btn => btn.remove());
  document.querySelectorAll('input, div').forEach(el => delete el.dataset.btnAdded);
}

function removeAll() {
  removeButtons();
  document.querySelectorAll('.api-monitor-2fa-picker').forEach(p => p.remove());
  document.querySelectorAll('input, div').forEach(el => {
    delete el.dataset.scanAdded;
    delete el.dataset.apiMonitor2fa;
  });
}

safeSendMessage({ type: 'GET_CONFIG' }, (config) => {
  if (config) {
    if (config.serverUrl) responseServerUrl = config.serverUrl.endsWith('/') ? config.serverUrl.slice(0, -1) : config.serverUrl;
    showFillButton = config.showFillButton !== false;
    masterEnabled = config.masterEnabled !== false;
    
    if (!masterEnabled) {
      removeAll();
    } else if (showFillButton) {
      scanInputs();
    } else {
      removeButtons();
    }
  }
});

chrome.storage.onChanged.addListener((changes) => {
  if (changes.masterEnabled) {
    masterEnabled = changes.masterEnabled.newValue !== false;
    if (!masterEnabled) removeAll(); else scanInputs();
  }
  if (changes.showFillButton) {
    showFillButton = changes.showFillButton.newValue !== false;
    if (masterEnabled) {
      if (showFillButton) scanInputs(); else removeButtons();
    }
  }
});

const observer = new MutationObserver(() => isContextValid() && masterEnabled && scanInputs());
if (isContextValid()) observer.observe(document.body, { childList: true, subtree: true });
scanInputs();

// --- 核心功能：主站一键同步配置 ---
window.addEventListener('message', (event) => {
  // 安全校验：只接受来自主站的消息（这里逻辑可以根据主站域名加固）
  if (event.data && event.data.type === 'API_MONITOR_SYNC_CONFIG') {
    const { serverUrl, password } = event.data;
    if (serverUrl) {
      const cleanUrl = serverUrl.endsWith('/') ? serverUrl.slice(0, -1) : serverUrl;
      chrome.storage.sync.set({ 
        serverUrl: cleanUrl, 
        password: password || '' 
      }, () => {
        // 同步成功后通知主站显示成功状态
        window.postMessage({ type: 'API_MONITOR_SYNC_SUCCESS' }, '*');
        // 同时立即更新当前页面的变量
        responseServerUrl = cleanUrl;
        console.log('API Monitor: 配置已自动同步');
      });
    }
  }
});
