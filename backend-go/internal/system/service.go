package system

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/apikeys"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type Notifier interface {
	Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error
}

type alertState struct {
	cpuHigh    bool
	memoryHigh bool
	diskHigh   bool
}

type Service struct {
	cfg       config.Config
	startedAt time.Time
	store     *database.Store

	mu         sync.Mutex
	statsCache map[string]*APICounters
	stopChan   chan struct{}
	wg         sync.WaitGroup
	notifier   Notifier
	aiCaller   AICaller
	apiKeys    *apikeys.Manager
	alertState alertState
}

type APICounters struct {
	Audit int64 `json:"audit"`
	Ops   int64 `json:"ops"`
}

func (s *Service) SetNotifier(n Notifier) {
	s.notifier = n
}

func (s *Service) SetAICaller(caller AICaller) {
	s.aiCaller = caller
}

func New(cfg config.Config) *Service {
	s := &Service{
		cfg:        cfg,
		startedAt:  time.Now(),
		store:      database.New(cfg),
		apiKeys:    apikeys.New(cfg),
		statsCache: make(map[string]*APICounters),
		stopChan:   make(chan struct{}),
	}

	s.wg.Add(1)
	go s.runFlushLoop()

	s.wg.Add(1)
	go s.runHostMonitorLoop()

	return s
}

func (s *Service) runFlushLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.flushToDB()
		case <-s.stopChan:
			s.flushToDB()
			return
		}
	}
}

func (s *Service) runHostMonitorLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkHostAlerts()
		case <-s.stopChan:
			return
		}
	}
}

