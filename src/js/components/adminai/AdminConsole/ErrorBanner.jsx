import React from 'react';

/* ==================== 通用小组件 ==================== */

function ErrorBanner({ message }) {
  if (!message) return null;
  return (
    <div className="rounded-lg bg-kumo-danger/10 px-3 py-2 text-xs text-kumo-danger">{message}</div>
  );
}

export default ErrorBanner;
