package workbuddy

// 本文件是「上游 usage 与出网字节」的诊断面。
//
// 为什么需要它：这个插件把上游 usage 归一化后透传给网关，而网关是用**字面量正则**
// 从字节流里捞令牌数的。一旦两边统计不一致（插件有值、网关为 0），唯一能定位的办法就是
// 看「我们究竟往网关写了什么字节」。诊断把两样东西都留痕：
//   - 上游**原始** usage 的字段名与 JSON 类型（判断是不是命名/类型不匹配）；
//   - 归一化后**实际发出**的 usage（判断正则到底能不能命中）。
//
// 仅供排障：不落任何账号凭据、不含对话内容，只存 usage 里的数值字段。

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// captureEmittedUsage 记录归一化后实际写往下游的 usage（取该 chunk 的 usage 子对象）。
// 传**已清洗归一**的 chunk，因为那才是网关真正收到的内容。
func (s *Service) captureEmittedUsage(cleanedChunk string) {
	if !strings.Contains(cleanedChunk, `"usage"`) {
		return
	}
	var obj map[string]any
	if json.Unmarshal([]byte(cleanedChunk), &obj) != nil {
		return
	}
	usage, ok := obj["usage"].(map[string]any)
	if !ok || len(usage) == 0 {
		return
	}
	raw, err := json.Marshal(usage)
	if err != nil {
		return
	}
	s.usageMu.Lock()
	s.emittedUsage = truncate(string(raw), 512)
	s.emittedUsageAt = time.Now().UTC().Format(time.RFC3339)
	s.usageMu.Unlock()
}

// diagnosticsSnapshot 汇总当前诊断数据。
func (s *Service) diagnosticsSnapshot() map[string]interface{} {
	// 限流面用**另一把锁**：先取完再进 usageMu，避免锁序问题。
	rateSample, rateSampleAt, limits := s.rateLimitDiag()
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	return map[string]interface{}{
		// 模型级限流（账号 × 模型）：原文留痕 + 当前生效记录
		// （「某模型没反应/一直失败」时先看这里，能直接看到恢复时刻）。
		"rateLimitSample":   rateSample,
		"rateLimitSampleAt": rateSampleAt,
		"modelLimits":       limits,
		// 上游原始 usage：字段名 + 类型 + 原始样例
		"upstreamKeys":   s.usageKeys,
		"upstreamTypes":  s.usageTypes,
		"upstreamSample": s.usageSample,
		"cacheReported":  s.usageReportsCache,
		"upstreamAt":     s.usageAt,
		// 归一化后实际发给网关的 usage
		"emittedUsage":   s.emittedUsage,
		"emittedUsageAt": s.emittedUsageAt,
	}
}

// persistDiagnostics 把诊断快照写进 workbuddy_diagnostics（单行）。
// 由每分钟的 ticker 调用，避免每请求写库。
func (s *Service) persistDiagnostics(ctx context.Context) {
	snap := s.diagnosticsSnapshot()
	if snap["upstreamAt"] == "" && snap["emittedUsageAt"] == "" {
		return
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return
	}
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.ExecContext(ctx, `
		INSERT INTO workbuddy_diagnostics (id, data, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		string(raw), time.Now().UTC().Format(time.RFC3339))
}

// diagDoc 是 /_diag 的返回体：内存里的实时快照，叠加落库的最后一份（进程重启也能看到）。
type diagDoc struct {
	Live      map[string]interface{} `json:"live"`
	Persisted map[string]interface{} `json:"persisted,omitempty"`
	Note      string                 `json:"note"`
}

// handleDiag 处理 GET /api/workbuddy/v1/_diag。
//
// 挂在**中继前缀**下（manifest 里是 AuthInternal）：只有本机回环能访问、无需凭据、
// 不新增鉴权面，正好用于排障。只读，不产生上游请求。
func (s *Service) handleDiag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "只支持 GET", "invalid_request_error")
		return
	}
	doc := diagDoc{
		Live: s.diagnosticsSnapshot(),
		Note: "upstream* 是上游原始 usage；emittedUsage 是归一化后实际写给下游的 usage" +
			"（网关的正则只认不含引号的 \"cached_tokens\":<整数>）；" +
			"modelLimits 是当前生效的「账号 × 模型」限流（含恢复时刻），rateLimitSample 是最近一次的上游原文。",
	}
	if db, err := s.open(r.Context()); err == nil {
		var raw string
		if db.QueryRowContext(r.Context(), `SELECT data FROM workbuddy_diagnostics WHERE id = 1`).Scan(&raw) == nil && raw != "" {
			var persisted map[string]interface{}
			if json.Unmarshal([]byte(raw), &persisted) == nil {
				doc.Persisted = persisted
			}
		}
		db.Close()
	}
	responseJSON(w, doc)
}
