import React, { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react';
import useStore, {
  MODULE_GROUPS,
  MODULE_CONFIG,
  getGroupModuleIds,
  getModuleName,
  store,
} from '../store.js';
import { Sidebar, useSidebar } from '@cloudflare/kumo/components/sidebar';
import { Tooltip } from '@cloudflare/kumo/components/tooltip';
import { Button } from '@cloudflare/kumo/components/button';
import { ClipboardText, Empty, Tabs } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { APP_VERSION } from '../modules/appVersion.js';
import AppPageHeader, { AppBreadcrumbs } from './AppPageHeader.jsx';
import { AppCard } from './ui/AppPrimitives.jsx';
import { AlertTriangle } from './IconsCore.jsx';
import {
  Globe,
  Server,
  LogOut,
  DesktopDisplay,
  Palette,
  Sun,
  Moon,
  Settings,
  Sparkle,
  LayoutSidebar,
  MODULE_GROUP_ICON_MAP,
  getModuleIconComponent,
} from './Icons.jsx';

const DashboardPage = lazy(() => import('../pages/DashboardPage.jsx'));
const ServerPage = lazy(() => import('../pages/ServerPage.jsx'));
const TotpPage = lazy(() => import('../pages/TotpPage.jsx'));
const FileboxPage = lazy(() => import('../pages/FileboxPage.jsx'));
const UptimePage = lazy(() => import('../pages/UptimePage.jsx'));
const NotificationPage = lazy(() => import('../pages/NotificationPage.jsx'));
const OpenAIPage = lazy(() => import('../pages/OpenAIPage.jsx'));
const SubscriptionPage = lazy(() => import('../pages/SubscriptionPage.jsx'));
const GitHubPage = lazy(() => import('../pages/GitHubPage.jsx'));

const PaasPage = lazy(() => import('../pages/PaasPage.jsx'));
const DnsPage = lazy(() => import('../pages/DnsPage.jsx'));
const AliyunPage = lazy(() => import('../pages/AliyunPage.jsx'));
const TencentPage = lazy(() => import('../pages/TencentPage.jsx'));
const OraclePage = lazy(() => import('../pages/OraclePage.jsx'));
const M365Page = lazy(() => import('../pages/M365Page.jsx'));
const SettingsPage = lazy(() => import('../pages/SettingsPage.jsx'));
const SchedulerPage = lazy(() => import('../pages/SchedulerPage.jsx'));
const ApiDocsPage = lazy(() => import('../pages/ApiDocsPage.jsx'));
const SystemLogsPage = lazy(() => import('../pages/SystemLogsPage.jsx'));
const DrawioPage = lazy(() => import('../pages/DrawioPage.jsx'));
const PromptLibraryPage = lazy(() => import('../pages/PromptLibraryPage.jsx'));
const AdminAIPage = lazy(() => import('../pages/AdminAIPage.jsx'));

import { pageStackClass } from './ui/AppPrimitives.jsx';
import AskAiPanel from './adminai/AskAiPanel.jsx';

const PageLoadingFallback = () => (
  <div className={`${pageStackClass} pt-3 cq-sm:pt-4`}>
    <div className="rounded-xl border border-kumo-fill bg-kumo-control">
      <div className="flex items-center gap-2 border-b border-kumo-line px-4 py-2.5">
        <SkeletonLine className="h-4 w-4" />
        <SkeletonLine className="h-3.5 w-24" />
      </div>
      <div className="flex flex-col gap-3 p-4">
        <SkeletonLine className="h-3.5 w-3/4" />
        <SkeletonLine className="h-3.5 w-1/2" />
        <SkeletonLine className="h-3.5 w-5/6" />
      </div>
    </div>
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
        <Empty
          size="sm"
          className="max-w-xl"
          icon={<AlertTriangle size={32} />}
          title="模块加载失败"
          description="前端资源已更新或缓存过期，请重新加载页面。"
          contents={
            <div className="flex flex-col items-center gap-3">
              <ClipboardText
                text={this.state.error?.message || '未知错误'}
                className="w-full max-w-md"
              />
              <Button
                type="button"
                size="sm"
                variant="primary"
                onClick={() => {
                  const url = new URL(window.location.href);
                  url.searchParams.set('_reload', String(Date.now()));
                  window.location.replace(url.toString());
                }}
                className="font-bold"
              >
                重新加载
              </Button>
            </div>
          }
        />
      </div>
    );
  }
}

