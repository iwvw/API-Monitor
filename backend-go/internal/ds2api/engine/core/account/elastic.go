package account

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// DefaultElasticPoolGlobalCount 是弹性号池全局模式下每批启用账号数的默认值。
const DefaultElasticPoolGlobalCount = 3

// ReconcileElasticPool 根据弹性号池配置重算每个账号的 Disabled 状态。
//
// 开启时按 config.Accounts 原始顺序遍历：被封(muted)的账号一律禁用且不占名额，
// 其余未禁言账号按顺序启用前 N 个，超出 N 的禁用(休眠)。
// PerPool=false 时所有账号共用 GlobalCount；PerPool=true 时 default/no_tools/
// tools_only 三种号池类型分别使用各自的 Count。
//
// 调用方需在 Store.Update 的 mutator 内调用，随后执行 Pool.Reset()。
// 若弹性号池未开启则不做任何操作。
func ReconcileElasticPool(cfg *config.Config) {
	if cfg == nil || !cfg.ElasticPool.Enabled {
		return
	}
	ep := cfg.ElasticPool
	if !ep.PerPool {
		count := ep.GlobalCount
		if count < 0 {
			count = 0
		}
		reconcileGroupFiltered(cfg.Accounts, config.PoolTypeDefault, count)
		reconcileGroupFiltered(cfg.Accounts, config.PoolTypeNoTools, count)
		reconcileGroupFiltered(cfg.Accounts, config.PoolTypeToolsOnly, count)
		return
	}
	reconcileGroupFiltered(cfg.Accounts, config.PoolTypeDefault, effectivePoolCount(ep.DefaultCount))
	reconcileGroupFiltered(cfg.Accounts, config.PoolTypeNoTools, effectivePoolCount(ep.NoToolsCount))
	reconcileGroupFiltered(cfg.Accounts, config.PoolTypeToolsOnly, effectivePoolCount(ep.ToolsOnlyCount))
}

// DisableAllAccounts 将所有账号设为启用(Disabled=false)。
// 用于关闭弹性号池时恢复全部账号可用。
func DisableAllAccounts(cfg *config.Config) {
	if cfg == nil {
		return
	}
	for i := range cfg.Accounts {
		cfg.Accounts[i].Disabled = false
	}
}

func effectivePoolCount(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// reconcileGroupFiltered 在 accounts 中按原始顺序对属于指定 poolType 的账号执行弹性启用，
// 不属于该 poolType 的账号保持原状。
func reconcileGroupFiltered(accounts []config.Account, poolType string, count int) {
	enabled := 0
	for i := range accounts {
		if config.NormalizePoolType(accounts[i].PoolType) != poolType {
			continue
		}
		applyElasticState(&accounts[i], &enabled, count)
	}
}

// applyElasticState 对单个账号应用弹性号池规则：
//   - 被封(muted) -> Disabled=true，不计数
//   - 已启用数 < count -> Disabled=false，计数+1
//   - 否则 -> Disabled=true
func applyElasticState(acc *config.Account, enabled *int, count int) {
	if acc.IsMuted() {
		acc.Disabled = true
		return
	}
	if *enabled < count {
		acc.Disabled = false
		*enabled++
		return
	}
	acc.Disabled = true
}
