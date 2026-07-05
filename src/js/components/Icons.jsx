import React from 'react';
import {
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
  Compass as PhCompass,
  Copy as PhCopy,
  Cpu as PhCpu,
  Cube,
  Database as PhDatabase,
  DotsThreeVertical,
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
  Info as PhInfo,
  Key as PhKey,
  List,
  ListBullets,
  Lock as PhLock,
  MagnifyingGlass,
  Moon as PhMoon,
  PaperPlaneTilt,
  Paperclip as PhPaperclip,
  Pause as PhPause,
  PencilSimple,
  Play as PhPlay,
  Plugs,
  Plus as PhPlus,
  Power,
  Pulse,
  PushPin,
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
  SquaresFour,
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

export const LayoutDashboard = createIcon(SquaresFour);
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
export const LogOut = createIcon(SignOut);
export const Menu = createIcon(List);
export const Clock = createIcon(PhClock);
export const ChevronLeft = createIcon(CaretLeft);
export const ChevronRight = createIcon(CaretRight);
export const ArrowRight = createIcon(PhArrowRight);
export const History = createIcon(ClockCounterClockwise);
export const RefreshCw = createIcon(ArrowsClockwise);
export const Box = createIcon(Cube);
export const Send = createIcon(PaperPlaneTilt);
export const Shield = createIcon(PhShield);
export const TrendingUp = createIcon(TrendUp);
export const Rocket = createIcon(RocketLaunch);
export const Key = createIcon(PhKey);
export const CheckDouble = createIcon(Checks);
export const Info = createIcon(PhInfo);
export const Lock = createIcon(PhLock);
export const AlertTriangle = createIcon(Warning);
export const LogIn = createIcon(SignIn);
export const LayoutSidebar = createIcon(PhSidebar);
export const Plus = createIcon(PhPlus);
export const Trash = createIcon(PhTrash);
export const Play = createIcon(PhPlay);
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
export const X = createIcon(PhX);
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
export const Pin = createIcon(PushPin);
export const Check = createIcon(PhCheck);
export const Paperclip = createIcon(PhPaperclip);
export const PieChart = createIcon(ChartPie);
export const Heart = createIcon(PhHeart);
export const Grid = createIcon(GridFour);
export const Google = createIcon(GoogleLogo);
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
export const Home = createIcon(House);
export const User = createIcon(PhUser);
export const Maximize2 = createIcon(ArrowsOutSimple);
export const ExternalLink = createIcon(ArrowSquareOut);
export const ArrowLeft = createIcon(PhArrowLeft);
export const Undo = createIcon(ArrowCounterClockwise);

export const MODULE_ICON_MAP = {
  dashboard: LayoutDashboard,
  settings: Settings,
  openai: Bot,
  paas: Cloud,
  dns: Globe,
  aliyun: Database,
  tencent: Hexagon,
  server: Server,
  scheduler: Clock,
  totp: ShieldCheck,
  uptime: Activity,
  filebox: FolderOpen,
  notification: Bell,
  apidocs: CodeFile,
  systemlogs: LogList,
};

export const MODULE_GROUP_ICON_MAP = {
  overview: LayoutDashboard,
  'api-gateway': TrendingUp,
  infrastructure: Layers,
  toolbox: Grid,
  system: Settings,
};

export const getModuleIconComponent = (moduleId, fallback = Server) => (
  MODULE_ICON_MAP[moduleId] || fallback
);
