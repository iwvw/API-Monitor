import { Bot, FileText, History, Key } from '../../components/Icons.jsx';

export const tabs = [
  { value: 'routes', label: <span className="inline-flex items-center gap-1.5"><FileText className="h-3.5 w-3.5" />接口</span> },
  { value: 'ai', label: <span className="inline-flex items-center gap-1.5"><Bot className="h-3.5 w-3.5" />AI 接入</span> },
  { value: 'audit', label: <span className="inline-flex items-center gap-1.5"><History className="h-3.5 w-3.5" />调用审计</span> },
  { value: 'keys', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />密钥管理</span> },
];
