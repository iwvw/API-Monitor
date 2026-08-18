package adminai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// ==================== 工具调用教训自动沉淀 ====================
// 目标：AI 在一次执行中「先失败、后修正成功」的接口调用经验，无需用户开口、
// 也无需等空闲提炼（10 分钟 + LLM 判断），在 run 收尾时确定性沉淀为长期记忆，
// 下次同类调用直接按教训采用正确参数，避免反复试错。

const (
	lessonSource       = "lesson"
	lessonImportance   = 6
	maxLessonsPerRun   = 2
	lessonMaxErrChars  = 100
	lessonMaxArgsChars = 120
)

type lessonCall struct {
	toolName string
	args     map[string]interface{}
	errText  string
}

// toolLessonTracker 收集单次执行内的失败与成功调用（仅 call_api / get_route 等
// 契约相关工具，纯内部工具不沉淀）。
type toolLessonTracker struct {
	failures  []lessonCall
	successes []lessonCall
}

func (t *toolLessonTracker) record(toolName string, args map[string]interface{}, errText string, success bool) {
	if !lessonRelevantTool(toolName) {
		return
	}
	c := lessonCall{toolName: toolName, args: args, errText: errText}
	if success {
		t.successes = append(t.successes, c)
	} else {
		t.failures = append(t.failures, c)
	}
}

func lessonRelevantTool(toolName string) bool {
	switch toolName {
	case "call_api", "get_route":
		return true
	}
	return false
}

// callKey 返回调用指纹：call_api 按 path（+方法），get_route 按 path。
// 失败的修正成功 = 同 key 下先败后成（参数差异即修正内容）。
func callKey(c lessonCall) string {
	path, _ := c.args["path"].(string)
	if path == "" {
		path = "?"
	}
	method, _ := c.args["method"].(string)
	if c.toolName == "call_api" && method != "" {
		return c.toolName + "|" + strings.ToUpper(method) + " " + path
	}
	return c.toolName + "|" + path
}

// captureToolLessons run 收尾调用：失败→修正成功 模式沉淀为长期记忆。
// 准入：同一指纹先失败后成功；错误与成功参数有实际差异；去重后插入。
// 另含「路径修正」配对：猜错路径（404/不存在）后改用清单内真实子路由成功，
// 同样沉淀（否则「别猜 /api/scheduler」这类教训永远不会进入长期记忆）。
func (s *Service) captureToolLessons(ctx context.Context, db *sql.DB, sessionID string, t *toolLessonTracker) {
	if t == nil || len(t.failures) == 0 || len(t.successes) == 0 {
		return
	}
	if !s.getBoolSetting(ctx, adminAIKeyMemoriesEnabled, true) {
		return
	}
	// 失败→成功配对（同指纹）
	paired := map[string]lessonCall{}
	for _, f := range t.failures {
		key := callKey(f)
		if _, done := paired[key]; done {
			continue
		}
		for _, ok := range t.successes {
			if callKey(ok) != key {
				continue
			}
			if argsEqual(f.args, ok.args) {
				continue // 参数相同：非修正场景（如瞬时网络错误），不沉淀
			}
			if lessonText, diff := buildLessonText(f, ok); lessonText != "" {
				paired[key] = ok
				_ = diff
				s.persistLesson(ctx, db, sessionID, lessonText)
				if len(paired) >= maxLessonsPerRun {
					return
				}
			}
			break
		}
	}
	// 路径修正配对：失败错误表明「路径不存在/不可用」（404 类），本执行内
	// 成功调用过以失败路径为前缀的具体子路由 → 沉淀「应改用子路由」。
	// 只适用于 call_api 的路径猜测（get_route 的契约查询失败由错误提示引导即可）。
	for _, f := range t.failures {
		if f.toolName != "call_api" {
			continue
		}
		key := callKey(f)
		if _, done := paired[key]; done {
			continue
		}
		if !lessonPathUnavailable(f.errText) {
			continue
		}
		fPath := lessonCallPath(f)
		if fPath == "" {
			continue
		}
		for _, ok := range t.successes {
			if ok.toolName != "call_api" {
				continue
			}
			okPath := lessonCallPath(ok)
			if okPath == "" || okPath == fPath || !strings.HasPrefix(okPath, fPath+"/") {
				continue
			}
			errKey := strings.Join(strings.Fields(f.errText), " ")
			if runes := []rune(errKey); len(runes) > lessonMaxErrChars {
				errKey = string(runes[:lessonMaxErrChars])
			}
			text := fmt.Sprintf("工具教训：调用 %s %s 失败（%s）：该路径不存在或不可直接调用；应改用清单内的 %s。后续调用该接口直接使用真实子路径，勿再猜测聚合路径。",
				strings.ToUpper(lessonCallMethod(f)), fPath, errKey, okPath)
			if runes := []rune(text); len(runes) > 500 {
				text = string(runes[:500])
			}
			paired[key] = ok
			s.persistLesson(ctx, db, sessionID, text)
			if len(paired) >= maxLessonsPerRun {
				return
			}
			break
		}
	}
}

