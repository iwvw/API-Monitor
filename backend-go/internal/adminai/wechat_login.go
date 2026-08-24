package adminai

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/adminai/channel"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// wechatQRCode POST /api/admin-ai/channels/{id}/wechat/qrcode
// 向 iLink API 请求登录二维码，返回二维码 URL 和 Base64 图片。
// 无需 bot_token（此端点用于获取 token 的前置步骤）。
func (s *Service) wechatQRCode(w http.ResponseWriter, r *http.Request, channelID string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	var ctype, encrypted string
	err = db.QueryRowContext(r.Context(),
		`SELECT type, config_encrypted FROM admin_ai_channels WHERE id = ?`, channelID).Scan(&ctype, &encrypted)
	if err != nil {
		response.Error(w, http.StatusNotFound, "频道不存在")
		return
	}
	if ctype != "wechat" {
		response.Error(w, http.StatusBadRequest, "该端点仅用于微信频道")
		return
	}

	client := channel.NewILinkClient("", "")
	qrData, err := client.GetBotQRCode(r.Context())
	if err != nil {
		response.Error(w, http.StatusBadGateway, "获取微信二维码失败："+err.Error())
		return
	}

	qrcode, _ := qrData["qrcode"].(string)
	qrcodeImg, _ := qrData["qrcode_img_content"].(string)
	qrcodeURL, _ := qrData["url"].(string)
	// iLink 实际返回的 qrcode_img_content 可能是完整链接（liteapp.weixin.qq.com/...）
	// 而非 base64 图片；此时把它作为扫码链接返回，供前端本地生成二维码。
	if qrcodeURL == "" && strings.HasPrefix(qrcodeImg, "http") {
		qrcodeURL = qrcodeImg
		qrcodeImg = ""
	}

	response.OK(w, map[string]interface{}{
		"qrcode":    qrcode,
		"qrcodeImg": qrcodeImg,
		"qrcodeUrl": qrcodeURL,
	})
}

// wechatQRCodeStatus GET /api/admin-ai/channels/{id}/wechat/qrcode/status?qrcode=xxx
// 轮询二维码扫描状态。确认登录后自动将 bot_token 持久化到频道配置。
func (s *Service) wechatQRCodeStatus(w http.ResponseWriter, r *http.Request, channelID string) {
	qrcode := r.URL.Query().Get("qrcode")
	if qrcode == "" {
		response.Error(w, http.StatusBadRequest, "缺少 qrcode 参数")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	var ctype, encrypted string
	err = db.QueryRowContext(r.Context(),
		`SELECT type, config_encrypted FROM admin_ai_channels WHERE id = ?`, channelID).Scan(&ctype, &encrypted)
	if err != nil {
		response.Error(w, http.StatusNotFound, "频道不存在")
		return
	}
	if ctype != "wechat" {
		response.Error(w, http.StatusBadRequest, "该端点仅用于微信频道")
		return
	}

	client := channel.NewILinkClient("", "")
	statusData, err := client.GetQRCodeStatus(r.Context(), qrcode)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "查询二维码状态失败："+err.Error())
		return
	}

	status, _ := statusData["status"].(string)

	if status == "confirmed" {
		botToken, _ := statusData["bot_token"].(string)
		baseURL, _ := statusData["baseurl"].(string)
		if botToken == "" {
			response.Error(w, http.StatusInternalServerError, "二维码已确认但未返回 bot_token")
			return
		}

		var cfg channel.WeChatConfig
		if encrypted != "" {
			_ = secure.DecryptJSON(encrypted, &cfg)
		}
		cfg.BotToken = botToken
		newEncrypted, err := secure.EncryptJSON(cfg)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "配置加密失败")
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		_, err = db.ExecContext(r.Context(),
			`UPDATE admin_ai_channels SET config_encrypted = ?, updated_at = ? WHERE id = ?`,
			newEncrypted, now, channelID)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		var enabled int
		_ = db.QueryRowContext(r.Context(), `SELECT enabled FROM admin_ai_channels WHERE id = ?`, channelID).Scan(&enabled)
		if enabled == 1 {
			s.stopChannelInstance(channelID)
			_ = s.startChannelInstance(r.Context(), channelID)
		}

		slog.Info("wechat-qrcode-login-confirmed", "channelId", channelID)
		response.OK(w, map[string]interface{}{
			"status":      "confirmed",
			"botTokenSet": true,
			"baseURL":     baseURL,
		})
		return
	}

	response.OK(w, map[string]interface{}{
		"status": status,
	})
}

// handleWechatChannelPath 处理 /channels/{id}/wechat/* 子路径。
// 返回 true 表示已处理（命中 wechat 子路由），false 表示不匹配。
func (s *Service) handleWechatChannelPath(w http.ResponseWriter, r *http.Request, channelID, action string) bool {
	switch {
	case action == "wechat/qrcode" && r.Method == http.MethodPost:
		s.wechatQRCode(w, r, channelID)
		return true
	case action == "wechat/qrcode/status" && r.Method == http.MethodGet:
		s.wechatQRCodeStatus(w, r, channelID)
		return true
	}
	return false
}
