import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { ClipboardText, LayerCard, Table } from '@cloudflare/kumo';
import { handleEditableRowDoubleClick } from '../../modules/tableInteractions.js';
import { formatDateTime } from '../../modules/utils.js';
import { StatusBadge, AppTable } from '../../components/ui/AppPrimitives.jsx';
import { Trash, RotateCw, Edit, Star, Reboot } from '../../components/Icons.jsx';
import {
  MobileCard,
  MobileCardActions,
  MobileCardEmpty,
  MobileCardField,
  MobileCardFields,
  MobileCardList,
  MobileCardSkeleton,
} from '../../components/ui/MobileTableCards.jsx';

const GATEWAY_KEY_COLUMNS = [
  { id: 'name', role: 'primary', minWidth: 180, grow: 1 },
  { id: 'key', role: 'identifier', minWidth: 240, align: 'center' },
  { id: 'status', role: 'status' },
  { id: 'lastUsed', role: 'datetime', align: 'center' },
  { id: 'expiresAt', role: 'datetime', align: 'center' },
  { id: 'requests', role: 'count', align: 'center' },
  { id: 'tokens', role: 'count', align: 'center' },
  { id: 'actions', role: 'actions-lg' },
];

export function GatewayKeysTab({ keys, isArmed }) {
  const {
    gatewayKeys,
    gatewayKeysLoading,
    gatewayKeyToggleLoading,
    openEditGatewayKeyModal,
    toggleGatewayKey,
    setDefaultGatewayKey,
    rotateGatewayKey,
    deleteGatewayKey,
  } = keys;

  const renderKeyActions = item => (
    <div className="flex justify-center gap-2">
      <Button
        shape="square"
        size="sm"
        variant={item.enabled ? 'secondary-destructive' : 'primary'}
        aria-label={item.enabled ? '停用密钥' : '启用密钥'}
        onClick={() => toggleGatewayKey(item)}
        title={item.enabled ? '停用密钥' : '启用密钥'}
        loading={!!gatewayKeyToggleLoading[item.id]}
        icon={<Reboot className="h-3.5 w-3.5" />}
      />
      <Button
        shape="square"
        size="sm"
        variant={item.isDefault ? 'primary' : 'outline'}
        aria-label={item.isDefault ? '默认密钥' : '设为默认密钥'}
        onClick={() => setDefaultGatewayKey(item)}
        disabled={item.isDefault}
        className={item.isDefault ? undefined : 'text-kumo-subtle hover:text-brand'}
        title={item.isDefault ? '当前为默认密钥' : '设为默认密钥'}
      >
        <Star className="w-3.5 h-3.5" />
      </Button>
      <Button
        shape="square"
        size="sm"
        variant="outline"
        aria-label="轮换密钥"
        onClick={() => rotateGatewayKey(item)}
        className="text-kumo-subtle hover:text-brand"
        title="轮换密钥"
      >
        <RotateCw className="w-3.5 h-3.5" />
      </Button>
      <Button
        shape="square"
        size="sm"
        variant="outline"
        aria-label="编辑密钥"
        onClick={() => openEditGatewayKeyModal(item)}
        className="hover:text-brand text-kumo-subtle"
        title="编辑密钥"
      >
        <Edit className="w-3.5 h-3.5" />
      </Button>
      <Button
        shape="square"
        size="sm"
        variant={isArmed(`gateway-key-${item.id}`) ? 'destructive' : 'secondary-destructive'}
        aria-label="删除密钥"
        onClick={() => deleteGatewayKey(item)}
        title={isArmed(`gateway-key-${item.id}`) ? '再次点击确认删除' : '删除密钥'}
      >
        <Trash className="w-3.5 h-3.5" />
      </Button>
    </div>
  );

  const renderTokenUsage = item => (
    <>
      <span
        className={
          item.maxTokensQuota > 0 && (item.totalTokensUsed || 0) >= item.maxTokensQuota
            ? 'text-kumo-danger'
            : undefined
        }
      >
        {(item.totalTokensUsed || 0).toLocaleString('en-US', { useGrouping: false })}
      </span>
      {item.maxTokensQuota > 0 && (
        <span className="text-kumo-subtle">
          {' / '}
          {item.maxTokensQuota.toLocaleString('en-US', { useGrouping: false })}
        </span>
      )}
    </>
  );

  const renderKeyCard = item => (
    <MobileCard key={item.id}>
      <div className="flex min-w-0 items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-kumo-strong" title={item.name}>
            {item.name || '未命名密钥'}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            <StatusBadge tone={item.enabled ? 'success' : 'neutral'}>
              {item.enabled ? '已启用' : '已停用'}
            </StatusBadge>
            {item.isDefault && <StatusBadge tone="brand">默认</StatusBadge>}
          </div>
        </div>
      </div>
      <div className="mt-2 min-w-0">
        {item.apiKey ? (
          <ClipboardText
            size="sm"
            text={item.apiKey}
            className="w-full min-w-0 font-mono text-[0.9em]"
            tooltip={{ text: '复制 API Key', copiedText: 'API Key 已复制', side: 'bottom' }}
            labels={{ copyAction: `复制 ${item.name} 的 API Key` }}
          />
        ) : (
          <span className="text-xs text-kumo-subtle">轮换后可查看并复制</span>
        )}
      </div>
      <MobileCardFields>
        <MobileCardField label="最近使用">
          {item.lastUsed ? formatDateTime(item.lastUsed) : '从未使用'}
        </MobileCardField>
        <MobileCardField label="过期时间">
          {item.expiresAt ? formatDateTime(item.expiresAt) : '永不过期'}
        </MobileCardField>
        <MobileCardField label="请求数">
          {(item.requestCount || 0).toLocaleString('en-US', { useGrouping: false })}
        </MobileCardField>
        <MobileCardField label="词元用量">{renderTokenUsage(item)}</MobileCardField>
      </MobileCardFields>
      <MobileCardActions>{renderKeyActions(item)}</MobileCardActions>
    </MobileCard>
  );

  return (
    <div className="flex grow flex-col gap-3">
      {gatewayKeysLoading ? (
        <MobileCardList>
          {[...Array(3)].map((_, i) => (
            <MobileCardSkeleton key={i} rows={4} />
          ))}
        </MobileCardList>
      ) : gatewayKeys.length === 0 ? (
        <MobileCardList>
          <MobileCardEmpty>暂无网关 API 密钥</MobileCardEmpty>
        </MobileCardList>
      ) : (
        <MobileCardList>{gatewayKeys.map(renderKeyCard)}</MobileCardList>
      )}

      <LayerCard className="hidden w-full min-w-0 overflow-hidden p-0 shadow-none cq-md:block">
        <div className="min-w-0 overflow-x-auto scrollbar-thin">
          <AppTable tableId="gateway-keys" columns={GATEWAY_KEY_COLUMNS} className="min-w-[1200px] [&_td]:!px-2 [&_td]:!py-2 [&_th]:!px-2 [&_th]:!py-2">
            <Table.Header sticky variant="compact">
              <Table.Row>
                <Table.Head>名称</Table.Head>
                <Table.Head className="text-center">密钥</Table.Head>
                <Table.Head className="text-center">状态</Table.Head>
                <Table.Head className="text-center">最近使用</Table.Head>
                <Table.Head className="text-center">过期时间</Table.Head>
                <Table.Head className="text-center">请求数</Table.Head>
                <Table.Head className="text-center">词元用量</Table.Head>
                <Table.Head className="app-table-action">操作</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {gatewayKeysLoading ? (
                [...Array(3)].map((_, i) => (
                  <Table.Row key={i}>
                    <Table.Cell>
                      <SkeletonLine className="h-4 w-24" />
                    </Table.Cell>
                    <Table.Cell>
                      <SkeletonLine className="h-4 w-28" />
                    </Table.Cell>
                    <Table.Cell className="text-center">
                      <SkeletonLine className="mx-auto h-4 w-12" />
                    </Table.Cell>
                    <Table.Cell>
                      <SkeletonLine className="h-4 w-24" />
                    </Table.Cell>
                    <Table.Cell>
                      <SkeletonLine className="h-4 w-24" />
                    </Table.Cell>
                    <Table.Cell className="text-right">
                      <SkeletonLine className="ml-auto h-4 w-12" />
                    </Table.Cell>
                    <Table.Cell>
                      <SkeletonLine className="mx-auto h-4 w-24" />
                    </Table.Cell>
                  </Table.Row>
                ))
              ) : gatewayKeys.length === 0 ? (
                <Table.Row>
                  <Table.Cell colSpan={8} className="p-8 text-center text-kumo-subtle">
                    暂无网关 API 密钥
                  </Table.Cell>
                </Table.Row>
              ) : (
                gatewayKeys.map(item => (
                  <Table.Row
                    key={item.id}
                    className="hover:bg-kumo-recessed/5 cursor-pointer"
                    title="双击编辑密钥"
                    onDoubleClick={event =>
                      handleEditableRowDoubleClick(event, () => openEditGatewayKeyModal(item))
                    }
                  >
                    <Table.Cell
                      className="truncate font-semibold text-kumo-strong"
                      title={item.name}
                    >
                      {item.name || '未命名密钥'}
                    </Table.Cell>
                    <Table.Cell className="text-center">
                      {item.apiKey ? (
                        <ClipboardText
                          size="sm"
                          text={item.apiKey}
                          className="min-w-0 w-full font-mono text-[0.9em]"
                          tooltip={{ text: '复制 API Key', copiedText: 'API Key 已复制', side: 'bottom' }}
                          labels={{ copyAction: `复制 ${item.name} 的 API Key` }}
                        />
                      ) : (
                        <span className="text-sm text-kumo-subtle">轮换后可查看并复制</span>
                      )}
                    </Table.Cell>
                    <Table.Cell className="text-center">
                      <StatusBadge tone={item.enabled ? 'success' : 'neutral'}>
                        {item.enabled ? '已启用' : '已停用'}
                      </StatusBadge>
                    </Table.Cell>
                    <Table.Cell className="truncate text-center text-sm text-kumo-subtle">
                      {item.lastUsed ? formatDateTime(item.lastUsed) : '从未使用'}
                    </Table.Cell>
                    <Table.Cell className="truncate text-center text-sm text-kumo-subtle">
                      {item.expiresAt ? formatDateTime(item.expiresAt) : '永不过期'}
                    </Table.Cell>
                    <Table.Cell className="text-center font-mono text-[0.9em] text-kumo-strong">
                      {(item.requestCount || 0).toLocaleString('en-US', { useGrouping: false })}
                    </Table.Cell>
                    <Table.Cell className="text-center font-mono text-[0.9em]">
                      {item.maxTokensQuota > 0 ? (
                        <span className="inline-flex items-center gap-2">
                          {renderTokenUsage(item)}
                        </span>
                      ) : (
                        <span className="text-kumo-subtle">
                          {(item.totalTokensUsed || 0).toLocaleString('en-US', { useGrouping: false })}
                        </span>
                      )}
                    </Table.Cell>
                    <Table.Cell>{renderKeyActions(item)}</Table.Cell>
                  </Table.Row>
                ))
              )}
            </Table.Body>
          </AppTable>
        </div>
      </LayerCard>
    </div>
  );
}