func (s *Service) checkHostAlerts() {
	if s.notifier == nil {
		return
	}
	metrics, err := s.hostMetrics()
	if err != nil {
		return
	}

	cpuInfo, _ := metrics["cpu"].(map[string]interface{})
	cpuVal, _ := cpuInfo["usage"].(float64)

	memInfo, _ := metrics["memory"].(map[string]interface{})
	memVal, _ := memInfo["usage"].(float64)

	diskInfo, _ := metrics["disk"].(map[string]interface{})
	diskVal, _ := diskInfo["usage"].(float64)

	hostname, _ := metrics["hostname"].(string)
	if hostname == "" {
		hostname = "local-host"
	}

	eventData := map[string]interface{}{
		"serverId":    "local-host",
		"serverName":  hostname,
		"host":        hostname,
		"hostname":    hostname,
		"cpu_usage":   cpuVal,
		"mem_percent": memVal,
		"disk_usage":  diskVal,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s.mu.Lock()
	defer s.mu.Unlock()

	// CPU
	if cpuVal >= 90 {
		if !s.alertState.cpuHigh {
			s.alertState.cpuHigh = true
			eventData["eventType"] = "cpu_high"
			_ = s.notifier.Trigger(ctx, "system", "cpu_high", eventData)
		}
	} else if cpuVal < 85 {
		if s.alertState.cpuHigh {
			s.alertState.cpuHigh = false
			eventData["eventType"] = "cpu_normal"
			_ = s.notifier.Trigger(ctx, "system", "cpu_normal", eventData)
		}
	}

	// Memory
	if memVal >= 90 {
		if !s.alertState.memoryHigh {
			s.alertState.memoryHigh = true
			eventData["eventType"] = "memory_high"
			_ = s.notifier.Trigger(ctx, "system", "memory_high", eventData)
		}
	} else if memVal < 85 {
		if s.alertState.memoryHigh {
			s.alertState.memoryHigh = false
			eventData["eventType"] = "memory_normal"
			_ = s.notifier.Trigger(ctx, "system", "memory_normal", eventData)
		}
	}

	// Disk
	if diskVal >= 90 {
		if !s.alertState.diskHigh {
			s.alertState.diskHigh = true
			eventData["eventType"] = "disk_high"
			_ = s.notifier.Trigger(ctx, "system", "disk_high", eventData)
		}
	} else if diskVal < 85 {
		if s.alertState.diskHigh {
			s.alertState.diskHigh = false
			eventData["eventType"] = "disk_normal"
			_ = s.notifier.Trigger(ctx, "system", "disk_normal", eventData)
		}
	}
}

func (s *Service) flushToDB() {
	s.mu.Lock()
	if len(s.statsCache) == 0 {
		s.mu.Unlock()
		return
	}

	// Copy counters to release the lock quickly
	statsToSave := make(map[string]*APICounters)
	for k, v := range s.statsCache {
		statsToSave[k] = &APICounters{Audit: v.Audit, Ops: v.Ops}
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := s.store.Open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO system_api_stats (date, audit_count, ops_count)
		VALUES (?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			audit_count = audit_count + excluded.audit_count,
			ops_count = ops_count + excluded.ops_count,
			updated_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return
	}
	defer stmt.Close()

	for date, counters := range statsToSave {
		if _, err := stmt.ExecContext(ctx, date, counters.Audit, counters.Ops); err != nil {
			return
		}
	}

	if err := tx.Commit(); err != nil {
		return
	}

	// Subtract the successfully written values
	s.mu.Lock()
	for date, saved := range statsToSave {
		if current, exists := s.statsCache[date]; exists {
			current.Audit -= saved.Audit
			current.Ops -= saved.Ops
			if current.Audit <= 0 && current.Ops <= 0 {
				delete(s.statsCache, date)
			}
		}
	}
	s.mu.Unlock()
}

func (s *Service) RecordAPICall(method string, path string) {
	// Filter high-frequency heartbeat and stats queries
	if path == "/api/system/host-metrics" || path == "/api/system/api-stats" || path == "/health" {
		return
	}

	date := time.Now().Format("2006-01-02")

	s.mu.Lock()
	defer s.mu.Unlock()

	counters, exists := s.statsCache[date]
	if !exists {
		counters = &APICounters{}
		s.statsCache[date] = counters
	}

	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		counters.Audit++
	} else {
		counters.Ops++
	}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/openapi.json" {
		clone := r.Clone(r.Context())
		nextURL := *r.URL
		nextURL.Path = "/api/system/openapi.json"
		nextURL.RawPath = ""
		clone.URL = &nextURL
		r = clone
	}
	if strings.HasPrefix(r.URL.Path, "/api/ai-access") {
		clone := r.Clone(r.Context())
		nextURL := *r.URL
		nextURL.Path = "/api/system/ai-access" + strings.TrimPrefix(r.URL.Path, "/api/ai-access")
		nextURL.RawPath = ""
		clone.URL = &nextURL
		r = clone
	}
	if strings.HasPrefix(r.URL.Path, "/api/api-keys") {
		clone := r.Clone(r.Context())
		nextURL := *r.URL
		nextURL.Path = "/api/system/api-keys" + strings.TrimPrefix(r.URL.Path, "/api/api-keys")
		nextURL.RawPath = ""
		clone.URL = &nextURL
		r = clone
	}

	switch r.URL.Path {
	case "/api/system/host-metrics":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, err := s.hostMetrics()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/system/api-stats":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, err := s.apiStats()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/system/api-docs":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		response.OK(w, s.apiDocs())
	case "/api/system/openapi.json":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		response.JSON(w, http.StatusOK, s.openapiDocument(r))
	case "/api/system/api-keys":
		s.handleAPIKeyCollection(w, r)
	case "/api/system/ai-access":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, err := s.aiAccessOverview(r)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/system/ai-access/write":
		if r.Method != http.MethodPut {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, err := s.setAIAgentWriteEnabled(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/system/ai-access/key/rotate":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, err := s.rotateAIAgentKey(r)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/system/ai-access/mcp-servers":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, err := s.saveMCPServer(r, "")
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/system/ai-access/skills":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, err := s.saveSkill(r, "")
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/system/ai-access/audit":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, err := s.listAIAuditPage(r)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/system/ai-access/audit/clear":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, err := s.clearAIAudit(r)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/ai/manifest":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, err := s.aiManifest(r)
		if errors.Is(err, errUnauthorizedAI) {
			response.Error(w, http.StatusUnauthorized, "AI 接入密钥无效")
			return
		}
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/ai/mcp":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		payload, status, err := s.handleMCP(r)
		if errors.Is(err, errUnauthorizedAI) {
			response.Error(w, http.StatusUnauthorized, "AI 接入密钥无效")
			return
		}
		if err != nil {
			response.Error(w, status, err.Error())
			return
		}
		if payload == nil && status == http.StatusAccepted {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		response.JSON(w, status, payload)
	default:
		if strings.HasPrefix(r.URL.Path, "/api/system/api-keys/") {
			s.handleAPIKeyItem(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/system/ai-access/mcp-servers/") {
			id := strings.TrimPrefix(r.URL.Path, "/api/system/ai-access/mcp-servers/")
			if r.Method == http.MethodDelete {
				payload, err := s.deleteMCPServer(r, id)
				if err != nil {
					response.Error(w, http.StatusInternalServerError, err.Error())
					return
				}
				response.OK(w, payload)
				return
			}
			if r.Method == http.MethodPut {
				payload, err := s.saveMCPServer(r, id)
				if err != nil {
					response.Error(w, http.StatusBadRequest, err.Error())
					return
				}
				response.OK(w, payload)
				return
			}
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/system/ai-access/skills/") {
			id := strings.TrimPrefix(r.URL.Path, "/api/system/ai-access/skills/")
			if r.Method == http.MethodDelete {
				payload, err := s.deleteSkill(r, id)
				if err != nil {
					response.Error(w, http.StatusInternalServerError, err.Error())
					return
				}
				response.OK(w, payload)
				return
			}
			if r.Method == http.MethodPut {
				payload, err := s.saveSkill(r, id)
				if err != nil {
					response.Error(w, http.StatusBadRequest, err.Error())
					return
				}
				response.OK(w, payload)
				return
			}
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		response.Error(w, http.StatusNotFound, "system route not implemented")
	}
}

type apiDocRoute struct {
	Prefix        string                 `json:"prefix"`
	Module        string                 `json:"module"`
	Group         string                 `json:"group"`
	Owner         manifest.Owner         `json:"owner"`
	Auth          manifest.AuthMode      `json:"auth"`
	ResponseMode  manifest.ResponseMode  `json:"responseMode"`
	Description   string                 `json:"description"`
	Detail        string                 `json:"detail,omitempty"`
	MatchMode     manifest.MatchMode     `json:"matchMode"`
	Methods       []string               `json:"methods"`
	Status        string                 `json:"status"`
	PathParams    []apiDocParameter      `json:"pathParams,omitempty"`
	QueryParams   []apiDocParameter      `json:"queryParams,omitempty"`
	Headers       []apiDocParameter      `json:"headers,omitempty"`
	RequestType   string                 `json:"requestContentType,omitempty"`
	RequestBody   interface{}            `json:"requestExample,omitempty"`
	RequestSchema map[string]interface{} `json:"requestSchema,omitempty"`
	ResponseBody  interface{}            `json:"responseExample,omitempty"`
	Notes         []string               `json:"notes,omitempty"`
}

func (s *Service) apiDocs() map[string]interface{} {
	routes := append([]manifest.Route{}, manifest.Routes()...)
	routes = append(routes, supplementalRoutes()...)
	seen := map[string]bool{}
	items := make([]apiDocRoute, 0, len(routes))
	for _, route := range routes {
		if seen[route.Prefix] {
			continue
		}
		seen[route.Prefix] = true
		matchMode := route.MatchMode
		if matchMode == "" {
			matchMode = manifest.MatchPrefix
		}
		description := routeDescription(route)
		docs := routeDocs(route)
		if desc, ok := routeDescriptions[route.Prefix]; ok {
			docs.Detail = desc
		}
		requestSchema := map[string]interface{}(nil)
		requestBody := docs.RequestExample
		if schema, contractExample, ok := requestContractFor(route.Prefix); ok {
			requestSchema = schema
			if requestBody == nil {
				requestBody = contractExample
			}
		}
		items = append(items, apiDocRoute{
			Prefix:        route.Prefix,
			Module:        route.Module,
			Group:         routeGroup(route),
			Owner:         route.Owner,
			Auth:          route.Auth,
			ResponseMode:  route.ResponseMode,
			Description:   description,
			Detail:        docs.Detail,
			MatchMode:     matchMode,
			Methods:       docs.Methods,
			Status:        routeStatus(route),
			PathParams:    docs.PathParams,
			QueryParams:   docs.QueryParams,
			Headers:       docs.Headers,
			RequestType:   docs.RequestContentType,
			RequestBody:   requestBody,
			RequestSchema: requestSchema,
			ResponseBody:  docs.ResponseExample,
			Notes:         docs.Notes,
		})
	}

	// 补充：把已登记请求契约但尚未出现在清单中的具体子路由加入文档，
	// 保证 get_route 能命中具体路由并返回契约，而不是只命中家族前缀聚合路由。
	for prefix := range routeRequestContracts {
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		route := manifest.Route{
			Prefix:       prefix,
			Module:       moduleFromPrefix(prefix),
			Owner:        manifest.OwnerGo,
			Auth:         manifest.AuthSession,
			ResponseMode: manifest.ResponseJSON,
			MatchMode:    manifest.MatchPattern,
		}
		docs := routeDocs(route)
		if desc, ok := routeDescriptions[route.Prefix]; ok {
			docs.Detail = desc
		}
		requestSchema := map[string]interface{}(nil)
		requestBody := docs.RequestExample
		if schema, contractExample, ok := requestContractFor(prefix); ok {
			requestSchema = schema
			if requestBody == nil {
				requestBody = contractExample
			}
		}
		items = append(items, apiDocRoute{
			Prefix:        route.Prefix,
			Module:        route.Module,
			Group:         routeGroup(route),
			Owner:         route.Owner,
			Auth:          route.Auth,
			ResponseMode:  route.ResponseMode,
			Description:   routeDescription(route),
			Detail:        docs.Detail,
			MatchMode:     route.MatchMode,
			Methods:       docs.Methods,
			Status:        routeStatus(route),
			PathParams:    docs.PathParams,
			QueryParams:   docs.QueryParams,
			Headers:       docs.Headers,
			RequestType:   docs.RequestContentType,
			RequestBody:   requestBody,
			RequestSchema: requestSchema,
			ResponseBody:  docs.ResponseExample,
			Notes:         docs.Notes,
		})
	}

	return map[string]interface{}{
		"version":     s.cfg.Version,
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"summary": map[string]interface{}{
			"total":        len(items),
			"byOwner":      countRoutesBy(items, func(route apiDocRoute) string { return string(route.Owner) }),
			"byAuth":       countRoutesBy(items, func(route apiDocRoute) string { return string(route.Auth) }),
			"byGroup":      countRoutesBy(items, func(route apiDocRoute) string { return route.Group }),
			"byStatus":     countRoutesBy(items, func(route apiDocRoute) string { return route.Status }),
			"byResponse":   countRoutesBy(items, func(route apiDocRoute) string { return string(route.ResponseMode) }),
			"openapiRoute": "/api/openapi.json",
		},
		"routes": items,
		"aiAccess": map[string]interface{}{
			"currentGateway": "/api/openai",
			"compatibleAPI":  "/v1",
			"plannedModules": []map[string]string{
				{"id": "providers", "name": "模型端点", "description": "统一管理 OpenAI 兼容端点、模型发现、健康检测与负载均衡"},
				{"id": "mcp", "name": "MCP 服务", "description": "管理 MCP 服务、工具发现、资源、提示词与调用权限"},
				{"id": "skills", "name": "Skill 管理", "description": "管理本地 Skill、版本、入口、依赖与启用状态"},
				{"id": "permissions", "name": "工具权限", "description": "统一约束模型、MCP、Skill 和内部系统动作的调用边界"},
				{"id": "audit", "name": "调用审计", "description": "记录模型请求、工具调用、Skill 执行、耗时和失败原因"},
			},
		},
	}
}

func (s *Service) openapiDocument(r *http.Request) map[string]interface{} {
	paths := map[string]interface{}{}
	for _, route := range s.apiDocs()["routes"].([]apiDocRoute) {
		methods := route.Methods
		if len(methods) == 0 {
			methods = []string{"GET"}
		}
		operations := map[string]interface{}{}
		for _, method := range methods {
			operation := map[string]interface{}{
				"tags":        []string{route.Group},
				"summary":     route.Description,
				"description": buildOpenAPIDescription(route),
				"deprecated":  route.Owner == manifest.OwnerRetired,
				"responses": map[string]interface{}{
					"200": map[string]interface{}{"description": "请求成功"},
					"401": map[string]interface{}{"description": "未授权"},
					"404": map[string]interface{}{"description": "接口不存在"},
				},
			}
			if len(route.PathParams) > 0 || len(route.QueryParams) > 0 || len(route.Headers) > 0 {
				operation["parameters"] = openAPIParameters(route)
			}
			if security := openAPISecurity(route.Auth); len(security) > 0 {
				operation["security"] = security
			}
			if route.RequestBody != nil {
				contentType := route.RequestType
				if strings.TrimSpace(contentType) == "" {
					contentType = "application/json"
				}
				operation["requestBody"] = map[string]interface{}{
					"required": method != http.MethodGet && method != http.MethodDelete,
					"content": map[string]interface{}{
						contentType: map[string]interface{}{
							"example": route.RequestBody,
						},
					},
				}
			}
			if route.ResponseBody != nil {
				responses := operation["responses"].(map[string]interface{})
				responses["200"] = map[string]interface{}{
					"description": "请求成功",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"example": route.ResponseBody,
						},
					},
				}
			}
			operations[strings.ToLower(method)] = operation
		}
		paths[route.Prefix] = operations
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	serverURL := fmt.Sprintf("%s://%s", scheme, r.Host)
	if r.Host == "" {
		serverURL = "/"
	}

	return map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]interface{}{
			"title":       "API Monitor API",
			"version":     s.cfg.Version,
			"description": "由系统路由清单自动生成的接口索引。请求/响应 Schema 会在后续元数据补齐后逐步增强。",
		},
		"servers": []map[string]string{{"url": serverURL}},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"sessionCookie": map[string]interface{}{
					"type": "apiKey",
					"in":   "cookie",
					"name": "sid",
				},
				"bearerAuth": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "API Key",
				},
				"agentBearer": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "Agent Key",
				},
			},
		},
		"paths": paths,
	}
}

