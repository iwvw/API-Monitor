package qoder

// 本文件实现账号管理：脱敏视图、token 状态、设备登录入口、账号 CRUD、
// 选号与失败冷却，以及余额/签到相关的账号级操作。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/accountpick"
)

// toAccountView 把账号脱敏成可下发前端的视图（不下发任何 token）。
func (s *Service) toAccountView(a Account) AccountView {
	ident := identityForAccount(a)
	view := AccountView{
		ID:            a.ID,
		Nickname:      a.Nickname,
		UserID:        a.UserID,
		Region:        normalizeRegion(a.Region),
		Credits:       a.Credits,
		Disabled:      a.Disabled,
		TokenState:    tokenState(a),
		ExpiresAt:     a.ExpiresAt,
		Available:     accountAvailable(a),
		Cooling:       s.inCooldown(a.ID, time.Now()),
		CallCount:     s.callDisplay(a.ID),
		LastCheckinAt: a.LastCheckinAt,
		LastRefreshAt: a.LastRefreshAt,
		LastError:     a.LastError,
		TokenAgeDays:  -1,
		Fingerprint:   ident.Real,
		ModelsModern:  ident.modernProtocol(),
		NeedsReauth:   needsReauth(a),
	}
	if a.ExpiresAt > 0 {
		if left := a.ExpiresAt - time.Now().Unix(); left > 0 {
			view.ExpiresInSeconds = left
		}
	}
	// TokenAgeDays：以「签发时刻」为基准；未记录签发时刻时为 -1（无法判断）。
	if a.TokenIssuedAt > 0 {
		view.TokenAgeDays = int(time.Since(time.Unix(a.TokenIssuedAt, 0)).Hours() / 24)
	}
	if until, cooled := s.cooldownUntilOf(a.ID, time.Now()); cooled {
		if left := int64(time.Until(until).Seconds()); left > 0 {
			view.CooldownSeconds = left
		}
	}
	return view
}

// accountView 构造单个账号视图并补上该账号的签到统计（单账号接口用）。
func (s *Service) accountView(ctx context.Context, a Account) AccountView {
	view := s.toAccountView(a)
	view.StreakDays, view.TotalCheckinDays, view.TotalCheckinCredits = s.localCheckinStats(ctx, a.ID)
	if recs := s.loadCheckinHistory(ctx, a.ID); len(recs) > 0 {
		view.LastCheckinWindow = recs[len(recs)-1].Date
	}
	return view
}

// accountStats 是一次批量查询得到的签到统计。
type accountStats struct {
	streak   int
	total    int
	credits  float64
	lastDate string
}

