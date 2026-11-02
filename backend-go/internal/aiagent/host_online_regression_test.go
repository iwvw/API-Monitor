package aiagent

import (
	"context"
	"io"
	"testing"
)

// 回归：实例期望状态为 running、主机在线、Agent 支持 lifecycle 时，
// 列表视图必须报告 hostOnline=true 且 lifecycle.supported=true。
//
// 历史 Bug：生产环境的 /api/aiagent/instances 对所有实例返回
// hostOnline=false / supported=false，但同一实例的 /status 与
// /api/aiagent/servers 都报告在线，导致面板与 App 显示「全部离线」。
func TestBuildInstanceViewReportsHostOnlineForManagedInstance(t *testing.T) {
	runtime := &lifecycleStubRuntime{
		online:   true,
		supports: true,
		status: LifecycleResult{
			Running:                true,
			PortListening:          true,
			ListenerMatchesProcess: true,
			PID:                    699301,
			ListenerPID:            699301,
		},
	}

	service := &Service{runtime: runtime}
	instance := Instance{
		ID:           "inst_1",
		ServerID:     "srv-1",
		Provider:     "opencode-v2",
		Port:         4096,
		Enabled:      true,
		DesiredState: DesiredStateRunning,
	}

	view := service.buildInstanceView(context.Background(), instance, true)

	if !view.HostOnline {
		t.Fatalf("hostOnline = false, want true (runtime online=%v supports=%v)", runtime.online, runtime.supports)
	}
	if !view.Lifecycle.Supported {
		t.Fatalf("lifecycle.supported = false, want true")
	}
	if !view.Lifecycle.Running {
		t.Fatalf("lifecycle.running = false, want true")
	}
	if !view.Status.HostOnline {
		t.Fatalf("status.hostOnline = false, want true")
	}
}

// 回归：主机离线时必须明确报告离线，不能因为 lifecycle 查询失败而静默当成「已停止」。
func TestBuildInstanceViewReportsOfflineHost(t *testing.T) {
	runtime := &lifecycleStubRuntime{
		online:   false,
		supports: false,
	}

	service := &Service{runtime: runtime}
	instance := Instance{
		ID:           "inst_3",
		ServerID:     "srv-offline",
		Provider:     "opencode-v2",
		Port:         4096,
		Enabled:      true,
		DesiredState: DesiredStateRunning,
	}

	view := service.buildInstanceView(context.Background(), instance, true)

	if view.HostOnline {
		t.Fatalf("hostOnline = true, want false when agent offline")
	}
	if view.Lifecycle.Supported {
		t.Fatalf("lifecycle.supported = true, want false when agent offline")
	}
}

// 回归：Agent 在线但不支持 lifecycle 能力时，hostOnline 仍应为 true
// （主机是在线的，只是不支持托管），避免 UI 把「不支持托管」误显示成「主机离线」。
func TestBuildInstanceViewHostOnlineWithoutLifecycleCapability(t *testing.T) {
	runtime := &lifecycleStubRuntime{
		online:   true,
		supports: false,
	}

	service := &Service{runtime: runtime}
	instance := Instance{
		ID:           "inst_4",
		ServerID:     "srv-1",
		Provider:     "opencode-v2",
		Port:         4096,
		Enabled:      true,
		DesiredState: DesiredStateRunning,
	}

	view := service.buildInstanceView(context.Background(), instance, true)

	if !view.HostOnline {
		t.Fatalf("hostOnline = false, want true: agent is online, only lifecycle capability is missing")
	}
	if view.Lifecycle.Supported {
		t.Fatalf("lifecycle.supported = true, want false")
	}
}

type lifecycleStubRuntime struct {
	online   bool
	supports bool
	status   LifecycleResult
	probe    ProbeResult
}

func (r *lifecycleStubRuntime) AgentOnline(string) bool { return r.online }

func (r *lifecycleStubRuntime) AgentSupportsAIAgentStream(string) bool { return r.supports }

func (r *lifecycleStubRuntime) AgentSupportsLifecycle(string) bool { return r.supports }

func (r *lifecycleStubRuntime) StartProcess(context.Context, string, LifecycleStartPayload) (LifecycleResult, error) {
	return r.status, nil
}

func (r *lifecycleStubRuntime) StopProcess(context.Context, string, string) (LifecycleResult, error) {
	return r.status, nil
}

func (r *lifecycleStubRuntime) ProcessStatus(context.Context, string, string) (LifecycleResult, error) {
	return r.status, nil
}

func (r *lifecycleStubRuntime) Probe(context.Context, string, string, int, []string) (ProbeResult, error) {
	return r.probe, nil
}

func (r *lifecycleStubRuntime) Diagnose(context.Context, string, string) (DiagnoseResult, error) {
	return DiagnoseResult{}, nil
}

func (r *lifecycleStubRuntime) RoundTrip(context.Context, string, int, AgentHTTPRequest) (AgentHTTPResponse, error) {
	return AgentHTTPResponse{}, nil
}

func (r *lifecycleStubRuntime) OpenStream(context.Context, string, int) (io.ReadWriteCloser, error) {
	return nil, nil
}
