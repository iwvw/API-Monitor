package adminai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// ErrMemoryNotFound 是记忆条目不存在时的哨兵错误（updateMemory/引用记忆更新共用）。
var ErrMemoryNotFound = errors.New("记忆条目不存在")

// MemoryItem 是长期记忆条目（跨会话持久事实/用户偏好/历史决策）。
// 检索与排序参考 OpenClaw memory-core：bm25 基础分 × importance 乘数 × recency 半衰期衰减 × pinned 加成，
// 全文索引采用 FTS5 trigram（面向中文子串检索）。
type MemoryItem struct {
	ID             string `json:"id"`
	Content        string `json:"content"`
	Importance     int    `json:"importance"`
	Triggers       string `json:"triggers"`
	Pinned         bool   `json:"pinned"`
	Source         string `json:"source"`
	SessionID      string `json:"sessionId,omitempty"`
	AccessCount    int    `json:"accessCount"`
	LastAccessedAt string `json:"lastAccessedAt,omitempty"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

const memoryItemCols = `m.id, m.content, m.importance, m.triggers, m.pinned, m.source, COALESCE(m.session_id,''), m.access_count, COALESCE(m.last_accessed_at,''), m.created_at, m.updated_at`

func scanMemoryItem(scanner interface{ Scan(...interface{}) error }) (MemoryItem, error) {
	var it MemoryItem
	err := scanner.Scan(&it.ID, &it.Content, &it.Importance, &it.Triggers, &it.Pinned, &it.Source,
		&it.SessionID, &it.AccessCount, &it.LastAccessedAt, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return it, err
	}
	return it, nil
}

// --- HTTP handlers（/api/admin-ai/memories*） ---

// handleListMemories 列出记忆；q 参数走全文检索（含评分排序），无 q 按置顶/重要性/最近使用排序。
func (s *Service) handleListMemories(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var items []MemoryItem
	if q == "" {
		rows, err := db.QueryContext(r.Context(),
			`SELECT `+memoryItemCols+` FROM admin_ai_memories m ORDER BY pinned DESC, importance DESC, COALESCE(last_accessed_at, updated_at) DESC, updated_at DESC LIMIT 200`)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()
		for rows.Next() {
			it, scanErr := scanMemoryItem(rows)
			if scanErr != nil {
				response.Error(w, http.StatusInternalServerError, "读取记忆列表失败: "+scanErr.Error())
				return
			}
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		items, err = s.searchMemories(r.Context(), db, q, 50)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	response.OK(w, map[string]interface{}{"items": items, "total": len(items)})
}

// handleCreateMemory 手动新增记忆（管理视图/AI 工具共同底层的 HTTP 入口）。
func (s *Service) handleCreateMemory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content    string `json:"content"`
		Importance *int   `json:"importance"`
		Triggers   string `json:"triggers"`
		Pinned     bool   `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		response.Error(w, http.StatusBadRequest, "记忆内容不能为空")
		return
	}
	if len([]rune(content)) > 500 {
		response.Error(w, http.StatusBadRequest, "记忆内容过长（最多 500 字）")
		return
	}
	importance := 5
	if req.Importance != nil {
		importance = clampImportance(*req.Importance)
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	it, err := s.insertMemory(r.Context(), db, content, importance, strings.TrimSpace(req.Triggers), req.Pinned, "manual", "")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, it)
}

// handleUpdateMemory 部分更新记忆（content/importance/triggers/pinned 任选）。
func (s *Service) handleUpdateMemory(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Content    *string `json:"content"`
		Importance *int    `json:"importance"`
		Triggers   *string `json:"triggers"`
		Pinned     *bool   `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.Content != nil && strings.TrimSpace(*req.Content) == "" {
		response.Error(w, http.StatusBadRequest, "记忆内容不能为空")
		return
	}
	if req.Content != nil && len([]rune(strings.TrimSpace(*req.Content))) > 500 {
		response.Error(w, http.StatusBadRequest, "记忆内容过长（最多 500 字）")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	it, err := s.updateMemory(r.Context(), db, id, memoryPatch{Content: req.Content, Importance: req.Importance, Triggers: req.Triggers, Pinned: req.Pinned})
	if err != nil {
		if errors.Is(err, ErrMemoryNotFound) {
			response.Error(w, http.StatusNotFound, err.Error())
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	response.OK(w, it)
}

// handleDeleteMemory 删除记忆（FTS 索引由触发器同步清理）。
func (s *Service) handleDeleteMemory(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	res, err := db.ExecContext(r.Context(), `DELETE FROM admin_ai_memories WHERE id = ?`, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "记忆条目不存在")
		return
	}
	response.OK(w, map[string]interface{}{"deleted": true})
}

// --- 存储层 ---

func (s *Service) insertMemory(ctx context.Context, db *sql.DB, content string, importance int, triggers string, pinned bool, source, sessionID string) (MemoryItem, error) {
	id, err := randomID("aamem_")
	if err != nil {
		return MemoryItem{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`INSERT INTO admin_ai_memories (id, content, importance, triggers, pinned, source, session_id, access_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		id, content, importance, triggers, boolToInt(pinned), source, sessionID, now, now)
	if err != nil {
		return MemoryItem{}, err
	}
	return MemoryItem{ID: id, Content: content, Importance: importance, Triggers: triggers, Pinned: pinned, Source: source, SessionID: sessionID, CreatedAt: now, UpdatedAt: now}, nil
}

