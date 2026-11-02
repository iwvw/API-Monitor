package huawei

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

var errProjectNotFound = errors.New("未找到指定项目")

// dnsClient 解析区域并构造 DNS 子 client（DNS 为全局服务，区域仅用于端点选择）。
func (s *Service) dnsClient(ctx context.Context, w http.ResponseWriter, account Account, projectID string) (*client, bool) {
	c, ok := s.clientForAccount(ctx, w, account)
	if !ok {
		return nil, false
	}
	region, err := s.regionForProject(ctx, c, projectID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return nil, false
	}
	return regionClient(c, region), true
}

func (s *Service) zones(w http.ResponseWriter, r *http.Request, accountID, projectID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		zones, err := s.listZonesForProjects(r.Context(), c, projectID)
		if err != nil {
			response.Error(w, http.StatusBadGateway, "获取 DNS zone 列表失败："+err.Error())
			return
		}
		response.OK(w, zones)
	case http.MethodPost:
		var payload struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Email string `json:"email"`
		}
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		zone, err := s.createZoneForProject(r.Context(), c, projectID, payload.Name, payload.Type, payload.Email)
		if err != nil {
			response.Error(w, http.StatusBadGateway, "创建 DNS zone 失败："+err.Error())
			return
		}
		response.OK(w, zone)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listZonesForProjects projectID 为 "all"（或空）时跨所有项目区域聚合去重查询，
