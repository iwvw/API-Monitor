package cloudflare

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/emailcode"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

//go:embed email_inbox_worker.js
var emailInboxWorkerScript string

// defaultInboxWorkerName 是收件箱 Worker 的脚本名前缀。
const defaultInboxWorkerName = "email-inbox"

// inboxWorkerName 返回某域名的收件箱 Worker 脚本名。
// Worker 脚本按 zone 独立部署（各域名的续转目标不同，不能共用一个脚本）。
func inboxWorkerName(zoneName string) string {
	slug := strings.ToLower(zoneName)
	slug = strings.NewReplacer(".", "-", "_", "-", " ", "-").Replace(slug)
	slug = strings.Trim(slug, "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if slug == "" {
		return defaultInboxWorkerName
	}
	name := defaultInboxWorkerName + "-" + slug
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.Trim(name, "-")
}

type emailInboxStatus struct {
	Deployed     bool   `json:"deployed"`
	WorkerName   string `json:"workerName"`
	PanelBaseURL string `json:"panelBaseUrl"`
	ForwardTo    string `json:"forwardTo"`
	Strategy     string `json:"strategy"`
}

// 转发策略：决定收件与转发的关系，两者本可独立存在。
const (
	// strategyForward 只用 Cloudflare 原生转发，不部署 Worker、不收件。
	strategyForward = "forward"
	// strategyInboxOnly 只收件进面板，不再向原地址外发。
	strategyInboxOnly = "inbox_only"
	// strategyInboxAndForward 收件进面板，同时续转到原地址。
	strategyInboxAndForward = "inbox_and_forward"
)

func validForwardStrategy(s string) bool {
	switch s {
	case strategyForward, strategyInboxOnly, strategyInboxAndForward:
		return true
	}
	return false
}

// InboxDomains 返回已部署收件箱的域名，供通用收件箱生成收件邮箱使用。
// 实现 emailcode.DomainProvider，让收件箱无需感知 Cloudflare。
func (s *Service) InboxDomains(ctx context.Context) ([]string, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT zone_name FROM cf_email_inboxes WHERE zone_name <> '' ORDER BY zone_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// emailRoutingInbox 管理「邮箱验证码收件箱 Worker」：部署、卸载与状态查询。
// 部署时把内置脚本上传到 Cloudflare、注入回调地址与握手密钥，并将该域名的 catch-all
// 规则指向 Worker；原本的转发目标由 Worker 通过 message.forward 续转，邮件流不中断。
func (s *Service) emailRoutingInbox(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.emailInboxStatus(w, r, accountID)
	case http.MethodPost:
		s.emailInboxDeploy(w, r, auth, cfAccountID, accountID)
	case http.MethodDelete:
		s.emailInboxRemove(w, r, auth, cfAccountID, accountID)
	default:
		response.JSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
	}
}

func (s *Service) emailInboxStatus(w http.ResponseWriter, r *http.Request, accountID string) {
	zoneID := strings.TrimSpace(r.URL.Query().Get("zoneId"))
	if zoneID == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "缺少 zoneId"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "database error"})
		return
	}
	defer db.Close()
	status, err := loadEmailInbox(r.Context(), db, zoneID)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"deployed":     status.Deployed,
		"workerName":   status.WorkerName,
		"panelBaseUrl": status.PanelBaseURL,
		"forwardTo":    status.ForwardTo,
		"strategy":     status.Strategy,
	})
}

