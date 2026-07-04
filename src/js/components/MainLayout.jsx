import React, { lazy, Suspense, useEffect, useMemo } from 'react';
import useStore, { MODULE_GROUPS, MODULE_CONFIG, getModuleName } from '../store.js';
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
  ShieldCheck,
  Activity,
  FolderOpen,
  FileText,
  Bell,
  LogOut,
  Hexagon,
  Settings,
  Clock
} from './Icons.jsx';

const DashboardPage = lazy(() => import('../pages/DashboardPage.jsx'));
const ServerPage = lazy(() => import('../pages/ServerPage.jsx'));
const TotpPage = lazy(() => import('../pages/TotpPage.jsx'));
const FileboxPage = lazy(() => import('../pages/FileboxPage.jsx'));
const UptimePage = lazy(() => import('../pages/UptimePage.jsx'));
const NotificationPage = lazy(() => import('../pages/NotificationPage.jsx'));
const OpenAIPage = lazy(() => import('../pages/OpenAIPage.jsx'));


const PaasPage = lazy(() => import('../pages/PaasPage.jsx'));
const DnsPage = lazy(() => import('../pages/DnsPage.jsx'));
const AliyunPage = lazy(() => import('../pages/AliyunPage.jsx'));
const TencentPage = lazy(() => import('../pages/TencentPage.jsx'));
const SettingsPage = lazy(() => import('../pages/SettingsPage.jsx'));
const SchedulerPage = lazy(() => import('../pages/SchedulerPage.jsx'));
const ApiDocsPage = lazy(() => import('../pages/ApiDocsPage.jsx'));
const SystemLogsPage = lazy(() => import('../pages/SystemLogsPage.jsx'));

const PageLoadingFallback = () => (
  <div className="flex min-h-[240px] items-center justify-center">
    <div
      className="h-8 w-8 animate-spin rounded-full border-2 border-kumo-line border-t-kumo-brand"
      aria-label="Loading"
      role="status"
    />
  </div>
);

class ModuleErrorBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    console.error('module render failed:', error, info);
  }

  componentDidUpdate(prevProps) {
    if (prevProps.moduleId !== this.props.moduleId && this.state.error) {
      this.setState({ error: null });
    }
  }

  render() {
    if (!this.state.error) {
      return this.props.children;
    }
    return (
      <div className="flex min-h-[360px] items-center justify-center">
        <div className="app-card w-full max-w-xl p-5">
          <div className="mb-2 text-sm font-bold text-kumo-strong">模块加载失败</div>
          <div className="mb-4 text-xs leading-relaxed text-kumo-subtle">
            前端资源可能已更新或缓存仍指向旧文件。请重新加载当前页面后再试。
          </div>
          <div className="mb-4 rounded-md border border-kumo-line bg-kumo-recessed/50 p-3 font-mono text-[11px] leading-relaxed text-kumo-subtle">
            {this.state.error?.message || '未知错误'}
          </div>
          <button
            type="button"
            onClick={() => {
              const url = new URL(window.location.href);
              url.searchParams.set('_reload', String(Date.now()));
              window.location.replace(url.toString());
            }}
            className="h-8 rounded-md bg-kumo-brand px-3 text-xs font-bold text-white"
          >
            重新加载
          </button>
        </div>
      </div>
    );
  }
}

// 图标映射配置
const ICON_MAP = {
  dashboard: LayoutDashboard,
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
  apidocs: FileText,
  systemlogs: FileText,
};

const MODULE_PATHS = Object.keys(MODULE_CONFIG).reduce((paths, moduleId) => {
  paths[moduleId] = `/${moduleId}`;
  return paths;
}, { dashboard: '/dashboard' });

const LEGACY_MODULE_PATHS = {
  'self-h': 'scheduler',
};

const getPathModule = (pathname) => {
  const normalized = pathname.replace(/\/+$/, '') || '/';
  if (normalized === '/') return 'dashboard';
  const route = normalized.slice(1);
  if (LEGACY_MODULE_PATHS[route]) return LEGACY_MODULE_PATHS[route];
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
  const { isMobile } = useSidebar();
  const config = MODULE_CONFIG[module];
  if (!config) return null;
  const tooltip = !isMobile ? config.name : undefined;

  return (
    <Sidebar.MenuButton
      active={active}
      aria-current={active ? 'page' : undefined}
      onClick={() => navigateAndClose(module)}
      icon={IconComponent}
      tooltip={tooltip}
    >
      {config.name}
    </Sidebar.MenuButton>
  );
};

const SidebarLogoutButton = ({ onLogout }) => {
  const { isMobile } = useSidebar();
  const tooltip = !isMobile ? '安全退出' : undefined;

  return (
    <Sidebar.MenuButton
      onClick={onLogout}
      className="text-kumo-danger hover:bg-kumo-danger/10"
      icon={LogOut}
      tooltip={tooltip}
    >
      安全退出
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
    })).filter((group) => group.modules.length > 0 && group.id !== 'system');
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

  useEffect(() => {
    const legacyModule = window.location.pathname.replace(/\/+$/, '').slice(1);
    const currentModule = LEGACY_MODULE_PATHS[legacyModule];
    if (!currentModule) return;
    const nextPath = MODULE_PATHS[currentModule] || `/${currentModule}`;
    window.history.replaceState({ module: currentModule }, '', nextPath);
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
      case 'scheduler':
        return <SchedulerPage />;
      case 'apidocs':
        return <ApiDocsPage />;
      case 'systemlogs':
        return <SystemLogsPage />;
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
              {moduleOrder.includes('apidocs') && moduleVisibility.apidocs !== false && (
                <SidebarModuleButton
                  module="apidocs"
                  active={mainActiveTab === 'apidocs'}
                  icon={FileText}
                  onNavigate={navigateToModule}
                />
              )}

              {moduleOrder.includes('systemlogs') && moduleVisibility.systemlogs !== false && (
                <SidebarModuleButton
                  module="systemlogs"
                  active={mainActiveTab === 'systemlogs'}
                  icon={FileText}
                  onNavigate={navigateToModule}
                />
              )}

              <SidebarModuleButton
                module="settings"
                active={mainActiveTab === 'settings'}
                icon={Settings}
                onNavigate={navigateToModule}
              />

              <SidebarLogoutButton onLogout={logout} />
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
              {/* <div className="flex h-6.5 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-kumo-success/20 bg-kumo-success/10 px-2 text-[11px] text-kumo-success">
                <span className="w-1 h-1 rounded-full bg-current animate-pulse"></span>
                <span className="hidden min-[520px]:inline">健康</span>
                <span className="min-[520px]:hidden">正常</span>
              </div> */}
            </AppPageHeader>
          </div>
        </header>

        {/* 主内容画布 */}
        <main className="flex-1 overflow-x-hidden overflow-y-auto p-3 sm:p-4 lg:px-8 lg:pb-6 lg:pt-3 scrollbar-thin">
          <div className={`mx-auto flex min-h-full w-full min-w-0 flex-col ${pageWidthClass}`}>
            <ModuleErrorBoundary moduleId={mainActiveTab}>
              <Suspense fallback={<PageLoadingFallback />}>
                {renderActivePage()}
              </Suspense>
            </ModuleErrorBoundary>
          </div>
        </main>
      </div>
    </Sidebar.Provider>
  );
}

export default MainLayout;
