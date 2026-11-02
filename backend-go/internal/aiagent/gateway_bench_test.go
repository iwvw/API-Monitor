package aiagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// benchConfig 返回一个用临时目录的测试配置。
func benchConfig(b *testing.B) config.Config {
	b.Helper()
	return config.Config{
		Version: "bench",
		DataDir: b.TempDir(),
		DBName:  "aiagent_bench.db",
	}
}

// BenchmarkGatewayAuthPath 量出网关「鉴权 + 实例查询 + 授权检查」这条热路径
// 的真实 DB 开销。这是每个经网关的请求都要走的一段，与上游转发无关。
func BenchmarkGatewayAuthPath(b *testing.B) {
	ctx := context.Background()
	service := New(benchConfig(b))
	if err := service.Initialize(ctx); err != nil {
		b.Fatalf("Initialize: %v", err)
	}

	db, err := service.store.Open(ctx)
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	user, err := service.createUser(ctx, db, "bench", "bench-pass-1", "")
	if err != nil {
		b.Fatalf("createUser: %v", err)
	}
	plain, token, err := service.issueToken(ctx, db, user.ID, "bench", "")
	if err != nil {
		b.Fatalf("issueToken: %v", err)
	}
	_ = token
	instance, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-bench", Provider: "opencode", Label: "bench"})
	if err != nil {
		b.Fatalf("createInstance: %v", err)
	}
	if err := service.setGrantsForUser(ctx, db, user.ID, []string{instance.ID}); err != nil {
		b.Fatalf("setGrantsForUser: %v", err)
	}
	db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 模拟 handleGateway 在转发前的 DB 段：鉴权 → 实例 → 授权。
		adb, err := service.store.Open(ctx)
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		auth, authErr := service.authenticateToken(ctx, adb, plain, "127.0.0.1")
		if authErr != nil {
			b.Fatalf("authenticateToken: %v", authErr)
		}
		inst, instErr := service.getInstance(ctx, adb, instance.ID)
		if instErr != nil {
			b.Fatalf("getInstance: %v", instErr)
		}
		granted, grantErr := service.instanceGrantedTo(ctx, adb, inst.ID, auth.UserID)
		if grantErr != nil {
			b.Fatalf("instanceGrantedTo: %v", grantErr)
		}
		if !granted {
			b.Fatal("expected granted")
		}
		adb.Close()
	}
}

// BenchmarkGatewayAuthPathPinned 与 BenchmarkGatewayAuthPath 相同，但持有常驻
// 句柄：量出「池常暖」后鉴权+实例+授权这段的真实成本（决定是否还需要结果缓存）。
func BenchmarkGatewayAuthPathPinned(b *testing.B) {
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

	db, err := service.store.Open(ctx)
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	user, err := service.createUser(ctx, db, "bench", "bench-pass-1", "")
	if err != nil {
		b.Fatalf("createUser: %v", err)
	}
	plain, _, err := service.issueToken(ctx, db, user.ID, "bench", "")
	if err != nil {
		b.Fatalf("issueToken: %v", err)
	}
	instance, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-bench", Provider: "opencode", Label: "bench"})
	if err != nil {
		b.Fatalf("createInstance: %v", err)
	}
	if err := service.setGrantsForUser(ctx, db, user.ID, []string{instance.ID}); err != nil {
		b.Fatalf("setGrantsForUser: %v", err)
	}
	db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		adb, err := service.store.Open(ctx)
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		auth, authErr := service.authenticateToken(ctx, adb, plain, "127.0.0.1")
		if authErr != nil {
			b.Fatalf("authenticateToken: %v", authErr)
		}
		inst, instErr := service.getInstance(ctx, adb, instance.ID)
		if instErr != nil {
			b.Fatalf("getInstance: %v", instErr)
		}
		granted, grantErr := service.instanceGrantedTo(ctx, adb, inst.ID, auth.UserID)
		if grantErr != nil {
			b.Fatalf("instanceGrantedTo: %v", grantErr)
		}
		if !granted {
			b.Fatal("expected granted")
		}
		adb.Close()
	}
}

// BenchmarkGatewayAccessLogEnqueue 量出「投递一条审计到异步队列」的成本。
// 这是优化后网关请求结束时的实际开销（不再同步落库）。
func BenchmarkGatewayAccessLogEnqueue(b *testing.B) {
	ctx := context.Background()
	service := New(benchConfig(b))
	if err := service.Initialize(ctx); err != nil {
		b.Fatalf("Initialize: %v", err)
	}
	// 用大缓冲消费，避免队列满导致丢包影响测量。
	defer service.StopAccessLog()
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/gw/x/session", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.writeAccessLog(req.Context(), AccessLog{
			InstanceID: "inst-bench",
			Action:     "gw GET /session",
			Result:     "ok",
			StatusCode: 200,
			IP:         "127.0.0.1",
			UserAgent:  "bench",
		})
	}
}

// BenchmarkAccessLogBatchPersist 量出一批（64 条）审计的单事务落库成本。
func BenchmarkAccessLogBatchPersist(b *testing.B) {
	ctx := context.Background()
	service := New(benchConfig(b))
	if err := service.Initialize(ctx); err != nil {
		b.Fatalf("Initialize: %v", err)
	}
	batch := make([]AccessLog, accessLogBatchSize)
	for i := range batch {
		batch[i] = AccessLog{
			InstanceID: "inst-bench",
			Action:     "gw GET /session",
			Result:     "ok",
			StatusCode: 200,
			IP:         "127.0.0.1",
			UserAgent:  "bench",
			CreatedAt:  nowRFC3339(),
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.persistAccessLogBatch(batch)
	}
}