func buildOpenAPIDescription(route apiDocRoute) string {
	parts := []string{
		route.Detail,
		fmt.Sprintf("模块: %s；认证: %s；响应: %s；匹配: %s", route.Module, route.Auth, route.ResponseMode, route.MatchMode),
	}
	if len(route.Notes) > 0 {
		parts = append(parts, "备注: "+strings.Join(route.Notes, "；"))
	}
	return strings.Join(filterNonEmpty(parts), "\n\n")
}

func openAPIParameters(route apiDocRoute) []map[string]interface{} {
	params := make([]map[string]interface{}, 0, len(route.PathParams)+len(route.QueryParams)+len(route.Headers))
	appendParam := func(param apiDocParameter) {
		params = append(params, map[string]interface{}{
			"name":        param.Name,
			"in":          param.In,
			"required":    param.Required,
			"description": param.Description,
			"schema": map[string]interface{}{
				"type":    "string",
				"example": param.Example,
			},
		})
	}
	for _, param := range route.PathParams {
		appendParam(param)
	}
	for _, param := range route.QueryParams {
		appendParam(param)
	}
	for _, param := range route.Headers {
		appendParam(param)
	}
	return params
}

func openAPISecurity(mode manifest.AuthMode) []map[string][]string {
	switch mode {
	case manifest.AuthSession:
		return []map[string][]string{{"sessionCookie": {}}}
	case manifest.AuthAPIKey:
		return []map[string][]string{{"bearerAuth": {}}}
	case manifest.AuthAgent:
		return []map[string][]string{{"agentBearer": {}}}
	default:
		return nil
	}
}

