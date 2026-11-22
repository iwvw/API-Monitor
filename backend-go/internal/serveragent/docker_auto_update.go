package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/robfig/cron/v3"
)

// DockerAutoUpdatePolicy 是容器自动更新的全局策略(单行表 docker_auto_update_policy)。
// 默认 enabled=false,即无人值守写操作默认关闭;开启后按站点时区的 cron 计划执行。
type DockerAutoUpdatePolicy struct {
	Enabled                    bool   `json:"enabled"`
	Schedule                   string `json:"schedule"`
	HostSelector               string `json:"hostSelector"`
	ContainerLabel             string `json:"containerLabel"`
	ContainerNames             string `json:"containerNames"`
	IncludeCompose             bool   `json:"includeCompose"`
	MaxConcurrency             int    `json:"maxConcurrency"`
	PerContainerTimeoutSeconds int    `json:"perContainerTimeoutSeconds"`
	NotifyOnFailure            bool   `json:"notifyOnFailure"`
}

const (
	dockerAutoUpdateDefaultSchedule     = "0 4 * * *"
	dockerAutoUpdateDefaultConcurrency  = 2
	dockerAutoUpdateMaxConcurrency      = 10
	dockerAutoUpdateDefaultTimeoutSec   = 600
	dockerAutoUpdateMinTimeoutSec       = 60
	dockerAutoUpdateMaxTimeoutSec       = 3600
	dockerAutoUpdateContainerListBudget = 30 * time.Second
	dockerAutoUpdateCheckBudget         = 180 * time.Second
)

// dockerAutoUpdateRunGuard 防止自动更新任务重叠执行(定时触发与手动触发共用同一把闸)。
var dockerAutoUpdateRunGuard atomic.Bool

