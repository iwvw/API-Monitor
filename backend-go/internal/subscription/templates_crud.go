package subscription

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) listTemplates(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	items, err := loadTemplates(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, items)
}

func (s *Service) createTemplate(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var tpl Template
	if !decodeJSON(w, r, &tpl) {
		return
	}
	tpl.Name = strings.TrimSpace(tpl.Name)
	tpl.Format = normalizeTemplateFormat(tpl.Format)
	if tpl.Name == "" || strings.TrimSpace(tpl.Content) == "" {
		response.Error(w, http.StatusBadRequest, "模板名称和内容不能为空")
		return
	}
	if err := validateTemplateDefinition(tpl.Format, tpl.Content); err != nil {
		response.Error(w, http.StatusBadRequest, "模板配置无效: "+err.Error())
		return
	}
	if tpl.ID == "" {
		tpl.ID = randomID("tpl")
	}
	_, err := db.ExecContext(r.Context(), `INSERT INTO subscription_templates (id, name, format, content, builtin, is_default, description) VALUES (?, ?, ?, ?, 0, 0, ?)`,
		tpl.ID, tpl.Name, tpl.Format, tpl.Content, tpl.Description)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	tpl.Valid = true
	response.OK(w, tpl)
}

func (s *Service) updateTemplate(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var tpl Template
	if !decodeJSON(w, r, &tpl) {
		return
	}
	tpl.Name = strings.TrimSpace(tpl.Name)
	tpl.Format = normalizeTemplateFormat(tpl.Format)
	if tpl.Name == "" || strings.TrimSpace(tpl.Content) == "" {
		response.Error(w, http.StatusBadRequest, "模板名称和内容不能为空")
		return
	}
	if err := validateTemplateDefinition(tpl.Format, tpl.Content); err != nil {
		response.Error(w, http.StatusBadRequest, "模板配置无效: "+err.Error())
		return
	}
	result, err := db.ExecContext(r.Context(), `UPDATE subscription_templates SET name = ?, format = ?, content = ?, description = ?, updated_at = datetime('now') WHERE id = ?`,
		tpl.Name, tpl.Format, tpl.Content, tpl.Description, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "模板不存在")
		return
	}
	response.OK(w, map[string]bool{"updated": true})
}

func (s *Service) deleteTemplate(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var builtin int
	if err := db.QueryRowContext(r.Context(), `SELECT builtin FROM subscription_templates WHERE id=?`, id).Scan(&builtin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "模板不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if builtin == 1 {
		response.Error(w, http.StatusConflict, "内置模板不能删除")
		return
	}
	var profiles, subscriptions, defaults int
	for _, dependency := range []struct {
		query string
		count *int
	}{
		{`SELECT COUNT(*) FROM subscription_profiles WHERE template_id=?`, &profiles},
		{`SELECT COUNT(*) FROM subscription_subscriptions WHERE template_id=?`, &subscriptions},
		{`SELECT COUNT(*) FROM subscription_settings WHERE default_template_id=?`, &defaults},
	} {
		if err := db.QueryRowContext(r.Context(), dependency.query, id).Scan(dependency.count); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if profiles+subscriptions+defaults > 0 {
		response.JSON(w, http.StatusConflict, map[string]interface{}{"success": false, "error": "模板仍被引用，请先更换关联模板", "dependencies": map[string]int{"profiles": profiles, "subscriptions": subscriptions, "defaults": defaults}})
		return
	}
	result, err := db.ExecContext(r.Context(), `DELETE FROM subscription_templates WHERE id = ?`, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "模板不存在")
		return
	}
	response.OK(w, map[string]bool{"deleted": true})
}

