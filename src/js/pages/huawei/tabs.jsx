// 华为云 Tab 定义：含图标 JSX，故为 .jsx。
import React from 'react';
import {
  Cloud,
  Globe,
  HardDrive,
  Key,
  Layers,
  PieChart,
  Server,
} from '../../components/Icons.jsx';

export const tabs = [
  { value: 'compute', label: <span className="inline-flex items-center gap-1.5"><Server className="h-3.5 w-3.5" />计算实例</span> },
  { value: 'flexus', label: <span className="inline-flex items-center gap-1.5"><Cloud className="h-3.5 w-3.5" />Flexus L</span> },
  { value: 'dns', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />域名解析</span> },
  { value: 'network', label: <span className="inline-flex items-center gap-1.5"><Layers className="h-3.5 w-3.5" />网络</span> },
  { value: 'storage', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="h-3.5 w-3.5" />存储</span> },
  { value: 'billing', label: <span className="inline-flex items-center gap-1.5"><PieChart className="h-3.5 w-3.5" />费用</span> },
  { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />账号管理</span> },
];
