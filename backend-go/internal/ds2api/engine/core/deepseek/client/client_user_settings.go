package client

import (
	"context"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"fmt"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// DisableTrainingAllowed 关闭账号的"数据用于优化体验"（training_allowed）授权。
// 在登录/刷新 token 后通过 PostLogin 钩子调用，保证账号始终处于关闭状态。
// 失败仅记录日志，不阻断登录或后续业务流程。
func (c *Client) DisableTrainingAllowed(ctx context.Context, a *auth.RequestAuth) error {
	if c == nil || a == nil {
		return nil
	}
	if strings.TrimSpace(a.DeepSeekToken) == "" {
		return nil
	}
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken, a.Account.Locale)
	payload := map[string]any{
		"training_allowed": false,
	}

	resp, status, err := c.postJSONWithStatus(ctx, clients.regular, clients.fallback, dsprotocol.DeepSeekUpdateSettingsURL, headers, payload)
	if err != nil {
		return fmt.Errorf("disable training_allowed request error: %w", err)
	}

	code, bizCode, msg, bizMsg := extractResponseStatus(resp)
	if status != http.StatusOK || code != 0 || bizCode != 0 {
		return fmt.Errorf("disable training_allowed failed: status=%d code=%d biz_code=%d msg=%s biz_msg=%s", status, code, bizCode, msg, bizMsg)
	}

	config.Logger.Info("[disable_training] training_allowed disabled", "account", a.AccountID)
	return nil
}
