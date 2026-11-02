package client

import (
	"testing"
	"time"
)

func TestIsFreshChallenge(t *testing.T) {
	now := time.Now().Unix()
	tests := []struct {
		name  string
		chal  map[string]any
		fresh bool
	}{
		{
			name:  "nil challenge",
			chal:  nil,
			fresh: false,
		},
		{
			name:  "no expire_at",
			chal:  map[string]any{"algorithm": "DeepSeekHashV1"},
			fresh: false,
		},
		{
			name:  "expired",
			chal:  map[string]any{"expire_at": float64(now - 100)},
			fresh: false,
		},
		{
			name:  "expires within margin",
			chal:  map[string]any{"expire_at": float64(now + 10)},
			fresh: false,
		},
		{
			name:  "fresh",
			chal:  map[string]any{"expire_at": float64(now + 120)},
			fresh: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFreshChallenge(tt.chal); got != tt.fresh {
				t.Fatalf("expected %v, got %v", tt.fresh, got)
			}
		})
	}
}

func TestPowChallengeCacheGetSet(t *testing.T) {
	cache := newPowChallengeCache()
	now := time.Now().Unix()
	challenge := map[string]any{
		"algorithm":   "DeepSeekHashV1",
		"challenge":   "abc",
		"salt":        "salt",
		"expire_at":   float64(now + 120),
		"difficulty":  float64(144000),
		"signature":   "sig",
		"target_path": "/api/v0/chat/completion",
	}

	cache.set("acc1", "/api/v0/chat/completion", challenge)

	got, ok := cache.get("acc1", "/api/v0/chat/completion")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if got["challenge"] != "abc" {
		t.Fatalf("unexpected challenge: %v", got["challenge"])
	}

	if _, ok := cache.get("acc1", "/api/v0/chat/completion"); ok {
		t.Fatal("expected cache miss after get (get should consume), got hit")
	}
}

func TestPowChallengeCacheStaleEntryEvicted(t *testing.T) {
	cache := newPowChallengeCache()
	stale := map[string]any{
		"expire_at": float64(time.Now().Unix() - 100),
	}
	cache.set("acc1", "/path", stale)
	if _, ok := cache.get("acc1", "/path"); ok {
		t.Fatal("expected stale entry to be evicted, got hit")
	}
}

func TestPowChallengeCacheDifferentAccounts(t *testing.T) {
	cache := newPowChallengeCache()
	now := time.Now().Unix()
	chal1 := map[string]any{"challenge": "c1", "expire_at": float64(now + 120)}
	chal2 := map[string]any{"challenge": "c2", "expire_at": float64(now + 120)}

	cache.set("acc1", "/path", chal1)
	cache.set("acc2", "/path", chal2)

	got1, ok1 := cache.get("acc1", "/path")
	if !ok1 || got1["challenge"] != "c1" {
		t.Fatalf("expected acc1 to have c1, got ok=%v chal=%v", ok1, got1)
	}
	got2, ok2 := cache.get("acc2", "/path")
	if !ok2 || got2["challenge"] != "c2" {
		t.Fatalf("expected acc2 to have c2, got ok=%v chal=%v", ok2, got2)
	}
}

func TestCreateRandomDeviceID(t *testing.T) {
	id1, err := createRandomDeviceID()
	if err != nil {
		t.Fatalf("createRandomDeviceID failed: %v", err)
	}
	if len(id1) < 2 || id1[0] != 'B' {
		t.Fatalf("expected device id to start with B, got %q", id1)
	}
	id2, err := createRandomDeviceID()
	if err != nil {
		t.Fatalf("second createRandomDeviceID failed: %v", err)
	}
	if id1 == id2 {
		t.Fatal("expected two random device ids to differ")
	}
}
