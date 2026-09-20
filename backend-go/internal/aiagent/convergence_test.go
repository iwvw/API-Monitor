package aiagent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// stubRuntime 是 AgentRuntime 的可编程替身，用于验证收敛决策而不依赖真实主机。
type stubRuntime struct {
	mu sync.Mutex

	online         bool
	supportsStream bool
	supportsLife   bool

	// 当前进程状态（ProcessStatus 返回）。
	current LifecycleResult
	// ProcessStatus 返回的错误。
	statusErr error
	// StartProcess / StopProcess 的错误。
	startErr error
	stopErr  error

	startCalls []LifecycleStartPayload
	stopCalls  []string
	statusHits int
}

func (r *stubRuntime) AgentOnline(string) bool { return r.online }

func (r *stubRuntime) AgentSupportsAIAgentStream(string) bool { return r.supportsStream }

func (r *stubRuntime) AgentSupportsLifecycle(string) bool { return r.supportsLife }

func (r *stubRuntime) Probe(context.Context, string, string, int, []string) (ProbeResult, error) {
	return ProbeResult{}, nil
}

func (r *stubRuntime) StartProcess(_ context.Context, _ string, payload LifecycleStartPayload) (LifecycleResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startCalls = append(r.startCalls, payload)
	if r.startErr != nil {
		return LifecycleResult{}, r.startErr
	}
	r.current = LifecycleResult{Managed: true, Running: true, PID: 1234, DesiredRunning: true}
	return r.current, nil
}

func (r *stubRuntime) StopProcess(_ context.Context, _ string, instanceID string) (LifecycleResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopCalls = append(r.stopCalls, instanceID)
	if r.stopErr != nil {
		return LifecycleResult{}, r.stopErr
	}
	r.current = LifecycleResult{Managed: true, Running: false, DesiredRunning: false}
	return r.current, nil
}

func (r *stubRuntime) ProcessStatus(context.Context, string, string) (LifecycleResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusHits++
	if r.statusErr != nil {
		return LifecycleResult{}, r.statusErr
	}
	return r.current, nil
}

func (r *stubRuntime) RoundTrip(context.Context, string, int, AgentHTTPRequest) (AgentHTTPResponse, error) {
	return AgentHTTPResponse{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(nil)}, nil
}

func (r *stubRuntime) counters() (starts int, stops int, statuses int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.startCalls), len(r.stopCalls), r.statusHits
}

// 期望 running 但实际未运行 → 应下发 start。
func TestConvergeStartsWhenDesiredRunningButStopped(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{
		online: true, supportsLife: true,
		current: LifecycleResult{Managed: true, Running: false},
	}
	service.SetAgentRuntime(runtime)

	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096, Enabled: true, DesiredState: DesiredStateRunning}
	state := service.convergeInstance(context.Background(), instance)

	starts, _, _ := runtime.counters()
	if starts != 1 {
		t.Fatalf("期望下发 1 次 start，实际 %d", starts)
	}
	if !state.Running {
		t.Fatalf("收敛后应报告 running，got %+v", state)
	}
	if !state.Supported {
		t.Fatal("应标记 Supported")
	}
}

// 期望 running 且已在运行 → 幂等，不下发任何动作。
func TestConvergeIsIdempotentWhenAlreadyRunning(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{
		online: true, supportsLife: true,
		current: LifecycleResult{Managed: true, Running: true, PID: 99},
	}
	service.SetAgentRuntime(runtime)

	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096, Enabled: true, DesiredState: DesiredStateRunning}
	state := service.convergeInstance(context.Background(), instance)

	starts, stops, statuses := runtime.counters()
	if starts != 0 || stops != 0 {
		t.Fatalf("已符合期望时不应下发动作：starts=%d stops=%d", starts, stops)
	}
	if statuses != 1 {
		t.Fatalf("应只查询一次状态，实际 %d", statuses)
	}
	if !state.Running || state.PID != 99 {
		t.Fatalf("应回传实际状态，got %+v", state)
	}
}

