package huawei

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) flexusInstances(w http.ResponseWriter, r *http.Request, accountID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	instances, err := listFlexusInstances(r.Context(), c, account.DomainID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "获取 Flexus L 实例列表失败："+err.Error())
		return
	}
	s.enrichFlexusInstances(r.Context(), c, instances)
	response.OK(w, instances)
}

func (s *Service) flexusAction(w http.ResponseWriter, r *http.Request, accountID, instanceID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	var payload actionPayload
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	target, ok := s.flexusTarget(r.Context(), w, c, account, instanceID)
	if !ok {
		return
	}
	if target.CloudServerID == "" || target.RegionID == "" || target.ProjectID == "" {
		response.Error(w, http.StatusBadRequest, "该 Flexus L 实例缺少云主机信息，无法执行动作")
		return
	}
	jobID, err := runInstanceAction(r.Context(), regionClient(c, target.RegionID), target.ProjectID, payload.Action, []string{target.CloudServerID})
	if err != nil {
		response.Error(w, http.StatusBadGateway, "执行 Flexus L 实例动作失败："+err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"jobId": jobID, "message": "指令已下发"})
}

func (s *Service) flexusResetPassword(w http.ResponseWriter, r *http.Request, accountID, instanceID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
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
	target, ok := s.flexusTarget(r.Context(), w, c, account, instanceID)
	if !ok {
		return
	}
	if target.CloudServerID == "" || target.RegionID == "" || target.ProjectID == "" {
		response.Error(w, http.StatusBadRequest, "该 Flexus L 实例缺少云主机信息")
		return
	}
	if err := resetServerPassword(r.Context(), regionClient(c, target.RegionID), target.ProjectID, target.CloudServerID, payload.NewPassword); err != nil {
		response.Error(w, http.StatusBadGateway, "重置密码失败："+err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"id": instanceID})
}

func (s *Service) flexusRename(w http.ResponseWriter, r *http.Request, accountID, instanceID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	var payload struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if payload.Name == "" {
		response.Error(w, http.StatusBadRequest, "请填写新名称")
		return
	}
	target, ok := s.flexusTarget(r.Context(), w, c, account, instanceID)
	if !ok {
		return
	}
	if target.CloudServerID == "" || target.RegionID == "" || target.ProjectID == "" {
		response.Error(w, http.StatusBadRequest, "该 Flexus L 实例缺少云主机信息")
		return
	}
	if err := renameServer(r.Context(), regionClient(c, target.RegionID), target.ProjectID, target.CloudServerID, payload.Name); err != nil {
		response.Error(w, http.StatusBadGateway, "修改名称失败："+err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"id": instanceID})
}

// flexusTarget 查找指定套餐（轻量 RMS 查询，不触发云主机/BSS 增强）。
func (s *Service) flexusTarget(ctx context.Context, w http.ResponseWriter, c *client, account Account, instanceID string) (*normalFlexusInstance, bool) {
	instances, err := listFlexusInstances(ctx, c, account.DomainID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "获取 Flexus L 实例失败："+err.Error())
		return nil, false
	}
	for i := range instances {
		if instances[i].ID == instanceID {
			return &instances[i], true
		}
	}
	response.Error(w, http.StatusNotFound, "Flexus L 实例不存在")
	return nil, false
}