func validateDockerAutoUpdateSchedule(schedule string) error {
	trimmed := strings.TrimSpace(schedule)
	if trimmed == "" {
		return nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 6 {
		if fields[0] != "0" {
			return fmt.Errorf("带秒的表达式仅支持秒段为 0")
		}
		trimmed = strings.Join(fields[1:], " ")
	}
	if len(fields) != 5 {
		return fmt.Errorf("需要 5 段 Cron 表达式")
	}
	_, err := cron.ParseStandard(trimmed)
	return err
}

func defaultDockerAutoUpdatePolicy() DockerAutoUpdatePolicy {
	return DockerAutoUpdatePolicy{
		Enabled:                    false,
		Schedule:                   dockerAutoUpdateDefaultSchedule,
		HostSelector:               "",
		ContainerLabel:             "",
		ContainerNames:             "",
		IncludeCompose:             false,
		MaxConcurrency:             dockerAutoUpdateDefaultConcurrency,
		PerContainerTimeoutSeconds: dockerAutoUpdateDefaultTimeoutSec,
		NotifyOnFailure:            true,
	}
}

func normalizeDockerAutoUpdatePolicy(p DockerAutoUpdatePolicy) DockerAutoUpdatePolicy {
	p.Schedule = strings.TrimSpace(p.Schedule)
	if p.Schedule == "" {
		p.Schedule = dockerAutoUpdateDefaultSchedule
	}
	if p.MaxConcurrency <= 0 {
		p.MaxConcurrency = dockerAutoUpdateDefaultConcurrency
	}
	if p.MaxConcurrency > dockerAutoUpdateMaxConcurrency {
		p.MaxConcurrency = dockerAutoUpdateMaxConcurrency
	}
	if p.PerContainerTimeoutSeconds <= 0 {
		p.PerContainerTimeoutSeconds = dockerAutoUpdateDefaultTimeoutSec
	}
	if p.PerContainerTimeoutSeconds < dockerAutoUpdateMinTimeoutSec {
		p.PerContainerTimeoutSeconds = dockerAutoUpdateMinTimeoutSec
	}
	if p.PerContainerTimeoutSeconds > dockerAutoUpdateMaxTimeoutSec {
		p.PerContainerTimeoutSeconds = dockerAutoUpdateMaxTimeoutSec
	}
	p.HostSelector = strings.TrimSpace(p.HostSelector)
	p.ContainerLabel = strings.TrimSpace(p.ContainerLabel)
	p.ContainerNames = strings.TrimSpace(p.ContainerNames)
	return p
}

func loadDockerAutoUpdatePolicy(ctx context.Context, db *sql.DB) (DockerAutoUpdatePolicy, error) {
	policy := defaultDockerAutoUpdatePolicy()
	var raw sql.NullString
	err := db.QueryRowContext(ctx, `SELECT config_json FROM docker_auto_update_policy WHERE id = 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	if raw.Valid && strings.TrimSpace(raw.String) != "" {
		_ = json.Unmarshal([]byte(raw.String), &policy)
	}
	return normalizeDockerAutoUpdatePolicy(policy), nil
}

func saveDockerAutoUpdatePolicy(ctx context.Context, db *sql.DB, policy DockerAutoUpdatePolicy) error {
	policy = normalizeDockerAutoUpdatePolicy(policy)
	encoded, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO docker_auto_update_policy (id, config_json, updated_at)
		VALUES (1, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET config_json = excluded.config_json, updated_at = datetime('now')
	`, string(encoded))
	return err
}

// SetAutoUpdateScheduleHook 注册策略变更后的调度刷新回调(由 server 组装层接入 cronjobs)。
func (s *Service) SetAutoUpdateScheduleHook(hook func()) {
	s.autoUpdateHookMu.Lock()
	s.autoUpdateScheduleHook = hook
	s.autoUpdateHookMu.Unlock()
}

func (s *Service) notifyAutoUpdateScheduleChanged() {
	s.autoUpdateHookMu.Lock()
	hook := s.autoUpdateScheduleHook
	s.autoUpdateHookMu.Unlock()
	if hook != nil {
		hook()
	}
}

// DockerAutoUpdateEnabled / DockerAutoUpdateSchedule 供 cronjobs 内置调度读取当前策略。
func (s *Service) DockerAutoUpdateEnabled(ctx context.Context) bool {
	db, err := s.open(ctx)
	if err != nil {
		return false
	}
	defer db.Close()
	policy, err := loadDockerAutoUpdatePolicy(ctx, db)
	if err != nil {
		return false
	}
	return policy.Enabled
}

func (s *Service) DockerAutoUpdateSchedule(ctx context.Context) string {
	db, err := s.open(ctx)
	if err != nil {
		return dockerAutoUpdateDefaultSchedule
	}
	defer db.Close()
	policy, err := loadDockerAutoUpdatePolicy(ctx, db)
	if err != nil {
		return dockerAutoUpdateDefaultSchedule
	}
	return policy.Schedule
}

// DockerAutoUpdateContainerResult 是单容器一次自动更新的结果(落库供容器表展示)。
type DockerAutoUpdateContainerResult struct {
	ServerID      string `json:"serverId"`
	ServerName    string `json:"serverName"`
	ContainerID   string `json:"containerId"`
	ContainerName string `json:"containerName"`
	Image         string `json:"image"`
	Status        string `json:"status"`
	Message       string `json:"message"`
	UpdatedAt     string `json:"updatedAt"`
}

// DockerAutoUpdateSummary 是一次自动更新运行的汇总。
type DockerAutoUpdateSummary struct {
	StartedAt  string                            `json:"startedAt"`
	FinishedAt string                            `json:"finishedAt"`
	Status     string                            `json:"status"`
	Hosts      int                               `json:"hosts"`
	Scanned    int                               `json:"scanned"`
	Updated    int                               `json:"updated"`
	UpToDate   int                               `json:"upToDate"`
	Skipped    int                               `json:"skipped"`
	Failed     int                               `json:"failed"`
	Message    string                            `json:"message"`
	Results    []DockerAutoUpdateContainerResult `json:"results"`
}

type dockerAutoUpdateHost struct {
	ID   string
	Name string
}

type dockerImageUpdateStatus struct {
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Image         string `json:"image"`
	HasUpdate     bool   `json:"has_update"`
	Skipped       bool   `json:"skipped"`
	Error         string `json:"error"`
}

// StartDockerAutoUpdateRun 在后台启动一次自动更新,返回是否成功启动(已在运行则返回 false)。
func (s *Service) StartDockerAutoUpdateRun(manual bool) bool {
	if !dockerAutoUpdateRunGuard.CompareAndSwap(false, true) {
		return false
	}
	go func() {
		defer dockerAutoUpdateRunGuard.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		_, _ = s.RunDockerAutoUpdate(ctx, manual)
	}()
	return true
}

// RunDockerAutoUpdate 执行一次自动更新:逐主机检测命中策略的容器,拉取 + 重建有更新的容器。
// manual=true 时即使总开关关闭也按当前策略执行(用户主动触发)。
func (s *Service) RunDockerAutoUpdate(ctx context.Context, manual bool) (DockerAutoUpdateSummary, error) {
	kickoff := time.Now().UTC()
	summary := DockerAutoUpdateSummary{
		StartedAt: kickoff.Format(time.RFC3339),
		Results:   []DockerAutoUpdateContainerResult{},
	}

	db, err := s.open(ctx)
	if err != nil {
		return summary, err
	}
	defer db.Close()

	policy, err := loadDockerAutoUpdatePolicy(ctx, db)
	if err != nil {
		return summary, err
	}
	if !policy.Enabled && !manual {
		summary.Status = "disabled"
		summary.Message = "容器自动更新未开启"
		return summary, nil
	}

	hosts, err := s.dockerAutoUpdateHosts(ctx, db, policy.HostSelector)
	if err != nil {
		return summary, err
	}
	summary.Hosts = len(hosts)
	if len(hosts) == 0 {
		summary.Status = "skipped"
		summary.Message = "没有匹配的在线主机"
		s.persistDockerAutoUpdateRun(ctx, db, summary)
		return summary, nil
	}

	concurrency := policy.MaxConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	timeout := time.Duration(policy.PerContainerTimeoutSeconds) * time.Second

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		sem      = make(chan struct{}, concurrency)
		results  []DockerAutoUpdateContainerResult
		runError []string
	)

	record := func(res DockerAutoUpdateContainerResult) {
		mu.Lock()
		results = append(results, res)
		mu.Unlock()
	}

	for _, host := range hosts {
		containers, err := s.dockerAutoUpdateContainers(host.ID)
		if err != nil {
			runError = append(runError, fmt.Sprintf("%s: 容器列表失败: %v", host.Name, err))
			continue
		}
		candidates := filterDockerAutoUpdateCandidates(containers, policy)
		if len(candidates) == 0 {
			continue
		}

		statuses, err := s.dockerAutoUpdateCheck(host.ID)
		if err != nil {
			runError = append(runError, fmt.Sprintf("%s: 检测更新失败: %v", host.Name, err))
			continue
		}

		for _, c := range candidates {
			st := findDockerUpdateStatus(statuses, c.Name, c.ID)
			base := DockerAutoUpdateContainerResult{
				ServerID:      host.ID,
				ServerName:    host.Name,
				ContainerID:   c.ID,
				ContainerName: c.Name,
				Image:         c.Image,
			}
			switch {
			case st == nil:
				base.Status = "skipped"
				base.Message = "未返回检测结果"
				base.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				record(base)
			case st.Skipped:
				base.Status = "skipped"
				base.Message = "无外部镜像地址,跳过"
				base.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				record(base)
			case st.Error != "":
				base.Status = "failed"
				base.Message = st.Error
				base.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				record(base)
			case !st.HasUpdate:
				base.Status = "up-to-date"
				base.Message = "已是最新"
				base.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				record(base)
			default:
				wg.Add(1)
				sem <- struct{}{}
				go func(candidate dockerAutoUpdateCandidate, seed DockerAutoUpdateContainerResult) {
					defer wg.Done()
					defer func() { <-sem }()
					msg, err := s.dockerAutoUpdateContainer(host.ID, candidate, timeout)
					if err != nil {
						seed.Status = "failed"
						seed.Message = err.Error()
					} else {
						seed.Status = "updated"
						seed.Message = msg
					}
					seed.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					record(seed)
				}(c, base)
			}
		}
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		if results[i].ServerName != results[j].ServerName {
			return results[i].ServerName < results[j].ServerName
		}
		return results[i].ContainerName < results[j].ContainerName
	})
	summary.Results = results
	for _, res := range results {
		switch res.Status {
		case "updated":
			summary.Updated++
		case "up-to-date":
			summary.UpToDate++
		case "failed":
			summary.Failed++
		default:
			summary.Skipped++
		}
		summary.Scanned++
	}

	switch {
	case summary.Failed > 0 && summary.Updated > 0:
		summary.Status = "partial"
	case summary.Failed > 0:
		summary.Status = "failed"
	case summary.Updated > 0:
		summary.Status = "success"
	default:
		summary.Status = "success"
	}
	if len(runError) > 0 {
		summary.Message = strings.Join(runError, "; ")
		if summary.Status == "success" {
			summary.Status = "partial"
		}
	}

	summary.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	s.persistDockerAutoUpdateRun(ctx, db, summary)
	if policy.NotifyOnFailure {
		s.notifyDockerAutoUpdateFailures(ctx, summary)
	}
	return summary, nil
}

