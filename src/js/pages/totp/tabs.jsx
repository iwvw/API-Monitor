import React from 'react';
import { FolderOpen, Key, Settings } from '../../components/Icons.jsx';

export const TOTP_TABS = [
  {
    value: 'accounts',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Key className="w-3.5 h-3.5" />
        验证码
      </span>
    ),
  },
  {
    value: 'groups',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <FolderOpen className="w-3.5 h-3.5" />
        分组
      </span>
    ),
  },
  {
    value: 'settings',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Settings className="w-3.5 h-3.5" />
        设置
      </span>
    ),
  },
];
