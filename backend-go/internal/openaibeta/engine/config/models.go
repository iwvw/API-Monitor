package config

import (
	"strings"
	"sync"
)

// ModelEntry 是模型注册表中的一条记录。嵌入版由 openaibeta.Service 从 DB
// 持久化的模型开关/别名同步到本包的内存 store，避免原版 models.json 文件 IO。
type ModelEntry struct {
	ID                 string `json:"id"`
	Enabled            bool   `json:"enabled"`
	TrailingFixEnabled bool   `json:"trailing_fix_enabled"`
}

// defaultModelRegistry 是模型 ID 和缺省能力的唯一内置来源（与上游保持一致）。
//
//nolint:gochecknoglobals // Read-only default registry
var defaultModelRegistry = []ModelEntry{
	{ID: "gemini-2.5-flash", Enabled: true},
	{ID: "gemini-2.5-flash-lite", Enabled: true},
	{ID: "gemini-2.5-flash-image", Enabled: true},
	{ID: "gemini-2.5-pro", Enabled: true},
	{ID: "gemini-3-flash-preview", Enabled: true},
	{ID: "gemini-3-pro-image", Enabled: true},
	{ID: "gemini-3.1-flash-lite", Enabled: true},
	{ID: "gemini-3.1-flash-lite-image", Enabled: true},
	{ID: "gemini-3.1-flash-image", Enabled: true},
	{ID: "gemini-3.1-flash-tts-preview", Enabled: true},
	{ID: "gemini-3.1-pro-preview", Enabled: true},
	{ID: "gemini-3.5-flash", Enabled: true},
	{ID: "gemini-3.5-flash-lite", Enabled: true, TrailingFixEnabled: true},
	{ID: "gemini-3.6-flash", Enabled: true, TrailingFixEnabled: true},
	{ID: "imagen-3.0-capability", Enabled: true},
	{ID: "imagen-4.0-generate-001", Enabled: true},
	{ID: "imagen-4.0-ultra-generate-001", Enabled: true},
	{ID: "imagen-4.0-fast-generate-001", Enabled: true},
	{ID: "virtual-try-on-001", Enabled: true},
	{ID: "lyria-002", Enabled: true},
	{ID: "veo-2-generate-001", Enabled: true},
	{ID: "veo-3-generate-001", Enabled: true},
	{ID: "veo-3-fast-generate-001", Enabled: true},
}

// 内存模型 store：由 Service 通过 SetModelStore 同步；未设置时回落到内置注册表。
var (
	storeMu    sync.RWMutex
	storeModel []ModelEntry
	storeAlias map[string]string
)

// SetModelStore 以调用方提供的模型注册表与别名映射覆盖内存 store。
func SetModelStore(models []ModelEntry, aliasMap map[string]string) {
	storeMu.Lock()
	defer storeMu.Unlock()
	storeModel = cloneModelEntries(models)
	if aliasMap == nil {
		storeAlias = map[string]string{}
	} else {
		storeAlias = make(map[string]string, len(aliasMap))
		for k, v := range aliasMap {
			storeAlias[k] = v
		}
	}
}

func cloneModelEntries(in []ModelEntry) []ModelEntry {
	out := make([]ModelEntry, len(in))
	copy(out, in)
	return out
}

func DefaultModelRegistry() []ModelEntry { return cloneModelEntries(defaultModelRegistry) }

func DefaultModelEntry(id string) ModelEntry {
	for _, entry := range defaultModelRegistry {
		if entry.ID == id {
			return entry
		}
	}
	return ModelEntry{ID: id, Enabled: true}
}

func currentStore() ([]ModelEntry, map[string]string) {
	storeMu.RLock()
	defer storeMu.RUnlock()
	if storeModel == nil {
		return cloneModelEntries(defaultModelRegistry), map[string]string{}
	}
	return cloneModelEntries(storeModel), copyAlias(storeAlias)
}

func copyAlias(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ModelRegistry 返回全部模型（包含前端禁用项）。
func ModelRegistry() []ModelEntry {
	models, _ := currentStore()
	return models
}

// BaseModels 只返回启用的基础模型。
func BaseModels() []string {
	models, _ := currentStore()
	out := make([]string, 0, len(models))
	for _, entry := range models {
		if entry.Enabled {
			out = append(out, entry.ID)
		}
	}
	return out
}

func AliasMap() map[string]string {
	_, alias := currentStore()
	return alias
}

func LookupModel(model string) (ModelEntry, bool) {
	models, _ := currentStore()
	for _, entry := range models {
		if entry.ID == model {
			return entry, true
		}
	}
	return ModelEntry{}, false
}

func ResolveModelName(model string) string {
	_, alias := currentStore()
	if real, ok := alias[model]; ok {
		return real
	}
	return strings.TrimSpace(model)
}
