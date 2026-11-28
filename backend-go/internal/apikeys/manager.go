package apikeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

const (
	ScopeTOTPRead        = "totp:read"
	ScopeAIMCP           = "ai:mcp"
	ScopeModelHubGateway = "modelhub:gateway"
	ScopeAPIRead         = "api:read"
	ScopeAPIWrite        = "api:write"
)

var (
	ErrInvalid  = errors.New("API Key 无效")
	ErrDisabled = errors.New("API Key 已停用")
	ErrExpired  = errors.New("API Key 已过期")
	ErrDenied   = errors.New("API Key 权限不足")
	ErrNotFound = errors.New("API Key 不存在")
)

type Key struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Scopes        []string `json:"scopes"`
	MaskedKey     string   `json:"maskedKey"`
	APIKey        string   `json:"apiKey,omitempty"`
	Enabled       bool     `json:"enabled"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
	ExpiresAt     *string  `json:"expiresAt"`
	RevokedAt     *string  `json:"revokedAt"`
	LastUsedAt    *string  `json:"lastUsedAt"`
	LastIPAddress string   `json:"lastIpAddress"`
	LastUserAgent string   `json:"lastUserAgent"`
	RequestCount  int64    `json:"requestCount"`
}

type Input struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Scopes    []string `json:"scopes"`
	ExpiresAt *string  `json:"expiresAt"`
	Enabled   *bool    `json:"enabled,omitempty"`
}

type Pairing struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expiresAt"`
}

type Manager struct {
	store             *database.Store
	schema            database.SchemaEnsurer
	trustedProxyCIDRs []string
}

func New(cfg config.Config) *Manager {
	return &Manager{store: database.New(cfg), trustedProxyCIDRs: cfg.TrustedProxyCIDRs}
}