// allAccountStats 一次查询取出全部账号的签到统计（避免面板为每个账号各开一次 DB）。
func (s *Service) allAccountStats(ctx context.Context) map[string]accountStats {
	out := map[string]accountStats{}
	db, err := s.open(ctx)
	if err != nil {
		return out
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT account_id, window_date, amount FROM qoder_checkin_history`)
	if err != nil {
		return out
	}
	defer rows.Close()
	grouped := map[string][]checkinRecord{}
	for rows.Next() {
		var id string
		var r checkinRecord
		if rows.Scan(&id, &r.Date, &r.Amount) == nil {
			grouped[id] = append(grouped[id], r)
		}
	}
	for id, recs := range grouped {
		streak, total, credits := calcLocalCheckinStats(recs)
		stat := accountStats{streak: streak, total: total, credits: credits}
		if len(recs) > 0 {
			stat.lastDate = recs[len(recs)-1].Date
		}
		out[id] = stat
	}
	return out
}

// accountViews 批量构造账号视图：签到统计只查一次库。
func (s *Service) accountViews(ctx context.Context, accounts []Account) []AccountView {
	stats := s.allAccountStats(ctx)
	out := make([]AccountView, 0, len(accounts))
	for _, a := range accounts {
		view := s.toAccountView(a)
		if stat, ok := stats[a.ID]; ok {
			view.StreakDays = stat.streak
			view.TotalCheckinDays = stat.total
			view.TotalCheckinCredits = stat.credits
			view.LastCheckinWindow = stat.lastDate
		}
		out = append(out, view)
	}
	return out
}

// needsReauth 报告账号凭据是否需要重新授权：无 access token，
// 或 token 已过期且没有可用的 refresh token。
func needsReauth(a Account) bool {
	if strings.TrimSpace(a.AccessToken) == "" {
		return true
	}
	if tokenState(a) == "expired" && strings.TrimSpace(a.RefreshToken) == "" {
		return true
	}
	return false
}

// tokenState 依据过期时刻给出 token 状态。
func tokenState(a Account) string {
	if a.AccessToken == "" || a.ExpiresAt <= 0 {
		return "unknown"
	}
	left := time.Until(time.Unix(a.ExpiresAt, 0))
	if left <= 0 {
		return "expired"
	}
	if left < tokenExpiringWindow {
		return "expiring"
	}
	return "valid"
}

// accountAvailable 表示账号当前是否可用于转发。
// 无凭据、被停用或 access token 已过期都视为不可用；
// ExpiresAt 缺失（tokenState = unknown）仍视为可用，避免误伤缺少过期时刻的旧凭据。
func accountAvailable(a Account) bool {
	if a.Disabled || a.ID == "" || a.AccessToken == "" {
		return false
	}
	return tokenState(a) != "expired"
}

// -----------------------------------------------------------------------------
// 设备登录入口
// -----------------------------------------------------------------------------

// handleLoginStart 发起设备登录，返回登录 URL 与 loginId。
// 请求体可选 {"region":"cn|intl"}，缺省国内版。
func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Region string `json:"region"`
	}
	// 请求体可空：解析失败按缺省区域处理，不报错。
	_ = json.NewDecoder(r.Body).Decode(&body)
	region := normalizeRegion(body.Region)
	resp, err := s.startLogin(region)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{
		"success":   true,
		"state":     resp.LoginID,
		"url":       resp.VerificationURI,
		"region":    region,
		"expiresAt": time.Now().Add(loginTTL).UTC().Format(time.RFC3339),
	})
}

// handleLoginPoll 单次轮询登录状态；轮询节奏由前端驱动。
func (s *Service) handleLoginPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	state := strings.TrimSpace(body.State)
	if state == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 state"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	acc, done, err := s.pollLogin(ctx, state)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if !done {
		responseJSON(w, map[string]interface{}{"success": true, "status": "pending"})
		return
	}
	s.finishLogin(ctx, w, acc)
}

// finishLogin 落库登录得到的账号并返回脱敏视图。
func (s *Service) finishLogin(ctx context.Context, w http.ResponseWriter, acc Account) {
	if acc.ID == "" {
		acc.ID = strings.TrimSpace(firstNonEmpty(acc.Nickname, acc.UserID))
	}
	if acc.ID == "" {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": "登录返回缺少账号标识"})
		return
	}
	if acc.CreatedAt == "" {
		acc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	// 重新授权必须保留同一账号已有的真实设备指纹（否则国际版签到失效、
	// Sonus/Cantus/Fast 三档模型从清单消失——账号会被上游当成换了设备）。
	if prev, ok := s.findAccount(acc.ID); ok {
		acc.CheckinFingerprint = prev.CheckinFingerprint
		acc.StreakDays = prev.StreakDays
		acc.TotalCheckinDays = prev.TotalCheckinDays
		acc.LastCheckinAt = prev.LastCheckinAt
		if acc.CreatedAt == "" {
			acc.CreatedAt = prev.CreatedAt
		}
	}
	// 登录后立即拉一次额度，让前端马上看到余额。
	if q, err := s.FetchQuota(ctx, acc); err == nil {
		acc.Credits = q.Remaining
	}
	if err := s.upsertAccount(ctx, acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{
		"success": true,
		"status":  "success",
		"account": s.accountView(ctx, acc),
	})
}

// -----------------------------------------------------------------------------
// 账号管理
// -----------------------------------------------------------------------------

// handleAccounts 管理账号列表（登录产生的凭据，故不提供手工新增）。
func (s *Service) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	st := s.Settings()
	out := s.accountViews(r.Context(), st.Accounts)
	responseJSON(w, map[string]interface{}{"success": true, "accounts": out})
}

// handleDeleteAccount 删除账号。
func (s *Service) handleDeleteAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodDelete {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	id = strings.TrimSpace(id)
	if _, ok := s.findAccount(id); !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	if err := s.removeAccount(r.Context(), id); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// handleUpdateAccount 修改账号备注名（昵称）、区域与签到设备指纹。
// region 为空表示不修改；非空时归一化为 cn/intl。
// checkinFingerprint 语义：字段缺省表示不修改；显式 null 表示清空（回落派生指纹）。
func (s *Service) handleUpdateAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPut {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Nickname           *string             `json:"nickname"`
		Region             string              `json:"region"`
		CheckinFingerprint *CheckinFingerprint `json:"checkinFingerprint"`
		// ClearFingerprint 为 true 时显式清空指纹（JSON 里很难表达「显式 null vs 缺省」，
		// 用独立布尔避免误清）。
		ClearFingerprint bool `json:"clearFingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	id = strings.TrimSpace(id)
	acc, ok := s.findAccount(id)
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	if body.Nickname != nil {
		acc.Nickname = strings.TrimSpace(*body.Nickname)
	}
	if strings.TrimSpace(body.Region) != "" {
		acc.Region = normalizeRegion(body.Region)
		acc.Domain = regionConfigFor(acc.Region).website
		// 区域变更后 token 需在新区域重新验证，清空历史错误。
		acc.LastError = ""
	}
	switch {
	case body.ClearFingerprint:
		acc.CheckinFingerprint = nil
	case body.CheckinFingerprint != nil:
		fp := *body.CheckinFingerprint
		fp.MachineID = strings.TrimSpace(fp.MachineID)
		fp.MachineToken = strings.TrimSpace(fp.MachineToken)
		fp.MachineType = strings.TrimSpace(fp.MachineType)
		if fp.MachineToken == "" {
			responseJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "设备指纹至少需要 machine_token（国际版签到与新版推理协议依赖它）",
			})
			return
		}
		acc.CheckinFingerprint = &fp
	}
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "account": s.accountView(r.Context(), acc)})
}

