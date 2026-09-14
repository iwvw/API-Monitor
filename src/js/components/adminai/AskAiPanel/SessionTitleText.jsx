import React, { useEffect, useState } from 'react';

/* 会话标题：有标题直接显示；空会话按实时时间兜底，时钟只重渲染本组件
   （避免把每秒 setState 提到面板主组件导致整棵会话/消息树反复渲染） */
function SessionTitleText({ title, active }) {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    if (title || !active) return undefined;
    const t = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(t);
  }, [title, active]);
  return <>{title || (active ? new Date(now).toLocaleString('zh-CN') : '新对话')}</>;
}

export default SessionTitleText;

