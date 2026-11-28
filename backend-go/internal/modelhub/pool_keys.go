package modelhub

import (
	"math/rand"
	"strings"
	"time"
)

// Key 命中策略枚举（持久化为字符串）。
const (
	keyStrategyRoundRobin     = "round_robin"
	keyStrategyRandom         = "random"
	keyStrategyRandomSurvivor = "random_survivor"
)

// defaultKeyCooldownSeconds 是端点「回避冷却」缺省秒数（前端缺省 0 = 关闭，仍以配置为准）。
const defaultKeyCooldownSeconds = 0

// normalizeKeyStrategy 把任意字符串规整为合法策略：未知/空回退顺序轮询。
func normalizeKeyStrategy(v string) string {
	switch v {
	case keyStrategyRandom, keyStrategyRandomSurvivor:
		return v
	default:
		return keyStrategyRoundRobin
	}
}

// normalizeKeyCooldownSeconds 规整回避冷却秒数：负值按 0（关闭）处理。
func normalizeKeyCooldownSeconds(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

// pickKey 按端点配置的命中策略（+可选回避冷却）从全部 key 中选一把，返回 (key, index)。
// 通用约束（与策略无关）：
//   - key 不因健康状态被永久冻结；只是单请求内每条 key 最多尝试 maxTries 次
//     （triedKeys 为本请求内失败计数，达到 maxTries 即被跳过）。
//   - 仅当「回避冷却」开启（KeyCooldownSeconds>0）时，近期 401/403/429 的 key
//     会在冷却期内被降权/跳过；全部候选都冷却时回退为全候选，避免整端点不可用。
//   - 全部 key 均达到单请求上限时返回 ("", -1)，由调用方触发端点级切换。
//
// 策略差异只体现在「候选内的选择顺序」：
//   - round_robin：持久游标顺序轮询。
//   - random：完全随机等概率。
//   - random_survivor：错误时随机抽剩余——本轮请求内尚未失败(存活)的 key 中随机。
func (s *Service) pickKey(ep Endpoint, keys []string, triedKeys map[string]int, maxTries int) (string, int) {
	cleaned := cleanKeyList(keys)
	if len(cleaned) == 0 {
		return "", -1
	}
	if maxTries < 1 {
		maxTries = 1
	}
	strategy := normalizeKeyStrategy(ep.KeyStrategy)
	cooldown := time.Duration(normalizeKeyCooldownSeconds(ep.KeyCooldownSeconds)) * time.Second

	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	state, ok := s.keyStateByEndpoint[ep.ID]
	if !ok {
		state = newEndpointKeyState()
		s.keyStateByEndpoint[ep.ID] = state
	}

	// 单请求内未耗尽(尝试次数 < maxTries)的 key 构成候选。
	candidates := make([]int, 0, len(cleaned))
	for i := range cleaned {
		if triedKeys == nil || triedKeys[cleaned[i]] < maxTries {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return "", -1
	}

	// 冷却判定：关闭时恒 false。
	isCooled := func(i int) bool {
		if cooldown <= 0 {
			return false
		}
		entry := state.health[cleaned[i]]
		if entry == nil || entry.cooldownUntil.IsZero() {
			return false
		}
		return time.Now().Before(entry.cooldownUntil)
	}

	// 优先在未冷却候选中选择；全部冷却时回退为全部候选（保留端点可用性）。
	pool := candidates
	notCooled := make([]int, 0, len(candidates))
	for _, i := range candidates {
		if !isCooled(i) {
			notCooled = append(notCooled, i)
		}
	}
	if len(notCooled) > 0 {
		pool = notCooled
	}

	switch strategy {
	case keyStrategyRandom:
		chosen := pool[rand.Intn(len(pool))]
		return cleaned[chosen], chosen
	case keyStrategyRandomSurvivor:
		// 错误时随机抽剩余：优先在本轮请求内尚未失败的 key 中随机；全失败则退避为全候选。
		survivors := make([]int, 0, len(pool))
		for _, i := range pool {
			if triedKeys == nil || triedKeys[cleaned[i]] == 0 {
				survivors = append(survivors, i)
			}
		}
		if len(survivors) == 0 {
			survivors = pool
		}
		chosen := survivors[rand.Intn(len(survivors))]
		return cleaned[chosen], chosen
	default: // round_robin
		if state.cursor < 0 || state.cursor >= len(cleaned) {
			state.cursor = 0
		}
		start := state.cursor
		for offset := 0; offset < len(cleaned); offset++ {
			idx := (start + offset) % len(cleaned)
			inPool := false
			for _, p := range pool {
				if p == idx {
					inPool = true
					break
				}
			}
			if inPool {
				state.cursor = (idx + 1) % len(cleaned)
				return cleaned[idx], idx
			}
		}
		return "", -1
	}
}

// cleanKeyList 清洗并去重 API Key 列表（保留顺序，剔除空串）。
func cleanKeyList(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}
