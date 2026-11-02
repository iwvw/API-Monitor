package bookmarks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// SunPanel（.sun-panel.json）导入。
//
// 该格式与本站 bookmarks 模型存在四处语义错配，朴素逐字段搬运必然出问题：
//  1. cardType=3 的「系统应用」卡片没有 url，而 bookmarks.url 为必填 -> 必须跳过。
//  2. icon.itemType=3 在 SunPanel 中表示「图标集名」（如 ri:ai），在本站
//     icon_type=3 表示 Emoji 字符，直接透传会把图标集名当文字渲染出来。
//  3. icon.src 常为 /uploads/... 相对路径（指向 SunPanel 自己的站点），
//     在本站必然 404；需按策略跳过或替换。
//  4. sort 值在同组内可重复、且不连续（如 1,1,...,9999），需重新编号。
//
// 导入必须在单个事务内完成：此前的逐条 POST 会出现「导入一半」且没有失败报告。

// ImportOptions 控制导入行为。
type ImportOptions struct {
	// GroupMode: "new"（每组新建）| "merge"（并入同名分组）| "single"（全部并入一个分组）
	GroupMode string
	// TargetGroupID 在 GroupMode="single" 时生效。
	TargetGroupID int64
	// SkipLocalIcons 跳过 /uploads/ 等本地相对路径图标（默认 true）。
	SkipLocalIcons bool
	// IncludeSystemCards 是否导入无 URL 的系统卡片（默认 false，导入时会转为文字图标占位链接）。
	IncludeSystemCards bool
}

func defaultImportOptions() ImportOptions {
	return ImportOptions{GroupMode: "new", SkipLocalIcons: true}
}

// sunPanelExport 是 .sun-panel.json 的顶层结构。
type sunPanelExport struct {
	Version    int             `json:"version"`
	AppName    string          `json:"appName"`
	ExportTime string          `json:"exportTime"`
	Icons      []sunPanelGroup `json:"icons"`
}

type sunPanelGroup struct {
	Title     string                 `json:"title"`
	Sort      int                    `json:"sort"`
	Children  []sunPanelItem         `json:"children"`
	CardStyle map[string]interface{} `json:"cardStyle"`
}

type sunPanelItem struct {
	Icon struct {
		ItemType        int    `json:"itemType"`
		Src             string `json:"src"`
		Text            string `json:"text"`
		BackgroundColor string `json:"backgroundColor"`
	} `json:"icon"`
	Sort            int                    `json:"sort"`
	Title           string                 `json:"title"`
	URL             string                 `json:"url"`
	LanURL          string                 `json:"lanUrl"`
	Description     string                 `json:"description"`
	OpenMethod      int                    `json:"openMethod"`
	CardType        int                    `json:"cardType"`
	BackgroundColor string                 `json:"backgroundColor"`
	ExpandParam     map[string]interface{} `json:"expandParam"`
}

// ImportReport 是导入结果，逐项列出跳过原因，避免「静默丢数据」。
type ImportReport struct {
	GroupsCreated int          `json:"groups_created"`
	GroupsUsed    int          `json:"groups_used"`
	ItemsImported int          `json:"items_imported"`
	ItemsSkipped  int          `json:"items_skipped"`
	Skipped       []ImportSkip `json:"skipped,omitempty"`
	GroupNames    []string     `json:"group_names,omitempty"`
}

