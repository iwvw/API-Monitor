package client

import (
	"context"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

var clientSettingsScopes = []string{"main", "model", "web_upgrade", "banner"}

func (c *Client) ReportClientSettings(ctx context.Context, a *auth.RequestAuth, ssoID string) error {
	if c == nil || a == nil {
		return nil
	}
	deviceID := strings.TrimSpace(a.Account.DeviceID)
	if deviceID == "" && c.Store != nil && strings.TrimSpace(a.AccountID) != "" {
		if acc, ok := c.Store.FindAccount(a.AccountID); ok {
			deviceID = strings.TrimSpace(acc.DeviceID)
			a.Account.DeviceID = deviceID
		}
	}
	if strings.TrimSpace(a.DeepSeekToken) == "" || deviceID == "" {
		return nil
	}

	settingsIDs := map[int]struct{}{}
	for _, scope := range clientSettingsScopes {
		payload, err := c.fetchClientSettingsScope(ctx, a, deviceID, scope)
		if err != nil {
			return err
		}
		collectSettingIDs(payload, settingsIDs)
	}
	ids := make([]int, 0, len(settingsIDs))
	for id := range settingsIDs {
		ids = append(ids, id)
	}
	return c.postClientSettingsReport(ctx, a, deviceID, ids, ssoID)
}

func (c *Client) fetchClientSettingsScope(ctx context.Context, a *auth.RequestAuth, deviceID string, scope string) (map[string]any, error) {
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken, a.Account.Locale)
	headers["Accept"] = "application/json"
	query := url.Values{}
	query.Set("did", deviceID)
	query.Set("scope", scope)
	resp, status, err := c.getJSONWithStatus(ctx, clients.regular, dsprotocol.DeepSeekClientSettingsURL+"?"+query.Encode(), headers)
	if err != nil {
		return nil, err
	}
	code, bizCode, msg, bizMsg := extractResponseStatus(resp)
	if status != http.StatusOK || code != 0 || bizCode != 0 {
		return nil, fmt.Errorf("client settings %s failed: status=%d code=%d biz_code=%d msg=%s biz_msg=%s", scope, status, code, bizCode, msg, bizMsg)
	}
	return resp, nil
}

func (c *Client) postClientSettingsReport(ctx context.Context, a *auth.RequestAuth, deviceID string, settingsIDs []int, ssoID string) error {
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken, a.Account.Locale)
	payload := map[string]any{
		"settings_ids": settingsIDs,
		"did":          clientSettingsReportDID(deviceID),
		"sso_id":       strings.TrimSpace(ssoID),
	}
	resp, status, err := c.postJSONWithStatus(ctx, clients.regular, clients.fallback, dsprotocol.DeepSeekClientSettingsReportURL, headers, payload)
	if err != nil {
		return err
	}
	code, bizCode, msg, bizMsg := extractResponseStatus(resp)
	if status != http.StatusOK || code != 0 || bizCode != 0 {
		return fmt.Errorf("client settings report failed: status=%d code=%d biz_code=%d msg=%s biz_msg=%s", status, code, bizCode, msg, bizMsg)
	}
	config.Logger.Info("[client_settings] reported", "account", a.AccountID, "settings_ids", len(settingsIDs))
	return nil
}

func collectSettingIDs(value any, ids map[int]struct{}) {
	if ids == nil || value == nil {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		if id, ok := settingIDFromValue(v["id"]); ok {
			ids[id] = struct{}{}
		}
		for _, child := range v {
			collectSettingIDs(child, ids)
		}
	case []any:
		for _, child := range v {
			collectSettingIDs(child, ids)
		}
	}
}

func settingIDFromValue(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		if v >= math.MinInt && v <= math.MaxInt {
			return int(v), true
		}
	case float64:
		if math.Trunc(v) == v && v >= math.MinInt && v <= math.MaxInt {
			return int(v), true
		}
	}
	return 0, false
}

func clientSettingsReportDID(deviceID string) string {
	deviceID = strings.TrimSpace(deviceID)
	trimmed := strings.TrimPrefix(deviceID, "B")
	if len(trimmed) > 36 {
		trimmed = trimmed[:36]
	}
	if trimmed != "" {
		return trimmed
	}
	return deviceID
}
