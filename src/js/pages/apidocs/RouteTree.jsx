import React, { useEffect, useMemo, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { AppCard, EmptyState, SectionCard, StatusBadge, cx } from '../../components/ui/AppPrimitives.jsx';
import {
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Folder,
  FolderOpen,
  Search,
} from '../../components/Icons.jsx';
import { RouteMethodPills } from './components.jsx';
import { AUTH_LABEL, AUTH_TONE, STATUS_LABEL, STATUS_TONE } from './constants.js';
import { getRouteKey, moduleLabel, sectionOfGroup } from './utils.js';

export default function RouteTree({ routes, selectedRoute, onSelect, revealAll }) {
  const selectedKey = selectedRoute ? getRouteKey(selectedRoute) : '';

  const tree = useMemo(() => {
    const sectionMap = new Map();
    routes.forEach(route => {
      const groupName = route.group || '基础';
      const sectionName = sectionOfGroup(groupName);
      if (!sectionMap.has(sectionName)) sectionMap.set(sectionName, new Map());
      const groupMap = sectionMap.get(sectionName);
      if (!groupMap.has(groupName)) groupMap.set(groupName, new Map());
      const moduleMap = groupMap.get(groupName);
      const moduleName = route.module || '';
      if (!moduleMap.has(moduleName)) moduleMap.set(moduleName, []);
      moduleMap.get(moduleName).push(route);
    });
    return [...sectionMap.entries()].map(([section, groupMap]) => ({
      section,
      count: [...groupMap.values()].reduce(
        (acc, moduleMap) => acc + [...moduleMap.values()].reduce((sum, list) => sum + list.length, 0),
        0
      ),
      groups: [...groupMap.entries()].map(([group, moduleMap]) => ({
        group,
        count: [...moduleMap.values()].reduce((acc, list) => acc + list.length, 0),
        modules: [...moduleMap.entries()].map(([module, list]) => ({
          module,
          label: moduleLabel(module),
          count: list.length,
          routes: list,
        })),
      })),
    }));
  }, [routes]);

  const [collapsedSections, setCollapsedSections] = useState(() => new Set());
  const [collapsedGroups, setCollapsedGroups] = useState(() => new Set());
  const [collapsedModules, setCollapsedModules] = useState(() => new Set());

  // 数据异步到达前 tree 为空，此时 collapsed 集为空会误判为「全部展开」。
  // 数据到达（或搜索态切换）时重置三份折叠集为「全折叠」，之后由用户交互接管。
  useEffect(() => {
    if (tree.length === 0) return;
    setCollapsedSections(new Set(tree.map(item => item.section)));
    setCollapsedGroups(new Set(tree.flatMap(item => item.groups.map(group => group.group))));
    setCollapsedModules(
      new Set(tree.flatMap(item => item.groups.flatMap(group => group.modules.map(mod => `${group.group}\u0000${mod.module}`))))
    );
  }, [tree, revealAll]);

  // 用户点击选中某路由后，自动展开其所在层级；初始未选择时保持全部折叠。
  useEffect(() => {
    if (!selectedRoute || !selectedKey) return;
    const sectionName = sectionOfGroup(selectedRoute.group);
    const groupName = selectedRoute.group || '基础';
    const moduleName = selectedRoute.module || '';
    setCollapsedSections(current => {
      if (!current.has(sectionName)) return current;
      const next = new Set(current);
      next.delete(sectionName);
      return next;
    });
    setCollapsedGroups(current => {
      if (!current.has(groupName)) return current;
      const next = new Set(current);
      next.delete(groupName);
      return next;
    });
    setCollapsedModules(current => {
      const key = `${groupName}\u0000${moduleName}`;
      if (!current.has(key)) return current;
      const next = new Set(current);
      next.delete(key);
      return next;
    });
  }, [selectedRoute, selectedKey]);

  const toggleSection = sectionName => {
    setCollapsedSections(current => {
      const next = new Set(current);
      if (next.has(sectionName)) next.delete(sectionName);
      else next.add(sectionName);
      return next;
    });
  };

  const toggleGroup = groupName => {
    setCollapsedGroups(current => {
      const next = new Set(current);
      if (next.has(groupName)) next.delete(groupName);
      else next.add(groupName);
      return next;
    });
  };

  const toggleModule = (groupName, moduleName) => {
    const key = `${groupName}\u0000${moduleName}`;
    setCollapsedModules(current => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const expandAll = () => {
    setCollapsedSections(new Set());
    setCollapsedGroups(new Set());
    setCollapsedModules(new Set());
  };

  const collapseAll = () => {
    setCollapsedSections(new Set(tree.map(item => item.section)));
    setCollapsedGroups(new Set(tree.flatMap(item => item.groups.map(group => group.group))));
    setCollapsedModules(
      new Set(tree.flatMap(item => item.groups.map(group => `${group.group}\u0000${group.module}`)))
    );
  };

  const sectionCollapsed = name => !revealAll && collapsedSections.has(name);
  const groupCollapsed = name => !revealAll && collapsedGroups.has(name);
  const moduleCollapsed = (groupName, moduleName) =>
    !revealAll && collapsedModules.has(`${groupName}\u0000${moduleName}`);

  return (
    <SectionCard
      title={`接口目录 (${routes.length})`}
      icon={<Search className="h-4 w-4 text-brand" />}
      action={
        <div className="flex items-center gap-1">
          <Button size="sm" variant="ghost" onClick={expandAll} className="gap-1">
            <ChevronDown className="h-3.5 w-3.5" />
            <span className="hidden cq-sm:inline">展开</span>
          </Button>
          <Button size="sm" variant="ghost" onClick={collapseAll} className="gap-1">
            <ChevronUp className="h-3.5 w-3.5" />
            <span className="hidden cq-sm:inline">折叠</span>
          </Button>
        </div>
      }
      className="min-h-0 self-start"
      bodyPadding="none"
      bodyClassName="flex min-w-0 flex-col"
    >
      {routes.length === 0 ? (
        <AppCard padding="none" className="flex min-h-0 items-center justify-center">
          <EmptyState
            icon={Search}
            title="没有匹配的接口"
            description="换个筛选条件试试"
            card={false}
            className="min-h-0"
          />
        </AppCard>
      ) : (
        <div className="divide-y divide-kumo-line/80">
          {tree.map(sectionItem => (
            <div key={sectionItem.section}>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => toggleSection(sectionItem.section)}
                className="h-auto w-full min-w-0 !justify-between gap-1.5 rounded-none px-3 py-2 text-left"
              >
                <span className="flex min-w-0 items-center gap-2">
                  {sectionCollapsed(sectionItem.section) ? (
                    <ChevronRight className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />
                  ) : (
                    <ChevronDown className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />
                  )}
                  {sectionCollapsed(sectionItem.section) ? (
                    <Folder className="h-4 w-4 shrink-0 text-brand/80" />
                  ) : (
                    <FolderOpen className="h-4 w-4 shrink-0 text-brand" />
                  )}
                  <span className="truncate text-xs font-semibold text-kumo-strong">
                    {sectionItem.section}
                  </span>
                </span>
                <StatusBadge tone="neutral">{sectionItem.count}</StatusBadge>
              </Button>
              {!sectionCollapsed(sectionItem.section) && (
                <div className="border-t border-kumo-line/50">
                  {sectionItem.groups.map(groupItem => (
                    <div key={groupItem.group}>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        title={groupItem.group}
                        onClick={() => toggleGroup(groupItem.group)}
                        className="h-auto w-full min-w-0 !justify-between gap-1.5 rounded-none py-1.5 pl-7 pr-3 text-left"
                      >
                        <span className="flex min-w-0 items-center gap-1.5">
                          {groupCollapsed(groupItem.group) ? (
                            <ChevronRight className="h-3 w-3 shrink-0 text-kumo-subtle" />
                          ) : (
                            <ChevronDown className="h-3 w-3 shrink-0 text-kumo-subtle" />
                          )}
                          <span className="truncate text-[11px] font-semibold text-kumo-subtle">
                            {groupItem.group}
                          </span>
                        </span>
                        <StatusBadge tone="neutral">{groupItem.count}</StatusBadge>
                      </Button>
                      {!groupCollapsed(groupItem.group) && (
                        <div className="border-t border-kumo-line/40">
                          {groupItem.modules.map(moduleItem => (
                            <div key={moduleItem.module}>
                              <Button
                                type="button"
                                size="sm"
                                variant="ghost"
                                title={moduleItem.module}
                                onClick={() => toggleModule(groupItem.group, moduleItem.module)}
                                className="h-auto w-full min-w-0 !justify-between gap-1.5 rounded-none py-1.5 pl-11 pr-3 text-left"
                              >
                                <span className="flex min-w-0 items-center gap-1.5">
                                  {moduleCollapsed(groupItem.group, moduleItem.module) ? (
                                    <ChevronRight className="h-3 w-3 shrink-0 text-kumo-subtle" />
                                  ) : (
                                    <ChevronDown className="h-3 w-3 shrink-0 text-kumo-subtle" />
                                  )}
                                  <span className="truncate text-[11px] font-semibold text-kumo-subtle">
                                    {moduleItem.label}
                                  </span>
                                </span>
                                <StatusBadge tone="neutral">{moduleItem.count}</StatusBadge>
                              </Button>
                              {!moduleCollapsed(groupItem.group, moduleItem.module) && (
                                <div className="border-t border-kumo-line/40">
                                  {moduleItem.routes.map(route => {
                                    const active = selectedKey === getRouteKey(route);
                                    return (
                                      <Button
                                        key={getRouteKey(route)}
                                        type="button"
                                        size="sm"
                                        variant="ghost"
                                        onClick={() => onSelect(route)}
                                        className={cx(
                                          'h-auto w-full min-w-0 flex-col items-stretch gap-1 rounded-none py-2 pl-14 pr-3 text-left',
                                          active && 'bg-brand/10'
                                        )}
                                      >
<div className="flex min-w-0 items-center justify-between gap-2">
                                          <div className="min-w-0 truncate font-mono text-xs font-semibold text-kumo-strong">
                                            {route.prefix}
                                          </div>
                                          <StatusBadge tone={STATUS_TONE[route.status]}>
                                            {STATUS_LABEL[route.status] || route.status}
                                          </StatusBadge>
                                        </div>
                                        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                                          <RouteMethodPills methods={route.methods} />
                                          <StatusBadge tone={AUTH_TONE[route.auth]}>
                                            {AUTH_LABEL[route.auth] || route.auth}
                                          </StatusBadge>
                                        </div>
                                        <div className="line-clamp-1 text-xs leading-relaxed text-kumo-subtle">
                                          {route.description}
                                        </div>
                                      </Button>
                                    );
                                  })}
                                </div>
                              )}
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </SectionCard>
  );
}
