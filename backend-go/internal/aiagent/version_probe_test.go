package aiagent

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// versionProbeRuntime 是一个可配置响应体的 AgentRuntime mock：
// RoundTrip 按请求路径返回预置的 JSON（模拟主机 opencode 的 /api/info 或 /global/health）。
type versionProbeRuntime struct {
	online bool
	// 路径 -> 响应体（"" 表示该路径 404/失败）
	bodies map[string]string
	// 返回的状态码（默认 200）
	status int
}

func (r *versionProbeRuntime) AgentOnline(string) bool { return r.online }

func (r *versionProbeRuntime) AgentSupportsAIAgentStream(string) bool { return true }

func (r *versionProbeRuntime) AgentSupportsLifecycle(string) bool { return false }

func (r *versionProbeRuntime) Probe(context.Context, string, string, int, []string) (ProbeResult, error) {
	return ProbeResult{ProcessRunning: true, PortListening: true, ListenerMatchesProcess: true}, nil
}

func (r *versionProbeRuntime) StartProcess(context.Context, string, LifecycleStartPayload) (LifecycleResult, error) {
	return LifecycleResult{}, nil
}

func (r *versionProbeRuntime) StopProcess(context.Context, string, string) (LifecycleResult, error) {
	return LifecycleResult{}, nil
}

func (r *versionProbeRuntime) ProcessStatus(context.Context, string, string) (LifecycleResult, error) {
	return LifecycleResult{}, nil
}

func (r *versionProbeRuntime) Diagnose(context.Context, string, string) (DiagnoseResult, error) {
	return DiagnoseResult{}, nil
}

func (r *versionProbeRuntime) RoundTrip(_ context.Context, _ string, _ int, req AgentHTTPRequest) (AgentHTTPResponse, error) {
	body, ok := r.bodies[req.Path]
	if !ok {
		return AgentHTTPResponse{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	return AgentHTTPResponse{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (r *versionProbeRuntime) OpenStream(_ context.Context, _ string, _ int) (io.ReadWriteCloser, error) {
	c1, _ := net.Pipe()
	return c1, nil
}

func probeInstanceState(t *testing.T, provider string, bodies map[string]string) InstanceState {
	t.Helper()
	service := newTestService(t)
	runtime := &versionProbeRuntime{online: true, bodies: bodies}
	service.SetAgentRuntime(runtime)
	ctx := context.Background()
	instance := Instance{
		ServerID: "srv-1",
		Provider: provider,
		Port:     4096,
	}
	// 先确认在线（Probe 返回在线），再触发版本探测
	state := service.probeInstance(ctx, instance)
	return state
}

func TestDetectProviderVersion_v2Matching(t *testing.T) {
	// opencode-v2 实例 + /api/info 返回 2.x → 版本匹配
	state := probeInstanceState(t, "opencode-v2", map[string]string{
		"/api/info": `{"version":"2.0.28","pid":1}`,
	})
	if state.DetectedVersion != "2" {
		t.Fatalf("expected detected version 2, got %q", state.DetectedVersion)
	}
	if state.VersionMismatch {
		t.Fatalf("expected no mismatch for correctly-tagged v2 instance")
	}
}

func TestDetectProviderVersion_v1Matching(t *testing.T) {
	// opencode (v1) 实例 + /global/health 返回 1.x → 版本匹配
	state := probeInstanceState(t, "opencode", map[string]string{
		"/global/health": `{"healthy":true,"version":"1.18.34"}`,
	})
	if state.DetectedVersion != "1" {
		t.Fatalf("expected detected version 1, got %q", state.DetectedVersion)
	}
	if state.VersionMismatch {
		t.Fatalf("expected no mismatch for correctly-tagged v1 instance")
	}
}

func TestDetectProviderVersion_v2TaggedAsV1(t *testing.T) {
	// v2 实例被误标成 opencode (v1)：/api/info 返回 2.x → 版本不符，建议 opencode-v2
	state := probeInstanceState(t, "opencode", map[string]string{
		"/api/info": `{"version":"2.0.28"}`,
	})
	if !state.VersionMismatch {
		t.Fatalf("expected version mismatch")
	}
	if state.VersionSuggestion != "opencode-v2" {
		t.Fatalf("expected suggestion opencode-v2, got %q", state.VersionSuggestion)
	}
	if state.DetectedVersion != "2" {
		t.Fatalf("expected detected version 2, got %q", state.DetectedVersion)
	}
}

func TestDetectProviderVersion_v1TaggedAsV2(t *testing.T) {
	// v1 实例被误标成 opencode-v2：/global/health 返回 1.x → 版本不符，建议 opencode
	state := probeInstanceState(t, "opencode-v2", map[string]string{
		"/global/health": `{"healthy":true,"version":"1.18.34"}`,
	})
	if !state.VersionMismatch {
		t.Fatalf("expected version mismatch")
	}
	if state.VersionSuggestion != "opencode" {
		t.Fatalf("expected suggestion opencode, got %q", state.VersionSuggestion)
	}
	if state.DetectedVersion != "1" {
		t.Fatalf("expected detected version 1, got %q", state.DetectedVersion)
	}
}

func TestDetectProviderVersion_undetectable(t *testing.T) {
	// 两个端点都无法识别 → 版本探测为空，但实例仍保持在线
	state := probeInstanceState(t, "opencode", map[string]string{})
	if state.DetectedVersion != "" {
		t.Fatalf("expected empty detected version, got %q", state.DetectedVersion)
	}
	if state.VersionMismatch {
		t.Fatalf("expected no mismatch when version undetectable")
	}
	if !state.Online {
		t.Fatalf("expected instance to stay online when version undetectable")
	}
}
