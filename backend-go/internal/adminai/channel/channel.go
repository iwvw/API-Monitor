// Package channel 定义管理 AI 频道接入抽象层（PRD-03）。
// Channel 接口统一「入站 → 引擎执行 → 出站」管道；首个实现为 Telegram 长轮询。
package channel

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Channel 是频道接入的统一接口。
type Channel interface {
	// ID 返回频道唯一标识（如 "telegram"）。
	ID() string
	// Start 启动频道（长轮询/监听），阻塞直到 Stop 或 ctx 取消。
	Start(ctx context.Context) error
	// Stop 停止频道，清理轮询 goroutine。
	Stop(ctx context.Context) error
	// Send 发送消息，返回渠道侧消息 ID。
	Send(ctx context.Context, to string, msg OutboundMessage) (string, error)
	// Edit 编辑已有消息（流式编辑预览）。
	Edit(ctx context.Context, to, id string, msg OutboundMessage) error
	// Status 返回当前运行状态。
	Status() ChannelStatus
}

// InboundEnvelope 是入站消息的统一载体。
type InboundEnvelope struct {
	ChannelID string          `json:"channelId"`           // "telegram"
	ChatID    string          `json:"chatId"`              // 发送者/群组的唯一键
	UserID    string          `json:"userId"`              // 渠道侧发送者 ID
	Username  string          `json:"username,omitempty"`  // 显示名
	Text      string          `json:"text"`                // 纯文本内容
	ChatType  string          `json:"chatType"`            // "dm" | "group" | "topic"
	IsMention bool            `json:"isMention,omitempty"` // 群组中是否被 @
	Raw       json.RawMessage `json:"raw,omitempty"`       // 渠道原始消息
	MessageID int64           `json:"messageId,omitempty"` // 渠道侧消息 ID（用于回复）
}

// OutboundBlock 是出站消息的结构化块。
type OutboundBlock struct {
	Type  string `json:"type"`            // "text" | "code" | "error"
	Text  string `json:"text,omitempty"`  // 文本内容
	Code  string `json:"code,omitempty"`  // 代码块内容
	Title string `json:"title,omitempty"` // 块标题
}

// OutboundMessage 是出站消息（支持多段与流式编辑）。
type OutboundMessage struct {
	Text   string          `json:"text,omitempty"`   // 主文本（HTML 子集）
	Blocks []OutboundBlock `json:"blocks,omitempty"` // 结构化块
	Stream bool            `json:"stream,omitempty"` // 是否流式编辑
}

// ChannelState 是频道的运行状态枚举。
type ChannelState string

const (
	StateStopped  ChannelState = "stopped"
	StateRunning  ChannelState = "running"
	StateStarting ChannelState = "starting"
	StateError    ChannelState = "error"
)

// ChannelStatus 描述频道的运行时状态。
type ChannelStatus struct {
	State     ChannelState `json:"state"`
	Error     string       `json:"error,omitempty"`
	StartedAt time.Time    `json:"startedAt,omitempty"`
	LastError string       `json:"lastError,omitempty"`
}

// OnInboundFunc 是入站消息回调（由上层 adminai service 注入）。
type OnInboundFunc func(env InboundEnvelope)

// Registry 管理已注册的 Channel 实例。
type Registry struct {
	mu        sync.Mutex
	channels  map[string]Channel
	onInbound OnInboundFunc
}

func NewRegistry() *Registry {
	return &Registry{channels: make(map[string]Channel)}
}

// SetOnInbound 注入入站消息回调。
func (r *Registry) SetOnInbound(fn OnInboundFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onInbound = fn
}

// OnInbound 触发入站回调（不存在时静默丢弃）。
func (r *Registry) OnInbound(env InboundEnvelope) {
	r.mu.Lock()
	fn := r.onInbound
	r.mu.Unlock()
	if fn != nil {
		fn(env)
	}
}

// Register 注册频道实例（按 ID 覆盖）。
func (r *Registry) Register(c Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels[c.ID()] = c
}

// Unregister 注销频道实例。
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.channels, id)
}

// Get 按 ID 获取频道实例。
func (r *Registry) Get(id string) (Channel, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.channels[id]
	return c, ok
}

// All 返回全部频道实例。
func (r *Registry) All() []Channel {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Channel, 0, len(r.channels))
	for _, c := range r.channels {
		out = append(out, c)
	}
	return out
}
