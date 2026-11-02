package bookmarks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/publicpageicon"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type Service struct {
	cfg   config.Config
	store *database.Store
}

func New(cfg config.Config) *Service {
	s := &Service{cfg: cfg, store: database.New(cfg)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := s.store.Open(ctx); err == nil {
		_ = ensureSchema(ctx, db)
		db.Close()
	}
	_ = os.MkdirAll(s.faviconDir(), 0o755)
	return s
}

func (s *Service) faviconDir() string {
	return filepath.Join(s.cfg.DataDir, "bookmarks-favicons")
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := ensureSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/bookmarks")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	if s.isPublicRoute(parts, r.Method) {
		s.servePublic(w, r, parts)
		return
	}

	switch {
	case len(parts) == 1 && parts[0] == "groups":
		s.groups(w, r)
	case len(parts) == 2 && parts[0] == "groups" && parts[1] == "sort" && r.Method == http.MethodPost:
		s.saveGroupSort(w, r)
	case len(parts) == 2 && parts[0] == "groups" && parts[1] == "public" && r.Method == http.MethodPost:
		s.setGroupsPublic(w, r)
	case len(parts) == 2 && parts[0] == "groups":
		s.groupByID(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "items":
		s.items(w, r)
	case len(parts) == 2 && parts[0] == "items" && parts[1] == "sort" && r.Method == http.MethodPost:
		s.saveItemSort(w, r)
	case len(parts) == 2 && parts[0] == "items":
		s.itemByID(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "favicon" && parts[1] == "fetch" && r.Method == http.MethodPost:
		s.fetchFavicon(w, r)
	case len(parts) == 2 && parts[0] == "favicons" && parts[1] == "fetch-batch" && r.Method == http.MethodPost:
		s.fetchFaviconsBatch(w, r)
	case len(parts) == 1 && parts[0] == "public-settings":
		switch r.Method {
		case http.MethodGet:
			s.getPublicSettingsHandler(w, r)
		case http.MethodPut:
			s.updatePublicSettingsHandler(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "backgrounds" && parts[1] == "upload" && r.Method == http.MethodPost:
		s.uploadBackgroundImage(w, r)
	case len(parts) == 1 && parts[0] == "backgrounds" && r.Method == http.MethodGet:
		s.listBackgroundImages(w, r)
	case len(parts) == 2 && parts[0] == "backgrounds" && r.Method == http.MethodDelete:
		s.deleteBackgroundImage(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "favicons" && r.Method == http.MethodGet:
		s.serveFaviconFile(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "import" && r.Method == http.MethodPost:
		s.importSunPanelHandler(w, r)
	case len(parts) == 1 && parts[0] == "import" && r.Method == http.MethodGet:
		s.importInfo(w, r)
	// 原生格式的导出/导入（与 SunPanel 格式区分开，见 export.go）
	case len(parts) == 1 && parts[0] == "export" && r.Method == http.MethodGet:
		s.exportHandler(w, r)
	case len(parts) == 1 && parts[0] == "import-native" && r.Method == http.MethodPost:
		s.importNativeHandler(w, r)
	default:
		response.Error(w, http.StatusNotFound, "bookmarks route not implemented")
	}
}

// --- Groups ---

func (s *Service) groups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listGroups(w, r)
	case http.MethodPost:
		s.createGroup(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) listGroups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT id, title, description, icon, sort_order, public, slug, domain, cache_seconds, config_json, created_at, updated_at FROM bookmark_groups ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to list groups")
		return
	}
	defer rows.Close()

	groups := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var sortOrder, public, cacheSeconds sql.NullInt64
		var title, description, icon, createdAt, updatedAt sql.NullString
		var slug, domain, configJSON sql.NullString
		if err := rows.Scan(&id, &title, &description, &icon, &sortOrder, &public, &slug, &domain, &cacheSeconds, &configJSON, &createdAt, &updatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to scan group")
			return
		}
		cache := int(cacheSeconds.Int64)
		if cache <= 0 {
			cache = 300
		}
		groups = append(groups, map[string]interface{}{
			"id":            id,
			"title":         title.String,
			"description":   description.String,
			"icon":          icon.String,
			"sort_order":    sortOrder.Int64,
			"public":        public.Int64 == 1,
			"slug":          slug.String,
			"domain":        domain.String,
			"cache_seconds": cache,
			"config":        parseConfigJSON(configJSON.String),
			"created_at":    createdAt.String,
			"updated_at":    updatedAt.String,
		})
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to iterate groups")
		return
	}
	rows.Close()

	itemRows, err := db.QueryContext(ctx, `SELECT id, group_id, title, url, description, icon_type, icon_src, icon_text, icon_bg_color, open_method, sort_order FROM bookmarks ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to list bookmarks")
		return
	}
	defer itemRows.Close()

	itemsByGroup := map[int64][]map[string]interface{}{}
	for itemRows.Next() {
		var id, groupID, sortOrder sql.NullInt64
		var iconType, openMethod sql.NullInt64
		var title, url, description, iconSrc, iconText, iconBgColor sql.NullString
		if err := itemRows.Scan(&id, &groupID, &title, &url, &description, &iconType, &iconSrc, &iconText, &iconBgColor, &openMethod, &sortOrder); err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to scan bookmark")
			return
		}
		icon := int(iconType.Int64)
		if icon != 1 && icon != 2 && icon != 3 {
			icon = 2
		}
		open := int(openMethod.Int64)
		if open != 1 && open != 2 {
			open = 2
		}
		itemsByGroup[groupID.Int64] = append(itemsByGroup[groupID.Int64], map[string]interface{}{
			"id":            id.Int64,
			"group_id":      groupID.Int64,
			"title":         title.String,
			"url":           url.String,
			"description":   description.String,
			"icon_type":     icon,
			"icon_src":      iconSrc.String,
			"icon_text":     iconText.String,
			"icon_bg_color": iconBgColor.String,
			"open_method":   open,
			"sort_order":    sortOrder.Int64,
		})
	}

	for _, group := range groups {
		id, _ := group["id"].(int64)
		if items, ok := itemsByGroup[id]; ok {
			group["items"] = items
		} else {
			group["items"] = []map[string]interface{}{}
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"groups": groups})
}

func (s *Service) createGroup(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	title := strings.TrimSpace(stringValue(req["title"]))
	if title == "" {
		response.Error(w, http.StatusBadRequest, "title is required")
		return
	}
	description := stringValue(req["description"])
	icon := stringValue(req["icon"])
	public := boolValue(req["public"])
	domain := normalizeDomain(stringValue(req["domain"]))
	cacheSeconds := intValue(req["cache_seconds"], 300)
	if cacheSeconds <= 0 {
		cacheSeconds = 300
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	slug, err := s.allocateSlug(ctx, db, title, stringValue(req["slug"]))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to allocate slug")
		return
	}
	configJSON := marshalConfig(req["config"])
	// sort_order 此前被丢弃并硬编码为 0，导致批量导入后所有分组挤在同一
	// 排序值上、顺序退化为插入顺序。导入方显式给出的顺序必须被尊重。
	sortOrder := 0
	if raw, ok := req["sort_order"]; ok {
		sortOrder = intValue(raw, 0)
	}

	res, err := db.ExecContext(ctx, `INSERT INTO bookmark_groups (title, description, icon, public, slug, domain, cache_seconds, config_json, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		title, description, icon, boolToInt(public), slug, domain, cacheSeconds, configJSON, sortOrder)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to create group")
		return
	}
	id, _ := res.LastInsertId()
	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"group": map[string]interface{}{
			"id":            id,
			"title":         title,
			"description":   description,
			"icon":          icon,
			"sort_order":    sortOrder,
			"public":        public,
			"slug":          slug,
			"domain":        domain,
			"cache_seconds": cacheSeconds,
			"config":        parseConfigJSON(configJSON),
			"items":         []map[string]interface{}{},
		},
	})
}

// setGroupsPublic 批量设置分组的公开状态。
// 请求体：{"public": true, "ids": [1,2]}（ids 省略/为空表示对所有分组生效）
//
// 这样用户不必逐个进设置里切换；并且公开后如果 slug 为空会自动补一个，
// 否则「已公开但没有地址」仍然访问不到，是此前最容易踩的坑。
func (s *Service) setGroupsPublic(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Public bool    `json:"public"`
		IDs    []int64 `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to begin transaction")
		return
	}
	defer tx.Rollback()

	// 先确定作用范围。
	targets := req.IDs
	if len(targets) == 0 {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM bookmark_groups ORDER BY sort_order ASC, id ASC`)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to list groups")
			return
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				response.Error(w, http.StatusInternalServerError, "Failed to scan group id")
				return
			}
			targets = append(targets, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to iterate groups")
			return
		}
	}

	update, err := tx.PrepareContext(ctx, `UPDATE bookmark_groups SET public = ?, updated_at = datetime('now') WHERE id = ?`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to prepare update")
		return
	}
	defer update.Close()

	affected := 0
	missing := []int64{}
	for _, id := range targets {
		res, err := update.ExecContext(ctx, boolToInt(req.Public), id)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to update group")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			missing = append(missing, id)
			continue
		}
		affected++
	}

	// 公开时必须保证有可用 slug，否则公开了也没有地址可访问。
	slugsFixed := 0
	if req.Public {
		rows, err := tx.QueryContext(ctx, `SELECT id, title FROM bookmark_groups WHERE public = 1 AND (slug IS NULL OR slug = '') ORDER BY id ASC`)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to query groups without slug")
			return
		}
		type pending struct {
			id    int64
			title string
		}
		toFix := []pending{}
		for rows.Next() {
			var p pending
			var title sql.NullString
			if err := rows.Scan(&p.id, &title); err != nil {
				rows.Close()
				response.Error(w, http.StatusInternalServerError, "Failed to scan group")
				return
			}
			p.title = title.String
			toFix = append(toFix, p)
		}
		rows.Close()

		for _, p := range toFix {
			slug, err := allocateSlugTx(ctx, tx, p.title, "")
			if err != nil {
				response.Error(w, http.StatusInternalServerError, "Failed to allocate slug")
				return
			}
			if _, err := tx.ExecContext(ctx, `UPDATE bookmark_groups SET slug = ? WHERE id = ?`, slug, p.id); err != nil {
				response.Error(w, http.StatusInternalServerError, "Failed to set slug")
				return
			}
			slugsFixed++
		}
	}

	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to commit")
		return
	}

	response.OK(w, map[string]interface{}{
		"updated":     affected,
		"missing":     missing,
		"slugs_fixed": slugsFixed,
		"public":      req.Public,
	})
}

