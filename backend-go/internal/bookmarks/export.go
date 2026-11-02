package bookmarks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// 网址导航的原生导出/导入。
//
// 格式（exportFormat 标识）刻意与 SunPanel 的 .sun-panel.json 区分开：
// 那个格式是「图标 + 面板」的扁平结构，表达不了本项目的分组公开状态、
// 图标类型、打开方式、公开页外观等字段。原生格式能无损往返。
//
// 设计取舍：
//   - 导出**不含 id**：id 是库内自增主键，换一个库就没有意义，
//     带着 id 走反而会让「导入到另一个实例」出现冲突。导入时按顺序重建。
//   - 分组用嵌套的 items 数组，而不是「分组列表 + 条目列表 + group_id」，
//     避免导入方还要自己 join。
//   - publicSettings 一并导出：它是公开页的外观（背景/宽度/搜索引擎），
//     单看分组和网址无法复现同一个公开页。
//   - 不导出 favicon 文件与背景图二进制：导出里只保留 URL 引用。
//     把二进制塞进 JSON 会让导出文件膨胀到几十 MB，且这些资源本来就
//     由各自的 URL 定位。因此导入后可能需要重新抓一次图标。

const (
	exportFormatName    = "api-monitor.bookmarks"
	exportFormatVersion = 1
)

// exportFile 是导出文件的顶层结构。
type exportFile struct {
	Format         string                 `json:"format"`
	Version        int                    `json:"version"`
	ExportedAt     string                 `json:"exportedAt"`
	Groups         []exportGroup          `json:"groups"`
	PublicSettings map[string]interface{} `json:"publicSettings,omitempty"`
	Stats          map[string]int         `json:"stats,omitempty"`
}

type exportGroup struct {
	Title        string                 `json:"title"`
	Description  string                 `json:"description,omitempty"`
	Icon         string                 `json:"icon,omitempty"`
	Public       bool                   `json:"public"`
	Slug         string                 `json:"slug,omitempty"`
	Domain       string                 `json:"domain,omitempty"`
	CacheSeconds int                    `json:"cacheSeconds,omitempty"`
	Config       map[string]interface{} `json:"config,omitempty"`
	Items        []exportItem           `json:"items"`
}

type exportItem struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	IconType    int    `json:"iconType"`
	IconSrc     string `json:"iconSrc,omitempty"`
	IconText    string `json:"iconText,omitempty"`
	IconBgColor string `json:"iconBgColor,omitempty"`
	OpenMethod  int    `json:"openMethod"`
}

// exportHandler 处理 GET /api/bookmarks/export。
// 返回可直接落盘的 JSON（前端触发下载）。
func (s *Service) exportHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	payload, err := s.buildExport(ctx, db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to export bookmarks")
		return
	}
	response.OK(w, payload)
}

