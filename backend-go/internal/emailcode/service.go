package emailcode

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

// workerSecretEnv 覆盖 Worker 握手密钥的环境变量名。
const workerSecretEnv = "EMAIL_INBOX_WORKER_SECRET"

// 默认保留时长：临时邮箱语义下邮件必须过期消失。
const defaultRetention = 7 * 24 * time.Hour

// maxBodyRunes 限制入库正文长度，避免单封邮件撑爆存储。
const maxBodyRunes = 64 * 1024

// WorkerSecret 返回下发给邮件 Worker 的握手密钥：优先环境变量，否则从加密根密钥派生。
func WorkerSecret() string {
	if v := strings.TrimSpace(os.Getenv(workerSecretEnv)); v != "" {
		return v
	}
	return secure.DeriveSecret("email-inbox")
}

// Message 是一封被收件箱捕获的邮件。
type Message struct {
	ID            int64  `json:"id"`
	MessageID     string `json:"messageId,omitempty"`
	Mailbox       string `json:"mailbox"`
	Domain        string `json:"domain"`
	Sender        string `json:"sender"`
	FromDomain    string `json:"fromDomain"`
	Subject       string `json:"subject"`
	Code          string `json:"code"`
	Link          string `json:"link"`
	Snippet       string `json:"snippet"`
	TextBody      string `json:"textBody,omitempty"`
	ExtractStatus string `json:"extractStatus"`
	ReceivedAt    string `json:"receivedAt"`
	ConsumedAt    string `json:"consumedAt,omitempty"`
	ConsumedBy    string `json:"consumedBy,omitempty"`
}

// Selector 定位一封邮件：收件人是必需键，其余用于在多发件人、同邮箱场景下精确收敛。
type Selector struct {
	Mailbox         string
	Domain          string
	FromDomain      string
	SubjectContains string
	Since           time.Time
	RequireCode     bool
}

// Service 是通用邮箱验证码收件箱：邮件 Worker 投递，任意消费者按选择器等待或取用。
type Service struct {
	cfg    config.Config
	store  *database.Store
	schema database.SchemaEnsurer

	mu      sync.Mutex
	waiters map[string]map[int]chan struct{}
	nextID  int

	domains DomainProvider
}

// DomainProvider 提供「已部署收件箱、可用于接收验证码」的域名列表。
// 由 server 注入（数据源在 Cloudflare 模块的部署记录），收件箱自身不感知 Cloudflare。
type DomainProvider interface {
	InboxDomains(ctx context.Context) ([]string, error)
}

