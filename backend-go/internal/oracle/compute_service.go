package oracle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

func (s *Service) verifyAccount(ctx context.Context, account Account) error {
	_, err := s.listAvailabilityDomains(ctx, account, firstNonEmpty(account.DefaultCompartmentID, account.TenancyOCID))
	return err
}

func (s *Service) listInstances(ctx context.Context, account Account, compartmentID string) ([]NormalizedInstance, error) {
	compartmentID = firstNonEmpty(compartmentID, account.DefaultCompartmentID, account.TenancyOCID)
	client, err := s.clients.compute(account)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := contextWithReadTimeout(ctx)
	defer cancel()
	res, err := client.ListInstances(callCtx, core.ListInstancesRequest{CompartmentId: common.String(compartmentID)})
	if err != nil {
		return nil, err
	}
	items := make([]NormalizedInstance, len(res.Items))
	var wg sync.WaitGroup
	for i, instance := range res.Items {
		wg.Add(1)
		go func(idx int, inst core.Instance) {
			defer wg.Done()
			vnics, _ := s.listVNICs(ctx, account, compartmentID, stringValuePtr(inst.Id))
			items[idx] = normalizeInstance(account, inst, vnics)
		}(i, instance)
	}
	wg.Wait()
	return items, nil
}

func (s *Service) listShapes(ctx context.Context, account Account, compartmentID, availabilityDomain, imageID string) ([]NormalizedShape, error) {
	compartmentID = firstNonEmpty(compartmentID, account.DefaultCompartmentID, account.TenancyOCID)
	client, err := s.clients.compute(account)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := contextWithReadTimeout(ctx)
	defer cancel()
	req := core.ListShapesRequest{
		CompartmentId: common.String(compartmentID),
	}
	if availabilityDomain != "" {
		req.AvailabilityDomain = common.String(availabilityDomain)
	}
	if imageID != "" {
		req.ImageId = common.String(imageID)
	}
	res, err := client.ListShapes(callCtx, req)
	if err != nil {
		return nil, err
	}
	items := make([]NormalizedShape, 0, len(res.Items))
	for _, shape := range res.Items {
		items = append(items, normalizeShape(shape))
	}
	return items, nil
}

func (s *Service) getInstance(ctx context.Context, account Account, compartmentID, instanceID string) (NormalizedInstance, error) {
	compartmentID = firstNonEmpty(compartmentID, account.DefaultCompartmentID, account.TenancyOCID)
	client, err := s.clients.compute(account)
	if err != nil {
		return NormalizedInstance{}, err
	}
	callCtx, cancel := contextWithReadTimeout(ctx)
	defer cancel()
	res, err := client.GetInstance(callCtx, core.GetInstanceRequest{InstanceId: common.String(instanceID)})
	if err != nil {
		return NormalizedInstance{}, err
	}
	vnics, _ := s.listVNICs(ctx, account, compartmentID, instanceID)

	var bootVolumes []NormalizedVolume
	var blockVolumes []NormalizedVolume
	var connections []NormalizedConsole
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		bootVolumes, _ = s.listBootVolumes(ctx, account, compartmentID, stringValuePtr(res.Instance.AvailabilityDomain), instanceID)
	}()
	go func() {
		defer wg.Done()
		blockVolumes, _ = s.listBlockVolumes(ctx, account, compartmentID, instanceID)
	}()
	go func() {
		defer wg.Done()
		connections, _ = s.listConsoleConnections(ctx, account, compartmentID, instanceID)
	}()
	wg.Wait()
	item := normalizeInstance(account, res.Instance, vnics)
	item.BootVolumeSummary = bootVolumes
	item.BlockVolumeSummary = blockVolumes
	item.ConsoleSummary = connections
	return item, nil
}

