package adminai

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/adminai/channel"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// fakeQueueChannel 记录出站消息的伪频道（排队测试用）。
type fakeQueueChannel struct {
	mu     sync.Mutex
	sent   []string
	edited []string
}

func (f *fakeQueueChannel) ID() string { return "telegram" }
func (f *fakeQueueChannel) Start(context.Context) error { return nil }
func (f *fakeQueueChannel) Stop(context.Context) error  { return nil }
func (f *fakeQueueChannel) Send(_ context.Context, to string, msg channel.OutboundMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg.Text)
	return "m1", nil
}
func (f *fakeQueueChannel) Edit(_ context.Context, to, id string, msg channel.OutboundMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edited = append(f.edited, msg.Text)
	return nil
}
func (f *fakeQueueChannel) Delete(context.Context, string, string) error { return nil }
func (f *fakeQueueChannel) Status() channel.ChannelStatus {
	return channel.ChannelStatus{State: channel.StateRunning}
}
func (f *fakeQueueChannel) texts() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.sent...)
	ed := append([]string(nil), f.edited...)
	return out, ed
}

func queueTestService(t *testing.T) (*Service, *fakeQueueChannel) {
	t.Helper()
	s := newTestService(t)
	s.SetAICaller(func(_ context.Context, _ systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error) {
		return systemmetrics.AICallResponse{}, nil
	})
	s.chanMgr = &channelManager{registry: channel.NewRegistry(), cancels: make(map[string]context.CancelFunc)}
	fc := &fakeQueueChannel{}
	s.chanMgr.registry.Register(fc)
	return s, fc
}

// 队列消费：活跃 run 存在时入队等待，run 退出后自动启动新 run 处理（不再被互斥拒绝）。
func TestChannelQueueConsumesAfterRunEnds(t *testing.T) {
	s, fc := queueTestService(t)
	env := channel.InboundEnvelope{ChannelID: "telegram", ChatID: "c1", UserID: "u1", Username: "u", Text: "第二条消息", ChatType: "dm"}

	// 模拟会话忙碌（上一条 run 进行中）
	s.mu.Lock()
	s.sessionRuns["cha_c1"] = "aae_busy"
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		// 300ms 后上一条 run 结束（释放会话）
		time.Sleep(300 * time.Millisecond)
		s.mu.Lock()
		delete(s.sessionRuns, "cha_c1")
		s.mu.Unlock()
		close(done)
	}()

	queueChannelConversationTest := func() {
		// 同步调用排队逻辑（内部会等待）
		s.queueChannelConversation(env, "cha_c1", "channel:telegram", `{"source":"channel:telegram"}`)
	}
	queueChannelConversationTest()
	<-done

	sent, edited := fc.texts()
	if len(sent) == 0 || !strings.Contains(sent[0], "排队") {
		t.Fatalf("expected queued placeholder sent, got %q", sent)
	}
	// 等待循环结束后的占位已经切换为处理中（RunLoop 成功启动）
	hasProcessing := false
	for _, e := range edited {
		if strings.Contains(e, "正在处理中") {
			hasProcessing = true
		}
	}
	if !hasProcessing {
		t.Fatalf("expected processing placeholder after queue, got %v", edited)
	}
	// 队列任务最终被处理：会话重新活跃（新 run 启动）或已完成
	s.mu.Lock()
	_, busy := s.sessionRuns["cha_c1"]
	s.mu.Unlock()
	_ = busy
}

// 排队超时：持续忙碌超过上限 → 提示稍后再试，不无限挂起。
func TestChannelQueueTimeout(t *testing.T) {
	s, fc := queueTestService(t)
	env := channel.InboundEnvelope{ChannelID: "telegram", ChatID: "c2", UserID: "u1", Text: "第三条", ChatType: "dm"}

	old := channelQueueTimeout
	channelQueueTimeout = 300 * time.Millisecond
	defer func() { channelQueueTimeout = old }()

	s.mu.Lock()
	s.sessionRuns["cha_c2"] = "aae_never_ends"
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.sessionRuns, "cha_c2")
		s.mu.Unlock()
	}()

	start := time.Now()
	s.queueChannelConversation(env, "cha_c2", "channel:telegram", "{}")
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("queue timeout took too long: %v", elapsed)
	}
	_, edited := fc.texts()
	found := false
	for _, e := range edited {
		if strings.Contains(e, "排队超时") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected queue timeout notice, got %v", edited)
	}
}