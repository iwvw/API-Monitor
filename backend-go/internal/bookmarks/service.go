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
	case len(parts) == 2 && parts[0] == "favicons" && r.Method == http.MethodGet:
		s.serveFaviconFile(w, r, parts[1])
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
		var id, sortOrder int64
		var public, cacheSeconds int
		var title, description, icon, createdAt, updatedAt string
		var slug, domain, configJSON sql.NullString
		if err := rows.Scan(&id, &title, &description, &icon, &sortOrder, &public, &slug, &domain, &cacheSeconds, &configJSON, &createdAt, &updatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to scan group")
			return
		}
		groups = append(groups, map[string]interface{}{
			"id":            id,
			"title":         title,
			"description":   description,
			"icon":          icon,
			"sort_order":    sortOrder,
			"public":        public == 1,
			"slug":          slug.String,
			"domain":        domain.String,
			"cache_seconds": cacheSeconds,
			"config":        parseConfigJSON(configJSON.String),
			"created_at":    createdAt,
			"updated_at":    updatedAt,
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
		var id, groupID, sortOrder int64
		var iconType, openMethod int
		var title, url, description, iconSrc, iconText, iconBgColor string
		if err := itemRows.Scan(&id, &groupID, &title, &url, &description, &iconType, &iconSrc, &iconText, &iconBgColor, &openMethod, &sortOrder); err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to scan bookmark")
			return
		}
		item := map[string]interface{}{
			"id":           id,
			"group_id":     groupID,
			"title":        title,
			"url":          url,
			"description":  description,
			"icon_type":    iconType,
			"icon_src":     iconSrc,
			"icon_text":    iconText,
			"icon_bg_color": iconBgColor,
			"open_method":  openMethod,
			"sort_order":   sortOrder,
		}
		itemsByGroup[groupID] = append(itemsByGroup[groupID], item)
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

	res, err := db.ExecContext(ctx, `INSERT INTO bookmark_groups (title, description, icon, public, slug, domain, cache_seconds, config_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		title, description, icon, boolToInt(public), slug, domain, cacheSeconds, configJSON)
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
			"sort_order":    0,
			"public":        public,
			"slug":          slug,
			"domain":        domain,
			"cache_seconds": cacheSeconds,
			"config":        parseConfigJSON(configJSON),
			"items":         []map[string]interface{}{},
		},
	})
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

	for _, item := range req.Items {
		if _, err := db.ExecContext(ctx, `UPDATE bookmark_groups SET sort_order = ? WHERE id = ?`, item.Sort, item.ID); err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to save group sort")
			return
		}
	}
	response.OK(w, map[string]interface{}{"updated": true})
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

	slug := stringValue(req["slug"])
	if strings.TrimSpace(slug) != "" {
		slug = normalizeSlug(slug)
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
	domain := normalizeDomain(stringValue(req["domain"]))
	cacheSeconds := intValue(req["cache_seconds"], 300)
	if cacheSeconds <= 0 {
		cacheSeconds = 300
	}

	res, err := db.ExecContext(ctx, `UPDATE bookmark_groups SET title = ?, description = ?, icon = ?, public = ?, slug = ?, domain = ?, cache_seconds = ?, config_json = ?, updated_at = datetime('now') WHERE id = ?`,
		title, stringValue(req["description"]), stringValue(req["icon"]), boolToInt(boolValue(req["public"])), slug, domain, cacheSeconds, marshalConfig(req["config"]), id)
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
		var id, groupID, sortOrder int64
		var iconType, openMethod int
		var title, url, description, iconSrc, iconText, iconBgColor string
		if err := rows.Scan(&id, &groupID, &title, &url, &description, &iconType, &iconSrc, &iconText, &iconBgColor, &openMethod, &sortOrder); err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to scan bookmark")
			return
		}
		items = append(items, map[string]interface{}{
			"id":           id,
			"group_id":     groupID,
			"title":        title,
			"url":          url,
			"description":  description,
			"icon_type":    iconType,
			"icon_src":     iconSrc,
			"icon_text":    iconText,
			"icon_bg_color": iconBgColor,
			"open_method":  openMethod,
			"sort_order":   sortOrder,
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
			"id":           id,
			"group_id":     groupID,
			"title":        title,
			"url":          rawURL,
			"description":  stringValue(req["description"]),
			"icon_type":    intValue(req["icon_type"], 2),
			"icon_src":     stringValue(req["icon_src"]),
			"icon_text":    stringValue(req["icon_text"]),
			"icon_bg_color": stringValue(req["icon_bg_color"]),
			"open_method":  intValue(req["open_method"], 2),
			"sort_order":   intValue(req["sort_order"], 0),
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

	for _, item := range req.Items {
		if _, err := db.ExecContext(ctx, `UPDATE bookmarks SET sort_order = ? WHERE id = ? AND group_id = ?`, item.Sort, item.ID, req.GroupID); err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to save bookmark sort")
			return
		}
	}
	response.OK(w, map[string]interface{}{"updated": true})
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
	iconURL, err := resolveFaviconURL(req.URL)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "favicon resolve failed: "+err.Error())
		return
	}
	localPath, err := s.downloadFavicon(iconURL)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "favicon download failed: "+err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"icon_src": localPath})
}

func (s *Service) serveFaviconFile(w http.ResponseWriter, r *http.Request, filename string) {
	if strings.Contains(filename, "..") || strings.ContainsAny(filename, `/\`) {
		response.Error(w, http.StatusBadRequest, "invalid filename")
		return
	}
	fullPath := filepath.Join(s.faviconDir(), filename)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, fullPath)
}

// --- Public ---

func (s *Service) isPublicRoute(parts []string, method string) bool {
	return (len(parts) == 3 && parts[0] == "public" && parts[1] == "groups" && method == http.MethodGet) ||
		(len(parts) == 2 && parts[0] == "public" && parts[1] == "page-by-domain" && method == http.MethodGet)
}

func (s *Service) servePublic(w http.ResponseWriter, r *http.Request, parts []string) {
	switch {
	case len(parts) == 3 && parts[0] == "public" && parts[1] == "groups":
		s.publicGroup(w, r, parts[2])
	case len(parts) == 2 && parts[0] == "public" && parts[1] == "page-by-domain":
		s.publicGroupByDomain(w, r)
	default:
		response.Error(w, http.StatusNotFound, "bookmarks public route not implemented")
	}
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
	response.OK(w, map[string]interface{}{"group": group})
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
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM bookmark_groups WHERE slug = ?`, candidate).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
		suffix++
	}
}

// --- helpers ---

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

// normalizeSlug 将文本规范化为小写连字符 slug，仅保留字母与数字。
func normalizeSlug(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
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
