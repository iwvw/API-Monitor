import React from 'react';
import { Breadcrumbs, Tabs } from '@cloudflare/kumo';

const spacingClass = {
  compact: 'gap-1',
  base: 'gap-2',
  relaxed: 'gap-4',
};

function AppPageHeader({
  breadcrumbs,
  tabs,
  value,
  onValueChange,
  children,
  spacing = 'base',
  className = '',
}) {
  return (
    <div className={`flex min-w-0 flex-1 items-center ${spacingClass[spacing] || spacingClass.base} ${className}`}>
      <div className="flex min-w-0 flex-1 items-center overflow-hidden">{breadcrumbs}</div>
      {(tabs || children) && (
        <div className="flex shrink-0 items-center justify-end gap-2 min-[520px]:gap-3">
          {tabs && (
            <Tabs
              variant="segmented"
              size="sm"
              value={value}
              onValueChange={onValueChange}
              tabs={tabs}
            />
          )}
          {children}
        </div>
      )}
    </div>
  );
}

export const AppBreadcrumbs = Breadcrumbs;
export default AppPageHeader;
