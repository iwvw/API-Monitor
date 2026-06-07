import React from 'react';
import { createRoot } from 'react-dom/client';
import { Toasty } from '@cloudflare/kumo/components/toast';
import App from './App.jsx';
import GlobalDialogHost from './components/GlobalDialogHost.jsx';
import { kumoToastManager } from './modules/toast.js';
import '../css/app.css';
import '@fortawesome/fontawesome-free/css/all.min.css';
import 'simple-icons-font/font/simple-icons.min.css';
import '@xterm/xterm/css/xterm.css';

// 渲染 React 应用
const container = document.getElementById('root');
if (container) {
  const root = createRoot(container);
  root.render(
    <React.StrictMode>
      <Toasty toastManager={kumoToastManager}>
        <App />
        <GlobalDialogHost />
      </Toasty>
    </React.StrictMode>
  );

  setTimeout(() => {
    const loader = document.getElementById('app-loading');
    if (loader) {
      loader.style.opacity = '0';
      setTimeout(() => {
        loader.style.display = 'none';
      }, 300);
    }
  }, 200);
}