// ImportSkip 记录被跳过的条目及原因。
type ImportSkip struct {
	Group  string `json:"group"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// parseSunPanelExport 解析并做基本校验。
func parseSunPanelExport(raw []byte) (sunPanelExport, error) {
	var export sunPanelExport
	if err := json.Unmarshal(raw, &export); err != nil {
		return export, fmt.Errorf("不是合法的 JSON：%w", err)
	}
	if export.Icons == nil {
		return export, fmt.Errorf("缺少 icons 字段，可能不是 SunPanel 导出的配置文件")
	}
	return export, nil
}

// isSystemCard 判断是否为 SunPanel 内置系统应用卡片（无 URL）。
func isSystemCard(item sunPanelItem) bool {
	return item.CardType == 3 || strings.TrimSpace(item.URL) == ""
}

var hexColorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// normalizeColor 把 SunPanel 的背景色规范为本站可用的 #rrggbb。
// SunPanel 里常出现 "ffffff"（无 #）、""、"rgba(...)" 等，直接塞进
// style.backgroundColor 会被浏览器忽略，并与 bg 的有无判断打架。
func normalizeColor(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "#") {
		value = "#" + value
	}
	if hexColorRe.MatchString(value) {
		return strings.ToLower(value)
	}
	return ""
}

// isLocalIconSrc 判断图标是否指向 SunPanel 自身站点（相对路径），本站无法加载。
func isLocalIconSrc(src string) bool {
	value := strings.TrimSpace(src)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "data:") {
		return false
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return false
	}
	// 其余（/uploads/...、./x.png、x.png）都是相对路径，指向源站。
	return true
}

// normalizeItemURL 校验并规范化链接，只接受 http/https。
func normalizeItemURL(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	if parsed.Host == "" {
		return "", false
	}
	return value, true
}

// mapItem 将一条 SunPanel 条目映射为本站 bookmarks 字段。
// 返回 ok=false 时以 reason 说明跳过原因。
func mapItem(item sunPanelItem, opts ImportOptions) (title, rawURL, description string, iconType int, iconSrc, iconText, iconBg string, openMethod int, ok bool, reason string) {
	title = strings.TrimSpace(item.Title)
	if title == "" {
		return "", "", "", 0, "", "", "", 0, false, "标题为空"
	}

	if isSystemCard(item) {
		if !opts.IncludeSystemCards {
			return "", "", "", 0, "", "", "", 0, false, "SunPanel 系统卡片（无链接）"
		}
		// 允许保留为「文字图标 + 占位链接」，避免丢失条目信息。
		return title, "#", strings.TrimSpace(item.Description), 1, "", firstRune(title), normalizeColor(item.BackgroundColor), 1, true, ""
	}

	normalized, valid := normalizeItemURL(item.URL)
	if !valid {
		return "", "", "", 0, "", "", "", 0, false, fmt.Sprintf("链接不是 http/https：%q", item.URL)
	}
	rawURL = normalized
	description = strings.TrimSpace(item.Description)
	iconBg = normalizeColor(item.Icon.BackgroundColor)
	if iconBg == "" {
		iconBg = normalizeColor(item.BackgroundColor)
	}

	// openMethod: SunPanel 1=当前页 / 2=新窗口；0 与缺失按新窗口处理。
	openMethod = 2
	if item.OpenMethod == 1 {
		openMethod = 1
	}

	// 图标：SunPanel itemType 2=图片 3=图标集名，语义与本站不同。
	switch item.Icon.ItemType {
	case 3:
		// 图标集名（如 ri:ai）在本站无法渲染成图形，降级为文字首字符，
		// 而不是让 icon_type=3 把 "ri:ai" 当 Emoji 整串显示。
		iconType = 1
		iconText = firstRune(title)
		if src := strings.TrimSpace(item.Icon.Src); src != "" && !isLocalIconSrc(src) {
			iconType = 2
			iconSrc = src
			iconText = ""
		}
	case 2:
		src := strings.TrimSpace(item.Icon.Src)
		if src == "" {
			iconType = 1
			iconText = firstRune(title)
			break
		}
		if isLocalIconSrc(src) {
			if opts.SkipLocalIcons {
				iconType = 1
				iconText = firstRune(title)
				reason = "本地图标路径已跳过（改为文字图标）"
				break
			}
			iconType = 2
			iconSrc = src
			break
		}
		iconType = 2
		iconSrc = src
	default:
		if text := strings.TrimSpace(item.Icon.Text); text != "" {
			iconType = 1
			iconText = firstRune(text)
		} else {
			iconType = 1
			iconText = firstRune(title)
		}
	}
	return title, rawURL, description, iconType, iconSrc, iconText, iconBg, openMethod, true, reason
}

// firstRune 取首个字符（按 rune，避免 emoji 被截成半个代理对）。
func firstRune(value string) string {
	for _, r := range strings.TrimSpace(value) {
		return string(r)
	}
	return ""
}

// importInfo 返回导入端点支持的选项，供前端构建表单。
func (s *Service) importInfo(w http.ResponseWriter, r *http.Request) {
	response.OK(w, map[string]interface{}{
		"format": "sun-panel",
		"group_modes": []map[string]string{
			{"value": "new", "label": "每个分组单独创建"},
			{"value": "merge", "label": "并入同名分组"},
			{"value": "single", "label": "全部并入指定分组"},
		},
		"defaults": map[string]interface{}{
			"group_mode":           "new",
			"skip_local_icons":     true,
			"include_system_cards": false,
		},
		"notes": []string{
			"SunPanel 的系统应用卡片没有网址，默认跳过。",
			"图标集名称（如 ri:ai）无法渲染，降级为标题首字。",
			"/uploads/ 相对路径图标指向 SunPanel 站点，默认跳过。",
			"导入在单个事务内完成，失败会整体回滚。",
		},
	})
}

// importSunPanelHandler 处理 POST /api/bookmarks/import。
// 请求体：
//
//	{ "payload": <.sun-panel.json 内容，对象或字符串>,
//	  "group_mode": "new|merge|single", "target_group_id": 1,
//	  "skip_local_icons": true, "include_system_cards": false,
//	  "dry_run": false }
//
// dry_run=true 时只解析并回报将要导入/跳过的内容，不写库。
func (s *Service) importSunPanelHandler(w http.ResponseWriter, r *http.Request) {
	// 导入体量远大于单条目接口，单独放宽到 16MiB（含 base64 图标）。
	var req struct {
		Payload            json.RawMessage `json:"payload"`
		GroupMode          string          `json:"group_mode"`
		TargetGroupID      int64           `json:"target_group_id"`
		SkipLocalIcons     *bool           `json:"skip_local_icons"`
		IncludeSystemCards bool            `json:"include_system_cards"`
		DryRun             bool            `json:"dry_run"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(req.Payload) == 0 {
		response.Error(w, http.StatusBadRequest, "payload is required")
		return
	}

	// payload 既接受内嵌对象，也接受已序列化的字符串（前端直接传文件内容）。
	raw := []byte(req.Payload)
	if len(raw) > 0 && raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			response.Error(w, http.StatusBadRequest, "payload string is not valid")
			return
		}
		raw = []byte(text)
	}

	export, err := parseSunPanelExport(raw)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	opts := defaultImportOptions()
	if req.GroupMode != "" {
		opts.GroupMode = req.GroupMode
	}
	switch opts.GroupMode {
	case "new", "merge", "single":
	default:
		response.Error(w, http.StatusBadRequest, "group_mode must be new, merge or single")
		return
	}
	opts.TargetGroupID = req.TargetGroupID
	if req.SkipLocalIcons != nil {
		opts.SkipLocalIcons = *req.SkipLocalIcons
	}
	opts.IncludeSystemCards = req.IncludeSystemCards

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	if req.DryRun {
		report, err := s.previewSunPanel(ctx, db, export, opts)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"dry_run": true, "report": report})
		return
	}

	report, err := s.importSunPanel(ctx, db, export, opts)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"dry_run": false, "report": report})
}

