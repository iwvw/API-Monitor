import React from 'react';
import { Cloud, Folder, Globe, Key, Users } from '../../components/Icons.jsx';

export const M365_TABS = [
  {
    value: 'tenants',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Cloud className="h-4 w-4" />
        租户
      </span>
    ),
  },
  {
    value: 'users',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Users className="h-4 w-4" />
        用户与许可证
      </span>
    ),
  },
  {
    value: 'groups',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Folder className="h-4 w-4" />组
      </span>
    ),
  },
  {
    value: 'public',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Globe className="h-4 w-4" />
        公开页
      </span>
    ),
  },
];

export const M365_PUBLIC_TABS = [
  { value: 'pages', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />公开页配置</span> },
  { value: 'codes', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />邀请码批次</span> },
  { value: 'registrations', label: <span className="inline-flex items-center gap-1.5"><Users className="h-3.5 w-3.5" />注册记录</span> },
];