// allocateSlugTx 是 allocateSlug 的事务版本（批量操作需要同一个 tx）。
func allocateSlugTx(ctx context.Context, tx *sql.Tx, title, requested string) (string, error) {
	base := normalizeSlug(requested)
	if base == "" {
		base = normalizeSlug(title)
	}
	if base == "" {
		base = "bookmarks"
	}
	candidate := base
	suffix := 2
	for {
		if !isReservedSlug(candidate) {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM bookmark_groups WHERE slug = ?`, candidate).Scan(&count); err != nil {
				return "", err
			}
			if count == 0 {
				return candidate, nil
			}
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
		suffix++
	}
}

func (s *Service) saveGroupSort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			ID   int64 `json:"id"`
			Sort int   `json:"sort"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	// 排序此前逐条 Exec 且无事务：中途失败会留下「改了一半」的顺序，
	// 而对不存在的 id 又返回 updated=true（静默 no-op）。改为单事务 +
	// 校验 + 如实回报命中行数。
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to begin sort transaction")
		return
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `UPDATE bookmark_groups SET sort_order = ? WHERE id = ?`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to prepare group sort")
		return
	}
	defer stmt.Close()

	missing := []int64{}
	for _, item := range req.Items {
		if item.Sort < 0 {
			response.Error(w, http.StatusBadRequest, "sort must be non-negative")
			return
		}
		res, err := stmt.ExecContext(ctx, item.Sort, item.ID)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to save group sort")
			return
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			missing = append(missing, item.ID)
		}
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to commit group sort")
		return
	}
	response.OK(w, map[string]interface{}{"updated": true, "missing": missing})
}

