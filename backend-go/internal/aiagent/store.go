package aiagent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost = 12

	defaultTokenTTLDays = 180
	maxTokenTTLDays     = 3650

	loginMaxFailures = 8
	loginLockWindow  = 10 * time.Minute
)

var (
	errNotFound      = errors.New("not found")
	errDuplicate     = errors.New("duplicate")
	errLockedOut     = errors.New("too many failed attempts")
	errInvalidCreds  = errors.New("invalid credentials")
	errUserDisabled  = errors.New("user disabled")
	errTokenExpired  = errors.New("token expired")
	errTokenRevoked  = errors.New("token revoked")
	errMetaTooLarge  = errors.New("metadata too large")
	maxMetadataBytes = 64 * 1024
)

// newID 生成带前缀的随机标识。随机源不可用时返回空串（由调用方视作失败），
// 绝不退化到基于时间戳的可预测值——这些 ID 会出现在网关路径与变更 URL 中。
func newID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return prefix + hex.EncodeToString(buf)
}

// randomToken 生成 32 字节随机令牌的十六进制表示。随机源失败时返回空串，
// 由签发方拒签，绝不退化到可预测的值。
func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

// hashToken 返回令牌的 SHA-256 十六进制摘要，用于落库比对。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func hashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func verifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ---------- 登录限流 ----------

type loginAttempt struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

type loginLimiter struct {
	mu         sync.Mutex
	attempts   map[string]loginAttempt
	ipAttempts map[string]ipFailureBucket
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		attempts:   make(map[string]loginAttempt),
		ipAttempts: make(map[string]ipFailureBucket),
	}
}

func (l *loginLimiter) key(username, ip string) string {
	return strings.ToLower(strings.TrimSpace(username)) + "|" + ip
}

func (l *loginLimiter) locked(username, ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.ipLockedLocked(ip, now) {
		return true
	}
	key := l.key(username, ip)
	attempt, ok := l.attempts[key]
	if !ok {
		return false
	}
	// 锁定窗口已过，或长时间无新失败：清理条目，避免映射无界增长。
	if !attempt.lockedUntil.IsZero() && now.After(attempt.lockedUntil) {
		delete(l.attempts, key)
		return false
	}
	if now.Sub(attempt.lastSeen) > loginLockWindow {
		delete(l.attempts, key)
		return false
	}
	return now.Before(attempt.lockedUntil)
}

// ipFailureBucket 记录单个来源 IP 的失败总数，用于抑制跨用户名的密码喷洒。
type ipFailureBucket struct {
	failures int
	lastSeen time.Time
}

func (l *loginLimiter) ipLockedLocked(ip string, now time.Time) bool {
	bucket, ok := l.ipAttempts[ip]
	if !ok {
		return false
	}
	if now.Sub(bucket.lastSeen) > loginLockWindow {
		delete(l.ipAttempts, ip)
		return false
	}
	return bucket.failures >= loginMaxFailures*4
}

func (l *loginLimiter) fail(username, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := l.key(username, ip)
	now := time.Now()
	l.pruneLocked(now)
	attempt := l.attempts[key]
	attempt.failures++
	attempt.lastSeen = now
	if attempt.failures >= loginMaxFailures {
		attempt.lockedUntil = now.Add(loginLockWindow)
	}
	l.attempts[key] = attempt
	// 同一来源 IP 的失败总数用于抑制跨用户名的密码喷洒。
	bucket := l.ipAttempts[ip]
	bucket.failures++
	bucket.lastSeen = now
	l.ipAttempts[ip] = bucket
}

// pruneLocked 清理超过锁定窗口未再出现的条目，约束映射规模上限。
func (l *loginLimiter) pruneLocked(now time.Time) {
	for key, attempt := range l.attempts {
		if now.Sub(attempt.lastSeen) > loginLockWindow {
			delete(l.attempts, key)
		}
	}
	for ip, bucket := range l.ipAttempts {
		if now.Sub(bucket.lastSeen) > loginLockWindow {
			delete(l.ipAttempts, ip)
		}
	}
}