func filterNonEmpty(parts []string) []string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return filtered
}

func countRoutesBy(routes []apiDocRoute, keyFn func(apiDocRoute) string) map[string]int {
	counts := map[string]int{}
	for _, route := range routes {
		counts[keyFn(route)]++
	}
	return counts
}

func moduleFromPrefix(prefix string) string {
	trimmed := strings.Trim(prefix, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) >= 3 && parts[0] == "api" {
		return parts[1]
	}
	if len(parts) >= 2 {
		return parts[0]
	}
	return "api"
}

func routeGroup(route manifest.Route) string {
	prefix := route.Prefix
	switch {
	// 模型网关
	case strings.HasPrefix(prefix, "/api/openai"), strings.HasPrefix(prefix, "/api/chat"), strings.HasPrefix(prefix, "/v1"):
		return "模型网关"
	// 订阅分发
	case strings.HasPrefix(prefix, "/api/subscription"), strings.HasPrefix(prefix, "/sub"):
		return "订阅分发"
	// Cloudflare
	case strings.HasPrefix(prefix, "/api/cloudflare"):
		return "Cloudflare"
	// 阿里云
	case strings.HasPrefix(prefix, "/api/aliyun"):
		return "阿里云"
	// 腾讯云
	case strings.HasPrefix(prefix, "/api/tencent"):
		return "腾讯云"
	// 甲骨文云
	case strings.HasPrefix(prefix, "/api/oracle"):
		return "甲骨文云"
	// Microsoft 365
	case strings.HasPrefix(prefix, "/api/m365"):
		return "Microsoft 365"
	// GitHub
	case strings.HasPrefix(prefix, "/api/github"):
		return "GitHub"
	// 主机实例
	case strings.HasPrefix(prefix, "/api/server"), strings.HasPrefix(prefix, "/api/onepanel"), strings.HasPrefix(prefix, "/ws/ssh"), strings.HasPrefix(prefix, "/ws/agent-terminal"), strings.HasPrefix(prefix, "/socket.io"):
		return "主机实例"
	// PaaS
	case strings.HasPrefix(prefix, "/api/koyeb"), strings.HasPrefix(prefix, "/api/flyio"):
		return "PaaS"
	// 定时任务
	case strings.HasPrefix(prefix, "/api/scheduler"), strings.HasPrefix(prefix, "/api/cron"):
		return "定时任务"
	// 可用性监测
	case strings.HasPrefix(prefix, "/api/uptime"):
		return "可用性监测"
	// 文件柜
	case strings.HasPrefix(prefix, "/api/filebox"):
		return "文件柜"
	// 图编辑器
	case strings.HasPrefix(prefix, "/api/drawio"):
		return "图编辑器"
	// 提示词库
	case strings.HasPrefix(prefix, "/api/prompts"):
		return "提示词库"
	// 双因子认证
	case strings.HasPrefix(prefix, "/api/totp"):
		return "双因子认证"
	// 通知中心
	case strings.HasPrefix(prefix, "/api/notification"):
		return "通知中心"
	// 认证
	case strings.HasPrefix(prefix, "/api/auth"):
		return "认证"
	// 系统日志
	case strings.HasPrefix(prefix, "/api/system/logs"), strings.HasPrefix(prefix, "/api/logs"), strings.HasPrefix(prefix, "/ws/logs"),
		strings.HasPrefix(prefix, "/api/settings/sys-logs"), strings.HasPrefix(prefix, "/api/settings/app-log-file"),
		strings.HasPrefix(prefix, "/api/settings/log-settings"), strings.HasPrefix(prefix, "/api/settings/enforce-log-limits"),
		strings.HasPrefix(prefix, "/api/settings/clear-logs"), strings.HasPrefix(prefix, "/api/settings/clear-app-logs"):
		return "系统日志"
	// API 接口（文档、密钥、AI 接入）
	case prefix == "/api/system/api-docs", prefix == "/api/system/openapi.json", prefix == "/api/openapi.json",
		strings.HasPrefix(prefix, "/api/api-keys"), strings.HasPrefix(prefix, "/api/system/api-keys"),
		strings.HasPrefix(prefix, "/api/ai-access"), strings.HasPrefix(prefix, "/api/system/ai-access"),
		prefix == "/api/ai/manifest", prefix == "/api/ai/mcp":
		return "API 接口"
	// 系统设置（含备份）
	case strings.HasPrefix(prefix, "/api/settings"), strings.HasPrefix(prefix, "/api/backup"):
		return "系统设置"
	// 仪表盘 / 系统内核
	case prefix == "/health", prefix == "/api/migration/status",
		prefix == "/api/system/host-metrics", prefix == "/api/system/api-stats":
		return "仪表盘"
	// 其余系统级接口
	case strings.HasPrefix(prefix, "/api/system"):
		return "系统"
	default:
		return "基础"
	}
}

