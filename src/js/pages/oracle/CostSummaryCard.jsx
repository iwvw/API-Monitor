import React from 'react';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { formatCurrency } from './utils.js';

export default function CostSummaryCard({ title, amount, month }) {
  return (
    <SectionCard title={title} description={month ? `月份 ${month}` : undefined} className="min-h-0" bodyPadding="none" bodyClassName="flex items-center justify-center p-6">
      <div className="text-center">
        <div className="text-sm font-semibold text-kumo-strong">{formatCurrency(amount)}</div>
        <div className="mt-1 text-xs text-kumo-subtle">当前估算成本</div>
      </div>
    </SectionCard>
  );
}
