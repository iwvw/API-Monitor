import React from 'react';
import { GitBranch, PlayCircle, TrendingUp, Bell, Globe, Settings } from '../../components/Icons.jsx';

const tabs = [
  { value: 'repositories', label: <span className="inline-flex items-center gap-1.5"><GitBranch className="h-3.5 w-3.5" />仓库</span> },
  { value: 'actions', label: <span className="inline-flex items-center gap-1.5"><PlayCircle className="h-3.5 w-3.5" />Actions</span> },
  { value: 'trends', label: <span className="inline-flex items-center gap-1.5"><TrendingUp className="h-3.5 w-3.5" />趋势</span> },
  { value: 'events', label: <span className="inline-flex items-center gap-1.5"><Bell className="h-3.5 w-3.5" />事件</span> },
  { value: 'public-pages', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />公开页</span> },
  { value: 'settings', label: <span className="inline-flex items-center gap-1.5"><Settings className="h-3.5 w-3.5" />设置</span> },
];

export default tabs;