func routeDescription(route manifest.Route) string {
	if desc, ok := routeDescriptions[route.Prefix]; ok {
		return desc
	}
	prefix := route.Prefix
	switch {
	case prefix == "/health":
		return "服务健康检查与版本状态"
	case prefix == "/api/migration/status":
		return "读取迁移状态、路由归属和废弃模块信息"
	case prefix == "/api/system/api-docs":
		return "读取系统自动生成的 API 文档清单"
	case prefix == "/api/system/openapi.json":
		return "导出 OpenAPI 3.1 接口文档"
	case prefix == "/api/openapi.json":
		return "导出 OpenAPI 3.1 接口文档"
	case prefix == "/api/ai-access":
		return "读取 AI 接入、Agent Key 和审计概览"
	case prefix == "/api/ai-access/key/rotate":
		return "轮换 AI Agent Key"
	case strings.HasPrefix(prefix, "/api/ai-access/mcp-servers"):
		return "管理 AI 接入的 MCP 服务配置"
	case strings.HasPrefix(prefix, "/api/ai-access/skills"):
		return "管理 AI 接入的 Skill 配置"
	case prefix == "/api/ai-access/audit":
		return "分页查询 AI 接入调用审计"
	case prefix == "/api/ai-access/audit/clear":
		return "清空 AI 接入调用审计"
	case prefix == "/api/system/ai-access":
		return "读取 AI 接入、Agent Key 和审计概览"
	case prefix == "/api/system/ai-access/key/rotate":
		return "轮换 AI Agent Key"
	case strings.HasPrefix(prefix, "/api/system/ai-access/mcp-servers"):
		return "管理 AI 接入的 MCP 服务配置"
	case strings.HasPrefix(prefix, "/api/system/ai-access/skills"):
		return "管理 AI 接入的 Skill 配置"
	case prefix == "/api/system/ai-access/audit":
		return "分页查询 AI 接入调用审计"
	case prefix == "/api/system/ai-access/audit/clear":
		return "清空 AI 接入调用审计"
	case prefix == "/api/ai/manifest":
		return "供外部 AI 客户端读取系统接入能力清单"
	case prefix == "/api/ai/mcp":
		return "供外部 AI 客户端通过 MCP 调用系统工具"
	case strings.HasPrefix(prefix, "/api/auth"):
		return "登录认证、会话校验和退出登录"
	case strings.HasPrefix(prefix, "/api/settings"):
		return "读取和保存系统运行配置"
	case strings.HasPrefix(prefix, "/api/system"):
		return "系统运行状态、日志、统计和管理能力"
	case strings.HasPrefix(prefix, "/api/logs"), strings.HasPrefix(prefix, "/ws/logs"):
		return "读取系统日志和实时日志流"
	case strings.HasPrefix(prefix, "/api/cloudflare"):
		return "管理 Cloudflare 账号、DNS、Tunnel 和相关资源"
	case strings.HasPrefix(prefix, "/api/server"), strings.HasPrefix(prefix, "/ws/ssh"), strings.HasPrefix(prefix, "/ws/agent-terminal"), strings.HasPrefix(prefix, "/socket.io"):
		return "管理主机实例、SSH 终端和实时连接"
	case strings.HasPrefix(prefix, "/api/openai"), strings.HasPrefix(prefix, "/v1"), strings.HasPrefix(prefix, "/api/chat"):
		return "OpenAI 兼容模型代理、聊天和流式响应"
	case strings.HasPrefix(prefix, "/api/aliyun"):
		return "管理阿里云资源和云服务接口"
	case strings.HasPrefix(prefix, "/api/tencent"):
		return "管理腾讯云资源和云服务接口"
	case strings.HasPrefix(prefix, "/api/koyeb"), strings.HasPrefix(prefix, "/api/flyio"):
		return "管理 PaaS 平台应用和部署资源"
	case strings.HasPrefix(prefix, "/api/totp"):
		return "管理双因子认证账户和动态验证码"
	case strings.HasPrefix(prefix, "/api/filebox"):
		return "管理文件柜上传、下载和文件条目"
	case strings.HasPrefix(prefix, "/api/uptime"):
		return "管理可用性监测、探针和状态记录"
	case strings.HasPrefix(prefix, "/api/notification"):
		return "管理通知渠道、消息发送和通知记录"
	case strings.HasPrefix(prefix, "/api/scheduler"), strings.HasPrefix(prefix, "/api/cron"):
		return "管理定时任务、计划执行和任务记录"
	case strings.HasPrefix(prefix, "/api/backup"):
		return "管理备份任务、备份记录和恢复操作"
	}
	if strings.TrimSpace(route.Description) != "" {
		return route.Description
	}
	return "系统接口"
}

