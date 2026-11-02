package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/account"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

type ctxKey string

const authCtxKey ctxKey = "auth_context"

const toolsPresentCtxKey ctxKey = "tools_present"

var (
	ErrUnauthorized = errors.New("unauthorized: missing auth token")
	ErrNoAccount    = errors.New("no accounts configured or all accounts are busy")
)

type RequestAuth struct {
	UseConfigToken bool
	DeepSeekToken  string
	CallerID       string
	AccountID      string
	TargetAccount  string
	Account        config.Account
	TriedAccounts  map[string]bool
	// ToolsPresent 表示本次请求是否携带工具定义（请求 body 的 tools 非空）。
	// 它决定账号池调度：含工具请求走默认/仅工具池，无工具请求走无工具池。
	// 账号切换重试时沿用该标志，保证切换后仍遵守号池约束。
	ToolsPresent bool
	resolver     *Resolver
}

type LoginFunc func(ctx context.Context, acc config.Account) (string, error)
type PostLoginFunc func(ctx context.Context, a *RequestAuth)

type Resolver struct {
	Store     *config.Store
	Pool      *account.Pool
	Login     LoginFunc
	PostLogin PostLoginFunc

	mu               sync.Mutex
	tokenRefreshedAt map[string]time.Time
	callCounts       map[string]int64

	// callObserver 每次账号被选中调度服务请求时回调（插件侧持久化计数用，可为 nil）。
	callObserver func(string)
}

func NewResolver(store *config.Store, pool *account.Pool, login LoginFunc) *Resolver {
	return &Resolver{
		Store:            store,
		Pool:             pool,
		Login:            login,
		tokenRefreshedAt: map[string]time.Time{},
		callCounts:       map[string]int64{},
	}
}

// IncrementCall 记录一次账号被选中调度服务请求（成功绑定账号后调用）。
func (r *Resolver) IncrementCall(accountID string) {
	if accountID == "" {
		return
	}
	r.mu.Lock()
	if r.callCounts == nil {
		r.callCounts = map[string]int64{}
	}
	r.callCounts[accountID]++
	r.mu.Unlock()
	if r.callObserver != nil {
		r.callObserver(accountID)
	}
}

// SetCallObserver 注入账号调用回调（引擎外插件用于持久化累计）。
// 回调在锁外同步调用，内部自行保证并发安全。
func (r *Resolver) SetCallObserver(fn func(string)) {
	r.mu.Lock()
	r.callObserver = fn
	r.mu.Unlock()
}

// CallCount 返回账号自引擎启动以来被选中调度服务的累计次数。
func (r *Resolver) CallCount(accountID string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.callCounts[accountID]
}

func (r *Resolver) Determine(req *http.Request) (*RequestAuth, error) {
	callerKey := extractCallerToken(req)
	if callerKey == "" {
		return nil, ErrUnauthorized
	}
	callerID := callerTokenID(callerKey)
	ctx := req.Context()
	if !r.Store.HasAPIKey(callerKey) {
		return &RequestAuth{
			UseConfigToken: false,
			DeepSeekToken:  callerKey,
			CallerID:       callerID,
			resolver:       r,
			TriedAccounts:  map[string]bool{},
		}, nil
	}
	target := strings.TrimSpace(req.Header.Get("X-Ds2-Target-Account"))
	// 号池选择按“本次请求 body 是否携带工具定义”路由。含工具请求走默认/仅
	// 工具池，无工具请求走无工具池；该标志由各协议适配器在标准化前写入请求
	// 上下文（见 WithToolsPresent）。
	toolsPresent := toolsPresentFromContext(ctx)
	a, err := r.acquireManagedRequestAuth(ctx, callerID, target, toolsPresent)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *Resolver) acquireManagedRequestAuth(ctx context.Context, callerID, target string, toolsPresent bool) (*RequestAuth, error) {
	tried := map[string]bool{}
	var lastEnsureErr error
	filter := account.AccountFilter(func(acc config.Account) bool {
		return acc.MatchesPoolType(toolsPresent)
	})
	for {
		if target == "" && len(tried) >= len(r.Store.Accounts()) {
			if lastEnsureErr != nil {
				return nil, lastEnsureErr
			}
			return nil, ErrNoAccount
		}
		acc, ok := r.Pool.AcquireWait(ctx, target, tried, filter)
		if !ok {
			if lastEnsureErr != nil {
				return nil, lastEnsureErr
			}
			return nil, ErrNoAccount
		}

		a := &RequestAuth{
			UseConfigToken: true,
			CallerID:       callerID,
			AccountID:      acc.Identifier(),
			TargetAccount:  target,
			Account:        acc,
			TriedAccounts:  tried,
			ToolsPresent:   toolsPresent,
			resolver:       r,
		}

		if err := r.ensureManagedToken(ctx, a); err != nil {
			lastEnsureErr = err
			tried[a.AccountID] = true
			r.Pool.Release(a.AccountID)
			if target != "" {
				return nil, err
			}
			continue
		}
		r.IncrementCall(a.AccountID)
		return a, nil
	}
}

