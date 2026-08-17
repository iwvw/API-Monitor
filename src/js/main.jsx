import React from 'react';
import { createRoot } from 'react-dom/client';
import { Toasty } from '@cloudflare/kumo/components/toast';
import App from './App.jsx';
import GlobalDialogHost from './components/GlobalDialogHost.jsx';
import { setupPwa } from './modules/pwa.js';
import { kumoToastManager } from './modules/toast.js';
import { ensureSiteChartFont } from './chartFont.js';
import '../css/app.css';
import '@fortawesome/fontawesome-free/css/all.min.css';
import '../css/simple-icons.css';
import 'flag-icons/css/flag-icons.min.css';

setupPwa();
ensureSiteChartFont();

const container = document.getElementById('root');
if (container) {
  try {
    const root = createRoot(container);
    root.render(
      <React.StrictMode>
        <Toasty toastManager={kumoToastManager}>
          <App />
          <GlobalDialogHost />
        </Toasty>
      </React.StrictMode>
    );
    window.__API_MONITOR_BOOTED = true;

    setTimeout(() => {
      const loader = document.getElementById('app-loading');
      if (loader) {
        loader.style.opacity = '0';
        setTimeout(() => {
          loader.style.display = 'none';
        }, 300);
      }
    }, 200);
  } catch (error) {
    console.error('React app boot failed:', error);
    window.__API_MONITOR_BOOTED = false;
    if (typeof window.__API_MONITOR_SHOW_BOOT_ERROR === 'function') {
      window.__API_MONITOR_SHOW_BOOT_ERROR(error?.message || '应用启动失败');
    } else {
      const loader = document.getElementById('app-loading');
      if (loader) {
        loader.textContent = '启动失败，请刷新重试';
      }
    }
  }
} else if (typeof window.__API_MONITOR_SHOW_BOOT_ERROR === 'function') {
  window.__API_MONITOR_SHOW_BOOT_ERROR('缺少 root 挂载节点');
}