func (m *Manager) open(ctx context.Context) (*sql.DB, error) {
	db, err := m.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.schema.Ensure(func() error { return ensureSchema(ctx, db) }); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS api_access_keys (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		key_hash TEXT NOT NULL UNIQUE,
		key_cipher TEXT NOT NULL,
		key_prefix TEXT NOT NULL,
		key_suffix TEXT NOT NULL,
		scopes_json TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		expires_at TEXT,
		revoked_at TEXT,
		last_used_at TEXT,
		last_ip_address TEXT,
		last_user_agent TEXT,
		request_count INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return fmt.Errorf("ensure api key schema: %w", err)
	}
	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_api_access_keys_hash ON api_access_keys(key_hash)`)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS api_key_pairings (
		code_hash TEXT PRIMARY KEY,
		key_id TEXT NOT NULL,
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		claimed_at TEXT
	)`)
	if err != nil {
		return err
	}
	// 模块改名兼容：历史 API Key 的 kind='openai' 与 scope 'openai:gateway' 迁移到
	// modelhub，否则存量网关 Key 因 scope 不匹配而失效。
	if _, err := db.ExecContext(ctx,
		`UPDATE api_access_keys SET kind='modelhub' WHERE kind='openai'`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE api_access_keys SET scopes_json=REPLACE(scopes_json, '"openai:gateway"', '"modelhub:gateway"') WHERE scopes_json LIKE '%"openai:gateway"%'`); err != nil {
		return err
	}
	return nil
}

func (m *Manager) List(ctx context.Context) ([]Key, error) {
	db, err := m.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id,name,kind,key_cipher,key_prefix,key_suffix,scopes_json,enabled,created_at,updated_at,expires_at,revoked_at,last_used_at,COALESCE(last_ip_address,''),COALESCE(last_user_agent,''),request_count FROM api_access_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []Key{}
	for rows.Next() {
		key, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		key.APIKey = ""
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (m *Manager) Create(ctx context.Context, input Input) (Key, error) {
	input, err := normalizeInput(input)
	if err != nil {
		return Key{}, err
	}
	raw, err := generateKey(input.Kind)
	if err != nil {
		return Key{}, err
	}
	cipher, err := secure.SecureEncrypt(raw)
	if err != nil {
		return Key{}, err
	}
	var expiryValue string
	if input.ExpiresAt != nil {
		expiryValue = *input.ExpiresAt
	}
	expiresAt, err := normalizeExpiry(expiryValue)
	if err != nil {
		return Key{}, err
	}
	scopesJSON, _ := json.Marshal(input.Scopes)
	now := time.Now().UTC().Format(time.RFC3339)
	id := "key_" + uuid.NewString()
	prefix, suffix := keyParts(raw)
	db, err := m.open(ctx)
	if err != nil {
		return Key{}, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `INSERT INTO api_access_keys (id,name,kind,key_hash,key_cipher,key_prefix,key_suffix,scopes_json,enabled,created_at,updated_at,expires_at) VALUES (?,?,?,?,?,?,?,?,1,?,?,?)`,
		id, input.Name, input.Kind, hashKey(raw), cipher, prefix, suffix, string(scopesJSON), now, now, expiresAt)
	if err != nil {
		return Key{}, err
	}
	key := Key{ID: id, Name: input.Name, Kind: input.Kind, Scopes: input.Scopes, MaskedKey: maskKey(prefix, suffix), APIKey: raw, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if value, ok := expiresAt.(string); ok {
		key.ExpiresAt = &value
	}
	return key, nil
}

func (m *Manager) Update(ctx context.Context, id string, input Input) error {
	input, err := normalizeInput(input)
	if err != nil {
		return err
	}
	var expiresAt interface{}
	// 省略 expiresAt(局部更新,只改 name/enabled)时保留原值;显式传空串才表示清除。
	expiresAtSet := input.ExpiresAt != nil
	if expiresAtSet {
		expiresAt, err = normalizeExpiry(*input.ExpiresAt)
		if err != nil {
			return err
		}
	}
	scopesJSON, _ := json.Marshal(input.Scopes)
	var enabled interface{}
	if input.Enabled != nil {
		enabled = 0
		if *input.Enabled {
			enabled = 1
		}
	}
	db, err := m.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := db.ExecContext(ctx, `UPDATE api_access_keys SET name=?,scopes_json=?,enabled=COALESCE(?,enabled),expires_at=CASE WHEN ? THEN ? ELSE expires_at END,updated_at=? WHERE id=?`,
		input.Name, string(scopesJSON), enabled, expiresAtSet, expiresAt, time.Now().UTC().Format(time.RFC3339), id)
	return mutationError(result, err)
}

func (m *Manager) Rotate(ctx context.Context, id string) (string, error) {
	db, err := m.open(ctx)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var kind string
	if err := db.QueryRowContext(ctx, `SELECT kind FROM api_access_keys WHERE id=?`, id).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	raw, err := generateKey(kind)
	if err != nil {
		return "", err
	}
	cipher, err := secure.SecureEncrypt(raw)
	if err != nil {
		return "", err
	}
	prefix, suffix := keyParts(raw)
	result, err := db.ExecContext(ctx, `UPDATE api_access_keys SET key_hash=?,key_cipher=?,key_prefix=?,key_suffix=?,enabled=1,revoked_at=NULL,updated_at=? WHERE id=?`,
		hashKey(raw), cipher, prefix, suffix, time.Now().UTC().Format(time.RFC3339), id)
	if err := mutationError(result, err); err != nil {
		return "", err
	}
	return raw, nil
}

func (m *Manager) Revoke(ctx context.Context, id string) error {
	db, err := m.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.ExecContext(ctx, `UPDATE api_access_keys SET enabled=0,revoked_at=?,updated_at=? WHERE id=?`, now, now, id)
	return mutationError(result, err)
}

func (m *Manager) CreatePluginPairing(ctx context.Context, name string) (Pairing, error) {
	keyExpiresAt := time.Now().UTC().AddDate(0, 3, 0).Format(time.RFC3339)
	key, err := m.Create(ctx, Input{
		Name:      strings.TrimSpace(name),
		Kind:      "plugin",
		ExpiresAt: &keyExpiresAt,
	})
	if err != nil {
		return Pairing{}, err
	}
	code, err := generatePairingCode()
	if err != nil {
		_ = m.Revoke(ctx, key.ID)
		return Pairing{}, err
	}
	expiresAt := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
	db, err := m.open(ctx)
	if err != nil {
		_ = m.Revoke(ctx, key.ID)
		return Pairing{}, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `INSERT INTO api_key_pairings (code_hash,key_id,created_at,expires_at) VALUES (?,?,?,?)`,
		hashKey(code), key.ID, time.Now().UTC().Format(time.RFC3339), expiresAt)
	if err != nil {
		_ = m.Revoke(ctx, key.ID)
		return Pairing{}, err
	}
	return Pairing{Code: code, ExpiresAt: expiresAt}, nil
}

func (m *Manager) ClaimPluginPairing(ctx context.Context, code string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(code), "pair_") {
		return "", ErrInvalid
	}
	db, err := m.open(ctx)
	if err != nil {
		return "", err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var keyID, expiresAt, cipher string
	var claimedAt sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT p.key_id,p.expires_at,p.claimed_at,k.key_cipher
		FROM api_key_pairings p JOIN api_access_keys k ON k.id=p.key_id WHERE p.code_hash=?`, hashKey(code)).Scan(&keyID, &expiresAt, &claimedAt, &cipher)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalid
	}
	if err != nil {
		return "", err
	}
	deadline, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || claimedAt.Valid || !deadline.After(time.Now()) {
		return "", ErrInvalid
	}
	claimed := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.ExecContext(ctx, `UPDATE api_key_pairings SET claimed_at=? WHERE code_hash=? AND claimed_at IS NULL`, claimed, hashKey(code))
	if err := mutationError(result, err); err != nil {
		return "", ErrInvalid
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	apiKey := secure.SecureDecrypt(cipher)
	if !strings.HasPrefix(apiKey, "akp_") {
		return "", ErrInvalid
	}
	return apiKey, nil
}