func (s *Service) groupByID(w http.ResponseWriter, r *http.Request, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid group id")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.updateGroup(w, r, id)
	case http.MethodDelete:
		s.deleteGroup(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) updateGroup(w http.ResponseWriter, r *http.Request, id int64) {
	var req map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	title := strings.TrimSpace(stringValue(req["title"]))
	if title == "" {
		response.Error(w, http.StatusBadRequest, "title is required")
		return
	}
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	// 局部更新语义：只有请求体中明确出现的字段才允许被改写。
	// 此前 slug/domain/public/config_json/cache_seconds 无条件写入，
	// 导致「只改标题」的 PUT 会静默取消公开、清空 slug 与自定义图标配置。
	current, found, err := loadGroupRow(ctx, db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to load group")
		return
	}
	if !found {
		response.Error(w, http.StatusNotFound, "Group not found")
		return
	}

	slug := current.Slug
	if raw, ok := req["slug"]; ok {
		slug = normalizeSlug(stringValue(raw))
		if slug != current.Slug && slug != "" {
			var count int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM bookmark_groups WHERE slug = ? AND id != ?`, slug, id).Scan(&count); err != nil {
				response.Error(w, http.StatusInternalServerError, "Failed to check slug")
				return
			}
			if count > 0 {
				response.Error(w, http.StatusBadRequest, "slug already in use")
				return
			}
		}
	}

	domain := current.Domain
	if raw, ok := req["domain"]; ok {
		domain = normalizeDomain(stringValue(raw))
	}
	// public 缺省时保持原值；显式传 null 不做取消公开（避免误取消）。
	public := current.Public
	if raw, ok := req["public"]; ok && raw != nil {
		public = boolToInt(boolValue(raw))
	}
	cacheSeconds := current.CacheSeconds
	if raw, ok := req["cache_seconds"]; ok {
		cacheSeconds = intValue(raw, current.CacheSeconds)
	}
	if cacheSeconds <= 0 {
		cacheSeconds = 300
	}
	// config_json 缺省时保持原值，避免抹掉 publicIconId。
	configJSON := current.ConfigJSON
	if raw, ok := req["config"]; ok && raw != nil {
		configJSON = marshalConfig(raw)
	}
	description := current.Description
	if raw, ok := req["description"]; ok {
		description = stringValue(raw)
	}
	icon := current.Icon
	if raw, ok := req["icon"]; ok {
		icon = stringValue(raw)
	}

	res, err := db.ExecContext(ctx, `UPDATE bookmark_groups SET title = ?, description = ?, icon = ?, public = ?, slug = ?, domain = ?, cache_seconds = ?, config_json = ?, updated_at = datetime('now') WHERE id = ?`,
		title, description, icon, public, nullIfEmpty(slug), domain, cacheSeconds, configJSON, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to update group")
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "Group not found")
		return
	}
	response.OK(w, map[string]interface{}{"updated": true})
}

func (s *Service) deleteGroup(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to begin transaction")
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM bookmarks WHERE group_id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to delete bookmarks")
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bookmark_groups WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to delete group")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to commit delete")
		return
	}
	response.OK(w, map[string]interface{}{"deleted": true})
}

// --- Items ---

func (s *Service) items(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listItems(w, r)
	case http.MethodPost:
		s.createItem(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) listItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	query := `SELECT id, group_id, title, url, description, icon_type, icon_src, icon_text, icon_bg_color, open_method, sort_order FROM bookmarks`
	args := []interface{}{}
	if groupID := r.URL.Query().Get("group_id"); groupID != "" {
		if id, perr := strconv.ParseInt(groupID, 10, 64); perr == nil {
			query += ` WHERE group_id = ?`
			args = append(args, id)
		}
	}
	query += ` ORDER BY sort_order ASC, id ASC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to list bookmarks")
		return
	}
	defer rows.Close()

	items := []map[string]interface{}{}
	for rows.Next() {
		var id, groupID, sortOrder sql.NullInt64
		var iconType, openMethod sql.NullInt64
		var title, url, description, iconSrc, iconText, iconBgColor sql.NullString
		if err := rows.Scan(&id, &groupID, &title, &url, &description, &iconType, &iconSrc, &iconText, &iconBgColor, &openMethod, &sortOrder); err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to scan bookmark")
			return
		}
		icon := int(iconType.Int64)
		if icon != 1 && icon != 2 && icon != 3 {
			icon = 2
		}
		open := int(openMethod.Int64)
		if open != 1 && open != 2 {
			open = 2
		}
		items = append(items, map[string]interface{}{
			"id":            id.Int64,
			"group_id":      groupID.Int64,
			"title":         title.String,
			"url":           url.String,
			"description":   description.String,
			"icon_type":     icon,
			"icon_src":      iconSrc.String,
			"icon_text":     iconText.String,
			"icon_bg_color": iconBgColor.String,
			"open_method":   open,
			"sort_order":    sortOrder.Int64,
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Service) createItem(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	groupID := int64Value(req["group_id"])
	if groupID == 0 {
		response.Error(w, http.StatusBadRequest, "group_id is required")
		return
	}
	title := strings.TrimSpace(stringValue(req["title"]))
	rawURL := strings.TrimSpace(stringValue(req["url"]))
	if title == "" || rawURL == "" {
		response.Error(w, http.StatusBadRequest, "title and url are required")
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM bookmark_groups WHERE id = ?`, groupID).Scan(&exists); err != nil || exists == 0 {
		response.Error(w, http.StatusBadRequest, "group does not exist")
		return
	}

	res, err := db.ExecContext(ctx, `INSERT INTO bookmarks (group_id, title, url, description, icon_type, icon_src, icon_text, icon_bg_color, open_method, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		groupID, title, rawURL, stringValue(req["description"]), intValue(req["icon_type"], 2), stringValue(req["icon_src"]), stringValue(req["icon_text"]), stringValue(req["icon_bg_color"]), intValue(req["open_method"], 2), intValue(req["sort_order"], 0))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to create bookmark")
		return
	}
	id, _ := res.LastInsertId()
	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"item": map[string]interface{}{
			"id":            id,
			"group_id":      groupID,
			"title":         title,
			"url":           rawURL,
			"description":   stringValue(req["description"]),
			"icon_type":     intValue(req["icon_type"], 2),
			"icon_src":      stringValue(req["icon_src"]),
			"icon_text":     stringValue(req["icon_text"]),
			"icon_bg_color": stringValue(req["icon_bg_color"]),
			"open_method":   intValue(req["open_method"], 2),
			"sort_order":    intValue(req["sort_order"], 0),
		},
	})
}

func (s *Service) saveItemSort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID int64 `json:"group_id"`
		Items   []struct {
			ID   int64 `json:"id"`
			Sort int   `json:"sort"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	// group_id 缺省/为 0 时此前会匹配零行却返回成功，让调用方误以为排序已保存。
	if req.GroupID == 0 {
		response.Error(w, http.StatusBadRequest, "group_id is required")
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to begin sort transaction")
		return
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `UPDATE bookmarks SET sort_order = ? WHERE id = ? AND group_id = ?`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to prepare bookmark sort")
		return
	}
	defer stmt.Close()

	missing := []int64{}
	for _, item := range req.Items {
		if item.Sort < 0 {
			response.Error(w, http.StatusBadRequest, "sort must be non-negative")
			return
		}
		res, err := stmt.ExecContext(ctx, item.Sort, item.ID, req.GroupID)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to save bookmark sort")
			return
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			missing = append(missing, item.ID)
		}
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to commit bookmark sort")
		return
	}
	response.OK(w, map[string]interface{}{"updated": true, "missing": missing})
}

func (s *Service) itemByID(w http.ResponseWriter, r *http.Request, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid bookmark id")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.updateItem(w, r, id)
	case http.MethodDelete:
		s.deleteItem(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) updateItem(w http.ResponseWriter, r *http.Request, id int64) {
	var req map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	title := strings.TrimSpace(stringValue(req["title"]))
	rawURL := strings.TrimSpace(stringValue(req["url"]))
	if title == "" || rawURL == "" {
		response.Error(w, http.StatusBadRequest, "title and url are required")
		return
	}
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	res, err := db.ExecContext(ctx, `UPDATE bookmarks SET title = ?, url = ?, description = ?, icon_type = ?, icon_src = ?, icon_text = ?, icon_bg_color = ?, open_method = ?, updated_at = datetime('now') WHERE id = ?`,
		title, rawURL, stringValue(req["description"]), intValue(req["icon_type"], 2), stringValue(req["icon_src"]), stringValue(req["icon_text"]), stringValue(req["icon_bg_color"]), intValue(req["open_method"], 2), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to update bookmark")
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "Bookmark not found")
		return
	}
	response.OK(w, map[string]interface{}{"updated": true})
}

func (s *Service) deleteItem(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `DELETE FROM bookmarks WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to delete bookmark")
		return
	}
	response.OK(w, map[string]interface{}{"deleted": true})
}

// --- Favicon ---

func (s *Service) fetchFavicon(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		response.Error(w, http.StatusBadRequest, "url is required")
		return
	}
	candidates, err := resolveFaviconCandidates(req.URL)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "favicon resolve failed: "+err.Error())
		return
	}
	localPath, err := s.downloadFaviconFromCandidates(candidates)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "favicon download failed: "+err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"icon_src": localPath})
}