// DetermineCaller resolves caller identity without acquiring any pooled account.
// Use this for local-cache lookup routes that only need tenant isolation.
func (r *Resolver) DetermineCaller(req *http.Request) (*RequestAuth, error) {
	callerKey := extractCallerToken(req)
	if callerKey == "" {
		return nil, ErrUnauthorized
	}
	callerID := callerTokenID(callerKey)
	a := &RequestAuth{
		UseConfigToken: false,
		CallerID:       callerID,
		resolver:       r,
		TriedAccounts:  map[string]bool{},
	}
	if r == nil || r.Store == nil || !r.Store.HasAPIKey(callerKey) {
		a.DeepSeekToken = callerKey
	}
	return a, nil
}

func WithAuth(ctx context.Context, a *RequestAuth) context.Context {
	return context.WithValue(ctx, authCtxKey, a)
}

func FromContext(ctx context.Context) (*RequestAuth, bool) {
	v := ctx.Value(authCtxKey)
	a, ok := v.(*RequestAuth)
	return a, ok
}

// WithToolsPresent records whether the current request body carries a non-empty
// tool definition. Protocol adapters set this (after normalizing their own
// request shape onto the shared `tools` field) so that Determine can route the
// request to the matching account pool without re-parsing the body. It also
// governs whether tool-call prompt injection happens for the request.
func WithToolsPresent(ctx context.Context, present bool) context.Context {
	return context.WithValue(ctx, toolsPresentCtxKey, present)
}

// toolsPresentFromContext returns the tools-present flag stored by
// WithToolsPresent. When unset (e.g. routes that do not inspect the body such
// as embeddings or file uploads) it defaults to false, routing to the
// no-tools/default pool.
func toolsPresentFromContext(ctx context.Context) bool {
	v := ctx.Value(toolsPresentCtxKey)
	b, _ := v.(bool)
	return b
}

func (r *Resolver) loginAndPersist(ctx context.Context, a *RequestAuth) error {
	token, err := r.Login(ctx, a.Account)
	if err != nil {
		return err
	}
	a.Account.Token = token
	a.DeepSeekToken = token
	r.markTokenRefreshedNow(a.AccountID)
	if err := r.Store.UpdateAccountToken(a.AccountID, token); err != nil {
		return err
	}
	if r.PostLogin != nil {
		r.PostLogin(ctx, a)
	}
	return nil
}

func (r *Resolver) RefreshToken(ctx context.Context, a *RequestAuth) bool {
	if !a.UseConfigToken || a.AccountID == "" {
		return false
	}
	_ = r.Store.UpdateAccountToken(a.AccountID, "")
	a.Account.Token = ""
	if err := r.loginAndPersist(ctx, a); err != nil {
		config.Logger.Error("[refresh_token] failed", "account", a.AccountID, "error", err)
		return false
	}
	return true
}

func (r *Resolver) MarkTokenInvalid(a *RequestAuth) {
	if !a.UseConfigToken || a.AccountID == "" {
		return
	}
	a.Account.Token = ""
	a.DeepSeekToken = ""
	r.clearTokenRefreshMark(a.AccountID)
	_ = r.Store.UpdateAccountToken(a.AccountID, "")
}

// SetAccountMutedUntil 持久化账号禁言到期时间。
// 与 DisableAccount 不同，这只是临时禁用，到期后号池会自动恢复调度。
// 当弹性号池开启时，封号后立即在同一个事务内触发 ReconcileElasticPool
// 补位：被封账号让出名额，按原始顺序从后面的休眠账号中启用一个补上。
func (r *Resolver) SetAccountMutedUntil(a *RequestAuth, muteUntil float64) {
	if !a.UseConfigToken || a.AccountID == "" || muteUntil <= 0 {
		return
	}
	identifier := a.AccountID
	if err := r.Store.Update(func(c *config.Config) error {
		for i := range c.Accounts {
			if c.Accounts[i].Identifier() != identifier {
				continue
			}
			c.Accounts[i].MutedUntil = muteUntil
			break
		}
		if c.ElasticPool.Enabled {
			account.ReconcileElasticPool(c)
		}
		return nil
	}); err != nil {
		config.Logger.Error("[muted_account] failed to persist muted_until", "account", identifier, "error", err)
		return
	}
	a.Account.MutedUntil = muteUntil
	if r.Pool != nil {
		r.Pool.Reset()
	}
	config.Logger.Info("[muted_account] account muted until", "account", identifier, "mute_until", muteUntil)
}

