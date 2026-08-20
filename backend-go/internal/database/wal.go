package database

import (
	"context"
	"database/sql"
	"time"
)

// WALCheckpointTruncate 在独立维护连接上执行 PRAGMA wal_checkpoint(TRUNCATE)。
//
// 与 PASSIVE 不同，TRUNCATE 会把 WAL 完整写回主库并截断 WAL 文件，真正回收
// 磁盘空间；但它要求执行瞬间没有活跃读事务，面板持续读写时可能返回 busy。
// 该连接不经过进程级连接池，busy_timeout 等 PRAGMA 改动不会污染池化复用
// 连接。busy 置位时调用方应在低峰重试。
func WALCheckpointTruncate(ctx context.Context, dbPath string) (busy bool, logFrames, checkpointed int64, err error) {
	handle, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return false, 0, 0, err
	}
	defer handle.Close()

	for _, stmt := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 60000",
		"PRAGMA wal_autocheckpoint = 256",
	} {
		if _, err := handle.ExecContext(ctx, stmt); err != nil {
			return false, 0, 0, err
		}
	}

	var b, l, c int64
	if err := handle.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&b, &l, &c); err != nil {
		return false, 0, 0, err
	}
	return b != 0, l, c, nil
}

// WALMaintenance 执行一次完整的 WAL 维护：先用 PASSIVE checkpoint 把能写的
// 帧尽量写回主库（PASSIVE 遇到活跃读者也会推进，不阻塞），再在 attemptCtx
// 时间内反复尝试 TRUNCATE 截断文件。返回 true 表示 WAL 已成功截断。
//
// PASSIVE 先行很关键：WAL 巨大时直接 TRUNCATE 要把大量帧写回主库，耗时可观
// 且几乎必被新读者打断；PASSIVE 分段写回后待截断帧量骤减，TRUNCATE 更易于
// 在读者间隙完成。attemptCtx 建议给到分钟级，让 busy_timeout 真正生效。
func WALMaintenance(ctx context.Context, dbPath string, attemptCtx context.Context) (truncated bool, err error) {
	handle, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return false, err
	}
	defer handle.Close()

	for _, stmt := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 60000",
		"PRAGMA wal_autocheckpoint = 256",
	} {
		if _, err := handle.ExecContext(ctx, stmt); err != nil {
			return false, err
		}
	}
	// 先 PASSIVE 推进多轮，尽量把可直接写回的帧落地。
	for i := 0; i < 3; i++ {
		var b, l, c int64
		if err := handle.QueryRowContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`).Scan(&b, &l, &c); err != nil {
			return false, err
		}
		if l == 0 {
			break
		}
	}

	for {
		var b, l, c int64
		err := handle.QueryRowContext(attemptCtx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&b, &l, &c)
		if err != nil {
			return false, err
		}
		if b == 0 {
			return true, nil
		}
		select {
		case <-attemptCtx.Done():
			return false, nil
		case <-ctx.Done():
			return false, nil
		default:
		}
		select {
		case <-attemptCtx.Done():
			return false, nil
		case <-ctx.Done():
			return false, nil
		case <-time.After(5 * time.Second):
		}
	}
}
