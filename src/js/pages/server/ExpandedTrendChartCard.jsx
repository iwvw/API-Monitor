import { ChartBoundaryBox } from '../../components/ui/AppPrimitives.jsx';
import { EXPANDED_SECTION_ACCENTS } from './ExpandedSection.jsx';

export function ExpandedTrendChartCard({ title, tone = 'brand', legend, compact = false, className = '', children }) {
  const accentClassName = EXPANDED_SECTION_ACCENTS[tone] || EXPANDED_SECTION_ACCENTS.brand;
  const headerHeightClassName = compact ? 'min-h-2' : 'min-h-2';
  const legendGapClassName = compact ? 'gap-x-2 gap-y-0.5' : 'gap-x-2.5 gap-y-0.5';

  return (
    <ChartBoundaryBox className={`min-w-0 overflow-hidden rounded-lg border border-kumo-line/90 bg-kumo-base p-1.5 shadow-none ${compact ? 'rounded-md' : ''} h-full ${className}`}>
      {(tooltipBoundary) => (
        <div className="flex h-full min-w-0 flex-col">
          <div className={`grid min-w-0 grid-cols-[minmax(0,max-content)_minmax(0,1fr)] items-center gap-2 overflow-hidden ${headerHeightClassName}`}>
            <h4 className="flex min-w-0 items-center gap-1.5 text-xs font-semibold text-kumo-strong">
              <span className={`h-3 w-1 shrink-0 rounded-full ${accentClassName}`}></span>
              <span className="truncate">{title}</span>
            </h4>
            {legend && (
              <div className="flex min-w-0 justify-end overflow-hidden">
                <div className={`flex min-w-0 flex-wrap items-center justify-end text-[11px] leading-none ${legendGapClassName}`}>
                  {legend}
                </div>
              </div>
            )}
          </div>
          <div className="mt-1 min-w-0 shrink-0">
            {typeof children === 'function' ? children(tooltipBoundary) : children}
          </div>
        </div>
      )}
    </ChartBoundaryBox>
  );
}