// handleToggleAccount 启用/停用账号（停用即摘出转发与自动刷新）。
func (s *Service) handleToggleAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Disabled bool `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	id = strings.TrimSpace(id)
	acc, ok := s.findAccount(id)
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	acc.Disabled = body.Disabled
	if !body.Disabled {
		acc.LastError = ""
	}
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// handleTestAccount 对指定账号做一次连通性测试（用额度查询作为探针）。
func (s *Service) handleTestAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.ensureToken(ctx, &acc); err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	q, err := s.FetchQuota(ctx, acc)
	if err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc.LastError = ""
	acc.Credits = q.Remaining
	_ = s.upsertAccount(ctx, acc)
	responseJSON(w, map[string]interface{}{"success": true, "credits": q.Remaining, "quota": q})
}

// handleAccountQuota 返回单账号额度明细。
func (s *Service) handleAccountQuota(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.ensureToken(ctx, &acc); err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	q, err := s.FetchQuota(ctx, acc)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc.Credits = q.Remaining
	_ = s.upsertAccount(ctx, acc)
	responseJSON(w, map[string]interface{}{"success": true, "credits": q.Remaining, "quota": q})
}

// handleCheckinAccount 对单账号立即执行一次签到。
func (s *Service) handleCheckinAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	result, err := s.runCheckin(ctx, acc)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "result": result})
}

// handleCheckinAll 立即对全部可用账号执行一次签到。
func (s *Service) handleCheckinAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	results := s.CheckinAll(ctx)
	responseJSON(w, map[string]interface{}{"success": true, "results": results})
}

// runCheckin 执行签到并回写账号状态（余额 + 最近签到时间 + 错误 + 本地连签统计）。
func (s *Service) runCheckin(ctx context.Context, acc Account) (CheckinResult, error) {
	if err := s.ensureToken(ctx, &acc); err != nil {
		s.persistAccountError(ctx, acc, err)
		return CheckinResult{}, err
	}
	result, err := s.DailyCheckin(ctx, acc)
	if err != nil {
		s.persistAccountError(ctx, acc, err)
		return CheckinResult{}, err
	}
	if q, qerr := s.FetchQuota(ctx, acc); qerr == nil {
		acc.Credits = q.Remaining
	}
	acc.LastError = ""
	// 只有真正领取/已领取才更新「最近签到时刻」；none / need_fingerprint 不算签到成功。
	if result.Status == "success" || result.Status == "already" {
		acc.LastCheckinAt = time.Now().UTC().Format(time.RFC3339)
	}
	if result.StreakDays > 0 {
		acc.StreakDays = result.StreakDays
	}
	if result.TotalDays > 0 {
		acc.TotalCheckinDays = result.TotalDays
	}
	_ = s.upsertAccount(ctx, acc)
	return result, nil
}

// persistAccountError 把上游失败原因写回账号。
func (s *Service) persistAccountError(ctx context.Context, acc Account, err error) {
	acc.LastError = err.Error()
	_ = s.upsertAccount(ctx, acc)
}

// handleAccountCheckinHistory 返回单账号的签到历史（最近 N 条 + 统计）。
func (s *Service) handleAccountCheckinHistory(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	recs := s.loadCheckinHistory(r.Context(), acc.ID)
	streak, total, credits := calcLocalCheckinStats(recs)
	// 只回传最近 60 条，避免历史过长把面板撑爆。
	const limit = 60
	if len(recs) > limit {
		recs = recs[len(recs)-limit:]
	}
	out := make([]map[string]interface{}, 0, len(recs))
	for i := len(recs) - 1; i >= 0; i-- {
		out = append(out, map[string]interface{}{"date": recs[i].Date, "amount": recs[i].Amount})
	}
	responseJSON(w, map[string]interface{}{
		"success":      true,
		"accountId":    acc.ID,
		"streakDays":   streak,
		"totalDays":    total,
		"totalCredits": credits,
		"records":      out,
	})
}

// handleAccountRefresh 手动续期该账号 token。body {"force": true} 忽略年龄窗口。
func (s *Service) handleAccountRefresh(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	var body struct {
		Force bool `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	// force 目前无差别续期：手动续期本身就是用户明确要求「现在就换」，
	// 不需要再按 token 年龄过滤（自动续期那条路径才按窗口判断）。
	_ = body.Force
	if strings.TrimSpace(acc.RefreshToken) == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "账号缺少 refresh_token，无法续期，需要重新授权",
		})
		return
	}
	if err := s.refreshAccessToken(ctx, &acc); err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	_ = s.upsertAccount(ctx, acc)
	responseJSON(w, map[string]interface{}{"success": true, "account": s.accountView(r.Context(), acc)})
}