func (s *Service) setDefaultTemplate(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if err := validateTemplateReference(r.Context(), db, id); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `UPDATE subscription_settings SET default_template_id = ?, updated_at = datetime('now') WHERE id = 1`, id)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE subscription_templates SET is_default = CASE WHEN id = ? THEN 1 ELSE 0 END`, id)
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]bool{"updated": true})
}

func ensureDefaultNodeLibrary(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_profiles (
			id,name,remark,enabled,template_id,traffic_source,ownership,management,traffic_reporting,
			cycle_type,cycle_day,rate_limit_enabled,rate_limit_per_minute,updated_at
		) VALUES (?, '外部节点池', '系统统一外部节点池', 1, ?, 'manual', 'external', 'unmanaged', 'unavailable', 'none', 1, 0, ?, datetime('now'))`,
		defaultNodeLibrary, rawTemplateID, defaultLimitPerMin); err != nil {
		return fmt.Errorf("create default external node pool: %w", err)
	}
	// Older releases represented the node pool as an enabled public
	// subscription. Retire that anchor so it cannot leak a hidden public URL or
	// pollute subscription counts; nodes continue to reference the profile ID.
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_access_logs WHERE subscription_id=?`, defaultNodeLibrary); err != nil {
		return fmt.Errorf("delete legacy node pool access logs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_subscriptions WHERE id=?`, defaultNodeLibrary); err != nil {
		return fmt.Errorf("delete legacy node pool subscription: %w", err)
	}
	return tx.Commit()
}

func ensureBuiltins(ctx context.Context, db *sql.DB, overwrite bool) error {
	defaultTemplate := loadDefaultMihomoTemplate()
	templates := []Template{
		{ID: rawTemplateID, Name: "Raw URI List", Format: "raw", Content: "{{ raw_uri_list }}", Builtin: true, Description: "一行一个节点链接"},
		{ID: base64TemplateID, Name: "Base64 URI List", Format: "base64", Content: "{{ raw_uri_list }}", Builtin: true, Description: "v2rayN 常用 Base64 订阅"},
	}
	if strings.TrimSpace(defaultTemplate) != "" {
		templates = append([]Template{{ID: defaultTemplateID, Name: "默认 Mihomo/Clash YAML", Format: "clash", Content: defaultTemplate, Builtin: true, IsDefault: true, Description: "基于项目默认三网分流配置的 Mihomo 模板"}}, templates...)
	}
	for _, tpl := range templates {
		if overwrite {
			_, err := db.ExecContext(ctx, `INSERT INTO subscription_templates (id, name, format, content, builtin, is_default, description, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
				ON CONFLICT(id) DO UPDATE SET name = excluded.name, format = excluded.format, content = excluded.content, builtin = excluded.builtin, description = excluded.description, updated_at = datetime('now')`,
				tpl.ID, tpl.Name, tpl.Format, tpl.Content, boolToInt(tpl.Builtin), boolToInt(tpl.IsDefault), tpl.Description)
			if err != nil {
				return err
			}
			continue
		}
		_, err := db.ExecContext(ctx, `INSERT INTO subscription_templates (id, name, format, content, builtin, is_default, description)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name=CASE WHEN subscription_templates.builtin=1 THEN excluded.name ELSE subscription_templates.name END,
				format=CASE WHEN subscription_templates.builtin=1 THEN excluded.format ELSE subscription_templates.format END,
				content=CASE WHEN subscription_templates.builtin=1 THEN excluded.content ELSE subscription_templates.content END,
				builtin=CASE WHEN subscription_templates.builtin=1 THEN 1 ELSE subscription_templates.builtin END,
				description=CASE WHEN subscription_templates.builtin=1 THEN excluded.description ELSE subscription_templates.description END,
				updated_at=CASE WHEN subscription_templates.builtin=1 THEN datetime('now') ELSE subscription_templates.updated_at END`,
			tpl.ID, tpl.Name, tpl.Format, tpl.Content, boolToInt(tpl.Builtin), boolToInt(tpl.IsDefault), tpl.Description)
		if err != nil {
			return err
		}
	}
	_, _ = db.ExecContext(ctx, `UPDATE subscription_templates SET is_default = CASE WHEN id = (SELECT default_template_id FROM subscription_settings WHERE id = 1) THEN 1 ELSE 0 END`)
	return nil
}