type memoryPatch struct {
	Content    *string
	Importance *int
	Triggers   *string
	Pinned     *bool
}

func (s *Service) updateMemory(ctx context.Context, db *sql.DB, id string, patch memoryPatch) (MemoryItem, error) {
	var existing MemoryItem
	err := db.QueryRowContext(ctx,
		`SELECT `+memoryItemCols+` FROM admin_ai_memories m WHERE m.id = ?`, id).Scan(
		&existing.ID, &existing.Content, &existing.Importance, &existing.Triggers, &existing.Pinned, &existing.Source,
		&existing.SessionID, &existing.AccessCount, &existing.LastAccessedAt, &existing.CreatedAt, &existing.UpdatedAt)
	if err == sql.ErrNoRows {
		return MemoryItem{}, ErrMemoryNotFound
	}
	if err != nil {
		return MemoryItem{}, err
	}

	newContent := existing.Content
	if patch.Content != nil {
		newContent = strings.TrimSpace(*patch.Content)
	}
	newImportance := existing.Importance
	if patch.Importance != nil {
		newImportance = clampImportance(*patch.Importance)
	}
	newTriggers := existing.Triggers
	if patch.Triggers != nil {
		newTriggers = strings.TrimSpace(*patch.Triggers)
	}
	newPinned := existing.Pinned
	if patch.Pinned != nil {
		newPinned = *patch.Pinned
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`UPDATE admin_ai_memories SET content = ?, importance = ?, triggers = ?, pinned = ?, updated_at = ? WHERE id = ?`,
		newContent, newImportance, newTriggers, boolToInt(newPinned), now, id)
	if err != nil {
		return MemoryItem{}, err
	}
	existing.Content, existing.Importance, existing.Triggers, existing.Pinned, existing.UpdatedAt = newContent, newImportance, newTriggers, newPinned, now
	return existing, nil
}

// bumpMemoryAccess 记录检索命中（访问计数与最近访问时间，供默认排序参考）。
func bumpMemoryAccess(ctx context.Context, db *sql.DB, ids []string) {
	if len(ids) == 0 {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, now)
	marks := make([]string, 0, len(ids))
	for _, id := range ids {
		marks = append(marks, "?")
		args = append(args, id)
	}
	_, _ = db.ExecContext(ctx,
		`UPDATE admin_ai_memories SET access_count = access_count + 1, last_accessed_at = ? WHERE id IN (`+strings.Join(marks, ",")+`)`,
		args...)
}

// ---- 检索与评分（memory_search / bootstrap 注入共用） ----

// memoryRecencyFactor 时间衰减：按 updated_at 距今天数，30 天半衰期（OpenClaw temporal-decay 同款）。
func memoryRecencyFactor(updatedAt string, now time.Time) float64 {
	t, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return 1.0
	}
	days := now.Sub(t).Hours() / 24
	if days <= 0 {
		return 1.0
	}
	return math.Pow(0.5, days/30)
}

// memoryScore 综合评分：bm25 归一化 × importance 乘数 × recency 衰减 × pinned 加成。
func memoryScore(rank float64, importance int, pinned bool, updatedAt string, now time.Time) float64 {
	score := 1.0 / (1.0 + math.Abs(rank))
	score *= 1.0 + float64(importance-5)*0.06
	if pinned {
		score *= 1.3
	}
	score *= memoryRecencyFactor(updatedAt, now)
	return score
}

