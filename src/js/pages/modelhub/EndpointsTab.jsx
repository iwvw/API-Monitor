import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { ClipboardText, LayerCard, Loader, Popover, Table, Badge } from '@cloudflare/kumo';
import {
  AppCard,
  AppTable,
  EmptyState,
  StatusBadge,
  actionIconClass,
} from '../../components/ui/AppPrimitives.jsx';
import {
  Bot,
  Server,
  Plus,
  Trash,
  Edit,
  RefreshCw,
  Activity,
  Copy,
  Key,
} from '../../components/Icons.jsx';
import {
  activeModelIdsForEndpoint,
} from './utils.js';
import { modelHealthKey } from '../../modules/modelhubModelHealth.js';
import { ProxyRuntimeMeta } from './ProxyRuntimeMeta.jsx';
import { MultiSelectPopover } from './MultiSelectPopover.jsx';
import { KeyStatusBadge } from './KeyStatusBadge.jsx';
import { toast } from '../../modules/toast.js';
import {
  MobileCard,
  MobileCardActions,
  MobileCardEmpty,
  MobileCardList,
  MobileCardSkeleton,
} from './MobileTableCards.jsx';

const ENDPOINT_LIST_COLUMNS = [
  { id: 'endpoint', role: 'primary', minWidth: 200, grow: 1 },
  { id: 'priority', role: 'count', align: 'center', width: 72 },
  { id: 'weight', role: 'count', align: 'center', width: 72 },
  { id: 'status', role: 'status', width: 72 },
];

const ENDPOINT_MODEL_COLUMNS = [
  { id: 'check', role: 'control' },
  { id: 'model', role: 'primary', minWidth: 260, maxWidth: 320, grow: 1 },
  { id: 'mapping', role: 'meta', minWidth: 180, grow: 1 },
  { id: 'health', role: 'status' },
  { id: 'actions', role: 'actions-md' },
];

function healthMeta(health) {
  const tone = health?.loading
    ? 'info'
    : health?.status === 'healthy'
      ? 'success'
      : health?.status === 'degraded'
        ? 'warning'
        : health?.status === 'error'
          ? 'danger'
          : 'neutral';
  const label = health?.loading
    ? '检测中'
    : health?.status === 'healthy'
      ? '可用'
      : health?.status === 'degraded'
        ? '较慢'
        : health?.status === 'error'
          ? '失败'
          : health?.status === 'cancelled'
            ? '已停止'
            : '未检测';
  return { tone, label };
}

function RoutingValue({
  item,
  field,
  min,
  max,
  displayValue,
  emptyValue,
  hint,
  tapToEdit = false,
  routingEditKey,
  routingDraft,
  setRoutingDraft,
  setRoutingEditKey,
  saveEndpointRouting,
}) {
  const editKey = `${item.id}:${field}`;
  const commit = () =>
    saveEndpointRouting(item.id, field, Number(routingDraft) || emptyValue);

  if (routingEditKey === editKey) {
    return (
      <Input
        autoFocus
        size="sm"
        type="number"
        min={min}
        max={max}
        aria-label={hint}
        value={routingDraft}
        onChange={event => setRoutingDraft(event.target.value)}
        onKeyDown={event => {
          event.stopPropagation();
          if (event.key === 'Enter') {
            commit();
          } else if (event.key === 'Escape') {
            setRoutingEditKey(null);
          }
        }}
        onBlur={() => {
          if (routingEditKey === editKey) commit();
        }}
        onClick={event => event.stopPropagation()}
        className="h-6 w-12 shrink-0 text-center font-mono text-[11px]"
      />
    );
  }

  const startEdit = event => {
    event.stopPropagation();
    setRoutingDraft(String(item[field] ?? displayValue));
    setRoutingEditKey(editKey);
  };

  return (
    <span
      className="inline-flex h-6 w-12 shrink-0 cursor-text items-center justify-end font-mono text-[11px] leading-normal text-kumo-strong"
      title={hint}
      onClick={tapToEdit ? startEdit : undefined}
      onDoubleClick={startEdit}
    >
      {item[field] ?? displayValue}
    </span>
  );
}

