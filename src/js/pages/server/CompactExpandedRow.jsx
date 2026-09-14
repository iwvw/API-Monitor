import { Table } from '@cloudflare/kumo/components/table';
import { AnimatedCollapse } from '../../components/AnimatedCollapse.jsx';

export function CompactExpandedRow({ open, colSpan, children }) {
  return (
    <Table.Row className="border-b border-kumo-line/80 bg-kumo-canvas/45">
      <Table.Cell colSpan={colSpan} className="!p-0">
        <AnimatedCollapse open={open} keepMounted>
          {children}
        </AnimatedCollapse>
      </Table.Cell>
    </Table.Row>
  );
}