// handleRefreshAll 手动续期全部账号（体量小，逐个同步执行）。
func (s *Service) handleRefreshAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	results := make([]map[string]interface{}, 0)
	for _, a := range s.Settings().Accounts {
		item := map[string]interface{}{"accountId": a.ID, "nickname": a.Nickname}
		acc := a
		if err := s.refreshAccessToken(ctx, &acc); err != nil {
			acc.LastError = err.Error()
			_ = s.upsertAccount(ctx, acc)
			item["success"] = false
			item["error"] = err.Error()
		} else {
			_ = s.upsertAccount(ctx, acc)
			item["success"] = true
		}
		results = append(results, item)
	}
	responseJSON(w, map[string]interface{}{"success": true, "results": results})
}

// -----------------------------------------------------------------------------
// 账号导入导出
// -----------------------------------------------------------------------------

// exportPayload 是账号导出文件的形状（含 token，属敏感数据）。
type exportPayload struct {
	Version    int       `json:"version"`
	ExportedAt string    `json:"exportedAt"`
	Accounts   []Account `json:"accounts"`
}

// handleExportAccounts 导出全部账号（含 token，仅本机会话可下载，用于迁移/备份）。
func (s *Service) handleExportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	st := s.Settings()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=qoder-accounts-%s.json", time.Now().UTC().Format("20060102")))
	_ = json.NewEncoder(w).Encode(exportPayload{
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Accounts:   st.Accounts,
	})
}

// handleImportAccounts 导入账号，按 ID 去重合并（已存在则保留本机版本，不覆盖）。
// 兼容两种形状：{"accounts":[...]} 或裸数组。
func (s *Service) handleImportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	raw, err := decodeImportBody(r)
	if err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if len(raw) == 0 {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "没有可导入的账号"})
		return
	}
	st := s.Settings()
	seen := map[string]bool{}
	for _, a := range st.Accounts {
		seen[a.ID] = true
	}
	added := 0
	for _, a := range raw {
		a.ID = strings.TrimSpace(a.ID)
		if a.ID == "" {
			a.ID = strings.TrimSpace(firstNonEmpty(a.Nickname, a.UserID))
		}
		if a.ID == "" || strings.TrimSpace(a.AccessToken) == "" || seen[a.ID] {
			continue
		}
		if a.CreatedAt == "" {
			a.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		// 区域：显式 region 优先；否则按 domain 自证；都无则默认国内版。
		if strings.TrimSpace(a.Region) == "" {
			a.Region = regionOfDomain(a.Domain)
		}
		a.Region = normalizeRegion(a.Region)
		seen[a.ID] = true
		st.Accounts = append(st.Accounts, a)
		added++
	}
	if err := s.SaveSettings(r.Context(), st); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "added": added})
}

