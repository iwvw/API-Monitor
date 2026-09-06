import React from 'react';
import {
  AppWindow as PhAppWindow,
  ArrowClockwise,
  ArrowCounterClockwise,
  ArrowLeft as PhArrowLeft,
  ArrowRight as PhArrowRight,
  ArrowSquareOut,
  ArrowsClockwise,
  ArrowsOutSimple,
  Bell as PhBell,
  BookmarkSimple as PhBookmarkSimple,
  Brain as PhBrain,
  CaretDown,
  CaretLeft,
  CaretRight,
  CaretUp,
  ChartPie,
  ChatCircle,
  Check as PhCheck,
  Checks,
  Clock as PhClock,
  ClockCounterClockwise,
  Cloud as PhCloud,
  Columns as PhColumns,
  Compass as PhCompass,
  Copy as PhCopy,
  Cpu as PhCpu,
  Cursor as PhCursor,
  Cube,
  Database as PhDatabase,
  DotsThreeVertical,
  Desktop as PhDesktop,
  DownloadSimple,
  EnvelopeSimple,
  Eye as PhEye,
  EyeSlash,
  FileCode as PhFileCode,
  FileText as PhFileText,
  FloppyDisk,
  Folder as PhFolder,
  FolderOpen as PhFolderOpen,
  GearSix,
  GitBranch as PhGitBranch,
  GlobeHemisphereWest,
  GoogleLogo,
  GridFour,
  HardDrives,
  Heart as PhHeart,
  Hexagon as PhHexagon,
  House,
  Image as PhImage,
  Key as PhKey,
  List,
  ListBullets,
  Lock as PhLock,
  MagnifyingGlass,
  Minus as PhMinus,
  Moon as PhMoon,
  Palette as PhPalette,
  PaperPlaneTilt,
  Paperclip as PhPaperclip,
  Pause as PhPause,
  PencilSimple,
  Play as PhPlay,
  PlayCircle as PhPlayCircle,
  Plugs,
  Plus as PhPlus,
  Power,
  Pulse,
  PushPin,
  Rectangle as PhRectangle,
  Repeat as PhRepeat,
  RepeatOnce,
  Robot,
  RocketLaunch,
  Shield as PhShield,
  ShieldCheck as PhShieldCheck,
  Shuffle as PhShuffle,
  Sidebar as PhSidebar,
  SignIn,
  SignOut,
  SkipBack as PhSkipBack,
  SkipForward as PhSkipForward,
  SlidersHorizontal,
  SpeakerHigh,
  SpeakerSlash,
  Sparkle as PhSparkle,
  SquaresFour,
  ThumbsDown as PhThumbsDown,
  ThumbsUp as PhThumbsUp,
  Square as PhSquare,
  Stack,
  Star as PhStar,
  Sun as PhSun,
  TerminalWindow,
  Trash as PhTrash,
  TrendUp,
  UploadSimple,
  User as PhUser,
  Users as PhUsers,
  Warning,
  Wrench as PhWrench,
  X as PhX,
} from '@phosphor-icons/react';
import { createFontIcon, GitHubBrand } from './IconsCore.jsx';

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

// 首屏所需的最小图标集合定义在 IconsCore.jsx（登录页/全局对话框使用），
// 这里 re-export 以保持既有导入路径兼容。
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
} from './IconsCore.jsx';

import tencentCloudIcon from '../../assets/brand-icons/tencentcloud.svg';

const createAssetIcon = (asset, label) => {
  const AssetIcon = ({ className = '', style, ...props }) => (
    <span
      {...props}
      className={`app-icon app-brand-icon app-brand-icon--asset ${className}`.trim()}
      aria-hidden={props['aria-label'] ? undefined : true}
      style={{
        '--app-brand-icon-url': `url("${asset}")`,
        ...style,
      }}
    />
  );

  AssetIcon.displayName = `AppIcon(${label})`;
  return AssetIcon;
};

