import { AppCard } from '../../components/ui/AppPrimitives.jsx';

export const EXPANDED_SECTION_ACCENTS = {
  brand: 'bg-brand',
  success: 'bg-kumo-success',
  warning: 'bg-kumo-warning',
  info: 'bg-kumo-info',
  danger: 'bg-kumo-danger',
};

export function ExpandedSection({ title, tone = 'brand', action, className = '', children }) {
  return (
    <AppCard as="section" padding="none" className={`min-w-0 overflow-hidden p-1.5 ${className}`}>
      <div className="mb-2 flex min-w-0 items-center justify-between gap-2">
        <h4 className="flex min-w-0 items-center gap-1.5 text-xs font-semibold text-kumo-strong">
          <span className={`h-3 w-1 shrink-0 rounded-full ${EXPANDED_SECTION_ACCENTS[tone] || EXPANDED_SECTION_ACCENTS.brand}`}></span>
          <span className="truncate">{title}</span>
        </h4>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      {children}
    </AppCard>
  );
}