const MODULE_PATHS = Object.keys(MODULE_CONFIG).reduce(
  (paths, moduleId) => {
    paths[moduleId] = `/${moduleId}`;
    return paths;
  },
  { dashboard: '/' }
);

const LEGACY_MODULE_PATHS = {
  dashboard: 'dashboard',
  'self-h': 'scheduler',
};

const getPathModule = pathname => {
  const normalized = pathname.replace(/\/+$/, '') || '/';
  if (
    typeof window !== 'undefined' &&
	new URLSearchParams(window.location.search).has('mockDocker') &&
	normalized === '/server'
  ) {
    return 'server';
  }
  if (normalized === '/') return 'dashboard';
  const route = normalized.slice(1);
  if (LEGACY_MODULE_PATHS[route]) return LEGACY_MODULE_PATHS[route];
  return MODULE_CONFIG[route] ? route : null;
};

const renderSidebarStyleIcon = (IconComponent, label) => (
  <span
    title={label}
    aria-label={label}
    className="inline-flex h-4 w-4 items-center justify-center"
  >
    <IconComponent className="h-3.5 w-3.5" />
  </span>
);

const formatAppProcessUptime = seconds => {
  const totalSeconds = Math.max(0, Math.floor(Number(seconds) || 0));
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);

  if (days > 0) return `${days}天 ${hours}小时`;
  if (hours > 0) return `${hours}小时 ${minutes}分钟`;
  if (minutes > 0) return `${minutes}分钟`;
  return `${totalSeconds}秒`;
};

const THEME_MODE_OPTIONS = [
  {
    value: 'auto',
    label: renderSidebarStyleIcon(DesktopDisplay, '自动跟随系统'),
    className: 'w-full !justify-center !px-0',
  },
  {
    value: 'light',
    label: renderSidebarStyleIcon(Sun, '浅色模式'),
    className: 'w-full !justify-center !px-0',
  },
  {
    value: 'dark',
    label: renderSidebarStyleIcon(Moon, '深色模式'),
    className: 'w-full !justify-center !px-0',
  },
];

const HAPTIC_INTERACTIVE_SELECTOR =
  'button, a, [role="button"], [role="tab"], [role="switch"], input, select, textarea';

const useMobileClosingNavigation = onNavigate => {
  const { isMobile, setOpenMobile } = useSidebar();

  return module => {
    onNavigate(module);
    if (isMobile) setOpenMobile(false);
  };
};

/* 左侧边栏开合桥：顶栏移动端按钮位于右侧 AskAI Provider 子树内，
   Sidebar.Trigger 的上下文会解析到右侧面板（导致窄屏点左上角开错面板）。
   这里在左侧 Provider 内注册 toggleSidebar，供顶栏按钮显式调用。 */
const leftSidebarToggles = new Set();
function LeftSidebarBridge() {
  const { toggleSidebar } = useSidebar();
  useEffect(() => {
    leftSidebarToggles.add(toggleSidebar);
    return () => leftSidebarToggles.delete(toggleSidebar);
  }, [toggleSidebar]);
  return null;
}

const SidebarTooltipMenuButton = ({ label, children, ...props }) => {
  const { isMobile, state } = useSidebar();
  const [tooltipOpen, setTooltipOpen] = useState(false);
  const allowTooltip = !isMobile && state === 'collapsed';

  useEffect(() => {
    if (!allowTooltip) setTooltipOpen(false);
  }, [allowTooltip]);

  return (
    <Sidebar.MenuItem>
      <Tooltip
        content={label}
        side="right"
        open={allowTooltip ? tooltipOpen : false}
        onOpenChange={open => setTooltipOpen(allowTooltip && open)}
        render={
          <Sidebar.MenuButton {...props} aria-label={label}>
            {children}
          </Sidebar.MenuButton>
        }
      />
    </Sidebar.MenuItem>
  );
};

