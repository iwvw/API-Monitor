package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestPickKeyCountsPerKeyTries 验证 pickKey 的每 key 计数语义：
// maxTries=2 时每个 key 在同一请求内可被选中两次，全部 key 达到上限后返回空。
func TestPickKeyCountsPerKeyTries(t *testing.T) {
	s := newOpenAIService(t)
	keys := []string{"primary", "ext-a", "ext-b"}

	tried := map[string]int{}
	picks := []string{}
	for {
		k, _ := s.pickKey("ep-1", keys, tried, 2)
		if k == "" {
			break
		}
		picks = append(picks, k)
		tried[k]++
	}
	if len(picks) != 6 {
		t.Fatalf("with 3 keys and maxTries=2 expect 6 picks, got %d: %v", len(picks), picks)
	}
	counts := map[string]int{}
	for _, k := range picks {
		counts[k]++
	}
	for _, k := range keys {
		if counts[k] != 2 {
			t.Fatalf("key %q picked %d times, want 2 (counts=%v)", k, counts[k], counts)
		}
	}

	// maxTries=1 时退化为旧的「每 key 一次」语义。
	tried1 := map[string]int{}
	picks1 := []string{}
	for {
		k, _ := s.pickKey("ep-2", keys, tried1, 1)
		if k == "" {
			break
		}
		picks1 = append(picks1, k)
		tried1[k]++
	}
	if len(picks1) != 3 {
		t.Fatalf("with maxTries=1 expect 3 picks, got %d", len(picks1))
	}

	// 轮询顺序保持 round-robin：cursor 连续推进不重复同一 key。
	seen := map[string]bool{}
	for _, k := range picks1 {
		if seen[k] {
			t.Fatalf("maxTries=1 must not repeat key %q", k)
		}
		seen[k] = true
	}
}

// TestEffectiveKeyRetryRoundsDefault 验证未配置（0/负值）回退默认 2。
func TestEffectiveKeyRetryRoundsDefault(t *testing.T) {
	if got := (Endpoint{}).effectiveKeyRetryRounds(); got != 2 {
		t.Fatalf("zero KeyRetryRounds should default to 2, got %d", got)
	}
	if got := (Endpoint{KeyRetryRounds: -3}).effectiveKeyRetryRounds(); got != 2 {
		t.Fatalf("negative KeyRetryRounds should default to 2, got %d", got)
	}
	if got := (Endpoint{KeyRetryRounds: 5}).effectiveKeyRetryRounds(); got != 5 {
		t.Fatalf("explicit KeyRetryRounds=5 should be honored, got %d", got)
	}
}

// TestMultiKeyRetriesRoundsWithinRequest 端到端验证「循环两遍」：
// 双 key 端点（主 key k1 + 扩展 key k2），两把 key 前 2 次都 401、第 3 次仍 401。
// keyRetryRounds=2 时每个 key 应各被尝试 2 次（共 4 次上游 401）后才判端点耗尽；
// keyRetryRounds=1 时共 2 次即耗尽（旧行为）。
func TestMultiKeyRetriesRoundsWithinRequest(t *testing.T) {
	run := func(rounds int) int32 {
		var hits int32
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&hits, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":{"message":"401 hit %d"}}`, n)
		}))
		t.Cleanup(upstream.Close)

		service := newOpenAIService(t)
		createPayload := fmt.Sprintf(`{
			"name": "MultiKey Rounds",
			"baseUrl": "%s",
			"apiKey": "k1",
			"apiKeys": ["k2"],
			"keyRetryRounds": %d,
			"skipVerify": true
		}`, upstream.URL, rounds)
		wCreate := httptest.NewRecorder()
		rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
		service.ServeHTTP(wCreate, rCreate)
		if wCreate.Code != http.StatusOK {
			t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
		}
		var createRes struct {
			Endpoint Endpoint `json:"endpoint"`
		}
		mustDecode(t, wCreate.Body.String(), &createRes)
		if createRes.Endpoint.KeyRetryRounds != rounds {
			t.Fatalf("KeyRetryRounds not persisted, got %d", createRes.Endpoint.KeyRetryRounds)
		}

		db, err := service.open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, createRes.Endpoint.ID); err != nil {
			db.Close()
			t.Fatal(err)
		}
		db.Close()

		w := chatRequest(t, service, createRes.Endpoint.ID, false, "")
		// 全部 key 401 且唯一候选端点：聚合为「所有端点均返回 HTTP 401」的 401。
		if w.Code != http.StatusUnauthorized && w.Code != http.StatusBadGateway && w.Code != http.StatusServiceUnavailable && w.Code != http.StatusInternalServerError {
			t.Fatalf("all-401 request should fail, got %d body=%s", w.Code, w.Body.String())
		}
		return atomic.LoadInt32(&hits)
	}

	// rounds=2 与 rounds=1 一样都会在 failover 层做整轮重试（endpointRetryRounds），
	// 但单轮内的 key 尝试数翻倍（2 key × 2 与 2 key × 1）。相对命中数应精确 2 倍。
	byRounds2 := run(2)
	byRounds1 := run(1)
	if byRounds2 != byRounds1*2 {
		t.Fatalf("rounds=2 (%d hits) must be exactly 2x rounds=1 (%d hits)", byRounds2, byRounds1)
	}
}

