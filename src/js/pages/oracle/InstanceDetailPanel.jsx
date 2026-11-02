import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { DropdownMenu } from '@cloudflare/kumo';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Cpu, MoreVertical, Play, Settings, Square, Trash } from '../../components/Icons.jsx';
import { ADVANCED_INSTANCE_ACTIONS } from './constants.js';
import DetailGrid from './DetailGrid.jsx';
import DetailGridSkeleton from './DetailGridSkeleton.jsx';
import EmptySelection from './EmptySelection.jsx';

export default function InstanceDetailPanel({ loadingDetail, selectedInstance, instanceDetail, onRunAction, onOpenResizeDialog, onTerminate }) {
  return (
    <SectionCard
      title="实例详情"
      icon={<Settings className="h-4 w-4" />}
      className="min-w-0 cq-xl:sticky cq-xl:top-0 cq-xl:self-start"
      bodyPadding="none"
      bodyClassName="flex flex-col"
      actions={selectedInstance && (
        <>
          <Button type="button" size="sm" variant="secondary" onClick={() => onRunAction('START')} aria-label="启动实例"><Play className="h-4 w-4" /></Button>
          <Button type="button" size="sm" variant="secondary" onClick={() => onRunAction('STOP')} aria-label="停止实例"><Square className="h-4 w-4" /></Button>
          <Button type="button" size="sm" variant="secondary" onClick={onOpenResizeDialog} aria-label="调整规格"><Cpu className="h-4 w-4" /></Button>
          <DropdownMenu>
            <DropdownMenu.Trigger
              render={<Button type="button" size="sm" variant="secondary" icon={<MoreVertical className="h-4 w-4" />} aria-label="更多实例动作" title="更多动作" />}
            />
            <DropdownMenu.Content side="bottom" align="end" sideOffset={6} className="min-w-40">
              <DropdownMenu.Item onClick={() => onRunAction('SOFTSTOP')}>
                软停止
              </DropdownMenu.Item>
              <DropdownMenu.Separator />
              {ADVANCED_INSTANCE_ACTIONS.filter((action) => action.value !== 'SOFTSTOP').map((action) => (
                <DropdownMenu.Item key={action.value} onClick={() => onRunAction(action.value)}>
                  {action.label}
                </DropdownMenu.Item>
              ))}
            </DropdownMenu.Content>
          </DropdownMenu>
          <Button type="button" size="sm" variant="secondary-destructive" onClick={onTerminate}><Trash className="h-4 w-4" /></Button>
        </>
      )}
    >
      {loadingDetail ? <DetailGridSkeleton /> : selectedInstance ? <DetailGrid instance={instanceDetail || selectedInstance} /> : <EmptySelection />}
    </SectionCard>
  );
}
