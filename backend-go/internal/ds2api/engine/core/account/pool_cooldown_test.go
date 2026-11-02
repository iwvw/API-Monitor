package account

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func newCooldownPoolForTest(t *testing.T, acc1CooldownOffset, acc2CooldownOffset time.Duration) *Pool {
	t.Helper()
	t.Setenv("DS2API_ACCOUNT_MAX_INFLIGHT", "2")
	t.Setenv("DS2API_ACCOUNT_MAX_QUEUE", "")

	stamp := func(offset time.Duration) float64 {
		if offset == 0 {
			return 0
		}
		return float64(time.Now().Add(offset).Unix())
	}
	t.Setenv("DS2API_CONFIG_JSON", fmt.Sprintf(`{
		"keys":["k1"],
		"accounts":[
			{"email":"acc1@example.com","token":"token1","cooldown_until":%.0f},
			{"email":"acc2@example.com","token":"token2","cooldown_until":%.0f}
		]
	}`, stamp(acc1CooldownOffset), stamp(acc2CooldownOffset)))
	return NewPool(config.LoadStore())
}

// A captcha challenge means risk control already flagged the account. It must
// not be handed straight back out to the next request.
func TestPoolSkipsCoolingDownAccount(t *testing.T) {
	pool := newCooldownPoolForTest(t, 30*time.Minute, 0)

	for i := 0; i < 2; i++ {
		acc, ok := pool.Acquire("", nil, nil)
		if !ok {
			t.Fatalf("acquire %d: expected the non-cooling account to be available", i)
		}
		if acc.Email != "acc2@example.com" {
			t.Fatalf("acquire %d: got %s, want the account that is not cooling down", i, acc.Email)
		}
	}
}

func TestPoolRefusesTargetingACoolingDownAccount(t *testing.T) {
	pool := newCooldownPoolForTest(t, 30*time.Minute, 0)

	if _, ok := pool.Acquire("acc1@example.com", nil, nil); ok {
		t.Fatal("explicitly targeting a cooling-down account should fail")
	}
	if _, ok := pool.Acquire("acc2@example.com", nil, nil); !ok {
		t.Fatal("targeting a healthy account should still work")
	}
}

// The deadline is evaluated live, so an expired cooldown needs no sweeper.
func TestPoolRecoversAfterCooldownExpires(t *testing.T) {
	pool := newCooldownPoolForTest(t, -1*time.Minute, 30*time.Minute)

	acc, ok := pool.Acquire("", nil, nil)
	if !ok {
		t.Fatal("expected the expired-cooldown account to be schedulable again")
	}
	if acc.Email != "acc1@example.com" {
		t.Fatalf("got %s, want acc1@example.com", acc.Email)
	}
}

// With every account cooling down there is nothing to wait for, so the request
// must fail fast rather than hang until its context expires.
func TestPoolDoesNotQueueWhenEveryAccountIsCoolingDown(t *testing.T) {
	pool := newCooldownPoolForTest(t, 30*time.Minute, 30*time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan bool, 1)
	go func() {
		_, ok := pool.AcquireWait(ctx, "", nil, nil)
		done <- ok
	}()

	select {
	case ok := <-done:
		if ok {
			t.Fatal("acquired an account while all of them were cooling down")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("AcquireWait hung instead of failing fast with no schedulable account")
	}
}

func TestIsCoolingDownBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		until float64
		want  bool
	}{
		{"unset", 0, false},
		{"negative", -1, false},
		{"past", float64(time.Now().Add(-time.Minute).Unix()), false},
		{"future", float64(time.Now().Add(time.Minute).Unix()), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acc := config.Account{Email: "a@example.com", CooldownUntil: tc.until}
			if got := acc.IsCoolingDown(); got != tc.want {
				t.Fatalf("IsCoolingDown() = %v, want %v", got, tc.want)
			}
			if got := acc.IsSchedulable(); got != !tc.want {
				t.Fatalf("IsSchedulable() = %v, want %v", got, !tc.want)
			}
		})
	}
}

// Cooldown and mute are independent mechanisms and must not mask each other.
func TestSchedulableCombinesEveryGate(t *testing.T) {
	future := float64(time.Now().Add(time.Hour).Unix())
	cases := map[string]config.Account{
		"disabled":      {Email: "a@example.com", Disabled: true},
		"muted":         {Email: "a@example.com", MutedUntil: future},
		"cooling":       {Email: "a@example.com", CooldownUntil: future},
		"muted+cooling": {Email: "a@example.com", MutedUntil: future, CooldownUntil: future},
	}
	for name, acc := range cases {
		if acc.IsSchedulable() {
			t.Errorf("%s account should not be schedulable", name)
		}
	}
	healthy := config.Account{Email: "a@example.com"}
	if !healthy.IsSchedulable() {
		t.Error("a healthy account should be schedulable")
	}
}
