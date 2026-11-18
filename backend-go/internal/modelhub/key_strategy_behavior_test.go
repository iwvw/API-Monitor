package modelhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// 本文件用「真实转发链路 + 记录上游命中顺序」区分三种 Key 策略的实际行为。
//
// 关键前提：一次客户端请求会经历 1 次初始轮 + endpointRetryRounds(3) 次整轮重试，
// 所以上游命中序列是「多轮拼接」。因此断言必须按「每 keyCount 个为一轮」分段，
// 不能对总数直接断言——否则会把外层重试误读成策略失效（这正是我第一版测试的错）。

const strategyProbeModel = "strategy-probe"

type hitRecorder struct {
	mu    sync.Mutex
	order []string
}

func (h *hitRecorder) record(key string) {
	h.mu.Lock()
	h.order = append(h.order, key)
	h.mu.Unlock()
}

func (h *hitRecorder) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.order))
	copy(out, h.order)
	return out
}

// newStrategyEndpoint 建一个多 key 端点，并把模型直接写进库
// （skipVerify 不拉上游模型列表，models 会为空，端点便不进入候选集）。
func newStrategyEndpoint(t *testing.T, service *Service, upstreamURL string, keys []string, strategy string, retryRounds int) string {
	t.Helper()
	apiKeysJSON, err := json.Marshal(keys[1:])
	if err != nil {
		t.Fatalf("marshal apiKeys: %v", err)
	}
	payload := fmt.Sprintf(`{
		"name": "Strategy %s",
		"baseUrl": "%s",
		"apiKey": "%s",
		"apiKeys": %s,
		"keyStrategy": %q,
		"keyRetryRounds": %d,
		"keyCooldownSeconds": 0,
		"proxyEnabled": false,
		"autoSwitch": false,
		"skipVerify": true
	}`, strategy, upstreamURL, keys[0], string(apiKeysJSON), strategy, retryRounds)

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/api/modelhub/endpoints", strings.NewReader(payload))
	service.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var res struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, w.Body.String(), &res)
	if res.Endpoint.ID == "" {
		t.Fatal("empty endpoint id")
	}

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	seed, _ := json.Marshal([]string{strategyProbeModel})
	_, err = db.ExecContext(context.Background(),
		"UPDATE modelhub_endpoints SET models = ?, disabled_models = '[]' WHERE id = ?",
		string(seed), res.Endpoint.ID)
	db.Close()
	if err != nil {
		t.Fatalf("seed models: %v", err)
	}
	service.invalidateRouteCache()
	return res.Endpoint.ID
}

func sendStrategyChat(t *testing.T, service *Service, endpointID string) int {
	t.Helper()
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":false}`, strategyProbeModel)
	r, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("x-endpoint-id", endpointID)
	service.ServeHTTP(w, r)
	return w.Code
}

// newAlways429Upstream 返回一个恒返回 429 的上游，并把每次命中的 key 记入 recorder。
func newAlways429Upstream(t *testing.T, rec *hitRecorder) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit","type":"rate_limit_exceeded"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestKeyStrategyRoundRobinFollowsStoredOrder 验证「顺序轮询」：
// 每一轮内部严格按配置顺序 k0,k1,k2,k3 推进。
func TestKeyStrategyRoundRobinFollowsStoredOrder(t *testing.T) {
	var rec hitRecorder
	upstream := newAlways429Upstream(t, &rec)

	service := newOpenAIService(t)
	keys := []string{"k0", "k1", "k2", "k3"}
	endpointID := newStrategyEndpoint(t, service, upstream.URL, keys, keyStrategyRoundRobin, 1)

	sendStrategyChat(t, service, endpointID)

	order := rec.snapshot()
	if len(order)%len(keys) != 0 {
		t.Fatalf("round_robin hit count %d is not a multiple of keyCount %d: %v", len(order), len(keys), order)
	}
	for block := 0; block < len(order)/len(keys); block++ {
		got := order[block*len(keys) : (block+1)*len(keys)]
		for i, want := range keys {
			if got[i] != want {
				t.Fatalf("round_robin block %d = %v, want exactly %v (full order %v)", block, got, keys, order)
			}
		}
	}
}

