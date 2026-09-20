package aiagent

import (
	"encoding/json"
	"time"
)

// 错误码：网关与用户面接口在错误响应中使用，便于客户端区分处置方式。
const (
	CodeUnauthorized  = "AIAGENT_UNAUTHORIZED"
	CodeForbidden     = "AIAGENT_FORBIDDEN"
	CodeNotFound      = "AIAGENT_NOT_FOUND"
	CodeConflict      = "AIAGENT_CONFLICT"
	CodeInvalid       = "AIAGENT_INVALID"
	CodeHostOffline   = "AIAGENT_HOST_OFFLINE"
	CodeChannelError  = "AIAGENT_CHANNEL_ERROR"
	CodeTimeout       = "AIAGENT_TIMEOUT"
	CodeRateLimited   = "AIAGENT_RATE_LIMITED"
	CodeAgentOutdated = "AIAGENT_AGENT_OUTDATED"
)

// User 是模块自带的用户，与面板管理员账号相互独立。
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
	Disabled    bool   `json:"disabled"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	LastLoginAt string `json:"lastLoginAt,omitempty"`
}

// Token 是长期设备令牌的展示视图，不包含明文与哈希。
type Token struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	Username    string `json:"username,omitempty"`
	Prefix      string `json:"prefix"`
	DeviceLabel string `json:"deviceLabel,omitempty"`
	ExpiresAt   string `json:"expiresAt"`
	RevokedAt   string `json:"revokedAt,omitempty"`
	LastUsedAt  string `json:"lastUsedAt,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

