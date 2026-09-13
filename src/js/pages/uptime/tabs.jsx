import { Activity, Globe, Plus, Shield, Upload } from '../../components/Icons.jsx';

export const uptimeTabs = [
  { value: 'list', label: <span className="inline-flex items-center gap-1.5"><Activity className="w-3.5 h-3.5" />仪表盘</span> },
  { value: 'add', label: <span className="inline-flex items-center gap-1.5"><Plus className="w-3.5 h-3.5" />添加监测</span> },
  { value: 'status-pages', label: <span className="inline-flex items-center gap-1.5"><Globe className="w-3.5 h-3.5" />状态页</span> },
  { value: 'maintenance', label: <span className="inline-flex items-center gap-1.5"><Shield className="w-3.5 h-3.5" />维护窗口</span> },
  { value: 'stats', label: <span className="inline-flex items-center gap-1.5"><Upload className="w-3.5 h-3.5" />配置迁移</span> },
];