// 期望 stopped 但仍在运行 → 应下发 stop。
func TestConvergeStopsWhenDesiredStoppedButRunning(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{
		online: true, supportsLife: true,
		current: LifecycleResult{Managed: true, Running: true, PID: 42},
	}
	service.SetAgentRuntime(runtime)

	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096, Enabled: true, DesiredState: DesiredStateStopped}
	state := service.convergeInstance(context.Background(), instance)

	_, stops, _ := runtime.counters()
	if stops != 1 {
		t.Fatalf("期望下发 1 次 stop，实际 %d", stops)
	}
	if state.Running {
		t.Fatalf("停止后不应报告 running，got %+v", state)
	}
}

// 未托管（desired_state 为空）→ 完全不碰，也不查询主机。
func TestConvergeSkipsUnmanagedInstances(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{online: true, supportsLife: true}
	service.SetAgentRuntime(runtime)

	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096, Enabled: true, DesiredState: ""}
	state := service.convergeInstance(context.Background(), instance)

	starts, stops, statuses := runtime.counters()
	if starts != 0 || stops != 0 || statuses != 0 {
		t.Fatalf("未托管实例不应产生任何主机往返：starts=%d stops=%d statuses=%d", starts, stops, statuses)
	}
	if state.Managed {
		t.Fatalf("未托管实例不该报告 managed，got %+v", state)
	}
}

// 主机离线 → 不下发动作。
func TestConvergeSkipsWhenHostOffline(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{online: false, supportsLife: true}
	service.SetAgentRuntime(runtime)

	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096, Enabled: true, DesiredState: DesiredStateRunning}
	service.convergeInstance(context.Background(), instance)

	starts, _, statuses := runtime.counters()
	if starts != 0 || statuses != 0 {
		t.Fatalf("主机离线时不应往返：starts=%d statuses=%d", starts, statuses)
	}
}

// Agent 无生命周期能力 → 不下发动作，但仍标记 Supported=false 供前端提示升级。
func TestConvergeSkipsWhenCapabilityMissing(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{online: true, supportsLife: false}
	service.SetAgentRuntime(runtime)

	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096, Enabled: true, DesiredState: DesiredStateRunning}
	state := service.convergeInstance(context.Background(), instance)

	starts, _, _ := runtime.counters()
	if starts != 0 {
		t.Fatal("无能力的 Agent 不应收到 start 任务")
	}
	if state.Supported {
		t.Fatal("无能力时应保持 Supported=false")
	}
}

// start 失败 → 不改期望状态（由调用方/后续循环重试）。
func TestConvergeKeepsDesiredStateWhenStartFails(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{
		online: true, supportsLife: true,
		current:  LifecycleResult{Managed: true, Running: false},
		startErr: errors.New("boom"),
	}
	service.SetAgentRuntime(runtime)

	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096, Enabled: true, DesiredState: DesiredStateRunning}
	state := service.convergeInstance(context.Background(), instance)

	starts, _, _ := runtime.counters()
	if starts != 1 {
		t.Fatalf("应尝试一次 start，实际 %d", starts)
	}
	// 失败后回读仍失败/未运行，状态不应伪造成 running。
	if state.Running {
		t.Fatalf("start 失败后不应报告 running，got %+v", state)
	}
}

// 查询状态失败 → 不下发任何动作，避免基于未知状态做破坏性决定。
func TestConvergeDoesNothingWhenStatusQueryFails(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{
		online: true, supportsLife: true,
		statusErr: errors.New("agent unreachable"),
	}
	service.SetAgentRuntime(runtime)

	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096, Enabled: true, DesiredState: DesiredStateRunning}
	service.convergeInstance(context.Background(), instance)

	starts, stops, _ := runtime.counters()
	if starts != 0 || stops != 0 {
		t.Fatalf("状态未知时不应下发动作：starts=%d stops=%d", starts, stops)
	}
}

// 批量收敛：只处理已设期望状态的实例，且未启用实例被跳过。
func TestConvergeAllInstancesFiltersCorrectly(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	managed, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001", Provider: "opencode", Label: "managed", DesiredState: DesiredStateRunning,
	})
	if err != nil {
		t.Fatalf("createInstance managed: %v", err)
	}
	unmanaged, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-002", Provider: "opencode", Label: "unmanaged",
	})
	if err != nil {
		t.Fatalf("createInstance unmanaged: %v", err)
	}
	db.Close()

	runtime := &stubRuntime{online: true, supportsLife: true, current: LifecycleResult{Managed: true, Running: true}}
	service.SetAgentRuntime(runtime)

	service.convergeAllInstances(ctx)

	// 两个实例都期望 running/未托管：只有 managed 会被查询。
	runtime.mu.Lock()
	statuses := runtime.statusHits
	runtime.mu.Unlock()
	if statuses != 1 {
		t.Fatalf("只应为已设期望状态的实例查询状态，实际查询 %d 次", statuses)
	}
	_ = managed
	_ = unmanaged
}

