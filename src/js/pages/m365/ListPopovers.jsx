import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Popover } from '@cloudflare/kumo';
import { InlineCopyText } from '@cloudflare/kumo/components/inline-copy-text';
import { Copy } from '../../components/Icons.jsx';
import { normalizeDomainValue } from './utils.js';

export function ItemListPopover({
  items,
  copyText,
  label = '列表',
  emptyText = '暂无内容',
  unitLabel = '个',
  codeStyle = false,
}) {
  return (
    <Popover>
      <Popover.Trigger
        render={
          <Button
            size="sm"
            variant="secondary"
            className="w-full justify-between"
            disabled={items.length === 0}
          >
            <span>{items.length > 0 ? `共 ${items.length} ${unitLabel}` : '暂无'}</span>
            <span className="text-[10px] text-kumo-subtle">查看</span>
          </Button>
        }
      />
      <Popover.Content side="bottom" align="start" className="w-[min(24rem,calc(100vw-2rem))] p-3">
        <div className="mb-2 flex items-center justify-between gap-2">
          <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
            {label}
          </Popover.Title>
          <Button
            size="sm"
            variant="secondary"
            disabled={items.length === 0}
            icon={<Copy className="h-3.5 w-3.5" />}
            onClick={() => copyText(items.join('\n'), `${label}已复制`)}
          >
            全部
          </Button>
        </div>
        <div className="grid gap-2">
          {items.length > 0 ? (
            items.map((item, index) => (
              <div
                key={`${item}-${index}`}
                className="flex min-w-0 items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2"
              >
                <InlineCopyText
                  value={item}
                  variant={codeStyle ? 'mono-secondary' : 'secondary'}
                  size={codeStyle ? 'lg' : undefined}
                  truncate
                  className="min-w-0 flex-1"
                  labels={{ copyAction: `复制 ${item}`, copied: `${item} 已复制` }}
                >
                  {item}
                </InlineCopyText>
              </div>
            ))
          ) : (
            <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-3 text-center text-xs text-kumo-subtle">
              {emptyText}
            </div>
          )}
        </div>
      </Popover.Content>
    </Popover>
  );
}

export function DomainListPopover({ domains, copyText, label = '域名列表' }) {
  const items = Array.isArray(domains)
    ? Array.from(new Set(domains.map(item => normalizeDomainValue(item)).filter(Boolean)))
    : [];

  return (
    <ItemListPopover
      items={items}
      copyText={copyText}
      label={label}
      emptyText="暂无域名"
      unitLabel="个"
      codeStyle
    />
  );
}

export function LicenseListPopover({ licenses, copyText, label = '许可证列表' }) {
  const items = Array.isArray(licenses)
    ? Array.from(new Set(licenses.map(item => String(item || '').trim()).filter(Boolean)))
    : [];

  return (
    <ItemListPopover
      items={items}
      copyText={copyText}
      label={label}
      emptyText="暂无许可证"
      unitLabel="项"
    />
  );
}
