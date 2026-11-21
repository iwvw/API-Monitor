package database

import (
	"context"
	"database/sql"
)

// WALMaintenance 执行一次周期 WAL 维护：以 wal_checkpoint(PASSIVE) 把当前可落
// 地的帧写回主库，让已回写部分可被后续写复用，避免 WAL 内帧无限累积。
//
// 只做 PASSIVE 是有意的结构性选择，请勿在此路径重引入 RESTART/TRUNCATE：
// 这两种重置型 checkpoint 要求"完全无活跃读者"。面板有常驻轮询读者（如
// admin-ai/sessions 每 2s 一次），现代码驱动下它们遇到读者会无视 busy_timeout
// 与调用方 ctx 无限阻塞；即使偶尔在读者间隙完成，重置瞬间也会短暂阻挡新读者
// 开启快照，表现为周期性的 "database is locked" 风暴——这正是本面板此前反复
// SQLITE_BUSY 的根因。PASSIVE 相反从不阻止读者、也从不长时间持锁，连有读者时
// 都能推进回写（读者不锁住已读过的帧）。
//
// 代价是 WAL 文件本身不会因 PASSIVE 缩小（截断回收只发生在读者间隙由
// journal_size_limit/自动 checkpoint 顺便完成，文件收敛在高水位附近；本面板
// 实测约 20MB/1GB 卷，可接受）。需要主动回收磁盘时走设置页数据库压缩。
func WALMaintenance(ctx context.Context, dbPath string) (ok bool, err error) {
	handle, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return false, err
	}
	defer handle.Close()

	if _, err := handle.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
		return false, err
	}
	// 多轮 PASSIVE 直至无帧可写回，推进 checkpoint 标记。
	for i := 0; i < 5; i++ {
		var b, l, c int64
		if err := handle.QueryRowContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`).Scan(&b, &l, &c); err != nil {
			return false, err
		}
		if l <= 64 {
			break
		}
	}
	return true, nil
}