// faviconFilenameRe 与 favicon.go 的 md5+扩展名命名严格对应。
// 用白名单取代此前的「黑名单 + filepath.Join」，避免任何路径构造空间。
// 白名单必须包含 svg：favicon.go 明确支持并优选位图、回落到 SVG，
// 漏掉它会让存下来的 .svg 图标一律 400，等于这条回退路径从来没生效过。
var faviconFilenameRe = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|gif|ico|webp|svg)$`)

func (s *Service) serveFaviconFile(w http.ResponseWriter, r *http.Request, filename string) {
	if !faviconFilenameRe.MatchString(filename) {
		response.Error(w, http.StatusBadRequest, "invalid filename")
		return
	}
	fullPath := filepath.Join(s.faviconDir(), filename)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// SVG 是唯一可携带脚本的图标格式。落盘时已剥离脚本/事件属性，
	// 但浏览器直接访问这个地址时会把它当独立文档解析，
	// 因此再加一层 CSP：不加载任何子资源、禁止脚本执行。
	// style-src 放开内联样式是为了不破坏 SVG 里的渐变与路径着色。
	if strings.HasSuffix(filename, ".svg") {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	http.ServeFile(w, r, fullPath)
}

// --- Public ---

// ReservedAggregateSlug 是「全部公开分组」聚合页的保留 slug。
// allocateSlug 会避开它，因此用户分组不会占用该地址。
const ReservedAggregateSlug = "all"

func (s *Service) isPublicRoute(parts []string, method string) bool {
	if method != http.MethodGet {
		return false
	}
	// GET /public/all            -> 全部分组（默认排序）
	// GET /public/all/{sort}     -> 全部分组（指定排序）
	if len(parts) >= 2 && parts[0] == "public" && parts[1] == "all" && len(parts) <= 3 {
		return true
	}
	return (len(parts) == 3 && parts[0] == "public" && parts[1] == "groups") ||
		(len(parts) == 2 && parts[0] == "public" && parts[1] == "page-by-domain")
}

func (s *Service) servePublic(w http.ResponseWriter, r *http.Request, parts []string) {
	switch {
	case len(parts) >= 2 && parts[0] == "public" && parts[1] == "all":
		spec := ""
		if len(parts) == 3 {
			spec = parts[2]
		}
		s.publicAllGroups(w, r, spec)
	case len(parts) == 3 && parts[0] == "public" && parts[1] == "groups":
		s.publicGroup(w, r, parts[2])
	case len(parts) == 2 && parts[0] == "public" && parts[1] == "page-by-domain":
		s.publicGroupByDomain(w, r)
	default:
		response.Error(w, http.StatusNotFound, "bookmarks public route not implemented")
	}
}

// publicAllGroups 返回全部已公开分组（public = 1）及其网址，供 /bookmarks/all 聚合页使用。
// parts[2] 为可选的 sort（name|items|updated）与 order（asc|desc）组合，形如 "name-asc"。
func (s *Service) publicAllGroups(w http.ResponseWriter, r *http.Request, spec string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	groups, err := listPublicGroups(ctx, db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to list public groups")
		return
	}

	sortKey, sortOrder := parsePublicSort(spec)
	sortPublicGroups(groups, sortKey, sortOrder)

	// 聚合页的缓存时长取所有公开分组中最保守（最小）的值，
	// 避免某个分组刚改完却因别的分组的长缓存而看不到更新。
	cacheSeconds := 300
	for index, group := range groups {
		if seconds := intValue(group["cache_seconds"], 300); seconds > 0 && (index == 0 || seconds < cacheSeconds) {
			cacheSeconds = seconds
		}
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", cacheSeconds))

	totalItems := 0
	for _, group := range groups {
		if items, ok := group["items"].([]map[string]interface{}); ok {
			totalItems += len(items)
		}
	}
	// 统计未公开的分组数量，让聚合页能明确告知「还有 N 个分组未公开」，
	// 避免用户以为分组丢了。只暴露数量，不泄漏标题等内容。
	var hiddenGroups int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM bookmark_groups WHERE public IS NULL OR public != 1`).Scan(&hiddenGroups); err != nil {
		hiddenGroups = 0
	}

	// 背景是「整个公开页」的全局配置，随聚合数据一起下发，
	// 这样免登录访客一次请求就能拿到渲染所需的全部信息。
	publicConfig, err := s.loadPublicSettings(ctx, db)
	if err != nil {
		publicConfig = map[string]interface{}{}
	}

	response.OK(w, map[string]interface{}{
		"groups":        groups,
		"total":         len(groups),
		"total_items":   totalItems,
		"hidden_groups": hiddenGroups,
		"sort":          sortKey,
		"order":         sortOrder,
		"config":        publicConfig,
	})
}