func (s *Service) emailInboxDeploy(w http.ResponseWriter, r *http.Request, auth map[string]string, cfAccountID, accountID string) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	zoneID := strings.TrimSpace(stringValue(payload["zoneId"], ""))
	if zoneID == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "缺少 zoneId"})
		return
	}
	panelBaseURL := cleanURL(stringValue(payload["panelBaseUrl"], ""))
	if panelBaseURL == "" {
		panelBaseURL = s.panelBaseURL(r.Context())
	}
	if panelBaseURL == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "缺少面板公网地址，请在「系统设置」填写公共 API 地址或部署时传入"})
		return
	}
	zoneName := s.zoneNameByZoneID(r.Context(), auth, zoneID)
	workerName := strings.TrimSpace(stringValue(payload["workerName"], ""))
	if workerName == "" {
		workerName = inboxWorkerName(zoneName)
	}

	// Email Routing 未启用的 zone 无法建规则，先启用（会自动接管 MX）。
	if err := s.ensureEmailRoutingEnabled(r.Context(), auth, zoneID); err != nil {
		response.JSON(w, http.StatusBadGateway, map[string]interface{}{"error": "启用 Email Routing 失败：" + err.Error()})
		return
	}

	// 转发策略：forward（只转发，不部署 Worker 收件）/ inbox_only（只收件，不再外发）/
	// inbox_and_forward（收件并续转，默认）。收件类策略才会把 catch-all 指向 Worker。
	strategy := strings.TrimSpace(stringValue(payload["strategy"], ""))
	if strategy == "" {
		strategy = strategyInboxAndForward
	}
	if !validForwardStrategy(strategy) {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "无效的转发策略：" + strategy})
		return
	}

	// 记录当前 catch-all 的转发目标，作为 Worker 的续转地址，保持原邮件流不中断。
	// 若已部署过（catch-all 当前指向 Worker），则沿用上次记录的转发目标，避免重复部署时丢失。
	// 部署前先把当前 DB 记录读成快照：重复部署时续转目标沿用旧值；
	// 中途失败也要靠它把 catch-all 与 Worker 恢复原状，避免留下不可回滚的脏状态。
	var prev emailInboxStatus
	if db, dbErr := s.open(r.Context()); dbErr == nil {
		prev, _ = loadEmailInbox(r.Context(), db, zoneID)
		db.Close()
	}
	forwardTo := strings.TrimSpace(stringValue(payload["forwardTo"], ""))
	if forwardTo == "" && prev.Deployed {
		forwardTo = prev.ForwardTo
	}
	if forwardTo == "" {
		forwardTo = s.catchAllForwardTarget(r.Context(), auth, zoneID)
	}

	// 只转发：不部署 Worker，catch-all 直接设回原生转发，并清理既有 Worker 与记录。
	if strategy == strategyForward {
		if forwardTo == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "只转发策略需要提供 forwardTo 目标邮箱"})
			return
		}
		if err := s.setCatchAllAction(r.Context(), auth, zoneID, "forward", forwardTo); err != nil {
			response.JSON(w, http.StatusBadGateway, map[string]interface{}{"error": "设置邮件转发失败：" + err.Error()})
			return
		}
		db, dbErr := s.open(r.Context())
		if dbErr == nil {
			if prev.WorkerName != "" {
				_, _ = s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+cfAccountID+"/workers/scripts/"+prev.WorkerName, auth, nil)
			}
			_ = deleteEmailInbox(r.Context(), db, zoneID)
			db.Close()
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"deployed":  false,
			"forwardTo": forwardTo,
			"strategy":  strategy,
		})
		return
	}

	bindings := []interface{}{
		map[string]interface{}{"type": "plain_text", "name": "PANEL_BASE_URL", "text": panelBaseURL},
		map[string]interface{}{"type": "plain_text", "name": "PANEL_WORKER_SECRET", "text": emailcode.WorkerSecret()},
	}
	// 只有需要续转的策略才注入 FORWARD_TO；inbox_only 下 Worker 不再外发。
	if forwardTo != "" && strategy == strategyInboxAndForward {
		bindings = append(bindings, map[string]interface{}{"type": "plain_text", "name": "FORWARD_TO", "text": forwardTo})
	}
	if _, err := s.putWorkerScript(r.Context(), auth, cfAccountID, workerName, emailInboxWorkerScript, map[string]interface{}{
		"bindings":           bindings,
		"compatibility_date": "2025-01-01",
	}); err != nil {
		response.JSON(w, http.StatusBadGateway, map[string]interface{}{"error": "部署 Worker 失败：" + err.Error()})
		return
	}

	if err := s.setCatchAllAction(r.Context(), auth, zoneID, "worker", workerName); err != nil {
		response.JSON(w, http.StatusBadGateway, map[string]interface{}{"error": "设置邮件路由规则失败：" + err.Error()})
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "database error"})
		return
	}
	defer db.Close()
	if err := upsertEmailInbox(r.Context(), db, zoneID, accountID, zoneName, workerName, panelBaseURL, forwardTo, strategy); err != nil {
		// 回滚已生效的 infra 变更：此刻 Worker 已上传、catch-all 已指向它，
		// 若 DB 无记录，后续 remove 将无法恢复原转发目标。
		s.rollbackInboxDeploy(r.Context(), auth, cfAccountID, zoneID, prev, workerName)
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	// 回读验证：确认 catch-all 已指向该 Worker 且已启用。
	applied, _ := s.catchAllAction(r.Context(), auth, zoneID)
	catchAll, _ := s.catchAllState(r.Context(), auth, zoneID)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"workerName":   workerName,
		"panelBaseUrl": panelBaseURL,
		"forwardTo":    forwardTo,
		"strategy":     strategy,
		"applied":      applied,
		"enabled":      catchAll["enabled"],
	})
}

