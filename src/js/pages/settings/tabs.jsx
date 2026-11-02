import { Activity, Database, FileText, LayoutDashboard, Settings, Shield, Sun } from '../../components/Icons.jsx';

export const SETTINGS_TABS = [
  { value: 'general', label: <span className="inline-flex items-center gap-1.5"><LayoutDashboard className="h-4 w-4" />常规</span> },
  { value: 'modules', label: <span className="inline-flex items-center gap-1.5"><Activity className="h-4 w-4" />模块</span> },
  { value: 'security', label: <span className="inline-flex items-center gap-1.5"><Shield className="h-4 w-4" />安全</span> },
  { value: 'database', label: <span className="inline-flex items-center gap-1.5"><Database className="h-4 w-4" />数据库</span> },
  { value: 'logs', label: <span className="inline-flex items-center gap-1.5"><FileText className="h-4 w-4" />审计</span> },
  { value: 'appearance', label: <span className="inline-flex items-center gap-1.5"><Sun className="h-4 w-4" />外观</span> },
  { value: 'about', label: <span className="inline-flex items-center gap-1.5"><Settings className="h-4 w-4" />关于</span> },
];