func (l *loginLimiter) reset(username, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// 只清该用户名在该 IP 上的失败计数；保留按 IP 的聚合喷洒计数，
	// 否则攻击者用任意一个有效账号登录一次即可清空整条 IP 的跨用户名闸门。
	delete(l.attempts, l.key(username, ip))
}

// ---------- 用户 ----------

func (s *Service) listUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, username, display_name, disabled, created_at, updated_at, last_login_at
		FROM aiagent_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func scanUser(scanner interface {
	Scan(dest ...interface{}) error
}) (User, error) {
	var user User
	var displayName, lastLogin sql.NullString
	var disabled int
	if err := scanner.Scan(&user.ID, &user.Username, &displayName, &disabled, &user.CreatedAt, &user.UpdatedAt, &lastLogin); err != nil {
		return User{}, err
	}
	user.DisplayName = displayName.String
	user.LastLoginAt = lastLogin.String
	user.Disabled = disabled != 0
	return user, nil
}

func (s *Service) getUserByID(ctx context.Context, db *sql.DB, id string) (User, error) {
	row := db.QueryRowContext(ctx, `SELECT id, username, display_name, disabled, created_at, updated_at, last_login_at
		FROM aiagent_users WHERE id = ?`, id)
	return scanUser(row)
}

func (s *Service) getUserByName(ctx context.Context, db *sql.DB, username string) (User, string, error) {
	var passwordHash string
	var user User
	var displayName, lastLogin sql.NullString
	var disabled int
	err := db.QueryRowContext(ctx, `SELECT id, username, display_name, disabled, created_at, updated_at, last_login_at, password_hash
		FROM aiagent_users WHERE username = ?`, username).
		Scan(&user.ID, &user.Username, &displayName, &disabled, &user.CreatedAt, &user.UpdatedAt, &lastLogin, &passwordHash)
	if err != nil {
		return User{}, "", err
	}
	user.DisplayName = displayName.String
	user.LastLoginAt = lastLogin.String
	user.Disabled = disabled != 0
	return user, passwordHash, nil
}

func (s *Service) createUser(ctx context.Context, db *sql.DB, username, password, displayName string) (User, error) {
	hashed, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	now := nowRFC3339()
	user := User{
		ID:          newID("usr_"),
		Username:    username,
		DisplayName: displayName,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if user.ID == "" {
		return User{}, errors.New("failed to generate user id")
	}
	var display interface{}
	if displayName != "" {
		display = displayName
	}
	_, err = db.ExecContext(ctx, `INSERT INTO aiagent_users (id, username, password_hash, display_name, disabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)`, user.ID, user.Username, hashed, display, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return User{}, errDuplicate
		}
		return User{}, err
	}
	return user, nil
}

func (s *Service) updateUser(ctx context.Context, db *sql.DB, id string, displayName *string, disabled *bool) error {
	if _, err := s.getUserByID(ctx, db, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		return err
	}
	now := nowRFC3339()
	// displayName 为 nil 表示不修改；指向空串表示显式清空（写入 NULL）。
	var display interface{}
	if displayName != nil {
		trimmed := strings.TrimSpace(*displayName)
		if trimmed != "" {
			display = trimmed
		} else {
			display = nil
		}
	}
	var disabledValue interface{}
	if disabled != nil {
		if *disabled {
			disabledValue = 1
		} else {
			disabledValue = 0
		}
	}
	// displayName 提供时直接赋值（允许清空为 NULL）；未提供时用 COALESCE 保留原值。
	_, err := db.ExecContext(ctx, `UPDATE aiagent_users SET
		display_name = CASE WHEN ? THEN ? ELSE display_name END,
		disabled = COALESCE(?, disabled),
		updated_at = ?
		WHERE id = ?`, displayName != nil, display, disabledValue, now, id)
	return err
}

func (s *Service) resetUserPassword(ctx context.Context, db *sql.DB, id, password string) error {
	hashed, err := hashPassword(password)
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `UPDATE aiagent_users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		hashed, nowRFC3339(), id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return errNotFound
	}
	// 重置密码后吊销该用户全部令牌，避免旧凭证继续可用。
	return s.revokeUserTokens(ctx, db, id)
}

func (s *Service) deleteUser(ctx context.Context, db *sql.DB, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM aiagent_tokens WHERE user_id = ?`, id); err != nil {
		return err
	}
	// 实例是平台资源，删除用户只收回其授权，不删除实例本身。
	if _, err := tx.ExecContext(ctx, `DELETE FROM aiagent_instance_grants WHERE user_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM aiagent_user_preferences WHERE user_id = ?`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM aiagent_users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return errNotFound
	}
	return tx.Commit()
}

