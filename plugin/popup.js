/**
 * API Monitor 2FA - Popup Logic
 */

const mainEl = document.getElementById('main');
const accountCountEl = document.getElementById('accountCount');
const toastEl = document.getElementById('toast');
const searchInput = document.getElementById('searchInput');
const datalist = document.getElementById('issuerList');
const toggleFillButton = document.getElementById('toggleFillButton');

let refreshInterval;
let serverUrl = '';
let allAccounts = [];
let currentFilter = '';

function showToast(message) {
  toastEl.textContent = message || '复制成功';
  toastEl.classList.add('show');
  setTimeout(() => toastEl.classList.remove('show'), 2000);
}

async function copyToClipboard(text) {
  try { await navigator.clipboard.writeText(text); return true; } catch {
    const input = document.createElement('input'); input.value = text;
    document.body.appendChild(input); input.select();
    const success = document.execCommand('copy'); document.body.removeChild(input);
    return success;
  }
}

function formatCode(code) {
  if (!code) return '------';
  return code.length === 6 ? code.substring(0, 3) + ' ' + code.substring(3) : code;
}

function renderAccounts(accounts) {
  const filtered = accounts.filter(acc => {
    const term = currentFilter.toLowerCase();
    return (acc.issuer || '').toLowerCase().includes(term) || (acc.account || '').toLowerCase().includes(term);
  });

  accountCountEl.textContent = `(${filtered.length})`;
  if (filtered.length === 0) {
    mainEl.innerHTML = `<div class="empty">📭 ${currentFilter ? '未找到相关账号' : '暂无 2FA 账号'}</div>`;
    return;
  }

  // 按厂商（Issuer）分组
  const groups = {};
  filtered.forEach(acc => {
    const issuer = acc.issuer || '其他';
    if (!groups[issuer]) groups[issuer] = [];
    groups[issuer].push(acc);
  });

  // 排序厂商：按数量降序排列，数量一样则按名称排序
  const sortedIssuers = Object.keys(groups).sort((a, b) => {
    const countA = groups[a].length;
    const countB = groups[b].length;
    if (countB !== countA) return countB - countA;
    return a.localeCompare(b);
  });

  let html = '<div class="account-list">';
  sortedIssuers.forEach(issuer => {
    html += `<div class="group-header">${issuer}</div>`;
    html += groups[issuer].map(acc => {
      return `
        <div class="account-item no-icon" data-id="${acc.id}" data-code="${acc.currentCode || ''}" title="点击复制验证码">
          <div class="account-info">
            <span class="issuer" style="font-weight: 600;">${acc.account || '未命名'}</span>
            <span class="account-name" style="font-size: 11px; opacity: 0.6;">${acc.issuer || '其他'}</span>
          </div>
          <div class="code-container">
            <div class="code">${formatCode(acc.currentCode)}</div>
            <div class="account-progress"><div class="progress-bar" id="progress-${acc.id}"></div></div>
          </div>
        </div>
      `;
    }).join('');
  });
  html += '</div>';
  mainEl.innerHTML = html;

  document.querySelectorAll('.account-item').forEach(item => {
    item.addEventListener('click', async () => {
      const code = item.dataset.code;
      if (code && await copyToClipboard(code)) {
        showToast();
        setTimeout(() => window.close(), 800);
      }
    });
  });
  updateProgressBars();
}

function updateProgressBars() {
  const rem = 30 - (Math.floor(Date.now() / 1000) % 30);
  document.querySelectorAll('.progress-bar').forEach(bar => {
    bar.style.width = `${(rem / 30) * 100}%`;
    bar.classList.toggle('low', rem <= 5);
  });
}

function startTimer() {
  if (refreshInterval) clearInterval(refreshInterval);
  const update = () => {
    updateProgressBars();
    if (30 - (Math.floor(Date.now() / 1000) % 30) === 30) loadAccounts(false);
  };
  update();
  refreshInterval = setInterval(update, 1000);
}

async function loadAccounts(showLoading = true) {
  if (showLoading) mainEl.innerHTML = '<div class="loading"><div class="spinner"></div><p>正在同步数据...</p></div>';
  chrome.runtime.sendMessage({ type: 'GET_ACCOUNTS' }, (response) => {
    if (chrome.runtime.lastError) { mainEl.innerHTML = '<div class="error">无法连接至扩展后台</div>'; return; }
    if (!response || !response.success) {
      mainEl.innerHTML = `<div class="error"><p>${response?.error || '同步失败'}</p><button class="retry-btn" id="goSettings">前往配置</button></div>`;
      document.getElementById('goSettings')?.addEventListener('click', () => chrome.runtime.openOptionsPage());
      return;
    }
    allAccounts = response.data || [];
    updateSearchDatalist(allAccounts);
    renderAccounts(allAccounts);
    if (showLoading) startTimer();
  });
}

function updateSearchDatalist(accounts) {
  if (!datalist) return;
  const issuers = [...new Set(accounts.map(acc => acc.issuer || '其他'))].sort((a, b) => {
    const countA = accounts.filter(x => (x.issuer || '其他') === a).length;
    const countB = accounts.filter(x => (x.issuer || '其他') === b).length;
    return countB - countA;
  });

  datalist.innerHTML = issuers.map(iss => `<div class="api-monitor-2fa-suggestion-item">${iss}</div>`).join('');

  datalist.querySelectorAll('.api-monitor-2fa-suggestion-item').forEach(item => {
    item.onclick = (e) => {
      e.stopPropagation();
      searchInput.value = item.textContent;
      currentFilter = item.textContent;
      renderAccounts(allAccounts);
      datalist.style.display = 'none';
    };
  });

  searchInput.onfocus = () => { datalist.style.display = 'block'; };
  searchInput.onclick = () => { datalist.style.display = 'block'; };

  const hideDropdown = (e) => {
    if (datalist && !datalist.contains(e.target) && e.target !== searchInput) {
      datalist.style.display = 'none';
    }
  };
  document.addEventListener('mousedown', hideDropdown);
}

searchInput.addEventListener('input', (e) => {
  currentFilter = e.target.value;
  renderAccounts(allAccounts);
  if (datalist) datalist.style.display = 'none';
});

chrome.runtime.sendMessage({ type: 'GET_CONFIG' }, (config) => {
  if (config) {
    if (config.serverUrl) serverUrl = config.serverUrl.endsWith('/') ? config.serverUrl.slice(0, -1) : config.serverUrl;
    toggleFillButton.checked = config.showFillButton !== false;
  }
  loadAccounts();
});

toggleFillButton.addEventListener('change', () => {
  chrome.storage.sync.set({ showFillButton: toggleFillButton.checked });
});

document.getElementById('btnSettings').addEventListener('click', () => chrome.runtime.openOptionsPage());