func clampImportance(v int) int {
	if v < 1 {
		return 1
	}
	if v > 10 {
		return 10
	}
	return v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type memoryToken struct {
	raw string
}

// memoryTokens 提取查询词条（保留原始大小写），供 FTS 构建与短词条补偿共用。
func memoryTokens(q string) []string {
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r >= 0x3400 && r <= 0x4dbf || r >= 0x4e00 && r <= 0x9fff || r >= 0x3040 && r <= 0x30ff)
	})
	seen := make(map[string]bool, len(fields))
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		k := strings.ToLower(f)
		if seen[k] {
			continue
		}
		seen[k] = true
		tokens = append(tokens, k)
	}
	return tokens
}

// buildMemoryFTSQuery 把查询词转化为 FTS5 trigram 安全查询：
// 仅保留 ≥3 字符的词条（trigram 索引要求），AND 组合；短词条留给 Go 侧子串补偿。
func buildMemoryFTSQuery(tokens []string) string {
	parts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if len([]rune(tok)) < 3 {
			continue
		}
		parts = append(parts, `"`+strings.ReplaceAll(tok, `"`, `""`)+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " AND ")
}

// memoryShortTokens 提取 <3 字符的词条（已小写化），用于对候选取做子串补偿过滤。
func memoryShortTokens(tokens []string) []string {
	var short []string
	for _, tok := range tokens {
		if len([]rune(tok)) < 3 {
			short = append(short, tok)
		}
	}
	return short
}

