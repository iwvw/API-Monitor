package shared

import (
	"context"
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"
)

const (
	// UploadMaxSize limits total multipart request body size (16 MiB).
	// Capped for small-memory hosts (200 MB total): a 100 MiB request body
	// could single-handedly exhaust the container and be OOM-killed.
	UploadMaxSize = 16 << 20
	// GeneralMaxSize limits total JSON request body size (16 MiB).
	// Same small-memory rationale as UploadMaxSize.
	GeneralMaxSize = 16 << 20
)

type AuthResolver interface {
	Determine(req *http.Request) (*auth.RequestAuth, error)
	DetermineCaller(req *http.Request) (*auth.RequestAuth, error)
	Release(a *auth.RequestAuth)
	SetAccountMutedUntil(a *auth.RequestAuth, muteUntil float64)
	SetAccountBanned(a *auth.RequestAuth, reason string)
}

type DeepSeekCaller interface {
	CreateSession(ctx context.Context, a *auth.RequestAuth, maxAttempts int) (string, error)
	GetPow(ctx context.Context, a *auth.RequestAuth, maxAttempts int) (string, error)
	UploadFile(ctx context.Context, a *auth.RequestAuth, req dsclient.UploadFileRequest, maxAttempts int) (*dsclient.UploadFileResult, error)
	CallCompletion(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string, maxAttempts int) (*http.Response, error)
	StopStream(ctx context.Context, a *auth.RequestAuth, sessionID string, messageID int) error
	FireCompletionAndStop(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string) (int, error)
	DeleteSessionForToken(ctx context.Context, token string, sessionID string) (*dsclient.DeleteSessionResult, error)
	DeleteAllSessionsForToken(ctx context.Context, token string) error
}

type ConfigReader interface {
	ModelAliases() map[string]string
	ToolcallMode() string
	ToolcallEarlyEmitConfidence() string
	ResponsesStoreTTLSeconds() int
	EmbeddingsProvider() string
	AutoDeleteMode() string
	AutoDeleteSessions() bool
	CurrentInputFileEnabled() bool
	CurrentInputFileMinChars() int
	ThinkingInjectionEnabled() bool
	ThinkingInjectionPrompt() string
	ExpertPromptSegmentEnabled() bool
	ExpertPromptSegmentMaxChars() int
	ExpertTextFileInlineEnabled() bool
	ExpertTextFileInlineMaxFileBytes() int
	ExpertTextFileInlineAllowedExtensions() map[string]struct{}
	AutoRouteVisionEnabled() bool
}

type Deps struct {
	Store       ConfigReader
	Auth        AuthResolver
	DS          DeepSeekCaller
	ChatHistory *chathistory.Store
}

var WriteJSON = util.WriteJSON

var _ AuthResolver = (*auth.Resolver)(nil)
var _ DeepSeekCaller = (*dsclient.Client)(nil)
var _ ConfigReader = (*config.Store)(nil)