// 跳过失败区域（不整体 502）。
func (s *Service) listZonesForProjects(ctx context.Context, c *client, projectID string) ([]normalZone, error) {
	projects, err := s.fetchProjects(ctx, c)
	if err != nil {
		return nil, err
	}
	if projectID != "all" && projectID != "" {
		for _, project := range projects {
			if project.ProjectID == projectID {
				return listZones(ctx, regionClient(c, project.Name))
			}
		}
		return nil, errProjectNotFound
	}
	zones, err := aggregateProjects(ctx, projects, func(ctx context.Context, project normalProject) ([]normalZone, error) {
		return listZones(ctx, regionClient(c, project.Name))
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	deduped := make([]normalZone, 0, len(zones))
	for _, zone := range zones {
		if zone.ID != "" && seen[zone.ID] {
			continue
		}
		if zone.ID != "" {
			seen[zone.ID] = true
		}
		deduped = append(deduped, zone)
	}
	return deduped, nil
}

// createZoneForProject 创建 zone：指定项目时用其区域，否则用第一个项目区域。
func (s *Service) createZoneForProject(ctx context.Context, c *client, projectID, name, zoneType, email string) (normalZone, error) {
	projects, err := s.fetchProjects(ctx, c)
	if err != nil {
		return normalZone{}, err
	}
	region := ""
	for _, project := range projects {
		if project.ProjectID == projectID {
			region = project.Name
			break
		}
		if projectID == "" || projectID == "all" {
			region = project.Name
			break
		}
	}
	if region == "" && len(projects) > 0 {
		region = projects[0].Name
	}
	if region == "" {
		return normalZone{}, errors.New("无法确定创建区域")
	}
	return createZone(ctx, regionClient(c, region), name, zoneType, email)
}

func listZones(ctx context.Context, c *client) ([]normalZone, error) {
	zones := []normalZone{}
	err := c.listJSON(ctx, "dns", "/v2/zones", url.Values{}, "zones", "marker", func(raw json.RawMessage) error {
		var zone struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			ZoneType  string `json:"zone_type"`
			Status    string `json:"status"`
			RecordNum int64  `json:"record_num"`
			CreatedAt string `json:"created_at"`
		}
		if err := json.Unmarshal(raw, &zone); err != nil {
			return nil
		}
		zones = append(zones, normalZone{
			ID:        zone.ID,
			Name:      zone.Name,
			Type:      zone.ZoneType,
			Status:    zone.Status,
			RecordNum: zone.RecordNum,
			CreatedAt: zone.CreatedAt,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return zones, nil
}

func createZone(ctx context.Context, c *client, name, zoneType, email string) (normalZone, error) {
	body := map[string]interface{}{"name": name}
	if zoneType != "" {
		body["zone_type"] = zoneType
	}
	if email != "" {
		body["email"] = email
	}
	var result struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ZoneType  string `json:"zone_type"`
		Status    string `json:"status"`
	}
	if err := c.do(ctx, "dns", http.MethodPost, "/v2/zones", nil, body, &result); err != nil {
		return normalZone{}, err
	}
	return normalZone{ID: result.ID, Name: result.Name, Type: result.ZoneType, Status: result.Status}, nil
}

func (s *Service) zoneDetail(w http.ResponseWriter, r *http.Request, accountID, projectID, zoneID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.dnsClient(r.Context(), w, account, projectID)
	if !ok {
		return
	}
	path := "/v2/zones/" + url.PathEscape(zoneID)
	switch r.Method {
	case http.MethodGet:
		var result struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			ZoneType  string `json:"zone_type"`
			Status    string `json:"status"`
			RecordNum int64  `json:"record_num"`
			CreatedAt string `json:"created_at"`
		}
		if err := c.do(r.Context(), "dns", http.MethodGet, path, nil, nil, &result); err != nil {
			response.Error(w, http.StatusBadGateway, "获取 DNS zone 详情失败："+err.Error())
			return
		}
		response.OK(w, normalZone{
			ID: result.ID, Name: result.Name, Type: result.ZoneType,
			Status: result.Status, RecordNum: result.RecordNum, CreatedAt: result.CreatedAt,
		})
	case http.MethodDelete:
		if err := c.do(r.Context(), "dns", http.MethodDelete, path, nil, nil, nil); err != nil {
			response.Error(w, http.StatusBadGateway, "删除 DNS zone 失败："+err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": zoneID})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) recordsets(w http.ResponseWriter, r *http.Request, accountID, projectID, zoneID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.dnsClient(r.Context(), w, account, projectID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		records, err := listRecordsets(r.Context(), c, zoneID)
		if err != nil {
			response.Error(w, http.StatusBadGateway, "获取记录集列表失败："+err.Error())
			return
		}
		response.OK(w, records)
	case http.MethodPost:
		var payload struct {
			Name    string   `json:"name"`
			Type    string   `json:"type"`
			TTL     int64    `json:"ttl"`
			Records []string `json:"records"`
		}
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err := createRecordset(r.Context(), c, zoneID, payload.Name, payload.Type, payload.TTL, payload.Records)
		if err != nil {
			response.Error(w, http.StatusBadGateway, "创建记录集失败："+err.Error())
			return
		}
		response.OK(w, record)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func listRecordsets(ctx context.Context, c *client, zoneID string) ([]normalRecordset, error) {
	records := []normalRecordset{}
	path := "/v2/zones/" + url.PathEscape(zoneID) + "/recordsets"
	err := c.listJSON(ctx, "dns", path, url.Values{}, "recordsets", "marker", func(raw json.RawMessage) error {
		var record struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Type    string   `json:"type"`
			TTL     int64    `json:"ttl"`
			Records []string `json:"records"`
			Status  string   `json:"status"`
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil
		}
		records = append(records, normalRecordset{
			ID: record.ID, Name: record.Name, Type: record.Type,
			TTL: record.TTL, Records: record.Records, Status: record.Status,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

func createRecordset(ctx context.Context, c *client, zoneID, name, recordType string, ttl int64, records []string) (normalRecordset, error) {
	path := "/v2/zones/" + url.PathEscape(zoneID) + "/recordsets"
	body := map[string]interface{}{"name": name, "type": recordType, "records": records}
	if ttl > 0 {
		body["ttl"] = ttl
	}
	var result struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		TTL     int64    `json:"ttl"`
		Records []string `json:"records"`
	}
	if err := c.do(ctx, "dns", http.MethodPost, path, nil, body, &result); err != nil {
		return normalRecordset{}, err
	}
	return normalRecordset{ID: result.ID, Name: result.Name, Type: result.Type, TTL: result.TTL, Records: result.Records}, nil
}

func (s *Service) recordsetMutation(w http.ResponseWriter, r *http.Request, accountID, projectID, zoneID, recordsetID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.dnsClient(r.Context(), w, account, projectID)
	if !ok {
		return
	}
	path := "/v2/zones/" + url.PathEscape(zoneID) + "/recordsets/" + url.PathEscape(recordsetID)
	switch r.Method {
	case http.MethodPut:
		var payload struct {
			Name    string   `json:"name"`
			Type    string   `json:"type"`
			TTL     int64    `json:"ttl"`
			Records []string `json:"records"`
		}
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		body := map[string]interface{}{"records": payload.Records}
		if payload.Name != "" {
			body["name"] = payload.Name
		}
		if payload.Type != "" {
			body["type"] = payload.Type
		}
		if payload.TTL > 0 {
			body["ttl"] = payload.TTL
		}
		if err := c.do(r.Context(), "dns", http.MethodPut, path, nil, body, nil); err != nil {
			response.Error(w, http.StatusBadGateway, "修改记录集失败："+err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": recordsetID})
	case http.MethodDelete:
		if err := c.do(r.Context(), "dns", http.MethodDelete, path, nil, nil, nil); err != nil {
			response.Error(w, http.StatusBadGateway, "删除记录集失败："+err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": recordsetID})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
