package huawei

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// fetchProjects 调用 IAM 项目接口返回全部项目（region 名 + project_id）。
func (s *Service) fetchProjects(ctx context.Context, c *client) ([]normalProject, error) {
	var raw struct {
		Projects []struct {
			DomainID string `json:"domain_id"`
			Name     string `json:"name"`
			ID       string `json:"id"`
		} `json:"projects"`
	}
	if err := c.do(ctx, "iam", http.MethodGet, "/v3/projects", nil, nil, &raw); err != nil {
		return nil, err
	}
	projects := make([]normalProject, 0, len(raw.Projects))
	for _, project := range raw.Projects {
		projects = append(projects, normalProject{
			Name:      project.Name,
			ProjectID: project.ID,
			DomainID:  project.DomainID,
		})
	}
	return projects, nil
}

// verifyAccount 用 AK/SK 调 IAM 项目接口验证账号，返回项目列表与 domain_id。
func (s *Service) verifyAccount(ctx context.Context, account Account) ([]normalProject, string, error) {
	c, err := s.buildClient(account)
	if err != nil {
		return nil, "", err
	}
	projects, err := s.fetchProjects(ctx, c)
	if err != nil {
		return nil, "", err
	}
	domainID := ""
	for _, project := range projects {
		if domainID == "" {
			domainID = project.DomainID
		}
	}
	if len(projects) == 0 {
		return nil, domainID, errors.New("该凭证下未发现可用项目")
	}
	return projects, domainID, nil
}

func (s *Service) buildClient(account Account) (*client, error) {
	if account.SecretAccessKey == "" {
		return nil, errors.New("账号缺少 Secret Access Key")
	}
	return &client{http: s.http, ak: account.AccessKeyID, sk: account.SecretAccessKey, site: account.Site}, nil
}

func (s *Service) projects(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	projects, _, err := s.verifyAccount(r.Context(), account)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "获取项目列表失败："+err.Error())
		return
	}
	response.OK(w, projects)
}

func (s *Service) setDefaults(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid account id")
		return
	}
	var payload defaultPayload
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	if err := updateDefaults(r.Context(), db, id, payload.DefaultRegion, payload.DefaultProjectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "huawei account not found")
			return
		}
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"id": id})
}

// unmarshalRaw 便捷解码 json.RawMessage 到目标结构。
func unmarshalRaw(raw json.RawMessage, out interface{}) error {
	return json.Unmarshal(raw, out)
}