func (s *Service) touchLogin(ctx context.Context, db *sql.DB, id string) {
	_, _ = db.ExecContext(ctx, `UPDATE aiagent_users SET last_login_at = ? WHERE id = ?`, nowRFC3339(), id)
}

// ---------- 令牌 ----------

func (s *Service) issueToken(ctx context.Context, db *sql.DB, userID, deviceLabel, ip string) (string, Token, error) {
	plain := randomToken()
	if plain == "" {
		return "", Token{}, errors.New("token generation failed")
	}
	now := time.Now().UTC()
	ttlDays := s.getTokenTTLDays()
	if ttlDays <= 0 || ttlDays > maxTokenTTLDays {
		ttlDays = defaultTokenTTLDays
	}
	expiresAt := now.AddDate(0, 0, ttlDays)
	prefix := plain
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	token := Token{
		ID:          newID("tok_"),
		UserID:      userID,
		Prefix:      prefix,
		DeviceLabel: deviceLabel,
		ExpiresAt:   expiresAt.Format(time.RFC3339),
		CreatedAt:   now.Format(time.RFC3339),
	}
	if token.ID == "" {
		return "", Token{}, errors.New("failed to generate token id")
	}
	var label interface{}
	if deviceLabel != "" {
		label = deviceLabel
	}
	var ipValue interface{}
	if ip != "" {
		ipValue = ip
	}
	_, err := db.ExecContext(ctx, `INSERT INTO aiagent_tokens
		(id, user_id, token_hash, token_prefix, device_label, expires_at, last_ip, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		token.ID, userID, hashToken(plain), prefix, label, token.ExpiresAt, ipValue, token.CreatedAt)
	if err != nil {
		return "", Token{}, err
	}
	return plain, token, nil
}

func (s *Service) listTokens(ctx context.Context, db *sql.DB, userID string) ([]Token, error) {
	query := `SELECT t.id, t.user_id, u.username, t.token_prefix, t.device_label, t.expires_at, t.revoked_at, t.last_used_at, t.created_at
		FROM aiagent_tokens t LEFT JOIN aiagent_users u ON u.id = t.user_id`
	args := []interface{}{}
	if userID != "" {
		query += ` WHERE t.user_id = ?`
		args = append(args, userID)
	}
	query += ` ORDER BY t.created_at DESC`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := make([]Token, 0)
	for rows.Next() {
		var token Token
		var username, label, revoked, lastUsed sql.NullString
		if err := rows.Scan(&token.ID, &token.UserID, &username, &token.Prefix, &label,
			&token.ExpiresAt, &revoked, &lastUsed, &token.CreatedAt); err != nil {
			return nil, err
		}
		token.Username = username.String
		token.DeviceLabel = label.String
		token.RevokedAt = revoked.String
		token.LastUsedAt = lastUsed.String
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

// authenticateToken 按明文令牌解析出认证上下文，并同步更新 last_used_at。
func (s *Service) authenticateToken(ctx context.Context, db *sql.DB, plain, ip string) (authContext, error) {
	if plain == "" {
		return authContext{}, errInvalidCreds
	}
	hash := hashToken(plain)
	var (
		tokenID    string
		userID     string
		expiresAt  string
		revokedAt  sql.NullString
		lastUsedAt sql.NullString
		username   string
		disabled   int
	)
	err := db.QueryRowContext(ctx, `SELECT t.id, t.user_id, t.expires_at, t.revoked_at, t.last_used_at, u.username, u.disabled
		FROM aiagent_tokens t JOIN aiagent_users u ON u.id = t.user_id
		WHERE t.token_hash = ?`, hash).
		Scan(&tokenID, &userID, &expiresAt, &revokedAt, &lastUsedAt, &username, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return authContext{}, errInvalidCreds
	}
	if err != nil {
		return authContext{}, err
	}
	if revokedAt.Valid && revokedAt.String != "" {
		return authContext{}, errTokenRevoked
	}
	if disabled != 0 {
		return authContext{}, errUserDisabled
	}
	if parsed, parseErr := time.Parse(time.RFC3339, expiresAt); parseErr == nil && time.Now().UTC().After(parsed) {
		return authContext{}, errTokenExpired
	}
	var ipValue interface{}
	if ip != "" {
		ipValue = ip
	}
	// 令牌活跃时间用于展示，无需每次请求都写库；节流到 1 分钟，避免流式热路径上的写放大。
	if shouldTouchToken(lastUsedAt, time.Now().UTC()) {
		_, _ = db.ExecContext(ctx, `UPDATE aiagent_tokens SET last_used_at = ?, last_ip = COALESCE(?, last_ip) WHERE id = ?`,
			nowRFC3339(), ipValue, tokenID)
	}

	return authContext{UserID: userID, TokenID: tokenID, Username: username}, nil
}

// tokenTouchInterval 是 last_used_at 的最小落库间隔。
const tokenTouchInterval = time.Minute

func shouldTouchToken(lastUsedAt sql.NullString, now time.Time) bool {
	if !lastUsedAt.Valid || lastUsedAt.String == "" {
		return true
	}
	previous, err := time.Parse(time.RFC3339, lastUsedAt.String)
	if err != nil {
		return true
	}
	return now.Sub(previous) >= tokenTouchInterval
}

func (s *Service) revokeToken(ctx context.Context, db *sql.DB, tokenID, userID string) error {
	query := `UPDATE aiagent_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`
	args := []interface{}{nowRFC3339(), tokenID}
	if userID != "" {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return errNotFound
	}
	return nil
}

func (s *Service) revokeUserTokens(ctx context.Context, db *sql.DB, userID string) error {
	_, err := db.ExecContext(ctx, `UPDATE aiagent_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		nowRFC3339(), userID)
	return err
}

