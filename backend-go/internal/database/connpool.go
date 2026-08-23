package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"
	"sync/atomic"
)

// 进程级 SQLite 物理连接池。
//
// 全仓约 400 处调用点遵循「store.Open → 用完 defer db.Close()」的模式，此前
// 每次 Open 都新建物理连接并串行执行 7 条 PRAGMA。这里改为：Store.Open 返回
// 的 *sql.DB 仍是独立句柄（调用点语义不变），但其底层物理连接来自本池——
// db.Close() 时归还池中复用，PRAGMA 只在物理连接首次建立时执行一次。

const maxIdlePhysicalConns = 4

// 通过一次不建连的 sql.Open 拿到已注册的 modernc sqlite 驱动句柄，
// 避免依赖驱动包的导出符号。
var sqliteDriver = func() driver.Driver {
	db, err := sql.Open("sqlite", "")
	if err != nil {
		panic(fmt.Sprintf("resolve sqlite driver: %v", err))
	}
	defer db.Close()
	return db.Driver()
}()

var (
	poolsMu sync.Mutex
	pools   = map[string]*connPool{}
)

// poolFor 返回 dbPath 对应的连接池；已通过 ResetPool 失效的路径会被重建。
func poolFor(dbPath string) *connPool {
	poolsMu.Lock()
	defer poolsMu.Unlock()
	if p, ok := pools[dbPath]; ok {
		return p
	}
	p := &connPool{dbPath: dbPath}
	pools[dbPath] = p
	return p
}

// ResetPool 使 dbPath 对应连接池失效：关闭所有空闲物理连接并从注册表移除。
// 之后任何新的 Store.Open 都会重新建立连接、重新打开磁盘上的数据库文件。
// 用于数据库文件被整体替换（导入）后，保证后续访问不会再读写旧文件句柄。
// 已在使用的连接（包括进程常驻句柄）不受影响，仍指向替换前的旧文件，
// 因此导入成功后应尽快重启后端，避免常驻句柄读到旧快照。
func ResetPool(dbPath string) {
	poolsMu.Lock()
	p, ok := pools[dbPath]
	if !ok {
		poolsMu.Unlock()
		return
	}
	delete(pools, dbPath)
	poolsMu.Unlock()

	p.mu.Lock()
	drained := p.idle
	p.idle = nil
	p.mu.Unlock()
	for _, conn := range drained {
		_ = conn.Close()
	}
}

type connPool struct {
	dbPath string
	mu     sync.Mutex
	idle   []driver.Conn
	refs   int
	// schemaMu/schemaDone 串行化同一池内的 core schema 保障；schemaOnce
	// 语义（失败也固化）会让一次瞬时失败永久粘池——进程内所有后续 Open
	// 持续报错直到重启，因此改为「成功才缓存，失败不缓存」。
	schemaMu   sync.Mutex
	schemaDone bool
}

// ensureSchema 确保核心 schema 就绪：同一池内并发 Open 互斥执行，
// 成功结果缓存；失败不缓存，由下一次 Open 重试（EnsureCoreSchema 全部
// 语句幂等，可安全重入）。
func (p *connPool) ensureSchema(ctx context.Context, db *sql.DB) error {
	p.schemaMu.Lock()
	defer p.schemaMu.Unlock()
	if p.schemaDone {
		return nil
	}
	if err := WithSchemaLock(ctx, func() error {
		return EnsureCoreSchema(ctx, db)
	}); err != nil {
		return err
	}
	p.schemaDone = true
	return nil
}

// addRef 记录一个存活的 *sql.DB 逻辑句柄。
func (p *connPool) addRef() {
	p.mu.Lock()
	p.refs++
	p.mu.Unlock()
}

// release 在逻辑句柄 Close 时回调；最后一个句柄关闭后排空空闲物理连接，
// 释放数据库文件句柄（Windows 下测试的 TempDir 清理依赖这一点）。
// 生产进程通过常驻的 pin 句柄（server 启动时打开、进程存活期间不关）
// 使 refs 恒 >0，池保持常暖。
func (p *connPool) release() {
	p.mu.Lock()
	p.refs--
	var drained []driver.Conn
	if p.refs <= 0 {
		drained = p.idle
		p.idle = nil
		poolsMu.Lock()
		delete(pools, p.dbPath)
		poolsMu.Unlock()
	}
	p.mu.Unlock()
	for _, conn := range drained {
		_ = conn.Close()
	}
}

