/**
 * API Monitor 2FA - Popup Logic (Bulletproof Version)
 */

window.addEventListener('DOMContentLoaded', () => {
    const mainEl = document.getElementById('main');
    const accountCountEl = document.getElementById('accountCount');
    const toastEl = document.getElementById('toast');
    const searchInput = document.getElementById('searchInput');
    const toggleMaster = document.getElementById('toggleMaster');
    const btnToggleFill = document.getElementById('btnToggleFill');
    const btnSettings = document.getElementById('btnSettings');

    let refreshInterval;
    let allAccounts = [];
    let state = {
        showFillButton: true,
        masterEnabled: true
    };

    function showToast(message) {
        if (!toastEl) return;
        toastEl.textContent = message || '已复制';
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
        const term = (searchInput?.value || '').toLowerCase();
        const filtered = accounts.filter(acc => 
            (acc.issuer || '').toLowerCase().includes(term) || (acc.account || '').toLowerCase().includes(term)
        );

        if (accountCountEl) accountCountEl.textContent = `(${filtered.length})`;
        if (filtered.length === 0) {
            mainEl.innerHTML = `<div style="padding: 40px; text-align: center; opacity: 0.5;">📭 ${term ? '未找到结果' : '暂无账号'}</div>`;
            return;
        }

        const groups = {};
        filtered.forEach(acc => {
            const issuer = acc.issuer || '其他';
            if (!groups[issuer]) groups[issuer] = [];
            groups[issuer].push(acc);
        });

        const sortedIssuers = Object.keys(groups).sort((a, b) => groups[b].length - groups[a].length || a.localeCompare(b));

        let html = '';
        sortedIssuers.forEach(issuer => {
            html += `<div class="group-header">${issuer}</div>`;
            html += groups[issuer].map(acc => `
                <div class="account-item" data-code="${acc.currentCode || ''}" title="点击复制">
                    <div class="account-info">
                        <span class="issuer">${acc.account || '未命名'}</span>
                        <span class="account-name">${acc.issuer || '其他'}</span>
                    </div>
                    <div class="code-container">
                        <div class="code">${formatCode(acc.currentCode)}</div>
                        <div class="account-progress"><div class="progress-bar" id="prog-${acc.id}"></div></div>
                    </div>
                </div>`).join('');
        });
        mainEl.innerHTML = html;

        mainEl.querySelectorAll('.account-item').forEach(item => {
            item.onclick = async () => {
                if (item.dataset.code && await copyToClipboard(item.dataset.code)) {
                    showToast();
                    setTimeout(() => window.close(), 800);
                }
            };
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

    async function loadAccounts(showLoading = true) {
        if (!mainEl) return;
        if (showLoading) mainEl.innerHTML = '<div style="padding: 40px; text-align: center;">同步中...</div>';
        chrome.runtime.sendMessage({ type: 'GET_ACCOUNTS' }, (response) => {
            if (!response || !response.success) {
                mainEl.innerHTML = `<div style="padding: 20px; text-align: center;"><p>${response?.error || '连接失败'}</p></div>`;
                return;
            }
            allAccounts = response.data || [];
            renderAccounts(allAccounts);
            if (!refreshInterval) {
                refreshInterval = setInterval(() => {
                    updateProgressBars();
                    if ((30 - (Math.floor(Date.now() / 1000) % 30)) === 30) loadAccounts(false);
                }, 1000);
            }
        });
    }

    function updateFillBtnUI() {
        if (!btnToggleFill) return;
        btnToggleFill.style.opacity = state.showFillButton ? '1' : '0.3';
        btnToggleFill.style.color = state.showFillButton ? 'var(--accent-color)' : 'var(--text-sub)';
    }

    // 初始化加载
    chrome.storage.sync.get(['masterEnabled', 'showFillButton'], (res) => {
        state.masterEnabled = res.masterEnabled !== false;
        state.showFillButton = res.showFillButton !== false;
        
        if (toggleMaster) toggleMaster.checked = state.masterEnabled;
        updateFillBtnUI();
        loadAccounts();
    });

    // 绑定事件
    if (toggleMaster) {
        toggleMaster.onchange = () => {
            state.masterEnabled = toggleMaster.checked;
            chrome.storage.sync.set({ masterEnabled: state.masterEnabled });
            showToast(state.masterEnabled ? '已开启全局识别' : '识别已全局禁用');
        };
    }

    if (btnToggleFill) {
        btnToggleFill.onclick = () => {
            state.showFillButton = !state.showFillButton;
            chrome.storage.sync.set({ showFillButton: state.showFillButton });
            updateFillBtnUI();
            showToast(state.showFillButton ? '图标已启用' : '图标已隐藏');
        };
    }

    if (searchInput) {
        searchInput.oninput = () => renderAccounts(allAccounts);
    }

    if (btnSettings) {
        btnSettings.onclick = () => chrome.runtime.openOptionsPage();
    }
});
