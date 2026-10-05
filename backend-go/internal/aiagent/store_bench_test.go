package aiagent

import (
	"context"
	"testing"
)

// BenchmarkStoreOpenOnly 量出 store.Open → Close 本身的开销（不含任何 SQL）。
func BenchmarkStoreOpenOnly(b *testing.B) {
	ctx := context.Background()
	service := New(benchConfig(b))
	if err := service.Initialize(ctx); err != nil {
		b.Fatalf("Initialize: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db, err := service.store.Open(ctx)
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		db.Close()
	}
}

// BenchmarkStoreOpenOnlyPinned 与上者相同，但先持有一个常驻句柄（模拟生产
// pin）：验证「池保持常暖」后 Open→Close 是否显著变快。
func BenchmarkStoreOpenOnlyPinned(b *testing.B) {
	ctx := context.Background()
	service := New(benchConfig(b))
	if err := service.Initialize(ctx); err != nil {
		b.Fatalf("Initialize: %v", err)
	}
	release, err := service.store.Pin(ctx)
	if err != nil {
		b.Fatalf("Pin: %v", err)
	}
	defer release()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db, err := service.store.Open(ctx)
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		db.Close()
	}
}

// BenchmarkBareInsert 量出「Open → 单条 INSERT → Close」的开销。
func BenchmarkBareInsert(b *testing.B) {
	ctx := context.Background()
	service := New(benchConfig(b))
	if err := service.Initialize(ctx); err != nil {
		b.Fatalf("Initialize: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db, err := service.store.Open(ctx)
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO aiagent_access_logs
			(user_id, token_id, instance_id, action, result, status_code, error_summary, ip, user_agent, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			nil, nil, "inst", "gw", "ok", 200, nil, "127.0.0.1", "bench", "2026-01-01T00:00:00Z"); err != nil {
			b.Fatalf("insert: %v", err)
		}
		db.Close()
	}
}
