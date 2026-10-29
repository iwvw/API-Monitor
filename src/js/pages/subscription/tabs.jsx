import { Box, FileText, Globe, Plug, Server } from '../../components/Icons.jsx';

export const tabs = [
  { value: 'instances', label: <span className="inline-flex items-center gap-1.5"><Server className="h-3.5 w-3.5" />实例管理</span> },
  { value: 'nodes', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />节点管理</span> },
  { value: 'plans', label: <span className="inline-flex items-center gap-1.5"><Box className="h-3.5 w-3.5" />套餐管理</span> },
  { value: 'subscriptions', label: <span className="inline-flex items-center gap-1.5"><Plug className="h-3.5 w-3.5" />订阅管理</span> },
  { value: 'templates', label: <span className="inline-flex items-center gap-1.5"><FileText className="h-3.5 w-3.5" />模板管理</span> },
];
