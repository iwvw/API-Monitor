package openai

import (
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method

	// Completions proxy (intelligent routing or specific load balancer)
	if method == http.MethodPost && (path == "/v1/chat/completions" || path == "/chat/completions" || path == "/api/openai" || path == "/api/openai/v1/chat/completions" || path == "/api/openai/chat/completions") {
		s.proxyChatCompletions(w, r)
		return
	}

	// Responses proxy (OpenAI Responses API)
	if method == http.MethodPost && (path == "/v1/responses" || path == "/responses") {
		s.proxyResponses(w, r)
		return
	}

	// Anthropic Messages proxy (Anthropic Messages API)
	if method == http.MethodPost && (path == "/v1/messages" || path == "/messages") {
		s.proxyAnthropicMessages(w, r)
		return
	}

	// Models proxy
	if method == http.MethodGet && (path == "/v1/models" || path == "/models" || path == "/api/openai/v1/models" || path == "/api/openai/models") {
		s.proxyModels(w, r)
		return
	}

	// Admin CRUD prefix
	adminPath := strings.TrimPrefix(path, "/api/openai")
	adminPath = strings.Trim(adminPath, "/")
	parts := []string{}
	if adminPath != "" {
		parts = strings.Split(adminPath, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "endpoints" && method == http.MethodGet:
		s.listEndpoints(w, r)
	case len(parts) == 1 && parts[0] == "endpoints" && method == http.MethodPost:
		s.createEndpoint(w, r)
	case len(parts) == 2 && parts[0] == "endpoints" && method == http.MethodPut:
		s.updateEndpoint(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "toggle" && method == http.MethodPost:
		s.toggleEndpoint(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "endpoints" && method == http.MethodDelete:
		s.deleteEndpoint(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "verify" && method == http.MethodPost:
		s.verifyEndpoint(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "models" && method == http.MethodGet:
		s.getEndpointModels(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "endpoints" && parts[2] == "models" && parts[3] == "toggle" && method == http.MethodPost:
		s.toggleEndpointModel(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "endpoints" && parts[2] == "models" && parts[3] == "toggle-batch" && method == http.MethodPost:
		s.toggleEndpointModelsBatch(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "endpoints" && parts[2] == "models" && parts[3] == "add" && method == http.MethodPost:
		s.addEndpointModels(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "model-mappings" && method == http.MethodPut:
		s.updateModelMappings(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "routing" && method == http.MethodPut:
		s.updateEndpointRouting(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "test" && method == http.MethodPost:
		s.testEndpointChat(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "health-check" && method == http.MethodPost:
		s.healthCheckModelRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "health-check-all" && method == http.MethodPost:
		s.healthCheckAllModelsRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "key-check" && method == http.MethodPost:
		s.healthCheckKeysRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "health" && method == http.MethodGet:
		s.getEndpointHealthRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "proxy-state" && method == http.MethodGet:
		s.getEndpointProxyStateRoute(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "endpoints" && parts[2] == "proxy-state" && parts[3] == "unban" && method == http.MethodPost:
		s.unbanEndpointProxies(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "endpoints" && parts[2] == "proxy-state" && parts[3] == "probe" && method == http.MethodPost:
		s.probeEndpointProxies(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "endpoints" && (parts[1] == "refresh" || parts[1] == "refresh-all") && method == http.MethodPost:
		s.refreshAllEndpointsRoute(w, r)
	case len(parts) == 2 && parts[0] == "endpoints" && parts[1] == "reorder" && method == http.MethodPost:
		s.reorderEndpoints(w, r)
	case len(parts) == 1 && parts[0] == "health-check-all" && method == http.MethodPost:
		s.healthCheckAllRoute(w, r)
	case len(parts) == 1 && parts[0] == "keys" && method == http.MethodGet:
		s.listGatewayKeys(w, r)
	case len(parts) == 1 && parts[0] == "keys" && method == http.MethodPost:
		s.createGatewayKey(w, r)
	case len(parts) == 2 && parts[0] == "keys" && method == http.MethodPut:
		s.updateGatewayKey(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "keys" && method == http.MethodDelete:
		s.deleteGatewayKey(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "keys" && parts[2] == "toggle" && method == http.MethodPost:
		s.toggleGatewayKey(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "keys" && parts[2] == "rotate" && method == http.MethodPost:
		s.rotateGatewayKey(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "keys" && parts[2] == "default" && method == http.MethodPut:
		s.setDefaultGatewayKey(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "export" && method == http.MethodGet:
		s.exportEndpointsRoute(w, r)
	case len(parts) == 1 && parts[0] == "import" && method == http.MethodPost:
		s.importEndpointsRoute(w, r)
	case len(parts) == 2 && parts[0] == "proxies" && parts[1] == "subscription-nodes" && method == http.MethodGet:
		s.listSubscriptionSocksProxies(w, r)
	case len(parts) == 2 && parts[0] == "proxies" && parts[1] == "resolve-subscription" && method == http.MethodPost:
		s.resolveSubscriptionProxies(w, r)
	case len(parts) == 2 && parts[0] == "proxies" && parts[1] == "import-list" && method == http.MethodPost:
		s.importProxyListRoute(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "summary" && method == http.MethodGet:
		s.getAnalyticsSummary(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "charts" && method == http.MethodGet:
		s.getAnalyticsCharts(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "logs" && method == http.MethodGet:
		s.getAnalyticsLogs(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "stream" && method == http.MethodGet:
		s.analyticsEventStream(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "clear" && method == http.MethodPost:
		s.clearAnalyticsLogs(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "clear-history" && method == http.MethodPost:
		s.clearAnalyticsHistory(w, r)
	case len(parts) == 1 && parts[0] == "relay-errors" && method == http.MethodGet:
		s.handleRelayErrors(w, r)
	default:
		response.Error(w, http.StatusNotFound, "openai admin route not found")
	}
}

func (s *Service) resolveTargetEndpoint(r *http.Request) string {
	if !strings.HasPrefix(r.URL.Path, "/api/openai") {
		return ""
	}
	return r.Header.Get("x-endpoint-id")
}
