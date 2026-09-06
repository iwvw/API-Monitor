package huawei

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type Service struct {
	cfg    config.Config
	store  *database.Store
	schema database.SchemaEnsurer
	http   *http.Client
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:   cfg,
		store: database.New(cfg),
		http:  &http.Client{Timeout: requestTimeout},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	} else {
		applog.Warn(ctx, "huawei", "init schema open failed", "error", err)
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 用 EscapedPath 切分再逐段 PathUnescape，避免含斜杠的动态段（OBS 对象名）失配。
	escapedPath := r.URL.EscapedPath()
	if escapedPath == "" {
		escapedPath = r.URL.Path
	}
	path := strings.TrimPrefix(escapedPath, "/api/huawei")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
		for i, part := range parts {
			if decoded, err := url.PathUnescape(part); err == nil {
				parts[i] = decoded
			}
		}
	}

	switch {
	case len(parts) == 1 && parts[0] == "accounts":
		s.accounts(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.accountMutation(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "verify" && r.Method == http.MethodPost:
		s.accountVerify(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "projects" && r.Method == http.MethodGet:
		s.projects(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "ssh" && r.Method == http.MethodGet:
		s.sshTerminal(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "defaults" && r.Method == http.MethodPut:
		s.setDefaults(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "flexus-instances" && r.Method == http.MethodGet:
		s.flexusInstances(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "flexus-instances" && parts[4] == "actions" && r.Method == http.MethodPost:
		s.flexusAction(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "flexus-instances" && parts[4] == "reset-password" && r.Method == http.MethodPost:
		s.flexusResetPassword(w, r, parts[1], parts[3])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "flexus-instances" && r.Method == http.MethodPut:
		s.flexusRename(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "instances" && r.Method == http.MethodGet:
		s.instances(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "instances" && parts[5] == "actions" && r.Method == http.MethodPost:
		s.instanceAction(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "instances":
		s.instanceDetail(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "instances" && parts[6] == "reset-password" && r.Method == http.MethodPost:
		s.instanceResetPassword(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "dns" && parts[5] == "zones":
		s.zones(w, r, parts[1], parts[3])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "dns" && parts[5] == "zones":
		s.zoneDetail(w, r, parts[1], parts[3], parts[6])
	case len(parts) == 8 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "dns" && parts[5] == "zones" && parts[7] == "recordsets":
		s.recordsets(w, r, parts[1], parts[3], parts[6])
	case len(parts) == 9 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "dns" && parts[5] == "zones" && parts[7] == "recordsets":
		s.recordsetMutation(w, r, parts[1], parts[3], parts[6], parts[8])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "eips" && r.Method == http.MethodGet:
		s.eips(w, r, parts[1], parts[3])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "eips" && (parts[6] == "associate" || parts[6] == "disassociate") && r.Method == http.MethodPut:
		s.eipAssociate(w, r, parts[1], parts[3], parts[5], parts[6])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "vpcs" && r.Method == http.MethodGet:
		s.vpcs(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "security-groups" && r.Method == http.MethodGet:
		s.securityGroups(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "buckets":
		s.buckets(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "buckets" && r.Method == http.MethodDelete:
		s.deleteBucket(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "buckets" && parts[6] == "objects":
		s.objects(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 8 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "buckets" && parts[6] == "objects" && r.Method == http.MethodDelete:
		s.deleteObject(w, r, parts[1], parts[3], parts[5], parts[7])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "billing" && (parts[3] == "overview" || parts[3] == "free-resources") && r.Method == http.MethodGet:
		s.billing(w, r, parts[1], parts[3])
	default:
		response.Error(w, http.StatusNotFound, "huawei route not implemented")
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

func (s *Service) accounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		accounts, err := listAccounts(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		safe := make([]map[string]interface{}, 0, len(accounts))
		for _, account := range accounts {
			safe = append(safe, safeAccount(account))
		}
		response.OK(w, safe)
	case http.MethodPost:
		var payload accountPayload
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		id, err := createAccount(r.Context(), db, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": id})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) accountMutation(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid account id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodPut:
		var payload accountPayload
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := updateAccount(r.Context(), db, id, payload); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "huawei account not found")
				return
			}
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": id})
	case http.MethodDelete:
		if err := deleteAccount(r.Context(), db, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "huawei account not found")
				return
			}
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": id})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) accountVerify(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	projects, domainID, err := s.verifyAccount(r.Context(), account)
	if err != nil {
		_ = updateVerifyStatus(r.Context(), db, account.ID, "failed", err.Error())
		response.Error(w, http.StatusBadGateway, "验证华为云账号失败："+err.Error())
		return
	}
	_ = updateVerifyStatus(r.Context(), db, account.ID, "success", "")
	if domainID != "" {
		_ = updateDomainID(r.Context(), db, account.ID, domainID)
	}
	// 未设置默认项目时自动写入第一个项目。
	if account.DefaultProjectID == "" && len(projects) > 0 {
		_ = updateDefaults(r.Context(), db, account.ID, account.DefaultRegion, projects[0].ProjectID)
	}
	response.OK(w, map[string]interface{}{
		"status":   "success",
		"message":  "账号验证成功",
		"projects": projects,
		"domainId": domainID,
	})
}

func (s *Service) accountForRequest(w http.ResponseWriter, r *http.Request, idText string) (Account, *sql.DB, bool) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid account id")
		return Account{}, nil, false
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return Account{}, nil, false
	}
	account, err := getAccount(r.Context(), db, id)
	if err != nil {
		db.Close()
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "huawei account not found")
			return Account{}, nil, false
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return Account{}, nil, false
	}
	return account, db, true
}

// clientForAccount 解密账号 SK 构造签名 client。
func (s *Service) clientForAccount(ctx context.Context, w http.ResponseWriter, account Account) (*client, bool) {
	if account.SecretAccessKey == "" {
		if w != nil {
			response.Error(w, http.StatusBadRequest, "账号缺少 Secret Access Key")
		}
		return nil, false
	}
	c := &client{http: s.http, ak: account.AccessKeyID, sk: account.SecretAccessKey, site: account.Site}
	return c, true
}

// clientWithRegion 构造签名 client 并解析 project 对应区域。
func (s *Service) clientWithRegion(ctx context.Context, w http.ResponseWriter, account Account, projectID string) (*client, bool) {
	c, ok := s.clientForAccount(ctx, w, account)
	if !ok {
		return nil, false
	}
	region, err := s.resolveRegion(ctx, c, projectID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return nil, false
	}
	if region == "" {
		region = account.DefaultRegion
	}
	if region == "" {
		response.Error(w, http.StatusBadRequest, "无法确定项目区域，请先验证账号")
		return nil, false
	}
	c.region = region
	return c, true
}

func (s *Service) resolveRegion(ctx context.Context, c *client, projectID string) (string, error) {
	projects, err := s.fetchProjects(ctx, c)
	if err != nil {
		return "", err
	}
	for _, project := range projects {
		if project.ProjectID == projectID {
			return project.Name, nil
		}
	}
	return "", nil
}

// regionClient 返回指定区域子 client（复用凭证，仅区域不同）。
func regionClient(c *client, region string) *client {
	clone := *c
	clone.region = region
	return &clone
}

// regionForProject 解析区域：projectID 为 all/空 时返回第一个项目区域。
func (s *Service) regionForProject(ctx context.Context, c *client, projectID string) (string, error) {
	projects, err := s.fetchProjects(ctx, c)
	if err != nil {
		return "", err
	}
	if projectID == "" || projectID == "all" {
		if len(projects) == 0 {
			return "", errors.New("账号下无可用项目")
		}
		return projects[0].Name, nil
	}
	for _, project := range projects {
		if project.ProjectID == projectID {
			return project.Name, nil
		}
	}
	return "", fmt.Errorf("未找到项目 %s", projectID)
}

// projectQueryTimeout 单区域查询超时：聚合模式下不可用/慢区域快速跳过，
// 避免最慢区域把整体请求拖到超时（实测部分区域 API 端点响应极慢）。
const projectQueryTimeout = 8 * time.Second

// aggregateProjects 并发遍历项目执行 fetch，跳过失败项目并返回成功结果；
// 全部失败时返回首个错误摘要（all 模式聚合不因单个区域失败而整体 502）。
func aggregateProjects[T any](ctx context.Context, projects []normalProject, fetch func(ctx context.Context, project normalProject) ([]T, error)) ([]T, error) {
	type result struct {
		list []T
		err  error
	}
	results := make(chan result, len(projects))
	var wg sync.WaitGroup
	for _, project := range projects {
		wg.Add(1)
		go func(p normalProject) {
			defer wg.Done()
			fetchCtx, cancel := context.WithTimeout(ctx, projectQueryTimeout)
			defer cancel()
			list, err := fetch(fetchCtx, p)
			if err != nil {
				applog.Warn(ctx, "huawei", "region aggregation skipped", "region", p.Name, "project", p.ProjectID, "error", err.Error())
			}
			results <- result{list: list, err: err}
		}(project)
	}
	wg.Wait()
	close(results)
	var all []T
	var errs []string
	successCount := 0
	for r := range results {
		if r.err != nil {
			errs = append(errs, r.err.Error())
			continue
		}
		successCount++
		all = append(all, r.list...)
	}
	// 只要有一个区域查询成功（即使实例为空列表）就返回聚合结果；
	// 全部区域失败（含 IAM 受限/服务未开通/域名不可解析等）才报错。
	if successCount == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("全部区域查询失败: %s", strings.Join(errs, "; "))
	}
	return all, nil
}

func decodeJSON(r *http.Request, target interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func writeResult(w http.ResponseWriter, data interface{}, err error) {
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, data)
}
