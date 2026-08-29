package proxypool

import (
	"context"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func testCfg(t *testing.T) config.Config {
	t.Helper()
	return config.Config{DataDir: t.TempDir(), DBName: "test.db", Version: "test"}
}

func TestPoolCRUD(t *testing.T) {
	ctx := context.Background()
	s := New(testCfg(t))

	// 空列表
	pools, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 0 {
		t.Fatalf("want empty, got %d", len(pools))
	}

	// 创建（含无效项清洗）
	pool, err := s.Create(ctx, "pool-1", "测试池", []string{"socks5://1.2.3.4:1080", "  ", "http://5.6.7.8:8080", "bad://x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pool.Proxies) != 2 {
		t.Fatalf("want 2 cleaned proxies, got %d: %+v", len(pool.Proxies), pool.Proxies)
	}

	got, err := s.Get(ctx, "pool-1")
	if err != nil || got == nil {
		t.Fatalf("get pool: %v %v", got, err)
	}
	if got.Name != "测试池" || len(got.Proxies) != 2 {
		t.Fatalf("unexpected pool: %+v", got)
	}

	// 更新
	if err := s.Update(ctx, "pool-1", "改名", []string{"socks5://1.2.3.4:1080"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, "pool-1")
	if got.Name != "改名" || len(got.Proxies) != 1 {
		t.Fatalf("after update: %+v", got)
	}

	// 删除
	if err := s.Delete(ctx, "pool-1"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, "pool-1")
	if got != nil {
		t.Fatal("pool should be deleted")
	}
}

func TestSelectProxyAndHealth(t *testing.T) {
	ctx := context.Background()
	s := New(testCfg(t))

	proxies := []string{"socks5://a:1080", "socks5://b:1080", "socks5://c:1080"}
	if _, err := s.Create(ctx, "pool-x", "x", proxies); err != nil {
		t.Fatal(err)
	}

	// 轮询：3 次应覆盖 3 个不同代理
	seen := map[string]bool{}
	for i := 0; i < len(proxies); i++ {
		sel, err := s.SelectProxy(ctx, "pool-x", "")
		if err != nil {
			t.Fatal(err)
		}
		if sel == "" {
			t.Fatal("got empty proxy")
		}
		seen[sel] = true
	}
	if len(seen) != len(proxies) {
		t.Fatalf("round-robin should cover all proxies, got %d: %v", len(seen), seen)
	}

	// 429 冻结一个：该代理应从候选中消失
	if err := s.ReportResult(ctx, "pool-x", "socks5://a:1080", false, true, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		sel, _ := s.SelectProxy(ctx, "pool-x", "")
		if sel == "socks5://a:1080" {
			t.Fatalf("frozen proxy selected: %s", sel)
		}
	}

	// 报告成功：解除
	if err := s.ReportResult(ctx, "pool-x", "socks5://a:1080", true, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportResult(ctx, "pool-x", "socks5://a:1080", false, false, nil); err != nil {
		t.Fatal(err)
	}
	// cool 默认 30s，短时间内仍应被跳过
	sel, _ := s.SelectProxy(ctx, "pool-x", "")
	if sel == "socks5://a:1080" {
		t.Fatalf("cooled proxy selected: %s", sel)
	}

	// 解封
	if err := s.UnbanPool(ctx, "pool-x"); err != nil {
		t.Fatal(err)
	}
	count, err := s.BlockedCount(ctx, "pool-x")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("after unban blocked=%d, want 0", count)
	}
}

func TestSelectEmptyPool(t *testing.T) {
	ctx := context.Background()
	s := New(testCfg(t))
	if _, err := s.Create(ctx, "pool-empty", "empty", []string{}); err != nil {
		t.Fatal(err)
	}
	sel, err := s.SelectProxy(ctx, "pool-empty", "")
	if err != nil {
		t.Fatal(err)
	}
	if sel != "" {
		t.Fatalf("empty pool should return empty proxy, got %s", sel)
	}
	sel, err = s.SelectProxy(ctx, "missing", "")
	if err != nil {
		t.Fatal(err)
	}
	if sel != "" {
		t.Fatalf("missing pool should return empty proxy, got %s", sel)
	}
}
