import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppCard } from '../../components/ui/AppPrimitives.jsx';
import { AlertTriangle, RotateCw, CheckDouble, Edit, Trash } from '../../components/Icons.jsx';
import { getSourceModuleName, getEventTypeName } from './constants.js';

export function RulesPanel({
  isArmed,
  notificationLoading,
  notificationRules,
  filteredRules,
  notificationRuleFilter,
  setNotificationRuleFilter,
  ruleFilterItems,
  highlightRuleId,
  dryRunResults,
  dryRunLoadingId,
  loadNotificationRules,
  handleOpenAddRule,
  handleDryRunRule,
  handleOpenEditRule,
  handleDeleteRule,
  handleToggleRuleEnabled,
}) {
  return (
    <div className="space-y-4">
      {/* 筛选控制栏 */}
      <div className="flex items-center justify-between pb-2 select-none">
        <Select alignItemWithTrigger
          aria-label="告警规则模块筛选" size="sm"
          value={notificationRuleFilter}
          onValueChange={setNotificationRuleFilter}
          placeholder="所有模块"
          items={ruleFilterItems}
        />

        <Button
          onClick={loadNotificationRules}
          loading={notificationLoading}
          variant="secondary" size="sm"
          shape="square"
          aria-label="刷新告警规则"
          className="text-kumo-subtle hover:text-kumo-strong"
          title="刷新"
          icon={<RotateCw className="w-3.5 h-3.5" />}
        />
      </div>

      {notificationLoading && notificationRules.length === 0 ? (
        <div className="grid grid-cols-1 cq-md:grid-cols-2 cq-lg:grid-cols-3 gap-4">
          {[...Array(3)].map((_, i) => (
            <AppCard key={i} padding="none" className="space-y-4 p-4">
              <div className="flex items-start justify-between gap-3">
                <SkeletonLine className="w-8 h-8 rounded-lg" />
                <div className="flex-1 space-y-1.5">
                  <SkeletonLine className="w-2/3 h-3.5" />
                  <SkeletonLine className="w-1/3 h-2.5" />
                </div>
              </div>
              <SkeletonLine className="w-full h-1" />
            </AppCard>
          ))}
        </div>
      ) : filteredRules.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
          <AlertTriangle className="w-12 h-12 opacity-30 mb-4" />
          <div className="text-sm">暂无匹配的告警规则</div>
          <Button size="sm" variant="primary" className="mt-4" onClick={handleOpenAddRule}>
            创建告警规则
          </Button>
        </div>
      ) : (
        <div className="grid grid-cols-1 cq-md:grid-cols-2 cq-lg:grid-cols-3 gap-4">
          {filteredRules.map((rule) => (
            <AppCard
              key={rule.id}
              padding="none"
              interactive
              className={`flex min-h-[148px] flex-col justify-between p-4 hover:border-brand/50 ${
                highlightRuleId === rule.id ? 'border-brand/60 ring-2 ring-brand/20' : ''
              }`}
            >
              <div className="flex items-start justify-between gap-3">
                {/* Severity Indicator */}
                <div className={`w-8 h-8 rounded-lg flex items-center justify-center text-kumo-inverse text-base flex-shrink-0 shadow-xs ${
                  rule.severity === 'critical'
                    ? 'bg-kumo-danger'
                    : rule.severity === 'warning'
                    ? 'bg-kumo-warning'
                    : 'bg-kumo-info'
                }`}>
                  <AlertTriangle className="w-4 h-4" />
                </div>

                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5 flex-wrap">
                    <span className="text-xs font-semibold text-kumo-strong truncate leading-tight" title={rule.name}>
                      {rule.name}
                    </span>
                    <span className={`text-[8.5px] px-1.5 py-0.5 rounded font-semibold uppercase select-none ${
                      rule.severity === 'critical'
                        ? 'bg-kumo-danger/10 text-kumo-danger border border-kumo-danger/20'
                        : rule.severity === 'warning'
                        ? 'bg-kumo-warning/10 text-kumo-warning border border-kumo-warning/20'
                        : 'bg-kumo-info/10 text-kumo-info border border-kumo-info/20'
                    }`}>
                      {rule.severity}
                    </span>
                  </div>
                  <div className="flex items-center gap-1.5 mt-2 flex-wrap select-none">
                    <Badge className="bg-kumo-recessed text-[9px] font-medium text-kumo-subtle border border-kumo-line/40">
                      {getSourceModuleName(rule.source_module)}
                    </Badge>
                    <Badge className="bg-kumo-recessed text-[9px] font-medium text-kumo-subtle border border-kumo-line/40">
                      {getEventTypeName(rule.event_type)}
                    </Badge>
                  </div>
                </div>

                {/* Actions */}
                <div className="flex items-center gap-1.5 flex-shrink-0">
                  <Button
                    onClick={() => handleDryRunRule(rule)}
                    variant="secondary" size="sm"
                    shape="square"
                    aria-label="预演告警规则"
                    title="Dry-run"
                    loading={dryRunLoadingId === rule.id}
                    icon={<CheckDouble className="w-3.5 h-3.5" />}
                  />
                  <Button
                    onClick={() => handleOpenEditRule(rule)}
                    variant="secondary" size="sm"
                    shape="square"
                    aria-label="编辑告警规则"
                    title="编辑"
                    icon={<Edit className="w-3.5 h-3.5" />}
                  />
                  <Button
                    onClick={() => handleDeleteRule(rule.id)}
                    variant={isArmed(`rule:${rule.id}`) ? 'destructive' : 'secondary-destructive'} size="sm"
                    shape="square"
                    aria-label="删除告警规则"
                    title="删除"
                    icon={<Trash className="w-3.5 h-3.5" />}
                  />
                </div>
              </div>

              {/* Settings status summary */}
              <div className="border-t border-kumo-line/60 pt-3 mt-3.5 space-y-2">
                {rule.suppression && (
                  <div className="flex items-center gap-1.5 text-[10px] text-kumo-subtle font-mono select-none">
                    <span>• 重复抑制: {rule.suppression.repeat_count || 1} 次</span>
                    <span className="text-kumo-subtle/30">/</span>
                    <span>静默期: {rule.suppression.silence_minutes || 0} 分钟</span>
                  </div>
                )}
                <div className="flex items-center justify-between">
                  <span className={`text-[10px] font-semibold flex items-center gap-1.5 ${
                    rule.enabled ? 'text-kumo-success' : 'text-kumo-subtle'
                  }`}>
                    <span className={`w-1.5 h-1.5 rounded-full ${rule.enabled ? 'bg-kumo-success animate-pulse' : 'bg-kumo-subtle'}`} />
                    {rule.enabled ? '已开启规则' : '已禁用规则'}
                  </span>
                  <Button
                    onClick={() => handleToggleRuleEnabled(rule)}
                    variant="secondary" size="sm"
                    className="h-6 text-[10px] font-semibold px-2"
                  >
                    {rule.enabled ? '一键禁用' : '一键启用'}
                  </Button>
                </div>
                {dryRunResults[rule.id] && (
                  <div className="rounded-md border border-kumo-line/60 bg-kumo-recessed/40 p-2.5 text-[10px] leading-relaxed text-kumo-subtle space-y-1">
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-semibold text-kumo-strong">
                        {dryRunResults[rule.id].wouldNotify ? '✅ 预演结果：触发发送' : 'ℹ️ 预演结果：未触发'}
                      </span>
                      <span className="font-mono text-[9px] text-kumo-subtle">{dryRunResults[rule.id].fingerprint}</span>
                    </div>
                    <div className="truncate font-mono text-[10px] text-kumo-subtle">{dryRunResults[rule.id].title}</div>
                  </div>
                )}
              </div>
            </AppCard>
          ))}
        </div>
      )}
    </div>
  );
}