type dockerAutoUpdateCandidate struct {
	ID     string
	Name   string
	Image  string
	Labels map[string]string
}

func (s *Service) dockerAutoUpdateHosts(ctx context.Context, db *sql.DB, selector string) ([]dockerAutoUpdateHost, error) {
	want := parseSelectorLabels(selector)
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, COALESCE(tags, '[]') FROM server_accounts
		ORDER BY order_index ASC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []dockerAutoUpdateHost
	for rows.Next() {
		var id, name, tags string
		if err := rows.Scan(&id, &name, &tags); err != nil {
			return nil, err
		}
		if len(want) > 0 && !matchAnyLabel(parseJSONTags(tags), want) {
			continue
		}
		if _, online := s.registry.Get(id); !online {
			continue
		}
		hosts = append(hosts, dockerAutoUpdateHost{ID: id, Name: name})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hosts, nil
}

func (s *Service) dockerAutoUpdateContainers(serverID string) ([]map[string]interface{}, error) {
	raw, err := s.runAgentTaskAndWaitTransient(serverID, 27, "", dockerAutoUpdateContainerListBudget)
	if err != nil {
		return nil, err
	}
	var parsed []interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("解析容器列表失败: %w", err)
	}
	return asMapList(parsed), nil
}

func (s *Service) dockerAutoUpdateCheck(serverID string) ([]dockerImageUpdateStatus, error) {
	raw, err := s.runAgentTaskAndWaitTransient(serverID, 11, "", dockerAutoUpdateCheckBudget)
	if err != nil {
		return nil, err
	}
	var statuses []dockerImageUpdateStatus
	if err := json.Unmarshal([]byte(raw), &statuses); err != nil {
		return nil, fmt.Errorf("解析检测结果失败: %w", err)
	}
	return statuses, nil
}

