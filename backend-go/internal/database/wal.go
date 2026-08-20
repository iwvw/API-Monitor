package database

import (
	"context"
	"database/sql"
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
		"PRAGMA busy_timeout = 15000",
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
