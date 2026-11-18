package modelhub

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// endpointKeyRuntimeItem 描述单个 API Key 的运行时状态，供端点编辑弹窗标注
// 「冷却中/不可用」的 key。key 值本身不回显请求方之外的任何地方，仅用于前端
// 与编辑框行内容做字符串对齐。
type endpointKeyRuntimeItem struct {
	Key           string `json:"key"`
	Cooled        bool   `json:"cooled"`
	CooldownUntil string `json:"cooldownUntil,omitempty"`
	FailCount     int    `json:"failCount,omitempty"`
	LastStatus    int    `json:"lastStatus,omitempty"`
	LastError     string `json:"lastError,omitempty"`
}

// getEndpointKeyStateRoute 返回端点各 API Key 的运行时冷却状态。
// 数据源是 relayLoop 在 401/403/429 后写入的 keyStateByEndpoint（内存态，
// 成功转发即清除），因此它反映的是「此刻是否还会被 pickKey 回避」。
func (s *Service) getEndpointKeyStateRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var apiKey string
	var apiKeysRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT api_key, api_keys FROM modelhub_endpoints WHERE id = ?", id).Scan(&apiKey, &apiKeysRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	ep := Endpoint{APIKey: strings.TrimSpace(secure.SecureDecrypt(apiKey))}
	if apiKeysRaw.Valid && apiKeysRaw.String != "" {
		_ = json.Unmarshal([]byte(secure.SecureDecrypt(apiKeysRaw.String)), &ep.APIKeys)
	}

	health := s.keyHealthByEndpoint(id)
	now := time.Now()
	items := make([]endpointKeyRuntimeItem, 0, len(ep.AllKeys()))
	for _, key := range ep.AllKeys() {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		item := endpointKeyRuntimeItem{Key: key}
		if entry, ok := health[key]; ok {
			item.FailCount = entry.failCount
			item.LastStatus = entry.lastStatus
			item.LastError = entry.lastError
			if !entry.cooldownUntil.IsZero() && now.Before(entry.cooldownUntil) {
				item.Cooled = true
				item.CooldownUntil = entry.cooldownUntil.Format(time.RFC3339)
			}
		}
		items = append(items, item)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "keys": items})
}