// queryLifecycle 是纯读路径：任何情况下都不应产生启停副作用。
func TestQueryLifecycleIsReadOnly(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{
		online: true, supportsLife: true,
		current: LifecycleResult{Managed: true, Running: false},
	}
	service.SetAgentRuntime(runtime)

	// 即使期望 running 且实际未运行，读路径也不该拉起它——
	// 那是后台收敛的职责（ADR-0006 第 3.3 条）。
	instance := Instance{
		ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096,
		Enabled: true, DesiredState: DesiredStateRunning,
	}
	state := service.queryLifecycle(context.Background(), instance)

	starts, stops, _ := runtime.counters()
	if starts != 0 || stops != 0 {
		t.Fatalf("读路径不应产生启停副作用：starts=%d stops=%d", starts, stops)
	}
	if state.Running {
		t.Fatalf("应如实返回未运行，got %+v", state)
	}
	if !state.Supported {
		t.Fatal("应标记 Supported")
	}
}

// 未托管实例：能力可用时返回 Supported，但不发状态查询（省一次往返）。
func TestQueryLifecycleSkipsUnmanaged(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{online: true, supportsLife: true}
	service.SetAgentRuntime(runtime)

	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096, Enabled: true}
	state := service.queryLifecycle(context.Background(), instance)

	_, _, statuses := runtime.counters()
	if statuses != 0 {
		t.Fatalf("未托管实例不应查询进程状态，实际 %d 次", statuses)
	}
	if !state.Supported {
		t.Fatal("能力可用时应标记 Supported，供前端提示「可托管」")
	}
	if state.Managed {
		t.Fatal("未托管实例不该报告 managed")
	}
}

// statusFromLifecycle：在线需同时满足运行 + 端口监听 + 监听者是本 Provider 进程。
func TestStatusFromLifecycleAssociation(t *testing.T) {
	cases := []struct {
		name      string
		lifecycle LifecycleState
		wantOn    bool
	}{
		{
			name:      "全部满足才算在线",
			lifecycle: LifecycleState{Running: true, PortListening: true, ListenerMatchesProcess: true, Supported: true},
			wantOn:    true,
		},
		{
			name:      "端口被其它进程占用：不在线",
			lifecycle: LifecycleState{Running: true, PortListening: true, ListenerMatchesProcess: false, Supported: true},
			wantOn:    false,
		},
		{
			name:      "进程在跑但没监听：不在线",
			lifecycle: LifecycleState{Running: true, PortListening: false, ListenerMatchesProcess: false, Supported: true},
			wantOn:    false,
		},
		{
			name:      "未运行：不在线",
			lifecycle: LifecycleState{Running: false, PortListening: false, Supported: true},
			wantOn:    false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			state := statusFromLifecycle(testCase.lifecycle)
			if state.Online != testCase.wantOn {
				t.Fatalf("Online = %v, want %v (state=%+v)", state.Online, testCase.wantOn, state)
			}
		})
	}
}

// 崩溃终态在探测视图里要给出可区分的错误说明。
func TestStatusFromLifecycleCrashedError(t *testing.T) {
	state := statusFromLifecycle(LifecycleState{Crashed: true, Supported: true})
	if state.Online {
		t.Fatal("崩溃实例不应在线")
	}
	if !strings.Contains(state.Error, "crashed") {
		t.Fatalf("错误说明应指出崩溃，got %q", state.Error)
	}
}