// searchMemories 全文检索：FTS5 trigram 取候选（bm25 排序）→ 短词条子串补偿 → 综合评分重排 top limit。
func (s *Service) searchMemories(ctx context.Context, db *sql.DB, q string, limit int) ([]MemoryItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 6
	}
	now := time.Now().UTC()
	tokens := memoryTokens(q)
	shortTokens := memoryShortTokens(tokens)
	subMatch := func(content string) bool {
		lower := strings.ToLower(content)
		for _, tok := range shortTokens {
			if !strings.Contains(lower, tok) {
				return false
			}
		}
		return true
	}

	type scored struct {
		item  MemoryItem
		score float64
	}
	candidates := make([]scored, 0, 64)
	var ftsErr error

	ftsQuery := buildMemoryFTSQuery(tokens)
	if ftsQuery != "" {
		// 外部内容表（content='admin_ai_memories'）与主表 JOIN 时列名会与 fts 虚拟表列歧义，
		// 因此分两段：FTS 只取 rowid + bm25 排名，再按 rowid 查主表。
		rows, err := db.QueryContext(ctx,
			`SELECT rowid, bm25(admin_ai_memories_fts) FROM admin_ai_memories_fts WHERE admin_ai_memories_fts MATCH ? ORDER BY rank LIMIT 64`,
			ftsQuery)
		if err == nil {
			hitRanks := make(map[int64]float64, 64)
			rowIDs := make([]int64, 0, 64)
			for rows.Next() {
				var r int64
				var rank float64
				if scanErr := rows.Scan(&r, &rank); scanErr != nil {
					continue
				}
				if _, dup := hitRanks[r]; !dup {
					rowIDs = append(rowIDs, r)
				}
				hitRanks[r] = rank
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				rows.Close()
				return nil, rowsErr
			}
			rows.Close()
			if len(rowIDs) > 0 {
				marks := make([]string, len(rowIDs))
				args := make([]interface{}, len(rowIDs))
				for i, rid := range rowIDs {
					marks[i] = "?"
					args[i] = rid
				}
				mrows, err := db.QueryContext(ctx,
					`SELECT m.rowid, `+memoryItemCols+` FROM admin_ai_memories m WHERE m.rowid IN (`+strings.Join(marks, ",")+`)`,
					args...)
				if err == nil {
					for mrows.Next() {
						var rid int64
						var it MemoryItem
						scanErr := mrows.Scan(&rid, &it.ID, &it.Content, &it.Importance, &it.Triggers, &it.Pinned, &it.Source,
							&it.SessionID, &it.AccessCount, &it.LastAccessedAt, &it.CreatedAt, &it.UpdatedAt)
						if scanErr != nil {
							continue
						}
						if !subMatch(it.Content) {
							continue
						}
						candidates = append(candidates, scored{item: it, score: memoryScore(hitRanks[rid], it.Importance, it.Pinned, it.UpdatedAt, now)})
					}
					if merr := mrows.Err(); merr != nil {
						mrows.Close()
						return nil, merr
					}
					mrows.Close()
				}
			}
		} else {
			ftsErr = err
		}
	}

	if ftsQuery == "" || ftsErr != nil {
		// 无长词条（纯短词检索）或 FTS 异常：降级为全表子串扫描，保证检索永不失败
		rows, err := db.QueryContext(ctx,
			`SELECT `+memoryItemCols+` FROM admin_ai_memories m LIMIT 1000`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			it, scanErr := scanMemoryItem(rows)
			if scanErr != nil {
				continue
			}
			if !subMatch(it.Content) {
				continue
			}
			candidates = append(candidates, scored{item: it, score: memoryScore(0, it.Importance, it.Pinned, it.UpdatedAt, now)})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	items := make([]MemoryItem, 0, len(candidates))
	hitIDs := make([]string, 0, len(candidates))
	for _, c := range candidates {
		items = append(items, c.item)
		hitIDs = append(hitIDs, c.item.ID)
	}
	bumpMemoryAccess(ctx, db, hitIDs)
	return items, nil
}

// bootstrapMemories 为 system prompt 组装常驻记忆区块：
// 置顶与高重要性优先，按字符预算截断；返回空串表示无记忆。
func (s *Service) bootstrapMemories(ctx context.Context, db *sql.DB, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	rows, err := db.QueryContext(ctx,
		`SELECT `+memoryItemCols+` FROM admin_ai_memories m ORDER BY pinned DESC, importance DESC, COALESCE(last_accessed_at, updated_at) DESC, updated_at DESC LIMIT 40`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var sb strings.Builder
	budget := maxChars
	for rows.Next() {
		it, scanErr := scanMemoryItem(rows)
		if scanErr != nil {
			continue
		}
		line := fmt.Sprintf("- [重要度 %d] %s", it.Importance, it.Content)
		runes := len([]rune(line)) + 2
		if runes > budget {
			continue
		}
		sb.WriteString(line)
		sb.WriteString("\n")
		budget -= runes
	}
	if sb.Len() == 0 {
		return ""
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// ---- AI 工具执行（memory_search / memory_add / memory_delete） ----

type memorySearchResult struct {
	ID         string  `json:"id"`
	Content    string  `json:"content"`
	Importance int     `json:"importance"`
	Triggers   string  `json:"triggers"`
	Score      float64 `json:"score"`
	UpdatedAt  string  `json:"updatedAt"`
}

// executeMemorySearch 工具实现：query（必填）、limit（默认 6，最大 10）。
func (s *Service) executeMemorySearch(ctx context.Context, db *sql.DB, args map[string]interface{}) (interface{}, error) {
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query 不能为空")
	}
	limit := 6
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
		if limit > 10 {
			limit = 10
		}
	}
	items, err := s.searchMemories(ctx, db, query, limit)
	if err != nil {
		return nil, err
	}
	results := make([]memorySearchResult, 0, len(items))
	for _, it := range items {
		results = append(results, memorySearchResult{
			ID: it.ID, Content: it.Content, Importance: it.Importance,
			Triggers: it.Triggers, UpdatedAt: it.UpdatedAt,
		})
	}
	return map[string]interface{}{"results": results, "count": len(results), "query": query}, nil
}

// executeMemoryAdd 工具实现：content（必填）、importance（1-10，默认 5）、triggers（选填）。
func (s *Service) executeMemoryAdd(ctx context.Context, db *sql.DB, args map[string]interface{}, sessionID string) (interface{}, error) {
	content, _ := args["content"].(string)
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("content 不能为空")
	}
	if len([]rune(content)) > 500 {
		return nil, fmt.Errorf("记忆内容过长（最多 500 字），请压缩为要点")
	}
	importance := 5
	if v, ok := args["importance"].(float64); ok {
		importance = clampImportance(int(v))
	}
	triggers, _ := args["triggers"].(string)
	it, err := s.insertMemory(ctx, db, content, importance, strings.TrimSpace(triggers), false, "agent", sessionID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"ok": true, "id": it.ID, "content": it.Content, "importance": it.Importance,
		"triggers": it.Triggers,
	}, nil
}

// executeMemoryDelete 工具实现：id 必填。
func (s *Service) executeMemoryDelete(ctx context.Context, db *sql.DB, args map[string]interface{}) (interface{}, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id 不能为空")
	}
	res, err := db.ExecContext(ctx, `DELETE FROM admin_ai_memories WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, fmt.Errorf("记忆条目不存在: %s", id)
	}
	return map[string]interface{}{"ok": true, "deletedId": id}, nil
}
