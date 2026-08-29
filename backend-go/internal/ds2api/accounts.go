package ds2api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	engineaccount "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/account"
	engineauth "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	engineconfig "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	engineds "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
)

// accountView 是返回给前端的账号展示结构。
type accountView struct {
	Name       string `json:"name,omitempty"`
	Email      string `json:"email,omitempty"`
	Mobile     string `json:"mobile,omitempty"`
	Disabled   bool   `json:"disabled"`
	Banned     bool   `json:"banned"`
	PoolType   string `json:"poolType,omitempty"`
	Identifier string `json:"identifier"`
	// 冷却状态（Unix 秒，<=0 表示无冷却）。
	MutedUntil      float64 `json:"mutedUntil,omitempty"`
	CooldownUntil   float64 `json:"cooldownUntil,omitempty"`
	NodeCooldownUntil float64 `json:"nodeCooldownUntil,omitempty"`
	Available       bool    `json:"available"`
}

// handleAccounts 管理 DeepSeek 网页版账号池。
func (s *Service) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAccounts(w, r)
	case http.MethodPost:
		s.addAccount(w, r)
	default:
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
	}
}

// loadEngineStore 从落盘 config.json 加载引擎配置（须引擎已启用）。
func (s *Service) loadEngineStore() (*engineconfig.Store, error) {
	store, err := engineconfig.LoadStoreWithError()
	if err != nil {
		return nil, err
	}
	return store, nil
}

// persistEngineStore 用新配置更新落盘文件，并重启引擎使其生效。
func (s *Service) persistEngineStore(ctx context.Context, mutate func(*engineconfig.Config) error) error {
	store, err := s.loadEngineStore()
	if err != nil {
		return err
	}
	if err := store.Update(mutate); err != nil {
		return err
	}
	// 把引擎落盘配置同步回插件 DB 设置，保持单源一致。
	st := s.Settings()
	raw, _ := json.Marshal(store.Snapshot())
	st.ConfigJSON = string(raw)
	return s.SaveSettings(ctx, st)
}

func (s *Service) listAccounts(w http.ResponseWriter, r *http.Request) {
	store, err := s.loadEngineStore()
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "引擎未启用或配置不可读: " + err.Error()})
		return
	}
	accs := store.Snapshot().Accounts
	out := make([]accountView, 0, len(accs))
	for _, a := range accs {
		out = append(out, accountView{
			Name:            a.Name,
			Email:           a.Email,
			Mobile:          a.Mobile,
			Disabled:        a.Disabled,
			Banned:          a.Banned,
			PoolType:        a.PoolType,
			Identifier:      a.Identifier(),
			MutedUntil:      a.MutedUntil,
			CooldownUntil:   a.CooldownUntil,
			NodeCooldownUntil: a.NodeCooldownUntil,
			Available:       a.IsEnabled() && !a.IsMuted() && !a.IsBanned() && !a.IsCoolingDown(),
		})
	}
	responseJSON(w, map[string]interface{}{"success": true, "accounts": out})
}

func (s *Service) addAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Mobile   string `json:"mobile"`
		Password string `json:"password"`
		PoolType string `json:"poolType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	body.Email = strings.TrimSpace(body.Email)
	body.Mobile = strings.TrimSpace(body.Mobile)
	body.Password = strings.TrimSpace(body.Password)
	if body.Email == "" && body.Mobile == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "邮箱或手机号至少填一项"})
		return
	}
	if body.Password == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请填写密码"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	err := s.persistEngineStore(ctx, func(cfg *engineconfig.Config) error {
		cfg.Accounts = append(cfg.Accounts, engineconfig.Account{
			Name:     body.Name,
			Email:    body.Email,
			Mobile:   body.Mobile,
			Password: body.Password,
			PoolType: body.PoolType,
		})
		return nil
	})
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

func (s *Service) handleDeleteAccount(w http.ResponseWriter, r *http.Request, identifier string) {
	if r.Method != http.MethodDelete {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	identifier = strings.TrimSpace(identifier)
	err := s.persistEngineStore(ctx, func(cfg *engineconfig.Config) error {
		out := cfg.Accounts[:0]
		for _, a := range cfg.Accounts {
			if a.Identifier() != identifier {
				out = append(out, a)
			}
		}
		cfg.Accounts = out
		return nil
	})
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// handleUpdateAccount 编辑账号可编辑字段（名称、备注、邮箱、手机号、密码、池类型）。
func (s *Service) handleUpdateAccount(w http.ResponseWriter, r *http.Request, identifier string) {
	if r.Method != http.MethodPut {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Name     string `json:"name"`
		Remark   string `json:"remark"`
		Email    string `json:"email"`
		Mobile   string `json:"mobile"`
		Password string `json:"password"`
		PoolType string `json:"poolType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	body.Email = strings.TrimSpace(body.Email)
	body.Mobile = strings.TrimSpace(body.Mobile)
	body.Password = strings.TrimSpace(body.Password)
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少账号标识"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	found := false
	err := s.persistEngineStore(ctx, func(cfg *engineconfig.Config) error {
		for i := range cfg.Accounts {
			if cfg.Accounts[i].Identifier() != identifier {
				continue
			}
			found = true
			cfg.Accounts[i].Name = body.Name
			cfg.Accounts[i].Remark = body.Remark
			cfg.Accounts[i].Email = body.Email
			cfg.Accounts[i].Mobile = body.Mobile
			if body.Password != "" {
				cfg.Accounts[i].Password = body.Password
			}
			cfg.Accounts[i].PoolType = body.PoolType
			return nil
		}
		return nil
	})
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if !found {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// handleTestAccount 用引擎客户端测试指定账号登录。
func (s *Service) handleTestAccount(w http.ResponseWriter, r *http.Request, identifier string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	store, err := s.loadEngineStore()
	if err != nil {
		responseJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "引擎未启用: " + err.Error()})
		return
	}
	var acc *engineconfig.Account
	for i := range store.Snapshot().Accounts {
		a := &store.Snapshot().Accounts[i]
		if a.Identifier() == identifier {
			acc = a
			break
		}
	}
	if acc == nil {
		responseJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	pool := engineaccount.NewPool(store)
	var dsClient *engineds.Client
	resolver := engineauth.NewResolver(store, pool, func(ctx context.Context, a engineconfig.Account) (string, error) {
		return dsClient.Login(ctx, a)
	})
	dsClient = engineds.NewClient(store, resolver)
	tok, err := dsClient.Login(ctx, *acc)
	if err != nil {
		responseJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "登录失败: " + err.Error()})
		return
	}
	_ = store.UpdateAccountToken(identifier, tok)
	responseJSON(w, map[string]interface{}{"success": true, "tokenSet": strings.TrimSpace(tok) != ""})
}
