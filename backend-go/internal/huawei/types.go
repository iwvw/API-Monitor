package huawei

import (
	"strconv"
	"time"
)

// flexInt64 兼容 JSON 数字或字符串的 int64 字段（华为云 ECS flavor 等字段用字符串表示整数）。
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(data []byte) error {
	trimmed := string(data)
	if len(trimmed) >= 2 && trimmed[0] == '"' {
		trimmed = trimmed[1 : len(trimmed)-1]
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return err
	}
	*f = flexInt64(value)
	return nil
}

// Account 华为云账号（AK/SK）。
// 敏感字段 SecretAccessKey 仅用于内存，不参与 JSON 序列化。
type Account struct {
	ID                        int64      `json:"id"`
	Name                      string     `json:"name"`
	Site                      string     `json:"site"`
	AccessKeyID               string     `json:"accessKeyId"`
	SecretAccessKey           string     `json:"-"`
	SecretAccessKeyEncrypted  string     `json:"-"`
	DomainID                  string     `json:"domainId,omitempty"`
	DefaultRegion             string     `json:"defaultRegion,omitempty"`
	DefaultProjectID          string     `json:"defaultProjectId,omitempty"`
	Description               string     `json:"description,omitempty"`
	SSHUser                   string     `json:"sshUser,omitempty"`
	SSHPort                   int        `json:"sshPort,omitempty"`
	SSHPrivateKey             string     `json:"-"`
	SSHPrivateKeyEncrypted    string     `json:"-"`
	SSHPassword               string     `json:"-"`
	SSHPasswordEncrypted      string     `json:"-"`
	LastVerifiedAt            *time.Time `json:"lastVerifiedAt,omitempty"`
	LastVerifyStatus          string     `json:"lastVerifyStatus,omitempty"`
	LastVerifyError           string     `json:"lastVerifyError,omitempty"`
	CreatedAt                 time.Time  `json:"createdAt"`
	UpdatedAt                 time.Time  `json:"updatedAt"`
}

type accountPayload struct {
	Name             string `json:"name"`
	Site             string `json:"site"`
	AccessKeyID      string `json:"accessKeyId"`
	SecretAccessKey  string `json:"secretAccessKey"`
	DefaultRegion    string `json:"defaultRegion"`
	DefaultProjectID string `json:"defaultProjectId"`
	Description      string `json:"description"`
	SSHUser          string `json:"sshUser"`
	SSHPort          int    `json:"sshPort"`
	SSHPrivateKey    string `json:"sshPrivateKey"`
	SSHPassword      string `json:"sshPassword"`
}

type defaultPayload struct {
	DefaultRegion    string `json:"defaultRegion"`
	DefaultProjectID string `json:"defaultProjectId"`
}

type actionPayload struct {
	Action    string   `json:"action"`
	ServerIds []string `json:"serverIds"`
}

type normalProject struct {
	Name      string `json:"name"`
	ProjectID string `json:"projectId"`
	DomainID  string `json:"domainId,omitempty"`
}

type normalInstance struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ProjectID  string `json:"projectId,omitempty"`
	Status     string `json:"status,omitempty"`
	FlavorID   string `json:"flavorId,omitempty"`
	FlavorName string `json:"flavorName,omitempty"`
	VCPUs      int64  `json:"vcpus"`
	MemoryMB   int64  `json:"memoryMb"`
	Region     string `json:"region,omitempty"`
	PublicIP   string `json:"publicIp,omitempty"`
	PrivateIP  string `json:"privateIp,omitempty"`
	ChargeMode string `json:"chargeMode,omitempty"`
	OrderID    string `json:"orderId,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
	ImageName  string `json:"imageName,omitempty"`
}

type normalFlexusInstance struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	RegionID         string                 `json:"regionId"`
	ProjectID        string                 `json:"projectId"`
	SpecCode         string                 `json:"specCode,omitempty"`
	ChargeMode       string                 `json:"chargeMode,omitempty"`
	OrderID          string                 `json:"orderId,omitempty"`
	CreatedAt        string                 `json:"createdAt,omitempty"`
	UpdatedAt        string                 `json:"updatedAt,omitempty"`
	CloudServerID    string                 `json:"cloudServerId,omitempty"`
	CloudServerName  string                 `json:"cloudServerName,omitempty"`
	ServerStatus     string                 `json:"serverStatus,omitempty"`
	PublicIP         string                 `json:"publicIp,omitempty"`
	PrivateIP        string                 `json:"privateIp,omitempty"`
	FlavorName       string                 `json:"flavorName,omitempty"`
	VCPUs            int64                  `json:"vcpus"`
	MemoryMB         int64                  `json:"memoryMb"`
	ImageName        string                 `json:"imageName,omitempty"`
	TrafficTypeName  string                 `json:"trafficTypeName,omitempty"`
	TrafficAmount    float64                `json:"trafficAmount"`
	TrafficOriginal  float64                `json:"trafficOriginal"`
	TrafficExpireAt  string                 `json:"trafficExpireAt,omitempty"`
	TrafficMeasureID int                    `json:"trafficMeasureId"`
	ExpireAt         string                 `json:"expireAt,omitempty"`
	SpecDescription  string                 `json:"specDescription,omitempty"`
	ComposedResources []normalComposedResource `json:"composedResources,omitempty"`
}

type normalComposedResource struct {
	TypeName string `json:"typeName,omitempty"`
	Name     string `json:"name,omitempty"`
	ID       string `json:"id,omitempty"`
}

type normalFlexusTraffic struct {
	FreeResourceID string  `json:"freeResourceId"`
	TypeName       string  `json:"typeName,omitempty"`
	StartTime      string  `json:"startTime,omitempty"`
	EndTime        string  `json:"endTime,omitempty"`
	Amount         float64 `json:"amount"`
	OriginalAmount float64 `json:"originalAmount"`
	MeasureID      int     `json:"measureId"`
}

type normalZone struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Status      string `json:"status,omitempty"`
	RecordNum   int64  `json:"recordNum"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

type normalRecordset struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Type      string   `json:"type,omitempty"`
	TTL       int64    `json:"ttl"`
	Records   []string `json:"records,omitempty"`
	Status    string   `json:"status,omitempty"`
}

type normalEIP struct {
	ID       string `json:"id"`
	PublicIP string `json:"publicIp"`
	Status   string `json:"status,omitempty"`
	Bandwidth int64 `json:"bandwidth"`
	Region   string `json:"region,omitempty"`
}

type normalBucket struct {
	Name         string `json:"name"`
	Region       string `json:"region,omitempty"`
	StorageClass string `json:"storageClass,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
}

type normalObject struct {
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified,omitempty"`
}
