import React from 'react';
import {
  ArrowClockwise,
  ArrowSquareOut,
  ArrowsOutCardinal,
  BookmarkSimple,
  CaretDown,
  Check as PhCheck,
  Clock as PhClock,
  Copy as PhCopy,
  DotsSixVertical,
  DownloadSimple,
  Eye as PhEye,
  FileCode,
  FileText as PhFileText,
  Folder as PhFolder,
  FolderOpen as PhFolderOpen,
  GitBranch as PhGitBranch,
  GlobeHemisphereWest,
  HardDrives,
  House,
  List,
  MagnifyingGlass,
  Minus as PhMinus,
  PaperPlaneTilt,
  PencilSimple,
  Plugs,
  Plus as PhPlus,
  Pulse,
  Trash as PhTrash,
  TrendUp,
  UploadSimple,
  User as PhUser,
} from '@phosphor-icons/react';
import { createFontIcon, GitHubBrand } from './IconsCore.jsx';

// 公开页专用图标集。
//
// 公开页(公开书签/分享/提示词/状态/订阅/GitHub 页)历史上直接从 Icons.jsx 取图标;
// 但 Icons.jsx 会被打包成一个模块,内含**整个后台**用到的全部图标(实测约 352KB),
// 于是每个公开页首屏都被迫下载这一大坨。这里只落地公开页真正需要的一小撮图标,
// 公开页改从这里导入,Icons.jsx 便不再进入公开页的关键路径。
//
// createIcon 与 Icons.jsx 保持一致,保证图标外观(尺寸/权重/aria/app-icon 类)不变。

const createIcon = (Icon) => {
  const AppIcon = ({
    className = '',
    size = 24,
    weight = 'regular',
    mirrored = false,
    ...props
  }) => (
    <Icon
      size={size}
      weight={weight}
      mirrored={mirrored}
      className={`app-icon ${className}`.trim()}
      aria-hidden={props['aria-label'] ? undefined : true}
      focusable="false"
      {...props}
    />
  );

  AppIcon.displayName = `AppIcon(${Icon.displayName || Icon.name || 'Phosphor'})`;
  return AppIcon;
};

// 首屏最小集合(IconsCore)按需再导出,公开页可与 Icons.jsx 保持同名导入。
export {
  AlertTriangle,
  ArrowRight,
  ChevronLeft,
  Info,
  Key,
  LogIn,
  RefreshCw,
  Shield,
  X,
  GitHubBrand,
  createFontIcon,
} from './IconsCore.jsx';

export const Activity = createIcon(Pulse);
export const Bookmark = createIcon(BookmarkSimple);
export const Check = createIcon(PhCheck);
export const ChevronDown = createIcon(CaretDown);
export const Clock = createIcon(PhClock);
export const CodeFile = createIcon(FileCode);
export const Copy = createIcon(PhCopy);
export const Download = createIcon(DownloadSimple);
export const DragHandle = createIcon(DotsSixVertical);
export const Edit = createIcon(PencilSimple);
export const ExternalLink = createIcon(ArrowSquareOut);
export const Eye = createIcon(PhEye);
export const FileText = createIcon(PhFileText);
export const Folder = createIcon(PhFolder);
export const FolderOpen = createIcon(PhFolderOpen);
export const GitBranch = createIcon(PhGitBranch);
export const Globe = createIcon(GlobeHemisphereWest);
export const HardDrive = createIcon(HardDrives);
export const Home = createIcon(House);
export const Menu = createIcon(List);
export const Minus = createIcon(PhMinus);
export const Plug = createIcon(Plugs);
export const Plus = createIcon(PhPlus);
export const RotateCw = createIcon(ArrowClockwise);
export const Search = createIcon(MagnifyingGlass);
export const Send = createIcon(PaperPlaneTilt);
export const Server = createIcon(HardDrives);
export const SortArrows = createIcon(ArrowsOutCardinal);
export const Trash = createIcon(PhTrash);
export const TrendingUp = createIcon(TrendUp);
export const Upload = createIcon(UploadSimple);
export const User = createIcon(PhUser);
