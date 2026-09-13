import React from 'react';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppCard, cx, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Users } from '../../components/Icons.jsx';
import {
  panelBodyClass,
  scrollViewportClass,
  tableFrameClass,
  tenantGridClass,
} from './constants.js';

export function SkuGridSkeleton() {
  return (
    <div
      className={cx(scrollViewportClass, 'grid auto-rows-max content-start gap-2.5 pr-1')}
      style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))' }}
    >
      {Array.from({ length: 6 }).map((_, index) => (
        <div
          key={index}
          className="rounded-lg border border-kumo-line/70 bg-kumo-base/95 px-3 py-2.5"
        >
          <SkeletonLine className="h-5 w-3/5" />
          <div className="mt-2 flex flex-wrap gap-2">
            <SkeletonLine className="h-3 w-20" />
            <SkeletonLine className="h-3 w-16" />
            <SkeletonLine className="h-3 w-14" />
          </div>
          <SkeletonLine className="mt-3 h-1.5 w-full rounded-full" />
        </div>
      ))}
    </div>
  );
}

export function TenantGridSkeleton() {
  return (
    <div
      className={cx(
        scrollViewportClass,
        'grid auto-rows-max content-start items-start gap-3 p-1',
        tenantGridClass
      )}
    >
      {Array.from({ length: 4 }).map((_, index) => (
        <div key={index} className="rounded-xl border border-kumo-line/80 bg-kumo-base/95 p-3">
          <div className="flex items-start justify-between gap-3">
            <div className="flex min-w-0 items-center gap-2">
              <SkeletonLine className="h-9 w-9 rounded-lg" />
              <div className="min-w-0 space-y-2">
                <SkeletonLine className="h-4 w-32" />
                <SkeletonLine className="h-3 w-24" />
              </div>
            </div>
            <SkeletonLine className="h-6 w-14 rounded-full" />
          </div>
          <div className="mt-3 space-y-2 rounded-lg border border-kumo-line/60 bg-kumo-recessed/20 p-2">
            <SkeletonLine className="h-3 w-full" />
            <SkeletonLine className="h-3 w-5/6" />
            <SkeletonLine className="h-3 w-4/5" />
          </div>
          <div className="mt-3 flex items-center justify-between gap-2">
            <SkeletonLine className="h-3 w-16" />
            <div className="flex gap-2">
              <SkeletonLine className="h-8 w-8 rounded-md" />
              <SkeletonLine className="h-8 w-12 rounded-md" />
              <SkeletonLine className="h-8 w-8 rounded-md" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

export function CardTableSkeleton({ rows = 6, showToolbar = false }) {
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      {showToolbar ? (
        <div className="flex items-center gap-2">
          <SkeletonLine className="h-8 w-44" />
          <SkeletonLine className="h-8 w-24" />
        </div>
      ) : null}
      <div className={cx(tableFrameClass, 'rounded-lg border border-kumo-line/80 bg-kumo-base')}>
        <div className="space-y-3 p-3">
          <SkeletonLine className="h-9 w-full" />
          {Array.from({ length: rows }).map((_, index) => (
            <SkeletonLine key={index} className="h-10 w-full" />
          ))}
        </div>
      </div>
    </div>
  );
}

export function GroupsTabSkeleton() {
  return (
    <div className="grid min-h-0 flex-1 gap-4 cq-lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
      <AppCard padding="none" className="flex min-h-0 flex-col">
        <div className="space-y-3 p-3">
          <SkeletonLine className="h-9 w-full" />
          {Array.from({ length: 6 }).map((_, index) => (
            <SkeletonLine key={index} className="h-12 w-full" />
          ))}
        </div>
      </AppCard>

      <SectionCard
        className="flex min-h-0 flex-col"
        bodyClassName={panelBodyClass}
        title="组成员"
        description="输入成员对象 ID"
        icon={<Users className="h-4 w-4" />}
        bodyPadding="sm"
        action={
          <div className="flex items-center gap-2">
            <SkeletonLine className="h-8 w-44" />
            <SkeletonLine className="h-8 w-24" />
          </div>
        }
      >
        <CardTableSkeleton rows={5} showToolbar />
      </SectionCard>
    </div>
  );
}