func (m *Manager) Authorize(ctx context.Context, r *http.Request, scope string) (Key, error) {
	raw := bearerToken(r)
	if raw == "" {
		return Key{}, ErrInvalid
	}
	db, err := m.open(ctx)
	if err != nil {
		return Key{}, err
	}
	defer db.Close()
	row := db.QueryRowContext(ctx, `SELECT id,name,kind,key_cipher,key_prefix,key_suffix,scopes_json,enabled,created_at,updated_at,expires_at,revoked_at,last_used_at,COALESCE(last_ip_address,''),COALESCE(last_user_agent,''),request_count FROM api_access_keys WHERE key_hash=? LIMIT 1`, hashKey(raw))
	key, err := scanKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Key{}, ErrInvalid
	}
	if err != nil {
		return Key{}, err
	}
	if key.Kind == "plugin" {
		var activeSessions int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM sessions WHERE is_active=1 AND expires_at > ?`, time.Now().UTC().Format(time.RFC3339)).Scan(&activeSessions); err != nil {
			return Key{}, err
		}
		if activeSessions == 0 {
			return Key{}, ErrDisabled
		}
	}
	if !key.Enabled || key.RevokedAt != nil {
		return Key{}, ErrDisabled
	}
	if key.ExpiresAt != nil {
		expires, err := time.Parse(time.RFC3339, *key.ExpiresAt)
		if err != nil || !expires.After(time.Now()) {
			return Key{}, ErrExpired
		}
	}
	if !hasScope(key.Scopes, scope) {
		return Key{}, ErrDenied
	}
	now := time.Now().UTC().Format(time.RFC3339)
	ip := requestIP(r, m.trustedProxyCIDRs)
	_, _ = db.ExecContext(ctx, `UPDATE api_access_keys SET last_used_at=?,last_ip_address=?,last_user_agent=?,request_count=request_count+1 WHERE id=?`, now, ip, r.UserAgent(), key.ID)
	key.LastUsedAt = &now
	key.LastIPAddress = ip
	key.LastUserAgent = r.UserAgent()
	key.RequestCount++
	return key, nil
}

func normalizeInput(input Input) (Input, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	if input.Name == "" {
		return input, errors.New("密钥名称必填")
	}
	switch input.Kind {
	case "plugin":
		input.Scopes = []string{ScopeTOTPRead}
	case "ai":
		input.Scopes = []string{ScopeAIMCP}
	case "modelhub":
		input.Scopes = []string{ScopeModelHubGateway}
	case "api":
		allowed := map[string]bool{ScopeAPIRead: true, ScopeAPIWrite: true}
		clean := []string{}
		for _, scope := range input.Scopes {
			scope = strings.TrimSpace(scope)
			if allowed[scope] && !hasScope(clean, scope) {
				clean = append(clean, scope)
			}
		}
		if len(clean) == 0 {
			return input, errors.New("通用 API Key 至少需要一个权限")
		}
		input.Scopes = clean
	default:
		return input, errors.New("不支持的密钥类型")
	}
	return input, nil
}

func normalizeExpiry(value string) (interface{}, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("过期时间格式无效")
	}
	return parsed.UTC().Format(time.RFC3339), nil
}

func generateKey(kind string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	prefix := map[string]string{"plugin": "akp_", "ai": "aka_", "modelhub": "akh_", "api": "ak_"}[kind]
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func generatePairingCode() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "pair_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func keyParts(raw string) (string, string) {
	if len(raw) <= 14 {
		return raw, raw
	}
	return raw[:10], raw[len(raw)-4:]
}

func maskKey(prefix, suffix string) string { return prefix + "..." + suffix }

func bearerToken(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-API-Key")); value != "" {
		return value
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authorization) > 7 && strings.EqualFold(authorization[:7], "Bearer ") {
		return strings.TrimSpace(authorization[7:])
	}
	return ""
}

func hasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required || scope == "*" {
			return true
		}
	}
	return false
}

func requestIP(r *http.Request, trustedProxyCIDRs []string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		if isTrustedProxy(net.ParseIP(host), trustedProxyCIDRs) {
			if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
				candidate := strings.TrimSpace(strings.Split(forwarded, ",")[0])
				if ip := net.ParseIP(candidate); ip != nil {
					return ip.String()
				}
			}
		}
		return host
	}
	return strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
}

func isTrustedProxy(ip net.IP, entries []string) bool {
	if ip == nil {
		return false
	}
	for _, entry := range entries {
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
			return true
		}
		if candidate := net.ParseIP(entry); candidate != nil && candidate.Equal(ip) {
			return true
		}
	}
	return false
}

func scanKey(scanner interface{ Scan(...interface{}) error }) (Key, error) {
	var key Key
	var cipher, scopesJSON, keyPrefix, keySuffix string
	var enabled int
	var expires, revoked, lastUsed sql.NullString
	err := scanner.Scan(&key.ID, &key.Name, &key.Kind, &cipher, &keyPrefix, &keySuffix, &scopesJSON, &enabled, &key.CreatedAt, &key.UpdatedAt, &expires, &revoked, &lastUsed, &key.LastIPAddress, &key.LastUserAgent, &key.RequestCount)
	if err != nil {
		return key, err
	}
	key.Enabled = enabled == 1
	key.APIKey = secure.SecureDecrypt(cipher)
	key.MaskedKey = maskKey(keyPrefix, keySuffix)
	_ = json.Unmarshal([]byte(scopesJSON), &key.Scopes)
	if expires.Valid {
		key.ExpiresAt = &expires.String
	}
	if revoked.Valid {
		key.RevokedAt = &revoked.String
	}
	if lastUsed.Valid {
		key.LastUsedAt = &lastUsed.String
	}
	return key, nil
}

func mutationError(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