// TestKeyStrategyRandomSurvivorNeverRepeatsWithinRound 验证「错误时随机抽剩余」：
// 每一轮内部是个排列——已失败的 key 不会再被抽中，直到本轮再无存活 key。
func TestKeyStrategyRandomSurvivorNeverRepeatsWithinRound(t *testing.T) {
	var rec hitRecorder
	upstream := newAlways429Upstream(t, &rec)

	service := newOpenAIService(t)
	keys := []string{"k0", "k1", "k2", "k3"}
	endpointID := newStrategyEndpoint(t, service, upstream.URL, keys, keyStrategyRandomSurvivor, 1)

	sendStrategyChat(t, service, endpointID)

	order := rec.snapshot()
	if len(order)%len(keys) != 0 {
		t.Fatalf("random_survivor hit count %d is not a multiple of keyCount %d: %v", len(order), len(keys), order)
	}
	for block := 0; block < len(order)/len(keys); block++ {
		got := order[block*len(keys) : (block+1)*len(keys)]
		seen := map[string]bool{}
		for _, k := range got {
			if seen[k] {
				t.Fatalf("random_survivor repeated key %q within round %d: %v (full order %v)", k, block, got, order)
			}
			seen[k] = true
		}
		if len(seen) != len(keys) {
			t.Fatalf("random_survivor round %d should cover all %d keys, got %v", block, len(keys), got)
		}
	}
}

// TestKeyStrategyRandomHasNoRecencyOrder 验证「完全随机」与 survivor 的差别：
// 它不看本轮失败历史，同一轮内可能重复抽到同一个 key，且不保证每轮覆盖全部 key。
// 用较大轮数把「全部轮次恰好都是排列」的概率压到可忽略。
func TestKeyStrategyRandomHasNoRecencyOrder(t *testing.T) {
	var rec hitRecorder
	upstream := newAlways429Upstream(t, &rec)

	service := newOpenAIService(t)
	keys := []string{"k0", "k1", "k2"}
	// retryRounds 越多，轮数越多，"每轮都恰好是排列"的概率越低
	// （单轮无重复概率 = 3!/3^3 ≈ 0.22，6 轮全无重复 ≈ 1.1e-4）。
	endpointID := newStrategyEndpoint(t, service, upstream.URL, keys, keyStrategyRandom, 6)

	sendStrategyChat(t, service, endpointID)

	order := rec.snapshot()
	if len(order)%len(keys) != 0 {
		t.Fatalf("random hit count %d is not a multiple of keyCount %d: %v", len(order), len(keys), order)
	}
	rounds := len(order) / len(keys)
	sawRepeat := false
	for block := 0; block < rounds; block++ {
		got := order[block*len(keys) : (block+1)*len(keys)]
		seen := map[string]bool{}
		for _, k := range got {
			if seen[k] {
				sawRepeat = true
				break
			}
			seen[k] = true
		}
		if sawRepeat {
			break
		}
	}
	if !sawRepeat {
		t.Fatalf("random should repeat a key within a round across %d rounds (full order %v)", rounds, order)
	}
}

// TestKeyStrategyPersistedAndRespected 验证策略字段从请求落库后确实被 pickKey 采用：
// 同一个端点先按 round_robin 建，再改成 random_survivor，行为应随之改变。
func TestKeyStrategyPersistedAndRespected(t *testing.T) {
	var rec hitRecorder
	upstream := newAlways429Upstream(t, &rec)

	service := newOpenAIService(t)
	keys := []string{"k0", "k1", "k2"}

	for _, strategy := range []string{keyStrategyRoundRobin, keyStrategyRandom, keyStrategyRandomSurvivor} {
		rec.mu.Lock()
		rec.order = nil
		rec.mu.Unlock()

		endpointID := newStrategyEndpoint(t, service, upstream.URL, keys, strategy, 1)

		// 落库值必须与提交值一致（避免 normalizeKeyStrategy 静默改写）。
		db, err := service.open(context.Background())
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		var stored string
		if err := db.QueryRowContext(context.Background(),
			"SELECT key_strategy FROM modelhub_endpoints WHERE id = ?", endpointID).Scan(&stored); err != nil {
			db.Close()
			t.Fatalf("read key_strategy: %v", err)
		}
		db.Close()
		if stored != strategy {
			t.Fatalf("submitted %q but persisted %q", strategy, stored)
		}

		sendStrategyChat(t, service, endpointID)
		if len(rec.snapshot()) == 0 {
			t.Fatalf("strategy %q produced no upstream hits (endpoint not routed?)", strategy)
		}
	}
}