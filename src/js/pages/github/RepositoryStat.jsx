import { LayerCard, Text } from '@cloudflare/kumo';

function RepositoryMetric({ icon, label, value, detail }) {
  return (
    <LayerCard className="min-w-0 self-start px-3 py-2 shadow-none">
      <div className="flex min-w-0 items-center gap-1.5 text-kumo-subtle">
        {icon}
        <Text variant="secondary" size="sm" truncate>{label}</Text>
      </div>
      <div className="mt-1 flex min-w-0 items-baseline justify-between gap-2">
        <Text variant="heading" as="span" truncate>{value}</Text>
        {detail && <Text variant="secondary" size="xs" truncate>{detail}</Text>}
      </div>
    </LayerCard>
  );
}

function RepositoryStat({ label, value }) {
  return (
    <LayerCard className="min-w-0 p-2 text-center shadow-none">
      <div className="truncate text-[10px] font-medium text-kumo-subtle">{label}</div>
      <div className="truncate text-xs font-semibold text-kumo-strong">{value}</div>
    </LayerCard>
  );
}

export default RepositoryStat;
export { RepositoryMetric };