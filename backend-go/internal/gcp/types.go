package gcp

import (
	"strconv"
	"time"
)

// flexInt64 兼容 JSON 数字或字符串的 int64 字段（GCP 部分接口用字符串表示整数字段）。
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(data []byte) error {
	trimmed := string(data)
	if len(trimmed) >= 2 && trimmed[0] == '"' {
		trimmed = trimmed[1 : len(trimmed)-1]
	}
	v, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return err
	}
	*f = flexInt64(v)
	return nil
}

type Account struct {
	ID                         int64      `json:"id"`
	Name                       string     `json:"name"`
	ClientEmail                string     `json:"clientEmail"`
	DefaultProjectID           string     `json:"defaultProjectId,omitempty"`
	ServiceAccountJSON         string     `json:"-"`
	ServiceAccountJSONEncrypted string    `json:"-"`
	Description                string     `json:"description,omitempty"`
	LastVerifiedAt             *time.Time `json:"lastVerifiedAt,omitempty"`
	LastVerifyStatus           string     `json:"lastVerifyStatus,omitempty"`
	LastVerifyError            string     `json:"lastVerifyError,omitempty"`
	CreatedAt                  time.Time  `json:"createdAt"`
	UpdatedAt                  time.Time  `json:"updatedAt"`
}

type accountPayload struct {
	Name              string `json:"name"`
	ClientEmail       string `json:"clientEmail"`
	DefaultProjectID  string `json:"defaultProjectId"`
	ServiceAccountJSON string `json:"serviceAccountJson"`
	Description       string `json:"description"`
}

type instanceActionPayload struct {
	Action string `json:"action"`
}

type instanceCreatePayload struct {
	Name        string `json:"name"`
	Zone        string `json:"zone"`
	MachineType string `json:"machineType"`
	Image       string `json:"image"`
	BootDiskSizeGB int64 `json:"bootDiskSizeGb"`
	Network     string `json:"network"`
	Subnetwork  string `json:"subnetwork"`
	Metadata    map[string]string `json:"metadata"`
	Labels      map[string]string `json:"labels"`
}

type labelsPayload struct {
	Labels map[string]string `json:"labels"`
}

type diskResizePayload struct {
	SizeGB int64 `json:"sizeGb"`
}

type normalInstance struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Zone              string            `json:"zone"`
	MachineType       string            `json:"machineType"`
	GuestCpus         int64             `json:"guestCpus"`
	MemoryMb          int64             `json:"memoryMb"`
	State             string            `json:"state"`
	PublicIP          string            `json:"publicIp,omitempty"`
	PrivateIP         string            `json:"privateIp,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	Image             string            `json:"image,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	LabelFingerprint  string            `json:"labelFingerprint,omitempty"`
	CpuPlatform       string            `json:"cpuPlatform,omitempty"`
	DeletionProtection bool             `json:"deletionProtection"`
	NetworkInterfaces []normalNetworkInterface `json:"networkInterfaces,omitempty"`
	Disks             []normalAttachedDisk     `json:"disks,omitempty"`
}

type normalNetworkInterface struct {
	Name          string `json:"name"`
	Network       string `json:"network,omitempty"`
	Subnetwork    string `json:"subnetwork,omitempty"`
	NetworkIP     string `json:"networkIp,omitempty"`
	AccessConfigs []normalAccessConfig `json:"accessConfigs,omitempty"`
}

type normalAccessConfig struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
	NatIP string `json:"natIp,omitempty"`
}

type normalAttachedDisk struct {
	Type        string `json:"type,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Source      string `json:"source,omitempty"`
	DeviceName  string `json:"deviceName,omitempty"`
	Boot        bool   `json:"boot"`
	AutoDelete  bool   `json:"autoDelete"`
	DiskSizeGB  int64  `json:"diskSizeGb"`
}

type normalDisk struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Zone              string            `json:"zone"`
	Type              string            `json:"type,omitempty"`
	SizeGB            int64             `json:"sizeGb"`
	State             string            `json:"state,omitempty"`
	Status            string            `json:"status,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	SourceSnapshot    string            `json:"sourceSnapshot,omitempty"`
	Users             []string          `json:"users,omitempty"`
}

