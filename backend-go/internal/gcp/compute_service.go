package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

var (
	errFieldRequired = errors.New("必填字段缺失")
)

const (
	publicImagesProject = "ubuntu-os-cloud"
	publicImagesFamily  = "ubuntu-minimal-2204-lts"
)

// ==================== 实例 ====================

// instanceFromRaw 从 GCP instance JSON 构造 Normalized 结构。
func instanceFromRaw(raw json.RawMessage) normalInstance {
	var source struct {
		ID                string            `json:"id"`
		Name              string            `json:"name"`
		Zone              string            `json:"zone"`
		MachineType       string            `json:"machineType"`
		Status            string            `json:"status"`
		CreationTimestamp string            `json:"creationTimestamp"`
		Labels            map[string]string `json:"labels"`
		LabelFingerprint  string            `json:"labelFingerprint"`
		NetworkInterfaces []struct {
			Name          string `json:"name"`
			Network       string `json:"network"`
			Subnetwork    string `json:"subnetwork"`
			NetworkIP     string `json:"networkIP"`
			AccessConfigs []struct {
				Name  string `json:"name"`
				Type  string `json:"type"`
				NatIP string `json:"natIP"`
			} `json:"accessConfigs"`
		} `json:"networkInterfaces"`
		Disks []struct {
			Type       string `json:"type"`
			Mode       string `json:"mode"`
			Source     string `json:"source"`
			DeviceName string `json:"deviceName"`
			Boot       bool   `json:"boot"`
			AutoDelete bool   `json:"autoDelete"`
			DiskSizeGB flexInt64 `json:"diskSizeGb"`
		} `json:"disks"`
		Metadata struct {
			Items []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"items"`
		} `json:"metadata"`
	}
	_ = json.Unmarshal(raw, &source)
	item := normalInstance{
		ID:                source.ID,
		Name:              source.Name,
		Zone:              shortZone(source.Zone),
		MachineType:       shortName(source.MachineType),
		State:             source.Status,
		CreationTimestamp: source.CreationTimestamp,
		Labels:            source.Labels,
		LabelFingerprint:  source.LabelFingerprint,
	}
	for _, iface := range source.NetworkInterfaces {
		normalizedIface := normalNetworkInterface{
			Name:       iface.Name,
			Network:    shortName(iface.Network),
			Subnetwork: shortName(iface.Subnetwork),
			NetworkIP:  iface.NetworkIP,
		}
		for _, config := range iface.AccessConfigs {
			normalizedIface.AccessConfigs = append(normalizedIface.AccessConfigs, normalAccessConfig{Name: config.Name, Type: config.Type, NatIP: config.NatIP})
			if item.PublicIP == "" && config.NatIP != "" {
				item.PublicIP = config.NatIP
			}
		}
		if item.PrivateIP == "" && iface.NetworkIP != "" {
			item.PrivateIP = iface.NetworkIP
		}
		item.NetworkInterfaces = append(item.NetworkInterfaces, normalizedIface)
	}
	for _, disk := range source.Disks {
		item.Disks = append(item.Disks, normalAttachedDisk{
			Type:       disk.Type,
			Mode:       disk.Mode,
			Source:     shortName(disk.Source),
			DeviceName: disk.DeviceName,
			Boot:       disk.Boot,
			AutoDelete: disk.AutoDelete,
			DiskSizeGB: int64(disk.DiskSizeGB),
		})
		if item.Image == "" && disk.Boot {
			item.Image = shortName(disk.Source)
		}
	}
	return item
}

func (s *Service) getInstance(ctx context.Context, c *client, projectID, zone, name string) (normalInstance, error) {
	path := "projects/" + projectID + "/zones/" + zone + "/instances/" + name
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodGet, "compute", path, nil, nil, &raw); err != nil {
		return normalInstance{}, err
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return normalInstance{}, err
	}
	return instanceFromRaw(payload), nil
}

func (s *Service) runInstanceAction(ctx context.Context, c *client, projectID, zone, name, action string) (operationStatus, error) {
	path := "projects/" + projectID + "/zones/" + zone + "/instances/" + name + "/" + action
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodPost, "compute", path, nil, map[string]interface{}{}, &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

func (s *Service) createInstance(ctx context.Context, c *client, projectID string, r *http.Request) (operationStatus, error) {
	var payload instanceCreatePayload
	if err := decodeJSON(r, &payload); err != nil {
		return operationStatus{}, err
	}
	if payload.Name == "" || payload.Zone == "" || payload.MachineType == "" {
		return operationStatus{}, errFieldRequired
	}
	path := "projects/" + projectID + "/zones/" + payload.Zone + "/instances"
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodPost, "compute", path, nil, buildInstanceBody(payload), &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

func buildInstanceBody(payload instanceCreatePayload) map[string]interface{} {
	body := map[string]interface{}{
		"name":        payload.Name,
		"zone":        payload.Zone,
		"machineType": "zones/" + payload.Zone + "/machineTypes/" + payload.MachineType,
	}
	diskBody := map[string]interface{}{
		"boot":       true,
		"autoDelete": true,
	}
	if payload.BootDiskSizeGB > 0 || payload.Image != "" {
		initParams := map[string]interface{}{}
		if payload.Image != "" {
			initParams["sourceImage"] = payload.Image
		}
		if payload.BootDiskSizeGB > 0 {
			initParams["diskSizeGb"] = payload.BootDiskSizeGB
		}
		diskBody["initializeParams"] = initParams
	}
	body["disks"] = []interface{}{diskBody}
	networkInterface := map[string]interface{}{}
	if payload.Network != "" {
		networkInterface["network"] = payload.Network
	}
	if payload.Subnetwork != "" {
		networkInterface["subnetwork"] = payload.Subnetwork
	}
	networkInterface["accessConfigs"] = []map[string]interface{}{
		{"name": "external-nat", "type": "ONE_TO_ONE_NAT"},
	}
	body["networkInterfaces"] = []interface{}{networkInterface}
	if len(payload.Metadata) > 0 {
		metadataItems := []map[string]string{}
		for key, value := range payload.Metadata {
			metadataItems = append(metadataItems, map[string]string{"key": key, "value": value})
		}
		body["metadata"] = map[string]interface{}{"items": metadataItems}
	}
	if len(payload.Labels) > 0 {
		body["labels"] = payload.Labels
	}
	return body
}

func (s *Service) setInstanceLabels(ctx context.Context, c *client, projectID, zone, name string, labels map[string]string) (operationStatus, error) {
	instance, err := s.getInstance(ctx, c, projectID, zone, name)
	if err != nil {
		return operationStatus{}, err
	}
	body := map[string]interface{}{"labels": labels}
	if instance.LabelFingerprint != "" {
		body["labelFingerprint"] = instance.LabelFingerprint
	}
	path := "projects/" + projectID + "/zones/" + zone + "/instances/" + name + "/setLabels"
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodPost, "compute", path, nil, body, &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

// ==================== 磁盘 ====================

func (s *Service) listDisks(ctx context.Context, c *client, projectID string) ([]normalDisk, error) {
	query := url.Values{}
	var items []normalDisk
	err := c.listJSON(ctx, http.MethodGet, "compute", "projects/"+projectID+"/aggregated/disks", query, "", []string{"disks"}, func(raw json.RawMessage) error {
		items = append(items, diskFromRaw(raw))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func diskFromRaw(raw json.RawMessage) normalDisk {
	var disk struct {
		ID                string            `json:"id"`
		Name              string            `json:"name"`
		Zone              string            `json:"zone"`
		Type              string            `json:"type"`
		SizeGB            flexInt64         `json:"sizeGb"`
		Status            string            `json:"status"`
		CreationTimestamp string            `json:"creationTimestamp"`
		Labels            map[string]string `json:"labels"`
		SourceSnapshot    string            `json:"sourceSnapshot"`
		Users             []string          `json:"users"`
	}
	_ = json.Unmarshal(raw, &disk)
	return normalDisk{
		ID:                disk.ID,
		Name:              disk.Name,
		Zone:              shortZone(disk.Zone),
		Type:              shortName(disk.Type),
		SizeGB:            int64(disk.SizeGB),
		State:             disk.Status,
		Status:            disk.Status,
		CreationTimestamp: disk.CreationTimestamp,
		Labels:            disk.Labels,
		SourceSnapshot:    shortName(disk.SourceSnapshot),
		Users:             disk.Users,
	}
}

func (s *Service) getDisk(ctx context.Context, c *client, projectID, zone, name string) (normalDisk, error) {
	path := "projects/" + projectID + "/zones/" + zone + "/disks/" + name
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodGet, "compute", path, nil, nil, &raw); err != nil {
		return normalDisk{}, err
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return normalDisk{}, err
	}
	return diskFromRaw(payload), nil
}

func (s *Service) deleteDisk(ctx context.Context, c *client, projectID, zone, name string) (operationStatus, error) {
	path := "projects/" + projectID + "/zones/" + zone + "/disks/" + name
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodDelete, "compute", path, nil, nil, &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

func (s *Service) resizeDisk(ctx context.Context, c *client, projectID, zone, name string, sizeGB int64) (operationStatus, error) {
	if sizeGB <= 0 {
		return operationStatus{}, errFieldRequired
	}
	path := "projects/" + projectID + "/zones/" + zone + "/disks/" + name + "/resize"
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodPost, "compute", path, nil, map[string]interface{}{"sizeGb": sizeGB}, &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

func (s *Service) snapshotDisk(ctx context.Context, c *client, projectID, zone, name string, snapshotName string) (operationStatus, error) {
	if snapshotName == "" {
		snapshotName = name + "-snap"
	}
	path := "projects/" + projectID + "/zones/" + zone + "/disks/" + name + "/createSnapshot"
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodPost, "compute", path, nil, map[string]interface{}{"name": snapshotName}, &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

// ==================== 选择器 ====================

func (s *Service) listZones(ctx context.Context, c *client, projectID string) ([]normalZone, error) {
	query := url.Values{}
	var items []normalZone
	err := c.listJSON(ctx, http.MethodGet, "compute", "projects/"+projectID+"/zones", query, "items", nil, func(raw json.RawMessage) error {
		var zone struct {
			Name   string `json:"name"`
			Region string `json:"region"`
			Status string `json:"status"`
		}
		_ = json.Unmarshal(raw, &zone)
		items = append(items, normalZone{Name: zone.Name, Region: shortName(zone.Region), Status: zone.Status})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) listMachineTypes(ctx context.Context, c *client, projectID, zone string) ([]normalMachineType, error) {
	query := url.Values{}
	path := "projects/" + projectID + "/aggregated/machineTypes"
	subKeys := []string{"machineTypes"}
	if zone != "" {
		path = "projects/" + projectID + "/zones/" + zone + "/machineTypes"
		subKeys = nil
	}
	var items []normalMachineType
	err := c.listJSON(ctx, http.MethodGet, "compute", path, query, "", subKeys, func(raw json.RawMessage) error {
		var mt struct {
			Name      string `json:"name"`
			Zone      string `json:"zone"`
			GuestCpus int64  `json:"guestCpus"`
			MemoryMb  int64  `json:"memoryMb"`
		}
		_ = json.Unmarshal(raw, &mt)
		items = append(items, normalMachineType{Name: shortName(mt.Name), Zone: shortZone(mt.Zone), GuestCpus: mt.GuestCpus, MemoryMb: mt.MemoryMb})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) listImages(ctx context.Context, c *client, filter string) ([]normalImage, error) {
	query := url.Values{}
	if filter != "" {
		query.Set("filter", filter)
	} else {
		query.Set("filter", "family="+publicImagesFamily)
	}
	var items []normalImage
	path := "projects/" + publicImagesProject + "/global/images"
	err := c.listJSON(ctx, http.MethodGet, "compute", path, query, "items", nil, func(raw json.RawMessage) error {
		var image struct {
			Name         string `json:"name"`
			Family       string `json:"family"`
			Status       string `json:"status"`
			Architecture string `json:"architecture"`
			DiskSizeGB   flexInt64 `json:"diskSizeGb"`
		}
		_ = json.Unmarshal(raw, &image)
		items = append(items, normalImage{Name: image.Name, Family: image.Family, Status: image.Status, Architecture: image.Architecture, DiskSizeGB: int64(image.DiskSizeGB)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) listSubnetworks(ctx context.Context, c *client, projectID string) ([]normalSubnetwork, error) {
	query := url.Values{}
	items := []normalSubnetwork{}
	err := c.listJSON(ctx, http.MethodGet, "compute", "projects/"+projectID+"/aggregated/subnetworks", query, "", []string{"subnetworks"}, func(raw json.RawMessage) error {
		var sub struct {
			Name        string `json:"name"`
			Region      string `json:"region"`
			Network     string `json:"network"`
			IPCidrRange string `json:"ipCidrRange"`
		}
		_ = json.Unmarshal(raw, &sub)
		items = append(items, normalSubnetwork{Name: sub.Name, Region: shortName(sub.Region), Network: shortName(sub.Network), IPCidrRange: sub.IPCidrRange})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) listFirewalls(ctx context.Context, c *client, projectID string) ([]normalFirewall, error) {
	query := url.Values{}
	var items []normalFirewall
	err := c.listJSON(ctx, http.MethodGet, "compute", "projects/"+projectID+"/global/firewalls", query, "items", nil, func(raw json.RawMessage) error {
		var fw struct {
			Name              string   `json:"name"`
			Direction         string   `json:"direction"`
			Priority          int64    `json:"priority"`
			Network           string   `json:"network"`
			SourceRanges      []string `json:"sourceRanges"`
			DestinationRanges []string `json:"destinationRanges"`
			Allowed           []struct {
				IPProtocol string   `json:"IPProtocol"`
				Ports      []string `json:"ports"`
			} `json:"allowed"`
			Denied []struct {
				IPProtocol string   `json:"IPProtocol"`
				Ports      []string `json:"ports"`
			} `json:"denied"`
		}
		_ = json.Unmarshal(raw, &fw)
		normalized := normalFirewall{
			Name:              fw.Name,
			Direction:         fw.Direction,
			Priority:          fw.Priority,
			Network:           shortName(fw.Network),
			SourceRanges:      fw.SourceRanges,
			DestinationRanges: fw.DestinationRanges,
		}
		if len(fw.Allowed) > 0 {
			normalized.Action = "ALLOW"
			for _, rule := range fw.Allowed {
				normalized.Allowed = append(normalized.Allowed, normalFirewallRule{IPProtocol: rule.IPProtocol, Ports: rule.Ports})
			}
		}
		if len(fw.Denied) > 0 {
			normalized.Action = "DENY"
			for _, rule := range fw.Denied {
				normalized.Denied = append(normalized.Denied, normalFirewallRule{IPProtocol: rule.IPProtocol, Ports: rule.Ports})
			}
		}
		items = append(items, normalized)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// createFirewall 创建防火墙规则（Compute Engine firewalls.insert）。
func (s *Service) createFirewall(ctx context.Context, c *client, projectID string, payload firewallWritePayload) (operationStatus, error) {
	if payload.Name == "" {
		return operationStatus{}, errFieldRequired
	}
	path := "projects/" + projectID + "/global/firewalls"
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodPost, "compute", path, nil, firewallWriteBody(payload), &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

// updateFirewall 更新防火墙规则（Compute Engine firewalls.patch）。
func (s *Service) updateFirewall(ctx context.Context, c *client, projectID, name string, payload firewallWritePayload) (operationStatus, error) {
	if name == "" {
		return operationStatus{}, errFieldRequired
	}
	path := "projects/" + projectID + "/global/firewalls/" + name
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodPatch, "compute", path, nil, firewallWriteBody(payload), &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

// deleteFirewall 删除防火墙规则（Compute Engine firewalls.delete）。
func (s *Service) deleteFirewall(ctx context.Context, c *client, projectID, name string) (operationStatus, error) {
	if name == "" {
		return operationStatus{}, errFieldRequired
	}
	path := "projects/" + projectID + "/global/firewalls/" + name
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodDelete, "compute", path, nil, nil, &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

// firewallWriteBody 将写请求 payload 归一为 Compute Engine Firewall 资源 JSON。
func firewallWriteBody(payload firewallWritePayload) map[string]interface{} {
	body := map[string]interface{}{}
	if payload.Name != "" {
		body["name"] = payload.Name
	}
	if payload.Description != "" {
		body["description"] = payload.Description
	}
	if payload.Network != "" {
		body["network"] = payload.Network
	}
	if payload.Direction != "" {
		body["direction"] = payload.Direction
	}
	if payload.Priority != 0 {
		body["priority"] = payload.Priority
	}
	if len(payload.SourceRanges) > 0 {
		body["sourceRanges"] = payload.SourceRanges
	}
	if len(payload.DestinationRanges) > 0 {
		body["destinationRanges"] = payload.DestinationRanges
	}
	if len(payload.Allowed) > 0 {
		body["allowed"] = payload.Allowed
	}
	if len(payload.Denied) > 0 {
		body["denied"] = payload.Denied
	}
	if payload.Disabled {
		body["disabled"] = true
	}
	return body
}

func (s *Service) listAddresses(ctx context.Context, c *client, projectID string) ([]normalAddress, error) {
	query := url.Values{}
	var items []normalAddress
	err := c.listJSON(ctx, http.MethodGet, "compute", "projects/"+projectID+"/aggregated/addresses", query, "", []string{"addresses"}, func(raw json.RawMessage) error {
		var addr struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Region  string   `json:"region"`
			Type    string   `json:"addressType"`
			Status  string   `json:"status"`
			Address string   `json:"address"`
			Users   []string `json:"users"`
		}
		_ = json.Unmarshal(raw, &addr)
		items = append(items, normalAddress{ID: addr.ID, Name: addr.Name, Region: shortName(addr.Region), Type: addr.Type, Status: addr.Status, Address: addr.Address, Users: addr.Users})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// ==================== Operation ====================

func (s *Service) getOperation(ctx context.Context, c *client, projectID, operationName string) (operationStatus, error) {
	// GCP operation 名是完整多段路径（projects/p/zones/z/operations/op 等）。
	// 路由已按 EscapedPath 逐段解码保留斜杠：若是完整路径直接使用，否则按项目前缀拼接。
	path := strings.TrimPrefix(operationName, "/")
	if !strings.HasPrefix(path, "projects/") {
		path = "projects/" + projectID + "/" + path
	}
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodGet, "compute", path, nil, nil, &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

// ==================== 工具 ====================

func shortZone(zone string) string {
	return strings.TrimPrefix(zone, "zones/")
}

func shortName(value string) string {
	if value == "" {
		return value
	}
	segments := strings.Split(value, "/")
	return segments[len(segments)-1]
}

func operationFromRaw(raw map[string]json.RawMessage) operationStatus {
	operation := operationStatus{
		Name:          fieldString(raw, "name"),
		Status:        fieldString(raw, "status"),
		OperationType: fieldString(raw, "operationType"),
		TargetLink:    fieldString(raw, "targetLink"),
	}
	if errBytes := fieldBytes(raw, "error"); len(errBytes) > 0 && string(errBytes) != "null" {
		operation.Error = string(errBytes)
	}
	return operation
}

func fieldString(raw map[string]json.RawMessage, key string) string {
	if value, ok := raw[key]; ok && len(value) > 0 {
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			return text
		}
	}
	return ""
}

func fieldBytes(raw map[string]json.RawMessage, key string) []byte {
	if value, ok := raw[key]; ok {
		return value
	}
	return nil
}

// ==================== HTTP handler ====================

func (s *Service) instances(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodGet:
		client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
		if !ok {
			return
		}
		items, err := s.listInstances(r.Context(), client, projectID, r.URL.Query().Get("filter"))
		writeResult(w, map[string]interface{}{"instances": items}, err)
	case http.MethodPost:
		client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
		if !ok {
			return
		}
		item, err := s.createInstance(r.Context(), client, projectID, r)
		writeResult(w, map[string]interface{}{"operation": item}, err)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listInstances 前端调用入口（带 filter 透传）。
func (s *Service) listInstances(ctx context.Context, c *client, projectID string, filter string) ([]normalInstance, error) {
	query := url.Values{}
	if filter != "" {
		query.Set("filter", filter)
	}
	var items []normalInstance
	err := c.listJSON(ctx, http.MethodGet, "compute", "projects/"+projectID+"/aggregated/instances", query, "", []string{"instances"}, func(raw json.RawMessage) error {
		items = append(items, instanceFromRaw(raw))
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.enrichInstanceMachineSpecs(ctx, c, projectID, items)
	return items, nil
}

// enrichInstanceMachineSpecs 为实例补充机型的 CPU/内存规格（GCP instance 对象不含该信息）。
func (s *Service) enrichInstanceMachineSpecs(ctx context.Context, c *client, projectID string, items []normalInstance) {
	zones := map[string]bool{}
	for _, item := range items {
		if item.Zone != "" {
			zones[item.Zone] = true
		}
	}
	if len(zones) == 0 {
		return
	}
	specByType := map[string]normalMachineType{}
	for zone := range zones {
		mts, err := s.listMachineTypes(ctx, c, projectID, zone)
		if err != nil {
			continue
		}
		for _, mt := range mts {
			specByType[mt.Name] = mt
		}
	}
	for i := range items {
		if mt, ok := specByType[items[i].MachineType]; ok {
			items[i].GuestCpus = mt.GuestCpus
			items[i].MemoryMb = mt.MemoryMb
		}
	}
}

func (s *Service) instanceDetail(w http.ResponseWriter, r *http.Request, idText, projectID, instanceName string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	zone := r.URL.Query().Get("zone")
	if zone == "" {
		response.Error(w, http.StatusBadRequest, "zone 必填")
		return
	}
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	item, err := s.getInstance(r.Context(), client, projectID, zone, instanceName)
	writeResult(w, map[string]interface{}{"instance": item}, err)
}

func (s *Service) instanceAction(w http.ResponseWriter, r *http.Request, idText, projectID, instanceName string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	zone := r.URL.Query().Get("zone")
	if zone == "" {
		response.Error(w, http.StatusBadRequest, "zone 必填")
		return
	}
	var payload instanceActionPayload
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	switch payload.Action {
	case "start", "stop", "reset":
		result, err := s.runInstanceAction(r.Context(), client, projectID, zone, instanceName, payload.Action)
		writeResult(w, result, err)
	case "delete":
		result, err := s.deleteInstance(r.Context(), client, projectID, zone, instanceName)
		writeResult(w, result, err)
	default:
		response.Error(w, http.StatusBadRequest, "action 必须是 start/stop/reset/delete")
	}
}

func (s *Service) deleteInstance(ctx context.Context, c *client, projectID, zone, name string) (operationStatus, error) {
	path := "projects/" + projectID + "/zones/" + zone + "/instances/" + name
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodDelete, "compute", path, nil, nil, &raw); err != nil {
		return operationStatus{}, err
	}
	return operationFromRaw(raw), nil
}

func (s *Service) instanceLabels(w http.ResponseWriter, r *http.Request, idText, projectID, instanceName string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	zone := r.URL.Query().Get("zone")
	if zone == "" {
		response.Error(w, http.StatusBadRequest, "zone 必填")
		return
	}
	var payload labelsPayload
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	result, err := s.setInstanceLabels(r.Context(), client, projectID, zone, instanceName, payload.Labels)
	writeResult(w, result, err)
}

func (s *Service) operationStatus(w http.ResponseWriter, r *http.Request, idText, projectID, operationName string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	item, err := s.getOperation(r.Context(), client, projectID, operationName)
	writeResult(w, map[string]interface{}{"operation": item}, err)
}

func (s *Service) disks(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	items, err := s.listDisks(r.Context(), client, projectID)
	writeResult(w, map[string]interface{}{"disks": items}, err)
}

func (s *Service) diskDetail(w http.ResponseWriter, r *http.Request, idText, projectID, diskName string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	zone := r.URL.Query().Get("zone")
	if zone == "" {
		response.Error(w, http.StatusBadRequest, "zone 必填")
		return
	}
	switch r.Method {
	case http.MethodGet:
		client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
		if !ok {
			return
		}
		item, err := s.getDisk(r.Context(), client, projectID, zone, diskName)
		writeResult(w, map[string]interface{}{"disk": item}, err)
	case http.MethodDelete:
		client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
		if !ok {
			return
		}
		result, err := s.deleteDisk(r.Context(), client, projectID, zone, diskName)
		writeResult(w, result, err)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) diskAction(w http.ResponseWriter, r *http.Request, idText, projectID, diskName, action string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	zone := r.URL.Query().Get("zone")
	if zone == "" {
		response.Error(w, http.StatusBadRequest, "zone 必填")
		return
	}
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	switch action {
	case "resize":
		var payload diskResizePayload
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := s.resizeDisk(r.Context(), client, projectID, zone, diskName, payload.SizeGB)
		writeResult(w, result, err)
	case "snapshot":
		var payload struct {
			SnapshotName string `json:"snapshotName"`
		}
		_ = decodeJSON(r, &payload)
		result, err := s.snapshotDisk(r.Context(), client, projectID, zone, diskName, payload.SnapshotName)
		writeResult(w, result, err)
	default:
		response.Error(w, http.StatusBadRequest, "action 必须是 resize/snapshot")
	}
}

func (s *Service) zones(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	items, err := s.listZones(r.Context(), client, projectID)
	writeResult(w, map[string]interface{}{"zones": items}, err)
}

func (s *Service) machineTypes(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	items, err := s.listMachineTypes(r.Context(), client, projectID, r.URL.Query().Get("zone"))
	writeResult(w, map[string]interface{}{"machineTypes": items}, err)
}

func (s *Service) images(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	items, err := s.listImages(r.Context(), client, r.URL.Query().Get("filter"))
	writeResult(w, map[string]interface{}{"images": items}, err)
}

func (s *Service) subnetworks(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	items, err := s.listSubnetworks(r.Context(), client, projectID)
	writeResult(w, map[string]interface{}{"subnetworks": items}, err)
}

func (s *Service) firewalls(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.listFirewalls(r.Context(), client, projectID)
		writeResult(w, map[string]interface{}{"firewalls": items}, err)
	case http.MethodPost:
		var payload firewallWritePayload
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item, err := s.createFirewall(r.Context(), client, projectID, payload)
		writeResult(w, map[string]interface{}{"operation": item}, err)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) firewallMutation(w http.ResponseWriter, r *http.Request, idText, projectID, name string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var payload firewallWritePayload
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item, err := s.updateFirewall(r.Context(), client, projectID, name, payload)
		writeResult(w, map[string]interface{}{"operation": item}, err)
	case http.MethodDelete:
		item, err := s.deleteFirewall(r.Context(), client, projectID, name)
		writeResult(w, map[string]interface{}{"operation": item}, err)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) addresses(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	items, err := s.listAddresses(r.Context(), client, projectID)
	writeResult(w, map[string]interface{}{"addresses": items}, err)
}