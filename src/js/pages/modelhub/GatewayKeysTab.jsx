import { Button } from '@cloudflare/kumo/components/button';
import { Switch } from '@cloudflare/kumo/components/switch';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { LayerCard, Table, ClipboardText, Popover } from '@cloudflare/kumo';
import { handleEditableRowDoubleClick } from '../../modules/tableInteractions.js';
import { GATEWAY_KEY_DISPLAY_MASK } from './constants.js';
import { formatTokensZh } from './utils.js';
import { formatDateTime } from '../../modules/utils.js';
import { StatusBadge, AppTable } from '../../components/ui/AppPrimitives.jsx';
import { Trash, RotateCw, Edit, Star } from '../../components/Icons.jsx';
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
  { id: 'status', role: 'status', width: 72, minWidth: 64 },
  { id: 'name', role: 'primary', minWidth: 140, grow: 1 },
  { id: 'key', role: 'identifier', minWidth: 180, grow: 2, align: 'center' },
  { id: 'lastUsed', role: 'datetime', width: 148, minWidth: 128, align: 'center' },
  { id: 'expiresAt', role: 'datetime', width: 148, minWidth: 128, align: 'center' },
  { id: 'requests', role: 'count', width: 80, minWidth: 64, align: 'center' },
  { id: 'tokens', role: 'count', width: 132, minWidth: 80, maxWidth: 160, align: 'center' },
  { id: 'actions', role: 'actions-lg', width: 184, minWidth: 168 },
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

  const renderKeyToggle = item => (
    <Switch
      size="sm"
      aria-label={item.enabled ? '停用密钥' : '启用密钥'}
      checked={item.enabled}
      onCheckedChange={() => toggleGatewayKey(item)}
      disabled={!!gatewayKeyToggleLoading[item.id]}
    />
  );

  const renderKeyActions = item => (
    <div className="flex justify-center gap-2">
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

  const renderTokenUsage = item => {
    const used = Number(item.totalTokensUsed) || 0;
    const quota = Number(item.maxTokensQuota) || 0;
    const overQuota = quota > 0 && used >= quota;
    return (
      <Popover>
        <Popover.Trigger
          nativeButton={false}
          render={
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-auto min-h-0 gap-1 px-1.5 font-mono text-[0.9em] font-normal"
              title="查看完整用量"
            >
              <span className={overQuota ? 'text-kumo-danger' : 'text-kumo-strong'}>
                {formatTokensZh(used)}
              </span>
              {quota > 0 && <span className="text-kumo-subtle">/ {formatTokensZh(quota)}</span>}
            </Button>
          }
        />
        <Popover.Content side="bottom" align="center" className="w-auto px-3 py-2">
          <div className="flex items-center justify-between gap-6 text-xs leading-normal">
            <span className="text-kumo-subtle">已用词元</span>
            <span className="font-mono tabular-nums text-kumo-strong">
              {used.toLocaleString('en-US', { useGrouping: false })}
            </span>
          </div>
          {quota > 0 && (
            <div className="mt-1 flex items-center justify-between gap-6 text-xs leading-normal">
              <span className="text-kumo-subtle">配额</span>
              <span className="font-mono tabular-nums text-kumo-strong">
                {quota.toLocaleString('en-US', { useGrouping: false })}
              </span>
            </div>
          )}
        </Popover.Content>
      </Popover>
    );
  };

  const renderKeyCard = item => (
    <MobileCard key={item.id}>
      <div className="flex min-w-0 items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-kumo-strong" title={item.name}>
            {item.name || '未命名密钥'}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            <div className="flex items-center gap-1.5">
              {renderKeyToggle(item)}
              <span className={`text-xs ${item.enabled ? 'text-kumo-strong' : 'text-kumo-subtle'}`}>
                {item.enabled ? '已启用' : '已停用'}
              </span>
            </div>
            {item.isDefault && <StatusBadge tone="brand">默认</StatusBadge>}
          </div>
        </div>
      </div>
      <div className="mt-2 min-w-0">
        {item.apiKey ? (
          <ClipboardText
            size="sm"
            text={GATEWAY_KEY_DISPLAY_MASK}
            textToCopy={item.apiKey}
            className="w-full min-w-0 font-mono text-[0.9em]"
            labels={{ copyAction: `复制 ${item.name || '未命名密钥'} 的 API Key` }}
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
          <AppTable tableId="gateway-keys" columns={GATEWAY_KEY_COLUMNS} className="[&_td]:!px-2 [&_td]:!py-2 [&_th]:!px-2 [&_th]:!py-2">
            <Table.Header sticky variant="compact">
              <Table.Row>
                <Table.Head className="text-center">状态</Table.Head>
                <Table.Head>名称</Table.Head>
                <Table.Head className="text-center">密钥</Table.Head>
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
                    <Table.Cell className="text-center">
                      <SkeletonLine className="mx-auto h-4 w-12" />
                    </Table.Cell>
                    <Table.Cell>
                      <SkeletonLine className="h-4 w-24" />
                    </Table.Cell>
                    <Table.Cell>
                      <SkeletonLine className="h-4 w-28" />
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
                    <Table.Cell className="text-center">
                      <div className="flex justify-center">{renderKeyToggle(item)}</div>
                    </Table.Cell>
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
                          text={GATEWAY_KEY_DISPLAY_MASK}
                          textToCopy={item.apiKey}
                          className="min-w-0 w-full font-mono text-[0.9em]"
                          labels={{ copyAction: `复制 ${item.name || '未命名密钥'} 的 API Key` }}
                        />
                      ) : (
                        <span className="text-sm text-kumo-subtle">轮换后可查看并复制</span>
                      )}
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
                      <div className="flex justify-center">{renderTokenUsage(item)}</div>
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