type normalFirewall struct {
	Name        string   `json:"name"`
	Direction   string   `json:"direction,omitempty"`
	Priority    int64    `json:"priority"`
	Action      string   `json:"action,omitempty"`
	Network     string   `json:"network,omitempty"`
	SourceRanges []string `json:"sourceRanges,omitempty"`
	DestinationRanges []string `json:"destinationRanges,omitempty"`
	Allowed     []normalFirewallRule `json:"allowed,omitempty"`
	Denied      []normalFirewallRule `json:"denied,omitempty"`
}

type normalFirewallRule struct {
	IPProtocol string `json:"ipProtocol,omitempty"`
	Ports      []string `json:"ports,omitempty"`
}

// firewallWritePayload 创建/更新防火墙规则的请求体（对应 Compute Engine Firewall 资源）。
type firewallWritePayload struct {
	Name              string               `json:"name,omitempty"`
	Description       string               `json:"description,omitempty"`
	Network           string               `json:"network,omitempty"`
	Direction         string               `json:"direction,omitempty"`
	Priority          int64                `json:"priority,omitempty"`
	SourceRanges      []string             `json:"sourceRanges,omitempty"`
	DestinationRanges []string             `json:"destinationRanges,omitempty"`
	Allowed           []firewallRuleEntry  `json:"allowed,omitempty"`
	Denied            []firewallRuleEntry  `json:"denied,omitempty"`
	Disabled          bool                 `json:"disabled,omitempty"`
}

type firewallRuleEntry struct {
	IPProtocol string   `json:"IPProtocol"`
	Ports      []string `json:"ports,omitempty"`
}

type normalAddress struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Zone              string `json:"zone,omitempty"`
	Region            string `json:"region,omitempty"`
	Type              string `json:"type,omitempty"`
	Status            string `json:"status,omitempty"`
	Address           string `json:"address,omitempty"`
	Users             []string `json:"users,omitempty"`
}

type normalZone struct {
	Name  string `json:"name"`
	Region string `json:"region,omitempty"`
	Status string `json:"status,omitempty"`
}

type normalSubnetwork struct {
	Name        string `json:"name"`
	Region      string `json:"region,omitempty"`
	Network     string `json:"network,omitempty"`
	IPCidrRange string `json:"ipCidrRange,omitempty"`
}

type normalMachineType struct {
	Name        string  `json:"name"`
	Zone        string  `json:"zone,omitempty"`
	GuestCpus   int64   `json:"guestCpus"`
	MemoryMb    int64   `json:"memoryMb"`
	Deprecated  bool    `json:"deprecated"`
}

type normalImage struct {
	Name        string `json:"name"`
	Family      string `json:"family,omitempty"`
	Status      string `json:"status,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	DiskSizeGB  int64  `json:"diskSizeGb"`
	Deprecated  bool   `json:"deprecated"`
}

type normalProject struct {
	ProjectID   string            `json:"projectId"`
	Name        string            `json:"name,omitempty"`
	State       string            `json:"state,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	CreateTime  string            `json:"createTime,omitempty"`
}

type normalBucket struct {
	Name        string                 `json:"name"`
	Location    string                 `json:"location,omitempty"`
	StorageClass string                `json:"storageClass,omitempty"`
	TimeCreated string                 `json:"timeCreated,omitempty"`
	Versioning  map[string]interface{} `json:"versioning,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty"`
}

type normalObject struct {
	Name         string `json:"name"`
	Size         string `json:"size,omitempty"`
	ContentType  string `json:"contentType,omitempty"`
	TimeCreated  string `json:"timeCreated,omitempty"`
	Updated      string `json:"updated,omitempty"`
}

type normalBillingAccount struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName,omitempty"`
	Open         bool   `json:"open"`
}

type normalBillingInfo struct {
	Name         string `json:"name,omitempty"`
	BillingAccountName string `json:"billingAccountName,omitempty"`
	BillingEnabled bool  `json:"billingEnabled"`
}

type normalBudget struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"displayName,omitempty"`
	Amount       string   `json:"amount,omitempty"`
	CurrencyCode string   `json:"currencyCode,omitempty"`
	ThresholdRules []normalBudgetThreshold `json:"thresholdRules,omitempty"`
	State        string   `json:"state,omitempty"`
}

type normalBudgetThreshold struct {
	ThresholdPercent float64 `json:"thresholdPercent"`
	SpendBasis       string  `json:"spendBasis,omitempty"`
}

type operationStatus struct {
	Name      string `json:"name,omitempty"`
	Status    string `json:"status,omitempty"`
	OperationType string `json:"operationType,omitempty"`
	TargetLink string `json:"targetLink,omitempty"`
	Error     string `json:"error,omitempty"`
}