// decodeImportBody 解析导入请求体，同时支持 {"accounts":[...]} 与裸数组。
func decodeImportBody(r *http.Request) ([]Account, error) {
	var probe json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&probe); err != nil {
		return nil, fmt.Errorf("请求体解析失败")
	}
	trimmed := strings.TrimSpace(string(probe))
	if trimmed == "" {
		return nil, fmt.Errorf("请求体为空")
	}
	if strings.HasPrefix(trimmed, "[") {
		var arr []Account
		if err := json.Unmarshal(probe, &arr); err != nil {
			return nil, fmt.Errorf("账号解析失败: %w", err)
		}
		return arr, nil
	}
	var wrapper exportPayload
	if err := json.Unmarshal(probe, &wrapper); err != nil {
		return nil, fmt.Errorf("配置解析失败: %w", err)
	}
	return wrapper.Accounts, nil
}

// -----------------------------------------------------------------------------
// 选号
// -----------------------------------------------------------------------------

// pickAccount 按当前策略选取本次转发的账号。
//
// 两种策略（Settings.AccountStrategy）：
//   - first：列表首个可用账号，其余作主备。
//   - round-robin：从上次位置继续向后轮询，请求均匀分摊。
//
// 两种策略都跳过：不可用（停用/过期/无 token）、本次已尝试、处于冷却期、
// 以及该账号所属区域不提供目标模型（model 为空则不限）的账号。
// 若可用账号全在冷却中，退回「冷却最早结束」的那个继续尝试。
func (s *Service) pickAccount(exclude map[string]bool, model string) (Account, bool) {
	st := s.Settings()
	now := time.Now()
	strategy := normalizeStrategy(st.AccountStrategy)

	cands := make([]accountpick.Candidate, 0, len(st.Accounts))
	byID := make(map[string]Account, len(st.Accounts))
	for i, a := range st.Accounts {
		byID[a.ID] = a
		if exclude[a.ID] || !accountAvailable(a) || s.inCooldown(a.ID, now) {
			continue
		}
		if !s.accountServesModel(a, model) {
			continue
		}
		cands = append(cands, accountpick.Candidate{ID: a.ID, Index: i})
	}

	if len(cands) > 0 {
		switch strategy {
		case accountpick.RoundRobin:
			if c, ok := s.rr.Pick(cands); ok {
				return byID[c.ID], true
			}
		default: // first
			return byID[cands[0].ID], true
		}
	}

	// 全部在冷却中：退回最早恢复的账号。
	bestIdx := -1
	var bestUntil time.Time
	for i, a := range st.Accounts {
		if exclude[a.ID] || !accountAvailable(a) {
			continue
		}
		until, cooled := s.cooldownUntilOf(a.ID, now)
		if !cooled {
			continue
		}
		if bestIdx < 0 || until.Before(bestUntil) {
			bestIdx, bestUntil = i, until
		}
	}
	if bestIdx >= 0 {
		return st.Accounts[bestIdx], true
	}
	return Account{}, false
}

// inCooldown 判断账号是否处于失败冷却期。
func (s *Service) inCooldown(id string, now time.Time) bool {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	until, ok := s.cooldownUntil[id]
	return ok && now.Before(until)
}

// setCooldown 给账号设置失败冷却。
func (s *Service) setCooldown(id string, d time.Duration) {
	if id == "" || d <= 0 {
		return
	}
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	s.cooldownUntil[id] = time.Now().Add(d)
}

// clearCooldown 清除账号冷却（转发成功时调用）。
func (s *Service) clearCooldown(id string) {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	delete(s.cooldownUntil, id)
}

// cooldownUntilOf 读取账号当前冷却的截止时刻；未冷却或已过冷却期返回 ok=false。
func (s *Service) cooldownUntilOf(id string, now time.Time) (time.Time, bool) {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	until, ok := s.cooldownUntil[id]
	if !ok || !now.Before(until) {
		return time.Time{}, false
	}
	return until, true
}
