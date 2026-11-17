package tokenharbor

// 本文件实现邮箱密码登录:直接调 Supabase password grant 登录并入库,
// 免去手动抓 Cookie;登录成功后账号即纳入池并由后台自动续期。

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// handleLogin 处理邮箱密码登录:body {email, password, nickname?}。
func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	email := strings.TrimSpace(body.Email)
	if email == "" || strings.TrimSpace(body.Password) == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请填写邮箱与密码"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	cookie, userID, expiresAtMS, err := s.loginEmail(ctx, email, body.Password)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc := Account{
		Cookie:      cookie,
		Email:       email,
		UserID:      userID,
		Nickname:    strings.TrimSpace(body.Nickname),
		ExpiresAtMS: expiresAtMS,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	acc.ID = deriveAccountID(acc)
	existing, exists := s.findAccount(acc.ID)
	if exists {
		acc.CreatedAt = existing.CreatedAt
		if acc.Nickname == "" {
			acc.Nickname = existing.Nickname
		}
	}
	if err := s.upsertAccount(ctx, acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{
		"success": true,
		"updated": exists,
		"account": s.toAccountView(acc),
	})
}