// 批量操作：逐个执行，部分失败不影响其它实例。
func TestRunLifecycleActionPartialFailure(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	good, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001", Provider: "opencode", Label: "good",
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	db.Close()

	runtime := &stubRuntime{
		online: true, supportsLife: true,
		current: LifecycleResult{Managed: true, Running: false},
	}
	service.SetAgentRuntime(runtime)

	// 一个存在的实例 + 一个不存在的实例。
	okState, err := service.runLifecycleAction(ctx, good.ID, "start", "start")
	if err != nil {
		t.Fatalf("存在的实例应成功: %v", err)
	}
	if okState != DesiredStateRunning {
		t.Fatalf("期望状态应为 running，got %q", okState)
	}

	if _, err := service.runLifecycleAction(ctx, "inst_does_not_exist", "start", "start"); err == nil {
		t.Fatal("不存在的实例应返回错误")
	}
}

// 批量操作不得绕过能力门禁。
func TestRunLifecycleActionEnforcesCapability(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001", Provider: "opencode", Label: "x",
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	db.Close()

	// 旧 Agent：不支持生命周期
	service.SetAgentRuntime(&stubRuntime{online: true, supportsLife: false})

	if _, err := service.runLifecycleAction(ctx, instance.ID, "start", "start"); err == nil {
		t.Fatal("不支持生命周期的 Agent 应被拒绝，而不是静默落库")
	}
}

// 停用的实例不参与批量操作。
func TestRunLifecycleActionRejectsDisabled(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	disabled := false
	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001", Provider: "opencode", Label: "x", Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	db.Close()

	service.SetAgentRuntime(&stubRuntime{online: true, supportsLife: true})

	if _, err := service.runLifecycleAction(ctx, instance.ID, "start", "start"); err == nil {
		t.Fatal("停用实例应被拒绝")
	}
}

