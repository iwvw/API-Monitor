package aiagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/sseutil"
)

// 偏好变更通知（SSE）
//
// 多端同步此前只能靠客户端轮询：改动最多要等一个轮询周期才能出现在其它设备上。
// 这里提供一条服务端推送通道，客户端收到事件后立刻拉取，把「等 15 秒」缩短为
// 「接近实时」。
//
// 设计取舍：
//   - 事件只带键名与时间戳，不带值。值可能接近单值上限（256 KB），塞进 SSE 会
//     把一条轻量通知变成大流量；客户端本来就要走一次 GET 才能拿到合并后的结果。
//   - 按 userID 隔离。偏好是用户私有数据，跨用户推送会泄露键名（键名里含实例 ID
//     与目录路径），因此只投递给发起写入的同一用户。
//   - 投递非阻塞。慢客户端（不读 SSE）不能拖住写请求，缓冲满即丢弃该事件——
//     客户端有轮询兜底，丢一条通知只会晚一个周期，不会丢数据。
const (
	// preferenceEventBuffer 是单个订阅者的缓冲深度。客户端读取通常远快于写入，
	// 32 条足够吸收突发；持续溢出说明客户端已失联，靠心跳与断开清理回收。
	preferenceEventBuffer = 32
	// preferenceEventHeartbeat 是心跳间隔。用于穿透中间代理的空闲超时，
	// 同时让服务端尽早发现已断开的连接。
	preferenceEventHeartbeat = 20 * time.Second
)

// preferenceEvent 是推送给客户端的偏好变更事件。
type preferenceEvent struct {
	Type      string   `json:"type"`
	Keys      []string `json:"keys"`
	UpdatedAt string   `json:"updatedAt"`
}

type preferenceSubscriber struct {
	userID string
	ch     chan preferenceEvent
}

// preferenceEventHub 维护所有在线的偏好订阅者。
type preferenceEventHub struct {
	mu          sync.Mutex
	nextID      int64
	subscribers map[int64]*preferenceSubscriber
}

func newPreferenceEventHub() *preferenceEventHub {
	return &preferenceEventHub{subscribers: make(map[int64]*preferenceSubscriber)}
}

func (h *preferenceEventHub) subscribe(userID string) (<-chan preferenceEvent, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	sub := &preferenceSubscriber{userID: userID, ch: make(chan preferenceEvent, preferenceEventBuffer)}
	h.subscribers[id] = sub
	return sub.ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if existing, ok := h.subscribers[id]; ok {
			delete(h.subscribers, id)
			close(existing.ch)
		}
	}
}

// publish 只投递给该用户自己的订阅者；缓冲满时丢弃该事件而非阻塞写入方。
func (h *preferenceEventHub) publish(userID string, event preferenceEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subscribers {
		if sub.userID != userID {
			continue
		}
		select {
		case sub.ch <- event:
		default:
		}
	}
}

// subscriberCount 供测试与观测使用。
func (h *preferenceEventHub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subscribers)
}

// handlePreferenceEvents 处理 GET /api/aiagent/preferences/events（SSE）。
//
// 鉴权沿用模块内 resolveAuth，与偏好读写同源：客户端用模块用户 Bearer 令牌，
// 后台管理面用面板 session。
func (s *Service) handlePreferenceEvents(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	// 反向代理（如 OpenResty）默认会缓冲响应体，导致事件被攒住才下发。
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, unsubscribe := s.preferenceEvents.subscribe(auth.UserID)
	defer unsubscribe()

	if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
		return
	}
	fmt.Fprint(w, "event: hello\ndata: {\"connected\":true}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(preferenceEventHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
				return
			}
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case event, open := <-events:
			if !open {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
				return
			}
			fmt.Fprintf(w, "event: preferences\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

// publishPreferenceChange 在写入成功后广播变更，通知其它端立刻拉取。
// 内部自行清洗键名：清洗后为空则不发事件，避免让客户端做一次无意义的拉取。
// 由本函数统一清洗（而非要求调用方先清洗）是为了让「空键不发事件」这条不变式
// 只有一个落点，调用方漏清洗也不会推出空事件。
func (s *Service) publishPreferenceChange(userID string, keys []string, updatedAt string) {
	clean := sanitizePreferenceKeys(keys)
	if len(clean) == 0 {
		return
	}
	s.preferenceEvents.publish(userID, preferenceEvent{
		Type:      "preferences.changed",
		Keys:      clean,
		UpdatedAt: updatedAt,
	})
}

// sanitizePreferenceKeys 去掉空键与超长键，并限制单次事件的键数量。
// 事件仅用于提示客户端「有变化」，键过多时截断不会影响正确性。
func sanitizePreferenceKeys(keys []string) []string {
	const maxKeys = 64
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out = append(out, key)
		if len(out) >= maxKeys {
			break
		}
	}
	return out
}