function MappingValue({
  endpoint,
  modelId,
  idleText = '双击设置',
  tapToEdit = false,
  mappingEditKey,
  mappingDraft,
  setMappingDraft,
  setMappingEditKey,
  saveEndpointMapping,
}) {
  const editKey = `${endpoint.id}:${modelId}`;

  if (mappingEditKey === editKey) {
    return (
      <Input
        autoFocus
        size="sm"
        value={mappingDraft}
        aria-label="模型对外映射名称"
        onChange={event => setMappingDraft(event.target.value)}
        onKeyDown={event => {
          event.stopPropagation();
          if (event.key === 'Enter') {
            saveEndpointMapping(endpoint, modelId, mappingDraft);
          } else if (event.key === 'Escape') {
            setMappingEditKey(null);
          }
        }}
        onBlur={() => {
          if (mappingEditKey === editKey) {
            saveEndpointMapping(endpoint, modelId, mappingDraft);
          }
        }}
        className="w-full font-mono text-[10px]"
        placeholder="对外名称"
      />
    );
  }

  const startEdit = event => {
    event.stopPropagation();
    setMappingDraft(endpoint.modelMappings?.[modelId] || '');
    setMappingEditKey(editKey);
  };

  return (
    <span
      className="block cursor-text truncate font-mono text-[10px]"
      title="双击编辑对外映射名称"
      onClick={tapToEdit ? startEdit : undefined}
      onDoubleClick={startEdit}
    >
      {endpoint.modelMappings?.[modelId] ? (
        <span className="text-base text-brand">{endpoint.modelMappings[modelId]}</span>
      ) : (
        <span className="text-kumo-subtle">{idleText}</span>
      )}
    </span>
  );
}

function copyTextHelper(value, label = '内容') {
  const text = String(value || '').trim();
  if (!text) {
    toast.error('没有可复制的内容');
    return;
  }
  const fallback = () => {
    try {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.setAttribute('readonly', '');
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
      toast.success(`${label}已复制`);
    } catch {
      toast.error('复制失败');
    }
  };
  if (navigator.clipboard?.writeText) {
    navigator.clipboard
      .writeText(text)
      .then(() => toast.success(`${label}已复制`))
      .catch(() => fallback());
  } else {
    fallback();
  }
}

