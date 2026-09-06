package huawei

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) eips(w http.ResponseWriter, r *http.Request, accountID, projectID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	eips, err := s.listEIPsForProjects(r.Context(), c, projectID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "获取弹性公网 IP 列表失败："+err.Error())
		return
	}
	response.OK(w, eips)
}

func (s *Service) listEIPsForProjects(ctx context.Context, c *client, projectID string) ([]normalEIP, error) {
	projects, err := s.fetchProjects(ctx, c)
	if err != nil {
		return nil, err
	}
	if projectID != "all" && projectID != "" {
		for _, project := range projects {
			if project.ProjectID == projectID {
				return listEIPs(ctx, regionClient(c, project.Name), projectID)
			}
		}
		return nil, errProjectNotFound
	}
	return aggregateProjects(ctx, projects, func(ctx context.Context, project normalProject) ([]normalEIP, error) {
		return listEIPs(ctx, regionClient(c, project.Name), project.ProjectID)
	})
}

func listEIPs(ctx context.Context, c *client, projectID string) ([]normalEIP, error) {
	path := "/v1/" + projectID + "/publicips"
	var raw struct {
		PublicIPs []json.RawMessage `json:"publicips"`
	}
	if err := c.do(ctx, "vpc", http.MethodGet, path, nil, nil, &raw); err != nil {
		return nil, err
	}
	eips := make([]normalEIP, 0, len(raw.PublicIPs))
	for _, entry := range raw.PublicIPs {
		var ip struct {
			ID       string `json:"id"`
			PublicIP string `json:"public_ip_address"`
			Status   string `json:"status"`
			Bandwidth struct {
				Size int64 `json:"size"`
			} `json:"bandwidth"`
		}
		if err := json.Unmarshal(entry, &ip); err != nil {
			continue
		}
		eips = append(eips, normalEIP{
			ID: ip.ID, PublicIP: ip.PublicIP, Status: ip.Status,
			Bandwidth: ip.Bandwidth.Size, Region: c.region,
		})
	}
	return eips, nil
}

func (s *Service) eipAssociate(w http.ResponseWriter, r *http.Request, accountID, projectID, eipID, action string) {
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
		InstanceType string `json:"instanceType"`
		InstanceID   string `json:"instanceId"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	suffix := "associate-instance"
	if action == "disassociate" {
		suffix = "disassociate-instance"
	}
	path := "/v1/" + projectID + "/publicips/" + url.PathEscape(eipID) + "/" + suffix
	body := map[string]interface{}{"publicip_id": eipID}
	if payload.InstanceType != "" {
		body["instance_type"] = payload.InstanceType
	}
	if payload.InstanceID != "" {
		body["instance_id"] = payload.InstanceID
	}
	if err := c.do(r.Context(), "vpc", http.MethodPut, path, nil, body, nil); err != nil {
		response.Error(w, http.StatusBadGateway, "EIP 绑定操作失败："+err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"id": eipID, "action": action})
}

func (s *Service) vpcs(w http.ResponseWriter, r *http.Request, accountID, projectID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	vpcs, err := s.listVPCsForProjects(r.Context(), c, projectID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "获取 VPC 列表失败："+err.Error())
		return
	}
	response.OK(w, vpcs)
}

func (s *Service) listVPCsForProjects(ctx context.Context, c *client, projectID string) ([]map[string]interface{}, error) {
	projects, err := s.fetchProjects(ctx, c)
	if err != nil {
		return nil, err
	}
	if projectID != "all" && projectID != "" {
		for _, project := range projects {
			if project.ProjectID == projectID {
				return listVPCs(ctx, regionClient(c, project.Name), projectID)
			}
		}
		return nil, errProjectNotFound
	}
	return aggregateProjects(ctx, projects, func(ctx context.Context, project normalProject) ([]map[string]interface{}, error) {
		return listVPCs(ctx, regionClient(c, project.Name), project.ProjectID)
	})
}

func listVPCs(ctx context.Context, c *client, projectID string) ([]map[string]interface{}, error) {
	var raw struct {
		VPCs []json.RawMessage `json:"vpcs"`
	}
	if err := c.do(ctx, "vpc", http.MethodGet, "/v1/"+projectID+"/vpcs", nil, nil, &raw); err != nil {
		return nil, err
	}
	vpcs := make([]map[string]interface{}, 0, len(raw.VPCs))
	for _, entry := range raw.VPCs {
		var vpc map[string]interface{}
		if err := json.Unmarshal(entry, &vpc); err != nil {
			continue
		}
		vpcs = append(vpcs, map[string]interface{}{
			"id":        vpc["id"],
			"name":      vpc["name"],
			"cidr":      vpc["cidr"],
			"status":    vpc["status"],
			"createdAt": vpc["created_at"],
			"region":    c.region,
		})
	}
	return vpcs, nil
}

func (s *Service) securityGroups(w http.ResponseWriter, r *http.Request, accountID, projectID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	groups, err := s.listSecurityGroupsForProjects(r.Context(), c, projectID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "获取安全组列表失败："+err.Error())
		return
	}
	response.OK(w, groups)
}

func (s *Service) listSecurityGroupsForProjects(ctx context.Context, c *client, projectID string) ([]map[string]interface{}, error) {
	projects, err := s.fetchProjects(ctx, c)
	if err != nil {
		return nil, err
	}
	if projectID != "all" && projectID != "" {
		for _, project := range projects {
			if project.ProjectID == projectID {
				return listSecurityGroups(ctx, regionClient(c, project.Name), projectID)
			}
		}
		return nil, errProjectNotFound
	}
	return aggregateProjects(ctx, projects, func(ctx context.Context, project normalProject) ([]map[string]interface{}, error) {
		return listSecurityGroups(ctx, regionClient(c, project.Name), project.ProjectID)
	})
}

func listSecurityGroups(ctx context.Context, c *client, projectID string) ([]map[string]interface{}, error) {
	var raw struct {
		SecurityGroups []json.RawMessage `json:"security_groups"`
	}
	if err := c.do(ctx, "vpc", http.MethodGet, "/v1/"+projectID+"/security-groups", nil, nil, &raw); err != nil {
		return nil, err
	}
	groups := make([]map[string]interface{}, 0, len(raw.SecurityGroups))
	for _, entry := range raw.SecurityGroups {
		var group struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(entry, &group); err != nil {
			continue
		}
		groups = append(groups, map[string]interface{}{"id": group.ID, "name": group.Name})
	}
	return groups, nil
}