// rollbackInboxDeploy 补偿部署中途失败已生效的变更：删除本次新 Worker 脚本，
// 并把 catch-all 恢复为部署前的形态（原 Worker / 原转发目标 / 停用丢弃）。
// 全部尽力而为——已尽力把现场还原，不再覆盖要返回给用户的原始错误。
func (s *Service) rollbackInboxDeploy(ctx context.Context, auth map[string]string, cfAccountID, zoneID string, prev emailInboxStatus, newWorkerName string) {
	if newWorkerName != "" {
		_, _ = s.cfRequest(ctx, http.MethodDelete, "/accounts/"+cfAccountID+"/workers/scripts/"+newWorkerName, auth, nil)
	}
	switch {
	case prev.Deployed && prev.WorkerName != "" && prev.WorkerName != newWorkerName:
		_ = s.setCatchAllAction(ctx, auth, zoneID, "worker", prev.WorkerName)
	case prev.ForwardTo != "":
		_ = s.setCatchAllAction(ctx, auth, zoneID, "forward", prev.ForwardTo)
	default:
		_ = s.setCatchAllDisabledDrop(ctx, auth, zoneID)
	}
}

// ensureEmailRoutingEnabled 确保 zone 的 Email Routing 已启用：
// 未启用时 CF 会拒绝创建任何路由规则（含 catch-all）。
func (s *Service) ensureEmailRoutingEnabled(ctx context.Context, auth map[string]string, zoneID string) error {
	payload, err := s.cfRequest(ctx, http.MethodGet, "zones/"+zoneID+"/email/routing", auth, nil)
	if err == nil && boolValue(objectValue(payload["result"])["enabled"]) {
		return nil
	}
	_, err = s.cfRequest(ctx, http.MethodPost, "zones/"+zoneID+"/email/routing/enable", auth, nil)
	return err
}

func (s *Service) emailInboxRemove(w http.ResponseWriter, r *http.Request, auth map[string]string, cfAccountID, accountID string) {
	zoneID := strings.TrimSpace(r.URL.Query().Get("zoneId"))
	if zoneID == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "缺少 zoneId"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "database error"})
		return
	}
	defer db.Close()
	status, err := loadEmailInbox(r.Context(), db, zoneID)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	// 恢复 catch-all：原先转发则恢复转发；否则恢复为 drop 并停用。
	if status.Deployed {
		if status.ForwardTo != "" {
			if err := s.setCatchAllAction(r.Context(), auth, zoneID, "forward", status.ForwardTo); err != nil {
				response.JSON(w, http.StatusBadGateway, map[string]interface{}{"error": "恢复邮件转发失败：" + err.Error()})
				return
			}
		} else if err := s.setCatchAllDisabledDrop(r.Context(), auth, zoneID); err != nil {
			response.JSON(w, http.StatusBadGateway, map[string]interface{}{"error": "恢复邮件路由失败：" + err.Error()})
			return
		}
	}
	// 删除 Worker 脚本（失败不阻断：可能已被手动删除）。
	if status.WorkerName != "" {
		if _, err := s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+cfAccountID+"/workers/scripts/"+status.WorkerName, auth, nil); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
			response.JSON(w, http.StatusBadGateway, map[string]interface{}{"error": "删除 Worker 失败：" + err.Error()})
			return
		}
	}
	if err := deleteEmailInbox(r.Context(), db, zoneID); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// catchAllAction 读取 zone 的 catch-all 规则，返回动作类型与目标值。
func (s *Service) catchAllAction(ctx context.Context, auth map[string]string, zoneID string) (map[string]string, error) {
	state, err := s.catchAllState(ctx, auth, zoneID)
	if err != nil {
		return nil, err
	}
	return map[string]string{"type": state["type"], "value": state["value"]}, nil
}

// catchAllState 读取 zone 的 catch-all 规则完整状态。
func (s *Service) catchAllState(ctx context.Context, auth map[string]string, zoneID string) (map[string]string, error) {
	payload, err := s.cfRequest(ctx, http.MethodGet, "zones/"+zoneID+"/email/routing/rules/catch_all", auth, nil)
	if err != nil {
		return nil, err
	}
	result := objectValue(payload["result"])
	out := map[string]string{"type": "", "value": "", "enabled": "false"}
	actions := arrayValue(result["actions"])
	if len(actions) > 0 {
		a := objectValue(actions[0])
		out["type"] = stringValue(a["type"], "")
		out["value"] = joinStringArray(arrayValue(a["value"]))
	}
	if boolValue(result["enabled"]) {
		out["enabled"] = "true"
	}
	return out, nil
}