export const LayoutDashboard = createIcon(SquaresFour);
export const AppWindow = createIcon(PhAppWindow);
export const Bot = createIcon(Robot);
export const Terminal = createIcon(TerminalWindow);
export const Cpu = createIcon(PhCpu);
export const Cloud = createIcon(PhCloud);
export const Globe = createIcon(GlobeHemisphereWest);
export const Database = createIcon(PhDatabase);
export const Server = createIcon(HardDrives);
export const HardDrive = createIcon(HardDrives);
export const ShieldCheck = createIcon(PhShieldCheck);
export const Activity = createIcon(Pulse);
export const FolderOpen = createIcon(PhFolderOpen);
export const Bell = createIcon(PhBell);
export const Bookmark = createIcon(PhBookmarkSimple);
export const Mail = createIcon(EnvelopeSimple);
export const MessageSquare = createIcon(ChatCircle);
export const Settings = createIcon(GearSix);
export const Sun = createIcon(PhSun);
export const Moon = createIcon(PhMoon);
export const DesktopDisplay = createIcon(PhDesktop);
export const Palette = createIcon(PhPalette);
export const LogOut = createIcon(SignOut);
export const Menu = createIcon(List);
export const Clock = createIcon(PhClock);
export const ChevronRight = createIcon(CaretRight);
export const History = createIcon(ClockCounterClockwise);
export const Box = createIcon(Cube);
export const Send = createIcon(PaperPlaneTilt);
export const TrendingUp = createIcon(TrendUp);
export const Rocket = createIcon(RocketLaunch);
export const CheckDouble = createIcon(Checks);
export const Lock = createIcon(PhLock);
export const LayoutSidebar = createIcon(PhSidebar);
export const Plus = createIcon(PhPlus);
export const Minus = createIcon(PhMinus);
export const Trash = createIcon(PhTrash);
export const Play = createIcon(PhPlay);
export const PlayCircle = createIcon(PhPlayCircle);
export const Pause = createIcon(PhPause);
export const Folder = createIcon(PhFolder);
export const FileText = createIcon(PhFileText);
export const CodeFile = createIcon(PhFileCode);
export const LogList = createIcon(ListBullets);
export const Save = createIcon(FloppyDisk);
export const RotateCw = createIcon(ArrowClockwise);
export const Search = createIcon(MagnifyingGlass);
export const Upload = createIcon(UploadSimple);
export const Download = createIcon(DownloadSimple);
export const Edit = createIcon(PencilSimple);
export const Reboot = createIcon(Power);
export const ChevronDown = createIcon(CaretDown);
export const ChevronUp = createIcon(CaretUp);
export const Users = createIcon(PhUsers);
export const Eye = createIcon(PhEye);
export const EyeOff = createIcon(EyeSlash);
export const Copy = createIcon(PhCopy);
export const Plug = createIcon(Plugs);
export const Brain = createIcon(PhBrain);
export const Image = createIcon(PhImage);
export const Star = createIcon(PhStar);
export const Sparkle = createIcon(PhSparkle);
export const ThumbsDown = createIcon(PhThumbsDown);
export const ThumbsUp = createIcon(PhThumbsUp);
export const Pin = createIcon(PushPin);
export const Check = createIcon(PhCheck);
export const Paperclip = createIcon(PhPaperclip);
export const PieChart = createIcon(ChartPie);
export const Heart = createIcon(PhHeart);
export const Columns = createIcon(PhColumns);
export const Grid = createIcon(GridFour);
export const Google = createIcon(GoogleLogo);
export const Rectangle = createIcon(PhRectangle);
export const Sliders = createIcon(SlidersHorizontal);
export const Layers = createIcon(Stack);
export const GitBranch = createIcon(PhGitBranch);
export const Square = createIcon(PhSquare);
export const Hexagon = createIcon(PhHexagon);
export const MoreVertical = createIcon(DotsThreeVertical);
export const SkipBack = createIcon(PhSkipBack);
export const SkipForward = createIcon(PhSkipForward);
export const Repeat = createIcon(PhRepeat);
export const Repeat1 = createIcon(RepeatOnce);
export const Shuffle = createIcon(PhShuffle);
export const Volume2 = createIcon(SpeakerHigh);
export const VolumeX = createIcon(SpeakerSlash);
export const Compass = createIcon(PhCompass);
export const Cursor = createIcon(PhCursor);
export const Home = createIcon(House);
export const User = createIcon(PhUser);
export const Maximize2 = createIcon(ArrowsOutSimple);
export const ExternalLink = createIcon(ArrowSquareOut);
export const ArrowLeft = createIcon(PhArrowLeft);
export const Undo = createIcon(ArrowCounterClockwise);
export const Wrench = createIcon(PhWrench);
export const CloudflareBrand = createFontIcon('si si-cloudflare', 'Cloudflare');
export const AlibabaCloudBrand = createFontIcon('si si-alibabacloud', 'AlibabaCloud');
export const TencentCloudBrand = createAssetIcon(tencentCloudIcon, 'TencentCloud');
export const KoyebBrand = createFontIcon('si si-koyeb', 'Koyeb');
export const FlyIoBrand = createFontIcon('si si-flydotio', 'Fly.io');

