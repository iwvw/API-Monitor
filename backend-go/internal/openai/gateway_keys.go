package openai

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/apikeys"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

type gatewayKeyContextKey struct{}

type GatewayKey struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	KeyPrefix        string   `json:"keyPrefix"`
	KeySuffix        string   `json:"keySuffix"`
	MaskedKey        string   `json:"maskedKey"`
	APIKey           string   `json:"apiKey,omitempty"`
	Enabled          bool     `json:"enabled"`
	IsDefault        bool     `json:"isDefault"`
	CreatedAt        string   `json:"createdAt"`
	LastUsed         *string  `json:"lastUsed"`
	ExpiresAt        *string  `json:"expiresAt"`
	RequestCount     int64    `json:"requestCount"`
	AllowedModels    []string `json:"allowedModels,omitempty"`
	AllowedEndpoints []string `json:"allowedEndpoints,omitempty"`
	MaxTokensQuota   int64    `json:"maxTokensQuota"`
	TotalTokensUsed  int64    `json:"totalTokensUsed"`
}

type gatewayKeyIdentity struct {
	ID               string
	Name             string
	AllowedModels    []string
	AllowedEndpoints []string
	MaxTokensQuota   int64
	TotalTokensUsed  int64
}

var (
	errGatewayKeyMissing  = errors.New("缺少 API Key")
	errGatewayKeyInvalid  = errors.New("API Key 无效或已禁用")
	errGatewayKeyExpired  = errors.New("API Key 已过期")
	errGatewayKeyNotFound = errors.New("API Key 不存在")
)

func gatewayKeyFromContext(ctx context.Context) gatewayKeyIdentity {
	identity, _ := ctx.Value(gatewayKeyContextKey{}).(gatewayKeyIdentity)
	return identity
}

func (s *Service) AuthorizeGatewayRequest(r *http.Request) (*http.Request, error) {
	// 本机内部调用（管理 AI 会话 / 部署脚本等）免密钥放行，以“internal”身份进入网关。
	// 信任边界：运行本后台的宿主机上的任何进程（loopback 来源）都被视为内部可信调用，
	// 可免密钥消费/路由/审计网关能力。这与单机部署的既有信任模型一致；若需多租户或
	// 暴露到不可信网络，应改由 API Key 强制鉴权并移除该放行。
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && (host == "127.0.0.1" || host == "::1") {
		return r.WithContext(context.WithValue(r.Context(), gatewayKeyContextKey{}, gatewayKeyIdentity{ID: "internal", Name: "内部调用"})), nil
	}
	rawKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if rawKey == "" {
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			rawKey = strings.TrimSpace(authHeader[len("Bearer "):])
		}
	}
	if rawKey == "" {
		return r, errGatewayKeyMissing
	}

	if key, err := s.apiKeys.Authorize(r.Context(), r, apikeys.ScopeOpenAIGateway); err == nil {
		identity := gatewayKeyIdentity{ID: key.ID, Name: key.Name}
		return r.WithContext(context.WithValue(r.Context(), gatewayKeyContextKey{}, identity)), nil
	} else if errors.Is(err, apikeys.ErrExpired) {
		return r, errGatewayKeyExpired
	} else if errors.Is(err, apikeys.ErrDisabled) {
		return r, errGatewayKeyInvalid
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		return r, err
	}
	defer db.Close()

	var identity gatewayKeyIdentity
	var enabledInt int
	var expiresAt sql.NullString
	var allowedModelsRaw, allowedEndpointsRaw sql.NullString
	var maxQuota, usedTokens int64
	err = db.QueryRowContext(ctx, `
		SELECT id, name, enabled, expires_at, COALESCE(allowed_models, ''), COALESCE(allowed_endpoints, ''), COALESCE(max_tokens_quota, 0), COALESCE(total_tokens_used, 0)
		FROM openai_gateway_keys
		WHERE key_hash = ?
		LIMIT 1`, hashGatewayKey(rawKey)).
		Scan(&identity.ID, &identity.Name, &enabledInt, &expiresAt, &allowedModelsRaw, &allowedEndpointsRaw, &maxQuota, &usedTokens)
	if errors.Is(err, sql.ErrNoRows) || enabledInt != 1 {
		return r, errGatewayKeyInvalid
	}
	if err != nil {
		return r, err
	}
	if expiresAt.Valid {
		expires, parseErr := time.Parse(time.RFC3339, expiresAt.String)
		if parseErr == nil && !expires.After(time.Now()) {
			return r, errGatewayKeyExpired
		}
	}
	if allowedModelsRaw.String != "" {
		_ = json.Unmarshal([]byte(allowedModelsRaw.String), &identity.AllowedModels)
	}
	if allowedEndpointsRaw.String != "" {
		_ = json.Unmarshal([]byte(allowedEndpointsRaw.String), &identity.AllowedEndpoints)
	}
	identity.MaxTokensQuota = maxQuota
	identity.TotalTokensUsed = usedTokens

	now := time.Now().Format(time.RFC3339)
	_, _ = db.ExecContext(ctx, `
		UPDATE openai_gateway_keys
		SET last_used = ?, request_count = request_count + 1
		WHERE id = ?`, now, identity.ID)

	return r.WithContext(context.WithValue(ctx, gatewayKeyContextKey{}, identity)), nil
}