// lessonPathUnavailable 判断失败文本是否为「路径不存在/不可调用」类错误。
func lessonPathUnavailable(errText string) bool {
	lower := strings.ToLower(errText)
	for _, kw := range []string{"不存在", "未能命中", "not implemented", "not found", "http 404", "api 路由不存在", "未命中可调用接口"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// lessonCallPath 取调用参数里的 path（可能是模板或真实路径）。
func lessonCallPath(c lessonCall) string {
	if c.args == nil {
		return ""
	}
	p, _ := c.args["path"].(string)
	return p
}

// lessonCallMethod 取调用方法（call_api 无 method 时按 GET 计）。
func lessonCallMethod(c lessonCall) string {
	if c.args == nil {
		return "GET"
	}
	m, _ := c.args["method"].(string)
	if m == "" {
		return "GET"
	}
	return m
}

// buildLessonText 生成经验文本：错误要点 + 修正后的关键参数。
func buildLessonText(f, ok lessonCall) (string, string) {
	errKey := strings.Join(strings.Fields(f.errText), " ")
	if runes := []rune(errKey); len(runes) > lessonMaxErrChars {
		errKey = string(runes[:lessonMaxErrChars])
	}
	if errKey == "" {
		return "", ""
	}
	diff := argsDiff(f.args, ok.args)
	if diff == "" {
		return "", ""
	}
	label := "调用"
	if path, _ := f.args["path"].(string); path != "" {
		method, _ := f.args["method"].(string)
		if method == "" {
			method = "GET"
		}
		label = fmt.Sprintf("调用 %s %s", strings.ToUpper(method), path)
	}
	text := fmt.Sprintf("工具教训：%s 失败（%s）；修正为 %s 后成功。后续调用该接口直接使用修正后的参数，勿重复试错。",
		label, errKey, diff)
	if runes := []rune(text); len(runes) > 500 {
		text = string(runes[:500])
	}
	return text, diff
}

// persistLesson 落库（带查重：同路径 + 同错误要点已存在则跳过）。
func (s *Service) persistLesson(ctx context.Context, db *sql.DB, sessionID, text string) {
	errKey := errorFingerprint(text)
	path := lessonPathOf(text)
	var dupID string
	if path != "" && errKey != "" {
		_ = db.QueryRowContext(ctx,
			`SELECT id FROM admin_ai_memories WHERE content LIKE ? AND content LIKE ? LIMIT 1`,
			"%"+path+"%", "%"+errKey+"%").Scan(&dupID)
	}
	if dupID != "" {
		slog.Debug("lesson-skip-dup", "path", path)
		return
	}
	triggers := path
	if triggers == "" {
		triggers = "接口调用"
	}
	if _, err := s.insertMemory(ctx, db, text, lessonImportance, triggers, false, lessonSource, sessionID); err != nil {
		slog.Warn("lesson-persist", "err", err.Error())
		return
	}
	slog.Info("lesson-persisted", "session", sessionID, "path", path)
}

func lessonPathOf(text string) string {
	i := strings.Index(text, "失败")
	if i < 0 {
		return ""
	}
	head := text[:i]
	j := strings.Index(head, "http")
	if j >= 0 {
		head = head[j:]
	}
	fields := strings.Fields(head)
	if len(fields) == 0 {
		return ""
	}
	last := fields[len(fields)-1]
	if strings.HasPrefix(last, "/") {
		return strings.Trim(last, ",。；;）)")
	}
	return last
}

func errorFingerprint(text string) string {
	i := strings.Index(text, "（")
	j := strings.Index(text, "）")
	if i >= 0 && j > i && j-i <= 60+len("（") {
		return text[i+len("（") : j]
	}
	return ""
}

func argsEqual(a, b map[string]interface{}) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

// argsDiff 提取失败与成功参数间的差异字段（按 key 排序，值截断）。
func argsDiff(fail, ok map[string]interface{}) string {
	var parts []string
	keys := make(map[string]bool, len(fail)+len(ok))
	for k := range fail {
		keys[k] = true
	}
	for k := range ok {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		fv, fHas := fail[k]
		ov, oHas := ok[k]
		if fHas && oHas && fmt.Sprint(fv) == fmt.Sprint(ov) {
			continue
		}
		val := "（移除）"
		if oHas {
			raw, _ := json.Marshal(ov)
			val = string(raw)
			if runes := []rune(val); len(runes) > lessonMaxArgsChars {
				val = string(runes[:lessonMaxArgsChars]) + "…"
			}
		}
		parts = append(parts, k+"="+val)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}
