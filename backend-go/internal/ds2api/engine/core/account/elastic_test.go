package account

import (
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func makeElasticAccount(email string, disabled bool, mutedUntil float64) config.Account {
	return config.Account{
		Email:      email,
		Token:      "token-" + email,
		Disabled:   disabled,
		MutedUntil: mutedUntil,
	}
}

func futureMuteUntil() float64 {
	return float64(time.Now().Unix() + 3600)
}

func TestReconcileElasticPoolDisabledNoOp(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.Account{
			makeElasticAccount("a@x.com", false, 0),
			makeElasticAccount("b@x.com", true, 0),
		},
		ElasticPool: config.ElasticPoolConfig{Enabled: false, GlobalCount: 1},
	}
	ReconcileElasticPool(cfg)
	if cfg.Accounts[0].Disabled {
		t.Error("account 0 should remain enabled when elastic pool is off")
	}
	if !cfg.Accounts[1].Disabled {
		t.Error("account 1 should remain disabled when elastic pool is off")
	}
}

func TestReconcileElasticPoolGlobalTopN(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.Account{
			makeElasticAccount("a@x.com", false, 0),
			makeElasticAccount("b@x.com", false, 0),
			makeElasticAccount("c@x.com", false, 0),
			makeElasticAccount("d@x.com", false, 0),
		},
		ElasticPool: config.ElasticPoolConfig{Enabled: true, PerPool: false, GlobalCount: 2},
	}
	ReconcileElasticPool(cfg)
	if cfg.Accounts[0].Disabled {
		t.Error("account 0 (a) should be enabled")
	}
	if cfg.Accounts[1].Disabled {
		t.Error("account 1 (b) should be enabled")
	}
	if !cfg.Accounts[2].Disabled {
		t.Error("account 2 (c) should be disabled")
	}
	if !cfg.Accounts[3].Disabled {
		t.Error("account 3 (d) should be disabled")
	}
}

func TestReconcileElasticPoolMutedDisabledNotCounted(t *testing.T) {
	mute := futureMuteUntil()
	cfg := &config.Config{
		Accounts: []config.Account{
			makeElasticAccount("a@x.com", false, mute),
			makeElasticAccount("b@x.com", false, 0),
			makeElasticAccount("c@x.com", false, 0),
			makeElasticAccount("d@x.com", false, 0),
		},
		ElasticPool: config.ElasticPoolConfig{Enabled: true, PerPool: false, GlobalCount: 2},
	}
	ReconcileElasticPool(cfg)
	if !cfg.Accounts[0].Disabled {
		t.Error("muted account 0 (a) should be disabled")
	}
	if cfg.Accounts[1].Disabled {
		t.Error("account 1 (b) should be enabled to fill the slot")
	}
	if cfg.Accounts[2].Disabled {
		t.Error("account 2 (c) should be enabled to fill the slot")
	}
	if !cfg.Accounts[3].Disabled {
		t.Error("account 3 (d) should be disabled")
	}
}

func TestReconcileElasticPoolBanTriggersBackfillFromFront(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.Account{
			makeElasticAccount("a@x.com", false, 0),
			makeElasticAccount("b@x.com", false, 0),
			makeElasticAccount("c@x.com", false, 0),
		},
		ElasticPool: config.ElasticPoolConfig{Enabled: true, PerPool: false, GlobalCount: 2},
	}
	ReconcileElasticPool(cfg)
	if cfg.Accounts[2].Disabled == false {
		t.Fatal("account 2 (c) should be disabled initially")
	}
	cfg.Accounts[0].MutedUntil = futureMuteUntil()
	ReconcileElasticPool(cfg)
	if !cfg.Accounts[0].Disabled {
		t.Error("banned account 0 (a) should be disabled")
	}
	if cfg.Accounts[1].Disabled {
		t.Error("account 1 (b) should remain enabled")
	}
	if cfg.Accounts[2].Disabled {
		t.Error("account 2 (c) should be backfilled to enabled")
	}
}

func TestReconcileElasticPoolFrontRecoversAfterBanExpires(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.Account{
			makeElasticAccount("a@x.com", false, 0),
			makeElasticAccount("b@x.com", false, 0),
			makeElasticAccount("c@x.com", true, 0),
		},
		ElasticPool: config.ElasticPoolConfig{Enabled: true, PerPool: false, GlobalCount: 2},
	}
	cfg.Accounts[0].MutedUntil = futureMuteUntil()
	ReconcileElasticPool(cfg)
	if !cfg.Accounts[0].Disabled {
		t.Fatal("banned account 0 should be disabled after first reconcile")
	}
	if cfg.Accounts[2].Disabled {
		t.Fatal("account 2 should be backfilled after first reconcile")
	}
	cfg.Accounts[0].MutedUntil = float64(time.Now().Unix() - 1)
	ReconcileElasticPool(cfg)
	if cfg.Accounts[0].Disabled {
		t.Error("account 0 (a) should be re-enabled after ban expires (front priority)")
	}
	if cfg.Accounts[1].Disabled {
		t.Error("account 1 (b) should remain enabled")
	}
	if !cfg.Accounts[2].Disabled {
		t.Error("account 2 (c) should be disabled again after front recovers")
	}
}

func TestReconcileElasticPoolPerPool(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.Account{
			{Email: "d1@x.com", Token: "t1", PoolType: "default"},
			{Email: "d2@x.com", Token: "t2", PoolType: "default"},
			{Email: "d3@x.com", Token: "t3", PoolType: "default"},
			{Email: "n1@x.com", Token: "t4", PoolType: "no_tools"},
			{Email: "n2@x.com", Token: "t5", PoolType: "no_tools"},
		},
		ElasticPool: config.ElasticPoolConfig{
			Enabled:        true,
			PerPool:        true,
			DefaultCount:   2,
			NoToolsCount:   1,
			ToolsOnlyCount: 1,
		},
	}
	ReconcileElasticPool(cfg)
	if cfg.Accounts[0].Disabled {
		t.Error("default account 0 should be enabled")
	}
	if cfg.Accounts[1].Disabled {
		t.Error("default account 1 should be enabled")
	}
	if !cfg.Accounts[2].Disabled {
		t.Error("default account 2 should be disabled")
	}
	if cfg.Accounts[3].Disabled {
		t.Error("no_tools account 0 should be enabled")
	}
	if !cfg.Accounts[4].Disabled {
		t.Error("no_tools account 1 should be disabled")
	}
}

func TestReconcileElasticPoolDefaultCountZeroDisablesAll(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.Account{
			makeElasticAccount("a@x.com", false, 0),
			makeElasticAccount("b@x.com", false, 0),
			makeElasticAccount("c@x.com", false, 0),
		},
		ElasticPool: config.ElasticPoolConfig{Enabled: true, PerPool: false, GlobalCount: 0},
	}
	ReconcileElasticPool(cfg)
	for i, acc := range cfg.Accounts {
		if !acc.Disabled {
			t.Errorf("account %d should be disabled when global_count=0", i)
		}
	}
}

func TestDisableAllAccounts(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.Account{
			makeElasticAccount("a@x.com", true, 0),
			makeElasticAccount("b@x.com", true, 0),
		},
	}
	DisableAllAccounts(cfg)
	for i, acc := range cfg.Accounts {
		if acc.Disabled {
			t.Errorf("account %d should be enabled after DisableAllAccounts", i)
		}
	}
}
