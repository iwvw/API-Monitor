import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Download, Play, RotateCw, Settings, Shield, Square, Trash } from '../../components/Icons.jsx';

export function InstanceActions({ instance, onRunAction }) {
  const running = instance.state === 'RUNNING';
  const terminated = instance.state === 'TERMINATED';
  return (
    <div className="flex items-center justify-center gap-1">
      {!running && !terminated && <span className="px-1 text-xs text-kumo-subtle">处理中</span>}
      {!running && (
        <Button type="button" size="sm" shape="square" variant="secondary" title="启动" aria-label="启动" onClick={() => onRunAction(instance, 'start')}>
          <Play className="h-4 w-4" />
        </Button>
      )}
      {running && (
        <>
          <Button type="button" size="sm" shape="square" variant="secondary" title="停止" aria-label="停止" onClick={() => onRunAction(instance, 'stop')}>
            <Square className="h-4 w-4" />
          </Button>
          <Button type="button" size="sm" shape="square" variant="secondary" title="重启" aria-label="重启" onClick={() => onRunAction(instance, 'reset')}>
            <RotateCw className="h-4 w-4" />
          </Button>
        </>
      )}
      {!terminated && (
        <Button type="button" size="sm" shape="square" variant="danger" title="删除" aria-label="删除" onClick={() => onRunAction(instance, 'delete')}>
          <Trash className="h-4 w-4" />
        </Button>
      )}
    </div>
  );
}

export function DiskActions({ disk, onResize, onSnapshot, onDelete }) {
  return (
    <div className="flex items-center justify-center gap-1">
      <Button type="button" size="sm" shape="square" variant="secondary" title="扩容" aria-label="扩容" onClick={() => onResize(disk)}>
        <Settings className="h-4 w-4" />
      </Button>
      <Button type="button" size="sm" shape="square" variant="secondary" title="快照" aria-label="快照" onClick={() => onSnapshot(disk)}>
        <Download className="h-4 w-4" />
      </Button>
      <Button type="button" size="sm" shape="square" variant="danger" title="删除" aria-label="删除" onClick={() => onDelete(disk)}>
        <Trash className="h-4 w-4" />
      </Button>
    </div>
  );
}

export function FirewallActions({ fw, onEdit, onDelete }) {
  return (
    <div className="flex items-center justify-center gap-1">
      <Button type="button" size="sm" shape="square" variant="secondary" title="编辑" aria-label="编辑" onClick={() => onEdit(fw)}>
        <Settings className="h-4 w-4" />
      </Button>
      <Button type="button" size="sm" shape="square" variant="danger" title="删除" aria-label="删除" onClick={() => onDelete(fw)}>
        <Trash className="h-4 w-4" />
      </Button>
    </div>
  );
}

export function AccountActions({ account, onVerify, onEdit, onDelete }) {
  return (
    <div className="flex items-center justify-end gap-1">
      <Button type="button" size="sm" shape="square" variant="secondary" title="验证" aria-label="验证" onClick={() => onVerify(account)}>
        <Shield className="h-4 w-4" />
      </Button>
      <Button type="button" size="sm" shape="square" variant="secondary" title="编辑" aria-label="编辑" onClick={() => onEdit(account)}>
        <Settings className="h-4 w-4" />
      </Button>
      <Button type="button" size="sm" shape="square" variant="danger" title="删除" aria-label="删除" onClick={() => onDelete(account)}>
        <Trash className="h-4 w-4" />
      </Button>
    </div>
  );
}
