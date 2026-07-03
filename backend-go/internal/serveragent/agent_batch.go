package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type AgentBatchStatus string

const (
	AgentBatchQueued    AgentBatchStatus = "queued"
	AgentBatchRunning   AgentBatchStatus = "running"
	AgentBatchVerifying AgentBatchStatus = "verifying"
	AgentBatchSucceeded AgentBatchStatus = "succeeded"
	AgentBatchFailed    AgentBatchStatus = "failed"
)

type AgentBatchKind string

const (
	AgentBatchInstall AgentBatchKind = "install"
	AgentBatchUpgrade AgentBatchKind = "upgrade"
)

type AgentBatchItem struct {
	ServerID    string           `json:"serverId"`
	ServerName  string           `json:"serverName"`
	Status      AgentBatchStatus `json:"status"`
	Error       string           `json:"error,omitempty"`
	Log         []string         `json:"log,omitempty"`
	StartedAt   string           `json:"startedAt,omitempty"`
	CompletedAt string           `json:"completedAt,omitempty"`

	mu sync.RWMutex
}

type AgentBatch struct {
	ID          string                     `json:"id"`
	Kind        AgentBatchKind             `json:"kind"`
	Status      AgentBatchStatus           `json:"status"`
	Protocol    string                     `json:"protocol"`
	ForceSSH    bool                       `json:"forceSsh"`
	FallbackSSH bool                       `json:"fallbackSsh"`
	Concurrency int                        `json:"concurrency"`
	CreatedAt   string                     `json:"createdAt"`
	UpdatedAt   string                     `json:"updatedAt"`
	Items       map[string]*AgentBatchItem `json:"items"`

	mu sync.RWMutex
}

type AgentBatchSnapshot struct {
	ID          string                   `json:"id"`
	Kind        AgentBatchKind           `json:"kind"`
	Status      AgentBatchStatus         `json:"status"`
	Protocol    string                   `json:"protocol"`
	ForceSSH    bool                     `json:"forceSsh"`
	FallbackSSH bool                     `json:"fallbackSsh"`
	Concurrency int                      `json:"concurrency"`
	CreatedAt   string                   `json:"createdAt"`
	UpdatedAt   string                   `json:"updatedAt"`
	Summary     map[string]int           `json:"summary"`
	Items       []AgentBatchItemSnapshot `json:"items"`
}

type AgentBatchItemSnapshot struct {
	ServerID    string           `json:"serverId"`
	ServerName  string           `json:"serverName"`
	Status      AgentBatchStatus `json:"status"`
	Error       string           `json:"error,omitempty"`
	Log         []string         `json:"log,omitempty"`
	StartedAt   string           `json:"startedAt,omitempty"`
	CompletedAt string           `json:"completedAt,omitempty"`
}

type AgentBatchManager struct {
	mu      sync.RWMutex
	batches map[string]*AgentBatch
}

func NewAgentBatchManager() *AgentBatchManager {
	return &AgentBatchManager{batches: make(map[string]*AgentBatch)}
}

func (m *AgentBatchManager) Create(kind AgentBatchKind, protocol string, forceSSH bool, fallbackSSH bool, concurrency int, servers []serverIdentity) *AgentBatch {
	if concurrency <= 0 {
		concurrency = 4
	}
	if concurrency > 10 {
		concurrency = 10
	}
	if protocol != "http" && protocol != "https" {
		protocol = "https"
	}

	now := time.Now().Format(time.RFC3339Nano)
	batch := &AgentBatch{
		ID:          uuid.NewString(),
		Kind:        kind,
		Status:      AgentBatchQueued,
		Protocol:    protocol,
		ForceSSH:    forceSSH,
		FallbackSSH: fallbackSSH,
		Concurrency: concurrency,
		CreatedAt:   now,
		UpdatedAt:   now,
		Items:       make(map[string]*AgentBatchItem, len(servers)),
	}
	for _, server := range servers {
		batch.Items[server.ID] = &AgentBatchItem{
			ServerID:   server.ID,
			ServerName: server.Name,
			Status:     AgentBatchQueued,
		}
	}

	m.mu.Lock()
	m.batches[batch.ID] = batch
	m.mu.Unlock()
	return batch
}

func (m *AgentBatchManager) Get(id string) (*AgentBatch, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	batch, ok := m.batches[id]
	return batch, ok
}