// ---------- 实例 ----------

// listInstances 返回全部实例。allUsers 参数保留以兼容调用点语义：
// 实例已是平台资源，不再按用户过滤，可见性由调用方按授权另行收窄。
func (s *Service) listInstances(ctx context.Context, db *sql.DB) ([]Instance, error) {
	query := `SELECT id, server_id, provider, label, port, enabled, created_at, updated_at
		FROM aiagent_instances ORDER BY label`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	instances := make([]Instance, 0)
	for rows.Next() {
		instance, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, rows.Err()
}

// listInstancesForUser 只返回该用户被授权的实例。空授权即空列表（默认不授权）。
func (s *Service) listInstancesForUser(ctx context.Context, db *sql.DB, userID string) ([]Instance, error) {
	query := `SELECT i.id, i.server_id, i.provider, i.label, i.port, i.enabled, i.created_at, i.updated_at
		FROM aiagent_instances i
		INNER JOIN aiagent_instance_grants g ON g.instance_id = i.id
		WHERE g.user_id = ?
		ORDER BY i.label`
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	instances := make([]Instance, 0)
	for rows.Next() {
		instance, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, rows.Err()
}

func scanInstance(scanner interface {
	Scan(dest ...interface{}) error
}) (Instance, error) {
	var instance Instance
	var enabled int
	if err := scanner.Scan(&instance.ID, &instance.ServerID, &instance.Provider,
		&instance.Label, &instance.Port, &enabled, &instance.CreatedAt, &instance.UpdatedAt); err != nil {
		return Instance{}, err
	}
	instance.Enabled = enabled != 0
	return instance, nil
}

func (s *Service) getInstance(ctx context.Context, db *sql.DB, id string) (Instance, error) {
	row := db.QueryRowContext(ctx, `SELECT id, server_id, provider, label, port, enabled, created_at, updated_at
		FROM aiagent_instances WHERE id = ?`, id)
	instance, err := scanInstance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Instance{}, errNotFound
	}
	return instance, err
}

// instanceGrantedTo 判断实例是否已授权给该用户。
func (s *Service) instanceGrantedTo(ctx context.Context, db *sql.DB, instanceID, userID string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM aiagent_instance_grants WHERE instance_id = ? AND user_id = ?`,
		instanceID, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// listInstanceIDsForUser 返回该用户被授权的实例 ID 集合。
func (s *Service) listInstanceIDsForUser(ctx context.Context, db *sql.DB, userID string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT instance_id FROM aiagent_instance_grants WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// listGrantsForInstance 返回实例已授权的用户（含用户名），供实例侧展示。
func (s *Service) listGrantsForInstance(ctx context.Context, db *sql.DB, instanceID string) ([]InstanceGrant, error) {
	rows, err := db.QueryContext(ctx, `SELECT g.instance_id, g.user_id, COALESCE(u.username, ''), g.created_at
		FROM aiagent_instance_grants g LEFT JOIN aiagent_users u ON u.id = g.user_id
		WHERE g.instance_id = ? ORDER BY u.username`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := make([]InstanceGrant, 0)
	for rows.Next() {
		var grant InstanceGrant
		if err := rows.Scan(&grant.InstanceID, &grant.UserID, &grant.Username, &grant.CreatedAt); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

// setGrantsForUser 把该用户的授权集合整体替换为 instanceIDs。事务内先删后插，
// 避免中途失败留下半套授权。返回实际生效的实例 ID。
func (s *Service) setGrantsForUser(ctx context.Context, db *sql.DB, userID string, instanceIDs []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM aiagent_instance_grants WHERE user_id = ?`, userID); err != nil {
		return err
	}
	now := nowRFC3339()
	seen := make(map[string]bool, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		instanceID = strings.TrimSpace(instanceID)
		if instanceID == "" || seen[instanceID] {
			continue
		}
		seen[instanceID] = true
		// 实例可能已被删除：跳过无效 ID，而不是让整批写入失败。
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM aiagent_instances WHERE id = ?`, instanceID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO aiagent_instance_grants (instance_id, user_id, created_at) VALUES (?, ?, ?)`,
			instanceID, userID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) createInstance(ctx context.Context, db *sql.DB, payload instancePayload) (Instance, error) {
	provider, ok := LookupProvider(payload.Provider)
	if !ok {
		return Instance{}, errInvalidProvider
	}
	port := payload.Port
	if port == 0 {
		port = provider.DefaultPort
	}
	// 第一版只允许 Provider 的已知端口：网关会转发到 127.0.0.1:<port>，
	// 放开任意端口会把网关变成到 Agent 主机任意本地服务的转发器。
	if port != provider.DefaultPort {
		return Instance{}, errInvalidPort
	}
	now := nowRFC3339()
	instance := Instance{
		ID:        newID("inst_"),
		ServerID:  payload.ServerID,
		Provider:  provider.ID,
		Label:     payload.Label,
		Port:      port,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if instance.ID == "" {
		return Instance{}, errors.New("failed to generate instance id")
	}
	if payload.Enabled != nil {
		instance.Enabled = *payload.Enabled
	}
	enabled := 0
	if instance.Enabled {
		enabled = 1
	}
	_, err := db.ExecContext(ctx, `INSERT INTO aiagent_instances
		(id, server_id, provider, label, port, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		instance.ID, instance.ServerID, instance.Provider, instance.Label, instance.Port,
		enabled, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Instance{}, errDuplicate
		}
		return Instance{}, err
	}
	return instance, nil
}

func (s *Service) updateInstance(ctx context.Context, db *sql.DB, id string, payload instancePayload) (Instance, error) {
	current, err := s.getInstance(ctx, db, id)
	if err != nil {
		return Instance{}, err
	}
	provider := current.Provider
	if payload.Provider != "" {
		found, ok := LookupProvider(payload.Provider)
		if !ok {
			return Instance{}, errInvalidProvider
		}
		provider = found.ID
	}
	targetProvider, ok := LookupProvider(provider)
	if !ok {
		return Instance{}, errInvalidProvider
	}
	label := current.Label
	if payload.Label != "" {
		label = payload.Label
	}
	serverID := current.ServerID
	if payload.ServerID != "" {
		serverID = payload.ServerID
	}
	port := current.Port
	providerChanged := payload.Provider != "" && payload.Provider != current.Provider
	// 换 Provider 且未显式给端口时，端口回落到新 Provider 的默认端口，而不是沿用旧端口。
	if providerChanged && payload.Port == 0 {
		port = targetProvider.DefaultPort
	}
	if payload.Port != 0 {
		if payload.Port < 0 || payload.Port > 65535 {
			return Instance{}, errInvalidPort
		}
		port = payload.Port
	}
	// 无论是否显式传端口，最终端口都必须匹配（可能已变更的）Provider 默认端口，
	// 否则换 Provider 但沿用旧端口会写入一个与 Provider 不匹配的实例。
	if port != targetProvider.DefaultPort {
		return Instance{}, errInvalidPort
	}
	enabled := current.Enabled
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	enabledValue := 0
	if enabled {
		enabledValue = 1
	}
	now := nowRFC3339()
	_, err = db.ExecContext(ctx, `UPDATE aiagent_instances SET
		server_id = ?, provider = ?, label = ?, port = ?, enabled = ?, updated_at = ?
		WHERE id = ?`, serverID, provider, label, port, enabledValue, now, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Instance{}, errDuplicate
		}
		return Instance{}, err
	}
	return s.getInstance(ctx, db, id)
}

func (s *Service) deleteInstance(ctx context.Context, db *sql.DB, id string) error {
	if _, err := s.getInstance(ctx, db, id); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{
		`DELETE FROM aiagent_instance_meta WHERE instance_id = ?`,
		`DELETE FROM aiagent_instance_grants WHERE instance_id = ?`,
		`DELETE FROM aiagent_instances WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

var errInvalidProvider = errors.New("invalid provider")
var errInvalidPort = errors.New("port out of range")

// ---------- 实例元数据 ----------

func (s *Service) getMeta(ctx context.Context, db *sql.DB, instanceID string) (string, error) {
	var meta string
	err := db.QueryRowContext(ctx, `SELECT metadata_json FROM aiagent_instance_meta WHERE instance_id = ?`, instanceID).Scan(&meta)
	if errors.Is(err, sql.ErrNoRows) {
		return "{}", nil
	}
	if err != nil {
		return "", err
	}
	return meta, nil
}

func (s *Service) putMeta(ctx context.Context, db *sql.DB, instanceID, userID, meta string) error {
	if len(meta) > maxMetadataBytes {
		return errMetaTooLarge
	}
	_, err := db.ExecContext(ctx, `INSERT INTO aiagent_instance_meta (instance_id, user_id, metadata_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET metadata_json = excluded.metadata_json, updated_at = excluded.updated_at`,
		instanceID, userID, meta, nowRFC3339())
	return err
}

// ---------- 用户偏好（多端同步） ----------

// maxPreferenceValueBytes 限制单条偏好值大小。偏好是 UI 设置类小数据，
// 给足余量的同时防止客户端误传大对象把库撑起来。
const maxPreferenceValueBytes = 256 * 1024

var errPreferenceTooLarge = errors.New("preference value too large")
var errInvalidPreferenceKey = errors.New("invalid preference key")

func (s *Service) listPreferences(ctx context.Context, db *sql.DB, userID string) ([]Preference, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value_json, updated_at FROM aiagent_user_preferences WHERE user_id = ? ORDER BY key`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Preference, 0)
	for rows.Next() {
		var item Preference
		var raw string
		if err := rows.Scan(&item.Key, &raw, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Value = json.RawMessage(raw)
		items = append(items, item)
	}
	return items, rows.Err()
}

// putPreferences 批量 upsert 偏好，返回实际写入的键。lastWriteWins 为真表示仅当
// 传入时间戳不早于库中现值时才覆盖，用于避免多端并发下旧数据回退新数据。
func (s *Service) putPreferences(ctx context.Context, db *sql.DB, userID string, values map[string]json.RawMessage, updatedAt string, lastWriteWins bool) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if updatedAt == "" {
		updatedAt = nowRFC3339()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	written := make([]string, 0, len(values))
	for key, value := range values {
		if key == "" || len(key) > 128 {
			return nil, errInvalidPreferenceKey
		}
		if len(value) > maxPreferenceValueBytes {
			return nil, errPreferenceTooLarge
		}
		query := `INSERT INTO aiagent_user_preferences (user_id, key, value_json, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`
		if lastWriteWins {
			query = `INSERT INTO aiagent_user_preferences (user_id, key, value_json, updated_at)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(user_id, key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at
				WHERE excluded.updated_at >= aiagent_user_preferences.updated_at`
		}
		result, err := tx.ExecContext(ctx, query, userID, key, string(value), updatedAt)
		if err != nil {
			return nil, err
		}
		// 被 WHERE 拦下的写入影响行数为 0，不计入实际写入集合。
		if affected, _ := result.RowsAffected(); affected > 0 {
			written = append(written, key)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return written, nil
}

func (s *Service) deletePreferences(ctx context.Context, db *sql.DB, userID string, keys []string) ([]string, error) {
	removed := make([]string, 0, len(keys))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, key := range keys {
		if key == "" || len(key) > 128 {
			return nil, errInvalidPreferenceKey
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM aiagent_user_preferences WHERE user_id = ? AND key = ?`, userID, key)
		if err != nil {
			return nil, err
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			removed = append(removed, key)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return removed, nil
}

// ---------- 访问日志 ----------

func (s *Service) writeAccessLog(ctx context.Context, entry AccessLog) {
	if entry.CreatedAt == "" {
		entry.CreatedAt = nowRFC3339()
	}
	// 审计写入必须尽量落地：即便请求上下文已被取消（客户端中途断开、拒绝登录后
	// 直接断流），也要把记录写进去。这里用脱离请求的短超时上下文。
	logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	db, err := s.store.Open(logCtx)
	if err != nil {
		return
	}
	defer db.Close()
	var userID, tokenID, instanceID, errorText, ip, userAgent interface{}
	if entry.UserID != "" {
		userID = entry.UserID
	}
	if entry.TokenID != "" {
		tokenID = entry.TokenID
	}
	if entry.InstanceID != "" {
		instanceID = entry.InstanceID
	}
	if entry.Error != "" {
		errorText = entry.Error
	}
	if entry.IP != "" {
		ip = entry.IP
	}
	if entry.UserAgent != "" {
		userAgent = entry.UserAgent
	}
	var statusCode interface{}
	if entry.StatusCode != 0 {
		statusCode = entry.StatusCode
	}
	_, _ = db.ExecContext(logCtx, `INSERT INTO aiagent_access_logs
		(user_id, token_id, instance_id, action, result, status_code, error_summary, ip, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, tokenID, instanceID, entry.Action, entry.Result, statusCode, errorText, ip, userAgent, entry.CreatedAt)

	// 低频顺带清理过期日志（每 6 小时最多一次），避免长期运行时表无界增长。
	s.accessLogPurgeMu.Lock()
	shouldPurge := time.Since(s.lastAccessLogPurge) >= accessLogPurgeInterval
	if shouldPurge {
		s.lastAccessLogPurge = time.Now()
	}
	s.accessLogPurgeMu.Unlock()
	if shouldPurge {
		_ = s.purgeAccessLogs(logCtx, db, accessLogRetention)
	}
}

// accessLogPurgeInterval 是运行期清理访问日志的最小间隔。
const accessLogPurgeInterval = 6 * time.Hour

func (s *Service) listAccessLogs(ctx context.Context, db *sql.DB, limit int) ([]AccessLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `SELECT id, user_id, token_id, instance_id, action, result, status_code, error_summary, ip, user_agent, created_at
		FROM aiagent_access_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]AccessLog, 0)
	for rows.Next() {
		var entry AccessLog
		var userID, tokenID, instanceID, errorText, ip, userAgent sql.NullString
		var statusCode sql.NullInt64
		if err := rows.Scan(&entry.ID, &userID, &tokenID, &instanceID, &entry.Action, &entry.Result,
			&statusCode, &errorText, &ip, &userAgent, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entry.UserID = userID.String
		entry.TokenID = tokenID.String
		entry.InstanceID = instanceID.String
		entry.Error = errorText.String
		entry.IP = ip.String
		entry.UserAgent = userAgent.String
		entry.StatusCode = int(statusCode.Int64)
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}