// New 构造收件箱服务。
// 预热只确认核心库可打开；模块表结构在首次请求时懒建——SchemaEnsurer 的失败
// 会进程级固化，预热若带短超时，启动期锁竞争就会让整个收件箱模块持久瘫痪。
func New(cfg config.Config) *Service {
	s := &Service{cfg: cfg, store: database.New(cfg), waiters: map[string]map[int]chan struct{}{}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := s.store.Open(ctx); err == nil {
		_ = db.Close()
	}
	return s
}

// SetDomainProvider 注入可用域名来源。
func (s *Service) SetDomainProvider(p DomainProvider) {
	s.domains = p
}

// AvailableDomains 返回可用于生成收件邮箱的域名。
func (s *Service) AvailableDomains(ctx context.Context) ([]string, error) {
	if s.domains == nil {
		return []string{}, nil
	}
	out, err := s.domains.InboxDomains(ctx)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// open 打开共享 SQLite 连接并确保表结构存在。调用方负责 Close。
func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.schema.Ensure(func() error { return ensureSchema(ctx, db) }); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func mailboxKey(mailbox string) string {
	return strings.ToLower(strings.TrimSpace(mailbox))
}

// Ingest 写入一封邮件（按 Message-ID 去重），通知等待者并累计统计。
func (s *Service) Ingest(ctx context.Context, msg Message) (int64, error) {
	msg.Mailbox = strings.TrimSpace(msg.Mailbox)
	if msg.Mailbox == "" {
		return 0, errors.New("收件人不能为空")
	}
	if msg.Domain == "" {
		msg.Domain = domainOfAddress(msg.Mailbox)
	}
	if msg.FromDomain == "" {
		msg.FromDomain = domainOfAddress(msg.Sender)
	}
	if msg.ReceivedAt == "" {
		msg.ReceivedAt = time.Now().UTC().Format(time.RFC3339)
	}
	// 入库字段截断：邮件元数据来自公网投递，超长字段会撑爆 SQLite。
	msg.Mailbox = truncate(msg.Mailbox, 256)
	msg.Domain = truncate(msg.Domain, 253)
	msg.Sender = truncate(msg.Sender, 512)
	msg.FromDomain = truncate(msg.FromDomain, 253)
	msg.Subject = truncate(msg.Subject, 1024)
	msg.MessageID = truncate(msg.MessageID, 1024)
	msg.Code = truncate(msg.Code, 64)
	msg.Link = truncate(msg.Link, 4096)
	msg.Snippet = truncate(msg.Snippet, 1024)
	msg.TextBody = truncate(msg.TextBody, maxBodyRunes)

	db, err := s.open(ctx)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	// Message-ID 去重：同一封邮件重复投递时复用既有记录，不重复入库。
	if msg.MessageID != "" {
		var existing int64
		if err := db.QueryRowContext(ctx, `SELECT id FROM emailcode_messages WHERE message_id = ?`, msg.MessageID).Scan(&existing); err == nil {
			return existing, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}

	res, err := db.ExecContext(ctx, `INSERT INTO emailcode_messages
		(message_id, mailbox, domain, sender, from_domain, subject, code, link, snippet, text_body, extract_status, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.MessageID, msg.Mailbox, msg.Domain, msg.Sender, msg.FromDomain, msg.Subject,
		msg.Code, msg.Link, msg.Snippet, msg.TextBody, msg.ExtractStatus, msg.ReceivedAt)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	msg.ID = id
	s.recordStats(ctx, db, msg)
	s.notify(msg.Mailbox)
	return id, nil
}

// recordStats 累计「某日 / 收件域名 / 发件域名」的收件与提取计数。
// 日期桶按站点时区归属（唯一的时区控制点），避免跨时区统计错位。
func (s *Service) recordStats(ctx context.Context, db *sql.DB, msg Message) {
	loc := timeutil.LocationFromSettings(ctx, db)
	received := time.Now().UTC()
	if t, err := time.Parse(time.RFC3339, msg.ReceivedAt); err == nil {
		received = t
	}
	day := received.In(loc).Format("2006-01-02")
	extracted := 0
	if msg.Code != "" {
		extracted = 1
	}
	_, _ = db.ExecContext(ctx, `INSERT INTO emailcode_daily_stats (day, domain, from_domain, received, extracted, primary_spans, updated_at)
		VALUES (?, ?, ?, 1, ?, 0, ?)
		ON CONFLICT(day, domain, from_domain) DO UPDATE SET
			received = received + 1,
			extracted = extracted + ?,
			updated_at = excluded.updated_at`,
		day, msg.Domain, msg.FromDomain, extracted, time.Now().UTC().Format(time.RFC3339), extracted)
}

// notify 唤醒等待该收件人的订阅者（只发信号，不传数据，避免广播同一封邮件）。
func (s *Service) notify(mailbox string) {
	key := mailboxKey(mailbox)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.waiters[key] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Service) subscribe(mailbox string) (int, chan struct{}, func()) {
	key := mailboxKey(mailbox)
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	if s.waiters[key] == nil {
		s.waiters[key] = map[int]chan struct{}{}
	}
	s.nextID++
	id := s.nextID
	s.waiters[key][id] = ch
	s.mu.Unlock()
	cancel := func() {
		s.mu.Lock()
		if w, ok := s.waiters[key]; ok {
			delete(w, id)
			if len(w) == 0 {
				delete(s.waiters, key)
			}
		}
		s.mu.Unlock()
	}
	return id, ch, cancel
}

// Wait 等待一封匹配选择器的邮件并原子认领（先到先得，其它等待者继续等）。
// consumer 记录认领方，便于排查与展示。
func (s *Service) Wait(ctx context.Context, sel Selector, consumer string, timeout time.Duration) (Message, error) {
	if strings.TrimSpace(sel.Mailbox) == "" {
		return Message{}, errors.New("收件人不能为空")
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_, ch, cancel := s.subscribe(sel.Mailbox)
	defer cancel()

	if msg, ok := s.tryClaim(ctx, sel, consumer); ok {
		return msg, nil
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	poll := time.NewTicker(3 * time.Second)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return Message{}, ctx.Err()
		case <-timer.C:
			return Message{}, errors.New("等待验证码超时")
		case <-ch:
		case <-poll.C:
		}
		if msg, ok := s.tryClaim(ctx, sel, consumer); ok {
			return msg, nil
		}
	}
}

// tryClaim 找一封匹配的未消费邮件并原子认领。
func (s *Service) tryClaim(ctx context.Context, sel Selector, consumer string) (Message, bool) {
	db, err := s.open(ctx)
	if err != nil {
		return Message{}, false
	}
	defer db.Close()
	msg, ok := s.findMatch(ctx, db, sel)
	if !ok {
		return Message{}, false
	}
	res, err := db.ExecContext(ctx, `UPDATE emailcode_messages SET consumed_at = ?, consumed_by = ?
		WHERE id = ? AND consumed_at IS NULL`, time.Now().UTC().Format(time.RFC3339), consumer, msg.ID)
	if err != nil {
		return Message{}, false
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 被并发等待者抢走，交由调用方继续等待。
		return Message{}, false
	}
	msg.ConsumedBy = consumer
	return msg, true
}

// findMatch 按选择器查询最新一封匹配邮件。
func (s *Service) findMatch(ctx context.Context, db *sql.DB, sel Selector) (Message, bool) {
	query := `SELECT id, message_id, mailbox, domain, sender, from_domain, subject, code, link, snippet, text_body, extract_status, received_at, COALESCE(consumed_at, ''), consumed_by
		FROM emailcode_messages WHERE lower(mailbox) = ? AND consumed_at IS NULL`
	args := []interface{}{mailboxKey(sel.Mailbox)}
	if sel.Domain != "" {
		query += ` AND lower(domain) = ?`
		args = append(args, strings.ToLower(sel.Domain))
	}
	if sel.FromDomain != "" {
		// 后缀匹配：站点常从子域发信（如 cioeu80164.posthog.com），
		// 用 posthog.com 作为后缀可同时命中主域与任意子域。
		query += ` AND (lower(from_domain) = ? OR lower(from_domain) LIKE ?)`
		d := strings.ToLower(sel.FromDomain)
		args = append(args, d, "%."+d)
	}
	if sel.SubjectContains != "" {
		query += ` AND lower(subject) LIKE ?`
		args = append(args, "%"+strings.ToLower(sel.SubjectContains)+"%")
	}
	if sel.RequireCode {
		query += ` AND code <> ''`
	}
	if !sel.Since.IsZero() {
		query += ` AND received_at >= ?`
		args = append(args, sel.Since.Add(-2*time.Second).UTC().Format(time.RFC3339))
	}
	query += ` ORDER BY id DESC LIMIT 1`
	row := db.QueryRowContext(ctx, query, args...)
	msg, err := scanMessage(row)
	if err != nil {
		return Message{}, false
	}
	return msg, true
}

// List 返回收件箱最近邮件。
func (s *Service) List(ctx context.Context, mailbox string, includeConsumed bool, limit int) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	query := `SELECT id, message_id, mailbox, domain, sender, from_domain, subject, code, link, snippet, text_body, extract_status, received_at, COALESCE(consumed_at, ''), consumed_by
		FROM emailcode_messages WHERE 1 = 1`
	args := []interface{}{}
	if mailbox != "" {
		query += ` AND lower(mailbox) = ?`
		args = append(args, mailboxKey(mailbox))
	}
	if !includeConsumed {
		query += ` AND consumed_at IS NULL`
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Message, 0, limit)
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

// Get 返回单封邮件（含完整正文）。
func (s *Service) Get(ctx context.Context, id int64) (Message, error) {
	db, err := s.open(ctx)
	if err != nil {
		return Message{}, err
	}
	defer db.Close()
	row := db.QueryRowContext(ctx, `SELECT id, message_id, mailbox, domain, sender, from_domain, subject, code, link, snippet, text_body, extract_status, received_at, COALESCE(consumed_at, ''), consumed_by
		FROM emailcode_messages WHERE id = ?`, id)
	return scanMessage(row)
}

// Consume 标记一封邮件为已消费（供人工在界面取用）。
func (s *Service) Consume(ctx context.Context, id int64, consumer string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `UPDATE emailcode_messages SET consumed_at = ?, consumed_by = ?
		WHERE id = ? AND consumed_at IS NULL`, time.Now().UTC().Format(time.RFC3339), consumer, id)
	return err
}

// Delete 删除一封邮件。
func (s *Service) Delete(ctx context.Context, id int64) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `DELETE FROM emailcode_messages WHERE id = ?`, id)
	return err
}

// Clear 清理邮件：onlyConsumed 为 true 时只删已消费的，否则全清。
func (s *Service) Clear(ctx context.Context, onlyConsumed bool) (int64, error) {
	db, err := s.open(ctx)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	query := `DELETE FROM emailcode_messages`
	if onlyConsumed {
		query += ` WHERE consumed_at IS NOT NULL`
	}
	res, err := db.ExecContext(ctx, query)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurgeBefore 删除早于 cutoff 的邮件，返回删除条数（TTL 清理用）。
func (s *Service) PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	db, err := s.open(ctx)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	res, err := db.ExecContext(ctx, `DELETE FROM emailcode_messages WHERE received_at < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// StartCleanup 启动 TTL 清理：定期删除超过保留期的邮件。
// 临时邮箱语义下邮件必须过期消失，避免敏感内容长期滞留。
func (s *Service) StartCleanup(ctx context.Context, retention time.Duration) {
	if retention <= 0 {
		retention = defaultRetention
	}
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = s.PurgeBefore(ctx, time.Now().Add(-retention))
			}
		}
	}()
}

// StatsRow 是一行按日聚合的提取统计。
type StatsRow struct {
	Day        string `json:"day"`
	Domain     string `json:"domain"`
	FromDomain string `json:"fromDomain"`
	Received   int    `json:"received"`
	Extracted  int    `json:"extracted"`
}

// Stats 返回近 days 天的聚合统计与总提取率。
func (s *Service) Stats(ctx context.Context, days int) (map[string]interface{}, error) {
	if days <= 0 || days > 90 {
		days = 14
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	loc := timeutil.LocationFromSettings(ctx, db)
	from := time.Now().In(loc).AddDate(0, 0, -days).Format("2006-01-02")
	rows, err := db.QueryContext(ctx, `SELECT day, domain, from_domain, received, extracted FROM emailcode_daily_stats
		WHERE day >= ? ORDER BY day DESC, received DESC LIMIT 500`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StatsRow{}
	received, extracted := 0, 0
	for rows.Next() {
		var r StatsRow
		if err := rows.Scan(&r.Day, &r.Domain, &r.FromDomain, &r.Received, &r.Extracted); err != nil {
			return nil, err
		}
		received += r.Received
		extracted += r.Extracted
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rate := 0.0
	if received > 0 {
		rate = float64(extracted) / float64(received)
	}
	return map[string]interface{}{
		"rows":      out,
		"received":  received,
		"extracted": extracted,
		"rate":      rate,
		"days":      days,
	}, nil
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// rowScanner 兼容 *sql.Row 与 *sql.Rows。
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanMessage(row rowScanner) (Message, error) {
	var msg Message
	err := row.Scan(&msg.ID, &msg.MessageID, &msg.Mailbox, &msg.Domain, &msg.Sender, &msg.FromDomain,
		&msg.Subject, &msg.Code, &msg.Link, &msg.Snippet, &msg.TextBody, &msg.ExtractStatus,
		&msg.ReceivedAt, &msg.ConsumedAt, &msg.ConsumedBy)
	if err != nil {
		return Message{}, err
	}
	return msg, nil
}