func (s *Service) listGatewayKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT id, name, key_cipher, key_prefix, key_suffix, enabled, is_default, created_at, last_used, expires_at, request_count,
			COALESCE(allowed_models, ''), COALESCE(allowed_endpoints, ''), COALESCE(max_tokens_quota, 0), COALESCE(total_tokens_used, 0)
		FROM openai_gateway_keys
		ORDER BY is_default DESC, created_at DESC`)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	keys := []GatewayKey{}
	for rows.Next() {
		key, scanErr := scanGatewayKey(rows)
		if scanErr != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": scanErr.Error()})
			return
		}
		keys = append(keys, key)
	}
	response.JSON(w, http.StatusOK, keys)
}

func (s *Service) createGatewayKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string   `json:"name"`
		ExpiresAt        string   `json:"expiresAt"`
		AllowedModels    []string `json:"allowedModels"`
		AllowedEndpoints []string `json:"allowedEndpoints"`
		MaxTokensQuota   int64    `json:"maxTokensQuota"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "密钥名称必填"})
		return
	}

	expiresAt, err := normalizeGatewayKeyExpiry(req.ExpiresAt)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rawKey, err := generateGatewayKey()
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "生成 API Key 失败"})
		return
	}
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	allowedModelsJSON, _ := json.Marshal(cleanStringList(req.AllowedModels))
	allowedEndpointsJSON, _ := json.Marshal(cleanStringList(req.AllowedEndpoints))

	id := "gk_" + uuid.NewString()
	createdAt := time.Now().Format(time.RFC3339)
	prefix, suffix := gatewayKeyParts(rawKey)
	keyCipher, encryptErr := secure.SecureEncrypt(rawKey)
	if encryptErr != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "加密密钥失败"})
		return
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO openai_gateway_keys
			(id, name, key_hash, key_cipher, key_prefix, key_suffix, enabled, created_at, expires_at, allowed_models, allowed_endpoints, max_tokens_quota)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?)`,
		id, req.Name, hashGatewayKey(rawKey), keyCipher, prefix, suffix, createdAt, expiresAt,
		string(allowedModelsJSON), string(allowedEndpointsJSON), maxInt64(req.MaxTokensQuota, 0))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"apiKey":  rawKey,
		"key": GatewayKey{
			ID:               id,
			Name:             req.Name,
			KeyPrefix:        prefix,
			KeySuffix:        suffix,
			MaskedKey:        maskGatewayKey(prefix, suffix),
			Enabled:          true,
			CreatedAt:        createdAt,
			ExpiresAt:        nullableStringPointer(expiresAt),
			AllowedModels:    cleanStringList(req.AllowedModels),
			AllowedEndpoints: cleanStringList(req.AllowedEndpoints),
			MaxTokensQuota:   maxInt64(req.MaxTokensQuota, 0),
		},
	})
}

func (s *Service) updateGatewayKey(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Name             string   `json:"name"`
		ExpiresAt        string   `json:"expiresAt"`
		AllowedModels    []string `json:"allowedModels"`
		AllowedEndpoints []string `json:"allowedEndpoints"`
		MaxTokensQuota   *int64   `json:"maxTokensQuota"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "密钥名称必填"})
		return
	}
	expiresAt, err := normalizeGatewayKeyExpiry(req.ExpiresAt)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	allowedModelsJSON, _ := json.Marshal(cleanStringList(req.AllowedModels))
	allowedEndpointsJSON, _ := json.Marshal(cleanStringList(req.AllowedEndpoints))
	quota := int64(0)
	if req.MaxTokensQuota != nil && *req.MaxTokensQuota > 0 {
		quota = *req.MaxTokensQuota
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()
	result, err := db.ExecContext(r.Context(), `
		UPDATE openai_gateway_keys
		SET name = ?, expires_at = ?, allowed_models = ?, allowed_endpoints = ?, max_tokens_quota = ?
		WHERE id = ?`,
		req.Name, expiresAt, string(allowedModelsJSON), string(allowedEndpointsJSON), quota, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": errGatewayKeyNotFound.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) toggleGatewayKey(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	enabled := 0
	if req.Enabled {
		enabled = 1
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()
	result, err := db.ExecContext(r.Context(), "UPDATE openai_gateway_keys SET enabled = ? WHERE id = ?", enabled, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeGatewayKeyMutationResult(w, result)
}

func (s *Service) rotateGatewayKey(w http.ResponseWriter, r *http.Request, id string) {
	rawKey, err := generateGatewayKey()
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "生成 API Key 失败"})
		return
	}
	prefix, suffix := gatewayKeyParts(rawKey)

	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()
	keyCipher, encryptErr := secure.SecureEncrypt(rawKey)
	if encryptErr != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "加密密钥失败"})
		return
	}
	result, err := db.ExecContext(r.Context(), `
		UPDATE openai_gateway_keys
		SET key_hash = ?, key_cipher = ?, key_prefix = ?, key_suffix = ?, last_used = NULL, request_count = 0
		WHERE id = ?`, hashGatewayKey(rawKey), keyCipher, prefix, suffix, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": errGatewayKeyNotFound.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "apiKey": rawKey})
}

