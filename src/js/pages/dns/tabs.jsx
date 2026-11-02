import { Globe, Database, FileText, Layers, Lock, Settings, Terminal, Mail } from '../../components/Icons.jsx';

export const CLOUDFLARE_TABS = [
  { value: 'dns', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />DNS</span> },
  { value: 'workers', label: <span className="inline-flex items-center gap-1.5"><Terminal className="h-3.5 w-3.5" />Workers</span> },
  { value: 'pages', label: <span className="inline-flex items-center gap-1.5"><Layers className="h-3.5 w-3.5" />Pages</span> },
  { value: 'r2', label: <span className="inline-flex items-center gap-1.5"><Database className="h-3.5 w-3.5" />R2 存储</span> },
  { value: 'tunnels', label: <span className="inline-flex items-center gap-1.5"><Lock className="h-3.5 w-3.5" />Tunnel</span> },
  { value: 'email', label: <span className="inline-flex items-center gap-1.5"><Mail className="h-3.5 w-3.5" />邮件</span> },
  { value: 'templates', label: <span className="inline-flex items-center gap-1.5"><FileText className="h-3.5 w-3.5" />DNS 模板</span> },
  { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Settings className="h-3.5 w-3.5" />账号</span> },
];