func (p *connPool) get(ctx context.Context) (driver.Conn, error) {
	p.mu.Lock()
	if n := len(p.idle); n > 0 {
		conn := p.idle[n-1]
		p.idle = p.idle[:n-1]
		p.mu.Unlock()
		return &pooledConn{conn: conn, pool: p}, nil
	}
	p.mu.Unlock()

	conn, err := sqliteDriver.Open(p.dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := applyConnPragmas(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &pooledConn{conn: conn, pool: p}, nil
}

func (p *connPool) put(conn driver.Conn) error {
	p.mu.Lock()
	if p.refs > 0 && len(p.idle) < maxIdlePhysicalConns {
		p.idle = append(p.idle, conn)
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	return conn.Close()
}

// connPragmas 每条都会在「新物理连接」上执行，因此只适合放连接级设置。
// journal_mode/auto_vacuum/cache_size 等数据库级、持久化于文件的设置不应放在
// 这里：每连接重复执行会让新连接在繁忙期抢库文件头的排他写锁（SQLITE_BUSY，
// 例如「get session: configure sqlite (PRAGMA auto_vacuum = INCREMENTAL)」）。
// auto_vacuum 由迁移路径（settings 数据库压缩 / github 清理）在需要时设置，
// 连接打开阶段不再触碰。
var connPragmas = []string{
	"PRAGMA foreign_keys = ON",
	"PRAGMA busy_timeout = 30000",
	"PRAGMA journal_mode = WAL",
	// NORMAL keeps WAL commits durable across process crashes while avoiding an
	// fsync for every transaction. Temporary sort/index data stays in memory.
	"PRAGMA synchronous = NORMAL",
	"PRAGMA temp_store = MEMORY",
	// 约 4MB 页缓存（负值单位为 KB）。页缓存按物理连接独立持有
	// （最多 maxIdlePhysicalConns 个常驻连接 = 上限 16MB），WAL 下共享
	// 场景数据量不大（百 MB 级库），4MB 对范围查询足够；小内存主机
	// （Fly 256MB）上过大缓存会白白占住堆外内存预算。
	"PRAGMA cache_size = -4096",
	"PRAGMA wal_autocheckpoint = 256",
	"PRAGMA journal_size_limit = 8388608",
}

func applyConnPragmas(ctx context.Context, conn driver.Conn) error {
	for _, pragma := range connPragmas {
		if err := execOnConn(ctx, conn, pragma); err != nil {
			return fmt.Errorf("configure sqlite (%s): %w", pragma, err)
		}
	}
	return nil
}

func execOnConn(ctx context.Context, conn driver.Conn, query string) error {
	if execer, ok := conn.(driver.ExecerContext); ok {
		_, err := execer.ExecContext(ctx, query, nil)
		return err
	}
	stmt, err := conn.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(nil)
	return err
}

// pooledConn 包装物理连接：Close 归还池中而非关闭；持有未完成事务的连接
// 不回收（database/sql 只会在调用方泄漏 Tx 时走到这条路径），直接真关。
type pooledConn struct {
	conn   driver.Conn
	pool   *connPool
	inTx   atomic.Bool
	closed bool
}

func (c *pooledConn) Prepare(query string) (driver.Stmt, error) {
	return c.conn.Prepare(query)
}

func (c *pooledConn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	if c.inTx.Load() {
		return c.conn.Close()
	}
	return c.pool.put(c.conn)
}

func (c *pooledConn) Begin() (driver.Tx, error) {
	tx, err := c.conn.Begin() //nolint:staticcheck // 仅在驱动不支持 BeginTx 时的回退路径
	if err != nil {
		return nil, err
	}
	c.inTx.Store(true)
	return &pooledTx{tx: tx, conn: c}, nil
}

func (c *pooledConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	beginner, ok := c.conn.(driver.ConnBeginTx)
	if !ok {
		return c.Begin()
	}
	tx, err := beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	c.inTx.Store(true)
	return &pooledTx{tx: tx, conn: c}, nil
}

func (c *pooledConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if preparer, ok := c.conn.(driver.ConnPrepareContext); ok {
		return preparer.PrepareContext(ctx, query)
	}
	return c.conn.Prepare(query)
}

func (c *pooledConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := c.conn.(driver.ExecerContext); ok {
		return execer.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *pooledConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := c.conn.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *pooledConn) Ping(ctx context.Context) error {
	if pinger, ok := c.conn.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *pooledConn) ResetSession(ctx context.Context) error {
	if resetter, ok := c.conn.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

func (c *pooledConn) IsValid() bool {
	if validator, ok := c.conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

type pooledTx struct {
	tx   driver.Tx
	conn *pooledConn
}

func (t *pooledTx) Commit() error {
	t.conn.inTx.Store(false)
	return t.tx.Commit()
}

func (t *pooledTx) Rollback() error {
	t.conn.inTx.Store(false)
	return t.tx.Rollback()
}

// poolConnector 让 sql.OpenDB 从进程级池取物理连接。
// 实现 io.Closer：database/sql 在 DB.Close 时回调，用于池的引用计数。
type poolConnector struct {
	pool      *connPool
	closeOnce sync.Once
}

func (c *poolConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return c.pool.get(ctx)
}

func (c *poolConnector) Driver() driver.Driver {
	return sqliteDriver
}

func (c *poolConnector) Close() error {
	c.closeOnce.Do(c.pool.release)
	return nil
}
