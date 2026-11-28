import React from 'react';
import { createRoot } from 'react-dom/client';
import { Toasty } from '@cloudflare/kumo/components/toast';
import { ShikiProvider } from '@cloudflare/kumo/code';
import { KumoPortalProvider } from '@cloudflare/kumo/utils';
import App from './App.jsx';
import GlobalDialogHost from './components/GlobalDialogHost.jsx';
import { setupPwa } from './modules/pwa.js';
import { useOverlayPortalContainer } from './modules/overlayPortal.js';
import { kumoToastManager } from './modules/toast.js';
import { ensureSiteChartFont } from './chartFont.js';
import '../css/app.css';
import '@fortawesome/fontawesome-free/css/all.min.css';
import '../css/simple-icons.css';
import 'flag-icons/css/flag-icons.min.css';

setupPwa();
ensureSiteChartFont();

// 顶层浮层容器:Kumo 浮层默认 portal 到 document.body,会被 portal 到 body 的高 z-index
// 全屏层盖住(编辑弹窗、全局确认弹窗、Toast)。这里把「当前最上层全屏容器」下发给所有
// Kumo 浮层,使其落到全屏层内部;无全屏层时容器为 undefined,回落 body 默认行为。
function RootShell() {
  const overlayPortalContainer = useOverlayPortalContainer();
  return (
    <ShikiProvider
      engine="javascript"
      languages={['javascript', 'typescript', 'jsx', 'tsx', 'json', 'jsonc', 'html', 'css', 'python', 'yaml', 'markdown', 'graphql', 'sql', 'bash', 'diff', 'hcl', 'toml']}
      labels={{ copy: '复制代码', copied: '已复制' }}
    >
      <KumoPortalProvider container={overlayPortalContainer ?? undefined}>
        <Toasty toastManager={kumoToastManager}>
          <App />
          <GlobalDialogHost />
        </Toasty>
      </KumoPortalProvider>
    </ShikiProvider>
  );
}

const container = document.getElementById('root');
if (container) {
  try {
    const root = createRoot(container);
    root.render(
      <React.StrictMode>
        <RootShell />
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
        loader.textContent = '启动失败，刷新重试';
      }
    }
  }
} else if (typeof window.__API_MONITOR_SHOW_BOOT_ERROR === 'function') {
  window.__API_MONITOR_SHOW_BOOT_ERROR('缺少 root 挂载节点');
}