func (s *Service) setDefaultGatewayKey(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM openai_gateway_keys WHERE id = ?", id).Scan(&exists); err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": errGatewayKeyNotFound.Error()})
		return
	}
	if _, err := tx.ExecContext(ctx, "UPDATE openai_gateway_keys SET is_default = 0"); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.ExecContext(ctx, "UPDATE openai_gateway_keys SET is_default = 1 WHERE id = ?", id); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deleteGatewayKey(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()
	result, err := db.ExecContext(r.Context(), "DELETE FROM openai_gateway_keys WHERE id = ?", id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeGatewayKeyMutationResult(w, result)
}

func scanGatewayKey(scanner interface {
	Scan(dest ...interface{}) error
}) (GatewayKey, error) {
	var key GatewayKey
	var enabled int
	var isDefault int
	var keyCipher, lastUsed, expiresAt sql.NullString
	var allowedModelsRaw, allowedEndpointsRaw string
	err := scanner.Scan(
		&key.ID,
		&key.Name,
		&keyCipher,
		&key.KeyPrefix,
		&key.KeySuffix,
		&enabled,
		&isDefault,
		&key.CreatedAt,
		&lastUsed,
		&expiresAt,
		&key.RequestCount,
		&allowedModelsRaw,
		&allowedEndpointsRaw,
		&key.MaxTokensQuota,
		&key.TotalTokensUsed,
	)
	if err != nil {
		return key, err
	}
	key.Enabled = enabled == 1
	key.IsDefault = isDefault == 1
	key.MaskedKey = maskGatewayKey(key.KeyPrefix, key.KeySuffix)
	if keyCipher.Valid {
		key.APIKey = secure.SecureDecrypt(keyCipher.String)
	}
	if lastUsed.Valid {
		key.LastUsed = &lastUsed.String
	}
	if expiresAt.Valid {
		key.ExpiresAt = &expiresAt.String
	}
	if allowedModelsRaw != "" {
		_ = json.Unmarshal([]byte(allowedModelsRaw), &key.AllowedModels)
	}
	if allowedEndpointsRaw != "" {
		_ = json.Unmarshal([]byte(allowedEndpointsRaw), &key.AllowedEndpoints)
	}
	return key, nil
}

func writeGatewayKeyMutationResult(w http.ResponseWriter, result sql.Result) {
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": errGatewayKeyNotFound.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func generateGatewayKey() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "sk-am-" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashGatewayKey(rawKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawKey)))
	return hex.EncodeToString(sum[:])
}

func gatewayKeyParts(rawKey string) (string, string) {
	if len(rawKey) <= 12 {
		return rawKey, rawKey
	}
	return rawKey[:10], rawKey[len(rawKey)-4:]
}

func maskGatewayKey(prefix, suffix string) string {
	return prefix + "..." + suffix
}

func normalizeGatewayKeyExpiry(value string) (interface{}, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("过期时间格式无效")
	}
	if !parsed.After(time.Now()) {
		return nil, errors.New("过期时间必须晚于当前时间")
	}
	return parsed.Format(time.RFC3339), nil
}

