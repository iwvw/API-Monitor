import React from 'react';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { DataTableFrame } from '../../components/ui/AppPrimitives.jsx';

export default function SkeletonLines() {
  return (
    <DataTableFrame variant="embedded" density="dense">
      <Table>
        <Table.Body>
          {[0, 1, 2].map((i) => (
            <Table.Row key={i}>
              <Table.Cell colSpan={7}><SkeletonLine className="h-5 w-full" /></Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </DataTableFrame>
  );
}