func (b *AgentBatch) snapshot() AgentBatchSnapshot {
	b.mu.RLock()
	snapshot := AgentBatchSnapshot{
		ID:          b.ID,
		Kind:        b.Kind,
		Status:      b.Status,
		Protocol:    b.Protocol,
		ForceSSH:    b.ForceSSH,
		FallbackSSH: b.FallbackSSH,
		Concurrency: b.Concurrency,
		CreatedAt:   b.CreatedAt,
		UpdatedAt:   b.UpdatedAt,
		Summary: map[string]int{
			"queued":    0,
			"running":   0,
			"verifying": 0,
			"succeeded": 0,
			"failed":    0,
		},
		Items: make([]AgentBatchItemSnapshot, 0, len(b.Items)),
	}
	items := make([]*AgentBatchItem, 0, len(b.Items))
	for _, item := range b.Items {
		items = append(items, item)
	}
	b.mu.RUnlock()

	for _, item := range items {
		item.mu.RLock()
		logCopy := append([]string(nil), item.Log...)
		itemSnapshot := AgentBatchItemSnapshot{
			ServerID:    item.ServerID,
			ServerName:  item.ServerName,
			Status:      item.Status,
			Error:       item.Error,
			Log:         logCopy,
			StartedAt:   item.StartedAt,
			CompletedAt: item.CompletedAt,
		}
		snapshot.Summary[string(item.Status)]++
		item.mu.RUnlock()
		snapshot.Items = append(snapshot.Items, itemSnapshot)
	}
	return snapshot
}

type serverIdentity struct {
	ID   string
	Name string
}

type agentBatchRequest struct {
	ServerIDs   []string `json:"serverIds"`
	ForceSSH    bool     `json:"force_ssh"`
	FallbackSSH bool     `json:"fallback_ssh"`
	Concurrency int      `json:"concurrency"`
}

type agentInstallOrigin struct {
	Proto string
	Host  string
}

func (s *Service) handleAgentBatchInstall(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	s.handleAgentBatchStart(w, r, db, AgentBatchInstall)
}

func (s *Service) handleAgentBatchUpgrade(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	s.handleAgentBatchStart(w, r, db, AgentBatchUpgrade)
}

func (s *Service) handleAgentBatchStart(w http.ResponseWriter, r *http.Request, db *sql.DB, kind AgentBatchKind) {
	var req agentBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.ServerIDs) == 0 {
		response.Error(w, http.StatusBadRequest, "serverIds required")
		return
	}

	proto, host := resolveInstallOrigin(r)
	servers, err := loadServerIdentities(r.Context(), db, req.ServerIDs)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(servers) == 0 {
		response.Error(w, http.StatusBadRequest, "no matching servers")
		return
	}

	forceSSH := req.ForceSSH
	if kind == AgentBatchInstall {
		forceSSH = req.ForceSSH
	}

	batch := s.agentBatches.Create(kind, proto, forceSSH, req.FallbackSSH, req.Concurrency, servers)
	go s.runAgentBatch(batch, agentInstallOrigin{Proto: proto, Host: host})

	response.OK(w, batch.snapshot())
}

func (s *Service) handleAgentBatchStatus(w http.ResponseWriter, r *http.Request, batchID string) {
	batch, ok := s.agentBatches.Get(batchID)
	if !ok {
		response.Error(w, http.StatusNotFound, "batch not found")
		return
	}
	response.OK(w, batch.snapshot())
}

func loadServerIdentities(ctx context.Context, db *sql.DB, ids []string) ([]serverIdentity, error) {
	out := make([]serverIdentity, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		var name, host string
		err := db.QueryRowContext(ctx, `SELECT name, host FROM server_accounts WHERE id = ?`, id).Scan(&name, &host)
		if err == sql.ErrNoRows {
			out = append(out, serverIdentity{ID: id, Name: "未知主机"})
			continue
		}
		if err != nil {
			return nil, err
		}
		if name == "" {
			name = host
		}
		if name == "" {
			name = id
		}
		out = append(out, serverIdentity{ID: id, Name: name})
	}
	return out, nil
}

func (s *Service) runAgentBatch(batch *AgentBatch, origin agentInstallOrigin) {
	batch.setStatus(AgentBatchRunning)

	jobs := make(chan *AgentBatchItem)
	var wg sync.WaitGroup
	for i := 0; i < batch.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				s.runAgentBatchItem(batch, item, origin)
			}
		}()
	}

	for _, item := range batch.Items {
		jobs <- item
	}
	close(jobs)
	wg.Wait()

	status := AgentBatchSucceeded
	for _, item := range batch.Items {
		item.mu.RLock()
		if item.Status != AgentBatchSucceeded {
			status = AgentBatchFailed
		}
		item.mu.RUnlock()
	}
	batch.setStatus(status)
}