export const MODULE_ICON_MAP = {
  dashboard: LayoutDashboard,
  settings: Settings,
  openai: Sparkle,
  subscription: Plug,
  paas: Rocket,
  dns: CloudflareBrand,
  aliyun: AlibabaCloudBrand,
  tencent: TencentCloudBrand,
  oracle: Cloud,
  gcp: Cloud,
  huawei: Cloud,
  m365: Cloud,
  github: GitHubBrand,
  dockerhub: Box,
  server: Server,
  scheduler: Clock,
  totp: ShieldCheck,
  uptime: Activity,
  filebox: FolderOpen,
  notification: Bell,
  apidocs: CodeFile,
  systemlogs: LogList,
  drawio: Compass,
  prompts: MessageSquare,
  bookmarks: Bookmark,
  forward: Shuffle,
};

export const MODULE_GROUP_ICON_MAP = {
  overview: LayoutDashboard,
  'api-gateway': TrendingUp,
  infrastructure: Layers,
  'cloud-vendors': Cloud,
  devops: GitBranch,
  toolbox: Grid,
  'system-tools': Sliders,
  'utility-tools': FolderOpen,
  system: Settings,
  'global-config': Settings,
  configuration: Settings,
};

export const getModuleIconComponent = (moduleId, fallback = Server) => (
  MODULE_ICON_MAP[moduleId] || fallback
);

/* 微信 / Telegram 品牌图标（彩色圆角方块原始形状，来自 svgrepo）。
   weight 属性直接吞掉，避免调用方按 phosphor 语义传入后透传到 <svg> 上。 */
export const WechatBrand = ({ className = '', size = 24, weight, ...props }) => (
  <svg
    viewBox="0 0 512 512"
    width={size}
    height={size}
    className={`app-icon ${className}`.trim()}
    aria-hidden={props['aria-label'] ? undefined : true}
    focusable="false"
    {...props}
  >
    <rect width="512" height="512" rx="15%" fill="#00c70a" />
    <path fill="#ffffff" d="M402 369c23-17 38-42 38-70 0-51-50-92-111-92s-110 41-110 92 49 92 110 92c13 0 25-2 36-5 4-1 8 0 9 1l25 14c3 2 6 0 5-4l-6-22c0-3 2-5 4-6m-110-85a15 15 0 1 1 0-29 15 15 0 0 1 0 29m74 0a15 15 0 1 1 0-29 15 15 0 0 1 0 29" />
    <path fill="#ffffff" d="m205 105c-73 0-132 50-132 111 0 33 17 63 45 83 3 2 5 5 4 10l-7 24c-1 5 3 7 6 6l30-17c3-2 7-3 11-2 26 8 48 6 51 6-24-84 59-132 123-128-10-52-65-93-131-93m-44 93a18 18 0 1 1 0-35 18 18 0 0 1 0 35m89 0a18 18 0 1 1 0-35 18 18 0 0 1 0 35" />
  </svg>
);