// parsePublicSort 解析 "name-asc" 这类排序描述，默认按分组顺序（sort_order）。
func parsePublicSort(spec string) (string, string) {
	key, order := "order", "asc"
	value := strings.ToLower(strings.TrimSpace(spec))
	if value == "" || value == "default" {
		return key, order
	}
	parts := strings.SplitN(value, "-", 2)
	switch parts[0] {
	case "name", "title":
		key = "name"
	case "items", "count":
		key = "items"
	case "updated":
		key = "updated"
	case "order":
		key = "order"
	default:
		return "order", "asc"
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "desc":
			order = "desc"
		case "asc":
			order = "asc"
		}
	}
	return key, order
}

func (s *Service) publicGroup(w http.ResponseWriter, r *http.Request, slug string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()
	group, ok, err := getPublicGroup(r.Context(), db, slug)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "Not found")
		return
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", intValue(group["cache_seconds"], 300)))
	// 单分组页也带上全局背景配置（背景属于整个公开页，不随分组变化）
	publicConfig, err := s.loadPublicSettings(r.Context(), db)
	if err != nil {
		publicConfig = map[string]interface{}{}
	}
	response.OK(w, map[string]interface{}{"group": group, "config": publicConfig})
}

func (s *Service) publicGroupByDomain(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		domain = r.Host
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()
	normalized := normalizeDomain(domain)
	if normalized == "" {
		response.OK(w, map[string]interface{}{"found": false})
		return
	}
	group, ok, err := getPublicGroupByDomain(r.Context(), db, normalized)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.OK(w, map[string]interface{}{"found": false})
		return
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", intValue(group["cache_seconds"], 300)))
	response.OK(w, map[string]interface{}{"found": true, "group": group})
}