func (s *Service) runAgentBatchItem(batch *AgentBatch, item *AgentBatchItem, origin agentInstallOrigin) {
	item.setStatus(AgentBatchRunning, "")
	item.appendLog("任务开始")

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	db, err := s.open(ctx)
	if err != nil {
		item.fail("打开数据库失败: " + err.Error())
		return
	}
	defer db.Close()

	forceSSH := batch.ForceSSH
	if batch.Kind == AgentBatchInstall {
		forceSSH = batch.ForceSSH
	}
	startedAt := time.Now()
	startMs := startedAt.UnixNano() / int64(time.Millisecond)

	if conn, exists := s.registry.Get(item.ServerID); exists && !forceSSH {
		item.appendLog("Agent 在线，发送自升级任务")
		if !s.sendUpgradeTask(conn) {
			item.fail("发送升级任务失败")
			return
		}
		item.setStatus(AgentBatchVerifying, "")
		if s.waitForAgentReconnect(item.ServerID, startMs, 90*time.Second) {
			item.succeed("Agent 已重新上线")
			return
		}
		if batch.Kind != AgentBatchUpgrade || !batch.FallbackSSH {
			item.fail("验证超时: Agent 未能在 90 秒内重新上线")
			return
		}
		item.appendLog("自升级验证超时，切换到 SSH 覆盖安装")
		forceSSH = true
	}

	if forceSSH {
		s.registry.Disconnect(item.ServerID)
		item.appendLog("使用 SSH 强制覆盖安装")
	} else {
		item.appendLog("Agent 离线，使用 SSH 安装")
	}

	script, err := s.renderAgentInstallScript(context.Background(), db, item.ServerID, origin)
	if err != nil {
		item.fail("生成安装脚本失败: " + err.Error())
		return
	}

	cmd := fmt.Sprintf("cat << 'EOF' > /tmp/agent_install.sh\n%s\nEOF\nsudo bash /tmp/agent_install.sh", script)
	output, err := s.executeSSHCommand(context.Background(), db, item.ServerID, cmd, 150*time.Second)
	if output != "" {
		item.appendLog(trimLog(output))
	}
	if err != nil {
		item.fail("SSH 安装失败: " + err.Error())
		return
	}

	item.setStatus(AgentBatchVerifying, "")
	item.appendLog("安装脚本执行成功，等待 Agent 连接")
	if s.waitForAgentReconnect(item.ServerID, startMs, 90*time.Second) {
		item.succeed("Agent 已上线")
		return
	}
	item.fail("验证超时: Agent 未能在 90 秒内上线")
}

func (s *Service) renderAgentInstallScript(ctx context.Context, db *sql.DB, serverID string, origin agentInstallOrigin) (string, error) {
	proto := origin.Proto
	if proto != "http" && proto != "https" {
		proto = "https"
	}
	host := strings.TrimSpace(origin.Host)
	if host == "" {
		return "", fmt.Errorf("install host is empty")
	}

	req := httptest.NewRequest(http.MethodGet, "/?protocol="+url.QueryEscape(proto), nil).WithContext(ctx)
	req.Host = host
	rec := httptest.NewRecorder()
	s.getAgentInstallScript(rec, req, db, serverID)
	if rec.Code != http.StatusOK {
		return "", errors.New(strings.TrimSpace(rec.Body.String()))
	}
	return rec.Body.String(), nil
}

func (s *Service) waitForAgentReconnect(serverID string, afterConnectedAtMs int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, exists := s.registry.Get(serverID); exists {
			connectedAt := conn.AuthenticatedAt.UnixNano() / int64(time.Millisecond)
			if connectedAt > afterConnectedAtMs {
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

func (b *AgentBatch) setStatus(status AgentBatchStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Status = status
	b.UpdatedAt = time.Now().Format(time.RFC3339Nano)
}

func (i *AgentBatchItem) setStatus(status AgentBatchStatus, errorMsg string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Status = status
	i.Error = errorMsg
	now := time.Now().Format(time.RFC3339Nano)
	if status == AgentBatchRunning && i.StartedAt == "" {
		i.StartedAt = now
	}
	if status == AgentBatchSucceeded || status == AgentBatchFailed {
		i.CompletedAt = now
	}
}

func (i *AgentBatchItem) appendLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Log = append(i.Log, line)
	if len(i.Log) > 20 {
		i.Log = i.Log[len(i.Log)-20:]
	}
}

func (i *AgentBatchItem) succeed(line string) {
	i.appendLog(line)
	i.setStatus(AgentBatchSucceeded, "")
}

func (i *AgentBatchItem) fail(errorMsg string) {
	i.appendLog(errorMsg)
	i.setStatus(AgentBatchFailed, errorMsg)
}

func trimLog(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.Join(lines, "\n")
}