const SidebarModuleButton = ({ module, active, icon: IconComponent, onNavigate }) => {
  const navigateAndClose = useMobileClosingNavigation(onNavigate);
  const config = MODULE_CONFIG[module];
  if (!config) return null;

  return (
    <SidebarTooltipMenuButton
      label={config.name}
      active={active}
      aria-current={active ? 'page' : undefined}
      onClick={() => navigateAndClose(module)}
      icon={IconComponent}
    >
      {config.name}
    </SidebarTooltipMenuButton>
  );
};

const SidebarModuleSubButton = ({ module, active, onNavigate }) => {
  const navigateAndClose = useMobileClosingNavigation(onNavigate);
  const config = MODULE_CONFIG[module];
  if (!config) return null;

  return (
    <Sidebar.MenuSubButton
      active={active}
      aria-current={active ? 'page' : undefined}
      onClick={() => navigateAndClose(module)}
    >
      {config.name}
    </Sidebar.MenuSubButton>
  );
};

const SidebarModuleSubgroup = ({ subgroup, activeModule, onNavigate }) => {
  const subgroupModules = subgroup.modules || [];
  const active = subgroupModules.includes(activeModule);
  const [open, setOpen] = useState(active);
  const ParentIcon = subgroup.icon || MODULE_GROUP_ICON_MAP[subgroup.id] || Globe;
  const quietTriggerClassName = [
    '!bg-transparent',
    '!shadow-none',
    '!text-inherit',
    'active:!bg-transparent',
    'hover:!bg-transparent',
    'hover:!text-inherit',
    'focus-visible:!bg-transparent',
    'focus-visible:!shadow-none',
    'data-[active=true]:!bg-transparent',
    'data-[selected=true]:!bg-transparent',
    'data-[state=open]:!bg-transparent',
    'data-[state=open]:!text-inherit',
    '[&_[data-slot=sidebar-menu-button-label]]:!font-normal',
  ].join(' ');

  useEffect(() => {
    if (active) {
      setOpen(true);
    }
  }, [active]);

  return (
    <Sidebar.MenuItem>
      <Sidebar.Collapsible open={open} onOpenChange={setOpen} autoScrollOnOpen>
        <Sidebar.CollapsibleTrigger
          render={
            <Sidebar.MenuButton icon={ParentIcon} className={quietTriggerClassName}>
              {subgroup.name}
              <Sidebar.MenuChevron />
            </Sidebar.MenuButton>
          }
        />
        <Sidebar.CollapsibleContent>
          <Sidebar.MenuSub>
            {subgroupModules.map(module => (
              <SidebarModuleSubButton
                key={module}
                module={module}
                active={activeModule === module}
                onNavigate={onNavigate}
              />
            ))}
          </Sidebar.MenuSub>
        </Sidebar.CollapsibleContent>
      </Sidebar.Collapsible>
    </Sidebar.MenuItem>
  );
};

const SidebarLogoutButton = ({ onLogout }) => {
  return (
    <SidebarTooltipMenuButton
      label="退出"
      onClick={onLogout}
      className="text-kumo-danger hover:bg-kumo-danger/10"
      icon={LogOut}
    >
      退出
    </SidebarTooltipMenuButton>
  );
};