// SetAccountBanned 将账号标记为被上游停用（USER_IS_BANNED）。
// 手动启用或启用全部账号不会解除该状态；只有成功刷新 token 后才会在 Login 中清除。
func (r *Resolver) SetAccountBanned(a *RequestAuth, reason string) {
	if !a.UseConfigToken || a.AccountID == "" {
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "账户已被停用"
	}
	identifier := a.AccountID
	if err := r.Store.Update(func(c *config.Config) error {
		for i := range c.Accounts {
			if c.Accounts[i].Identifier() != identifier {
				continue
			}
			c.Accounts[i].Banned = true
			c.Accounts[i].Disabled = true
			c.Accounts[i].DisabledReason = reason
			break
		}
		if c.ElasticPool.Enabled {
			account.ReconcileElasticPool(c)
		}
		return nil
	}); err != nil {
		config.Logger.Error("[banned_account] failed to persist banned state", "account", identifier, "error", err)
		return
	}
	a.Account.Banned = true
	a.Account.Disabled = true
	a.Account.DisabledReason = reason
	if r.Pool != nil {
		r.Pool.Reset()
	}
	config.Logger.Warn("[banned_account] account disabled after USER_IS_BANNED", "account", identifier, "reason", reason)
}

func (r *Resolver) SwitchAccount(ctx context.Context, a *RequestAuth) bool {
	if !a.UseConfigToken {
		return false
	}
	if strings.TrimSpace(a.TargetAccount) != "" {
		return false
	}
	if a.TriedAccounts == nil {
		a.TriedAccounts = map[string]bool{}
	}
	if a.AccountID != "" {
		a.TriedAccounts[a.AccountID] = true
		r.Pool.Release(a.AccountID)
	}
	filter := account.AccountFilter(func(acc config.Account) bool {
		return acc.MatchesPoolType(a.ToolsPresent)
	})
	for {
		acc, ok := r.Pool.Acquire("", a.TriedAccounts, filter)
		if !ok {
			return false
		}
		a.Account = acc
		a.AccountID = acc.Identifier()
		if err := r.ensureManagedToken(ctx, a); err != nil {
			a.TriedAccounts[a.AccountID] = true
			r.Pool.Release(a.AccountID)
			continue
		}
		r.IncrementCall(a.AccountID)
		return true
	}
}

func (a *RequestAuth) SwitchAccount(ctx context.Context) bool {
	if a == nil || a.resolver == nil {
		return false
	}
	return a.resolver.SwitchAccount(ctx, a)
}

func (r *Resolver) Release(a *RequestAuth) {
	if a == nil || !a.UseConfigToken || a.AccountID == "" {
		return
	}
	r.Pool.Release(a.AccountID)
}

func extractCallerToken(req *http.Request) string {
	authHeader := strings.TrimSpace(req.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token := strings.TrimSpace(authHeader[7:])
		if token != "" {
			return token
		}
	}
	if key := strings.TrimSpace(req.Header.Get("x-api-key")); key != "" {
		return key
	}
	// Gemini/Google clients commonly send API key via x-goog-api-key.
	if key := strings.TrimSpace(req.Header.Get("x-goog-api-key")); key != "" {
		return key
	}
	// Gemini AI Studio compatibility: allow query key fallback only when no
	// header-based credential is present.
	if key := strings.TrimSpace(req.URL.Query().Get("key")); key != "" {
		return key
	}
	return strings.TrimSpace(req.URL.Query().Get("api_key"))
}

func callerTokenID(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "caller:" + hex.EncodeToString(sum[:8])
}

func (r *Resolver) ensureManagedToken(ctx context.Context, a *RequestAuth) error {
	if strings.TrimSpace(a.Account.Token) == "" {
		return r.loginAndPersist(ctx, a)
	}
	if r.shouldForceRefresh(a.AccountID) {
		if err := r.loginAndPersist(ctx, a); err != nil {
			return err
		}
		return nil
	}
	a.DeepSeekToken = a.Account.Token
	return nil
}

func (r *Resolver) shouldForceRefresh(accountID string) bool {
	if r == nil || r.Store == nil {
		return false
	}
	if strings.TrimSpace(accountID) == "" {
		return false
	}
	intervalHours := r.Store.RuntimeTokenRefreshIntervalHours()
	if intervalHours <= 0 {
		return false
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	last, ok := r.tokenRefreshedAt[accountID]
	if !ok || last.IsZero() {
		r.tokenRefreshedAt[accountID] = now
		return false
	}
	return now.Sub(last) >= time.Duration(intervalHours)*time.Hour
}

func (r *Resolver) markTokenRefreshedNow(accountID string) {
	if strings.TrimSpace(accountID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokenRefreshedAt[accountID] = time.Now()
}

func (r *Resolver) clearTokenRefreshMark(accountID string) {
	if strings.TrimSpace(accountID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tokenRefreshedAt, accountID)
}