// Instance 是平台上登记的一台机器上的一个 AI Agent 服务。
//
// 实例是平台资源，不归属任何用户：用户通过 aiagent_instance_grants 被授权
// 使用其中的若干实例。
type Instance struct {
	ID       string `json:"id"`
	ServerID string `json:"serverId"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Port     int    `json:"port"`
	Enabled  bool   `json:"enabled"`
	// DesiredState 是期望状态：running / stopped / ""（空表示不托管，仅探测与转发）。
	// 由用户显式设置后，云端按它收敛实际进程状态（ADR-0006 第 3 条）。
	DesiredState string `json:"desiredState,omitempty"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

// 期望状态取值。空串表示「不托管」，用于兼容用户手工启动的实例。
const (
	DesiredStateRunning = "running"
	DesiredStateStopped = "stopped"
)

// ValidDesiredState 判断期望状态是否合法。空串合法（不托管）。
func ValidDesiredState(value string) bool {
	return value == "" || value == DesiredStateRunning || value == DesiredStateStopped
}

// InstanceView 是实例列表返回的完整视图，含在线探测结果与接入信息。
type InstanceView struct {
	Instance
	ProviderLabel string        `json:"providerLabel"`
	HostName      string        `json:"hostName,omitempty"`
	HostOnline    bool          `json:"hostOnline"`
	Status        InstanceState `json:"status"`
	// Lifecycle 是托管进程的状态；未托管时为 managed=false。
	Lifecycle  LifecycleState `json:"lifecycle"`
	AccessPath string         `json:"accessPath"`
	GatewayURL string         `json:"gatewayUrl"`
}

// LifecycleState 是主机 Agent 上报的托管进程状态（ADR-0006 第 5 条）。
type LifecycleState struct {
	Managed bool `json:"managed"`
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
	// DesiredRunning 是 Agent 侧记录的期望运行标记：显式 stop 后为 false。
	DesiredRunning bool `json:"desiredRunning"`
	// Crashed 为真表示重启次数已用尽，进入终态，需显式 start 才能恢复。
	Crashed bool `json:"crashed"`
	// PortListening / ListenerPID / ListenerMatchesProcess 让托管实例
	// 无需额外探测即可完成端口关联验证（ADR-0006 第 2 条）。
	PortListening          bool   `json:"portListening"`
	ListenerPID            int    `json:"listenerPid,omitempty"`
	ListenerMatchesProcess bool   `json:"listenerMatchesProcess"`
	UptimeSeconds          int    `json:"uptimeSeconds,omitempty"`
	Restarts               int    `json:"restarts,omitempty"`
	// MemoryBytes 是托管进程的常驻内存（字节）。
	MemoryBytes uint64 `json:"memoryBytes,omitempty"`
	// CPUPercent 是托管进程的 CPU 占用（相对单核的百分比）。
	CPUPercent float32 `json:"cpuPercent,omitempty"`
	// Supported 表示目标主机 Agent 是否声明了生命周期能力。
	Supported bool `json:"supported"`
}

// InstanceState 是实例的在线状态。
type InstanceState struct {
	// Online 为真表示「监听该端口的进程」正是目标 Provider 的进程。
	// 仅凭端口监听或仅凭进程存在都不足以判定在线（见 ADR-0006 第 2 条）。
	Online         bool `json:"online"`
	HostOnline     bool `json:"hostOnline"`
	ProcessRunning bool `json:"processRunning"`
	PortListening  bool `json:"portListening"`
	// PID 是进程匹配命中的 PID（可能不是监听端口的那个进程）。
	PID int `json:"pid,omitempty"`
	// ListenerPID 是实际监听实例端口的进程 PID。
	ListenerPID int `json:"listenerPid,omitempty"`
	// ListenerMatchesProcess 为真表示监听端口的进程命中了 Provider 的进程规则。
	ListenerMatchesProcess bool `json:"listenerMatchesProcess"`
	// MemoryBytes 是目标进程的常驻内存（字节）。0 表示未采集到。
	MemoryBytes uint64 `json:"memoryBytes,omitempty"`
	// CPUPercent 是目标进程的 CPU 占用（相对单核的百分比）。
	CPUPercent float32        `json:"cpuPercent,omitempty"`
	ProbedAt   string         `json:"probedAt,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// ProbeResult 是主机 Agent 返回的运行时探测结果。
type ProbeResult struct {
	ProcessRunning bool `json:"processRunning"`
	PortListening  bool `json:"portListening"`
	PID            int  `json:"pid,omitempty"`
	// ListenerPID 是监听目标端口的进程 PID；查不到为 0。
	ListenerPID int `json:"listenerPid,omitempty"`
	// ListenerMatchesProcess 表示监听端口的进程是否命中 Provider 的进程匹配规则。
	ListenerMatchesProcess bool `json:"listenerMatchesProcess"`
	// MemoryBytes 是目标进程的常驻内存（字节）。
	MemoryBytes uint64 `json:"memoryBytes,omitempty"`
	// CPUPercent 是目标进程的 CPU 占用（相对单核的百分比）。
	CPUPercent float32 `json:"cpuPercent,omitempty"`
	Detail     string  `json:"detail,omitempty"`
}

// AccessLog 是一条模块访问日志。
type AccessLog struct {
	ID         int64  `json:"id"`
	UserID     string `json:"userId,omitempty"`
	TokenID    string `json:"tokenId,omitempty"`
	InstanceID string `json:"instanceId,omitempty"`
	Action     string `json:"action"`
	Result     string `json:"result"`
	StatusCode int    `json:"statusCode,omitempty"`
	Error      string `json:"error,omitempty"`
	IP         string `json:"ip,omitempty"`
	UserAgent  string `json:"userAgent,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

// 请求载荷。

type loginPayload struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DeviceLabel string `json:"deviceLabel"`
}

type userPayload struct {
	Username    string  `json:"username"`
	Password    string  `json:"password"`
	DisplayName *string `json:"displayName"`
	Disabled    *bool   `json:"disabled"`
}

type instancePayload struct {
	ServerID string `json:"serverId"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Port     int    `json:"port"`
	Enabled  *bool  `json:"enabled"`
	// DesiredState 设置期望状态（running/stopped）。空串表示不改动。
	DesiredState string `json:"desiredState"`
	// ClearDesiredState 显式把期望状态清空（转回「不托管」）。
	// 需要独立布尔量，因为 JSON 里无法区分「未传」与「传了空串」。
	ClearDesiredState bool `json:"clearDesiredState"`
}

// grantsPayload 是「设置某用户可用实例」的载荷：传入的列表即最终授权集合，
// 未包含的实例会被收回授权。
type grantsPayload struct {
	InstanceIDs []string `json:"instanceIds"`
}

// InstanceGrant 描述某用户被授权使用的某个实例，用于实例侧展示授权用户。
type InstanceGrant struct {
	InstanceID string `json:"instanceId"`
	UserID     string `json:"userId"`
	Username   string `json:"username,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

type metaPayload struct {
	Meta string `json:"meta"`
}

// preferencesPayload 是客户端偏好同步的批量写入载荷。Values 为键到原始 JSON
// 的映射，服务端不解析其内容，仅原样存储后回吐，因此新增客户端设置项无需改
// 服务端；UpdatedAt 为客户端本地改动时间戳，用于多端冲突时判定新旧。
type preferencesPayload struct {
	Values    map[string]json.RawMessage `json:"values"`
	Keys      []string                   `json:"keys,omitempty"`
	UpdatedAt string                     `json:"updatedAt,omitempty"`
}

// Preference 是单条用户偏好的存储视图。
type Preference struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt string          `json:"updatedAt"`
}

// 认证上下文：由令牌或 session 解析得到。

type authContext struct {
	UserID   string
	TokenID  string
	Username string
	IsAdmin  bool
}

// nowRFC3339 返回当前 UTC 时间的 RFC3339 字符串，统一写库与响应格式。
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