// PublicPageIconID 返回公开分组配置的自定义图标 ID（未设置时为空字符串），
// 供服务端 favicon 解析端点使用；lookup 为 slug 或域名。
func (s *Service) PublicPageIconID(ctx context.Context, lookup string, byDomain bool) (string, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return "", false, err
	}
	defer db.Close()
	arg := normalizeSlug(lookup)
	if byDomain {
		arg = normalizeDomain(lookup)
	}
	return publicpageicon.LookupIconID(ctx, db, "bookmark_groups", arg, byDomain)
}

func getPublicGroup(ctx context.Context, db *sql.DB, slug string) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, title, description, public, slug, domain, cache_seconds, config_json FROM bookmark_groups WHERE slug = ? AND public = 1`, normalizeSlug(slug))
	return groupFromPublicRows(ctx, db, rows, err)
}

func getPublicGroupByDomain(ctx context.Context, db *sql.DB, domain string) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, title, description, public, slug, domain, cache_seconds, config_json FROM bookmark_groups WHERE lower(domain) = lower(?) AND public = 1`, domain)
	return groupFromPublicRows(ctx, db, rows, err)
}

// listPublicGroups 一次取回全部公开分组及其网址。
// 用两条查询（分组 + 全部网址）在内存里归并，避免按分组逐个查询造成 N+1。
func listPublicGroups(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, title, description, public, slug, domain, cache_seconds, config_json, sort_order, updated_at
		FROM bookmark_groups
		WHERE public = 1
		ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []map[string]interface{}{}
	order := []int64{}
	byID := map[int64]map[string]interface{}{}
	for rows.Next() {
		var id int64
		var cacheSeconds, public, sortOrder sql.NullInt64
		var title, description sql.NullString
		var slug, domain, configJSON, updatedAt sql.NullString
		if err := rows.Scan(&id, &title, &description, &public, &slug, &domain, &cacheSeconds, &configJSON, &sortOrder, &updatedAt); err != nil {
			return nil, err
		}
		cache := int(cacheSeconds.Int64)
		if cache <= 0 {
			cache = 300
		}
		group := map[string]interface{}{
			"id":            id,
			"title":         title.String,
			"description":   description.String,
			"public":        public.Int64 == 1,
			"slug":          slug.String,
			"domain":        domain.String,
			"cache_seconds": cache,
			"config":        parseConfigJSON(configJSON.String),
			"sort_order":    sortOrder.Int64,
			"updated_at":    updatedAt.String,
			"items":         []map[string]interface{}{},
		}
		groups = append(groups, group)
		byID[id] = group
		order = append(order, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(groups) == 0 {
		return groups, nil
	}

	// 仅取这些公开分组的网址。
	itemRows, err := db.QueryContext(ctx, `
		SELECT group_id, id, title, url, description, icon_type, icon_src, icon_text, icon_bg_color, open_method
		FROM bookmarks
		ORDER BY group_id ASC, sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var groupID, itemID sql.NullInt64
		var iconType, openMethod sql.NullInt64
		var title, url, description, iconSrc, iconText, iconBg sql.NullString
		if err := itemRows.Scan(&groupID, &itemID, &title, &url, &description, &iconType, &iconSrc, &iconText, &iconBg, &openMethod); err != nil {
			return nil, err
		}
		group, ok := byID[groupID.Int64]
		if !ok {
			continue // 非公开分组的网址，跳过
		}
		items, _ := group["items"].([]map[string]interface{})
		group["items"] = append(items, map[string]interface{}{
			"id":            itemID.Int64,
			"group_id":      groupID.Int64,
			"title":         title.String,
			"url":           url.String,
			"description":   description.String,
			"icon_type":     normalizeIconType(int(iconType.Int64)),
			"icon_src":      iconSrc.String,
			"icon_text":     iconText.String,
			"icon_bg_color": iconBg.String,
			"open_method":   normalizeOpenMethod(int(openMethod.Int64)),
		})
	}
	if err := itemRows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

// normalizeIconType / normalizeOpenMethod 把越界或 NULL 的值收敛到合法取值。
func normalizeIconType(value int) int {
	if value == 1 || value == 2 || value == 3 {
		return value
	}
	return 2
}

func normalizeOpenMethod(value int) int {
	if value == 1 {
		return 1
	}
	return 2
}

// sortPublicGroups 按请求的键排序聚合页分组。
func sortPublicGroups(groups []map[string]interface{}, key, order string) {
	less := func(i, j int) bool {
		left, right := groups[i], groups[j]
		switch key {
		case "name":
			return strings.ToLower(stringValue(left["title"])) < strings.ToLower(stringValue(right["title"]))
		case "items":
			leftItems, _ := left["items"].([]map[string]interface{})
			rightItems, _ := right["items"].([]map[string]interface{})
			return len(leftItems) < len(rightItems)
		case "updated":
			return stringValue(left["updated_at"]) < stringValue(right["updated_at"])
		default:
			return int64Value(left["sort_order"]) < int64Value(right["sort_order"])
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if order == "desc" {
			return less(j, i)
		}
		return less(i, j)
	})
}

func groupFromPublicRows(ctx context.Context, db *sql.DB, rows *sql.Rows, err error) (map[string]interface{}, bool, error) {
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, rows.Err()
	}
	var id, cacheSeconds int
	var public int
	var title, description string
	var slug, domain, configJSON sql.NullString
	if err := rows.Scan(&id, &title, &description, &public, &slug, &domain, &cacheSeconds, &configJSON); err != nil {
		return nil, false, err
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	rows.Close()

	itemRows, err := db.QueryContext(ctx, `SELECT id, title, url, description, icon_type, icon_src, icon_text, icon_bg_color, open_method FROM bookmarks WHERE group_id = ? ORDER BY sort_order ASC, id ASC`, id)
	if err != nil {
		return nil, false, err
	}
	defer itemRows.Close()
	items := []map[string]interface{}{}
	for itemRows.Next() {
		var itemID int64
		var iconType, openMethod int
		var title, url, description, iconSrc, iconText, iconBg string
		if err := itemRows.Scan(&itemID, &title, &url, &description, &iconType, &iconSrc, &iconText, &iconBg, &openMethod); err != nil {
			return nil, false, err
		}
		items = append(items, map[string]interface{}{
			"id":            itemID,
			"title":         title,
			"url":           url,
			"description":   description,
			"icon_type":     iconType,
			"icon_src":      iconSrc,
			"icon_text":     iconText,
			"icon_bg_color": iconBg,
			"open_method":   openMethod,
		})
	}
	if err := itemRows.Err(); err != nil {
		return nil, false, err
	}

	return map[string]interface{}{
		"id":            id,
		"title":         title,
		"description":   description,
		"public":        public == 1,
		"slug":          slug.String,
		"domain":        domain.String,
		"cache_seconds": cacheSeconds,
		"config":        parseConfigJSON(configJSON.String),
		"items":         items,
	}, true, nil
}

func (s *Service) allocateSlug(ctx context.Context, db *sql.DB, title, requested string) (string, error) {
	base := normalizeSlug(requested)
	if base == "" {
		base = normalizeSlug(title)
	}
	if base == "" {
		base = "bookmarks"
	}
	candidate := base
	suffix := 2
	for {
		// 保留 slug（如聚合页的 "all"）不可被用户分组占用，否则 /bookmarks/all 会被劫持。
		if !isReservedSlug(candidate) {
			var count int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM bookmark_groups WHERE slug = ?`, candidate).Scan(&count); err != nil {
				return "", err
			}
			if count == 0 {
				return candidate, nil
			}
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
		suffix++
	}
}

// reservedSlugs 是不能分配给用户分组的 slug（对应前端固定路由）。
var reservedSlugs = map[string]bool{
	ReservedAggregateSlug: true, // /bookmarks/all 聚合页
	"index":               true,
	"public":              true,
	"page-by-domain":      true,
}

func isReservedSlug(slug string) bool {
	return reservedSlugs[strings.ToLower(strings.TrimSpace(slug))]
}

// --- helpers ---

// groupRow 承载分组当前完整状态，供局部更新（PUT 缺省字段保持原值）使用。
type groupRow struct {
	Title        string
	Description  string
	Icon         string
	Public       int
	Slug         string
	Domain       string
	CacheSeconds int
	ConfigJSON   string
}

func loadGroupRow(ctx context.Context, db *sql.DB, id int64) (groupRow, bool, error) {
	var row groupRow
	var slug, domain, configJSON sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT title, description, icon, public, slug, domain, cache_seconds, config_json FROM bookmark_groups WHERE id = ?`, id).
		Scan(&row.Title, &row.Description, &row.Icon, &row.Public, &slug, &domain, &row.CacheSeconds, &configJSON)
	if err == sql.ErrNoRows {
		return groupRow{}, false, nil
	}
	if err != nil {
		return groupRow{}, false, err
	}
	row.Slug = slug.String
	row.Domain = domain.String
	row.ConfigJSON = configJSON.String
	return row, true, nil
}

func stringValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return ""
	}
}

func intValue(v interface{}, fallback int) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case string:
		if parsed, err := strconv.Atoi(val); err == nil {
			return parsed
		}
	}
	return fallback
}

func int64Value(v interface{}) int64 {
	return int64(intValue(v, 0))
}

func boolValue(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	default:
		return false
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func parseConfigJSON(raw string) map[string]interface{} {
	if raw == "" {
		return map[string]interface{}{}
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return map[string]interface{}{}
	}
	return config
}

func marshalConfig(v interface{}) string {
	if v == nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// normalizeSlug 将文本规范化为小写连字符 slug。
// ASCII 字母数字保留；非 ASCII（中文等）此前被整体丢弃，导致「网站」「常用工具」
// 一律塌缩为空串、再退化成 bookmarks / bookmarks-2 / bookmarks-3 这类
// 与标题无关且随导入顺序漂移的定位符。这里改为把非 ASCII 码点编码为
// u<hex> 片段，保证同一标题始终得到同一 slug，且不同中文标题不再碰撞。
func normalizeSlug(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range text {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case r > 127:
			// 非 ASCII：保留可判定性（同标题 -> 同 slug）。
			if !lastDash {
				b.WriteByte('-')
			}
			fmt.Fprintf(&b, "u%x-", r)
			lastDash = true
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// normalizeDomain 剥离协议与路径，仅保留 host（含端口剥离）。
func normalizeDomain(value string) string {
	domain := strings.TrimSpace(strings.ToLower(value))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	if index := strings.Index(domain, "/"); index >= 0 {
		domain = domain[:index]
	}
	domain = strings.TrimSuffix(domain, "/")
	if host, _, err := splitHostPort(domain); err == nil {
		return host
	}
	return domain
}

func splitHostPort(hostport string) (string, string, error) {
	if strings.Count(hostport, ":") == 1 {
		idx := strings.LastIndex(hostport, ":")
		return hostport[:idx], hostport[idx+1:], nil
	}
	return hostport, "", fmt.Errorf("missing port")
}
