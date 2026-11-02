package gcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// verifyAccount 用 SA 换取只读 token 后调用 Resource Manager projects.list 确认凭证可用。
func (s *Service) verifyAccount(ctx context.Context, account Account) error {
	c, err := s.buildClient(account, scopeFull)
	if err != nil {
		return err
	}
	_, err = s.listProjects(ctx, c)
	return err
}

// listProjects 调用 Resource Manager projects.list（projectId 查询，聚合分页）。
func (s *Service) listProjects(ctx context.Context, c *client) ([]normalProject, error) {
	query := url.Values{}
	var projects []normalProject
	err := c.listJSON(ctx, http.MethodGet, "crm", "projects", query, "projects", nil, func(raw json.RawMessage) error {
		extra := struct {
			ProjectID  string            `json:"projectId"`
			Name       string            `json:"name"`
			State      string            `json:"lifecycleState"`
			Labels     map[string]string `json:"labels"`
			CreateTime string            `json:"createTime"`
		}{}
		if err := json.Unmarshal(raw, &extra); err != nil {
			return err
		}
		projects = append(projects, normalProject{
			ProjectID:  extra.ProjectID,
			Name:       extra.Name,
			State:      extra.State,
			Labels:     extra.Labels,
			CreateTime: extra.CreateTime,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return projects, nil
}

func (s *Service) projects(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	items, err := s.listProjects(r.Context(), client)
	writeResult(w, map[string]interface{}{"projects": items}, err)
}

func (s *Service) setDefaultProject(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	var payload struct {
		DefaultProjectId string `json:"defaultProjectId"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := setDefaultProject(r.Context(), db, account.ID, payload.DefaultProjectId); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"id": account.ID, "defaultProjectId": payload.DefaultProjectId})
}