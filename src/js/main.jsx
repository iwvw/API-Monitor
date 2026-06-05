import React from 'react';
import { createRoot } from 'react-dom/client';
import App from './App.jsx';
import '../css/app.css';
import '@fortawesome/fontawesome-free/css/all.min.css';
import 'simple-icons-font/font/simple-icons.min.css';

// 渲染 React 应用
const container = document.getElementById('root');
if (container) {
  const root = createRoot(container);
  root.render(
    <React.StrictMode>
      <App />
    </React.StrictMode>
  );

  // 渲染完成后，淡出 Loading 加载屏
  setTimeout(() => {
    const loader = document.getElementById('app-loading');
    if (loader) {
      loader.style.opacity = '0';
      setTimeout(() => {
        loader.style.display = 'none';
      }, 300); // 等待淡出过渡完成
    }
  }, 200);
}
