package oracle

import (
	"context"
	"errors"
	"strings"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

func (s *Service) listConsoleConnections(ctx context.Context, account Account, compartmentID, instanceID string) ([]NormalizedConsole, error) {
	client, err := s.clients.compute(account)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := contextWithReadTimeout(ctx)
	defer cancel()
	connections, err := paginate(func(page *string) ([]core.InstanceConsoleConnection, *string, error) {
		res, err := client.ListInstanceConsoleConnections(callCtx, core.ListInstanceConsoleConnectionsRequest{
			CompartmentId: common.String(compartmentID),
			InstanceId:    common.String(instanceID),
			Page:          page,
		})
		if err != nil {
			return nil, nil, err
		}
		return res.Items, res.OpcNextPage, nil
	})
	if err != nil {
		return nil, err
	}
	items := []NormalizedConsole{}
	for _, item := range connections {
		items = append(items, normalizeConsole(item))
	}
	return items, nil
}

func (s *Service) createConsoleConnection(ctx context.Context, account Account, instanceID, publicKey string) (NormalizedConsole, error) {
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return NormalizedConsole{}, errors.New("请填写 SSH 公钥")
	}
	client, err := s.clients.compute(account)
	if err != nil {
		return NormalizedConsole{}, err
	}
	callCtx, cancel := contextWithWriteTimeout(ctx)
	defer cancel()
	res, err := client.CreateInstanceConsoleConnection(callCtx, core.CreateInstanceConsoleConnectionRequest{
		CreateInstanceConsoleConnectionDetails: core.CreateInstanceConsoleConnectionDetails{
			InstanceId: common.String(instanceID),
			PublicKey:  common.String(publicKey),
		},
	})
	if err != nil {
		return NormalizedConsole{}, err
	}
	return normalizeConsole(res.InstanceConsoleConnection), nil
}

func (s *Service) deleteConsoleConnection(ctx context.Context, account Account, connectionID string) error {
	client, err := s.clients.compute(account)
	if err != nil {
		return err
	}
	callCtx, cancel := contextWithWriteTimeout(ctx)
	defer cancel()
	_, err = client.DeleteInstanceConsoleConnection(callCtx, core.DeleteInstanceConsoleConnectionRequest{
		InstanceConsoleConnectionId: common.String(connectionID),
	})
	return err
}
