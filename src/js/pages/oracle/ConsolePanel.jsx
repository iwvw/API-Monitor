import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { EmptyState, InsetPanel, KeyValueGrid, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Plus, Settings, Terminal, Trash } from '../../components/Icons.jsx';
import ResourceList from './ResourceList.jsx';
import { getOciStatusTone } from './utils.js';

export default function ConsolePanel({ selectedInstance, consolePublicKey, onConsolePublicKeyChange, onCreateConsoleConnection, loadingDetail, instanceDetail, onCopy, isArmed, deletingConsoleId, onDeleteConsoleConnection }) {
  return (
    <div className="grid min-h-0 flex-1 gap-4 cq-xl:grid-cols-[minmax(300px,0.72fr)_minmax(0,1.28fr)]">
      <SectionCard
        title="创建控制台连接"
        icon={<Terminal className="h-4 w-4 text-brand" />}
        className="min-h-0"
        bodyClassName="flex min-h-0 flex-1 flex-col gap-4"
        action={<Button type="button" size="sm" onClick={onCreateConsoleConnection} disabled={!selectedInstance || !consolePublicKey.trim()}><Plus className="mr-2 h-4 w-4" />创建连接</Button>}
      >
        {selectedInstance ? (
          <>
            <InsetPanel tone="recessed" padding="sm">
              <KeyValueGrid
                columns={1}
                items={[
                  {
                    label: '实例',
                    value: (
                      <span className="block truncate font-semibold" title={selectedInstance.name || selectedInstance.id}>
                        {selectedInstance.name || selectedInstance.id}
                      </span>
                    ),
                  },
                  {
                    label: '状态',
                    value: <StatusBadge tone={getOciStatusTone(selectedInstance.state)}>{selectedInstance.state || '-'}</StatusBadge>,
                  },
                  {
                    label: '公共 IP',
                    value: <span className="font-mono text-xs">{selectedInstance.primaryPublicIp || '-'}</span>,
                  },
                ]}
              />
            </InsetPanel>
            <CodeEditor
              label="控制台连接 SSH 公钥"
              language="text"
              value={consolePublicKey}
              onChange={onConsolePublicKeyChange}
              placeholder="粘贴 SSH 公钥"
              minHeight="10rem"
            />
            <div className="text-xs leading-5 text-kumo-subtle">
              创建后可在右侧查看连接串和指纹。
            </div>
          </>
        ) : (
          <EmptyState
            icon={Terminal}
            title="请先选择实例"
            card={false}
            className="min-h-[18rem]"
          />
        )}
      </SectionCard>

      <SectionCard
        title="控制台连接列表"
        icon={<Settings className="h-4 w-4" />}
        className="min-h-0 flex-1"
        bodyPadding="none"
        bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
      >
        {!selectedInstance ? (
          <EmptyState
            icon={Terminal}
            title="暂无可展示连接"
            description="选中实例后显示现有连接。"
            card={false}
            className="min-h-[20rem]"
          />
        ) : (
          <ResourceList
            embedded
            loading={loadingDetail}
            items={instanceDetail?.consoleSummary || []}
            columns={['state', 'connectionString', 'fingerprint', 'timeCreated']}
            onCopy={onCopy}
            renderActions={(item) => (
              <Button
                type="button"
                size="sm"
                shape="square"
                variant={isArmed(`console:${item.id}`) ? 'destructive' : 'secondary-destructive'}
                disabled={deletingConsoleId === item.id}
                onClick={() => onDeleteConsoleConnection(item.id)}
                aria-label="删除控制台连接"
                title="删除连接"
                icon={<Trash className="h-3.5 w-3.5" />}
              />
            )}
          />
        )}
      </SectionCard>
    </div>
  );
}