export const TelegramBrand = ({ className = '', size = 24, weight, ...props }) => (
  <svg
    viewBox="0 0 512 512"
    width={size}
    height={size}
    className={`app-icon ${className}`.trim()}
    aria-hidden={props['aria-label'] ? undefined : true}
    focusable="false"
    {...props}
  >
    <rect width="512" height="512" rx="15%" fill="#37aee2" />
    <path fill="#c8daea" d="M199 404c-11 0-10-4-13-14l-32-105 245-144" />
    <path fill="#a9c9dd" d="M199 404c7 0 11-4 16-8l45-43-56-34" />
    <path fill="#f6fbfe" d="M204 319l135 99c14 9 26 4 30-14l55-258c5-22-9-32-24-25L79 245c-21 8-21 21-4 26l83 26 190-121c9-5 17-3 11 4" />
  </svg>
);

export const WeComBrand = ({ className = '', size = 24, weight, ...props }) => (
  <svg
    viewBox="0 0 24 24"
    width={size}
    height={size}
    className={`app-icon ${className}`.trim()}
    aria-hidden={props['aria-label'] ? undefined : true}
    focusable="false"
    {...props}
  >
    <path
      fill="#0082ef"
      d="M10,3 C14.0711,3 17.7,5.67053 17.9824,9.3685 C20.2862,10.2297 22,12.1789 22,14.6285 C22,16.4955 20.9737,18.0999 19.4809,19.1207 C19.4565,19.26655 19.454325,19.41305 19.45825,19.55975 L19.4722,20 L19.4722,20 C19.4722,20.5523 19.0245,21 18.4722,21 C17.7145,21 17.0123,20.7532 16.418,20.2902 C16.0578,20.3415 15.6884,20.3681 15.3125,20.3681 C12.5459,20.3681 9.99138,18.877 9.02908,16.6169 C8.87636,16.6012 8.7249,16.5818 8.57481,16.5588 C7.85947,17.1257 7.03533,17.4444 6.11111,17.4444 C5.55883,17.4444 5.11111,16.9967 5.11111,16.4444 L5.11603833,16.2406824 L5.11603833,16.2406824 L5.13418,15.830575 C5.1389175,15.625575 5.13006,15.4216 5.07568,15.2229 C3.24799,14.004 2,12.0725 2,9.83333 C2,5.89642 5.76018,3 10,3 Z M15.3125,10.8889 C12.5452,10.8889 10.625,12.7262 10.625,14.6285 C10.625,16.5308 12.5452,18.3681 15.3125,18.3681 C15.6846,18.3681 16.0454,18.3333 16.3906,18.268 C16.8249,18.1859 17.2098,18.3676 17.5404,18.6282 C17.6399,18.195 17.865,17.7856 18.2466,17.5397 C19.3632,16.8203 20,15.7528 20,14.6285 C20,12.7262 18.0798,10.8889 15.3125,10.8889 Z M13.62,13 C14.1723,13 14.62,13.4477 14.62,14 C14.62,14.5523 14.1723,15 13.62,15 C13.0677,15 12.62,14.5523 12.62,14 C12.62,13.4477 13.0677,13 13.62,13 Z M17,13 C17.5523,13 18,13.4477 18,14 C18,14.5523 17.5523,15 17,15 C16.4477,15 16,14.5523 16,14 C16,13.4477 16.4477,13 17,13 Z M8,7 C8.55228,7 9,7.44772 9,8 C9,8.55228 8.55228,9 8,9 C7.44772,9 7,8.55228 7,8 C7,7.44772 7.44772,7 8,7 Z M12,7 C12.5523,7 13,7.44772 13,8 C13,8.55228 12.5523,9 12,9 C11.4477,9 11,8.55228 11,8 C11,7.44772 11.4477,7 12,7 Z"
    />
  </svg>
);
