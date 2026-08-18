package github

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/publicpageicon"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"github.com/iwvw/api-monitor/backend-go/internal/sseutil"
)

type Service struct {
	cfg      config.Config
	store    *database.Store
	schema   database.SchemaEnsurer
	client   *apiClient
	notifier Notifier

	stop           chan struct{}
	stopped        chan struct{}
	statusMu       sync.Mutex
	status         CollectorStatus
	streamMu       sync.Mutex
	streamNext     int64
	streams        map[int64]chan map[string]interface{}
	actionPollMu   sync.Mutex
	actionLastPoll map[int64]time.Time
	// webhook 刷新的每仓库去重：事件风暴时同一仓库不并发发起多个 GitHub API 刷新
	webhookRefreshMu    sync.Mutex
	webhookRefreshBusy  map[int64]bool
	webhookRefreshTimer map[int64]*time.Timer
}

func New(cfg config.Config) *Service {
	s := &Service{
		cfg:                 cfg,
		store:               database.New(cfg),
		client:              newAPIClient(),
		stop:                make(chan struct{}),
		stopped:             make(chan struct{}),
		streams:             map[int64]chan map[string]interface{}{},
		actionLastPoll:      map[int64]time.Time{},
		webhookRefreshBusy:  map[int64]bool{},
		webhookRefreshTimer: map[int64]*time.Timer{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := s.open(ctx); err == nil {
		db.Close()
	}
	go s.collectorLoop()
	return s
}

func (s *Service) SetNotifier(notifier Notifier) {
	s.notifier = notifier
}

func (s *Service) Stop() {
	select {
	case <-s.stopped:
		return
	default:
	}
	close(s.stop)
	<-s.stopped
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/github")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case path == "" && r.Method == http.MethodGet:
		s.overview(w, r)
	case len(parts) == 1 && parts[0] == "public-pages":
		s.publicPages(w, r)
	case len(parts) == 2 && parts[0] == "public-pages":
		s.publicPageByID(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "public" && parts[1] == "pages" && parts[3] == "repositories" && r.Method == http.MethodGet:
		s.publicPageRepositoryBySlug(w, r, parts[2], parts[4])
	case len(parts) == 4 && parts[0] == "public" && parts[1] == "pages" && parts[3] == "stream" && r.Method == http.MethodGet:
		s.publicPageEventStream(w, r, parts[2])
	case len(parts) == 3 && parts[0] == "public" && parts[1] == "pages" && r.Method == http.MethodGet:
		s.publicPageBySlug(w, r, parts[2])
	case len(parts) == 2 && parts[0] == "public" && parts[1] == "page-by-domain" && r.Method == http.MethodGet:
		s.publicPageByDomain(w, r)
	case len(parts) == 1 && parts[0] == "tokens":
		s.tokens(w, r)
	case len(parts) == 2 && parts[0] == "tokens":
		s.tokenByID(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "tokens" && parts[2] == "test" && r.Method == http.MethodPost:
		s.testToken(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "tokens" && parts[2] == "rotate" && r.Method == http.MethodPost:
		s.rotateToken(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "repositories":
		s.repositories(w, r)
	case len(parts) == 2 && parts[0] == "repositories" && parts[1] == "parse-url" && r.Method == http.MethodPost:
		s.parseURL(w, r)
	case len(parts) == 2 && parts[0] == "repositories" && parts[1] == "reorder" && r.Method == http.MethodPost:
		s.reorderRepositories(w, r)
	case len(parts) == 2 && parts[0] == "repositories":
		s.repositoryByID(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "repositories" && parts[2] == "refresh" && r.Method == http.MethodPost:
		s.refreshRepository(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "repositories" && parts[2] == "summary" && r.Method == http.MethodGet:
		s.repositorySummary(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "repositories" && parts[2] == "trends" && r.Method == http.MethodGet:
		s.repositoryTrends(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "repositories" && parts[2] == "actions" && parts[3] == "runs" && r.Method == http.MethodGet:
		s.repositoryActionRuns(w, r, parts[1])
	case len(parts) == 6 && parts[0] == "repositories" && parts[2] == "actions" && parts[3] == "runs" && parts[5] == "jobs" && r.Method == http.MethodGet:
		s.repositoryWorkflowJobs(w, r, parts[1], parts[4])
	case len(parts) == 4 && parts[0] == "repositories" && parts[2] == "actions" && parts[3] == "refresh" && r.Method == http.MethodPost:
		s.refreshRepositoryActions(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "repositories" && parts[2] == "actions" && parts[3] == "workflows" && r.Method == http.MethodGet:
		s.repositoryWorkflows(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "repositories" && parts[2] == "branches" && r.Method == http.MethodGet:
		s.repositoryBranches(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "repositories" && parts[2] == "webhook" && parts[3] == "configure" && r.Method == http.MethodPost:
		s.configureRepositoryWebhook(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "repositories" && parts[2] == "traffic" && r.Method == http.MethodGet:
		s.repositoryTraffic(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "repositories" && parts[2] == "contributors" && r.Method == http.MethodGet:
		s.repositoryContributors(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "repositories" && parts[2] == "events" && r.Method == http.MethodGet:
		s.repositoryEvents(w, r, parts[1])
	case len(parts) == 6 && parts[0] == "repositories" && parts[2] == "actions" && parts[3] == "runs" && parts[5] == "rerun" && r.Method == http.MethodPost:
		s.workflowRunOperation(w, r, parts[1], parts[4], "rerun")
	case len(parts) == 6 && parts[0] == "repositories" && parts[2] == "actions" && parts[3] == "runs" && parts[5] == "rerun-failed-jobs" && r.Method == http.MethodPost:
		s.workflowRunOperation(w, r, parts[1], parts[4], "rerun-failed-jobs")
	case len(parts) == 6 && parts[0] == "repositories" && parts[2] == "actions" && parts[3] == "runs" && parts[5] == "cancel" && r.Method == http.MethodPost:
		s.workflowRunOperation(w, r, parts[1], parts[4], "cancel")
	case len(parts) == 6 && parts[0] == "repositories" && parts[2] == "actions" && parts[3] == "workflows" && parts[5] == "dispatch" && r.Method == http.MethodPost:
		s.workflowDispatch(w, r, parts[1], parts[4])
	case len(parts) == 1 && parts[0] == "settings":
		s.settings(w, r)
	case len(parts) == 2 && parts[0] == "collector" && parts[1] == "run" && r.Method == http.MethodPost:
		s.runCollector(w, r)
	case len(parts) == 2 && parts[0] == "collector" && parts[1] == "status" && r.Method == http.MethodGet:
		s.collectorStatus(w, r)
	case len(parts) == 1 && parts[0] == "history" && r.Method == http.MethodDelete:
		s.deleteHistory(w, r)
	case len(parts) == 2 && parts[0] == "history" && parts[1] == "compact" && r.Method == http.MethodPost:
		s.compactHistory(w, r)
	case len(parts) == 1 && parts[0] == "events" && r.Method == http.MethodGet:
		s.events(w, r)
	case len(parts) == 2 && parts[0] == "events" && parts[1] == "stream" && r.Method == http.MethodGet:
		s.eventStream(w, r)
	case len(parts) >= 1 && parts[0] == "webhook":
		s.webhook(w, r, parts)
	default:
		response.Error(w, http.StatusNotFound, "github route not implemented")
	}
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.schema.Ensure(func() error { return ensureSchema(ctx, db) }); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Service) overview(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	repos, _ := listRepositories(r.Context(), db, false)
	attachLatestActionTiming(r.Context(), db, repos)
	defaultTokenAvailable := false
	defaultTokenLogin := ""
	if token, ok, tokenErr := getDefaultToken(r.Context(), db); tokenErr == nil && ok {
		defaultTokenAvailable = token.Enabled && token.TokenEncrypted != ""
		defaultTokenLogin = token.AccountLogin
	}
	for i := range repos {
		repos[i].Authenticated = defaultTokenAvailable
		repos[i].OwnedByToken = defaultTokenLogin != "" && strings.EqualFold(defaultTokenLogin, repos[i].Owner)
		if repos[i].TokenID != nil {
			if token, ok, tokenErr := getToken(r.Context(), db, *repos[i].TokenID); tokenErr == nil && ok {
				repos[i].Authenticated = token.Enabled && token.TokenEncrypted != ""
				repos[i].OwnedByToken = token.AccountLogin != "" && strings.EqualFold(token.AccountLogin, repos[i].Owner)
			} else {
				repos[i].Authenticated = false
				repos[i].OwnedByToken = false
			}
		}
	}
	settings, _ := loadSettings(r.Context(), db)
	response.OK(w, map[string]interface{}{"repositories": repos, "settings": settings, "collector": s.currentStatus()})
}

func (s *Service) publicPages(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodGet:
		pages, err := listPublicPages(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, pages)
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		page, ok, err := savePublicPage(r.Context(), db, 0, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ok {
			response.Error(w, http.StatusNotFound, "公开页不存在")
			return
		}
		response.OK(w, page)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) publicPageByID(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid public page id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		page, ok, err := savePublicPage(r.Context(), db, id, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ok {
			response.Error(w, http.StatusNotFound, "公开页不存在")
			return
		}
		response.OK(w, page)
	case http.MethodDelete:
		result, err := db.ExecContext(r.Context(), `DELETE FROM github_public_pages WHERE id = ?`, id)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			response.Error(w, http.StatusNotFound, "公开页不存在")
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) publicPageBySlug(w http.ResponseWriter, r *http.Request, slug string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	page, ok, err := getPublicPageBySlug(r.Context(), db, slug, true)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "公开页不存在或未公开")
		return
	}

	summary := boolQuery(r, "summary", false)
	payload, err := s.publicPagePayload(r.Context(), db, page, !summary)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	setPublicPageCacheControl(w, page, summary)
	response.OK(w, payload)
}

func (s *Service) publicPageByDomain(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		domain = r.Host
	}

	page, ok, err := getPublicPageByDomain(r.Context(), db, domain)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.OK(w, map[string]interface{}{"found": false})
		return
	}

	summary := boolQuery(r, "summary", false)
	payload, err := s.publicPagePayload(r.Context(), db, page, !summary)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	setPublicPageCacheControl(w, page, summary)
	response.OK(w, payload)
}

// PublicPageIconID 返回公开页配置的自定义图标 ID（未设置时为空字符串），
// 供服务端 favicon 解析端点使用；lookup 为 slug 或域名。
func (s *Service) PublicPageIconID(ctx context.Context, lookup string, byDomain bool) (string, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return "", false, err
	}
	defer db.Close()
	arg := normalizeGitHubPublicSlug(lookup, "github")
	if byDomain {
		arg = normalizeGitHubPublicDomain(lookup)
	}
	return publicpageicon.LookupIconID(ctx, db, `github_public_pages`, arg, byDomain)
}

func (s *Service) publicPageRepositoryBySlug(w http.ResponseWriter, r *http.Request, slug, repoText string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	page, ok, err := getPublicPageBySlug(r.Context(), db, slug, true)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "公开页不存在或未公开")
		return
	}

	repoID, err := parseID(repoText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid repository id")
		return
	}
	if !containsInt64(page.RepositoryIDs, repoID) {
		response.Error(w, http.StatusNotFound, "仓库未绑定到当前公开页")
		return
	}

	item, _, latestRun, ok, err := s.publicRepositorySummaryItem(r.Context(), db, repoID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "仓库不存在")
		return
	}
	if runIDParam := r.URL.Query().Get("run_id"); runIDParam != "" {
		if rid, parseErr := strconv.ParseInt(runIDParam, 10, 64); parseErr == nil && rid > 0 {
			if specificRun, ok, _ := getActionRunByRunID(r.Context(), db, repoID, rid); ok {
				latestRun = specificRun
				item["latest_run"] = specificRun
			}
		}
	}
	s.attachPublicRepositoryWorkflowDetail(r.Context(), db, item, latestRun)
	setPublicPageCacheControl(w, page, true)
	response.OK(w, item)
}

func setPublicPageCacheControl(w http.ResponseWriter, page PublicPage, realtime bool) {
	if realtime {
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		return
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", clamp(page.CacheSeconds, 30, 86400)))
}

func (s *Service) publicPageEventStream(w http.ResponseWriter, r *http.Request, slug string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		response.Error(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	page, found, pageErr := getPublicPageBySlug(r.Context(), db, slug, true)
	db.Close()
	if pageErr != nil {
		response.Error(w, http.StatusInternalServerError, pageErr.Error())
		return
	}
	if !found {
		response.Error(w, http.StatusNotFound, "公开页不存在或未公开")
		return
	}

	repositoryIDs := make(map[int64]struct{}, len(page.RepositoryIDs))
	for _, repositoryID := range page.RepositoryIDs {
		repositoryIDs[repositoryID] = struct{}{}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.subscribe()
	defer cancel()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
		return
	}
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", jsonString(map[string]interface{}{"connected": true}))
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
				return
			}
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case event := <-ch:
			payload, visible := publicPageEventPayload(repositoryIDs, event)
			if !visible {
				continue
			}
			if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
				return
			}
			fmt.Fprintf(w, "event: github\ndata: %s\n\n", jsonString(payload))
			flusher.Flush()
		}
	}
}

func publicPageEventPayload(repositoryIDs map[int64]struct{}, event map[string]interface{}) (map[string]interface{}, bool) {
	repositoryID := int64Value(event["repository_id"], 0)
	if _, visible := repositoryIDs[repositoryID]; !visible {
		return nil, false
	}
	return map[string]interface{}{
		"kind":          asString(event["kind"]),
		"repository_id": repositoryID,
	}, true
}

func (s *Service) publicPagePayload(ctx context.Context, db *sql.DB, page PublicPage, includeWorkflowDetails bool) (map[string]interface{}, error) {
	repositories := make([]map[string]interface{}, 0, len(page.RepositoryIDs))
	for _, repoID := range page.RepositoryIDs {
		item, _, latestRun, ok, err := s.publicRepositorySummaryItem(ctx, db, repoID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if includeWorkflowDetails {
			s.attachPublicRepositoryWorkflowDetail(ctx, db, item, latestRun)
		}
		repositories = append(repositories, item)
	}

	return map[string]interface{}{
		"id":            page.ID,
		"slug":          page.Slug,
		"domain":        page.Domain,
		"title":         page.Title,
		"description":   page.Description,
		"public":        page.Public,
		"cacheSeconds":  page.CacheSeconds,
		"config":        page.Config,
		"repositoryIds": page.RepositoryIDs,
		"repositories":  repositories,
		"createdAt":     page.CreatedAt,
		"updatedAt":     page.UpdatedAt,
	}, nil
}

func (s *Service) publicRepositorySummaryItem(ctx context.Context, db *sql.DB, repoID int64) (map[string]interface{}, Repository, map[string]interface{}, bool, error) {
	repo, ok, err := getRepository(ctx, db, repoID)
	if err != nil {
		return nil, Repository{}, nil, false, err
	}
	if !ok {
		return nil, Repository{}, nil, false, nil
	}

	repoWithTiming := []Repository{repo}
	attachLatestActionTiming(ctx, db, repoWithTiming)
	repo = repoWithTiming[0]
	latestRun, _, err := latestActionRunForRepository(ctx, db, repo.ID)
	if err != nil {
		return nil, Repository{}, nil, false, err
	}
	recentRuns, _ := recentActionRunsForRepository(ctx, db, repo.ID, 15)

	item := map[string]interface{}{
		"id":                       repo.ID,
		"owner":                    repo.Owner,
		"name":                     repo.Name,
		"full_name":                repo.FullName,
		"html_url":                 repo.HTMLURL,
		"description":              repo.Description,
		"private":                  repo.Private,
		"default_branch":           repo.DefaultBranch,
		"language":                 repo.Language,
		"stars":                    repo.Stars,
		"forks":                    repo.Forks,
		"watchers":                 repo.Watchers,
		"open_issues":              repo.OpenIssues,
		"open_pull_requests":       repo.OpenPullRequests,
		"latest_release":           repo.LatestRelease,
		"latest_release_url":       repo.LatestReleaseURL,
		"latest_action_status":     repo.LatestActionStatus,
		"latest_action_conclusion": repo.LatestActionConclusion,
		"latest_action_started_at": repo.LatestActionStartedAt,
		"latest_action_created_at": repo.LatestActionCreatedAt,
		"latest_action_updated_at": repo.LatestActionUpdatedAt,
		"latest_run":               latestRun,
		"recent_runs":              recentRuns,
		"updated_at":               repo.UpdatedAt,
	}
	return item, repo, latestRun, true, nil
}

func (s *Service) attachPublicRepositoryWorkflowDetail(ctx context.Context, db *sql.DB, item map[string]interface{}, latestRun map[string]interface{}) {
	if item == nil || latestRun == nil {
		return
	}

	repo := Repository{
		ID:            int64Value(item["id"], 0),
		Owner:         asString(item["owner"]),
		Name:          asString(item["name"]),
		DefaultBranch: asString(item["default_branch"]),
		Private:       boolValue(item["private"], false),
	}

	token, err := s.tokenForRepository(ctx, db, repo)
	if err != nil {
		item["workflow_error"] = err.Error()
		return
	}

	runID := int64Value(latestRun["run_id"], 0)
	if runID <= 0 {
		return
	}

	jobs, _, err := s.client.fetchWorkflowJobs(ctx, token, repo.Owner, repo.Name, runID)
	if err != nil {
		item["workflow_error"] = err.Error()
		return
	}

	workflow := s.workflowLayoutForRun(
		ctx,
		token,
		repo,
		asString(latestRun["workflow_name"]),
		asString(latestRun["branch"]),
		asString(latestRun["commit_sha"]),
	)
	item["jobs"] = jobs.Jobs
	item["workflow"] = workflow
}

func (s *Service) tokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		tokens, err := listTokens(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, safeTokens(tokens))
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		name := strings.TrimSpace(stringValue(payload, "name", ""))
		rawToken := strings.TrimSpace(stringValue(payload, "token", ""))
		if name == "" || rawToken == "" {
			response.Error(w, http.StatusBadRequest, "名称和 Token 不能为空")
			return
		}
		encrypted, err := secure.SecureEncrypt(rawToken)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer tx.Rollback()
		if boolValue(payload["default_token"], false) {
			_, _ = tx.ExecContext(r.Context(), `UPDATE github_tokens SET default_token = 0`)
		}
		res, err := tx.ExecContext(r.Context(), `INSERT INTO github_tokens (name, type, token_encrypted, enabled, default_token, note)
			VALUES (?, ?, ?, ?, ?, ?)`, name, firstNonEmpty(stringValue(payload, "type", ""), "fine_grained"), encrypted, boolInt(boolValue(payload["enabled"], true)), boolInt(boolValue(payload["default_token"], false)), stringValue(payload, "note", ""))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		id, _ := res.LastInsertId()
		token, _, _ := getToken(r.Context(), db, id)
		response.OK(w, safeToken(token))
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) tokenByID(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid token id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		sets := []string{"updated_at = CURRENT_TIMESTAMP"}
		args := []interface{}{}
		if v := strings.TrimSpace(stringValue(payload, "name", "")); v != "" {
			sets = append(sets, "name = ?")
			args = append(args, v)
		}
		if v, ok := payload["enabled"]; ok {
			sets = append(sets, "enabled = ?")
			args = append(args, boolInt(boolValue(v, true)))
		}
		if v, ok := payload["default_token"]; ok {
			def := boolValue(v, false)
			if def {
				_, _ = db.ExecContext(r.Context(), `UPDATE github_tokens SET default_token = 0`)
			}
			sets = append(sets, "default_token = ?")
			args = append(args, boolInt(def))
		}
		if v := strings.TrimSpace(stringValue(payload, "token", "")); v != "" {
			encrypted, err := secure.SecureEncrypt(v)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			sets = append(sets, "token_encrypted = ?")
			args = append(args, encrypted)
		}
		if _, ok := payload["note"]; ok {
			sets = append(sets, "note = ?")
			args = append(args, stringValue(payload, "note", ""))
		}
		args = append(args, id)
		if _, err := db.ExecContext(r.Context(), `UPDATE github_tokens SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		token, _, _ := getToken(r.Context(), db, id)
		response.OK(w, safeToken(token))
	case http.MethodDelete:
		if _, err := db.ExecContext(r.Context(), `DELETE FROM github_tokens WHERE id = ?`, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) testToken(w http.ResponseWriter, r *http.Request, idText string) {
	token, db, ok := s.tokenForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	plain := secure.SecureDecrypt(token.TokenEncrypted)
	var repo *Repository
	if repoID := int64Query(r, "repositoryId", 0); repoID > 0 {
		if found, exists, repoErr := getRepository(r.Context(), db, repoID); repoErr == nil && exists {
			repo = &found
		}
	}
	result, rate, err := s.client.testToken(r.Context(), plain, repo)
	status := "success"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}
	permissions := jsonString(map[string]interface{}{})
	accountLogin := ""
	if result != nil {
		permissions = jsonString(result["permissions"])
		if user, exists := result["user"].(map[string]interface{}); exists {
			accountLogin = strings.TrimSpace(asString(user["login"]))
		}
		if permissionMap, exists := result["permissions"].(map[string]interface{}); exists {
			if checks, exists := permissionMap["checks"].([]map[string]interface{}); exists {
				for _, check := range checks {
					if check["status"] == "failed" {
						status = "warning"
						break
					}
				}
			}
		}
	}
	if err == nil && repo != nil {
		if remote, _, repoErr := s.client.fetchRepository(r.Context(), plain, repo.Owner, repo.Name); repoErr == nil {
			ownedByToken := accountLogin != "" && strings.EqualFold(accountLogin, remote.Owner.Login)
			canOperateActions := remote.Permissions.Admin || remote.Permissions.Maintain || remote.Permissions.Push
			if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("bind")), "true") {
				_, _ = db.ExecContext(r.Context(), `UPDATE github_repositories SET token_id = ?, owned_by_token = ?, can_operate_actions = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
					token.ID, boolInt(ownedByToken), boolInt(canOperateActions), repo.ID)
			} else {
				_, _ = db.ExecContext(r.Context(), `UPDATE github_repositories SET owned_by_token = ?, can_operate_actions = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
					boolInt(ownedByToken), boolInt(canOperateActions), repo.ID)
			}
		}
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE github_tokens SET last_test_status = ?, last_test_error = ?, last_test_at = CURRENT_TIMESTAMP,
		account_login = ?, scopes = ?, permissions_json = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, errMsg, accountLogin, rate.OAuthScopes, permissions, token.ID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, result)
}

func (s *Service) rotateToken(w http.ResponseWriter, r *http.Request, idText string) {
	s.tokenByID(w, r, idText)
}

func (s *Service) repositories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		repos, err := listRepositories(r.Context(), db, false)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, repos)
	case http.MethodPost:
		s.createRepository(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) createRepository(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	owner, repo := parseRepoInput(firstNonEmpty(stringValue(payload, "url", ""), stringValue(payload, "repository", ""), stringValue(payload, "full_name", "")))
	if owner == "" || repo == "" {
		owner = strings.TrimSpace(stringValue(payload, "owner", ""))
		repo = strings.TrimSpace(stringValue(payload, "name", ""))
	}
	if owner == "" || repo == "" {
		response.Error(w, http.StatusBadRequest, "请输入 GitHub 仓库 URL 或 owner/repo")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	settings, _ := loadSettings(r.Context(), db)
	var tokenID *int64
	if v, ok := payload["token_id"]; ok {
		if id := int64Value(v, 0); id > 0 {
			tokenID = &id
		}
	}
	plainToken := ""
	tokenAccountLogin := ""
	if tokenID != nil {
		token, ok, err := getToken(r.Context(), db, *tokenID)
		if err != nil || !ok {
			response.Error(w, http.StatusBadRequest, "GitHub Token 不存在")
			return
		}
		plainToken = secure.SecureDecrypt(token.TokenEncrypted)
		tokenAccountLogin = token.AccountLogin
	} else if token, ok, _ := getDefaultToken(r.Context(), db); ok {
		id := token.ID
		tokenID = &id
		plainToken = secure.SecureDecrypt(token.TokenEncrypted)
		tokenAccountLogin = token.AccountLogin
	}
	ghRepo, rate, err := s.client.fetchRepository(r.Context(), plainToken, owner, repo)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	secret := ""
	if settings.AutoCreateWebhookSecret {
		secret = randomSecret()
	}
	fullName := firstNonEmpty(ghRepo.FullName, owner+"/"+repo)
	ownedByToken := tokenAccountLogin != "" && strings.EqualFold(tokenAccountLogin, ghRepo.Owner.Login)
	canOperateActions := ghRepo.Permissions.Admin || ghRepo.Permissions.Maintain || ghRepo.Permissions.Push
	res, err := db.ExecContext(r.Context(), `INSERT INTO github_repositories (
		token_id, owner, name, full_name, html_url, description, private, owned_by_token, can_operate_actions, default_branch, language,
		tags, note, enabled, notify_enabled, webhook_enabled, webhook_secret, collect_interval_seconds, retention_days,
		last_status, stars, forks, watchers, open_issues, rate_limit_remaining, rate_limit_reset
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?)`,
		nullableInt64(tokenID), owner, repo, fullName, ghRepo.HTMLURL, ghRepo.Description, boolInt(ghRepo.Private), boolInt(ownedByToken), boolInt(canOperateActions), ghRepo.DefaultBranch, ghRepo.Language,
		parseTags(payload["tags"]), stringValue(payload, "note", ""), boolInt(boolValue(payload["enabled"], true)), boolInt(boolValue(payload["notify_enabled"], true)),
		boolInt(boolValue(payload["webhook_enabled"], false)), secret, intValue(payload, "collect_interval_seconds", settings.DefaultCollectInterval),
		intValue(payload, "retention_days", settings.DefaultRetentionDays), ghRepo.StargazersCount, ghRepo.ForksCount, ghRepo.WatchersCount, ghRepo.OpenIssuesCount,
		rate.Remaining, timeOrNil(rate.Reset))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	_, _ = db.ExecContext(r.Context(), `UPDATE github_repositories SET display_order = ? WHERE id = ? AND display_order = 0`, id, id)
	go s.refreshRepositoryByID(context.Background(), id, "create")
	repository, _, _ := getRepository(r.Context(), db, id)
	response.OK(w, repository)
}

func (s *Service) reorderRepositories(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	values, ok := payload["repository_ids"].([]interface{})
	if !ok || len(values) == 0 {
		response.Error(w, http.StatusBadRequest, "仓库顺序不能为空")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	seen := map[int64]bool{}
	for index, value := range values {
		id := int64Value(value, 0)
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := tx.ExecContext(r.Context(), `UPDATE github_repositories SET display_order = ?, updated_at = updated_at WHERE id = ?`, index+1, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	repos, _ := listRepositories(r.Context(), db, false)
	response.OK(w, repos)
}

func (s *Service) repositoryByID(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid repository id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodGet:
		repo, ok, err := getRepository(r.Context(), db, id)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			response.Error(w, http.StatusNotFound, "仓库不存在")
			return
		}
		response.OK(w, repo)
	case http.MethodPut, http.MethodPatch:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		updates := []string{"updated_at = CURRENT_TIMESTAMP"}
		args := []interface{}{}
		for key, column := range map[string]string{"note": "note", "tags": "tags", "description": "description"} {
			if _, ok := payload[key]; ok {
				updates = append(updates, column+" = ?")
				if key == "tags" {
					args = append(args, parseTags(payload[key]))
				} else {
					args = append(args, stringValue(payload, key, ""))
				}
			}
		}
		for key, column := range map[string]string{"enabled": "enabled", "notify_enabled": "notify_enabled", "webhook_enabled": "webhook_enabled"} {
			if v, ok := payload[key]; ok {
				updates = append(updates, column+" = ?")
				args = append(args, boolInt(boolValue(v, true)))
			}
		}
		for key, column := range map[string]string{"collect_interval_seconds": "collect_interval_seconds", "retention_days": "retention_days", "token_id": "token_id"} {
			if v, ok := payload[key]; ok {
				updates = append(updates, column+" = ?")
				if key == "token_id" {
					args = append(args, nullableInt64(int64Value(v, 0)))
					updates = append(updates, "owned_by_token = ?", "can_operate_actions = ?")
					args = append(args, 0, 0)
				} else {
					args = append(args, int64Value(v, 0))
				}
			}
		}
		args = append(args, id)
		if _, err := db.ExecContext(r.Context(), `UPDATE github_repositories SET `+strings.Join(updates, ", ")+` WHERE id = ?`, args...); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		repo, _, _ := getRepository(r.Context(), db, id)
		response.OK(w, repo)
	case http.MethodDelete:
		clean := strings.EqualFold(r.URL.Query().Get("clean"), "true")
		if clean {
			for _, table := range []string{"github_repository_snapshots", "github_action_runs", "github_traffic_samples", "github_contributors", "github_events", "github_operation_audit"} {
				_, _ = db.ExecContext(r.Context(), `DELETE FROM `+table+` WHERE repository_id = ?`, id)
			}
		}
		if _, err := db.ExecContext(r.Context(), `DELETE FROM github_repositories WHERE id = ?`, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true, "history_cleaned": clean})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) parseURL(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	owner, repo := parseRepoInput(firstNonEmpty(stringValue(payload, "url", ""), stringValue(payload, "repository", "")))
	if owner == "" || repo == "" {
		response.Error(w, http.StatusBadRequest, "无法解析 GitHub 仓库地址")
		return
	}
	response.OK(w, map[string]interface{}{"owner": owner, "repo": repo, "full_name": owner + "/" + repo, "html_url": "https://github.com/" + owner + "/" + repo})
}

func (s *Service) refreshRepository(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid repository id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	if _, ok, err := getRepository(r.Context(), db, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	} else if !ok {
		response.Error(w, http.StatusNotFound, "repository not found")
		return
	}
	if err := s.refreshRepositoryByID(r.Context(), id, "manual"); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	repo, _, _ := getRepository(r.Context(), db, id)
	response.OK(w, repo)
}

func (s *Service) repositorySummary(w http.ResponseWriter, r *http.Request, idText string) {
	id, _ := parseID(idText)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	repo, ok, err := getRepository(r.Context(), db, id)
	if err != nil || !ok {
		response.Error(w, http.StatusNotFound, "仓库不存在")
		return
	}
	events, _ := listEvents(r.Context(), db, id, 20)
	response.OK(w, map[string]interface{}{"repository": repo, "events": events})
}

func (s *Service) repositoryTrends(w http.ResponseWriter, r *http.Request, idText string) {
	id, _ := parseID(idText)
	days := clamp(intQuery(r, "days", 30), 1, 3650)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	// 快照时间戳是 SQLite CURRENT_TIMESTAMP（空格格式），cutoff 必须用同一
	// 格式比较，否则 '2026-01-01T...' 与 '2026-01-01 10:00:00' 在字节 10
	// 处错位（'T' > ' '），首日快照会被永久排除。
	cutoff := time.Now().AddDate(0, 0, -days).UTC().Format("2006-01-02 15:04:05")
	rows, err := db.QueryContext(r.Context(), `SELECT id, repository_id, stars, forks, watchers, open_issues, open_pull_requests,
		commit_count, release_count, contributor_count, actions_total, actions_success, actions_failed,
		traffic_views, traffic_uniques, traffic_clones, traffic_clone_uniques, collected_at
		FROM github_repository_snapshots WHERE repository_id = ? AND collected_at >= ? ORDER BY collected_at ASC`, id, cutoff)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var snapshots []Snapshot
	for rows.Next() {
		var snap Snapshot
		if err := rows.Scan(&snap.ID, &snap.RepositoryID, &snap.Stars, &snap.Forks, &snap.Watchers, &snap.OpenIssues, &snap.OpenPullRequests,
			&snap.CommitCount, &snap.ReleaseCount, &snap.ContributorCount, &snap.ActionsTotal, &snap.ActionsSuccess, &snap.ActionsFailed,
			&snap.TrafficViews, &snap.TrafficUniques, &snap.TrafficClones, &snap.TrafficCloneUniques, &snap.CollectedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		snapshots = append(snapshots, snap)
	}
	response.OK(w, map[string]interface{}{"days": days, "snapshots": snapshots})
}

func (s *Service) repositoryActionRuns(w http.ResponseWriter, r *http.Request, idText string) {
	id, _ := parseID(idText)
	limit := clamp(intQuery(r, "limit", 50), 1, 200)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(r.Context(), `SELECT run_id, workflow_name, display_title, status, conclusion, event, branch, commit_sha, commit_message, actor, html_url,
		run_started_at, created_at, updated_at, collected_at FROM github_action_runs WHERE repository_id = ? ORDER BY COALESCE(created_at, collected_at) DESC LIMIT ?`, id, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var runs []map[string]interface{}
	for rows.Next() {
		var runID int64
		var workflow, title, status, conclusion, event, branch, sha, commitMessage, actor, htmlURL string
		var started, created, updated, collected sql.NullString
		if err := rows.Scan(&runID, &workflow, &title, &status, &conclusion, &event, &branch, &sha, &commitMessage, &actor, &htmlURL, &started, &created, &updated, &collected); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		runs = append(runs, map[string]interface{}{"run_id": runID, "workflow_name": workflow, "display_title": title, "status": status, "conclusion": conclusion, "event": event, "branch": branch, "commit_sha": sha, "commit_message": commitMessage, "actor": actor, "html_url": htmlURL, "run_started_at": nullString(started), "created_at": nullString(created), "updated_at": nullString(updated), "collected_at": nullString(collected)})
	}
	response.OK(w, runs)
}

func (s *Service) repositoryWorkflowJobs(w http.ResponseWriter, r *http.Request, idText, runText string) {
	runID, err := parseID(runText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid workflow run id")
		return
	}
	repo, token, db, ok := s.repoAndTokenForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	jobs, _, err := s.client.fetchWorkflowJobs(r.Context(), token, repo.Owner, repo.Name, runID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	layout := s.workflowLayoutForRun(r.Context(), token, repo, r.URL.Query().Get("workflow_name"), r.URL.Query().Get("branch"), r.URL.Query().Get("commit_sha"))
	response.OK(w, workflowJobsDetailResponse{Jobs: jobs.Jobs, Workflow: layout})
}

func (s *Service) workflowLayoutForRun(ctx context.Context, token string, repo Repository, workflowName, branch, commitSHA string) *workflowLayoutResponse {
	ref := firstNonEmpty(commitSHA, branch, repo.DefaultBranch)
	layout := &workflowLayoutResponse{Ref: ref}
	workflows, _, err := s.client.fetchWorkflows(ctx, token, repo.Owner, repo.Name)
	if err != nil {
		layout.Error = err.Error()
		return layout
	}
	var workflowPath string
	for _, workflow := range workflows.Workflows {
		if strings.EqualFold(strings.TrimSpace(workflow.Name), strings.TrimSpace(workflowName)) {
			workflowPath = workflow.Path
			break
		}
	}
	if workflowPath == "" && len(workflows.Workflows) == 1 {
		workflowPath = workflows.Workflows[0].Path
	}
	if workflowPath == "" {
		layout.Error = "未找到匹配的 workflow yml"
		return layout
	}
	layout.Path = workflowPath
	raw, file, _, err := s.client.fetchWorkflowFile(ctx, token, repo.Owner, repo.Name, workflowPath, ref)
	if err != nil && strings.TrimSpace(branch) != "" && !strings.EqualFold(strings.TrimSpace(ref), strings.TrimSpace(branch)) {
		branchRaw, branchFile, _, branchErr := s.client.fetchWorkflowFile(ctx, token, repo.Owner, repo.Name, workflowPath, branch)
		if branchErr == nil {
			raw = branchRaw
			file = branchFile
			ref = branch
			layout.Ref = ref
			err = nil
		}
	}
	if err != nil && strings.TrimSpace(repo.DefaultBranch) != "" && !strings.EqualFold(strings.TrimSpace(ref), strings.TrimSpace(repo.DefaultBranch)) {
		defaultRaw, defaultFile, _, defaultErr := s.client.fetchWorkflowFile(ctx, token, repo.Owner, repo.Name, workflowPath, repo.DefaultBranch)
		if defaultErr == nil {
			raw = defaultRaw
			file = defaultFile
			ref = repo.DefaultBranch
			layout.Ref = ref
			err = nil
		}
	}
	if err != nil {
		layout.Error = err.Error()
		return layout
	}
	if strings.TrimSpace(file.Path) != "" {
		layout.Path = file.Path
	}
	layers, err := parseWorkflowLayout(raw)
	if err != nil {
		layout.Error = err.Error()
		return layout
	}
	layout.Layers = layers
	return layout
}

func (s *Service) refreshRepositoryActions(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid repository id")
		return
	}
	if err := s.refreshActionsRepositoryByID(r.Context(), id, "manual_actions_refresh"); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"repository_id": id, "status": "refreshed"})
}

func (s *Service) repositoryWorkflows(w http.ResponseWriter, r *http.Request, idText string) {
	repo, token, db, ok := s.repoAndTokenForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	workflows, _, err := s.client.fetchWorkflows(r.Context(), token, repo.Owner, repo.Name)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, workflows.Workflows)
}

func (s *Service) repositoryBranches(w http.ResponseWriter, r *http.Request, idText string) {
	repo, token, db, ok := s.repoAndTokenForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	branches, _, err := s.client.fetchBranches(r.Context(), token, repo.Owner, repo.Name)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, branches)
}

func (s *Service) configureRepositoryWebhook(w http.ResponseWriter, r *http.Request, idText string) {
	repo, token, db, ok := s.repoAndTokenForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	payloadURL := strings.TrimSpace(stringValue(payload, "payload_url", ""))
	parsedURL, err := url.Parse(payloadURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" || strings.EqualFold(parsedURL.Hostname(), "localhost") || parsedURL.Hostname() == "127.0.0.1" || parsedURL.Hostname() == "::1" {
		response.Error(w, http.StatusBadRequest, "Webhook Payload URL 必须是可供 GitHub 访问的公网 HTTPS 地址")
		return
	}
	secret := repo.WebhookSecret
	if secret == "" {
		secret = randomSecret()
		if _, err := db.ExecContext(r.Context(), `UPDATE github_repositories SET webhook_secret = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, secret, repo.ID); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	hookID, created, _, err := s.client.configureWebhook(r.Context(), token, repo.Owner, repo.Name, payloadURL, secret)
	s.auditOperation(r.Context(), db, repo.ID, "webhook_configure", payloadURL, payload, map[string]interface{}{"hook_id": hookID, "created": created}, err)
	if err != nil {
		message := err.Error()
		if webhookPermissionDenied(err) {
			message = fmt.Sprintf("Token 无权管理 %s 的 Webhook。Fine-grained PAT 需要将 Resource owner 设为 %s、授权当前仓库并开启 Webhooks: read and write；若组织要求审批，还需等待组织管理员批准。", repo.FullName, repo.Owner)
		}
		response.Error(w, http.StatusBadGateway, message)
		return
	}
	if _, err := db.ExecContext(r.Context(), `UPDATE github_repositories SET webhook_enabled = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, repo.ID); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"hook_id": hookID, "created": created, "payload_url": payloadURL})
}

func webhookPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "resource not accessible by personal access token") ||
		strings.Contains(message, "requires webhooks") ||
		strings.Contains(message, "must have webhooks")
}

func (s *Service) repositoryTraffic(w http.ResponseWriter, r *http.Request, idText string) {
	s.queryTable(w, r, idText, `SELECT views, view_uniques, clones, clone_uniques, collected_at FROM github_traffic_samples WHERE repository_id = ? ORDER BY collected_at DESC LIMIT ?`, []string{"views", "view_uniques", "clones", "clone_uniques", "collected_at"})
}

func (s *Service) repositoryContributors(w http.ResponseWriter, r *http.Request, idText string) {
	s.queryTable(w, r, idText, `SELECT login, avatar_url, html_url, contributions, collected_at FROM github_contributors WHERE repository_id = ? ORDER BY contributions DESC LIMIT ?`, []string{"login", "avatar_url", "html_url", "contributions", "collected_at"})
}

func (s *Service) repositoryEvents(w http.ResponseWriter, r *http.Request, idText string) {
	id, _ := parseID(idText)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	events, err := listEvents(r.Context(), db, id, intQuery(r, "limit", 100))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, events)
}

func (s *Service) queryTable(w http.ResponseWriter, r *http.Request, idText, query string, columns []string) {
	id, _ := parseID(idText)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(r.Context(), query, id, clamp(intQuery(r, "limit", 100), 1, 500))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		ptrs := make([]interface{}, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		item := map[string]interface{}{}
		for i, column := range columns {
			item[column] = normalizeDBValue(values[i])
		}
		result = append(result, item)
	}
	response.OK(w, result)
}

func (s *Service) settings(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodGet:
		settings, err := loadSettings(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, settings)
	case http.MethodPut, http.MethodPatch:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		settings, err := updateSettings(r.Context(), db, payload)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, settings)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) runCollector(w http.ResponseWriter, r *http.Request) {
	if err := s.collectOnce(r.Context()); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, s.currentStatus())
}

func (s *Service) collectorStatus(w http.ResponseWriter, r *http.Request) {
	response.OK(w, s.currentStatus())
}

func (s *Service) deleteHistory(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	repoID := int64Query(r, "repositoryId", 0)
	days := intQuery(r, "days", 90)
	result, err := cleanupHistory(r.Context(), db, repoID, days)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	var deleted int64
	for _, count := range result {
		deleted += count
	}
	if deleted > 0 {
		if err := reclaimDatabaseSpace(r.Context(), db); err != nil {
			response.Error(w, http.StatusInternalServerError, "github history vacuum failed: "+err.Error())
			return
		}
	}
	response.OK(w, result)
}

func (s *Service) compactHistory(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	repoID := int64Query(r, "repositoryId", 0)
	result, err := compactHistoryPayloads(r.Context(), db, repoID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated := result["github_events"] + result["github_webhook_deliveries"]
	if updated > 0 {
		if err := reclaimDatabaseSpace(r.Context(), db); err != nil {
			response.Error(w, http.StatusInternalServerError, "github history vacuum failed: "+err.Error())
			return
		}
	}
	response.OK(w, result)
}

// reclaimDatabaseSpace 分批增量回收删除后留下的空闲页（每批 4096 页，
// 每次持写锁仅几十毫秒），避免全量 VACUUM 长时间独占数据库。
// auto_vacuum=NONE 的存量库无法增量回收（全量迁移由设置页「数据库压缩」处理），
// 此处静默跳过，原有的 wal_checkpoint 逻辑保留。
func reclaimDatabaseSpace(ctx context.Context, db *sql.DB) error {
	var mode int
	if err := db.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		return err
	}
	if mode == 2 {
		for rounds := 0; rounds < 64; rounds++ {
			var freePages int64
			if err := db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freePages); err != nil || freePages == 0 {
				break
			}
			if _, err := db.ExecContext(ctx, `PRAGMA incremental_vacuum(4096)`); err != nil {
				return err
			}
		}
	}
	_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return nil
}

func (s *Service) events(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	events, err := listEvents(r.Context(), db, int64Query(r, "repositoryId", 0), intQuery(r, "limit", 100))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, events)
}

func (s *Service) eventStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		response.Error(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch, cancel := s.subscribe()
	defer cancel()
	if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
		return
	}
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", jsonString(map[string]interface{}{"connected": true}))
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
				return
			}
			fmt.Fprintf(w, "event: github\ndata: %s\n\n", jsonString(event))
			flusher.Flush()
		}
	}
}

func (s *Service) workflowRunOperation(w http.ResponseWriter, r *http.Request, idText, runText, operation string) {
	repo, token, db, ok := s.repoAndTokenForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	runID, err := strconv.ParseInt(runText, 10, 64)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid run id")
		return
	}
	var opErr error
	if operation == "cancel" {
		_, opErr = s.client.cancelWorkflowRun(r.Context(), token, repo.Owner, repo.Name, runID)
	} else {
		_, opErr = s.client.rerunWorkflowRun(r.Context(), token, repo.Owner, repo.Name, runID, operation == "rerun-failed-jobs")
	}
	s.auditOperation(r.Context(), db, repo.ID, operation, runText, map[string]interface{}{}, map[string]interface{}{"ok": opErr == nil}, opErr)
	if opErr != nil {
		status := http.StatusBadGateway
		if actionPermissionDenied(opErr) {
			_, _ = db.ExecContext(r.Context(), `UPDATE github_repositories SET can_operate_actions = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, repo.ID)
			status = http.StatusForbidden
		}
		response.Error(w, status, opErr.Error())
		return
	}
	response.OK(w, map[string]interface{}{"operation": operation, "run_id": runID, "status": "submitted"})
}

func (s *Service) workflowDispatch(w http.ResponseWriter, r *http.Request, idText, workflowID string) {
	repo, token, db, ok := s.repoAndTokenForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	payload, _ := readObject(r)
	ref := firstNonEmpty(stringValue(payload, "ref", ""), repo.DefaultBranch)
	inputs := objectValue(payload["inputs"])
	_, err := s.client.dispatchWorkflow(r.Context(), token, repo.Owner, repo.Name, workflowID, ref, inputs)
	s.auditOperation(r.Context(), db, repo.ID, "workflow_dispatch", workflowID, payload, map[string]interface{}{"ref": ref}, err)
	if err != nil {
		status := http.StatusBadGateway
		if actionPermissionDenied(err) {
			_, _ = db.ExecContext(r.Context(), `UPDATE github_repositories SET can_operate_actions = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, repo.ID)
			status = http.StatusForbidden
		}
		response.Error(w, status, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"workflow_id": workflowID, "ref": ref, "status": "submitted"})
}

func actionPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "resource not accessible by personal access token") ||
		strings.Contains(message, "must have actions: write") ||
		strings.Contains(message, "requires actions: write")
}

func (s *Service) webhook(w http.ResponseWriter, r *http.Request, parts []string) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	repoID := int64(0)
	if len(parts) >= 2 {
		repoID, _ = parseID(parts[1])
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	var payload map[string]interface{}
	_ = json.Unmarshal(raw, &payload)
	var repo Repository
	ok := false
	if repoID > 0 {
		repo, ok, err = getRepository(r.Context(), db, repoID)
		if err != nil || !ok {
			response.Error(w, http.StatusNotFound, "仓库不存在")
			return
		}
	} else {
		fullName := stringValue(objectValue(payload["repository"]), "full_name", "")
		repo, ok, err = getRepositoryByFullName(r.Context(), db, fullName)
		if err != nil || !ok {
			response.Error(w, http.StatusNotFound, "仓库不存在")
			return
		}
		repoID = repo.ID
	}
	delivery := r.Header.Get("X-GitHub-Delivery")
	eventType := r.Header.Get("X-GitHub-Event")
	valid := verifySignature(raw, repo.WebhookSecret, r.Header.Get("X-Hub-Signature-256"))
	duplicate := false
	if delivery != "" {
		var count int
		_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM github_webhook_deliveries WHERE delivery_id = ?`, delivery).Scan(&count)
		duplicate = count > 0
	}
	storedPayload, _ := json.Marshal(compactWebhookDeliveryPayload(eventType, payload, raw))
	_, _ = db.ExecContext(r.Context(), `INSERT OR IGNORE INTO github_webhook_deliveries (repository_id, delivery_id, event_type, signature_valid, duplicate, payload_json)
		VALUES (?, ?, ?, ?, ?, ?)`, repoID, delivery, eventType, boolInt(valid), boolInt(duplicate), string(storedPayload))
	if !valid {
		response.Error(w, http.StatusUnauthorized, "GitHub webhook signature invalid")
		return
	}
	if duplicate {
		response.OK(w, map[string]interface{}{"duplicate": true})
		return
	}
	s.handleWebhookEvent(r.Context(), db, repo, eventType, payload)
	response.OK(w, map[string]interface{}{"received": true, "event": eventType})
}

func (s *Service) tokenForRequest(w http.ResponseWriter, r *http.Request, idText string) (Token, *sql.DB, bool) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid token id")
		return Token{}, nil, false
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return Token{}, nil, false
	}
	token, ok, err := getToken(r.Context(), db, id)
	if err != nil {
		db.Close()
		response.Error(w, http.StatusInternalServerError, err.Error())
		return Token{}, nil, false
	}
	if !ok {
		db.Close()
		response.Error(w, http.StatusNotFound, "GitHub Token 不存在")
		return Token{}, nil, false
	}
	return token, db, true
}

func (s *Service) repoAndTokenForRequest(w http.ResponseWriter, r *http.Request, idText string) (Repository, string, *sql.DB, bool) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid repository id")
		return Repository{}, "", nil, false
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return Repository{}, "", nil, false
	}
	repo, ok, err := getRepository(r.Context(), db, id)
	if err != nil || !ok {
		db.Close()
		response.Error(w, http.StatusNotFound, "仓库不存在")
		return Repository{}, "", nil, false
	}
	token, err := s.tokenForRepository(r.Context(), db, repo)
	if err != nil {
		db.Close()
		response.Error(w, http.StatusBadRequest, err.Error())
		return Repository{}, "", nil, false
	}
	return repo, token, db, true
}

func (s *Service) tokenForRepository(ctx context.Context, db *sql.DB, repo Repository) (string, error) {
	if repo.TokenID != nil {
		token, ok, err := getToken(ctx, db, *repo.TokenID)
		if err != nil {
			return "", err
		}
		if !ok || !token.Enabled {
			return "", errors.New("仓库绑定的 GitHub Token 不可用")
		}
		return secure.SecureDecrypt(token.TokenEncrypted), nil
	}
	if token, ok, err := getDefaultToken(ctx, db); err != nil {
		return "", err
	} else if ok {
		return secure.SecureDecrypt(token.TokenEncrypted), nil
	}
	if repo.Private {
		return "", errors.New("私有仓库需要 GitHub Token")
	}
	return "", nil
}

func (s *Service) tokenAccountLoginForRepository(ctx context.Context, db *sql.DB, repo Repository) string {
	if repo.TokenID != nil {
		if token, ok, err := getToken(ctx, db, *repo.TokenID); err == nil && ok && token.Enabled {
			return token.AccountLogin
		}
		return ""
	}
	if token, ok, err := getDefaultToken(ctx, db); err == nil && ok && token.Enabled {
		return token.AccountLogin
	}
	return ""
}

func (s *Service) auditOperation(ctx context.Context, db *sql.DB, repoID int64, operation, target string, req, res map[string]interface{}, opErr error) {
	status := "success"
	errMsg := ""
	if opErr != nil {
		status = "failed"
		errMsg = opErr.Error()
	}
	_, _ = db.ExecContext(ctx, `INSERT INTO github_operation_audit (repository_id, operation, target, status, request_json, response_json, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, repoID, operation, target, status, jsonString(req), jsonString(res), errMsg)
}

func safeTokens(tokens []Token) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(tokens))
	for _, token := range tokens {
		result = append(result, safeToken(token))
	}
	return result
}

func safeToken(token Token) map[string]interface{} {
	item := map[string]interface{}{
		"id": token.ID, "name": token.Name, "type": token.Type, "enabled": token.Enabled,
		"default_token": token.DefaultToken, "note": token.Note, "account_login": token.AccountLogin, "scopes": token.Scopes,
		"permissions_json": token.PermissionsJSON, "last_test_status": token.LastTestStatus,
		"created_at": token.CreatedAt, "updated_at": token.UpdatedAt, "has_token": token.TokenEncrypted != "",
	}
	if token.LastTestError != "" {
		item["last_test_error"] = token.LastTestError
	}
	if token.LastTestAt != nil {
		item["last_test_at"] = *token.LastTestAt
	}
	return item
}

func verifySignature(body []byte, secret, signature string) bool {
	if secret == "" || signature == "" || !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func randomSecret() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func parseRepoInput(input string) (string, string) {
	raw := strings.TrimSpace(strings.TrimSuffix(input, ".git"))
	if raw == "" {
		return "", ""
	}
	if strings.HasPrefix(raw, "git@github.com:") {
		raw = strings.TrimPrefix(raw, "git@github.com:")
	} else if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := url.Parse(raw)
		if err != nil || !strings.EqualFold(parsed.Host, "github.com") {
			return "", ""
		}
		raw = strings.Trim(parsed.Path, "/")
	}
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) < 2 {
		return "", ""
	}
	return cleanRepoPart(parts[0]), cleanRepoPart(parts[1])
}

func cleanRepoPart(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}
