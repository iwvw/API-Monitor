package vercel

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func (h *Handler) syncVercel(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	opts, err := parseVercelSyncOptions(req, h.Store.Snapshot().Vercel)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	validated, failed := h.validateAccountsForVercelSync(r.Context(), opts.AutoValidate)
	cfgJSON, cfgB64, err := h.exportSyncConfig(req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}
	client := &http.Client{Timeout: 30 * time.Second}
	params := buildVercelParams(opts.TeamID)
	headers := map[string]string{"Authorization": "Bearer " + opts.VercelToken}

	envResp, status, err := vercelRequest(r.Context(), client, http.MethodGet, "https://api.vercel.com/v9/projects/"+opts.ProjectID+"/env", params, headers, nil)
	if err != nil || status != http.StatusOK {
		writeJSON(w, statusOr(status, http.StatusInternalServerError), map[string]any{"detail": "获取环境变量失败"})
		return
	}
	envs, _ := envResp["envs"].([]any)
	status, err = upsertVercelEnv(r.Context(), client, opts.ProjectID, params, headers, envs, "DS2API_CONFIG_JSON", cfgB64)
	if err != nil || (status != http.StatusOK && status != http.StatusCreated) {
		writeJSON(w, statusOr(status, http.StatusInternalServerError), map[string]any{"detail": "更新环境变量失败"})
		return
	}
	savedCreds := h.saveVercelProjectCredentials(r.Context(), client, opts, params, headers, envs)
	credentialsWarning := ""
	if saved, err := h.saveLocalVercelCredentials(opts); err == nil && saved {
		savedCreds = append(savedCreds, "config.vercel")
	} else if err != nil {
		credentialsWarning = "保存 Vercel 凭据到本地配置失败: " + err.Error()
	}
	manual, deployURL := triggerVercelDeployment(r.Context(), client, opts.ProjectID, params, headers)
	_ = h.Store.SetVercelSync(syncHashForJSON(cfgJSON), time.Now().Unix())
	result := map[string]any{"success": true, "validated_accounts": validated}
	if manual {
		result["message"] = "配置已同步到 Vercel，请手动触发重新部署"
		result["manual_deploy_required"] = true
	} else {
		result["message"] = "配置已同步，正在重新部署..."
		result["deployment_url"] = deployURL
	}
	if len(failed) > 0 {
		result["failed_accounts"] = failed
	}
	if len(savedCreds) > 0 {
		result["saved_credentials"] = savedCreds
	}
	if credentialsWarning != "" {
		result["credentials_warning"] = credentialsWarning
	}
	writeJSON(w, http.StatusOK, result)
}

type vercelSyncOptions struct {
	VercelToken  string
	ProjectID    string
	TeamID       string
	AutoValidate bool
	SaveCreds    bool
	UsePreconfig bool
}

