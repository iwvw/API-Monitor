package geminicli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// 本文件实现「模型目录 + 配额」的上游查询：
//
//   - fetchAvailableModels：拿该账号真实可用的模型及其 quotaInfo（剩余比例、重置时刻）；
//   - retrieveUserQuotaSummary：拿账号级的配额分组（窗口 bucket）。
//
// 两者都要求有效的 project 与 token，并走与转发相同的出网出口（含代理池）。

// ModelQuota 是单个模型的配额信息（quotaInfo）。
type ModelQuota struct {
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
}

// QuotaModel 是上游返回的可用模型条目。
type QuotaModel struct {
	ID            string      `json:"id"`
	DisplayName   string      `json:"displayName,omitempty"`
	ContextLength int64       `json:"contextLength,omitempty"`
	MaxOutput     int64       `json:"maxOutputTokens,omitempty"`
	Quota         *ModelQuota `json:"quota,omitempty"`
}

// QuotaBucket 是账号配额摘要里的一个窗口桶。
type QuotaBucket struct {
	Window            string  `json:"window,omitempty"`
	DisplayName       string  `json:"displayName,omitempty"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
}

// QuotaGroup 是账号配额摘要里的一个分组。
type QuotaGroup struct {
	DisplayName string        `json:"displayName,omitempty"`
	Buckets     []QuotaBucket `json:"buckets"`
}

// AccountQuota 是单个账号的配额快照。
type AccountQuota struct {
	AccountID string       `json:"accountId"`
	Email     string       `json:"email,omitempty"`
	ProjectID string       `json:"projectId,omitempty"`
	Models    []QuotaModel `json:"models"`
	Groups    []QuotaGroup `json:"groups,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// quotaCacheEntry 是配额快照的内存缓存（避免每次刷新都打上游）。
type quotaCacheEntry struct {
	at    time.Time
	quota AccountQuota
}

var quotaCacheTTL = 60 * time.Second

// quotaCacheKey 返回缓存键（账号 ID）。
func quotaCacheKey(id string) string { return "quota:" + id }

// availableModelsURL 返回 fetchAvailableModels 地址。
func availableModelsURL() string {
	return fmt.Sprintf("%s/%s:fetchAvailableModels", codeAssistEndpoint, codeAssistVersion)
}

// quotaSummaryURL 返回 retrieveUserQuotaSummary 地址。
func quotaSummaryURL() string {
	return fmt.Sprintf("%s/%s:retrieveUserQuotaSummary", codeAssistEndpoint, codeAssistVersion)
}

// fetchAvailableModels 拉取账号可用的模型及其配额。
func (s *Service) fetchAvailableModels(ctx context.Context, client *http.Client, acc Account) ([]QuotaModel, error) {
	if strings.TrimSpace(acc.ProjectID) == "" {
		return nil, fmt.Errorf("账号缺少 project_id")
	}
	payload, _ := json.Marshal(map[string]string{"project": acc.ProjectID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, availableModelsURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	codeAssistHeaders(req, acc.AccessToken, defaultUserAgentModel)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetchAvailableModels 返回 %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 300))
	}
	var parsed struct {
		Models map[string]struct {
			DisplayName     string `json:"displayName"`
			MaxTokens       int64  `json:"maxTokens"`
			MaxOutputTokens int64  `json:"maxOutputTokens"`
			QuotaInfo       *struct {
				RemainingFraction float64 `json:"remainingFraction"`
				ResetTime         string  `json:"resetTime"`
			} `json:"quotaInfo"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("解析 fetchAvailableModels 响应失败: %w", err)
	}
	out := make([]QuotaModel, 0, len(parsed.Models))
	for id, m := range parsed.Models {
		m := m
		qm := QuotaModel{ID: id, DisplayName: m.DisplayName, ContextLength: m.MaxTokens, MaxOutput: m.MaxOutputTokens}
		if m.QuotaInfo != nil {
			qm.Quota = &ModelQuota{RemainingFraction: m.QuotaInfo.RemainingFraction, ResetTime: m.QuotaInfo.ResetTime}
		}
		out = append(out, qm)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// retrieveUserQuotaSummary 拉取账号级配额摘要（窗口 bucket）。
func (s *Service) retrieveUserQuotaSummary(ctx context.Context, client *http.Client, acc Account) ([]QuotaGroup, error) {
	if strings.TrimSpace(acc.ProjectID) == "" {
		return nil, fmt.Errorf("账号缺少 project_id")
	}
	payload, _ := json.Marshal(map[string]string{"project": acc.ProjectID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quotaSummaryURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	codeAssistHeaders(req, acc.AccessToken, defaultUserAgentModel)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("retrieveUserQuotaSummary 返回 %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 300))
	}
	var parsed struct {
		Groups []struct {
			DisplayName string `json:"displayName"`
			Buckets     []struct {
				Window            string  `json:"window"`
				DisplayName       string  `json:"displayName"`
				RemainingFraction float64 `json:"remainingFraction"`
				ResetTime         string  `json:"resetTime"`
			} `json:"buckets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("解析 retrieveUserQuotaSummary 响应失败: %w", err)
	}
	out := make([]QuotaGroup, 0, len(parsed.Groups))
	for _, g := range parsed.Groups {
		grp := QuotaGroup{DisplayName: g.DisplayName}
		for _, b := range g.Buckets {
			grp.Buckets = append(grp.Buckets, QuotaBucket{
				Window: b.Window, DisplayName: b.DisplayName,
				RemainingFraction: b.RemainingFraction, ResetTime: b.ResetTime,
			})
		}
		out = append(out, grp)
	}
	return out, nil
}

// quotaForAccount 取单个账号的配额快照（带短 TTL 缓存）。
func (s *Service) quotaForAccount(ctx context.Context, acc Account, force bool) AccountQuota {
	key := quotaCacheKey(acc.ID)
	if !force {
		s.quotaMu.Lock()
		if e, ok := s.quotaCache[key]; ok && time.Since(e.at) < quotaCacheTTL {
			s.quotaMu.Unlock()
			return e.quota
		}
		s.quotaMu.Unlock()
	}
	q := AccountQuota{AccountID: acc.ID, Email: acc.Email, ProjectID: acc.ProjectID}
	a := acc
	if err := s.ensureToken(ctx, &a); err != nil {
		q.Error = err.Error()
	} else {
		client := s.httpClientFor(a.ID)
		if models, err := s.fetchAvailableModels(ctx, client, a); err != nil {
			q.Error = err.Error()
		} else {
			q.Models = models
		}
		if groups, err := s.retrieveUserQuotaSummary(ctx, client, a); err == nil {
			q.Groups = groups
		}
	}
	s.quotaMu.Lock()
	if s.quotaCache == nil {
		s.quotaCache = map[string]quotaCacheEntry{}
	}
	s.quotaCache[key] = quotaCacheEntry{at: time.Now(), quota: q}
	s.quotaMu.Unlock()
	return q
}

// handleQuota 返回配额快照：默认只查第一个可用账号，?all=1 时查全部账号。
func (s *Service) handleQuota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	force := r.URL.Query().Get("refresh") == "1"
	all := r.URL.Query().Get("all") == "1"
	accounts := s.Settings().Accounts
	out := make([]AccountQuota, 0, len(accounts))
	if all {
		for _, a := range accounts {
			if a.Disabled {
				continue
			}
			q := s.quotaForAccount(r.Context(), a, force)
			s.recordQuotaSnapshotFromAccount(q)
			out = append(out, q)
		}
	} else {
		if acc, ok := s.pickAccount(nil); ok {
			q := s.quotaForAccount(r.Context(), acc, force)
			s.recordQuotaSnapshotFromAccount(q)
			out = append(out, q)
		}
	}
	responseJSON(w, map[string]interface{}{"success": true, "accounts": out})
}