// previewSunPanel 只解析与映射，不写库，用于导入前的确认弹窗。
func (s *Service) previewSunPanel(ctx context.Context, db *sql.DB, export sunPanelExport, opts ImportOptions) (ImportReport, error) {
	report := ImportReport{Skipped: []ImportSkip{}}
	for _, g := range export.Icons {
		groupTitle := strings.TrimSpace(g.Title)
		if groupTitle == "" {
			groupTitle = fmt.Sprintf("导入分组%d", g.Sort)
		}
		matched := 0
		for _, raw := range g.Children {
			_, _, _, _, _, _, _, _, ok, reason := mapItem(raw, opts)
			if !ok {
				report.ItemsSkipped++
				report.Skipped = append(report.Skipped, ImportSkip{Group: groupTitle, Title: strings.TrimSpace(raw.Title), Reason: reason})
				continue
			}
			if reason != "" {
				report.Skipped = append(report.Skipped, ImportSkip{Group: groupTitle, Title: strings.TrimSpace(raw.Title), Reason: reason})
			}
			report.ItemsImported++
			matched++
		}
		switch opts.GroupMode {
		case "merge":
			var id int64
			err := db.QueryRowContext(ctx, `SELECT id FROM bookmark_groups WHERE title = ? ORDER BY id LIMIT 1`, groupTitle).Scan(&id)
			if err == sql.ErrNoRows {
				report.GroupsCreated++
				report.GroupNames = append(report.GroupNames, groupTitle)
			} else if err != nil {
				return report, err
			} else {
				report.GroupsUsed++
				report.GroupNames = append(report.GroupNames, groupTitle)
			}
		case "single":
			if opts.TargetGroupID == 0 {
				return report, fmt.Errorf("single 模式必须提供 target_group_id")
			}
			report.GroupsUsed++
		default:
			report.GroupsCreated++
			report.GroupNames = append(report.GroupNames, groupTitle)
		}
		_ = matched
	}
	return report, nil
}