func (s *Service) dockerAutoUpdateContainer(serverID string, c dockerAutoUpdateCandidate, timeout time.Duration) (string, error) {
	payload, err := json.Marshal(map[string]interface{}{
		"container_id":   c.ID,
		"container_name": c.Name,
		"image":          c.Image,
	})
	if err != nil {
		return "", err
	}
	out, err := s.runAgentTaskAndWaitTransient(serverID, 24, string(payload), timeout)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "更新完成", nil
	}
	return out, nil
}

// filterDockerAutoUpdateCandidates 按策略从容器列表里筛出可更新的候选容器。
func filterDockerAutoUpdateCandidates(containers []map[string]interface{}, policy DockerAutoUpdatePolicy) []dockerAutoUpdateCandidate {
	nameTokens := parseSelectorLabels(policy.ContainerNames)
	labelKey, labelValue, hasLabel := parseContainerLabel(policy.ContainerLabel)

	var out []dockerAutoUpdateCandidate
	for _, raw := range containers {
		name, _ := raw["name"].(string)
		if name == "" {
			continue
		}
		state, _ := raw["state"].(string)
		if state != "" && state != "running" {
			continue
		}
		image, _ := raw["image"].(string)
		if image == "" || strings.HasPrefix(image, "sha256:") {
			continue
		}
		labels := containerLabelMap(raw)

		if hasLabel {
			if !matchContainerLabel(labels, labelKey, labelValue) {
				continue
			}
		}
		if len(nameTokens) > 0 && !matchContainerName(name, nameTokens) {
			continue
		}
		if !policy.IncludeCompose && labels["com.docker.compose.project"] != "" {
			continue
		}

		id, _ := raw["id"].(string)
		out = append(out, dockerAutoUpdateCandidate{ID: id, Name: name, Image: image, Labels: labels})
	}
	return out
}

