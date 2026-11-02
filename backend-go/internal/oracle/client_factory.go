package oracle

import (
	"context"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"github.com/oracle/oci-go-sdk/v65/budget"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"github.com/oracle/oci-go-sdk/v65/usageapi"
)

const (
	readTimeout  = 20 * time.Second
	writeTimeout = 30 * time.Second
)

type clientFactory struct{}

func (clientFactory) provider(account Account) common.ConfigurationProvider {
	privateKey := secure.SecureDecrypt(account.PrivateKeyEncrypted)
	passphrase := secure.SecureDecrypt(account.PassphraseEncrypted)
	if passphrase == "" {
		return common.NewRawConfigurationProvider(account.TenancyOCID, account.UserOCID, account.Region, account.Fingerprint, privateKey, nil)
	}
	return common.NewRawConfigurationProvider(account.TenancyOCID, account.UserOCID, account.Region, account.Fingerprint, privateKey, &passphrase)
}

func (f clientFactory) compute(account Account) (core.ComputeClient, error) {
	return core.NewComputeClientWithConfigurationProvider(f.provider(account))
}

func (f clientFactory) network(account Account) (core.VirtualNetworkClient, error) {
	return core.NewVirtualNetworkClientWithConfigurationProvider(f.provider(account))
}

func (f clientFactory) blockstorage(account Account) (core.BlockstorageClient, error) {
	return core.NewBlockstorageClientWithConfigurationProvider(f.provider(account))
}

func (f clientFactory) identity(account Account) (identity.IdentityClient, error) {
	return identity.NewIdentityClientWithConfigurationProvider(f.provider(account))
}

func (f clientFactory) usage(account Account) (usageapi.UsageapiClient, error) {
	return usageapi.NewUsageapiClientWithConfigurationProvider(f.provider(account))
}

func (f clientFactory) budget(account Account) (budget.BudgetClient, error) {
	return budget.NewBudgetClientWithConfigurationProvider(f.provider(account))
}

func contextWithReadTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, readTimeout)
}

func contextWithWriteTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, writeTimeout)
}