func nullableStringPointer(value interface{}) *string {
	text, ok := value.(string)
	if !ok || text == "" {
		return nil
	}
	return &text
}

// cleanStringList 去重并剔除空字符串，返回有序列表。
func cleanStringList(items []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// enforceGatewayKeyLimits 校验网关密钥的模型/端点白名单与 token 配额。
// 返回错误消息（空表示通过）。
func (s *Service) enforceGatewayKeyLimits(ctx context.Context, identity gatewayKeyIdentity, model string, endpointID string) string {
	if len(identity.AllowedModels) > 0 {
		allowed := map[string]bool{}
		for _, m := range identity.AllowedModels {
			allowed[m] = true
		}
		if !allowed[model] {
			return "模型不在该密钥的允许列表中"
		}
	}
	if len(identity.AllowedEndpoints) > 0 {
		allowed := map[string]bool{}
		for _, id := range identity.AllowedEndpoints {
			allowed[id] = true
		}
		if !allowed[endpointID] {
			return "端点不在该密钥的允许列表中"
		}
	}
	if identity.MaxTokensQuota > 0 && identity.TotalTokensUsed >= identity.MaxTokensQuota {
		return "该密钥的 Token 配额已用尽"
	}
	return ""
}

// consumeGatewayKeyTokens 在请求完成后累加该密钥的 token 用量。
func (s *Service) consumeGatewayKeyTokens(ctx context.Context, identity gatewayKeyIdentity, tokens int64) {
	if identity.ID == "" || tokens <= 0 {
		return
	}
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.ExecContext(ctx, `
		UPDATE openai_gateway_keys
		SET total_tokens_used = total_tokens_used + ?
		WHERE id = ?`, tokens, identity.ID)
}

// filterModelsByKey 按密钥白名单过滤模型列表；白名单为空时返回原列表。
func filterModelsByKey(identity gatewayKeyIdentity, models []map[string]interface{}) []map[string]interface{} {
	if len(identity.AllowedModels) == 0 {
		return models
	}
	allowed := map[string]bool{}
	for _, m := range identity.AllowedModels {
		allowed[m] = true
	}
	out := []map[string]interface{}{}
	for _, m := range models {
		id, _ := m["id"].(string)
		if allowed[id] {
			out = append(out, m)
		}
	}
	return out
}