// listFlexusInstances 轻量列表：RMS 套餐信息（含组成资源），不做云主机/BSS 增强。
func listFlexusInstances(ctx context.Context, c *client, domainID string) ([]normalFlexusInstance, error) {
	if domainID == "" {
		resolved, err := resolveDomainID(ctx, c)
		if err != nil {
			return nil, err
		}
		domainID = resolved
	}
	path := "/v1/resource-manager/domains/" + domainID + "/all-resources"
	values := url.Values{}
	values.Set("type", "hcss.l-instance")
	instances := []normalFlexusInstance{}
	err := c.listJSON(ctx, "rms", path, values, "resources", "marker", func(raw json.RawMessage) error {
		instance, err := normalizeFlexusInstance(raw)
		if err != nil {
			return nil
		}
		instances = append(instances, instance)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return instances, nil
}

// enrichFlexusInstances 并发 enrichment：云主机状态/IP + 到期时间 + 流量包用量。
func (s *Service) enrichFlexusInstances(ctx context.Context, c *client, instances []normalFlexusInstance) {
	var wg sync.WaitGroup
	for i := range instances {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			enrichCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			s.enrichFlexusInstance(enrichCtx, c, &instances[i])
		}(i)
	}
	wg.Wait()
}

// enrichFlexusInstance 补充云主机状态/规格/IP、套餐到期时间（BSS 包周期资源）、流量包用量。
// 单项失败不阻断整体：查询不到只留空。
func (s *Service) enrichFlexusInstance(ctx context.Context, c *client, instance *normalFlexusInstance) {
	if instance.RegionID != "" && instance.ProjectID != "" && instance.CloudServerID != "" {
		if server, err := getServerStatus(ctx, regionClient(c, instance.RegionID), instance.ProjectID, instance.CloudServerID); err == nil {
			instance.ServerStatus = server.Status
			instance.PublicIP = server.PublicIP
			instance.PrivateIP = server.PrivateIP
			instance.FlavorName = server.FlavorName
			instance.VCPUs = server.VCPUs
			instance.MemoryMB = server.MemoryMB
			instance.ImageName = server.ImageName
		} else {
			applog.Warn(ctx, "huawei", "flexus enrich server failed", "flexus", instance.ID, "server", instance.CloudServerID, "region", instance.RegionID, "error", err.Error())
		}
	}
	if instance.ID != "" {
		if expire, specDesc, err := queryFlexusExpiry(ctx, c, instance.ID); err == nil {
			instance.ExpireAt = expire
			instance.SpecDescription = specDesc
		} else {
			applog.Warn(ctx, "huawei", "flexus enrich expiry failed", "flexus", instance.ID, "error", err.Error())
		}
	}
	trafficIDs := []string{}
	for _, res := range instance.ComposedResources {
		if strings.HasPrefix(res.TypeName, "huaweicloudinternal_cbc_freeresource") {
			trafficIDs = append(trafficIDs, res.ID)
		}
	}
	if len(trafficIDs) > 0 {
		if usage, err := queryFlexusTraffic(ctx, c, trafficIDs); err == nil && len(usage) > 0 {
			instance.TrafficTypeName = usage[0].TypeName
			instance.TrafficAmount = usage[0].Amount
			instance.TrafficOriginal = usage[0].OriginalAmount
			instance.TrafficExpireAt = usage[0].EndTime
			instance.TrafficMeasureID = usage[0].MeasureID
		} else if err != nil {
			applog.Warn(ctx, "huawei", "flexus enrich traffic failed", "flexus", instance.ID, "error", err.Error())
		}
	}
}

// queryFlexusExpiry 查询客户包年/包月资源（BSS ListPayPerUseCustomerResources），
// 返回套餐到期时间与规格描述。到期时间与华为云控制台一致（如 2029-11-23 23:59:59 GMT+08:00）。
func queryFlexusExpiry(ctx context.Context, c *client, flexusID string) (string, string, error) {
	body := map[string]interface{}{"resource_ids": []string{flexusID}, "offset": 0, "limit": 10}
	var result struct {
		Data []struct {
			ExpireTime string `json:"expire_time"`
			SpecDesc   string `json:"product_spec_desc"`
		} `json:"data"`
	}
	if err := c.do(ctx, "bss", http.MethodPost, "/v2/orders/suscriptions/resources/query", nil, body, &result); err != nil {
		return "", "", err
	}
	if len(result.Data) == 0 {
		return "", "", errors.New("未查询到套餐计费资源")
	}
	return result.Data[0].ExpireTime, result.Data[0].SpecDesc, nil
}

// queryFlexusTraffic 查询流量包剩余量（BSS 查询资源包使用量）。
func queryFlexusTraffic(ctx context.Context, c *client, freeResourceIDs []string) ([]normalFlexusTraffic, error) {
	if len(freeResourceIDs) == 0 {
		return nil, nil
	}
	body := map[string]interface{}{"free_resource_ids": freeResourceIDs}
	var result struct {
		FreeResources []struct {
			FreeResourceID string  `json:"free_resource_id"`
			TypeName       string  `json:"free_resource_type_name"`
			StartTime      string  `json:"start_time"`
			EndTime        string  `json:"end_time"`
			Amount         float64 `json:"amount"`
			OriginalAmount float64 `json:"original_amount"`
			MeasureID      int     `json:"measure_id"`
		} `json:"free_resources"`
	}
	if err := c.do(ctx, "bss", http.MethodPost, "/v2/payments/free-resources/usages/details/query", nil, body, &result); err != nil {
		return nil, err
	}
	out := make([]normalFlexusTraffic, 0, len(result.FreeResources))
	for _, fr := range result.FreeResources {
		out = append(out, normalFlexusTraffic{
			FreeResourceID: fr.FreeResourceID,
			TypeName:       fr.TypeName,
			StartTime:      fr.StartTime,
			EndTime:        fr.EndTime,
			Amount:         fr.Amount,
			OriginalAmount: fr.OriginalAmount,
			MeasureID:      fr.MeasureID,
		})
	}
	return out, nil
}

// resolveDomainID 从 IAM 项目接口推断账号 domain_id。
func resolveDomainID(ctx context.Context, c *client) (string, error) {
	var raw struct {
		Projects []struct {
			DomainID string `json:"domain_id"`
		} `json:"projects"`
	}
	if err := c.do(ctx, "iam", http.MethodGet, "/v3/projects", nil, nil, &raw); err != nil {
		return "", err
	}
	for _, project := range raw.Projects {
		if project.DomainID != "" {
			return project.DomainID, nil
		}
	}
	return "", errors.New("账号下无项目，无法解析 domain id")
}

func normalizeFlexusInstance(raw json.RawMessage) (normalFlexusInstance, error) {
	var item struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		RegionID  string `json:"region_id"`
		ProjectID string `json:"project_id"`
		Created   string `json:"created"`
		Updated   string `json:"updated"`
		Properties struct {
			Metadata struct {
				ChargeMode string `json:"charging_mode"`
				SpecCode   string `json:"resource_spec_code"`
				OrderID    string `json:"order_id"`
			} `json:"metadata"`
			Resources []struct {
				Type       string `json:"logical_resource_type"`
				ResourceID string `json:"physical_resource_id"`
				Name       string `json:"physical_resource_name"`
			} `json:"resources"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return normalFlexusInstance{}, err
	}
	instance := normalFlexusInstance{
		ID:         item.ID,
		Name:       item.Name,
		RegionID:   item.RegionID,
		ProjectID:  item.ProjectID,
		SpecCode:   item.Properties.Metadata.SpecCode,
		ChargeMode: item.Properties.Metadata.ChargeMode,
		OrderID:    item.Properties.Metadata.OrderID,
		CreatedAt:  item.Created,
		UpdatedAt:  item.Updated,
	}
	for _, resource := range item.Properties.Resources {
		instance.ComposedResources = append(instance.ComposedResources, normalComposedResource{
			TypeName: resource.Type,
			Name:     resource.Name,
			ID:       resource.ResourceID,
		})
		if resource.Type == "huaweicloudinternal_ecs_instance" {
			instance.CloudServerID = resource.ResourceID
			instance.CloudServerName = resource.Name
		}
	}
	return instance, nil
}