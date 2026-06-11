import React, { useEffect, useMemo } from 'react';
import useStore, { MODULE_GROUPS, MODULE_CONFIG, getModuleName } from '../store.js';
import DashboardPage from '../pages/DashboardPage.jsx';
import ServerPage from '../pages/ServerPage.jsx';
import TotpPage from '../pages/TotpPage.jsx';
import FileboxPage from '../pages/FileboxPage.jsx';
import UptimePage from '../pages/UptimePage.jsx';
import NotificationPage from '../pages/NotificationPage.jsx';
import OpenAIPage from '../pages/OpenAIPage.jsx';
import GeminiCliPage from '../pages/GeminiCliPage.jsx';
import QwenPage from '../pages/QwenPage.jsx';
import PaasPage from '../pages/PaasPage.jsx';
import DnsPage from '../pages/DnsPage.jsx';
import AliyunPage from '../pages/AliyunPage.jsx';
import TencentPage from '../pages/TencentPage.jsx';
import SettingsPage from '../pages/SettingsPage.jsx';
import SelfHPage from '../pages/SelfHPage.jsx';
import MusicPage from '../pages/MusicPage.jsx';
import {
  Sidebar,
  useSidebar
} from '@cloudflare/kumo/components/sidebar';
import { Tabs } from '@cloudflare/kumo';
import AppPageHeader, { AppBreadcrumbs } from './AppPageHeader.jsx';
import {
  LayoutDashboard,
  Bot,
  Terminal,
  Cpu,
  Cloud,
  Globe,
  Database,
  Server,
  HardDrive,
  ShieldCheck,
  Music,
  Activity,
  FolderOpen,
  Bell,
  LogOut,
  Hexagon,
  Settings
} from './Icons.jsx';

// 图标映射配置
const ICON_MAP = {
  dashboard: LayoutDashboard,
  openai: Bot,
  'gemini-cli': Terminal,
  qwen: Cpu,
  paas: Cloud,
  dns: Globe,
  aliyun: Database,
  tencent: Hexagon,
  server: Server,
  'self-h': HardDrive,
  totp: ShieldCheck,
  music: Music,
  uptime: Activity,
  filebox: FolderOpen,
  notification: Bell,
};

const MODULE_PATHS = Object.keys(MODULE_CONFIG).reduce((paths, moduleId) => {
  paths[moduleId] = `/${moduleId}`;
  return paths;
}, { dashboard: '/dashboard' });

const getPathModule = (pathname) => {
  const normalized = pathname.replace(/\/+$/, '') || '/';
  if (normalized === '/') return 'dashboard';
  const route = normalized.slice(1);
  return MODULE_CONFIG[route] ? route : null;
};

const PAGE_WIDTH_CLASSES = {
  standard: 'max-w-7xl',
  wide: 'max-w-[1600px]',
  full: 'max-w-none',
};

const PAGE_WIDTH_OPTIONS = [
  { value: 'standard', label: '标准' },
  { value: 'wide', label: '宽屏' },
  { value: 'full', label: '全宽' },
];

const THEME_MODE_OPTIONS = [
  { value: 'auto', label: '自动' },
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
];

const useMobileClosingNavigation = (onNavigate) => {
  const { isMobile, setOpenMobile } = useSidebar();

  return (module) => {
    onNavigate(module);
    if (isMobile) setOpenMobile(false);
  };
};

const SidebarModuleButton = ({ module, active, icon: IconComponent, onNavigate }) => {
  const navigateAndClose = useMobileClosingNavigation(onNavigate);
  const config = MODULE_CONFIG[module];
  if (!config) return null;

  return (
    <Sidebar.MenuButton
      active={active}
      aria-current={active ? 'page' : undefined}
      onClick={() => navigateAndClose(module)}
      icon={IconComponent}
      tooltip={config.name}
    >
      {config.name}
    </Sidebar.MenuButton>
  );
};

const SidebarBrand = () => (
  <div className="flex h-full w-full min-w-0 items-center gap-2">
    <span className="flex size-8.5 shrink-0 items-center justify-center">
      <img src="/logo.svg" className="size-5 shrink-0 object-contain" alt="" />
    </span>
    <span className="min-w-0 truncate text-sm font-semibold text-kumo-strong">
      API Monitor
    </span>
  </div>
);

