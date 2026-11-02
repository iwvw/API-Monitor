import React from 'react';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Activity, Clock, Columns, Database, GitHubBrand, Globe, HardDrive, LayoutDashboard, Terminal } from '../../components/Icons.jsx';
import { APP_BUILD_TIME, APP_VERSION, FRAMEWORK_VERSIONS } from '../../modules/appVersion.js';

export function AboutPanel({ currentOrigin, healthInfo }) {
  return (
        <div className="grid items-start gap-4 overflow-auto cq-lg:grid-cols-1">
          <SectionCard
            title={<span className="app-brand-wordmark">API Monitor</span>}
            icon={<img src="/logo.svg" alt="" className="h-6 w-6 object-contain" />}
          >
            <div className="grid gap-3 cq-sm:grid-cols-2">
              <div className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                <div className="flex items-center gap-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                    <HardDrive className="h-3.5 w-3.5" />
                  </span>
                  <span className="text-xs font-medium text-kumo-subtle">当前版本</span>
                </div>
                <span className="truncate font-mono text-sm leading-6 text-kumo-strong">{APP_VERSION}</span>
              </div>
              <div className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                <div className="flex items-center gap-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                    <Clock className="h-3.5 w-3.5" />
                  </span>
                  <span className="text-xs font-medium text-kumo-subtle">构建时间</span>
                </div>
                <span className="truncate font-mono text-sm leading-6 text-kumo-strong">
                  {APP_BUILD_TIME ? new Date(APP_BUILD_TIME).toLocaleString() : '未知'}
                </span>
              </div>
            </div>
            <div className="mt-3 grid gap-3 cq-sm:grid-cols-2 cq-lg:grid-cols-3">
              {[
                { label: 'React', value: FRAMEWORK_VERSIONS.react, icon: <Activity className="h-3.5 w-3.5" /> },
                { label: 'Vite', value: FRAMEWORK_VERSIONS.vite, icon: <Terminal className="h-3.5 w-3.5" /> },
                { label: 'Tailwind CSS', value: FRAMEWORK_VERSIONS.tailwind, icon: <Columns className="h-3.5 w-3.5" /> },
                { label: 'Kumo', value: FRAMEWORK_VERSIONS.kumo, icon: <LayoutDashboard className="h-3.5 w-3.5" /> },
                { label: 'Zustand', value: FRAMEWORK_VERSIONS.zustand, icon: <Database className="h-3.5 w-3.5" /> },
                { label: 'Go 后端', value: healthInfo?.goVersion || '…', icon: <Globe className="h-3.5 w-3.5" /> },
              ].map((item) => (
                <div key={item.label} className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                  <div className="flex items-center gap-2">
                    <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                      {item.icon}
                    </span>
                    <span className="text-xs font-medium text-kumo-subtle">{item.label}</span>
                  </div>
                  <span className="truncate font-mono text-sm leading-6 text-kumo-strong">{item.value || '-'}</span>
                </div>
              ))}
            </div>
            <div className="mt-3 grid gap-3 cq-sm:grid-cols-2 cq-lg:grid-cols-3">
              <div className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                <div className="flex items-center gap-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                    <Globe className="h-3.5 w-3.5" />
                  </span>
                  <span className="text-xs font-medium text-kumo-subtle">当前源</span>
                </div>
                <span className="truncate font-mono text-sm leading-6 text-kumo-strong">{currentOrigin}</span>
              </div>
              <div className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                <div className="flex items-center gap-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                    <Terminal className="h-3.5 w-3.5" />
                  </span>
                  <span className="text-xs font-medium text-kumo-subtle">API 地址</span>
                </div>
                <span className="truncate font-mono text-sm leading-6 text-kumo-strong">{`${currentOrigin}/api`}</span>
              </div>
              <div className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                <div className="flex items-center gap-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                    <GitHubBrand className="h-3.5 w-3.5" />
                  </span>
                  <span className="text-xs font-medium text-kumo-subtle">仓库地址</span>
                </div>
                <a
                  href="https://github.com/iwvw/API-Monitor"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="truncate font-mono text-sm leading-6 text-kumo-strong hover:text-brand hover:underline"
                >
                  https://github.com/iwvw/API-Monitor
                </a>
              </div>
            </div>
          </SectionCard>

          {/* <LayerCard className="p-6">
            <h2 className="text-base font-semibold text-kumo-strong">已对接接口</h2>
            <div className="mt-4 grid gap-2 text-xs text-kumo-default">
              {[
                '/api/settings',
                '/api/settings/log-settings',
                '/api/settings/database-stats',
                '/api/settings/database-analysis',
                '/api/auth/change-password',
                '/api/auth/2fa/*',
              ].map((item) => (
                <div key={item} className="flex items-center gap-2">
                  <Check className="h-3.5 w-3.5 text-kumo-success" />
                  <span className="font-mono">{item}</span>
                </div>
              ))}
            </div>
          </LayerCard> */}
        </div>
  );
}
