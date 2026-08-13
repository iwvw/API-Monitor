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
  m365: Cloud,
  github: GitHubBrand,
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