func routeStatus(route manifest.Route) string {
	switch route.Owner {
	case manifest.OwnerGo:
		return "active"
	case manifest.OwnerRetired:
		return "retired"
	default:
		return "unknown"
	}
}

func inferRouteMethods(route manifest.Route) []string {
	if route.ResponseMode == manifest.ResponseWebSocket {
		return []string{"GET"}
	}
	if route.ResponseMode == manifest.ResponseStream {
		if strings.HasPrefix(route.Prefix, "/v1") {
			return []string{"POST", "GET"}
		}
		return []string{"GET"}
	}
	switch route.Prefix {
	case "/api/settings":
		return []string{"GET", "POST", "PATCH"}
	case "/api/auth/login", "/api/auth/logout", "/api/auth/set-password", "/api/auth/verify-password", "/api/auth/change-password", "/api/auth/2fa/setup", "/api/auth/2fa/enable", "/api/auth/2fa/disable", "/api/system/ai-access/key/rotate", "/api/ai-access/key/rotate", "/api/backup/run", "/api/backup/restore":
		return []string{"POST"}
	case "/api/auth/check-password", "/api/auth/session", "/api/auth/2fa/status", "/api/system/api-docs", "/api/system/openapi.json", "/api/openapi.json":
		return []string{"GET"}
	}
	description := strings.ToLower(route.Description)
	switch {
	case strings.Contains(description, "list/create"), strings.Contains(description, "list/create/clear"):
		if strings.Contains(description, "clear") {
			return []string{"GET", "POST", "DELETE"}
		}
		return []string{"GET", "POST"}
	case strings.Contains(description, "update/delete"):
		return []string{"PUT", "DELETE"}
	case strings.Contains(description, "read/update/delete"), strings.Contains(description, "get/update/delete"):
		return []string{"GET", "PUT", "DELETE"}
	case strings.Contains(description, "list/update"):
		return []string{"GET", "PUT"}
	case strings.Contains(description, "read/update"), strings.Contains(description, "config"), strings.Contains(description, "configuration"):
		return []string{"GET", "PUT"}
	case strings.Contains(description, "status update"), strings.Contains(description, "toggle"), strings.Contains(description, "action"), strings.Contains(description, "run"), strings.Contains(description, "retry"), strings.Contains(description, "cancel"), strings.Contains(description, "import"), strings.Contains(description, "preview"), strings.Contains(description, "verify"), strings.Contains(description, "refresh"), strings.Contains(description, "restore"), strings.Contains(description, "health check"), strings.Contains(description, "health-check"):
		return []string{"POST"}
	case strings.Contains(description, "delete"), strings.Contains(description, "cleanup"):
		return []string{"DELETE"}
	case strings.Contains(description, "list"), strings.Contains(description, "status"), strings.Contains(description, "summary"), strings.Contains(description, "logs"), strings.Contains(description, "metrics"), strings.Contains(description, "history"), strings.Contains(description, "models"), strings.Contains(description, "analytics"), strings.Contains(description, "export"):
		return []string{"GET"}
	}
	switch route.MatchMode {
	case manifest.MatchExact:
		if strings.Contains(route.Description, "list") || strings.Contains(route.Description, "read") || strings.Contains(route.Description, "status") {
			return []string{"GET"}
		}
	case manifest.MatchPattern:
		return []string{"GET", "POST", "PUT", "DELETE"}
	}
	if route.Owner == manifest.OwnerRetired {
		return []string{"GET"}
	}
	if route.Auth == manifest.AuthPublic && (route.Prefix == "/health" || strings.Contains(route.Description, "status")) {
		return []string{"GET"}
	}
	return []string{"GET", "POST", "PUT", "DELETE"}
}