const SidebarBrand = ({ onHome }) => (
  <Button
    type="button"
    variant="ghost"
    onClick={onHome}
    className="!h-full w-full min-w-0 justify-start gap-1 rounded-none text-left !bg-transparent !p-0 hover:!bg-transparent active:!bg-transparent focus:!bg-transparent focus-visible:!bg-transparent data-[active=true]:!bg-transparent data-[selected=true]:!bg-transparent"
    aria-label="返回首页"
  >
    <span className="flex size-10 shrink-0 items-center justify-center transition-transform duration-250 ease-[cubic-bezier(0.4,0,0.2,1)]">
      <img src="/logo.svg" className="size-7 shrink-0 object-contain" alt="" />
    </span>
    <span className="app-brand-wordmark min-w-0 max-w-48 overflow-hidden truncate whitespace-nowrap text-xl font-semibold text-kumo-strong opacity-100 transition-[max-width,opacity] duration-250 ease-[cubic-bezier(0.4,0,0.2,1)] group-data-[state=collapsed]/sidebar:max-w-0 group-data-[state=collapsed]/sidebar:opacity-0">
      API Monitor
    </span>
  </Button>
);

const SidebarStyleSwitchItems = ({
  themeMode,
  onThemeModeChange,
}) => {
  const controlRowClassName = [
    'group/menu-button relative flex w-full min-w-0 items-center gap-2.5 rounded-lg text-kumo-default',
    'before:absolute before:inset-x-0 before:-inset-y-px',
    'min-h-8.5 px-3 py-0 text-sm transition-[color,box-shadow,outline] duration-(--sidebar-animation-duration)',
    'hover:bg-transparent',
    'active:bg-transparent',
    'focus-within:bg-transparent',
  ].join(' ');
  const controlRowInnerClassName = [
    'flex flex-1 min-w-0 items-center gap-3',
    'translate-x-[-3px] group-not-data-[state=collapsed]/sidebar:translate-x-0',
    'transition-transform duration-(--sidebar-animation-duration)',
  ].join(' ');

  return (
    <>
      <Sidebar.MenuItem>
        <div className={controlRowClassName} data-sidebar="menu-button">
          <div className={controlRowInnerClassName}>
            <span className="h-4 w-4 shrink-0 opacity-40" title="主题模式" aria-label="主题模式">
              <Palette className="h-4 w-4" />
            </span>
            <div className="sidebar-style-tabs min-w-0 flex-1 group-data-[state=collapsed]/sidebar:hidden">
              <Tabs
                {...TOOL_TABS_PROPS}
                className="w-full min-w-0"
                listClassName="grid w-full grid-cols-3"
                value={themeMode}
                onValueChange={onThemeModeChange}
                tabs={THEME_MODE_OPTIONS}
              />
            </div>
          </div>
        </div>
      </Sidebar.MenuItem>
    </>
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
    dashboardFooterVisible,
    dashboardFooterRecordNumber,
    appProcessUptimeSeconds,
    appProcessUptimeMeasuredAt,
    moduleVisibility,
    moduleOrder,
    userSettingsLoaded,
    loadUserSettings,
    triggerHaptic,
    logout,
    showAskAI,
    setShowAskAI,
  } = useStore();
  const [runtimeClock, setRuntimeClock] = useState(() => Date.now());
  const displayedAppProcessUptime =
    appProcessUptimeSeconds > 0
      ? appProcessUptimeSeconds + Math.max(0, runtimeClock - appProcessUptimeMeasuredAt) / 1000
      : 0;

  // 监听 1024px 响应式断点（与 Sidebar mobileBreakpoint={1024} 保持一致）
  const [isMobileScreen, setIsMobileScreen] = useState(() =>
    typeof window !== 'undefined' ? window.innerWidth < 1024 : false
  );

  useEffect(() => {
    if (typeof window === 'undefined') return undefined;
    const media = window.matchMedia('(max-width: 1023px)');
    const updateMobile = () => setIsMobileScreen(media.matches);
    updateMobile();
    media.addEventListener('change', updateMobile);
    return () => media.removeEventListener('change', updateMobile);
  }, []);

  const handleSidebarOpenChange = useCallback((open) => {
    // 只有在桌面大屏模式下，用户的展开/收起操作才进行持久化；
    // 移动端断点引发的自动收起绝不污染或覆盖桌面端的 sidebarCollapsed 偏好
    if (!isMobileScreen) {
      setSidebarCollapsed(!open);
    }
  }, [isMobileScreen, setSidebarCollapsed]);

  useEffect(() => {
    if (mainActiveTab !== 'dashboard' || !dashboardFooterVisible) return undefined;
    setRuntimeClock(Date.now());
    const interval = window.setInterval(() => setRuntimeClock(Date.now()), 30000);
    return () => window.clearInterval(interval);
  }, [dashboardFooterVisible, mainActiveTab]);

  const visibleModuleGroups = useMemo(() => {
    return MODULE_GROUPS.map(group => {
      const directModules = moduleOrder.filter(
        moduleId => (group.modules || []).includes(moduleId) && moduleVisibility[moduleId] !== false
      );
      const subgroups = (group.subgroups || [])
        .map(subgroup => ({
          ...subgroup,
          modules: moduleOrder.filter(
            moduleId =>
              (subgroup.modules || []).includes(moduleId) && moduleVisibility[moduleId] !== false
          ),
        }))
        .filter(subgroup => subgroup.modules.length > 0);
      const trailingModules = moduleOrder.filter(
        moduleId =>
          (group.trailingModules || []).includes(moduleId) && moduleVisibility[moduleId] !== false
      );

      return {
        ...group,
        modules: directModules,
        subgroups,
        trailingModules,
      };
    }).filter(group => {
      if (group.id === 'system') return false;
      return getGroupModuleIds(group).some(moduleId => moduleVisibility[moduleId] !== false);
    });
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
      if (
        routeTab === 'server' &&
        new URLSearchParams(window.location.search).has('mockDocker') &&
        window.location.pathname !== '/server'
      ) {
        window.history.replaceState({ module: 'server' }, '', `/server${window.location.search}`);
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

  const navigateToModule = (module, query) => {
    triggerHaptic();
    setMainActiveTab(module);
    const basePath = MODULE_PATHS[module] || `/${module}`;
    const nextPath = query ? `${basePath}?${new URLSearchParams(query).toString()}` : basePath;
    if (window.location.pathname + window.location.search !== nextPath) {
      window.history.pushState({ module }, '', nextPath);
    }
  };

  const navigateHome = () => {
    setMainActiveTab('dashboard');
    if (window.location.pathname !== '/' || window.location.search || window.location.hash) {
      window.history.pushState({ module: 'dashboard' }, '', '/');
    }
  };

  useEffect(() => {
    if (!userSettingsLoaded || mainActiveTab === 'settings') return;
    if (moduleVisibility[mainActiveTab] !== false) return;

    const nextModule =
      moduleOrder.find(moduleId => moduleVisibility[moduleId] !== false) || 'dashboard';
    setMainActiveTab(nextModule);
    const nextPath = MODULE_PATHS[nextModule] || `/${nextModule}`;
    if (window.location.pathname !== nextPath) {
      window.history.replaceState({ module: nextModule }, '', nextPath);
    }
  }, [mainActiveTab, moduleOrder, moduleVisibility, setMainActiveTab, userSettingsLoaded]);

  useEffect(() => {
    const matchesInteractiveTarget = target =>
      target instanceof Element && Boolean(target.closest(HAPTIC_INTERACTIVE_SELECTOR));

    const handlePointerUp = event => {
      if (event.pointerType && event.pointerType !== 'touch') return;
      if (!matchesInteractiveTarget(event.target)) return;
      triggerHaptic();
    };

    const handleClick = event => {
      const hasTouchCapability = typeof navigator !== 'undefined' && navigator.maxTouchPoints > 0;
      if (!hasTouchCapability) return;
      if (!matchesInteractiveTarget(event.target)) return;
      triggerHaptic();
    };

    document.addEventListener('pointerup', handlePointerUp, true);
    document.addEventListener('click', handleClick, true);

    return () => {
      document.removeEventListener('pointerup', handlePointerUp, true);
      document.removeEventListener('click', handleClick, true);
    };
  }, [triggerHaptic]);

const viewportWorkspaceModule = ['systemlogs', 'drawio', 'prompts'].includes(mainActiveTab);
  const stickyHeaderScrollModule = [
    'server',
    'github',
    'settings',
    'paas',
    'scheduler',
    'uptime',
    'totp',
    'notification',
    'filebox',
    'subscription',
    'openai',
    'apidocs',
    'dns',
    'oracle',
    'aliyun',
    'tencent',
    'm365',
    'adminai',
  ].includes(mainActiveTab);
  const mainCanvasClassName =
    (stickyHeaderScrollModule
      ? 'flex-1 min-w-0 overflow-x-clip px-[var(--app-canvas-gutter-x)] pb-[var(--app-canvas-gutter-bottom)]'
      : viewportWorkspaceModule
        ? 'flex-1 overflow-hidden px-[var(--app-canvas-gutter-x)] pt-[var(--app-canvas-gutter-top)] pb-[var(--app-canvas-gutter-bottom)]'
        : 'flex-1 overflow-x-hidden overflow-y-auto px-[var(--app-canvas-gutter-x)] pt-[var(--app-canvas-gutter-top)] pb-[var(--app-canvas-gutter-bottom)] scrollbar-thin') +
    ' transition-[margin-right] duration-300 ease-in-out lg:mr-[var(--askai-sidebar-w,0px)]';
  // 容器放在画布内层而非 main 自身：container-type(size containment) 与 main 的
  // lg:mr 让位 margin 同元素时在 Chrome 中让位失效（flex item 交互问题），
  // 内层 div 宽度 = main 内容可用宽度，cq-* 断点语义一致。
  const mainCanvasInnerClassName = `@container mx-auto flex w-full min-w-0 flex-col ${
    stickyHeaderScrollModule
      ? 'min-h-full'
      : viewportWorkspaceModule
        ? 'h-full min-h-0'
        : 'min-h-full'
  }`;

  // 渲染当前模块页
  const renderActivePage = () => {
    switch (mainActiveTab) {
      case 'dashboard':
        return <DashboardPage onNavigate={navigateToModule} />;
      case 'openai':
        return <OpenAIPage />;
      case 'subscription':
        return <SubscriptionPage />;

      case 'paas':
        return <PaasPage />;
      case 'dns':
        return <DnsPage />;
      case 'aliyun':
        return <AliyunPage />;
      case 'tencent':
        return <TencentPage />;
      case 'oracle':
        return <OraclePage />;
      case 'm365':
        return <M365Page />;
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
      case 'github':
        return <GitHubPage />;
      case 'settings':
        return <SettingsPage />;
      case 'scheduler':
        return <SchedulerPage onNavigate={navigateToModule} />;
      case 'apidocs':
        return <ApiDocsPage />;
      case 'systemlogs':
        return <SystemLogsPage />;
      case 'drawio':
        return <DrawioPage />;
      case 'prompts':
        return <PromptLibraryPage />;
      case 'adminai':
        return <AdminAIPage />;
      default:
        const ActiveIcon = getModuleIconComponent(mainActiveTab, Server);
        return (
          <AppCard
            padding="none"
            className="mx-auto flex h-[60vh] max-w-xl flex-col items-center justify-center p-6 text-center"
          >
            <div className="mb-5 flex h-16 w-16 items-center justify-center rounded-full border border-kumo-line bg-kumo-recessed text-kumo-brand shadow-none">
              <ActiveIcon className="w-7 h-7" />
            </div>
            <h2 className="text-base font-bold text-kumo-strong mb-2.5">
              {getModuleName(mainActiveTab)} 模块重构中
            </h2>
            <p className="text-xs text-kumo-subtle max-w-sm leading-relaxed">
              页面正在使用 React + Kumo + Tailwind v4 重构，原有逻辑暂时不可用。
            </p>
          </AppCard>
        );
    }
  };

return (
    <Sidebar.Provider
      mobileBreakpoint={1024}
      defaultOpen={!sidebarCollapsed}
      open={isMobileScreen ? undefined : !sidebarCollapsed}
      onOpenChange={handleSidebarOpenChange}
      peekable
      style={{
        '--sidebar-width': '11.5rem',
        '--sidebar-width-icon': '57px',
      }}
      className="app-main-shell flex h-dvh w-screen overflow-hidden text-kumo-default"
    >
      <>
        {/* ==================== 1. 侧边栏 (Sidebar) ==================== */}
        <Sidebar>
          {/* 顶部 Logo */}
          <Sidebar.Header className="h-[58px]! shrink-0 overflow-hidden px-3! transition-[padding] duration-250 ease-[cubic-bezier(0.4,0,0.2,1)] group-data-[state=collapsed]/sidebar:px-2!">
            <SidebarBrand onHome={navigateHome} />
          </Sidebar.Header>

          {/* 导航栏项 */}
          <Sidebar.Content>
            {visibleModuleGroups.map(group => {
              const showGroupLabel = group.id !== 'overview';
              const groupLabel = group.name;

              return (
                <Sidebar.Group key={group.id}>
                  {showGroupLabel ? <Sidebar.GroupLabel>{groupLabel}</Sidebar.GroupLabel> : null}
                  <Sidebar.Menu>
                    {group.modules.map(module => (
                      <SidebarModuleButton
                        key={module}
                        module={module}
                        active={mainActiveTab === module}
                        icon={getModuleIconComponent(module, Server)}
                        onNavigate={navigateToModule}
                      />
                    ))}
                    {(group.subgroups || []).map(subgroup => (
                      <SidebarModuleSubgroup
                        key={subgroup.id}
                        subgroup={subgroup}
                        activeModule={mainActiveTab}
                        onNavigate={navigateToModule}
                      />
                    ))}
                    {(group.trailingModules || []).map(module => (
                      <SidebarModuleButton
                        key={module}
                        module={module}
                        active={mainActiveTab === module}
                        icon={getModuleIconComponent(module, Server)}
                        onNavigate={navigateToModule}
                      />
                    ))}
                  </Sidebar.Menu>
                </Sidebar.Group>
              );
            })}
            <Sidebar.Group className="mt-auto">
              <Sidebar.GroupLabel>系统</Sidebar.GroupLabel>
              <Sidebar.Menu>
                <SidebarModuleSubgroup
                  subgroup={{
                    id: 'global-config',
                    name: '全局配置',
                    icon: Settings,
                    modules: ['notification', 'apidocs', 'systemlogs', 'settings'].filter(
                      module =>
                        module === 'settings' ||
                        (moduleOrder.includes(module) && moduleVisibility[module] !== false)
                    ),
                  }}
                  activeModule={mainActiveTab}
                  onNavigate={navigateToModule}
                />
                <SidebarStyleSwitchItems
                  themeMode={themeMode}
                  onThemeModeChange={setThemeMode}
                />
                <SidebarLogoutButton onLogout={logout} />
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>

          {/* 底部功能栏 */}
          <Sidebar.Footer className="px-[11px]!">
            <Sidebar.Trigger />
          </Sidebar.Footer>
        </Sidebar>

        {/* 顶栏移动端按钮的左侧 Provider 桥 */}
        <LeftSidebarBridge />

        {/* ==================== 2. 主页面区 (Main Panel) + 右侧 Ask AI 侧栏 ==================== */}
        <Sidebar.Provider
          side="right"
          open={showAskAI}
          onOpenChange={(open) => setShowAskAI(open)}
          collapsible="none"
          animationDuration={200}
        >
          <div
          className={`app-main-panel flex-1 flex flex-col h-full ${
            stickyHeaderScrollModule ? 'overflow-x-hidden overflow-y-auto scrollbar-thin' : 'overflow-hidden'
          }`}
        >
          {/* 顶部导航（跟随 Ask AI 侧栏压缩，保证按钮随主视图移动） */}
          <header
            className={`app-main-topbar box-border flex h-[58px] flex-shrink-0 items-center border-b border-kumo-line px-3 @[450px]:px-4 cq-md:px-6 transition-[margin-right] duration-300 ease-in-out lg:mr-[var(--askai-sidebar-w,0px)] ${
              stickyHeaderScrollModule ? 'sticky top-0 z-20' : ''
            }`}
          >
            <div className="flex h-full min-w-0 flex-1 items-center gap-3.5">
              <Button
                type="button"
                size="sm"
                variant="ghost"
                shape="square"
                className="lg:hidden h-8 w-8 shrink-0"
                onClick={() => leftSidebarToggles.forEach((fn) => fn())}
                aria-label="切换侧边栏"
                title="侧边栏"
              >
                <LayoutSidebar className="h-4 w-4" />
              </Button>

<AppPageHeader
                className="flex-row items-center justify-between"
                spacing="compact"
                breadcrumbs={
                  <AppBreadcrumbs size="sm" className="mr-0 min-w-0 overflow-hidden">
                    <AppBreadcrumbs.Link href="/">首页</AppBreadcrumbs.Link>
                    <AppBreadcrumbs.Separator />
                    <AppBreadcrumbs.Current>{getModuleName(mainActiveTab)}</AppBreadcrumbs.Current>
                  </AppBreadcrumbs>
                }
              >
              </AppPageHeader>
              <Button
                onClick={() => store.toggleAskAI()}
                className={`askai-entry-btn ml-auto h-8 w-8 transition-colors duration-200 focus:!ring-0 focus-visible:!ring-0 ${
                  showAskAI ? '!bg-kumo-brand/10 ring-1 ring-kumo-brand/30' : ''
                }`}
                shape="square"
                variant="ghost"
                aria-label="Ask AI"
                title="管理 AI"
              >
                <Sparkle className="askai-entry-sparkle h-5 w-5 text-kumo-brand" />
              </Button>
            </div>
            </header>

          {/* 主内容画布（@container：页面布局按内容实际可用宽度自适应，侧栏让位后自动降级） */}
          <main className={mainCanvasClassName}>
            <div className={mainCanvasInnerClassName}>
              <ModuleErrorBoundary moduleId={mainActiveTab}>
                <Suspense fallback={<PageLoadingFallback />}>{renderActivePage()}</Suspense>
              </ModuleErrorBoundary>
            </div>
          </main>
          {mainActiveTab === 'dashboard' && dashboardFooterVisible && (
            <footer className="app-main-footer flex h-12 shrink-0 items-center justify-between gap-4 border-t border-kumo-line px-3 text-[11px] text-kumo-subtle @[450px]:px-4 cq-md:px-6 transition-[margin-right] duration-300 ease-in-out lg:mr-[var(--askai-sidebar-w,0px)]">
              <div className="flex min-w-0 items-center gap-2">
                <img src="/logo.svg" alt="" className="h-5 w-5 shrink-0 object-contain" />
                <span className="app-brand-wordmark truncate font-semibold text-kumo-strong">API Monitor</span>
                <span className="hidden shrink-0 text-kumo-subtle @[520px]:inline">
                  · 已运行{' '}
                  {appProcessUptimeMeasuredAt > 0
                    ? formatAppProcessUptime(displayedAppProcessUptime)
                    : '加载中'}
                </span>
              </div>
              <div className="flex min-w-0 items-center justify-end gap-3">
                {dashboardFooterRecordNumber ? (
                  <a
                    href="https://beian.miit.gov.cn/"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="min-w-0 truncate text-right text-kumo-subtle transition-colors hover:text-kumo-strong hover:underline"
                  >
                    {dashboardFooterRecordNumber}
                  </a>
                ) : null}
                <a
                  href="https://github.com/iwvw/API-Monitor"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="shrink-0 text-kumo-subtle transition-colors hover:text-kumo-strong hover:underline"
                >
                  GitHub
                </a>
                <span
                  className="shrink-0 font-mono text-[10px] tabular-nums text-kumo-subtle"
                  aria-label={`版本 ${APP_VERSION}`}
                  title={`API Monitor ${APP_VERSION}`}
                >
                  {APP_VERSION}
                </span>
              </div>
            </footer>
          )}
</div>
          <AskAiPanel />
        </Sidebar.Provider>
      </>
    </Sidebar.Provider>
  );
}

export default MainLayout;
