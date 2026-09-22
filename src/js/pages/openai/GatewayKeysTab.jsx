import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { ClipboardText, LayerCard, Table } from '@cloudflare/kumo';
import { handleEditableRowDoubleClick } from '../../modules/tableInteractions.js';
import { formatDateTime } from '../../modules/utils.js';
import { StatusBadge, AppTable } from '../../components/ui/AppPrimitives.jsx';
import { Trash, RotateCw, Edit, Star, Reboot } from '../../components/Icons.jsx';

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

  return (
        <div className="flex grow flex-col gap-3">
          <LayerCard className="w-full min-w-0 overflow-hidden p-0 shadow-none">
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
                      <Table.Cell colSpan={7} className="p-8 text-center text-kumo-subtle">
                        暂无网关 API 密钥
                      </Table.Cell>
                    </Table.Row>
                  ) : (
                    gatewayKeys.map(key => (
                      <Table.Row
                        key={key.id}
                        className="hover:bg-kumo-recessed/5 cursor-pointer"
                        title="双击编辑密钥"
                        onDoubleClick={event =>
                          handleEditableRowDoubleClick(event, () => openEditGatewayKeyModal(key))
                        }
                      >
                        <Table.Cell
                          className="truncate font-semibold text-kumo-strong"
                          title={key.name}
                        >
                          {key.name || '未命名密钥'}
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          {key.apiKey ? (
                            <ClipboardText
                              size="sm"
                              text={key.apiKey}
                              className="min-w-0 w-full font-mono text-[0.9em]"
                              tooltip={{ text: '复制 API Key', copiedText: 'API Key 已复制', side: 'bottom' }}
                              labels={{ copyAction: `复制 ${key.name} 的 API Key` }}
                            />
                          ) : (
                            <span className="text-sm text-kumo-subtle">轮换后可查看并复制</span>
                          )}
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <StatusBadge tone={key.enabled ? 'success' : 'neutral'}>
                            {key.enabled ? '已启用' : '已停用'}
                          </StatusBadge>
                        </Table.Cell>
                        <Table.Cell className="truncate text-center text-sm text-kumo-subtle">
                          {key.lastUsed ? formatDateTime(key.lastUsed) : '从未使用'}
                        </Table.Cell>
                        <Table.Cell className="truncate text-center text-sm text-kumo-subtle">
                          {key.expiresAt ? formatDateTime(key.expiresAt) : '永不过期'}
                        </Table.Cell>
                        <Table.Cell className="text-center font-mono text-[0.9em] text-kumo-strong">
                          {(key.requestCount || 0).toLocaleString('en-US', { useGrouping: false })}
                        </Table.Cell>
                        <Table.Cell className="text-center font-mono text-[0.9em]">
                          {key.maxTokensQuota > 0 ? (
                            <span className="inline-flex items-center gap-2">
                              <span
                                className={
                                  (key.totalTokensUsed || 0) >= key.maxTokensQuota
                                    ? 'text-kumo-danger'
                                    : 'text-kumo-strong'
                                }
                              >
{(key.totalTokensUsed || 0).toLocaleString('en-US', { useGrouping: false })}
                              </span>
                              <span className="text-kumo-subtle">
                                / {key.maxTokensQuota.toLocaleString('en-US', { useGrouping: false })}
                              </span>
                            </span>
                          ) : (
                            <span className="text-kumo-subtle">
                              {(key.totalTokensUsed || 0).toLocaleString('en-US', { useGrouping: false })}
                            </span>
                          )}
                        </Table.Cell>
                        <Table.Cell>
                          <div className="flex justify-center gap-2">
                            <Button
                              shape="square"
                              size="sm"
                              variant={key.enabled ? 'secondary-destructive' : 'primary'}
                              aria-label={key.enabled ? '停用密钥' : '启用密钥'}
                              onClick={() => toggleGatewayKey(key)}
                              title={key.enabled ? '停用密钥' : '启用密钥'}
                              loading={!!gatewayKeyToggleLoading[key.id]}
                              icon={<Reboot className="h-3.5 w-3.5" />}
                            />
                            <Button
                              shape="square"
                              size="sm"
                              variant={key.isDefault ? 'primary' : 'outline'}
                              aria-label={key.isDefault ? '默认密钥' : '设为默认密钥'}
                              onClick={() => setDefaultGatewayKey(key)}
                              disabled={key.isDefault}
                              className={key.isDefault ? undefined : 'text-kumo-subtle hover:text-brand'}
                              title={key.isDefault ? '当前为默认密钥' : '设为默认密钥'}
                            >
                              <Star className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                              shape="square"
                              size="sm"
                              variant="outline"
                              aria-label="轮换密钥"
                              onClick={() => rotateGatewayKey(key)}
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
                              onClick={() => openEditGatewayKeyModal(key)}
                              className="hover:text-brand text-kumo-subtle"
                              title="编辑密钥"
                            >
                              <Edit className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                              shape="square"
                              size="sm"
                              variant={
                                isArmed(`gateway-key-${key.id}`)
                                  ? 'destructive'
                                  : 'secondary-destructive'
                              }
                              aria-label="删除密钥"
                              onClick={() => deleteGatewayKey(key)}
                              title={isArmed(`gateway-key-${key.id}`) ? '再次点击确认删除' : '删除密钥'}
                            >
                              <Trash className="w-3.5 h-3.5" />
                            </Button>
                          </div>
                        </Table.Cell>
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
