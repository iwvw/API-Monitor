import React from 'react';
import { Bot, ShieldCheck, Sliders, Database } from '../../Icons.jsx';

/* ==================== 设置页（多键表单） ==================== */

export const SETTING_FIELDS = [
  { key: 'admin_ai_enabled', kind: 'switch', group: 'basic', label: '管理 AI 总开关'},
  { key: 'admin_ai_default_model', kind: 'select', group: 'basic', label: '推理模型'},
  { key: 'admin_ai_reasoning_effort', kind: 'effort_select', group: 'basic', label: '思考强度'},
  { key: 'admin_ai_summary_model', kind: 'multi_select', group: 'basic', label: '摘要模型' },
  { key: 'admin_ai_briefing_model', kind: 'select', group: 'basic', label: '简报模型'},
  { key: 'admin_ai_write_enabled', kind: 'switch', group: 'security', label: '写操作全局开关'},
  { key: 'admin_ai_auto_approve', kind: 'switch', group: 'security', label: '完全批准模式' },
  { key: 'admin_ai_tool_call_limit', kind: 'number', group: 'runtime', label: '工具调用上限'},
  { key: 'admin_ai_timeout_seconds', kind: 'number', group: 'runtime', label: '执行超时（秒）' },
  { key: 'admin_ai_context_window', kind: 'number', group: 'runtime', label: '上下文窗口' },
  { key: 'admin_ai_memories_enabled', kind: 'switch', group: 'runtime', label: '长期记忆总开关'},
  { key: 'admin_ai_memories_model', kind: 'select', group: 'runtime', label: '记忆提炼模型' },
  { key: 'admin_ai_memories_bootstrap_chars', kind: 'number', group: 'runtime', label: '记忆注入上限' },
  { key: 'admin_ai_memories_auto_capture', kind: 'switch', group: 'runtime', label: '自动记忆提炼' },
  { key: 'admin_ai_memories_idle_minutes', kind: 'number', group: 'runtime', label: '提炼空闲分钟数' },
  { key: 'admin_ai_audit_retention_days', kind: 'number', group: 'retention', label: '审计保留天数' },
  { key: 'admin_ai_max_concurrent_runs', kind: 'number', group: 'runtime', label: '全局并发执行上限' },
];

// 思考强度选项（与后端 reasoningEffortValues 对齐；空值=不传，保持上游默认）。
export const REASONING_EFFORT_OPTIONS = [
  { value: '', label: '不指定（上游默认）' },
  { value: 'low', label: 'low（低）' },
  { value: 'medium', label: 'medium（中）' },
  { value: 'high', label: 'high（高）' },
];

export const SETTING_SECTIONS = [  { key: 'basic', title: '基础设置', description: '总开关与模型选择', icon: <Bot className="h-4 w-4 text-brand" /> },
  { key: 'security', title: '安全与审批', description: '写操作与审批策略', icon: <ShieldCheck className="h-4 w-4 text-brand" /> },
  { key: 'runtime', title: '运行参数', description: '工具调用上限与超时', icon: <Sliders className="h-4 w-4 text-brand" /> },
  { key: 'retention', title: '数据保留', description: '审计记录保留时长', icon: <Database className="h-4 w-4 text-brand" /> },
];

// 站点简报模板清单（与后端 briefingTemplatePrompts 保持一致）。
export const BRIEFING_TEMPLATE_OPTIONS = [
  { value: 'standard', label: '标准简报', description: '标题 + 关键指标小节（系统资源 / API 调用 / 可用性），突出异常与风险，全文 ≤ 400 字' },
  { value: 'brief', label: '简洁版', description: '一句话结论 + 关键指标与异常项目符号，全文 ≤ 150 字' },
  { value: 'detailed', label: '详细版', description: '摘要 / 分节指标 / 风险建议，全文 ≤ 800 字' },
  { value: 'alert_only', label: '仅异常', description: '只报告异常与风险（按严重度排序）；一切正常时仅输出一句"一切正常"' },
  { value: 'custom', label: '自定义', description: '内容直接作为格式指令注入' },
];
