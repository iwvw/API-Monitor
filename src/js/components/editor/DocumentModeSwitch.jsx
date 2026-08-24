import React from 'react';
import { Tabs } from '@cloudflare/kumo';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { Eye, CodeFile, Columns } from '../Icons.jsx';

const MODE_OPTIONS = [
  {
    value: 'write',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Eye className="h-3.5 w-3.5" />
        <span className="hidden cq-sm:inline">编辑</span>
      </span>
    ),
  },
  {
    value: 'split',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Columns className="h-3.5 w-3.5" />
        <span className="hidden cq-sm:inline">分栏</span>
      </span>
    ),
  },
  {
    value: 'source',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <CodeFile className="h-3.5 w-3.5" />
        <span className="hidden cq-sm:inline">源码</span>
      </span>
    ),
  },
];

/**
 * 编辑模式切换器 — write / split / source 三模式。
 */
export default function DocumentModeSwitch({ mode, onModeChange }) {
  return (
    <Tabs
      {...TOOL_TABS_PROPS}
      value={mode}
      onValueChange={onModeChange}
      tabs={MODE_OPTIONS}
    />
  );
}
