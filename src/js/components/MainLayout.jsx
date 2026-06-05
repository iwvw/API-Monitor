import React from 'react';
import useStore, { MODULE_GROUPS, MODULE_CONFIG, getModuleName } from '../store.js';
import DashboardPage from '../pages/DashboardPage.jsx';
import ServerPage from '../pages/ServerPage.jsx';
import TotpPage from '../pages/TotpPage.jsx';
import FileboxPage from '../pages/FileboxPage.jsx';
import {
  Sidebar,
  SidebarProvider,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarTrigger
} from "@cloudflare/kumo/components/sidebar";
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
  MessageSquare,
  Sun,
  Moon,
  LogOut,
  Menu
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
  tencent: Database,
  server: Server,
  'self-h': HardDrive,
  totp: ShieldCheck,
  music: Music,
  uptime: Activity,
  filebox: FolderOpen,
  notification: Bell,
  'ai-chat': MessageSquare,
};

function MainLayout() {
  const {
    mainActiveTab,
    setMainActiveTab,
    sidebarCollapsed,
    setSidebarCollapsed,
    theme,
    setTheme,
    logout,
  } = useStore();

  // 切换主题
  const toggleTheme = () => {
    setTheme(theme === 'dark' ? 'light' : 'dark');
  };

  // 渲染当前模块页
  const renderActivePage = () => {
    switch (mainActiveTab) {
      case 'dashboard':
        return <DashboardPage />;
      case 'server':
        return <ServerPage />;
      case 'totp':
        return <TotpPage />;
      case 'filebox':
        return <FileboxPage />;
      default:
        const ActiveIcon = ICON_MAP[mainActiveTab] || Server;
        return (
          <div className="flex flex-col items-center justify-center h-[60vh] text-center p-6 bg-kumo-base border border-kumo-line rounded-lg max-w-xl mx-auto shadow-sm">
            <div className="w-16 h-16 rounded-full bg-kumo-recessed border border-kumo-line flex items-center justify-center mb-5 text-kumo-brand">
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
    <SidebarProvider
      open={!sidebarCollapsed}
      onOpenChange={(open) => setSidebarCollapsed(!open)}
      className="flex h-screen w-screen overflow-hidden bg-kumo-canvas text-kumo-default"
    >
      {/* ==================== 1. 侧边栏 (Sidebar) ==================== */}
      <Sidebar className="border-r border-kumo-line bg-kumo-base">
        {/* 顶部 Logo */}
        <SidebarHeader className="h-14 flex items-center px-4 border-b border-kumo-line">
          <div className="flex items-center overflow-hidden">
            <img src="/logo.svg" className="w-6 h-6 flex-shrink-0" alt="Logo" />
            <span className="font-bold text-sm text-kumo-strong tracking-wide whitespace-nowrap ml-2.5">
              API Monitor
            </span>
          </div>
        </SidebarHeader>

        {/* 导航栏项 */}
        <SidebarContent>
          {MODULE_GROUPS.map((group) => {
            const isOverview = group.id === 'overview';

            return (
              <SidebarGroup key={group.id} className="space-y-1">
                {/* 菜单组标题 */}
                {!isOverview && (
                  <SidebarGroupLabel>
                    {group.name}
                  </SidebarGroupLabel>
                )}

                <SidebarMenu>
                  {group.modules.map((module) => {
                    const isActive = mainActiveTab === module;
                    const config = MODULE_CONFIG[module];
                    if (!config) return null;

                    const ModuleIcon = ICON_MAP[module] || Server;

                    return (
                      <SidebarMenuItem key={module}>
                        <SidebarMenuButton
                          active={isActive}
                          onClick={() => setMainActiveTab(module)}
                          icon={<ModuleIcon className="w-4 h-4" />}
                          tooltip={config.name}
                        >
                          {config.name}
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    );
                  })}
                </SidebarMenu>
              </SidebarGroup>
            );
          })}
        </SidebarContent>

        {/* 底部功能栏 */}
        <SidebarFooter className="border-t border-kumo-line flex flex-col p-2 gap-1 bg-kumo-base h-auto">
          <SidebarMenu className="w-full">
            <SidebarMenuItem>
              <SidebarMenuButton
                onClick={toggleTheme}
                icon={theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
                tooltip={theme === 'dark' ? '日间模式' : '夜间模式'}
              >
                {theme === 'dark' ? '日间模式' : '夜间模式'}
              </SidebarMenuButton>
            </SidebarMenuItem>

            <SidebarMenuItem>
              <SidebarMenuButton
                onClick={logout}
                className="text-kumo-danger hover:bg-kumo-danger/10"
                icon={<LogOut className="w-4 h-4" />}
                tooltip="安全退出"
              >
                安全退出
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>

          {/* 折叠切换按钮 */}
          <div className="flex justify-center w-full mt-1">
            <SidebarTrigger />
          </div>
        </SidebarFooter>
      </Sidebar>

      {/* ==================== 2. 主页面区 (Main Panel) ==================== */}
      <div className="flex-1 flex flex-col h-full overflow-hidden">
        {/* 顶部导航 */}
        <header className="h-14 bg-kumo-base border-b border-kumo-line flex items-center justify-between px-6 flex-shrink-0">
          <div className="flex items-center gap-3.5">
            <SidebarTrigger className="lg:hidden" />
            
            <div className="flex items-center gap-1.5 text-xs text-kumo-subtle font-medium select-none">
              <span>DSUK</span>
              <span className="text-kumo-subtle/40">/</span>
              <span className="text-kumo-strong font-semibold">
                {getModuleName(mainActiveTab)}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-4">
            <div className="flex items-center gap-1.5 text-[11px] text-kumo-success bg-kumo-success/10 px-2 py-0.5 rounded-md border border-kumo-success/20">
              <span className="w-1 h-1 rounded-full bg-current animate-pulse"></span>
              系统正常运行
            </div>
          </div>
        </header>

        {/* 主内容画布 */}
        <main className="flex-1 overflow-y-auto p-6 lg:p-8 scrollbar-thin">
          {renderActivePage()}
        </main>
      </div>
    </SidebarProvider>
  );
}

export default MainLayout;