func (s *Service) importSunPanel(ctx context.Context, db *sql.DB, export sunPanelExport, opts ImportOptions) (ImportReport, error) {
	report := ImportReport{Skipped: []ImportSkip{}}

	// 预先解析出待写入的结构，避免事务内做无用功。
	type pendingItem struct {
		title, url, description string
		iconType                int
		iconSrc, iconText       string
		iconBg                  string
		openMethod              int
		sort                    int
	}
	type pendingGroup struct {
		title string
		sort  int
		items []pendingItem
	}

	pending := []pendingGroup{}
	for _, g := range export.Icons {
		groupTitle := strings.TrimSpace(g.Title)
		if groupTitle == "" {
			groupTitle = fmt.Sprintf("导入分组%d", g.Sort)
		}
		pg := pendingGroup{title: groupTitle, sort: g.Sort}
		// 组内按 SunPanel 的 sort 稳定排序，再统一重新编号，
		// 消除重复/空洞的 sort 值造成的并列与稀疏。
		items := make([]sunPanelItem, len(g.Children))
		copy(items, g.Children)
		sort.SliceStable(items, func(i, j int) bool { return items[i].Sort < items[j].Sort })

		// 用独立计数器：idx 会把被跳过的条目也算进去，导致导入项排序从 1 开始。
		imported := 0
		for _, raw := range items {
			title, rawURL, desc, iconType, iconSrc, iconText, iconBg, openMethod, ok, reason := mapItem(raw, opts)
			if !ok {
				report.ItemsSkipped++
				report.Skipped = append(report.Skipped, ImportSkip{Group: groupTitle, Title: strings.TrimSpace(raw.Title), Reason: reason})
				continue
			}
			if reason != "" {
				report.Skipped = append(report.Skipped, ImportSkip{Group: groupTitle, Title: title, Reason: reason})
			}
			pg.items = append(pg.items, pendingItem{
				title: title, url: rawURL, description: desc,
				iconType: iconType, iconSrc: iconSrc, iconText: iconText,
				iconBg: iconBg, openMethod: openMethod, sort: imported,
			})
			imported++
		}
		pending = append(pending, pg)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()

	insertGroup, err := tx.PrepareContext(ctx,
		`INSERT INTO bookmark_groups (title, description, icon, public, slug, domain, cache_seconds, config_json, sort_order) VALUES (?, '', '', 0, ?, '', 300, '', ?)`)
	if err != nil {
		return report, err
	}
	defer insertGroup.Close()

	insertItem, err := tx.PrepareContext(ctx,
		`INSERT INTO bookmarks (group_id, title, url, description, icon_type, icon_src, icon_text, icon_bg_color, open_method, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return report, err
	}
	defer insertItem.Close()

	updateSlug, err := tx.PrepareContext(ctx, `UPDATE bookmark_groups SET slug = ? WHERE id = ?`)
	if err != nil {
		return report, err
	}
	defer updateSlug.Close()

	nextGroupSort := 0
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), -1) + 1 FROM bookmark_groups`).Scan(&nextGroupSort); err != nil {
		return report, err
	}

	for _, pg := range pending {
		groupID := int64(0)
		switch opts.GroupMode {
		case "single":
			if opts.TargetGroupID == 0 {
				return report, fmt.Errorf("single 模式必须提供 target_group_id")
			}
			groupID = opts.TargetGroupID
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM bookmark_groups WHERE id = ?`, groupID).Scan(&exists); err != nil {
				return report, err
			}
			if exists == 0 {
				return report, fmt.Errorf("目标分组 %d 不存在", groupID)
			}
			report.GroupsUsed++
		case "merge":
			err := tx.QueryRowContext(ctx, `SELECT id FROM bookmark_groups WHERE title = ? ORDER BY id LIMIT 1`, pg.title).Scan(&groupID)
			if err == sql.ErrNoRows {
				groupID = 0
			} else if err != nil {
				return report, err
			}
			if groupID != 0 {
				report.GroupsUsed++
			}
		}

		if groupID == 0 {
			res, err := insertGroup.ExecContext(ctx, pg.title, "", nextGroupSort)
			if err != nil {
				return report, err
			}
			groupID, err = res.LastInsertId()
			if err != nil {
				return report, err
			}
			// slug 需在拿到自增 id 后生成，保证唯一且稳定。
			slug := normalizeSlug(pg.title)
			if slug == "" {
				slug = "bookmarks"
			}
			slug = fmt.Sprintf("%s-%d", slug, groupID)
			if _, err := updateSlug.ExecContext(ctx, slug, groupID); err != nil {
				return report, err
			}
			nextGroupSort++
			report.GroupsCreated++
			report.GroupNames = append(report.GroupNames, pg.title)
		}

		// 追加到目标分组末尾。新分组为空时 base 应为 0，否则沿用 (最大值+1)，
		// 避免此前 COALESCE(MAX)+1 在空表上得到 1 造成排序从 1 开始。
		baseSort := 0
		var maxSort sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM bookmarks WHERE group_id = ?`, groupID).Scan(&maxSort); err != nil {
			return report, err
		}
		if maxSort.Valid {
			baseSort = int(maxSort.Int64) + 1
		}
		for _, it := range pg.items {
			if _, err := insertItem.ExecContext(ctx, groupID, it.title, it.url, it.description, it.iconType, it.iconSrc, it.iconText, it.iconBg, it.openMethod, baseSort+it.sort); err != nil {
				return report, err
			}
			report.ItemsImported++
		}
	}

	if err := tx.Commit(); err != nil {
		return report, err
	}
	return report, nil
}
