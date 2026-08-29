package account

import (
	"context"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// newMixedPoolForTest 构建一个包含三种号池类型的测试池：
//   - acc1: default
//   - acc2: no_tools
//   - acc3: tools_only
func newMixedPoolForTest(t *testing.T) *Pool {
	t.Helper()
	t.Setenv("DS2API_ACCOUNT_MAX_INFLIGHT", "1")
	t.Setenv("DS2API_ACCOUNT_MAX_QUEUE", "")
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["k1"],
		"accounts":[
			{"email":"acc1@example.com","token":"token1","pool_type":"default"},
			{"email":"acc2@example.com","token":"token2","pool_type":"no_tools"},
			{"email":"acc3@example.com","token":"token3","pool_type":"tools_only"}
		]
	}`)
	return NewPool(config.LoadStore())
}

func toolsPresentFilter(present bool) AccountFilter {
	return func(acc config.Account) bool {
		return acc.MatchesPoolType(present)
	}
}

func TestPoolAcquireFilterSkipsNonMatchingAccounts(t *testing.T) {
	pool := newMixedPoolForTest(t)

	// toolsPresent=true 的请求：应跳过 acc2 (no_tools)，可获取 acc1 或 acc3
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		acc, ok := pool.Acquire("", nil, toolsPresentFilter(true))
		if !ok {
			t.Fatalf("expected acquire success at step %d with tools-enabled filter", i+1)
		}
		seen[acc.Identifier()] = true
		pool.Release(acc.Identifier())
	}
	if seen["acc2@example.com"] {
		t.Fatal("no_tools account should never be acquired with tools-enabled filter")
	}
	if !seen["acc1@example.com"] || !seen["acc3@example.com"] {
		t.Fatalf("expected both default and tools_only accounts to be acquired, seen=%v", seen)
	}
}

func TestPoolAcquireFilterToolsDisabledRequestSkipsToolsOnly(t *testing.T) {
	pool := newMixedPoolForTest(t)

	// toolsPresent=false 的请求：应跳过 acc3 (tools_only)，可获取 acc1 或 acc2
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		acc, ok := pool.Acquire("", nil, toolsPresentFilter(false))
		if !ok {
			t.Fatalf("expected acquire success at step %d with tools-disabled filter", i+1)
		}
		seen[acc.Identifier()] = true
		pool.Release(acc.Identifier())
	}
	if seen["acc3@example.com"] {
		t.Fatal("tools_only account should never be acquired with tools-disabled filter")
	}
	if !seen["acc1@example.com"] || !seen["acc2@example.com"] {
		t.Fatalf("expected both default and no_tools accounts to be acquired, seen=%v", seen)
	}
}

func TestPoolAcquireTargetRejectedByFilter(t *testing.T) {
	pool := newMixedPoolForTest(t)

	// 指定 acc2 (no_tools) 但 filter 要求 tools-enabled，应被拒绝
	if _, ok := pool.Acquire("acc2@example.com", nil, toolsPresentFilter(true)); ok {
		t.Fatal("expected target acquire on no_tools account to fail with tools-enabled filter")
	}
	// 指定 acc3 (tools_only) 但 filter 要求 tools-disabled，应被拒绝
	if _, ok := pool.Acquire("acc3@example.com", nil, toolsPresentFilter(false)); ok {
		t.Fatal("expected target acquire on tools_only account to fail with tools-disabled filter")
	}
	// 指定 acc1 (default) 应总是成功
	if _, ok := pool.Acquire("acc1@example.com", nil, toolsPresentFilter(true)); !ok {
		t.Fatal("expected target acquire on default account to succeed with tools-enabled filter")
	}
}

func TestPoolAcquireWaitReturnsFalseWhenNoAccountMatchesFilter(t *testing.T) {
	t.Setenv("DS2API_ACCOUNT_MAX_INFLIGHT", "1")
	t.Setenv("DS2API_ACCOUNT_MAX_QUEUE", "5")
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["k1"],
		"accounts":[
			{"email":"acc1@example.com","token":"token1","pool_type":"no_tools"},
			{"email":"acc2@example.com","token":"token2","pool_type":"no_tools"}
		]
	}`)
	pool := NewPool(config.LoadStore())

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, ok := pool.AcquireWait(ctx, "", nil, toolsPresentFilter(true)); ok {
		t.Fatal("expected AcquireWait to fail when no account matches tools-enabled filter")
	}
	// 应快速失败，不应等待到超时
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("expected fast failure when no account matches filter, took %s", elapsed)
	}
}

func TestPoolAcquireWaitQueuesForMatchingAccountOnly(t *testing.T) {
	t.Setenv("DS2API_ACCOUNT_MAX_INFLIGHT", "1")
	t.Setenv("DS2API_ACCOUNT_MAX_QUEUE", "5")
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["k1"],
		"accounts":[
			{"email":"acc1@example.com","token":"token1","pool_type":"default"},
			{"email":"acc2@example.com","token":"token2","pool_type":"no_tools"}
		]
	}`)
	pool := NewPool(config.LoadStore())

	// 占用 acc1 (default) — tools-enabled 请求只能用 acc1
	acc1, ok := pool.Acquire("acc1@example.com", nil, toolsPresentFilter(true))
	if !ok {
		t.Fatal("expected to acquire acc1 with tools-enabled filter")
	}

	// 另一个 tools-enabled 请求应排队等待 acc1 释放（acc2 是 no_tools 不匹配）
	type result struct {
		id string
		ok bool
	}
	resCh := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		acc, ok := pool.AcquireWait(ctx, "", nil, toolsPresentFilter(true))
		resCh <- result{id: acc.Identifier(), ok: ok}
	}()

	waitForWaitingCount(t, pool, 1)
	pool.Release(acc1.Identifier())

	select {
	case res := <-resCh:
		if !res.ok || res.id != "acc1@example.com" {
			t.Fatalf("expected queued acquire to get acc1 after release, got ok=%v id=%q", res.ok, res.id)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued acquire result")
	}
}
