import { FileText, FolderOpen, History, Send, Settings, Users } from '../../components/Icons.jsx';

export const SHARE_TYPE_TABS = [
  {
    value: 'file',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <FolderOpen className="h-3.5 w-3.5" />
        文件
      </span>
    ),
  },
  {
    value: 'text',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <FileText className="h-3.5 w-3.5" />
        文本
      </span>
    ),
  },
];

export const PAGE_TABS = [
  {
    value: 'share',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Send className="h-3.5 w-3.5" />
        创建分享
      </span>
    ),
  },
  {
    value: 'void',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Users className="h-3.5 w-3.5" />
        虚空房间
      </span>
    ),
  },
  {
    value: 'history',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <History className="h-3.5 w-3.5" />
        分享记录
      </span>
    ),
  },
  {
    value: 'settings',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Settings className="h-3.5 w-3.5" />
        策略
      </span>
    ),
  },
];
