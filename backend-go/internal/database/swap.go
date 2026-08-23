package database

import (
	"database/sql"
	"fmt"
	"os"
	"sync"

	_ "modernc.org/sqlite"
)

// swapMu 是进程级「数据库文件整体替换」互斥量：settings 的数据库导入
// （replaceDatabase）、数据库压缩（runVacuum）与 backup 的备份恢复
// （restoreFromZip）共用。任一执行体重写库文件期间必须持有它，
// 其余执行体要么阻塞等待（Lock），要么放弃当轮（TryLock）。
// 该互斥必须在 database 包导出：backup 无法导入 settings（依赖方向），
// 互斥语义只有落在双方共同依赖的底层包才能闭环。
var swapMu sync.Mutex

// SwapMutex 返回数据库整体替换/压缩/恢复共用的互斥量。
// 持锁顺序约束：调用方已持有其它锁时，只能在获取 swapMu 之后再去获取
// 更细粒度的锁（如 settings.vacuumMu），不得反向嵌套。
func SwapMutex() *sync.Mutex { return &swapMu }

// PrepareSwapFile 在数据库文件被整体替换/恢复写入 dbPath 之前做预处理：
//  1. integrity_check 校验内容完整——损坏/非 SQLite 文件必须在替换前报错，
//     先替换再校验会在库路径上遗留 -wal/-shm 句柄，回滚后所有后续打开
//     长时间 SQLITE_BUSY；
//  2. 转为 WAL——进程常驻句柄（database/sql 层缓存的连接，ResetPool 无法
//     关闭）使替换在 Windows 上只能退回 O_TRUNC 就地重写；若新内容仍为
//     DELETE 日志模式，后续连接执行 journal_mode=WAL 需要全库排他锁，
//     会被旧句柄的残留锁阻塞（实测 30s BUSY）；预先转为 WAL 后该
//     PRAGMA 成为无锁 no-op；
//  3. 清理预处理自身产生的 sidecar（WAL 模式已持久化在库文件头）。
func PrepareSwapFile(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		_ = db.Close()
		return fmt.Errorf("integrity_check %s: %w", path, err)
	}
	if integrity != "ok" {
		_ = db.Close()
		return fmt.Errorf("integrity_check %s: %s", path, integrity)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		_ = db.Close()
		return fmt.Errorf("switch %s to WAL: %w", path, err)
	}
	closeErr := db.Close()
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	return closeErr
}