const SidebarStyleSwitches = ({
  pageWidthMode,
  onPageWidthChange,
  themeMode,
  onThemeModeChange,
}) => {
  const { isMobile, state } = useSidebar();

  if (!isMobile && state === 'collapsed') return null;

  return (
    <Sidebar.Group className="mt-auto">
      <Sidebar.GroupLabel>样式切换</Sidebar.GroupLabel>
      <div className="flex flex-col gap-2 px-2">
        <Tabs
          variant="segmented"
          size="sm"
          className="w-fit max-w-full"
          listClassName="w-fit max-w-full"
          value={pageWidthMode}
          onValueChange={onPageWidthChange}
          tabs={PAGE_WIDTH_OPTIONS}
        />
        <Tabs
          variant="segmented"
          size="sm"
          className="w-fit max-w-full"
          listClassName="w-fit max-w-full"
          value={themeMode}
          onValueChange={onThemeModeChange}
          tabs={THEME_MODE_OPTIONS}
        />
      </div>
    </Sidebar.Group>
  );
};

function MainLayout() {
  const {
    mainActiveTab,
    setMainActiveTab,
    sidebarCollapsed,
    setSidebarCollapsed,
    themeMode,
    setThemeMode,
    pageWidthMode,
    setPageWidthMode,
    moduleVisibility,
    moduleOrder,
    userSettingsLoaded,
    loadUserSettings,
    logout,
  } = useStore();
  const pageWidthClass = PAGE_WIDTH_CLASSES[pageWidthMode] || PAGE_WIDTH_CLASSES.standard;

  const visibleModuleGroups = useMemo(() => {
    return MODULE_GROUPS.map((group) => ({
      ...group,
      modules: moduleOrder.filter(
        (moduleId) => group.modules.includes(moduleId) && moduleVisibility[moduleId] !== false
      ),
    })).filter((group) => group.modules.length > 0);
  }, [moduleOrder, moduleVisibility]);

  useEffect(() => {
    if (!userSettingsLoaded) {
      loadUserSettings();
    }
  }, [loadUserSettings, userSettingsLoaded]);

  useEffect(() => {
    const syncTabFromLocation = () => {
      const routeTab = getPathModule(window.location.pathname);
      if (!routeTab) return;
      const currentTab = useStore.getState().mainActiveTab;
      if (currentTab !== routeTab) {
        useStore.getState().setMainActiveTab(routeTab);
      }
    };

    syncTabFromLocation();
    window.addEventListener('popstate', syncTabFromLocation);
    return () => window.removeEventListener('popstate', syncTabFromLocation);
  }, []);

  const navigateToModule = (module) => {
    setMainActiveTab(module);
    const nextPath = MODULE_PATHS[module] || `/${module}`;
    if (window.location.pathname !== nextPath) {
      window.history.pushState({ module }, '', nextPath);
    }
  };

  useEffect(() => {
    if (!userSettingsLoaded || mainActiveTab === 'settings') return;
    if (moduleVisibility[mainActiveTab] !== false) return;

    const nextModule = moduleOrder.find((moduleId) => moduleVisibility[moduleId] !== false) || 'dashboard';
    setMainActiveTab(nextModule);
    const nextPath = MODULE_PATHS[nextModule] || `/${nextModule}`;
    if (window.location.pathname !== nextPath) {
      window.history.replaceState({ module: nextModule }, '', nextPath);
    }
  }, [mainActiveTab, moduleOrder, moduleVisibility, setMainActiveTab, userSettingsLoaded]);

  // 渲染当前模块页
  const renderActivePage = () => {
    switch (mainActiveTab) {
      case 'dashboard':
        return <DashboardPage onNavigate={navigateToModule} />;
      case 'openai':
        return <OpenAIPage />;
      case 'gemini-cli':
        return <GeminiCliPage />;
      case 'qwen':
        return <QwenPage />;
      case 'paas':
        return <PaasPage />;
      case 'dns':
        return <DnsPage />;
      case 'aliyun':
        return <AliyunPage />;
      case 'tencent':
        return <TencentPage />;
      case 'server':
        return <ServerPage />;
      case 'totp':
        return <TotpPage />;
      case 'filebox':
        return <FileboxPage />;
      case 'uptime':
        return <UptimePage />;
      case 'notification':
        return <NotificationPage />;
      case 'settings':
        return <SettingsPage />;
      case 'self-h':
        return <SelfHPage />;
      case 'music':
        return <MusicPage />;
      default:
        const ActiveIcon = ICON_MAP[mainActiveTab] || Server;
        return (
          <div className="flex flex-col items-center justify-center h-[60vh] text-center p-6 app-card max-w-xl mx-auto">
            <div className="w-16 h-16 rounded-full app-subcard bg-kumo-recessed flex items-center justify-center mb-5 text-kumo-brand">
              <ActiveIcon className="w-7 h-7" />
            </div>
            <h2 className="text-base font-bold text-kumo-strong mb-2.5">
              {getModuleName(mainActiveTab)} 模块重构中
            </h2>
            <p className="text-xs text-kumo-subtle max-w-sm leading-relaxed">
              我们正在使用 React + Kumo + Tailwind v4 像素级重构该页面，在此期间原有逻辑将暂时不可用。
            </p>
          </div>
        );
    }
  };

  return (
    <Sidebar.Provider
      defaultOpen={!sidebarCollapsed}
      open={!sidebarCollapsed}
      onOpenChange={(open) => setSidebarCollapsed(!open)}
      style={{ '--sidebar-width': '12.5rem', '--sidebar-width-icon': '54px' }}
      className="flex h-screen w-screen overflow-hidden bg-kumo-canvas text-kumo-default"
    >
      {/* ==================== 1. 侧边栏 (Sidebar) ==================== */}
      <Sidebar>
        {/* 顶部 Logo */}
        <Sidebar.Header className="h-14! px-2.5!">
          <SidebarBrand />
        </Sidebar.Header>

        {/* 导航栏项 */}
        <Sidebar.Content>
          {visibleModuleGroups.map((group) => {
            const groupLabel = group.id === 'overview' ? '总览' : group.name;

            return (
              <Sidebar.Group key={group.id}>
                <Sidebar.GroupLabel>{groupLabel}</Sidebar.GroupLabel>
                <Sidebar.Menu>
                  {group.modules.map((module) => (
                    <SidebarModuleButton
                      key={module}
                      module={module}
                      active={mainActiveTab === module}
                      icon={ICON_MAP[module] || Server}
                      onNavigate={navigateToModule}
                    />
                  ))}
                </Sidebar.Menu>
              </Sidebar.Group>
            );
          })}
          <Sidebar.Group>
            <Sidebar.GroupLabel>系统</Sidebar.GroupLabel>
            <Sidebar.Menu>
              <SidebarModuleButton
                module="settings"
                active={mainActiveTab === 'settings'}
                icon={Settings}
                onNavigate={navigateToModule}
              />

              <Sidebar.MenuButton
                onClick={logout}
                className="text-kumo-danger hover:bg-kumo-danger/10"
                icon={LogOut}
                tooltip="安全退出"
              >
                安全退出
              </Sidebar.MenuButton>
            </Sidebar.Menu>
          </Sidebar.Group>
          <SidebarStyleSwitches
            pageWidthMode={pageWidthMode}
            onPageWidthChange={setPageWidthMode}
            themeMode={themeMode}
            onThemeModeChange={setThemeMode}
          />
        </Sidebar.Content>

        {/* 底部功能栏 */}
        <Sidebar.Footer className="px-[11px]!">
          <Sidebar.Trigger />
        </Sidebar.Footer>
      </Sidebar>

      {/* ==================== 2. 主页面区 (Main Panel) ==================== */}
      <div className="flex-1 flex flex-col h-full overflow-hidden">
        {/* 顶部导航 */}
        <header className="flex h-14 flex-shrink-0 items-center border-b border-kumo-line bg-kumo-base px-3 min-[450px]:px-4 md:px-6">
          <div className="flex h-full min-w-0 flex-1 items-center gap-3.5">
            <Sidebar.Trigger className="md:hidden" />

            <AppPageHeader
              className="flex-row items-center justify-between"
              spacing="compact"
              breadcrumbs={(
                <AppBreadcrumbs size="sm" className="mr-0 min-w-0 overflow-hidden">
                  <AppBreadcrumbs.Link href={MODULE_PATHS.dashboard}>首页</AppBreadcrumbs.Link>
                  <AppBreadcrumbs.Separator />
                  <AppBreadcrumbs.Current>{getModuleName(mainActiveTab)}</AppBreadcrumbs.Current>
                </AppBreadcrumbs>
              )}
            >
              <div className="flex h-6.5 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-kumo-success/20 bg-kumo-success/10 px-2 text-[11px] text-kumo-success">
                <span className="w-1 h-1 rounded-full bg-current animate-pulse"></span>
                <span className="hidden min-[520px]:inline">系统正常运行</span>
                <span className="min-[520px]:hidden">正常</span>
              </div>
            </AppPageHeader>
          </div>
        </header>

        {/* 主内容画布 */}
        <main className="flex-1 overflow-y-auto p-3 sm:p-4 lg:px-8 lg:pb-6 lg:pt-3 scrollbar-thin">
          <div className={`mx-auto flex min-h-full w-full min-w-0 flex-col ${pageWidthClass}`}>
            {renderActivePage()}
          </div>
        </main>
      </div>
    </Sidebar.Provider>
  );
}

export default MainLayout;