// TestMultiKeySecondRoundSucceeds 验证第二遍循环里 key 恢复可用时请求最终成功：
// 双 key 均先 401 一次，随后恢复 200 —— keyRetryRounds=2 应在第 3 次尝试成功。
func TestMultiKeySecondRoundSucceeds(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) <= 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"transient"}}`))
			return
		}
		okHandler(w, r)
	}))
	t.Cleanup(upstream.Close)

	service := newOpenAIService(t)
	createPayload := fmt.Sprintf(`{
		"name": "Second Round",
		"baseUrl": "%s",
		"apiKey": "k1",
		"apiKeys": ["k2"],
		"keyRetryRounds": 2,
		"skipVerify": true
	}`, upstream.URL)
	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, createRes.Endpoint.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	w := chatRequest(t, service, createRes.Endpoint.ID, false, "")
	if w.Code != http.StatusOK {
		t.Fatalf("second-round recovery should succeed, got %d body=%s", w.Code, w.Body.String())
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("expect 3 attempts (2 failures + 1 success), got %d", got)
	}
}

// TestKeyRetryRoundsPersistsThroughUpdate 验证 keyRetryRounds 更新链路：
// 未提交字段时保留存量；显式提交时更新为新值。
func TestKeyRetryRoundsPersistsThroughUpdate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"id":"gpt-4","object":"model"}]}`))
	}))
	t.Cleanup(upstream.Close)

	service := newOpenAIService(t)
	createPayload := fmt.Sprintf(`{"name":"Rounds","baseUrl":"%s","apiKey":"k","keyRetryRounds":3,"skipVerify":true}`, upstream.URL)
	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	if createRes.Endpoint.KeyRetryRounds != 3 {
		t.Fatalf("create should persist keyRetryRounds=3, got %d", createRes.Endpoint.KeyRetryRounds)
	}
	id := createRes.Endpoint.ID

	list := func() Endpoint {
		w := httptest.NewRecorder()
		r, _ := http.NewRequest("GET", "/api/openai/endpoints", nil)
		service.ServeHTTP(w, r)
		var eps []Endpoint
		mustDecode(t, w.Body.String(), &eps)
		for _, ep := range eps {
			if ep.ID == id {
				return ep
			}
		}
		t.Fatalf("endpoint %s not found", id)
		return Endpoint{}
	}

	// 未提交 keyRetryRounds 的局部更新：保留存量 3。
	wU := httptest.NewRecorder()
	rU, _ := http.NewRequest("PUT", "/api/openai/endpoints/"+id, strings.NewReader(fmt.Sprintf(`{"name":"Rounds Renamed"}`)))
	service.ServeHTTP(wU, rU)
	if wU.Code != http.StatusOK {
		t.Fatalf("partial update status = %d body=%s", wU.Code, wU.Body.String())
	}
	if got := list().KeyRetryRounds; got != 3 {
		t.Fatalf("partial update must preserve keyRetryRounds=3, got %d", got)
	}

	// 全量提交（不带 keyRetryRounds 字段）：同样保留存量 3。
	wU2 := httptest.NewRecorder()
	rU2, _ := http.NewRequest("PUT", "/api/openai/endpoints/"+id, strings.NewReader(fmt.Sprintf(`{"name":"Rounds","baseUrl":"%s","apiKey":"k"}`, upstream.URL)))
	service.ServeHTTP(wU2, rU2)
	if wU2.Code != http.StatusOK {
		t.Fatalf("full update status = %d body=%s", wU2.Code, wU2.Body.String())
	}
	if got := list().KeyRetryRounds; got != 3 {
		t.Fatalf("full update without field must preserve keyRetryRounds=3, got %d", got)
	}

	// 显式提交新值：更新为 5。
	wU3 := httptest.NewRecorder()
	rU3, _ := http.NewRequest("PUT", "/api/openai/endpoints/"+id, strings.NewReader(fmt.Sprintf(`{"name":"Rounds","keyRetryRounds":5}`)))
	service.ServeHTTP(wU3, rU3)
	if wU3.Code != http.StatusOK {
		t.Fatalf("rounds update status = %d body=%s", wU3.Code, wU3.Body.String())
	}
	if got := list().KeyRetryRounds; got != 5 {
		t.Fatalf("explicit keyRetryRounds=5 should persist, got %d", got)
	}
}
