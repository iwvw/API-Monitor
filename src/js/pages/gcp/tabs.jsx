import { Globe, HardDrive, Key, Layers, PieChart, Server } from '../../components/Icons.jsx';

export const tabs = [
  { value: 'instances', label: <span className="inline-flex items-center gap-1.5"><Server className="h-3.5 w-3.5" />实例</span> },
  { value: 'disks', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="h-3.5 w-3.5" />磁盘</span> },
  { value: 'network', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />网络</span> },
  { value: 'storage', label: <span className="inline-flex items-center gap-1.5"><Layers className="h-3.5 w-3.5" />存储</span> },
  { value: 'billing', label: <span className="inline-flex items-center gap-1.5"><PieChart className="h-3.5 w-3.5" />费用</span> },
  { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />账号管理</span> },
];