// buildExport 组装导出内容（与 HTTP 层解耦，便于测试直接调用）。
func (s *Service) buildExport(ctx context.Context, db *sql.DB) (exportFile, error) {
	out := exportFile{
		Format:     exportFormatName,
		Version:    exportFormatVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Groups:     []exportGroup{},
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, title, description, icon, public, slug, domain, cache_seconds, config_json
		 FROM bookmark_groups ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	byID := map[int64]int{} // group_id -> out.Groups 下标
	for rows.Next() {
		var id int64
		var public, cacheSeconds sql.NullInt64
		var title, description, icon, slug, domain, configJSON sql.NullString
		if err := rows.Scan(&id, &title, &description, &icon, &public, &slug, &domain, &cacheSeconds, &configJSON); err != nil {
			return out, err
		}
		cache := int(cacheSeconds.Int64)
		if cache <= 0 {
			cache = 300
		}
		byID[id] = len(out.Groups)
		out.Groups = append(out.Groups, exportGroup{
			Title:        title.String,
			Description:  description.String,
			Icon:         icon.String,
			Public:       public.Int64 == 1,
			Slug:         slug.String,
			Domain:       domain.String,
			CacheSeconds: cache,
			Config:       parseConfigJSON(configJSON.String),
			Items:        []exportItem{},
		})
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	rows.Close()

	itemRows, err := db.QueryContext(ctx,
		`SELECT group_id, title, url, description, icon_type, icon_src, icon_text, icon_bg_color, open_method
		 FROM bookmarks ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return out, err
	}
	defer itemRows.Close()

	totalItems := 0
	for itemRows.Next() {
		var groupID sql.NullInt64
		var iconType, openMethod sql.NullInt64
		var title, url, description, iconSrc, iconText, iconBgColor sql.NullString
		if err := itemRows.Scan(&groupID, &title, &url, &description, &iconType, &iconSrc, &iconText, &iconBgColor, &openMethod); err != nil {
			return out, err
		}
		idx, ok := byID[groupID.Int64]
		if !ok {
			// 孤儿条目（所属分组已删）：跳过，否则导出里会出现没有归属的网址
			continue
		}
		it := int(iconType.Int64)
		if it != 1 && it != 2 && it != 3 {
			it = 2
		}
		om := int(openMethod.Int64)
		if om != 1 && om != 2 {
			om = 2
		}
		out.Groups[idx].Items = append(out.Groups[idx].Items, exportItem{
			Title:       title.String,
			URL:         url.String,
			Description: description.String,
			IconType:    it,
			IconSrc:     iconSrc.String,
			IconText:    iconText.String,
			IconBgColor: iconBgColor.String,
			OpenMethod:  om,
		})
		totalItems++
	}
	if err := itemRows.Err(); err != nil {
		return out, err
	}

	// 公开页外观一并导出：只带分组和网址无法复现同一个公开页
	if cfg, err := s.loadPublicSettings(ctx, db); err == nil && len(cfg) > 0 {
		out.PublicSettings = cfg
	}

	out.Stats = map[string]int{"groups": len(out.Groups), "items": totalItems}
	return out, nil
}

// importNativeHandler 处理 POST /api/bookmarks/import-native。
//
// 请求体：
//
//	{ "payload": <原生导出文件，对象或 JSON 字符串>,
//	  "mode": "replace|merge", "dry_run": false }
//
// mode：
//   - merge（默认）：按分组标题匹配，已存在的分组只追加不存在的网址；
//   - replace：先清空现有分组与网址，再整份导入（公开设置也会被覆盖）。
//
// dry_run=true 时只解析并回报将要发生的事情，不写库。
func (s *Service) importNativeHandler(w http.ResponseWriter, r *http.Request) {
	// 导出体量可能较大（几千条网址），与 SunPanel 导入同样放宽到 16MiB
	var req struct {
		Payload json.RawMessage `json:"payload"`
		Mode    string          `json:"mode"`
		DryRun  bool            `json:"dry_run"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(req.Payload) == 0 {
		response.Error(w, http.StatusBadRequest, "payload is required")
		return
	}

	parsed, err := parseNativeExport(req.Payload)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode != "replace" && mode != "merge" {
		mode = "merge"
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	report, err := s.importNative(ctx, db, parsed, mode, req.DryRun)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, report)
}

// parseNativeExport 解析并校验导出文件。
// 只接受本项目自己的格式，避免把 SunPanel 的 JSON 误当原生格式导入。
func parseNativeExport(raw []byte) (exportFile, error) {
	var file exportFile
	// payload 可能是对象，也可能是被双引号包起来的 JSON 字符串
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, `"`) {
		var inner string
		if err := json.Unmarshal(raw, &inner); err != nil {
			return file, fmt.Errorf("不是合法的 JSON：%w", err)
		}
		raw = []byte(inner)
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return file, fmt.Errorf("不是合法的 JSON：%w", err)
	}
	if file.Format != exportFormatName {
		return file, fmt.Errorf("不是网址导航的导出文件（format=%q，期望 %q）", file.Format, exportFormatName)
	}
	if file.Version > exportFormatVersion {
		return file, fmt.Errorf("导出文件版本过高（%d），请升级面板后再导入", file.Version)
	}
	if file.Groups == nil {
		return file, fmt.Errorf("导出文件缺少 groups 字段")
	}
	return file, nil
}

// NativeImportReport 是导入/dry-run 的结果。
type NativeImportReport struct {
	DryRun         bool     `json:"dry_run"`
	Mode           string   `json:"mode"`
	GroupsCreated  int      `json:"groups_created"`
	GroupsMerged   int      `json:"groups_merged"`
	ItemsImported  int      `json:"items_imported"`
	ItemsSkipped   int      `json:"items_skipped"`
	SettingsSynced bool     `json:"settings_synced"`
	Warnings       []string `json:"warnings,omitempty"`
}

// importNative 执行导入。dry_run 时只统计不写库。
func (s *Service) importNative(ctx context.Context, db *sql.DB, file exportFile, mode string, dryRun bool) (NativeImportReport, error) {
	report := NativeImportReport{DryRun: dryRun, Mode: mode}

	// 先摸清现有分组，用于 merge 模式的匹配与 skip 判定
	existing := map[string]int64{} // 标题(小写) -> group_id
	rows, err := db.QueryContext(ctx, `SELECT id, title FROM bookmark_groups`)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var id int64
		var title sql.NullString
		if err := rows.Scan(&id, &title); err != nil {
			rows.Close()
			return report, err
		}
		existing[strings.ToLower(strings.TrimSpace(title.String))] = id
	}
	rows.Close()

	// 已存在的 (group_id, url) 组合：merge 模式下用它跳过重复网址
	existingItems := map[string]bool{}
	if mode == "merge" {
		itemRows, err := db.QueryContext(ctx, `SELECT group_id, url FROM bookmarks`)
		if err != nil {
			return report, err
		}
		for itemRows.Next() {
			var groupID sql.NullInt64
			var url sql.NullString
			if err := itemRows.Scan(&groupID, &url); err != nil {
				itemRows.Close()
				return report, err
			}
			existingItems[itemKey(groupID.Int64, url.String)] = true
		}
		itemRows.Close()
	}

	// 当前公开设置是否为空：merge 模式下只有「本来就没配置过」才跟随导入覆盖。
	//
	// **必须在事务外读取**：SQLite 的写事务会持有锁，
	// 若在事务内用另一个连接（db）读同一张表，两者会互相等待造成死锁
	// —— 这个坑实际踩到过，表现为导入接口永久挂起。
	publicSettingsEmpty := false
	if mode != "replace" && len(file.PublicSettings) > 0 {
		if current, err := s.loadPublicSettings(ctx, db); err == nil {
			publicSettingsEmpty = len(current) == 0
		}
	}

	// 事务：整体成功或整体回滚，避免导入一半失败留下残缺数据
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer func() {
		// dry-run 与出错路径都会走到这里；Commit 成功后 Rollback 是无害的
		_ = tx.Rollback()
	}()

	if mode == "replace" {
		// 先删条目再删分组：虽然外键是 ON DELETE CASCADE，
		// 但显式删除不依赖调用方是否开启了 foreign_keys
		if _, err := tx.ExecContext(ctx, `DELETE FROM bookmarks`); err != nil {
			return report, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM bookmark_groups`); err != nil {
			return report, err
		}
		existing = map[string]int64{}
		existingItems = map[string]bool{}
	}

	for gi, group := range file.Groups {
		title := strings.TrimSpace(group.Title)
		if title == "" {
			report.Warnings = append(report.Warnings, fmt.Sprintf("第 %d 个分组没有名称，已跳过", gi+1))
			continue
		}

		key := strings.ToLower(title)
		var groupID int64
		merged := false
		if id, ok := existing[key]; ok {
			groupID = id
			merged = true
			report.GroupsMerged++
			// merge 模式下，已存在的分组保留原有公开设置与外观，
			// 只把它的网址补进来 —— 覆盖会让「导入」变成「改配置」
		} else {
			cache := group.CacheSeconds
			if cache <= 0 {
				cache = 300
			}
			slug := strings.TrimSpace(group.Slug)
			if slug != "" {
				// slug 有唯一约束：导入时若与现有冲突，清空它而不是让整批导入失败
				var exists int
				if err := tx.QueryRowContext(ctx,
					`SELECT COUNT(1) FROM bookmark_groups WHERE slug = ?`, slug).Scan(&exists); err == nil && exists > 0 {
					report.Warnings = append(report.Warnings,
						fmt.Sprintf("分组「%s」的 slug %q 已被占用，已清空（导入后可在管理页重新设置）", title, slug))
					slug = ""
				}
			}
			cfgJSON := "{}"
			if len(group.Config) > 0 {
				if data, err := json.Marshal(group.Config); err == nil {
					cfgJSON = string(data)
				}
			}
			public := 0
			if group.Public {
				public = 1
			}
			res, err := tx.ExecContext(ctx,
				`INSERT INTO bookmark_groups (title, description, icon, sort_order, public, slug, domain, cache_seconds, config_json)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				title, group.Description, group.Icon, gi, public,
				nullIfEmpty(slug), nullIfEmpty(group.Domain), cache, cfgJSON)
			if err != nil {
				return report, err
			}
			groupID, err = res.LastInsertId()
			if err != nil {
				return report, fmt.Errorf("read inserted group id: %w", err)
			}
			existing[key] = groupID
			report.GroupsCreated++
		}

		// 并入已有分组时，新网址必须接在现有条目之后：
		// 直接用组内序号 ii 会和已有条目的 sort_order 撞成 0,1,2…，
		// 排序（sort_order ASC, id ASC）就会把新旧条目交错排列。
		baseSort := 0
		if merged {
			if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM bookmarks WHERE group_id = ?`, groupID).Scan(&baseSort); err != nil {
				return report, err
			}
		}

		for ii, item := range group.Items {
			itemTitle := strings.TrimSpace(item.Title)
			itemURL := strings.TrimSpace(item.URL)
			if itemTitle == "" || itemURL == "" {
				report.ItemsSkipped++
				continue
			}
			if mode == "merge" && existingItems[itemKey(groupID, itemURL)] {
				// 同一分组里已有相同网址：跳过，避免重复导入产生一堆重复卡片
				report.ItemsSkipped++
				continue
			}
			iconType := item.IconType
			if iconType != 1 && iconType != 2 && iconType != 3 {
				iconType = 2
			}
			openMethod := item.OpenMethod
			if openMethod != 1 && openMethod != 2 {
				openMethod = 2
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO bookmarks (group_id, title, url, description, icon_type, icon_src, icon_text, icon_bg_color, open_method, sort_order)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				groupID, itemTitle, itemURL, item.Description,
				iconType, item.IconSrc, item.IconText, item.IconBgColor, openMethod, baseSort+ii); err != nil {
				return report, err
			}
			existingItems[itemKey(groupID, itemURL)] = true
			report.ItemsImported++
		}
	}

	// 公开设置：仅在有内容时覆盖，且只在 replace 模式或当前为空时写入。
	// merge 模式下不动现有外观，避免「导入网址」顺手把背景换了。
	if len(file.PublicSettings) > 0 {
		// 用事务外预先读到的结果判断，不在事务内再查一次（见上方死锁说明）
		shouldSync := mode == "replace" || publicSettingsEmpty
		if shouldSync {
			report.SettingsSynced = true
			if !dryRun {
				if err := s.savePublicSettingsTx(ctx, tx, file.PublicSettings); err != nil {
					return report, err
				}
			}
		}
	}

	if dryRun {
		// dry-run 不落库：直接回滚，报告已经统计完毕
		return report, nil
	}
	if err := tx.Commit(); err != nil {
		return report, err
	}
	return report, nil
}

// nullIfEmpty 把空串写成 NULL（slug 有唯一约束，多个空串会互相冲突）。
func nullIfEmpty(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// itemKey 是「分组 + 网址」的去重键。
func itemKey(groupID int64, url string) string {
	return fmt.Sprintf("%d\x00%s", groupID, strings.ToLower(strings.TrimSpace(url)))
}
