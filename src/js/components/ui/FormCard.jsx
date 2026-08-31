import React from 'react';
import { LayerCard } from '@cloudflare/kumo';
import { sectionCardHeaderClass } from './AppPrimitives.jsx';

/* 弹窗内分组卡（与设置页 SectionCard 同语言：圆角 + 抬升底 + 图标方角标题） */
export default function FormCard({ icon, title, description, children, className = '' }) {
  return (
    <LayerCard className={`flex flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0 ${className}`}>
      <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0`}>
        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-brand">
          {icon}
        </span>
        <div className="min-w-0">
          <div className="text-sm font-semibold text-kumo-strong">{title}</div>
          {description && <div className="truncate text-xs text-kumo-subtle">{description}</div>}
        </div>
      </LayerCard.Secondary>
      <LayerCard.Primary className="gap-0 overflow-visible bg-kumo-elevated px-4 pt-0 pb-0 ring-0">
        {children}
      </LayerCard.Primary>
    </LayerCard>
  );
}
