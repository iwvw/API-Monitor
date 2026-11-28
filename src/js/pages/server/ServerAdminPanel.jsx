import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Select } from '@cloudflare/kumo/components/select';
import { Input } from '@cloudflare/kumo/components/input';
import { Table } from '@cloudflare/kumo/components/table';
import { Toolbar } from '@cloudflare/kumo';
import { AppTable, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import {
  Database,
  Download,
  Edit,
  FolderOpen,
  Globe,
  Key,
  Plus,
  Save,
  Settings,
  Star,
  Trash,
  Upload,
} from '../../components/Icons.jsx';
import { MANAGEMENT_CARD_ICON_CLASS } from './constants.js';
import { NETWORK_TARGET_COLUMNS, SERVER_CREDENTIAL_COLUMNS } from './serverHelpers.js';

export default function ServerAdminPanel(props) {
  const {
    batchAddServers,
    deleteCredential,
    deleteNetworkTarget,
    exportServers,
    handleServerIpDisplayModeChange,
    isArmed,
    networkTargets,
    openImportServerModal,
    saveServerModuleSettings,
    serverAddingBatch,
    serverBatchError,
    serverBatchSuccess,
    serverBatchText,
    serverCredentials,
    serverCurrentTab,
    serverIpDisplayMode,
    serverSettingsForm,
    serverSettingsSaving,
    setDefaultCredential,
    setNetworkTargetForm,
    setNetworkTargetModalMode,
    setServerBatchText,
    setServerSettingsForm,
    setShowAddCredentialModal,
    setShowNetworkTargetModal,
    toggleNetworkTargetEnabled,
  } = props;
  return (
    <>
      {serverCurrentTab === 'management' && (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-1 items-start gap-4 cq-xl:grid-cols-2">
            <div className="flex min-w-0 flex-col gap-4">
              <SectionCard
                title="主机偏好"
                icon={<Settings className={MANAGEMENT_CARD_ICON_CLASS} />}
                actions={
                  <Button
                    size="sm"
                    variant="primary"
                    onClick={saveServerModuleSettings}
                    loading={serverSettingsSaving}
                    icon={<Save className="h-3.5 w-3.5" />}
                  >
                    保存偏好
                  </Button>
                }
                bodyPadding="none"
              >
                <div className="divide-y divide-kumo-line/80">
                  <div className="grid gap-3 px-4 py-3.5 cq-lg:grid-cols-[minmax(0,1fr)_minmax(14rem,22rem)] cq-lg:items-center">
                    <div className="min-w-0">
                      <div className="text-xs font-semibold leading-5 text-kumo-strong">
                        主机地址显示
                      </div>
                    </div>
                    <div className="flex justify-start cq-lg:justify-end">
                      <Select
                        alignItemWithTrigger
                        size="sm"
                        aria-label="主机地址显示"
                        value={serverIpDisplayMode}
                        onValueChange={handleServerIpDisplayModeChange}
                        items={[
                          { value: 'normal', label: '明文' },
                          { value: 'masked', label: '打码' },
                          { value: 'hidden', label: '隐藏' },
                        ]}
                        className="w-full cq-lg:w-auto"
                      />
                    </div>
                  </div>
                  <div className="grid gap-3 px-4 py-3.5 cq-lg:grid-cols-[minmax(0,1fr)_minmax(18rem,1fr)] cq-lg:items-center">
                    <div className="min-w-0">
                      <div className="text-xs font-semibold leading-5 text-kumo-strong">
                        Agent 下载目录
                      </div>
                      <div className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
                        留空使用主控端内置 /agent 目录；自定义时填目录 URL，不填文件名。
                      </div>
                    </div>
                    <div className="flex justify-start cq-lg:justify-end">
                      <Input
                        size="sm"
                        aria-label="Agent 下载目录"
                        value={serverSettingsForm.agentDownloadUrl}
                        onChange={event =>
                          setServerSettingsForm(prev => ({
                            ...prev,
                            agentDownloadUrl: event.target.value,
                          }))
                        }
                        placeholder="https://cdn.example.com/agent"
                        className="w-full cq-lg:w-auto"
                      />
                    </div>
                  </div>
                </div>
              </SectionCard>

              <SectionCard
                title="批量录入"
                icon={<FolderOpen className={MANAGEMENT_CARD_ICON_CLASS} />}
                meta={<span className="text-xs font-semibold text-kumo-subtle">CSV</span>}
                bodyPadding="sm"
                bodyClassName="flex flex-col gap-2"
              >
                <CodeEditor
                  label="主机列表"
                  language="text"
                  value={serverBatchText}
                  onChange={setServerBatchText}
                  placeholder="名称,IP,端口,用户名,密码 
例如: prod-server,192.168.1.10,22,root,password"
                  minHeight="7rem"
                />
                {serverBatchError && <Badge variant="error">{serverBatchError}</Badge>}
                {serverBatchSuccess && <Badge variant="success">{serverBatchSuccess}</Badge>}
                <Button
                  size="sm"
                  variant="primary"
                  onClick={batchAddServers}
                  loading={serverAddingBatch}
                  disabled={!serverBatchText.trim()}
                  icon={<Plus className="h-3.5 w-3.5" />}
                  className="w-fit"
                >
                  批量录入
                </Button>
              </SectionCard>

              <SectionCard
                title="配置迁移"
                icon={<Database className={MANAGEMENT_CARD_ICON_CLASS} />}
                meta={<span className="text-xs font-semibold text-kumo-subtle">JSON</span>}
                bodyPadding="sm"
                bodyClassName="flex flex-col gap-3 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between"
              >
                <div className="min-w-0 space-y-1">
                  <div className="text-xs font-medium text-kumo-strong">主机配置</div>
                  <div className="max-w-xl text-xs leading-5 text-kumo-subtle">
                    包含连接、认证、标签等配置。
                  </div>
                </div>
                <div className="flex w-full flex-col gap-2 cq-sm:w-auto cq-sm:flex-row cq-sm:items-center">
                  <Toolbar
                    size="sm"
                    aria-label="导出导入主机配置"
                    className="w-full justify-center cq-sm:w-auto"
                  >
                    <Toolbar.Button
                      onClick={exportServers}
                      aria-label="导出主机配置备份"
                      icon={<Upload className="h-3.5 w-3.5" />}
                      className="w-full cq-sm:w-auto"
                    >
                      <span className="hidden cq-sm:inline">导出</span>
                    </Toolbar.Button>
                    <Toolbar.Button
                      onClick={openImportServerModal}
                      aria-label="导入主机配置"
                      icon={<Download className="h-3.5 w-3.5" />}
                      className="w-full cq-sm:w-auto"
                    >
                      <span className="hidden cq-sm:inline">导入</span>
                    </Toolbar.Button>
                  </Toolbar>
                </div>
              </SectionCard>
            </div>

            <div className="flex min-w-0 flex-col gap-4">
              <SectionCard
                title="SSH 凭据库"
                icon={<Key className={MANAGEMENT_CARD_ICON_CLASS} />}
                actions={
                  <Button
                    size="sm"
                    variant="primary"
                    onClick={() => setShowAddCredentialModal()}
                    icon={<Plus className="h-3.5 w-3.5" />}
                  >
                    添加
                  </Button>
                }
                bodyPadding="none"
              >
                {serverCredentials.length === 0 ? (
                  <div className="px-3 py-8 text-center text-xs text-kumo-subtle">
                    暂无预设访问凭据
                  </div>
                ) : (
                  <div className="max-h-64 overflow-auto">
                    <AppTable tableId="server-credentials" columns={SERVER_CREDENTIAL_COLUMNS}>
                      <Table.Header variant="compact">
                        <Table.Row>
                          <Table.Head className="text-left">名称</Table.Head>
                          <Table.Head className="text-center">用户</Table.Head>
                          <Table.Head className="app-table-action">操作</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {serverCredentials.map(cred => (
                          <Table.Row
                            key={cred.id}
                            className="border-b border-kumo-line/80 hover:bg-kumo-recessed/10 text-center"
                          >
                            <Table.Cell className="whitespace-nowrap">
                              {/* <div className="flex justify-center"> */}
                              <div className="flex min-w-0 items-center gap-2">
                                <span
                                  className="truncate font-semibold text-kumo-strong"
                                  title={cred.name}
                                >
                                  {cred.name}
                                </span>
                                {cred.is_default && (
                                  <Badge variant="success" appearance="dot">
                                    默认
                                  </Badge>
                                )}
                              </div>
                              {/* </div> */}
                            </Table.Cell>
                            <Table.Cell
                              className="whitespace-nowrap font-mono text-[11px] text-kumo-subtle"
                              title={cred.username}
                            >
                              {cred.username}
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap text-center">
                              <div className="inline-flex items-center justify-center gap-1">
                                {!cred.is_default && (
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant="secondary"
                                    aria-label="设为默认凭据"
                                    onClick={() => setDefaultCredential(cred.id)}
                                    icon={<Star className="h-3.5 w-3.5" />}
                                    title="设为默认"
                                  />
                                )}
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant={
                                    isArmed(`credential.delete::${cred.id}`)
                                      ? 'destructive'
                                      : 'secondary-destructive'
                                  }
                                  aria-label="删除凭据"
                                  onClick={() => deleteCredential(cred.id)}
                                  icon={<Trash className="h-3.5 w-3.5" />}
                                  title="删除"
                                />
                              </div>
                            </Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </AppTable>
                  </div>
                )}
              </SectionCard>

              <SectionCard
                title="网络拨测目标"
                icon={<Globe className={MANAGEMENT_CARD_ICON_CLASS} />}
                actions={
                  <Button
                    size="sm"
                    variant="primary"
                    onClick={() => {
                      setNetworkTargetModalMode('add');
                      setNetworkTargetForm({
                        id: null,
                        name: '',
                        host: '',
                        port: 80,
                        type: 'tcp',
                        enabled: true,
                        order_index: 0,
                      });
                      setShowNetworkTargetModal(true);
                    }}
                    icon={<Plus className="h-3.5 w-3.5" />}
                  >
                    添加
                  </Button>
                }
                bodyPadding="none"
              >
                {networkTargets.length === 0 ? (
                  <div className="px-3 py-8 text-center text-xs text-kumo-subtle">暂无监测目标</div>
                ) : (
                  <div className="max-h-64 overflow-auto">
                    <AppTable tableId="network-targets" columns={NETWORK_TARGET_COLUMNS} className="min-w-[520px]">
                      <Table.Header variant="compact">
                        <Table.Row>
                          <Table.Head className="text-left">名称</Table.Head>
                          <Table.Head className="text-left">节点地址</Table.Head>
                          <Table.Head className="text-center">状态</Table.Head>
                          <Table.Head className="app-table-action">操作</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {networkTargets.map(target => (
                          <Table.Row
                            key={target.id}
                            className="border-b border-kumo-line/80 hover:bg-kumo-recessed/10"
                          >
                            <Table.Cell className="whitespace-nowrap text-left">
                              <span
                                className="font-semibold text-kumo-strong truncate block"
                                title={target.name}
                              >
                                {target.name}
                              </span>
                            </Table.Cell>
                            <Table.Cell
                              className="text-left"
                              title={`${target.host}:${target.port} (${target.type})`}
                            >
                              <span className="block truncate font-mono text-[11px] leading-4 text-kumo-subtle">
                                {target.host}:{target.port}
                              </span>
                              <span className="block truncate text-[10px] leading-4 text-kumo-subtle/70">
                                {target.type}
                              </span>
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap">
                              <div className="flex justify-center">
                                <Checkbox
                                  checked={!!target.enabled}
                                  onCheckedChange={checked =>
                                    toggleNetworkTargetEnabled(target, Boolean(checked))
                                  }
                                  aria-label={`${target.name} 启用状态`}
                                />
                              </div>
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap text-center">
                              <div className="inline-flex items-center justify-center gap-1">
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant="secondary"
                                  aria-label="编辑目标"
                                  onClick={() => {
                                    setNetworkTargetModalMode('edit');
                                    setNetworkTargetForm({
                                      id: target.id,
                                      name: target.name,
                                      host: target.host,
                                      port: target.port,
                                      type: target.type || 'tcp',
                                      enabled: !!target.enabled,
                                      order_index: target.order_index ?? 0,
                                    });
                                    setShowNetworkTargetModal(true);
                                  }}
                                  icon={<Edit className="h-3.5 w-3.5" />}
                                  title="编辑"
                                />
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant={
                                    isArmed(`network-target.delete::${target.id}`)
                                      ? 'destructive'
                                      : 'secondary-destructive'
                                  }
                                  aria-label="删除目标"
                                  onClick={() => deleteNetworkTarget(target.id)}
                                  icon={<Trash className="h-3.5 w-3.5" />}
                                  title="删除"
                                />
                              </div>
                            </Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </AppTable>
                  </div>
                )}
              </SectionCard>
            </div>
          </div>
        </div>
      )}


    </>
  );
}