func containerLabelMap(raw map[string]interface{}) map[string]string {
	out := map[string]string{}
	labels, ok := raw["labels"].(map[string]interface{})
	if !ok {
		return out
	}
	for k, v := range labels {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func matchContainerLabel(labels map[string]string, key, value string) bool {
	if key == "" {
		return true
	}
	got, ok := labels[key]
	if !ok {
		return false
	}
	return value == "" || got == value
}

func parseContainerLabel(spec string) (key, value string, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", false
	}
	if idx := strings.Index(spec, "="); idx >= 0 {
		return strings.TrimSpace(spec[:idx]), strings.TrimSpace(spec[idx+1:]), true
	}
	return spec, "", true
}

func matchContainerName(name string, tokens []string) bool {
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if name == token || strings.Contains(name, token) {
			return true
		}
	}
	return false
}

func findDockerUpdateStatus(statuses []dockerImageUpdateStatus, name, id string) *dockerImageUpdateStatus {
	if name != "" {
		for i := range statuses {
			if statuses[i].ContainerName == name {
				return &statuses[i]
			}
		}
	}
	for i := range statuses {
		if id == "" {
			break
		}
		if statuses[i].ContainerID == id || strings.HasPrefix(statuses[i].ContainerID, id) {
			return &statuses[i]
		}
	}
	return nil
}

func (s *Service) persistDockerAutoUpdateRun(ctx context.Context, db *sql.DB, summary DockerAutoUpdateSummary) {
	encoded, err := json.Marshal(summary)
	if err != nil {
		return
	}
	_, _ = db.ExecContext(ctx, `
		UPDATE docker_auto_update_policy
		SET last_run_at = datetime('now'), last_run_status = ?, last_run_summary = ?
		WHERE id = 1
	`, summary.Status, string(encoded))

	for _, res := range summary.Results {
		_, _ = db.ExecContext(ctx, `
			INSERT INTO docker_auto_update_results (server_id, container_id, container_name, image, status, message, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
			ON CONFLICT(server_id, container_id) DO UPDATE SET
				container_name = excluded.container_name,
				image = excluded.image,
				status = excluded.status,
				message = excluded.message,
				updated_at = datetime('now')
		`, res.ServerID, res.ContainerID, res.ContainerName, res.Image, res.Status, res.Message)
	}
}

func (s *Service) notifyDockerAutoUpdateFailures(ctx context.Context, summary DockerAutoUpdateSummary) {
	if s.notifier == nil {
		return
	}
	for _, res := range summary.Results {
		if res.Status != "failed" {
			continue
		}
		payload := map[string]interface{}{
			"serverId":      res.ServerID,
			"serverName":    res.ServerName,
			"containerId":   res.ContainerID,
			"containerName": res.ContainerName,
			"image":         res.Image,
			"status":        res.Status,
			"message":       res.Message,
			"eventType":     "docker.container.update.failed",
		}
		_ = s.notifier.Trigger(ctx, "docker", "container.update.failed", payload)
	}
}