// catchAllForwardTarget 返回 catch-all 当前转发目标（仅当动作为 forward 时）。
func (s *Service) catchAllForwardTarget(ctx context.Context, auth map[string]string, zoneID string) string {
	action, err := s.catchAllAction(ctx, auth, zoneID)
	if err != nil {
		return ""
	}
	if action["type"] == "forward" {
		return action["value"]
	}
	return ""
}

// setCatchAllAction 把 zone 的 catch-all 规则设为单动作。
func (s *Service) setCatchAllAction(ctx context.Context, auth map[string]string, zoneID, actionType, value string) error {
	body := map[string]interface{}{
		"matchers": []interface{}{map[string]interface{}{"type": "all"}},
		"actions":  []interface{}{map[string]interface{}{"type": actionType, "value": []string{value}}},
		"enabled":  true,
	}
	_, err := s.cfRequest(ctx, http.MethodPut, "zones/"+zoneID+"/email/routing/rules/catch_all", auth, body)
	return err
}

// setCatchAllDisabledDrop 把 catch-all 恢复为默认的「丢弃且停用」。
func (s *Service) setCatchAllDisabledDrop(ctx context.Context, auth map[string]string, zoneID string) error {
	body := map[string]interface{}{
		"matchers": []interface{}{map[string]interface{}{"type": "all"}},
		"actions":  []interface{}{map[string]interface{}{"type": "drop", "value": []string{}}},
		"enabled":  false,
	}
	_, err := s.cfRequest(ctx, http.MethodPut, "zones/"+zoneID+"/email/routing/rules/catch_all", auth, body)
	return err
}

func (s *Service) zoneNameByZoneID(ctx context.Context, auth map[string]string, zoneID string) string {
	zones, _, err := s.listZones(ctx, auth, url.Values{})
	if err != nil {
		return ""
	}
	return zoneNameByID(zones, zoneID)
}

// panelBaseURL 读取用户设置里的公共 API 地址。
func (s *Service) panelBaseURL(ctx context.Context) string {
	db, err := s.open(ctx)
	if err != nil {
		return ""
	}
	defer db.Close()
	var raw sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT public_api_url FROM user_settings WHERE id = 1`).Scan(&raw); err != nil {
		return ""
	}
	return cleanURL(raw.String)
}

func loadEmailInbox(ctx context.Context, db *sql.DB, zoneID string) (emailInboxStatus, error) {
	var (
		workerName, panelBaseURL string
		forwardTo                sql.NullString
		strategy                 sql.NullString
	)
	err := db.QueryRowContext(ctx, `SELECT worker_name, panel_base_url, forward_to, strategy FROM cf_email_inboxes WHERE zone_id = ?`, zoneID).
		Scan(&workerName, &panelBaseURL, &forwardTo, &strategy)
	if errors.Is(err, sql.ErrNoRows) {
		return emailInboxStatus{}, nil
	}
	if err != nil {
		return emailInboxStatus{}, err
	}
	out := emailInboxStatus{
		Deployed:     true,
		WorkerName:   workerName,
		PanelBaseURL: panelBaseURL,
		ForwardTo:    forwardTo.String,
		Strategy:     strategy.String,
	}
	if out.Strategy == "" {
		// 旧记录无策略字段：按是否配置续转推断。
		if out.ForwardTo != "" {
			out.Strategy = strategyInboxAndForward
		} else {
			out.Strategy = strategyInboxOnly
		}
	}
	return out, nil
}

func upsertEmailInbox(ctx context.Context, db *sql.DB, zoneID, accountID, zoneName, workerName, panelBaseURL, forwardTo, strategy string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO cf_email_inboxes (zone_id, account_id, zone_name, worker_name, panel_base_url, forward_to, strategy, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(zone_id) DO UPDATE SET account_id=excluded.account_id, zone_name=excluded.zone_name, worker_name=excluded.worker_name, panel_base_url=excluded.panel_base_url, forward_to=excluded.forward_to, strategy=excluded.strategy, updated_at=CURRENT_TIMESTAMP`,
		zoneID, accountID, zoneName, workerName, panelBaseURL, nullEmptyString(forwardTo), strategy)
	return err
}

func deleteEmailInbox(ctx context.Context, db *sql.DB, zoneID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM cf_email_inboxes WHERE zone_id = ?`, zoneID)
	return err
}

func nullEmptyString(v string) interface{} {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
