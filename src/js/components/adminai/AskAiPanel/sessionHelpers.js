/* ---------- 会话来源判定与分组 ---------- */
// 机器人/自动化来源的会话（定时任务 cron、Telegram 频道 channel:*）由对应流程
// 管理上下文，用户在前端只能查看（只读），不能继续对话，避免污染机器人上下文。
export function isBotSession(session) {
  return !!session && !!session.source && session.source !== 'web';
}

export function sessionSourceLabel(source, channelType) {
  if (source === 'cron') return '任务';
  if (channelType === 'wechat') return '微信';
  if (channelType === 'telegram') return 'TG';
  if (channelType === 'wecom') return '企微';
  return 'BOT';
}

export function sessionActivityTime(s) {
  const value = s.lastActivityAt || s.createdAt;
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? 0 : d.getTime();
}

// 定时任务会话（source=cron）按会话标题（即任务名）合并成组，
// 组内按最近活跃倒序，组间按组内最新活跃倒序。
export function groupCronSessions(sessions) {
  const byTitle = new Map();
  for (const s of sessions) {
    const key = s.title || '未命名任务';
    if (!byTitle.has(key)) byTitle.set(key, []);
    byTitle.get(key).push(s);
  }
  const groups = [];
  for (const [title, items] of byTitle) {
    const sorted = [...items].sort((a, b) => sessionActivityTime(b) - sessionActivityTime(a));
    groups.push({ title, items: sorted, latestAt: sessionActivityTime(sorted[0]) });
  }
  groups.sort((a, b) => b.latestAt - a.latestAt);
  return groups;
}
