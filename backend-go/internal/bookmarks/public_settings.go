package bookmarks

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// 网址导航「公开页」的全局设置。
//
// 背景属于整个公开页，而不是某个分组：聚合页 /bookmarks/all 会同时展示多个分组，
// 若背景按分组存放，同一页面上就会出现互相冲突的配置。因此改用单行全局配置
// （bookmark_public_settings，id 恒为 1）。
//
// 目前只有一个字段：背景（bgImage/bgColor/bgBlur/bgDim/bgSize/bgFixed）。
// 归一化与校验在 handleUpdatePublicSettings 里做，前端另有同名的纯函数实现
// （src/js/modules/publicPageBackground.js），两侧保持一致的取值域。

type publicSettings struct {
	Config map[string]interface{} `json:"config"`
}

func (s *Service) loadPublicSettings(ctx context.Context, db *sql.DB) (map[string]interface{}, error) {
	var raw sql.NullString
	err := db.QueryRowContext(ctx, `SELECT config_json FROM bookmark_public_settings WHERE id = 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return map[string]interface{}{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !raw.Valid || raw.String == "" {
		return map[string]interface{}{}, nil
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(raw.String), &config); err != nil {
		// 配置损坏时返回空配置而不是报错：公开页仍应能显示网址列表
		return map[string]interface{}{}, nil
	}
	if config == nil {
		return map[string]interface{}{}, nil
	}
	return config, nil
}

// execContexter 覆盖 *sql.DB 与 *sql.Tx 的公共部分。
// 抽出来是为了让公开设置的写入既能单独调用（*sql.DB），
// 也能在导入事务里调用（*sql.Tx），避免同一段 SQL 写两遍。
type execContexter interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func (s *Service) savePublicSettings(ctx context.Context, db *sql.DB, config map[string]interface{}) error {
	return s.savePublicSettingsTx(ctx, db, config)
}

func (s *Service) savePublicSettingsTx(ctx context.Context, ex execContexter, config map[string]interface{}) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = ex.ExecContext(ctx, `
		INSERT INTO bookmark_public_settings (id, config_json, updated_at)
		VALUES (1, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET config_json = excluded.config_json, updated_at = datetime('now')`,
		string(data))
	return err
}

// getPublicSettingsHandler 处理 GET /api/bookmarks/public-settings。
func (s *Service) getPublicSettingsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	config, err := s.loadPublicSettings(ctx, db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to load public settings")
		return
	}
	response.OK(w, map[string]interface{}{"config": config})
}

// updatePublicSettingsHandler 处理 PUT /api/bookmarks/public-settings。
// 只接受已知的背景字段，其余键一律丢弃（避免把任意 JSON 写进库里）。
func (s *Service) updatePublicSettingsHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Config map[string]interface{} `json:"config"`
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

	// 以现有配置为基础做局部更新，缺省字段保持原值（与分组更新同一语义）
	current, err := s.loadPublicSettings(ctx, db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to load public settings")
		return
	}
	merged := sanitizeBackgroundConfig(current, req.Config)

	if err := s.savePublicSettings(ctx, db, merged); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to save public settings")
		return
	}
	response.OK(w, map[string]interface{}{"config": merged})
}

// backgroundConfigKeys 是允许持久化的公开页设置字段白名单。
//
// 注意：这里不只放背景字段，还包含公开页的其它全局外观设置（如 pageWidth）。
// 白名单是「公开页设置」的总闸门，任何新增的全局字段都必须登记在这里，
// 否则 sanitizeBackgroundConfig 会静默丢弃它（前端看起来就是「保存无效」）。
var backgroundConfigKeys = map[string]bool{
	"bgImage": true, "bgColor": true, "bgBlur": true,
	"bgDim": true, "bgSize": true, "bgFixed": true,
	"pageWidth": true,
	// 自定义搜索引擎列表（数组，见 sanitizeSearchEngines）。
	// 由管理页统一配置，公开页对所有访客读取同一份。
	"searchEngines": true,
}

// searchEngineLimits 约束自定义引擎列表的规模。
// 上限存在的意义：这是一份会随公开端点下发给**未登录访客**的数据，
// 不设限就等于允许把任意大的 JSON 塞进公开响应。
const (
	searchEngineMaxCount = 12   // 最多几个引擎
	searchEngineMaxLabel = 24   // 名称最大字符数（按 rune 计）
	searchEngineMaxURL   = 512  // 搜索地址模板最大长度
	searchEngineMaxIcon  = 2048 // 图标最大长度（Iconify 名或图片 URL）
)

// pageWidthValues 是 pageWidth 的合法取值（字符串枚举）。
// 存字符串而不是像素值，便于后期调整各档的实际 px 而不影响已存数据。
var pageWidthValues = map[string]bool{
	"narrow": true, // 窄：适合单列阅读
	"normal": true, // 标准
	"wide":   true, // 宽
	"full":   true, // 铺满视口（默认，与历史行为一致）
}

// sanitizeBackgroundConfig 合并并归一化背景配置：
// 只保留白名单字段，类型/取值范围与前端保持一致。
func sanitizeBackgroundConfig(current, patch map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	// 先保留 current 里合法的键
	for key, value := range current {
		if backgroundConfigKeys[key] {
			out[key] = value
		}
	}
	// 再叠加 patch（同样只认白名单）
	for key, value := range patch {
		if !backgroundConfigKeys[key] {
			continue
		}
		if value == nil || value == "" {
			delete(out, key)
			continue
		}
		out[key] = value
	}

	// 归一化
	if image, ok := out["bgImage"].(string); ok {
		out["bgImage"] = normalizeBackgroundImage(image)
		if out["bgImage"] == "" {
			delete(out, "bgImage")
		}
	} else {
		delete(out, "bgImage")
	}

	if color, ok := out["bgColor"].(string); ok {
		out["bgColor"] = normalizeBackgroundColor(color)
		if out["bgColor"] == "" {
			delete(out, "bgColor")
		}
	} else {
		delete(out, "bgColor")
	}

	if blur, ok := toFloat(out["bgBlur"]); ok {
		out["bgBlur"] = clampFloat(blur, 0, 40)
	} else {
		delete(out, "bgBlur")
	}

	if dim, ok := toFloat(out["bgDim"]); ok {
		out["bgDim"] = clampFloat(dim, 0, 0.9)
	} else {
		delete(out, "bgDim")
	}

	if size, ok := out["bgSize"].(string); ok {
		switch size {
		case "cover", "contain", "auto":
			out["bgSize"] = size
		default:
			delete(out, "bgSize")
		}
	} else {
		delete(out, "bgSize")
	}

	if fixed, ok := out["bgFixed"].(bool); ok {
		out["bgFixed"] = fixed
	} else {
		delete(out, "bgFixed")
	}

	// pageWidth：字符串枚举，非法值丢弃（回落到前端默认的 full）。
	if width, ok := out["pageWidth"].(string); ok {
		if pageWidthValues[width] {
			out["pageWidth"] = width
		} else {
			delete(out, "pageWidth")
		}
	} else {
		delete(out, "pageWidth")
	}

	// searchEngines：自定义搜索引擎列表。
	// 空数组或全部非法时删除该键（公开页回落到内置默认列表）。
	if engines, ok := sanitizeSearchEngines(out["searchEngines"]); ok {
		if len(engines) > 0 {
			out["searchEngines"] = engines
		} else {
			delete(out, "searchEngines")
		}
	} else {
		delete(out, "searchEngines")
	}

	return out
}

// sanitizeSearchEngines 归一化自定义搜索引擎列表。
//
// 每项要求：{ id, label, url, icon, color }
//   - url 必须含 %s 占位符（搜索关键词的注入点），否则该项无意义、丢弃；
//   - 只接受 http/https 的模板，拒绝 javascript: 之类可执行的协议；
//   - label 不能为空（菜单里没有可读名称的项没法用）；
//   - id 缺失时用 url 生成稳定 id，保证前端选中态可持久化。
//
// 返回 (列表, 是否是一组合法输入)。类型不对（非数组）时返回 false，
// 让调用方把该键删掉、回落到内置默认。
func sanitizeSearchEngines(value interface{}) ([]map[string]interface{}, bool) {
	if value == nil {
		return nil, false
	}
	raw, ok := value.([]interface{})
	if !ok {
		return nil, false
	}

	out := make([]map[string]interface{}, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		if len(out) >= searchEngineMaxCount {
			break
		}
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		label := strings.TrimSpace(stringValue(entry["label"]))
		urlTemplate := strings.TrimSpace(stringValue(entry["url"]))
		if label == "" || urlTemplate == "" {
			continue
		}
		if !strings.Contains(urlTemplate, "%s") {
			// 没有 %s 就没法把关键词注入地址，存了也不能用
			continue
		}
		lower := strings.ToLower(urlTemplate)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			// 拒绝 javascript:/data: 等协议，避免公开页出现可执行跳转
			continue
		}
		if utf8.RuneCountInString(label) > searchEngineMaxLabel || len(urlTemplate) > searchEngineMaxURL {
			continue
		}

		icon := strings.TrimSpace(stringValue(entry["icon"]))
		if len(icon) > searchEngineMaxIcon {
			icon = ""
		}
		color := strings.TrimSpace(stringValue(entry["color"]))
		if color != "" && !strings.HasPrefix(color, "#") {
			color = "#" + color
		}

		id := strings.TrimSpace(stringValue(entry["id"]))
		if id == "" {
			id = engineIDFromURL(urlTemplate)
		}
		if id == "" || seen[id] {
			// id 冲突时加后缀，确保唯一（前端用它做 key 与选中态）
			base := id
			if base == "" {
				base = "engine"
			}
			for n := 2; seen[base]; n++ {
				base = id + "-" + strconv.Itoa(n)
			}
			id = base
		}
		seen[id] = true

		out = append(out, map[string]interface{}{
			"id":    id,
			"label": label,
			"url":   urlTemplate,
			"icon":  icon,
			"color": color,
		})
	}
	return out, true
}

// engineIDFromURL 从搜索地址模板生成一个稳定的 id。
// 取主机名并去掉非字母数字，例如 https://www.bing.com/search?q=%s -> bing
func engineIDFromURL(urlTemplate string) string {
	u, err := neturl.Parse(urlTemplate)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	if idx := strings.Index(host, "."); idx > 0 {
		host = host[:idx]
	}
	var b strings.Builder
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func toFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// normalizeBackgroundImage 只接受 http(s) 绝对地址与本地上传路径。
func normalizeBackgroundImage(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "/site-brand-backgrounds/") {
		return value
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	// 相对路径在公开页必然 404，拒绝以免出现空白背景
	return ""
}

// normalizeBackgroundColor 只接受 #rgb / #rrggbb(aa)。
func normalizeBackgroundColor(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "#") {
		value = "#" + value
	}
	for _, r := range value[1:] {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !isHex {
			return ""
		}
	}
	switch len(value) {
	case 4, 5, 7, 9:
		return strings.ToLower(value)
	}
	return ""
}