func parseVercelSyncOptions(req map[string]any, saved config.VercelConfig) (vercelSyncOptions, error) {
	vercelToken, _ := req["vercel_token"].(string)
	projectID, _ := req["project_id"].(string)
	teamID, _ := req["team_id"].(string)
	autoValidate := true
	if v, ok := req["auto_validate"].(bool); ok {
		autoValidate = v
	}
	saveCreds := true
	if v, ok := req["save_credentials"].(bool); ok {
		saveCreds = v
	}
	usePreconfig := vercelToken == "__USE_PRECONFIG__" || strings.TrimSpace(vercelToken) == ""
	if usePreconfig {
		vercelToken = firstNonEmpty(os.Getenv("VERCEL_TOKEN"), saved.Token)
	}
	if strings.TrimSpace(projectID) == "" {
		projectID = firstNonEmpty(os.Getenv("VERCEL_PROJECT_ID"), saved.ProjectID)
	}
	if strings.TrimSpace(teamID) == "" {
		teamID = firstNonEmpty(os.Getenv("VERCEL_TEAM_ID"), saved.TeamID)
	}
	vercelToken = strings.TrimSpace(vercelToken)
	projectID = strings.TrimSpace(projectID)
	teamID = strings.TrimSpace(teamID)
	if vercelToken == "" || projectID == "" {
		return vercelSyncOptions{}, fmt.Errorf("需要 Vercel Token 和 Project ID")
	}
	return vercelSyncOptions{
		VercelToken:  vercelToken,
		ProjectID:    projectID,
		TeamID:       teamID,
		AutoValidate: autoValidate,
		SaveCreds:    saveCreds,
		UsePreconfig: usePreconfig,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func buildVercelParams(teamID string) url.Values {
	params := url.Values{}
	if strings.TrimSpace(teamID) != "" {
		params.Set("teamId", strings.TrimSpace(teamID))
	}
	return params
}

func (h *Handler) validateAccountsForVercelSync(ctx context.Context, enabled bool) (int, []string) {
	if !enabled {
		return 0, nil
	}
	validated, failed := 0, []string{}
	for _, acc := range h.Store.Snapshot().Accounts {
		if strings.TrimSpace(acc.Token) != "" {
			continue
		}
		token, err := h.DS.Login(ctx, acc)
		if err != nil {
			failed = append(failed, acc.Identifier())
		} else {
			validated++
			_ = h.Store.UpdateAccountToken(acc.Identifier(), token)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return validated, failed
}

func upsertVercelEnv(ctx context.Context, client *http.Client, projectID string, params url.Values, headers map[string]string, envs []any, key, value string) (int, error) {
	existingID := findEnvID(envs, key)
	if existingID != "" {
		_, status, err := vercelRequest(ctx, client, http.MethodPatch, "https://api.vercel.com/v9/projects/"+projectID+"/env/"+existingID, params, headers, map[string]any{"value": value})
		return status, err
	}
	_, status, err := vercelRequest(ctx, client, http.MethodPost, "https://api.vercel.com/v10/projects/"+projectID+"/env", params, headers, map[string]any{
		"key":    key,
		"value":  value,
		"type":   "encrypted",
		"target": []string{"production", "preview"},
	})
	return status, err
}

func (h *Handler) saveVercelProjectCredentials(ctx context.Context, client *http.Client, opts vercelSyncOptions, params url.Values, headers map[string]string, envs []any) []string {
	if !opts.SaveCreds || opts.UsePreconfig {
		return nil
	}
	saved := []string{}
	creds := [][2]string{{"VERCEL_TOKEN", opts.VercelToken}, {"VERCEL_PROJECT_ID", opts.ProjectID}}
	if opts.TeamID != "" {
		creds = append(creds, [2]string{"VERCEL_TEAM_ID", opts.TeamID})
	}
	for _, kv := range creds {
		status, _ := upsertVercelEnv(ctx, client, opts.ProjectID, params, headers, envs, kv[0], kv[1])
		if status == http.StatusOK || status == http.StatusCreated {
			saved = append(saved, kv[0])
		}
	}
	return saved
}

func (h *Handler) saveLocalVercelCredentials(opts vercelSyncOptions) (bool, error) {
	if !opts.SaveCreds {
		return false, nil
	}
	err := h.Store.Update(func(c *config.Config) error {
		token := opts.VercelToken
		if opts.UsePreconfig {
			token = c.Vercel.Token
		}
		c.Vercel = config.NormalizeVercelConfig(config.VercelConfig{
			Token:     token,
			ProjectID: opts.ProjectID,
			TeamID:    opts.TeamID,
		})
		return nil
	})
	return err == nil, err
}

func triggerVercelDeployment(ctx context.Context, client *http.Client, projectID string, params url.Values, headers map[string]string) (bool, string) {
	projectResp, status, _ := vercelRequest(ctx, client, http.MethodGet, "https://api.vercel.com/v9/projects/"+projectID, params, headers, nil)
	if status != http.StatusOK {
		return true, ""
	}
	link, ok := projectResp["link"].(map[string]any)
	if !ok {
		return true, ""
	}
	linkType, _ := link["type"].(string)
	if linkType != "github" {
		return true, ""
	}
	repoID := intFrom(link["repoId"])
	ref, _ := link["productionBranch"].(string)
	if ref == "" {
		ref = "main"
	}
	depResp, depStatus, _ := vercelRequest(ctx, client, http.MethodPost, "https://api.vercel.com/v13/deployments", params, headers, map[string]any{
		"name":    projectID,
		"project": projectID,
		"target":  "production",
		"gitSource": map[string]any{
			"type":   "github",
			"repoId": repoID,
			"ref":    ref,
		},
	})
	if depStatus != http.StatusOK && depStatus != http.StatusCreated {
		return true, ""
	}
	deployURL, _ := depResp["url"].(string)
	return false, deployURL
}

func (h *Handler) vercelStatus(w http.ResponseWriter, r *http.Request) {
	snap := h.Store.Snapshot()
	current := h.computeSyncHash()
	synced := snap.VercelSyncHash != "" && snap.VercelSyncHash == current
	draftHash := ""
	draftDiffers := false
	if r != nil && r.Method == http.MethodPost && r.Body != nil {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			if cfgJSON, _, err := h.exportSyncConfig(req); err == nil {
				draftHash = syncHashForJSON(cfgJSON)
				draftDiffers = draftHash != "" && draftHash != current
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"synced":            synced,
		"last_sync_time":    nilIfZero(snap.VercelSyncTime),
		"has_synced_before": snap.VercelSyncHash != "",
		"env_backed":        h.Store.IsEnvBacked(),
		"config_hash":       current,
		"last_synced_hash":  snap.VercelSyncHash,
		"draft_hash":        draftHash,
		"draft_differs":     draftDiffers,
	})
}

func (h *Handler) exportSyncConfig(req map[string]any) (string, string, error) {
	override, ok := req["config_override"]
	if !ok || override == nil {
		return encodeVercelSyncConfig(h.Store.Snapshot())
	}
	raw, err := json.Marshal(override)
	if err != nil {
		return "", "", err
	}
	var cfg config.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", "", err
	}
	return encodeVercelSyncConfig(cfg)
}

func encodeVercelSyncConfig(cfg config.Config) (string, string, error) {
	cfg.DropInvalidAccounts()
	cfg.ClearAccountTokens()
	cfg.ClearVercelCredentials()
	cfg.VercelSyncHash = ""
	cfg.VercelSyncTime = 0
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", "", err
	}
	return string(b), base64.StdEncoding.EncodeToString(b), nil
}

func syncHashForJSON(s string) string {
	var cfg config.Config
	if err := json.Unmarshal([]byte(s), &cfg); err != nil {
		return ""
	}
	cfg.VercelSyncHash = ""
	cfg.VercelSyncTime = 0
	cfg.ClearAccountTokens()
	cfg.ClearVercelCredentials()
	b, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	sum := md5.Sum(b)
	return fmt.Sprintf("%x", sum)
}

func vercelRequest(ctx context.Context, client *http.Client, method, endpoint string, params url.Values, headers map[string]string, body any) (map[string]any, int, error) {
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	parsed := map[string]any{}
	_ = json.Unmarshal(b, &parsed)
	if len(parsed) == 0 {
		parsed["raw"] = string(b)
	}
	return parsed, resp.StatusCode, nil
}

func findEnvID(envs []any, key string) string {
	for _, item := range envs {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if k, _ := m["key"].(string); k == key {
			id, _ := m["id"].(string)
			return id
		}
	}
	return ""
}
