import React from 'react';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';

function ActionFlowPlaceholder() {
  return (
    <div className="flex min-h-[260px] items-center justify-center rounded-md border border-kumo-line bg-kumo-base px-8">
      <div className="flex w-full max-w-4xl items-center justify-center gap-4">
        {[0, 1, 2, 3].map((item) => (
          <React.Fragment key={item}>
            <div className="w-52 rounded-md border border-kumo-line bg-kumo-base p-3">
              <div className="flex items-center justify-between gap-3">
                <SkeletonLine className="h-4 w-24" />
                <SkeletonLine className="h-5 w-14 rounded-full" />
              </div>
              <SkeletonLine className="mt-3 h-3 w-28" />
              <SkeletonLine className="mt-5 h-3 w-36" />
            </div>
            {item < 3 && <SkeletonLine className="h-1 w-12 shrink-0 rounded-full" />}
          </React.Fragment>
        ))}
      </div>
    </div>
  );
}

export default ActionFlowPlaceholder;