import React from 'react';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';

export default function DetailGridSkeleton() {
  return (
    <div className="min-h-0 flex-1 overflow-auto scrollbar-thin">
      <div className="divide-y divide-kumo-line/80">
        {Array.from({ length: 8 }).map((_, index) => (
          <div key={index} className="grid grid-cols-[108px_minmax(0,1fr)] items-center gap-3 px-4 py-2.5">
            <SkeletonLine className="h-4 w-16" />
            <SkeletonLine className="h-4 w-full" />
          </div>
        ))}
      </div>
    </div>
  );
}
