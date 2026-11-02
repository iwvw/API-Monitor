package bookmarks

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// 批量抓取网站 favicon。
//
// 单条抓取（POST /favicon/fetch）只在编辑弹窗里用得上，几十条网址要一个个点。
// 这里提供批量入口：并发抓取、逐条回报结果，并支持「只补缺失的图标」。
//
// 注意抓取是外网请求，必须限并发与总时长，否则几十个死站点会把请求挂很久。

const (
	faviconBatchMaxItems   = 200
	faviconBatchConcurrent = 6
	faviconBatchTimeout    = 90 * time.Second
)

type faviconTarget struct {
	ID    int64  `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type faviconResult struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	IconSrc string `json:"icon_src,omitempty"`
	Error   string `json:"error,omitempty"`
}

// fetchFaviconsBatch 处理 POST /api/bookmarks/favicons/fetch-batch。
//
// 请求体：
//
//	{ "ids": [1,2],              // 指定条目；省略表示全部
//	  "only_missing": true,      // 只处理还没有图标的条目（默认 true）
//	  "overwrite": false,        // 是否覆盖已有图标
//	  "limit": 50,               // 本次最多处理几条（默认 50，上限 200）
//	  "skip_ids": [3,4] }        // 本次跳过（用于前端断点续抓，见下）
//
// skip_ids 的存在是为了修复一个实际踩到的问题：抓取失败的条目（站点 502、
// 超时等）在下一次调用时仍然是「没有图标」，于是永远排在队首，重复抓取
// 同一批失败项而永远走不到后面的条目。前端每轮把失败 id 累积传回来，
// 就能一轮轮推进到全部条目都被尝试过。
func (s *Service) fetchFaviconsBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs         []int64 `json:"ids"`
		OnlyMissing *bool   `json:"only_missing"`
		Overwrite   bool    `json:"overwrite"`
		Limit       int     `json:"limit"`
		SkipIDs     []int64 `json:"skip_ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	onlyMissing := true
	if req.OnlyMissing != nil {
		onlyMissing = *req.OnlyMissing
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > faviconBatchMaxItems {
		limit = faviconBatchMaxItems
	}

	// 抓取可能耗时较久，用独立于请求的 context，避免前端超时重试导致重复抓取；
	// 但仍受总超时约束。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), faviconBatchTimeout)
	defer cancel()

	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	targets, err := s.selectFaviconTargets(ctx, db, req.IDs, onlyMissing || !req.Overwrite, limit, req.SkipIDs)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to select bookmarks")
		return
	}
	if len(targets) == 0 {
		response.OK(w, map[string]interface{}{
			"results": []faviconResult{},
			"total":   0, "succeeded": 0, "failed": 0,
			"message": "没有需要抓取的网址",
		})
		return
	}

	results := s.downloadFaviconsConcurrently(ctx, targets)

	succeeded, failed := 0, 0
	for _, res := range results {
		if res.Error != "" {
			failed++
			continue
		}
		if res.IconSrc == "" {
			failed++
			continue
		}
		succeeded++
	}

	response.OK(w, map[string]interface{}{
		"results":   results,
		"total":     len(results),
		"succeeded": succeeded,
		"failed":    failed,
	})
}

// selectFaviconTargets 挑出待抓取的条目。
// skipIDs 中的条目会被跳过（前端断点续抓用，避免反复重试同批失败项）。
func (s *Service) selectFaviconTargets(ctx context.Context, db *sql.DB, ids []int64, onlyMissing bool, limit int, skipIDs []int64) ([]faviconTarget, error) {
	skip := map[int64]bool{}
	for _, id := range skipIDs {
		skip[id] = true
	}

	query := `SELECT id, title, url, icon_src FROM bookmarks`
	args := []interface{}{}
	if len(ids) > 0 {
		placeholders := make([]string, 0, len(ids))
		for _, id := range ids {
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}
		query += ` WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	}
	query += ` ORDER BY id ASC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	targets := []faviconTarget{}
	for rows.Next() {
		var id int64
		var title, url, iconSrc sql.NullString
		if err := rows.Scan(&id, &title, &url, &iconSrc); err != nil {
			return nil, err
		}
		if skip[id] {
			continue
		}
		// 只抓 http(s) 链接
		if !strings.HasPrefix(url.String, "http://") && !strings.HasPrefix(url.String, "https://") {
			continue
		}
		if onlyMissing {
			existing := strings.TrimSpace(iconSrc.String)
			// 已有本地/远端图标的跳过；文字图标（icon_src 为空）仍需抓取
			if existing != "" {
				continue
			}
		}
		targets = append(targets, faviconTarget{ID: id, URL: url.String, Title: title.String})
		if len(targets) >= limit {
			break
		}
	}
	return targets, rows.Err()
}

// downloadFaviconsConcurrently 限并发抓取，并逐条写回数据库。
func (s *Service) downloadFaviconsConcurrently(ctx context.Context, targets []faviconTarget) []faviconResult {
	type job struct {
		index  int
		target faviconTarget
	}
	jobs := make(chan job)
	results := make([]faviconResult, len(targets))

	workers := faviconBatchConcurrent
	if len(targets) < workers {
		workers = len(targets)
	}

	done := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := range jobs {
				results[j.index] = s.fetchOneFavicon(ctx, j.target)
			}
		}()
	}

	for i, target := range targets {
		jobs <- job{index: i, target: target}
	}
	close(jobs)
	for i := 0; i < workers; i++ {
		<-done
	}
	return results
}

// fetchOneFavicon 抓取单个条目的图标并落库。
func (s *Service) fetchOneFavicon(ctx context.Context, target faviconTarget) faviconResult {
	res := faviconResult{ID: target.ID, Title: target.Title}

	candidates, err := resolveFaviconCandidates(target.URL)
	if err != nil {
		res.Error = "解析图标地址失败：" + err.Error()
		return res
	}
	localPath, err := s.downloadFaviconFromCandidates(candidates)
	if err != nil {
		res.Error = "下载图标失败：" + err.Error()
		return res
	}
	res.IconSrc = localPath

	// 写回：图标类型设为图片，并清掉文字图标残留。
	// 用独立 context，避免总超时后写不进结果（抓都抓到了，不该白费）。
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	db, err := s.open(writeCtx)
	if err != nil {
		res.Error = "保存图标失败：无法打开数据库"
		return res
	}
	defer db.Close()
	if _, err := db.ExecContext(writeCtx,
		`UPDATE bookmarks SET icon_type = 2, icon_src = ?, icon_text = '', updated_at = datetime('now') WHERE id = ?`,
		localPath, target.ID); err != nil {
		res.Error = "保存图标失败：" + err.Error()
		return res
	}
	return res
}