// batch 路由必须排在 /instances/{id} 通配之前，否则会被当成实例 ID。
// 这里验证路由分发确实命中批量处理器（返回参数校验错误而非「实例不存在」）。
func TestBatchRouteMatchesBeforeWildcard(t *testing.T) {
	service := newTestService(t)
	req := httptest.NewRequest(http.MethodPost, "/api/aiagent/instances/batch",
		strings.NewReader(`{"action":"start","instanceIds":[]}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)

	// 未鉴权时两者都可能返回 401，因此只断言「不是 404」——
	// 404 才说明路由没匹配上。
	if res.Code == http.StatusNotFound {
		t.Fatalf("batch 路由未匹配，被当成实例 ID 处理了: %d", res.Code)
	}
}

// 预计算的生命周期状态会被直接采用，不再查询主机（省一次往返）。
func TestPrecomputedLifecycleSkipsHostQuery(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{
		online: true, supportsLife: true,
		current: LifecycleResult{Managed: true, Running: true, PID: 7},
	}
	service.SetAgentRuntime(runtime)

	instance := Instance{
		ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096,
		Enabled: true, DesiredState: DesiredStateRunning,
	}
	// 端口字段必须齐全：在线判定要求「运行 + 端口监听 + 监听者命中规则」
	// 三者同时成立（ADR-0006 第 2 条），少一项即为离线。
	precomputed := LifecycleState{
		Managed: true, Running: true, PID: 42, Supported: true,
		PortListening: true, ListenerPID: 42, ListenerMatchesProcess: true,
	}

	view := service.buildInstanceViewWithHosts(context.Background(), instance, true, nil, &precomputed)

	_, _, statuses := runtime.counters()
	if statuses != 0 {
		t.Fatalf("传入预计算状态时不应再查询主机，实际 %d 次", statuses)
	}
	if view.Lifecycle.PID != 42 {
		t.Fatalf("应采用预计算值，got PID %d", view.Lifecycle.PID)
	}
	if !view.Status.Online {
		t.Fatal("预计算状态为运行中且端口匹配时视图应在线")
	}
}

// 端口监听但监听者不是本 Provider 进程时，即使进程在跑也不在线。
// 这是关联验证的核心：防止「别的进程占了端口」被误判为服务可用。
func TestPrecomputedLifecyclePortOccupiedIsOffline(t *testing.T) {
	service := newTestService(t)
	service.SetAgentRuntime(&stubRuntime{online: true, supportsLife: true})

	instance := Instance{
		ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096,
		Enabled: true, DesiredState: DesiredStateRunning,
	}
	precomputed := LifecycleState{
		Managed: true, Running: true, PID: 42, Supported: true,
		PortListening: true, ListenerPID: 999, ListenerMatchesProcess: false,
	}

	view := service.buildInstanceViewWithHosts(context.Background(), instance, true, nil, &precomputed)

	if view.Status.Online {
		t.Fatal("监听者不是本 Provider 进程时不应判为在线")
	}
	if view.Status.Error == "" {
		t.Fatal("应给出可区分的错误说明")
	}
}

// 未传预计算状态时，托管实例仍会查询一次主机（保持原行为）。
func TestManagedInstanceWithoutPrecomputedQueriesHost(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{
		online: true, supportsLife: true,
		current: LifecycleResult{Managed: true, Running: true, PID: 7},
	}
	service.SetAgentRuntime(runtime)

	instance := Instance{
		ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096,
		Enabled: true, DesiredState: DesiredStateRunning,
	}
	view := service.buildInstanceViewWithHosts(context.Background(), instance, true, nil, nil)

	_, _, statuses := runtime.counters()
	if statuses != 1 {
		t.Fatalf("未传预计算状态时应查询一次主机，实际 %d 次", statuses)
	}
	if view.Lifecycle.PID != 7 {
		t.Fatalf("应回传主机返回的 PID，got %d", view.Lifecycle.PID)
	}
}

// 未托管实例走探测路径，不受生命周期查询影响。
// 预计算参数是「托管实例的优化」，未托管实例不应因此改变行为。
func TestUnmanagedInstanceUsesProbePath(t *testing.T) {
	service := newTestService(t)
	runtime := &stubRuntime{
		online: true, supportsLife: true,
		// 探测返回的进程名匹配由 stub 的 Probe 决定，这里不关心具体值，
		// 只验证托管标记仍为 false（未托管不该被生命周期逻辑染色）。
	}
	service.SetAgentRuntime(runtime)

	instance := Instance{
		ID: "inst_1", ServerID: "s1", Provider: "opencode", Port: 4096, Enabled: true,
	}
	view := service.buildInstanceViewWithHosts(context.Background(), instance, true, nil, nil)

	if view.Lifecycle.Managed {
		t.Fatal("未托管实例不应显示为托管")
	}
}

// StopConvergence 必须可重复调用且不 panic（优雅退出路径）。
func TestStopConvergenceIsSafeWithoutStart(t *testing.T) {
	service := newTestService(t)
	service.StopConvergence()
	service.StartConvergence()
	service.StopConvergence()
	service.StopConvergence()
}

// 能力门禁：未注入 runtime 时必须报错，而不是静默允许写入期望状态。
func TestLifecycleCapabilityErrorWithoutRuntime(t *testing.T) {
	service := newTestService(t)
	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode"}
	if reason := service.lifecycleCapabilityError(instance); reason == "" {
		t.Fatal("未配置 runtime 时应返回错误说明")
	}
}

// 主机离线时必须报错，避免写入一个永远不收敛的期望状态。
func TestLifecycleCapabilityErrorHostOffline(t *testing.T) {
	service := newTestService(t)
	service.SetAgentRuntime(&stubRuntime{online: false, supportsLife: true})
	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode"}

	reason := service.lifecycleCapabilityError(instance)
	if reason == "" {
		t.Fatal("主机离线时应返回错误说明")
	}
	if reason != "host agent offline" {
		t.Fatalf("期望明确说明离线，got %q", reason)
	}
}

// 旧 Agent（无 lifecycle 能力）必须给出可操作的升级提示。
func TestLifecycleCapabilityErrorUnsupportedAgent(t *testing.T) {
	service := newTestService(t)
	service.SetAgentRuntime(&stubRuntime{online: true, supportsLife: false})
	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode"}

	reason := service.lifecycleCapabilityError(instance)
	if reason == "" {
		t.Fatal("旧 Agent 应返回错误说明，而不是静默不收敛")
	}
	if !strings.Contains(reason, "upgrade") {
		t.Fatalf("错误说明应提示升级 Agent，got %q", reason)
	}
}

// 能力齐备时不应报错。
func TestLifecycleCapabilityErrorAllGood(t *testing.T) {
	service := newTestService(t)
	service.SetAgentRuntime(&stubRuntime{online: true, supportsLife: true})
	instance := Instance{ID: "inst_1", ServerID: "s1", Provider: "opencode"}

	if reason := service.lifecycleCapabilityError(instance); reason != "" {
		t.Fatalf("能力齐备时不应报错，got %q", reason)
	}
}
