// 1Panel 连接表单的纯逻辑：与组件分离，便于在 node 环境单测。
//
// 安全约束（对应后端 createConfig 的语义）：API Key 在后端 SecureEncrypt 加密入库，
// listConfigs 只回 hasKey 布尔值。因此前端永远不应把密钥回填到输入框。

export const DEFAULT_ONEPANEL_BASE_URL = 'https://127.0.0.1:8888';

// 列出「尚未配置 1Panel 连接」的主机。已配置的主机不应再出现在新增下拉里，
// 否则用户会以为可以重复添加（后端实际是 upsert，会静默覆盖旧密钥）。
// probedIds 非空时只保留已由 Agent 探测到安装了 1Panel 的主机；
// 传 null/undefined 表示不做安装过滤。
export const getAvailableOnepanelServers = (servers = [], configs = [], probedIds = null) => {
  const configured = new Set((configs || []).map(c => String(c?.serverId ?? '')));
  const allow = probedIds ? new Set(Array.from(probedIds, id => String(id))) : null;
  return (servers || []).filter(s => {
    const id = String(s?.id ?? '');
    if (!id || configured.has(id)) return false;
    return allow ? allow.has(id) : true;
  });
};

// 表单校验：返回错误文案或空字符串。baseUrl 留空时回落到默认值。
export const validateOnepanelForm = (form = {}) => {
  if (!String(form.serverId || '').trim()) return '请选择主机';
  if (!String(form.apiKey || '').trim()) return '请填写 1Panel API Key';
  return '';
};

export const buildOnepanelPayload = (form = {}) => ({
  serverId: String(form.serverId || '').trim(),
  apiKey: String(form.apiKey || '').trim(),
  baseUrl: String(form.baseUrl || '').trim() || DEFAULT_ONEPANEL_BASE_URL,
});

// 主机显示名：优先「名称（host）」，缺失时退化为 id，避免出现空白行。
export const describeOnepanelServer = (server) => {
  const name = String(server?.name || '').trim();
  const host = String(server?.host || '').trim();
  if (name && host) return `${name}（${host}）`;
  return name || host || String(server?.id || '') || '未知主机';
};
