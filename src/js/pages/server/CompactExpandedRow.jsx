import { Table } from '@cloudflare/kumo/components/table';
import { AnimatedCollapse } from '../../components/AnimatedCollapse.jsx';

export function CompactExpandedRow({ open, colSpan, children }) {
  return (
    <Table.Row className={`${open ? 'border-b border-kumo-line/80' : 'border-b-0'} bg-kumo-canvas/45`}>
      <Table.Cell colSpan={colSpan} className="!p-0">
        <AnimatedCollapse open={open} keepMounted>
          {children}
        </AnimatedCollapse>
      </Table.Cell>
    </Table.Row>
  );
}
