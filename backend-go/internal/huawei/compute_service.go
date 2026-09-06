package huawei

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) instances(w http.ResponseWriter, r *http.Request, accountID, projectID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	servers, err := s.listInstancesForProjects(r.Context(), c, projectID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "获取实例列表失败："+err.Error())
		return
	}
	response.OK(w, servers)
}

// listInstancesForProjects projectID 为 "all"（或空）时跨所有项目区域聚合查询，
// 每项带 region 标记；否则仅查指定项目。聚合模式跳过失败区域（不整体 502）。
func (s *Service) listInstancesForProjects(ctx context.Context, c *client, projectID string) ([]normalInstance, error) {
	projects, err := s.fetchProjects(ctx, c)
	if err != nil {
		return nil, err
	}
	if projectID != "all" && projectID != "" {
		for _, project := range projects {
			if project.ProjectID == projectID {
				return listInstances(ctx, regionClient(c, project.Name), projectID)
			}
		}
		return nil, errors.New("未找到指定项目：" + projectID)
	}
	return aggregateProjects(ctx, projects, func(ctx context.Context, project normalProject) ([]normalInstance, error) {
		return listInstances(ctx, regionClient(c, project.Name), project.ProjectID)
	})
}

func listInstances(ctx context.Context, c *client, projectID string) ([]normalInstance, error) {
	path := "/v1/" + projectID + "/cloudservers/detail"
	var raw struct {
		Servers []json.RawMessage `json:"servers"`
	}
	if err := c.do(ctx, "ecs", http.MethodGet, path, nil, nil, &raw); err != nil {
		return nil, err
	}
	instances := make([]normalInstance, 0, len(raw.Servers))
	for _, entry := range raw.Servers {
		instance, err := normalizeInstance(entry, c.region)
		if err != nil {
			continue
		}
		instance.ProjectID = projectID
		instances = append(instances, instance)
	}
	return instances, nil
}

func normalizeInstance(raw json.RawMessage, region string) (normalInstance, error) {
	var server struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		Flavor    struct {
			ID     string    `json:"id"`
			Name   string    `json:"name"`
			VCPUs  flexInt64 `json:"vcpus"`
			RAM    flexInt64 `json:"ram"`
			Disk   flexInt64 `json:"disk"`
		} `json:"flavor"`
		Addresses map[string][]struct {
			Addr    string    `json:"addr"`
			Version flexInt64 `json:"version"`
			Type    string    `json:"OS-EXT-IPS:type"`
		} `json:"addresses"`
		Metadata struct {
			ChargeMode string `json:"charging_mode"`
		} `json:"metadata"`
		Image struct {
			Name string `json:"name"`
		} `json:"image"`
		Created string `json:"created"`
		OrderID string `json:"enterprise_project_id"`
	}
	if err := json.Unmarshal(raw, &server); err != nil {
		return normalInstance{}, err
	}
	instance := normalInstance{
		ID:         server.ID,
		Name:       server.Name,
		Status:     server.Status,
		FlavorID:   server.Flavor.ID,
		FlavorName: server.Flavor.Name,
		VCPUs:      int64(server.Flavor.VCPUs),
		MemoryMB:   int64(server.Flavor.RAM),
		Region:     region,
		ChargeMode: server.Metadata.ChargeMode,
		CreatedAt:  server.Created,
		ImageName:  server.Image.Name,
		OrderID:    server.OrderID,
	}
	for _, networkAddrs := range server.Addresses {
		for _, addr := range networkAddrs {
			if int(addr.Version) != 4 {
				continue
			}
			if addr.Type == "floating" && instance.PublicIP == "" {
				instance.PublicIP = addr.Addr
			}
			if addr.Type != "floating" && instance.PrivateIP == "" {
				instance.PrivateIP = addr.Addr
			}
		}
	}
	return instance, nil
}

