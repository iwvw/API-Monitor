import { StatusBadge } from '../../components/ui/AppPrimitives.jsx';

export function keyCheckBadgeProps(check) {
  if (!check) return null;
  switch (check.status) {
    case 'checking':
      return { tone: 'info', label: '检测中' };
    case 'valid':
      return { tone: 'success', label: '有效' };
    case 'invalid':
      return { tone: 'danger', label: '失效' };
    case 'overdue':
      return { tone: 'warning', label: '欠费' };
    default:
      return { tone: 'neutral', label: '异常' };
  }
}

export function KeyStatusBadge({ check }) {
  const props = keyCheckBadgeProps(check);
  if (!props) return <span className="w-9 shrink-0" />;
  return (
    <StatusBadge tone={props.tone} title={check?.message} className="shrink-0">
      {props.label}
    </StatusBadge>
  );
}