func (s *Service) apiStats() (map[string]interface{}, error) {
	now := time.Now()
	startDateStr := now.AddDate(0, 0, -6).Format("2006-01-02")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT date, audit_count, ops_count FROM system_api_stats
		WHERE date >= ? ORDER BY date ASC
	`, startDateStr)
	if err != nil {
		return nil, fmt.Errorf("query api stats: %w", err)
	}
	defer rows.Close()

	dbData := make(map[string]*APICounters)
	for rows.Next() {
		var date string
		var audit, ops int64
		if err := rows.Scan(&date, &audit, &ops); err != nil {
			return nil, err
		}
		dbData[date] = &APICounters{Audit: audit, Ops: ops}
	}

	trend := make([]map[string]interface{}, 0, 7)
	var totalAudit, totalOps int64

	s.mu.Lock()
	for i := 0; i < 7; i++ {
		day := now.AddDate(0, 0, -6+i).Format("2006-01-02")

		var auditVal, opsVal int64
		if dbVal, exists := dbData[day]; exists {
			auditVal += dbVal.Audit
			opsVal += dbVal.Ops
		}
		if memVal, exists := s.statsCache[day]; exists {
			auditVal += memVal.Audit
			opsVal += memVal.Ops
		}

		totalAudit += auditVal
		totalOps += opsVal

		trend = append(trend, map[string]interface{}{
			"bucket": day,
			"audit":  auditVal,
			"ops":    opsVal,
			"total":  auditVal + opsVal,
		})
	}
	s.mu.Unlock()

	// 汇总最近 7 天的词元用量（OpenAI 网关）与订阅实际用量（流量），并按天对齐趋势桶。
	var totalTokens, totalTraffic int64
	tokensByDay, trafficByDay := systemUsageDaily(ctx, db, now)
	for _, item := range trend {
		bucket := item["bucket"].(string)
		tokens := tokensByDay[bucket]
		traffic := trafficByDay[bucket]
		item["tokens"] = tokens
		item["traffic"] = traffic
		totalTokens += tokens
		totalTraffic += traffic
	}
	return map[string]interface{}{
		"total": map[string]interface{}{
			"audit": totalAudit,
			"ops":   totalOps,
			"all":   totalAudit + totalOps,
		},
		"trend":   trend,
		"tokens":  totalTokens,
		"traffic": totalTraffic,
	}, nil
}

// systemUsageDaily 按天汇总最近 7 天的 OpenAI 网关词元消耗与订阅实际流量（上传+下载字节）。
// 返回以 "2006-01-02" 为键的逐日用量表，缺失日期返回 0。
func systemUsageDaily(ctx context.Context, db *sql.DB, now time.Time) (map[string]int64, map[string]int64) {
	start := now.AddDate(0, 0, -6).Format("2006-01-02 15:04:05")
	tokensByDay := make(map[string]int64)
	if rows, err := db.QueryContext(ctx, `
		SELECT strftime('%Y-%m-%d', timestamp) AS day, COALESCE(SUM(total_tokens), 0)
		FROM openai_gateway_analytics
		WHERE timestamp >= ? AND route != 'models'
		GROUP BY day`, start); err != nil {
		// 表可能尚未建好，返回空表即可。
		tokensByDay = nil
	} else {
		for rows.Next() {
			var day string
			var tokens int64
			if err := rows.Scan(&day, &tokens); err == nil {
				tokensByDay[day] = tokens
			}
		}
		rows.Close()
	}
	trafficByDay := make(map[string]int64)
	if rows, err := db.QueryContext(ctx, `
		SELECT strftime('%Y-%m-%d', reported_at) AS day, COALESCE(SUM(upload_bytes + download_bytes), 0)
		FROM subscription_usage_reports
		WHERE reported_at >= ?
		GROUP BY day`, start); err != nil {
		trafficByDay = nil
	} else {
		for rows.Next() {
			var day string
			var traffic int64
			if err := rows.Scan(&day, &traffic); err == nil {
				trafficByDay[day] = traffic
			}
		}
		rows.Close()
	}
	return tokensByDay, trafficByDay
}

func (s *Service) Shutdown() {
	close(s.stopChan)
	s.wg.Wait()
}

func (s *Service) hostMetrics() (map[string]interface{}, error) {
	hostInfo, _ := host.Info()
	cpuPercent := readCPUPercent()
	cpuInfo := readCPUInfo()
	cpuCoreInfo := readCPUCoreInfo()
	virtualMemory := readVirtualMemory()
	diskUsage := readDiskUsage()
	currentProcess := readProcessInfo(s.startedAt)

	return map[string]interface{}{
		"hostname":      hostname(),
		"platform":      nodePlatformName(runtime.GOOS),
		"platformLabel": platformLabel(hostInfo),
		"uptime":        systemUptimeSeconds(hostInfo),
		"cpu": map[string]interface{}{
			"usage":         cpuPercent,
			"cores":         cpuCoreInfo.physical,
			"physicalCores": cpuCoreInfo.physical,
			"logicalCores":  cpuCoreInfo.logical,
			"threads":       cpuCoreInfo.logical,
			"model":         cpuInfo.model,
			"loadAverage":   readLoadAverage(),
		},
		"memory":    virtualMemory,
		"disk":      diskUsage,
		"process":   currentProcess,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

type cpuDetails struct {
	model string
}

type cpuCoreDetails struct {
	physical int
	logical  int
}

func readCPUPercent() float64 {
	percentages, err := cpu.Percent(120*time.Millisecond, false)
	if err != nil || len(percentages) == 0 {
		return 0
	}
	return clampPercent(percentages[0])
}

func readCPUInfo() cpuDetails {
	info, err := cpu.Info()
	if err != nil || len(info) == 0 {
		return cpuDetails{}
	}
	return cpuDetails{model: info[0].ModelName}
}

func readCPUCoreInfo() cpuCoreDetails {
	logical, err := cpu.Counts(true)
	if err != nil || logical < 1 {
		logical = runtime.NumCPU()
	}
	physical, err := cpu.Counts(false)
	if err != nil || physical < 1 {
		physical = logical
	}
	return cpuCoreDetails{
		physical: physical,
		logical:  logical,
	}
}

func readVirtualMemory() map[string]interface{} {
	stats, err := mem.VirtualMemory()
	if err != nil {
		return map[string]interface{}{
			"total": uint64(0),
			"used":  uint64(0),
			"free":  uint64(0),
			"usage": float64(0),
			"error": err.Error(),
		}
	}
	return map[string]interface{}{
		"total": stats.Total,
		"used":  stats.Used,
		"free":  stats.Free,
		"usage": clampPercent(stats.UsedPercent),
	}
}

func isVirtualFS(fstype string) bool {
	virtualFS := map[string]bool{
		"tmpfs":       true,
		"devtmpfs":    true,
		"sysfs":       true,
		"proc":        true,
		"devpts":      true,
		"cgroup":      true,
		"overlay":     true,
		"squashfs":    true,
		"iso9660":     true,
		"udf":         true,
		"configfs":    true,
		"debugfs":     true,
		"tracefs":     true,
		"securityfs":  true,
		"pstore":      true,
		"bpf":         true,
		"fusectl":     true,
		"mqueue":      true,
		"hugetlbfs":   true,
		"autofs":      true,
		"binfmt_misc": true,
		"devfs":       true,
		"fdescfs":     true,
		"linprocfs":   true,
		"linsysfs":    true,
		"procfs":      true,
		"sysctlfs":    true,
	}
	return virtualFS[fstype]
}

func readDiskUsageSingle() map[string]interface{} {
	root := rootPath()
	usage, err := disk.Usage(root)
	if err != nil {
		return map[string]interface{}{
			"root":  root,
			"total": uint64(0),
			"used":  uint64(0),
			"free":  uint64(0),
			"usage": float64(0),
			"error": err.Error(),
		}
	}
	return map[string]interface{}{
		"root":  usage.Path,
		"total": usage.Total,
		"used":  usage.Used,
		"free":  usage.Free,
		"usage": clampPercent(usage.UsedPercent),
	}
}

func readDiskUsage() map[string]interface{} {
	parts, err := disk.Partitions(false)
	if err != nil {
		return readDiskUsageSingle()
	}

	var total, used, free uint64
	var roots []string
	seenDevices := make(map[string]bool)

	for _, p := range parts {
		if isVirtualFS(p.Fstype) {
			continue
		}
		if p.Device == "" || seenDevices[p.Device] {
			continue
		}

		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}
		if usage.Total == 0 {
			continue
		}

		seenDevices[p.Device] = true
		total += usage.Total
		used += usage.Used
		free += usage.Free
		roots = append(roots, p.Mountpoint)
	}

	if len(roots) == 0 {
		return readDiskUsageSingle()
	}

	var usagePercent float64
	if total > 0 {
		usagePercent = float64(used) / float64(total) * 100
	}

	return map[string]interface{}{
		"root":  strings.Join(roots, ", "),
		"total": total,
		"used":  used,
		"free":  free,
		"usage": clampPercent(usagePercent),
	}
}

func readLoadAverage() []float64 {
	avg, err := load.Avg()
	if err != nil || avg == nil {
		return []float64{0, 0, 0}
	}
	return []float64{avg.Load1, avg.Load5, avg.Load15}
}

func readProcessInfo(startedAt time.Time) map[string]interface{} {
	memoryRSS := uint64(0)
	if current, err := process.NewProcess(int32(os.Getpid())); err == nil {
		if info, err := current.MemoryInfo(); err == nil && info != nil {
			memoryRSS = info.RSS
		}
	}
	return map[string]interface{}{
		"uptime":    time.Since(startedAt).Seconds(),
		"memoryRss": memoryRSS,
	}
}

func systemUptimeSeconds(info *host.InfoStat) uint64 {
	if info != nil && info.Uptime > 0 {
		return info.Uptime
	}
	return 0
}

func platformLabel(info *host.InfoStat) string {
	osType := osTypeName(runtime.GOOS)
	release := ""
	if info != nil {
		release = info.KernelVersion
		if release == "" {
			release = info.PlatformVersion
		}
	}
	if release == "" {
		return osType
	}
	return fmt.Sprintf("%s %s", osType, release)
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

func rootPath() string {
	wd, err := os.Getwd()
	if err != nil {
		if runtime.GOOS == "windows" {
			return `C:\`
		}
		return "/"
	}
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(wd)
		if volume == "" {
			return `C:\`
		}
		return volume + `\`
	}
	volume := filepath.VolumeName(wd)
	if volume != "" {
		return volume + string(os.PathSeparator)
	}
	return string(os.PathSeparator)
}

func nodePlatformName(goos string) string {
	switch goos {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	default:
		return goos
	}
}

func osTypeName(goos string) string {
	switch goos {
	case "windows":
		return "Windows_NT"
	case "darwin":
		return "Darwin"
	case "linux":
		return "Linux"
	default:
		return goos
	}
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