func (s *Service) instanceDetail(w http.ResponseWriter, r *http.Request, accountID, projectID, serverID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientWithRegion(r.Context(), w, account, projectID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		path := "/v1/" + projectID + "/cloudservers/" + url.PathEscape(serverID)
		var raw json.RawMessage
		if err := c.do(r.Context(), "ecs", http.MethodGet, path, nil, nil, &raw); err != nil {
			response.Error(w, http.StatusBadGateway, "获取实例详情失败："+err.Error())
			return
		}
		var envelope struct {
			Server json.RawMessage `json:"server"`
		}
		_ = json.Unmarshal(raw, &envelope)
		instance, err := normalizeInstance(envelope.Server, c.region)
		if err != nil {
			response.Error(w, http.StatusBadGateway, "解析实例详情失败："+err.Error())
			return
		}
		response.OK(w, instance)
	case http.MethodDelete:
		query := url.Values{}
		if r.URL.Query().Get("deletePublicIp") == "true" {
			query.Set("delete_publicip", "true")
		}
		if r.URL.Query().Get("deleteVolume") == "true" {
			query.Set("delete_volume", "TRUE")
		}
		path := "/v1/" + projectID + "/cloudservers/" + url.PathEscape(serverID)
		if err := c.do(r.Context(), "ecs", http.MethodDelete, path, query, nil, nil); err != nil {
			response.Error(w, http.StatusBadGateway, "删除实例失败："+err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": serverID})
	case http.MethodPut:
		var payload struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		path := "/v1/" + projectID + "/cloudservers/" + url.PathEscape(serverID)
		body := map[string]interface{}{"server": map[string]interface{}{"name": payload.Name}}
		if err := c.do(r.Context(), "ecs", http.MethodPut, path, nil, body, nil); err != nil {
			response.Error(w, http.StatusBadGateway, "修改实例失败："+err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": serverID})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) instanceAction(w http.ResponseWriter, r *http.Request, accountID, projectID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientWithRegion(r.Context(), w, account, projectID)
	if !ok {
		return
	}
	var payload actionPayload
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(payload.ServerIds) == 0 {
		response.Error(w, http.StatusBadRequest, "请选择要操作的实例")
		return
	}
	jobID, err := runInstanceAction(r.Context(), c, projectID, payload.Action, payload.ServerIds)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "执行实例动作失败："+err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"jobId": jobID, "message": "指令已下发"})
}

func runInstanceAction(ctx context.Context, c *client, projectID, action string, serverIDs []string) (string, error) {
	servers := make([]map[string]interface{}, 0, len(serverIDs))
	for _, id := range serverIDs {
		servers = append(servers, map[string]interface{}{"id": id})
	}
	var body map[string]interface{}
	switch action {
	case "start":
		body = map[string]interface{}{"os-start": map[string]interface{}{"servers": servers}}
	case "stop":
		body = map[string]interface{}{"os-stop": map[string]interface{}{"servers": servers}}
	case "reboot":
		body = map[string]interface{}{"reboot": map[string]interface{}{"servers": servers, "type": "SOFT"}}
	default:
		return "", errInvalidAction(action)
	}
	var result struct {
		JobID string `json:"job_id"`
	}
	if err := c.do(ctx, "ecs", http.MethodPost, "/v1/"+projectID+"/cloudservers/action", nil, body, &result); err != nil {
		return "", err
	}
	return result.JobID, nil
}

func errInvalidAction(action string) error {
	return &invalidActionError{action: action}
}

type invalidActionError struct {
	action string
}

func (e *invalidActionError) Error() string {
	return "不支持的实例动作：" + e.action
}

func (s *Service) instanceResetPassword(w http.ResponseWriter, r *http.Request, accountID, projectID, serverID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientWithRegion(r.Context(), w, account, projectID)
	if !ok {
		return
	}
	var payload struct {
		NewPassword string `json:"newPassword"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if payload.NewPassword == "" {
		response.Error(w, http.StatusBadRequest, "请填写新密码")
		return
	}
	path := "/v1/" + projectID + "/cloudservers/" + url.PathEscape(serverID) + "/os-reset-password"
	body := map[string]interface{}{"new_password": payload.NewPassword}
	if err := c.do(r.Context(), "ecs", http.MethodPost, path, nil, body, nil); err != nil {
		response.Error(w, http.StatusBadGateway, "重置密码失败："+err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"id": serverID})
}

func getServerStatus(ctx context.Context, c *client, projectID, serverID string) (normalInstance, error) {
	path := "/v1/" + projectID + "/cloudservers/" + url.PathEscape(serverID)
	var envelope struct {
		Server json.RawMessage `json:"server"`
	}
	if err := c.do(ctx, "ecs", http.MethodGet, path, nil, nil, &envelope); err != nil {
		return normalInstance{}, err
	}
	return normalizeInstance(envelope.Server, c.region)
}

func renameServer(ctx context.Context, c *client, projectID, serverID, name string) error {
	path := "/v1/" + projectID + "/cloudservers/" + url.PathEscape(serverID)
	body := map[string]interface{}{"server": map[string]interface{}{"name": name}}
	return c.do(ctx, "ecs", http.MethodPut, path, nil, body, nil)
}

func resetServerPassword(ctx context.Context, c *client, projectID, serverID, newPassword string) error {
	path := "/v1/" + projectID + "/cloudservers/" + url.PathEscape(serverID) + "/os-reset-password"
	body := map[string]interface{}{"new_password": newPassword}
	return c.do(ctx, "ecs", http.MethodPost, path, nil, body, nil)
}
