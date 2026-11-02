import React from 'react';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Activity, getModuleIconComponent } from '../../components/Icons.jsx';
import { ResponsiveSearchInput, SectionCard, cx } from '../../components/ui/AppPrimitives.jsx';

export function ModulesPanel({ filteredModuleRows, moduleGroups, moduleSearch, setModuleSearch, settings, toggleModule }) {
  return (
        <div className="min-h-0 overflow-auto cq-md:h-full">
        <SectionCard
          className="flex min-h-0 cq-md:h-full"
          headerClassName="max-sm:min-h-12 max-sm:flex-row max-sm:items-center max-sm:px-3 max-sm:py-2"
          title="功能模块"
          icon={<Activity className="h-4 w-4 text-brand" />}
          actionsClassName="max-sm:ml-auto max-sm:w-auto max-sm:gap-1.5"
          actions={
              <>
                <ResponsiveSearchInput
                  value={moduleSearch}
                  onChange={(event) => setModuleSearch(event.target.value)}
                  placeholder="搜索模块"
                  ariaLabel="搜索模块"
                  className="cq-sm:w-52"
                />
              </>
          }
          bodyClassName="flex min-h-0 flex-1 flex-col gap-3 overflow-auto"
        >
          <div className="flex flex-col gap-3 cq-sm:gap-4">
            {moduleGroups.map((group) => {
              const groupRows = filteredModuleRows.filter((row) => row.groupId === group.id);
              if (groupRows.length === 0) return null;

              return (
                <section key={group.id} className="min-w-0">
                  <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-kumo-subtle">
                    <span>{group.name}</span>
                    <span className="h-px min-w-4 flex-1 bg-kumo-line" />
                    <span className="font-normal">{groupRows.length} 项</span>
                  </div>
                  <div className="grid gap-1.5 cq-lg:grid-cols-2 cq-xl:grid-cols-3">
                    {groupRows.map((row) => {
                      const ModuleIcon = getModuleIconComponent(row.id);
                      const isVisible = settings.moduleVisibility[row.id] !== false;

                      return (
                        <div key={row.id} className={cx('flex min-h-15 items-center gap-2.5 rounded-md border px-2.5 py-2 cq-sm:min-h-16 cq-sm:gap-3 cq-sm:px-3', isVisible ? 'border-kumo-line bg-kumo-base' : 'border-kumo-line/70 bg-kumo-recessed/35 opacity-75')}>
                          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-kumo-line bg-kumo-recessed text-brand">
                            <ModuleIcon className="h-4 w-4" />
                          </div>
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-sm font-semibold text-kumo-strong">{row.config.name}</div>
                            <div className="hidden truncate text-xs text-kumo-subtle cq-sm:block">{row.config.description}</div>
                          </div>
                          <Switch
                            checked={isVisible}
                            onCheckedChange={(checked) => toggleModule(row.id, checked)}
                            disabled={row.id === 'dashboard'}
                            aria-label={`切换 ${row.config.name}`}
                          />
                        </div>
                      );
                    })}
                  </div>
                </section>
              );
            })}
          </div>
          {filteredModuleRows.length === 0 && (
            <div className="rounded-lg border border-dashed border-kumo-line p-8 text-center text-sm text-kumo-subtle">没有匹配模块，请调整搜索。</div>
          )}
        </SectionCard>
        </div>
  );
}