func (s *Service) loadDockerAutoUpdateResults(ctx context.Context, db *sql.DB, serverID string) ([]DockerAutoUpdateContainerResult, error) {
	query := `SELECT server_id, container_id, container_name, image, status, message, updated_at FROM docker_auto_update_results`
	args := []interface{}{}
	if strings.TrimSpace(serverID) != "" {
		query += ` WHERE server_id = ?`
		args = append(args, serverID)
	}
	query += ` ORDER BY updated_at DESC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DockerAutoUpdateContainerResult{}
	for rows.Next() {
		var res DockerAutoUpdateContainerResult
		var name, image, message sql.NullString
		var updatedAt sql.NullString
		if err := rows.Scan(&res.ServerID, &res.ContainerID, &name, &image, &res.Status, &message, &updatedAt); err != nil {
			return nil, err
		}
		res.ContainerName = name.String
		res.Image = image.String
		res.Message = message.String
		res.UpdatedAt = updatedAt.String
		out = append(out, res)
	}
	return out, rows.Err()
}

func (s *Service) handleDockerAutoUpdateRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 {
		response.Error(w, http.StatusBadRequest, "docker auto-update operation not specified")
		return
	}
	switch subparts[0] {
	case "policy":
		switch r.Method {
		case http.MethodGet:
			s.respondDockerAutoUpdatePolicy(w, r, db)
		case http.MethodPut, http.MethodPost:
			s.saveDockerAutoUpdatePolicyRoute(w, r, db)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case "run":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.StartDockerAutoUpdateRun(true) {
			response.Error(w, http.StatusConflict, "自动更新正在运行中")
			return
		}
		response.JSON(w, http.StatusAccepted, map[string]interface{}{"success": true, "data": map[string]interface{}{"started": true}})
	case "results":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		results, err := s.loadDockerAutoUpdateResults(r.Context(), db, r.URL.Query().Get("serverId"))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": results})
	default:
		response.Error(w, http.StatusNotFound, "docker auto-update route not found")
	}
}

func (s *Service) dockerAutoUpdateStatusPayload(ctx context.Context, db *sql.DB) map[string]interface{} {
	policy, err := loadDockerAutoUpdatePolicy(ctx, db)
	if err != nil {
		policy = defaultDockerAutoUpdatePolicy()
	}
	var lastRunAt, lastRunStatus, lastRunSummary sql.NullString
	_ = db.QueryRowContext(ctx, `SELECT last_run_at, last_run_status, last_run_summary FROM docker_auto_update_policy WHERE id = 1`).
		Scan(&lastRunAt, &lastRunStatus, &lastRunSummary)

	var summary interface{} = map[string]interface{}{}
	if lastRunSummary.Valid && strings.TrimSpace(lastRunSummary.String) != "" {
		var parsed map[string]interface{}
		if json.Unmarshal([]byte(lastRunSummary.String), &parsed) == nil {
			summary = parsed
		}
	}
	return map[string]interface{}{
		"policy":         policy,
		"running":        dockerAutoUpdateRunGuard.Load(),
		"lastRunAt":      nullStringValue(lastRunAt),
		"lastRunStatus":  lastRunStatus.String,
		"lastRunSummary": summary,
	}
}

func nullStringValue(v sql.NullString) interface{} {
	if !v.Valid {
		return nil
	}
	return v.String
}

func (s *Service) respondDockerAutoUpdatePolicy(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": s.dockerAutoUpdateStatusPayload(r.Context(), db)})
}

func (s *Service) saveDockerAutoUpdatePolicyRoute(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var payload DockerAutoUpdatePolicy
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateDockerAutoUpdateSchedule(payload.Schedule); err != nil {
		response.Error(w, http.StatusBadRequest, "cron 表达式无效: "+err.Error())
		return
	}
	if err := saveDockerAutoUpdatePolicy(r.Context(), db, payload); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.notifyAutoUpdateScheduleChanged()
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": s.dockerAutoUpdateStatusPayload(r.Context(), db)})
}
