import React from 'react';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';

export default function TableSkeletonRows({ columns, rows = 5 }) {
  return Array.from({ length: rows }).map((_, index) => (
    <Table.Row key={index}>
      <Table.Cell colSpan={columns}>
        <SkeletonLine className="h-4 w-full" />
      </Table.Cell>
    </Table.Row>
  ));
}