func (s *Service) runInstanceAction(ctx context.Context, account Account, instanceID, action string) (map[string]interface{}, error) {
	client, err := s.clients.compute(account)
	if err != nil {
		return nil, err
	}
	enum, err := instanceActionEnum(action)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := contextWithWriteTimeout(ctx)
	defer cancel()
	res, err := client.InstanceAction(callCtx, core.InstanceActionRequest{
		InstanceId: common.String(instanceID),
		Action:     enum,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "instance": normalizeInstance(account, res.Instance, nil)}, nil
}

func (s *Service) terminateInstance(ctx context.Context, account Account, instanceID string, preserveBootVolume bool) error {
	client, err := s.clients.compute(account)
	if err != nil {
		return err
	}
	callCtx, cancel := contextWithWriteTimeout(ctx)
	defer cancel()
	_, err = client.TerminateInstance(callCtx, core.TerminateInstanceRequest{
		InstanceId:         common.String(instanceID),
		PreserveBootVolume: common.Bool(preserveBootVolume),
	})
	return err
}

func (s *Service) updateInstance(ctx context.Context, account Account, instanceID string, payload updateInstancePayload) (NormalizedInstance, error) {
	client, err := s.clients.compute(account)
	if err != nil {
		return NormalizedInstance{}, err
	}
	details, err := buildUpdateInstanceDetails(payload)
	if err != nil {
		return NormalizedInstance{}, err
	}
	callCtx, cancel := contextWithWriteTimeout(ctx)
	defer cancel()
	res, err := client.UpdateInstance(callCtx, core.UpdateInstanceRequest{
		InstanceId:            common.String(instanceID),
		UpdateInstanceDetails: details,
	})
	if err != nil {
		return NormalizedInstance{}, err
	}
	return normalizeInstance(account, res.Instance, nil), nil
}

func buildUpdateInstanceDetails(payload updateInstancePayload) (core.UpdateInstanceDetails, error) {
	shape := strings.TrimSpace(payload.Shape)
	details := core.UpdateInstanceDetails{}
	if shape != "" {
		details.Shape = common.String(shape)
	}
	if payload.AvoidDowntime != nil {
		if *payload.AvoidDowntime {
			details.UpdateOperationConstraint = core.UpdateInstanceDetailsUpdateOperationConstraintAvoidDowntime
		} else {
			details.UpdateOperationConstraint = core.UpdateInstanceDetailsUpdateOperationConstraintAllowDowntime
		}
	}

	var shapeConfig core.UpdateInstanceShapeConfigDetails
	hasShapeConfig := false
	if payload.OCPUCount != nil {
		if *payload.OCPUCount <= 0 {
			return core.UpdateInstanceDetails{}, errors.New("OCPU 必须大于 0")
		}
		value := float32(*payload.OCPUCount)
		shapeConfig.Ocpus = &value
		hasShapeConfig = true
	}
	if payload.MemoryGB != nil {
		if *payload.MemoryGB <= 0 {
			return core.UpdateInstanceDetails{}, errors.New("内存必须大于 0")
		}
		value := float32(*payload.MemoryGB)
		shapeConfig.MemoryInGBs = &value
		hasShapeConfig = true
	}
	if baseline := strings.TrimSpace(payload.BaselineOcpuUtilization); baseline != "" {
		enum, ok := core.GetMappingUpdateInstanceShapeConfigDetailsBaselineOcpuUtilizationEnum(baseline)
		if !ok {
			return core.UpdateInstanceDetails{}, fmt.Errorf("不支持的 baseline OCPU 配置：%s", baseline)
		}
		shapeConfig.BaselineOcpuUtilization = enum
		hasShapeConfig = true
	}
	if !hasShapeConfig && details.Shape == nil {
		return core.UpdateInstanceDetails{}, errors.New("请至少提供目标规格、OCPU 或内存中的一项")
	}
	if hasShapeConfig {
		details.ShapeConfig = &shapeConfig
	}
	return details, nil
}

func instanceActionEnum(action string) (core.InstanceActionActionEnum, error) {
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "START":
		return core.InstanceActionActionStart, nil
	case "STOP":
		return core.InstanceActionActionStop, nil
	case "SOFTSTOP":
		return core.InstanceActionActionSoftstop, nil
	case "RESET":
		return core.InstanceActionActionReset, nil
	case "SOFTRESET":
		return core.InstanceActionActionSoftreset, nil
	case "REBOOTMIGRATE":
		return core.InstanceActionActionRebootmigrate, nil
	default:
		return "", errors.New("不支持的实例动作")
	}
}
