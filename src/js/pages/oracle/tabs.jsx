import { Globe, HardDrive, Key, PieChart, Server, Terminal } from '../../components/Icons.jsx';

export const tabs = [
  { value: 'instances', label: <span className="inline-flex items-center gap-1.5"><Server className="h-3.5 w-3.5" />实例</span> },
  { value: 'network', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />网络</span> },
  { value: 'storage', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="h-3.5 w-3.5" />卷</span> },
  { value: 'console', label: <span className="inline-flex items-center gap-1.5"><Terminal className="h-3.5 w-3.5" />控制台</span> },
  { value: 'cost', label: <span className="inline-flex items-center gap-1.5"><PieChart className="h-3.5 w-3.5" />成本</span> },
  { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />账号管理</span> },
];