export function EndpointsTab({
  endpointsApi,
  healthApi,
  keysApi,
  exposedModels,
  exposedModelsLoading,
  gatewayOrigin,
  renderExposedModels,
  handleExposedModelsOpenChange,
  loadExposedModels,
  batchCloseNonHealthyModels,
  modelBatchActionLoading,
  setAddModelOpen,
  setAddModelTarget,
  setAddModelText,
}) {
  const {
    endpoints,
    endpointsLoading,
    endpointImportInputRef,
    importEndpointsFromFile,
    selectedEndpoint,
    setSelectedEndpointId,
    openAddEndpointModal,
    openEditEndpointModal,
    refreshEndpointModels,
    saveEndpointMapping,
    saveEndpointRouting,
    mappingDraft,
    mappingEditKey,
    routingDraft,
    routingEditKey,
    setMappingDraft,
    setMappingEditKey,
    setRoutingDraft,
    setRoutingEditKey,
    toggleEndpointEnabled,
    toggleModelEnabled,
    modelEnabledForEndpoint,
    modelSwitchLoading,
    endpointToggleLoading,
    batchEnableDisabledModels,
    deleteEndpoint,
    deleteEndpointConfirmActive,
  } = endpointsApi;
  const {
    modelHealthBatchLoading,
    modelHealthAbortControllersRef,
    modelhubModelHealth,
    testModelHealth,
    openHealthCheckForEndpoint,
  } = healthApi;
  const { defaultGatewayKey } = keysApi;

  return (
        <div className="flex min-h-0 flex-1 flex-col gap-2.5">
          <Input
            ref={endpointImportInputRef}
            type="file"
            accept="application/json,.json"
            aria-label="导入端点 JSON"
            className="hidden"
            onChange={importEndpointsFromFile}
          />
          <div className="flex flex-col gap-2 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
<div className="flex min-w-0 flex-col gap-2 cq-tight:flex-row cq-tight:items-center">
              <ClipboardText
                size="sm"
                text={`${gatewayOrigin}/v1`}
                className="min-w-0 max-w-md flex-1 font-mono text-[0.9em]"
                tooltip={{ text: '复制 API Base URL', copiedText: '地址已复制', side: 'bottom' }}
                labels={{ copyAction: '复制 API Base URL' }}
              />
              {defaultGatewayKey?.apiKey ? (
                <ClipboardText
                  size="sm"
                  text={defaultGatewayKey.apiKey}
                  className="min-w-0 max-w-md flex-1 font-mono text-[0.9em]"
                  tooltip={{ text: '复制默认密钥', copiedText: '密钥已复制', side: 'bottom' }}
                  labels={{ copyAction: '复制默认密钥' }}
                />
              ) : (
                <span className="shrink-0 text-xs font-medium text-kumo-subtle">
                  未设置默认密钥
                </span>
              )}
              <Popover onOpenChange={handleExposedModelsOpenChange}>
                <Popover.Trigger
                  nativeButton={false}
                  render={
                    <Button type="button" size="sm" variant="secondary" className="shrink-0 focus:outline-none focus:ring-0 focus-visible:outline-none focus-visible:ring-0">
                      模型
                      <Badge variant="secondary">{exposedModels.length}</Badge>
                    </Button>
                  }
                />
                <Popover.Content side="bottom" align="start" className="w-72 shrink-0 px-3 pb-2 pt-2.5 max-h-[min(70vh,28rem)] overflow-y-auto overscroll-contain scrollbar-thin">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-semibold leading-normal text-kumo-strong">对外暴露的模型</span>
                    <Button
                      type="button"
                      size="xs"
                      variant="ghost"
                      onClick={loadExposedModels}
                      disabled={exposedModelsLoading}
                      aria-label="刷新对外模型"
                    >
                      <RefreshCw className={`h-3 w-3 ${exposedModelsLoading ? 'animate-spin' : ''}`} />
                    </Button>
                  </div>
                  <div className="mt-1.5">
                    {renderExposedModels()}
                  </div>
                  <div className="mt-1.5 border-t border-kumo-line pt-1.5 pb-0.5">
                    <span className="text-[0.7em] leading-normal text-kumo-subtle">
                      共 {exposedModels.length} 个模型 · 实时取自 /v1/models
                    </span>
                  </div>
                </Popover.Content>
              </Popover>
            </div>
            </div>
          {endpointsLoading ? (
            <div className="space-y-2.5">
              {[...Array(2)].map((_, i) => (
                <AppCard key={i} padding="md" className="space-y-2.5">
                  <div className="flex items-center gap-3">
                    <SkeletonLine className="w-10 h-10 rounded-lg" />
                    <div className="flex-1 space-y-1.5">
                      <SkeletonLine className="w-1/4 h-3.5" />
                      <SkeletonLine className="w-1/2 h-2.5" />
                    </div>
                  </div>
                </AppCard>
              ))}
            </div>
          ) : endpoints.length === 0 ? (
            <EmptyState
              icon={Bot}
              title="暂无 API 端点"
              description="新增 OpenAI 兼容端点"
            />
          ) : (
            (() => {
              const endpoint = selectedEndpoint;
              const validStatus = endpoint.status === 'valid';
              const invalidStatus = endpoint.status === 'invalid';
              const disabledModelCount = Array.isArray(endpoint.disabledModels)
                ? endpoint.disabledModels.length
                : 0;

              const renderBatchSwitch = () => (
                <Switch
                  size="sm"
                  aria-label={
                    disabledModelCount > 0
                      ? `启用 ${disabledModelCount} 个被停用的模型`
                      : '关闭所有非有效模型(检测有效的保留)'
                  }
                  checked={disabledModelCount === 0}
                  onCheckedChange={checked => {
                    if (checked) {
                      batchEnableDisabledModels(endpoint);
                    } else {
                      batchCloseNonHealthyModels(endpoint);
                    }
                  }}
                  disabled={modelBatchActionLoading || modelHealthBatchLoading}
                />
              );

              const renderEndpointCard = item => (
                <MobileCard
                  key={item.id}
                  selected={item.id === endpoint.id}
                  clickable
                  onClick={() => setSelectedEndpointId(item.id)}
                >
                  <div className="flex min-w-0 items-start gap-2">
                    <span
                      className="inline-flex w-7 shrink-0 items-center justify-center rounded border-2 border-kumo-line/70 font-mono text-xs font-semibold leading-5 tabular-nums text-brand"
                      title="启用模型数"
                    >
                      {activeModelIdsForEndpoint(item).length}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex min-w-0 items-center gap-1.5">
                        {item.pluginId && (
                          <span
                            className="shrink-0 rounded border-2 border-kumo-line/70 px-1 text-[10px] font-semibold leading-5 text-brand"
                            title="由插件注册"
                          >
                            插件
                          </span>
                        )}
                        <span
                          className="truncate text-sm font-semibold leading-5 text-kumo-strong"
                          title={item.name}
                        >
                          {item.name || '未命名端点'}
                        </span>
                      </div>
                      <div
                        className="truncate font-mono text-[10px] leading-4 text-kumo-subtle"
                        title={item.baseUrl}
                      >
                        {item.baseUrl}
                      </div>
                    </div>
                    <div className="shrink-0" onClick={event => event.stopPropagation()}>
                      <Switch
                        size="sm"
                        aria-label={item.enabled ? '停用端点' : '启用端点'}
                        checked={item.enabled}
                        onCheckedChange={() => toggleEndpointEnabled(item)}
                        disabled={!!endpointToggleLoading[item.id]}
                      />
                    </div>
                  </div>
                  <div className="mt-2 flex items-center justify-between gap-2">
                    <span className="shrink-0 text-[11px] leading-normal text-kumo-subtle">路由</span>
                    <div className="flex shrink-0 items-center gap-1.5 text-[11px] leading-normal">
                      <span className="text-kumo-subtle">优先</span>
                      <RoutingValue
                        item={item}
                        field="priority"
                        min={0}
                        max={999}
                        displayValue={0}
                        emptyValue={0}
                        hint="点击编辑路由优先级(值越大越优先)"
                        tapToEdit
                        routingEditKey={routingEditKey}
                        routingDraft={routingDraft}
                        setRoutingDraft={setRoutingDraft}
                        setRoutingEditKey={setRoutingEditKey}
                        saveEndpointRouting={saveEndpointRouting}
                      />
                      <span className="ml-1 text-kumo-subtle">权重</span>
                      <RoutingValue
                        item={item}
                        field="weight"
                        min={1}
                        max={9999}
                        displayValue={100}
                        emptyValue={1}
                        hint="点击编辑加权因子(值越大被选中概率越高)"
                        tapToEdit
                        routingEditKey={routingEditKey}
                        routingDraft={routingDraft}
                        setRoutingDraft={setRoutingDraft}
                        setRoutingEditKey={setRoutingEditKey}
                        saveEndpointRouting={saveEndpointRouting}
                      />
                    </div>
                  </div>
                </MobileCard>
              );

              const renderModelCard = model => {
                const modelId =
                  typeof model === 'string' ? model.trim() : (model.id || '').trim();
                const healthKey = modelHealthKey(endpoint.id, modelId);
                const health = modelhubModelHealth[healthKey];
                const canStopHealthCheck =
                  health?.loading && modelHealthAbortControllersRef.current.has(healthKey);
                const { tone: healthTone, label: healthLabel } = healthMeta(health);
                const modelName = endpoint.modelNames?.[modelId];
                const enabled = modelEnabledForEndpoint(endpoint, modelId);
                return (
                  <MobileCard key={`mobile:${endpoint.id}:${modelId}`}>
                    <div className="flex min-w-0 items-start gap-2">
                      <div className="pt-0.5" onClick={event => event.stopPropagation()}>
                        <Switch
                          size="sm"
                          aria-label={enabled ? `停用 ${modelId}` : `启用 ${modelId}`}
                          checked={enabled}
                          onCheckedChange={value => toggleModelEnabled(endpoint, modelId, value)}
                          disabled={!!modelSwitchLoading[`${endpoint.id}:${modelId}`]}
                        />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div
                          className="truncate text-sm font-medium leading-5 text-kumo-strong"
                          title={modelName ? `${modelName}(${modelId})` : modelId}
                        >
                          {modelName || modelId}
                        </div>
                        {modelName && (
                          <div
                            className="truncate font-mono text-[10px] leading-4 text-kumo-subtle"
                            title={modelId}
                          >
                            {modelId}
                          </div>
                        )}
                      </div>
                      <div className="shrink-0">
                        {health?.status !== 'healthy' && health?.status !== 'degraded' ? (
                          <StatusBadge tone={healthTone}>{healthLabel}</StatusBadge>
                        ) : (
                          health?.latency != null && (
                            <span
                              className={`font-mono text-xs font-semibold ${
                                health?.status === 'healthy'
                                  ? 'text-kumo-success'
                                  : 'text-kumo-warning'
                              }`}
                            >
                              {health.latency} ms
                            </span>
                          )
                        )}
                      </div>
                    </div>
                    <div className="mt-2 flex min-w-0 items-center gap-1.5 text-[11px] leading-normal">
                      <span className="shrink-0 text-kumo-subtle">映射</span>
                      <div className="min-w-0 flex-1">
                        <MappingValue
                          endpoint={endpoint}
                          modelId={modelId}
                          idleText="未设置"
                          tapToEdit
                          mappingEditKey={mappingEditKey}
                          mappingDraft={mappingDraft}
                          setMappingDraft={setMappingDraft}
                          setMappingEditKey={setMappingEditKey}
                          saveEndpointMapping={saveEndpointMapping}
                        />
                      </div>
                    </div>
                    <MobileCardActions>
                      <Button
                        shape="square"
                        size="sm"
                        variant={canStopHealthCheck ? 'secondary-destructive' : 'secondary'}
                        aria-label={
                          canStopHealthCheck ? `停止检测 ${modelId}` : `检测 ${modelId}`
                        }
                        onClick={() => testModelHealth({ id: modelId }, endpoint.id)}
                        disabled={modelHealthBatchLoading}
                        title={
                          health?.error ||
                          (canStopHealthCheck
                            ? '停止检测'
                            : health?.loading
                              ? '检测中'
                              : '检测模型')
                        }
                        icon={
                          health?.loading ? (
                            <Loader size="sm" />
                          ) : (
                            <Activity className="h-3.5 w-3.5" />
                          )
                        }
                      />
                      <Button
                        shape="square"
                        size="sm"
                        variant="secondary"
                        aria-label={`复制 ${modelId}`}
                        onClick={() => {
                          navigator.clipboard.writeText(modelId);
                          toast.success('已复制模型名称');
                        }}
                        title="复制模型名称"
                        icon={<Copy className="h-3.5 w-3.5" />}
                      />
                    </MobileCardActions>
                  </MobileCard>
                );
              };

              return (
                <div className="grid min-w-0 gap-3 cq-lg:grid-cols-[fit-content(28rem)_minmax(0,1fr)] cq-lg:grid-rows-[minmax(0,calc(100dvh-11.25rem))]">
                  <section className="flex min-h-0 min-w-0 flex-col gap-2">
                    <div className="flex min-h-8 items-center justify-between gap-2 px-1">
                      <div className="flex items-center gap-2 text-xs text-kumo-subtle">
                        <Server className="h-3.5 w-3.5" />
                        <span className="font-medium text-kumo-strong">上游端点</span>
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-xs text-kumo-subtle">{endpoints.length} 个</span>
                        <Button
                          type="button"
                          size="sm"
                          variant="primary"
                          icon={<Plus className="h-3.5 w-3.5" />}
                          onClick={openAddEndpointModal}
                          className="flex items-center gap-1.5 px-2.5"
                        >
                          新增端点
                        </Button>
                      </div>
                    </div>
                    <MobileCardList>
                      {endpoints.map(renderEndpointCard)}
                    </MobileCardList>
                    <LayerCard className="hidden min-h-64 min-w-0 flex-1 flex-col overflow-hidden p-0 shadow-none cq-md:flex">
                      <div className="min-h-0 min-w-0 flex-1 overflow-auto overscroll-x-contain scrollbar-thin">
                        <AppTable tableId="endpoints-list" columns={ENDPOINT_LIST_COLUMNS} className="w-full min-w-[420px] text-xs">
                          <Table.Header sticky variant="compact">
                            <Table.Row className="h-8">
                              <Table.Head className="!px-2.5 !py-1.5">端点</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center" title="路由优先级（值越大越优先）">优先</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center" title="同优先级内的加权因子">权重</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">状态</Table.Head>
                            </Table.Row>
                          </Table.Header>
                          <Table.Body>
                            {endpoints.map(item => (
                              <Table.Row
                                key={item.id}
                                variant={item.id === endpoint.id ? 'selected' : 'default'}
                                className="h-11 cursor-pointer"
                                onClick={() => setSelectedEndpointId(item.id)}
                                onDoubleClick={() => openEditEndpointModal(item)}
                              >
                                <Table.Cell className="!px-2.5 !py-1.5">
                                  <div className="min-w-0 flex-1">
                                    <div className="flex items-center gap-1.5 min-w-0">
                                      <span
                                        className="inline-flex w-7 shrink-0 items-center justify-center rounded border-2 border-kumo-line/70 font-mono text-xs font-semibold leading-5 tabular-nums text-brand"
                                        title="启用模型数"
                                      >
                                        {activeModelIdsForEndpoint(item).length}
                                      </span>
                                      {item.pluginId && (
                                        <span
                                          className="shrink-0 rounded border-2 border-kumo-line/70 px-1 text-[10px] font-semibold leading-5 text-brand"
                                          title="由插件注册"
                                        >
                                          插件
                                        </span>
                                      )}
                                      <div className="truncate font-semibold leading-5 text-kumo-strong" title={item.name}>
                                        {item.name || '未命名端点'}
                                      </div>
                                    </div>
                                    <div className="truncate font-mono text-[10px] leading-4 text-kumo-subtle" title={item.baseUrl}>
                                      {item.baseUrl}
                                    </div>
                                  </div>
                                </Table.Cell>
                                <Table.Cell className="!px-1.5 !py-1.5 text-center">
                                {routingEditKey === `${item.id}:priority` ? (
                                  <Input
                                    autoFocus
                                    size="sm"
                                    type="number"
                                    min={0}
                                    max={999}
                                    aria-label="路由优先级"
                                    value={routingDraft}
                                    onChange={event => setRoutingDraft(event.target.value)}
                                    onKeyDown={event => {
                                      event.stopPropagation();
                                      if (event.key === 'Enter') {
                                        saveEndpointRouting(item.id, 'priority', Number(routingDraft) || 0);
                                      } else if (event.key === 'Escape') {
                                        setRoutingEditKey(null);
                                      }
                                    }}
                                    onBlur={() => {
                                      if (routingEditKey === `${item.id}:priority`) {
                                        saveEndpointRouting(item.id, 'priority', Number(routingDraft) || 0);
                                      }
                                    }}
                                    className="h-6 w-12 text-center font-mono text-[11px]"
                                  />
                                ) : (
                                  <span
                                    className="block cursor-text font-mono text-[11px] text-kumo-strong"
                                    title="双击编辑路由优先级（值越大越优先）"
                                    onDoubleClick={event => {
                                      event.stopPropagation();
                                      setRoutingDraft(String(item.priority ?? 0));
                                      setRoutingEditKey(`${item.id}:priority`);
                                    }}
                                  >
                                    {item.priority ?? 0}
                                  </span>
                                )}
                              </Table.Cell>
                              <Table.Cell className="!px-1.5 !py-1.5 text-center">
                                {routingEditKey === `${item.id}:weight` ? (
                                  <Input
                                    autoFocus
                                    size="sm"
                                    type="number"
                                    min={1}
                                    max={9999}
                                    aria-label="路由权重"
                                    value={routingDraft}
                                    onChange={event => setRoutingDraft(event.target.value)}
                                    onKeyDown={event => {
                                      event.stopPropagation();
                                      if (event.key === 'Enter') {
                                        saveEndpointRouting(item.id, 'weight', Number(routingDraft) || 1);
                                      } else if (event.key === 'Escape') {
                                        setRoutingEditKey(null);
                                      }
                                    }}
                                    onBlur={() => {
                                      if (routingEditKey === `${item.id}:weight`) {
                                        saveEndpointRouting(item.id, 'weight', Number(routingDraft) || 1);
                                      }
                                    }}
                                    className="h-6 w-12 text-center font-mono text-[11px]"
                                  />
                                ) : (
                                  <span
                                    className="block cursor-text font-mono text-[11px] text-kumo-strong"
                                    title="双击编辑加权因子（值越大被选中概率越高）"
                                    onDoubleClick={event => {
                                      event.stopPropagation();
                                      setRoutingDraft(String(item.weight ?? 100));
                                      setRoutingEditKey(`${item.id}:weight`);
                                    }}
                                  >
                                    {item.weight ?? 100}
                                  </span>
                                )}
                              </Table.Cell>
                                <Table.Cell className="!px-2 !py-1.5 text-center">
                                  <div
                                    className="flex justify-center"
                                    onClick={event => event.stopPropagation()}
                                  >
                                    <Switch
                                      size="sm"
                                      aria-label={item.enabled ? '停用端点' : '启用端点'}
                                      checked={item.enabled}
                                      onCheckedChange={() => toggleEndpointEnabled(item)}
                                      disabled={!!endpointToggleLoading[item.id]}
                                    />
                                  </div>
                                </Table.Cell>
                              </Table.Row>
                            ))}
                          </Table.Body>
                        </AppTable>
                      </div>
                    </LayerCard>
                  </section>

                  <section className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
                    <div className="flex min-h-8 flex-wrap items-center justify-between gap-2 px-1">
                      <div className="flex min-w-0 items-center gap-2 text-xs">
                        <span className="truncate font-medium text-kumo-strong">
                          {endpoint.name || '未命名端点'}
                        </span>
                        <Button
                          size="sm"
                          variant="secondary"
                          aria-label="复制端点地址"
                          title={endpoint.baseUrl}
                          onClick={() => copyTextHelper(endpoint.baseUrl, '端点地址')}
                        >
                          端点
                        </Button>
                        <Button
                          size="sm"
                          variant="secondary"
                          aria-label="复制 API Key"
                          title="复制 API Key（默认首个）"
                          onClick={() => {
                            const keys = [endpoint.apiKey, ...(endpoint.apiKeys || [])].filter(Boolean);
                            copyTextHelper(keys[0], keys.length > 1 ? '首个 API Key' : 'API Key');
                          }}
                        >
                          密钥
                        </Button>
                      </div>
                      <div className="flex shrink-0 flex-wrap items-center gap-1.5">
                        <StatusBadge
                          tone={validStatus ? 'success' : invalidStatus ? 'danger' : 'neutral'}
                        >
                          {validStatus ? '有效' : invalidStatus ? '无效' : '待检测'}
                        </StatusBadge>
                        {Array.isArray(endpoint.headers) && endpoint.headers.length > 0 && (
                          <StatusBadge
                            tone="info"
                            title={(endpoint.headers || [])
                              .map(h => `${h.name}: ${h.value}`)
                              .join('\n')}
                          >
                            {endpoint.headers.length} 请求头
                          </StatusBadge>
                        )}
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary"
                          aria-label="模型健康检测"
                          onClick={() => openHealthCheckForEndpoint(endpoint.id)}
                          disabled={modelHealthBatchLoading}
                          title="模型健康检测"
                          icon={
                            modelHealthBatchLoading ? (
                              <Loader size="sm" />
                            ) : (
                              <Activity className={actionIconClass} />
                            )
                          }
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary"
                          aria-label="刷新模型列表"
                          onClick={() => refreshEndpointModels(endpoint)}
                          loading={endpoint.refreshing}
                          title="刷新模型列表"
                          icon={<RefreshCw className={actionIconClass} />}
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary"
                          aria-label="手动添加模型"
                          title="手动添加模型（Vertex AI 等无法自动拉取模型列表的上游使用）"
                          onClick={() => {
                            setAddModelTarget(endpoint);
                            // 仅无自动列表的上游（vertex）预填手动添加的模型（其
                            // models 全部来自手动添加）；可自动拉取的上游（openai/
                            // gemini）不把自动获取的模型填进去，弹窗保持追加语义。
                            setAddModelText(
                              endpoint.upstreamType === 'vertex' && Array.isArray(endpoint.models)
                                ? endpoint.models.join('\n')
                                : ''
                            );
                            setAddModelOpen(true);
                          }}
                          icon={<Plus className={actionIconClass} />}
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary"
                          aria-label="编辑端点"
                          onClick={() => openEditEndpointModal(endpoint)}
                          title="编辑端点"
                        >
                          <Edit className={actionIconClass} />
                        </Button>
                        <Button
                          shape="square"
                          size="sm"
                          variant={
                            deleteEndpointConfirmActive(endpoint.id)
                              ? 'primary'
                              : 'secondary-destructive'
                          }
                          aria-label={
                            deleteEndpointConfirmActive(endpoint.id)
                              ? `再次点击确认删除 ${endpoint.name || endpoint.baseUrl}`
                              : `删除 ${endpoint.name || endpoint.baseUrl}`
                          }
                          onClick={() => deleteEndpoint(endpoint)}
                          disabled={!!endpoint.pluginId}
                          title={
                            endpoint.pluginId
                              ? '插件端点不可删除'
                              : deleteEndpointConfirmActive(endpoint.id)
                                ? '再次点击确认删除'
                                : '删除端点'
                          }
                        >
                          <Trash className={actionIconClass} />
                        </Button>
                      </div>
                    </div>

                    <div className="flex items-center justify-between gap-2 px-1 cq-md:hidden">
                      <span className="min-w-0 truncate text-xs leading-normal text-kumo-subtle">
                        {endpoint.models?.length || 0} 个模型 · 已启用 {activeModelIdsForEndpoint(endpoint).length}
                      </span>
                      <div className="flex shrink-0 items-center gap-1.5">
                        <span className="text-[11px] leading-normal text-kumo-subtle">
                          {disabledModelCount > 0 ? '启用全部' : '关闭非有效'}
                        </span>
                        {renderBatchSwitch()}
                      </div>
                    </div>
                    <MobileCardList>
                      {endpoint.models && endpoint.models.length > 0 ? (
                        endpoint.models.map(renderModelCard)
                      ) : (
                        <MobileCardEmpty>暂无模型数据,可刷新端点获取</MobileCardEmpty>
                      )}
                    </MobileCardList>
                    <LayerCard className="hidden min-h-64 min-w-0 flex-1 flex-col overflow-hidden p-0 shadow-none cq-md:flex">
                      <div className="min-h-0 min-w-0 flex-1 overflow-auto overscroll-x-contain scrollbar-thin">
                        <AppTable tableId="endpoint-models" columns={ENDPOINT_MODEL_COLUMNS} className="min-w-[820px] text-xs">
                          <Table.Header sticky variant="compact">
                            <Table.Row className="h-8">
                              <Table.Head className="!px-2 !py-1.5 text-center">
                                <div
                                  className="flex items-center justify-center"
                                  onClick={event => event.stopPropagation()}
                                  title={
                                    disabledModelCount > 0
                                      ? `启用 ${disabledModelCount} 个被停用的模型`
                                      : '关闭所有非有效模型（检测有效的保留）'
                                  }
                                >
                                  {renderBatchSwitch()}
                                </div>
                              </Table.Head>
                              <Table.Head className="!px-2.5 !py-1.5">模型</Table.Head>
                              <Table.Head className="!px-2 !py-1.5">映射</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">健康</Table.Head>
                              <Table.Head className="app-table-action !px-2 !py-1.5">操作</Table.Head>
                            </Table.Row>
                          </Table.Header>
                          <Table.Body>
                            {endpoint.models && endpoint.models.length > 0 ? (
                              endpoint.models.map(model => {
                                const modelId =
                                  typeof model === 'string'
                                    ? model.trim()
                                    : (model.id || '').trim();
                                const healthKey = modelHealthKey(endpoint.id, modelId);
                                const health = modelhubModelHealth[healthKey];
                                const canStopHealthCheck =
                                  health?.loading &&
                                  modelHealthAbortControllersRef.current.has(healthKey);
                                const healthCheckAnimating = !!health?.loading;
                                const healthTone = health?.loading
                                  ? 'info'
                                  : health?.status === 'healthy'
                                    ? 'success'
                                    : health?.status === 'degraded'
                                      ? 'warning'
                                      : health?.status === 'error'
                                        ? 'danger'
                                        : 'neutral';
                                const healthLabel = health?.loading
                                  ? '检测中'
                                  : health?.status === 'healthy'
                                    ? '可用'
                                    : health?.status === 'degraded'
                                      ? '较慢'
                                      : health?.status === 'error'
                                        ? '失败'
                                        : health?.status === 'cancelled'
                                          ? '已停止'
                                          : '未检测';

                                return (
                                  <Table.Row key={`${endpoint.id}:${modelId}`} className="h-9">
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      <div
                                        className="flex justify-center"
                                        onClick={event => event.stopPropagation()}
                                      >
                                        <Switch
                                          size="sm"
                                          aria-label={modelEnabledForEndpoint(endpoint, modelId) ? `停用 ${modelId}` : `启用 ${modelId}`}
                                          checked={modelEnabledForEndpoint(endpoint, modelId)}
                                          onCheckedChange={enabled =>
                                            toggleModelEnabled(endpoint, modelId, enabled)
                                          }
                                          disabled={!!modelSwitchLoading[`${endpoint.id}:${modelId}`]}
                                        />
                                      </div>
                                    </Table.Cell>
                                    <Table.Cell className="!px-2.5 !py-1.5">
                                      {(() => {
                                        const modelName = endpoint.modelNames?.[modelId];
                                        return (
                                          <>
                                            <span
                                              className="block truncate font-medium leading-5 text-kumo-strong"
                                              title={modelName ? `${modelName}（${modelId}）` : modelId}
                                            >
                                              {modelName || modelId}
                                            </span>
                                            {modelName ? (
                                              <span className="block truncate font-mono text-[0.85em] leading-4 text-kumo-subtle" title={modelId}>
                                                {modelId}
                                              </span>
                                            ) : null}
                                          </>
                                        );
                                      })()}
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5">
                                      {mappingEditKey === `${endpoint.id}:${modelId}` ? (
                                        <Input
                                          autoFocus
                                          size="sm"
                                          value={mappingDraft}
                                          aria-label="模型对外映射名称"
                                          onChange={event => setMappingDraft(event.target.value)}
                                          onKeyDown={event => {
                                            event.stopPropagation();
                                            if (event.key === 'Enter') {
                                              saveEndpointMapping(endpoint, modelId, mappingDraft);
                                            } else if (event.key === 'Escape') {
                                              setMappingEditKey(null);
                                            }
                                          }}
                                          onBlur={() => {
                                            if (mappingEditKey === `${endpoint.id}:${modelId}`) {
                                              saveEndpointMapping(endpoint, modelId, mappingDraft);
                                            }
                                          }}
                                          className="w-full font-mono text-[10px]"
                                          placeholder="对外名称"
                                        />
                                      ) : (
                                        <span
                                          className="block cursor-text truncate font-mono text-[10px]"
                                          title="双击编辑对外映射名称"
                                          onDoubleClick={event => {
                                            event.stopPropagation();
                                            setMappingDraft(endpoint.modelMappings?.[modelId] || '');
                                            setMappingEditKey(`${endpoint.id}:${modelId}`);
                                          }}
                                        >
                                          {endpoint.modelMappings?.[modelId] ? (
                                            <span className="text-base text-brand">
                                              {endpoint.modelMappings[modelId]}
                                            </span>
                                          ) : (
                                            <span className="text-kumo-subtle">双击设置</span>
                                          )}
                                        </span>
                                      )}
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      <div className="inline-flex items-center gap-2">
                                        {health?.status !== 'healthy' && health?.status !== 'degraded' && (
                                          <StatusBadge tone={healthTone}>
                                            {healthLabel}
                                          </StatusBadge>
                                        )}
                                        {(health?.status === 'healthy' || health?.status === 'degraded') &&
                                          health?.latency != null && (
                                            <span
                                              className={`font-mono font-semibold ${
                                                health?.status === 'healthy'
                                                  ? 'text-kumo-success'
                                                  : 'text-kumo-warning'
                                              }`}
                                            >
                                              {health.latency} ms
                                            </span>
                                          )}
                                      </div>
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      <div className="inline-flex gap-1">
                                        <Button
                                          shape="square"
                                          size="sm"
                                          variant={
                                            canStopHealthCheck
                                              ? 'secondary-destructive'
                                              : 'secondary'
                                          }
                                          aria-label={
                                            canStopHealthCheck
                                              ? `停止检测 ${modelId}`
                                              : `检测 ${modelId}`
                                          }
                                          onClick={() =>
                                            testModelHealth({ id: modelId }, endpoint.id)
                                          }
                                          disabled={modelHealthBatchLoading}
                                          title={
                                            health?.error ||
                                            (canStopHealthCheck
                                              ? '停止检测'
                                              : health?.loading
                                                ? '检测中'
                                                : '检测模型')
                                          }
                                          icon={
                                            healthCheckAnimating ? (
                                              <Loader size="sm" />
                                            ) : (
                                              <Activity className="h-3.5 w-3.5" />
                                            )
                                          }
                                        />
<Button
                                            shape="square"
                                            size="sm"
                                            variant="secondary"
                                            aria-label={`复制 ${modelId}`}
                                            onClick={() => {
                                              navigator.clipboard.writeText(modelId);
                                              toast.success('已复制模型名称');
                                            }}
                                            title="复制模型名称"
                                            icon={<Copy className="h-3.5 w-3.5" />}
                                          />
                                        </div>
                                      </Table.Cell>
                                    </Table.Row>
                                );
                              })
                            ) : (
                              <Table.Row>
                                <Table.Cell
                                  colSpan={5}
                                  className="py-10 text-center text-kumo-subtle"
                                >
                                  暂无模型数据，可刷新端点获取
                                </Table.Cell>
                              </Table.Row>
                            )}
                          </Table.Body>
                        </AppTable>
                      </div>
                    </LayerCard>
                  </section>
                </div>
              );
            })()
          )}
        </div>
  );
}
