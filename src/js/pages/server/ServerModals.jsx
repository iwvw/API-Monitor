import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import { TagInput } from '@cloudflare/kumo/components/tag-input';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { ClipboardText, Meter, Tabs } from '@cloudflare/kumo';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { DateField } from '../../components/ui/DateField.jsx';
import { AppCard } from '../../components/ui/AppPrimitives.jsx';
import CountryFlag from '../../components/CountryFlag.jsx';
import {
  FolderOpen,
  Download,
  Trash,
  Key,
  Play,
  Upload,
  FileText,
  RefreshCw,
  Save,
  Edit,
  Settings,
  Activity,
} from '../../components/Icons.jsx';
import {
  trafficQuotaInputToBytes,
  normalizeTrafficAlertPercentInput,
  normalizeTrafficCycleDayInput,
  TRAFFIC_CYCLE_OPTIONS,
  DOCKER_LOG_TAIL_ITEMS,
} from './serverHelpers.js';
import { isWindowsAgentInstallOs, getAgentInstallExecutionHint } from '../../modules/agentInstall.js';
import { getFlagCountry } from './utils.js';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';

export default function ServerModals(props) {
  const {
    addCredential,
    agentForceSsh,
    agentInstallLoading,
    agentInstallLog,
    agentInstallOS,
    agentInstallResult,
    agentInstalling,
    agentModalData,
    applyCredential,
    autoInstallAgent,
    batchInstallResults,
    canSshDeployAgent,
    confirmImportServers,
    credForm,
    dockerComposeEditor,
    dockerLogsContainer,
    dockerLogsContent,
    dockerLogsLoading,
    dockerLogsModalOpen,
    dockerLogsServer,
    dockerLogsTail,
    generateQuickInstallCommand,
    getAgentBaseApiUrl,
    getAgentInstallCommand,
    getAgentUpgradeTargets,
    getDockerContainerName,
    getUpgradeBatchStatusLabel,
    getUpgradeItemStatusLabel,
    importModalError,
    importModalSaving,
    importPreview,
    loadDockerContainerLogs,
    networkTargetForm,
    networkTargetModalMode,
    performOneKeyUpgrade,
    processImportFile,
    quickDeployName,
    quickDeployResult,
    regenerateAgentKey,
    requestCloseDockerComposeEditor,
    runBatchAgentInstall,
    saveDockerComposeConfig,
    saveNetworkTarget,
    saveServer,
    selectAllBatchServers,
    selectedBatchServers,
    selectedCredentialId,
    serverAddMode,
    serverCredentials,
    serverForm,
    serverList,
    serverModalMode,
    serverModalPortalRef,
    serverModalSaving,
    setAgentForceSsh,
    setAgentInstallOS,
    setAgentModalData,
    setCredForm,
    setDockerComposeEditor,
    setDockerLogsContent,
    setDockerLogsLoading,
    setDockerLogsModalOpen,
    setDockerLogsTail,
    setNetworkTargetForm,
    setQuickDeployName,
    setSelectedBatchServers,
    setServerAddMode,
    setServerForm,
    setShowAddCredentialModal,
    setShowAgentModal,
    setShowBatchAgentModal,
    setShowImportServerModal,
    setShowNetworkTargetModal,
    setShowServerModal,
    setShowUpgradeModal,
    setUpgradeFallbackSsh,
    showAddCredentialModal,
    showAgentModal,
    showBatchAgentModal,
    showImportServerModal,
    showNetworkTargetModal,
    showServerModal,
    showUpgradeModal,
    testServerConnection,
    testTrafficAlert,
    uninstallAgent,
    updateDockerComposeDeployment,
    upgradeBatchSnapshot,
    upgradeFallbackSsh,
    upgradeLog,
    upgradeLogViewportRef,
    upgradeProgress,
    upgrading,
  } = props;
  return (
    <>
      <LayerDialog.Root open={showServerModal} onOpenChange={setShowServerModal}>
        <LayerDialog.Content size="xl">
          <LayerDialog.Title>
            {serverModalMode === 'add' ? '新增主机实例' : '编辑主机实例'}
          </LayerDialog.Title>
          <LayerDialog.Body>
            <div ref={serverModalPortalRef} className="flex flex-col gap-3 text-xs">
              {serverModalMode === 'add' && (
                <Tabs
                  {...TOOL_TABS_PROPS}
                  value={serverAddMode}
                  onValueChange={value => {
                    setServerAddMode(value);
                  }}
                  tabs={[
                    { value: 'ssh', label: 'SSH' },
                    { value: 'agent', label: 'Agent' },
                  ]}
                />
              )}

              {serverModalMode === 'edit' || serverAddMode === 'ssh' ? (
                <>
                  <div className="flex flex-col gap-3">
                    <div className="flex flex-col gap-1.5">
                      <label className="font-semibold text-kumo-subtle">主机名称 (别名)</label>
                      <Input
                        size="sm"
                        aria-label="主机名称"
                        type="text"
                        value={serverForm.name}
                        onChange={e => setServerForm(prev => ({ ...prev, name: e.target.value }))}
                        placeholder="生产数据库-01"
                        className="px-3 py-2 text-kumo-strong"
                      />
                    </div>
                    <div className="grid grid-cols-2 gap-3">
                      <div className="flex flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">开始时间</label>
                        <DateField
                          value={serverForm.startsAt}
                          onChange={startsAt => setServerForm(prev => ({ ...prev, startsAt }))}
                          placeholder="未设置"
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">到期时间</label>
                        <DateField
                          value={serverForm.expiresAt}
                          onChange={expiresAt => setServerForm(prev => ({ ...prev, expiresAt }))}
                          placeholder="永不过期"
                        />
                      </div>
                    </div>
                  </div>

                  <section className="rounded-lg border border-kumo-line bg-kumo-recessed/20 p-3.5">
                    <div className="mb-3 flex items-center justify-between gap-3">
                      <h3 className="font-semibold text-kumo-strong">流量配置</h3>
                      <span className="text-[11px] text-kumo-subtle">配额、报警与重置周期</span>
                    </div>

                    <div className="grid grid-cols-1 gap-3 cq-lg:grid-cols-[minmax(0,1.35fr)_minmax(18rem,0.85fr)]">
                      <div className="grid min-w-0 grid-cols-1 gap-3 cq-sm:grid-cols-[minmax(0,1fr)_8rem]">
                        <div className="flex min-w-0 flex-col gap-1.5">
                          <label className="font-semibold text-kumo-subtle">流量配额</label>
                          <div className="grid grid-cols-[minmax(0,1fr)_4.5rem] gap-2">
                            <Input
                              size="sm"
                              aria-label="总流量配额"
                              type="number"
                              min="0"
                              step="0.001"
                              value={serverForm.trafficLimitValue}
                              onChange={e =>
                                setServerForm(prev => ({
                                  ...prev,
                                  trafficLimitValue: e.target.value,
                                }))
                              }
                              placeholder="留空则不限额"
                              className="px-3 py-2 text-kumo-strong"
                            />
                            <Select
                              alignItemWithTrigger
                              size="sm"
                              aria-label="流量配额单位"
                              value={serverForm.trafficLimitUnit}
                              onValueChange={value =>
                                setServerForm(prev => ({
                                  ...prev,
                                  trafficLimitUnit: String(value),
                                }))
                              }
                              className="px-3 py-2"
                              items={[
                                { value: 'GB', label: 'GB' },
                                { value: 'TB', label: 'TB' },
                                { value: 'PB', label: 'PB' },
                              ]}
                            />
                          </div>
                        </div>
                        <div className="flex min-w-0 flex-col gap-1.5">
                          <label className="font-semibold text-kumo-subtle">统计范围</label>
                          <Select
                            alignItemWithTrigger
                            size="sm"
                            aria-label="配额方向"
                            value={serverForm.trafficLimitMode}
                            onValueChange={value =>
                              setServerForm(prev => ({ ...prev, trafficLimitMode: String(value) }))
                            }
                            className="px-3 py-2"
                            items={[
                              { value: 'total', label: '总流量' },
                              { value: 'upload', label: '上行' },
                              { value: 'download', label: '下行' },
                            ]}
                          />
                        </div>
                      </div>

                      <div className="flex min-w-0 flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">流量报警</label>
                        <div className="grid min-w-0 grid-cols-[auto_minmax(4.75rem,1fr)_auto_auto] items-center gap-2">
                          <div className="whitespace-nowrap">
                            <Checkbox
                              label="启用"
                              checked={Boolean(serverForm.trafficAlertEnabled)}
                              disabled={
                                trafficQuotaInputToBytes(
                                  serverForm.trafficLimitValue,
                                  serverForm.trafficLimitUnit
                                ) <= 0
                              }
                              onCheckedChange={checked =>
                                setServerForm(prev => ({
                                  ...prev,
                                  trafficAlertEnabled: Boolean(checked),
                                }))
                              }
                            />
                          </div>
                          <Input
                            size="sm"
                            aria-label="报警阈值百分比"
                            type="number"
                            min="1"
                            max="100"
                            step="1"
                            value={serverForm.trafficAlertPercent}
                            disabled={
                              !serverForm.trafficAlertEnabled ||
                              trafficQuotaInputToBytes(
                                serverForm.trafficLimitValue,
                                serverForm.trafficLimitUnit
                              ) <= 0
                            }
                            onChange={e =>
                              setServerForm(prev => ({
                                ...prev,
                                trafficAlertPercent: e.target.value,
                              }))
                            }
                            onBlur={() =>
                              setServerForm(prev => ({
                                ...prev,
                                trafficAlertPercent: normalizeTrafficAlertPercentInput(
                                  prev.trafficAlertPercent
                                ),
                              }))
                            }
                            className="min-w-0 px-3 py-2 text-kumo-strong"
                          />
                          <span className="text-kumo-subtle">%</span>
                          <Button
                            type="button"
                            size="sm"
                            variant="secondary"
                            disabled={
                              serverModalSaving ||
                              !serverForm.trafficAlertEnabled ||
                              trafficQuotaInputToBytes(
                                serverForm.trafficLimitValue,
                                serverForm.trafficLimitUnit
                              ) <= 0
                            }
                            onClick={testTrafficAlert}
                            className="whitespace-nowrap px-3 py-1.5 text-xs font-semibold"
                          >
                            测试
                          </Button>
                        </div>
                      </div>
                    </div>

                    <div className="mt-3 grid grid-cols-1 gap-3 border-t border-kumo-line pt-3 cq-sm:grid-cols-3">
                      <div className="flex flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">流量周期</label>
                        <Select
                          alignItemWithTrigger
                          size="sm"
                          aria-label="流量周期"
                          value={serverForm.trafficCycleType}
                          onValueChange={value =>
                            setServerForm(prev => ({ ...prev, trafficCycleType: String(value) }))
                          }
                          className="px-3 py-2"
                          items={TRAFFIC_CYCLE_OPTIONS}
                        />
                      </div>
                      {serverForm.trafficCycleType === 'monthly' && (
                        <div className="flex flex-col gap-1.5 cq-sm:col-span-2">
                          <label className="font-semibold text-kumo-subtle">账单日</label>
                          <Input
                            size="sm"
                            aria-label="每月流量重置日"
                            type="number"
                            min="1"
                            max="28"
                            step="1"
                            value={serverForm.trafficCycleDay}
                            onChange={e =>
                              setServerForm(prev => ({ ...prev, trafficCycleDay: e.target.value }))
                            }
                            onBlur={() =>
                              setServerForm(prev => ({
                                ...prev,
                                trafficCycleDay: normalizeTrafficCycleDayInput(
                                  prev.trafficCycleDay
                                ),
                              }))
                            }
                            className="px-3 py-2 text-kumo-strong"
                          />
                        </div>
                      )}
                      {serverForm.trafficCycleType === 'custom' && (
                        <div className="grid grid-cols-1 gap-3 cq-sm:col-span-2 cq-sm:grid-cols-2">
                          <div className="flex flex-col gap-1.5">
                            <label className="font-semibold text-kumo-subtle">周期开始</label>
                            <DateField
                              value={serverForm.trafficCycleStart}
                              onChange={trafficCycleStart =>
                                setServerForm(prev => ({ ...prev, trafficCycleStart }))
                              }
                              placeholder="未设置"
                            />
                          </div>
                          <div className="flex flex-col gap-1.5">
                            <label className="font-semibold text-kumo-subtle">周期结束</label>
                            <DateField
                              value={serverForm.trafficCycleEnd}
                              onChange={trafficCycleEnd =>
                                setServerForm(prev => ({ ...prev, trafficCycleEnd }))
                              }
                              placeholder="未设置"
                            />
                          </div>
                        </div>
                      )}
                      {(serverForm.trafficCycleType === 'calendar_month' ||
                        serverForm.trafficCycleType === 'none') && (
                        <div className="rounded-md border border-kumo-line bg-kumo-surface px-3 py-2 text-xs text-kumo-subtle cq-sm:col-span-2">
                          {serverForm.trafficCycleType === 'calendar_month'
                            ? '每月 1 日作为新流量周期。'
                            : '不设置重置周期，按累计流量显示。'}
                        </div>
                      )}
                    </div>
                  </section>

                  <section className="rounded-lg border border-kumo-line bg-kumo-recessed/20 p-3.5">
                    <div className="mb-3 flex items-center justify-between gap-3">
                      <h3 className="font-semibold text-kumo-strong">连接与验证</h3>
                      <div className="flex items-center gap-2">
                        <span className="text-[11px] text-kumo-subtle">SSH 登录信息</span>
                        <Button
                          size="sm"
                          variant="secondary"
                          onClick={testServerConnection}
                          disabled={serverModalSaving}
                        >
                          连接测试
                        </Button>
                      </div>
                    </div>

                    <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-3">
                      <div className="flex flex-col gap-1.5 cq-sm:col-span-2">
                        <label className="font-semibold text-kumo-subtle">
                          连接地址（IP 或域名）
                        </label>
                        <Input
                          size="sm"
                          aria-label="连接地址"
                          type="text"
                          value={serverForm.host}
                          onChange={e => setServerForm(prev => ({ ...prev, host: e.target.value }))}
                          placeholder="12.34.56.78"
                          className="px-3 py-2 text-kumo-strong"
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">端口</label>
                        <Input
                          size="sm"
                          aria-label="端口"
                          type="number"
                          value={serverForm.port}
                          onChange={e =>
                            setServerForm(prev => ({
                              ...prev,
                              port: parseInt(e.target.value) || 22,
                            }))
                          }
                          placeholder="22"
                          className="px-3 py-2 text-kumo-strong"
                        />
                      </div>
                    </div>

                    <div className="mt-3 flex flex-col gap-1.5">
                      <label className="font-semibold text-kumo-subtle">凭据预设</label>
                      <Select
                        alignItemWithTrigger
                        size="sm"
                        aria-label="选择凭据预设"
                        value={selectedCredentialId}
                        onValueChange={applyCredential}
                        placeholder="手动录入"
                        className="w-full min-w-0 px-3 py-2"
                        items={[
                          { value: '', label: '手动录入' },
                          ...serverCredentials.map(c => ({
                            value: String(c.id),
                            label: `${c.name} (${c.username})`,
                          })),
                        ]}
                      />
                    </div>

                    <div className="mt-3 flex flex-col gap-3 border-t border-kumo-line pt-3">
                      <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-2">
                        <div className="flex flex-col gap-1.5">
                          <label className="font-semibold text-kumo-subtle">登录用户名</label>
                          <Input
                            size="sm"
                            aria-label="登录用户名"
                            type="text"
                            value={serverForm.username}
                            onChange={e =>
                              setServerForm(prev => ({ ...prev, username: e.target.value }))
                            }
                            placeholder="root"
                            className="px-3 py-2 text-kumo-strong"
                          />
                        </div>
                        <div className="flex flex-col gap-1.5">
                          <label className="font-semibold text-kumo-subtle">身份验证方案</label>
                          <div className="flex flex-wrap gap-2">
                            <Button
                              size="sm"
                              variant={serverForm.authType === 'password' ? 'primary' : 'secondary'}
                              onClick={() =>
                                setServerForm(prev => ({ ...prev, authType: 'password' }))
                              }
                            >
                              密码验证
                            </Button>
                            <Button
                              size="sm"
                              variant={
                                serverForm.authType === 'privateKey' ? 'primary' : 'secondary'
                              }
                              onClick={() =>
                                setServerForm(prev => ({ ...prev, authType: 'privateKey' }))
                              }
                            >
                              密钥证书
                            </Button>
                          </div>
                        </div>
                      </div>

                      {serverForm.authType === 'password' ? (
                        <div className="flex flex-col gap-1.5">
                          <label className="font-semibold text-kumo-subtle">连接密码</label>
                          <Input
                            size="sm"
                            aria-label="连接密码"
                            type="text"
                            value={serverForm.password}
                            onChange={e =>
                              setServerForm(prev => ({ ...prev, password: e.target.value }))
                            }
                            placeholder={
                              serverModalMode === 'edit' ? '****** (留空不修改)' : '登录密码'
                            }
                            autoComplete="off"
                            data-1p-ignore
                            data-lpignore="true"
                            data-bwignore="true"
                            data-form-type="other"
                            spellCheck={false}
                            className="px-3 py-2 text-kumo-strong"
                          />
                        </div>
                      ) : (
                        <div className="flex flex-col gap-3">
                          <div className="flex flex-col gap-1.5">
                            <label className="font-semibold text-kumo-subtle">私钥证书</label>
                            <CodeEditor
                              label="证书密钥"
                              language="text"
                              value={serverForm.privateKey}
                              onChange={privateKey =>
                                setServerForm(prev => ({ ...prev, privateKey }))
                              }
                              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                              minHeight="8rem"
                            />
                          </div>
                          <div className="flex flex-col gap-1.5">
                            <label className="font-semibold text-kumo-subtle">
                              密钥口令（如有）
                            </label>
                            <Input
                              size="sm"
                              aria-label="密钥口令"
                              type="text"
                              value={serverForm.passphrase}
                              onChange={e =>
                                setServerForm(prev => ({ ...prev, passphrase: e.target.value }))
                              }
                              placeholder="Key Passphrase"
                              autoComplete="off"
                              data-1p-ignore
                              data-lpignore="true"
                              data-bwignore="true"
                              data-form-type="other"
                              spellCheck={false}
                              className="px-3 py-2 text-kumo-strong"
                            />
                          </div>
                        </div>
                      )}
                    </div>

                    <div className="mt-3 flex flex-col gap-1.5 border-t border-kumo-line pt-3">
                      <TagInput
                        size="sm"
                        label="主机标签"
                        value={serverForm.tags}
                        onValueChange={tags => setServerForm(prev => ({ ...prev, tags }))}
                        placeholder="输入后回车添加，如：生产环境"
                      />
                    </div>
                  </section>

                  <section className="rounded-lg border border-kumo-line bg-kumo-recessed/20 p-3.5">
                    <div className="mb-3 flex items-center justify-between gap-3">
                      <h3 className="font-semibold text-kumo-strong">远程桌面直连</h3>
                      <span className="text-[11px] text-kumo-subtle">可选</span>
                    </div>
                    <div className="flex flex-col gap-3">
                      <div className="flex flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">
                          1:1 NAT / 公网 IP
                        </label>
                        <Input
                          size="sm"
                          aria-label="远程桌面 1:1 NAT 公网 IP"
                          type="text"
                          value={serverForm.remoteDesktopNatIps}
                          onChange={e =>
                            setServerForm(prev => ({ ...prev, remoteDesktopNatIps: e.target.value }))
                          }
                          placeholder="203.0.113.7, 198.51.100.9（留空则不启用）"
                          spellCheck={false}
                          className="px-3 py-2 text-kumo-strong"
                        />
                        <span className="text-[11px] text-kumo-subtle">
                          填写后，远程桌面会把该地址作为 ICE 候选直接广播，免去 STUN
                          打洞即可建立直连；多个地址用逗号分隔。
                        </span>
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">候选类型</label>
                        <Select
                          alignItemWithTrigger
                          size="sm"
                          aria-label="远程桌面直连候选类型"
                          value={serverForm.remoteDesktopNatType}
                          onValueChange={value =>
                            setServerForm(prev => ({ ...prev, remoteDesktopNatType: value }))
                          }
                          className="w-full min-w-0 px-3 py-2"
                          items={[
                            { value: 'srflx', label: 'srflx（公网 IP 经 NAT 映射，推荐）' },
                            { value: 'host', label: 'host（地址就在本机网卡上）' },
                          ]}
                        />
                      </div>
                    </div>
                  </section>
                </>
              ) : (
                <div className="flex flex-col gap-4">
                  <Input
                    label="主机名称"
                    size="sm"
                    value={quickDeployName}
                    onChange={e => setQuickDeployName(e.target.value)}
                    placeholder="prod-agent-01"
                  />

                  <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-[11px] leading-relaxed text-kumo-subtle">
                    Agent 模式会创建或复用主机记录，并生成目标机器上的安装命令。
                  </div>
                  {quickDeployResult && (
                    <div className="flex flex-col gap-3">
                      <Select
                        alignItemWithTrigger
                        label="安装目标"
                        size="sm"
                        value={agentInstallOS}
                        onValueChange={setAgentInstallOS}
                        items={[
                          { value: 'linux', label: 'Linux / macOS' },
                          { value: 'win', label: 'Windows PowerShell' },
                        ]}
                      />
                      <ClipboardText
                        size="sm"
                        text={
                          isWindowsAgentInstallOs(agentInstallOS)
                            ? quickDeployResult.winInstallCommand || ''
                            : quickDeployResult.installCommand || ''
                        }
                        className="w-full"
                        tooltip={{ text: '复制命令', copiedText: '安装命令已复制', side: 'top' }}
                        labels={{ copyAction: '复制安装命令' }}
                      />
                      <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-2 text-[11px] leading-relaxed text-kumo-subtle">
                        {getAgentInstallExecutionHint(agentInstallOS)}
                      </div>
                      <div className="grid grid-cols-1 gap-2 text-[11px] text-kumo-subtle cq-sm:grid-cols-2">
                        <AppCard padding="none" className="p-2">
                          <div className="font-semibold text-kumo-strong">主机 ID</div>
                          <div className="mt-1 font-mono">{quickDeployResult.serverId}</div>
                        </AppCard>
                        <AppCard padding="none" className="p-2">
                          <div className="font-semibold text-kumo-strong">API 地址</div>
                          <div className="mt-1 truncate font-mono" title={quickDeployResult.apiUrl}>
                            {quickDeployResult.apiUrl}
                          </div>
                        </AppCard>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>

            {serverModalMode === 'add' && serverAddMode === 'agent' && (
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  loading={serverModalSaving}
                  onClick={generateQuickInstallCommand}
                >
                  生成 Agent 安装命令
                </Button>
              </div>
            )}
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Action type="button" loading={serverModalSaving} onClick={saveServer}>
              {serverModalMode === 'add' ? '新增' : '保存'}
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      {/* ==================== 模态框: 凭据预设新增 ==================== */}
      <LayerDialog.Root open={showAddCredentialModal} onOpenChange={setShowAddCredentialModal}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>新增 SSH 验证凭据</LayerDialog.Title>
          <LayerDialog.Body>
            <div className="flex flex-col gap-4 text-xs">
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">凭据别名</label>
                <Input
                  size="sm"
                  aria-label="凭据别名"
                  type="text"
                  value={credForm.name}
                  onChange={e => setCredForm(prev => ({ ...prev, name: e.target.value }))}
                  placeholder="美国节点通用 root 秘钥"
                  className="px-3 py-2 text-kumo-strong"
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle">用户登录名</label>
                <Input
                  size="sm"
                  aria-label="用户登录名"
                  type="text"
                  value={credForm.username}
                  onChange={e => setCredForm(prev => ({ ...prev, username: e.target.value }))}
                  placeholder="root"
                  className="px-3 py-2 text-kumo-strong"
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">登录凭据模式</label>
                <Select
                  alignItemWithTrigger
                  size="sm"
                  aria-label="登录凭据模式"
                  value={credForm.auth_type}
                  onValueChange={value =>
                    setCredForm(prev => ({ ...prev, auth_type: String(value) }))
                  }
                  className="w-full min-w-0 px-3 py-2"
                  items={[
                    { value: 'password', label: '明文密码' },
                    { value: 'key', label: '私钥证书 (RSA / OpenSSH)' },
                  ]}
                />
              </div>

              {credForm.auth_type === 'password' ? (
                <div className="flex flex-col gap-1.5">
                  <label className="font-semibold text-kumo-subtle">默认登录密码</label>
                  <Input
                    size="sm"
                    aria-label="默认登录密码"
                    type="text"
                    value={credForm.password}
                    onChange={e => setCredForm(prev => ({ ...prev, password: e.target.value }))}
                    autoComplete="off"
                    data-1p-ignore
                    data-lpignore="true"
                    data-bwignore="true"
                    data-form-type="other"
                    spellCheck={false}
                    className="px-3 py-2 text-kumo-strong"
                  />
                </div>
              ) : (
                <div className="flex flex-col gap-3">
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">PEM 私钥证书内容</label>
                    <CodeEditor
                      label="PEM 私钥证书内容"
                      language="text"
                      value={credForm.private_key}
                      onChange={private_key => setCredForm(prev => ({ ...prev, private_key }))}
                      placeholder="-----BEGIN RSA PRIVATE KEY-----"
                      minHeight="8rem"
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">
                      证书保护密码短语 (口令)
                    </label>
                    <Input
                      size="sm"
                      aria-label="证书保护密码短语"
                      type="text"
                      value={credForm.passphrase}
                      onChange={e => setCredForm(prev => ({ ...prev, passphrase: e.target.value }))}
                      placeholder="Passphrase"
                      autoComplete="off"
                      data-1p-ignore
                      data-lpignore="true"
                      data-bwignore="true"
                      data-form-type="other"
                      spellCheck={false}
                      className="px-3 py-2 text-kumo-strong"
                    />
                  </div>
                </div>
              )}
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Action type="button" onClick={addCredential}>
              保存
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      {/* ==================== 模态框: 导入主机备份 ==================== */}
      <LayerDialog.Root open={showImportServerModal} onOpenChange={setShowImportServerModal}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>导入主机备份配置</LayerDialog.Title>
          <LayerDialog.Body>
            <div className="flex flex-col gap-4 text-xs">
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">
                  选择备份 JSON 文件
                </label>
                <Input
                  size="sm"
                  aria-label="选择备份 JSON 文件"
                  type="file"
                  onChange={e => {
                    const f = e.target.files[0];
                    if (f) processImportFile(f);
                  }}
                  className="px-3 py-2"
                />
              </div>

              {importPreview && (
                <div className="bg-kumo-success/10 border border-kumo-success/20 p-2.5 rounded text-xs text-kumo-success font-semibold">
                  ✓ 已识别 {importPreview.length} 台主机，确认恢复？
                </div>
              )}

              {importModalError && (
                <div className="text-xs text-kumo-danger font-semibold bg-kumo-danger/10 border border-kumo-danger/20 p-2.5 rounded">
                  {importModalError}
                </div>
              )}
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Action
              type="button"
              onClick={confirmImportServers}
              disabled={importModalSaving || !importPreview}
            >
              确认恢复导入
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      {/* ==================== 模态框: 单机 Agent 部署 ==================== */}
      <LayerDialog.Root
        open={showAgentModal}
        onOpenChange={open => {
          setShowAgentModal(open);
          if (!open) setAgentModalData(null);
        }}
      >
        <LayerDialog.Content size="xl">
          <LayerDialog.Title>
            <span className="flex min-w-0 items-center gap-2">
              <span className="truncate">部署 Agent</span>
              {agentModalData?.serverName && (
                <Badge variant="outline" className="max-w-40 shrink-0 truncate text-[10px] leading-4">
                  {agentModalData.serverName}
                </Badge>
              )}
            </span>
          </LayerDialog.Title>
          <LayerDialog.Body>
            <div className="flex w-0 min-w-full flex-col gap-4 text-xs text-kumo-default">
            {agentInstallLoading && !agentModalData?.installCommand ? (
              <div className="space-y-3 py-8">
                <SkeletonLine className="h-4 w-1/3 mx-auto" />
                <SkeletonLine className="h-20 w-full" />
                <SkeletonLine className="h-4 w-2/3 mx-auto" />
              </div>
            ) : agentModalData ? (
              <div className="flex min-w-0 flex-col gap-4">
                <div className="flex min-w-0 flex-col gap-3">
                  <div className="font-semibold text-kumo-subtle">安装命令</div>
                  {[
                    { os: 'linux', label: 'Linux / macOS', binary: 'agent-linux-amd64', downloadLabel: 'Linux x64' },
                    { os: 'win', label: 'Windows', binary: 'agent-windows-amd64.exe', downloadLabel: 'Windows x64' },
                  ].map(({ os, label, binary, downloadLabel }) => (
                    <div
                      key={os}
                      className="flex min-w-0 flex-col gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3"
                    >
                      <div className="text-xs font-semibold text-kumo-strong">{label}</div>
                      <ClipboardText
                        size="sm"
                        text={getAgentInstallCommand(os)}
                        className="w-full"
                        tooltip={{ text: '复制', copiedText: 'Agent 安装命令已复制', side: 'top' }}
                        labels={{ copyAction: `复制 ${label} Agent 安装命令` }}
                      />
                    </div>
                  ))}
                  <div className="flex min-w-0 flex-col gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3">
                    <div className="flex items-center justify-between gap-2">
                      <div className="text-xs font-semibold text-kumo-strong">手动安装</div>
                      <div className="flex shrink-0 items-center gap-2">
                        <LinkButton
                          size="sm"
                          variant="secondary"
                          href={`${agentModalData.agentDownloadUrl || `${getAgentBaseApiUrl() || agentModalData.apiUrl}/agent`}/agent-linux-amd64`}
                          target="_blank"
                          rel="noreferrer"
                          external
                          icon={<Download className="h-3.5 w-3.5" />}
                        >
                          下载 Linux x64 Agent
                        </LinkButton>
                        <LinkButton
                          size="sm"
                          variant="secondary"
                          href={`${agentModalData.agentDownloadUrl || `${getAgentBaseApiUrl() || agentModalData.apiUrl}/agent`}/agent-windows-amd64.exe`}
                          target="_blank"
                          rel="noreferrer"
                          external
                          icon={<Download className="h-3.5 w-3.5" />}
                        >
                          下载 Windows x64 Agent
                        </LinkButton>
                      </div>
                    </div>
                    {agentModalData.agentKey && (
                      <>
                        <div className="text-[10px] text-kumo-subtle">Linux / macOS</div>
                        <ClipboardText
                          size="sm"
                          text={`chmod +x agent-linux-amd64 && ./agent-linux-amd64 service install --url ${getAgentBaseApiUrl()} --key ${agentModalData.agentKey}`}
                          className="w-full"
                          tooltip={{ text: '复制', copiedText: '手动安装命令已复制', side: 'top' }}
                          labels={{ copyAction: '复制 Linux 手动安装命令' }}
                        />
                        <div className="text-[10px] text-kumo-subtle">Windows PowerShell</div>
                        <ClipboardText
                          size="sm"
                          text={`.\\agent-windows-amd64.exe service install --url ${getAgentBaseApiUrl()} --key ${agentModalData.agentKey}`}
                          className="w-full"
                          tooltip={{ text: '复制', copiedText: '手动安装命令已复制', side: 'top' }}
                          labels={{ copyAction: '复制 Windows 手动安装命令' }}
                        />
                      </>
                    )}
                  </div>
                </div>

                {agentInstallLog && (
                  <CodeEditor
                    label="安装日志"
                    language="text"
                    value={agentInstallLog}
                    readOnly
                    minHeight="10rem"
                    className={`${
                      agentInstallResult === 'success'
                        ? 'ring-kumo-success/40'
                        : agentInstallResult === 'error'
                          ? 'ring-kumo-danger/40'
                          : agentInstallResult === 'warning'
                            ? 'ring-kumo-warning/40'
                            : ''
                    }`}
                  />
                )}
              </div>
            ) : (
              <div className="py-8 text-center text-xs text-kumo-subtle">未加载 Agent 数据</div>
            )}
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 pt-3">
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                size="sm"
                variant="secondary-destructive"
                icon={<Trash className="h-3.5 w-3.5" />}
                disabled={!agentModalData || agentInstallLoading || agentInstalling}
                onClick={() => agentModalData && uninstallAgent(agentModalData.serverId)}
              >
                卸载
              </Button>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                icon={<Key className="h-3.5 w-3.5" />}
                disabled={!agentModalData || agentInstallLoading || agentInstalling}
                onClick={regenerateAgentKey}
              >
                重新生成 Key
              </Button>
            </div>
            {canSshDeployAgent(
              serverList.find(server => server.id === agentModalData?.serverId)
            ) && (
              <Checkbox
                label="Linux SSH 覆盖"
                checked={agentForceSsh}
                disabled={agentInstallLoading || agentInstalling}
                onCheckedChange={checked => setAgentForceSsh(Boolean(checked))}
              />
            )}
          </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="关闭">
            <LayerDialog.Action
              type="button"
              icon={<Play className="h-3.5 w-3.5" />}
              loading={agentInstalling}
              disabled={!agentModalData || agentInstallLoading}
              onClick={() => agentModalData && autoInstallAgent(agentModalData.serverId)}
            >
              一键安装
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      {/* ==================== 模态框: 批量 Agent 部署 ==================== */}
      <LayerDialog.Root
        open={showBatchAgentModal}
        dismissDisabled={agentInstallLoading}
        onOpenChange={open => {
          if (!open && agentInstallLoading) return;
          setShowBatchAgentModal(open);
        }}
      >
        <LayerDialog.Content size="xl">
          <LayerDialog.Title>批量部署 Agent</LayerDialog.Title>
          <LayerDialog.Body>
            <div className="flex w-full flex-col gap-4">
              <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3 text-kumo-subtle">
                已选择 {selectedBatchServers.length} / {serverList.length} 台主机。
              </div>

              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="font-semibold text-kumo-strong">目标主机</div>
                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    onClick={selectAllBatchServers}
                    disabled={agentInstallLoading}
                  >
                    全选
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    onClick={() => setSelectedBatchServers([])}
                    disabled={agentInstallLoading}
                  >
                    清空
                  </Button>
                </div>
              </div>

              <div className="grid grid-cols-1 gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3 sm:grid-cols-2 md:grid-cols-3">
                {serverList.map(server => {
                  const deployable = canSshDeployAgent(server);
                  return (
                    <AppCard key={server.id} padding="none" className="p-2">
                      <Checkbox
                        label={
                          <span className="inline-flex min-w-0 items-center gap-1.5">
                            {getFlagCountry(server) && (
                              <CountryFlag
                                countryCode={getFlagCountry(server)}
                                className="h-3 w-4 text-xs"
                              />
                            )}
                            <span className="truncate font-semibold text-kumo-strong">
                              {server.name}
                            </span>
                            {!deployable && (
                              <span className="shrink-0 text-[10px] font-medium text-kumo-subtle">
                                无 SSH
                              </span>
                            )}
                          </span>
                        }
                        checked={selectedBatchServers.includes(server.id)}
                        disabled={agentInstallLoading || !deployable}
                        onCheckedChange={checked => {
                          setSelectedBatchServers(prev => {
                            if (checked)
                              return prev.includes(server.id) ? prev : [...prev, server.id];
                            return prev.filter(id => id !== server.id);
                          });
                        }}
                      />
                    </AppCard>
                  );
                })}
              </div>

              {batchInstallResults.length > 0 && (
                <div className="flex flex-col gap-2">
                  <div className="font-semibold text-kumo-strong">部署进度</div>
                  <div className="overflow-hidden rounded-md border border-kumo-line">
                    {batchInstallResults.map(result => (
                      <div
                        key={result.serverId}
                        className="flex items-center justify-between gap-3 border-b border-kumo-line bg-kumo-base px-3 py-2 last:border-b-0"
                      >
                        <div className="min-w-0">
                          <div className="truncate font-semibold text-kumo-strong">
                            {result.serverName}
                          </div>
                          {result.error && (
                            <div
                              className="truncate text-[11px] text-kumo-danger"
                              title={result.error}
                            >
                              {result.error}
                            </div>
                          )}
                        </div>
                        <span
                          className={`shrink-0 rounded px-2 py-0.5 text-[11px] font-semibold ${
                            result.status === 'success'
                              ? 'bg-kumo-success/10 text-kumo-success'
                              : result.status === 'failed'
                                ? 'bg-kumo-danger/10 text-kumo-danger'
                                : result.status === 'verifying'
                                  ? 'bg-kumo-warning/10 text-kumo-warning'
                                  : result.status === 'processing'
                                    ? 'bg-brand/10 text-brand'
                                    : 'bg-kumo-recessed text-kumo-subtle'
                          }`}
                        >
                          {result.status === 'waiting'
                            ? '等待'
                            : result.status === 'processing'
                              ? '处理中'
                              : result.status === 'verifying'
                                ? '验证中'
                                : result.status === 'success'
                                  ? '成功'
                                  : '失败'}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="关闭">
            <LayerDialog.Action
              type="button"
              icon={<Play className="h-3.5 w-3.5" />}
              loading={agentInstallLoading}
              disabled={selectedBatchServers.length === 0}
              onClick={runBatchAgentInstall}
            >
              开始部署
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      {/* ==================== 模态框: Agent 一键升级 ==================== */}
      <LayerDialog.Root
        open={showUpgradeModal}
        onOpenChange={open => {
          setShowUpgradeModal(open);
        }}
      >
        <LayerDialog.Content size="xl">
          <LayerDialog.Title>升级 Agent</LayerDialog.Title>
          <LayerDialog.Body>
            <div className="@container flex w-full flex-col gap-4 text-xs text-kumo-default">
              <div className="grid grid-cols-1 gap-2 cq-sm:grid-cols-2">
                <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3">
                  <div className="text-[11px] font-medium text-kumo-subtle">目标 Agent</div>
                  <div className="mt-1 text-sm font-semibold text-kumo-strong">
                    {upgradeBatchSnapshot?.items?.length || getAgentUpgradeTargets().length}
                  </div>
                </div>
                <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3">
                  <div className="text-[11px] font-medium text-kumo-subtle">状态</div>
                  <div className="mt-1 font-semibold text-kumo-strong">
                    {upgrading
                      ? getUpgradeBatchStatusLabel(upgradeBatchSnapshot?.status || 'running')
                      : upgradeBatchSnapshot
                        ? getUpgradeBatchStatusLabel(upgradeBatchSnapshot.status)
                        : upgradeProgress >= 100
                          ? '已完成'
                          : '待执行'}
                  </div>
                </div>
              </div>

              {upgradeProgress > 0 && (
                <Meter
                  label="升级进度"
                  value={upgradeProgress}
                  min={0}
                  max={100}
                  customValue={`${upgradeProgress}%`}
                />
              )}

              <div className="grid min-w-0 gap-4 cq-md:grid-cols-[minmax(18rem,0.85fr)_minmax(0,1.15fr)] cq-md:items-stretch">
                <div className="min-w-0 space-y-1.5">
                  <div className="text-xs font-semibold text-kumo-strong">主机状态</div>
                  {upgradeBatchSnapshot?.items?.length > 0 ? (
                    <div className="max-h-[24rem] overflow-auto rounded-md border border-kumo-line scrollbar-thin">
                      {upgradeBatchSnapshot.items.map(item => (
                        <div
                          key={item.serverId}
                          className="flex min-w-0 items-center justify-between gap-3 border-b border-kumo-line bg-kumo-base px-3 py-2 last:border-b-0"
                        >
                          <div className="min-w-0">
                            <div className="truncate text-xs font-semibold text-kumo-strong">
                              {item.serverName || item.serverId}
                            </div>
                            {(item.error || item.log?.at?.(-1)) && (
                              <div
                                className={`mt-0.5 truncate text-[11px] ${item.error ? 'text-kumo-danger' : 'text-kumo-subtle'}`}
                                title={item.error || item.log.at(-1)}
                              >
                                {item.error || item.log.at(-1)}
                              </div>
                            )}
                          </div>
                          <Badge
                            variant={
                              item.status === 'succeeded'
                                ? 'success'
                                : item.status === 'failed'
                                  ? 'danger'
                                  : item.status === 'verifying'
                                    ? 'warning'
                                    : 'info'
                            }
                          >
                            {getUpgradeItemStatusLabel(item.status)}
                          </Badge>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <AppCard padding="none" className="min-h-56 p-3 text-kumo-subtle">
                      等待开始升级。
                    </AppCard>
                  )}
                </div>

                <div className="min-w-0 space-y-1.5">
                  <div className="text-xs font-semibold text-kumo-strong">升级日志</div>
                  {upgradeLog ? (
                    <pre
                      ref={upgradeLogViewportRef}
                      className="min-h-56 max-h-[24rem] overflow-auto whitespace-pre-wrap break-words rounded-md border border-kumo-line bg-kumo-recessed/25 p-3 font-mono text-[11px] leading-5 text-kumo-strong scrollbar-thin"
                    >
                      {upgradeLog}
                    </pre>
                  ) : (
                    <AppCard padding="none" className="min-h-56 p-3 text-kumo-subtle">
                      将对在线 Agent 下发后台自升级任务。
                    </AppCard>
                  )}
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-3 text-xs text-kumo-subtle">
                <Checkbox
                  label="Linux SSH 保底"
                  checked={upgradeFallbackSsh}
                  disabled={upgrading}
                  onCheckedChange={checked => setUpgradeFallbackSsh(Boolean(checked))}
                />
              </div>
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel={upgrading ? '后台运行' : '关闭'}>
            <LayerDialog.Action
              type="button"
              icon={<Upload className="h-3.5 w-3.5" />}
              loading={upgrading}
              disabled={upgrading || getAgentUpgradeTargets().length === 0}
              onClick={performOneKeyUpgrade}
            >
              开始升级
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      <LayerDialog.Root
        open={Boolean(dockerComposeEditor)}
        onOpenChange={open => {
          if (!open) requestCloseDockerComposeEditor();
        }}
      >
        <LayerDialog.Content size="xl">
          <LayerDialog.Title>
            <span className="flex min-w-0 items-center gap-2">
              <FolderOpen className="h-4 w-4 shrink-0 text-brand" />
              <span className="truncate">
                {dockerComposeEditor?.mode === 'edit' ? '修改 Compose 配置' : '查看 Compose 配置'}
              </span>
            </span>
          </LayerDialog.Title>
          <LayerDialog.Body>
            <div className="w-0 min-w-full text-xs text-kumo-default">
              <div className="@container grid min-w-0 items-start gap-2 grid-cols-1 cq-md:grid-cols-[17rem_minmax(0,1fr)]">
            <div className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-kumo-line/70">
              <div className="flex shrink-0 items-center justify-between gap-2 border-b border-kumo-line bg-kumo-recessed/40 px-2 py-1.5">
                <span className="inline-flex min-w-0 items-center gap-1.5 text-xs font-semibold text-kumo-strong">
                  <Settings className="h-3.5 w-3.5 shrink-0 text-brand" />
                  项目信息
                </span>
                <Badge
                  variant={dockerComposeEditor?.status?.includes('运行') ? 'success' : 'neutral'}
                  appearance="dot"
                >
                  {dockerComposeEditor?.status || '-'}
                </Badge>
              </div>
              <div className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-y-auto p-2">
                <div className="grid grid-cols-2 items-start gap-1.5">
                  <div className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-1.5 py-1">
                    <div className="text-[10px] leading-none text-kumo-subtle">主机</div>
                    <div
                      className="mt-0.5 truncate text-xs font-semibold leading-tight text-kumo-strong"
                      title={dockerComposeEditor?.serverName || '-'}
                    >
                      {dockerComposeEditor?.serverName || '-'}
                    </div>
                  </div>
                  <div className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-1.5 py-1">
                    <div className="text-[10px] leading-none text-kumo-subtle">项目</div>
                    <div
                      className="mt-0.5 truncate text-xs font-semibold leading-tight text-kumo-strong"
                      title={dockerComposeEditor?.projectName || '-'}
                    >
                      {dockerComposeEditor?.projectName || '-'}
                    </div>
                  </div>
                </div>
                <div className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-1.5 py-1">
                  <div className="text-[10px] leading-none text-kumo-subtle">工作目录</div>
                  <div className="mt-0.5 break-all font-mono text-[11px] leading-tight text-kumo-strong">
                    {dockerComposeEditor?.workingDir || '-'}
                  </div>
                </div>
                <div className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-1.5 py-1">
                  <div className="text-[10px] leading-none text-kumo-subtle">配置文件</div>
                  <div className="mt-0.5 flex flex-col gap-0.5">
                    {(dockerComposeEditor?.configFiles?.length
                      ? dockerComposeEditor.configFiles
                      : ['-']
                    ).map(path => (
                      <span
                        key={path}
                        className="break-all font-mono text-[11px] leading-tight text-kumo-strong"
                      >
                        {path}
                      </span>
                    ))}
                  </div>
                </div>
                {dockerComposeEditor?.mode === 'view' && dockerComposeEditor?.path && (
                  <Button
                    size="sm"
                    variant="primary"
                    icon={<Edit className="h-3.5 w-3.5" />}
                    onClick={() =>
                      setDockerComposeEditor(prev => (prev ? { ...prev, mode: 'edit' } : prev))
                    }
                    className="mt-0.5 w-full justify-center"
                  >
                    修改配置
                  </Button>
                )}
              </div>
            </div>

            <div className="flex min-h-0 min-w-0 flex-col gap-2">
              {dockerComposeEditor?.error && (
                <Badge variant="error">{dockerComposeEditor.error}</Badge>
              )}
              <CodeEditor
                label="Compose 配置内容"
                language="yaml"
                value={
                  dockerComposeEditor?.loading
                    ? '正在读取 Compose 配置...'
                    : dockerComposeEditor?.content || ''
                }
                onChange={content =>
                  setDockerComposeEditor(prev => (prev ? { ...prev, content } : prev))
                }
                readOnly={dockerComposeEditor?.mode !== 'edit' || dockerComposeEditor?.loading}
                minHeight="16rem"
              />
              </div>
            </div>
          </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="关闭">
            {dockerComposeEditor?.mode === 'edit' ? (
              <LayerDialog.Action
                type="button"
                icon={<Save className="h-3.5 w-3.5" />}
                loading={dockerComposeEditor?.saving}
                disabled={dockerComposeEditor?.loading || !dockerComposeEditor?.path}
                onClick={saveDockerComposeConfig}
              >
                保存
              </LayerDialog.Action>
            ) : (
              <LayerDialog.Action
                type="button"
                icon={<Upload className="h-3.5 w-3.5" />}
                loading={dockerComposeEditor?.updating}
                disabled={dockerComposeEditor?.updating || !dockerComposeEditor?.path || !dockerComposeEditor?.saved}
                onClick={updateDockerComposeDeployment}
              >
                更新编排
              </LayerDialog.Action>
            )}
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      <LayerDialog.Root
        open={dockerLogsModalOpen}
        onOpenChange={open => {
          setDockerLogsModalOpen(open);
          if (!open) {
            setDockerLogsContent('');
            setDockerLogsLoading(false);
          }
        }}
      >
        <LayerDialog.Content size="lg">
          <LayerDialog.Title>
            <FileText className="h-4 w-4 text-brand" />
            <span>
              容器日志: {dockerLogsContainer ? getDockerContainerName(dockerLogsContainer) : ''}
            </span>
          </LayerDialog.Title>
          <LayerDialog.Body>
            <div className="flex flex-col gap-3 text-xs font-mono text-kumo-default">
              <div className="flex items-center gap-3">
                <Select
                  alignItemWithTrigger
                  size="sm"
                  label="日志行数"
                  value={String(dockerLogsTail)}
                  onValueChange={value => {
                    const val = Number(value);
                    setDockerLogsTail(val);
                    loadDockerContainerLogs(dockerLogsServer, dockerLogsContainer, val);
                  }}
                  disabled={dockerLogsLoading}
                  items={DOCKER_LOG_TAIL_ITEMS}
                />
                {dockerLogsLoading && (
                  <span className="inline-flex items-center gap-1 text-[11px] text-kumo-subtle">
                    <RefreshCw className="h-3 w-3 animate-spin" />
                    更新中...
                  </span>
                )}
              </div>
              <div className="min-h-96 flex-1 rounded border border-kumo-line bg-kumo-canvas/15 p-2 overflow-auto max-h-[50vh] whitespace-pre-wrap select-text font-mono text-[11px] leading-relaxed">
                {dockerLogsContent}
              </div>
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="关闭">
            <LayerDialog.Action
              type="button"
              onClick={() => {
                loadDockerContainerLogs(dockerLogsServer, dockerLogsContainer, dockerLogsTail);
              }}
              disabled={dockerLogsLoading}
              icon={
                <RefreshCw className={`h-3.5 w-3.5 ${dockerLogsLoading ? 'animate-spin' : ''}`} />
              }
            >
              刷新
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
      <LayerDialog.Root open={showNetworkTargetModal} onOpenChange={setShowNetworkTargetModal}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>
            {networkTargetModalMode === 'add' ? '添加拨测目标' : '编辑拨测目标'}
          </LayerDialog.Title>
          <LayerDialog.Body>
            <form
              id="network-target-form"
              onSubmit={saveNetworkTarget}
              className="flex flex-col gap-3"
            >
              <Input
                size="sm"
                label="目标名称"
                required
                value={networkTargetForm.name}
                onChange={e => setNetworkTargetForm(prev => ({ ...prev, name: e.target.value }))}
                placeholder="例如: 电信"
              />
              <Input
                size="sm"
                label="节点地址 (IP / Host)"
                required
                value={networkTargetForm.host}
                onChange={e => setNetworkTargetForm(prev => ({ ...prev, host: e.target.value }))}
                placeholder="如：hb-ct-v4.ip.zstaticcdn.com"
              />
              <div className="grid gap-3 cq-sm:grid-cols-[minmax(0,1fr)_9rem]">
                <Input
                  size="sm"
                  label="端口"
                  type="number"
                  required
                  value={networkTargetForm.port}
                  onChange={e =>
                    setNetworkTargetForm(prev => ({ ...prev, port: parseInt(e.target.value) || 0 }))
                  }
                />
                <Select
                  alignItemWithTrigger
                  size="sm"
                  label="协议"
                  value={networkTargetForm.type}
                  onValueChange={value =>
                    setNetworkTargetForm(prev => ({ ...prev, type: String(value) }))
                  }
                  className="w-full min-w-0"
                  items={[
                    { label: 'TCP', value: 'tcp' },
                    { label: 'UDP', value: 'udp' },
                  ]}
                />
              </div>
              <Input
                size="sm"
                label="排序权重"
                type="number"
                value={networkTargetForm.order_index}
                onChange={e =>
                  setNetworkTargetForm(prev => ({
                    ...prev,
                    order_index: parseInt(e.target.value) || 0,
                  }))
                }
              />
              <div className="flex items-center gap-2 mt-2">
                <Checkbox
                  checked={networkTargetForm.enabled}
                  onChange={() =>
                    setNetworkTargetForm(prev => ({ ...prev, enabled: !prev.enabled }))
                  }
                  id="target-enabled-checkbox"
                />
                <label
                  htmlFor="target-enabled-checkbox"
                  className="text-xs font-semibold text-kumo-strong select-none cursor-pointer"
                >
                  是否启用该探测目标
                </label>
              </div>
            </form>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Action type="submit" form="network-target-form">
              保存
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </>
  );
}
